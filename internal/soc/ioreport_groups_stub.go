//go:build !(darwin && arm64 && cgo)

package soc

type Group struct {
	Name     string
	Channels int
}

func IOReportGroups() (groups []Group, complete bool, err error) {
	return nil, false, ErrUnsupportedPlatform
}
