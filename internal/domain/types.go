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

// Bandwidth holds memory/ANE throughput in GB/s. A nil field means no source
// for it exists on this machine, never that the reading was zero: a resolved
// channel with no traffic is a pointer to 0.0.
//
// DRAMReadGBs and DRAMWriteGBs are set only where read and write were counted
// independently. Where the platform exposes a single figure for total traffic
// -- one combined counter, or a DRAM-power-derived estimate -- only
// DRAMCombinedGBs is set and the two directions stay nil, because splitting
// one number in half does not make it two measurements.
type Bandwidth struct {
	DRAMReadGBs  *float64 `json:"dram_read_gbs"`
	DRAMWriteGBs *float64 `json:"dram_write_gbs"`
	// DRAMCombinedGBs is total DRAM traffic: the sum when both directions were
	// counted, the counter's own figure when only a combined one exists.
	DRAMCombinedGBs *float64 `json:"dram_combined_gbs"`
	// DRAMEstimated marks DRAMCombinedGBs as derived rather than counted (on
	// Apple silicon, inferred from DRAM power via runtime calibration).
	// Renderers prefix an estimate with "~".
	DRAMEstimated  bool     `json:"dram_estimated"`
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

// UsageKey identifies one pricing bucket of a UsageLedger: the model a
// request ran on, and its prompt size in thousands of tokens.
//
// PromptK is ceil(promptTokens/1000). Pricing tiers are published as
// *_above_<N>k_tokens and apply when a request's prompt is strictly greater
// than N*1000 tokens; ceil preserves that comparison exactly, since
// promptTokens > N*1000 iff ceil(promptTokens/1000) > N. So 272000 tokens
// is PromptK 272 (not above 272k) and 272001 is PromptK 273 (above), and
// pricing PromptK*1000 selects the same tier the request's own size would.
type UsageKey struct {
	Model   string
	PromptK int64
}

// UsageLedger is usage bucketed by (model, prompt-size tier), so cost can be
// priced per request at that request's own model and long-context tier
// rather than cumulative usage at the latest model and prompt size. Sessions
// and subagents carry one alongside their cumulative Usage, which stays the
// display total. A nil ledger means the source did not record one.
type UsageLedger map[UsageKey]Usage

// PromptK converts a prompt size in tokens to UsageKey.PromptK:
// ceil(promptTokens/1000), and 0 for a non-positive size.
func PromptK(promptTokens int64) int64 {
	if promptTokens <= 0 {
		return 0
	}
	return (promptTokens + 999) / 1000
}

// Add accumulates u under (model, PromptK(promptTokens)). The ledger must be
// non-nil. A negative u (a correction to an earlier add) is applied as is.
func (l UsageLedger) Add(model string, promptTokens int64, u Usage) {
	k := UsageKey{Model: model, PromptK: PromptK(promptTokens)}
	l[k] = l[k].Plus(u)
}

// Merge adds every bucket of o into l. l must be non-nil.
func (l UsageLedger) Merge(o UsageLedger) {
	for k, u := range o {
		l[k] = l[k].Plus(u)
	}
}

// Clone returns an independent copy, or nil for a nil ledger. Sources hand
// out clones so a snapshot never aliases a map the next poll mutates.
func (l UsageLedger) Clone() UsageLedger {
	if l == nil {
		return nil
	}
	out := make(UsageLedger, len(l))
	for k, u := range l {
		out[k] = u
	}
	return out
}

// Plus returns the field-wise sum of u and o.
func (u Usage) Plus(o Usage) Usage {
	return Usage{
		Input:         u.Input + o.Input,
		Output:        u.Output + o.Output,
		CacheRead:     u.CacheRead + o.CacheRead,
		CacheCreate5m: u.CacheCreate5m + o.CacheCreate5m,
		CacheCreate1h: u.CacheCreate1h + o.CacheCreate1h,
		Thinking:      u.Thinking + o.Thinking,
		CachedInput:   u.CachedInput + o.CachedInput,
	}
}

// Minus returns the field-wise difference u - o.
func (u Usage) Minus(o Usage) Usage {
	return Usage{
		Input:         u.Input - o.Input,
		Output:        u.Output - o.Output,
		CacheRead:     u.CacheRead - o.CacheRead,
		CacheCreate5m: u.CacheCreate5m - o.CacheCreate5m,
		CacheCreate1h: u.CacheCreate1h - o.CacheCreate1h,
		Thinking:      u.Thinking - o.Thinking,
		CachedInput:   u.CachedInput - o.CachedInput,
	}
}

// IsZero reports whether every field is zero.
func (u Usage) IsZero() bool { return u == Usage{} }

// TokenRate is recorded transcript usage per second over the last 60 seconds.
// Input includes cache reads/writes; output includes reported reasoning.
type TokenRate struct {
	InputPerSec     float64 `json:"input_per_sec"`
	OutputPerSec    float64 `json:"output_per_sec"`
	CacheReadPerSec float64 `json:"cache_read_per_sec"`
}

// ToolCall is one tool invocation observed in a transcript.
type ToolCall struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
	ID   string    `json:"id"`
}

