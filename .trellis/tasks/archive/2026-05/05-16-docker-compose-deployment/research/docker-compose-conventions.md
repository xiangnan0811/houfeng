# Research: Docker Compose conventions for Houfeng center deployment

- **Query**: Research Docker/Compose packaging conventions for a Go backend plus built React/Vite SPA served by the Go process, with PostgreSQL as a sibling service. Map findings to this repo's constraints: single center+web project image, PostgreSQL volume, minimal environment surface, external reverse proxy/TLS managed by the operator, agents not containerized.
- **Scope**: mixed
- **Date**: 2026-05-16

## Findings

### Files Found

| File Path | Description |
|---|---|
| `README.md` | Public topology and current packaging limitations. States center serves API + built web UI, PostgreSQL is separate, agents connect outbound, and Docker/Kubernetes deployment is not currently provided (`README.md:5`, `README.md:15-20`, `README.md:23-25`, `README.md:107-110`). |
| `docs/deploy/local-and-systemd.md` | Canonical current deployment guide. Defines center env, built SPA path, PostgreSQL URL, reverse-proxy/TLS requirement, one-command agent install flow, and systemd agent boundary (`docs/deploy/local-and-systemd.md:11-16`, `47-65`, `80`, `140-161`, `214-224`). |
| `.env.example` | Full current env inventory for center and agent. Center values are separate from agent values (`.env.example:3-21`, `.env.example:23-28`). |
| `internal/center/config/config.go` | Center env loader. Required center envs: `HOUFENG_DATABASE_URL`, `HOUFENG_INITIAL_USERNAME`, `HOUFENG_INITIAL_PASSWORD`; defaults exist for `HOUFENG_HTTP_ADDR`, `HOUFENG_WEB_DIST_DIR`, `HOUFENG_INCIDENT_SWEEP_INTERVAL`, and `HOUFENG_SESSION_TTL`; `HOUFENG_PUBLIC_BASE_URL` is optional at startup but validated when set (`internal/center/config/config.go:31-89`, `120-141`). |
| `cmd/houfeng-center/main.go` | Center entrypoint loads config, bootstraps, and runs the app; version defaults to `dev` unless stamped by build ldflags (`cmd/houfeng-center/main.go:13-24`). |
| `cmd/houfeng-center/bootstrap.go` | Center bootstrap opens PostgreSQL, applies embedded migrations, wires repositories, workers, auth, API handlers, installer script, and SPA handler into a single HTTP app (`cmd/houfeng-center/bootstrap.go:59-181`). |
| `internal/center/app/app.go` | The Go center process owns one HTTP server plus in-process workers; no external worker service is required by current code (`internal/center/app/app.go:19-27`, `29-80`). |
| `internal/center/store/postgres.go` | PostgreSQL pool setup parses `HOUFENG_DATABASE_URL` and pings within 5 seconds during startup (`internal/center/store/postgres.go:11-30`). |
| `internal/center/store/migrate/migrate.go` | Embedded migrations are applied by the center at startup; no separate migration container is present in current architecture (`internal/center/store/migrate/migrate.go:44-50`, `70-103`). |
| `internal/center/http/router.go` | `/api/healthz` is public, `/api/agent/*` is public/token-authenticated as applicable, protected API routes use session auth, and the SPA handler is mounted at `/` (`internal/center/http/router.go:61-65`, `78`, `220-240`). |
| `internal/center/http/handlers/spa.go` | Built SPA is served directly from `HOUFENG_WEB_DIST_DIR`; non-API GET routes fall back to `index.html` (`internal/center/http/handlers/spa.go:11-40`). |
| `Makefile` | Build targets for `houfeng-center`, `houfeng-agent`, web verification, and agent release artifacts. Center version is stamped with `-X main.version=$(VERSION)` (`Makefile:8-13`, `51-57`, `67-84`, `88-96`). |
| `web/package.json` | Web app uses Node 22 and `npm run build` runs `tsc -b && vite build`, producing the SPA dist consumed by the center (`web/package.json:6-14`). |
| `.trellis/spec/backend/directory-structure.md` | Trellis backend spec repeats the current topology: 1 Go center + 1 Postgres + N systemd Go agents, thin-agent boundary, and one-command installer contract (`.trellis/spec/backend/directory-structure.md:7-18`, `265-314`). |
| `.trellis/spec/backend/database-guidelines.md` | Trellis database spec states PostgreSQL + pgx/v5, raw SQL migrations embedded and applied on center startup (`.trellis/spec/backend/database-guidelines.md:7-15`, `61-86`). |
| `.trellis/spec/web/directory-structure.md` | Trellis web spec states production `web/dist/` is served by center via `HOUFENG_WEB_DIST_DIR` and `spa.go` (`.trellis/spec/web/directory-structure.md:28`). |

