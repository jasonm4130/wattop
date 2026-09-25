package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// t0 is the base time every synthetic record in this file is stamped from.
var t0 = time.Date(2026, 9, 6, 1, 0, 0, 0, time.UTC)

func stampAt(d time.Duration) string { return t0.Add(d).Format(time.RFC3339Nano) }

// usageLine is an assistant record with usage and no tool calls.
func usageLine(d time.Duration, msgID string, in, out int) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":"claude-opus-5","role":"assistant",`+
		`"content":[{"type":"text","text":"x"}],"usage":{"input_tokens":%d,"output_tokens":%d,"cache_read_input_tokens":5,`+
		`"cache_creation":{"ephemeral_5m_input_tokens":2,"ephemeral_1h_input_tokens":3}}}}`+"\n",
		stampAt(d), msgID, in, out)
}

// toolUseLine is an assistant record with usage making one tool_use call.
func toolUseLine(d time.Duration, msgID, toolID, name string) string {
	return fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"id":%q,"model":"claude-opus-5","role":"assistant",`+
		`"content":[{"type":"tool_use","id":%q,"name":%q}],"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n",
		stampAt(d), msgID, toolID, name)
}

func toolResultLine(d time.Duration, toolID string) string {
	return fmt.Sprintf(`{"type":"user","timestamp":%q,"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q}]}}`+"\n",
		stampAt(d), toolID)
}

func notificationLine(d time.Duration, taskID, toolUseID, status string) string {
	content := fmt.Sprintf("<task-notification>\n<task-id>%s</task-id>\n<tool-use-id>%s</tool-use-id>\n"+
		"<output-file>/tmp/x.output</output-file>\n<status>%s</status>\n<summary>s</summary>\n</task-notification>",
		taskID, toolUseID, status)
	return fmt.Sprintf(`{"type":"queue-operation","operation":"enqueue","timestamp":%q,"content":%q}`+"\n", stampAt(d), content)
}

func pollSession(t *testing.T, s *Source, now time.Time) domain.Session {
	t.Helper()
	sessions, err := s.Poll(context.Background(), now, nil)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	return sessions[0]
}

