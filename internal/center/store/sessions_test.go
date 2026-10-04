package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/auth"
)

type fakeSessionDB struct {
	execArgs  [][]any
	queries   []string
	queryArgs [][]any
	row       fakeSessionRow
	tx        *fakeSessionTx
}

func (f *fakeSessionDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	f.execArgs = append(f.execArgs, args)
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (f *fakeSessionDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	f.queries = append(f.queries, query)
	f.queryArgs = append(f.queryArgs, append([]any(nil), args...))
	return f.row
}

func (f *fakeSessionDB) BeginTx(_ context.Context, _ pgx.TxOptions) (sessionTx, error) {
	if f.tx == nil {
		f.tx = &fakeSessionTx{}
	}
	return f.tx, nil
}

type fakeSessionTx struct {
	rows      []pgx.Row
	execTags  []pgconn.CommandTag
	commitErr error
	commits   int
}

func (f *fakeSessionTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	if len(f.execTags) > 0 {
		tag := f.execTags[0]
		f.execTags = f.execTags[1:]
		return tag, nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeSessionTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	if len(f.rows) == 0 {
		return fakeSessionRow{scan: func(...any) error { return errors.New("unexpected QueryRow") }}
	}
	row := f.rows[0]
	f.rows = f.rows[1:]
	return row
}

func (f *fakeSessionTx) Commit(context.Context) error {
	f.commits++
	return f.commitErr
}

func (f *fakeSessionTx) Rollback(context.Context) error { return nil }

type fakeSessionRow struct {
	scan func(dest ...any) error
}

func (f fakeSessionRow) Scan(dest ...any) error {
	if f.scan != nil {
		return f.scan(dest...)
	}
	return nil
}

func sessionUserRow(hash string, changedAt time.Time) fakeSessionRow {
	return fakeSessionRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = hash
		*(dest[1].(*time.Time)) = changedAt
		return nil
	}}
}

func sessionOwnerRow(userID string) fakeSessionRow {
	return fakeSessionRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = userID
		return nil
	}}
}

func sessionDetailsRow(userID string, issuedAt, lastSeenAt, expiresAt time.Time) fakeSessionRow {
	return fakeSessionRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = userID
		*(dest[1].(*time.Time)) = issuedAt
		*(dest[2].(*time.Time)) = lastSeenAt
		*(dest[3].(*time.Time)) = expiresAt
		*(dest[4].(*string)) = "ua"
		*(dest[5].(*string)) = "203.0.113.10"
		return nil
	}}
}

func sessionPasswordChangedRow(changedAt time.Time) fakeSessionRow {
	return fakeSessionRow{scan: func(dest ...any) error {
		*(dest[0].(*time.Time)) = changedAt
		return nil
	}}
}

func sessionValidationRow(issuedAt, expiresAt, passwordChangedAt time.Time) fakeSessionRow {
	return fakeSessionRow{scan: func(dest ...any) error {
		*(dest[0].(*time.Time)) = issuedAt
		*(dest[1].(*time.Time)) = expiresAt
		*(dest[2].(*time.Time)) = passwordChangedAt
		return nil
	}}
}