No existing `Dockerfile`, `docker-compose.yml`, `compose.yaml`, or `.dockerignore` was found by searching the repository root to depth 3.

### Code Patterns

#### Single center+web image shape

Current code already combines API and SPA serving in one Go center process:

```go
func SPA(webDistDir string) http.Handler {
	indexPath := filepath.Join(webDistDir, "index.html")
	// ... serves static files and falls back to index.html
}
```

Source: `internal/center/http/handlers/spa.go:11-40`.

Mapping to Docker/Compose convention:

- A conventional production image can be a single `houfeng-center` runtime image containing:
  - the compiled `houfeng-center` binary;
  - the built Vite output copied into an internal path such as `/app/web/dist`;
  - `HOUFENG_WEB_DIST_DIR` pointing at that copied path.
- A separate frontend/nginx container is not implied by current code because the Go center already serves the built SPA and API from the same router.
- A multi-stage Dockerfile convention maps cleanly to this repo: one Node 22 stage for `web/` build, one Go stage for `cmd/houfeng-center`, and one runtime stage containing only the binary plus SPA assets.

#### PostgreSQL sibling service and volume

The center is hardwired to PostgreSQL through a URL:

```go
databaseURL, err := requiredEnv("HOUFENG_DATABASE_URL")
```

Source: `internal/center/config/config.go:42-45`.

The database is opened and pinged during bootstrap:

```go
pool, err := pgxpool.NewWithConfig(ctx, cfg)
// ...
if err := db.Ping(pingCtx); err != nil {
	db.Close()
	return nil, fmt.Errorf("ping postgres: %w", err)
}
```

Source: `internal/center/store/postgres.go:17-30`.

Mapping to Docker/Compose convention:

- Compose should model PostgreSQL as a sibling service, commonly named `db` or `postgres`.
- The center-side database URL should use the Compose service DNS name, for example `postgres://houfeng:<password>@db:5432/houfeng?sslmode=disable`.
- Because the center pings PostgreSQL at startup and exits on failure, Compose healthchecks and `depends_on` long syntax with a `service_healthy` condition are the convention that best matches current startup behavior.
- The Postgres service should use a named volume mounted at the image's data directory, `/var/lib/postgresql/data`, to preserve database state across container replacement.

#### Embedded migrations remove the need for a migration sidecar

The center applies embedded migrations at startup:

```go
if err := deps.applyMigrations(ctx, db); err != nil {
	db.Close()
	return nil, nil, fmt.Errorf("apply migrations: %w", err)
}
```

Source: `cmd/houfeng-center/bootstrap.go:67-70`.

Migration application reads the embedded FS and records applied filenames in `schema_migrations`:

```go
func Apply(ctx context.Context, db *pgxpool.Pool) error {
	return applyFS(ctx, poolStore{db: db}, migrations.FS)
}
```

Source: `internal/center/store/migrate/migrate.go:48-50`.

Mapping to Docker/Compose convention:

- A separate `migrate` service is not part of current runtime shape.
- The Compose app service should run the normal `houfeng-center` binary; migrations are already in-process.

#### Minimal center environment surface

Current config behavior:

| Variable | Startup behavior | Compose mapping |
|---|---|---|
| `HOUFENG_HTTP_ADDR` | default `:8080` (`config.go:11-14`, `31-39`) | Can be fixed to `:8080` inside the container. |
| `HOUFENG_WEB_DIST_DIR` | default `web/dist`, but deployed path must exist (`config.go:37-40`, `docs/deploy/local-and-systemd.md:65`) | Set to the copied in-image SPA path. |
| `HOUFENG_DATABASE_URL` | required (`config.go:42-45`) | Compose env should point at sibling Postgres service DNS and database name. |
| `HOUFENG_PUBLIC_BASE_URL` | optional at startup but validates absolute `http(s)` URL without query/fragment (`config.go:52-55`, `120-141`) | Set to the operator-facing URL, usually the reverse-proxy HTTPS URL, because generated agent install commands use it. |
| `HOUFENG_INCIDENT_SWEEP_INTERVAL` | default `1m` (`config.go:47-50`) | Omit unless overriding. |
| `HOUFENG_INITIAL_USERNAME` | required (`config.go:63-66`) | Include for first startup; ignored after users table exists. |
| `HOUFENG_INITIAL_PASSWORD` | required (`config.go:67-70`) | Include for first startup; treat as secret in operator env handling. |
| `HOUFENG_INITIAL_DISPLAY_NAME` | optional (`config.go:71`) | Omit unless desired. |
| `HOUFENG_SESSION_TTL` | default `168h` (`config.go:72-75`) | Omit unless overriding. |
| `HOUFENG_TELEGRAM_BOT_TOKEN` / `HOUFENG_TELEGRAM_CHAT_ID` | both empty or both set (`config.go:57-61`) | Omit/empty by default; set as a pair only when Telegram delivery is enabled. |

