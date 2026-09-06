// Package ui is wattop's Bubble Tea program: the Model in model.go, its
// keybindings in keys.go, and the widgets it composes from internal/ui/panel
// and internal/ui/theme.
package ui

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/state"
	"github.com/jasonm4130/wattop/internal/ui/panel"
	"github.com/jasonm4130/wattop/internal/ui/theme"
)

// CycleMsg carries one coordinated sampling cycle. Task 13's supervisor
// sends exactly one of these per cycle -- never three separate messages for
// Sys/Procs/Sessions -- so that Reduce always sees one instant's worth of
// data at once.
type CycleMsg struct {
	Inputs state.Inputs
}

// Model is wattop's Bubble Tea model. It holds a *state.State constructed
// by cmd/wattop (Task 13) and passed in here, never built by the model
// itself: the pricing book and burn tracker State wraps must outlive any
// single frame, and Update must never construct anything that does I/O.
type Model struct {
	st   *state.State
	snap *domain.Snapshot

	themeNames []string
	themeIdx   int
	roles      theme.Roles

	sortIdx        int
	filterHeadless bool
	// showAll disables the dormant-row filter (the `a` key). Off by
	// default: a machine that ran Codex earlier in the day carries a dozen
	// dead rollouts that would otherwise bury every live session.
	showAll    bool
	paused     bool
	showDetail bool
	showHelp   bool
	selected   int

	// noColor drops every ANSI escape from the rendered panels. cmd/wattop
	// (Task 13) owns the --no-color flag and NO_COLOR; internal/ui reads
	// neither, so the resolved answer arrives through WithNoColor.
	noColor bool

	width, height int
}

// New constructs a Model. st is owned by the caller for the life of the
// program; roles is the initially selected theme (resolved by cmd/wattop
// from --theme/WATTOP_THEME before this is called, since internal/ui does
// not read flags or the environment).
func New(st *state.State, themeName string, roles theme.Roles) Model {
	names := theme.Names()
	idx := 0
	for i, n := range names {
		if n == themeName {
			idx = i
			break
		}
	}
	return Model{
		st:         st,
		themeNames: names,
		themeIdx:   idx,
		roles:      roles,
		width:      120,
		height:     40,
	}
}

// WithNoColor returns m with colour suppression set. It is a setter rather
// than a New parameter so Task 13 can wire --no-color without every other
// caller of New changing shape.
func (m Model) WithNoColor(v bool) Model {
	m.noColor = v
	return m
}

// renderOpts is the one place the model's display flags become panel
// options, so a new flag reaches every panel by being added here once.
func (m Model) renderOpts() panel.Options {
	return panel.Options{NoColor: m.noColor}
}

// Init starts the program with no initial command: the sampling cycle that
// drives CycleMsg lives entirely in Task 13's supervisor, outside this
// model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update handles exactly one message per call and returns immediately.
// Nothing that blocks may run here: a CycleMsg triggers one Reduce call
// (pure, per internal/state's own contract) and a keypress only mutates
// the model's own display state.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case CycleMsg:
		if m.paused {
			return m, nil
		}
		m.snap = m.st.Reduce(msg.Inputs)
		m.clampSelection()
		return m, nil

	case tea.WindowSizeMsg:
		// The frame is drawn to whatever the terminal currently is, so a
		// resize re-lays out on the next View rather than corrupting the
		// old width's padding. New's 120x40 is only the pre-resize default.
		if msg.Width > 0 && msg.Height > 0 {
			m.width, m.height = msg.Width, msg.Height
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch keyAction(key) {
	case actionQuit:
		return m, tea.Quit
	case actionUp:
		if m.selected > 0 {
			m.selected--
		}
	case actionDown:
		if m.selected < m.flatRowCount()-1 {
			m.selected++
		}
	case actionToggleDetail:
		m.showDetail = !m.showDetail
	case actionThemeForward:
		m.cycleTheme(1)
	case actionThemeBack:
		m.cycleTheme(-1)
	case actionCycleSort:
		m.sortIdx = (m.sortIdx + 1) % len(sortKeys)
	case actionToggleFilter:
		m.filterHeadless = !m.filterHeadless
		m.clampSelection()
	case actionToggleShowAll:
		m.showAll = !m.showAll
		m.clampSelection()
	case actionTogglePause:
		m.paused = !m.paused
	case actionToggleHelp:
		m.showHelp = !m.showHelp
	}
	return m, nil
}

