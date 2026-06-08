package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/renewals"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresSubscriptionMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0018_add_subscriptions.sql"))
	if err != nil {
		t.Fatalf("ReadFile(subscription migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists subscriptions",
		"subscription_id text primary key",
		"vps_id text not null references vps_assets(vps_id) on delete cascade",
		"price numeric(12, 2) not null",
		"currency text not null",
		"billing_months integer not null",
		"monthly_price numeric(12, 4) not null",
		"started_at date",
		"renew_at date",
		"auto_renew boolean not null default false",
		"auto_renew_cancelled boolean not null default false",
		"subscriptions_price_non_negative",
		"price >= 0",
		"subscriptions_billing_months_positive",
		"billing_months > 0",
		"subscriptions_currency_code",
		"currency = upper(currency) and currency ~ '^[A-Z]{3}$'",
		"subscriptions_status_allowed",
		"'active', 'paused', 'cancelled', 'expired', 'unknown'",
		"idx_subscriptions_vps",
		"idx_subscriptions_renew_at",
		"idx_subscriptions_status",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("subscription migration missing %q", snippet)
		}
	}

	periodSource, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0031_subscription_periods_and_validity_extension.sql"))
	if err != nil {
		t.Fatalf("ReadFile(subscription period migration) error = %v", err)
	}
	periodText := string(periodSource)
	for _, snippet := range []string{
		"add column if not exists billing_period_unit text not null default 'month'",
		"add column if not exists billing_period_length integer not null default 1",
		"add column if not exists renewal_mode text not null default 'manual'",
		"subscriptions_billing_period_unit_allowed",
		"billing_period_unit in ('day', 'week', 'month', 'year')",
		"subscriptions_billing_period_length_positive",
		"subscriptions_renewal_mode_allowed",
		"renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'bonus', 'other')",
		"add column if not exists from_billing_period_unit text not null default 'month'",
		"add column if not exists to_billing_period_unit text not null default 'month'",
		"add column if not exists from_billing_period_length integer not null default 1",
		"add column if not exists to_billing_period_length integer not null default 1",
		"add column if not exists from_renewal_mode text not null default 'manual'",
		"add column if not exists to_renewal_mode text not null default 'manual'",
		"price_histories_billing_period_unit_allowed",
		"price_histories_billing_period_length_positive",
		"price_histories_renewal_mode_allowed",
		"action_type in ('cancel_vps', 'extend_validity')",
		"'subscription_renew_at'",
	} {
		if !strings.Contains(periodText, snippet) {
			t.Fatalf("subscription period migration missing %q", snippet)
		}
	}
}

func TestPostgresSubscriptionPriceHistoryMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0021_create_asset_histories.sql"))
	if err != nil {
		t.Fatalf("ReadFile(asset history migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists price_histories",
		"price_history_id text primary key",
		"subscription_id text not null references subscriptions(subscription_id) on delete cascade",
		"vps_id text not null references vps_assets(vps_id) on delete cascade",
		"from_price numeric(12, 2) not null",
		"to_price numeric(12, 2) not null",
		"from_monthly_price numeric(12, 4) not null",
		"to_monthly_price numeric(12, 4) not null",
		"price_histories_price_non_negative",
		"price_histories_billing_months_positive",
		"price_histories_currency_code",
		"price_histories_status_allowed",
		"idx_price_histories_vps_time",
		"idx_price_histories_subscription_time",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("asset history migration missing %q", snippet)
		}
	}
}

