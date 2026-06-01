package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/targets"
	"houfeng/internal/center/vpsassets"
)

type AssetLifecycleRepository interface {
	GetVPSCancellationPreview(context.Context, string) (assetlifecycle.CancellationPreview, error)
	ApplyVPSCancellation(context.Context, string, assetlifecycle.ApplyCancellationInput) (assetlifecycle.LifecycleActionResult, error)
	ListMonitoringInstanceAssetContexts(context.Context) ([]assetlifecycle.AssetContextForMonitoringInstance, error)
	ListTargetAssetContexts(context.Context) ([]assetlifecycle.AssetContextForTarget, error)
}

func VPSCancellationPreview(repo AssetLifecycleRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "cancellation-preview")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		preview, err := repo.GetVPSCancellationPreview(r.Context(), vpsID)
		if handled := writeAssetLifecycleError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, preview)
	})
}

func VPSCancellation(repo AssetLifecycleRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "cancellation")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input assetlifecycle.ApplyCancellationInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		input = assetlifecycle.NormalizeApplyCancellationInput(input)
		if err := assetlifecycle.ValidateApplyCancellationInput(input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		result, err := repo.ApplyVPSCancellation(r.Context(), vpsID, input)
		if handled := writeAssetLifecycleError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func AssetContextMonitoringInstances(repo AssetLifecycleRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Trim(r.URL.Path, "/") != "api/asset-context/monitoring-instances" {
			writeError(w, http.StatusNotFound, "asset context not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		records, err := repo.ListMonitoringInstanceAssetContexts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}

func AssetContextTargets(repo AssetLifecycleRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Trim(r.URL.Path, "/") != "api/asset-context/targets" {
			writeError(w, http.StatusNotFound, "asset context not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		records, err := repo.ListTargetAssetContexts(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}

func writeAssetLifecycleError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked):
		writeError(w, http.StatusConflict, "lifecycle action blocked")
	case errors.Is(err, vpsassets.ErrVPSAssetNotFound):
		writeError(w, http.StatusNotFound, "vps asset not found")
	case errors.Is(err, subscriptions.ErrSubscriptionNotFound):
		writeError(w, http.StatusNotFound, "subscription not found")
	case errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound):
		writeError(w, http.StatusNotFound, "monitoring instance not found")
	case errors.Is(err, targets.ErrTargetNotFound):
		writeError(w, http.StatusNotFound, "target not found")
	case errors.Is(err, vpsassets.ErrInvalidVPSAssetInput), errors.Is(err, subscriptions.ErrInvalidSubscriptionInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	default:
		return false
	}
	return true
}
