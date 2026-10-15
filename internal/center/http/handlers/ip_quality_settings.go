package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	centersettings "houfeng/internal/center/settings"
)

// Keep this whitelist independent of the persisted settings, which contain keys.
type ipQualitySettingsResponse struct {
	Enabled           bool                     `json:"enabled"`
	FrequencySeconds  int                      `json:"frequency_seconds"`
	StaleAfterSeconds int                      `json:"stale_after_seconds"`
	TimeoutSeconds    int                      `json:"timeout_seconds"`
	Services          []string                 `json:"services"`
	MailEnabled       bool                     `json:"mail_enabled"`
	Sources           ipQualitySourcesResponse `json:"sources"`
}

type ipQualitySourcesResponse struct {
	IPAPIIS   ipQualitySourceResponse `json:"ipapi_is"`
	AbuseIPDB ipQualitySourceResponse `json:"abuseipdb"`
	MaxMind   ipQualitySourceResponse `json:"maxmind"`
}

type ipQualitySourceResponse struct {
	Configured    bool   `json:"configured"`
	MaskedSummary string `json:"masked_summary"`
}

type ipQualitySettingsUpdateRequest struct {
	Enabled           *bool                          `json:"enabled,omitempty"`
	FrequencySeconds  *int                           `json:"frequency_seconds,omitempty"`
	StaleAfterSeconds *int                           `json:"stale_after_seconds,omitempty"`
	TimeoutSeconds    *int                           `json:"timeout_seconds,omitempty"`
	Services          *[]string                      `json:"services,omitempty"`
	MailEnabled       *bool                          `json:"mail_enabled,omitempty"`
	Sources           *ipQualitySourcesUpdateRequest `json:"sources,omitempty"`
}

type ipQualitySourcesUpdateRequest struct {
	IPAPIISKey        optionalString `json:"ipapi_is_key"`
	AbuseIPDBKey      optionalString `json:"abuseipdb_key"`
	MaxMindAccountID  optionalString `json:"maxmind_account_id"`
	MaxMindLicenseKey optionalString `json:"maxmind_license_key"`
}

func (request *ipQualitySourcesUpdateRequest) UnmarshalJSON(data []byte) error {
	type wire ipQualitySourcesUpdateRequest
	var decoded wire
	if err := decodeJSONValue(bytes.NewReader(data), &decoded); err != nil {
		return err
	}
	id, key := decoded.MaxMindAccountID, decoded.MaxMindLicenseKey
	// Clearing either member requires an explicit clear of the complete pair.
	if id.Set && strings.TrimSpace(id.Value) == "" || key.Set && strings.TrimSpace(key.Value) == "" {
		if !id.Set || !key.Set || strings.TrimSpace(id.Value) != "" || strings.TrimSpace(key.Value) != "" {
			return errors.New("maxmind credentials must be cleared together")
		}
	}
	*request = ipQualitySourcesUpdateRequest(decoded)
	return nil
}

func (request *ipQualitySettingsUpdateRequest) UnmarshalJSON(data []byte) error {
	// Pointer fields preserve omission, but encoding/json alone treats null as
	// omission. Reject null before decoding the independently whitelisted DTO.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return errors.New("ip quality settings cannot be null")
	}
	for _, raw := range fields {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("ip quality settings field cannot be null")
		}
	}
	type wire ipQualitySettingsUpdateRequest
	var decoded wire
	if err := decodeJSONValue(bytes.NewReader(data), &decoded); err != nil {
		return err
	}
	*request = ipQualitySettingsUpdateRequest(decoded)
	return nil
}

func mergeIPQualitySettingsUpdate(current centersettings.IPQualitySettings, input ipQualitySettingsUpdateRequest) centersettings.IPQualitySettings {
	merged := current
	if input.Enabled != nil {
		merged.Enabled = *input.Enabled
	}
	if input.FrequencySeconds != nil {
		merged.FrequencySeconds = *input.FrequencySeconds
	}
	if input.StaleAfterSeconds != nil {
		merged.StaleAfterSeconds = *input.StaleAfterSeconds
	}
	if input.TimeoutSeconds != nil {
		merged.TimeoutSeconds = *input.TimeoutSeconds
	}
	if input.Services != nil {
		merged.Services = append([]string{}, (*input.Services)...)
	}
	if input.MailEnabled != nil {
		merged.MailEnabled = *input.MailEnabled
	}
	if input.Sources != nil {
		if input.Sources.IPAPIISKey.Set {
			merged.Sources.IPAPIISKey = input.Sources.IPAPIISKey.Value
		}
		if input.Sources.AbuseIPDBKey.Set {
			merged.Sources.AbuseIPDBKey = input.Sources.AbuseIPDBKey.Value
		}
		if input.Sources.MaxMindAccountID.Set {
			merged.Sources.MaxMindAccountID = input.Sources.MaxMindAccountID.Value
		}
		if input.Sources.MaxMindLicenseKey.Set {
			merged.Sources.MaxMindLicenseKey = input.Sources.MaxMindLicenseKey.Value
		}
	}
	return merged
}

func newIPQualitySettingsResponse(record centersettings.IPQualitySettings) ipQualitySettingsResponse {
	ipapiMasked := maskSecretTail(record.Sources.IPAPIISKey)
	abuseMasked := maskSecretTail(record.Sources.AbuseIPDBKey)
	return ipQualitySettingsResponse{
		Enabled:           record.Enabled,
		FrequencySeconds:  record.FrequencySeconds,
		StaleAfterSeconds: record.StaleAfterSeconds,
		TimeoutSeconds:    record.TimeoutSeconds,
		Services:          append([]string{}, record.Services...),
		MailEnabled:       record.MailEnabled,
		Sources: ipQualitySourcesResponse{
			IPAPIIS:   ipQualitySourceResponse{Configured: ipapiMasked != "", MaskedSummary: ipapiMasked},
			AbuseIPDB: ipQualitySourceResponse{Configured: abuseMasked != "", MaskedSummary: abuseMasked},
			MaxMind: ipQualitySourceResponse{
				Configured:    record.Sources.MaxMindAccountID != "" && record.Sources.MaxMindLicenseKey != "",
				MaskedSummary: maskSecretTail(record.Sources.MaxMindLicenseKey),
			},
		},
	}
}
