package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	centersettings "houfeng/internal/center/settings"
)

type SettingsRepository interface {
	GetSettings(ctx context.Context) (centersettings.CenterSettings, error)
	MutateSettings(ctx context.Context, mutate centersettings.MutateSettingsFunc) (centersettings.CenterSettings, error)
}

type settingsResponse struct {
	Telegram                 telegramSettingsResponse              `json:"telegram"`
	Feishu                   feishuSettingsResponse                `json:"feishu"`
	HostSampleFrequencyTier  string                                `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults   centersettings.ProbeFrequencyDefaults `json:"probe_frequency_defaults"`
	IncidentDefaults         centersettings.IncidentDefaults       `json:"incident_defaults"`
	IncidentSweep            incidentSweepResponse                 `json:"incident_sweep"`
	OverrideRules            centersettings.OverrideRules          `json:"override_rules"`
	RetentionPolicy          centersettings.RetentionPolicy        `json:"retention_policy"`
	SubscriptionCostSettings subscriptionCostSettingsResponse      `json:"subscription_cost_settings"`
	IPQualitySettings        ipQualitySettingsResponse             `json:"ip_quality_settings"`
	SettingsSource           centersettings.ConfigSource           `json:"settings_source"`
}

type telegramSettingsResponse struct {
	Mode               string                      `json:"mode"`
	Source             centersettings.ConfigSource `json:"source"`
	Active             bool                        `json:"active"`
	ChatID             string                      `json:"chat_id"`
	TokenPresent       bool                        `json:"token_present"`
	TokenMaskedSummary string                      `json:"token_masked_summary,omitempty"`
	Deployment         telegramDeploymentResponse  `json:"deployment"`
}

type telegramDeploymentResponse struct {
	Configured         bool   `json:"configured"`
	ChatID             string `json:"chat_id"`
	TokenPresent       bool   `json:"token_present"`
	TokenMaskedSummary string `json:"token_masked_summary,omitempty"`
}

type feishuSettingsResponse struct {
	Enabled                 bool   `json:"enabled"`
	WebhookURLPresent       bool   `json:"webhook_url_present"`
	WebhookURLMaskedSummary string `json:"webhook_url_masked_summary,omitempty"`
}

type incidentSweepResponse struct {
	Mode              string                      `json:"mode"`
	Source            centersettings.ConfigSource `json:"source"`
	DeploymentSeconds int                         `json:"deployment_seconds"`
}

type settingsUpdateRequest struct {
	Telegram                 *telegramSettingsUpdateRequest         `json:"telegram,omitempty"`
	Feishu                   feishuSettingsUpdateRequest            `json:"feishu"`
	HostSampleFrequencyTier  string                                 `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults   centersettings.ProbeFrequencyDefaults  `json:"probe_frequency_defaults"`
	IncidentDefaults         centersettings.IncidentDefaults        `json:"incident_defaults"`
	IncidentSweepMode        *string                                `json:"incident_sweep_mode,omitempty"`
	OverrideRules            centersettings.OverrideRules           `json:"override_rules"`
	RetentionPolicy          centersettings.RetentionPolicy         `json:"retention_policy"`
	SubscriptionCostSettings *subscriptionCostSettingsUpdateRequest `json:"subscription_cost_settings,omitempty"`
	IPQualitySettings        *ipQualitySettingsUpdateRequest        `json:"ip_quality_settings,omitempty"`
}

