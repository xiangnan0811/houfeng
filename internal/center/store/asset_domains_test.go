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

	"houfeng/internal/center/assetdomains"
	"houfeng/internal/center/subscriptions"
)

func TestPostgresAssetDomainMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0024_create_asset_domains.sql"))
	if err != nil {
		t.Fatalf("ReadFile(asset domain migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists asset_domains",
		"domain_id text primary key",
		"vps_id text not null",
		"service_id text",
		"target_id text",
		"domain_name text not null",
		"expires_at date",
		"auto_renew boolean not null default false",
		"https_enabled boolean not null default false",
		"labels text[] not null default '{}'",
		"asset_domains_vps_fk foreign key (vps_id) references vps_assets(vps_id) on delete cascade",
		"asset_domains_service_fk foreign key (service_id) references asset_services(service_id) on delete set null",
		"asset_domains_target_fk foreign key (target_id) references targets(target_id) on delete set null",
		"asset_domains_name_unique unique (domain_name)",
		"asset_domains_name_not_blank",
		"asset_domains_name_normalized",
		"asset_domains_status_allowed",
		"'active', 'paused', 'retired', 'unknown'",
		"idx_asset_domains_vps",
		"idx_asset_domains_service",
		"idx_asset_domains_target",
		"idx_asset_domains_status",
		"idx_asset_domains_expires_at",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("asset domain migration missing %q", snippet)
		}
	}
}

func TestPostgresAssetDomainCreateAndList(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	serviceID := "svc_001"
	targetID := "tg_001"
	expiresAt := subscriptions.NewDate(time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC))
	var queryCalls []string
	var queryArgs [][]any
	var rowCalls []string
	var rowArgs [][]any
	repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalls = append(queryCalls, sql)
			queryArgs = append(queryArgs, append([]any(nil), args...))
			return &fakeAssetDomainRows{rows: []fakeAssetDomainScan{
				{scan: func(dest ...any) error {
					scanAssetDomainRecordDestinations(dest, assetdomains.Record{
						DomainID:     "dom_001",
						VPSID:        "vps_001",
						ServiceID:    &serviceID,
						TargetID:     &targetID,
						DomainName:   "www.example.com",
						Purpose:      "site",
						Status:       assetdomains.DomainStatusActive,
						Registrar:    "NameSilo",
						ExpiresAt:    &expiresAt,
						AutoRenew:    true,
						HTTPSEnabled: true,
						Labels:       []string{"prod"},
						Note:         "primary",
						CreatedAt:    now,
						UpdatedAt:    now,
					})
					return nil
				}},
			}}, nil
		},
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCalls = append(rowCalls, sql)
			rowArgs = append(rowArgs, append([]any(nil), args...))
			if strings.Contains(sql, "from asset_services") {
				if len(args) != 2 || args[0] != "svc_001" || args[1] != "vps_001" {
					t.Fatalf("service ownership args = %#v, want svc/vps pair", args)
				}
				return fakeAssetDomainRow{scan: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					return nil
				}}
			}
			if !strings.Contains(sql, "insert into asset_domains") {
				t.Fatalf("unexpected QueryRow SQL %q", sql)
			}
			return fakeAssetDomainRow{scan: func(dest ...any) error {
				domainID, ok := args[0].(string)
				if !ok || !strings.HasPrefix(domainID, "dom_") {
					t.Fatalf("generated domain id arg = %#v, want dom_ prefix", args[0])
				}
				scanAssetDomainRecordDestinations(dest, assetdomains.Record{
					DomainID:     domainID,
					VPSID:        "vps_001",
					ServiceID:    &serviceID,
					TargetID:     &targetID,
					DomainName:   "www.example.com",
					Purpose:      "site",
					Status:       assetdomains.DomainStatusActive,
					Registrar:    "NameSilo",
					ExpiresAt:    &expiresAt,
					AutoRenew:    true,
					HTTPSEnabled: true,
					Labels:       []string{"prod"},
					Note:         "primary",
					CreatedAt:    now,
					UpdatedAt:    now,
				})
				return nil
			}}
		},
	}}

	created, err := repo.CreateAssetDomain(context.Background(), assetdomains.CreateInput{
		VPSID:        " vps_001 ",
		ServiceID:    stringPtr(" svc_001 "),
		TargetID:     stringPtr(" tg_001 "),
		DomainName:   " WWW.Example.COM. ",
		Purpose:      " site ",
		Status:       assetdomains.DomainStatusActive,
		Registrar:    " NameSilo ",
		ExpiresAt:    &expiresAt,
		AutoRenew:    true,
		HTTPSEnabled: true,
		Labels:       []string{" prod ", "", "prod"},
		Note:         " primary ",
	})
	if err != nil {
		t.Fatalf("CreateAssetDomain() error = %v", err)
	}
	if !strings.HasPrefix(created.DomainID, "dom_") {
		t.Fatalf("DomainID = %q, want dom_ prefix", created.DomainID)
	}
	if len(rowArgs) != 2 || len(rowArgs[1]) != 13 {
		t.Fatalf("create args = %#v, want 13 args", rowArgs)
	}
	insertArgs := rowArgs[1]
	if insertArgs[1] != "vps_001" || insertArgs[2] != "svc_001" || insertArgs[3] != "tg_001" || insertArgs[4] != "www.example.com" {
		t.Fatalf("create normalized args = %#v", insertArgs)
	}
	if insertArgs[8] != expiresAt.Time {
		t.Fatalf("expires_at arg = %#v, want %v", insertArgs[8], expiresAt.Time)
	}
	labels, ok := insertArgs[11].([]string)
	if !ok || len(labels) != 1 || labels[0] != "prod" {
		t.Fatalf("labels arg = %#v, want normalized labels", insertArgs[11])
	}
	for _, snippet := range []string{
		"insert into asset_domains",
		"returning " + assetDomainSelectColumns,
	} {
		if !strings.Contains(rowCalls[1], snippet) {
			t.Fatalf("CreateAssetDomain SQL missing %q in %q", snippet, rowCalls[1])
		}
	}

	list, err := repo.ListAssetDomains(context.Background(), assetdomains.ListFilters{
		VPSID:     " vps_001 ",
		ServiceID: " svc_001 ",
		TargetID:  " tg_001 ",
		Status:    " active ",
	})
	if err != nil {
		t.Fatalf("ListAssetDomains() error = %v", err)
	}
	if len(list) != 1 || list[0].DomainID != "dom_001" {
		t.Fatalf("ListAssetDomains() = %#v, want one domain", list)
	}
	for _, snippet := range []string{
		"vps_id = $1",
		"service_id = $2",
		"target_id = $3",
		"status = $4",
		"v.lifecycle_status not in ('cancelled', 'archived')",
		"order by lower(asset_domains.domain_name), asset_domains.domain_id",
	} {
		if !strings.Contains(queryCalls[0], snippet) {
			t.Fatalf("ListAssetDomains SQL missing %q in %q", snippet, queryCalls[0])
		}
	}
	if len(queryArgs[0]) != 4 || queryArgs[0][0] != "vps_001" || queryArgs[0][1] != "svc_001" || queryArgs[0][2] != "tg_001" || queryArgs[0][3] != "active" {
		t.Fatalf("list args = %#v, want normalized filters", queryArgs[0])
	}
}

