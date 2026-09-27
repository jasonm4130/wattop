package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
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
		a.add(ev, ev.Model)
		transcript += line + "\n"
	}
	if a.usage.Output != 577 || a.usage.Input != 100 {
		t.Fatalf("duplicated usage: %+v", a.usage)
	}
	now := time.Date(2026, 9, 6, 12, 0, 10, 0, time.UTC)
	if r := a.window.Rate(now); r.OutputPerSec != float64(577)/60 || r.InputPerSec != 5 {
		t.Fatalf("rate %+v", r)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent-c.meta.json"), []byte(`{"agentType":"general-purpose"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-c.jsonl"), []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	subs, err := Walk(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 || subs[0].Usage != a.usage {
		t.Fatalf("child accounting differs: %+v", subs)
	}
}

func TestUsageUpdateReplacesPriorCounters(t *testing.T) {
	var a usageAccounting
	ev, _ := ParseRecord([]byte(`{"type":"assistant","timestamp":"2026-09-06T12:00:00Z","message":{"id":"request","usage":{"input_tokens":100,"output_tokens":10}}}`))
	a.add(ev, ev.Model)
	ev.Usage.Output = 100
	a.add(ev, ev.Model)
	if a.usage.Output != 100 {
		t.Fatalf("usage %+v", a.usage)
	}
	if r := a.window.Rate(ev.Timestamp); r.OutputPerSec != float64(100)/60 {
		t.Fatalf("rate %+v", r)
	}
}

// TestLedgerRefilesRevisedMessage: a message whose usage is revised by a
// later row ends up in the ledger once, under its latest usage and prompt
// size, with no stale or zero bucket left behind.
func TestLedgerRefilesRevisedMessage(t *testing.T) {
	var a usageAccounting
	ev, _ := ParseRecord([]byte(`{"type":"assistant","timestamp":"2026-09-06T12:00:00Z","message":{"id":"request","model":"claude-opus-5","usage":{"input_tokens":1000,"output_tokens":10}}}`))
	a.add(ev, ev.Model)
	ev.Usage.Input, ev.Usage.Output = 2500, 90
	a.add(ev, ev.Model)
	other, _ := ParseRecord([]byte(`{"type":"assistant","timestamp":"2026-09-06T12:00:01Z","message":{"id":"next","model":"claude-opus-5","usage":{"input_tokens":2400,"output_tokens":5}}}`))
	a.add(other, other.Model)

	want := domain.UsageLedger{
		{Model: "claude-opus-5", PromptK: 3}: {Input: 2500 + 2400, Output: 95},
	}
	if !reflect.DeepEqual(a.ledger, want) {
		t.Fatalf("ledger = %+v, want %+v", a.ledger, want)
	}
	if a.usage.Input != 4900 || a.usage.Output != 95 {
		t.Fatalf("usage = %+v", a.usage)
	}
}
