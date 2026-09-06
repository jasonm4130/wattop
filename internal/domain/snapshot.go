package domain

import "time"

// SysSample is one point-in-time reading of the SoC. GPU is anonymous in the
// spec sketch — it stays that way here; it has no identity outside SysSample.
type SysSample struct {
	At       time.Time `json:"at"`
	SoCName  string    `json:"soc_name"`
	Clusters []Cluster `json:"clusters"`
	GPU      struct {
		ActivePct *float64 `json:"active_pct"`
		FreqMHz   *float64 `json:"freq_mhz"`
		CoreCount int      `json:"core_count"`
	} `json:"gpu"`
	Power        Power              `json:"power"`
	Bandwidth    Bandwidth          `json:"bandwidth"`
	Temps        map[string]float64 `json:"temps"`
	Fans         []Fan              `json:"fans"`
	ThermalState int                `json:"thermal_state"`
	Memory       MemorySample       `json:"memory"`
	Net          NetSample          `json:"net"`
	Disk         DiskSample         `json:"disk"`
	Missing      []string           `json:"missing"`
}

// Snapshot is the full state of one refresh tick — the headless JSON contract.
type Snapshot struct {
	TokenRate         *TokenRate `json:"token_rate,omitempty"`
	At                time.Time  `json:"at"`
	Sys               SysSample  `json:"sys"`
	Sessions          []Session  `json:"sessions"`
	TotalCostUSD      float64    `json:"total_cost_usd"`
	TotalBurnUSDPerHr float64    `json:"total_burn_usd_per_hr"`
	UnpricedModels    []string   `json:"unpriced_models"`
	Degraded          []string   `json:"degraded"`
	SelfCPUPct        float64    `json:"self_cpu_pct"`
}
