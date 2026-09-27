// Package demo generates synthetic, deterministic telemetry for `wattop
// --demo`: an Apple-silicon SoC, a process table and a handful of Claude
// Code and Codex sessions, none of it read from the machine. It exists so
// the README hero GIF and social card can be re-recorded reproducibly
// without exposing anyone's real sessions, paths or spend.
//
// Everything is a function of the seed and the tick count. World never
// reads the wall clock, the environment, the filesystem or the process
// table, and it uses no global random source: each "random" value is a
// hash of (seed, channel, index). Timestamps it emits are offsets from the
// time the caller passes in, so the numbers are identical on every run and
// only the clock-time labels move.
package demo

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// DefaultSeed is the seed `wattop --demo` runs with.
const DefaultSeed uint64 = 0x776174746f70 // "wattop"

// SoCName is the chip the demo claims to be. It is deliberately not the
// machine the project is developed on.
const SoCName = "Apple M4 Pro"

// Chip topology for SoCName: 4 efficiency cores, 10 performance cores and
// a 20-core GPU.
const (
	eCores   = 4
	pCores   = 10
	gpuCores = 20
)

// rateWindow is how far back TokenRate looks, matching the real sources'
// "recorded over 60s".
const rateWindow = 60 * time.Second

// World is the demo's whole simulated machine. The zero value is not
// usable; construct one with New. Methods are safe for concurrent use, but
// the demo is only ever driven from the one sampling goroutine.
type World struct {
	mu   sync.Mutex
	seed uint64
	step time.Duration
	tick int

	winTicks int

	temp     float64 // °C, first-order lag toward the load target
	sessions []*session

	// Per-tick system readings computed by Advance, so Sys, Procs and
	// Sessions called at the same tick agree with each other.
	cpuLoad, gpuLoad float64
	nBusy            int
}

// New returns a World at tick 0. step is the simulated time between ticks
// and should be the sampling interval, so the synthetic token rates and
// burn figures come out in real per-second units.
func New(seed uint64, step time.Duration) *World {
	if step <= 0 {
		step = time.Second
	}
	w := &World{
		seed:     seed,
		step:     step,
		winTicks: max(1, int(rateWindow/step)),
		temp:     54,
	}
	for i, spec := range sessionSpecs() {
		w.sessions = append(w.sessions, newSession(w, i, spec))
	}
	w.computeTick()
	return w
}

// Step returns the simulated time between ticks.
func (w *World) Step() time.Duration { return w.step }

// Tick returns how many times Advance has been called.
func (w *World) Tick() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tick
}

// Advance moves the world forward one tick: statuses flip, tokens accrue,
// temperatures drift.
func (w *World) Advance() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tick++
	w.computeTick()
}

// seconds is simulated elapsed time at tick.
func (w *World) seconds(tick int) float64 {
	return float64(tick) * w.step.Seconds()
}

// ago converts "n ticks before the current one" into a time before at.
func (w *World) ago(at time.Time, ticks int) time.Time {
	return at.Add(-time.Duration(ticks) * w.step)
}

func (w *World) computeTick() {
	t := w.seconds(w.tick)
	dt := w.step.Seconds()

	w.nBusy = 0
	for _, s := range w.sessions {
		s.advance(w, t, dt)
		if s.status == "busy" {
			w.nBusy++
		}
	}

	// Machine load follows how many agents are working, plus its own
	// wander and the occasional build/test burst.
	burst := 0.0
	if b := smooth(w.seed, chCPUBurst, t, 5); b > 0.68 {
		burst = (b - 0.68) * 2.2
	}
	w.cpuLoad = clamp(0.08+0.09*float64(w.nBusy)+0.28*smooth(w.seed, chCPU, t, 6)+burst, 0.04, 0.97)
	gburst := 0.0
	if b := smooth(w.seed, chGPUBurst, t, 7); b > 0.62 {
		gburst = (b - 0.62) * 2.0
	}
	w.gpuLoad = clamp(0.04+0.22*smooth(w.seed, chGPU, t, 4)+gburst, 0.02, 0.95)

	target := 46 + 30*w.cpuLoad + 10*w.gpuLoad
	w.temp += (target - w.temp) * clamp(0.12*dt, 0, 1)
}

