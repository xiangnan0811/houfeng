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

	"houfeng/internal/center/assetservices"
)

func TestPostgresAssetServiceMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0023_create_asset_services.sql"))
	if err != nil {
		t.Fatalf("ReadFile(asset service migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists asset_services",
		"service_id text primary key",
		"vps_id text not null",
		"target_id text",
		"asset_services_vps_fk foreign key (vps_id) references vps_assets(vps_id) on delete cascade",
		"asset_services_target_fk foreign key (target_id) references targets(target_id) on delete set null",
		"name text not null",
		"labels text[] not null default '{}'",
		"asset_services_name_not_blank",
		"length(btrim(name)) > 0",
		"asset_services_type_allowed",
		"'web', 'api', 'database', 'worker', 'proxy', 'other'",
		"asset_services_status_allowed",
		"'active', 'paused', 'retired', 'unknown'",
		"asset_services_port_range",
		"port is null or port between 1 and 65535",
		"idx_asset_services_vps",
		"idx_asset_services_target",
		"idx_asset_services_status",
		"idx_asset_services_type",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("asset service migration missing %q", snippet)
		}
	}
}

func TestPostgresAssetServiceCreateAndList(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 10, 0, 0, 0, time.UTC)
	targetID := "tg_001"
	port := 443
	var queryCalls []string
	var queryArgs [][]any
	var rowCalls []string
	var rowArgs [][]any
	repo := &PostgresAssetServiceRepository{db: fakeAssetServiceDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalls = append(queryCalls, sql)
			queryArgs = append(queryArgs, append([]any(nil), args...))
			return &fakeAssetServiceRows{rows: []fakeAssetServiceScan{
				{scan: func(dest ...any) error {
					scanAssetServiceRecordDestinations(dest, assetservices.Record{
						ServiceID:   "svc_001",
						VPSID:       "vps_001",
						TargetID:    &targetID,
						Name:        "Blog",
						ServiceType: assetservices.ServiceTypeWeb,
						Status:      assetservices.ServiceStatusActive,
						URL:         "https://example.com",
						Port:        &port,
						Labels:      []string{"prod"},
						CreatedAt:   now,
						UpdatedAt:   now,
					})
					return nil
				}},
			}}, nil
		},
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCalls = append(rowCalls, sql)
			rowArgs = append(rowArgs, append([]any(nil), args...))
			if !strings.Contains(sql, "insert into asset_services") {
				t.Fatalf("unexpected QueryRow SQL %q", sql)
			}
			return fakeAssetServiceRow{scan: func(dest ...any) error {
				serviceID, ok := args[0].(string)
				if !ok || !strings.HasPrefix(serviceID, "svc_") {
					t.Fatalf("generated service id arg = %#v, want svc_ prefix", args[0])
				}
				scanAssetServiceRecordDestinations(dest, assetservices.Record{
					ServiceID:   serviceID,
					VPSID:       "vps_001",
					TargetID:    &targetID,
					Name:        "Blog",
					ServiceType: assetservices.ServiceTypeWeb,
					Status:      assetservices.ServiceStatusActive,
					URL:         "https://example.com",
					Port:        &port,
					Labels:      []string{"prod"},
					Note:        "primary",
					CreatedAt:   now,
					UpdatedAt:   now,
				})
				return nil
			}}
		},
	}}

	created, err := repo.CreateAssetService(context.Background(), assetservices.CreateInput{
		VPSID:       " vps_001 ",
		TargetID:    stringPtr(" tg_001 "),
		Name:        " Blog ",
		ServiceType: assetservices.ServiceTypeWeb,
		Status:      assetservices.ServiceStatusActive,
		URL:         " https://example.com ",
		Port:        &port,
		Labels:      []string{" prod ", "", "prod"},
		Note:        " primary ",
	})
	if err != nil {
		t.Fatalf("CreateAssetService() error = %v", err)
	}
	if !strings.HasPrefix(created.ServiceID, "svc_") {
		t.Fatalf("ServiceID = %q, want svc_ prefix", created.ServiceID)
	}
	if len(rowArgs) != 1 || len(rowArgs[0]) != 10 {
		t.Fatalf("create args = %#v, want 10 args", rowArgs)
	}
	if rowArgs[0][1] != "vps_001" || rowArgs[0][2] != "tg_001" || rowArgs[0][3] != "Blog" || rowArgs[0][7] != 443 {
		t.Fatalf("create normalized args = %#v", rowArgs[0])
	}
	labels, ok := rowArgs[0][8].([]string)
	if !ok || len(labels) != 1 || labels[0] != "prod" {
		t.Fatalf("labels arg = %#v, want normalized labels", rowArgs[0][8])
	}
	for _, snippet := range []string{
		"insert into asset_services",
		"returning " + assetServiceSelectColumns,
	} {
		if !strings.Contains(rowCalls[0], snippet) {
			t.Fatalf("CreateAssetService SQL missing %q in %q", snippet, rowCalls[0])
		}
	}

	list, err := repo.ListAssetServices(context.Background(), assetservices.ListFilters{
		VPSID:       " vps_001 ",
		TargetID:    " tg_001 ",
		ServiceType: " web ",
		Status:      " active ",
	})
	if err != nil {
		t.Fatalf("ListAssetServices() error = %v", err)
	}
	if len(list) != 1 || list[0].ServiceID != "svc_001" {
		t.Fatalf("ListAssetServices() = %#v, want one service", list)
	}
	for _, snippet := range []string{
		"vps_id = $1",
		"target_id = $2",
		"service_type = $3",
		"status = $4",
		"order by lower(name), service_id",
	} {
		if !strings.Contains(queryCalls[0], snippet) {
			t.Fatalf("ListAssetServices SQL missing %q in %q", snippet, queryCalls[0])
		}
	}
	if len(queryArgs[0]) != 4 || queryArgs[0][0] != "vps_001" || queryArgs[0][1] != "tg_001" || queryArgs[0][2] != "web" || queryArgs[0][3] != "active" {
		t.Fatalf("list args = %#v, want normalized filters", queryArgs[0])
	}
}

