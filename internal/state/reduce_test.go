package state

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
)

func newTestState(t *testing.T, window time.Duration) *State {
	t.Helper()
	book, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}
	return New(book, pricing.NewBurnTracker(window, 0.3))
}

func at(sec int) time.Time {
	return time.Date(2026, 9, 6, 12, 0, sec, 0, time.UTC)
}

func findSession(t *testing.T, snap *domain.Snapshot, agent, id string) domain.Session {
	t.Helper()
	for _, s := range snap.Sessions {
		if s.Agent == agent && s.ID == id {
			return s
		}
	}
	t.Fatalf("session %s/%s not found in snapshot (have %d sessions)", agent, id, len(snap.Sessions))
	return domain.Session{}
}

func hasSession(snap *domain.Snapshot, agent, id string) bool {
	for _, s := range snap.Sessions {
		if s.Agent == agent && s.ID == id {
			return true
		}
	}
	return false
}

func TestBurnDecaysToZeroWhenIdle(t *testing.T) {
	st := newTestState(t, 5*time.Second)

	mk := func(usage int64) domain.Session {
		return domain.Session{
			Agent:  "claude",
			ID:     "s1",
			Status: "busy",
			Model:  "claude-opus-5",
			Usage:  domain.Usage{Input: usage, Output: usage / 2},
		}
	}

	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{mk(1000)}})
	s := findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("first cycle: BurnUSDPerHr = %v, want 0 (no prior observation)", s.BurnUSDPerHr)
	}

	snap = st.Reduce(Inputs{At: at(1), Sessions: []domain.Session{mk(2000)}})
	s = findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr <= 0 {
		t.Fatalf("after cost increase: BurnUSDPerHr = %v, want > 0", s.BurnUSDPerHr)
	}

	snap = st.Reduce(Inputs{At: at(2), Sessions: []domain.Session{mk(2000)}})
	s = findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr <= 0 {
		t.Fatalf("still within window: BurnUSDPerHr = %v, want > 0", s.BurnUSDPerHr)
	}

	snap = st.Reduce(Inputs{At: at(10), Sessions: []domain.Session{mk(2000)}})
	s = findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("past window while idle: BurnUSDPerHr = %v, want exactly 0", s.BurnUSDPerHr)
	}
}

func TestPidDisappearsRowSurvives(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	pid := 4242

	session := domain.Session{
		Agent:  "claude",
		ID:     "s1",
		PID:    &pid,
		Status: "busy",
		Model:  "claude-opus-5",
		Usage:  domain.Usage{Input: 100},
	}
	proc := domain.ProcSample{PID: pid, CPUPct: 12.5}

	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{session}, Procs: []domain.ProcSample{proc}})
	s := findSession(t, snap, "claude", "s1")
	if s.Proc == nil || s.Proc.CPUPct != 12.5 {
		t.Fatalf("Proc = %+v, want bound to pid %d", s.Proc, pid)
	}

	snap = st.Reduce(Inputs{At: at(1), Sessions: []domain.Session{session}})
	s = findSession(t, snap, "claude", "s1")
	if s.Proc != nil {
		t.Fatalf("Proc = %+v, want nil once the pid is gone from the process table", s.Proc)
	}
	if s.Status != "busy" {
		t.Fatalf("Status = %q, want row to survive unchanged", s.Status)
	}
}

func TestUnpricedContributesZeroAndIsFlagged(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	priced := domain.Session{
		Agent: "claude", ID: "priced", Status: "busy",
		Model: "claude-opus-5", Usage: domain.Usage{Input: 1000, Output: 500},
	}
	unpriced := domain.Session{
		Agent: "claude", ID: "unpriced", Status: "busy",
		Model: "wattop-nonexistent-model", Usage: domain.Usage{Input: 1000, Output: 500},
	}

	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{priced, unpriced}})

	up := findSession(t, snap, "claude", "unpriced")
	if up.Priced {
		t.Fatalf("Priced = true, want false for an unresolvable model")
	}
	if up.CostUSD != nil {
		t.Fatalf("CostUSD = %v, want nil for an unpriced session", *up.CostUSD)
	}

	found := false
	for _, m := range snap.UnpricedModels {
		if m == "wattop-nonexistent-model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("UnpricedModels = %v, want it to name wattop-nonexistent-model", snap.UnpricedModels)
	}

	p := findSession(t, snap, "claude", "priced")
	if p.CostUSD == nil {
		t.Fatalf("priced session CostUSD is nil, want a value")
	}
	if snap.TotalCostUSD != *p.CostUSD {
		t.Fatalf("TotalCostUSD = %v, want exactly the priced session's cost (%v) — the unpriced session must contribute 0", snap.TotalCostUSD, *p.CostUSD)
	}
}

