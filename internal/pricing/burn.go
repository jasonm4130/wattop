package pricing

import (
	"sync"
	"time"
)

const (
	DefaultBurnWindow = 60 * time.Second
	DefaultBurnAlpha  = 0.3
)

type eventTime struct {
	at    time.Time
	known bool
}

func eventTimeOf(t time.Time) eventTime {
	return eventTime{at: t, known: !t.IsZero()}
}

type spanKind int

const (
	spanUncomparable spanKind = iota
	spanEventStale
	spanWall
	spanEvent
)

type burnSpan struct {
	kind    spanKind
	elapsed time.Duration
}

func (s burnSpan) counts() bool {
	return s.kind == spanWall || s.kind == spanEvent
}

type sessionBurn struct {
	baselined bool
	lastObs   time.Time
	lastEvent eventTime
	lastUSD   float64
	ewma      float64
	hasEWMA   bool
}

type BurnTracker struct {
	mu     sync.Mutex
	window time.Duration
	alpha  float64
	byID   map[string]*sessionBurn
}

func NewBurnTracker(window time.Duration, alpha float64) *BurnTracker {
	return &BurnTracker{
		window: window,
		alpha:  alpha,
		byID:   make(map[string]*sessionBurn),
	}
}

func (t *BurnTracker) Observe(sessionID string, cumulativeUSD float64, at, eventAt time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sb, ok := t.byID[sessionID]
	if !ok {
		sb = &sessionBurn{}
		t.byID[sessionID] = sb
	}

	event := eventTimeOf(eventAt)
	if sb.baselined {
		if s := t.span(sb, at, event); s.counts() && s.elapsed > 0 {
			inst := (cumulativeUSD - sb.lastUSD) / s.elapsed.Hours()
			if inst < 0 {
				inst = 0
			}
			if sb.hasEWMA {
				sb.ewma = t.alpha*inst + (1-t.alpha)*sb.ewma
			} else {
				sb.ewma = inst
				sb.hasEWMA = true
			}
		}
	}

	sb.baselined = true
	sb.lastObs = at
	sb.lastEvent = event
	sb.lastUSD = cumulativeUSD
}

func (t *BurnTracker) span(sb *sessionBurn, at time.Time, event eventTime) burnSpan {
	wall := at.Sub(sb.lastObs)
	switch {
	case event.known != sb.lastEvent.known:
		return burnSpan{kind: spanUncomparable}
	case !event.known:
		return burnSpan{kind: spanWall, elapsed: wall}
	case at.Sub(event.at) > t.window:
		return burnSpan{kind: spanEventStale}
	}
	if gap := event.at.Sub(sb.lastEvent.at); gap > wall {
		return burnSpan{kind: spanEvent, elapsed: gap}
	}
	return burnSpan{kind: spanWall, elapsed: wall}
}

func (t *BurnTracker) Forget(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byID, sessionID)
}

func (t *BurnTracker) RatePerHour(sessionID string, at time.Time) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	sb, ok := t.byID[sessionID]
	if !ok || !sb.baselined {
		return 0
	}
	if at.Sub(sb.lastObs) > t.window {
		return 0
	}
	return sb.ewma
}
