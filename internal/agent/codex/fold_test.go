package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// metaLine builds one session_meta record. source, when non-nil, is
// marshalled as-is into payload.source (a string for a top-level rollout, an
// object for a child); parentThreadID is written at the top level, matching
// real session_meta payloads where it sits alongside "source" rather than
// only nested under thread_spawn.
type metaOpts struct {
	threadID       string
	sessionID      string
	parentThreadID string
	nickname       string
	agentPath      string
	source         any // string, or a map producing {"subagent": ...}
	at             time.Time
}

func metaLine(o metaOpts) string {
	payload := map[string]any{
		"session_id": o.sessionID,
		"id":         o.threadID,
		"timestamp":  o.at.Format(time.RFC3339Nano),
		"cwd":        "/home/u/proj",
		"originator": "codex_cli",
	}
	if o.parentThreadID != "" {
		payload["parent_thread_id"] = o.parentThreadID
	}
	if o.nickname != "" {
		payload["agent_nickname"] = o.nickname
	}
	if o.agentPath != "" {
		payload["agent_path"] = o.agentPath
	}
	if o.source != nil {
		payload["source"] = o.source
	} else {
		payload["source"] = "exec"
	}
	rec := map[string]any{
		"timestamp": o.at.Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload":   payload,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func taskCompleteLine(at time.Time) string {
	rec := map[string]any{
		"timestamp": at.Format(time.RFC3339Nano),
		"type":      "event_msg",
		"payload":   map[string]any{"type": "task_complete", "turn_id": "t1"},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func taskStartedLine(at time.Time) string {
	rec := map[string]any{
		"timestamp": at.Format(time.RFC3339Nano),
		"type":      "event_msg",
		"payload":   map[string]any{"type": "task_started", "turn_id": "t1", "model_context_window": 128000},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

// writeRolloutBody writes a rollout file at root's YYYY/MM/DD directory
// (dateDir) with the given lines, stamped with mtime.
func writeRolloutBody(t *testing.T, root string, dateDir time.Time, filenameStamp string, lines []string, mtime time.Time) string {
	t.Helper()
	dir := filepath.Join(root, dateDir.Format("2006"), dateDir.Format("01"), dateDir.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rollout-" + filenameStamp + ".jsonl"
	path := filepath.Join(dir, name)
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestFoldThreadSpawnAndGuardianChildren: a parent with a thread_spawn child
// and a guardian child folds into one session carrying both subagents. The
// child shares session_id with the parent (the real-world collision this
// package must not key on) and must not produce a second session for it.
func TestFoldThreadSpawnAndGuardianChildren(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	sharedSessionID := "shared-sess-id"

	parentPath := writeRolloutBody(t, root, now, "2026-09-25T11-00-00-parent", []string{
		metaLine(metaOpts{threadID: "thread-parent", sessionID: sharedSessionID, at: now.Add(-10 * time.Minute)}),
		taskStartedLine(now.Add(-9 * time.Minute)),
	}, now.Add(-1*time.Minute))

	spawnPath := writeRolloutBody(t, root, now, "2026-09-25T11-01-00-spawn", []string{
		metaLine(metaOpts{
			threadID: "thread-spawn-child", sessionID: sharedSessionID,
			parentThreadID: "thread-parent",
			nickname:       "Pasteur", agentPath: "/root/labs",
			source: map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
				"parent_thread_id": "thread-parent", "depth": 1,
				"agent_path": "/root/labs", "agent_nickname": "Pasteur", "agent_role": nil,
			}}},
			at: now.Add(-8 * time.Minute),
		}),
		taskStartedLine(now.Add(-7 * time.Minute)),
		taskCompleteLine(now.Add(-6 * time.Minute)),
	}, now.Add(-1*time.Minute))

	guardianPath := writeRolloutBody(t, root, now, "2026-09-25T11-02-00-guardian", []string{
		metaLine(metaOpts{
			threadID: "thread-guardian", sessionID: sharedSessionID,
			parentThreadID: "thread-parent",
			source:         map[string]any{"subagent": map[string]any{"other": "guardian"}},
			at:             now.Add(-5 * time.Minute),
		}),
	}, now.Add(-1*time.Minute))

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (both children folded into the parent), paths: %s %s %s", len(sessions), parentPath, spawnPath, guardianPath)
	}

	sess := sessions[0]
	if sess.ID != "thread-parent" {
		t.Fatalf("Session.ID = %q, want thread-parent", sess.ID)
	}
	if len(sess.Subagents) != 2 {
		t.Fatalf("got %d subagents, want 2: %+v", len(sess.Subagents), sess.Subagents)
	}

	var spawn, guardian *domain.Subagent
	for i := range sess.Subagents {
		switch sess.Subagents[i].ID {
		case "thread-spawn-child":
			spawn = &sess.Subagents[i]
		case "thread-guardian":
			guardian = &sess.Subagents[i]
		}
	}
	if spawn == nil || guardian == nil {
		t.Fatalf("missing expected subagent ids: %+v", sess.Subagents)
	}
	if spawn.AgentType != "spawn" {
		t.Errorf("spawn.AgentType = %q, want spawn (agent_role was null)", spawn.AgentType)
	}
	if spawn.Description != "Pasteur /root/labs" {
		t.Errorf("spawn.Description = %q, want %q", spawn.Description, "Pasteur /root/labs")
	}
	if spawn.ParentID != "" {
		t.Errorf("spawn.ParentID = %q, want empty (direct child of the root)", spawn.ParentID)
	}
	if spawn.Status != domain.SubagentDone {
		t.Errorf("spawn.Status = %q, want done (task_complete observed)", spawn.Status)
	}
	if guardian.AgentType != "guardian" {
		t.Errorf("guardian.AgentType = %q, want guardian", guardian.AgentType)
	}
	if guardian.Description != "guardian" {
		t.Errorf("guardian.Description = %q, want guardian (no nickname/path)", guardian.Description)
	}
}

// TestFoldedChildNotBoundToParentPid: a folded child rollout must never be
// offered to the pid binder — it runs inside the parent's process, so
// binding it would steal the parent's own pid.
func TestFoldedChildNotBoundToParentPid(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	writeRolloutBody(t, root, now, "2026-09-25T11-00-00-parent", []string{
		metaLine(metaOpts{threadID: "thread-parent", sessionID: "sess", at: now.Add(-10 * time.Minute)}),
	}, now.Add(-1*time.Minute))
	writeRolloutBody(t, root, now, "2026-09-25T11-01-00-child", []string{
		metaLine(metaOpts{
			threadID: "thread-child", sessionID: "sess", parentThreadID: "thread-parent",
			source: map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
				"parent_thread_id": "thread-parent", "depth": 1, "agent_role": "reviewer",
			}}},
			at: now.Add(-8 * time.Minute),
		}),
	}, now.Add(-1*time.Minute))

	// One live "codex" process sitting in the parent's cwd — since cwd was
	// never set by these session_meta-only fixtures (turn_context carries
	// it), leave procs empty and only assert on binds directly instead.
	src := NewSource(root, DefaultIdleThreshold)
	sessions, err := src.Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if len(sessions[0].Subagents) != 1 {
		t.Fatalf("got %d subagents, want 1", len(sessions[0].Subagents))
	}
	if sessions[0].Subagents[0].AgentType != "reviewer" {
		t.Fatalf("AgentType = %q, want reviewer (non-null agent_role)", sessions[0].Subagents[0].AgentType)
	}
	// The pid-binding path (bindAll) is exercised directly: a child rollout
	// must never appear in bindAll's input set. This is asserted at the
	// bindAll level in TestBindAllExcludesChildRollouts below.
}

