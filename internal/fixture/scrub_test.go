package fixture

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRealPathsInCommittedFixtures is redaction as a test, not a promise:
// it walks every committed file under testdata/agent/ and fails if any of
// this machine's real identifiers leaked through.
func TestNoRealPathsInCommittedFixtures(t *testing.T) {
	root := filepath.Join(CorpusDir(), "agent")
	banned := []string{"/Users/jasonmatthew", "jasonm4130", "jasonm4130@gmail.com"}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, s := range banned {
			if bytes.Contains(data, []byte(s)) {
				t.Errorf("%s: contains banned string %q", path, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

func TestScrubPreservesByteLength(t *testing.T) {
	line := []byte(`{"type":"text","text":"café — naïve 日本語"}`)

	out, err := Scrub(line)
	if err != nil {
		t.Fatalf("Scrub: %v", err)
	}
	if len(out) != len(line) {
		t.Fatalf("len(Scrub(line)) = %d, want %d (in=%q out=%q)", len(out), len(line), line, out)
	}
}

func TestScrubPreservesUsageBlockByteForByte(t *testing.T) {
	const usage = `{"input_tokens":2,"cache_creation_input_tokens":25528,"cache_read_input_tokens":26415,"output_tokens":884,"output_tokens_details":{"thinking_tokens":588}}`
	line := []byte(`{"parentUuid":"642a4fe3-523d-4d78-b0b0-d951288b6858","message":{"model":"claude-fable-5-1","usage":` + usage + `}}`)

	out, err := Scrub(line)
	if err != nil {
		t.Fatalf("Scrub: %v", err)
	}

	var decoded struct {
		Message struct {
			Usage json.RawMessage `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal scrubbed output: %v", err)
	}
	if string(decoded.Message.Usage) != usage {
		t.Fatalf("usage block changed:\n  got  %s\n  want %s", decoded.Message.Usage, usage)
	}
}

func TestScrubMapsToolUseIDConsistently(t *testing.T) {
	parent := []byte(`{"uuid":"11111111-1111-1111-1111-111111111111","toolUseId":"toolu_01Abc123"}`)
	meta := []byte(`{"toolUseId":"toolu_01Abc123","agentType":"general-purpose"}`)

	outParent, err := Scrub(parent)
	if err != nil {
		t.Fatalf("Scrub(parent): %v", err)
	}
	outMeta, err := Scrub(meta)
	if err != nil {
		t.Fatalf("Scrub(meta): %v", err)
	}

	var p struct {
		ToolUseID string `json:"toolUseId"`
	}
	var m struct {
		ToolUseID string `json:"toolUseId"`
	}
	if err := json.Unmarshal(outParent, &p); err != nil {
		t.Fatalf("unmarshal parent: %v", err)
	}
	if err := json.Unmarshal(outMeta, &m); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}

	if p.ToolUseID == "" || p.ToolUseID == "toolu_01Abc123" {
		t.Fatalf("toolUseId was not rewritten: %q", p.ToolUseID)
	}
	if p.ToolUseID != m.ToolUseID {
		t.Fatalf("toolUseId rewritten inconsistently: parent=%q meta=%q", p.ToolUseID, m.ToolUseID)
	}
}

func TestScrubRedactsHomePath(t *testing.T) {
	line := []byte(`{"cwd":"/Users/jasonmatthew/Work/Git/ai-dashboard"}`)

	out, err := Scrub(line)
	if err != nil {
		t.Fatalf("Scrub: %v", err)
	}
	if bytes.Contains(out, []byte("jasonmatthew")) {
		t.Fatalf("Scrub left the real username in: %s", out)
	}
	if !strings.Contains(string(out), "/home/u/Work/Git/ai-dashboard") {
		t.Fatalf("Scrub did not rewrite the home path: %s", out)
	}
}

func TestScrubPreservesModelAndStatus(t *testing.T) {
	line := []byte(`{"model":"claude-opus-5","status":"busy","entrypoint":"cli","kind":"interactive"}`)

	out, err := Scrub(line)
	if err != nil {
		t.Fatalf("Scrub: %v", err)
	}

	var decoded map[string]string
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]string{
		"model":      "claude-opus-5",
		"status":     "busy",
		"entrypoint": "cli",
		"kind":       "interactive",
	}
	for k, v := range want {
		if decoded[k] != v {
			t.Errorf("field %q = %q, want %q", k, decoded[k], v)
		}
	}
}

func TestScrubRejectsTruncatedJSON(t *testing.T) {
	if _, err := Scrub([]byte(`{"type":"text","text":"unterminat`)); err == nil {
		t.Fatal("Scrub of truncated JSON: want error, got nil")
	}
}
