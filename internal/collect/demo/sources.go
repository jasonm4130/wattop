package demo

import (
	"context"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// Sampler is the domain.Sampler over a World. Each Sample advances the
// world one tick, so the sampling loop's cadence is the demo's clock; it
// returns immediately and lets the loop's own pacing fill the interval.
type Sampler struct{ w *World }

// NewSampler returns a Sampler driving w.
func NewSampler(w *World) *Sampler { return &Sampler{w: w} }

// Init always succeeds: there is no hardware to open.
func (s *Sampler) Init() error { return nil }

// Sample advances the world one tick and returns its SoC reading.
func (s *Sampler) Sample(ctx context.Context, _ int) (domain.SysSample, error) {
	if err := ctx.Err(); err != nil {
		return domain.SysSample{}, err
	}
	s.w.Advance()
	return s.w.Sys(), nil
}

// ThermalState is always Nominal.
func (s *Sampler) ThermalState() int { return 0 }

// Channels reports every channel the demo synthesises as resolved.
func (s *Sampler) Channels() map[string]bool {
	return map[string]bool{
		"cpu_power": true, "gpu_power": true, "ane_power": true, "dram_power": true,
		"system_power": true, "cpu_temp": true, "gpu_temp": true, "fans": true,
	}
}

// Close releases nothing.
func (s *Sampler) Close() error { return nil }

// ProcSource is the domain.ProcSource over a World: one synthetic process
// per session.
type ProcSource struct {
	w   *World
	now func() time.Time
}

// NewProcSource returns a ProcSource over w. now dates each process's
// start time; nil means time.Now.
func NewProcSource(w *World, now func() time.Time) *ProcSource {
	if now == nil {
		now = time.Now
	}
	return &ProcSource{w: w, now: now}
}

// Scan returns the current tick's synthetic process table.
func (p *ProcSource) Scan(ctx context.Context) ([]domain.ProcSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p.w.Procs(p.now()), nil
}

// Close releases nothing.
func (p *ProcSource) Close() error { return nil }

// AgentSource is the domain.AgentSource over a World for one agent
// ("claude" or "codex"), so the dashboard's per-source health badges read
// exactly as they would for the real collectors.
type AgentSource struct {
	w     *World
	agent string
}

// NewAgentSource returns an AgentSource reporting w's sessions for agent.
func NewAgentSource(w *World, agent string) *AgentSource {
	return &AgentSource{w: w, agent: agent}
}

// Name is the agent name, matching the real sources' health keys.
func (a *AgentSource) Name() string { return a.agent }

// Poll returns the current tick's sessions for this agent, dated back from
// now. procs is ignored: the demo sessions carry their own pids.
func (a *AgentSource) Poll(ctx context.Context, now time.Time, _ []domain.ProcSample) ([]domain.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.w.Sessions(a.agent, now), nil
}

// Close releases nothing.
func (a *AgentSource) Close() error { return nil }
