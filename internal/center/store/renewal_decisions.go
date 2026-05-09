package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	"houfeng/internal/center/renewals"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

var _ renewals.Repository = (*PostgresRenewalDecisionRepository)(nil)

type renewalDecisionDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type renewalDecisionQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRenewalDecisionRepository struct {
	db renewalDecisionDB
}

func NewPostgresRenewalDecisionRepository(db *pgxpool.Pool) *PostgresRenewalDecisionRepository {
	return &PostgresRenewalDecisionRepository{db: db}
}

const renewalDecisionSelectColumns = `
	decision_id,
	vps_id,
	from_decision,
	to_decision,
	reason,
	decided_at,
	created_at`

const priceHistorySelectColumns = `
	price_history_id,
	subscription_id,
	vps_id,
	from_price,
	to_price,
	from_currency,
	to_currency,
	from_billing_cycle,
	to_billing_cycle,
	from_billing_months,
	to_billing_months,
	from_monthly_price,
	to_monthly_price,
	from_renew_at,
	to_renew_at,
	from_auto_renew,
	to_auto_renew,
	from_auto_renew_cancelled,
	to_auto_renew_cancelled,
	from_status,
	to_status,
	changed_at,
	created_at`

const ipHistorySelectColumns = `
	ip_history_id,
	vps_id,
	from_ipv4,
	to_ipv4,
	from_ipv6,
	to_ipv6,
	changed_at,
	created_at`

const specSnapshotSelectColumns = `
	snapshot_id,
	vps_id,
	product_name,
	ssh_host,
	ssh_port,
	ssh_user,
	os_name,
	virtualization,
	captured_at,
	created_at`

type renewalDecisionScanner interface {
	Scan(dest ...any) error
}

func scanRenewalDecision(row renewalDecisionScanner) (renewals.DecisionRecord, error) {
	var record renewals.DecisionRecord
	var fromDecision *string
	var toDecision string
	if err := row.Scan(
		&record.DecisionID,
		&record.VPSID,
		&fromDecision,
		&toDecision,
		&record.Reason,
		&record.DecidedAt,
		&record.CreatedAt,
	); err != nil {
		return renewals.DecisionRecord{}, err
	}
	if fromDecision != nil {
		decision := vpsassets.RenewalDecision(*fromDecision)
		record.FromDecision = &decision
	}
	record.ToDecision = vpsassets.RenewalDecision(toDecision)
	return record, nil
}

func scanPriceHistory(row renewalDecisionScanner) (renewals.PriceHistoryRecord, error) {
	var record renewals.PriceHistoryRecord
	var fromRenewAt *time.Time
	var toRenewAt *time.Time
	var fromStatus string
	var toStatus string
	if err := row.Scan(
		&record.PriceHistoryID,
		&record.SubscriptionID,
		&record.VPSID,
		&record.FromPrice,
		&record.ToPrice,
		&record.FromCurrency,
		&record.ToCurrency,
		&record.FromBillingCycle,
		&record.ToBillingCycle,
		&record.FromBillingMonths,
		&record.ToBillingMonths,
		&record.FromMonthlyPrice,
		&record.ToMonthlyPrice,
		&fromRenewAt,
		&toRenewAt,
		&record.FromAutoRenew,
		&record.ToAutoRenew,
		&record.FromAutoRenewCancelled,
		&record.ToAutoRenewCancelled,
		&fromStatus,
		&toStatus,
		&record.ChangedAt,
		&record.CreatedAt,
	); err != nil {
		return renewals.PriceHistoryRecord{}, err
	}
	record.FromRenewAt = subscriptions.DateFromTimePtr(fromRenewAt)
	record.ToRenewAt = subscriptions.DateFromTimePtr(toRenewAt)
	record.FromStatus = subscriptions.Status(fromStatus)
	record.ToStatus = subscriptions.Status(toStatus)
	return record, nil
}

func scanIPHistory(row renewalDecisionScanner) (renewals.IPHistoryRecord, error) {
	var record renewals.IPHistoryRecord
	if err := row.Scan(
		&record.IPHistoryID,
		&record.VPSID,
		&record.FromIPv4,
		&record.ToIPv4,
		&record.FromIPv6,
		&record.ToIPv6,
		&record.ChangedAt,
		&record.CreatedAt,
	); err != nil {
		return renewals.IPHistoryRecord{}, err
	}
	return record, nil
}

