package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

const (
	testUploadID             = "aup_httpcontract"
	testAttachmentID         = "att_httpcontract"
	testDraftID              = "rdf_httpcontract"
	testS3TemporaryObjectKey = "temporary/1111111111111111111111111111111111111111111111111111111111111111"
	testS3UploadURL          = "https://objects.example.test/private-upload?X-Amz-Credential=runtime"
)

func TestAttachmentUploadsCreateLocalReturnsAllowlistedTarget(t *testing.T) {
	now := time.Date(2026, time.August, 4, 8, 30, 0, 0, time.UTC)
	service := &fakeAttachmentUploadService{
		createResult: attachments.CreateUploadResult{
			UploadID: testUploadID, AttachmentID: testAttachmentID, State: attachments.UploadStateCreated,
			Quota: attachments.QuotaSnapshot{
				Usage:                attachments.QuotaUsage{LogicalBytes: 11, ReservedBytes: 22, PhysicalBytes: 7},
				EffectiveRecordBytes: 33,
				ProjectWarning:       true,
			},
			Target: attachments.UploadTarget{TransportKind: attachments.TransportKindLocal, UploadID: testUploadID},
		},
	}
	handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{
		Now:             func() time.Time { return now },
		UploadLifetime:  20 * time.Minute,
		NewUploadID:     func() (string, error) { return testUploadID, nil },
		NewAttachmentID: func() (string, error) { return testAttachmentID, nil },
	})
	request := newAttachmentRequest(t, http.MethodPost, "/api/attachment-uploads", strings.NewReader(
		`{"draft_id":"rdf_httpcontract","display_name":"evidence.txt","media_type":"text/plain","declared_size_bytes":4}`,
	))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	wantBody := `{"upload_id":"aup_httpcontract","attachment_id":"att_httpcontract","state":"created","expires_at":"2026-08-04T08:50:00Z","quota":{"logical_bytes":11,"reserved_bytes":22,"physical_bytes":7,"effective_record_bytes":33,"project_warning":true},"target":{"transport":"local","upload_url":"/api/attachment-uploads/aup_httpcontract/content","method":"PUT","required_headers":["X-Houfeng-Draft-ID","X-Content-SHA256"]}}` + "\n"
	if got := recorder.Body.String(); got != wantBody {
		t.Fatalf("body = %s, want exact %s", got, wantBody)
	}
	if service.createCalls != 1 {
		t.Fatalf("CreateUpload calls = %d, want 1", service.createCalls)
	}
	if got := service.createRequest; got.UploadID != testUploadID || got.AttachmentID != testAttachmentID ||
		got.DraftID != testDraftID || got.DisplayName != "evidence.txt" || got.MediaType != "text/plain" ||
		got.DeclaredSizeBytes != 4 || !got.ExpiresAt.Equal(now.Add(20*time.Minute)) || got.Actor.UserID != "usr_0123456789abcdef01234567" {
		t.Fatalf("CreateUpload request = %#v, want normalized transport values", got)
	}
}

func TestAttachmentUploadsCreateS3ReturnsExecutableUploadInstructions(t *testing.T) {
	service := &fakeAttachmentUploadService{
		createResult: attachments.CreateUploadResult{
			UploadID: testUploadID, AttachmentID: testAttachmentID, State: attachments.UploadStateUploading,
			Quota: attachments.QuotaSnapshot{},
			Target: attachments.UploadTarget{
				TransportKind: attachments.TransportKindS3, UploadID: testUploadID,
				TemporaryObjectKey: testS3TemporaryObjectKey, UploadURL: testS3UploadURL,
				Method: http.MethodPut, RequiredHeaders: []string{},
			},
		},
	}
	handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{
		Now:             func() time.Time { return time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC) },
		UploadLifetime:  10 * time.Minute,
		NewUploadID:     func() (string, error) { return testUploadID, nil },
		NewAttachmentID: func() (string, error) { return testAttachmentID, nil },
	})
	request := newAttachmentRequest(t, http.MethodPost, "/api/attachment-uploads", strings.NewReader(
		`{"draft_id":"rdf_httpcontract","display_name":"archive.zip","media_type":"application/zip","declared_size_bytes":4}`,
	))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	wantBody := `{"upload_id":"aup_httpcontract","attachment_id":"att_httpcontract","state":"uploading","expires_at":"2026-08-04T09:10:00Z","quota":{"logical_bytes":0,"reserved_bytes":0,"physical_bytes":0,"effective_record_bytes":0,"project_warning":false},"target":{"transport":"s3","upload_url":"` + testS3UploadURL + `","method":"PUT","required_headers":[],"temporary_object_key":"` + testS3TemporaryObjectKey + `"}}` + "\n"
	if got := recorder.Body.String(); got != wantBody {
		t.Fatalf("body = %s, want exact %s", got, wantBody)
	}
	for _, forbidden := range []string{"authorization", "secret", "version_id"} {
		if strings.Contains(strings.ToLower(recorder.Body.String()), forbidden) {
			t.Fatalf("S3 target leaked unsupported instruction %q: %s", forbidden, recorder.Body.String())
		}
	}
}

