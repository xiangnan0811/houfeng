package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"reflect"
)

const RecordDeletionContractVersionV1 uint64 = 1

const recoverySurfaceDigestDomainV1 = "houfeng.record-deletion.recovery-surfaces.v1"

var (
	ErrInvalidRecoveryAdapter      = errors.New("invalid record deletion recovery adapter")
	ErrRecoveryContractUnavailable = errors.New("record deletion recovery contract unavailable")
)

type RecoverySurface string

const (
	RecoverySurfaceContentDeliveryEpochs      RecoverySurface = "content_delivery_epochs"
	RecoverySurfaceDeletionFenceLeases        RecoverySurface = "deletion_fence_leases"
	RecoverySurfaceDeletionReplayState        RecoverySurface = "deletion_replay_state"
	RecoverySurfaceDeletionReservations       RecoverySurface = "deletion_reservations"
	RecoverySurfaceRecordCorePurgeReceipts    RecoverySurface = "record_core_purge_receipts"
	RecoverySurfaceRecordDeletionAudits       RecoverySurface = "record_deletion_audits"
	RecoverySurfaceRecordDomainActivities     RecoverySurface = "record_domain_activities"
	RecoverySurfaceRecordDraftCheckpoints     RecoverySurface = "record_draft_checkpoints"
	RecoverySurfaceRecordDrafts               RecoverySurface = "record_drafts"
	RecoverySurfaceRecordPurgeOperations      RecoverySurface = "record_purge_operations"
	RecoverySurfaceRecordRevisionParticipants RecoverySurface = "record_revision_participants"
	RecoverySurfaceRecordRevisionSubjects     RecoverySurface = "record_revision_subjects"
	RecoverySurfaceRecordRevisionTags         RecoverySurface = "record_revision_tags"
	RecoverySurfaceRecordRevisions            RecoverySurface = "record_revisions"
	RecoverySurfaceRecords                    RecoverySurface = "records"
)

var recordDeleteRecoverySurfaces = []RecoverySurface{
	RecoverySurfaceContentDeliveryEpochs,
	RecoverySurfaceDeletionFenceLeases,
	RecoverySurfaceDeletionReplayState,
	RecoverySurfaceDeletionReservations,
	RecoverySurfaceRecordCorePurgeReceipts,
	RecoverySurfaceRecordDeletionAudits,
	RecoverySurfaceRecordDomainActivities,
	RecoverySurfaceRecordDraftCheckpoints,
	RecoverySurfaceRecordDrafts,
	RecoverySurfaceRecordPurgeOperations,
	RecoverySurfaceRecordRevisionParticipants,
	RecoverySurfaceRecordRevisionSubjects,
	RecoverySurfaceRecordRevisionTags,
	RecoverySurfaceRecordRevisions,
	RecoverySurfaceRecords,
}

var recordNotCommittedRecoverySurfaces = []RecoverySurface{
	RecoverySurfaceDeletionFenceLeases,
	RecoverySurfaceDeletionReplayState,
	RecoverySurfaceDeletionReservations,
	RecoverySurfaceRecordDeletionAudits,
	RecoverySurfaceRecordPurgeOperations,
}

func RecordDeleteRecoverySurfaces() []RecoverySurface {
	return append([]RecoverySurface(nil), recordDeleteRecoverySurfaces...)
}

func RecordNotCommittedRecoverySurfaces() []RecoverySurface {
	return append([]RecoverySurface(nil), recordNotCommittedRecoverySurfaces...)
}

type RecoveryReplayCursor struct {
	Sequence  uint64
	EntryHash [sha256.Size]byte
}

func (cursor RecoveryReplayCursor) validate() error {
	if cursor.Sequence == 0 {
		if !zeroDeletionDigest(cursor.EntryHash) {
			return ErrRecoveryContractUnavailable
		}
		return nil
	}
	if zeroDeletionDigest(cursor.EntryHash) {
		return ErrRecoveryContractUnavailable
	}
	return nil
}

// RecoveryReplayRequest identifies a ledger entry and its expected predecessor.
// Entry contents, fingerprint and witness proof come from the verifier.
type RecoveryReplayRequest struct {
	Cursor  RecoveryReplayCursor
	Locator LedgerLocator
}

