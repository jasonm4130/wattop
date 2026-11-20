package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jasonm4130/wattop/internal/domain"
)

type ProcSource struct {
	scans []([]domain.ProcSample)
	idx   int
}

func NewProcSource(path string) (*ProcSource, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: reading %s: %w", path, err)
	}
	var scans [][]domain.ProcSample
	if err := json.Unmarshal(raw, &scans); err != nil {
		return nil, fmt.Errorf("replay: decoding %s: %w", path, err)
	}
	if len(scans) == 0 {
		return nil, fmt.Errorf("replay: %s decoded to zero scans", path)
	}
	return &ProcSource{scans: scans}, nil
}

func (s *ProcSource) Scan(ctx context.Context) ([]domain.ProcSample, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	scan := s.scans[s.idx%len(s.scans)]
	s.idx++
	return scan, nil
}

func (s *ProcSource) Close() error { return nil }
