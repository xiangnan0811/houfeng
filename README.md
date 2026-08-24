# 候风 / Houfeng Fleet Control Plane

Houfeng is an early-stage, self-hosted fleet control plane for a single operator. It focuses on monitoring servers and service entrypoints first, then adds a lightweight VPS Asset Ledger so infrastructure inventory and observability evidence can be reviewed in one place.

The repository contains the Go center, Go agent, PostgreSQL schema, React/Vite web UI, a published-image production Docker Compose stack, local/systemd deployment notes, and validation workflows. There are no package-manager repositories, Kubernetes deployment manifests, automatic upgrade service, or completed real-inventory validation claims in this repo.

## Current shape

The supported deployment topology is small and explicit:

```text
operator browser
      |
      v
Nginx Proxy Manager
      |
      v
houfeng-center (Go API + React SPA) ----> PostgreSQL <---- attachment processor
       ^                                  ^                    |
       |                                  |                    +----> ClamAV
Records authority -----------------------+  (signed state in ./data/records-authority)
       |
       +---------------- ./data/attachments ------------------+

houfeng-agent(s) --outbound enroll/sync--> houfeng-center
```

- **Center**: serves the API and built web UI, applies embedded PostgreSQL migrations, manages auth sessions, settings, incidents/events, retention, node onboarding, and Asset Ledger APIs.
- **Attachment processor + ClamAV**: isolates Records content extraction and malware scanning from the HTTP process while sharing only the runtime database role and attachment storage.
- **Records authority**: verifies the signed single-host deployment state outside PostgreSQL and renews the exact Center membership required by fail-closed Records admission.
- **Agent**: runs on monitored hosts, reads a token file, fingerprints the host, samples host/probe/container facts, buffers sync data locally, and initiates all communication to the center.
- **Web UI**: React 19 + Vite SPA for dashboard, nodes, targets, events, settings, onboarding, and asset workflows.
- **Asset Ledger**: manual/API/JSON-import records for providers, VPS assets, subscriptions, VPS-to-Node links, renewal decisions/history, and lightweight service/domain records.

The center does not SSH into agents. Agents do not accept arbitrary scripts or user-supplied shell commands; the current command surface is bounded to compiled-in diagnostic action IDs.

## Quick start for local development

Prerequisites:

- Go toolchain
- Node.js 22 + npm
- PostgreSQL

Build the web UI and center, then run the center with local environment values:

```bash
cd web && npm ci && npm run build
cd ..

export HOUFENG_HTTP_ADDR=:8080
export HOUFENG_WEB_DIST_DIR=web/dist
export HOUFENG_DATABASE_URL='postgres://houfeng:houfeng@localhost:5432/houfeng?sslmode=disable'
export HOUFENG_DATABASE_REQUIRE_TLS=false
export HOUFENG_PUBLIC_BASE_URL='http://localhost:8080'
export HOUFENG_INCIDENT_SWEEP_INTERVAL=5s
export HOUFENG_INITIAL_USERNAME=admin
export HOUFENG_INITIAL_PASSWORD='replace-me-with-a-real-password'
export HOUFENG_SESSION_HMAC_KEY='replace-me-with-32-plus-random-bytes'

make build-center
./bin/houfeng-center
```

Open `http://127.0.0.1:8080/`, log in with the initial credentials, and create a Node from the UI. The default center version is `dev`, so one-command install generation is intentionally blocked until the center is built with a real release tag and matching agent release assets exist.

A published release can run the complete production Compose topology without a
source checkout, local build, helper launcher, or manual SQL step:

```bash
install -d -m 0700 houfeng && cd houfeng
sudo install -d -o 10001 -g 10001 -m 0700 optional-secrets optional-secrets/comparison-keyring optional-secrets/s3
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.yaml -o compose.yaml
curl -fL https://github.com/xiangnan0811/houfeng/releases/latest/download/compose.env.example -o .env
chmod 0600 .env
# Edit every value in "Must change"; keep HOUFENG_IMAGE pinned to the downloaded release.
${EDITOR:-vi} .env
docker compose config
docker compose pull
docker compose up -d
```

The optional-secret directories are owned by the image's non-root UID/GID so
Center and the processor can traverse their scoped read-only bind mounts. Install
any optional key file with `sudo install -o 10001 -g 10001 -m 0400 SOURCE DEST`;
do not loosen the directory modes to make a host-side copy succeed.

