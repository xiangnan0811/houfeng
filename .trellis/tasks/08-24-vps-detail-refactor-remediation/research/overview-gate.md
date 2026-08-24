# Research: I-02 VPS overview success decoding and capability gate

- Query: Map the current API request, VPS overview normalization, and capability-gate contracts; identify existing repository decoder patterns; propose the smallest runtime validation/error-classification design and exact regression/verification commands for audit finding I-02.
- Scope: internal
- Date: 2026-08-24

## Findings

### Files found

- `web/src/lib/apiRequest.ts` — the single shared transport owner; 2xx JSON is parsed at lines 110-117, non-2xx responses become `ApiError` at lines 89-105, while fetch and success-JSON parse failures remain native errors.
- `web/src/lib/apiRequest.test.ts` — lines 202-206 deliberately require malformed successful JSON to propagate as `SyntaxError`; a global transport behavior change would therefore be broader than I-02.
- `web/src/lib/apiError.ts` — allowlists `code`, valid field errors, and recovery metadata for non-2xx Records errors; it is an error-body decoder, not a 2xx success decoder.
- `web/src/lib/recordsApi.ts` — owns the route-lazy Records/overview façade. `getVPSOverview` is `requestJSON<unknown>(...).then(normalizeVPSOverview)` at lines 583-589. The normalizer at lines 512-580 substitutes empty strings/arrays/sections for missing fields instead of validating the success DTO.
- `web/src/lib/types.ts` — lines 3303-3388 define the handwritten `VPSOverview` contract and the `records_v2_read` capability.
- `web/src/pages/VPSDetailPage.tsx` — canonical gate. Lines 50-77 choose overview/legacy/error; all non-`ApiError` failures currently choose legacy. Lines 98-105 render an error without a retry action.
- `web/src/pages/vps-detail/hooks/useVPSOverview.ts` — post-gate loader. Lines 20-36 already classify unknown failures as errors rather than legacy; lines 91-107 retain an already loaded overview on refresh failure.
- `web/src/pages/VPSDetailPage.test.tsx` — current gate coverage at lines 467-497 is limited to 404, explicit 503 overview-unavailable, and 500 `ApiError`; there is no transport/decode/invalid-2xx case or valid capability-off success case.
- `web/src/pages/vps-detail/hooks/useVPSOverview.test.tsx` — covers stale-request suppression, not-found, and the seeded first paint, but not contract/decode failures.
- `web/src/lib/recordsApi.test.ts` — transport façade tests exist, but there is no `getVPSOverview` success/invalid-shape test.
- `web/src/security/recordsTransportArchitectureContract.test.ts` — lines 11-14 constrain `recordsApi.ts` to the existing runtime dependencies `apiRequest` and `apiError`; keeping the decoder local avoids a new runtime edge and avoids weakening this contract.
- `web/e2e/page-states.spec.ts` — lines 268-309 cover overview loading, healthy, and anomaly states; it is the natural production-build fixture test location for invalid 2xx + retry behavior.
- `web/e2e/fixtures/profiles.ts` — lines 611-743 provide the canonical valid overview fixture/profile and exact `/api/vps/vps_001/overview` route.
- `internal/center/vpsoverview/types.go` — lines 27-59 are the Go wire authority and guarantee non-null top-level arrays plus recent-activity items; lines 62-160 define required/optional nested fields.
- `internal/center/http/handlers/vps_overview.go` — lines 17-45 define endpoint behavior; `overview_unavailable` is the explicit service-unavailable code, and `resource_not_found` is the identity/path 404 code.
- `web/src/lib/recordInboxUnreadApi.ts` — small exact runtime validator after `requestJSON<unknown>`; rejects invalid root/key/integer shapes.
- `web/src/lib/recordCollaborationDto.ts` — full fail-closed DTO decoder pattern with object/primitive/array helpers and stable invalid-response errors.
- `web/src/lib/documentMarkdown.ts` — lines 94-113 and 261-295 provide the strongest typed-error pattern: a dedicated error class plus one boundary decoder and small validating helpers.

### Current request-to-render flow

```text
VPSDetailPage gate
  -> recordsApi.getVPSOverview(vpsId)
    -> recordsApi requestJSON wrapper
      -> apiRequest.requestJSON<unknown>()
        -> fetch
        -> non-2xx: ApiError (Records decoder preserves allowlisted code)
        -> 2xx: JSON.parse(raw text) as unknown
    -> normalizeVPSOverview(unknown)
      -> fill missing objects/scalars/arrays with empty values
  -> overviewHasRecordsV2Read(normalized.capabilities)
    -> true: overview route
    -> false: legacy route
```

