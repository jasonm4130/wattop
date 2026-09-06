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
	// The import path is github.com/charmbracelet/colorprofile, NOT
	// charm.land/colorprofile as the v0.1 plan's dependency table guesses. The
	// charm.land path serves a module whose own go.mod declares
	// "module github.com/charmbracelet/colorprofile", so importing the vanity
	// path fails with a module-path mismatch. (charm.land/bubbletea/v2 and its
	// siblings above really do declare the charm.land path -- colorprofile is
	// the exception.) lipgloss/v2 v2.0.6 requires the github.com path, so this
	// is also the package whose Profile type the Task 11/12 golden tests must
	// set. No later task may edit go.mod, so use this path.
	_ "github.com/charmbracelet/colorprofile"
	_ "github.com/charmbracelet/x/exp/golden"
	_ "golang.org/x/term"
)
