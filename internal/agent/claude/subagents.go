package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

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
// matching tool_result in the parent transcript — the back-link a subagent
// finished. A subagent is Live when its own toolUseId is not in this set.
//
// KNOWN GAP, deliberately left to the reducer. The spec's liveness rule is
// a conjunction: a subagent is live when its jsonl is growing AND the
// parent has recorded no matching tool_result yet. Only the second half is
// evaluated here, because the first needs the previous cycle's size and
// mtime and this package holds no cross-cycle state by design — the
// reducer in internal/state does, and it already owns every other
// "has this stopped moving" judgement (session retention, Codex's
// mtime-derived stale). Two consequences for whoever adds the growth half
// there: a child that died or hung before the parent wrote its tool_result
// reads Live until that record lands, and after a context compaction
// resets the parent's transcript, resultedToolUseIDs is re-derived from a
// file that may no longer carry those tool_result lines, so finished
// subagents can flip back to Live.
func Walk(dir string, resultedToolUseIDs map[string]bool) ([]domain.Subagent, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []domain.Subagent
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

		jsonlName := strings.TrimSuffix(name, ".meta.json") + ".jsonl"
		usage, model, err := sumTranscript(filepath.Join(dir, jsonlName))
		if err != nil {
			return nil, err
		}

		out = append(out, domain.Subagent{
			Hash:        meta.Hash,
			AgentType:   meta.AgentType,
			Description: meta.Description,
			Model:       model,
			ToolUseID:   meta.ToolUseID,
			SpawnDepth:  meta.SpawnDepth,
			Usage:       usage,
			Live:        !resultedToolUseIDs[meta.ToolUseID],
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Hash < out[j].Hash })
	return out, nil
}

// sumTranscript reads a whole subagent transcript (these are small — a few
// turns — unlike the multi-MB main session transcript, so a full read
// rather than an offset tail is appropriate here) and returns the summed
// Usage across every assistant record plus the last non-empty model seen,
// which is the API-resolved model id.
func sumTranscript(path string) (domain.Usage, string, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return domain.Usage{}, "", nil
	}
	if err != nil {
		return domain.Usage{}, "", err
	}

	lines, _ := splitCompleteLines(append(raw, '\n'))

	var usage domain.Usage
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
			usage.Input += ev.Usage.Input
			usage.Output += ev.Usage.Output
			usage.CacheRead += ev.Usage.CacheRead
			usage.CacheCreate5m += ev.Usage.CacheCreate5m
			usage.CacheCreate1h += ev.Usage.CacheCreate1h
			usage.Thinking += ev.Usage.Thinking
			usage.CachedInput += ev.Usage.CachedInput
		}
	}
	return usage, model, nil
}
