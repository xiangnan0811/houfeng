package ids

import (
	"regexp"
	"testing"
)

func TestNewSecretTokenUsesThirtyTwoRandomBytes(t *testing.T) {
	t.Parallel()

	token, err := NewSecretToken("sync")
	if err != nil {
		t.Fatalf("NewSecretToken() error = %v", err)
	}

	pattern := regexp.MustCompile(`^sync_[0-9a-f]{64}$`)
	if !pattern.MatchString(token) {
		t.Fatalf("NewSecretToken() = %q, want sync_ plus 64 lowercase hex chars", token)
	}
}

func TestNewSecretTokenRequiresPrefix(t *testing.T) {
	t.Parallel()

	if _, err := NewSecretToken(""); err == nil {
		t.Fatal("NewSecretToken() error = nil, want prefix error")
	}
}
