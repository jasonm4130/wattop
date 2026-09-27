---
title: Install
description: Install wattop with Homebrew, go install, or from source. Apple Silicon and macOS 14+ only.
---

## Requirements

- An **Apple Silicon (arm64) Mac on macOS 14 or newer**. wattop links Apple's private IOReport framework through CGO, so there is no Intel or Linux build.
- Nothing else to install: no daemons, no root, no account. wattop is one binary that uses macOS system frameworks only, and it never asks for `sudo`.

## Homebrew

```sh
brew install --cask jasonm4130/wattop/wattop
```

Update with:

```sh
brew update && brew upgrade --cask wattop
```

The [official tap](https://github.com/jasonm4130/homebrew-wattop) tracks stable releases; it excludes prereleases. Before publishing a cask, the tap verifies the release archive's SHA-256 and its GitHub build provenance, installs it, and runs `wattop --version`.

:::note[Not Apple-notarized]
wattop release binaries are not Apple-notarized. The cask's `postflight` step runs `/usr/bin/xattr -dr com.apple.quarantine` on its installed `wattop` binary, and only that binary, so macOS Gatekeeper lets it launch. Checksums and provenance do not replace notarization.
:::

## Release archive

Download the `darwin_arm64` archive and `checksums.txt` from the same [GitHub release](https://github.com/jasonm4130/wattop/releases/latest). Release archives include checksums and GitHub build provenance; they are **not Apple-notarized**. To verify a download, replace `VERSION` with its version number:

```sh
shasum -a 256 -c checksums.txt
gh attestation verify \
  wattop_VERSION_darwin_arm64.tar.gz \
  --repo jasonm4130/wattop
```

A binary extracted from the archive by hand keeps the quarantine attribute your browser set, so Gatekeeper may block it on first launch. See [Troubleshooting](/docs/troubleshooting/#gatekeeper-blocks-the-binary).

## go install

With Go 1.27 and the Xcode command line tools:

```sh
go install github.com/jasonm4130/wattop/cmd/wattop@latest
```

## Build from source

```sh
git clone https://github.com/jasonm4130/wattop.git
cd wattop
make build
./bin/wattop
```

The build enables CGO for IOReport and SMC. `CGO_ENABLED=1 GOOS=darwin GOARCH=arm64` on an Apple Silicon host is the only supported build; there is no cross-compilation from an Intel host. Tagged releases are built by GitHub Actions with a macOS 14 deployment target.

The rest of these docs assume `wattop` is on your `PATH`; otherwise use `./bin/wattop`.

## Try it without any sessions

```sh
wattop --demo
```

`--demo` runs a synthetic machine and synthetic Claude and Codex sessions. It reads no local data, ignores `config.toml`, and makes no network request, so you can see the whole dashboard before you have an agent running.

## Check what resolved

```sh
wattop doctor
```

[`wattop doctor`](/docs/doctor/) prints which SoC channels, agent sources, pricing and theme resolve on this machine, without entering the TUI. Run it first on a new machine or after a macOS upgrade.

wattop has been built and tested on one machine so far, an M5 Max. Other Apple Silicon chips should work, but that has not been observed yet; see [limitations](/docs/limitations/#untested-outside-this-machine).
