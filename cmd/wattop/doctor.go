package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/soc"
)

func runDoctor(ctx context.Context, w io.Writer, src Sources, interval time.Duration, book *pricing.Book, themeName string, groups bool) {
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
		printIOReportGroups(w, soc.IOReportGroups)
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
	if ok, err := pricingHealth(book.Entries(), fetchedAt, now); ok {
		fmt.Fprintln(w, "  status: ok")
	} else {
		fmt.Fprintf(w, "  status: degraded (%v)\n", err)
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "theme: %s\n", themeName)
}

func printIOReportGroups(w io.Writer, enumerate func() ([]soc.Group, bool, error)) {
	fmt.Fprintln(w, "ioreport-groups:")
	groups, complete, err := enumerate()
	if err != nil {
		fmt.Fprintf(w, "  unavailable: %v\n", err)
		return
	}
	if len(groups) == 0 {
		fmt.Fprintln(w, "  (none reported)")
		return
	}
	total := 0
	for _, g := range groups {
		fmt.Fprintf(w, "  %s: %d channels\n", g.Name, g.Channels)
		total += g.Channels
	}
	fmt.Fprintf(w, "  total: %d groups, %d channels\n", len(groups), total)
	if complete {
		fmt.Fprintln(w, "  listing: wildcard channel copy (every group this machine publishes)")
	} else {
		fmt.Fprintln(w, "  listing: named-group probe (IOReport's wildcard channel copy returned nothing)")
	}
}

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
