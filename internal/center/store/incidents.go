package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/incidents"
)

type incidentStoreTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresIncidentRepository struct {
	beginTx func(context.Context, pgx.TxOptions) (incidentStoreTx, error)
}

func NewPostgresIncidentRepository(db *pgxpool.Pool) *PostgresIncidentRepository {
	return &PostgresIncidentRepository{
		beginTx: func(ctx context.Context, options pgx.TxOptions) (incidentStoreTx, error) {
			return db.BeginTx(ctx, options)
		},
	}
}

func (r *PostgresIncidentRepository) ApplyIncidentMutation(ctx context.Context, mutation incidents.IncidentMutation) error {
	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin incident mutation tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := replaceActiveIncidents(ctx, tx, mutation); err != nil {
		return err
	}
	if err := insertStateChangeEvents(ctx, tx, mutation.Events); err != nil {
		return err
	}
	if err := insertNotificationRecords(ctx, tx, mutation.Notifications); err != nil {
		return err
	}
	if err := projectObjectSummary(ctx, tx, mutation.ObjectType, mutation.ObjectID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit incident mutation tx: %w", err)
	}
	return nil
}

func (r *PostgresIncidentRepository) AppendNotificationRecords(ctx context.Context, notifications []incidents.NotificationRecordWrite) error {
	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin notification append tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := insertNotificationRecords(ctx, tx, notifications); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit notification append tx: %w", err)
	}
	return nil
}

func replaceActiveIncidents(ctx context.Context, tx incidentStoreTx, mutation incidents.IncidentMutation) error {
	if _, err := tx.Exec(ctx, `
		delete from active_incidents
		where object_type = $1 and object_id = $2`,
		string(mutation.ObjectType),
		mutation.ObjectID,
	); err != nil {
		return fmt.Errorf("delete active incidents for %s %q: %w", mutation.ObjectType, mutation.ObjectID, err)
	}

	for _, incident := range mutation.Active {
		if _, err := tx.Exec(ctx, `
			insert into active_incidents (
				incident_id,
				object_type,
				object_id,
				incident_class,
				severity,
				started_at,
				last_evaluated_at,
				status,
				source_summary
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			incident.IncidentID,
			string(incident.ObjectType),
			incident.ObjectID,
			string(incident.IncidentClass),
			string(incident.Severity),
			incident.StartedAt,
			incident.LastEvaluatedAt,
			incident.Status,
			incident.SourceSummary,
		); err != nil {
			return fmt.Errorf("insert active incident %q: %w", incident.IncidentID, err)
		}
	}
	return nil
}

func insertStateChangeEvents(ctx context.Context, tx incidentStoreTx, events []incidents.StateChangeEventRecord) error {
	for _, event := range events {
		eventID, err := ids.New("evt")
		if err != nil {
			return fmt.Errorf("generate event id: %w", err)
		}
		payload, err := json.Marshal(map[string]string{
			"incident_id":    event.IncidentID,
			"incident_class": string(event.IncidentClass),
		})
		if err != nil {
			return fmt.Errorf("marshal event payload: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into state_change_events (
				event_id,
				object_type,
				object_id,
				event_type,
				severity,
				summary,
				payload,
				created_at
			) values ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`,
			eventID,
			string(event.ObjectType),
			event.ObjectID,
			string(event.EventType),
			string(event.Severity),
			event.Summary,
			payload,
			event.CreatedAt,
		); err != nil {
			return fmt.Errorf("insert state change event %q: %w", eventID, err)
		}
	}
	return nil
}

func insertNotificationRecords(ctx context.Context, tx incidentStoreTx, notifications []incidents.NotificationRecordWrite) error {
	for _, notification := range notifications {
		notificationID, err := ids.New("notif")
		if err != nil {
			return fmt.Errorf("generate notification id: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			insert into notification_records (
				notification_id,
				incident_id,
				object_type,
				object_id,
				channel,
				delivery_status,
				summary,
				sent_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8)`,
			notificationID,
			notification.IncidentID,
			string(notification.ObjectType),
			notification.ObjectID,
			notification.Channel,
			string(notification.DeliveryStatus),
			notification.Summary,
			notification.SentAt,
		); err != nil {
			return fmt.Errorf("insert notification record %q: %w", notificationID, err)
		}
	}
	return nil
}

func projectObjectSummary(ctx context.Context, tx incidentStoreTx, objectType incidents.ObjectType, objectID string) error {
	var (
		count    int
		severity string
		summary  string
	)
	if err := tx.QueryRow(ctx, `
		select
			count(*),
			coalesce((
				select severity
				from active_incidents
				where object_type = $1 and object_id = $2
				order by case severity
					when '严重' then 3
					when '告警' then 2
					when '关注' then 1
					else 0
				end desc, started_at desc
				limit 1
			), '正常'),
			coalesce((
				select source_summary
				from active_incidents
				where object_type = $1 and object_id = $2
				order by case severity
					when '严重' then 3
					when '告警' then 2
					when '关注' then 1
					else 0
				end desc, started_at desc
				limit 1
			), '')
		from active_incidents
		where object_type = $1 and object_id = $2`,
		string(objectType),
		objectID,
	).Scan(&count, &severity, &summary); err != nil {
		return fmt.Errorf("query incident summary for %s %q: %w", objectType, objectID, err)
	}

	switch objectType {
	case incidents.ObjectTypeNode:
		tag, err := tx.Exec(ctx, `
			update nodes
			set current_health_status = $2,
				current_active_incident_count = $3,
				current_primary_issue_summary = $4,
				updated_at = now()
			where node_id = $1`,
			objectID,
			severity,
			count,
			summary,
		)
		if err != nil {
			return fmt.Errorf("update node summary %q: %w", objectID, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("update node summary %q: node not found", objectID)
		}
	case incidents.ObjectTypeTarget:
		tag, err := tx.Exec(ctx, `
			update targets
			set current_health_status = $2,
				current_active_incident_count = $3,
				current_primary_issue_summary = $4,
				updated_at = now()
			where target_id = $1`,
			objectID,
			severity,
			count,
			summary,
		)
		if err != nil {
			return fmt.Errorf("update target summary %q: %w", objectID, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("update target summary %q: target not found", objectID)
		}
	default:
		return fmt.Errorf("unsupported object type %q", objectType)
	}

	return nil
}
