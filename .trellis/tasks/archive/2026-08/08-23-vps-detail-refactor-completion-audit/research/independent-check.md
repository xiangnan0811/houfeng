# Independent Trellis check

- Audit date: 2026-08-23 (Asia/Shanghai)
- Frozen commit: `08730e7991f3242ed43fcad561cde1f3ea60b6fb`
- Branch: `codex/vps-detail-refactor-completion-audit`
- Scope: independent, findings-only review of the VPS detail refactor and its
  Records support chain. No product code, tests, specs, configuration, migrations,
  Git refs, delivery state, or external data were changed.

> **Closure note (2026-08-25):** this report is the historical independent review
> of frozen commit `08730e79`. Its four Important findings were subsequently fixed
> by PR #444, the React Router production finding was closed in the same remediation,
> and post-release CI reliability was closed by PR #446. The superseding current-main
> reconciliation and zero-finding verdict are recorded in `final-audit-report.md`.

The reviewer loaded the complete current specs listed by both task manifests,
the active PRD/design/implementation artifacts, the archived parent and twelve
functional-child artifacts, and the overview-management closeout artifacts. The
current code paths and their callers were then reviewed at the frozen commit.

## Findings (not fixed — findings-only scope)

### Critical

None.

### Important

#### I-01 — Overview anomaly and relation actions do not land on their promised operation

- **Files:**
  - `/home/murray/code/houfeng/internal/center/vpsoverview/anomalies.go:55`
  - `/home/murray/code/houfeng/internal/center/store/vps_overview.go:258`
  - `/home/murray/code/houfeng/web/src/app/router.tsx:119`
  - `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewAnomalies.tsx:35`
  - `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewRelations.tsx:17`
  - `/home/murray/code/houfeng/web/src/pages/vps-detail/hooks/useVPSManagementController.ts:25`
- **Issue:** the backend presents route strings as executable primary actions and
  relation destinations, and the Web renders every non-empty route as a link.
  Several destinations are missing or are the already-open overview route:
  - monitoring anomalies emit `/vps/{id}/monitoring` at
    `anomalies.go:69-71`, but the registered VPS routes are only IP quality,
    activity, records, evidence, and the base detail route at
    `router.tsx:121-125`;
  - incident anomalies emit `/incidents` at `anomalies.go:82-84`, while the
    registered event destination is `/events` at `router.tsx:180`;
  - all IP-quality actions emit `/vps/{id}` at `anomalies.go:97-99`,
    `:111-113`, and `:124-126`, even though the registered detail destination is
    `/vps/{id}/ip-quality`;
  - subscription actions, management, and retry emit the current `/vps/{id}`
    route at `anomalies.go:140-142`, `:154-156`, `:170-172`, and `:185-187`.
    The management controller is local button state only
    (`useVPSManagementController.ts:25-35`), so same-route navigation neither
    opens management nor refreshes the overview;
  - all four relation kinds use the same current route at
    `vps_overview.go:264-295`, including services and domains, despite being
    presented as separate linked resources.
  The wildcard route redirects unregistered paths to `/` at `router.tsx:183`.
- **Impact:** the decision surface tells an operator that a concrete next step is
  available, but clicking can redirect to the dashboard or leave the operator
  on the unchanged overview. Monitoring incidents, IP-quality diagnosis,
  subscription handling, management, retry, and relation navigation therefore
  cannot reliably complete the task advertised by the action label.
- **Reproduction:** return any corresponding anomaly (for example
  `monitoring.health_abnormal`) or relation from the overview endpoint and click
  its rendered link. `/vps/{id}/monitoring` and `/incidents` hit the wildcard;
  current-route actions do not invoke `onRefresh` or `management.openPanel`.
- **Why tests missed it:** `/home/murray/code/houfeng/internal/center/vpsoverview/anomalies_test.go:30-134`
  checks that each rule exists but not its destination. The page test at
  `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewPageView.test.tsx:137-157`
  supplies the current route and asserts only that a link exists. The store test
  at `/home/murray/code/houfeng/internal/center/store/vps_overview_test.go:100-113`
  checks only relation count, not route landability.
- **Minimum fix direction:** define one tested owner for overview destinations;
  return only registered, task-completing routes, and represent refresh or
  management as commands rather than same-route links. Add router-level tests for
  every anomaly/relation action and omit or disable actions that have no real
  destination.

#### I-02 — Unknown transport and successful-body decode failures silently select legacy

