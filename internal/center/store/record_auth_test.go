package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
)

const recordAuthTestUserID = "usr_0123456789abcdef01234567"

func TestPostgresRecordAuthorizationRepositoryListsStableGroupIDs(t *testing.T) {
	const wantSQL = `select g.group_id
from public.record_access_groups g
join public.record_access_group_members m on m.group_id = g.group_id
where g.project_id = $1 and m.user_id = $2
order by g.group_id asc`

	db := &fakeRecordAuthorizationDB{
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if got, want := strings.TrimSpace(sql), wantSQL; got != want {
				t.Fatalf("SQL = %q, want %q", got, want)
			}
			if len(args) != 2 || args[0] != recordauth.ProjectIDDefault || args[1] != recordAuthTestUserID {
				t.Fatalf("query args = %#v, want [%q %q]", args, recordauth.ProjectIDDefault, recordAuthTestUserID)
			}
			return &fakeRecordAuthorizationRows{scans: []fakeRecordAuthorizationScan{
				recordAuthorizationGroupIDScan("rag_alpha"),
				recordAuthorizationGroupIDScan("rag_beta"),
			}}, nil
		},
	}
	repo := newPostgresRecordAuthorizationRepository(db)

	got, err := repo.ListActorGroupIDs(context.Background(), recordauth.ProjectIDDefault, recordAuthTestUserID)
	if err != nil {
		t.Fatalf("ListActorGroupIDs() error = %v", err)
	}
	if want := []string{"rag_alpha", "rag_beta"}; !sameStrings(got, want) {
		t.Fatalf("group IDs = %#v, want %#v", got, want)
	}
	if db.queryCalls != 1 {
		t.Fatalf("Query calls = %d, want 1", db.queryCalls)
	}
	if strings.Contains(strings.ToLower(db.sql[0]), "display") || strings.Contains(strings.ToLower(db.sql[0]), "content") {
		t.Fatalf("group lookup selected product fields: %q", db.sql[0])
	}
}

func TestPostgresRecordAuthorizationRepositoryFailsClosed(t *testing.T) {
	queryErr := errors.New("query unavailable")
	scanErr := errors.New("scan unavailable")
	rowsErr := errors.New("rows unavailable")

	tests := []struct {
		name     string
		rows     *fakeRecordAuthorizationRows
		queryErr error
		wantErr  error
	}{
		{
			name:     "query error",
			queryErr: queryErr,
			wantErr:  queryErr,
		},
		{
			name: "scan error",
			rows: &fakeRecordAuthorizationRows{scans: []fakeRecordAuthorizationScan{
				func(...any) error { return scanErr },
			}},
			wantErr: scanErr,
		},
		{
			name:    "rows error",
			rows:    &fakeRecordAuthorizationRows{err: rowsErr},
			wantErr: rowsErr,
		},
		{
			name: "malformed persisted group id",
			rows: &fakeRecordAuthorizationRows{scans: []fakeRecordAuthorizationScan{
				recordAuthorizationGroupIDScan("RAG_not-canonical"),
			}},
			wantErr: recordauth.ErrInvalidActorScope,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakeRecordAuthorizationDB{
				query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
					if tt.queryErr != nil {
						return nil, tt.queryErr
					}
					return tt.rows, nil
				},
			}
			repo := newPostgresRecordAuthorizationRepository(db)

			_, err := repo.ListActorGroupIDs(context.Background(), recordauth.ProjectIDDefault, recordAuthTestUserID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListActorGroupIDs() error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
		})
	}
}

type fakeRecordAuthorizationDB struct {
	query      func(context.Context, string, ...any) (pgx.Rows, error)
	queryCalls int
	sql        []string
	args       [][]any
}

func (db *fakeRecordAuthorizationDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.queryCalls++
	db.sql = append(db.sql, sql)
	db.args = append(db.args, append([]any(nil), args...))
	return db.query(ctx, sql, args...)
}

type fakeRecordAuthorizationScan func(...any) error

type fakeRecordAuthorizationRows struct {
	scans []fakeRecordAuthorizationScan
	index int
	err   error
}

func (rows *fakeRecordAuthorizationRows) Close()                                       {}
func (rows *fakeRecordAuthorizationRows) Err() error                                   { return rows.err }
func (rows *fakeRecordAuthorizationRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *fakeRecordAuthorizationRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *fakeRecordAuthorizationRows) RawValues() [][]byte                          { return nil }
func (rows *fakeRecordAuthorizationRows) Values() ([]any, error)                       { return nil, nil }
func (rows *fakeRecordAuthorizationRows) Conn() *pgx.Conn                              { return nil }
func (rows *fakeRecordAuthorizationRows) Next() bool {
	if rows.index >= len(rows.scans) {
		return false
	}
	rows.index++
	return true
}
func (rows *fakeRecordAuthorizationRows) Scan(dest ...any) error {
	return rows.scans[rows.index-1](dest...)
}

func recordAuthorizationGroupIDScan(groupID string) fakeRecordAuthorizationScan {
	return func(dest ...any) error {
		*(dest[0].(*string)) = groupID
		return nil
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