Before `up`, create or select the existing Docker network named by
`HOUFENG_PROXY_NETWORK`; Nginx Proxy Manager must already join that network.
Create an NPM Proxy Host with scheme `http`, forward hostname `houfeng`, forward
port `16001`, Websockets Support and Block Common Exploits enabled, a valid SSL
certificate, and Force SSL enabled. Houfeng publishes no host port by default.

The stack runs Center with baked Web, the Records attachment processor, ClamAV,
PostgreSQL 16, a long-running Records authority, and bounded storage, secret,
and database initializer services. Center alone joins the NPM network.
Initialization creates private paths below `./data`, stages only the secrets
needed by read-only services, provisions distinct runtime/platform-admin/
migrator/authority database roles, converges the current Records schema,
activates the signed single-host contract through the existing projector, and
proves runtime admission before Center or the processor may start. PostgreSQL,
attachments, logs, ClamAV cache, public Center identity, staged private secret
files, and signed authority state all remain visible below `./data`, including
`./data/records-authority`. Agents remain host-installed Linux/systemd services.
See `docs/deploy/local-and-systemd.md` for coordinated backup, upgrade,
rollback, migration, troubleshooting, and advanced direct deployment guidance.

For a real Linux agent onboarding run, follow `docs/deploy/local-and-systemd.md`. One-command installation depends on a center-generated command, an externally reachable `HOUFENG_PUBLIC_BASE_URL`, and Linux agent release assets built with a real version tag:

```bash
make build-center VERSION=v1.2.3
make build-agent-release VERSION=v1.2.3
```

Upload `dist/houfeng-agent_v1.2.3_linux_amd64`, `dist/houfeng-agent_v1.2.3_linux_arm64`, `dist/sha256sums.txt`, and `dist/sha256sums.txt.minisig` to the matching GitHub Release. The installer script itself is served by the deployed center at `/api/agent/install.sh`; GitHub Release hosts only the binary and signed checksum assets.

## Verification commands

Repository quality gates are exposed through the Makefile:

```bash
make fmt-go
make vet-go
make test-go
make verify-go
make verify-web
./scripts/verify.sh
```

Useful focused commands:

```bash
go test ./internal/center/http/handlers -run TestNodeOnboarding -v
cd web && npx vitest run src/pages/NodesPage.test.tsx
```

## Documentation map

Start with `docs/README.md` for the maintained documentation index.

Primary operator docs:

- `docs/deploy/local-and-systemd.md` — canonical production Compose, advanced local/systemd, and one-command agent install guide.
- `docs/operations/fresh-install-smoke-run.md` — fresh-install smoke run with one-command onboarding as the primary path.
- `docs/operations/ui-preview-and-browser-sanity.md` — UI preview and browser-sanity workflow; screenshots are local/untracked unless explicitly approved as public assets.
- `docs/operations/asset-ledger-real-data-validation-readiness.md` — local sample and real-data validation boundaries for Asset Ledger.

Design/reference docs:

- `docs/design/current/` — maintained product, architecture, interface, and component guidance.
- `docs/design/v1-baseline/` and `docs/design/v2-houfeng/` — historical stubs retained for traceability; full old bundles are available through git history and do not freeze future direction.

Completed roadmap, release-gate, archived visual-history, and one-off evidence logs have been removed from the tracked public docs tree. Durable operator cautions and current constraints are folded into README, `docs/README.md`, deployment guidance, smoke guidance, and current design guidance.

## Current limitations and cautions

- Houfeng is still early-stage and single-operator oriented.
- The documented agent installer supports Linux + systemd + `amd64`/`arm64` only.
- The project provides a single-host production Compose path for Center/Web, Records processing, ClamAV, and PostgreSQL, but does not provide containerized agents, Kubernetes deployment, package repositories, or automatic upgrade UX.
- Production deployments must keep `HOUFENG_PUBLIC_BASE_URL` on HTTPS, reject broad trusted proxy ranges, apply request body/rate/connection limits in Nginx Proxy Manager, and keep the project image pinned to a release tag or digest.
- Agent diagnostic command output is best-effort redacted before upload and again before persistence, but it can still contain operationally sensitive host state. Run agents as the dedicated `houfeng-agent` user and do not add broad host privileges such as Docker group membership unless the risk is accepted.
- Asset Ledger facts are only as true as the manually entered or imported data. Do not claim provider account truth, billing accuracy, exchange-rate truth, or completed real-inventory validation unless the specific evidence exists.
- Enrollment/install commands contain one-time tokens. Treat them as secrets and do not paste them into public issues, screenshots, logs, or shared transcripts.