func TestAttachmentUploadsCreateRejectsImpossibleServiceResults(t *testing.T) {
	tests := []struct {
		name          string
		transport     attachments.TransportKind
		state         attachments.UploadState
		temporaryKey  string
		rejectedValue string
	}{
		{name: "local uploading", transport: attachments.TransportKindLocal, state: attachments.UploadStateUploading, rejectedValue: "uploading"},
		{name: "local quarantined", transport: attachments.TransportKindLocal, state: attachments.UploadStateQuarantined, rejectedValue: "quarantined"},
		{name: "local temporary key", transport: attachments.TransportKindLocal, state: attachments.UploadStateCreated, temporaryKey: testS3TemporaryObjectKey, rejectedValue: testS3TemporaryObjectKey},
		{name: "S3 created", transport: attachments.TransportKindS3, state: attachments.UploadStateCreated, temporaryKey: testS3TemporaryObjectKey, rejectedValue: "created"},
		{name: "S3 quarantined", transport: attachments.TransportKindS3, state: attachments.UploadStateQuarantined, temporaryKey: testS3TemporaryObjectKey, rejectedValue: "quarantined"},
		{name: "S3 missing key", transport: attachments.TransportKindS3, state: attachments.UploadStateUploading},
		{name: "S3 short key", transport: attachments.TransportKindS3, state: attachments.UploadStateUploading, temporaryKey: "temporary/deadbeef", rejectedValue: "temporary/deadbeef"},
		{name: "S3 uppercase key", transport: attachments.TransportKindS3, state: attachments.UploadStateUploading, temporaryKey: "temporary/" + strings.Repeat("A", sha256.Size*2), rejectedValue: "temporary/AAAA"},
		{name: "S3 URL key", transport: attachments.TransportKindS3, state: attachments.UploadStateUploading, temporaryKey: "https://storage.invalid/private-object", rejectedValue: "https://storage.invalid"},
		{name: "S3 credential-like key", transport: attachments.TransportKindS3, state: attachments.UploadStateUploading, temporaryKey: "temporary/aws_secret_access_key=private", rejectedValue: "aws_secret_access_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeAttachmentUploadService{
				createResult: attachments.CreateUploadResult{
					UploadID: testUploadID, AttachmentID: testAttachmentID, State: tt.state,
					Quota: attachments.QuotaSnapshot{},
					Target: attachments.UploadTarget{
						TransportKind: tt.transport, UploadID: testUploadID, TemporaryObjectKey: tt.temporaryKey,
					},
				},
			}
			handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{
				NewUploadID:     func() (string, error) { return testUploadID, nil },
				NewAttachmentID: func() (string, error) { return testAttachmentID, nil },
			})
			request := newAttachmentRequest(t, http.MethodPost, "/api/attachment-uploads", strings.NewReader(
				`{"draft_id":"rdf_httpcontract","display_name":"evidence.txt","media_type":"text/plain","declared_size_bytes":4}`,
			))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, http.StatusServiceUnavailable, "attachment_service_unavailable")
			if tt.rejectedValue != "" && strings.Contains(recorder.Body.String(), tt.rejectedValue) {
				t.Fatalf("response echoed rejected service value %q: %s", tt.rejectedValue, recorder.Body.String())
			}
		})
	}
}

func TestAttachmentUploadsCreateRejectsUnencodableTimeAndInvalidGeneratorsBeforeService(t *testing.T) {
	validNow := time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		now              time.Time
		uploadLifetime   time.Duration
		uploadID         string
		uploadErr        error
		attachmentID     string
		attachmentErr    error
		rejectedResponse string
	}{
		{name: "unencodable now", now: time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)},
		{name: "unencodable expiry", now: time.Date(9999, time.December, 31, 23, 50, 0, 0, time.UTC), uploadLifetime: 20 * time.Minute},
		{name: "upload ID generator error", uploadID: "aup_generatorsecret", uploadErr: errors.New("upload generator secret"), rejectedResponse: "generator secret"},
		{name: "attachment ID generator error", attachmentID: "att_generatorsecret", attachmentErr: errors.New("attachment generator secret"), rejectedResponse: "generator secret"},
		{name: "invalid generated upload ID", uploadID: "invalid-upload-id", rejectedResponse: "invalid-upload-id"},
		{name: "invalid generated attachment ID", attachmentID: "invalid-attachment-id", rejectedResponse: "invalid-attachment-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := tt.now
			if now.IsZero() {
				now = validNow
			}
			uploadID := tt.uploadID
			if uploadID == "" {
				uploadID = testUploadID
			}
			attachmentID := tt.attachmentID
			if attachmentID == "" {
				attachmentID = testAttachmentID
			}
			service := &fakeAttachmentUploadService{createResult: attachments.CreateUploadResult{
				UploadID: uploadID, AttachmentID: attachmentID, State: attachments.UploadStateCreated,
				Quota:  attachments.QuotaSnapshot{},
				Target: attachments.UploadTarget{TransportKind: attachments.TransportKindLocal, UploadID: uploadID},
			}}
			handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{
				Now:            func() time.Time { return now },
				UploadLifetime: tt.uploadLifetime,
				NewUploadID:    func() (string, error) { return uploadID, tt.uploadErr },
				NewAttachmentID: func() (string, error) {
					return attachmentID, tt.attachmentErr
				},
			})
			request := newAttachmentRequest(t, http.MethodPost, "/api/attachment-uploads", strings.NewReader(
				`{"draft_id":"rdf_httpcontract","display_name":"evidence.txt","media_type":"text/plain","declared_size_bytes":4}`,
			))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, http.StatusServiceUnavailable, "attachment_service_unavailable")
			if service.createCalls != 0 {
				t.Fatalf("CreateUpload calls = %d, want 0", service.createCalls)
			}
			if tt.rejectedResponse != "" && strings.Contains(recorder.Body.String(), tt.rejectedResponse) {
				t.Fatalf("response leaked rejected generator value %q: %s", tt.rejectedResponse, recorder.Body.String())
			}
		})
	}
}

