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

// sourceFixture plants one ~/.claude/sessions/<pid>.json and returns a
// Source pointed at it plus the transcript path the session's records go
// to, so a test can rewrite that transcript underneath a live Source.
func sourceFixture(t *testing.T) (*Source, string) {
	s, transcript, _ := sourceFixtureWithSubagents(t)
	return s, transcript
}

// sourceFixtureWithSubagents is sourceFixture plus the <sessionId>/subagents/
// directory the walker reads, returned so a test can plant children in it.
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

// assistantRecord is one transcript line carrying usage and a tool_use
// block, with padding the decoder ignores so a test can control the file's
// byte length independently of its token counts.
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

// TestCompactionResetDoesNotDoubleCount is the regression the offset
// tailer alone cannot prevent: Claude Code compacts a session's context in
// place, truncating and rewriting the SAME transcript for the SAME session
// id. The rewrite here is deliberately LONGER than the pre-truncation
// offset, so a call site comparing offsets sees ordinary growth — only
// TailResult.Reset distinguishes the two, and every session total derived
// from the transcript is wrong for the rest of the session's life if it is
// ignored.
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

	// Compaction: same path, same session id, rewritten from byte 0 with
	// one 30-token record — padded so the new file is longer than the old.
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

	// A plain append after the reset must still accumulate onto the
	// post-compaction totals — the reset must not become permanent.
	appendTo(t, transcript, assistantRecord(5, "Grep", ""))
	third := pollOnly(t, s, now.Add(2*time.Second))
	if third.input != 35 {
		t.Fatalf("Usage.Input = %d after a post-compaction append, want 35", third.input)
	}
}

// TestInodeSwapResetDoesNotDoubleCount covers the second reset trigger: the
// transcript is replaced at the same path with a new inode, at a size a
// size-only check would never flag.
func TestInodeSwapResetDoesNotDoubleCount(t *testing.T) {
	s, transcript := sourceFixture(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, transcript, assistantRecord(100, "Read", ""))
	if got := pollOnly(t, s, now); got.input != 100 {
		t.Fatalf("first poll Usage.Input = %d, want 100", got.input)
	}

	if err := os.Remove(transcript); err != nil {
		t.Fatalf("remove transcript: %v", err)
	}
	// Same byte length as before, new inode.
	writeFile(t, transcript, assistantRecord(100, "Grep", ""))

	second := pollOnly(t, s, now.Add(time.Second))
	if second.input != 100 {
		t.Fatalf("Usage.Input = %d after an inode swap, want 100 (the new file's own total), not 200", second.input)
	}
	if second.toolCounts["Read"] != 0 {
		t.Fatalf("tool histogram from the replaced file survived: %v", second.toolCounts)
	}
}

