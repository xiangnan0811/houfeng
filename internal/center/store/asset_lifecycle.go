package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/assetservices"
	"houfeng/internal/center/ids"

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
			from asset_service_associations
			where vps_id = $1 and target_id is not null and ended_at is null
			union
			select target_id
			from asset_domain_associations
			where vps_id = $1 and target_id is not null and ended_at is null
		)
		select count(*)::int
		from linked_targets lt
		join targets t on t.target_id = lt.target_id
		where t.run_status in ($2, $3) and t.lifecycle_status='active'
		and exists (select 1 from vps_assets v where v.vps_id = $1 and v.lifecycle_status = 'active')`,
		vpsID,
		targets.RunStatusEnabled,
		targets.RunStatusMaintenance,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count running targets for vps %q: %w", vpsID, err)
	}
	return count, nil
}

// The caller attaches the digest after the complete dependency graph is loaded.

func (r *PostgresAssetLifecycleRepository) GetVPSArchiveReview(ctx context.Context, vpsID string) (assetlifecycle.ArchiveReview, error) {
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return assetlifecycle.ArchiveReview{}, fmt.Errorf("%w: vps_id is required", assetlifecycle.ErrInvalidLifecycleActionInput)
	}

	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, fmt.Errorf("begin archive review: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	vps, err := getLifecycleVPSAsset(ctx, tx, vpsID, false)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	return buildVPSArchiveReview(ctx, tx, vps, vpsID, false)
}

func (r *PostgresAssetLifecycleRepository) ApplyVPSArchive(ctx context.Context, vpsID string, input assetlifecycle.ApplyArchiveInput) (_ assetlifecycle.ArchiveReview, resultErr error) {
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return assetlifecycle.ArchiveReview{}, fmt.Errorf("%w: vps_id is required", assetlifecycle.ErrInvalidLifecycleActionInput)
	}
	input = assetlifecycle.NormalizeApplyArchiveInput(input)
	if err := assetlifecycle.ValidateApplyArchiveInput(input); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}

	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, fmt.Errorf("begin vps archive transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	requestDigest := assetlifecycle.DigestArchiveRequest(input)
	var storedDigest string
	var storedResponse []byte
	err = tx.QueryRow(ctx, `select request_digest,response from vps_archive_requests where vps_id=$1 and idempotency_key=$2`, vpsID, input.IdempotencyKey).Scan(&storedDigest, &storedResponse)
	if err == nil {
		if storedDigest != requestDigest {
			return assetlifecycle.ArchiveReview{}, assetlifecycle.ErrArchiveIdempotencyConflict
		}
		var original assetlifecycle.ArchiveReview
		if err := json.Unmarshal(storedResponse, &original); err != nil {
			return original, err
		}
		return original, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return assetlifecycle.ArchiveReview{}, err
	}

	currentVPS, err := getLifecycleVPSAsset(ctx, tx, vpsID, true)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	defer r.finishVPSStateFailure(ctx, tx, currentVPS, assetlifecycle.ActionTypeArchiveVPS, input.Reason, &resultErr)
	review, err := buildVPSArchiveReview(ctx, tx, currentVPS, vpsID, true)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	if strings.TrimSpace(input.ConfirmationName) != strings.TrimSpace(currentVPS.DisplayName) {
		return assetlifecycle.ArchiveReview{}, fmt.Errorf("%w: confirmation_name does not match vps display_name", assetlifecycle.ErrInvalidLifecycleActionInput)
	}
	if len(review.Blockers) > 0 {
		return assetlifecycle.ArchiveReview{}, &assetlifecycle.ArchiveBlockedError{Review: review}
	}
	if !assetlifecycle.MatchesArchivePreview(review, input.PreviewDigest) {
		return assetlifecycle.ArchiveReview{}, &assetlifecycle.StaleArchivePreviewError{Review: review}
	}
	if review.OnlineEvidence.ManualConfirmationRequired && !input.NeverConnectedConfirmation {
		return assetlifecycle.ArchiveReview{}, fmt.Errorf("%w: never_connected_confirmation is required for an object without an issued Agent session", assetlifecycle.ErrInvalidLifecycleActionInput)
	}
	if err := archiveVPSMonitoringAndTargets(ctx, tx, vpsID, input.Reason); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	if err := archiveVPSRelations(ctx, tx, vpsID, input.Reason); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	if err := endVPSMaintenanceForArchive(ctx, tx, vpsID, input.Reason); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}

	before := vpsLifecycleAuditState(currentVPS)
	archivePatch := vpsassets.PatchInput{
		LifecycleStatus: vpsassets.PatchLifecycle(vpsassets.LifecycleArchived),
	}
	if err := vpsassets.ValidatePatchInput(archivePatch); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	if err := validateMergedVPSAssetPatch(currentVPS, archivePatch); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	if _, err := tx.Exec(ctx, `update vps_assets set archived_state_snapshot = jsonb_build_object('lifecycle_status', lifecycle_status, 'usage_tags', usage_tags, 'renewal_decision', renewal_decision, 'captured_at', now(), 'source', 'archive') where vps_id = $1`, currentVPS.VPSID); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	updated, err := patchVPSAssetRow(ctx, tx, currentVPS.VPSID, archivePatch, false)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	if _, err := auditVPSStateTransition(ctx, tx, currentVPS.VPSID, assetlifecycle.ActionTypeArchiveVPS, input.Reason, before, updated); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	commitReview := review
	review.VPS = updated
	review.Eligible = false
	response, err := json.Marshal(review)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	if _, err := tx.Exec(ctx, `insert into vps_archive_requests(vps_id,idempotency_key,request_digest,response) values($1,$2,$3,$4)`, vpsID, input.IdempotencyKey, requestDigest, response); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	// The graph lock keeps enrollment/live evidence and associations stable.
	// Health must continue ticking during business work, so lock its row only
	// for this final short validation and commit interval.
	if err := loadArchiveReceiverEvidence(ctx, tx, &commitReview.OnlineEvidence, true); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	if receiverFaultGeneration.Load() != commitReview.ReceiverFaultGeneration || receiverFaultGeneration.Load() != receiverObservedFaultGeneration.Load() {
		commitReview.OnlineEvidence.ReceiverHealthy = false
	}
	var finalBlockers []assetlifecycle.BlockerDetail
	commitReview.OnlineEvidence, finalBlockers = assetlifecycle.EvaluateArchiveSafety(commitReview.OnlineEvidence)
	if len(finalBlockers) > 0 {
		commitReview.BlockerDetails = finalBlockers
		commitReview.Warnings, commitReview.Blockers = buildArchiveReviewFindings(commitReview)
		commitReview.Eligible = false
		assetlifecycle.AttachArchivePreviewDigest(&commitReview)
		return assetlifecycle.ArchiveReview{}, &assetlifecycle.ArchiveBlockedError{Review: commitReview}
	}
	if !assetlifecycle.MatchesArchivePreview(commitReview, input.PreviewDigest) {
		assetlifecycle.AttachArchivePreviewDigest(&commitReview)
		return assetlifecycle.ArchiveReview{}, &assetlifecycle.StaleArchivePreviewError{Review: commitReview}
	}
	if !review.OnlineEvidence.NeverConnected && (receiverFaultGeneration.Load() != review.ReceiverFaultGeneration || receiverFaultGeneration.Load() != receiverObservedFaultGeneration.Load()) {
		review.VPS = currentVPS
		review.OnlineEvidence.ReceiverHealthy = false
		review.OnlineEvidence.EarliestArchiveAt = nil
		review.Eligible = false
		review.BlockerDetails = append(review.BlockerDetails, assetlifecycle.BlockerDetail{Code: "receiver_observation_unhealthy", BlockedAction: "archive", ResolutionAction: "wait_for_continuous_offline_observation"})
		review.Warnings, review.Blockers = buildArchiveReviewFindings(review)
		return assetlifecycle.ArchiveReview{}, &assetlifecycle.ArchiveBlockedError{Review: review}
	}
	if err := tx.Commit(ctx); err != nil {
		return assetlifecycle.ArchiveReview{}, fmt.Errorf("commit vps archive transaction for %q: %w", currentVPS.VPSID, err)
	}

	return review, nil
}

func (r *PostgresAssetLifecycleRepository) RestoreVPSFromArchive(ctx context.Context, vpsID string, input assetlifecycle.RestoreArchiveInput) (_ vpsassets.Record, resultErr error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if err := assetlifecycle.ValidateLifecycleReason(input.Reason); err != nil {
		return vpsassets.Record{}, err
	}
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return vpsassets.Record{}, fmt.Errorf("%w: vps_id is required", assetlifecycle.ErrInvalidLifecycleActionInput)
	}

	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("begin restore vps from archive transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	currentVPS, err := getLifecycleVPSAsset(ctx, tx, vpsID, true)
	if err != nil {
		return vpsassets.Record{}, err
	}
	defer r.finishVPSStateFailure(ctx, tx, currentVPS, assetlifecycle.ActionTypeRestoreVPS, input.Reason, &resultErr)
	if currentVPS.LifecycleStatus != vpsassets.LifecycleArchived {
		return vpsassets.Record{}, fmt.Errorf("%w: only archived vps %q can be restored from archive", assetlifecycle.ErrLifecycleActionBlocked, currentVPS.VPSID)
	}

	restorePatch := vpsassets.PatchInput{
		LifecycleStatus: vpsassets.PatchLifecycle(vpsassets.LifecycleActive),
	}
	if err := vpsassets.ValidatePatchInput(restorePatch); err != nil {
		return vpsassets.Record{}, err
	}
	if err := validateMergedVPSAssetPatch(currentVPS, restorePatch); err != nil {
		return vpsassets.Record{}, err
	}
	updated, err := patchVPSAssetRow(ctx, tx, currentVPS.VPSID, restorePatch, false)
	if err != nil {
		return vpsassets.Record{}, err
	}
	if _, err := tx.Exec(ctx, `update vps_assets set usage_tags=array['闲置']::text[] where vps_id=$1`, vpsID); err != nil {
		return vpsassets.Record{}, err
	}
	updated, err = getLifecycleVPSAsset(ctx, tx, vpsID, false)
	if err != nil {
		return vpsassets.Record{}, err
	}
	if err := clearVPSPendingCommands(ctx, tx, vpsID, input.Reason); err != nil {
		return vpsassets.Record{}, err
	}
	if _, err := auditVPSStateTransition(ctx, tx, currentVPS.VPSID, assetlifecycle.ActionTypeRestoreVPS, input.Reason, vpsLifecycleAuditState(currentVPS), updated); err != nil {
		return vpsassets.Record{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return vpsassets.Record{}, fmt.Errorf("commit restore vps from archive transaction for %q: %w", currentVPS.VPSID, err)
	}
	return updated, nil
}

func (r *PostgresAssetLifecycleRepository) ExtendVPSValidity(ctx context.Context, vpsID string, input assetlifecycle.ExtendValidityInput) (assetlifecycle.LifecycleActionResult, error) {
	vpsID = strings.TrimSpace(vpsID)
	if vpsID == "" {
		return assetlifecycle.LifecycleActionResult{}, fmt.Errorf("%w: vps_id is required", assetlifecycle.ErrInvalidLifecycleActionInput)
	}
	input = assetlifecycle.NormalizeExtendValidityInput(input)
	if err := assetlifecycle.ValidateExtendValidityInput(input); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}

	tx, err := beginAssetGraphTx(ctx, r.db.BeginTx)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, fmt.Errorf("begin vps validity extension transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	currentVPS, err := getLifecycleVPSAsset(ctx, tx, vpsID, true)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	if currentVPS.LifecycleStatus != vpsassets.LifecycleActive {
		return assetlifecycle.LifecycleActionResult{}, fmt.Errorf("%w: archived VPS validity cannot be extended", assetlifecycle.ErrLifecycleActionBlocked)
	}
	before := map[string]any{"validity_mode": currentVPS.ValidityMode, "expires_at": currentVPS.ExpiresAt}
	if _, err := tx.Exec(ctx, `update vps_assets set validity_mode='fixed',expires_at=$2,updated_at=now() where vps_id=$1`, vpsID, input.ExtendTo.Time); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	action, err := insertAssetLifecycleAudit(ctx, tx, vpsID, assetlifecycle.ActionTypeExtendValidity, assetlifecycle.ActionStatusCompleted, input.Reason, map[string]any{"extend_to": input.ExtendTo.Time.Format(subscriptions.DateLayout), "fee": input.Fee, "fee_currency": input.FeeCurrency, "source_type": input.SourceType})
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	step, err := insertLifecycleStep(ctx, tx, action.ActionID, assetlifecycle.ObjectTypeVPS, vpsID, "vps_validity", assetlifecycle.StepStatusCompleted, before, map[string]any{"validity_mode": "fixed", "expires_at": input.ExtendTo.Time.Format(subscriptions.DateLayout)}, input.Reason)
	if err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return assetlifecycle.LifecycleActionResult{}, err
	}
	return assetlifecycle.LifecycleActionResult{Action: action, Steps: []assetlifecycle.LifecycleActionStep{step}}, nil
}

func buildVPSArchiveReview(ctx context.Context, queryer assetLifecycleQueryer, vps vpsassets.Record, vpsID string, lockSubscriptions bool) (assetlifecycle.ArchiveReview, error) {
	subscriptionRecords, err := listLifecycleSubscriptionsForVPS(ctx, queryer, vpsID, lockSubscriptions)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	monitoringInstanceLinks, err := listLifecycleMonitoringInstancesForVPS(ctx, queryer, vpsID, lockSubscriptions)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	services, err := listLifecycleAssetServicesForVPS(ctx, queryer, vpsID, lockSubscriptions)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	domains, err := listLifecycleAssetDomainsForVPS(ctx, queryer, vpsID, lockSubscriptions)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	targetLinks, err := listLifecycleTargetImpacts(ctx, queryer, services, domains, lockSubscriptions)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}

	review := assetlifecycle.ArchiveReview{
		VPS:                     vps,
		Subscriptions:           buildSubscriptionImpacts(subscriptionRecords),
		MonitoringInstanceLinks: monitoringInstanceLinks,
		Services:                services,
		Domains:                 domains,
		TargetLinks:             targetLinks,
	}
	var associationJSON []byte
	if err := queryer.QueryRow(ctx, `with refs as (select 'service' kind,to_jsonb(a) value,target_id,vps_id from asset_service_associations a where ended_at is null union all select 'domain',to_jsonb(a),target_id,vps_id from asset_domain_associations a where ended_at is null) select coalesce(jsonb_agg(value order by kind,value->>'id'),'[]'::jsonb) from refs where vps_id=$1 or target_id in(select target_id from refs where vps_id=$1)`, vpsID).Scan(&associationJSON); err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	review.AssociationDigest = fmt.Sprintf("%x", sha256.Sum256(associationJSON))
	evidence, err := loadArchiveOnlineEvidence(ctx, queryer, vpsID, false)
	if err != nil {
		return assetlifecycle.ArchiveReview{}, err
	}
	var evidenceBlockers []assetlifecycle.BlockerDetail
	review.ReceiverFaultGeneration = receiverFaultGeneration.Load()
	review.OnlineEvidence, evidenceBlockers = assetlifecycle.EvaluateArchiveSafety(evidence)
	review.BlockerDetails = archiveBlockerDetails(review)
	review.BlockerDetails = append(review.BlockerDetails, evidenceBlockers...)
	review.Warnings, review.Blockers = buildArchiveReviewFindings(review)
	review.Eligible = len(review.Blockers) == 0
	assetlifecycle.AttachArchivePreviewDigest(&review)
	return review, nil
}

func (r *PostgresAssetLifecycleRepository) ListTargetAssetContexts(ctx context.Context) ([]assetlifecycle.AssetContextForTarget, error) {
	rows, err := r.db.Query(ctx, `
		with target_assets as (
			select target_id, vps_id, service_id, null::text as domain_id
			from asset_service_associations
			where target_id is not null and ended_at is null
			union all
			select target_id, vps_id, null::text as service_id, domain_id
			from asset_domain_associations
			where target_id is not null and ended_at is null
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
			), 'missing') as subscription_state,
			exists (
				select 1 from subscriptions s
				where s.vps_id = v.vps_id and s.status <> 'active' and s.auto_renew
			) as historical_auto_renew
		from target_assets ta
		join vps_assets v on v.vps_id = ta.vps_id
		join targets t on t.target_id=ta.target_id
		where v.lifecycle_status='active' and t.lifecycle_status='active'
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
			targetID            string
			serviceID           *string
			domainID            *string
			summary             assetlifecycle.LinkedVPSContext
			lifecycleStatus     string
			renewalDecision     string
			subscriptionState   string
			historicalAutoRenew bool
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
			&historicalAutoRenew,
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
			attention, message := linkedVPSCancellationContext(summary, historicalAutoRenew)
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

