---
title: Install
description: Install wattop with Homebrew, go install, or from source. Apple Silicon and macOS 14+ only.
---

wattop needs an **Apple Silicon (arm64) Mac on macOS 14 or newer**. There is no Intel or Linux build.

## Homebrew

```sh
brew install --cask jasonm4130/wattop/wattop
```

Update with:

```sh
brew update && brew upgrade --cask wattop
```

The [official tap](https://github.com/jasonm4130/homebrew-wattop) tracks stable releases. The cask removes quarantine from its installed `wattop` binary so it can launch.

## Release archive

Download the arm64 archive from [GitHub Releases](https://github.com/jasonm4130/wattop/releases/latest). Release archives include checksums and GitHub build provenance; they are **not Apple-notarized**. See the [verification instructions](https://github.com/jasonm4130/wattop/blob/main/docs/releasing.md#verify-a-download).

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

The build enables CGO for IOReport and SMC. Tagged releases are built by GitHub Actions with a macOS 14 deployment target.

## First run

```sh
wattop doctor   # which SoC channels, agent sources, pricing and theme resolve here
wattop          # the dashboard
```

wattop has been built and tested on one machine so far, an M5 Max. Other Apple Silicon chips should work; [`wattop doctor`](/docs/doctor/) tells you what resolved on yours.
