package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/incidents"
)

type EventListItem struct {
	EventID       string                  `json:"event_id"`
	IncidentID    string                  `json:"incident_id"`
	IncidentClass incidents.IncidentClass `json:"incident_class"`
	ObjectType    incidents.ObjectType    `json:"object_type"`
	ObjectID      string                  `json:"object_id"`
	EventType     incidents.EventType     `json:"event_type"`
	Severity      incidents.Severity      `json:"severity"`
	Summary       string                  `json:"summary"`
	CreatedAt     time.Time               `json:"created_at"`
}

type EventsFilter struct {
	ObjectType incidents.ObjectType
	ObjectID   string
	Severity   incidents.Severity
	EventType  incidents.EventType
	Limit      int
}

type dashboardQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresDashboardRepository struct {
	db dashboardQueryer
}

func NewPostgresDashboardRepository(db *pgxpool.Pool) *PostgresDashboardRepository {
	return &PostgresDashboardRepository{db: db}
}

func (r *PostgresDashboardRepository) GetDashboardOverview(ctx context.Context, limit int) (incidents.DashboardOverview, error) {
	if limit <= 0 {
		limit = 10
	}

	overview, err := loadDashboardCounts(ctx, r.db)
	if err != nil {
		return incidents.DashboardOverview{}, err
	}
	events, err := r.ListEvents(ctx, EventsFilter{Limit: limit})
	if err != nil {
		return incidents.DashboardOverview{}, err
	}
	overview.RecentEvents = make([]incidents.StateChangeEventRecord, 0, len(events))
	for _, event := range events {
		overview.RecentEvents = append(overview.RecentEvents, incidents.StateChangeEventRecord{
			IncidentID:    event.IncidentID,
			IncidentClass: event.IncidentClass,
			ObjectType:    event.ObjectType,
			ObjectID:      event.ObjectID,
			EventType:     event.EventType,
			Severity:      event.Severity,
			Summary:       event.Summary,
			CreatedAt:     event.CreatedAt,
		})
	}
	return overview, nil
}

func loadDashboardCounts(ctx context.Context, queryer dashboardQueryer) (incidents.DashboardOverview, error) {
	var overview incidents.DashboardOverview
	if err := queryer.QueryRow(ctx, `
		select
			(select count(*) from nodes),
			(select count(*) from targets),
			(select count(*) from nodes where current_health_status <> '正常'),
			(select count(*) from targets where current_health_status <> '正常'),
			(select count(*) from nodes where current_health_status = '严重'),
			(select count(*) from targets where current_health_status = '严重'),
			(select count(*) from nodes where monitoring_status = '维护中'),
			(select count(*) from targets where run_status = '维护中'),
			(select count(*) from state_change_events where event_type = 'incident_started' and created_at >= now() - interval '24 hours'),
			(select count(*) from state_change_events where event_type = 'incident_recovered' and created_at >= now() - interval '24 hours')
	`).Scan(
		&overview.TotalNodeCount,
		&overview.TotalTargetCount,
		&overview.AbnormalNodeCount,
		&overview.AbnormalTargetCount,
		&overview.SevereNodeCount,
		&overview.SevereTargetCount,
		&overview.MaintenanceNodeCount,
		&overview.MaintenanceTargetCount,
		&overview.RecentNewIncidentCount,
		&overview.RecentRecoveryCount,
	); err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("query dashboard overview: %w", err)
	}
	return overview, nil
}

func (r *PostgresDashboardRepository) ListEvents(ctx context.Context, filter EventsFilter) ([]EventListItem, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	args := []any{}
	conditions := []string{}
	if filter.ObjectType != "" {
		args = append(args, string(filter.ObjectType))
		conditions = append(conditions, fmt.Sprintf("object_type = $%d", len(args)))
	}
	if filter.ObjectID != "" {
		args = append(args, filter.ObjectID)
		conditions = append(conditions, fmt.Sprintf("object_id = $%d", len(args)))
	}
	if filter.Severity != "" {
		args = append(args, string(filter.Severity))
		conditions = append(conditions, fmt.Sprintf("severity = $%d", len(args)))
	}
	if filter.EventType != "" {
		args = append(args, string(filter.EventType))
		conditions = append(conditions, fmt.Sprintf("event_type = $%d", len(args)))
	}
	args = append(args, limit)
	limitArg := len(args)

	query := `
		select event_id, object_type, object_id, event_type, coalesce(severity, ''), summary, payload, created_at
		from state_change_events`
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += fmt.Sprintf(" order by created_at desc limit $%d", limitArg)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events list: %w", err)
	}
	defer rows.Close()

	events := make([]EventListItem, 0)
	for rows.Next() {
		var (
			event    EventListItem
			payload  []byte
			severity string
		)
		if err := rows.Scan(
			&event.EventID,
			&event.ObjectType,
			&event.ObjectID,
			&event.EventType,
			&severity,
			&event.Summary,
			&payload,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan events list row: %w", err)
		}
		event.Severity = incidents.Severity(severity)
		if len(payload) > 0 {
			var decoded struct {
				IncidentID    string `json:"incident_id"`
				IncidentClass string `json:"incident_class"`
			}
			if err := json.Unmarshal(payload, &decoded); err != nil {
				return nil, fmt.Errorf("decode event payload: %w", err)
			}
			event.IncidentID = decoded.IncidentID
			event.IncidentClass = incidents.IncidentClass(decoded.IncidentClass)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events list: %w", err)
	}
	return events, nil
}
