package evidence

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRedactionForbiddenFieldCorpus(t *testing.T) {
	descriptor := testDescriptor(t, CommandAuditV1Key())
	forbidden := []string{
		"token", "password", "passwd", "api_key", "access_key", "private_key",
		"authorization", "cookie", "env", "environment", "mounts", "container_id",
		"fingerprint", "raw_json", "diagnostics_json", "stdout", "stderr", "output", "details",
	}
	for _, field := range forbidden {
		t.Run(field, func(t *testing.T) {
			payload := map[string]any{"command_id": "cmd_1", field: "must-not-survive"}
			if _, _, err := CanonicalizePayload(descriptor, payload, RedactionIncludeSensitiveTopology); !errors.Is(err, ErrForbiddenField) {
				t.Fatalf("CanonicalizePayload(%q) error = %v, want ErrForbiddenField", field, err)
			}
		})
	}
}

func TestRedactionForbiddenFieldCorpusNestedAndCamelCase(t *testing.T) {
	descriptor := testDescriptor(t, CommandAuditV1Key())
	for _, payload := range []map[string]any{
		{"command_id": "cmd_1", "apiKey": "secret"},
		{"command_id": "cmd_1", "rawJSON": "{}"},
		{"command_id": "cmd_1", "rawJson": "{}"},
		{"command_id": "cmd_1", "containerID": "deadbeef"},
		{"command_id": "cmd_1", "containerId": "deadbeef"},
		{"command_id": "cmd_1", "stdout_preview": "first line"},
		{"command_id": "cmd_1", "stdoutPreview": "first line"},
		{"command_id": "cmd_1", "request_headers": map[string]any{"accept": "application/json"}},
		{"command_id": "cmd_1", "container": map[string]any{"id": "deadbeef"}},
		{"command_id": "cmd_1", "raw": map[string]any{"json": "{}"}},
		{"command_id": "cmd_1", "mount": map[string]any{"path": "/root"}},
		{"command_id": "cmd_1", "query": "token=secret"},
	} {
		if _, _, err := CanonicalizePayload(descriptor, payload, RedactionIncludeSensitiveTopology); !errors.Is(err, ErrForbiddenField) {
			t.Fatalf("CanonicalizePayload(%#v) error = %v, want ErrForbiddenField", payload, err)
		}
	}
}

func TestRedactionRejectsCompoundForbiddenFieldNames(t *testing.T) {
	descriptor := testDescriptor(t, CommandAuditV1Key())
	for _, field := range []string{
		"command_output", "commandOutput",
		"output_preview", "outputPreview",
		"command_details", "commandDetails",
		"url_query", "urlQuery",
		"url_fragment", "urlFragment",
		"command_output_preview", "commandOutputPreview",
		"command_stdout_preview", "commandStdoutPreview",
		"archived_url_query_value", "archivedURLQueryValue",
	} {
		t.Run(field, func(t *testing.T) {
			payload := map[string]any{"command_id": "cmd_1", field: "benign-looking"}
			if _, _, err := CanonicalizePayload(descriptor, payload, RedactionNormalOnly); !errors.Is(err, ErrForbiddenField) {
				t.Fatalf("CanonicalizePayload(%q) error = %v, want ErrForbiddenField", field, err)
			}
		})
	}
}

