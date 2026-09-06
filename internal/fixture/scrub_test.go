package fixture

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// homeUserGuardRe bans any "/Users/<name>" home path other than the
// redacted placeholder "/Users/u" would produce — but Scrub's placeholder is
// "/home/u", so any surviving "/Users/<name>" at all is a leak. It is
// intentionally broader than homeUserRe in scrub.go: the rule and the guard
// must not be the same literal, or neither can catch the other's gap (a
// fixture recorded under a different account, or after a username change,
// would pass the old exact-string guard even though the redaction rule
// missed it too).
var homeUserGuardRe = regexp.MustCompile(`/Users/[^/"]+`)

// TestNoRealPathsInCommittedFixtures is redaction as a test, not a promise:
// it walks every committed testdata corpus in the repo — not just
// testdata/agent/, which is only one of several places a real path could
// leak (testdata/soc/ is recorded from mactop and can carry account-derived
// strings; golden/snapshot frames under internal/*/testdata render cwd
// strings) — and fails if any real identifier leaked through, using a
// regex that is broader than scrub.go's own redaction pattern so the guard
// does not share blind spots with the rule it is checking.
func TestNoRealPathsInCommittedFixtures(t *testing.T) {
	root := filepath.Dir(CorpusDir()) // repo root
	bannedLiterals := []string{"jasonm4130", "jasonm4130@gmail.com"}
	skipDirs := map[string]bool{".git": true, "docs": true, "dist": true}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.Contains(path, string(filepath.Separator)+"testdata"+string(filepath.Separator)) {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			// Documentation describing the redaction pattern (e.g.
			// testdata/README.md) legitimately mentions "/Users/<name>"
			// as prose, not as a leaked real path.
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, s := range bannedLiterals {
			if bytes.Contains(data, []byte(s)) {
				t.Errorf("%s: contains banned string %q", path, s)
			}
		}
		if m := homeUserGuardRe.Find(data); m != nil {
			t.Errorf("%s: contains unredacted home path %q", path, m)
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

// TestScrubStreamPreservesMissingFinalNewline is the tailer's invariant:
// a .jsonl file recorded from a live transcript ends mid-write, so its last
// line carries no terminator, and the scrubbed copy must not grow one. A
// line-splitter that strips terminators and re-appends '\n' passes every
// other test here and fails this one.
func TestScrubStreamPreservesMissingFinalNewline(t *testing.T) {
	in := []byte(`{"type":"user","message":{"content":[{"type":"text","text":"first"}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"text","text":"café — naïve 日本語"}]}}`)

	var out bytes.Buffer
	if err := ScrubStream(bytes.NewReader(in), &out, "test"); err != nil {
		t.Fatalf("ScrubStream: %v", err)
	}

	got := out.Bytes()
	if bytes.HasSuffix(got, []byte("\n")) {
		t.Errorf("output ends in a newline the input did not have: %q", got[len(got)-8:])
	}
	if len(got) != len(in) {
		t.Errorf("len(out) = %d, want %d (byte length must survive scrubbing)", len(got), len(in))
	}
	if n := bytes.Count(got, []byte("\n")); n != 1 {
		t.Errorf("newline count = %d, want 1", n)
	}
}

// TestScrubStreamPreservesTrailingNewline is the other half: a terminated
// final line stays terminated, with exactly one newline and no extra blank
// line appended.
func TestScrubStreamPreservesTrailingNewline(t *testing.T) {
	in := []byte(`{"type":"user","message":{"content":[{"type":"text","text":"first"}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"text","text":"second"}]}}` + "\n")

	var out bytes.Buffer
	if err := ScrubStream(bytes.NewReader(in), &out, "test"); err != nil {
		t.Fatalf("ScrubStream: %v", err)
	}

	got := out.Bytes()
	if !bytes.HasSuffix(got, []byte("}\n")) {
		t.Errorf("output does not end in exactly one terminated record: %q", got[len(got)-8:])
	}
	if n := bytes.Count(got, []byte("\n")); n != 2 {
		t.Errorf("newline count = %d, want 2", n)
	}
	if len(got) != len(in) {
		t.Errorf("len(out) = %d, want %d", len(got), len(in))
	}
}

// TestScrubStreamCopiesTruncatedFinalLineThrough covers the error branch,
// which is the path testdata/agent/claude/truncated-final-line.jsonl
// actually takes: the partial record cannot be parsed, so it is written out
// byte-for-byte — still partial, still unterminated.
func TestScrubStreamCopiesTruncatedFinalLineThrough(t *testing.T) {
	partial := `{"type":"user","message":{"content":[{"type":"text","text":"Half of th`
	in := []byte(`{"type":"user","message":{"content":[{"type":"text","text":"whole"}]}}` + "\n" + partial)

	var out bytes.Buffer
	if err := ScrubStream(bytes.NewReader(in), &out, "test"); err != nil {
		t.Fatalf("ScrubStream: %v", err)
	}

	got := out.Bytes()
	if !bytes.HasSuffix(got, []byte(partial)) {
		t.Errorf("truncated final line was not copied through verbatim: %q", got)
	}
}

// TestScrubStreamPreservesCorpusLineFraming re-scrubs every committed
// .jsonl fixture and asserts the framing survives: same number of lines,
// same trailing-terminator state. Byte equality is deliberately not
// asserted — Scrub rewrites identifiers by hashing them, so re-scrubbing an
// already-scrubbed uuid yields a different (equally valid) uuid.
func TestScrubStreamPreservesCorpusLineFraming(t *testing.T) {
	root := filepath.Join(CorpusDir(), "agent")

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		in, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var out bytes.Buffer
		if err := ScrubStream(bytes.NewReader(in), &out, filepath.Base(path)); err != nil {
			t.Errorf("%s: ScrubStream: %v", path, err)
			return nil
		}

		got := out.Bytes()
		if a, b := bytes.HasSuffix(in, []byte("\n")), bytes.HasSuffix(got, []byte("\n")); a != b {
			t.Errorf("%s: trailing newline present = %v after re-scrub, want %v", path, b, a)
		}
		if a, b := bytes.Count(in, []byte("\n")), bytes.Count(got, []byte("\n")); a != b {
			t.Errorf("%s: newline count = %d after re-scrub, want %d", path, b, a)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}
