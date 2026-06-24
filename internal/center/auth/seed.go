package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type SeedInitialUserOptions struct {
	Username           string
	Password           string
	DisplayName        string
	Now                func() time.Time
	PasswordBcryptCost int
}

// SeedInitialUser creates the first admin user when the users table is empty.
// On non-empty repositories it is a no-op. Validates username/password against
// the package-level limits before any DB write.
func SeedInitialUser(ctx context.Context, users UserRepository, username, password, displayName string, now func() time.Time) error {
	return SeedInitialUserWithOptions(ctx, users, SeedInitialUserOptions{
		Username:    username,
		Password:    password,
		DisplayName: displayName,
		Now:         now,
	})
}

// SeedInitialUserWithOptions is the configured seed path used by the center
// bootstrap so password hashes use the process bcrypt cost policy.
func SeedInitialUserWithOptions(ctx context.Context, users UserRepository, opts SeedInitialUserOptions) error {
	username := strings.TrimSpace(opts.Username)
	if len(username) < MinUsernameLength || len(username) > MaxUsernameLength {
		return ErrUsernameInvalid
	}
	password := opts.Password
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
	passwordBcryptCost := opts.PasswordBcryptCost
	if passwordBcryptCost == 0 {
		passwordBcryptCost = DefaultPasswordBcryptCost
	}
	hash, err := HashPasswordWithCost(password, passwordBcryptCost)
	if err != nil {
		return err
	}
	id, err := newUserID()
	if err != nil {
		return err
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	t := now().UTC()
	displayName := opts.DisplayName
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