func TestPostgresAssetServiceListForVPS(t *testing.T) {
	t.Parallel()

	var queryArgs []any
	var existsCheckArg any
	repo := &PostgresAssetServiceRepository{db: fakeAssetServiceDB{
		query: func(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
			queryArgs = append([]any(nil), args...)
			return &fakeAssetServiceRows{}, nil
		},
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "from vps_assets") {
				t.Fatalf("unexpected QueryRow SQL %q", sql)
			}
			existsCheckArg = args[0]
			return fakeAssetServiceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
	}}

	records, err := repo.ListAssetServicesForVPS(context.Background(), " vps_001 ")
	if err != nil {
		t.Fatalf("ListAssetServicesForVPS() error = %v", err)
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
}

func TestPostgresAssetServiceListForVPSMissingOwner(t *testing.T) {
	t.Parallel()

	repo := &PostgresAssetServiceRepository{db: fakeAssetServiceDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "from vps_assets") {
				t.Fatalf("unexpected QueryRow SQL %q", sql)
			}
			if len(args) != 1 || args[0] != "vps_missing" {
				t.Fatalf("exists args = %#v, want vps_missing", args)
			}
			return fakeAssetServiceRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}}

	_, err := repo.ListAssetServicesForVPS(context.Background(), "vps_missing")
	if !errors.Is(err, assetservices.ErrServiceOwnerNotFound) {
		t.Fatalf("ListAssetServicesForVPS() error = %v, want ErrServiceOwnerNotFound", err)
	}
}

func TestPostgresAssetServiceRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	repo := &PostgresAssetServiceRepository{db: fakeAssetServiceDB{}}
	if _, err := repo.CreateAssetService(context.Background(), assetservices.CreateInput{Name: "Blog", ServiceType: assetservices.ServiceTypeWeb, Status: assetservices.ServiceStatusActive}); !errors.Is(err, assetservices.ErrInvalidServiceInput) {
		t.Fatalf("CreateAssetService() error = %v, want ErrInvalidServiceInput", err)
	}
	if _, err := repo.ListAssetServices(context.Background(), assetservices.ListFilters{Status: "deleted"}); !errors.Is(err, assetservices.ErrInvalidServiceInput) {
		t.Fatalf("ListAssetServices() error = %v, want ErrInvalidServiceInput", err)
	}
	if _, err := repo.ListAssetServicesForVPS(context.Background(), " "); !errors.Is(err, assetservices.ErrInvalidServiceInput) {
		t.Fatalf("ListAssetServicesForVPS() error = %v, want ErrInvalidServiceInput", err)
	}
}

func TestPostgresAssetServiceMapsForeignKeyAndCheckViolations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		wantSentinel error
	}{
		{
			name:         "missing vps",
			err:          &pgconn.PgError{Code: "23503", ConstraintName: "asset_services_vps_fk"},
			wantSentinel: assetservices.ErrServiceOwnerNotFound,
		},
		{
			name:         "missing target",
			err:          &pgconn.PgError{Code: "23503", ConstraintName: "asset_services_target_fk"},
			wantSentinel: assetservices.ErrServiceTargetNotFound,
		},
		{
			name:         "check",
			err:          &pgconn.PgError{Code: "23514"},
			wantSentinel: assetservices.ErrInvalidServiceInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &PostgresAssetServiceRepository{db: fakeAssetServiceDB{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeAssetServiceRow{scan: func(dest ...any) error {
						return tt.err
					}}
				},
			}}

			_, err := repo.CreateAssetService(context.Background(), assetservices.CreateInput{
				VPSID:       "vps_001",
				Name:        "Blog",
				ServiceType: assetservices.ServiceTypeWeb,
				Status:      assetservices.ServiceStatusActive,
			})
			if !errors.Is(err, tt.wantSentinel) {
				t.Fatalf("CreateAssetService() error = %v, want %v", err, tt.wantSentinel)
			}
		})
	}
}

type fakeAssetServiceDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeAssetServiceDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeAssetServiceRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeAssetServiceDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeAssetServiceRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeAssetServiceRow struct {
	scan func(dest ...any) error
}

func (r fakeAssetServiceRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeAssetServiceScan struct {
	scan func(dest ...any) error
}

type fakeAssetServiceRows struct {
	rows []fakeAssetServiceScan
	idx  int
	err  error
}

func (f *fakeAssetServiceRows) Close()                                       {}
func (f *fakeAssetServiceRows) Err() error                                   { return f.err }
func (f *fakeAssetServiceRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeAssetServiceRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeAssetServiceRows) RawValues() [][]byte                          { return nil }
func (f *fakeAssetServiceRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeAssetServiceRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeAssetServiceRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeAssetServiceRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func scanAssetServiceRecordDestinations(dest []any, record assetservices.Record) {
	*(dest[0].(*string)) = record.ServiceID
	*(dest[1].(*string)) = record.VPSID
	*(dest[2].(**string)) = cloneStringPtr(record.TargetID)
	*(dest[3].(*string)) = record.Name
	*(dest[4].(*assetservices.ServiceType)) = record.ServiceType
	*(dest[5].(*assetservices.ServiceStatus)) = record.Status
	*(dest[6].(*string)) = record.URL
	*(dest[7].(**int)) = cloneAssetServiceIntPtr(record.Port)
	*(dest[8].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[9].(*string)) = record.Note
	*(dest[10].(*time.Time)) = record.CreatedAt
	*(dest[11].(*time.Time)) = record.UpdatedAt
}

func cloneAssetServiceIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
