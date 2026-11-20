package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

var subagentMetaPattern = regexp.MustCompile(`^agent-(.+)\.meta\.json$`)

var workflowDirPattern = regexp.MustCompile(`^wf_.+$`)

const subagentQuietAfter = 2 * time.Minute

type rawSubagentMeta struct {
	ToolUseID     string `json:"toolUseId"`
	Hash          string `json:"hash"`
	AgentType     string `json:"agentType"`
	Description   string `json:"description"`
	Name          string `json:"name"`
	Model         string `json:"model"`
	ParentAgentID string `json:"parentAgentId"`
	RequestShape  string `json:"requestShape"`
	SpawnDepth    int    `json:"spawnDepth"`
}

type pendingTool struct {
	name string
	seq  int
}

type childState struct {
	meta       rawSubagentMeta
	stem       string
	workflowID string

	tail       TailState
	statSize   int64
	statInode  uint64
	observed   bool
	lastGrowth time.Time

	acc            usageAccounting
	model          string
	toolCalls      int
	pending        map[string]pendingTool
	seq            int
	startedAt      time.Time
	lastActivityAt time.Time
	lastUsageAt    time.Time
	lastPrompt     int64
}

func (c *childState) reset() {
	c.acc = usageAccounting{}
	c.toolCalls = 0
	c.pending = make(map[string]pendingTool)
	c.seq = 0
	c.startedAt = time.Time{}
	c.lastPrompt = 0
}

type journalStart struct {
	label string
	phase string
}

type journalState struct {
	tail     TailState
	records  int
	started  map[string]journalStart
	terminal map[string]string
	phase    string
}

type rawJournal struct {
	Type    string `json:"type"`
	AgentID string `json:"agentId"`
	Label   string `json:"label"`
	Phase   string `json:"phase"`
}

type subagentTracker struct {
	children      map[string]*childState
	journals      map[string]*journalState
	notifications map[string]string
}

func newSubagentTracker() subagentTracker {
	return subagentTracker{
		children:      make(map[string]*childState),
		journals:      make(map[string]*journalState),
		notifications: make(map[string]string),
	}
}

func (t *subagentTracker) noteNotification(n *TaskNotification) {
	if n != nil && n.ToolUseID != "" {
		t.notifications[n.ToolUseID] = n.Status
	}
}

func Walk(dir string, resultedToolUseIDs map[string]bool) ([]domain.Subagent, error) {
	resulted := make(map[string]bool, len(resultedToolUseIDs))
	for id := range resultedToolUseIDs {
		resulted[id] = true
	}
	tr := newSubagentTracker()
	subs, _, _, err := tr.collect(dir, time.Now(), resulted)
	return subs, err
}

func (t *subagentTracker) collect(dir string, now time.Time, resulted map[string]bool) ([]domain.Subagent, []domain.Workflow, time.Time, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		t.children = make(map[string]*childState)
		t.journals = make(map[string]*journalState)
		return nil, nil, time.Time{}, nil
	}
	if err != nil {
		return nil, nil, time.Time{}, err
	}

	seenChildren := make(map[string]bool)
	seenJournals := make(map[string]bool)
	var workflowDirs []string

	t.scanAgents(dir, "", entries, now, resulted, seenChildren)
	for _, ent := range entries {
		if !ent.IsDir() || ent.Name() != "workflows" {
			continue
		}
		wfRoot := filepath.Join(dir, "workflows")
		wfEntries, err := os.ReadDir(wfRoot)
		if err != nil {
			break
		}
		for _, wf := range wfEntries {
			if !wf.IsDir() || !workflowDirPattern.MatchString(wf.Name()) {
				continue
			}
			wfDir := filepath.Join(wfRoot, wf.Name())
			agentEntries, err := os.ReadDir(wfDir)
			if err != nil {
				continue
			}
			workflowDirs = append(workflowDirs, wf.Name())
			t.scanAgents(wfDir, wf.Name(), agentEntries, now, resulted, seenChildren)
			t.readJournal(wfDir)
			seenJournals[wfDir] = true
		}
	}

	for p := range t.children {
		if !seenChildren[p] {
			delete(t.children, p)
		}
	}
	for p := range t.journals {
		if !seenJournals[p] {
			delete(t.journals, p)
		}
	}

	journalsByID := make(map[string]*journalState, len(workflowDirs))
	for _, name := range workflowDirs {
		journalsByID[name] = t.journals[filepath.Join(dir, "workflows", name)]
	}

	var subs []domain.Subagent
	var lastUsage time.Time
	for _, c := range t.children {
		subs = append(subs, t.subagentOf(c, journalsByID[c.workflowID], now, resulted))
		if c.lastUsageAt.After(lastUsage) {
			lastUsage = c.lastUsageAt
		}
	}
	propagateRunning(subs)
	subs = treeOrder(subs)
	workflows := summariseWorkflows(workflowDirs, journalsByID, subs)
	return subs, workflows, lastUsage, nil
}

