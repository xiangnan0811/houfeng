package store

import (
	"context"
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
)

var _ subscriptions.Repository = (*PostgresSubscriptionRepository)(nil)

type subscriptionDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type subscriptionQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresSubscriptionRepository struct {
	db      subscriptionDB
	beginTx func(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func NewPostgresSubscriptionRepository(db *pgxpool.Pool) *PostgresSubscriptionRepository {
	return &PostgresSubscriptionRepository{
		db:      db,
		beginTx: db.BeginTx,
	}
}

const subscriptionSelectColumns = `
	subscription_id,
	vps_id,
	price,
	currency,
	billing_cycle,
	billing_months,
	billing_period_unit,
	billing_period_length,
	monthly_price,
	started_at,
	renew_at,
	auto_renew,
	auto_renew_cancelled,
	renewal_mode,
	status,
	payment_method,
	display_name,
	cost_category,
	labels,
	trial_ends_at,
	ends_at,
	note,
	created_at,
	updated_at`

type subscriptionScanner interface {
	Scan(dest ...any) error
}

func scanSubscription(row subscriptionScanner) (subscriptions.Record, error) {
	var record subscriptions.Record
	var startedAt *time.Time
	var renewAt *time.Time
	var trialEndsAt *time.Time
	var endsAt *time.Time
	if err := row.Scan(
		&record.SubscriptionID,
		&record.VPSID,
		&record.Price,
		&record.Currency,
		&record.BillingCycle,
		&record.BillingMonths,
		&record.BillingPeriodUnit,
		&record.BillingPeriodLength,
		&record.MonthlyPrice,
		&startedAt,
		&renewAt,
		&record.AutoRenew,
		&record.AutoRenewCancelled,
		&record.RenewalMode,
		&record.Status,
		&record.PaymentMethod,
		&record.DisplayName,
		&record.CostCategory,
		&record.Labels,
		&trialEndsAt,
		&endsAt,
		&record.Note,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return subscriptions.Record{}, err
	}
	record.StartedAt = dateFromScannedTime(startedAt)
	record.RenewAt = dateFromScannedTime(renewAt)
	record.TrialEndsAt = dateFromScannedTime(trialEndsAt)
	record.EndsAt = dateFromScannedTime(endsAt)
	return record, nil
}

func dateFromScannedTime(value *time.Time) *subscriptions.Date {
	return subscriptions.DateFromTimePtr(value)
}

func (r *PostgresSubscriptionRepository) ListSubscriptions(ctx context.Context, filters subscriptions.ListFilters) ([]subscriptions.Record, error) {
	filters = subscriptions.NormalizeListFilters(filters)
	if err := subscriptions.ValidateListFilters(filters); err != nil {
		return nil, err
	}

	args := []any{}
	conditions := []string{}
	if filters.VPSID != "" {
		args = append(args, filters.VPSID)
		conditions = append(conditions, fmt.Sprintf("vps_id = $%d", len(args)))
	}
	if filters.Status != "" {
		args = append(args, string(filters.Status))
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if filters.RenewBefore != nil {
		args = append(args, subscriptionDateArg(filters.RenewBefore))
		conditions = append(conditions, fmt.Sprintf("renew_at <= $%d::date", len(args)))
	}
	if filters.RenewAfter != nil {
		args = append(args, subscriptionDateArg(filters.RenewAfter))
		conditions = append(conditions, fmt.Sprintf("renew_at >= $%d::date", len(args)))
	}
	if filters.RenewWithinDays != nil {
		args = append(args, *filters.RenewWithinDays)
		conditions = append(conditions, fmt.Sprintf("renew_at >= current_date and renew_at <= current_date + $%d::integer", len(args)))
	}
	if filters.Currency != "" {
		args = append(args, filters.Currency)
		conditions = append(conditions, fmt.Sprintf("currency = $%d", len(args)))
	}
	if filters.ProviderID != "" {
		args = append(args, filters.ProviderID)
		conditions = append(conditions, fmt.Sprintf(`exists (
			select 1 from vps_assets v where v.vps_id = subscriptions.vps_id and v.provider_id = $%d
		)`, len(args)))
	}
	if filters.AutoRenew != nil {
		args = append(args, *filters.AutoRenew)
		conditions = append(conditions, fmt.Sprintf("auto_renew = $%d", len(args)))
	}
	if filters.PaymentMethod != "" {
		args = append(args, filters.PaymentMethod)
		conditions = append(conditions, fmt.Sprintf("payment_method = $%d", len(args)))
	}
	if filters.Label != "" {
		args = append(args, filters.Label)
		conditions = append(conditions, fmt.Sprintf("labels @> array[$%d]::text[]", len(args)))
	}
	if filters.RenewalDecision != "" {
		args = append(args, filters.RenewalDecision)
		conditions = append(conditions, fmt.Sprintf(`exists (
			select 1 from vps_assets v where v.vps_id = subscriptions.vps_id and v.renewal_decision = $%d
		)`, len(args)))
	}
	switch filters.AssetScope {
	case vpsassets.AssetScopeArchived, vpsassets.AssetScopeHistorical:
		conditions = append(conditions, `exists (
			select 1 from vps_assets v where v.vps_id = subscriptions.vps_id and v.lifecycle_status in ('cancelled', 'archived')
		)`)
	case vpsassets.AssetScopeAll, "":
	default:
		conditions = append(conditions, `exists (
			select 1 from vps_assets v where v.vps_id = subscriptions.vps_id and v.lifecycle_status not in ('cancelled', 'archived')
		)`)
	}

	query := `
		select ` + subscriptionSelectColumns + `
		from subscriptions`
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by renew_at " + filters.Order + " nulls last, subscription_id"

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptions.Record, 0)
	for rows.Next() {
		record, err := scanSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionRepository) GetSubscription(ctx context.Context, subscriptionID string) (subscriptions.Record, error) {
	record, err := scanSubscription(r.db.QueryRow(ctx, `
		select `+subscriptionSelectColumns+`
		from subscriptions
		where subscription_id = $1`, subscriptionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return subscriptions.Record{}, subscriptions.ErrSubscriptionNotFound
	}
	if err != nil {
		return subscriptions.Record{}, fmt.Errorf("query subscription %q: %w", subscriptionID, err)
	}
	return record, nil
}

func (r *PostgresSubscriptionRepository) CreateSubscription(ctx context.Context, input subscriptions.CreateInput) (subscriptions.Record, error) {
	input = subscriptions.NormalizeCreateInput(input)
	if err := subscriptions.ValidateCreateInput(input); err != nil {
		return subscriptions.Record{}, err
	}

	subscriptionID, err := ids.New("sub")
	if err != nil {
		return subscriptions.Record{}, fmt.Errorf("generate subscription id: %w", err)
	}

	record, err := scanSubscription(r.db.QueryRow(ctx, `
		insert into subscriptions (
			subscription_id,
			vps_id,
			price,
			currency,
			billing_cycle,
			billing_months,
			billing_period_unit,
			billing_period_length,
			monthly_price,
			started_at,
			renew_at,
			auto_renew,
			auto_renew_cancelled,
			renewal_mode,
			status,
			payment_method,
			display_name,
			cost_category,
			labels,
			trial_ends_at,
			ends_at,
			note
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
			$22
		)
		returning `+subscriptionSelectColumns,
		subscriptionID,
		input.VPSID,
		input.Price,
		input.Currency,
		input.BillingCycle,
		input.BillingMonths,
		input.BillingPeriodUnit,
		input.BillingPeriodLength,
		subscriptions.CalculateMonthlyPriceForPeriod(input.Price, input.BillingPeriodUnit, input.BillingPeriodLength),
		subscriptionDateArg(input.StartedAt),
		subscriptionDateArg(input.RenewAt),
		input.AutoRenew,
		input.AutoRenewCancelled,
		input.RenewalMode,
		string(input.Status),
		input.PaymentMethod,
		input.DisplayName,
		input.CostCategory,
		input.Labels,
		subscriptionDateArg(input.TrialEndsAt),
		subscriptionDateArg(input.EndsAt),
		input.Note,
	))
	if err != nil {
		if isSubscriptionInvalidPostgresError(err) {
			return subscriptions.Record{}, subscriptions.ErrInvalidSubscriptionInput
		}
		return subscriptions.Record{}, fmt.Errorf("create subscription: %w", err)
	}
	return record, nil
}

func (r *PostgresSubscriptionRepository) PatchSubscription(ctx context.Context, subscriptionID string, input subscriptions.PatchInput) (subscriptions.Record, error) {
	input = subscriptions.NormalizePatchInput(input)
	if err := subscriptions.ValidatePatchInput(input); err != nil {
		return subscriptions.Record{}, err
	}
	if !input.HasChanges() {
		return r.GetSubscription(ctx, subscriptionID)
	}

	if patchRequiresPriceHistory(input) {
		return r.patchSubscriptionWithPriceHistory(ctx, subscriptionID, input)
	}

	record, err := patchSubscriptionRow(ctx, r.db, subscriptionID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		return subscriptions.Record{}, subscriptions.ErrSubscriptionNotFound
	}
	if err != nil {
		if isSubscriptionInvalidPostgresError(err) {
			return subscriptions.Record{}, subscriptions.ErrInvalidSubscriptionInput
		}
		return subscriptions.Record{}, fmt.Errorf("patch subscription %q: %w", subscriptionID, err)
	}
	return record, nil
}

func (r *PostgresSubscriptionRepository) patchSubscriptionWithPriceHistory(ctx context.Context, subscriptionID string, input subscriptions.PatchInput) (subscriptions.Record, error) {
	if r.beginTx == nil {
		return subscriptions.Record{}, errors.New("subscription repository cannot record price history without transaction support")
	}

	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return subscriptions.Record{}, fmt.Errorf("begin subscription price history transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanSubscription(tx.QueryRow(ctx, `
		select `+subscriptionSelectColumns+`
		from subscriptions
		where subscription_id = $1
		for update`, subscriptionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return subscriptions.Record{}, subscriptions.ErrSubscriptionNotFound
	}
	if err != nil {
		return subscriptions.Record{}, fmt.Errorf("query subscription %q before price history patch: %w", subscriptionID, err)
	}

	record, err := patchSubscriptionRow(ctx, tx, subscriptionID, input)
	if errors.Is(err, pgx.ErrNoRows) {
		return subscriptions.Record{}, subscriptions.ErrSubscriptionNotFound
	}
	if err != nil {
		if isSubscriptionInvalidPostgresError(err) {
			return subscriptions.Record{}, subscriptions.ErrInvalidSubscriptionInput
		}
		return subscriptions.Record{}, fmt.Errorf("patch subscription %q: %w", subscriptionID, err)
	}

	if subscriptionPriceHistoryChanged(current, record) {
		if _, err := createPriceHistory(ctx, tx, renewals.CreatePriceHistoryInput{
			From: current,
			To:   record,
		}); err != nil {
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) || errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				return subscriptions.Record{}, subscriptions.ErrInvalidSubscriptionInput
			}
			return subscriptions.Record{}, fmt.Errorf("record price history for subscription %q: %w", subscriptionID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return subscriptions.Record{}, fmt.Errorf("commit subscription price history transaction: %w", err)
	}
	return record, nil
}

func patchSubscriptionRow(ctx context.Context, db subscriptionQueryer, subscriptionID string, input subscriptions.PatchInput) (subscriptions.Record, error) {
	return scanSubscription(db.QueryRow(ctx, `
		update subscriptions
		set vps_id = case when $2::boolean then $3 else vps_id end,
		    price = case when $4::boolean then $5::numeric else price end,
		    currency = case when $6::boolean then $7 else currency end,
		    billing_cycle = case when $8::boolean then $9 else billing_cycle end,
		    billing_months = case when $10::boolean then $11::integer else billing_months end,
		    billing_period_unit = case when $12::boolean then $13 else billing_period_unit end,
		    billing_period_length = case when $14::boolean then $15::integer else billing_period_length end,
		    monthly_price = case
		        when $4::boolean or $12::boolean or $14::boolean then round((
		            (case when $4::boolean then $5::numeric else price end) /
		            case (case when $12::boolean then $13 else billing_period_unit end)
		                when 'day' then (case when $14::boolean then $15::integer else billing_period_length end)::numeric / 30
		                when 'week' then ((case when $14::boolean then $15::integer else billing_period_length end)::numeric * 7) / 30
		                when 'year' then (case when $14::boolean then $15::integer else billing_period_length end)::numeric * 12
		                else (case when $14::boolean then $15::integer else billing_period_length end)::numeric
		            end
		        ) * 10000) / 10000
		        else monthly_price
		    end,
		    started_at = case when $16::boolean then $17::date else started_at end,
		    renew_at = case when $18::boolean then $19::date else renew_at end,
		    auto_renew = case when $20::boolean then $21 else auto_renew end,
		    auto_renew_cancelled = case when $22::boolean then $23 else auto_renew_cancelled end,
		    renewal_mode = case when $24::boolean then $25 else renewal_mode end,
		    status = case when $26::boolean then $27 else status end,
		    payment_method = case when $28::boolean then $29 else payment_method end,
		    display_name = case when $30::boolean then $31 else display_name end,
		    cost_category = case when $32::boolean then $33 else cost_category end,
		    labels = case when $34::boolean then $35::text[] else labels end,
		    trial_ends_at = case when $36::boolean then $37::date else trial_ends_at end,
		    ends_at = case when $38::boolean then $39::date else ends_at end,
		    note = case when $40::boolean then $41 else note end,
		    updated_at = now()
		where subscription_id = $1
		returning `+subscriptionSelectColumns,
		subscriptionID,
		input.VPSID.Set,
		input.VPSID.Value,
		input.Price.Set,
		input.Price.Value,
		input.Currency.Set,
		input.Currency.Value,
		input.BillingCycle.Set,
		input.BillingCycle.Value,
		input.BillingMonths.Set,
		input.BillingMonths.Value,
		input.BillingPeriodUnit.Set,
		input.BillingPeriodUnit.Value,
		input.BillingPeriodLength.Set,
		input.BillingPeriodLength.Value,
		input.StartedAt.Set,
		subscriptionDateArg(input.StartedAt.Value),
		input.RenewAt.Set,
		subscriptionDateArg(input.RenewAt.Value),
		input.AutoRenew.Set,
		input.AutoRenew.Value,
		input.AutoRenewCancelled.Set,
		input.AutoRenewCancelled.Value,
		input.RenewalMode.Set,
		input.RenewalMode.Value,
		input.Status.Set,
		string(input.Status.Value),
		input.PaymentMethod.Set,
		input.PaymentMethod.Value,
		input.DisplayName.Set,
		input.DisplayName.Value,
		input.CostCategory.Set,
		input.CostCategory.Value,
		input.Labels.Set,
		input.Labels.Values,
		input.TrialEndsAt.Set,
		subscriptionDateArg(input.TrialEndsAt.Value),
		input.EndsAt.Set,
		subscriptionDateArg(input.EndsAt.Value),
		input.Note.Set,
		input.Note.Value,
	))
}

func patchRequiresPriceHistory(input subscriptions.PatchInput) bool {
	return input.Price.Set ||
		input.Currency.Set ||
		input.BillingCycle.Set ||
		input.BillingMonths.Set ||
		input.BillingPeriodUnit.Set ||
		input.BillingPeriodLength.Set ||
		input.RenewAt.Set ||
		input.AutoRenew.Set ||
		input.AutoRenewCancelled.Set ||
		input.RenewalMode.Set ||
		input.Status.Set
}

func subscriptionPriceHistoryChanged(from, to subscriptions.Record) bool {
	return from.Price != to.Price ||
		from.Currency != to.Currency ||
		from.BillingCycle != to.BillingCycle ||
		from.BillingMonths != to.BillingMonths ||
		from.BillingPeriodUnit != to.BillingPeriodUnit ||
		from.BillingPeriodLength != to.BillingPeriodLength ||
		from.MonthlyPrice != to.MonthlyPrice ||
		!sameSubscriptionDate(from.RenewAt, to.RenewAt) ||
		from.AutoRenew != to.AutoRenew ||
		from.AutoRenewCancelled != to.AutoRenewCancelled ||
		from.RenewalMode != to.RenewalMode ||
		from.Status != to.Status
}

func sameSubscriptionDate(left, right *subscriptions.Date) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.Time.Equal(right.Time)
}

func isSubscriptionInvalidPostgresError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503" || pgErr.Code == "23514"
}

func subscriptionDateArg(value *subscriptions.Date) any {
	if value == nil {
		return nil
	}
	return value.Time
}
