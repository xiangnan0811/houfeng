package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

var (
	_ evidence.ComparisonSubjectResolver   = (*ComparisonLiveSubjectResolver)(nil)
	_ evidence.ComparisonCandidateSource   = (*PostgresEvidenceRepository)(nil)
	_ evidence.ComparisonRecordScopeSource = (*PostgresCurrentRecordAuthorizationSource)(nil)
	_ evidence.ComparisonSelectionSource   = (*PostgresEvidenceRepository)(nil)
)

type ComparisonLiveSubjectResolver struct {
	subjects records.SubjectAdapterRegistry
}

func NewComparisonLiveSubjectResolver(subjects records.SubjectAdapterRegistry) *ComparisonLiveSubjectResolver {
	return &ComparisonLiveSubjectResolver{subjects: subjects}
}

func (resolver *ComparisonLiveSubjectResolver) ResolveLiveSubject(
	ctx context.Context,
	actor recordauth.ActorScope,
	subject evidence.ComparisonSubjectRef,
) error {
	if resolver == nil {
		return evidence.ErrComparisonSubjectNotFound
	}
	kind := records.SubjectKind(subject.Kind)
	if !records.ValidSubjectKind(kind) || !records.ValidSubjectSourceID(kind, subject.ID) {
		return evidence.ErrInvalidComparisonSelection
	}
	_, err := resolver.subjects.Resolve(ctx, actor, records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            kind,
		Role:            records.RelationRoleEvidenceSource,
		SourceID:        subject.ID,
	})
	if err != nil {
		return evidence.ErrComparisonSubjectNotFound
	}
	return nil
}

func (source *PostgresCurrentRecordAuthorizationSource) ResolveComparisonRecordScope(
	ctx context.Context,
	actor recordauth.ActorScope,
	recordID string,
) (recordauth.ResourceScope, error) {
	current, err := source.ResolveCurrentRecordAuthorization(ctx, actor, recordID)
	if err != nil {
		return recordauth.ResourceScope{}, err
	}
	return recordauth.ResourceScope{
		Version:    recordauth.ResourceScopeVersionV1,
		ProjectID:  current.Evidence.ProjectID,
		Visibility: current.Evidence.Visibility,
		Sources:    append([]recordauth.SourceAuthorization(nil), current.Evidence.Sources...),
	}, nil
}

func (repository *PostgresEvidenceRepository) ListComparisonCandidateRefs(
	ctx context.Context,
	subjects []evidence.ComparisonSubjectRef,
	window evidence.TimeWindow,
	kinds []evidence.KindKey,
) ([]evidence.ComparisonCandidateRef, error) {
	if ctx == nil || repository == nil || len(subjects) < 2 || len(subjects) > 6 {
		return nil, evidence.ErrInvalidComparisonSelection
	}
	subjectKinds := make([]string, 0, len(subjects))
	subjectIDs := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		subjectKinds = append(subjectKinds, subject.Kind)
		subjectIDs = append(subjectIDs, subject.ID)
	}
	kindNames := make([]string, 0, len(kinds))
	kindVersions := make([]int64, 0, len(kinds))
	for _, key := range kinds {
		kindNames = append(kindNames, string(key.Kind))
		kindVersions = append(kindVersions, int64(key.SchemaVersion))
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, comparisonCandidateListSQL,
		subjectKinds, subjectIDs, window.Start.UTC(), window.End.UTC(), kindNames, kindVersions,
	)
	if err != nil {
		return nil, fmt.Errorf("list comparison candidates: %w", err)
	}
	defer rows.Close()

	refs := make([]evidence.ComparisonCandidateRef, 0)
	for rows.Next() {
		ref, err := scanComparisonCandidateRef(rows)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comparison candidates: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit comparison candidate list: %w", err)
	}
	return refs, nil
}

func (repository *PostgresEvidenceRepository) LoadComparisonSnapshots(
	ctx context.Context,
	actor evidence.ActorScope,
	ids []string,
) (map[string]evidence.ComparisonLoadedSnapshot, error) {
	if ctx == nil || repository == nil {
		return nil, evidence.ErrInvalidComparisonSelection
	}
	out := make(map[string]evidence.ComparisonLoadedSnapshot, len(ids))
	for _, snapshotID := range uniqueComparisonIDs(ids) {
		if !evidence.ValidSnapshotID(snapshotID) {
			continue
		}
		persisted, recordScope, source, available, snapshot, err := repository.loadAndAuthorizeEvidenceSnapshot(ctx, actor, snapshotID, true)
		if err == nil {
			out[snapshotID] = evidence.ComparisonLoadedSnapshot{
				SnapshotID: persisted.snapshotID, RecordID: persisted.recordID,
				Kind: persisted.envelope.Key, Hash: persisted.payloadDigest, Snapshot: snapshot,
				RecordScope: recordScope, SourceAuthorization: source, SourceAvailable: available,
			}
			continue
		}
		meta, metaScope, metaSource, metaAvailable, _, metaErr := repository.loadAndAuthorizeEvidenceSnapshot(ctx, actor, snapshotID, false)
		include, unreadable := comparisonSnapshotLoadDecision(err, metaErr)
		if !include {
			continue
		}
		out[snapshotID] = evidence.ComparisonLoadedSnapshot{
			SnapshotID: meta.snapshotID, RecordID: meta.recordID,
			Kind: meta.envelope.Key, Hash: meta.payloadDigest,
			RecordScope: metaScope, SourceAuthorization: metaSource, SourceAvailable: metaAvailable,
			Unreadable: unreadable,
		}
	}
	return out, nil
}

