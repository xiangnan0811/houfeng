package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/renewals"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

var _ assetlifecycle.Repository = (*PostgresAssetLifecycleRepository)(nil)

type assetLifecycleDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type assetLifecycleQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresAssetLifecycleRepository struct {
	db assetLifecycleDB
}

func NewPostgresAssetLifecycleRepository(db *pgxpool.Pool) *PostgresAssetLifecycleRepository {
	return &PostgresAssetLifecycleRepository{db: db}
}

func (r *PostgresAssetLifecycleRepository) CountRunningTargetsForVPS(ctx context.Context, vpsID string) (int, error) {
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return 0, fmt.Errorf("%w: vps_id is required", assetlifecycle.ErrInvalidLifecycleActionInput)
	}

	var count int
	if err := r.db.QueryRow(ctx, `
		with linked_targets as (
			select target_id
			from asset_services
			where vps_id = $1 and target_id is not null
			union
			select target_id
			from asset_domains
			where vps_id = $1 and target_id is not null
		)
		select count(*)::int
		from linked_targets lt
		join targets t on t.target_id = lt.target_id
		where t.run_status not in ($2, $3)`,
		vpsID,
		targets.RunStatusArchived,
		targets.RunStatusPaused,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count running targets for vps %q: %w", vpsID, err)
	}
	return count, nil
}

func (r *PostgresAssetLifecycleRepository) GetVPSCancellationPreview(ctx context.Context, vpsID string) (assetlifecycle.CancellationPreview, error) {
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return assetlifecycle.CancellationPreview{}, fmt.Errorf("%w: vps_id is required", assetlifecycle.ErrInvalidLifecycleActionInput)
	}

	vps, err := getLifecycleVPSAsset(ctx, r.db, vpsID, false)
	if err != nil {
		return assetlifecycle.CancellationPreview{}, err
	}

	subscriptionRecords, err := listLifecycleSubscriptionsForVPS(ctx, r.db, vpsID, false)
	if err != nil {
		return assetlifecycle.CancellationPreview{}, err
	}
	monitoringInstanceLinks, err := listLifecycleMonitoringInstancesForVPS(ctx, r.db, vpsID)
	if err != nil {
		return assetlifecycle.CancellationPreview{}, err
	}
	services, err := listLifecycleAssetServicesForVPS(ctx, r.db, vpsID)
	if err != nil {
		return assetlifecycle.CancellationPreview{}, err
	}
	domains, err := listLifecycleAssetDomainsForVPS(ctx, r.db, vpsID)
	if err != nil {
		return assetlifecycle.CancellationPreview{}, err
	}
	targetLinks, err := listLifecycleTargetImpacts(ctx, r.db, services, domains)
	if err != nil {
		return assetlifecycle.CancellationPreview{}, err
	}

	preview := assetlifecycle.CancellationPreview{
		VPS:                     vps,
		Subscriptions:           buildSubscriptionImpacts(subscriptionRecords),
		MonitoringInstanceLinks: monitoringInstanceLinks,
		Services:                services,
		Domains:                 domains,
		TargetLinks:             targetLinks,
	}
	preview.RecommendedSteps = buildCancellationRecommendedSteps(preview)
	preview.Warnings, preview.Blockers = buildCancellationPreviewFindings(preview)
	return preview, nil
}

func (r *PostgresAssetLifecycleRepository) ApplyVPSCancellation(ctx context.Context, vpsID string, input assetlifecycle.ApplyCancellationInput) (assetlifecycle.LifecycleActionResult, error) {
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return assetlifecycle.LifecycleActionResult{}, fmt.Errorf("%w: vps_id is required", assetlifecycle.ErrInvalidLifecycleActionInput)
	}
	input = assetlifecycle.NormalizeApplyCancellationInput(input)
	if err := assetlifecycle.ValidateApplyCancellationInput(input); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, fmt.Errorf("begin asset lifecycle action transaction: %w", err)
	}
	txClosed := false
	defer func() {
		if !txClosed {
			_ = tx.Rollback(ctx)
		}
	}()
	rollbackForFailure := func() {
		if !txClosed {
			_ = tx.Rollback(ctx)
			txClosed = true
		}
	}
	var (
		currentVPS vpsassets.Record
		action     assetlifecycle.LifecycleActionRecord
	)
	recordFailedStep := func(completedSteps []assetlifecycle.LifecycleActionStep, objectType, objectID, stepType, message string, cause error) error {
		rollbackForFailure()
		return r.recordFailedVPSCancellation(ctx, currentVPS.VPSID, input, action, completedSteps, objectType, objectID, stepType, message, cause)
	}

	currentVPS, err = getLifecycleVPSAsset(ctx, tx, vpsID, true)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	if err := ensureVPSCancellationNotBlocked(currentVPS); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}

	action, err = insertLifecycleAction(ctx, tx, currentVPS.VPSID, input)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}

	steps := make([]assetlifecycle.LifecycleActionStep, 0, 1+len(input.SubscriptionIDs)+len(input.MonitoringInstanceActions)*2+len(input.TargetActions))
	addStep := func(step assetlifecycle.LifecycleActionStep, err error) error {
		if err != nil {
			return err
		}
		steps = append(steps, step)
		return nil
	}

	updatedVPS, err := applyVPSCancellationState(ctx, tx, action.ActionID, currentVPS, input)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, recordFailedStep(steps, assetlifecycle.ObjectTypeVPS, currentVPS.VPSID, assetlifecycle.StepTypeVPSLifecycle, "VPS 取消/退役状态写入失败。", err)
	}
	if err := addStep(updatedVPS, nil); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}

	for _, subscriptionID := range input.SubscriptionIDs {
		step, err := applySubscriptionCancellationState(ctx, tx, action.ActionID, currentVPS.VPSID, subscriptionID)
		if err != nil {
			return assetlifecycle.LifecycleActionResult{}, recordFailedStep(steps, assetlifecycle.ObjectTypeSubscription, subscriptionID, assetlifecycle.StepTypeSubscriptionStatus, "订阅取消状态写入失败。", err)
		}
		steps = append(steps, step)
	}
	for _, monitoringInstanceAction := range input.MonitoringInstanceActions {
		monitoringInstanceSteps, err := applyMonitoringInstanceLifecycleAction(ctx, tx, action.ActionID, currentVPS.VPSID, monitoringInstanceAction)
		if err != nil {
			stepType := assetlifecycle.StepTypeMonitoringInstanceLifecycle
			if monitoringInstanceAction.LifecycleStatus == "" && monitoringInstanceAction.MonitoringStatus != "" {
				stepType = assetlifecycle.StepTypeMonitoringInstanceMonitoring
			}
			return assetlifecycle.LifecycleActionResult{}, recordFailedStep(steps, assetlifecycle.ObjectTypeMonitoringInstance, monitoringInstanceAction.MonitoringInstanceID, stepType, "MonitoringInstance 生命周期或监控状态写入失败。", err)
		}
		steps = append(steps, monitoringInstanceSteps...)
	}
	for _, targetAction := range input.TargetActions {
		step, err := applyTargetLifecycleAction(ctx, tx, action.ActionID, currentVPS.VPSID, targetAction)
		if err != nil {
			return assetlifecycle.LifecycleActionResult{}, recordFailedStep(steps, assetlifecycle.ObjectTypeTarget, targetAction.TargetID, assetlifecycle.StepTypeTargetRunStatus, "Target/实例运行状态写入失败。", err)
		}
		steps = append(steps, step)
	}

	if err := tx.Commit(ctx); err != nil {
		txClosed = true
		return assetlifecycle.LifecycleActionResult{}, fmt.Errorf("commit asset lifecycle action %q: %w", action.ActionID, err)
	}
	txClosed = true
	return assetlifecycle.LifecycleActionResult{Action: action, Steps: steps}, nil
}

