package codex

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// shortlistWindow bounds how far back ShortlistRollouts looks for a rollout
// file, by mtime. A rollout untouched for longer than this cannot be a live
// session regardless of its status inference.
const shortlistWindow = 24 * time.Hour

// DefaultIdleThreshold is the mtime age past which a rollout with no other
// signal is considered stale, per the plan's default.
const DefaultIdleThreshold = 5 * time.Minute

// Source implements domain.AgentSource for the Codex CLI.
type Source struct {
	root          string
	idleThreshold time.Duration
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
		codexPath:     codexPath,
		tails:         make(map[string]*tailState),
		rollouts:      make(map[string]*Rollout),
	}
}

func (s *Source) Name() string { return "codex" }

func (s *Source) Close() error { return nil }

// Poll shortlists rollout files by mtime, tails whichever are new since the
// last call, binds the result against procs, and emits one domain.Session
// per shortlisted rollout — including one that binds to no pid, which
// renders rather than being dropped.
func (s *Source) Poll(ctx context.Context, now time.Time, procs []domain.ProcSample) ([]domain.Session, error) {
	paths, err := ShortlistRollouts(s.root, now, shortlistWindow)
	if err != nil {
		return nil, err
	}

	live := make(map[string]bool, len(paths))
	var rollouts []Rollout

	for _, path := range paths {
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
		if mt, err := statModTime(path); err == nil {
			r.ModTime = mt
		}

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

	binds := bindAll(rollouts, procs, s.codexPath)

	sessions := make([]domain.Session, 0, len(rollouts))
	for _, r := range rollouts {
		status := inferStatus(r.Status, r.ModTime, now, s.idleThreshold)
		sessions = append(sessions, sessionFromRollout(r, binds[r.Path], status, procs))
	}
	return sessions, nil
}

// sessionFromRollout maps one Rollout plus its bind result into the
// domain.Session the reducer consumes. It is a pure function so bind_test.go
// can exercise the "unbound rollout still renders" case directly.
func sessionFromRollout(r Rollout, b binding, status string, procs []domain.ProcSample) domain.Session {
	id := r.SessionID
	if id == "" {
		// session_meta never arrived for this rollout (e.g. a file that
		// opens on turn_context) — fall back to the filename, which is
		// always unique.
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
