# Task 2B capture transaction contract

## Reopened Task 2A defect

- `internal/center/evidence/conformance.go` owns the established intent identity contract: `evi_` followed by exactly 24 lowercase hexadecimal characters.
- `db/migrations/0054_create_record_evidence.sql` and `internal/center/store/migrate/record_evidence_migration_test.go` currently accept `eci_[a-z0-9]{1,64}`.
- Add a cross-layer RED before changing production SQL. Preserve the observed RED output, then align SQL and its migration assertion to the existing Go contract and rerun migration, ACL, formatting, vet and real-PostgreSQL gates.

## Approved Task 2B flow

1. Preview persists the normalized selection, full preview, preview/source digests and a 15-minute expiry.
2. Before opening the revision transaction, reauthorize and recapture new evidence. Compare every preview-bound field exactly, write the content-addressed payload, and construct an immutable server-owned prepared capture.
3. Reauthorize existing logical snapshots outside the transaction and construct prepared references. Reuse them without recapture or payload duplication.
4. Pass prepared captures/references explicitly with the revision commit. Do not use context values or a process singleton/map.
5. Include the final ordered snapshot IDs in revision canonical content/hash and the idempotency fingerprint.
6. Inside the existing revision participant transaction, atomically consume each live intent with `DELETE ... RETURNING`, insert logical snapshots, and insert ordered revision references. A rollback restores the intent and removes all logical rows.
7. A payload written before a failed revision may remain orphaned. Task 2B owns expiry/orphan-GC repository primitives and real-PostgreSQL behavior tests with a 24-hour grace period. Task 7 owns scheduling, metrics, capacity and alerts.
8. Production capture/save stays fail-closed until Child 10 supplies the real `AdmissionGate`. Do not add an allow-all fallback.

## Source map

- Evidence contracts and 15-minute TTL: `internal/center/evidence/types.go`.
- Existing intent ID validator and preview/intent conformance: `internal/center/evidence/conformance.go`.
- Current schema and immutable tables: `db/migrations/0054_create_record_evidence.sql`.
- Revision service preparation/fingerprint path: `internal/center/records/service.go`.
- Transaction-local participant contract: `internal/center/records/revisions.go`.
- Revision authority transaction and participant call site: `internal/center/store/records.go`.
- Closest participant implementation: `internal/center/store/record_attachment_participant.go`.
- Admission gate and typed-nil fail-closed behavior: `internal/center/store/record_platform.go`.
- Migration assertions: `internal/center/store/migrate/record_evidence_migration_test.go` and `record_evidence_app_acl_test.go`.

## Required behavior tests

- Cross-layer intent identity mismatch RED, then exact `evi_<24 lowercase hex>` GREEN.
- Preview drift for every bound envelope field fails before the revision transaction.
- Expired, missing, replayed or double-consumed intent fails closed.
- Participant failure rolls back revision, snapshot, reference and intent consumption.
- Existing snapshot reference is reused without adapter recapture or payload duplication.
- Ordered snapshot IDs affect revision canonical hash/fingerprint deterministically.
- Orphan payload is retained before 24 hours and reclaimable only after the grace period when globally unreferenced.
- Nil/typed-nil/unavailable production admission performs no primitive write.

## Rejected alternatives

- No durable staging table: it would introduce a second recoverable state machine.
- No source capture inside the revision transaction: participants cannot perform network or external work.
- No context-carried or singleton prepared state: commit input must be explicit, immutable and testable.
- No permissive production gate while the real admission dependency is unavailable.
