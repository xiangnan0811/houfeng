package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/commandaudits"
)

type commandAuditQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresCommandAuditRepository struct {
	db commandAuditQueryer
}

func NewPostgresCommandAuditRepository(db *pgxpool.Pool) *PostgresCommandAuditRepository {
	return newPostgresCommandAuditRepository(db)
}

func newPostgresCommandAuditRepository(db commandAuditQueryer) *PostgresCommandAuditRepository {
	return &PostgresCommandAuditRepository{db: db}
}

var _ commandaudits.Repository = (*PostgresCommandAuditRepository)(nil)

const listCommandAuditActionsSQL = `
with ranked_starters as (
	select
		a.*,
		case when a.event_type = 'rejected' then a.audit_id else a.action_id end as id,
		row_number() over (
			partition by case when a.event_type = 'rejected' then a.audit_id else a.action_id end
			order by a.occurred_at asc, a.audit_id asc
		) as starter_rank
	from monitoring_instance_command_action_audit a
	where a.event_type in ('queued', 'rejected')
		and ($1::timestamptz is null or a.occurred_at >= $1)
		and a.occurred_at <= $2
), starters as (
	select *
	from ranked_starters
	where starter_rank = 1
), classified as (
	select
		s.id,
		s.action_id,
		s.monitoring_instance_id,
		s.monitoring_instance_name_snapshot,
		s.command_id,
		s.sensitivity,
		s.actor_user_id,
		s.actor_username_snapshot,
		s.actor_display_name_snapshot,
		s.occurred_at as started_at,
		case
			when s.event_type = 'rejected' then 'rejected'
			when action_state.has_completed and action_state.completed_exit_code = 0 then 'succeeded'
			when action_state.has_completed then 'failed'
			when action_state.has_dispatched then 'dispatched'
			else 'queued'
		end as outcome
	from starters s
	left join lateral (
		select
			coalesce(bool_or(e.event_type = 'completed'), false) as has_completed,
			coalesce(bool_or(e.event_type = 'dispatched'), false) as has_dispatched,
			(array_agg(e.exit_code order by e.occurred_at desc, e.audit_id desc)
				filter (where e.event_type = 'completed'))[1] as completed_exit_code
		from monitoring_instance_command_action_audit e
		where s.action_id is not null
			and e.action_id = s.action_id
			and e.occurred_at <= $2
	) action_state on true
)
select
	c.id,
	c.action_id,
	c.monitoring_instance_id,
	c.monitoring_instance_name_snapshot,
	(mi.monitoring_instance_id is null) as monitoring_instance_deleted,
	c.command_id,
	c.sensitivity,
	c.outcome,
	c.actor_user_id,
	c.actor_username_snapshot,
	c.actor_display_name_snapshot,
	c.started_at
from classified c
left join monitoring_instances mi on mi.monitoring_instance_id = c.monitoring_instance_id
where (
		$3 = '' or
		c.monitoring_instance_id ilike $3 escape '\' or
		c.monitoring_instance_name_snapshot ilike $3 escape '\'
	)
	and ($4 = '' or c.command_id = $4)
	and ($5 = '' or c.sensitivity = $5)
	and ($6 = '' or c.outcome = $6)
	and (
		$7 = '' or
		coalesce(c.actor_user_id, '') ilike $7 escape '\' or
		c.actor_username_snapshot ilike $7 escape '\' or
		c.actor_display_name_snapshot ilike $7 escape '\'
	)
	and ($8 = '' or c.action_id = $8)
	and ($9::timestamptz is null or (c.started_at, c.id) < ($9::timestamptz, $10::text))
order by started_at desc, id desc
limit $11`

const listCommandAuditEventsSQL = `
select
	case when event_type = 'rejected' then audit_id else action_id end as group_id,
	audit_id,
	event_type,
	source,
	occurred_at,
	exit_code,
	case
		when event_type = 'rejected'
			and details->>'reason' = 'sensitive_confirmation_required'
		then 'sensitive_confirmation_required'
		else ''
	end as rejection_reason
from monitoring_instance_command_action_audit
where occurred_at <= $3
	and event_type in ('queued', 'dispatched', 'completed', 'rejected')
	and (
		action_id = any($1::text[]) or
		(event_type = 'rejected' and audit_id = any($2::text[]))
	)
order by group_id asc, occurred_at asc, audit_id asc`

