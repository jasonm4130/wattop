package demo

import (
	"math"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// rates is token throughput in tokens/second while an entity is actively
// working, before the per-tick activity factor scales it.
type rates struct {
	in, out, cacheRead, cacheCreate float64
	// cachedFrac is, for Codex, the share of in that was served from cache
	// (Usage.CachedInput is a subset of Input there, not additive).
	cachedFrac float64
}

// track is one entity's cumulative token usage plus the short history
// TokenRate is computed from.
type track struct {
	in, out, cr, cc, cached float64
	hist                    []rateSnap // ring of cumulative snapshots, one per tick
	lastActive              int        // tick of the newest usage; negative is before tick 0
	ctx                     float64
	busyTicks               int
}

type rateSnap struct{ in, out, cr float64 }

func newTrack(base domain.Usage, winTicks, lastActive int, ctx float64) track {
	tr := track{
		in:         float64(base.Input),
		out:        float64(base.Output),
		cr:         float64(base.CacheRead),
		cc:         float64(base.CacheCreate5m),
		cached:     float64(base.CachedInput),
		lastActive: lastActive,
		ctx:        ctx,
		hist:       make([]rateSnap, 0, winTicks+1),
	}
	return tr
}

func (tr *track) accrue(r rates, factor, dt float64, tick int) {
	if factor <= 0 {
		return
	}
	in := r.in * factor * dt
	tr.in += in
	tr.cached += in * r.cachedFrac
	tr.out += r.out * factor * dt
	tr.cr += r.cacheRead * factor * dt
	tr.cc += r.cacheCreate * factor * dt
	tr.lastActive = tick
	tr.busyTicks++
}

// snapshot records this tick's cumulative totals, keeping winTicks+1 of
// them so rate() can difference across the full window.
func (tr *track) snapshot(winTicks int, codex bool) {
	s := rateSnap{in: tr.in + tr.cr + tr.cc, out: tr.out, cr: tr.cr}
	if codex {
		s = rateSnap{in: tr.in, out: tr.out, cr: tr.cached}
	}
	if len(tr.hist) == winTicks+1 {
		copy(tr.hist, tr.hist[1:])
		tr.hist = tr.hist[:winTicks]
	}
	tr.hist = append(tr.hist, s)
}

func (tr *track) usage() domain.Usage {
	return domain.Usage{
		Input:         int64(tr.in),
		Output:        int64(tr.out),
		CacheRead:     int64(tr.cr),
		CacheCreate5m: int64(tr.cc),
		CachedInput:   int64(tr.cached),
	}
}

// rate is recorded throughput over the ring's span, or nil before there
// are two snapshots to difference.
func (tr *track) rate(step time.Duration) *domain.TokenRate {
	if len(tr.hist) < 2 {
		return nil
	}
	first, last := tr.hist[0], tr.hist[len(tr.hist)-1]
	span := float64(len(tr.hist)-1) * step.Seconds()
	return &domain.TokenRate{
		InputPerSec:     round1((last.in - first.in) / span),
		OutputPerSec:    round1((last.out - first.out) / span),
		CacheReadPerSec: round1((last.cr - first.cr) / span),
	}
}

// Subagent lifecycles.
const (
	subRunning = iota // works the whole time
	subToggle         // alternates running and idle
	subDone           // finished before the demo began
)

type subSpec struct {
	id, parent, workflow, phase string
	agentType, desc, model      string
	background                  bool
	depth                       int
	mode                        int
	r                           rates
	base                        domain.Usage
	ctx                         float64
	startedMin                  float64 // minutes before tick 0
	idleSec                     float64 // for subDone: seconds before tick 0 it last worked
	tools                       []string
}

type subagent struct {
	spec        subSpec
	tr          track
	status      string
	toolCalls   int
	currentTool string
}

type workflowSpec struct {
	id, phase  string
	startedMin float64
}

type sessionSpec struct {
	agent, id, cwd, name, model, kind string
	comm                              string
	argv                              []string
	pid                               int
	busyFrac, segSec, phase           float64
	r                                 rates
	base                              domain.Usage
	ctxMax                            int64
	ctx, ctxGrowth                    float64
	exact                             bool
	rssMB                             float64
	ageMin                            float64
	tools                             []string
	baseTools                         int
	subs                              []subSpec
	workflow                          *workflowSpec
	rateLimits                        bool
}

type toolRec struct {
	name, id string
	tick     int
}

type session struct {
	idx         int
	spec        sessionSpec
	tr          track
	status      string
	statusSince int
	tools       []toolRec
	toolCounts  map[string]int
	subs        []*subagent
}

// maxTools caps the retained tool-call list so a demo left running all day
// stays bounded; the TL column tops out long before this.
const maxTools = 900

func newSession(w *World, idx int, spec sessionSpec) *session {
	s := &session{
		idx:        idx,
		spec:       spec,
		tr:         newTrack(spec.base, w.winTicks, -int(20*rnd(w.seed, chSessBase+400+uint64(idx), 0)), spec.ctx),
		toolCounts: make(map[string]int),
	}
	// Tool calls from before the demo began, spaced back from tick 0.
	for n := 0; n < spec.baseTools; n++ {
		name := spec.tools[int(rnd(w.seed, chToolBase+uint64(idx), int64(-n-1))*float64(len(spec.tools)))]
		s.tools = append(s.tools, toolRec{name: name, id: toolID("toolu", idx, n), tick: -(spec.baseTools - n) * 9})
		s.toolCounts[name]++
	}
	for i, ss := range spec.subs {
		lastActive := 0
		if ss.mode == subDone {
			lastActive = -int(ss.idleSec / w.step.Seconds())
		}
		sa := &subagent{spec: ss, tr: newTrack(ss.base, w.winTicks, lastActive, ss.ctx)}
		sa.toolCalls = 6 + int(14*rnd(w.seed, chToolBase+500+uint64(idx*16+i), 0))
		s.subs = append(s.subs, sa)
	}
	s.status = s.statusAt(w, 0)
	s.statusSince = -int((15 + 40*rnd(w.seed, chSessBase+600+uint64(idx), 0)) / w.step.Seconds())
	return s
}

// statusAt decides busy/waiting at simulated second t: the timeline is cut
// into segSec-long segments and each is busy with probability busyFrac.
func (s *session) statusAt(w *World, t float64) string {
	k := math.Floor((t + s.spec.phase) / s.spec.segSec)
	if rnd(w.seed, chSessBase+uint64(s.idx), int64(k)) < s.spec.busyFrac {
		return "busy"
	}
	return "waiting"
}

// activity scales a busy entity's token rates so the graphs have texture:
// a per-entity wobble (0.35x to 1.6x, with the occasional long-context
// burst) times a slow machine-wide swell, so the 60-second token-rate
// graphs show hills rather than a plateau.
func activity(w *World, ch uint64, t float64) float64 {
	a := 0.35 + 1.0*smooth(w.seed, ch, t, 3)
	if b := smooth(w.seed, ch+7777, t, 11); b > 0.75 {
		a += (b - 0.75) * 1.0
	}
	return a * (0.3 + 1.3*math.Pow(smooth(w.seed, chSwell, t, 18), 1.5))
}

func (s *session) advance(w *World, t, dt float64) {
	status := s.statusAt(w, t)
	if w.tick > 0 && status != s.status {
		s.statusSince = w.tick
	}
	s.status = status

	codex := s.spec.agent == "codex"
	if status == "busy" && w.tick > 0 {
		s.tr.accrue(s.spec.r, activity(w, chSessBase+700+uint64(s.idx), t), dt, w.tick)
		s.tr.ctx = math.Min(s.tr.ctx+s.spec.ctxGrowth*dt, 0.93*float64(s.spec.ctxMax))
		if !codex && rnd(w.seed, chToolBase+uint64(s.idx), int64(w.tick)) < 0.3*dt {
			name := s.spec.tools[int(rnd(w.seed, chToolBase+50+uint64(s.idx), int64(w.tick))*float64(len(s.spec.tools)))]
			s.tools = append(s.tools, toolRec{name: name, id: toolID("toolu", s.idx, len(s.tools)+s.spec.baseTools), tick: w.tick})
			s.toolCounts[name]++
			if len(s.tools) > maxTools {
				s.tools = s.tools[len(s.tools)-maxTools:]
			}
		}
	}
	s.tr.snapshot(w.winTicks, codex)

	for i, sa := range s.subs {
		ch := chSessBase + 800 + uint64(s.idx*16+i)
		switch sa.spec.mode {
		case subRunning:
			sa.status = domain.SubagentRunning
		case subToggle:
			k := math.Floor((t + 3*float64(i)) / 7)
			if rnd(w.seed, ch, int64(k)) < 0.62 {
				sa.status = domain.SubagentRunning
			} else {
				sa.status = domain.SubagentIdle
			}
		default:
			sa.status = domain.SubagentDone
		}
		sa.currentTool = ""
		if sa.status == domain.SubagentRunning {
			if w.tick > 0 {
				sa.tr.accrue(sa.spec.r, activity(w, ch+50, t), dt, w.tick)
				sa.tr.ctx += sa.spec.r.out * 3 * dt
				if rnd(w.seed, ch+100, int64(w.tick)) < 0.25*dt {
					sa.toolCalls++
				}
			}
			if len(sa.spec.tools) > 0 {
				k := int64(math.Floor(t / 4))
				sa.currentTool = sa.spec.tools[int(rnd(w.seed, ch+200, k)*float64(len(sa.spec.tools)))]
			}
		}
		sa.tr.snapshot(w.winTicks, codex)
	}
}

func (s *session) render(w *World, at time.Time) domain.Session {
	sp := s.spec
	pid := sp.pid
	out := domain.Session{
		TokenRate:    s.tr.rate(w.step),
		Agent:        sp.agent,
		ID:           sp.id,
		PID:          &pid,
		BindConf:     "exact",
		CWD:          sp.cwd,
		Name:         sp.name,
		Kind:         sp.kind,
		Status:       s.status,
		StatusSince:  w.ago(at, w.tick-s.statusSince),
		Model:        sp.model,
		Usage:        s.tr.usage(),
		ContextUsed:  int64(s.tr.ctx),
		ContextMax:   sp.ctxMax,
		ContextExact: sp.exact,
		LastUsageAt:  w.ago(at, w.tick-s.tr.lastActive),
	}
	if sp.agent == "codex" {
		out.Cmdline = joinArgs(sp.argv)
	}

	if len(s.tools) > 0 {
		out.Tools = make([]domain.ToolCall, 0, len(s.tools))
		for _, tc := range s.tools {
			out.Tools = append(out.Tools, domain.ToolCall{Name: tc.name, ID: tc.id, At: w.ago(at, w.tick-tc.tick)})
		}
		out.ToolCounts = make(map[string]int, len(s.toolCounts))
		for k, v := range s.toolCounts {
			out.ToolCounts[k] = v
		}
	}

	var wf *domain.Workflow
	if sp.workflow != nil {
		wf = &domain.Workflow{
			ID:        sp.workflow.id,
			Status:    domain.SubagentRunning,
			Phase:     sp.workflow.phase,
			StartedAt: w.ago(at, w.tick).Add(-time.Duration(sp.workflow.startedMin * float64(time.Minute))),
		}
	}

	for _, sa := range s.subs {
		ss := sa.spec
		child := domain.Subagent{
			TokenRate:      sa.tr.rate(w.step),
			ID:             ss.id,
			ParentID:       ss.parent,
			WorkflowID:     ss.workflow,
			Phase:          ss.phase,
			Hash:           ss.id,
			AgentType:      ss.agentType,
			Description:    ss.desc,
			Model:          ss.model,
			ToolUseID:      "toolu_demo_" + ss.id,
			SpawnDepth:     ss.depth,
			Background:     ss.background,
			Status:         sa.status,
			StartedAt:      w.ago(at, w.tick).Add(-time.Duration(ss.startedMin * float64(time.Minute))),
			LastActivityAt: w.ago(at, w.tick-sa.tr.lastActive),
			CurrentTool:    sa.currentTool,
			ToolCalls:      sa.toolCalls,
			ContextUsed:    int64(sa.tr.ctx),
			Usage:          sa.tr.usage(),
			Live:           sa.status == domain.SubagentRunning,
		}
		if child.LastActivityAt.After(out.LastUsageAt) {
			out.LastUsageAt = child.LastActivityAt
		}
		out.Subagents = append(out.Subagents, child)

		if wf != nil && ss.workflow == wf.ID {
			wf.Agents++
			switch sa.status {
			case domain.SubagentRunning:
				wf.Running++
			case domain.SubagentDone:
				wf.Done++
			}
			u := child.Usage
			wf.Usage.Input += u.Input
			wf.Usage.Output += u.Output
			wf.Usage.CacheRead += u.CacheRead
			wf.Usage.CacheCreate5m += u.CacheCreate5m
			if child.LastActivityAt.After(wf.LastActivityAt) {
				wf.LastActivityAt = child.LastActivityAt
			}
			if r := child.TokenRate; r != nil {
				if wf.TokenRate == nil {
					wf.TokenRate = &domain.TokenRate{}
				}
				wf.TokenRate.InputPerSec += r.InputPerSec
				wf.TokenRate.OutputPerSec += r.OutputPerSec
				wf.TokenRate.CacheReadPerSec += r.CacheReadPerSec
			}
		}
	}
	if wf != nil {
		out.Workflows = []domain.Workflow{*wf}
	}

	if sp.rateLimits {
		t := w.seconds(w.tick)
		primary := round1(23 + t/60)
		weekly := round1(41 + t/600)
		out.RateLimits = []domain.RateLimit{
			{Scope: "primary", UsedPct: &primary, WindowMins: 300, ResetsAt: w.ago(at, w.tick).Add(3*time.Hour + 12*time.Minute)},
			{Scope: "secondary", UsedPct: &weekly, WindowMins: 10080, ResetsAt: w.ago(at, w.tick).Add(4*24*time.Hour + 7*time.Hour)},
		}
	}
	return out
}

func joinArgs(argv []string) string {
	out := ""
	for i, a := range argv {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
