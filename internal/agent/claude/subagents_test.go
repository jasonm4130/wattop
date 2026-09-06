package claude

import (
	"path/filepath"
	"testing"

	"github.com/jasonm4130/wattop/internal/fixture"
)

// resultedToolUseIDsFromParent replays subagent-tree.jsonl and returns the
// set of tool_use ids that already have a matching tool_result — the
// back-link Walk uses to decide Live.
func resultedToolUseIDsFromParent(t *testing.T) map[string]bool {
	t.Helper()
	lines := readFixtureLines(t, "agent", "claude", "subagent-tree.jsonl")
	resulted := map[string]bool{}
	for _, l := range lines {
		ev, err := ParseRecord(l)
		if err != nil {
			t.Fatalf("ParseRecord: %v", err)
		}
		for _, id := range ev.ToolResultIDs {
			resulted[id] = true
		}
	}
	return resulted
}

// TestSubagentTreeAssemblesOneLive walks the corpus's three-subagent
// fixture and asserts exactly one is live: agent-3's tool_use has no
// matching tool_result in the parent transcript.
func TestSubagentTreeAssemblesOneLive(t *testing.T) {
	dir := filepath.Join(fixture.CorpusDir(), "agent", "claude", "subagents")
	resulted := resultedToolUseIDsFromParent(t)

	subs, err := Walk(dir, resulted)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("got %d subagents, want 3", len(subs))
	}

	live := 0
	for _, s := range subs {
		if s.Live {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("got %d live subagents, want 1: %+v", live, subs)
	}
}

// TestSubagentUsesResolvedModel is the corpus's central assertion: the
// child transcript's message.model ("claude-fable-5-1") is used, not the
// alias meta.json recorded ("fable").
func TestSubagentUsesResolvedModel(t *testing.T) {
	dir := filepath.Join(fixture.CorpusDir(), "agent", "claude", "subagents")
	resulted := resultedToolUseIDsFromParent(t)

	subs, err := Walk(dir, resulted)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	var liveOne *struct {
		Model string
		Live  bool
	}
	for _, s := range subs {
		if s.Live {
			liveOne = &struct {
				Model string
				Live  bool
			}{s.Model, s.Live}
		}
	}
	if liveOne == nil {
		t.Fatalf("no live subagent found among %+v", subs)
	}
	if liveOne.Model != "claude-fable-5-1" {
		t.Fatalf("Model = %q, want the resolved id claude-fable-5-1, not the meta.json alias", liveOne.Model)
	}
}

func TestWalkIgnoresNonMetaFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "agent-x.meta.json"), `{"toolUseId":"t1","hash":"h1","agentType":"general-purpose","model":"opus"}`)
	writeFile(t, filepath.Join(dir, "agent-x.jsonl"), `{"type":"assistant","message":{"model":"claude-opus-5","content":[],"usage":{"input_tokens":1,"output_tokens":1}}}`+"\n")
	// A stray file that is not a .meta.json sibling must be ignored, not
	// opened as metadata.
	writeFile(t, filepath.Join(dir, "notes.txt"), "not a subagent file")

	subs, err := Walk(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("got %d subagents, want 1: %+v", len(subs), subs)
	}
	if subs[0].Model != "claude-opus-5" {
		t.Fatalf("Model = %q, want claude-opus-5", subs[0].Model)
	}
	if !subs[0].Live {
		t.Fatalf("Live = false, want true (toolUseId t1 has no matching result)")
	}
}

func TestWalkMissingDirIsNotAnError(t *testing.T) {
	subs, err := Walk(filepath.Join(t.TempDir(), "no-such-dir"), nil)
	if err != nil {
		t.Fatalf("Walk on missing dir: %v", err)
	}
	if subs != nil {
		t.Fatalf("got %+v, want nil", subs)
	}
}
