//go:build darwin && arm64 && cgo

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

const argvBufSize = 256 * 1024

const (
	commBufSize = 64
	cwdBufSize  = 1024
)

type Scanner struct {
	cpu *CPUTracker
	gpu *GPUTracker

	tbOnce      sync.Once
	timebaseNum uint32
	timebaseDen uint32

	argvBuf []byte
}

var _ domain.ProcSource = (*Scanner)(nil)

func NewScanner() *Scanner {
	return &Scanner{
		cpu:     NewCPUTracker(),
		gpu:     NewGPUTracker(),
		argvBuf: make([]byte, argvBufSize),
	}
}

func (s *Scanner) Close() error { return nil }

func (s *Scanner) Scan(ctx context.Context, systemGPUActivePct *float64) ([]domain.ProcSample, error) {
	s.tbOnce.Do(func() {
		var tb C.mach_timebase_info_data_t
		if C.mach_timebase_info(&tb) != 0 || tb.denom == 0 {
			s.timebaseNum, s.timebaseDen = 1, 1
			return
		}
		s.timebaseNum = uint32(tb.numer)
		s.timebaseDen = uint32(tb.denom)
	})

	nowWall := time.Now()

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
	gpuPctApprox := RescaleGPUPct(gpuMsPerSec, systemGPUActivePct)

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
			continue
		}
		alive[pid] = true

		var commBuf [commBufSize]C.char
		commBuf[0] = 0
		var comm string
		if C.wattop_comm(cpid, &commBuf[0], C.int(len(commBuf))) == 0 {
			comm = C.GoString(&commBuf[0])
		}

		var diskRead, diskWrite C.uint64_t
		_ = C.wattop_rusage(cpid, &diskRead, &diskWrite)

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

		cpuNs := MachTicksToNs(uint64(cpuTicks), s.timebaseNum, s.timebaseDen)

		var cpuPct *float64
		if pct, measured := s.cpu.Update(pid, cpuNs, nowWall); measured {
			cpuPct = &pct
		}

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

// KERN_PROCARGS2 layout: argc (int32), the exec path (NUL-terminated), NUL
// padding to word alignment, argc NUL-terminated argv strings, then the
// environment.
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