func ensureVPSCancellationNotBlocked(current vpsassets.Record) error {
	if current.LifecycleStatus == vpsassets.LifecycleArchived {
		return fmt.Errorf("%w: archived vps %q cannot be cancelled by lifecycle action", assetlifecycle.ErrLifecycleActionBlocked, current.VPSID)
	}
	return nil
}

func (r *PostgresAssetLifecycleRepository) recordFailedVPSCancellation(
	ctx context.Context,
	vpsID string,
	input assetlifecycle.ApplyCancellationInput,
	action assetlifecycle.LifecycleActionRecord,
	completedSteps []assetlifecycle.LifecycleActionStep,
	objectType string,
	objectID string,
	stepType string,
	message string,
	cause error,
) error {
	if auditErr := recordFailedLifecycleAction(ctx, r.db, vpsID, input, action, completedSteps, objectType, objectID, stepType, message, cause); auditErr != nil {
		return fmt.Errorf("%w: record failed asset lifecycle action audit: %w", cause, auditErr)
	}
	return cause
}

func (r *PostgresAssetLifecycleRepository) ListMonitoringInstanceAssetContexts(ctx context.Context) ([]assetlifecycle.AssetContextForMonitoringInstance, error) {
	rows, err := r.db.Query(ctx, `
		select
			l.monitoring_instance_id,
			v.vps_id,
			v.display_name,
			v.lifecycle_status,
			v.renewal_decision,
			coalesce((
				select s.status
				from subscriptions s
				where s.vps_id = v.vps_id
				order by
					case s.status
						when 'active' then 0
						when 'expired' then 1
						when 'cancelled' then 2
						when 'paused' then 3
						else 4
					end,
					s.renew_at desc nulls last,
					s.subscription_id
				limit 1
			), 'missing') as subscription_state
		from vps_monitoring_instance_links l
		join vps_assets v on v.vps_id = l.vps_id
		where l.unlinked_at is null
		order by l.monitoring_instance_id, lower(v.display_name), v.vps_id`)
	if err != nil {
		return nil, fmt.Errorf("query monitoring instance asset contexts: %w", err)
	}
	defer rows.Close()

	contexts := map[string]*assetlifecycle.AssetContextForMonitoringInstance{}
	order := []string{}
	for rows.Next() {
		var (
			monitoringInstanceID string
			summary              assetlifecycle.LinkedVPSContext
			lifecycleStatus      string
			renewalDecision      string
			subscriptionState    string
		)
		if err := rows.Scan(
			&monitoringInstanceID,
			&summary.VPSID,
			&summary.DisplayName,
			&lifecycleStatus,
			&renewalDecision,
			&subscriptionState,
		); err != nil {
			return nil, fmt.Errorf("scan monitoring instance asset context: %w", err)
		}
		summary.LifecycleStatus = vpsassets.LifecycleStatus(lifecycleStatus)
		summary.RenewalDecision = vpsassets.RenewalDecision(renewalDecision)
		summary.SubscriptionState = subscriptionState
		attention, message := linkedVPSCancellationContext(summary)
		summary.Message = message

		context, ok := contexts[monitoringInstanceID]
		if !ok {
			context = &assetlifecycle.AssetContextForMonitoringInstance{MonitoringInstanceID: monitoringInstanceID}
			contexts[monitoringInstanceID] = context
			order = append(order, monitoringInstanceID)
		}
		context.Summaries = append(context.Summaries, summary)
		context.LinkedVPSCount = len(context.Summaries)
		context.CancellationAttention = context.CancellationAttention || attention
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monitoring instance asset contexts: %w", err)
	}

	records := make([]assetlifecycle.AssetContextForMonitoringInstance, 0, len(order))
	for _, monitoringInstanceID := range order {
		records = append(records, *contexts[monitoringInstanceID])
	}
	return records, nil
}

func (r *PostgresAssetLifecycleRepository) ListTargetAssetContexts(ctx context.Context) ([]assetlifecycle.AssetContextForTarget, error) {
	rows, err := r.db.Query(ctx, `
		with target_assets as (
			select target_id, vps_id, service_id, null::text as domain_id
			from asset_services
			where target_id is not null
			union all
			select target_id, vps_id, null::text as service_id, domain_id
			from asset_domains
			where target_id is not null
		)
		select
			ta.target_id,
			ta.service_id,
			ta.domain_id,
			v.vps_id,
			v.display_name,
			v.lifecycle_status,
			v.renewal_decision,
			coalesce((
				select s.status
				from subscriptions s
				where s.vps_id = v.vps_id
				order by
					case s.status
						when 'active' then 0
						when 'expired' then 1
						when 'cancelled' then 2
						when 'paused' then 3
						else 4
					end,
					s.renew_at desc nulls last,
					s.subscription_id
				limit 1
			), 'missing') as subscription_state
		from target_assets ta
		join vps_assets v on v.vps_id = ta.vps_id
		order by ta.target_id, lower(v.display_name), v.vps_id`)
	if err != nil {
		return nil, fmt.Errorf("query target asset contexts: %w", err)
	}
	defer rows.Close()

	contexts := map[string]*assetlifecycle.AssetContextForTarget{}
	summaryIndexes := map[string]map[string]int{}
	order := []string{}
	for rows.Next() {
		var (
			targetID          string
			serviceID         *string
			domainID          *string
			summary           assetlifecycle.LinkedVPSContext
			lifecycleStatus   string
			renewalDecision   string
			subscriptionState string
		)
		if err := rows.Scan(
			&targetID,
			&serviceID,
			&domainID,
			&summary.VPSID,
			&summary.DisplayName,
			&lifecycleStatus,
			&renewalDecision,
			&subscriptionState,
		); err != nil {
			return nil, fmt.Errorf("scan target asset context: %w", err)
		}
		context, ok := contexts[targetID]
		if !ok {
			context = &assetlifecycle.AssetContextForTarget{TargetID: targetID}
			contexts[targetID] = context
			summaryIndexes[targetID] = map[string]int{}
			order = append(order, targetID)
		}
		if serviceID != nil {
			context.ServiceIDs = appendUniqueString(context.ServiceIDs, *serviceID)
		}
		if domainID != nil {
			context.DomainIDs = appendUniqueString(context.DomainIDs, *domainID)
		}

		index, exists := summaryIndexes[targetID][summary.VPSID]
		if !exists {
			summary.LifecycleStatus = vpsassets.LifecycleStatus(lifecycleStatus)
			summary.RenewalDecision = vpsassets.RenewalDecision(renewalDecision)
			summary.SubscriptionState = subscriptionState
			attention, message := linkedVPSCancellationContext(summary)
			summary.Message = message
			context.Summaries = append(context.Summaries, summary)
			index = len(context.Summaries) - 1
			summaryIndexes[targetID][summary.VPSID] = index
			context.CancellationAttention = context.CancellationAttention || attention
		}
		_ = index
		context.LinkedVPSCount = len(context.Summaries)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate target asset contexts: %w", err)
	}

	records := make([]assetlifecycle.AssetContextForTarget, 0, len(order))
	for _, targetID := range order {
		records = append(records, *contexts[targetID])
	}
	return records, nil
}

