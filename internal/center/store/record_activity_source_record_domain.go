package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
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
	settledThrough, horizon, err := settledTransactionBound(ctx, source.pool)
	if err != nil {
		return activity.SourceHead{}, fmt.Errorf("read record domain authoritative head: %w", err)
	}
	return activity.NewSettledSourceHead(activity.SourceKindRecordDomain, settledThrough, horizon), nil
}

// Readiness reports this source as export-ready only when the head it is given
// can carry a completeness claim on its own, and the active projector checkpoint
// says the source has caught up to that head.
func (source *RecordDomainActivitySource) Readiness(
	ctx context.Context,
	_ activity.ExportScope,
	head activity.SourceHead,
) (activity.SourceReadiness, error) {
	if head.Kind != activity.SourceKindRecordDomain || !head.SupportsCompletenessClaim() {
		return activity.SourceReadiness{}, fmt.Errorf("%w: record domain head carries no transaction horizon", activity.ErrSourceNotReady)
	}
	caughtUp, err := loadActiveSourceCaughtUp(ctx, source.pool, activity.SourceKindRecordDomain)
	if err != nil {
		return activity.SourceReadiness{}, err
	}
	return activity.SourceReadiness{
		Kind:     activity.SourceKindRecordDomain,
		Head:     head,
		CaughtUp: caughtUp,
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
	// namedRevision distinguishes an event that carries its own revision from one
	// whose revision the lateral join resolved. Only the former can have created
	// that revision; a resolved one merely happened while it was current.
	namedRevision bool
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
		  coalesce(named.revision_no, effective.revision_no, 0),
		  domain_activity.revision_id is not null
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
		  and (
		    domain_activity.recorded_at > $2
		    or (
		      domain_activity.recorded_at = $2
		      and ($3 = '' or domain_activity.activity_id > $3)
		    )
		  )
		  and domain_activity.recorded_at <= $4
		order by domain_activity.recorded_at, domain_activity.activity_id
		limit $5`,
		recordplatform.ProjectIDDefault,
		windowLowerBound(window),
		activityKeysetAfter(window),
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
			&row.namedRevision,
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
	authScopes, err := source.loadRevisionAuthScopes(ctx, revisionIDs)
	if err != nil {
		return nil, err
	}

	candidates := make([]activity.CandidateEvent, 0, len(scanned))
	for _, row := range scanned {
		if row.revisionID == "" {
			return nil, fmt.Errorf(
				"%w: record domain activity %s has no revision for authorization",
				activity.ErrUnreachableCandidate, row.activityID,
			)
		}
		authScope, ok := authScopes[row.revisionID]
		if !ok {
			return nil, fmt.Errorf(
				"%w: record domain activity %s revision %q has no visibility",
				activity.ErrUnreachableCandidate, row.activityID, row.revisionID,
			)
		}
		candidate, err := buildRecordDomainCandidate(source.namespace, row, subjects[row.revisionID], authScope)
		if err != nil {
			return nil, fmt.Errorf("normalize record domain activity %s: %w", row.activityID, err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func (source *RecordDomainActivitySource) loadRevisionAuthScopes(
	ctx context.Context,
	revisionIDs []string,
) (map[string]recordauth.ResourceScope, error) {
	byRevision := make(map[string]recordauth.ResourceScope, len(revisionIDs))
	if len(revisionIDs) == 0 {
		return byRevision, nil
	}
	rows, err := source.pool.Query(ctx, `
		select revision_id, visibility_scope, visibility_digest
		from public.record_revisions
		where revision_id = any($1::text[])`,
		revisionIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("load record revision visibility: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			revisionID string
			raw        []byte
			digest     []byte
		)
		if err := rows.Scan(&revisionID, &raw, &digest); err != nil {
			return nil, fmt.Errorf("scan record revision visibility: %w", err)
		}
		visibility, err := decodeStoredRecordVisibility(raw, digest)
		if err != nil {
			return nil, fmt.Errorf("decode revision %s visibility: %w", revisionID, err)
		}
		byRevision[revisionID] = activity.AuthScopeFromVisibility(visibility)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("load record revision visibility: %w", err)
	}
	return byRevision, nil
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

// recordDomainRevisionCommitKinds are the kinds whose writer commits a new
// revision. The split is the writer's own, not a guess: `RevisionCommitCommand`
// accepts exactly these three, while archive and unarchive go through the
// lifecycle path and name the revision that was already current.
var recordDomainRevisionCommitKinds = map[activity.EventKind]bool{
	activity.EventKind(records.DomainActivityRecordCreated):  true,
	activity.EventKind(records.DomainActivityRecordRevised):  true,
	activity.EventKind(records.DomainActivityRecordRestored): true,
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
	authScope recordauth.ResourceScope,
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
	if authScope.Visibility.Kind == "" || authScope.Visibility.CanonicalHash == ([32]byte{}) {
		return activity.CandidateEvent{}, fmt.Errorf(
			"%w: missing authoritative visibility for %s",
			activity.ErrUnreachableCandidate, row.activityID,
		)
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
		RevisionNo: uint64(row.revisionNo),
		AuthScope:  authScope,
		// Only an event that carries its own revision and comes from the commit
		// path created that revision. Everything else — comments, actions, archive
		// — names a revision it merely happened alongside.
		OpensRevision: row.namedRevision && recordDomainRevisionCommitKinds[eventKind],
	}
	if candidate.OpensRevision && (candidate.RevisionID == "" || candidate.RevisionNo == 0) {
		// A commit whose revision row cannot be found means the join answered for a
		// revision that is not there, and publication would build a validity
		// interval on it.
		return activity.CandidateEvent{}, fmt.Errorf(
			"%w: %s commits revision %q with no revision number",
			activity.ErrInvalidSourceIdentity, row.activityID, row.revisionID,
		)
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
