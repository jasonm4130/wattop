package claude

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// Event is what one transcript record (one line of a Claude Code
// <sessionId>.jsonl file, main or subagent) contributes: the model and
// usage of an assistant turn, the tool_use calls it made, the tool_use ids
// any tool_result in it resolves, and a rate-limit record if this line is
// one.
type Event struct {
	MessageID     string
	Type          string // the record's own "type": user | assistant | system | ...
	Timestamp     time.Time
	Model         string
	Usage         domain.Usage
	HasUsage      bool
	Tools         []domain.ToolCall
	ToolResultIDs []string
	RateLimit     *domain.RateLimit
	// Notification is set for a queue-operation enqueue record carrying a
	// <task-notification>: the completion signal of a background task.
	Notification *TaskNotification
}

// TaskNotification is the part of a background task-notification wattop
// correlates on. ToolUseID is the tool_use that launched the task; Status is
// the <status> text verbatim ("completed" for a clean finish).
type TaskNotification struct {
	TaskID    string
	ToolUseID string
	Status    string
}

type rawRecord struct {
	Type        string          `json:"type"`
	Operation   string          `json:"operation"`
	Timestamp   string          `json:"timestamp"`
	Content     json.RawMessage `json:"content"`
	Message     *rawMessage     `json:"message"`
	QuotaLimits *rawQuota       `json:"quotaLimits"`
}

type rawMessage struct {
	ID      string      `json:"id"`
	Model   string      `json:"model"`
	Role    string      `json:"role"`
	Content rawContents `json:"content"`
	Usage   *rawUsage   `json:"usage"`
}

// rawContents is message.content, which is an array of blocks on most
// records but a plain string on some user records. A string (or null)
// decodes to no blocks rather than failing the whole record, so those
// records still contribute their timestamp.
type rawContents []rawContent

func (c *rawContents) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '[' {
		*c = nil
		return nil
	}
	var blocks []rawContent
	if err := json.Unmarshal(b, &blocks); err != nil {
		return err
	}
	*c = blocks
	return nil
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

var (
	notificationTaskIDPattern    = regexp.MustCompile(`<task-id>([^<]*)</task-id>`)
	notificationToolUseIDPattern = regexp.MustCompile(`<tool-use-id>([^<]*)</tool-use-id>`)
	notificationStatusPattern    = regexp.MustCompile(`<status>([^<]*)</status>`)
)

// parseTaskNotification pulls the correlating tags out of a queue-operation
// enqueue record's content string. It returns nil when the content carries
// no <task-notification> at all (an ordinary queued prompt).
func parseTaskNotification(content string) *TaskNotification {
	tag := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(content); m != nil {
			return m[1]
		}
		return ""
	}
	n := TaskNotification{
		TaskID:    tag(notificationTaskIDPattern),
		ToolUseID: tag(notificationToolUseIDPattern),
		Status:    tag(notificationStatusPattern),
	}
	if n == (TaskNotification{}) {
		return nil
	}
	return &n
}

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
		ev.MessageID = raw.Message.ID
		if ev.MessageID == "" {
			ev.MessageID = fmt.Sprintf("%x", sha256.Sum256(line))
		}
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

	if raw.Type == "queue-operation" && raw.Operation == "enqueue" && len(raw.Content) > 0 {
		var content string
		if err := json.Unmarshal(raw.Content, &content); err == nil {
			ev.Notification = parseTaskNotification(content)
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
