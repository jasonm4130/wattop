# Publish a release

Releases build on GitHub's Apple Silicon runner and publish macOS arm64 archives,
checksums, and build provenance. The Homebrew tap verifies and installs each
stable release before publishing its cask. Both repositories use their own
`GITHUB_TOKEN`; no cross-repository credential is required.

Refresh `THIRD_PARTY_LICENSES` when dependencies change. Check every module linked
by `go list -deps ./cmd/wattop` against the included license texts.

## Publish the archive

1. Merge release changes into `main` after required checks pass.
2. Switch to `main` and run `git pull --ff-only`.
3. Run `goreleaser check`.
4. Run `goreleaser release --snapshot --clean --skip=publish` on Apple Silicon.
5. Run the generated binary with `--version`; GoReleaser prints its path.
6. Create an annotated tag, replacing the example version: `git tag -a v0.2.0 -m 'wattop v0.2.0'`.
7. Push that tag with `git push origin v0.2.0`.
8. Wait for the [release workflow](https://github.com/jasonm4130/wattop/actions/workflows/release.yml) to succeed.
9. Review the published assets and edit the release notes.

The workflow rejects commits outside `main`, runs tests, packages the binary,
and attests the archive. Builds target macOS 14; this does not establish hardware
compatibility on every older chip or OS release.

Use a prerelease tag such as `v0.2.0-rc.1` for a release candidate. The tap excludes
prereleases. Never move a published tag; fix a broken release with a new version.

## Update Homebrew

1. Run `gh workflow run update.yml --repo jasonm4130/homebrew-wattop` after the release workflow succeeds.
2. Wait for the [tap updater](https://github.com/jasonm4130/homebrew-wattop/actions/workflows/update.yml) to succeed.
3. Run `brew update`.
4. Install with `brew install --cask jasonm4130/wattop/wattop`, or upgrade with `brew upgrade --cask wattop`.
5. Run `wattop --version` and confirm the published version.

The tap also checks hourly. It verifies SHA256 and provenance from the release
workflow at the matching tag, installs the candidate, and executes `--version`
before committing the cask. A failed check leaves the published cask unchanged.
GitHub may delay scheduled runs or disable them after 60 days without repository
activity. Re-enable and manually run the updater if needed.

## Verify a download

Download the arm64 archive and `checksums.txt` from the same release.
Replace `VERSION` with its version number:

```sh
shasum -a 256 -c checksums.txt
gh attestation verify wattop_VERSION_darwin_arm64.tar.gz --repo jasonm4130/wattop
```

Checksums and provenance do not replace Apple notarization. The current binary
is not Apple-notarized. Source builds remain available through the README.
