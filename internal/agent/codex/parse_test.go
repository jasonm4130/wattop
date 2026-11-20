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

func TestStatusFromTaskEvents(t *testing.T) {
	lines, err := readAllLines(codexFixture(t, "full-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	r := &Rollout{}
	for i := 0; i < 3; i++ {
		if err := r.Apply(lines[i]); err != nil {
			t.Fatalf("Apply line %d: %v", i, err)
		}
	}
	if r.Status != "busy" {
		t.Fatalf("after task_started: Status = %q, want busy", r.Status)
	}

	for i := 3; i < len(lines); i++ {
		if err := r.Apply(lines[i]); err != nil {
			t.Fatalf("Apply line %d: %v", i, err)
		}
	}
	if r.Status != "waiting" {
		t.Fatalf("after task_complete: Status = %q, want waiting", r.Status)
	}
}

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
		t.Fatalf("full-turn: ContextUsed = %d, want 5380 (last_token_usage.total_tokens; this single-turn fixture has last == total)", full.ContextUsed)
	}

	if full.Usage.Input != 5200 || full.Usage.CachedInput != 3100 {
		t.Fatalf("full-turn Usage = %+v, want Input=5200 CachedInput=3100", full.Usage)
	}
	if billable := full.Usage.Input - full.Usage.CachedInput; billable != 2100 {
		t.Fatalf("billable_input = %d, want 2100", billable)
	}

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

func TestContextUsedIsLastTurnNotCumulative(t *testing.T) {
	r, err := LoadRollout(codexFixture(t, "multi-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if r.ContextMax != 258400 {
		t.Fatalf("ContextMax = %d, want 258400", r.ContextMax)
	}
	if r.ContextUsed != 62844 {
		t.Fatalf("ContextUsed = %d, want 62844 (last_token_usage.total_tokens, not the cumulative 261188)", r.ContextUsed)
	}
	if r.ContextUsed >= r.ContextMax {
		t.Fatalf("ContextUsed = %d >= ContextMax = %d: gauge would read over 100%%", r.ContextUsed, r.ContextMax)
	}
	if r.Usage.Input != 253000 {
		t.Fatalf("Usage.Input = %d, want 253000 (cumulative total_token_usage.input_tokens)", r.Usage.Input)
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
