package handlers

import (
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/assetlinks"
)

func VPSMonitoringInstances(repo assetlinks.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "monitoring-instances")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		records, err := repo.ListMonitoringInstancesForVPS(r.Context(), vpsID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}

func VPSLinkMonitoringInstance(repo assetlinks.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "link-monitoring-instance")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input assetlinks.LinkInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		input = assetlinks.NormalizeLinkInput(input)
		if err := assetlinks.ValidateLinkInput(input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		record, err := repo.LinkMonitoringInstance(r.Context(), vpsID, input)
		if errors.Is(err, assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkNotFound) {
			writeError(w, http.StatusNotFound, "vps or monitoring instance not found")
			return
		}
		if errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
			writeError(w, http.StatusConflict, "vps monitoring instance link conflict")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusCreated, record)
	})
}

func VPSUnlinkMonitoringInstance(repo assetlinks.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "unlink-monitoring-instance")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		var input assetlinks.UnlinkInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		input = assetlinks.NormalizeUnlinkInput(input)
		if err := assetlinks.ValidateUnlinkInput(input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}

		record, err := repo.UnlinkMonitoringInstance(r.Context(), vpsID, input)
		if errors.Is(err, assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkNotFound) {
			writeError(w, http.StatusNotFound, "vps monitoring instance link not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, record)
	})
}

func MonitoringInstanceVPS(repo assetlinks.Repository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		monitoringInstanceID, ok := parseMonitoringInstanceSubresourcePath(r.URL.Path, "vps")
		if !ok {
			writeError(w, http.StatusNotFound, "monitoring instance not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		records, err := repo.ListVPSForMonitoringInstance(r.Context(), monitoringInstanceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, records)
	})
}

func parseVPSSubresourcePath(path, wantSubresource string) (string, bool) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/vps/"), "/")
	segments := strings.Split(relative, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != wantSubresource {
		return "", false
	}
	return segments[0], true
}

func parseMonitoringInstanceSubresourcePath(path, wantSubresource string) (string, bool) {
	relative := strings.Trim(strings.TrimPrefix(path, "/api/monitoring-instances/"), "/")
	segments := strings.Split(relative, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] != wantSubresource {
		return "", false
	}
	return segments[0], true
}