func comparisonSnapshotLoadDecision(payloadErr, metadataErr error) (include bool, unreadable bool) {
	if payloadErr == nil {
		return true, false
	}
	if metadataErr == nil {
		return true, true
	}
	return false, false
}

func (repository *PostgresEvidenceRepository) LoadComparisonRevisions(
	ctx context.Context,
	actor evidence.ActorScope,
	keys []evidence.ComparisonRevisionKey,
) (map[evidence.ComparisonRevisionKey]evidence.ComparisonLoadedRevision, error) {
	if ctx == nil || repository == nil {
		return nil, evidence.ErrInvalidComparisonSelection
	}
	recordIDs := make([]string, 0, len(keys))
	revisionIDs := make([]string, 0, len(keys))
	wanted := make(map[evidence.ComparisonRevisionKey]struct{}, len(keys))
	for _, key := range keys {
		if !validEvidenceStoreID(key.RecordID, "rec_") || !validEvidenceStoreID(key.RevisionID, "rrv_") {
			continue
		}
		recordIDs = append(recordIDs, key.RecordID)
		revisionIDs = append(revisionIDs, key.RevisionID)
		wanted[key] = struct{}{}
	}
	if len(wanted) == 0 {
		return map[evidence.ComparisonRevisionKey]evidence.ComparisonLoadedRevision{}, nil
	}
	tx, err := repository.startAdmittedTransaction(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, comparisonRevisionListSQL, recordIDs, revisionIDs)
	if err != nil {
		return nil, fmt.Errorf("list comparison revisions: %w", err)
	}
	defer rows.Close()
	loaded := make(map[evidence.ComparisonRevisionKey]evidence.ComparisonLoadedRevision)
	for rows.Next() {
		revision, err := scanComparisonRevision(rows)
		if err != nil {
			return nil, err
		}
		key := evidence.ComparisonRevisionKey{RecordID: revision.RecordID, RevisionID: revision.RevisionID}
		if _, ok := wanted[key]; !ok {
			continue
		}
		loaded[key] = revision
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comparison revisions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit comparison revision list: %w", err)
	}
	out := make(map[evidence.ComparisonRevisionKey]evidence.ComparisonLoadedRevision, len(loaded))
	scopes := make(map[string]recordauth.ResourceScope)
	for key, revision := range loaded {
		scope, ok := scopes[key.RecordID]
		if !ok {
			scope, err = repository.resolveComparisonRevisionScope(ctx, actor, key.RecordID)
			if err != nil {
				scopes[key.RecordID] = recordauth.ResourceScope{}
				continue
			}
			scopes[key.RecordID] = scope
		}
		if scope.Version == 0 {
			continue
		}
		revision.RecordScope = scope
		out[key] = revision
	}
	return out, nil
}

func (repository *PostgresEvidenceRepository) resolveComparisonRevisionScope(
	ctx context.Context,
	actor evidence.ActorScope,
	recordID string,
) (recordauth.ResourceScope, error) {
	if repository == nil || repository.current == nil {
		return recordauth.ResourceScope{}, evidence.ErrComparisonSelectionNotFound
	}
	current, err := repository.current.ResolveCurrentRecordAuthorization(ctx, actor, recordID)
	if err != nil {
		return recordauth.ResourceScope{}, err
	}
	return recordauth.ResourceScope{
		Version:    recordauth.ResourceScopeVersionV1,
		ProjectID:  current.Evidence.ProjectID,
		Visibility: current.Evidence.Visibility,
		Sources:    append([]recordauth.SourceAuthorization(nil), current.Evidence.Sources...),
	}, nil
}

const comparisonRevisionListSQL = `
	select
	  revision.record_id,
	  revision.revision_id,
	  revision.record_type,
	  coalesce(revision.business_status, ''),
	  coalesce(revision.status_group, ''),
	  revision.impact_level,
	  revision.occurred_at,
	  coalesce(refs.snapshot_ids, '{}')
	from public.record_revisions as revision
	join unnest($1::text[], $2::text[]) as wanted(record_id, revision_id)
	  on revision.record_id = wanted.record_id
	 and revision.revision_id = wanted.revision_id
	left join lateral (
	  select coalesce(array_agg(evidence.snapshot_id order by evidence.ordinal), '{}') as snapshot_ids
	  from public.record_revision_evidence as evidence
	  where evidence.revision_id = revision.revision_id
	) as refs on true`

