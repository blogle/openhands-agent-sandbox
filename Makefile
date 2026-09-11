.PHONY: build test test-race lint vet clean build-image test-live test-openhands-live test-warm-live

# Build variables
BINARY_NAME := runtime-api
BUILD_DIR := bin
IMAGE_NAME := openhands-agent-sandbox-runtime
IMAGE_TAG := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Go variables
GO := go
GOFLAGS := -trimpath
LDFLAGS := -s -w

.PHONY: all
all: lint test build

## build: Build the binary
build:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/runtime-api

## test: Run unit tests
test:
	$(GO) test ./...

## test-race: Run tests with race detector
test-race:
	$(GO) test -race ./...

## lint: Run golangci-lint
lint:
	golangci-lint run

## vet: Run go vet
vet:
	$(GO) vet ./...

## clean: Remove build artifacts
clean:
	rm -rf $(BUILD_DIR)

## build-image: Build container image
build-image:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

## test-live: Run optional live E2E tests against an existing cluster
test-live:
	@if [ "$(RUN_LIVE_E2E)" != "1" ]; then \
		echo "Set RUN_LIVE_E2E=1 to run live tests"; \
		exit 1; \
	fi
	$(GO) test -tags live -v -count=1 -timeout 10m ./... -run TestLive

## test-openhands-live: Run OpenHands APIRemoteWorkspace compatibility test
test-openhands-live:
	@if [ "$(RUN_LIVE_E2E)" != "1" ]; then \
		echo "Set RUN_LIVE_E2E=1 to run live tests"; \
		exit 1; \
	fi
	$(GO) test -tags live -v -count=1 -timeout 10m ./... -run TestOpenHandsLive

## test-warm-live: Run warm pool live test
test-warm-live:
	@if [ "$(RUN_LIVE_E2E)" != "1" ]; then \
		echo "Set RUN_LIVE_E2E=1 to run live tests"; \
		exit 1; \
	fi
	$(GO) test -tags live -v -count=1 -timeout 10m ./... -run TestWarmPoolLive

## fmt: Format code
fmt:
	gofmt -s -w .

## tidy: Run go mod tidy
tidy:
	$(GO) mod tidy

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'
