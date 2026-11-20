package replay

import "time"

type Clock interface {
	Now() time.Time
	Advance(d time.Duration)
}

type VirtualClock struct {
	now time.Time
}

func NewVirtualClock(t time.Time) *VirtualClock {
	return &VirtualClock{now: t}
}

func (c *VirtualClock) Now() time.Time {
	return c.now
}

func (c *VirtualClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}
