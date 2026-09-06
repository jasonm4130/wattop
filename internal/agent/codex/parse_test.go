package codex

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/fixture"
)

func codexFixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(fixture.CorpusDir(), "agent", "codex", name)
}

// TestStatusFromTaskEvents: busy iff the most recent of task_started/
// task_complete is task_started; waiting once task_complete has fired.
func TestStatusFromTaskEvents(t *testing.T) {
	lines, err := readAllLines(codexFixture(t, "full-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	r := &Rollout{}
	// Apply through task_started (ordinal 2, the third line) only: busy.
	for i := 0; i < 3; i++ {
		if err := r.Apply(lines[i]); err != nil {
			t.Fatalf("Apply line %d: %v", i, err)
		}
	}
	if r.Status != "busy" {
		t.Fatalf("after task_started: Status = %q, want busy", r.Status)
	}

	// Apply the rest, through task_complete: waiting.
	for i := 3; i < len(lines); i++ {
		if err := r.Apply(lines[i]); err != nil {
			t.Fatalf("Apply line %d: %v", i, err)
		}
	}
	if r.Status != "waiting" {
		t.Fatalf("after task_complete: Status = %q, want waiting", r.Status)
	}
}

// TestModelReadPerTurn: the model comes from turn_context.payload.model on
// this rollout, never a config default, and a different rollout with a
// different turn_context.model yields that rollout's own model.
func TestModelReadPerTurn(t *testing.T) {
	full, err := LoadRollout(codexFixture(t, "full-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if full.Model != "gpt-5.6-terra" {
		t.Fatalf("full-turn Model = %q, want gpt-5.6-terra", full.Model)
	}

	override, err := LoadRollout(codexFixture(t, "model-override.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if override.Model != "gpt-5.6-terra-mini" {
		t.Fatalf("model-override Model = %q, want gpt-5.6-terra-mini (the argv override, not a config default)", override.Model)
	}
}

// TestContextWindowFromTranscript: ContextExact is true and the window
// comes from the transcript's own token_count event, taking precedence over
// task_started's top-level fallback when both are present.
func TestContextWindowFromTranscript(t *testing.T) {
	full, err := LoadRollout(codexFixture(t, "full-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !full.ContextExact {
		t.Fatal("full-turn: ContextExact = false, want true")
	}
	if full.ContextMax != 258400 {
		t.Fatalf("full-turn: ContextMax = %d, want 258400", full.ContextMax)
	}
	if full.ContextUsed != 5380 {
		t.Fatalf("full-turn: ContextUsed = %d, want 5380 (total_tokens)", full.ContextUsed)
	}

	// billable_input = input - cached_input, and both fields must be raw
	// (never pre-subtracted here — Task 7's cost.go does that subtraction).
	if full.Usage.Input != 5200 || full.Usage.CachedInput != 3100 {
		t.Fatalf("full-turn Usage = %+v, want Input=5200 CachedInput=3100", full.Usage)
	}
	if billable := full.Usage.Input - full.Usage.CachedInput; billable != 2100 {
		t.Fatalf("billable_input = %d, want 2100", billable)
	}

	// model-override.jsonl has no token_count event, only task_started's
	// top-level model_context_window (128000): the fallback fires, but
	// ContextExact stays false since there is no total_tokens numerator to
	// pair it with.
	override, err := LoadRollout(codexFixture(t, "model-override.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if override.ContextExact {
		t.Fatal("model-override: ContextExact = true, want false (no token_count numerator)")
	}
	if override.ContextMax != 128000 {
		t.Fatalf("model-override: ContextMax = %d, want 128000 (task_started fallback)", override.ContextMax)
	}
}

func TestRateLimitsParse(t *testing.T) {
	r, err := LoadRollout(codexFixture(t, "rate-limits.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.RateLimits) != 2 {
		t.Fatalf("len(RateLimits) = %d, want 2", len(r.RateLimits))
	}
	var primary, secondary *float64
	for _, rl := range r.RateLimits {
		switch rl.Scope {
		case "primary":
			primary = rl.UsedPct
		case "secondary":
			secondary = rl.UsedPct
		}
	}
	if primary == nil || *primary != 92.5 {
		t.Fatalf("primary UsedPct = %v, want 92.5", primary)
	}
	if secondary == nil || *secondary != 61.0 {
		t.Fatalf("secondary UsedPct = %v, want 61.0", secondary)
	}
}

// TestSessionMetaMissingLeavesMetaAtZero: rate-limits.jsonl opens on
// turn_context with no session_meta at all — there is no session id and no
// session_meta timestamp for this rollout, and a zero MetaAt must make it
// unmatched in bind.go's time tiebreaker exactly like a candidate's zero
// StartTime.
func TestSessionMetaMissingLeavesMetaAtZero(t *testing.T) {
	r, err := LoadRollout(codexFixture(t, "rate-limits.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.SessionID != "" {
		t.Fatalf("SessionID = %q, want empty (no session_meta record in this fixture)", r.SessionID)
	}
	if !r.MetaAt.IsZero() {
		t.Fatalf("MetaAt = %v, want zero", r.MetaAt)
	}
}

func TestInferStatusStaleOverridesTaskEvent(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	mtime := now.Add(-10 * time.Minute)
	got := inferStatus("busy", mtime, now, 5*time.Minute)
	if got != "stale" {
		t.Fatalf("inferStatus = %q, want stale (mtime beyond idle threshold)", got)
	}

	fresh := now.Add(-1 * time.Minute)
	got = inferStatus("busy", fresh, now, 5*time.Minute)
	if got != "busy" {
		t.Fatalf("inferStatus = %q, want busy (within idle threshold)", got)
	}
}
