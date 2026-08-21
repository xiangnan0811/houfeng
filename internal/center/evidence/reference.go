package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"

	"houfeng/internal/center/recordauth"
)

var (
	ErrInvalidPreparedReference   = errors.New("invalid prepared evidence reference")
	ErrInvalidRevisionPreparation = errors.New("invalid evidence revision preparation")
)

// ExistingSnapshotReferenceSource reauthorizes one existing logical snapshot
// outside the revision transaction. It returns identity and authorization
// metadata only; existing reference preparation never recaptures source facts
// or copies payload bytes.
type ExistingSnapshotReferenceSource interface {
	ReauthorizeExistingSnapshot(
		context.Context,
		ActorScope,
		string,
		string,
	) (ExistingSnapshotReferenceState, error)
}

type ExistingSnapshotReferenceState struct {
	RecordID                   string
	SnapshotID                 string
	Key                        KindKey
	SourceType                 string
	SourceID                   string
	CaptureAuthorizationDigest [sha256.Size]byte
	PayloadDigest              [sha256.Size]byte
	Authorization              AuthorizationScope
}

// PreparedReference is an immutable, server-owned authorization result for
// reusing an existing logical snapshot in a later revision commit.
type PreparedReference struct {
	recordID                   string
	snapshotID                 string
	key                        KindKey
	sourceType                 string
	sourceID                   string
	captureAuthorizationDigest [sha256.Size]byte
	payloadDigest              [sha256.Size]byte
	authorization              AuthorizationScope
	actorScopeDigest           [sha256.Size]byte
}