func getLifecycleVPSAsset(ctx context.Context, queryer assetLifecycleQueryer, vpsID string, forUpdate bool) (vpsassets.Record, error) {
	query := `
		select ` + vpsAssetSelectColumns + `
		from vps_assets
		where vps_id = $1`
	if forUpdate {
		query += `
		for update`
	}
	record, err := scanVPSAsset(queryer.QueryRow(ctx, query, vpsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsassets.Record{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("query vps asset %q for lifecycle action: %w", vpsID, err)
	}
	return record, nil
}

func listLifecycleSubscriptionsForVPS(ctx context.Context, queryer assetLifecycleQueryer, vpsID string, forUpdate bool) ([]subscriptions.Record, error) {
	query := `
		select ` + subscriptionSelectColumns + `
		from subscriptions
		where vps_id = $1
		order by
			case status
				when 'active' then 0
				when 'expired' then 1
				when 'cancelled' then 2
				when 'paused' then 3
				else 4
			end,
			renew_at asc nulls last,
			subscription_id`
	if forUpdate {
		query += `
		for update`
	}
	rows, err := queryer.Query(ctx, query, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions for vps lifecycle %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]subscriptions.Record, 0)
	for rows.Next() {
		record, err := scanSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("scan subscription for vps lifecycle %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions for vps lifecycle %q: %w", vpsID, err)
	}
	return records, nil
}

func listLifecycleMonitoringInstancesForVPS(ctx context.Context, queryer assetLifecycleQueryer, vpsID string) ([]assetlinks.MonitoringInstanceSummary, error) {
	rows, err := queryer.Query(ctx, `
		select
			n.monitoring_instance_id,
			n.display_name,
			n."group",
			n.region,
			n.city,
			n.provider,
			n.lifecycle_status,
			n.monitoring_status,
			n.binding_status,
			n.current_health_status,
			n.last_heartbeat_at,
			n.last_sync_at,
			n.current_active_incident_count,
			n.current_primary_issue_summary,
			l.linked_at,
			l.note
		from vps_monitoring_instance_links l
		join monitoring_instances n on n.monitoring_instance_id = l.monitoring_instance_id
		where l.vps_id = $1
		  and l.unlinked_at is null
		order by l.linked_at desc, n.display_name, n.monitoring_instance_id`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query active monitoring instances for vps lifecycle %q: %w", vpsID, err)
	}
	defer rows.Close()

	summaries := make([]assetlinks.MonitoringInstanceSummary, 0)
	for rows.Next() {
		var summary assetlinks.MonitoringInstanceSummary
		if err := rows.Scan(
			&summary.MonitoringInstanceID,
			&summary.DisplayName,
			&summary.Group,
			&summary.Region,
			&summary.City,
			&summary.Provider,
			&summary.LifecycleStatus,
			&summary.MonitoringStatus,
			&summary.BindingStatus,
			&summary.CurrentHealthStatus,
			&summary.LastHeartbeatAt,
			&summary.LastSyncAt,
			&summary.CurrentActiveIncidentCount,
			&summary.CurrentPrimaryIssueSummary,
			&summary.LinkedAt,
			&summary.Note,
		); err != nil {
			return nil, fmt.Errorf("scan active monitoring instance for vps lifecycle %q: %w", vpsID, err)
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active monitoring instances for vps lifecycle %q: %w", vpsID, err)
	}
	return summaries, nil
}

func listLifecycleAssetServicesForVPS(ctx context.Context, queryer assetLifecycleQueryer, vpsID string) ([]assetservices.Record, error) {
	rows, err := queryer.Query(ctx, `
		select `+assetServiceSelectColumns+`
		from asset_services
		where vps_id = $1
		order by lower(name), service_id`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query asset services for vps lifecycle %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]assetservices.Record, 0)
	for rows.Next() {
		record, err := scanAssetService(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset service for vps lifecycle %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset services for vps lifecycle %q: %w", vpsID, err)
	}
	return records, nil
}

func listLifecycleAssetDomainsForVPS(ctx context.Context, queryer assetLifecycleQueryer, vpsID string) ([]assetdomains.Record, error) {
	rows, err := queryer.Query(ctx, `
		select `+assetDomainSelectColumns+`
		from asset_domains
		where vps_id = $1
		order by lower(domain_name), domain_id`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query asset domains for vps lifecycle %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]assetdomains.Record, 0)
	for rows.Next() {
		record, err := scanAssetDomain(rows)
		if err != nil {
			return nil, fmt.Errorf("scan asset domain for vps lifecycle %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate asset domains for vps lifecycle %q: %w", vpsID, err)
	}
	return records, nil
}

func listLifecycleTargetImpacts(ctx context.Context, queryer assetLifecycleQueryer, services []assetservices.Record, domains []assetdomains.Record) ([]assetlifecycle.TargetImpact, error) {
	targetIDs := make([]string, 0)
	impactsByID := map[string]*assetlifecycle.TargetImpact{}
	for _, service := range services {
		if service.TargetID == nil {
			continue
		}
		impact := ensureTargetImpact(impactsByID, *service.TargetID)
		impact.ServiceIDs = appendUniqueString(impact.ServiceIDs, service.ServiceID)
		if impact.LastLinkedAt == nil || service.UpdatedAt.After(*impact.LastLinkedAt) {
			linkedAt := service.UpdatedAt
			impact.LastLinkedAt = &linkedAt
		}
		targetIDs = appendUniqueString(targetIDs, *service.TargetID)
	}
	for _, domain := range domains {
		if domain.TargetID == nil {
			continue
		}
		impact := ensureTargetImpact(impactsByID, *domain.TargetID)
		impact.DomainIDs = appendUniqueString(impact.DomainIDs, domain.DomainID)
		if impact.LastLinkedAt == nil || domain.UpdatedAt.After(*impact.LastLinkedAt) {
			linkedAt := domain.UpdatedAt
			impact.LastLinkedAt = &linkedAt
		}
		targetIDs = appendUniqueString(targetIDs, *domain.TargetID)
	}
	if len(targetIDs) == 0 {
		return []assetlifecycle.TargetImpact{}, nil
	}

	rows, err := queryer.Query(ctx, `
		select target_id, name, run_status
		from targets
		where target_id = any($1::text[])
		order by lower(name), target_id`, targetIDs)
	if err != nil {
		return nil, fmt.Errorf("query targets for vps lifecycle impact: %w", err)
	}
	defer rows.Close()
	order := make([]string, 0, len(targetIDs))
	for rows.Next() {
		var targetID string
		impact := assetlifecycle.TargetImpact{}
		if err := rows.Scan(&targetID, &impact.Name, &impact.RunStatus); err != nil {
			return nil, fmt.Errorf("scan target for vps lifecycle impact: %w", err)
		}
		stored := ensureTargetImpact(impactsByID, targetID)
		stored.Name = impact.Name
		stored.RunStatus = impact.RunStatus
		order = append(order, targetID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate targets for vps lifecycle impact: %w", err)
	}

	impacts := make([]assetlifecycle.TargetImpact, 0, len(order))
	for _, targetID := range order {
		impacts = append(impacts, *impactsByID[targetID])
	}
	return impacts, nil
}

func ensureTargetImpact(impacts map[string]*assetlifecycle.TargetImpact, targetID string) *assetlifecycle.TargetImpact {
	impact, ok := impacts[targetID]
	if !ok {
		impact = &assetlifecycle.TargetImpact{TargetID: targetID}
		impacts[targetID] = impact
	}
	return impact
}

func buildSubscriptionImpacts(records []subscriptions.Record) []assetlifecycle.SubscriptionImpact {
	impacts := make([]assetlifecycle.SubscriptionImpact, 0, len(records))
	for _, record := range records {
		impact := assetlifecycle.SubscriptionImpact{Record: record}
		switch record.Status {
		case subscriptions.StatusActive:
			impact.Role = "active"
			impact.RecommendedAction = "cancel_auto_renew_and_mark_cancelled"
			impact.Message = "订阅账单记录仍显示自动续费有效，需要显式确认取消自动续费。"
		case subscriptions.StatusExpired, subscriptions.StatusCancelled, subscriptions.StatusPaused:
			impact.Role = "inactive"
			impact.RecommendedAction = "keep_inactive"
			impact.Message = "订阅账单记录已无续费动作，仍需处理 VPS、MonitoringInstance 与入口探测状态。"
		case subscriptions.StatusUnknown:
			impact.Role = "inactive"
			impact.RecommendedAction = "review_inactive"
			impact.Message = "订阅账单记录缺少明确自动续费事实，仍需处理 VPS、MonitoringInstance 与入口探测状态。"
		default:
			impact.Role = "attention"
			impact.RecommendedAction = "review_before_cancel"
			impact.Message = "订阅账单记录不是明确可续费事实，请在执行前确认自动续费与支付记录。"
		}
		impacts = append(impacts, impact)
	}
	return impacts
}

func buildCancellationRecommendedSteps(preview assetlifecycle.CancellationPreview) []assetlifecycle.RecommendedLifecycleStep {
	steps := make([]assetlifecycle.RecommendedLifecycleStep, 0)
	recommendedVPSLifecycle := recommendedVPSCancellationLifecycle(preview)
	if preview.VPS.LifecycleStatus != recommendedVPSLifecycle || preview.VPS.RenewalDecision != vpsassets.RenewalCancel {
		steps = append(steps, assetlifecycle.RecommendedLifecycleStep{
			ObjectType: assetlifecycle.ObjectTypeVPS,
			ObjectID:   preview.VPS.VPSID,
			StepType:   assetlifecycle.StepTypeVPSLifecycle,
			FromState:  string(preview.VPS.LifecycleStatus) + "/" + string(preview.VPS.RenewalDecision),
			ToState:    string(recommendedVPSLifecycle) + "/" + string(vpsassets.RenewalCancel),
			Required:   true,
			Message:    "将 VPS 续费决策设为 cancel，并根据订阅到期情况设置生命周期。",
		})
	}
	for _, impact := range preview.Subscriptions {
		if impact.Record.Status != subscriptions.StatusActive {
			continue
		}
		steps = append(steps, assetlifecycle.RecommendedLifecycleStep{
			ObjectType: assetlifecycle.ObjectTypeSubscription,
			ObjectID:   impact.Record.SubscriptionID,
			StepType:   assetlifecycle.StepTypeSubscriptionStatus,
			FromState:  string(impact.Record.Status),
			ToState:    string(subscriptions.StatusCancelled),
			Required:   true,
			Message:    "取消订阅自动续费，并将 active 订阅标记为 cancelled。",
		})
	}
	for _, link := range preview.MonitoringInstanceLinks {
		targetLifecycle := monitoringinstances.LifecycleNoRenewal
		if recommendedVPSLifecycle == vpsassets.LifecycleCancelled {
			targetLifecycle = monitoringinstances.LifecycleRetired
		}
		if link.LifecycleStatus != targetLifecycle {
			steps = append(steps, assetlifecycle.RecommendedLifecycleStep{
				ObjectType: assetlifecycle.ObjectTypeMonitoringInstance,
				ObjectID:   link.MonitoringInstanceID,
				StepType:   assetlifecycle.StepTypeMonitoringInstanceLifecycle,
				FromState:  link.LifecycleStatus,
				ToState:    targetLifecycle,
				Required:   false,
				Message:    "关联监控实例需要由用户确认后标记不续费或已退役。",
			})
		}
		if recommendedVPSLifecycle == vpsassets.LifecycleCancelled && link.MonitoringStatus != monitoringinstances.MonitoringPaused {
			steps = append(steps, assetlifecycle.RecommendedLifecycleStep{
				ObjectType: assetlifecycle.ObjectTypeMonitoringInstance,
				ObjectID:   link.MonitoringInstanceID,
				StepType:   assetlifecycle.StepTypeMonitoringInstanceMonitoring,
				FromState:  link.MonitoringStatus,
				ToState:    monitoringinstances.MonitoringPaused,
				Required:   false,
				Message:    "已实际退役的监控实例可在确认后暂停监控。",
			})
		}
	}
	for _, target := range preview.TargetLinks {
		if target.RunStatus == targets.RunStatusArchived {
			continue
		}
		steps = append(steps, assetlifecycle.RecommendedLifecycleStep{
			ObjectType: assetlifecycle.ObjectTypeTarget,
			ObjectID:   target.TargetID,
			StepType:   assetlifecycle.StepTypeTargetRunStatus,
			FromState:  target.RunStatus,
			ToState:    targets.RunStatusArchived,
			Required:   false,
			Message:    "随 VPS 下线的 Target/实例应由用户确认后归档。",
		})
	}
	return steps
}

func buildCancellationPreviewFindings(preview assetlifecycle.CancellationPreview) ([]string, []string) {
	warnings := make([]string, 0)
	blockers := make([]string, 0)

	activeSubscriptions := 0
	inactiveSubscriptions := 0
	for _, impact := range preview.Subscriptions {
		switch {
		case impact.Record.Status == subscriptions.StatusActive:
			activeSubscriptions++
		case isInactiveSubscriptionEvidence(impact.Record.Status):
			inactiveSubscriptions++
		}
	}

	if len(preview.Subscriptions) == 0 {
		warnings = append(warnings, "没有找到关联订阅；仍可继续处理 VPS、MonitoringInstance 与实例生命周期。")
	}
	if activeSubscriptions == 0 && inactiveSubscriptions > 0 {
		warnings = append(warnings, "关联订阅账单记录已无续费动作；这不是“没有关联订阅”，仍需处理 VPS、MonitoringInstance 与入口探测状态。")
	}
	if activeSubscriptions > 1 {
		warnings = append(warnings, "存在多条 active 订阅，执行取消时必须显式选择要处理的订阅。")
	}
	if inactiveSubscriptions > 0 && preview.VPS.LifecycleStatus != vpsassets.LifecycleCancelled && preview.VPS.LifecycleStatus != vpsassets.LifecycleToCancel {
		warnings = append(warnings, "订阅账单记录已无续费动作，但 VPS 尚未进入 to_cancel/cancelled，存在状态割裂。")
	}
	if preview.VPS.LifecycleStatus == vpsassets.LifecycleArchived {
		blockers = append(blockers, "VPS 已归档，普通取消/退役动作不应再修改归档资产。")
	}

	runningMonitoringInstances := 0
	for _, link := range preview.MonitoringInstanceLinks {
		if link.LifecycleStatus != monitoringinstances.LifecycleNoRenewal && link.LifecycleStatus != monitoringinstances.LifecycleRetired {
			runningMonitoringInstances++
		}
	}
	if runningMonitoringInstances > 0 {
		warnings = append(warnings, fmt.Sprintf("仍有 %d 个关联 MonitoringInstance 未标记不续费或已退役。", runningMonitoringInstances))
	}

	runningTargets := 0
	for _, target := range preview.TargetLinks {
		if target.RunStatus != targets.RunStatusArchived && target.RunStatus != targets.RunStatusPaused {
			runningTargets++
		}
	}
	if runningTargets > 0 {
		warnings = append(warnings, fmt.Sprintf("仍有 %d 个关联 Target/实例处于运行或维护状态。", runningTargets))
	}

	return warnings, blockers
}

func recommendedVPSCancellationLifecycle(preview assetlifecycle.CancellationPreview) vpsassets.LifecycleStatus {
	if preview.VPS.LifecycleStatus == vpsassets.LifecycleCancelled || preview.VPS.LifecycleStatus == vpsassets.LifecycleToCancel {
		return preview.VPS.LifecycleStatus
	}
	today := subscriptions.NewDate(time.Now().UTC())
	for _, impact := range preview.Subscriptions {
		if impact.Record.Status == subscriptions.StatusExpired || impact.Record.Status == subscriptions.StatusCancelled {
			return vpsassets.LifecycleCancelled
		}
		if impact.Record.RenewAt != nil && !impact.Record.RenewAt.Time.After(today.Time) {
			return vpsassets.LifecycleCancelled
		}
	}
	return vpsassets.LifecycleToCancel
}

func insertLifecycleAction(ctx context.Context, tx pgx.Tx, vpsID string, input assetlifecycle.ApplyCancellationInput) (assetlifecycle.LifecycleActionRecord, error) {
	actionID, err := ids.New("ala")
	if err != nil {
		return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("generate asset lifecycle action id: %w", err)
	}
	now := time.Now().UTC()
	summary := map[string]any{
		"subscription_count":               len(input.SubscriptionIDs),
		"monitoring_instance_action_count": len(input.MonitoringInstanceActions),
		"target_action_count":              len(input.TargetActions),
		"vps_lifecycle_status":             string(input.VPSLifecycleStatus),
		"renewal_decision":                 string(vpsassets.RenewalCancel),
		"confirmed_by_workbench":           true,
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("marshal asset lifecycle action summary: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into asset_lifecycle_actions (
			action_id,
			vps_id,
			action_type,
			status,
			reason,
			effective_date,
			summary,
			created_at,
			confirmed_at,
			completed_at
		) values ($1,$2,$3,$4,$5,$6::date,$7::jsonb,$8,$8,$8)`,
		actionID,
		vpsID,
		string(assetlifecycle.ActionTypeCancelVPS),
		assetlifecycle.ActionStatusCompleted,
		input.Reason,
		subscriptionDateArg(input.EffectiveDate),
		summaryJSON,
		now,
	); err != nil {
		if isLifecycleActionInvalidPostgresError(err) {
			return assetlifecycle.LifecycleActionRecord{}, assetlifecycle.ErrInvalidLifecycleActionInput
		}
		return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("insert asset lifecycle action for vps %q: %w", vpsID, err)
	}

	return assetlifecycle.LifecycleActionRecord{
		ActionID:      actionID,
		VPSID:         vpsID,
		ActionType:    assetlifecycle.ActionTypeCancelVPS,
		Status:        assetlifecycle.ActionStatusCompleted,
		Reason:        input.Reason,
		EffectiveDate: cloneSubscriptionDate(input.EffectiveDate),
		CreatedAt:     now,
		ConfirmedAt:   &now,
		CompletedAt:   &now,
		Summary:       summary,
	}, nil
}

func recordFailedLifecycleAction(
	ctx context.Context,
	db assetLifecycleDB,
	vpsID string,
	input assetlifecycle.ApplyCancellationInput,
	attemptedAction assetlifecycle.LifecycleActionRecord,
	completedSteps []assetlifecycle.LifecycleActionStep,
	objectType string,
	objectID string,
	stepType string,
	message string,
	cause error,
) error {
	failureTx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin failed asset lifecycle action audit transaction: %w", err)
	}
	defer func() { _ = failureTx.Rollback(ctx) }()

	failedAction, err := insertFailedLifecycleAction(ctx, failureTx, vpsID, input, attemptedAction, completedSteps, cause)
	if err != nil {
		return err
	}
	beforeState := map[string]any{
		"completed_step_count": len(completedSteps),
	}
	afterState := map[string]any{
		"error": cause.Error(),
	}
	if objectID == "" {
		objectID = vpsID
	}
	if message == "" {
		message = "生命周期动作执行失败。"
	}
	if _, err := insertLifecycleStep(ctx, failureTx, failedAction.ActionID, objectType, objectID, stepType, assetlifecycle.StepStatusFailed, beforeState, afterState, message+" "+cause.Error()); err != nil {
		return err
	}
	if err := failureTx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failed asset lifecycle action audit %q: %w", failedAction.ActionID, err)
	}
	return nil
}

func insertFailedLifecycleAction(
	ctx context.Context,
	tx pgx.Tx,
	vpsID string,
	input assetlifecycle.ApplyCancellationInput,
	attemptedAction assetlifecycle.LifecycleActionRecord,
	completedSteps []assetlifecycle.LifecycleActionStep,
	cause error,
) (assetlifecycle.LifecycleActionRecord, error) {
	actionID := attemptedAction.ActionID
	if strings.TrimSpace(actionID) == "" {
		var err error
		actionID, err = ids.New("ala")
		if err != nil {
			return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("generate failed asset lifecycle action id: %w", err)
		}
	}
	now := time.Now().UTC()
	summary := map[string]any{
		"subscription_count":               len(input.SubscriptionIDs),
		"monitoring_instance_action_count": len(input.MonitoringInstanceActions),
		"target_action_count":              len(input.TargetActions),
		"vps_lifecycle_status":             string(input.VPSLifecycleStatus),
		"renewal_decision":                 string(vpsassets.RenewalCancel),
		"confirmed_by_workbench":           true,
		"completed_step_count":             len(completedSteps),
		"failure_reason":                   cause.Error(),
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("marshal failed asset lifecycle action summary: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into asset_lifecycle_actions (
			action_id,
			vps_id,
			action_type,
			status,
			reason,
			effective_date,
			summary,
			created_at,
			confirmed_at,
			completed_at
		) values ($1,$2,$3,$4,$5,$6::date,$7::jsonb,$8,$8,$8)`,
		actionID,
		vpsID,
		string(assetlifecycle.ActionTypeCancelVPS),
		assetlifecycle.ActionStatusFailed,
		input.Reason,
		subscriptionDateArg(input.EffectiveDate),
		summaryJSON,
		now,
	); err != nil {
		if isLifecycleActionInvalidPostgresError(err) {
			return assetlifecycle.LifecycleActionRecord{}, assetlifecycle.ErrInvalidLifecycleActionInput
		}
		return assetlifecycle.LifecycleActionRecord{}, fmt.Errorf("insert failed asset lifecycle action for vps %q: %w", vpsID, err)
	}

	return assetlifecycle.LifecycleActionRecord{
		ActionID:      actionID,
		VPSID:         vpsID,
		ActionType:    assetlifecycle.ActionTypeCancelVPS,
		Status:        assetlifecycle.ActionStatusFailed,
		Reason:        input.Reason,
		EffectiveDate: cloneSubscriptionDate(input.EffectiveDate),
		CreatedAt:     now,
		ConfirmedAt:   &now,
		CompletedAt:   &now,
		Summary:       summary,
	}, nil
}

func applyVPSCancellationState(ctx context.Context, tx pgx.Tx, actionID string, current vpsassets.Record, input assetlifecycle.ApplyCancellationInput) (assetlifecycle.LifecycleActionStep, error) {
	before := map[string]any{
		"lifecycle_status": string(current.LifecycleStatus),
		"renewal_decision": string(current.RenewalDecision),
	}
	patch := vpsassets.PatchInput{}
	if current.LifecycleStatus != input.VPSLifecycleStatus {
		patch.LifecycleStatus = vpsassets.PatchLifecycle(input.VPSLifecycleStatus)
	}
	if current.RenewalDecision != vpsassets.RenewalCancel {
		patch.RenewalDecision = vpsassets.PatchRenewal(vpsassets.RenewalCancel)
		patch.RenewalReason = vpsassets.PatchString(input.Reason)
	}

	if !patch.HasChanges() {
		return insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeVPS, current.VPSID, assetlifecycle.StepTypeVPSLifecycle, assetlifecycle.StepStatusSkipped, before, before, "VPS 已处于确认的取消状态。")
	}
	patch = vpsassets.NormalizePatchInput(patch)
	if err := vpsassets.ValidatePatchInput(patch); err != nil {
		return assetlifecycle.LifecycleActionStep{}, err
	}

	updated, err := patchVPSAssetRow(ctx, tx, current.VPSID, patch)
	if errors.Is(err, pgx.ErrNoRows) {
		return assetlifecycle.LifecycleActionStep{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		if isVPSAssetInvalidPostgresError(err) {
			return assetlifecycle.LifecycleActionStep{}, vpsassets.ErrInvalidVPSAssetInput
		}
		return assetlifecycle.LifecycleActionStep{}, fmt.Errorf("patch vps %q for lifecycle action: %w", current.VPSID, err)
	}
	if err := recordVPSAssetHistoryChanges(ctx, tx, current, updated, patch); err != nil {
		return assetlifecycle.LifecycleActionStep{}, err
	}
	after := map[string]any{
		"lifecycle_status": string(updated.LifecycleStatus),
		"renewal_decision": string(updated.RenewalDecision),
	}
	return insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeVPS, current.VPSID, assetlifecycle.StepTypeVPSLifecycle, assetlifecycle.StepStatusCompleted, before, after, "VPS 取消/退役状态已确认。")
}

func applySubscriptionCancellationState(ctx context.Context, tx pgx.Tx, actionID, vpsID, subscriptionID string) (assetlifecycle.LifecycleActionStep, error) {
	current, err := lockSubscriptionForLifecycleAction(ctx, tx, vpsID, subscriptionID)
	if err != nil {
		return assetlifecycle.LifecycleActionStep{}, err
	}
	before := subscriptionLifecycleState(current)

	patch := subscriptions.PatchInput{}
	switch current.Status {
	case subscriptions.StatusExpired, subscriptions.StatusCancelled:
	default:
		patch.Status = subscriptions.PatchStatus(subscriptions.StatusCancelled)
	}
	if current.AutoRenew {
		patch.AutoRenew = subscriptions.PatchBool(false)
	}
	if !current.AutoRenewCancelled {
		patch.AutoRenewCancelled = subscriptions.PatchBool(true)
	}

	if !patch.HasChanges() {
		return insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeSubscription, current.SubscriptionID, assetlifecycle.StepTypeSubscriptionStatus, assetlifecycle.StepStatusSkipped, before, before, "订阅已处于非活跃或取消自动续费状态。")
	}
	patch = subscriptions.NormalizePatchInput(patch)
	if err := subscriptions.ValidatePatchInput(patch); err != nil {
		return assetlifecycle.LifecycleActionStep{}, err
	}
	updated, err := patchSubscriptionRow(ctx, tx, current.SubscriptionID, patch)
	if errors.Is(err, pgx.ErrNoRows) {
		return assetlifecycle.LifecycleActionStep{}, subscriptions.ErrSubscriptionNotFound
	}
	if err != nil {
		if isSubscriptionInvalidPostgresError(err) {
			return assetlifecycle.LifecycleActionStep{}, subscriptions.ErrInvalidSubscriptionInput
		}
		return assetlifecycle.LifecycleActionStep{}, fmt.Errorf("patch subscription %q for lifecycle action: %w", current.SubscriptionID, err)
	}
	if subscriptionPriceHistoryChanged(current, updated) {
		if _, err := createPriceHistory(ctx, tx, renewals.CreatePriceHistoryInput{
			From: current,
			To:   updated,
		}); err != nil {
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) || errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				return assetlifecycle.LifecycleActionStep{}, subscriptions.ErrInvalidSubscriptionInput
			}
			return assetlifecycle.LifecycleActionStep{}, fmt.Errorf("record price history for subscription %q: %w", current.SubscriptionID, err)
		}
	}
	return insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeSubscription, current.SubscriptionID, assetlifecycle.StepTypeSubscriptionStatus, assetlifecycle.StepStatusCompleted, before, subscriptionLifecycleState(updated), "订阅取消状态已确认。")
}

