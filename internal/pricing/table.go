package pricing

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// SourceURL is the upstream LiteLLM pricing table both scripts/pricing.sh
// and refresh.go fetch, pinned in one place so the two never drift.
const SourceURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

//go:embed table.json.gz
var embeddedTable []byte

// modelEntry is one model's raw pricing record from the upstream table,
// kept as a loose map because tiered cost keys (e.g. *_above_272k_tokens)
// vary by model and are discovered at lookup time, never declared as
// fields — see (*Book).Cost's tier scan.
type modelEntry map[string]any

// modelTable maps a bare model key, as published upstream, to its entry.
type modelTable map[string]modelEntry

// snapshot is the envelope stored in table.json.gz and in the refresh cache
// file: the model table plus enough provenance to answer (*Book).Source().
type snapshot struct {
	SourceURL   string     `json:"source_url"`
	SourceSHA   string     `json:"source_sha256"`
	GeneratedAt time.Time  `json:"generated_at"`
	Models      modelTable `json:"models"`
}

// Book is the resolved pricing table plus provenance. It is safe for
// concurrent use: (*Book).Refresh may swap the table underneath readers.
type Book struct {
	mu   sync.RWMutex
	snap snapshot
}

// Load returns the embedded snapshot. It never touches the network and
// never blocks: table.json.gz is compiled into the binary, so first run is
// correct offline and instant.
func Load() (*Book, error) {
	snap, err := decodeSnapshot(embeddedTable)
	if err != nil {
		return nil, fmt.Errorf("pricing: load embedded table: %w", err)
	}
	return &Book{snap: snap}, nil
}

// Entries reports how many models the current table carries, for `doctor`.
func (b *Book) Entries() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.snap.Models)
}

// Source reports where the current table came from: the upstream URL, the
// SHA-256 of the exact upstream bytes it was built from, and when that
// snapshot was generated (embed time, or the last successful refresh).
func (b *Book) Source() (url string, sha256hex string, fetchedAt time.Time) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.snap.SourceURL, b.snap.SourceSHA, b.snap.GeneratedAt
}

func decodeSnapshot(gz []byte) (snapshot, error) {
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return snapshot{}, fmt.Errorf("decompress: %w", err)
	}
	defer r.Close()
	data, err := io.ReadAll(r)
	if err != nil {
		return snapshot{}, fmt.Errorf("read: %w", err)
	}
	return parseSnapshotJSON(data)
}

func parseSnapshotJSON(data []byte) (snapshot, error) {
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return snapshot{}, fmt.Errorf("parse: %w", err)
	}
	return snap, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
