package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ids"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptioncosts"
	"houfeng/internal/center/subscriptions"
)

var _ subscriptioncosts.Repository = (*PostgresSubscriptionCostRepository)(nil)

type subscriptionCostDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type PostgresSubscriptionCostRepository struct {
	db subscriptionCostDB
}

func NewPostgresSubscriptionCostRepository(db *pgxpool.Pool) *PostgresSubscriptionCostRepository {
	return &PostgresSubscriptionCostRepository{db: db}
}

func (r *PostgresSubscriptionCostRepository) ListCostRows(ctx context.Context, settings centersettings.SubscriptionCostSettings) ([]subscriptioncosts.CostRow, error) {
	rows, err := r.db.Query(ctx, `
		with latest_rates as (
			select distinct on (provider, base_currency, quote_currency)
				provider,
				base_currency,
				quote_currency,
				rate,
				rate_date,
				fetched_at
			from subscription_exchange_rates
			where provider = $1
			  and base_currency = $2
			order by provider, base_currency, quote_currency, fetched_at desc, rate_date desc
		),
			reminder_windows as (
				select
					s.subscription_id,
					s.renew_at,
					o.offset_days,
					(s.renew_at - o.offset_days)::timestamp at time zone 'UTC' as reminder_at
				from subscriptions s
				cross join unnest($4::integer[]) as o(offset_days)
				where s.status = 'active'
				  and s.renew_at is not null
				  and s.renew_at >= current_date
				  and s.renew_at - o.offset_days >= current_date
			),
			next_reminders as (
				select
					rw.subscription_id,
					min(rw.reminder_at) as next_reminder_at
				from reminder_windows rw
				where not exists (
					select 1
					from subscription_reminder_deliveries d
					where d.subscription_id = rw.subscription_id
					  and d.renew_at = rw.renew_at
					  and d.offset_days = rw.offset_days
				)
				group by rw.subscription_id
			)
		select
			s.subscription_id,
			s.vps_id,
			v.display_name,
			coalesce(v.provider_id, ''),
			coalesce(nullif(v.provider_name, ''), p.name, ''),
			coalesce(nullif(s.display_name, ''), v.display_name),
			s.cost_category,
			s.labels,
			s.price::float8,
			s.currency,
			s.monthly_price::float8,
			case
				when s.currency = $2 then s.monthly_price::float8
				when lr.rate is not null then round((s.monthly_price * lr.rate)::numeric, 4)::float8
				else null::float8
			end,
			case
				when s.currency = $2 then round((s.monthly_price * 12)::numeric, 4)::float8
				when lr.rate is not null then round((s.monthly_price * lr.rate * 12)::numeric, 4)::float8
				else null::float8
			end,
			$2::text,
			case when s.currency = $2 then 1::float8 else lr.rate::float8 end,
			case when s.currency = $2 then current_date else lr.rate_date end,
			case
				when s.currency = $2 then false
				when lr.rate is null then true
				when lr.fetched_at < now() - ($3::integer * interval '1 hour') then true
				else false
			end,
			s.renew_at,
			nr.next_reminder_at,
			s.status,
			s.payment_method,
			v.country,
			v.region,
			v.lifecycle_status,
			v.renewal_decision
		from subscriptions s
		join vps_assets v on v.vps_id = s.vps_id
		left join providers p on p.provider_id = v.provider_id
		left join latest_rates lr on lr.quote_currency = s.currency
		left join next_reminders nr on nr.subscription_id = s.subscription_id
		where s.status = 'active'
		  and v.lifecycle_status not in ('cancelled', 'archived')
		order by s.renew_at asc nulls last, s.subscription_id asc`,
		settings.ExchangeRateProvider,
		settings.BaseCurrency,
		settings.ExchangeRateStaleAfterHours,
		settings.DefaultReminderOffsetsDays,
	)
	if err != nil {
		return nil, fmt.Errorf("query subscription cost rows: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptioncosts.CostRow, 0)
	for rows.Next() {
		var record subscriptioncosts.CostRow
		var labels []string
		var monthlyPriceBase pgtype.Float8
		var yearlyPriceBase pgtype.Float8
		var exchangeRate pgtype.Float8
		var exchangeRateDate *time.Time
		var renewAt *time.Time
		if err := rows.Scan(
			&record.SubscriptionID,
			&record.VPSID,
			&record.VPSDisplayName,
			&record.ProviderID,
			&record.ProviderName,
			&record.DisplayName,
			&record.CostCategory,
			&labels,
			&record.Price,
			&record.Currency,
			&record.MonthlyPrice,
			&monthlyPriceBase,
			&yearlyPriceBase,
			&record.BaseCurrency,
			&exchangeRate,
			&exchangeRateDate,
			&record.ExchangeRateStale,
			&renewAt,
			&record.NextReminderAt,
			&record.Status,
			&record.PaymentMethod,
			&record.Country,
			&record.Region,
			&record.LifecycleStatus,
			&record.RenewalDecision,
		); err != nil {
			return nil, fmt.Errorf("scan subscription cost row: %w", err)
		}
		record.Labels = labels
		record.MonthlyPriceBase = nullableFloat(monthlyPriceBase)
		record.YearlyPriceBase = nullableFloat(yearlyPriceBase)
		record.ExchangeRate = nullableFloat(exchangeRate)
		record.ExchangeRateDate = subscriptions.DateFromTimePtr(exchangeRateDate)
		record.RenewAt = subscriptions.DateFromTimePtr(renewAt)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscription cost rows: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionCostRepository) ListCostMonthBuckets(ctx context.Context, settings centersettings.SubscriptionCostSettings, months int, now time.Time) ([]subscriptioncosts.SeriesPoint, error) {
	if months <= 0 {
		return []subscriptioncosts.SeriesPoint{}, nil
	}
	rows, err := r.db.Query(ctx, `
		with buckets as (
			select generate_series(
				date_trunc('month', $3::timestamptz) - (($4::integer - 1) * interval '1 month'),
				date_trunc('month', $3::timestamptz),
				interval '1 month'
			)::date as bucket_start
		),
		states as (
			select
				b.bucket_start,
				s.subscription_id,
				coalesce(
					(
						select h.to_monthly_price::float8
						from price_histories h
						where h.subscription_id = s.subscription_id
						  and h.changed_at < (b.bucket_start + interval '1 month')
						order by h.changed_at desc, h.created_at desc, h.price_history_id desc
						limit 1
					),
					(
						select h.from_monthly_price::float8
						from price_histories h
						where h.subscription_id = s.subscription_id
						  and h.changed_at >= (b.bucket_start + interval '1 month')
						order by h.changed_at asc, h.created_at asc, h.price_history_id asc
						limit 1
					),
					s.monthly_price::float8
				) as monthly_price,
				coalesce(
					(
						select h.to_currency
						from price_histories h
						where h.subscription_id = s.subscription_id
						  and h.changed_at < (b.bucket_start + interval '1 month')
						order by h.changed_at desc, h.created_at desc, h.price_history_id desc
						limit 1
					),
					(
						select h.from_currency
						from price_histories h
						where h.subscription_id = s.subscription_id
						  and h.changed_at >= (b.bucket_start + interval '1 month')
						order by h.changed_at asc, h.created_at asc, h.price_history_id asc
						limit 1
					),
					s.currency
				) as currency,
				coalesce(
					(
						select h.to_status
						from price_histories h
						where h.subscription_id = s.subscription_id
						  and h.changed_at < (b.bucket_start + interval '1 month')
						order by h.changed_at desc, h.created_at desc, h.price_history_id desc
						limit 1
					),
					(
						select h.from_status
						from price_histories h
						where h.subscription_id = s.subscription_id
						  and h.changed_at >= (b.bucket_start + interval '1 month')
						order by h.changed_at asc, h.created_at asc, h.price_history_id asc
						limit 1
					),
					s.status
				) as status
			from buckets b
			join subscriptions s on s.created_at < (b.bucket_start + interval '1 month')
			join vps_assets v on v.vps_id = s.vps_id
			where v.lifecycle_status not in ('cancelled', 'archived')
		)
		select
			to_char(b.bucket_start, 'YYYY-MM') as bucket,
			coalesce(round(sum(
				case
					when st.status <> 'active' then 0
					when st.currency = $2 then st.monthly_price
					when lr.rate is not null then st.monthly_price * lr.rate
					else null
				end
			)::numeric, 4)::float8, 0::float8) as monthly_cost,
			count(*) filter (
				where st.status = 'active'
				  and st.currency <> $2
				  and lr.rate is null
			) > 0 as data_insufficient
		from buckets b
		left join states st on st.bucket_start = b.bucket_start
		left join lateral (
			select er.rate::float8
			from subscription_exchange_rates er
			where er.provider = $1
			  and er.base_currency = $2
			  and er.quote_currency = st.currency
			  and er.fetched_at < (b.bucket_start + interval '1 month')
			order by er.fetched_at desc, er.rate_date desc
			limit 1
		) lr on st.currency <> $2
		group by b.bucket_start
		order by b.bucket_start asc`,
		settings.ExchangeRateProvider,
		settings.BaseCurrency,
		now.UTC(),
		months,
	)
	if err != nil {
		return nil, fmt.Errorf("query subscription cost month buckets: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptioncosts.SeriesPoint, 0, months)
	for rows.Next() {
		var record subscriptioncosts.SeriesPoint
		if err := rows.Scan(&record.Bucket, &record.MonthlyCost, &record.DataInsufficient); err != nil {
			return nil, fmt.Errorf("scan subscription cost month bucket: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscription cost month buckets: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionCostRepository) ListBudgetMonthBuckets(ctx context.Context, settings centersettings.SubscriptionCostSettings, months int, now time.Time) ([]subscriptioncosts.SeriesPoint, error) {
	if months <= 0 {
		return []subscriptioncosts.SeriesPoint{}, nil
	}
	rows, err := r.db.Query(ctx, `
		with buckets as (
			select generate_series(
				date_trunc('month', $1::timestamptz) - (($2::integer - 1) * interval '1 month'),
				date_trunc('month', $1::timestamptz),
				interval '1 month'
			)::date as bucket_start
		)
		select
			to_char(b.bucket_start, 'YYYY-MM') as bucket,
			mb.monthly_limit::float8,
			coalesce(mb.base_currency, '') as budget_currency,
			coalesce(mb.warning_pct, 0) as warning_pct,
			mb.base_currency is not null and mb.base_currency <> $3 as currency_mismatch
		from buckets b
		left join lateral (
			select
				budget_month,
				base_currency,
				monthly_limit,
				warning_pct
			from subscription_monthly_budgets
			where budget_month <= b.bucket_start
			order by budget_month desc
			limit 1
		) mb on true
		order by b.bucket_start asc`,
		now.UTC(),
		months,
		settings.BaseCurrency,
	)
	if err != nil {
		return nil, fmt.Errorf("query subscription budget month buckets: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptioncosts.SeriesPoint, 0, months)
	for rows.Next() {
		var record subscriptioncosts.SeriesPoint
		var budgetLimit pgtype.Float8
		var currencyMismatch bool
		if err := rows.Scan(
			&record.Bucket,
			&budgetLimit,
			&record.BudgetCurrency,
			&record.BudgetWarningPct,
			&currencyMismatch,
		); err != nil {
			return nil, fmt.Errorf("scan subscription budget month bucket: %w", err)
		}
		record.BudgetLimit = nullableFloat(budgetLimit)
		if currencyMismatch {
			record.DataInsufficient = true
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscription budget month buckets: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionCostRepository) ListMissingSubscriptionAssets(ctx context.Context) ([]subscriptioncosts.MissingSubscriptionAsset, error) {
	rows, err := r.db.Query(ctx, `
		select
			v.vps_id,
			v.display_name,
			coalesce(v.provider_id, ''),
			coalesce(nullif(v.provider_name, ''), p.name, ''),
			v.lifecycle_status,
			v.renewal_decision
		from vps_assets v
		left join providers p on p.provider_id = v.provider_id
		where v.lifecycle_status not in ('archived', 'cancelled')
		  and not exists (
			select 1
			from subscriptions s
			where s.vps_id = v.vps_id
			  and s.status = 'active'
		  )
		order by v.display_name asc, v.vps_id asc`)
	if err != nil {
		return nil, fmt.Errorf("query vps assets missing subscription: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptioncosts.MissingSubscriptionAsset, 0)
	for rows.Next() {
		var record subscriptioncosts.MissingSubscriptionAsset
		if err := rows.Scan(
			&record.VPSID,
			&record.DisplayName,
			&record.ProviderID,
			&record.ProviderName,
			&record.LifecycleStatus,
			&record.RenewalDecision,
		); err != nil {
			return nil, fmt.Errorf("scan missing subscription asset row: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missing subscription assets: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionCostRepository) ListBudgets(ctx context.Context, filters subscriptioncosts.BudgetListFilters) ([]subscriptioncosts.BudgetRecord, error) {
	filters = subscriptioncosts.NormalizeBudgetListFilters(filters)
	if err := subscriptioncosts.ValidateBudgetListFilters(filters); err != nil {
		return nil, err
	}
	args := []any{}
	conditions := []string{}
	if filters.ScopeType != "" {
		args = append(args, filters.ScopeType)
		conditions = append(conditions, fmt.Sprintf("scope_type = $%d", len(args)))
	}
	if filters.ScopeID != "" {
		args = append(args, filters.ScopeID)
		conditions = append(conditions, fmt.Sprintf("scope_id = $%d", len(args)))
	}
	if filters.Enabled != nil {
		args = append(args, *filters.Enabled)
		conditions = append(conditions, fmt.Sprintf("enabled = $%d", len(args)))
	}
	query := `
		select
			budget_id,
			scope_type,
			scope_id,
			name,
			base_currency,
			monthly_limit::float8,
			yearly_limit::float8,
			warning_pct,
			enabled,
			note,
			created_at,
			updated_at
		from subscription_budgets`
	if len(conditions) > 0 {
		query += " where " + strings.Join(conditions, " and ")
	}
	query += " order by enabled desc, scope_type asc, name asc, budget_id asc"

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query subscription budgets: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptioncosts.BudgetRecord, 0)
	for rows.Next() {
		record, err := scanSubscriptionBudget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan subscription budget: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscription budgets: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionCostRepository) CreateBudget(ctx context.Context, input subscriptioncosts.CreateBudgetInput) (subscriptioncosts.BudgetRecord, error) {
	input = subscriptioncosts.NormalizeCreateBudgetInput(input)
	if err := subscriptioncosts.ValidateCreateBudgetInput(input); err != nil {
		return subscriptioncosts.BudgetRecord{}, err
	}
	budgetID, err := ids.New("sub_budget")
	if err != nil {
		return subscriptioncosts.BudgetRecord{}, fmt.Errorf("generate subscription budget id: %w", err)
	}
	record, err := scanSubscriptionBudget(r.db.QueryRow(ctx, `
		insert into subscription_budgets (
			budget_id,
			scope_type,
			scope_id,
			name,
			base_currency,
			monthly_limit,
			yearly_limit,
			warning_pct,
			enabled,
			note
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		returning
			budget_id,
			scope_type,
			scope_id,
			name,
			base_currency,
			monthly_limit::float8,
			yearly_limit::float8,
			warning_pct,
			enabled,
			note,
			created_at,
			updated_at`,
		budgetID,
		input.ScopeType,
		input.ScopeID,
		input.Name,
		input.BaseCurrency,
		input.MonthlyLimit,
		input.YearlyLimit,
		input.WarningPct,
		input.Enabled,
		input.Note,
	))
	if err != nil {
		if isSubscriptionCostInvalidPostgresError(err) {
			return subscriptioncosts.BudgetRecord{}, subscriptioncosts.ErrInvalidInput
		}
		return subscriptioncosts.BudgetRecord{}, fmt.Errorf("create subscription budget: %w", err)
	}
	return record, nil
}

func (r *PostgresSubscriptionCostRepository) PatchBudget(ctx context.Context, input subscriptioncosts.PatchBudgetInput) (subscriptioncosts.BudgetRecord, error) {
	input = subscriptioncosts.NormalizePatchBudgetInput(input)
	if err := subscriptioncosts.ValidatePatchBudgetInput(input); err != nil {
		return subscriptioncosts.BudgetRecord{}, err
	}
	if !input.HasChanges() {
		record, err := scanSubscriptionBudget(r.db.QueryRow(ctx, `
			select
				budget_id,
				scope_type,
				scope_id,
				name,
				base_currency,
				monthly_limit::float8,
				yearly_limit::float8,
				warning_pct,
				enabled,
				note,
				created_at,
				updated_at
			from subscription_budgets
			where budget_id = $1`, input.BudgetID))
		if errors.Is(err, pgx.ErrNoRows) {
			return subscriptioncosts.BudgetRecord{}, subscriptioncosts.ErrBudgetNotFound
		}
		return record, err
	}
	record, err := scanSubscriptionBudget(r.db.QueryRow(ctx, `
		update subscription_budgets
		set scope_type = case when $2::boolean then $3 else scope_type end,
		    scope_id = case when $4::boolean then $5 else scope_id end,
		    name = case when $6::boolean then $7 else name end,
		    base_currency = case when $8::boolean then $9 else base_currency end,
		    monthly_limit = case when $10::boolean then $11::numeric else monthly_limit end,
		    yearly_limit = case when $12::boolean then $13::numeric else yearly_limit end,
		    warning_pct = case when $14::boolean then $15::integer else warning_pct end,
		    enabled = case when $16::boolean then $17 else enabled end,
		    note = case when $18::boolean then $19 else note end,
		    updated_at = now()
		where budget_id = $1
		returning
			budget_id,
			scope_type,
			scope_id,
			name,
			base_currency,
			monthly_limit::float8,
			yearly_limit::float8,
			warning_pct,
			enabled,
			note,
			created_at,
			updated_at`,
		input.BudgetID,
		input.ScopeType.Set,
		input.ScopeType.Value,
		input.ScopeID.Set,
		input.ScopeID.Value,
		input.Name.Set,
		input.Name.Value,
		input.BaseCurrency.Set,
		input.BaseCurrency.Value,
		input.MonthlyLimit.Set,
		input.MonthlyLimit.Value,
		input.YearlyLimit.Set,
		input.YearlyLimit.Value,
		input.WarningPct.Set,
		input.WarningPct.Value,
		input.Enabled.Set,
		input.Enabled.Value,
		input.Note.Set,
		input.Note.Value,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return subscriptioncosts.BudgetRecord{}, subscriptioncosts.ErrBudgetNotFound
	}
	if err != nil {
		if isSubscriptionCostInvalidPostgresError(err) {
			return subscriptioncosts.BudgetRecord{}, subscriptioncosts.ErrInvalidInput
		}
		return subscriptioncosts.BudgetRecord{}, fmt.Errorf("patch subscription budget %q: %w", input.BudgetID, err)
	}
	return record, nil
}

func (r *PostgresSubscriptionCostRepository) ListMonthlyBudgets(ctx context.Context) ([]subscriptioncosts.MonthlyBudgetRecord, error) {
	rows, err := r.db.Query(ctx, `
		select
			budget_month,
			base_currency,
			monthly_limit::float8,
			warning_pct,
			note,
			created_at,
			updated_at
		from subscription_monthly_budgets
		order by budget_month desc`)
	if err != nil {
		return nil, fmt.Errorf("query subscription monthly budgets: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptioncosts.MonthlyBudgetRecord, 0)
	for rows.Next() {
		record, err := scanSubscriptionMonthlyBudget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan subscription monthly budget: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscription monthly budgets: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionCostRepository) UpsertMonthlyBudget(ctx context.Context, input subscriptioncosts.UpsertMonthlyBudgetInput) (subscriptioncosts.MonthlyBudgetRecord, error) {
	input = subscriptioncosts.NormalizeUpsertMonthlyBudgetInput(input)
	if err := subscriptioncosts.ValidateUpsertMonthlyBudgetInput(input); err != nil {
		return subscriptioncosts.MonthlyBudgetRecord{}, err
	}
	record, err := scanSubscriptionMonthlyBudget(r.db.QueryRow(ctx, `
		insert into subscription_monthly_budgets (
			budget_month,
			base_currency,
			monthly_limit,
			warning_pct,
			note
		) values ($1,$2,$3,$4,$5)
		on conflict (budget_month) do update
		set base_currency = excluded.base_currency,
		    monthly_limit = excluded.monthly_limit,
		    warning_pct = excluded.warning_pct,
		    note = excluded.note,
		    updated_at = now()
		returning
			budget_month,
			base_currency,
			monthly_limit::float8,
			warning_pct,
			note,
			created_at,
			updated_at`,
		input.BudgetMonth.Time,
		input.BaseCurrency,
		input.MonthlyLimit,
		input.WarningPct,
		input.Note,
	))
	if err != nil {
		if isSubscriptionCostInvalidPostgresError(err) {
			return subscriptioncosts.MonthlyBudgetRecord{}, subscriptioncosts.ErrInvalidInput
		}
		return subscriptioncosts.MonthlyBudgetRecord{}, fmt.Errorf("upsert subscription monthly budget %s: %w", input.BudgetMonth.Time.Format("2006-01-02"), err)
	}
	return record, nil
}

func (r *PostgresSubscriptionCostRepository) EarliestSubscriptionMonth(ctx context.Context) (*subscriptions.Date, error) {
	var month pgtype.Date
	err := r.db.QueryRow(ctx, `
		with source_dates as (
			select created_at::date as source_date from subscriptions
			union all
			select started_at from subscriptions where started_at is not null
			union all
			select renew_at from subscriptions where renew_at is not null
			union all
			select changed_at::date from price_histories
		)
		select date_trunc('month', min(source_date))::date
		from source_dates
		where source_date is not null`).Scan(&month)
	if err != nil {
		return nil, fmt.Errorf("query earliest subscription month: %w", err)
	}
	if !month.Valid {
		return nil, nil
	}
	date := subscriptions.NewDate(month.Time)
	return &date, nil
}

func (r *PostgresSubscriptionCostRepository) UpsertMonthlyBudgets(ctx context.Context, inputs []subscriptioncosts.UpsertMonthlyBudgetInput) ([]subscriptioncosts.MonthlyBudgetRecord, error) {
	if len(inputs) == 0 {
		return []subscriptioncosts.MonthlyBudgetRecord{}, nil
	}
	months := make([]time.Time, 0, len(inputs))
	baseCurrencies := make([]string, 0, len(inputs))
	monthlyLimits := make([]float64, 0, len(inputs))
	warningPcts := make([]int, 0, len(inputs))
	notes := make([]string, 0, len(inputs))
	for _, input := range inputs {
		input = subscriptioncosts.NormalizeUpsertMonthlyBudgetInput(input)
		if err := subscriptioncosts.ValidateUpsertMonthlyBudgetInput(input); err != nil {
			return nil, err
		}
		months = append(months, input.BudgetMonth.Time)
		baseCurrencies = append(baseCurrencies, input.BaseCurrency)
		monthlyLimits = append(monthlyLimits, input.MonthlyLimit)
		warningPcts = append(warningPcts, input.WarningPct)
		notes = append(notes, input.Note)
	}

	rows, err := r.db.Query(ctx, `
		with input_rows as (
			select *
			from unnest($1::date[], $2::text[], $3::double precision[], $4::integer[], $5::text[]) as i(
				budget_month,
				base_currency,
				monthly_limit,
				warning_pct,
				note
			)
		),
		upserted as (
			insert into subscription_monthly_budgets (
				budget_month,
				base_currency,
				monthly_limit,
				warning_pct,
				note
			)
			select
				budget_month,
				base_currency,
				monthly_limit,
				warning_pct,
				note
			from input_rows
			on conflict (budget_month) do update
			set base_currency = excluded.base_currency,
			    monthly_limit = excluded.monthly_limit,
			    warning_pct = excluded.warning_pct,
			    note = excluded.note,
			    updated_at = now()
			returning
				budget_month,
				base_currency,
				monthly_limit::float8,
				warning_pct,
				note,
				created_at,
				updated_at
		)
		select
			budget_month,
			base_currency,
			monthly_limit::float8,
			warning_pct,
			note,
			created_at,
			updated_at
		from upserted
		order by budget_month asc`,
		months,
		baseCurrencies,
		monthlyLimits,
		warningPcts,
		notes,
	)
	if err != nil {
		if isSubscriptionCostInvalidPostgresError(err) {
			return nil, subscriptioncosts.ErrInvalidInput
		}
		return nil, fmt.Errorf("bulk upsert subscription monthly budgets: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptioncosts.MonthlyBudgetRecord, 0, len(inputs))
	for rows.Next() {
		record, err := scanSubscriptionMonthlyBudget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan bulk subscription monthly budget: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bulk subscription monthly budgets: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionCostRepository) ListActiveCurrencies(ctx context.Context) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		select distinct currency
		from subscriptions
		join vps_assets v on v.vps_id = subscriptions.vps_id
		where status = 'active'
		  and v.lifecycle_status not in ('cancelled', 'archived')
		order by currency asc`)
	if err != nil {
		return nil, fmt.Errorf("query active subscription currencies: %w", err)
	}
	defer rows.Close()
	currencies := make([]string, 0)
	for rows.Next() {
		var currency string
		if err := rows.Scan(&currency); err != nil {
			return nil, fmt.Errorf("scan active subscription currency: %w", err)
		}
		currencies = append(currencies, currency)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active subscription currencies: %w", err)
	}
	return currencies, nil
}

func (r *PostgresSubscriptionCostRepository) UpsertExchangeRate(ctx context.Context, input subscriptioncosts.ExchangeRateUpsert) (subscriptioncosts.ExchangeRateRecord, error) {
	rateID, err := ids.New("sub_rate")
	if err != nil {
		return subscriptioncosts.ExchangeRateRecord{}, fmt.Errorf("generate subscription exchange rate id: %w", err)
	}
	record, err := scanSubscriptionExchangeRate(r.db.QueryRow(ctx, `
		insert into subscription_exchange_rates (
			rate_id,
			provider,
			base_currency,
			quote_currency,
			rate,
			rate_date,
			fetched_at,
			error_summary
		) values ($1,$2,$3,$4,$5,$6,$7,$8)
		on conflict (provider, base_currency, quote_currency, rate_date) do update
		set rate = excluded.rate,
		    fetched_at = excluded.fetched_at,
		    error_summary = excluded.error_summary,
		    updated_at = now()
		returning
			rate_id,
			provider,
			base_currency,
			quote_currency,
			rate::float8,
			rate_date,
			fetched_at,
			false,
			error_summary,
			created_at,
			updated_at`,
		rateID,
		input.Provider,
		input.BaseCurrency,
		input.QuoteCurrency,
		input.Rate,
		input.RateDate.Time,
		input.FetchedAt,
		input.ErrorSummary,
	))
	if err != nil {
		if isSubscriptionCostInvalidPostgresError(err) {
			return subscriptioncosts.ExchangeRateRecord{}, subscriptioncosts.ErrInvalidInput
		}
		return subscriptioncosts.ExchangeRateRecord{}, fmt.Errorf("upsert subscription exchange rate: %w", err)
	}
	return record, nil
}

func (r *PostgresSubscriptionCostRepository) ListReminderCandidates(ctx context.Context, settings centersettings.SubscriptionCostSettings, offsets []int) ([]subscriptioncosts.ReminderCandidate, error) {
	if len(offsets) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		with offsets(offset_days) as (
			select unnest($1::integer[])
		),
		latest_rates as (
			select distinct on (provider, base_currency, quote_currency)
				provider,
				base_currency,
				quote_currency,
				rate,
				fetched_at,
				rate_date
			from subscription_exchange_rates
			where provider = $2
			  and base_currency = $3
			order by provider, base_currency, quote_currency, fetched_at desc, rate_date desc
		),
		candidates as (
			select
				s.subscription_id,
				s.vps_id,
				v.display_name as vps_display_name,
				coalesce(nullif(s.display_name, ''), v.display_name) as display_name,
				coalesce(nullif(v.provider_name, ''), p.name, '') as provider_name,
				s.renew_at,
				o.offset_days,
				case
					when v.renewal_decision in ('cancel', 'auto_renew_cancelled', 'migrate')
					  or v.lifecycle_status in ('to_cancel', 'to_migrate')
					  or s.auto_renew_cancelled
					then 'decision_attention'
					else 'renewal'
				end as reminder_kind,
				$3::text as base_currency,
				case
					when s.currency = $3 then s.monthly_price::float8
					when lr.rate is not null then round((s.monthly_price * lr.rate)::numeric, 4)::float8
					else null::float8
				end as monthly_price_base,
				v.renewal_decision,
				v.lifecycle_status
			from subscriptions s
			join offsets o on s.renew_at = current_date + o.offset_days
			join vps_assets v on v.vps_id = s.vps_id
			left join providers p on p.provider_id = v.provider_id
			left join latest_rates lr on lr.quote_currency = s.currency
			where s.status = 'active'
			  and v.lifecycle_status not in ('archived', 'cancelled')
		)
		select
			subscription_id,
			vps_id,
			vps_display_name,
			display_name,
			provider_name,
			renew_at,
			offset_days,
			reminder_kind,
			base_currency,
			monthly_price_base,
			renewal_decision,
			lifecycle_status
		from candidates
		order by renew_at asc, offset_days desc, subscription_id asc`,
		offsets,
		settings.ExchangeRateProvider,
		settings.BaseCurrency,
	)
	if err != nil {
		return nil, fmt.Errorf("query subscription reminder candidates: %w", err)
	}
	defer rows.Close()

	records := make([]subscriptioncosts.ReminderCandidate, 0)
	for rows.Next() {
		var record subscriptioncosts.ReminderCandidate
		var renewAt time.Time
		var kind string
		var monthlyPriceBase pgtype.Float8
		if err := rows.Scan(
			&record.SubscriptionID,
			&record.VPSID,
			&record.VPSDisplayName,
			&record.DisplayName,
			&record.ProviderName,
			&renewAt,
			&record.OffsetDays,
			&kind,
			&record.BaseCurrency,
			&monthlyPriceBase,
			&record.RenewalDecision,
			&record.LifecycleStatus,
		); err != nil {
			return nil, fmt.Errorf("scan subscription reminder candidate: %w", err)
		}
		record.RenewAt = subscriptions.NewDate(renewAt)
		record.Kind = subscriptioncosts.ReminderKind(kind)
		record.MonthlyPriceBase = nullableFloat(monthlyPriceBase)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscription reminder candidates: %w", err)
	}
	return records, nil
}

func (r *PostgresSubscriptionCostRepository) TryCreateReminderDelivery(ctx context.Context, input subscriptioncosts.ReminderDeliveryInput) (string, bool, error) {
	deliveryID, err := ids.New("sub_reminder")
	if err != nil {
		return "", false, fmt.Errorf("generate subscription reminder delivery id: %w", err)
	}
	var insertedID string
	err = r.db.QueryRow(ctx, `
		insert into subscription_reminder_deliveries (
			delivery_id,
			subscription_id,
			vps_id,
			renew_at,
			offset_days,
			reminder_kind,
			channel,
			delivery_status,
			summary,
			sent_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
			on conflict (subscription_id, renew_at, offset_days) do nothing
		returning delivery_id`,
		deliveryID,
		input.SubscriptionID,
		input.VPSID,
		input.RenewAt.Time,
		input.OffsetDays,
		string(input.Kind),
		input.Channel,
		input.Status,
		input.Summary,
		input.SentAt,
	).Scan(&insertedID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		if isSubscriptionCostInvalidPostgresError(err) {
			return "", false, subscriptioncosts.ErrInvalidInput
		}
		return "", false, fmt.Errorf("insert subscription reminder delivery: %w", err)
	}
	return insertedID, true, nil
}

func (r *PostgresSubscriptionCostRepository) UpdateReminderDelivery(ctx context.Context, deliveryID string, input subscriptioncosts.ReminderDeliveryUpdate) error {
	if _, err := r.db.Exec(ctx, `
		update subscription_reminder_deliveries
		set delivery_status = $2,
		    summary = $3,
		    sent_at = $4,
		    updated_at = now()
		where delivery_id = $1`,
		deliveryID,
		input.Status,
		input.Summary,
		input.SentAt,
	); err != nil {
		return fmt.Errorf("update subscription reminder delivery %q: %w", deliveryID, err)
	}
	return nil
}

func scanSubscriptionBudget(row subscriptionBudgetScanner) (subscriptioncosts.BudgetRecord, error) {
	var record subscriptioncosts.BudgetRecord
	var monthlyLimit pgtype.Float8
	var yearlyLimit pgtype.Float8
	if err := row.Scan(
		&record.BudgetID,
		&record.ScopeType,
		&record.ScopeID,
		&record.Name,
		&record.BaseCurrency,
		&monthlyLimit,
		&yearlyLimit,
		&record.WarningPct,
		&record.Enabled,
		&record.Note,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return subscriptioncosts.BudgetRecord{}, err
	}
	record.MonthlyLimit = nullableFloat(monthlyLimit)
	record.YearlyLimit = nullableFloat(yearlyLimit)
	record.Status = subscriptioncosts.BudgetStatusUnknown
	return record, nil
}

type subscriptionBudgetScanner interface {
	Scan(...any) error
}

func scanSubscriptionMonthlyBudget(row subscriptionMonthlyBudgetScanner) (subscriptioncosts.MonthlyBudgetRecord, error) {
	var record subscriptioncosts.MonthlyBudgetRecord
	var budgetMonth time.Time
	if err := row.Scan(
		&budgetMonth,
		&record.BaseCurrency,
		&record.MonthlyLimit,
		&record.WarningPct,
		&record.Note,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return subscriptioncosts.MonthlyBudgetRecord{}, err
	}
	record.BudgetMonth = subscriptions.NewDate(budgetMonth)
	return record, nil
}

type subscriptionMonthlyBudgetScanner interface {
	Scan(...any) error
}

func scanSubscriptionExchangeRate(row subscriptionExchangeRateScanner) (subscriptioncosts.ExchangeRateRecord, error) {
	var record subscriptioncosts.ExchangeRateRecord
	var rateDate time.Time
	if err := row.Scan(
		&record.RateID,
		&record.Provider,
		&record.BaseCurrency,
		&record.QuoteCurrency,
		&record.Rate,
		&rateDate,
		&record.FetchedAt,
		&record.Stale,
		&record.ErrorSummary,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return subscriptioncosts.ExchangeRateRecord{}, err
	}
	record.RateDate = subscriptions.NewDate(rateDate)
	return record, nil
}

type subscriptionExchangeRateScanner interface {
	Scan(...any) error
}

func isSubscriptionCostInvalidPostgresError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23503" || pgErr.Code == "23505" || pgErr.Code == "23514"
}

func nullableFloat(value pgtype.Float8) *float64 {
	if !value.Valid {
		return nil
	}
	floatValue := value.Float64
	return &floatValue
}
