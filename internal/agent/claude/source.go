package claude

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

var contextWindowLadder = []int64{200_000, 1_000_000}

func contextWindow(hwm int64) int64 {
	for _, tier := range contextWindowLadder {
		if hwm <= tier {
			return tier
		}
	}
	return contextWindowLadder[len(contextWindowLadder)-1]
}

const fiveHourRateLimitScope = "five_hour"

const maxRetainedTools = 64

type sessionAgg struct {
	usageAccounting
	tail               TailState
	model              string
	tools              []domain.ToolCall
	toolCounts         map[string]int
	resultedToolUseIDs map[string]bool
	rateLimit          *domain.RateLimit
	rateLimitAt        time.Time
	subagents          subagentTracker
	lastUsageAt        time.Time
	highWaterMark      int64
	lastPromptTokens   int64
	nextTranscriptScan time.Time
}

const transcriptRescanInterval = time.Minute

func (a *sessionAgg) resetAccumulators() {
	a.usageAccounting = usageAccounting{}
	a.tools = nil
	a.toolCounts = make(map[string]int)
	a.highWaterMark = 0
	a.lastPromptTokens = 0
}

type Source struct {
	sessionsDir string
	projectsDir string
	overrides   map[string]int64
	agg         map[string]*sessionAgg
}

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

func (s *Source) Poll(_ context.Context, now time.Time, _ []domain.ProcSample) ([]domain.Session, error) {
	files, err := ListSessionFiles(s.sessionsDir)
	if err != nil {
		return nil, err
	}

	live := make(map[string]bool, len(files))
	for _, f := range files {
		live[f.SessionID] = true
	}

	sessions := make([]domain.Session, 0, len(files))
	for _, f := range files {
		sess, err := s.pollOne(f, now)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}

	for id := range s.agg {
		if !live[id] {
			delete(s.agg, id)
		}
	}

	return sessions, nil
}

func (s *Source) pollOne(f SessionFile, now time.Time) (domain.Session, error) {
	agg := s.agg[f.SessionID]
	if agg == nil {
		agg = &sessionAgg{
			toolCounts:         make(map[string]int),
			resultedToolUseIDs: make(map[string]bool),
			subagents:          newSubagentTracker(),
		}
		s.agg[f.SessionID] = agg
	}

	if agg.tail.Path == "" && !now.Before(agg.nextTranscriptScan) {
		if path, ok := s.resolveTranscript(f.CWD, f.SessionID); ok {
			agg.tail.Path = path
		} else {
			agg.nextTranscriptScan = now.Add(transcriptRescanInterval)
		}
	}

	var res TailResult
	var err error
	if agg.tail.Path != "" {
		res, err = Tail(agg.tail)
		if err != nil && !os.IsNotExist(err) {
			return domain.Session{}, err
		}
	}
	var lines [][]byte
	if agg.tail.Path != "" && err == nil {
		if res.Reset {
			agg.resetAccumulators()
		}
		agg.tail = res.State
		lines = res.Lines
	}

	pid := f.PID
	status := f.Status
	statusSince := f.StatusUpdatedAt

	for _, line := range lines {
		ev, perr := ParseRecord(line)
		if perr != nil {
			continue
		}

		if realModel(ev.Model) {
			agg.model = ev.Model
		}
		if ev.HasUsage {
			agg.usageAccounting.add(ev, agg.model)
			if ev.Timestamp.After(agg.lastUsageAt) {
				agg.lastUsageAt = ev.Timestamp
			}

			if !ev.Usage.IsZero() {
				prompt := promptTokens(ev.Usage)
				agg.lastPromptTokens = prompt
				if prompt > agg.highWaterMark {
					agg.highWaterMark = prompt
				}
			}
		}
		for _, tc := range ev.Tools {
			agg.tools = append(agg.tools, tc)
			if len(agg.tools) > maxRetainedTools {
				agg.tools = agg.tools[len(agg.tools)-maxRetainedTools:]
			}
			agg.toolCounts[tc.Name]++
		}
		for _, id := range ev.ToolResultIDs {
			agg.resultedToolUseIDs[id] = true
		}
		agg.subagents.noteNotification(ev.Notification)
		if ev.RateLimit != nil && ev.RateLimit.Scope == fiveHourRateLimitScope {
			agg.rateLimit = ev.RateLimit
			agg.rateLimitAt = ev.Timestamp
		}
	}

	if agg.rateLimit != nil && !agg.rateLimit.ResetsAt.IsZero() && now.After(agg.rateLimit.ResetsAt) {
		agg.rateLimit = nil
		agg.rateLimitAt = time.Time{}
	}

	if agg.rateLimit != nil {
		status = domain.StatusRateLimited
		if !agg.rateLimitAt.IsZero() {
			statusSince = agg.rateLimitAt
		}
	}

	subagentsDir := filepath.Join(s.projectsDir, sanitizeCWD(f.CWD), f.SessionID, "subagents")
	subagents, workflows, childUsageAt, err := agg.subagents.collect(subagentsDir, now, agg.resultedToolUseIDs)
	if err != nil {
		return domain.Session{}, err
	}
	lastUsageAt := agg.lastUsageAt
	if childUsageAt.After(lastUsageAt) {
		lastUsageAt = childUsageAt
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
		TokenRate:    agg.window.Rate(now),
		Agent:        "claude",
		ID:           f.SessionID,
		PID:          &pid,
		BindConf:     domain.BindExact,
		CWD:          f.CWD,
		Name:         f.Name,
		Kind:         f.Kind,
		Status:       status,
		StatusSince:  statusSince,
		Model:        agg.model,
		Usage:        agg.usage,
		Ledger:       agg.ledger.Clone(),
		ContextUsed:  agg.lastPromptTokens,
		ContextMax:   ctxMax,
		ContextExact: false,
		Tools:        agg.tools,
		ToolCounts:   agg.toolCounts,
		Subagents:    subagents,
		Workflows:    workflows,
		LastUsageAt:  lastUsageAt,
		RateLimits:   rateLimits,
	}
	return sess, nil
}

func (s *Source) transcriptPath(cwd, sessionID string) string {
	return filepath.Join(s.projectsDir, sanitizeCWD(cwd), sessionID+".jsonl")
}

// A session resumed in a different cwd keeps its id but writes under the
// project directory it was resumed in, so the id is the fallback key.
func (s *Source) resolveTranscript(cwd, sessionID string) (string, bool) {
	derived := s.transcriptPath(cwd, sessionID)
	if _, err := os.Stat(derived); err == nil {
		return derived, true
	}
	matches, err := filepath.Glob(filepath.Join(s.projectsDir, "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return "", false
	}
	sort.Strings(matches)
	return matches[0], true
}

func sanitizeCWD(cwd string) string {
	var b strings.Builder
	b.Grow(len(cwd))
	for _, r := range cwd {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
