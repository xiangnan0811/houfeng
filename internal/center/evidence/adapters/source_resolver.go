package adapters

import (
	"context"
	"errors"
	"fmt"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

var ErrEvidenceSourceUnavailable = errors.New("evidence source unavailable")

// RecordEvidenceSourceResolver is the closed bridge from an evidence selection
// to the existing Records subject authority. It deliberately has no generic
// source or local tombstone fallback.
type RecordEvidenceSourceResolver struct {
	subjects records.SubjectAdapterRegistry
}

func NewRecordEvidenceSourceResolver(subjects records.SubjectAdapterRegistry) (*RecordEvidenceSourceResolver, error) {
	return &RecordEvidenceSourceResolver{subjects: subjects}, nil
}

func (resolver *RecordEvidenceSourceResolver) ResolveEvidenceSource(
	ctx context.Context,
	actor evidence.ActorScope,
	selection evidence.Selection,
) (ResolvedEvidenceSource, error) {
	if ctx == nil || resolver == nil {
		return ResolvedEvidenceSource{}, ErrEvidenceSourceUnavailable
	}
	kind, ok := evidenceSourceSubjectKind(selection.SourceType)
	if !ok {
		return ResolvedEvidenceSource{}, ErrEvidenceSourceUnavailable
	}
	resolved, err := resolver.subjects.Resolve(ctx, actor, records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            kind,
		Role:            records.RelationRoleEvidenceSource,
		SourceID:        selection.SourceID,
	})
	if err != nil {
		return ResolvedEvidenceSource{}, fmt.Errorf("%w: %v", ErrEvidenceSourceUnavailable, err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(resolved.CaptureAuthorization)
	if err != nil || authorization.Digest != resolved.CaptureAuthorization.Digest ||
		authorization.State != recordauth.SourceStateLive || authorization.CurrentScope == nil ||
		authorization.CaptureScope.ProjectID != actor.ProjectID || authorization.Kind != recordauth.SourceKind(selection.SourceType) ||
		authorization.SourceID != selection.SourceID || resolved.ProjectID != actor.ProjectID ||
		resolved.StableID != selection.SourceID || resolved.IdentitySnapshot.Kind() != kind {
		return ResolvedEvidenceSource{}, ErrEvidenceSourceUnavailable
	}
	identity := evidence.IdentitySnapshot{
		Type:   selection.SourceType,
		ID:     selection.SourceID,
		Fields: resolved.IdentitySnapshot.Fields(),
	}
	return ResolvedEvidenceSource{Subject: identity, Source: identity, Authorization: authorization}, nil
}

func evidenceSourceSubjectKind(sourceType string) (records.SubjectKind, bool) {
	switch recordauth.SourceKind(sourceType) {
	case recordauth.SourceKindVPS:
		return records.SubjectKindVPS, true
	case recordauth.SourceKindMonitoringInstance:
		return records.SubjectKindMonitoringInstance, true
	case recordauth.SourceKindTarget:
		return records.SubjectKindTarget, true
	default:
		return "", false
	}
}
