package evidence

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"

	"houfeng/internal/center/recordauth"
)

var ErrInvalidPreparedCapture = errors.New("invalid prepared evidence capture")

// PreparedCapture is an immutable, server-owned capture prepared outside the
// revision transaction. Its state is carried explicitly into later commit
// orchestration; it does not perform source reads or persistence.
type PreparedCapture struct {
	recordID      string
	snapshotID    string
	descriptor    Descriptor
	preview       Preview
	intent        Intent
	authorization AuthorizationScope
	snapshot      CanonicalSnapshot
}

func PrepareCapture(
	recordID string,
	snapshotID string,
	descriptor Descriptor,
	preview Preview,
	intent Intent,
	authorization AuthorizationScope,
	snapshot CanonicalSnapshot,
) (PreparedCapture, error) {
	if err := validatePreparedCapture(recordID, snapshotID, descriptor, preview, intent, authorization, snapshot); err != nil {
		return PreparedCapture{}, err
	}
	normalizedAuthorization, err := normalizeCaptureAuthorization(intent.Selection, authorization)
	if err != nil {
		return PreparedCapture{}, preparedCaptureError("authorization", err)
	}
	return PreparedCapture{
		recordID:      recordID,
		snapshotID:    snapshotID,
		descriptor:    cloneDescriptor(descriptor),
		preview:       clonePreview(preview),
		intent:        cloneIntent(intent),
		authorization: normalizedAuthorization,
		snapshot:      cloneCanonicalSnapshot(snapshot),
	}, nil
}

func (prepared PreparedCapture) Validate() error {
	return validatePreparedCapture(
		prepared.recordID,
		prepared.snapshotID,
		prepared.descriptor,
		prepared.preview,
		prepared.intent,
		prepared.authorization,
		prepared.snapshot,
	)
}

func (prepared PreparedCapture) RecordID() string {
	return prepared.recordID
}

func (prepared PreparedCapture) SnapshotID() string {
	return prepared.snapshotID
}

func (prepared PreparedCapture) Descriptor() Descriptor {
	return cloneDescriptor(prepared.descriptor)
}

func (prepared PreparedCapture) Preview() Preview {
	return clonePreview(prepared.preview)
}

func (prepared PreparedCapture) Intent() Intent {
	return cloneIntent(prepared.intent)
}

func (prepared PreparedCapture) Authorization() AuthorizationScope {
	normalized, err := normalizeCaptureAuthorization(prepared.intent.Selection, prepared.authorization)
	if err != nil {
		return AuthorizationScope{}
	}
	return normalized
}

func (prepared PreparedCapture) Snapshot() CanonicalSnapshot {
	return cloneCanonicalSnapshot(prepared.snapshot)
}

func validatePreparedCapture(
	recordID string,
	snapshotID string,
	descriptor Descriptor,
	preview Preview,
	intent Intent,
	authorization AuthorizationScope,
	snapshot CanonicalSnapshot,
) error {
	if !validClosedPreparedID(recordID, "rec_") {
		return preparedCaptureError("record identity", nil)
	}
	if !ValidSnapshotID(snapshotID) {
		return preparedCaptureError("snapshot identity", nil)
	}
	if err := descriptor.Validate(); err != nil {
		return preparedCaptureError("descriptor", err)
	}
	if err := validateConformancePreview(descriptor, intent.Selection, preview); err != nil {
		return preparedCaptureError("preview", err)
	}
	if preview.QuotaOutcome.Status != QuotaAllowed {
		return preparedCaptureError("preview quota", ErrInvalidSnapshotEnvelope)
	}
	if err := validateConformanceIntent(descriptor, intent.Selection, preview, intent); err != nil {
		return preparedCaptureError("intent", err)
	}
	normalizedAuthorization, err := normalizeCaptureAuthorization(intent.Selection, authorization)
	if err != nil {
		return preparedCaptureError("authorization", err)
	}
	if err := snapshot.Validate(descriptor); err != nil {
		return preparedCaptureError("snapshot", err)
	}
	if err := validatePreviewCaptureAgreement(descriptor, intent.Selection, preview, normalizedAuthorization, snapshot); err != nil {
		return preparedCaptureError("snapshot drift", err)
	}
	return nil
}

