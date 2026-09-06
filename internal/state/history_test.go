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
// "<sessionID>:<metric>" key grammar.
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

	cpu := st.History("s1:cpu")
	if len(cpu) != 1 || cpu[0] != 42 {
		t.Fatalf("History(\"s1:cpu\") = %v, want [42]", cpu)
	}
	machineWatts := st.History("watts")
	if len(machineWatts) != 1 || machineWatts[0] != 30 {
		t.Fatalf("History(\"watts\") = %v, want [30]", machineWatts)
	}
}
