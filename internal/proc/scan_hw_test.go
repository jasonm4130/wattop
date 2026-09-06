//go:build hardware

package proc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
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

	// pid 1 (launchd) is the strongest epoch-anchoring check named in the
	// spec: its start time must be within a few seconds of boot, where a
	// short-lived test process's own start time would not catch a broken
	// anchor. On this machine, run as a regular (non-root) user, pid 1 is
	// launchd running as root: proc_pidinfo(PROC_PIDTASKINFO, 1) and
	// proc_pid_rusage(1, ...) both fail with EPERM — confirmed directly
	// with a small C probe outside this test, not assumed — so Scan skips
	// pid 1's row entirely, the same way it skips any other-user pid. That
	// is a genuine conflict between the plan's "no sudo prompt" acceptance
	// requirement and its "assert it for pid 1" suggestion on this
	// specific machine/user; this test degrades to a logged note rather
	// than failing or requiring sudo.
	boot := bootTime(t)
	if initSample, ok := byPID[1]; ok {
		if initSample.StartTime.IsZero() {
			t.Fatalf("pid 1's StartTime is the zero time — epoch anchoring failed")
		}
		if initSample.StartTime.Before(boot.Add(-5*time.Second)) || initSample.StartTime.After(boot.Add(30*time.Second)) {
			t.Fatalf("pid 1 StartTime %v is not within a few seconds of boot %v", initSample.StartTime, boot)
		}
	} else {
		t.Logf("pid 1 (root-owned launchd) is not readable without sudo on this machine/user — skipping the pid-1-specific boot-time check; see comment above")
	}

	now := time.Now()
	for _, ps := range second {
		if ps.StartTime.IsZero() {
			continue
		}
		if ps.StartTime.Before(boot.Add(-5*time.Second)) || ps.StartTime.After(now) {
			t.Fatalf("pid %d StartTime %v is not a plausible wall-clock instant (want between boot %v and now %v)", ps.PID, ps.StartTime, boot, now)
		}
	}
}
