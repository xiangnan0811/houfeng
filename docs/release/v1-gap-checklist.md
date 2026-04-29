# Houfeng V1 Gap Checklist

## Scope

This checklist compares the implementation repository against the frozen V1 baseline. It does not revise the baseline.

Status values:

- **Closed:** implemented and covered by automated or documented evidence.
- **Partial:** implemented or documented, but final live evidence is still required.
- **Deferred outside V1:** intentionally not part of frozen V1 delivery.

## Product and architecture baseline

| Area | Status | Evidence |
| --- | --- | --- |
| Product naming is `候风 / Houfeng Fleet Control Plane` | Closed | `README.md`, binary names, design handoff |
| Go center + Go agent + React/Vite + PostgreSQL | Closed | `go.mod`, `cmd/houfeng-center`, `cmd/houfeng-agent`, `web/package.json`, `db/migrations` |
| Single center process owns API/UI/background workers/notifications | Closed | `cmd/houfeng-center/bootstrap.go` |
| systemd agent direction documented | Closed | `docs/deploy/systemd/houfeng-agent.service` |
| Docker-first deployment | Deferred outside V1 | Frozen tech selection excludes Docker as required runtime |

## Core object model

| Area | Status | Evidence |
| --- | --- | --- |
| Node persistence and UI | Closed | `internal/center/store/nodes.go`, `web/src/pages/NodesPage.tsx` |
| Target persistence and UI | Closed | `internal/center/store/targets.go`, `web/src/pages/TargetsPage.tsx` |
| ProbeItem persistence and UI | Closed | `internal/center/store/targets.go`, `web/src/pages/TargetDetailPage.tsx` |
| HostSample and ProbeObservation ingestion | Closed | `internal/center/observations`, `internal/center/syncing`, `agent/hostsample`, `agent/probe` |
| Incident and Event model | Closed | `internal/center/incidents`, `internal/center/store/dashboard.go`, `web/src/pages/EventsPage.tsx` |

## Runtime behavior

| Area | Status | Evidence |
| --- | --- | --- |
| Node enrollment and binding state | Closed | `internal/center/enrollment`, `web/src/pages/NodeOnboardingPage.tsx` |
| Agent durable sync buffer | Closed | `agent/syncqueue`, `agent/runtime/runtime.go` |
| Node pause/maintenance/retire sync semantics | Closed | `internal/center/store/agent_plan.go`, runtime control tests |
| Target pause/maintenance/archive semantics | Closed | `internal/center/http/handlers/runtime_controls.go`, target page tests |
| Retention and daily aggregation execution | Closed | `internal/center/retention`, `internal/center/store/retention.go` |
| Trend degradation incident families | Closed | `internal/center/incidents/evaluator.go` |

## UI and interaction surfaces

| Area | Status | Evidence |
| --- | --- | --- |
| Frozen app shell and primary navigation | Closed | `web/src/app/layout/AppShell.tsx`, `web/src/app/router.tsx` |
| Dashboard abnormal summaries and event stream | Closed | `web/src/pages/DashboardPage.tsx` |
| Nodes list filters and onboarding entry | Closed | `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx` |
| Node detail operational summary and trends | Closed | `web/src/pages/NodeDetailPage.tsx` |
| Target list/detail and ProbeItem management | Closed | `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx` |
| Events advanced filters | Closed | `web/src/pages/EventsPage.tsx` |
| Settings runtime truthfulness | Closed | `web/src/pages/SettingsPage.tsx`, `internal/center/settings` |
| Visual screenshot comparison against baseline PNGs | Partial | `docs/operations/v1-visual-verification.md`; live screenshot evidence pending unless PNG evidence is later committed |

## Notifications

| Area | Status | Evidence |
| --- | --- | --- |
| Telegram notifier implementation | Closed | `internal/center/notify/telegram.go` |
| Settings-aware notification policy | Closed | `internal/center/incidents/service.go`, settings tests |
| Live Telegram delivery evidence | Partial | Requires operator credentials; smoke guide records evidence path |

## Delivery and operations

| Area | Status | Evidence |
| --- | --- | --- |
| Local build/test verification path | Closed | `Makefile`, `scripts/verify.sh` |
| systemd examples for center and agent | Closed | `docs/deploy/systemd/*.service` |
| Deployment guide | Closed | `docs/deploy/local-and-systemd.md` |
| Fresh-install smoke procedure | Partial | `docs/operations/v1-smoke-run.md` is not present in this branch state; live smoke evidence therefore remains pending |
| Fresh-install smoke executed on live PostgreSQL | Partial | Requires live PostgreSQL and agent run; evidence table remains pending until filled |

## Final V1 release gate

Before tagging or declaring V1 fully release-ready, collect:

- passing `go test ./...`;
- passing `./scripts/verify.sh`;
- passing `cd web && npm run build`;
- completed live PostgreSQL smoke table in `docs/operations/v1-smoke-run.md`;
- visual screenshot comparison artifacts or an explicit accepted waiver for pending screenshot evidence;
- Telegram delivery proof or an explicit note that Telegram is disabled for the deployment.
