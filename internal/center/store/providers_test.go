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

	"houfeng/internal/center/providers"
)

func TestPostgresProviderMigrationDefinesTableAndConstraints(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0016_create_asset_ledger.sql"))
	if err != nil {
		t.Fatalf("ReadFile(provider migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists providers",
		"provider_id text primary key",
		"name text not null",
		"rating integer",
		"labels text[] not null default '{}'",
		"providers_name_not_blank",
		"length(btrim(name)) > 0",
		"providers_rating_range",
		"rating is null or rating between 1 and 5",
		"idx_providers_name_lower",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("provider migration missing %q", snippet)
		}
	}
}

func TestPostgresProviderCreateListGetAndPatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC)
	var queryCalls []string
	var rowCalls []string
	var rowArgs [][]any
	repo := &PostgresProviderRepository{db: fakeProviderDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalls = append(queryCalls, sql)
			if len(args) != 0 {
				t.Fatalf("ListProviders args = %#v, want none", args)
			}
			return &fakeProviderRows{rows: []fakeProviderScan{
				{scan: func(dest ...any) error {
					scanProviderRecordDestinations(dest, providers.Record{
						ProviderID: "pv_001",
						Name:       "Akamai",
						Rating:     intPtr(4),
						Labels:     []string{"edge"},
						CreatedAt:  now,
						UpdatedAt:  now,
					})
					return nil
				}},
				{scan: func(dest ...any) error {
					scanProviderRecordDestinations(dest, providers.Record{
						ProviderID: "pv_002",
						Name:       "Hetzner",
						Rating:     nil,
						Labels:     []string{"core"},
						CreatedAt:  now,
						UpdatedAt:  now,
					})
					return nil
				}},
			}}, nil
		},
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCalls = append(rowCalls, sql)
			rowArgs = append(rowArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "insert into providers"):
				return fakeProviderRow{scan: func(dest ...any) error {
					providerID, ok := args[0].(string)
					if !ok || !strings.HasPrefix(providerID, "pv_") {
						t.Fatalf("generated provider id arg = %#v, want pv_ prefix", args[0])
					}
					scanProviderRecordDestinations(dest, providers.Record{
						ProviderID:  providerID,
						Name:        "Hetzner",
						Website:     "https://hetzner.com",
						AccountHint: "ops@example.com",
						Country:     "DE",
						Rating:      intPtr(5),
						Labels:      []string{"core"},
						CreatedAt:   now,
						UpdatedAt:   now,
					})
					return nil
				}}
			case strings.Contains(sql, "where provider_id = $1") && strings.Contains(sql, "select"):
				return fakeProviderRow{scan: func(dest ...any) error {
					scanProviderRecordDestinations(dest, providers.Record{
						ProviderID: "pv_001",
						Name:       "Akamai",
						Labels:     []string{"edge"},
						CreatedAt:  now,
						UpdatedAt:  now,
					})
					return nil
				}}
			case strings.Contains(sql, "update providers"):
				return fakeProviderRow{scan: func(dest ...any) error {
					scanProviderRecordDestinations(dest, providers.Record{
						ProviderID: "pv_001",
						Name:       "Akamai Cloud",
						Website:    "https://akamai.com",
						Rating:     nil,
						Labels:     []string{"edge", "backup"},
						CreatedAt:  now.Add(-time.Hour),
						UpdatedAt:  now,
					})
					return nil
				}}
			default:
				t.Fatalf("unexpected query row SQL %q", sql)
				return fakeProviderRow{scan: func(dest ...any) error { return nil }}
			}
		},
	}}

	created, err := repo.CreateProvider(context.Background(), providers.CreateInput{
		Name:        " Hetzner ",
		Website:     " https://hetzner.com ",
		AccountHint: " ops@example.com ",
		Country:     " DE ",
		Rating:      intPtr(5),
		Labels:      []string{" core ", ""},
	})
	if err != nil {
		t.Fatalf("CreateProvider() error = %v", err)
	}
	if !strings.HasPrefix(created.ProviderID, "pv_") {
		t.Fatalf("ProviderID = %q, want pv_ prefix", created.ProviderID)
	}
	if created.Name != "Hetzner" {
		t.Fatalf("created.Name = %q, want Hetzner", created.Name)
	}

	list, err := repo.ListProviders(context.Background())
	if err != nil {
		t.Fatalf("ListProviders() error = %v", err)
	}
	if len(list) != 2 || list[0].ProviderID != "pv_001" || list[1].ProviderID != "pv_002" {
		t.Fatalf("ListProviders() = %#v, want two provider records", list)
	}
	if len(queryCalls) != 1 || !strings.Contains(queryCalls[0], "order by lower(name), provider_id") {
		t.Fatalf("ListProviders SQL = %#v, want stable name ordering", queryCalls)
	}

	got, err := repo.GetProvider(context.Background(), "pv_001")
	if err != nil {
		t.Fatalf("GetProvider() error = %v", err)
	}
	if got.Name != "Akamai" {
		t.Fatalf("GetProvider().Name = %q, want Akamai", got.Name)
	}

	patched, err := repo.PatchProvider(context.Background(), "pv_001", providers.PatchInput{
		Name:    providers.PatchString(" Akamai Cloud "),
		Website: providers.PatchString(" https://akamai.com "),
		Rating:  providers.PatchRating(nil),
		Labels:  providers.PatchLabels([]string{" edge ", "backup", "edge"}),
	})
	if err != nil {
		t.Fatalf("PatchProvider() error = %v", err)
	}
	if patched.Name != "Akamai Cloud" {
		t.Fatalf("patched.Name = %q, want Akamai Cloud", patched.Name)
	}
	if patched.Rating != nil {
		t.Fatalf("patched.Rating = %#v, want nil", patched.Rating)
	}
	if len(rowCalls) != 3 {
		t.Fatalf("QueryRow calls = %d, want create/get/patch", len(rowCalls))
	}
	patchArgs := rowArgs[2]
	if len(patchArgs) != 17 {
		t.Fatalf("patch args len = %d, want 17", len(patchArgs))
	}
	if patchArgs[0] != "pv_001" || patchArgs[1] != true || patchArgs[2] != "Akamai Cloud" {
		t.Fatalf("patch name args = %#v, want provider id and trimmed name", patchArgs[:3])
	}
	if patchArgs[13] != true || patchArgs[14] != nil {
		t.Fatalf("patch rating args = set:%#v value:%#v, want explicit null", patchArgs[13], patchArgs[14])
	}
	labels, ok := patchArgs[16].([]string)
	if !ok || len(labels) != 2 || labels[0] != "edge" || labels[1] != "backup" {
		t.Fatalf("patch labels arg = %#v, want normalized labels", patchArgs[16])
	}
	for _, snippet := range []string{
		"name = case when $2::boolean then $3 else name end",
		"website = case when $4::boolean then $5 else website end",
		"rating = case when $14::boolean then $15::integer else rating end",
		"labels = case when $16::boolean then $17::text[] else labels end",
		"updated_at = now()",
		"where provider_id = $1",
		"returning " + providerSelectColumns,
	} {
		if !strings.Contains(rowCalls[2], snippet) {
			t.Fatalf("PatchProvider SQL missing %q in %q", snippet, rowCalls[2])
		}
	}
}

