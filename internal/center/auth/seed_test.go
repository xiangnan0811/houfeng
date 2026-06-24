package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func staticNow() func() time.Time {
	t := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func TestSeedInitialUserCreatesWhenEmpty(t *testing.T) {
	users := newFakeUsers()
	err := SeedInitialUser(context.Background(), users, "admin", "correct-horse-battery", "管理员", staticNow())
	if err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	if n, _ := users.CountUsers(context.Background()); n != 1 {
		t.Fatalf("user count = %d, want 1", n)
	}
}

func TestSeedInitialUserUsesConfiguredBcryptCost(t *testing.T) {
	users := newFakeUsers()
	err := SeedInitialUserWithOptions(context.Background(), users, SeedInitialUserOptions{
		Username:           "admin",
		Password:           "correct-horse-battery",
		DisplayName:        "管理员",
		Now:                staticNow(),
		PasswordBcryptCost: bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("SeedInitialUserWithOptions: %v", err)
	}
	u, err := users.FindByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("FindByUsername: %v", err)
	}
	got, err := bcrypt.Cost([]byte(u.PasswordHash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if got != bcrypt.MinCost {
		t.Fatalf("seeded password bcrypt cost = %d, want %d", got, bcrypt.MinCost)
	}
}

func TestSeedInitialUserDefaultsDisplayName(t *testing.T) {
	users := newFakeUsers()
	err := SeedInitialUser(context.Background(), users, "admin", "correct-horse-battery", "", staticNow())
	if err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	u, err := users.FindByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("FindByUsername: %v", err)
	}
	if u.DisplayName != "admin" {
		t.Fatalf("DisplayName = %q, want admin", u.DisplayName)
	}
}

func TestSeedInitialUserSkipWhenNonEmpty(t *testing.T) {
	users := newFakeUsers()
	_ = users.Create(context.Background(), User{
		UserID: "existing", Username: "someone", PasswordHash: "x", Role: RoleAdmin,
	})

	err := SeedInitialUser(context.Background(), users, "admin", "correct-horse-battery", "管理员", staticNow())
	if err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	if n, _ := users.CountUsers(context.Background()); n != 1 {
		t.Fatalf("user count = %d, want 1 (skip)", n)
	}
}

func TestSeedInitialUserDoesNotOverwriteExistingPassword(t *testing.T) {
	users := newFakeUsers()
	existingHash, err := HashPassword("existing-credential-2026!")
	if err != nil {
		t.Fatalf("HashPassword existing: %v", err)
	}
	_ = users.Create(context.Background(), User{
		UserID:       "existing",
		Username:     "admin",
		PasswordHash: existingHash,
		Role:         RoleAdmin,
	})

	err = SeedInitialUser(context.Background(), users, "admin", "new-credential-2026!", "管理员", staticNow())
	if err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	u, err := users.FindByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("FindByUsername: %v", err)
	}
	if u.PasswordHash != existingHash {
		t.Fatal("SeedInitialUser changed an existing password hash")
	}
	if err := VerifyPassword(u.PasswordHash, "existing-credential-2026!"); err != nil {
		t.Fatalf("existing password no longer verifies: %v", err)
	}
	if err := VerifyPassword(u.PasswordHash, "new-credential-2026!"); err == nil {
		t.Fatal("seed password unexpectedly replaced existing password")
	}
}

func TestSeedInitialUserRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name, user, pass string
		wantErr          error
	}{
		{"empty username", "", "correct-horse-battery", ErrUsernameInvalid},
		{"short password", "admin", "abc", ErrPasswordTooShort},
		{"long password", "admin", strings.Repeat("a", MaxPasswordLength+1), ErrPasswordTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			users := newFakeUsers()
			err := SeedInitialUser(context.Background(), users, tc.user, tc.pass, "管理员", staticNow())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if n, _ := users.CountUsers(context.Background()); n != 0 {
				t.Fatalf("count = %d, want 0", n)
			}
		})
	}
}

func TestNewUserIDFormat(t *testing.T) {
	id, err := newUserID()
	if err != nil {
		t.Fatalf("newUserID: %v", err)
	}
	if !strings.HasPrefix(id, "usr_") {
		t.Fatalf("id = %q, want prefix usr_", id)
	}
	if len(id) != 4+24 {
		t.Fatalf("len(id) = %d, want 28", len(id))
	}
}
