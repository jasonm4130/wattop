package fixture

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestSubagentMetaModelIsAliasNotResolved(t *testing.T) {
	metas, err := filepath.Glob(filepath.Join(CorpusDir(), "agent", "claude", "subagents", "agent-*.meta.json"))
	if err != nil {
		t.Fatalf("glob subagent metas: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("found %d subagent meta files, want the 3 the corpus documents", len(metas))
	}

	for _, metaPath := range metas {
		raw, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("read %s: %v", metaPath, err)
		}
		var meta struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			t.Fatalf("unmarshal %s: %v", metaPath, err)
		}
		if meta.Model == "" {
			t.Fatalf("%s has no model field", metaPath)
		}

		transcript := strings.TrimSuffix(metaPath, ".meta.json") + ".jsonl"
		resolved := assistantModel(t, transcript)

		if resolved == "" {
			t.Fatalf("%s has no assistant record carrying message.model", transcript)
		}
		if meta.Model == resolved {
			t.Fatalf("%s and %s both say %q: the corpus must keep the requested alias "+
				"distinct from the resolved model id, or a reader of the wrong field "+
				"cannot be caught", metaPath, transcript, resolved)
		}
		if strings.HasPrefix(meta.Model, "claude-") || !strings.HasPrefix(resolved, "claude-") {
			t.Fatalf("%s model=%q, %s model=%q: meta must hold the requested alias and "+
				"the transcript the resolved id, not the reverse",
				metaPath, meta.Model, transcript, resolved)
		}
	}
}

func assistantModel(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Type == "assistant" && rec.Message.Model != "" {
			return rec.Message.Model
		}
	}
	return ""
}
