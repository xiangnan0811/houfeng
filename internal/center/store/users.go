package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/auth"
)

type PostgresUserRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresUserRepository(pool *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{pool: pool}
}

func (r *PostgresUserRepository) Create(ctx context.Context, u auth.User) error {
	_, err := r.pool.Exec(ctx, `
		insert into users (user_id, username, password_hash, display_name, role, created_at, password_changed_at)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		u.UserID, u.Username, u.PasswordHash, u.DisplayName, u.Role, u.CreatedAt, u.PasswordChangedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return auth.ErrUsernameTaken
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *PostgresUserRepository) FindByUsername(ctx context.Context, username string) (auth.User, error) {
	return r.queryOne(ctx, `
		select user_id, username, password_hash, display_name, role, created_at, password_changed_at
		from users where username = $1`, username)
}

func (r *PostgresUserRepository) FindByID(ctx context.Context, userID string) (auth.User, error) {
	return r.queryOne(ctx, `
		select user_id, username, password_hash, display_name, role, created_at, password_changed_at
		from users where user_id = $1`, userID)
}

func (r *PostgresUserRepository) queryOne(ctx context.Context, sql, arg string) (auth.User, error) {
	var u auth.User
	err := r.pool.QueryRow(ctx, sql, arg).Scan(
		&u.UserID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.Role, &u.CreatedAt, &u.PasswordChangedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}
	if err != nil {
		return auth.User{}, fmt.Errorf("query user: %w", err)
	}
	return u, nil
}

func (r *PostgresUserRepository) UpdatePassword(ctx context.Context, userID, newHash string, changedAt time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		update users set password_hash = $2, password_changed_at = $3 where user_id = $1`,
		userID, newHash, changedAt,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return auth.ErrUserNotFound
	}
	return nil
}

func (r *PostgresUserRepository) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `select count(*) from users`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

var _ auth.UserRepository = (*PostgresUserRepository)(nil)