func TestPostgresSubscriptionCreateListGetAndPatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	startedAt := subscriptions.NewDate(time.Date(2026, time.January, 1, 8, 0, 0, 0, time.UTC))
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC))
	patchedRenewAt := subscriptions.NewDate(time.Date(2026, time.December, 1, 8, 0, 0, 0, time.UTC))
	var queryCalls []string
	var queryArgs [][]any
	var rowCalls []string
	var rowArgs [][]any
	repo := &PostgresSubscriptionRepository{db: fakeSubscriptionDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalls = append(queryCalls, sql)
			queryArgs = append(queryArgs, append([]any(nil), args...))
			return &fakeSubscriptionRows{rows: []fakeSubscriptionScan{
				{scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:      "sub_001",
						VPSID:               "vps_001",
						Price:               120,
						Currency:            "USD",
						BillingCycle:        "annual",
						BillingMonths:       12,
						BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
						BillingPeriodLength: 1,
						MonthlyPrice:        10,
						StartedAt:           &startedAt,
						RenewAt:             &renewAt,
						AutoRenew:           true,
						AutoRenewCancelled:  false,
						Status:              subscriptions.StatusActive,
						PaymentMethod:       "card",
						Note:                "production",
						CreatedAt:           now,
						UpdatedAt:           now,
					})
					return nil
				}},
				{scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:      "sub_002",
						VPSID:               "vps_002",
						Price:               6,
						Currency:            "EUR",
						BillingCycle:        "monthly",
						BillingMonths:       1,
						BillingPeriodUnit:   string(subscriptions.BillingPeriodMonth),
						BillingPeriodLength: 1,
						MonthlyPrice:        6,
						Status:              subscriptions.StatusPaused,
						CreatedAt:           now,
						UpdatedAt:           now,
					})
					return nil
				}},
			}}, nil
		},
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCalls = append(rowCalls, sql)
			rowArgs = append(rowArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "insert into subscriptions"):
				return fakeSubscriptionRow{scan: func(dest ...any) error {
					subscriptionID, ok := args[0].(string)
					if !ok || !strings.HasPrefix(subscriptionID, "sub_") {
						t.Fatalf("generated subscription id arg = %#v, want sub_ prefix", args[0])
					}
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:      subscriptionID,
						VPSID:               "vps_001",
						Price:               120,
						Currency:            "USD",
						BillingCycle:        "annual",
						BillingMonths:       12,
						BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
						BillingPeriodLength: 1,
						MonthlyPrice:        10,
						StartedAt:           &startedAt,
						RenewAt:             &renewAt,
						AutoRenew:           true,
						AutoRenewCancelled:  false,
						Status:              subscriptions.StatusActive,
						PaymentMethod:       "card",
						Note:                "production",
						CreatedAt:           now,
						UpdatedAt:           now,
					})
					return nil
				}}
			case strings.Contains(sql, "where subscription_id = $1") && strings.Contains(sql, "select"):
				return fakeSubscriptionRow{scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:      "sub_001",
						VPSID:               "vps_001",
						Price:               120,
						Currency:            "USD",
						BillingCycle:        "annual",
						BillingMonths:       12,
						BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
						BillingPeriodLength: 1,
						MonthlyPrice:        10,
						StartedAt:           &startedAt,
						RenewAt:             &renewAt,
						Status:              subscriptions.StatusActive,
						CreatedAt:           now,
						UpdatedAt:           now,
					})
					return nil
				}}
			case strings.Contains(sql, "update subscriptions"):
				return fakeSubscriptionRow{scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:      "sub_001",
						VPSID:               "vps_002",
						Price:               240,
						Currency:            "EUR",
						BillingCycle:        "biennial",
						BillingMonths:       24,
						BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
						BillingPeriodLength: 2,
						MonthlyPrice:        10,
						StartedAt:           nil,
						RenewAt:             &patchedRenewAt,
						AutoRenew:           false,
						AutoRenewCancelled:  true,
						Status:              subscriptions.StatusPaused,
						PaymentMethod:       "paypal",
						Note:                "review",
						CreatedAt:           now.Add(-time.Hour),
						UpdatedAt:           now,
					})
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow SQL %q", sql)
				return fakeSubscriptionRow{scan: func(dest ...any) error { return nil }}
			}
		},
	}}

	created, err := repo.CreateSubscription(context.Background(), subscriptions.CreateInput{
		VPSID:              " vps_001 ",
		Price:              120,
		Currency:           " usd ",
		BillingCycle:       " annual ",
		BillingMonths:      12,
		StartedAt:          &startedAt,
		RenewAt:            &renewAt,
		AutoRenew:          true,
		AutoRenewCancelled: false,
		PaymentMethod:      " card ",
		Note:               " production ",
	})
	if err != nil {
		t.Fatalf("CreateSubscription() error = %v", err)
	}
	if !strings.HasPrefix(created.SubscriptionID, "sub_") {
		t.Fatalf("SubscriptionID = %q, want sub_ prefix", created.SubscriptionID)
	}
	if created.Currency != "USD" || created.MonthlyPrice != 10 {
		t.Fatalf("created = %#v, want normalized USD and monthly price", created)
	}
	if len(rowArgs[0]) != 22 {
		t.Fatalf("create args len = %d, want 22", len(rowArgs[0]))
	}
	if rowArgs[0][1] != "vps_001" || rowArgs[0][3] != "USD" || rowArgs[0][6] != string(subscriptions.BillingPeriodMonth) || rowArgs[0][7] != 12 || rowArgs[0][8] != float64(10) || rowArgs[0][14] != string(subscriptions.StatusActive) {
		t.Fatalf("create normalized args = %#v", rowArgs[0])
	}

	list, err := repo.ListSubscriptions(context.Background(), subscriptions.ListFilters{
		VPSID:           " vps_001 ",
		Status:          " active ",
		RenewBefore:     &patchedRenewAt,
		RenewAfter:      &startedAt,
		RenewWithinDays: intPtr(30),
		Sort:            "renew_at",
		Order:           "desc",
		AssetScope:      vpsassets.AssetScopeCurrent,
	})
	if err != nil {
		t.Fatalf("ListSubscriptions() error = %v", err)
	}
	if len(list) != 2 || list[0].SubscriptionID != "sub_001" || list[1].SubscriptionID != "sub_002" {
		t.Fatalf("ListSubscriptions() = %#v, want two records", list)
	}
	for _, snippet := range []string{
		"vps_id = $1",
		"status = $2",
		"renew_at <= $3::date",
		"renew_at >= $4::date",
		"renew_at >= current_date and renew_at <= current_date + $5::integer",
		"exists (",
		"v.lifecycle_status not in ('cancelled', 'archived')",
		"order by renew_at desc nulls last, subscription_id",
	} {
		if !strings.Contains(queryCalls[0], snippet) {
			t.Fatalf("ListSubscriptions SQL missing %q in %q", snippet, queryCalls[0])
		}
	}
	if len(queryArgs[0]) != 5 || queryArgs[0][0] != "vps_001" || queryArgs[0][1] != "active" || queryArgs[0][4] != 30 {
		t.Fatalf("list args = %#v, want normalized filter args", queryArgs[0])
	}

	got, err := repo.GetSubscription(context.Background(), "sub_001")
	if err != nil {
		t.Fatalf("GetSubscription() error = %v", err)
	}
	if got.SubscriptionID != "sub_001" || got.RenewAt == nil || got.RenewAt.Time.Format(subscriptions.DateLayout) != "2026-06-01" {
		t.Fatalf("GetSubscription() = %#v, want subscription with renew_at", got)
	}

	patched, err := repo.PatchSubscription(context.Background(), "sub_001", subscriptions.PatchInput{
		VPSID:         subscriptions.PatchString(" vps_002 "),
		StartedAt:     subscriptions.PatchDate(nil),
		PaymentMethod: subscriptions.PatchString(" paypal "),
		Note:          subscriptions.PatchString(" review "),
	})
	if err != nil {
		t.Fatalf("PatchSubscription() error = %v", err)
	}
	if patched.VPSID != "vps_002" || patched.MonthlyPrice != 10 || patched.StartedAt != nil {
		t.Fatalf("patched = %#v, want patched values and cleared started_at", patched)
	}
	if len(rowCalls) != 3 {
		t.Fatalf("QueryRow calls = %d, want create/get/patch", len(rowCalls))
	}
	patchArgs := rowArgs[2]
	if len(patchArgs) != 41 {
		t.Fatalf("patch args len = %d, want 41", len(patchArgs))
	}
	if patchArgs[0] != "sub_001" || patchArgs[1] != true || patchArgs[2] != "vps_002" {
		t.Fatalf("patch vps args = %#v, want subscription id and vps", patchArgs[:3])
	}
	if patchArgs[3] != false || patchArgs[9] != false || patchArgs[25] != false {
		t.Fatalf("patch tracked history args = %#v, want price/month/status unset in direct patch", patchArgs)
	}
	if patchArgs[15] != true || patchArgs[16] != nil {
		t.Fatalf("patch started_at args = set:%#v value:%#v, want explicit null", patchArgs[15], patchArgs[16])
	}
	for _, snippet := range []string{
		"vps_id = case when $2::boolean then $3 else vps_id end",
		"price = case when $4::boolean then $5::numeric else price end",
		"billing_months = case when $10::boolean then $11::integer else billing_months end",
		"billing_period_unit = case when $12::boolean then $13 else billing_period_unit end",
		"billing_period_length = case when $14::boolean then $15::integer else billing_period_length end",
		"when $4::boolean or $12::boolean or $14::boolean then",
		"started_at = case when $16::boolean then $17::date else started_at end",
		"renew_at = case when $18::boolean then $19::date else renew_at end",
		"renewal_mode = case when $24::boolean then $25 else renewal_mode end",
		"updated_at = now()",
		"where subscription_id = $1",
		"returning " + subscriptionSelectColumns,
	} {
		if !strings.Contains(rowCalls[2], snippet) {
			t.Fatalf("PatchSubscription SQL missing %q in %q", snippet, rowCalls[2])
		}
	}
}

