package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/collect/replay"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/fixture"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/state"
)

// replaySources builds a Sources over the Task 3 corpus (soc) and the
// Task 4 replay fixtures (proc, one agent source), for tests that need the
// real coordinated cycle over deterministic, cgo-free data.
func replaySources(t *testing.T) Sources {
	t.Helper()

	sys, err := replay.NewSysSampler(filepath.Join(fixture.CorpusDir(), "soc"))
	if err != nil {
		t.Fatalf("replay.NewSysSampler: %v", err)
	}
	proc, err := replay.NewProcSource(filepath.Join(fixture.CorpusDir(), "replay", "procs.json"))
	if err != nil {
		t.Fatalf("replay.NewProcSource: %v", err)
	}
	agent, err := replay.NewAgentSource(filepath.Join(fixture.CorpusDir(), "replay"))
	if err != nil {
		t.Fatalf("replay.NewAgentSource: %v", err)
	}

	return Sources{Sys: sys, Proc: proc, Agents: []domain.AgentSource{agent}}
}

func testState(t *testing.T) *state.State {
	t.Helper()
	book, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}
	return state.New(book, pricing.NewBurnTracker(burnWindow, burnAlpha))
}

// TestFlagSurfaceParses asserts every documented flag is recognised and
// takes the expected type.
func TestFlagSurfaceParses(t *testing.T) {
	flags, err := parseFlags([]string{
		"--theme", "nord",
		"--interval", "2s",
		"--json",
		"--once",
		"--no-color",
	})
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if flags.theme != "nord" {
		t.Errorf("theme = %q, want nord", flags.theme)
	}
	if flags.interval != 2*time.Second {
		t.Errorf("interval = %v, want 2s", flags.interval)
	}
	if !flags.json || !flags.once || !flags.noColor {
		t.Errorf("json/once/no-color = %v/%v/%v, want all true", flags.json, flags.once, flags.noColor)
	}

	versionFlags, err := parseFlags([]string{"--version"})
	if err != nil {
		t.Fatalf("parseFlags(--version): %v", err)
	}
	if !versionFlags.version {
		t.Error("version = false, want true")
	}
}

// TestOnceJSONFromReplay wires the replay sources over the corpus through
// one coordinated cycle and asserts --once --json's contract: exactly one
// valid Snapshot line, with sys.soc_name set and sessions an array.
func TestOnceJSONFromReplay(t *testing.T) {
	src := replaySources(t)
	loop := NewLoop(src, time.Second, testState(t), true)

	var buf bytes.Buffer
	if _, err := runOnce(context.Background(), loop, &buf); err != nil {
		t.Fatalf("runOnce: %v", err)
	}

	lines := 0
	scanner := bufio.NewScanner(&buf)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var snap domain.Snapshot
	for scanner.Scan() {
		lines++
		if err := json.Unmarshal(scanner.Bytes(), &snap); err != nil {
			t.Fatalf("decoding Snapshot line: %v", err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning output: %v", err)
	}
	if lines != 1 {
		t.Fatalf("got %d lines, want exactly 1", lines)
	}
	if snap.Sys.SoCName == "" {
		t.Error("sys.soc_name is empty")
	}
	if snap.Sessions == nil {
		t.Error("sessions is nil, want a (possibly empty) array")
	}
}

// TestCycleStampIsShared asserts one replay cycle's Snapshot.At equals
// Snapshot.Sys.At -- the coordinated-cycle invariant that makes the sys and
// session halves of a Snapshot describe one instant.
func TestCycleStampIsShared(t *testing.T) {
	src := replaySources(t)
	loop := NewLoop(src, time.Second, testState(t), true)

	in := loop.Cycle(context.Background())
	snap := loop.State.Reduce(in)

	if !snap.At.Equal(snap.Sys.At) {
		t.Errorf("Snapshot.At = %v, Snapshot.Sys.At = %v; want equal", snap.At, snap.Sys.At)
	}
}

// panickingSampler implements domain.Sampler, panicking on its first N
// Sample calls and succeeding thereafter -- the fixture
// TestPanickingCollectorDegradesNotDies drives to prove a panic degrades a
// source rather than killing the run loop, and that the badge clears once
// the panic stops.
type panickingSampler struct {
	failCalls int
	calls     int
	inner     domain.Sampler
}

func (s *panickingSampler) Init() error               { return s.inner.Init() }
func (s *panickingSampler) Close() error              { return s.inner.Close() }
func (s *panickingSampler) ThermalState() int         { return s.inner.ThermalState() }
func (s *panickingSampler) Channels() map[string]bool { return s.inner.Channels() }
func (s *panickingSampler) Sample(ctx context.Context, intervalMs int) (domain.SysSample, error) {
	s.calls++
	if s.calls <= s.failCalls {
		panic("panickingSampler: simulated collector panic")
	}
	return s.inner.Sample(ctx, intervalMs)
}

// TestPanickingCollectorDegradesNotDies covers both halves of a collector
// panic: the badge appears in Snapshot.Degraded while the fake sampler
// panics, and the badge is gone from Degraded on the first cycle after it
// recovers -- a badge that never clears is a different bug wearing the
// same shirt.
func TestPanickingCollectorDegradesNotDies(t *testing.T) {
	inner, err := replay.NewSysSampler(filepath.Join(fixture.CorpusDir(), "soc"))
	if err != nil {
		t.Fatalf("replay.NewSysSampler: %v", err)
	}
	src := replaySources(t)
	src.Sys = &panickingSampler{failCalls: 2, inner: inner}

	loop := NewLoop(src, time.Second, testState(t), true)
	loop.MaxCycles = 1

	ctx := context.Background()

	// Cycle 1: sampler panics. The run loop must not die, and the cycle
	// must still produce a Snapshot with a "soc" badge in Degraded.
	in1 := loop.Cycle(ctx)
	snap1 := loop.State.Reduce(in1)
	if !containsPrefix(snap1.Degraded, "soc:") {
		t.Fatalf("cycle 1 Degraded = %v, want a soc: entry while the collector panics", snap1.Degraded)
	}

	// Cycle 2: still panicking (failCalls == 2).
	in2 := loop.Cycle(ctx)
	snap2 := loop.State.Reduce(in2)
	if !containsPrefix(snap2.Degraded, "soc:") {
		t.Fatalf("cycle 2 Degraded = %v, want a soc: entry while the collector still panics", snap2.Degraded)
	}

	// Cycle 3: the fake stops panicking. The badge must clear on this
	// first healthy cycle.
	in3 := loop.Cycle(ctx)
	snap3 := loop.State.Reduce(in3)
	if containsPrefix(snap3.Degraded, "soc:") {
		t.Fatalf("cycle 3 Degraded = %v, want no soc: entry once the collector recovers", snap3.Degraded)
	}
}

func containsPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
