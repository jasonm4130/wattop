package codex

import (
	"encoding/json"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// record is the envelope shared by every Codex rollout line.
type record struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
}

type turnContextPayload struct {
	Model string `json:"model"`
	CWD   string `json:"cwd"`
}

// tokenUsage is the shape of both total_token_usage and last_token_usage.
// Fields are copied raw: CachedInputTokens is a SUBSET of InputTokens, not
// additive (domain.Usage.CachedInput carries the same contract), and
// ReasoningOutputTokens is already inside OutputTokens — proved arithmetically
// against the full-turn fixture (input 5200 + output 180 == total 5380, with
// reasoning 40 not added on top). Never subtract or add these here; Task 7's
// cost.go is what computes billable_input = input − cached_input.
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
	ResetsAt      int64    `json:"resets_at"` // unix seconds
}

type rateLimitsPayload struct {
	Primary   *rateLimitWindow `json:"primary"`
	Secondary *rateLimitWindow `json:"secondary"`
}

// eventMsgPayload covers the event_msg.payload.type values this package
// reads: task_started, task_complete, token_count. thread_settings_applied
// is accepted (it un-marshals into the same struct) but carries nothing this
// package needs yet.
type eventMsgPayload struct {
	Type string `json:"type"`

	// task_started carries the context window at the top level of its own
	// payload — not nested under "info", which is a token_count-only shape.
	ModelContextWindow int64 `json:"model_context_window"`

	// token_count only.
	Info       *tokenCountInfo    `json:"info"`
	RateLimits *rateLimitsPayload `json:"rate_limits"`
}

// Apply parses one rollout line and folds it into r. Unknown record types
// and fields are ignored rather than erroring, since a rollout schema this
// under-verified will drift and an unrecognised event should not crash the
// tailer.
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
			// busy iff the most recent of task_started/task_complete is
			// task_started.
			r.Status = "busy"
			if r.ContextMax == 0 && p.ModelContextWindow > 0 {
				// Fallback only: a token_count event's info.model_context_window
				// takes precedence where present (applied below), since it is
				// paired with an actual total_tokens numerator.
				r.ContextMax = p.ModelContextWindow
			}
		case "task_complete":
			r.Status = "waiting"
		case "token_count":
			if p.Info != nil {
				u := p.Info.TotalTokenUsage
				r.Usage = domain.Usage{
					Input:         u.InputTokens,
					Output:        u.OutputTokens,
					CachedInput:   u.CachedInputTokens,
					CacheCreate5m: u.CacheWriteInputTokens, // Codex reports one cache-write figure with no 5m/1h tier; bucketed here for cost.go's sake.
					Thinking:      u.ReasoningOutputTokens,
				}
				// ContextUsed is current context occupancy, not cumulative
				// session spend: last_token_usage.total_tokens reflects the most
				// recent turn's context, while total_token_usage.total_tokens sums
				// every turn and can exceed model_context_window many times over on
				// a long session. r.Usage above stays sourced from total_token_usage
				// since cost is correctly cumulative.
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
	}

	return nil
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

// parseCodexTime parses a rollout record's top-level "timestamp" field
// (RFC3339 with millisecond fraction, e.g. "2026-09-06T09:00:00.000Z") as
// UTC. Both sides of every timestamp comparison in this package are UTC:
// the filename embeds local time, but every record inside is UTC.
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

// inferStatus applies the mtime-derived staleness rule on top of the
// task_started/task_complete-derived base status: stale if the file's mtime
// exceeds the idle threshold, regardless of which task event was last seen.
func inferStatus(base string, mtime, now time.Time, idleThreshold time.Duration) string {
	if base == "" {
		base = "unknown"
	}
	if !mtime.IsZero() && now.Sub(mtime) > idleThreshold {
		return "stale"
	}
	return base
}

// LoadRollout reads path in full and folds every line into a fresh Rollout.
// It is the non-incremental counterpart to the tailState-driven path
// source.go uses on a running poll loop, and it is what the tests in this
// package exercise directly against the committed fixtures.
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
			// A malformed or truncated final line is buffered-and-retried by
			// the tailer in production; LoadRollout reads a whole file at
			// once, so it simply skips a line it cannot parse rather than
			// failing the whole rollout.
			continue
		}
	}
	return r, nil
}
