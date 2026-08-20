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
	"houfeng/internal/contracts/agentapi"
)

// CommandAuditActivitySource projects the diagnostic command audit trail.
//
// The audit records three phases per action — queued, dispatched, completed —
// and each is its own row with its own time, so each projects as its own event.
// Collapsing them to one would lose the record of a command that was dispatched
// and never came back, which is exactly the case an operator is looking for.
//
// Command output never enters the projection. stdout and stderr are not in this
// table at all, and the `details` payload is deliberately never read: a timeline
// is metadata about what was run, not a copy of what it printed.
type CommandAuditActivitySource struct {
	pool      *pgxpool.Pool
	namespace activity.Namespace
}

var _ activity.SourceAdapter = (*CommandAuditActivitySource)(nil)

func NewCommandAuditActivitySource(pool *pgxpool.Pool, namespace activity.Namespace) (*CommandAuditActivitySource, error) {
	if pool == nil {
		return nil, errors.New("new command audit activity source: nil pool")
	}
	if namespace.ProjectID == "" {
		return nil, activity.ErrInvalidNamespace
	}
	return &CommandAuditActivitySource{pool: pool, namespace: namespace}, nil
}

func (source *CommandAuditActivitySource) Kind() activity.SourceKind {
	return activity.SourceKindCommandAudit
}

func (source *CommandAuditActivitySource) IncrementalHead(ctx context.Context) (activity.SourceHead, error) {
	var databaseNow time.Time
	if err := source.pool.QueryRow(ctx, `select now()`).Scan(&databaseNow); err != nil {
		return activity.SourceHead{}, fmt.Errorf("read command audit head: %w", err)
	}
	return activity.NewIncrementalSourceHead(
		activity.SourceKindCommandAudit,
		databaseNow,
		activity.DefaultSourceSafetyLag,
	), nil
}

func (source *CommandAuditActivitySource) AuthoritativeHead(
	ctx context.Context,
	_ activity.ExportScope,
) (activity.SourceHead, error) {
	settledThrough, horizon, err := settledTransactionBound(ctx, source.pool)
	if err != nil {
		return activity.SourceHead{}, fmt.Errorf("read command audit authoritative head: %w", err)
	}
	return activity.NewSettledSourceHead(activity.SourceKindCommandAudit, settledThrough, horizon), nil
}

func (source *CommandAuditActivitySource) Readiness(
	ctx context.Context,
	_ activity.ExportScope,
	head activity.SourceHead,
) (activity.SourceReadiness, error) {
	if head.Kind != activity.SourceKindCommandAudit || !head.SupportsCompletenessClaim() {
		return activity.SourceReadiness{}, fmt.Errorf("%w: command audit head carries no transaction horizon", activity.ErrSourceNotReady)
	}
	caughtUp, err := loadActiveSourceCaughtUp(ctx, source.pool, activity.SourceKindCommandAudit)
	if err != nil {
		return activity.SourceReadiness{}, err
	}
	return activity.SourceReadiness{
		Kind:     activity.SourceKindCommandAudit,
		Head:     head,
		CaughtUp: caughtUp,
	}, nil
}

type commandAuditActivityRow struct {
	auditID              string
	monitoringInstanceID string
	instanceNameSnapshot string
	liveInstanceName     string
	commandID            string
	sensitivity          string
	eventType            string
	actorUserID          string
	actorDisplayName     string
	exitCode             *int32
	occurredAt           time.Time
}

// commandAuditActivityScanSQL reads one page.
//
// `occurred_at` is both the occurrence and the recorded time: the audit row is
// written in the same transaction as the moment it describes, so there is no
// separate save time to distinguish.
//
// The live instance name is joined only as a fallback. The snapshot column is
// authoritative because it is what the instance was called when the command ran,
// and a later rename must not silently rewrite history.
const commandAuditActivityScanSQL = `
	select
	  audit.audit_id,
	  audit.monitoring_instance_id,
	  audit.monitoring_instance_name_snapshot,
	  coalesce(instance.display_name, ''),
	  audit.command_id,
	  audit.sensitivity,
	  audit.event_type,
	  coalesce(audit.actor_user_id, ''),
	  coalesce(nullif(audit.actor_display_name_snapshot, ''), audit.actor_username_snapshot),
	  audit.exit_code,
	  audit.occurred_at
	from monitoring_instance_command_action_audit audit
	left join monitoring_instances instance
	  on instance.monitoring_instance_id = audit.monitoring_instance_id
	where (
	    audit.occurred_at > $1
	    or (
	      audit.occurred_at = $1
	      and ($2 = '' or audit.audit_id > $2)
	    )
	  )
	  and audit.occurred_at <= $3
	order by audit.occurred_at, audit.audit_id
	limit $4`

