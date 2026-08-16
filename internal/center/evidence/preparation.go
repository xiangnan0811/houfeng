package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"time"

	"houfeng/internal/center/recordauth"
)

var (
	ErrCaptureIntentUnavailable          = errors.New("evidence capture intent unavailable")
	ErrInvalidCaptureIntentBinding       = errors.New("invalid evidence capture intent binding")
	ErrInvalidRevisionPreparer           = errors.New("invalid evidence revision preparer")
	ErrInvalidRevisionPreparationRequest = errors.New("invalid evidence revision preparation request")
)

// CaptureIntentBinding is the complete persisted preview/intent state required
// to reauthorize and recapture one fresh snapshot outside a revision transaction.
type CaptureIntentBinding struct {
	RecordID   string
	SnapshotID string
	Intent     Intent
	Preview    Preview
}

func (binding CaptureIntentBinding) Validate() error {
	if !captureIntentBindingTimestampsCanonical(binding) ||
		!validClosedPreparedID(binding.RecordID, "rec_") || !ValidSnapshotID(binding.SnapshotID) ||
		!ValidCaptureIntentID(binding.Intent.ID) || binding.Preview.IntentID != binding.Intent.ID ||
		validateKnownKindKey(binding.Intent.Key) != nil || binding.Preview.Key != binding.Intent.Key ||
		binding.Intent.Selection.Key != binding.Intent.Key || binding.Preview.Selection.Key != binding.Intent.Key ||
		!reflect.DeepEqual(binding.Intent.Selection, binding.Preview.Selection) ||
		binding.Intent.PreviewDigest == [sha256.Size]byte{} || binding.Preview.SourceDigest == [sha256.Size]byte{} ||
		binding.Preview.EstimatedCanonicalBytes == 0 || binding.Preview.EstimatedCanonicalBytes > MaxCanonicalPayloadBytes ||
		binding.Preview.ValidUntil != binding.Intent.ValidUntil ||
		binding.Preview.ValidUntil != binding.Preview.PreviewedAt.Add(CaptureIntentTTL) {
		return ErrInvalidCaptureIntentBinding
	}
	return nil
}

func captureIntentBindingTimestampsCanonical(binding CaptureIntentBinding) bool {
	values := []time.Time{
		binding.Intent.Selection.RequestedWindow.Start,
		binding.Intent.Selection.RequestedWindow.End,
		binding.Intent.ValidUntil,
		binding.Preview.Selection.RequestedWindow.Start,
		binding.Preview.Selection.RequestedWindow.End,
		binding.Preview.RequestedWindow.Start,
		binding.Preview.RequestedWindow.End,
		binding.Preview.ActualWindow.Start,
		binding.Preview.ActualWindow.End,
		binding.Preview.ObservedAt,
		binding.Preview.PreviewedAt,
		binding.Preview.ValidUntil,
	}
	for _, value := range values {
		if value.IsZero() || value.Location() != time.UTC || value != value.Round(0) ||
			value.Nanosecond()%int(time.Microsecond) != 0 {
			return false
		}
	}
	return true
}

// CaptureIntentBindingSource loads the complete server-owned binding. A
// production implementation must finish its database transaction before it
// returns; adapters are invoked only by RevisionPreparer afterwards.
type CaptureIntentBindingSource interface {
	LoadCaptureIntentBinding(context.Context, string, string) (CaptureIntentBinding, error)
}

// CapturePayloadSink persists canonical bytes by their content digest. It is
// deliberately separate from revision commit so a later rollback can leave at
// most a reclaimable orphan payload.
type CapturePayloadSink interface {
	PersistCapturePayload(context.Context, CanonicalSnapshot) error
}

// RevisionPreparationItem is a strict tagged union. Exactly one identity must
// be present, and slice order becomes the revision's canonical evidence order.
type RevisionPreparationItem struct {
	CaptureIntentID    string
	ExistingSnapshotID string
}

type RevisionPreparationRequest struct {
	RecordID string
	Items    []RevisionPreparationItem
}

// RevisionPreparer performs all source-facing work before revision commit.
// It has no pgx transaction, context value, singleton, or durable staging seam.
type RevisionPreparer struct {
	registry   Registry
	intents    CaptureIntentBindingSource
	payloads   CapturePayloadSink
	references ExistingSnapshotReferenceSource
	capacity   *CapacityEnforcer
}

