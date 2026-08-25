# Primary static, cross-layer, and delivery audit

Reviewed on 2026-08-23 (Asia/Shanghai) at
`08730e7991f3242ed43fcad561cde1f3ea60b6fb`. Local `HEAD`, `main`,
`origin/main`, and the GitHub `main` ref resolved to that same commit. The
review branch was `codex/vps-detail-refactor-completion-audit`, with hooks from
`.githooks` enabled.

This was a findings-only review. No product code, tests, specs, archived task
artifacts, configuration, migrations, CI, Git refs, PRs, releases, or external
data were changed.

## Findings

### Critical

None found in the reviewed static/current-delivery slice.

### Important 1 — overview anomaly and relation actions lead to missing or inert destinations

**Evidence**

- `/home/murray/code/houfeng/internal/center/vpsoverview/anomalies.go:69`
  emits `/vps/{id}/monitoring`, and `:82` emits `/incidents`. Neither route is
  registered. The actual route table exposes `/monitoring` and `/events` at
  `/home/murray/code/houfeng/web/src/app/router.tsx:142` and `:180`; its wildcard
  redirects unknown paths to `/` at `:183`.
- The IP-quality actions at
  `/home/murray/code/houfeng/internal/center/vpsoverview/anomalies.go:97`, `:111`,
  and `:124`, subscription actions at `:140` and `:154`, lifecycle management
  action at `:170`, and source retry action at `:185` all point back to the
  current `/vps/{id}` overview rather than the owned destination or command.
- All four relation kinds are also assigned the current overview route at
  `/home/murray/code/houfeng/internal/center/store/vps_overview.go:264`, `:268`,
  `:274`, `:280`, and `:289`.
- The web renders these server strings as unconditional router links at
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewAnomalies.tsx:35`
  and
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewRelations.tsx:17`.
  A same-`vpsId` navigation does not re-run the overview gate because its effect
  depends only on `vpsId` at
  `/home/murray/code/houfeng/web/src/pages/VPSDetailPage.tsx:36` and `:82`.

**Impact**

Common decision-entry actions are not usable. “查看监控” and “查看事件” leave
the intended VPS context and are redirected to the dashboard. IP-quality,
renewal, management, retry, and relation links remain on the same overview and
do not open the corresponding workflow. This violates the approved explicit
route/action contract and prevents the overview from functioning as a decision
surface.

**Bounded reproduction**

1. Open an overview whose snapshot produces `monitoring.health_abnormal` and
   activate “查看监控”; the generated URL is `/vps/<id>/monitoring`, which falls
   through the router wildcard to `/`.
2. Produce `monitoring.incidents_open` and activate “查看事件”; `/incidents`
   likewise falls through to `/` although the current event route is `/events`.
3. Produce an IP, renewal, lifecycle, or source-unavailable anomaly, or activate
   any relation card; the target is the already-mounted `/vps/<id>` route, so no
   owned page, management panel, or refresh command runs.

**Why tests did not prevent it**

- `/home/murray/code/houfeng/internal/center/vpsoverview/anomalies_test.go:30`
  checks rule presence and non-nil secondary actions but never validates action
  IDs against route/command destinations.
- `/home/murray/code/houfeng/internal/center/store/vps_overview_test.go:75`
  checks only the relation count, not each relation route.
- `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewPageView.test.tsx:137`
  uses `/vps/vps_001` as its anomaly fixture route and only asserts that a link
  exists at `:155`.
- The focused suites still pass: the two Go packages passed, and all 21 selected
  web tests passed, demonstrating the missing cross-layer assertion.

**Minimal direction**

Define one frontend-recognized action/destination contract and validate server
actions against it. Use registered destinations for event, monitoring,
IP-quality, subscription, and relation navigation; dispatch management and
retry as commands rather than same-route links. Add route-map tests spanning
the Go read model and React router/callback owner.

### Important 2 — unknown transport, decoding, and successful-response contract failures silently open the legacy page

**Evidence**

- The canonical entry documents that only capability-off or an unavailable
  endpoint may fall back, while real overview errors must surface, at
  `/home/murray/code/houfeng/web/src/pages/VPSDetailPage.tsx:20`.