func lockSubscriptionForLifecycleAction(ctx context.Context, tx pgx.Tx, vpsID, subscriptionID string) (subscriptions.Record, error) {
	record, err := scanSubscription(tx.QueryRow(ctx, `
		select `+subscriptionSelectColumns+`
		from subscriptions
		where vps_id = $1
		  and subscription_id = $2
		for update`, vpsID, subscriptionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return subscriptions.Record{}, fmt.Errorf("%w: subscription %q is not associated with vps %q", assetlifecycle.ErrInvalidLifecycleActionInput, subscriptionID, vpsID)
	}
	if err != nil {
		return subscriptions.Record{}, fmt.Errorf("lock subscription %q for lifecycle action: %w", subscriptionID, err)
	}
	return record, nil
}

func applyMonitoringInstanceLifecycleAction(ctx context.Context, tx pgx.Tx, actionID, vpsID string, input assetlifecycle.MonitoringInstanceActionInput) ([]assetlifecycle.LifecycleActionStep, error) {
	current, err := lockMonitoringInstanceForLifecycleAction(ctx, tx, vpsID, input.MonitoringInstanceID)
	if err != nil {
		return nil, err
	}
	steps := make([]assetlifecycle.LifecycleActionStep, 0, 2)
	if input.LifecycleStatus != "" {
		step, updated, err := applyMonitoringInstanceLifecycleStatus(ctx, tx, actionID, current, input.LifecycleStatus)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
		current = updated
	}
	if input.MonitoringStatus != "" {
		step, updated, err := applyMonitoringInstanceMonitoringStatus(ctx, tx, actionID, current, input.MonitoringStatus)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
		current = updated
	}
	return steps, nil
}

func lockMonitoringInstanceForLifecycleAction(ctx context.Context, tx pgx.Tx, vpsID, monitoringInstanceID string) (monitoringinstances.Record, error) {
	record, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		select `+qualifiedMonitoringInstanceSelectColumns("n")+`
		from vps_monitoring_instance_links l
		join monitoring_instances n on n.monitoring_instance_id = l.monitoring_instance_id
		where l.vps_id = $1
		  and l.monitoring_instance_id = $2
		  and l.unlinked_at is null
		for update of n`, vpsID, monitoringInstanceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoringinstances.Record{}, fmt.Errorf("%w: monitoring instance %q is not actively linked to vps %q", assetlifecycle.ErrInvalidLifecycleActionInput, monitoringInstanceID, vpsID)
	}
	if err != nil {
		return monitoringinstances.Record{}, fmt.Errorf("lock monitoring instance %q for lifecycle action: %w", monitoringInstanceID, err)
	}
	return record, nil
}

func applyMonitoringInstanceLifecycleStatus(ctx context.Context, tx pgx.Tx, actionID string, current monitoringinstances.Record, nextStatus string) (assetlifecycle.LifecycleActionStep, monitoringinstances.Record, error) {
	before := map[string]any{"lifecycle_status": current.LifecycleStatus}
	if current.LifecycleStatus == nextStatus {
		step, err := insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeMonitoringInstance, current.MonitoringInstanceID, assetlifecycle.StepTypeMonitoringInstanceLifecycle, assetlifecycle.StepStatusSkipped, before, before, "监控实例生命周期已处于确认状态。")
		return step, current, err
	}

	updated, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set lifecycle_status = $2,
		    updated_at = now()
		where monitoring_instance_id = $1
		returning `+monitoringInstanceSelectColumns,
		current.MonitoringInstanceID,
		nextStatus,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetlifecycle.LifecycleActionStep{}, monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return assetlifecycle.LifecycleActionStep{}, monitoringinstances.Record{}, fmt.Errorf("update monitoring instance %q lifecycle for asset lifecycle action: %w", current.MonitoringInstanceID, err)
	}
	eventType, summary := monitoringInstanceLifecycleEventForStatus(nextStatus)
	if err := insertMonitoringInstanceLifecycleEvent(ctx, tx, updated, eventType, summary); err != nil {
		return assetlifecycle.LifecycleActionStep{}, monitoringinstances.Record{}, err
	}
	after := map[string]any{"lifecycle_status": updated.LifecycleStatus}
	step, err := insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeMonitoringInstance, current.MonitoringInstanceID, assetlifecycle.StepTypeMonitoringInstanceLifecycle, assetlifecycle.StepStatusCompleted, before, after, "监控实例生命周期状态已确认。")
	return step, updated, err
}

