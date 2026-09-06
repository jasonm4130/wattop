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

// knownOptionalChannels are the mactop soc_metrics keys (and the one
// top-level key, "fans") that are known to disappear on a version-drifted
// or degraded capture. Presence of each is tracked per decoded record for
// both SysSample.Missing and Channels() ("doctor").
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

// SysSampler implements domain.Sampler by replaying mactop headless JSON
// captures committed under testdata/soc/. It reads every *.jsonl and *.json
// file in dir, peeking each file's first non-whitespace byte to choose NDJSON
// ('{') or JSON-array ('[') framing, and walks the concatenated sequence of
// decoded records, looping once exhausted.
type SysSampler struct {
	samples  []domain.SysSample
	channels []map[string]bool

	idx              int
	lastThermalState int
}

// NewSysSampler builds a SysSampler over every recorded fixture in dir
// (typically filepath.Join(fixture.CorpusDir(), "soc")).
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

// splitRecords peeks the first non-whitespace byte of raw to choose framing:
// '{' means NDJSON (one object per non-empty line), '[' means a JSON array.
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
	"Fair":     1,
	"Serious":  2,
	"Critical": 3,
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
		DRAMReadGBs:    getFloatPtr(socMetrics, "dram_read_bw_gbs"),
		DRAMWriteGBs:   getFloatPtr(socMetrics, "dram_write_bw_gbs"),
		ANECombinedGBs: getFloatPtr(socMetrics, "ane_bw_combined_gbs"),
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

// buildClusters iterates whatever cluster core counts system_info actually
// reports rather than hardcoding a P/E pair — this machine has no E cores
// (system_info carries no e_core_count key), so no E cluster is emitted.
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

// Init satisfies domain.Sampler; replay has nothing to initialise.
func (s *SysSampler) Init() error { return nil }

// Sample returns the next recorded SysSample, looping once the sequence is
// exhausted. intervalMs is accepted for interface compatibility and ignored.
func (s *SysSampler) Sample(ctx context.Context, intervalMs int) (domain.SysSample, error) {
	if err := ctx.Err(); err != nil {
		return domain.SysSample{}, err
	}
	sample := s.samples[s.idx%len(s.samples)]
	chans := s.channels[s.idx%len(s.channels)]
	s.lastThermalState = sample.ThermalState
	s.idx++
	_ = chans
	return sample, nil
}

// ThermalState returns the thermal state from the most recent Sample call.
func (s *SysSampler) ThermalState() int {
	return s.lastThermalState
}

// Channels reports which known optional channels were present in the most
// recently sampled record.
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

// Close satisfies domain.Sampler; replay holds no resources to release.
func (s *SysSampler) Close() error { return nil }
