package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/store/migrate"
	"houfeng/internal/center/subscriptioncosts"
	"houfeng/internal/center/subscriptions"
)

func TestPostgresSubscriptionCostRepositoryListCostMonthBucketsMarksInsufficientData(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 9, 0, 0, 0, time.UTC)
	var seenSQL string
	var seenArgs []any
	repo := &PostgresSubscriptionCostRepository{db: fakeSubscriptionCostDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			seenSQL = sql
			seenArgs = append([]any(nil), args...)
			return &fakeSubscriptionCostRows{rows: []fakeSubscriptionCostScan{
				{scan: func(dest ...any) error {
					if len(dest) != 3 {
						t.Fatalf("scan destinations = %d, want 3", len(dest))
					}
					*(dest[0].(*string)) = "2025-07"
					*(dest[1].(*float64)) = 90
					*(dest[2].(*bool)) = false
					return nil
				}},
				{scan: func(dest ...any) error {
					if len(dest) != 3 {
						t.Fatalf("scan destinations = %d, want 3", len(dest))
					}
					*(dest[0].(*string)) = "2025-08"
					*(dest[1].(*float64)) = 0
					*(dest[2].(*bool)) = true
					return nil
				}},
			}}, nil
		},
	}}

	got, err := repo.ListCostMonthBuckets(context.Background(), centersettings.SubscriptionCostSettings{
		BaseCurrency:         "CNY",
		ExchangeRateProvider: "frankfurter",
	}, 12, now)
	if err != nil {
		t.Fatalf("ListCostMonthBuckets() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListCostMonthBuckets() returned %d rows, want 2", len(got))
	}
	if got[0].Bucket != "2025-07" || got[0].MonthlyCost != 90 || got[0].DataInsufficient {
		t.Fatalf("first bucket = %#v, want complete 2025-07 monthly cost", got[0])
	}
	if got[1].Bucket != "2025-08" || !got[1].DataInsufficient {
		t.Fatalf("second bucket = %#v, want data_insufficient true", got[1])
	}
	if len(seenArgs) != 4 || seenArgs[0] != "frankfurter" || seenArgs[1] != "CNY" || seenArgs[3] != 12 {
		t.Fatalf("query args = %#v, want provider/base/now/months", seenArgs)
	}
	if !seenArgs[2].(time.Time).Equal(now.UTC()) {
		t.Fatalf("now arg = %v, want %v", seenArgs[2], now.UTC())
	}
	for _, snippet := range []string{
		"price_histories",
		"to_monthly_price",
		"from_monthly_price",
		"to_currency",
		"from_currency",
		"to_status",
		"from_status",
		"data_insufficient",
		"lr.rate is null",
		"else null",
	} {
		if !strings.Contains(seenSQL, snippet) {
			t.Fatalf("ListCostMonthBuckets SQL missing %q in %s", snippet, seenSQL)
		}
	}
	if strings.Contains(seenSQL, "else 0\n") {
		t.Fatalf("ListCostMonthBuckets SQL still collapses missing exchange rates to 0: %s", seenSQL)
	}
}

func TestPostgresSubscriptionCostRepositoryListCostMonthBucketsSkipsQueryForInvalidWindow(t *testing.T) {
	t.Parallel()

	repo := &PostgresSubscriptionCostRepository{db: fakeSubscriptionCostDB{
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			t.Fatal("Query() should not be called for non-positive months")
			return nil, nil
		},
	}}

	got, err := repo.ListCostMonthBuckets(context.Background(), centersettings.SubscriptionCostSettings{}, 0, time.Now())
	if err != nil {
		t.Fatalf("ListCostMonthBuckets() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ListCostMonthBuckets() = %#v, want empty slice", got)
	}
}

