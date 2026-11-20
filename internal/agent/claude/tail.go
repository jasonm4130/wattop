package claude

import (
	"bytes"
	"io"
	"os"
	"syscall"
)

type TailState struct {
	Path   string
	Inode  uint64
	Offset int64
}

// TailResult is one Tail call's outcome. Reset means Lines start from byte 0
// of a file the caller already read part of. Callers must discard anything
// accumulated from earlier Lines when it is true. Offsets cannot detect this:
// Claude Code compacts in place at the same path and the rewritten file is
// often longer than the old offset.
type TailResult struct {
	State TailState
	Lines [][]byte
	Reset bool
}

const maxReadPerCall = 8 * 1024 * 1024

func Tail(state TailState) (TailResult, error) {
	fi, err := os.Stat(state.Path)
	if err != nil {
		return TailResult{State: state}, err
	}

	ino := inodeOf(fi)
	offset := state.Offset
	reset := false
	if state.Inode != 0 && ino != state.Inode {
		offset = 0
		reset = true
	}
	if fi.Size() < offset {
		offset = 0
		reset = true
	}

	f, err := os.Open(state.Path)
	if err != nil {
		return TailResult{State: state}, err
	}
	defer f.Close()

	if offset > 0 {
		// Offset always follows a newline; if it does not, the file was rewritten
		// in place with more content than the old offset, which size and inode
		// checks cannot see.
		ok, err := endsLineAt(f, offset)
		if err != nil {
			return TailResult{State: state}, err
		}
		if !ok {
			offset = 0
			reset = true
		}
	}

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return TailResult{State: state}, err
		}
	}

	data, err := io.ReadAll(io.LimitReader(f, maxReadPerCall))
	if err != nil {
		return TailResult{State: state}, err
	}

	lines, consumed := splitCompleteLines(data)

	return TailResult{
		State: TailState{
			Path:   state.Path,
			Inode:  ino,
			Offset: offset + consumed,
		},
		Lines: lines,
		Reset: reset,
	}, nil
}

func splitCompleteLines(data []byte) ([][]byte, int64) {
	var lines [][]byte
	var consumed int64
	rest := data
	for {
		idx := bytes.IndexByte(rest, '\n')
		if idx < 0 {
			break
		}
		line := rest[:idx]
		lines = append(lines, line)
		consumed += int64(idx) + 1
		rest = rest[idx+1:]
	}
	return lines, consumed
}

func endsLineAt(f *os.File, offset int64) (bool, error) {
	var b [1]byte
	n, err := f.ReadAt(b[:], offset-1)
	if err != nil && n == 0 {
		if err == io.EOF {
			return false, nil
		}
		return false, err
	}
	return b[0] == '\n', nil
}

func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
