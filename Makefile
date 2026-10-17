SHELL := /bin/bash
GO ?= go
NPM ?= npm
CENTER_BIN := ./bin/houfeng-center
AGENT_BIN := ./bin/houfeng-agent
GO_VERIFY_PATTERNS := ./agent/... ./cmd/... ./db/... ./internal/...
VERSION ?= dev
AGENT_RELEASE_DIR ?= ./dist
AGENT_RELEASE_AMD64 := $(AGENT_RELEASE_DIR)/houfeng-agent_$(VERSION)_linux_amd64
AGENT_RELEASE_ARM64 := $(AGENT_RELEASE_DIR)/houfeng-agent_$(VERSION)_linux_arm64
AGENT_RELEASE_SOURCE := $(AGENT_RELEASE_DIR)/houfeng-agent_$(VERSION)_source.tar.gz
AGENT_RELEASE_LICENSES := $(AGENT_RELEASE_DIR)/houfeng-agent_$(VERSION)_licenses.txt

.PHONY: fmt-go check-fmt-go test-go vet-go build-center build-agent build-agent-release verify-go test-web-toolchain test-ci-tools verify-web verify-docs verify verify-ci

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

check-fmt-go:
	@set -euo pipefail; \
	if ! command -v $(GO) >/dev/null 2>&1; then \
		echo '$(GO) not found' >&2; \
		exit 1; \
	fi; \
	if ! command -v gofmt >/dev/null 2>&1; then \
		echo 'gofmt not found' >&2; \
		exit 1; \
	fi; \
	inputs="$$($(GO) list -f '{{range .GoFiles}}{{printf "%s/%s\n" $$.Dir .}}{{end}}{{range .CgoFiles}}{{printf "%s/%s\n" $$.Dir .}}{{end}}{{range .TestGoFiles}}{{printf "%s/%s\n" $$.Dir .}}{{end}}{{range .XTestGoFiles}}{{printf "%s/%s\n" $$.Dir .}}{{end}}' $(GO_VERIFY_PATTERNS))"; \
	if [ -z "$$inputs" ]; then \
		echo 'no Go packages yet'; \
		exit 0; \
	fi; \
	files=(); \
	while IFS= read -r file; do \
		if [ -n "$$file" ]; then files+=("$$file"); fi; \
	done <<< "$$inputs"; \
	unformatted="$$(gofmt -l "$${files[@]}")"; \
	if [ -n "$$unformatted" ]; then \
		printf 'Go files need formatting; run make fmt-go:\n%s\n' "$$unformatted" >&2; \
		exit 1; \
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
		$(GO) test -timeout=30m $$packages; \
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
	@set -euo pipefail; \
	untracked="$$(git ls-files --others --exclude-standard)"; \
	if ! git diff --quiet -- || ! git diff --cached --quiet HEAD -- || [ -n "$$untracked" ]; then \
		echo 'agent release requires a clean tracked tree/index and no untracked non-ignored files; binary and corresponding source must match HEAD' >&2; \
		exit 1; \
	fi; \
	if ! find cmd/houfeng-agent -maxdepth 1 -type f -name '*.go' | grep -q .; then \
		echo 'houfeng-agent not implemented yet'; \
	else \
		mkdir -p $(AGENT_RELEASE_DIR); \
		build_dir="$$(mktemp -d)"; \
		trap 'rm -rf "$$build_dir"' EXIT; \
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "-s -w -X houfeng/agent/runtime.agentVersion=$(VERSION)" -o "$$build_dir/houfeng-agent_$(VERSION)_linux_amd64" ./cmd/houfeng-agent; \
		CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags "-s -w -X houfeng/agent/runtime.agentVersion=$(VERSION)" -o "$$build_dir/houfeng-agent_$(VERSION)_linux_arm64" ./cmd/houfeng-agent; \
		git archive --format=tar --prefix=houfeng-agent_$(VERSION)_source/ HEAD | gzip -n > "$$build_dir/houfeng-agent_$(VERSION)_source.tar.gz"; \
		cp "$$build_dir/houfeng-agent_$(VERSION)_linux_amd64" $(AGENT_RELEASE_AMD64); \
		cp "$$build_dir/houfeng-agent_$(VERSION)_linux_arm64" $(AGENT_RELEASE_ARM64); \
		cp "$$build_dir/houfeng-agent_$(VERSION)_source.tar.gz" $(AGENT_RELEASE_SOURCE); \
		cp web/public/licenses.txt $(AGENT_RELEASE_LICENSES); \
		cd $(AGENT_RELEASE_DIR) && \
			if command -v sha256sum >/dev/null 2>&1; then \
				sha256sum houfeng-agent_$(VERSION)_linux_amd64 houfeng-agent_$(VERSION)_linux_arm64 houfeng-agent_$(VERSION)_source.tar.gz houfeng-agent_$(VERSION)_licenses.txt > sha256sums.txt; \
			else \
				shasum -a 256 houfeng-agent_$(VERSION)_linux_amd64 houfeng-agent_$(VERSION)_linux_arm64 houfeng-agent_$(VERSION)_source.tar.gz houfeng-agent_$(VERSION)_licenses.txt > sha256sums.txt; \
			fi; \
	fi

verify-go: check-fmt-go vet-go test-go

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
		echo 'web/package.json is required for verify-web' >&2; \
		exit 1; \
	fi

verify-docs:
	node --test scripts/verify-docs.test.mjs
	node scripts/verify-docs.mjs

verify:
	./scripts/verify.sh

test-ci-tools:
	node --test scripts/ci-scope.test.mjs scripts/verify-ci.test.mjs scripts/verify-candidate.test.mjs scripts/check-ci-results.test.mjs

verify-ci:
	node scripts/verify-ci.mjs
