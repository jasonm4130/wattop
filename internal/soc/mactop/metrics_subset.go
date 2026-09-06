//go:build darwin && arm64 && cgo

// Copyright (c) 2024-2026 Carsen Klock under MIT License
//
// metrics_subset.go is a partial extraction from upstream metrics.go — see
// VENDOR.md. The nine collector functions the plan names are copied
// verbatim (normalizeSocMetricsPower, cpuMetricsFromSoc, gpuMetricsFromSoc,
// aneUtilizationPercent, calculateCoreAveragesForSystem, coreTypeForIndex,
// getMemoryMetrics, getNetDiskMetrics, GetCPUPercentages), plus two small
// pure helpers those nine call internally (averageCPUUsage,
// averageCoreRange) that the plan's list omitted but the closure requires.
// Everything else in upstream metrics.go (Prometheus export, dispatch
// goroutines, process metrics) has UI/exporter coupling this package does
// not need and is intentionally left behind. This is NOT a file-for-file
// vendor copy and vendor-diff.sh treats it as expected-partial.
package mactop

import (
	"math"
	"time"
)

func normalizeSocMetricsPower(m SocMetrics) SocMetrics {
	componentSum := m.TotalPower
	totalPower := m.SystemPower
	if totalPower < componentSum {
		totalPower = componentSum
	}
	m.SystemPower = totalPower - componentSum
	m.TotalPower = totalPower
	return m
}

func cpuMetricsFromSoc(m SocMetrics, coreUsages []float64, avgUsage float64, throttled bool) CPUMetrics {
	return CPUMetrics{
		CPUW:            m.CPUPower,
		GPUW:            m.GPUPower,
		ANEW:            m.ANEPower,
		ANEActive:       m.ANEActive,
		ANEReadBW:       m.ANEReadBW,
		ANEWriteBW:      m.ANEWriteBW,
		DRAMW:           m.DRAMPower,
		GPUSRAMW:        m.GPUSRAMPower,
		SystemW:         m.SystemPower,
		PackageW:        m.TotalPower,
		Throttled:       throttled,
		CPUTemp:         float64(m.CPUTemp),
		GPUTemp:         float64(m.GPUTemp),
		SoCTemp:         float64(m.SocTemp),
		EClusterActive:  int(m.EClusterActive),
		PClusterActive:  int(m.PClusterActive),
		EClusterFreqMHz: int(m.EClusterFreqMHz),
		PClusterFreqMHz: int(m.PClusterFreqMHz),
		SClusterActive:  int(m.SClusterActive),
		SClusterFreqMHz: int(m.SClusterFreqMHz),
		DRAMReadBW:      m.DRAMReadBW,
		DRAMWriteBW:     m.DRAMWriteBW,
		DRAMBWCombined:  m.DRAMBWCombined,
		DRAMBWSource:    m.DRAMBWSource,
		ANEBW:           m.ANEBWCombined,
		Fans:            m.Fans,
		TempSensors:     m.TempSensors,
		CoreUsages:      coreUsages,
		AvgUsage:        avgUsage,
	}
}

// aneMaxPowerW is the assumed full-tilt ANE power draw used for the
// power-based utilization estimate (historical mactop behavior).
const aneMaxPowerW = 8.0

// aneBWRefFloorGBs is the minimum bandwidth treated as "100% ANE activity"
// for the bandwidth-based estimate. A saturating conv workload measured
// ~3.5-3.8 GB/s of ANE fabric traffic on M1 Ultra; the reference grows
// adaptively if higher bandwidth is ever observed (maxANEBWSeen).
const aneBWRefFloorGBs = 4.0

// aneUtilizationPercent estimates Neural Engine utilization. It prefers the
// power-based estimate from the Energy Model ANE channel; when that channel
// is dead (macOS 27 beta zeroed all per-block energy counters) but the AMC
// "ANE RD/WR" byte counters show traffic, it falls back to a bandwidth-based
// activity estimate so ANE usage doesn't silently read 0 on newer OSes.