func (r *PostgresCommandAuditRepository) ListCommandAudits(ctx context.Context, query commandaudits.Query) (commandaudits.Page, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := r.db.Query(ctx, listCommandAuditActionsSQL,
		commandAuditTimeArg(query.StartedFrom),
		query.StartedTo.UTC(),
		commandAuditLiteralPattern(query.MonitoringInstance),
		query.CommandID,
		query.Sensitivity,
		query.Outcome,
		commandAuditLiteralPattern(query.Actor),
		query.ActionID,
		commandAuditTimeArg(query.BeforeStartedAt),
		query.BeforeID,
		limit+1,
	)
	if err != nil {
		return commandaudits.Page{}, fmt.Errorf("query command audit actions: %w", err)
	}

	items := make([]commandaudits.Action, 0, limit+1)
	for rows.Next() {
		var (
			action           commandaudits.Action
			actionID         *string
			actorUserID      *string
			instanceName     string
			actorUsername    string
			actorDisplayName string
		)
		if err := rows.Scan(
			&action.ID,
			&actionID,
			&action.MonitoringInstance.ID,
			&instanceName,
			&action.MonitoringInstance.Deleted,
			&action.CommandID,
			&action.Sensitivity,
			&action.Outcome,
			&actorUserID,
			&actorUsername,
			&actorDisplayName,
			&action.StartedAt,
		); err != nil {
			rows.Close()
			return commandaudits.Page{}, fmt.Errorf("scan command audit action: %w", err)
		}
		if actionID != nil {
			action.ActionID = *actionID
		}
		action.MonitoringInstance.Name = commandAuditFallback(instanceName, action.MonitoringInstance.ID)
		action.Actor = commandAuditActor(actorUserID, actorUsername, actorDisplayName)
		action.Events = make([]commandaudits.Event, 0)
		items = append(items, action)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return commandaudits.Page{}, fmt.Errorf("iterate command audit actions: %w", err)
	}
	rows.Close()

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	actionIDs := make([]string, 0, len(items))
	rejectedAuditIDs := make([]string, 0, len(items))
	itemIndex := make(map[string]int, len(items))
	for i := range items {
		itemIndex[items[i].ID] = i
		if items[i].ActionID == "" {
			rejectedAuditIDs = append(rejectedAuditIDs, items[i].ID)
		} else {
			actionIDs = append(actionIDs, items[i].ActionID)
		}
	}

	eventRows, err := r.db.Query(ctx, listCommandAuditEventsSQL, actionIDs, rejectedAuditIDs, query.StartedTo.UTC())
	if err != nil {
		return commandaudits.Page{}, fmt.Errorf("query command audit events: %w", err)
	}
	for eventRows.Next() {
		var (
			groupID string
			event   commandaudits.Event
			reason  string
		)
		if err := eventRows.Scan(
			&groupID,
			&event.AuditID,
			&event.EventType,
			&event.Source,
			&event.OccurredAt,
			&event.ExitCode,
			&reason,
		); err != nil {
			eventRows.Close()
			return commandaudits.Page{}, fmt.Errorf("scan command audit event: %w", err)
		}
		if event.EventType == "rejected" && reason == commandaudits.RejectionReasonSensitiveConfirmationRequired {
			event.RejectionReason = reason
		}
		if index, ok := itemIndex[groupID]; ok {
			items[index].Events = append(items[index].Events, event)
		}
	}
	if err := eventRows.Err(); err != nil {
		eventRows.Close()
		return commandaudits.Page{}, fmt.Errorf("iterate command audit events: %w", err)
	}
	eventRows.Close()

	return commandaudits.Page{Items: items, HasMore: hasMore}, nil
}

func commandAuditTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func commandAuditLiteralPattern(value string) string {
	if value == "" {
		return ""
	}
	escaped := strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	).Replace(value)
	return "%" + escaped + "%"
}

func commandAuditFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func commandAuditActor(userID *string, username, displayName string) *commandaudits.ActorIdentity {
	if userID == nil || strings.TrimSpace(*userID) == "" {
		return nil
	}
	id := *userID
	username = commandAuditFallback(username, id)
	displayName = commandAuditFallback(displayName, username)
	return &commandaudits.ActorIdentity{
		UserID:      id,
		Username:    username,
		DisplayName: displayName,
	}
}