func scanComparisonRevision(row pgx.Row) (evidence.ComparisonLoadedRevision, error) {
	var recordID, revisionID, recordType, businessStatus, statusGroup, impactLevel string
	var occurredAt *time.Time
	var snapshotIDs []string
	if err := row.Scan(
		&recordID, &revisionID, &recordType, &businessStatus, &statusGroup, &impactLevel, &occurredAt, &snapshotIDs,
	); err != nil {
		return evidence.ComparisonLoadedRevision{}, fmt.Errorf("scan comparison revision: %w", err)
	}
	if snapshotIDs == nil {
		snapshotIDs = []string{}
	}
	metadata := evidence.RevisionMetadataSnapshot{
		RecordType: recordType, BusinessStatus: businessStatus, StatusGroup: statusGroup, ImpactLevel: impactLevel,
	}
	if occurredAt != nil {
		metadata.OccurredAt = occurredAt.UTC()
		metadata.HasOccurredAt = true
	}
	return evidence.ComparisonLoadedRevision{
		RecordID: recordID, RevisionID: revisionID, Metadata: metadata, SnapshotIDs: snapshotIDs,
	}, nil
}

func uniqueComparisonIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

const comparisonCandidateListSQL = `
	select
	  wanted.kind,
	  wanted.id,
	  snapshot.snapshot_id,
	  snapshot.record_id,
	  snapshot.kind,
	  snapshot.schema_version,
	  snapshot.requested_started_at,
	  snapshot.requested_ended_at,
	  snapshot.actual_started_at,
	  snapshot.actual_ended_at,
	  snapshot.captured_at,
	  snapshot.canonical_hash,
	  snapshot.quality,
	  snapshot.capture_authorization,
	  coalesce(refs.revision_ids, '{}')
	from public.evidence_snapshots as snapshot
	join unnest($1::text[], $2::text[]) as wanted(kind, id)
	  on (
	    (snapshot.source_kind = wanted.kind and snapshot.source_id = wanted.id)
	    or (
	      snapshot.subject_identity_snapshot->>'Type' = wanted.kind
	      and snapshot.subject_identity_snapshot->>'ID' = wanted.id
	    )
	  )
	left join lateral (
	  select coalesce(array_agg(revision.revision_id order by revision.revision_id), '{}') as revision_ids
	  from public.record_revision_evidence as revision
	  where revision.record_id = snapshot.record_id
	    and revision.snapshot_id = snapshot.snapshot_id
	) as refs on true
	where snapshot.actual_started_at < $4
	  and snapshot.actual_ended_at > $3
	  and (
	    cardinality($5::text[]) = 0
	    or (snapshot.kind, snapshot.schema_version) in (
	      select kind_name, schema_version
	      from unnest($5::text[], $6::bigint[]) as filter(kind_name, schema_version)
	    )
	  )
	order by wanted.kind, wanted.id, snapshot.snapshot_id`

func scanComparisonCandidateRef(row pgx.Row) (evidence.ComparisonCandidateRef, error) {
	var (
		subjectKind, subjectID, snapshotID, recordID, kind               string
		schemaVersion                                                    int64
		requestedStart, requestedEnd, actualStart, actualEnd, capturedAt time.Time
		hash, qualityJSON, authorizationJSON                             []byte
		revisionIDs                                                      []string
	)
	if err := row.Scan(
		&subjectKind, &subjectID, &snapshotID, &recordID, &kind, &schemaVersion,
		&requestedStart, &requestedEnd, &actualStart, &actualEnd, &capturedAt,
		&hash, &qualityJSON, &authorizationJSON, &revisionIDs,
	); err != nil {
		return evidence.ComparisonCandidateRef{}, fmt.Errorf("scan comparison candidate: %w", err)
	}
	if len(hash) != 32 {
		return evidence.ComparisonCandidateRef{}, evidence.ErrEvidenceServiceUnavailable
	}
	var digest [32]byte
	copy(digest[:], hash)
	var quality evidence.Quality
	if len(qualityJSON) > 0 && json.Unmarshal(qualityJSON, &quality) != nil {
		return evidence.ComparisonCandidateRef{}, evidence.ErrEvidenceServiceUnavailable
	}
	var authorization recordauth.SourceAuthorization
	if len(authorizationJSON) > 0 {
		_ = json.Unmarshal(authorizationJSON, &authorization)
	}
	if revisionIDs == nil {
		revisionIDs = []string{}
	}
	return evidence.ComparisonCandidateRef{
		Subject:    evidence.ComparisonSubjectRef{Kind: subjectKind, ID: subjectID},
		SnapshotID: snapshotID, RecordID: recordID,
		RevisionIDs:     revisionIDs,
		Kind:            evidence.KindKey{Kind: evidence.KindName(kind), SchemaVersion: evidence.SchemaVersion(schemaVersion)},
		CanonicalHash:   digest,
		RequestedWindow: evidence.TimeWindow{Start: requestedStart.UTC(), End: requestedEnd.UTC()},
		ActualWindow:    evidence.TimeWindow{Start: actualStart.UTC(), End: actualEnd.UTC()},
		Quality:         quality, CapturedAt: capturedAt.UTC(), SourceAuthorization: authorization,
	}, nil
}
