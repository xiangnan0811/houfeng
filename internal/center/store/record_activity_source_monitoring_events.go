package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// MonitoringEventActivitySource projects `state_change_events`, the incident and
// runtime state log.
//
// Unlike the record log this table keeps its semantics in a jsonb payload rather
// than in columns, and `incidents.ValidMonitoringEventMetadata` is the closed
// contract its writers already validate against. This adapter reuses that
// contract instead of re-deriving one: a second opinion about which event types
// and state transitions are legal would drift from the writers and either drop
// real events or project impossible ones.
type MonitoringEventActivitySource struct {
	pool      *pgxpool.Pool
	namespace activity.Namespace
}

var _ activity.SourceAdapter = (*MonitoringEventActivitySource)(nil)

func NewMonitoringEventActivitySource(pool *pgxpool.Pool, namespace activity.Namespace) (*MonitoringEventActivitySource, error) {
	if pool == nil {
		return nil, errors.New("new monitoring event activity source: nil pool")
	}
	if namespace.ProjectID == "" {
		return nil, activity.ErrInvalidNamespace
	}
	return &MonitoringEventActivitySource{pool: pool, namespace: namespace}, nil
}

func (source *MonitoringEventActivitySource) Kind() activity.SourceKind {
	return activity.SourceKindMonitoringEvent
}

func (source *MonitoringEventActivitySource) IncrementalHead(ctx context.Context) (activity.SourceHead, error) {
	var databaseNow time.Time
	if err := source.pool.QueryRow(ctx, `select now()`).Scan(&databaseNow); err != nil {
		return activity.SourceHead{}, fmt.Errorf("read monitoring event head: %w", err)
	}
	return activity.NewIncrementalSourceHead(
		activity.SourceKindMonitoringEvent,
		databaseNow,
		activity.DefaultSourceSafetyLag,
	), nil
}

// AuthoritativeHead applies the same reasoning as the record domain source:
// `created_at` defaults to now(), which is a transaction start time, so the
// settled bound is the start of the oldest transaction still running.
func (source *MonitoringEventActivitySource) AuthoritativeHead(
	ctx context.Context,
	_ activity.ExportScope,
) (activity.SourceHead, error) {
	settledThrough, horizon, err := settledTransactionBound(ctx, source.pool)
	if err != nil {
		return activity.SourceHead{}, fmt.Errorf("read monitoring event authoritative head: %w", err)
	}
	return activity.NewSettledSourceHead(activity.SourceKindMonitoringEvent, settledThrough, horizon), nil
}

func (source *MonitoringEventActivitySource) Readiness(
	ctx context.Context,
	_ activity.ExportScope,
	head activity.SourceHead,
) (activity.SourceReadiness, error) {
	if head.Kind != activity.SourceKindMonitoringEvent || !head.SupportsCompletenessClaim() {
		return activity.SourceReadiness{}, fmt.Errorf("%w: monitoring event head carries no transaction horizon", activity.ErrSourceNotReady)
	}
	// An export has to be able to state what this source left out, so the count of
	// pre-contract rows is read here rather than inferred from projection totals.
	var excluded int64
	if err := source.pool.QueryRow(ctx, `
		select count(*)
		from state_change_events event
		where event.object_type in ('monitoring_instance', 'target')
		  and event.created_at <= $1
		  and not (`+monitoringEventMetadataCompleteSQL+`)`,
		head.RecordedThrough,
	).Scan(&excluded); err != nil {
		return activity.SourceReadiness{}, fmt.Errorf("count pre-contract monitoring events: %w", err)
	}
	caughtUp, err := loadActiveSourceCaughtUp(ctx, source.pool, activity.SourceKindMonitoringEvent)
	if err != nil {
		return activity.SourceReadiness{}, err
	}
	return activity.SourceReadiness{
		Kind:         activity.SourceKindMonitoringEvent,
		Head:         head,
		CaughtUp:     caughtUp,
		ExcludedRows: uint64(excluded),
	}, nil
}

type monitoringEventActivityRow struct {
	eventID             string
	objectType          string
	objectID            string
	displayName         string
	eventType           string
	severity            string
	eventAt             time.Time
	recordedAt          time.Time
	backfilled          bool
	provenance          string
	producerVersion     string
	ruleVersion         string
	priorState          string
	resultingState      string
	correctionOfEventID string
}

