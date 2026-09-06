//go:build !(darwin && arm64 && cgo)

// Package mactop vendors mactop's collector layer (see VENDOR.md). This file
// exists solely so the package is non-empty on platforms/build modes where
// the real, CGO-backed collectors in this directory are excluded by their
// own build constraints -- without it "go build ./internal/soc/mactop/"
// fails with "build constraints exclude all Go files" on Linux and CGO_ENABLED=0.
package mactop
