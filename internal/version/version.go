package version

import (
	"fmt"
	"runtime/debug"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

func String() string {
	v, c, d := resolve(Version, Commit, BuildDate, debug.ReadBuildInfo)
	return fmt.Sprintf("wattop %s (%s, %s)", v, c, d)
}

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
