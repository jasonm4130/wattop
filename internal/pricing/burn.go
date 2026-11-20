package pricing

import (
	"sync"
	"time"
)

type sessionBurn struct {
	baselined bool
	lastObs   time.Time
	lastEvent time.Time
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

	if sb.baselined {
		if dt, counts := t.span(sb, at, eventAt); counts && dt > 0 {
			inst := (cumulativeUSD - sb.lastUSD) / dt.Hours()
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
	sb.lastEvent = eventAt
	sb.lastUSD = cumulativeUSD
}

func (t *BurnTracker) span(sb *sessionBurn, at, eventAt time.Time) (time.Duration, bool) {
	eventKnown, lastKnown := !eventAt.IsZero(), !sb.lastEvent.IsZero()

	switch {
	case eventKnown != lastKnown:
		return 0, false
	case !eventKnown:
		return at.Sub(sb.lastObs), true
	case at.Sub(eventAt) > t.window:
		return 0, false
	default:
		wall := at.Sub(sb.lastObs)
		if ev := eventAt.Sub(sb.lastEvent); ev > wall {
			return ev, true
		}
		return wall, true
	}
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
