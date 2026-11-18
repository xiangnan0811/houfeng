package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recorddeletion"
)

const (
	comparisonDeletionPreviewDigestDomainV1 = "houfeng.record-comparison.deletion-preview.v1"
	comparisonDeletionImpactDigestDomainV1  = "houfeng.record-comparison.deletion-impact.v1"
)

// ComparisonDependencyIndexHealthSnapshot binds the verified actual result set.
// It deliberately cannot stand in for deletion-adapter readiness.
type ComparisonDependencyIndexHealthSnapshot struct {
	VerifiedSetDigest [sha256.Size]byte
}

// ComparisonDependencyIndexHealth describes only the verified dependency index.
// It is not the health or readiness of a complete comparison deletion adapter.
func (repository *PostgresEvidenceRepository) ComparisonDependencyIndexHealth(ctx context.Context) (ComparisonDependencyIndexHealthSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil {
		return ComparisonDependencyIndexHealthSnapshot{}, ErrRecordPlatformAdmissionUnavailable
	}
	var health ComparisonDependencyIndexHealthSnapshot
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		decision, err := readComparisonDeletionDecision(ctx, transaction.tx, nil)
		if err != nil {
			return err
		}
		health.VerifiedSetDigest = decision.IndexDigest
		return nil
	})
	if err != nil {
		return ComparisonDependencyIndexHealthSnapshot{}, err
	}
	return health, nil
}

// PreviewComparisonDeletion inventories online comparison state without changing
// results, lineage, references, client registrations, or deletion authority.
func (repository *PostgresEvidenceRepository) PreviewComparisonDeletion(ctx context.Context, target recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	if ctx == nil || repository == nil || repository.platform == nil || target.Validate() != nil ||
		target.LockVersion > math.MaxInt64 || target.AuthorizationEpoch > math.MaxInt64 || uint64(target.ContentDeliveryEpoch) > math.MaxInt64 {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrInvalidDeletionPreview
	}
	var preview recorddeletion.AdapterPreviewSnapshot
	err := repository.platform.runRecordPlatformTransaction(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	}, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		decision, err := readComparisonDeletionDecision(ctx, transaction.tx, &target)
		if err != nil {
			return err
		}
		preview = decision.Preview
		return nil
	})
	if err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	return preview, nil
}

// A decision belongs to one database statement snapshot. A future issuer must
// capture it once after acquiring its writer fence and use both snapshots.
type comparisonDeletionDecision struct {
	IndexDigest [sha256.Size]byte
	Health      recorddeletion.AdapterHealthSnapshot
	Preview     recorddeletion.AdapterPreviewSnapshot
	material    []byte
	surviving   int64
	identity    comparisonObservedIdentity
}

type comparisonObservedState = comparisonDeletionDecision

type comparisonObservedIdentity struct {
	ProjectID          string `json:"project_id"`
	RecordID           string `json:"record_id"`
	RevisionID         string `json:"revision_id"`
	LockVersion        int64  `json:"lock_version"`
	AuthorizationEpoch int64  `json:"authorization_epoch"`
	ContentEpoch       int64  `json:"content_epoch"`
}