func (source *CommandAuditActivitySource) ScanAfter(
	ctx context.Context,
	window activity.ScanWindow,
	limit int,
) ([]activity.CandidateEvent, error) {
	if limit <= 0 {
		limit = activity.DefaultPageSize
	}
	rows, err := source.pool.Query(
		ctx,
		commandAuditActivityScanSQL,
		windowLowerBound(window),
		activityKeysetAfter(window),
		window.Through.UTC(),
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("scan command audits: %w", err)
	}
	defer rows.Close()

	candidates := make([]activity.CandidateEvent, 0, limit)
	for rows.Next() {
		var row commandAuditActivityRow
		if err := rows.Scan(
			&row.auditID, &row.monitoringInstanceID, &row.instanceNameSnapshot,
			&row.liveInstanceName, &row.commandID, &row.sensitivity, &row.eventType,
			&row.actorUserID, &row.actorDisplayName, &row.exitCode, &row.occurredAt,
		); err != nil {
			return nil, fmt.Errorf("scan command audit row: %w", err)
		}
		candidate, err := buildCommandAuditCandidate(source.namespace, row)
		if err != nil {
			return nil, fmt.Errorf("normalize command audit %s: %w", row.auditID, err)
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan command audits: %w", err)
	}
	return candidates, nil
}

// commandAuditPhaseTitles labels the three phases. A phase the schema does not
// allow has no label, so a new one cannot reach a timeline unlabelled.
var commandAuditPhaseTitles = map[string]string{
	"queued":     "命令已排队",
	"dispatched": "命令已下发",
	"completed":  "命令已完成",
}

func buildCommandAuditCandidate(
	namespace activity.Namespace,
	row commandAuditActivityRow,
) (activity.CandidateEvent, error) {
	title, known := commandAuditPhaseTitles[row.eventType]
	if !known {
		return activity.CandidateEvent{}, fmt.Errorf("%w: command audit phase %q", activity.ErrInvalidEventKind, row.eventType)
	}
	// The command id comes from a compiled-in catalog shared with the agent, so it
	// is safe to show. Checking it here keeps an id that no longer exists in the
	// catalog from being presented as though it were still a real command.
	if !agentapi.IsKnownCommandID(row.commandID) {
		return activity.CandidateEvent{}, fmt.Errorf("%w: command %q is not in the catalog", activity.ErrInvalidEventKind, row.commandID)
	}
	if !records.ValidSubjectSourceID(records.SubjectKindMonitoringInstance, row.monitoringInstanceID) {
		return activity.CandidateEvent{}, fmt.Errorf("%w: monitoring instance %q", activity.ErrUnreachableCandidate, row.monitoringInstanceID)
	}

	// The audit id is the row's primary key, and its event type is immutable, so
	// the id alone is a stable coordinate for exactly this phase of this action.
	sourceIdentity := activity.SourceIdentity{
		Kind:    activity.SourceKindCommandAudit,
		EventID: row.auditID,
		Version: 1,
	}
	activityID, err := activity.NewActivityID(namespace, sourceIdentity, activity.EventKindCommandExecuted)
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	resolved, err := activity.ResolveEventTime(activity.EventTimeInput{
		Kind:          activity.EventKindCommandExecuted,
		OccurredAt:    row.occurredAt,
		SavedAt:       row.occurredAt,
		Authoritative: true,
	})
	if err != nil {
		return activity.CandidateEvent{}, err
	}

	displayName := row.instanceNameSnapshot
	if displayName == "" {
		displayName = row.liveInstanceName
	}
	identity := map[string]string{}
	if displayName != "" {
		identity["display_name"] = displayName
	}

	candidate := activity.CandidateEvent{
		ActivityID: activityID,
		Source:     sourceIdentity,
		EventKind:  activity.EventKindCommandExecuted,
		EventAt:    resolved.EventAt,
		RecordedAt: resolved.RecordedAt,
		Subjects: []activity.SubjectSnapshot{{
			Kind:     records.SubjectKindMonitoringInstance,
			SourceID: row.monitoringInstanceID,
			Role:     records.RelationRoleAffected,
			Primary:  true,
			Identity: identity,
		}},
		Presentation: activity.Presentation{
			Version: activity.PresentationVersionV1,
			Title:   title,
			Summary: row.commandID,
		},
		Severity: commandAuditSeverity(row),
	}
	// An agent-initiated phase has no user behind it, and leaving the actor unset
	// says so rather than attributing the command to nobody-in-particular.
	if row.actorUserID != "" {
		candidate.Actor = &activity.ActorSnapshot{
			ActorID:     row.actorUserID,
			DisplayName: row.actorDisplayName,
		}
	}

	authScope, err := activity.ProjectAuthScope(recordauth.ProjectIDDefault)
	if err != nil {
		return activity.CandidateEvent{}, err
	}
	candidate.AuthScope = authScope

	candidate.CanonicalHash = candidate.ComputeCanonicalHash()
	return candidate, nil
}

// commandAuditSeverity raises a completed command that exited non-zero.
//
// The exit code is metadata, not output, and a failed diagnostic is the row an
// operator scanning a timeline is looking for. Only the completed phase can
// carry one: a queued or dispatched command has not exited yet, so a code there
// would be meaningless.
func commandAuditSeverity(row commandAuditActivityRow) string {
	if row.eventType != "completed" || row.exitCode == nil || *row.exitCode == 0 {
		return "info"
	}
	return "warning"
}
