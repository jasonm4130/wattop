package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRepeatedMessageUsageCountedOnce(t *testing.T) {
	var a usageAccounting
	var transcript string
	for i, kind := range []string{"thinking", "text", "tool_use"} {
		line := fmt.Sprintf(`{"type":"assistant","timestamp":"2026-09-06T12:00:0%dZ","message":{"id":"one-request","model":"claude-opus-5","content":[{"type":%q}],"usage":{"input_tokens":100,"output_tokens":577,"cache_read_input_tokens":200}}}`, i, kind)
		ev, err := ParseRecord([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		a.add(ev)
		transcript += line + "\n"
	}
	if a.usage.Output != 577 || a.usage.Input != 100 {
		t.Fatalf("duplicated usage: %+v", a.usage)
	}
	now := time.Date(2026, 9, 6, 12, 0, 10, 0, time.UTC)
	if r := a.window.Rate(now); r.OutputPerSec != float64(577)/60 || r.InputPerSec != 5 {
		t.Fatalf("rate %+v", r)
	}
	path := filepath.Join(t.TempDir(), "child.jsonl")
	if err := os.WriteFile(path, []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	child, _, _, err := sumTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if child.usage != a.usage {
		t.Fatalf("child accounting differs: %+v", child.usage)
	}
}

func TestUsageUpdateReplacesPriorCounters(t *testing.T) {
	var a usageAccounting
	ev, _ := ParseRecord([]byte(`{"type":"assistant","timestamp":"2026-09-06T12:00:00Z","message":{"id":"request","usage":{"input_tokens":100,"output_tokens":10}}}`))
	a.add(ev)
	ev.Usage.Output = 100
	a.add(ev)
	if a.usage.Output != 100 {
		t.Fatalf("usage %+v", a.usage)
	}
	if r := a.window.Rate(ev.Timestamp); r.OutputPerSec != float64(100)/60 {
		t.Fatalf("rate %+v", r)
	}
}
