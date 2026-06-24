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
	hmacKey []byte
}

const minSessionHMACKeyBytes = 32

type sessionDB interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func NewPostgresSessionRepository(pool *pgxpool.Pool, hmacKey []byte) (*PostgresSessionRepository, error) {
	return newPostgresSessionRepositoryWithDB(pool, hmacKey)
}

func newPostgresSessionRepositoryWithDB(db sessionDB, hmacKey []byte) (*PostgresSessionRepository, error) {
	if len(hmacKey) < minSessionHMACKeyBytes {
		return nil, fmt.Errorf("session HMAC key must be at least %d bytes", minSessionHMACKeyBytes)
	}
	return &PostgresSessionRepository{db: db, hmacKey: append([]byte(nil), hmacKey...)}, nil
}

func (r *PostgresSessionRepository) hashSessionID(sessionID string) string {
	mac := hmac.New(sha256.New, r.hmacKey)
	_, _ = mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

func (r *PostgresSessionRepository) Create(ctx context.Context, s auth.Session) error {
	_, err := r.db.Exec(ctx, `
		insert into sessions (session_id_hash, user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		r.hashSessionID(s.SessionID), s.UserID, s.IssuedAt, s.LastSeenAt, s.ExpiresAt, s.UserAgent, s.ClientIP,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) Find(ctx context.Context, sessionID string) (auth.Session, error) {
	var s auth.Session
	err := r.db.QueryRow(ctx, `
		select user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip
		from sessions where session_id_hash = $1`, r.hashSessionID(sessionID),
	).Scan(&s.UserID, &s.IssuedAt, &s.LastSeenAt, &s.ExpiresAt, &s.UserAgent, &s.ClientIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	if err != nil {
		return auth.Session{}, fmt.Errorf("query session: %w", err)
	}
	s.SessionID = sessionID
	return s, nil
}

func (r *PostgresSessionRepository) RefreshExpires(ctx context.Context, sessionID string, lastSeenAt, expiresAt time.Time) error {
	tag, err := r.db.Exec(ctx, `
		update sessions set last_seen_at = $2, expires_at = $3 where session_id_hash = $1`,
		r.hashSessionID(sessionID), lastSeenAt, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("refresh session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrSessionNotFound
	}
	return nil
}

func (r *PostgresSessionRepository) Delete(ctx context.Context, sessionID string) error {
	_, err := r.db.Exec(ctx, `delete from sessions where session_id_hash = $1`, r.hashSessionID(sessionID))
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) DeleteByUserID(ctx context.Context, userID, exceptSessionID string) error {
	_, err := r.db.Exec(ctx, `delete from sessions where user_id = $1 and session_id_hash <> $2`, userID, r.hashSessionID(exceptSessionID))
	if err != nil {
		return fmt.Errorf("delete sessions by user: %w", err)
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
