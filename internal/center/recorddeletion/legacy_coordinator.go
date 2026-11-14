package recorddeletion

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
)

var ErrLegacyLedgerUnresolved = errors.New("legacy record deletion ledger outcome is unresolved")

// LegacyLedgerOperation is read from the existing, locked V1 operation and its
// reservation. It is never constructed from command-line deletion data.
type LegacyLedgerOperation struct {
	Request         LedgerAppendRequest
	State           DeletionState
	LedgerSequence  uint64
	LedgerEntryHash [32]byte
}

func (operation LegacyLedgerOperation) validate() error {
	request := operation.Request
	if request.Validate() != nil || !knownDeletionState(operation.State) || operation.LedgerSequence > math.MaxInt64 ||
		(operation.LedgerSequence == 0) != zeroDeletionDigest(operation.LedgerEntryHash) {
		return ErrInvalidDeletionWorkerResult
	}
	if request.EntryType == LedgerEntryDeleteCommit && request.DeletionContractVersion != RecordDeletionContractVersionV1 {
		return ErrInvalidDeletionWorkerResult
	}
	if request.EntryType == LedgerEntryAttemptNotCommitted && operation.State != DeletionStateReleasePending && operation.State != DeletionStateNotCommitted {
		return ErrInvalidDeletionWorkerResult
	}
	return nil
}

// LegacyVerifiedEntryInstaller uses the same direct admin transaction as the
// operation lock, or the same explicitly admitted recovery transaction.
type LegacyVerifiedEntryInstaller interface {
	InstallVerifiedEntry(context.Context, []byte) error
}

// LegacyLedgerOperationStore obtains an existing V1 operation under a database
// lock. The callback and commit complete before its lock is released. Recovery
// may instead have no APP projection, but must run under the protected RID.
type LegacyLedgerOperationStore interface {
	WithLegacyOperation(context.Context, string, func(context.Context, LegacyLedgerOperation, LegacyVerifiedEntryInstaller) error) error
	WithLegacyRecovery(context.Context, func(context.Context, LegacyVerifiedEntryInstaller) error) error
}

// LegacyManagementFence is supplied only by the trusted management composition.
// The concrete fence owns the protected maintenance guard and external anchor
// journal; it must persist an append intent before reporting that it is fresh.
type LegacyManagementFence interface {
	LedgerContinuityGuard
	WithHeldLedgerFence(context.Context, func(context.Context) error) error
	RecoveryBinding() (string, uint64, error)
	NeverEnrolled() (bool, error)
	Trust() (LedgerVerificationTrust, error)
	Preflight(context.Context) error
	BeginAppend(context.Context, LedgerAppendRequest) (bool, error)
	// WithAbsentOperation resolves an existing exact protected outcome. It must
	// never append or infer absence from a missing row.
	WithAbsentOperation(context.Context, LedgerAppendRequest) (DeletionLedgerEntry, error)
	Advance(context.Context) error
}

// LegacyLedgerCoordinator is deliberately separate from the deletion worker.
// It receives the APP admin transaction boundary and the independent ledger
// clients only in the trusted management process.
type LegacyLedgerCoordinator struct {
	store         LegacyLedgerOperationStore
	primary       DeletionLedger
	witness       DeletionEntryWitness
	primaryReader LedgerSnapshotReader
	witnessReader LedgerSnapshotReader
	fence         LegacyManagementFence
	domain        [32]byte
}

func NewLegacyLedgerCoordinator(store LegacyLedgerOperationStore, primary DeletionLedger, witness DeletionEntryWitness, primaryReader, witnessReader LedgerSnapshotReader, fence LegacyManagementFence, ledgerDomain [32]byte) (*LegacyLedgerCoordinator, error) {
	if nilDeletionServiceDependency(store) || nilDeletionServiceDependency(primary) || nilDeletionServiceDependency(witness) ||
		nilDeletionServiceDependency(primaryReader) || nilDeletionServiceDependency(witnessReader) ||
		nilDeletionServiceDependency(fence) || zeroDeletionDigest(ledgerDomain) {
		return nil, ErrDeletionSafetyUnavailable
	}
	return &LegacyLedgerCoordinator{store: store, primary: primary, witness: witness, primaryReader: primaryReader, witnessReader: witnessReader, fence: fence, domain: ledgerDomain}, nil
}