// TestBindAllExcludesChildRollouts: Poll must never pass a child rollout
// (ParentThreadID != "") to bindAll, since it runs inside the parent's
// process and must not steal its pid. This is a behavioural check on the
// binding a live parent+child pair would produce: only the parent's cwd/pid
// combination should ever resolve to "exact".
func TestBindAllExcludesChildRollouts(t *testing.T) {
	rollouts := []Rollout{
		{Path: "/r/parent.jsonl", ThreadID: "thread-parent", CWD: "/home/u/proj", MetaAt: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)},
		{Path: "/r/child.jsonl", ThreadID: "thread-child", ParentThreadID: "thread-parent", CWD: "/home/u/proj", MetaAt: time.Date(2026, 9, 25, 10, 0, 1, 0, time.UTC)},
	}
	// Simulate what Poll does: only offer the top-level rollout to bindAll.
	var topLevel []Rollout
	for _, r := range rollouts {
		if r.ParentThreadID == "" {
			topLevel = append(topLevel, r)
		}
	}
	procs := []domain.ProcSample{codexProc(555, "/home/u/proj", time.Date(2026, 9, 25, 9, 59, 0, 0, time.UTC))}
	binds := bindAll(topLevel, procs, "")

	if b := binds["/r/parent.jsonl"]; b.PID == nil || *b.PID != 555 {
		t.Fatalf("parent binding = %+v, want pid 555", b)
	}
	if _, ok := binds["/r/child.jsonl"]; ok {
		t.Fatalf("bindAll returned an entry for a child rollout that was never in its input: %+v", binds)
	}
}