The resulting failure matrix is:

| Input/failure | Current boundary result | Current gate result | Problem |
| --- | --- | --- | --- |
| Valid overview with `records_v2_read` | `VPSOverview` | overview | Correct |
| Valid overview without capability | `VPSOverview` | legacy | Intended capability-off path |
| HTTP 404 / `resource_not_found` | `ApiError` | not-found | Intended identity path |
| HTTP 503 / `overview_unavailable` | `ApiError` | legacy | Intended explicit service/capability-off path |
| Any HTTP 503 with another/no code | `ApiError` | legacy | Over-broad; authorization or dependency failure can be masked |
| HTTP 500 | `ApiError` | visible error | Correct |
| Fetch/network rejection | native `TypeError` or platform error | legacy | Masks transport failure |
| HTTP 200 malformed JSON | native `SyntaxError` | legacy | Masks response decode failure |
| HTTP 200 `null` | normalizer dereference failure | legacy | Masks invalid DTO |
| HTTP 200 `{}` | synthetic empty overview, empty capabilities | legacy | Treats contract drift as feature-off |
| HTTP 200 with malformed/missing nested fields or non-string capability entries | partially normalized/cast object | legacy or unsafe render | Handwritten TypeScript types provide no runtime validation |

The important invariant is: **legacy is a product capability decision, not a generic error recovery path**. A response must first pass the complete overview success decoder before its capabilities can participate in that decision.

### Existing decoder patterns and what to reuse

1. `recordInboxUnreadApi.ts` demonstrates `requestJSON<unknown>` followed by validation at the owning façade. This is the correct ownership boundary, but its generic `Error` and one-field implementation are too small to copy literally.
2. `recordCollaborationDto.ts` demonstrates closed, recursive decoding with small `object/text/integer/array` helpers. Its exact-key policy is appropriate for a closed security DTO but would make an additive overview response unnecessarily brittle.
3. `documentMarkdown.ts` demonstrates the preferred typed error shape (`InvalidDocumentRenderModelError`) and a single `invalid(): never` exit. This is the closest pattern for I-02.
4. `apiError.ts` must remain the non-2xx decoder. It should not be stretched into success-body validation.
5. `apiRequest.ts` should keep its current generic behavior. The existing test explicitly requires native `SyntaxError` propagation for malformed 2xx JSON, and changing all API consumers is not the smallest safe remediation.

No schema library should be added. The Web dependency policy is intentionally minimal, and this one DTO can be decoded with repository-native helpers without increasing bundle/runtime dependency surface.

### Smallest reliable design

#### 1. Add one overview-specific typed boundary error in `recordsApi.ts`

Use a stable class such as:

```ts
export class InvalidVPSOverviewResponseError extends Error {
  readonly reason: 'malformed_json' | 'invalid_shape'
}
```

The class must not retain the raw response or rejected value. A stable reason is enough for tests/diagnostics and avoids leaking payload data.

Keep this class and the decoder in `recordsApi.ts`. This preserves the architecture contract's two runtime dependencies and is smaller than adding `zod`, a new shared decoder module, or a success-decoder seam to every `requestJSON` caller. The architecture contract only inventories exported function declarations, so exporting the class for `instanceof` tests does not alter its expected function list.

#### 2. Replace permissive normalization with a validating, projecting decoder

`getVPSOverview` should still request `unknown`, but:

- catch only the success-body `SyntaxError` from `requestJSON` and rethrow `InvalidVPSOverviewResponseError('malformed_json')`;
- preserve `ApiError`, fetch/network errors, aborts, and all other transport failures unchanged;
- pass parsed values to `decodeVPSOverview(value)`, which throws `InvalidVPSOverviewResponseError('invalid_shape')` on any required contract mismatch;
- construct a fresh `VPSOverview` from allowlisted fields rather than spreading source objects;
- ignore additive unknown fields after validating all known required fields. Exact-key rejection is not needed for this non-secret read model and would make additive server changes fail unnecessarily.

The minimum complete structural contract, derived from `vpsoverview/types.go` and `types.ts`, is:

