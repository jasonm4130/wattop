package claude

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTailerReadsOnlyDelta appends to a file across two Tail calls and
// asserts the second call returns only the newly-appended line, never
// re-reading the whole file — and that neither call reports a Reset.
func TestTailerReadsOnlyDelta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	writeFile(t, path, "line one\nline two\n")

	res, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(res.Lines) != 2 || string(res.Lines[0]) != "line one" || string(res.Lines[1]) != "line two" {
		t.Fatalf("first Tail returned %q, want [line one, line two]", res.Lines)
	}
	if res.Reset {
		t.Fatalf("first Tail on a fresh path reported Reset; a new baseline is not a reset")
	}

	appendTo(t, path, "line three\n")

	res, err = Tail(res.State)
	if err != nil {
		t.Fatalf("Tail (delta): %v", err)
	}
	if len(res.Lines) != 1 {
		t.Fatalf("delta Tail returned %d lines, want 1 (only the new one): %q", len(res.Lines), res.Lines)
	}
	if string(res.Lines[0]) != "line three" {
		t.Fatalf("delta line = %q, want %q", res.Lines[0], "line three")
	}
	if res.Reset {
		t.Fatalf("plain append reported Reset = true")
	}
	if res.State.Offset == 0 {
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

	res, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(res.Lines) != 2 {
		t.Fatalf("got %d lines, want 2 (the partial line must be buffered, not returned): %q", len(res.Lines), res.Lines)
	}

	// Retry with no new data: still nothing new, offset unchanged, and an
	// unfinished line is not mistaken for a truncation.
	res2, err := Tail(res.State)
	if err != nil {
		t.Fatalf("Tail (retry): %v", err)
	}
	if len(res2.Lines) != 0 {
		t.Fatalf("retry with no new bytes returned %d lines, want 0", len(res2.Lines))
	}
	if res2.State.Offset != res.State.Offset {
		t.Fatalf("Offset moved on an unchanged partial line: %d -> %d", res.State.Offset, res2.State.Offset)
	}
	if res2.Reset {
		t.Fatalf("a buffered partial line reported Reset = true")
	}

	// The writer finishes the line.
	appendTo(t, path, "\": \"done\"}\n")

	res3, err := Tail(res2.State)
	if err != nil {
		t.Fatalf("Tail (completed): %v", err)
	}
	if len(res3.Lines) != 1 {
		t.Fatalf("got %d lines after completion, want 1", len(res3.Lines))
	}
	want := `{"partial": "done"}`
	if string(res3.Lines[0]) != want {
		t.Fatalf("completed line = %q, want %q", res3.Lines[0], want)
	}
}

func TestTailerResetsOnTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	writeFile(t, path, "line one\nline two\nline three\n")

	res, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}

	// Truncate and write something shorter than the old offset.
	writeFile(t, path, "new one\n")

	res2, err := Tail(res.State)
	if err != nil {
		t.Fatalf("Tail (post-truncate): %v", err)
	}
	if len(res2.Lines) != 1 || string(res2.Lines[0]) != "new one" {
		t.Fatalf("post-truncate lines = %q, want [new one] (offset must reset to 0)", res2.Lines)
	}
	if !res2.Reset {
		t.Fatalf("Reset = false after a truncation; the caller cannot tell its accumulations are stale")
	}
}

// TestTailerReportsResetWhenRewriteIsLonger is the case an offset
// comparison at the call site cannot see: the file is rewritten from byte
// 0 with MORE content than the old offset, so the offset moves forward
// exactly as an append would. Only the Reset flag distinguishes them.
func TestTailerReportsResetWhenRewriteIsLonger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	writeFile(t, path, "a\nb\n")

	res, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	oldOffset := res.State.Offset

	// Rewrite in place, longer than before. os.WriteFile truncates first,
	// which is what makes this a reset rather than an append.
	writeFile(t, path, "compacted one\ncompacted two\ncompacted three\n")

	res2, err := Tail(res.State)
	if err != nil {
		t.Fatalf("Tail (post-rewrite): %v", err)
	}
	if !res2.Reset {
		t.Fatalf("Reset = false after an in-place rewrite longer than the old offset")
	}
	if res2.State.Offset <= oldOffset {
		t.Fatalf("new offset %d is not past the old %d; this case must look like growth by offset alone",
			res2.State.Offset, oldOffset)
	}
	if len(res2.Lines) != 3 {
		t.Fatalf("got %d lines, want all 3 from the rewritten file: %q", len(res2.Lines), res2.Lines)
	}
}

func TestTailerResetsOnInodeSwap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	writeFile(t, path, "old content\n")

	res, err := Tail(TailState{Path: path})
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}

	// Recreate the file at the same path (new inode), same size as before
	// so a size-only check would not catch it.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	writeFile(t, path, "new content\n")

	res2, err := Tail(res.State)
	if err != nil {
		t.Fatalf("Tail (post-swap): %v", err)
	}
	if len(res2.Lines) != 1 || string(res2.Lines[0]) != "new content" {
		t.Fatalf("post-swap lines = %q, want [new content] (offset must reset on inode change)", res2.Lines)
	}
	if !res2.Reset {
		t.Fatalf("Reset = false after an inode swap")
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