func TestCostTierKeysOnLastPromptNotCumulativeUsage(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	s := domain.Session{
		Agent: "claude", ID: "s1", Status: "busy",
		Model: "claude-sonnet-4-5",
		Usage: domain.Usage{
			Input:     120_000,
			Output:    60_000,
			CacheRead: 4_800_000,
		},
		ContextUsed: 130_000,
	}

	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{s}})
	got := findSession(t, snap, "claude", "s1")
	if got.CostUSD == nil {
		t.Fatalf("CostUSD is nil, want a value")
	}

	const (
		inputRate     = 3e-06
		outputRate    = 1.5e-05
		cacheReadRate = 3e-07
	)
	want := float64(s.Usage.Input)*inputRate + float64(s.Usage.Output)*outputRate + float64(s.Usage.CacheRead)*cacheReadRate
	if diff := *got.CostUSD - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("CostUSD = %v, want base-tier price %v (cumulative usage exceeds the 200k tier threshold but the last prompt, ContextUsed=%d, did not)", *got.CostUSD, want, s.ContextUsed)
	}
}

func TestSubagentCostSurvivesAfterItFinishes(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	withSubagent := func(live bool) domain.Session {
		return domain.Session{
			Agent: "claude", ID: "s1", Status: "busy",
			Model: "claude-opus-5", Usage: domain.Usage{Input: 1000, Output: 500},
			Subagents: []domain.Subagent{{
				Hash: "abc", Model: "claude-opus-5", Live: live,
				Usage: domain.Usage{Input: 2000, Output: 1000},
			}},
		}
	}

	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{withSubagent(true)}})
	s := findSession(t, snap, "claude", "s1")
	if len(s.Subagents) != 1 || s.Subagents[0].CostUSD == nil {
		t.Fatalf("subagent not priced: %+v", s.Subagents)
	}
	subCost := *s.Subagents[0].CostUSD
	if s.CostUSD == nil || *s.CostUSD <= subCost {
		t.Fatalf("session CostUSD = %v, want it to include the subagent's %v", s.CostUSD, subCost)
	}
	totalWithLiveSubagent := *s.CostUSD

	snap = st.Reduce(Inputs{At: at(1), Sessions: []domain.Session{withSubagent(false)}})
	s = findSession(t, snap, "claude", "s1")
	if s.Subagents[0].Live {
		t.Fatalf("subagent still Live, want it dropped out of the live set")
	}
	if s.CostUSD == nil || *s.CostUSD != totalWithLiveSubagent {
		t.Fatalf("session CostUSD after subagent finished = %v, want unchanged %v — a finished subagent's cost must stay in the session total", s.CostUSD, totalWithLiveSubagent)
	}
}

func TestHistoryRingCaps(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	for i := 0; i < historyCap+50; i++ {
		st.Reduce(Inputs{
			At: at(i),
			Sys: domain.SysSample{
				Power: domain.Power{SystemWatts: floatp(float64(i))},
			},
		})
	}

	watts := st.History("watts")
	if len(watts) != historyCap {
		t.Fatalf("len(History(\"watts\")) = %d, want %d", len(watts), historyCap)
	}
	if watts[0] != 50 {
		t.Fatalf("watts[0] = %v, want 50 (oldest 50 samples dropped)", watts[0])
	}
	if watts[len(watts)-1] != float64(historyCap+49) {
		t.Fatalf("watts[last] = %v, want %v", watts[len(watts)-1], float64(historyCap+49))
	}
}

