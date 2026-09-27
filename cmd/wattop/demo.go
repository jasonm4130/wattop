// demo.go wires --demo: the normal TUI and --json paths over the synthetic
// sources in internal/collect/demo instead of the real collectors.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/jasonm4130/wattop/internal/collect/demo"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/state"
)

// demoPrewarm is how much synthetic history is reduced into State before
// the first real cycle, so the 120-second graphs are already full on the
// first frame instead of filling in over two minutes of recording.
const demoPrewarm = 120 * time.Second

// buildDemoSources returns Sources over a fresh demo World. It never
// touches soc, proc, ~/.claude or ~/.codex, and it prices from the
// embedded table only: there is no Refresh, so --demo makes no network
// request. Sources.Pricing is left nil so the demo never shows a pricing
// age badge however old the embedded snapshot gets.
func buildDemoSources(interval time.Duration) (Sources, *pricing.Book, *demo.World, error) {
	book, err := pricing.Load()
	if err != nil {
		return Sources{}, nil, nil, fmt.Errorf("loading pricing table: %w", err)
	}
	w := demo.New(demo.DefaultSeed, interval)
	src := Sources{
		Sys:  demo.NewSampler(w),
		Proc: demo.NewProcSource(w, nil, os.Getpid()),
		Agents: []domain.AgentSource{
			demo.NewAgentSource(w, "claude"),
			demo.NewAgentSource(w, "codex"),
		},
	}
	return src, book, w, nil
}

// prewarmDemo reduces span's worth of synthetic cycles into st, dated
// backwards from now, advancing w exactly as the demo Sampler would. The
// history rings and burn tracker then start in a steady state.
func prewarmDemo(st *state.State, w *demo.World, now time.Time, span time.Duration) {
	n := int(span / w.Step())
	for k := 0; k < n; k++ {
		w.Advance()
		at := now.Add(-time.Duration(n-k) * w.Step())
		st.Reduce(state.Inputs{
			At:       at,
			Sys:      w.Sys(),
			Procs:    append(w.Procs(at), w.SelfProc(os.Getpid(), at)),
			Sessions: w.Sessions("", at),
		})
	}
}
