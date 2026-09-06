package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// plantRollout writes a real rollout (session_meta + turn_context, so it
// carries a cwd to bind on) into root's YYYY/MM/DD directory for dateDir,
// stamped with mtime.
func plantRollout(t *testing.T, root string, dateDir time.Time, id, cwd string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, dateDir.Format("2006"), dateDir.Format("01"), dateDir.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-09-06T09-00-00-"+id+".jsonl")
	body := `{"timestamp":"2026-09-06T09:00:00.000Z","ordinal":0,"type":"session_meta","payload":{"session_id":"` + id +
		`","timestamp":"2026-09-06T09:00:00.000Z","cwd":"` + cwd + `","originator":"codex_cli","source":"exec"}}` + "\n" +
		`{"timestamp":"2026-09-06T09:00:01.000Z","ordinal":1,"type":"turn_context","payload":{"cwd":"` + cwd +
		`","model":"gpt-5.6-terra","effort":"high"}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestPollDropsRolloutsOlderThanLookback is the QA defect of 2026-09-06:
// twelve dormant Codex rollouts, up to 23h old and every one of them
// unbound, buried five live Claude sessions in the table. A rollout past
// the lookback with no pid is not a session and must not be emitted at all
// — including in --json, which is why it is dropped here and not in the UI.
func TestPollDropsRolloutsOlderThanLookback(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)

	plantRollout(t, root, now, "recent", "/home/u/live", now.Add(-10*time.Minute))
	plantRollout(t, root, now, "dormant", "/home/u/done", now.Add(-6*time.Hour))

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("want only the rollout inside the %v lookback, got %d: %+v", DefaultLookback, len(sessions), sessions)
	}
	if sessions[0].ID != "recent" {
		t.Errorf("kept the wrong rollout: %q", sessions[0].ID)
	}
}

// TestPollKeepsAnOldRolloutBoundToALivePid is the exception that keeps the
// filter honest: a rollout untouched for hours still describes something
// running right now if a live codex process binds to it, so mtime alone
// never drops it.
func TestPollKeepsAnOldRolloutBoundToALivePid(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)

	plantRollout(t, root, now, "old-but-live", "/home/u/slow", now.Add(-8*time.Hour))
	procs := []domain.ProcSample{codexProc(4242, "/home/u/slow", now.Add(-9*time.Hour))}

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, procs)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("an old rollout bound to a live pid was dropped: got %d sessions", len(sessions))
	}
	if sessions[0].PID == nil || *sessions[0].PID != 4242 {
		t.Errorf("PID = %v, want 4242", sessions[0].PID)
	}
}

// TestWithLookbackWidensTheWindow pins the config knob cmd/wattop wires
// (`codex_lookback_minutes`): a longer lookback brings back exactly the
// rollouts the default drops, and a zero duration is ignored rather than
// collapsing the window to nothing.
func TestWithLookbackWidensTheWindow(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 6, 16, 0, 0, 0, time.UTC)
	plantRollout(t, root, now, "six-hours-old", "/home/u/done", now.Add(-6*time.Hour))

	s := NewSource(root, DefaultIdleThreshold).WithLookback(8 * time.Hour)
	sessions, err := s.Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("an 8h lookback should keep a 6h-old rollout, got %d sessions", len(sessions))
	}

	if got := NewSource(root, DefaultIdleThreshold).WithLookback(0).lookback; got != DefaultLookback {
		t.Errorf("WithLookback(0) set lookback to %v, want the %v default kept", got, DefaultLookback)
	}
}
