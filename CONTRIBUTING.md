# Contributing to wattop

wattop welcomes fixes, tests, documentation, and focused improvements to its
Apple Silicon dashboard. Open an issue before a large change so the maintainer
can discuss scope before you invest time.

## Run locally

Use Go 1.27 and Xcode command line tools on an Apple Silicon Mac.

```sh
git clone https://github.com/jasonm4130/wattop.git
cd wattop
make build
./bin/wattop
make test
make lint
go test -race ./...
```

Linux contributors can run portable tests with `CGO_ENABLED=0 go test ./...`.
Hardware collectors use stubs there; Linux tests do not validate macOS sampling.
Run `make test-hw` only on a compatible Mac when changing hardware collection.

## Make a change

Keep each pull request focused. Match the surrounding Go style and format changed
Go files with `gofmt`. Add a regression test for a bug when practical. Explain
what you tested and anything your machine could not verify.

The agent parsers live in `internal/agent`, shared data in `internal/domain`,
aggregation in `internal/state`, and rendering in `internal/ui`. Hardware code
lives in `internal/soc`; read its `VENDOR.md` before changing copied collectors.
See [testdata/README.md](testdata/README.md) for fixture provenance and privacy rules.
Use synthetic transcripts and screenshots; never commit real prompts or session logs.

CI checks portable tests, native macOS builds, race detection, formatting,
workflow syntax, and CodeQL analysis. A maintainer merges after checks pass.
AI-assisted contributions follow the same review and testing standards.

## Community and licensing

Follow the [code of conduct](CODE_OF_CONDUCT.md). Contributions are provided
under the project's [MIT license](LICENSE). Keep upstream license notices when
copying code. Report vulnerabilities through [private security reporting](SECURITY.md).
