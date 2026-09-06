package state

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
)

// newTestState returns a State backed by the real embedded pricing table
// (Load never touches the network or disk) and a burn tracker whose window
// is short enough that idle-decays-to-zero assertions don't need to wait
// out a production-sized window.
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

// TestBurnDecaysToZeroWhenIdle: a session goes busy (cost climbing) then
// idle (cost flat) and its burn rate decays to exactly zero once the
// tracker's window has elapsed since the last real cost change.
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

	// Idle: usage (and so cost) stops changing. Reduce must not keep
	// refreshing the tracker's clock on an unchanged total.
	snap = st.Reduce(Inputs{At: at(2), Sessions: []domain.Session{mk(2000)}})
	s = findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr <= 0 {
		t.Fatalf("still within window: BurnUSDPerHr = %v, want > 0", s.BurnUSDPerHr)
	}

	// Past the window since the last real cost change (at(1)).
	snap = st.Reduce(Inputs{At: at(10), Sessions: []domain.Session{mk(2000)}})
	s = findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("past window while idle: BurnUSDPerHr = %v, want exactly 0", s.BurnUSDPerHr)
	}
}

// TestPidDisappearsRowSurvives: a pid vanishes from the process table
// mid-run and the session's Proc becomes nil without the row itself
// vanishing from the snapshot.
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

	snap = st.Reduce(Inputs{At: at(1), Sessions: []domain.Session{session}}) // no Procs at all now
	s = findSession(t, snap, "claude", "s1")
	if s.Proc != nil {
		t.Fatalf("Proc = %+v, want nil once the pid is gone from the process table", s.Proc)
	}
	if s.Status != "busy" {
		t.Fatalf("Status = %q, want row to survive unchanged", s.Status)
	}
}

// TestUnpricedContributesZeroAndIsFlagged: a model the pricing table does
// not carry contributes 0 to TotalCostUSD, renders as unpriced ($— via a
// nil CostUSD) and is named in UnpricedModels.
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

// TestCostTierKeysOnLastPromptNotCumulativeUsage: a session whose
// cumulative Usage has crossed claude-sonnet-4-5's 200k-token tier
// threshold, but whose last request (ContextUsed) was well under it, must
// still price at the base rate — tier selection keys on the per-request
// prompt size, never on the lifetime sum. Pricing this at the long-context
// tier because the running total happens to exceed 200k would overcharge
// essentially every real multi-turn session within a handful of turns.
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

	// Base-rate cost computed directly from the sonnet-4-5 base per-token
	// rates, independent of reduce.go's own tier logic.
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

// TestSubagentCostSurvivesAfterItFinishes: a subagent's cost is folded into
// the session total, and stays there once the subagent drops out of the
// live set (Live: false) on a later cycle — it is never re-subtracted.
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

// TestHistoryRingCaps: pushing more than historyCap samples into a ring
// keeps exactly historyCap of them, dropping the oldest first.
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
	// Oldest surviving sample is from i=50 (0..49 dropped), newest is
	// i=historyCap+49.
	if watts[0] != 50 {
		t.Fatalf("watts[0] = %v, want 50 (oldest 50 samples dropped)", watts[0])
	}
	if watts[len(watts)-1] != float64(historyCap+49) {
		t.Fatalf("watts[last] = %v, want %v", watts[len(watts)-1], float64(historyCap+49))
	}
}