func aneUtilizationPercent(m CPUMetrics) float64 {
	// 1. PMP state-residency utilization (macOS 27+/M5): true time-above-
	//    idle-floor measurement parsed from the ANE-AF-BW / ANE-DCS-BW
	//    channels — the most accurate signal where it exists. Latch the
	//    bandwidth-form label: residency flowing while watts read 0 proves
	//    the energy counter is dead.
	if m.ANEActive > 0 {
		// This machine has a live PMP residency signal (M5-class). Latch it so
		// the history chart plots stored residency percentages as-is rather
		// than re-deriving them from bandwidth (which is a different quantity
		// and would diverge from this gauge).
		aneResidencyLatched.Store(true)
		if m.ANEW <= 0 {
			aneBWModeLatched.Store(true)
		}
		pct := m.ANEActive
		if pct > 100 {
			pct = 100
		}
		return pct
	}
	// 2. Energy Model power estimate (macOS 26 and any OS with working
	//    per-block energy counters).
	if m.ANEW > 0 {
		pct := m.ANEW / aneMaxPowerW * 100
		if pct > 100 {
			pct = 100
		}
		return pct
	}
	// 3. Bandwidth activity estimate (M1-M4 on macOS 27: AMC byte counters).
	if m.ANEBW > 0 {
		// ANE traffic with zero watts proves the energy counter is dead (an
		// idle ANE produces neither). Latch bandwidth mode for the session so
		// the UI label stays in GB/s form even when traffic later drops to 0,
		// instead of reverting to a misleading "@ 0.00 W".
		aneBWModeLatched.Store(true)
		// Monotonic session max via CAS (callers run on several goroutines).
		// 3% ratchet hysteresis: a single burst-aligned sample window
		// marginally above the sustained plateau would otherwise become the
		// permanent 100% reference, pinning genuine saturation at a
		// misleading 96-98%. Bursts within 3% read as 100% via the clamp
		// below; real step-ups beyond 3% still re-scale the reference.
		for {
			cur := math.Float64frombits(maxANEBWSeenBits.Load())
			if m.ANEBW <= cur*1.03 {
				break
			}
			if maxANEBWSeenBits.CompareAndSwap(math.Float64bits(cur), math.Float64bits(m.ANEBW)) {
				break
			}
		}
		ref := max(math.Float64frombits(maxANEBWSeenBits.Load()), aneBWRefFloorGBs)
		pct := m.ANEBW / ref * 100
		if pct > 100 {
			pct = 100
		}
		return pct
	}
	return 0
}

// aneBWLabelMode reports whether ANE displays should use the bandwidth-form
// label (GB/s) instead of watts. True while the power channel yields nothing
// and either traffic is currently flowing or bandwidth mode was latched
// earlier this session. On OSes with a working energy counter (macOS 26) the
// latch never trips, so labels behave exactly as before.

func gpuMetricsFromSoc(m SocMetrics) GPUMetrics {
	return GPUMetrics{
		FreqMHz:       int(m.GPUFreqMHz),
		ActivePercent: m.GPUActive,
		Power:         m.GPUPower + m.GPUSRAMPower,
		Temp:          m.GPUTemp,
	}
}

func averageCPUUsage(coreUsages []float64) float64 {
	if len(coreUsages) == 0 {
		return 0
	}
	total := 0.0
	for _, usage := range coreUsages {
		total += usage
	}
	return total / float64(len(coreUsages))
}

func averageCoreRange(coreUsages []float64, start, count int) float64 {
	if count <= 0 || start < 0 || len(coreUsages) < start+count {
		return 0
	}
	total := 0.0
	for _, usage := range coreUsages[start : start+count] {
		total += usage
	}
	return total / float64(count)
}

func calculateCoreAveragesForSystem(coreUsages []float64, sysInfo SystemInfo) (ecoreAvg, pcoreAvg, scoreAvg float64) {
	ecoreAvg = averageCoreRange(coreUsages, 0, sysInfo.ECoreCount)
	pcoreAvg = averageCoreRange(coreUsages, sysInfo.ECoreCount, sysInfo.PCoreCount)
	scoreAvg = averageCoreRange(coreUsages, sysInfo.ECoreCount+sysInfo.PCoreCount, sysInfo.SCoreCount)
	return ecoreAvg, pcoreAvg, scoreAvg
}

func coreTypeForIndex(index int, sysInfo SystemInfo) string {
	if index < sysInfo.ECoreCount {
		return "e"
	}
	if index < sysInfo.ECoreCount+sysInfo.PCoreCount {
		return "p"
	}
	return "s"
}

