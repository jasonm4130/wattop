//go:build tools

// Package tools pins build-time and test-time dependencies that no
// production code imports yet, so `go mod tidy` does not prune them before
// the tasks that use them land. This file is never compiled into the
// binary.
package tools

import (
	_ "charm.land/bubbles/v2"
	_ "charm.land/bubbletea/v2"
	_ "charm.land/lipgloss/v2"
	_ "github.com/BurntSushi/toml"
	_ "github.com/NimbleMarkets/ntcharts/v2/sparkline"
	_ "github.com/charmbracelet/colorprofile"
	_ "github.com/charmbracelet/x/exp/golden"
	_ "golang.org/x/term"
)
