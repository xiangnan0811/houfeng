package handlers

import (
	"errors"
	"net/http"

	"houfeng/internal/center/assetservices"
)

func AssetServicesCollection(repo assetservices.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filters := assetservices.NormalizeListFilters(assetservices.ListFilters{
				VPSID:       r.URL.Query().Get("vps_id"),
				TargetID:    r.URL.Query().Get("target_id"),
				ServiceType: assetservices.ServiceType(r.URL.Query().Get("service_type")),
				Status:      assetservices.ServiceStatus(r.URL.Query().Get("status")),
			})
			if err := assetservices.ValidateListFilters(filters); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			records, err := repo.ListAssetServices(r.Context(), filters)
			if handled := writeAssetServiceRepositoryError(w, err); handled {
				return
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input assetservices.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = assetservices.NormalizeCreateInput(input)
			if err := assetservices.ValidateCreateInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateAssetService(r.Context(), input)
			if handled := writeAssetServiceRepositoryError(w, err); handled {
				return
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func VPSServices(repo assetservices.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "services")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListAssetServicesForVPS(r.Context(), vpsID)
			if handled := writeAssetServiceRepositoryError(w, err); handled {
				return
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input assetservices.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input.VPSID = vpsID
			input = assetservices.NormalizeCreateInput(input)
			if err := assetservices.ValidateCreateInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateAssetService(r.Context(), input)
			if handled := writeAssetServiceRepositoryError(w, err); handled {
				return
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func writeAssetServiceRepositoryError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, assetservices.ErrInvalidServiceInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, assetservices.ErrServiceOwnerNotFound):
		writeError(w, http.StatusNotFound, "vps asset not found")
	case errors.Is(err, assetservices.ErrServiceTargetNotFound):
		writeError(w, http.StatusNotFound, "target not found")
	case errors.Is(err, assetservices.ErrServiceNotFound):
		writeError(w, http.StatusNotFound, "asset service not found")
	default:
		return false
	}
	return true
}
