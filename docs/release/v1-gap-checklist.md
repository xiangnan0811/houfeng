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
| Frozen app shell and primary navigation | Closed | Implementation-level shell hierarchy and routes are aligned in `web/src/app/layout/AppShell.tsx`, `web/src/app/router.tsx`, and `web/src/index.css`; screenshot evidence remains tracked separately |
| Dashboard abnormal summaries and event stream | Closed | `web/src/pages/DashboardPage.tsx` |
| Nodes list filters and onboarding entry | Closed | `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx` |
| Node detail operational summary and trends | Closed | `web/src/pages/NodeDetailPage.tsx` |
| Target list/detail and ProbeItem management | Closed | `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx` |
| Events advanced filters | Closed | `web/src/pages/EventsPage.tsx` |
| Settings runtime truthfulness | Closed | `web/src/pages/SettingsPage.tsx`, `internal/center/settings` |
| Chinese-first UI copy and dense baseline hierarchy | Closed | Alignment pass recorded in `docs/operations/v1-visual-verification.md`; frontend evidence in `web/src/app/layout/AppShell.tsx`, `web/src/components/ActionConfirmationCard.tsx`, `web/src/pages/DashboardPage.tsx`, `web/src/pages/NodesPage.tsx`, `web/src/pages/NodeDetailPage.tsx`, `web/src/pages/NodeOnboardingPage.tsx`, `web/src/pages/TargetsPage.tsx`, `web/src/pages/TargetDetailPage.tsx`, `web/src/pages/EventsPage.tsx`, and `web/src/pages/SettingsPage.tsx` |
| Visual screenshot comparison against baseline PNGs | Partial | Live route screenshots were captured on 2026-04-29 under `docs/operations/visual-evidence/` and indexed by `docs/operations/visual-evidence/manifest.json`; strict visual-fidelity acceptance remains pending because the captures have not been accepted as high-fidelity matches to the frozen references |

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
| Fresh-install smoke procedure | Closed | `docs/operations/v1-smoke-run.md` documents the reproducible Node → agent enrollment → Target → ProbeItem → observation → incident/event/notification path |
| Fresh-install smoke executed on live PostgreSQL | Closed | `docs/operations/v1-smoke-run.md` records the 2026-04-29 live run against PostgreSQL `192.168.100.192:5432/user_82Xkx5`: center health, Node, agent enrollment/sync, Target, ProbeItem, observation, incident start/recovery, and notification-backed event query passed. Telegram delivery and browser screenshots remain separate evidence rows. |

## Authentication (V1.x scope add)

| Area | Status | Evidence |
| --- | --- | --- |
| Username + password login (方案 2) | Closed | `internal/center/auth/`, `internal/center/store/users.go`, `internal/center/store/sessions.go`, migration `db/migrations/0010_add_users_and_sessions.sql` |
| All non-agent / non-health API protected by session cookie | Closed | `internal/center/http/middleware.go`, `internal/center/http/router.go`, `internal/center/http/auth_e2e_test.go` |
| Initial user seed from env on first startup | Closed | `internal/center/auth/seed.go`, `cmd/houfeng-center/bootstrap.go` |
| Session cleanup worker | Closed | `internal/center/auth/cleanup.go`, wired in `cmd/houfeng-center/bootstrap.go` |

## V1.x visual baseline (replaces frozen V1 visual portion)

The V1 visual baseline (Stitch Unified / Baseline screens, `docs/design/v1-baseline/ui-ux-spec.md`,
`visual-review-round2.md`, `baseline-screens.md`) was officially **unfrozen 2026-04-29** and replaced
by `docs/design/v1.x-frontend-redesign/`. The structural sections of the V1 baseline
(`architecture-data-model.md`, `rules-and-interaction.md`, `tech-selection.md`) remain frozen and
authoritative.

| Area | Status | Evidence |
| --- | --- | --- |
| 4-theme token system (候风原色 / 经典 × 深 / 浅) | Closed | `web/src/styles/tokens.css`, `web/src/lib/theme.ts`, `web/src/lib/theme-context.tsx` |
| FOUC-free sync theme bootstrap | Closed | inline script in `web/index.html` |
| 6 component atoms with tests | Closed | `web/src/components/atoms/*` |
| Sidebar shell with user chip + sync status | Closed | `web/src/app/layout/Sidebar.tsx`, `UserChip.tsx`, `SyncStatus.tsx` |
| Login page with backend auth (方案 2) | Closed | `web/src/pages/LoginPage.tsx`, Plan 1 backend |
| Route guard + 401 redirect | Closed | `web/src/app/RequireAuth.tsx`, `web/src/lib/auth-context.tsx` |
| Token-driven page chrome (8 pages) | Closed | `web/src/styles/pages.css` re-skins every page class through V1.x tokens |
| Theme tab inside Settings | Closed | `ThemeSettingsSection` block in `web/src/pages/SettingsPage.tsx` |
| Page-level redesign per spec §10 (身份卡, 5 Tab, 危险区, 趋势条) | Deferred — follow-up | Tracked as V1.x.1; current pages keep their pre-V1.x layouts under the new shell + tokens |
| Visual evidence (4 themes × representative pages) | Deferred — follow-up | Operations work; legacy V1 captures preserved under `docs/operations/visual-evidence/` |
| WCAG AA contrast verified per theme | Deferred — follow-up | Manual smoke pending |

## Final V1 release gate

Before tagging or declaring V1 fully release-ready, collect:

- passing `go test ./...`;
- passing `./scripts/verify.sh`;
- passing `cd web && npm run build`;
- completed live PostgreSQL smoke table in `docs/operations/v1-smoke-run.md` (collected 2026-04-29);
- visual screenshot comparison artifacts are captured; strict visual-fidelity acceptance or an explicit accepted waiver remains pending;
- Telegram delivery proof or an explicit note that Telegram is disabled for the deployment.