- root object required; top-level `generated_at`, `identity`, `anomalies`, `summary`, `recent_activity`, `facts`, `relations`, and `capabilities` all required;
- `identity`: every declared scalar is a string; `labels` is an array of strings;
- each summary cell: string `status`, optional string `detail`, and required section;
- every section: string `state`/`reason_code` and string-or-null `observed_at`/`last_success_at`;
- anomaly: required string `rule_id`/`severity`/`title`/`source`; optional string `detail`; optional string-or-null `event_at`; optional action-or-null `primary_action`; required action array `secondary_actions`; every action has string `id`/`label` and optional string `route`;
- recent activity: required section and item array, optional string `snapshot_cursor`; every item/actor/subject/presentation field consumed by `SubjectActivityItem` must be validated before projection rather than passed through with object spread;
- facts: array of `{key,label,value}` strings;
- relations: string `kind`/`route`/`label`, non-negative safe-integer `count`, optional string `status`;
- capabilities: required array of strings. Only after this succeeds may `overviewHasRecordsV2Read` inspect it.

Do not invent stricter enums or arbitrary array limits in this I-02 slice unless the final task design records them as wire authority. Structural type/presence validation is enough to catch the audited drift without accidentally rejecting a forward-compatible string value. The existing server limit of five recent activity items may be asserted because it is explicit in `vpsoverview/types.go`.

#### 3. Make gate fallback an explicit allowlist

Recommended classification:

| Condition | Gate mode |
| --- | --- |
| Valid decoded response contains `records_v2_read` | `overview` |
| Valid decoded response lacks `records_v2_read` | `legacy` |
| `ApiError` has `resource_not_found` or HTTP 404 | `not_found` |
| `ApiError.code === 'overview_unavailable'` | `legacy` |
| Any other `ApiError`, including another/no-code 503 | `error` |
| `InvalidVPSOverviewResponseError` | `error` |
| Network `TypeError`, abort, or any other unknown error | `error` |

In particular, remove `error.status === 503` as a standalone legacy condition. The backend already emits the explicit `overview_unavailable` code. A generic 503 can represent `authorization_unavailable` or another operational failure and must not be presented as a normal legacy capability state.

The existing comment that a "missing overview endpoint" falls back is not safely implementable from status alone: an untyped 404 cannot distinguish a missing route from a missing/unauthorized VPS. Current backend code registers this endpoint and emits structured codes, so the gate should rely on the explicit code and fail visibly for ambiguous failures rather than guessing.

#### 4. Make the initial gate error visible and retryable

Add a probe revision/attempt state to `VPSDetailPage`; include it in the gate effect dependency and render a `PageState.action` button that increments it. Unknown errors should use a bounded Chinese message such as `VPS 概览请求或响应校验失败，请重试。`; do not render native exception text or raw response content.

The existing `useVPSOverview` post-gate loader already treats unknown failures as errors and never selects legacy. If this slice also promises refresh visibility, pass its retained `errorMessage` into `VPSOverviewPageView` and render a local retry notice while preserving the last valid overview. Otherwise document that retained-overview refresh notice belongs to the I-03 freshness work so the same UI is not implemented twice.

### Exact RED/GREEN tests

#### `web/src/lib/recordsApi.test.ts`

Add `getVPSOverview` and `InvalidVPSOverviewResponseError` imports plus a raw overview wire fixture.

1. Valid full response resolves to an explicitly projected `VPSOverview`, uses exact encoded URL, and strips an additive `internal_debug` field rather than spreading it.
2. Valid response with `capabilities: []` resolves normally; capability absence is a gate decision, not a decoder failure.
3. Malformed successful JSON (`new Response('{', {status: 200})`) rejects with typed reason `malformed_json`.
4. Table-driven invalid root/envelope cases (`null`, `[]`, `{}`) reject with typed reason `invalid_shape`.
5. Table-driven nested cases reject: missing/null identity; non-string identity scalar; non-string labels; missing summary cell/section; invalid section timestamps; null recent-activity items; malformed activity item/subject/presentation; malformed anomaly/action; malformed fact; negative/fractional relation count; non-string capability entry.
6. Additive unknown fields are ignored, not copied into the returned object.

#### `web/src/pages/VPSDetailPage.test.tsx`

1. A valid decoded overview with `capabilities: []` renders the legacy shell.
2. `InvalidVPSOverviewResponseError('invalid_shape')` renders `无法加载 VPS 概览`, does not render the legacy shell, and exposes a `重试` button.
3. A network `TypeError` follows the same visible error path and does not load legacy.
4. Clicking retry after the first contract/network failure issues a second probe and can recover into a valid overview.
5. A 503 with `code: 'overview_unavailable'` still selects legacy (existing case).
6. A 503 with `code: 'authorization_unavailable'`, `internal_error`, or no code renders the error state and never selects legacy.
7. Existing 404/not-found and 500 cases remain green.

#### `web/src/pages/vps-detail/hooks/useVPSOverview.test.tsx`

