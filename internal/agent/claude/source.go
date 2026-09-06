package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// contextWindowLadder is the fixed tier ladder context fill is estimated
// against, since Claude gives no denominator of its own. This is a package
// constant, never a lookup against a pricing/model table — see the Task 8
// spec for why max_input_tokens would be the wrong ceiling even if this
// package imported the pricing table, which it must not.
var contextWindowLadder = []int64{200_000, 500_000, 1_000_000}

// contextWindow returns the smallest ladder entry that is >= hwm (the
// observed high-water-mark prompt token count), so the estimate
// self-corrects upward as a session grows instead of shrinking back down.
func contextWindow(hwm int64) int64 {
	for _, tier := range contextWindowLadder {
		if hwm <= tier {
			return tier
		}
	}
	return contextWindowLadder[len(contextWindowLadder)-1]
}

// fiveHourRateLimitScope is the RateLimit.Scope value ParseRecord sets for
// a quotaLimits record with rateLimitType "five_hour" — the only value
// Claude has been observed to emit.
const fiveHourRateLimitScope = "five_hour"

// sessionAgg is what Source remembers across polls for one live session id:
// the tail position into its transcript, everything accumulated from the
// lines read so far, and the running high-water mark that drives the
// context-fill estimate.
type sessionAgg struct {
	tail               TailState
	model              string
	usage              domain.Usage
	tools              []domain.ToolCall
	toolCounts         map[string]int
	resultedToolUseIDs map[string]bool
	rateLimit          *domain.RateLimit
	rateLimitAt        time.Time
	highWaterMark      int64
	lastPromptTokens   int64
}

// Source implements domain.AgentSource for Claude Code by polling
// ~/.claude/sessions/ and tailing each live session's transcript under
// ~/.claude/projects/. It ignores the procs argument entirely: Claude's own
// session file already carries the pid, so the process table adds nothing
// here.
type Source struct {
	sessionsDir string
	projectsDir string
	// overrides is a per-session context-window override, keyed by either
	// session id or cwd (Task 13 reads it from config either way).
	overrides map[string]int64
	agg       map[string]*sessionAgg
}

// NewSource constructs a Source polling sessionsDir (~/.claude/sessions)
// and reading transcripts under projectsDir (~/.claude/projects).
func NewSource(sessionsDir, projectsDir string, overrides map[string]int64) *Source {
	return &Source{
		sessionsDir: sessionsDir,
		projectsDir: projectsDir,
		overrides:   overrides,
		agg:         make(map[string]*sessionAgg),
	}
}

func (s *Source) Name() string { return "claude" }

func (s *Source) Close() error { return nil }

// Poll emits only the sessions that exist right now — when a Claude
// process is killed, ~/.claude/sessions/<pid>.json disappears and the next
// poll simply does not return that session. It deliberately implements no
// grace period or stale transition: internal/state's reducer owns session
// retention because it already holds the previous cycle's state.
func (s *Source) Poll(_ context.Context, now time.Time, _ []domain.ProcSample) ([]domain.Session, error) {
	files, err := ListSessionFiles(s.sessionsDir)
	if err != nil {
		return nil, err
	}

	sessions := make([]domain.Session, 0, len(files))
	for _, f := range files {
		sess, err := s.pollOne(f, now)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, nil
}

func (s *Source) pollOne(f SessionFile, now time.Time) (domain.Session, error) {
	agg := s.agg[f.SessionID]
	if agg == nil {
		agg = &sessionAgg{
			toolCounts:         make(map[string]int),
			resultedToolUseIDs: make(map[string]bool),
		}
		s.agg[f.SessionID] = agg
	}

	transcriptPath := s.transcriptPath(f.CWD, f.SessionID)
	if agg.tail.Path == "" {
		agg.tail.Path = transcriptPath
	}

	newTail, lines, err := Tail(agg.tail)
	if err != nil && !os.IsNotExist(err) {
		return domain.Session{}, err
	}
	if err == nil {
		agg.tail = newTail
	}

	pid := f.PID
	status := f.Status
	statusSince := f.StatusUpdatedAt

	for _, line := range lines {
		ev, perr := ParseRecord(line)
		if perr != nil {
			// A still-partial line should already be held back by Tail;
			// anything else that fails to parse is skipped rather than
			// failing the whole poll.
			continue
		}

		if ev.Model != "" {
			agg.model = ev.Model
		}
		if ev.HasUsage {
			agg.usage.Input += ev.Usage.Input
			agg.usage.Output += ev.Usage.Output
			agg.usage.CacheRead += ev.Usage.CacheRead
			agg.usage.CacheCreate5m += ev.Usage.CacheCreate5m
			agg.usage.CacheCreate1h += ev.Usage.CacheCreate1h
			agg.usage.Thinking += ev.Usage.Thinking

			prompt := ev.Usage.Input + ev.Usage.CacheRead + ev.Usage.CacheCreate5m + ev.Usage.CacheCreate1h
			agg.lastPromptTokens = prompt
			if prompt > agg.highWaterMark {
				agg.highWaterMark = prompt
			}
		}
		for _, tc := range ev.Tools {
			agg.tools = append(agg.tools, tc)
			agg.toolCounts[tc.Name]++
		}
		for _, id := range ev.ToolResultIDs {
			agg.resultedToolUseIDs[id] = true
		}
		if ev.RateLimit != nil && ev.RateLimit.Scope == fiveHourRateLimitScope {
			agg.rateLimit = ev.RateLimit
			agg.rateLimitAt = ev.Timestamp
		}
	}

	if agg.rateLimit != nil {
		status = "rate-limited"
		if !agg.rateLimitAt.IsZero() {
			statusSince = agg.rateLimitAt
		}
	}

	subagentsDir := filepath.Join(s.projectsDir, sanitizeCWD(f.CWD), f.SessionID, "subagents")
	subagents, err := Walk(subagentsDir, agg.resultedToolUseIDs)
	if err != nil {
		return domain.Session{}, err
	}

	ctxMax := contextWindow(agg.highWaterMark)
	if override, ok := s.overrides[f.SessionID]; ok {
		ctxMax = override
	} else if override, ok := s.overrides[f.CWD]; ok {
		ctxMax = override
	}

	var rateLimits []domain.RateLimit
	if agg.rateLimit != nil {
		rateLimits = append(rateLimits, *agg.rateLimit)
	}

	sess := domain.Session{
		Agent:        "claude",
		ID:           f.SessionID,
		PID:          &pid,
		BindConf:     "exact", // Claude's own session file names its pid directly
		CWD:          f.CWD,
		Name:         f.Name,
		Kind:         f.Kind,
		Status:       status,
		StatusSince:  statusSince,
		Model:        agg.model,
		Usage:        agg.usage,
		ContextUsed:  agg.lastPromptTokens,
		ContextMax:   ctxMax,
		ContextExact: false,
		Tools:        agg.tools,
		ToolCounts:   agg.toolCounts,
		Subagents:    subagents,
		RateLimits:   rateLimits,
	}
	return sess, nil
}

// transcriptPath returns ~/.claude/projects/<cwd with '/' -> '-'>/<sessionId>.jsonl
// — a sibling of the <sessionId>/ directory, not inside it.
func (s *Source) transcriptPath(cwd, sessionID string) string {
	return filepath.Join(s.projectsDir, sanitizeCWD(cwd), sessionID+".jsonl")
}

func sanitizeCWD(cwd string) string {
	return strings.ReplaceAll(cwd, "/", "-")
}
