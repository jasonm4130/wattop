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

const SourceURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

//go:embed table.json.gz
var embeddedTable []byte

type modelEntry map[string]any

type modelTable map[string]modelEntry

type snapshot struct {
	SourceURL   string     `json:"source_url"`
	SourceSHA   string     `json:"source_sha256"`
	GeneratedAt time.Time  `json:"generated_at"`
	Models      modelTable `json:"models"`
}

type Book struct {
	mu    sync.RWMutex
	snap  snapshot
	cards sync.Map
}

func Load() (*Book, error) {
	snap, err := decodeSnapshot(embeddedTable)
	if err != nil {
		return nil, fmt.Errorf("pricing: load embedded table: %w", err)
	}
	return &Book{snap: snap}, nil
}

func (b *Book) Entries() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.snap.Models)
}

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
