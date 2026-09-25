package codex

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// shortlistWindow bounds how far back ShortlistRollouts looks for a rollout
// file, by mtime. A rollout untouched for longer than this cannot be a live
// session regardless of its status inference. It is the outer bound only:
// Poll then applies the much tighter lookback below, and reaches past it
// solely for a rollout that binds to a live codex process.
const shortlistWindow = 24 * time.Hour

// DefaultIdleThreshold is the mtime age past which a rollout with no other
// signal is considered stale, per the plan's default.
const DefaultIdleThreshold = 5 * time.Minute

// DefaultLookback is how far back Poll considers a rollout at all, by
// mtime. Codex leaves one rollout file per exec run behind forever, so a
// day's shortlist on a working machine is a dozen-odd dead transcripts that
// bury every live session in the table. A rollout older than this is only
// reached for when it still binds to a live codex process — the one case
// where an old file describes something running right now.
//
// cmd/wattop overrides it from config via WithLookback; see that method's
// comment for the config key.
const DefaultLookback = 2 * time.Hour

// Source implements domain.AgentSource for the Codex CLI.
type Source struct {
	root          string
	idleThreshold time.Duration
	lookback      time.Duration
	codexPath     string // resolved once at startup; "" if codex is not on $PATH.

	tails    map[string]*tailState
	rollouts map[string]*Rollout
}

// NewSource constructs a Codex Source rooted at root (normally
// ~/.codex/sessions). exec.LookPath("codex") is resolved once here, not in
// bind.go, which stays syscall-free; a lookup failure (codex not installed,
// or running on Linux CI) is tolerated and simply disables the resolved-path
// half of candidate selection — the argv-basename match still works.
func NewSource(root string, idleThreshold time.Duration) *Source {
	codexPath, _ := exec.LookPath("codex")
	return &Source{
		root:          root,
		idleThreshold: idleThreshold,
		lookback:      DefaultLookback,
		codexPath:     codexPath,
		tails:         make(map[string]*tailState),
		rollouts:      make(map[string]*Rollout),
	}
}

// WithLookback overrides DefaultLookback (config key
// `codex_lookback_minutes`). It is a setter rather than a NewSource
// parameter for the same reason ui.Model.WithNoColor is: cmd/wattop can
// wire the config key without every other caller of NewSource changing
// shape. A non-positive d is ignored, so an unset config key keeps the
// default rather than collapsing the window to zero.
func (s *Source) WithLookback(d time.Duration) *Source {
	if d > 0 {
		s.lookback = d
	}
	return s
}

func (s *Source) Name() string { return "codex" }

func (s *Source) Close() error { return nil }

// Poll shortlists rollout files by mtime, tails whichever are new since the
// last call, binds the result against procs, and emits one domain.Session
// per rollout that is either within the lookback window or bound to a live
// pid — including a recent one that binds to no pid, which renders rather
// than being dropped. Only a rollout that is both older than the lookback
// and bound to nothing is dropped, since nothing about it can be current.
func (s *Source) Poll(ctx context.Context, now time.Time, procs []domain.ProcSample) ([]domain.Session, error) {
	paths, err := ShortlistRollouts(s.root, now, shortlistWindow)
	if err != nil {
		return nil, err
	}

	// Tier 1 is every rollout touched within the lookback; tier 2 is the
	// older ones, which are read at all only when a live codex process
	// exists for them to bind to. With no codex running — the common case
	// on a machine whose Codex work finished hours ago — the whole of tier
	// 2 is skipped without being opened.
	codexRunning := anyCodexCandidate(procs, s.codexPath)
	cutoff := now.Add(-s.lookback)

	live := make(map[string]bool, len(paths))
	fresh := make(map[string]bool, len(paths))
	var rollouts []Rollout

	for _, path := range paths {
		mtime, err := statModTime(path)
		if err != nil {
			continue // the file vanished between the shortlist and here.
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
			continue // a transient read error leaves this rollout's prior state in place.
		}
		for _, line := range lines {
			_ = r.Apply(line) // a malformed line is skipped, never fatal to the rollout.
		}
		r.ModTime = mtime

		rollouts = append(rollouts, *r)
	}

	// Drop tailer/rollout state for anything that fell out of the shortlist
	// window so this map does not grow without bound over a long run.
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

	// filtered applies the same "old and bound to nothing" drop to every
	// rollout, root or child alike: a child's binding is always nil (it is
	// never in binds), so a stale child is dropped exactly when a stale,
	// unbound top-level rollout would be.
	var filtered []Rollout
	for _, r := range rollouts {
		b := binds[r.Path]
		if !fresh[r.Path] && b.PID == nil {
			// Older than the lookback and bound to nothing: a dead
			// transcript, not a session. Dropping it here rather than in
			// the table keeps --json honest too — it is not a session that
			// happens to be hidden, it is not a session.
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

	usedThreadIDs := make(map[string]bool, len(filtered))
	sessionIdxByThreadID := make(map[string]int, len(filtered))
	var sessions []domain.Session
	var children []Rollout

	for _, r := range filtered {
		if r.ParentThreadID != "" {
			children = append(children, r)
			continue
		}
		b := binds[r.Path]
		status := inferStatus(r.Status, r.ModTime, now, s.idleThreshold)
		sess := sessionFromRollout(r, b, status, procs)
		sess.ID = dedupSessionID(r, sess.ID, usedThreadIDs)
		sess.TokenRate = r.tokens.Rate(now)
		sessions = append(sessions, sess)
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

		// The root is not present in this poll (or the parent chain is
		// broken): this child cannot be folded, so it renders as its own
		// session instead of being silently dropped.
		status := inferStatus(c.Status, c.ModTime, now, s.idleThreshold)
		sess := sessionFromRollout(c, binding{Conf: "unknown"}, status, procs)
		sess.Kind = "subagent"
		sess.ID = dedupSessionID(c, sess.ID, usedThreadIDs)
		sess.TokenRate = c.tokens.Rate(now)
		sessions = append(sessions, sess)
	}

	for idx, subs := range pending {
		sessions[idx].Subagents = append(sessions[idx].Subagents, sortSubagentsTree(subs)...)
	}

	return sessions, nil
}

// dedupSessionID keeps two rollouts that happen to share a ThreadID within
// one poll from colliding into the same session row: the later one (by the
// path-sorted order Poll processes rollouts in) gets its path folded into
// the id instead. A rollout with no ThreadID never triggers this — its id
// already came from SessionID or Path, which are unique per rollout.
func dedupSessionID(r Rollout, id string, used map[string]bool) string {
	if r.ThreadID == "" {
		return id
	}
	if used[r.ThreadID] {
		return r.ThreadID + "#" + filepath.Base(r.Path)
	}
	used[r.ThreadID] = true
	return id
}

// sessionFromRollout maps one Rollout plus its bind result into the
// domain.Session the reducer consumes. It is a pure function so bind_test.go
// can exercise the "unbound rollout still renders" case directly.
func sessionFromRollout(r Rollout, b binding, status string, procs []domain.ProcSample) domain.Session {
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
