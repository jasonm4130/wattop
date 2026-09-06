package pricing

import (
	"sync"
	"time"
)

// sessionBurn is one session's burn-rate state: the last observed
// cumulative cost, the observation and transcript-event times that cost was
// read at, and an EWMA of the $/hr derivative between observations.
//
// baselined records that this session's cumulative cost has been seen at
// least once. The first sighting is a *baseline*: whatever it costs was
// spent before wattop could observe it — usually hours of transcript the
// tailers read in the first second or two — so it seeds lastUSD and
// contributes no rate at all.
type sessionBurn struct {
	baselined bool
	lastObs   time.Time // wall-clock time of the last observation
	lastEvent time.Time // transcript-record time of the newest usage in it (zero: unknown)
	lastUSD   float64
	ewma      float64
	hasEWMA   bool
}

// BurnTracker computes burn rate as the derivative of cumulative session
// cost over a sliding window of transcript deltas, smoothed with an EWMA.
// It is stateful, so it lives outside domain.Snapshot (domain never imports
// pricing) — cmd/wattop constructs one and hands it to state.New alongside
// a Book.
//
// Cost incurred before wattop started must never count toward $/hr. Two
// rules enforce that, and both are in Observe: the first observation of a
// session id is a baseline that contributes nothing, and a delta between
// two observations is attributed to the transcript timestamps of the usage
// that produced it wherever those are known, not to the wall-clock instant
// the tailer happened to read it. A delta whose newest usage is older than
// the tracker's window is discarded outright.
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

// Observe records one point of a session's cumulative cost: cumulativeUSD as
// read at wall-clock time at, where eventAt is the transcript timestamp of
// the newest usage record behind that total (zero when the source exposes
// none — Codex rollouts carry no usage timestamp into domain.Session).
//
// The first observation of a session id only baselines it. After that, a
// delta feeds the EWMA through whichever of five ordered cases it falls in:
//
//  1. exactly one of the two observations carries an event time — the span
//     the delta was generated over has no lower bound, so it is discarded;
//  2. neither carries one — fall back to the wall-clock span between
//     observations, which is all a Codex session can offer;
//  3. the newest usage is older than the window — the tokens were generated
//     before the window opened (a resume, or a backfilled chunk of history),
//     so the delta is discarded;
//  4. otherwise the delta is spread over the *longer* of the event span and
//     the wall-clock span, so a burst of same-instant usage records can
//     never manufacture a rate that neither clock supports;
//  5. a negative delta (a corrected or rewound total) is floored at zero
//     rather than reported as a negative burn.
//
// Every path advances lastUSD/lastObs/lastEvent, discards included: a
// discarded delta must not ride along into the next one and spike an
// interval later.
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

// span reports the duration a delta observed at (at, eventAt) should be
// spread over, and whether it counts toward the rate at all. See Observe's
// cases 1–4.
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
	if !ok || !sb.baselined {
		return 0
	}
	if at.Sub(sb.lastObs) > t.window {
		return 0
	}
	return sb.ewma
}
