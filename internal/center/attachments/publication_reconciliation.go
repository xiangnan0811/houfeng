package attachments

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	DefaultBlobPublicationCleanupRetryDelay = 30 * time.Second
	maxBlobPublicationReconciliationRetry   = 24 * time.Hour
)

var ErrBlobPublicationReconciliationConflict = errors.New("Blob publication reconciliation conflict")

// BlobPublicationResolver performs one exact-key lookup for a final digest
// object.  It intentionally exposes no listing or prefix-scan capability.
type BlobPublicationResolver interface {
	ResolveBlobPublicationObject(context.Context, BlobPublicationTarget) (ObjectVersion, error)
}

// BlobPublicationRepository is the durable publisher half of the final-object
// protocol. Callers must prepare before external final-object I/O and bind the
// exact observed version before consuming the intent in their metadata tx.
type BlobPublicationRepository interface {
	PrepareBlobPublication(context.Context, BlobPublicationPrepareRequest) (BlobPublicationIntent, error)
	RecordBlobPublicationVersion(context.Context, BlobPublicationVersionRequest) (BlobPublicationIntent, error)
}

// BlobPublicationCleanupRepository is the durable half of the restart
// protocol.  All methods bind the caller's exact claim identity.
type BlobPublicationCleanupRepository interface {
	ClaimBlobPublicationCleanup(context.Context, BlobPublicationCleanupClaimRequest) (*BlobPublicationCleanupClaim, error)
	RecordBlobPublicationCleanupVersion(context.Context, BlobPublicationCleanupVersionRequest) (BlobPublicationCleanupClaim, error)
	RetryBlobPublicationCleanup(context.Context, BlobPublicationCleanupRetryRequest) error
	CompleteBlobPublicationCleanup(context.Context, BlobPublicationCleanupCompletionRequest) (BlobPublicationCleanupResult, error)
}

type BlobPublicationReconcilerCutpoint string

const (
	BlobPublicationReconcilerCutpointAfterClaim          BlobPublicationReconcilerCutpoint = "after_claim"
	BlobPublicationReconcilerCutpointAfterVersionResolve BlobPublicationReconcilerCutpoint = "after_version_resolve"
	BlobPublicationReconcilerCutpointAfterVersionCAS     BlobPublicationReconcilerCutpoint = "after_version_cas"
	BlobPublicationReconcilerCutpointAfterPhysicalPurge  BlobPublicationReconcilerCutpoint = "after_physical_purge"
	BlobPublicationReconcilerCutpointAfterCompletion     BlobPublicationReconcilerCutpoint = "after_completion"
)

type BlobPublicationReconcilerConfig struct {
	ProjectID          string
	BackendKind        BackendKind
	CleanupOwnerID     string
	OwnerLeaseDuration time.Duration
	RetryDelay         time.Duration
	Now                func() time.Time
	Cutpoint           func(BlobPublicationReconcilerCutpoint) error
}

type BlobPublicationReconciler struct {
	repository BlobPublicationCleanupRepository
	resolver   BlobPublicationResolver
	blob       BlobStore
	projectID  string
	backend    BackendKind
	ownerID    string
	lease      time.Duration
	retryDelay time.Duration
	now        func() time.Time
	cutpoint   func(BlobPublicationReconcilerCutpoint) error
}

