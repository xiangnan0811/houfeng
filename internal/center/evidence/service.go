package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"houfeng/internal/center/recordauth"
)

const previewDigestDomainV1 = "houfeng.evidence.capture-preview.v1"

var (
	ErrInvalidCapturePreviewRequest = errors.New("invalid evidence capture preview request")
	ErrInvalidReadSnapshotRequest   = errors.New("invalid evidence snapshot read request")
	ErrInvalidSnapshotReadState     = errors.New("invalid evidence snapshot read state")
	ErrEvidenceServiceUnavailable   = errors.New("evidence service unavailable")
	ErrSnapshotNotFound             = errors.New("evidence snapshot not found")
	ErrSourceUnstable               = errors.New("evidence source unstable")
	ErrPreviewStale                 = errors.New("evidence preview stale")
)

type CapturePreviewRequest struct {
	Actor      ActorScope
	RecordID   string
	SnapshotID string
	Selection  Selection
}

type CapturePreviewResult struct {
	RecordID   string
	SnapshotID string
	Preview    Preview
}

type ReadSnapshotRequest struct {
	Actor      ActorScope
	SnapshotID string
}

type ReadSnapshotResult struct {
	RecordID        string
	SnapshotID      string
	Envelope        SnapshotEnvelope
	Summary         Summary
	SourceAvailable bool
}

// SnapshotReadState is the complete authority input for one ordinary evidence
// read. RecordScope is current record/revision authority. SourceAuthorization
// is the current live authorization or the final tombstone floor for the
// snapshot source. Implementations must never substitute capture-time scope for
// current source authority.
type SnapshotReadState struct {
	RecordID            string
	SnapshotID          string
	Envelope            SnapshotEnvelope
	CanonicalPayload    []byte
	RecordScope         recordauth.ResourceScope
	SourceAuthorization recordauth.SourceAuthorization
	SourceAvailable     bool
}

type CaptureIntentStore interface {
	PersistCaptureIntent(context.Context, string, string, Intent, Preview) error
}

type SnapshotReadSource interface {
	LoadEvidenceSnapshot(context.Context, ActorScope, string) (SnapshotReadState, error)
}

type Service struct {
	registry  Registry
	intents   CaptureIntentStore
	snapshots SnapshotReadSource
}

func NewService(registry Registry, intents CaptureIntentStore, snapshots SnapshotReadSource) (*Service, error) {
	if len(registry.kinds) == 0 || nilRevisionPreparationDependency(intents) || nilRevisionPreparationDependency(snapshots) {
		return nil, ErrEvidenceServiceUnavailable
	}
	return &Service{registry: registry, intents: intents, snapshots: snapshots}, nil
}

