VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/jasonm4130/wattop/internal/version.Version=$(VERSION) \
           -X github.com/jasonm4130/wattop/internal/version.Commit=$(COMMIT) \
           -X github.com/jasonm4130/wattop/internal/version.BuildDate=$(DATE)

.PHONY: test test-hw build lint clean fixtures pricing vendor-diff

test:
	go test ./...

test-hw:
	go test -tags=hardware ./...

build:
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/wattop ./cmd/wattop

lint:
	go vet ./...

clean:
	rm -rf bin dist

fixtures:
	./scripts/fixtures.sh

pricing:
	./scripts/pricing.sh

vendor-diff:
	./scripts/vendor-diff.sh