func TestPostgresAssetDomainListForVPS(t *testing.T) {
	t.Parallel()

	var queryArgs []any
	var querySQL string
	var existsCheckArg any
	repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			querySQL = sql
			queryArgs = append([]any(nil), args...)
			return &fakeAssetDomainRows{}, nil
		},
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "from vps_assets") {
				t.Fatalf("unexpected QueryRow SQL %q", sql)
			}
			existsCheckArg = args[0]
			return fakeAssetDomainRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
	}}

	records, err := repo.ListAssetDomainsForVPS(context.Background(), " vps_001 ")
	if err != nil {
		t.Fatalf("ListAssetDomainsForVPS() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want empty list", records)
	}
	if existsCheckArg != "vps_001" {
		t.Fatalf("exists check arg = %#v, want vps_001", existsCheckArg)
	}
	if len(queryArgs) != 1 || queryArgs[0] != "vps_001" {
		t.Fatalf("query args = %#v, want vps filter", queryArgs)
	}
	if strings.Contains(querySQL, "lifecycle_status not in ('cancelled', 'archived')") {
		t.Fatalf("ListAssetDomainsForVPS SQL = %q, want history read without current asset scope filter", querySQL)
	}
}

func TestPostgresAssetDomainListForVPSMissingOwner(t *testing.T) {
	t.Parallel()

	repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "from vps_assets") {
				t.Fatalf("unexpected QueryRow SQL %q", sql)
			}
			if len(args) != 1 || args[0] != "vps_missing" {
				t.Fatalf("exists args = %#v, want vps_missing", args)
			}
			return fakeAssetDomainRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}}

	_, err := repo.ListAssetDomainsForVPS(context.Background(), "vps_missing")
	if !errors.Is(err, assetdomains.ErrDomainOwnerNotFound) {
		t.Fatalf("ListAssetDomainsForVPS() error = %v, want ErrDomainOwnerNotFound", err)
	}
}

func TestPostgresAssetDomainRejectsServiceFromAnotherVPS(t *testing.T) {
	t.Parallel()

	repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "from asset_services") {
				t.Fatalf("unexpected QueryRow SQL %q", sql)
			}
			if len(args) != 2 || args[0] != "svc_other" || args[1] != "vps_001" {
				t.Fatalf("service ownership args = %#v, want svc/vps pair", args)
			}
			return fakeAssetDomainRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}}

	_, err := repo.CreateAssetDomain(context.Background(), assetdomains.CreateInput{
		VPSID:      "vps_001",
		ServiceID:  stringPtr("svc_other"),
		DomainName: "example.com",
		Status:     assetdomains.DomainStatusActive,
	})
	if !errors.Is(err, assetdomains.ErrDomainServiceNotFound) {
		t.Fatalf("CreateAssetDomain() error = %v, want ErrDomainServiceNotFound", err)
	}
}

func TestPostgresAssetDomainRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{}}
	if _, err := repo.CreateAssetDomain(context.Background(), assetdomains.CreateInput{DomainName: "example.com", Status: assetdomains.DomainStatusActive}); !errors.Is(err, assetdomains.ErrInvalidDomainInput) {
		t.Fatalf("CreateAssetDomain() error = %v, want ErrInvalidDomainInput", err)
	}
	if _, err := repo.ListAssetDomains(context.Background(), assetdomains.ListFilters{Status: "deleted"}); !errors.Is(err, assetdomains.ErrInvalidDomainInput) {
		t.Fatalf("ListAssetDomains() error = %v, want ErrInvalidDomainInput", err)
	}
	if _, err := repo.ListAssetDomainsForVPS(context.Background(), " "); !errors.Is(err, assetdomains.ErrInvalidDomainInput) {
		t.Fatalf("ListAssetDomainsForVPS() error = %v, want ErrInvalidDomainInput", err)
	}
}

func TestPostgresAssetDomainMapsForeignKeyUniqueAndCheckViolations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantSentinel error
	}{
		{
			name:         "missing vps",
			err:          &pgconn.PgError{Code: "23503", ConstraintName: "asset_domains_vps_fk"},
			wantSentinel: assetdomains.ErrDomainOwnerNotFound,
		},
		{
			name:         "missing service",
			err:          &pgconn.PgError{Code: "23503", ConstraintName: "asset_domains_service_fk"},
			wantSentinel: assetdomains.ErrDomainServiceNotFound,
		},
		{
			name:         "missing target",
			err:          &pgconn.PgError{Code: "23503", ConstraintName: "asset_domains_target_fk"},
			wantSentinel: assetdomains.ErrDomainTargetNotFound,
		},
		{
			name:         "duplicate domain",
			err:          &pgconn.PgError{Code: "23505", ConstraintName: "asset_domains_name_unique"},
			wantSentinel: assetdomains.ErrDomainConflict,
		},
		{
			name:         "check",
			err:          &pgconn.PgError{Code: "23514"},
			wantSentinel: assetdomains.ErrInvalidDomainInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &PostgresAssetDomainRepository{db: fakeAssetDomainDB{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeAssetDomainRow{scan: func(dest ...any) error {
						return tt.err
					}}
				},
			}}

			_, err := repo.CreateAssetDomain(context.Background(), assetdomains.CreateInput{
				VPSID:      "vps_001",
				DomainName: "example.com",
				Status:     assetdomains.DomainStatusActive,
			})
			if !errors.Is(err, tt.wantSentinel) {
				t.Fatalf("CreateAssetDomain() error = %v, want %v", err, tt.wantSentinel)
			}
		})
	}
}

type fakeAssetDomainDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeAssetDomainDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeAssetDomainRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeAssetDomainDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeAssetDomainRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeAssetDomainRow struct {
	scan func(dest ...any) error
}

func (r fakeAssetDomainRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeAssetDomainScan struct {
	scan func(dest ...any) error
}

type fakeAssetDomainRows struct {
	rows []fakeAssetDomainScan
	idx  int
	err  error
}

func (f *fakeAssetDomainRows) Close()                                       {}
func (f *fakeAssetDomainRows) Err() error                                   { return f.err }
func (f *fakeAssetDomainRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeAssetDomainRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeAssetDomainRows) RawValues() [][]byte                          { return nil }
func (f *fakeAssetDomainRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeAssetDomainRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeAssetDomainRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeAssetDomainRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func scanAssetDomainRecordDestinations(dest []any, record assetdomains.Record) {
	*(dest[0].(*string)) = record.DomainID
	*(dest[1].(*string)) = record.VPSID
	*(dest[2].(**string)) = cloneStringPtr(record.ServiceID)
	*(dest[3].(**string)) = cloneStringPtr(record.TargetID)
	*(dest[4].(*string)) = record.DomainName
	*(dest[5].(*string)) = record.Purpose
	*(dest[6].(*assetdomains.DomainStatus)) = record.Status
	*(dest[7].(*string)) = record.Registrar
	*(dest[8].(**time.Time)) = cloneAssetDomainDatePtr(record.ExpiresAt)
	*(dest[9].(*bool)) = record.AutoRenew
	*(dest[10].(*bool)) = record.HTTPSEnabled
	*(dest[11].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[12].(*string)) = record.Note
	*(dest[13].(*time.Time)) = record.CreatedAt
	*(dest[14].(*time.Time)) = record.UpdatedAt
}

func cloneAssetDomainDatePtr(value *assetdomains.Date) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.Time
	return &cloned
}
