package ipquality

import (
	"encoding/json"
	"strings"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestSanitizeRawJSONRedactsSensitiveFields(t *testing.T) {
	raw := json.RawMessage(`{
		"Info":{"ASN":"AS64500"},
		"token":"top-secret",
		"client_secret":"synthetic-root",
		"nested":{"api_key":"nested-secret","Authorization":"Bearer hidden"," APP_SECRET ":"synthetic-nested"},
		"items":[{"access-token":"service-secret","Signing-Secret":"synthetic-array","PRIVATE_SECRET":"synthetic-uppercase"}],
		"ordinary":"kept","secretary":"retained","secret_count":2
	}`)

	for name, sanitize := range map[string]func(json.RawMessage) json.RawMessage{
		"raw":         SanitizeRawJSON,
		"extra":       SanitizeExtraJSON,
		"diagnostics": SanitizeCollectionDiagnostics,
	} {
		t.Run(name, func(t *testing.T) {
			got := sanitize(raw)
			body := string(got)
			for _, secret := range []string{"top-secret", "nested-secret", "Bearer hidden", "service-secret", "synthetic-root", "synthetic-nested", "synthetic-array", "synthetic-uppercase"} {
				if strings.Contains(body, secret) {
					t.Fatalf("sanitizer leaked %q in %s", secret, body)
				}
			}
			for _, field := range []string{
				`"token":"[redacted]"`, `"api_key":"[redacted]"`, `"Authorization":"[redacted]"`, `"access-token":"[redacted]"`,
				`"client_secret":"[redacted]"`, `" APP_SECRET ":"[redacted]"`, `"Signing-Secret":"[redacted]"`, `"PRIVATE_SECRET":"[redacted]"`,
				`"ordinary":"kept"`, `"secretary":"retained"`, `"secret_count":2`,
			} {
				if !strings.Contains(body, field) {
					t.Fatalf("sanitized JSON = %s, want field %s", body, field)
				}
			}
			if !json.Valid(got) {
				t.Fatalf("sanitizer returned invalid JSON: %s", got)
			}
		})
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

func TestSanitizeExtraJSONUsesExtraTruncationMarker(t *testing.T) {
	raw := json.RawMessage(`{"payload":"` + strings.Repeat("x", MaxRawJSONBytes+1) + `","token":"secret"}`)

	got := SanitizeExtraJSON(raw)
	if len(got) > MaxRawJSONBytes {
		t.Fatalf("len(SanitizeExtraJSON) = %d, want <= %d", len(got), MaxRawJSONBytes)
	}
	if !json.Valid(got) {
		t.Fatalf("SanitizeExtraJSON returned invalid JSON: %s", got)
	}
	if !strings.Contains(string(got), `"reason":"extra_json_size_limit"`) {
		t.Fatalf("SanitizeExtraJSON = %s, want extra JSON truncation marker", got)
	}
	if strings.Contains(string(got), "secret") {
		t.Fatalf("SanitizeExtraJSON leaked secret: %s", got)
	}
}

func TestSanitizeCollectionDiagnosticsSecretExpansionPreservesPairedBoundary(t *testing.T) {
	for _, revision := range []string{"5", "5.0", "6"} {
		t.Run(revision, func(t *testing.T) {
			raw := json.RawMessage(`{"collection_revision":` + revision + `,"collection_id":"synthetic","expected_ip_versions":[4],"ip_address_family":4,"family_availability":"available","collect_request_id":"ipqc_synthetic","items":[` +
				strings.Repeat(`{"client_secret":""},`, 6000) + `{}]}`)
			if len(raw) > MaxRawJSONBytes || !json.Valid(raw) {
				t.Fatalf("input must be valid and below the size limit: %d bytes", len(raw))
			}
			got := SanitizeCollectionDiagnostics(raw)
			if len(got) > MaxRawJSONBytes || !json.Valid(got) {
				t.Fatalf("sanitized diagnostics must remain bounded JSON: %s", got)
			}
			if !strings.Contains(string(got), `"reason":"extra_json_size_limit"`) {
				t.Fatalf("secret redaction must trigger the existing truncation marker: %s", got)
			}
			metadata := agentapi.ParseCollectionMetadata(got)
			if !metadata.Paired || metadata.Valid {
				t.Fatalf("truncation upgraded or downgraded the paired boundary: %#v; JSON=%s", metadata, got)
			}
		})
	}
}
