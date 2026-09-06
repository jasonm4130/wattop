//go:build !(darwin && arm64 && cgo)

package proc

import (
	"context"
	"errors"

	"github.com/jasonm4130/wattop/internal/domain"
)

// ErrUnsupportedPlatform is returned by Scanner.Scan on any build that is
// not darwin/arm64 with cgo enabled — the process scanner is a hand-written
// CGO wrapper over proc_pidinfo/proc_pid_rusage/sysctl(KERN_PROC_ALL), all
// Apple-Silicon-only, so this stub only exists to keep the tree compiling
// on Linux CI.
var ErrUnsupportedPlatform = errors.New("proc: unsupported platform (requires darwin/arm64 with cgo)")

// Scanner is the non-Apple-Silicon stub implementation of domain.ProcSource.
type Scanner struct{}

var _ domain.ProcSource = (*Scanner)(nil)

// NewScanner returns the stub domain.ProcSource.
func NewScanner() *Scanner { return &Scanner{} }

// SetSystemGPUActivePct is a no-op on this stub; see scan_darwin.go for why
// it exists at all.
func (s *Scanner) SetSystemGPUActivePct(pct *float64) {}

func (s *Scanner) Scan(ctx context.Context) ([]domain.ProcSample, error) {
	return nil, ErrUnsupportedPlatform
}

func (s *Scanner) Close() error { return nil }
