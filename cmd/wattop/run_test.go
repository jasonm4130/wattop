package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/collect/replay"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/fixture"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/soc"
	"github.com/jasonm4130/wattop/internal/state"
)

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

	book, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}

	return Sources{Sys: sys, Proc: proc, Agents: []domain.AgentSource{agent}, Pricing: book}
}

func testState(t *testing.T) *state.State {
	t.Helper()
	book, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}
	return state.New(book, pricing.NewBurnTracker(pricing.DefaultBurnWindow, pricing.DefaultBurnAlpha))
}

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

func TestHelpPrintsUsageAndExitsZero(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		_, err := parseFlags([]string{arg})
		var stdout, stderr bytes.Buffer
		if code := parseErrorExit(err, &stdout, &stderr); code != 0 {
			t.Errorf("%s: exit code = %d, want 0", arg, code)
		}
		if stderr.Len() != 0 {
			t.Errorf("%s: wrote to stderr: %q", arg, stderr.String())
		}
		out := stdout.String()
		for _, want := range []string{"-json", "-once", "-interval", "doctor", "-ioreport-groups"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: usage is missing %q:\n%s", arg, want, out)
			}
		}
	}

	_, err := parseFlags([]string{"--bogus"})
	var stdout, stderr bytes.Buffer
	if code := parseErrorExit(err, &stdout, &stderr); code != 2 {
		t.Errorf("--bogus: exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "bogus") {
		t.Errorf("--bogus: stdout=%q stderr=%q, want the error on stderr only", stdout.String(), stderr.String())
	}
}

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

func TestCycleStampIsShared(t *testing.T) {
	src := replaySources(t)
	loop := NewLoop(src, time.Second, testState(t), true)

	in := loop.Cycle(context.Background())
	snap := loop.State.Reduce(in)

	if !snap.At.Equal(snap.Sys.At) {
		t.Errorf("Snapshot.At = %v, Snapshot.Sys.At = %v; want equal", snap.At, snap.Sys.At)
	}
}

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

	in1 := loop.Cycle(ctx)
	snap1 := loop.State.Reduce(in1)
	if !containsPrefix(snap1.Degraded, "soc:") {
		t.Fatalf("cycle 1 Degraded = %v, want a soc: entry while the collector panics", snap1.Degraded)
	}

	in2 := loop.Cycle(ctx)
	snap2 := loop.State.Reduce(in2)
	if !containsPrefix(snap2.Degraded, "soc:") {
		t.Fatalf("cycle 2 Degraded = %v, want a soc: entry while the collector still panics", snap2.Degraded)
	}

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

type erroringSampler struct{ calls int }

func (s *erroringSampler) Init() error               { return nil }
func (s *erroringSampler) Close() error              { return nil }
func (s *erroringSampler) ThermalState() int         { return 0 }
func (s *erroringSampler) Channels() map[string]bool { return nil }
func (s *erroringSampler) Sample(context.Context, int) (domain.SysSample, error) {
	s.calls++
	return domain.SysSample{}, errors.New("erroringSampler: simulated sample failure")
}

type blockingSampler struct{ block time.Duration }

func (s *blockingSampler) Init() error               { return nil }
func (s *blockingSampler) Close() error              { return nil }
func (s *blockingSampler) ThermalState() int         { return 0 }
func (s *blockingSampler) Channels() map[string]bool { return nil }
func (s *blockingSampler) Sample(context.Context, int) (domain.SysSample, error) {
	time.Sleep(s.block)
	return domain.SysSample{}, nil
}

