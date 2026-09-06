package codex

import (
	"testing"
	"time"

	"github.com/jasonm4130/wattop/internal/domain"
)

func codexProc(pid int, cwd string, start time.Time) domain.ProcSample {
	return domain.ProcSample{
		PID:       pid,
		Argv:      []string{"codex", "exec"},
		CWD:       cwd,
		StartTime: start,
	}
}

// TestBindRefusesDoubleBind: two candidate pids share one cwd, and only one
// rollout has that cwd. The time tiebreaker (nearest session_meta timestamp
// to, and not before, the pid's start time) must pick exactly one of them —
// never both.
func TestBindRefusesDoubleBind(t *testing.T) {
	metaAt := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	rollouts := []Rollout{
		{Path: "/r/one.jsonl", CWD: "/home/u/proj", MetaAt: metaAt},
	}

	nearStart := metaAt.Add(-5 * time.Second) // pid 100 started 5s before session_meta
	farStart := metaAt.Add(-2 * time.Minute)  // pid 200 started 2m before session_meta
	procs := []domain.ProcSample{
		codexProc(100, "/home/u/proj", nearStart),
		codexProc(200, "/home/u/proj", farStart),
	}

	got := bindAll(rollouts, procs, "")
	b, ok := got["/r/one.jsonl"]
	if !ok {
		t.Fatal("no binding entry for the rollout")
	}
	if b.PID == nil {
		t.Fatal("rollout bound to no pid, want the nearer one bound")
	}
	if *b.PID != 100 {
		t.Fatalf("bound pid = %d, want 100 (nearest-and-not-before session_meta)", *b.PID)
	}
	if b.Conf != "inferred" {
		t.Fatalf("BindConf = %q, want inferred (the time tiebreaker decided)", b.Conf)
	}

	// The losing pid must not silently also claim this rollout, and no
	// second rollout exists for it to claim instead — Bind's exported
	// projection should agree with bindAll.
	pidMap := Bind(rollouts, procs)
	if pidMap["/r/one.jsonl"] == nil || *pidMap["/r/one.jsonl"] != 100 {
		t.Fatalf("Bind() = %v, want {/r/one.jsonl: 100}", derefAll(pidMap))
	}
}

func derefAll(m map[string]*int) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if v == nil {
			out[k] = nil
		} else {
			out[k] = *v
		}
	}
	return out
}

// TestExactBindWhenCWDUnique: exactly one rollout and one candidate share a
// cwd — BindConf must be "exact", not "inferred", since no tiebreak was
// needed.
func TestExactBindWhenCWDUnique(t *testing.T) {
	rollouts := []Rollout{
		{Path: "/r/one.jsonl", CWD: "/home/u/proj", MetaAt: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)},
	}
	procs := []domain.ProcSample{
		codexProc(100, "/home/u/proj", time.Date(2026, 9, 6, 9, 59, 0, 0, time.UTC)),
	}

	got := bindAll(rollouts, procs, "")
	b := got["/r/one.jsonl"]
	if b.PID == nil || *b.PID != 100 {
		t.Fatalf("binding = %+v, want pid 100 bound", b)
	}
	if b.Conf != "exact" {
		t.Fatalf("BindConf = %q, want exact", b.Conf)
	}
}

// TestZeroStartTimeUnmatched: a candidate whose StartTime is the zero
// time.Time (Task 6's guard for an underivable start) is left unmatched,
// exactly like an empty Argv or CWD.
func TestZeroStartTimeUnmatched(t *testing.T) {
	rollouts := []Rollout{
		{Path: "/r/one.jsonl", CWD: "/home/u/proj", MetaAt: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)},
	}
	procs := []domain.ProcSample{
		codexProc(100, "/home/u/proj", time.Time{}), // zero StartTime
	}

	got := bindAll(rollouts, procs, "")
	b := got["/r/one.jsonl"]
	if b.PID != nil {
		t.Fatalf("binding = %+v, want unmatched (zero StartTime)", b)
	}
	if b.Conf != "unknown" {
		t.Fatalf("BindConf = %q, want unknown", b.Conf)
	}
}

func TestEmptyCWDOrArgvUnmatched(t *testing.T) {
	rollouts := []Rollout{
		{Path: "/r/one.jsonl", CWD: "/home/u/proj", MetaAt: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)},
	}
	start := time.Date(2026, 9, 6, 9, 59, 0, 0, time.UTC)
	procs := []domain.ProcSample{
		{PID: 100, Argv: []string{"codex"}, CWD: "", StartTime: start}, // empty cwd
		{PID: 200, Argv: nil, CWD: "/home/u/proj", StartTime: start},   // empty argv
	}

	got := bindAll(rollouts, procs, "")
	b := got["/r/one.jsonl"]
	if b.PID != nil {
		t.Fatalf("binding = %+v, want unmatched (both candidates are unusable)", b)
	}
}

// TestUnboundRolloutStillRendered: a rollout with no candidate pid renders
// as a domain.Session with PID == nil and BindConf == "unknown" — dropped
// rows read as "nothing is running", which is worse than an honest gap.
func TestUnboundRolloutStillRendered(t *testing.T) {
	r := Rollout{
		Path:      "/r/unbound.jsonl",
		SessionID: "sess-1",
		CWD:       "/home/u/proj",
		Model:     "gpt-5.6-terra",
		ModTime:   time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
	}

	binds := bindAll([]Rollout{r}, nil, "")
	b := binds[r.Path]

	sess := sessionFromRollout(r, b, "waiting", nil)

	if sess.PID != nil {
		t.Fatalf("Session.PID = %v, want nil", *sess.PID)
	}
	if sess.BindConf != "unknown" {
		t.Fatalf("Session.BindConf = %q, want unknown", sess.BindConf)
	}
	if sess.Agent != "codex" {
		t.Fatalf("Session.Agent = %q, want codex", sess.Agent)
	}
	if sess.ID != "sess-1" {
		t.Fatalf("Session.ID = %q, want sess-1", sess.ID)
	}
}

// TestSessionIDFallsBackToPathWhenNoSessionMeta: a rollout whose
// session_meta never arrived (rate-limits.jsonl's shape) still renders as a
// session, identified by its file path rather than an empty id.
func TestSessionIDFallsBackToPathWhenNoSessionMeta(t *testing.T) {
	r := Rollout{Path: "/r/no-meta.jsonl", CWD: "/home/u/proj"}
	sess := sessionFromRollout(r, binding{Conf: "unknown"}, "waiting", nil)
	if sess.ID != "/r/no-meta.jsonl" {
		t.Fatalf("Session.ID = %q, want the rollout path as a fallback", sess.ID)
	}
}
