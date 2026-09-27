package state

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/agent/claude"
	"github.com/jasonm4130/wattop/internal/agent/codex"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
)

// claudeFixture plants one live Claude session (sessions/<pid>.json) whose
// transcript is lines, and returns a Source reading it plus its subagents
// directory.
func claudeFixture(t *testing.T, lines ...string) (*claude.Source, string) {
	t.Helper()
	root := t.TempDir()
	sessionsDir := filepath.Join(root, "sessions")
	projectDir := filepath.Join(root, "projects", "-repo-x")
	subagentsDir := filepath.Join(projectDir, "sess-1", "subagents")
	for _, d := range []string{sessionsDir, subagentsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	mustWrite(t, filepath.Join(sessionsDir, "1234.json"),
		`{"pid":1234,"sessionId":"sess-1","cwd":"/repo/x","status":"busy","kind":"interactive",`+
			`"entrypoint":"cli","name":"work","updatedAt":1757116800000,"statusUpdatedAt":1757116800000}`)
	mustWrite(t, filepath.Join(projectDir, "sess-1.jsonl"), strings.Join(lines, ""))
	return claude.NewSource(sessionsDir, filepath.Join(root, "projects"), nil), subagentsDir
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// usageLine is one assistant record for model with its own message id and
// the given prompt (all fresh input) and output sizes.
func usageLine(id, model string, input, output int64) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"id":%q,"model":%q,`+
		`"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":%d,"output_tokens":%d}}}`+"\n",
		id, model, input, output)
}

// syntheticLine is the shape Claude Code writes for a locally generated
// assistant message (an API error, a login prompt): model "<synthetic>" and
// all-zero usage. Copied from a real transcript with ids and text scrubbed.
const syntheticLine = `{"parentUuid":"00000000-0000-0000-0000-000000000001","isSidechain":false,"type":"assistant",` +
	`"uuid":"00000000-0000-0000-0000-000000000002","timestamp":"2026-09-06T01:00:05.000Z","message":{"diagnostics":null,` +
	`"id":"00000000-0000-0000-0000-000000000003","container":null,"model":"<synthetic>","role":"assistant",` +
	`"stop_details":null,"stop_reason":"stop_sequence","stop_sequence":"","type":"message","usage":{"output_tokens_details":null,` +
	`"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,` +
	`"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":null,` +
	`"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0},"inference_geo":null,` +
	`"iterations":null,"speed":null},"content":[{"type":"text","text":"Login expired"}],"context_management":null},` +
	`"error":"authentication_failed","isApiErrorMessage":true,"userType":"external","entrypoint":"cli",` +
	`"cwd":"/repo/x","sessionId":"sess-1","version":"2.1.278"}` + "\n"

func reduceClaude(t *testing.T, src *claude.Source) (*domain.Snapshot, domain.Session) {
	t.Helper()
	now := time.Date(2026, 9, 6, 1, 1, 0, 0, time.UTC)
	sessions, err := src.Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	st := newTestState(t, time.Minute)
	snap := st.Reduce(Inputs{At: now, Sessions: sessions})
	return snap, findSession(t, snap, "claude", "sess-1")
}

func mustCost(t *testing.T, b *pricing.Book, model string, u domain.Usage, prompt int64) float64 {
	t.Helper()
	c, ok := b.Cost(model, u, prompt)
	if !ok {
		t.Fatalf("model %q does not price in the embedded table", model)
	}
	return c
}

func embeddedBook(t *testing.T) *pricing.Book {
	t.Helper()
	b, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}
	return b
}

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-9 }

// TestCostPricesEachRequestAtItsOwnTier is the lifetime-repricing bug: one
// request past claude-sonnet-4-5's 200k threshold must not reprice every
// earlier short request at the long-context rate. The total is the sum of
// each request priced at its own prompt size.
func TestCostPricesEachRequestAtItsOwnTier(t *testing.T) {
	const model = "claude-sonnet-4-5"
	src, _ := claudeFixture(t,
		usageLine("m1", model, 100_000, 1_000),
		usageLine("m2", model, 150_000, 1_000),
		usageLine("m3", model, 250_000, 1_000),
	)
	_, s := reduceClaude(t, src)

	b := embeddedBook(t)
	want := mustCost(t, b, model, domain.Usage{Input: 100_000, Output: 1_000}, 100_000) +
		mustCost(t, b, model, domain.Usage{Input: 150_000, Output: 1_000}, 150_000) +
		mustCost(t, b, model, domain.Usage{Input: 250_000, Output: 1_000}, 250_000)
	lifetimeLong := mustCost(t, b, model, domain.Usage{Input: 500_000, Output: 3_000}, 250_000)
	if near(want, lifetimeLong) {
		t.Fatalf("test does not bite: per-request %.6f equals lifetime-at-long-rate %.6f", want, lifetimeLong)
	}
	if s.CostUSD == nil {
		t.Fatalf("CostUSD = nil, want priced")
	}
	if !near(*s.CostUSD, want) {
		t.Fatalf("CostUSD = %.6f, want %.6f (sum of per-request costs); lifetime at the long rate is %.6f",
			*s.CostUSD, want, lifetimeLong)
	}
}

// TestCostPricesEachRequestAtItsOwnModel: a /model switch mid-session must
// not reprice history at the new model's rates.
func TestCostPricesEachRequestAtItsOwnModel(t *testing.T) {
	src, _ := claudeFixture(t,
		usageLine("m1", "claude-opus-5", 10_000, 2_000),
		usageLine("m2", "claude-sonnet-4-5", 20_000, 3_000),
	)
	_, s := reduceClaude(t, src)

	b := embeddedBook(t)
	want := mustCost(t, b, "claude-opus-5", domain.Usage{Input: 10_000, Output: 2_000}, 10_000) +
		mustCost(t, b, "claude-sonnet-4-5", domain.Usage{Input: 20_000, Output: 3_000}, 20_000)
	if s.Model != "claude-sonnet-4-5" {
		t.Fatalf("Model = %q, want the latest model shown", s.Model)
	}
	if s.CostUSD == nil {
		t.Fatalf("CostUSD = nil, want %.6f", want)
	}
	if !near(*s.CostUSD, want) {
		t.Fatalf("CostUSD = %.6f, want %.6f (each request at its own model)", *s.CostUSD, want)
	}
}

// TestCodexCostPricesEachTurnAtItsOwnTier: a Codex session whose second
// turn crosses gpt-5.6-terra's 272k threshold prices its first turn at the
// base rate, not the whole cumulative total at the long-context rate.
func TestCodexCostPricesEachTurnAtItsOwnTier(t *testing.T) {
	const model = "gpt-5.6-terra"
	r := &codex.Rollout{}
	for _, line := range []string{
		`{"timestamp":"2026-09-06T01:00:00Z","type":"turn_context","payload":{"model":"` + model + `"}}`,
		`{"timestamp":"2026-09-06T01:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{` +
			`"total_token_usage":{"input_tokens":200000,"cached_input_tokens":150000,"output_tokens":2000,"total_tokens":202000},` +
			`"last_token_usage":{"input_tokens":200000,"cached_input_tokens":150000,"output_tokens":2000,"total_tokens":202000}}}}`,
		`{"timestamp":"2026-09-06T01:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{` +
			`"total_token_usage":{"input_tokens":500000,"cached_input_tokens":400000,"output_tokens":5000,"total_tokens":505000},` +
			`"last_token_usage":{"input_tokens":300000,"cached_input_tokens":250000,"output_tokens":3000,"total_tokens":303000}}}}`,
	} {
		if err := r.Apply([]byte(line)); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	st := newTestState(t, time.Minute)
	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{{
		Agent: "codex", ID: "c1", Model: r.Model, Usage: r.Usage, ContextUsed: r.ContextUsed, Ledger: r.Ledger,
	}}})
	s := findSession(t, snap, "codex", "c1")

	b := embeddedBook(t)
	want := mustCost(t, b, model, domain.Usage{Input: 200_000, CachedInput: 150_000, Output: 2_000}, 200_000) +
		mustCost(t, b, model, domain.Usage{Input: 300_000, CachedInput: 250_000, Output: 3_000}, 300_000)
	lifetimeLong := mustCost(t, b, model, r.Usage, r.ContextUsed)
	if near(want, lifetimeLong) {
		t.Fatalf("test does not bite")
	}
	if s.CostUSD == nil || !near(*s.CostUSD, want) {
		t.Fatalf("CostUSD = %v, want %.6f (lifetime at the long rate would be %.6f)", s.CostUSD, want, lifetimeLong)
	}
}