func TestAttachmentUploadsPutStreamsRawBodyBeyondJSONLimit(t *testing.T) {
	content := strings.Repeat("x", handlers.DefaultJSONBodyLimit+1)
	digest := sha256.Sum256([]byte(content))
	service := &fakeAttachmentUploadService{}
	service.putResult = attachments.PutUploadContentResult{
		UploadID: testUploadID, AttachmentID: testAttachmentID,
		Object: attachments.ObjectVersion{
			Key: "sha256/" + hex.EncodeToString(digest[:]), VersionID: "local-v1", SHA256: digest, SizeBytes: int64(len(content)),
		},
	}
	handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{
		MaxFileBytes: int64(len(content)),
	})
	request := newAttachmentRequest(t, http.MethodPut, "/api/attachment-uploads/aup_httpcontract/content", strings.NewReader(content))
	request.Header.Set("X-Houfeng-Draft-ID", testDraftID)
	request.Header.Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	wantBody := `{"upload_id":"aup_httpcontract","attachment_id":"att_httpcontract","size_bytes":262145,"sha256":"` + hex.EncodeToString(digest[:]) + `"}` + "\n"
	if got := recorder.Body.String(); got != wantBody {
		t.Fatalf("body = %s, want exact %s", got, wantBody)
	}
	if service.putBody != content {
		t.Fatalf("streamed body length = %d, want %d", len(service.putBody), len(content))
	}
}

func TestAttachmentUploadsPutRejectsFileOverflow(t *testing.T) {
	service := &fakeAttachmentUploadService{readPutContent: true}
	handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{MaxFileBytes: 4})
	digest := sha256.Sum256([]byte("12345"))
	request := newAttachmentRequest(t, http.MethodPut, "/api/attachment-uploads/aup_httpcontract/content", strings.NewReader("12345"))
	request.Header.Set("X-Houfeng-Draft-ID", testDraftID)
	request.Header.Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertAttachmentError(t, recorder, http.StatusRequestEntityTooLarge, "attachment_too_large")
	if service.putCalls != 0 {
		t.Fatalf("PutContent calls = %d, want 0 for known oversized body", service.putCalls)
	}
}

func TestAttachmentUploadsPutRejectsUnknownLengthOverflow(t *testing.T) {
	const maxFileBytes = 4
	content := "12345"
	digest := sha256.Sum256([]byte(content))
	service := &fakeAttachmentUploadService{readPutContent: true}
	handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{MaxFileBytes: maxFileBytes})
	request := newAttachmentRequest(t, http.MethodPut, "/api/attachment-uploads/aup_httpcontract/content", strings.NewReader(content))
	request.ContentLength = -1
	request.Header.Set("X-Houfeng-Draft-ID", testDraftID)
	request.Header.Set("X-Content-SHA256", hex.EncodeToString(digest[:]))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertAttachmentError(t, recorder, http.StatusRequestEntityTooLarge, "attachment_too_large")
	if service.putCalls != 1 {
		t.Fatalf("PutContent calls = %d, want 1 for unknown-length stream", service.putCalls)
	}
	if got := int64(len(service.putBody)); got > maxFileBytes {
		t.Fatalf("service observed %d body bytes, want no more than %d", got, maxFileBytes)
	}
}

func TestAttachmentUploadsPutMapsShortBodySizeMismatchToConflict(t *testing.T) {
	content := "123"
	expectedDigest := sha256.Sum256([]byte("1234"))
	service := &fakeAttachmentUploadService{readPutContent: true, putErr: attachments.ErrBlobSizeMismatch}
	handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{MaxFileBytes: 4})
	request := newAttachmentRequest(t, http.MethodPut, "/api/attachment-uploads/aup_httpcontract/content", strings.NewReader(content))
	request.Header.Set("X-Houfeng-Draft-ID", testDraftID)
	request.Header.Set("X-Content-SHA256", hex.EncodeToString(expectedDigest[:]))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertAttachmentError(t, recorder, http.StatusConflict, "attachment_upload_conflict")
	if service.putBody != content {
		t.Fatalf("service observed body %q, want short body %q", service.putBody, content)
	}
}

func TestAttachmentUploadsPutRequiresCanonicalIdentityHeaders(t *testing.T) {
	canonicalDigest := strings.Repeat("a", sha256.Size*2)
	tests := []struct {
		name       string
		path       string
		draft      []string
		digest     []string
		wantStatus int
		wantCode   string
	}{
		{name: "invalid upload id", path: "/api/attachment-uploads/not-an-upload/content", draft: []string{testDraftID}, digest: []string{canonicalDigest}, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "missing draft", path: "/api/attachment-uploads/aup_httpcontract/content", digest: []string{canonicalDigest}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "multiple drafts", path: "/api/attachment-uploads/aup_httpcontract/content", draft: []string{testDraftID, testDraftID}, digest: []string{canonicalDigest}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "invalid draft", path: "/api/attachment-uploads/aup_httpcontract/content", draft: []string{"draft-1"}, digest: []string{canonicalDigest}, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "missing digest", path: "/api/attachment-uploads/aup_httpcontract/content", draft: []string{testDraftID}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "uppercase digest", path: "/api/attachment-uploads/aup_httpcontract/content", draft: []string{testDraftID}, digest: []string{strings.Repeat("A", sha256.Size*2)}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "short digest", path: "/api/attachment-uploads/aup_httpcontract/content", draft: []string{testDraftID}, digest: []string{"abcd"}, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeAttachmentUploadService{}
			handler := handlers.AttachmentUploads(service)
			request := newAttachmentRequest(t, http.MethodPut, tt.path, strings.NewReader("x"))
			for _, value := range tt.draft {
				request.Header.Add("X-Houfeng-Draft-ID", value)
			}
			for _, value := range tt.digest {
				request.Header.Add("X-Content-SHA256", value)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, tt.wantStatus, tt.wantCode)
			if service.putCalls != 0 {
				t.Fatalf("PutContent calls = %d, want 0", service.putCalls)
			}
		})
	}
}

