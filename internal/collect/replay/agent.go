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

// AgentSource implements domain.AgentSource by replaying a scripted
// sequence of session lists recorded as testdata/replay/sessions.json — a
// [][]domain.Session, one element per Poll() call, looping at the end.
// It ignores the procs argument: the fixture already carries whatever
// Proc/PID/BindConf shape each scripted session needs.
type AgentSource struct {
	name  string
	polls [][]domain.Session
	idx   int
}

// NewAgentSource loads the scripted poll sequence from
// filepath.Join(dir, "sessions.json").
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

// Name identifies this AgentSource for Snapshot.Degraded badges.
func (s *AgentSource) Name() string {
	return s.name
}

// Poll returns the next recorded session list, looping once the scripted
// sequence is exhausted. now and procs are accepted for interface
// compatibility and ignored.
func (s *AgentSource) Poll(ctx context.Context, now time.Time, procs []domain.ProcSample) ([]domain.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	poll := s.polls[s.idx%len(s.polls)]
	s.idx++
	return poll, nil
}

// Close satisfies domain.AgentSource; replay holds no resources to release.
func (s *AgentSource) Close() error { return nil }