func TestPostgresSubscriptionListAppliesAssetScope(t *testing.T) {
	tests := []struct {
		name      string
		scope     vpsassets.AssetScope
		wantSQL   string
		rejectSQL string
	}{
		{name: "current", scope: vpsassets.AssetScopeCurrent, wantSQL: "v.lifecycle_status not in ('cancelled', 'archived')"},
		{name: "archived", scope: vpsassets.AssetScopeArchived, wantSQL: "v.lifecycle_status in ('cancelled', 'archived')"},
		{name: "all", scope: vpsassets.AssetScopeAll, rejectSQL: "lifecycle_status in ("},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var query string
			repo := &PostgresSubscriptionRepository{db: fakeSubscriptionDB{
				query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
					query = sql
					return &fakeSubscriptionRows{}, nil
				},
			}}

			if _, err := repo.ListSubscriptions(context.Background(), subscriptions.ListFilters{AssetScope: tt.scope}); err != nil {
				t.Fatalf("ListSubscriptions() error = %v", err)
			}
			if tt.wantSQL != "" && !strings.Contains(query, tt.wantSQL) {
				t.Fatalf("query = %q, want %q", query, tt.wantSQL)
			}
			if tt.rejectSQL != "" && strings.Contains(query, tt.rejectSQL) {
				t.Fatalf("query = %q, do not want %q", query, tt.rejectSQL)
			}
		})
	}
}