// ReconcileOperation accepts only an existing V1 operation ID. An unknown
// append is resolved by the original operation; absence never manufactures an
// outcome. Only a newly journaled, still-provisional operation may be sent.
func (coordinator *LegacyLedgerCoordinator) ReconcileOperation(ctx context.Context, operationID string) (LedgerLocator, error) {
	if ctx == nil || coordinator == nil || !validDeletionOperationID(operationID) {
		return LedgerLocator{}, ErrDeletionSafetyUnavailable
	}
	var locator LedgerLocator
	err := coordinator.fence.WithHeldLedgerFence(ctx, func(ctx context.Context) error {
		if _, _, err := legacyFenceBinding(coordinator.fence); err != nil {
			return err
		}
		return coordinator.store.WithLegacyOperation(ctx, operationID, func(ctx context.Context, operation LegacyLedgerOperation, installer LegacyVerifiedEntryInstaller) error {
			if operation.validate() != nil || operation.Request.OperationID != operationID || nilDeletionServiceDependency(installer) {
				return ErrDeletionSafetyUnavailable
			}
			if err := coordinator.checkContinuity(ctx); err != nil {
				return err
			}
			resolution, err := coordinator.primary.ResolveDeletionEntry(ctx, operation.Request)
			var entry DeletionLedgerEntry
			if err != nil {
				if operation.State != DeletionStateLedgerCommitUnknown || !errors.Is(err, ErrInvalidLedgerEvidence) {
					return fmt.Errorf("resolve legacy deletion entry: %w", err)
				}
				// The W1 operation may instead be an outcome committed by an earlier
				// guarded ceremony. Resolve only its exact protected journal binding.
				entry, err = coordinator.fence.WithAbsentOperation(ctx, operation.Request)
				if errors.Is(err, ErrLedgerPending) {
					return ErrLegacyLedgerUnresolved
				}
				if err != nil {
					return fmt.Errorf("resolve journaled legacy outcome: %w", err)
				}
			} else {
				if err := resolution.validate(operation.Request); err != nil {
					return err
				}
				switch resolution.Kind {
				case LedgerResolutionCommitted:
					entry = *resolution.Entry
				case LedgerResolutionUnresolved:
					if operation.State != DeletionStateProvisionalFenced || operation.Request.EntryType != LedgerEntryDeleteCommit || operation.LedgerSequence != 0 {
						return ErrLegacyLedgerUnresolved
					}
					fresh, err := coordinator.fence.BeginAppend(ctx, operation.Request)
					if err != nil {
						return err
					}
					if !fresh {
						return ErrLegacyLedgerUnresolved
					}
					entry, err = coordinator.primary.AppendDeletionEntry(ctx, operation.Request)
					if err != nil {
						// Retain the intent and resolve the original operation on retry.
						return errors.Join(ErrLegacyLedgerUnresolved, fmt.Errorf("append legacy deletion entry: %w", err))
					}
				default:
					return ErrLegacyLedgerUnresolved
				}
			}
			expected := operation.Request
			if operation.State == DeletionStateLedgerCommitUnknown && entry.Request.EntryType == LedgerEntryAttemptNotCommitted {
				expected = expected.AttemptNotCommitted(entry.Request.ReleaseEpoch)
			}
			if entry.Validate() != nil || !sameLedgerAppendRequest(entry.Request, expected) ||
				(operation.LedgerSequence != 0 && (operation.LedgerSequence != entry.Sequence || operation.LedgerEntryHash != entry.EntryHash)) {
				return ErrDeletionSafetyUnavailable
			}
			if err := coordinator.checkContinuity(ctx); err != nil {
				return err
			}
			receipt, err := coordinator.witness.ConfirmDeletionEntry(ctx, entry)
			if err != nil {
				return fmt.Errorf("confirm legacy deletion entry: %w", err)
			}
			if receipt.validate(entry) != nil {
				return ErrDeletionSafetyUnavailable
			}
			locator = LedgerLocator{LedgerDomain: coordinator.domain, Sequence: entry.Sequence, EntryHash: entry.EntryHash}
			return coordinator.verifyAndInstall(ctx, locator, installer)
		})
	})
	if err != nil {
		return LedgerLocator{}, err
	}
	return locator, nil
}

