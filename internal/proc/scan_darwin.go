//go:build darwin && arm64 && cgo

// Package proc's Apple-Silicon implementation: a hand-written CGO process
// scanner (no gopsutil, no third-party dependency). Enumeration and the
// per-pid syscalls live in scan_darwin.c; this file owns orchestration, the
// wall-clock start-time conversion (delta.go's ProcStartWall) and the
// CPU/GPU deltas (delta.go's trackers). All of it is passwordless for the
// calling user's own processes.
package proc

/*
#include <stdlib.h>
#include <stdint.h>
#include <sys/types.h>
#include <mach/mach_time.h>

int wattop_list_pids(pid_t **out_pids, int64_t **out_start_usec, int *out_count);
void wattop_free(void *p);
int wattop_task_info(pid_t pid, uint64_t *rss_bytes, uint64_t *cpu_ticks);
int wattop_comm(pid_t pid, char *buf, int buflen);
int wattop_rusage(pid_t pid, uint64_t *diskread, uint64_t *diskwrite);
int wattop_cwd(pid_t pid, char *buf, int buflen);
int wattop_argv(pid_t pid, char *buf, int bufcap, int *out_size, int *out_argc);
*/
import "C"

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/jasonm4130/wattop/internal/domain"
)

// argvBufSize is the KERN_PROCARGS2 scratch buffer size, allocated once per
// Scanner and reused across every pid in a scan (rather than once per pid)
// — the same footprint discipline the plan rejected jouletop's child
// process over.
const argvBufSize = 256 * 1024

// commBufSize and cwdBufSize are generous fixed sizes for proc_pidinfo's
// truncated comm string and PROC_PIDVNODEPATHINFO's path, respectively.
const (
	commBufSize = 64
	cwdBufSize  = 1024
)

// Scanner implements domain.ProcSource over the syscalls in scan_darwin.c.
// It is not safe for concurrent use: it is meant to be driven by one
// goroutine, the same one that samples the SoC (see the plan's "read in
// the same instant" requirement), and its CPU/GPU delta trackers assume
// calls are sequential.
type Scanner struct {
	cpu *CPUTracker
	gpu *GPUTracker

	tbOnce      sync.Once
	timebaseNum uint32
	timebaseDen uint32

	systemGPUActivePct *float64

	argvBuf []byte
}

var _ domain.ProcSource = (*Scanner)(nil)

// NewScanner returns a ready-to-use Scanner. Its first Scan establishes CPU
// and GPU baselines only — see CPUTracker and GPUTracker.
func NewScanner() *Scanner {
	return &Scanner{
		cpu:     NewCPUTracker(),
		gpu:     NewGPUTracker(),
		argvBuf: make([]byte, argvBufSize),
	}
}

// SetSystemGPUActivePct feeds the scanner the just-sampled system-wide GPU
// busy percentage (domain.SysSample.GPU.ActivePct) so Scan can rescale
// per-pid GPUMsPerSec into GPUPctApprox.
//
// This exists because of a real gap between Task 2's domain.ProcSource
// interface (Scan takes no such argument, and internal/domain cannot be
// edited here) and the rescale this task specifies: the caller — the
// wiring goroutine that samples both the Sampler and this Scanner each
// cycle — must call this right after sampling the SoC and before calling
// Scan. Passing nil (the default) leaves GPUPctApprox nil for every pid.
func (s *Scanner) SetSystemGPUActivePct(pct *float64) {
	s.systemGPUActivePct = pct
}

// Close is a no-op: the scanner holds no OS resources between scans.
func (s *Scanner) Close() error { return nil }

