package ipquality

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeRawJSONRedactsSensitiveFields(t *testing.T) {
	raw := json.RawMessage(`{
		"Info":{"ASN":"AS64500"},
		"token":"top-secret",
		"nested":{"api_key":"nested-secret","Authorization":"Bearer hidden"},
		"items":[{"access-token":"service-secret"}]
	}`)

	got := SanitizeRawJSON(raw)
	body := string(got)
	for _, secret := range []string{"top-secret", "nested-secret", "Bearer hidden", "service-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("SanitizeRawJSON leaked %q in %s", secret, body)
		}
	}
	for _, field := range []string{`"token":"[redacted]"`, `"api_key":"[redacted]"`, `"Authorization":"[redacted]"`, `"access-token":"[redacted]"`} {
		if !strings.Contains(body, field) {
			t.Fatalf("SanitizeRawJSON = %s, want redacted field %s", body, field)
		}
	}
	if !json.Valid(got) {
		t.Fatalf("SanitizeRawJSON returned invalid JSON: %s", got)
	}
}

func TestSanitizeRawJSONDropsOversizedPayload(t *testing.T) {
	raw := json.RawMessage(`{"payload":"` + strings.Repeat("x", MaxRawJSONBytes+1) + `"}`)

	got := SanitizeRawJSON(raw)
	if len(got) > MaxRawJSONBytes {
		t.Fatalf("len(SanitizeRawJSON) = %d, want <= %d", len(got), MaxRawJSONBytes)
	}
	if !json.Valid(got) {
		t.Fatalf("SanitizeRawJSON returned invalid JSON: %s", got)
	}
	if !strings.Contains(string(got), `"truncated":true`) {
		t.Fatalf("SanitizeRawJSON = %s, want truncation marker", got)
	}
}