func TestRedactionNormalizesPreviewDisplayActionsForCapture(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	preview := []FieldDecision{
		{Path: "stdout", Sensitivity: SensitivityForbidden, Action: RedactionActionForbidden},
		{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
		{Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Action: RedactionActionMasked},
	}
	capture, err := NormalizeCaptureRedaction(descriptor, preview)
	if err != nil {
		t.Fatalf("NormalizeCaptureRedaction() error = %v", err)
	}
	want := []FieldDecision{
		{Path: "endpoint", Sensitivity: SensitivitySensitiveTopology, Action: RedactionActionStripped},
		{Path: "metric_name", Sensitivity: SensitivityNormal, Action: RedactionActionIncluded},
		{Path: "stdout", Sensitivity: SensitivityForbidden, Action: RedactionActionStripped},
	}
	if !reflect.DeepEqual(capture, want) {
		t.Fatalf("NormalizeCaptureRedaction() = %#v, want %#v", capture, want)
	}
	if preview[0].Action != RedactionActionForbidden || preview[2].Action != RedactionActionMasked {
		t.Fatalf("NormalizeCaptureRedaction() mutated preview: %#v", preview)
	}
}

func TestRedactionForbiddenNormalizationAvoidsConceptSubstrings(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringHostV1Key())
	for _, path := range []string{
		"keyspace_hit_rate", "tokenizer_version", "cookiecutter_version", "mountain_region", "environmental_score",
		"outputting_status", "detailed_status", "queryable_count", "fragmentation_count",
		"archived_outputting_preview", "archived_queryable_value", "archived_fragmentation_value",
	} {
		descriptor.Fields = append(descriptor.Fields, FieldDefinition{Path: path, Sensitivity: SensitivityNormal})
	}
	payload := map[string]any{
		"metric_name":                  "cpu",
		"keyspace_hit_rate":            0.9,
		"tokenizer_version":            "v1",
		"cookiecutter_version":         "v2",
		"mountain_region":              "west",
		"environmental_score":          10,
		"outputting_status":            "complete",
		"detailed_status":              "healthy",
		"queryable_count":              3,
		"fragmentation_count":          4,
		"archived_outputting_preview":  "complete",
		"archived_queryable_value":     5,
		"archived_fragmentation_value": 6,
	}
	if _, _, err := CanonicalizePayload(descriptor, payload, RedactionNormalOnly); err != nil {
		t.Fatalf("CanonicalizePayload(false-positive corpus) error = %v", err)
	}
}

func TestRedactionRejectsSecretContentInAllowedString(t *testing.T) {
	descriptor := testDescriptor(t, IPQualityReportV1Key())
	payload := map[string]any{
		"metric_name":        "risk",
		"diagnostic_summary": "upstream failed: authorization: Bearer abc123",
	}
	if _, _, err := CanonicalizePayload(descriptor, payload, RedactionNormalOnly); !errors.Is(err, ErrForbiddenField) {
		t.Fatalf("CanonicalizePayload(secret content) error = %v, want ErrForbiddenField", err)
	}
}

func TestRedactionRejectsHostileSecretContentCorpus(t *testing.T) {
	descriptor := testDescriptor(t, IPQualityReportV1Key())
	hostile := []string{
		"upstream rejected secret=abc123",
		`diagnostic={"client_secret":"abc123"}`,
		"session eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.signature0123456789",
		"credential ghp_0123456789abcdefghijklmnopqrstuvwxyz",
		"credential sk-0123456789abcdefghijklmnopqrstuv",
		"credential AKIA0123456789ABCDEF",
		"-----BEGIN RSA PRIVATE KEY-----",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"-----BEGIN PGP PRIVATE KEY BLOCK-----",
		"\uff53\uff45\uff43\uff52\uff45\uff54\uff1dabc123",
		"\uff0d\uff0d\uff0d\uff0d\uff0dBEGIN PRIVATE KEY\uff0d\uff0d\uff0d\uff0d\uff0d",
		"\uff0d\uff0d\uff0d\uff0d\uff0d\uff22\uff25\uff27\uff29\uff2e \uff30\uff27\uff30 \uff30\uff32\uff29\uff36\uff21\uff34\uff25 \uff2b\uff25\uff39 \uff22\uff2c\uff2f\uff23\uff2b\uff0d\uff0d\uff0d\uff0d\uff0d",
	}
	for _, value := range hostile {
		t.Run(value, func(t *testing.T) {
			_, _, err := CanonicalizePayload(descriptor, map[string]any{
				"metric_name":        "risk",
				"diagnostic_summary": value,
			}, RedactionNormalOnly)
			if !errors.Is(err, ErrForbiddenField) {
				t.Fatalf("CanonicalizePayload(%q) error = %v, want ErrForbiddenField", value, err)
			}
		})
	}
}

