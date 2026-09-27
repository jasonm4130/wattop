package demo

import "github.com/jasonm4130/wattop/internal/domain"

// Models are ids the embedded pricing table prices exactly, so every demo
// cost renders as a figure rather than "$—".
const (
	modelOpus   = "claude-opus-5"
	modelSonnet = "claude-sonnet-5"
	modelHaiku  = "claude-haiku-4-5"
	modelCodex  = "gpt-5.6-terra"
)

var (
	claudeTools = []string{"Read", "Read", "Edit", "Bash", "Grep", "Glob", "Edit", "Write", "TodoWrite", "Agent"}
	exploreTool = []string{"Read", "Grep", "Glob", "Bash"}
	codexTools  = []string{"shell", "apply_patch", "shell", "update_plan"}
)

// sessionSpecs is the demo's cast: four Claude Code sessions and two Codex
// sessions in fictional projects. Paths are written with a literal "~" so
// no real home directory can ever appear in a screenshot.
func sessionSpecs() []sessionSpec {
	claudeArgv := []string{"claude"}
	return []sessionSpec{
		{
			agent: "claude", id: "7f3c2a9e-1b4d-4e8a-9c21-5d0b6e8f4a17",
			cwd: "~/code/api-server", name: "api-server", model: modelOpus, kind: "interactive",
			comm: "2.1.270", argv: claudeArgv, pid: 48213,
			busyFrac: 0.82, segSec: 11, phase: 2,
			r:      rates{in: 30, out: 44, cacheRead: 2600, cacheCreate: 100},
			base:   domain.Usage{Input: 38_000, Output: 61_000, CacheRead: 5_900_000, CacheCreate5m: 210_000},
			ctxMax: 1_000_000, ctx: 342_000, ctxGrowth: 90,
			rssMB: 612, ageMin: 47,
			tools: claudeTools, baseTools: 64,
			workflow: &workflowSpec{id: "wf_7c1e9a42d3b8", phase: "review", startedMin: 6},
			subs: []subSpec{
				{
					id: "a3f9c1e27b04", agentType: "Explore", desc: "Map auth middleware call sites",
					model: modelHaiku, mode: subRunning, startedMin: 4.5,
					r:    rates{in: 30, out: 55, cacheRead: 2600, cacheCreate: 120},
					base: domain.Usage{Input: 9_000, Output: 12_000, CacheRead: 610_000, CacheCreate5m: 48_000},
					ctx:  61_000, tools: exploreTool,
				},
				{
					id: "b81d0f5c9a36", agentType: "general-purpose", desc: "Draft sessions table migration",
					model: modelSonnet, mode: subToggle, startedMin: 3.2, background: true,
					r:    rates{in: 25, out: 40, cacheRead: 2100, cacheCreate: 110},
					base: domain.Usage{Input: 6_500, Output: 9_800, CacheRead: 420_000, CacheCreate5m: 36_000},
					ctx:  74_000, tools: []string{"Read", "Edit", "Bash", "Write"},
				},
				{
					id: "c5e27a9d1f80", parent: "b81d0f5c9a36", depth: 1, agentType: "Explore",
					desc: "Find existing migration helpers", model: modelHaiku, mode: subDone,
					startedMin: 2.6, idleSec: 70,
					base: domain.Usage{Input: 3_200, Output: 4_100, CacheRead: 180_000, CacheCreate5m: 21_000},
					ctx:  38_000,
				},
				{
					id: "d0a4b7e39c12", workflow: "wf_7c1e9a42d3b8", phase: "review", agentType: "code-reviewer",
					desc: "Review rate limiter", model: modelSonnet, mode: subDone, startedMin: 6, idleSec: 150,
					base: domain.Usage{Input: 4_800, Output: 7_300, CacheRead: 260_000, CacheCreate5m: 30_000},
					ctx:  52_000,
				},
				{
					id: "e6c3f1a08b57", workflow: "wf_7c1e9a42d3b8", phase: "review", agentType: "code-reviewer",
					desc: "Review token refresh", model: modelSonnet, mode: subDone, startedMin: 6, idleSec: 95,
					base: domain.Usage{Input: 5_100, Output: 8_200, CacheRead: 300_000, CacheCreate5m: 33_000},
					ctx:  57_000,
				},
				{
					id: "f29b8d4c7e61", workflow: "wf_7c1e9a42d3b8", phase: "review", agentType: "code-reviewer",
					desc: "Review audit logging", model: modelSonnet, mode: subRunning, startedMin: 2.1,
					r:    rates{in: 20, out: 36, cacheRead: 1800, cacheCreate: 90},
					base: domain.Usage{Input: 2_900, Output: 4_400, CacheRead: 150_000, CacheCreate5m: 19_000},
					ctx:  41_000, tools: exploreTool,
				},
				{
					id: "0b7e5a2d9c43", workflow: "wf_7c1e9a42d3b8", phase: "review", agentType: "code-reviewer",
					desc: "Review error envelopes", model: modelSonnet, mode: subToggle, startedMin: 1.4,
					r:    rates{in: 18, out: 30, cacheRead: 1500, cacheCreate: 80},
					base: domain.Usage{Input: 1_700, Output: 2_600, CacheRead: 90_000, CacheCreate5m: 14_000},
					ctx:  33_000, tools: exploreTool,
				},
			},
		},
		{
			agent: "claude", id: "c41e8d02-6a7f-4b39-8e15-2f9d3c7a0b64",
			cwd: "~/code/web-app", name: "web-app", model: modelSonnet, kind: "interactive",
			comm: "2.1.270", argv: claudeArgv, pid: 51877,
			busyFrac: 0.58, segSec: 9, phase: 5,
			r:      rates{in: 50, out: 66, cacheRead: 3800, cacheCreate: 180},
			base:   domain.Usage{Input: 52_000, Output: 88_000, CacheRead: 7_400_000, CacheCreate5m: 330_000},
			ctxMax: 1_000_000, ctx: 218_000, ctxGrowth: 120,
			rssMB: 540, ageMin: 83,
			tools: claudeTools, baseTools: 112,
		},
		{
			agent: "claude", id: "2d8b6f41-9e0c-4a73-b5d2-7c1e8a3f9d05",
			cwd: "~/code/infra", name: "infra", model: modelOpus, kind: "interactive",
			comm: "2.1.270", argv: claudeArgv, pid: 39502,
			busyFrac: 0.38, segSec: 14, phase: 9,
			r:      rates{in: 26, out: 38, cacheRead: 2300, cacheCreate: 90},
			base:   domain.Usage{Input: 21_000, Output: 34_000, CacheRead: 3_100_000, CacheCreate5m: 150_000},
			ctxMax: 1_000_000, ctx: 156_000, ctxGrowth: 70,
			rssMB: 455, ageMin: 128,
			tools: claudeTools, baseTools: 41,
		},
		{
			agent: "claude", id: "9a1f7c3e-5b2d-4e86-a0c4-3d6e9b2f1a78",
			cwd: "~/code/mobile-app", name: "mobile-app", model: modelSonnet, kind: "interactive",
			comm: "2.1.270", argv: claudeArgv, pid: 55310,
			busyFrac: 0.46, segSec: 8, phase: 1,
			r:      rates{in: 40, out: 54, cacheRead: 3000, cacheCreate: 150},
			base:   domain.Usage{Input: 17_000, Output: 26_000, CacheRead: 2_300_000, CacheCreate5m: 120_000},
			ctxMax: 200_000, ctx: 96_000, ctxGrowth: 110,
			rssMB: 498, ageMin: 22,
			tools: claudeTools, baseTools: 29,
		},
		{
			agent: "codex", id: "019a4c2e-7b3d-7f10-9e8a-4c5d6b7e8f90",
			cwd: "~/code/data-pipeline", model: modelCodex, kind: "exec",
			comm: "codex", argv: []string{"codex"}, pid: 60144,
			busyFrac: 0.74, segSec: 12, phase: 4,
			r:      rates{in: 3600, out: 40, cachedFrac: 0.9},
			base:   domain.Usage{Input: 4_100_000, Output: 58_000, CachedInput: 3_560_000},
			ctxMax: 272_000, ctx: 118_000, ctxGrowth: 60, exact: true,
			rssMB: 186, ageMin: 36,
			tools: codexTools, rateLimits: true,
			subs: []subSpec{
				{
					id: "019a4c31-2e6f-7a44-b1d8-9f0e3a5c7b21", agentType: "reviewer",
					desc: "Hopper etl/loaders", model: modelCodex, mode: subToggle, depth: 1, startedMin: 3.8,
					r:    rates{in: 3100, out: 30, cachedFrac: 0.85},
					base: domain.Usage{Input: 640_000, Output: 9_100, CachedInput: 540_000},
					ctx:  64_000, tools: codexTools,
				},
			},
		},
		{
			agent: "codex", id: "019a4b97-1c5e-7d28-8f3a-6b2c4d9e0a15",
			cwd: "~/code/cli-tool", model: modelCodex, kind: "exec",
			comm: "codex", argv: []string{"codex"}, pid: 61022,
			busyFrac: 0.34, segSec: 10, phase: 7,
			r:      rates{in: 2800, out: 32, cachedFrac: 0.9},
			base:   domain.Usage{Input: 1_900_000, Output: 27_000, CachedInput: 1_700_000},
			ctxMax: 272_000, ctx: 71_000, ctxGrowth: 50, exact: true,
			rssMB: 142, ageMin: 58,
			tools: codexTools, rateLimits: true,
		},
	}
}
