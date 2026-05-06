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
	ObjectType       incidents.ObjectType
	ObjectID         string
	Severity         incidents.Severity
	EventType        incidents.EventType
	CreatedFrom      *time.Time
	CreatedTo        *time.Time
	Label            string
	NotificationOnly bool
	RecoveryOnly     bool
	MaintenanceOnly  bool
	Limit            int
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
	overview.AbnormalNodes, err = loadAbnormalNodeSummaries(ctx, r.db, limit)
	if err != nil {
		return incidents.DashboardOverview{}, err
	}
	overview.AbnormalTargets, err = loadAbnormalTargetSummaries(ctx, r.db, limit)
	if err != nil {
		return incidents.DashboardOverview{}, err
	}
	overview.NewIncidentTrend24h, overview.RecoveryTrend24h, err = loadDashboardTrends24h(ctx, r.db)
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

func loadAbnormalNodeSummaries(ctx context.Context, queryer dashboardQueryer, limit int) ([]incidents.DashboardNodeSummary, error) {
	rows, err := queryer.Query(ctx, `
		select
			node_id,
			display_name,
			"group",
			region,
			city,
			provider,
			lifecycle_status,
			monitoring_status,
			current_health_status,
			last_heartbeat_at,
			current_active_incident_count,
			current_primary_issue_summary
		from nodes
		where current_health_status <> '正常'
		order by case current_health_status
			when '严重' then 3
			when '告警' then 2
			when '关注' then 1
			else 0
		end desc,
		current_active_incident_count desc,
		updated_at desc,
		node_id asc
		limit $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query dashboard abnormal nodes: %w", err)
	}
	defer rows.Close()

	records := make([]incidents.DashboardNodeSummary, 0)
	for rows.Next() {
		var record incidents.DashboardNodeSummary
		if err := rows.Scan(
			&record.NodeID,
			&record.DisplayName,
			&record.Group,
			&record.Region,
			&record.City,
			&record.Provider,
			&record.LifecycleStatus,
			&record.MonitoringStatus,
			&record.CurrentHealthStatus,
			&record.LastHeartbeatAt,
			&record.CurrentActiveIncidentCount,
			&record.CurrentPrimaryIssueSummary,
		); err != nil {
			return nil, fmt.Errorf("scan dashboard abnormal node row: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard abnormal nodes: %w", err)
	}
	return records, nil
}

func loadAbnormalTargetSummaries(ctx context.Context, queryer dashboardQueryer, limit int) ([]incidents.DashboardTargetSummary, error) {
	rows, err := queryer.Query(ctx, `
		select
			target_id,
			name,
			target_type,
			host,
			base_port,
			run_status,
			"group",
			current_health_status,
			last_success_at,
			last_failure_at,
			current_active_incident_count,
			current_primary_issue_summary
		from targets
		where current_health_status <> '正常'
		order by case current_health_status
			when '严重' then 3
			when '告警' then 2
			when '关注' then 1
			else 0
		end desc,
		current_active_incident_count desc,
		updated_at desc,
		target_id asc
		limit $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query dashboard abnormal targets: %w", err)
	}
	defer rows.Close()

	records := make([]incidents.DashboardTargetSummary, 0)
	for rows.Next() {
		var record incidents.DashboardTargetSummary
		if err := rows.Scan(
			&record.TargetID,
			&record.Name,
			&record.TargetType,
			&record.Host,
			&record.BasePort,
			&record.RunStatus,
			&record.Group,
			&record.CurrentHealthStatus,
			&record.LastSuccessAt,
			&record.LastFailureAt,
			&record.CurrentActiveIncidentCount,
			&record.CurrentPrimaryIssueSummary,
		); err != nil {
			return nil, fmt.Errorf("scan dashboard abnormal target row: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard abnormal targets: %w", err)
	}
	return records, nil
}

// loadDashboardTrends24h returns two 24-element arrays of per-hour event
// counts: incident_started and incident_recovered. Index 0 is 23 hours ago
// (oldest bucket), index 23 is the current hour. Used by the dashboard
// sparkline. Empty buckets are zero-filled.
func loadDashboardTrends24h(ctx context.Context, queryer dashboardQueryer) ([]int, []int, error) {
	rows, err := queryer.Query(ctx, `
		with hour_buckets as (
			select generate_series(
				date_trunc('hour', now() - interval '23 hours'),
				date_trunc('hour', now()),
				interval '1 hour'
			) as bucket_start
		)
		select
			coalesce((
				select count(*)::int
				from state_change_events e
				where e.event_type = 'incident_started'
					and date_trunc('hour', e.created_at) = hb.bucket_start
			), 0),
			coalesce((
				select count(*)::int
				from state_change_events e
				where e.event_type = 'incident_recovered'
					and date_trunc('hour', e.created_at) = hb.bucket_start
			), 0)
		from hour_buckets hb
		order by hb.bucket_start asc`)
	if err != nil {
		return nil, nil, fmt.Errorf("query dashboard trends 24h: %w", err)
	}
	defer rows.Close()

	started := make([]int, 0, 24)
	recovered := make([]int, 0, 24)
	for rows.Next() {
		var s, r int
		if err := rows.Scan(&s, &r); err != nil {
			return nil, nil, fmt.Errorf("scan dashboard trend row: %w", err)
		}
		started = append(started, s)
		recovered = append(recovered, r)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate dashboard trends 24h: %w", err)
	}
	return started, recovered, nil
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
		conditions = append(conditions, fmt.Sprintf("e.object_type = $%d", len(args)))
	}
	if filter.ObjectID != "" {
		args = append(args, filter.ObjectID)
		conditions = append(conditions, fmt.Sprintf("e.object_id = $%d", len(args)))
	}
	if filter.Severity != "" {
		args = append(args, string(filter.Severity))
		conditions = append(conditions, fmt.Sprintf("e.severity = $%d", len(args)))
	}
	if filter.EventType != "" {
		args = append(args, string(filter.EventType))
		conditions = append(conditions, fmt.Sprintf("e.event_type = $%d", len(args)))
	}
	if filter.CreatedFrom != nil {
		args = append(args, *filter.CreatedFrom)
		conditions = append(conditions, fmt.Sprintf("e.created_at >= $%d", len(args)))
	}
	if filter.CreatedTo != nil {
		args = append(args, *filter.CreatedTo)
		conditions = append(conditions, fmt.Sprintf("e.created_at <= $%d", len(args)))
	}
	if filter.Label != "" {
		args = append(args, filter.Label)
		labelArg := len(args)
		conditions = append(conditions, fmt.Sprintf(`(
			(e.object_type = 'node' and exists (
				select 1 from nodes n where n.node_id = e.object_id and n.labels @> array[$%d]::text[]
			))
			or
			(e.object_type = 'target' and exists (
				select 1 from targets t where t.target_id = e.object_id and t.labels @> array[$%d]::text[]
			))
		)`, labelArg, labelArg))
	}
	if filter.NotificationOnly {
		conditions = append(conditions, `exists (
			select 1
			from notification_records nr
			where nr.incident_id = e.payload ->> 'incident_id'
				and nr.object_type = e.object_type
				and nr.object_id = e.object_id
		)`)
	}
	if filter.RecoveryOnly {
		conditions = append(conditions, fmt.Sprintf("e.event_type = '%s'", incidents.EventIncidentRecovered))
	}
	if filter.MaintenanceOnly {
		conditions = append(conditions, fmt.Sprintf("e.event_type in ('%s', '%s', '%s', '%s')",
			incidents.EventNodeMonitoringMaintenanceEntered,
			incidents.EventNodeMonitoringMaintenanceExited,
			incidents.EventTargetMaintenanceEntered,
			incidents.EventTargetMaintenanceExited,
		))
	}
	args = append(args, limit)
	limitArg := len(args)

	query := `
		select e.event_id, e.object_type, e.object_id, e.event_type, coalesce(e.severity, ''), e.summary, e.payload, e.created_at
		from state_change_events e`
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += fmt.Sprintf(" order by e.created_at desc limit $%d", limitArg)

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
