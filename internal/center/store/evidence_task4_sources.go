package store

import (
	"context"
	"fmt"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/evidence/adapters"
	"houfeng/internal/center/incidents"
)

const monitoringEventEvidenceSQL = `
	select
		e.event_id,
		e.object_type,
		e.object_id,
		e.event_type,
		coalesce(e.severity, ''),
		e.summary,
		coalesce(e.payload->>'event_at', '') as event_at,
		e.created_at as recorded_at,
		case when jsonb_typeof(e.payload->'is_backfilled') = 'boolean' then (e.payload->>'is_backfilled')::boolean else false end as is_backfilled,
		coalesce(e.payload->>'provenance', '') as provenance,
		coalesce(e.payload->>'producer_version', '') as producer_version,
		coalesce(e.payload->>'rule_version', '') as rule_version,
		coalesce(e.payload->>'prior_state', '') as prior_state,
		coalesce(e.payload->>'resulting_state', '') as resulting_state,
		coalesce(e.payload->>'correction_of_event_id', '') as correction_of_event_id,
		coalesce(e.payload->>'metric_name', '') as metric_name,
		coalesce(e.payload->>'metric_unit', '') as metric_unit,
		case when jsonb_typeof(e.payload->'metric_value') = 'number' then (e.payload->>'metric_value')::double precision end as metric_value,
		case when jsonb_typeof(e.payload->'metric_threshold') = 'number' then (e.payload->>'metric_threshold')::double precision end as metric_threshold,
		(e.payload ?& array['event_at','is_backfilled','provenance','producer_version','rule_version','prior_state','resulting_state'])
			and jsonb_typeof(e.payload->'event_at') = 'string'
			and jsonb_typeof(e.payload->'is_backfilled') = 'boolean'
			and jsonb_typeof(e.payload->'provenance') = 'string'
			and jsonb_typeof(e.payload->'producer_version') = 'string'
			and jsonb_typeof(e.payload->'rule_version') = 'string'
			and jsonb_typeof(e.payload->'prior_state') = 'string'
			and jsonb_typeof(e.payload->'resulting_state') = 'string' as metadata_complete
	from state_change_events e
	where e.object_id = $1
		and case
			when jsonb_typeof(e.payload->'event_at') = 'string' then (e.payload->>'event_at')::timestamptz
			else e.created_at
		end >= $2
		and case
			when jsonb_typeof(e.payload->'event_at') = 'string' then (e.payload->>'event_at')::timestamptz
			else e.created_at
		end < $3
		and e.object_type = $4
	order by case
		when jsonb_typeof(e.payload->'event_at') = 'string' then (e.payload->>'event_at')::timestamptz
		else e.created_at
	end asc, e.event_id asc
	limit $5`

const commandAuditEvidenceSQL = `
	select
		a.audit_id,
		coalesce(a.action_id, ''),
		a.monitoring_instance_id,
		a.monitoring_instance_name_snapshot,
		coalesce(a.actor_user_id, starter.actor_user_id, ''),
		coalesce(nullif(a.actor_username_snapshot, ''), starter.actor_username_snapshot, ''),
		coalesce(nullif(a.actor_display_name_snapshot, ''), starter.actor_display_name_snapshot, ''),
		a.command_id,
		a.sensitivity,
		a.event_type,
		case
			when a.event_type = 'rejected' then 'rejected'
			when action_state.completed_exit_code = 0 then 'succeeded'
			when action_state.completed_exit_code is not null then 'failed'
			when action_state.has_dispatched then 'dispatched'
			else 'queued'
		end as outcome,
		a.source,
		a.exit_code,
		a.occurred_at,
		a.occurred_at as recorded_at
	from monitoring_instance_command_action_audit a
	left join lateral (
		select q.actor_user_id, q.actor_username_snapshot, q.actor_display_name_snapshot
		from monitoring_instance_command_action_audit q
		where a.action_id is not null and q.action_id = a.action_id and q.event_type = 'queued'
		order by q.occurred_at asc, q.audit_id asc
		limit 1
	) starter on true
	left join lateral (
		select
			coalesce(bool_or(s.event_type = 'dispatched'), false) as has_dispatched,
			(array_agg(s.exit_code order by s.occurred_at desc, s.audit_id desc) filter (where s.event_type = 'completed'))[1] as completed_exit_code
		from monitoring_instance_command_action_audit s
		where a.action_id is not null and s.action_id = a.action_id and s.occurred_at < $3
	) action_state on true
	where a.monitoring_instance_id = $1
		and a.occurred_at >= $2
		and a.occurred_at < $3
	order by a.occurred_at asc, a.audit_id asc
	limit $4`