- **Files:**
  - `/home/murray/code/houfeng/web/src/pages/VPSDetailPage.tsx:20`
  - `/home/murray/code/houfeng/web/src/lib/apiRequest.ts:110`
  - `/home/murray/code/houfeng/web/src/lib/recordsApi.ts:403`
  - `/home/murray/code/houfeng/web/src/lib/recordsApi.ts:512`
- **Issue:** the gate comment limits legacy fallback to capability-off or an
  unavailable/missing overview endpoint (`VPSDetailPage.tsx:20-25`), but the
  catch branch sends every non-`ApiError` to legacy at
  `VPSDetailPage.tsx:60-77`. `requestJSON` performs an unchecked `JSON.parse` at
  `apiRequest.ts:110-117`, so malformed JSON throws `SyntaxError`; network/fetch
  failures throw `TypeError`; both silently fall back. Successful JSON also has
  no runtime shape validation: `normalizeVPSOverview` casts `unknown` to a
  partial DTO at `recordsApi.ts:512-513` and manufactures missing identity,
  summary, collections, and capabilities at `:514-580`. A `200 {}` therefore
  becomes an empty capability list and selects legacy at
  `VPSDetailPage.tsx:53-58`; `200 null` reaches a property-access `TypeError` and
  also selects legacy.
- **Impact:** an outage, proxy/body corruption, or frontend/backend DTO drift is
  presented as a legitimate feature-off state. Operators see stale legacy
  composition instead of a truthful error and retry path, masking deployment or
  compatibility failures and defeating the documented capability gate.
- **Reproduction:** make `/api/vps/{id}/overview` reject fetch, return malformed
  JSON, return `200 null`, or return `200 {}`. The first three enter the
  non-`ApiError` legacy branch; the last normalizes to no capabilities and also
  renders legacy.
- **Why tests missed it:** `/home/murray/code/houfeng/web/src/pages/VPSDetailPage.test.tsx:467-497`
  covers explicit 404, explicit 503 fallback, and explicit 500 error only. It
  does not cover `TypeError`, `SyntaxError`, `null`, `{}`, or an invalid 2xx DTO.
  The broad normalizer has no overview decoder contract test. This also conflicts
  with the current rule that a whole missing/failed response must not be
  converted to a successful empty state at
  `/home/murray/code/houfeng/.trellis/spec/web/state-and-data.md:851` and that
  runtime `unknown` parsing should expose DTO drift at `:1570`.
- **Minimum fix direction:** validate the complete overview success envelope at
  the API boundary and return a typed decode error. Allow legacy only for the
  explicit feature/capability-off contract; render all transport and decode
  failures as retryable errors. Add focused tests for each failure class above.

#### I-03 — Overview couples source budgets and drops or falsifies required local-failure freshness

- **Files:**
  - `/home/murray/code/houfeng/internal/center/vpsoverview/service.go:139`
  - `/home/murray/code/houfeng/internal/center/store/vps_overview.go:74`
  - `/home/murray/code/houfeng/internal/center/store/vps_overview.go:223`
  - `/home/murray/code/houfeng/internal/center/vpsoverview/types.go:83`
  - `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewSummaryGrid.tsx:14`
  - `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewRecentActivity.tsx:13`
  - `/home/murray/code/houfeng/web/src/pages/vps-detail/VPSOverviewPageView.tsx:85`
- **Issue:** current authority requires monitoring, IP, subscription, relations,
  and activity to degrade independently with per-section state, observation,
  last success, safe reason, and retry
  (`/home/murray/code/houfeng/.trellis/tasks/archive/2026-08/07-14-vps-records-activity-overview/prd.md:27-31`,
  `:47-48`; design `:135-143`, `:159-161`, and `:172`). The implementation
  violates that contract in four connected ways:
  1. `service.go:139-145` gives the entire non-activity `LoadSources` call one
     timeout. `vps_overview.go:95-109` then reads monitoring, IP, renewal, and
     relations sequentially with that same context. One slow early source can
     exhaust the shared context and cascade later, otherwise independent
     sections into failure; no per-source budget exists.
  2. `RelationSummary` has no `SectionState` at `types.go:123-130`. Relation read
     errors are reduced to `count: 0, status: unavailable` at
     `vps_overview.go:278-295`, while monitoring/subscription relation counts can
     look like ordinary zeroes. `snapshotFromBundle` reports only monitoring,
     IP, and renewal unavailability at `service.go:292-304`, so relation failure
     has no unified anomaly/freshness/retry envelope.
  3. a successful renewal read assigns the future business deadline
     `nextRenew` to both `ObservedAt` and `LastSuccessAt` at
     `vps_overview.go:251-254`. A subscription due five days from now is thus
     reported as having been observed and successfully read five days in the
     future.
  4. the Web summary renders only status/detail and discards `cell.section` at
     `VPSOverviewSummaryGrid.tsx:18-27`; recent activity receives only items at
     `VPSOverviewRecentActivity.tsx:6-18`. The sole retry notice checks activity
     or monitoring at `VPSOverviewPageView.tsx:85-94`, not IP, renewal, or
     relations, and it does not display last-success/reason.
