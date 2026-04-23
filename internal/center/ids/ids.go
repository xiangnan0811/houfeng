package ids

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
)

func New(prefix string) (string, error) {
	if prefix == "" {
		return "", errors.New("prefix is required")
	}

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	return prefix + "_" + hex.EncodeToString(buf), nil
}