func recordAfter(loop *Loop) *[]time.Duration {
	var waits []time.Duration
	loop.after = func(d time.Duration) <-chan time.Time {
		waits = append(waits, d)
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	return &waits
}

func TestRunPacesWhenSampleFailsFast(t *testing.T) {
	src := replaySources(t)
	src.Sys = &erroringSampler{}

	const interval = time.Second
	loop := NewLoop(src, interval, testState(t), true)
	loop.MaxCycles = 5
	waits := recordAfter(loop)

	loop.Run(context.Background())

	if len(*waits) != loop.MaxCycles-1 {
		t.Fatalf("paced %d times over %d cycles, want %d", len(*waits), loop.MaxCycles, loop.MaxCycles-1)
	}
	for i, d := range *waits {
		if d <= 0 || d > interval {
			t.Errorf("wait %d = %v, want a positive remainder of %v", i, d, interval)
		}
		if d < interval/2 {
			t.Errorf("wait %d = %v, want nearly the whole %v interval for a cycle that did no blocking work", i, d, interval)
		}
	}
	if loop.n != loop.MaxCycles {
		t.Errorf("loop.n = %d, want %d", loop.n, loop.MaxCycles)
	}
	if sampler := src.Sys.(*erroringSampler); sampler.calls != loop.MaxCycles {
		t.Errorf("sampler was called %d times over %d cycles, want one call each", sampler.calls, loop.MaxCycles)
	}

	t.Run("on the real clock", func(t *testing.T) {
		const short = 50 * time.Millisecond
		realSrc := replaySources(t)
		realSrc.Sys = &erroringSampler{}
		realLoop := NewLoop(realSrc, short, testState(t), true)
		realLoop.MaxCycles = 4

		start := time.Now()
		realLoop.Run(context.Background())
		elapsed := time.Since(start)

		if want := short * time.Duration(realLoop.MaxCycles-1); elapsed < want {
			t.Fatalf("%d cycles of an instantly-failing sampler took %v, want at least %v", realLoop.MaxCycles, elapsed, want)
		}
	})
}

func TestRunDoesNotPaceWhenSampleBlocks(t *testing.T) {
	src := replaySources(t)
	const interval = 20 * time.Millisecond
	src.Sys = &blockingSampler{block: interval + 10*time.Millisecond}

	loop := NewLoop(src, interval, testState(t), true)
	loop.MaxCycles = 3
	waits := recordAfter(loop)

	loop.Run(context.Background())

	if len(*waits) != 0 {
		t.Fatalf("paced %v on top of a sampler that already blocked for the interval, want no extra wait", *waits)
	}
}

func TestRunJSONPacesAndStops(t *testing.T) {
	src := replaySources(t)
	src.Sys = &erroringSampler{}

	const interval = time.Second
	loop := NewLoop(src, interval, testState(t), true)
	loop.MaxCycles = 3
	waits := recordAfter(loop)

	var buf bytes.Buffer
	if err := runJSON(context.Background(), loop, &buf); err != nil {
		t.Fatalf("runJSON: %v", err)
	}

	if got := bytes.Count(buf.Bytes(), []byte("\n")); got != loop.MaxCycles {
		t.Errorf("wrote %d NDJSON lines, want %d", got, loop.MaxCycles)
	}
	if len(*waits) != loop.MaxCycles-1 {
		t.Fatalf("paced %d times over %d cycles, want %d", len(*waits), loop.MaxCycles, loop.MaxCycles-1)
	}
}

func TestCycleReportsPricingHealth(t *testing.T) {
	src := replaySources(t)
	loop := NewLoop(src, time.Second, testState(t), true)

	in := loop.Cycle(context.Background())
	var found *state.SourceHealth
	for i := range in.Health {
		if in.Health[i].Name == "pricing" {
			found = &in.Health[i]
		}
	}
	if found == nil {
		names := make([]string, 0, len(in.Health))
		for _, h := range in.Health {
			names = append(names, h.Name)
		}
		t.Fatalf("Health = %v, want a pricing entry", names)
	}
	if found.OK && found.Err != "" {
		t.Errorf("pricing health is OK but carries Err %q", found.Err)
	}
	if !found.OK && found.Err == "" {
		t.Error("pricing health is not OK but says nothing about why")
	}

	src.Pricing = nil
	bookless := NewLoop(src, time.Second, testState(t), true)
	for _, h := range bookless.Cycle(context.Background()).Health {
		if h.Name == "pricing" {
			t.Errorf("Health carries a pricing entry (%+v) with no book wired", h)
		}
	}
}

func TestPricingHealthPredicate(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		entries    int
		fetchedAt  time.Time
		refreshErr error
		wantOK     bool
	}{
		{"fresh embed", 900, now.Add(-time.Hour), nil, true},
		{"one hour inside the window", 900, now.Add(-pricingMaxAge + time.Hour), nil, true},
		{"one hour past the window", 900, now.Add(-pricingMaxAge - time.Hour), nil, false},
		{"empty table", 0, now, nil, false},
		{"no generation stamp", 900, time.Time{}, nil, false},
		{"fresh embed but refresh failed", 900, now.Add(-time.Hour), errors.New("fetch: offline"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := pricingHealth(tt.entries, tt.fetchedAt, tt.refreshErr, now)
			if ok != tt.wantOK {
				t.Fatalf("pricingHealth = %v (%v), want %v", ok, err, tt.wantOK)
			}
			if !ok && err == nil {
				t.Fatal("pricingHealth reported not-OK with a nil error")
			}
			if ok && err != nil {
				t.Fatalf("pricingHealth reported OK with error %v", err)
			}
		})
	}
}

