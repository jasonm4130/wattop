package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jasonm4130/wattop/internal/agent/tokenrate"
	"github.com/jasonm4130/wattop/internal/domain"
)

// subagentMetaPattern is the positive allowlist for subagent metadata
// filenames: agent-<hash>.meta.json.
var subagentMetaPattern = regexp.MustCompile(`^agent-.+\.meta\.json$`)

// rawSubagentMeta mirrors one agent-<hash>.meta.json file.
type rawSubagentMeta struct {
	ToolUseID   string `json:"toolUseId"`
	Hash        string `json:"hash"`
	AgentType   string `json:"agentType"`
	Description string `json:"description"`
	Model       string `json:"model"`
	SpawnDepth  int    `json:"spawnDepth"`
}

// subagentRecord pairs one walked subagent with the identity and observed
// byte length of its own transcript: key is the agent-<hash> filename stem
// (the meta.json's "hash" field is not it — the two disagree in the
// corpus), and size is the jsonl's length at this walk, which is what a
// caller compares across polls to decide the transcript is still growing.
type subagentRecord struct {
	window tokenrate.Window
	Sub    domain.Subagent
	Key    string
	Size   int64
}

// Walk reads every agent-<hash>.meta.json / agent-<hash>.jsonl pair under
// dir (<sessionId>/subagents/ in production) and returns one
// domain.Subagent per pair, sorted by hash for a deterministic order.
//
// Cost accounting uses the child transcript's own message.model, not
// meta.json's model: meta.json records the alias the caller asked for
// ("opus") while the transcript records the id the API resolved
// ("claude-opus-5").
//
// resultedToolUseIDs is the set of tool_use ids that already have a
// matching tool_result in the parent transcript — the back-link that says a
// subagent has finished, and the same link that assembles the tree.
//
// Live as returned here is that back-link alone. The full rule is a
// conjunction — a subagent is live when its jsonl is growing AND its
// tool_use is still unanswered — and "growing" is a comparison against the
// previous poll's byte count, which no single walk of a directory can make.
// Source.pollOne holds that count across polls and ANDs the growth half in,
// reading each child's current size from walkRecords below.
func Walk(dir string, resultedToolUseIDs map[string]bool) ([]domain.Subagent, error) {
	recs, err := walkRecords(dir, resultedToolUseIDs)
	if err != nil || recs == nil {
		return nil, err
	}
	out := make([]domain.Subagent, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.Sub)
	}
	return out, nil
}

// walkRecords is Walk plus each subagent's on-disk identity and size.
func walkRecords(dir string, resultedToolUseIDs map[string]bool) ([]subagentRecord, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []subagentRecord
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !subagentMetaPattern.MatchString(name) {
			continue
		}

		metaRaw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var meta rawSubagentMeta
		if err := json.Unmarshal(metaRaw, &meta); err != nil {
			return nil, err
		}

		key := strings.TrimSuffix(name, ".meta.json")
		accounting, model, size, err := sumTranscript(filepath.Join(dir, key+".jsonl"))
		if err != nil {
			return nil, err
		}

		out = append(out, subagentRecord{
			window: accounting.window,
			Sub: domain.Subagent{
				Hash:        meta.Hash,
				AgentType:   meta.AgentType,
				Description: meta.Description,
				Model:       model,
				ToolUseID:   meta.ToolUseID,
				SpawnDepth:  meta.SpawnDepth,
				Usage:       accounting.usage,
				Live:        !resultedToolUseIDs[meta.ToolUseID],
			},
			Key:  key,
			Size: size,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Sub.Hash < out[j].Sub.Hash })
	return out, nil
}

// sumTranscript reads a whole subagent transcript (these are small — a few
// turns — unlike the multi-MB main session transcript, so a full read
// rather than an offset tail is appropriate here) and returns the summed
// Usage across every assistant record, the last non-empty model seen —
// which is the API-resolved model id — and the transcript's byte length,
// taken from the bytes actually read rather than a second stat, so the
// size a caller compares across polls is the size of the content it was
// given. A transcript that does not exist yet is zero bytes, not an error.
func sumTranscript(path string) (usageAccounting, string, int64, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return usageAccounting{}, "", 0, nil
	}
	if err != nil {
		return usageAccounting{}, "", 0, err
	}

	lines, _ := splitCompleteLines(append(raw, '\n'))

	var accounting usageAccounting
	var model string
	for _, line := range lines {
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		ev, err := ParseRecord(line)
		if err != nil {
			// A trailing partial line (agent-3.jsonl ends mid-tool-call)
			// is not valid JSON — skip it rather than fail the whole walk.
			continue
		}
		if ev.Model != "" {
			model = ev.Model
		}
		if ev.HasUsage {
			accounting.add(ev)
		}
	}
	return accounting, model, int64(len(raw)), nil
}
