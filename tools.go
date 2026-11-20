//go:build tools

package tools

import (
	_ "charm.land/bubbles/v2"
	_ "charm.land/bubbletea/v2"
	_ "charm.land/lipgloss/v2"
	_ "github.com/BurntSushi/toml"
	// Not charm.land/colorprofile: that vanity path serves a module whose go.mod
	// declares "module github.com/charmbracelet/colorprofile", so importing it
	// fails with a module-path mismatch.
	_ "github.com/charmbracelet/colorprofile"
	_ "github.com/charmbracelet/x/exp/golden"
	_ "golang.org/x/term"
)
