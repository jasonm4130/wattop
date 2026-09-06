package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeRollout(t *testing.T, root string, dateDir time.Time, filenameTS string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, dateDir.Format("2006"), dateDir.Format("01"), dateDir.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rollout-" + filenameTS + "-00000000-0000-0000-0000-000000000000.jsonl"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestShortlistByMtimeNotFilename: a file physically in today's directory
// but named with a stale embedded timestamp (rollout filenames are local
// time, and the records inside are UTC, so the two can disagree) must still
// be shortlisted because its mtime is fresh — an implementation that
// filtered by parsing the filename's date instead of stat'ing the file would
// wrongly drop it. The inverse also holds: a file in yesterday's directory
// named with a *fresh*-looking timestamp, but whose actual mtime is outside
// the window, must be excluded — a filename-based implementation would
// wrongly keep it.
func TestShortlistByMtimeNotFilename(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 6, 23, 55, 0, 0, time.UTC)
	yesterday := now.Add(-24 * time.Hour)
	longAgo := now.Add(-30 * 24 * time.Hour)

	freshButStaleNamed := writeRollout(t, root, now, longAgo.Format("2006-01-02T15-04-05"), now)
	staleButFreshNamed := writeRollout(t, root, yesterday, now.Format("2006-01-02T15-04-05"), yesterday.Add(-time.Hour))

	got, err := ShortlistRollouts(root, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	foundFresh := false
	for _, p := range got {
		if p == freshButStaleNamed {
			foundFresh = true
		}
		if p == staleButFreshNamed {
			t.Fatalf("shortlist included a file whose mtime is outside the window, only its filename looked fresh: %s", p)
		}
	}
	if !foundFresh {
		t.Fatalf("shortlist dropped a file with a fresh mtime because its filename's embedded timestamp is stale; got %v", got)
	}
}

func TestShortlistExcludesNonRolloutFiles(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	dir := filepath.Join(root, now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-a-rollout.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ShortlistRollouts(root, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("shortlist = %v, want empty (only .txt file present)", got)
	}
}

func TestTailStateReadsOnlyDelta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := &tailState{path: path}
	lines, err := ts.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("first read: got %d lines, want 1", len(lines))
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{\"a\":2}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	lines, err = ts.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"a":2}` {
		t.Fatalf("second read: got %v, want exactly the appended line", lines)
	}
}

func TestTailStateBuffersTrailingPartialLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"a\":2"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := &tailState{path: path}
	lines, err := ts.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (the partial trailing line must be buffered, not parsed)", len(lines))
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("}\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	lines, err = ts.readNewLines()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || string(lines[0]) != `{"a":2}` {
		t.Fatalf("got %v, want the completed buffered line", lines)
	}
}
