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

// spinner starts a shell busy-loop pinned to one core and returns its pid.
// It is the magnitude check for CPU%: a process consuming one full core for
// the whole inter-scan window must be reported near 100%, and the unit bug
// this file guards against (Mach ticks read as nanoseconds) reports it at
// ~2.4% instead — a value the "at least one process has cpu>0" assertion
// happily accepts. The pid comes from Process.Pid, never from matching
// comm: p_comm is display-only here as everywhere else.
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

// bootTime shells out to `sysctl -n kern.boottime` and parses its
// "{ sec = <n>, usec = <n> } ..." output. No cgo needed for this one read,
// so this test file only carries the `hardware` build tag (per the spec's
// FILES list) rather than also gating on darwin/arm64/cgo — on any other
// platform NewScanner's stub fails the scan-count assertions immediately,
// which is the correct failure for a test documented as requiring this
// machine.
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

// TestScanHardware exercises the real Apple Silicon scanner: two scans 1s
// apart against the live process table, no sudo. It also serves as the
// "scratch program" the plan's acceptance commands ask for: every required
// line form is printed here (stdout, via fmt.Printf so `-v` output carries
// it), so `go test -tags=hardware ./internal/proc/ -v` is the artifact
// those acceptance commands check.
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

	// Magnitude, not just sign. The busy-loop child held one full core for
	// the whole 1s window, so its CPU% must be near 100. Reading
	// pti_total_user/pti_total_system as nanoseconds instead of Mach ticks
	// puts it at ~2.4% (measured: a `yes` process at 2.39% while `ps`
	// reported 95.9%), which every sign-only assertion in this file passes.
	// The band is wide because scheduling on a loaded machine is noisy; it
	// is still an order of magnitude clear of the bug.
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

	// The plan's acceptance requires >=1 nonzero gpu_ms_per_sec line, so
	// this fails rather than logs. The diagnostic distinguishes the two
	// failure modes: gpuRows==0 means soc.GPUProcessStats returned no pid
	// that also survived this scanner's own-user filter (nothing to
	// difference), while gpuRows>0 with no nonzero value means the table
	// resolved but nothing touched the GPU between the two scans.
	if !anyGPULine {
		t.Fatalf("no pid reported nonzero gpu_ms_per_sec across two scans 1s apart "+
			"(%d of %d rows carried a GPU delta at all); if the machine is truly GPU-idle, "+
			"re-run with a GPU-accelerated window on screen",
			gpuRows, len(second))
	}

	// StartTime is cross-checked against `ps` for EVERY row, and this is
	// the assertion that closes the start-time question. `ps` renders
	// kinfo_proc's p_starttime, a wall-clock timeval stamped at exec, and
	// so is fully independent of the Mach clocks — which is the point: an
	// earlier revision of this scanner anchored StartTime on
	// mach_absolute_time and dated pid 410 (loginwindow) 71.37h late on
	// this machine, and nothing in this file caught it.
	//
	// The plan asks for the check to be made against pid 1, whose start
	// must be within seconds of boot. That branch is gone rather than
	// degraded to a log: pid 1 is root-owned launchd, proc_pidinfo(
	// PROC_PIDTASKINFO, 1) fails EPERM without sudo, so Scan never emits a
	// row for it and a pid-1 branch guards nothing on this machine/user.
	// The `ps` cross-check below is strictly stronger — it covers hundreds
	// of rows instead of one, it is exact rather than a boot-adjacency
	// window, and it cannot silently skip.
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
		// A plausible wall-clock instant: no earlier than boot, no later
		// than now. Weak on its own (it does not discriminate a
		// sleep-skewed anchor, which moves start times forward), which is
		// why it is the secondary check here and not the primary one.
		if ps.StartTime.Before(boot.Add(-5*time.Second)) || ps.StartTime.After(now) {
			t.Fatalf("pid %d StartTime %v is not a plausible wall-clock instant (want between boot %v and now %v)", ps.PID, ps.StartTime, boot, now)
		}

		want, ok := psStart[ps.PID]
		if !ok {
			// Started after, or exited before, the `ps` call — not a
			// disagreement, just a race. Skip.
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

	// Without a floor the loop above is vacuous: every row could carry a
	// zero StartTime and nothing would be asserted. 20 is far below the
	// hundreds of own-user processes any desktop session has and far above
	// what a fluke could supply.
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

// psSkewTolerance bounds the disagreement between a row's StartTime and
// `ps -o lstart=`. `ps` prints whole seconds, so a correct implementation
// still differs by up to 1s of truncation; 2s leaves headroom without
// coming anywhere near the multi-hour errors a mis-anchored Mach clock
// produces.
const psSkewTolerance = 2 * time.Second

// psLstartLayout parses `ps -o lstart=` under LC_ALL=C, which prints e.g.
// "Thu Aug 27 18:56:00 2026" — and "Sun Sep  6 12:43:08 2026" for a
// single-digit day. The fields are re-joined with single spaces before
// parsing, so the layout uses a plain "2" rather than "_2".
const psLstartLayout = "Mon Jan 2 15:04:05 2006"

// psStartTimes returns every pid's start time as `ps` reports it: the
// kinfo_proc p_starttime wall-clock stamp, read through a path completely
// independent of this package's syscalls. That independence is what makes
// it a usable oracle for StartTime.
func psStartTimes(t *testing.T) map[int]time.Time {
	t.Helper()
	cmd := exec.Command("ps", "-eo", "pid=,lstart=")
	// LC_ALL must be forced: exec.Command inherits the parent environment,
	// and the user's locale renders lstart as "Thu 27 Aug ..." rather than
	// the "Thu Aug 27 ..." psLstartLayout expects. TZ is deliberately NOT
	// forced — lstart is local time and ParseInLocation reads it as such.
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