func TestPostgresProviderPatchWithoutChangesReturnsExistingProvider(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 8, 12, 0, 0, 0, time.UTC)
	queryCount := 0
	repo := &PostgresProviderRepository{db: fakeProviderDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			queryCount++
			if strings.Contains(sql, "update providers") {
				t.Fatalf("PatchProvider without changes should not update: %q", sql)
			}
			if len(args) != 1 || args[0] != "pv_001" {
				t.Fatalf("GetProvider args = %#v, want provider id", args)
			}
			return fakeProviderRow{scan: func(dest ...any) error {
				scanProviderRecordDestinations(dest, providers.Record{
					ProviderID: "pv_001",
					Name:       "Akamai",
					CreatedAt:  now,
					UpdatedAt:  now,
				})
				return nil
			}}
		},
	}}

	record, err := repo.PatchProvider(context.Background(), "pv_001", providers.PatchInput{})
	if err != nil {
		t.Fatalf("PatchProvider() error = %v", err)
	}
	if record.ProviderID != "pv_001" {
		t.Fatalf("ProviderID = %q, want pv_001", record.ProviderID)
	}
	if queryCount != 1 {
		t.Fatalf("QueryRow calls = %d, want only get", queryCount)
	}
}

func TestPostgresProviderMapsNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresProviderRepository{db: fakeProviderDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeProviderRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	if _, err := repo.GetProvider(context.Background(), "pv_missing"); !errors.Is(err, providers.ErrProviderNotFound) {
		t.Fatalf("GetProvider() error = %v, want ErrProviderNotFound", err)
	}
	if _, err := repo.PatchProvider(context.Background(), "pv_missing", providers.PatchInput{Name: providers.PatchString("New")}); !errors.Is(err, providers.ErrProviderNotFound) {
		t.Fatalf("PatchProvider() error = %v, want ErrProviderNotFound", err)
	}
}

func TestPostgresProviderRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	repo := &PostgresProviderRepository{db: fakeProviderDB{}}
	if _, err := repo.CreateProvider(context.Background(), providers.CreateInput{Name: " "}); !errors.Is(err, providers.ErrInvalidProviderInput) {
		t.Fatalf("CreateProvider() error = %v, want ErrInvalidProviderInput", err)
	}
	if _, err := repo.PatchProvider(context.Background(), "pv_001", providers.PatchInput{Rating: providers.PatchRating(intPtr(7))}); !errors.Is(err, providers.ErrInvalidProviderInput) {
		t.Fatalf("PatchProvider() error = %v, want ErrInvalidProviderInput", err)
	}
}

type fakeProviderDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeProviderDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeProviderRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeProviderDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeProviderRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeProviderRow struct {
	scan func(dest ...any) error
}

func (r fakeProviderRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeProviderScan struct {
	scan func(dest ...any) error
}

type fakeProviderRows struct {
	rows []fakeProviderScan
	idx  int
	err  error
}

func (f *fakeProviderRows) Close()                                       {}
func (f *fakeProviderRows) Err() error                                   { return f.err }
func (f *fakeProviderRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeProviderRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeProviderRows) RawValues() [][]byte                          { return nil }
func (f *fakeProviderRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeProviderRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeProviderRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeProviderRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func scanProviderRecordDestinations(dest []any, record providers.Record) {
	*(dest[0].(*string)) = record.ProviderID
	*(dest[1].(*string)) = record.Name
	*(dest[2].(*string)) = record.Website
	*(dest[3].(*string)) = record.PanelURL
	*(dest[4].(*string)) = record.AccountHint
	*(dest[5].(*string)) = record.Country
	*(dest[6].(*string)) = record.Note
	*(dest[7].(**int)) = cloneProviderIntPtr(record.Rating)
	*(dest[8].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[9].(*time.Time)) = record.CreatedAt
	*(dest[10].(*time.Time)) = record.UpdatedAt
}

func cloneProviderIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func intPtr(value int) *int {
	return &value
}
