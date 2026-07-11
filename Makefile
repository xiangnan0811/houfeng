SHELL := /bin/bash
GO ?= go
NPM ?= npm
CENTER_BIN := ./bin/houfeng-center
AGENT_BIN := ./bin/houfeng-agent
GO_PACKAGES := $(shell $(GO) list ./... 2>/dev/null)
GO_VERIFY_PATTERNS := ./agent/... ./cmd/... ./db/... ./internal/...
VERSION ?= dev
AGENT_RELEASE_DIR ?= ./dist
AGENT_RELEASE_AMD64 := $(AGENT_RELEASE_DIR)/houfeng-agent_$(VERSION)_linux_amd64
AGENT_RELEASE_ARM64 := $(AGENT_RELEASE_DIR)/houfeng-agent_$(VERSION)_linux_arm64

.PHONY: fmt-go test-go vet-go build-center build-agent build-agent-release verify-go test-web-toolchain verify-web verify

fmt-go:
	@if ! command -v $(GO) >/dev/null 2>&1; then \
		echo '$(GO) not found' >&2; \
		exit 1; \
	fi; \
	packages="$$($(GO) list $(GO_VERIFY_PATTERNS))" || exit $$?; \
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
	packages="$$($(GO) list $(GO_VERIFY_PATTERNS))" || exit $$?; \
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
	packages="$$($(GO) list $(GO_VERIFY_PATTERNS))" || exit $$?; \
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
		$(GO) build -ldflags "-X main.version=$(VERSION)" -o $(CENTER_BIN) ./cmd/houfeng-center; \
	fi

build-agent:
	@if ! find cmd/houfeng-agent -maxdepth 1 -type f -name '*.go' | grep -q .; then \
		echo 'houfeng-agent not implemented yet'; \
	else \
		mkdir -p $(dir $(AGENT_BIN)); \
		$(GO) build -o $(AGENT_BIN) ./cmd/houfeng-agent; \
	fi

build-agent-release:
	@if [ "$(VERSION)" = "dev" ] || [ -z "$(VERSION)" ]; then \
		echo 'VERSION must be set to a release tag, for example: make build-agent-release VERSION=v1.2.3' >&2; \
		exit 1; \
	fi
	@if ! find cmd/houfeng-agent -maxdepth 1 -type f -name '*.go' | grep -q .; then \
		echo 'houfeng-agent not implemented yet'; \
	else \
		mkdir -p $(AGENT_RELEASE_DIR); \
		build_dir="$$(mktemp -d)"; \
		trap 'rm -rf "$$build_dir"' EXIT; \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X houfeng/agent/runtime.agentVersion=$(VERSION)" -o "$$build_dir/houfeng-agent_$(VERSION)_linux_amd64" ./cmd/houfeng-agent; \
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X houfeng/agent/runtime.agentVersion=$(VERSION)" -o "$$build_dir/houfeng-agent_$(VERSION)_linux_arm64" ./cmd/houfeng-agent; \
		cp "$$build_dir/houfeng-agent_$(VERSION)_linux_amd64" $(AGENT_RELEASE_AMD64); \
		cp "$$build_dir/houfeng-agent_$(VERSION)_linux_arm64" $(AGENT_RELEASE_ARM64); \
		cd $(AGENT_RELEASE_DIR) && \
			if command -v sha256sum >/dev/null 2>&1; then \
				sha256sum houfeng-agent_$(VERSION)_linux_amd64 houfeng-agent_$(VERSION)_linux_arm64 > sha256sums.txt; \
			else \
				shasum -a 256 houfeng-agent_$(VERSION)_linux_amd64 houfeng-agent_$(VERSION)_linux_arm64 > sha256sums.txt; \
			fi; \
	fi

verify-go: fmt-go vet-go test-go

test-web-toolchain:
	@scripts/check-web-toolchain.test.sh
	@scripts/check-web-quality-gates.test.sh

verify-web: test-web-toolchain
	@scripts/check-web-toolchain.sh
	@if [ -f web/package.json ]; then \
		env -u NODE_ENV $(NPM) --prefix web ci --include=dev && \
		NODE_ENV=test $(NPM) --prefix web run lint && \
		NODE_ENV=test $(NPM) --prefix web run test:coverage && \
		NODE_ENV=production $(NPM) --prefix web run build && \
		$(NPM) --prefix web run bundle:check && \
		$(NPM) --prefix web run css:analyze; \
	else \
		echo 'web workspace not initialized yet'; \
	fi

verify:
	./scripts/verify.sh