func TestPostgresSubscriptionPatchWithoutChangesReturnsExistingSubscription(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	queryCount := 0
	repo := &PostgresSubscriptionRepository{db: fakeSubscriptionDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			if strings.Contains(sql, "update subscriptions") {
				t.Fatalf("PatchSubscription without changes should not update: %q", sql)
			}
			if len(args) != 1 || args[0] != "sub_001" {
				t.Fatalf("GetSubscription args = %#v, want subscription id", args)
			}
			return fakeSubscriptionRow{scan: func(dest ...any) error {
				scanSubscriptionRecordDestinations(dest, subscriptions.Record{
					SubscriptionID: "sub_001",
					VPSID:          "vps_001",
					Price:          120,
					Currency:       "USD",
					BillingMonths:  12,
					MonthlyPrice:   10,
					Status:         subscriptions.StatusActive,
					CreatedAt:      now,
					UpdatedAt:      now,
				})
				return nil
			}}
		},
	}}

	record, err := repo.PatchSubscription(context.Background(), "sub_001", subscriptions.PatchInput{})
	if err != nil {
		t.Fatalf("PatchSubscription() error = %v", err)
	}
	if record.SubscriptionID != "sub_001" {
		t.Fatalf("SubscriptionID = %q, want sub_001", record.SubscriptionID)
	}
	if queryCount != 1 {
		t.Fatalf("QueryRow calls = %d, want only get", queryCount)
	}
}

