package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/auth"
)

type PostgresSessionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresSessionRepository(pool *pgxpool.Pool) *PostgresSessionRepository {
	return &PostgresSessionRepository{pool: pool}
}

func (r *PostgresSessionRepository) Create(ctx context.Context, s auth.Session) error {
	_, err := r.pool.Exec(ctx, `
		insert into sessions (session_id, user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		s.SessionID, s.UserID, s.IssuedAt, s.LastSeenAt, s.ExpiresAt, s.UserAgent, s.ClientIP,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) Find(ctx context.Context, sessionID string) (auth.Session, error) {
	var s auth.Session
	err := r.pool.QueryRow(ctx, `
		select session_id, user_id, issued_at, last_seen_at, expires_at, user_agent, client_ip
		from sessions where session_id = $1`, sessionID,
	).Scan(&s.SessionID, &s.UserID, &s.IssuedAt, &s.LastSeenAt, &s.ExpiresAt, &s.UserAgent, &s.ClientIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrSessionNotFound
	}
	if err != nil {
		return auth.Session{}, fmt.Errorf("query session: %w", err)
	}
	return s, nil
}

func (r *PostgresSessionRepository) RefreshExpires(ctx context.Context, sessionID string, lastSeenAt, expiresAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		update sessions set last_seen_at = $2, expires_at = $3 where session_id = $1`,
		sessionID, lastSeenAt, expiresAt,
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
	_, err := r.pool.Exec(ctx, `delete from sessions where session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *PostgresSessionRepository) DeleteExpiredBefore(ctx context.Context, cutoff time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `delete from sessions where expires_at < $1`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

var _ auth.SessionRepository = (*PostgresSessionRepository)(nil)