1. Initial contract error maps to `status: 'error'`, not `unavailable` or an invented overview.
2. If refresh starts from a seeded valid overview and then receives a contract error, `refresh()` resolves `false` and retains the prior overview. If I-02 owns visible refresh feedback, also assert the page renders the bounded retry notice.

#### `web/e2e/page-states.spec.ts`

Add one production-build fixture case with exact `/api/vps/vps_001/overview` returning HTTP 200 and `{}`:

- page displays `无法加载 VPS 概览`;
- legacy content is absent (undeclared legacy API calls would also fail the fixture router);
- `重试` is visible;
- clicking retry increments the exact overview request count from 1 to 2;
- fixture diagnostics remain clean.

Malformed raw JSON is already covered at the transport/façade unit boundary; the fixture router serializes JSON bodies, so `{}` is the appropriate browser-level invalid-success proof.

### Verification commands

Run with the repository-pinned Node 22 runtime:

```bash
PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH \
  NODE_ENV=test npm --prefix web run test -- --run \
  src/lib/apiRequest.test.ts \
  src/lib/recordsApi.test.ts \
  src/pages/VPSDetailPage.test.tsx \
  src/pages/vps-detail/hooks/useVPSOverview.test.tsx \
  src/security/recordsTransportArchitectureContract.test.ts

PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH \
  NODE_ENV=test npm --prefix web run lint

PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH \
  NODE_ENV=production npm --prefix web run build

PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH \
  npm --prefix web run bundle:check

PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH \
  npm --prefix web run test:e2e -- page-states.spec.ts

PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web

PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH \
  npm --prefix web run test:e2e
```

The focused Vitest command is the TDD loop. `make verify-web` is the required source/coverage/build/bundle/CSS gate; the full Playwright run is required before claiming the user-visible gate remediation complete.

### Related specs

- `.trellis/spec/web/state-and-data.md:20-35` — `apiRequest.ts` is the single transport owner; business façades own endpoints; success DTO types are handwritten and must be aligned with Go.
- `.trellis/spec/web/state-and-data.md:53-65` — domain façade dependency/bundle constraints and explicit projection/allowlisting principles.
- `.trellis/spec/web/state-and-data.md:91-100` — API wire tests, page loading/error/data tests, production build, and bundle checks.
- `.trellis/spec/web/state-and-data.md:1462-1494` — page loading/error/data state and unknown-error handling pattern.
- `.trellis/spec/web/state-and-data.md:1569-1582` — runtime `unknown` parsing is the expected mechanism for exposing Go/TypeScript drift.
- `.trellis/spec/web/quality-guidelines.md:23-65` — Node 22 and the authoritative Web command portal.
- `.trellis/spec/web/quality-guidelines.md:121-175` — production Chromium fixture gate and fail-closed diagnostics.
- `.trellis/spec/web/quality-guidelines.md:211-218` — use `unknown` plus narrowing; do not use `any`.
- `.trellis/spec/web/quality-guidelines.md:222-285` — colocated Vitest/RTL conventions and user-visible assertions.
- `.trellis/spec/web/quality-guidelines.md:428-439` — pre-commit focused/full/browser checklist.
- `.trellis/spec/guides/cross-layer-thinking-guide.md:18-50,75-87` — map the boundary, validate once at entry, and test null/empty/invalid shapes.
- `.trellis/spec/guides/code-reuse-thinking-guide.md:18-38,63-74` — search existing patterns, but do not extract a one-use abstraction that is more complex than the local decoder.

### External references

No external library or web reference is needed for this remediation. The authoritative wire contract is the repository's Go type/handler, and the recommended approach deliberately adds no dependency.

## Caveats / Not Found

- 本研究开始时 remediation `prd.md` 尚未填充；当前 parent/child PRD、design 与 implementation
  plan 已结合本结论补齐。本文仍只是 planning input，须在 context manifests/validate 完成并取得
  用户对最终规划的后续明确批准后才能 task start/implementation。
- I-01 may change overview action destination shape, and I-03 is expected to add/adjust per-relation section freshness. The final success decoder must be written against the merged final `VPSOverview`/Go wire contract, not freeze today's action/relation shape and then reject the sibling remediation.
- The current normalizer also handles recent activity through permissive shared helpers. Tightening those shared helpers globally would change `listSubjectActivity`; the smallest I-02 slice should decode overview activity locally unless the task deliberately expands and tests the subject-activity API too.
- A valid capability-off response is distinct from `{}`. Tests must preserve a valid no-capability fixture so future cleanup cannot reintroduce "invalid equals legacy" behavior.
- No product, test, spec, task-planning, Git, or external state was changed by this research; only this research artifact was written.
