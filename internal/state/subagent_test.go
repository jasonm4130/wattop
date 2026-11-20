package state

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func TestParentBlockedOnSubagentStillBurns(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	spawn := at(0).Add(-5 * time.Minute)

	mk := func(childIn int64, now time.Time) domain.Session {
		return domain.Session{
			Agent: "claude", ID: "s1", Status: "busy",
			Model: "claude-opus-5", Usage: domain.Usage{Input: 1000, Output: 500},
			Tools:       []domain.ToolCall{{Name: "Agent", ID: "t1", At: spawn}},
			LastUsageAt: now,
			Subagents: []domain.Subagent{{
				ID: "a1", Model: "claude-opus-5", Status: domain.SubagentRunning, Live: true,
				StartedAt: spawn, LastActivityAt: now,
				Usage: domain.Usage{Input: childIn, Output: childIn / 10},
			}},
		}
	}

	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{mk(100_000, at(0))}})
	snap := st.Reduce(Inputs{At: at(10), Sessions: []domain.Session{mk(200_000, at(10))}})
	s := findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr <= 0 {
		t.Fatalf("parent blocked on a spending subagent: BurnUSDPerHr = %v, want > 0", s.BurnUSDPerHr)
	}
	sa := s.Subagents[0]
	if sa.BurnUSDPerHr == nil || *sa.BurnUSDPerHr <= 0 {
		t.Fatalf("spending subagent: BurnUSDPerHr = %v, want > 0", sa.BurnUSDPerHr)
	}
	if snap.TotalBurnUSDPerHr != *s.BurnUSDPerHr {
		t.Fatalf("TotalBurnUSDPerHr = %v, want only the session's %v — child burn is display-only", snap.TotalBurnUSDPerHr, *s.BurnUSDPerHr)
	}
}

func TestNewChildIsSeededNotBaselined(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	sess := domain.Session{
		Agent: "claude", ID: "s1", Status: "busy", Model: "claude-opus-5",
		Subagents: []domain.Subagent{{
			ID: "a1", Model: "claude-opus-5", Status: domain.SubagentRunning,
			StartedAt: at(0), LastActivityAt: at(20),
			Usage: domain.Usage{Input: 50_000, Output: 5_000},
		}},
	}
	snap := st.Reduce(Inputs{At: at(20), Sessions: []domain.Session{sess}})
	sa := findSession(t, snap, "claude", "s1").Subagents[0]
	if sa.BurnUSDPerHr == nil || *sa.BurnUSDPerHr <= 0 {
		t.Fatalf("new child: BurnUSDPerHr = %v, want > 0", sa.BurnUSDPerHr)
	}

	old := sess
	old.ID = "s2"
	old.Subagents = []domain.Subagent{sess.Subagents[0]}
	old.Subagents[0].StartedAt = at(0).Add(-time.Hour)
	old.Subagents[0].LastActivityAt = at(0).Add(-50 * time.Minute)
	snap = st.Reduce(Inputs{At: at(21), Sessions: []domain.Session{old}})
	sa = findSession(t, snap, "claude", "s2").Subagents[0]
	if sa.BurnUSDPerHr == nil || *sa.BurnUSDPerHr != 0 {
		t.Fatalf("old child first sighting: BurnUSDPerHr = %v, want 0", sa.BurnUSDPerHr)
	}
}

