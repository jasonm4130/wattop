package demo

import "math"

// splitmix64 is the finaliser from Vigna's SplitMix64: a cheap, well-mixed
// 64-bit hash. Every "random" number the demo produces is this function of
// (seed, channel, index), never a draw from a stateful generator, so a
// value depends only on the seed and on which tick asked for it.
func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// rnd returns a uniform value in [0, 1) for (seed, ch, k).
func rnd(seed, ch uint64, k int64) float64 {
	h := splitmix64(seed ^ splitmix64(ch) ^ splitmix64(uint64(k)*0x2545f4914f6cdd1d))
	return float64(h>>11) / float64(1<<53)
}

// smooth is 1-D value noise in [0, 1]: rnd at integer multiples of period,
// cosine-interpolated between them, so a channel wanders instead of
// jumping.
func smooth(seed, ch uint64, t, period float64) float64 {
	x := t / period
	k := math.Floor(x)
	f := x - k
	a := rnd(seed, ch, int64(k))
	b := rnd(seed, ch, int64(k)+1)
	u := (1 - math.Cos(math.Pi*f)) / 2
	return a + (b-a)*u
}

func clamp(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, v))
}

// channel names a noise stream. Distinct constants keep, say, the GPU's
// wander from being the CPU's wander shifted.
const (
	chCPU uint64 = iota + 1
	chCPUBurst
	chECluster
	chGPU
	chGPUBurst
	chANE
	chMem
	chNetIn
	chNetOut
	chDiskR
	chDiskW
	chSwell
	chCoreBase = 1000
	chSessBase = 2000
	chToolBase = 3000
)
