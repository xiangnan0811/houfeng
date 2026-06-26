package redact

import (
	"strings"
	"testing"
)

func TestSecretsRedactsCommonSensitiveValues(t *testing.T) {
	input := strings.Join([]string{
		`Authorization: Bearer abc.def.ghi`,
		`token=plain-token-001`,
		`access_token: access-secret`,
		`refresh_token="refresh-secret"`,
		`password='password-secret'`,
		`api_key=api-secret`,
		`{"secret":"json-secret","safe":"kept"}`,
	}, "\n")

	got := Secrets(input)

	for _, leaked := range []string{"abc.def.ghi", "plain-token-001", "access-secret", "refresh-secret", "password-secret", "api-secret", "json-secret"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted output leaked %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, `"safe":"kept"`) {
		t.Fatalf("redacted output lost non-sensitive JSON field: %s", got)
	}
}

func TestSecretsRedactsPrivateKeyBlocks(t *testing.T) {
	input := "before\n-----BEGIN PRIVATE KEY-----\nabc123\n-----END PRIVATE KEY-----\nafter"

	got := Secrets(input)

	if strings.Contains(got, "abc123") || strings.Contains(got, "BEGIN PRIVATE KEY") {
		t.Fatalf("private key was not redacted: %s", got)
	}
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Fatalf("redaction lost surrounding text: %s", got)
	}
}

func TestSecretsPreservesPlainOperationalOutput(t *testing.T) {
	input := "load average: 0.10, 0.20, 0.30\nFilesystem /dev/root 42%"

	if got := Secrets(input); got != input {
		t.Fatalf("Secrets() = %q, want unchanged operational output", got)
	}
}
