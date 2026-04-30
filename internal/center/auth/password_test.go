package auth

import (
	"errors"
	"strings"
	"testing"
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

func TestVerifyPasswordRejectsInvalidHash(t *testing.T) {
	err := VerifyPassword("xx", "any-password-x")
	if !errors.Is(err, ErrPasswordHashInvalid) {
		t.Fatalf("VerifyPassword bad hash = %v, want ErrPasswordHashInvalid", err)
	}
}
