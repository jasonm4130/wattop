// json.go is wattop's headless contract: --json prints one domain.Snapshot
// per interval as NDJSON to stdout and never enters the alt screen; --once
// prints exactly one and exits. Snapshot's JSON shape is the public
// interface (see internal/domain/snapshot_test.go's schema test).
package main

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

// runOnce runs exactly one coordinated cycle, reduces it, and writes the
// resulting Snapshot to w as a single JSON line. It returns the Snapshot so
// doctor.go can reuse the same one-cycle machinery for its own reporting.
//
// --once therefore takes one full --interval before it prints anything,
// because Loop.Cycle's soc.Sample call blocks inside C for that long when
// soc is available -- that is the price of the first sample being a real
// power delta rather than an instantaneous zero, not a hang.
func runOnce(ctx context.Context, loop *Loop, w io.Writer) (*domain.Snapshot, error) {
	in := loop.Cycle(ctx)
	snap := loop.State.Reduce(in)
	return snap, writeSnapshotLine(w, snap)
}

// runJSON runs cycles until ctx is cancelled, writing one NDJSON line per
// cycle to w.
//
// It paces with the same Loop.pace as the interactive path: soc.Sample
// blocking for the interval is the period when it works, and the remainder
// wait is what keeps a fast-failing sampler from spraying NDJSON at
// whatever rate the error returns.
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
