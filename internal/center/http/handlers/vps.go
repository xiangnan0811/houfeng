package handlers

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/renewals"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

type renewalSubscriptionLinker interface {
	PatchVPSAssetWithSubscriptionRenewalLinkage(context.Context, string, vpsassets.PatchInput) (vpsassets.Record, vpsassets.RenewalSubscriptionLinkage, error)
}

type vpsPatchResponse struct {
	vpsassets.Record
	RenewalSubscriptionLinkage *vpsassets.RenewalSubscriptionLinkage `json:"renewal_subscription_linkage,omitempty"`
}

func VPSCollection(repo vpsassets.Repository, linkRepos ...assetlinks.Repository) http.Handler {
	var linkRepo assetlinks.Repository
	if len(linkRepos) > 0 {
		linkRepo = linkRepos[0]
	}

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
			if linkRepo != nil {
				for i := range records {
					count, err := linkRepo.CountActiveLinksForVPS(r.Context(), records[i].VPSID)
					if err != nil {
						writeError(w, http.StatusInternalServerError, "internal server error")
						return
					}
					records[i].ActiveNodeLinkCount = count
				}
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
			if linkRepo != nil {
				count, err := linkRepo.CountActiveLinksForVPS(r.Context(), record.VPSID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				record.ActiveNodeLinkCount = count
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func VPSItem(repo vpsassets.Repository, linkRepos ...assetlinks.Repository) http.Handler {
	var linkRepo assetlinks.Repository
	if len(linkRepos) > 0 {
		linkRepo = linkRepos[0]
	}

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
			if linkRepo != nil {
				nodeLinks, err := linkRepo.ListNodesForVPS(r.Context(), vpsID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				record.ActiveNodeLinkCount = len(nodeLinks)
				writeJSON(w, http.StatusOK, vpsDetailResponse{Record: record, NodeLinks: nodeLinks})
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

			var linkage *vpsassets.RenewalSubscriptionLinkage
			var record vpsassets.Record
			var err error
			if input.RenewalDecision.Set && vpsassets.IsCancellationRenewalDecision(input.RenewalDecision.Value) {
				if linker, ok := repo.(renewalSubscriptionLinker); ok {
					var linked vpsassets.RenewalSubscriptionLinkage
					record, linked, err = linker.PatchVPSAssetWithSubscriptionRenewalLinkage(r.Context(), vpsID, input)
					linkage = &linked
				} else {
					record, err = repo.PatchVPSAsset(r.Context(), vpsID, input)
				}
			} else {
				record, err = repo.PatchVPSAsset(r.Context(), vpsID, input)
			}
			if errors.Is(err, vpsassets.ErrVPSAssetNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) || errors.Is(err, subscriptions.ErrInvalidSubscriptionInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if errors.Is(err, subscriptions.ErrSubscriptionNotFound) {
				writeError(w, http.StatusNotFound, "subscription not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			if linkRepo != nil {
				count, err := linkRepo.CountActiveLinksForVPS(r.Context(), record.VPSID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, "internal server error")
					return
				}
				record.ActiveNodeLinkCount = count
			}
			if linkage != nil {
				writeJSON(w, http.StatusOK, vpsPatchResponse{Record: record, RenewalSubscriptionLinkage: linkage})
				return
			}
			writeJSON(w, http.StatusOK, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

type vpsDetailResponse struct {
	vpsassets.Record
	NodeLinks []assetlinks.NodeSummary `json:"node_links"`
}

func VPSTimeline(repo renewals.TimelineRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "timeline")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		timeline, err := repo.GetVPSTimeline(r.Context(), vpsID)
		if errors.Is(err, renewals.ErrInvalidRenewalDecisionInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if errors.Is(err, renewals.ErrRenewalTimelineNotFound) {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, timeline)
	})
}

func VPSExperienceLogs(repo renewals.ExperienceLogRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vpsID, ok := parseVPSSubresourcePath(r.URL.Path, "experience-logs")
		if !ok {
			writeError(w, http.StatusNotFound, "vps asset not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListExperienceLogsForVPS(r.Context(), vpsID)
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input renewals.CreateExperienceLogInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}

			input.VPSID = vpsID
			input = renewals.NormalizeCreateExperienceLogInput(input)
			if err := renewals.ValidateCreateExperienceLogInput(input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}

			record, err := repo.CreateExperienceLog(r.Context(), input)
			if errors.Is(err, renewals.ErrInvalidAssetHistoryInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if errors.Is(err, renewals.ErrAssetTimelineNotFound) {
				writeError(w, http.StatusNotFound, "vps asset not found")
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
