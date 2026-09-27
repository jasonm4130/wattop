package version

import (
	"runtime/debug"
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	s := String()
	if s == "" {
		t.Fatal("String() returned an empty string")
	}
	v, _, _ := resolve(Version, Commit, BuildDate, debug.ReadBuildInfo)
	if !strings.Contains(s, v) {
		t.Fatalf("String() = %q, want it to contain Version %q", s, v)
	}
}

func buildInfo(mainVersion string, settings map[string]string) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		info := &debug.BuildInfo{}
		info.Main.Version = mainVersion
		for k, v := range settings {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: k, Value: v})
		}
		return info, true
	}
}

// TestResolveFallsBackToBuildInfo: a `go install ...@latest` binary has no
// ldflags, so the placeholders fall back to the module version and the
// VCS stamp the toolchain embedded.
func TestResolveFallsBackToBuildInfo(t *testing.T) {
	v, c, d := resolve("dev", "none", "unknown", buildInfo("v0.3.0", map[string]string{
		"vcs.revision": "4544e44abcdef0123456789",
		"vcs.time":     "2026-09-20T10:00:00Z",
	}))
	if v != "v0.3.0" || c != "4544e44" || d != "2026-09-20T10:00:00Z" {
		t.Errorf("resolve = %q, %q, %q; want v0.3.0, 4544e44, 2026-09-20T10:00:00Z", v, c, d)
	}
}

// TestResolveLdflagsWin: values set with -ldflags are never overridden.
func TestResolveLdflagsWin(t *testing.T) {
	v, c, d := resolve("v1.2.3", "abc1234", "2026-01-01T00:00:00Z", buildInfo("v0.3.0", map[string]string{
		"vcs.revision": "4544e44abcdef",
		"vcs.time":     "2026-09-20T10:00:00Z",
	}))
	if v != "v1.2.3" || c != "abc1234" || d != "2026-01-01T00:00:00Z" {
		t.Errorf("resolve = %q, %q, %q; want the ldflags values unchanged", v, c, d)
	}
}

// TestResolveKeepsPlaceholdersWithoutInfo: a local `go build` reports its
// main module as "(devel)", which is no better than "dev"; and a missing
// build info leaves every placeholder in place.
func TestResolveKeepsPlaceholdersWithoutInfo(t *testing.T) {
	if v, _, _ := resolve("dev", "none", "unknown", buildInfo("(devel)", nil)); v != "dev" {
		t.Errorf("version with (devel) main module = %q, want dev", v)
	}
	none := func() (*debug.BuildInfo, bool) { return nil, false }
	v, c, d := resolve("dev", "none", "unknown", none)
	if v != "dev" || c != "none" || d != "unknown" {
		t.Errorf("resolve without build info = %q, %q, %q; want the placeholders", v, c, d)
	}
}
