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

	"houfeng/internal/center/ids"
	"houfeng/internal/center/renewals"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/ipidentity"
)

var _ vpsassets.Repository = (*PostgresVPSAssetRepository)(nil)

type vpsAssetDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type vpsAssetQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresVPSAssetRepository struct {
	db      vpsAssetDB
	beginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func NewPostgresVPSAssetRepository(db *pgxpool.Pool) *PostgresVPSAssetRepository {
	return &PostgresVPSAssetRepository{
		db:      db,
		beginTx: db.BeginTx,
	}
}

const vpsAssetSelectColumns = `
	vps_id,
	display_name,
	provider_id,
	provider_name,
	product_name,
	order_ref,
	country,
	region,
	city,
	datacenter,
	ipv4,
	ipv6,
	ssh_host,
	ssh_port,
	ssh_user,
	os_name,
	virtualization,
	lifecycle_status,
	usage_status,
	renewal_decision,
	importance,
		labels,
		note,
		0::int as active_monitoring_instance_link_count,
		0::int as running_monitoring_instance_count,
		0::int as running_target_count,
		created_at,
		updated_at,
		archived_at,
		archived_state_snapshot,
		usage_tags, validity_mode, expires_at::text, auto_renew_check, auto_renew_checked_at,
		renewal_reason, renewal_review_at, acquisition_source`

type vpsAssetScanner interface {
	Scan(dest ...any) error
}

func scanVPSAsset(row vpsAssetScanner) (vpsassets.Record, error) {
	var record vpsassets.Record
	if err := row.Scan(
		&record.VPSID,
		&record.DisplayName,
		&record.ProviderID,
		&record.ProviderName,
		&record.ProductName,
		&record.OrderRef,
		&record.Country,
		&record.Region,
		&record.City,
		&record.Datacenter,
		&record.IPv4,
		&record.IPv6,
		&record.SSHHost,
		&record.SSHPort,
		&record.SSHUser,
		&record.OSName,
		&record.Virtualization,
		&record.LifecycleStatus,
		&record.UsageStatus,
		&record.RenewalDecision,
		&record.Importance,
		&record.Labels,
		&record.Note,
		&record.ActiveMonitoringInstanceLinkCount,
		&record.RunningMonitoringInstanceCount,
		&record.RunningTargetCount,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.ArchivedAt,
		&record.ArchivedStateSnapshot,
		&record.UsageTags, &record.ValidityMode, &record.ExpiresAt, &record.AutoRenewCheck, &record.AutoRenewCheckedAt,
		&record.RenewalReason, &record.RenewalReviewAt, &record.AcquisitionSource,
	); err != nil {
		return vpsassets.Record{}, err
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) ListVPSAssets(ctx context.Context, filters vpsassets.ListFilters) ([]vpsassets.Record, error) {
	filters = vpsassets.NormalizeListFilters(filters)
	if err := vpsassets.ValidateListFilters(filters); err != nil {
		return nil, err
	}

	args := []any{}
	conditions := []string{}
	if filters.UsageTag != "" {
		args = append(args, filters.UsageTag)
		conditions = append(conditions, fmt.Sprintf("$%d = any(usage_tags)", len(args)))
	}
	if filters.ProviderID != "" {
		args = append(args, filters.ProviderID)
		conditions = append(conditions, fmt.Sprintf("provider_id = $%d", len(args)))
	}
	if filters.LifecycleStatus != "" {
		args = append(args, string(filters.LifecycleStatus))
		conditions = append(conditions, fmt.Sprintf("lifecycle_status = $%d", len(args)))
	} else {
		switch filters.AssetScope {
		case vpsassets.AssetScopeArchived, vpsassets.AssetScopeHistorical:
			conditions = append(conditions, "lifecycle_status in ('cancelled', 'archived')")
		case vpsassets.AssetScopeAll, "":
		default:
			conditions = append(conditions, "lifecycle_status not in ('cancelled', 'archived')")
		}
	}
	if filters.UsageStatus != "" {
		args = append(args, string(filters.UsageStatus))
		conditions = append(conditions, fmt.Sprintf("usage_status = $%d", len(args)))
	}
	if filters.RenewalDecision != "" {
		args = append(args, string(filters.RenewalDecision))
		conditions = append(conditions, fmt.Sprintf("renewal_decision = $%d", len(args)))
	}

	query := `
		select ` + vpsAssetSelectColumns + `
		from vps_assets`
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by lower(display_name), vps_id"

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query vps assets: %w", err)
	}
	defer rows.Close()

	records := make([]vpsassets.Record, 0)
	for rows.Next() {
		record, err := scanVPSAsset(rows)
		if err != nil {
			return nil, fmt.Errorf("scan vps asset: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate vps assets: %w", err)
	}
	return records, nil
}

func (r *PostgresVPSAssetRepository) GetVPSAsset(ctx context.Context, vpsID string) (vpsassets.Record, error) {
	record, err := scanVPSAsset(r.db.QueryRow(ctx, `
		select `+vpsAssetSelectColumns+`
		from vps_assets
		where vps_id = $1`, vpsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsassets.Record{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("query vps asset %q: %w", vpsID, err)
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) loadVPSRecordSubject(ctx context.Context, vpsID string) (vpsRecordSubject, error) {
	var subject vpsRecordSubject
	err := r.db.QueryRow(ctx, `
		select vps_id, display_name, provider_name, region
		from vps_assets
		where vps_id = $1`, vpsID).Scan(
		&subject.VPSID,
		&subject.DisplayName,
		&subject.Provider,
		&subject.Region,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsRecordSubject{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		return vpsRecordSubject{}, fmt.Errorf("query VPS record subject: %w", err)
	}
	return subject, nil
}

func (r *PostgresVPSAssetRepository) CreateVPSAsset(ctx context.Context, input vpsassets.CreateInput) (vpsassets.Record, error) {
	input = vpsassets.NormalizeCreateInput(input)
	if err := vpsassets.ValidateCreateInput(input); err != nil {
		return vpsassets.Record{}, err
	}
	if r.beginTx == nil {
		return vpsassets.Record{}, errors.New("vps asset repository cannot create without transaction support")
	}

	vpsID, err := ids.New("vps")
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("generate vps asset id: %w", err)
	}
	tx, err := beginAssetGraphTx(ctx, r.beginTx)
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("begin vps asset create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	record, err := scanVPSAsset(tx.QueryRow(ctx, `
		insert into vps_assets (
			vps_id,
			display_name,
			provider_id,
			provider_name,
			product_name,
			order_ref,
			country,
			region,
			city,
			datacenter,
			ipv4,
			ipv6,
			ssh_host,
			ssh_port,
			ssh_user,
			os_name,
			virtualization,
			lifecycle_status,
			usage_status,
			renewal_decision,
			importance,
			labels,
			note,
				archived_at, usage_tags, validity_mode, expires_at, auto_renew_check, auto_renew_checked_at, renewal_reason, renewal_review_at, acquisition_source
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13,
			$14,
			$15,
			$16,
			$17,
			$18,
			$19,
			$20,
			$21,
			$22,
			$23,
			case when $18::text = 'archived' then now() else null end,
			$24::text[], $25, $26::date, $27, $28::timestamptz, $29, $30::timestamptz, $31
		)
		returning `+vpsAssetSelectColumns,
		vpsID,
		input.DisplayName,
		nullableStringArg(input.ProviderID),
		input.ProviderName,
		input.ProductName,
		input.OrderRef,
		input.Country,
		input.Region,
		input.City,
		input.Datacenter,
		input.IPv4,
		input.IPv6,
		input.SSHHost,
		input.SSHPort,
		input.SSHUser,
		input.OSName,
		input.Virtualization,
		string(input.LifecycleStatus),
		string(input.UsageStatus),
		string(input.RenewalDecision),
		input.Importance,
		input.Labels,
		input.Note,
		input.UsageTags, input.ValidityMode, nullableStringArg(input.ExpiresAt), input.AutoRenewCheck, nullableTimeArg(input.AutoRenewCheckedAt), input.RenewalReason, nullableTimeArg(input.RenewalReviewAt), input.AcquisitionSource,
	))
	if err != nil {
		if isVPSAssetInvalidPostgresError(err) {
			return vpsassets.Record{}, vpsassets.ErrInvalidVPSAssetInput
		}
		return vpsassets.Record{}, fmt.Errorf("create vps asset: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return vpsassets.Record{}, fmt.Errorf("commit vps asset create transaction: %w", err)
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) PatchVPSAsset(ctx context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, error) {
	input = vpsassets.NormalizePatchInput(input)
	if err := vpsassets.ValidateOrdinaryPatchInput(input); err != nil {
		return vpsassets.Record{}, err
	}
	if !input.HasChanges() {
		return r.GetVPSAsset(ctx, vpsID)
	}

	if patchRequiresVPSAssetHistory(input) {
		return r.patchVPSAssetWithHistory(ctx, vpsID, input)
	}
	if r.beginTx == nil {
		return vpsassets.Record{}, errors.New("vps asset repository cannot patch without transaction support")
	}

	tx, err := beginAssetGraphTx(ctx, r.beginTx)
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("begin vps asset patch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanVPSAsset(tx.QueryRow(ctx, `
		select `+vpsAssetSelectColumns+`
		from vps_assets
		where vps_id = $1
		for update`, vpsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsassets.Record{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("query vps asset %q before ordinary patch: %w", vpsID, err)
	}
	if err := ensureVPSAssetPatchAllowed(current, input); err != nil {
		return vpsassets.Record{}, err
	}
	if err := validateMergedVPSAssetPatch(current, input); err != nil {
		return vpsassets.Record{}, err
	}
	if conflict := vpsAssetPreconditionConflict(current, input.ExpectedUpdatedAt); conflict != nil {
		return vpsassets.Record{}, conflict
	}
	if err := validateChangedVPSAddresses(current, input); err != nil {
		return vpsassets.Record{}, err
	}

	record, err := patchOrdinaryVPSAssetRow(ctx, tx, vpsID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := ensureVPSAssetPatchAllowed(current, input); err != nil {
			return vpsassets.Record{}, err
		}
		if input.ExpectedUpdatedAt != nil {
			return vpsassets.Record{}, vpsassets.ErrVPSAssetConflict
		}
		return vpsassets.Record{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		if isVPSAssetInvalidPostgresError(err) {
			return vpsassets.Record{}, vpsassets.ErrInvalidVPSAssetInput
		}
		return vpsassets.Record{}, fmt.Errorf("patch vps asset %q: %w", vpsID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return vpsassets.Record{}, fmt.Errorf("commit vps asset patch transaction: %w", err)
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) patchVPSAssetWithHistory(ctx context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, error) {
	record, _, err := r.patchVPSAssetWithHistoryAndOptionalSubscriptionLinkage(ctx, vpsID, input, false)
	return record, err
}

func (r *PostgresVPSAssetRepository) PatchVPSAssetWithSubscriptionRenewalLinkage(ctx context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, vpsassets.RenewalSubscriptionLinkage, error) {
	input = vpsassets.NormalizePatchInput(input)
	if err := vpsassets.ValidateOrdinaryPatchInput(input); err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, err
	}
	if !input.HasChanges() {
		record, err := r.GetVPSAsset(ctx, vpsID)
		return record, noRenewalSubscriptionLinkage(), err
	}
	if !input.RenewalDecision.Set || !vpsassets.IsCancellationRenewalDecision(input.RenewalDecision.Value) {
		record, err := r.PatchVPSAsset(ctx, vpsID, input)
		return record, noRenewalSubscriptionLinkage(), err
	}
	return r.patchVPSAssetWithHistoryAndOptionalSubscriptionLinkage(ctx, vpsID, input, true)
}

func (r *PostgresVPSAssetRepository) patchVPSAssetWithHistoryAndOptionalSubscriptionLinkage(ctx context.Context, vpsID string, input vpsassets.PatchInput, linkSubscription bool) (vpsassets.Record, vpsassets.RenewalSubscriptionLinkage, error) {
	if r.beginTx == nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, errors.New("vps asset repository cannot record asset history without transaction support")
	}

	tx, err := beginAssetGraphTx(ctx, r.beginTx)
	if err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, fmt.Errorf("begin vps asset history transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanVPSAsset(tx.QueryRow(ctx, `
			select `+vpsAssetSelectColumns+`
			from vps_assets
			where vps_id = $1
			for update`, vpsID))
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, fmt.Errorf("query vps asset %q before history patch: %w", vpsID, err)
	}
	if err := ensureVPSAssetPatchAllowed(current, input); err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, err
	}
	if err := validateMergedVPSAssetPatch(current, input); err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, err
	}
	if conflict := vpsAssetPreconditionConflict(current, input.ExpectedUpdatedAt); conflict != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, conflict
	}
	if err := validateChangedVPSAddresses(current, input); err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, err
	}

	record, err := patchOrdinaryVPSAssetRow(ctx, tx, vpsID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := ensureVPSAssetPatchAllowed(current, input); err != nil {
			return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, err
		}
		if input.ExpectedUpdatedAt != nil {
			return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, vpsassets.ErrVPSAssetConflict
		}
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		if isVPSAssetInvalidPostgresError(err) {
			return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, vpsassets.ErrInvalidVPSAssetInput
		}
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, fmt.Errorf("patch vps asset %q: %w", vpsID, err)
	}

	if err := recordVPSAssetHistoryChanges(ctx, tx, current, record, input); err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, err
	}

	linkage := noRenewalSubscriptionLinkage()

	if err := tx.Commit(ctx); err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, fmt.Errorf("commit vps asset history transaction: %w", err)
	}
	return record, linkage, nil
}

func recordVPSAssetHistoryChanges(ctx context.Context, tx pgx.Tx, current, record vpsassets.Record, input vpsassets.PatchInput) error {
	if input.AutoRenewCheck.Set || input.AutoRenewCheckedAt.Set || input.RenewalReviewAt.Set || input.RenewalReason.Set || input.ValidityMode.Set || input.ExpiresAt.Set || (current.LifecycleStatus == vpsassets.LifecycleArchived && input.Note.Set) {
		before, after := vpsIndependentFactsSnapshot(current), vpsIndependentFactsSnapshot(record)
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if string(beforeJSON) != string(afterJSON) {
			details, err := json.Marshal(map[string]any{"before": before, "after": after})
			if err != nil {
				return err
			}
			if _, err := createExperienceLog(ctx, tx, renewals.CreateExperienceLogInput{VPSID: record.VPSID, Category: renewals.ExperienceBilling, Severity: "info", Summary: "资源有效期、续费核对或归档补充修订", Details: string(details)}); err != nil {
				return err
			}
		}
	}
	if current.RenewalDecision != record.RenewalDecision {
		fromDecision := current.RenewalDecision
		if _, err := createRenewalDecision(ctx, tx, renewals.CreateDecisionInput{
			VPSID:        record.VPSID,
			FromDecision: &fromDecision,
			ToDecision:   record.RenewalDecision,
			Reason:       input.RenewalReason.Value,
		}); err != nil {
			if errors.Is(err, renewals.ErrInvalidRenewalDecisionInput) || errors.Is(err, renewals.ErrRenewalTimelineNotFound) {
				return vpsassets.ErrInvalidVPSAssetInput
			}
			return fmt.Errorf("record renewal decision history for vps %q: %w", record.VPSID, err)
		}
	}

	if ipidentity.Changed(current.IPv4, record.IPv4) || ipidentity.Changed(current.IPv6, record.IPv6) {
		if _, err := createIPHistory(ctx, tx, renewals.CreateIPHistoryInput{
			VPSID:    record.VPSID,
			FromIPv4: current.IPv4,
			ToIPv4:   record.IPv4,
			FromIPv6: current.IPv6,
			ToIPv6:   record.IPv6,
		}); err != nil {
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) || errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				return vpsassets.ErrInvalidVPSAssetInput
			}
			return fmt.Errorf("record ip history for vps %q: %w", record.VPSID, err)
		}
	}

	if vpsSpecChanged(current, record) {
		if _, err := createSpecSnapshot(ctx, tx, renewals.CreateSpecSnapshotInput{
			VPSID:          record.VPSID,
			ProductName:    record.ProductName,
			SSHHost:        record.SSHHost,
			SSHPort:        record.SSHPort,
			SSHUser:        record.SSHUser,
			OSName:         record.OSName,
			Virtualization: record.Virtualization,
		}); err != nil {
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) || errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				return vpsassets.ErrInvalidVPSAssetInput
			}
			return fmt.Errorf("record spec snapshot for vps %q: %w", record.VPSID, err)
		}
	}
	return nil
}

func noRenewalSubscriptionLinkage() vpsassets.RenewalSubscriptionLinkage {
	return vpsassets.RenewalSubscriptionLinkage{
		Status:  vpsassets.RenewalSubscriptionLinkageNone,
		Message: "续费决策不需要联动订阅自动续费。",
	}
}

func listSubscriptionsForVPSForUpdate(ctx context.Context, tx pgx.Tx, vpsID string) ([]subscriptions.Record, error) {
	rows, err := tx.Query(ctx, `
		select `+subscriptionSelectColumns+`
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
			subscription_id
		for update`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions for vps %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]subscriptions.Record, 0)
	for rows.Next() {
		record, err := scanSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("scan subscription for vps %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions for vps %q: %w", vpsID, err)
	}
	return records, nil
}

func applySubscriptionPatchPreview(record subscriptions.Record, input subscriptions.PatchInput) subscriptions.Record {
	if input.VPSID.Set {
		record.VPSID = input.VPSID.Value
	}
	if input.Price.Set {
		record.Price = input.Price.Value
	}
	if input.Currency.Set {
		record.Currency = input.Currency.Value
	}
	if input.BillingCycle.Set {
		record.BillingCycle = input.BillingCycle.Value
	}
	if input.BillingMonths.Set {
		record.BillingMonths = input.BillingMonths.Value
	}
	if input.BillingPeriodUnit.Set {
		record.BillingPeriodUnit = input.BillingPeriodUnit.Value
	}
	if input.BillingPeriodLength.Set {
		record.BillingPeriodLength = input.BillingPeriodLength.Value
	}
	if input.Price.Set || input.BillingPeriodUnit.Set || input.BillingPeriodLength.Set {
		record.MonthlyPrice = subscriptions.CalculateMonthlyPriceForPeriod(record.Price, record.BillingPeriodUnit, record.BillingPeriodLength)
	}
	if input.StartedAt.Set {
		record.StartedAt = cloneSubscriptionDate(input.StartedAt.Value)
	}
	if input.RenewAt.Set {
		record.RenewAt = cloneSubscriptionDate(input.RenewAt.Value)
	}
	if input.AutoRenew.Set {
		record.AutoRenew = input.AutoRenew.Value
	}
	if input.AutoRenewCancelled.Set {
		record.AutoRenewCancelled = input.AutoRenewCancelled.Value
	}
	if input.RenewalMode.Set {
		record.RenewalMode = input.RenewalMode.Value
	}
	if input.Status.Set {
		record.Status = input.Status.Value
	}
	if input.PaymentMethod.Set {
		record.PaymentMethod = input.PaymentMethod.Value
	}
	if input.Note.Set {
		record.Note = input.Note.Value
	}
	return record
}

func cloneSubscriptionDate(value *subscriptions.Date) *subscriptions.Date {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func patchRequiresVPSAssetHistory(input vpsassets.PatchInput) bool {
	return input.AutoRenewCheck.Set || input.AutoRenewCheckedAt.Set || input.RenewalReviewAt.Set || input.RenewalReason.Set || input.ValidityMode.Set || input.ExpiresAt.Set || input.Note.Set || input.RenewalDecision.Set ||
		input.IPv4.Set ||
		input.IPv6.Set ||
		input.ProductName.Set ||
		input.SSHHost.Set ||
		input.SSHPort.Set ||
		input.SSHUser.Set ||
		input.OSName.Set ||
		input.Virtualization.Set
}

// validateChangedVPSAddresses 必须在版本前提检查之后调用：过期表单回传旧地址时应先得到 409 冲突，
// 而不是 400。整表单编辑会原样回传地址，改动判定与 IP history 同用 ipidentity.Changed
// （不可解析时按 trim 后原文比较），存量非法文本不阻塞其他字段的保存。
func validateChangedVPSAddresses(current vpsassets.Record, input vpsassets.PatchInput) error {
	changedIPv4, changedIPv6 := "", ""
	if input.IPv4.Set && ipidentity.Changed(current.IPv4, input.IPv4.Value) {
		changedIPv4 = input.IPv4.Value
	}
	if input.IPv6.Set && ipidentity.Changed(current.IPv6, input.IPv6.Value) {
		changedIPv6 = input.IPv6.Value
	}
	return vpsassets.ValidateHostAddresses(changedIPv4, changedIPv6)
}

func validateMergedVPSAssetPatch(current vpsassets.Record, input vpsassets.PatchInput) error {
	merged := applyVPSAssetPatchPreview(current, input)
	if err := vpsassets.ValidateVPSStateCombination(merged.LifecycleStatus, merged.UsageStatus, merged.RenewalDecision); err != nil {
		return err
	}
	return vpsassets.ValidateIndependentFacts(merged.ValidityMode, merged.ExpiresAt, merged.AutoRenewCheck, merged.AutoRenewCheckedAt)
}

func ensureVPSAssetOrdinaryPatchAllowed(current vpsassets.Record) error {
	switch current.LifecycleStatus {
	case vpsassets.LifecycleCancelled, vpsassets.LifecycleArchived:
		return fmt.Errorf("%w: %s vps %q cannot be patched through ordinary update", vpsassets.ErrVPSAssetReadonly, current.LifecycleStatus, current.VPSID)
	default:
		return nil
	}
}

func ensureVPSAssetPatchAllowed(current vpsassets.Record, input vpsassets.PatchInput) error {
	if current.LifecycleStatus == vpsassets.LifecycleArchived && input.IsArchivedSupplement() {
		return nil
	}
	return ensureVPSAssetOrdinaryPatchAllowed(current)
}

func vpsIndependentFactsSnapshot(record vpsassets.Record) map[string]any {
	return map[string]any{"validity_mode": record.ValidityMode, "expires_at": record.ExpiresAt, "auto_renew_check": record.AutoRenewCheck, "auto_renew_checked_at": record.AutoRenewCheckedAt, "renewal_reason": record.RenewalReason, "renewal_review_at": record.RenewalReviewAt, "note": record.Note}
}

func applyVPSAssetPatchPreview(record vpsassets.Record, input vpsassets.PatchInput) vpsassets.Record {
	if input.UsageTags.Set {
		record.UsageTags = append([]string{}, input.UsageTags.Values...)
	}
	if input.ValidityMode.Set {
		record.ValidityMode = input.ValidityMode.Value
	}
	if input.ExpiresAt.Set {
		record.ExpiresAt = cloneVPSAssetStringPtr(input.ExpiresAt.Value)
	}
	if input.AutoRenewCheck.Set {
		record.AutoRenewCheck = input.AutoRenewCheck.Value
	}
	if input.AutoRenewCheckedAt.Set {
		record.AutoRenewCheckedAt = input.AutoRenewCheckedAt.Value
	}
	if input.RenewalReason.Set {
		record.RenewalReason = input.RenewalReason.Value
	}
	if input.RenewalReviewAt.Set {
		record.RenewalReviewAt = input.RenewalReviewAt.Value
	}
	if input.AcquisitionSource.Set {
		record.AcquisitionSource = input.AcquisitionSource.Value
	}
	if input.DisplayName.Set {
		record.DisplayName = input.DisplayName.Value
	}
	if input.ProviderID.Set {
		record.ProviderID = cloneVPSAssetStringPtr(input.ProviderID.Value)
	}
	if input.ProviderName.Set {
		record.ProviderName = input.ProviderName.Value
	}
	if input.ProductName.Set {
		record.ProductName = input.ProductName.Value
	}
	if input.OrderRef.Set {
		record.OrderRef = input.OrderRef.Value
	}
	if input.Country.Set {
		record.Country = input.Country.Value
	}
	if input.Region.Set {
		record.Region = input.Region.Value
	}
	if input.City.Set {
		record.City = input.City.Value
	}
	if input.Datacenter.Set {
		record.Datacenter = input.Datacenter.Value
	}
	if input.IPv4.Set {
		record.IPv4 = input.IPv4.Value
	}
	if input.IPv6.Set {
		record.IPv6 = input.IPv6.Value
	}
	if input.SSHHost.Set {
		record.SSHHost = input.SSHHost.Value
	}
	if input.SSHPort.Set {
		record.SSHPort = input.SSHPort.Value
	}
	if input.SSHUser.Set {
		record.SSHUser = input.SSHUser.Value
	}
	if input.OSName.Set {
		record.OSName = input.OSName.Value
	}
	if input.Virtualization.Set {
		record.Virtualization = input.Virtualization.Value
	}
	if input.LifecycleStatus.Set {
		record.LifecycleStatus = input.LifecycleStatus.Value
	}
	if input.UsageStatus.Set {
		record.UsageStatus = input.UsageStatus.Value
	}
	if input.RenewalDecision.Set {
		record.RenewalDecision = input.RenewalDecision.Value
	}
	if input.Importance.Set {
		record.Importance = input.Importance.Value
	}
	if input.Labels.Set {
		record.Labels = append([]string(nil), input.Labels.Values...)
	}
	if input.Note.Set {
		record.Note = input.Note.Value
	}
	return record
}

func vpsSpecChanged(from, to vpsassets.Record) bool {
	return from.ProductName != to.ProductName ||
		from.SSHHost != to.SSHHost ||
		from.SSHPort != to.SSHPort ||
		from.SSHUser != to.SSHUser ||
		from.OSName != to.OSName ||
		from.Virtualization != to.Virtualization
}

func patchOrdinaryVPSAssetRow(ctx context.Context, db vpsAssetQueryer, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, error) {
	return patchVPSAssetRow(ctx, db, vpsID, input, true)
}

func patchVPSAssetRow(ctx context.Context, db vpsAssetQueryer, vpsID string, input vpsassets.PatchInput, ordinary bool) (vpsassets.Record, error) {
	return scanVPSAsset(db.QueryRow(ctx, `
		update vps_assets
		set display_name = case when $2::boolean then $3 else display_name end,
		    provider_id = case when $4::boolean then $5::text else provider_id end,
		    provider_name = case when $6::boolean then $7 else provider_name end,
		    product_name = case when $8::boolean then $9 else product_name end,
		    order_ref = case when $10::boolean then $11 else order_ref end,
		    country = case when $12::boolean then $13 else country end,
		    region = case when $14::boolean then $15 else region end,
		    city = case when $16::boolean then $17 else city end,
		    datacenter = case when $18::boolean then $19 else datacenter end,
		    ipv4 = case when $20::boolean then $21 else ipv4 end,
		    ipv6 = case when $22::boolean then $23 else ipv6 end,
		    ssh_host = case when $24::boolean then $25 else ssh_host end,
		    ssh_port = case when $26::boolean then $27::integer else ssh_port end,
		    ssh_user = case when $28::boolean then $29 else ssh_user end,
		    os_name = case when $30::boolean then $31 else os_name end,
		    virtualization = case when $32::boolean then $33 else virtualization end,
		    lifecycle_status = case when $34::boolean then $35 else lifecycle_status end,
		    usage_status = case when $36::boolean then $37 else usage_status end,
		    renewal_decision = case when $38::boolean then $39 else renewal_decision end,
		    importance = case when $40::boolean then $41 else importance end,
		    labels = case when $42::boolean then $43::text[] else labels end,
		    note = case when $44::boolean then $45 else note end,
		    usage_tags = case when $48::boolean then $49::text[] else usage_tags end,
		    validity_mode = case when $50::boolean then $51 else validity_mode end,
		    expires_at = case when $52::boolean then $53::date else expires_at end,
		    auto_renew_check = case when $54::boolean then $55 else auto_renew_check end,
		    auto_renew_checked_at = case when $56::boolean then $57::timestamptz else auto_renew_checked_at end,
		    renewal_reason = case when $58::boolean then $59 else renewal_reason end,
		    renewal_review_at = case when $60::boolean then $61::timestamptz else renewal_review_at end,
		    acquisition_source = case when $62::boolean then $63 else acquisition_source end,
		    archived_at = case
		        when $34::boolean and $35::text = 'archived' then coalesce(archived_at, now())
		        when $34::boolean and $35::text <> 'archived' then null
		        else archived_at
		    end,
		    updated_at = now()
		where vps_id = $1
		  and ($46::timestamptz is null or updated_at = $46)
		  and (not $47::boolean or lifecycle_status not in ('cancelled', 'archived'))
		returning `+vpsAssetSelectColumns,
		vpsID,
		input.DisplayName.Set,
		input.DisplayName.Value,
		input.ProviderID.Set,
		nullableStringArg(input.ProviderID.Value),
		input.ProviderName.Set,
		input.ProviderName.Value,
		input.ProductName.Set,
		input.ProductName.Value,
		input.OrderRef.Set,
		input.OrderRef.Value,
		input.Country.Set,
		input.Country.Value,
		input.Region.Set,
		input.Region.Value,
		input.City.Set,
		input.City.Value,
		input.Datacenter.Set,
		input.Datacenter.Value,
		input.IPv4.Set,
		input.IPv4.Value,
		input.IPv6.Set,
		input.IPv6.Value,
		input.SSHHost.Set,
		input.SSHHost.Value,
		input.SSHPort.Set,
		input.SSHPort.Value,
		input.SSHUser.Set,
		input.SSHUser.Value,
		input.OSName.Set,
		input.OSName.Value,
		input.Virtualization.Set,
		input.Virtualization.Value,
		input.LifecycleStatus.Set,
		string(input.LifecycleStatus.Value),
		input.UsageStatus.Set,
		string(input.UsageStatus.Value),
		input.RenewalDecision.Set,
		string(input.RenewalDecision.Value),
		input.Importance.Set,
		input.Importance.Value,
		input.Labels.Set,
		input.Labels.Values,
		input.Note.Set,
		input.Note.Value,
		nullableTimeArg(input.ExpectedUpdatedAt),
		ordinary && !input.IsArchivedSupplement(),
		input.UsageTags.Set, input.UsageTags.Values,
		input.ValidityMode.Set, input.ValidityMode.Value,
		input.ExpiresAt.Set, nullableStringArg(input.ExpiresAt.Value),
		input.AutoRenewCheck.Set, input.AutoRenewCheck.Value,
		input.AutoRenewCheckedAt.Set, nullableTimeArg(input.AutoRenewCheckedAt.Value),
		input.RenewalReason.Set, input.RenewalReason.Value,
		input.RenewalReviewAt.Set, nullableTimeArg(input.RenewalReviewAt.Value),
		input.AcquisitionSource.Set, input.AcquisitionSource.Value,
	))
}

func nullableTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func vpsAssetPreconditionConflict(current vpsassets.Record, expected *time.Time) error {
	if expected == nil {
		return nil
	}
	if current.UpdatedAt.UTC().Equal(expected.UTC()) {
		return nil
	}
	return vpsassets.ErrVPSAssetConflict
}

func vpsAssetPatchMissingRowError(
	ctx context.Context,
	repo *PostgresVPSAssetRepository,
	vpsID string,
	expected *time.Time,
) error {
	current, err := repo.GetVPSAsset(ctx, vpsID)
	if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
		return vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		return err
	}
	if err := ensureVPSAssetOrdinaryPatchAllowed(current); err != nil {
		return err
	}
	if expected != nil {
		return vpsassets.ErrVPSAssetConflict
	}
	return vpsassets.ErrVPSAssetNotFound
}

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func cloneVPSAssetStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func isVPSAssetInvalidPostgresError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503" || pgErr.Code == "23514"
}
