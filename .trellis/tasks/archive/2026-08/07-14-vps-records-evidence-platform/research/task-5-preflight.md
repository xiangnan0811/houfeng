# Task 5 preflight (2026-08-16)

## Baseline and scope

- The delegated checkout was clean at `425a758df86c4d138ba80d376e66d10274ff28ae`; that commit is also `codex/vps-records-evidence-platform-phase2`, `origin/main`, and their merge base.
- Task 5 runs on `codex/vps-records-evidence-platform-task5`; repository hooks are enabled with `sh scripts/setup-git-hooks.sh`.
- Task 1–4 evidence is authoritative in `implement.md`, including the two non-blocking Task 4 observations. Task 5 must reuse those contracts and must not redesign adapters or extend Task 6/7.
- Baseline focused tests pass for `evidence`, HTTP handlers/router, Records, bootstrap, and the store evidence/deletion slice.

## Existing implementation seams to reuse

- `evidence.Registry` is the only authority for supported kind/schema pairs. `LookupKey` and the registered wrapper already fail closed and delegate `Export` only to the concrete kind.
- `evidence.RevisionPreparer` already implements the approved pre-transaction recapture/reference flow. It consumes ordered tagged `CaptureIntentID` / `ExistingSnapshotID` items, reauthorizes sources, detects persisted-preview drift, persists canonical payloads, and returns immutable `RevisionPreparation`.
- `records.Application` and `RevisionService` already accept `evidence.RevisionPreparation`; snapshot order already enters canonical revision content and the idempotency fingerprint.
- `store.RecordEvidenceRevisionParticipant` already consumes capture intents and writes logical snapshots/revision references inside the caller's transaction. Bootstrap currently omits this participant.
- `store.PostgresEvidenceRepository` already implements intent binding, intent persistence, payload persistence, expiry, and orphan-payload GC, all behind `recordplatform.AdmissionGate`. It must be extended through narrow interfaces rather than bypassed.
- The six Task 3/4 source adapters exist, but bootstrap does not yet build their registry, production resolver, preview/read application, or `RevisionPreparer` dependencies.
- Router and bootstrap currently expose Records, drafts, deletion, and attachment handlers only. Bootstrap intentionally passes a nil production gate and returns stable fail-closed handlers; Task 5 must preserve that behavior until Child 10 provides the real gate.

## HTTP and Records save contract

- Add one Evidence handler with exactly `POST /api/evidence/capture-previews` and `GET /api/evidence/:id`. Use strict request decoding and explicit response DTOs; never serialize canonical payload bytes, arbitrary metadata, or a generic JSON rendering fallback.
- The preview path validates actor scope and kind/schema through the registry, validates/normalizes the selection, calls the kind preview, allocates server-owned `evi_` and `evs_` identities, and persists the complete preview binding. Source unavailable/unstable and unsupported kind/schema are stable fail-closed outcomes.
- A capture intent is record-bound. For a not-yet-published record the server must allocate the `rec_` identity before persisting the preview and return it in the preview envelope. Records create may accept that identity only when ordered evidence items are present and `RevisionPreparer` proves those intents are bound to it; the legacy no-evidence create path continues to allocate the record ID itself. Existing-record saves use the path record ID.
- Extend create/revise/restore inputs with a strict ordered evidence tagged union. Every save calls `RevisionPreparer` before entering the Records transaction. Empty evidence is represented by a valid empty preparation, never by skipping the evidence participant contract. Restore reconstructs the target revision's evidence IDs as existing references and reauthorizes them before commit.
- Map expired/drifted preview state to the stable preview-stale response; do not weaken `RevisionPreparer` comparison or allow clients to submit payloads, digests, authorization scopes, snapshot IDs for new captures, or reordered prepared output.
- Evidence read loads the logical snapshot, its canonical payload and owner record, resolves the exact registered kind/schema, reconstructs and validates the snapshot, then enforces the intersection of current record/revision permission and current-or-captured-source authorization before calling `kind.Summarize`. Denial and nonexistence remain opaque. The response is an allowlisted envelope plus the versioned kind summary only.

## Deletion, export, and recovery

- Extend the closed record-deletion surface list for `record_evidence`. Its adapter delegates health/preview/purge/verify to a narrow store. Purge removes the deleted record's revision references, capture intents, and record-owned logical snapshots. It may delete payloads only after a global reference check; logical copies owned by other records and their lineage survive.
- `ExportAdapter` loads and validates an authorized canonical snapshot through a narrow source, requires the exact registry key, and returns only `kind.Export(snapshot, mode)`. Unknown kind/schema, invalid payloads, or unsupported export fail closed; no raw bytes or generic JSON fallback is allowed.
- `evidence.NewRecoveryAdapter` accepts an explicit registry and narrow recovery repository. Its deterministic inventory/replay contract covers payloads, logical snapshots, intents, revision references, source authorization floor/provenance, and copy lineage. It validates every snapshot key against the injected registry before replay. This allows `comparison.result/*` only after the comparison task explicitly registers its concrete kind/version; a prefix is not an authorization rule. Unknown kind/version fails the whole restore.
- Recovery performs payload GC only after replay and only with a global reference check. Store integration tests must prove a surviving copy preserves its payload and an unreferenced payload is reclaimed.

## Production admission and verification

- No allow-all gate, no typed-nil escape, and no feature enablement belongs to Task 5. With the current nil production gate, Evidence and evidence-backed Records saves must return a stable 503 and must not persist an intent, payload, snapshot, or revision.
- Write the handler RED matrix first: preview/read success, unknown kind, unstable source, stale preview, record/source permission intersection, and response-field allowlist. Add adapter/store RED tests before implementation.
- PostgreSQL is required for this task because correctness depends on FK ordering, transactional intent consumption, record-owned logical deletion, copy survival, gzip/digest reconstruction, replay, and global-reference GC. Use the repository's strict Docker-backed PostgreSQL runner and reject `SKIP` as evidence.
- Minimum gate: focused tests, all affected packages, affected-package race, `make verify-go`, `go test ./... -count=1`, `go vet ./...`, `go mod verify`, changed-Go `gofmt -d`, `git diff --check`, `task.py validate`, and strict PostgreSQL Task 5 integration tests.