func scanSpecSnapshot(row renewalDecisionScanner) (renewals.SpecSnapshotRecord, error) {
	var record renewals.SpecSnapshotRecord
	if err := row.Scan(
		&record.SnapshotID,
		&record.VPSID,
		&record.ProductName,
		&record.SSHHost,
		&record.SSHPort,
		&record.SSHUser,
		&record.OSName,
		&record.Virtualization,
		&record.CapturedAt,
		&record.CreatedAt,
	); err != nil {
		return renewals.SpecSnapshotRecord{}, err
	}
	return record, nil
}

func (r *PostgresRenewalDecisionRepository) CreateRenewalDecision(ctx context.Context, input renewals.CreateDecisionInput) (renewals.DecisionRecord, error) {
	record, err := createRenewalDecision(ctx, r.db, input)
	if err != nil {
		return renewals.DecisionRecord{}, err
	}
	return record, nil
}

func (r *PostgresRenewalDecisionRepository) ListRenewalDecisionsForVPS(ctx context.Context, vpsID string) ([]renewals.DecisionRecord, error) {
	vpsID = renewals.NormalizeVPSID(vpsID)
	if vpsID == "" {
		return nil, fmt.Errorf("%w: vps_id is required", renewals.ErrInvalidRenewalDecisionInput)
	}

	rows, err := r.db.Query(ctx, `
		select `+renewalDecisionSelectColumns+`
		from renewal_decisions
		where vps_id = $1
		order by decided_at desc, created_at desc, decision_id desc`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query renewal decisions for vps %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]renewals.DecisionRecord, 0)
	for rows.Next() {
		record, err := scanRenewalDecision(rows)
		if err != nil {
			return nil, fmt.Errorf("scan renewal decision for vps %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate renewal decisions for vps %q: %w", vpsID, err)
	}
	return records, nil
}

func (r *PostgresRenewalDecisionRepository) CreatePriceHistory(ctx context.Context, input renewals.CreatePriceHistoryInput) (renewals.PriceHistoryRecord, error) {
	record, err := createPriceHistory(ctx, r.db, input)
	if err != nil {
		return renewals.PriceHistoryRecord{}, err
	}
	return record, nil
}

func (r *PostgresRenewalDecisionRepository) ListPriceHistoriesForVPS(ctx context.Context, vpsID string) ([]renewals.PriceHistoryRecord, error) {
	vpsID = renewals.NormalizeVPSID(vpsID)
	if vpsID == "" {
		return nil, fmt.Errorf("%w: vps_id is required", renewals.ErrInvalidAssetHistoryInput)
	}

	rows, err := r.db.Query(ctx, `
		select `+priceHistorySelectColumns+`
		from price_histories
		where vps_id = $1
		order by changed_at desc, created_at desc, price_history_id desc`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query price histories for vps %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]renewals.PriceHistoryRecord, 0)
	for rows.Next() {
		record, err := scanPriceHistory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan price history for vps %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate price histories for vps %q: %w", vpsID, err)
	}
	return records, nil
}

func (r *PostgresRenewalDecisionRepository) CreateIPHistory(ctx context.Context, input renewals.CreateIPHistoryInput) (renewals.IPHistoryRecord, error) {
	record, err := createIPHistory(ctx, r.db, input)
	if err != nil {
		return renewals.IPHistoryRecord{}, err
	}
	return record, nil
}

func (r *PostgresRenewalDecisionRepository) ListIPHistoriesForVPS(ctx context.Context, vpsID string) ([]renewals.IPHistoryRecord, error) {
	vpsID = renewals.NormalizeVPSID(vpsID)
	if vpsID == "" {
		return nil, fmt.Errorf("%w: vps_id is required", renewals.ErrInvalidAssetHistoryInput)
	}

	rows, err := r.db.Query(ctx, `
		select `+ipHistorySelectColumns+`
		from ip_histories
		where vps_id = $1
		order by changed_at desc, created_at desc, ip_history_id desc`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query ip histories for vps %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]renewals.IPHistoryRecord, 0)
	for rows.Next() {
		record, err := scanIPHistory(rows)
		if err != nil {
			return nil, fmt.Errorf("scan ip history for vps %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ip histories for vps %q: %w", vpsID, err)
	}
	return records, nil
}

func (r *PostgresRenewalDecisionRepository) CreateSpecSnapshot(ctx context.Context, input renewals.CreateSpecSnapshotInput) (renewals.SpecSnapshotRecord, error) {
	record, err := createSpecSnapshot(ctx, r.db, input)
	if err != nil {
		return renewals.SpecSnapshotRecord{}, err
	}
	return record, nil
}

