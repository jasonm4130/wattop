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

const demoPrewarm = 120 * time.Second

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
