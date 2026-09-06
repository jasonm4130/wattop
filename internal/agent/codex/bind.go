package codex

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// Bind performs the Codex pid join. Its only input is the []domain.ProcSample
// handed to Poll — it runs no lsof, no ps, and no syscall, which keeps this
// package green on Linux CI and makes the bind unit-testable from a table of
// literals. The returned map is keyed by Rollout.Path (the one identifier
// every rollout has, session_meta or not).
//
// A candidate proc is selected by Argv[0]'s basename being "codex"; matching
// the resolved codex binary path is source.go's job (exec.LookPath is
// discovered once at startup, not in this syscall-free package), so a
// direct call to Bind only matches on the argv-basename form. Use bindAll
// (unexported) when the resolved path matters.
func Bind(rollouts []Rollout, procs []domain.ProcSample) map[string]*int {
	out := make(map[string]*int, len(rollouts))
	for path, b := range bindAll(rollouts, procs, "") {
		out[path] = b.PID
	}
	return out
}

// binding is one rollout's bind result: the pid it resolved to (nil if
// unbound) and the confidence that produced it.
type binding struct {
	PID  *int
	Conf string // "exact" | "inferred" | "unknown"
}

// candidate is a codex process usable for binding: it has a non-empty argv,
// a readable cwd, and a derivable start time. Task 6 fills Argv and CWD for
// exactly this purpose; a candidate missing either, or whose StartTime is
// the zero time.Time (Task 6's guard for an underivable start), is treated
// as unmatched rather than guessed at.
type candidate struct {
	pid       int
	startTime time.Time
}

// bindAll is the pure matching core behind Bind. resolvedCodexPath, when
// non-empty, additionally matches a candidate whose argv contains that exact
// path (the codex binary run via its full path rather than found on $PATH).
//
// Selection is by cwd: turn_context.payload.cwd against ProcSample.CWD.
// Two codex exec runs in the same cwd is a real case on this machine, so the
// disambiguator is time, not path — the rollout whose session_meta timestamp
// is nearest to, and not before, the candidate's start time. A rollout is
// never bound to two pids: when more than one live candidate could match a
// rollout, the tiebreaker picks at most one winner and the rest stay
// unmatched for that rollout rather than being guessed.
func bindAll(rollouts []Rollout, procs []domain.ProcSample, resolvedCodexPath string) map[string]binding {
	result := make(map[string]binding, len(rollouts))
	for _, r := range rollouts {
		result[r.Path] = binding{PID: nil, Conf: "unknown"}
	}

	byCWD := make(map[string][]candidate)
	for _, p := range procs {
		if !isCodexCandidate(p, resolvedCodexPath) {
			continue
		}
		if p.CWD == "" || p.StartTime.IsZero() {
			continue
		}
		byCWD[p.CWD] = append(byCWD[p.CWD], candidate{pid: p.PID, startTime: p.StartTime})
	}

	rolloutsByCWD := make(map[string][]int) // cwd -> indices into rollouts
	for i, r := range rollouts {
		if r.CWD == "" {
			continue
		}
		rolloutsByCWD[r.CWD] = append(rolloutsByCWD[r.CWD], i)
	}

	used := make(map[int]bool) // pids already bound to some rollout

	// Resolve cwds with exactly one rollout first: those are the "exact"
	// candidates by definition (unless more than one live pid also shares
	// that cwd, in which case they fall back to the same time tiebreaker as
	// an ambiguous cwd, demoted to "inferred"). Doing these first means an
	// unambiguous bind never loses its pid to a later group's greedy pass.
	var exactCWDs, ambiguousCWDs []string
	for cwd, idxs := range rolloutsByCWD {
		if len(idxs) == 1 {
			exactCWDs = append(exactCWDs, cwd)
		} else {
			ambiguousCWDs = append(ambiguousCWDs, cwd)
		}
	}
	sort.Strings(exactCWDs)
	sort.Strings(ambiguousCWDs)

	for _, cwd := range exactCWDs {
		idx := rolloutsByCWD[cwd][0]
		contenders := unusedContenders(byCWD[cwd], used)
		switch len(contenders) {
		case 0:
			// no live candidate; stays unknown.
		case 1:
			pid := contenders[0].pid
			result[rollouts[idx].Path] = binding{PID: &pid, Conf: "exact"}
			used[pid] = true
		default:
			// Only one rollout has this cwd, but more than one live pid does
			// too: refuse to guess which pid owns it and fall back to the
			// time tiebreaker instead of binding both.
			if pid, ok := nearestNotBefore(contenders, rollouts[idx].MetaAt); ok {
				result[rollouts[idx].Path] = binding{PID: &pid, Conf: "inferred"}
				used[pid] = true
			}
		}
	}

	for _, cwd := range ambiguousCWDs {
		for _, idx := range rolloutsByCWD[cwd] {
			contenders := unusedContenders(byCWD[cwd], used)
			if len(contenders) == 0 {
				continue
			}
			if pid, ok := nearestNotBefore(contenders, rollouts[idx].MetaAt); ok {
				result[rollouts[idx].Path] = binding{PID: &pid, Conf: "inferred"}
				used[pid] = true
			}
		}
	}

	return result
}

func unusedContenders(cands []candidate, used map[int]bool) []candidate {
	var out []candidate
	for _, c := range cands {
		if !used[c.pid] {
			out = append(out, c)
		}
	}
	return out
}

// nearestNotBefore picks the candidate whose start time is nearest to, and
// not after, metaAt (a rollout's session_meta timestamp cannot precede the
// process it belongs to). Both sides are wall-clock UTC — session_meta is
// parsed as UTC and ProcSample.StartTime arrives already converted from
// ri_proc_start_abstime by Task 6's StartWall; this function does no
// timebase arithmetic and never sees a tick count.
func nearestNotBefore(cands []candidate, metaAt time.Time) (int, bool) {
	if metaAt.IsZero() {
		return 0, false
	}
	best := -1
	var bestDiff time.Duration
	for _, c := range cands {
		if c.startTime.After(metaAt) {
			continue // the process started after this session was created: not a match.
		}
		diff := metaAt.Sub(c.startTime)
		if best == -1 || diff < bestDiff {
			best = c.pid
			bestDiff = diff
		}
	}
	if best == -1 {
		return 0, false
	}
	return best, true
}

// anyCodexCandidate reports whether procs contains any process that could
// own a rollout. Source.Poll uses it as the cheap gate on reading rollouts
// older than its lookback window: with no codex process running, no old
// rollout can be pid-bound, so none of them needs opening.
func anyCodexCandidate(procs []domain.ProcSample, resolvedCodexPath string) bool {
	for _, p := range procs {
		if isCodexCandidate(p, resolvedCodexPath) {
			return true
		}
	}
	return false
}

func isCodexCandidate(p domain.ProcSample, resolvedCodexPath string) bool {
	if len(p.Argv) == 0 {
		return false
	}
	if filepath.Base(p.Argv[0]) == "codex" {
		return true
	}
	if resolvedCodexPath == "" {
		return false
	}
	for _, a := range p.Argv {
		if a == resolvedCodexPath {
			return true
		}
	}
	return false
}