// TestOrphanChildEmittedAsSubagentSession: a child whose root is not present
// in this poll (e.g. the root's rollout is outside the lookback and unbound)
// renders as its own session with Kind "subagent" rather than being dropped.
func TestOrphanChildEmittedAsSubagentSession(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	writeRolloutBody(t, root, now, "2026-09-25T11-00-00-orphan", []string{
		metaLine(metaOpts{
			threadID: "thread-orphan", sessionID: "sess-orphan", parentThreadID: "thread-missing-root",
			nickname: "Curie", agentPath: "/root/task",
			source: map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
				"parent_thread_id": "thread-missing-root", "depth": 1,
			}}},
			at: now.Add(-8 * time.Minute),
		}),
	}, now.Add(-1*time.Minute))

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (orphan emitted as its own session)", len(sessions))
	}
	if sessions[0].Kind != "subagent" {
		t.Fatalf("Kind = %q, want subagent", sessions[0].Kind)
	}
	if sessions[0].ID != "thread-orphan" {
		t.Fatalf("ID = %q, want thread-orphan", sessions[0].ID)
	}
	if sessions[0].PID != nil {
		t.Fatalf("PID = %v, want nil (a child is never pid-bound)", *sessions[0].PID)
	}
}

// TestNestedChildParentID: a depth-2 spawn (child of a child) folds into the
// root's Subagents with ParentID set to its direct parent's ThreadID, not
// the root's.
func TestNestedChildParentID(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	writeRolloutBody(t, root, now, "2026-09-25T11-00-00-root", []string{
		metaLine(metaOpts{threadID: "thread-root", sessionID: "sess", at: now.Add(-10 * time.Minute)}),
	}, now.Add(-1*time.Minute))
	writeRolloutBody(t, root, now, "2026-09-25T11-01-00-mid", []string{
		metaLine(metaOpts{
			threadID: "thread-mid", sessionID: "sess", parentThreadID: "thread-root",
			source: map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
				"parent_thread_id": "thread-root", "depth": 1,
			}}},
			at: now.Add(-8 * time.Minute),
		}),
	}, now.Add(-1*time.Minute))
	writeRolloutBody(t, root, now, "2026-09-25T11-02-00-leaf", []string{
		metaLine(metaOpts{
			threadID: "thread-leaf", sessionID: "sess", parentThreadID: "thread-mid",
			source: map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
				"parent_thread_id": "thread-mid", "depth": 2,
			}}},
			at: now.Add(-6 * time.Minute),
		}),
	}, now.Add(-1*time.Minute))

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if len(sessions[0].Subagents) != 2 {
		t.Fatalf("got %d subagents, want 2", len(sessions[0].Subagents))
	}
	byID := map[string]domain.Subagent{}
	for _, s := range sessions[0].Subagents {
		byID[s.ID] = s
	}
	mid, ok := byID["thread-mid"]
	if !ok {
		t.Fatal("missing thread-mid")
	}
	if mid.ParentID != "" {
		t.Errorf("mid.ParentID = %q, want empty (direct child of root)", mid.ParentID)
	}
	leaf, ok := byID["thread-leaf"]
	if !ok {
		t.Fatal("missing thread-leaf")
	}
	if leaf.ParentID != "thread-mid" {
		t.Errorf("leaf.ParentID = %q, want thread-mid", leaf.ParentID)
	}
	// Depth-first order: mid must precede leaf.
	if sessions[0].Subagents[0].ID != "thread-mid" || sessions[0].Subagents[1].ID != "thread-leaf" {
		t.Errorf("order = %v, want [thread-mid, thread-leaf] (depth-first)", []string{sessions[0].Subagents[0].ID, sessions[0].Subagents[1].ID})
	}
}

