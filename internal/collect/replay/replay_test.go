package replay

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/fixture"
)

func socDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(fixture.CorpusDir(), "soc")
}

// TestCorpusPathResolves is the cross-package half of Task 3's CorpusDir()
// check: a package other than internal/fixture itself, whose go test working
// directory is internal/collect/replay, must still resolve and open a
// corpus file.
func TestCorpusPathResolves(t *testing.T) {
	sampler, err := NewSysSampler(socDir(t))
	if err != nil {
		t.Fatalf("NewSysSampler over fixture.CorpusDir()/soc: %v", err)
	}
	sample, err := sampler.Sample(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	if sample.SoCName == "" {
		t.Fatal("expected a non-empty SoCName from the resolved corpus")
	}
}

func assertPlausible(t *testing.T, sample domain.SysSample) {
	t.Helper()
	if sample.SoCName == "" {
		t.Error("SoCName is empty")
	}
	if len(sample.Clusters) < 2 {
		t.Errorf("expected at least two clusters, got %d", len(sample.Clusters))
	}
	if sample.Power.CPUWatts == nil {
		t.Error("Power.CPUWatts is nil, want a value")
	}
}

// TestBothFramings proves the sampler decodes NDJSON ('{'-first) and
// JSON-array ('['-first) captures to the same plausible shape.
func TestBothFramings(t *testing.T) {
	t.Run("ndjson", func(t *testing.T) {
		dir := t.TempDir()
		copyFixture(t, filepath.Join(socDir(t), "mactop-ndjson.jsonl"), filepath.Join(dir, "mactop-ndjson.jsonl"))
		sampler, err := NewSysSampler(dir)
		if err != nil {
			t.Fatalf("NewSysSampler: %v", err)
		}
		sample, err := sampler.Sample(context.Background(), 1000)
		if err != nil {
			t.Fatalf("Sample: %v", err)
		}
		assertPlausible(t, sample)
	})

	t.Run("array", func(t *testing.T) {
		dir := t.TempDir()
		copyFixture(t, filepath.Join(socDir(t), "mactop-array.json"), filepath.Join(dir, "mactop-array.json"))
		sampler, err := NewSysSampler(dir)
		if err != nil {
			t.Fatalf("NewSysSampler: %v", err)
		}
		sample, err := sampler.Sample(context.Background(), 1000)
		if err != nil {
			t.Fatalf("Sample: %v", err)
		}
		assertPlausible(t, sample)
	})
}

// TestDegradedFixtureRendersNil is the version-drift regression test: a
// capture missing the DRAM-bandwidth channel and the fans array must decode
// to nil/empty fields and a populated Missing list, never panic.
func TestDegradedFixtureRendersNil(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, filepath.Join(socDir(t), "mactop-degraded.jsonl"), filepath.Join(dir, "mactop-degraded.jsonl"))

	sampler, err := NewSysSampler(dir)
	if err != nil {
		t.Fatalf("NewSysSampler: %v", err)
	}

	sample, err := sampler.Sample(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Sample panicked or errored: %v", err)
	}

	if sample.Bandwidth.DRAMReadGBs != nil {
		t.Errorf("DRAMReadGBs = %v, want nil", *sample.Bandwidth.DRAMReadGBs)
	}
	if sample.Bandwidth.DRAMWriteGBs != nil {
		t.Errorf("DRAMWriteGBs = %v, want nil", *sample.Bandwidth.DRAMWriteGBs)
	}
	if len(sample.Fans) != 0 {
		t.Errorf("len(Fans) = %d, want 0", len(sample.Fans))
	}

	want := []string{"ane_active", "dram_bw_combined_gbs", "dram_read_bw_gbs", "dram_write_bw_gbs", "fans"}
	for _, name := range want {
		if !contains(sample.Missing, name) {
			t.Errorf("Missing = %v, want it to contain %q", sample.Missing, name)
		}
	}

	channels := sampler.Channels()
	if channels["fans"] {
		t.Error("Channels()[\"fans\"] = true, want false on the degraded fixture")
	}
	if channels["dram_read_bw_gbs"] {
		t.Error("Channels()[\"dram_read_bw_gbs\"] = true, want false on the degraded fixture")
	}
	// ane_bw_combined_gbs was NOT deleted from the degraded fixture.
	if !channels["ane_bw_combined_gbs"] {
		t.Error("Channels()[\"ane_bw_combined_gbs\"] = false, want true (this key survives in the degraded fixture)")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func copyFixture(t *testing.T, src, dst string) {
	t.Helper()
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, raw, 0o644); err != nil {
		t.Fatalf("writing temp fixture %s: %v", dst, err)
	}
}

// TestSysSamplerLoops proves Sample() walks the whole recorded sequence and
// then loops rather than erroring once exhausted.
func TestSysSamplerLoops(t *testing.T) {
	sampler, err := NewSysSampler(socDir(t))
	if err != nil {
		t.Fatalf("NewSysSampler: %v", err)
	}
	n := len(sampler.samples)
	if n < 2 {
		t.Fatalf("expected the corpus to decode to more than one record, got %d", n)
	}
	first, err := sampler.Sample(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Sample: %v", err)
	}
	for i := 1; i < n; i++ {
		if _, err := sampler.Sample(context.Background(), 1000); err != nil {
			t.Fatalf("Sample at index %d: %v", i, err)
		}
	}
	looped, err := sampler.Sample(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Sample after wraparound: %v", err)
	}
	if !looped.At.Equal(first.At) {
		t.Errorf("after looping, sample .At = %v, want the first record's %v", looped.At, first.At)
	}
}

// TestProcSourceScansAndLoops exercises the scripted proc replay: a pid
// that appears then vanishes, one GPUMsPerSec nil and one non-nil, a
// codex-exec-shaped process, and looping.
func TestProcSourceScansAndLoops(t *testing.T) {
	path := filepath.Join(fixture.CorpusDir(), "replay", "procs.json")
	source, err := NewProcSource(path)
	if err != nil {
		t.Fatalf("NewProcSource: %v", err)
	}

	scan1, err := source.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan 1: %v", err)
	}
	if len(scan1) < 3 {
		t.Fatalf("expected at least 3 procs in scan 1, got %d", len(scan1))
	}

	var sawNilGPU, sawNonNilGPU, sawCodexExec bool
	for _, p := range scan1 {
		if p.GPUMsPerSec == nil {
			sawNilGPU = true
		} else {
			sawNonNilGPU = true
		}
		if len(p.Argv) >= 2 && p.Argv[0] == "codex" && p.Argv[1] == "exec" {
			sawCodexExec = true
		}
	}
	if !sawNilGPU {
		t.Error("expected at least one proc with GPUMsPerSec == nil in scan 1")
	}
	if !sawNonNilGPU {
		t.Error("expected at least one proc with GPUMsPerSec != nil in scan 1")
	}
	if !sawCodexExec {
		t.Error("expected a codex-exec-shaped process (argv[0]==\"codex\", argv[1]==\"exec\") in scan 1")
	}

	pid9012InScan1 := false
	for _, p := range scan1 {
		if p.PID == 9012 {
			pid9012InScan1 = true
		}
	}
	if !pid9012InScan1 {
		t.Fatal("expected pid 9012 in scan 1 (it must vanish by a later scan)")
	}

	scan2, err := source.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan 2: %v", err)
	}
	for _, p := range scan2 {
		if p.PID == 9012 {
			t.Error("pid 9012 is still present in scan 2, expected it to have vanished")
		}
	}

	if _, err := source.Scan(context.Background()); err != nil {
		t.Fatalf("Scan 3: %v", err)
	}
	looped, err := source.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan 4 (wraparound): %v", err)
	}
	if len(looped) != len(scan1) {
		t.Errorf("after looping, scan length = %d, want %d (scan 1's length)", len(looped), len(scan1))
	}
}