func TestUnpricedChildMakesTotalsPartial(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	sess := domain.Session{
		Agent: "claude", ID: "s1", Status: "busy",
		Model: "claude-opus-5", Usage: domain.Usage{Input: 1000, Output: 500},
		Subagents: []domain.Subagent{
			{ID: "a1", WorkflowID: "wf_1", Model: "claude-opus-5", Usage: domain.Usage{Input: 2000, Output: 100}},
			{ID: "a2", WorkflowID: "wf_1", Model: "wattop-nonexistent-model", Usage: domain.Usage{Input: 2000, Output: 100}},
			{ID: "a3", WorkflowID: "wf_2", Model: "claude-opus-5", Usage: domain.Usage{Input: 3000, Output: 100}},
			{ID: "a4", WorkflowID: "wf_2"},
		},
		Workflows: []domain.Workflow{{ID: "wf_1", Agents: 2}, {ID: "wf_2", Agents: 2}},
	}
	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{sess}})
	s := findSession(t, snap, "claude", "s1")
	if !s.CostPartial || s.CostUSD == nil {
		t.Fatalf("session CostPartial=%v CostUSD=%v, want partial with a value", s.CostPartial, s.CostUSD)
	}
	if !snap.TotalCostPartial {
		t.Fatalf("TotalCostPartial = false, want true")
	}
	wf1, wf2 := s.Workflows[0], s.Workflows[1]
	if !wf1.CostPartial || wf1.CostUSD == nil || *wf1.CostUSD != *s.Subagents[0].CostUSD {
		t.Fatalf("wf_1 = %+v, want partial with a1's cost", wf1)
	}
	if wf2.CostPartial || wf2.CostUSD == nil || *wf2.CostUSD != *s.Subagents[2].CostUSD {
		t.Fatalf("wf_2 = %+v, want exact with a3's cost", wf2)
	}
	if sess.Subagents[0].CostUSD != nil || sess.Workflows[0].CostUSD != nil {
		t.Fatalf("Reduce mutated the source session's slices")
	}
}

func TestChildTiersOnItsOwnContext(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	mk := func(id string, ctx int64) domain.Session {
		return domain.Session{
			Agent: "claude", ID: id, Model: "claude-sonnet-4-5",
			Subagents: []domain.Subagent{{
				ID: "a1", Model: "claude-sonnet-4-5", ContextUsed: ctx,
				Usage: domain.Usage{Input: 250_000, Output: 10_000},
			}},
		}
	}
	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{mk("small", 50_000), mk("large", 250_000)}})
	small := *findSession(t, snap, "claude", "small").Subagents[0].CostUSD
	large := *findSession(t, snap, "claude", "large").Subagents[0].CostUSD
	if large <= small {
		t.Fatalf("child at 250k context cost %v, want more than %v at 50k", large, small)
	}
}

func TestDroppedSessionForgetsChildBurn(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	sess := domain.Session{
		Agent: "claude", ID: "s1", Model: "claude-opus-5",
		Subagents: []domain.Subagent{{ID: "a1", Model: "claude-opus-5", Usage: domain.Usage{Input: 10}}},
	}
	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{sess}})
	st.Reduce(Inputs{At: at(40)})
	for key := range st.burnLastCost {
		if strings.HasPrefix(key, "claude:s1") {
			t.Fatalf("burn key %q survived the session's drop", key)
		}
	}
}

func TestSiblingsWithoutIDsBurnSeparately(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	sess := domain.Session{
		Agent: "claude", ID: "s1", Model: "claude-opus-5",
		Subagents: []domain.Subagent{
			{Hash: "agent-1", Model: "claude-opus-5", LastActivityAt: at(0).Add(-time.Hour), Usage: domain.Usage{Input: 100_000}},
			{Hash: "agent-2", Model: "claude-opus-5", LastActivityAt: at(0).Add(-time.Hour), Usage: domain.Usage{Input: 1_000}},
		},
	}
	for sec := 0; sec < 5; sec++ {
		snap := st.Reduce(Inputs{At: at(sec), Sessions: []domain.Session{sess}})
		for _, sa := range findSession(t, snap, "claude", "s1").Subagents {
			if sa.BurnUSDPerHr != nil && *sa.BurnUSDPerHr != 0 {
				t.Fatalf("cycle %d: unchanged child %s burns %v, want none", sec, sa.Hash, *sa.BurnUSDPerHr)
			}
		}
	}
}

func TestNewbornChildBurnIsBounded(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	sess := domain.Session{
		Agent: "claude", ID: "s1", Model: "claude-opus-5",
		Subagents: []domain.Subagent{{
			ID: "a1", Model: "claude-opus-5", StartedAt: at(0), LastActivityAt: at(1),
			Usage: domain.Usage{Input: 10_000},
		}},
	}
	snap := st.Reduce(Inputs{At: at(1), Sessions: []domain.Session{sess}})
	sa := findSession(t, snap, "claude", "s1").Subagents[0]
	maxRate := *sa.CostUSD / childSeedMinSpan.Hours()
	if sa.BurnUSDPerHr == nil || *sa.BurnUSDPerHr <= 0 || *sa.BurnUSDPerHr > maxRate+1e-9 {
		t.Fatalf("newborn burn = %v, want in (0, %v]", sa.BurnUSDPerHr, maxRate)
	}
}
