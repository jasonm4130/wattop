//go:build hardware

package proc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func spinner(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "while :; do :; done")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the busy-loop child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process.Pid
}

var bootSecRe = regexp.MustCompile(`sec\s*=\s*(\d+)`)

func bootTime(t *testing.T) time.Time {
	t.Helper()
	out, err := exec.Command("sysctl", "-n", "kern.boottime").Output()
	if err != nil {
		t.Fatalf("sysctl kern.boottime: %v", err)
	}
	m := bootSecRe.FindSubmatch(out)
	if m == nil {
		t.Fatalf("could not parse kern.boottime output: %q", out)
	}
	sec, err := strconv.ParseInt(string(m[1]), 10, 64)
	if err != nil {
		t.Fatalf("could not parse kern.boottime seconds: %v", err)
	}
	return time.Unix(sec, 0)
}

func TestScanHardware(t *testing.T) {
	spinPID := spinner(t)

	s := NewScanner()
	defer s.Close()

	ctx := context.Background()

	first, err := s.Scan(ctx)
	if err != nil {
		t.Fatalf("first Scan: %v", err)
	}
	if len(first) < 50 {
		t.Fatalf("first scan returned %d processes, want >= 50 (no sudo, own-user only)", len(first))
	}

	time.Sleep(1 * time.Second)

	second, err := s.Scan(ctx)
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(second) < 50 {
		t.Fatalf("second scan returned %d processes, want >= 50", len(second))
	}

	byPID := make(map[int]domain.ProcSample, len(second))
	for _, ps := range second {
		byPID[ps.PID] = ps
	}

	anyCPUNonZero := false
	anyGPULine := false
	gpuRows := 0
	selfPID := os.Getpid()
	var selfSample domain.ProcSample
	sawSelf := false
	var otherArgvSample domain.ProcSample
	sawOtherArgv := false

	for _, ps := range second {
		fmt.Printf("pid=%d comm=%s rss=%dKB cpu=%.2f%%\n", ps.PID, ps.Comm, ps.RSSBytes/1024, ps.CPUPct)
		if ps.CPUPct > 0 {
			anyCPUNonZero = true
		}
		if ps.GPUMsPerSec != nil {
			gpuRows++
		}
		if ps.GPUMsPerSec != nil && *ps.GPUMsPerSec > 0 {
			fmt.Printf("pid=%d gpu_ms_per_sec=%.3f\n", ps.PID, *ps.GPUMsPerSec)
			anyGPULine = true
		}
		if ps.PID == selfPID {
			selfSample = ps
			sawSelf = true
		}
		if ps.PID != selfPID && len(ps.Argv) > 0 && !sawOtherArgv {
			otherArgvSample = ps
			sawOtherArgv = true
		}
	}

	if !anyCPUNonZero {
		t.Fatalf("no process reported nonzero CPU%% across two scans 1s apart")
	}

	spin, ok := byPID[spinPID]
	if !ok {
		t.Fatalf("the busy-loop child (pid %d) did not appear in the second scan", spinPID)
	}
	if spin.CPUPct < 50 || spin.CPUPct > 200 {
		t.Fatalf("busy-loop child (pid %d) reported cpu=%.2f%%, want ~100%% (50-200): "+
			"a value near 2.4%% means CPU ticks are being read as nanoseconds without the mach timebase",
			spinPID, spin.CPUPct)
	}
	fmt.Printf("pid=%d comm=%s rss=%dKB cpu=%.2f%% (busy-loop child, want ~100%%)\n",
		spin.PID, spin.Comm, spin.RSSBytes/1024, spin.CPUPct)
	if !sawSelf {
		t.Fatalf("this test's own pid (%d) did not appear in the second scan", selfPID)
	}
	if selfSample.RSSBytes == 0 {
		t.Fatalf("this test's own pid reported RSS=0, want nonzero")
	}
	if len(selfSample.Argv) == 0 {
		t.Fatalf("this test's own pid reported empty argv")
	}
	fmt.Printf("pid=%d cwd=%s argv=%v\n", selfSample.PID, selfSample.CWD, selfSample.Argv)
	if selfSample.CWD == "" {
		t.Fatalf("this test's own pid reported empty cwd")
	}

	selfPath, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	foundSelfPath := false
	for _, a := range selfSample.Argv {
		if a == selfPath || a == os.Args[0] {
			foundSelfPath = true
			break
		}
	}
	if !foundSelfPath {
		t.Fatalf("own argv %v does not contain this test binary's own path (%s or %s)", selfSample.Argv, selfPath, os.Args[0])
	}

	if !sawOtherArgv {
		t.Fatalf("no other pid reported a nonempty argv")
	}
	fmt.Printf("pid=%d argv=%v\n", otherArgvSample.PID, otherArgvSample.Argv)

	if !anyGPULine {
		t.Fatalf("no pid reported nonzero gpu_ms_per_sec across two scans 1s apart "+
			"(%d of %d rows carried a GPU delta at all); if the machine is truly GPU-idle, "+
			"re-run with a GPU-accelerated window on screen",
			gpuRows, len(second))
	}

	boot := bootTime(t)
	psStart := psStartTimes(t)
	now := time.Now()

	checked, zeroStart := 0, 0
	var worstPID int
	var worstSkew time.Duration
	for _, ps := range second {
		if ps.StartTime.IsZero() {
			zeroStart++
			continue
		}
		if ps.StartTime.Before(boot.Add(-5*time.Second)) || ps.StartTime.After(now) {
			t.Fatalf("pid %d StartTime %v is not a plausible wall-clock instant (want between boot %v and now %v)", ps.PID, ps.StartTime, boot, now)
		}

		want, ok := psStart[ps.PID]
		if !ok {
			continue
		}
		skew := ps.StartTime.Sub(want)
		if skew < 0 {
			skew = -skew
		}
		if skew > psSkewTolerance {
			t.Fatalf("pid %d (%s) StartTime %v disagrees with `ps -o lstart=` (%v) by %v, tolerance %v: "+
				"StartTime must come from kinfo_proc p_starttime, never from a Mach tick count anchored "+
				"against a clock read now — mach_absolute_time pauses during sleep and mach_continuous_time "+
				"does not, so neither recovers a wall-clock start on a laptop that has slept",
				ps.PID, ps.Comm, ps.StartTime, want, skew, psSkewTolerance)
		}
		if skew > worstSkew {
			worstSkew, worstPID = skew, ps.PID
		}
		checked++
	}

	if checked < 20 {
		t.Fatalf("only %d of %d rows had a StartTime cross-checked against `ps` (%d rows carried the zero time); "+
			"want >= 20, or the start-time assertion is asserting nothing",
			checked, len(second), zeroStart)
	}
	if zeroStart > 0 {
		t.Logf("%d of %d rows carried a zero StartTime (kernel gave no p_starttime)", zeroStart, len(second))
	}
	fmt.Printf("start_time cross-checked against ps for %d pids; worst skew %v (pid %d), tolerance %v\n",
		checked, worstSkew, worstPID, psSkewTolerance)
}

const psSkewTolerance = 2 * time.Second

const psLstartLayout = "Mon Jan 2 15:04:05 2006"

func psStartTimes(t *testing.T) map[int]time.Time {
	t.Helper()
	cmd := exec.Command("ps", "-eo", "pid=,lstart=")
	// ps renders lstart in the user's locale (e.g. "Thu 27 Aug ...").
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ps -eo pid=,lstart=: %v", err)
	}
	m := make(map[int]time.Time)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		when, err := time.ParseInLocation(psLstartLayout, strings.Join(fields[1:6], " "), time.Local)
		if err != nil {
			t.Fatalf("could not parse ps lstart %q: %v", line, err)
		}
		m[pid] = when
	}
	if len(m) < 50 {
		t.Fatalf("ps reported start times for only %d pids, want >= 50 — the oracle did not parse", len(m))
	}
	return m
}