func TestRedactionSecretContentAvoidsNearMatchFalsePositives(t *testing.T) {
	descriptor := testDescriptor(t, IPQualityReportV1Key())
	allowed := []string{
		"secretary=abc",
		"passwordless authentication enabled",
		"tokenizer output is stable",
		"public key rotation complete",
		"bearer health is a harmless phrase",
		"JWT-shaped example eyJ.not-a-token",
		"-----BEGIN PUBLIC KEY-----",
		"-----BEGIN PGP PUBLIC KEY BLOCK-----",
		"PGP PRIVATE KEY BLOCK import is disabled",
		"-----END PGP PRIVATE KEY BLOCK-----",
		"-----BEGIN PGP PRIVATE KEY BLOCKED-----",
		"AKIA is an airport code in this sentence",
	}
	for _, value := range allowed {
		t.Run(value, func(t *testing.T) {
			if _, _, err := CanonicalizePayload(descriptor, map[string]any{
				"metric_name":        "risk",
				"diagnostic_summary": value,
			}, RedactionNormalOnly); err != nil {
				t.Fatalf("CanonicalizePayload(%q) error = %v", value, err)
			}
		})
	}
}

func TestRedactionRejectsCompatibilityUnicodeForbiddenField(t *testing.T) {
	descriptor := testDescriptor(t, CommandAuditV1Key())
	_, _, err := CanonicalizePayload(descriptor, map[string]any{
		"command_id": "cmd_1",
		"\uff43\uff4f\uff4d\uff4d\uff41\uff4e\uff44\uff3f\uff4f\uff55\uff54\uff50\uff55\uff54": "must-not-survive",
	}, RedactionNormalOnly)
	if !errors.Is(err, ErrForbiddenField) {
		t.Fatalf("CanonicalizePayload(compatibility field) error = %v, want ErrForbiddenField", err)
	}
}

func TestRedactionTopologyModesAndURLNormalization(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	payload := map[string]any{
		"metric_name": "latency_ms",
		"endpoint":    "HTTPS://user:pass@Example.COM:8443/health?token=secret#debug",
	}

	normal, report, err := CanonicalizePayload(descriptor, payload, RedactionNormalOnly)
	if err != nil {
		t.Fatalf("CanonicalizePayload(normal) error = %v", err)
	}
	if strings.Contains(string(normal.Bytes()), "endpoint") {
		t.Fatalf("normal canonical payload retained sensitive endpoint: %s", normal.Bytes())
	}
	if got := decisionAction(t, report, "endpoint"); got != RedactionActionStripped {
		t.Fatalf("endpoint action = %q, want %q", got, RedactionActionStripped)
	}

	included, report, err := CanonicalizePayload(descriptor, payload, RedactionIncludeSensitiveTopology)
	if err != nil {
		t.Fatalf("CanonicalizePayload(include topology) error = %v", err)
	}
	got := string(included.Bytes())
	if !strings.Contains(got, `"endpoint":"https://example.com:8443/health"`) {
		t.Fatalf("canonical payload did not normalize endpoint: %s", got)
	}
	for _, forbidden := range []string{"user", "pass", "token", "secret", "debug"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("canonical endpoint retained %q: %s", forbidden, got)
		}
	}
	if got := decisionAction(t, report, "endpoint"); got != RedactionActionIncluded {
		t.Fatalf("endpoint action = %q, want %q", got, RedactionActionIncluded)
	}

	masked, report, err := CanonicalizePayload(descriptor, payload, RedactionMaskSensitiveTopology)
	if err != nil {
		t.Fatalf("CanonicalizePayload(mask topology) error = %v", err)
	}
	if !strings.Contains(string(masked.Bytes()), `"endpoint":"[redacted]"`) {
		t.Fatalf("masked canonical payload = %s", masked.Bytes())
	}
	if got := decisionAction(t, report, "endpoint"); got != RedactionActionMasked {
		t.Fatalf("endpoint action = %q, want %q", got, RedactionActionMasked)
	}
}

