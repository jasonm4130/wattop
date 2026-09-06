// Package proc scans the local process table for per-pid CPU, RSS, disk and
// GPU usage. delta.go holds every pure arithmetic helper: no syscalls, no
// build tag, so it compiles and its tests run on any platform including
// Linux CI.
package proc

import "time"

// CPUPct computes Δcpu_ns / Δwall_ns × 100 from two cumulative CPU-time
// samples taken wall apart. Both arguments must already be nanoseconds:
// proc_pidinfo(PROC_PIDTASKINFO)'s pti_total_user+pti_total_system are Mach
// absolute ticks and must be passed through MachTicksToNs first, or every
// percentage reads ~41.7x low on Apple Silicon.
//
// It is never a lifetime average: callers must pass the previous sample
// from the last scan, not a value from process start.
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

// MachTicksToNs converts a Mach absolute-time tick count into nanoseconds
// using the timebase (numer/denom) from mach_timebase_info. On Apple
// Silicon the timebase is 125/3, so a tick is ~41.67ns and treating raw
// ticks as nanoseconds understates every duration by ~41.7x.
//
// One counter in this package is a tick count rather than a duration and
// must come through here: proc_pidinfo(PROC_PIDTASKINFO)'s pti_total_user
// + pti_total_system (filled from task_absolutetime_info, which reports
// Mach units despite the plain "total_user" naming — a `yes` process pinned
// to one core measured 2.39% before this conversion existed and 99.66%
// after, against ps's 95.9% for the same pid). proc_pid_rusage's
// ri_proc_start_abstime is also a tick count but is no longer read at all;
// StartWall's doc comment explains why no timebase conversion of it yields
// a wall-clock start.
//
// The division is done as quotient-plus-remainder so a large tick count
// cannot overflow uint64 on the multiply. A zero denominator (a failed
// mach_timebase_info) returns 0 rather than panicking; callers treat that
// as "no reading".
func MachTicksToNs(ticks uint64, numer, denom uint32) uint64 {
	if denom == 0 {
		return 0
	}
	q, r := ticks/uint64(denom), ticks%uint64(denom)
	return q*uint64(numer) + r*uint64(numer)/uint64(denom)
}

// ProcStartWall converts a kinfo_proc p_starttime — microseconds since the
// Unix epoch, stamped from the wall clock at exec and the same field `ps`
// renders as lstart — into a time.Time. A non-positive value means the
// kernel gave no start time; it returns the zero time.Time, which Task 9
// must treat as "unmatched" rather than binding a rollout on it.
//
// This, not StartWall, is what the darwin scanner populates
// domain.ProcSample.StartTime from. Read StartWall's doc comment below for
// the measurements that forced the change.
func ProcStartWall(usec int64) time.Time {
	if usec <= 0 {
		return time.Time{}
	}
	return time.Unix(usec/1_000_000, (usec%1_000_000)*1000)
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
//
// # This function is deliberately NOT used by the scanner
//
// The plan's Task 6 specifies this anchor as the source of
// domain.ProcSample.StartTime. It cannot be, and neither Mach clock fixes
// it: the elapsed duration this computes is not the elapsed *wall* time
// whenever the machine slept in between.
//
//   - mach_absolute_time() pauses during system sleep. Measured on the
//     target laptop (uptime 9.74 days, 2.97 of them asleep): loginwindow
//     (pid 410, started 20s after boot) anchors to 2026-08-30 18:18 where
//     `ps -o lstart=` and kern.boottime both say 2026-08-27 18:56 — a
//     71.37h error, exactly the accumulated sleep.
//   - mach_continuous_time() keeps running through sleep, so swapping it in
//     fixes boot-era pids and breaks recent ones by the same 71.37h. That
//     is because ri_proc_start_abstime is itself stamped on the *absolute*
//     clock: a process 3.3ms old stamps it 78,444 ticks below the absolute
//     reading and 6.17e12 ticks below the continuous one. Anchoring an
//     absolute-clock stamp against the continuous clock would have made a
//     just-launched Claude Code process look three days old — worse for
//     Task 9's rollout binding, which binds recent processes.
//
// No single-clock anchor can be right, because the wall gap between two
// absolute-clock readings depends on sleep that happened between them and
// the tick count does not record it. The scanner therefore reads the
// kernel's own wall-clock stamp (kinfo_proc p_starttime, via
// ProcStartWall), which matched `ps -o lstart=` to the second on both a
// boot-era pid and a freshly-spawned one.
//
// StartWall is kept, exported and tested because the arithmetic is correct
// for what it claims — an elapsed Mach duration subtracted from an anchor —
// and TestStartWall pins the negative case (raw ticks read as an epoch land
// decades away) that the plan asks for. Do not wire it back into Scan.
func StartWall(startTicks, nowTicks uint64, numer, denom uint32, nowWall time.Time) time.Time {
	if startTicks > nowTicks {
		return time.Time{}
	}
	elapsedNs := MachTicksToNs(nowTicks-startTicks, numer, denom)
	return nowWall.Add(-time.Duration(elapsedNs))
}