// Sys returns the SoC reading for the current tick. At is left zero: the
// reducer stamps every cycle with the loop's own clock.
func (w *World) Sys() domain.SysSample {
	w.mu.Lock()
	defer w.mu.Unlock()

	t := w.seconds(w.tick)
	var s domain.SysSample
	s.SoCName = SoCName

	eAct := clamp(0.22+0.55*w.cpuLoad+0.2*smooth(w.seed, chECluster, t, 3), 0.1, 0.96)
	pAct := w.cpuLoad
	s.Clusters = []domain.Cluster{
		w.cluster("E", eCores, eAct, 1020, 2592, 0, t),
		w.cluster("P", pCores, pAct, 1260, 4512, eCores, t),
	}

	gpuPct := 100 * w.gpuLoad
	gpuFreq := 338 + (1578-338)*math.Sqrt(w.gpuLoad)
	s.GPU.ActivePct = ptr(round1(gpuPct))
	s.GPU.FreqMHz = ptr(math.Round(gpuFreq))
	s.GPU.CoreCount = gpuCores

	cpuW := 1.1 + 17*math.Pow(w.cpuLoad, 1.25)
	gpuW := 0.25 + 11.5*w.gpuLoad
	aneW := 0.0
	if a := smooth(w.seed, chANE, t, 9); a > 0.7 {
		aneW = 0.4 + (a-0.7)*9
	}
	dramW := 0.55 + 1.7*w.cpuLoad + 0.6*w.gpuLoad
	sysW := clamp(cpuW+gpuW+aneW+dramW+2.1, 4, 35)
	s.Power = domain.Power{
		CPUWatts:    ptr(round2(cpuW)),
		GPUWatts:    ptr(round2(gpuW)),
		ANEWatts:    ptr(round2(aneW)),
		DRAMWatts:   ptr(round2(dramW)),
		SystemWatts: ptr(round2(sysW)),
	}

	// This chip, like the real M-series fallback, reports DRAM traffic as
	// one power-derived total rather than read and write separately.
	dram := round1(4 + 38*w.cpuLoad + 22*w.gpuLoad)
	s.Bandwidth = domain.Bandwidth{DRAMCombinedGBs: &dram, DRAMEstimated: true}
	s.Missing = []string{"dram_read_bw_gbs", "dram_write_bw_gbs"}
	if aneW > 0 {
		s.Bandwidth.ANECombinedGBs = ptr(round1(aneW * 2.4))
	} else {
		s.Missing = append(s.Missing, "ane_bw_combined_gbs")
	}

	s.Temps = map[string]float64{
		"cpu": round1(w.temp + 3.5),
		"gpu": round1(w.temp - 2 + 6*w.gpuLoad),
		"soc": round1(w.temp),
	}
	minRPM, maxRPM := 1200.0, 5700.0
	rpm := clamp(minRPM+(w.temp-52)*135, minRPM, maxRPM)
	s.Fans = []domain.Fan{
		{Label: "Left", RPM: math.Round(rpm), MinRPM: ptr(minRPM), MaxRPM: ptr(maxRPM)},
		{Label: "Right", RPM: math.Round(rpm * 1.045), MinRPM: ptr(minRPM), MaxRPM: ptr(maxRPM)},
	}

	const gb = 1e9
	used := 29.5*gb + 4.5*gb*smooth(w.seed, chMem, t, 20) + 1.5*gb*w.cpuLoad
	total := 48 * gb
	s.Memory = domain.MemorySample{
		TotalBytes:     uint64(total),
		UsedBytes:      uint64(used),
		AvailableBytes: uint64(total - used),
		SwapTotalBytes: uint64(3 * gb),
		SwapUsedBytes:  uint64(0.62 * gb),
	}
	s.Net = domain.NetSample{
		InBytesPerSec:  math.Round(40e3 + 3.2e6*math.Pow(smooth(w.seed, chNetIn, t, 3), 3) + 60e3*float64(w.nBusy)),
		OutBytesPerSec: math.Round(18e3 + 0.9e6*math.Pow(smooth(w.seed, chNetOut, t, 4), 3) + 25e3*float64(w.nBusy)),
	}
	s.Disk = domain.DiskSample{
		ReadBytesPerSec:  math.Round(0.3e6 + 60e6*math.Pow(smooth(w.seed, chDiskR, t, 5), 4)),
		WriteBytesPerSec: math.Round(0.6e6 + 28e6*math.Pow(smooth(w.seed, chDiskW, t, 6), 4)),
	}
	return s
}

