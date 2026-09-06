package pricing

import (
	"sync"
	"time"
)

// sessionBurn is one session's burn-rate state: the last observed
// cumulative cost and an EWMA of the $/hr derivative between observations.
type sessionBurn struct {
	lastAt  time.Time
	lastUSD float64
	ewma    float64
	hasEWMA bool
}

// BurnTracker computes burn rate as the derivative of cumulative session
// cost over a sliding window of transcript deltas, smoothed with an EWMA.
// It is stateful, so it lives outside domain.Snapshot (domain never imports
// pricing) — cmd/wattop constructs one and hands it to state.New alongside
// a Book.
type BurnTracker struct {
	mu     sync.Mutex
	window time.Duration
	alpha  float64
	byID   map[string]*sessionBurn
}

// NewBurnTracker returns a tracker whose sliding window is window and whose
// EWMA smoothing factor is alpha (spec default α≈0.3: higher weights recent
// deltas more).
func NewBurnTracker(window time.Duration, alpha float64) *BurnTracker {
	return &BurnTracker{
		window: window,
		alpha:  alpha,
		byID:   make(map[string]*sessionBurn),
	}
}

// Observe records one point of a session's cumulative cost at time at. The
// instantaneous rate since the previous observation feeds the EWMA; a
// negative delta (a corrected/rewound total) is floored at zero rather than
// reported as a negative burn.
func (t *BurnTracker) Observe(sessionID string, cumulativeUSD float64, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sb, ok := t.byID[sessionID]
	if !ok {
		sb = &sessionBurn{}
		t.byID[sessionID] = sb
	}

	if !sb.lastAt.IsZero() {
		dtHours := at.Sub(sb.lastAt).Hours()
		if dtHours > 0 {
			inst := (cumulativeUSD - sb.lastUSD) / dtHours
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

	sb.lastAt = at
	sb.lastUSD = cumulativeUSD
}

// Forget removes a session's tracked state entirely. Callers should invoke
// this when a session is dropped from the domain snapshot (e.g. its TTL
// expires) so byID does not grow without bound for the life of the process.
func (t *BurnTracker) Forget(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byID, sessionID)
}

// RatePerHour reports the smoothed $/hr burn rate for a session as of at. A
// session with no observation inside the window (it has gone quiet) reports
// $0.00/hr, never the last nonzero value.
func (t *BurnTracker) RatePerHour(sessionID string, at time.Time) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	sb, ok := t.byID[sessionID]
	if !ok || sb.lastAt.IsZero() {
		return 0
	}
	if at.Sub(sb.lastAt) > t.window {
		return 0
	}
	return sb.ewma
}