const subscriptionCostEvidenceSQL = `
	with settings as (
		select
			coalesce(subscription_cost_settings->>'base_currency', '') as base_currency,
			coalesce(subscription_cost_settings->>'exchange_rate_provider', '') as rate_provider,
			case when coalesce(subscription_cost_settings->>'exchange_rate_stale_after_hours', '') ~ '^[0-9]+$'
				then (subscription_cost_settings->>'exchange_rate_stale_after_hours')::integer else 0 end as stale_after_hours,
			updated_at
		from center_settings
		where settings_id = 'center'
	), candidates as (
		select s.*, count(*) over ()::integer as candidate_count
		from subscriptions s
		where s.vps_id = $1
			and s.status = 'active'
			and coalesce(s.ends_at, $3::date) > $2::date
			and coalesce(s.started_at, s.created_at::date) < $3::date
	), chosen as (
		select * from candidates order by subscription_id limit 1
	), rate as (
		select er.*
		from subscription_exchange_rates er, settings cfg, chosen s
		where s.currency <> cfg.base_currency
			and er.provider = cfg.rate_provider
			and er.base_currency = cfg.base_currency
			and er.quote_currency = s.currency
			and er.fetched_at < $3
		order by er.fetched_at desc, er.rate_date desc, er.rate_id desc
		limit 1
	), observed as (
		select transaction_timestamp() as at
	)
	select
		s.candidate_count,
		s.subscription_id,
		s.vps_id,
		s.subscription_id || '/' || coalesce(r.rate_id, 'identity') || '/' || to_char($2::date, 'YYYY-MM') as source_revision,
		s.price::double precision as original_amount,
		s.currency as original_currency,
		s.billing_period_unit,
		s.billing_period_length,
		case when s.currency = cfg.base_currency then 1::double precision else r.rate::double precision end as conversion_rate,
		case when s.currency = cfg.base_currency then 'identity' else cfg.rate_provider end as conversion_provider,
		case when s.currency = cfg.base_currency then $2::date else r.rate_date end as rate_date,
		case when s.currency = cfg.base_currency then o.at else r.fetched_at end as rate_fetched_at,
		case when s.currency = cfg.base_currency then false else r.fetched_at < o.at - (cfg.stale_after_hours * interval '1 hour') end as rate_stale,
		round((s.price * case when s.currency = cfg.base_currency then 1 else r.rate end)::numeric, 4)::double precision as base_amount,
		cfg.base_currency,
		(greatest(coalesce(s.started_at, $2::date), $2::date)::timestamp at time zone 'UTC') as coverage_start,
		(least(coalesce(s.ends_at, $3::date), $3::date)::timestamp at time zone 'UTC') as coverage_end,
		case when coalesce(s.started_at, $2::date) <= $2::date and coalesce(s.ends_at, $3::date) >= $3::date then 'complete' else 'partial' end as coverage_status,
		(least(coalesce(s.ends_at, $3::date), $3::date) - greatest(coalesce(s.started_at, $2::date), $2::date))::integer as covered_days,
		($3::date - $2::date)::integer as total_days,
		o.at as observed_at,
		greatest(s.updated_at, cfg.updated_at, coalesce(r.updated_at, s.updated_at), o.at) as source_watermark,
		cfg.base_currency ~ '^[A-Z]{3}$' and cfg.rate_provider in ('frankfurter','fixer') and cfg.stale_after_hours > 0
			and (s.currency = cfg.base_currency or r.rate_id is not null) as metadata_complete
	from chosen s
	cross join settings cfg
	cross join observed o
	left join rate r on true
	where greatest(coalesce(s.started_at, $2::date), $2::date) >= $2::date
		and least(coalesce(s.ends_at, $3::date), $3::date) <= $3::date`

