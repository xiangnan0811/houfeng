SHELL := /bin/bash
GO ?= go
NPM ?= npm
CENTER_BIN := ./bin/houfeng-center
AGENT_BIN := ./bin/houfeng-agent
GO_PACKAGES := $(shell $(GO) list ./... 2>/dev/null)

.PHONY: fmt-go test-go vet-go build-center build-agent verify-go verify-web verify

fmt-go:
	@if ! command -v $(GO) >/dev/null 2>&1; then \
		echo '$(GO) not found' >&2; \
		exit 1; \
	fi; \
	packages="$$($(GO) list ./...)" || exit $$?; \
	if [ -z "$$packages" ]; then \
		echo 'no Go packages yet'; \
	else \
		$(GO) fmt $$packages; \
	fi

test-go:
	@if ! command -v $(GO) >/dev/null 2>&1; then \
		echo '$(GO) not found' >&2; \
		exit 1; \
	fi; \
	packages="$$($(GO) list ./...)" || exit $$?; \
	if [ -z "$$packages" ]; then \
		echo 'no Go packages yet'; \
	else \
		$(GO) test $$packages; \
	fi

vet-go:
	@if ! command -v $(GO) >/dev/null 2>&1; then \
		echo '$(GO) not found' >&2; \
		exit 1; \
	fi; \
	packages="$$($(GO) list ./...)" || exit $$?; \
	if [ -z "$$packages" ]; then \
		echo 'no Go packages yet'; \
	else \
		$(GO) vet $$packages; \
	fi

build-center:
	@if ! find cmd/houfeng-center -maxdepth 1 -type f -name '*.go' | grep -q .; then \
		echo 'houfeng-center not implemented yet'; \
	else \
		mkdir -p $(dir $(CENTER_BIN)); \
		$(GO) build -o $(CENTER_BIN) ./cmd/houfeng-center; \
	fi

build-agent:
	@if ! find cmd/houfeng-agent -maxdepth 1 -type f -name '*.go' | grep -q .; then \
		echo 'houfeng-agent not implemented yet'; \
	else \
		mkdir -p $(dir $(AGENT_BIN)); \
		$(GO) build -o $(AGENT_BIN) ./cmd/houfeng-agent; \
	fi

verify-go: fmt-go test-go vet-go

verify-web:
	@if [ ! -f web/package.json ]; then \
		echo 'web workspace not initialized yet'; \
	else \
		$(NPM) --prefix web run verify; \
	fi

verify: verify-go verify-web
