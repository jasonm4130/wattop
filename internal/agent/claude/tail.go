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

// TailResult is one Tail call's outcome. Reset is the load-bearing field:
// it is the only way a caller can tell that Lines start from byte 0 of a
// file it has already read part of, rather than continuing where the last
// call stopped.
//
// A caller that accumulates anything derived from Lines — token sums, a
// tool histogram, a high-water mark — MUST discard those accumulations
// when Reset is true, or it double-counts the rewritten prefix. Comparing
// offsets instead does not work: Claude Code compacts a session's context
// in place, rewriting the transcript at the same path, and the rewritten
// file is routinely longer than the old offset, so the offset moves
// forward exactly as it does for an ordinary append.
type TailResult struct {
	State TailState
	Lines [][]byte
	Reset bool
}

// maxReadPerCall bounds how many bytes a single Tail call will pull off
// disk. Without it, the first poll on a path (Offset == 0) or any poll
// that follows an in-place compaction (which resets Offset to 0, see
// TailResult.Reset) reads the entire transcript in one shot — Claude Code
// transcripts routinely run to tens of MB, and the returned Lines keep
// that whole allocation reachable for as long as any line is held. A
// transcript larger than this cap is caught up over several calls instead
// of one: each call advances Offset by what it consumed, and the next
// call picks up where it left off, exactly as it would for a plain
// append. That is correct behaviour for a dashboard polling on an
// interval, not a regression — it trades a multi-hundred-MB synchronous
// spike for a few extra poll cycles.
const maxReadPerCall = 8 * 1024 * 1024

// Tail reads the bytes appended to state.Path since state.Offset and
// returns the complete lines found (each without its trailing newline),
// plus the advanced state. It never re-reads a whole multi-MB file in one
// call: only the delta past Offset is read, capped at maxReadPerCall per
// call (see its doc comment) so a large first read or post-reset read is
// spread across several polls instead of spiking memory.
//
// A trailing partial line (no terminating newline yet) is left unconsumed
// — the returned state's Offset stops before it — so it is buffered on
// disk rather than in memory and is retried whole on the next call once
// the writer finishes it. The same holds for a line split by the
// maxReadPerCall boundary: it is simply retried in full next call.
//
// State resets to offset 0 on any of three signals that the bytes under
// the offset are not the bytes that produced it — the file shrank below
// the last-known offset (truncation), its inode changed (rotation or
// recreation), or the byte before the offset is no longer a newline (an
// in-place rewrite that landed past the old offset, which neither of the
// other two sees) — and TailResult.Reset reports that it happened. A caller with
// no prior state passes TailState{Path: p} — the zero Inode value only
// ever matches a file whose own inode is 0, so the first call for a path
// always establishes a fresh baseline rather than spuriously resetting,
// and never reports Reset.
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

	// Offset always sits immediately after a newline, because only
	// complete lines are ever consumed. If the byte before it is not a
	// newline, the bytes under this offset are no longer the bytes that
	// produced it: the file was truncated and rewritten in place at the
	// same inode with MORE content than the old offset, which neither the
	// inode nor the size check above can see. Without this, the next read
	// would resume mid-record in an unrelated file and resync on whatever
	// newline it hit. One byte to check, and it cannot fire on an
	// untouched file.
	if offset > 0 {
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

// endsLineAt reports whether the byte at offset-1 is a newline — the
// invariant a tailer offset must satisfy, since it is only ever advanced
// past complete lines. A read that comes up short (the file shrank between
// the stat and this read) is reported as false, which resets rather than
// resuming at an offset nothing vouches for.
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