func TestPostgresSessionRepositoryValidateSessionIsReadOnlyAndCurrent(t *testing.T) {
	now := time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")

	t.Run("valid does not renew", func(t *testing.T) {
		db := &fakeSessionDB{row: sessionValidationRow(now.Add(-time.Minute), now.Add(time.Hour), now.Add(-time.Hour))}
		repo, err := newPostgresSessionRepositoryWithDB(db, key)
		if err != nil {
			t.Fatalf("new repository: %v", err)
		}
		if err := repo.ValidateSession(context.Background(), "plain-session-id", func() time.Time { return now }); err != nil {
			t.Fatalf("ValidateSession() = %v", err)
		}
		if len(db.execArgs) != 0 || db.tx != nil {
			t.Fatalf("ValidateSession performed a write or transaction: exec=%d tx=%v", len(db.execArgs), db.tx != nil)
		}
		if len(db.queryArgs) != 1 || len(db.queryArgs[0]) != 1 {
			t.Fatalf("query args = %#v, want one hashed session id", db.queryArgs)
		}
		hashedID, ok := db.queryArgs[0][0].(string)
		if !ok || hashedID == "plain-session-id" || len(hashedID) != 64 {
			t.Fatalf("query session id = %#v, want 64-character HMAC digest", db.queryArgs[0][0])
		}
		query := strings.ToLower(db.queries[0])
		if strings.Contains(query, "for update") || strings.Contains(query, "update ") || strings.Contains(query, "delete ") {
			t.Fatalf("validation query has write/lock clause: %q", db.queries[0])
		}
	})

	t.Run("expired and password watermark are rejected without deletion", func(t *testing.T) {
		cases := []struct {
			name              string
			issuedAt          time.Time
			expiresAt         time.Time
			passwordChangedAt time.Time
		}{
			{name: "expires at now", issuedAt: now.Add(-time.Hour), expiresAt: now, passwordChangedAt: now.Add(-time.Hour)},
			{name: "issued before password change", issuedAt: now.Add(-time.Hour), expiresAt: now.Add(time.Hour), passwordChangedAt: now},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				db := &fakeSessionDB{row: sessionValidationRow(tt.issuedAt, tt.expiresAt, tt.passwordChangedAt)}
				repo, err := newPostgresSessionRepositoryWithDB(db, key)
				if err != nil {
					t.Fatalf("new repository: %v", err)
				}
				err = repo.ValidateSession(context.Background(), "sid", func() time.Time { return now })
				if !errors.Is(err, auth.ErrSessionExpired) {
					t.Fatalf("ValidateSession() = %v, want ErrSessionExpired", err)
				}
				if len(db.execArgs) != 0 {
					t.Fatalf("ValidateSession writes = %d, want 0", len(db.execArgs))
				}
			})
		}
	})

	t.Run("missing and query errors preserve classification", func(t *testing.T) {
		missing := &fakeSessionDB{row: fakeSessionRow{scan: func(...any) error { return pgx.ErrNoRows }}}
		repo, err := newPostgresSessionRepositoryWithDB(missing, key)
		if err != nil {
			t.Fatalf("new repository: %v", err)
		}
		if err := repo.ValidateSession(context.Background(), "sid", func() time.Time { return now }); !errors.Is(err, auth.ErrSessionNotFound) {
			t.Fatalf("ValidateSession(missing) = %v, want ErrSessionNotFound", err)
		}

		queryErr := errors.New("database unavailable")
		failed := &fakeSessionDB{row: fakeSessionRow{scan: func(...any) error { return queryErr }}}
		repo, err = newPostgresSessionRepositoryWithDB(failed, key)
		if err != nil {
			t.Fatalf("new repository: %v", err)
		}
		err = repo.ValidateSession(context.Background(), "sid", func() time.Time { return now })
		if !errors.Is(err, queryErr) || !strings.Contains(err.Error(), "validate session") {
			t.Fatalf("ValidateSession(query error) = %v, want wrapped database error", err)
		}
	})
}

func TestPostgresSessionRepositoryDeletesByHMACInsteadOfPlainSessionID(t *testing.T) {
	db := &fakeSessionDB{}
	repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
	}
	if err := repo.Delete(context.Background(), "plain-session-id"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(db.execArgs) != 1 {
		t.Fatalf("exec calls = %d, want one hashed delete", len(db.execArgs))
	}
	storedID, ok := db.execArgs[0][0].(string)
	if !ok {
		t.Fatalf("stored id arg type = %T, want string", db.execArgs[0][0])
	}
	if storedID == "plain-session-id" {
		t.Fatal("repository sent plaintext session ID")
	}
	if len(storedID) != 64 {
		t.Fatalf("stored hash length = %d, want sha256 hex length 64", len(storedID))
	}
}

