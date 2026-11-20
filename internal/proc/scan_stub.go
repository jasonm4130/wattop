//go:build !(darwin && arm64 && cgo)

package proc

import (
	"context"
	"errors"

	"github.com/jasonm4130/wattop/internal/domain"
)

var ErrUnsupportedPlatform = errors.New("proc: unsupported platform (requires darwin/arm64 with cgo)")

type Scanner struct{}

var _ domain.ProcSource = (*Scanner)(nil)

func NewScanner() *Scanner { return &Scanner{} }

func (s *Scanner) Scan(ctx context.Context, systemGPUActivePct *float64) ([]domain.ProcSample, error) {
	return nil, ErrUnsupportedPlatform
}

func (s *Scanner) Close() error { return nil }