// monitoringEventMetadataCompleteSQL is true only for rows carrying the whole
// metadata contract with the right jsonb types.
//
// It separates two very different failures. A row missing these keys predates the
// contract; it is permanent history and is filtered out below, because failing on
// it would stall the source forever on a row that will never improve. A row that
// has the keys but violates the contract is a live writer bug, and that one is
// rejected loudly in Go so it cannot be mistaken for old data.
const monitoringEventMetadataCompleteSQL = `
	event.payload ?& array['event_at','is_backfilled','provenance','producer_version','rule_version','prior_state','resulting_state']
	  and jsonb_typeof(event.payload->'event_at') = 'string'
	  and jsonb_typeof(event.payload->'is_backfilled') = 'boolean'
	  and jsonb_typeof(event.payload->'provenance') = 'string'
	  and jsonb_typeof(event.payload->'producer_version') = 'string'
	  and jsonb_typeof(event.payload->'rule_version') = 'string'
	  and jsonb_typeof(event.payload->'prior_state') = 'string'
	  and jsonb_typeof(event.payload->'resulting_state') = 'string'`

// monitoringEventActivityScanSQL reads one page.
//
// Only monitoring instances and targets appear: those are the two object types
// the metadata contract admits, and they are also the only two that are subject
// kinds a timeline can be scoped to. The display name comes from whichever live
// table owns the object, so a projected row stays readable by name.
const monitoringEventActivityScanSQL = `
	select
	  event.event_id,
	  event.object_type,
	  event.object_id,
	  coalesce(instance.display_name, target.name, ''),
	  event.event_type,
	  coalesce(event.severity, ''),
	  case
	    when jsonb_typeof(event.payload->'event_at') = 'string'
	      then (event.payload->>'event_at')::timestamptz
	    else event.created_at
	  end,
	  event.created_at,
	  case
	    when jsonb_typeof(event.payload->'is_backfilled') = 'boolean'
	      then (event.payload->>'is_backfilled')::boolean
	    else false
	  end,
	  coalesce(event.payload->>'provenance', ''),
	  coalesce(event.payload->>'producer_version', ''),
	  coalesce(event.payload->>'rule_version', ''),
	  coalesce(event.payload->>'prior_state', ''),
	  coalesce(event.payload->>'resulting_state', ''),
	  coalesce(event.payload->>'correction_of_event_id', '')
	from state_change_events event
	left join monitoring_instances instance
	  on event.object_type = 'monitoring_instance' and instance.monitoring_instance_id = event.object_id
	left join targets target
	  on event.object_type = 'target' and target.target_id = event.object_id
	where event.object_type in ('monitoring_instance', 'target')
	  and (
	    event.created_at > $1
	    or (
	      event.created_at = $1
	      and ($2 = '' or event.event_id > $2)
	    )
	  )
	  and event.created_at <= $3
	  and (` + monitoringEventMetadataCompleteSQL + `)
	order by event.created_at, event.event_id
	limit $4`

