package main

import "testing"

func TestAllowedFile(t *testing.T) {
	cases := map[string]bool{
		"/home/u/.claude/projects/-x/abc.jsonl":                 true,
		"/home/u/.claude/projects/-x/abc/subagents/a.meta.json": true,
		"/home/u/.claude/sessions/12345.json":                   true,
		"/raw/soc/mactop-array.json":                            true,
		"/home/u/.codex/sessions/2026/11/20/rollout-x.jsonl":    true,
		"/home/u/.codex/auth.json":                              false,
		"/home/u/.claude/.credentials.json":                     false,
		"/home/u/.claude/settings.json":                         false,
		"/home/u/.claude/.claude.json":                          false,
		"/home/u/.claude/sessions/12345.abcdef.key":             false,
		"/home/u/.claude/12345.json":                            false,
		"/home/u/.codex/models_cache.json":                      false,
	}
	for path, want := range cases {
		if got := allowedFile(path); got != want {
			t.Errorf("allowedFile(%q) = %v, want %v", path, got, want)
		}
	}
}
