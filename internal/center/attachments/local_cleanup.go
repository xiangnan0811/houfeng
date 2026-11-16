package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

type LocalCleanupOperation struct {
	OperationID *string    `json:"operation_id"`
	OwnerID     *string    `json:"operation_owner_id"`
	Generation  *int64     `json:"operation_generation"`
	ExpiresAt   *time.Time `json:"operation_expires_at"`
}

type LocalCleanupPreparation struct {
	LocalCleanupOperation
	DeletionID      string     `json:"deletion_id"`
	NamespaceID     string     `json:"namespace_id"`
	IncarnationID   string     `json:"incarnation_id"`
	Key             string     `json:"key"`
	ExpectedVersion *string    `json:"expected_version"`
	OrphanedBefore  *time.Time `json:"orphaned_before"`
	PublicationID   *string    `json:"publication_id"`
}

type LocalCleanupIdentity struct {
	LocalCleanupOperation
	DeletionID    string `json:"deletion_id"`
	NamespaceID   string `json:"namespace_id"`
	IncarnationID string `json:"incarnation_id"`
	OwnerID       string `json:"cleanup_owner_id"`
	DispatchID    string `json:"cleanup_dispatch_id"`
}

type LocalCleanupClaim struct {
	LocalCleanupIdentity
	AnchorInode uint64     `json:"anchor_inode"`
	AnchorNonce string     `json:"anchor_nonce"`
	Generation  int64      `json:"cleanup_generation"`
	ExpiresAt   *time.Time `json:"cleanup_expires_at"`
}

type LocalCleanupCurrent struct {
	LocalCleanupIdentity
	Generation int64      `json:"cleanup_generation"`
	ExpiresAt  *time.Time `json:"cleanup_expires_at"`
}

type LocalCleanupFinalBinding struct {
	LocalCleanupCurrent
	Version         *string `json:"version"`
	Device          uint64  `json:"device"`
	Inode           uint64  `json:"inode"`
	InventoryDigest string  `json:"inventory_digest"`
}

type LocalCleanupCompletion struct {
	LocalCleanupCurrent
	Outcome         string `json:"outcome"`
	InventoryDigest string `json:"inventory_digest"`
}

type LocalCleanupTask struct {
	ID                 string     `json:"deletion_id"`
	NamespaceID        string     `json:"namespace_id"`
	IncarnationID      string     `json:"incarnation_id"`
	OperationID        *string    `json:"operation_id"`
	Key                string     `json:"blob_key"`
	Version            *string    `json:"object_version"`
	Size               *int64     `json:"size_bytes"`
	State              string     `json:"deletion_state"`
	OwnerID            *string    `json:"owner_id"`
	Generation         *int64     `json:"owner_generation"`
	ExpiresAt          *time.Time `json:"lease_expires_at"`
	DispatchID         *string    `json:"cleanup_dispatch_id"`
	FinalIdentityBound bool       `json:"final_identity_bound"`
	FinalWasPresent    *bool      `json:"final_was_present"`
	FinalDevice        *uint64    `json:"final_device"`
	FinalInode         *uint64    `json:"final_inode"`
	InventoryDigest    *string    `json:"inventory_digest"`
	Outcome            *string    `json:"physical_delete_result"`
	ReceiptDigest      *string    `json:"receipt_digest"`
	CompletedAt        *time.Time `json:"completed_at"`
}

type LocalCleanupSnapshot struct {
	Task        LocalCleanupTask  `json:"task"`
	Referenced  bool              `json:"referenced"`
	Obligations []WriteObligation `json:"obligations"`
}

type LocalCleanupRepository interface {
	PrepareLocalCleanup(context.Context, LocalCleanupPreparation) (LocalCleanupTask, error)
	ClaimLocalCleanup(context.Context, LocalCleanupClaim) (LocalCleanupSnapshot, error)
	ResolveLocalCleanup(context.Context, LocalCleanupIdentity) (LocalCleanupSnapshot, error)
	BindLocalCleanupFinal(context.Context, LocalCleanupFinalBinding) (LocalCleanupSnapshot, error)
	CompleteLocalCleanup(context.Context, LocalCleanupCompletion) (LocalCleanupSnapshot, error)
}

type LocalCleanupCoordinator struct {
	store      *LocalBlobStore
	repository LocalCleanupRepository
}

