package createidempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

const (
	MinKeyLength = 8
	MaxKeyLength = 128
)

var (
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")
	ErrIdempotencyKeyReused  = errors.New("idempotency key reused")

	validKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
)

// NormalizeKey trims transport whitespace and validates the stable key format.
// Rejected values are intentionally omitted from errors so keys cannot leak.
func NormalizeKey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) < MinKeyLength || len(value) > MaxKeyLength || !validKeyPattern.MatchString(value) {
		return "", ErrInvalidIdempotencyKey
	}
	return value, nil
}

// DigestNormalizedRequest returns a lowercase SHA-256 digest of the canonical
// JSON representation supplied by the caller after domain normalization.
func DigestNormalizedRequest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", errors.New("encode normalized request for idempotency digest")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// NamespacedLockKey separates advisory locks for otherwise identical keys used
// by different create operations.
func NamespacedLockKey(operation, normalizedKey string) string {
	return operation + ":" + normalizedKey
}