func (r *PostgresRenewalDecisionRepository) ListSpecSnapshotsForVPS(ctx context.Context, vpsID string) ([]renewals.SpecSnapshotRecord, error) {
	vpsID = renewals.NormalizeVPSID(vpsID)
	if vpsID == "" {
		return nil, fmt.Errorf("%w: vps_id is required", renewals.ErrInvalidAssetHistoryInput)
	}

	rows, err := r.db.Query(ctx, `
		select `+specSnapshotSelectColumns+`
		from vps_spec_snapshots
		where vps_id = $1
		order by captured_at desc, created_at desc, snapshot_id desc`, vpsID)
	if err != nil {
		return nil, fmt.Errorf("query spec snapshots for vps %q: %w", vpsID, err)
	}
	defer rows.Close()

	records := make([]renewals.SpecSnapshotRecord, 0)
	for rows.Next() {
		record, err := scanSpecSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan spec snapshot for vps %q: %w", vpsID, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate spec snapshots for vps %q: %w", vpsID, err)
	}
	return records, nil
}

func (r *PostgresRenewalDecisionRepository) GetVPSTimeline(ctx context.Context, vpsID string) (renewals.VPSTimeline, error) {
	vpsID = renewals.NormalizeVPSID(vpsID)
	if vpsID == "" {
		return renewals.VPSTimeline{}, fmt.Errorf("%w: vps_id is required", renewals.ErrInvalidRenewalDecisionInput)
	}

	var exists bool
	if err := r.db.QueryRow(ctx, `
		select exists (
			select 1
			from vps_assets
			where vps_id = $1
		)`, vpsID).Scan(&exists); err != nil {
		return renewals.VPSTimeline{}, fmt.Errorf("check vps asset %q for renewal timeline: %w", vpsID, err)
	}
	if !exists {
		return renewals.VPSTimeline{}, renewals.ErrRenewalTimelineNotFound
	}

	records, err := r.ListRenewalDecisionsForVPS(ctx, vpsID)
	if err != nil {
		return renewals.VPSTimeline{}, err
	}
	priceHistories, err := r.ListPriceHistoriesForVPS(ctx, vpsID)
	if err != nil {
		return renewals.VPSTimeline{}, err
	}
	ipHistories, err := r.ListIPHistoriesForVPS(ctx, vpsID)
	if err != nil {
		return renewals.VPSTimeline{}, err
	}
	specSnapshots, err := r.ListSpecSnapshotsForVPS(ctx, vpsID)
	if err != nil {
		return renewals.VPSTimeline{}, err
	}
	return renewals.VPSTimeline{
		VPSID:            vpsID,
		RenewalDecisions: records,
		PriceHistories:   priceHistories,
		IPHistories:      ipHistories,
		SpecSnapshots:    specSnapshots,
	}, nil
}

func createRenewalDecision(ctx context.Context, db renewalDecisionQueryer, input renewals.CreateDecisionInput) (renewals.DecisionRecord, error) {
	input = renewals.NormalizeCreateDecisionInput(input)
	if err := renewals.ValidateCreateDecisionInput(input); err != nil {
		return renewals.DecisionRecord{}, err
	}

	decisionID, err := ids.New("rdec")
	if err != nil {
		return renewals.DecisionRecord{}, fmt.Errorf("generate renewal decision id: %w", err)
	}

	record, err := scanRenewalDecision(db.QueryRow(ctx, `
		insert into renewal_decisions (
			decision_id,
			vps_id,
			from_decision,
			to_decision,
			reason,
			decided_at
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			coalesce($6::timestamptz, now())
		)
		returning `+renewalDecisionSelectColumns,
		decisionID,
		input.VPSID,
		renewalDecisionPtrArg(input.FromDecision),
		string(input.ToDecision),
		input.Reason,
		input.DecidedAt,
	))
	if err != nil {
		return renewals.DecisionRecord{}, mapRenewalDecisionWriteError(err, "create renewal decision for vps %q", input.VPSID)
	}
	return record, nil
}

