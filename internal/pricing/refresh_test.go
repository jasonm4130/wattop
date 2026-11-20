package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchClientHasTimeout(t *testing.T) {
	if pricingClient.Timeout != 10*time.Second {
		t.Errorf("pricingClient.Timeout = %v, want 10s", pricingClient.Timeout)
	}
}

func TestFetchRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := strings.Repeat(" ", 1<<20)
		for written := int64(0); written <= maxPricingBody; written += int64(len(chunk)) {
			if _, err := w.Write([]byte(chunk)); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	_, err := fetchSnapshotFrom(context.Background(), srv.Client(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("fetchSnapshotFrom over an oversized body: err = %v, want a size-limit error", err)
	}
}

func TestFetchAcceptsTableWithinLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"claude-test":{"input_cost_per_token":0.000003}}`))
	}))
	defer srv.Close()

	snap, err := fetchSnapshotFrom(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("fetchSnapshotFrom: %v", err)
	}
	if _, ok := snap.Models["claude-test"]; !ok {
		t.Errorf("fetched table is missing claude-test: %+v", snap.Models)
	}
}

func TestRefreshRecordsFailureAndKeepsTable(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	b := bookFrom(modelTable{"m": {"input_cost_per_token": 1e-06}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b.refreshAndRecord(ctx, log.New(io.Discard, "", 0))

	if err := b.RefreshErr(); err == nil {
		t.Fatal("RefreshErr() = nil after a failed fetch, want the fetch error")
	}
	if b.Entries() != 1 {
		t.Fatalf("Entries() = %d after a failed refresh, want the existing table kept", b.Entries())
	}
}

func TestRefreshFromFreshCacheClearsError(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	data, err := json.Marshal(snapshot{Models: modelTable{"a": {}, "b": {}}})
	if err != nil {
		t.Fatal(err)
	}
	path := cacheFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	b := bookFrom(modelTable{"m": {}})
	b.refreshErr = errors.New("earlier failure")

	b.refreshAndRecord(context.Background(), log.New(io.Discard, "", 0))

	if err := b.RefreshErr(); err != nil {
		t.Fatalf("RefreshErr() = %v after loading a fresh cache, want nil", err)
	}
	if b.Entries() != 2 {
		t.Fatalf("Entries() = %d, want the cached table's 2", b.Entries())
	}
}