func (source *MonitoringEventActivitySource) ScanAfter(
	ctx context.Context,
	window activity.ScanWindow,
	limit int,
) ([]activity.CandidateEvent, error) {
	if limit <= 0 {
		limit = activity.DefaultPageSize
	}
	rows, err := source.pool.Query(
		ctx,
		monitoringEventActivityScanSQL,
		windowLowerBound(window),
		activityKeysetAfter(window),
		window.Through.UTC(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("scan monitoring events: %w", err)
	}
	defer rows.Close()

	candidates := make([]activity.CandidateEvent, 0, limit)
	for rows.Next() {
		var row monitoringEventActivityRow
		if err := rows.Scan(
			&row.eventID, &row.objectType, &row.objectID, &row.displayName,
			&row.eventType, &row.severity, &row.eventAt, &row.recordedAt, &row.backfilled,
			&row.provenance, &row.producerVersion, &row.ruleVersion,
			&row.priorState, &row.resultingState, &row.correctionOfEventID,
		); err != nil {
			return nil, fmt.Errorf("scan monitoring event row: %w", err)
		}
		candidate, err := buildMonitoringEventCandidate(source.namespace, row)
		if err != nil {
			return nil, fmt.Errorf("normalize monitoring event %s: %w", row.eventID, err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan monitoring events: %w", err)
	}
	return candidates, nil
}

// monitoringEventTitles labels every admitted event type. A projected row has to
// say what happened without carrying the writer's free-text summary, which is
// composed from live host and target detail.
var monitoringEventTitles = map[incidents.EventType]string{
	incidents.EventIncidentStarted:                                "故障开始",
	incidents.EventIncidentEscalated:                              "故障升级",
	incidents.EventIncidentRecovered:                              "故障恢复",
	incidents.EventMonitoringInstanceBindingRebindConfirmed:       "重新绑定已确认",
	incidents.EventMonitoringInstanceBindingPendingRejected:       "待确认绑定已拒绝",
	incidents.EventMonitoringInstanceBindingReset:                 "绑定已重置",
	incidents.EventMonitoringInstanceMonitoringMaintenanceEntered: "进入维护",
	incidents.EventMonitoringInstanceMonitoringMaintenanceExited:  "退出维护",
	incidents.EventMonitoringInstanceMonitoringPaused:             "监控已暂停",
	incidents.EventMonitoringInstanceMonitoringResumed:            "监控已恢复",
	incidents.EventMonitoringInstanceLifecycleUpdated:             "生命周期已更新",
	incidents.EventMonitoringInstanceRetired:                      "实例已退役",
	incidents.EventMonitoringInstanceRestoredToObserving:          "已恢复为观察中",
	incidents.EventTargetMaintenanceEntered:                       "目标进入维护",
	incidents.EventTargetMaintenanceExited:                        "目标退出维护",
	incidents.EventTargetPaused:                                   "目标已暂停",
	incidents.EventTargetResumed:                                  "目标已恢复",
	incidents.EventTargetArchived:                                 "目标已归档",
	incidents.EventTargetRestoredToPaused:                         "目标已恢复为暂停",
	incidents.EventCorrected:                                      "事件已更正",
}

// MonitoringEventActivityTypes is the closed set this source can project.
func MonitoringEventActivityTypes() []incidents.EventType {
	types := make([]incidents.EventType, 0, len(monitoringEventTitles))
	for eventType := range monitoringEventTitles {
		types = append(types, eventType)
	}
	return types
}

// monitoringEventSeverities maps the operator-facing severity scale onto the
// projection's. They are separate scales on purpose: the projection stores one
// vocabulary for every source, and monitoring is the only source that speaks the
// incident scale.
var monitoringEventSeverities = map[incidents.Severity]string{
	incidents.SeverityNormal:   "info",
	incidents.SeverityNotice:   "notice",
	incidents.SeverityAlert:    "warning",
	incidents.SeverityCritical: "critical",
}

func monitoringEventSubjectKind(objectType string) (records.SubjectKind, bool) {
	switch incidents.ObjectType(objectType) {
	case incidents.ObjectTypeMonitoringInstance:
		return records.SubjectKindMonitoringInstance, true
	case incidents.ObjectTypeTarget:
		return records.SubjectKindTarget, true
	default:
		return "", false
	}
}

func buildMonitoringEventCandidate(
	namespace activity.Namespace,
	row monitoringEventActivityRow,
) (activity.CandidateEvent, error) {
	// Rows that predate the metadata contract are filtered out by the scan, so
	// anything arriving here claims to satisfy the contract. A claim that fails is
	// a live writer bug and must not be projected or quietly skipped.
	if !incidents.ValidMonitoringEventMetadata(
		incidents.ObjectType(row.objectType),
		incidents.EventType(row.eventType),
		incidents.Severity(row.severity),
		row.backfilled,
		row.provenance,
		row.producerVersion,
		row.ruleVersion,
		row.priorState,
		row.resultingState,
		row.correctionOfEventID,
	) {
		return activity.CandidateEvent{}, fmt.Errorf("%w: %s/%s violates the monitoring event contract", activity.ErrInvalidEventKind, row.objectType, row.eventType)
	}
	title, known := monitoringEventTitles[incidents.EventType(row.eventType)]
	if !known {
		return activity.CandidateEvent{}, fmt.Errorf("%w: %q", activity.ErrInvalidEventKind, row.eventType)
	}
	subjectKind, projectable := monitoringEventSubjectKind(row.objectType)
	if !projectable {
		return activity.CandidateEvent{}, fmt.Errorf("%w: object type %q is not a subject", activity.ErrUnreachableCandidate, row.objectType)
	}
	if !records.ValidSubjectSourceID(subjectKind, row.objectID) {
		return activity.CandidateEvent{}, fmt.Errorf("%w: %s %q", activity.ErrUnreachableCandidate, subjectKind, row.objectID)
	}

	// Every monitoring event projects as one kind, so its version is fixed at 1;
	// the table has no version column and the event id is already unique.
	source := activity.SourceIdentity{
		Kind:    activity.SourceKindMonitoringEvent,
		EventID: row.eventID,
		Version: 1,
	}
	activityID, err := activity.NewActivityID(namespace, source, activity.EventKindMonitoringStateChanged)
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	resolved, err := activity.ResolveEventTime(activity.EventTimeInput{
		Kind:          activity.EventKindMonitoringStateChanged,
		OccurredAt:    row.eventAt,
		SavedAt:       row.recordedAt,
		Authoritative: true,
		SourceIsLate:  row.backfilled,
	})
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	severity := "info"
	if row.severity != "" {
		mapped, known := monitoringEventSeverities[incidents.Severity(row.severity)]
		if !known {
			return activity.CandidateEvent{}, fmt.Errorf("%w: severity %q", activity.ErrInvalidEventKind, row.severity)
		}
		severity = mapped
	}

	identity := map[string]string{}
	if row.displayName != "" {
		identity["display_name"] = row.displayName
	}

	candidate := activity.CandidateEvent{
		ActivityID: activityID,
		Source:     source,
		EventKind:  activity.EventKindMonitoringStateChanged,
		EventAt:    resolved.EventAt,
		RecordedAt: resolved.RecordedAt,
		Backfilled: resolved.Backfilled,
		Subjects: []activity.SubjectSnapshot{{
			Kind:     subjectKind,
			SourceID: row.objectID,
			Role:     records.RelationRoleAffected,
			Primary:  true,
			Identity: identity,
		}},
		Presentation: activity.Presentation{
			Version: activity.PresentationVersionV1,
			Title:   title,
		},
		Severity: severity,
	}

	// A correction points at the projected identity of the event it corrects, not
	// at the raw source id. Because every monitoring event projects under one kind
	// and version, that identity is derivable without reading the corrected row.
	if row.correctionOfEventID != "" {
		correctedID, err := activity.NewActivityID(
			namespace,
			activity.SourceIdentity{
				Kind:    activity.SourceKindMonitoringEvent,
				EventID: row.correctionOfEventID,
				Version: 1,
			},
			activity.EventKindMonitoringStateChanged,
		)
		if err != nil {
			return activity.CandidateEvent{}, fmt.Errorf("derive corrected activity id: %w", err)
		}
		if correctedID == activityID {
			return activity.CandidateEvent{}, fmt.Errorf("%w: event corrects itself", activity.ErrInvalidSourceIdentity)
		}
		candidate.Corrects = correctedID
	}

	authScope, err := activity.ProjectAuthScope(recordauth.ProjectIDDefault)
	if err != nil {
		return activity.CandidateEvent{}, err
	}
	candidate.AuthScope = authScope

	candidate.CanonicalHash = candidate.ComputeCanonicalHash()
	return candidate, nil
}

// settledTransactionBound is shared by the sources whose recorded time is a
// transaction start time. It returns the recorded time below which nothing can
// still appear, plus the transaction horizon that proves it.
//
// It fails closed when the role cannot see every session's transaction start
// time, because the bound is meaningless without them and a completeness claim
// built on a partial view would be wrong rather than merely conservative.
func settledTransactionBound(ctx context.Context, pool *pgxpool.Pool) (time.Time, uint64, error) {
	var (
		oldestTransactionStart *time.Time
		databaseNow            time.Time
		horizon                uint64
		visibleSessions        int64
		totalSessions          int64
	)
	if err := pool.QueryRow(ctx, `
		select
		  min(session.xact_start) filter (where session.xact_start is not null),
		  now(),
		  pg_snapshot_xmin(pg_current_snapshot())::text::bigint,
		  count(*) filter (where session.xact_start is not null or session.state = 'idle'),
		  count(*)
		from pg_stat_activity session
		where session.datname = current_database()`,
	).Scan(&oldestTransactionStart, &databaseNow, &horizon, &visibleSessions, &totalSessions); err != nil {
		return time.Time{}, 0, err
	}
	if totalSessions == 0 || visibleSessions < totalSessions {
		return time.Time{}, 0, fmt.Errorf(
			"%w: a settled head needs transaction start times for all %d sessions, only %d are readable",
			activity.ErrSourceNotReady, totalSessions, visibleSessions,
		)
	}
	settledThrough := databaseNow
	if oldestTransactionStart != nil && oldestTransactionStart.Before(settledThrough) {
		settledThrough = *oldestTransactionStart
	}
	return settledThrough, horizon, nil
}