func TestPostgresSubscriptionPatchRecordsPriceHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC))
	patchedRenewAt := subscriptions.NewDate(time.Date(2026, time.December, 1, 8, 0, 0, 0, time.UTC))
	tx := &fakeSubscriptionTx{}
	var calls []string
	var args [][]any
	tx.queryRow = func(_ context.Context, sql string, callArgs ...any) pgx.Row {
		calls = append(calls, sql)
		args = append(args, append([]any(nil), callArgs...))
		switch {
		case strings.Contains(sql, "from subscriptions") && strings.Contains(sql, "for update"):
			return fakeSubscriptionRow{scan: func(dest ...any) error {
				scanSubscriptionRecordDestinations(dest, subscriptions.Record{
					SubscriptionID:      "sub_001",
					VPSID:               "vps_001",
					Price:               120,
					Currency:            "USD",
					BillingCycle:        "annual",
					BillingMonths:       12,
					BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
					BillingPeriodLength: 1,
					MonthlyPrice:        10,
					RenewAt:             &renewAt,
					AutoRenew:           true,
					AutoRenewCancelled:  false,
					RenewalMode:         string(subscriptions.RenewalModeAuto),
					Status:              subscriptions.StatusActive,
					CreatedAt:           now.Add(-time.Hour),
					UpdatedAt:           now.Add(-time.Hour),
				})
				return nil
			}}
		case strings.Contains(sql, "update subscriptions"):
			return fakeSubscriptionRow{scan: func(dest ...any) error {
				scanSubscriptionRecordDestinations(dest, subscriptions.Record{
					SubscriptionID:      "sub_001",
					VPSID:               "vps_001",
					Price:               240,
					Currency:            "USD",
					BillingCycle:        "biennial",
					BillingMonths:       24,
					BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
					BillingPeriodLength: 2,
					MonthlyPrice:        10,
					RenewAt:             &patchedRenewAt,
					AutoRenew:           false,
					AutoRenewCancelled:  true,
					RenewalMode:         string(subscriptions.RenewalModeAutoCancelled),
					Status:              subscriptions.StatusPaused,
					CreatedAt:           now.Add(-time.Hour),
					UpdatedAt:           now,
				})
				return nil
			}}
		case strings.Contains(sql, "insert into price_histories"):
			return fakeSubscriptionRow{scan: func(dest ...any) error {
				priceHistoryID, ok := callArgs[0].(string)
				if !ok || !strings.HasPrefix(priceHistoryID, "ph_") {
					t.Fatalf("price history id arg = %#v, want ph_ prefix", callArgs[0])
				}
				scanPriceHistoryRecordDestinations(dest, priceHistoryFixture(priceHistoryID, now, renewAt, patchedRenewAt))
				return nil
			}}
		default:
			t.Fatalf("unexpected QueryRow SQL %q", sql)
			return fakeSubscriptionRow{scan: func(dest ...any) error { return nil }}
		}
	}

	repo := &PostgresSubscriptionRepository{
		db: fakeSubscriptionDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}
	record, err := repo.PatchSubscription(context.Background(), "sub_001", subscriptions.PatchInput{
		Price:               subscriptions.PatchFloat(240),
		BillingCycle:        subscriptions.PatchString(" biennial "),
		BillingPeriodUnit:   subscriptions.PatchString(" year "),
		BillingPeriodLength: subscriptions.PatchInt(2),
		RenewAt:             subscriptions.PatchDate(&patchedRenewAt),
		RenewalMode:         subscriptions.PatchString("auto_cancelled"),
		Status:              subscriptions.PatchStatus(subscriptions.StatusPaused),
	})
	if err != nil {
		t.Fatalf("PatchSubscription() error = %v", err)
	}
	if record.Price != 240 || record.Status != subscriptions.StatusPaused {
		t.Fatalf("record = %#v, want patched subscription", record)
	}
	if !tx.committed || tx.rolledBack == 0 {
		t.Fatalf("transaction committed=%t rollbackCalls=%d, want committed with deferred rollback", tx.committed, tx.rolledBack)
	}
	if len(calls) != 3 {
		t.Fatalf("query row calls = %d, want lock/update/history", len(calls))
	}
	if !strings.Contains(calls[0], "for update") || !strings.Contains(calls[2], "insert into price_histories") {
		t.Fatalf("calls = %#v, want lock/update/price history", calls)
	}
	historyArgs := args[2]
	if len(historyArgs) != 28 {
		t.Fatalf("history args len = %d, want 28", len(historyArgs))
	}
	if historyArgs[1] != "sub_001" || historyArgs[2] != "vps_001" || historyArgs[3] != float64(120) || historyArgs[4] != float64(240) {
		t.Fatalf("history args = %#v, want from/to price and ids", historyArgs)
	}
	if historyArgs[9] != 12 || historyArgs[10] != 24 || historyArgs[11] != "year" || historyArgs[14] != 2 {
		t.Fatalf("history period args = %#v", historyArgs)
	}
	if historyArgs[23] != "auto" || historyArgs[24] != "auto_cancelled" || historyArgs[25] != "active" || historyArgs[26] != "paused" {
		t.Fatalf("history billing/status args = %#v", historyArgs)
	}
}