// Optional blocks use raw messages when omission must remain distinct from
// explicit null. The existing core settings remain full-form value fields for
// compatibility with the settings page contract.
type settingsUpdateRequestWire struct {
	Telegram                 json.RawMessage                        `json:"telegram"`
	Feishu                   feishuSettingsUpdateRequest            `json:"feishu"`
	HostSampleFrequencyTier  string                                 `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults   centersettings.ProbeFrequencyDefaults  `json:"probe_frequency_defaults"`
	IncidentDefaults         centersettings.IncidentDefaults        `json:"incident_defaults"`
	IncidentSweepMode        json.RawMessage                        `json:"incident_sweep_mode"`
	OverrideRules            centersettings.OverrideRules           `json:"override_rules"`
	RetentionPolicy          centersettings.RetentionPolicy         `json:"retention_policy"`
	SubscriptionCostSettings *subscriptionCostSettingsUpdateRequest `json:"subscription_cost_settings,omitempty"`
	IPQualitySettings        json.RawMessage                        `json:"ip_quality_settings"`
}

func (request *settingsUpdateRequest) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("settings request cannot be null")
	}
	var wire settingsUpdateRequestWire
	if err := decodeJSONValue(bytes.NewReader(data), &wire); err != nil {
		return err
	}

	decoded := settingsUpdateRequest{
		Feishu:                   wire.Feishu,
		HostSampleFrequencyTier:  wire.HostSampleFrequencyTier,
		ProbeFrequencyDefaults:   wire.ProbeFrequencyDefaults,
		IncidentDefaults:         wire.IncidentDefaults,
		OverrideRules:            wire.OverrideRules,
		RetentionPolicy:          wire.RetentionPolicy,
		SubscriptionCostSettings: wire.SubscriptionCostSettings,
	}
	if wire.Telegram != nil {
		var telegram telegramSettingsUpdateRequest
		if err := decodeOptionalSettingsJSON(wire.Telegram, &telegram); err != nil {
			return err
		}
		decoded.Telegram = &telegram
	}
	if wire.IncidentSweepMode != nil {
		var mode string
		if err := decodeOptionalSettingsJSON(wire.IncidentSweepMode, &mode); err != nil {
			return err
		}
		decoded.IncidentSweepMode = &mode
	}
	if wire.IPQualitySettings != nil {
		var ipQuality ipQualitySettingsUpdateRequest
		if err := decodeOptionalSettingsJSON(wire.IPQualitySettings, &ipQuality); err != nil {
			return err
		}
		decoded.IPQualitySettings = &ipQuality
	}

	*request = decoded
	return nil
}

func decodeOptionalSettingsJSON(raw json.RawMessage, dst any) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errors.New("settings field cannot be null")
	}
	return decodeJSONValue(bytes.NewReader(raw), dst)
}

// optionalString records presence while rejecting null and non-string values.
type optionalString struct {
	Value string
	Set   bool
}

func (value *optionalString) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("settings string cannot be null")
	}
	var decoded string
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value.Value = decoded
	value.Set = true
	return nil
}

type telegramSettingsUpdateRequest struct {
	Mode     optionalString `json:"mode"`
	BotToken optionalString `json:"bot_token"`
	ChatID   optionalString `json:"chat_id"`
}

type feishuSettingsUpdateRequest struct {
	Enabled    *bool   `json:"enabled,omitempty"`
	WebhookURL *string `json:"webhook_url,omitempty"`
}

type subscriptionCostSettingsResponse struct {
	BaseCurrency                string `json:"base_currency"`
	ExchangeRateProvider        string `json:"exchange_rate_provider"`
	FixerConfigured             bool   `json:"fixer_configured"`
	FixerMaskedSummary          string `json:"fixer_masked_summary,omitempty"`
	DefaultReminderOffsetsDays  []int  `json:"default_reminder_offsets_days"`
	MaxReminderLeadDays         int    `json:"max_reminder_lead_days"`
	ExchangeRateStaleAfterHours int    `json:"exchange_rate_stale_after_hours"`
}

type subscriptionCostSettingsUpdateRequest struct {
	BaseCurrency                *string `json:"base_currency,omitempty"`
	ExchangeRateProvider        *string `json:"exchange_rate_provider,omitempty"`
	FixerAPIKey                 *string `json:"fixer_api_key,omitempty"`
	FixerConfigured             *bool   `json:"fixer_configured,omitempty"`
	FixerMaskedSummary          *string `json:"fixer_masked_summary,omitempty"`
	DefaultReminderOffsetsDays  *[]int  `json:"default_reminder_offsets_days,omitempty"`
	MaxReminderLeadDays         *int    `json:"max_reminder_lead_days,omitempty"`
	ExchangeRateStaleAfterHours *int    `json:"exchange_rate_stale_after_hours,omitempty"`
}

func Settings(repo SettingsRepository, refresher ExchangeRateRefreshRequester, deployment centersettings.DeploymentDefaults) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetSettings(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, newSettingsResponse(record, deployment))
		case http.MethodPut:
			var input settingsUpdateRequest
			if err := decodeSettingsJSONBody(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if err := validateSettingsUpdateRequest(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.MutateSettings(r.Context(), func(current centersettings.CenterSettings) (centersettings.CenterSettings, error) {
				return mergeSettingsUpdate(current, input, deployment)
			})
			if err != nil {
				if errors.Is(err, centersettings.ErrInvalidSettings) {
					writeError(w, http.StatusBadRequest, "invalid input")
					return
				}
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if refresher != nil {
				refresher.RequestRefresh(false)
			}
			writeJSON(w, http.StatusOK, newSettingsResponse(record, deployment))
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func validateSettingsUpdateRequest(input settingsUpdateRequest) error {
	if input.Telegram != nil {
		mode := input.Telegram.Mode.Value
		switch mode {
		case "inherit", "disabled":
			if input.Telegram.BotToken.Set || input.Telegram.ChatID.Set {
				return centersettings.ErrInvalidSettings
			}
		case "override":
			if input.Telegram.BotToken.Set {
				token := strings.TrimSpace(input.Telegram.BotToken.Value)
				if token == "" || strings.Contains(token, "*") {
					return centersettings.ErrInvalidSettings
				}
			}
			if input.Telegram.ChatID.Set && strings.TrimSpace(input.Telegram.ChatID.Value) == "" {
				return centersettings.ErrInvalidSettings
			}
		default:
			return centersettings.ErrInvalidSettings
		}
	}

	if input.IncidentDefaults.SweepIntervalSeconds <= 0 {
		return centersettings.ErrInvalidSettings
	}
	if input.IncidentSweepMode != nil {
		switch *input.IncidentSweepMode {
		case "inherit":
		case "override":
			if input.IncidentDefaults.SweepIntervalSeconds <= 0 {
				return centersettings.ErrInvalidSettings
			}
		default:
			return centersettings.ErrInvalidSettings
		}
	}
	return nil
}

func mergeSettingsUpdate(current centersettings.CenterSettings, input settingsUpdateRequest, deployment centersettings.DeploymentDefaults) (centersettings.CenterSettings, error) {
	merged := current

	if input.Telegram != nil {
		switch input.Telegram.Mode.Value {
		case "inherit":
			merged.Telegram = centersettings.TelegramSettings{}
		case "disabled":
			merged.Telegram = centersettings.TelegramSettings{RuntimeManaged: true}
		case "override":
			effective := centersettings.ResolveTelegram(current.Telegram, deployment.Telegram)
			token := strings.TrimSpace(effective.BotToken)
			chatID := strings.TrimSpace(effective.ChatID)
			if input.Telegram.BotToken.Set {
				token = strings.TrimSpace(input.Telegram.BotToken.Value)
			}
			if input.Telegram.ChatID.Set {
				chatID = strings.TrimSpace(input.Telegram.ChatID.Value)
			}
			if token == "" || chatID == "" {
				return centersettings.CenterSettings{}, centersettings.ErrInvalidSettings
			}
			merged.Telegram = centersettings.TelegramSettings{
				BotToken:       token,
				ChatID:         chatID,
				RuntimeManaged: true,
			}
		default:
			return centersettings.CenterSettings{}, centersettings.ErrInvalidSettings
		}
	}

	if input.Feishu.Enabled != nil {
		merged.FeishuEnabled = *input.Feishu.Enabled
	}
	if input.Feishu.WebhookURL != nil {
		merged.FeishuWebhookURL = strings.TrimSpace(*input.Feishu.WebhookURL)
	}
	merged.HostSampleFrequencyTier = input.HostSampleFrequencyTier
	merged.ProbeFrequencyDefaults = input.ProbeFrequencyDefaults
	merged.OverrideRules = input.OverrideRules
	merged.RetentionPolicy = input.RetentionPolicy
	if input.SubscriptionCostSettings != nil {
		merged.SubscriptionCost = mergeSubscriptionCostSettingsUpdate(current.SubscriptionCost, *input.SubscriptionCostSettings)
	}
	if input.IPQualitySettings != nil {
		merged.IPQuality = mergeIPQualitySettingsUpdate(current.IPQuality, *input.IPQualitySettings)
	}

	incidentDefaults := input.IncidentDefaults
	if input.IncidentSweepMode != nil {
		switch *input.IncidentSweepMode {
		case "inherit":
			incidentDefaults.SweepIntervalSeconds = centersettings.Default().IncidentDefaults.SweepIntervalSeconds
			merged.IncidentSweepRuntimeManaged = false
		case "override":
			merged.IncidentSweepRuntimeManaged = true
		}
	} else {
		// A full settings form may echo the currently displayed effective
		// sweep. Without an explicit mode, preserve the lock-held raw value
		// and ownership instead of turning that echo into an override.
		incidentDefaults.SweepIntervalSeconds = current.IncidentDefaults.SweepIntervalSeconds
		merged.IncidentSweepRuntimeManaged = current.IncidentSweepRuntimeManaged
	}
	merged.IncidentDefaults = incidentDefaults

	return merged, nil
}

func newSettingsResponse(record centersettings.CenterSettings, deployment centersettings.DeploymentDefaults) settingsResponse {
	effective := centersettings.ResolveEffectiveSettings(record, deployment)
	deploymentTelegram := deployment.Telegram
	deploymentTelegram.BotToken = strings.TrimSpace(deploymentTelegram.BotToken)
	deploymentTelegram.ChatID = strings.TrimSpace(deploymentTelegram.ChatID)
	deploymentTelegram.RuntimeManaged = false

	deploymentSweepSeconds := deployment.IncidentSweepIntervalSeconds
	if deploymentSweepSeconds <= 0 {
		deploymentSweepSeconds = centersettings.Default().IncidentDefaults.SweepIntervalSeconds
	}
	deploymentSweepSource := deployment.IncidentSweepSource
	if deploymentSweepSource != centersettings.ConfigSourceDeployment {
		deploymentSweepSource = centersettings.ConfigSourceDefault
	}

	telegramSource := effective.Effective.TelegramSource
	if telegramSource == "" {
		if effective.Telegram.RuntimeManaged {
			telegramSource = centersettings.ConfigSourceDatabase
		} else if effective.Telegram.Enabled() {
			telegramSource = centersettings.ConfigSourceDeployment
		} else {
			telegramSource = centersettings.ConfigSourceDefault
		}
	}
	incidentSweepSource := effective.Effective.IncidentSweepSource
	if incidentSweepSource == "" {
		if effective.IncidentSweepRuntimeManaged {
			incidentSweepSource = centersettings.ConfigSourceDatabase
		} else {
			incidentSweepSource = deploymentSweepSource
		}
	}

	return settingsResponse{
		Telegram: telegramSettingsResponse{
			Mode:               telegramMode(record.Telegram),
			Source:             telegramSource,
			Active:             effective.Telegram.Enabled(),
			ChatID:             strings.TrimSpace(effective.Telegram.ChatID),
			TokenPresent:       strings.TrimSpace(effective.Telegram.BotToken) != "",
			TokenMaskedSummary: maskTelegramBotToken(effective.Telegram.BotToken),
			Deployment: telegramDeploymentResponse{
				Configured:         deploymentTelegram.Enabled(),
				ChatID:             deploymentTelegram.ChatID,
				TokenPresent:       strings.TrimSpace(deploymentTelegram.BotToken) != "",
				TokenMaskedSummary: maskTelegramBotToken(deploymentTelegram.BotToken),
			},
		},
		Feishu: feishuSettingsResponse{
			Enabled:                 effective.FeishuEnabled,
			WebhookURLPresent:       strings.TrimSpace(effective.FeishuWebhookURL) != "",
			WebhookURLMaskedSummary: maskWebhookURL(effective.FeishuWebhookURL),
		},
		HostSampleFrequencyTier: effective.HostSampleFrequencyTier,
		ProbeFrequencyDefaults:  effective.ProbeFrequencyDefaults,
		IncidentDefaults:        effective.IncidentDefaults,
		IncidentSweep: incidentSweepResponse{
			Mode:              incidentSweepMode(record.IncidentSweepRuntimeManaged),
			Source:            incidentSweepSource,
			DeploymentSeconds: deploymentSweepSeconds,
		},
		OverrideRules:            effective.OverrideRules,
		RetentionPolicy:          effective.RetentionPolicy,
		SubscriptionCostSettings: newSubscriptionCostSettingsResponse(effective.SubscriptionCost),
		IPQualitySettings:        newIPQualitySettingsResponse(effective.IPQuality),
		SettingsSource:           settingsSource(effective),
	}
}

func settingsSource(record centersettings.CenterSettings) centersettings.ConfigSource {
	if record.Effective.Persisted {
		return centersettings.ConfigSourceDatabase
	}
	return centersettings.ConfigSourceDefault
}

func telegramMode(record centersettings.TelegramSettings) string {
	if !record.RuntimeManaged {
		return "inherit"
	}
	if record.Enabled() {
		return "override"
	}
	return "disabled"
}

func incidentSweepMode(runtimeManaged bool) string {
	if runtimeManaged {
		return "override"
	}
	return "inherit"
}

func mergeSubscriptionCostSettingsUpdate(current centersettings.SubscriptionCostSettings, input subscriptionCostSettingsUpdateRequest) centersettings.SubscriptionCostSettings {
	merged := current
	if input.BaseCurrency != nil {
		merged.BaseCurrency = strings.TrimSpace(*input.BaseCurrency)
	}
	if input.ExchangeRateProvider != nil {
		merged.ExchangeRateProvider = strings.TrimSpace(*input.ExchangeRateProvider)
	}
	if input.FixerAPIKey != nil {
		merged.FixerAPIKey = strings.TrimSpace(*input.FixerAPIKey)
	}
	if input.DefaultReminderOffsetsDays != nil {
		merged.DefaultReminderOffsetsDays = append([]int(nil), (*input.DefaultReminderOffsetsDays)...)
	}
	if input.MaxReminderLeadDays != nil {
		merged.MaxReminderLeadDays = *input.MaxReminderLeadDays
	}
	if input.ExchangeRateStaleAfterHours != nil {
		merged.ExchangeRateStaleAfterHours = *input.ExchangeRateStaleAfterHours
	}
	return merged
}

func newSubscriptionCostSettingsResponse(record centersettings.SubscriptionCostSettings) subscriptionCostSettingsResponse {
	key := strings.TrimSpace(record.FixerAPIKey)
	return subscriptionCostSettingsResponse{
		BaseCurrency:                record.BaseCurrency,
		ExchangeRateProvider:        record.ExchangeRateProvider,
		FixerConfigured:             key != "",
		FixerMaskedSummary:          maskSecretTail(key),
		DefaultReminderOffsetsDays:  append([]int(nil), record.DefaultReminderOffsetsDays...),
		MaxReminderLeadDays:         record.MaxReminderLeadDays,
		ExchangeRateStaleAfterHours: record.ExchangeRateStaleAfterHours,
	}
}

func maskSecretTail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	return strings.Repeat("*", len(value)-4) + value[len(value)-4:]
}

func maskWebhookURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const keep = 4
	if len(value) <= keep {
		return strings.Repeat("*", len(value))
	}
	return value[:min(len(value), 24)] + "***" + value[len(value)-keep:]
}

func maskTelegramBotToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 4 {
		return strings.Repeat("*", len(token))
	}
	return strings.Repeat("*", len(token)-4) + token[len(token)-4:]
}

func decodeSettingsJSONBody(r *http.Request, dst any) error {
	return decodeJSON(r, dst)
}
