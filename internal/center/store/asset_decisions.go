package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetdecisions"
	"houfeng/internal/center/subscriptions"
)

var _ assetdecisions.Repository = (*PostgresAssetDecisionRepository)(nil)

type assetDecisionDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresAssetDecisionRepository struct {
	db assetDecisionDB
}

func NewPostgresAssetDecisionRepository(db *pgxpool.Pool) *PostgresAssetDecisionRepository {
	return &PostgresAssetDecisionRepository{db: db}
}

func (r *PostgresAssetDecisionRepository) GetOverview(ctx context.Context, filters assetdecisions.ListFilters) (assetdecisions.Overview, error) {
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return assetdecisions.Overview{}, err
	}
	return assetdecisions.DeriveOverview(facts, filters)
}

func (r *PostgresAssetDecisionRepository) ListGroups(ctx context.Context, filters assetdecisions.ListFilters) ([]assetdecisions.GroupSummary, error) {
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := assetdecisions.DeriveGroups(facts, filters)
	if err != nil {
		return nil, err
	}
	summaries := make([]assetdecisions.GroupSummary, 0, len(groups))
	for _, group := range groups {
		summaries = append(summaries, group.GroupSummary)
	}
	return summaries, nil
}

func (r *PostgresAssetDecisionRepository) GetGroup(ctx context.Context, groupID string, filters assetdecisions.ListFilters) (assetdecisions.GroupDetail, error) {
	facts, err := r.loadFacts(ctx)
	if err != nil {
		return assetdecisions.GroupDetail{}, err
	}
	return assetdecisions.FindGroup(facts, groupID, filters)
}

func (r *PostgresAssetDecisionRepository) loadFacts(ctx context.Context) ([]assetdecisions.Fact, error) {
	rows, err := r.db.Query(ctx, `
		with primary_subscriptions as (
			select distinct on (s.vps_id)
				s.subscription_id,
				s.vps_id,
				s.price::float8,
				s.currency,
				s.billing_cycle,
				s.billing_months,
				s.billing_period_unit,
				s.billing_period_length,
				s.monthly_price::float8,
				s.started_at,
				s.renew_at,
				s.auto_renew,
				s.auto_renew_cancelled,
				s.renewal_mode,
				s.status,
				s.payment_method,
				s.display_name,
				s.cost_category,
				s.labels,
				s.trial_ends_at,
				s.ends_at,
				s.note,
				s.created_at,
				s.updated_at
			from subscriptions s
			order by
				s.vps_id,
				case when s.status = 'active' then 0 else 1 end,
				s.renew_at asc nulls last,
				s.updated_at desc,
				s.subscription_id asc
		),
		subscription_rollup as (
			select
				s.vps_id,
				count(*)::int as subscription_count,
				(count(*) filter (where s.status = 'active'))::int as active_subscription_count,
				(count(*) filter (where s.status in ('expired', 'cancelled', 'paused')))::int as inactive_subscription_count
			from subscriptions s
			group by s.vps_id
		),
		service_rollup as (
			select
				vps_id,
				count(*)::int as service_count
			from asset_services
			group by vps_id
		),
		domain_rollup as (
			select
				vps_id,
				count(*)::int as domain_count
			from asset_domains
			group by vps_id
		),
		target_rollup as (
			select
				a.vps_id,
				count(distinct a.target_id)::int as target_count,
				(count(distinct a.target_id) filter (where t.run_status not in ('已归档', '暂停')))::int as running_target_count
			from (
				select vps_id, target_id from asset_services where target_id is not null
				union all
				select vps_id, target_id from asset_domains where target_id is not null
			) a
			left join targets t on t.target_id = a.target_id
			group by a.vps_id
		),
		monitoring_rollup as (
			select
				l.vps_id,
				count(*)::int as monitoring_link_count,
				(count(*) filter (where n.lifecycle_status not in ('不续费', '已退役')))::int as running_monitoring_count,
				(count(*) filter (where n.current_health_status <> '正常'))::int as abnormal_monitoring_count,
				coalesce(sum(n.current_active_incident_count), 0)::int as active_incident_count,
				coalesce((array_remove(array_agg(nullif(n.current_primary_issue_summary, '') order by n.current_active_incident_count desc, n.updated_at desc), null))[1], '') as primary_issue_summary
			from vps_monitoring_instance_links l
			left join monitoring_instances n on n.monitoring_instance_id = l.monitoring_instance_id
			where l.unlinked_at is null
			group by l.vps_id
		)
		select
			v.vps_id,
			v.display_name,
			v.provider_id,
			coalesce(nullif(v.provider_name, ''), p.name, ''),
			v.product_name,
			v.order_ref,
			v.country,
			v.region,
			v.city,
			v.datacenter,
			v.ipv4,
			v.ipv6,
			v.ssh_host,
			v.ssh_port,
			v.ssh_user,
			v.os_name,
			v.virtualization,
			v.lifecycle_status,
			v.usage_status,
			v.renewal_decision,
			v.importance,
			v.labels,
			v.note,
			coalesce(mr.monitoring_link_count, 0),
			coalesce(mr.running_monitoring_count, 0),
			coalesce(tr.running_target_count, 0),
			v.created_at,
			v.updated_at,
			v.archived_at,
			coalesce(sr.subscription_count, 0),
			coalesce(sr.active_subscription_count, 0),
			coalesce(sr.inactive_subscription_count, 0),
			coalesce(svr.service_count, 0),
			coalesce(dr.domain_count, 0),
			coalesce(tr.target_count, 0),
			coalesce(tr.running_target_count, 0),
			coalesce(mr.monitoring_link_count, 0),
			coalesce(mr.running_monitoring_count, 0),
			coalesce(mr.abnormal_monitoring_count, 0),
			coalesce(mr.active_incident_count, 0),
			coalesce(mr.primary_issue_summary, ''),
			ps.subscription_id is not null,
			coalesce(ps.subscription_id, ''),
			coalesce(ps.vps_id, ''),
			coalesce(ps.price, 0),
			coalesce(ps.currency, ''),
			coalesce(ps.billing_cycle, ''),
			coalesce(ps.billing_months, 0),
			coalesce(ps.billing_period_unit, ''),
			coalesce(ps.billing_period_length, 0),
			coalesce(ps.monthly_price, 0),
			ps.started_at,
			ps.renew_at,
			coalesce(ps.auto_renew, false),
			coalesce(ps.auto_renew_cancelled, false),
			coalesce(ps.renewal_mode, ''),
			coalesce(ps.status, ''),
			coalesce(ps.payment_method, ''),
			coalesce(ps.display_name, ''),
			coalesce(ps.cost_category, ''),
			coalesce(ps.labels, '{}'::text[]),
			ps.trial_ends_at,
			ps.ends_at,
			coalesce(ps.note, ''),
			ps.created_at,
			ps.updated_at
		from vps_assets v
		left join providers p on p.provider_id = v.provider_id
		left join subscription_rollup sr on sr.vps_id = v.vps_id
		left join primary_subscriptions ps on ps.vps_id = v.vps_id
		left join service_rollup svr on svr.vps_id = v.vps_id
		left join domain_rollup dr on dr.vps_id = v.vps_id
		left join target_rollup tr on tr.vps_id = v.vps_id
		left join monitoring_rollup mr on mr.vps_id = v.vps_id
		order by lower(v.display_name), v.vps_id`)
	if err != nil {
		return nil, fmt.Errorf("query asset decision facts: %w", err)
	}
	defer rows.Close()

	facts := make([]assetdecisions.Fact, 0)
	for rows.Next() {
		fact, err := scanAssetDecisionFact(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset decision fact: %w", err)
		}
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset decision facts: %w", err)
	}
	return facts, nil
}