const subscriptionBudgetEvidenceSQL = `
	with settings as (
		select
			coalesce(subscription_cost_settings->>'base_currency', '') as base_currency,
			coalesce(subscription_cost_settings->>'exchange_rate_provider', '') as rate_provider
		from center_settings
		where settings_id = 'center'
	), states as (
		select
			s.subscription_id,
			coalesce(
					(select h.to_monthly_price::double precision from price_histories h where h.subscription_id = s.subscription_id and h.changed_at < $2 order by h.changed_at desc, h.created_at desc, h.price_history_id desc limit 1),
					(select h.from_monthly_price::double precision from price_histories h where h.subscription_id = s.subscription_id and h.changed_at >= $2 order by h.changed_at asc, h.created_at asc, h.price_history_id asc limit 1),
				s.monthly_price::double precision
			) as monthly_price,
			coalesce(
					(select h.to_currency from price_histories h where h.subscription_id = s.subscription_id and h.changed_at < $2 order by h.changed_at desc, h.created_at desc, h.price_history_id desc limit 1),
					(select h.from_currency from price_histories h where h.subscription_id = s.subscription_id and h.changed_at >= $2 order by h.changed_at asc, h.created_at asc, h.price_history_id asc limit 1),
				s.currency
			) as currency,
			coalesce(
					(select h.to_status from price_histories h where h.subscription_id = s.subscription_id and h.changed_at < $2 order by h.changed_at desc, h.created_at desc, h.price_history_id desc limit 1),
					(select h.from_status from price_histories h where h.subscription_id = s.subscription_id and h.changed_at >= $2 order by h.changed_at asc, h.created_at asc, h.price_history_id asc limit 1),
				s.status
			) as status
		from subscriptions s
		join vps_assets v on v.vps_id = s.vps_id
			where s.created_at < $2
				and v.lifecycle_status not in ('cancelled','archived')
	), converted as (
		select st.*,
			case when st.currency = cfg.base_currency then 1::double precision else er.rate::double precision end as rate
		from states st
		cross join settings cfg
		left join lateral (
			select rate
			from subscription_exchange_rates er
				where er.provider = cfg.rate_provider and er.base_currency = cfg.base_currency and er.quote_currency = st.currency and er.fetched_at < $2
			order by er.fetched_at desc, er.rate_date desc, er.rate_id desc
			limit 1
		) er on st.currency <> cfg.base_currency
	), totals as (
		select
			coalesce(round(sum(case when status = 'active' and rate is not null then monthly_price * rate else null end)::numeric, 4)::double precision, 0) as actual_spend,
			count(*) filter (where status = 'active' and rate is not null)::bigint as converted_count,
			count(*) filter (where status = 'active' and rate is null)::bigint as missing_rate_count
		from converted
	)
	select
		'subscription_monthly_budgets'::text as budget_source,
		b.budget_month,
		b.base_currency,
		b.monthly_limit::double precision,
		b.warning_pct,
		t.actual_spend,
		t.converted_count,
		t.missing_rate_count,
		b.updated_at
	from subscription_monthly_budgets b
	cross join totals t
		where b.budget_month <= $1::date
			and b.budget_month < $2::date
		order by b.budget_month desc
		limit 1`

const assetRenewalEvidenceSQL = `
	select decision_id, from_decision, to_decision, reason, decided_at, created_at
	from renewal_decisions
	where vps_id = $1 and decided_at >= $2 and decided_at < $3
	order by decided_at asc, decision_id asc
	limit $4`

