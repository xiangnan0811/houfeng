package handlers

import (
	"errors"
	"net/http"

	"houfeng/internal/center/assetdomains"
)

func AssetDomainsCollection(repo assetdomains.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filters := assetdomains.NormalizeListFilters(assetdomains.ListFilters{
				VPSID:     r.URL.Query().Get("vps_id"),
				ServiceID: r.URL.Query().Get("service_id"),
				TargetID:  r.URL.Query().Get("target_id"),
				Status:    assetdomains.DomainStatus(r.URL.Query().Get("status")),
			})
			if err := assetdomains.ValidateListFilters(filters); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			records, err := repo.ListAssetDomains(r.Context(), filters)
			if handled := writeAssetDomainRepositoryError(w, err); handled {
				return
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input assetdomains.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input = assetdomains.NormalizeCreateInput(input)
			if err := assetdomains.ValidateCreateInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateAssetDomain(r.Context(), input)
			if handled := writeAssetDomainRepositoryError(w, err); handled {
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

func VPSDomains(repo assetdomains.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "domains")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListAssetDomainsForVPS(r.Context(), vpsID)
			if handled := writeAssetDomainRepositoryError(w, err); handled {
				return
			} else if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input assetdomains.CreateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input.VPSID = vpsID
			input = assetdomains.NormalizeCreateInput(input)
			if err := assetdomains.ValidateCreateInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateAssetDomain(r.Context(), input)
			if handled := writeAssetDomainRepositoryError(w, err); handled {
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

func writeAssetDomainRepositoryError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, assetdomains.ErrInvalidDomainInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, assetdomains.ErrDomainOwnerNotFound):
		writeError(w, http.StatusNotFound, "vps asset not found")
	case errors.Is(err, assetdomains.ErrDomainServiceNotFound):
		writeError(w, http.StatusNotFound, "asset service not found")
	case errors.Is(err, assetdomains.ErrDomainTargetNotFound):
		writeError(w, http.StatusNotFound, "target not found")
	case errors.Is(err, assetdomains.ErrDomainConflict):
		writeError(w, http.StatusConflict, "asset domain conflict")
	case errors.Is(err, assetdomains.ErrDomainNotFound):
		writeError(w, http.StatusNotFound, "asset domain not found")
	default:
		return false
	}
	return true
}