// themeName returns the active palette's name for the footer status line,
// or "—" when New was never given any themes to choose from (cycleTheme
// guards the same empty case).
func (m Model) themeName() string {
	if len(m.themeNames) == 0 {
		return "—"
	}
	return m.themeNames[m.themeIdx]
}

func (m *Model) cycleTheme(dir int) {
	if len(m.themeNames) == 0 {
		return
	}
	m.themeIdx = (m.themeIdx + dir + len(m.themeNames)) % len(m.themeNames)
	if r, err := theme.Load(m.themeNames[m.themeIdx]); err == nil {
		m.roles = r
	}
}

// dormant reports whether a session is both stale and bound to no live
// process — a rollout or session file left behind by something that has
// finished. Stale alone is not enough (a live process can idle past the
// threshold) and unbound alone is not enough (a recent rollout whose pid
// join failed is still worth showing); only the pair means nothing about
// the row can be current.
func dormant(s domain.Session) bool {
	return s.Status == "stale" && (s.PID == nil || s.BindConf == "unknown")
}

// visibleSessions applies the dormant-row filter, the current sort and the
// headless-child filter. filterHeadless hides subagent rows entirely (the
// child rows a headless Task invocation spawns) rather than the top-level
// sessions.
//
// It never mutates m.snap: the footer's machine totals, and --json, still
// count every session including the ones hidden here.
func (m Model) visibleSessions() []domain.Session {
	if m.snap == nil {
		return nil
	}
	out := make([]domain.Session, 0, len(m.snap.Sessions))
	for _, s := range m.snap.Sessions {
		if !m.showAll && dormant(s) {
			continue
		}
		out = append(out, s)
	}

	if m.filterHeadless {
		for i := range out {
			out[i].Subagents = nil
		}
	}

	// Liveness is the primary key under every sort: a dormant row (shown
	// only under `a`) never sits above a session that is running right
	// now, whatever its cost or CPU. The metric is the secondary key, so
	// the sort the user chose still orders the rows they came for.
	metric := func(i, j int) bool { return false }
	switch sortKeys[m.sortIdx] {
	case "cost":
		metric = func(i, j int) bool { return costOrZero(out[i]) > costOrZero(out[j]) }
	case "burn":
		metric = func(i, j int) bool { return burnOrZero(out[i]) > burnOrZero(out[j]) }
	case "cpu":
		metric = func(i, j int) bool { return cpuOrZero(out[i]) > cpuOrZero(out[j]) }
	case "status":
		// No numeric metric: "status" leaves each liveness group in
		// whatever order Reduce and the sources produced.
	}
	sort.SliceStable(out, func(i, j int) bool {
		if di, dj := dormant(out[i]), dormant(out[j]); di != dj {
			return !di
		}
		return metric(i, j)
	})
	return out
}

// hiddenSessions is how many top-level sessions the dormant filter is
// currently keeping off screen — the count the footer advertises, so a
// hidden row is never silently hidden.
func (m Model) hiddenSessions() int {
	if m.snap == nil || m.showAll {
		return 0
	}
	n := 0
	for _, s := range m.snap.Sessions {
		if dormant(s) {
			n++
		}
	}
	return n
}

func costOrZero(s domain.Session) float64 {
	if s.CostUSD == nil {
		return 0
	}
	return *s.CostUSD
}

func burnOrZero(s domain.Session) float64 {
	if s.BurnUSDPerHr == nil {
		return 0
	}
	return *s.BurnUSDPerHr
}