const assetPriceEvidenceSQL = `
	select price_history_id, subscription_id, from_price::double precision, to_price::double precision,
		from_currency, to_currency, from_billing_period_unit, to_billing_period_unit,
		from_billing_period_length, to_billing_period_length, changed_at, created_at
	from price_histories
	where vps_id = $1 and changed_at >= $2 and changed_at < $3
	order by changed_at asc, price_history_id asc
	limit $4`

const assetIPEvidenceSQL = `
	select ip_history_id, from_ipv4, to_ipv4, from_ipv6, to_ipv6, changed_at, created_at
	from ip_histories
	where vps_id = $1 and changed_at >= $2 and changed_at < $3
	order by changed_at asc, ip_history_id asc
	limit $4`

const assetSpecEvidenceSQL = `
	select snapshot_id, product_name, os_name, virtualization, ssh_port, captured_at, created_at
	from vps_spec_snapshots
	where vps_id = $1 and captured_at >= $2 and captured_at < $3
	order by captured_at asc, snapshot_id asc
	limit $4`

func (r *PostgresIncidentRepository) LoadMonitoringEventEvidence(ctx context.Context, sourceType, sourceID string, window evidence.TimeWindow) (adapters.MonitoringEventCapture, error) {
	if r == nil || r.query == nil {
		return adapters.MonitoringEventCapture{}, fmt.Errorf("monitoring event evidence source unavailable")
	}
	rows, err := r.query(ctx, monitoringEventEvidenceSQL, sourceID, window.Start, window.End, sourceType, int(evidence.MaxSnapshotDataPoints)+1)
	if err != nil {
		return adapters.MonitoringEventCapture{}, fmt.Errorf("query monitoring event evidence: %w", err)
	}
	defer rows.Close()
	capture := adapters.MonitoringEventCapture{ProducerVersion: incidents.MonitoringEventEvidenceSourceVersion}
	var watermark time.Time
	for rows.Next() {
		var fact adapters.MonitoringEventFact
		var rawEventAt string
		var metricName, metricUnit string
		var metricValue, metricThreshold *float64
		var metadataComplete bool
		if err := rows.Scan(&fact.EventID, &fact.ObjectType, &fact.ObjectID, &fact.EventType, &fact.Severity, &fact.Summary, &rawEventAt, &fact.RecordedAt, &fact.Backfilled, &fact.Provenance, &fact.ProducerVersion, &fact.RuleVersion, &fact.PriorState, &fact.ResultingState, &fact.CorrectionOfEventID, &metricName, &metricUnit, &metricValue, &metricThreshold, &metadataComplete); err != nil {
			return adapters.MonitoringEventCapture{}, fmt.Errorf("scan monitoring event evidence: %w", err)
		}
		fact.RecordedAt = canonicalEvidenceSourceTime(fact.RecordedAt)
		metricEmpty := metricName == "" && metricUnit == "" && metricValue == nil && metricThreshold == nil
		metricComplete := metricName != "" && metricUnit != "" && metricValue != nil && metricThreshold != nil
		if !metadataComplete || (!metricEmpty && !metricComplete) {
			return adapters.MonitoringEventCapture{}, fmt.Errorf("monitoring event evidence metadata incomplete")
		}
		eventAt, err := parseCanonicalMonitoringEventSourceTime(rawEventAt)
		if err != nil {
			return adapters.MonitoringEventCapture{}, fmt.Errorf("monitoring event evidence timestamp invalid: %w", err)
		}
		fact.EventAt = eventAt
		if metricName != "" {
			fact.Metrics = []adapters.MonitoringEventMetric{{Metric: metricName, Unit: metricUnit, Value: *metricValue, Threshold: *metricThreshold}}
		}
		capture.Events = append(capture.Events, fact)
		if fact.RecordedAt.After(watermark) {
			watermark = fact.RecordedAt
		}
		if uint64(len(capture.Events)) > evidence.MaxSnapshotDataPoints {
			return adapters.MonitoringEventCapture{}, fmt.Errorf("monitoring event evidence exceeds source bound")
		}
	}
	if err := rows.Err(); err != nil {
		return adapters.MonitoringEventCapture{}, fmt.Errorf("iterate monitoring event evidence: %w", err)
	}
	capture.EventCount = uint64(len(capture.Events))
	if !watermark.IsZero() {
		capture.SourceWatermark = watermark.UTC().Format(time.RFC3339Nano)
	}
	return capture, nil
}

