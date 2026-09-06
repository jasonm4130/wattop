# Publish a release

Releases build on GitHub's Apple Silicon runner and publish macOS arm64 archives,
checksums, and build provenance. The workflow uses `GITHUB_TOKEN`; no Homebrew
tap token or paid service is required.

Refresh `THIRD_PARTY_LICENSES` when dependencies change. It contains license
texts from the module cache; check every module linked by `go list -deps ./cmd/wattop`.

1. Merge the release changes into `main` after CI passes.
2. Pull `main` locally with `git pull --ff-only`.
3. Run `goreleaser check`.
4. Run `goreleaser release --snapshot --clean --skip=publish` on Apple Silicon.
5. Run the generated binary with `--version`; GoReleaser prints its path.
6. Create an annotated version tag, for example `git tag -a v0.1.0 -m 'wattop v0.1.0'`.
7. Push that tag with `git push origin v0.1.0`.
8. Check the release workflow and the published assets.

Use a prerelease tag such as `v0.1.0-rc.1` for a release candidate. Never move a
published tag; fix a broken release with a new version.

Users can verify downloaded files with `shasum -a 256 -c checksums.txt` when all
listed files are present. Verify provenance with
`gh attestation verify wattop_VERSION_darwin_arm64.tar.gz --repo jasonm4130/wattop`.
Checksums and provenance do not replace Apple signing or notarization; the
current archives are unsigned. Source builds are the documented installation path.

Homebrew distribution can be added when a maintained tap and its scoped
credentials exist. Do not advertise an install command before testing it.
