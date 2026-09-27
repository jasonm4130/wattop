package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// TestShortlistFindsResumedRolloutInOldDayFolder: Codex writes a rollout
// into the YYYY/MM/DD folder of the day the session STARTED and keeps
// appending to it, so a resumed or multi-day session lives in an old folder
// with a fresh mtime. Selecting by folder date (today/yesterday) never finds
// it; selecting by mtime must.
func TestShortlistFindsResumedRolloutInOldDayFolder(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	started := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)

	resumed := writeRollout(t, root, started, started.Format("2006-01-02T15-04-05"), now.Add(-time.Minute))

	got, err := ShortlistRollouts(root, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != resumed {
		t.Fatalf("shortlist = %v, want the resumed rollout in the 2026/09/21 folder", got)
	}
}

// TestPollFindsResumedSessionInOldDayFolder is the same defect end to end:
// a session started six days ago and resumed a minute ago renders.
func TestPollFindsResumedSessionInOldDayFolder(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	plantRollout(t, root, time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC), "resumed", "/home/u/proj", now.Add(-time.Minute))

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "resumed" {
		t.Fatalf("want the resumed session, got %+v", sessions)
	}
}

// TestPollKeepsRolloutIdleLongerThanShortlistWindowWhileCodexAlive: a codex
// process that has sat idle for more than a day still owns its rollout.
// Dropping it because its mtime is outside the 24h shortlist window loses a
// live session.
func TestPollKeepsRolloutIdleLongerThanShortlistWindowWhileCodexAlive(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	lastWrite := now.Add(-30 * time.Hour)
	// Planted in the folder of its own last write (yesterday relative to
	// now, which the old today/yesterday scan did cover) so this isolates
	// the idle-time bound from the folder bug.
	plantRollout(t, root, lastWrite, "idle-but-live", "/home/u/slow", lastWrite)
	procs := []domain.ProcSample{codexProc(4242, "/home/u/slow", now.Add(-31*time.Hour))}

	s := NewSource(root, DefaultIdleThreshold)
	// First seen while still inside the 24h window: it binds.
	earlier := now.Add(-10 * time.Hour)
	sessions, err := s.Poll(context.Background(), earlier, procs)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].PID == nil {
		t.Fatalf("setup: want one bound session at 20h idle, got %+v", sessions)
	}

	// Ten hours later it has been idle 30h, and its process is still alive.
	sessions, err = s.Poll(context.Background(), now, procs)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("a rollout idle >24h bound to a live codex pid was dropped: got %d sessions", len(sessions))
	}
	if sessions[0].PID == nil || *sessions[0].PID != 4242 {
		t.Errorf("PID = %v, want 4242", sessions[0].PID)
	}

	// Once the process exits the rollout is a dead transcript again.
	sessions, err = s.Poll(context.Background(), now.Add(time.Second), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("with no codex process, a 30h-idle rollout must be dropped; got %+v", sessions)
	}
}

// TestPollDropsPinnedRolloutOnPidReuse: the pin is to a process, not a pid
// number. A recycled pid with a different start time does not keep the
// rollout alive.
func TestPollDropsPinnedRolloutOnPidReuse(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	lastWrite := now.Add(-30 * time.Hour)
	plantRollout(t, root, lastWrite, "idle", "/home/u/slow", lastWrite)

	s := NewSource(root, DefaultIdleThreshold)
	orig := []domain.ProcSample{codexProc(4242, "/home/u/slow", now.Add(-31*time.Hour))}
	if sessions, err := s.Poll(context.Background(), now.Add(-10*time.Hour), orig); err != nil || len(sessions) != 1 {
		t.Fatalf("setup: got %+v, %v", sessions, err)
	}

	reused := []domain.ProcSample{codexProc(4242, "/home/u/slow", now.Add(-time.Minute))}
	sessions, err := s.Poll(context.Background(), now, reused)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("a recycled pid kept a 30h-idle rollout listed: %+v", sessions)
	}
}

// TestIndexRewalksOnInterval pins the cache contract: between full walks
// only cached day folders (plus today/yesterday) are listed, so an old
// folder's rollout that is resumed is picked up by the next full walk, at
// most rewalkInterval later, and never lost.
func TestIndexRewalksOnInterval(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	started := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	old := writeRollout(t, root, started, started.Format("2006-01-02T15-04-05"), started)

	idx := newRolloutIndex(root)
	got, err := idx.shortlist(now, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("first walk: got %v, want nothing recent", got)
	}

	// The session is resumed: its old file is appended to.
	resumedAt := now.Add(time.Second)
	if err := os.Chtimes(old, resumedAt, resumedAt); err != nil {
		t.Fatal(err)
	}

	// Within the interval the cache does not look in 2026/09/10.
	got, err = idx.shortlist(now.Add(2*time.Second), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected the cache to skip a cold folder between walks, got %v", got)
	}

	later := now.Add(rewalkInterval)
	got, err = idx.shortlist(later, later.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != old {
		t.Fatalf("after rewalkInterval: got %v, want the resumed rollout %s", got, old)
	}

	// Its folder is now hot: a poll within the next interval re-stats it and
	// keeps it...
	got, err = idx.shortlist(later.Add(time.Second), later.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("cached candidate lost between walks: got %v", got)
	}
	// ...and drops it as soon as its mtime leaves the window, without
	// waiting for the next walk.
	got, err = idx.shortlist(later.Add(2*time.Second), later.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("cached candidate not re-stated against the cutoff: got %v", got)
	}
}

// TestIndexSeesNewFileInTodayFolderBetweenWalks: a brand-new session lands
// in today's folder and must show up on the very next poll, not up to
// rewalkInterval later.
func TestIndexSeesNewFileInTodayFolderBetweenWalks(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	// Something must exist so the root is present and the first walk runs.
	writeRollout(t, root, now.Add(-10*24*time.Hour), "2026-09-17T09-00-00", now.Add(-10*24*time.Hour))

	idx := newRolloutIndex(root)
	if _, err := idx.shortlist(now, now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "2026", "09", "27")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "rollout-2026-09-27T12-00-01-00000000-0000-0000-0000-000000000001.jsonl")
	if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now.Add(time.Second), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	got, err := idx.shortlist(now.Add(2*time.Second), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != p {
		t.Fatalf("new file in today's folder not seen before the next full walk: got %v", got)
	}
}

// TestIndexRewalksWhenCutoffWidens: a cutoff earlier than the last full
// walk's (a long-lived codex process appeared) cannot be answered from the
// cache, so it forces a full walk immediately.
func TestIndexRewalksWhenCutoffWidens(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	started := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	old := writeRollout(t, root, started, started.Format("2006-01-02T15-04-05"), now.Add(-72*time.Hour))

	idx := newRolloutIndex(root)
	got, err := idx.shortlist(now, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v, want nothing inside 24h", got)
	}
	got, err = idx.shortlist(now.Add(time.Second), now.Add(-96*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != old {
		t.Fatalf("widened cutoff did not force a walk: got %v", got)
	}
}

// TestIndexMissingRoot: no ~/.codex/sessions at all is an empty result, not
// an error.
func TestIndexMissingRoot(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	got, err := newRolloutIndex(filepath.Join(t.TempDir(), "absent")).shortlist(now, now.Add(-time.Hour))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want empty, nil", got, err)
	}
}