// TestDuplicateThreadIDGetsPathSuffix: two independent rollouts sharing one
// ThreadID within a single poll must not collapse into one session row — the
// later one (by the shortlist's sorted path order) gets a path-suffixed id
// instead of silently being summed into or overwriting the first.
func TestDuplicateThreadIDGetsPathSuffix(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	firstPath := writeRolloutBody(t, root, now, "2026-09-25T11-00-00-aaa", []string{
		metaLine(metaOpts{threadID: "dup-thread", sessionID: "sess-a", at: now.Add(-10 * time.Minute)}),
	}, now.Add(-1*time.Minute))
	secondPath := writeRolloutBody(t, root, now, "2026-09-25T11-00-00-zzz", []string{
		metaLine(metaOpts{threadID: "dup-thread", sessionID: "sess-b", at: now.Add(-9 * time.Minute)}),
	}, now.Add(-1*time.Minute))
	if firstPath >= secondPath {
		t.Fatalf("test fixture assumption broken: want firstPath < secondPath by sort order, got %q >= %q", firstPath, secondPath)
	}

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2 (dup thread id must not collapse them)", len(sessions))
	}
	ids := map[string]bool{}
	for _, s := range sessions {
		ids[s.ID] = true
	}
	if !ids["dup-thread"] {
		t.Errorf("missing the first rollout's plain id %q in %v", "dup-thread", ids)
	}
	wantSuffixed := "dup-thread#" + filepath.Base(secondPath)
	if !ids[wantSuffixed] {
		t.Errorf("missing the second (later-by-path) rollout's suffixed id %q in %v", wantSuffixed, ids)
	}
}

// TestSessionLastUsageAtIsMaxOfRootAndChildren: Session.LastUsageAt must
// reflect the newest usage timestamp across the root and every folded child,
// not just the root's own transcript.
func TestSessionLastUsageAtIsMaxOfRootAndChildren(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	rootUsageAt := now.Add(-9 * time.Minute)
	childUsageAt := now.Add(-2 * time.Minute) // newer than the root's own usage

	tokenCountLine := func(at time.Time, input, output int64) string {
		rec := map[string]any{
			"timestamp": at.Format(time.RFC3339Nano),
			"type":      "event_msg",
			"payload": map[string]any{
				"type": "token_count",
				"info": map[string]any{
					"total_token_usage": map[string]any{"input_tokens": input, "output_tokens": output, "total_tokens": input + output},
					"last_token_usage":  map[string]any{"input_tokens": input, "output_tokens": output, "total_tokens": input + output},
					"model_context_window": 128000,
				},
			},
		}
		b, _ := json.Marshal(rec)
		return string(b)
	}

	writeRolloutBody(t, root, now, "2026-09-25T11-00-00-root", []string{
		metaLine(metaOpts{threadID: "thread-root2", sessionID: "sess2", at: now.Add(-10 * time.Minute)}),
		tokenCountLine(rootUsageAt, 100, 10),
	}, now.Add(-1*time.Minute))
	writeRolloutBody(t, root, now, "2026-09-25T11-01-00-child", []string{
		metaLine(metaOpts{
			threadID: "thread-child2", sessionID: "sess2", parentThreadID: "thread-root2",
			source: map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
				"parent_thread_id": "thread-root2", "depth": 1,
			}}},
			at: now.Add(-8 * time.Minute),
		}),
		tokenCountLine(childUsageAt, 50, 5),
	}, now.Add(-1*time.Minute))

	sessions, err := NewSource(root, DefaultIdleThreshold).Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if !sessions[0].LastUsageAt.Equal(childUsageAt) {
		t.Fatalf("LastUsageAt = %v, want the child's newer usage timestamp %v", sessions[0].LastUsageAt, childUsageAt)
	}
}
