package codex

import (
	"encoding/json"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

type record struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	SessionID      string          `json:"session_id"`
	ID             string          `json:"id"`
	ParentThreadID string          `json:"parent_thread_id"`
	CWD            string          `json:"cwd"`
	AgentNickname  string          `json:"agent_nickname"`
	AgentPath      string          `json:"agent_path"`
	Source         json.RawMessage `json:"source"`
}

// subagentSource is session_meta.payload.source when it names a child
// thread. A top-level (non-child) rollout's source is a plain string
// ("vscode", "exec", ...) and fails to unmarshal into this shape, which is
// treated as "not a child" rather than an error.
type subagentSource struct {
	Subagent *struct {
		ThreadSpawn *struct {
			ParentThreadID string  `json:"parent_thread_id"`
			Depth          int     `json:"depth"`
			AgentPath      string  `json:"agent_path"`
			AgentNickname  string  `json:"agent_nickname"`
			AgentRole      *string `json:"agent_role"`
		} `json:"thread_spawn"`
		Other string `json:"other"`
	} `json:"subagent"`
}

type turnContextPayload struct {
	Model string `json:"model"`
	CWD   string `json:"cwd"`
}

// Codex reports CachedInputTokens as a subset of InputTokens and
// ReasoningOutputTokens as already inside OutputTokens.
type tokenUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
}

type tokenCountInfo struct {
	TotalTokenUsage    tokenUsage `json:"total_token_usage"`
	LastTokenUsage     tokenUsage `json:"last_token_usage"`
	ModelContextWindow int64      `json:"model_context_window"`
}

type rateLimitWindow struct {
	UsedPercent   *float64 `json:"used_percent"`
	WindowMinutes int      `json:"window_minutes"`
	ResetsAt      int64    `json:"resets_at"`
}

type rateLimitsPayload struct {
	Primary   *rateLimitWindow `json:"primary"`
	Secondary *rateLimitWindow `json:"secondary"`
}

type eventMsgPayload struct {
	Type string `json:"type"`

	ModelContextWindow int64 `json:"model_context_window"`

	Info       *tokenCountInfo    `json:"info"`
	RateLimits *rateLimitsPayload `json:"rate_limits"`
}

type responseItemPayload struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	CallID string `json:"call_id"`
}

type pendingCall struct {
	ID   string
	Name string
}

