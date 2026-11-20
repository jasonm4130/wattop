package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

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

func TestPollKeepsRolloutIdleLongerThanShortlistWindowWhileCodexAlive(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	lastWrite := now.Add(-30 * time.Hour)
	plantRollout(t, root, lastWrite, "idle-but-live", "/home/u/slow", lastWrite)
	procs := []domain.ProcSample{codexProc(4242, "/home/u/slow", now.Add(-31*time.Hour))}

	s := NewSource(root, DefaultIdleThreshold)
	earlier := now.Add(-10 * time.Hour)
	sessions, err := s.Poll(context.Background(), earlier, procs)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].PID == nil {
		t.Fatalf("setup: want one bound session at 20h idle, got %+v", sessions)
	}

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

	sessions, err = s.Poll(context.Background(), now.Add(time.Second), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("with no codex process, a 30h-idle rollout must be dropped; got %+v", sessions)
	}
}

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

	resumedAt := now.Add(time.Second)
	if err := os.Chtimes(old, resumedAt, resumedAt); err != nil {
		t.Fatal(err)
	}

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

	got, err = idx.shortlist(later.Add(time.Second), later.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("cached candidate lost between walks: got %v", got)
	}
	got, err = idx.shortlist(later.Add(2*time.Second), later.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("cached candidate not re-stated against the cutoff: got %v", got)
	}
}

func TestIndexSeesNewFileInTodayFolderBetweenWalks(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
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

func TestIndexMissingRoot(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	got, err := newRolloutIndex(filepath.Join(t.TempDir(), "absent")).shortlist(now, now.Add(-time.Hour))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want empty, nil", got, err)
	}
}
