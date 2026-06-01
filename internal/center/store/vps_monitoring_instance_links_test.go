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

	"houfeng/internal/center/assetlinks"
)

func TestPostgresVPSMonitoringInstanceLinkMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0029_rename_nodes_to_monitoring_instances.sql"))
	if err != nil {
		t.Fatalf("ReadFile(vps monitoringInstance links migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"alter table vps_node_links rename to vps_monitoring_instance_links",
		"alter table vps_monitoring_instance_links rename column node_id to monitoring_instance_id",
		"idx_vps_monitoring_instance_links_pair_active",
		"idx_vps_monitoring_instance_links_vps_active",
		"idx_vps_monitoring_instance_links_monitoring_instance_active",
		"vps_monitoring_instance_links_monitoring_instance_id_fkey",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("vps monitoringInstance link migration missing %q", snippet)
		}
	}
}

func TestPostgresVPSMonitoringInstanceLinkLinkUnlinkListAndCount(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	unlinkedAt := now.Add(time.Hour)
	providerID := "pv_001"
	var rowCalls []string
	var rowArgs [][]any
	var queryCalls []string
	var queryArgs [][]any
	repo := &PostgresVPSMonitoringInstanceLinkRepository{db: fakeVPSMonitoringInstanceLinkDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCalls = append(rowCalls, sql)
			rowArgs = append(rowArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "insert into vps_monitoring_instance_links"):
				return fakeVPSMonitoringInstanceLinkRow{scan: func(dest ...any) error {
					linkID, ok := args[0].(string)
					if !ok || !strings.HasPrefix(linkID, "vnl_") {
						t.Fatalf("generated link id arg = %#v, want vnl_ prefix", args[0])
					}
					scanVPSMonitoringInstanceLinkRecordDestinations(dest, assetlinks.Record{
						LinkID:               linkID,
						VPSID:                "vps_001",
						MonitoringInstanceID: "mi_001",
						LinkedAt:             now,
						Note:                 "primary",
					})
					return nil
				}}
			case strings.Contains(sql, "update vps_monitoring_instance_links"):
				return fakeVPSMonitoringInstanceLinkRow{scan: func(dest ...any) error {
					scanVPSMonitoringInstanceLinkRecordDestinations(dest, assetlinks.Record{
						LinkID:               "vnl_001",
						VPSID:                "vps_001",
						MonitoringInstanceID: "mi_001",
						LinkedAt:             now,
						UnlinkedAt:           &unlinkedAt,
						Note:                 "rotated",
					})
					return nil
				}}
			case strings.Contains(sql, "select count(*)"):
				return fakeVPSMonitoringInstanceLinkRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 2
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow SQL %q", sql)
				return fakeVPSMonitoringInstanceLinkRow{scan: func(dest ...any) error { return nil }}
			}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalls = append(queryCalls, sql)
			queryArgs = append(queryArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "join monitoring_instances n"):
				return &fakeVPSMonitoringInstanceLinkRows{rows: []fakeVPSMonitoringInstanceLinkScan{{
					scan: func(dest ...any) error {
						*(dest[0].(*string)) = "mi_001"
						*(dest[1].(*string)) = "Tokyo MonitoringInstance"
						*(dest[2].(*string)) = "edge"
						*(dest[3].(*string)) = "JP"
						*(dest[4].(*string)) = "Tokyo"
						*(dest[5].(*string)) = "MonitoringInstance Hint"
						*(dest[6].(*string)) = "在用"
						*(dest[7].(*string)) = "启用"
						*(dest[8].(*string)) = "已绑定"
						*(dest[9].(*string)) = "关注"
						*(dest[10].(**time.Time)) = cloneTimePtr(&now)
						*(dest[11].(**time.Time)) = cloneTimePtr(&now)
						*(dest[12].(*int)) = 1
						*(dest[13].(*string)) = "high latency"
						*(dest[14].(*time.Time)) = now
						*(dest[15].(*string)) = "primary"
						return nil
					},
				}}}, nil
			case strings.Contains(sql, "join vps_assets v"):
				return &fakeVPSMonitoringInstanceLinkRows{rows: []fakeVPSMonitoringInstanceLinkScan{{
					scan: func(dest ...any) error {
						*(dest[0].(*string)) = "vps_001"
						*(dest[1].(*string)) = "Tokyo VPS"
						*(dest[2].(**string)) = cloneStringPtr(&providerID)
						*(dest[3].(*string)) = "Asset Provider"
						*(dest[4].(*string)) = "JP"
						*(dest[5].(*string)) = "Kanto"
						*(dest[6].(*string)) = "Tokyo"
						*(dest[7].(*string)) = "active"
						*(dest[8].(*string)) = "in_use"
						*(dest[9].(*string)) = "keep"
						*(dest[10].(*string)) = "normal"
						*(dest[11].(*[]string)) = []string{"edge"}
						*(dest[12].(**time.Time)) = nil
						*(dest[13].(*time.Time)) = now
						*(dest[14].(*string)) = "primary"
						return nil
					},
				}}}, nil
			default:
				t.Fatalf("unexpected Query SQL %q", sql)
				return &fakeVPSMonitoringInstanceLinkRows{}, nil
			}
		},
	}}

	linked, err := repo.LinkMonitoringInstance(context.Background(), "vps_001", assetlinks.LinkInput{MonitoringInstanceID: " mi_001 ", Note: " primary "})
	if err != nil {
		t.Fatalf("LinkMonitoringInstance() error = %v", err)
	}
	if !strings.HasPrefix(linked.LinkID, "vnl_") || linked.MonitoringInstanceID != "mi_001" {
		t.Fatalf("LinkMonitoringInstance() = %#v, want generated active link", linked)
	}
	if len(rowArgs[0]) != 4 || rowArgs[0][1] != "vps_001" || rowArgs[0][2] != "mi_001" || rowArgs[0][3] != "primary" {
		t.Fatalf("link args = %#v, want normalized vps/monitoringInstance/note", rowArgs[0])
	}

	unlinked, err := repo.UnlinkMonitoringInstance(context.Background(), "vps_001", assetlinks.UnlinkInput{MonitoringInstanceID: " mi_001 ", Note: " rotated "})
	if err != nil {
		t.Fatalf("UnlinkMonitoringInstance() error = %v", err)
	}
	if unlinked.UnlinkedAt == nil {
		t.Fatalf("UnlinkMonitoringInstance().UnlinkedAt = nil, want historical timestamp")
	}
	for _, snippet := range []string{
		"update vps_monitoring_instance_links",
		"set unlinked_at = now()",
		"and unlinked_at is null",
		"returning " + vpsMonitoringInstanceLinkSelectColumns,
	} {
		if !strings.Contains(rowCalls[1], snippet) {
			t.Fatalf("UnlinkMonitoringInstance SQL missing %q in %q", snippet, rowCalls[1])
		}
	}
	if strings.Contains(rowCalls[1], "update monitoring_instances") {
		t.Fatalf("UnlinkMonitoringInstance SQL must not update monitoring_instances: %q", rowCalls[1])
	}

	monitoringInstances, err := repo.ListMonitoringInstancesForVPS(context.Background(), "vps_001")
	if err != nil {
		t.Fatalf("ListMonitoringInstancesForVPS() error = %v", err)
	}
	if len(monitoringInstances) != 1 || monitoringInstances[0].MonitoringInstanceID != "mi_001" || monitoringInstances[0].CurrentHealthStatus != "关注" {
		t.Fatalf("ListMonitoringInstancesForVPS() = %#v, want monitoringInstance health summary", monitoringInstances)
	}
	if queryArgs[0][0] != "vps_001" || !strings.Contains(queryCalls[0], "where l.vps_id = $1") || !strings.Contains(queryCalls[0], "l.unlinked_at is null") {
		t.Fatalf("ListMonitoringInstancesForVPS SQL/args = %q %#v, want active vps filter", queryCalls[0], queryArgs[0])
	}

	vpsAssets, err := repo.ListVPSForMonitoringInstance(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("ListVPSForMonitoringInstance() error = %v", err)
	}
	if len(vpsAssets) != 1 || vpsAssets[0].VPSID != "vps_001" || vpsAssets[0].ProviderName != "Asset Provider" {
		t.Fatalf("ListVPSForMonitoringInstance() = %#v, want vps summary", vpsAssets)
	}
	if queryArgs[1][0] != "mi_001" || !strings.Contains(queryCalls[1], "where l.monitoring_instance_id = $1") || !strings.Contains(queryCalls[1], "l.unlinked_at is null") {
		t.Fatalf("ListVPSForMonitoringInstance SQL/args = %q %#v, want active monitoringInstance filter", queryCalls[1], queryArgs[1])
	}

	count, err := repo.CountActiveLinksForVPS(context.Background(), "vps_001")
	if err != nil {
		t.Fatalf("CountActiveLinksForVPS() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("CountActiveLinksForVPS() = %d, want 2", count)
	}
	if strings.Contains(strings.Join(rowCalls, "\n"), "update monitoring_instances") {
		t.Fatalf("repository must not update monitoring_instances; SQL calls: %#v", rowCalls)
	}
}

