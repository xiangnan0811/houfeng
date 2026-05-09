package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/providers"
)

func ProvidersCollection(repo providers.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListProviders(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input providers.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = providers.NormalizeCreateInput(input)
			if err := providers.ValidateCreateInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateProvider(r.Context(), input)
			if errors.Is(err, providers.ErrInvalidProviderInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func ProviderItem(repo providers.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerID := strings.TrimPrefix(r.URL.Path, "/api/providers/")
		providerID = strings.Trim(providerID, "/")
		if providerID == "" || strings.Contains(providerID, "/") {
			writeError(w, http.StatusNotFound, "provider not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetProvider(r.Context(), providerID)
			if errors.Is(err, providers.ErrProviderNotFound) {
				writeError(w, http.StatusNotFound, "provider not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, record)
		case http.MethodPatch:
			var input providers.PatchInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = providers.NormalizePatchInput(input)
			if err := providers.ValidatePatchInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.PatchProvider(r.Context(), providerID, input)
			if errors.Is(err, providers.ErrProviderNotFound) {
				writeError(w, http.StatusNotFound, "provider not found")
				return
			}
			if errors.Is(err, providers.ErrInvalidProviderInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}
