package state

import (
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func TestRingPushAndValues(t *testing.T) {
	r := newRing(3)
	if got := r.values(); len(got) != 0 {
		t.Fatalf("values() on empty ring = %v, want empty", got)
	}
	r.push(1)
	r.push(2)
	if got := r.values(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("values() = %v, want [1 2]", got)
	}
}

func TestRingDropsOldest(t *testing.T) {
	r := newRing(3)
	r.push(1)
	r.push(2)
	r.push(3)
	r.push(4)
	got := r.values()
	want := []float64{2, 3, 4}
	if len(got) != len(want) {
		t.Fatalf("values() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("values() = %v, want %v", got, want)
		}
	}
}

func TestRingValuesIsACopy(t *testing.T) {
	r := newRing(3)
	r.push(1)
	got := r.values()
	got[0] = 99
	if r.values()[0] != 1 {
		t.Fatalf("mutating a returned slice affected the ring's own backing array")
	}
}

func TestStateHistoryUnknownKeyIsNil(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	if got := st.History("no-such-key"); got != nil {
		t.Fatalf("History(unknown key) = %v, want nil", got)
	}
}

// TestStateHistoryPerSessionKeys: Reduce pushes machine-wide and
// per-session rings on every cycle, readable back through History with the
// "<agent>:<sessionID>:<metric>" key grammar.
func TestStateHistoryPerSessionKeys(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	pid := 100
	session := domain.Session{
		Agent: "claude", ID: "s1", PID: &pid, Status: "busy", Model: "claude-opus-5",
	}
	proc := domain.ProcSample{PID: pid, CPUPct: 42}
	watts := 30.0

	st.Reduce(Inputs{
		At:       at(0),
		Sessions: []domain.Session{session},
		Procs:    []domain.ProcSample{proc},
		Sys:      domain.SysSample{Power: domain.Power{SystemWatts: &watts}},
	})

	cpu := st.History("claude:s1:cpu")
	if len(cpu) != 1 || cpu[0] != 42 {
		t.Fatalf("History(\"claude:s1:cpu\") = %v, want [42]", cpu)
	}
	machineWatts := st.History("watts")
	if len(machineWatts) != 1 || machineWatts[0] != 30 {
		t.Fatalf("History(\"watts\") = %v, want [30]", machineWatts)
	}
}

// TestStateHistoryDroppedOnSessionTTLExpiry: a session's three per-session
// rings must not outlive its tracked entry, or st.histories grows without
// bound over a long run (mirroring the guard already in
// internal/agent/codex/source.go).
func TestStateHistoryDroppedOnSessionTTLExpiry(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	pid := 100
	session := domain.Session{
		Agent: "claude", ID: "s1", PID: &pid, Status: "busy", Model: "claude-opus-5",
	}
	proc := domain.ProcSample{PID: pid, CPUPct: 42}

	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{session}, Procs: []domain.ProcSample{proc}})

	if got := st.History("claude:s1:cpu"); got == nil {
		t.Fatalf("History(\"claude:s1:cpu\") = nil after first sighting, want a populated ring")
	}

	// Vanish it past sessionTTL: at(0) then nothing until well past the
	// 30s TTL drops the tracked entry.
	st.Reduce(Inputs{At: at(31)})

	if got := st.History("claude:s1:cpu"); got != nil {
		t.Fatalf("History(\"claude:s1:cpu\") = %v after TTL drop, want nil (ring must be deleted, not retained forever)", got)
	}
	if got := st.History("claude:s1:gpu"); got != nil {
		t.Fatalf("History(\"claude:s1:gpu\") = %v after TTL drop, want nil", got)
	}
	if got := st.History("claude:s1:cost"); got != nil {
		t.Fatalf("History(\"claude:s1:cost\") = %v after TTL drop, want nil", got)
	}
	if _, ok := st.histories["claude:s1:cpu"]; ok {
		t.Fatalf("st.histories still holds claude:s1:cpu after TTL drop, want it deleted from the map")
	}
}

// TestStateHistoryPerSessionKeysDistinguishAgent: a Claude and a Codex
// session sharing an ID must not collide in st.histories — dropping one's
// rings on TTL expiry must not clobber the other's.
func TestStateHistoryPerSessionKeysDistinguishAgent(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	claudePID, codexPID := 100, 200
	claude := domain.Session{Agent: "claude", ID: "dup", PID: &claudePID, Status: "busy", Model: "claude-opus-5"}
	codex := domain.Session{Agent: "codex", ID: "dup", PID: &codexPID, Status: "busy", Model: "claude-opus-5"}
	procs := []domain.ProcSample{
		{PID: claudePID, CPUPct: 11},
		{PID: codexPID, CPUPct: 22},
	}

	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{claude, codex}, Procs: procs})

	if got := st.History("claude:dup:cpu"); len(got) != 1 || got[0] != 11 {
		t.Fatalf("History(\"claude:dup:cpu\") = %v, want [11]", got)
	}
	if got := st.History("codex:dup:cpu"); len(got) != 1 || got[0] != 22 {
		t.Fatalf("History(\"codex:dup:cpu\") = %v, want [22]", got)
	}

	// Claude's session vanishes and drops past TTL; Codex's ring under the
	// same bare ID must survive untouched.
	st.Reduce(Inputs{At: at(31), Sessions: []domain.Session{codex}, Procs: []domain.ProcSample{{PID: codexPID, CPUPct: 22}}})

	if got := st.History("claude:dup:cpu"); got != nil {
		t.Fatalf("History(\"claude:dup:cpu\") = %v after TTL drop, want nil", got)
	}
	if got := st.History("codex:dup:cpu"); got == nil {
		t.Fatalf("History(\"codex:dup:cpu\") = nil, want the codex session's ring to survive the claude session's drop")
	}
}
