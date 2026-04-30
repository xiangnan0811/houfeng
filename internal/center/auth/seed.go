package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// SeedInitialUser creates the first admin user when the users table is empty.
// On non-empty repositories it is a no-op. Validates username/password against
// the package-level limits before any DB write.
func SeedInitialUser(ctx context.Context, users UserRepository, username, password, displayName string, now func() time.Time) error {
	username = strings.TrimSpace(username)
	if len(username) < MinUsernameLength || len(username) > MaxUsernameLength {
		return ErrUsernameInvalid
	}
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	count, err := users.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	id, err := newUserID()
	if err != nil {
		return err
	}
	if now == nil {
		now = time.Now
	}
	t := now().UTC()
	if displayName == "" {
		displayName = username
	}
	return users.Create(ctx, User{
		UserID:            id,
		Username:          username,
		PasswordHash:      hash,
		DisplayName:       displayName,
		Role:              RoleAdmin,
		CreatedAt:         t,
		PasswordChangedAt: t,
	})
}

func newUserID() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("read random: %w", err)
	}
	return "usr_" + hex.EncodeToString(buf[:]), nil
}