func PrepareExistingSnapshotReference(
	ctx context.Context,
	source ExistingSnapshotReferenceSource,
	actor ActorScope,
	recordID string,
	snapshotID string,
) (PreparedReference, error) {
	if ctx == nil || nilExistingSnapshotReferenceSource(source) ||
		!validClosedPreparedID(recordID, "rec_") || !ValidSnapshotID(snapshotID) {
		return PreparedReference{}, preparedReferenceError("request", nil)
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil || !reflect.DeepEqual(normalizedActor, actor) {
		return PreparedReference{}, preparedReferenceError("actor", err)
	}
	state, err := source.ReauthorizeExistingSnapshot(ctx, normalizedActor.Clone(), recordID, snapshotID)
	if err != nil {
		return PreparedReference{}, fmt.Errorf("reauthorize existing evidence snapshot: %w", err)
	}
	normalizedAuthorization, err := recordauth.NormalizeSourceAuthorization(state.Authorization)
	if err != nil || !reflect.DeepEqual(normalizedAuthorization, state.Authorization) {
		return PreparedReference{}, preparedReferenceError("authorization", err)
	}
	prepared := PreparedReference{
		recordID:                   state.RecordID,
		snapshotID:                 state.SnapshotID,
		key:                        state.Key,
		sourceType:                 state.SourceType,
		sourceID:                   state.SourceID,
		captureAuthorizationDigest: state.CaptureAuthorizationDigest,
		payloadDigest:              state.PayloadDigest,
		authorization:              normalizedAuthorization,
		actorScopeDigest:           normalizedActor.CanonicalHash(),
	}
	if state.RecordID != recordID || state.SnapshotID != snapshotID {
		return PreparedReference{}, preparedReferenceError("snapshot identity", nil)
	}
	if err := prepared.Validate(); err != nil {
		return PreparedReference{}, err
	}
	return prepared, nil
}

func (prepared PreparedReference) Validate() error {
	if !validClosedPreparedID(prepared.recordID, "rec_") || !ValidSnapshotID(prepared.snapshotID) {
		return preparedReferenceError("identity", nil)
	}
	if err := validateKnownKindKey(prepared.key); err != nil {
		return preparedReferenceError("kind", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(prepared.authorization)
	if err != nil || !reflect.DeepEqual(authorization, prepared.authorization) ||
		string(authorization.Kind) != prepared.sourceType || authorization.SourceID != prepared.sourceID ||
		prepared.captureAuthorizationDigest == [sha256.Size]byte{} ||
		prepared.payloadDigest == [sha256.Size]byte{} || prepared.actorScopeDigest == [sha256.Size]byte{} {
		return preparedReferenceError("authorization or payload identity", err)
	}
	return nil
}

func (prepared PreparedReference) RecordID() string { return prepared.recordID }

func (prepared PreparedReference) SnapshotID() string { return prepared.snapshotID }

func (prepared PreparedReference) Key() KindKey { return prepared.key }

func (prepared PreparedReference) SourceType() string { return prepared.sourceType }

func (prepared PreparedReference) SourceID() string { return prepared.sourceID }

func (prepared PreparedReference) CaptureAuthorizationDigest() [sha256.Size]byte {
	return prepared.captureAuthorizationDigest
}

func (prepared PreparedReference) PayloadDigest() [sha256.Size]byte { return prepared.payloadDigest }

func (prepared PreparedReference) Authorization() AuthorizationScope {
	return cloneAuthorizationScope(prepared.authorization)
}

func (prepared PreparedReference) ActorScopeDigest() [sha256.Size]byte {
	return prepared.actorScopeDigest
}

type RevisionPreparationValues struct {
	Captures           []PreparedCapture
	References         []PreparedReference
	Imported           []PreparedImportedSnapshot
	OrderedSnapshotIDs []string
	ComparisonSave     ComparisonSavePreparation
}

// RevisionPreparation explicitly transports immutable evidence preparation
// into a revision commit. The ordered IDs are canonical revision content.
type RevisionPreparation struct {
	recordID       string
	captures       []PreparedCapture
	references     []PreparedReference
	imported       []PreparedImportedSnapshot
	snapshotIDs    []string
	comparisonSave ComparisonSavePreparation
}

func NewRevisionPreparation(recordID string, values RevisionPreparationValues) (RevisionPreparation, error) {
	prepared := RevisionPreparation{
		recordID:       recordID,
		captures:       clonePreparedCaptures(values.Captures),
		references:     clonePreparedReferences(values.References),
		imported:       cloneImportedSnapshots(values.Imported),
		snapshotIDs:    append([]string(nil), values.OrderedSnapshotIDs...),
		comparisonSave: cloneComparisonSavePreparation(values.ComparisonSave),
	}
	if err := prepared.ValidateForRecord(recordID); err != nil {
		return RevisionPreparation{}, err
	}
	return prepared, nil
}

func (prepared RevisionPreparation) Empty() bool {
	return prepared.recordID == "" && len(prepared.captures) == 0 &&
		len(prepared.references) == 0 && len(prepared.imported) == 0 &&
		len(prepared.snapshotIDs) == 0 && prepared.comparisonSave.Empty()
}

func (prepared RevisionPreparation) ValidateForRecord(recordID string) error {
	if prepared.Empty() {
		if !validClosedPreparedID(recordID, "rec_") {
			return revisionPreparationError("record identity", nil)
		}
		return nil
	}
	if !validClosedPreparedID(recordID, "rec_") || prepared.recordID != recordID {
		return revisionPreparationError("record identity", nil)
	}
	available := make(map[string]struct{}, len(prepared.captures)+len(prepared.references)+len(prepared.imported)+len(prepared.comparisonSave.Copies)+1)
	for _, capture := range prepared.captures {
		if capture.RecordID() != recordID || capture.Validate() != nil || !addPreparedSnapshotIdentity(available, capture.SnapshotID()) {
			return revisionPreparationError("capture", nil)
		}
	}
	for _, reference := range prepared.references {
		if reference.RecordID() != recordID || reference.Validate() != nil || !addPreparedSnapshotIdentity(available, reference.SnapshotID()) {
			return revisionPreparationError("reference", nil)
		}
	}
	for _, imported := range prepared.imported {
		if imported.RecordID() != recordID || imported.Validate() != nil || !addPreparedSnapshotIdentity(available, imported.SnapshotID()) {
			return revisionPreparationError("imported snapshot", nil)
		}
	}
	for _, copy := range prepared.comparisonSave.Copies {
		if copy.RecordID() != recordID || copy.Validate() != nil || !addPreparedSnapshotIdentity(available, copy.SnapshotID()) {
			return revisionPreparationError("comparison copy", nil)
		}
	}
	if !prepared.comparisonSave.Result.Empty() {
		if prepared.comparisonSave.Result.RecordID() != recordID || prepared.comparisonSave.Result.Validate() != nil ||
			!addPreparedSnapshotIdentity(available, prepared.comparisonSave.Result.SnapshotID()) {
			return revisionPreparationError("comparison result", nil)
		}
	}
	if len(prepared.snapshotIDs) != len(available) {
		return revisionPreparationError("ordered snapshot identities", nil)
	}
	ordered := make(map[string]struct{}, len(prepared.snapshotIDs))
	for _, snapshotID := range prepared.snapshotIDs {
		if !ValidSnapshotID(snapshotID) {
			return revisionPreparationError("ordered snapshot identity", nil)
		}
		if _, exists := ordered[snapshotID]; exists {
			return revisionPreparationError("duplicate ordered snapshot identity", nil)
		}
		if _, exists := available[snapshotID]; !exists {
			return revisionPreparationError("unprepared snapshot identity", nil)
		}
		ordered[snapshotID] = struct{}{}
	}
	return nil
}

func (prepared RevisionPreparation) ValidateReferencesForActor(actor ActorScope) error {
	normalized, err := recordauth.NormalizeActorScope(actor)
	if err != nil || !reflect.DeepEqual(normalized, actor) {
		return revisionPreparationError("actor", err)
	}
	digest := normalized.CanonicalHash()
	for _, reference := range prepared.references {
		if reference.ActorScopeDigest() != digest {
			return revisionPreparationError("reference actor", nil)
		}
	}
	return nil
}

func (prepared RevisionPreparation) SnapshotIDs() []string {
	return append([]string{}, prepared.snapshotIDs...)
}

func (prepared RevisionPreparation) Captures() []PreparedCapture {
	return clonePreparedCaptures(prepared.captures)
}

func (prepared RevisionPreparation) References() []PreparedReference {
	return clonePreparedReferences(prepared.references)
}

func (prepared RevisionPreparation) ComparisonSave() ComparisonSavePreparation {
	return cloneComparisonSavePreparation(prepared.comparisonSave)
}

func (prepared RevisionPreparation) Imported() []PreparedImportedSnapshot {
	return cloneImportedSnapshots(prepared.imported)
}

func ValidSnapshotID(value string) bool {
	return validClosedPreparedID(value, "evs_")
}

func cloneAuthorizationScope(value AuthorizationScope) AuthorizationScope {
	cloned, err := recordauth.NormalizeSourceAuthorization(value)
	if err != nil {
		return AuthorizationScope{}
	}
	return cloned
}

func clonePreparedCaptures(values []PreparedCapture) []PreparedCapture {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]PreparedCapture, len(values))
	for index, value := range values {
		cloned[index] = PreparedCapture{
			recordID:      value.RecordID(),
			snapshotID:    value.SnapshotID(),
			descriptor:    value.Descriptor(),
			preview:       value.Preview(),
			intent:        value.Intent(),
			authorization: value.Authorization(),
			snapshot:      value.Snapshot(),
		}
	}
	return cloned
}

func cloneComparisonSavePreparation(value ComparisonSavePreparation) ComparisonSavePreparation {
	if value.Empty() {
		return ComparisonSavePreparation{}
	}
	cloned := value
	cloned.Copies = append([]PreparedComparisonCopy(nil), value.Copies...)
	return cloned
}

func clonePreparedReferences(values []PreparedReference) []PreparedReference {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]PreparedReference, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].authorization = value.Authorization()
	}
	return cloned
}

func addPreparedSnapshotIdentity(seen map[string]struct{}, snapshotID string) bool {
	if !ValidSnapshotID(snapshotID) {
		return false
	}
	if _, exists := seen[snapshotID]; exists {
		return false
	}
	seen[snapshotID] = struct{}{}
	return true
}

func nilExistingSnapshotReferenceSource(source ExistingSnapshotReferenceSource) bool {
	if source == nil {
		return true
	}
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func preparedReferenceError(stage string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrInvalidPreparedReference, stage)
	}
	return fmt.Errorf("%w: %s: %w", ErrInvalidPreparedReference, stage, err)
}

func revisionPreparationError(stage string, err error) error {
	if err == nil {
		return fmt.Errorf("%w: %s", ErrInvalidRevisionPreparation, stage)
	}
	return fmt.Errorf("%w: %s: %w", ErrInvalidRevisionPreparation, stage, err)
}