- **Impact:** one slow source can make unrelated sections fail, while isolated
  IP/subscription/relation failures can appear as normal `unknown`, zero, or
  empty data without safe freshness or recovery. The future renewal timestamp is
  a false operational fact. This undermines the overview's role as a trustworthy
  decision surface even when identity remains available.
- **Reproduction:** delay monitoring until the shared source timeout and observe
  that later context-aware readers receive the expired context. Separately,
  return one active subscription with `renew_at = now + 5 days`; the overview
  emits `summary.renewal.section.observed_at` and `last_success_at` at that future
  deadline. Fail only a service/domain relation read and observe no relation
  section state or retry contract.
- **Why tests missed it:** `/home/murray/code/houfeng/internal/center/store/vps_overview_test.go:75-113`
  creates exactly the future renewal fixture but asserts only identity, counts,
  and relation length. There is no slow-source/cascading-timeout matrix, no
  relation failure-state assertion, and page fixtures do not assert visible
  section state, last success, reason, or retry coverage.
- **Minimum fix direction:** give each source an independent bounded read within
  an overall budget; add relation `SectionState`; preserve a real read/observation
  time distinct from `next_renew_at`; and render every degraded section with safe
  freshness and a working retry. Add table tests for all five local failures,
  early-source timeout isolation, and future renewal dates.

#### I-04 — S3 verification scripts false-green failed cleanup and leave privileged MinIO state

- **Files:**
  - `/home/murray/code/houfeng/scripts/run-records-integration.sh:43`
  - `/home/murray/code/houfeng/scripts/run-records-integration.sh:48`
  - `/home/murray/code/houfeng/scripts/run-records-integration.sh:85`
  - `/home/murray/code/houfeng/scripts/run-records-recovery.sh:54`
  - `/home/murray/code/houfeng/scripts/run-records-recovery.sh:59`
  - `/home/murray/code/houfeng/scripts/run-records-recovery.sh:96`
- **Issue:** both required S3 harnesses create a user-owned temporary workspace,
  then bind-mount `$workspace/minio` into a MinIO container without a host-user
  mapping (`run-records-integration.sh:90-99`,
  `run-records-recovery.sh:101-110`). MinIO therefore creates root-owned nested
  state on this host. Cleanup subsequently runs unprivileged `rm -rf`, but masks
  every removal error with `|| true` at `run-records-integration.sh:48-58` and
  `run-records-recovery.sh:59-69`; the trap exits with the pre-cleanup suite
  status, so a passed suite remains green even though teardown failed.
- **Observed evidence:** the fresh integration S3 run supplied by the root audit completed its
  selected suites, emitted repeated `rm: ... Permission denied`, and returned
  success. Independent inspection after that run found
  `/tmp/houfeng-records-integration.kfusdi/minio/.minio.sys` and its children
  owned by `root:root`, while the outer workspace is owned by `murray:wheel`.
  The undeleted residue was 156 KiB at inspection time. The recovery script has
  the same mount and cleanup implementation, so it has the same deterministic
  ownership/teardown defect even though this checker did not launch a second S3
  recovery run merely to create another privileged residue.
- **Impact:** the repository's required S3 integration/recovery gates report a
  false-clean success and leave data the invoking user cannot remove normally.
  Repeated verification accumulates privileged `/tmp` artifacts, consumes the
  shared temp quota, and makes later runs less repeatable. This does not negate
  the functional assertions that completed before teardown, but it prevents the
  S3 harness lifecycle from qualifying as a clean, reproducible completion gate.