// TestContextHighWaterMarkResetsWithTheTranscript pins the context-fill
// half of the same bug: the ladder rung is chosen from a high-water mark,
// and a mark carried across a compaction pins the gauge to a window the
// live file no longer justifies.
func TestContextHighWaterMarkResetsWithTheTranscript(t *testing.T) {
	s, transcript := sourceFixture(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, transcript, assistantRecord(250_000, "Read", ""))
	if got := pollOnly(t, s, now); got.ctxMax != 500_000 {
		t.Fatalf("ContextMax = %d at a 250k mark, want the 500k ladder rung", got.ctxMax)
	}

	writeFile(t, transcript, assistantRecord(1_000, "Read", ""))
	second := pollOnly(t, s, now.Add(time.Second))
	if second.ctxMax != 200_000 {
		t.Fatalf("ContextMax = %d after compaction, want the 200k rung the rewritten file justifies", second.ctxMax)
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

// TestCompactionDoesNotResurrectFinishedSubagents covers the seam between
// the two halves of subagent liveness. Walk decides Live from one signal
// only — whether the parent recorded a tool_result for the child's
// toolUseId — and a compacted transcript no longer carries the line that
// retired it. An answered tool_use stays answered, so the resulted-id set
// is the one thing derived from transcript lines that must survive a
// reset.
func TestCompactionDoesNotResurrectFinishedSubagents(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(subagentsDir, "agent-1.meta.json"),
		`{"toolUseId":"tu-1","hash":"h1","agentType":"general-purpose","description":"d","model":"opus","spawnDepth":1}`)
	writeFile(t, filepath.Join(subagentsDir, "agent-1.jsonl"),
		`{"type":"assistant","message":{"model":"claude-opus-5","content":[],"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n")

	// The parent spawns the child and then records its result.
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

	// Compaction rewrites the transcript without the tool_result line.
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

// childRecord is one line of a subagent's own transcript, with padding the
// decoder ignores so a test can grow the file without changing its totals.
func childRecord(padding string) string {
	return `{"type":"assistant","message":{"model":"claude-opus-5","role":"assistant","content":[],` +
		`"usage":{"input_tokens":1,"output_tokens":1}},"pad":"` + padding + `"}` + "\n"
}

// pollSubagent polls once and returns the session's single subagent.
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

// TestHungSubagentStopsBeingLive is the growth half of the liveness rule.
// Liveness is a conjunction — the child's jsonl is growing AND the parent
// has recorded no tool_result for its tool_use — so a child that died or
// hung before its result was written must stop reading Live once its
// transcript stops moving, rather than standing as Live forever waiting on
// a record that is never coming.
func TestHungSubagentStopsBeingLive(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(subagentsDir, "agent-1.meta.json"),
		`{"toolUseId":"tu-1","hash":"h1","agentType":"general-purpose","description":"d","model":"opus","spawnDepth":1}`)
	writeFile(t, filepath.Join(subagentsDir, "agent-1.jsonl"), childRecord(""))

	// The parent spawns the child and never records its result.
	writeFile(t, transcript,
		`{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"model":"claude-opus-5","role":"assistant",`+
			`"content":[{"type":"tool_use","id":"tu-1","name":"Task"}],"usage":{"input_tokens":10,"output_tokens":1}}}`+"\n")

	if sub := pollSubagent(t, s, now); !sub.Live {
		t.Fatalf("first sighting should be Live: a transcript that has just appeared has grown; got %+v", sub)
	}

	// Nothing writes to the child transcript before the next poll: the
	// process behind it is gone or wedged.
	if sub := pollSubagent(t, s, now.Add(time.Second)); sub.Live {
		t.Fatalf("Live = true for a child whose transcript stopped growing and whose tool_use has no result: %+v", sub)
	}

	// It was not dead after all — the transcript grows again.
	appendTo(t, filepath.Join(subagentsDir, "agent-1.jsonl"), childRecord("more"))
	if sub := pollSubagent(t, s, now.Add(2*time.Second)); !sub.Live {
		t.Fatalf("Live = false for a child whose transcript grew again: %+v", sub)
	}
}

// TestGrowingSubagentWithToolResultIsNotLive pins the other half of the
// conjunction: growth alone is not liveness. A child still flushing its
// last records after the parent has recorded its tool_result is finished.
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

// TestCompactionDoesNotRelightHungSubagent is the mirror of
// TestCompactionDoesNotResurrectFinishedSubagents, on the growth half.
// Compaction rewrites the PARENT transcript; a child's jsonl is a separate
// file it does not touch. If the reset dropped the recorded child sizes,
// every child would look unseen — and an unseen child counts as growing —
// so a hung child with no tool_result would come back Live.
func TestCompactionDoesNotRelightHungSubagent(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	now := time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

	writeFile(t, filepath.Join(subagentsDir, "agent-1.meta.json"),
		`{"toolUseId":"tu-1","hash":"h1","agentType":"general-purpose","description":"d","model":"opus","spawnDepth":1}`)
	writeFile(t, filepath.Join(subagentsDir, "agent-1.jsonl"), childRecord(""))

	writeFile(t, transcript,
		`{"type":"assistant","timestamp":"2026-09-06T01:00:00.000Z","message":{"model":"claude-opus-5","role":"assistant",`+
			`"content":[{"type":"tool_use","id":"tu-1","name":"Task"}],"usage":{"input_tokens":10,"output_tokens":1}}}`+"\n")

	pollSubagent(t, s, now)                                        // first sighting: Live
	if sub := pollSubagent(t, s, now.Add(time.Second)); sub.Live { // stopped growing: not Live
		t.Fatalf("precondition: child should have gone quiet, got %+v", sub)
	}

	writeFile(t, transcript, assistantRecord(30, "Grep", strings.Repeat("x", 512)))

	if sub := pollSubagent(t, s, now.Add(2*time.Second)); sub.Live {
		t.Fatalf("compaction of the parent relit a hung child: %+v", sub)
	}
}
