package auth

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const DefaultPasswordBcryptCost = bcrypt.DefaultCost

func HashPassword(plain string) (string, error) {
	return HashPasswordWithCost(plain, DefaultPasswordBcryptCost)
}

func HashPasswordWithCost(plain string, cost int) (string, error) {
	if len(plain) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}
	if len(plain) > MaxPasswordLength {
		return "", ErrPasswordTooLong
	}
	if !isAcceptablePassword(plain) {
		return "", ErrPasswordTooWeak
	}
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return "", ErrPasswordBcryptCostInvalid
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", fmt.Errorf("bcrypt generate: %w", err)
	}
	return string(hashed), nil
}

func isAcceptablePassword(plain string) bool {
	normalized := strings.ToLower(strings.TrimSpace(plain))
	for _, weak := range []string{
		"password",
		"qwerty",
		"admin",
		"letmein",
		"welcome",
		"123456",
	} {
		if strings.Contains(normalized, weak) {
			return false
		}
	}

	var (
		classes int
		lower   bool
		upper   bool
		digit   bool
		symbol  bool
		first   rune
		same    = true
		seen    bool
	)
	for _, r := range plain {
		if !seen {
			first = r
			seen = true
		} else if r != first {
			same = false
		}
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			symbol = true
		}
	}
	if same {
		return false
	}
	for _, present := range []bool{lower, upper, digit, symbol} {
		if present {
			classes++
		}
	}
	return classes >= 2
}

func VerifyPassword(hashed, plain string) error {
	if len(hashed) < 4 {
		return ErrPasswordHashInvalid
	}
	err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(plain))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return ErrInvalidCredentials
	}
	if errors.Is(err, bcrypt.ErrHashTooShort) {
		return ErrPasswordHashInvalid
	}
	if err != nil {
		return fmt.Errorf("bcrypt compare: %w", err)
	}
	return nil
}
