package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

var (
	ErrInvalidDeletionWorker       = errors.New("invalid record deletion worker")
	ErrInvalidDeletionWorkerResult = errors.New("invalid record deletion worker result")
)

type LedgerEntryType string

const (
	LedgerEntryDeleteCommit        LedgerEntryType = "delete_commit"
	LedgerEntryAttemptNotCommitted LedgerEntryType = "attempt_not_committed"
)

// LedgerAppendRequest is the exact content-free deletion identity sent to the
// independent ledger. RequestFingerprint is trusted readback from preview
// persistence, not caller input and not a write-capable idempotency value.
type LedgerAppendRequest struct {
	EntryType               LedgerEntryType
	DeploymentID            recordplatform.DeploymentID
	ProjectID               recordplatform.ProjectID
	OperationID             string
	ActorID                 string
	Object                  recordplatform.ObjectRef
	TokenCommitment         [sha256.Size]byte
	RequestFingerprint      recordplatform.PersistedRequestFingerprintV1
	ReasonCode              DeletionReasonCode
	DeletionContractVersion uint64
	ReleaseEpoch            uint64
}

func (request LedgerAppendRequest) Validate() error {
	if recordplatform.ValidateDeploymentID(request.DeploymentID) != nil ||
		recordplatform.ValidateProjectID(request.ProjectID) != nil ||
		!validDeletionOperationID(request.OperationID) || recordauth.ValidateActorUserID(request.ActorID) != nil ||
		request.Object.Validate() != nil || request.Object.ProjectID != string(request.ProjectID) ||
		request.Object.ObjectKind != "record" || !validDeletionRecordID(request.Object.ObjectID) ||
		zeroDeletionDigest(request.TokenCommitment) || request.RequestFingerprint.Validate() != nil ||
		request.ReasonCode != DeletionReasonUserConfirmed {
		return ErrInvalidDeletionWorkerResult
	}
	switch request.EntryType {
	case LedgerEntryDeleteCommit:
		if request.DeletionContractVersion == 0 || request.ReleaseEpoch != 0 {
			return ErrInvalidDeletionWorkerResult
		}
	case LedgerEntryAttemptNotCommitted:
		if request.DeletionContractVersion != 0 || request.ReleaseEpoch == 0 {
			return ErrInvalidDeletionWorkerResult
		}
	default:
		return ErrInvalidDeletionWorkerResult
	}
	return nil
}

func (request LedgerAppendRequest) AttemptNotCommitted(releaseEpoch uint64) LedgerAppendRequest {
	outcome := request
	outcome.EntryType = LedgerEntryAttemptNotCommitted
	outcome.DeletionContractVersion = 0
	outcome.ReleaseEpoch = releaseEpoch
	return outcome
}

type DeletionLedgerEntry struct {
	Request   LedgerAppendRequest
	Sequence  uint64
	EntryHash [sha256.Size]byte
}

func (entry DeletionLedgerEntry) Validate() error {
	if entry.Request.Validate() != nil || entry.Sequence == 0 || zeroDeletionDigest(entry.EntryHash) {
		return ErrInvalidDeletionWorkerResult
	}
	return nil
}

type DeletionAbsenceProof struct {
	releaseEpoch uint64
	proofDigest  [sha256.Size]byte
	sealed       bool
}

// NewDeletionAbsenceProof seals the only result that permits the worker to
// choose attempt_not_committed. Its producer must have fenced the prior append
// owner and proved absence from fresh primary and full-witness reads.
func NewDeletionAbsenceProof(releaseEpoch uint64, proofDigest [sha256.Size]byte) (DeletionAbsenceProof, error) {
	if releaseEpoch == 0 || zeroDeletionDigest(proofDigest) {
		return DeletionAbsenceProof{}, ErrInvalidDeletionWorkerResult
	}
	return DeletionAbsenceProof{releaseEpoch: releaseEpoch, proofDigest: proofDigest, sealed: true}, nil
}

func (proof DeletionAbsenceProof) ReleaseEpoch() uint64 {
	if !proof.sealed {
		return 0
	}
	return proof.releaseEpoch
}

func (proof DeletionAbsenceProof) validate() error {
	if !proof.sealed || proof.releaseEpoch == 0 || zeroDeletionDigest(proof.proofDigest) {
		return ErrInvalidDeletionWorkerResult
	}
	return nil
}

type LedgerResolutionKind string