// TestLedgerWithAnUnpricedModelIsPartial: a session whose ledger mixes a
// priced model with one the book cannot price keeps the priced part, names
// the other, and is marked partial; one whose ledger prices nothing renders
// "$—" as before.
func TestLedgerWithAnUnpricedModelIsPartial(t *testing.T) {
	b := embeddedBook(t)
	st := newTestState(t, time.Minute)

	mixed := domain.UsageLedger{}
	mixed.Add("claude-opus-5", 10_000, domain.Usage{Input: 10_000, Output: 100})
	mixed.Add("mystery-model-x", 10_000, domain.Usage{Input: 10_000, Output: 100})
	none := domain.UsageLedger{}
	none.Add("mystery-model-y", 10_000, domain.Usage{Input: 10_000})

	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{
		{Agent: "claude", ID: "mixed", Model: "mystery-model-x", Usage: domain.Usage{Input: 20_000, Output: 200}, Ledger: mixed},
		{Agent: "claude", ID: "none", Model: "mystery-model-y", Usage: domain.Usage{Input: 10_000}, Ledger: none},
	}})

	m := findSession(t, snap, "claude", "mixed")
	want := mustCost(t, b, "claude-opus-5", domain.Usage{Input: 10_000, Output: 100}, 10_000)
	if !m.Priced || m.CostUSD == nil || !near(*m.CostUSD, want) || !m.CostPartial {
		t.Fatalf("mixed: Priced=%v CostUSD=%v CostPartial=%v, want priced %.6f and partial", m.Priced, m.CostUSD, m.CostPartial, want)
	}
	n := findSession(t, snap, "claude", "none")
	if n.Priced || n.CostUSD != nil {
		t.Fatalf("none: Priced=%v CostUSD=%v, want unpriced", n.Priced, n.CostUSD)
	}
	if got := strings.Join(snap.UnpricedModels, ","); got != "mystery-model-x,mystery-model-y" {
		t.Fatalf("UnpricedModels = %v", snap.UnpricedModels)
	}
}