func NewRevisionPreparer(
	registry Registry,
	intents CaptureIntentBindingSource,
	payloads CapturePayloadSink,
	references ExistingSnapshotReferenceSource,
	capacity *CapacityEnforcer,
) (*RevisionPreparer, error) {
	if len(registry.kinds) == 0 || nilRevisionPreparationDependency(intents) ||
		nilRevisionPreparationDependency(payloads) || nilExistingSnapshotReferenceSource(references) ||
		capacity == nil || capacity.policy.Validate() != nil || nilCapacityDependency(capacity.source) {
		return nil, ErrInvalidRevisionPreparer
	}
	return &RevisionPreparer{
		registry: registry, intents: intents, payloads: payloads, references: references, capacity: capacity,
	}, nil
}

func (preparer *RevisionPreparer) Prepare(
	ctx context.Context,
	actor ActorScope,
	request RevisionPreparationRequest,
) (RevisionPreparation, error) {
	if ctx == nil || preparer == nil || len(preparer.registry.kinds) == 0 ||
		nilRevisionPreparationDependency(preparer.intents) || nilRevisionPreparationDependency(preparer.payloads) ||
		nilExistingSnapshotReferenceSource(preparer.references) || preparer.capacity == nil ||
		preparer.capacity.policy.Validate() != nil || nilCapacityDependency(preparer.capacity.source) {
		return RevisionPreparation{}, ErrInvalidRevisionPreparer
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil || !reflect.DeepEqual(normalizedActor, actor) {
		return RevisionPreparation{}, revisionPreparationRequestError("actor", err)
	}
	if err := validateRevisionPreparationRequest(request); err != nil {
		return RevisionPreparation{}, err
	}

	captures := make([]PreparedCapture, 0, len(request.Items))
	references := make([]PreparedReference, 0, len(request.Items))
	orderedSnapshotIDs := make([]string, 0, len(request.Items))
	preparedSnapshotIDs := make(map[string]struct{}, len(request.Items))
	var additionalLogicalBytes uint64
	for _, item := range request.Items {
		if err := ctx.Err(); err != nil {
			return RevisionPreparation{}, err
		}
		if item.CaptureIntentID != "" {
			capture, err := preparer.prepareCapture(
				ctx, normalizedActor, request.RecordID, item.CaptureIntentID, additionalLogicalBytes,
			)
			if err != nil {
				return RevisionPreparation{}, err
			}
			if !addPreparedSnapshotIdentity(preparedSnapshotIDs, capture.SnapshotID()) {
				return RevisionPreparation{}, revisionPreparationRequestError("duplicate snapshot identity", nil)
			}
			captures = append(captures, capture)
			orderedSnapshotIDs = append(orderedSnapshotIDs, capture.SnapshotID())
			if capture.Snapshot().Size() > ^uint64(0)-additionalLogicalBytes {
				return RevisionPreparation{}, ErrCapacityArithmetic
			}
			additionalLogicalBytes += capture.Snapshot().Size()
			continue
		}

		reference, err := PrepareExistingSnapshotReference(
			ctx,
			preparer.references,
			normalizedActor.Clone(),
			request.RecordID,
			item.ExistingSnapshotID,
		)
		if err != nil {
			return RevisionPreparation{}, err
		}
		if !addPreparedSnapshotIdentity(preparedSnapshotIDs, reference.SnapshotID()) {
			return RevisionPreparation{}, revisionPreparationRequestError("duplicate snapshot identity", nil)
		}
		references = append(references, reference)
		orderedSnapshotIDs = append(orderedSnapshotIDs, reference.SnapshotID())
	}

	return NewRevisionPreparation(request.RecordID, RevisionPreparationValues{
		Captures: captures, References: references, OrderedSnapshotIDs: orderedSnapshotIDs,
	})
}

func (preparer *RevisionPreparer) prepareCapture(
	ctx context.Context,
	actor ActorScope,
	recordID string,
	intentID string,
	priorAdditionalBytes uint64,
) (PreparedCapture, error) {
	binding, err := preparer.intents.LoadCaptureIntentBinding(ctx, recordID, intentID)
	if err != nil {
		return PreparedCapture{}, fmt.Errorf("load evidence capture intent binding: %w", err)
	}
	if binding.RecordID != recordID || binding.Intent.ID != intentID || binding.Validate() != nil {
		return PreparedCapture{}, ErrInvalidCaptureIntentBinding
	}
	binding = cloneCaptureIntentBinding(binding)
	kind, err := preparer.registry.LookupKey(binding.Intent.Key)
	if err != nil {
		return PreparedCapture{}, err
	}
	descriptor := kind.Descriptor()
	if err := validateConformancePreview(descriptor, binding.Intent.Selection, binding.Preview); err != nil {
		return PreparedCapture{}, preparedCaptureError("persisted preview", err)
	}
	if err := validateConformanceIntent(descriptor, binding.Intent.Selection, binding.Preview, binding.Intent); err != nil {
		return PreparedCapture{}, preparedCaptureError("persisted intent", err)
	}
	selection := cloneSelection(binding.Intent.Selection)
	if err := kind.ValidateSelection(ctx, actor.Clone(), selection); err != nil {
		return PreparedCapture{}, fmt.Errorf("validate persisted evidence selection: %w", err)
	}
	authorization, err := kind.Authorize(ctx, actor.Clone(), cloneSelection(selection))
	if err != nil {
		return PreparedCapture{}, fmt.Errorf("reauthorize evidence source: %w", err)
	}
	snapshot, err := kind.Capture(ctx, actor.Clone(), cloneIntent(binding.Intent))
	if err != nil {
		return PreparedCapture{}, fmt.Errorf("recapture evidence source: %w", err)
	}
	if snapshot.Size() > ^uint64(0)-priorAdditionalBytes {
		return PreparedCapture{}, ErrCapacityArithmetic
	}
	capacity, err := preparer.capacity.Evaluate(
		ctx,
		string(actor.ProjectID),
		priorAdditionalBytes+snapshot.Size(),
	)
	if err != nil || capacity.Outcome.Status == QuotaUnavailable {
		return PreparedCapture{}, ErrCapacityUnavailable
	}
	if capacity.Outcome.Status == QuotaExceeded || capacity.Outcome != binding.Preview.QuotaOutcome {
		return PreparedCapture{}, ErrPreviewStale
	}
	snapshot, err = withSnapshotQuotaOutcome(snapshot, binding.Preview.QuotaOutcome)
	if err != nil {
		return PreparedCapture{}, preparedCaptureError("capacity outcome", err)
	}
	if err := validatePreparedCapture(
		binding.RecordID,
		binding.SnapshotID,
		descriptor,
		binding.Preview,
		binding.Intent,
		authorization,
		snapshot,
	); err != nil {
		return PreparedCapture{}, err
	}
	if err := preparer.payloads.PersistCapturePayload(ctx, snapshot); err != nil {
		return PreparedCapture{}, fmt.Errorf("persist evidence capture payload: %w", err)
	}
	return PrepareCapture(
		binding.RecordID,
		binding.SnapshotID,
		descriptor,
		binding.Preview,
		binding.Intent,
		authorization,
		snapshot,
	)
}

func withSnapshotQuotaOutcome(snapshot CanonicalSnapshot, outcome QuotaOutcome) (CanonicalSnapshot, error) {
	if err := validateCapturedQuotaOutcome(outcome); err != nil {
		return CanonicalSnapshot{}, err
	}
	cloned := cloneCanonicalSnapshot(snapshot)
	cloned.envelope.QuotaOutcome = outcome
	return cloned, nil
}

func validateRevisionPreparationRequest(request RevisionPreparationRequest) error {
	if !validClosedPreparedID(request.RecordID, "rec_") {
		return revisionPreparationRequestError("record identity", nil)
	}
	intentIDs := make(map[string]struct{}, len(request.Items))
	snapshotIDs := make(map[string]struct{}, len(request.Items))
	for _, item := range request.Items {
		hasIntent := item.CaptureIntentID != ""
		hasSnapshot := item.ExistingSnapshotID != ""
		if hasIntent == hasSnapshot {
			return revisionPreparationRequestError("item identity", nil)
		}
		if hasIntent {
			if !ValidCaptureIntentID(item.CaptureIntentID) {
				return revisionPreparationRequestError("capture intent identity", nil)
			}
			if _, exists := intentIDs[item.CaptureIntentID]; exists {
				return revisionPreparationRequestError("duplicate capture intent identity", nil)
			}
			intentIDs[item.CaptureIntentID] = struct{}{}
			continue
		}
		if !ValidSnapshotID(item.ExistingSnapshotID) {
			return revisionPreparationRequestError("existing snapshot identity", nil)
		}
		if _, exists := snapshotIDs[item.ExistingSnapshotID]; exists {
			return revisionPreparationRequestError("duplicate existing snapshot identity", nil)
		}
		snapshotIDs[item.ExistingSnapshotID] = struct{}{}
	}
	return nil
}

func cloneCaptureIntentBinding(binding CaptureIntentBinding) CaptureIntentBinding {
	binding.Intent = cloneIntent(binding.Intent)
	binding.Preview = clonePreview(binding.Preview)
	return binding
}

func nilRevisionPreparationDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func revisionPreparationRequestError(stage string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrInvalidRevisionPreparationRequest, stage)
	}
	return fmt.Errorf("%w: %s: %w", ErrInvalidRevisionPreparationRequest, stage, err)
}
