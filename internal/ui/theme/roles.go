package theme

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
