package panel

import (
	"fmt"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// ChildRow is one table row under a session: either a non-workflow
// subagent (Subagent set, Depth its nesting level) or one collapsed
// workflow (Workflow set). Exactly one of Subagent and Workflow is non-nil.
// Both point into the Session they were built from.
type ChildRow struct {
	Subagent *domain.Subagent
	Depth    int
	Workflow *domain.Workflow
}

// ChildRows flattens a session's children into table-row order: every
// subagent without a WorkflowID in the order s.Subagents lists them (the
// sources emit tree order), then one row per workflow in s.Workflows order.
// Workflow agents never get rows of their own; the workflow row stands for
// them. Depth counts ParentID hops to a root within s.Subagents; a parent
// that does not resolve ends the walk, so an orphan reads as depth 0.
//
// Every place that flattens sessions into rows (the table, the model's row
// count and its selection lookup) goes through this one function, so they
// cannot disagree on how many rows a session owns.
func ChildRows(s domain.Session) []ChildRow {
	if len(s.Subagents) == 0 && len(s.Workflows) == 0 {
		return nil
	}
	byID := make(map[string]int, len(s.Subagents))
	for i := range s.Subagents {
		if id := s.Subagents[i].ID; id != "" {
			byID[id] = i
		}
	}
	out := make([]ChildRow, 0, len(s.Subagents)+len(s.Workflows))
	for i := range s.Subagents {
		sa := &s.Subagents[i]
		if sa.WorkflowID != "" {
			continue
		}
		out = append(out, ChildRow{Subagent: sa, Depth: subagentDepth(s.Subagents, byID, i)})
	}
	for i := range s.Workflows {
		out = append(out, ChildRow{Workflow: &s.Workflows[i]})
	}
	return out
}

// subagentDepth walks ParentID links from subagents[i] up to a root. The
// walk is bounded by the slice length so a ParentID cycle cannot loop.
func subagentDepth(subagents []domain.Subagent, byID map[string]int, i int) int {
	depth := 0
	for hops := 0; hops < len(subagents); hops++ {
		p := subagents[i].ParentID
		if p == "" {
			break
		}
		j, ok := byID[p]
		if !ok || j == i {
			break
		}
		depth++
		i = j
	}
	return depth
}

// childStatus maps a subagent or workflow Status to its color and short
// label. An empty Status (a source that predates status tracking) falls
// back to the Live flag.
func childStatus(r theme.Roles, status string, live bool) (color, label string) {
	if status == "" {
		status = domain.SubagentDone
		if live {
			status = domain.SubagentRunning
		}
	}
	switch status {
	case domain.SubagentRunning:
		return r.Busy, "● run"
	case domain.SubagentIdle:
		return r.Waiting, "idle"
	case domain.SubagentDone:
		return r.Muted, "done"
	case domain.SubagentFailed:
		if r.Error != "" {
			return r.Error, "fail"
		}
		return r.Warn, "fail"
	default:
		return r.Muted, status
	}
}

func childRunning(status string, live bool) bool {
	return status == domain.SubagentRunning || (status == "" && live)
}

// childCost renders a child's cost, "$—" when it is unpriced, with the
// partial-cost "~" when the figure omits an unpriced agent.
func childCost(cost *float64, partial bool) string {
	text := "$—"
	if cost != nil {
		text = fmt.Sprintf("$%.2f", *cost)
	}
	if partial {
		text = "~" + text
	}
	return text
}

// childBurn renders a child's burn in the parent row's format ("$x.xx",
// the column header carries the unit), blank when there is no positive
// burn to report: an idle or finished child burning $0.00 is noise.
func childBurn(burn *float64) string {
	if burn == nil || *burn <= 0 {
		return ""
	}
	return fmt.Sprintf("$%.2f", *burn)
}

// childElapsed is how long a child has run: to its last activity, or to at
// while it is still running.
func childElapsed(started, last, at time.Time, running bool) string {
	if started.IsZero() {
		return "—"
	}
	end := last
	if running && !at.IsZero() {
		end = at
	}
	if end.IsZero() {
		return "—"
	}
	return formatAge(end.Sub(started))
}

// shortWorkflowID keeps a workflow id short enough to leave the table's
// CWD cell room for the phase and counts, the way a short commit sha does.
func shortWorkflowID(id string) string {
	const n = 10
	if r := []rune(id); len(r) > n {
		return string(r[:n])
	}
	return id
}

// workflowCounts is "R run D/N done" plus " F fail" when any agent failed.
func workflowCounts(w *domain.Workflow) string {
	s := fmt.Sprintf("%d run %d/%d done", w.Running, w.Done, w.Agents)
	if w.Failed > 0 {
		s += fmt.Sprintf(" %d fail", w.Failed)
	}
	return s
}