- **Reproduction:** as a non-root Docker user, run
  `scripts/run-records-integration.sh --profile s3`, allow the tests to pass,
  and inspect the workspace named in the cleanup errors. Root-owned
  `.minio.sys` entries remain while the script returns the child test status.
  The recovery script follows the same path with `--profile s3 --all`.
- **Why tests missed it:**
  `/home/murray/code/houfeng/internal/center/recordbackup/profile_script_test.go:18-34`
  and
  `/home/murray/code/houfeng/internal/center/recordbackup/recovery_script_test.go:18-34`
  are string-presence tests that explicitly require
  `rm -rf "$workspace" || true`; they do not execute the container lifecycle,
  verify ownership, assert workspace removal, or propagate cleanup failure.
- **Minimum fix direction:** make MinIO storage lifecycle-owned by the invoking
  user (for example, a tracked Docker volume or a verified host UID/GID mapping),
  remove the unconditional cleanup-success mask, preserve the original suite
  failure while also failing a successful suite when teardown fails, and add a
  real S3 harness assertion that no container, volume, or workspace remains.

### Minor

None.

## Findings (fixed)

None. The approved audit scope forbids product/spec/test/configuration fixes; the
only write by this reviewer is this report.

## Candidate disposition

| Candidate from `primary-static-audit.md` | Independent disposition |
| --- | --- |
| anomaly/relation actions route to missing or inert destinations | Confirmed as Important; refined to distinguish unregistered wildcard destinations from same-route links that cannot invoke management/retry |
| unknown transport/decode failures silently fall back to legacy | Confirmed as Important; expanded to include malformed JSON, `null`, and structurally invalid successful bodies |
| overview drops local-failure/freshness semantics | Confirmed as Important; refined to include the shared sequential timeout that can cascade degradation, in addition to missing relation state/UI freshness and future renewal timestamps |

No fourth evidence-backed **product** finding was established; I-04 is a separate
verification-harness finding confirmed from fresh residue. Broad scans found no
unexplained debug path, `.only`, `@ts-ignore`, or speculative placeholder in the
reviewed surface. The few scoped ESLint suppressions explain React effect
ownership and passed focused lint; environment-gated PostgreSQL tests remain
subject to the strict root-owned runners rather than being counted as passes.

## Reviewed without an additional finding

### Authorization and non-leakage

- The sole record policy uses resource-free denial reasons and validates actor,
  project, role capability, record visibility, capture scope, and current or
  tombstone source floor before access at
  `/home/murray/code/houfeng/internal/center/recordauth/policy.go:5-8` and
  `:56-95`.
- Record detail authorizes current evidence before loading content at
  `/home/murray/code/houfeng/internal/center/records/read_service.go:181-214`;
  list processing skips denied, missing, conflicting, and deletion-reserved
  candidates before returning rows at `:252-288`.
- Search deliberately has no total count and hydrates candidates through the
  authorized record reader at
  `/home/murray/code/houfeng/internal/center/recordsearch/service.go:65-80` and
  `:169-221`.
- Activity derives an authorization filter before the ordered page request and
  unifies live-missing/projected-missing behavior at
  `/home/murray/code/houfeng/internal/center/activity/service.go:181-247`.
  Viewer activity remains project-digest-only and unstamped rows fail closed at
  `/home/murray/code/houfeng/internal/center/activity/auth_filter.go:13-35`.
- The focused authorization/search/activity tests listed below passed. No
  current path was found that exposes unauthorized title, summary, row count,
  identity, cursor, credential, proof, secret, or command output.

### Permanent-delete fail-closed boundary

- Configuration defaults the flag off and rejects every attempt to enable it at
  `/home/murray/code/houfeng/internal/center/config/config.go:90-110`.
- Production wires `handlers.RecordDeletions(nil)` at
  `/home/murray/code/houfeng/cmd/houfeng-center/bootstrap.go:1049-1052`; the nil
  application returns a stable 503 safety/status-unavailable response at
  `/home/murray/code/houfeng/internal/center/http/handlers/record_deletions.go:35-55`
  and `:232-238`.
- Readiness requires the full deletion/recovery/authority/backup/restore matrix
  at `/home/murray/code/houfeng/internal/center/recordreadiness/types.go:47-68`
  and enables only when every required row is healthy/present at
  `/home/murray/code/houfeng/internal/center/recordreadiness/registry.go:118-143`.
- Record capabilities set ordinary archive/restore where authorized but do not
  expose permanent delete at
  `/home/murray/code/houfeng/internal/center/records/read_service.go:477-501`.
  The requested production boundary therefore remains disabled and fail-closed.

