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
	ListManualGroups(context.Context, assetdecisions.ListFilters) ([]assetdecisions.ManualGroupSummary, error)
	CreateManualGroup(context.Context, assetdecisions.CreateManualGroupInput) (assetdecisions.ManualGroupDetail, error)
	GetManualGroup(context.Context, string) (assetdecisions.ManualGroupDetail, error)
	PatchManualGroup(context.Context, string, assetdecisions.PatchManualGroupInput) (assetdecisions.ManualGroupDetail, error)
	AddManualGroupMember(context.Context, string, assetdecisions.CreateManualGroupMemberInput) (assetdecisions.ManualGroupDetail, error)
	PatchManualGroupMember(context.Context, string, string, assetdecisions.PatchManualGroupMemberInput) (assetdecisions.ManualGroupDetail, error)
	DeleteManualGroupMember(context.Context, string, string) (assetdecisions.ManualGroupDetail, error)
	ListScenarioTemplates(context.Context) ([]assetdecisions.ScenarioTemplateSummary, error)
	CreateScenarioTemplate(context.Context, assetdecisions.CreateScenarioTemplateInput) (assetdecisions.ScenarioTemplateDetail, error)
	GetScenarioTemplate(context.Context, string) (assetdecisions.ScenarioTemplateDetail, error)
	PatchScenarioTemplate(context.Context, string, assetdecisions.PatchScenarioTemplateInput) (assetdecisions.ScenarioTemplateDetail, error)
	CreateManualGroupFromTemplate(context.Context, string, assetdecisions.CreateManualGroupFromTemplateInput) (assetdecisions.ManualGroupDetail, error)
	ListRecords(context.Context, assetdecisions.ListFilters) ([]assetdecisions.RecordSummary, error)
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

func AssetDecisionManualGroups(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filters, err := assetDecisionFiltersFromQuery(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			groups, err := repo.ListManualGroups(r.Context(), filters)
			if errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, groups)
		case http.MethodPost:
			var input assetdecisions.CreateManualGroupInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			group, err := repo.CreateManualGroup(r.Context(), input)
			writeManualGroupResult(w, group, err, http.StatusCreated)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func AssetDecisionManualGroup(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := assetDecisionManualGroupPathParts(r.URL.Path)
		if len(parts) == 0 || parts[0] == "" {
			writeError(w, http.StatusNotFound, "asset decision manual group not found")
			return
		}
		manualGroupID := parts[0]
		switch len(parts) {
		case 1:
			handleAssetDecisionManualGroupItem(w, r, repo, manualGroupID)
		case 2:
			if parts[1] != "members" {
				writeError(w, http.StatusNotFound, "asset decision manual group not found")
				return
			}
			handleAssetDecisionManualGroupMembers(w, r, repo, manualGroupID)
		case 3:
			if parts[1] != "members" || parts[2] == "" {
				writeError(w, http.StatusNotFound, "asset decision manual group member not found")
				return
			}
			handleAssetDecisionManualGroupMember(w, r, repo, manualGroupID, parts[2])
		default:
			writeError(w, http.StatusNotFound, "asset decision manual group not found")
		}
	})
}

func AssetDecisionRecords(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			filters, err := assetDecisionFiltersFromQuery(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
			records, err := repo.ListRecords(r.Context(), filters)
			if errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput) {
				writeError(w, http.StatusBadRequest, "invalid input")
				return
			}
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
			case errors.Is(err, assetdecisions.ErrAssetDecisionManualGroupNotFound):
				writeError(w, http.StatusNotFound, "asset decision manual group not found")
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

func AssetDecisionScenarioTemplates(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			templates, err := repo.ListScenarioTemplates(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			writeJSON(w, http.StatusOK, templates)
		case http.MethodPost:
			var input assetdecisions.CreateScenarioTemplateInput
			if err := decodeJSON(r, &input); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			template, err := repo.CreateScenarioTemplate(r.Context(), input)
			writeScenarioTemplateResult(w, template, err, http.StatusCreated)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func AssetDecisionScenarioTemplate(repo AssetDecisionRepository) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := assetDecisionScenarioTemplatePathParts(r.URL.Path)
		if len(parts) == 0 || parts[0] == "" {
			writeError(w, http.StatusNotFound, "asset decision scenario template not found")
			return
		}
		templateID := parts[0]
		switch len(parts) {
		case 1:
			handleAssetDecisionScenarioTemplateItem(w, r, repo, templateID)
		case 2:
			if parts[1] != "manual-groups" {
				writeError(w, http.StatusNotFound, "asset decision scenario template not found")
				return
			}
			handleAssetDecisionScenarioTemplateManualGroups(w, r, repo, templateID)
		default:
			writeError(w, http.StatusNotFound, "asset decision scenario template not found")
		}
	})
}