func (t *subagentTracker) scanAgents(dir, workflowID string, entries []os.DirEntry, now time.Time, resulted map[string]bool, seen map[string]bool) {
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		m := subagentMetaPattern.FindStringSubmatch(ent.Name())
		if m == nil {
			continue
		}
		stem := m[1]
		path := filepath.Join(dir, "agent-"+stem+".jsonl")
		c := t.children[path]
		if c == nil {
			raw, err := os.ReadFile(filepath.Join(dir, ent.Name()))
			if err != nil {
				continue
			}
			var meta rawSubagentMeta
			if err := json.Unmarshal(raw, &meta); err != nil {
				continue
			}
			c = &childState{
				meta:       meta,
				stem:       stem,
				workflowID: workflowID,
				tail:       TailState{Path: path},
				pending:    make(map[string]pendingTool),
			}
			t.children[path] = c
		}
		seen[path] = true
		t.readChild(c, now, resulted)
	}
}

func (t *subagentTracker) readChild(c *childState, now time.Time, resulted map[string]bool) {
	fi, err := os.Stat(c.tail.Path)
	if err != nil {
		return
	}
	size, ino := fi.Size(), inodeOf(fi)
	if c.observed && (size != c.statSize || ino != c.statInode) {
		c.lastGrowth = now
	}
	c.statSize, c.statInode, c.observed = size, ino, true

	if ino == c.tail.Inode && size == c.tail.Offset {
		return
	}
	res, err := Tail(c.tail)
	if err != nil {
		return
	}
	if res.Reset {
		c.reset()
	}
	c.tail = res.State
	for _, line := range res.Lines {
		ev, err := ParseRecord(line)
		if err != nil {
			continue
		}
		t.applyChildEvent(c, ev, resulted)
	}
}

func (t *subagentTracker) applyChildEvent(c *childState, ev Event, resulted map[string]bool) {
	if ts := ev.Timestamp; !ts.IsZero() {
		if c.startedAt.IsZero() {
			c.startedAt = ts
		}
		if ts.After(c.lastActivityAt) {
			c.lastActivityAt = ts
		}
	}
	if realModel(ev.Model) {
		c.model = ev.Model
	}
	if ev.HasUsage {
		c.acc.add(ev, c.model)
		if !ev.Usage.IsZero() {
			c.lastPrompt = promptTokens(ev.Usage)
		}
		if ev.Timestamp.After(c.lastUsageAt) {
			c.lastUsageAt = ev.Timestamp
		}
	}
	for _, tc := range ev.Tools {
		c.toolCalls++
		c.seq++
		if tc.ID != "" {
			c.pending[tc.ID] = pendingTool{name: tc.Name, seq: c.seq}
		}
	}
	for _, id := range ev.ToolResultIDs {
		delete(c.pending, id)
		resulted[id] = true
	}
	t.noteNotification(ev.Notification)
}