// Subagent status values. Done and Failed are set only from a definitive
// signal (a tool_result, a task-notification, a workflow journal record, a
// finished Codex turn); silence is never completion, so an unfinished child
// with no recent activity reads Idle rather than Done.
const (
	SubagentRunning = "running"
	SubagentIdle    = "idle"
	SubagentDone    = "done"
	SubagentFailed  = "failed"
)

// Subagent is one child invocation within a session: a Claude Agent/Task
// spawn, a Claude workflow agent, or a Codex spawned or guardian thread.
// Session.Subagents is flat; ParentID and WorkflowID carry the tree.
type Subagent struct {
	TokenRate   *TokenRate `json:"token_rate,omitempty"`
	ID          string     `json:"id"`
	ParentID    string     `json:"parent_id,omitempty"`
	WorkflowID  string     `json:"workflow_id,omitempty"`
	Phase       string     `json:"phase,omitempty"`
	Hash        string     `json:"hash"`
	AgentType   string     `json:"agent_type"`
	Description string     `json:"description"`
	Model       string     `json:"model"`
	ToolUseID   string     `json:"tool_use_id"`
	SpawnDepth  int        `json:"spawn_depth"`
	Background  bool       `json:"background"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"started_at"`
	// LastActivityAt is the newest transcript record timestamp, which is also
	// the burn tracker's event time for this child's usage.
	LastActivityAt time.Time `json:"last_activity_at"`
	// CurrentTool is the newest tool_use still awaiting its tool_result.
	CurrentTool string `json:"current_tool,omitempty"`
	ToolCalls   int    `json:"tool_calls"`
	// ContextUsed is this child's last-request prompt size (input plus cache
	// reads and writes), which selects its long-context pricing tier.
	ContextUsed int64 `json:"context_used"`
	Usage       Usage `json:"usage"`
	// Ledger is Usage bucketed per request by model and prompt tier, which
	// is what pricing reads when present. Not serialised: Usage is the
	// display total.
	Ledger       UsageLedger `json:"-"`
	Live         bool        `json:"live"` // Status == SubagentRunning
	CostUSD      *float64    `json:"cost_usd"`
	BurnUSDPerHr *float64    `json:"burn_usd_per_hr"`
}

// Workflow summarises one Claude workflow run (subagents/workflows/wf_*):
// its agents are the Session.Subagents carrying this WorkflowID. Counts and
// times come from the source; CostUSD and BurnUSDPerHr are summed from those
// subagents by the reducer.
type Workflow struct {
	TokenRate      *TokenRate `json:"token_rate,omitempty"`
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	Phase          string     `json:"phase,omitempty"` // newest phase any agent started in
	Agents         int        `json:"agents"`
	Running        int        `json:"running"`
	Done           int        `json:"done"`
	Failed         int        `json:"failed"`
	StartedAt      time.Time  `json:"started_at"`
	LastActivityAt time.Time  `json:"last_activity_at"`
	Usage          Usage      `json:"usage"`
	CostUSD        *float64   `json:"cost_usd"`
	CostPartial    bool       `json:"cost_partial"` // CostUSD omits an unpriced agent
	BurnUSDPerHr   *float64   `json:"burn_usd_per_hr"`
}

// Session is one agent session (Claude or Codex), joined to a process where
// binding succeeded.
type Session struct {
	TokenRate    *TokenRate     `json:"token_rate,omitempty"`
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
	Ledger       UsageLedger    `json:"-"` // Usage per request by model and prompt tier; see Subagent.Ledger
	CostUSD      *float64       `json:"cost_usd"`
	BurnUSDPerHr *float64       `json:"burn_usd_per_hr"`
	Priced       bool           `json:"priced"`
	CostPartial  bool           `json:"cost_partial"` // CostUSD omits an unpriced subagent
	ContextUsed  int64          `json:"context_used"`
	ContextMax   int64          `json:"context_max"`
	ContextExact bool           `json:"context_exact"`
	Tools        []ToolCall     `json:"tools"`
	ToolCounts   map[string]int `json:"tool_counts"`
	Subagents    []Subagent     `json:"subagents"`
	Workflows    []Workflow     `json:"workflows"`
	// LastUsageAt is the newest usage-record timestamp in this session's own
	// transcript or any child's. The burn tracker times cost deltas by it, so
	// a parent blocked on a subagent still burns at the child's rate.
	LastUsageAt time.Time   `json:"last_usage_at"`
	Proc        *ProcSample `json:"proc"`
	RateLimits  []RateLimit `json:"rate_limits"`
}
