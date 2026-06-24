package ids

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

func New(prefix string) (string, error) {
	return newWithBytes(prefix, 8)
}

func NewSecretToken(prefix string) (string, error) {
	return newWithBytes(prefix, 32)
}

func newWithBytes(prefix string, randomBytes int) (string, error) {
	if prefix == "" {
		return "", errors.New("prefix is required")
	}

	buf := make([]byte, randomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return prefix + "_" + hex.EncodeToString(buf), nil
}
