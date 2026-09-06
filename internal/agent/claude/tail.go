package claude

import (
	"bytes"
	"io"
	"os"
	"syscall"
)

// TailState is the offset-tailer's persisted position: which file, which
// inode it last saw at that path, and how far into it has been consumed.
// Shared in shape with Task 9's Codex tailer.
type TailState struct {
	Path   string
	Inode  uint64
	Offset int64
}

// Tail reads the bytes appended to state.Path since state.Offset and
// returns the complete lines found (each without its trailing newline),
// plus the advanced state. It never re-reads a whole multi-MB file: only
// the delta past Offset is read.
//
// A trailing partial line (no terminating newline yet) is left unconsumed
// — the returned state's Offset stops before it — so it is buffered on
// disk rather than in memory and is retried whole on the next call once
// the writer finishes it.
//
// State resets to offset 0 when the file shrank below the last-known
// offset (truncation) or its inode changed (rotation/recreation) since the
// last call. A caller with no prior state passes TailState{Path: p} — the
// zero Inode value only ever matches a file whose own inode is 0, so the
// first call for a path always establishes a fresh baseline rather than
// spuriously resetting.
func Tail(state TailState) (TailState, [][]byte, error) {
	fi, err := os.Stat(state.Path)
	if err != nil {
		return state, nil, err
	}

	ino := inodeOf(fi)
	offset := state.Offset
	if state.Inode != 0 && ino != state.Inode {
		offset = 0
	}
	if fi.Size() < offset {
		offset = 0
	}

	f, err := os.Open(state.Path)
	if err != nil {
		return state, nil, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return state, nil, err
		}
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return state, nil, err
	}

	lines, consumed := splitCompleteLines(data)

	newState := TailState{
		Path:   state.Path,
		Inode:  ino,
		Offset: offset + consumed,
	}
	return newState, lines, nil
}

// splitCompleteLines splits data on '\n' and returns every complete line
// (terminator stripped) plus the byte count consumed by those complete
// lines. Any trailing bytes with no terminating '\n' are not returned and
// not counted as consumed, so the next Tail call re-reads them from the
// same offset along with whatever was appended after them.
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

func inodeOf(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
