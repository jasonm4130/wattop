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

// channelsFromComposite answers `wattop doctor`: which channels this machine
// resolves. Resolution is a question about the *source*, never about the
// value -- a DRAM counter that is alive and counted zero bytes is resolved,
// and reporting it unresolved is what made doctor claim DRAM bandwidth was
// permanently dead on hardware where it is not (QA 2026-09-06 §2).
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

// bandwidthFromComposite reports DRAM and ANE bandwidth, and it is the one
// place that decides what "unresolved" means for these channels.
//
// Unresolved means no source produced a figure -- mactop.DRAMBWNone. It does
// NOT mean the figure was zero: on this hardware DRAM traffic really does
// fall to a rounded 0.0 GB/s at idle and climb under memory load, so
// dashing an exact 0.0 threw away a real measurement (QA 2026-09-06 §2).
//
// Direction is reported only when it was measured. mactop's byte fields
// always carry two numbers, but on a combined counter (or on the
// DRAM-power-derived estimate this M5 Max falls back to) those two numbers
// are one figure halved -- which is why dram_read_gbs and dram_write_gbs
// used to come out identical to the last digit. Those cases publish the
// total in DRAMCombinedGBs and leave read and write unresolved rather than
// printing one measurement twice.
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
		// One combined figure. mactop split it across the two byte fields to
		// fill the struct; add them back rather than reporting either half as
		// a direction.
		combined := c.CPU.DRAMReadBW + c.CPU.DRAMWriteBW
		bw.DRAMCombinedGBs = &combined
		bw.DRAMEstimated = src.Estimated()
		*missing = append(*missing, "dram_read_bw_gbs", "dram_write_bw_gbs")
	default:
		*missing = append(*missing, "dram_read_bw_gbs", "dram_write_bw_gbs", "dram_bw_combined_gbs")
	}

	// ANE bandwidth has no source flag of its own: mactop reports it only as
	// a byte total with no channel-presence signal, so an exact zero stays
	// indistinguishable from an absent channel here and is reported
	// unresolved. Narrower than the DRAM rule above, and deliberately so --
	// see docs/limitations.md.
	if v := c.CPU.ANEBW; v != 0 {
		bw.ANECombinedGBs = &v
	} else {
		*missing = append(*missing, "ane_bw_combined_gbs")
	}
	return bw
}

// tempsFromComposite publishes the three aggregated temperatures the domain
// contract names -- cpu, gpu and soc -- and nothing else.
//
// mactop's CPU.TempSensors carries every raw SMC and HID sensor the machine
// exposes (325 keys on this M5 Max: TAOL, TB0T, Tg5q, ...). Folding those
// into Temps put all 325 through the SoC panel's Temp row and clipped the
// frame (QA 2026-09-06 §2/§3). They are diagnostics, not a metric: mactop's
// DumpAllSMCTemps still prints them, and the panel never should.
//
// A sensor that read zero is absent, not 0.0 °C, so its key is omitted --
// the panel renders a missing key as a dash.
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
