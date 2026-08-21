package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/store"
)

func TestRecordPortabilityPreviewCreateGetAndContent(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	application := &recordPortabilityHandlerStub{
		preview: func(_ context.Context, request portability.PreviewRequest) (portability.Preview, error) {
			if request.RecordID != "rec_export1" || request.ExportKind != portability.ExportKindComparisonJSON ||
				request.SnapshotID != "evs_comparison01" || request.IdempotencyKey != "export-cmp-1" {
				t.Fatalf("Preview() request = %#v", request)
			}
			return portability.Preview{
				PreviewID:       "rej_preview1",
				PreviewToken:    "aa",
				ExportKind:      request.ExportKind,
				ExportMode:      request.ExportMode,
				InventoryDigest: "bb",
				ExpectedFiles: []portability.ExpectedFile{{
					Name: "comparison.result_v1.json", MediaType: "application/json", ByteSize: 2,
				}},
				ComparisonSummary: map[string]any{"version": "comparison_result_read_model/v1"},
				ExpiresAt:         time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC),
			}, nil
		},
		create: func(_ context.Context, request portability.CreateRequest) (portability.ExportView, error) {
			if request.PreviewID != "rej_preview1" || request.PreviewToken != "aa" || request.InventoryDigest != "bb" {
				t.Fatalf("Create() request = %#v", request)
			}
			return portability.ExportView{
				ExportID: "rej_preview1", JobState: store.RecordExportJobStatePublished,
				ExportKind: portability.ExportKindComparisonJSON, MediaType: "application/json", ByteSize: 2,
				ExpiresAt: time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC),
			}, nil
		},
		get: func(_ context.Context, got recordauth.ActorScope, exportID string) (portability.ExportView, error) {
			if got.UserID != actor.UserID || exportID != "rej_preview1" {
				t.Fatalf("Get() = %s %s", got.UserID, exportID)
			}
			return portability.ExportView{
				ExportID: exportID, JobState: store.RecordExportJobStatePublished,
				ExportKind: portability.ExportKindComparisonJSON, MediaType: "application/json", ByteSize: 2,
			}, nil
		},
		open: func(context.Context, recordauth.ActorScope, string) (portability.Content, error) {
			return portability.Content{
				MediaType: "application/json",
				Filename:  "comparison.result_v1.json",
				ByteSize:  2,
				Body:      io.NopCloser(bytes.NewReader([]byte(`{}`))),
			}, nil
		},
	}
	handler := RecordPortability(application)

	preview := serveRecordPortability(t, handler, actor, http.MethodPost, "/api/record-export-previews",
		`{"record_id":"rec_export1","snapshot_id":"evs_comparison01","export_kind":"comparison_json","export_mode":"safe"}`,
		"export-cmp-1")
	if preview.Code != http.StatusOK || strings.Contains(preview.Body.String(), `"conclusion"`) ||
		strings.Contains(preview.Body.String(), `"markdown"`) {
		t.Fatalf("preview = %d %s", preview.Code, preview.Body.String())
	}

	created := serveRecordPortability(t, handler, actor, http.MethodPost, "/api/record-exports",
		`{"preview_id":"rej_preview1","preview_token":"aa","inventory_digest":"bb"}`,
		"export-cmp-1")
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d %s", created.Code, created.Body.String())
	}

	got := serveRecordPortability(t, handler, actor, http.MethodGet, "/api/record-exports/rej_preview1", "", "")
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d %s", got.Code, got.Body.String())
	}

	content := serveRecordPortability(t, handler, actor, http.MethodGet, "/api/record-exports/rej_preview1/content", "", "")
	if content.Code != http.StatusOK || content.Body.String() != "{}" {
		t.Fatalf("content = %d %q", content.Code, content.Body.String())
	}
	if content.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", content.Header().Get("Content-Type"))
	}
}

func TestRecordPortabilityHandlerRejectsCapabilityErrorsAndOversizedBodies(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		key        string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name: "disabled", method: http.MethodPost, path: "/api/record-export-previews",
			body: `{"record_id":"rec_export1","export_kind":"markdown","export_mode":"safe"}`,
			key:  "export-1", err: portability.ErrPortabilityDisabled,
			wantStatus: http.StatusNotFound, wantCode: "resource_not_found",
		},
		{
			name: "unauthorized", method: http.MethodPost, path: "/api/record-export-previews",
			body: `{"record_id":"rec_export1","export_kind":"markdown","export_mode":"safe"}`,
			key:  "export-1", err: portability.ErrExportUnauthorized,
			wantStatus: http.StatusNotFound, wantCode: "resource_not_found",
		},
		{
			name: "drift", method: http.MethodPost, path: "/api/record-exports",
			body: `{"preview_id":"rej_x","preview_token":"aa","inventory_digest":"bb"}`,
			key:  "export-1", err: portability.ErrExportInventoryDrift,
			wantStatus: http.StatusConflict, wantCode: "export_inventory_drift",
		},
		{
			name: "revoked", method: http.MethodGet, path: "/api/record-exports/rej_x/content",
			err: portability.ErrExportLeaseRevoked, wantStatus: http.StatusConflict, wantCode: "export_lease_revoked",
		},
		{
			name: "too large", method: http.MethodPost, path: "/api/record-export-previews",
			body: `{"record_id":"rec_export1","export_kind":"markdown","export_mode":"safe","padding":"` + strings.Repeat("x", DefaultJSONBodyLimit) + `"}`,
			key:  "export-1", wantStatus: http.StatusRequestEntityTooLarge, wantCode: "request_too_large",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := RecordPortability(&recordPortabilityHandlerStub{
				preview: func(context.Context, portability.PreviewRequest) (portability.Preview, error) {
					return portability.Preview{}, test.err
				},
				create: func(context.Context, portability.CreateRequest) (portability.ExportView, error) {
					return portability.ExportView{}, test.err
				},
				open: func(context.Context, recordauth.ActorScope, string) (portability.Content, error) {
					return portability.Content{}, test.err
				},
			})
			recorder := serveRecordPortability(t, handler, actor, test.method, test.path, test.body, test.key)
			assertRecordsHandlerError(t, recorder, test.wantStatus, test.wantCode)
		})
	}
}