func TestVanishedSessionGoesStaleThenDrops(t *testing.T) {
	t.Run("stale then drops at sessionTTL", func(t *testing.T) {
		st := newTestState(t, 60*time.Second)
		busy := domain.Session{Agent: "claude", ID: "s1", Status: "busy", Model: "claude-opus-5"}

		st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{busy}})

		snap := st.Reduce(Inputs{At: at(1)})
		s := findSession(t, snap, "claude", "s1")
		if s.Status != "stale" {
			t.Fatalf("Status = %q, want stale immediately after vanishing", s.Status)
		}
		if !s.StatusSince.Equal(at(1)) {
			t.Fatalf("StatusSince = %v, want %v (the cycle it vanished in)", s.StatusSince, at(1))
		}

		snap = st.Reduce(Inputs{At: at(29)})
		s = findSession(t, snap, "claude", "s1")
		if s.Status != "stale" || !s.StatusSince.Equal(at(1)) {
			t.Fatalf("at +29s: Status=%q StatusSince=%v, want stale/%v still retained", s.Status, s.StatusSince, at(1))
		}

		snap = st.Reduce(Inputs{At: at(31)})
		if hasSession(snap, "claude", "s1") {
			t.Fatalf("session still present at +31s, want it dropped past sessionTTL")
		}
	})

	t.Run("reappears inside TTL with source status", func(t *testing.T) {
		st := newTestState(t, 60*time.Second)
		busy := domain.Session{Agent: "claude", ID: "s1", Status: "busy", Model: "claude-opus-5"}

		st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{busy}})
		st.Reduce(Inputs{At: at(1)})

		waiting := domain.Session{Agent: "claude", ID: "s1", Status: "waiting", Model: "claude-opus-5"}
		snap := st.Reduce(Inputs{At: at(5), Sessions: []domain.Session{waiting}})
		s := findSession(t, snap, "claude", "s1")
		if s.Status != "waiting" {
			t.Fatalf("Status = %q, want the source-supplied status on reappearance, not stale", s.Status)
		}
	})

	t.Run("Codex-supplied stale status is not restamped", func(t *testing.T) {
		st := newTestState(t, 60*time.Second)
		codexStale := domain.Session{
			Agent: "codex", ID: "c1", Status: "stale",
			StatusSince: at(0), Model: "gpt-5.6-terra",
		}

		snap := st.Reduce(Inputs{At: at(10), Sessions: []domain.Session{codexStale}})
		s := findSession(t, snap, "codex", "c1")
		if s.Status != "stale" || !s.StatusSince.Equal(at(0)) {
			t.Fatalf("Status=%q StatusSince=%v, want the source's own stale/StatusSince (%v) preserved verbatim", s.Status, s.StatusSince, at(0))
		}
	})
}

func TestFailedPollPreservesRows(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	claudeBusy := domain.Session{Agent: "claude", ID: "c1", Status: "busy", Model: "claude-opus-5"}
	codexWaiting := domain.Session{Agent: "codex", ID: "x1", Status: "waiting", Model: "gpt-5.6-terra"}

	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{claudeBusy, codexWaiting}})
	if !hasSession(snap, "claude", "c1") || !hasSession(snap, "codex", "x1") {
		t.Fatalf("cycle 1: expected both sessions present")
	}

	times := []int{10, 20, 30, 40}
	for _, sec := range times {
		snap = st.Reduce(Inputs{
			At:       at(sec),
			Sessions: []domain.Session{codexWaiting},
			Health:   []SourceHealth{{Name: "claude", OK: false, Err: "sessions dir unreadable"}},
		})
		s := findSession(t, snap, "claude", "c1")
		if s.Status != "busy" || !s.StatusSince.IsZero() {
			t.Fatalf("at +%ds: claude row = %+v, want busy/zero StatusSince preserved through the outage", sec, s)
		}
		degradedNamesClaude := false
		for _, d := range snap.Degraded {
			if d == "claude: sessions dir unreadable" {
				degradedNamesClaude = true
			}
		}
		if !degradedNamesClaude {
			t.Fatalf("at +%ds: Degraded = %v, want it to name claude", sec, snap.Degraded)
		}
		if !hasSession(snap, "codex", "x1") {
			t.Fatalf("at +%ds: codex row missing despite a healthy poll", sec)
		}
	}

	snap = st.Reduce(Inputs{
		At:       at(50),
		Sessions: []domain.Session{codexWaiting},
		Health:   []SourceHealth{{Name: "claude", OK: true}},
	})
	s := findSession(t, snap, "claude", "c1")
	if s.Status != "stale" {
		t.Fatalf("at +50s (claude healthy, still absent): Status = %q, want stale now that the outage ended", s.Status)
	}
	if !s.StatusSince.Equal(at(50)) {
		t.Fatalf("StatusSince = %v, want %v (the first healthy cycle that omitted it)", s.StatusSince, at(50))
	}

	snap = st.Reduce(Inputs{
		At:       at(75),
		Sessions: []domain.Session{codexWaiting},
		Health:   []SourceHealth{{Name: "claude", OK: true}},
	})
	s = findSession(t, snap, "claude", "c1")
	if s.Status != "stale" || !s.StatusSince.Equal(at(50)) {
		t.Fatalf("at +75s: Status=%q StatusSince=%v, want stale/%v — the countdown runs from the first healthy cycle that omitted the row, not from the outage", s.Status, s.StatusSince, at(50))
	}

	snap = st.Reduce(Inputs{
		At:       at(50 + 31),
		Sessions: []domain.Session{codexWaiting},
		Health:   []SourceHealth{{Name: "claude", OK: true}},
	})
	if hasSession(snap, "claude", "c1") {
		t.Fatalf("claude row still present past sessionTTL after recovery, want dropped")
	}
}