// TestVanishedSessionGoesStaleThenDrops covers the whole "stale then
// drops" lifecycle: retained at +1s and +29s, dropped at +31s; a session
// that reappears inside the TTL comes back with its source-supplied
// status; a Codex session already "stale" from its own source keeps its
// own StatusSince rather than being restamped.
func TestVanishedSessionGoesStaleThenDrops(t *testing.T) {
	t.Run("stale then drops at sessionTTL", func(t *testing.T) {
		st := newTestState(t, 60*time.Second)
		busy := domain.Session{Agent: "claude", ID: "s1", Status: "busy", Model: "claude-opus-5"}

		st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{busy}})

		snap := st.Reduce(Inputs{At: at(1)}) // vanished
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
		st.Reduce(Inputs{At: at(1)}) // vanished, goes stale

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

// TestFailedPollPreservesRows: while a source reports unhealthy, its rows
// are retained exactly as last seen — no stale transition, no TTL
// countdown — and only once the poll succeeds again (even with the session
// still absent) does the ordinary stale-then-drop lifecycle resume.
func TestFailedPollPreservesRows(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	claudeBusy := domain.Session{Agent: "claude", ID: "c1", Status: "busy", Model: "claude-opus-5"}
	codexWaiting := domain.Session{Agent: "codex", ID: "x1", Status: "waiting", Model: "gpt-5.6-terra"}

	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{claudeBusy, codexWaiting}})
	if !hasSession(snap, "claude", "c1") || !hasSession(snap, "codex", "x1") {
		t.Fatalf("cycle 1: expected both sessions present")
	}

	// Cycles 2-5 (0..40s): claude's poll is failing, no claude sessions
	// reported; codex stays healthy and keeps reporting its row.
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

	// Cycle 6 (+50s): claude reports healthy again, session still absent —
	// only now does the stale transition happen.
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

	// The TTL countdown is anchored at cycle 6 (+50s), not at the last
	// unhealthy cycle (+40s). +75s is inside the window those two anchors
	// disagree about: 25s past cycle 6 (retained) but 35s past +40s
	// (would already be dropped). Jumping straight from +50s to +81s
	// cannot tell the two apart, which is how the wrong anchor survived a
	// previous round of this test.
	snap = st.Reduce(Inputs{
		At:       at(75),
		Sessions: []domain.Session{codexWaiting},
		Health:   []SourceHealth{{Name: "claude", OK: true}},
	})
	s = findSession(t, snap, "claude", "c1")
	if s.Status != "stale" || !s.StatusSince.Equal(at(50)) {
		t.Fatalf("at +75s: Status=%q StatusSince=%v, want stale/%v — the countdown runs from the first healthy cycle that omitted the row, not from the outage", s.Status, s.StatusSince, at(50))
	}

	// sessionTTL after cycle 6's stamp, the row drops.
	snap = st.Reduce(Inputs{
		At:       at(50 + 31),
		Sessions: []domain.Session{codexWaiting},
		Health:   []SourceHealth{{Name: "claude", OK: true}},
	})
	if hasSession(snap, "claude", "c1") {
		t.Fatalf("claude row still present past sessionTTL after recovery, want dropped")
	}
}

// TestResightingAfterOutageReanchorsTTL: the outage-recovery re-anchor is
// armed by an outage and disarmed by a real sighting. A session seen again
// after its source recovers must go back to the ordinary lifecycle —
// dropping sessionTTL after that sighting — rather than carrying the
// outage's "start the countdown at the stale stamp" behaviour forward.
func TestResightingAfterOutageReanchorsTTL(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	busy := domain.Session{Agent: "claude", ID: "c1", Status: "busy", Model: "claude-opus-5"}

	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{busy}})

	// Outage: the row is held, and the TTL anchor moves off lastSeenAt.
	st.Reduce(Inputs{
		At:     at(10),
		Health: []SourceHealth{{Name: "claude", OK: false, Err: "sessions dir unreadable"}},
	})

	// Recovery with the session actually reported again: this is a real
	// sighting, so the anchor returns to lastSeenAt and stays there.
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

	// Anchored at the +20s sighting: retained at exactly sessionTTL...
	snap = st.Reduce(Inputs{At: at(50), Health: []SourceHealth{{Name: "claude", OK: true}}})
	if !hasSession(snap, "claude", "c1") {
		t.Fatalf("at +50s: row dropped, want it retained until sessionTTL after the +20s sighting")
	}

	// ...and gone one second past it. This is the assertion that bites if
	// the outage re-anchor leaks past a real sighting: anchored at the
	// +21s stale stamp instead, the row would still be here.
	snap = st.Reduce(Inputs{At: at(51), Health: []SourceHealth{{Name: "claude", OK: true}}})
	if hasSession(snap, "claude", "c1") {
		t.Fatalf("at +51s: row still present, want it dropped sessionTTL after the +20s sighting — the outage re-anchor must not survive a real sighting")
	}
}

