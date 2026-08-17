# Child 4 production composition preflight (2026-08-17)

## Authority and current baseline

- This continuation starts from released `origin/main` / `v0.65.0` commit
  `94bebab0c9bca1325e16bf9912e955d92f4bf375` on
  `codex/vps-records-evidence-platform-production`; repository hooks are enabled.
- Tasks 1-7 and their independent checks are already merged through PR #406.
  Do not rewrite their domain contracts, migrations, adapters, Web registry, or
  maintenance/capacity behavior.
- Continue Child 4 only. Do not start the Collaboration child or Markdown child,
  and do not implement portability quarantine/generic rendering.

## Verified missing production seams

- `cmd/houfeng-center/bootstrap.go:newRecordsHTTPHandlers` still passes a nil
  `store.AdmissionGate`, constructs no evidence registry/application/preparer,
  returns `handlers.Evidence(nil)`, and registers no evidence maintenance worker.
- The six registered kinds already have production source implementations:
  `PostgresRuntimeFactsRepository` (host/probe), `PostgresIPQualityRepository`,
  `PostgresIncidentRepository`, `PostgresSubscriptionCostRepository`, and
  `PostgresCommandAuditRepository`. `PostgresRenewalDecisionRepository` is the
  authoritative asset-history source but is deliberately not an evidence kind.
- `PostgresEvidenceRepository` implements intent/payload/capacity/lifecycle and
  Task 5 deletion/recovery, but not `SnapshotReadSource`,
  `AuthorizedSnapshotSource`, or `ExistingSnapshotReferenceSource`.
- The live source adapters can be reused through the existing closed Records
  `SubjectAdapterRegistry`; the new evidence resolver must accept only exact
  VPS/monitoring-instance/target kind+ID pairs and return the canonical live
  identity and `SourceAuthorization`. It must not invent a generic source or a
  tombstone from the local digest-only projection.
- Current record/revision authority already exists in
  `PostgresCurrentRecordAuthorizationSource` and must be reused. Snapshot read,
  export, and existing-reference authorization must intersect the owner record
  authority with the current live source or an injected witnessed tombstone
  authority; capture-time scope alone is never sufficient.

## Admission boundary that must remain closed

- `store.AdmissionGate` is a same-transaction deployment-membership gate. Its
  contract binds an immutable membership identity and checks it before record
  primitives. The foundation design explicitly assigns the concrete
  `deployment_membership` writer/heartbeat/readiness and admission query to a
  later platform gate; the repository currently has no production identity,
  writer, heartbeat, or readiness implementation to compose here.
- `migrate.AdmitAppACLCurrentRuntime` is only a startup APP ACL/catalog check. It
  opens its own read-only transaction and is not a substitute for the
  same-transaction membership gate.
- Therefore this Child 4 slice may expose one explicit composition function that
  receives a real `store.AdmissionGate`, and may use an explicit test gate in
  unit/strict-PostgreSQL tests. Default production bootstrap must continue to
  pass nil/typed-nil and expose stable fail-closed handlers with no evidence
  worker, persistence, or allow-all fallback until the platform gate supplies
  the dependency. Do not fabricate an identity, query only APP ACL state, or
  use `store.AdmissionGateFunc` in production code.

## Required RED -> GREEN matrix

1. Production composition rejects nil/typed-nil gate and nil required source
   dependency without constructing a service, preparer, handler, or worker.
2. With an injected non-nil gate, construct exactly the six authoritative kinds
   and fail closed on duplicate/unknown kind or missing source; asset history is
   not registered as a seventh kind.
3. Add a closed evidence-source resolver matrix for VPS, monitoring instance,
   and target identities, wrong kind/ID/project, missing source, and dependency
   failure. No generic or digest-only tombstone fallback.
4. Add PostgreSQL evidence read/reference/export source methods through admitted
   transactions. Validate exact snapshot/payload gzip/hash/size/envelope,
   current record/revision authority, current source authority, source
   availability, and opaque denial/not-found behavior before returning domain
   state. Existing-reference paths return metadata only and never recapture or
   copy payload bytes.
5. Wire the injected evidence service, `RevisionPreparer`, Records save hook,
   Evidence handler, and maintenance worker together. Default bootstrap remains
   fail closed until the real membership gate is injected.
6. Strict PostgreSQL tests cover preview -> intent -> recapture -> revision ->
   read and existing-reference reuse, preview/source drift, rollback/double
   consume, intent expiry, orphan grace/reclaim, unknown kind/schema, permission
   intersection, and nil-gate zero-write behavior. No `SKIP` is acceptance.

## Verification and stop boundary

- Minimum gates: focused RED/GREEN, affected packages, affected race, strict
  Docker PostgreSQL, `make verify-go`, `go test ./... -count=1`, `go vet ./...`,
  `go mod verify`, changed-Go `gofmt -d`, `git diff --check`, and Trellis
  validation. Web code is unchanged; run the established Node 22 full Web gate
  only if a shared Web/DTO file changes.
- Stop after independent `trellis-check` and Child 4 delivery evidence. Do not
  enter Child 9 Collaboration or Child 5 Markdown.
