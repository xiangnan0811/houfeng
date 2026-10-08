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
	ExtendVPSValidity(context.Context, string, assetlifecycle.ExtendValidityInput) (assetlifecycle.LifecycleActionResult, error)
	GetVPSArchiveReview(context.Context, string) (assetlifecycle.ArchiveReview, error)
	ApplyVPSArchive(context.Context, string, assetlifecycle.ApplyArchiveInput) (assetlifecycle.ArchiveReview, error)
	RestoreVPSFromArchive(context.Context, string, assetlifecycle.RestoreArchiveInput) (vpsassets.Record, error)
	ListTargetAssetContexts(context.Context) ([]assetlifecycle.AssetContextForTarget, error)
}

func VPSExtendValidity(repo AssetLifecycleRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "extend-validity")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input assetlifecycle.ExtendValidityInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		input = assetlifecycle.NormalizeExtendValidityInput(input)
		if err := assetlifecycle.ValidateExtendValidityInput(input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		result, err := repo.ExtendVPSValidity(r.Context(), vpsID, input)
		if handled := writeAssetLifecycleError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

func VPSArchiveReview(repo AssetLifecycleRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "archive-review")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		review, err := repo.GetVPSArchiveReview(r.Context(), vpsID)
		if handled := writeAssetLifecycleError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, review)
	})
}

func VPSArchive(repo AssetLifecycleRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "archive")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input assetlifecycle.ApplyArchiveInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		input = assetlifecycle.NormalizeApplyArchiveInput(input)
		if err := assetlifecycle.ValidateApplyArchiveInput(input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		review, err := repo.ApplyVPSArchive(r.Context(), vpsID, input)
		if handled := writeAssetLifecycleError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, review)
	})
}

func VPSRestoreFromArchive(repo AssetLifecycleRepository, refresher ExchangeRateRefreshRequester) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "restore-from-archive")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input assetlifecycle.RestoreArchiveInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json body")
			return
		}
		if err := assetlifecycle.ValidateLifecycleReason(input.Reason); err != nil {
			writeAssetLifecycleError(w, err)
			return
		}
		record, err := repo.RestoreVPSFromArchive(r.Context(), vpsID, input)
		if handled := writeAssetLifecycleError(w, err); handled {
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if refresher != nil {
			refresher.RequestRefresh(false)
		}
		writeJSON(w, http.StatusOK, record)
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
	var stale *assetlifecycle.StaleArchivePreviewError
	if errors.As(err, &stale) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "archive preview stale", "code": "archive_preview_stale", "review": stale.Review})
		return true
	}
	var archiveBlocked *assetlifecycle.ArchiveBlockedError
	if errors.As(err, &archiveBlocked) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "lifecycle action blocked", "code": "lifecycle_action_blocked", "review": archiveBlocked.Review})
		return true
	}
	switch {
	case err == nil:
		return false
	case errors.Is(err, assetlifecycle.ErrArchiveIdempotencyConflict):
		writeCodedError(w, http.StatusConflict, "archive idempotency key reused", "archive_idempotency_conflict")
	case errors.Is(err, assetlifecycle.ErrInvalidLifecycleActionInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, assetlifecycle.ErrLifecycleActionBlocked):
		writeCodedError(w, http.StatusConflict, "lifecycle action blocked", "lifecycle_action_blocked")
	case errors.Is(err, assetlifecycle.ErrSharedImpactConfirmationRequired):
		writeCodedError(w, http.StatusConflict, "shared impact confirmation required", "shared_impact_confirmation_required")
	case errors.Is(err, assetlifecycle.ErrStaleCancellationPreview):
		writeCodedError(w, http.StatusConflict, "cancellation preview stale", "cancellation_preview_stale")
	case errors.Is(err, assetlifecycle.ErrRetryableLifecycleConflict):
		writeCodedError(w, http.StatusConflict, "lifecycle transaction conflict", "lifecycle_transaction_conflict")
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
