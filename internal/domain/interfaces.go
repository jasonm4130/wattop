package domain

import (
	"context"
	"time"
)

// Sampler is the SoC telemetry seam. Implementations live under
// internal/soc (real, CGO) and internal/collect/replay (fake, fixture-driven).
type Sampler interface {
	Init() error
	Sample(ctx context.Context, intervalMs int) (SysSample, error)
	ThermalState() int
	Channels() map[string]bool // for `doctor`
	Close() error
}

// ProcSource is the process-table seam. Implementations live under
// internal/proc (real) and internal/collect/replay (fake).
type ProcSource interface {
	Scan(ctx context.Context) ([]ProcSample, error)
	Close() error
}

// AgentSource is the per-agent session seam. Poll takes the process table as
// an argument rather than reading it itself: Codex has no per-pid session
// file, so binding a rollout to a pid needs each candidate's argv, cwd and
// start time, and passing them in keeps internal/agent/* free of syscalls
// and therefore testable on Linux CI. Every implementation — replay.Source,
// claude.Source, codex.Source and the agent-watcher in cmd/wattop/run.go —
// uses this three-argument form.
type AgentSource interface {
	Name() string
	Poll(ctx context.Context, now time.Time, procs []ProcSample) ([]Session, error)
	Close() error
}
