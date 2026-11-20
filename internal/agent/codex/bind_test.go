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

func TestBindRefusesDoubleBind(t *testing.T) {
	metaAt := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)
	rollouts := []Rollout{
		{Path: "/r/one.jsonl", CWD: "/home/u/proj", MetaAt: metaAt},
	}

	nearStart := metaAt.Add(-5 * time.Second)
	farStart := metaAt.Add(-2 * time.Minute)
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
	if b.Conf != domain.BindInferred {
		t.Fatalf("BindConf = %q, want inferred (the time tiebreaker decided)", b.Conf)
	}

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
	if b.Conf != domain.BindExact {
		t.Fatalf("BindConf = %q, want exact", b.Conf)
	}
}

func TestZeroStartTimeUnmatched(t *testing.T) {
	rollouts := []Rollout{
		{Path: "/r/one.jsonl", CWD: "/home/u/proj", MetaAt: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)},
	}
	procs := []domain.ProcSample{
		codexProc(100, "/home/u/proj", time.Time{}),
	}

	got := bindAll(rollouts, procs, "")
	b := got["/r/one.jsonl"]
	if b.PID != nil {
		t.Fatalf("binding = %+v, want unmatched (zero StartTime)", b)
	}
	if b.Conf != domain.BindUnknown {
		t.Fatalf("BindConf = %q, want unknown", b.Conf)
	}
}

func TestEmptyCWDOrArgvUnmatched(t *testing.T) {
	rollouts := []Rollout{
		{Path: "/r/one.jsonl", CWD: "/home/u/proj", MetaAt: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)},
	}
	start := time.Date(2026, 9, 6, 9, 59, 0, 0, time.UTC)
	procs := []domain.ProcSample{
		{PID: 100, Argv: []string{"codex"}, CWD: "", StartTime: start},
		{PID: 200, Argv: nil, CWD: "/home/u/proj", StartTime: start},
	}

	got := bindAll(rollouts, procs, "")
	b := got["/r/one.jsonl"]
	if b.PID != nil {
		t.Fatalf("binding = %+v, want unmatched (both candidates are unusable)", b)
	}
}

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
	if sess.BindConf != domain.BindUnknown {
		t.Fatalf("Session.BindConf = %q, want unknown", sess.BindConf)
	}
	if sess.Agent != "codex" {
		t.Fatalf("Session.Agent = %q, want codex", sess.Agent)
	}
	if sess.ID != "sess-1" {
		t.Fatalf("Session.ID = %q, want sess-1", sess.ID)
	}
}

func TestSessionIDFallsBackToPathWhenNoSessionMeta(t *testing.T) {
	r := Rollout{Path: "/r/no-meta.jsonl", CWD: "/home/u/proj"}
	sess := sessionFromRollout(r, binding{Conf: domain.BindUnknown}, "waiting", nil)
	if sess.ID != "/r/no-meta.jsonl" {
		t.Fatalf("Session.ID = %q, want the rollout path as a fallback", sess.ID)
	}
}

func TestAppServerDaemonNeverBinds(t *testing.T) {
	metaAt := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	rollouts := []Rollout{{Path: "/r/home.jsonl", CWD: "/Users/u", MetaAt: metaAt}}
	daemon := domain.ProcSample{
		PID:       83924,
		Argv:      []string{"/Users/u/.codex/packages/standalone/current/bin/codex", "app-server", "daemon", "pid-update-loop"},
		CWD:       "/Users/u",
		StartTime: metaAt.Add(-18 * 24 * time.Hour),
	}

	got := bindAll(rollouts, []domain.ProcSample{daemon}, "")
	if b := got["/r/home.jsonl"]; b.PID != nil || b.Conf != domain.BindUnknown {
		t.Fatalf("binding = {pid %v, %q}, want unbound: the daemon owns no session", derefAll(map[string]*int{"pid": b.PID}), b.Conf)
	}
	if anyCodexCandidate([]domain.ProcSample{daemon}, "") {
		t.Error("anyCodexCandidate = true for the daemon alone, want false")
	}

	session := codexProc(500, "/Users/u", metaAt.Add(-3*time.Second))
	got = bindAll(rollouts, []domain.ProcSample{daemon, session}, "")
	if b := got["/r/home.jsonl"]; b.PID == nil || *b.PID != 500 || b.Conf != domain.BindExact {
		t.Fatalf("with a real session beside the daemon: binding = %+v, want pid 500 exact", b)
	}
}