// The header and every canonical result share one READ COMMITTED statement
// snapshot. Only the at-most-seven dependency tuples for one result are
// aggregated; compressed payloads are returned one result at a time.
const comparisonDeletionDecisionSQL = `
	with owned as materialized (
	  select snapshot_id, payload_digest from public.evidence_snapshots
	  where record_id = $2 and kind = $7
	), retained as materialized (
	  select distinct result.snapshot_id
	  from public.comparison_result_dependencies as dependency
	  join public.evidence_snapshots as result on result.snapshot_id = dependency.result_snapshot_id
	  join public.records as owner on owner.record_id = result.record_id
	  where dependency.project_id = $1 and owner.project_id = $1
	    and dependency.source_record_id = $2 and owner.record_id <> $2 and result.kind = $7
	), header as materialized (
	  select (not $10::boolean or exists (
	    select 1 from public.records as record
	    join public.content_delivery_epochs as epoch
	      on epoch.project_id = record.project_id and epoch.object_kind = 'record'
	     and epoch.object_id = record.record_id
	    where record.project_id = $1 and record.record_id = $2
	      and record.current_revision_id = $3 and record.lock_version = $4
	      and record.authorization_epoch = $5 and epoch.delivery_epoch = $6
	  )) as target_matches,
	  (select count(*) from pg_catalog.pg_class as relation
	    join pg_catalog.pg_namespace as namespace on namespace.oid = relation.relnamespace
	    where namespace.nspname = 'public' and relation.relkind in ('r', 'p')
	      and relation.relname = any($9::text[])) = cardinality($9::text[]) as surfaces_present,
	  pg_catalog.to_regprocedure('public.record_comparison_purge(bytea)') is not null
	    and pg_catalog.to_regprocedure('public.record_comparison_purge_verify(bytea)') is not null
	    and pg_catalog.to_regprocedure('public.record_comparison_purge_v1(bytea)') is not null
	    and pg_catalog.to_regprocedure('public.record_comparison_purge_verify_v1(bytea)') is not null as functions_present,
	  case when $10::boolean then pg_catalog.convert_to(jsonb_build_object(
	    'results', coalesce((select jsonb_agg(jsonb_build_array(snapshot_id, encode(payload_digest, 'hex'))
	      order by snapshot_id collate "C") from owned), '[]'::jsonb),
	    'dependencies', coalesce((select jsonb_agg(jsonb_build_array(
	      dependency.result_snapshot_id, dependency.source_record_id, dependency.source_revision_id,
	      dependency.source_snapshot_id, dependency.project_id, dependency.created_at)
	      order by dependency.result_snapshot_id collate "C", dependency.source_record_id collate "C",
	        dependency.source_revision_id collate "C", dependency.source_snapshot_id collate "C")
	      from public.comparison_result_dependencies as dependency
	      join owned on owned.snapshot_id = dependency.result_snapshot_id), '[]'::jsonb),
	    'completions', coalesce((select jsonb_agg(jsonb_build_array(
	      completion.result_snapshot_id, encode(completion.payload_digest, 'hex'), completion.project_id, completion.indexed_at)
	      order by completion.result_snapshot_id collate "C")
	      from public.comparison_result_dependency_index_completions as completion
	      join owned on owned.snapshot_id = completion.result_snapshot_id), '[]'::jsonb),
	    'workbenches', coalesce((select jsonb_agg(jsonb_build_array(user_id, browser_id,
	      encode(workbench_digest, 'hex'), project_id, destination_record_id, source_record_ids,
	      generation, registered_at, updated_at) order by user_id collate "C", browser_id collate "C", workbench_digest)
	      from public.comparison_client_workbenches where project_id = $1
	        and (destination_record_id = $2 or $2 = any(source_record_ids))), '[]'::jsonb),
	    'save_attempts', coalesce((select jsonb_agg(jsonb_build_array(user_id, browser_id,
	      encode(save_digest, 'hex'), project_id, destination_record_id, source_record_ids, idempotency_key,
	      generation, registered_at, updated_at) order by user_id collate "C", browser_id collate "C", save_digest)
	      from public.comparison_client_save_attempts where project_id = $1
	        and (destination_record_id = $2 or $2 = any(source_record_ids))), '[]'::jsonb),
	    'retained_results', coalesce((select jsonb_agg(snapshot_id order by snapshot_id collate "C") from retained), '[]'::jsonb)
	  )::text, 'UTF8') end as inventory,
	  case when $10::boolean then (select count(*) from retained) else 0 end as surviving,
	  case when $10::boolean then (select jsonb_build_object(
	    'project_id', r.project_id, 'record_id', r.record_id, 'revision_id', r.current_revision_id,
	    'lock_version', r.lock_version, 'authorization_epoch', r.authorization_epoch,
	    'content_epoch', e.delivery_epoch)
	    from public.records r join public.content_delivery_epochs e
	      on e.project_id = r.project_id and e.object_kind = 'record' and e.object_id = r.record_id
	    where r.project_id = $1 and r.record_id = $2) end as identity
	), family as materialized (
	  select 0 as row_kind, null::text as snapshot_id
	  union all
	  select 1, snapshot_id from public.evidence_snapshots where kind = $7
	)
	select family.row_kind,
	  case when family.row_kind = 0 then header.target_matches end,
	  case when family.row_kind = 0 then header.surfaces_present end,
	  case when family.row_kind = 0 then header.functions_present end,
	  case when family.row_kind = 0 then header.inventory end,
	  case when family.row_kind = 0 then header.surviving end,
	  case when family.row_kind = 0 then header.identity end,
	  ` + evidenceSnapshotReadColumns + `,
	  coalesce(payload.payload_encoding, ''), coalesce(payload.canonical_size_bytes, 0),
	  coalesce(payload.compressed_size_bytes, 0), payload.compressed_payload,
	  payload.payload_digest is not null,
	  coalesce(completion.payload_digest = snapshot.payload_digest
	    and snapshot.canonical_hash = completion.payload_digest
	    and snapshot.kind = $7 and snapshot.schema_version = $8, false),
	  dependencies.record_ids, dependencies.revision_ids, dependencies.snapshot_ids
	from family cross join header
	left join public.evidence_snapshots as snapshot on snapshot.snapshot_id = family.snapshot_id
	left join public.evidence_payloads as payload on payload.payload_digest = snapshot.payload_digest
	left join public.comparison_result_dependency_index_completions as completion
	  on completion.result_snapshot_id = snapshot.snapshot_id
	left join lateral (
	  select coalesce(array_agg(dependency.source_record_id order by
	      dependency.source_record_id collate "C", dependency.source_revision_id collate "C", dependency.source_snapshot_id collate "C"), '{}'::text[]) as record_ids,
	    coalesce(array_agg(dependency.source_revision_id order by
	      dependency.source_record_id collate "C", dependency.source_revision_id collate "C", dependency.source_snapshot_id collate "C"), '{}'::text[]) as revision_ids,
	    coalesce(array_agg(dependency.source_snapshot_id order by
	      dependency.source_record_id collate "C", dependency.source_revision_id collate "C", dependency.source_snapshot_id collate "C"), '{}'::text[]) as snapshot_ids
	  from (
	    select source_record_id, coalesce(source_revision_id, '') as source_revision_id,
	      coalesce(source_snapshot_id, '') as source_snapshot_id
	    from public.comparison_result_dependencies where result_snapshot_id = snapshot.snapshot_id
	    order by source_record_id collate "C", coalesce(source_revision_id, '') collate "C", coalesce(source_snapshot_id, '') collate "C"
	    limit 7
	  ) as dependency
	) as dependencies on family.row_kind = 1
	order by family.row_kind, family.snapshot_id collate "C"`

