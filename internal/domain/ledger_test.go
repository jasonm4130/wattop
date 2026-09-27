package domain

import "testing"

// TestPromptKPreservesTierThresholds: a tier *_above_<N>k_tokens applies
// when the prompt exceeds N*1000, and PromptK*1000 must select the same
// tier the exact prompt size does.
func TestPromptKPreservesTierThresholds(t *testing.T) {
	for _, tc := range []struct{ tokens, want int64 }{
		{0, 0}, {-5, 0}, {1, 1}, {999, 1}, {1000, 1}, {1001, 2},
		{200_000, 200}, {200_001, 201}, {272_000, 272}, {272_001, 273},
	} {
		got := PromptK(tc.tokens)
		if got != tc.want {
			t.Errorf("PromptK(%d) = %d, want %d", tc.tokens, got, tc.want)
		}
		for _, n := range []int64{200, 272} {
			if exact, bucketed := tc.tokens > n*1000, got*1000 > n*1000; exact != bucketed {
				t.Errorf("PromptK(%d) moves the %dk tier: exact above=%v, bucketed above=%v", tc.tokens, n, exact, bucketed)
			}
		}
	}
}

func TestUsageLedgerAddMergeClone(t *testing.T) {
	l := UsageLedger{}
	l.Add("m", 1_500, Usage{Input: 1_500, Output: 10})
	l.Add("m", 2_000, Usage{Input: 2_000, Output: 5})
	l.Add("n", 2_000, Usage{Input: 7})
	if got := l[UsageKey{Model: "m", PromptK: 2}]; got != (Usage{Input: 3_500, Output: 15}) {
		t.Fatalf("m@2k = %+v", got)
	}

	c := l.Clone()
	c.Merge(UsageLedger{{Model: "n", PromptK: 2}: {Input: 3}})
	if c[UsageKey{Model: "n", PromptK: 2}].Input != 10 || l[UsageKey{Model: "n", PromptK: 2}].Input != 7 {
		t.Fatalf("Clone is not independent: clone %+v, original %+v", c, l)
	}
	if UsageLedger(nil).Clone() != nil {
		t.Fatalf("Clone of nil ledger is not nil")
	}
}
