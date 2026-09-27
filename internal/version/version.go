// Package version exposes build-time version information, settable via
// -ldflags at build time, falling back to the Go toolchain's embedded build
// info, and defaulting to placeholder values otherwise.
package version

import (
	"fmt"
	"runtime/debug"
)

// Version, Commit and BuildDate are set at build time with -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/jasonm4130/wattop/internal/version.Version=1.2.3"
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// String returns a human-readable summary of the build, e.g.
// "wattop v0.3.0 (4544e44, 2026-09-20T10:00:00Z)". Any field left at its
// placeholder (no ldflags, e.g. a `go install ...@latest` binary) falls
// back to debug.ReadBuildInfo: the main module version, vcs.revision and
// vcs.time.
func String() string {
	v, c, d := resolve(Version, Commit, BuildDate, debug.ReadBuildInfo)
	return fmt.Sprintf("wattop %s (%s, %s)", v, c, d)
}

// resolve fills each placeholder field from readInfo's build info. A value
// set with -ldflags always wins. readInfo is a parameter so tests can
// supply a synthetic debug.BuildInfo.
func resolve(version, commit, date string, readInfo func() (*debug.BuildInfo, bool)) (string, string, string) {
	info, ok := readInfo()
	if !ok || info == nil {
		return version, commit, date
	}
	if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "none" && s.Value != "" {
				commit = s.Value
				if len(commit) > 7 {
					commit = commit[:7]
				}
			}
		case "vcs.time":
			if date == "unknown" && s.Value != "" {
				date = s.Value
			}
		}
	}
	return version, commit, date
}