func (r *Rollout) Apply(line []byte) error {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil {
		return err
	}

	recAt, _ := parseCodexTime(rec.Timestamp)

	switch rec.Type {
	case "session_meta":
		var p sessionMetaPayload
		if err := json.Unmarshal(rec.Payload, &p); err != nil {
			return err
		}
		if p.SessionID != "" {
			r.SessionID = p.SessionID
		}
		if p.CWD != "" {
			r.CWD = p.CWD
		}
		if !recAt.IsZero() {
			r.MetaAt = recAt
		}
		if p.ID != "" {
			r.ThreadID = p.ID
		}
		if p.ParentThreadID != "" {
			r.ParentThreadID = p.ParentThreadID
		}
		if p.AgentNickname != "" {
			r.Nickname = p.AgentNickname
		}
		if p.AgentPath != "" {
			r.AgentPath = p.AgentPath
		}
		if len(p.Source) > 0 {
			var src subagentSource
			if err := json.Unmarshal(p.Source, &src); err == nil && src.Subagent != nil {
				switch {
				case src.Subagent.Other == "guardian":
					r.AgentType = "guardian"
					r.SpawnDepth = 1
				case src.Subagent.ThreadSpawn != nil:
					ts := src.Subagent.ThreadSpawn
					if r.ParentThreadID == "" && ts.ParentThreadID != "" {
						r.ParentThreadID = ts.ParentThreadID
					}
					r.SpawnDepth = ts.Depth
					if ts.AgentPath != "" {
						r.AgentPath = ts.AgentPath
					}
					if ts.AgentNickname != "" {
						r.Nickname = ts.AgentNickname
					}
					if ts.AgentRole != nil && *ts.AgentRole != "" {
						r.AgentType = *ts.AgentRole
					} else {
						r.AgentType = "spawn"
					}
				}
			}
		}

	case "turn_context":
		var p turnContextPayload
		if err := json.Unmarshal(rec.Payload, &p); err != nil {
			return err
		}
		// Read per turn, never from ~/.codex/config.toml: codex exec -m
		// <model> overrides the pinned default, and this is the only place
		// that override is visible.
		if p.Model != "" {
			r.Model = p.Model
		}
		if p.CWD != "" {
			r.CWD = p.CWD
		}

	case "event_msg":
		var p eventMsgPayload
		if err := json.Unmarshal(rec.Payload, &p); err != nil {
			return err
		}
		switch p.Type {
		case "task_started":
			r.Status = domain.StatusBusy
			r.TaskCompleted = false
			if r.ContextMax == 0 && p.ModelContextWindow > 0 {
				r.ContextMax = p.ModelContextWindow
			}
		case "task_complete":
			r.Status = domain.StatusWaiting
			r.TaskCompleted = true
		case "token_count":
			if !recAt.IsZero() && recAt.Before(r.lastUsageAt) {
				break // replayed prefix after file rotation, not new usage
			}
			if p.Info != nil {
				u := p.Info.TotalTokenUsage
				input, output, cached := u.InputTokens-r.Usage.Input, u.OutputTokens-r.Usage.Output, u.CachedInputTokens-r.Usage.CachedInput
				if !r.hasUsage || input < 0 || output < 0 || cached < 0 {
					last := p.Info.LastTokenUsage
					input, output, cached = last.InputTokens, last.OutputTokens, last.CachedInputTokens
				}
				if !r.hasUsage || input != 0 || output != 0 || cached != 0 {
					r.tokens.Add(recAt, input, output, cached)
				}
				total := domain.Usage{
					Input:         u.InputTokens,
					Output:        u.OutputTokens,
					CachedInput:   u.CachedInputTokens,
					CacheCreate5m: u.CacheWriteInputTokens, // Codex reports one cache-write figure with no 5m/1h tier
					Thinking:      u.ReasoningOutputTokens,
				}
				r.fileLedger(p.Info, total)
				r.hasUsage = true
				if !recAt.IsZero() {
					r.lastUsageAt = recAt
				}
				r.Usage = total
				// total_token_usage sums every turn, so ContextUsed comes from the last request.
				if p.Info.LastTokenUsage.TotalTokens > 0 {
					r.ContextUsed = p.Info.LastTokenUsage.TotalTokens
				} else {
					r.ContextUsed = u.TotalTokens
				}
				if p.Info.ModelContextWindow > 0 {
					r.ContextMax = p.Info.ModelContextWindow
					r.ContextExact = true
				}
			}
			if p.RateLimits != nil {
				r.RateLimits = rateLimitsToDomain(p.RateLimits)
			}
		}

	case "response_item":
		var p responseItemPayload
		if err := json.Unmarshal(rec.Payload, &p); err != nil {
			return err
		}
		switch p.Type {
		case "function_call":
			r.ToolCalls++
			r.pendingCalls = append(r.pendingCalls, pendingCall{ID: p.CallID, Name: p.Name})
			r.CurrentTool = p.Name
		case "function_call_output":
			for i, pc := range r.pendingCalls {
				if pc.ID == p.CallID {
					r.pendingCalls = append(r.pendingCalls[:i], r.pendingCalls[i+1:]...)
					break
				}
			}
			if n := len(r.pendingCalls); n > 0 {
				r.CurrentTool = r.pendingCalls[n-1].Name
			} else {
				r.CurrentTool = ""
			}
		}
	}

	return nil
}

func (r *Rollout) fileLedger(info *tokenCountInfo, total domain.Usage) {
	prompt := info.LastTokenUsage.InputTokens
	if prompt <= 0 {
		prompt = info.LastTokenUsage.TotalTokens
		if prompt <= 0 {
			prompt = info.TotalTokenUsage.TotalTokens
		}
	}
	delta := total.Minus(r.Usage)
	reset := delta.Input < 0 || delta.Output < 0 || delta.CachedInput < 0
	if !r.hasUsage || reset || r.Ledger == nil {
		r.Ledger = make(domain.UsageLedger)
		delta = total
	}
	if delta.IsZero() {
		return
	}
	r.Ledger.Add(r.Model, prompt, delta)
}

func rateLimitsToDomain(p *rateLimitsPayload) []domain.RateLimit {
	var out []domain.RateLimit
	if p.Primary != nil {
		out = append(out, domain.RateLimit{
			Scope:      "primary",
			UsedPct:    p.Primary.UsedPercent,
			WindowMins: p.Primary.WindowMinutes,
			ResetsAt:   time.Unix(p.Primary.ResetsAt, 0).UTC(),
		})
	}
	if p.Secondary != nil {
		out = append(out, domain.RateLimit{
			Scope:      "secondary",
			UsedPct:    p.Secondary.UsedPercent,
			WindowMins: p.Secondary.WindowMinutes,
			ResetsAt:   time.Unix(p.Secondary.ResetsAt, 0).UTC(),
		})
	}
	return out
}

func parseCodexTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}

func inferStatus(base domain.SessionStatus, mtime, now time.Time, idleThreshold time.Duration) domain.SessionStatus {
	if base == "" {
		base = domain.StatusUnknown
	}
	if !mtime.IsZero() && now.Sub(mtime) > idleThreshold {
		return domain.StatusStale
	}
	return base
}

func LoadRollout(path string) (*Rollout, error) {
	lines, err := readAllLines(path)
	if err != nil {
		return nil, err
	}

	r := &Rollout{Path: path}
	if mt, err := statModTime(path); err == nil {
		r.ModTime = mt
	}
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		if err := r.Apply(line); err != nil {
			continue
		}
	}
	return r, nil
}