func applyMonitoringInstanceMonitoringStatus(ctx context.Context, tx pgx.Tx, actionID string, current monitoringinstances.Record, nextStatus string) (assetlifecycle.LifecycleActionStep, monitoringinstances.Record, error) {
	before := map[string]any{"monitoring_status": current.MonitoringStatus}
	if current.MonitoringStatus == nextStatus {
		step, err := insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeMonitoringInstance, current.MonitoringInstanceID, assetlifecycle.StepTypeMonitoringInstanceMonitoring, assetlifecycle.StepStatusSkipped, before, before, "监控实例监控状态已处于确认状态。")
		return step, current, err
	}

	updated, err := scanMonitoringInstance(tx.QueryRow(ctx, `
		update monitoring_instances
		set monitoring_status = $2,
		    updated_at = now()
		where monitoring_instance_id = $1
		returning `+monitoringInstanceSelectColumns,
		current.MonitoringInstanceID,
		nextStatus,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetlifecycle.LifecycleActionStep{}, monitoringinstances.Record{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return assetlifecycle.LifecycleActionStep{}, monitoringinstances.Record{}, fmt.Errorf("update monitoring instance %q monitoring for asset lifecycle action: %w", current.MonitoringInstanceID, err)
	}
	eventType, summary := monitoringInstanceMonitoringEventForStatus(current.MonitoringStatus, nextStatus)
	if err := insertMonitoringInstanceRuntimeEvent(ctx, tx, updated, eventType, summary); err != nil {
		return assetlifecycle.LifecycleActionStep{}, monitoringinstances.Record{}, err
	}
	after := map[string]any{"monitoring_status": updated.MonitoringStatus}
	step, err := insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeMonitoringInstance, current.MonitoringInstanceID, assetlifecycle.StepTypeMonitoringInstanceMonitoring, assetlifecycle.StepStatusCompleted, before, after, "监控实例监控状态已确认。")
	return step, updated, err
}