func NewLocalCleanupCoordinator(store *LocalBlobStore, repository LocalCleanupRepository) (*LocalCleanupCoordinator, error) {
	if store == nil || nilUploadServiceDependency(repository) {
		return nil, ErrInvalidBlobStoreConfig
	}
	return &LocalCleanupCoordinator{store: store, repository: repository}, nil
}

// WithRepository reuses the configured local store with a different authority
// repository while retaining the same namespace and incarnation.
func (coordinator *LocalCleanupCoordinator) WithRepository(repository LocalCleanupRepository) (*LocalCleanupCoordinator, error) {
	if coordinator == nil {
		return nil, ErrInvalidBlobStoreConfig
	}
	return NewLocalCleanupCoordinator(coordinator.store, repository)
}

func (coordinator *LocalCleanupCoordinator) NamespaceID() string {
	return coordinator.store.namespace.NamespaceID
}

func (coordinator *LocalCleanupCoordinator) IncarnationID() string {
	return coordinator.store.namespace.IncarnationID
}

type LocalBlobGCCandidate struct {
	Preparation LocalCleanupPreparation
	Candidate   BlobGCCandidate
}

type LocalBlobGCDiscovery interface {
	DiscoverLocalBlobGC(context.Context, BlobGCClaimRequest, string) (*LocalBlobGCCandidate, error)
}

type LocalPublicationCleanupDiscovery interface {
	DiscoverLocalPublicationCleanup(context.Context, BlobPublicationCleanupClaimRequest, string) (*LocalCleanupPreparation, error)
}

// Cleanup rediscovers and claims the task only after obtaining the key guard.
// Purge callers may prepare its pending handoff in their owner-removal SQL
// transaction, but that preparation never authorizes filesystem mutation.
func (coordinator *LocalCleanupCoordinator) Cleanup(ctx context.Context, request LocalCleanupPreparation, owner string) (LocalCleanupTask, error) {
	var result LocalCleanupTask
	if ctx == nil || coordinator == nil || owner == "" || request.DeletionID == "" {
		return result, ErrInvalidBlobRequest
	}
	n := coordinator.store.Namespace()
	if request.NamespaceID != n.NamespaceID || request.IncarnationID != n.IncarnationID {
		return result, ErrLocalIdentityChanged
	}
	err := coordinator.store.WithKeyGuard(ctx, request.Key, func(ctx context.Context, guard *LocalKeyGuard) error {
		task, err := coordinator.repository.PrepareLocalCleanup(ctx, request)
		if err != nil {
			return err
		}
		if !sameLocalCleanupScope(task, request) {
			return ErrLocalIdentityChanged
		}
		dispatch, err := newLocalUUID()
		if err != nil {
			return err
		}
		identity := LocalCleanupIdentity{LocalCleanupOperation: request.LocalCleanupOperation, DeletionID: task.ID, NamespaceID: request.NamespaceID, IncarnationID: request.IncarnationID, OwnerID: owner, DispatchID: dispatch}
		claim := LocalCleanupClaim{LocalCleanupIdentity: identity, AnchorInode: guard.anchor.Inode, AnchorNonce: guard.anchor.Nonce, ExpiresAt: task.ExpiresAt}
		if task.Generation != nil {
			claim.Generation = *task.Generation
		}
		snapshot, err := coordinator.repository.ClaimLocalCleanup(ctx, claim)
		if err != nil {
			finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), managedBlobFinalizationTimeout)
			defer cancel()
			resolved, resolveErr := coordinator.repository.ResolveLocalCleanup(finalCtx, identity)
			if resolveErr != nil {
				return errors.Join(ErrWriteOutcomeUnknown, err, resolveErr)
			}
			snapshot = resolved
		}
		if !sameLocalCleanupScope(snapshot.Task, request) {
			return ErrLocalIdentityChanged
		}
		if snapshot.Task.State == "completed" {
			result = snapshot.Task
			return nil
		}
		if !sameLocalCleanupDispatch(snapshot.Task, identity) {
			return ErrWriteOutcomeUnknown
		}
		current := LocalCleanupCurrent{LocalCleanupIdentity: identity, Generation: *snapshot.Task.Generation, ExpiresAt: snapshot.Task.ExpiresAt}
		inventory, err := localCleanupInventoryDigest(snapshot)
		if err != nil {
			return err
		}
		if !snapshot.Task.FinalIdentityBound {
			version, device, inode, err := coordinator.store.inspectCleanupFinal(ctx, guard, snapshot)
			if err != nil {
				return err
			}
			binding := LocalCleanupFinalBinding{LocalCleanupCurrent: current, Version: version, Device: device, Inode: inode, InventoryDigest: inventory}
			bound, err := coordinator.repository.BindLocalCleanupFinal(ctx, binding)
			if err != nil {
				finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), managedBlobFinalizationTimeout)
				defer cancel()
				bound, err = coordinator.repository.ResolveLocalCleanup(finalCtx, identity)
				if err != nil {
					return errors.Join(ErrWriteOutcomeUnknown, err)
				}
			}
			if !sameLocalCleanupDispatch(bound.Task, identity) || !bound.Task.FinalIdentityBound || !sameOptionalString(bound.Task.Version, version) {
				return ErrWriteOutcomeUnknown
			}
			snapshot = bound
		}
		if snapshot.Task.InventoryDigest == nil || *snapshot.Task.InventoryDigest != inventory {
			return ErrLocalIdentityChanged
		}
		// Exact temporary names remain inventory even after their original write
		// received a retained receipt. Never infer absence from a final-key stat.
		for _, obligation := range snapshot.Obligations {
			if err := coordinator.store.removeKnownTemporary(ctx, guard, obligation); err != nil {
				return err
			}
		}
		outcome, err := coordinator.store.cleanupFinal(ctx, guard, snapshot)
		if err != nil {
			return err
		}
		finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), managedBlobFinalizationTimeout)
		defer cancel()
		completed, err := coordinator.repository.CompleteLocalCleanup(finalCtx, LocalCleanupCompletion{LocalCleanupCurrent: current, Outcome: outcome, InventoryDigest: inventory})
		if err != nil {
			resolved, resolveErr := coordinator.repository.ResolveLocalCleanup(finalCtx, identity)
			if resolveErr != nil {
				return errors.Join(ErrWriteOutcomeUnknown, err, resolveErr)
			}
			completed = resolved
		}
		if !sameLocalCleanupDispatch(completed.Task, identity) || completed.Task.State != "completed" || completed.Task.Outcome == nil || *completed.Task.Outcome != outcome || completed.Task.InventoryDigest == nil || *completed.Task.InventoryDigest != inventory {
			return ErrWriteOutcomeUnknown
		}
		result = completed.Task
		return nil
	})
	return result, err
}

