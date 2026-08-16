package handlers

import (
	"context"
	"errors"
	"math"
	"net/http"
	"reflect"
	"strings"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/store"
)

const evidencePrivateCacheControl = "private, no-store"

type evidenceHandlerApplication interface {
	CapturePreview(context.Context, evidence.CapturePreviewRequest) (evidence.CapturePreviewResult, error)
	ReadSnapshot(context.Context, evidence.ReadSnapshotRequest) (evidence.ReadSnapshotResult, error)
}

type EvidenceHandlerOptions struct {
	NewRecordID   func() (string, error)
	NewSnapshotID func() (string, error)
}

func Evidence(application evidenceHandlerApplication) http.Handler {
	return EvidenceWithOptions(application, EvidenceHandlerOptions{})
}

func EvidenceWithOptions(application evidenceHandlerApplication, options EvidenceHandlerOptions) http.Handler {
	if options.NewRecordID == nil {
		options.NewRecordID = func() (string, error) { return ids.New("rec") }
	}
	if options.NewSnapshotID == nil {
		options.NewSnapshotID = func() (string, error) { return ids.New("evs") }
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", evidencePrivateCacheControl)
		actor, ok := sessionctx.ActorScopeFromContext(request.Context())
		if !ok {
			writeEvidenceError(w, http.StatusServiceUnavailable, "authorization_unavailable", "authorization unavailable")
			return
		}
		if nilEvidenceHandlerDependency(application) {
			writeEvidenceError(w, http.StatusServiceUnavailable, "evidence_service_unavailable", "evidence service unavailable")
			return
		}
		switch {
		case request.URL.Path == "/api/evidence/capture-previews":
			handleEvidenceCapturePreview(w, request, actor, application, options)
		case strings.HasPrefix(request.URL.Path, "/api/evidence/"):
			handleEvidenceRead(w, request, actor, application)
		default:
			writeEvidenceNotFound(w)
		}
	})
}

type evidenceCapturePreviewInput struct {
	RecordID                string             `json:"record_id,omitempty"`
	Kind                    evidence.KindName  `json:"kind"`
	SchemaVersion           uint16             `json:"schema_version"`
	SourceType              string             `json:"source_type"`
	SourceID                string             `json:"source_id"`
	RequestedWindow         evidenceTimeWindow `json:"requested_window"`
	Metrics                 []string           `json:"metrics"`
	PrecisionSeconds        uint64             `json:"precision_seconds"`
	SensitiveTopologyFields []string           `json:"sensitive_topology_fields"`
}

type evidenceTimeWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

