package state

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

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

		if healthFailed[key.Agent] {
			tr.ttlClock = in.At
			tr.heldByOutage = true
			enriched := tr.session
			clearTokenRates(&enriched)
			st.enrichSession(&enriched, procByPID, unpriced, in.At)
			sessions = append(sessions, enriched)
			continue
		}

		if tr.heldByOutage {
			tr.ttlClock = in.At
			tr.heldByOutage = false
		}

		if in.At.Sub(tr.ttlClock) > sessionTTL {
			delete(st.tracked, key)
			st.burn.Forget(key.Agent + ":" + key.ID)
			delete(st.burnLastCost, key.Agent+":"+key.ID)
			st.forgetSubagentBurn(key.Agent + ":" + key.ID)
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
		clearTokenRates(&enriched)
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
		TotalCostPartial:  len(unpricedList) > 0,
		UnpricedModels:    unpricedList,
		Degraded:          degraded,
		SelfCPUPct:        selfCPU,
	}
	for _, s := range sessions {
		addTokenRate(&snap.TokenRate, s.TokenRate)
		for _, child := range s.Subagents {
			addTokenRate(&snap.TokenRate, child.TokenRate)
		}
	}
	input, output := math.NaN(), math.NaN()
	if snap.TokenRate != nil {
		input, output = snap.TokenRate.InputPerSec, snap.TokenRate.OutputPerSec
	}
	st.recordHistory("tokens_in", input)
	st.recordHistory("tokens_out", output)
	st.recordHistory("time", float64(in.At.UnixMilli())/1000)

	cpu, gpu, watts := machineCPUPct(sys.Clusters), derefOr(sys.GPU.ActivePct, math.NaN()), derefOr(sys.Power.SystemWatts, math.NaN())
	if healthFailed["soc"] {
		cpu, gpu, watts = math.NaN(), math.NaN(), math.NaN()
	}
	st.recordHistory("cpu", cpu)
	st.recordHistory("gpu", gpu)
	st.recordHistory("watts", watts)
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

func clearTokenRates(s *domain.Session) {
	s.TokenRate = nil
	s.Subagents = append([]domain.Subagent(nil), s.Subagents...)
	for i := range s.Subagents {
		s.Subagents[i].TokenRate = nil
	}
}

func addTokenRate(total **domain.TokenRate, rate *domain.TokenRate) {
	if rate == nil {
		return
	}
	if *total == nil {
		*total = &domain.TokenRate{}
	}
	(*total).InputPerSec += rate.InputPerSec
	(*total).OutputPerSec += rate.OutputPerSec
	(*total).CacheReadPerSec += rate.CacheReadPerSec
}

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
	st.burnSubagents(s, burnKey, at)
	summarizeWorkflows(s)
	if s.CostUSD == nil {
		s.BurnUSDPerHr = nil
		return
	}
	if last, ok := st.burnLastCost[burnKey]; !ok || last != *s.CostUSD {
		st.burn.Observe(burnKey, *s.CostUSD, at, lastTranscriptAt(s))
		st.burnLastCost[burnKey] = *s.CostUSD
	}
	rate := st.burn.RatePerHour(burnKey, at)
	s.BurnUSDPerHr = &rate
}

const childSeedWindow = 2 * time.Minute

const childSeedMinSpan = 30 * time.Second

func subagentKey(sa domain.Subagent, i int) string {
	switch {
	case sa.ID != "":
		return sa.ID
	case sa.Hash != "":
		return "hash:" + sa.Hash
	default:
		return fmt.Sprintf("#%d", i)
	}
}

func subagentBurnKey(sessionKey, subID string) string {
	return sessionKey + ":sub:" + subID
}

func (st *State) burnSubagents(s *domain.Session, sessionBurnKey string, at time.Time) {
	for i := range s.Subagents {
		sa := &s.Subagents[i]
		if sa.CostUSD == nil || sa.LastActivityAt.IsZero() {
			sa.BurnUSDPerHr = nil
			continue
		}
		key := subagentBurnKey(sessionBurnKey, subagentKey(*sa, i))
		last, seen := st.burnLastCost[key]
		if !seen && !sa.StartedAt.IsZero() && !sa.LastActivityAt.IsZero() && at.Sub(sa.StartedAt) <= childSeedWindow {
			seedAt := sa.StartedAt
			if floor := sa.LastActivityAt.Add(-childSeedMinSpan); floor.Before(seedAt) {
				seedAt = floor
			}
			st.burn.Observe(key, 0, at, seedAt)
			seen, last = true, 0
		}
		if !seen || last != *sa.CostUSD {
			st.burn.Observe(key, *sa.CostUSD, at, sa.LastActivityAt)
			st.burnLastCost[key] = *sa.CostUSD
		}
		rate := st.burn.RatePerHour(key, at)
		sa.BurnUSDPerHr = &rate
	}
}