func (w *World) cluster(label string, cores int, act, fLo, fHi float64, first int, t float64) domain.Cluster {
	perCore := make([]float64, cores)
	sum := 0.0
	for i := range perCore {
		v := clamp(act*(0.55+0.9*smooth(w.seed, chCoreBase+uint64(first+i), t, 2.5)), 0, 1)
		perCore[i] = round1(100 * v)
		sum += perCore[i]
	}
	pct := round1(sum / float64(cores))
	freq := math.Round(fLo + (fHi-fLo)*math.Sqrt(act))
	return domain.Cluster{Label: label, CoreCount: cores, ActivePct: &pct, FreqMHz: &freq, CoreActive: perCore}
}

// Procs returns one process-table row per session, consistent with the
// sessions' own pid, cwd and busy/waiting state. at is the cycle's clock
// time, used only to date each process's start.
func (w *World) Procs(at time.Time) []domain.ProcSample {
	w.mu.Lock()
	defer w.mu.Unlock()

	t := w.seconds(w.tick)
	out := make([]domain.ProcSample, 0, len(w.sessions))
	for i, s := range w.sessions {
		sp := s.spec
		cpu := 0.4 + 1.6*smooth(w.seed, chSessBase+100+uint64(i), t, 3)
		if s.status == "busy" {
			cpu = 7 + 28*smooth(w.seed, chSessBase+100+uint64(i), t, 2)
		}
		rss := sp.rssMB*1e6 + 40e6*smooth(w.seed, chSessBase+200+uint64(i), t, 15) + float64(len(s.tools))*0.15e6
		p := domain.ProcSample{
			PID:        sp.pid,
			Comm:       sp.comm,
			Argv:       append([]string(nil), sp.argv...),
			CWD:        sp.cwd,
			RSSBytes:   uint64(rss),
			CPUPct:     round1(cpu),
			DiskReadB:  uint64(12e6 + 3e4*float64(w.tick)),
			DiskWriteB: uint64(4e6 + 1e4*float64(w.tick)),
			StartTime:  w.ago(at, w.tick).Add(-time.Duration(sp.ageMin * float64(time.Minute))),
		}
		if sp.agent == "claude" {
			g := round1(2 + 18*w.gpuLoad*smooth(w.seed, chSessBase+300+uint64(i), t, 4))
			p.GPUMsPerSec = &g
			pct := round1(g / 10)
			p.GPUPctApprox = &pct
		}
		out = append(out, p)
	}
	return out
}

// SelfProc returns a process row for wattop itself under pid, so the
// footer's self-CPU figure reads like a real run (about 1.5-3% CPU and
// 60 MB resident) instead of 0.0%. The caller supplies pid (the reducer
// keys self on os.Getpid); World itself never looks it up.
func (w *World) SelfProc(pid int, at time.Time) domain.ProcSample {
	w.mu.Lock()
	defer w.mu.Unlock()

	t := w.seconds(w.tick)
	return domain.ProcSample{
		PID:       pid,
		Comm:      "wattop",
		Argv:      []string{"wattop", "--demo"},
		CWD:       "~/code/wattop",
		RSSBytes:  uint64(56e6 + 8e6*smooth(w.seed, chSelf+1, t, 12)),
		CPUPct:    round1(1.5 + 1.5*smooth(w.seed, chSelf, t, 4)),
		StartTime: w.ago(at, w.tick),
	}
}

// Sessions returns every session whose Agent is agent ("claude" or
// "codex"), or all of them when agent is empty. at is the cycle's clock
// time: every timestamp in the result is an offset back from it.
func (w *World) Sessions(agent string, at time.Time) []domain.Session {
	w.mu.Lock()
	defer w.mu.Unlock()

	var out []domain.Session
	for _, s := range w.sessions {
		if agent != "" && s.spec.agent != agent {
			continue
		}
		out = append(out, s.render(w, at))
	}
	return out
}

func ptr(v float64) *float64 { return &v }

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// toolID is a stable, obviously synthetic tool-use id.
func toolID(prefix string, session, n int) string {
	return fmt.Sprintf("%s_demo%02d%05d", prefix, session, n)
}