func cpuOrZero(s domain.Session) float64 {
	if s.Proc == nil {
		return 0
	}
	return s.Proc.CPUPct
}

// flatRowCount is how many selectable rows visibleSessions renders: one
// per session plus one per visible subagent.
func (m Model) flatRowCount() int {
	n := 0
	for _, s := range m.visibleSessions() {
		n += 1 + len(s.Subagents)
	}
	return n
}

func (m *Model) clampSelection() {
	if n := m.flatRowCount(); m.selected >= n {
		m.selected = n - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
}

// selectedSession returns the top-level session owning the currently
// selected flattened row (a subagent row's parent), or false when there is
// nothing to select.
func (m Model) selectedSession() (domain.Session, bool) {
	row := 0
	for _, s := range m.visibleSessions() {
		if row == m.selected {
			return s, true
		}
		row++
		for range s.Subagents {
			if row == m.selected {
				return s, true
			}
			row++
		}
	}
	return domain.Session{}, false
}

// View renders only from the stored Snapshot -- never a collector, never
// the clock -- and returns a tea.View per the v2 Model interface (v1's
// View() string no longer satisfies it).
func (m Model) View() tea.View {
	if m.snap == nil {
		return tea.NewView("wattop: waiting for the first sample...")
	}

	if m.showHelp {
		return tea.NewView(panel.HelpRender(m.roles, m.width, m.height, m.renderOpts()))
	}

	if m.showDetail {
		if s, ok := m.selectedSession(); ok {
			return tea.NewView(panel.DetailRender(s, m.roles, m.width, m.height, m.renderOpts()))
		}
	}

	// footerH, socH and tableH must always sum to exactly m.height: each
	// panel is framed independently to its own declared height, so if the
	// three didn't add up the frame would either fall short of the
	// terminal or overflow it. socHeight's fixed count (10 + len(Clusters))
	// is a want, not a guarantee -- on a terminal shorter than the footer
	// plus the SoC panel it is capped, and the footer itself shrinks
	// before anything is asked to render at a negative height.
	// 4, not 3: the footer now always carries a sort/theme/paused status
	// line on top of the machine-totals line, and the standing estimate
	// caveat must still fit alongside it whenever there is no unpriced or
	// Degraded line competing for the same budget (footer.go's own doc
	// comment: the caveat is permanent UI, never truncated away silently).
	footerH := min(4, max(0, m.height))
	socH := min(socHeight(m.snap.Sys), max(0, m.height-footerH))
	tableH := max(0, m.height-socH-footerH)

	soc := panel.Render(m.snap.Sys, m.roles, m.width, socH, m.renderOpts())
	table := panel.SessionsRender(m.visibleSessions(), m.roles, m.width, tableH, m.selected, m.snap.At, m.renderOpts())
	footer := panel.FooterRender(m.snap, m.roles, m.width, footerH, sortKeys[m.sortIdx], m.themeName(), m.paused, m.hiddenSessions(), m.renderOpts())

	// A section given 0 height still contributes an empty string, and
	// joining with a bare "+ \"\\n\" +" would insert a spurious blank line
	// for it (two adjacent separators around nothing). Skipping zero-height
	// sections keeps socH+tableH+footerH == the exact line count of the
	// joined frame.
	parts := make([]string, 0, 3)
	if socH > 0 {
		parts = append(parts, soc)
	}
	if tableH > 0 {
		parts = append(parts, table)
	}
	if footerH > 0 {
		parts = append(parts, footer)
	}
	return tea.NewView(strings.Join(parts, "\n"))
}

// socHeight is the number of lines panel.Render emits for sample: a border
// line, the SoC name, one line per cluster, then GPU, power, bandwidth,
// temps, fans, thermal, memory, and net/disk -- 10 fixed lines plus one per
// cluster. Computing it from the cluster count (rather than hardcoding,
// e.g. the 12 lines an M5 Max's two clusters produce) keeps a chip with a
// different cluster count from getting clipped or padded.
func socHeight(sample domain.SysSample) int {
	return 10 + len(sample.Clusters)
}