func TestResightingAfterOutageReanchorsTTL(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	busy := domain.Session{Agent: "claude", ID: "c1", Status: "busy", Model: "claude-opus-5"}

	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{busy}})

	st.Reduce(Inputs{
		At:     at(10),
		Health: []SourceHealth{{Name: "claude", OK: false, Err: "sessions dir unreadable"}},
	})

	snap := st.Reduce(Inputs{
		At:       at(20),
		Sessions: []domain.Session{busy},
		Health:   []SourceHealth{{Name: "claude", OK: true}},
	})
	s := findSession(t, snap, "claude", "c1")
	if s.Status != "busy" {
		t.Fatalf("at +20s: Status = %q, want the source-supplied busy on a real sighting", s.Status)
	}

	snap = st.Reduce(Inputs{At: at(21), Health: []SourceHealth{{Name: "claude", OK: true}}})
	s = findSession(t, snap, "claude", "c1")
	if s.Status != "stale" || !s.StatusSince.Equal(at(21)) {
		t.Fatalf("at +21s: Status=%q StatusSince=%v, want stale/%v", s.Status, s.StatusSince, at(21))
	}

	snap = st.Reduce(Inputs{At: at(50), Health: []SourceHealth{{Name: "claude", OK: true}}})
	if !hasSession(snap, "claude", "c1") {
		t.Fatalf("at +50s: row dropped, want it retained until sessionTTL after the +20s sighting")
	}

	snap = st.Reduce(Inputs{At: at(51), Health: []SourceHealth{{Name: "claude", OK: true}}})
	if hasSession(snap, "claude", "c1") {
		t.Fatalf("at +51s: row still present, want it dropped sessionTTL after the +20s sighting — the outage re-anchor must not survive a real sighting")
	}
}

func TestHealthSetsAndClearsDegraded(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	session := domain.Session{Agent: "claude", ID: "s1", Status: "busy", Model: "claude-opus-5"}

	snap := st.Reduce(Inputs{
		At:       at(0),
		Sessions: []domain.Session{session},
		Health:   []SourceHealth{{Name: "soc", OK: false, Err: "ioreport"}},
	})
	if len(snap.Degraded) != 1 || snap.Degraded[0] != "soc: ioreport" {
		t.Fatalf("Degraded = %v, want exactly one entry naming soc", snap.Degraded)
	}
	if !hasSession(snap, "claude", "s1") {
		t.Fatalf("session rows disturbed by an unrelated source's failure")
	}

	snap = st.Reduce(Inputs{
		At:       at(1),
		Sessions: []domain.Session{session},
		Health:   []SourceHealth{{Name: "soc", OK: true}},
	})
	if len(snap.Degraded) != 0 {
		t.Fatalf("Degraded = %v, want empty once the source recovers", snap.Degraded)
	}
}

func TestSnapshotAtIsTheCycleStamp(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	cycleAt := at(5)
	staleSysAt := at(0)

	snap := st.Reduce(Inputs{
		At:  cycleAt,
		Sys: domain.SysSample{At: staleSysAt, SoCName: "M5 Max"},
	})

	if !snap.At.Equal(cycleAt) {
		t.Fatalf("Snapshot.At = %v, want %v", snap.At, cycleAt)
	}
	if !snap.Sys.At.Equal(cycleAt) {
		t.Fatalf("Snapshot.Sys.At = %v, want %v (overwritten from the sampler's own %v)", snap.Sys.At, cycleAt, staleSysAt)
	}
}

