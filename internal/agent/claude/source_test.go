package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func sourceFixture(t *testing.T) (*Source, string) {
	s, transcript, _ := sourceFixtureWithSubagents(t)
	return s, transcript
}

func sourceFixtureWithSubagents(t *testing.T) (*Source, string, string) {
	t.Helper()
	sessionsDir := filepath.Join(t.TempDir(), "sessions")
	projectsDir := filepath.Join(t.TempDir(), "projects")
	cwd := "/repo/x"
	projectDir := filepath.Join(projectsDir, sanitizeCWD(cwd))
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}

	writeFile(t, filepath.Join(sessionsDir, "1234.json"),
		`{"pid":1234,"sessionId":"sess-1","cwd":"`+cwd+`","status":"busy","kind":"interactive",`+
			`"entrypoint":"cli","name":"work","updatedAt":1757116800000,"statusUpdatedAt":1757116800000}`)

	subagentsDir := filepath.Join(projectDir, "sess-1", "subagents")
	if err := os.MkdirAll(subagentsDir, 0o755); err != nil {
		t.Fatalf("mkdir subagents: %v", err)
	}

	return NewSource(sessionsDir, projectsDir, nil), filepath.Join(projectDir, "sess-1.jsonl"), subagentsDir
}

func assistantRecord(inputTokens int, tool, padding string) string {
	return fmt.Sprintf(
		`{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"model":"claude-opus-5",`+
			`"role":"assistant","content":[{"type":"tool_use","id":"tu-%d","name":%q}],`+
			`"usage":{"input_tokens":%d,"output_tokens":1}},"pad":%q}`+"\n",
		inputTokens, tool, inputTokens, padding)
}

func pollOnly(t *testing.T, s *Source, now time.Time) (sess sessionForTest) {
	t.Helper()
	sessions, err := s.Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	got := sessions[0]
	return sessionForTest{
		input:      got.Usage.Input,
		output:     got.Usage.Output,
		ctxUsed:    got.ContextUsed,
		ctxMax:     got.ContextMax,
		toolCalls:  len(got.Tools),
		toolCounts: got.ToolCounts,
		model:      got.Model,
	}
}

type sessionForTest struct {
	input      int64
	output     int64
	ctxUsed    int64
	ctxMax     int64
	toolCalls  int
	toolCounts map[string]int
	model      string
}

func TestCompactionResetDoesNotDoubleCount(t *testing.T) {
	s, transcript := sourceFixture(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, transcript,
		assistantRecord(40, "Read", "")+
			assistantRecord(30, "Read", "")+
			assistantRecord(30, "Bash", ""))

	first := pollOnly(t, s, now)
	if first.input != 100 {
		t.Fatalf("first poll Usage.Input = %d, want 100", first.input)
	}
	if first.toolCalls != 3 {
		t.Fatalf("first poll recorded %d tool calls, want 3", first.toolCalls)
	}
	beforeSize := statSize(t, transcript)

	writeFile(t, transcript, assistantRecord(30, "Grep", strings.Repeat("x", int(beforeSize)+512)))
	if got := statSize(t, transcript); got <= beforeSize {
		t.Fatalf("rewritten transcript is %d bytes, must exceed the pre-truncation %d for this test to bite", got, beforeSize)
	}

	second := pollOnly(t, s, now.Add(time.Second))
	if second.input != 30 {
		t.Fatalf("Usage.Input = %d after compaction, want 30 (the file's own total); "+
			"a stale accumulator makes this 130", second.input)
	}
	if second.ctxUsed != 30 {
		t.Fatalf("ContextUsed = %d after compaction, want 30", second.ctxUsed)
	}
	if second.toolCalls != 1 || second.toolCounts["Grep"] != 1 || second.toolCounts["Read"] != 0 {
		t.Fatalf("tool log/histogram survived the compaction: %d calls, counts %v",
			second.toolCalls, second.toolCounts)
	}
	if second.model != "claude-opus-5" {
		t.Fatalf("Model = %q after compaction, want the last-observed id kept, not blanked", second.model)
	}

	appendTo(t, transcript, assistantRecord(5, "Grep", ""))
	third := pollOnly(t, s, now.Add(2*time.Second))
	if third.input != 35 {
		t.Fatalf("Usage.Input = %d after a post-compaction append, want 35", third.input)
	}
}

func TestInodeSwapResetDoesNotDoubleCount(t *testing.T) {
	s, transcript := sourceFixture(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, transcript, assistantRecord(100, "Read", ""))
	if got := pollOnly(t, s, now); got.input != 100 {
		t.Fatalf("first poll Usage.Input = %d, want 100", got.input)
	}

	replacement := transcript + ".replacement"
	writeFile(t, replacement, assistantRecord(100, "Grep", ""))
	if err := os.Rename(replacement, transcript); err != nil {
		t.Fatalf("replace transcript: %v", err)
	}

	second := pollOnly(t, s, now.Add(time.Second))
	if second.input != 100 {
		t.Fatalf("Usage.Input = %d after an inode swap, want 100 (the new file's own total), not 200", second.input)
	}
	if second.toolCounts["Read"] != 0 {
		t.Fatalf("tool histogram from the replaced file survived: %v", second.toolCounts)
	}
}

