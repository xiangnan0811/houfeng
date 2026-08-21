package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
)

const recordPortabilityPrivateCache = "private, no-store"

var recordPortabilityIdempotencyPattern = regexp.MustCompile(`^[a-z0-9._:-]{1,128}$`)

type recordPortabilityApplication interface {
	Preview(context.Context, portability.PreviewRequest) (portability.Preview, error)
	Create(context.Context, portability.CreateRequest) (portability.ExportView, error)
	Get(context.Context, recordauth.ActorScope, string) (portability.ExportView, error)
	OpenContent(context.Context, recordauth.ActorScope, string) (portability.Content, error)
	DryRun(context.Context, portability.DryRunRequest) (portability.ImportPlanView, error)
	Apply(context.Context, portability.ApplyRequest) (portability.ApplyResult, error)
}

func RecordPortability(application recordPortabilityApplication) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", recordPortabilityPrivateCache)
		actor, ok := sessionctx.ActorScopeFromContext(request.Context())
		if !ok {
			writeRecordError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable", nil)
			return
		}
		if application == nil {
			writeRecordNotFound(w)
			return
		}
		switch {
		case request.URL.Path == "/api/record-export-previews":
			handleRecordExportPreview(w, request, actor, application)
		case request.URL.Path == "/api/record-exports":
			handleRecordExportCreate(w, request, actor, application)
		case request.URL.Path == "/api/record-imports/dry-run":
			handleRecordImportDryRun(w, request, actor, application)
		default:
			if planID, ok := recordImportApplyPath(request.URL.Path); ok {
				handleRecordImportApply(w, request, actor, planID, application)
				return
			}
			exportID, content, ok := recordExportPath(request.URL.Path)
			if !ok {
				writeRecordNotFound(w)
				return
			}
			if content {
				handleRecordExportContent(w, request, actor, exportID, application)
				return
			}
			handleRecordExportGet(w, request, actor, exportID, application)
		}
	})
}

type recordExportPreviewInput struct {
	RecordID        string `json:"record_id"`
	RevisionID      string `json:"revision_id,omitempty"`
	SnapshotID      string `json:"snapshot_id,omitempty"`
	ExportKind      string `json:"export_kind"`
	ExportMode      string `json:"export_mode"`
	IncludeActivity bool   `json:"include_activity,omitempty"`
}

type recordExportCreateInput struct {
	PreviewID       string `json:"preview_id"`
	PreviewToken    string `json:"preview_token"`
	InventoryDigest string `json:"inventory_digest"`
	ConfirmToken    string `json:"confirm_token,omitempty"`
}

