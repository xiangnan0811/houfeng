# Center file logging

## Goal

Add file-based Houfeng application logging so Docker/Compose and systemd center deployments expose a stable log file path that operators can collect for troubleshooting and feedback, instead of relying only on container stdout/stderr or journald.

## What I already know

* The user previously identified file logs as a required follow-up: if the app does not write files, users cannot reliably troubleshoot or provide useful feedback.
* Current Docker/Compose docs intentionally avoid a log bind mount because the app currently writes only stdout/stderr.
* The Docker/Compose deployment boundary remains center+web plus PostgreSQL only; agents stay host Linux/systemd installs.
* Log output must not expose enrollment tokens, sync tokens, passwords, session cookies, webhook URLs, Telegram/Feishu credentials, or other secrets.
* `cmd/houfeng-center/main.go` currently logs fatal errors through package-level `slog.Error`; bootstrap and workers consume `slog.Default()`.
* `internal/center/config.CenterConfig` has no logging field today; `.env.example`, Compose env, Dockerfile, and systemd examples do not configure file logging.
* The runtime image runs as the non-root `houfeng` user, so any container log path must be writable by that user.
* The systemd unit currently has `ProtectSystem=full` and `ReadWritePaths=/opt/houfeng`, so writing to `/var/log/houfeng` would require a unit change.

## Assumptions (temporary)

* A simple append-only application log file is enough for MVP; rotation can be left to Docker/systemd/logrotate unless code inspection shows a better existing pattern.
* The center should continue emitting logs to stdout/stderr so container and systemd logs still work.
* Recommended default path is `/var/log/houfeng/center.log` for deployed center environments, with Docker mapping `./data/logs:/var/log/houfeng`.

## Open Questions

* None currently.

## Requirements (evolving)

* Scope is center only. Do not implement agent file logging in this task; the product direction is that agent file logging is unlikely to be considered later.
* Add `HOUFENG_LOG_FILE` for center deployments. Empty or unset means stdout/stderr-only behavior remains available for local development.
* When `HOUFENG_LOG_FILE` is set, center writes structured application logs to both stdout/stderr and the configured file.
* Use `/var/log/houfeng/center.log` as the deployed file path in Docker/Compose and systemd examples.
* Update Docker/Compose so `/var/log/houfeng` is backed by host directory `./data/logs` only after code writes files.
* Update the Docker runtime image so `/var/log/houfeng` exists and is writable by the non-root `houfeng` user.
* Update the systemd unit and deployment docs so `/var/log/houfeng` is created, owned correctly, and allowed by `ReadWritePaths`.
* Preserve secret redaction boundaries: do not log enrollment tokens, sync tokens, passwords, session cookies, provider credentials, webhook URLs, or Telegram/Feishu tokens.

## Acceptance Criteria (evolving)

* [ ] Center parses optional `HOUFENG_LOG_FILE` into config.
* [ ] When `HOUFENG_LOG_FILE` is set, center creates/opens the file append-only and writes `slog` output to both stdout/stderr and the file.
* [ ] When the log file cannot be opened, center startup fails with a clear error before serving traffic.
* [ ] Center still logs to stdout/stderr for Docker/systemd compatibility.
* [ ] Docker/Compose maps `./data/logs` to `/var/log/houfeng` and configures `HOUFENG_LOG_FILE=/var/log/houfeng/center.log`.
* [ ] Systemd deployment docs create/own `/var/log/houfeng`, configure `HOUFENG_LOG_FILE=/var/log/houfeng/center.log`, and unit permissions allow writes.
* [ ] Tests or static checks cover config parsing and file creation/write behavior.
* [ ] Docs no longer describe center file logging as unresolved once implemented.

## Definition of Done

* Tests added/updated where appropriate.
* `make verify-go` passes.
* `make verify-web` passes if touched docs/frontend build requirements do not make it irrelevant.
* Docker/Compose config validation passes when Docker Compose is available.
* Docs/spec updated only with truthful active guidance.
* PR workflow completed per established Houfeng process.

## Out of Scope (explicit)

* Centralized log aggregation, search, or UI log viewer.
* Kubernetes logging.
* Agent file logging.
* Agent containerization.
* Automatic log upload to external services.
* Claiming production-grade retention/rotation unless implemented or delegated explicitly to host tooling.

## Technical Notes

* Task directory: `.trellis/tasks/05-17-center-file-logging`.
* Existing Compose docs mention file logging as a required follow-up; this task should close that gap only when code actually writes files.
