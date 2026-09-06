package state

import (
	"math"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func TestTokenRatesIncludeChildrenOnceAndDisappearOnOutage(t *testing.T) {
	st := newTestState(t, time.Minute)
	in := Inputs{At: at(0), Sessions: []domain.Session{
		{Agent: "claude", ID: "a", TokenRate: &domain.TokenRate{OutputPerSec: 10}, Subagents: []domain.Subagent{{Hash: "child", TokenRate: &domain.TokenRate{OutputPerSec: 20}}}},
		{Agent: "codex", ID: "b", TokenRate: &domain.TokenRate{OutputPerSec: 30}},
	}}
	snap := st.Reduce(in)
	if snap.TokenRate == nil || snap.TokenRate.OutputPerSec != 60 {
		t.Fatalf("total rate %+v", snap.TokenRate)
	}
	if snap.Sessions[0].TokenRate.OutputPerSec != 10 {
		t.Fatal("parent's own rate changed")
	}
	if got := st.History("tokens_out"); len(got) != 1 || got[0] != 60 {
		t.Fatalf("history %v", got)
	}
	snap = st.Reduce(Inputs{At: at(1), Health: []SourceHealth{{Name: "claude", OK: false}, {Name: "codex", OK: false}}})
	if snap.TokenRate != nil {
		t.Fatalf("stale rates survived outage: %+v", snap.TokenRate)
	}
	if got := st.History("tokens_out"); !math.IsNaN(got[len(got)-1]) {
		t.Fatalf("missing rate plotted as a measurement: %v", got)
	}
}
