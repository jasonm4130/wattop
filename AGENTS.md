# wattop

A Go terminal monitor for Apple Silicon power and Claude Code / Codex sessions, plus the Astro docs site in `site/`. [CONTRIBUTING.md](CONTRIBUTING.md) covers layout, local setup and style; this file adds what an agent needs on top.

- Work in a git worktree under `.claude/worktrees/`; Jason commits in the main checkout at the same time.
- `wattop --json` (`internal/domain` Snapshot) is a public NDJSON format. Field names and string values must not change; typed enums must marshal to the same strings. Compare key paths against a `main` build when touching domain types.
- `internal/soc/mactop` is copied verbatim from upstream; never edit it. `make vendor-diff` checks it, and gofmt in CI skips it.
- Fixtures and screenshots are synthetic. Never commit a real transcript, prompt or session log; real captures go through `cmd/wattop-scrub` (see [testdata/README.md](testdata/README.md)).
- Before a PR: `gofmt -l` (outside mactop) prints nothing, `go vet ./...`, `CGO_ENABLED=0 go vet ./...`, `go test -count=1 ./...`, and `go test -race` on `internal/state`, `internal/ui/...` and `cmd/wattop`. For `site/`: `pnpm install --frozen-lockfile && pnpm build && pnpm test`.
- PRs must be up to date with `main` to merge (`gh pr update-branch`). Merge with `gh pr merge --merge`. Merging to `main` deploys the site.
- Comments: keep only what the code can't say (external-dependency behaviour, public API docs, issue links). Code that needs a paragraph to explain should be restructured instead.
- Pricing updates follow [docs/pricing-update.md](docs/pricing-update.md); releases follow [docs/releasing.md](docs/releasing.md).