// InstallRecoveryEntry installs an already witnessed entry by locator while the
// current real recovery RID is held. It does not infer authority from a missing
// APP operation and does not create or append a deletion request.
func (coordinator *LegacyLedgerCoordinator) InstallRecoveryEntry(ctx context.Context, locator LedgerLocator) error {
	if ctx == nil || coordinator == nil || locator.LedgerDomain != coordinator.domain {
		return ErrDeletionSafetyUnavailable
	}
	return coordinator.fence.WithHeldLedgerFence(ctx, func(ctx context.Context) error {
		rid, generation, err := legacyFenceBinding(coordinator.fence)
		if err != nil || rid == "" || generation > math.MaxInt64 {
			return ErrDeletionSafetyUnavailable
		}
		return coordinator.store.WithLegacyRecovery(ctx, func(ctx context.Context, installer LegacyVerifiedEntryInstaller) error {
			if nilDeletionServiceDependency(installer) {
				return ErrDeletionSafetyUnavailable
			}
			return coordinator.verifyAndInstall(ctx, locator, installer)
		})
	})
}

func (coordinator *LegacyLedgerCoordinator) checkContinuity(ctx context.Context) error {
	trust, err := coordinator.fence.Trust()
	if err != nil {
		return err
	}
	fenceValue := reflect.ValueOf(coordinator.fence)
	if fenceValue.Kind() != reflect.Pointer || fenceValue.IsNil() || fenceValue != reflect.ValueOf(trust.Continuity) {
		return ErrDeletionSafetyUnavailable
	}
	domain, err := LedgerDomainDigest(trust.Primary.ID)
	if err != nil || domain != coordinator.domain {
		return ErrDeletionSafetyUnavailable
	}
	return coordinator.fence.Preflight(ctx)
}

func (coordinator *LegacyLedgerCoordinator) verifyAndInstall(ctx context.Context, locator LedgerLocator, installer LegacyVerifiedEntryInstaller) error {
	trust, err := coordinator.fence.Trust()
	if err != nil {
		return err
	}
	fenceValue := reflect.ValueOf(coordinator.fence)
	if fenceValue.Kind() != reflect.Pointer || fenceValue.IsNil() || fenceValue != reflect.ValueOf(trust.Continuity) {
		return ErrDeletionSafetyUnavailable
	}
	verifier, err := NewLedgerVerifier(coordinator.primaryReader, coordinator.witnessReader, trust)
	if err != nil {
		return err
	}
	verified, err := verifier.Verify(ctx, locator)
	if err != nil {
		return fmt.Errorf("verify legacy ledger and witness: %w", err)
	}
	if !verified.Valid() || verified.Locator() != locator {
		return ErrDeletionSafetyUnavailable
	}
	// Protect the witnessed prefix before installing APP authority. Advance must
	// independently reread the physical chains while the same guard is held.
	if err := coordinator.fence.Advance(ctx); err != nil {
		return fmt.Errorf("retain legacy ledger anchors: %w", err)
	}
	rid, generation, err := legacyFenceBinding(coordinator.fence)
	if err != nil {
		return err
	}
	payload, err := EncodeLegacyVerifiedEntry(verified, rid, generation)
	if err != nil {
		return err
	}
	if err := installer.InstallVerifiedEntry(ctx, payload); err != nil {
		return fmt.Errorf("install verified legacy entry: %w", err)
	}
	return nil
}

func legacyFenceBinding(fence LegacyManagementFence) (string, uint64, error) {
	rid, generation, err := fence.RecoveryBinding()
	if err != nil || generation > math.MaxInt64 || (rid == "" && generation != 0) {
		return "", 0, ErrDeletionSafetyUnavailable
	}
	if rid == "" {
		return rid, generation, nil
	}
	neverEnrolled, err := fence.NeverEnrolled()
	if err != nil || neverEnrolled != (generation == 0) {
		return "", 0, ErrDeletionSafetyUnavailable
	}
	return rid, generation, nil
}

