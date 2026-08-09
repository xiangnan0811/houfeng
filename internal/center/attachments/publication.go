package attachments

import (
	"crypto/sha256"
	"errors"
	"math"
	"time"
)

var (
	ErrInvalidBlobPublicationRequest = errors.New("invalid blob publication request")
	ErrBlobPublicationConflict       = errors.New("blob publication conflict")
	ErrBlobPublicationClaimLost      = errors.New("blob publication claim lost")
)

const DefaultBlobPublicationCleanupLeaseDuration = 5 * time.Minute

type BlobPublicationOwnerKind string

const (
	BlobPublicationOwnerUpload           BlobPublicationOwnerKind = "upload"
	BlobPublicationOwnerProcessorPreview BlobPublicationOwnerKind = "processor_preview"
)

type BlobPublicationState string

const (
	BlobPublicationStatePrepared       BlobPublicationState = "prepared"
	BlobPublicationStatePublished      BlobPublicationState = "published"
	BlobPublicationStateCleanupClaimed BlobPublicationState = "cleanup_claimed"
	BlobPublicationStateRetryWait      BlobPublicationState = "retry_wait"
	BlobPublicationStateCompleted      BlobPublicationState = "completed"
)

type BlobPublicationCompletionOutcome string

const (
	BlobPublicationCompletionOutcomeConsumed      BlobPublicationCompletionOutcome = "consumed"
	BlobPublicationCompletionOutcomeDeleted       BlobPublicationCompletionOutcome = "deleted"
	BlobPublicationCompletionOutcomeAlreadyAbsent BlobPublicationCompletionOutcome = "already_absent"
)

type BlobPublicationTarget struct {
	Key         string
	SHA256      [sha256.Size]byte
	SizeBytes   int64
	BackendKind BackendKind
}

