//go:build darwin && arm64 && cgo

package proc

import (
	"context"
	"os"
	"testing"

	"github.com/jasonm4130/wattop/internal/domain"
)

func scanSelf(t *testing.T, s *Scanner) domain.ProcSample {
	t.Helper()
	procs, err := s.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, ps := range procs {
		if ps.PID == os.Getpid() {
			return ps
		}
	}
	t.Fatalf("own pid %d missing from scan", os.Getpid())
	return domain.ProcSample{}
}

func TestScanFirstSightingHasNoCPUPct(t *testing.T) {
	s := NewScanner()
	defer s.Close()

	if first := scanSelf(t, s); first.CPUPct != nil {
		t.Fatalf("first scan: CPUPct = %v, want nil (no baseline to measure against)", *first.CPUPct)
	}
	if second := scanSelf(t, s); second.CPUPct == nil {
		t.Fatalf("second scan: CPUPct = nil, want a measured percentage")
	}
}
