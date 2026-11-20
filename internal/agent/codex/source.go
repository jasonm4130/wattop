package codex

import (
	"context"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

const shortlistWindow = 24 * time.Hour

type pinnedPID struct {
	pid   int
	start time.Time
}

const DefaultIdleThreshold = 5 * time.Minute

const DefaultLookback = 2 * time.Hour

type Source struct {
	root          string
	idleThreshold time.Duration
	lookback      time.Duration
	codexPath     string

	index    *rolloutIndex
	tails    map[string]*tailState
	rollouts map[string]*Rollout

	pinned map[string]pinnedPID
}

func NewSource(root string, idleThreshold time.Duration) *Source {
	codexPath, _ := exec.LookPath("codex")
	return &Source{
		root:          root,
		idleThreshold: idleThreshold,
		lookback:      DefaultLookback,
		codexPath:     codexPath,
		index:         newRolloutIndex(root),
		tails:         make(map[string]*tailState),
		rollouts:      make(map[string]*Rollout),
		pinned:        make(map[string]pinnedPID),
	}
}

func (s *Source) WithLookback(d time.Duration) *Source {
	if d > 0 {
		s.lookback = d
	}
	return s
}

func (s *Source) Name() string { return "codex" }

func (s *Source) Close() error { return nil }

func (s *Source) Poll(ctx context.Context, now time.Time, procs []domain.ProcSample) ([]domain.Session, error) {
	paths, err := s.index.shortlist(now, now.Add(-shortlistWindow))
	if err != nil {
		return nil, err
	}
	paths = s.addPinned(paths, procs)

	codexRunning := anyCodexCandidate(procs, s.codexPath)
	cutoff := now.Add(-s.lookback)

	live := make(map[string]bool, len(paths))
	fresh := make(map[string]bool, len(paths))
	var rollouts []Rollout

	for _, path := range paths {
		mtime, err := statModTime(path)
		if err != nil {
			continue
		}
		recent := !mtime.Before(cutoff)
		if !recent && !codexRunning {
			continue
		}
		fresh[path] = recent
		live[path] = true

		t, ok := s.tails[path]
		if !ok {
			t = &tailState{path: path}
			s.tails[path] = t
		}
		r, ok := s.rollouts[path]
		if !ok {
			r = &Rollout{Path: path}
			s.rollouts[path] = r
		}

		lines, err := t.readNewLines()
		if err != nil {
			continue
		}
		for _, line := range lines {
			_ = r.Apply(line)
		}
		r.ModTime = mtime

		rollouts = append(rollouts, *r)
	}

	for path := range s.tails {
		if !live[path] {
			delete(s.tails, path)
			delete(s.rollouts, path)
		}
	}

	// Children run inside their root's process, not their own — binding one
	// to a pid would steal that pid from the root it actually belongs to, so
	// only top-level rollouts are offered to bindAll.
	var topLevel []Rollout
	for _, r := range rollouts {
		if r.ParentThreadID == "" {
			topLevel = append(topLevel, r)
		}
	}
	binds := bindAll(topLevel, procs, s.codexPath)
	s.repin(binds, procs)

	var filtered []Rollout
	for _, r := range rollouts {
		b := binds[r.Path]
		if !fresh[r.Path] && b.PID == nil {
			continue
		}
		filtered = append(filtered, r)
	}

	byThreadID := make(map[string]Rollout, len(filtered))
	for _, r := range filtered {
		if r.ThreadID != "" {
			byThreadID[r.ThreadID] = r
		}
	}

	sessionIdxByThreadID := make(map[string]int, len(filtered))
	var sessions []domain.Session
	var owners []Rollout
	var children []Rollout

	for _, r := range filtered {
		if r.ParentThreadID != "" {
			children = append(children, r)
			continue
		}
		b := binds[r.Path]
		status := inferStatus(r.Status, r.ModTime, now, s.idleThreshold)
		sess := sessionFromRollout(r, b, status, procs)
		sess.TokenRate = r.tokens.Rate(now)
		sessions = append(sessions, sess)
		owners = append(owners, r)
		if r.ThreadID != "" {
			sessionIdxByThreadID[r.ThreadID] = len(sessions) - 1
		}
	}

	pending := make(map[int][]domain.Subagent, len(children))
	for _, c := range children {
		root, ok := walkToRoot(c, byThreadID)
		if ok {
			if idx, present := sessionIdxByThreadID[root.ThreadID]; present {
				directParent := byThreadID[c.ParentThreadID]
				parentID := ""
				if directParent.ThreadID != root.ThreadID {
					parentID = directParent.ThreadID
				}
				pending[idx] = append(pending[idx], buildSubagent(c, parentID, s.idleThreshold, now))
				if !c.lastUsageAt.IsZero() && c.lastUsageAt.After(sessions[idx].LastUsageAt) {
					sessions[idx].LastUsageAt = c.lastUsageAt
				}
				continue
			}
		}

		status := inferStatus(c.Status, c.ModTime, now, s.idleThreshold)
		sess := sessionFromRollout(c, binding{Conf: domain.BindUnknown}, status, procs)
		sess.Kind = "subagent"
		sess.TokenRate = c.tokens.Rate(now)
		sessions = append(sessions, sess)
		owners = append(owners, c)
	}
	dedupSessionIDs(sessions, owners)

	for idx, subs := range pending {
		sessions[idx].Subagents = append(sessions[idx].Subagents, sortSubagentsTree(subs)...)
	}

	return sessions, nil
}

func (s *Source) addPinned(paths []string, procs []domain.ProcSample) []string {
	if len(s.pinned) == 0 {
		return paths
	}
	alive := make(map[int]time.Time, len(procs))
	for _, p := range procs {
		alive[p.PID] = p.StartTime
	}
	have := make(map[string]bool, len(paths))
	for _, p := range paths {
		have[p] = true
	}
	added := false
	for path, pp := range s.pinned {
		if start, ok := alive[pp.pid]; !ok || !start.Equal(pp.start) {
			delete(s.pinned, path)
			continue
		}
		if !have[path] {
			paths = append(paths, path)
			added = true
		}
	}
	if added {
		sort.Strings(paths)
	}
	return paths
}

func (s *Source) repin(binds map[string]binding, procs []domain.ProcSample) {
	starts := make(map[int]time.Time, len(procs))
	for _, p := range procs {
		starts[p.PID] = p.StartTime
	}
	clear(s.pinned)
	for path, b := range binds {
		if b.PID != nil {
			s.pinned[path] = pinnedPID{pid: *b.PID, start: starts[*b.PID]}
		}
	}
}

func dedupSessionIDs(sessions []domain.Session, owners []Rollout) {
	claimOrder := make([]int, len(sessions))
	for i := range claimOrder {
		claimOrder[i] = i
	}
	sort.Slice(claimOrder, func(a, b int) bool {
		ra, rb := owners[claimOrder[a]], owners[claimOrder[b]]
		if aChild, bChild := ra.ParentThreadID != "", rb.ParentThreadID != ""; aChild != bChild {
			return bChild
		}
		return ra.Path < rb.Path
	})
	claimed := make(map[string]bool, len(sessions))
	for _, i := range claimOrder {
		r := owners[i]
		if r.ThreadID == "" {
			continue
		}
		if claimed[r.ThreadID] {
			sessions[i].ID = r.ThreadID + "#" + filepath.Base(r.Path)
			continue
		}
		claimed[r.ThreadID] = true
	}
}

func sessionFromRollout(r Rollout, b binding, status domain.SessionStatus, procs []domain.ProcSample) domain.Session {
	// Session identity is the thread id where one is known (unique per
	// thread, never shared with a parent or sibling); SessionID is the
	// fallback (Codex reuses one session_id across a whole parent/child
	// tree), and the file path is the last resort for a rollout whose
	// session_meta never arrived (e.g. a file that opens on turn_context).
	id := r.ThreadID
	if id == "" {
		id = r.SessionID
	}
	if id == "" {
		id = r.Path
	}

	sess := domain.Session{
		Agent:        "codex",
		ID:           id,
		PID:          b.PID,
		BindConf:     b.Conf,
		CWD:          r.CWD,
		Kind:         "exec",
		Status:       status,
		StatusSince:  r.ModTime,
		Model:        r.Model,
		Usage:        r.Usage,
		Ledger:       r.Ledger.Clone(),
		ContextUsed:  r.ContextUsed,
		ContextMax:   r.ContextMax,
		ContextExact: r.ContextExact,
		RateLimits:   r.RateLimits,
		LastUsageAt:  r.lastUsageAt,
	}

	if b.PID != nil {
		for _, p := range procs {
			if p.PID == *b.PID {
				sess.Cmdline = strings.Join(p.Argv, " ")
				break
			}
		}
	}

	return sess
}