func readComparisonDeletionDecision(ctx context.Context, tx pgx.Tx, target *recorddeletion.PreviewTarget) (comparisonDeletionDecision, error) {
	if target != nil && !validClientDeletionPreviewTarget(*target) {
		return comparisonDeletionDecision{}, recorddeletion.ErrInvalidDeletionPreview
	}
	if ctx == nil || tx == nil {
		return comparisonDeletionDecision{}, recorddeletion.ErrInvalidDeletionPreview
	}
	if err := requireClientPurgeFunctionContracts(ctx, tx, []string{
		"public.record_comparison_purge(bytea)", "public.record_comparison_purge_verify(bytea)",
		"public.record_comparison_purge_v1(bytea)", "public.record_comparison_purge_verify_v1(bytea)",
	}); err != nil {
		return comparisonDeletionDecision{}, err
	}
	recordID := ""
	if target != nil {
		recordID = target.Object.ObjectID
	}
	observed, err := observeComparisonDeletionInTransaction(ctx, tx, recordID)
	if err != nil || target == nil {
		return observed, err
	}
	observed.Preview, err = deriveComparisonPreviewFromObserved(observed, *target)
	return observed, err
}

func observeComparisonDeletionInTransaction(ctx context.Context, tx pgx.Tx, recordID string) (comparisonObservedState, error) {
	if ctx == nil || tx == nil {
		return comparisonObservedState{}, recorddeletion.ErrInvalidDeletionPreview
	}
	projectID := ""
	if recordID != "" {
		projectID = "default"
	}
	surfaces := recorddeletion.RecordComparisonSurfaceNames()
	names := make([]string, len(surfaces))
	for index, surface := range surfaces {
		names[index] = string(surface)
	}
	key := evidence.ComparisonResultV1Key()
	rows, err := tx.Query(ctx, comparisonDeletionDecisionSQL, projectID, recordID, "",
		int64(0), int64(0), int64(0), string(key.Kind), int64(key.SchemaVersion), names, recordID != "")
	if err != nil {
		return comparisonDeletionDecision{}, fmt.Errorf("read comparison deletion decision: %w", err)
	}
	defer rows.Close()

	var rowKind int32
	var targetMatches, surfacesPresent, functionsPresent bool
	var material []byte
	var surviving int64
	var identity []byte
	var row persistedEvidenceSnapshotRow
	destinations := []any{&rowKind, &targetMatches, &surfacesPresent, &functionsPresent, &material, &surviving, &identity}
	snapshotDestinations := persistedEvidenceSnapshotScanDestinations(&row, true)
	destinations = append(destinations, make([]any, len(snapshotDestinations)+5)...)
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return comparisonDeletionDecision{}, fmt.Errorf("read comparison decision header: %w", err)
		}
		return comparisonDeletionDecision{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	if err := rows.Scan(destinations...); err != nil {
		return comparisonDeletionDecision{}, fmt.Errorf("scan comparison decision header: %w", err)
	}
	if rowKind != 0 {
		return comparisonDeletionDecision{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	var observedIdentity comparisonObservedIdentity
	if recordID != "" {
		if len(identity) == 0 || string(identity) == "null" {
			return comparisonDeletionDecision{}, recorddeletion.ErrDeletionPreviewStale
		}
		if len(material) == 0 || surviving < 0 || json.Unmarshal(identity, &observedIdentity) != nil {
			return comparisonDeletionDecision{}, recorddeletion.ErrDeletionSafetyUnavailable
		}
	}

	var payloadPresent, completionBound bool
	var recordIDs, revisionIDs, snapshotIDs []string
	destinations = destinations[:0]
	destinations = append(destinations, &rowKind, nil, nil, nil, nil, nil, nil)
	destinations = append(destinations, snapshotDestinations...)
	destinations = append(destinations, &payloadPresent, &completionBound, &recordIDs, &revisionIDs, &snapshotIDs)
	hasher := sha256.New()
	writeRecordPurgeDigestField(hasher, []byte("houfeng.comparison.dependency-index.v1"))
	for rows.Next() {
		row = persistedEvidenceSnapshotRow{}
		if err := rows.Scan(destinations...); err != nil {
			return comparisonDeletionDecision{}, fmt.Errorf("scan comparison decision result: %w", err)
		}
		if rowKind != 1 || !payloadPresent || !completionBound || len(recordIDs) != len(revisionIDs) || len(recordIDs) != len(snapshotIDs) || len(recordIDs) > 7 {
			return comparisonDeletionDecision{}, evidence.ErrComparisonDependencyIndexIncomplete
		}
		snapshot, owner, valid, err := restorePersistedComparisonSnapshot(row)
		if err != nil {
			return comparisonDeletionDecision{}, err
		}
		if !valid {
			return comparisonDeletionDecision{}, evidence.ErrComparisonDependencyIndexIncomplete
		}
		items, err := evidence.ExtractComparisonDependencyItems(snapshot)
		if err != nil {
			return comparisonDeletionDecision{}, evidence.ErrComparisonDependencyIndexIncomplete
		}
		var dependencyBuffer [7]evidence.ComparisonDependency
		dependencies := dependencyBuffer[:len(recordIDs)]
		for index := range recordIDs {
			dependencies[index] = evidence.ComparisonDependency{
				SourceRecordID: recordIDs[index], SourceRevisionID: revisionIDs[index], SourceSnapshotID: snapshotIDs[index],
			}
		}
		if !validCertifiedComparisonDependencies(completionBound, items, dependencies) {
			return comparisonDeletionDecision{}, evidence.ErrComparisonDependencyIndexIncomplete
		}
		payloadDigest := snapshot.Hash()
		writeRecordPurgeDigestField(hasher, []byte(row.snapshotID))
		writeRecordPurgeDigestField(hasher, []byte(owner))
		writeRecordPurgeDigestField(hasher, payloadDigest[:])
		writeRecordPurgeDigestField(hasher, recordPurgeUint64(uint64(len(dependencies))))
		for _, dependency := range dependencies {
			writeRecordPurgeDigestField(hasher, []byte(dependency.SourceRecordID))
			writeRecordPurgeDigestField(hasher, []byte(dependency.SourceRevisionID))
			writeRecordPurgeDigestField(hasher, []byte(dependency.SourceSnapshotID))
		}
	}
	if err := rows.Err(); err != nil {
		return comparisonDeletionDecision{}, fmt.Errorf("read comparison decision results: %w", err)
	}
	var decision comparisonDeletionDecision
	copy(decision.IndexDigest[:], hasher.Sum(nil))
	surfaceDigest := recorddeletion.RecordComparisonSurfaceDigest()
	proof := digestRecordPurgeBytes(comparisonDeletionHealthDigestDomainV1, surfaceDigest[:], decision.IndexDigest[:])
	decision.Health, err = recorddeletion.NewAdapterHealthSnapshot(surfacesPresent && functionsPresent, 1, proof)
	if err != nil {
		return comparisonDeletionDecision{}, recorddeletion.ErrDeletionSafetyUnavailable
	}
	decision.material, decision.surviving, decision.identity = material, surviving, observedIdentity
	return decision, nil
}

func deriveComparisonPreviewFromObserved(observed comparisonObservedState, target recorddeletion.PreviewTarget) (recorddeletion.AdapterPreviewSnapshot, error) {
	if !validClientDeletionPreviewTarget(target) {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrInvalidDeletionPreview
	}
	identity := observed.identity
	if identity.ProjectID != target.Object.ProjectID || identity.RecordID != target.Object.ObjectID ||
		identity.RevisionID != target.CurrentRevisionID || identity.LockVersion != int64(target.LockVersion) ||
		identity.AuthorizationEpoch != int64(target.AuthorizationEpoch) || identity.ContentEpoch != int64(target.ContentDeliveryEpoch) {
		return recorddeletion.AdapterPreviewSnapshot{}, recorddeletion.ErrDeletionPreviewStale
	}
	preview := recorddeletion.AdapterPreviewSnapshot{
		DependencyDigest: digestRecordPurgeBytes(comparisonDeletionPreviewDigestDomainV1,
			target.DependencyGraphDigest[:], observed.IndexDigest[:], observed.material),
		ImpactDigest: digestRecordPurgeBytes(comparisonDeletionImpactDigestDomainV1,
			observed.IndexDigest[:], observed.material, recordPurgeUint64(uint64(observed.surviving))),
		SurvivingCopies: []recorddeletion.AdapterSurvivingCopy{},
	}
	if observed.surviving > 0 {
		preview.SurvivingCopies = append(preview.SurvivingCopies,
			recorddeletion.KnownAdapterSurvivingCopy(recorddeletion.SurvivingCopyKindOtherRecord, uint64(observed.surviving)))
	}
	if err := preview.Validate(); err != nil {
		return recorddeletion.AdapterPreviewSnapshot{}, err
	}
	return preview, nil
}