func monitoringInstanceLifecycleEventForStatus(status string) (incidents.EventType, string) {
	switch status {
	case monitoringinstances.LifecycleRetired:
		return incidents.EventMonitoringInstanceRetired, "监控实例已退役并退出活跃观测集，历史记录保留"
	case monitoringinstances.LifecycleObserving:
		return incidents.EventMonitoringInstanceRestoredToObserving, "监控实例已恢复到观察中"
	default:
		return incidents.EventMonitoringInstanceLifecycleUpdated, fmt.Sprintf("监控实例生命周期已更新为%s", status)
	}
}

func monitoringInstanceMonitoringEventForStatus(previousStatus, nextStatus string) (incidents.EventType, string) {
	switch nextStatus {
	case monitoringinstances.MonitoringMaintenance:
		return incidents.EventMonitoringInstanceMonitoringMaintenanceEntered, "监控实例已进入维护"
	case monitoringinstances.MonitoringPaused:
		return incidents.EventMonitoringInstanceMonitoringPaused, "监控实例监控已暂停"
	default:
		if previousStatus == monitoringinstances.MonitoringMaintenance {
			return incidents.EventMonitoringInstanceMonitoringMaintenanceExited, "监控实例已退出维护"
		}
		return incidents.EventMonitoringInstanceMonitoringResumed, "监控实例监控已恢复"
	}
}

