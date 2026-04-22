SHELL := /bin/bash
GO ?= go
NPM ?= npm
CENTER_BIN := ./bin/houfeng-center
AGENT_BIN := ./bin/houfeng-agent
GO_PACKAGES := $(shell $(GO) list ./... 2>/dev/null)

.PHONY: fmt-go test-go vet-go build-center build-agent verify-go verify-web verify

fmt-go:
	@if [ -z "$(GO_PACKAGES)" ]; then \
		echo 'no Go packages yet'; \
	else \
		$(GO) fmt $(GO_PACKAGES); \
	fi

test-go:
	@if [ -z "$(GO_PACKAGES)" ]; then \
		echo 'no Go packages yet'; \
	else \
		$(GO) test $(GO_PACKAGES); \
	fi

vet-go:
	@if [ -z "$(GO_PACKAGES)" ]; then \
		echo 'no Go packages yet'; \
	else \
		$(GO) vet $(GO_PACKAGES); \
	fi

build-center:
	@mkdir -p $(dir $(CENTER_BIN))
	$(GO) build -o $(CENTER_BIN) ./cmd/houfeng-center

build-agent:
	@mkdir -p $(dir $(AGENT_BIN))
	$(GO) build -o $(AGENT_BIN) ./cmd/houfeng-agent

verify-go: fmt-go test-go vet-go

verify-web:
	@if [ ! -f web/package.json ]; then \
		echo 'web workspace not initialized yet'; \
	else \
		$(NPM) --prefix web run verify; \
	fi

verify: verify-go verify-web
