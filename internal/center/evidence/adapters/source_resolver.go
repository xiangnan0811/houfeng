package adapters

import (
	"context"
	"errors"
	"fmt"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// 来源错误分三类：不存在、已删除或无权采集对外不区分（ErrSourceNotFound）；来源选择本身不合法
// 是选择错误（ErrInvalidCanonicalPayload）；解析结果与请求不一致是完整性问题，不归入前两类。
var (
	ErrEvidenceSourceUnavailable  = fmt.Errorf("evidence source unavailable: %w", evidence.ErrSourceNotFound)
	ErrEvidenceSourceInvalid      = fmt.Errorf("evidence source selection invalid: %w", evidence.ErrInvalidCanonicalPayload)
	ErrEvidenceSourceInconsistent = errors.New("evidence source resolution inconsistent")
)

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
		return ResolvedEvidenceSource{}, ErrEvidenceSourceInvalid
	}
	resolved, err := resolver.subjects.Resolve(ctx, actor, records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            kind,
		Role:            records.RelationRoleEvidenceSource,
		SourceID:        selection.SourceID,
	})
	switch {
	case errors.Is(err, records.ErrInvalidSubjectReference):
		return ResolvedEvidenceSource{}, fmt.Errorf("%w: %w", ErrEvidenceSourceInvalid, err)
	case errors.Is(err, records.ErrInvalidResolvedSubject):
		return ResolvedEvidenceSource{}, fmt.Errorf("%w: %w", ErrEvidenceSourceInconsistent, err)
	case err != nil:
		// 保留内层错误链：主体服务暂不可用时由调用方映射为 503，而不是当成来源不存在。
		return ResolvedEvidenceSource{}, fmt.Errorf("%w: %w", ErrEvidenceSourceUnavailable, err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(resolved.CaptureAuthorization)
	if err != nil || authorization.Digest != resolved.CaptureAuthorization.Digest ||
		authorization.Kind != recordauth.SourceKind(selection.SourceType) || authorization.SourceID != selection.SourceID ||
		resolved.StableID != selection.SourceID || resolved.IdentitySnapshot.Kind() != kind {
		return ResolvedEvidenceSource{}, ErrEvidenceSourceInconsistent
	}
	// 来源已退役或不在当前项目：对采集者而言等同不存在。
	if authorization.State != recordauth.SourceStateLive || authorization.CurrentScope == nil ||
		authorization.CaptureScope.ProjectID != actor.ProjectID || resolved.ProjectID != actor.ProjectID {
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
