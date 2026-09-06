//go:build !(darwin && arm64 && cgo)

package soc

// Group mirrors the darwin/arm64/cgo definition so cmd/wattop's doctor
// compiles and tests on Linux CI. See ioreport_groups_darwin.go.
type Group struct {
	Name     string
	Channels int
}

// IOReportGroups is the non-Apple-Silicon stub: there is no IOReport to
// enumerate off this platform.
func IOReportGroups() (groups []Group, complete bool, err error) {
	return nil, false, ErrUnsupportedPlatform
}
