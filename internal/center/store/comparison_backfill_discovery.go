package store

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/records"
)

// Comparison backfill can read several retained results, their shared payloads,
// revision references and certified source records. Discover that complete
// graph before taking record locks; the caller repeats it after acquisition.
func discoverComparisonBackfillResources(ctx context.Context, tx pgx.Tx, snapshotID, recordID string) (recordMutationResources, error) {
	rows, err := tx.Query(ctx, `with parents as (
		select distinct parent.snapshot_id
		from public.record_revision_evidence copied
		join public.record_revision_evidence parent_ref on parent_ref.revision_id=copied.revision_id and parent_ref.record_id=copied.record_id
		join public.evidence_snapshots parent on parent.snapshot_id=parent_ref.snapshot_id
		join public.comparison_result_dependency_index_completions completion on completion.result_snapshot_id=parent.snapshot_id and completion.payload_digest=parent.payload_digest
		where copied.snapshot_id=$1 and copied.record_id=$2 and parent.snapshot_id<>$1 and parent.kind=$3 and parent.schema_version=$4
	), related as (
		select $1::text as snapshot_id
		union select snapshot_id from parents
		union select copied_from_snapshot_id from public.evidence_copy_lineage where snapshot_id=$1
		union select source_snapshot_id from public.comparison_result_dependencies
		  where result_snapshot_id in (select snapshot_id from parents union select $1::text) and source_snapshot_id is not null
	)
	select snapshot_id from related order by snapshot_id collate "C"`, snapshotID, recordID,
		string(evidence.ComparisonResultV1Key().Kind), int64(evidence.ComparisonResultV1Key().SchemaVersion))
	if err != nil {
		return recordMutationResources{}, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return recordMutationResources{}, err
	}
	var set records.RevisionMutationResources
	for _, id := range ids {
		snapshot, err := readRevisionMutationSnapshot(ctx, tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return recordMutationResources{}, err
		}
		if id == snapshotID && snapshot.RecordID != recordID {
			return recordMutationResources{}, ErrEvidencePersistenceConflict
		}
		set.Snapshots = append(set.Snapshots, snapshot)
		set.Payloads = append(set.Payloads, records.RevisionMutationPayload{PayloadDigest: snapshot.PayloadDigest})
		set.Records = append(set.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: snapshot.RecordID})
		set.Sources = append(set.Sources, records.RevisionMutationSource{ProjectID: "default", SourceKind: snapshot.SourceKind, SourceID: snapshot.SourceID})
	}
	dependencies, err := readRevisionMutationRows[records.RevisionMutationDependency](ctx, tx, `select result_snapshot_id,source_record_id,coalesce(source_revision_id,''),coalesce(source_snapshot_id,'')
		from public.comparison_result_dependencies where result_snapshot_id=any($1::text[])
		order by result_snapshot_id,source_record_id,source_revision_id,source_snapshot_id`, ids)
	if err != nil {
		return recordMutationResources{}, err
	}
	set.Dependencies = append(set.Dependencies, dependencies...)
	for _, dependency := range dependencies {
		set.Records = append(set.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: dependency.SourceRecordID})
		if dependency.SourceRevisionID != "" {
			set.Revisions = append(set.Revisions, records.RevisionMutationRevision{RecordID: dependency.SourceRecordID, RevisionID: dependency.SourceRevisionID})
		}
	}
	lineages, err := readRevisionMutationRows[records.RevisionMutationLineage](ctx, tx, `select snapshot_id,copied_from_snapshot_id from public.evidence_copy_lineage where snapshot_id=any($1::text[]) order by snapshot_id`, ids)
	if err != nil {
		return recordMutationResources{}, err
	}
	set.Lineages = append(set.Lineages, lineages...)
	revisions, err := readRevisionMutationRows[records.RevisionMutationRevision](ctx, tx, `select distinct record_id,revision_id from public.record_revision_evidence where snapshot_id=any($1::text[]) order by record_id,revision_id`, ids)
	if err != nil {
		return recordMutationResources{}, err
	}
	set.Revisions = append(set.Revisions, revisions...)
	for _, revision := range revisions {
		set.Records = append(set.Records, records.RevisionMutationRecord{ProjectID: "default", RecordID: revision.RecordID})
	}
	rows, err = tx.Query(ctx, `select result_snapshot_id,payload_digest from public.comparison_result_dependency_index_completions where result_snapshot_id=any($1::text[]) order by result_snapshot_id`, ids)
	if err != nil {
		return recordMutationResources{}, err
	}
	for rows.Next() {
		var completion records.RevisionMutationCompletion
		var digest []byte
		if err := rows.Scan(&completion.ResultSnapshotID, &digest); err != nil {
			rows.Close()
			return recordMutationResources{}, err
		}
		if len(digest) != sha256.Size {
			rows.Close()
			return recordMutationResources{}, records.ErrInvalidRevisionMutationResources
		}
		copy(completion.PayloadDigest[:], digest)
		set.Completions = append(set.Completions, completion)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return recordMutationResources{}, err
	}
	normalized, err := records.CombineRevisionMutationResources(set)
	if err != nil {
		return recordMutationResources{}, err
	}
	for _, record := range normalized.Records {
		if err := appendEvidenceCurrentRecordResources(ctx, tx, &normalized, record); err != nil {
			return recordMutationResources{}, err
		}
		if !normalized.RootObservations[len(normalized.RootObservations)-1].Exists {
			return recordMutationResources{}, records.ErrRecordNotFound
		}
	}
	return normalizeRecordMutationDeclarations(normalized)
}
