# authzer Makefile

BINARY := authzer
PREFIX ?= /usr/local
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X github.com/cloudygreybeard/authzer/cmd.Version=$(VERSION) \
	-X github.com/cloudygreybeard/authzer/cmd.Commit=$(COMMIT) \
	-X github.com/cloudygreybeard/authzer/cmd.Date=$(DATE)

.PHONY: all build test test-integration test-all lint clean install snapshot help

## all: Build the binary (default target)
all: build

## build: Build the binary
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## test: Run unit tests
test:
	go test -v -race ./...

## test-integration: Run integration tests (requires Chrome with CDP)
test-integration:
	go test -v -race -tags integration -timeout 90s ./...

## test-all: Run all tests (unit + integration)
test-all: test test-integration

## lint: Run linter
lint:
	golangci-lint run

## clean: Remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf dist/

## install: Install to PREFIX/bin
install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

## snapshot: Build a snapshot release (no publish)
snapshot:
	goreleaser release --snapshot --clean

## deps: Download dependencies
deps:
	go mod download
	go mod tidy

## help: Show this help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'
