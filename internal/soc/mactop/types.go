//go:build darwin && arm64 && cgo

package mactop

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

type CPUUsage struct {
	User   float64
	System float64
	Idle   float64
	Nice   float64
}

type CPUMetrics struct {
	EClusterActive, EClusterFreqMHz, PClusterActive, PClusterFreqMHz int
	SClusterActive, SClusterFreqMHz                                  int
	ECores, PCores, SCores                                           []int
	CoreMetrics                                                      map[string]int
	ANEW, CPUW, GPUW, DRAMW, GPUSRAMW, PackageW, SystemW             float64
	ANEActive                                                        float64
	ANEReadBW                                                        float64
	ANEWriteBW                                                       float64
	CoreUsages                                                       []float64
	AvgUsage                                                         float64
	Throttled                                                        bool
	CPUTemp                                                          float64
	GPUTemp                                                          float64
	DRAMReadBW                                                       float64
	DRAMWriteBW                                                      float64
	DRAMBWCombined                                                   float64
	ANEBW                                                            float64
	Fans                                                             []FanInfo
	TempSensors                                                      []TempSensor
}

type SystemInfo struct {
	Name         string `json:"name"`
	CoreCount    int    `json:"core_count"`
	ECoreCount   int    `json:"e_core_count,omitempty"`
	PCoreCount   int    `json:"p_core_count"`
	SCoreCount   int    `json:"s_core_count,omitempty"`
	GPUCoreCount int    `json:"gpu_core_count"`
}

type NetDiskMetrics struct {
	OutPacketsPerSec  float64 `json:"out_packets_per_sec"`
	OutBytesPerSec    float64 `json:"out_bytes_per_sec"`
	InPacketsPerSec   float64 `json:"in_packets_per_sec"`
	InBytesPerSec     float64 `json:"in_bytes_per_sec"`
	ReadOpsPerSec     float64 `json:"read_ops_per_sec"`
	WriteOpsPerSec    float64 `json:"write_ops_per_sec"`
	ReadKBytesPerSec  float64 `json:"read_kbytes_per_sec"`
	WriteKBytesPerSec float64 `json:"write_kbytes_per_sec"`
}

type GPUMetrics struct {
	FreqMHz       int
	ActivePercent float64
	EffectiveLoad float64 // Frequency-adjusted load: Active% * (current / max)
	Power         float64
	Temp          float32
}

type ProcessMetrics struct {
	PID                                      int
	CPU, LastTime, Memory, GPU               float64 // GPU is ms/s of GPU time
	VSZ, RSS                                 int64
	User, TTY, State, Started, Time, Command string
	LastUpdated                              time.Time
}

type MemoryMetrics struct {
	Total     uint64 `json:"total"`
	Used      uint64 `json:"used"`
	Available uint64 `json:"available"`
	SwapTotal uint64 `json:"swap_total"`
	SwapUsed  uint64 `json:"swap_used"`
}

type EventThrottler struct {
	pending     atomic.Bool
	gracePeriod time.Duration
	C           chan struct{}
}

func NewEventThrottler(gracePeriod time.Duration) *EventThrottler {
	return &EventThrottler{
		gracePeriod: gracePeriod,
		C:           make(chan struct{}, 1),
	}
}

func NewCPUMetrics() CPUMetrics {
	return CPUMetrics{
		CoreMetrics: make(map[string]int),
		ECores:      make([]int, 0),
		PCores:      make([]int, 0),
		SCores:      make([]int, 0),
	}
}

func (e *EventThrottler) Notify() {
	// CAS so only one grace window can be armed at a time: the previous
	// unsynchronized timer-pointer check raced with the callback goroutine
	// (caught by go test -race) and could double-arm on concurrent first
	// notifications. Clearing pending before the send preserves the old
	// ordering: a Notify arriving between the two re-arms a fresh window.
	if !e.pending.CompareAndSwap(false, true) {
		return
	}

	time.AfterFunc(e.gracePeriod, func() {
		e.pending.Store(false)
		select {
		case e.C <- struct{}{}:
		default:
		}
	})
}

// FormatCoreSummary builds a dynamic core summary string like "(6E/4P)" or "(12P/6S)"
// Only includes core types that have non-zero counts.
func FormatCoreSummary(eCount, pCount, sCount int) string {
	var parts []string
	if eCount > 0 {
		parts = append(parts, fmt.Sprintf("%dE", eCount))
	}
	if pCount > 0 {
		parts = append(parts, fmt.Sprintf("%dP", pCount))
	}
	if sCount > 0 {
		parts = append(parts, fmt.Sprintf("%dS", sCount))
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, "/") + ")"
}