func listLifecycleMonitoringInstancesForVPS(ctx context.Context, queryer assetLifecycleQueryer, vpsID string, forUpdate bool) ([]assetlinks.MonitoringInstanceSummary, error) {
	query := `select n.monitoring_instance_id,n.display_name,n."group",n.region,n.city,n.provider,n.lifecycle_status,n.monitoring_status,n.binding_status,` + monitoringHealthProjectionSQL("n", "(select lifecycle_status from vps_assets where vps_id=n.vps_id)") + `,n.last_heartbeat_at,n.last_sync_at,n.current_active_incident_count,n.current_primary_issue_summary,n.created_at,''::text,n.archived_at from monitoring_instances n where n.vps_id=$1 order by n.monitoring_instance_id`
	if forUpdate {
		query += ` for update of n`
	}
	rows, err := queryer.Query(ctx, query, vpsID)
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
			&summary.ArchivedAt,
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

func listLifecycleAssetServicesForVPS(ctx context.Context, queryer assetLifecycleQueryer, vpsID string, forUpdate bool) ([]assetservices.Record, error) {
	query := `select s.service_id,a.vps_id,a.target_id,s.name,s.service_type,s.status,a.address,a.port,s.labels,s.note,s.created_at,s.updated_at from asset_services s join asset_service_associations a using(service_id) where a.vps_id=$1 and a.ended_at is null order by lower(s.name),s.service_id`
	if forUpdate {
		query += ` for update of s,a`
	}
	rows, err := queryer.Query(ctx, query, vpsID)
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

func listLifecycleAssetDomainsForVPS(ctx context.Context, queryer assetLifecycleQueryer, vpsID string, forUpdate bool) ([]assetdomains.Record, error) {
	query := `select d.domain_id,a.vps_id,a.service_id,a.target_id,d.domain_name,d.purpose,d.status,d.registrar,d.expires_at,d.auto_renew,d.https_enabled,d.labels,d.note,d.created_at,d.updated_at from asset_domains d join asset_domain_associations a using(domain_id) where a.vps_id=$1 and a.ended_at is null order by lower(d.domain_name),d.domain_id`
	if forUpdate {
		query += ` for update of d,a`
	}
	rows, err := queryer.Query(ctx, query, vpsID)
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

func listLifecycleTargetImpacts(ctx context.Context, queryer assetLifecycleQueryer, services []assetservices.Record, domains []assetdomains.Record, forUpdate bool) ([]assetlifecycle.TargetImpact, error) {
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

	query := `
		select target_id, name, run_status
		from targets
		where target_id = any($1::text[])
		order by lower(name), target_id`
	if forUpdate {
		query += `
		for update`
	}
	rows, err := queryer.Query(ctx, query, targetIDs)
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
			impact.RecommendedAction = "review_provider_auto_renew"
		case subscriptions.StatusExpired, subscriptions.StatusCancelled, subscriptions.StatusPaused:
			impact.Role = "inactive"
			impact.RecommendedAction = "keep_inactive"
		default:
			impact.Role = "attention"
			impact.RecommendedAction = "review_billing_facts"
		}
		impact.Message = fmt.Sprintf("账单状态 %s；续费方式 %s。归档保留账单事实，请独立核对服务商自动续费。", record.Status, record.RenewalMode)
		if record.AutoRenew && record.Status != subscriptions.StatusActive {
			impact.Role = "attention"
			impact.RecommendedAction = "review_provider_auto_renew"
		}
		impacts = append(impacts, impact)
	}
	return impacts
}

func buildArchiveReviewFindings(review assetlifecycle.ArchiveReview) ([]string, []string) {
	warnings := make([]string, 0)
	blockers := make([]string, 0)

	if len(review.Subscriptions) == 0 {
		warnings = append(warnings, "没有订阅记录；资源有效期与服务商扣费请独立核对。")
	}
	if len(review.Services) == 0 {
		warnings = append(warnings, "没有服务关联。")
	}
	if len(review.Domains) == 0 {
		warnings = append(warnings, "没有域名关联。")
	}
	for _, impact := range review.Subscriptions {
		if impact.Record.Status == subscriptions.StatusActive || impact.Record.AutoRenew {
			warnings = append(warnings, "订阅仍可能产生费用，归档不会更改服务商续费事实。")
		}
	}
	if review.OnlineEvidence.ManualConfirmationRequired {
		warnings = append(warnings, "此 VPS 从未形成有效 Agent 会话，归档须人工确认并说明原因。")
	}
	for _, detail := range review.BlockerDetails {
		blockers = append(blockers, archiveBlockerMessage(detail))
	}

	return warnings, blockers
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

func linkedVPSCancellationContext(summary assetlifecycle.LinkedVPSContext, historicalAutoRenew bool) (bool, string) {
	switch {
	case historicalAutoRenew:
		return true, "关联历史或待确认订阅仍记录自动续费，请核对账单事实"
	case vpsassets.IsCancellationRenewalDecision(summary.RenewalDecision):
		return true, "关联 VPS 已决定不续费"
	case isInactiveSubscriptionEvidence(subscriptions.Status(summary.SubscriptionState)):
		return true, "关联订阅处于历史或待确认状态；请分别核对权益与续费事实"
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