const (
	LedgerResolutionUnresolved    LedgerResolutionKind = "unresolved"
	LedgerResolutionCommitted     LedgerResolutionKind = "committed"
	LedgerResolutionAbsenceProven LedgerResolutionKind = "absence_proven"
)

type LedgerResolution struct {
	Kind         LedgerResolutionKind
	Entry        *DeletionLedgerEntry
	AbsenceProof DeletionAbsenceProof
}

func NewCommittedLedgerResolution(entry DeletionLedgerEntry) LedgerResolution {
	copyEntry := entry
	return LedgerResolution{Kind: LedgerResolutionCommitted, Entry: &copyEntry}
}

func NewAbsenceProvenLedgerResolution(proof DeletionAbsenceProof) LedgerResolution {
	return LedgerResolution{Kind: LedgerResolutionAbsenceProven, AbsenceProof: proof}
}

func (resolution LedgerResolution) validate(expected LedgerAppendRequest) error {
	switch resolution.Kind {
	case LedgerResolutionUnresolved:
		if resolution.Entry != nil || resolution.AbsenceProof.sealed {
			return ErrInvalidDeletionWorkerResult
		}
	case LedgerResolutionCommitted:
		if resolution.Entry == nil || resolution.Entry.Validate() != nil ||
			!sameLedgerAppendRequest(resolution.Entry.Request, expected) || resolution.AbsenceProof.sealed {
			return ErrInvalidDeletionWorkerResult
		}
	case LedgerResolutionAbsenceProven:
		if resolution.Entry != nil || resolution.AbsenceProof.validate() != nil {
			return ErrInvalidDeletionWorkerResult
		}
		if expected.EntryType == LedgerEntryAttemptNotCommitted && resolution.AbsenceProof.ReleaseEpoch() != expected.ReleaseEpoch {
			return ErrInvalidDeletionWorkerResult
		}
	default:
		return ErrInvalidDeletionWorkerResult
	}
	return nil
}

type DeletionWitnessReceipt struct {
	Sequence    uint64
	EntryHash   [sha256.Size]byte
	ProofDigest [sha256.Size]byte
}

func (receipt DeletionWitnessReceipt) validate(entry DeletionLedgerEntry) error {
	if receipt.Sequence != entry.Sequence || receipt.EntryHash != entry.EntryHash || zeroDeletionDigest(receipt.ProofDigest) {
		return ErrInvalidDeletionWorkerResult
	}
	return nil
}

type OnlinePurgeReceipt struct {
	OperationID   string
	ReceiptDigest [sha256.Size]byte
}

func (receipt OnlinePurgeReceipt) validate(operation DeletionOperation) error {
	if receipt.OperationID != operation.OperationID || zeroDeletionDigest(receipt.ReceiptDigest) {
		return ErrInvalidDeletionWorkerResult
	}
	return nil
}

type DeletionWorkStage string

const (
	DeletionWorkAppendDeleteCommit         DeletionWorkStage = "append_delete_commit"
	DeletionWorkResolveDeleteCommit        DeletionWorkStage = "resolve_delete_commit"
	DeletionWorkConfirmDeleteWitness       DeletionWorkStage = "confirm_delete_witness"
	DeletionWorkPromotePermanentFence      DeletionWorkStage = "promote_permanent_fence"
	DeletionWorkPropagatePermanentFence    DeletionWorkStage = "propagate_permanent_fence"
	DeletionWorkBeginOnlinePurge           DeletionWorkStage = "begin_online_purge"
	DeletionWorkPurgeOnline                DeletionWorkStage = "purge_online"
	DeletionWorkResolveRetry               DeletionWorkStage = "resolve_retry"
	DeletionWorkResolveNotCommitted        DeletionWorkStage = "resolve_not_committed"
	DeletionWorkConfirmNotCommittedWitness DeletionWorkStage = "confirm_not_committed_witness"
)

type DeletionWorkClaimInput struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
}

func (input DeletionWorkClaimInput) Validate() error {
	return (recordplatform.LeaseClaimInputV1{OwnerID: input.OwnerID, LeaseDuration: input.OwnerLeaseDuration}).Validate()
}

type ClaimedDeletionWork struct {
	Operation  DeletionOperation
	Owner      recordplatform.OwnerLease
	Stage      DeletionWorkStage
	Request    LedgerAppendRequest
	Entry      *DeletionLedgerEntry
	RetryStage DeletionWorkStage
}