func floatp(f float64) *float64 { return &f }

func TestReduceFromJSONFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/cycle1.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var fixture struct {
		At       time.Time        `json:"at"`
		Sys      domain.SysSample `json:"sys"`
		Sessions []domain.Session `json:"sessions"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	st := newTestState(t, 60*time.Second)
	snap := st.Reduce(Inputs{At: fixture.At, Sys: fixture.Sys, Sessions: fixture.Sessions})

	if !snap.At.Equal(fixture.At) || !snap.Sys.At.Equal(fixture.At) {
		t.Fatalf("At/Sys.At = %v/%v, want both %v", snap.At, snap.Sys.At, fixture.At)
	}
	s := findSession(t, snap, "claude", "fixture-session-1")
	if s.CostUSD == nil || *s.CostUSD <= 0 {
		t.Fatalf("CostUSD = %v, want a priced positive value for claude-opus-5", s.CostUSD)
	}
}

func TestBackfilledHistoryDoesNotBurn(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	mk := func(usage int64, toolAt time.Time) domain.Session {
		return domain.Session{
			Agent:  "claude",
			ID:     "s1",
			Status: "busy",
			Model:  "claude-opus-5",
			Usage:  domain.Usage{Input: usage, Output: usage / 2},
			Tools:  []domain.ToolCall{{Name: "Bash", ID: "t1", At: toolAt}},
		}
	}

	old := at(0).Add(-2 * time.Hour)
	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{mk(20_000_000, old)}})
	s := findSession(t, snap, "claude", "s1")
	if s.CostUSD == nil || *s.CostUSD < 100 {
		t.Fatalf("fixture is not expensive enough to exercise the spike: CostUSD = %v", s.CostUSD)
	}
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("first sighting of existing spend: BurnUSDPerHr = %v, want 0", s.BurnUSDPerHr)
	}

	backfillAt := at(0).Add(1300 * time.Millisecond)
	snap = st.Reduce(Inputs{At: backfillAt, Sessions: []domain.Session{mk(40_000_000, old.Add(time.Minute))}})
	s = findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("backfilled cost jump: BurnUSDPerHr = %v, want exactly 0", s.BurnUSDPerHr)
	}
	if snap.TotalBurnUSDPerHr != 0 {
		t.Fatalf("TotalBurnUSDPerHr = %v after a backfill-only cycle, want 0", snap.TotalBurnUSDPerHr)
	}

	liveAt := at(0).Add(10 * time.Second)
	snap = st.Reduce(Inputs{At: liveAt, Sessions: []domain.Session{mk(40_100_000, liveAt)}})
	s = findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr <= 0 {
		t.Fatalf("live spend after a backfill: BurnUSDPerHr = %v, want > 0", s.BurnUSDPerHr)
	}
	if *s.BurnUSDPerHr > 1000 {
		t.Fatalf("live spend after a backfill: BurnUSDPerHr = %v, implausibly large", *s.BurnUSDPerHr)
	}
}

func TestExpiredSessionIsForgottenByTheBurnTracker(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	mk := func(usage int64) domain.Session {
		return domain.Session{
			Agent:  "claude",
			ID:     "s1",
			Status: "busy",
			Model:  "claude-opus-5",
			Usage:  domain.Usage{Input: usage, Output: usage / 2},
			Tools:  []domain.ToolCall{{Name: "Bash", ID: "t1", At: at(0)}},
		}
	}

	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{mk(1_000_000)}})
	st.Reduce(Inputs{At: at(1), Sessions: []domain.Session{mk(2_000_000)}})

	if snap := st.Reduce(Inputs{At: at(40)}); hasSession(snap, "claude", "s1") {
		t.Fatalf("session still present past sessionTTL, want it dropped")
	}

	back := mk(40_000_000)
	back.Tools = []domain.ToolCall{{Name: "Bash", ID: "t2", At: at(41)}}
	snap := st.Reduce(Inputs{At: at(41), Sessions: []domain.Session{back}})
	s := findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("returning session: BurnUSDPerHr = %v, want 0 — it must baseline again, not diff against pre-drop cost", s.BurnUSDPerHr)
	}
}
