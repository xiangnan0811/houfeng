# Records Integration Verification, Backup/Restore, and Final Acceptance

## Goal

Integrate Children 1-10 into one working Records experience, implement the
minimum supported backup/restore orchestration needed by that feature set, and
prove functional, security, recovery, performance, accessibility, and deletion
behavior on the current development build.

## 2026-08-02 Development Rebaseline

This child no longer owns deployment cutover, staging, release images, Release
Please, soak, or mixed-version compatibility. It adds no root migration and does
not require legacy data conversion. The supported restore target is a fresh
environment running the exact compatible current build/schema contract.

## Dependencies and ownership

All Children 1-10 must be merged and accepted on protected main.

Each domain child owns its backup/restore/deletion adapter and focused tests.
This child owns:

- the aggregate adapter registry completeness gate;
- backup and restore orchestration/CLI needed to exercise those adapters;
- real local and S3-compatible integration profiles;
- restore followed by deletion replay and projection rebuild;
- end-to-end product, security, performance, browser, and recovery acceptance;
- the final decision to enable or keep permanent delete disabled.
- aggregate construction and live verification of Child 10's real
  deployment-membership gate, witnessed source-deletion authority, and
  quarantine contract before any protected Records capability is enabled.

It does not silently implement a missing domain adapter. A missing or unhealthy
adapter blocks acceptance and returns work to the owning child.

## Requirements

### Supported backup and restore

- Back up the current root PostgreSQL database and every child-declared external
  object/version needed for Records, plus a typed manifest that binds build,
  migration set, adapter versions, database artifact, objects, hashes, sizes,
  classifications, and creation time.
- Use official command entry points suitable for local development and
  integration automation. Plan/dry-run modes perform no writes.
- Restore only into an isolated fresh target. Reject non-empty target state,
  incompatible migration/adapter contract, missing/tampered bytes, or unknown
  required manifest fields.
- Restore database and immutable objects, run every registered restore adapter,
  replay deletion outcomes, rebuild search/activity projections, verify current
  APP ACL/runtime admission, and only then report ready.
- Temporary backup/restore workspaces, partial objects, multipart uploads, pins,
  and leases have bounded cleanup and failure receipts.
- Local and S3-compatible storage profiles share the same manifest and adapter
  conformance rules.

### Deletion and no resurrection

- Permanent delete stays disabled until the aggregate registry proves every
  content-owning domain, artifact store, projection, delivery surface, backup,
  and restore replay adapter is present and healthy.
- Backup -> delete -> restore must not resurrect a record, revision, attachment,
  evidence, collaboration content, search/activity entry, portability artifact,
  or legacy source content.
- Restore replays deletion facts before serving traffic and rebuilds derived
  projections only from surviving authoritative rows.
- Failure or unknown outcome in purge/replay remains fail closed and observable.

### Real integration

- Provide reproducible integration profiles for PostgreSQL 16, local Blob, and
  S3-compatible Blob/ArtifactStore with the content processor/scanner paths used
  by the implemented children.
- Exercise create/edit/revise/draft, attachment scan/download, evidence capture,
  collaboration, search, activity/overview, comparison/save-as-record,
  export/import, archive/restore, permission revoke, source deletion, and
  permanent-delete preview/execute when enabled.
- Inject failures at database, object, worker, processor, stream, backup, restore,
  and projection boundaries; assert cleanup and idempotent retry.
- Integration fixtures and artifacts contain no real credentials, production
  data, or unrestricted user content.

### Security and observability

- Re-run authorization/IDOR, Markdown/XSS, MIME/archive, SSRF/network isolation,
  response allowlist, download lease, secret/content log, and permission-revoke
  contracts across the assembled system.
- Logs/metrics use bounded identifiers and reason codes; no Markdown, comment,
  evidence payload, attachment bytes, archive content, credential, or raw
  database URL is emitted.
- Verify all feature workers and routes stop or fail closed when their required
  adapter/store/source is unavailable.
- Verify nil/typed-nil, stale, wrong-deployment, discontinuous, and unavailable
  membership/tombstone authority keeps writes, source-deleted reads, quarantine
  apply, and permanent delete closed without an allow-all fallback.

### Performance and UI

- Run deterministic representative data for overview, Records list/search,
  timeline, draft save, revision save, comparison, evidence, and export/import.
- Treat the parent design latency/capacity numbers as target budgets; a material
  regression or resource exhaustion blocks acceptance. Hardware-specific
  benchmark publication is not required.
- Verify the six approved surfaces across applicable loading, empty/no-results,
  local failure, submitting/background, revoked/deleted, stable, and anomaly
  states on desktop and 390px.
- Require keyboard operation, focus restoration, 44px touch targets where
  applicable, no document-level mobile overflow, and no Axe critical/serious
  violations.
- Automated semantic/geometry/accessibility tests are the gate. A formal
  20-participant comprehension study is not required in the no-user development
  phase.

## Acceptance Criteria

- [ ] No `0060` or cutover schema is introduced; fresh current migrations end at
  the last child-owned root migration (`0058` in the planned sequence).
- [ ] The adapter registry has an exact expected set and fails on missing,
  duplicate, unknown, version-incompatible, or unhealthy entries.
- [ ] The real deployment-membership gate and source-deletion witness are
  composed on the exact build/deployment identity; all negative authority cases
  fail before serving or business writes, and no test gate is reachable in
  production wiring.
- [ ] Backup plan/create/verify and restore plan/apply/verify work for local and
  S3-compatible profiles with deterministic typed manifests and exact hashes.
- [ ] Every destructive/failure cutpoint leaves either a valid published backup/
  restored environment or a bounded cleanup state with no false readiness.
- [ ] Backup -> delete -> restore yields zero resurrected protected content and
  rebuilds search/activity only for surviving authoritative data.
- [ ] APP migration/ACL and runtime admission pass on restored databases before
  application readiness.
- [ ] Full Records happy paths and specified loading/empty/error/revoke/source-
  deleted workflows pass across backend and Web.
- [ ] Security corpus, response/log allowlists, permission-revoke streaming, and
  hostile attachment/archive cases pass in the assembled system.
- [ ] Representative performance/capacity tests meet reviewed budgets without
  OOM, unbounded queue/workspace growth, or silent partial results.
- [ ] Desktop/390px semantic, geometry, keyboard, focus, touch, reduced-motion,
  and Axe gates pass for all six surfaces.
- [ ] Permanent delete is enabled only when all deletion/recovery gates pass; if
  any gate cannot be proven, the final accepted build keeps it disabled and
  reports the exact missing capability.
- [ ] Full Go, Web, browser, local/S3 integration, recovery, `git diff --check`,
  and `trellis-check` pass on one commit.
- [ ] Child 11 is merged/archived and the parent final cross-child audit is
  complete; staging/release activity is not required.

## Out of Scope

- Staging deployment, production rollout, release-image publication, Release
  Please, soak, branch protection administration, or post-release monitoring.
- In-place upgrade, mixed-version restore, rolling deployment, or old database
  compatibility.
- `experience_logs` migration/backfill/dual-write.
- Multi-region/WORM disaster recovery, external key governance, or enterprise
  backup retention policy.
- Formal human research recruitment in the current no-user phase.

## Execution Gate

Keep `planning` until Children 1-10 are accepted on protected main. Before start,
reconcile this plan with their actual adapter registries and test entry points.
A missing child contract returns to that child; it does not expand Child 11.
