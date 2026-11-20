package state

import (
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
)

type SourceHealth struct {
	Name   string
	OK     bool
	Err    string
	LastOK time.Time
}

type Inputs struct {
	At       time.Time
	Sys      domain.SysSample
	Procs    []domain.ProcSample
	Sessions []domain.Session
	Health   []SourceHealth
}

const sessionTTL = 30 * time.Second

type sessionKey struct {
	Agent string
	ID    string
}

func (k sessionKey) burnKey() string {
	return k.Agent + ":" + k.ID
}

type Metric string

const (
	MetricTime      Metric = "time"
	MetricTokensIn  Metric = "tokens_in"
	MetricTokensOut Metric = "tokens_out"
	MetricCPU       Metric = "cpu"
	MetricGPU       Metric = "gpu"
	MetricWatts     Metric = "watts"
	MetricCost      Metric = "cost"
)

var sessionMetrics = []Metric{MetricCPU, MetricGPU, MetricCost}

type historyKey struct {
	session sessionKey
	metric  Metric
}

type ttlPhase int

const (
	ttlCounting ttlPhase = iota
	ttlHeldByOutage
)

type ttlClock struct {
	phase ttlPhase
	since time.Time
}

func (c *ttlClock) sighted(at time.Time) {
	c.countFrom(at)
}

func (c *ttlClock) countFrom(at time.Time) {
	*c = ttlClock{phase: ttlCounting, since: at}
}

func (c *ttlClock) holdForOutage() {
	*c = ttlClock{phase: ttlHeldByOutage}
}

func (c *ttlClock) expiredWhenAbsent(at time.Time) bool {
	if c.phase == ttlHeldByOutage {
		c.countFrom(at)
	}
	return at.Sub(c.since) > sessionTTL
}

type trackedSession struct {
	session domain.Session
	ttl     ttlClock
}

type State struct {
	book *pricing.Book
	burn *pricing.BurnTracker

	tracked map[sessionKey]*trackedSession

	histories map[historyKey]*ring
}

func New(book *pricing.Book, burn *pricing.BurnTracker) *State {
	return &State{
		book:      book,
		burn:      burn,
		tracked:   make(map[sessionKey]*trackedSession),
		histories: make(map[historyKey]*ring),
	}
}

func (st *State) History(m Metric) []float64 {
	return st.history(historyKey{metric: m})
}

func (st *State) history(key historyKey) []float64 {
	r, ok := st.histories[key]
	if !ok {
		return nil
	}
	return r.values()
}
