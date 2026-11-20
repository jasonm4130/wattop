package state

const historyCap = 300

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

func (r *ring) values() []float64 {
	out := make([]float64, len(r.buf))
	copy(out, r.buf)
	return out
}

func (st *State) recordHistory(key historyKey, v float64) {
	r, ok := st.histories[key]
	if !ok {
		r = newRing(historyCap)
		st.histories[key] = r
	}
	r.push(v)
}
