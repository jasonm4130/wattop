package domain

import "time"

// Cluster describes one CPU cluster (performance, efficiency or special) as
// reported by the platform's own topology — never a hardcoded "P"/"E" pair.
type Cluster struct {
	Label      string    `json:"label"`
	CoreCount  int       `json:"core_count"`
	ActivePct  *float64  `json:"active_pct"`
	FreqMHz    *float64  `json:"freq_mhz"`
	CoreActive []float64 `json:"core_active"`
}

// Power holds instantaneous power draw in watts. Every field is optional:
// a channel that does not resolve on a given chip yields nil, not zero.
type Power struct {
	CPUWatts    *float64 `json:"cpu_watts"`
	GPUWatts    *float64 `json:"gpu_watts"`
	ANEWatts    *float64 `json:"ane_watts"`
	DRAMWatts   *float64 `json:"dram_watts"`
	SystemWatts *float64 `json:"system_watts"`
}

// Bandwidth holds memory/ANE throughput in GB/s. All nil on chips where the
// counter latch never resolves (see Decision, deviation 1).
type Bandwidth struct {
	DRAMReadGBs    *float64 `json:"dram_read_gbs"`
	DRAMWriteGBs   *float64 `json:"dram_write_gbs"`
	ANECombinedGBs *float64 `json:"ane_combined_gbs"`
}

// Fan is one fan's reading. Label falls back to an SMC key when the platform
// does not name it.
type Fan struct {
	Label  string   `json:"label"`
	RPM    float64  `json:"rpm"`
	MinRPM *float64 `json:"min_rpm"`
	MaxRPM *float64 `json:"max_rpm"`
}

// MemorySample is bytes throughout — never MB or pages.
type MemorySample struct {
	TotalBytes     uint64 `json:"total_bytes"`
	UsedBytes      uint64 `json:"used_bytes"`
	AvailableBytes uint64 `json:"available_bytes"`
	SwapTotalBytes uint64 `json:"swap_total_bytes"`
	SwapUsedBytes  uint64 `json:"swap_used_bytes"`
}

// NetSample is system-wide network throughput.
type NetSample struct {
	InBytesPerSec  float64 `json:"in_bytes_per_sec"`
	OutBytesPerSec float64 `json:"out_bytes_per_sec"`
}

// DiskSample is system-wide disk throughput.
type DiskSample struct {
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
}

// RateLimit is one agent-reported usage window. UsedPct is nil for sources
// (Claude) that do not supply it; Rejected marks a window learned only from
// a rejection response rather than a proactive report.
type RateLimit struct {
	Scope      string    `json:"scope"`
	UsedPct    *float64  `json:"used_pct"`
	WindowMins int       `json:"window_mins"`
	ResetsAt   time.Time `json:"resets_at"`
	Rejected   bool      `json:"rejected"`
}

// ProcSample is one process's snapshot from the process table. Comm is
// display-only — for a live Claude pid it is a version string, not a
// process name — so Argv is the only reliable way to recognise an agent.
type ProcSample struct {
	PID          int       `json:"pid"`
	Comm         string    `json:"comm"`
	Argv         []string  `json:"argv"`
	CWD          string    `json:"cwd"`
	RSSBytes     uint64    `json:"rss_bytes"`
	CPUPct       float64   `json:"cpu_pct"`
	DiskReadB    uint64    `json:"disk_read_b"`
	DiskWriteB   uint64    `json:"disk_write_b"`
	StartTime    time.Time `json:"start_time"`
	GPUMsPerSec  *float64  `json:"gpu_ms_per_sec"`
	GPUPctApprox *float64  `json:"gpu_pct_approx"`
}

// Usage is token accounting shared by sessions and subagents.
// CachedInput is Codex-only and is a subset of Input, not additive.
type Usage struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	CacheRead     int64 `json:"cache_read"`
	CacheCreate5m int64 `json:"cache_create_5m"`
	CacheCreate1h int64 `json:"cache_create_1h"`
	Thinking      int64 `json:"thinking"`
	CachedInput   int64 `json:"cached_input"`
}

// ToolCall is one tool invocation observed in a transcript.
type ToolCall struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
	ID   string    `json:"id"`
}

// Subagent is one child (Task) invocation within a Claude session.
type Subagent struct {
	Hash        string   `json:"hash"`
	AgentType   string   `json:"agent_type"`
	Description string   `json:"description"`
	Model       string   `json:"model"`
	ToolUseID   string   `json:"tool_use_id"`
	SpawnDepth  int      `json:"spawn_depth"`
	Usage       Usage    `json:"usage"`
	Live        bool     `json:"live"`
	CostUSD     *float64 `json:"cost_usd"`
}

// Session is one agent session (Claude or Codex), joined to a process where
// binding succeeded.
type Session struct {
	Agent        string         `json:"agent"`
	ID           string         `json:"id"`
	PID          *int           `json:"pid"`
	BindConf     string         `json:"bind_conf"`
	CWD          string         `json:"cwd"`
	Name         string         `json:"name"`
	Cmdline      string         `json:"cmdline"`
	Kind         string         `json:"kind"`
	Status       string         `json:"status"`
	StatusSince  time.Time      `json:"status_since"`
	Model        string         `json:"model"`
	Usage        Usage          `json:"usage"`
	CostUSD      *float64       `json:"cost_usd"`
	BurnUSDPerHr *float64       `json:"burn_usd_per_hr"`
	Priced       bool           `json:"priced"`
	ContextUsed  int64          `json:"context_used"`
	ContextMax   int64          `json:"context_max"`
	ContextExact bool           `json:"context_exact"`
	Tools        []ToolCall     `json:"tools"`
	ToolCounts   map[string]int `json:"tool_counts"`
	Subagents    []Subagent     `json:"subagents"`
	Proc         *ProcSample    `json:"proc"`
	RateLimits   []RateLimit    `json:"rate_limits"`
}
