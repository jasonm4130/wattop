package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sourceFixture plants one ~/.claude/sessions/<pid>.json and returns a
// Source pointed at it plus the transcript path the session's records go
// to, so a test can rewrite that transcript underneath a live Source.
func sourceFixture(t *testing.T) (*Source, string) {
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

	return NewSource(sessionsDir, projectsDir, nil), filepath.Join(projectDir, "sess-1.jsonl")
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
