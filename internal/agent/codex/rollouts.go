// Package codex implements domain.AgentSource for the Codex CLI.
//
// This is the least-certain surface in the project: there is no per-pid
// session file the way Claude Code has one, so status is inferred from a
// task_started/task_complete histogram and the pid bind is a best-effort
// join on cwd and process start time. See bind.go and parse.go for the
// specifics, and the plan's Task 9 section for the evidence behind them.
package codex

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// inodeOf returns fi's inode number, or 0 if the platform's os.FileInfo.Sys()
// does not carry one (this project targets darwin and linux CI only; both
// expose *syscall.Stat_t).
func inodeOf(fi os.FileInfo) uint64 {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(st.Ino)
}

// Rollout is one shortlisted rollout file plus everything parsed out of it.
type Rollout struct {
	Path    string
	ModTime time.Time // file mtime; drives the stale inference (idle threshold) and the mtime shortlist

	SessionID string    // from session_meta.payload.session_id; "" if session_meta never arrived (e.g. rate-limits.jsonl)
	MetaAt    time.Time // session_meta's own record timestamp, parsed as UTC; zero if session_meta never arrived.
	// A zero MetaAt makes this rollout unmatched in bind.go's time
	// tiebreaker, exactly like a candidate's zero StartTime — there is no
	// session-start instant to compare a pid's start time against.
	CWD   string // most recently seen cwd, from session_meta or turn_context
	Model string // most recently seen turn_context.payload.model — read per turn, never a config default

	Status string // "busy" | "waiting" | "stale", inferred from the task_started/task_complete histogram; see parse.go

	Usage        domain.Usage
	ContextUsed  int64 // total_tokens from the most recent token_count event
	ContextMax   int64 // info.model_context_window, falling back to task_started's top-level model_context_window
	ContextExact bool  // true once both ContextUsed and ContextMax are known from the transcript
	RateLimits   []domain.RateLimit
}

// tailState is the offset-tailer shape shared with Task 8's Claude tailer:
// {path, inode, offset}. Open, Seek(offset), read complete lines only,
// advance the offset, close. Reset to offset 0 when the size shrinks or the
// inode changes. A trailing partial line is buffered, not parsed, and is
// retried on the next poll.
type tailState struct {
	path   string
	inode  uint64
	offset int64
}

// readNewLines opens path, seeks to the tailer's offset (resetting first if
// the file shrank or its inode changed), and returns every complete line
// appended since the last call. It never re-reads a whole multi-MB file.
func (t *tailState) readNewLines() ([][]byte, error) {
	f, err := os.Open(t.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	inode := inodeOf(fi)
	if inode != 0 && t.inode != 0 && inode != t.inode {
		t.offset = 0
	}
	if fi.Size() < t.offset {
		t.offset = 0
	}
	t.inode = inode

	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return nil, err
	}

	// A trailing partial line is never advanced past: t.offset stops right
	// before it, so the next call's Seek naturally re-reads those same bytes
	// once more has been appended — no separate buffer needed.
	r := bufio.NewReader(f)
	var lines [][]byte
	for {
		chunk, readErr := r.ReadBytes('\n')
		if readErr != nil {
			break // trailing partial line (or EOF with nothing pending): stop, retry next poll.
		}
		t.offset += int64(len(chunk))
		line := strings.TrimRight(string(chunk), "\r\n")
		if line != "" {
			lines = append(lines, []byte(line))
		}
	}

	return lines, nil
}

// ShortlistRollouts walks root (normally ~/.codex/sessions) looking for
// rollout-<ts>-<uuid>.jsonl files under today's and yesterday's YYYY/MM/DD
// local-time directories, and returns those modified within the last
// maxAge. Shortlisting is by mtime, never by the filename's embedded
// timestamp: rollout filenames are local time while the records inside are
// UTC, so a file named for "today" in local time can hold a last event
// timestamped "yesterday" in UTC (or vice versa) near midnight, and
// filtering by the filename would silently skip the current day's
// directory. now is passed in so tests are deterministic.
func ShortlistRollouts(root string, now time.Time, maxAge time.Duration) ([]string, error) {
	var dirs []string
	for _, d := range []time.Time{now, now.Add(-24 * time.Hour)} {
		dirs = append(dirs, filepath.Join(root, d.Format("2006"), d.Format("01"), d.Format("02")))
	}

	seen := make(map[string]bool)
	var matches []string
	cutoff := now.Add(-maxAge)

	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		seen[dir] = true

		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				continue
			}
			matches = append(matches, filepath.Join(dir, name))
		}
	}

	sort.Strings(matches)
	return matches, nil
}

// readAllLines reads path and splits it into complete lines, dropping a
// trailing partial line the way the tailer would buffer-and-retry it (a
// non-incremental read has no next poll to retry on, so it drops rather than
// blocks).
func readAllLines(path string) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			line := bytesTrimCR(data[start:i])
			if len(line) > 0 {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	// A trailing partial line (no terminating newline) is dropped, matching
	// what a real tailer would buffer rather than parse.
	return lines, nil
}

func bytesTrimCR(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\r' {
		return b[:n-1]
	}
	return b
}

func statModTime(path string) (time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return fi.ModTime(), nil
}