func applyTargetLifecycleAction(ctx context.Context, tx pgx.Tx, actionID, vpsID string, input assetlifecycle.TargetActionInput) (assetlifecycle.LifecycleActionStep, error) {
	current, err := lockTargetForLifecycleAction(ctx, tx, vpsID, input.TargetID)
	if err != nil {
		return assetlifecycle.LifecycleActionStep{}, err
	}
	before := map[string]any{"run_status": current.RunStatus}
	if current.RunStatus == input.RunStatus {
		return insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeTarget, current.TargetID, assetlifecycle.StepTypeTargetRunStatus, assetlifecycle.StepStatusSkipped, before, before, "Target/实例运行状态已处于确认状态。")
	}

	updated, err := scanTarget(tx.QueryRow(ctx, `
		update targets
		set run_status = $2,
		    updated_at = now()
		where target_id = $1
		returning `+targetSelectColumns,
		current.TargetID,
		input.RunStatus,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return assetlifecycle.LifecycleActionStep{}, targets.ErrTargetNotFound
	}
	if err != nil {
		return assetlifecycle.LifecycleActionStep{}, fmt.Errorf("update target %q for asset lifecycle action: %w", current.TargetID, err)
	}
	eventType, summary := targetRuntimeEventForStatus(current.RunStatus, input.RunStatus)
	if err := insertTargetRuntimeEvent(ctx, tx, updated, eventType, summary); err != nil {
		return assetlifecycle.LifecycleActionStep{}, err
	}
	after := map[string]any{"run_status": updated.RunStatus}
	return insertLifecycleStep(ctx, tx, actionID, assetlifecycle.ObjectTypeTarget, current.TargetID, assetlifecycle.StepTypeTargetRunStatus, assetlifecycle.StepStatusCompleted, before, after, "Target/实例运行状态已确认。")
}

