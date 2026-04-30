package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const bcryptCost = bcrypt.DefaultCost

func HashPassword(plain string) (string, error) {
	if len(plain) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}
	if len(plain) > MaxPasswordLength {
		return "", ErrPasswordTooLong
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt generate: %w", err)
	}
	return string(hashed), nil
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
