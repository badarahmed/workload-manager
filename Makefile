.DEFAULT_GOAL := build

GO ?= go
BIN_DIR ?= .local/bin
GOCACHE ?= $(CURDIR)/.local/go-cache
CGO_ENABLED ?= 0
export GOCACHE CGO_ENABLED

.PHONY: build build-wm build-provider build-mac test-go

# Build for the current platform, or supply GOOS and GOARCH explicitly.
build: build-wm build-provider

build-wm:
	@mkdir -p "$(BIN_DIR)"
	$(GO) build -o "$(BIN_DIR)/wm" ./cmd/wm

build-provider:
	@mkdir -p "$(BIN_DIR)"
	$(GO) build -o "$(BIN_DIR)/terraform-provider-wm" ./cmd/terraform-provider-wm

# Keep cross-build output separate from binaries used by the local operator.
build-mac:
	$(MAKE) build GOOS=darwin GOARCH=arm64 BIN_DIR="$(BIN_DIR)/darwin-arm64"

test-go:
	$(GO) test ./...
