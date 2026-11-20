package demo

import (
	"context"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

type Sampler struct{ w *World }

func NewSampler(w *World) *Sampler { return &Sampler{w: w} }

func (s *Sampler) Init() error { return nil }

func (s *Sampler) Sample(ctx context.Context, _ int) (domain.SysSample, error) {
	if err := ctx.Err(); err != nil {
		return domain.SysSample{}, err
	}
	s.w.Advance()
	return s.w.Sys(), nil
}

func (s *Sampler) ThermalState() int { return 0 }

func (s *Sampler) Channels() map[string]bool {
	return map[string]bool{
		"cpu_power": true, "gpu_power": true, "ane_power": true, "dram_power": true,
		"system_power": true, "cpu_temp": true, "gpu_temp": true, "fans": true,
	}
}

func (s *Sampler) Close() error { return nil }

type ProcSource struct {
	w       *World
	now     func() time.Time
	selfPID int
}

func NewProcSource(w *World, now func() time.Time, selfPID int) *ProcSource {
	if now == nil {
		now = time.Now
	}
	return &ProcSource{w: w, now: now, selfPID: selfPID}
}

func (p *ProcSource) Scan(ctx context.Context) ([]domain.ProcSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	at := p.now()
	procs := p.w.Procs(at)
	if p.selfPID > 0 {
		procs = append(procs, p.w.SelfProc(p.selfPID, at))
	}
	return procs, nil
}

func (p *ProcSource) Close() error { return nil }

type AgentSource struct {
	w     *World
	agent string
}

func NewAgentSource(w *World, agent string) *AgentSource {
	return &AgentSource{w: w, agent: agent}
}

func (a *AgentSource) Name() string { return a.agent }

func (a *AgentSource) Poll(ctx context.Context, now time.Time, _ []domain.ProcSample) ([]domain.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.w.Sessions(a.agent, now), nil
}

func (a *AgentSource) Close() error { return nil }