func TestPostgresSessionRepositoryCreateReturnsPersistedTimesAndRejectsStaleHash(t *testing.T) {
	now := time.Date(2026, time.June, 23, 10, 0, 0, 987654321, time.FixedZone("offset", 3600))
	persistedIssuedAt := now.UTC().Truncate(time.Microsecond).Add(time.Microsecond)
	db := &fakeSessionDB{tx: &fakeSessionTx{rows: []pgx.Row{
		sessionUserRow("old-hash", time.Time{}),
		sessionDetailsRow("usr_001", persistedIssuedAt, persistedIssuedAt, persistedIssuedAt.Add(time.Hour)),
	}}}
	repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
	}
	got, err := repo.CreateIfPasswordHash(context.Background(), "old-hash", auth.Session{
		SessionID: "plain-session-id",
		UserID:    "usr_001",
		UserAgent: "ua",
		ClientIP:  "203.0.113.10",
	}, func() time.Time { return now }, time.Hour)
	if err != nil {
		t.Fatalf("CreateIfPasswordHash() error = %v", err)
	}
	if got.SessionID != "plain-session-id" || !got.IssuedAt.Equal(persistedIssuedAt) || !got.ExpiresAt.Equal(persistedIssuedAt.Add(time.Hour)) {
		t.Fatalf("persisted session = %+v", got)
	}

	staleDB := &fakeSessionDB{tx: &fakeSessionTx{rows: []pgx.Row{sessionUserRow("current-hash", time.Time{})}}}
	staleRepo, err := newPostgresSessionRepositoryWithDB(staleDB, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
	}
	_, err = staleRepo.CreateIfPasswordHash(context.Background(), "stale-hash", auth.Session{SessionID: "sid", UserID: "usr_001"}, time.Now, time.Hour)
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("CreateIfPasswordHash() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestPostgresSessionRepositoryChangePasswordCommitsAtomicBoundary(t *testing.T) {
	changedAt := time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC)
	now := changedAt.Add(time.Minute)
	db := &fakeSessionDB{tx: &fakeSessionTx{rows: []pgx.Row{
		sessionUserRow("old-hash", changedAt),
		sessionDetailsRow("usr_001", changedAt, changedAt, now.Add(time.Hour)),
	}, execTags: []pgconn.CommandTag{
		pgconn.NewCommandTag("UPDATE 1"),
		pgconn.NewCommandTag("UPDATE 1"),
		pgconn.NewCommandTag("DELETE 1"),
	}}}
	repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
	}
	if err := repo.ChangePasswordIfHash(context.Background(), "usr_001", "sid", "old-hash", "new-hash", func() time.Time { return now }); err != nil {
		t.Fatalf("ChangePasswordIfHash() error = %v", err)
	}
	if db.tx.commits != 1 {
		t.Fatalf("commits = %d, want one atomic commit", db.tx.commits)
	}
}

func TestPostgresSessionRepositoryTouchUsesWatermarkBaseline(t *testing.T) {
	changedAt := time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC)
	lastSeen := changedAt.Add(10 * time.Minute)
	now := changedAt.Add(5 * time.Minute)
	db := &fakeSessionDB{
		row: sessionOwnerRow("usr_001"),
		tx: &fakeSessionTx{rows: []pgx.Row{
			sessionPasswordChangedRow(changedAt),
			sessionDetailsRow("usr_001", changedAt, lastSeen, now.Add(time.Hour)),
		}, execTags: []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1")}},
	}
	repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
	}
	got, err := repo.TouchWithUserLock(context.Background(), "sid", func() time.Time { return now }, time.Hour)
	if err != nil {
		t.Fatalf("TouchWithUserLock() error = %v", err)
	}
	if !got.LastSeenAt.Equal(lastSeen) || !got.ExpiresAt.Equal(lastSeen.Add(time.Hour)) {
		t.Fatalf("touched session = %+v, want max stored last_seen baseline", got)
	}
}

func TestPostgresSessionRepositoryTouchDeletesExpiredSessionBeforeReturning(t *testing.T) {
	now := time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC)
	db := &fakeSessionDB{
		row: sessionOwnerRow("usr_001"),
		tx: &fakeSessionTx{rows: []pgx.Row{
			sessionPasswordChangedRow(now.Add(-time.Hour)),
			sessionDetailsRow("usr_001", now.Add(-2*time.Hour), now.Add(-2*time.Hour), now),
		}, execTags: []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1")}},
	}
	repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
	}
	_, err = repo.TouchWithUserLock(context.Background(), "sid", func() time.Time { return now }, time.Hour)
	if !errors.Is(err, auth.ErrSessionExpired) {
		t.Fatalf("TouchWithUserLock() error = %v, want ErrSessionExpired", err)
	}
	if db.tx.commits != 1 {
		t.Fatalf("commits = %d, want deletion committed before sentinel", db.tx.commits)
	}
}

