package replay

import "time"

// Clock is the time seam replay collectors and their tests run on, so a
// replay-driven test is deterministic and instant rather than sleeping on
// the wall clock.
type Clock interface {
	Now() time.Time
	Advance(d time.Duration)
}

// VirtualClock is a Clock that only moves when told to. The zero value is
// not usable; construct one with NewVirtualClock.
type VirtualClock struct {
	now time.Time
}

// NewVirtualClock returns a VirtualClock starting at t.
func NewVirtualClock(t time.Time) *VirtualClock {
	return &VirtualClock{now: t}
}

// Now returns the clock's current time.
func (c *VirtualClock) Now() time.Time {
	return c.now
}

// Advance moves the clock forward by d (d may be negative to move it back).
func (c *VirtualClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}
