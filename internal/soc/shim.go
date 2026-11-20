//go:build darwin && arm64 && cgo

package soc

import "github.com/jasonm4130/wattop/internal/soc/mactop"

type (
	SocMetrics   = mactop.SocMetrics
	SystemInfo   = mactop.SystemInfo
	CPUMetrics   = mactop.CPUMetrics
	GPUMetrics   = mactop.GPUMetrics
	Composite    = mactop.Composite
	TempSensor   = mactop.TempSensor
	DRAMBWSource = mactop.DRAMBWSource
)

const (
	DRAMBWNone            = mactop.DRAMBWNone
	DRAMBWDirectional     = mactop.DRAMBWDirectional
	DRAMBWCombinedCounter = mactop.DRAMBWCombinedCounter
	DRAMBWEstimated       = mactop.DRAMBWEstimated
)

func Init() error                      { return mactop.Init() }
func Sample(durationMs int) SocMetrics { return mactop.Sample(durationMs) }
func Cleanup()                         { mactop.Cleanup() }
func SOCInfo() SystemInfo              { return mactop.SOCInfo() }
func ThermalState() int                { return mactop.ThermalState() }

func SampleAll(durationMs int) Composite { return mactop.SampleAll(durationMs) }

func CoreTypeForIndex(index int, info SystemInfo) string {
	return mactop.CoreTypeForIndex(index, info)
}

type GPUProcEntry struct {
	PID             int
	CumulativeGPUNs uint64
}

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
