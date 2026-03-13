APP_NAME := zenssh
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -s -w \
	-X zenssh/internal/version.Version=$(VERSION) \
	-X zenssh/internal/version.Commit=$(COMMIT) \
	-X zenssh/internal/version.Date=$(DATE)

.PHONY: build test clean release

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(APP_NAME) ./cmd/zenssh

test:
	go test ./...

release:
	./scripts/package-release.sh "$(VERSION)"

clean:
	rm -rf bin dist