func TestRecordPortabilityOpenFailureWritesNoContentHeaders(t *testing.T) {
	t.Parallel()

	opened := false
	handler := RecordPortability(&recordPortabilityHandlerStub{
		open: func(context.Context, recordauth.ActorScope, string) (portability.Content, error) {
			opened = true
			return portability.Content{}, portability.ErrExportLeaseRevoked
		},
	})
	recorder := serveRecordPortability(t, handler, mustRecordsHandlerActor(t), http.MethodGet,
		"/api/record-exports/rej_x/content", "", "")
	if !opened {
		t.Fatal("OpenContent was not called")
	}
	if recorder.Header().Get("Content-Disposition") != "" {
		t.Fatalf("revoked content leaked headers: %v", recorder.Header())
	}
	assertRecordsHandlerError(t, recorder, http.StatusConflict, "export_lease_revoked")
}

func TestRecordPortabilityHandlerMapsOriginConflict(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	handler := RecordPortability(&recordPortabilityHandlerStub{
		dryRun: func(context.Context, portability.DryRunRequest) (portability.ImportPlanView, error) {
			return portability.ImportPlanView{}, portability.ErrImportOriginConflict
		},
		apply: func(context.Context, portability.ApplyRequest) (portability.ApplyResult, error) {
			return portability.ApplyResult{}, portability.ErrImportOriginConflict
		},
	})
	dryRun := serveRecordPortability(t, handler, actor, http.MethodPost, "/api/record-imports/dry-run", "PK", "import-origin-1")
	assertRecordsHandlerError(t, dryRun, http.StatusConflict, "import_origin_conflict")
	applied := serveRecordPortability(t, handler, actor, http.MethodPost, "/api/record-imports/rip_origin1/apply",
		`{"lock_version":2}`, "")
	assertRecordsHandlerError(t, applied, http.StatusConflict, "import_origin_conflict")
}

type recordPortabilityHandlerStub struct {
	preview func(context.Context, portability.PreviewRequest) (portability.Preview, error)
	create  func(context.Context, portability.CreateRequest) (portability.ExportView, error)
	get     func(context.Context, recordauth.ActorScope, string) (portability.ExportView, error)
	open    func(context.Context, recordauth.ActorScope, string) (portability.Content, error)
	dryRun  func(context.Context, portability.DryRunRequest) (portability.ImportPlanView, error)
	apply   func(context.Context, portability.ApplyRequest) (portability.ApplyResult, error)
}

func (stub *recordPortabilityHandlerStub) Preview(ctx context.Context, request portability.PreviewRequest) (portability.Preview, error) {
	if stub.preview == nil {
		return portability.Preview{}, portability.ErrExportUnavailable
	}
	return stub.preview(ctx, request)
}

func (stub *recordPortabilityHandlerStub) Create(ctx context.Context, request portability.CreateRequest) (portability.ExportView, error) {
	if stub.create == nil {
		return portability.ExportView{}, portability.ErrExportUnavailable
	}
	return stub.create(ctx, request)
}

func (stub *recordPortabilityHandlerStub) Get(ctx context.Context, actor recordauth.ActorScope, exportID string) (portability.ExportView, error) {
	if stub.get == nil {
		return portability.ExportView{}, portability.ErrExportUnavailable
	}
	return stub.get(ctx, actor, exportID)
}

func (stub *recordPortabilityHandlerStub) OpenContent(ctx context.Context, actor recordauth.ActorScope, exportID string) (portability.Content, error) {
	if stub.open == nil {
		return portability.Content{}, portability.ErrExportUnavailable
	}
	return stub.open(ctx, actor, exportID)
}

func (stub *recordPortabilityHandlerStub) DryRun(ctx context.Context, request portability.DryRunRequest) (portability.ImportPlanView, error) {
	if stub.dryRun == nil {
		return portability.ImportPlanView{}, portability.ErrInvalidImportRequest
	}
	return stub.dryRun(ctx, request)
}

func (stub *recordPortabilityHandlerStub) Apply(ctx context.Context, request portability.ApplyRequest) (portability.ApplyResult, error) {
	if stub.apply == nil {
		return portability.ApplyResult{}, portability.ErrInvalidImportRequest
	}
	return stub.apply(ctx, request)
}

func serveRecordPortability(
	t *testing.T,
	handler http.Handler,
	actor recordauth.ActorScope,
	method, path, body, key string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		request.Header.Set("Idempotency-Key", key)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code >= 500 {
		var payload map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
	}
	return recorder
}
