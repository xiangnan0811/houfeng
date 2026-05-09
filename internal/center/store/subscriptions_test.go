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

	"houfeng/internal/center/subscriptions"
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
						SubscriptionID:     "sub_001",
						VPSID:              "vps_001",
						Price:              120,
						Currency:           "USD",
						BillingCycle:       "annual",
						BillingMonths:      12,
						MonthlyPrice:       10,
						StartedAt:          &startedAt,
						RenewAt:            &renewAt,
						AutoRenew:          true,
						AutoRenewCancelled: false,
						Status:             subscriptions.StatusActive,
						PaymentMethod:      "card",
						Note:               "production",
						CreatedAt:          now,
						UpdatedAt:          now,
					})
					return nil
				}},
				{scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID: "sub_002",
						VPSID:          "vps_002",
						Price:          6,
						Currency:       "EUR",
						BillingCycle:   "monthly",
						BillingMonths:  1,
						MonthlyPrice:   6,
						Status:         subscriptions.StatusPaused,
						CreatedAt:      now,
						UpdatedAt:      now,
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
						SubscriptionID:     subscriptionID,
						VPSID:              "vps_001",
						Price:              120,
						Currency:           "USD",
						BillingCycle:       "annual",
						BillingMonths:      12,
						MonthlyPrice:       10,
						StartedAt:          &startedAt,
						RenewAt:            &renewAt,
						AutoRenew:          true,
						AutoRenewCancelled: false,
						Status:             subscriptions.StatusActive,
						PaymentMethod:      "card",
						Note:               "production",
						CreatedAt:          now,
						UpdatedAt:          now,
					})
					return nil
				}}
			case strings.Contains(sql, "where subscription_id = $1") && strings.Contains(sql, "select"):
				return fakeSubscriptionRow{scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID: "sub_001",
						VPSID:          "vps_001",
						Price:          120,
						Currency:       "USD",
						BillingCycle:   "annual",
						BillingMonths:  12,
						MonthlyPrice:   10,
						StartedAt:      &startedAt,
						RenewAt:        &renewAt,
						Status:         subscriptions.StatusActive,
						CreatedAt:      now,
						UpdatedAt:      now,
					})
					return nil
				}}
			case strings.Contains(sql, "update subscriptions"):
				return fakeSubscriptionRow{scan: func(dest ...any) error {
					scanSubscriptionRecordDestinations(dest, subscriptions.Record{
						SubscriptionID:     "sub_001",
						VPSID:              "vps_002",
						Price:              240,
						Currency:           "EUR",
						BillingCycle:       "biennial",
						BillingMonths:      24,
						MonthlyPrice:       10,
						StartedAt:          nil,
						RenewAt:            &patchedRenewAt,
						AutoRenew:          false,
						AutoRenewCancelled: true,
						Status:             subscriptions.StatusPaused,
						PaymentMethod:      "paypal",
						Note:               "review",
						CreatedAt:          now.Add(-time.Hour),
						UpdatedAt:          now,
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
	if len(rowArgs[0]) != 14 {
		t.Fatalf("create args len = %d, want 14", len(rowArgs[0]))
	}
	if rowArgs[0][1] != "vps_001" || rowArgs[0][3] != "USD" || rowArgs[0][6] != float64(10) || rowArgs[0][11] != string(subscriptions.StatusActive) {
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
		VPSID:              subscriptions.PatchString(" vps_002 "),
		Price:              subscriptions.PatchFloat(240),
		Currency:           subscriptions.PatchString(" eur "),
		BillingCycle:       subscriptions.PatchString(" biennial "),
		BillingMonths:      subscriptions.PatchInt(24),
		StartedAt:          subscriptions.PatchDate(nil),
		RenewAt:            subscriptions.PatchDate(&patchedRenewAt),
		AutoRenew:          subscriptions.PatchBool(false),
		AutoRenewCancelled: subscriptions.PatchBool(true),
		Status:             subscriptions.PatchStatus(subscriptions.StatusPaused),
		PaymentMethod:      subscriptions.PatchString(" paypal "),
		Note:               subscriptions.PatchString(" review "),
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
	if len(patchArgs) != 25 {
		t.Fatalf("patch args len = %d, want 25", len(patchArgs))
	}
	if patchArgs[0] != "sub_001" || patchArgs[1] != true || patchArgs[2] != "vps_002" {
		t.Fatalf("patch vps args = %#v, want subscription id and vps", patchArgs[:3])
	}
	if patchArgs[3] != true || patchArgs[4] != float64(240) || patchArgs[9] != true || patchArgs[10] != 24 {
		t.Fatalf("patch price/month args = %#v, want explicit recalculation inputs", patchArgs)
	}
	if patchArgs[11] != true || patchArgs[12] != nil {
		t.Fatalf("patch started_at args = set:%#v value:%#v, want explicit null", patchArgs[11], patchArgs[12])
	}
	if patchArgs[19] != true || patchArgs[20] != string(subscriptions.StatusPaused) {
		t.Fatalf("patch status args = set:%#v value:%#v, want paused", patchArgs[19], patchArgs[20])
	}
	for _, snippet := range []string{
		"vps_id = case when $2::boolean then $3 else vps_id end",
		"price = case when $4::boolean then $5::numeric else price end",
		"billing_months = case when $10::boolean then $11::integer else billing_months end",
		"when $4::boolean or $10::boolean then",
		"started_at = case when $12::boolean then $13::date else started_at end",
		"renew_at = case when $14::boolean then $15::date else renew_at end",
		"updated_at = now()",
		"where subscription_id = $1",
		"returning " + subscriptionSelectColumns,
	} {
		if !strings.Contains(rowCalls[2], snippet) {
			t.Fatalf("PatchSubscription SQL missing %q in %q", snippet, rowCalls[2])
		}
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
	*(dest[6].(*float64)) = record.MonthlyPrice
	*(dest[7].(**time.Time)) = cloneTimePtr(subscriptions.TimePtrFromDate(record.StartedAt))
	*(dest[8].(**time.Time)) = cloneTimePtr(subscriptions.TimePtrFromDate(record.RenewAt))
	*(dest[9].(*bool)) = record.AutoRenew
	*(dest[10].(*bool)) = record.AutoRenewCancelled
	*(dest[11].(*subscriptions.Status)) = record.Status
	*(dest[12].(*string)) = record.PaymentMethod
	*(dest[13].(*string)) = record.Note
	*(dest[14].(*time.Time)) = record.CreatedAt
	*(dest[15].(*time.Time)) = record.UpdatedAt
}
