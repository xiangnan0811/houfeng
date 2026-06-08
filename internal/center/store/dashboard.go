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

func dashboardCurrentMonitoringInstanceVisibilitySQL(alias string) string {
	return fmt.Sprintf(`(
		not exists (
			select 1
			from vps_monitoring_instance_links l
			where l.monitoring_instance_id = %s.monitoring_instance_id
			  and l.unlinked_at is null
		)
		or exists (
			select 1
			from vps_monitoring_instance_links l
			join vps_assets v on v.vps_id = l.vps_id
			where l.monitoring_instance_id = %s.monitoring_instance_id
			  and l.unlinked_at is null
			  and v.lifecycle_status not in ('cancelled', 'archived')
		)
	)`, alias, alias)
}

func dashboardCurrentTargetVisibilitySQL(alias string) string {
	return fmt.Sprintf(`(
		not exists (
			select 1
			from (
				select vps_id, target_id from asset_services where target_id is not null
				union all
				select vps_id, target_id from asset_domains where target_id is not null
			) a
			where a.target_id = %s.target_id
		)
		or exists (
			select 1
			from (
				select vps_id, target_id from asset_services where target_id is not null
				union all
				select vps_id, target_id from asset_domains where target_id is not null
			) a
			join vps_assets v on v.vps_id = a.vps_id
			where a.target_id = %s.target_id
			  and v.lifecycle_status not in ('cancelled', 'archived')
		)
	)`, alias, alias)
}

