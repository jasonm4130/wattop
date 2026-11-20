package proc

import "time"

func CPUPct(prevNs, curNs uint64, wall time.Duration) float64 {
	if wall <= 0 || curNs < prevNs {
		return 0
	}
	return float64(curNs-prevNs) / float64(wall.Nanoseconds()) * 100
}

type cpuSample struct {
	ns uint64
	at time.Time
}

type CPUTracker struct {
	prev map[int]cpuSample
}

func NewCPUTracker() *CPUTracker {
	return &CPUTracker{prev: make(map[int]cpuSample)}
}

func (t *CPUTracker) Update(pid int, ns uint64, now time.Time) (pct float64, ok bool) {
	prev, seen := t.prev[pid]
	t.prev[pid] = cpuSample{ns: ns, at: now}
	if !seen {
		return 0, false
	}
	return CPUPct(prev.ns, ns, now.Sub(prev.at)), true
}

func (t *CPUTracker) Prune(alive map[int]bool) {
	for pid := range t.prev {
		if !alive[pid] {
			delete(t.prev, pid)
		}
	}
}

type gpuSample struct {
	ns uint64
	at time.Time
}

type GPUTracker struct {
	prev map[int]gpuSample
}

func NewGPUTracker() *GPUTracker {
	return &GPUTracker{prev: make(map[int]gpuSample)}
}

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

func (t *GPUTracker) Prune(alive map[int]bool) {
	for pid := range t.prev {
		if !alive[pid] {
			delete(t.prev, pid)
		}
	}
}

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

func MachTicksToNs(ticks uint64, numer, denom uint32) uint64 {
	if denom == 0 {
		return 0
	}
	q, r := ticks/uint64(denom), ticks%uint64(denom)
	return q*uint64(numer) + r*uint64(numer)/uint64(denom)
}

func ProcStartWall(usec int64) time.Time {
	if usec <= 0 {
		return time.Time{}
	}
	return time.Unix(usec/1_000_000, (usec%1_000_000)*1000)
}

func StartWall(startTicks, nowTicks uint64, numer, denom uint32, nowWall time.Time) time.Time {
	if startTicks > nowTicks {
		return time.Time{}
	}
	elapsedNs := MachTicksToNs(nowTicks-startTicks, numer, denom)
	return nowWall.Add(-time.Duration(elapsedNs))
}