// TestAgentSourceScriptedSessions exercises the scripted session replay:
// the required scenarios, and the present-then-dropped lifecycle Task 10's
// reducer implements.
func TestAgentSourceScriptedSessions(t *testing.T) {
	dir := filepath.Join(fixture.CorpusDir(), "replay")
	source, err := NewAgentSource(dir)
	if err != nil {
		t.Fatalf("NewAgentSource: %v", err)
	}
	if source.Name() == "" {
		t.Error("Name() is empty")
	}

	now := time.Date(2026, 9, 6, 9, 30, 0, 0, time.UTC)

	poll1, err := source.Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatalf("Poll 1: %v", err)
	}

	byID := func(sessions []domain.Session, id string) *domain.Session {
		for i := range sessions {
			if sessions[i].ID == id {
				return &sessions[i]
			}
		}
		return nil
	}

	busy := byID(poll1, "sess-busy-claude")
	if busy == nil || busy.Status != "busy" || busy.Agent != "claude" {
		t.Errorf("expected a busy claude session in poll 1, got %+v", busy)
	}

	waitingCodex := byID(poll1, "sess-waiting-codex")
	if waitingCodex == nil || waitingCodex.Status != "waiting" || waitingCodex.Agent != "codex" {
		t.Errorf("expected a waiting codex session in poll 1, got %+v", waitingCodex)
	}

	unbound := byID(poll1, "sess-unbound-rollout")
	if unbound == nil || unbound.PID != nil || unbound.BindConf != "unknown" {
		t.Errorf("expected an unbound rollout (PID nil, BindConf unknown) in poll 1, got %+v", unbound)
	}

	unpriced := byID(poll1, "sess-unpriced-model")
	if unpriced == nil || unpriced.Priced || unpriced.CostUSD != nil {
		t.Errorf("expected an unpriced session with nil CostUSD in poll 1, got %+v", unpriced)
	}

	threeSubagents := byID(poll1, "sess-three-subagents")
	if threeSubagents == nil || len(threeSubagents.Subagents) != 3 {
		t.Fatalf("expected a session with 3 subagents in poll 1, got %+v", threeSubagents)
	}
	liveCount := 0
	for _, sa := range threeSubagents.Subagents {
		if sa.Live {
			liveCount++
		}
	}
	if liveCount != 1 {
		t.Errorf("expected exactly 1 live subagent, got %d", liveCount)
	}

	if byID(poll1, "sess-goes-stale-then-drops") == nil {
		t.Fatal("expected sess-goes-stale-then-drops present in poll 1")
	}

	poll2, err := source.Poll(context.Background(), now.Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("Poll 2: %v", err)
	}
	if byID(poll2, "sess-goes-stale-then-drops") == nil {
		t.Fatal("expected sess-goes-stale-then-drops still present in poll 2")
	}

	poll3, err := source.Poll(context.Background(), now.Add(2*time.Minute), nil)
	if err != nil {
		t.Fatalf("Poll 3: %v", err)
	}
	if byID(poll3, "sess-goes-stale-then-drops") != nil {
		t.Error("expected sess-goes-stale-then-drops to be absent by poll 3")
	}
}

// TestVirtualClock exercises the Clock seam: Now() is stable until Advance()
// moves it, deterministically and instantly.
func TestVirtualClock(t *testing.T) {
	start := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	clock := NewVirtualClock(start)
	if !clock.Now().Equal(start) {
		t.Fatalf("Now() = %v, want %v", clock.Now(), start)
	}
	clock.Advance(5 * time.Minute)
	want := start.Add(5 * time.Minute)
	if !clock.Now().Equal(want) {
		t.Fatalf("after Advance, Now() = %v, want %v", clock.Now(), want)
	}
}

// Compile-time assertions that the replay collectors satisfy the domain
// interfaces they're standing in for.
var (
	_ domain.Sampler     = (*SysSampler)(nil)
	_ domain.ProcSource  = (*ProcSource)(nil)
	_ domain.AgentSource = (*AgentSource)(nil)
	_ Clock              = (*VirtualClock)(nil)
)
