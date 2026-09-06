package state

import (
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
)

// SourceHealth is one collector's liveness, produced by Task 13's
// supervisor and handed to Reduce every cycle. A source absent from a given
// Inputs.Health slice entirely has not started yet and is not degraded.
type SourceHealth struct {
	Name   string // "soc" | "proc" | "claude" | "codex" | "pricing"
	OK     bool
	Err    string    // non-empty only when !OK; what the badge says
	LastOK time.Time // zero if the source has never succeeded
}

// Inputs is one coordinated sampling cycle. Task 13 fills it and sends it as
// ONE message; At is that cycle's single timestamp for every field in it.
// Reduce copies At onto Snapshot.At and Snapshot.Sys.At, overwriting
// whatever the sampler wrote, so the sys and proc halves of a Snapshot
// always describe the same instant.
type Inputs struct {
	At       time.Time
	Sys      domain.SysSample
	Procs    []domain.ProcSample
	Sessions []domain.Session
	Health   []SourceHealth
}

// sessionTTL is how long a vanished session's row is retained (as "stale")
// after the cycle it stops appearing in Inputs.Sessions, before it is
// dropped entirely. It is a package constant, not a config key: Task 13's
// config already carries a Codex stale threshold and a second knob for
// this would be scope creep.
const sessionTTL = 30 * time.Second

// sessionKey identifies one session across the Claude and Codex id spaces,
// which are not guaranteed disjoint: Inputs.Sessions is the concatenation
// of both sources (Task 13, step 4).
type sessionKey struct {
	Agent string
	ID    string
}

// trackedSession is what State retains between cycles for one sessionKey.
//
// lastSeenAt is the At of the most recent cycle in which the source itself
// reported this session (a real sighting) — it is never advanced by the
// reducer's own stale bookkeeping.
//
// ttlClock is the working clock the sessionTTL expiry is measured against,
// and it advances in exactly three places:
//
//  1. A real sighting sets it to lastSeenAt (the ordinary case).
//  2. A cycle where the owning source's poll itself failed (Inputs.Health
//     says !OK for this session's agent) pushes it to that cycle's At, so
//     an outage never counts against the TTL. heldByOutage records that it
//     is now sitting on an outage-extended value rather than on lastSeenAt.
//  3. The *first* healthy cycle that omits a row held that way re-anchors
//     it to that cycle's At and clears heldByOutage, so the countdown to
//     dropping the row starts when a healthy poll actually omits it — not
//     at the last unhealthy cycle, and not at the last real sighting.
//
// Only case 3 consults heldByOutage: an ordinary vanish with no outage
// behind it keeps its anchor at lastSeenAt, so a session seen once and
// never again drops sessionTTL after that sighting rather than after the
// stale stamp.
type trackedSession struct {
	session      domain.Session
	lastSeenAt   time.Time
	ttlClock     time.Time
	heldByOutage bool
}

// State holds the single reducer's cross-cycle memory: the last-seen
// session map, the history rings, and the pricing book and burn tracker.
// All correlation in the project lives in this package and nowhere else —
// it is the only place that joins a session to its ProcSample by pid,
// prices a session through the pricing book, and decides whether a
// vanished session is stale, retained through a failed poll, or dropped.
// Constructed once in cmd/wattop.
type State struct {
	book *pricing.Book
	burn *pricing.BurnTracker

	tracked map[sessionKey]*trackedSession

	// burnLastCost is the cumulative cost most recently fed to burn.Observe
	// per burn key ("<agent>:<id>"). Reduce only calls Observe again when a
	// session's total actually changed (or on first sight) — an idle
	// session that keeps reporting the same total must not keep refreshing
	// the tracker's clock, or BurnTracker's window-expiry-to-zero path
	// never fires and the rate only ever decays asymptotically.
	burnLastCost map[string]float64

	histories map[string]*ring
}

// New constructs a State backed by book for pricing and burn for burn-rate
// smoothing. Both are held rather than threaded through Reduce because
// domain never imports pricing, so domain.Snapshot cannot own them.
func New(book *pricing.Book, burn *pricing.BurnTracker) *State {
	return &State{
		book:         book,
		burn:         burn,
		tracked:      make(map[sessionKey]*trackedSession),
		burnLastCost: make(map[string]float64),
		histories:    make(map[string]*ring),
	}
}

// History returns the current contents of one history ring, oldest sample
// first. "time" stores Unix seconds; "tokens_in" and "tokens_out" store
// recorded rates. Missing machine readings use NaN to leave graph gaps.
// key is "cpu" | "gpu" | "watts" | "cost" for the other machine-wide
// rings, populated from Sys.Clusters/GPU/Power.SystemWatts/the total burn
// rate, or "<agent>:<sessionID>:cpu" | "<agent>:<sessionID>:gpu" |
// "<agent>:<sessionID>:cost" for a per-session ring, populated from that
// session's Proc and burn rate. A key that has never been pushed to
// returns nil. Per-session keys carry the same (Agent, ID) pair as
// sessionKey so a Claude and a Codex session sharing an ID get distinct
// rings, and so Reduce can delete exactly the right three rings when a
// session's tracked entry is dropped (see Reduce's TTL-expiry branch).
func (st *State) History(key string) []float64 {
	r, ok := st.histories[key]
	if !ok {
		return nil
	}
	return r.values()
}
