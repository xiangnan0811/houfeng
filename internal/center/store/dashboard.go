package store

import (
	"context"
	"encoding/json"
	"errors"
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
	ObjectType        incidents.ObjectType
	ObjectID          string
	Severity          incidents.Severity
	EventType         incidents.EventType
	CreatedFrom       *time.Time
	CreatedTo         *time.Time
	Label             string
	NotificationOnly  bool
	RecoveryOnly      bool
	MaintenanceOnly   bool
	IncludeBackfilled bool
	Limit             int
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
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard counts: %w", err)
	}
	overview.SnapshotGeneratedAt = time.Now().UTC()
	overview.GroupSummaries, err = loadDashboardGroupSummaries(ctx, r.db)
	if err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard group summaries: %w", err)
	}
	overview.NotificationStatus, err = loadDashboardNotificationStatus(ctx, r.db)
	if err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard notification status: %w", err)
	}
	overview.AssetSummary, err = loadDashboardAssetSummary(ctx, r.db)
	if err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard asset summary: %w", err)
	}
	overview.AbnormalNodes, err = loadAbnormalNodeSummaries(ctx, r.db, limit)
	if err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard abnormal nodes: %w", err)
	}
	overview.AbnormalTargets, err = loadAbnormalTargetSummaries(ctx, r.db, limit)
	if err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard abnormal targets: %w", err)
	}
	overview.NewIncidentTrend24h, overview.RecoveryTrend24h, err = loadDashboardTrends24h(ctx, r.db)
	if err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard trends: %w", err)
	}
	events, err := r.ListEvents(ctx, EventsFilter{Limit: limit})
	if err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard recent events: %w", err)
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
			(select count(*)::int from nodes),
			(select count(*)::int from targets),
			(select count(*)::int from nodes where current_health_status <> '正常'),
			(select count(*)::int from targets where current_health_status <> '正常'),
			(select count(*)::int from nodes where current_health_status = '严重'),
			(select count(*)::int from targets where current_health_status = '严重'),
			(select count(*)::int from nodes where monitoring_status = '维护中'),
			(select count(*)::int from targets where run_status = '维护中'),
			(select count(*)::int from nodes where lifecycle_status = '待接入' or binding_status in ('未绑定', '指纹变更待确认')),
			(select count(*)::int from nodes where monitoring_status = '暂停'),
			(select count(*)::int from nodes where lifecycle_status = '已退役'),
			(select count(*)::int from targets where run_status = '暂停'),
			(select count(*)::int from targets where run_status = '已归档'),
			(select count(*)::int from state_change_events where event_type = 'incident_started' and created_at >= now() - interval '24 hours'),
			(select count(*)::int from state_change_events where event_type = 'incident_recovered' and created_at >= now() - interval '24 hours')
	`).Scan(
		&overview.TotalNodeCount,
		&overview.TotalTargetCount,
		&overview.AbnormalNodeCount,
		&overview.AbnormalTargetCount,
		&overview.SevereNodeCount,
		&overview.SevereTargetCount,
		&overview.MaintenanceNodeCount,
		&overview.MaintenanceTargetCount,
		&overview.PendingOnboardingNodeCount,
		&overview.PausedNodeCount,
		&overview.RetiredNodeCount,
		&overview.PausedTargetCount,
		&overview.ArchivedTargetCount,
		&overview.RecentNewIncidentCount,
		&overview.RecentRecoveryCount,
	); err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("query dashboard overview: %w", err)
	}
	return overview, nil
}

func loadDashboardGroupSummaries(ctx context.Context, queryer dashboardQueryer) ([]incidents.DashboardGroupSummary, error) {
	rows, err := queryer.Query(ctx, `
		with node_groups as (
			select
				coalesce(nullif(btrim("group"), ''), '未分组') as group_name,
				count(*)::int as node_count,
				(count(*) filter (where current_health_status <> '正常'))::int as abnormal_node_count,
				(count(*) filter (where current_health_status = '严重'))::int as severe_node_count,
				(count(*) filter (where monitoring_status = '维护中'))::int as maintenance_node_count
			from nodes
			group by 1
		),
		target_groups as (
			select
				coalesce(nullif(btrim("group"), ''), '未分组') as group_name,
				count(*)::int as target_count,
				(count(*) filter (where current_health_status <> '正常'))::int as abnormal_target_count,
				(count(*) filter (where current_health_status = '严重'))::int as severe_target_count,
				(count(*) filter (where run_status = '维护中'))::int as maintenance_target_count
			from targets
			group by 1
		)
		select
			coalesce(ng.group_name, tg.group_name) as group_name,
			coalesce(ng.node_count, 0),
			coalesce(tg.target_count, 0),
			coalesce(ng.abnormal_node_count, 0),
			coalesce(tg.abnormal_target_count, 0),
			coalesce(ng.severe_node_count, 0),
			coalesce(tg.severe_target_count, 0),
			coalesce(ng.maintenance_node_count, 0),
			coalesce(tg.maintenance_target_count, 0)
		from node_groups ng
		full outer join target_groups tg on tg.group_name = ng.group_name
		order by
			(coalesce(ng.abnormal_node_count, 0) + coalesce(tg.abnormal_target_count, 0)) desc,
			(coalesce(ng.severe_node_count, 0) + coalesce(tg.severe_target_count, 0)) desc,
			(coalesce(ng.node_count, 0) + coalesce(tg.target_count, 0)) desc,
			group_name asc`)
	if err != nil {
		return nil, fmt.Errorf("query dashboard group summaries: %w", err)
	}
	defer rows.Close()

	records := make([]incidents.DashboardGroupSummary, 0)
	for rows.Next() {
		var record incidents.DashboardGroupSummary
		if err := rows.Scan(
			&record.Group,
			&record.NodeCount,
			&record.TargetCount,
			&record.AbnormalNodeCount,
			&record.AbnormalTargetCount,
			&record.SevereNodeCount,
			&record.SevereTargetCount,
			&record.MaintenanceNodeCount,
			&record.MaintenanceTargetCount,
		); err != nil {
			return nil, fmt.Errorf("scan dashboard group summary row: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard group summaries: %w", err)
	}
	return records, nil
}

func loadDashboardNotificationStatus(ctx context.Context, queryer dashboardQueryer) (incidents.DashboardNotificationStatus, error) {
	var status incidents.DashboardNotificationStatus
	if err := queryer.QueryRow(ctx, `
		select
			btrim(telegram_bot_token) <> '' and btrim(telegram_chat_id) <> '',
			telegram_runtime_managed,
			telegram_runtime_managed and btrim(telegram_bot_token) <> '' and btrim(telegram_chat_id) <> '',
			feishu_enabled and btrim(feishu_webhook_url) <> ''
		from center_settings
		where settings_id = 'center'
	`).Scan(
		&status.TelegramConfigured,
		&status.TelegramRuntimeManaged,
		&status.TelegramRuntimeApplyActive,
		&status.FeishuConfigured,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return incidents.DashboardNotificationStatus{}, nil
		}
		return incidents.DashboardNotificationStatus{}, fmt.Errorf("query dashboard notification status: %w", err)
	}
	return status, nil
}

func loadDashboardAssetSummary(ctx context.Context, queryer dashboardQueryer) (incidents.DashboardAssetSummary, error) {
	var summary incidents.DashboardAssetSummary
	if err := queryer.QueryRow(ctx, `
		with inventory_vps as (
			select vps_id, lifecycle_status, renewal_decision
			from vps_assets
			where lifecycle_status <> 'archived'
		),
		active_vps as (
			select vps_id, lifecycle_status, renewal_decision
			from inventory_vps
			where lifecycle_status <> 'cancelled'
		),
		active_links as (
			select distinct vps_id, node_id
			from vps_node_links
			where unlinked_at is null
		),
		subscription_rollup as (
			select
				v.vps_id,
				count(*) filter (where s.status = 'active') as active_subscription_count,
				count(*) filter (where s.status in ('expired', 'cancelled', 'paused')) as inactive_subscription_count
			from inventory_vps v
			left join subscriptions s on s.vps_id = v.vps_id
			group by v.vps_id
		),
		cancelled_asset_runtime as (
			select v.vps_id, l.node_id::text as object_id
			from inventory_vps v
			join vps_node_links l on l.vps_id = v.vps_id and l.unlinked_at is null
			join nodes n on n.node_id = l.node_id
			where v.lifecycle_status in ('to_cancel', 'cancelled')
			  and n.lifecycle_status not in ('不续费', '已退役')
			union
			select distinct v.vps_id, t.target_id::text as object_id
			from inventory_vps v
			join (
				select vps_id, target_id from asset_services where target_id is not null
				union all
				select vps_id, target_id from asset_domains where target_id is not null
			) a on a.vps_id = v.vps_id
			join targets t on t.target_id = a.target_id
			where v.lifecycle_status in ('to_cancel', 'cancelled')
			  and t.run_status not in ('已归档', '暂停')
		),
		cancellation_attention as (
			select v.vps_id
			from inventory_vps v
			join subscription_rollup sr on sr.vps_id = v.vps_id
			where
				(sr.inactive_subscription_count > 0 and v.lifecycle_status not in ('to_cancel', 'cancelled'))
				or (sr.active_subscription_count > 0 and v.lifecycle_status in ('to_cancel', 'cancelled'))
				or (v.renewal_decision in ('cancel', 'auto_renew_cancelled') and v.lifecycle_status not in ('to_cancel', 'cancelled'))
			union
			select distinct vps_id from cancelled_asset_runtime
		),
		renewal_due as (
			select distinct s.subscription_id, s.vps_id
			from subscriptions s
			join active_vps v on v.vps_id = s.vps_id
			where s.status = 'active'
				and s.renew_at >= current_date
				and s.renew_at <= current_date + 30
		)
		select
			(select count(*)::int from renewal_due),
			(select count(distinct vps_id)::int from renewal_due),
			(select count(*)::int from active_vps v join vps_assets a on a.vps_id = v.vps_id where a.renewal_decision = 'unreviewed'),
			(select count(*)::int from active_vps v join vps_assets a on a.vps_id = v.vps_id where a.lifecycle_status = 'to_cancel'),
			(select count(*)::int from inventory_vps where lifecycle_status = 'cancelled'),
			(select count(distinct vps_id)::int from cancellation_attention),
			(select count(*)::int from cancelled_asset_runtime),
			(select count(*)::int from active_vps v join vps_assets a on a.vps_id = v.vps_id where a.lifecycle_status = 'to_migrate'),
			(select count(*)::int from active_vps v where not exists (
				select 1 from active_links l where l.vps_id = v.vps_id
			)),
			(select count(distinct l.vps_id)::int
				from active_links l
				join active_vps v on v.vps_id = l.vps_id
				join nodes n on n.node_id = l.node_id
				where n.current_health_status <> '正常')
	`).Scan(
		&summary.RenewalDue30dSubscriptionCount,
		&summary.RenewalDue30dVPSCount,
		&summary.UnreviewedVPSCount,
		&summary.ToCancelVPSCount,
		&summary.CancelledVPSCount,
		&summary.CancellationAttentionVPSCount,
		&summary.RunningCancelledAssetCount,
		&summary.ToMigrateVPSCount,
		&summary.UnlinkedVPSCount,
		&summary.AbnormalLinkedVPSCount,
	); err != nil {
		return incidents.DashboardAssetSummary{}, fmt.Errorf("query dashboard asset summary counts: %w", err)
	}

	costs, err := loadDashboardAssetCostByCurrency(ctx, queryer)
	if err != nil {
		return incidents.DashboardAssetSummary{}, err
	}
	summary.CostByCurrency = costs
	return summary, nil
}

func loadDashboardAssetCostByCurrency(ctx context.Context, queryer dashboardQueryer) ([]incidents.DashboardAssetCostByCurrency, error) {
	rows, err := queryer.Query(ctx, `
		select
			currency,
			round(sum(monthly_price)::numeric, 4)::float8 as monthly_total,
			round((sum(monthly_price) * 12)::numeric, 4)::float8 as yearly_total
		from subscriptions
		where status = 'active'
		group by currency
		order by currency asc`)
	if err != nil {
		return nil, fmt.Errorf("query dashboard asset costs: %w", err)
	}
	defer rows.Close()

	records := make([]incidents.DashboardAssetCostByCurrency, 0)
	for rows.Next() {
		var record incidents.DashboardAssetCostByCurrency
		if err := rows.Scan(
			&record.Currency,
			&record.MonthlyTotal,
			&record.YearlyTotal,
		); err != nil {
			return nil, fmt.Errorf("scan dashboard asset cost row: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard asset costs: %w", err)
	}
	return records, nil
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
	if !filter.IncludeBackfilled {
		conditions = append(conditions, "not "+backfilledEventConditionSQL())
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

func backfilledEventConditionSQL() string {
	return `(
		(e.object_type = 'node' and (
			exists (
				select 1
				from node_heartbeats nh
				where nh.node_id = e.object_id
					and nh.is_backfilled
					and nh.observed_at = e.created_at
			)
			or exists (
				select 1
				from host_samples hs
				where hs.node_id = e.object_id
					and hs.is_backfilled
					and hs.observed_at = e.created_at
			)
		))
		or
		(e.object_type = 'target' and exists (
			select 1
			from probe_observations po
			where po.target_id = e.object_id
				and po.is_backfilled
				and po.observed_at = e.created_at
		))
	)`
}
