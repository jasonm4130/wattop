package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonm4130/wattop/internal/fixture"
)

// TestWalkerIgnoresKeyFiles plants a fake <pid>.<hash>.key file alongside a
// real session file and asserts ListSessionFiles never opens it: the
// allowlist regex must reject it outright, not merely skip it after a
// failed open.
func TestWalkerIgnoresKeyFiles(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "12345.json"), `{"pid":12345,"sessionId":"s1","cwd":"/home/u/proj","status":"busy"}`)

	// A .key file that would panic os.ReadFile's caller if opened as JSON —
	// its content is deliberately not valid JSON, so the test fails loudly
	// (a decode error) if the allowlist regex ever lets it through.
	writeFile(t, filepath.Join(dir, "12345.a1b2c3d4.key"), "not json, this is a secret")

	got, err := ListSessionFiles(dir)
	if err != nil {
		t.Fatalf("ListSessionFiles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListSessionFiles returned %d sessions, want 1 (the .key file must never be opened): %+v", len(got), got)
	}
	if got[0].SessionID != "s1" {
		t.Fatalf("SessionID = %q, want s1", got[0].SessionID)
	}
}

// TestMissingStatusIsUnknown covers the corpus's missing-status.json fixture
// (pid 99571 on the capture machine): a session file with no "status" key
// at all must render "unknown", never default to "waiting" or "".
func TestMissingStatusIsUnknown(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(fixture.CorpusDir(), "agent", "claude", "sessions", "missing-status.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// The fixture isn't named for a pid (it lives under sessions/ for
	// documentation purposes, per testdata/README.md), so copy it into a
	// pid-named file to pass the allowlist.
	writeFile(t, filepath.Join(dir, "99571.json"), string(src))

	got, err := ListSessionFiles(dir)
	if err != nil {
		t.Fatalf("ListSessionFiles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
	if got[0].Status != "unknown" {
		t.Fatalf("Status = %q, want %q", got[0].Status, "unknown")
	}
	if got[0].PID != 99571 {
		t.Fatalf("PID = %d, want 99571", got[0].PID)
	}
}

func TestListSessionFilesMissingDirIsNotAnError(t *testing.T) {
	got, err := ListSessionFiles(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("ListSessionFiles on a missing dir: %v", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

func TestHeadlessChildSessionKind(t *testing.T) {
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(fixture.CorpusDir(), "agent", "claude", "sessions", "headless-child.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	writeFile(t, filepath.Join(dir, "88123.json"), string(src))

	got, err := ListSessionFiles(dir)
	if err != nil {
		t.Fatalf("ListSessionFiles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
	if got[0].Kind != "sdk-cli" || got[0].Entrypoint != "sdk-cli" {
		t.Fatalf("Kind/Entrypoint = %q/%q, want sdk-cli/sdk-cli", got[0].Kind, got[0].Entrypoint)
	}
	if got[0].Status != "busy" {
		t.Fatalf("Status = %q, want busy", got[0].Status)
	}
}

// TestTornFileDoesNotFailWholePoll plants one file that decodes to invalid
// JSON (a torn read of the shape Claude Code produces mid-rewrite)
// alongside two valid session files, and asserts ListSessionFiles returns
// both valid sessions rather than failing outright: one bad file must
// never blank every other Claude row for the cycle.
func TestTornFileDoesNotFailWholePoll(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "11111.json"), `{"pid":11111,"sessionId":"s1","cwd":"/home/u/proj","status":"busy"}`)
	// A torn write: valid JSON prefix, cut off mid-object.
	writeFile(t, filepath.Join(dir, "22222.json"), `{"pid": 1`)
	writeFile(t, filepath.Join(dir, "33333.json"), `{"pid":33333,"sessionId":"s3","cwd":"/home/u/proj","status":"waiting"}`)

	got, err := ListSessionFiles(dir)
	if err != nil {
		t.Fatalf("ListSessionFiles: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2 (the torn file must be skipped, not fail the poll): %+v", len(got), got)
	}
	ids := map[string]bool{got[0].SessionID: true, got[1].SessionID: true}
	if !ids["s1"] || !ids["s3"] {
		t.Fatalf("got session ids %v, want s1 and s3", ids)
	}
}

// TestTornFileUnreadableStillSkipped covers the ReadFile-error half of the
// same guarantee: a file matching the allowlist that cannot be read (here,
// permission-denied) must be skipped rather than failing the whole poll.
func TestTornFileUnreadableStillSkipped(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "11111.json"), `{"pid":11111,"sessionId":"s1","cwd":"/home/u/proj","status":"busy"}`)

	unreadable := filepath.Join(dir, "22222.json")
	writeFile(t, unreadable, `{"pid":22222,"sessionId":"s2","cwd":"/home/u/proj","status":"busy"}`)
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o644) })

	got, err := ListSessionFiles(dir)
	if err != nil {
		t.Fatalf("ListSessionFiles: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1 (unreadable file must be skipped, not fail the poll): %+v", len(got), got)
	}
	if got[0].SessionID != "s1" {
		t.Fatalf("SessionID = %q, want s1", got[0].SessionID)
	}
}

// TestStatusNormalisation covers every raw ~/.claude/sessions/<pid>.json
// status value observed on real Claude Code (docs/manual-qa.md) plus an
// invented one, and asserts each lands on one of the domain's five
// documented Session.Status values (busy|waiting|stale|unknown|
// rate-limited) — "stale" and "rate-limited" are never produced from a
// session file's own status field, so they are excluded from this table.
func TestStatusNormalisation(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"busy", "busy"},
		{"waiting", "waiting"},
		{"idle", "waiting"},
		{"shell", "busy"},
		{"tool", "busy"},
		{"unknown", "unknown"},
		{"", "unknown"},
		{"some-future-value-nobody-has-seen-yet", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			if got := normaliseStatus(tc.raw); got != tc.want {
				t.Fatalf("normaliseStatus(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestStatusNormalisationFixtures covers the real idle.json/shell.json
// fixtures end-to-end through ListSessionFiles, and asserts RawStatus
// still carries the on-disk value even though Status was remapped.
func TestStatusNormalisationFixtures(t *testing.T) {
	cases := []struct {
		fixture    string
		pid        string
		wantStatus string
		wantRaw    string
	}{
		{"idle.json", "14106", "waiting", "idle"},
		{"shell.json", "87821", "busy", "shell"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			dir := t.TempDir()
			src, err := os.ReadFile(filepath.Join(fixture.CorpusDir(), "agent", "claude", "sessions", tc.fixture))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			writeFile(t, filepath.Join(dir, tc.pid+".json"), string(src))

			got, err := ListSessionFiles(dir)
			if err != nil {
				t.Fatalf("ListSessionFiles: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d sessions, want 1", len(got))
			}
			if got[0].Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q", got[0].Status, tc.wantStatus)
			}
			if got[0].RawStatus != tc.wantRaw {
				t.Fatalf("RawStatus = %q, want %q", got[0].RawStatus, tc.wantRaw)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