// TestHealthSetsAndClearsDegraded: an unhealthy source produces a Degraded
// badge naming it without disturbing session rows; the badge disappears the
// moment that source reports healthy again — never carried forward.
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

// TestSnapshotAtIsTheCycleStamp: Inputs.At is copied onto both Snapshot.At
// and Snapshot.Sys.At, overwriting whatever the sampler put in Sys.At —
// this is the assertion that the sys and proc halves of a snapshot describe
// one instant.
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

// TestReduceFromJSONFixture: Inputs.Sys and Inputs.Sessions decode straight
// off disk, through the same domain JSON tags the headless --json contract
// uses, and Reduce still stamps Inputs.At over both Snapshot.At and
// Snapshot.Sys.At even though the fixture's own sys.at is different.
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

// TestBackfilledHistoryDoesNotBurn: at launch the tailers read hours of
// transcript in the first cycle or two, so a session's cost jumps by tens of
// dollars between two cycles a second apart. That is spend wattop was not
// running for; it must not appear as $/hr. Only a delta whose usage records
// are stamped inside the tracker's window counts.
func TestBackfilledHistoryDoesNotBurn(t *testing.T) {
	st := newTestState(t, 60*time.Second)

	// Tool-call timestamps stand in for the usage records' own timestamps:
	// they come off the same assistant lines (internal/agent/claude/parse.go).
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

	// Cycle 0 baselines a session already holding hours of spend.
	old := at(0).Add(-2 * time.Hour)
	snap := st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{mk(20_000_000, old)}})
	s := findSession(t, snap, "claude", "s1")
	if s.CostUSD == nil || *s.CostUSD < 100 {
		t.Fatalf("fixture is not expensive enough to exercise the spike: CostUSD = %v", s.CostUSD)
	}
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("first sighting of existing spend: BurnUSDPerHr = %v, want 0", s.BurnUSDPerHr)
	}

	// Cycle 1, 1.3s later: the tailer finishes the backlog and cost leaps,
	// but every usage record behind the leap is two hours old.
	backfillAt := at(0).Add(1300 * time.Millisecond)
	snap = st.Reduce(Inputs{At: backfillAt, Sessions: []domain.Session{mk(40_000_000, old.Add(time.Minute))}})
	s = findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("backfilled cost jump: BurnUSDPerHr = %v, want exactly 0", s.BurnUSDPerHr)
	}
	if snap.TotalBurnUSDPerHr != 0 {
		t.Fatalf("TotalBurnUSDPerHr = %v after a backfill-only cycle, want 0", snap.TotalBurnUSDPerHr)
	}

	// Live spend afterwards still reports: a small delta whose usage is
	// stamped now is a real, plausible rate.
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

// TestExpiredSessionIsForgottenByTheBurnTracker: dropping a row past
// sessionTTL must forget its burn state too. Otherwise the tracker keeps one
// entry per session for the life of the process, and an id that comes back
// is diffed against a cumulative cost from before it left — the backfill
// spike in miniature.
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

	// Vanish, then past sessionTTL: the row drops.
	if snap := st.Reduce(Inputs{At: at(40)}); hasSession(snap, "claude", "s1") {
		t.Fatalf("session still present past sessionTTL, want it dropped")
	}

	// The same id returns, having spent a great deal while it was gone.
	back := mk(40_000_000)
	back.Tools = []domain.ToolCall{{Name: "Bash", ID: "t2", At: at(41)}}
	snap := st.Reduce(Inputs{At: at(41), Sessions: []domain.Session{back}})
	s := findSession(t, snap, "claude", "s1")
	if s.BurnUSDPerHr == nil || *s.BurnUSDPerHr != 0 {
		t.Fatalf("returning session: BurnUSDPerHr = %v, want 0 — it must baseline again, not diff against pre-drop cost", s.BurnUSDPerHr)
	}
}
