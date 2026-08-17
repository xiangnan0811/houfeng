# Task 6 preflight (2026-08-16)

## Baseline and scope

- Task 5 is durably committed on `codex/vps-records-evidence-platform-task5` as `530f9b48` (implementation) and `af569884` (Trellis contracts). The worktree is clean and remains based on `425a758df86c4d138ba80d376e66d10274ff28ae`.
- Task 6 is the Web selector and allowlisted renderer registry only. It may extend canonical Web DTOs, the lazy Records facade, shared `MetricChart` gap semantics, route-private evidence components/tests, and an existing CSS owner. It must not implement Task 7 capacity/janitor work, Child 10 production composition, external quarantine, a new route, PR, push, or merge.
- The approved design already fixes the workflow and registry model, so implementation must not reopen product brainstorming: kind → source → absolute window → metrics → precision → sensitive fields → preview → confirm; clients never submit payload bytes or metadata.

## Existing Web seams to reuse

- `web/src/lib/recordsApi.ts` is the lazy-only Records facade. Runtime imports remain limited to `apiRequest.ts` and `apiError.ts`, with DTOs type-imported from `types.ts`. Until a formal lazy Records route exists, no production module may consume it and it must remain absent from the current production graph; the synthetic lazy bundle contract proves the future chunk boundary.
- `web/src/security/recordsTransportArchitectureContract.test.ts` owns the exact facade export/runtime-dependency/eager-import inventory. New evidence API helpers must be added to its export allowlist without weakening the eager-graph checks.
- `web/src/components/atoms/MetricChart.tsx` is the shared chart primitive. It currently renders one polyline across every adjacent sample and has no gap input. Add a backward-compatible explicit discontinuity seam and RED tests so monitoring evidence can render separate line segments; never insert zero buckets, interpolate, or connect across a declared gap.
- Route-private components belong under `web/src/pages/records/evidence/`. They must stay pure with injected options/callbacks and must not import `recordsApi.ts` directly, which both preserves the lazy facade boundary and makes preview/stale behavior deterministic in component tests.
- Evidence workflow styles belong to the existing `shared-atoms-page` owner (`web/src/styles/partials/page.css`) using BEM and existing tokens. Do not create component CSS, inline styles, utility classes, literal colors, or raise CSS/bundle budgets.

## Canonical transport and picker contract

- Add exact snake_case DTOs for `POST /api/evidence/capture-previews` and `GET /api/evidence/:id`, matching the Task 5 handler allowlist. Preview includes the server-owned `record_id`, `snapshot_id`, `capture_intent_id`, kind/schema, subject/source identity, requested/actual window, observed/source/producer/calculation facts, units/quality/sensitivity, precision/bucket, quota/retention/redaction, estimated bytes, renderer version, `previewed_at`, and `valid_until`. Read adds captured/referenced time, source availability, title, and a versioned `read_model`; it never exposes canonical payload, digest, authorization floor, or arbitrary metadata.
- Add typed evidence selection/input and the two API helpers to `recordsApi.ts`; tests must lock method, path encoding, body allowlist, optional abort signal, and stable 409/503 error codes through the existing decoder.
- The picker is a controlled route-private workflow with explicit kind/source option inputs and an injected preview requester. It enables steps strictly in order, resets every downstream choice when an upstream value changes, uses absolute UTC start/end, defaults sensitive fields to none, and requires an explicit user choice before any sensitive field enters the request.
- Preview confirmation emits only the record-bound capture reference needed by Records (`record_id` plus `capture_intent_id`, preserving server order). It is disabled once `valid_until` is reached and must surface a stale state without silently requesting or confirming a replacement.

## Renderer registry and read-model allowlist

- Registry lookup is an exact tuple of authoritative `kind`, `schema_version`, `renderer_version`, and `read_model.version`. The six admitted tuples are:
  - `ip_quality.report/v1` → `ip_quality_report_v1` → `ip_quality_report_read_model/v1`
  - `monitoring.host/v1` → `monitoring_host_v1` → `monitoring_host_read_model/v1`
  - `monitoring.probe/v2` → `monitoring_probe_v2` → `monitoring_probe_read_model/v1`
  - `monitoring.event/v2` → `monitoring_event_v2` → `monitoring_event_read_model/v2`
  - `subscription.cost/v1` → `subscription_cost_v1` → `subscription_cost_read_model/v1`
  - `command.audit/v1` → `command_audit_v1` → `command_audit_read_model/v1`
- Each renderer owns a manual runtime decoder for only its versioned read model. TypeScript annotations alone are insufficient at the network boundary. Invalid, unknown, duplicated, or mismatched tuples fail closed before rendering; there is no prefix matching, arbitrary-object spread, `JSON.stringify` fallback, or ordinary UI for external unsupported/quarantine metadata.
- IP renders only allowlisted report/status/stale/risk/coverage/provider/service/quality facts and no address/topology field. Monitoring renders bounded series/buckets/gaps/peaks/quality; declared gaps split chart segments. Events, costs, and command audits render only the allowlisted fields produced by their Task 3/4 summaries; command output payload remains explicitly unavailable.
- Response-envelope metadata shown beside a renderer is restricted to the Task 5 DTO. Tests must seed forbidden `payload`, `metadata`, `authorization`, `digest`, `stdout`, and `stderr` lookalikes and prove they do not enter the normal UI or source fallback paths.

## RED and verification matrix

- Start with RED Vitest coverage for picker step order/reset, absolute window/request fields, explicit sensitive opt-in, preview parity and stale confirmation; registry exact tuple dispatch, unknown/mismatched read model fail-closed, forbidden-field non-rendering, all six renderers, and monitoring gap segment behavior.
- Add a shared `MetricChart` RED asserting a declared gap produces distinct SVG line segments without an interpolating line. Existing chart callers must remain unchanged.
- Focused GREEN: evidence component/registry tests, `MetricChart.test.tsx`, `recordsApi.test.ts`, Records transport architecture, bundle contract, CSS reachability/owner contracts, TypeScript build, and lint.
- Final Task 6 gate: Node 22, full Web Vitest/coverage, `make verify-web` (lint, build, bundle, CSS), `git diff --check`, changed-file review, and Trellis validation. Browser E2E is not proof of live capture because Child 10 intentionally leaves production AdmissionGate/source composition fail closed and no formal Records route exists.
