package pricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestFetchClientHasTimeout: the refresh runs on a goroutine nobody waits
// on, so a stalled server must not hold its connection open forever.
func TestFetchClientHasTimeout(t *testing.T) {
	if pricingClient.Timeout != 10*time.Second {
		t.Errorf("pricingClient.Timeout = %v, want 10s", pricingClient.Timeout)
	}
}

// TestFetchRejectsOversizedBody: a response larger than maxPricingBody is
// refused rather than read into memory whole.
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

// TestFetchAcceptsTableWithinLimit: the cap does not break a normal fetch.
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