func sameLocalCleanupScope(task LocalCleanupTask, request LocalCleanupPreparation) bool {
	return task.ID == request.DeletionID && task.NamespaceID == request.NamespaceID && task.IncarnationID == request.IncarnationID && task.Key == request.Key && sameOptionalString(task.OperationID, request.OperationID)
}
func sameLocalCleanupDispatch(task LocalCleanupTask, identity LocalCleanupIdentity) bool {
	return task.ID == identity.DeletionID && task.NamespaceID == identity.NamespaceID && task.IncarnationID == identity.IncarnationID && sameOptionalString(task.OperationID, identity.OperationID) && task.OwnerID != nil && *task.OwnerID == identity.OwnerID && task.DispatchID != nil && *task.DispatchID == identity.DispatchID && task.Generation != nil && task.ExpiresAt != nil
}

func localCleanupInventoryDigest(snapshot LocalCleanupSnapshot) (string, error) {
	type target struct {
		ID        string
		Temporary string
	}
	targets := make([]target, 0, len(snapshot.Obligations))
	for _, o := range snapshot.Obligations {
		if o.NamespaceID == nil || o.IncarnationID == nil || *o.NamespaceID != snapshot.Task.NamespaceID || *o.IncarnationID != snapshot.Task.IncarnationID || o.Key != snapshot.Task.Key || o.TemporaryKey == nil || *o.TemporaryKey != "sha256/.blob-tmp-"+o.ID {
			return "", ErrLocalIdentityChanged
		}
		targets = append(targets, target{ID: o.ID, Temporary: *o.TemporaryKey})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	body, err := json.Marshal(struct {
		Namespace, Incarnation, Key string
		Targets                     []target
	}{snapshot.Task.NamespaceID, snapshot.Task.IncarnationID, snapshot.Task.Key, targets})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hexDigest(digest), nil
}
