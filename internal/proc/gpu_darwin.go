//go:build darwin && arm64 && cgo

package proc

import "github.com/jasonm4130/wattop/internal/soc"

// gpuNsTable converts soc.GPUProcessStats's slice into the pid -> cumulative
// GPU-ns map that GPUTracker (delta.go) consumes. This is the only file in
// the package that imports internal/soc: delta.go stays free of any soc
// type so its arithmetic keeps compiling and testing on Linux.
func gpuNsTable() (map[int]uint64, error) {
	entries, err := soc.GPUProcessStats()
	if err != nil {
		return nil, err
	}
	out := make(map[int]uint64, len(entries))
	for _, e := range entries {
		out[e.PID] = e.CumulativeGPUNs
	}
	return out, nil
}
