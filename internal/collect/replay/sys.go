package replay

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

var knownOptionalChannels = []string{
	"ane_active",
	"dram_read_bw_gbs",
	"dram_write_bw_gbs",
	"dram_bw_combined_gbs",
	"ane_read_bw_gbs",
	"ane_write_bw_gbs",
	"ane_bw_combined_gbs",
	"fans",
}

// mactop's headless JSON writes these keys as a plain 0 both when the
// channel counted zero and when it never resolved, so a 0 is unresolved.
var zeroIsUnresolved = map[string]bool{
	"dram_read_bw_gbs":     true,
	"dram_write_bw_gbs":    true,
	"dram_bw_combined_gbs": true,
	"ane_read_bw_gbs":      true,
	"ane_write_bw_gbs":     true,
	"ane_bw_combined_gbs":  true,
}

type SysSampler struct {
	samples  []domain.SysSample
	channels []map[string]bool

	idx              int
	lastThermalState int
}

func NewSysSampler(dir string) (*SysSampler, error) {
	var paths []string
	for _, pattern := range []string{"*.jsonl", "*.json"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, fmt.Errorf("replay: globbing %s: %w", dir, err)
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("replay: no *.jsonl or *.json fixtures found in %s", dir)
	}

	s := &SysSampler{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("replay: reading %s: %w", path, err)
		}
		records, err := splitRecords(raw)
		if err != nil {
			return nil, fmt.Errorf("replay: framing %s: %w", path, err)
		}
		for _, rec := range records {
			sample, chans, err := decodeSysRecord(rec)
			if err != nil {
				return nil, fmt.Errorf("replay: decoding %s: %w", path, err)
			}
			s.samples = append(s.samples, sample)
			s.channels = append(s.channels, chans)
		}
	}
	if len(s.samples) == 0 {
		return nil, fmt.Errorf("replay: fixtures in %s decoded to zero records", dir)
	}
	return s, nil
}

func splitRecords(raw []byte) ([][]byte, error) {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty file")
	}

	switch trimmed[0] {
	case '{':
		var records [][]byte
		scanner := bufio.NewScanner(bytes.NewReader(raw))
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			cp := make([]byte, len(line))
			copy(cp, line)
			records = append(records, cp)
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		return records, nil
	case '[':
		var raws []json.RawMessage
		if err := json.Unmarshal(trimmed, &raws); err != nil {
			return nil, err
		}
		records := make([][]byte, len(raws))
		for i, r := range raws {
			records[i] = []byte(r)
		}
		return records, nil
	default:
		return nil, fmt.Errorf("unrecognised framing byte %q", trimmed[0])
	}
}

func getRaw(m map[string]json.RawMessage, key string) (json.RawMessage, bool) {
	v, ok := m[key]
	return v, ok
}

func getFloatPtr(m map[string]json.RawMessage, key string) *float64 {
	v, ok := m[key]
	if !ok {
		return nil
	}
	var f float64
	if err := json.Unmarshal(v, &f); err != nil {
		return nil
	}
	return &f
}

func getResolvedBW(m map[string]json.RawMessage, key string) *float64 {
	f := getFloatPtr(m, key)
	if f == nil || *f == 0 {
		return nil
	}
	return f
}

func getIntPtr(m map[string]json.RawMessage, key string) *int {
	f := getFloatPtr(m, key)
	if f == nil {
		return nil
	}
	i := int(*f)
	return &i
}

func getString(m map[string]json.RawMessage, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	_ = json.Unmarshal(v, &s)
	return s
}

var thermalStates = map[string]int{
	"Nominal":  0,
	"Moderate": 1,
	"Heavy":    2,
	"Trapping": 3,
	"Sleeping": 4,
}