func (st *State) forgetSubagentBurn(sessionBurnKey string) {
	prefix := sessionBurnKey + ":sub:"
	for key := range st.burnLastCost {
		if strings.HasPrefix(key, prefix) {
			st.burn.Forget(key)
			delete(st.burnLastCost, key)
		}
	}
}

func summarizeWorkflows(s *domain.Session) {
	if len(s.Workflows) == 0 {
		return
	}
	s.Workflows = append([]domain.Workflow(nil), s.Workflows...)
	index := make(map[string]int, len(s.Workflows))
	for i := range s.Workflows {
		wf := &s.Workflows[i]
		wf.CostUSD, wf.BurnUSDPerHr, wf.CostPartial = nil, nil, false
		index[wf.ID] = i
	}
	for _, sa := range s.Subagents {
		i, ok := index[sa.WorkflowID]
		if !ok || sa.WorkflowID == "" {
			continue
		}
		wf := &s.Workflows[i]
		if sa.CostUSD == nil {
			if childBilled(sa) {
				wf.CostPartial = true
			}
			continue
		}
		if sa.CostPartial {
			wf.CostPartial = true
		}
		wf.CostUSD = addPtr(wf.CostUSD, *sa.CostUSD)
		if sa.BurnUSDPerHr != nil {
			wf.BurnUSDPerHr = addPtr(wf.BurnUSDPerHr, *sa.BurnUSDPerHr)
		}
	}
	for i := range s.Workflows {
		if s.Workflows[i].CostUSD == nil {
			s.Workflows[i].CostPartial = false
		}
	}
}

func addPtr(p *float64, v float64) *float64 {
	sum := v
	if p != nil {
		sum += *p
	}
	return &sum
}

func childBilled(sa domain.Subagent) bool {
	u := sa.Usage
	return sa.Model != "" || u.Input+u.Output+u.CacheRead+u.CacheCreate5m+u.CacheCreate1h > 0
}

func lastTranscriptAt(s *domain.Session) time.Time {
	newest := s.LastUsageAt
	for _, tc := range s.Tools {
		if tc.At.After(newest) {
			newest = tc.At
		}
	}
	return newest
}

func (st *State) priceSession(s *domain.Session, unpriced map[string]struct{}) {
	mainCost, mainOK, partial := st.priceUsage(s.Model, s.Usage, s.ContextUsed, s.Ledger, unpriced)

	total := mainCost
	s.Subagents = append([]domain.Subagent(nil), s.Subagents...)
	for i := range s.Subagents {
		sa := &s.Subagents[i]
		cost, ok, saPartial := st.priceUsage(sa.Model, sa.Usage, sa.ContextUsed, sa.Ledger, unpriced)
		sa.CostPartial = ok && saPartial
		if ok {
			c := cost
			sa.CostUSD = &c
			total += cost
			if saPartial {
				partial = true
			}
		} else {
			sa.CostUSD = nil
			if childBilled(*sa) {
				partial = true
			}
		}
	}

	s.Priced = mainOK
	if mainOK {
		c := total
		s.CostUSD = &c
		s.CostPartial = partial
	} else {
		s.CostUSD = nil
		s.CostPartial = false
	}
}

func (st *State) priceUsage(model string, u domain.Usage, contextUsed int64, l domain.UsageLedger, unpriced map[string]struct{}) (cost float64, ok, partial bool) {
	if len(l) > 0 {
		usd, missing, anyPriced := st.book.CostLedger(l)
		for _, m := range missing {
			if m != "" {
				unpriced[m] = struct{}{}
			}
		}
		return usd, anyPriced, anyPriced && len(missing) > 0
	}
	usd, priced := st.book.Cost(model, u, contextUsed)
	if !priced && model != "" {
		unpriced[model] = struct{}{}
	}
	return usd, priced, false
}

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
		return math.NaN()
	}
	return weighted / float64(cores)
}

func derefOr(p *float64, fallback float64) float64 {
	if p == nil {
		return fallback
	}
	return *p
}