func (t *subagentTracker) readJournal(wfDir string) {
	j := t.journals[wfDir]
	if j == nil {
		j = &journalState{
			tail:     TailState{Path: filepath.Join(wfDir, "journal.jsonl")},
			started:  make(map[string]journalStart),
			terminal: make(map[string]string),
		}
		t.journals[wfDir] = j
	}
	fi, err := os.Stat(j.tail.Path)
	if err != nil {
		return
	}
	if inodeOf(fi) == j.tail.Inode && fi.Size() == j.tail.Offset {
		return
	}
	res, err := Tail(j.tail)
	if err != nil {
		return
	}
	j.tail = res.State
	for _, line := range res.Lines {
		var rec rawJournal
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		j.records++
		switch rec.Type {
		case "started":
			s := j.started[rec.AgentID]
			if rec.Label != "" {
				s.label = rec.Label
			}
			if rec.Phase != "" {
				s.phase = rec.Phase
				j.phase = rec.Phase
			}
			j.started[rec.AgentID] = s
		case "result":
			j.terminal[rec.AgentID] = domain.SubagentDone
		case "failed":
			j.terminal[rec.AgentID] = domain.SubagentFailed
		}
	}
}

func (t *subagentTracker) subagentOf(c *childState, j *journalState, now time.Time, resulted map[string]bool) domain.Subagent {
	var start journalStart
	if j != nil {
		start = j.started[c.stem]
	}
	desc := c.meta.Description
	if desc == "" {
		desc = start.label
	}
	if desc == "" {
		desc = c.meta.Name
	}

	var current string
	newest := -1
	for _, p := range c.pending {
		if p.seq > newest {
			newest, current = p.seq, p.name
		}
	}

	sub := domain.Subagent{
		TokenRate:      c.acc.window.Rate(now),
		ID:             c.stem,
		ParentID:       c.meta.ParentAgentID,
		WorkflowID:     c.workflowID,
		Phase:          start.phase,
		Hash:           c.meta.Hash,
		AgentType:      c.meta.AgentType,
		Description:    desc,
		Model:          c.model,
		ToolUseID:      c.meta.ToolUseID,
		SpawnDepth:     c.meta.SpawnDepth,
		Background:     c.meta.RequestShape == "background",
		StartedAt:      c.startedAt,
		LastActivityAt: c.lastActivityAt,
		CurrentTool:    current,
		ToolCalls:      c.toolCalls,
		ContextUsed:    c.lastPrompt,
		Usage:          c.acc.usage,
		Ledger:         c.acc.ledger.Clone(),
	}

	switch {
	case c.workflowID != "":
		if j != nil {
			sub.Status = j.terminal[c.stem]
		}
	case sub.Background:
		// The spawn's own tool_result is the launch acknowledgement, not
		// completion; only the task-notification ends a background child.
		if st, ok := t.notifications[sub.ToolUseID]; ok && sub.ToolUseID != "" {
			if st == "completed" {
				sub.Status = domain.SubagentDone
			} else {
				sub.Status = domain.SubagentFailed
			}
		}
	default:
		if sub.ToolUseID != "" && resulted[sub.ToolUseID] {
			sub.Status = domain.SubagentDone
		}
	}
	if sub.Status == "" {
		last := c.lastActivityAt
		if c.lastGrowth.After(last) {
			last = c.lastGrowth
		}
		if current != "" || (!last.IsZero() && now.Sub(last) <= subagentQuietAfter) {
			sub.Status = domain.SubagentRunning
		} else {
			sub.Status = domain.SubagentIdle
		}
	}
	sub.Live = sub.Status == domain.SubagentRunning
	return sub
}

func propagateRunning(subs []domain.Subagent) {
	index := make(map[string]int, len(subs))
	for i, s := range subs {
		index[s.ID] = i
	}
	for i := range subs {
		if subs[i].Status != domain.SubagentRunning {
			continue
		}
		visited := map[string]bool{subs[i].ID: true}
		for p, ok := index[subs[i].ParentID]; ok && !visited[subs[p].ID]; p, ok = index[subs[p].ParentID] {
			visited[subs[p].ID] = true
			if subs[p].Status == domain.SubagentIdle {
				subs[p].Status = domain.SubagentRunning
				subs[p].Live = true
			}
		}
	}
}

func byStartThenID(subs []domain.Subagent) func(i, j int) bool {
	return func(i, j int) bool {
		if !subs[i].StartedAt.Equal(subs[j].StartedAt) {
			return subs[i].StartedAt.Before(subs[j].StartedAt)
		}
		return subs[i].ID < subs[j].ID
	}
}