func handleRecordExportPreview(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordPortabilityApplication,
) {
	if request.Method != http.MethodPost {
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	key, ok := recordPortabilityIdempotencyKey(request)
	if !ok {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required", nil)
		return
	}
	var input recordExportPreviewInput
	if !decodeRecordsRequestJSON(w, request, &input) {
		return
	}
	preview, err := application.Preview(request.Context(), portability.PreviewRequest{
		Actor:           actor,
		IdempotencyKey:  key,
		RecordID:        input.RecordID,
		RevisionID:      input.RevisionID,
		SnapshotID:      input.SnapshotID,
		ExportKind:      input.ExportKind,
		ExportMode:      input.ExportMode,
		IncludeActivity: input.IncludeActivity,
	})
	if err != nil {
		writeRecordPortabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func handleRecordExportCreate(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordPortabilityApplication,
) {
	if request.Method != http.MethodPost {
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	key, ok := recordPortabilityIdempotencyKey(request)
	if !ok {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required", nil)
		return
	}
	var input recordExportCreateInput
	if !decodeRecordsRequestJSON(w, request, &input) {
		return
	}
	view, err := application.Create(request.Context(), portability.CreateRequest{
		Actor:           actor,
		IdempotencyKey:  key,
		PreviewID:       input.PreviewID,
		PreviewToken:    input.PreviewToken,
		InventoryDigest: input.InventoryDigest,
		ConfirmToken:    input.ConfirmToken,
	})
	if err != nil {
		writeRecordPortabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

func handleRecordExportGet(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	exportID string,
	application recordPortabilityApplication,
) {
	if request.Method != http.MethodGet {
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	view, err := application.Get(request.Context(), actor, exportID)
	if err != nil {
		writeRecordPortabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func handleRecordExportContent(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	exportID string,
	application recordPortabilityApplication,
) {
	if request.Method != http.MethodGet {
		writeRecordError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	content, err := application.OpenContent(request.Context(), actor, exportID)
	if err != nil {
		writeRecordPortabilityError(w, err)
		return
	}
	defer content.Body.Close()
	w.Header().Set("Content-Type", content.MediaType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+content.Filename+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content.Body)
}

func recordExportPath(path string) (exportID string, content bool, ok bool) {
	const prefix = "/api/record-exports/"
	if !strings.HasPrefix(path, prefix) {
		return "", false, false
	}
	trimmed := strings.TrimPrefix(path, prefix)
	if trimmed == "" {
		return "", false, false
	}
	parts := strings.Split(trimmed, "/")
	if !strings.HasPrefix(parts[0], "rej_") {
		return "", false, false
	}
	switch len(parts) {
	case 1:
		return parts[0], false, true
	case 2:
		if parts[1] == "content" {
			return parts[0], true, true
		}
	}
	return "", false, false
}

func recordPortabilityIdempotencyKey(request *http.Request) (string, bool) {
	values := request.Header.Values("Idempotency-Key")
	if len(values) != 1 {
		return "", false
	}
	value := strings.TrimSpace(values[0])
	return value, recordPortabilityIdempotencyPattern.MatchString(value)
}

func handleRecordImportDryRun(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application recordPortabilityApplication,
) {
	if request.Method != http.MethodPost {
		writeRecordNotFound(w)
		return
	}
	key, ok := recordPortabilityIdempotencyKey(request)
	if !ok {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key is required", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, 64<<20+1))
	if err != nil || len(body) == 0 || uint64(len(body)) > 64<<20 {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid import archive", nil)
		return
	}
	plan, err := application.DryRun(request.Context(), portability.DryRunRequest{
		Actor: actor, IdempotencyKey: key, Archive: body,
	})
	if err != nil {
		writeRecordPortabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func handleRecordImportApply(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	planID string,
	application recordPortabilityApplication,
) {
	if request.Method != http.MethodPost {
		writeRecordNotFound(w)
		return
	}
	var input recordImportApplyInput
	if err := decodeJSON(request, &input); err != nil {
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid import apply", nil)
		return
	}
	result, err := application.Apply(request.Context(), portability.ApplyRequest{
		Actor: actor, PlanID: planID, LockVersion: input.LockVersion,
	})
	if err != nil {
		writeRecordPortabilityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type recordImportApplyInput struct {
	LockVersion uint64 `json:"lock_version"`
}

func recordImportApplyPath(path string) (string, bool) {
	const prefix = "/api/record-imports/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "rip_") || parts[1] != "apply" {
		return "", false
	}
	return parts[0], true
}

func writeRecordPortabilityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, portability.ErrPortabilityDisabled),
		errors.Is(err, portability.ErrExportUnauthorized),
		errors.Is(err, portability.ErrExportNotFound):
		writeRecordNotFound(w)
	case errors.Is(err, portability.ErrExportInventoryDrift):
		writeRecordError(w, http.StatusConflict, "export_inventory_drift", "export inventory drifted", nil)
	case errors.Is(err, portability.ErrExportLeaseRevoked):
		writeRecordError(w, http.StatusConflict, "export_lease_revoked", "export lease revoked", nil)
	case errors.Is(err, portability.ErrUnsupportedExportKind):
		writeRecordError(w, http.StatusBadRequest, "unsupported_export_kind", "export kind is not supported", nil)
	case errors.Is(err, portability.ErrInvalidExportRequest):
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid export request", nil)
	case errors.Is(err, portability.ErrInvalidImportRequest):
		writeRecordError(w, http.StatusBadRequest, "invalid_request", "invalid import request", nil)
	case errors.Is(err, portability.ErrInvalidArchive):
		writeRecordError(w, http.StatusBadRequest, "invalid_archive", "invalid import archive", nil)
	case errors.Is(err, portability.ErrUntrustedImportContent):
		writeRecordError(w, http.StatusBadRequest, "untrusted_import_content", "import content is untrusted", nil)
	case errors.Is(err, portability.ErrImportSchemaBlocked):
		writeRecordError(w, http.StatusBadRequest, "import_schema_blocked", "import schema is blocked", nil)
	case errors.Is(err, portability.ErrImportCASConflict):
		writeRecordError(w, http.StatusConflict, "import_cas_conflict", "import plan changed", nil)
	case errors.Is(err, portability.ErrOriginTombstoned):
		writeRecordError(w, http.StatusConflict, "origin_tombstoned", "origin is tombstoned", nil)
	case errors.Is(err, portability.ErrImportOriginConflict):
		writeRecordError(w, http.StatusConflict, "import_origin_conflict", "import origin already exists", nil)
	case errors.Is(err, portability.ErrExportUnavailable):
		writeRecordError(w, http.StatusServiceUnavailable, "export_unavailable", "export unavailable", nil)
	default:
		writeRecordInternalError(w)
	}
}
