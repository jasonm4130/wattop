package codex

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func turnContextLine(at time.Time, model string) string {
	b, _ := json.Marshal(map[string]any{
		"timestamp": at.Format(time.RFC3339Nano),
		"type":      "turn_context",
		"payload":   map[string]any{"model": model, "cwd": "/home/u/proj"},
	})
	return string(b)
}

// cumulativeTokenCount is a token_count whose total is the running sum and
// whose last_token_usage is this request alone.
func cumulativeTokenCount(at time.Time, total, last domain.Usage) string {
	usage := func(u domain.Usage) map[string]any {
		return map[string]any{
			"input_tokens": u.Input, "cached_input_tokens": u.CachedInput,
			"output_tokens": u.Output, "reasoning_output_tokens": u.Thinking,
			"total_tokens": u.Input + u.Output,
		}
	}
	b, _ := json.Marshal(map[string]any{
		"timestamp": at.Format(time.RFC3339Nano),
		"type":      "event_msg",
		"payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"total_token_usage":    usage(total),
				"last_token_usage":     usage(last),
				"model_context_window": 400000,
			},
		},
	})
	return string(b)
}

// TestRolloutLedgerFilesEachTurnAtItsOwnPromptAndModel: each token_count's
// delta over the running total is filed under the model and prompt size
// (last_token_usage.input_tokens) of the request that produced it — never
// the cumulative total under the latest ones — and a replayed or repeated
// token_count files nothing.
func TestRolloutLedgerFilesEachTurnAtItsOwnPromptAndModel(t *testing.T) {
	t0 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	turn1 := domain.Usage{Input: 100_000, CachedInput: 50_000, Output: 1_000}
	turn2 := domain.Usage{Input: 300_000, CachedInput: 250_000, Output: 2_000}
	turn3 := domain.Usage{Input: 20_000, Output: 500}

	r := &Rollout{}
	for _, line := range []string{
		turnContextLine(t0, "gpt-5.6-terra"),
		cumulativeTokenCount(t0.Add(time.Second), turn1, turn1),
		cumulativeTokenCount(t0.Add(2*time.Second), turn1, turn1), // repeated count: no new usage
		cumulativeTokenCount(t0.Add(3*time.Second), turn1.Plus(turn2), turn2),
		cumulativeTokenCount(t0, turn1, turn1), // replayed prefix: older than the last usage
		turnContextLine(t0.Add(4*time.Second), "gpt-5.6-sol"),
		cumulativeTokenCount(t0.Add(5*time.Second), turn1.Plus(turn2).Plus(turn3), turn3),
	} {
		if err := r.Apply([]byte(line)); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}

	want := domain.UsageLedger{
		{Model: "gpt-5.6-terra", PromptK: 100}: turn1,
		{Model: "gpt-5.6-terra", PromptK: 300}: turn2,
		{Model: "gpt-5.6-sol", PromptK: 20}:    turn3,
	}
	if !reflect.DeepEqual(r.Ledger, want) {
		t.Fatalf("Ledger = %+v\nwant     %+v", r.Ledger, want)
	}
	var sum domain.Usage
	for _, u := range r.Ledger {
		sum = sum.Plus(u)
	}
	if sum != r.Usage {
		t.Fatalf("ledger sums to %+v, cumulative Usage is %+v", sum, r.Usage)
	}
}

// TestRolloutLedgerRestartsWithTheCounter: when the cumulative counter goes
// backwards (a reset), Usage becomes the new total and the ledger restarts
// from it, so the two never diverge.
func TestRolloutLedgerRestartsWithTheCounter(t *testing.T) {
	t0 := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	big := domain.Usage{Input: 500_000, Output: 5_000}
	small := domain.Usage{Input: 10_000, Output: 100}

	r := &Rollout{}
	for _, line := range []string{
		turnContextLine(t0, "gpt-5.6-terra"),
		cumulativeTokenCount(t0.Add(time.Second), big, big),
		cumulativeTokenCount(t0.Add(2*time.Second), small, small),
	} {
		if err := r.Apply([]byte(line)); err != nil {
			t.Fatalf("Apply: %v", err)
		}
	}
	want := domain.UsageLedger{{Model: "gpt-5.6-terra", PromptK: 10}: small}
	if !reflect.DeepEqual(r.Ledger, want) || r.Usage != small {
		t.Fatalf("after reset: Ledger %+v Usage %+v, want %+v / %+v", r.Ledger, r.Usage, want, small)
	}
}

// TestPollCarriesLedgersForRootAndChild: the ledger reaches both the root
// session and its folded child, as an independent copy.
func TestPollCarriesLedgersForRootAndChild(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	rootTurn := domain.Usage{Input: 1_000, Output: 10}
	childTurn := domain.Usage{Input: 2_000, Output: 20}

	writeRolloutBody(t, root, now, "2026-09-25T11-00-00-root", []string{
		metaLine(metaOpts{threadID: "thread-root", sessionID: "sess", at: now.Add(-10 * time.Minute)}),
		turnContextLine(now.Add(-9*time.Minute), "gpt-5.6-terra"),
		cumulativeTokenCount(now.Add(-9*time.Minute), rootTurn, rootTurn),
	}, now.Add(-time.Minute))
	writeRolloutBody(t, root, now, "2026-09-25T11-01-00-child", []string{
		metaLine(metaOpts{
			threadID: "thread-child", sessionID: "sess", parentThreadID: "thread-root",
			source: map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
				"parent_thread_id": "thread-root", "depth": 1,
			}}},
			at: now.Add(-8 * time.Minute),
		}),
		turnContextLine(now.Add(-7*time.Minute), "gpt-5.6-sol"),
		cumulativeTokenCount(now.Add(-7*time.Minute), childTurn, childTurn),
	}, now.Add(-time.Minute))

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || len(sessions[0].Subagents) != 1 {
		t.Fatalf("got %d sessions, want 1 with 1 child", len(sessions))
	}
	s := sessions[0]
	if want := (domain.UsageLedger{{Model: "gpt-5.6-terra", PromptK: 1}: rootTurn}); !reflect.DeepEqual(s.Ledger, want) {
		t.Fatalf("root Ledger = %+v, want %+v", s.Ledger, want)
	}
	if want := (domain.UsageLedger{{Model: "gpt-5.6-sol", PromptK: 2}: childTurn}); !reflect.DeepEqual(s.Subagents[0].Ledger, want) {
		t.Fatalf("child Ledger = %+v, want %+v", s.Subagents[0].Ledger, want)
	}
}