// EncodeLegacyVerifiedEntry serializes a sealed verifier result for the narrow
// admin-only APP command. It does not authenticate external evidence by itself.
func EncodeLegacyVerifiedEntry(verified VerifiedLedgerEntry, rid string, generation uint64) ([]byte, error) {
	if !verified.Valid() || generation > math.MaxInt64 || (rid == "" && generation != 0) {
		return nil, ErrDeletionSafetyUnavailable
	}
	entry, locator := verified.Entry(), verified.Locator()
	fingerprint, err := entry.Request.RequestFingerprint.PersistedBytes()
	if err != nil {
		return nil, ErrDeletionSafetyUnavailable
	}
	primary, witness, proof := verified.PrimaryHead(), verified.WitnessHead(), verified.ProofDigest()
	payload := struct {
		LedgerDomain                string             `json:"ledger_domain"`
		LedgerSequence              uint64             `json:"ledger_sequence"`
		LedgerEntryHash             string             `json:"ledger_entry_hash"`
		PreviousHash                string             `json:"previous_hash"`
		CanonicalEntry              string             `json:"canonical_entry"`
		EntryType                   LedgerEntryType    `json:"entry_type"`
		DeploymentID                string             `json:"deployment_id"`
		ProjectID                   string             `json:"project_id"`
		OperationID                 string             `json:"operation_id"`
		ActorID                     string             `json:"actor_id"`
		Route                       string             `json:"route"`
		ObjectKind                  string             `json:"object_kind"`
		ObjectID                    string             `json:"object_id"`
		TokenCommitment             string             `json:"token_commitment"`
		RequestFingerprint          string             `json:"request_fingerprint"`
		ReasonCode                  DeletionReasonCode `json:"reason_code"`
		DeletionContractVersion     uint64             `json:"deletion_contract_version"`
		ReleaseEpoch                uint64             `json:"release_epoch"`
		ConfirmedAtUnixMicroseconds int64              `json:"confirmed_at_unix_microseconds"`
		PrimaryHeadSequence         uint64             `json:"primary_head_sequence"`
		PrimaryHeadHash             string             `json:"primary_head_hash"`
		WitnessHeadSequence         uint64             `json:"witness_head_sequence"`
		WitnessHeadHash             string             `json:"witness_head_hash"`
		WitnessProofDigest          string             `json:"witness_proof_digest"`
		MaintenanceRID              string             `json:"maintenance_rid"`
		MaintenanceGeneration       uint64             `json:"maintenance_generation"`
	}{
		LedgerDomain: hex.EncodeToString(locator.LedgerDomain[:]), LedgerSequence: locator.Sequence,
		LedgerEntryHash: hex.EncodeToString(locator.EntryHash[:]), PreviousHash: hex.EncodeToString(entry.PreviousHash[:]),
		CanonicalEntry: hex.EncodeToString(entry.Canonical), EntryType: entry.Request.EntryType,
		DeploymentID: string(entry.Request.DeploymentID), ProjectID: string(entry.Request.ProjectID),
		OperationID: entry.Request.OperationID, ActorID: entry.Request.ActorID, Route: LedgerRecordRoute,
		ObjectKind: entry.Request.Object.ObjectKind, ObjectID: entry.Request.Object.ObjectID,
		TokenCommitment: hex.EncodeToString(entry.Request.TokenCommitment[:]), RequestFingerprint: hex.EncodeToString(fingerprint[:]),
		ReasonCode: entry.Request.ReasonCode, DeletionContractVersion: entry.Request.DeletionContractVersion,
		ReleaseEpoch: entry.Request.ReleaseEpoch, ConfirmedAtUnixMicroseconds: entry.ConfirmedAt.UnixMicro(),
		PrimaryHeadSequence: primary.Sequence, PrimaryHeadHash: hex.EncodeToString(primary.Hash[:]),
		WitnessHeadSequence: witness.Sequence, WitnessHeadHash: hex.EncodeToString(witness.Hash[:]),
		WitnessProofDigest: hex.EncodeToString(proof[:]), MaintenanceRID: rid, MaintenanceGeneration: generation,
	}
	return json.Marshal(payload)
}
