package codex

import (
	"sort"
	"strings"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// walkToRoot follows c.ParentThreadID up through byThreadID (a poll's
// rollouts, keyed by ThreadID) until it reaches a rollout with no
// ParentThreadID at all, and returns that root. ok is false when any hop in
// the chain is missing from this poll (the true root is not visible) or a
// cycle is detected, in which case c cannot be folded and stays its own
// session.
func walkToRoot(c Rollout, byThreadID map[string]Rollout) (root Rollout, ok bool) {
	seen := make(map[string]bool)
	cur := c
	for {
		if cur.ParentThreadID == "" {
			return cur, true
		}
		if seen[cur.ThreadID] {
			return Rollout{}, false
		}
		seen[cur.ThreadID] = true
		parent, exists := byThreadID[cur.ParentThreadID]
		if !exists {
			return Rollout{}, false
		}
		cur = parent
	}
}

// buildSubagent turns one folded child rollout into the domain.Subagent
// entry it contributes to its root session. parentID is "" when c's direct
// parent is the root itself, or the direct parent's ThreadID when that
// parent is itself a folded child (a nested spawn).
func buildSubagent(c Rollout, parentID string, idleThreshold time.Duration, now time.Time) domain.Subagent {
	inferred := inferStatus(c.Status, c.ModTime, now, idleThreshold)
	status := childSubagentStatus(inferred, c.TaskCompleted)

	desc := strings.TrimSpace(strings.TrimSpace(c.Nickname) + " " + strings.TrimSpace(c.AgentPath))
	if desc == "" && c.AgentType == "guardian" {
		desc = "guardian"
	}

	lastActivity := c.lastUsageAt
	if lastActivity.IsZero() {
		lastActivity = c.ModTime
	}

	return domain.Subagent{
		ID:             c.ThreadID,
		ParentID:       parentID,
		AgentType:      c.AgentType,
		Description:    desc,
		Model:          c.Model,
		Usage:          c.Usage,
		ContextUsed:    c.ContextUsed,
		SpawnDepth:     c.SpawnDepth,
		StartedAt:      c.MetaAt,
		LastActivityAt: lastActivity,
		Status:         status,
		Live:           status == domain.SubagentRunning,
		ToolCalls:      c.ToolCalls,
		CurrentTool:    c.CurrentTool,
		TokenRate:      c.tokens.Rate(now),
	}
}

// childSubagentStatus maps a rollout's inferred busy/waiting/stale status
// (see inferStatus) plus whether a task_complete was ever observed onto the
// domain Subagent status vocabulary. "waiting" in practice always follows a
// task_complete (that is what sets it), so its branch is here mainly for
// symmetry with "stale", which can arrive with or without one.
func childSubagentStatus(inferred string, taskCompleted bool) string {
	switch inferred {
	case "busy":
		return domain.SubagentRunning
	case "waiting":
		if taskCompleted {
			return domain.SubagentDone
		}
		return domain.SubagentRunning
	case "stale":
		if taskCompleted {
			return domain.SubagentDone
		}
		return domain.SubagentIdle
	default:
		if taskCompleted {
			return domain.SubagentDone
		}
		return domain.SubagentRunning
	}
}

// sortSubagentsTree orders a root's folded children depth-first: each
// ParentID group (starting with "", the root's direct children) is sorted by
// StartedAt then ID, and every subagent is emitted immediately before its
// own children.
func sortSubagentsTree(subs []domain.Subagent) []domain.Subagent {
	byParent := make(map[string][]domain.Subagent, len(subs))
	for _, s := range subs {
		byParent[s.ParentID] = append(byParent[s.ParentID], s)
	}
	for k, grp := range byParent {
		sort.SliceStable(grp, func(i, j int) bool {
			if !grp[i].StartedAt.Equal(grp[j].StartedAt) {
				return grp[i].StartedAt.Before(grp[j].StartedAt)
			}
			return grp[i].ID < grp[j].ID
		})
		byParent[k] = grp
	}

	out := make([]domain.Subagent, 0, len(subs))
	var walk func(parentID string)
	walk = func(parentID string) {
		for _, s := range byParent[parentID] {
			out = append(out, s)
			walk(s.ID)
		}
	}
	walk("")
	return out
}
