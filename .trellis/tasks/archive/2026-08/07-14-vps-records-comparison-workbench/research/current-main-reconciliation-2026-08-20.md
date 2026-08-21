# Child 8 plan reconciliation against `origin/main` `ffda9a07` / `v0.70.0`

Date: 2026-08-20  
Baseline: protected main `ffda9a07` (`chore(main): release 0.70.0`, #420) after Child 7 `#422`.  
This note is the current-main reconciliation required before `task.py start`. It does not authorize implementation.

## 1. Dependency status

| Planned dependency | On main? | Evidence |
|---|---|---|
| Child 2 records core (`0052`) | Yes | live `/api/records*` + `RevisionCommitCommand` |
| Child 4 evidence (`0054`) | Yes | `POST /api/evidence/capture-previews`, `GET /api/evidence/{id}`, 6 registered kinds |
| Child 5 Markdown workspace | Yes | `RecordEditPage` / `RecordRevisionPage` |
| Child 6 search center | Yes | `RecordSearchPage` at `/records` |
| Child 7 activity / VPS overview (`0057`) | Yes | `#422` → `#420` → `v0.70.0` |
| Child 8 comparison | No | only Trellis planning artifacts |
| Root migration | None | `0058` reserved for Child 10 |

`/monitoring/compare` is a live 2-way monitoring A/B tool (`MonitoringComparePage`, `?id=`). It stays. It is not a fallback.

## 2. Real contracts Child 8 must use

### Evidence registry

Registered kinds (closed): `ip_quality.report/v1`, `monitoring.host/v1`, `monitoring.probe/v2`, `monitoring.event/v2`, `subscription.cost/v1`, `command.audit/v1`.

`Kind.Compare(left, right, Alignment) Comparison` is **pairwise**. The only alignment mode is `AlignmentExact`. Monitoring compare requires same kind/schema, calculation version, units, requested-window duration, precision, and bucket width, then returns **aggregate** `metric_deltas` / `quality_deltas`. It does **not** emit gap-aware series, 2–6 item orchestration, `actual_coverage`, or `common_overlap`.

`comparison.result/v1` is not registered. Recovery already fail-closes unknown `comparison.result/*` prefixes.

### HTTP that exists vs planned

| Planned | Reality |
|---|---|
| `POST /api/evidence/comparison-candidates` | Absent. Parent `design.md` §19.2 also omits it; only `POST /api/evidence/comparisons` is in the parent HTTP table. Subject-mode UX is still in parent §8. |
| `POST /api/evidence/comparisons` | Absent |
| `GET /api/evidence/{id}` | Live; `recordauth` + opaque 404; `source_available` bool |
| `POST /api/records` / `POST /api/records/{id}/revisions` | Live; required `Idempotency-Key`; optional `evidence_items[]` as `capture_intent_id XOR existing_snapshot_id` |
| Subject evidence list | `GET /api/subjects/{vps\|monitoring_instance\|target}/{id}/activity?view=evidence` — not a dedicated evidence collection API |

### Records / Web names

- Record center page is `RecordSearchPage`, not `RecordsPage`.
- Formal save command is `RevisionCommitCommand` / `RecordRevisionCreateRequest`, not `CreateRevisionCommand`.
- Revision metadata fields: `record_type`, `business_status`, `status_group` (derived), `impact_level` (not `impact`), `occurred_at`.
- `useRecordDraft.publish()` does **not** send `evidence_items`. Save-as-record cannot reuse that hook as-is.
- `/records/compare` is unregistered. Static route must sit before `/records/:recordId`.
- `recordsApi.ts` is already used by subject/overview lazy routes; web spec consumer list is stale and must be extended for `/records/compare` without entering AppShell.
- `RecordRevisionPage` is a **single-revision reader** (`RecordWorkspace mode="revision"`), not a revision/evidence list. Pages never call `listRecordRevisions()`. Revision discovery is the subject records timeline.
- `SubjectEvidencePage` lists `evidence_snapshot_id` on activity items and has **no compare basket**. `UnifiedTimeline` links “查看证据” to `/evidence/{evs_…}`, but that SPA route does **not** exist (`*` → `/`). Child 8 entry can read IDs from the timeline item; do not depend on a working evidence-detail page.
- `EvidenceCapturePicker` exists under `web/src/pages/records/evidence/` but is **not imported by any page**. Compare save must not assume the editor already wires capture/preview.
- `Breadcrumb` exists and is tested, but is **not mounted** in `AppShell`. Child 8 should not treat “modify Breadcrumb.tsx” as a mounted surface unless this child also mounts it — prefer `TopBar.derivePageTitle` + subject local nav for the compare title.

### Auth / visibility

- `recordauth.CapabilityComparisonRead` (`comparison.read`) already exists; viewers already have it.
- `recordauth.scopeAllowsActor` already honors restricted `AllowedRoles` / `AllowedGroupIDs`.
- Child 7 activity viewer allowlist is **project-visibility digest only**. Group-granted restricted `record_domain` rows stay hidden on subject activity/evidence pages.
- Comparison must re-authorize every selection through `recordauth` + evidence read. Do not import `internal/center/activity` or `recordsearch`.
- Denied selections stay opaque 404. Do not leak identity or counts.

### Copy lineage / leases / capability flags

- `evidence_copy_lineage(snapshot_id, copied_from_snapshot_id, copy_reason)` exists. Production write path does not; participant only captures or references.
- `RevisionPreparation` is only `Captures` (consume intent → new snapshot) and `References` (reuse existing `evs_`, never copy payload). Save-as-record must add a copy item that allocates a new `evs_` + lineage row + shared payload digest. Do not attach the source snapshot ID to the new record, and do not add a second `evidence_snapshots` writer.
- Domain activity / outbox are written by `CommitRevision` **before** `participants.ApplyRevision`. There is no activity revision participant. Comparison must not invent one. A `"comparison"` participant `Name()` sorts alphabetically between `"collaboration"` and `"evidence"`.
- There is no material-intent hook on `RevisionSaveRequest`. Intent must be a new field on the save request, or validated only inside the comparison participant from a token carried on `RevisionCommitted`. Do not invent a parallel records package.
- HMAC `ComparisonIntentSigner` is net-new. Existing patterns are domain-separated SHA-256 (capture-preview digest, deletion token commitment) and session HMAC. Keep a separate 0400 keyring; do not reuse session / deletion / backup keys.
- Limits already in `evidence/types.go`: 5 MiB canonical, 50k points, 2k buckets, 15m capture-intent TTL.
- Record-platform `object_content_leases` exist. Evidence HTTP read does not currently claim one before payload. Parent design §10 still requires lease-before-content for comparison results.
- Records/evidence routes are gated by `HOUFENG_RECORDS_ENABLED` + handler non-nil. Comparison needs its own default-off capability on top of that, not a second records platform.

### Source status

There is no `live|tombstoned|unavailable|restricted` enum. Reality:

- `SourceState`: `live` \| `tombstoned`
- HTTP: `source_available`
- Retention reason: `snapshot_retained_source_unavailable`
- Restriction is visibility (`project` \| `restricted`), not source state

## 3. Child 7 leftovers — not Child 8

### Overview manage panel (independent follow-up, leave it)

`VPSDetailPage.onManagePanel` is a no-op. `useVPSManagementController` only toggles menu/panel visibility. Writes remain in `LegacyVPSDetail`. Child 7 archive marked this open and non-blocking.

Judgement: **VPS overview finish work**. Do not fold into the comparison workbench. Do not open a parallel PR in this session. Track as a later tiny follow-up after Child 8 or when overview writes are actually needed.

### Restricted group-granted viewer on activity (do not expand)

Activity list will not show group-granted restricted record events to ordinary viewers. Comparison share-URL / exact IDs must still work if `recordauth` allows the snapshot. Subject-evidence entry points will only offer project-visible items to viewers.

Judgement: **analyze and re-auth, do not expand activity digest allowlist in Child 8**.

## 4. Outdated 2026-08-02 clauses

Must rewrite before start:

1. Example kind `monitoring_timeseries/v1` does not exist. Use `monitoring.host/v1` / `monitoring.probe/v2`.
2. “Extend Kind.Compare into an explicit descriptor” overstates the current interface. Current Compare is pairwise exact-aggregate.
3. File list: `RecordsPage.tsx` → `RecordSearchPage.tsx`; subject evidence is already `SubjectEvidencePage.tsx` wrapping `SubjectActivityWorkspace`.
4. `impact` → `impact_level`. Snapshot-only `revision_context=not_applicable` is still valid.
5. Source-status reason codes must map onto `live|tombstoned` + `source_available` + retention reason, not a 4-way source enum.
6. `implement.jsonl` / `check.jsonl` are seed-only. Cursor inline does not need them; do not treat them as curated context.
7. Parent HTTP table does not name `comparison-candidates`. Keep the UX; decide the wire in the revised implement plan.
8. Asset-ledger `comparison_insight` is a different product. Do not reuse those DTOs or UI.
9. `comparison.read` is in the closed capability set and viewers already hold it; no handler checks it. Default-off must be a **route/config gate**, not “add the capability string”.
10. `/evidence/:id` timeline links are dead. Do not plan Child 8 around an existing evidence-detail route.
11. “Modify Breadcrumb.tsx” assumes a mounted crumb trail. It is unmounted; title work is `TopBar` unless this child also mounts Breadcrumb.

## 5. Recommended shrink / split

Keep one Trellis child (parent map stays 8 → 10 → 11). Do not execute the 2026-08-02 task list as written.

### Keep in Child 8

- Capability default off; `/monitoring/compare` untouched
- 2–6 immutable revision/snapshot selections; comparability review before any metric
- `/records/compare` versioned URL state; entries on record center, revision, subject evidence
- Re-auth every open via `recordauth` + evidence read; opaque 404
- Wrap existing pairwise `Kind.Compare` for ip/cost/event/command
- Series / trend / matrix **only** for `monitoring.host/v1` and `monitoring.probe/v2`
- `common_overlap`: return a stable blocked reason unless a kind later opts in (none do today)
- Save-as-record: records create/revision + `evidence_items` + new logical copies + `copied_from_snapshot_id` + registered `comparison.result/v1`
- HMAC comparison intent (unforgeable save). Reuse 0400/no-follow keyring lessons; do not reuse deletion/backup keys
- No root migration

### Shrink

- Do not invent a registry-wide Compare descriptor language in this child
- Do not implement a generic common-overlap reaggregation engine with zero kind consumers
- Do not treat current Compare aggregate DTO as a trend series
- Do not route save-as-record through `useRecordDraft.publish()`
- Do not import activity/search packages for candidate SQL

### Split / defer

- Disposable 4 GiB cgroup peak / 512 MiB mixed-load saturation / `scripts/run-comparison-capacity.sh` outer harness → **Child 11** (already owns mixed profile). Child 8 may keep a process-local weighted admission + unit/integration tests.
- Overview manage-panel writes → **independent follow-up**
- Activity group-granted digest expansion → **not default Child 8**
- Export files / download / CSV → **Child 10**

Optional in-child PR split (same Trellis child, two reviewable PRs):

1. Workbench: candidates + fixed compare + URL + UI + entries + capability off
2. Save: copy lineage write + `comparison.result/v1` + intent + participant

## 6. Decision (2026-08-20)

User chose **A**: one Child 8; reuse pairwise Compare; host/probe series only; save + HMAC stay; 4 GiB harness → Child 11.

Wire lock: keep `POST /api/evidence/comparison-candidates` separate from `POST /api/evidence/comparisons`.

Planning artifacts converged. Implementation still needs an explicit later approval of the final planning summary before `task.py start`.
