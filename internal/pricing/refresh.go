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

const refreshTTL = 24 * time.Hour

var pricingClient = &http.Client{Timeout: 10 * time.Second}

const maxPricingBody = 20 << 20

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
	b.cards.Clear()
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

func fetchSnapshot(ctx context.Context) (snapshot, error) {
	return fetchSnapshotFrom(ctx, pricingClient, SourceURL)
}

func fetchSnapshotFrom(ctx context.Context, client *http.Client, url string) (snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return snapshot{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return snapshot{}, fmt.Errorf("fetch %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPricingBody+1))
	if err != nil {
		return snapshot{}, err
	}
	if len(body) > maxPricingBody {
		return snapshot{}, fmt.Errorf("fetch %s: response exceeds %d bytes", url, maxPricingBody)
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
