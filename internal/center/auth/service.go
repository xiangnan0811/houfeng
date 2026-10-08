package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Options struct {
	SessionTTL         time.Duration
	Now                func() time.Time
	PasswordBcryptCost int
}

type Service struct {
	users              UserRepository
	sessions           SessionRepository
	now                func() time.Time
	ttl                time.Duration
	passwordBcryptCost int
}

func New(users UserRepository, sessions SessionRepository, opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	ttl := opts.SessionTTL
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	passwordBcryptCost := opts.PasswordBcryptCost
	if passwordBcryptCost == 0 {
		passwordBcryptCost = DefaultPasswordBcryptCost
	}
	return &Service{users: users, sessions: sessions, now: now, ttl: ttl, passwordBcryptCost: passwordBcryptCost}
}

func (s *Service) Login(ctx context.Context, username, password, userAgent, clientIP string) (Session, error) {
	username = strings.TrimSpace(username)
	if len(username) < MinUsernameLength || len(username) > MaxUsernameLength {
		return Session{}, ErrInvalidCredentials
	}
	u, err := s.users.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Equalize timing/work to avoid leaking user existence.
			_ = VerifyPassword("$2y$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinvalid", password)
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, fmt.Errorf("find user by username: %w", err)
	}
	if err := VerifyPassword(u.PasswordHash, password); err != nil {
		return Session{}, ErrInvalidCredentials
	}
	if u.DisabledAt != nil {
		return Session{}, ErrInvalidCredentials
	}

	id, err := NewSessionID()
	if err != nil {
		return Session{}, fmt.Errorf("new session id: %w", err)
	}
	sess := Session{
		SessionID: id,
		UserID:    u.UserID,
		UserAgent: userAgent,
		ClientIP:  clientIP,
	}
	persisted, err := s.sessions.CreateIfPasswordHash(ctx, u.PasswordHash, sess, s.now, s.ttl)
	if err != nil {
		return Session{}, err
	}
	return persisted, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	return s.sessions.Delete(ctx, sessionID)
}

// Touch validates a session, extends its expiry, and returns the refreshed Session.
func (s *Service) Touch(ctx context.Context, sessionID string) (Session, error) {
	return s.sessions.TouchWithUserLock(ctx, sessionID, s.now, s.ttl)
}

// ValidateSession checks the current persisted session authority without
// touching its activity timestamps or extending its expiry.
func (s *Service) ValidateSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return ErrSessionNotFound
	}
	return s.sessions.ValidateSession(ctx, sessionID, s.now)
}

func (s *Service) UserBySession(ctx context.Context, sessionID string) (User, error) {
	sess, err := s.Touch(ctx, sessionID)
	if err != nil {
		return User{}, err
	}
	u, err := s.users.FindByID(ctx, sess.UserID)
	if err != nil {
		return User{}, err
	}
	if u.DisabledAt != nil {
		return User{}, ErrSessionExpired
	}
	return u, nil
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentSessionID, oldPassword, newPassword string) error {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := VerifyPassword(u.PasswordHash, oldPassword); err != nil {
		return ErrInvalidCredentials
	}
	if u.DisabledAt != nil {
		return ErrSessionExpired
	}
	hash, err := HashPasswordWithCost(newPassword, s.passwordBcryptCost)
	if err != nil {
		return err
	}
	return s.sessions.ChangePasswordIfHash(ctx, userID, currentSessionID, u.PasswordHash, hash, s.now)
}
