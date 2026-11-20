package codex

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jasonm4130/wattop/internal/agent/tokenrate"
	"github.com/jasonm4130/wattop/internal/domain"
)

func inodeOf(fi os.FileInfo) uint64 {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(st.Ino)
}

type Rollout struct {
	tokens      tokenrate.Window
	hasUsage    bool
	lastUsageAt time.Time
	Path        string
	ModTime     time.Time

	SessionID string
	MetaAt    time.Time
	CWD       string
	Model     string

	ThreadID       string
	ParentThreadID string
	SpawnDepth     int
	AgentType      string
	Nickname       string
	AgentPath      string

	Status        string
	TaskCompleted bool

	Usage        domain.Usage
	Ledger       domain.UsageLedger
	ContextUsed  int64
	ContextMax   int64
	ContextExact bool
	RateLimits   []domain.RateLimit

	ToolCalls    int
	CurrentTool  string
	pendingCalls []pendingCall
}

type tailState struct {
	path   string
	inode  uint64
	offset int64
}

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

	r := bufio.NewReader(f)
	var lines [][]byte
	for {
		chunk, readErr := r.ReadBytes('\n')
		if readErr != nil {
			break
		}
		t.offset += int64(len(chunk))
		line := strings.TrimRight(string(chunk), "\r\n")
		if line != "" {
			lines = append(lines, []byte(line))
		}
	}

	return lines, nil
}

// Selection is by mtime, never by the YYYY/MM/DD folder or the filename's
// timestamp. Codex writes a rollout into the folder of the day the session
// started and keeps appending to it, so a resumed session lives in an old
// folder with a fresh mtime; filenames are local time while records are UTC.
func ShortlistRollouts(root string, now time.Time, maxAge time.Duration) ([]string, error) {
	matches, _, err := walkRollouts(root, now.Add(-maxAge))
	return matches, err
}

func walkRollouts(root string, cutoff time.Time) ([]string, map[string]bool, error) {
	var matches []string
	dirs := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !isRolloutName(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			return nil
		}
		matches = append(matches, path)
		dirs[filepath.Dir(path)] = true
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, dirs, nil
		}
		return nil, nil, err
	}
	sort.Strings(matches)
	return matches, dirs, nil
}

func scanRolloutDir(dir string, cutoff time.Time) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var matches []string
	for _, e := range entries {
		if e.IsDir() || !isRolloutName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().Before(cutoff) {
			continue
		}
		matches = append(matches, filepath.Join(dir, e.Name()))
	}
	return matches, nil
}

func isRolloutName(name string) bool {
	return strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, ".jsonl")
}

const rewalkInterval = 30 * time.Second

type rolloutIndex struct {
	root       string
	walkedAt   time.Time
	walkCutoff time.Time
	rootMtime  time.Time
	hotDirs    map[string]bool
}

func newRolloutIndex(root string) *rolloutIndex {
	return &rolloutIndex{root: root}
}

func (x *rolloutIndex) shortlist(now, cutoff time.Time) ([]string, error) {
	fi, err := os.Stat(x.root)
	if err != nil {
		if os.IsNotExist(err) {
			x.walkedAt = time.Time{}
			x.hotDirs = nil
			return nil, nil
		}
		return nil, err
	}

	needWalk := x.walkedAt.IsZero() ||
		now.Sub(x.walkedAt) >= rewalkInterval ||
		now.Before(x.walkedAt) ||
		cutoff.Before(x.walkCutoff) ||
		!fi.ModTime().Equal(x.rootMtime)

	if needWalk {
		matches, dirs, err := walkRollouts(x.root, cutoff)
		if err != nil {
			return nil, err
		}
		x.walkedAt = now
		x.walkCutoff = cutoff
		x.rootMtime = fi.ModTime()
		x.hotDirs = dirs
		return matches, nil
	}

	dirs := make(map[string]bool, len(x.hotDirs)+2)
	for d := range x.hotDirs {
		dirs[d] = true
	}
	for _, d := range []time.Time{now, now.Add(-24 * time.Hour)} {
		dirs[filepath.Join(x.root, d.Format("2006"), d.Format("01"), d.Format("02"))] = true
	}

	var matches []string
	for dir := range dirs {
		m, err := scanRolloutDir(dir, cutoff)
		if err != nil {
			continue
		}
		matches = append(matches, m...)
	}
	sort.Strings(matches)
	return matches, nil
}

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