func (service *Service) CapturePreview(
	ctx context.Context,
	request CapturePreviewRequest,
) (CapturePreviewResult, error) {
	if ctx == nil || service == nil || len(service.registry.kinds) == 0 ||
		nilRevisionPreparationDependency(service.intents) ||
		!validClosedPreparedID(request.RecordID, "rec_") || !ValidSnapshotID(request.SnapshotID) ||
		request.Selection.Key.Kind == "" || request.Selection.Key.SchemaVersion == 0 {
		return CapturePreviewResult{}, ErrInvalidCapturePreviewRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil || !reflect.DeepEqual(actor, request.Actor) {
		return CapturePreviewResult{}, ErrInvalidCapturePreviewRequest
	}
	kind, err := service.registry.LookupKey(request.Selection.Key)
	if err != nil {
		return CapturePreviewResult{}, err
	}
	selection := cloneSelection(request.Selection)
	if err := kind.ValidateSelection(ctx, actor.Clone(), selection); err != nil {
		return CapturePreviewResult{}, err
	}
	preview, err := kind.PreviewCapture(ctx, actor.Clone(), cloneSelection(selection))
	if err != nil {
		return CapturePreviewResult{}, err
	}
	if err := validateConformancePreview(kind.Descriptor(), selection, preview); err != nil {
		return CapturePreviewResult{}, fmt.Errorf("%w: preview", ErrEvidenceServiceUnavailable)
	}
	digest, err := CapturePreviewDigest(preview)
	if err != nil {
		return CapturePreviewResult{}, err
	}
	intent := Intent{
		ID: preview.IntentID, Key: preview.Key, Selection: cloneSelection(preview.Selection),
		PreviewDigest: digest, ValidUntil: preview.ValidUntil,
	}
	if err := validateConformanceIntent(kind.Descriptor(), selection, preview, intent); err != nil {
		return CapturePreviewResult{}, fmt.Errorf("%w: intent", ErrEvidenceServiceUnavailable)
	}
	if err := service.intents.PersistCaptureIntent(ctx, request.RecordID, request.SnapshotID, intent, preview); err != nil {
		return CapturePreviewResult{}, err
	}
	return CapturePreviewResult{
		RecordID: request.RecordID, SnapshotID: request.SnapshotID, Preview: clonePreview(preview),
	}, nil
}

func (service *Service) ReadSnapshot(
	ctx context.Context,
	request ReadSnapshotRequest,
) (ReadSnapshotResult, error) {
	if ctx == nil || service == nil || len(service.registry.kinds) == 0 ||
		nilRevisionPreparationDependency(service.snapshots) || !ValidSnapshotID(request.SnapshotID) {
		return ReadSnapshotResult{}, ErrInvalidReadSnapshotRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil || !reflect.DeepEqual(actor, request.Actor) {
		return ReadSnapshotResult{}, ErrInvalidReadSnapshotRequest
	}
	state, err := service.snapshots.LoadEvidenceSnapshot(ctx, actor.Clone(), request.SnapshotID)
	if err != nil {
		return ReadSnapshotResult{}, err
	}
	if state.SnapshotID != request.SnapshotID || !validClosedPreparedID(state.RecordID, "rec_") ||
		state.RecordScope.Version != recordauth.ResourceScopeVersionV1 || state.RecordScope.ProjectID != actor.ProjectID {
		return ReadSnapshotResult{}, ErrInvalidSnapshotReadState
	}
	kind, err := service.registry.LookupKey(state.Envelope.Key)
	if err != nil {
		return ReadSnapshotResult{}, err
	}
	snapshot, err := RestoreCanonicalSnapshot(kind.Descriptor(), state.Envelope, state.CanonicalPayload)
	if err != nil {
		return ReadSnapshotResult{}, fmt.Errorf("%w: canonical snapshot", ErrInvalidSnapshotReadState)
	}
	currentSource, err := recordauth.NormalizeSourceAuthorization(state.SourceAuthorization)
	if err != nil || !reflect.DeepEqual(currentSource, state.SourceAuthorization) ||
		currentSource.Kind != snapshot.Envelope().Authorization.Kind ||
		currentSource.SourceID != snapshot.Envelope().Authorization.SourceID ||
		!reflect.DeepEqual(currentSource.CaptureScope, snapshot.Envelope().Authorization.CaptureScope) {
		return ReadSnapshotResult{}, ErrInvalidSnapshotReadState
	}
	if err := recordauth.Authorize(actor, recordauth.CapabilityEvidenceRead, state.RecordScope); err != nil {
		return ReadSnapshotResult{}, err
	}
	intersection := state.RecordScope
	intersection.Sources = append(append([]recordauth.SourceAuthorization(nil), state.RecordScope.Sources...), currentSource)
	if err := recordauth.Authorize(actor, recordauth.CapabilityEvidenceRead, intersection); err != nil {
		return ReadSnapshotResult{}, err
	}
	summary := kind.Summarize(snapshot)
	if summary.Key != state.Envelope.Key || summary.RendererVersion != kind.Descriptor().Conformance.RendererVersion ||
		strings.TrimSpace(summary.Title) == "" || strings.TrimSpace(summary.SearchText) == "" || summary.ReadModel == nil ||
		!validVersionedReadModel(summary.ReadModel) ||
		validateSafeStructuredValue(summary.Title, "summary.title") != nil ||
		validateSafeStructuredValue(summary.SearchText, "summary.search_text") != nil ||
		validateSafeStructuredValue(summary.ReadModel, "summary") != nil {
		return ReadSnapshotResult{}, ErrEvidenceServiceUnavailable
	}
	summary = cloneSummary(summary)
	if summary.ReadModel == nil {
		return ReadSnapshotResult{}, ErrEvidenceServiceUnavailable
	}
	return ReadSnapshotResult{
		RecordID: state.RecordID, SnapshotID: state.SnapshotID, Envelope: snapshot.Envelope(),
		Summary: summary, SourceAvailable: state.SourceAvailable,
	}, nil
}

func validVersionedReadModel(readModel map[string]any) bool {
	version, ok := readModel["version"].(string)
	if !ok {
		return false
	}
	separator := strings.LastIndex(version, "/v")
	if separator <= 0 || separator+2 >= len(version) || !validRegistryToken(version[:separator]) {
		return false
	}
	value, err := strconv.ParseUint(version[separator+2:], 10, 16)
	return err == nil && value > 0 && strconv.FormatUint(value, 10) == version[separator+2:]
}

func CapturePreviewDigest(preview Preview) ([sha256.Size]byte, error) {
	encoded, err := json.Marshal(clonePreview(preview))
	if err != nil || len(encoded) == 0 || uint64(len(encoded)) > MaxCanonicalPayloadBytes {
		return [sha256.Size]byte{}, ErrInvalidCapturePreviewRequest
	}
	hasher := sha256.New()
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(previewDigestDomainV1)))
	_, _ = hasher.Write(size[:])
	_, _ = hasher.Write([]byte(previewDigestDomainV1))
	binary.BigEndian.PutUint64(size[:], uint64(len(encoded)))
	_, _ = hasher.Write(size[:])
	_, _ = hasher.Write(encoded)
	var digest [sha256.Size]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func cloneSummary(summary Summary) Summary {
	cloned := summary
	if summary.ReadModel == nil {
		return cloned
	}
	encoded, err := json.Marshal(summary.ReadModel)
	if err != nil {
		cloned.ReadModel = nil
		return cloned
	}
	cloned.ReadModel = make(map[string]any)
	if json.Unmarshal(encoded, &cloned.ReadModel) != nil {
		cloned.ReadModel = nil
	}
	return cloned
}
