package state

// historyCap is the fixed ring capacity for every history ring (machine-wide
// and per-session alike): ~300 samples, enough for v0.2's sparklines on a
// shared x-axis without unbounded growth.
const historyCap = 300

// ring is a fixed-capacity FIFO of float64 samples. Pushing past capacity
// drops the oldest sample.
type ring struct {
	buf []float64
	cap int
}

func newRing(cap int) *ring {
	return &ring{buf: make([]float64, 0, cap), cap: cap}
}

func (r *ring) push(v float64) {
	r.buf = append(r.buf, v)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
}

// values returns a copy of the ring's contents, oldest first, so callers
// (and future goroutines reading through State.History) cannot mutate the
// ring's backing array.
func (r *ring) values() []float64 {
	out := make([]float64, len(r.buf))
	copy(out, r.buf)
	return out
}

// recordHistory pushes one sample onto the named ring, creating it at
// historyCap on first use.
func (st *State) recordHistory(key string, v float64) {
	r, ok := st.histories[key]
	if !ok {
		r = newRing(historyCap)
		st.histories[key] = r
	}
	r.push(v)
}
