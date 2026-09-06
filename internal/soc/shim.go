//go:build darwin && arm64 && cgo

package soc

import "github.com/jasonm4130/wattop/internal/soc/mactop"

// SocMetrics, SystemInfo and Composite are aliased (not wrapped) so
// sampler_darwin.go can use them by name without importing
// internal/soc/mactop itself.
type (
	SocMetrics = mactop.SocMetrics
	SystemInfo = mactop.SystemInfo
	CPUMetrics = mactop.CPUMetrics
	GPUMetrics = mactop.GPUMetrics
	Composite  = mactop.Composite
)

// Init, Sample, Cleanup, SOCInfo and ThermalState are the five lifecycle
// wrappers the plan names, re-exported under package soc so no file above
// internal/soc ever needs to import internal/soc/mactop directly.
func Init() error                      { return mactop.Init() }
func Sample(durationMs int) SocMetrics { return mactop.Sample(durationMs) }
func Cleanup()                         { mactop.Cleanup() }
func SOCInfo() SystemInfo              { return mactop.SOCInfo() }
func ThermalState() int                { return mactop.ThermalState() }

// SampleAll is the composed sample (see mactop.Composite) sampler_darwin.go
// builds domain.SysSample from.
func SampleAll(durationMs int) Composite { return mactop.SampleAll(durationMs) }

// CoreTypeForIndex labels core index i as "e", "p" or "s" per the reported
// topology, per mactop's coreTypeForIndex.
func CoreTypeForIndex(index int, info SystemInfo) string {
	return mactop.CoreTypeForIndex(index, info)
}

// GPUProcEntry is one process's cumulative GPU time in nanoseconds. This is
// a duplicate of mactop.GPUProcEntry, not an alias: it keeps
// GPUProcessStats's signature free of any mactop type, so callers of this
// package (Task 6's internal/proc) import internal/soc only.
type GPUProcEntry struct {
	PID             int
	CumulativeGPUNs uint64
}

// GPUProcessStats returns the per-pid cumulative GPU time table.
func GPUProcessStats() ([]GPUProcEntry, error) {
	raw, err := mactop.GPUProcessStats()
	if err != nil {
		return nil, err
	}
	out := make([]GPUProcEntry, len(raw))
	for i, e := range raw {
		out[i] = GPUProcEntry{PID: e.PID, CumulativeGPUNs: e.CumulativeGPUNs}
	}
	return out, nil
}