func decodeSysRecord(rec []byte) (domain.SysSample, map[string]bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rec, &top); err != nil {
		return domain.SysSample{}, nil, err
	}

	var socMetrics map[string]json.RawMessage
	if raw, ok := top["soc_metrics"]; ok {
		if err := json.Unmarshal(raw, &socMetrics); err != nil {
			return domain.SysSample{}, nil, err
		}
	}

	channels := make(map[string]bool, len(knownOptionalChannels))
	var missing []string
	for _, key := range knownOptionalChannels {
		var present bool
		if key == "fans" {
			_, present = top[key]
		} else {
			_, present = socMetrics[key]
		}
		if present && zeroIsUnresolved[key] {
			present = getResolvedBW(socMetrics, key) != nil
		}
		channels[key] = present
		if !present {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)

	var sample domain.SysSample

	if raw, ok := top["timestamp"]; ok {
		var ts string
		_ = json.Unmarshal(raw, &ts)
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			sample.At = t
		}
	}

	var systemInfo map[string]json.RawMessage
	if raw, ok := top["system_info"]; ok {
		_ = json.Unmarshal(raw, &systemInfo)
	}
	sample.SoCName = getString(systemInfo, "name")

	sample.Clusters = buildClusters(systemInfo, socMetrics)

	sample.GPU.ActivePct = getFloatPtr(socMetrics, "gpu_active")
	sample.GPU.FreqMHz = getFloatPtr(socMetrics, "gpu_freq_mhz")
	if cc := getIntPtr(systemInfo, "gpu_core_count"); cc != nil {
		sample.GPU.CoreCount = *cc
	}

	sample.Power = domain.Power{
		CPUWatts:    getFloatPtr(socMetrics, "cpu_power"),
		GPUWatts:    getFloatPtr(socMetrics, "gpu_power"),
		ANEWatts:    getFloatPtr(socMetrics, "ane_power"),
		DRAMWatts:   getFloatPtr(socMetrics, "dram_power"),
		SystemWatts: getFloatPtr(socMetrics, "system_power"),
	}

	sample.Bandwidth = domain.Bandwidth{
		DRAMReadGBs:     getResolvedBW(socMetrics, "dram_read_bw_gbs"),
		DRAMWriteGBs:    getResolvedBW(socMetrics, "dram_write_bw_gbs"),
		DRAMCombinedGBs: getResolvedBW(socMetrics, "dram_bw_combined_gbs"),
		ANECombinedGBs:  getResolvedBW(socMetrics, "ane_bw_combined_gbs"),
	}

	sample.Temps = map[string]float64{}
	for domainKey, mactopKey := range map[string]string{
		"cpu": "cpu_temp",
		"gpu": "gpu_temp",
		"soc": "soc_temp",
	} {
		if f := getFloatPtr(socMetrics, mactopKey); f != nil {
			sample.Temps[domainKey] = *f
		}
	}

	if raws, ok := getRaw(top, "fans"); ok {
		var rawFans []struct {
			Name   string  `json:"name"`
			RPM    float64 `json:"rpm"`
			MinRPM float64 `json:"min_rpm"`
			MaxRPM float64 `json:"max_rpm"`
		}
		if err := json.Unmarshal(raws, &rawFans); err == nil {
			for _, rf := range rawFans {
				minRPM, maxRPM := rf.MinRPM, rf.MaxRPM
				sample.Fans = append(sample.Fans, domain.Fan{
					Label:  rf.Name,
					RPM:    rf.RPM,
					MinRPM: &minRPM,
					MaxRPM: &maxRPM,
				})
			}
		}
	}

	if ts := getString(top, "thermal_state"); ts != "" {
		if state, ok := thermalStates[ts]; ok {
			sample.ThermalState = state
			sample.Throttled = state >= 1
		} else {
			sample.ThermalState = -1
		}
	}

	var memory struct {
		Total     uint64 `json:"total"`
		Used      uint64 `json:"used"`
		Available uint64 `json:"available"`
		SwapTotal uint64 `json:"swap_total"`
		SwapUsed  uint64 `json:"swap_used"`
	}
	if raw, ok := top["memory"]; ok {
		_ = json.Unmarshal(raw, &memory)
	}
	sample.Memory = domain.MemorySample{
		TotalBytes:     memory.Total,
		UsedBytes:      memory.Used,
		AvailableBytes: memory.Available,
		SwapTotalBytes: memory.SwapTotal,
		SwapUsedBytes:  memory.SwapUsed,
	}

	var netDisk struct {
		InBytesPerSec     float64 `json:"in_bytes_per_sec"`
		OutBytesPerSec    float64 `json:"out_bytes_per_sec"`
		ReadKBytesPerSec  float64 `json:"read_kbytes_per_sec"`
		WriteKBytesPerSec float64 `json:"write_kbytes_per_sec"`
	}
	if raw, ok := top["net_disk"]; ok {
		_ = json.Unmarshal(raw, &netDisk)
	}
	sample.Net = domain.NetSample{
		InBytesPerSec:  netDisk.InBytesPerSec,
		OutBytesPerSec: netDisk.OutBytesPerSec,
	}
	sample.Disk = domain.DiskSample{
		ReadBytesPerSec:  netDisk.ReadKBytesPerSec * 1024,
		WriteBytesPerSec: netDisk.WriteKBytesPerSec * 1024,
	}

	sample.Missing = missing

	return sample, channels, nil
}

func buildClusters(systemInfo, socMetrics map[string]json.RawMessage) []domain.Cluster {
	var clusters []domain.Cluster
	for _, spec := range []struct {
		label    string
		countKey string
		activeK  string
		freqK    string
	}{
		{"E", "e_core_count", "e_cluster_active", "e_cluster_freq_mhz"},
		{"P", "p_core_count", "p_cluster_active", "p_cluster_freq_mhz"},
		{"S", "s_core_count", "s_cluster_active", "s_cluster_freq_mhz"},
	} {
		count := getIntPtr(systemInfo, spec.countKey)
		if count == nil || *count == 0 {
			continue
		}
		clusters = append(clusters, domain.Cluster{
			Label:     spec.label,
			CoreCount: *count,
			ActivePct: getFloatPtr(socMetrics, spec.activeK),
			FreqMHz:   getFloatPtr(socMetrics, spec.freqK),
		})
	}
	return clusters
}

func (s *SysSampler) Init() error { return nil }

func (s *SysSampler) Sample(ctx context.Context, intervalMs int) (domain.SysSample, error) {
	if err := ctx.Err(); err != nil {
		return domain.SysSample{}, err
	}
	sample := s.samples[s.idx%len(s.samples)]
	s.lastThermalState = sample.ThermalState
	s.idx++
	return sample, nil
}

func (s *SysSampler) ThermalState() int {
	return s.lastThermalState
}

func (s *SysSampler) Channels() map[string]bool {
	if len(s.channels) == 0 {
		return map[string]bool{}
	}
	idx := s.idx
	if idx > 0 {
		idx--
	}
	return s.channels[idx%len(s.channels)]
}

func (s *SysSampler) Close() error { return nil }