### Accepted deferrals and spec synchronization

- Group-granted activity digest remains deferred: viewer filtering currently
  admits only the project visibility digest (`activity/auth_filter.go:13-35`),
  so the future trigger has not silently become active.
- The comparison matrix keeps a named focusable local scroll region and semantic
  row headers; no implementation or documentation claim upgrades this to the
  deferred 390px sticky body-row contract.
- `/home/murray/code/houfeng/scripts/run-records-capacity.sh:49-80` remains a
  bounded scale-`0.001` focused runner with skip rejection; it does not claim the
  deferred formal 4 GiB / 512 MiB fixed-arrival three-round harness.
- No `.trellis/spec/` edit is needed to explain the three product findings: the active
  Web specs and archived current-authority task already state the relevant
  route, error, per-section degradation, and freshness expectations. The drift
  is in implementation and test coverage, not missing documentation. Any future
  fix should decide whether to promote the overview-specific archived contract
  into a current layer spec, but that documentation change is outside this audit.

## Local delivery-state reconciliation

- `HEAD` and branch matched the frozen values above before and after checks; the
  worktree contained only the untracked active task directory.
- `git rev-parse 2cbeb1bb^1` resolved the program baseline to
  `d38a8cad382667822059188e46afc31f096f8916`.
- Every recorded protected-main merge SHA from PR #394 through #443 is an
  ancestor of reviewed `HEAD`. Every selected/head SHA is tree-equal to its
  recorded merge SHA (`git diff --quiet <selected> <merge>`).
- The first seven pairs are two-parent merges whose selected heads are graph
  ancestors. Activity/overview and later pairs are one-parent squash merges:
  their selected heads are **not** graph ancestors even though their trees are
  equal. In particular, `35ade851` is not an ancestor of `38a5524d`; describing
  it that way would be inaccurate. Its same-tree squash inclusion and the merge
  commit's ancestry to `HEAD` are the correct local evidence.
- Live GitHub PR/check/workflow, release assets, registry manifest, and image
  digest were not independently queried by this checker; the root audit owns
  those fresh external checks.

## Acceptance-criteria status mapping

| AC | Independent status | Evidence / blocker |
| --- | --- | --- |
| AC-01 | Partial | Archived tree and local Git inclusion reconciled; live PR/CI/release/image verification remains root-owned. Squash heads are tree-equal, not graph ancestors. |
| AC-02 | Fail | I-01, I-02, and I-03 are unresolved defects in the canonical `/vps/:id` overview path. |
| AC-03 | Partial | Full specs and core Records auth/search/activity/read paths were reviewed with no additional static finding; full strict/integration proof remains root-owned. |
| AC-04 | Partial/pass for audited boundary | Permanent delete is production-disabled/fail-closed and the three deferrals remain accurately deferred; full archive/restore integration evidence remains root-owned. |
| AC-05 | Partial | Focused Go vet/tests and focused Web lint/tests passed; full formatting/vet/test/coverage/build/budget gates were not duplicated here. |
| AC-06 | Fail/partial | The root owns the full suites, but I-04 independently confirms that the required S3 integration/recovery harness lifecycle can return success after failed cleanup. Other full gates were not duplicated here. |
| AC-07 | Not run | No independent production-preview 1440px/390px geometry, keyboard, focus, overflow, or Axe run. |
| AC-08 | Fail | Zero Critical, four Important, zero Minor; unresolved Important findings block unconditional completion. |
| AC-09 | Pass for this reviewer | No write other than this authorized report; no staging, commit, push, ref, PR, release, or external mutation. |
| AC-10 | Pass for this report | Commit, scope, exact evidence, commands, results, limits, and falsifiable verdict are recorded. |

## Verification

All commands ran from `/home/murray/code/houfeng` unless a Web working directory
is explicitly implied.

