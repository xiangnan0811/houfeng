package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "" {
		t.Fatal("hash must not be empty")
	}
	if strings.HasPrefix(hash, "correct") {
		t.Fatal("hash must not contain plaintext")
	}

	if err := VerifyPassword(hash, "correct-horse-battery-staple"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
}

func TestHashPasswordWithCostEmbedsConfiguredCost(t *testing.T) {
	hash, err := HashPasswordWithCost("correct-horse-battery-staple", bcrypt.MinCost)
	if err != nil {
		t.Fatalf("HashPasswordWithCost: %v", err)
	}

	got, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if got != bcrypt.MinCost {
		t.Fatalf("bcrypt cost = %d, want %d", got, bcrypt.MinCost)
	}
}

func TestHashPasswordWithCostRejectsInvalidCost(t *testing.T) {
	_, err := HashPasswordWithCost("correct-horse-battery-staple", bcrypt.MinCost-1)
	if !errors.Is(err, ErrPasswordBcryptCostInvalid) {
		t.Fatalf("HashPasswordWithCost invalid cost = %v, want ErrPasswordBcryptCostInvalid", err)
	}
}

func TestVerifyPasswordWrong(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	err = VerifyPassword(hash, "wrong-password-xx")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("VerifyPassword wrong = %v, want ErrInvalidCredentials", err)
	}
}

func TestHashPasswordRejectsShort(t *testing.T) {
	_, err := HashPassword("short")
	if !errors.Is(err, ErrPasswordTooShort) {
		t.Fatalf("HashPassword short = %v, want ErrPasswordTooShort", err)
	}
}

func TestHashPasswordRejectsLong(t *testing.T) {
	_, err := HashPassword(strings.Repeat("a", MaxPasswordLength+1))
	if !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("HashPassword long = %v, want ErrPasswordTooLong", err)
	}
}

func TestHashPasswordRejectsWeakPasswords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
	}{
		{name: "common password", password: "password123"},
		{name: "repeated character", password: "aaaaaaaaaaaa"},
		{name: "single character class", password: "correcthorsebatterystaple"},
		{name: "keyboard sequence", password: "qwertyuiop123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := HashPassword(tt.password)
			if !errors.Is(err, ErrPasswordTooWeak) {
				t.Fatalf("HashPassword(%q) error = %v, want ErrPasswordTooWeak", tt.password, err)
			}
		})
	}
}

func TestHashPasswordAcceptsLongPassphrase(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("correct-horse-battery-staple-2026")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if err := VerifyPassword(hash, "correct-horse-battery-staple-2026"); err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
}

func TestVerifyPasswordRejectsInvalidHash(t *testing.T) {
	err := VerifyPassword("xx", "any-password-x")
	if !errors.Is(err, ErrPasswordHashInvalid) {
		t.Fatalf("VerifyPassword bad hash = %v, want ErrPasswordHashInvalid", err)
	}
}