type assetDecisionFactScanner interface {
	Scan(dest ...any) error
}

func scanAssetDecisionFact(row assetDecisionFactScanner) (assetdecisions.Fact, error) {
	var (
		fact        assetdecisions.Fact
		hasSub      bool
		sub         subscriptions.Record
		startedAt   *time.Time
		renewAt     *time.Time
		trialEndsAt *time.Time
		endsAt      *time.Time
		subCreated  *time.Time
		subUpdated  *time.Time
	)
	if err := row.Scan(
		&fact.VPS.VPSID,
		&fact.VPS.DisplayName,
		&fact.VPS.ProviderID,
		&fact.VPS.ProviderName,
		&fact.VPS.ProductName,
		&fact.VPS.OrderRef,
		&fact.VPS.Country,
		&fact.VPS.Region,
		&fact.VPS.City,
		&fact.VPS.Datacenter,
		&fact.VPS.IPv4,
		&fact.VPS.IPv6,
		&fact.VPS.SSHHost,
		&fact.VPS.SSHPort,
		&fact.VPS.SSHUser,
		&fact.VPS.OSName,
		&fact.VPS.Virtualization,
		&fact.VPS.LifecycleStatus,
		&fact.VPS.UsageStatus,
		&fact.VPS.RenewalDecision,
		&fact.VPS.Importance,
		&fact.VPS.Labels,
		&fact.VPS.Note,
		&fact.VPS.ActiveMonitoringInstanceLinkCount,
		&fact.VPS.RunningMonitoringInstanceCount,
		&fact.VPS.RunningTargetCount,
		&fact.VPS.CreatedAt,
		&fact.VPS.UpdatedAt,
		&fact.VPS.ArchivedAt,
		&fact.SubscriptionCount,
		&fact.ActiveSubscriptionCount,
		&fact.InactiveSubscriptionCount,
		&fact.ServiceCount,
		&fact.DomainCount,
		&fact.TargetCount,
		&fact.RunningTargetCount,
		&fact.MonitoringLinkCount,
		&fact.RunningMonitoringCount,
		&fact.AbnormalMonitoringCount,
		&fact.ActiveIncidentCount,
		&fact.PrimaryIssueSummary,
		&hasSub,
		&sub.SubscriptionID,
		&sub.VPSID,
		&sub.Price,
		&sub.Currency,
		&sub.BillingCycle,
		&sub.BillingMonths,
		&sub.BillingPeriodUnit,
		&sub.BillingPeriodLength,
		&sub.MonthlyPrice,
		&startedAt,
		&renewAt,
		&sub.AutoRenew,
		&sub.AutoRenewCancelled,
		&sub.RenewalMode,
		&sub.Status,
		&sub.PaymentMethod,
		&sub.DisplayName,
		&sub.CostCategory,
		&sub.Labels,
		&trialEndsAt,
		&endsAt,
		&sub.Note,
		&subCreated,
		&subUpdated,
	); err != nil {
		return assetdecisions.Fact{}, err
	}

	fact.SourceAvailability = assetdecisions.SourceAvailability{
		Subscriptions: true,
		Services:      true,
		Domains:       true,
		Monitoring:    true,
		Targets:       true,
	}
	if hasSub {
		sub.StartedAt = subscriptions.DateFromTimePtr(startedAt)
		sub.RenewAt = subscriptions.DateFromTimePtr(renewAt)
		sub.TrialEndsAt = subscriptions.DateFromTimePtr(trialEndsAt)
		sub.EndsAt = subscriptions.DateFromTimePtr(endsAt)
		if subCreated != nil {
			sub.CreatedAt = *subCreated
		}
		if subUpdated != nil {
			sub.UpdatedAt = *subUpdated
		}
		fact.PrimarySubscription = &sub
	}
	return fact, nil
}
