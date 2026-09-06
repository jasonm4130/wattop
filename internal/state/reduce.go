package state

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// Reduce turns one coordinated sampling cycle into a Snapshot. "Pure" here
// means no I/O and deterministic given (st, in), not functionally pure: it
// mutates st (rings advance, the burn tracker records observations) exactly
// as the old prev-Snapshot-threading design already did. It must never
// block, never call a collector, and never read the clock — in.At is the
// only source of time a cycle has.
func (st *State) Reduce(in Inputs) *domain.Snapshot {
	procByPID := make(map[int]domain.ProcSample, len(in.Procs))
	for _, p := range in.Procs {
		procByPID[p.PID] = p
	}

	healthFailed := make(map[string]bool, len(in.Health))
	degraded := make([]string, 0, len(in.Health))
	for _, h := range in.Health {
		if !h.OK {
			healthFailed[h.Name] = true
			degraded = append(degraded, fmt.Sprintf("%s: %s", h.Name, h.Err))
		}
	}

	unpriced := make(map[string]struct{})
	sessions := make([]domain.Session, 0, len(in.Sessions)+len(st.tracked))
	seen := make(map[sessionKey]bool, len(in.Sessions))

	// Sessions the source actually reported this cycle: refresh the
	// tracked entry from the raw, pre-enrichment session (per its own
	// source-supplied Status — this must never be overwritten here, since
	// Codex's own mtime-derived "stale" status must survive untouched),
	// then emit an enriched copy.
	for _, raw := range in.Sessions {
		key := sessionKey{Agent: raw.Agent, ID: raw.ID}
		seen[key] = true

		tr, ok := st.tracked[key]
		if !ok {
			tr = &trackedSession{}
			st.tracked[key] = tr
		}
		tr.session = raw
		tr.lastSeenAt = in.At
		tr.ttlClock = in.At
		tr.heldByOutage = false

		enriched := raw
		st.enrichSession(&enriched, procByPID, unpriced, in.At)
		sessions = append(sessions, enriched)
	}

	// Sessions retained from a previous cycle that the source omitted this
	// time. Order deterministically (Agent, ID) so Snapshot.Sessions is
	// reproducible for a given Inputs sequence.
	var missingKeys []sessionKey
	for key := range st.tracked {
		if !seen[key] {
			missingKeys = append(missingKeys, key)
		}
	}
	sort.Slice(missingKeys, func(i, j int) bool {
		if missingKeys[i].Agent != missingKeys[j].Agent {
			return missingKeys[i].Agent < missingKeys[j].Agent
		}
		return missingKeys[i].ID < missingKeys[j].ID
	})

	for _, key := range missingKeys {
		tr := st.tracked[key]

		// A failed poll is not a vanished session: while this session's
		// source reports unhealthy, re-emit the row exactly as retained,
		// run neither the stale transition nor the TTL drop, and push the
		// TTL clock forward so the outage never counts against it.
		if healthFailed[key.Agent] {
			tr.ttlClock = in.At
			tr.heldByOutage = true
			enriched := tr.session
			st.enrichSession(&enriched, procByPID, unpriced, in.At)
			sessions = append(sessions, enriched)
			continue
		}

		// First healthy cycle after an outage held this row: the TTL
		// countdown starts here, not at the last unhealthy cycle, so
		// re-anchor before the drop check — this cycle must only flip the
		// row to stale, never expire it, however long the outage ran.
		if tr.heldByOutage {
			tr.ttlClock = in.At
			tr.heldByOutage = false
		}

		if in.At.Sub(tr.ttlClock) > sessionTTL {
			delete(st.tracked, key)
			delete(st.burnLastCost, key.Agent+":"+key.ID)
			delete(st.histories, key.Agent+":"+key.ID+":cpu")
			delete(st.histories, key.Agent+":"+key.ID+":gpu")
			delete(st.histories, key.Agent+":"+key.ID+":cost")
			continue
		}

		if tr.session.Status != "stale" {
			tr.session.Status = "stale"
			tr.session.StatusSince = in.At
		}

		enriched := tr.session
		st.enrichSession(&enriched, procByPID, unpriced, in.At)
		sessions = append(sessions, enriched)
	}

	unpricedList := make([]string, 0, len(unpriced))
	for m := range unpriced {
		unpricedList = append(unpricedList, m)
	}
	sort.Strings(unpricedList)

	var totalCost, totalBurn float64
	for _, s := range sessions {
		if s.CostUSD != nil {
			totalCost += *s.CostUSD
		}
		if s.BurnUSDPerHr != nil {
			totalBurn += *s.BurnUSDPerHr
		}
	}

	sys := in.Sys
	sys.At = in.At

	var selfCPU float64
	if p, ok := procByPID[os.Getpid()]; ok {
		selfCPU = p.CPUPct
	}

	snap := &domain.Snapshot{
		At:                in.At,
		Sys:               sys,
		Sessions:          sessions,
		TotalCostUSD:      totalCost,
		TotalBurnUSDPerHr: totalBurn,
		UnpricedModels:    unpricedList,
		Degraded:          degraded,
		SelfCPUPct:        selfCPU,
	}

	st.recordHistory("cpu", machineCPUPct(sys.Clusters))
	st.recordHistory("gpu", derefOr(sys.GPU.ActivePct, 0))
	st.recordHistory("watts", derefOr(sys.Power.SystemWatts, 0))
	st.recordHistory("cost", totalBurn)

	for _, s := range sessions {
		var cpu, gpu, cost float64
		if s.Proc != nil {
			cpu = s.Proc.CPUPct
			gpu = derefOr(s.Proc.GPUMsPerSec, 0)
		}
		if s.BurnUSDPerHr != nil {
			cost = *s.BurnUSDPerHr
		}
		st.recordHistory(s.Agent+":"+s.ID+":cpu", cpu)
		st.recordHistory(s.Agent+":"+s.ID+":gpu", gpu)
		st.recordHistory(s.Agent+":"+s.ID+":cost", cost)
	}

	return snap
}