- Its catch branch classifies explicit `ApiError` values but sends every other
  error to `legacy` at
  `/home/murray/code/houfeng/web/src/pages/VPSDetailPage.tsx:60` and `:76`.
- `/home/murray/code/houfeng/web/src/lib/apiRequest.ts:110` parses every
  successful response with raw `JSON.parse` at `:116`; malformed JSON throws a
  `SyntaxError`, and a failed `fetch` can throw a `TypeError`, neither of which
  is an `ApiError`.
- `/home/murray/code/houfeng/web/src/lib/recordsApi.ts:512` casts an `unknown`
  response to `Partial<VPSOverview>` without a runtime schema. A successful `{}`
  response is normalized to an empty capability set at `:579`, and the gate at
  `:588` therefore selects legacy. A successful `null` response throws while
  reading `identity`, which the broad catch also converts to legacy.

**Impact**

A network interruption, proxy/body corruption, incompatible 2xx payload, or
server contract regression is presented as an intentional feature-off state.
The user sees the much larger legacy workbench instead of “无法加载 VPS 概览”,
and the failure is hidden from both diagnosis and the fail-closed overview
contract. The legacy graph can also issue additional requests during an outage.

**Bounded reproduction**

1. Stub `getVPSOverview` to reject with `new TypeError('Failed to fetch')` or a
   `SyntaxError`; the page renders the legacy shell.
2. Return HTTP 200 with `{}`; normalization yields `capabilities=[]`, and the
   legacy shell renders.
3. Return HTTP 200 with `null` or invalid JSON; the raw decode/normalization
   exception follows the same legacy branch rather than the `error` state.

**Why tests did not prevent it**

`/home/murray/code/houfeng/web/src/pages/VPSDetailPage.test.tsx:467` through
`:497` covers only `ApiError` 404, 503, and 500 cases. It has no unknown-error,
malformed-JSON, empty-2xx, or invalid-DTO case. Those existing tests all passed
in the focused 21-test run.

**Minimal direction**

Allow legacy fallback only for an explicit, allowlisted capability-unavailable
signal or a runtime-validated capability-off DTO. Validate the successful
overview response before normalization, and map all unknown transport/decode/
contract failures to the visible gate error. Add focused tests for `TypeError`,
`SyntaxError`, `null`, `{}`, and structurally invalid 2xx bodies.

### Important 3 — the overview drops local failure/freshness state and reports renewal deadlines as future observation times

**Evidence**

- The current authority requires every overview section to carry
  `ready|stale|unavailable`, observation time, last-success time, and a safe
  reason, and requires monitoring/IP/subscription/activity failures to degrade
  only their section with freshness and retry. See
  `/home/murray/code/houfeng/.trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/prd.md:27`,
  `:31`, and acceptance coverage at `:48`. The corresponding data contract is
  `/home/murray/code/houfeng/.trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/design.md:135`,
  with section-failure presentation at `:159` and `:161`.
- The renewal repository sets `section.ObservedAt` and
  `section.LastSuccessAt` to the next renewal deadline at
  `/home/murray/code/houfeng/internal/center/store/vps_overview.go:251` through
  `:254`. A healthy subscription due five days from now therefore claims its
  source was observed and last succeeded five days in the future.
- Relation failures retain only `Status: "unavailable"` at
  `/home/murray/code/houfeng/internal/center/store/vps_overview.go:282` and
  `:291`; they carry no section freshness/retry owner.
- The summary UI reads only `status` and `detail` at
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewSummaryGrid.tsx:18`
  through `:26`, discarding each cell’s `section.state`, `observed_at`,
  `last_success_at`, and `reason_code`.
- The only working section retry note is gated on recent-activity or monitoring
  unavailability at
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewPageView.tsx:85`;
  IP, renewal, and relation failures do not receive it. The source-unavailable
  anomaly’s apparent retry is instead the inert same-route link from Important
  1.

**Impact**

IP, subscription, or relation-source failures can look like ordinary summary
values with no trustworthy freshness or working retry. Operators cannot tell a
negative fact from an unavailable source. In the healthy renewal case, the API
publishes a future “last success” timestamp, which is a false freshness fact and
can corrupt operator judgement or downstream presentation.

**Bounded reproduction**