func TestPostgresSubscriptionPatchSkipsPriceHistoryWhenTrackedFieldsUnchanged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC))
	tx := &fakeSubscriptionTx{}
	insertedHistory := false
	tx.queryRow = func(_ context.Context, sql string, _ ...any) pgx.Row {
		switch {
		case strings.Contains(sql, "from subscriptions") && strings.Contains(sql, "for update"), strings.Contains(sql, "update subscriptions"):
			return fakeSubscriptionRow{scan: func(dest ...any) error {
				scanSubscriptionRecordDestinations(dest, subscriptions.Record{
					SubscriptionID:      "sub_001",
					VPSID:               "vps_001",
					Price:               120,
					Currency:            "USD",
					BillingCycle:        "annual",
					BillingMonths:       12,
					BillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
					BillingPeriodLength: 1,
					MonthlyPrice:        10,
					RenewAt:             &renewAt,
					AutoRenew:           true,
					AutoRenewCancelled:  false,
					RenewalMode:         string(subscriptions.RenewalModeAuto),
					Status:              subscriptions.StatusActive,
					CreatedAt:           now,
					UpdatedAt:           now,
				})
				return nil
			}}
		case strings.Contains(sql, "insert into price_histories"):
			insertedHistory = true
			return fakeSubscriptionRow{scan: func(dest ...any) error { return nil }}
		default:
			t.Fatalf("unexpected QueryRow SQL %q", sql)
			return fakeSubscriptionRow{scan: func(dest ...any) error { return nil }}
		}
	}

	repo := &PostgresSubscriptionRepository{
		db: fakeSubscriptionDB{},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}
	if _, err := repo.PatchSubscription(context.Background(), "sub_001", subscriptions.PatchInput{Price: subscriptions.PatchFloat(120)}); err != nil {
		t.Fatalf("PatchSubscription() error = %v", err)
	}
	if insertedHistory {
		t.Fatal("PatchSubscription() inserted price history for unchanged tracked values")
	}
}

func TestPostgresSubscriptionMapsNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresSubscriptionRepository{db: fakeSubscriptionDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeSubscriptionRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	if _, err := repo.GetSubscription(context.Background(), "sub_missing"); !errors.Is(err, subscriptions.ErrSubscriptionNotFound) {
		t.Fatalf("GetSubscription() error = %v, want ErrSubscriptionNotFound", err)
	}
	if _, err := repo.PatchSubscription(context.Background(), "sub_missing", subscriptions.PatchInput{Note: subscriptions.PatchString("review")}); !errors.Is(err, subscriptions.ErrSubscriptionNotFound) {
		t.Fatalf("PatchSubscription() error = %v, want ErrSubscriptionNotFound", err)
	}
}

func TestPostgresSubscriptionRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	repo := &PostgresSubscriptionRepository{db: fakeSubscriptionDB{}}
	if _, err := repo.CreateSubscription(context.Background(), subscriptions.CreateInput{VPSID: " ", Price: 1, Currency: "USD", BillingMonths: 1}); !errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
		t.Fatalf("CreateSubscription(blank vps) error = %v, want ErrInvalidSubscriptionInput", err)
	}
	if _, err := repo.CreateSubscription(context.Background(), subscriptions.CreateInput{VPSID: "vps_001", Price: -1, Currency: "USD", BillingMonths: 1}); !errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
		t.Fatalf("CreateSubscription(negative price) error = %v, want ErrInvalidSubscriptionInput", err)
	}
	if _, err := repo.CreateSubscription(context.Background(), subscriptions.CreateInput{VPSID: "vps_001", Price: 12.345, Currency: "USD", BillingMonths: 1}); !errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
		t.Fatalf("CreateSubscription(too precise price) error = %v, want ErrInvalidSubscriptionInput", err)
	}
	if _, err := repo.PatchSubscription(context.Background(), "sub_001", subscriptions.PatchInput{BillingMonths: subscriptions.PatchInt(0)}); !errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
		t.Fatalf("PatchSubscription(invalid months) error = %v, want ErrInvalidSubscriptionInput", err)
	}
	if _, err := repo.PatchSubscription(context.Background(), "sub_001", subscriptions.PatchInput{Price: subscriptions.PatchFloat(12.345)}); !errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
		t.Fatalf("PatchSubscription(too precise price) error = %v, want ErrInvalidSubscriptionInput", err)
	}
	if _, err := repo.ListSubscriptions(context.Background(), subscriptions.ListFilters{Status: "online"}); !errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
		t.Fatalf("ListSubscriptions(invalid filter) error = %v, want ErrInvalidSubscriptionInput", err)
	}
}

