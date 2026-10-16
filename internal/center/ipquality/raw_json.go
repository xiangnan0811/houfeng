package ipquality

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"houfeng/internal/contracts/agentapi"
)

const MaxRawJSONBytes = 128 * 1024

func SanitizeRawJSON(raw json.RawMessage) json.RawMessage {
	return sanitizeJSONWithLimit(raw, MaxRawJSONBytes, "raw_json_size_limit")
}

func SanitizeExtraJSON(raw json.RawMessage) json.RawMessage {
	return sanitizeJSONWithLimit(raw, MaxRawJSONBytes, "extra_json_size_limit")
}

// SanitizeCollectionDiagnostics preserves invalid paired identity across JSONB
// numeric normalization and size truncation. A null ID remains paired, but can
// never become a valid collection or be mistaken for a legacy report.
func SanitizeCollectionDiagnostics(raw json.RawMessage) json.RawMessage {
	metadata := agentapi.ParseCollectionMetadata(raw)
	clean := SanitizeExtraJSON(raw)
	if !metadata.Paired {
		return clean
	}
	if metadata.Valid && agentapi.ParseCollectionMetadata(clean).Valid {
		return clean
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(clean, &fields) != nil || fields == nil {
		fields = make(map[string]json.RawMessage, 1)
	}
	fields[agentapi.IPQualityDiagnosticsCollectionIDKey] = json.RawMessage("null")
	result, err := json.Marshal(fields)
	if err != nil {
		return json.RawMessage(`{"collection_id":null}`)
	}
	return result
}

func sanitizeJSONWithLimit(raw json.RawMessage, limit int, reason string) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	// Keep numeric lexemes: normalizing 5.0 into 5 could turn invalid
	// collection metadata into a valid database family-state assertion.
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil
	}
	payload, err := json.Marshal(sanitizeRawJSONValue(value))
	if err != nil || len(payload) == 0 {
		return nil
	}
	if len(payload) <= limit {
		return json.RawMessage(payload)
	}
	marker, err := json.Marshal(map[string]any{
		"truncated": true,
		"reason":    reason,
	})
	if err != nil {
		return nil
	}
	return json.RawMessage(marker)
}

func sanitizeRawJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if isSensitiveRawJSONKey(key) {
				out[key] = "[redacted]"
				continue
			}
			out[key] = sanitizeRawJSONValue(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, sanitizeRawJSONValue(child))
		}
		return out
	default:
		return value
	}
}

func isSensitiveRawJSONKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "-", "_"))
	return normalized == "authorization" ||
		normalized == "cookie" ||
		normalized == "set_cookie" ||
		normalized == "token" ||
		normalized == "access_token" ||
		normalized == "refresh_token" ||
		normalized == "api_key" ||
		normalized == "apikey" ||
		normalized == "secret" ||
		strings.HasSuffix(normalized, "_secret") ||
		strings.HasSuffix(normalized, "_token") ||
		strings.HasSuffix(normalized, "_key") ||
		strings.Contains(normalized, "password")
}