1. Make the subscription or IP-quality repository fail while monitoring and
   activity remain ready. The API carries an unavailable section/anomaly, but
   the summary cell renders no state, safe reason, last success, or local retry.
2. Make service/domain relation loading fail; the card displays a string status
   and its current-route link, with no section freshness or working recovery
   action.
3. Load one active subscription with a renewal date five days in the future;
   `observed_at` and `last_success_at` equal that future business deadline.

**Why tests did not prevent it**

- `/home/murray/code/houfeng/internal/center/store/vps_overview_test.go:75`
  creates exactly the five-days-future renewal fixture at `:77` but asserts only
  identity, subscription count, and relation count through `:112`; it never
  checks section timestamps, failure states, or routes.
- `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewPageView.test.tsx:32`
  supplies section fields but its assertions beginning at `:107` never inspect
  them and include no IP, renewal, or relation-failure fixture.
- The focused Go packages and all 21 selected web tests passed despite these
  missing contracts.

**Minimal direction**

Populate renewal observation/last-success from the actual source read or
authoritative persisted observation time, never the renewal deadline. Preserve
and render per-section state, safe reason, and last-success information for all
approved sections. Give each failed section a real callback-owned retry and add
backend/Web tests for every local failure plus a “no future freshness” invariant.

### Minor

None found after excluding subjective cleanups, historical metadata drift, the
explicit permanent-delete boundary, and the three accepted deferrals.

## Reviewed surfaces without an additional finding

### Current specifications and callers

Every current spec named by the task manifests was opened from disk and read in
full. The review followed current callers rather than treating archived child
claims as proof:

- `/vps/:id` router and capability gate → overview hook/page/management owner →
  `recordsApi` → overview handler/service/repository and its monitoring,
  IP-quality, subscription, activity, service, and domain sources;
- Records routes and handlers → trusted actor context → record read/write,
  revision, draft, attachment, evidence, Markdown, search, comparison,
  collaboration/notification, portability, activity, archive/restore, backup,
  recovery, and readiness owners;
- migration `0052` through `0059`, current APP ACL manifest/admission, handwritten
  SQL, cursor/watermark/projection ownership, Blob local/S3 ownership, and
  bootstrap/config wiring;
- overview management controllers and current asset/subscription/lifecycle APIs,
  including request-generation guards, route switches, submit locks, refresh
  warnings, and focus-return ownership.

The three Important items above were the only evidence-backed current defects
found in that static slice. The absence of another finding here is not a claim
that the main session’s full, strict, or browser gates have passed freshly.

### Permission and non-leakage review

No separate permission or confidentiality finding was identified in the
reviewed current paths:

- Record detail authorization precedes revision payload reads at
  `/home/murray/code/houfeng/internal/center/records/read_service.go:181` through
  `:207`; list and revision paths reauthorize before materialization at `:253`,
  `:263`, `:323`, and `:360`.
- The sole v1 policy exposes only stable `ErrDenied` text while retaining a
  resource-free in-process reason at
  `/home/murray/code/houfeng/internal/center/recordauth/policy.go:21`, and it
  fails closed on project, role, visibility, capture, and live/tombstoned source
  scope at `:56` and `:72` through `:95`.
- Search hydration is constructed with that same read service at
  `/home/murray/code/houfeng/cmd/houfeng-center/bootstrap.go:939`.
- Evidence reads intersect record authorization with the current source at
  `/home/murray/code/houfeng/internal/center/evidence/service.go:266` through
  `:279`.
- Attachment delivery authorizes before lease issuance at
  `/home/murray/code/houfeng/internal/center/attachments/download.go:465` through
  `:480` and reasserts content plus current authorization while streaming at
  `:955` through `:1000`.
- The inspected handlers use trusted request actor context, private/no-store
  responses, bounded strict JSON decoding, stable opaque not-found/denial
  behavior, and the Markdown/attachment/portability surfaces sanitize or
  allowlist rendered/imported content rather than exposing raw locators,
  credentials, grants, tokens, or internal authorization evidence.

### Permanent deletion remains fail-closed

The intended absence is intact and is not a missing-feature finding:

- Both Records and permanent-delete flags default false; an attempted enabled
  permanent-delete configuration is rejected at
  `/home/murray/code/houfeng/internal/center/config/config.go:90` through `:110`.
