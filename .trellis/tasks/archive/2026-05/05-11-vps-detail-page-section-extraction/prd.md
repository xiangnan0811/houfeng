# VPS detail page section extraction

## Goal

Reduce `web/src/pages/VPSDetailPage.tsx` long-file complexity by extracting page-private presentation sections, forms, and table column helpers while preserving the current Asset Ledger behavior, API flow, user-visible copy, CSS classes, tests-visible request order, and validation semantics.

## Context

- `houfeng_codex_下一步开发计划.md` is functionally closed for the current Asset Ledger implementation track; real 40+ VPS data execution remains user-data-dependent and deferred.
- `docs/release/next-phase-plan.md` keeps Stage 2 long-page file splitting as deferred technical debt.
- Recent long-page extractions already completed `SettingsPage` and `NodesPage`; the current largest page is `VPSDetailPage.tsx`.
- The user explicitly requested no subagents; this task is implemented directly in the main session.

## Requirements

- Create page-private modules under `web/src/pages/vps-detail/` or an equivalent local directory.
- Extract low-risk VPS detail presentation sections/helpers, targeting:
  - hero/header actions;
  - operations panel sections for renewal decision, Node linking, lifecycle archive/restore, and experience logs;
  - facts display and facts edit form;
  - linked Node table columns;
  - services list/form and service table columns;
  - domains list/form and domain table columns;
  - access summary.
- Keep `VPSDetailPage.tsx` responsible for:
  - loading VPS detail, timeline, services, and domains;
  - all API calls and refresh ordering;
  - all submit handlers and local validation invocation;
  - draft state ownership and notices/errors;
  - navigation and route parameter handling.
- Extracted components must be controlled by props and callbacks; they must not call API helpers directly or mutate route state directly.
- Preserve current behavior:
  - request order in existing tests;
  - decision/facts/link/lifecycle/experience/service/domain validation text;
  - service/domain lightweight manual-record boundary;
  - archive/restore soft lifecycle behavior;
  - linked Node unlink behavior;
  - table columns, labels, links, empty states, and CSS classes.
- Do not change backend, API payloads, state labels, Asset Ledger model, real-data dry-run/import behavior, or release/publish workflow.

## Out of Scope

- Redesigning the VPS detail page layout or changing visual styling.
- Changing Asset Ledger API helpers, backend handlers, migrations, or data model.
- Running real VPS data dry-run/import.
- Adding service discovery, DNS provider sync, registrar sync, or certificate probing.
- Changing `VPSPage`, `SubscriptionsPage`, `NodeDetailPage`, or Dashboard behavior.
- Release/publish workflow.

## Acceptance Criteria

- [x] `VPSDetailPage.tsx` is materially smaller and more orchestration-focused.
- [x] Extracted VPS detail modules are page-private and controlled by props/callbacks.
- [x] Existing `VPSDetailPage` tests pass without weakening assertions.
- [x] Lint, focused VPS detail tests, build, and `make verify-web` pass locally.
- [ ] Trellis task is archived and journal recorded after the work commit.
- [ ] PR flow is completed: feature branch, PR, green CI, merge, main CI, local `main` synced.

## Verification

- `npm --prefix web run lint`
- `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp npm --prefix web run test -- --run src/pages/VPSDetailPage.test.tsx --reporter=verbose`
- `npm --prefix web run build`
- `make verify-web` — 60 test files, 456 tests passed; build passed. Local npm reported an engine warning because this machine uses Node v24.14.1 while `web/package.json` declares Node 22.x.

## Spec Update Review

No `.trellis/spec/` update was needed. This task split existing page-private presentation code without changing API signatures, payloads, validation contracts, data flow semantics, or project conventions.