func validatePreviewCaptureAgreement(
	descriptor Descriptor,
	selection Selection,
	preview Preview,
	normalizedAuthorization AuthorizationScope,
	snapshot CanonicalSnapshot,
) error {
	envelope := snapshot.Envelope()
	expectedCaptureRedaction, err := NormalizeCaptureRedaction(descriptor, preview.Redaction)
	if err != nil {
		return err
	}
	if preview.Key != descriptor.Key || preview.Selection.Key != descriptor.Key ||
		!reflect.DeepEqual(cloneSelection(preview.Selection), cloneSelection(selection)) ||
		envelope.Key != descriptor.Key ||
		envelope.Source.Type != selection.SourceType || envelope.Source.ID != selection.SourceID ||
		envelope.RequestedWindow != normalizeWindow(selection.RequestedWindow) ||
		envelope.Authorization.Digest != normalizedAuthorization.Digest ||
		envelope.ActualWindow != normalizeWindow(preview.ActualWindow) ||
		envelope.ObservedAt != normalizeTime(preview.ObservedAt) ||
		envelope.SourceRevision != preview.SourceRevision || envelope.SourceWatermark != preview.SourceWatermark ||
		envelope.SourceDigest != preview.SourceDigest || envelope.ProducerVersion != preview.ProducerVersion ||
		envelope.CalculationVersion != preview.CalculationVersion ||
		!reflect.DeepEqual(envelope.Subject, preview.Subject) || !reflect.DeepEqual(envelope.Source, preview.Source) ||
		!reflect.DeepEqual(envelope.Units, preview.Units) || !reflect.DeepEqual(envelope.Quality, preview.Quality) ||
		envelope.Sensitivity != preview.Sensitivity || !reflect.DeepEqual(envelope.Redaction, expectedCaptureRedaction) ||
		envelope.ActualPrecision != preview.ActualPrecision || envelope.BucketWidth != preview.BucketWidth ||
		envelope.QuotaOutcome != preview.QuotaOutcome || envelope.Retention != preview.Retention ||
		envelope.CanonicalSize != preview.EstimatedCanonicalBytes ||
		preview.RendererVersion != descriptor.Conformance.RendererVersion {
		return ErrInvalidSnapshotEnvelope
	}
	return nil
}

func normalizeCaptureAuthorization(selection Selection, authorization AuthorizationScope) (AuthorizationScope, error) {
	normalized, err := recordauth.NormalizeSourceAuthorization(authorization)
	if err != nil || !reflect.DeepEqual(normalized, authorization) ||
		string(normalized.Kind) != selection.SourceType || normalized.SourceID != selection.SourceID ||
		normalized.State != recordauth.SourceStateLive || normalized.Digest == [sha256.Size]byte{} {
		return AuthorizationScope{}, ErrInvalidSnapshotEnvelope
	}
	return normalized, nil
}

func validClosedPreparedID(value, prefix string) bool {
	if len(value) < len(prefix)+1 || len(value) > len(prefix)+64 || value[:len(prefix)] != prefix {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func clonePreview(preview Preview) Preview {
	preview.Selection = cloneSelection(preview.Selection)
	preview.Subject = cloneIdentitySnapshot(preview.Subject)
	preview.Source = cloneIdentitySnapshot(preview.Source)
	preview.RequestedWindow = normalizeWindow(preview.RequestedWindow)
	preview.ActualWindow = normalizeWindow(preview.ActualWindow)
	preview.ObservedAt = normalizeTime(preview.ObservedAt)
	preview.Units = cloneUnitsSemantics(preview.Units)
	preview.Redaction = append([]FieldDecision(nil), preview.Redaction...)
	preview.PreviewedAt = normalizeTime(preview.PreviewedAt)
	preview.ValidUntil = normalizeTime(preview.ValidUntil)
	return preview
}

func cloneCanonicalSnapshot(snapshot CanonicalSnapshot) CanonicalSnapshot {
	return CanonicalSnapshot{
		envelope: cloneSnapshotEnvelope(snapshot.envelope),
		payload: CanonicalPayload{
			bytes: snapshot.Bytes(),
			hash:  snapshot.Hash(),
		},
	}
}

func preparedCaptureError(stage string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrInvalidPreparedCapture, stage)
	}
	return fmt.Errorf("%w: %s: %w", ErrInvalidPreparedCapture, stage, err)
}
