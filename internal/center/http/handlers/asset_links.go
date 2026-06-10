package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/vpsassets"
)

type linkedMonitoringInstanceCreator interface {
	CreateLinkedMonitoringInstance(context.Context, string, monitoringinstances.CreateInput, string) (monitoringinstances.Record, assetlinks.Record, error)
}

type vpsMonitoringInstanceCreateRequest struct {
	DisplayName string   `json:"display_name"`
	Group       string   `json:"group"`
	Region      string   `json:"region"`
	City        string   `json:"city"`
	Provider    string   `json:"provider"`
	Labels      []string `json:"labels"`
	Note        string   `json:"note"`
	LinkNote    string   `json:"link_note"`
}

type vpsMonitoringInstanceCreateResponse struct {
	monitoringinstances.Record
	Link assetlinks.Record `json:"link"`
}

func VPSMonitoringInstances(repo assetlinks.Repository, vpsRepo vpsassets.Repository, creator linkedMonitoringInstanceCreator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "monitoring-instances")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListMonitoringInstancesForVPS(r.Context(), vpsID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			if vpsRepo == nil || creator == nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			var request vpsMonitoringInstanceCreateRequest
			if err := decodeJSON(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			vps, err := vpsRepo.GetVPSAsset(r.Context(), vpsID)
			if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}

			input := monitoringInstanceInputFromVPS(vps, request)
			if !isValidCreateInput(input) || !isValidMetadata(input.Labels, input.Note) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			linkNote := strings.TrimSpace(request.LinkNote)
			if linkNote == "" {
				linkNote = "created from vps detail"
			}
			record, link, err := creator.CreateLinkedMonitoringInstance(r.Context(), vps.VPSID, input, linkNote)
			if errors.Is(err, assetlinks.ErrInvalidVPSMonitoringInstanceLinkInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if errors.Is(err, assetlinks.ErrVPSMonitoringInstanceLinkConflict) {
				writeError(w, http.StatusConflict, "vps monitoring instance link conflict")
				return
			}
			if errors.Is(err, assetlinks.ErrVPSActiveMonitoringInstanceExists) {
				writeError(w, http.StatusConflict, "vps active monitoring instance exists")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, vpsMonitoringInstanceCreateResponse{Record: record, Link: link})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func monitoringInstanceInputFromVPS(vps vpsassets.Record, request vpsMonitoringInstanceCreateRequest) monitoringinstances.CreateInput {
	displayName := firstNonEmpty(request.DisplayName, vps.DisplayName, vps.VPSID)
	region := firstNonEmpty(request.Region, vps.Region, vps.Country, "未确认")
	city := firstNonEmpty(request.City, vps.City, vps.Datacenter, "未确认")
	provider := firstNonEmpty(request.Provider, vps.ProviderName, "未关联服务商")
	labels := request.Labels
	if len(labels) == 0 {
		labels = vps.Labels
	}
	note := firstNonEmpty(request.Note, vps.Note)

	input := monitoringinstances.CreateInput{
		DisplayName:     displayName,
		Group:           request.Group,
		Region:          region,
		City:            city,
		Provider:        provider,
		LifecycleStatus: monitoringinstances.LifecyclePendingEnrollment,
		Labels:          labels,
		Note:            note,
	}
	input = normalizeCreateInput(input)
	input.Labels, input.Note = normalizeMetadata(input.Labels, input.Note)
	return input
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
		if errors.Is(err, assetlinks.ErrVPSActiveMonitoringInstanceExists) {
			writeError(w, http.StatusConflict, "vps active monitoring instance exists")
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
