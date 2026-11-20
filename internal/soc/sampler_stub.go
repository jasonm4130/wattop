//go:build !(darwin && arm64 && cgo)

package soc

import (
	"context"
	"errors"

	"github.com/jasonm4130/wattop/internal/domain"
)

var ErrUnsupportedPlatform = errors.New("soc: unsupported platform (requires darwin/arm64 with cgo)")

type Sampler struct{}

func NewSampler() *Sampler { return &Sampler{} }

func (s *Sampler) Init() error {
	return ErrUnsupportedPlatform
}

func (s *Sampler) Close() error {
	return nil
}

func (s *Sampler) ThermalState() int {
	return 0
}

func (s *Sampler) Channels() map[string]bool {
	return nil
}

func (s *Sampler) Sample(ctx context.Context, intervalMs int) (domain.SysSample, error) {
	return domain.SysSample{}, ErrUnsupportedPlatform
}