```text
python3 ./.trellis/scripts/get_context.py --mode packages
PASS — single-repository context, backend and web spec layers identified

git status --short --branch
git rev-parse HEAD
git branch --show-current
PASS — frozen branch/SHA; only active task directory untracked

git merge-base --is-ancestor <merge-sha> HEAD
git diff --quiet <selected-sha> <merge-sha>
git show -s --format=%P <merge-sha>
PASS for all recorded pairs — merges reach HEAD and selected/merge trees match;
merge-vs-squash ancestry distinction recorded above

TMPDIR=/dev/shm go test ./internal/center/vpsoverview ./internal/center/store \
  -run 'VPSOverview|EvaluateAnomalies' -count=1
PASS — 2 packages

TMPDIR=/dev/shm go vet ./internal/center/vpsoverview ./internal/center/store
PASS — 2 packages

TMPDIR=/dev/shm go test ./internal/center/http/handlers ./internal/center/http \
  -run 'VPSOverview|RouterRegistersVPSOverview|RecordDeletionsHandlerFailsClosed' -count=1
PASS — 2 packages

TMPDIR=/dev/shm go test ./internal/center/config ./internal/center/recordreadiness \
  ./internal/center/records \
  -run 'LoadRecordPlatformMode|LoadCenterConfigRejectsUnsupportedRecordPlatformMode|RegistryEvaluateKeepsPermanentDeleteDisabled|RecordAuthorization|RecordReadServiceAuthorizesCurrentSources|RecordReadServiceListSkipsDenied|RecordReadServiceListContinuesPastDenied' \
  -count=1
PASS — 3 packages

TMPDIR=/dev/shm go test ./internal/center/recordauth ./internal/center/activity \
  ./internal/center/recordsearch \
  -run 'AuthorizeEnforcesResourceIntersection|ListSubjectActivity(AuthFilter|ResultJSON|LiveSubject|Tombstone|NotFound)|SearchService(DropsUnauthorizedCandidates|SurfacesReadFaults|ReadsAhead)' \
  -count=1
PASS — 3 packages

NODE_VERSION=22.23.1 /home/murray/.nvm/nvm-exec npm --prefix web test -- --run \
  src/pages/VPSDetailPage.test.tsx \
  src/pages/vps-detail/VPSOverviewAnomalies.test.tsx \
  src/pages/vps-detail/VPSOverviewPageView.test.tsx \
  src/pages/vps-detail/hooks/useVPSOverview.test.tsx \
  src/app/router.test.tsx src/lib/recordsApi.test.ts src/lib/apiRequest.test.ts
PASS — Node v22.23.1, 7 files, 75 tests

NODE_VERSION=22.23.1 /home/murray/.nvm/nvm-exec npm exec -- eslint <17 scoped files>
PASS — focused overview/router/API-client files and their focused tests

stat -c '%A %U:%G %u:%g %n' \
  /tmp/houfeng-records-integration.kfusdi \
  /tmp/houfeng-records-integration.kfusdi/minio
du -sh /tmp/houfeng-records-integration.kfusdi
find /tmp/houfeng-records-integration.kfusdi/minio -maxdepth 2 \
  -printf '%M %u:%g %p\n'
CONFIRMED I-04 — outer workspace murray:wheel; nested .minio.sys root:root;
156 KiB residue remained after the successful S3 integration command
```

The first Go test attempt without `TMPDIR` did not compile because `/tmp`
reported `cgo: write ... disk quota exceeded`. `/tmp` still had filesystem free
space, so this was recorded as an environment quota failure. One bounded retry
using the separate ephemeral `/dev/shm` mount passed; repository state remained
unchanged.

- **Lint:** pass for the focused Web slice.
- **TypeCheck:** not run independently; the root session owns the full Node 22
  build/type gate.
- **Tests:** pass for all bounded slices above. This is not a substitute for the
  full/strict/browser gates.

## Residual limits and verdict

This checker did not run full Go formatting/vet/tests, full Web
coverage/build/bundle/CSS, complete Playwright/production-preview inspection,
strict PostgreSQL security/capacity/integration, independently launch MinIO/S3
or local/S3 recovery, or perform fresh external delivery/registry checks. Those
are deliberately left to the root audit. This checker did independently inspect
the S3 residue recorded under I-04. Static review cannot prove future absence of
defects; this verdict is limited to the frozen commit, reviewed authority, and
commands above.

The independent result is **zero Critical, four Important, and zero Minor**.
The three product candidates are confirmed, with I-03 refined to include cascading shared
source timeouts and I-01 refined to separate missing routes from inert
same-route actions. Permission/non-leakage and permanent-delete boundaries
showed no additional finding, and the accepted deferrals remain valid. I-04 adds
an independently confirmed S3 harness lifecycle defect. Because four Important
findings remain unresolved, the approved VPS detail refactor and its required
verification chain cannot be confirmed complete or as requiring no further work.
