package main

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func runOnce(ctx context.Context, loop *Loop, w io.Writer) (*domain.Snapshot, error) {
	in := loop.Cycle(ctx)
	snap := loop.State.Reduce(in)
	return snap, writeSnapshotLine(w, snap)
}

func runJSON(ctx context.Context, loop *Loop, w io.Writer) error {
	loop.Send = nil
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		start := time.Now()
		in := loop.Cycle(ctx)
		snap := loop.State.Reduce(in)
		if err := writeSnapshotLine(w, snap); err != nil {
			return err
		}
		if loop.MaxCycles > 0 && loop.n >= loop.MaxCycles {
			return nil
		}
		if !loop.pace(ctx, start) {
			return nil
		}
	}
}

func writeSnapshotLine(w io.Writer, snap *domain.Snapshot) error {
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}