type countingProcSource struct {
	inner domain.ProcSource
	scans int
}

func (p *countingProcSource) Scan(ctx context.Context) ([]domain.ProcSample, error) {
	p.scans++
	return p.inner.Scan(ctx)
}

func (p *countingProcSource) Close() error { return p.inner.Close() }

func TestDoctorScansTwiceAndReports(t *testing.T) {
	src := replaySources(t)
	counting := &countingProcSource{inner: src.Proc}
	src.Proc = counting

	book, err := pricing.Load()
	if err != nil {
		t.Fatalf("pricing.Load: %v", err)
	}

	var buf bytes.Buffer
	runDoctor(context.Background(), &buf, src, time.Second, book, "wattop-dark", true)
	out := buf.String()

	if counting.scans != 2 {
		t.Errorf("doctor called Scan %d times, want 2 (a baseline and the reported scan)", counting.scans)
	}

	for _, want := range []string{
		"channels:",
		"resolved,",
		"ioreport-groups:",
		"rows with a cpu percentage:",
		"gpu table rows:",
		"sessions:",
		"pricing:",
		"status:",
		"theme: wattop-dark",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output is missing %q\n---\n%s", want, out)
		}
	}
}

func TestDoctorIOReportGroupsSection(t *testing.T) {
	tests := []struct {
		name      string
		enumerate func() ([]soc.Group, bool, error)
		want      []string
		notWant   []string
	}{
		{
			name: "groups with counts",
			enumerate: func() ([]soc.Group, bool, error) {
				return []soc.Group{
					{Name: "AMC Stats", Channels: 223},
					{Name: "Energy Model", Channels: 364},
				}, false, nil
			},
			want: []string{
				"ioreport-groups:",
				"AMC Stats: 223 channels",
				"Energy Model: 364 channels",
				"total: 2 groups, 587 channels",
				"listing: named-group probe",
			},
		},
		{
			name: "wildcard listing is labelled complete",
			enumerate: func() ([]soc.Group, bool, error) {
				return []soc.Group{{Name: "CPU Stats", Channels: 130}}, true, nil
			},
			want:    []string{"CPU Stats: 130 channels", "listing: wildcard channel copy"},
			notWant: []string{"named-group probe"},
		},
		{
			name: "no groups",
			enumerate: func() ([]soc.Group, bool, error) {
				return nil, true, nil
			},
			want:    []string{"(none reported)"},
			notWant: []string{"channels\n"},
		},
		{
			name: "enumeration failed",
			enumerate: func() ([]soc.Group, bool, error) {
				return nil, false, errors.New("boom")
			},
			want:    []string{"unavailable: boom"},
			notWant: []string{"total:"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			printIOReportGroups(&buf, tc.enumerate)
			out := buf.String()
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output is missing %q\n---\n%s", want, out)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(out, notWant) {
					t.Errorf("output unexpectedly contains %q\n---\n%s", notWant, out)
				}
			}
		})
	}
}