func TestContextHighWaterMarkResetsWithTheTranscript(t *testing.T) {
	s, transcript := sourceFixture(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, transcript, assistantRecord(250_000, "Read", ""))
	if got := pollOnly(t, s, now); got.ctxMax != 1_000_000 {
		t.Fatalf("ContextMax = %d at a 250k mark, want the 1M ladder rung (Claude has no 500k window)", got.ctxMax)
	}

	writeFile(t, transcript, assistantRecord(1_000, "Read", ""))
	second := pollOnly(t, s, now.Add(time.Second))
	if second.ctxMax != 200_000 {
		t.Fatalf("ContextMax = %d after compaction, want the 200k rung the rewritten file justifies", second.ctxMax)
	}
}

func TestRateLimitExpiresAtResetsAt(t *testing.T) {
	s, transcript := sourceFixture(t)
	lines := readFixtureLines(t, "agent", "claude", "rate-limited.jsonl")
	var content []byte
	for _, l := range lines {
		content = append(content, l...)
		content = append(content, '\n')
	}
	writeFile(t, transcript, string(content))

	resetsAt := time.Date(2026, 9, 6, 14, 0, 0, 0, time.UTC)

	sessions, err := s.Poll(context.Background(), resetsAt.Add(-time.Minute), nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Status != "rate-limited" {
		t.Fatalf("Status before resetsAt = %+v, want rate-limited", sessions)
	}

	sessions, err = s.Poll(context.Background(), resetsAt.Add(time.Minute), nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Status == "rate-limited" {
		t.Fatalf("Status after resetsAt = %+v, want the session file's own status restored, not a latched rate-limited", sessions)
	}
	if len(sessions[0].RateLimits) != 0 {
		t.Fatalf("RateLimits = %+v after resetsAt, want none", sessions[0].RateLimits)
	}
}

func TestDeadSessionAggIsPruned(t *testing.T) {
	s, transcript := sourceFixture(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, transcript, assistantRecord(10, "Read", ""))
	if _, err := s.Poll(context.Background(), now, nil); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if _, ok := s.agg["sess-1"]; !ok {
		t.Fatalf("precondition: sess-1 should have an accumulator after being polled")
	}

	sessionFile := filepath.Join(s.sessionsDir, "1234.json")
	if err := os.Remove(sessionFile); err != nil {
		t.Fatalf("remove session file: %v", err)
	}

	sessions, err := s.Poll(context.Background(), now.Add(time.Second), nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("got %d sessions after the session file was removed, want 0", len(sessions))
	}
	if _, ok := s.agg["sess-1"]; ok {
		t.Fatalf("s.agg still holds sess-1's accumulator after its session file disappeared")
	}
}

func TestToolLogIsCapped(t *testing.T) {
	s, transcript := sourceFixture(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	var records string
	for i := 0; i < maxRetainedTools+10; i++ {
		records += assistantRecord(1, "Read", "")
	}
	writeFile(t, transcript, records)

	got := pollOnly(t, s, now)
	if got.toolCalls != maxRetainedTools {
		t.Fatalf("toolCalls = %d after %d tool_use records, want capped at %d",
			got.toolCalls, maxRetainedTools+10, maxRetainedTools)
	}
}

func statSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Size()
}

func TestCompactionDoesNotResurrectFinishedSubagents(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(subagentsDir, "agent-1.meta.json"),
		`{"toolUseId":"tu-1","hash":"h1","agentType":"general-purpose","description":"d","model":"opus","spawnDepth":1}`)
	writeFile(t, filepath.Join(subagentsDir, "agent-1.jsonl"),
		`{"type":"assistant","message":{"model":"claude-opus-5","content":[],"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n")

	writeFile(t, transcript,
		`{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"model":"claude-opus-5","role":"assistant",`+
			`"content":[{"type":"tool_use","id":"tu-1","name":"Task"}],"usage":{"input_tokens":10,"output_tokens":1}}}`+"\n"+
			`{"type":"user","timestamp":"2026-09-06T01:00:01.000Z","message":{"role":"user",`+
			`"content":[{"type":"tool_result","tool_use_id":"tu-1"}]}}`+"\n")

	sessions, err := s.Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions[0].Subagents) != 1 || sessions[0].Subagents[0].Live {
		t.Fatalf("subagent should be finished after its tool_result: %+v", sessions[0].Subagents)
	}

	writeFile(t, transcript, assistantRecord(30, "Grep", strings.Repeat("x", 512)))

	sessions, err = s.Poll(context.Background(), now.Add(time.Second), nil)
	if err != nil {
		t.Fatalf("Poll (post-compaction): %v", err)
	}
	if sessions[0].Subagents[0].Live {
		t.Fatalf("finished subagent came back Live after compaction dropped its tool_result")
	}
	if sessions[0].Usage.Input != 30 {
		t.Fatalf("Usage.Input = %d after compaction, want 30", sessions[0].Usage.Input)
	}
}

func childRecord(padding string) string {
	return `{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"model":"claude-opus-5","role":"assistant","content":[],` +
		`"usage":{"input_tokens":1,"output_tokens":1}},"pad":"` + padding + `"}` + "\n"
}

func pollSubagent(t *testing.T, s *Source, now time.Time) domain.Subagent {
	t.Helper()
	sessions, err := s.Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 1 || len(sessions[0].Subagents) != 1 {
		t.Fatalf("want 1 session with 1 subagent, got %+v", sessions)
	}
	return sessions[0].Subagents[0]
}

func TestHungSubagentStopsBeingLive(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(subagentsDir, "agent-1.meta.json"),
		`{"toolUseId":"tu-1","hash":"h1","agentType":"general-purpose","description":"d","model":"opus","spawnDepth":1}`)
	writeFile(t, filepath.Join(subagentsDir, "agent-1.jsonl"), childRecord(""))

	writeFile(t, transcript,
		`{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"model":"claude-opus-5","role":"assistant",`+
			`"content":[{"type":"tool_use","id":"tu-1","name":"Task"}],"usage":{"input_tokens":10,"output_tokens":1}}}`+"\n")

	if sub := pollSubagent(t, s, now); !sub.Live {
		t.Fatalf("a child whose newest record is fresh should be Live; got %+v", sub)
	}

	if sub := pollSubagent(t, s, now.Add(3*time.Minute)); sub.Live || sub.Status != domain.SubagentIdle {
		t.Fatalf("want idle for a quiet child whose tool_use has no result: %+v", sub)
	}

	appendTo(t, filepath.Join(subagentsDir, "agent-1.jsonl"), childRecord("more"))
	if sub := pollSubagent(t, s, now.Add(3*time.Minute+time.Second)); !sub.Live {
		t.Fatalf("Live = false for a child whose transcript grew again: %+v", sub)
	}
}

func TestGrowingSubagentWithToolResultIsNotLive(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(subagentsDir, "agent-1.meta.json"),
		`{"toolUseId":"tu-1","hash":"h1","agentType":"general-purpose","description":"d","model":"opus","spawnDepth":1}`)
	writeFile(t, filepath.Join(subagentsDir, "agent-1.jsonl"), childRecord(""))

	writeFile(t, transcript,
		`{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"model":"claude-opus-5","role":"assistant",`+
			`"content":[{"type":"tool_use","id":"tu-1","name":"Task"}],"usage":{"input_tokens":10,"output_tokens":1}}}`+"\n"+
			`{"type":"user","timestamp":"2026-09-06T01:00:01.000Z","message":{"role":"user",`+
			`"content":[{"type":"tool_result","tool_use_id":"tu-1"}]}}`+"\n")

	if sub := pollSubagent(t, s, now); sub.Live {
		t.Fatalf("Live = true for a child whose tool_use already has a result: %+v", sub)
	}
	appendTo(t, filepath.Join(subagentsDir, "agent-1.jsonl"), childRecord("more"))
	if sub := pollSubagent(t, s, now.Add(time.Second)); sub.Live {
		t.Fatalf("a growing transcript relit a subagent the parent had already answered: %+v", sub)
	}
}

func TestCompactionDoesNotRelightHungSubagent(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(subagentsDir, "agent-1.meta.json"),
		`{"toolUseId":"tu-1","hash":"h1","agentType":"general-purpose","description":"d","model":"opus","spawnDepth":1}`)
	writeFile(t, filepath.Join(subagentsDir, "agent-1.jsonl"), childRecord(""))

	writeFile(t, transcript,
		`{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"model":"claude-opus-5","role":"assistant",`+
			`"content":[{"type":"tool_use","id":"tu-1","name":"Task"}],"usage":{"input_tokens":10,"output_tokens":1}}}`+"\n")

	pollSubagent(t, s, now)
	if sub := pollSubagent(t, s, now.Add(3*time.Minute)); sub.Live {
		t.Fatalf("precondition: child should have gone quiet, got %+v", sub)
	}

	writeFile(t, transcript, assistantRecord(30, "Grep", strings.Repeat("x", 512)))

	if sub := pollSubagent(t, s, now.Add(3*time.Minute+time.Second)); sub.Live {
		t.Fatalf("compaction of the parent relit a hung child: %+v", sub)
	}
}

func TestSanitizeCWDEncodesDotsAndUnderscores(t *testing.T) {
	cases := []struct{ cwd, want string }{
		{"/repo/x", "-repo-x"},
		{"/Users/me/.local/share/chezmoi", "-Users-me--local-share-chezmoi"},
		{"/Users/me/.local/share/chezmoi/dot_local/src/claude-hooks", "-Users-me--local-share-chezmoi-dot-local-src-claude-hooks"},
		{"/tmp/a.b_c-d", "-tmp-a-b-c-d"},
	}
	for _, tc := range cases {
		if got := sanitizeCWD(tc.cwd); got != tc.want {
			t.Errorf("sanitizeCWD(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}

func TestPollReadsTranscriptForDottedCWD(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions")
	projectsDir := filepath.Join(t.TempDir(), "projects")
	const cwd = "/Users/me/.local/share/chezmoi/dot_local"
	projectDir := filepath.Join(projectsDir, "-Users-me--local-share-chezmoi-dot-local")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	writeFile(t, filepath.Join(sessionsDir, "4321.json"),
		`{"pid":4321,"sessionId":"sess-dot","cwd":"`+cwd+`","status":"busy","kind":"interactive",`+
			`"entrypoint":"cli","updatedAt":1757116800000,"statusUpdatedAt":1757116800000}`)
	writeFile(t, filepath.Join(projectDir, "sess-dot.jsonl"), assistantRecord(10, "Bash", ""))

	sessions, err := NewSource(sessionsDir, projectsDir, nil).Poll(context.Background(), time.Now(), nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if sessions[0].Model != "claude-opus-5" {
		t.Errorf("model = %q, want claude-opus-5 (transcript path derived from a dotted cwd)", sessions[0].Model)
	}
	if sessions[0].Usage.Input != 10 {
		t.Errorf("usage.Input = %d, want 10", sessions[0].Usage.Input)
	}
}

func TestPollFindsTranscriptResumedInAnotherProject(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions")
	projectsDir := filepath.Join(t.TempDir(), "projects")
	elsewhere := filepath.Join(projectsDir, "-somewhere-else")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	writeFile(t, filepath.Join(sessionsDir, "5555.json"),
		`{"pid":5555,"sessionId":"sess-moved","cwd":"/repo/y","status":"busy","kind":"interactive",`+
			`"entrypoint":"cli","updatedAt":1757116800000,"statusUpdatedAt":1757116800000}`)
	writeFile(t, filepath.Join(elsewhere, "sess-moved.jsonl"), assistantRecord(7, "Read", ""))

	sessions, err := NewSource(sessionsDir, projectsDir, nil).Poll(context.Background(), time.Now(), nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Model != "claude-opus-5" {
		t.Fatalf("want the resumed session's model, got %+v", sessions)
	}
}

func TestPollWithNoTranscriptLeavesModelEmpty(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions")
	projectsDir := filepath.Join(t.TempDir(), "projects")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}
	writeFile(t, filepath.Join(sessionsDir, "6666.json"),
		`{"pid":6666,"sessionId":"sess-headless","cwd":"/repo/z","status":"busy","kind":"interactive",`+
			`"entrypoint":"sdk-cli","updatedAt":1757116800000,"statusUpdatedAt":1757116800000}`)

	sessions, err := NewSource(sessionsDir, projectsDir, nil).Poll(context.Background(), time.Now(), nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if sessions[0].Model != "" {
		t.Errorf("model = %q, want \"\" (no transcript on disk)", sessions[0].Model)
	}
}

func TestPollPicksUpATranscriptThatAppearsLater(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "sessions")
	projectsDir := filepath.Join(t.TempDir(), "projects")
	projectDir := filepath.Join(projectsDir, "-repo-late")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	writeFile(t, filepath.Join(sessionsDir, "7777.json"),
		`{"pid":7777,"sessionId":"sess-late","cwd":"/repo/late","status":"busy","kind":"interactive",`+
			`"entrypoint":"cli","updatedAt":1757116800000,"statusUpdatedAt":1757116800000}`)

	s := NewSource(sessionsDir, projectsDir, nil)
	now := time.Now()
	if sessions, err := s.Poll(context.Background(), now, nil); err != nil {
		t.Fatalf("Poll: %v", err)
	} else if sessions[0].Model != "" {
		t.Fatalf("precondition: model should be empty before the transcript exists, got %q", sessions[0].Model)
	}

	writeFile(t, filepath.Join(projectDir, "sess-late.jsonl"), assistantRecord(4, "Bash", ""))

	sessions, err := s.Poll(context.Background(), now.Add(2*transcriptRescanInterval), nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if sessions[0].Model != "claude-opus-5" {
		t.Errorf("model = %q, want claude-opus-5 once the transcript appeared", sessions[0].Model)
	}
}