func (r *PostgresCommandAuditRepository) LoadCommandAuditEvidence(ctx context.Context, sourceID string, window evidence.TimeWindow) (adapters.CommandAuditCapture, error) {
	if r == nil || r.db == nil {
		return adapters.CommandAuditCapture{}, fmt.Errorf("command audit evidence source unavailable")
	}
	rows, err := r.db.Query(ctx, commandAuditEvidenceSQL, sourceID, window.Start, window.End, int(evidence.MaxSnapshotDataPoints)+1)
	if err != nil {
		return adapters.CommandAuditCapture{}, fmt.Errorf("query command audit evidence: %w", err)
	}
	defer rows.Close()
	capture := adapters.CommandAuditCapture{ProducerVersion: "command-audit-store/v1"}
	var watermark time.Time
	for rows.Next() {
		var fact adapters.CommandAuditFact
		var recordedAt time.Time
		if err := rows.Scan(&fact.AuditID, &fact.ActionID, &fact.MonitoringInstanceID, &fact.MonitoringInstanceName, &fact.ActorUserID, &fact.ActorUsername, &fact.ActorDisplayName, &fact.CommandID, &fact.Sensitivity, &fact.EventType, &fact.Outcome, &fact.Source, &fact.ExitCode, &fact.OccurredAt, &recordedAt); err != nil {
			return adapters.CommandAuditCapture{}, fmt.Errorf("scan command audit evidence: %w", err)
		}
		fact.OccurredAt = canonicalEvidenceSourceTime(fact.OccurredAt)
		recordedAt = canonicalEvidenceSourceTime(recordedAt)
		capture.Audits = append(capture.Audits, fact)
		if recordedAt.After(watermark) {
			watermark = recordedAt
		}
		if uint64(len(capture.Audits)) > evidence.MaxSnapshotDataPoints {
			return adapters.CommandAuditCapture{}, fmt.Errorf("command audit evidence exceeds source bound")
		}
	}
	if err := rows.Err(); err != nil {
		return adapters.CommandAuditCapture{}, fmt.Errorf("iterate command audit evidence: %w", err)
	}
	capture.AuditCount = uint64(len(capture.Audits))
	if !watermark.IsZero() {
		capture.SourceWatermark = watermark.UTC().Format(time.RFC3339Nano)
	}
	return capture, nil
}

