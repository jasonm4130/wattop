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
	if got := st.history(historyKey{sessionKey{"claude", "no-such-session"}, MetricCPU}); got != nil {
		t.Fatalf("History(unknown key) = %v, want nil", got)
	}
}

func TestStateHistoryPerSessionKeys(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	pid := 100
	session := domain.Session{
		Agent: "claude", ID: "s1", PID: &pid, Status: "busy", Model: "claude-opus-5",
	}
	proc := domain.ProcSample{PID: pid, CPUPct: floatp(42)}
	watts := 30.0

	st.Reduce(Inputs{
		At:       at(0),
		Sessions: []domain.Session{session},
		Procs:    []domain.ProcSample{proc},
		Sys:      domain.SysSample{Power: domain.Power{SystemWatts: &watts}},
	})

	cpu := sessionHistory(st, "claude", "s1", MetricCPU)
	if len(cpu) != 1 || cpu[0] != 42 {
		t.Fatalf("History(\"claude:s1:cpu\") = %v, want [42]", cpu)
	}
	machineWatts := st.History(MetricWatts)
	if len(machineWatts) != 1 || machineWatts[0] != 30 {
		t.Fatalf("History(\"watts\") = %v, want [30]", machineWatts)
	}
}

func TestStateHistoryDroppedOnSessionTTLExpiry(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	pid := 100
	session := domain.Session{
		Agent: "claude", ID: "s1", PID: &pid, Status: "busy", Model: "claude-opus-5",
	}
	proc := domain.ProcSample{PID: pid, CPUPct: floatp(42)}

	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{session}, Procs: []domain.ProcSample{proc}})

	if got := sessionHistory(st, "claude", "s1", MetricCPU); got == nil {
		t.Fatalf("History(\"claude:s1:cpu\") = nil after first sighting, want a populated ring")
	}

	st.Reduce(Inputs{At: at(31)})

	if got := sessionHistory(st, "claude", "s1", MetricCPU); got != nil {
		t.Fatalf("History(\"claude:s1:cpu\") = %v after TTL drop, want nil (ring must be deleted, not retained forever)", got)
	}
	if got := sessionHistory(st, "claude", "s1", MetricGPU); got != nil {
		t.Fatalf("History(\"claude:s1:gpu\") = %v after TTL drop, want nil", got)
	}
	if got := sessionHistory(st, "claude", "s1", MetricCost); got != nil {
		t.Fatalf("History(\"claude:s1:cost\") = %v after TTL drop, want nil", got)
	}
	if _, ok := st.histories[historyKey{sessionKey{"claude", "s1"}, MetricCPU}]; ok {
		t.Fatalf("st.histories still holds claude:s1:cpu after TTL drop, want it deleted from the map")
	}
}

func TestStateHistoryPerSessionKeysDistinguishAgent(t *testing.T) {
	st := newTestState(t, 60*time.Second)
	claudePID, codexPID := 100, 200
	claude := domain.Session{Agent: "claude", ID: "dup", PID: &claudePID, Status: "busy", Model: "claude-opus-5"}
	codex := domain.Session{Agent: "codex", ID: "dup", PID: &codexPID, Status: "busy", Model: "claude-opus-5"}
	procs := []domain.ProcSample{
		{PID: claudePID, CPUPct: floatp(11)},
		{PID: codexPID, CPUPct: floatp(22)},
	}

	st.Reduce(Inputs{At: at(0), Sessions: []domain.Session{claude, codex}, Procs: procs})

	if got := sessionHistory(st, "claude", "dup", MetricCPU); len(got) != 1 || got[0] != 11 {
		t.Fatalf("History(\"claude:dup:cpu\") = %v, want [11]", got)
	}
	if got := sessionHistory(st, "codex", "dup", MetricCPU); len(got) != 1 || got[0] != 22 {
		t.Fatalf("History(\"codex:dup:cpu\") = %v, want [22]", got)
	}

	st.Reduce(Inputs{At: at(31), Sessions: []domain.Session{codex}, Procs: []domain.ProcSample{{PID: codexPID, CPUPct: floatp(22)}}})

	if got := sessionHistory(st, "claude", "dup", MetricCPU); got != nil {
		t.Fatalf("History(\"claude:dup:cpu\") = %v after TTL drop, want nil", got)
	}
	if got := sessionHistory(st, "codex", "dup", MetricCPU); got == nil {
		t.Fatalf("History(\"codex:dup:cpu\") = nil, want the codex session's ring to survive the claude session's drop")
	}
}

func sessionHistory(st *State, agent, id string, m Metric) []float64 {
	return st.history(historyKey{sessionKey{agent, id}, m})
}