func TestAttachmentUploadsCompleteQueuesProcessing(t *testing.T) {
	service := &fakeAttachmentUploadService{
		completeResult: attachments.UploadMutationResult{
			UploadID: testUploadID, AttachmentID: testAttachmentID, State: attachments.UploadStateQuarantined,
			Quota: attachments.QuotaSnapshot{
				Usage:                attachments.QuotaUsage{LogicalBytes: 4, ReservedBytes: 0, PhysicalBytes: 4},
				EffectiveRecordBytes: 4,
			},
		},
	}
	handler := handlers.AttachmentUploads(service)
	request := newAttachmentRequest(t, http.MethodPost, "/api/attachment-uploads/aup_httpcontract/complete", strings.NewReader(
		`{"draft_id":"rdf_httpcontract"}`,
	))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s, want 202", recorder.Code, recorder.Body.String())
	}
	wantBody := `{"upload_id":"aup_httpcontract","attachment_id":"att_httpcontract","state":"quarantined","quota":{"logical_bytes":4,"reserved_bytes":0,"physical_bytes":4,"effective_record_bytes":4,"project_warning":false}}` + "\n"
	if got := recorder.Body.String(); got != wantBody {
		t.Fatalf("body = %s, want exact %s", got, wantBody)
	}
	if service.completeRequest.DraftID != testDraftID || service.completeRequest.UploadID != testUploadID {
		t.Fatalf("CompleteUpload request = %#v, want exact draft/upload", service.completeRequest)
	}
}

func TestAttachmentUploadsCompleteAcceptsAvailableReplay(t *testing.T) {
	service := &fakeAttachmentUploadService{
		completeResult: attachments.UploadMutationResult{
			UploadID: testUploadID, AttachmentID: testAttachmentID, State: attachments.UploadStateAvailable,
			Quota: attachments.QuotaSnapshot{},
		},
	}
	handler := handlers.AttachmentUploads(service)
	request := newAttachmentRequest(t, http.MethodPost, "/api/attachment-uploads/aup_httpcontract/complete", strings.NewReader(
		`{"draft_id":"rdf_httpcontract"}`,
	))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted || !strings.Contains(recorder.Body.String(), `"state":"available"`) {
		t.Fatalf("status = %d body=%s, want available replay 202", recorder.Code, recorder.Body.String())
	}
}

func TestAttachmentUploadsCompleteRejectsImpossibleServiceStates(t *testing.T) {
	states := []attachments.UploadState{
		attachments.UploadStateCreated,
		attachments.UploadStateUploading,
		attachments.UploadStateRejected,
		attachments.UploadStateExpired,
		attachments.UploadState("credential_state"),
	}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			service := &fakeAttachmentUploadService{
				completeResult: attachments.UploadMutationResult{
					UploadID: testUploadID, AttachmentID: testAttachmentID, State: state,
					Quota: attachments.QuotaSnapshot{},
				},
			}
			handler := handlers.AttachmentUploads(service)
			request := newAttachmentRequest(t, http.MethodPost, "/api/attachment-uploads/aup_httpcontract/complete", strings.NewReader(
				`{"draft_id":"rdf_httpcontract"}`,
			))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, http.StatusServiceUnavailable, "attachment_service_unavailable")
			if strings.Contains(recorder.Body.String(), string(state)) {
				t.Fatalf("response echoed rejected completion state %q: %s", state, recorder.Body.String())
			}
		})
	}
}

