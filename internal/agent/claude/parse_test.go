package claude

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonm4130/wattop/internal/fixture"
)

func readFixtureLines(t *testing.T, rel ...string) [][]byte {
	t.Helper()
	path := filepath.Join(append([]string{fixture.CorpusDir()}, rel...)...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines [][]byte
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		cp := make([]byte, len(line))
		copy(cp, line)
		lines = append(lines, cp)
	}
	return lines
}

func TestParseRecordBusyInteractiveHasToolUseNoResult(t *testing.T) {
	lines := readFixtureLines(t, "agent", "claude", "busy-interactive.jsonl")

	var lastEv Event
	for _, l := range lines {
		ev, err := ParseRecord(l)
		if err != nil {
			t.Fatalf("ParseRecord: %v", err)
		}
		lastEv = ev
	}
	if lastEv.Model != "claude-opus-5" {
		t.Fatalf("Model = %q, want claude-opus-5", lastEv.Model)
	}
	if len(lastEv.Tools) != 1 || lastEv.Tools[0].Name != "Bash" {
		t.Fatalf("Tools = %+v, want one Bash tool_use", lastEv.Tools)
	}
	if len(lastEv.ToolResultIDs) != 0 {
		t.Fatalf("ToolResultIDs = %v, want none (this is the busy signal)", lastEv.ToolResultIDs)
	}
	if !lastEv.HasUsage || lastEv.Usage.Input != 4 || lastEv.Usage.Output != 61 {
		t.Fatalf("Usage = %+v, want input=4 output=61", lastEv.Usage)
	}
}

// TestCacheTiersDistinct is the case the plan calls out by name: an
// assistant record whose cache_creation carries a nonzero 1h tier and a
// zero 5m tier must not collapse the two into one field.
func TestCacheTiersDistinct(t *testing.T) {
	lines := readFixtureLines(t, "agent", "claude", "cache-1h.jsonl")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	ev, err := ParseRecord(lines[0])
	if err != nil {
		t.Fatalf("ParseRecord: %v", err)
	}
	if !ev.HasUsage {
		t.Fatalf("HasUsage = false, want true")
	}
	if ev.Usage.CacheCreate1h != 17973 {
		t.Fatalf("CacheCreate1h = %d, want 17973", ev.Usage.CacheCreate1h)
	}
	if ev.Usage.CacheCreate5m != 0 {
		t.Fatalf("CacheCreate5m = %d, want 0", ev.Usage.CacheCreate5m)
	}
}

func TestParseRecordRateLimited(t *testing.T) {
	lines := readFixtureLines(t, "agent", "claude", "rate-limited.jsonl")

	var found *Event
	for _, l := range lines {
		ev, err := ParseRecord(l)
		if err != nil {
			t.Fatalf("ParseRecord: %v", err)
		}
		if ev.RateLimit != nil {
			e := ev
			found = &e
		}
	}
	if found == nil {
		t.Fatalf("no RateLimit found in rate-limited.jsonl")
	}
	if found.RateLimit.Scope != "five_hour" {
		t.Fatalf("Scope = %q, want five_hour", found.RateLimit.Scope)
	}
	if !found.RateLimit.Rejected {
		t.Fatalf("Rejected = false, want true")
	}
	if found.RateLimit.ResetsAt.IsZero() {
		t.Fatalf("ResetsAt is zero")
	}
}

func TestParseRecordSubagentToolUseFanOut(t *testing.T) {
	lines := readFixtureLines(t, "agent", "claude", "subagent-tree.jsonl")

	var toolUseIDs []string
	resulted := map[string]bool{}
	for _, l := range lines {
		ev, err := ParseRecord(l)
		if err != nil {
			t.Fatalf("ParseRecord: %v", err)
		}
		for _, tc := range ev.Tools {
			toolUseIDs = append(toolUseIDs, tc.ID)
		}
		for _, id := range ev.ToolResultIDs {
			resulted[id] = true
		}
	}
	if len(toolUseIDs) != 3 {
		t.Fatalf("got %d tool_use ids, want 3", len(toolUseIDs))
	}
	if len(resulted) != 2 {
		t.Fatalf("got %d resulted tool_use ids, want 2 (one Task is still running)", len(resulted))
	}
}

func TestParseRecordRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseRecord([]byte(`{"partial`)); err == nil {
		t.Fatalf("ParseRecord on invalid JSON returned no error")
	}
}
