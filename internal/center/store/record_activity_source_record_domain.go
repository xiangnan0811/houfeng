package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// RecordDomainActivitySource projects `record_domain_activities`, the append-only
// log every record, comment and action mutation already writes.
//
// Two properties of that table shape this adapter. Its `recorded_at` defaults to
// now(), which in PostgreSQL is the transaction start time, so a slow transaction
// can insert a row whose recorded time is well below a watermark that has already
// moved past it; the projector's trailing re-read is what catches those. And its
// subjects live on the revision rather than on the event, so an event carries the
// subject set of the revision it belongs to.
type RecordDomainActivitySource struct {
	pool      *pgxpool.Pool
	namespace activity.Namespace
}

var _ activity.SourceAdapter = (*RecordDomainActivitySource)(nil)

func NewRecordDomainActivitySource(pool *pgxpool.Pool, namespace activity.Namespace) (*RecordDomainActivitySource, error) {
	if pool == nil {
		return nil, errors.New("new record domain activity source: nil pool")
	}
	if namespace.ProjectID == "" {
		return nil, activity.ErrInvalidNamespace
	}
	return &RecordDomainActivitySource{pool: pool, namespace: namespace}, nil
}

func (source *RecordDomainActivitySource) Kind() activity.SourceKind {
	return activity.SourceKindRecordDomain
}

// IncrementalHead reads the clock from the database rather than from this
// process. `recorded_at` is written by PostgreSQL, so comparing it against a Go
// clock would fold any skew between the two machines into the safety lag.
func (source *RecordDomainActivitySource) IncrementalHead(ctx context.Context) (activity.SourceHead, error) {
	var databaseNow time.Time
	if err := source.pool.QueryRow(ctx, `select now()`).Scan(&databaseNow); err != nil {
		return activity.SourceHead{}, fmt.Errorf("read record domain head: %w", err)
	}
	return activity.NewIncrementalSourceHead(
		activity.SourceKindRecordDomain,
		databaseNow,
		activity.DefaultSourceSafetyLag,
	), nil
}

// AuthoritativeHead answers the stronger question an export asks: is there a
// recorded time below which nothing can still appear? Because `recorded_at` is a
// transaction start time, that bound is the start of the oldest transaction still
// running. Anything earlier belongs to a transaction that has already finished.
//
// A role that cannot see other sessions' transaction start times cannot compute
// the bound, and this fails closed rather than returning the incremental head
// dressed up as proof: a wrong completeness claim is worse than no export.
func (source *RecordDomainActivitySource) AuthoritativeHead(
	ctx context.Context,
	_ activity.ExportScope,
) (activity.SourceHead, error) {
	var (
		oldestTransactionStart *time.Time
		databaseNow            time.Time
		horizon                uint64
		visibleSessions        int64
		totalSessions          int64
	)
	if err := source.pool.QueryRow(ctx, `
		select
		  min(activity.xact_start) filter (where activity.xact_start is not null),
		  now(),
		  pg_snapshot_xmin(pg_current_snapshot())::text::bigint,
		  count(*) filter (where activity.xact_start is not null or activity.state = 'idle'),
		  count(*)
		from pg_stat_activity activity
		where activity.datname = current_database()`,
	).Scan(&oldestTransactionStart, &databaseNow, &horizon, &visibleSessions, &totalSessions); err != nil {
		return activity.SourceHead{}, fmt.Errorf("read record domain authoritative head: %w", err)
	}
	// pg_stat_activity always lists rows, but blanks the columns this bound needs
	// for sessions the role may not inspect. A blanked row is indistinguishable
	// from an idle one here, so an unreadable session set means no proof.
	if totalSessions == 0 || visibleSessions < totalSessions {
		return activity.SourceHead{}, fmt.Errorf(
			"%w: record domain settled head needs transaction start times for all %d sessions, only %d are readable",
			activity.ErrSourceNotReady, totalSessions, visibleSessions,
		)
	}

	settledThrough := databaseNow
	if oldestTransactionStart != nil && oldestTransactionStart.Before(settledThrough) {
		settledThrough = *oldestTransactionStart
	}
	return activity.NewSettledSourceHead(activity.SourceKindRecordDomain, settledThrough, horizon), nil
}

// Readiness reports this source as export-ready only when the head it is given
// can carry a completeness claim on its own.
func (source *RecordDomainActivitySource) Readiness(
	_ context.Context,
	_ activity.ExportScope,
	head activity.SourceHead,
) (activity.SourceReadiness, error) {
	if head.Kind != activity.SourceKindRecordDomain || !head.SupportsCompletenessClaim() {
		return activity.SourceReadiness{}, fmt.Errorf("%w: record domain head carries no transaction horizon", activity.ErrSourceNotReady)
	}
	return activity.SourceReadiness{
		Kind:     activity.SourceKindRecordDomain,
		Head:     head,
		CaughtUp: true,
	}, nil
}

