# Implementation Plan

## Checklist

1. Branch and Trellis setup
   - Work on non-main branch `worktree/monitoring-instances-concept`.
   - Enable repository hooks.
   - Start this Trellis task after PRD/design/implement exist.

2. Backend and database migration
   - Rename domain package `nodes` to `monitoringinstances`.
   - Rename store, handler, router, bootstrap, sync, runtime facts, runtime controls, onboarding, sparklines, batch action, dashboard, incidents, and asset link contracts.
   - Add migration after current max migration to rename tables/columns/indexes/FKs and update persisted enum-like values.
   - Update ids prefix for new monitoring instances from `nd` to a monitoring-instance prefix.

3. Agent contract
   - Update `agentapi` types, token file schema, runtime logs, enroll/sync client tests, and sync queue payloads to `monitoring_instance_id`.
   - Remove old `node_id` assumptions.

4. Frontend migration
   - Rename routes/pages/components/types/API helpers from Node to MonitoringInstance where they represent the old domain.
   - Replace `/nodes` with `/monitoring` in router, sidebar, breadcrumbs, topbar, dashboard links, global search, tests, and visual evidence fixtures.
   - Keep ReactNode, DOM Node, Node.js version references untouched.
   - Update UI copy to use `监控` for the page and `监控实例` for the entity.

5. Docs/specs
   - Update `CLAUDE.md`, active backend/web specs, deploy/smoke/visual evidence docs, and asset real-data readiness docs.
   - Do not edit frozen `docs/design/v1-baseline/*`.

6. Verification
   - Run targeted Go tests as rename stabilizes.
   - Run targeted web tests as routes stabilize.
   - Run `make verify-go`, `cd web && npm run lint`, `cd web && npm run test -- --run`, `cd web && npm run build`, `./scripts/verify.sh`, and `git diff --check`.
   - Run browser sanity if the local environment supports it; otherwise record the tooling blocker.

## Rollback Points

- If backend contract migration fails broadly, revert before frontend route migration and re-run Go tests.
- If frontend route migration fails broadly, keep backend compiled and iterate from API type errors.
- If final visual sanity is blocked by local Playwright/browser tooling, preserve CLI verification and report the limitation.
