package demo

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

// frame is everything a World emits at one tick, serialised so two worlds
// can be compared byte for byte.
func frame(t *testing.T, w *World, at time.Time) string {
	t.Helper()
	b, err := json.Marshal(struct {
		Sys      any
		Procs    any
		Sessions any
	}{w.Sys(), w.Procs(at), w.Sessions("", at)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestWorldIsDeterministic(t *testing.T) {
	a := New(DefaultSeed, time.Second)
	b := New(DefaultSeed, time.Second)
	for i := 0; i < 300; i++ {
		at := epoch.Add(time.Duration(i) * time.Second)
		if fa, fb := frame(t, a, at), frame(t, b, at); fa != fb {
			t.Fatalf("tick %d: same seed and times diverged:\n%s\n%s", i, fa, fb)
		}
		a.Advance()
		b.Advance()
	}
}

func TestWorldSeedMatters(t *testing.T) {
	a := New(DefaultSeed, time.Second)
	b := New(DefaultSeed+1, time.Second)
	for i := 0; i < 30; i++ {
		a.Advance()
		b.Advance()
	}
	if frame(t, a, epoch) == frame(t, b, epoch) {
		t.Fatal("different seeds produced identical output")
	}
}

func TestWorldIsPlausible(t *testing.T) {
	w := New(DefaultSeed, time.Second)
	statuses := map[string]bool{}
	var lastCost = map[string]int64{}
	for i := 0; i < 600; i++ {
		w.Advance()
		at := epoch.Add(time.Duration(i) * time.Second)

		s := w.Sys()
		if s.SoCName != SoCName {
			t.Fatalf("SoCName = %q", s.SoCName)
		}
		if p := *s.Power.SystemWatts; p < 4 || p > 35 {
			t.Errorf("tick %d: system power %.2f W outside 4-35", i, p)
		}
		for k, v := range s.Temps {
			if v < 40 || v > 85 {
				t.Errorf("tick %d: temp %s = %.1f outside 40-85", i, k, v)
			}
		}
		if len(s.Fans) != 2 || len(s.Clusters) != 2 {
			t.Fatalf("tick %d: fans %d clusters %d, want 2 and 2", i, len(s.Fans), len(s.Clusters))
		}

		sessions := w.Sessions("", at)
		procs := w.Procs(at)
		if len(procs) != len(sessions) {
			t.Fatalf("procs %d != sessions %d", len(procs), len(sessions))
		}
		for j, sess := range sessions {
			statuses[sess.Status] = true
			if *sess.PID != procs[j].PID || sess.CWD != procs[j].CWD {
				t.Errorf("session %s not joined to its process", sess.ID)
			}
			// Cumulative usage must never go backwards, or burn would dip.
			u := sess.Usage
			total := u.Input + u.Output + u.CacheRead + u.CacheCreate5m
			if total < lastCost[sess.ID] {
				t.Errorf("tick %d: session %s usage went backwards", i, sess.ID)
			}
			lastCost[sess.ID] = total
		}
	}
	if !statuses["busy"] || !statuses["waiting"] {
		t.Errorf("statuses seen = %v, want both busy and waiting", statuses)
	}
	if n := len(w.Sessions("claude", epoch)); n < 3 {
		t.Errorf("claude sessions = %d, want >= 3", n)
	}
	if n := len(w.Sessions("codex", epoch)); n < 1 {
		t.Errorf("codex sessions = %d, want >= 1", n)
	}
}

// TestNoRealPaths guards the reason --demo exists: nothing it emits may
// carry the recording machine's home directory or any absolute user path.
func TestNoRealPaths(t *testing.T) {
	w := New(DefaultSeed, time.Second)
	var out strings.Builder
	for i := 0; i < 120; i++ {
		w.Advance()
		out.WriteString(frame(t, w, epoch.Add(time.Duration(i)*time.Second)))
	}
	text := out.String()
	forbidden := []string{"/Users/", "/home/", "/root/"}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		forbidden = append(forbidden, home)
	}
	for _, f := range forbidden {
		if strings.Contains(text, f) {
			t.Errorf("demo output contains %q", f)
		}
	}
}
