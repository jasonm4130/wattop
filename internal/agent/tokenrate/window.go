// Package tokenrate aggregates timestamped transcript usage into a rolling
// minute. It measures recorded throughput, not model decoding speed.
package tokenrate

import (
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// Window retains at most 61 one-second buckets, even while reading old logs.
type Window struct {
	buckets map[int64]domain.TokenRate
	latest  int64
	known   bool
}

func (w *Window) Add(at time.Time, input, output, cached int64) {
	if at.IsZero() {
		return
	}
	sec := at.Unix()
	if !w.known || sec > w.latest {
		w.latest = sec
	}
	w.known = true
	if w.buckets == nil {
		w.buckets = make(map[int64]domain.TokenRate)
	}
	for t := range w.buckets {
		if t < w.latest-60 {
			delete(w.buckets, t)
		}
	}
	if sec < w.latest-60 {
		return
	}
	b := w.buckets[sec]
	b.InputPerSec += float64(input)
	b.OutputPerSec += float64(output)
	b.CacheReadPerSec += float64(cached)
	w.buckets[sec] = b
}

func (w *Window) Rate(now time.Time) *domain.TokenRate {
	if !w.known {
		return nil
	}
	var r domain.TokenRate
	for sec, b := range w.buckets {
		if sec > now.Unix()-60 && sec <= now.Unix() {
			r.InputPerSec += b.InputPerSec / 60
			r.OutputPerSec += b.OutputPerSec / 60
			r.CacheReadPerSec += b.CacheReadPerSec / 60
		}
	}
	r.InputPerSec = max(0, r.InputPerSec)
	r.OutputPerSec = max(0, r.OutputPerSec)
	r.CacheReadPerSec = max(0, r.CacheReadPerSec)
	return &r
}
