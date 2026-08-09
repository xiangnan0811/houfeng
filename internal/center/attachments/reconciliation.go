package attachments

import (
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"time"
)

const maxTemporaryObjectReconciliationRetryDelay = 24 * time.Hour

var ErrTemporaryObjectReconciliationConflict = errors.New("temporary object reconciliation conflict")

var ErrProcessorWorkspaceReconciliationConflict = errors.New("processor workspace reconciliation conflict")

type ProcessorWorkspaceCleanupClaimInput struct {
	ProjectID  string
	RetryDelay time.Duration
}

func (input ProcessorWorkspaceCleanupClaimInput) Validate() error {
	if input.ProjectID != "default" || input.RetryDelay < time.Microsecond ||
		input.RetryDelay > maxTemporaryObjectReconciliationRetryDelay {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorWorkspaceCleanupCandidate struct {
	WorkspaceID         string
	WorkspacePathDigest [sha256.Size]byte
}

func (candidate ProcessorWorkspaceCleanupCandidate) Validate() error {
	if ValidateWorkspaceID(candidate.WorkspaceID) != nil ||
		candidate.WorkspacePathDigest == [sha256.Size]byte{} {
		return ErrInvalidProcessorCommand
	}
	return nil
}

type ProcessorWorkspaceReconcilerRepository interface {
	ProcessorWorkspaceRepository
	ClaimProcessorWorkspaceCleanup(context.Context, ProcessorWorkspaceCleanupClaimInput) (*ProcessorWorkspaceCleanupCandidate, error)
}

type ProcessorWorkspaceReconcilerConfig struct {
	Root           string
	CleanupTimeout time.Duration
	RetryDelay     time.Duration
	Cutpoint       func(ProcessorWorkspaceCutpoint) error
}

type ProcessorWorkspaceReconciler struct {
	repository ProcessorWorkspaceReconcilerRepository
	janitor    *WorkspaceJanitor
	config     ProcessorWorkspaceReconcilerConfig
}

func NewProcessorWorkspaceReconciler(
	repository ProcessorWorkspaceReconcilerRepository,
	config ProcessorWorkspaceReconcilerConfig,
) (*ProcessorWorkspaceReconciler, error) {
	if nilUploadServiceDependency(repository) || validateWorkspaceRootConfiguration(config.Root) != nil ||
		config.CleanupTimeout <= 0 || (ProcessorWorkspaceCleanupClaimInput{
		ProjectID: "default", RetryDelay: config.RetryDelay,
	}).Validate() != nil {
		return nil, ErrInvalidProcessorCommand
	}
	config.Root = filepath.Clean(config.Root)
	janitor := newWorkspaceJanitor(config.Root, repository, config.CleanupTimeout)
	janitor.cutpoint = config.Cutpoint
	return &ProcessorWorkspaceReconciler{repository: repository, janitor: janitor, config: config}, nil
}

func (reconciler *ProcessorWorkspaceReconciler) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil || reconciler == nil || nilUploadServiceDependency(reconciler.repository) ||
		reconciler.janitor == nil {
		return false, ErrInvalidProcessorCommand
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	candidate, err := reconciler.repository.ClaimProcessorWorkspaceCleanup(ctx, ProcessorWorkspaceCleanupClaimInput{
		ProjectID: "default", RetryDelay: reconciler.config.RetryDelay,
	})
	if err != nil {
		return false, err
	}
	if candidate == nil {
		return false, nil
	}
	if candidate.Validate() != nil {
		return true, ErrProcessorWorkspaceReconciliationConflict
	}
	_, err = reconciler.janitor.Purge(ctx, ProcessorWorkspaceTransition{
		WorkspaceID: candidate.WorkspaceID, WorkspacePathDigest: candidate.WorkspacePathDigest,
		Authorization: NewProcessorWorkspaceReconciliationAuthorization(),
	})
	return true, err
}

// TemporaryObjectCleanupCandidate is the only durable identity a restart
// reconciler may use.  It intentionally carries no bucket or object-listing
// capability: the key is selected by PostgreSQL and the version is either
// persisted there or obtained with one exact-key lookup.
type TemporaryObjectCleanupCandidate struct {
	ProjectID              string
	UploadID               string
	AuthorID               string
	TemporaryObjectKey     string
	TemporaryObjectVersion string
	State                  UploadState
	ExpiresAt              time.Time
}

func (candidate TemporaryObjectCleanupCandidate) Validate() error {
	if candidate.ProjectID != "default" || ValidateUploadID(candidate.UploadID) != nil ||
		!validPrefixedID(candidate.AuthorID, "usr_") || !validS3BlobTemporaryKey(candidate.TemporaryObjectKey) ||
		(candidate.TemporaryObjectVersion != "" && !validS3BlobCleanupVersionID(candidate.TemporaryObjectVersion)) ||
		!knownTemporaryObjectCleanupState(candidate.State) || candidate.ExpiresAt.IsZero() {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

func knownTemporaryObjectCleanupState(state UploadState) bool {
	switch state {
	case UploadStateUploading, UploadStateQuarantined, UploadStateAvailable,
		UploadStateRejected, UploadStateExpired:
		return true
	default:
		return false
	}
}

type TemporaryObjectCleanupClaimInput struct {
	ProjectID  string
	RetryDelay time.Duration
}

func (input TemporaryObjectCleanupClaimInput) Validate() error {
	if input.ProjectID != "default" || input.RetryDelay < time.Microsecond ||
		input.RetryDelay > maxTemporaryObjectReconciliationRetryDelay {
		return ErrInvalidAttachmentCommand
	}
	return nil
}

type TemporaryObjectReconcilerRepository interface {
	ClaimTemporaryObjectCleanup(context.Context, TemporaryObjectCleanupClaimInput) (*TemporaryObjectCleanupCandidate, error)
	RecordTemporaryObjectVersion(context.Context, RecordTemporaryObjectVersionCommand) (UploadPreparation, error)
	MarkTemporaryObjectCleaned(context.Context, TemporaryObjectCleanupCandidate) error
}

type TemporaryObjectReconcilerCutpoint string

const (
	TemporaryObjectReconcilerCutpointAfterClaim          TemporaryObjectReconcilerCutpoint = "after_claim"
	TemporaryObjectReconcilerCutpointAfterVersionResolve TemporaryObjectReconcilerCutpoint = "after_version_resolve"
	TemporaryObjectReconcilerCutpointAfterVersionCAS     TemporaryObjectReconcilerCutpoint = "after_version_cas"
	TemporaryObjectReconcilerCutpointAfterPhysicalPurge  TemporaryObjectReconcilerCutpoint = "after_physical_purge"
	TemporaryObjectReconcilerCutpointAfterCompletion     TemporaryObjectReconcilerCutpoint = "after_completion"
)

type TemporaryObjectReconcilerConfig struct {
	ProjectID  string
	RetryDelay time.Duration
	Cutpoint   func(TemporaryObjectReconcilerCutpoint) error
}

type TemporaryObjectReconciler struct {
	repository TemporaryObjectReconcilerRepository
	store      TemporaryObjectStore
	config     TemporaryObjectReconcilerConfig
}

func NewTemporaryObjectReconciler(
	repository TemporaryObjectReconcilerRepository,
	store TemporaryObjectStore,
	config TemporaryObjectReconcilerConfig,
) (*TemporaryObjectReconciler, error) {
	if nilUploadServiceDependency(repository) || nilUploadServiceDependency(store) ||
		config.ProjectID != "default" || config.RetryDelay < time.Microsecond ||
		config.RetryDelay > maxTemporaryObjectReconciliationRetryDelay {
		return nil, ErrInvalidAttachmentCommand
	}
	return &TemporaryObjectReconciler{repository: repository, store: store, config: config}, nil
}

// RunOnce performs one bounded cleanup.  A missing current version is not
// treated as proof of cleanup when the DB has no observed version; the row is
// left eligible for a later bounded retry.  Once a version is persisted,
// idempotent absence is safe because it can represent a crash after delete.
func (reconciler *TemporaryObjectReconciler) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil || reconciler == nil || nilUploadServiceDependency(reconciler.repository) ||
		nilUploadServiceDependency(reconciler.store) {
		return false, ErrInvalidAttachmentCommand
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	candidate, err := reconciler.repository.ClaimTemporaryObjectCleanup(ctx, TemporaryObjectCleanupClaimInput{
		ProjectID: reconciler.config.ProjectID, RetryDelay: reconciler.config.RetryDelay,
	})
	if err != nil {
		return false, err
	}
	if candidate == nil {
		return false, nil
	}
	if candidate.Validate() != nil {
		return true, ErrTemporaryObjectReconciliationConflict
	}
	if err := reconciler.hitCutpoint(TemporaryObjectReconcilerCutpointAfterClaim); err != nil {
		return true, err
	}

	version := candidate.TemporaryObjectVersion
	if version == "" {
		resolved, resolveErr := reconciler.store.ResolveTemporaryVersion(ctx, candidate.TemporaryObjectKey)
		if resolveErr != nil {
			return true, resolveErr
		}
		if resolved.Validate() != nil || resolved.Key != candidate.TemporaryObjectKey {
			return true, ErrTemporaryObjectReconciliationConflict
		}
		if err := reconciler.hitCutpoint(TemporaryObjectReconcilerCutpointAfterVersionResolve); err != nil {
			return true, err
		}
		preparation, recordErr := reconciler.repository.RecordTemporaryObjectVersion(ctx, RecordTemporaryObjectVersionCommand{
			ProjectID: candidate.ProjectID, UploadID: candidate.UploadID, AuthorID: candidate.AuthorID,
			TemporaryObjectKey: resolved.Key, TemporaryObjectVersion: resolved.VersionID,
		})
		if recordErr != nil {
			return true, recordErr
		}
		if preparation.ProjectID != candidate.ProjectID || preparation.UploadID != candidate.UploadID ||
			preparation.AuthorID != candidate.AuthorID || preparation.TransportKind != TransportKindS3 ||
			preparation.TemporaryObjectKey != resolved.Key || preparation.TemporaryObjectVersion != resolved.VersionID {
			return true, ErrTemporaryObjectReconciliationConflict
		}
		version = resolved.VersionID
		candidate.TemporaryObjectVersion = version
		if err := reconciler.hitCutpoint(TemporaryObjectReconcilerCutpointAfterVersionCAS); err != nil {
			return true, err
		}
	}

	deleteErr := reconciler.store.DeleteTemporaryVersion(ctx, TemporaryObjectVersion{
		Key: candidate.TemporaryObjectKey, VersionID: version,
	})
	if deleteErr != nil && !errors.Is(deleteErr, ErrBlobNotFound) {
		return true, deleteErr
	}
	if err := reconciler.hitCutpoint(TemporaryObjectReconcilerCutpointAfterPhysicalPurge); err != nil {
		return true, err
	}
	if err := reconciler.repository.MarkTemporaryObjectCleaned(ctx, *candidate); err != nil {
		return true, err
	}
	if err := reconciler.hitCutpoint(TemporaryObjectReconcilerCutpointAfterCompletion); err != nil {
		return true, err
	}
	return true, nil
}

func (reconciler *TemporaryObjectReconciler) hitCutpoint(cutpoint TemporaryObjectReconcilerCutpoint) error {
	if reconciler.config.Cutpoint == nil {
		return nil
	}
	return reconciler.config.Cutpoint(cutpoint)
}
