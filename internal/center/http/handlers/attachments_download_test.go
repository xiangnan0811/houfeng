package handlers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

func TestAttachmentsDownloadMetadataAndContentUseSafeHeaders(t *testing.T) {
	service, blob, _ := newHTTPDownloadService(t, false)
	handler := handlers.AttachmentsWithDownloadService(service)

	metadataRequest := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract", nil)
	metadataRecorder := httptest.NewRecorder()
	handler.ServeHTTP(metadataRecorder, metadataRequest)
	if metadataRecorder.Code != http.StatusOK {
		t.Fatalf("metadata status = %d body=%s, want 200", metadataRecorder.Code, metadataRecorder.Body.String())
	}
	if got := metadataRecorder.Body.String(); !strings.Contains(got, `"attachment_id":"att_httpcontract"`) ||
		!strings.Contains(got, `"preview_available":true`) {
		t.Fatalf("metadata body = %s, want attachment and preview fields", got)
	}
	if got := metadataRecorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("metadata Cache-Control = %q", got)
	}

	contentRequest := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content", nil)
	contentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(contentRecorder, contentRequest)
	if contentRecorder.Code != http.StatusOK || contentRecorder.Body.String() != "0123456789" {
		t.Fatalf("content = status %d body %q, want 200/full bytes", contentRecorder.Code, contentRecorder.Body.String())
	}
	if got := contentRecorder.Header().Get("Content-Length"); got != "10" {
		t.Fatalf("Content-Length = %q, want 10", got)
	}
	if got := contentRecorder.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes", got)
	}
	if got := contentRecorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := contentRecorder.Header().Get("Content-Security-Policy"); got == "" {
		t.Fatal("content response omitted Content-Security-Policy")
	}
	if strings.Contains(contentRecorder.Header().Get("Content-Disposition"), "\r") ||
		strings.Contains(contentRecorder.Header().Get("Content-Disposition"), "\n") {
		t.Fatalf("Content-Disposition contains line break: %q", contentRecorder.Header().Get("Content-Disposition"))
	}
	if blob.openCalls != 1 {
		t.Fatalf("Blob.Open calls = %d, want 1", blob.openCalls)
	}
}

func TestAttachmentsDownloadRangeAndPreviewAreClosedContracts(t *testing.T) {
	service, blob, _ := newHTTPDownloadService(t, false)
	handler := handlers.AttachmentsWithDownloadService(service)

	request := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content?variant=preview", nil)
	request.Header.Set("Range", "bytes=2-4")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "234" {
		t.Fatalf("preview range = status %d body %q, want 206/234", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Range"); got != "bytes 2-4/10" {
		t.Fatalf("Content-Range = %q, want bytes 2-4/10", got)
	}
	if got := recorder.Header().Get("Content-Length"); got != "3" {
		t.Fatalf("range Content-Length = %q, want 3", got)
	}
	if blob.lastRange != attachments.ClosedByteRange(2, 4) {
		t.Fatalf("Blob range = %#v, want closed 2-4", blob.lastRange)
	}

	for _, raw := range []string{"thumbnail", "original,preview"} {
		invalid := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content?variant="+raw, nil)
		invalidRecorder := httptest.NewRecorder()
		handler.ServeHTTP(invalidRecorder, invalid)
		if invalidRecorder.Code != http.StatusBadRequest {
			t.Fatalf("variant %q status = %d body=%s, want 400", raw, invalidRecorder.Code, invalidRecorder.Body.String())
		}
	}

	unsatisfied := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content", nil)
	unsatisfied.Header.Set("Range", "bytes=99-")
	unsatisfiedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unsatisfiedRecorder, unsatisfied)
	if unsatisfiedRecorder.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsatisfied range status = %d body=%s, want 416", unsatisfiedRecorder.Code, unsatisfiedRecorder.Body.String())
	}
	if got := unsatisfiedRecorder.Header().Get("Content-Range"); got != "bytes */10" {
		t.Fatalf("unsatisfied Content-Range = %q, want bytes */10", got)
	}
}