func (claim ClaimedDeletionWork) Validate() error {
	if claim.Operation.Validate() != nil || claim.Owner.Validate() != nil || claim.Request.Validate() != nil ||
		claim.Request.OperationID != claim.Operation.OperationID || claim.Request.Object != claim.Operation.Object ||
		claim.Request.ReasonCode != claim.Operation.ReasonCode {
		return ErrInvalidDeletionWorkerResult
	}
	entryMatches := func(expectedType LedgerEntryType) bool {
		return claim.Entry != nil && claim.Entry.Validate() == nil && claim.Entry.Request.EntryType == expectedType &&
			sameLedgerAppendRequest(claim.Entry.Request, claim.Request) &&
			claim.Operation.LedgerSequence == claim.Entry.Sequence && claim.Operation.LedgerEntryHash == claim.Entry.EntryHash
	}
	switch claim.Stage {
	case DeletionWorkAppendDeleteCommit:
		if claim.Operation.State != DeletionStateProvisionalFenced || claim.Request.EntryType != LedgerEntryDeleteCommit || claim.Entry != nil {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkResolveDeleteCommit:
		if claim.Operation.State != DeletionStateLedgerCommitUnknown || claim.Request.EntryType != LedgerEntryDeleteCommit || claim.Entry != nil {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkConfirmDeleteWitness:
		if claim.Operation.State != DeletionStateWitnessPending || !entryMatches(LedgerEntryDeleteCommit) {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkPromotePermanentFence:
		if claim.Operation.State != DeletionStateDeleteRequested || claim.Entry != nil {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkPropagatePermanentFence:
		if claim.Operation.State != DeletionStateFencePropagating || claim.Entry != nil {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkBeginOnlinePurge:
		if claim.Operation.State != DeletionStateReadFenced || claim.Entry != nil {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkPurgeOnline:
		if claim.Operation.State != DeletionStateOnlinePurging || claim.Entry != nil {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkResolveRetry:
		if claim.Operation.State != DeletionStateRetryRequired || !retryableDeletionStage(claim.RetryStage) || claim.Entry != nil {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkResolveNotCommitted:
		if claim.Operation.State != DeletionStateReleasePending || claim.Request.EntryType != LedgerEntryAttemptNotCommitted || claim.Entry != nil ||
			claim.Operation.ReleaseEpoch != claim.Request.ReleaseEpoch {
			return ErrInvalidDeletionWorkerResult
		}
	case DeletionWorkConfirmNotCommittedWitness:
		if claim.Operation.State != DeletionStateReleasePending || !entryMatches(LedgerEntryAttemptNotCommitted) ||
			claim.Operation.ReleaseEpoch != claim.Request.ReleaseEpoch {
			return ErrInvalidDeletionWorkerResult
		}
	default:
		return ErrInvalidDeletionWorkerResult
	}
	return nil
}

type DeletionWorkerRepository interface {
	ClaimDeletionWork(context.Context, DeletionWorkClaimInput) (*ClaimedDeletionWork, error)
	MarkDeleteCommitUnknown(context.Context, ClaimedDeletionWork) error
	RecordDeleteEntry(context.Context, ClaimedDeletionWork, DeletionLedgerEntry) error
	RecordOutcomeEntry(context.Context, ClaimedDeletionWork, DeletionLedgerEntry) error
	MarkOutcomeCommitUnknown(context.Context, ClaimedDeletionWork, uint64) error
	MarkDeleteWitnessed(context.Context, ClaimedDeletionWork, DeletionWitnessReceipt) error
	FinalizeNotCommitted(context.Context, ClaimedDeletionWork, DeletionWitnessReceipt) error
	PromotePermanentFence(context.Context, ClaimedDeletionWork) error
	PermanentFenceApplied(context.Context, ClaimedDeletionWork) (bool, error)
	MarkReadFenced(context.Context, ClaimedDeletionWork) error
	BeginOnlinePurge(context.Context, ClaimedDeletionWork) error
	CompleteOnlinePurge(context.Context, ClaimedDeletionWork, OnlinePurgeReceipt) error
	MarkRetryRequired(context.Context, ClaimedDeletionWork, DeletionWorkStage) error
	ResumeRetry(context.Context, ClaimedDeletionWork, DeletionWorkStage) error
}

type DeletionLedger interface {
	AppendDeletionEntry(context.Context, LedgerAppendRequest) (DeletionLedgerEntry, error)
	ResolveDeletionEntry(context.Context, LedgerAppendRequest) (LedgerResolution, error)
}

type DeletionEntryWitness interface {
	ConfirmDeletionEntry(context.Context, DeletionLedgerEntry) (DeletionWitnessReceipt, error)
}

type DeletionOnlinePurger interface {
	PurgeOnline(context.Context, DeletionOperation) (OnlinePurgeReceipt, error)
}

type DeletionWorkerOptions struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
	PollInterval       time.Duration
	Logger             *slog.Logger
}

type DeletionWorker struct {
	repository DeletionWorkerRepository
	ledger     DeletionLedger
	witness    DeletionEntryWitness
	purger     DeletionOnlinePurger
	options    DeletionWorkerOptions
}

func NewDeletionWorker(
	repository DeletionWorkerRepository,
	ledger DeletionLedger,
	witness DeletionEntryWitness,
	purger DeletionOnlinePurger,
	options DeletionWorkerOptions,
) *DeletionWorker {
	if options.PollInterval == 0 {
		options.PollInterval = time.Second
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &DeletionWorker{repository: repository, ledger: ledger, witness: witness, purger: purger, options: options}
}

func (worker *DeletionWorker) Run(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidDeletionWorker
	}
	if err := worker.validate(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return nil
	}
	ticker := time.NewTicker(worker.options.PollInterval)
	defer ticker.Stop()
	for {
		if err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			worker.options.Logger.Error("record deletion pass failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *DeletionWorker) RunOnce(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidDeletionWorker
	}
	if err := worker.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	claim, err := worker.repository.ClaimDeletionWork(ctx, DeletionWorkClaimInput{
		OwnerID:            worker.options.OwnerID,
		OwnerLeaseDuration: worker.options.OwnerLeaseDuration,
	})
	if err != nil {
		return fmt.Errorf("claim record deletion work: %w", err)
	}
	if claim == nil {
		return nil
	}
	if err := claim.Validate(); err != nil {
		return err
	}
	switch claim.Stage {
	case DeletionWorkAppendDeleteCommit:
		return worker.appendDeleteCommit(ctx, *claim)
	case DeletionWorkResolveDeleteCommit:
		return worker.resolveEntry(ctx, *claim, false)
	case DeletionWorkConfirmDeleteWitness:
		return worker.confirmEntry(ctx, *claim, false)
	case DeletionWorkResolveNotCommitted:
		return worker.resolveEntry(ctx, *claim, true)
	case DeletionWorkConfirmNotCommittedWitness:
		return worker.confirmEntry(ctx, *claim, true)
	case DeletionWorkPromotePermanentFence:
		if err := worker.repository.PromotePermanentFence(ctx, *claim); err != nil {
			return worker.markRetryRequired(ctx, *claim, DeletionWorkPromotePermanentFence, err)
		}
		return nil
	case DeletionWorkPropagatePermanentFence:
		ready, err := worker.repository.PermanentFenceApplied(ctx, *claim)
		if err != nil {
			return worker.markRetryRequired(ctx, *claim, DeletionWorkPropagatePermanentFence, err)
		}
		if !ready {
			return nil
		}
		if err := worker.repository.MarkReadFenced(ctx, *claim); err != nil {
			return worker.markRetryRequired(ctx, *claim, DeletionWorkPropagatePermanentFence, err)
		}
		return nil
	case DeletionWorkBeginOnlinePurge:
		if err := worker.repository.BeginOnlinePurge(ctx, *claim); err != nil {
			return worker.markRetryRequired(ctx, *claim, DeletionWorkBeginOnlinePurge, err)
		}
		return nil
	case DeletionWorkPurgeOnline:
		return worker.purgeOnline(ctx, *claim)
	case DeletionWorkResolveRetry:
		return worker.repository.ResumeRetry(ctx, *claim, claim.RetryStage)
	default:
		return ErrInvalidDeletionWorkerResult
	}
}

func (worker *DeletionWorker) appendDeleteCommit(ctx context.Context, claim ClaimedDeletionWork) error {
	entry, err := worker.ledger.AppendDeletionEntry(ctx, claim.Request)
	if err != nil {
		return worker.repository.MarkDeleteCommitUnknown(ctx, claim)
	}
	if entry.Validate() != nil || !sameLedgerAppendRequest(entry.Request, claim.Request) {
		return ErrInvalidDeletionWorkerResult
	}
	return worker.repository.RecordDeleteEntry(ctx, claim, entry)
}

func (worker *DeletionWorker) resolveEntry(ctx context.Context, claim ClaimedDeletionWork, outcome bool) error {
	resolution, err := worker.ledger.ResolveDeletionEntry(ctx, claim.Request)
	if err != nil {
		return fmt.Errorf("resolve deletion ledger entry: %w", err)
	}
	if err := resolution.validate(claim.Request); err != nil {
		return err
	}
	switch resolution.Kind {
	case LedgerResolutionUnresolved:
		return nil
	case LedgerResolutionCommitted:
		if outcome {
			return worker.repository.RecordOutcomeEntry(ctx, claim, *resolution.Entry)
		}
		return worker.repository.RecordDeleteEntry(ctx, claim, *resolution.Entry)
	case LedgerResolutionAbsenceProven:
		request := claim.Request
		if !outcome {
			request = claim.Request.AttemptNotCommitted(resolution.AbsenceProof.ReleaseEpoch())
		}
		entry, appendErr := worker.ledger.AppendDeletionEntry(ctx, request)
		if appendErr != nil {
			return worker.repository.MarkOutcomeCommitUnknown(ctx, claim, request.ReleaseEpoch)
		}
		if entry.Validate() != nil || !sameLedgerAppendRequest(entry.Request, request) {
			return ErrInvalidDeletionWorkerResult
		}
		return worker.repository.RecordOutcomeEntry(ctx, claim, entry)
	default:
		return ErrInvalidDeletionWorkerResult
	}
}

func (worker *DeletionWorker) confirmEntry(ctx context.Context, claim ClaimedDeletionWork, outcome bool) error {
	receipt, err := worker.witness.ConfirmDeletionEntry(ctx, *claim.Entry)
	if err != nil {
		return fmt.Errorf("confirm deletion ledger entry: %w", err)
	}
	if err := receipt.validate(*claim.Entry); err != nil {
		return err
	}
	if outcome {
		return worker.repository.FinalizeNotCommitted(ctx, claim, receipt)
	}
	return worker.repository.MarkDeleteWitnessed(ctx, claim, receipt)
}

func (worker *DeletionWorker) purgeOnline(ctx context.Context, claim ClaimedDeletionWork) error {
	receipt, err := worker.purger.PurgeOnline(ctx, claim.Operation)
	if err != nil {
		return worker.markRetryRequired(ctx, claim, DeletionWorkPurgeOnline, err)
	}
	if err := receipt.validate(claim.Operation); err != nil {
		return worker.markRetryRequired(ctx, claim, DeletionWorkPurgeOnline, err)
	}
	if err := worker.repository.CompleteOnlinePurge(ctx, claim, receipt); err != nil {
		return worker.markRetryRequired(ctx, claim, DeletionWorkPurgeOnline, err)
	}
	return nil
}

func (worker *DeletionWorker) markRetryRequired(
	ctx context.Context,
	claim ClaimedDeletionWork,
	stage DeletionWorkStage,
	cause error,
) error {
	if cause == nil {
		return ErrInvalidDeletionWorkerResult
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return worker.repository.MarkRetryRequired(ctx, claim, stage)
}

func (worker *DeletionWorker) validate() error {
	if worker == nil || nilDeletionServiceDependency(worker.repository) || nilDeletionServiceDependency(worker.ledger) ||
		nilDeletionServiceDependency(worker.witness) || nilDeletionServiceDependency(worker.purger) {
		return ErrInvalidDeletionWorker
	}
	if err := (DeletionWorkClaimInput{OwnerID: worker.options.OwnerID, OwnerLeaseDuration: worker.options.OwnerLeaseDuration}).Validate(); err != nil ||
		worker.options.PollInterval <= 0 {
		return ErrInvalidDeletionWorker
	}
	return nil
}

func sameLedgerAppendRequest(left, right LedgerAppendRequest) bool {
	return left.EntryType == right.EntryType && left.DeploymentID == right.DeploymentID &&
		left.ProjectID == right.ProjectID && left.OperationID == right.OperationID &&
		left.ActorID == right.ActorID && left.Object == right.Object &&
		left.TokenCommitment == right.TokenCommitment &&
		left.RequestFingerprint.Equal(right.RequestFingerprint) &&
		left.ReasonCode == right.ReasonCode && left.DeletionContractVersion == right.DeletionContractVersion &&
		left.ReleaseEpoch == right.ReleaseEpoch
}

func retryableDeletionStage(stage DeletionWorkStage) bool {
	switch stage {
	case DeletionWorkPromotePermanentFence, DeletionWorkPropagatePermanentFence,
		DeletionWorkBeginOnlinePurge, DeletionWorkPurgeOnline:
		return true
	default:
		return false
	}
}
