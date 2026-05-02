# Houfeng Target and ProbeItem Creation Design

## Context

V1 baseline requires Target creation from the target list, followed by ProbeItem creation from the Target detail page or the post-create empty state. The current backend already exposes `POST /api/targets` and `POST /api/targets/:targetId/probe-items`, and Target detail already renders ProbeItem lists and an empty state. The gap is the missing frontend data helpers and UI workflow for creating Targets and adding ProbeItems.

This slice does not redesign V1. It implements the frozen "short form, then continue to ProbeItem" flow from `docs/design/v1-baseline/rules-and-interaction.md` and `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md`.

## Scope

Implement:

- Target list primary action `新建目标`.
- Target list empty-state action `创建第一个目标`.
- Short Target creation form with name, type, host, optional base port, execution node labels, initial run status, labels, and note.
- After successful Target creation, navigate to `/targets/:targetId`.
- Typed API helpers and tests for `createTarget` and `createProbeItem`.
- Target detail ProbeItem creation entry from the ProbeItem section, including the empty state.
- ProbeItem creation form that first selects TCP, HTTP, or TLS and then collects the corresponding short-form fields.
- Created ProbeItem appears in the Target detail ProbeItem list without requiring a full page navigation.

Do not implement in this slice:

- Target edit flows.
- Target label edit popovers.
- ProbeItem edit, enable/disable, or delete.
- Heavy wizard flow.
- New backend endpoints beyond narrow validation/test fixes if required.
- Rule-engine behavior or per-object custom rules.

## Product Behavior

Target creation remains a compact operational form, not a multi-step wizard. The required fields match V1: name, type, host, execution node labels, and run status. `base_port`, labels, and note are optional. Comma-separated label inputs map to arrays.

After creation, the user lands on Target detail. If no ProbeItems exist yet, the ProbeItem section shows the existing empty-state message plus an add action. This keeps the V1 sequence clear: first create the observable entry, then attach the concrete observation method.

ProbeItem creation uses a single inline panel in Target detail. The user chooses `tcp`, `http`, or `tls`; the form then shows only fields relevant to that kind:

- TCP: port, timeout seconds, frequency tier, enabled.
- HTTP: scheme, path, method (`GET` or `HEAD`), expected status range, timeout seconds, frequency tier, enabled.
- TLS: port, certificate warning days, timeout seconds, frequency tier, enabled.

The form sends the backend's existing JSON config shape. Validation stays local enough to avoid obvious bad requests, while backend validation remains the source of truth.

## Architecture

Frontend data contracts are added to `web/src/lib/types.ts` and `web/src/lib/api.ts`. API tests lock request paths, JSON bodies, and returned records.

`web/src/pages/TargetsPage.tsx` owns Target creation because list-level creation is a page concern. It follows the existing Node creation pattern but redirects to Target detail instead of issuing any secondary token.

`web/src/pages/TargetDetailPage.tsx` owns ProbeItem creation because ProbeItems are scoped to a Target. The page updates local `probeItems` after a successful create and leaves runtime facts, incidents, and events untouched until their normal reload path.

Backend changes should be avoided unless tests reveal a contract issue. Existing Go handlers and store methods already support the needed create paths.

## Error Handling

Target creation errors stay inside the creation panel and do not clear the loaded list.

ProbeItem creation errors stay inside the ProbeItem creation panel and do not hide existing ProbeItems or activity data.

Client-side validation covers required strings, positive integer fields, valid status range ordering, and comma-separated label normalization. Unknown or deeper validation errors are surfaced from the API through the existing `ApiError` path.

## Testing

Required focused tests:

- `web/src/lib/api.test.ts`: `createTarget` and `createProbeItem` POST helpers.
- `web/src/pages/TargetsPage.test.tsx`: opens Target create panel, validates required execution labels, posts expected payload, and navigates to the created Target detail route.
- `web/src/pages/TargetDetailPage.test.tsx`: empty-state add action creates a ProbeItem, posts the kind-specific config, and appends the created record to the list.
- Existing Go handler tests remain the backend contract check; add a small test only if frontend work exposes a missing backend validation case.

Full verification remains:

- `go test ./...`
- `cd web && npm test -- --run`
- `cd web && npm run build`
- `./scripts/verify.sh`

## Self-Review

No placeholders remain. The scope is one implementation slice: Target creation and ProbeItem creation only. The design follows the frozen V1 sequence and explicitly excludes edit/delete/status work that belongs to later slices.
