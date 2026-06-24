package auth

import (
	"context"
	"errors"
	"time"
)

const (
	RoleAdmin = "admin"

	SessionCookieName = "__Host-houfeng_session"

	DefaultSessionTTL = 7 * 24 * time.Hour
	MinPasswordLength = 8
	MaxPasswordLength = 256
	MinUsernameLength = 1
	MaxUsernameLength = 64
)

var (
	ErrUserNotFound              = errors.New("user not found")
	ErrUsernameTaken             = errors.New("username already taken")
	ErrInvalidCredentials        = errors.New("invalid username or password")
	ErrSessionNotFound           = errors.New("session not found")
	ErrSessionExpired            = errors.New("session expired")
	ErrPasswordTooShort          = errors.New("password too short")
	ErrPasswordTooLong           = errors.New("password too long")
	ErrPasswordTooWeak           = errors.New("password too weak")
	ErrUsernameInvalid           = errors.New("username invalid")
	ErrPasswordHashInvalid       = errors.New("password hash invalid")
	ErrPasswordBcryptCostInvalid = errors.New("password bcrypt cost invalid")
)

type User struct {
	UserID            string
	Username          string
	PasswordHash      string
	DisplayName       string
	Role              string
	CreatedAt         time.Time
	PasswordChangedAt time.Time
}

type Session struct {
	SessionID  string
	UserID     string
	IssuedAt   time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	ClientIP   string
}

type UserRepository interface {
	Create(ctx context.Context, u User) error
	FindByUsername(ctx context.Context, username string) (User, error)
	FindByID(ctx context.Context, userID string) (User, error)
	UpdatePassword(ctx context.Context, userID, newHash string, changedAt time.Time) error
	CountUsers(ctx context.Context) (int, error)
}

type SessionRepository interface {
	Create(ctx context.Context, s Session) error
	Find(ctx context.Context, sessionID string) (Session, error)
	RefreshExpires(ctx context.Context, sessionID string, lastSeenAt, expiresAt time.Time) error
	Delete(ctx context.Context, sessionID string) error
	DeleteByUserID(ctx context.Context, userID, exceptSessionID string) error
	DeleteExpiredBefore(ctx context.Context, cutoff time.Time) (int, error)
}
