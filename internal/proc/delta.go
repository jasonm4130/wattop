// Package proc scans the local process table for per-pid CPU, RSS, disk and
// GPU usage. delta.go holds every pure arithmetic helper: no syscalls, no
// build tag, so it compiles and its tests run on any platform including
// Linux CI.
package proc

import "time"

// CPUPct computes Δcpu_ns / Δwall_ns × 100 from two cumulative CPU-time
// samples (proc_pidinfo PROC_PIDTASKINFO's pti_total_user+pti_total_system)
// taken wall apart. It is never a lifetime average: callers must pass the
// previous sample from the last scan, not a value from process start.
//
// Guards against a non-positive wall duration and against a wrapped or
// reset counter (curNs < prevNs, e.g. the pid was reused) by returning 0
// rather than an infinite or negative percentage.
func CPUPct(prevNs, curNs uint64, wall time.Duration) float64 {
	if wall <= 0 || curNs < prevNs {
		return 0
	}
	return float64(curNs-prevNs) / float64(wall.Nanoseconds()) * 100
}

// cpuSample is one pid's cumulative-CPU-ns reading, anchored to the wall
// time it was taken.
type cpuSample struct {
	ns uint64
	at time.Time
}

// CPUTracker keeps the previous cumulative CPU-ns sample per pid so the
// scanner can report a real delta rather than a lifetime average. The first
// time a pid is seen, Update reports ok=false: there is no baseline yet, so
// no percentage is reported at all — this is what "the first scan produces
// no CPU percentage" means structurally, rather than reporting 0% (which is
// indistinguishable from a genuinely idle process).
type CPUTracker struct {
	prev map[int]cpuSample
}

// NewCPUTracker returns an empty tracker, ready for the first scan.
func NewCPUTracker() *CPUTracker {
	return &CPUTracker{prev: make(map[int]cpuSample)}
}

// Update records pid's new cumulative CPU-ns sample taken at now and
// returns the CPU percentage since the pid's previous sample. ok is false
// on the pid's first appearance in this tracker (no baseline yet).
func (t *CPUTracker) Update(pid int, ns uint64, now time.Time) (pct float64, ok bool) {
	prev, seen := t.prev[pid]
	t.prev[pid] = cpuSample{ns: ns, at: now}
	if !seen {
		return 0, false
	}
	return CPUPct(prev.ns, ns, now.Sub(prev.at)), true
}

// Prune drops the baseline for every pid not in alive, e.g. once the
// scanner notices they have exited — without this the tracker's map grows
// without bound on a long-running dashboard.
func (t *CPUTracker) Prune(alive map[int]bool) {
	for pid := range t.prev {
		if !alive[pid] {
			delete(t.prev, pid)
		}
	}
}

// gpuSample is one pid's cumulative GPU-ns reading, anchored to the wall
// time it was taken.
type gpuSample struct {
	ns uint64
	at time.Time
}

// GPUTracker keeps the previous cumulative GPU-ns sample per pid (from
// soc.GPUProcessStats, converted to a plain map by gpu_darwin.go so this
// file never imports the cgo-tagged soc package) and turns two samples
// taken ≥1s apart into a ms/sec delta. Never a lifetime average, same
// reasoning as CPUTracker.
type GPUTracker struct {
	prev map[int]gpuSample
}

// NewGPUTracker returns an empty tracker, ready for the first scan.
func NewGPUTracker() *GPUTracker {
	return &GPUTracker{prev: make(map[int]gpuSample)}
}

// Update takes this scan's cumulative per-pid GPU-ns table and returns
// ms/sec deltas for pids seen in a previous scan. A pid with no prior
// sample establishes its baseline and is omitted from the result.
func (t *GPUTracker) Update(cur map[int]uint64, now time.Time) map[int]float64 {
	out := make(map[int]float64, len(cur))
	for pid, ns := range cur {
		prev, seen := t.prev[pid]
		t.prev[pid] = gpuSample{ns: ns, at: now}
		if !seen {
			continue
		}
		elapsed := now.Sub(prev.at)
		if elapsed <= 0 || ns < prev.ns {
			continue
		}
		out[pid] = float64(ns-prev.ns) / 1e6 / elapsed.Seconds()
	}
	return out
}

// Prune drops the baseline for every pid not in alive.
func (t *GPUTracker) Prune(alive map[int]bool) {
	for pid := range t.prev {
		if !alive[pid] {
			delete(t.prev, pid)
		}
	}
}

// RescaleGPUPct turns a set of per-pid GPU ms/sec deltas into
// domain.ProcSample.GPUPctApprox: an approximate share of the system-wide
// GPU-busy percentage, derived by proportion — never independently
// verified, and never a sort key (see the type comment on GPUPctApprox).
//
// If systemGPUActivePct is nil (the channel didn't resolve this scan) or
// the ms/sec values sum to zero, every pid gets a nil result back rather
// than a value produced by dividing by a stand-in.
func RescaleGPUPct(msPerSec map[int]float64, systemGPUActivePct *float64) map[int]*float64 {
	out := make(map[int]*float64, len(msPerSec))
	if systemGPUActivePct == nil {
		for pid := range msPerSec {
			out[pid] = nil
		}
		return out
	}
	var total float64
	for _, v := range msPerSec {
		total += v
	}
	if total <= 0 {
		for pid := range msPerSec {
			out[pid] = nil
		}
		return out
	}
	for pid, v := range msPerSec {
		pct := v / total * *systemGPUActivePct
		out[pid] = &pct
	}
	return out
}

// StartWall converts a Mach start-abstime to a wall-clock instant using an
// anchor pair (nowTicks, nowWall) read adjacently in the same scan.
// numer/denom come from mach_timebase_info. Returns the zero time when
// startTicks is in the future relative to nowTicks (a nonsense or garbage
// reading) — callers must treat that as "unmatched", never bind on it.
//
// ri_proc_start_abstime is a Mach absolute *tick count* on a monotonic
// clock with an arbitrary origin (usually boot), never an epoch. Only the
// elapsed *duration* since that reading — derived via the timebase, then
// subtracted from an adjacently-read wall clock — is meaningful; the raw
// tick count multiplied by the timebase and read as a Unix epoch lands
// decades away from reality (see TestStartWall's negative case).
func StartWall(startTicks, nowTicks uint64, numer, denom uint32, nowWall time.Time) time.Time {
	if startTicks > nowTicks {
		return time.Time{}
	}
	elapsedTicks := nowTicks - startTicks
	elapsedNs := elapsedTicks * uint64(numer) / uint64(denom)
	return nowWall.Add(-time.Duration(elapsedNs))
}
