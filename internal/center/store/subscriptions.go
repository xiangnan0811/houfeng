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
	"houfeng/internal/center/subscriptions"
)

var _ subscriptions.Repository = (*PostgresSubscriptionRepository)(nil)

type subscriptionDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresSubscriptionRepository struct {
	db subscriptionDB
}

func NewPostgresSubscriptionRepository(db *pgxpool.Pool) *PostgresSubscriptionRepository {
	return &PostgresSubscriptionRepository{db: db}
}

const subscriptionSelectColumns = `
	subscription_id,
	vps_id,
	price,
	currency,
	billing_cycle,
	billing_months,
	monthly_price,
	started_at,
	renew_at,
	auto_renew,
	auto_renew_cancelled,
	status,
	payment_method,
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
	if err := row.Scan(
		&record.SubscriptionID,
		&record.VPSID,
		&record.Price,
		&record.Currency,
		&record.BillingCycle,
		&record.BillingMonths,
		&record.MonthlyPrice,
		&startedAt,
		&renewAt,
		&record.AutoRenew,
		&record.AutoRenewCancelled,
		&record.Status,
		&record.PaymentMethod,
		&record.Note,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return subscriptions.Record{}, err
	}
	record.StartedAt = dateFromScannedTime(startedAt)
	record.RenewAt = dateFromScannedTime(renewAt)
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
			monthly_price,
			started_at,
			renew_at,
			auto_renew,
			auto_renew_cancelled,
			status,
			payment_method,
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
			$14
		)
		returning `+subscriptionSelectColumns,
		subscriptionID,
		input.VPSID,
		input.Price,
		input.Currency,
		input.BillingCycle,
		input.BillingMonths,
		subscriptions.CalculateMonthlyPrice(input.Price, input.BillingMonths),
		subscriptionDateArg(input.StartedAt),
		subscriptionDateArg(input.RenewAt),
		input.AutoRenew,
		input.AutoRenewCancelled,
		string(input.Status),
		input.PaymentMethod,
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

	record, err := scanSubscription(r.db.QueryRow(ctx, `
		update subscriptions
		set vps_id = case when $2::boolean then $3 else vps_id end,
		    price = case when $4::boolean then $5::numeric else price end,
		    currency = case when $6::boolean then $7 else currency end,
		    billing_cycle = case when $8::boolean then $9 else billing_cycle end,
		    billing_months = case when $10::boolean then $11::integer else billing_months end,
		    monthly_price = case
		        when $4::boolean or $10::boolean then (
		            case when $4::boolean then $5::numeric else price end /
		            case when $10::boolean then $11::integer else billing_months end
		        )
		        else monthly_price
		    end,
		    started_at = case when $12::boolean then $13::date else started_at end,
		    renew_at = case when $14::boolean then $15::date else renew_at end,
		    auto_renew = case when $16::boolean then $17 else auto_renew end,
		    auto_renew_cancelled = case when $18::boolean then $19 else auto_renew_cancelled end,
		    status = case when $20::boolean then $21 else status end,
		    payment_method = case when $22::boolean then $23 else payment_method end,
		    note = case when $24::boolean then $25 else note end,
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
		input.StartedAt.Set,
		subscriptionDateArg(input.StartedAt.Value),
		input.RenewAt.Set,
		subscriptionDateArg(input.RenewAt.Value),
		input.AutoRenew.Set,
		input.AutoRenew.Value,
		input.AutoRenewCancelled.Set,
		input.AutoRenewCancelled.Value,
		input.Status.Set,
		string(input.Status.Value),
		input.PaymentMethod.Set,
		input.PaymentMethod.Value,
		input.Note.Set,
		input.Note.Value,
	))
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
