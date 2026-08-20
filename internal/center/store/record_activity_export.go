package store

import (
	"context"
	"encoding/json"
	"fmt"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

var _ activity.ExportPageStore = (*ActivityProjectionRepository)(nil)

// ScanExportRecordPage returns one page of projected envelopes for an explicit
// record selection at a fixed snapshot. Auth filtering uses the same digest
// allowlist as subject pages so export never sees more than the online path.
func (repository *ActivityProjectionRepository) ScanExportRecordPage(
	ctx context.Context,
	actor recordauth.ActorScope,
	selection activity.RecordSelection,
	snapshot activity.ActivitySnapshot,
	cursor activity.PageCursor,
	limit int,
) (activity.ActivityPage, error) {
	if ctx == nil || repository == nil || repository.pool == nil || limit < 1 {
		return activity.ActivityPage{}, activity.ErrInvalidListRequest
	}
	if !selection.Normalized() || snapshot.ProjectionGeneration == 0 {
		return activity.ActivityPage{}, activity.ErrInvalidListRequest
	}
	auth, err := activity.AuthFilterForActor(actor)
	if err != nil {
		return activity.ActivityPage{}, err
	}

	arguments := []any{
		recordplatform.ProjectIDDefault,
		int64(snapshot.ProjectionGeneration),
		int64(snapshot.PublishedIngestSequence),
		selection.RecordIDs,
	}
	conditions := []string{
		"p.project_id = $1",
		"p.projection_generation = $2",
		"p.ingest_sequence <= $3",
		"p.record_id = any($4::text[])",
	}
	if !auth.Unrestricted {
		digests := make([][]byte, 0, len(auth.AllowedAuthDigests))
		for _, digest := range auth.AllowedAuthDigests {
			copyDigest := digest
			digests = append(digests, copyDigest[:])
		}
		arguments = append(arguments, digests)
		conditions = append(conditions, fmt.Sprintf("p.auth_scope_digest = any($%d::bytea[])", len(arguments)))
	}
	if !cursor.FirstPage() {
		position := cursor.Position
		eventAtArg := len(arguments) + 1
		arguments = append(arguments, position.EventAt.UTC())
		recordedAtArg := len(arguments) + 1
		arguments = append(arguments, position.RecordedAt.UTC())
		sourceKindArg := len(arguments) + 1
		arguments = append(arguments, string(position.SourceKind))
		activityIDArg := len(arguments) + 1
		arguments = append(arguments, position.ActivityID)
		conditions = append(conditions, fmt.Sprintf(`(
		  p.event_at < $%d
		  or (p.event_at = $%d and p.recorded_at < $%d)
		  or (p.event_at = $%d and p.recorded_at = $%d and p.source_kind > $%d)
		  or (p.event_at = $%d and p.recorded_at = $%d and p.source_kind = $%d and p.activity_id > $%d)
		)`,
			eventAtArg,
			eventAtArg, recordedAtArg,
			eventAtArg, recordedAtArg, sourceKindArg,
			eventAtArg, recordedAtArg, sourceKindArg, activityIDArg,
		))
	}
	arguments = append(arguments, limit+1)
	whereSQL := conditions[0]
	for _, condition := range conditions[1:] {
		whereSQL += " and " + condition
	}
	rows, err := repository.pool.Query(ctx, `
		select p.activity_id, p.ingest_sequence, p.event_kind, p.event_at, p.recorded_at,
		       p.backfilled, p.severity, p.source_kind, p.source_event_id, p.source_version,
		       p.presentation_json, p.actor_id, p.record_id, p.revision_id,
		       p.evidence_snapshot_id, p.corrects_activity_id, p.canonical_hash
		from public.record_activity_projection p
		where `+whereSQL+`
		order by p.event_at desc, p.recorded_at desc, p.source_kind asc, p.activity_id asc
		limit $`+fmt.Sprintf("%d", len(arguments)),
		arguments...,
	)
	if err != nil {
		return activity.ActivityPage{}, fmt.Errorf("scan export activity page: %w", err)
	}
	defer rows.Close()

	envelopes := make([]activity.ActivityEnvelope, 0, limit)
	for rows.Next() {
		var (
			envelope     activity.ActivityEnvelope
			ingest       int64
			eventKind    string
			severity     string
			sourceKind   string
			sourceEvent  string
			sourceVer    int64
			presentation []byte
			actorID      *string
			recordID     *string
			revisionID   *string
			evidenceID   *string
			corrects     *string
			hash         []byte
		)
		if err := rows.Scan(
			&envelope.ActivityID, &ingest, &eventKind, &envelope.EventAt, &envelope.RecordedAt,
			&envelope.Backfilled, &severity, &sourceKind, &sourceEvent, &sourceVer,
			&presentation, &actorID, &recordID, &revisionID, &evidenceID, &corrects, &hash,
		); err != nil {
			return activity.ActivityPage{}, fmt.Errorf("scan export activity row: %w", err)
		}
		if err := json.Unmarshal(presentation, &envelope.Presentation); err != nil {
			return activity.ActivityPage{}, fmt.Errorf("decode export presentation: %w", err)
		}
		envelope.IngestSequence = uint64(ingest)
		envelope.EventKind = activity.EventKind(eventKind)
		envelope.Severity = severity
		envelope.Source = activity.SourceIdentity{
			Kind: activity.SourceKind(sourceKind), EventID: sourceEvent, Version: uint64(sourceVer),
		}
		envelope.EventAt = envelope.EventAt.UTC()
		envelope.RecordedAt = envelope.RecordedAt.UTC()
		if actorID != nil {
			envelope.Actor = &activity.ActorSnapshot{ActorID: *actorID}
		}
		if recordID != nil {
			envelope.RecordID = *recordID
		}
		if revisionID != nil {
			envelope.RevisionID = *revisionID
		}
		if evidenceID != nil {
			envelope.EvidenceID = *evidenceID
		}
		if corrects != nil {
			envelope.Corrects = *corrects
		}
		if len(hash) == 32 {
			copy(envelope.CanonicalHash[:], hash)
		}
		envelopes = append(envelopes, envelope)
	}
	if err := rows.Err(); err != nil {
		return activity.ActivityPage{}, err
	}

	hasMore := len(envelopes) > limit
	if hasMore {
		envelopes = envelopes[:limit]
	}
	page := activity.ActivityPage{
		Snapshot:  snapshot,
		Envelopes: envelopes,
		HasMore:   hasMore,
	}
	if hasMore && len(envelopes) > 0 {
		next, err := activity.NewPageCursor(
			activity.ExportScope{Actor: actor},
			selection,
			snapshot,
			envelopes[len(envelopes)-1].SortKeyValue(),
		)
		if err != nil {
			return activity.ActivityPage{}, err
		}
		page.NextCursor = next
	}
	return page, nil
}
