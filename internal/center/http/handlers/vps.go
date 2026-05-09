package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/vpsassets"
)

func VPSCollection(repo vpsassets.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filters := vpsassets.NormalizeListFilters(vpsassets.ListFilters{
				ProviderID:      r.URL.Query().Get("provider_id"),
				LifecycleStatus: vpsassets.LifecycleStatus(r.URL.Query().Get("lifecycle_status")),
				UsageStatus:     vpsassets.UsageStatus(r.URL.Query().Get("usage_status")),
				RenewalDecision: vpsassets.RenewalDecision(r.URL.Query().Get("renewal_decision")),
			})
			if err := vpsassets.ValidateListFilters(filters); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			records, err := repo.ListVPSAssets(r.Context(), filters)
			if errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input vpsassets.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = vpsassets.NormalizeCreateInput(input)
			if err := vpsassets.ValidateCreateInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateVPSAsset(r.Context(), input)
			if errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
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

func VPSItem(repo vpsassets.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID := strings.TrimPrefix(r.URL.Path, "/api/vps/")
		vpsID = strings.Trim(vpsID, "/")
		if vpsID == "" || strings.Contains(vpsID, "/") {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetVPSAsset(r.Context(), vpsID)
			if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, record)
		case http.MethodPatch:
			var input vpsassets.PatchInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = vpsassets.NormalizePatchInput(input)
			if err := vpsassets.ValidatePatchInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.PatchVPSAsset(r.Context(), vpsID, input)
			if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
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