The `.env.example` also includes agent variables (`.env.example:23-28`), but those belong to systemd agents and should not be part of the center Compose service environment.

#### Reverse proxy/TLS stays outside the app container

The current deployment guide states the session cookie does not carry `Secure`, and deployment is expected to terminate HTTPS at a reverse proxy:

```text
The session cookie does not carry the Secure attribute in the current implementation — the
deployment is expected to terminate HTTPS at a reverse proxy (Caddy, Nginx,
etc.) and forward to HOUFENG_HTTP_ADDR.
```

Source: `docs/deploy/local-and-systemd.md:214-220`.

Mapping to Docker/Compose convention:

- The center container should listen on plain HTTP internally, matching `HOUFENG_HTTP_ADDR`.
- TLS termination should be modeled as operator-managed infrastructure outside this app image, or as an externally managed reverse proxy that forwards to the center's published/local port.
- `HOUFENG_PUBLIC_BASE_URL` should be the externally reachable HTTPS URL used by browsers and target agents, not the internal Compose hostname.
- If exposing the center service from Compose directly to the host for a reverse proxy, binding to loopback (for example host `127.0.0.1:8080` to container `8080`) matches the external-proxy boundary better than publishing it openly without TLS.

#### Agents remain non-containerized

Current docs and specs define agents as systemd-managed Go binaries:

- README topology: `houfeng-agent(s) --outbound enroll/sync--> houfeng-center` (`README.md:20`).
- Deployment scope: one Go center process, one PostgreSQL database, and Go agents managed by systemd (`docs/deploy/local-and-systemd.md:11-16`).
- One-command installer supports Linux systemd `amd64`/`arm64` and writes `/etc/houfeng-agent/agent.env`, `/etc/houfeng-agent/token`, `/var/lib/houfeng-agent`, and a systemd unit (`docs/deploy/local-and-systemd.md:140-161`).
- Trellis backend spec marks Docker/Kubernetes installs out of scope for agent installer MVP (`.trellis/spec/backend/directory-structure.md:282-287`).

Mapping to Docker/Compose convention:

- A Compose file for this task should not include an `agent` service.
- The center image may still serve `/api/agent/install.sh`; target hosts run the generated command outside Docker on Linux systemd hosts.
- The center build version matters for this flow: `main.version` defaults to `dev` (`cmd/houfeng-center/main.go:13`) and `make build-center VERSION=...` stamps it (`Makefile:51-57`). If a Docker build does not stamp a real release version, install-command generation follows current behavior and reports a configuration error instead of generating production commands (`docs/deploy/local-and-systemd.md:80`, `154-158`).

### External References