type RecoveryReplayCommand struct {
	Cursor                RecoveryReplayCursor
	VerifiedEntry         VerifiedLedgerEntry
	MaintenanceRID        string
	MaintenanceGeneration uint64
	AllowedSurfaces       []RecoverySurface
	SurfaceDigest         [sha256.Size]byte
	PurgeContent          bool
	fence                 RecoveryMaintenanceFence
}

type RecoveryReplayReceipt struct {
	Sequence      uint64
	EntryHash     [sha256.Size]byte
	SurfaceDigest [sha256.Size]byte
	ContentPurged bool
}

type RecoveryStore interface {
	ApplyRecoveryEntry(context.Context, RecoveryReplayCommand) (RecoveryReplayReceipt, error)
}

// RecoveryMaintenanceFence is the trusted management coordinator's live
// offline fence. Its lifetime encloses independent verification and replay.
type RecoveryMaintenanceFence interface {
	LedgerContinuityGuard
	WithHeldLedgerFence(context.Context, func(context.Context) error) error
	RecoveryBinding() (string, uint64, error)
	NeverEnrolled() (bool, error)
}

type RecoveryAdapter struct {
	store    RecoveryStore
	verifier *LedgerVerifier
	fence    RecoveryMaintenanceFence
}

func NewRecoveryAdapter(store RecoveryStore, verifier *LedgerVerifier, fence RecoveryMaintenanceFence) (*RecoveryAdapter, error) {
	if nilDeletionServiceDependency(store) || verifier == nil || nilDeletionServiceDependency(fence) {
		return nil, ErrInvalidRecoveryAdapter
	}
	// The continuity check and replay lifetime must use the same concrete fence.
	fenceValue := reflect.ValueOf(fence)
	if fenceValue.Kind() != reflect.Pointer || fenceValue != reflect.ValueOf(verifier.trust.Continuity) {
		return nil, ErrInvalidRecoveryAdapter
	}
	return &RecoveryAdapter{store: store, verifier: verifier, fence: fence}, nil
}

func (adapter *RecoveryAdapter) Replay(ctx context.Context, request RecoveryReplayRequest) (RecoveryReplayReceipt, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) || adapter.verifier == nil || nilDeletionServiceDependency(adapter.fence) {
		return RecoveryReplayReceipt{}, ErrInvalidRecoveryAdapter
	}
	if err := ctx.Err(); err != nil {
		return RecoveryReplayReceipt{}, err
	}
	if request.Cursor.validate() != nil || request.Cursor.Sequence >= math.MaxInt64 ||
		request.Locator.Sequence != request.Cursor.Sequence+1 || zeroDeletionDigest(request.Locator.LedgerDomain) ||
		zeroDeletionDigest(request.Locator.EntryHash) {
		return RecoveryReplayReceipt{}, ErrRecoveryContractUnavailable
	}
	var receipt RecoveryReplayReceipt
	err := adapter.fence.WithHeldLedgerFence(ctx, func(heldCtx context.Context) error {
		rid, generation, err := adapter.fence.RecoveryBinding()
		if err != nil {
			return err
		}
		neverEnrolled, err := adapter.fence.NeverEnrolled()
		if err != nil {
			return err
		}
		if rid == "" || generation > math.MaxInt64 || (generation == 0) != neverEnrolled {
			return ErrRecoveryContractUnavailable
		}
		verified, err := adapter.verifier.Verify(heldCtx, request.Locator)
		if err != nil {
			return err
		}
		surfaces, purgeContent, err := recoveryEntrySurfaces(request.Cursor, verified)
		if err != nil {
			return err
		}
		entry := verified.Entry()
		surfaceDigest := digestRecoverySurfaces(entry.Request.EntryType, surfaces)
		command := RecoveryReplayCommand{
			Cursor: request.Cursor, VerifiedEntry: verified, MaintenanceRID: rid, MaintenanceGeneration: generation,
			AllowedSurfaces: surfaces, SurfaceDigest: surfaceDigest, PurgeContent: purgeContent, fence: adapter.fence,
		}
		receipt, err = adapter.store.ApplyRecoveryEntry(heldCtx, command)
		if err != nil {
			return err
		}
		if receipt.Sequence != request.Locator.Sequence || receipt.EntryHash != request.Locator.EntryHash ||
			receipt.SurfaceDigest != surfaceDigest || receipt.ContentPurged != purgeContent {
			return ErrRecoveryContractUnavailable
		}
		currentRID, currentGeneration, err := adapter.fence.RecoveryBinding()
		if err != nil {
			return err
		}
		if currentRID != rid || currentGeneration != generation {
			return ErrRecoveryContractUnavailable
		}
		return nil
	})
	if err != nil {
		return RecoveryReplayReceipt{}, err
	}
	return receipt, nil
}

