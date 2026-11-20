package codex

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func Bind(rollouts []Rollout, procs []domain.ProcSample) map[string]*int {
	out := make(map[string]*int, len(rollouts))
	for path, b := range bindAll(rollouts, procs, "") {
		out[path] = b.PID
	}
	return out
}

type binding struct {
	PID  *int
	Conf domain.BindConf
}

type candidate struct {
	pid       int
	startTime time.Time
}

func bindAll(rollouts []Rollout, procs []domain.ProcSample, resolvedCodexPath string) map[string]binding {
	result := make(map[string]binding, len(rollouts))
	for _, r := range rollouts {
		result[r.Path] = binding{PID: nil, Conf: domain.BindUnknown}
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

	rolloutsByCWD := make(map[string][]int)
	for i, r := range rollouts {
		if r.CWD == "" {
			continue
		}
		rolloutsByCWD[r.CWD] = append(rolloutsByCWD[r.CWD], i)
	}

	var singleCWDs, sharedCWDs []string
	for cwd, idxs := range rolloutsByCWD {
		if len(idxs) == 1 {
			singleCWDs = append(singleCWDs, cwd)
		} else {
			sharedCWDs = append(sharedCWDs, cwd)
		}
	}
	sort.Strings(singleCWDs)
	sort.Strings(sharedCWDs)

	b := binder{rollouts: rollouts, byCWD: byCWD, rolloutsByCWD: rolloutsByCWD, used: make(map[int]bool), result: result}
	b.bindSingleRolloutCWDs(singleCWDs)
	b.bindSharedCWDs(sharedCWDs)
	return result
}

type binder struct {
	rollouts      []Rollout
	byCWD         map[string][]candidate
	rolloutsByCWD map[string][]int
	used          map[int]bool
	result        map[string]binding
}

func (b *binder) bind(idx, pid int, conf domain.BindConf) {
	b.result[b.rollouts[idx].Path] = binding{PID: &pid, Conf: conf}
	b.used[pid] = true
}

func (b *binder) bindSingleRolloutCWDs(cwds []string) {
	for _, cwd := range cwds {
		idx := b.rolloutsByCWD[cwd][0]
		contenders := unusedContenders(b.byCWD[cwd], b.used)
		switch len(contenders) {
		case 0:
		case 1:
			b.bind(idx, contenders[0].pid, domain.BindExact)
		default:
			if pid, ok := nearestNotBefore(contenders, b.rollouts[idx].MetaAt); ok {
				b.bind(idx, pid, domain.BindInferred)
			}
		}
	}
}

func (b *binder) bindSharedCWDs(cwds []string) {
	for _, cwd := range cwds {
		for _, idx := range b.rolloutsByCWD[cwd] {
			if pid, ok := nearestNotBefore(unusedContenders(b.byCWD[cwd], b.used), b.rollouts[idx].MetaAt); ok {
				b.bind(idx, pid, domain.BindInferred)
			}
		}
	}
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

func nearestNotBefore(cands []candidate, metaAt time.Time) (int, bool) {
	if metaAt.IsZero() {
		return 0, false
	}
	best := -1
	var bestDiff time.Duration
	for _, c := range cands {
		if c.startTime.After(metaAt) {
			continue
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

func anyCodexCandidate(procs []domain.ProcSample, resolvedCodexPath string) bool {
	for _, p := range procs {
		if isCodexCandidate(p, resolvedCodexPath) {
			return true
		}
	}
	return false
}

func isCodexCandidate(p domain.ProcSample, resolvedCodexPath string) bool {
	if len(p.Argv) == 0 || isAppServerDaemon(p.Argv) {
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

func isAppServerDaemon(argv []string) bool {
	for i, a := range argv[1:] {
		if a == "app-server" {
			for _, b := range argv[i+2:] {
				if b == "daemon" {
					return true
				}
			}
			return false
		}
	}
	return false
}