// enrichSession joins s to its ProcSample by pid, prices it (and its
// subagents) through st.book, and feeds its burn tracker. s is mutated in
// place; it must already carry the source- (or reducer-) supplied
// Status/StatusSince, which this never touches.
func (st *State) enrichSession(s *domain.Session, procByPID map[int]domain.ProcSample, unpriced map[string]struct{}, at time.Time) {
	if s.PID != nil {
		if p, ok := procByPID[*s.PID]; ok {
			pc := p
			s.Proc = &pc
		} else {
			s.Proc = nil
		}
	} else {
		s.Proc = nil
	}

	st.priceSession(s, unpriced)

	burnKey := s.Agent + ":" + s.ID
	if s.CostUSD == nil {
		s.BurnUSDPerHr = nil
		return
	}
	if last, ok := st.burnLastCost[burnKey]; !ok || last != *s.CostUSD {
		st.burn.Observe(burnKey, *s.CostUSD, at)
		st.burnLastCost[burnKey] = *s.CostUSD
	}
	rate := st.burn.RatePerHour(burnKey, at)
	s.BurnUSDPerHr = &rate
}

// priceSession prices s's own usage plus every subagent's usage through
// st.book. Each subagent's CostUSD is set independently (nil, and its model
// added to unpriced, when its own model does not resolve) so the UI can
// still show per-subagent cost breakdown. Session.CostUSD is the *combined*
// total of s's own usage and every priced subagent when s's own model
// prices; when s's own model does not resolve, Session.CostUSD is nil and
// Session.Priced is false — the whole session renders "$—" rather than a
// partial number built only from its children, and contributes 0 to
// TotalCostUSD.
//
// Tier selection keys on s.ContextUsed, the last-request prompt size
// (internal/agent/claude/source.go sets it from the most recent assistant
// record's input+cache tokens; codex/parse.go from the last token_count's
// total_tokens, which folds in output and so overstates prompt size by a
// few percent — the right tier, not an exact one), never on s.Usage, which
// is the cumulative session total and would push every long-lived session
// into the long-context tier within a handful of turns regardless of how
// large any single request actually was.
//
// domain.Subagent carries no equivalent last-request figure, so each
// subagent prices at the base (untiered) rate — passing 0 falls through
// tieredRate's threshold check unconditionally. That undercharges a
// subagent whose own prompt genuinely crossed a tier threshold; revisit by
// adding a last-prompt-tokens field to Subagent in the child parser if that
// turns out to matter in practice.
func (st *State) priceSession(s *domain.Session, unpriced map[string]struct{}) {
	mainCost, mainOK := st.book.Cost(s.Model, s.Usage, s.ContextUsed)
	if !mainOK && s.Model != "" {
		unpriced[s.Model] = struct{}{}
	}

	total := mainCost
	for i := range s.Subagents {
		sa := &s.Subagents[i]
		cost, ok := st.book.Cost(sa.Model, sa.Usage, 0)
		if ok {
			c := cost
			sa.CostUSD = &c
			total += cost
		} else {
			sa.CostUSD = nil
			if sa.Model != "" {
				unpriced[sa.Model] = struct{}{}
			}
		}
	}

	s.Priced = mainOK
	if mainOK {
		c := total
		s.CostUSD = &c
	} else {
		s.CostUSD = nil
	}
}

// machineCPUPct is the machine-wide CPU history value: the cluster
// ActivePcts weighted by core count, since a plain mean would over-weight a
// small cluster (this box is 12 P-cores against 6 S-cores). Clusters with a
// nil ActivePct do not resolve and are excluded from both sums; an
// all-nil topology (or none reported) contributes 0.
func machineCPUPct(clusters []domain.Cluster) float64 {
	var weighted float64
	var cores int
	for _, c := range clusters {
		if c.ActivePct == nil {
			continue
		}
		weighted += *c.ActivePct * float64(c.CoreCount)
		cores += c.CoreCount
	}
	if cores == 0 {
		return 0
	}
	return weighted / float64(cores)
}

func derefOr(p *float64, fallback float64) float64 {
	if p == nil {
		return fallback
	}
	return *p
}
