package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	centersettings "houfeng/internal/center/settings"
)

type SettingsRepository interface {
	GetSettings(ctx context.Context) (centersettings.CenterSettings, error)
	PutSettings(ctx context.Context, input centersettings.CenterSettings) (centersettings.CenterSettings, error)
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
			writeJSON(w, http.StatusOK, record)
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
			writeJSON(w, http.StatusOK, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
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