// Scan enumerates every pid via sysctl(KERN_PROC_ALL) and reads per-pid
// CPU/RSS/disk/argv/cwd. The first call on a Scanner establishes CPU and
// GPU baselines and reports 0 for both — never a lifetime average; see
// CPUTracker and GPUTracker in delta.go.
func (s *Scanner) Scan(ctx context.Context) ([]domain.ProcSample, error) {
	s.tbOnce.Do(func() {
		// The timebase converts Mach absolute ticks to nanoseconds. It is
		// load-bearing for exactly one field now — pti_total_user +
		// pti_total_system, which proc_pidinfo reports in Mach units — and
		// deliberately not for StartTime. It is 125/3 on Apple Silicon (1/1
		// on Intel); a failed read leaves both fields zero, so fall back to
		// 1/1 rather than letting MachTicksToNs's denom==0 guard silently
		// zero every CPU duration in the scan.
		var tb C.mach_timebase_info_data_t
		if C.mach_timebase_info(&tb) != 0 || tb.denom == 0 {
			s.timebaseNum, s.timebaseDen = 1, 1
			return
		}
		s.timebaseNum = uint32(tb.numer)
		s.timebaseDen = uint32(tb.denom)
	})

	// One wall clock read per scan, so every CPU and GPU delta in this scan
	// shares a basis. Process start times do NOT come from here, and do not
	// come from any Mach clock — see the startUsec slice below.
	nowWall := time.Now()

	// The enumeration sysctl yields the pid list and, in the same pass,
	// each process's wall-clock start time (kinfo_proc's p_starttime, in
	// microseconds since the Unix epoch). The two slices are parallel.
	var pidsPtr *C.pid_t
	var startPtr *C.int64_t
	var count C.int
	if C.wattop_list_pids(&pidsPtr, &startPtr, &count) != 0 {
		return nil, fmt.Errorf("proc: sysctl(KERN_PROC_ALL) enumeration failed")
	}
	defer C.wattop_free(unsafe.Pointer(pidsPtr))
	defer C.wattop_free(unsafe.Pointer(startPtr))
	pids := unsafe.Slice(pidsPtr, int(count))
	startUsec := unsafe.Slice(startPtr, int(count))

	gpuTable, gpuErr := gpuNsTable()
	var gpuMsPerSec map[int]float64
	if gpuErr == nil {
		gpuMsPerSec = s.gpu.Update(gpuTable, nowWall)
	}
	gpuPctApprox := RescaleGPUPct(gpuMsPerSec, s.systemGPUActivePct)

	out := make([]domain.ProcSample, 0, len(pids))
	alive := make(map[int]bool, len(pids))

	for i, cpid := range pids {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		pid := int(cpid)

		var rss, cpuTicks C.uint64_t
		if C.wattop_task_info(cpid, &rss, &cpuTicks) != 0 {
			// Exited between enumeration and this call, or owned by
			// another user — proc_pidinfo(PROC_PIDTASKINFO) is
			// passwordless only for the calling user's own processes.
			continue
		}
		alive[pid] = true

		// p_comm is display-only and NEVER a join key: a live Claude Code
		// pid reports its version string here (e.g. "2.1.261"), and its
		// Workflow children report "node". Join by pid; take the display
		// name from the session file and the command from Argv.
		var commBuf [commBufSize]C.char
		commBuf[0] = 0
		var comm string
		if C.wattop_comm(cpid, &commBuf[0], C.int(len(commBuf))) == 0 {
			comm = C.GoString(&commBuf[0])
		}

		var diskRead, diskWrite C.uint64_t
		_ = C.wattop_rusage(cpid, &diskRead, &diskWrite)
		// On rusage failure (permission error, or the pid is gone), the
		// disk counters stay zero — best-effort, per the spec: leave the
		// field empty, carry on.

		// StartTime comes from the enumeration's p_starttime, a wall-clock
		// timeval, and never from a Mach tick count anchored against a
		// clock read now. The plan's Task 6 specifies the latter; it cannot
		// work, and the deviation is deliberate. Measured on this machine
		// (uptime 9.74 days, of which 2.97 days asleep): loginwindow (pid
		// 410, started 20s after boot) computes 2026-08-30 18:18 from an
		// absolute-clock anchor against `ps`/kern.boottime's 2026-08-27
		// 18:56, a 71.37h error; a continuous-clock anchor fixes that pid
		// and breaks a just-started one by the same 71.37h, because
		// ri_proc_start_abstime is itself stamped on the absolute clock (a
		// 3.3ms-old process stamps it 78,444 ticks below the absolute
		// reading and 6.17e12 below the continuous one). p_starttime
		// matched `ps -o lstart=` to the second on both. Full reasoning in
		// StartWall's doc comment in delta.go.
		startTime := ProcStartWall(int64(startUsec[i]))

		var cwdBuf [cwdBufSize]C.char
		cwdBuf[0] = 0
		var cwd string
		if C.wattop_cwd(cpid, &cwdBuf[0], C.int(len(cwdBuf))) == 0 {
			cwd = C.GoString(&cwdBuf[0])
		}

		var argv []string
		var argSize, argc C.int
		if C.wattop_argv(cpid, (*C.char)(unsafe.Pointer(&s.argvBuf[0])), C.int(len(s.argvBuf)), &argSize, &argc) == 0 {
			argv = parseProcArgs2(s.argvBuf[:int(argSize)], int(argc))
		}
		// On argv failure (permission error, or the pid is gone), argv
		// stays nil — best-effort, per the spec.

		// pti_total_user+pti_total_system come back as Mach absolute
		// ticks, NOT nanoseconds (see wattop_task_info in scan_darwin.c).
		// CPUTracker/CPUPct are documented in nanoseconds, so the timebase
		// conversion has to happen here, before the tracker sees the
		// value; skipping it reads ~41.7x low on Apple Silicon.
		cpuNs := MachTicksToNs(uint64(cpuTicks), s.timebaseNum, s.timebaseDen)

		// The ok return is deliberately discarded: it distinguishes "no
		// baseline yet" (the first scan of a pid) from a genuine 0%, but
		// domain.ProcSample.CPUPct is a plain float64, not a *float64, and
		// internal/domain is out of this task's scope. A first-scan row
		// therefore reports 0% rather than "no percentage yet" — a forced
		// compromise with the existing type, not an oversight.
		cpuPct, _ := s.cpu.Update(pid, cpuNs, nowWall)

		ps := domain.ProcSample{
			PID:        pid,
			Comm:       comm,
			Argv:       argv,
			CWD:        cwd,
			RSSBytes:   uint64(rss),
			CPUPct:     cpuPct,
			DiskReadB:  uint64(diskRead),
			DiskWriteB: uint64(diskWrite),
			StartTime:  startTime,
		}
		if v, ok := gpuMsPerSec[pid]; ok {
			msPerSec := v
			ps.GPUMsPerSec = &msPerSec
			ps.GPUPctApprox = gpuPctApprox[pid]
		}
		out = append(out, ps)
	}

	s.cpu.Prune(alive)
	s.gpu.Prune(alive)

	return out, nil
}

// parseProcArgs2 parses a raw KERN_PROCARGS2 blob — argc (int32), the exec
// path (NUL-terminated), NUL padding up to word alignment, then argc
// NUL-terminated argv strings, then the environment — into argv. Pure Go,
// no syscalls; kept here because the layout is KERN_PROCARGS2-specific, not
// because it needs its own build tag.
func parseProcArgs2(buf []byte, argc int) []string {
	if argc <= 0 || len(buf) < 4 {
		return nil
	}
	p := 4
	for p < len(buf) && buf[p] != 0 {
		p++
	}
	for p < len(buf) && buf[p] == 0 {
		p++
	}
	argv := make([]string, 0, argc)
	for i := 0; i < argc && p < len(buf); i++ {
		start := p
		for p < len(buf) && buf[p] != 0 {
			p++
		}
		argv = append(argv, string(buf[start:p]))
		if p < len(buf) {
			p++
		}
	}
	return argv
}
