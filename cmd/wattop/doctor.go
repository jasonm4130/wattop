// doctor.go implements the `doctor` subcommand: a plain-text report of what
// actually resolved on this machine, printed without ever entering the TUI.
package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/jasonm4130/wattop/internal/pricing"
)

// runDoctor samples one cycle's worth of data directly from src (no Loop,
// no state.State -- doctor reports on the raw collector layer, not on a
// priced, joined Snapshot) and prints what resolved. book and themeName are
// reported as-is; groups additionally prints the --ioreport-groups section.
func runDoctor(ctx context.Context, w io.Writer, src Sources, interval time.Duration, book *pricing.Book, themeName string, groups bool) {
	sys, errSys := safeSample(ctx, src.Sys, interval)
	procs, errProc := safeScan(ctx, src.Proc)
	now := time.Now()

	fmt.Fprintln(w, "wattop doctor")
	fmt.Fprintln(w)

	fmt.Fprintln(w, "soc:")
	if errSys != nil {
		fmt.Fprintf(w, "  sample failed: %v\n", errSys)
	} else {
		fmt.Fprintf(w, "  name: %s\n", sys.SoCName)
		fmt.Fprintf(w, "  thermal_state: %d\n", src.Sys.ThermalState())
	}
	fmt.Fprintln(w, "  clusters:")
	for _, c := range sys.Clusters {
		fmt.Fprintf(w, "    %s: %d cores\n", c.Label, c.CoreCount)
	}
	if len(sys.Clusters) == 0 {
		fmt.Fprintln(w, "    (none reported)")
	}

	fmt.Fprintln(w, "channels:")
	printChannels(w, src.Sys.Channels())

	if groups {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "ioreport-groups:")
		fmt.Fprintln(w, "  (this build's soc seam exposes per-channel resolution, not raw")
		fmt.Fprintln(w, "  IOReport group names -- see the channels section above for what")
		fmt.Fprintln(w, "  actually resolved; a renamed group would show up there as a new")
		fmt.Fprintln(w, "  miss, not here)")
		printChannels(w, src.Sys.Channels())
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "proc:")
	if errProc != nil {
		fmt.Fprintf(w, "  scan failed: %v\n", errProc)
	} else {
		fmt.Fprintf(w, "  rows: %d\n", len(procs))
		gpuRows := 0
		for _, p := range procs {
			if p.GPUMsPerSec != nil {
				gpuRows++
			}
		}
		fmt.Fprintf(w, "  gpu table rows: %d\n", gpuRows)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "sessions:")
	total, bound := 0, 0
	for _, agent := range src.Agents {
		sessions, err := safePoll(ctx, agent, now, procs)
		if err != nil {
			fmt.Fprintf(w, "  %s: poll failed: %v\n", agent.Name(), err)
			continue
		}
		agentBound := 0
		for _, s := range sessions {
			if s.PID != nil {
				agentBound++
			}
		}
		fmt.Fprintf(w, "  %s: %d discovered, %d bound to a pid\n", agent.Name(), len(sessions), agentBound)
		total += len(sessions)
		bound += agentBound
	}
	fmt.Fprintf(w, "  total: %d discovered, %d bound to a pid\n", total, bound)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "pricing:")
	url, sha, fetchedAt := book.Source()
	fmt.Fprintf(w, "  source: %s\n", url)
	fmt.Fprintf(w, "  sha256: %s\n", sha)
	fmt.Fprintf(w, "  entries: %d\n", book.Entries())
	fmt.Fprintf(w, "  age: %s\n", formatAge(fetchedAt, now))

	fmt.Fprintln(w)
	fmt.Fprintf(w, "theme: %s\n", themeName)
}

func printChannels(w io.Writer, channels map[string]bool) {
	if len(channels) == 0 {
		fmt.Fprintln(w, "  (none reported)")
		return
	}
	names := make([]string, 0, len(channels))
	for name := range channels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		status := "unresolved"
		if channels[name] {
			status = "resolved"
		}
		fmt.Fprintf(w, "  %s: %s\n", name, status)
	}
}

func formatAge(fetchedAt, now time.Time) string {
	if fetchedAt.IsZero() {
		return "unknown"
	}
	return now.Sub(fetchedAt).Round(time.Second).String()
}