func TestPostgresSubscriptionMapsInvalidVPSForeignKey(t *testing.T) {
	t.Parallel()

	fkErr := &pgconn.PgError{Code: "23503", ConstraintName: "subscriptions_vps_id_fkey"}
	repo := &PostgresSubscriptionRepository{db: fakeSubscriptionDB{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if !strings.Contains(sql, "subscriptions") {
				t.Fatalf("unexpected SQL %q", sql)
			}
			return fakeSubscriptionRow{scan: func(dest ...any) error { return fkErr }}
		},
	}}

	_, err := repo.CreateSubscription(context.Background(), subscriptions.CreateInput{
		VPSID:         "vps_missing",
		Price:         12,
		Currency:      "USD",
		BillingMonths: 1,
	})
	if !errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
		t.Fatalf("CreateSubscription() error = %v, want ErrInvalidSubscriptionInput", err)
	}

	_, err = repo.PatchSubscription(context.Background(), "sub_001", subscriptions.PatchInput{
		VPSID: subscriptions.PatchString("vps_missing"),
	})
	if !errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
		t.Fatalf("PatchSubscription() error = %v, want ErrInvalidSubscriptionInput", err)
	}
}

type fakeSubscriptionDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

type fakeSubscriptionTx struct {
	queryRow   func(context.Context, string, ...any) pgx.Row
	query      func(context.Context, string, ...any) (pgx.Rows, error)
	exec       func(context.Context, string, ...any) (pgconn.CommandTag, error)
	committed  bool
	rolledBack int
}

