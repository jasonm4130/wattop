package panel

import (
	"fmt"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

type ChildRow interface {
	childRow()
}

type SubagentRow struct {
	Subagent *domain.Subagent
	Depth    int
}

type WorkflowRow struct {
	Workflow *domain.Workflow
}

func (SubagentRow) childRow() {}
func (WorkflowRow) childRow() {}

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
		out = append(out, SubagentRow{Subagent: sa, Depth: subagentDepth(s.Subagents, byID, i)})
	}
	for i := range s.Workflows {
		out = append(out, WorkflowRow{Workflow: &s.Workflows[i]})
	}
	return out
}

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

func childBurn(burn *float64) string {
	if burn == nil || *burn <= 0 {
		return ""
	}
	return fmt.Sprintf("$%.2f", *burn)
}

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

func shortWorkflowID(id string) string {
	const n = 10
	if r := []rune(id); len(r) > n {
		return string(r[:n])
	}
	return id
}

func workflowCounts(w *domain.Workflow) string {
	s := fmt.Sprintf("%d run %d/%d done", w.Running, w.Done, w.Agents)
	if w.Failed > 0 {
		s += fmt.Sprintf(" %d fail", w.Failed)
	}
	return s
}
