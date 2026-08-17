# Records Integration Verification, Backup/Restore, and Final Acceptance Design

## 1. Boundary

Child 11 is the integration owner, not the owner of every domain implementation.
It assembles published contracts from Children 1-10, provides the minimum
backup/restore runtime required for end-to-end verification, and owns final
cross-domain acceptance.

It adds no root migration and does not deploy a release.

Child 10 supplies the concrete deployment-membership gate, witnessed
source-deletion authority, and unsupported-evidence quarantine. Child 11 owns
their aggregate bootstrap/readiness composition and proves that no test gate,
typed-nil bypass, local tombstone substitute, or partial registry can enable a
protected capability.

## 2. Integration topology

The reproducible test topology contains:

- `houfeng-center` with direct current-runtime database credentials;
- PostgreSQL 16 root database plus foundation deletion/recovery stores actually
  selected by the implementation;
- local filesystem or S3-compatible Blob/ArtifactStore profile;
- content processor and configured scanner path;
- background projectors/workers;
- browser test server and deterministic fixtures.

Each profile creates isolated names/ports/directories, records its exact
configuration digest, and cleans up unless diagnostic retention is explicitly
requested after a failure. Secrets are generated per run and never stored in
tracked artifacts.

## 3. Aggregate adapter registry

Each domain publishes typed capabilities:

```go
type RecoveryAdapter interface {
	Kind() string
	Version() uint32
	Backup(context.Context, BackupWriter) error
	Restore(context.Context, RestoreReader) error
	ReplayDeletions(context.Context, DeletionWatermark) error
	Verify(context.Context) error
}
```

The aggregate registry has a compile/test-owned expected kind set. It rejects
missing, duplicate, unknown, or incompatible adapters. Child 11 can supply
shared orchestration adapters, but domain-specific missing behavior returns to
the domain owner.

Permanent-delete readiness consumes this exact registry plus live health. It is
not a feature flag that bypasses incomplete adapters.

Records write/read readiness also consumes the real deployment-membership and
source-deletion authority health. Their deployment identity, contract epoch,
and witness continuity must match the current build manifest; any mismatch or
unavailable dependency keeps routes/workers closed before traffic.

## 4. Backup manifest

Use a typed current-build manifest containing:

- format and minimum reader version;
- build commit/version and canonical embedded migration digest;
- APP ACL manifest digest and adapter kind/version set;
- database artifact version/hash/size;
- exact external object kind/key version/hash/size/classification;
- deletion watermark/reference needed for replay;
- created time, profile, and completion digest.

The manifest contains no credentials, database URL, record title/Markdown,
comment, evidence summary, or attachment filename. Content is represented only
by its encrypted/private artifact and integrity metadata.

Backup is staged, verified, and atomically published. A manifest is never
published before every required adapter and object reports durable success.

## 5. Restore state machine

```text
validate request/empty target
  -> verify manifest/build/migration/adapters
  -> stage and verify all bytes
  -> restore database in isolation
  -> restore external objects/adapters
  -> replay deletion outcomes
  -> rebuild derived projections
  -> converge/verify current APP ACL
  -> run adapter/domain verification
  -> publish readiness
```

Every state is idempotent or has a durable compensation/cleanup rule. Traffic
and workers remain off until final readiness. Unknown manifest versions,
incompatible current build, partial bytes, adapter failure, deletion replay
failure, or catalog admission failure leaves the target isolated.

The supported compatibility contract is exact current format/build semantics,
not arbitrary released database upgrade.

## 6. Deletion replay

The backup manifest captures a consistent deletion reference. Restore obtains
the authoritative current deletion view, applies it before traffic, calls every
adapter's replay method, and verifies protected content is absent from:

- domain roots and revisions;
- attachments/evidence/collaboration;
- portability artifacts and import origins;
- search/activity projections;
- processor/export/download workspaces;
- supported backup/object inventory.

Derived projections rebuild after replay, never before. A failed or ambiguous
replay cannot be skipped.

## 7. Integration harness

`scripts/run-records-integration.sh` owns profile setup, health, seeded workflow,
failure injection, evidence verification, and cleanup. `scripts/run-records-
recovery.sh` owns backup/restore/delete/replay scenarios. Both emit typed,
content-safe summaries tied to commit/config/manifest digests.

Integration tests use real HTTP/domain/store/worker paths. Stubs are allowed only
for external services whose adapter conformance is separately run against the
real selected implementation.

## 8. Security verification

A fixed hostile corpus flows through Markdown, comments, evidence, attachments,
archives, imports, logs, errors, downloads, and browser rendering. Tests assert:

- authorization and source-scope intersection;
- no active content or unsafe URL/network execution;
- no payload/credential/raw URL in logs or test artifacts;
- response allowlists and safe error codes;
- permission revoke/deletion fence before headers and during streaming;
- workspace/profile/partial cleanup after crashes.

## 9. Performance and capacity

Use deterministic representative data and fixed operations rather than a
production-scale claim. Record environment, seed digest, request counts,
p50/p95/p99, error categories, database connections/slow queries, queue age,
workspace/object usage, and memory peak.

Budgets are reviewed before start against implemented behavior and the parent
targets. Tests fail on OOM, unbounded growth, unexpected error, silent
truncation, or material regression. Dedicated release benchmark hardware is not
required.

## 10. Browser acceptance

Cover:

- VPS overview;
- Records center;
- subject timeline;
- Markdown editor/material workspace;
- evidence selector;
- comparison workbench.

Use semantic DOM, state, geometry, overflow, focus, keyboard, touch, reduced-
motion, and Axe assertions. Screenshots may support local review but are not
tracked pixel goldens. Automated browser fixtures never ship in production
bundles.

## 11. Completion decision

Child 11 produces a final capability matrix. Permanent delete is `enabled` only
when its adapter/recovery rows are all green; otherwise the feature remains
`disabled` with a precise reason while the rest of Records may still pass.

Child completion requires one commit with all relevant local gates green and a
protected-main merge. The parent then performs its cross-child audit. No
staging/release receipt is created.

## 12. Rollback

Integration-only scripts/config can be reverted without schema change. Backup/
restore targets are isolated and disposable. Feature flags may disable a
failing assembled surface, but acceptance cannot hide a required Records child.
Development databases may be rebuilt when returning to an older code version.
