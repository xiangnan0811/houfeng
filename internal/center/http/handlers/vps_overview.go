package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/vpsoverview"
)

type vpsOverviewService interface {
	Get(context.Context, vpsoverview.Request) (vpsoverview.Overview, error)
}

// VPSOverview serves GET /api/vps/{id}/overview.
func VPSOverview(service vpsOverviewService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPrivateCacheControl)
		actor, ok := sessionctx.ActorScopeFromContext(request.Context())
		if !ok {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		if service == nil {
			writeRecordError(w, http.StatusServiceUnavailable, "overview_unavailable", "vps overview is unavailable", nil)
			return
		}
		if request.Method != http.MethodGet {
			writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
			return
		}
		vpsID, err := vpsIDFromOverviewPath(request.URL.Path)
		if err != nil {
			writeRecordError(w, http.StatusNotFound, "resource_not_found", "vps not found", nil)
			return
		}
		overview, err := service.Get(request.Context(), vpsoverview.Request{Actor: actor, VPSID: vpsID})
		if err != nil {
			writeVPSOverviewError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, overview)
	})
}

func vpsIDFromOverviewPath(path string) (string, error) {
	const prefix = "/api/vps/"
	const suffix = "/overview"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", vpsoverview.ErrVPSNotFound
	}
	vpsID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if vpsID == "" || strings.Contains(vpsID, "/") {
		return "", vpsoverview.ErrVPSNotFound
	}
	return vpsID, nil
}

func writeVPSOverviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, vpsoverview.ErrVPSNotFound):
		writeRecordError(w, http.StatusNotFound, "resource_not_found", "vps not found", nil)
	case errors.Is(err, vpsoverview.ErrInvalidOverviewRequest):
		writeRecordError(w, http.StatusBadRequest, "query_invalid", "invalid overview request", nil)
	default:
		writeRecordError(w, http.StatusInternalServerError, "internal_error", "internal error", nil)
	}
}
