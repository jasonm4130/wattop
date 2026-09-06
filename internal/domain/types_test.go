package domain

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func fp(v float64) *float64 { return &v }

func fixedTime(offsetSec int) time.Time {
	return time.Date(2026, 9, 6, 12, 0, offsetSec, 0, time.UTC)
}

// roundTrip marshals v to JSON and unmarshals it back into a fresh T.
func roundTrip[T any](t *testing.T, v T) T {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func assertRoundTrip[T any](t *testing.T, name string, v T) {
	t.Helper()
	got := roundTrip(t, v)
	if !reflect.DeepEqual(v, got) {
		t.Errorf("%s: round trip mismatch\n  want: %#v\n  got:  %#v", name, v, got)
	}
}

// --- fully-populated fixtures for every top-level type ---

func fullCluster() Cluster {
	return Cluster{Label: "P", CoreCount: 6, ActivePct: fp(42.5), FreqMHz: fp(3800), CoreActive: []float64{10, 20, 30, 40, 50, 60}}
}

func fullPower() Power {
	return Power{CPUWatts: fp(4.1), GPUWatts: fp(2.2), ANEWatts: fp(0.3), DRAMWatts: fp(1.1), SystemWatts: fp(12.5)}
}

func fullBandwidth() Bandwidth {
	return Bandwidth{DRAMReadGBs: fp(10.1), DRAMWriteGBs: fp(5.2), ANECombinedGBs: fp(0.9)}
}

func fullFan() Fan {
	return Fan{Label: "fan0", RPM: 1800, MinRPM: fp(0), MaxRPM: fp(4200)}
}

func fullMemorySample() MemorySample {
	return MemorySample{TotalBytes: 34359738368, UsedBytes: 12345678, AvailableBytes: 22222222, SwapTotalBytes: 1073741824, SwapUsedBytes: 0}
}

func fullNetSample() NetSample {
	return NetSample{InBytesPerSec: 1024.5, OutBytesPerSec: 512.25}
}

func fullDiskSample() DiskSample {
	return DiskSample{ReadBytesPerSec: 4096, WriteBytesPerSec: 2048}
}

func fullRateLimit() RateLimit {
	return RateLimit{Scope: "five_hour", UsedPct: fp(63.2), WindowMins: 300, ResetsAt: fixedTime(10), Rejected: false}
}

func fullProcSample() ProcSample {
	return ProcSample{
		PID: 407, Comm: "2.1.261", Argv: []string{"claude", "--resume"}, CWD: "/repo",
		RSSBytes: 987654321, CPUPct: 12.3, DiskReadB: 111, DiskWriteB: 222,
		StartTime: fixedTime(1), GPUMsPerSec: fp(20.026), GPUPctApprox: fp(1.6),
	}
}

func fullUsage() Usage {
	return Usage{Input: 100, Output: 200, CacheRead: 300, CacheCreate5m: 400, CacheCreate1h: 500, Thinking: 600, CachedInput: 50}
}

func fullToolCall() ToolCall {
	return ToolCall{Name: "Read", At: fixedTime(2), ID: "tc_1"}
}

func fullSubagent() Subagent {
	return Subagent{
		Hash: "abc123", AgentType: "Explore", Description: "search the repo",
		Model: "claude-opus-5", ToolUseID: "tu_1", SpawnDepth: 1,
		Usage: fullUsage(), Live: true, CostUSD: fp(0.42),
	}
}

func fullSession() Session {
	pid := 407
	return Session{
		Agent: "claude", ID: "sess-1", PID: &pid, BindConf: "exact", CWD: "/repo",
		Name: "wattop work", Cmdline: "claude --resume", Kind: "interactive",
		Status: "busy", StatusSince: fixedTime(3), Model: "claude-opus-5",
		Usage: fullUsage(), CostUSD: fp(1.23), BurnUSDPerHr: fp(4.56), Priced: true,
		ContextUsed: 1000, ContextMax: 200000, ContextExact: false,
		Tools:      []ToolCall{fullToolCall()},
		ToolCounts: map[string]int{"Read": 3},
		Subagents:  []Subagent{fullSubagent()},
		Proc:       procPtr(),
		RateLimits: []RateLimit{fullRateLimit()},
	}
}

func procPtr() *ProcSample {
	p := fullProcSample()
	return &p
}

func fullSysSample() SysSample {
	s := SysSample{
		At: fixedTime(4), SoCName: "Apple M5 Max", Clusters: []Cluster{fullCluster()},
		Power: fullPower(), Bandwidth: fullBandwidth(),
		Temps: map[string]float64{"cpu": 45.5, "gpu": 40.1, "soc": 42.0},
		Fans:  []Fan{fullFan()}, ThermalState: 1, Memory: fullMemorySample(),
		Net: fullNetSample(), Disk: fullDiskSample(), Missing: []string{"dram_read"},
	}
	s.GPU.ActivePct = fp(33.3)
	s.GPU.FreqMHz = fp(1200)
	s.GPU.CoreCount = 40
	return s
}

func fullSnapshot() Snapshot {
	return Snapshot{
		At: fixedTime(5), Sys: fullSysSample(), Sessions: []Session{fullSession()},
		TotalCostUSD: 12.34, TotalBurnUSDPerHr: 5.67, UnpricedModels: []string{"gpt-99"},
		Degraded: []string{"ioreport"}, SelfCPUPct: 0.5,
	}
}

// --- full round trips ---

func TestTypesFullRoundTrip(t *testing.T) {
	assertRoundTrip(t, "Cluster", fullCluster())
	assertRoundTrip(t, "Power", fullPower())
	assertRoundTrip(t, "Bandwidth", fullBandwidth())
	assertRoundTrip(t, "Fan", fullFan())
	assertRoundTrip(t, "MemorySample", fullMemorySample())
	assertRoundTrip(t, "NetSample", fullNetSample())
	assertRoundTrip(t, "DiskSample", fullDiskSample())
	assertRoundTrip(t, "RateLimit", fullRateLimit())
	assertRoundTrip(t, "ProcSample", fullProcSample())
	assertRoundTrip(t, "Usage", fullUsage())
	assertRoundTrip(t, "ToolCall", fullToolCall())
	assertRoundTrip(t, "Subagent", fullSubagent())
	assertRoundTrip(t, "Session", fullSession())
	assertRoundTrip(t, "SysSample", fullSysSample())
	assertRoundTrip(t, "Snapshot", fullSnapshot())
}

// --- nil-optional round trips: absent must never decode as zero ---

func TestNilOptionalsRoundTrip(t *testing.T) {
	t.Run("Cluster", func(t *testing.T) {
		v := Cluster{Label: "E", CoreCount: 4}
		got := roundTrip(t, v)
		if got.ActivePct != nil || got.FreqMHz != nil {
			t.Fatalf("expected nil optionals, got %#v", got)
		}
	})

	t.Run("Power", func(t *testing.T) {
		got := roundTrip(t, Power{})
		if got.CPUWatts != nil || got.GPUWatts != nil || got.ANEWatts != nil || got.DRAMWatts != nil || got.SystemWatts != nil {
			t.Fatalf("expected all nil, got %#v", got)
		}
	})

	t.Run("Bandwidth", func(t *testing.T) {
		got := roundTrip(t, Bandwidth{})
		if got.DRAMReadGBs != nil || got.DRAMWriteGBs != nil || got.ANECombinedGBs != nil {
			t.Fatalf("expected all nil, got %#v", got)
		}
	})

	t.Run("Fan", func(t *testing.T) {
		got := roundTrip(t, Fan{Label: "fan1", RPM: 900})
		if got.MinRPM != nil || got.MaxRPM != nil {
			t.Fatalf("expected nil optionals, got %#v", got)
		}
	})

	t.Run("RateLimit", func(t *testing.T) {
		got := roundTrip(t, RateLimit{Scope: "weekly"})
		if got.UsedPct != nil {
			t.Fatalf("expected nil UsedPct, got %#v", got)
		}
	})

	t.Run("ProcSample", func(t *testing.T) {
		got := roundTrip(t, ProcSample{PID: 1})
		if got.GPUMsPerSec != nil || got.GPUPctApprox != nil {
			t.Fatalf("expected nil optionals, got %#v", got)
		}
	})

	t.Run("Subagent", func(t *testing.T) {
		got := roundTrip(t, Subagent{Hash: "x"})
		if got.CostUSD != nil {
			t.Fatalf("expected nil CostUSD, got %#v", got)
		}
	})

	t.Run("Session", func(t *testing.T) {
		got := roundTrip(t, Session{ID: "s1"})
		if got.PID != nil || got.CostUSD != nil || got.BurnUSDPerHr != nil || got.Proc != nil {
			t.Fatalf("expected nil optionals, got %#v", got)
		}
	})

	t.Run("SysSample", func(t *testing.T) {
		got := roundTrip(t, SysSample{SoCName: "Apple M5 Max"})
		if got.GPU.ActivePct != nil || got.GPU.FreqMHz != nil {
			t.Fatalf("expected nil GPU optionals, got %#v", got)
		}
	})
}

// TestSnapshotJSONKeys proves the JSON tags used by Task 13's `jq` are
// exactly what land on the wire, so a missing/renamed tag fails here rather
// than downstream.
func TestSnapshotJSONKeys(t *testing.T) {
	snap := fullSnapshot()
	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal top level: %v", err)
	}
	if _, ok := raw["sys"]; !ok {
		t.Error("missing top-level key \"sys\"")
	}
	if _, ok := raw["sessions"]; !ok {
		t.Error("missing top-level key \"sessions\"")
	}

	var sys map[string]json.RawMessage
	if err := json.Unmarshal(raw["sys"], &sys); err != nil {
		t.Fatalf("unmarshal sys: %v", err)
	}
	if _, ok := sys["soc_name"]; !ok {
		t.Error("missing \"sys.soc_name\"")
	}
	if _, ok := sys["bandwidth"]; !ok {
		t.Fatal("missing \"sys.bandwidth\"")
	}

	var bandwidth map[string]json.RawMessage
	if err := json.Unmarshal(sys["bandwidth"], &bandwidth); err != nil {
		t.Fatalf("unmarshal sys.bandwidth: %v", err)
	}
	if _, ok := bandwidth["dram_read_gbs"]; !ok {
		t.Error("missing \"sys.bandwidth.dram_read_gbs\"")
	}
}
