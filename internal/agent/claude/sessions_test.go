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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
