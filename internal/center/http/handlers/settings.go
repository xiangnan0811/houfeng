package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	centersettings "houfeng/internal/center/settings"
)

type SettingsRepository interface {
	GetSettings(ctx context.Context) (centersettings.CenterSettings, error)
	PutSettings(ctx context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error)
}

type settingsResponse struct {
	Telegram                 telegramSettingsResponse              `json:"telegram"`
	Feishu                   feishuSettingsResponse                `json:"feishu"`
	HostSampleFrequencyTier  string                                `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults   centersettings.ProbeFrequencyDefaults `json:"probe_frequency_defaults"`
	IncidentDefaults         centersettings.IncidentDefaults       `json:"incident_defaults"`
	OverrideRules            centersettings.OverrideRules          `json:"override_rules"`
	RetentionPolicy          centersettings.RetentionPolicy        `json:"retention_policy"`
	SubscriptionCostSettings subscriptionCostSettingsResponse      `json:"subscription_cost_settings"`
}

type telegramSettingsResponse struct {
	ChatID             string `json:"chat_id"`
	TokenPresent       bool   `json:"token_present"`
	TokenMaskedSummary string `json:"token_masked_summary,omitempty"`
	RuntimeManaged     bool   `json:"runtime_managed"`
	RuntimeApplyActive bool   `json:"runtime_apply_active"`
}

type settingsUpdateRequest struct {
	Telegram                 telegramSettingsUpdateRequest          `json:"telegram"`
	Feishu                   feishuSettingsUpdateRequest            `json:"feishu"`
	HostSampleFrequencyTier  string                                 `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults   centersettings.ProbeFrequencyDefaults  `json:"probe_frequency_defaults"`
	IncidentDefaults         centersettings.IncidentDefaults        `json:"incident_defaults"`
	OverrideRules            centersettings.OverrideRules           `json:"override_rules"`
	RetentionPolicy          centersettings.RetentionPolicy         `json:"retention_policy"`
	SubscriptionCostSettings *subscriptionCostSettingsUpdateRequest `json:"subscription_cost_settings,omitempty"`
}

type feishuSettingsResponse struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhook_url"`
}

type feishuSettingsUpdateRequest struct {
	Enabled    *bool   `json:"enabled,omitempty"`
	WebhookURL *string `json:"webhook_url,omitempty"`
}

type telegramSettingsUpdateRequest struct {
	BotToken       *string `json:"bot_token,omitempty"`
	ChatID         string  `json:"chat_id"`
	RuntimeManaged *bool   `json:"runtime_managed,omitempty"`
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

func Settings(repo SettingsRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetSettings(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, newSettingsResponse(record))
		case http.MethodPut:
			current, err := repo.GetSettings(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}

			var input settingsUpdateRequest
			if err := decodeSettingsJSONBody(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if input.Telegram.BotToken != nil && strings.TrimSpace(input.Telegram.ChatID) == "" {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.PutSettings(r.Context(), mergeSettingsUpdate(current, input))
			if err != nil {
				if errors.Is(err, centersettings.ErrInvalidSettings) {
					writeError(w, http.StatusBadRequest, "invalid input")
					return
				}
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, newSettingsResponse(record))
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func mergeSettingsUpdate(current centersettings.CenterSettings, input settingsUpdateRequest) centersettings.CenterSettings {
	merged := centersettings.CenterSettings{
		Telegram: centersettings.TelegramSettings{
			BotToken:       current.Telegram.BotToken,
			ChatID:         strings.TrimSpace(input.Telegram.ChatID),
			RuntimeManaged: current.Telegram.RuntimeManaged,
		},
		FeishuEnabled:           current.FeishuEnabled,
		FeishuWebhookURL:        current.FeishuWebhookURL,
		HostSampleFrequencyTier: input.HostSampleFrequencyTier,
		ProbeFrequencyDefaults:  input.ProbeFrequencyDefaults,
		IncidentDefaults:        input.IncidentDefaults,
		OverrideRules:           input.OverrideRules,
		RetentionPolicy:         input.RetentionPolicy,
		SubscriptionCost:        current.SubscriptionCost,
	}

	if input.Telegram.BotToken != nil {
		merged.Telegram.BotToken = strings.TrimSpace(*input.Telegram.BotToken)
	}
	if input.Telegram.RuntimeManaged != nil {
		merged.Telegram.RuntimeManaged = *input.Telegram.RuntimeManaged
	}
	if merged.Telegram.ChatID == "" {
		merged.Telegram.BotToken = ""
	}
	if input.Feishu.Enabled != nil {
		merged.FeishuEnabled = *input.Feishu.Enabled
	}
	if input.Feishu.WebhookURL != nil {
		merged.FeishuWebhookURL = strings.TrimSpace(*input.Feishu.WebhookURL)
	}
	if input.SubscriptionCostSettings != nil {
		merged.SubscriptionCost = mergeSubscriptionCostSettingsUpdate(current.SubscriptionCost, *input.SubscriptionCostSettings)
	}

	return merged
}

func newSettingsResponse(record centersettings.CenterSettings) settingsResponse {
	tokenPresent := strings.TrimSpace(record.Telegram.BotToken) != ""
	return settingsResponse{
		Telegram: telegramSettingsResponse{
			ChatID:             record.Telegram.ChatID,
			TokenPresent:       tokenPresent,
			TokenMaskedSummary: maskTelegramBotToken(record.Telegram.BotToken),
			RuntimeManaged:     record.Telegram.RuntimeManaged,
			RuntimeApplyActive: record.Telegram.RuntimeManaged && record.Telegram.Enabled(),
		},
		Feishu: feishuSettingsResponse{
			Enabled:    record.FeishuEnabled,
			WebhookURL: record.FeishuWebhookURL,
		},
		HostSampleFrequencyTier:  record.HostSampleFrequencyTier,
		ProbeFrequencyDefaults:   record.ProbeFrequencyDefaults,
		IncidentDefaults:         record.IncidentDefaults,
		OverrideRules:            record.OverrideRules,
		RetentionPolicy:          record.RetentionPolicy,
		SubscriptionCostSettings: newSubscriptionCostSettingsResponse(record.SubscriptionCost),
	}
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
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(new(struct{})); err != io.EOF {
		if err == nil {
			return errors.New("unexpected trailing data")
		}
		return err
	}
	return nil
}
