# Asset Ledger local center sample evidence

## Goal

Execute or explicitly block the next real-data readiness step: run the Asset Ledger local-center sample path with a real center login, sample JSON import, and protected-route browser sanity. Persist the evidence so the project can decide whether it is ready for user-authorized real 40+ VPS validation.

## Context

- PR #76 added authenticated browser sanity support, `docs/operations/asset-ledger-local-sample.json`, and `docs/operations/asset-ledger-real-data-validation-readiness.md`.
- PR #77 archived that readiness task and recorded the session.
- The roadmap now says the next useful step is local center sample evidence, not more visual-only polishing.
- The user asked to continue without subagents; this task stays in the main session.

## Requirements

1. Attempt to run a disposable local PostgreSQL instance for `houfeng-center`.
2. Build/start `houfeng-center` with the existing web dist and local env vars.
3. Run database-aware `houfeng-import-vps-json -dry-run` against the committed sample.
4. If dry-run is clean, import the committed sample into the disposable database.
5. Start Vite against the local center and run authenticated browser sanity for `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions` at `1440x1000` and `390x900`.
6. Record exact evidence in `docs/operations/asset-ledger-real-data-validation-readiness.md` or a linked operations evidence section.
7. If local runtime cannot be started, record the blocker precisely with commands and outputs; do not fabricate local-center evidence.

## Acceptance Criteria

- [x] Local center sample evidence is recorded with date, commands, routes, viewports, row counts, and limitations.
- [x] Evidence clearly distinguishes completed checks from blocked checks.
- [x] If local center runs, authenticated browser sanity uses `auth=session-login` and data source `local center sample`.
- [x] If local center cannot run, the blocker is concrete enough to unblock later work.
- [x] No production or user real inventory data is imported or committed.
- [x] Relevant local verification passes.
- [ ] PR CI is green before merge.

## Out of Scope

- Do not import the user's real 40+ VPS inventory.
- Do not add new backend fields, migrations, or UI redesigns.
- Do not install new long-lived local services into the repo.
- Do not use subagents.

## Verification Plan

- `git diff --check`
- `python3 ./.trellis/scripts/task.py validate .trellis/tasks/05-14-asset-ledger-local-center-sample-evidence`
- If docs only changed, run `make validate-visual-evidence`; if evidence uses helper paths, rerun relevant browser sanity or document the blocker.

## Verification Evidence

- `orb start` - pass; OrbStack changed from `Stopped` to `Running`.
- `docker pull postgres:16-alpine` - pass.
- `docker run --rm --name houfeng-local-sample-postgres ... -p 15432:5432 -d postgres:16-alpine` - pass.
- `docker exec houfeng-local-sample-postgres pg_isready -U houfeng -d houfeng` - pass.
- `npm --prefix web run build` - pass.
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp GOTMPDIR=/Users/weibo/Code/houfeng/.tmp/go-build go build -o ./bin/houfeng-center ./cmd/houfeng-center` - pass.
- `curl -fsS http://127.0.0.1:18080/api/healthz` - pass.
- `select count(*) from schema_migrations` - 25.
- Database-aware dry-run after migrations - pass; `database_checked=true`, `can_import=true`, 5 input rows, 4 provider candidates, 5 VPS candidates, 4 subscription candidates, 0 validation errors, 0 duplicates.
- Sample import - pass; created 4 providers, 5 VPS assets, 4 subscriptions.
- Post-import database counts - providers 4, vps_assets 5, subscriptions 4.
- Authenticated browser sanity against `http://127.0.0.1:18080/` - pass for `/asset-decisions`, `/vps`, `/providers`, and `/subscriptions` at `1440x1000` and `390x900`, all with `auth=session-login`.
- Cleanup - center stopped and `docker rm -f houfeng-local-sample-postgres` completed.
