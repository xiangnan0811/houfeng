# Records Import, Export, and Portability

## Goal

Provide authorized human-readable export, canonical machine archives, safe
import, and portable deletion/recovery behavior for Records data without
migrating legacy `experience_logs`.

## 2026-08-02 Development Rebaseline

This child no longer owns legacy conversion. It adds one root migration,
`0058_create_record_portability.sql`, after the content-owning Records children
have stabilized. It supports fresh/current development databases only.

## Dependencies

Children 2-9 must be merged and accepted before this child starts:

- Core owns records, revisions, drafts, identity, and revision transactions.
- Attachments owns logical and physical attachment export/import adapters.
- Evidence owns schema-aware evidence export/import adapters.
- Markdown owns the safe human render model.
- Collaboration owns actions/comments/watch export policy.
- Search and Activity own projections; they are rebuilt, not imported as
  authoritative mutable state.
- Comparison owns `comparison.result/v1` portability.
- Platform foundation supplies auth, idempotency, outbox, deletion fence, and
  current migration/ACL admission.

The 2026-08-17 rebaseline also assigns this child the real
deployment-membership `store.AdmissionGate`, witnessed source-deletion
tombstone authority, and integrity-valid external unsupported-evidence
quarantine required by the fail-closed Records composition. Child 11 owns their
aggregate composition/readiness and final enablement evidence.

## Requirements

- Create `0058_create_record_portability.sql` for export/import jobs, artifacts,
  import plans, identity/origin mappings, and minimal purge receipts.
- Register `0058` objects and privileges in the current APP ACL fragment.
- Human export supports Markdown and PDF through the same authorization-safe
  render model. PDF is a derived presentation, not the machine source of truth.
- Machine export uses a versioned canonical archive with typed manifest,
  deterministic ordering, per-file sizes/hashes/classification, bounded parsing,
  and optional signature metadata.
- Export preview fixes record set, revision range, material inclusion,
  authorization/fence state, expected files/bytes, and expiry. Publish rechecks
  all mutable gates.
- Import registers bytes in quarantine, validates archive/path/hash/size/schema,
  produces a reviewable expiring dry-run plan, remaps IDs/references, and applies
  through existing domain participants.
- Import never trusts archive-declared authorization, actor role, classification,
  renderer, SQL, file path, or external URL.
- Apply is idempotent and atomic for one plan. A partial failure publishes no
  record, attachment, evidence, collaboration, search, or activity state.
- Imported author/source identity is provenance, not local authority. The local
  operator is recorded separately.
- Search and activity projections rebuild from imported authoritative domain
  rows and typed origin facts; checkpoints/cursors/leases are never imported.
- Export/import artifacts, workspaces, leases, and external-copy disclosures
  participate in permanent-delete preview, purge, backup inventory, and restore.
- An origin tombstone prevents an officially restored or re-imported archive
  from resurrecting a permanently deleted target.
- The production deployment-membership gate reuses the existing `0051`
  `deployment_membership` plus `deployment_contract_state` authority; `0058`
  creates no duplicate gate table. That gate and the source-deletion witness bind
  the external deletion-ledger/contract-activation identity and fail closed on
  nil, typed-nil, stale, incomplete, or unreachable authority. No APP-local
  allow-all or digest-only tombstone can satisfy the contract.
- External unsupported evidence may expose only locally derived allowlisted
  envelope metadata after archive/entry integrity succeeds; it cannot create a
  record/snapshot, render/compare/apply, or be re-exported as trusted evidence.
- Local and S3-compatible ArtifactStore implementations pass one conformance
  suite.
- No `0059` migration, no `experience_logs` reader, no text heuristic conversion,
  and no old/new dual-write.

## Acceptance Criteria

- [ ] `0058` fresh/repeat migration and current APP ACL/admission tests pass.
- [ ] Canonical archive output is byte-deterministic for the same fixed input;
  manifest/path/hash/size/schema limits and hostile ZIP cases fail closed.
- [ ] Human Markdown/PDF and machine export include only authorized allowlisted
  data, preserve required semantics, and clearly identify unavailable material.
- [ ] Export preview/publish rejects permission, revision, inventory, fence, or
  source-readiness drift without publishing a partial artifact.
- [ ] Import dry-run reports exact creates/remaps/warnings/blockers/bytes and
  performs no authoritative domain writes.
- [ ] Apply remaps record/revision/subject/evidence/attachment/collaboration and
  comparison references atomically, idempotently, and without actor escalation.
- [ ] Unsupported required schemas block apply; optional opaque material remains
  quarantined and cannot render, compare, or re-export as trusted evidence.
- [ ] Real deployment-membership admission and witnessed source-deletion
  tombstones satisfy transaction/readiness/replay contracts; nil, stale,
  incomplete, or unavailable authority keeps writes and affected reads closed.
- [ ] Source deletion, target archive, permission revoke, cancellation, expiry,
  crash, and janitor paths leave no unauthorized download or orphan workspace.
- [ ] Permanent delete purges owned content/artifacts and keeps only the minimal
  origin/tombstone facts needed to prevent official re-import resurrection.
- [ ] Backup/restore adapters preserve published authorized artifacts and active
  import state only as declared; restore followed by deletion replay cannot
  resurrect deleted content.
- [ ] Local and MinIO-backed integration, hostile corpus, race tests, Web
  import/export workflows, full Go/Web gates, and `trellis-check` pass.
- [ ] Existing asset JSON import behavior remains independently tested but is not
  folded into the Records archive contract unless explicitly adapted.

## Out of Scope

- Migrating, backfilling, or deleting `experience_logs`.
- General cross-version database upgrade.
- Anonymous/public links or permanent bearer downloads.
- Executing imported code, SQL, macros, remote URLs, or active content.
- A general backup product; Child 11 owns end-to-end backup/restore validation.
- Aggregate production readiness/enablement; Child 11 owns final composition and
  keeps protected capabilities disabled if any authority or adapter is missing.
- Release, staging, or cutover orchestration.

## Execution Gate

Keep `planning` until Children 2-9 are on protected main and this plan is
reconciled against their actual adapter APIs. Start in a fresh branch/worktree
from that main; do not reserve `0058` through an unmerged placeholder migration.
