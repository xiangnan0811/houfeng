package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
)

// tryIndexInheritedComparisonResult is also the history/import/recovery path:
// canonical validity is checked even when provenance is unavailable. A missing
// proof leaves the result uncompleted; embedded IDs are never joined to local
// sources as a fallback. trustedSourceID must come from an actual local copy
// operation or its retained result-bound lineage, not from canonical items.
func tryIndexInheritedComparisonResult(ctx context.Context, tx pgx.Tx, recordID, snapshotID string, snapshot evidence.CanonicalSnapshot, trustedSourceID string) (bool, error) {
	if snapshot.Envelope().Key.Kind != evidence.ComparisonResultV1Key().Kind {
		return true, nil
	}
	items, err := evidence.ExtractComparisonDependencyItems(snapshot)
	if err != nil {
		return false, nil
	}
	digest := snapshot.Hash()
	if _, complete, err := readCertifiedComparisonDependencies(ctx, tx, snapshotID, digest, items); err != nil || complete {
		return complete, err
	}
	if trustedSourceID == "" || trustedSourceID == snapshotID {
		return false, nil
	}
	dependencies, complete, err := readCertifiedComparisonDependencies(ctx, tx, trustedSourceID, digest, items)
	if err != nil || !complete {
		return false, err
	}
	if err := insertComparisonDependencyIndex(ctx, tx, recordID, snapshotID, snapshot, dependencies, trustedSourceID); err != nil {
		return false, err
	}
	return true, nil
}

func readCertifiedComparisonDependencies(ctx context.Context, tx pgx.Tx, snapshotID string, digest [sha256.Size]byte, items []evidence.ComparisonDependencyItem) ([]evidence.ComparisonDependency, bool, error) {
	var bound bool
	if err := tx.QueryRow(ctx, `
		select exists (
			select 1 from public.comparison_result_dependency_index_completions as completion
			join public.evidence_snapshots as snapshot on snapshot.snapshot_id = completion.result_snapshot_id
			where completion.result_snapshot_id = $1 and completion.payload_digest = $2
			  and snapshot.payload_digest = completion.payload_digest
			  and snapshot.canonical_hash = completion.payload_digest
			  and snapshot.kind = $3 and snapshot.schema_version = $4
		)`, snapshotID, digest[:], string(evidence.ComparisonResultV1Key().Kind), int64(evidence.ComparisonResultV1Key().SchemaVersion)).Scan(&bound); err != nil {
		return nil, false, fmt.Errorf("read comparison index completion: %w", err)
	}
	if !bound {
		return nil, false, nil
	}
	rows, err := tx.Query(ctx, `
		select source_record_id, coalesce(source_revision_id, ''), coalesce(source_snapshot_id, '')
		from public.comparison_result_dependencies where result_snapshot_id = $1
		order by source_record_id collate "C", coalesce(source_revision_id, '') collate "C", coalesce(source_snapshot_id, '') collate "C"
		limit 7`, snapshotID)
	if err != nil {
		return nil, false, fmt.Errorf("read comparison dependency coverage: %w", err)
	}
	defer rows.Close()
	dependencies := make([]evidence.ComparisonDependency, 0, len(items))
	for rows.Next() {
		var dependency evidence.ComparisonDependency
		if err := rows.Scan(&dependency.SourceRecordID, &dependency.SourceRevisionID, &dependency.SourceSnapshotID); err != nil {
			return nil, false, fmt.Errorf("scan comparison dependency: %w", err)
		}
		dependencies = append(dependencies, dependency)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read comparison dependencies: %w", err)
	}
	if !validCertifiedComparisonDependencies(bound, items, dependencies) {
		return nil, false, nil
	}
	return dependencies, true, nil
}

func validCertifiedComparisonDependencies(bound bool, items []evidence.ComparisonDependencyItem, dependencies []evidence.ComparisonDependency) bool {
	return bound && evidence.ComparisonDependencyCoverageMatches(items, dependencies)
}

func loadComparisonCanonicalSnapshot(ctx context.Context, tx pgx.Tx, snapshotID string) (evidence.CanonicalSnapshot, string, bool, error) {
	var row persistedEvidenceSnapshotRow
	if err := scanPersistedEvidenceSnapshotRow(tx.QueryRow(ctx, fullEvidenceSnapshotReadSQL, snapshotID), &row, true); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return evidence.CanonicalSnapshot{}, "", false, nil
		}
		return evidence.CanonicalSnapshot{}, "", false, fmt.Errorf("load comparison index candidate: %w", err)
	}
	return restorePersistedComparisonSnapshot(row)
}