func dashboardCurrentEventVisibilitySQL(alias string) string {
	return fmt.Sprintf(`(
		(%s.object_type = 'monitoring_instance' and exists (
			select 1
			from monitoring_instances mi
			where mi.monitoring_instance_id = %s.object_id
			  and `+dashboardCurrentMonitoringInstanceVisibilitySQL("mi")+`
		))
		or (%s.object_type = 'target' and exists (
			select 1
			from targets t
			where t.target_id = %s.object_id
			  and `+dashboardCurrentTargetVisibilitySQL("t")+`
		))
		or %s.object_type not in ('monitoring_instance', 'target')
	)`, alias, alias, alias, alias, alias)
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
	overview.AbnormalMonitoringInstances, err = loadAbnormalMonitoringInstanceSummaries(ctx, r.db, limit)
	if err != nil {
		return incidents.DashboardOverview{}, fmt.Errorf("load dashboard abnormal monitoring instances: %w", err)
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

func loadAbnormalMonitoringInstanceSummaries(ctx context.Context, queryer dashboardQueryer, limit int) ([]incidents.DashboardMonitoringInstanceSummary, error) {
	rows, err := queryer.Query(ctx, `
		select
			mi.monitoring_instance_id,
			mi.display_name,
			mi."group",
			mi.region,
			mi.city,
			mi.provider,
			mi.lifecycle_status,
			mi.monitoring_status,
			mi.current_health_status,
			mi.last_heartbeat_at,
			mi.current_active_incident_count,
			mi.current_primary_issue_summary
		from monitoring_instances mi
		where mi.current_health_status <> '正常'
		  and `+dashboardCurrentMonitoringInstanceVisibilitySQL("mi")+`
		order by case mi.current_health_status
			when '严重' then 3
			when '告警' then 2
			when '关注' then 1
			else 0
		end desc,
		mi.current_active_incident_count desc,
		mi.updated_at desc,
		mi.monitoring_instance_id asc
		limit $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query dashboard abnormal monitoring instances: %w", err)
	}
	defer rows.Close()

	records := make([]incidents.DashboardMonitoringInstanceSummary, 0)
	for rows.Next() {
		var record incidents.DashboardMonitoringInstanceSummary
		if err := rows.Scan(
			&record.MonitoringInstanceID,
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
			return nil, fmt.Errorf("scan dashboard abnormal monitoring instance row: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate dashboard abnormal monitoring instances: %w", err)
	}
	return records, nil
}

func loadAbnormalTargetSummaries(ctx context.Context, queryer dashboardQueryer, limit int) ([]incidents.DashboardTargetSummary, error) {
	rows, err := queryer.Query(ctx, `
		select
			t.target_id,
			t.name,
			t.target_type,
			t.host,
			t.base_port,
			t.run_status,
			t."group",
			t.current_health_status,
			t.last_success_at,
			t.last_failure_at,
			t.current_active_incident_count,
			t.current_primary_issue_summary
		from targets t
		where t.current_health_status <> '正常'
		  and `+dashboardCurrentTargetVisibilitySQL("t")+`
		order by case t.current_health_status
			when '严重' then 3
			when '告警' then 2
			when '关注' then 1
			else 0
		end desc,
		t.current_active_incident_count desc,
		t.updated_at desc,
		t.target_id asc
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
		),
		visible_events as (
			select e.*
			from state_change_events e
			where `+dashboardCurrentEventVisibilitySQL("e")+`
		)
		select
			coalesce((
				select count(*)::int
				from visible_events e
				where e.event_type = 'incident_started'
					and date_trunc('hour', e.created_at) = hb.bucket_start
			), 0),
			coalesce((
				select count(*)::int
				from visible_events e
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
		with visible_monitoring_instances as (
			select mi.*
			from monitoring_instances mi
			where `+dashboardCurrentMonitoringInstanceVisibilitySQL("mi")+`
		),
		visible_targets as (
			select t.*
			from targets t
			where `+dashboardCurrentTargetVisibilitySQL("t")+`
		),
		visible_events as (
			select e.*
			from state_change_events e
			where `+dashboardCurrentEventVisibilitySQL("e")+`
		)
		select
			(select count(*)::int from visible_monitoring_instances),
			(select count(*)::int from visible_targets),
			(select count(*)::int from visible_monitoring_instances where current_health_status <> '正常'),
			(select count(*)::int from visible_targets where current_health_status <> '正常'),
			(select count(*)::int from visible_monitoring_instances where current_health_status = '严重'),
			(select count(*)::int from visible_targets where current_health_status = '严重'),
			(select count(*)::int from visible_monitoring_instances where monitoring_status = '维护中'),
			(select count(*)::int from visible_targets where run_status = '维护中'),
			(select count(*)::int from visible_monitoring_instances where lifecycle_status = '待接入' or binding_status in ('未绑定', '指纹变更待确认')),
			(select count(*)::int from visible_monitoring_instances where monitoring_status = '暂停'),
			(select count(*)::int from visible_monitoring_instances where lifecycle_status = '已退役'),
			(select count(*)::int from visible_targets where run_status = '暂停'),
			(select count(*)::int from visible_targets where run_status = '已归档'),
			(select count(*)::int from visible_events where event_type = 'incident_started' and created_at >= now() - interval '24 hours'),
			(select count(*)::int from visible_events where event_type = 'incident_recovered' and created_at >= now() - interval '24 hours')
	`).Scan(
		&overview.TotalMonitoringInstanceCount,
		&overview.TotalTargetCount,
		&overview.AbnormalMonitoringInstanceCount,
		&overview.AbnormalTargetCount,
		&overview.SevereMonitoringInstanceCount,
		&overview.SevereTargetCount,
		&overview.MaintenanceMonitoringInstanceCount,
		&overview.MaintenanceTargetCount,
		&overview.PendingOnboardingMonitoringInstanceCount,
		&overview.PausedMonitoringInstanceCount,
		&overview.RetiredMonitoringInstanceCount,
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
		with visible_monitoring_instances as (
			select mi.*
			from monitoring_instances mi
			where `+dashboardCurrentMonitoringInstanceVisibilitySQL("mi")+`
		),
		visible_targets as (
			select t.*
			from targets t
			where `+dashboardCurrentTargetVisibilitySQL("t")+`
		),
		monitoring_instance_groups as (
			select
				coalesce(nullif(btrim("group"), ''), '未分组') as group_name,
				count(*)::int as monitoring_instance_count,
				(count(*) filter (where current_health_status <> '正常'))::int as abnormal_monitoring_instance_count,
				(count(*) filter (where current_health_status = '严重'))::int as severe_monitoring_instance_count,
				(count(*) filter (where monitoring_status = '维护中'))::int as maintenance_monitoring_instance_count
			from visible_monitoring_instances
			group by 1
		),
		target_groups as (
			select
				coalesce(nullif(btrim("group"), ''), '未分组') as group_name,
				count(*)::int as target_count,
				(count(*) filter (where current_health_status <> '正常'))::int as abnormal_target_count,
				(count(*) filter (where current_health_status = '严重'))::int as severe_target_count,
				(count(*) filter (where run_status = '维护中'))::int as maintenance_target_count
			from visible_targets
			group by 1
		)
		select
			coalesce(ng.group_name, tg.group_name) as group_name,
			coalesce(ng.monitoring_instance_count, 0),
			coalesce(tg.target_count, 0),
			coalesce(ng.abnormal_monitoring_instance_count, 0),
			coalesce(tg.abnormal_target_count, 0),
			coalesce(ng.severe_monitoring_instance_count, 0),
			coalesce(tg.severe_target_count, 0),
			coalesce(ng.maintenance_monitoring_instance_count, 0),
			coalesce(tg.maintenance_target_count, 0)
		from monitoring_instance_groups ng
		full outer join target_groups tg on tg.group_name = ng.group_name
		order by
			(coalesce(ng.abnormal_monitoring_instance_count, 0) + coalesce(tg.abnormal_target_count, 0)) desc,
			(coalesce(ng.severe_monitoring_instance_count, 0) + coalesce(tg.severe_target_count, 0)) desc,
			(coalesce(ng.monitoring_instance_count, 0) + coalesce(tg.target_count, 0)) desc,
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
			&record.MonitoringInstanceCount,
			&record.TargetCount,
			&record.AbnormalMonitoringInstanceCount,
			&record.AbnormalTargetCount,
			&record.SevereMonitoringInstanceCount,
			&record.SevereTargetCount,
			&record.MaintenanceMonitoringInstanceCount,
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
			where lifecycle_status not in ('cancelled', 'archived')
		),
		active_vps as (
			select vps_id, lifecycle_status, renewal_decision
			from inventory_vps
		),
		active_links as (
			select distinct vps_id, monitoring_instance_id
			from vps_monitoring_instance_links
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
			select v.vps_id, l.monitoring_instance_id::text as object_id
			from inventory_vps v
			join vps_monitoring_instance_links l on l.vps_id = v.vps_id and l.unlinked_at is null
			join monitoring_instances n on n.monitoring_instance_id = l.monitoring_instance_id
			where v.lifecycle_status = 'to_cancel'
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
			where v.lifecycle_status = 'to_cancel'
			  and t.run_status not in ('已归档', '暂停')
		),
		cancellation_attention as (
			select v.vps_id
			from inventory_vps v
			join subscription_rollup sr on sr.vps_id = v.vps_id
			where
				(sr.inactive_subscription_count > 0 and v.lifecycle_status <> 'to_cancel')
				or (sr.active_subscription_count > 0 and v.lifecycle_status = 'to_cancel')
				or (v.renewal_decision in ('cancel', 'auto_renew_cancelled') and v.lifecycle_status <> 'to_cancel')
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
				join monitoring_instances n on n.monitoring_instance_id = l.monitoring_instance_id
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
		join vps_assets v on v.vps_id = subscriptions.vps_id
		where subscriptions.status = 'active'
		  and v.lifecycle_status not in ('cancelled', 'archived')
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
			(e.object_type = 'monitoring_instance' and exists (
				select 1 from monitoring_instances n where n.monitoring_instance_id = e.object_id and n.labels @> array[$%d]::text[]
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
			incidents.EventMonitoringInstanceMonitoringMaintenanceEntered,
			incidents.EventMonitoringInstanceMonitoringMaintenanceExited,
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
		with visible_events as (
			select e.*
			from state_change_events e
			where ` + dashboardCurrentEventVisibilitySQL("e") + `
		)
		select e.event_id, e.object_type, e.object_id, e.event_type, coalesce(e.severity, ''), e.summary, e.payload, e.created_at
		from visible_events e`
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
		(e.object_type = 'monitoring_instance' and (
			exists (
				select 1
				from monitoring_instance_heartbeats nh
				where nh.monitoring_instance_id = e.object_id
					and nh.is_backfilled
					and nh.observed_at = e.created_at
			)
			or exists (
				select 1
				from host_samples hs
				where hs.monitoring_instance_id = e.object_id
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
