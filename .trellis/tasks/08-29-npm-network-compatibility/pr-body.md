## Summary

- Split the production deployment into one common eight-service Compose graph plus exactly one thin proxy overlay.
- Keep shared-network mode as the default: Houfeng joins the existing Nginx Proxy Manager network and NPM forwards to `houfeng:16001`.
- Add host-proxy mode for an existing `network_mode: host` NPM: NPM remains unchanged and forwards to the fixed `127.0.0.1:16001` mapping. This mode requires Docker Engine 28.0.0 or newer.
- Publish, validate, and publicly read back all four deployment assets. Same-version publication is serialized and retries are non-destructive: identical assets are retained, drift fails closed, and only missing assets upload.

## Operator contract

- Default: `COMPOSE_FILE=compose.yaml:compose.proxy-network.yaml`; set `HOUFENG_PROXY_NETWORK` to the exact existing NPM user-defined network.
- Host NPM: `COMPOSE_FILE=compose.yaml:compose.proxy-host.yaml`; leave `HOUFENG_PROXY_NETWORK` blank and configure the upstream as `127.0.0.1:16001`.
- Do not load both overlays, set `HOUFENG_PROXY_NETWORK=host`, invent a placeholder network for host mode, or expose Center on an all-interface/LAN/IPv6 bind.

## Verification

- `GOTOOLCHAIN=go1.26.2 make verify-go`
- `go test ./internal/center/deploy -count=1`
- `go test ./cmd/houfeng-record-platform-admin -count=1`
- Shared-network structured `docker compose config` render: Center on `default` + external proxy network, alias `houfeng`, no published port.
- Host-proxy structured render: Center on `default`, exactly `127.0.0.1:16001 -> 16001/tcp`, no external proxy network.
- Shared-network render with a blank network variable fails with the reviewed mode-specific diagnostic.
- GitHub Actions YAML parse and `bash -n` for all four deployment-assets run blocks.
- `git diff --check` and Trellis task validation.
- `actionlint` was not available locally; GitHub Actions remains the authoritative workflow execution gate.

## Rollback

- Revert this PR and restore the previous matching-tag `compose.yaml` plus `compose.env.example` release assets.
- For a host-proxy deployment, remove `compose.proxy-host.yaml` from `COMPOSE_FILE` before starting the previous release so the loopback mapping is not carried into an unsupported bundle.
- Roll back application/data only through the documented complete cold recovery unit; do not mix PostgreSQL, attachment, or Records-authority recovery points.
