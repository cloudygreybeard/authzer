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

.PHONY: all build test test-integration test-all lint clean install snapshot demo-build demo-run demo help

## all: Build the binary (default target)
all: build

## build: Build the binary
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## build-windows: Cross-compile for Windows amd64
build-windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BINARY).exe .

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

CONTAINER_RT ?= podman

## demo-build: Build the demo containers (mock-portal, chromium, authzer-demo)
demo-build:
	$(CONTAINER_RT) build -t mock-portal -f hack/demo/mock-portal/Containerfile hack/demo/mock-portal/
	$(CONTAINER_RT) build -t authzer-chromium -f hack/demo/chromium/Containerfile hack/demo/chromium/
	$(CONTAINER_RT) build -t authzer-demo -f hack/demo/Containerfile .

## demo-run: Run the demo interactively (no recording)
demo-run:
	./hack/record-demo.sh --no-record

## demo: Build containers and record the demo
demo:
	$(MAKE) demo-build
	./hack/record-demo.sh

## help: Show this help
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'