func (r *PostgresSubscriptionCostRepository) LoadSubscriptionCostEvidence(ctx context.Context, sourceID string, window evidence.TimeWindow) (adapters.SubscriptionCostCapture, error) {
	if r == nil || r.db == nil {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("subscription cost evidence source unavailable")
	}
	rows, err := r.db.Query(ctx, subscriptionCostEvidenceSQL, sourceID, window.Start, window.End)
	if err != nil {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("query subscription cost evidence: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return adapters.SubscriptionCostCapture{}, fmt.Errorf("iterate subscription cost evidence: %w", err)
		}
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("subscription cost evidence not found")
	}
	var capture adapters.SubscriptionCostCapture
	var candidateCount int
	var rateDate time.Time
	var watermark time.Time
	var metadataComplete bool
	if err := rows.Scan(&candidateCount, &capture.SubscriptionID, &capture.VPSID, &capture.SourceRevision, &capture.OriginalAmount, &capture.OriginalCurrency, &capture.BillingPeriodUnit, &capture.BillingPeriodLength, &capture.ConversionRate, &capture.ConversionProvider, &rateDate, &capture.RateFetchedAt, &capture.RateStale, &capture.BaseAmount, &capture.BaseCurrency, &capture.CoverageStart, &capture.CoverageEnd, &capture.CoverageStatus, &capture.CoveredDays, &capture.TotalDays, &capture.ObservedAt, &watermark, &metadataComplete); err != nil {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("scan subscription cost evidence: %w", err)
	}
	capture.RateFetchedAt = canonicalEvidenceSourceTime(capture.RateFetchedAt)
	capture.CoverageStart = canonicalEvidenceSourceTime(capture.CoverageStart)
	capture.CoverageEnd = canonicalEvidenceSourceTime(capture.CoverageEnd)
	capture.ObservedAt = canonicalEvidenceSourceTime(capture.ObservedAt)
	watermark = canonicalEvidenceSourceTime(watermark)
	if candidateCount != 1 || !metadataComplete || rows.Next() {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("subscription cost evidence source is ambiguous or incomplete")
	}
	if err := rows.Err(); err != nil {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("iterate subscription cost evidence: %w", err)
	}
	capture.RateDate = rateDate.UTC().Format("2006-01-02")

	budgetRows, err := r.db.Query(ctx, subscriptionBudgetEvidenceSQL, window.Start, window.End)
	if err != nil {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("query subscription budget evidence: %w", err)
	}
	defer budgetRows.Close()
	if !budgetRows.Next() {
		if err := budgetRows.Err(); err != nil {
			return adapters.SubscriptionCostCapture{}, fmt.Errorf("iterate subscription budget evidence: %w", err)
		}
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("subscription budget evidence not found")
	}
	var budgetMonth, budgetWatermark time.Time
	var budgetCurrency string
	var convertedCount, missingRateCount int64
	if err := budgetRows.Scan(&capture.BudgetSource, &budgetMonth, &budgetCurrency, &capture.BudgetMonthlyLimit, &capture.BudgetWarningPct, &capture.BudgetActualSpend, &convertedCount, &missingRateCount, &budgetWatermark); err != nil {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("scan subscription budget evidence: %w", err)
	}
	budgetWatermark = canonicalEvidenceSourceTime(budgetWatermark)
	if budgetCurrency != capture.BaseCurrency {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("subscription budget currency does not match evidence base currency")
	}
	capture.BudgetCurrency = budgetCurrency
	if convertedCount < 0 || missingRateCount < 0 || budgetRows.Next() {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("subscription budget evidence source is ambiguous or invalid")
	}
	if err := budgetRows.Err(); err != nil {
		return adapters.SubscriptionCostCapture{}, fmt.Errorf("iterate subscription budget evidence: %w", err)
	}
	capture.BudgetMonth = budgetMonth.UTC().Format("2006-01")
	capture.ConvertedSubscriptionCount = uint64(convertedCount)
	capture.MissingRateCount = uint64(missingRateCount)
	if capture.MissingRateCount > 0 || capture.BudgetMonthlyLimit <= 0 {
		capture.BudgetStatus = "unknown"
	} else if capture.BudgetActualSpend >= capture.BudgetMonthlyLimit {
		capture.BudgetStatus = "over"
	} else if capture.BudgetActualSpend >= capture.BudgetMonthlyLimit*float64(capture.BudgetWarningPct)/100 {
		capture.BudgetStatus = "warning"
	} else {
		capture.BudgetStatus = "ok"
	}
	if capture.MissingRateCount > 0 {
		capture.CoverageStatus = "missing_rate"
	}
	if budgetWatermark.After(watermark) {
		watermark = budgetWatermark
	}
	capture.SourceWatermark = watermark.UTC().Format(time.RFC3339Nano)
	capture.ProducerVersion = "subscription-cost-store/v1"
	return capture, nil
}