func restorePersistedComparisonSnapshot(row persistedEvidenceSnapshotRow) (evidence.CanonicalSnapshot, string, bool, error) {
	persisted, err := decodePersistedEvidenceSnapshot(row, true)
	if err != nil || persisted.envelope.Key != evidence.ComparisonResultV1Key() {
		return evidence.CanonicalSnapshot{}, "", false, nil
	}
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		return evidence.CanonicalSnapshot{}, "", false, err
	}
	snapshot, err := evidence.RestoreCanonicalSnapshot(kind.Descriptor(), persisted.envelope, persisted.canonicalPayload)
	if err != nil {
		return evidence.CanonicalSnapshot{}, "", false, nil
	}
	return snapshot, persisted.recordID, true, nil
}

// Backfill may inherit only through a certified parent result which binds this
// actual copy ID and content to a source. Neither an unkeyed ID derivation nor
// a recovery-supplied lineage row is a native-origin attestation by itself.
func backfillPersistedComparisonResult(ctx context.Context, tx pgx.Tx, snapshotID string) (bool, error) {
	snapshot, recordID, valid, err := loadComparisonCanonicalSnapshot(ctx, tx, snapshotID)
	if err != nil || !valid {
		return false, err
	}
	items, err := evidence.ExtractComparisonDependencyItems(snapshot)
	if err != nil {
		return false, nil
	}
	digest := snapshot.Hash()
	if _, complete, err := readCertifiedComparisonDependencies(ctx, tx, snapshotID, digest, items); err != nil || complete {
		return complete, err
	}
	rows, err := tx.Query(ctx, `
		select distinct parent.snapshot_id
		from public.record_revision_evidence as copied_ref
		join public.record_revision_evidence as parent_ref
		  on parent_ref.revision_id = copied_ref.revision_id and parent_ref.record_id = copied_ref.record_id
		join public.evidence_snapshots as parent on parent.snapshot_id = parent_ref.snapshot_id
		join public.comparison_result_dependency_index_completions as completion
		  on completion.result_snapshot_id = parent.snapshot_id and completion.payload_digest = parent.payload_digest
		where copied_ref.snapshot_id = $1 and copied_ref.record_id = $2
		  and parent.snapshot_id <> $1 and parent.kind = $3 and parent.schema_version = $4
		order by parent.snapshot_id`,
		snapshotID, recordID, string(evidence.ComparisonResultV1Key().Kind), int64(evidence.ComparisonResultV1Key().SchemaVersion))
	if err != nil {
		return false, fmt.Errorf("find certified comparison copy parents: %w", err)
	}
	parentIDs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, fmt.Errorf("read certified comparison copy parents: %w", err)
	}
	var encodedHash [64]byte
	hex.Encode(encodedHash[:], digest[:])
	for _, parentID := range parentIDs {
		parent, _, valid, err := loadComparisonCanonicalSnapshot(ctx, tx, parentID)
		if err != nil {
			return false, err
		}
		if !valid {
			continue
		}
		parentItems, err := evidence.ExtractComparisonDependencyItems(parent)
		if err != nil {
			continue
		}
		parentDependencies, complete, err := readCertifiedComparisonDependencies(ctx, tx, parentID, parent.Hash(), parentItems)
		if err != nil {
			return false, err
		}
		if !complete {
			continue
		}
		for _, item := range parentItems {
			if item.CopiedSnapshotID != snapshotID || item.Hash != string(encodedHash[:]) ||
				item.Kind != evidence.ComparisonResultV1Key().String() {
				continue
			}
			var sourceRecordID string
			for _, dependency := range parentDependencies {
				if dependency.SourceSnapshotID == item.OriginalSnapshotID && dependency.SourceRevisionID == item.RevisionID {
					sourceRecordID = dependency.SourceRecordID
					break
				}
			}
			var bound bool
			if err := tx.QueryRow(ctx, `
				select exists (
					select 1 from public.evidence_copy_lineage as lineage
					join public.evidence_snapshots as source on source.snapshot_id = lineage.copied_from_snapshot_id
					where lineage.snapshot_id = $1 and lineage.copied_from_snapshot_id = $2
					  and lineage.copy_reason = $3 and source.record_id = $4 and source.payload_digest = $5
				)`, snapshotID, item.OriginalSnapshotID, evidence.ComparisonCopyReason, sourceRecordID, digest[:]).Scan(&bound); err != nil {
				return false, fmt.Errorf("validate certified comparison copy route: %w", err)
			}
			if !bound {
				continue
			}
			dependencies, complete, err := readCertifiedComparisonDependencies(ctx, tx, item.OriginalSnapshotID, digest, items)
			if err != nil {
				return false, err
			}
			if !complete {
				continue
			}
			if err := insertComparisonDependencyIndex(ctx, tx, recordID, snapshotID, snapshot, dependencies, item.OriginalSnapshotID); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	return false, nil
}