- [Docker Compose file reference: services](https://docs.docker.com/reference/compose-file/services/) — Compose models an application as named services. Relevant service keys for this repo are `build`/`image`, `depends_on`, `environment`/`env_file`, `ports`, `expose`, `healthcheck`, `restart`, and service-level `volumes`.
- [Docker Compose file reference: build](https://docs.docker.com/reference/compose-file/build/) — Compose `build` can point at a build context and Dockerfile, and can target a named stage in a multi-stage Dockerfile. Relevant for building a single project image from the Go backend and Vite SPA in one repository.
- [Docker Build: multi-stage builds](https://docs.docker.com/build/building/multi-stage/) — Multi-stage builds allow multiple `FROM` stages and `COPY --from` so the final image contains runtime artifacts without compiler/toolchain layers. Relevant for Node build output + Go binary final image.
- [Docker Compose file reference: volumes](https://docs.docker.com/reference/compose-file/volumes/) — Top-level `volumes` declares named volumes reusable by services. Relevant for a persistent PostgreSQL data volume.
- [Docker: persisting container data](https://docs.docker.com/get-started/docker-concepts/running-containers/persisting-container-data/) — Container writable layers are ephemeral relative to container replacement; volumes persist data. Relevant to PostgreSQL state.
- [Official Postgres Docker image documentation](https://github.com/docker-library/docs/blob/master/postgres/README.md) — The image initializes a default user/database from `POSTGRES_USER`, `POSTGRES_PASSWORD`, and `POSTGRES_DB`, supports `_FILE` variants for some secrets, and documents `/var/lib/postgresql/data` as the data directory that must be mounted for persistence.

### Related Specs

- `.trellis/spec/backend/directory-structure.md` — Current backend topology and agent one-command install boundary; confirms 1 Go center + 1 Postgres + N systemd agents and that Docker/Kubernetes installs are out of current agent scope.
- `.trellis/spec/backend/database-guidelines.md` — PostgreSQL/pgx and embedded migration behavior; confirms no ORM and startup-applied migrations.
- `.trellis/spec/web/directory-structure.md` — Production web build artifact location and center-served SPA behavior.

## Compose Shape Mapped to Repo Constraints

This section is a mapping of conventions to existing constraints, not an implementation patch.

### Services

| Compose service | Repo constraint mapping |
|---|---|
| `center` | Single project image containing `houfeng-center` plus built `web/dist`. Runs the Go binary only. Serves `/api/*`, `/api/agent/install.sh`, and SPA fallback from one process. |
| `db` | Official `postgres` image with named volume mounted at `/var/lib/postgresql/data`. Center connects through `HOUFENG_DATABASE_URL` using Compose service DNS. |

No `agent` service is part of the mapped Compose shape because agents are installed on target hosts by the center-generated systemd installer.

### Image/build convention

A conventional Dockerfile layout for this repo would have these build stages:

1. `web-build`: Node 22, install `web/package-lock.json` dependencies, run `npm run build`, produce `web/dist/`.
2. `go-build`: Go toolchain, compile `./cmd/houfeng-center`, stamp `main.version` with a release version when production install-command generation is needed.
3. `runtime`: small Linux runtime base, copy `/houfeng-center` and built SPA, set the binary as entrypoint.

This matches:

- `web/package.json:6-14` for Node 22 and Vite build command;
- `Makefile:51-57` for center binary build and version ldflags;
- `internal/center/http/handlers/spa.go:11-40` for serving copied SPA assets.

### Environment convention

The minimal center service env for Compose is the center subset only:

```dotenv
HOUFENG_HTTP_ADDR=:8080
HOUFENG_WEB_DIST_DIR=/app/web/dist
HOUFENG_DATABASE_URL=postgres://houfeng:<password>@db:5432/houfeng?sslmode=disable
HOUFENG_PUBLIC_BASE_URL=https://center.example.com
HOUFENG_INITIAL_USERNAME=admin
HOUFENG_INITIAL_PASSWORD=<operator-provided-initial-password>
```

Optional values stay optional unless the operator enables them:

```dotenv
HOUFENG_INCIDENT_SWEEP_INTERVAL=1m
HOUFENG_SESSION_TTL=168h
HOUFENG_INITIAL_DISPLAY_NAME=
HOUFENG_TELEGRAM_BOT_TOKEN=
HOUFENG_TELEGRAM_CHAT_ID=
```

Postgres service env convention:

```dotenv
POSTGRES_USER=houfeng
POSTGRES_PASSWORD=<same password used in HOUFENG_DATABASE_URL>
POSTGRES_DB=houfeng
```

The center currently reads `HOUFENG_DATABASE_URL` directly and does not implement `_FILE` expansion for its own variables. The official Postgres image supports `_FILE` for selected initialization variables, but that behavior belongs to the Postgres image entrypoint, not to the Houfeng center config loader.

### Port/proxy convention

- Container listens on `:8080` by default.
- `ports` publishes the center to the operator host when an external reverse proxy needs a local upstream.
- `expose` or an external Docker network may be used when a separately managed proxy container/network routes to the center without publishing to all host interfaces.
- TLS is not terminated by the center process; the operator-managed reverse proxy terminates HTTPS and forwards HTTP to the center.

### Health convention

- Center health endpoint: `/api/healthz` is public (`internal/center/http/router.go:78`) and can be used for a container healthcheck after startup.
- Postgres healthcheck convention: `pg_isready` against the configured database/user.
- Center startup dependency convention: use a healthy Postgres dependency because the center pings PostgreSQL at startup and exits on failure.

## Caveats / Not Found

- No existing Dockerfile, Compose file, or `.dockerignore` exists in the searched repository paths.
- Public docs currently state Docker/Kubernetes deployment is not provided (`README.md:5`, `README.md:109`; `docs/deploy/local-and-systemd.md:15`). Any future Compose packaging changes that statement from current limitation to newly documented deployment path.
- The center does not currently retry PostgreSQL indefinitely during bootstrap; it parses and pings the configured database URL once during startup with a 5-second ping context (`internal/center/store/postgres.go:11-30`). Compose health dependency is therefore important for startup ordering.
- `HOUFENG_PUBLIC_BASE_URL` must be the externally reachable URL for agents and browser copy flows. Internal Compose names like `http://center:8080` are not suitable for production install commands unless target agents can actually reach that address.
- The current session cookie lacks the `Secure` attribute, so running the center directly on public plain HTTP conflicts with the deployment guide's HTTPS requirement (`docs/deploy/local-and-systemd.md:214-220`).
- Agent containerization is outside the current documented boundary; the mapped Compose shape intentionally covers only center + PostgreSQL.