func treeOrder(subs []domain.Subagent) []domain.Subagent {
	if len(subs) == 0 {
		return nil
	}
	sort.Slice(subs, byStartThenID(subs))

	known := make(map[string]bool, len(subs))
	for _, s := range subs {
		known[s.ID] = true
	}
	children := make(map[string][]int)
	var roots []int
	wfAgents := make(map[string][]int)
	var wfOrder []string
	for i, s := range subs {
		switch {
		case s.WorkflowID != "":
			if _, ok := wfAgents[s.WorkflowID]; !ok {
				wfOrder = append(wfOrder, s.WorkflowID)
			}
			wfAgents[s.WorkflowID] = append(wfAgents[s.WorkflowID], i)
		case s.ParentID != "" && s.ParentID != s.ID && known[s.ParentID]:
			children[s.ParentID] = append(children[s.ParentID], i)
		default:
			roots = append(roots, i)
		}
	}

	out := make([]domain.Subagent, 0, len(subs))
	emitted := make([]bool, len(subs))
	var visit func(i int)
	visit = func(i int) {
		if emitted[i] {
			return
		}
		emitted[i] = true
		out = append(out, subs[i])
		for _, c := range children[subs[i].ID] {
			visit(c)
		}
	}
	for _, r := range roots {
		visit(r)
	}
	for _, wf := range wfOrder {
		for _, i := range wfAgents[wf] {
			visit(i)
		}
	}
	for i := range subs {
		visit(i)
	}
	return out
}

func summariseWorkflows(names []string, journals map[string]*journalState, subs []domain.Subagent) []domain.Workflow {
	byID := make(map[string]*domain.Workflow, len(names))
	var out []*domain.Workflow
	for _, name := range names {
		w := &domain.Workflow{ID: name}
		if j := journals[name]; j != nil {
			w.Phase = j.phase
		}
		byID[name] = w
		out = append(out, w)
	}

	unfinished := make(map[string]bool)
	for _, s := range subs {
		w := byID[s.WorkflowID]
		if w == nil {
			continue
		}
		w.Agents++
		switch s.Status {
		case domain.SubagentRunning:
			w.Running++
			unfinished[w.ID] = true
		case domain.SubagentIdle:
			unfinished[w.ID] = true
		case domain.SubagentDone:
			w.Done++
		case domain.SubagentFailed:
			w.Failed++
		}
		if !s.StartedAt.IsZero() && (w.StartedAt.IsZero() || s.StartedAt.Before(w.StartedAt)) {
			w.StartedAt = s.StartedAt
		}
		if s.LastActivityAt.After(w.LastActivityAt) {
			w.LastActivityAt = s.LastActivityAt
		}
		w.Usage.Input += s.Usage.Input
		w.Usage.Output += s.Usage.Output
		w.Usage.CacheRead += s.Usage.CacheRead
		w.Usage.CacheCreate5m += s.Usage.CacheCreate5m
		w.Usage.CacheCreate1h += s.Usage.CacheCreate1h
		w.Usage.Thinking += s.Usage.Thinking
		w.Usage.CachedInput += s.Usage.CachedInput
		if s.TokenRate != nil {
			if w.TokenRate == nil {
				w.TokenRate = &domain.TokenRate{}
			}
			w.TokenRate.InputPerSec += s.TokenRate.InputPerSec
			w.TokenRate.OutputPerSec += s.TokenRate.OutputPerSec
			w.TokenRate.CacheReadPerSec += s.TokenRate.CacheReadPerSec
		}
	}

	var result []domain.Workflow
	for _, w := range out {
		if w.Agents == 0 && (journals[w.ID] == nil || journals[w.ID].records == 0) {
			continue
		}
		switch {
		case w.Agents == 0:
			w.Status = domain.SubagentIdle
		case w.Running > 0:
			w.Status = domain.SubagentRunning
		case unfinished[w.ID]:
			w.Status = domain.SubagentIdle
		case w.Failed > 0:
			w.Status = domain.SubagentFailed
		default:
			w.Status = domain.SubagentDone
		}
		result = append(result, *w)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if zi, zj := result[i].StartedAt.IsZero(), result[j].StartedAt.IsZero(); zi != zj {
			return zj
		}
		if !result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].StartedAt.Before(result[j].StartedAt)
		}
		return result[i].ID < result[j].ID
	})
	return result
}
