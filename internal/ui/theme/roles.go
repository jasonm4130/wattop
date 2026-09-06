package theme

// Palette is the on-disk shape of a theme file. It mirrors mactop's own
// convention ({"foreground": "#...", "background": "#..."}) so a user who
// already themed mactop can paste the same hexes across, extended with
// wattop's semantic role keys below.
type Palette struct {
	Foreground string `json:"foreground"`
	Background string `json:"background"`
	Accent     string `json:"accent"`
	Muted      string `json:"muted"`
	Border     string `json:"border"`
	Idle       string `json:"idle"`
	Busy       string `json:"busy"`
	Waiting    string `json:"waiting"`
	Warn       string `json:"warn"`
	Hot        string `json:"hot"`
	Error      string `json:"error"`
	CostHot    string `json:"cost_hot"`
	GaugeLow   string `json:"gauge_low"`
	GaugeMid   string `json:"gauge_mid"`
	GaugeHigh  string `json:"gauge_high"`
	BarTrack   string `json:"bar_track"`
	BarFill    string `json:"bar_fill"`
	ChartCPU   string `json:"chart_cpu"`
	ChartGPU   string `json:"chart_gpu"`
	ChartWatts string `json:"chart_watts"`
	ChartCost  string `json:"chart_cost"`
}

// Roles is the semantic palette every widget renders from. No widget ever
// reads a Palette field directly -- ToRoles is the one place a palette's raw
// hex values become meaning, which is what keeps four palettes from becoming
// four rendering bugs, and lets a light palette swap its two chart ramps
// rather than rendering pale-on-pale.
type Roles struct {
	Foreground string
	Accent     string
	Muted      string
	Border     string

	Idle    string
	Busy    string
	Waiting string
	Warn    string
	Hot     string
	Error   string

	CostHot string

	GaugeLow  string
	GaugeMid  string
	GaugeHigh string

	BarTrack string
	BarFill  string

	ChartCPU   string
	ChartGPU   string
	ChartWatts string
	ChartCost  string
}

// ToRoles maps a loaded Palette into its semantic Roles.
func (p Palette) ToRoles() Roles {
	return Roles{
		Foreground: p.Foreground,
		Accent:     p.Accent,
		Muted:      p.Muted,
		Border:     p.Border,

		Idle:    p.Idle,
		Busy:    p.Busy,
		Waiting: p.Waiting,
		Warn:    p.Warn,
		Hot:     p.Hot,
		Error:   p.Error,

		CostHot: p.CostHot,

		GaugeLow:  p.GaugeLow,
		GaugeMid:  p.GaugeMid,
		GaugeHigh: p.GaugeHigh,

		BarTrack: p.BarTrack,
		BarFill:  p.BarFill,

		ChartCPU:   p.ChartCPU,
		ChartGPU:   p.ChartGPU,
		ChartWatts: p.ChartWatts,
		ChartCost:  p.ChartCost,
	}
}

// Fields reports every Roles field by name, for validation (every field must
// be a non-empty hex string) without reflection at call sites.
func (r Roles) Fields() map[string]string {
	return map[string]string{
		"Foreground": r.Foreground,
		"Accent":     r.Accent,
		"Muted":      r.Muted,
		"Border":     r.Border,

		"Idle":    r.Idle,
		"Busy":    r.Busy,
		"Waiting": r.Waiting,
		"Warn":    r.Warn,
		"Hot":     r.Hot,
		"Error":   r.Error,

		"CostHot": r.CostHot,

		"GaugeLow":  r.GaugeLow,
		"GaugeMid":  r.GaugeMid,
		"GaugeHigh": r.GaugeHigh,

		"BarTrack": r.BarTrack,
		"BarFill":  r.BarFill,

		"ChartCPU":   r.ChartCPU,
		"ChartGPU":   r.ChartGPU,
		"ChartWatts": r.ChartWatts,
		"ChartCost":  r.ChartCost,
	}
}

// Severity picks GaugeLow/GaugeMid/GaugeHigh for pct (0-100) against the
// given warn/hot thresholds -- e.g. context-fill at 70/90.
func (r Roles) Severity(pct, warnAt, hotAt float64) string {
	switch {
	case pct >= hotAt:
		return r.GaugeHigh
	case pct >= warnAt:
		return r.GaugeMid
	default:
		return r.GaugeLow
	}
}