func recoveryEntrySurfaces(cursor RecoveryReplayCursor, verified VerifiedLedgerEntry) ([]RecoverySurface, bool, error) {
	if cursor.validate() != nil || cursor.Sequence >= math.MaxInt64 || !verified.Valid() {
		return nil, false, ErrRecoveryContractUnavailable
	}
	entry := verified.Entry()
	if entry.Sequence != cursor.Sequence+1 || entry.PreviousHash != cursor.EntryHash || entry.Request.Object.ObjectKind != "record" {
		return nil, false, ErrRecoveryContractUnavailable
	}
	switch entry.Request.EntryType {
	case LedgerEntryDeleteCommit:
		if entry.Request.DeletionContractVersion != RecordDeletionContractVersionV1 || entry.Request.ReleaseEpoch != 0 {
			return nil, false, ErrRecoveryContractUnavailable
		}
		return RecordDeleteRecoverySurfaces(), true, nil
	case LedgerEntryAttemptNotCommitted:
		if entry.Request.DeletionContractVersion != 0 || entry.Request.ReleaseEpoch == 0 {
			return nil, false, ErrRecoveryContractUnavailable
		}
		return RecordNotCommittedRecoverySurfaces(), false, nil
	default:
		return nil, false, ErrRecoveryContractUnavailable
	}
}

func digestRecoverySurfaces(entryType LedgerEntryType, surfaces []RecoverySurface) [sha256.Size]byte {
	payload := make([]byte, 0, 512)
	payload = appendLengthPrefixed(payload, recoverySurfaceDigestDomainV1)
	payload = appendUint64(payload, 1)
	payload = appendLengthPrefixed(payload, string(entryType))
	payload = appendUint64(payload, uint64(len(surfaces)))
	for _, surface := range surfaces {
		payload = appendLengthPrefixed(payload, string(surface))
	}
	return sha256.Sum256(payload)
}

func (command RecoveryReplayCommand) Validate() error {
	expected, purge, err := recoveryEntrySurfaces(command.Cursor, command.VerifiedEntry)
	if err != nil || nilDeletionServiceDependency(command.fence) || command.MaintenanceRID == "" ||
		command.MaintenanceGeneration > math.MaxInt64 ||
		purge != command.PurgeContent || command.SurfaceDigest != digestRecoverySurfaces(command.VerifiedEntry.Entry().Request.EntryType, expected) ||
		!equalRecoverySurfaces(command.AllowedSurfaces, expected) {
		return fmt.Errorf("%w: replay command", ErrRecoveryContractUnavailable)
	}
	rid, generation, err := command.fence.RecoveryBinding()
	if err != nil || rid != command.MaintenanceRID || generation != command.MaintenanceGeneration {
		return fmt.Errorf("%w: maintenance binding", ErrRecoveryContractUnavailable)
	}
	neverEnrolled, err := command.fence.NeverEnrolled()
	if err != nil || (generation == 0) != neverEnrolled {
		return fmt.Errorf("%w: maintenance enrollment", ErrRecoveryContractUnavailable)
	}
	return nil
}

// WithHeldMaintenance keeps the verified fence live through transaction commit,
// even if a caller retains the command after the adapter callback returns.
func (command RecoveryReplayCommand) WithHeldMaintenance(ctx context.Context, apply func(context.Context) error) error {
	if ctx == nil || apply == nil || nilDeletionServiceDependency(command.fence) {
		return ErrRecoveryContractUnavailable
	}
	return command.fence.WithHeldLedgerFence(ctx, func(heldCtx context.Context) error {
		if err := command.Validate(); err != nil {
			return err
		}
		return apply(heldCtx)
	})
}

func equalRecoverySurfaces(left, right []RecoverySurface) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
