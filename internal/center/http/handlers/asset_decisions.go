package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"houfeng/internal/center/assetdecisions"
)

type AssetDecisionRepository interface {
	GetOverview(context.Context, assetdecisions.ListFilters) (assetdecisions.Overview, error)
	ListGroups(context.Context, assetdecisions.ListFilters) ([]assetdecisions.GroupSummary, error)
	GetGroup(context.Context, string, assetdecisions.ListFilters) (assetdecisions.GroupDetail, error)
	ListRecords(context.Context) ([]assetdecisions.RecordSummary, error)
	CreateRecord(context.Context, assetdecisions.CreateRecordInput) (assetdecisions.RecordDetail, error)
	GetRecord(context.Context, string) (assetdecisions.RecordDetail, error)
	PatchRecord(context.Context, string, assetdecisions.PatchRecordInput) (assetdecisions.RecordDetail, error)
}

func AssetDecisionOverview(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		filters, err := assetDecisionFiltersFromQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		overview, err := repo.GetOverview(r.Context(), filters)
		if errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, overview)
	})
}

func AssetDecisionGroups(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		filters, err := assetDecisionFiltersFromQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		groups, err := repo.ListGroups(r.Context(), filters)
		if errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput) {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, groups)
	})
}

func AssetDecisionGroup(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		groupID := strings.TrimPrefix(r.URL.Path, "/api/asset-decisions/groups/")
		groupID = strings.Trim(groupID, "/")
		if groupID == "" || strings.Contains(groupID, "/") {
			writeError(w, http.StatusNotFound, "asset decision group not found")
			return
		}

		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		filters, err := assetDecisionFiltersFromQuery(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		}
		group, err := repo.GetGroup(r.Context(), groupID, filters)
		switch {
		case errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput):
			writeError(w, http.StatusBadRequest, "invalid input")
			return
		case errors.Is(err, assetdecisions.ErrAssetDecisionGroupNotFound):
			writeError(w, http.StatusNotFound, "asset decision group not found")
			return
		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		writeJSON(w, http.StatusOK, group)
	})
}

func AssetDecisionRecords(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			records, err := repo.ListRecords(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, records)
		case http.MethodPost:
			var input assetdecisions.CreateRecordInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			record, err := repo.CreateRecord(r.Context(), input)
			switch {
			case errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput):
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			case errors.Is(err, assetdecisions.ErrAssetDecisionGroupNotFound):
				writeError(w, http.StatusNotFound, "asset decision group not found")
				return
			case err != nil:
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusCreated, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func AssetDecisionRecord(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recordID := strings.TrimPrefix(r.URL.Path, "/api/asset-decisions/records/")
		recordID = strings.Trim(recordID, "/")
		if recordID == "" || strings.Contains(recordID, "/") {
			writeError(w, http.StatusNotFound, "asset decision record not found")
			return
		}

		switch r.Method {
		case http.MethodGet:
			record, err := repo.GetRecord(r.Context(), recordID)
			switch {
			case errors.Is(err, assetdecisions.ErrAssetDecisionRecordNotFound):
				writeError(w, http.StatusNotFound, "asset decision record not found")
				return
			case err != nil:
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, record)
		case http.MethodPatch:
			var request assetDecisionRecordPatchRequest
			if err := decodeJSON(r, &request); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			record, err := repo.PatchRecord(r.Context(), recordID, request.toInput())
			switch {
			case errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput):
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			case errors.Is(err, assetdecisions.ErrAssetDecisionRecordNotFound):
				writeError(w, http.StatusNotFound, "asset decision record not found")
				return
			case err != nil:
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, record)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func assetDecisionFiltersFromQuery(r *http.Request) (assetdecisions.ListFilters, error) {
	query := r.URL.Query()
	filters := assetdecisions.ListFilters{
		View: assetdecisions.View(strings.TrimSpace(query.Get("view"))),
	}
	if raw := strings.TrimSpace(query.Get("renew_within_days")); raw != "" {
		days, err := strconv.Atoi(raw)
		if err != nil {
			return assetdecisions.ListFilters{}, assetdecisions.ErrInvalidAssetDecisionInput
		}
		filters.RenewWithinDays = days
	}
	filters = assetdecisions.NormalizeFilters(filters)
	if err := assetdecisions.ValidateFilters(filters); err != nil {
		return assetdecisions.ListFilters{}, err
	}
	return filters, nil
}

type assetDecisionRecordPatchRequest struct {
	Title  *string                      `json:"title"`
	Goal   *string                      `json:"goal"`
	Status *assetdecisions.RecordStatus `json:"status"`
}

func (r assetDecisionRecordPatchRequest) toInput() assetdecisions.PatchRecordInput {
	var input assetdecisions.PatchRecordInput
	if r.Title != nil {
		input.Title.Set = true
		input.Title.Value = *r.Title
	}
	if r.Goal != nil {
		input.Goal.Set = true
		input.Goal.Value = *r.Goal
	}
	if r.Status != nil {
		input.Status.Set = true
		input.Status.Value = *r.Status
	}
	return input
}
