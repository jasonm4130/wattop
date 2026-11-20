package codex

import (
	"testing"

	"github.com/jasonm4130/wattop/internal/domain"
)

func TestDedupSessionIDsIgnoresInputOrder(t *testing.T) {
	owners := []Rollout{
		{Path: "/r/c-child.jsonl", ThreadID: "t1", ParentThreadID: "gone"},
		{Path: "/r/b.jsonl", ThreadID: "t1"},
		{Path: "/r/a.jsonl", ThreadID: "t1"},
		{Path: "/r/z.jsonl", ThreadID: "t2"},
	}
	sessions := make([]domain.Session, len(owners))
	for i, r := range owners {
		sessions[i].ID = r.ThreadID
	}

	dedupSessionIDs(sessions, owners)

	want := []string{"t1#c-child.jsonl", "t1#b.jsonl", "t1", "t2"}
	for i, w := range want {
		if sessions[i].ID != w {
			t.Errorf("sessions[%d] (%s) ID = %q, want %q", i, owners[i].Path, sessions[i].ID, w)
		}
	}
}
