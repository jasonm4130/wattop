package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// refreshTTL is how long a cached refresh is trusted before a new fetch is
// attempted.
const refreshTTL = 24 * time.Hour

// Refresh performs an async, non-blocking pricing refresh into
// $XDG_CACHE_HOME/wattop/pricing.json with a 24h TTL: a fresh cache is used
// rather than blocking on the network, a stale or missing one triggers a
// background fetch of SourceURL, and any failure is logged and left
// non-fatal — the embedded (or previously cached) table stays in place.
// Safe to call once at start.
func (b *Book) Refresh(ctx context.Context) {
	go b.refresh(ctx)
}

func (b *Book) refresh(ctx context.Context) {
	path := cacheFilePath()

	if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < refreshTTL {
		if data, err := os.ReadFile(path); err == nil {
			if snap, err := parseSnapshotJSON(data); err == nil {
				b.swap(snap)
				return
			}
		}
	}

	snap, err := fetchSnapshot(ctx)
	if err != nil {
		log.Printf("pricing: refresh failed, keeping existing table: %v", err)
		return
	}

	if data, err := json.Marshal(snap); err == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			if err := os.WriteFile(path, data, 0o644); err != nil {
				log.Printf("pricing: could not write cache %s: %v", path, err)
			}
		}
	}

	b.swap(snap)
}

func (b *Book) swap(snap snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snap = snap
}

func cacheFilePath() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		if home, err := os.UserHomeDir(); err == nil {
			base = filepath.Join(home, ".cache")
		} else {
			base = os.TempDir()
		}
	}
	return filepath.Join(base, "wattop", "pricing.json")
}

// fetchSnapshot downloads and parses the full upstream table (not the
// curated subset table.json.gz embeds), so a refresh resolves strictly more
// models than the embedded snapshot, never fewer.
func fetchSnapshot(ctx context.Context) (snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, SourceURL, nil)
	if err != nil {
		return snapshot{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return snapshot{}, fmt.Errorf("fetch %s: status %d", SourceURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return snapshot{}, err
	}
	var models modelTable
	if err := json.Unmarshal(body, &models); err != nil {
		return snapshot{}, err
	}
	return snapshot{
		SourceURL:   SourceURL,
		SourceSHA:   sha256Hex(body),
		GeneratedAt: time.Now().UTC(),
		Models:      models,
	}, nil
}
