package store

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/auth"
)

type PostgresSessionRepository struct {
	db      sessionDB
	beginTx func(context.Context, pgx.TxOptions) (sessionTx, error)
	hmacKey []byte
}

const minSessionHMACKeyBytes = 32

type sessionDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type sessionTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type sessionTxBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (sessionTx, error)
}

type sessionPGXTxBeginner interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func NewPostgresSessionRepository(pool *pgxpool.Pool, hmacKey []byte) (*PostgresSessionRepository, error) {
	return newPostgresSessionRepositoryWithDBAndBeginTx(pool, hmacKey, func(ctx context.Context, opts pgx.TxOptions) (sessionTx, error) {
		return pool.BeginTx(ctx, opts)
	})
}

func newPostgresSessionRepositoryWithDB(db sessionDB, hmacKey []byte) (*PostgresSessionRepository, error) {
	var beginTx func(context.Context, pgx.TxOptions) (sessionTx, error)
	if beginner, ok := db.(sessionTxBeginner); ok {
		beginTx = beginner.BeginTx
	} else if beginner, ok := db.(sessionPGXTxBeginner); ok {
		beginTx = func(ctx context.Context, opts pgx.TxOptions) (sessionTx, error) {
			return beginner.BeginTx(ctx, opts)
		}
	}
	return newPostgresSessionRepositoryWithDBAndBeginTx(db, hmacKey, beginTx)
}

func newPostgresSessionRepositoryWithDBAndBeginTx(db sessionDB, hmacKey []byte, beginTx func(context.Context, pgx.TxOptions) (sessionTx, error)) (*PostgresSessionRepository, error) {
	if len(hmacKey) < minSessionHMACKeyBytes {
		return nil, fmt.Errorf("session HMAC key must be at least %d bytes", minSessionHMACKeyBytes)
	}
	return &PostgresSessionRepository{
		db:      db,
		hmacKey: append([]byte(nil), hmacKey...),
		beginTx: beginTx,
	}, nil
}

func (r *PostgresSessionRepository) transaction(ctx context.Context, opts pgx.TxOptions) (sessionTx, error) {
	if r.beginTx == nil {
		return nil, errors.New("session transaction not supported")
	}
	return r.beginTx(ctx, opts)
}

func (r *PostgresSessionRepository) hashSessionID(sessionID string) string {
	mac := hmac.New(sha256.New, r.hmacKey)
	_, _ = mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeSessionClock(now func() time.Time) time.Time {
	return now().UTC().Truncate(time.Microsecond)
}

func laterSessionTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}