func handleAssetDecisionManualGroupItem(w http.ResponseWriter, r *http.Request, repo AssetDecisionRepository, manualGroupID string) {
	switch r.Method {
	case http.MethodGet:
		group, err := repo.GetManualGroup(r.Context(), manualGroupID)
		writeManualGroupResult(w, group, err, http.StatusOK)
	case http.MethodPatch:
		var request assetDecisionManualGroupPatchRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		group, err := repo.PatchManualGroup(r.Context(), manualGroupID, request.toInput())
		writeManualGroupResult(w, group, err, http.StatusOK)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleAssetDecisionManualGroupMembers(w http.ResponseWriter, r *http.Request, repo AssetDecisionRepository, manualGroupID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input assetdecisions.CreateManualGroupMemberInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	group, err := repo.AddManualGroupMember(r.Context(), manualGroupID, input)
	writeManualGroupResult(w, group, err, http.StatusCreated)
}

func handleAssetDecisionManualGroupMember(w http.ResponseWriter, r *http.Request, repo AssetDecisionRepository, manualGroupID, vpsID string) {
	switch r.Method {
	case http.MethodPatch:
		var request assetDecisionManualGroupMemberPatchRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		group, err := repo.PatchManualGroupMember(r.Context(), manualGroupID, vpsID, request.toInput())
		writeManualGroupResult(w, group, err, http.StatusOK)
	case http.MethodDelete:
		group, err := repo.DeleteManualGroupMember(r.Context(), manualGroupID, vpsID)
		writeManualGroupResult(w, group, err, http.StatusOK)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func writeManualGroupResult(w http.ResponseWriter, group assetdecisions.ManualGroupDetail, err error, status int) {
	switch {
	case errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, assetdecisions.ErrAssetDecisionGroupNotFound):
		writeError(w, http.StatusNotFound, "asset decision group not found")
	case errors.Is(err, assetdecisions.ErrAssetDecisionManualGroupNotFound):
		writeError(w, http.StatusNotFound, "asset decision manual group not found")
	case errors.Is(err, assetdecisions.ErrAssetDecisionManualGroupMemberNotFound):
		writeError(w, http.StatusNotFound, "asset decision manual group member not found")
	case errors.Is(err, assetdecisions.ErrAssetDecisionScenarioTemplateNotFound):
		writeError(w, http.StatusNotFound, "asset decision scenario template not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal server error")
	default:
		writeJSON(w, status, group)
	}
}

func handleAssetDecisionScenarioTemplateItem(w http.ResponseWriter, r *http.Request, repo AssetDecisionRepository, templateID string) {
	switch r.Method {
	case http.MethodGet:
		template, err := repo.GetScenarioTemplate(r.Context(), templateID)
		writeScenarioTemplateResult(w, template, err, http.StatusOK)
	case http.MethodPatch:
		var request assetDecisionScenarioTemplatePatchRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		template, err := repo.PatchScenarioTemplate(r.Context(), templateID, request.toInput())
		writeScenarioTemplateResult(w, template, err, http.StatusOK)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleAssetDecisionScenarioTemplateManualGroups(w http.ResponseWriter, r *http.Request, repo AssetDecisionRepository, templateID string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var input assetdecisions.CreateManualGroupFromTemplateInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	group, err := repo.CreateManualGroupFromTemplate(r.Context(), templateID, input)
	writeManualGroupResult(w, group, err, http.StatusCreated)
}

func writeScenarioTemplateResult(w http.ResponseWriter, template assetdecisions.ScenarioTemplateDetail, err error, status int) {
	switch {
	case errors.Is(err, assetdecisions.ErrInvalidAssetDecisionInput):
		writeError(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, assetdecisions.ErrAssetDecisionManualGroupNotFound):
		writeError(w, http.StatusNotFound, "asset decision manual group not found")
	case errors.Is(err, assetdecisions.ErrAssetDecisionScenarioTemplateNotFound):
		writeError(w, http.StatusNotFound, "asset decision scenario template not found")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal server error")
	default:
		writeJSON(w, status, template)
	}
}

func assetDecisionManualGroupPathParts(path string) []string {
	rest := strings.TrimPrefix(path, "/api/asset-decisions/manual-groups/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}

func assetDecisionScenarioTemplatePathParts(path string) []string {
	rest := strings.TrimPrefix(path, "/api/asset-decisions/scenario-templates/")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
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
		View:       assetdecisions.View(strings.TrimSpace(query.Get("view"))),
		ProviderID: strings.TrimSpace(query.Get("provider_id")),
		VPSID:      strings.TrimSpace(query.Get("vps_id")),
		Country:    strings.TrimSpace(query.Get("country")),
		Region:     strings.TrimSpace(query.Get("region")),
		City:       strings.TrimSpace(query.Get("city")),
		Scenario:   assetdecisions.ManualGroupScenario(strings.TrimSpace(query.Get("scenario"))),
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
	Title   *string                                 `json:"title"`
	Goal    *string                                 `json:"goal"`
	Status  *assetdecisions.RecordStatus            `json:"status"`
	Members []assetDecisionRecordMemberPatchRequest `json:"members"`
}

type assetDecisionScenarioTemplatePatchRequest struct {
	Status   *assetdecisions.ScenarioTemplateStatus `json:"status"`
	Scenario *assetdecisions.ManualGroupScenario    `json:"scenario"`
	Title    *string                                `json:"title"`
	Goal     *string                                `json:"goal"`
	Note     *string                                `json:"note"`
}

func (r assetDecisionScenarioTemplatePatchRequest) toInput() assetdecisions.PatchScenarioTemplateInput {
	var input assetdecisions.PatchScenarioTemplateInput
	if r.Status != nil {
		input.Status.Set = true
		input.Status.Value = *r.Status
	}
	if r.Scenario != nil {
		input.Scenario.Set = true
		input.Scenario.Value = *r.Scenario
	}
	if r.Title != nil {
		input.Title.Set = true
		input.Title.Value = *r.Title
	}
	if r.Goal != nil {
		input.Goal.Set = true
		input.Goal.Value = *r.Goal
	}
	if r.Note != nil {
		input.Note.Set = true
		input.Note.Value = *r.Note
	}
	return input
}

type assetDecisionRecordMemberPatchRequest struct {
	VPSID          string                         `json:"vps_id"`
	FollowupStatus *assetdecisions.FollowupStatus `json:"followup_status"`
	FollowupNote   *string                        `json:"followup_note"`
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
	for _, member := range r.Members {
		next := assetdecisions.PatchRecordMemberInput{
			VPSID: member.VPSID,
		}
		if member.FollowupStatus != nil {
			next.FollowupStatus.Set = true
			next.FollowupStatus.Value = *member.FollowupStatus
		}
		if member.FollowupNote != nil {
			next.FollowupNote.Set = true
			next.FollowupNote.Value = *member.FollowupNote
		}
		input.Members = append(input.Members, next)
	}
	return input
}

type assetDecisionManualGroupPatchRequest struct {
	Status   *assetdecisions.ManualGroupStatus   `json:"status"`
	Scenario *assetdecisions.ManualGroupScenario `json:"scenario"`
	Title    *string                             `json:"title"`
	Goal     *string                             `json:"goal"`
	Note     *string                             `json:"note"`
}

func (r assetDecisionManualGroupPatchRequest) toInput() assetdecisions.PatchManualGroupInput {
	var input assetdecisions.PatchManualGroupInput
	if r.Status != nil {
		input.Status.Set = true
		input.Status.Value = *r.Status
	}
	if r.Scenario != nil {
		input.Scenario.Set = true
		input.Scenario.Value = *r.Scenario
	}
	if r.Title != nil {
		input.Title.Set = true
		input.Title.Value = *r.Title
	}
	if r.Goal != nil {
		input.Goal.Set = true
		input.Goal.Value = *r.Goal
	}
	if r.Note != nil {
		input.Note.Set = true
		input.Note.Value = *r.Note
	}
	return input
}

type assetDecisionManualGroupMemberPatchRequest struct {
	IntendedRole   *assetdecisions.SuggestedRole   `json:"intended_role"`
	IntendedAction *assetdecisions.SuggestedAction `json:"intended_action"`
	Reason         *string                         `json:"reason"`
	Note           *string                         `json:"note"`
	SortOrder      *int                            `json:"sort_order"`
}

func (r assetDecisionManualGroupMemberPatchRequest) toInput() assetdecisions.PatchManualGroupMemberInput {
	var input assetdecisions.PatchManualGroupMemberInput
	if r.IntendedRole != nil {
		input.IntendedRole.Set = true
		input.IntendedRole.Value = *r.IntendedRole
	}
	if r.IntendedAction != nil {
		input.IntendedAction.Set = true
		input.IntendedAction.Value = *r.IntendedAction
	}
	if r.Reason != nil {
		input.Reason.Set = true
		input.Reason.Value = *r.Reason
	}
	if r.Note != nil {
		input.Note.Set = true
		input.Note.Value = *r.Note
	}
	if r.SortOrder != nil {
		input.SortOrder.Set = true
		input.SortOrder.Value = *r.SortOrder
	}
	return input
}
