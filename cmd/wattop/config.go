// config.go reads $XDG_CONFIG_HOME/wattop/config.toml. A missing file is not
// an error -- every field simply keeps Config's zero-value default, and
// callers apply their own fallback for anything left unset.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is wattop's on-disk configuration. Every field is optional --
// flags and environment variables take precedence over it (see the theme
// precedence and interval-flag handling in main.go), and a zero value here
// means "not configured" rather than an explicit choice.
type Config struct {
	Theme string `toml:"theme"`

	// IntervalMs is the SoC block duration in milliseconds, config.toml's
	// spelling of --interval. 0 means unset.
	IntervalMs int `toml:"interval_ms"`

	// BurnHotUSDPerHr is the $/hr burn rate rendered as "hot", a reserved
	// knob: no shipped panel reads it yet (the sessions/footer panels are
	// Task 12's files, out of this task's scope), but config.go's job is to
	// parse and expose it so a later task's rendering has it to consume
	// without a second config pass.
	BurnHotUSDPerHr float64 `toml:"burn_hot_usd_per_hr"`

	// CodexStaleMinutes is the mtime age, in minutes, past which a Codex
	// rollout with no other signal is considered stale. 0 means unset --
	// codex.DefaultIdleThreshold applies.
	CodexStaleMinutes int `toml:"codex_stale_minutes"`

	// ContextWindowOverrides maps a Claude session id or cwd to a context
	// window size in tokens, overriding the ladder estimate in
	// internal/agent/claude for that session. Passed straight into
	// claude.NewSource's overrides argument.
	ContextWindowOverrides map[string]int64 `toml:"context_window_overrides"`
}

// configFilePath returns $XDG_CONFIG_HOME/wattop/config.toml, falling back
// to $HOME/.config/wattop/config.toml when XDG_CONFIG_HOME is unset -- the
// same fallback shape internal/pricing's cacheFilePath uses for
// XDG_CACHE_HOME.
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

// loadConfig reads configFilePath(). A missing file returns a zero Config
// and no error; any other read or parse failure is returned so main can
// report it (config.toml exists and is broken is worth a message -- a
// config.toml that was never written is not).
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