func TestPostgresSessionRepositoryCommitErrorsAreReturned(t *testing.T) {
	now := time.Date(2026, time.June, 23, 10, 0, 0, 0, time.UTC)
	t.Run("create", func(t *testing.T) {
		db := &fakeSessionDB{tx: &fakeSessionTx{
			rows: []pgx.Row{
				sessionUserRow("old-hash", time.Time{}),
				sessionDetailsRow("usr_001", now, now, now.Add(time.Hour)),
			},
			commitErr: errors.New("create commit uncertain"),
		}}
		repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
		}
		_, err = repo.CreateIfPasswordHash(context.Background(), "old-hash", auth.Session{SessionID: "sid", UserID: "usr_001"}, func() time.Time { return now }, time.Hour)
		if err == nil {
			t.Fatal("CreateIfPasswordHash() returned nil for commit error")
		}
	})
	t.Run("change", func(t *testing.T) {
		db := &fakeSessionDB{tx: &fakeSessionTx{
			rows: []pgx.Row{
				sessionUserRow("old-hash", time.Time{}),
				sessionDetailsRow("usr_001", now.Add(-time.Minute), now, now.Add(time.Hour)),
			},
			execTags:  []pgconn.CommandTag{pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("UPDATE 1"), pgconn.NewCommandTag("DELETE 1")},
			commitErr: errors.New("change commit uncertain"),
		}}
		repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
		}
		if err := repo.ChangePasswordIfHash(context.Background(), "usr_001", "sid", "old-hash", "new-hash", func() time.Time { return now }); err == nil {
			t.Fatal("ChangePasswordIfHash() returned nil for commit error")
		}
	})
	t.Run("touch", func(t *testing.T) {
		db := &fakeSessionDB{
			row: sessionOwnerRow("usr_001"),
			tx: &fakeSessionTx{
				rows: []pgx.Row{
					sessionPasswordChangedRow(now),
					sessionDetailsRow("usr_001", now, now, now.Add(time.Hour)),
				},
				commitErr: errors.New("touch commit uncertain"),
			},
		}
		repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
		}
		if _, err := repo.TouchWithUserLock(context.Background(), "sid", func() time.Time { return now }, time.Hour); err == nil {
			t.Fatal("TouchWithUserLock() returned nil for commit error")
		}
	})
	t.Run("expired_deletion", func(t *testing.T) {
		db := &fakeSessionDB{
			row: sessionOwnerRow("usr_001"),
			tx: &fakeSessionTx{
				rows: []pgx.Row{
					sessionPasswordChangedRow(now),
					sessionDetailsRow("usr_001", now.Add(-time.Hour), now.Add(-time.Hour), now),
				},
				execTags:  []pgconn.CommandTag{pgconn.NewCommandTag("DELETE 1")},
				commitErr: errors.New("expired deletion commit uncertain"),
			},
		}
		repo, err := newPostgresSessionRepositoryWithDB(db, []byte("0123456789abcdef0123456789abcdef"))
		if err != nil {
			t.Fatalf("newPostgresSessionRepositoryWithDB() error = %v", err)
		}
		_, err = repo.TouchWithUserLock(context.Background(), "sid", func() time.Time { return now }, time.Hour)
		if err == nil || errors.Is(err, auth.ErrSessionExpired) {
			t.Fatalf("TouchWithUserLock() error = %v, want deletion commit error", err)
		}
	})
}

func TestNewPostgresSessionRepositoryRequiresHMACKey(t *testing.T) {
	if _, err := NewPostgresSessionRepository(nil, nil); err == nil {
		t.Fatal("NewPostgresSessionRepository() error = nil, want non-nil for empty HMAC key")
	}
}
