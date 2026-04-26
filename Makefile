# authzer Makefile

BINARY  := authzer
PREFIX  ?= /usr/local
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

EXE :=
ifeq ($(GOOS),windows)
EXE := .exe
endif

OUTBIN  := bin/$(BINARY)_$(GOOS)_$(GOARCH)$(EXE)
LINKBIN := bin/$(BINARY)$(EXE)

LDFLAGS := -s -w \
	-X github.com/cloudygreybeard/authzer/cmd.Version=$(VERSION) \
	-X github.com/cloudygreybeard/authzer/cmd.Commit=$(COMMIT) \
	-X github.com/cloudygreybeard/authzer/cmd.Date=$(DATE)

.PHONY: all build test test-integration test-all test-container lint clean install snapshot deps demo-build demo-run demo help

## all: Build the binary (default target)
all: build

## build: Build bin/authzer_GOOS_GOARCH and symlink bin/authzer
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(OUTBIN) .
	ln -sf $(notdir $(OUTBIN)) $(LINKBIN)

## build-linux-amd64: Cross-compile for Linux amd64
build-linux-amd64:
	$(MAKE) build GOOS=linux GOARCH=amd64

## build-linux-arm64: Cross-compile for Linux arm64
build-linux-arm64:
	$(MAKE) build GOOS=linux GOARCH=arm64

## build-darwin-amd64: Cross-compile for macOS amd64
build-darwin-amd64:
	$(MAKE) build GOOS=darwin GOARCH=amd64

## build-darwin-arm64: Cross-compile for macOS arm64
build-darwin-arm64:
	$(MAKE) build GOOS=darwin GOARCH=arm64

## build-windows-amd64: Cross-compile for Windows amd64
build-windows-amd64:
	$(MAKE) build GOOS=windows GOARCH=amd64

## build-windows-arm64: Cross-compile for Windows arm64
build-windows-arm64:
	$(MAKE) build GOOS=windows GOARCH=arm64

## test: Run unit tests
test:
	go test -v -race ./...

## test-integration: Run integration tests (requires Chrome with CDP)
test-integration:
	go test -v -race -tags integration -timeout 90s ./...

## test-all: Run all tests (unit + integration)
test-all: test test-integration

## test-container: Build and run all tests inside a container (Go 1.26 + Chromium)
test-container:
	$(CONTAINER_RT) build -f hack/Containerfile.test -t authzer-test .
	$(CONTAINER_RT) run --rm authzer-test

## lint: Run linter
lint:
	golangci-lint run

## clean: Remove build artifacts
clean:
	rm -rf bin/ dist/

## install: Install to PREFIX/bin
install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 755 $(OUTBIN) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

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
	$(CONTAINER_RT) build -t mock-portal -f hack/mock-portal/Containerfile hack/mock-portal/
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