func NewBlobPublicationReconciler(
	repository BlobPublicationCleanupRepository,
	resolver BlobPublicationResolver,
	blob BlobStore,
	config BlobPublicationReconcilerConfig,
) (*BlobPublicationReconciler, error) {
	if nilUploadServiceDependency(repository) || nilUploadServiceDependency(resolver) || nilUploadServiceDependency(blob) {
		return nil, ErrInvalidBlobPublicationRequest
	}
	if config.ProjectID == "" {
		config.ProjectID = "default"
	}
	if config.OwnerLeaseDuration == 0 {
		config.OwnerLeaseDuration = DefaultBlobPublicationCleanupLeaseDuration
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = DefaultBlobPublicationCleanupRetryDelay
	}
	if config.CleanupOwnerID == "" {
		config.CleanupOwnerID = "blob_publication_reconciler"
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ProjectID != "default" ||
		(config.BackendKind != BackendKindLocal && config.BackendKind != BackendKindS3) ||
		!validBlobGCPinOwnerID(config.CleanupOwnerID) ||
		config.OwnerLeaseDuration != DefaultBlobPublicationCleanupLeaseDuration ||
		config.RetryDelay < time.Microsecond || config.RetryDelay > maxBlobPublicationReconciliationRetry {
		return nil, ErrInvalidBlobPublicationRequest
	}
	return &BlobPublicationReconciler{
		repository: repository, resolver: resolver, blob: blob,
		projectID: config.ProjectID, backend: config.BackendKind, ownerID: config.CleanupOwnerID,
		lease: config.OwnerLeaseDuration, retryDelay: config.RetryDelay,
		now: config.Now, cutpoint: config.Cutpoint,
	}, nil
}

// RunOnce claims at most one intent and never discovers work through object
// listing.  A missing exact final key is terminal only when the resolver
// returns ErrBlobNotFound for that one key.
func (reconciler *BlobPublicationReconciler) RunOnce(ctx context.Context) (bool, error) {
	if err := reconciler.validate(ctx); err != nil {
		return false, err
	}
	now := reconciler.now().UTC()
	if now.IsZero() {
		return false, ErrInvalidBlobPublicationRequest
	}
	claim, err := reconciler.repository.ClaimBlobPublicationCleanup(ctx, BlobPublicationCleanupClaimRequest{
		ProjectID: reconciler.projectID, BackendKind: reconciler.backend,
		CleanupOwnerID: reconciler.ownerID, OwnerLeaseDuration: reconciler.lease,
	})
	if err != nil {
		return false, err
	}
	if claim == nil {
		return false, nil
	}
	if claim.Validate() != nil || claim.Intent.ProjectID != reconciler.projectID ||
		claim.Intent.Target.BackendKind != reconciler.backend || claim.CleanupOwnerID != reconciler.ownerID ||
		!claim.ObservedLeaseExpiresAt.After(now) {
		return true, ErrBlobPublicationReconciliationConflict
	}
	if err := reconciler.hitCutpoint(BlobPublicationReconcilerCutpointAfterClaim); err != nil {
		return true, err
	}

	var object ObjectVersion
	if claim.Intent.ObjectVersion == "" {
		object, err = reconciler.resolver.ResolveBlobPublicationObject(ctx, claim.Intent.Target)
		if err != nil {
			if errors.Is(err, ErrBlobNotFound) {
				return true, reconciler.complete(ctx, *claim, BlobPublicationCompletionOutcomeAlreadyAbsent, DeletionReceipt{})
			}
			return true, reconciler.retry(ctx, *claim, now, fmt.Errorf("resolve exact Blob publication object: %w", err))
		}
		if err := reconciler.hitCutpoint(BlobPublicationReconcilerCutpointAfterVersionResolve); err != nil {
			return true, err
		}
		updated, casErr := reconciler.repository.RecordBlobPublicationCleanupVersion(ctx, BlobPublicationCleanupVersionRequest{
			Claim: *claim, Object: object,
		})
		if casErr != nil {
			return true, casErr
		}
		if updated.Intent.ObjectVersion != object.VersionID || updated.Intent.Target != claim.Intent.Target ||
			updated.CleanupOwnerID != claim.CleanupOwnerID || updated.CleanupGeneration != claim.CleanupGeneration ||
			updated.Attempt != claim.Attempt || updated.ObservedLeaseExpiresAt != claim.ObservedLeaseExpiresAt {
			return true, ErrBlobPublicationReconciliationConflict
		}
		*claim = updated
		if err := reconciler.hitCutpoint(BlobPublicationReconcilerCutpointAfterVersionCAS); err != nil {
			return true, err
		}
	} else {
		physical, ok := claim.Intent.Object()
		if !ok {
			return true, ErrBlobPublicationReconciliationConflict
		}
		object = objectVersionFromPublicationBlobObject(physical)
		if err := reconciler.hitCutpoint(BlobPublicationReconcilerCutpointAfterVersionResolve); err != nil {
			return true, err
		}
	}

	receipt, deleteErr := reconciler.blob.Delete(ctx, object)
	if deleteErr != nil && !errors.Is(deleteErr, ErrBlobNotFound) {
		return true, reconciler.retry(ctx, *claim, now, fmt.Errorf("delete exact Blob publication object: %w", deleteErr))
	}
	if deleteErr != nil {
		receipt = DeletionReceipt{Version: object}
	}
	if receipt.Version != object {
		return true, reconciler.retry(ctx, *claim, now, ErrBlobPublicationReconciliationConflict)
	}
	outcome := BlobPublicationCompletionOutcomeAlreadyAbsent
	if receipt.Deleted {
		outcome = BlobPublicationCompletionOutcomeDeleted
	}
	if err := reconciler.hitCutpoint(BlobPublicationReconcilerCutpointAfterPhysicalPurge); err != nil {
		return true, err
	}
	return true, reconciler.complete(ctx, *claim, outcome, receipt)
}

func (reconciler *BlobPublicationReconciler) complete(
	ctx context.Context,
	claim BlobPublicationCleanupClaim,
	outcome BlobPublicationCompletionOutcome,
	receipt DeletionReceipt,
) error {
	completion := BlobPublicationCleanupCompletionRequest{Claim: claim, Outcome: outcome, Receipt: receipt}
	if completion.Validate() != nil {
		return ErrBlobPublicationReconciliationConflict
	}
	result, err := reconciler.repository.CompleteBlobPublicationCleanup(ctx, completion)
	if err != nil {
		return err
	}
	if err := result.ValidateAgainst(completion); err != nil {
		return ErrBlobPublicationReconciliationConflict
	}
	if err := reconciler.hitCutpoint(BlobPublicationReconcilerCutpointAfterCompletion); err != nil {
		return err
	}
	return nil
}

func (reconciler *BlobPublicationReconciler) retry(
	ctx context.Context,
	claim BlobPublicationCleanupClaim,
	now time.Time,
	cause error,
) error {
	retryErr := reconciler.repository.RetryBlobPublicationCleanup(ctx, BlobPublicationCleanupRetryRequest{
		Claim: claim, RetryAt: now.Add(reconciler.retryDelay),
	})
	if retryErr != nil {
		return errors.Join(cause, fmt.Errorf("schedule Blob publication cleanup retry: %w", retryErr))
	}
	return cause
}

func (reconciler *BlobPublicationReconciler) validate(ctx context.Context) error {
	if ctx == nil || reconciler == nil || nilUploadServiceDependency(reconciler.repository) ||
		nilUploadServiceDependency(reconciler.resolver) || nilUploadServiceDependency(reconciler.blob) ||
		reconciler.now == nil || reconciler.projectID != "default" ||
		(reconciler.backend != BackendKindLocal && reconciler.backend != BackendKindS3) ||
		!validBlobGCPinOwnerID(reconciler.ownerID) ||
		reconciler.lease != DefaultBlobPublicationCleanupLeaseDuration ||
		reconciler.retryDelay < time.Microsecond || reconciler.retryDelay > maxBlobPublicationReconciliationRetry {
		return ErrInvalidBlobPublicationRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (reconciler *BlobPublicationReconciler) hitCutpoint(cutpoint BlobPublicationReconcilerCutpoint) error {
	if reconciler == nil || reconciler.cutpoint == nil {
		return nil
	}
	return reconciler.cutpoint(cutpoint)
}

func newBlobPublicationReconcilerResult(
	completion BlobPublicationCleanupCompletionRequest,
) BlobPublicationCleanupResult {
	result := BlobPublicationCleanupResult{
		PublicationID: completion.Claim.Intent.PublicationID,
		Outcome:       completion.Outcome,
		Receipt:       completion.Receipt,
	}
	if object, ok := completion.Claim.Intent.Object(); ok {
		result.Object = object
	}
	return result
}

func objectVersionFromPublicationBlobObject(object BlobObject) ObjectVersion {
	return ObjectVersion{
		Key: object.Key, VersionID: object.ObjectVersion,
		SHA256: object.SHA256, SizeBytes: object.SizeBytes,
	}
}
