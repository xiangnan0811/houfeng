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
	Telegram                telegramSettingsResponse              `json:"telegram"`
	HostSampleFrequencyTier string                                `json:"host_sample_frequency_tier"`
	ProbeFrequencyDefaults  centersettings.ProbeFrequencyDefaults `json:"probe_frequency_defaults"`
	IncidentDefaults        centersettings.IncidentDefaults       `json:"incident_defaults"`
	OverrideRules           centersettings.OverrideRules          `json:"override_rules"`
	RetentionPolicy         centersettings.RetentionPolicy        `json:"retention_policy"`
}

type telegramSettingsResponse struct {
	ChatID             string `json:"chat_id"`
	TokenPresent       bool   `json:"token_present"`
	TokenMaskedSummary string `json:"token_masked_summary,omitempty"`
	RuntimeApplyActive bool   `json:"runtime_apply_active"`
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
			var input centersettings.CenterSettings
			if err := decodeSettingsJSONBody(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			record, err := repo.PutSettings(r.Context(), input)
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

func newSettingsResponse(record centersettings.CenterSettings) settingsResponse {
	tokenPresent := strings.TrimSpace(record.Telegram.BotToken) != ""
	return settingsResponse{
		Telegram: telegramSettingsResponse{
			ChatID:             record.Telegram.ChatID,
			TokenPresent:       tokenPresent,
			TokenMaskedSummary: maskTelegramBotToken(record.Telegram.BotToken),
			RuntimeApplyActive: false,
		},
		HostSampleFrequencyTier: record.HostSampleFrequencyTier,
		ProbeFrequencyDefaults:  record.ProbeFrequencyDefaults,
		IncidentDefaults:        record.IncidentDefaults,
		OverrideRules:           record.OverrideRules,
		RetentionPolicy:         record.RetentionPolicy,
	}
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