func TestAttachmentsDownloadDenialIsOpaqueAndDoesNotOpenBlob(t *testing.T) {
	service, blob, _ := newHTTPDownloadService(t, true)
	handler := handlers.AttachmentsWithDownloadService(service)
	request := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("denied status = %d body=%s, want opaque 404", recorder.Code, recorder.Body.String())
	}
	want := `{"code":"resource_not_found","message":"resource not found","field_errors":[]}` + "\n"
	if recorder.Body.String() != want {
		t.Fatalf("denied body = %s, want %s", recorder.Body.String(), want)
	}
	if blob.openCalls != 0 || blob.statCalls != 0 {
		t.Fatalf("denied Blob calls open/stat = %d/%d, want 0/0", blob.openCalls, blob.statCalls)
	}
}

func TestAttachmentsDownloadDoesNotTurnFenceFailureIntoJSON(t *testing.T) {
	service, _, serviceRepository := newHTTPDownloadService(t, false)
	serviceRepository.assertErrAt = 4
	serviceRepository.assertErr = recordplatform.ErrLostOwnerLease
	handler := handlers.AttachmentsWithDownloadService(service)
	request := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("fenced stream status = %d, want committed 200", recorder.Code)
	}
	if recorder.Body.String() != "0123" {
		t.Fatalf("fenced stream body = %q, want already-sent first chunk only", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"code"`) {
		t.Fatalf("fenced stream converted partial response into JSON: %s", recorder.Body.String())
	}
}

func TestAttachmentsDownloadDoesNotCommitSuccessBeforeFirstByteFence(t *testing.T) {
	service, _, serviceRepository := newHTTPDownloadService(t, false)
	// Open performs assertions before and after opening the Blob reader. The
	// next assertion is the first write preflight, before any response byte.
	serviceRepository.assertErrAt = 3
	serviceRepository.assertErr = recordplatform.ErrLostOwnerLease
	handler := handlers.AttachmentsWithDownloadService(service)
	request := newAttachmentRequest(t, http.MethodGet, "/api/attachments/att_httpcontract/content", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("first-byte fence status = %d body=%s, want 503 before success commit", recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() == "" || strings.Contains(recorder.Body.String(), "0123") {
		t.Fatalf("first-byte fence body = %q, want stable error without content bytes", recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Length"); got != "" {
		t.Fatalf("first-byte fence Content-Length = %q, want no committed content framing", got)
	}
}

func newHTTPDownloadService(t *testing.T, denied bool) (*attachments.DownloadService, *httpDownloadBlob, *httpDownloadRepository) {
	t.Helper()
	now := time.Date(2026, time.August, 7, 8, 0, 0, 0, time.UTC)
	repository := &httpDownloadRepository{attachment: httpDownloadAttachment()}
	authorizer := &httpDownloadAuthorizer{denied: denied}
	leases := &httpDownloadLeases{now: now}
	blob := &httpDownloadBlob{content: []byte("0123456789")}
	service, err := attachments.NewDownloadService(repository, authorizer, leases, blob, attachments.DownloadServiceOptions{
		Now: func() time.Time { return now }, LeaseDuration: time.Second, ChunkBytes: 4,
		NewLeaseOwnerID: func() (string, error) { return "attachment_delivery_http", nil },
	})
	if err != nil {
		t.Fatalf("NewDownloadService() error = %v", err)
	}
	return service, blob, repository
}

func httpDownloadAttachment() attachments.AttachmentContent {
	digest := func(fill byte) [sha256.Size]byte {
		var value [sha256.Size]byte
		for index := range value {
			value[index] = fill
		}
		return value
	}
	originalDigest := digest(0x11)
	previewDigest := digest(0x22)
	return attachments.AttachmentContent{
		ProjectID: "default", AttachmentID: testAttachmentID, RecordID: "rec_httpdownload",
		AuthorID: "usr_0123456789abcdef01234567", State: attachments.UploadStateAvailable,
		DisplayName: "../报告\r\n.svg", MediaType: "image/svg+xml", LogicalSizeBytes: 10,
		Original: attachments.ObjectVersion{Key: "sha256/" + strings.Repeat("11", sha256.Size), VersionID: "original-http-v1", SHA256: originalDigest, SizeBytes: 10},
		Preview:  &attachments.ManagedPreviewContent{Object: attachments.ObjectVersion{Key: "sha256/" + strings.Repeat("22", sha256.Size), VersionID: "preview-http-v1", SHA256: previewDigest, SizeBytes: 10}, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8},
	}
}

type httpDownloadRepository struct {
	attachment  attachments.AttachmentContent
	assertCalls int
	assertErrAt int
	assertErr   error
}

func (repo *httpDownloadRepository) GetAttachmentForDownload(context.Context, attachments.ContentLookup) (attachments.AttachmentContent, error) {
	return repo.attachment, nil
}

func (repo *httpDownloadRepository) AssertAttachmentContent(context.Context, attachments.ContentAssertion) error {
	repo.assertCalls++
	if repo.assertErrAt > 0 && repo.assertCalls >= repo.assertErrAt {
		return repo.assertErr
	}
	return nil
}

type httpDownloadAuthorizer struct{ denied bool }

func (authorizer *httpDownloadAuthorizer) AuthorizeRecordAttachmentRead(context.Context, recordauth.ActorScope, string) error {
	if authorizer.denied {
		return recordauth.ErrDenied
	}
	return nil
}

type httpDownloadLeases struct{ now time.Time }

func (leases *httpDownloadLeases) AcquireServingLease(_ context.Context, object recordplatform.ObjectRef, input recordplatform.LeaseClaimInputV1) (recordplatform.ServingLeaseV1, error) {
	return recordplatform.ServingLeaseV1{Object: object, Owner: recordplatform.OwnerLease{OwnerID: input.OwnerID, Generation: 1, ExpiresAt: leases.now.Add(input.LeaseDuration)}, CapturedEpoch: 1}, nil
}

func (leases *httpDownloadLeases) RenewServingLease(_ context.Context, serving recordplatform.ServingLeaseV1, duration time.Duration) (recordplatform.ServingLeaseV1, error) {
	serving.Owner.ExpiresAt = leases.now.Add(duration)
	return serving, nil
}

func (*httpDownloadLeases) AssertServingLease(context.Context, recordplatform.ServingLeaseV1) error {
	return nil
}
func (*httpDownloadLeases) ReleaseObjectContentLease(context.Context, recordplatform.ObjectRef, recordplatform.OwnerLease) error {
	return nil
}

type httpDownloadBlob struct {
	content   []byte
	openCalls int
	statCalls int
	lastRange attachments.ByteRange
}

func (*httpDownloadBlob) Put(context.Context, attachments.PutRequest, io.Reader) (attachments.ObjectVersion, error) {
	return attachments.ObjectVersion{}, errors.New("unexpected put")
}

func (blob *httpDownloadBlob) Open(_ context.Context, version attachments.ObjectVersion, byteRange attachments.ByteRange) (io.ReadCloser, error) {
	blob.openCalls++
	blob.lastRange = byteRange
	start, end := int64(0), int64(len(blob.content)-1)
	if byteRange != attachments.FullByteRange() {
		start, end = byteRange.Start, byteRange.EndInclusive
	}
	return io.NopCloser(bytes.NewReader(blob.content[start : end+1])), nil
}

func (blob *httpDownloadBlob) Stat(_ context.Context, version attachments.ObjectVersion) (attachments.ObjectInfo, error) {
	blob.statCalls++
	return attachments.ObjectInfo{Version: version}, nil
}

func (*httpDownloadBlob) Delete(context.Context, attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	return attachments.DeletionReceipt{}, errors.New("unexpected delete")
}

var _ = http.MethodGet
