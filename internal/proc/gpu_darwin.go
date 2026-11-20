//go:build darwin && arm64 && cgo

package proc

import "github.com/jasonm4130/wattop/internal/soc"

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
