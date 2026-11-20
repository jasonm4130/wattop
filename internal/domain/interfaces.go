package domain

import (
	"context"
	"time"
)

type Sampler interface {
	Init() error
	Sample(ctx context.Context, intervalMs int) (SysSample, error)
	ThermalState() int
	Channels() map[string]bool
	Close() error
}

type ProcSource interface {
	Scan(ctx context.Context) ([]ProcSample, error)
	Close() error
}

type AgentSource interface {
	Name() string
	Poll(ctx context.Context, now time.Time, procs []ProcSample) ([]Session, error)
	Close() error
}
