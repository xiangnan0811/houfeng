package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// EvidenceActivitySource projects `evidence_snapshots`, the captured-observation
// log.
//
// The subject comes from the snapshot's own captured source and identity, never
// from the record it hangs on. A snapshot's record is mutable — its subjects can
// change after the capture — so resolving through it would let today's record
// membership rewrite what an old capture was about.
type EvidenceActivitySource struct {
	pool      *pgxpool.Pool
	namespace activity.Namespace
}

var _ activity.SourceAdapter = (*EvidenceActivitySource)(nil)

func NewEvidenceActivitySource(pool *pgxpool.Pool, namespace activity.Namespace) (*EvidenceActivitySource, error) {
	if pool == nil {
		return nil, errors.New("new evidence activity source: nil pool")
	}
	if namespace.ProjectID == "" {
		return nil, activity.ErrInvalidNamespace
	}
	return &EvidenceActivitySource{pool: pool, namespace: namespace}, nil
}

func (source *EvidenceActivitySource) Kind() activity.SourceKind {
	return activity.SourceKindEvidenceSnapshot
}

func (source *EvidenceActivitySource) IncrementalHead(ctx context.Context) (activity.SourceHead, error) {
	var databaseNow time.Time
	if err := source.pool.QueryRow(ctx, `select now()`).Scan(&databaseNow); err != nil {
		return activity.SourceHead{}, fmt.Errorf("read evidence head: %w", err)
	}
	return activity.NewIncrementalSourceHead(
		activity.SourceKindEvidenceSnapshot,
		databaseNow,
		activity.DefaultSourceSafetyLag,
	), nil
}

func (source *EvidenceActivitySource) AuthoritativeHead(
	ctx context.Context,
	_ activity.ExportScope,
) (activity.SourceHead, error) {
	settledThrough, horizon, err := settledTransactionBound(ctx, source.pool)
	if err != nil {
		return activity.SourceHead{}, fmt.Errorf("read evidence authoritative head: %w", err)
	}
	return activity.NewSettledSourceHead(activity.SourceKindEvidenceSnapshot, settledThrough, horizon), nil
}

func (source *EvidenceActivitySource) Readiness(
	ctx context.Context,
	_ activity.ExportScope,
	head activity.SourceHead,
) (activity.SourceReadiness, error) {
	if head.Kind != activity.SourceKindEvidenceSnapshot || !head.SupportsCompletenessClaim() {
		return activity.SourceReadiness{}, fmt.Errorf("%w: evidence head carries no transaction horizon", activity.ErrSourceNotReady)
	}
	// The table's source_kind allows four values that are not subjects. Nothing
	// writes them today, but an export must state the count rather than assume it
	// stays zero.
	var excluded int64
	if err := source.pool.QueryRow(ctx, `
		select count(*)
		from public.evidence_snapshots snapshot
		where snapshot.created_at <= $1
		  and snapshot.source_kind not in ('vps', 'monitoring_instance', 'target')`,
		head.RecordedThrough,
	).Scan(&excluded); err != nil {
		return activity.SourceReadiness{}, fmt.Errorf("count non-subject evidence: %w", err)
	}
	return activity.SourceReadiness{
		Kind:         activity.SourceKindEvidenceSnapshot,
		Head:         head,
		CaughtUp:     true,
		ExcludedRows: uint64(excluded),
	}, nil
}

type evidenceActivityRow struct {
	snapshotID     string
	recordID       string
	kind           string
	schemaVersion  int64
	sourceKind     string
	sourceID       string
	displayName    string
	actualEndedAt  time.Time
	createdAt      time.Time
	referencedAt   time.Time
	sensitivityLvl string
}

// evidenceActivityScanSQL reads one page.
//
// Only the three subject source kinds are read. The others in the column's check
// constraint are schema headroom that no writer produces — `recordauth` admits
// only these three — and a row with a non-subject source would have no timeline
// to appear on.
//
// The display name comes out of the snapshot's own captured identity rather than
// a join, which is the whole point of capturing it: a renamed or deleted subject
// still reads correctly.
const evidenceActivityScanSQL = `
	select
	  snapshot.snapshot_id,
	  snapshot.record_id,
	  snapshot.kind,
	  snapshot.schema_version,
	  snapshot.source_kind,
	  snapshot.source_id,
	  coalesce(snapshot.subject_identity_snapshot->>'display_name', ''),
	  snapshot.actual_ended_at,
	  snapshot.created_at,
	  snapshot.referenced_at,
	  snapshot.sensitivity_level
	from public.evidence_snapshots snapshot
	where snapshot.created_at >= $1
	  and snapshot.created_at <= $2
	  and snapshot.source_kind in ('vps', 'monitoring_instance', 'target')
	order by snapshot.created_at, snapshot.snapshot_id
	limit $3`