func handleEvidenceCapturePreview(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application evidenceHandlerApplication,
	options EvidenceHandlerOptions,
) {
	if request.Method != http.MethodPost {
		writeEvidenceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	var input evidenceCapturePreviewInput
	if !decodeEvidenceRequestJSON(w, request, &input) {
		return
	}
	if input.Metrics == nil || input.SensitiveTopologyFields == nil ||
		input.PrecisionSeconds > uint64(math.MaxInt64/int64(time.Second)) {
		writeEvidenceError(w, http.StatusBadRequest, "invalid_request", "invalid evidence preview request")
		return
	}
	recordID := input.RecordID
	if recordID == "" {
		var err error
		recordID, err = options.NewRecordID()
		if err != nil || !validRecordTransportID(recordID) {
			writeEvidenceError(w, http.StatusServiceUnavailable, "evidence_service_unavailable", "evidence service unavailable")
			return
		}
	} else if !validRecordTransportID(recordID) {
		writeEvidenceError(w, http.StatusBadRequest, "invalid_request", "invalid evidence preview request")
		return
	}
	snapshotID, err := options.NewSnapshotID()
	if err != nil || !evidence.ValidSnapshotID(snapshotID) {
		writeEvidenceError(w, http.StatusServiceUnavailable, "evidence_service_unavailable", "evidence service unavailable")
		return
	}
	selection := evidence.Selection{
		Key:        evidence.KindKey{Kind: input.Kind, SchemaVersion: evidence.SchemaVersion(input.SchemaVersion)},
		SourceType: input.SourceType, SourceID: input.SourceID,
		RequestedWindow: evidence.TimeWindow{Start: input.RequestedWindow.Start, End: input.RequestedWindow.End},
		Metrics:         append([]string{}, input.Metrics...), Precision: time.Duration(input.PrecisionSeconds) * time.Second,
		SensitiveTopologyFields: append([]string{}, input.SensitiveTopologyFields...),
	}
	result, err := application.CapturePreview(request.Context(), evidence.CapturePreviewRequest{
		Actor: actor, RecordID: recordID, SnapshotID: snapshotID, Selection: selection,
	})
	if err != nil {
		writeEvidenceApplicationError(w, err)
		return
	}
	if result.RecordID != recordID || result.SnapshotID != snapshotID ||
		result.Preview.Key != selection.Key || result.Preview.Selection.Key != selection.Key ||
		!evidence.ValidCaptureIntentID(result.Preview.IntentID) {
		writeEvidenceError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, newEvidenceCapturePreviewResponse(result))
}

func handleEvidenceRead(
	w http.ResponseWriter,
	request *http.Request,
	actor recordauth.ActorScope,
	application evidenceHandlerApplication,
) {
	if request.Method != http.MethodGet {
		writeEvidenceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	snapshotID := strings.TrimPrefix(request.URL.Path, "/api/evidence/")
	if strings.Contains(snapshotID, "/") || !evidence.ValidSnapshotID(snapshotID) {
		writeEvidenceNotFound(w)
		return
	}
	result, err := application.ReadSnapshot(request.Context(), evidence.ReadSnapshotRequest{Actor: actor, SnapshotID: snapshotID})
	if err != nil {
		writeEvidenceApplicationError(w, err)
		return
	}
	if result.SnapshotID != snapshotID || !validRecordTransportID(result.RecordID) ||
		result.Envelope.Key != result.Summary.Key || result.Summary.RendererVersion == "" || result.Summary.ReadModel == nil {
		writeEvidenceError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, newEvidenceReadResponse(result))
}

type evidenceCapturePreviewResponse struct {
	RecordID                string                          `json:"record_id"`
	SnapshotID              string                          `json:"snapshot_id"`
	CaptureIntentID         string                          `json:"capture_intent_id"`
	Kind                    evidence.KindName               `json:"kind"`
	SchemaVersion           evidence.SchemaVersion          `json:"schema_version"`
	Subject                 evidenceIdentityResponse        `json:"subject"`
	Source                  evidenceIdentityResponse        `json:"source"`
	RequestedWindow         evidenceTimeWindow              `json:"requested_window"`
	ActualWindow            evidenceTimeWindow              `json:"actual_window"`
	ObservedAt              time.Time                       `json:"observed_at"`
	SourceRevision          string                          `json:"source_revision"`
	SourceWatermark         string                          `json:"source_watermark"`
	ProducerVersion         string                          `json:"producer_version"`
	CalculationVersion      string                          `json:"calculation_version"`
	Units                   evidenceUnitsResponse           `json:"units"`
	Quality                 evidenceQualityResponse         `json:"quality"`
	Sensitivity             evidence.Sensitivity            `json:"sensitivity"`
	ActualPrecisionSeconds  int64                           `json:"actual_precision_seconds"`
	BucketWidthSeconds      int64                           `json:"bucket_width_seconds"`
	Quota                   evidenceQuotaResponse           `json:"quota"`
	Retention               evidenceRetentionResponse       `json:"retention"`
	Redaction               []evidenceFieldDecisionResponse `json:"redaction"`
	EstimatedCanonicalBytes uint64                          `json:"estimated_canonical_bytes"`
	RendererVersion         string                          `json:"renderer_version"`
	PreviewedAt             time.Time                       `json:"previewed_at"`
	ValidUntil              time.Time                       `json:"valid_until"`
}

type evidenceReadResponse struct {
	RecordID               string                          `json:"record_id"`
	SnapshotID             string                          `json:"snapshot_id"`
	Kind                   evidence.KindName               `json:"kind"`
	SchemaVersion          evidence.SchemaVersion          `json:"schema_version"`
	Subject                evidenceIdentityResponse        `json:"subject"`
	Source                 evidenceIdentityResponse        `json:"source"`
	RequestedWindow        evidenceTimeWindow              `json:"requested_window"`
	ActualWindow           evidenceTimeWindow              `json:"actual_window"`
	ObservedAt             time.Time                       `json:"observed_at"`
	CapturedAt             time.Time                       `json:"captured_at"`
	ReferencedAt           time.Time                       `json:"referenced_at"`
	SourceRevision         string                          `json:"source_revision"`
	SourceWatermark        string                          `json:"source_watermark"`
	ProducerVersion        string                          `json:"producer_version"`
	CalculationVersion     string                          `json:"calculation_version"`
	Units                  evidenceUnitsResponse           `json:"units"`
	Quality                evidenceQualityResponse         `json:"quality"`
	Sensitivity            evidence.Sensitivity            `json:"sensitivity"`
	ActualPrecisionSeconds int64                           `json:"actual_precision_seconds"`
	BucketWidthSeconds     int64                           `json:"bucket_width_seconds"`
	Quota                  evidenceQuotaResponse           `json:"quota"`
	Retention              evidenceRetentionResponse       `json:"retention"`
	Redaction              []evidenceFieldDecisionResponse `json:"redaction"`
	SourceAvailable        bool                            `json:"source_available"`
	RendererVersion        string                          `json:"renderer_version"`
	Title                  string                          `json:"title"`
	ReadModel              map[string]any                  `json:"read_model"`
}

type evidenceIdentityResponse struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Region      string `json:"region,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	Version     string `json:"version,omitempty"`
	TargetType  string `json:"target_type,omitempty"`
}

// These transport-owned DTOs deliberately enumerate every nested response
// field. Domain structs have no JSON contract and must never become an
// accidental path for authorization, canonical payload, or digest fields.
type evidenceUnitsResponse struct {
	Status evidence.UnitsStatus `json:"status"`
	Values map[string]string    `json:"values"`
	Reason string               `json:"reason,omitempty"`
}

type evidenceQualityResponse struct {
	Status           evidence.QualityStatus `json:"status"`
	SampleCount      uint64                 `json:"sample_count"`
	GapCount         uint64                 `json:"gap_count"`
	MaintenanceCount uint64                 `json:"maintenance_count"`
	BackfilledCount  uint64                 `json:"backfilled_count"`
	BucketCount      uint64                 `json:"bucket_count"`
	DataPointCount   uint64                 `json:"data_point_count"`
	PeakCount        uint64                 `json:"peak_count"`
	Truncated        bool                   `json:"truncated"`
	Partial          bool                   `json:"partial"`
}

type evidenceQuotaResponse struct {
	Status evidence.QuotaStatus `json:"status"`
	Reason string               `json:"reason,omitempty"`
}

type evidenceRetentionResponse struct {
	Immutable      bool                             `json:"immutable"`
	Scope          evidence.RetentionScope          `json:"scope"`
	SourceDeletion evidence.SourceDeletionSemantics `json:"source_deletion"`
}

type evidenceFieldDecisionResponse struct {
	Path        string                   `json:"path"`
	Sensitivity evidence.Sensitivity     `json:"sensitivity"`
	Action      evidence.RedactionAction `json:"action"`
}

func newEvidenceCapturePreviewResponse(result evidence.CapturePreviewResult) evidenceCapturePreviewResponse {
	preview := result.Preview
	response := evidenceCapturePreviewResponse{
		RecordID: result.RecordID, SnapshotID: result.SnapshotID, CaptureIntentID: preview.IntentID,
		Kind: preview.Key.Kind, SchemaVersion: preview.Key.SchemaVersion,
		Subject: newEvidenceIdentityResponse(preview.Subject), Source: newEvidenceIdentityResponse(preview.Source),
		RequestedWindow: evidenceTimeWindow{Start: preview.RequestedWindow.Start.UTC(), End: preview.RequestedWindow.End.UTC()},
		ActualWindow:    evidenceTimeWindow{Start: preview.ActualWindow.Start.UTC(), End: preview.ActualWindow.End.UTC()},
		ObservedAt:      preview.ObservedAt.UTC(), SourceRevision: preview.SourceRevision, SourceWatermark: preview.SourceWatermark,
		ProducerVersion: preview.ProducerVersion, CalculationVersion: preview.CalculationVersion,
		Units: newEvidenceUnitsResponse(preview.Units), Quality: newEvidenceQualityResponse(preview.Quality), Sensitivity: preview.Sensitivity,
		ActualPrecisionSeconds: int64(preview.ActualPrecision.Value / time.Second),
		BucketWidthSeconds:     int64(preview.BucketWidth.Value / time.Second),
		Quota:                  newEvidenceQuotaResponse(preview.QuotaOutcome), Retention: newEvidenceRetentionResponse(preview.Retention),
		Redaction:               make([]evidenceFieldDecisionResponse, 0, len(preview.Redaction)),
		EstimatedCanonicalBytes: preview.EstimatedCanonicalBytes, RendererVersion: preview.RendererVersion,
		PreviewedAt: preview.PreviewedAt.UTC(), ValidUntil: preview.ValidUntil.UTC(),
	}
	for _, decision := range preview.Redaction {
		response.Redaction = append(response.Redaction, evidenceFieldDecisionResponse{
			Path: decision.Path, Sensitivity: decision.Sensitivity, Action: decision.Action,
		})
	}
	return response
}

func newEvidenceReadResponse(result evidence.ReadSnapshotResult) evidenceReadResponse {
	envelope := result.Envelope
	response := evidenceReadResponse{
		RecordID: result.RecordID, SnapshotID: result.SnapshotID,
		Kind: envelope.Key.Kind, SchemaVersion: envelope.Key.SchemaVersion,
		Subject: newEvidenceIdentityResponse(envelope.Subject), Source: newEvidenceIdentityResponse(envelope.Source),
		RequestedWindow: evidenceTimeWindow{Start: envelope.RequestedWindow.Start.UTC(), End: envelope.RequestedWindow.End.UTC()},
		ActualWindow:    evidenceTimeWindow{Start: envelope.ActualWindow.Start.UTC(), End: envelope.ActualWindow.End.UTC()},
		ObservedAt:      envelope.ObservedAt.UTC(), CapturedAt: envelope.CapturedAt.UTC(), ReferencedAt: envelope.ReferencedAt.UTC(),
		SourceRevision: envelope.SourceRevision, SourceWatermark: envelope.SourceWatermark,
		ProducerVersion: envelope.ProducerVersion, CalculationVersion: envelope.CalculationVersion,
		Units: newEvidenceUnitsResponse(envelope.Units), Quality: newEvidenceQualityResponse(envelope.Quality), Sensitivity: envelope.Sensitivity,
		ActualPrecisionSeconds: int64(envelope.ActualPrecision.Value / time.Second),
		BucketWidthSeconds:     int64(envelope.BucketWidth.Value / time.Second),
		Quota:                  newEvidenceQuotaResponse(envelope.QuotaOutcome), Retention: newEvidenceRetentionResponse(envelope.Retention),
		Redaction:       make([]evidenceFieldDecisionResponse, 0, len(envelope.Redaction)),
		SourceAvailable: result.SourceAvailable, RendererVersion: result.Summary.RendererVersion,
		Title: result.Summary.Title, ReadModel: result.Summary.ReadModel,
	}
	for _, decision := range envelope.Redaction {
		response.Redaction = append(response.Redaction, evidenceFieldDecisionResponse{
			Path: decision.Path, Sensitivity: decision.Sensitivity, Action: decision.Action,
		})
	}
	return response
}

func newEvidenceUnitsResponse(units evidence.UnitsSemantics) evidenceUnitsResponse {
	values := make(map[string]string, len(units.Values))
	for key, value := range units.Values {
		values[key] = value
	}
	return evidenceUnitsResponse{Status: units.Status, Values: values, Reason: units.Reason}
}

func newEvidenceQualityResponse(quality evidence.Quality) evidenceQualityResponse {
	return evidenceQualityResponse{
		Status: quality.Status, SampleCount: quality.SampleCount, GapCount: quality.GapCount,
		MaintenanceCount: quality.MaintenanceCount, BackfilledCount: quality.BackfilledCount,
		BucketCount: quality.BucketCount, DataPointCount: quality.DataPointCount,
		PeakCount: quality.PeakCount, Truncated: quality.Truncated, Partial: quality.Partial,
	}
}

func newEvidenceQuotaResponse(quota evidence.QuotaOutcome) evidenceQuotaResponse {
	return evidenceQuotaResponse{Status: quota.Status, Reason: quota.Reason}
}

func newEvidenceRetentionResponse(retention evidence.RetentionSemantics) evidenceRetentionResponse {
	return evidenceRetentionResponse{
		Immutable: retention.Immutable, Scope: retention.Scope, SourceDeletion: retention.SourceDeletion,
	}
}

func newEvidenceIdentityResponse(identity evidence.IdentitySnapshot) evidenceIdentityResponse {
	return evidenceIdentityResponse{
		Type: identity.Type, ID: identity.ID, DisplayName: identity.Fields["display_name"],
		Provider: identity.Fields["provider"], Region: identity.Fields["region"],
		Purpose: identity.Fields["purpose"], Version: identity.Fields["version"], TargetType: identity.Fields["target_type"],
	}
}

func decodeEvidenceRequestJSON(w http.ResponseWriter, request *http.Request, destination any) bool {
	err := decodeJSONLimited(w, request, destination, DefaultJSONBodyLimit)
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeEvidenceError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
		return false
	}
	if err != nil {
		writeEvidenceError(w, http.StatusBadRequest, "invalid_json", "invalid JSON request")
		return false
	}
	return true
}

func writeEvidenceApplicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recordauth.ErrDenied), errors.Is(err, evidence.ErrSnapshotNotFound):
		writeEvidenceNotFound(w)
	case errors.Is(err, evidence.ErrKindNotRegistered), errors.Is(err, evidence.ErrUnknownKindVersion):
		writeEvidenceError(w, http.StatusServiceUnavailable, "evidence_kind_unavailable", "evidence kind unavailable")
	case errors.Is(err, evidence.ErrSourceUnstable):
		writeEvidenceError(w, http.StatusConflict, "evidence_source_unstable", "evidence source is unstable")
	case errors.Is(err, evidence.ErrPreviewStale), errors.Is(err, evidence.ErrCaptureIntentUnavailable),
		errors.Is(err, evidence.ErrInvalidPreparedCapture), errors.Is(err, evidence.ErrInvalidCaptureIntentBinding):
		writeEvidenceError(w, http.StatusConflict, "evidence_preview_stale", "evidence preview is stale")
	case errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable), errors.Is(err, evidence.ErrEvidenceServiceUnavailable):
		writeEvidenceError(w, http.StatusServiceUnavailable, "evidence_service_unavailable", "evidence service unavailable")
	case errors.Is(err, evidence.ErrInvalidCapturePreviewRequest), errors.Is(err, evidence.ErrInvalidReadSnapshotRequest),
		errors.Is(err, evidence.ErrInvalidCanonicalPayload):
		writeEvidenceError(w, http.StatusUnprocessableEntity, "evidence_invalid", "evidence request is invalid")
	default:
		writeEvidenceError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeEvidenceNotFound(w http.ResponseWriter) {
	writeEvidenceError(w, http.StatusNotFound, "resource_not_found", "resource not found")
}

func writeEvidenceError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, recordErrorResponse{Code: code, Message: message, FieldErrors: make([]recordFieldError, 0)})
}

func nilEvidenceHandlerDependency(value any) bool {
	if value == nil {
		return true
	}
	typed := reflect.ValueOf(value)
	switch typed.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return typed.IsNil()
	default:
		return false
	}
}
