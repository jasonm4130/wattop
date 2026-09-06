//go:build darwin && arm64 && cgo

// Copyright (c) 2024-2026 Carsen Klock under MIT License
//
// adapter.go is authored (not vendored). It is the only file that can reach
// the copied unexported symbols in this directory, because they are
// unexported in package mactop: it exports thin wrappers over
// initSocMetrics/sampleSocMetrics/cleanupSocMetrics/getSOCInfo/
// getSocThermalState/GetGPUProcessStats, and it declares the package-level
// state upstream's globals.go supplied that the copied files reference but
// that this package has no other file for -- see VENDOR.md.
package mactop

import (
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ANE estimate state shared between the sampler (latch writes in
// sampleSocMetrics/aneUtilizationPercent) and any reader goroutine —
// atomics to avoid a data race. Ported verbatim from upstream globals.go.
var (
	maxANEBWSeenBits    atomic.Uint64 // float64 bits; monotonic session max
	aneBWModeLatched    atomic.Bool
	aneResidencyLatched atomic.Bool // M5-class: ANEActive (PMP residency) seen this session
)

// stderrLogger is referenced by metrics_subset.go's getMemoryMetrics.
var stderrLogger = log.New(os.Stderr, "", 0)

// Net/disk delta state referenced by metrics_subset.go's getNetDiskMetrics.
var (
	lastNetStats    NativeNetMetric
	lastDiskStats   NativeDiskMetric
	lastNetDiskTime time.Time
	netDiskMutex    sync.Mutex
)

// Init initializes the IOReport subscription. Passwordless on Apple
// Silicon; returns a descriptive error rather than hanging.
func Init() error {
	return initSocMetrics()
}

// Sample blocks for durationMs inside C sampling IOReport and SMC — the
// full interval is spent there, which is what makes power and bandwidth
// deltas accurate. Callers must never call this from a render path.
func Sample(durationMs int) SocMetrics {
	return sampleSocMetrics(durationMs)
}

// Cleanup tears down the IOReport subscription.
func Cleanup() {
	cleanupSocMetrics()
}

// SOCInfo returns the (cached) static topology/model description.
func SOCInfo() SystemInfo {
	return getSOCInfo()
}

// ThermalState returns the current thermal pressure level.
func ThermalState() int {
	return getSocThermalState()
}

// Composite is the fully composed sample the plan's "reuse selectively"
// functions build from one sampleSocMetrics call, modeled on upstream
// headless.go's collectHeadlessData (not vendored — headless.go is
// UI/exporter output shaping, but its composition order is the correct one
// to replicate: normalize power, take one core-percentage reading, then
// derive CPU/GPU/mem/net-disk metrics from both). SampleAll is the one
// piece of composition adapter.go supplies beyond the plan's five named
// thin wrappers — the nine reused metrics.go functions are unexported in
// this package and this is where they get called.
type Composite struct {
	SoC       SystemInfo
	CPU       CPUMetrics
	GPU       GPUMetrics
	Mem       MemoryMetrics
	NetDisk   NetDiskMetrics
	ANEPct    float64
	Thermal   int
	Throttled bool
	// ECoreAvg/PCoreAvg/SCoreAvg are calculateCoreAveragesForSystem's
	// OS-level (GetCPUPercentages-derived) per-cluster averages — the
	// number sampler_darwin.go uses for domain.Cluster.ActivePct. Distinct
	// from CPU.EClusterActive/PClusterActive/SClusterActive, which are the
	// IOReport hardware-residency numbers.
	ECoreAvg, PCoreAvg, SCoreAvg float64
}

// SampleAll blocks for durationMs (see Sample) and returns the composed
// metrics sampler_darwin.go maps onto domain.SysSample.
func SampleAll(durationMs int) Composite {
	soc := getSOCInfo()
	m := normalizeSocMetricsPower(sampleSocMetrics(durationMs))

	coreUsages, err := GetCPUPercentages()
	if err != nil {
		coreUsages = nil
	}
	avgUsage := averageCPUUsage(coreUsages)
	ecoreAvg, pcoreAvg, scoreAvg := calculateCoreAveragesForSystem(coreUsages, soc)

	thermalLevel := getSocThermalStateLevel()
	throttled := thermalStateThrottled(thermalLevel)

	cpu := cpuMetricsFromSoc(m, coreUsages, avgUsage, throttled)
	gpu := gpuMetricsFromSoc(m)
	anePct := aneUtilizationPercent(cpu)

	return Composite{
		SoC:       soc,
		CPU:       cpu,
		GPU:       gpu,
		Mem:       getMemoryMetrics(),
		NetDisk:   getNetDiskMetrics(),
		ANEPct:    anePct,
		Thermal:   int(thermalLevel),
		Throttled: throttled,
		ECoreAvg:  ecoreAvg,
		PCoreAvg:  pcoreAvg,
		SCoreAvg:  scoreAvg,
	}
}

// CoreTypeForIndex labels core index i as "e", "p" or "s" per the reported
// topology. Thin export of the unexported coreTypeForIndex.
func CoreTypeForIndex(index int, soc SystemInfo) string {
	return coreTypeForIndex(index, soc)
}

// getSocThermalStateLevel wraps sys_info.go's unexported getThermalStateLevel
// so SampleAll can derive Throttled the same way upstream headless.go does,
// without re-deriving it from the already-exported ThermalState() int.
func getSocThermalStateLevel() thermalStateLevel {
	return getThermalStateLevel()
}

// GPUProcEntry is one process's cumulative GPU time, in nanoseconds, as
// reported by mactop's GetGPUProcessStats. Declared here in package mactop
// because that is the only package that can name mactop's unexported GPU
// process table internals; internal/soc.GPUProcEntry is a separate,
// duplicated type so callers above internal/soc never import this package.
type GPUProcEntry struct {
	PID             int
	CumulativeGPUNs uint64
}

// GPUProcessStats wraps mactop's GetGPUProcessStats (native_stats.go),
// converting its map[int]uint64 into a stable-ish slice of entries.
func GPUProcessStats() ([]GPUProcEntry, error) {
	raw := GetGPUProcessStats()
	out := make([]GPUProcEntry, 0, len(raw))
	for pid, ns := range raw {
		out = append(out, GPUProcEntry{PID: pid, CumulativeGPUNs: ns})
	}
	return out, nil
}
