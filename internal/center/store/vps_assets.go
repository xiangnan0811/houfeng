package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/renewals"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
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
		archived_at`

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

func (r *PostgresVPSAssetRepository) CreateVPSAsset(ctx context.Context, input vpsassets.CreateInput) (vpsassets.Record, error) {
	input = vpsassets.NormalizeCreateInput(input)
	if err := vpsassets.ValidateCreateInput(input); err != nil {
		return vpsassets.Record{}, err
	}

	vpsID, err := ids.New("vps")
	if err != nil {
		return vpsassets.Record{}, fmt.Errorf("generate vps asset id: %w", err)
	}

	record, err := scanVPSAsset(r.db.QueryRow(ctx, `
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
			archived_at
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
			case when $18::text = 'archived' then now() else null end
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
	))
	if err != nil {
		if isVPSAssetInvalidPostgresError(err) {
			return vpsassets.Record{}, vpsassets.ErrInvalidVPSAssetInput
		}
		return vpsassets.Record{}, fmt.Errorf("create vps asset: %w", err)
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) PatchVPSAsset(ctx context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, error) {
	input = vpsassets.NormalizePatchInput(input)
	if err := vpsassets.ValidatePatchInput(input); err != nil {
		return vpsassets.Record{}, err
	}
	if !input.HasChanges() {
		return r.GetVPSAsset(ctx, vpsID)
	}

	if patchRequiresVPSAssetHistory(input) {
		return r.patchVPSAssetWithHistory(ctx, vpsID, input)
	}

	current, err := r.GetVPSAsset(ctx, vpsID)
	if err != nil {
		return vpsassets.Record{}, err
	}
	if err := validateMergedVPSAssetPatch(current, input); err != nil {
		return vpsassets.Record{}, err
	}

	record, err := patchVPSAssetRow(ctx, r.db, vpsID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsassets.Record{}, vpsassets.ErrVPSAssetNotFound
	}
	if err != nil {
		if isVPSAssetInvalidPostgresError(err) {
			return vpsassets.Record{}, vpsassets.ErrInvalidVPSAssetInput
		}
		return vpsassets.Record{}, fmt.Errorf("patch vps asset %q: %w", vpsID, err)
	}
	return record, nil
}

func (r *PostgresVPSAssetRepository) patchVPSAssetWithHistory(ctx context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, error) {
	record, _, err := r.patchVPSAssetWithHistoryAndOptionalSubscriptionLinkage(ctx, vpsID, input, false)
	return record, err
}

