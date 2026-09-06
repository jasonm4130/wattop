// Package version exposes build-time version information, settable via
// -ldflags at build time and defaulting to placeholder values otherwise.
package version

import "fmt"

// Version, Commit and BuildDate are set at build time with -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/jasonm4130/wattop/internal/version.Version=1.2.3"
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// String returns a human-readable summary of the build, e.g.
// "wattop dev (none, unknown)".
func String() string {
	return fmt.Sprintf("wattop %s (%s, %s)", Version, Commit, BuildDate)
}