type recordDomainActivityRow struct {
	activityID    string
	recordID      string
	revisionID    string
	eventKind     string
	sourceVersion int64
	actorID       string
	eventAt       time.Time
	recordedAt    time.Time
	revisionNo    int64
}

// ScanAfter reads one page and normalizes it. The lateral join resolves the
// revision an event belongs to when the event itself does not name one, which is
// the case for action mutations: without it those events would arrive with no
// subject and be unreachable from any timeline.
func (source *RecordDomainActivitySource) ScanAfter(
	ctx context.Context,
	window activity.ScanWindow,
	limit int,
) ([]activity.CandidateEvent, error) {
	if limit <= 0 {
		limit = activity.DefaultPageSize
	}
	rows, err := source.pool.Query(ctx, `
		select
		  domain_activity.activity_id,
		  domain_activity.record_id,
		  coalesce(domain_activity.revision_id, effective.revision_id, ''),
		  domain_activity.event_kind,
		  domain_activity.source_version,
		  domain_activity.actor_id,
		  domain_activity.event_at,
		  domain_activity.recorded_at,
		  coalesce(named.revision_no, effective.revision_no, 0)
		from public.record_domain_activities domain_activity
		left join public.record_revisions named
		  on named.revision_id = domain_activity.revision_id
		left join lateral (
		  select candidate.revision_id, candidate.revision_no
		  from public.record_revisions candidate
		  where candidate.record_id = domain_activity.record_id
		    and candidate.created_at <= domain_activity.event_at
		  order by candidate.revision_no desc
		  limit 1
		) effective on domain_activity.revision_id is null
		where domain_activity.project_id = $1
		  and domain_activity.recorded_at >= $2
		  and domain_activity.recorded_at <= $3
		order by domain_activity.recorded_at, domain_activity.activity_id
		limit $4`,
		recordplatform.ProjectIDDefault,
		windowLowerBound(window),
		window.Through.UTC(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("scan record domain activities: %w", err)
	}
	defer rows.Close()

	scanned := make([]recordDomainActivityRow, 0, limit)
	revisionIDs := make([]string, 0, limit)
	for rows.Next() {
		var row recordDomainActivityRow
		if err := rows.Scan(
			&row.activityID, &row.recordID, &row.revisionID, &row.eventKind,
			&row.sourceVersion, &row.actorID, &row.eventAt, &row.recordedAt, &row.revisionNo,
		); err != nil {
			return nil, fmt.Errorf("scan record domain activity row: %w", err)
		}
		scanned = append(scanned, row)
		if row.revisionID != "" {
			revisionIDs = append(revisionIDs, row.revisionID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan record domain activities: %w", err)
	}
	if len(scanned) == 0 {
		return nil, nil
	}

	subjects, err := source.loadRevisionSubjects(ctx, revisionIDs)
	if err != nil {
		return nil, err
	}

	candidates := make([]activity.CandidateEvent, 0, len(scanned))
	for _, row := range scanned {
		candidate, err := buildRecordDomainCandidate(source.namespace, row, subjects[row.revisionID])
		if err != nil {
			return nil, fmt.Errorf("normalize record domain activity %s: %w", row.activityID, err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// windowLowerBound keeps a zero From meaning "all history" rather than the zero
// instant, which PostgreSQL would happily compare against but which reads as a
// year-1 timestamp in a query plan.
func windowLowerBound(window activity.ScanWindow) time.Time {
	if window.From.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return window.From.UTC()
}

func (source *RecordDomainActivitySource) loadRevisionSubjects(
	ctx context.Context,
	revisionIDs []string,
) (map[string][]activity.SubjectSnapshot, error) {
	byRevision := make(map[string][]activity.SubjectSnapshot, len(revisionIDs))
	if len(revisionIDs) == 0 {
		return byRevision, nil
	}
	rows, err := source.pool.Query(ctx, `
		select revision_id, subject_kind, source_id, relation_role, is_primary, identity_snapshot
		from public.record_revision_subjects
		where revision_id = any($1::text[])
		order by revision_id, ordinal`,
		revisionIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("load record revision subjects: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			revisionID string
			subject    activity.SubjectSnapshot
			identity   []byte
		)
		if err := rows.Scan(
			&revisionID, &subject.Kind, &subject.SourceID, &subject.Role, &subject.Primary, &identity,
		); err != nil {
			return nil, fmt.Errorf("scan record revision subject: %w", err)
		}
		if len(identity) > 0 {
			if err := json.Unmarshal(identity, &subject.Identity); err != nil {
				return nil, fmt.Errorf("decode identity snapshot for %s: %w", revisionID, err)
			}
		}
		byRevision[revisionID] = append(byRevision[revisionID], subject)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load record revision subjects: %w", err)
	}
	return byRevision, nil
}

// recordDomainEventTitles gives every projected event a fixed label. The title is
// deliberately not derived from the record: a timeline row must not carry record
// body text, comment text or command output, and a per-kind label is the most a
// projection needs to be readable.
var recordDomainEventTitles = map[activity.EventKind]string{
	activity.EventKindRecordCreated:            "记录已创建",
	activity.EventKindRecordRevised:            "记录已修订",
	activity.EventKindRecordRestored:           "记录已恢复",
	activity.EventKindRecordArchived:           "记录已归档",
	activity.EventKindRecordUnarchived:         "记录已取消归档",
	activity.EventKindRecordOwnerChanged:       "负责人已变更",
	activity.EventKindRecordParticipantChanged: "参与人已变更",
	activity.EventKindRecordFollowUpChanged:    "跟进状态已变更",
	activity.EventKindCommentCreated:           "新增评论",
	activity.EventKindCommentEdited:            "评论已编辑",
	activity.EventKindCommentRedacted:          "评论已撤回",
	activity.EventKindActionCreated:            "新增行动项",
	activity.EventKindActionUpdated:            "行动项已更新",
	activity.EventKindActionCompleted:          "行动项已完成",
	activity.EventKindActionCancelled:          "行动项已取消",
	activity.EventKindActionReopened:           "行动项已重开",
}

// RecordDomainEventKinds is the closed set this source can emit. It exists so a
// test can prove the projected vocabulary matches what the record writers
// actually write, instead of drifting into kinds nothing produces.
func RecordDomainEventKinds() []activity.EventKind {
	kinds := make([]activity.EventKind, 0, len(recordDomainEventTitles))
	for kind := range recordDomainEventTitles {
		kinds = append(kinds, kind)
	}
	return kinds
}

func buildRecordDomainCandidate(
	namespace activity.Namespace,
	row recordDomainActivityRow,
	subjects []activity.SubjectSnapshot,
) (activity.CandidateEvent, error) {
	eventKind := activity.EventKind(row.eventKind)
	title, known := recordDomainEventTitles[eventKind]
	if !known {
		// A kind nothing here recognizes must stop the batch. Projecting it with a
		// placeholder label would put an unexplained row on an operator's timeline.
		return activity.CandidateEvent{}, fmt.Errorf("%w: %q", activity.ErrInvalidEventKind, row.eventKind)
	}
	if row.sourceVersion <= 0 {
		return activity.CandidateEvent{}, activity.ErrInvalidSourceIdentity
	}

	// The event's own primary key is the source coordinate. `source_event_id` is
	// the upstream id it was derived from and is not shaped consistently across
	// the five writers, so it is not a coordinate this projection can key on.
	source := activity.SourceIdentity{
		Kind:    activity.SourceKindRecordDomain,
		EventID: row.activityID,
		Version: uint64(row.sourceVersion),
	}
	activityID, err := activity.NewActivityID(namespace, source, eventKind)
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	resolved, err := activity.ResolveEventTime(activity.EventTimeInput{
		Kind:       eventKind,
		RevisionNo: uint64(row.revisionNo),
		OccurredAt: row.eventAt,
		SavedAt:    row.recordedAt,
	})
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	candidate := activity.CandidateEvent{
		ActivityID: activityID,
		Source:     source,
		EventKind:  eventKind,
		EventAt:    resolved.EventAt,
		RecordedAt: resolved.RecordedAt,
		Backfilled: resolved.Backfilled,
		Actor:      &activity.ActorSnapshot{ActorID: row.actorID},
		Subjects:   subjects,
		Presentation: activity.Presentation{
			Version: activity.PresentationVersionV1,
			Title:   title,
		},
		Severity:   "info",
		RecordID:   row.recordID,
		RevisionID: row.revisionID,
	}
	if len(candidate.Subjects) == 0 {
		return activity.CandidateEvent{}, fmt.Errorf(
			"%w: record %s revision %q has no subject",
			activity.ErrUnreachableCandidate, row.recordID, row.revisionID,
		)
	}
	for _, subject := range candidate.Subjects {
		if !records.ValidSubjectKind(subject.Kind) || !records.ValidRelationRole(subject.Role) {
			return activity.CandidateEvent{}, fmt.Errorf("%w: subject %s/%s", activity.ErrUnreachableCandidate, subject.Kind, subject.Role)
		}
	}
	candidate.CanonicalHash = candidate.ComputeCanonicalHash()
	return candidate, nil
}
