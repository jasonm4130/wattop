package claude

import (
	"encoding/json"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// Event is what one transcript record (one line of a Claude Code
// <sessionId>.jsonl file, main or subagent) contributes: the model and
// usage of an assistant turn, the tool_use calls it made, the tool_use ids
// any tool_result in it resolves, and a rate-limit record if this line is
// one.
type Event struct {
	Type          string // the record's own "type": user | assistant | system | ...
	Timestamp     time.Time
	Model         string
	Usage         domain.Usage
	HasUsage      bool
	Tools         []domain.ToolCall
	ToolResultIDs []string
	RateLimit     *domain.RateLimit
}

type rawRecord struct {
	Type        string      `json:"type"`
	Timestamp   string      `json:"timestamp"`
	Message     *rawMessage `json:"message"`
	QuotaLimits *rawQuota   `json:"quotaLimits"`
}

type rawMessage struct {
	Model   string       `json:"model"`
	Role    string       `json:"role"`
	Content []rawContent `json:"content"`
	Usage   *rawUsage    `json:"usage"`
}

type rawContent struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	ToolUseID string `json:"tool_use_id"`
}

type rawUsage struct {
	InputTokens              int64             `json:"input_tokens"`
	OutputTokens             int64             `json:"output_tokens"`
	CacheReadInputTokens     int64             `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64             `json:"cache_creation_input_tokens"`
	CacheCreation            *rawCacheCreation `json:"cache_creation"`
	OutputTokensDetails      *rawOutputDetails `json:"output_tokens_details"`
}

type rawCacheCreation struct {
	Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
	Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
}

type rawOutputDetails struct {
	ThinkingTokens int64 `json:"thinking_tokens"`
}

type rawQuota struct {
	Status        string `json:"status"`
	RateLimitType string `json:"rateLimitType"`
	ResetsAt      string `json:"resetsAt"`
}

// fiveHourWindowMins is the window length wattop renders for Claude's
// "five_hour" quotaLimits rateLimitType — the only value observed on this
// machine. Claude's API does not name the window length itself.
const fiveHourWindowMins = 5 * 60

// ParseRecord decodes one JSONL line from a Claude Code transcript
// (main session or subagent) into an Event. It never returns an error for
// a record type it does not recognise — only for bytes that are not valid
// JSON at all, since a caller trying to parse a still-being-written
// partial line is a bug in the caller (Tail already buffers those), not
// something this function should paper over.
func ParseRecord(line []byte) (Event, error) {
	var raw rawRecord
	if err := json.Unmarshal(line, &raw); err != nil {
		return Event{}, err
	}

	ev := Event{Type: raw.Type}
	if raw.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, raw.Timestamp); err == nil {
			ev.Timestamp = t
		}
	}

	if raw.Message != nil {
		ev.Model = raw.Message.Model
		for _, c := range raw.Message.Content {
			switch c.Type {
			case "tool_use":
				ev.Tools = append(ev.Tools, domain.ToolCall{
					Name: c.Name,
					ID:   c.ID,
					At:   ev.Timestamp,
				})
			case "tool_result":
				if c.ToolUseID != "" {
					ev.ToolResultIDs = append(ev.ToolResultIDs, c.ToolUseID)
				}
			}
		}
		if raw.Message.Usage != nil {
			ev.HasUsage = true
			ev.Usage = usageFromRaw(raw.Message.Usage)
		}
	}

	if raw.QuotaLimits != nil && raw.QuotaLimits.RateLimitType == "five_hour" {
		var resetsAt time.Time
		if raw.QuotaLimits.ResetsAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, raw.QuotaLimits.ResetsAt); err == nil {
				resetsAt = t
			}
		}
		ev.RateLimit = &domain.RateLimit{
			Scope:      raw.QuotaLimits.RateLimitType,
			UsedPct:    nil, // Claude never reports a proactive usage percentage
			WindowMins: fiveHourWindowMins,
			ResetsAt:   resetsAt,
			Rejected:   raw.QuotaLimits.Status == "rejected",
		}
	}

	return ev, nil
}

func usageFromRaw(u *rawUsage) domain.Usage {
	out := domain.Usage{
		Input:     u.InputTokens,
		Output:    u.OutputTokens,
		CacheRead: u.CacheReadInputTokens,
	}
	if u.CacheCreation != nil {
		// The record breaks its cache write down by TTL tier — use it
		// exactly, even though the tiers may not sum to
		// CacheCreationInputTokens (they should, but the tiered fields are
		// what pricing needs).
		out.CacheCreate5m = u.CacheCreation.Ephemeral5m
		out.CacheCreate1h = u.CacheCreation.Ephemeral1h
	} else if u.CacheCreationInputTokens != 0 {
		// No tier breakdown present: Claude's default cache write TTL is
		// 5 minutes unless the 1h beta header is set, so an un-broken-down
		// write is priced as a 5m write rather than silently dropped.
		out.CacheCreate5m = u.CacheCreationInputTokens
	}
	if u.OutputTokensDetails != nil {
		out.Thinking = u.OutputTokensDetails.ThinkingTokens
	}
	return out
}
