package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"houfeng/internal/center/recordplatform"
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

// RecoveryReplayRequest is accepted only after the independent ledger and
// full witness have supplied a fresh, continuous entry proof.
type RecoveryReplayRequest struct {
	Cursor                  RecoveryReplayCursor
	Entry                   DeletionLedgerEntry
	PreviousHash            [sha256.Size]byte
	WitnessProofDigest      [sha256.Size]byte
	RequestFingerprintBytes [sha256.Size]byte
}

type RecoveryReplayCommand struct {
	Cursor                  RecoveryReplayCursor
	Entry                   DeletionLedgerEntry
	PreviousHash            [sha256.Size]byte
	WitnessProofDigest      [sha256.Size]byte
	RequestFingerprintBytes [sha256.Size]byte
	AllowedSurfaces         []RecoverySurface
	SurfaceDigest           [sha256.Size]byte
	PurgeContent            bool
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

type RecoveryAdapter struct {
	store RecoveryStore
}

func NewRecoveryAdapter(store RecoveryStore) (*RecoveryAdapter, error) {
	if nilDeletionServiceDependency(store) {
		return nil, ErrInvalidRecoveryAdapter
	}
	return &RecoveryAdapter{store: store}, nil
}

func (adapter *RecoveryAdapter) Replay(ctx context.Context, request RecoveryReplayRequest) (RecoveryReplayReceipt, error) {
	if ctx == nil || adapter == nil || nilDeletionServiceDependency(adapter.store) {
		return RecoveryReplayReceipt{}, ErrInvalidRecoveryAdapter
	}
	if err := ctx.Err(); err != nil {
		return RecoveryReplayReceipt{}, err
	}
	surfaces, purgeContent, err := validateRecoveryReplayRequest(request)
	if err != nil {
		return RecoveryReplayReceipt{}, err
	}
	surfaceDigest := digestRecoverySurfaces(request.Entry.Request.EntryType, surfaces)
	command := RecoveryReplayCommand{
		Cursor:                  request.Cursor,
		Entry:                   request.Entry,
		PreviousHash:            request.PreviousHash,
		WitnessProofDigest:      request.WitnessProofDigest,
		RequestFingerprintBytes: request.RequestFingerprintBytes,
		AllowedSurfaces:         append([]RecoverySurface(nil), surfaces...),
		SurfaceDigest:           surfaceDigest,
		PurgeContent:            purgeContent,
	}
	receipt, err := adapter.store.ApplyRecoveryEntry(ctx, command)
	if err != nil {
		return RecoveryReplayReceipt{}, err
	}
	if receipt.Sequence != request.Entry.Sequence || receipt.EntryHash != request.Entry.EntryHash ||
		receipt.SurfaceDigest != surfaceDigest || receipt.ContentPurged != purgeContent {
		return RecoveryReplayReceipt{}, ErrRecoveryContractUnavailable
	}
	return receipt, nil
}

func validateRecoveryReplayRequest(request RecoveryReplayRequest) ([]RecoverySurface, bool, error) {
	persistedFingerprint, fingerprintErr := recordplatform.ParseTrustedPersistedRequestFingerprintV1(
		request.RequestFingerprintBytes[:],
	)
	if request.Cursor.validate() != nil || request.Entry.Validate() != nil ||
		request.Cursor.Sequence == ^uint64(0) || request.Entry.Sequence != request.Cursor.Sequence+1 ||
		request.PreviousHash != request.Cursor.EntryHash || zeroDeletionDigest(request.WitnessProofDigest) ||
		fingerprintErr != nil || !persistedFingerprint.Equal(request.Entry.Request.RequestFingerprint) ||
		request.Entry.Request.Object.ObjectKind != "record" {
		return nil, false, ErrRecoveryContractUnavailable
	}
	switch request.Entry.Request.EntryType {
	case LedgerEntryDeleteCommit:
		if request.Entry.Request.DeletionContractVersion != RecordDeletionContractVersionV1 || request.Entry.Request.ReleaseEpoch != 0 {
			return nil, false, ErrRecoveryContractUnavailable
		}
		return RecordDeleteRecoverySurfaces(), true, nil
	case LedgerEntryAttemptNotCommitted:
		if request.Entry.Request.DeletionContractVersion != 0 || request.Entry.Request.ReleaseEpoch == 0 {
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
	request := RecoveryReplayRequest{
		Cursor:                  command.Cursor,
		Entry:                   command.Entry,
		PreviousHash:            command.PreviousHash,
		WitnessProofDigest:      command.WitnessProofDigest,
		RequestFingerprintBytes: command.RequestFingerprintBytes,
	}
	expected, purge, err := validateRecoveryReplayRequest(request)
	if err != nil || purge != command.PurgeContent || command.SurfaceDigest != digestRecoverySurfaces(command.Entry.Request.EntryType, expected) ||
		!equalRecoverySurfaces(command.AllowedSurfaces, expected) {
		return fmt.Errorf("%w: replay command", ErrRecoveryContractUnavailable)
	}
	return nil
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
