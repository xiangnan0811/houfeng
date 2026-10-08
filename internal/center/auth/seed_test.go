package auth

import (
	"context"
	"encoding/hex"
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

func seedFixtureUsername() string {
	return strings.Join([]string{"fixture", "operator"}, "-")
}

func seedFixturePassphrase() string {
	return strings.Join([]string{"fixture", "credential", "2026!"}, "-")
}

func TestSeedInitialUserCreatesWhenEmpty(t *testing.T) {
	users := newFakeUsers()
	err := SeedInitialUser(context.Background(), users, seedFixtureUsername(), seedFixturePassphrase(), "管理员", staticNow())
	if err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	if n, _ := users.CountUsers(context.Background()); n != 1 {
		t.Fatalf("user count = %d, want 1", n)
	}
	u, err := users.FindByUsername(context.Background(), seedFixtureUsername())
	if err != nil {
		t.Fatalf("FindByUsername: %v", err)
	}
	if !u.IsSupervisor || u.Role != RoleAdmin || u.DisabledAt != nil {
		t.Fatalf("seeded user authority = %+v, want active supervisor admin", u)
	}
}

func TestSeedInitialUserUsesConfiguredBcryptCost(t *testing.T) {
	users := newFakeUsers()
	err := SeedInitialUserWithOptions(context.Background(), users, SeedInitialUserOptions{
		Username:           seedFixtureUsername(),
		Password:           seedFixturePassphrase(),
		DisplayName:        "管理员",
		Now:                staticNow(),
		PasswordBcryptCost: bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("SeedInitialUserWithOptions: %v", err)
	}
	u, err := users.FindByUsername(context.Background(), seedFixtureUsername())
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
	id, err := NewUserID()
	if err != nil {
		t.Fatalf("NewUserID: %v", err)
	}
	if !strings.HasPrefix(id, "usr_") {
		t.Fatalf("id = %q, want prefix usr_", id)
	}
	if len(id) != 4+24 {
		t.Fatalf("len(id) = %d, want 28", len(id))
	}
	if _, err := hex.DecodeString(id[len("usr_"):]); err != nil {
		t.Fatalf("id suffix = %q, want lowercase hexadecimal: %v", id[len("usr_"):], err)
	}
}

type seedConflictUsers struct {
	createErr error
	found     User
	findErr   error
}

func (s *seedConflictUsers) Create(context.Context, User) error { return s.createErr }
func (s *seedConflictUsers) FindByUsername(context.Context, string) (User, error) {
	if s.findErr != nil {
		return User{}, s.findErr
	}
	return s.found, nil
}
func (s *seedConflictUsers) FindByID(context.Context, string) (User, error) {
	return User{}, ErrUserNotFound
}
func (s *seedConflictUsers) CountUsers(context.Context) (int, error) { return 0, nil }

func TestSeedInitialUserAcceptsSupervisorConflict(t *testing.T) {
	users := &seedConflictUsers{createErr: ErrInitialUserAlreadyExists}
	if err := SeedInitialUser(context.Background(), users, "admin", "correct-horse-battery", "", staticNow()); err != nil {
		t.Fatalf("SeedInitialUser supervisor conflict = %v, want nil", err)
	}
}

func TestSeedInitialUserAcceptsOnlyActiveSupervisorUsernameConflict(t *testing.T) {
	active := &seedConflictUsers{
		createErr: ErrUsernameTaken,
		found:     User{Role: RoleAdmin, IsSupervisor: true},
	}
	if err := SeedInitialUser(context.Background(), active, "admin", "correct-horse-battery", "", staticNow()); err != nil {
		t.Fatalf("active supervisor username conflict = %v, want nil", err)
	}

	disabledAt := staticNow()()
	for _, existing := range []User{
		{Role: RoleAdmin},
		{Role: RoleAdmin, IsSupervisor: true, DisabledAt: &disabledAt},
		{Role: "viewer", IsSupervisor: true},
	} {
		users := &seedConflictUsers{createErr: ErrUsernameTaken, found: existing}
		if err := SeedInitialUser(context.Background(), users, "admin", "correct-horse-battery", "", staticNow()); !errors.Is(err, ErrUsernameTaken) {
			t.Fatalf("existing role=%q supervisor=%t disabled=%t: err = %v, want ErrUsernameTaken", existing.Role, existing.IsSupervisor, existing.DisabledAt != nil, err)
		}
	}
}
