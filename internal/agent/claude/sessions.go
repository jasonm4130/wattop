// Package claude implements domain.AgentSource for Claude Code by reading
// its on-disk state — no process is spawned and no syscall beyond stat,
// open, seek and read is ever made, which is what keeps this package
// fully testable on Linux CI.
package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// sessionFilePattern is a positive allowlist for filenames under
// ~/.claude/sessions/: exactly digits followed by ".json". This is
// deliberately not a blocklist on "*.key" — a future filename shape must
// fail to match rather than accidentally pass, since <pid>.<hash>.key files
// are auth secrets for the messaging socket and must never be opened.
var sessionFilePattern = regexp.MustCompile(`^\d+\.json$`)

// SessionFile is one decoded ~/.claude/sessions/<pid>.json record.
type SessionFile struct {
	PID             int
	SessionID       string
	CWD             string
	Status          string // normalised onto busy|waiting|unknown (never "" and never a raw Claude Code value)
	RawStatus       string // the on-disk status string verbatim, even when Status above was remapped or defaulted
	Kind            string
	Entrypoint      string
	Name            string
	BridgeSessionID string
	UpdatedAt       time.Time
	StatusUpdatedAt time.Time
}

// normaliseStatus maps a raw ~/.claude/sessions/<pid>.json "status" value
// onto the domain's busy|waiting|unknown vocabulary (domain.Session.Status
// also accepts "stale" and "rate-limited", but those are stamped elsewhere
// — a session file itself can only ever say busy/waiting/unknown).
//
// Real Claude Code has been observed emitting "idle", "shell", "busy" and
// "unknown" (docs/manual-qa.md), plus no status key at all — "waiting"
// itself has never once been observed on disk. "idle" means the session is
// genuinely waiting on the user, so it maps to "waiting" rather than falling
// through to "unknown". "shell"/"tool" mean a command is actively running,
// so they map to "busy". Anything else unrecognised — including "" — maps
// to "unknown" rather than being passed through raw, so the panel's status
// switch (which only handles the five domain values) never silently drops
// into its default Muted case for a value it could have mapped correctly.
func normaliseStatus(raw string) string {
	switch raw {
	case "busy":
		return "busy"
	case "waiting", "idle":
		return "waiting"
	case "shell", "tool":
		return "busy"
	default:
		return "unknown"
	}
}

// rawSessionFile mirrors the on-disk shape. Every field wattop does not use
// is still absent here deliberately — this is not a general-purpose decoder.
type rawSessionFile struct {
	PID             int    `json:"pid"`
	SessionID       string `json:"sessionId"`
	CWD             string `json:"cwd"`
	Status          string `json:"status"`
	Kind            string `json:"kind"`
	Entrypoint      string `json:"entrypoint"`
	Name            string `json:"name"`
	BridgeSessionID string `json:"bridgeSessionId"`
	UpdatedAt       int64  `json:"updatedAt"`
	StatusUpdatedAt int64  `json:"statusUpdatedAt"`
}

// ListSessionFiles reads every allowlisted session file directly inside
// dir (~/.claude/sessions/ in production) and decodes it. A missing dir
// yields (nil, nil) rather than an error — no Claude process has ever run
// is not a failure. Only a failure to list the directory itself (a
// permissions error, say) is returned as an error; a single file that
// fails to read or decode is skipped rather than failing every other
// session in the same poll, since Claude Code rewrites these files in
// place and a torn read is a live race, not a hypothetical.
func ListSessionFiles(dir string) ([]SessionFile, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []SessionFile
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !sessionFilePattern.MatchString(name) {
			// Never open anything that doesn't match the allowlist — in
			// particular, never <pid>.<hash>.key, which is a messaging
			// socket auth secret.
			continue
		}

		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			// Claude Code rewrites this file in place while wattop may be
			// reading it, so a torn read is a live race, not a hypothetical.
			// One unreadable file must not blank every other session on
			// this poll — skip it and keep going, matching the pattern
			// already used for an unparseable transcript line
			// (source.go:183-189) and a rollout tail-read error
			// (codex/source.go:80).
			continue
		}

		var rec rawSessionFile
		if err := json.Unmarshal(raw, &rec); err != nil {
			// Same reasoning: a file caught mid-write can decode to
			// invalid or truncated JSON. Skip it rather than failing the
			// whole poll.
			continue
		}

		out = append(out, SessionFile{
			PID:             rec.PID,
			SessionID:       rec.SessionID,
			CWD:             rec.CWD,
			Status:          normaliseStatus(rec.Status),
			RawStatus:       rec.Status,
			Kind:            rec.Kind,
			Entrypoint:      rec.Entrypoint,
			Name:            rec.Name,
			BridgeSessionID: rec.BridgeSessionID,
			UpdatedAt:       msToTime(rec.UpdatedAt),
			StatusUpdatedAt: msToTime(rec.StatusUpdatedAt),
		})
	}
	return out, nil
}

func msToTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}