func (target BlobPublicationTarget) Validate() error {
	if target.SHA256 == ([sha256.Size]byte{}) || target.Key != "sha256/"+hexDigest(target.SHA256) ||
		target.SizeBytes <= 0 || target.SizeBytes == math.MaxInt64 ||
		(target.BackendKind != BackendKindLocal && target.BackendKind != BackendKindS3) {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

type BlobPublicationPrepareRequest struct {
	ProjectID        string
	OwnerKind        BlobPublicationOwnerKind
	OwnerID          string
	OwnerGeneration  int64
	Target           BlobPublicationTarget
	PublishExpiresAt time.Time
}

func (request BlobPublicationPrepareRequest) Validate() error {
	if request.ProjectID != "default" || !validBlobPublicationOwner(
		request.OwnerKind,
		request.OwnerID,
		request.OwnerGeneration,
	) || request.Target.Validate() != nil || request.PublishExpiresAt.IsZero() {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

type BlobPublicationIntent struct {
	PublicationID    string
	ProjectID        string
	OwnerKind        BlobPublicationOwnerKind
	OwnerID          string
	OwnerGeneration  int64
	Target           BlobPublicationTarget
	ObjectVersion    string
	State            BlobPublicationState
	PublishExpiresAt time.Time
}

func (intent BlobPublicationIntent) Validate() error {
	prepare := BlobPublicationPrepareRequest{
		ProjectID: intent.ProjectID, OwnerKind: intent.OwnerKind, OwnerID: intent.OwnerID,
		OwnerGeneration: intent.OwnerGeneration, Target: intent.Target,
		PublishExpiresAt: intent.PublishExpiresAt,
	}
	if !validBlobPublicationID(intent.PublicationID) || prepare.Validate() != nil ||
		!validBlobPublicationState(intent.State) {
		return ErrInvalidBlobPublicationRequest
	}

	switch intent.State {
	case BlobPublicationStatePrepared:
		if intent.ObjectVersion != "" {
			return ErrInvalidBlobPublicationRequest
		}
	case BlobPublicationStatePublished:
		if !validBlobPublicationVersionID(intent.ObjectVersion) {
			return ErrInvalidBlobPublicationRequest
		}
	case BlobPublicationStateCleanupClaimed, BlobPublicationStateRetryWait, BlobPublicationStateCompleted:
		if intent.ObjectVersion != "" && !validBlobPublicationVersionID(intent.ObjectVersion) {
			return ErrInvalidBlobPublicationRequest
		}
	}
	return nil
}

func (intent BlobPublicationIntent) Object() (BlobObject, bool) {
	if intent.Validate() != nil || intent.ObjectVersion == "" {
		return BlobObject{}, false
	}
	switch intent.State {
	case BlobPublicationStatePublished, BlobPublicationStateCleanupClaimed,
		BlobPublicationStateRetryWait, BlobPublicationStateCompleted:
	default:
		return BlobObject{}, false
	}
	object := BlobObject{
		Key: intent.Target.Key, SHA256: intent.Target.SHA256,
		ObjectVersion: intent.ObjectVersion, SizeBytes: intent.Target.SizeBytes,
		BackendKind: intent.Target.BackendKind,
	}
	if object.Validate() != nil {
		return BlobObject{}, false
	}
	return object, true
}

type BlobPublicationVersionRequest struct {
	Intent BlobPublicationIntent
	Object ObjectVersion
}

func (request BlobPublicationVersionRequest) Validate() error {
	if request.Intent.Validate() != nil || request.Intent.State != BlobPublicationStatePrepared ||
		!validBlobPublicationObservedObject(request.Object) ||
		!blobPublicationObjectMatchesTarget(request.Object, request.Intent.Target) {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

type BlobPublicationCleanupClaimRequest struct {
	ProjectID          string
	BackendKind        BackendKind
	CleanupOwnerID     string
	OwnerLeaseDuration time.Duration
}

func (request BlobPublicationCleanupClaimRequest) Validate() error {
	if request.ProjectID != "default" ||
		(request.BackendKind != BackendKindLocal && request.BackendKind != BackendKindS3) ||
		!validBlobGCPinOwnerID(request.CleanupOwnerID) ||
		request.OwnerLeaseDuration != DefaultBlobPublicationCleanupLeaseDuration {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

type BlobPublicationCleanupClaim struct {
	Intent                 BlobPublicationIntent
	CleanupOwnerID         string
	CleanupGeneration      int64
	Attempt                int64
	ObservedLeaseExpiresAt time.Time
}

func (claim BlobPublicationCleanupClaim) Validate() error {
	if claim.Intent.Validate() != nil || claim.Intent.State != BlobPublicationStateCleanupClaimed ||
		!validBlobGCPinOwnerID(claim.CleanupOwnerID) || claim.CleanupGeneration <= 0 ||
		claim.Attempt <= 0 || claim.ObservedLeaseExpiresAt.IsZero() {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

type BlobPublicationCleanupRetryRequest struct {
	Claim   BlobPublicationCleanupClaim
	RetryAt time.Time
}

func (request BlobPublicationCleanupRetryRequest) Validate() error {
	if request.Claim.Validate() != nil || request.RetryAt.IsZero() {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

// BlobPublicationCleanupVersionRequest binds a resolver's exact observation
// to the cleanup claim that is allowed to persist it.  The claim may have no
// object version yet, but it must already be fenced by the cleanup owner.
type BlobPublicationCleanupVersionRequest struct {
	Claim  BlobPublicationCleanupClaim
	Object ObjectVersion
}

func (request BlobPublicationCleanupVersionRequest) Validate() error {
	if request.Claim.Validate() != nil || request.Claim.Intent.ObjectVersion != "" ||
		!blobPublicationObjectMatchesTarget(request.Object, request.Claim.Intent.Target) {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

type BlobPublicationCleanupCompletionRequest struct {
	Claim   BlobPublicationCleanupClaim
	Outcome BlobPublicationCompletionOutcome
	Receipt DeletionReceipt
}

func (request BlobPublicationCleanupCompletionRequest) Validate() error {
	if request.Claim.Validate() != nil || !validBlobPublicationCleanupOutcome(request.Outcome) {
		return ErrInvalidBlobPublicationRequest
	}
	if request.Claim.Intent.ObjectVersion == "" {
		if request.Outcome != BlobPublicationCompletionOutcomeAlreadyAbsent ||
			request.Receipt != (DeletionReceipt{}) {
			return ErrInvalidBlobPublicationRequest
		}
		return nil
	}
	object, ok := request.Claim.Intent.Object()
	if !ok || request.Receipt.Version != objectVersionFromPublicationObject(object) ||
		request.Receipt.Deleted != (request.Outcome == BlobPublicationCompletionOutcomeDeleted) {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

type BlobPublicationCleanupResult struct {
	PublicationID string
	Object        BlobObject
	Outcome       BlobPublicationCompletionOutcome
	Receipt       DeletionReceipt
}

func (result BlobPublicationCleanupResult) Validate() error {
	if !validBlobPublicationID(result.PublicationID) || !validBlobPublicationCleanupOutcome(result.Outcome) {
		return ErrInvalidBlobPublicationRequest
	}
	if result.Object == (BlobObject{}) {
		if result.Outcome != BlobPublicationCompletionOutcomeAlreadyAbsent ||
			result.Receipt != (DeletionReceipt{}) {
			return ErrInvalidBlobPublicationRequest
		}
		return nil
	}
	if !validBlobPublicationObject(result.Object) ||
		result.Receipt.Version != objectVersionFromPublicationObject(result.Object) ||
		result.Receipt.Deleted != (result.Outcome == BlobPublicationCompletionOutcomeDeleted) {
		return ErrInvalidBlobPublicationRequest
	}
	return nil
}

func (result BlobPublicationCleanupResult) ValidateAgainst(
	completion BlobPublicationCleanupCompletionRequest,
) error {
	if completion.Validate() != nil || result.Validate() != nil {
		return ErrInvalidBlobPublicationRequest
	}
	expected := BlobPublicationCleanupResult{
		PublicationID: completion.Claim.Intent.PublicationID,
		Outcome:       completion.Outcome,
		Receipt:       completion.Receipt,
	}
	if object, ok := completion.Claim.Intent.Object(); ok {
		expected.Object = object
	}
	if result != expected {
		return ErrBlobPublicationConflict
	}
	return nil
}

func validBlobPublicationOwner(kind BlobPublicationOwnerKind, ownerID string, generation int64) bool {
	switch kind {
	case BlobPublicationOwnerUpload:
		return ValidateUploadID(ownerID) == nil && generation == 1
	case BlobPublicationOwnerProcessorPreview:
		return ValidateProcessorJobID(ownerID) == nil && generation > 0
	default:
		return false
	}
}

func validBlobPublicationState(state BlobPublicationState) bool {
	switch state {
	case BlobPublicationStatePrepared, BlobPublicationStatePublished,
		BlobPublicationStateCleanupClaimed, BlobPublicationStateRetryWait,
		BlobPublicationStateCompleted:
		return true
	default:
		return false
	}
}

func validBlobPublicationVersionID(version string) bool {
	return version != "" && len(version) <= 1024
}

func validBlobPublicationID(value string) bool {
	const prefix = "bpi_"
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

func validBlobPublicationObservedObject(object ObjectVersion) bool {
	return object.Validate() == nil && object.SizeBytes != math.MaxInt64
}

func blobPublicationObjectMatchesTarget(object ObjectVersion, target BlobPublicationTarget) bool {
	return validBlobPublicationObservedObject(object) && object.Key == target.Key &&
		object.SHA256 == target.SHA256 && object.SizeBytes == target.SizeBytes
}

func validBlobPublicationObject(value BlobObject) bool {
	if value.Validate() != nil || value.SizeBytes == math.MaxInt64 {
		return false
	}
	return (BlobPublicationTarget{
		Key: value.Key, SHA256: value.SHA256, SizeBytes: value.SizeBytes, BackendKind: value.BackendKind,
	}).Validate() == nil
}

func validBlobPublicationCleanupOutcome(outcome BlobPublicationCompletionOutcome) bool {
	return outcome == BlobPublicationCompletionOutcomeDeleted || outcome == BlobPublicationCompletionOutcomeAlreadyAbsent
}

func objectVersionFromPublicationObject(object BlobObject) ObjectVersion {
	return ObjectVersion{
		Key: object.Key, VersionID: object.ObjectVersion,
		SHA256: object.SHA256, SizeBytes: object.SizeBytes,
	}
}
