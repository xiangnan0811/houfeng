# Narrow implementation and check context

## Source anchors

- compose.yaml:139-219 owns the Center service and its private/default plus proxy networks.
- compose.yaml:281-284 owns the current unconditional external proxy network.
- internal/center/deploy/production_compose_static_test.go:224-280 freezes portable data, no host port, only-Center proxy membership, and required external network.
- internal/center/deploy/production_compose_static_test.go:410-471 freezes env template sections and required variables.
- internal/center/deploy/production_compose_static_test.go:527-666 freezes quick-start and release staging/upload/public readback.
- .github/workflows/publish-images.yml:357-463 stages, validates, uploads, reads back, and reports deployment assets.
- .trellis/spec/backend/directory-structure.md:334-414 is the executable production Compose scenario.

## Target implementation invariants

- compose.yaml remains the only full eight-service graph. It has no proxy external network and no published Center port; Center explicitly retains the default network.
- compose.proxy-network.yaml adds only Center membership in the existing NPM network, stable alias houfeng, and the required HOUFENG_PROXY_NETWORK external-network name.
- compose.proxy-host.yaml adds only one long-syntax TCP mapping with host_ip 127.0.0.1, published 16001, and target 16001. It contains no external network or HOUFENG_PROXY_NETWORK.
- compose.env.example selects compose.yaml:compose.proxy-network.yaml by default through COMPOSE_FILE. Host users select compose.yaml:compose.proxy-host.yaml and leave HOUFENG_PROXY_NETWORK blank.
- Host-proxy support requires Docker Engine 28.0.0 or newer. Do not add an old-Engine firewall workaround, configurable bind address, or public host port.
- Shared-network users discover and reuse NPM's existing network. Houfeng joins NPM; documentation must not require NPM to create or join a Houfeng-owned network.
- PostgreSQL, ClamAV, Records authority, processor, initializer, secrets, mounts, health, and coordinated recovery behavior remain unchanged.
- Release automation owns four exact public deployment assets and validates both merged modes before upload, then verifies exact-name cardinality and byte identity without rejecting unrelated release assets.

## Required verification

- Focused RED/GREEN tests in internal/center/deploy.
- Full go test ./internal/center/deploy and go test ./cmd/houfeng-record-platform-admin.
- Shared-network and host-proxy docker compose config renders using complete test-only environment values.
- make verify-go.
- GitHub Actions YAML parse, changed shell syntax, actionlint when installed, and git diff --check.
- Independent Trellis check with Critical 0 and Important 0.
- PR required checks, protected merge, post-merge automation, and public release verification for all four assets and both rendered modes.