func createPriceHistory(ctx context.Context, db renewalDecisionQueryer, input renewals.CreatePriceHistoryInput) (renewals.PriceHistoryRecord, error) {
	input = renewals.NormalizeCreatePriceHistoryInput(input)
	if err := renewals.ValidateCreatePriceHistoryInput(input); err != nil {
		return renewals.PriceHistoryRecord{}, err
	}

	priceHistoryID, err := ids.New("ph")
	if err != nil {
		return renewals.PriceHistoryRecord{}, fmt.Errorf("generate price history id: %w", err)
	}

	record, err := scanPriceHistory(db.QueryRow(ctx, `
		insert into price_histories (
			price_history_id,
			subscription_id,
			vps_id,
			from_price,
			to_price,
			from_currency,
			to_currency,
			from_billing_cycle,
			to_billing_cycle,
			from_billing_months,
			to_billing_months,
			from_monthly_price,
			to_monthly_price,
			from_renew_at,
			to_renew_at,
			from_auto_renew,
			to_auto_renew,
			from_auto_renew_cancelled,
			to_auto_renew_cancelled,
			from_status,
			to_status,
			changed_at
		) values (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
			coalesce($22::timestamptz, now())
		)
		returning `+priceHistorySelectColumns,
		priceHistoryID,
		input.To.SubscriptionID,
		input.To.VPSID,
		input.From.Price,
		input.To.Price,
		input.From.Currency,
		input.To.Currency,
		input.From.BillingCycle,
		input.To.BillingCycle,
		input.From.BillingMonths,
		input.To.BillingMonths,
		input.From.MonthlyPrice,
		input.To.MonthlyPrice,
		subscriptionDateArg(input.From.RenewAt),
		subscriptionDateArg(input.To.RenewAt),
		input.From.AutoRenew,
		input.To.AutoRenew,
		input.From.AutoRenewCancelled,
		input.To.AutoRenewCancelled,
		string(input.From.Status),
		string(input.To.Status),
		input.ChangedAt,
	))
	if err != nil {
		return renewals.PriceHistoryRecord{}, mapAssetHistoryWriteError(err, "create price history for subscription %q", input.To.SubscriptionID)
	}
	return record, nil
}

func createIPHistory(ctx context.Context, db renewalDecisionQueryer, input renewals.CreateIPHistoryInput) (renewals.IPHistoryRecord, error) {
	input = renewals.NormalizeCreateIPHistoryInput(input)
	if err := renewals.ValidateCreateIPHistoryInput(input); err != nil {
		return renewals.IPHistoryRecord{}, err
	}

	ipHistoryID, err := ids.New("iph")
	if err != nil {
		return renewals.IPHistoryRecord{}, fmt.Errorf("generate ip history id: %w", err)
	}

	record, err := scanIPHistory(db.QueryRow(ctx, `
		insert into ip_histories (
			ip_history_id,
			vps_id,
			from_ipv4,
			to_ipv4,
			from_ipv6,
			to_ipv6,
			changed_at
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			coalesce($7::timestamptz, now())
		)
		returning `+ipHistorySelectColumns,
		ipHistoryID,
		input.VPSID,
		input.FromIPv4,
		input.ToIPv4,
		input.FromIPv6,
		input.ToIPv6,
		input.ChangedAt,
	))
	if err != nil {
		return renewals.IPHistoryRecord{}, mapAssetHistoryWriteError(err, "create ip history for vps %q", input.VPSID)
	}
	return record, nil
}

func createSpecSnapshot(ctx context.Context, db renewalDecisionQueryer, input renewals.CreateSpecSnapshotInput) (renewals.SpecSnapshotRecord, error) {
	input = renewals.NormalizeCreateSpecSnapshotInput(input)
	if err := renewals.ValidateCreateSpecSnapshotInput(input); err != nil {
		return renewals.SpecSnapshotRecord{}, err
	}

	snapshotID, err := ids.New("vss")
	if err != nil {
		return renewals.SpecSnapshotRecord{}, fmt.Errorf("generate vps spec snapshot id: %w", err)
	}

	record, err := scanSpecSnapshot(db.QueryRow(ctx, `
		insert into vps_spec_snapshots (
			snapshot_id,
			vps_id,
			product_name,
			ssh_host,
			ssh_port,
			ssh_user,
			os_name,
			virtualization,
			captured_at
		) values (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			coalesce($9::timestamptz, now())
		)
		returning `+specSnapshotSelectColumns,
		snapshotID,
		input.VPSID,
		input.ProductName,
		input.SSHHost,
		input.SSHPort,
		input.SSHUser,
		input.OSName,
		input.Virtualization,
		input.CapturedAt,
	))
	if err != nil {
		return renewals.SpecSnapshotRecord{}, mapAssetHistoryWriteError(err, "create vps spec snapshot for vps %q", input.VPSID)
	}
	return record, nil
}

func renewalDecisionPtrArg(value *vpsassets.RenewalDecision) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func mapRenewalDecisionWriteError(err error, format string, args ...any) error {
	return mapAssetHistoryWriteError(err, format, args...)
}

func mapAssetHistoryWriteError(err error, format string, args ...any) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return renewals.ErrAssetTimelineNotFound
		case "23514":
			return renewals.ErrInvalidAssetHistoryInput
		}
	}
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
