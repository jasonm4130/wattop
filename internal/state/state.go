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

type trackedSession struct {
	session      domain.Session
	lastSeenAt   time.Time
	ttlClock     time.Time
	heldByOutage bool
}

type State struct {
	book *pricing.Book
	burn *pricing.BurnTracker

	tracked map[sessionKey]*trackedSession

	burnLastCost map[string]float64

	histories map[string]*ring
}

func New(book *pricing.Book, burn *pricing.BurnTracker) *State {
	return &State{
		book:         book,
		burn:         burn,
		tracked:      make(map[sessionKey]*trackedSession),
		burnLastCost: make(map[string]float64),
		histories:    make(map[string]*ring),
	}
}

func (st *State) History(key string) []float64 {
	r, ok := st.histories[key]
	if !ok {
		return nil
	}
	return r.values()
}
