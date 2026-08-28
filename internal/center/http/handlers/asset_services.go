package handlers

import (
	"context"
	"errors"
	"net/http"

	"houfeng/internal/center/assetservices"
)

type vpsAssetServiceRepository interface {
	ListAssetServicesForVPS(context.Context, string) ([]assetservices.Record, error)
	assetservices.IdempotentRepository
}

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

func VPSServices(repo vpsAssetServiceRepository) http.Handler {
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

			key, ok := requestCreateIdempotencyKey(r)
			if !ok {
				writeInvalidCreateIdempotencyKey(w)
				return
			}

			record, replayed, err := repo.CreateAssetServiceIdempotent(r.Context(), input, key)
			if writeCreateIdempotencyError(w, err) {
				return
			}
			if handled := writeAssetServiceRepositoryError(w, err); handled {
				return
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, idempotentCreateStatus(replayed), record)
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
