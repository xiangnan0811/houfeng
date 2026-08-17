# Records Import, Export, and Portability Design

## 1. Boundary

Portability translates authoritative Records domain data into and out of
versioned artifacts. It does not migrate old application tables and does not
own mutable search/activity projections.

The child depends on domain-owned providers and participants rather than reading
another package's tables directly.

This child also closes the production authority deferred by earlier Records
children: a concrete transaction-scoped deployment-membership
`store.AdmissionGate`, witnessed source-deletion tombstone reads, and the
integrity-valid external evidence quarantine. These contracts bind the external
deletion-ledger/contract-activation identity; they are not inferred from APP ACL
state, local digests, archive claims, or test adapters. Child 11 composes and
verifies them before enabling protected capabilities.

## 2. Modules

- `internal/center/portability`: archive, preview, export/import services, plans,
  remapping, workers, policy, and adapter registries.
- `internal/center/store/record_portability.go`: job/plan/origin/artifact store.
- `internal/center/http/handlers/record_portability.go`: authenticated API.
- `web/src/pages/records` lazy import/export workflows.
- domain-owned provider/participant adapters remain in Records, Attachments,
  Evidence, Collaboration, Activity, and Comparison packages.

Dependency direction is portability -> published domain interfaces. Domain
packages must not import portability.

## 3. Migration 0058

`0058_create_record_portability.sql` contains metadata and state, not duplicate
Records content:

- `record_export_jobs` and `record_export_artifacts`;
- `record_import_jobs`, `record_import_plans`, and `record_import_artifacts`;
- `record_import_entity_mappings`;
- `record_origins` and `record_origin_tombstones`;
- `record_portability_purge_receipts`.

Rows use bounded enums, CAS versions, expiry, content classification, immutable
artifact version/hash, and explicit local operator/source provenance. Long-lived
origin/tombstone rows contain no title, Markdown, filename, evidence summary, or
free-text error.

Search/activity checkpoints, renderer caches, browser profiles, and raw
`experience_logs` mappings are absent.

## 4. Provider and participant contracts

```go
type ExportProvider interface {
	Kind() string
	Preview(context.Context, ExportScope) (ExportContribution, error)
	Write(context.Context, FixedExport, ArchiveWriter) error
}

type ImportParticipant interface {
	Kind() string
	Plan(context.Context, ValidatedArchive, IDMap) (ImportContribution, error)
	Apply(context.Context, pgx.Tx, FixedImportPlan) error
}
```

The registry is closed for the build. Missing required providers/participants
block preview or apply. Export contributions carry authorization/inventory
versions; the service rechecks them before publish. Apply runs in the Records
transaction participant chain where possible; Blob publication uses staged
objects and a durable cleanup receipt.

## 5. Archive v1

`houfeng-record-archive/v1` uses a ZIP64 container with a typed canonical JSON
manifest. Paths are normalized UTF-8 relative paths with no links, devices,
empty segments, `.`/`..`, or normalization collisions. Manifest entries are
sorted and contain media type, byte count, SHA-256, and classification.

The reader streams bounded entries and never trusts ZIP declared expansion size.
Limits cover archive bytes, expanded bytes, entry count, per-metadata entity,
path length/depth, manifest bytes, compression ratio, and working set.

Required schemas are versioned. Unknown required schema blocks the plan. An
unknown optional payload can remain quarantined only with locally derived
classification and no render/compare/re-export authority.

Optional signatures bind exact manifest bytes. Signature trust is advisory
provenance unless local policy explicitly requires it; it never grants local
authorization.

## 6. Export flow

```text
request -> authorized preview -> fixed contribution inventory/token
        -> staged writer -> recheck auth/fence/readiness
        -> hash/manifest/sign -> atomic publish -> expiring download
```

Human Markdown and PDF consume the same domain RenderModel. PDF generation runs
in the existing isolated content processor with network disabled. Attachment
bytes are explicitly selected; evidence uses its schema-owned exporter.

Download authorization and content lease are checked before headers and during
streaming. A revoke or deletion reservation stops new bytes; already delivered
external copies are disclosed but cannot be recalled.

## 7. Import flow

```text
quarantine bytes -> structural/integrity/schema validation
                 -> dry-run + ID preallocation + fixed plan
                 -> operator confirmation
                 -> staged blobs + one domain transaction
                 -> publish blobs/receipt or compensate
                 -> rebuild projections
```

The dry-run is read-only and lists exact counts, mappings, warnings, blockers,
capacity, and expiry. Apply rechecks plan digest, policy, authorization, fence,
capacity, and trust inputs.

Foreign users remain imported provenance. Local authorship/ownership is assigned
only through explicit local input validated by normal domain policy.

## 8. Deletion and recovery

Portability registers adapters for job rows, quarantine, published artifacts,
workspaces, mappings, and origin facts. Permanent delete removes content-bearing
rows and locators. A minimal origin tombstone and deletion-ledger reference may
remain to prevent official re-import.

Backup includes only declared published artifacts and active recoverable jobs.
Temporary/quarantine/workspace data is either excluded with a cleanup contract
or inventoried explicitly. Restore verifies object version/hash, then replays
deletion outcomes before traffic.

The source-deletion witness returns a typed final source identity and
authorization floor bound to the witnessed ledger entry. Missing, stale,
unknown-version, discontinuous, or unreachable witness state fails closed; a
local digest-only tombstone is never a substitute. The deployment-membership
gate reads the existing `0051` `deployment_membership` and
`deployment_contract_state` authority in every admitted transaction, uses the
same activated deployment identity, and rejects nil/typed-nil or drift before
any business write. `0058` does not duplicate those authority tables.

Unsupported external evidence remains quarantine-only after the archive and
entry integrity layers succeed. Only allowlisted kind/schema/time/size/digest
metadata may be shown; payload bytes never reach a generic JSON renderer and the
entry cannot be applied, compared, copied, or re-exported as trusted evidence.

## 9. Compatibility

Only archive format compatibility is supported. The database itself follows the
current development baseline and may be rebuilt. Archive v1 readers keep stable
conformance fixtures; future format changes add a new major/minor contract
instead of reinterpreting old bytes.

There is no `experience_logs` conversion path.

## 10. Rollback

Feature-off hides import/export routes and stops workers. Additive `0058` rows
remain inert. Unpublished staged objects are cleaned by janitor. A database
rollback to code without `0058` requires rebuilding the development database.
