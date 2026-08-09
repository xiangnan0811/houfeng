package attachments

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	DefaultBlobGCOrphanGracePeriod = 24 * time.Hour
	DefaultBlobGCLeaseDuration     = time.Minute
	DefaultBlobGCRetryDelay        = 30 * time.Second
)

var (
	ErrInvalidBlobGCRequest = errors.New("invalid Blob GC request")
	ErrBlobGCProtected      = errors.New("Blob GC object protected")
	ErrBlobGCConflict       = errors.New("Blob GC conflict")
	ErrBlobGCClaimLost      = errors.New("Blob GC claim lost")
)

type BlobGCPurgeMode string

const (
	BlobGCPurgeModeOrdinary  BlobGCPurgeMode = "ordinary"
	BlobGCPurgeModePermanent BlobGCPurgeMode = "permanent"
)

type BlobGCClaimRequest struct {
	ProjectID          string
	BackendKind        BackendKind
	Mode               BlobGCPurgeMode
	OrphanedBefore     time.Time
	Object             BlobObject
	OwnerID            string
	OwnerLeaseDuration time.Duration
}

func (request BlobGCClaimRequest) Validate() error {
	if request.ProjectID != "default" ||
		(request.BackendKind != BackendKindLocal && request.BackendKind != BackendKindS3) ||
		!validBlobGCPurgeMode(request.Mode) || !validBlobGCPinOwnerID(request.OwnerID) ||
		request.OwnerLeaseDuration != DefaultBlobGCLeaseDuration {
		return ErrInvalidBlobGCRequest
	}
	switch request.Mode {
	case BlobGCPurgeModeOrdinary:
		if request.OrphanedBefore.IsZero() || request.Object != (BlobObject{}) {
			return ErrInvalidBlobGCRequest
		}
	case BlobGCPurgeModePermanent:
		if request.OrphanedBefore != (time.Time{}) || request.Object.Validate() != nil ||
			request.Object.BackendKind != request.BackendKind {
			return ErrInvalidBlobGCRequest
		}
	}
	return nil
}

type BlobGCCandidate struct {
	Object    BlobObject
	CreatedAt time.Time
}

func (candidate BlobGCCandidate) Validate() error {
	if candidate.Object.Validate() != nil || candidate.CreatedAt.IsZero() {
		return ErrInvalidBlobGCRequest
	}
	return nil
}

type BlobGCClaim struct {
	DeletionID      string
	ProjectID       string
	Mode            BlobGCPurgeMode
	Candidate       BlobGCCandidate
	OwnerID         string
	OwnerGeneration int64
	Attempt         int64
	LeaseExpiresAt  time.Time
}

func (claim BlobGCClaim) Validate() error {
	if !validPrefixedID(claim.DeletionID, "bgd_") || claim.ProjectID != "default" ||
		!validBlobGCPurgeMode(claim.Mode) || claim.Candidate.Validate() != nil ||
		!validBlobGCPinOwnerID(claim.OwnerID) || claim.OwnerGeneration <= 0 || claim.Attempt <= 0 ||
		claim.LeaseExpiresAt.IsZero() {
		return ErrInvalidBlobGCRequest
	}
	return nil
}

type BlobGCCompletionRequest struct {
	Claim   BlobGCClaim
	Receipt DeletionReceipt
}

func (request BlobGCCompletionRequest) Validate() error {
	if request.Claim.Validate() != nil ||
		request.Receipt.Version != objectVersionFromGCObject(request.Claim.Candidate.Object) {
		return ErrInvalidBlobGCRequest
	}
	return nil
}

type BlobGCRetryRequest struct {
	Claim   BlobGCClaim
	RetryAt time.Time
}

func (request BlobGCRetryRequest) Validate() error {
	if request.Claim.Validate() != nil || request.RetryAt.IsZero() {
		return ErrInvalidBlobGCRequest
	}
	return nil
}

type BlobGCResolveRequest struct {
	Claim   BlobGCClaim
	Receipt DeletionReceipt
}

func (request BlobGCResolveRequest) Validate() error {
	return BlobGCCompletionRequest(request).Validate()
}

type BlobGCPurgeResult struct {
	DeletionID string
	Candidate  BlobGCCandidate
	Receipt    DeletionReceipt
}

func (result BlobGCPurgeResult) validate(
	request BlobGCClaimRequest,
	claim BlobGCClaim,
	receipt DeletionReceipt,
) error {
	if result.DeletionID != claim.DeletionID || result.Candidate != claim.Candidate ||
		result.Receipt != receipt || result.Receipt.Version != objectVersionFromGCObject(result.Candidate.Object) ||
		claimMatchesBlobGCRequest(claim, request) != nil {
		return ErrBlobGCConflict
	}
	return nil
}