func TestPostgresSubscriptionCostRepositoryListBudgetMonthBucketsCarriesForward(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 2, 9, 0, 0, 0, time.UTC)
	var seenSQL string
	var seenArgs []any
	repo := &PostgresSubscriptionCostRepository{db: fakeSubscriptionCostDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			seenSQL = sql
			seenArgs = append([]any(nil), args...)
			return &fakeSubscriptionCostRows{rows: []fakeSubscriptionCostScan{
				{scan: func(dest ...any) error {
					if len(dest) != 5 {
						t.Fatalf("scan destinations = %d, want 5", len(dest))
					}
					*(dest[0].(*string)) = "2026-05"
					*(dest[1].(*pgtype.Float8)) = pgtype.Float8{Float64: 100, Valid: true}
					*(dest[2].(*string)) = "CNY"
					*(dest[3].(*int)) = 80
					*(dest[4].(*bool)) = false
					return nil
				}},
				{scan: func(dest ...any) error {
					if len(dest) != 5 {
						t.Fatalf("scan destinations = %d, want 5", len(dest))
					}
					*(dest[0].(*string)) = "2026-06"
					*(dest[1].(*pgtype.Float8)) = pgtype.Float8{Float64: 120, Valid: true}
					*(dest[2].(*string)) = "USD"
					*(dest[3].(*int)) = 75
					*(dest[4].(*bool)) = true
					return nil
				}},
			}}, nil
		},
	}}

	got, err := repo.ListBudgetMonthBuckets(context.Background(), centersettings.SubscriptionCostSettings{BaseCurrency: "CNY"}, 2, now)
	if err != nil {
		t.Fatalf("ListBudgetMonthBuckets() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListBudgetMonthBuckets() returned %d rows, want 2", len(got))
	}
	if got[0].Bucket != "2026-05" || got[0].BudgetLimit == nil || *got[0].BudgetLimit != 100 || got[0].BudgetCurrency != "CNY" || got[0].DataInsufficient {
		t.Fatalf("first budget bucket = %#v, want carried CNY budget", got[0])
	}
	if got[1].Bucket != "2026-06" || got[1].BudgetLimit == nil || *got[1].BudgetLimit != 120 || got[1].BudgetCurrency != "USD" || !got[1].DataInsufficient {
		t.Fatalf("second budget bucket = %#v, want currency mismatch marked insufficient", got[1])
	}
	if len(seenArgs) != 3 || seenArgs[1] != 2 || seenArgs[2] != "CNY" {
		t.Fatalf("query args = %#v, want now/months/base", seenArgs)
	}
	if !seenArgs[0].(time.Time).Equal(now.UTC()) {
		t.Fatalf("now arg = %v, want %v", seenArgs[0], now.UTC())
	}
	for _, snippet := range []string{
		"subscription_monthly_budgets",
		"budget_month <= b.bucket_start",
		"order by budget_month desc",
		"currency_mismatch",
	} {
		if !strings.Contains(seenSQL, snippet) {
			t.Fatalf("ListBudgetMonthBuckets SQL missing %q in %s", snippet, seenSQL)
		}
	}
}

func TestPostgresSubscriptionCostRepositoryUpsertMonthlyBudget(t *testing.T) {
	t.Parallel()

	month, err := subscriptions.ParseDate("2026-07-01")
	if err != nil {
		t.Fatalf("parse date: %v", err)
	}
	createdAt := time.Date(2026, time.June, 3, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, time.June, 3, 8, 30, 0, 0, time.UTC)
	var seenSQL string
	var seenArgs []any
	repo := &PostgresSubscriptionCostRepository{db: fakeSubscriptionCostDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			seenSQL = sql
			seenArgs = append([]any(nil), args...)
			return fakeSubscriptionCostRow{scan: func(dest ...any) error {
				if len(dest) != 7 {
					t.Fatalf("scan destinations = %d, want 7", len(dest))
				}
				*(dest[0].(*time.Time)) = month.Time
				*(dest[1].(*string)) = "USD"
				*(dest[2].(*float64)) = 180.5
				*(dest[3].(*int)) = 75
				*(dest[4].(*string)) = "growth"
				*(dest[5].(*time.Time)) = createdAt
				*(dest[6].(*time.Time)) = updatedAt
				return nil
			}}
		},
	}}

	got, err := repo.UpsertMonthlyBudget(context.Background(), subscriptioncosts.UpsertMonthlyBudgetInput{
		BudgetMonth:  month,
		BaseCurrency: "usd",
		MonthlyLimit: 180.5,
		WarningPct:   75,
		Note:         " growth ",
	})
	if err != nil {
		t.Fatalf("UpsertMonthlyBudget() error = %v", err)
	}
	if got.BudgetMonth.Time.Format("2006-01-02") != "2026-07-01" || got.BaseCurrency != "USD" || got.MonthlyLimit != 180.5 || got.WarningPct != 75 || got.Note != "growth" {
		t.Fatalf("UpsertMonthlyBudget() = %#v, want normalized returned record", got)
	}
	if len(seenArgs) != 5 || seenArgs[1] != "USD" || seenArgs[2] != 180.5 || seenArgs[3] != 75 || seenArgs[4] != "growth" {
		t.Fatalf("query args = %#v, want normalized upsert args", seenArgs)
	}
	if !seenArgs[0].(time.Time).Equal(month.Time) {
		t.Fatalf("budget month arg = %v, want %v", seenArgs[0], month.Time)
	}
	for _, snippet := range []string{
		"insert into subscription_monthly_budgets",
		"on conflict (budget_month) do update",
		"updated_at = now()",
	} {
		if !strings.Contains(seenSQL, snippet) {
			t.Fatalf("UpsertMonthlyBudget SQL missing %q in %s", snippet, seenSQL)
		}
	}
}

