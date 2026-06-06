# Implementation Plan

## 1. Prepare

- Confirm worktree branch is `worktree/asset-decisions-scenario-recommendations-links`.
- Run `trellis-before-dev` context before editing backend/web code.
- Start task only after `prd.md`, `design.md`, and `implement.md` exist.

## 2. Backend

- Add migration for custom scenario template tables and migrate test expectation.
- Add domain types and validators for scenario templates and recommendation.
- Add deterministic built-in templates and template-to-manual-group builders.
- Extend `ListFilters` validation and derivation filters without cropping group details.
- Add store methods for template list/create/get/patch/create-manual-group.
- Extend manual groups and records list filtering using current facts.
- Add handlers/router/bootstrap wiring and tests.

## 3. Frontend

- Extend `web/src/lib/types.ts` and fix duplicate `lifecycle_status` type field.
- Add API helpers and tests for templates and expanded filters.
- Refactor `AssetDecisionsPage` enough to keep template/filter/recommendation helpers local and readable.
- Add template surface, template detail modal/drawer, save-as-template action, create-manual-group-from-template flow.
- Add filter chips, URL-state sync, and deep-link auto-open for group/manual group/record/template.
- Update cross-page links from Dashboard/VPS/VPSDetail/Subscriptions/Providers/Monitoring/Targets.

## 4. Specs

- Update backend asset decision spec for template layer, recommendation layer, filters, and no-runtime-facts boundary.
- Update web state/data spec and v2 component spec for template surface, deep-link URL-state, and recommendation display.

## 5. Verification

- Go:
  - `go test ./internal/center/assetdecisions ./internal/center/store ./internal/center/http/...`
  - `go test ./...`
- Web:
  - `cd web && npm run lint`
  - `cd web && TMPDIR=$PWD/.tmp npm run test -- --run AssetDecisionsPage api VPSPage SubscriptionsPage ProvidersPage DashboardPage`
  - `cd web && npm run build`
- Repo:
  - `git diff --check`
  - Trellis check / finish-work before commit.

## 6. Done

- Commit work on feature branch.
- Push branch and open PR.
- Monitor PR CI, fix failures, merge when green if allowed by repo workflow.
- Monitor release automation if merge triggers release.
