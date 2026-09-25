package panel

import (
	"fmt"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func TestWorkflowAgentsShownCapsFinished(t *testing.T) {
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	var subs []domain.Subagent
	for i := 0; i < 10; i++ {
		subs = append(subs, domain.Subagent{ID: fmt.Sprintf("d%d", i), WorkflowID: "wf_1", Status: domain.SubagentDone, LastActivityAt: base.Add(time.Duration(i) * time.Minute)})
	}
	subs = append(subs,
		domain.Subagent{ID: "run", WorkflowID: "wf_1", Status: domain.SubagentRunning},
		domain.Subagent{ID: "fail", WorkflowID: "wf_1", Status: domain.SubagentFailed},
		domain.Subagent{ID: "other", WorkflowID: "wf_2", Status: domain.SubagentRunning},
	)

	shown, hidden := workflowAgentsShown(subs, &domain.Workflow{ID: "wf_1", Status: domain.SubagentRunning})
	var ids []string
	for _, i := range shown {
		ids = append(ids, subs[i].ID)
	}
	if got, want := fmt.Sprint(ids), "[d7 d8 d9 run fail]"; got != want || hidden != 7 {
		t.Fatalf("running workflow shows %s hiding %d, want %s hiding 7", got, hidden, want)
	}

	shown, hidden = workflowAgentsShown(subs, &domain.Workflow{ID: "wf_1", Status: domain.SubagentDone})
	if len(shown) != 2 || hidden != 10 {
		t.Fatalf("done workflow shows %d hiding %d, want 2 (unfinished+failed) hiding 10", len(shown), hidden)
	}
}
