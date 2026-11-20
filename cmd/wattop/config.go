package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Theme string `toml:"theme"`

	IntervalMs int `toml:"interval_ms"`

	BurnHotUSDPerHr float64 `toml:"burn_hot_usd_per_hr"`

	CodexStaleMinutes int `toml:"codex_stale_minutes"`

	ContextWindowOverrides map[string]int64 `toml:"context_window_overrides"`
}

func configFilePath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".config")
		} else {
			base = "."
		}
	}
	return filepath.Join(base, "wattop", "config.toml")
}

func loadConfig() (Config, error) {
	path := configFilePath()
	var cfg Config
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: stat %s: %w", path, err)
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return cfg, nil
}
