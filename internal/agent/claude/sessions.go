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

type SessionFile struct {
	PID             int
	SessionID       string
	CWD             string
	Status          string
	RawStatus       string
	Kind            string
	Entrypoint      string
	Name            string
	BridgeSessionID string
	UpdatedAt       time.Time
	StatusUpdatedAt time.Time
}

// Raw status values seen on disk are idle, shell, busy and unknown; "waiting"
// has never been observed.
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
			continue
		}

		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}

		var rec rawSessionFile
		if err := json.Unmarshal(raw, &rec); err != nil {
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