// prometheusThermalStateValue maps the pressure level to a numeric gauge value
// matching the OSThermalPressureLevel scale (Nominal=0 .. Sleeping=4). Unknown
// reports 0.

func GetCPUPercentages() ([]float64, error) {
	currentTimes, err := GetCPUUsage()
	if err != nil {
		return nil, err
	}
	if firstRun {
		lastCPUTimes = currentTimes
		firstRun = false
		return make([]float64, len(currentTimes)), nil
	}
	percentages := make([]float64, len(currentTimes))
	for i := range currentTimes {
		totalDelta := (currentTimes[i].User - lastCPUTimes[i].User) +
			(currentTimes[i].System - lastCPUTimes[i].System) +
			(currentTimes[i].Idle - lastCPUTimes[i].Idle) +
			(currentTimes[i].Nice - lastCPUTimes[i].Nice)

		activeDelta := (currentTimes[i].User - lastCPUTimes[i].User) +
			(currentTimes[i].System - lastCPUTimes[i].System) +
			(currentTimes[i].Nice - lastCPUTimes[i].Nice)

		if totalDelta > 0 {
			percentages[i] = (activeDelta / totalDelta) * 100.0
		}
		if percentages[i] < 0 {
			percentages[i] = 0
		} else if percentages[i] > 100 {
			percentages[i] = 100
		}
	}
	lastCPUTimes = currentTimes
	return percentages, nil
}

func getNetDiskMetrics() NetDiskMetrics {
	var metrics NetDiskMetrics

	netDiskMutex.Lock()
	defer netDiskMutex.Unlock()

	now := time.Now()
	elapsed := now.Sub(lastNetDiskTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	// Native Network Metrics
	netMap, err := GetNativeNetworkMetrics()
	if err == nil {
		var totalNet NativeNetMetric
		for _, iface := range netMap {
			totalNet.BytesRecv += iface.BytesRecv
			totalNet.BytesSent += iface.BytesSent
			totalNet.PacketsRecv += iface.PacketsRecv
			totalNet.PacketsSent += iface.PacketsSent
		}

		if lastNetDiskTime.IsZero() {
			lastNetStats = totalNet
		} else {
			metrics.InBytesPerSec = float64(totalNet.BytesRecv-lastNetStats.BytesRecv) / elapsed
			metrics.OutBytesPerSec = float64(totalNet.BytesSent-lastNetStats.BytesSent) / elapsed
			metrics.InPacketsPerSec = float64(totalNet.PacketsRecv-lastNetStats.PacketsRecv) / elapsed
			metrics.OutPacketsPerSec = float64(totalNet.PacketsSent-lastNetStats.PacketsSent) / elapsed
		}
		lastNetStats = totalNet
	}

	// Native Disk Metrics
	diskMap, err := GetNativeDiskMetrics()
	if err == nil {
		var totalDisk NativeDiskMetric
		for _, d := range diskMap {
			totalDisk.ReadBytes += d.ReadBytes
			totalDisk.WriteBytes += d.WriteBytes
			totalDisk.ReadOps += d.ReadOps
			totalDisk.WriteOps += d.WriteOps
		}

		if !lastNetDiskTime.IsZero() {
			metrics.ReadKBytesPerSec = float64(totalDisk.ReadBytes-lastDiskStats.ReadBytes) / elapsed / 1024
			metrics.WriteKBytesPerSec = float64(totalDisk.WriteBytes-lastDiskStats.WriteBytes) / elapsed / 1024
			metrics.ReadOpsPerSec = float64(totalDisk.ReadOps-lastDiskStats.ReadOps) / elapsed
			metrics.WriteOpsPerSec = float64(totalDisk.WriteOps-lastDiskStats.WriteOps) / elapsed
		}
		lastDiskStats = totalDisk
	}

	lastNetDiskTime = now
	return metrics
}

func getMemoryMetrics() MemoryMetrics {
	native, err := GetNativeMemoryMetrics()
	if err != nil {
		stderrLogger.Printf("Error getting native memory metrics: %v\n", err)
		return MemoryMetrics{}
	}
	return MemoryMetrics{
		Total:     native.Total,
		Used:      native.Used,
		Available: native.Available,
		SwapTotal: native.SwapTotal,
		SwapUsed:  native.SwapUsed,
	}
}
