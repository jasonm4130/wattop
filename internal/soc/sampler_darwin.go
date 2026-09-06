//go:build darwin && arm64 && cgo

package soc

import (
	"context"
	"fmt"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// Sampler implements domain.Sampler over the vendored mactop collectors.
// Sample blocks for the full requested interval inside C (see
// mactop.Sample's doc comment) — callers must own a goroutine for it and
// must never call it from a render path.
type Sampler struct{}

// NewSampler returns the real, CGO-backed domain.Sampler.
func NewSampler() *Sampler { return &Sampler{} }

func (s *Sampler) Init() error {
	if err := Init(); err != nil {
		return fmt.Errorf("soc: IOReport init failed: %w", err)
	}
	return nil
}

func (s *Sampler) Close() error {
	Cleanup()
	return nil
}

func (s *Sampler) ThermalState() int {
	return ThermalState()
}

func (s *Sampler) Channels() map[string]bool {
	c := SampleAll(50)
	return channelsFromComposite(c)
}

func (s *Sampler) Sample(ctx context.Context, intervalMs int) (domain.SysSample, error) {
	c := SampleAll(intervalMs)
	return sysSampleFromComposite(c), nil
}

func channelsFromComposite(c Composite) map[string]bool {
	return map[string]bool{
		"cpu_power":           true,
		"gpu_power":           true,
		"ane_power":           true,
		"dram_power":          true,
		"system_power":        true,
		"cpu_temp":            true,
		"gpu_temp":            true,
		"dram_read_bw_gbs":    c.CPU.DRAMReadBW != 0,
		"dram_write_bw_gbs":   c.CPU.DRAMWriteBW != 0,
		"ane_bw_combined_gbs": c.CPU.ANEBW != 0,
		"fans":                len(c.CPU.Fans) > 0,
	}
}

// sysSampleFromComposite builds domain.SysSample from one Composite. It
// never emits a hardcoded E/P cluster pair — clusters come from the reported
// topology (SoC.ECoreCount/PCoreCount/SCoreCount), skipping any cluster type
// with zero cores (this M5 Max reports ECoreCount == 0).
func sysSampleFromComposite(c Composite) domain.SysSample {
	var missing []string

	sample := domain.SysSample{
		At:           time.Now(),
		SoCName:      c.SoC.Name,
		Clusters:     clustersFromComposite(c),
		Power:        powerFromComposite(c),
		Bandwidth:    bandwidthFromComposite(c, &missing),
		Temps:        tempsFromComposite(c),
		Fans:         fansFromComposite(c),
		ThermalState: c.Thermal,
		Memory: domain.MemorySample{
			TotalBytes:     c.Mem.Total,
			UsedBytes:      c.Mem.Used,
			AvailableBytes: c.Mem.Available,
			SwapTotalBytes: c.Mem.SwapTotal,
			SwapUsedBytes:  c.Mem.SwapUsed,
		},
		Net: domain.NetSample{
			InBytesPerSec:  c.NetDisk.InBytesPerSec,
			OutBytesPerSec: c.NetDisk.OutBytesPerSec,
		},
		Disk: domain.DiskSample{
			ReadBytesPerSec:  c.NetDisk.ReadKBytesPerSec * 1024,
			WriteBytesPerSec: c.NetDisk.WriteKBytesPerSec * 1024,
		},
	}
	sample.GPU.CoreCount = c.SoC.GPUCoreCount
	activePct := c.GPU.ActivePercent
	sample.GPU.ActivePct = &activePct
	freq := float64(c.GPU.FreqMHz)
	sample.GPU.FreqMHz = &freq
	sample.Missing = missing
	return sample
}

// clustersFromComposite iterates the clusters the topology actually
// reports via CoreTypeForIndex over CPU.CoreUsages, never a hardcoded E/P
// pair. ActivePct comes from calculateCoreAveragesForSystem's OS-level
// averages (ECoreAvg/PCoreAvg/SCoreAvg); FreqMHz from the IOReport
// per-cluster frequency fields.
func clustersFromComposite(c Composite) []domain.Cluster {
	var clusters []domain.Cluster
	soc := c.SoC

	addCluster := func(label string, count int, activePct float64, freqMHz int, start int) {
		if count <= 0 {
			return
		}
		pct := activePct
		freq := float64(freqMHz)
		var coreActive []float64
		for i := start; i < start+count && i < len(c.CPU.CoreUsages); i++ {
			coreActive = append(coreActive, c.CPU.CoreUsages[i])
		}
		clusters = append(clusters, domain.Cluster{
			Label:      label,
			CoreCount:  count,
			ActivePct:  &pct,
			FreqMHz:    &freq,
			CoreActive: coreActive,
		})
	}

	addCluster("E", soc.ECoreCount, c.ECoreAvg, c.CPU.EClusterFreqMHz, 0)
	addCluster("P", soc.PCoreCount, c.PCoreAvg, c.CPU.PClusterFreqMHz, soc.ECoreCount)
	addCluster("S", soc.SCoreCount, c.SCoreAvg, c.CPU.SClusterFreqMHz, soc.ECoreCount+soc.PCoreCount)

	return clusters
}

func powerFromComposite(c Composite) domain.Power {
	cpu, gpu, ane, dram, sys := c.CPU.CPUW, c.GPU.Power, c.CPU.ANEW, c.CPU.DRAMW, c.CPU.SystemW
	return domain.Power{
		CPUWatts:    &cpu,
		GPUWatts:    &gpu,
		ANEWatts:    &ane,
		DRAMWatts:   &dram,
		SystemWatts: &sys,
	}
}

// bandwidthFromComposite reports DRAM and ANE bandwidth. On the hardware
// this was built against (M5 Max), IOReport's byte-counter channels for
// both never resolve and sampleSocMetrics always yields exactly 0.0 GB/s
// (see VENDOR.md / the plan's Decision, deviation 1) — that reading is
// indistinguishable from "channel absent" on this chip, so it is treated
// as unresolved (nil, appended to Missing) rather than rendered as 0.
func bandwidthFromComposite(c Composite, missing *[]string) domain.Bandwidth {
	var bw domain.Bandwidth
	if v := c.CPU.DRAMReadBW; v != 0 {
		bw.DRAMReadGBs = &v
	} else {
		*missing = append(*missing, "dram_read_bw_gbs")
	}
	if v := c.CPU.DRAMWriteBW; v != 0 {
		bw.DRAMWriteGBs = &v
	} else {
		*missing = append(*missing, "dram_write_bw_gbs")
	}
	if v := c.CPU.ANEBW; v != 0 {
		bw.ANECombinedGBs = &v
	} else {
		*missing = append(*missing, "ane_bw_combined_gbs")
	}
	return bw
}

func tempsFromComposite(c Composite) map[string]float64 {
	temps := map[string]float64{
		"cpu": c.CPU.CPUTemp,
		"gpu": c.CPU.GPUTemp,
	}
	for _, s := range c.CPU.TempSensors {
		if s.Key != "" {
			temps[s.Key] = s.Value
		}
	}
	return temps
}

func fansFromComposite(c Composite) []domain.Fan {
	fans := make([]domain.Fan, 0, len(c.CPU.Fans))
	for _, f := range c.CPU.Fans {
		minRPM := float64(f.MinRPM)
		maxRPM := float64(f.MaxRPM)
		fans = append(fans, domain.Fan{
			Label:  f.Name,
			RPM:    float64(f.ActualRPM),
			MinRPM: &minRPM,
			MaxRPM: &maxRPM,
		})
	}
	return fans
}