- Production returns `handlers.RecordDeletions(nil)` at
  `/home/murray/code/houfeng/cmd/houfeng-center/bootstrap.go:1049` through `:1052`.
- User capabilities expose archive/restore but never permanent delete at
  `/home/murray/code/houfeng/internal/center/records/read_service.go:477` through
  `:488`.
- The registry requires all deletion, recovery, authority, backup, and restore
  members at
  `/home/murray/code/houfeng/internal/center/recordreadiness/types.go:47` through
  `:68`, and any missing/unhealthy member keeps the feature disabled at
  `/home/murray/code/houfeng/internal/center/recordreadiness/registry.go:118`
  through `:143`.
- Current bootstrap supplies seven deletion adapters and four recovery members
  at `/home/murray/code/houfeng/cmd/houfeng-center/bootstrap.go:1065` through
  `:1118`, but intentionally omits
  `deletion.record_markdown_client`, `deletion.record_comparison`,
  `recovery.record_search`, `recovery.record_collaboration`,
  `recovery.record_portability`, `backup.orchestration`, and `restore.replay`.
  Backup/restore pairing is also enforced at
  `/home/murray/code/houfeng/internal/center/recordreadiness/registry.go:68`
  through `:77`.

Ordinary record archive/restore and disposable-environment rebuild were kept
separate from irreversible single-record deletion throughout this review.

### Accepted deferrals remain correctly deferred

None of the three current-authority triggers is true, and current code does not
claim delivery:

1. **Activity group-granted digest:** viewer filtering still permits only the
   project-visibility digest at
   `/home/murray/code/houfeng/internal/center/activity/auth_filter.go:13` through
   `:35`; authorization has not expanded to a group-granted digest set.
2. **390 px sticky body-row headers:** the comparison matrix provides a named,
   focusable horizontal scroll region and semantic row headers at
   `/home/murray/code/houfeng/web/src/pages/records/compare/ComparisonMatrix.tsx:35`
   through `:63`, but does not claim sticky body rows. No new reproducible
   positioning failure was established by this static review.
3. **4 GiB / 512 MiB, fixed-arrival, three-round mixed-load harness:**
   `/home/murray/code/houfeng/scripts/run-records-capacity.sh:49` through `:80`
   remains a bounded focused Go/PostgreSQL runner with scale `0.001` and skip
   rejection. It does not claim the deferred formal harness, and no approved
   formal SLO/hardware trigger was found.

## Git and GitHub delivery reconciliation

### Ancestry and scope

- `2cbeb1bb^1` resolved to program baseline
  `d38a8cad382667822059188e46afc31f096f8916`.
- Selected child baseline `7858b30c` is an ancestor of `2cbeb1bb`, which is an
  ancestor of reviewed `HEAD`; overview merge `af23844a` is an ancestor of
  release target `ab1ad7cd`, which is an ancestor of `HEAD`; correction selected
  SHA `35ade851` is an ancestor of merge `38a5524d`, which is an ancestor of
  `HEAD`.
- The baseline-to-HEAD review span contains 193 commits and 1,059 changed paths.
  Current consumers and later fixes were reviewed in addition to the historical
  child diffs.

### PR heads, protected-main merges, and checks

Live GitHub state matched the selected/merge claims below. Every listed PR is
merged, and each had all seven required checks successful: `docker-image`, `go`,
three PostgreSQL 16 catalog variants, `web`, and `web-browser`.

