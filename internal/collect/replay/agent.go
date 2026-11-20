package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

type AgentSource struct {
	name  string
	polls [][]domain.Session
	idx   int
}

func NewAgentSource(dir string) (*AgentSource, error) {
	path := filepath.Join(dir, "sessions.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("replay: reading %s: %w", path, err)
	}
	var polls [][]domain.Session
	if err := json.Unmarshal(raw, &polls); err != nil {
		return nil, fmt.Errorf("replay: decoding %s: %w", path, err)
	}
	if len(polls) == 0 {
		return nil, fmt.Errorf("replay: %s decoded to zero polls", path)
	}
	return &AgentSource{name: "replay", polls: polls}, nil
}

func (s *AgentSource) Name() string {
	return s.name
}

func (s *AgentSource) Poll(ctx context.Context, now time.Time, procs []domain.ProcSample) ([]domain.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	poll := s.polls[s.idx%len(s.polls)]
	s.idx++
	return poll, nil
}

func (s *AgentSource) Close() error { return nil }