type BlobGCRepository interface {
	ClaimBlobGC(context.Context, BlobGCClaimRequest) (*BlobGCClaim, error)
	CompleteBlobGC(context.Context, BlobGCCompletionRequest) (BlobGCPurgeResult, error)
	RetryBlobGC(context.Context, BlobGCRetryRequest) error
	ResolveBlobGC(context.Context, BlobGCResolveRequest) (*BlobGCPurgeResult, error)
}

type BlobGCWorkerOptions struct {
	ProjectID          string
	BackendKind        BackendKind
	OwnerID            string
	OrphanGracePeriod  time.Duration
	OwnerLeaseDuration time.Duration
	RetryDelay         time.Duration
	Now                func() time.Time
}

type BlobGCWorker struct {
	repository         BlobGCRepository
	blob               BlobStore
	projectID          string
	backendKind        BackendKind
	ownerID            string
	orphanGracePeriod  time.Duration
	ownerLeaseDuration time.Duration
	retryDelay         time.Duration
	now                func() time.Time
}

func NewBlobGCWorker(
	repository BlobGCRepository,
	blob BlobStore,
	options BlobGCWorkerOptions,
) (*BlobGCWorker, error) {
	if nilUploadServiceDependency(repository) || nilUploadServiceDependency(blob) {
		return nil, ErrInvalidBlobGCRequest
	}
	if options.ProjectID == "" {
		options.ProjectID = "default"
	}
	if options.OwnerID == "" {
		options.OwnerID = "blob_gc_worker"
	}
	if options.OrphanGracePeriod == 0 {
		options.OrphanGracePeriod = DefaultBlobGCOrphanGracePeriod
	}
	if options.OwnerLeaseDuration == 0 {
		options.OwnerLeaseDuration = DefaultBlobGCLeaseDuration
	}
	if options.RetryDelay == 0 {
		options.RetryDelay = DefaultBlobGCRetryDelay
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.ProjectID != "default" || !validBlobGCPinOwnerID(options.OwnerID) ||
		(options.BackendKind != BackendKindLocal && options.BackendKind != BackendKindS3) ||
		options.OrphanGracePeriod != DefaultBlobGCOrphanGracePeriod ||
		options.OwnerLeaseDuration != DefaultBlobGCLeaseDuration ||
		options.RetryDelay != DefaultBlobGCRetryDelay {
		return nil, ErrInvalidBlobGCRequest
	}
	return &BlobGCWorker{
		repository: repository, blob: blob, projectID: options.ProjectID,
		backendKind: options.BackendKind, ownerID: options.OwnerID,
		orphanGracePeriod: options.OrphanGracePeriod, ownerLeaseDuration: options.OwnerLeaseDuration,
		retryDelay: options.RetryDelay, now: options.Now,
	}, nil
}

func (worker *BlobGCWorker) RunOnce(ctx context.Context) (bool, error) {
	if err := worker.validate(ctx); err != nil {
		return false, err
	}
	now := worker.now().UTC()
	if now.IsZero() {
		return false, ErrInvalidBlobGCRequest
	}
	request := BlobGCClaimRequest{
		ProjectID: worker.projectID, BackendKind: worker.backendKind, Mode: BlobGCPurgeModeOrdinary,
		OrphanedBefore: now.Add(-worker.orphanGracePeriod), OwnerID: worker.ownerID,
		OwnerLeaseDuration: worker.ownerLeaseDuration,
	}
	result, claimed, err := worker.execute(ctx, request, now)
	if err != nil {
		return claimed, err
	}
	if result == nil {
		return false, nil
	}
	return true, nil
}

func (worker *BlobGCWorker) PurgePermanent(
	ctx context.Context,
	object BlobObject,
) (BlobGCPurgeResult, error) {
	if err := worker.validate(ctx); err != nil || object.Validate() != nil ||
		object.BackendKind != worker.backendKind {
		return BlobGCPurgeResult{}, ErrInvalidBlobGCRequest
	}
	now := worker.now().UTC()
	if now.IsZero() {
		return BlobGCPurgeResult{}, ErrInvalidBlobGCRequest
	}
	request := BlobGCClaimRequest{
		ProjectID: worker.projectID, BackendKind: worker.backendKind,
		Mode: BlobGCPurgeModePermanent, Object: object, OwnerID: worker.ownerID,
		OwnerLeaseDuration: worker.ownerLeaseDuration,
	}
	result, _, err := worker.execute(ctx, request, now)
	if err != nil {
		return BlobGCPurgeResult{}, err
	}
	if result == nil {
		return BlobGCPurgeResult{}, ErrBlobGCProtected
	}
	return *result, nil
}

func (worker *BlobGCWorker) execute(
	ctx context.Context,
	request BlobGCClaimRequest,
	now time.Time,
) (*BlobGCPurgeResult, bool, error) {
	if request.Validate() != nil {
		return nil, false, ErrInvalidBlobGCRequest
	}
	claim, err := worker.repository.ClaimBlobGC(ctx, request)
	if err != nil {
		return nil, false, err
	}
	if claim == nil {
		return nil, false, nil
	}
	if err := claimMatchesBlobGCRequest(*claim, request); err != nil || !claim.LeaseExpiresAt.After(now) {
		return nil, true, ErrBlobGCConflict
	}
	version := objectVersionFromGCObject(claim.Candidate.Object)
	receipt, deleteErr := worker.blob.Delete(ctx, version)
	if deleteErr != nil {
		return nil, true, worker.scheduleRetry(ctx, *claim, now, fmt.Errorf("delete exact GC Blob: %w", deleteErr))
	}
	if receipt.Version != version {
		return nil, true, worker.scheduleRetry(ctx, *claim, now, ErrBlobGCConflict)
	}
	completion := BlobGCCompletionRequest{Claim: *claim, Receipt: receipt}
	result, err := worker.repository.CompleteBlobGC(ctx, completion)
	if err == nil {
		if result.validate(request, *claim, receipt) != nil {
			return nil, true, ErrBlobGCConflict
		}
		return &result, true, nil
	}
	resolved, resolveErr := worker.repository.ResolveBlobGC(ctx, BlobGCResolveRequest(completion))
	if resolveErr == nil && resolved != nil && resolved.validate(request, *claim, receipt) == nil {
		return resolved, true, nil
	}
	if resolveErr != nil {
		return nil, true, errors.Join(err, fmt.Errorf("resolve Blob GC completion: %w", resolveErr))
	}
	return nil, true, err
}

func (worker *BlobGCWorker) scheduleRetry(
	ctx context.Context,
	claim BlobGCClaim,
	now time.Time,
	cause error,
) error {
	retryErr := worker.repository.RetryBlobGC(ctx, BlobGCRetryRequest{
		Claim: claim, RetryAt: now.Add(worker.retryDelay),
	})
	if retryErr != nil {
		return errors.Join(cause, fmt.Errorf("schedule Blob GC retry: %w", retryErr))
	}
	return cause
}

func (worker *BlobGCWorker) validate(ctx context.Context) error {
	if ctx == nil || worker == nil || nilUploadServiceDependency(worker.repository) ||
		nilUploadServiceDependency(worker.blob) || worker.now == nil ||
		worker.projectID != "default" || !validBlobGCPinOwnerID(worker.ownerID) ||
		(worker.backendKind != BackendKindLocal && worker.backendKind != BackendKindS3) ||
		worker.orphanGracePeriod != DefaultBlobGCOrphanGracePeriod ||
		worker.ownerLeaseDuration != DefaultBlobGCLeaseDuration || worker.retryDelay != DefaultBlobGCRetryDelay {
		return ErrInvalidBlobGCRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func claimMatchesBlobGCRequest(claim BlobGCClaim, request BlobGCClaimRequest) error {
	if claim.Validate() != nil || request.Validate() != nil || claim.ProjectID != request.ProjectID ||
		claim.Mode != request.Mode || claim.OwnerID != request.OwnerID ||
		claim.Candidate.Object.BackendKind != request.BackendKind {
		return ErrBlobGCConflict
	}
	switch request.Mode {
	case BlobGCPurgeModeOrdinary:
		if claim.Candidate.CreatedAt.After(request.OrphanedBefore.UTC()) {
			return ErrBlobGCConflict
		}
	case BlobGCPurgeModePermanent:
		if claim.Candidate.Object != request.Object {
			return ErrBlobGCConflict
		}
	default:
		return ErrBlobGCConflict
	}
	return nil
}

func validBlobGCPurgeMode(mode BlobGCPurgeMode) bool {
	return mode == BlobGCPurgeModeOrdinary || mode == BlobGCPurgeModePermanent
}

func objectVersionFromGCObject(object BlobObject) ObjectVersion {
	return ObjectVersion{
		Key: object.Key, VersionID: object.ObjectVersion, SHA256: object.SHA256, SizeBytes: object.SizeBytes,
	}
}
