//go:build darwin && arm64 && cgo

package soc

import (
	"context"
	"fmt"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// Sample blocks for the full interval inside C. Callers must own a goroutine
// for it and never call it from a render path.
type Sampler struct{}

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
	src := c.CPU.DRAMBWSource
	return map[string]bool{
		"cpu_power":            true,
		"gpu_power":            true,
		"ane_power":            true,
		"dram_power":           true,
		"system_power":         true,
		"cpu_temp":             true,
		"gpu_temp":             true,
		"soc_temp":             c.CPU.SoCTemp > 0,
		"dram_read_bw_gbs":     src.Directional(),
		"dram_write_bw_gbs":    src.Directional(),
		"dram_bw_combined_gbs": src.Resolved(),
		"ane_bw_combined_gbs":  c.CPU.ANEBW != 0,
		"fans":                 len(c.CPU.Fans) > 0,
	}
}

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
		Throttled:    c.Throttled,
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

func bandwidthFromComposite(c Composite, missing *[]string) domain.Bandwidth {
	var bw domain.Bandwidth
	src := c.CPU.DRAMBWSource

	switch {
	case src.Directional():
		read, write := c.CPU.DRAMReadBW, c.CPU.DRAMWriteBW
		combined := read + write
		bw.DRAMReadGBs = &read
		bw.DRAMWriteGBs = &write
		bw.DRAMCombinedGBs = &combined
	case src.Resolved():
		// mactop halves one combined figure across both byte fields.
		combined := c.CPU.DRAMReadBW + c.CPU.DRAMWriteBW
		bw.DRAMCombinedGBs = &combined
		bw.DRAMEstimated = src.Estimated()
		*missing = append(*missing, "dram_read_bw_gbs", "dram_write_bw_gbs")
	default:
		*missing = append(*missing, "dram_read_bw_gbs", "dram_write_bw_gbs", "dram_bw_combined_gbs")
	}

	// mactop gives ANE bandwidth no presence signal, so zero cannot be told
	// from an absent channel.
	if v := c.CPU.ANEBW; v != 0 {
		bw.ANECombinedGBs = &v
	} else {
		*missing = append(*missing, "ane_bw_combined_gbs")
	}
	return bw
}

// A zero reading means the sensor is absent, so its key is omitted. mactop's
// TempSensors holds ~325 raw keys that must not be published.
func tempsFromComposite(c Composite) map[string]float64 {
	temps := make(map[string]float64, 3)
	for key, v := range map[string]float64{
		"cpu": c.CPU.CPUTemp,
		"gpu": c.CPU.GPUTemp,
		"soc": c.CPU.SoCTemp,
	} {
		if v > 0 {
			temps[key] = v
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
