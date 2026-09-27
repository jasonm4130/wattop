package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/state"
)

func TestDemoFlagParses(t *testing.T) {
	flags, err := parseFlags([]string{"--demo", "--json", "--once"})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !flags.demo {
		t.Error("demo = false, want true")
	}
	var usage bytes.Buffer
	writeUsage(&usage)
	if !strings.Contains(usage.String(), "synthetic data for screenshots") {
		t.Errorf("--help does not describe --demo:\n%s", usage.String())
	}
}

// demoSnapshot runs what `wattop --demo --json --once` runs and returns
// the printed line and its decoded Snapshot.
func demoSnapshot(t *testing.T) (string, domain.Snapshot) {
	t.Helper()
	src, book, world, err := buildDemoSources(time.Second)
	if err != nil {
		t.Fatalf("buildDemoSources: %v", err)
	}
	st := state.New(book, pricing.NewBurnTracker(burnWindow, burnAlpha))
	prewarmDemo(st, world, time.Now(), demoPrewarm)
	loop := NewLoop(src, time.Second, st, true)

	var buf bytes.Buffer
	if _, err := runOnce(context.Background(), loop, &buf); err != nil {
		t.Fatalf("runOnce: %v", err)
	}
	var snap domain.Snapshot
	if err := json.Unmarshal(buf.Bytes(), &snap); err != nil {
		t.Fatalf("decoding snapshot: %v", err)
	}
	return buf.String(), snap
}

func TestDemoSnapshotIsLiveAndPriced(t *testing.T) {
	_, snap := demoSnapshot(t)

	if len(snap.Sessions) < 4 {
		t.Fatalf("sessions = %d, want >= 4", len(snap.Sessions))
	}
	agents := map[string]int{}
	for _, s := range snap.Sessions {
		agents[s.Agent]++
		if !s.Priced || s.CostUSD == nil || *s.CostUSD <= 0 {
			t.Errorf("session %s (%s) is not priced: priced=%v cost=%v", s.ID, s.Model, s.Priced, s.CostUSD)
		}
		if s.CostPartial {
			t.Errorf("session %s cost is partial", s.ID)
		}
		if s.Proc == nil {
			t.Errorf("session %s has no process row", s.ID)
		}
		for _, sa := range s.Subagents {
			if sa.CostUSD == nil {
				t.Errorf("subagent %s (%s) is not priced", sa.ID, sa.Model)
			}
		}
	}
	if agents["claude"] < 3 || agents["codex"] < 1 {
		t.Errorf("agents = %v, want >= 3 claude and >= 1 codex", agents)
	}
	if len(snap.UnpricedModels) != 0 || snap.TotalCostPartial {
		t.Errorf("unpriced models %v, partial %v", snap.UnpricedModels, snap.TotalCostPartial)
	}
	if snap.TotalCostUSD <= 0 || snap.TotalBurnUSDPerHr <= 0 {
		t.Errorf("total cost %.2f burn %.2f, want both > 0", snap.TotalCostUSD, snap.TotalBurnUSDPerHr)
	}
	if p := snap.Sys.Power.SystemWatts; p == nil || *p <= 0 {
		t.Errorf("system watts = %v, want > 0", p)
	}
	if len(snap.Degraded) != 0 {
		t.Errorf("degraded = %v, want none", snap.Degraded)
	}
}

func TestDemoSnapshotHasNoRealPaths(t *testing.T) {
	line, _ := demoSnapshot(t)
	forbidden := []string{"/Users/", "/home/"}
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		forbidden = append(forbidden, home)
	}
	for _, f := range forbidden {
		if strings.Contains(line, f) {
			t.Errorf("demo snapshot contains %q", f)
		}
	}
}
