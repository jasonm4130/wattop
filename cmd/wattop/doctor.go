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
	// Two scans, with the soc block between them. internal/proc reports
	// deltas, never lifetime averages: a pid's first appearance only
	// establishes its baseline, so it carries no CPU percentage and no GPU
	// ms/sec at all. A single scan would therefore always report an empty
	// GPU table on a machine whose GPU is busy -- a false negative in the
	// one command whose job is to say what actually resolved. The
	// throwaway scan is the baseline; soc.Sample's block supplies the
	// interval between the two; the second scan is the one reported.
	_, _ = safeScan(ctx, src.Proc)
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
	resolved, unresolved := countChannels(src.Sys.Channels())
	fmt.Fprintf(w, "  %d resolved, %d unresolved\n", resolved, unresolved)
	printChannels(w, src.Sys.Channels())

	if groups {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "ioreport-groups:")
		fmt.Fprintln(w, "  BLOCKED: enumerating real IOReport groups needs a group-level API")
		fmt.Fprintln(w, "  in internal/soc (Task 5's shim.go) -- something like")
		fmt.Fprintln(w, "  soc.IOReportGroups() []Group{Name string; Channels int}, reading the")
		fmt.Fprintln(w, "  subscription's channel list from the vendored ObjC. domain.Sampler")
		fmt.Fprintln(w, "  exposes only Channels() map[string]bool, which is wattop's own field")
		fmt.Fprintln(w, "  resolution, not Apple's grouping, so no honest group name or channel")
		fmt.Fprintln(w, "  count can be derived here. What follows is that channel set with its")
		fmt.Fprintln(w, "  counts, labelled for what it is rather than dressed up as groups.")
		fmt.Fprintln(w, "  Until the soc API lands, a renamed DRAM channel shows up in this")
		fmt.Fprintln(w, "  listing as a new unresolved entry.")
		fmt.Fprintln(w)
		resolved, unresolved := countChannels(src.Sys.Channels())
		fmt.Fprintf(w, "  wattop channels: %d resolved, %d unresolved\n", resolved, unresolved)
		printChannels(w, src.Sys.Channels())
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "proc:")
	if errProc != nil {
		fmt.Fprintf(w, "  scan failed: %v\n", errProc)
	} else {
		fmt.Fprintf(w, "  rows: %d\n", len(procs))
		gpuRows, cpuRows := 0, 0
		for _, p := range procs {
			if p.GPUMsPerSec != nil {
				gpuRows++
			}
			if p.CPUPct > 0 {
				cpuRows++
			}
		}
		fmt.Fprintf(w, "  rows with a cpu percentage: %d\n", cpuRows)
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
	// The same predicate the cycle badges on, so doctor and the dashboard
	// never disagree about whether the table is trustworthy.
	if ok, err := pricingHealth(book.Entries(), fetchedAt, now); ok {
		fmt.Fprintln(w, "  status: ok")
	} else {
		fmt.Fprintf(w, "  status: degraded (%v)\n", err)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "theme: %s\n", themeName)
}

// countChannels reports how many of wattop's own SoC fields resolved on
// this hardware. It is a count of wattop channels, never of IOReport group
// members -- see the ioreport-groups section for why that number cannot be
// produced from the current soc seam.
func countChannels(channels map[string]bool) (resolved, unresolved int) {
	for _, ok := range channels {
		if ok {
			resolved++
		} else {
			unresolved++
		}
	}
	return resolved, unresolved
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