func (r *PostgresSessionRepository) CreateIfPasswordHash(ctx context.Context, expectedHash string, session auth.Session, now func() time.Time, ttl time.Duration) (auth.Session, error) {
	tx, err := r.transaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return auth.Session{}, fmt.Errorf("begin session creation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentHash string
	var passwordChangedAt time.Time
	err = tx.QueryRow(ctx, `
		select password_hash, password_changed_at
		from users
		where user_id = $1
		for update`, session.UserID).Scan(&currentHash, &passwordChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrInvalidCredentials
	}
	if err != nil {
		return auth.Session{}, fmt.Errorf("lock user for session creation: %w", err)
	}
	if !hmac.Equal([]byte(currentHash), []byte(expectedHash)) {
		return auth.Session{}, auth.ErrInvalidCredentials
	}

	issuedAt := laterSessionTime(normalizeSessionClock(now), passwordChangedAt.UTC())
	session.IssuedAt = issuedAt
	session.LastSeenAt = issuedAt
	session.ExpiresAt = issuedAt.Add(ttl)
	if err := tx.QueryRow(ctx, `
		insert into sessions (session_id_hash, user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip)
		values ($1, $2, $3, $4, $5, $6, $7)
		returning user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip`,
		r.hashSessionID(session.SessionID), session.UserID, session.IssuedAt, session.LastSeenAt, session.ExpiresAt, session.UserAgent, session.ClientIP,
	).Scan(&session.UserID, &session.IssuedAt, &session.LastSeenAt, &session.ExpiresAt, &session.UserAgent, &session.ClientIP); err != nil {
		return auth.Session{}, fmt.Errorf("insert session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.Session{}, fmt.Errorf("commit session creation: %w", err)
	}
	return session, nil
}

func (r *PostgresSessionRepository) ChangePasswordIfHash(ctx context.Context, userID, currentSessionID, expectedHash, newHash string, now func() time.Time) error {
	tx, err := r.transaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin password change transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentHash string
	var passwordChangedAt time.Time
	err = tx.QueryRow(ctx, `
		select password_hash, password_changed_at
		from users
		where user_id = $1
		for update`, userID).Scan(&currentHash, &passwordChangedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("lock user for password change: %w", err)
	}
	if !hmac.Equal([]byte(currentHash), []byte(expectedHash)) {
		return auth.ErrInvalidCredentials
	}

	var current auth.Session
	hashedSessionID := r.hashSessionID(currentSessionID)
	err = tx.QueryRow(ctx, `
		select user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip
		from sessions
		where session_id_hash = $1 and user_id = $2
		for update`, hashedSessionID, userID).Scan(
		&current.UserID, &current.IssuedAt, &current.LastSeenAt, &current.ExpiresAt, &current.UserAgent, &current.ClientIP,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("lock current session for password change: %w", err)
	}
	if current.UserID != userID {
		return auth.ErrSessionNotFound
	}

	checkedAt := normalizeSessionClock(now)
	if !current.ExpiresAt.After(checkedAt) || (!current.IssuedAt.IsZero() && current.IssuedAt.Before(passwordChangedAt)) {
		return auth.ErrSessionExpired
	}
	changedAt := laterSessionTime(checkedAt, passwordChangedAt.UTC())

	tag, err := tx.Exec(ctx, `
		update users
		set password_hash = $2, password_changed_at = $3
		where user_id = $1`, userID, newHash, changedAt)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}

	tag, err = tx.Exec(ctx, `
		update sessions
		set issued_at = $2
		where session_id_hash = $1 and user_id = $3`, hashedSessionID, changedAt, userID)
	if err != nil {
		return fmt.Errorf("update current session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrSessionNotFound
	}

	if _, err := tx.Exec(ctx, `
		delete from sessions
		where user_id = $1 and session_id_hash <> $2`, userID, hashedSessionID); err != nil {
		return fmt.Errorf("delete other sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit password change: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) TouchWithUserLock(ctx context.Context, sessionID string, now func() time.Time, ttl time.Duration) (auth.Session, error) {
	hashedSessionID := r.hashSessionID(sessionID)
	var userID string
	if err := r.db.QueryRow(ctx, `
		select user_id
		from sessions
		where session_id_hash = $1`, hashedSessionID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.Session{}, auth.ErrSessionNotFound
		}
		return auth.Session{}, fmt.Errorf("find session owner: %w", err)
	}

	tx, err := r.transaction(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return auth.Session{}, fmt.Errorf("begin session touch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var passwordChangedAt time.Time
	if err := tx.QueryRow(ctx, `
		select password_changed_at
		from users
		where user_id = $1
		for update`, userID).Scan(&passwordChangedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.Session{}, auth.ErrUserNotFound
		}
		return auth.Session{}, fmt.Errorf("lock session owner: %w", err)
	}

	var session auth.Session
	if err := tx.QueryRow(ctx, `
		select user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip
		from sessions
		where session_id_hash = $1
		for update`, hashedSessionID).Scan(
		&session.UserID, &session.IssuedAt, &session.LastSeenAt, &session.ExpiresAt, &session.UserAgent, &session.ClientIP,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.Session{}, auth.ErrSessionNotFound
		}
		return auth.Session{}, fmt.Errorf("lock session: %w", err)
	}
	session.SessionID = sessionID
	if session.UserID != userID {
		return auth.Session{}, auth.ErrSessionNotFound
	}

	checkedAt := normalizeSessionClock(now)
	if !session.ExpiresAt.After(checkedAt) || (!session.IssuedAt.IsZero() && session.IssuedAt.Before(passwordChangedAt)) {
		if _, err := tx.Exec(ctx, `delete from sessions where session_id_hash = $1 and user_id = $2`, hashedSessionID, userID); err != nil {
			return auth.Session{}, fmt.Errorf("delete expired session: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return auth.Session{}, fmt.Errorf("commit expired session deletion: %w", err)
		}
		return auth.Session{}, auth.ErrSessionExpired
	}

	baseline := laterSessionTime(checkedAt, session.LastSeenAt)
	baseline = laterSessionTime(baseline, passwordChangedAt.UTC())
	session.LastSeenAt = baseline
	session.ExpiresAt = baseline.Add(ttl)
	tag, err := tx.Exec(ctx, `
		update sessions
		set last_seen_at = $2, expires_at = $3
		where session_id_hash = $1 and user_id = $4`, hashedSessionID, session.LastSeenAt, session.ExpiresAt, userID)
	if err != nil {
		return auth.Session{}, fmt.Errorf("refresh session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.Session{}, fmt.Errorf("commit session touch: %w", err)
	}
	return session, nil
}

func (r *PostgresSessionRepository) Delete(ctx context.Context, sessionID string) error {
	_, err := r.db.Exec(ctx, `delete from sessions where session_id_hash = $1`, r.hashSessionID(sessionID))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) DeleteExpiredBefore(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := r.db.Exec(ctx, `delete from sessions where expires_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

var _ auth.SessionRepository = (*PostgresSessionRepository)(nil)