| Scope | PR | Selected/head | Protected-main merge | Main CI run |
| --- | ---: | --- | --- | ---: |
| Platform foundation | #394 | `7858b30c` | `2cbeb1bb` | 30751460764 |
| Records core | #397 | `ba5f2d8d` | `2279a7fd` | 30874511041 |
| Attachments/storage | #400 | `1887821c` | `78bf44c1` | 31317881804 |
| Evidence platform | #408 | `9ac3e255` | `6a0122a7` | 31981626635 |
| Collaboration | #410 | `c6742c3c` | `a3137864` | 32045454459 |
| Markdown workspace | #413 | `199d2a2b` | `d41c8630` | 32123291411 |
| Search center | #416 | `c01663df` | `bcd8e53a` | 32212463173 |
| Activity/overview | #422 | `b9698ffa` | `b3901e5f` | 32344976169 |
| Comparison workbench | #423 | `9924eae0` | `aacb9c50` | 32442306154 |
| Portability/migration | #425 | `808fda62` | `9e910d7c` | 32466708721 |
| Archive/restore fidelity | #428 | `7ce5dbdd` | `c7081519` | 32475409091 |
| Integration rollout | #433 | `1a440e86` | `79f62aac` | 32497370438 |
| musl/S3 correction | #436 | `35ade851` | `38a5524d` | 32542193350 |
| Overview management | #438 | `7e9080f2` | `af23844a` | 32637395760 |
| Current-authority audit artifact | #441 | `290a5c6d` | `6e9be76e` | 32640659843 |
| Parent archive artifact | #442 | `36d2f808` | `8615679c` | 32641517555 |
| Receipt/current-main artifact | #443 | `e802bb07` | `08730e79` | 32642179923 |

For every merge SHA above, the corresponding post-merge `main` push had both
`ci` and `Release Please` complete successfully. PRs #441–#443 are task/delivery
artifacts after the product release and were not misclassified as released
product changes.

### Release and image

- Git tag and published, non-draft, non-prerelease release `v0.75.0` both target
  `ab1ad7cdaab4a7ee57b782a3a9a45e5074b591bd`; the release was published at
  `2026-08-23T11:49:42Z`.
- Release assets include Linux amd64/arm64 agent binaries, `sha256sums.txt`, and
  its minisign signature, with uploaded asset digests present.
- Release-triggered `publish-images` run `32637639621` completed successfully,
  including resolve, credentials, agent-assets, amd64 build, arm64 build, and
  publish jobs.
- `docker.io/linnea7171/houfeng:v0.75.0` resolved during the audit to OCI index
  digest
  `sha256:22df0845c806f69f9d4bccecf02227b744b9588e73de86eb03338c068be14415`,
  with Linux amd64 and Linux arm64 platform manifests plus attestations.

The release therefore contains the reviewed overview-management product merge.
The three Important findings are current product defects despite an intact
protected delivery chain.

### Archived task metadata limitation

The archived parent and all twelve functional child directories are present and
marked completed, as is the overview-management task. Several older child
`task.json` files nevertheless omit `commit`/`pr_url`, and the comparison task
records two raw SHAs rather than the selected/protected-main pair. This is
historical receipt incompleteness, not a product-code finding: live GitHub state,
Git ancestry, required checks, post-merge CI, release, and image evidence above
provide the higher-authority delivery reconciliation.

## Verification performed by this reviewer

Focused, read-only checks only; the main session owns the long full/strict gates.

```text
go test ./internal/center/vpsoverview ./internal/center/store \
  -run 'VPSOverview|EvaluateAnomalies' -count=1
PASS: 2 packages

npm --prefix web test -- --run \
  src/pages/VPSDetailPage.test.tsx \
  src/pages/vps-detail/VPSOverviewAnomalies.test.tsx \
  src/pages/vps-detail/VPSOverviewPageView.test.tsx
PASS: 3 files, 21 tests
```

Read-only Git/GitHub/registry reconciliation also covered local/remote refs,
merge-base ancestry, PR heads and merge commits, all required check conclusions,
post-merge main workflows, release/tag/assets, image-publish workflow jobs, and
the current Docker OCI index/architectures.

## Residual limits and verdict

This sub-review intentionally did not run the full Go gate, Node 22 full Web
gate, complete Playwright/production-preview browser inspection, strict
PostgreSQL security/capacity/integration gates, MinIO/S3 gates, or local/S3
recovery gates. It does not claim those current gates or 1440×1000/390×900
manual geometry/Axe observations are fresh; the main session owns them.

Static/current-spec review and protected-delivery reconciliation found **zero
Critical, three Important, and zero Minor** findings. Important 1 breaks the
overview’s primary destinations, Important 2 masks real failures as feature-off
fallback, and Important 3 discards required local-failure/freshness semantics
while publishing false future renewal freshness. On this evidence, the
unconditional conclusion that the approved refactor is complete and needs no
further work is not supportable. Fixes remain out of scope for this findings-only
task and require separate authorization.
