package claude

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTailerReadsOnlyDelta appends to a file across two Tail calls and
// asserts the second call returns only the newly-appended line, never
// re-reading the whole file.
func TestTailerReadsOnlyDelta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	writeFile(t, path, "line one\nline two\n")

	state, lines, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(lines) != 2 || string(lines[0]) != "line one" || string(lines[1]) != "line two" {
		t.Fatalf("first Tail returned %q, want [line one, line two]", lines)
	}

	appendTo(t, path, "line three\n")

	state, lines, err = Tail(state)
	if err != nil {
		t.Fatalf("Tail (delta): %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("delta Tail returned %d lines, want 1 (only the new one): %q", len(lines), lines)
	}
	if string(lines[0]) != "line three" {
		t.Fatalf("delta line = %q, want %q", lines[0], "line three")
	}
	if state.Offset == 0 {
		t.Fatalf("Offset did not advance")
	}
}

// TestTruncatedFinalLineBuffered mirrors the corpus's
// truncated-final-line.jsonl: two complete lines followed by a partial
// record with no terminating newline. The partial line must not be
// returned, and Offset must stop before it so the next call re-reads it in
// full once the writer finishes it.
func TestTruncatedFinalLineBuffered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	writeFile(t, path, "line one\nline two\n{\"partial")

	state, lines, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 (the partial line must be buffered, not returned): %q", len(lines), lines)
	}

	// Retry with no new data: still nothing new, offset unchanged.
	state2, lines2, err := Tail(state)
	if err != nil {
		t.Fatalf("Tail (retry): %v", err)
	}
	if len(lines2) != 0 {
		t.Fatalf("retry with no new bytes returned %d lines, want 0", len(lines2))
	}
	if state2.Offset != state.Offset {
		t.Fatalf("Offset moved on an unchanged partial line: %d -> %d", state.Offset, state2.Offset)
	}

	// The writer finishes the line.
	appendTo(t, path, "\": \"done\"}\n")

	_, lines3, err := Tail(state2)
	if err != nil {
		t.Fatalf("Tail (completed): %v", err)
	}
	if len(lines3) != 1 {
		t.Fatalf("got %d lines after completion, want 1", len(lines3))
	}
	want := `{"partial": "done"}`
	if string(lines3[0]) != want {
		t.Fatalf("completed line = %q, want %q", lines3[0], want)
	}
}

func TestTailerResetsOnTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	writeFile(t, path, "line one\nline two\nline three\n")

	state, _, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}

	// Truncate and write something shorter than the old offset.
	writeFile(t, path, "new one\n")

	_, lines, err := Tail(state)
	if err != nil {
		t.Fatalf("Tail (post-truncate): %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != "new one" {
		t.Fatalf("post-truncate lines = %q, want [new one] (offset must reset to 0)", lines)
	}
}

func TestTailerResetsOnInodeSwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, path, "old content\n")

	state, _, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}

	// Recreate the file at the same path (new inode), same size as before
	// so a size-only check would not catch it.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	writeFile(t, path, "new content\n")

	_, lines, err := Tail(state)
	if err != nil {
		t.Fatalf("Tail (post-swap): %v", err)
	}
	if len(lines) != 1 || string(lines[0]) != "new content" {
		t.Fatalf("post-swap lines = %q, want [new content] (offset must reset on inode change)", lines)
	}
}

func appendTo(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append: %v", err)
	}
}
