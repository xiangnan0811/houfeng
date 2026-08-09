package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

const (
	attachmentUploadLifetime    = 15 * time.Minute
	maxAttachmentUploadLifetime = time.Hour

	attachmentDraftHeader = "X-Houfeng-Draft-ID"
	attachmentSHAHeader   = "X-Content-SHA256"
)

type attachmentUploadService interface {
	CreateUpload(context.Context, attachments.CreateUploadRequest) (attachments.CreateUploadResult, error)
	PutContent(context.Context, attachments.PutUploadContentRequest) (attachments.PutUploadContentResult, error)
	CompleteUpload(context.Context, attachments.CompleteUploadRequest) (attachments.UploadMutationResult, error)
}

type AttachmentUploadHandlerOptions struct {
	Now             func() time.Time
	UploadLifetime  time.Duration
	MaxFileBytes    int64
	NewUploadID     func() (string, error)
	NewAttachmentID func() (string, error)
}

type attachmentUploadHandler struct {
	service         attachmentUploadService
	now             func() time.Time
	uploadLifetime  time.Duration
	maxFileBytes    int64
	newUploadID     func() (string, error)
	newAttachmentID func() (string, error)
	configurationOK bool
}

func AttachmentUploads(service attachmentUploadService) http.Handler {
	return AttachmentUploadsWithOptions(service, AttachmentUploadHandlerOptions{})
}

func AttachmentUploadsWithOptions(
	service attachmentUploadService,
	options AttachmentUploadHandlerOptions,
) http.Handler {
	configurationOK := true
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.UploadLifetime == 0 {
		options.UploadLifetime = attachmentUploadLifetime
	}
	if options.UploadLifetime < 0 || options.UploadLifetime > maxAttachmentUploadLifetime {
		configurationOK = false
	}
	if options.MaxFileBytes == 0 {
		options.MaxFileBytes = attachments.DefaultLimits().MaxFileBytes
	}
	if options.MaxFileBytes < 0 {
		configurationOK = false
	}
	if options.NewUploadID == nil {
		options.NewUploadID = func() (string, error) { return ids.New("aup") }
	}
	if options.NewAttachmentID == nil {
		options.NewAttachmentID = func() (string, error) { return ids.New("att") }
	}
	handler := &attachmentUploadHandler{
		service: service, now: options.Now, uploadLifetime: options.UploadLifetime,
		maxFileBytes: options.MaxFileBytes, newUploadID: options.NewUploadID,
		newAttachmentID: options.NewAttachmentID, configurationOK: configurationOK,
	}
	return http.HandlerFunc(handler.serveHTTP)
}

