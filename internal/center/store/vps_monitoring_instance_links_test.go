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

func TestPostgresVPSMonitoringOwnershipCannotBeLinkedOrUnlinked(t *testing.T) {
	repo := &PostgresVPSMonitoringInstanceLinkRepository{}
	for _, id := range []string{"", "mi_missing", "mi_current", "mi_retired"} {
		if _, err := repo.LinkMonitoringInstance(context.Background(), "vps_other", assetlinks.LinkInput{MonitoringInstanceID: id}); !errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
			t.Fatalf("link %q = %v", id, err)
		}
		if _, err := repo.UnlinkMonitoringInstance(context.Background(), "vps_owner", assetlinks.UnlinkInput{MonitoringInstanceID: id}); !errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
			t.Fatalf("unlink %q = %v", id, err)
		}
	}
}
func TestPostgresVPSMonitoringHistoryUsesPermanentOwner(t *testing.T) {
	var query string
	repo := &PostgresVPSMonitoringInstanceLinkRepository{db: fakeVPSMonitoringInstanceLinkDB{query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
		query = sql
		return &fakeVPSMonitoringInstanceLinkRows{}, nil
	}}}
	if _, err := repo.ListMonitoringInstancesForVPS(context.Background(), "vps_owner"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(query, "where n.vps_id = $1") || strings.Contains(query, "l.unlinked_at is null") {
		t.Fatalf("history excludes ended phases: %s", query)
	}
}

type fakeVPSMonitoringInstanceLinkDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
	beginTx  func(context.Context, pgx.TxOptions) (pgx.Tx, error)
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

func (f fakeVPSMonitoringInstanceLinkDB) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	if f.beginTx == nil {
		return &fakeVPSMonitoringInstanceLinkTx{queryRow: f.queryRow}, nil
	}
	return f.beginTx(ctx, txOptions)
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

func indexSQL(calls []string, snippet string) int {
	for i, sql := range calls {
		if strings.Contains(sql, snippet) {
			return i
		}
	}
	return -1
}

type fakeVPSMonitoringInstanceLinkTx struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	commit   func(context.Context) error
	rollback func(context.Context) error
}

func (f *fakeVPSMonitoringInstanceLinkTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeVPSMonitoringInstanceLinkTx) Commit(ctx context.Context) error {
	if f.commit != nil {
		return f.commit(ctx)
	}
	return nil
}
func (f *fakeVPSMonitoringInstanceLinkTx) Rollback(ctx context.Context) error {
	if f.rollback != nil {
		return f.rollback(ctx)
	}
	return nil
}
func (f *fakeVPSMonitoringInstanceLinkTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeVPSMonitoringInstanceLinkTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}
func (f *fakeVPSMonitoringInstanceLinkTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (f *fakeVPSMonitoringInstanceLinkTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (f *fakeVPSMonitoringInstanceLinkTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 1"), nil
}
func (f *fakeVPSMonitoringInstanceLinkTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, nil
}
func (f *fakeVPSMonitoringInstanceLinkTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow != nil {
		return f.queryRow(ctx, sql, args...)
	}
	return fakeVPSMonitoringInstanceLinkRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
}
func (f *fakeVPSMonitoringInstanceLinkTx) Conn() *pgx.Conn { return nil }