func (f *fakeSubscriptionTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeSubscriptionTx) Commit(context.Context) error {
	f.committed = true
	return nil
}
func (f *fakeSubscriptionTx) Rollback(context.Context) error {
	f.rolledBack++
	return nil
}
func (f *fakeSubscriptionTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeSubscriptionTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeSubscriptionTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (f *fakeSubscriptionTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (f *fakeSubscriptionTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec != nil {
		return f.exec(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (f *fakeSubscriptionTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query != nil {
		return f.query(ctx, sql, args...)
	}
	return nil, nil
}
func (f *fakeSubscriptionTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow != nil {
		return f.queryRow(ctx, sql, args...)
	}
	return fakeSubscriptionRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
}
func (f *fakeSubscriptionTx) Conn() *pgx.Conn { return nil }

func (f fakeSubscriptionDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeSubscriptionRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeSubscriptionDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeSubscriptionRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeSubscriptionRow struct {
	scan func(dest ...any) error
}

func (r fakeSubscriptionRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeSubscriptionScan struct {
	scan func(dest ...any) error
}

type fakeSubscriptionRows struct {
	rows []fakeSubscriptionScan
	idx  int
	err  error
}

func (f *fakeSubscriptionRows) Close()                                       {}
func (f *fakeSubscriptionRows) Err() error                                   { return f.err }
func (f *fakeSubscriptionRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeSubscriptionRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeSubscriptionRows) RawValues() [][]byte                          { return nil }
func (f *fakeSubscriptionRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeSubscriptionRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeSubscriptionRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeSubscriptionRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func scanSubscriptionRecordDestinations(dest []any, record subscriptions.Record) {
	*(dest[0].(*string)) = record.SubscriptionID
	*(dest[1].(*string)) = record.VPSID
	*(dest[2].(*float64)) = record.Price
	*(dest[3].(*string)) = record.Currency
	*(dest[4].(*string)) = record.BillingCycle
	*(dest[5].(*int)) = record.BillingMonths
	*(dest[6].(*string)) = record.BillingPeriodUnit
	*(dest[7].(*int)) = record.BillingPeriodLength
	*(dest[8].(*float64)) = record.MonthlyPrice
	*(dest[9].(**time.Time)) = cloneTimePtr(subscriptions.TimePtrFromDate(record.StartedAt))
	*(dest[10].(**time.Time)) = cloneTimePtr(subscriptions.TimePtrFromDate(record.RenewAt))
	*(dest[11].(*bool)) = record.AutoRenew
	*(dest[12].(*bool)) = record.AutoRenewCancelled
	*(dest[13].(*string)) = record.RenewalMode
	*(dest[14].(*subscriptions.Status)) = record.Status
	*(dest[15].(*string)) = record.PaymentMethod
	*(dest[16].(*string)) = record.DisplayName
	*(dest[17].(*string)) = record.CostCategory
	*(dest[18].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[19].(**time.Time)) = cloneTimePtr(subscriptions.TimePtrFromDate(record.TrialEndsAt))
	*(dest[20].(**time.Time)) = cloneTimePtr(subscriptions.TimePtrFromDate(record.EndsAt))
	*(dest[21].(*string)) = record.Note
	*(dest[22].(*time.Time)) = record.CreatedAt
	*(dest[23].(*time.Time)) = record.UpdatedAt
}

func scanPriceHistoryRecordDestinations(dest []any, record renewals.PriceHistoryRecord) {
	*(dest[0].(*string)) = record.PriceHistoryID
	*(dest[1].(*string)) = record.SubscriptionID
	*(dest[2].(*string)) = record.VPSID
	*(dest[3].(*float64)) = record.FromPrice
	*(dest[4].(*float64)) = record.ToPrice
	*(dest[5].(*string)) = record.FromCurrency
	*(dest[6].(*string)) = record.ToCurrency
	*(dest[7].(*string)) = record.FromBillingCycle
	*(dest[8].(*string)) = record.ToBillingCycle
	*(dest[9].(*int)) = record.FromBillingMonths
	*(dest[10].(*int)) = record.ToBillingMonths
	*(dest[11].(*string)) = record.FromBillingPeriodUnit
	*(dest[12].(*string)) = record.ToBillingPeriodUnit
	*(dest[13].(*int)) = record.FromBillingPeriodLength
	*(dest[14].(*int)) = record.ToBillingPeriodLength
	*(dest[15].(*float64)) = record.FromMonthlyPrice
	*(dest[16].(*float64)) = record.ToMonthlyPrice
	*(dest[17].(**time.Time)) = cloneTimePtr(subscriptions.TimePtrFromDate(record.FromRenewAt))
	*(dest[18].(**time.Time)) = cloneTimePtr(subscriptions.TimePtrFromDate(record.ToRenewAt))
	*(dest[19].(*bool)) = record.FromAutoRenew
	*(dest[20].(*bool)) = record.ToAutoRenew
	*(dest[21].(*bool)) = record.FromAutoRenewCancelled
	*(dest[22].(*bool)) = record.ToAutoRenewCancelled
	*(dest[23].(*string)) = record.FromRenewalMode
	*(dest[24].(*string)) = record.ToRenewalMode
	*(dest[25].(*string)) = string(record.FromStatus)
	*(dest[26].(*string)) = string(record.ToStatus)
	*(dest[27].(*time.Time)) = record.ChangedAt
	*(dest[28].(*time.Time)) = record.CreatedAt
}

func priceHistoryFixture(id string, now time.Time, fromRenewAt, toRenewAt subscriptions.Date) renewals.PriceHistoryRecord {
	return renewals.PriceHistoryRecord{
		PriceHistoryID:          id,
		SubscriptionID:          "sub_001",
		VPSID:                   "vps_001",
		FromPrice:               120,
		ToPrice:                 240,
		FromCurrency:            "USD",
		ToCurrency:              "USD",
		FromBillingCycle:        "annual",
		ToBillingCycle:          "biennial",
		FromBillingMonths:       12,
		ToBillingMonths:         24,
		FromBillingPeriodUnit:   string(subscriptions.BillingPeriodYear),
		ToBillingPeriodUnit:     string(subscriptions.BillingPeriodYear),
		FromBillingPeriodLength: 1,
		ToBillingPeriodLength:   2,
		FromMonthlyPrice:        10,
		ToMonthlyPrice:          10,
		FromRenewAt:             &fromRenewAt,
		ToRenewAt:               &toRenewAt,
		FromAutoRenew:           true,
		ToAutoRenew:             false,
		FromAutoRenewCancelled:  false,
		ToAutoRenewCancelled:    true,
		FromRenewalMode:         string(subscriptions.RenewalModeAuto),
		ToRenewalMode:           string(subscriptions.RenewalModeAutoCancelled),
		FromStatus:              subscriptions.StatusActive,
		ToStatus:                subscriptions.StatusPaused,
		ChangedAt:               now,
		CreatedAt:               now,
	}
}