func TestAttachmentUploadsRejectsOverflowingQuotaServiceResults(t *testing.T) {
	overflowingQuota := attachments.QuotaSnapshot{
		Usage: attachments.QuotaUsage{LogicalBytes: math.MaxInt64, ReservedBytes: 1},
	}
	tests := []struct {
		name    string
		service *fakeAttachmentUploadService
		handler func(*fakeAttachmentUploadService) http.Handler
		path    string
		body    string
	}{
		{
			name: "create",
			service: &fakeAttachmentUploadService{createResult: attachments.CreateUploadResult{
				UploadID: testUploadID, AttachmentID: testAttachmentID, State: attachments.UploadStateCreated,
				Quota:  overflowingQuota,
				Target: attachments.UploadTarget{TransportKind: attachments.TransportKindLocal, UploadID: testUploadID},
			}},
			handler: func(service *fakeAttachmentUploadService) http.Handler {
				return handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{
					NewUploadID:     func() (string, error) { return testUploadID, nil },
					NewAttachmentID: func() (string, error) { return testAttachmentID, nil },
				})
			},
			path: "/api/attachment-uploads",
			body: `{"draft_id":"rdf_httpcontract","display_name":"evidence.txt","media_type":"text/plain","declared_size_bytes":4}`,
		},
		{
			name: "complete",
			service: &fakeAttachmentUploadService{completeResult: attachments.UploadMutationResult{
				UploadID: testUploadID, AttachmentID: testAttachmentID,
				State: attachments.UploadStateQuarantined, Quota: overflowingQuota,
			}},
			handler: func(service *fakeAttachmentUploadService) http.Handler {
				return handlers.AttachmentUploads(service)
			},
			path: "/api/attachment-uploads/aup_httpcontract/complete",
			body: `{"draft_id":"rdf_httpcontract"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := newAttachmentRequest(t, http.MethodPost, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			tt.handler(tt.service).ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, http.StatusServiceUnavailable, "attachment_service_unavailable")
			if calls := tt.service.createCalls + tt.service.completeCalls; calls != 1 {
				t.Fatalf("service calls = %d, want exactly 1 malformed result", calls)
			}
			if strings.Contains(recorder.Body.String(), "9223372036854775807") {
				t.Fatalf("response leaked rejected quota value: %s", recorder.Body.String())
			}
		})
	}
}

func TestAttachmentUploadsUsesBoundedStrictJSON(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "create unknown field", path: "/api/attachment-uploads", body: `{"draft_id":"rdf_httpcontract","display_name":"a","media_type":"text/plain","declared_size_bytes":1,"secret":"x"}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "create trailing JSON", path: "/api/attachment-uploads", body: `{"draft_id":"rdf_httpcontract"}{}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "complete unknown field", path: "/api/attachment-uploads/aup_httpcontract/complete", body: `{"draft_id":"rdf_httpcontract","scan":true}`, wantStatus: http.StatusBadRequest, wantCode: "invalid_json"},
		{name: "oversized JSON", path: "/api/attachment-uploads", body: `{"draft_id":"rdf_httpcontract","display_name":"` + strings.Repeat("x", handlers.DefaultJSONBodyLimit) + `","media_type":"text/plain","declared_size_bytes":1}`, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "request_too_large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeAttachmentUploadService{}
			handler := handlers.AttachmentUploads(service)
			request := newAttachmentRequest(t, http.MethodPost, tt.path, strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, tt.wantStatus, tt.wantCode)
			if service.createCalls != 0 || service.completeCalls != 0 {
				t.Fatalf("service calls create=%d complete=%d, want zero", service.createCalls, service.completeCalls)
			}
		})
	}
}

func TestAttachmentUploadErrorsUseStableOpaqueMapping(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantStatus    int
		wantCode      string
		rejectedValue string
	}{
		{name: "denied", err: recordauth.ErrDenied, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "owner missing", err: attachments.ErrAttachmentOwnerNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "draft missing", err: records.ErrDraftNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found", rejectedValue: records.ErrDraftNotFound.Error()},
		{name: "record missing", err: records.ErrRecordNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found", rejectedValue: records.ErrRecordNotFound.Error()},
		{name: "record deletion reserved", err: records.ErrRecordDeletionReserved, wantStatus: http.StatusNotFound, wantCode: "resource_not_found", rejectedValue: records.ErrRecordDeletionReserved.Error()},
		{name: "conflict", err: attachments.ErrAttachmentConflict, wantStatus: http.StatusConflict, wantCode: "attachment_upload_conflict"},
		{name: "expired", err: attachments.ErrUploadExpired, wantStatus: http.StatusConflict, wantCode: "attachment_upload_conflict"},
		{name: "hash drift", err: attachments.ErrBlobHashMismatch, wantStatus: http.StatusConflict, wantCode: "attachment_upload_conflict"},
		{name: "version drift", err: attachments.ErrBlobVersionMismatch, wantStatus: http.StatusConflict, wantCode: "attachment_upload_conflict"},
		{name: "size mismatch", err: attachments.ErrBlobSizeMismatch, wantStatus: http.StatusConflict, wantCode: "attachment_upload_conflict"},
		{name: "file quota", err: &attachments.QuotaExceededError{Scope: attachments.QuotaScopeFile}, wantStatus: http.StatusRequestEntityTooLarge, wantCode: "attachment_too_large"},
		{name: "record quota", err: &attachments.QuotaExceededError{Scope: attachments.QuotaScopeRecord}, wantStatus: http.StatusUnprocessableEntity, wantCode: "attachment_quota_exceeded"},
		{name: "project quota", err: &attachments.QuotaExceededError{Scope: attachments.QuotaScopeProject}, wantStatus: http.StatusUnprocessableEntity, wantCode: "attachment_quota_exceeded"},
		{name: "semantic invalid", err: attachments.ErrInvalidUploadServiceRequest, wantStatus: http.StatusUnprocessableEntity, wantCode: "attachment_invalid"},
		{name: "scanner unavailable", err: attachments.ErrArchiveScannerUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "attachment_service_unavailable"},
		{name: "Blob config unavailable", err: attachments.ErrInvalidBlobStoreConfig, wantStatus: http.StatusServiceUnavailable, wantCode: "attachment_service_unavailable"},
		{name: "unknown dependency", err: errors.New("sql secret object key temporary/private"), wantStatus: http.StatusServiceUnavailable, wantCode: "attachment_service_unavailable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeAttachmentUploadService{createErr: tt.err}
			handler := handlers.AttachmentUploadsWithOptions(service, handlers.AttachmentUploadHandlerOptions{
				NewUploadID:     func() (string, error) { return testUploadID, nil },
				NewAttachmentID: func() (string, error) { return testAttachmentID, nil },
			})
			request := newAttachmentRequest(t, http.MethodPost, "/api/attachment-uploads", strings.NewReader(
				`{"draft_id":"rdf_httpcontract","display_name":"evidence.txt","media_type":"text/plain","declared_size_bytes":4}`,
			))
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, tt.wantStatus, tt.wantCode)
			if tt.rejectedValue != "" && strings.Contains(recorder.Body.String(), tt.rejectedValue) {
				t.Fatalf("response leaked underlying error %q: %s", tt.rejectedValue, recorder.Body.String())
			}
			if tt.rejectedValue != "" {
				wantBody := `{"code":"resource_not_found","message":"resource not found","field_errors":[]}` + "\n"
				if got := recorder.Body.String(); got != wantBody {
					t.Fatalf("body = %s, want exact opaque error %s", got, wantBody)
				}
			}
			for _, secret := range []string{"sql secret", "object key", "temporary/private"} {
				if strings.Contains(recorder.Body.String(), secret) {
					t.Fatalf("response leaked underlying error detail %q: %s", secret, recorder.Body.String())
				}
			}
		})
	}
}

func TestAttachmentHandlersFailClosedWithoutActorOrDependencies(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.Handler
		method     string
		path       string
		withActor  bool
		wantStatus int
		wantCode   string
	}{
		{name: "upload missing actor", handler: handlers.AttachmentUploads(nil), method: http.MethodPost, path: "/api/attachment-uploads", wantStatus: http.StatusServiceUnavailable, wantCode: "authorization_unavailable"},
		{name: "upload missing service", handler: handlers.AttachmentUploads(nil), method: http.MethodPost, path: "/api/attachment-uploads", withActor: true, wantStatus: http.StatusServiceUnavailable, wantCode: "attachment_service_unavailable"},
		{name: "upload typed nil service", handler: handlers.AttachmentUploads((*fakeAttachmentUploadService)(nil)), method: http.MethodPost, path: "/api/attachment-uploads", withActor: true, wantStatus: http.StatusServiceUnavailable, wantCode: "attachment_service_unavailable"},
		{name: "metadata missing actor", handler: handlers.Attachments(), method: http.MethodGet, path: "/api/attachments/att_httpcontract", wantStatus: http.StatusServiceUnavailable, wantCode: "authorization_unavailable"},
		{name: "metadata unavailable", handler: handlers.Attachments(), method: http.MethodGet, path: "/api/attachments/att_httpcontract", withActor: true, wantStatus: http.StatusServiceUnavailable, wantCode: "attachment_service_unavailable"},
		{name: "content unavailable", handler: handlers.Attachments(), method: http.MethodGet, path: "/api/attachments/att_httpcontract/content", withActor: true, wantStatus: http.StatusServiceUnavailable, wantCode: "attachment_service_unavailable"},
		{name: "invalid attachment opaque", handler: handlers.Attachments(), method: http.MethodGet, path: "/api/attachments/not-an-attachment", withActor: true, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "deeper attachment subtree", handler: handlers.Attachments(), method: http.MethodGet, path: "/api/attachments/att_httpcontract/content/deeper", withActor: true, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, nil)
			if tt.withActor {
				request = request.WithContext(sessionctx.WithActorScope(request.Context(), testAttachmentActor(t)))
			}
			recorder := httptest.NewRecorder()

			tt.handler.ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, tt.wantStatus, tt.wantCode)
			if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("Cache-Control = %q, want private, no-store", got)
			}
		})
	}
}

func TestAttachmentHandlersFailClosedWithNonCanonicalActor(t *testing.T) {
	service := &fakeAttachmentUploadService{}
	uploadHandler := handlers.AttachmentUploads(service)
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/attachment-uploads", strings.NewReader(
		`{"draft_id":"rdf_httpcontract","display_name":"evidence.txt","media_type":"text/plain","declared_size_bytes":4}`,
	))
	uploadRequest = uploadRequest.WithContext(sessionctx.WithActorScope(uploadRequest.Context(), recordauth.ActorScope{
		UserID: "usr_invalid", ProjectID: recordauth.ProjectIDDefault, Role: recordauth.RoleProjectAdmin,
	}))
	uploadRecorder := httptest.NewRecorder()

	uploadHandler.ServeHTTP(uploadRecorder, uploadRequest)

	assertAttachmentError(t, uploadRecorder, http.StatusServiceUnavailable, "authorization_unavailable")
	if service.createCalls != 0 {
		t.Fatalf("CreateUpload calls = %d, want 0", service.createCalls)
	}

	readRequest := httptest.NewRequest(http.MethodGet, "/api/attachments/att_httpcontract", nil)
	readRequest = readRequest.WithContext(sessionctx.WithActorScope(readRequest.Context(), recordauth.ActorScope{
		UserID: "usr_invalid", ProjectID: recordauth.ProjectIDDefault, Role: recordauth.RoleProjectAdmin,
	}))
	readRecorder := httptest.NewRecorder()

	handlers.Attachments().ServeHTTP(readRecorder, readRequest)

	assertAttachmentError(t, readRecorder, http.StatusServiceUnavailable, "authorization_unavailable")
}

func TestAttachmentsHandlerReturnsAuthorizedMetadataDTO(t *testing.T) {
	service := &fakeAttachmentDownloadService{
		metadata: attachments.AttachmentMetadata{
			AttachmentID: "att_httpcontract", State: attachments.UploadStateAvailable,
			DisplayName: "notes.txt", MediaType: "text/plain", SizeBytes: 11, PreviewAvailable: true,
		},
	}
	handler := handlers.AttachmentsWithOptions(service)
	request := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", recorder.Code, recorder.Body.String())
	}
	want := `{"attachment_id":"att_httpcontract","state":"available","display_name":"notes.txt","media_type":"text/plain","size_bytes":11,"preview_available":true}` + "\n"
	if recorder.Body.String() != want {
		t.Fatalf("metadata body = %s, want %s", recorder.Body.String(), want)
	}
	if service.metadataCalls != 1 || service.metadataRequest.AttachmentID != "att_httpcontract" {
		t.Fatalf("metadata calls/request = %d/%#v", service.metadataCalls, service.metadataRequest)
	}
}

func TestAttachmentsHandlerStreamsOriginalAndPreviewWithRangeSecurityHeaders(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		variant     attachments.ContentVariant
		mediaType   string
		disposition string
		body        string
		status      int
		rangeValue  string
	}{
		{name: "original", path: "/api/attachments/att_httpcontract/content", variant: attachments.ContentVariantOriginal, mediaType: "application/octet-stream", disposition: "attachment", body: "original", status: http.StatusOK},
		{name: "preview range", path: "/api/attachments/att_httpcontract/content?variant=preview", variant: attachments.ContentVariantPreview, mediaType: attachments.ManagedPreviewMediaTypeTextUTF8, disposition: "inline", body: "rev", status: http.StatusPartialContent, rangeValue: "bytes=1-3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stream := &fakeAttachmentContentStream{metadata: attachments.DownloadMetadata{
				AttachmentID: "att_httpcontract", Variant: tt.variant, DisplayName: "../报告.txt", MediaType: tt.mediaType,
				Object: attachments.ObjectVersion{SizeBytes: int64(len(tt.body))},
				Range:  attachments.ResolvedContentRange{Start: 0, EndInclusive: int64(len(tt.body) - 1), Length: int64(len(tt.body)), Partial: tt.status == http.StatusPartialContent},
			}, body: tt.body}
			if tt.status == http.StatusPartialContent {
				stream.metadata.Range = attachments.ResolvedContentRange{Start: 1, EndInclusive: 3, Length: 3, Partial: true}
			}
			service := &fakeAttachmentDownloadService{stream: stream}
			handler := handlers.AttachmentsWithOptions(service)
			request := newAttachmentRequest(t, http.MethodGet, tt.path, nil)
			if tt.rangeValue != "" {
				request.Header.Set("Range", tt.rangeValue)
			}
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			if recorder.Code != tt.status || recorder.Body.String() != tt.body {
				t.Fatalf("response = %d/%q, want %d/%q", recorder.Code, recorder.Body.String(), tt.status, tt.body)
			}
			if recorder.Header().Get("Content-Type") != tt.mediaType ||
				!strings.HasPrefix(recorder.Header().Get("Content-Disposition"), tt.disposition+"; filename=") ||
				recorder.Header().Get("X-Content-Type-Options") != "nosniff" ||
				recorder.Header().Get("Cache-Control") != "private, no-store" ||
				recorder.Header().Get("Content-Security-Policy") == "" ||
				recorder.Header().Get("Accept-Ranges") != "bytes" {
				t.Fatalf("security headers = %#v", recorder.Header())
			}
			if tt.status == http.StatusPartialContent && recorder.Header().Get("Content-Range") != "bytes 1-3/4" {
				t.Fatalf("Content-Range = %q, want bytes 1-3/4", recorder.Header().Get("Content-Range"))
			}
		})
	}
}

func TestAttachmentsHandlerMapsRangeAndVariantErrorsWithoutBlobDetails(t *testing.T) {
	for _, test := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "invalid variant", err: attachments.ErrInvalidContentVariant, wantStatus: http.StatusBadRequest, wantCode: "invalid_request"},
		{name: "unsatisfiable range", err: attachments.ErrContentRangeNotSatisfiable, wantStatus: http.StatusRequestedRangeNotSatisfiable, wantCode: "attachment_range_not_satisfiable"},
		{name: "denied", err: recordauth.ErrDenied, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeAttachmentDownloadService{openErr: test.err}
			handler := handlers.AttachmentsWithOptions(service)
			request := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content", nil)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, request)

			assertAttachmentError(t, recorder, test.wantStatus, test.wantCode)
			for _, forbidden := range []string{"sha256/", "object-version", "Blob", "secret"} {
				if strings.Contains(recorder.Body.String(), forbidden) {
					t.Fatalf("error leaked %q: %s", forbidden, recorder.Body.String())
				}
			}
		})
	}
}

func TestAttachmentsHandlerUsesSelectedVariantSizeForRangeErrors(t *testing.T) {
	service := &fakeAttachmentDownloadService{
		metadata: attachments.AttachmentMetadata{
			AttachmentID: "att_httpcontract", State: attachments.UploadStateAvailable,
			DisplayName: "notes.txt", MediaType: "text/plain", SizeBytes: 10,
		},
		openErr: attachments.NewContentRangeError(4, attachments.ErrContentRangeNotSatisfiable),
	}
	handler := handlers.AttachmentsWithOptions(service)
	request := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content?variant=preview", nil)
	request.Header.Set("Range", "bytes=9-")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestedRangeNotSatisfiable || recorder.Header().Get("Content-Range") != "bytes */4" {
		t.Fatalf("range error response = %d/%q, want 416/bytes */4", recorder.Code, recorder.Header().Get("Content-Range"))
	}
	if service.metadataCalls != 0 {
		t.Fatalf("GetMetadata calls = %d, want 0 when typed range size is available", service.metadataCalls)
	}
}

func TestAttachmentsHandlerDoesNotWriteSuccessJSONAfterPartialStreamFence(t *testing.T) {
	stream := &fakeAttachmentContentStream{
		metadata: attachments.DownloadMetadata{
			AttachmentID: "att_httpcontract", Variant: attachments.ContentVariantOriginal,
			DisplayName: "notes.txt", MediaType: "text/plain",
			Object: attachments.ObjectVersion{SizeBytes: 8},
			Range:  attachments.ResolvedContentRange{Length: 8, EndInclusive: 7},
		}, body: "first", writeErr: attachments.ErrContentDeliveryRevoked,
	}
	service := &fakeAttachmentDownloadService{stream: stream}
	handler := handlers.AttachmentsWithOptions(service)
	request := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Body.String() != "first" || strings.Contains(recorder.Body.String(), "error") || stream.closeCalls != 1 {
		t.Fatalf("partial fenced response = %q closeCalls=%d", recorder.Body.String(), stream.closeCalls)
	}
}

type fakeAttachmentDownloadService struct {
	metadata        attachments.AttachmentMetadata
	metadataErr     error
	metadataCalls   int
	metadataRequest attachments.MetadataRequest
	stream          attachments.ContentStream
	openErr         error
}

func (service *fakeAttachmentDownloadService) GetMetadata(_ context.Context, request attachments.MetadataRequest) (attachments.AttachmentMetadata, error) {
	service.metadataCalls++
	service.metadataRequest = request
	return service.metadata, service.metadataErr
}

func (service *fakeAttachmentDownloadService) Open(_ context.Context, _ attachments.DownloadRequest) (attachments.ContentStream, error) {
	return service.stream, service.openErr
}

type fakeAttachmentContentStream struct {
	metadata   attachments.DownloadMetadata
	body       string
	writeErr   error
	closeCalls int
}

func (stream *fakeAttachmentContentStream) Metadata() attachments.DownloadMetadata {
	return stream.metadata
}

func (stream *fakeAttachmentContentStream) WriteTo(_ context.Context, writer io.Writer) (int64, error) {
	n, err := writer.Write([]byte(stream.body))
	if stream.writeErr != nil {
		return int64(n), stream.writeErr
	}
	return int64(n), err
}

func (stream *fakeAttachmentContentStream) Close(context.Context) error {
	stream.closeCalls++
	return nil
}

func TestAttachmentHandlersRejectWrongMethods(t *testing.T) {
	tests := []struct {
		name    string
		handler http.Handler
		method  string
		path    string
	}{
		{name: "upload collection", handler: handlers.AttachmentUploads(&fakeAttachmentUploadService{}), method: http.MethodGet, path: "/api/attachment-uploads"},
		{name: "upload content", handler: handlers.AttachmentUploads(&fakeAttachmentUploadService{}), method: http.MethodPost, path: "/api/attachment-uploads/aup_httpcontract/content"},
		{name: "upload complete", handler: handlers.AttachmentUploads(&fakeAttachmentUploadService{}), method: http.MethodPut, path: "/api/attachment-uploads/aup_httpcontract/complete"},
		{name: "attachment metadata", handler: handlers.Attachments(), method: http.MethodPost, path: "/api/attachments/att_httpcontract"},
		{name: "attachment content", handler: handlers.Attachments(), method: http.MethodPut, path: "/api/attachments/att_httpcontract/content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := newAttachmentRequest(t, tt.method, tt.path, nil)
			recorder := httptest.NewRecorder()
			tt.handler.ServeHTTP(recorder, request)
			assertAttachmentError(t, recorder, http.StatusMethodNotAllowed, "method_not_allowed")
		})
	}
}

type fakeAttachmentUploadService struct {
	createResult  attachments.CreateUploadResult
	createErr     error
	createCalls   int
	createRequest attachments.CreateUploadRequest

	putResult      attachments.PutUploadContentResult
	putErr         error
	putCalls       int
	putRequest     attachments.PutUploadContentRequest
	putBody        string
	readPutContent bool

	completeResult  attachments.UploadMutationResult
	completeErr     error
	completeCalls   int
	completeRequest attachments.CompleteUploadRequest
}

func (service *fakeAttachmentUploadService) CreateUpload(_ context.Context, request attachments.CreateUploadRequest) (attachments.CreateUploadResult, error) {
	service.createCalls++
	service.createRequest = request
	return service.createResult, service.createErr
}

func (service *fakeAttachmentUploadService) PutContent(_ context.Context, request attachments.PutUploadContentRequest) (attachments.PutUploadContentResult, error) {
	service.putCalls++
	service.putRequest = request
	if service.readPutContent || service.putErr == nil {
		body, err := io.ReadAll(request.Content)
		service.putBody = string(body)
		if err != nil {
			return attachments.PutUploadContentResult{}, fmt.Errorf("read upload content: %w", err)
		}
	}
	return service.putResult, service.putErr
}

func (service *fakeAttachmentUploadService) CompleteUpload(_ context.Context, request attachments.CompleteUploadRequest) (attachments.UploadMutationResult, error) {
	service.completeCalls++
	service.completeRequest = request
	return service.completeResult, service.completeErr
}

func newAttachmentRequest(t *testing.T, method, path string, body io.Reader) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, body)
	return request.WithContext(sessionctx.WithActorScope(request.Context(), testAttachmentActor(t)))
}

func testAttachmentActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_0123456789abcdef01234567", ProjectID: recordauth.ProjectIDDefault, Role: recordauth.RoleProjectAdmin,
		GroupIDs: []string{},
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func assertAttachmentError(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), wantStatus)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"`+wantCode+`"`) ||
		!strings.Contains(recorder.Body.String(), `"field_errors":[]`) {
		t.Fatalf("body = %s, want code %q and field_errors:[]", recorder.Body.String(), wantCode)
	}
}