func TestPostgresVPSMonitoringInstanceLinkRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	repo := &PostgresVPSMonitoringInstanceLinkRepository{db: fakeVPSMonitoringInstanceLinkDB{}}
	if _, err := repo.LinkMonitoringInstance(context.Background(), "vps_001", assetlinks.LinkInput{MonitoringInstanceID: " "}); !errors.Is(err, assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput) {
		t.Fatalf("LinkMonitoringInstance(blank monitoringInstance) error = %v, want ErrInvalidVPSMonitoringInstanceLinkInput", err)
	}
	if _, err := repo.UnlinkMonitoringInstance(context.Background(), "vps_001", assetlinks.UnlinkInput{MonitoringInstanceID: " "}); !errors.Is(err, assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput) {
		t.Fatalf("UnlinkMonitoringInstance(blank monitoringInstance) error = %v, want ErrInvalidVPSMonitoringInstanceLinkInput", err)
	}
}

func TestPostgresVPSMonitoringInstanceLinkMapsConflictForeignKeyAndMissingActiveLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "duplicate active link", err: &pgconn.PgError{Code: "23505", ConstraintName: "idx_vps_monitoring_instance_links_pair_active"}, want: assetlinks.ErrVPSMonitoringInstanceLinkConflict},
		{name: "missing vps or monitoringInstance", err: &pgconn.PgError{Code: "23503", ConstraintName: "vps_monitoring_instance_links_monitoring_instance_id_fkey"}, want: assetlinks.ErrVPSMonitoringInstanceLinkNotFound},
		{name: "invalid check", err: &pgconn.PgError{Code: "23514", ConstraintName: "vps_monitoring_instance_links_note_not_null"}, want: assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &PostgresVPSMonitoringInstanceLinkRepository{db: fakeVPSMonitoringInstanceLinkDB{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeVPSMonitoringInstanceLinkRow{scan: func(dest ...any) error { return tt.err }}
				},
			}}
			_, err := repo.LinkMonitoringInstance(context.Background(), "vps_001", assetlinks.LinkInput{MonitoringInstanceID: "mi_001"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("LinkMonitoringInstance() error = %v, want %v", err, tt.want)
			}
		})
	}

	repo := &PostgresVPSMonitoringInstanceLinkRepository{db: fakeVPSMonitoringInstanceLinkDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeVPSMonitoringInstanceLinkRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}
	if _, err := repo.UnlinkMonitoringInstance(context.Background(), "vps_001", assetlinks.UnlinkInput{MonitoringInstanceID: "mi_missing"}); !errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkNotFound) {
		t.Fatalf("UnlinkMonitoringInstance() error = %v, want ErrVPSMonitoringInstanceLinkNotFound", err)
	}
}

type fakeVPSMonitoringInstanceLinkDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeVPSMonitoringInstanceLinkDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeVPSMonitoringInstanceLinkRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeVPSMonitoringInstanceLinkDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeVPSMonitoringInstanceLinkRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeVPSMonitoringInstanceLinkRow struct {
	scan func(dest ...any) error
}

func (r fakeVPSMonitoringInstanceLinkRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeVPSMonitoringInstanceLinkScan struct {
	scan func(dest ...any) error
}

type fakeVPSMonitoringInstanceLinkRows struct {
	rows []fakeVPSMonitoringInstanceLinkScan
	idx  int
	err  error
}

func (f *fakeVPSMonitoringInstanceLinkRows) Close()     {}
func (f *fakeVPSMonitoringInstanceLinkRows) Err() error { return f.err }
func (f *fakeVPSMonitoringInstanceLinkRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}
func (f *fakeVPSMonitoringInstanceLinkRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeVPSMonitoringInstanceLinkRows) RawValues() [][]byte                          { return nil }
func (f *fakeVPSMonitoringInstanceLinkRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeVPSMonitoringInstanceLinkRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeVPSMonitoringInstanceLinkRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeVPSMonitoringInstanceLinkRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func scanVPSMonitoringInstanceLinkRecordDestinations(dest []any, record assetlinks.Record) {
	*(dest[0].(*string)) = record.LinkID
	*(dest[1].(*string)) = record.VPSID
	*(dest[2].(*string)) = record.MonitoringInstanceID
	*(dest[3].(*time.Time)) = record.LinkedAt
	*(dest[4].(**time.Time)) = cloneTimePtr(record.UnlinkedAt)
	*(dest[5].(*string)) = record.Note
}
