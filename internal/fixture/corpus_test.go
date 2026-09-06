package fixture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCorpusDirResolves(t *testing.T) {
	dir := CorpusDir()

	if !filepath.IsAbs(dir) {
		t.Fatalf("CorpusDir() = %q, want an absolute path", dir)
	}
	if filepath.Base(dir) != "testdata" {
		t.Fatalf("CorpusDir() = %q, want a path ending in testdata", dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("CorpusDir() = %q does not exist: %v", dir, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "go.mod")); err != nil {
		t.Fatalf("CorpusDir()'s parent has no go.mod: %v", err)
	}
}

func TestCorpusDirFindsSOCFixtures(t *testing.T) {
	path := filepath.Join(CorpusDir(), "soc", "mactop-ndjson.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