func TestRedactionNormalizesSensitiveURLArraysAndRejectsUnsafeSchemes(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	descriptor.Fields = append(descriptor.Fields, FieldDefinition{
		Path:        "endpoints",
		Sensitivity: SensitivitySensitiveTopology,
		Format:      FieldFormatURL,
	})

	canonical, _, err := CanonicalizePayload(descriptor, map[string]any{
		"metric_name": "latency_ms",
		"endpoints": []string{
			"https://user:pass@EXAMPLE.com/health?token=secret",
			"tcp://Probe.EXAMPLE.com:443/check#fragment",
		},
	}, RedactionIncludeSensitiveTopology)
	if err != nil {
		t.Fatalf("CanonicalizePayload(URL array) error = %v", err)
	}
	got := string(canonical.Bytes())
	for _, want := range []string{"https://example.com/health", "tcp://probe.example.com:443/check"} {
		if !strings.Contains(got, want) {
			t.Fatalf("canonical URL array = %s, want %q", got, want)
		}
	}
	for _, forbidden := range []string{"user", "pass", "token", "secret", "fragment"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("canonical URL array retained %q: %s", forbidden, got)
		}
	}

	if _, _, err := CanonicalizePayload(descriptor, map[string]any{
		"metric_name": "latency_ms",
		"endpoint":    "javascript://example.com/alert",
	}, RedactionIncludeSensitiveTopology); !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("CanonicalizePayload(unsafe URL) error = %v, want ErrInvalidCanonicalPayload", err)
	}
}

func TestRedactionNormalizesURLIDNAndDefaultPortsAndRejectsIPv6Zones(t *testing.T) {
	descriptor := testDescriptor(t, MonitoringProbeV2Key())
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "IDN", input: "HTTPS://B\u00dcCHER.example:443/health", want: "https://xn--bcher-kva.example/health"},
		{name: "HTTP default port", input: "http://Example.COM:80/health", want: "http://example.com/health"},
		{name: "SSH default port", input: "ssh://Example.COM:22/check", want: "ssh://example.com/check"},
		{name: "IPv6", input: "https://[2001:0db8::1]:443/health", want: "https://[2001:db8::1]/health"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canonical, _, err := CanonicalizePayload(descriptor, map[string]any{
				"metric_name": "latency_ms",
				"endpoint":    tt.input,
			}, RedactionIncludeSensitiveTopology)
			if err != nil {
				t.Fatalf("CanonicalizePayload(%q) error = %v", tt.input, err)
			}
			if got := string(canonical.Bytes()); !strings.Contains(got, `"endpoint":"`+tt.want+`"`) {
				t.Fatalf("canonical URL = %s, want %q", got, tt.want)
			}
		})
	}

	if _, _, err := CanonicalizePayload(descriptor, map[string]any{
		"metric_name": "latency_ms",
		"endpoint":    "http://[fe80::1%25eth0]:80/health",
	}, RedactionIncludeSensitiveTopology); !errors.Is(err, ErrInvalidCanonicalPayload) {
		t.Fatalf("CanonicalizePayload(IPv6 zone) error = %v, want ErrInvalidCanonicalPayload", err)
	}
}

func decisionAction(t *testing.T, report RedactionReport, path string) RedactionAction {
	t.Helper()
	for _, decision := range report.Decisions {
		if decision.Path == path {
			return decision.Action
		}
	}
	t.Fatalf("no redaction decision for %q in %#v", path, report.Decisions)
	return ""
}