func (r *PostgresVPSAssetRepository) PatchVPSAssetWithSubscriptionRenewalLinkage(ctx context.Context, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, vpsassets.RenewalSubscriptionLinkage, error) {
	input = vpsassets.NormalizePatchInput(input)
	if err := vpsassets.ValidatePatchInput(input); err != nil {
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

	tx, err := r.beginTx(ctx, pgx.TxOptions{})
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
	if err := validateMergedVPSAssetPatch(current, input); err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, err
	}

	record, err := patchVPSAssetRow(ctx, tx, vpsID, input)
	if errors.Is(err, pgx.ErrNoRows) {
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
	if linkSubscription && current.RenewalDecision != record.RenewalDecision && vpsassets.IsCancellationRenewalDecision(record.RenewalDecision) {
		linkage, err = cancelSingleActiveSubscriptionAutoRenew(ctx, tx, record.VPSID)
		if err != nil {
			return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return vpsassets.Record{}, vpsassets.RenewalSubscriptionLinkage{}, fmt.Errorf("commit vps asset history transaction: %w", err)
	}
	return record, linkage, nil
}

func recordVPSAssetHistoryChanges(ctx context.Context, tx pgx.Tx, current, record vpsassets.Record, input vpsassets.PatchInput) error {
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

	if current.IPv4 != record.IPv4 || current.IPv6 != record.IPv6 {
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

func cancelSingleActiveSubscriptionAutoRenew(ctx context.Context, tx pgx.Tx, vpsID string) (vpsassets.RenewalSubscriptionLinkage, error) {
	records, err := listSubscriptionsForVPSForUpdate(ctx, tx, vpsID)
	if err != nil {
		return vpsassets.RenewalSubscriptionLinkage{}, err
	}
	activeRecords := make([]subscriptions.Record, 0, len(records))
	for _, record := range records {
		if record.Status == subscriptions.StatusActive {
			activeRecords = append(activeRecords, record)
		}
	}
	if len(activeRecords) == 0 {
		message := "缺少生效中的订阅记录，续费决策已保存但没有自动取消订阅自动续费。"
		if len(records) > 0 {
			message = "关联订阅账单记录已无续费动作，续费决策已保存；仍需通过取消/退役工作台处理 VPS、监控实例与入口探测状态。"
		}
		return vpsassets.RenewalSubscriptionLinkage{
			Status:         vpsassets.RenewalSubscriptionLinkageNoActiveSubscription,
			CandidateCount: len(records),
			Message:        message,
		}, nil
	}
	if len(activeRecords) > 1 {
		return vpsassets.RenewalSubscriptionLinkage{
			Status:         vpsassets.RenewalSubscriptionLinkageMultipleActiveSubscription,
			CandidateCount: len(activeRecords),
			Message:        "存在多条仍显示自动续费有效的订阅账单记录，续费决策已保存但未自动批量修改；请到订阅页核对要取消自动续费的记录。",
		}, nil
	}

	current := activeRecords[0]
	input := subscriptions.NormalizePatchInput(subscriptions.PatchInput{
		AutoRenew:          subscriptions.PatchBool(false),
		AutoRenewCancelled: subscriptions.PatchBool(true),
	})
	if err := subscriptions.ValidatePatchInput(input); err != nil {
		return vpsassets.RenewalSubscriptionLinkage{}, err
	}
	if !subscriptionPriceHistoryChanged(current, applySubscriptionPatchPreview(current, input)) {
		return vpsassets.RenewalSubscriptionLinkage{
			Status:         vpsassets.RenewalSubscriptionLinkageAlreadyCancelled,
			CandidateCount: 1,
			SubscriptionID: current.SubscriptionID,
			Message:        "关联订阅已处于取消自动续费状态。",
		}, nil
	}

	updated, err := patchSubscriptionRow(ctx, tx, current.SubscriptionID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		return vpsassets.RenewalSubscriptionLinkage{}, subscriptions.ErrSubscriptionNotFound
	}
	if err != nil {
		if isSubscriptionInvalidPostgresError(err) {
			return vpsassets.RenewalSubscriptionLinkage{}, subscriptions.ErrInvalidSubscriptionInput
		}
		return vpsassets.RenewalSubscriptionLinkage{}, fmt.Errorf("patch subscription %q for vps renewal linkage: %w", current.SubscriptionID, err)
	}
	if subscriptionPriceHistoryChanged(current, updated) {
		if _, err := createPriceHistory(ctx, tx, renewals.CreatePriceHistoryInput{
			From: current,
			To:   updated,
		}); err != nil {
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) || errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				return vpsassets.RenewalSubscriptionLinkage{}, subscriptions.ErrInvalidSubscriptionInput
			}
			return vpsassets.RenewalSubscriptionLinkage{}, fmt.Errorf("record price history for subscription %q: %w", current.SubscriptionID, err)
		}
	}

	return vpsassets.RenewalSubscriptionLinkage{
		Status:         vpsassets.RenewalSubscriptionLinkageUpdated,
		CandidateCount: 1,
		SubscriptionID: updated.SubscriptionID,
		Updated:        true,
		Message:        "已同步取消关联订阅的自动续费。",
	}, nil
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
	return input.RenewalDecision.Set ||
		input.IPv4.Set ||
		input.IPv6.Set ||
		input.ProductName.Set ||
		input.SSHHost.Set ||
		input.SSHPort.Set ||
		input.SSHUser.Set ||
		input.OSName.Set ||
		input.Virtualization.Set
}

func validateMergedVPSAssetPatch(current vpsassets.Record, input vpsassets.PatchInput) error {
	merged := applyVPSAssetPatchPreview(current, input)
	return vpsassets.ValidateVPSStateCombination(merged.LifecycleStatus, merged.UsageStatus, merged.RenewalDecision)
}

func applyVPSAssetPatchPreview(record vpsassets.Record, input vpsassets.PatchInput) vpsassets.Record {
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

func patchVPSAssetRow(ctx context.Context, db vpsAssetQueryer, vpsID string, input vpsassets.PatchInput) (vpsassets.Record, error) {
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
		    archived_at = case
		        when $34::boolean and $35::text = 'archived' then coalesce(archived_at, now())
		        when $34::boolean and $35::text <> 'archived' then null
		        else archived_at
		    end,
		    updated_at = now()
		where vps_id = $1
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
	))
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
