package codex

import (
	"fmt"
	"testing"
	"time"
)

func TestTokenRateUsesDeltasAndEventTime(t *testing.T) {
	var r Rollout
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	apply := func(at time.Time, total, last int) {
		t.Helper()
		line := fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"output_tokens":%d},"last_token_usage":{"input_tokens":%d,"output_tokens":%d}}}}`, at.Format(time.RFC3339), total*10, total, last*10, last)
		if err := r.Apply([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	apply(now.Add(-time.Hour), 900000, 600)
	apply(now.Add(-time.Second), 900600, 600)
	apply(now, 900600, 600)
	apply(now.Add(-time.Hour), 900000, 600) // replayed prefix after rotation
	apply(now, 900600, 600)
	if rate := r.tokens.Rate(now); rate.OutputPerSec != 10 {
		t.Fatalf("backfill or duplicate inflated rate: %+v", rate)
	}
	apply(now, 60, 60)
	if rate := r.tokens.Rate(now); rate.OutputPerSec != 11 {
		t.Fatalf("reset produced wrong rate: %+v", rate)
	}
	if rate := r.tokens.Rate(now.Add(time.Minute)); rate.OutputPerSec != 0 {
		t.Fatalf("rate did not expire: %+v", rate)
	}
}
