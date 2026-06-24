package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/auth"
)

type fakeSessionDB struct {
	execSQL  []string
	execArgs [][]any
	row      fakeSessionRow
}

func (f *fakeSessionDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, args)
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (f *fakeSessionDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, args)
	return f.row
}

type fakeSessionRow struct {
	scan func(dest ...any) error
}

func (f fakeSessionRow) Scan(dest ...any) error {
	if f.scan != nil {
		return f.scan(dest...)
	}
	return nil
}

func TestPostgresSessionRepositoryStoresHashInsteadOfPlainSessionID(t *testing.T) {
	t.Parallel()

	db := &fakeSessionDB{}
	repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
	}
	session := auth.Session{
		SessionID:  "plain-session-id",
		UserID:     "usr_001",
		IssuedAt:   time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC),
		LastSeenAt: time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2026, time.June, 24, 10, 0, 0, 0, time.UTC),
		UserAgent:  "ua",
		ClientIP:   "203.0.113.10",
	}

	if err := repo.Create(context.Background(), session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if len(db.execArgs) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(db.execArgs))
	}
	if !strings.Contains(db.execSQL[0], "session_id_hash") {
		t.Fatalf("insert SQL = %q, want session_id_hash column", db.execSQL[0])
	}
	storedID, ok := db.execArgs[0][0].(string)
	if !ok {
		t.Fatalf("stored id arg type = %T, want string", db.execArgs[0][0])
	}
	if storedID == session.SessionID {
		t.Fatal("repository stored plaintext session ID")
	}
	if len(storedID) != 64 {
		t.Fatalf("stored hash length = %d, want sha256 hex length 64", len(storedID))
	}
}

func TestPostgresSessionRepositoryFindQueriesByHashAndReturnsPlainSessionID(t *testing.T) {
	t.Parallel()

	db := &fakeSessionDB{}
	repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
	}
	issuedAt := time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC)
	db.row = fakeSessionRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = "usr_001"
		*(dest[1].(*time.Time)) = issuedAt
		*(dest[2].(*time.Time)) = issuedAt
		*(dest[3].(*time.Time)) = issuedAt.Add(time.Hour)
		*(dest[4].(*string)) = "ua"
		*(dest[5].(*string)) = "203.0.113.10"
		return nil
	}}

	session, err := repo.Find(context.Background(), "plain-session-id")
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if session.SessionID != "plain-session-id" {
		t.Fatalf("SessionID = %q, want caller plaintext id", session.SessionID)
	}
	if len(db.execArgs) != 1 {
		t.Fatalf("query calls = %d, want 1", len(db.execArgs))
	}
	queryID, ok := db.execArgs[0][0].(string)
	if !ok {
		t.Fatalf("query id arg type = %T, want string", db.execArgs[0][0])
	}
	if queryID == "plain-session-id" {
		t.Fatal("repository queried by plaintext session ID")
	}
	if !strings.Contains(db.execSQL[0], "session_id_hash = $1") {
		t.Fatalf("query SQL = %q, want session_id_hash predicate", db.execSQL[0])
	}
}

func TestNewPostgresSessionRepositoryRequiresHMACKey(t *testing.T) {
	t.Parallel()

	if _, err := NewPostgresSessionRepository(nil, nil); err == nil {
		t.Fatal("NewPostgresSessionRepository() error = nil, want non-nil for empty HMAC key")
	}
}