func (r *PostgresRenewalDecisionRepository) LoadAssetHistory(ctx context.Context, sourceID string, window evidence.TimeWindow) (adapters.AssetHistoryCapture, error) {
	if r == nil || r.db == nil {
		return adapters.AssetHistoryCapture{}, fmt.Errorf("asset history source unavailable")
	}
	limit := int(evidence.MaxSnapshotDataPoints) + 1
	capture := adapters.AssetHistoryCapture{Version: adapters.AssetHistorySourceVersionV1, VPSID: sourceID, ProducerVersion: "asset-ledger/v1"}
	seenCount := uint64(0)
	watermark := time.Time{}
	updateWatermark := func(recordedAt time.Time) {
		if recordedAt.After(watermark) {
			watermark = recordedAt
		}
	}

	renewalRows, err := r.db.Query(ctx, assetRenewalEvidenceSQL, sourceID, window.Start, window.End, limit)
	if err != nil {
		return adapters.AssetHistoryCapture{}, fmt.Errorf("query asset renewal evidence: %w", err)
	}
	for renewalRows.Next() {
		var fact adapters.AssetRenewalDecision
		var from *string
		if err := renewalRows.Scan(&fact.DecisionID, &from, &fact.ToDecision, &fact.Reason, &fact.DecidedAt, &fact.RecordedAt); err != nil {
			renewalRows.Close()
			return adapters.AssetHistoryCapture{}, fmt.Errorf("scan asset renewal evidence: %w", err)
		}
		if from != nil {
			fact.FromDecision = *from
		}
		fact.DecidedAt = canonicalEvidenceSourceTime(fact.DecidedAt)
		fact.RecordedAt = canonicalEvidenceSourceTime(fact.RecordedAt)
		capture.RenewalDecisions = append(capture.RenewalDecisions, fact)
		seenCount++
		updateWatermark(fact.RecordedAt)
		if seenCount > evidence.MaxSnapshotDataPoints {
			renewalRows.Close()
			return adapters.AssetHistoryCapture{}, fmt.Errorf("asset history evidence exceeds source bound")
		}
	}
	if err := closeEvidenceRows(renewalRows, "asset renewal"); err != nil {
		return adapters.AssetHistoryCapture{}, err
	}

	priceRows, err := r.db.Query(ctx, assetPriceEvidenceSQL, sourceID, window.Start, window.End, limit)
	if err != nil {
		return adapters.AssetHistoryCapture{}, fmt.Errorf("query asset price evidence: %w", err)
	}
	for priceRows.Next() {
		var fact adapters.AssetPriceHistory
		if err := priceRows.Scan(&fact.HistoryID, &fact.SubscriptionID, &fact.FromAmount, &fact.ToAmount, &fact.FromCurrency, &fact.ToCurrency, &fact.FromBillingPeriodUnit, &fact.ToBillingPeriodUnit, &fact.FromBillingPeriodLength, &fact.ToBillingPeriodLength, &fact.ChangedAt, &fact.RecordedAt); err != nil {
			priceRows.Close()
			return adapters.AssetHistoryCapture{}, fmt.Errorf("scan asset price evidence: %w", err)
		}
		fact.ChangedAt = canonicalEvidenceSourceTime(fact.ChangedAt)
		fact.RecordedAt = canonicalEvidenceSourceTime(fact.RecordedAt)
		capture.PriceHistories = append(capture.PriceHistories, fact)
		seenCount++
		updateWatermark(fact.RecordedAt)
		if seenCount > evidence.MaxSnapshotDataPoints {
			priceRows.Close()
			return adapters.AssetHistoryCapture{}, fmt.Errorf("asset history evidence exceeds source bound")
		}
	}
	if err := closeEvidenceRows(priceRows, "asset price"); err != nil {
		return adapters.AssetHistoryCapture{}, err
	}

	ipRows, err := r.db.Query(ctx, assetIPEvidenceSQL, sourceID, window.Start, window.End, limit)
	if err != nil {
		return adapters.AssetHistoryCapture{}, fmt.Errorf("query asset IP evidence: %w", err)
	}
	for ipRows.Next() {
		var fact adapters.AssetIPHistory
		if err := ipRows.Scan(&fact.HistoryID, &fact.FromIPv4, &fact.ToIPv4, &fact.FromIPv6, &fact.ToIPv6, &fact.ChangedAt, &fact.RecordedAt); err != nil {
			ipRows.Close()
			return adapters.AssetHistoryCapture{}, fmt.Errorf("scan asset IP evidence: %w", err)
		}
		fact.ChangedAt = canonicalEvidenceSourceTime(fact.ChangedAt)
		fact.RecordedAt = canonicalEvidenceSourceTime(fact.RecordedAt)
		capture.IPHistories = append(capture.IPHistories, fact)
		seenCount++
		updateWatermark(fact.RecordedAt)
		if seenCount > evidence.MaxSnapshotDataPoints {
			ipRows.Close()
			return adapters.AssetHistoryCapture{}, fmt.Errorf("asset history evidence exceeds source bound")
		}
	}
	if err := closeEvidenceRows(ipRows, "asset IP"); err != nil {
		return adapters.AssetHistoryCapture{}, err
	}

	specRows, err := r.db.Query(ctx, assetSpecEvidenceSQL, sourceID, window.Start, window.End, limit)
	if err != nil {
		return adapters.AssetHistoryCapture{}, fmt.Errorf("query asset spec evidence: %w", err)
	}
	for specRows.Next() {
		var fact adapters.AssetSpecSnapshot
		if err := specRows.Scan(&fact.SnapshotID, &fact.ProductName, &fact.OSName, &fact.Virtualization, &fact.SSHPort, &fact.CapturedAt, &fact.RecordedAt); err != nil {
			specRows.Close()
			return adapters.AssetHistoryCapture{}, fmt.Errorf("scan asset spec evidence: %w", err)
		}
		fact.CapturedAt = canonicalEvidenceSourceTime(fact.CapturedAt)
		fact.RecordedAt = canonicalEvidenceSourceTime(fact.RecordedAt)
		capture.SpecSnapshots = append(capture.SpecSnapshots, fact)
		seenCount++
		updateWatermark(fact.RecordedAt)
		if seenCount > evidence.MaxSnapshotDataPoints {
			specRows.Close()
			return adapters.AssetHistoryCapture{}, fmt.Errorf("asset history evidence exceeds source bound")
		}
	}
	if err := closeEvidenceRows(specRows, "asset spec"); err != nil {
		return adapters.AssetHistoryCapture{}, err
	}
	if seenCount > evidence.MaxSnapshotDataPoints {
		return adapters.AssetHistoryCapture{}, fmt.Errorf("asset history evidence exceeds source bound")
	}
	capture.FactCount = seenCount
	if !watermark.IsZero() {
		capture.SourceWatermark = watermark.UTC().Format(time.RFC3339Nano)
	}
	return capture, nil
}

type closableEvidenceRows interface {
	Close()
	Err() error
}

func closeEvidenceRows(rows closableEvidenceRows, name string) error {
	defer rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s evidence: %w", name, err)
	}
	return nil
}

func canonicalEvidenceSourceTime(value time.Time) time.Time {
	return value.UTC().Round(0)
}

func parseCanonicalMonitoringEventSourceTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value || parsed.Nanosecond()%1_000 != 0 {
		return time.Time{}, fmt.Errorf("noncanonical monitoring event time")
	}
	return parsed, nil
}

var _ adapters.MonitoringEventSource = (*PostgresIncidentRepository)(nil)
var _ adapters.CommandAuditSource = (*PostgresCommandAuditRepository)(nil)
var _ adapters.SubscriptionCostSource = (*PostgresSubscriptionCostRepository)(nil)
var _ adapters.AssetHistorySource = (*PostgresRenewalDecisionRepository)(nil)
