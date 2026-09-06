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
	Status          string // "busy" | "waiting" | "unknown" (never defaulted to "waiting")
	Kind            string
	Entrypoint      string
	Name            string
	BridgeSessionID string
	UpdatedAt       time.Time
	StatusUpdatedAt time.Time
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
// is not a failure. Any other stat/read/decode error is returned as-is so
// the caller can report source health.
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
			return nil, err
		}

		var rec rawSessionFile
		if err := json.Unmarshal(raw, &rec); err != nil {
			return nil, err
		}

		status := rec.Status
		if status == "" {
			// A session file with no status key at all renders "unknown",
			// never "waiting" and never idle. This is real: pid 99571 on
			// the capture machine has no status key.
			status = "unknown"
		}

		out = append(out, SessionFile{
			PID:             rec.PID,
			SessionID:       rec.SessionID,
			CWD:             rec.CWD,
			Status:          status,
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
