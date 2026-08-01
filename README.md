# 候风 / Houfeng Fleet Control Plane

Houfeng is an early-stage, self-hosted fleet control plane for a single operator. It focuses on monitoring servers and service entrypoints first, then adds a lightweight VPS Asset Ledger so infrastructure inventory and observability evidence can be reviewed in one place.

The repository contains the Go center, Go agent, PostgreSQL schema, React/Vite web UI, Docker Compose center deployment artifacts, local/systemd deployment notes, and validation workflows. It is not documented as production-ready packaging: there are no package-manager repositories, Kubernetes deployment manifests, automatic upgrades, or completed real-inventory validation claims in this repo.

## Current shape

The supported deployment topology is small and explicit:

```text
operator browser
      |
      v
houfeng-center (Go API + React SPA)
      |
      v
PostgreSQL

houfeng-agent(s) --outbound enroll/sync--> houfeng-center
```

- **Center**: serves the API and built web UI, applies embedded PostgreSQL migrations, manages auth sessions, settings, incidents/events, retention, node onboarding, and Asset Ledger APIs.
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

A Docker Compose center quick start is also available:

```bash
cp docs/deploy/compose.env.example docs/deploy/compose.env
install -d -m 700 docs/deploy/secrets
umask 077
openssl rand -hex 32 > docs/deploy/secrets/postgres-bootstrap-password
openssl rand -hex 32 > docs/deploy/secrets/houfeng-database-password
# edit docs/deploy/compose.env and replace the admin password and session HMAC key
set -eu
scripts/compose-up.sh docs/deploy/compose.env
```

The Compose stack contains only the project image (`linnea7171/houfeng:latest`, with `houfeng-center`, a small runtime entrypoint, and baked `web/dist`) as service `houfeng` and PostgreSQL. The database bootstrap principal is distinct from the Houfeng application principal, and their untracked password files are mounted only into the services that need them. The fail-stop launcher waits for database readiness, runs the required pre-R1 provisioning as the bootstrap principal, rejects any existing application-role membership drift without transferring database ownership, creates or tightens the application login only when exact, and only then starts Houfeng. The entrypoint percent-encodes fallback DSN components, so printable URI-reserved characters in strong application passwords are supported. It binds Houfeng to `127.0.0.1:16001` by default for an operator-managed HTTPS reverse proxy, persists PostgreSQL data under `./data/postgres/` for easier migration, runs the project container as the non-root `houfeng` user, stores center file logs in the `houfeng_logs` named Docker volume, and does not containerize agents. Release Please opens or updates release PRs for eligible conventional changes on `main`; merging a release PR publishes a GitHub Release, which then publishes Docker images to `linnea7171/houfeng`. One-command agent onboarding still requires a center image built with a real release version and matching Linux agent release assets.

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

- `docs/deploy/local-and-systemd.md` — canonical local, Docker Compose center, systemd deployment, and one-command agent install guide.
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
- The project provides a minimal Docker Compose path for center + web + PostgreSQL, but does not provide containerized agents, Kubernetes deployment, package repositories, or automatic upgrade UX.
- Production deployments should set `HOUFENG_PUBLIC_BASE_URL`, reject broad trusted proxy ranges, run behind an HTTPS reverse proxy with request body/rate/connection limits, and pin Docker/image versions or digests instead of relying on mutable tags for change control.
- Agent diagnostic command output is best-effort redacted before upload and again before persistence, but it can still contain operationally sensitive host state. Run agents as the dedicated `houfeng-agent` user and do not add broad host privileges such as Docker group membership unless the risk is accepted.
- Asset Ledger facts are only as true as the manually entered or imported data. Do not claim provider account truth, billing accuracy, exchange-rate truth, or completed real-inventory validation unless the specific evidence exists.
- Enrollment/install commands contain one-time tokens. Treat them as secrets and do not paste them into public issues, screenshots, logs, or shared transcripts.