func (source *EvidenceActivitySource) ScanAfter(
	ctx context.Context,
	window activity.ScanWindow,
	limit int,
) ([]activity.CandidateEvent, error) {
	if limit <= 0 {
		limit = activity.DefaultPageSize
	}
	rows, err := source.pool.Query(
		ctx,
		evidenceActivityScanSQL,
		windowLowerBound(window),
		window.Through.UTC(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("scan evidence snapshots: %w", err)
	}
	defer rows.Close()

	candidates := make([]activity.CandidateEvent, 0, limit)
	for rows.Next() {
		var row evidenceActivityRow
		if err := rows.Scan(
			&row.snapshotID, &row.recordID, &row.kind, &row.schemaVersion,
			&row.sourceKind, &row.sourceID, &row.displayName,
			&row.actualEndedAt, &row.createdAt, &row.referencedAt, &row.sensitivityLvl,
		); err != nil {
			return nil, fmt.Errorf("scan evidence row: %w", err)
		}
		candidate, err := buildEvidenceCandidate(source.namespace, row)
		if err != nil {
			return nil, fmt.Errorf("normalize evidence snapshot %s: %w", row.snapshotID, err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan evidence snapshots: %w", err)
	}
	return candidates, nil
}

func evidenceSubjectKind(sourceKind string) (records.SubjectKind, bool) {
	switch recordauth.SourceKind(sourceKind) {
	case recordauth.SourceKindVPS:
		return records.SubjectKindVPS, true
	case recordauth.SourceKindMonitoringInstance:
		return records.SubjectKindMonitoringInstance, true
	case recordauth.SourceKindTarget:
		return records.SubjectKindTarget, true
	default:
		return "", false
	}
}

func buildEvidenceCandidate(
	namespace activity.Namespace,
	row evidenceActivityRow,
) (activity.CandidateEvent, error) {
	subjectKind, projectable := evidenceSubjectKind(row.sourceKind)
	if !projectable {
		return activity.CandidateEvent{}, fmt.Errorf("%w: evidence source kind %q is not a subject", activity.ErrUnreachableCandidate, row.sourceKind)
	}
	if !records.ValidSubjectSourceID(subjectKind, row.sourceID) {
		return activity.CandidateEvent{}, fmt.Errorf("%w: %s %q", activity.ErrUnreachableCandidate, subjectKind, row.sourceID)
	}
	if row.schemaVersion <= 0 {
		return activity.CandidateEvent{}, fmt.Errorf("%w: evidence schema version %d", activity.ErrInvalidSourceIdentity, row.schemaVersion)
	}

	// The schema version is the source version: a re-capture under a new schema is
	// a different fact about the same window, not an edit of the old one.
	sourceIdentity := activity.SourceIdentity{
		Kind:    activity.SourceKindEvidenceSnapshot,
		EventID: row.snapshotID,
		Version: uint64(row.schemaVersion),
	}
	activityID, err := activity.NewActivityID(namespace, sourceIdentity, activity.EventKindEvidenceCaptured)
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	// Evidence is timed by the end of the window it observed, not by when it was
	// written: a capture of last week's data belongs last week.
	resolved, err := activity.ResolveEventTime(activity.EventTimeInput{
		Kind:           activity.EventKindEvidenceCaptured,
		ObservationEnd: row.actualEndedAt,
		SavedAt:        row.createdAt,
		Authoritative:  true,
	})
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	identity := map[string]string{}
	if row.displayName != "" {
		identity["display_name"] = row.displayName
	}

	candidate := activity.CandidateEvent{
		ActivityID: activityID,
		Source:     sourceIdentity,
		EventKind:  activity.EventKindEvidenceCaptured,
		EventAt:    resolved.EventAt,
		RecordedAt: resolved.RecordedAt,
		Backfilled: resolved.Backfilled,
		Subjects: []activity.SubjectSnapshot{{
			Kind:     subjectKind,
			SourceID: row.sourceID,
			Role:     records.RelationRoleContext,
			Primary:  true,
			Identity: identity,
		}},
		Presentation: activity.Presentation{
			Version: activity.PresentationVersionV1,
			Title:   evidenceActivityTitle,
			// The evidence kind is a registered machine identifier, not captured
			// content, so it names what was measured without copying any of it.
			Summary: row.kind,
		},
		Severity: "info",
	}

	candidate.CanonicalHash = candidate.ComputeCanonicalHash()
	return candidate, nil
}

// evidenceActivityTitle is fixed. What a snapshot contains is its payload, and
// the payload is exactly what must not reach a timeline.
const evidenceActivityTitle = "证据已捕获"