func TestPostgresSubscriptionCostRepositoryListCostMonthBucketsIntegration(t *testing.T) {
	ctx := context.Background()
	db := openTemporarySubscriptionCostPostgresSchema(t, ctx)
	if err := migrate.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	insertSubscriptionCostBucketFixtures(t, ctx, db)

	repo := NewPostgresSubscriptionCostRepository(db)
	got, err := repo.ListCostMonthBuckets(ctx, centersettings.SubscriptionCostSettings{
		BaseCurrency:         "CNY",
		ExchangeRateProvider: "frankfurter",
	}, 4, time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ListCostMonthBuckets() error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("ListCostMonthBuckets() returned %d buckets, want 4: %#v", len(got), got)
	}

	assertSeriesPoint(t, got[0], "2026-03", 170, false)
	assertSeriesPoint(t, got[1], "2026-04", 170, true)
	assertSeriesPoint(t, got[2], "2026-05", 360, true)
	assertSeriesPoint(t, got[3], "2026-06", 360, true)
}

type fakeSubscriptionCostDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
	exec     func(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (f fakeSubscriptionCostDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeSubscriptionCostRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeSubscriptionCostDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeSubscriptionCostRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

func (f fakeSubscriptionCostDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec == nil {
		return pgconn.CommandTag{}, errors.New("unexpected Exec")
	}
	return f.exec(ctx, sql, args...)
}

type fakeSubscriptionCostRow struct {
	scan func(dest ...any) error
}

func (r fakeSubscriptionCostRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeSubscriptionCostScan struct {
	scan func(dest ...any) error
}

type fakeSubscriptionCostRows struct {
	rows []fakeSubscriptionCostScan
	idx  int
	err  error
}

func (f *fakeSubscriptionCostRows) Close()                                       {}
func (f *fakeSubscriptionCostRows) Err() error                                   { return f.err }
func (f *fakeSubscriptionCostRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeSubscriptionCostRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeSubscriptionCostRows) RawValues() [][]byte                          { return nil }
func (f *fakeSubscriptionCostRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeSubscriptionCostRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeSubscriptionCostRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeSubscriptionCostRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func openTemporarySubscriptionCostPostgresSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required for subscription cost PostgreSQL integration tests")
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for subscription cost PostgreSQL integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	schemaName := fmt.Sprintf("houfeng_sub_cost_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !isSafeSubscriptionCostPostgresIdentifier(schemaName) {
		t.Fatalf("unsafe generated schema name %q", schemaName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres pool for schema setup: %v", err)
	}
	t.Cleanup(adminPool.Close)

	if _, err := adminPool.Exec(ctx, `create schema `+quoteSubscriptionCostPostgresIdentifier(schemaName)); err != nil {
		t.Fatalf("create temporary postgres schema %q: %v", schemaName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop schema if exists `+quoteSubscriptionCostPostgresIdentifier(schemaName)+` cascade`); err != nil {
			t.Logf("drop temporary postgres schema %q: %v", schemaName, err)
		}
	})

	testConfig := adminConfig.Copy()
	if testConfig.ConnConfig.RuntimeParams == nil {
		testConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	testConfig.ConnConfig.RuntimeParams["search_path"] = schemaName

	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres schema %q: %v", schemaName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary postgres schema %q: %v", schemaName, err)
	}
	return testPool
}

func insertSubscriptionCostBucketFixtures(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	execSubscriptionCostSQL(t, ctx, db, `insert into providers (provider_id, name) values ('pv_001', 'Hetzner')`)
	for _, row := range []struct {
		id   string
		name string
	}{
		{"vps_price", "Price history VPS"},
		{"vps_fx", "Missing FX VPS"},
		{"vps_status", "Status history VPS"},
		{"vps_currency", "Currency history VPS"},
	} {
		execSubscriptionCostSQL(t, ctx, db, `
			insert into vps_assets (
				vps_id,
				display_name,
				provider_id,
				lifecycle_status,
				usage_status,
				renewal_decision
			) values ($1, $2, 'pv_001', 'active', 'in_use', 'keep')`,
			row.id,
			row.name,
		)
	}

	execSubscriptionCostSQL(t, ctx, db, `
		insert into subscriptions (
			subscription_id,
			vps_id,
			price,
			currency,
			billing_cycle,
			billing_months,
			monthly_price,
			status,
			created_at
		) values
			('sub_price', 'vps_price', 30, 'USD', 'monthly', 1, 30, 'active', '2026-02-15T00:00:00Z'),
			('sub_fx', 'vps_fx', 5, 'EUR', 'monthly', 1, 5, 'active', '2026-04-10T00:00:00Z'),
			('sub_status', 'vps_status', 100, 'CNY', 'monthly', 1, 100, 'cancelled', '2026-02-15T00:00:00Z'),
			('sub_currency', 'vps_currency', 20, 'USD', 'monthly', 1, 20, 'active', '2026-04-05T00:00:00Z')
	`)
	execSubscriptionCostSQL(t, ctx, db, `
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
			from_auto_renew,
			to_auto_renew,
			from_auto_renew_cancelled,
			to_auto_renew_cancelled,
			from_status,
			to_status,
			changed_at
		) values
			('ph_price', 'sub_price', 'vps_price', 10, 30, 'USD', 'USD', 'monthly', 'monthly', 1, 1, 10, 30, false, false, false, false, 'active', 'active', '2026-05-15T00:00:00Z'),
			('ph_status', 'sub_status', 'vps_status', 100, 100, 'CNY', 'CNY', 'monthly', 'monthly', 1, 1, 100, 100, false, false, false, false, 'active', 'cancelled', '2026-05-20T00:00:00Z'),
			('ph_currency', 'sub_currency', 'vps_currency', 1000, 20, 'JPY', 'USD', 'monthly', 'monthly', 1, 1, 1000, 20, false, false, false, false, 'active', 'active', '2026-05-01T00:00:00Z')
	`)
	execSubscriptionCostSQL(t, ctx, db, `
		insert into subscription_exchange_rates (
			rate_id,
			provider,
			base_currency,
			quote_currency,
			rate,
			rate_date,
			fetched_at
		) values
			('rate_usd_mar', 'frankfurter', 'CNY', 'USD', 7, '2026-03-15', '2026-03-15T00:00:00Z'),
			('rate_usd_may', 'frankfurter', 'CNY', 'USD', 7.2, '2026-05-20', '2026-05-20T00:00:00Z')
	`)
}

func assertSeriesPoint(t *testing.T, got subscriptioncosts.SeriesPoint, bucket string, monthlyCost float64, dataInsufficient bool) {
	t.Helper()
	if got.Bucket != bucket {
		t.Fatalf("bucket = %q, want %q in %#v", got.Bucket, bucket, got)
	}
	if math.Abs(got.MonthlyCost-monthlyCost) > 0.0001 {
		t.Fatalf("%s monthly cost = %.4f, want %.4f in %#v", bucket, got.MonthlyCost, monthlyCost, got)
	}
	if got.DataInsufficient != dataInsufficient {
		t.Fatalf("%s data_insufficient = %v, want %v in %#v", bucket, got.DataInsufficient, dataInsufficient, got)
	}
}

func execSubscriptionCostSQL(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec SQL %q: %v", sql, err)
	}
}

func isSafeSubscriptionCostPostgresIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func quoteSubscriptionCostPostgresIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
