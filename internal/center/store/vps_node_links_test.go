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

func TestPostgresVPSNodeLinkMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0019_create_vps_node_links.sql"))
	if err != nil {
		t.Fatalf("ReadFile(vps node links migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists vps_node_links",
		"link_id text primary key",
		"vps_id text not null references vps_assets(vps_id) on delete cascade",
		"node_id text not null references nodes(node_id) on delete cascade",
		"linked_at timestamptz not null default now()",
		"unlinked_at timestamptz",
		"note text not null default ''",
		"idx_vps_node_links_pair_active",
		"on vps_node_links (vps_id, node_id)",
		"where unlinked_at is null",
		"idx_vps_node_links_vps_active",
		"idx_vps_node_links_node_active",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("vps node link migration missing %q", snippet)
		}
	}
}

func TestPostgresVPSNodeLinkLinkUnlinkListAndCount(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 16, 0, 0, 0, time.UTC)
	unlinkedAt := now.Add(time.Hour)
	providerID := "pv_001"
	var rowCalls []string
	var rowArgs [][]any
	var queryCalls []string
	var queryArgs [][]any
	repo := &PostgresVPSNodeLinkRepository{db: fakeVPSNodeLinkDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCalls = append(rowCalls, sql)
			rowArgs = append(rowArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "insert into vps_node_links"):
				return fakeVPSNodeLinkRow{scan: func(dest ...any) error {
					linkID, ok := args[0].(string)
					if !ok || !strings.HasPrefix(linkID, "vnl_") {
						t.Fatalf("generated link id arg = %#v, want vnl_ prefix", args[0])
					}
					scanVPSNodeLinkRecordDestinations(dest, assetlinks.Record{
						LinkID:   linkID,
						VPSID:    "vps_001",
						NodeID:   "nd_001",
						LinkedAt: now,
						Note:     "primary",
					})
					return nil
				}}
			case strings.Contains(sql, "update vps_node_links"):
				return fakeVPSNodeLinkRow{scan: func(dest ...any) error {
					scanVPSNodeLinkRecordDestinations(dest, assetlinks.Record{
						LinkID:     "vnl_001",
						VPSID:      "vps_001",
						NodeID:     "nd_001",
						LinkedAt:   now,
						UnlinkedAt: &unlinkedAt,
						Note:       "rotated",
					})
					return nil
				}}
			case strings.Contains(sql, "select count(*)"):
				return fakeVPSNodeLinkRow{scan: func(dest ...any) error {
					*(dest[0].(*int)) = 2
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow SQL %q", sql)
				return fakeVPSNodeLinkRow{scan: func(dest ...any) error { return nil }}
			}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalls = append(queryCalls, sql)
			queryArgs = append(queryArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "join nodes n"):
				return &fakeVPSNodeLinkRows{rows: []fakeVPSNodeLinkScan{{
					scan: func(dest ...any) error {
						*(dest[0].(*string)) = "nd_001"
						*(dest[1].(*string)) = "Tokyo Node"
						*(dest[2].(*string)) = "edge"
						*(dest[3].(*string)) = "JP"
						*(dest[4].(*string)) = "Tokyo"
						*(dest[5].(*string)) = "Node Hint"
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
				return &fakeVPSNodeLinkRows{rows: []fakeVPSNodeLinkScan{{
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
				return &fakeVPSNodeLinkRows{}, nil
			}
		},
	}}

	linked, err := repo.LinkNode(context.Background(), "vps_001", assetlinks.LinkInput{NodeID: " nd_001 ", Note: " primary "})
	if err != nil {
		t.Fatalf("LinkNode() error = %v", err)
	}
	if !strings.HasPrefix(linked.LinkID, "vnl_") || linked.NodeID != "nd_001" {
		t.Fatalf("LinkNode() = %#v, want generated active link", linked)
	}
	if len(rowArgs[0]) != 4 || rowArgs[0][1] != "vps_001" || rowArgs[0][2] != "nd_001" || rowArgs[0][3] != "primary" {
		t.Fatalf("link args = %#v, want normalized vps/node/note", rowArgs[0])
	}

	unlinked, err := repo.UnlinkNode(context.Background(), "vps_001", assetlinks.UnlinkInput{NodeID: " nd_001 ", Note: " rotated "})
	if err != nil {
		t.Fatalf("UnlinkNode() error = %v", err)
	}
	if unlinked.UnlinkedAt == nil {
		t.Fatalf("UnlinkNode().UnlinkedAt = nil, want historical timestamp")
	}
	for _, snippet := range []string{
		"update vps_node_links",
		"set unlinked_at = now()",
		"and unlinked_at is null",
		"returning " + vpsNodeLinkSelectColumns,
	} {
		if !strings.Contains(rowCalls[1], snippet) {
			t.Fatalf("UnlinkNode SQL missing %q in %q", snippet, rowCalls[1])
		}
	}
	if strings.Contains(rowCalls[1], "update nodes") {
		t.Fatalf("UnlinkNode SQL must not update nodes: %q", rowCalls[1])
	}

	nodes, err := repo.ListNodesForVPS(context.Background(), "vps_001")
	if err != nil {
		t.Fatalf("ListNodesForVPS() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].NodeID != "nd_001" || nodes[0].CurrentHealthStatus != "关注" {
		t.Fatalf("ListNodesForVPS() = %#v, want node health summary", nodes)
	}
	if queryArgs[0][0] != "vps_001" || !strings.Contains(queryCalls[0], "where l.vps_id = $1") || !strings.Contains(queryCalls[0], "l.unlinked_at is null") {
		t.Fatalf("ListNodesForVPS SQL/args = %q %#v, want active vps filter", queryCalls[0], queryArgs[0])
	}

	vpsAssets, err := repo.ListVPSForNode(context.Background(), "nd_001")
	if err != nil {
		t.Fatalf("ListVPSForNode() error = %v", err)
	}
	if len(vpsAssets) != 1 || vpsAssets[0].VPSID != "vps_001" || vpsAssets[0].ProviderName != "Asset Provider" {
		t.Fatalf("ListVPSForNode() = %#v, want vps summary", vpsAssets)
	}
	if queryArgs[1][0] != "nd_001" || !strings.Contains(queryCalls[1], "where l.node_id = $1") || !strings.Contains(queryCalls[1], "l.unlinked_at is null") {
		t.Fatalf("ListVPSForNode SQL/args = %q %#v, want active node filter", queryCalls[1], queryArgs[1])
	}

	count, err := repo.CountActiveLinksForVPS(context.Background(), "vps_001")
	if err != nil {
		t.Fatalf("CountActiveLinksForVPS() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("CountActiveLinksForVPS() = %d, want 2", count)
	}
	if strings.Contains(strings.Join(rowCalls, "\n"), "update nodes") {
		t.Fatalf("repository must not update nodes; SQL calls: %#v", rowCalls)
	}
}

func TestPostgresVPSNodeLinkRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	repo := &PostgresVPSNodeLinkRepository{db: fakeVPSNodeLinkDB{}}
	if _, err := repo.LinkNode(context.Background(), "vps_001", assetlinks.LinkInput{NodeID: " "}); !errors.Is(err, assetlinks.ErrInvalidVPSNodeLinkInput) {
		t.Fatalf("LinkNode(blank node) error = %v, want ErrInvalidVPSNodeLinkInput", err)
	}
	if _, err := repo.UnlinkNode(context.Background(), "vps_001", assetlinks.UnlinkInput{NodeID: " "}); !errors.Is(err, assetlinks.ErrInvalidVPSNodeLinkInput) {
		t.Fatalf("UnlinkNode(blank node) error = %v, want ErrInvalidVPSNodeLinkInput", err)
	}
}

func TestPostgresVPSNodeLinkMapsConflictForeignKeyAndMissingActiveLink(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want error
	}{
		{name: "duplicate active link", err: &pgconn.PgError{Code: "23505", ConstraintName: "idx_vps_node_links_pair_active"}, want: assetlinks.ErrVPSNodeLinkConflict},
		{name: "missing vps or node", err: &pgconn.PgError{Code: "23503", ConstraintName: "vps_node_links_node_id_fkey"}, want: assetlinks.ErrVPSNodeLinkNotFound},
		{name: "invalid check", err: &pgconn.PgError{Code: "23514", ConstraintName: "vps_node_links_note_not_null"}, want: assetlinks.ErrInvalidVPSNodeLinkInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &PostgresVPSNodeLinkRepository{db: fakeVPSNodeLinkDB{
				queryRow: func(context.Context, string, ...any) pgx.Row {
					return fakeVPSNodeLinkRow{scan: func(dest ...any) error { return tt.err }}
				},
			}}
			_, err := repo.LinkNode(context.Background(), "vps_001", assetlinks.LinkInput{NodeID: "nd_001"})
			if !errors.Is(err, tt.want) {
				t.Fatalf("LinkNode() error = %v, want %v", err, tt.want)
			}
		})
	}

	repo := &PostgresVPSNodeLinkRepository{db: fakeVPSNodeLinkDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeVPSNodeLinkRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}
	if _, err := repo.UnlinkNode(context.Background(), "vps_001", assetlinks.UnlinkInput{NodeID: "nd_missing"}); !errors.Is(err, assetlinks.ErrVPSNodeLinkNotFound) {
		t.Fatalf("UnlinkNode() error = %v, want ErrVPSNodeLinkNotFound", err)
	}
}

type fakeVPSNodeLinkDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeVPSNodeLinkDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeVPSNodeLinkRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeVPSNodeLinkDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeVPSNodeLinkRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeVPSNodeLinkRow struct {
	scan func(dest ...any) error
}

func (r fakeVPSNodeLinkRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeVPSNodeLinkScan struct {
	scan func(dest ...any) error
}

type fakeVPSNodeLinkRows struct {
	rows []fakeVPSNodeLinkScan
	idx  int
	err  error
}

func (f *fakeVPSNodeLinkRows) Close()                                       {}
func (f *fakeVPSNodeLinkRows) Err() error                                   { return f.err }
func (f *fakeVPSNodeLinkRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeVPSNodeLinkRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeVPSNodeLinkRows) RawValues() [][]byte                          { return nil }
func (f *fakeVPSNodeLinkRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeVPSNodeLinkRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeVPSNodeLinkRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeVPSNodeLinkRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func scanVPSNodeLinkRecordDestinations(dest []any, record assetlinks.Record) {
	*(dest[0].(*string)) = record.LinkID
	*(dest[1].(*string)) = record.VPSID
	*(dest[2].(*string)) = record.NodeID
	*(dest[3].(*time.Time)) = record.LinkedAt
	*(dest[4].(**time.Time)) = cloneTimePtr(record.UnlinkedAt)
	*(dest[5].(*string)) = record.Note
}