func subByID(t *testing.T, sess domain.Session, id string) domain.Subagent {
	t.Helper()
	for _, s := range sess.Subagents {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no subagent %q in %+v", id, sess.Subagents)
	return domain.Subagent{}
}

func subIDs(subs []domain.Subagent) []string {
	out := make([]string, len(subs))
	for i, s := range subs {
		out[i] = s.ID
	}
	return out
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestParseRecordStringContentAndNotification(t *testing.T) {
	ev, err := ParseRecord([]byte(`{"type":"user","timestamp":"2026-09-06T01:00:00Z","message":{"role":"user","content":"plain prompt"}}`))
	if err != nil {
		t.Fatalf("string message.content must parse: %v", err)
	}
	if !ev.Timestamp.Equal(t0) || len(ev.Tools) != 0 || len(ev.ToolResultIDs) != 0 {
		t.Fatalf("unexpected event %+v", ev)
	}

	ev, err = ParseRecord([]byte(notificationLine(0, "task9", "toolu_9", "completed")))
	if err != nil {
		t.Fatal(err)
	}
	want := TaskNotification{TaskID: "task9", ToolUseID: "toolu_9", Status: "completed"}
	if ev.Notification == nil || *ev.Notification != want {
		t.Fatalf("Notification = %+v, want %+v", ev.Notification, want)
	}

	// A dequeue record, or an enqueue of an ordinary prompt, is no notification.
	for _, line := range []string{
		`{"type":"queue-operation","operation":"dequeue","timestamp":"2026-09-06T01:00:00Z"}`,
		`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-09-06T01:00:00Z","content":"just a prompt"}`,
		`{"type":"system","content":"<tool-use-id>x</tool-use-id>"}`,
	} {
		ev, err := ParseRecord([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		if ev.Notification != nil {
			t.Fatalf("%s: unexpected notification %+v", line, ev.Notification)
		}
	}
}

// TestWorkflowJournalStatusesAndAggregation covers workflow agents: status
// from journal result/failed only, phase and label from started records,
// label falling back to meta name, a journal agent with no transcript, and
// the per-workflow summary.
func TestWorkflowJournalStatusesAndAggregation(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	writeFile(t, transcript, usageLine(0, "p1", 10, 1))

	wfA := filepath.Join(subagentsDir, "workflows", "wf_a")
	wfB := filepath.Join(subagentsDir, "workflows", "wf_b")
	mkdir(t, wfA)
	mkdir(t, wfB)
	wfMeta := `{"agentType":"workflow-subagent","spawnDepth":1,"model":"sonnet"}`
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		writeFile(t, filepath.Join(wfA, "agent-"+id+".meta.json"), wfMeta)
	}
	writeFile(t, filepath.Join(wfA, "agent-a4.meta.json"), `{"agentType":"workflow-subagent","spawnDepth":1,"name":"named-agent"}`)
	writeFile(t, filepath.Join(wfA, "agent-a1.jsonl"), usageLine(10*time.Second, "a1m", 100, 10))
	writeFile(t, filepath.Join(wfA, "agent-a2.jsonl"), usageLine(11*time.Second, "a2m", 200, 20))
	writeFile(t, filepath.Join(wfA, "agent-a3.jsonl"), usageLine(12*time.Second, "a3m", 300, 30))
	writeFile(t, filepath.Join(wfA, "agent-a4.jsonl"), usageLine(13*time.Second, "a4m", 400, 40))
	writeFile(t, filepath.Join(wfA, "journal.jsonl"),
		`{"type":"launched"}`+"\n"+
			`{"type":"started","key":"k1","agentId":"a1","label":"Build the thing","phase":"build"}`+"\n"+
			`{"type":"started","key":"k2","agentId":"a2"}`+"\n"+
			`{"type":"started","key":"k3","agentId":"a3","phase":"verify"}`+"\n"+
			`{"type":"started","key":"k4","agentId":"a4"}`+"\n"+
			`{"type":"started","key":"k5","agentId":"ghost","label":"no transcript"}`+"\n"+
			`{"type":"result","key":"k1","agentId":"a1","result":{"pr":"https://example.invalid/pr/1","big":"`+strings.Repeat("y", 4096)+`"}}`+"\n"+
			`{"type":"failed","key":"k2","agentId":"a2"}`+"\n")

	writeFile(t, filepath.Join(wfB, "agent-b1.meta.json"), wfMeta)
	writeFile(t, filepath.Join(wfB, "agent-b1.jsonl"), usageLine(20*time.Second, "b1m", 1, 1))
	writeFile(t, filepath.Join(wfB, "journal.jsonl"),
		`{"type":"started","key":"k","agentId":"b1","phase":"only"}`+"\n"+
			`{"type":"result","key":"k","agentId":"b1","result":{}}`+"\n")

	// wf_c has a journal but no agents yet; wf_empty has nothing at all.
	wfC := filepath.Join(subagentsDir, "workflows", "wf_c")
	mkdir(t, wfC)
	writeFile(t, filepath.Join(wfC, "journal.jsonl"), `{"type":"launched"}`+"\n")
	mkdir(t, filepath.Join(subagentsDir, "workflows", "wf_empty"))

	// a3 is fresh (12s old at poll time); a4 was last active 13s in, so at
	// now = t0+60s both are within the quiet window. Push a4 out of it by
	// polling late and keep a3 running via an outstanding tool.
	appendTo(t, filepath.Join(wfA, "agent-a3.jsonl"), toolUseLine(14*time.Second, "a3t", "tu-a3", "Bash"))
	sess := pollSession(t, s, t0.Add(5*time.Minute))

	a1 := subByID(t, sess, "a1")
	if a1.Status != domain.SubagentDone || a1.WorkflowID != "wf_a" || a1.Phase != "build" || a1.Description != "Build the thing" {
		t.Fatalf("a1 = %+v", a1)
	}
	if a1.Usage.Input != 100 || a1.ContextUsed != 110 || a1.Model != "claude-opus-5" {
		t.Fatalf("a1 accounting = %+v", a1)
	}
	if a2 := subByID(t, sess, "a2"); a2.Status != domain.SubagentFailed || a2.Description != "" {
		t.Fatalf("a2 = %+v", a2)
	}
	a3 := subByID(t, sess, "a3")
	if a3.Status != domain.SubagentRunning || !a3.Live || a3.CurrentTool != "Bash" || a3.Phase != "verify" || a3.ToolCalls != 1 {
		t.Fatalf("a3 = %+v", a3)
	}
	if a4 := subByID(t, sess, "a4"); a4.Status != domain.SubagentIdle || a4.Live || a4.Description != "named-agent" {
		t.Fatalf("a4 = %+v", a4)
	}
	for _, sub := range sess.Subagents {
		if sub.ID == "ghost" {
			t.Fatalf("journal agent with no transcript should be skipped: %+v", sub)
		}
	}
	if got := strings.Join(subIDs(sess.Subagents), ","); got != "a1,a2,a3,a4,b1" {
		t.Fatalf("order = %s", got)
	}

	if len(sess.Workflows) != 3 {
		t.Fatalf("workflows = %+v", sess.Workflows)
	}
	wa, wb, wc := sess.Workflows[0], sess.Workflows[1], sess.Workflows[2]
	if wc.ID != "wf_c" || wc.Agents != 0 || wc.Status != domain.SubagentIdle {
		t.Fatalf("wf_c = %+v (a journal-only run sorts last)", wc)
	}
	if wa.ID != "wf_a" || wa.Agents != 4 || wa.Running != 1 || wa.Done != 1 || wa.Failed != 1 ||
		wa.Status != domain.SubagentRunning || wa.Phase != "verify" {
		t.Fatalf("wf_a = %+v", wa)
	}
	if wa.Usage.Input != 1001 || !wa.StartedAt.Equal(t0.Add(10*time.Second)) || !wa.LastActivityAt.Equal(t0.Add(14*time.Second)) {
		t.Fatalf("wf_a totals = %+v", wa)
	}
	if wa.TokenRate == nil || wa.CostUSD != nil || wa.BurnUSDPerHr != nil {
		t.Fatalf("wf_a rate/cost fields = %+v", wa)
	}
	if wb.ID != "wf_b" || wb.Status != domain.SubagentDone || wb.Done != 1 || wb.Phase != "only" {
		t.Fatalf("wf_b = %+v", wb)
	}
}

// TestJournalResultDoesNotFreezeAccounting: usage a workflow agent flushes
// after its journal result must still be counted on the next poll.
func TestJournalResultDoesNotFreezeAccounting(t *testing.T) {
	s, transcript, subagentsDir := sourceFixtureWithSubagents(t)
	writeFile(t, transcript, usageLine(0, "p1", 10, 1))
	wf := filepath.Join(subagentsDir, "workflows", "wf_x")
	mkdir(t, wf)
	writeFile(t, filepath.Join(wf, "agent-w1.meta.json"), `{"agentType":"workflow-subagent","spawnDepth":1}`)
	writeFile(t, filepath.Join(wf, "agent-w1.jsonl"), usageLine(time.Second, "m1", 100, 1))
	writeFile(t, filepath.Join(wf, "journal.jsonl"),
		`{"type":"started","agentId":"w1"}`+"\n"+`{"type":"result","agentId":"w1","result":{}}`+"\n")

	sess := pollSession(t, s, t0.Add(time.Minute))
	if w := subByID(t, sess, "w1"); w.Status != domain.SubagentDone || w.Usage.Input != 100 {
		t.Fatalf("w1 = %+v", w)
	}

	appendTo(t, filepath.Join(wf, "agent-w1.jsonl"), usageLine(2*time.Second, "m2", 50, 1))
	sess = pollSession(t, s, t0.Add(time.Minute+time.Second))
	if w := subByID(t, sess, "w1"); w.Status != domain.SubagentDone || w.Usage.Input != 150 {
		t.Fatalf("usage after the journal result was dropped: %+v", w)
	}
	if sess.Workflows[0].Usage.Input != 150 {
		t.Fatalf("workflow usage = %+v", sess.Workflows[0].Usage)
	}
	// An unchanged poll re-reads nothing and double-counts nothing.
	sess = pollSession(t, s, t0.Add(time.Minute+2*time.Second))
	if w := subByID(t, sess, "w1"); w.Usage.Input != 150 {
		t.Fatalf("unchanged poll changed usage: %+v", w)
	}
}

// TestNestedChildrenTreeAndPropagation: a nested child's tool_result lives
// in its parent agent's transcript; a running nested child keeps its quiet
// parent running; tree order is roots by start, each followed by its
// descendants, with an unresolvable parent treated as a root.
func TestNestedChildrenTreeAndPropagation(t *testing.T) {
	s, transcript, dir := sourceFixtureWithSubagents(t)
	writeFile(t, transcript, toolUseLine(0, "s1", "tu-p", "Agent")+toolUseLine(0, "s2", "tu-q", "Agent"))

	writeFile(t, filepath.Join(dir, "agent-p.meta.json"), `{"toolUseId":"tu-p","agentType":"general-purpose","description":"parent","spawnDepth":1}`)
	writeFile(t, filepath.Join(dir, "agent-p.jsonl"),
		usageLine(time.Second, "pm", 1, 1)+
			toolUseLine(2*time.Second, "pt1", "tu-n", "Agent")+
			toolUseLine(3*time.Second, "pt2", "tu-r", "Agent")+
			toolResultLine(8*time.Second, "tu-n"))
	writeFile(t, filepath.Join(dir, "agent-n.meta.json"), `{"toolUseId":"tu-n","parentAgentId":"p","spawnDepth":2,"description":"done child"}`)
	writeFile(t, filepath.Join(dir, "agent-n.jsonl"), usageLine(4*time.Second, "nm", 1, 1))
	writeFile(t, filepath.Join(dir, "agent-r.meta.json"), `{"toolUseId":"tu-r","parentAgentId":"p","spawnDepth":2}`)
	writeFile(t, filepath.Join(dir, "agent-r.jsonl"), usageLine(5*time.Second, "rm", 1, 1))
	writeFile(t, filepath.Join(dir, "agent-q.meta.json"), `{"toolUseId":"tu-q","spawnDepth":1}`)
	writeFile(t, filepath.Join(dir, "agent-q.jsonl"), usageLine(2*time.Second, "qm", 1, 1))
	writeFile(t, filepath.Join(dir, "agent-o.meta.json"), `{"toolUseId":"tu-o","parentAgentId":"zzz","spawnDepth":2}`)
	writeFile(t, filepath.Join(dir, "agent-o.jsonl"), usageLine(3*time.Second, "om", 1, 1))

	// First poll a long time later: everything unfinished is quiet.
	sess := pollSession(t, s, t0.Add(10*time.Minute))
	if got := strings.Join(subIDs(sess.Subagents), ","); got != "p,n,r,q,o" {
		t.Fatalf("tree order = %s, want p,n,r,q,o", got)
	}
	if n := subByID(t, sess, "n"); n.Status != domain.SubagentDone || n.ParentID != "p" || n.SpawnDepth != 2 {
		t.Fatalf("n (result in parent agent's transcript) = %+v", n)
	}
	// p has tu-r outstanding in its own transcript, so it reads running on
	// its CurrentTool regardless of propagation.
	if p := subByID(t, sess, "p"); p.Status != domain.SubagentRunning || p.CurrentTool != "Agent" || p.ToolCalls != 2 {
		t.Fatalf("p = %+v", p)
	}
	if r := subByID(t, sess, "r"); r.Status != domain.SubagentIdle {
		t.Fatalf("r = %+v", r)
	}
	if o := subByID(t, sess, "o"); o.ParentID != "zzz" || o.Status != domain.SubagentIdle {
		t.Fatalf("o = %+v", o)
	}
}

func TestRunningDescendantPropagatesToQuietParent(t *testing.T) {
	s, transcript, dir := sourceFixtureWithSubagents(t)
	writeFile(t, transcript, toolUseLine(0, "s1", "tu-p", "Agent"))
	// p's own spawn of c was answered, so p has no outstanding tool.
	writeFile(t, filepath.Join(dir, "agent-p.meta.json"), `{"toolUseId":"tu-p","spawnDepth":1}`)
	writeFile(t, filepath.Join(dir, "agent-p.jsonl"), usageLine(time.Second, "pm", 1, 1))
	writeFile(t, filepath.Join(dir, "agent-c.meta.json"), `{"toolUseId":"tu-c","parentAgentId":"p","spawnDepth":2}`)
	writeFile(t, filepath.Join(dir, "agent-c.jsonl"), toolUseLine(2*time.Second, "cm", "tu-c-bash", "Bash"))
	writeFile(t, filepath.Join(dir, "agent-g.meta.json"), `{"toolUseId":"tu-g","parentAgentId":"c","spawnDepth":3}`)
	writeFile(t, filepath.Join(dir, "agent-g.jsonl"), usageLine(2*time.Second, "gm", 1, 1))

	sess := pollSession(t, s, t0.Add(10*time.Minute))
	if c := subByID(t, sess, "c"); c.Status != domain.SubagentRunning || c.CurrentTool != "Bash" {
		t.Fatalf("c = %+v", c)
	}
	if p := subByID(t, sess, "p"); p.Status != domain.SubagentRunning || !p.Live {
		t.Fatalf("quiet parent of a running child should read running: %+v", p)
	}
	if g := subByID(t, sess, "g"); g.Status != domain.SubagentIdle {
		t.Fatalf("g (descendant, not ancestor) = %+v", g)
	}
	if got := strings.Join(subIDs(sess.Subagents), ","); got != "p,c,g" {
		t.Fatalf("order = %s", got)
	}
}

// TestBackgroundChildDoneOnlyOnItsNotification: the launch tool_result is
// not completion; a notification for another tool-use-id changes nothing;
// the child's own notification ends it; a non-completed status is failed;
// a nested background child's notification in a child transcript counts;
// and the notification survives a parent compaction.
func TestBackgroundChildDoneOnlyOnItsNotification(t *testing.T) {
	s, transcript, dir := sourceFixtureWithSubagents(t)
	writeFile(t, transcript,
		toolUseLine(0, "s1", "tu-bg", "Agent")+
			toolResultLine(time.Second, "tu-bg")+ // "Async agent launched successfully"
			toolUseLine(0, "s2", "tu-bad", "Agent")+
			toolResultLine(time.Second, "tu-bad"))
	bgMeta := func(tu, parent string) string {
		return `{"toolUseId":"` + tu + `","parentAgentId":"` + parent + `","requestShape":"background","spawnDepth":1}`
	}
	writeFile(t, filepath.Join(dir, "agent-bg.meta.json"), bgMeta("tu-bg", ""))
	writeFile(t, filepath.Join(dir, "agent-bg.jsonl"), usageLine(time.Second, "bm", 1, 1))
	writeFile(t, filepath.Join(dir, "agent-bad.meta.json"), bgMeta("tu-bad", ""))
	writeFile(t, filepath.Join(dir, "agent-bad.jsonl"), usageLine(time.Second, "xm", 1, 1))
	writeFile(t, filepath.Join(dir, "agent-host.meta.json"), `{"toolUseId":"tu-host","spawnDepth":1}`)
	writeFile(t, filepath.Join(dir, "agent-host.jsonl"), toolUseLine(time.Second, "hm", "tu-nest", "Agent")+toolResultLine(2*time.Second, "tu-nest"))
	writeFile(t, filepath.Join(dir, "agent-nest.meta.json"), bgMeta("tu-nest", "host"))
	writeFile(t, filepath.Join(dir, "agent-nest.jsonl"), usageLine(2*time.Second, "nm", 1, 1))

	now := t0.Add(30 * time.Second)
	sess := pollSession(t, s, now)
	for _, id := range []string{"bg", "bad", "nest"} {
		if sub := subByID(t, sess, id); sub.Status != domain.SubagentRunning || !sub.Background {
			t.Fatalf("%s should be running despite its launch tool_result: %+v", id, sub)
		}
	}

	appendTo(t, transcript, notificationLine(20*time.Second, "other", "tu-unrelated", "completed"))
	sess = pollSession(t, s, now.Add(time.Second))
	if sub := subByID(t, sess, "bg"); sub.Status != domain.SubagentRunning {
		t.Fatalf("an unrelated notification ended bg: %+v", sub)
	}

	appendTo(t, transcript,
		notificationLine(21*time.Second, "bg", "tu-bg", "completed")+
			notificationLine(21*time.Second, "bad", "tu-bad", "killed"))
	appendTo(t, filepath.Join(dir, "agent-host.jsonl"), notificationLine(22*time.Second, "nest", "tu-nest", "completed"))
	sess = pollSession(t, s, now.Add(2*time.Second))
	if sub := subByID(t, sess, "bg"); sub.Status != domain.SubagentDone || sub.Live {
		t.Fatalf("bg after its notification = %+v", sub)
	}
	if sub := subByID(t, sess, "bad"); sub.Status != domain.SubagentFailed {
		t.Fatalf("bad after a killed notification = %+v", sub)
	}
	if sub := subByID(t, sess, "nest"); sub.Status != domain.SubagentDone {
		t.Fatalf("nest after its notification in host's transcript = %+v", sub)
	}

	// Compaction drops the notification lines from the parent transcript.
	writeFile(t, transcript, assistantRecord(30, "Grep", strings.Repeat("x", 2048)))
	sess = pollSession(t, s, now.Add(3*time.Second))
	if sub := subByID(t, sess, "bg"); sub.Status != domain.SubagentDone {
		t.Fatalf("compaction resurrected bg: %+v", sub)
	}
}

// TestQuietChildIdleButOutstandingToolRunning pins the two-minute rule.
func TestQuietChildIdleButOutstandingToolRunning(t *testing.T) {
	s, transcript, dir := sourceFixtureWithSubagents(t)
	writeFile(t, transcript, toolUseLine(0, "s1", "tu-a", "Agent")+toolUseLine(0, "s2", "tu-b", "Agent"))
	writeFile(t, filepath.Join(dir, "agent-a.meta.json"), `{"toolUseId":"tu-a","spawnDepth":1}`)
	writeFile(t, filepath.Join(dir, "agent-a.jsonl"), usageLine(0, "am", 1, 1))
	writeFile(t, filepath.Join(dir, "agent-b.meta.json"), `{"toolUseId":"tu-b","spawnDepth":1}`)
	writeFile(t, filepath.Join(dir, "agent-b.jsonl"),
		toolUseLine(0, "bm1", "tu-b1", "Read")+toolResultLine(time.Second, "tu-b1")+
			`{"type":"user","timestamp":"`+stampAt(2*time.Second)+`","message":{"role":"user","content":"string content"}}`+"\n"+
			toolUseLine(3*time.Second, "bm2", "tu-b2", "Bash"))

	sess := pollSession(t, s, t0.Add(119*time.Second))
	if a := subByID(t, sess, "a"); a.Status != domain.SubagentRunning {
		t.Fatalf("a within the quiet window = %+v", a)
	}
	sess = pollSession(t, s, t0.Add(121*time.Second))
	if a := subByID(t, sess, "a"); a.Status != domain.SubagentIdle || a.Live {
		t.Fatalf("a after two quiet minutes = %+v", a)
	}
	b := subByID(t, sess, "b")
	if b.Status != domain.SubagentRunning || b.CurrentTool != "Bash" || b.ToolCalls != 2 {
		t.Fatalf("b with an outstanding tool = %+v", b)
	}
	if !b.StartedAt.Equal(t0) || !b.LastActivityAt.Equal(t0.Add(3*time.Second)) {
		t.Fatalf("b times = %v / %v (string-content record must count)", b.StartedAt, b.LastActivityAt)
	}
}

// TestParentResetKeepsChildState: compaction of the parent must neither
// re-read nor double-count a child, nor lose its tool_result evidence.
func TestParentResetKeepsChildState(t *testing.T) {
	s, transcript, dir := sourceFixtureWithSubagents(t)
	writeFile(t, transcript, toolUseLine(0, "s1", "tu-c", "Agent")+toolResultLine(9*time.Second, "tu-c"))
	writeFile(t, filepath.Join(dir, "agent-c.meta.json"), `{"toolUseId":"tu-c","spawnDepth":1}`)
	writeFile(t, filepath.Join(dir, "agent-c.jsonl"), usageLine(time.Second, "m1", 100, 1)+usageLine(2*time.Second, "m2", 50, 1))

	sess := pollSession(t, s, t0.Add(10*time.Second))
	before := subByID(t, sess, "c")
	if before.Status != domain.SubagentDone || before.Usage.Input != 150 {
		t.Fatalf("precondition: %+v", before)
	}
	writeFile(t, transcript, assistantRecord(30, "Grep", strings.Repeat("x", 2048)))
	sess = pollSession(t, s, t0.Add(11*time.Second))
	after := subByID(t, sess, "c")
	if after.Status != domain.SubagentDone || after.Usage != before.Usage || after.ToolCalls != before.ToolCalls {
		t.Fatalf("parent reset changed child: before %+v after %+v", before, after)
	}

	// A child's own truncation resets its accumulators.
	writeFile(t, filepath.Join(dir, "agent-c.jsonl"), usageLine(3*time.Second, "m3", 7, 1))
	sess = pollSession(t, s, t0.Add(12*time.Second))
	if c := subByID(t, sess, "c"); c.Usage.Input != 7 {
		t.Fatalf("child reset: %+v", c)
	}
}

func TestLastUsageAtCoversChildren(t *testing.T) {
	s, transcript, dir := sourceFixtureWithSubagents(t)
	writeFile(t, transcript, usageLine(0, "p1", 10, 1)+toolResultLine(30*time.Second, "whatever"))
	sess := pollSession(t, s, t0.Add(time.Minute))
	if !sess.LastUsageAt.Equal(t0) {
		t.Fatalf("LastUsageAt = %v, want the session's own usage record (tool_result carries no usage)", sess.LastUsageAt)
	}
	wf := filepath.Join(dir, "workflows", "wf_1")
	mkdir(t, wf)
	writeFile(t, filepath.Join(wf, "agent-w.meta.json"), `{"agentType":"workflow-subagent"}`)
	writeFile(t, filepath.Join(wf, "agent-w.jsonl"), usageLine(45*time.Second, "wm", 1, 1)+toolResultLine(50*time.Second, "x"))
	sess = pollSession(t, s, t0.Add(time.Minute))
	if !sess.LastUsageAt.Equal(t0.Add(45 * time.Second)) {
		t.Fatalf("LastUsageAt = %v, want the workflow child's usage time", sess.LastUsageAt)
	}
}

// TestWalkIgnoresUnknownLayout: only subagents/ and workflows/wf_*/ are read.
func TestWalkIgnoresUnknownLayout(t *testing.T) {
	dir := t.TempDir()
	meta := `{"toolUseId":"t","spawnDepth":1}`
	for _, sub := range []string{"other", filepath.Join("workflows", "not_wf"), filepath.Join("workflows", "wf_1", "deeper")} {
		mkdir(t, filepath.Join(dir, sub))
		writeFile(t, filepath.Join(dir, sub, "agent-hidden.meta.json"), meta)
	}
	writeFile(t, filepath.Join(dir, "agent-top.meta.json"), meta)
	subs, err := Walk(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(subIDs(subs), ","); got != "top" {
		t.Fatalf("walked %s, want only top", got)
	}
}
