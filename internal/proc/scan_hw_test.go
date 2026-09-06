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
		t.Logf("no pid reported nonzero gpu_ms_per_sec on this scan pair (GPU load is workload-dependent; re-run under GPU load — e.g. scroll a page in a GPU-accelerated app — if this acceptance line is required right now)")
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