func lockTargetForLifecycleAction(ctx context.Context, tx pgx.Tx, vpsID, targetID string) (targets.TargetRecord, error) {
	record, err := scanTarget(tx.QueryRow(ctx, `
		select `+qualifiedTargetSelectColumns("t")+`
		from targets t
		where t.target_id = $2
		  and (
			exists (
				select 1
				from asset_services s
				where s.vps_id = $1
				  and s.target_id = t.target_id
			)
			or exists (
				select 1
				from asset_domains d
				where d.vps_id = $1
				  and d.target_id = t.target_id
			)
		  )
		for update of t`, vpsID, targetID))
	if errors.Is(err, pgx.ErrNoRows) {
		return targets.TargetRecord{}, fmt.Errorf("%w: target %q is not associated with vps %q", assetlifecycle.ErrInvalidLifecycleActionInput, targetID, vpsID)
	}
	if err != nil {
		return targets.TargetRecord{}, fmt.Errorf("lock target %q for lifecycle action: %w", targetID, err)
	}
	return record, nil
}

func targetRuntimeEventForStatus(previousStatus, nextStatus string) (incidents.EventType, string) {
	switch nextStatus {
	case targets.RunStatusMaintenance:
		return incidents.EventTargetMaintenanceEntered, "目标运行已进入维护"
	case targets.RunStatusPaused:
		return incidents.EventTargetPaused, "目标运行已暂停"
	case targets.RunStatusArchived:
		return incidents.EventTargetArchived, "目标已归档"
	default:
		if previousStatus == targets.RunStatusMaintenance {
			return incidents.EventTargetMaintenanceExited, "目标运行已退出维护"
		}
		return incidents.EventTargetResumed, "目标运行已恢复"
	}
}

func insertLifecycleStep(ctx context.Context, tx pgx.Tx, actionID, objectType, objectID, stepType, status string, beforeState, afterState map[string]any, message string) (assetlifecycle.LifecycleActionStep, error) {
	stepID, err := ids.New("als")
	if err != nil {
		return assetlifecycle.LifecycleActionStep{}, fmt.Errorf("generate asset lifecycle action step id: %w", err)
	}
	now := time.Now().UTC()
	var executedAt *time.Time
	if status != assetlifecycle.StepStatusSkipped {
		executedAt = &now
	}
	beforeJSON, err := json.Marshal(beforeState)
	if err != nil {
		return assetlifecycle.LifecycleActionStep{}, fmt.Errorf("marshal lifecycle step before state: %w", err)
	}
	afterJSON, err := json.Marshal(afterState)
	if err != nil {
		return assetlifecycle.LifecycleActionStep{}, fmt.Errorf("marshal lifecycle step after state: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into asset_lifecycle_action_steps (
			step_id,
			action_id,
			object_type,
			object_id,
			step_type,
			status,
			before_state,
			after_state,
			message,
			executed_at,
			created_at
		) values ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,$10,$11)`,
		stepID,
		actionID,
		objectType,
		objectID,
		stepType,
		status,
		beforeJSON,
		afterJSON,
		message,
		executedAt,
		now,
	); err != nil {
		if isLifecycleActionInvalidPostgresError(err) {
			return assetlifecycle.LifecycleActionStep{}, assetlifecycle.ErrInvalidLifecycleActionInput
		}
		return assetlifecycle.LifecycleActionStep{}, fmt.Errorf("insert asset lifecycle step %q for %s %q: %w", stepType, objectType, objectID, err)
	}
	return assetlifecycle.LifecycleActionStep{
		StepID:      stepID,
		ActionID:    actionID,
		ObjectType:  objectType,
		ObjectID:    objectID,
		StepType:    stepType,
		Status:      status,
		BeforeState: beforeState,
		AfterState:  afterState,
		Message:     message,
		ExecutedAt:  executedAt,
		CreatedAt:   now,
	}, nil
}

func subscriptionLifecycleState(record subscriptions.Record) map[string]any {
	return map[string]any{
		"status":                 string(record.Status),
		"auto_renew":             record.AutoRenew,
		"auto_renew_cancelled":   record.AutoRenewCancelled,
		"renew_at":               subscriptionDateState(record.RenewAt),
		"monthly_price":          record.MonthlyPrice,
		"currency":               record.Currency,
		"subscription_record_id": record.SubscriptionID,
	}
}

func subscriptionDateState(value *subscriptions.Date) any {
	if value == nil {
		return nil
	}
	return value.Time.Format(subscriptions.DateLayout)
}

func linkedVPSCancellationContext(summary assetlifecycle.LinkedVPSContext) (bool, string) {
	switch {
	case summary.LifecycleStatus == vpsassets.LifecycleCancelled:
		return true, "关联 VPS 已取消"
	case summary.LifecycleStatus == vpsassets.LifecycleToCancel:
		return true, "关联 VPS 待取消"
	case vpsassets.IsCancellationRenewalDecision(summary.RenewalDecision):
		return true, "关联 VPS 已决定不续费"
	case isInactiveSubscriptionEvidence(subscriptions.Status(summary.SubscriptionState)):
		return true, "关联订阅账单记录已无续费动作，但 VPS 未进入取消状态"
	default:
		return false, "关联资产状态正常"
	}
}

func isInactiveSubscriptionEvidence(status subscriptions.Status) bool {
	switch status {
	case subscriptions.StatusExpired, subscriptions.StatusCancelled, subscriptions.StatusPaused, subscriptions.StatusUnknown:
		return true
	default:
		return false
	}
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func isLifecycleActionInvalidPostgresError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503" || pgErr.Code == "23514"
}
