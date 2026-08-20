# Child 7 current-main rebaseline (2026-08-19)

Read this before `task.py start`. Child 7's `prd.md` / `implement.md` / `design.md`
were written before Children 5, 6, and 9 landed. Their intent still holds; several
concrete names, formats, and starting points in them do not. Where this note and
those docs disagree about **what exists today**, this note is current.

## Baseline

- `main` at `5d086d97`, released `v0.69.0`. Program progress `7/11`.
- Direct dependencies are all archived on protected main: Child 2 (`0052`),
  Child 4 (`0054`), Child 9 (`0055`), Child 6 (`0056`).
- Highest migration is `0056_create_record_search.sql`. **`0057` is free** and
  still matches the parent's `RBL-AC-03` numbering contract.
- `./scripts/verify.sh` green on Node 22; bundle and CSS budgets pass unchanged
  (entry JS gzip 109504, max async 48453, zero CSS growth).

## Already on this baseline — extend, do not rebuild

- `public.record_domain_activities` (`db/migrations/0052_create_records_core.sql`
  lines 342–361) is the append-only source seam, with an immutability trigger.
- Comment and action events really do land in it today, so the "comments and
  action items enter the full timeline" deliverable has a real source rather
  than a stub: `store/record_comments.go:518` (`insertRecordCommentActivity`),
  `store/record_actions.go:530` (`insertRecordActionActivity`), and
  `store/record_collaboration_participant.go:368` for owner / participant /
  follow-up changes.
- Project-level `/records`, `/records/drafts`, `/records/new`, the record detail
  and revision routes, and the sidebar entry are Child 6's and stay Child 6's
  (`web/src/app/router.tsx:115-120`, `Sidebar.tsx:39`). Child 7 only adds
  VPS-local surfaces.
- Evidence transport and renderers exist (`GET /api/evidence/capture-previews`
  and `/api/evidence/{evidence_id}` at `internal/center/http/router.go:197-198`,
  plus `web/src/pages/records/evidence/`). `EvidenceCapturePicker` is still
  route-private with no production consumer.

## Corrections to the plan text

| Plan says | Actually on main |
|---|---|
| `recordcursor` package, AES-GCM token, namespace `subject-activity/v1` | No `recordcursor` package. Child 6's codec is `internal/center/recordsearch/cursor.go`, a base64url-wrapped JSON envelope |
| Reuse Child 6's cursor directly | The envelope hard-binds a `recordsearch.Query` digest and the search index generation, and its sort key is `(record_updated_at, record_id)`. Reuse the **pattern**, not the type |
| `activity_id` uses an `act_` prefix | The source table uses `rac_` (`0052` line 343) |
| Activity `records` view filters `record_revision`, `record_state_changed`, `record_visibility_changed` | None of those kinds are written. Real kinds are `record_created`, `record_revised`, `record_restored`, `record_archived`, `record_unarchived` (`records/revisions.go:32-36`), `comment_created|edited|redacted`, `action_created|updated|completed|cancelled|reopened`, and `record_owner_changed` / `record_participant_changed` / `record_follow_up_changed` |
| Move the current page to `vps-detail/LegacyVPSDetail.tsx` | That file does not exist. The page is still one 1632-line `web/src/pages/VPSDetailPage.tsx` |
| Recent activity no longer reads the legacy timeline | `VPSDetailPage.tsx` still calls `getVPSTimeline` in three places |
| Gate recomposition on a `records_v2_read` capability | No such flag exists anywhere outside these task docs. The only capability mechanism is per-record `RecordCapabilities` (`records/read_service.go:99`) |
| `internal/center/activity/`, `internal/center/vpsoverview/` | Neither package exists yet |

`GET /api/subjects/:type/:id/activity` and `GET /api/vps/:id/overview` do not
exist. The VPS subtree currently dispatches by enum in
`internal/center/http/router.go:587-606`, where `timeline` is the closest
existing sibling, so an overview endpoint joins that switch rather than becoming
a new top-level route.

## What this changes about the approach

1. **Build an activity-scoped query + cursor, don't wrap Child 6's.** The reusable
   part is the shape: normalize the query, digest it, bind actor scope and a
   generation into an opaque token, and refuse every mismatch with one
   undifferentiated error. Child 7's sort tuple and generation semantics differ,
   so a thin adapter over `recordsearch` would fight the type rather than reuse
   it. Align the constants (`CursorVersionV1`, page-size bounds in
   `recordsearch/types.go:18-28`) so the two surfaces behave the same to callers.
2. **The event-kind vocabulary must come from the writers, not the design doc.**
   The design's filter predicates name kinds nothing emits. Deriving the adapter
   vocabulary from the constants above is the difference between a timeline that
   shows revisions and one that silently shows nothing.
3. **`records_v2_read` is greenfield.** Both the server side (surfacing it, most
   naturally on the overview response's capabilities) and the client gate have to
   be designed, not wired. Decide its exact contract before Task 6, because the
   VPS recomposition sequencing depends on it.
4. **The VPS detail recomposition is a rewrite, not a refactor.** 1632 lines plus
   a client-side overview model (`vps-detail/vpsDetailOverviewModel.ts`) and 24
   supporting files, currently fed by legacy asset and timeline APIs. Extracting
   the legacy page behind the flag is itself a bounded task worth doing before
   any new sections land.
5. **`0057` has to register its ACL fragment or migration admission fails.**
   Follow `recordSearchAppACLCurrentMigrationFragment()`
   (`store/migrate/app_acl_current_contract.go:313-347`) and append to the
   fragment slice at lines 28-34; `compileAppACLCurrentSourceContract` rejects
   any migration without a fragment. Mirror `0056`'s conventions: per-column
   checks, `project_id` pinned to `'default'`, `on delete restrict` to
   authoritative tables, security-definer internal functions behind `public`
   `bytea` wrappers, and no cascade from authoritative sources.

## Still accurate

- The dependency set and its rationale (evidence + search → timeline/overview).
- Child 6 keeps project-level `/records`; Child 7 is VPS-local only.
- The execution gate: status stays `planning` until the user explicitly
  authorizes `task.py start`.
- Toolchain: Go 1.26.2, TypeScript ~6.0.2, Node 22 for web gates.

## Open questions for the user before start

1. Should the legacy VPS detail extraction land as its own PR ahead of the new
   overview sections, so the rewrite is reviewable in pieces?
2. `records_v2_read`: per-project setting, per-actor capability, or build-time
   flag? The plan assumes it exists and does not specify.
3. Does the subject timeline need the monitoring-instance and Target analogues in
   the same child, or is VPS enough to accept it and the others follow?