// TestSyntheticRecordDoesNotUnpriceSession: Claude writes locally generated
// messages (API errors, login prompts) as model "<synthetic>" with all-zero
// usage. One arriving last must not become the session's model, zero its
// context size, or push "<synthetic>" into UnpricedModels.
func TestSyntheticRecordDoesNotUnpriceSession(t *testing.T) {
	src, _ := claudeFixture(t,
		usageLine("m1", "claude-opus-5", 50_000, 1_000),
		syntheticLine,
	)
	snap, s := reduceClaude(t, src)

	if s.Model != "claude-opus-5" {
		t.Fatalf("Model = %q, want the last real model", s.Model)
	}
	if s.ContextUsed != 50_000 {
		t.Fatalf("ContextUsed = %d, want 50000 (the synthetic record's zero prompt ignored)", s.ContextUsed)
	}
	if !s.Priced || s.CostUSD == nil {
		t.Fatalf("session unpriced (Priced=%v, CostUSD=%v), want priced", s.Priced, s.CostUSD)
	}
	if len(snap.UnpricedModels) != 0 {
		t.Fatalf("UnpricedModels = %v, want none", snap.UnpricedModels)
	}
}

// TestSyntheticRecordDoesNotUnpriceSubagent is the same for a child
// transcript.
func TestSyntheticRecordDoesNotUnpriceSubagent(t *testing.T) {
	src, subDir := claudeFixture(t, usageLine("p1", "claude-opus-5", 1_000, 10))
	mustWrite(t, filepath.Join(subDir, "agent-a1.meta.json"), `{"agentType":"general-purpose","description":"child"}`)
	mustWrite(t, filepath.Join(subDir, "agent-a1.jsonl"),
		usageLine("c1", "claude-opus-5", 30_000, 500)+syntheticLine)
	snap, s := reduceClaude(t, src)

	if len(s.Subagents) != 1 {
		t.Fatalf("got %d subagents, want 1", len(s.Subagents))
	}
	sa := s.Subagents[0]
	if sa.Model != "claude-opus-5" || sa.ContextUsed != 30_000 {
		t.Fatalf("child Model=%q ContextUsed=%d, want claude-opus-5 / 30000", sa.Model, sa.ContextUsed)
	}
	if sa.CostUSD == nil || s.CostPartial {
		t.Fatalf("child CostUSD=%v session CostPartial=%v, want priced and exact", sa.CostUSD, s.CostPartial)
	}
	if len(snap.UnpricedModels) != 0 {
		t.Fatalf("UnpricedModels = %v, want none", snap.UnpricedModels)
	}
}

// TestSubagentCostPricesEachRequestAtItsOwnTier is the tier bug for a child.
func TestSubagentCostPricesEachRequestAtItsOwnTier(t *testing.T) {
	const model = "claude-sonnet-4-5"
	src, subDir := claudeFixture(t, usageLine("p1", model, 1_000, 10))
	mustWrite(t, filepath.Join(subDir, "agent-a1.meta.json"), `{"agentType":"general-purpose","description":"child"}`)
	mustWrite(t, filepath.Join(subDir, "agent-a1.jsonl"),
		usageLine("c1", model, 120_000, 1_000)+usageLine("c2", model, 210_000, 1_000))
	_, s := reduceClaude(t, src)

	b := embeddedBook(t)
	want := mustCost(t, b, model, domain.Usage{Input: 120_000, Output: 1_000}, 120_000) +
		mustCost(t, b, model, domain.Usage{Input: 210_000, Output: 1_000}, 210_000)
	if len(s.Subagents) != 1 || s.Subagents[0].CostUSD == nil {
		t.Fatalf("subagents = %+v, want one priced child", s.Subagents)
	}
	if got := *s.Subagents[0].CostUSD; !near(got, want) {
		t.Fatalf("child CostUSD = %.6f, want %.6f (sum of per-request costs)", got, want)
	}
}