func (handler *attachmentUploadHandler) serveHTTP(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Cache-Control", recordPrivateCacheControl)
	actor, ok := sessionctx.ActorScopeFromContext(request.Context())
	if !ok || len(actor.CanonicalBytes()) == 0 {
		writeAttachmentError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable")
		return
	}

	uploadID, action, pathOK := attachmentUploadPath(request.URL.Path)
	if !pathOK {
		writeRecordNotFound(w)
		return
	}
	wantMethod := http.MethodPost
	if action == attachmentUploadActionContent {
		wantMethod = http.MethodPut
	}
	if request.Method != wantMethod {
		writeAttachmentError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if handler == nil || !handler.configurationOK || nilAttachmentUploadService(handler.service) {
		writeAttachmentUnavailable(w)
		return
	}

	switch action {
	case attachmentUploadActionCreate:
		handler.create(w, request, actor)
	case attachmentUploadActionContent:
		handler.putContent(w, request, actor, uploadID)
	case attachmentUploadActionComplete:
		handler.complete(w, request, actor, uploadID)
	default:
		writeRecordNotFound(w)
	}
}

func (handler *attachmentUploadHandler) create(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
) {
	var input attachmentUploadCreateRequest
	if !decodeAttachmentJSON(w, request, &input) {
		return
	}
	if !validAttachmentDraftID(input.DraftID) {
		writeRecordNotFound(w)
		return
	}
	if input.DeclaredSizeBytes > handler.maxFileBytes {
		writeAttachmentTooLarge(w)
		return
	}
	if input.DeclaredSizeBytes <= 0 {
		writeAttachmentInvalid(w)
		return
	}
	uploadID, uploadErr := handler.newUploadID()
	attachmentID, attachmentErr := handler.newAttachmentID()
	now := handler.now().UTC()
	if uploadErr != nil || attachmentErr != nil || attachments.ValidateUploadID(uploadID) != nil ||
		attachments.ValidateAttachmentID(attachmentID) != nil || !validAttachmentJSONTime(now) {
		writeAttachmentUnavailable(w)
		return
	}
	expiresAt := now.Add(handler.uploadLifetime)
	if !validAttachmentJSONTime(expiresAt) || !expiresAt.After(now) ||
		expiresAt.After(now.Add(maxAttachmentUploadLifetime)) {
		writeAttachmentUnavailable(w)
		return
	}
	result, err := handler.service.CreateUpload(request.Context(), attachments.CreateUploadRequest{
		Actor: actor, UploadID: uploadID, AttachmentID: attachmentID, DraftID: input.DraftID,
		DisplayName: input.DisplayName, MediaType: input.MediaType,
		DeclaredSizeBytes: input.DeclaredSizeBytes, ExpiresAt: expiresAt,
	})
	if err != nil {
		writeAttachmentApplicationError(w, err)
		return
	}
	response, ok := newAttachmentUploadCreateResponse(result, expiresAt)
	if !ok || response.UploadID != uploadID || response.AttachmentID != attachmentID {
		writeAttachmentUnavailable(w)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func (handler *attachmentUploadHandler) putContent(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	uploadID string,
) {
	draftID, ok := singleAttachmentHeader(request, attachmentDraftHeader)
	if !ok {
		writeAttachmentInvalidRequest(w)
		return
	}
	if !validAttachmentDraftID(draftID) {
		writeRecordNotFound(w)
		return
	}
	digestValue, ok := singleAttachmentHeader(request, attachmentSHAHeader)
	if !ok {
		writeAttachmentInvalidRequest(w)
		return
	}
	digestBytes, err := hex.DecodeString(digestValue)
	if err != nil || len(digestBytes) != sha256.Size || hex.EncodeToString(digestBytes) != digestValue {
		writeAttachmentInvalidRequest(w)
		return
	}
	var digest [sha256.Size]byte
	copy(digest[:], digestBytes)
	if request.ContentLength > handler.maxFileBytes {
		writeAttachmentTooLarge(w)
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, handler.maxFileBytes)
	result, err := handler.service.PutContent(request.Context(), attachments.PutUploadContentRequest{
		Actor: actor, DraftID: draftID, UploadID: uploadID, ExpectedSHA256: digest, Content: request.Body,
	})
	if err != nil {
		writeAttachmentApplicationError(w, err)
		return
	}
	if result.UploadID != uploadID || attachments.ValidateAttachmentID(result.AttachmentID) != nil ||
		result.Object.Validate() != nil || result.Object.SHA256 != digest || result.Object.SizeBytes > handler.maxFileBytes {
		writeAttachmentUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, attachmentUploadContentResponse{
		UploadID: result.UploadID, AttachmentID: result.AttachmentID,
		SizeBytes: result.Object.SizeBytes, SHA256: hex.EncodeToString(result.Object.SHA256[:]),
	})
}

func (handler *attachmentUploadHandler) complete(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	uploadID string,
) {
	var input attachmentUploadCompleteRequest
	if !decodeAttachmentJSON(w, request, &input) {
		return
	}
	if !validAttachmentDraftID(input.DraftID) {
		writeRecordNotFound(w)
		return
	}
	result, err := handler.service.CompleteUpload(request.Context(), attachments.CompleteUploadRequest{
		Actor: actor, DraftID: input.DraftID, UploadID: uploadID,
	})
	if err != nil {
		writeAttachmentApplicationError(w, err)
		return
	}
	if result.UploadID != uploadID || attachments.ValidateAttachmentID(result.AttachmentID) != nil ||
		(result.State != attachments.UploadStateQuarantined && result.State != attachments.UploadStateAvailable) ||
		!validAttachmentQuota(result.Quota) {
		writeAttachmentUnavailable(w)
		return
	}
	writeJSON(w, http.StatusAccepted, attachmentUploadCompleteResponse{
		UploadID: result.UploadID, AttachmentID: result.AttachmentID, State: result.State,
		Quota: newAttachmentQuotaResponse(result.Quota),
	})
}

type attachmentDownloadService interface {
	GetMetadata(context.Context, attachments.MetadataRequest) (attachments.AttachmentMetadata, error)
	Open(context.Context, attachments.DownloadRequest) (attachments.ContentStream, error)
}

// Attachments keeps the transport registered even before storage dependencies
// are configured. The nil service intentionally remains fail-closed.
func Attachments() http.Handler {
	return AttachmentsWithDownloadService(nil)
}

func AttachmentsWithDownloadService(service attachmentDownloadService) http.Handler {
	return http.HandlerFunc((&attachmentDownloadHandler{service: service}).serveHTTP)
}

// AttachmentsWithOptions is the explicit constructor used by the records
// transport wiring. The service is intentionally a narrow interface so the
// HTTP layer cannot bypass authorization or lease checks.
func AttachmentsWithOptions(service attachmentDownloadService) http.Handler {
	return AttachmentsWithDownloadService(service)
}

type attachmentDownloadHandler struct {
	service attachmentDownloadService
}

func (handler *attachmentDownloadHandler) serveHTTP(w http.ResponseWriter, request *http.Request) {
	setAttachmentDeliveryHeaders(w)
	actor, ok := sessionctx.ActorScopeFromContext(request.Context())
	if !ok || len(actor.CanonicalBytes()) == 0 {
		writeAttachmentError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable")
		return
	}
	attachmentID, content, ok := attachmentPath(request.URL.Path)
	if !ok {
		writeRecordNotFound(w)
		return
	}
	if request.Method != http.MethodGet {
		writeAttachmentError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if handler == nil || nilAttachmentDownloadService(handler.service) {
		writeAttachmentUnavailable(w)
		return
	}
	if !content {
		handler.metadata(w, request, actor, attachmentID)
		return
	}
	handler.content(w, request, actor, attachmentID)
}

func (handler *attachmentDownloadHandler) metadata(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	attachmentID string,
) {
	result, err := handler.service.GetMetadata(request.Context(), attachments.MetadataRequest{
		Actor: actor, AttachmentID: attachmentID,
	})
	if err != nil {
		writeAttachmentApplicationError(w, err)
		return
	}
	if attachments.ValidateAttachmentID(result.AttachmentID) != nil ||
		result.AttachmentID != attachmentID || result.SizeBytes <= 0 ||
		!validAttachmentMetadataState(result.State) || result.DisplayName == "" ||
		!utf8.ValidString(result.DisplayName) || result.MediaType == "" {
		writeAttachmentUnavailable(w)
		return
	}
	writeJSON(w, http.StatusOK, attachmentMetadataResponse{
		AttachmentID: result.AttachmentID, State: result.State, DisplayName: result.DisplayName,
		MediaType: result.MediaType, SizeBytes: result.SizeBytes, PreviewAvailable: result.PreviewAvailable,
	})
}

func (handler *attachmentDownloadHandler) content(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	attachmentID string,
) {
	variant, err := attachmentContentVariant(request)
	if err != nil {
		writeAttachmentInvalidRequest(w)
		return
	}
	delivery, err := handler.service.Open(request.Context(), attachments.DownloadRequest{
		Actor: actor, AttachmentID: attachmentID, Variant: variant, HTTPRange: singleRangeHeader(request),
	})
	if err != nil {
		if errors.Is(err, attachments.ErrInvalidContentRange) || errors.Is(err, attachments.ErrContentRangeNotSatisfiable) {
			writeAttachmentRangeError(w, handler, request, actor, attachmentID, err)
			return
		}
		writeAttachmentApplicationError(w, err)
		return
	}
	if delivery == nil {
		writeAttachmentUnavailable(w)
		return
	}
	defer func() { _ = delivery.Close(request.Context()) }()
	metadata := delivery.Metadata()
	if !validAttachmentDownloadMetadata(metadata, attachmentID) {
		_ = delivery.Close(request.Context())
		writeAttachmentUnavailable(w)
		return
	}
	status := setAttachmentContentHeaders(w, metadata)
	writer := &attachmentFlushWriter{ResponseWriter: w, status: status}
	written, streamErr := delivery.WriteTo(request.Context(), writer)
	if streamErr != nil && !writer.committed {
		clearAttachmentContentHeaders(w)
		if written != 0 {
			writeAttachmentUnavailable(w)
			return
		}
		writeAttachmentApplicationError(w, streamErr)
		return
	}
	if !writer.committed {
		clearAttachmentContentHeaders(w)
		writeAttachmentUnavailable(w)
	}
}

type attachmentMetadataResponse struct {
	AttachmentID     string                  `json:"attachment_id"`
	State            attachments.UploadState `json:"state"`
	DisplayName      string                  `json:"display_name"`
	MediaType        string                  `json:"media_type"`
	SizeBytes        int64                   `json:"size_bytes"`
	PreviewAvailable bool                    `json:"preview_available"`
}

func validAttachmentMetadataState(state attachments.UploadState) bool {
	switch state {
	case attachments.UploadStateCreated, attachments.UploadStateUploading,
		attachments.UploadStateQuarantined, attachments.UploadStateAvailable,
		attachments.UploadStateRejected, attachments.UploadStateExpired:
		return true
	default:
		return false
	}
}

func validAttachmentDownloadMetadata(metadata attachments.DownloadMetadata, attachmentID string) bool {
	return metadata.AttachmentID == attachmentID && attachments.ValidateAttachmentID(metadata.AttachmentID) == nil &&
		(metadata.Variant == attachments.ContentVariantOriginal || metadata.Variant == attachments.ContentVariantPreview) &&
		metadata.Range.Length > 0 && metadata.Range.Start >= 0 &&
		metadata.Range.EndInclusive >= metadata.Range.Start && metadata.Range.Length == metadata.Range.EndInclusive-metadata.Range.Start+1 &&
		metadata.MediaType != ""
}

func setAttachmentDeliveryHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Cache-Control", recordPrivateCacheControl)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
}

func setAttachmentContentHeaders(w http.ResponseWriter, metadata attachments.DownloadMetadata) int {
	header := w.Header()
	header.Set("Content-Type", metadata.MediaType)
	header.Set("Content-Disposition", attachments.SafeContentDisposition(metadata.Variant, metadata.DisplayName))
	header.Set("Accept-Ranges", "bytes")
	header.Set("Content-Length", strconv.FormatInt(metadata.Range.Length, 10))
	if metadata.Range.Partial {
		totalSize := metadata.Object.SizeBytes
		if totalSize < metadata.Range.EndInclusive+1 {
			totalSize = metadata.Range.EndInclusive + 1
		}
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", metadata.Range.Start, metadata.Range.EndInclusive, totalSize))
		return http.StatusPartialContent
	}
	return http.StatusOK
}

func clearAttachmentContentHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Del("Content-Type")
	header.Del("Content-Disposition")
	header.Del("Accept-Ranges")
	header.Del("Content-Length")
	header.Del("Content-Range")
}

func attachmentContentVariant(request *http.Request) (attachments.ContentVariant, error) {
	values, present := request.URL.Query()["variant"]
	if !present || len(values) == 0 {
		return attachments.ContentVariantOriginal, nil
	}
	if len(values) != 1 {
		return "", attachments.ErrInvalidContentVariant
	}
	return attachments.ParseContentVariant(values[0])
}

func singleRangeHeader(request *http.Request) string {
	values := request.Header.Values("Range")
	if len(values) != 1 {
		if len(values) == 0 {
			return ""
		}
		return ","
	}
	return values[0]
}

func writeAttachmentRangeError(
	w http.ResponseWriter,
	handler *attachmentDownloadHandler,
	request *http.Request,
	actor recordauth.ActorScope,
	attachmentID string,
	rangeErr error,
) {
	// Resolve the immutable size only after the download service has performed
	// the normal authorization check. This preserves the opaque denial boundary.
	if sizeBytes, ok := attachments.ContentRangeSize(rangeErr); ok {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", sizeBytes))
	} else if handler != nil && !nilAttachmentDownloadService(handler.service) {
		if metadata, err := handler.service.GetMetadata(request.Context(), attachments.MetadataRequest{Actor: actor, AttachmentID: attachmentID}); err == nil && metadata.SizeBytes > 0 {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", metadata.SizeBytes))
		}
	}
	writeAttachmentError(w, http.StatusRequestedRangeNotSatisfiable,
		"attachment_range_not_satisfiable", "attachment range is not satisfiable")
}

type attachmentFlushWriter struct {
	http.ResponseWriter
	status    int
	committed bool
}

func (writer *attachmentFlushWriter) Write(payload []byte) (int, error) {
	if writer == nil || writer.ResponseWriter == nil {
		return 0, io.ErrClosedPipe
	}
	if len(payload) == 0 {
		return 0, nil
	}
	if !writer.committed {
		writer.ResponseWriter.WriteHeader(writer.status)
		writer.committed = true
	}
	count, err := writer.ResponseWriter.Write(payload)
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
	return count, err
}

func nilAttachmentDownloadService(service attachmentDownloadService) bool {
	if service == nil {
		return true
	}
	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type attachmentUploadAction uint8

const (
	attachmentUploadActionCreate attachmentUploadAction = iota + 1
	attachmentUploadActionContent
	attachmentUploadActionComplete
)

func attachmentUploadPath(path string) (string, attachmentUploadAction, bool) {
	if path == "/api/attachment-uploads" {
		return "", attachmentUploadActionCreate, true
	}
	const prefix = "/api/attachment-uploads/"
	if !strings.HasPrefix(path, prefix) || strings.HasSuffix(path, "/") {
		return "", 0, false
	}
	segments := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(segments) != 2 || attachments.ValidateUploadID(segments[0]) != nil {
		return "", 0, false
	}
	switch segments[1] {
	case "content":
		return segments[0], attachmentUploadActionContent, true
	case "complete":
		return segments[0], attachmentUploadActionComplete, true
	default:
		return "", 0, false
	}
}

func attachmentPath(path string) (attachmentID string, content bool, ok bool) {
	const prefix = "/api/attachments/"
	if !strings.HasPrefix(path, prefix) || strings.HasSuffix(path, "/") {
		return "", false, false
	}
	segments := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(segments) < 1 || len(segments) > 2 || attachments.ValidateAttachmentID(segments[0]) != nil {
		return "", false, false
	}
	if len(segments) == 2 {
		if segments[1] != "content" {
			return "", false, false
		}
		content = true
	}
	return segments[0], content, true
}

type attachmentUploadCreateRequest struct {
	DraftID           string `json:"draft_id"`
	DisplayName       string `json:"display_name"`
	MediaType         string `json:"media_type"`
	DeclaredSizeBytes int64  `json:"declared_size_bytes"`
}

type attachmentUploadCompleteRequest struct {
	DraftID string `json:"draft_id"`
}

type attachmentUploadCreateResponse struct {
	UploadID     string                         `json:"upload_id"`
	AttachmentID string                         `json:"attachment_id"`
	State        attachments.UploadState        `json:"state"`
	ExpiresAt    time.Time                      `json:"expires_at"`
	Quota        attachmentQuotaResponse        `json:"quota"`
	Target       attachmentUploadTargetResponse `json:"target"`
}

type attachmentUploadTargetResponse struct {
	Transport          attachments.TransportKind `json:"transport"`
	UploadURL          string                    `json:"upload_url,omitempty"`
	Method             string                    `json:"method,omitempty"`
	RequiredHeaders    []string                  `json:"required_headers"`
	TemporaryObjectKey string                    `json:"temporary_object_key,omitempty"`
}

type attachmentQuotaResponse struct {
	LogicalBytes         int64 `json:"logical_bytes"`
	ReservedBytes        int64 `json:"reserved_bytes"`
	PhysicalBytes        int64 `json:"physical_bytes"`
	EffectiveRecordBytes int64 `json:"effective_record_bytes"`
	ProjectWarning       bool  `json:"project_warning"`
}

type attachmentUploadContentResponse struct {
	UploadID     string `json:"upload_id"`
	AttachmentID string `json:"attachment_id"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

type attachmentUploadCompleteResponse struct {
	UploadID     string                  `json:"upload_id"`
	AttachmentID string                  `json:"attachment_id"`
	State        attachments.UploadState `json:"state"`
	Quota        attachmentQuotaResponse `json:"quota"`
}

func newAttachmentUploadCreateResponse(
	result attachments.CreateUploadResult,
	expiresAt time.Time,
) (attachmentUploadCreateResponse, bool) {
	if attachments.ValidateUploadID(result.UploadID) != nil ||
		attachments.ValidateAttachmentID(result.AttachmentID) != nil ||
		result.Target.UploadID != result.UploadID ||
		!validAttachmentQuota(result.Quota) {
		return attachmentUploadCreateResponse{}, false
	}
	target := attachmentUploadTargetResponse{
		Transport:       result.Target.TransportKind,
		RequiredHeaders: make([]string, 0),
	}
	switch result.Target.TransportKind {
	case attachments.TransportKindLocal:
		if result.State != attachments.UploadStateCreated || result.Target.TemporaryObjectKey != "" ||
			result.Target.UploadURL != "" || result.Target.Method != "" || result.Target.RequiredHeaders != nil {
			return attachmentUploadCreateResponse{}, false
		}
		target.UploadURL = "/api/attachment-uploads/" + result.UploadID + "/content"
		target.Method = http.MethodPut
		target.RequiredHeaders = []string{attachmentDraftHeader, attachmentSHAHeader}
	case attachments.TransportKindS3:
		if result.State != attachments.UploadStateUploading ||
			!validAttachmentS3TemporaryObjectKey(result.Target.TemporaryObjectKey) ||
			!validAttachmentS3UploadInstruction(result.Target) {
			return attachmentUploadCreateResponse{}, false
		}
		target.UploadURL = result.Target.UploadURL
		target.Method = result.Target.Method
		target.RequiredHeaders = make([]string, len(result.Target.RequiredHeaders))
		copy(target.RequiredHeaders, result.Target.RequiredHeaders)
		target.TemporaryObjectKey = result.Target.TemporaryObjectKey
	default:
		return attachmentUploadCreateResponse{}, false
	}
	return attachmentUploadCreateResponse{
		UploadID: result.UploadID, AttachmentID: result.AttachmentID, State: result.State,
		ExpiresAt: expiresAt.UTC(), Quota: newAttachmentQuotaResponse(result.Quota), Target: target,
	}, true
}

func validAttachmentS3UploadInstruction(target attachments.UploadTarget) bool {
	if target.Method != http.MethodPut || target.RequiredHeaders == nil || len(target.RequiredHeaders) > 16 ||
		len(target.UploadURL) == 0 || len(target.UploadURL) > 8192 || strings.ContainsAny(target.UploadURL, "\r\n") {
		return false
	}
	parsed, err := url.Parse(target.UploadURL)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	seen := make(map[string]struct{}, len(target.RequiredHeaders))
	for _, header := range target.RequiredHeaders {
		canonical := http.CanonicalHeaderKey(header)
		if header == "" || header != strings.TrimSpace(header) || len(header) > 256 ||
			strings.ContainsAny(header, "\r\n") || canonical == "" ||
			strings.EqualFold(canonical, "Authorization") {
			return false
		}
		if _, exists := seen[canonical]; exists {
			return false
		}
		seen[canonical] = struct{}{}
	}
	return true
}

func newAttachmentQuotaResponse(quota attachments.QuotaSnapshot) attachmentQuotaResponse {
	return attachmentQuotaResponse{
		LogicalBytes: quota.Usage.LogicalBytes, ReservedBytes: quota.Usage.ReservedBytes,
		PhysicalBytes: quota.Usage.PhysicalBytes, EffectiveRecordBytes: quota.EffectiveRecordBytes,
		ProjectWarning: quota.ProjectWarning,
	}
}

func validAttachmentQuota(quota attachments.QuotaSnapshot) bool {
	return quota.Usage.LogicalBytes >= 0 && quota.Usage.ReservedBytes >= 0 &&
		quota.Usage.PhysicalBytes >= 0 && quota.EffectiveRecordBytes >= 0 &&
		quota.Usage.LogicalBytes <= math.MaxInt64-quota.Usage.ReservedBytes
}

func validAttachmentJSONTime(value time.Time) bool {
	if value.IsZero() {
		return false
	}
	_, err := value.MarshalJSON()
	return err == nil
}

func validAttachmentS3TemporaryObjectKey(key string) bool {
	const prefix = "temporary/"
	if len(key) != len(prefix)+sha256.Size*2 || !strings.HasPrefix(key, prefix) {
		return false
	}
	for _, character := range key[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validAttachmentDraftID(value string) bool {
	return records.ValidateDraftID(value) == nil
}

func nilAttachmentUploadService(service attachmentUploadService) bool {
	if service == nil {
		return true
	}
	value := reflect.ValueOf(service)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func singleAttachmentHeader(request *http.Request, name string) (string, bool) {
	values := request.Header.Values(name)
	returnValues := len(values) == 1 && values[0] != "" && values[0] == strings.TrimSpace(values[0])
	if !returnValues {
		return "", false
	}
	return values[0], true
}

func decodeAttachmentJSON(w http.ResponseWriter, request *http.Request, destination any) bool {
	err := decodeJSONLimited(w, request, destination, DefaultJSONBodyLimit)
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeAttachmentError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
		return false
	}
	if err != nil {
		writeAttachmentError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request")
		return false
	}
	return true
}

func writeAttachmentApplicationError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	var quotaExceeded *attachments.QuotaExceededError
	switch {
	case errors.As(err, &maxBytesError):
		writeAttachmentTooLarge(w)
	case errors.As(err, &quotaExceeded):
		switch quotaExceeded.Scope {
		case attachments.QuotaScopeFile:
			writeAttachmentTooLarge(w)
		case attachments.QuotaScopeRecord, attachments.QuotaScopeProject:
			writeAttachmentError(w, http.StatusUnprocessableEntity, "attachment_quota_exceeded", "attachment quota exceeded")
		default:
			writeAttachmentUnavailable(w)
		}
	case errors.Is(err, recordauth.ErrDenied), errors.Is(err, attachments.ErrAttachmentOwnerNotFound),
		errors.Is(err, records.ErrDraftNotFound), errors.Is(err, records.ErrRecordNotFound),
		errors.Is(err, records.ErrRecordDeletionReserved), errors.Is(err, attachments.ErrContentUnavailable),
		errors.Is(err, attachments.ErrContentVariantUnavailable):
		writeRecordNotFound(w)
	case errors.Is(err, attachments.ErrAttachmentConflict), errors.Is(err, attachments.ErrUploadExpired),
		errors.Is(err, attachments.ErrBlobConflict), errors.Is(err, attachments.ErrBlobSizeMismatch),
		errors.Is(err, attachments.ErrBlobVersionMismatch),
		errors.Is(err, attachments.ErrBlobHashMismatch), errors.Is(err, attachments.ErrBlobNotFound):
		writeAttachmentError(w, http.StatusConflict, "attachment_upload_conflict", "attachment upload changed")
	case errors.Is(err, attachments.ErrInvalidUploadServiceRequest),
		errors.Is(err, attachments.ErrInvalidAttachmentCommand), errors.Is(err, attachments.ErrInvalidQuotaUsage),
		errors.Is(err, attachments.ErrInvalidDownloadRequest):
		writeAttachmentInvalid(w)
	case errors.Is(err, attachments.ErrInvalidContentVariant):
		writeAttachmentInvalidRequest(w)
	default:
		writeAttachmentUnavailable(w)
	}
}

func writeAttachmentError(w http.ResponseWriter, status int, code, message string) {
	writeRecordError(w, status, code, message, nil)
}

func writeAttachmentInvalidRequest(w http.ResponseWriter) {
	writeAttachmentError(w, http.StatusBadRequest, "invalid_request", "invalid attachment request")
}

func writeAttachmentInvalid(w http.ResponseWriter) {
	writeAttachmentError(w, http.StatusUnprocessableEntity, "attachment_invalid", "attachment input is invalid")
}

func writeAttachmentTooLarge(w http.ResponseWriter) {
	writeAttachmentError(w, http.StatusRequestEntityTooLarge, "attachment_too_large", "attachment content is too large")
}

func writeAttachmentUnavailable(w http.ResponseWriter) {
	writeAttachmentError(w, http.StatusServiceUnavailable, "attachment_service_unavailable", "attachment service unavailable")
}
