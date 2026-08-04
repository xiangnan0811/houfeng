package recorddeletion

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

const (
	DeletionPreviewTTL = 10 * time.Minute

	deletionPreviewBindingDomainV1 = "houfeng.record-deletion.preview-binding.v1"
	deletionAdapterPreviewDomainV1 = "houfeng.record-deletion.adapter-preview.v1"
	deletionRequestScopeDomainV1   = "houfeng.record-deletion.request-scope.v1"
)

var (
	ErrInvalidDeletionService     = errors.New("invalid record deletion service")
	ErrInvalidDeletionPreview     = errors.New("invalid record deletion preview")
	ErrInvalidDeletionOperation   = errors.New("invalid record deletion operation")
	ErrDeletionPreviewNotFound    = errors.New("record deletion preview not found")
	ErrDeletionPreviewStale       = errors.New("record deletion preview stale")
	ErrDeletionRequestTokenReused = errors.New("record deletion request token reused")
	ErrDeletionOperationNotFound  = errors.New("record deletion operation not found")
	ErrDeletionStatusUnavailable  = errors.New("record deletion status unavailable")
)

type DeletionReasonCode string

const (
	DeletionReasonUserConfirmed   DeletionReasonCode = "user_confirmed"
	DeletionReasonSourceRemoved   DeletionReasonCode = "source_removed"
	DeletionReasonRetentionReplay DeletionReasonCode = "retention_replay"
)

type DeletionState string

const (
	DeletionStateProvisionalFenced   DeletionState = "provisional_fenced"
	DeletionStateLedgerCommitUnknown DeletionState = "ledger_commit_unknown"
	DeletionStateWitnessPending      DeletionState = "witness_pending"
	DeletionStateDeleteRequested     DeletionState = "delete_requested"
	DeletionStateFencePropagating    DeletionState = "fence_propagating"
	DeletionStateReadFenced          DeletionState = "read_fenced"
	DeletionStateOnlinePurging       DeletionState = "online_purging"
	DeletionStateOnlinePurged        DeletionState = "online_purged"
	DeletionStateReleasePending      DeletionState = "release_pending"
	DeletionStateNotCommitted        DeletionState = "not_committed"
	DeletionStateRetryRequired       DeletionState = "retry_required"
)

// WitnessHead is a content-free, immutable ledger checkpoint confirmed by the
// configured full witness.
type WitnessHead struct {
	Sequence  uint64
	EntryHash [sha256.Size]byte
}

func (head WitnessHead) Validate() error {
	if head.Sequence == 0 || zeroDeletionDigest(head.EntryHash) {
		return fmt.Errorf("%w: witness head", ErrInvalidDeletionPreview)
	}
	return nil
}

// DeletionRecordSnapshot binds the current record CAS, authorization, delivery
// epoch, and the non-content inventory revisions used by a preview.
type DeletionRecordSnapshot struct {
	RecordID                 string
	CurrentRevisionID        string
	LockVersion              uint64
	AuthorizationEpoch       uint64
	ContentDeliveryEpoch     recordplatform.ContentEpoch
	Authorization            recordauth.ResourceScope
	DependencyGraphDigest    [sha256.Size]byte
	BackupInventoryDigest    [sha256.Size]byte
	ProcessorInventoryDigest [sha256.Size]byte
	ManagedBackup            ManagedBackupSummary
}

func (snapshot DeletionRecordSnapshot) Validate() error {
	if !validDeletionRecordID(snapshot.RecordID) || !validDeletionRevisionID(snapshot.CurrentRevisionID) ||
		snapshot.LockVersion == 0 || snapshot.AuthorizationEpoch == 0 ||
		zeroDeletionDigest(snapshot.DependencyGraphDigest) ||
		zeroDeletionDigest(snapshot.BackupInventoryDigest) ||
		zeroDeletionDigest(snapshot.ProcessorInventoryDigest) || snapshot.ManagedBackup.Validate() != nil {
		return fmt.Errorf("%w: record snapshot", ErrInvalidDeletionPreview)
	}
	if snapshot.Authorization.Version != recordauth.ResourceScopeVersionV1 ||
		snapshot.Authorization.ProjectID != recordauth.ProjectIDDefault {
		return fmt.Errorf("%w: authorization snapshot", ErrInvalidDeletionPreview)
	}
	return nil
}

// PreviewTarget contains only stable identity and concurrency facts. Adapters
// return digests and counts through their own typed implementations; content
// never crosses this orchestration boundary.
type PreviewTarget struct {
	Object                recordplatform.ObjectRef
	CurrentRevisionID     string
	LockVersion           uint64
	AuthorizationEpoch    uint64
	ContentDeliveryEpoch  recordplatform.ContentEpoch
	DependencyGraphDigest [sha256.Size]byte
}

func (target PreviewTarget) Validate() error {
	if err := target.Object.Validate(); err != nil || target.Object.ObjectKind != "record" ||
		!validDeletionRecordID(target.Object.ObjectID) ||
		!validDeletionRevisionID(target.CurrentRevisionID) || target.LockVersion == 0 ||
		target.AuthorizationEpoch == 0 || zeroDeletionDigest(target.DependencyGraphDigest) {
		return fmt.Errorf("%w: adapter preview target", ErrInvalidDeletionPreview)
	}
	return nil
}

type AdapterPreviewSnapshot struct {
	DependencyDigest [sha256.Size]byte
	ImpactDigest     [sha256.Size]byte
	SurvivingCopies  []AdapterSurvivingCopy
}

func (snapshot AdapterPreviewSnapshot) Validate() error {
	if zeroDeletionDigest(snapshot.DependencyDigest) || zeroDeletionDigest(snapshot.ImpactDigest) ||
		validateAdapterSurvivingCopies(snapshot.SurvivingCopies) != nil {
		return fmt.Errorf("%w: adapter preview snapshot", ErrInvalidDeletionPreview)
	}
	return nil
}

// DeletionPreviewAdapter is deliberately separate from Adapter so the
// readiness-only registry tests remain useful while orchestration still fails
// closed if a registered adapter lacks its preview contract.
type DeletionPreviewAdapter interface {
	Adapter
	PreviewDeletion(context.Context, PreviewTarget) (AdapterPreviewSnapshot, error)
}

type DeletionRecordSnapshotSource interface {
	CurrentDeletionSnapshot(context.Context, recordauth.ActorScope, string) (DeletionRecordSnapshot, error)
}

type DeletionWitnessSource interface {
	CurrentWitnessHead(context.Context) (WitnessHead, error)
	VerifyWitnessExtension(context.Context, WitnessHead) (WitnessHead, error)
}

type CreatePreviewCommand struct {
	Object                 recordplatform.ObjectRef
	ActorScopeDigest       [sha256.Size]byte
	TokenCommitment        [sha256.Size]byte
	RequestFingerprint     recordplatform.RequestFingerprintV1
	BindingDigest          [sha256.Size]byte
	Record                 DeletionRecordSnapshot
	WitnessHead            WitnessHead
	AdapterReadinessDigest [sha256.Size]byte
	AdapterPreviewDigest   [sha256.Size]byte
	TTL                    time.Duration
}

func (command CreatePreviewCommand) Validate() error {
	if err := command.Object.Validate(); err != nil || command.Object.ObjectKind != "record" ||
		command.Object.ObjectID != command.Record.RecordID ||
		zeroDeletionDigest(command.ActorScopeDigest) || zeroDeletionDigest(command.TokenCommitment) ||
		zeroDeletionDigest(command.BindingDigest) || zeroDeletionDigest(command.AdapterReadinessDigest) ||
		zeroDeletionDigest(command.AdapterPreviewDigest) || command.TTL != DeletionPreviewTTL {
		return fmt.Errorf("%w: create command", ErrInvalidDeletionPreview)
	}
	if err := command.RequestFingerprint.Validate(); err != nil {
		return fmt.Errorf("%w: request fingerprint", ErrInvalidDeletionPreview)
	}
	if err := command.Record.Validate(); err != nil {
		return err
	}
	return command.WitnessHead.Validate()
}

type PreviewLookup struct {
	ReservationID string
	Object        recordplatform.ObjectRef
	Token         recordplatform.DeletionRequestTokenTransportV1
}

func (lookup PreviewLookup) Validate() error {
	if !validDeletionReservationID(lookup.ReservationID) ||
		lookup.Object.Validate() != nil || lookup.Object.ObjectKind != "record" ||
		!validDeletionRecordID(lookup.Object.ObjectID) {
		return fmt.Errorf("%w: lookup", ErrInvalidDeletionPreview)
	}
	return nil
}

type StoredPreview struct {
	ReservationID      string
	Object             recordplatform.ObjectRef
	ActorScopeDigest   [sha256.Size]byte
	TokenCommitment    [sha256.Size]byte
	RequestFingerprint recordplatform.PersistedRequestFingerprintV1
	BindingDigest      [sha256.Size]byte
	WitnessHead        WitnessHead
	ExpiresAt          time.Time
	Operation          *DeletionOperation
}

func (preview StoredPreview) Validate() error {
	if !validDeletionReservationID(preview.ReservationID) || preview.Object.Validate() != nil ||
		preview.Object.ObjectKind != "record" || !validDeletionRecordID(preview.Object.ObjectID) ||
		zeroDeletionDigest(preview.ActorScopeDigest) || zeroDeletionDigest(preview.TokenCommitment) ||
		zeroDeletionDigest(preview.BindingDigest) || preview.ExpiresAt.IsZero() ||
		preview.RequestFingerprint.Validate() != nil || preview.WitnessHead.Validate() != nil {
		return fmt.Errorf("%w: stored preview", ErrInvalidDeletionPreview)
	}
	if preview.Operation != nil {
		if err := preview.Operation.Validate(); err != nil ||
			preview.Operation.ReservationID != preview.ReservationID || preview.Operation.Object != preview.Object {
			return fmt.Errorf("%w: preview operation", ErrInvalidDeletionPreview)
		}
	}
	return nil
}

type ReservePreviewCommand struct {
	DeploymentID            recordplatform.DeploymentID
	ActorID                 string
	DeletionContractVersion uint64
	Preview                 StoredPreview
	Record                  DeletionRecordSnapshot
	ExpectedBindingDigest   [sha256.Size]byte
	RequestFingerprint      recordplatform.RequestFingerprintV1
	ObservedWitnessHead     WitnessHead
	OwnerID                 string
	OwnerLeaseDuration      time.Duration
	ReasonCode              DeletionReasonCode
}

func (command ReservePreviewCommand) Validate() error {
	if recordplatform.ValidateDeploymentID(command.DeploymentID) != nil ||
		recordauth.ValidateActorUserID(command.ActorID) != nil ||
		command.DeletionContractVersion != RecordDeletionContractVersionV1 {
		return fmt.Errorf("%w: durable ledger identity", ErrInvalidDeletionPreview)
	}
	if err := command.Preview.Validate(); err != nil || command.Preview.Operation != nil {
		return fmt.Errorf("%w: reserve preview", ErrInvalidDeletionPreview)
	}
	if err := command.Record.Validate(); err != nil || command.Record.RecordID != command.Preview.Object.ObjectID {
		return fmt.Errorf("%w: reserve record", ErrInvalidDeletionPreview)
	}
	if zeroDeletionDigest(command.ExpectedBindingDigest) || command.ExpectedBindingDigest != command.Preview.BindingDigest ||
		command.RequestFingerprint.Validate() != nil ||
		!command.RequestFingerprint.MatchesPersisted(command.Preview.RequestFingerprint) ||
		command.ObservedWitnessHead.Validate() != nil || !validRecordDeletionReason(command.ReasonCode) {
		return fmt.Errorf("%w: reserve binding", ErrInvalidDeletionPreview)
	}
	if err := (recordplatform.LeaseClaimInputV1{OwnerID: command.OwnerID, LeaseDuration: command.OwnerLeaseDuration}).Validate(); err != nil {
		return fmt.Errorf("%w: reserve owner", ErrInvalidDeletionPreview)
	}
	return nil
}

type DeletionOperation struct {
	OperationID     string
	ReservationID   string
	Object          recordplatform.ObjectRef
	ReasonCode      DeletionReasonCode
	State           DeletionState
	FenceEpoch      recordplatform.ContentEpoch
	LedgerSequence  uint64
	LedgerEntryHash [sha256.Size]byte
	ReleaseEpoch    uint64
	ReceiptDigest   [sha256.Size]byte
}

func (operation DeletionOperation) Validate() error {
	if !validDeletionOperationID(operation.OperationID) || !validDeletionReservationID(operation.ReservationID) ||
		operation.Object.Validate() != nil || operation.Object.ObjectKind != "record" ||
		!validDeletionRecordID(operation.Object.ObjectID) || !validRecordDeletionReason(operation.ReasonCode) ||
		operation.FenceEpoch == 0 || !knownDeletionState(operation.State) {
		return ErrInvalidDeletionOperation
	}
	hasLedger := operation.LedgerSequence > 0 && !zeroDeletionDigest(operation.LedgerEntryHash)
	hasPartialLedger := (operation.LedgerSequence > 0) != !zeroDeletionDigest(operation.LedgerEntryHash)
	if hasPartialLedger {
		return ErrInvalidDeletionOperation
	}
	switch operation.State {
	case DeletionStateProvisionalFenced, DeletionStateLedgerCommitUnknown:
		if hasLedger || operation.ReleaseEpoch != 0 || !zeroDeletionDigest(operation.ReceiptDigest) {
			return ErrInvalidDeletionOperation
		}
	case DeletionStateWitnessPending, DeletionStateDeleteRequested, DeletionStateFencePropagating,
		DeletionStateReadFenced, DeletionStateOnlinePurging, DeletionStateRetryRequired:
		if !hasLedger || operation.ReleaseEpoch != 0 || !zeroDeletionDigest(operation.ReceiptDigest) {
			return ErrInvalidDeletionOperation
		}
	case DeletionStateOnlinePurged:
		if !hasLedger || operation.ReleaseEpoch != 0 || zeroDeletionDigest(operation.ReceiptDigest) {
			return ErrInvalidDeletionOperation
		}
	case DeletionStateReleasePending:
		if operation.ReleaseEpoch == 0 || !zeroDeletionDigest(operation.ReceiptDigest) {
			return ErrInvalidDeletionOperation
		}
	case DeletionStateNotCommitted:
		if !hasLedger || operation.ReleaseEpoch == 0 || !zeroDeletionDigest(operation.ReceiptDigest) {
			return ErrInvalidDeletionOperation
		}
	}
	return nil
}

type DeletionPreviewRepository interface {
	CreatePreview(context.Context, CreatePreviewCommand) (StoredPreview, error)
	ResolvePreview(context.Context, PreviewLookup) (StoredPreview, error)
	ReservePreview(context.Context, ReservePreviewCommand) (DeletionOperation, error)
	ResolveOperationStatus(context.Context, recordplatform.ProjectID, string) (DeletionOperationStatus, error)
}

type DeletionOperationStatus struct {
	Operation        DeletionOperation
	InitiatorActorID string
}

func (status DeletionOperationStatus) Validate() error {
	if status.Operation.Validate() != nil || recordauth.ValidateActorUserID(status.InitiatorActorID) != nil {
		return ErrDeletionStatusUnavailable
	}
	return nil
}

type ServiceOptions struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
}

type Service struct {
	deploymentID recordplatform.DeploymentID
	registry     Registry
	records      DeletionRecordSnapshotSource
	witness      DeletionWitnessSource
	repository   DeletionPreviewRepository
	options      ServiceOptions
}

func NewService(
	deploymentID recordplatform.DeploymentID,
	registry Registry,
	records DeletionRecordSnapshotSource,
	witness DeletionWitnessSource,
	repository DeletionPreviewRepository,
	options ServiceOptions,
) (*Service, error) {
	if recordplatform.ValidateDeploymentID(deploymentID) != nil ||
		nilDeletionServiceDependency(records) || nilDeletionServiceDependency(witness) ||
		nilDeletionServiceDependency(repository) {
		return nil, ErrInvalidDeletionService
	}
	if err := (recordplatform.LeaseClaimInputV1{OwnerID: options.OwnerID, LeaseDuration: options.OwnerLeaseDuration}).Validate(); err != nil {
		return nil, fmt.Errorf("%w: owner", ErrInvalidDeletionService)
	}
	return &Service{
		deploymentID: deploymentID,
		registry:     registry,
		records:      records,
		witness:      witness,
		repository:   repository,
		options:      options,
	}, nil
}

type PreviewRequest struct {
	Actor    recordauth.ActorScope
	RecordID string
}

type PreviewResult struct {
	ReservationID string
	Token         recordplatform.IssuedDeletionRequestTokenV1
	ExpiresAt     time.Time
	Summary       PreviewSummary
}

func (service *Service) Preview(ctx context.Context, request PreviewRequest) (PreviewResult, error) {
	actor, object, err := service.validateRequest(ctx, request.Actor, request.RecordID)
	if err != nil {
		return PreviewResult{}, err
	}
	readiness, err := service.registry.RequireReady(ctx)
	if err != nil {
		return PreviewResult{}, err
	}
	recordSnapshot, err := service.currentAuthorizedSnapshot(ctx, actor, request.RecordID)
	if err != nil {
		return PreviewResult{}, err
	}
	adapterPreview, err := service.registry.previewSnapshot(ctx, previewTarget(object, recordSnapshot))
	if err != nil {
		return PreviewResult{}, err
	}
	witnessHead, err := service.witness.CurrentWitnessHead(ctx)
	if err != nil {
		return PreviewResult{}, fmt.Errorf("%w: current witness head: %w", ErrDeletionSafetyUnavailable, err)
	}
	if err := witnessHead.Validate(); err != nil {
		return PreviewResult{}, fmt.Errorf("%w: %w", ErrDeletionSafetyUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return PreviewResult{}, err
	}

	summary := PreviewSummary{
		OnlinePurgeScopes: adapterPreview.onlinePurgeScopes,
		SurvivingCopies:   adapterPreview.survivingCopies,
		ManagedBackup:     recordSnapshot.ManagedBackup,
		LedgerHealth:      LedgerHealthHealthy,
	}
	if err := summary.Validate(); err != nil {
		return PreviewResult{}, fmt.Errorf("%w: invalid safety summary", ErrDeletionSafetyUnavailable)
	}
	bindingDigest := digestPreviewBinding(actor, object, recordSnapshot, readiness.Digest(), adapterPreview.digest, witnessHead)
	token, err := recordplatform.NewIssuedDeletionRequestTokenV1()
	if err != nil {
		return PreviewResult{}, fmt.Errorf("issue deletion request token: %w", err)
	}
	commitment, err := token.Commitment(service.deploymentID, recordplatform.ProjectID(actor.ProjectID))
	if err != nil {
		return PreviewResult{}, fmt.Errorf("commit deletion request token: %w", err)
	}
	fingerprint, err := service.requestFingerprint(actor, object, bindingDigest)
	if err != nil {
		return PreviewResult{}, err
	}
	command := CreatePreviewCommand{
		Object:                 object,
		ActorScopeDigest:       actor.CanonicalHash(),
		TokenCommitment:        commitment,
		RequestFingerprint:     fingerprint,
		BindingDigest:          bindingDigest,
		Record:                 recordSnapshot,
		WitnessHead:            witnessHead,
		AdapterReadinessDigest: readiness.Digest(),
		AdapterPreviewDigest:   adapterPreview.digest,
		TTL:                    DeletionPreviewTTL,
	}
	if err := command.Validate(); err != nil {
		return PreviewResult{}, err
	}
	stored, err := service.repository.CreatePreview(ctx, command)
	if err != nil {
		return PreviewResult{}, err
	}
	if err := validateCreatedPreview(stored, command); err != nil {
		return PreviewResult{}, fmt.Errorf("%w: created preview: %w", ErrDeletionSafetyUnavailable, err)
	}
	return PreviewResult{
		ReservationID: stored.ReservationID,
		Token:         token,
		ExpiresAt:     stored.ExpiresAt,
		Summary:       summary.clone(),
	}, nil
}

type ExecuteRequest struct {
	Actor         recordauth.ActorScope
	RecordID      string
	ReservationID string
	Token         recordplatform.DeletionRequestTokenTransportV1
	ReasonCode    DeletionReasonCode
}

func (service *Service) Execute(ctx context.Context, request ExecuteRequest) (DeletionOperation, error) {
	actor, object, err := service.validateRequest(ctx, request.Actor, request.RecordID)
	if err != nil {
		return DeletionOperation{}, err
	}
	if request.ReasonCode != DeletionReasonUserConfirmed {
		return DeletionOperation{}, fmt.Errorf("%w: record deletion reason", ErrInvalidDeletionPreview)
	}
	lookup := PreviewLookup{ReservationID: request.ReservationID, Object: object, Token: request.Token}
	if err := lookup.Validate(); err != nil {
		return DeletionOperation{}, err
	}
	stored, err := service.repository.ResolvePreview(ctx, lookup)
	if err != nil {
		return DeletionOperation{}, err
	}
	if err := stored.Validate(); err != nil {
		return DeletionOperation{}, fmt.Errorf("%w: persisted preview", ErrDeletionSafetyUnavailable)
	}
	if stored.Object != object || !request.Token.MatchesCommitment(service.deploymentID, recordplatform.ProjectID(actor.ProjectID), stored.TokenCommitment) ||
		stored.ActorScopeDigest != actor.CanonicalHash() {
		return DeletionOperation{}, ErrDeletionRequestTokenReused
	}
	if stored.Operation != nil {
		if stored.Operation.ReasonCode != request.ReasonCode {
			return DeletionOperation{}, ErrDeletionRequestTokenReused
		}
		return *stored.Operation, nil
	}

	readiness, err := service.registry.RequireReady(ctx)
	if err != nil {
		return DeletionOperation{}, err
	}
	recordSnapshot, err := service.currentAuthorizedSnapshot(ctx, actor, request.RecordID)
	if err != nil {
		return DeletionOperation{}, err
	}
	adapterPreview, err := service.registry.previewSnapshot(ctx, previewTarget(object, recordSnapshot))
	if err != nil {
		return DeletionOperation{}, err
	}
	bindingDigest := digestPreviewBinding(actor, object, recordSnapshot, readiness.Digest(), adapterPreview.digest, stored.WitnessHead)
	if bindingDigest != stored.BindingDigest {
		return DeletionOperation{}, ErrDeletionPreviewStale
	}
	currentWitnessHead, err := service.witness.VerifyWitnessExtension(ctx, stored.WitnessHead)
	if err != nil {
		return DeletionOperation{}, fmt.Errorf("%w: verify witness extension: %w", ErrDeletionSafetyUnavailable, err)
	}
	if err := currentWitnessHead.Validate(); err != nil || currentWitnessHead.Sequence < stored.WitnessHead.Sequence ||
		(currentWitnessHead.Sequence == stored.WitnessHead.Sequence && currentWitnessHead.EntryHash != stored.WitnessHead.EntryHash) {
		return DeletionOperation{}, fmt.Errorf("%w: invalid witness extension", ErrDeletionSafetyUnavailable)
	}
	fingerprint, err := service.requestFingerprint(actor, object, bindingDigest)
	if err != nil {
		return DeletionOperation{}, err
	}
	if !fingerprint.MatchesPersisted(stored.RequestFingerprint) {
		return DeletionOperation{}, ErrDeletionRequestTokenReused
	}
	command := ReservePreviewCommand{
		DeploymentID:            service.deploymentID,
		ActorID:                 actor.UserID,
		DeletionContractVersion: RecordDeletionContractVersionV1,
		Preview:                 stored,
		Record:                  recordSnapshot,
		ExpectedBindingDigest:   bindingDigest,
		RequestFingerprint:      fingerprint,
		ObservedWitnessHead:     currentWitnessHead,
		OwnerID:                 service.options.OwnerID,
		OwnerLeaseDuration:      service.options.OwnerLeaseDuration,
		ReasonCode:              request.ReasonCode,
	}
	if err := command.Validate(); err != nil {
		return DeletionOperation{}, err
	}
	operation, err := service.repository.ReservePreview(ctx, command)
	if err != nil {
		return DeletionOperation{}, err
	}
	if err := operation.Validate(); err != nil || operation.ReservationID != stored.ReservationID || operation.Object != object || operation.ReasonCode != request.ReasonCode {
		return DeletionOperation{}, fmt.Errorf("%w: reserved operation", ErrDeletionSafetyUnavailable)
	}
	return operation, nil
}

type StatusRequest struct {
	Actor       recordauth.ActorScope
	OperationID string
}

func (service *Service) Status(ctx context.Context, request StatusRequest) (DeletionOperation, error) {
	if ctx == nil || service == nil || nilDeletionServiceDependency(service.repository) {
		return DeletionOperation{}, ErrInvalidDeletionService
	}
	if err := ctx.Err(); err != nil {
		return DeletionOperation{}, err
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil || !validDeletionOperationID(request.OperationID) {
		return DeletionOperation{}, fmt.Errorf("%w: actor or operation", ErrInvalidDeletionOperation)
	}
	status, err := service.repository.ResolveOperationStatus(
		ctx,
		recordplatform.ProjectID(actor.ProjectID),
		request.OperationID,
	)
	if errors.Is(err, ErrDeletionOperationNotFound) {
		return DeletionOperation{}, ErrDeletionOperationNotFound
	}
	if err != nil {
		return DeletionOperation{}, ErrDeletionStatusUnavailable
	}
	if status.Validate() != nil || status.Operation.OperationID != request.OperationID ||
		status.Operation.Object.ProjectID != string(actor.ProjectID) {
		return DeletionOperation{}, ErrDeletionStatusUnavailable
	}
	if status.InitiatorActorID != actor.UserID && actor.Role != recordauth.RoleProjectAdmin {
		return DeletionOperation{}, ErrDeletionOperationNotFound
	}
	return status.Operation, nil
}

func (service *Service) validateRequest(ctx context.Context, actorInput recordauth.ActorScope, recordID string) (recordauth.ActorScope, recordplatform.ObjectRef, error) {
	if ctx == nil || service == nil || nilDeletionServiceDependency(service.records) ||
		nilDeletionServiceDependency(service.witness) || nilDeletionServiceDependency(service.repository) {
		return recordauth.ActorScope{}, recordplatform.ObjectRef{}, ErrInvalidDeletionService
	}
	if err := ctx.Err(); err != nil {
		return recordauth.ActorScope{}, recordplatform.ObjectRef{}, err
	}
	actor, err := recordauth.NormalizeActorScope(actorInput)
	if err != nil || !validDeletionRecordID(recordID) {
		return recordauth.ActorScope{}, recordplatform.ObjectRef{}, fmt.Errorf("%w: actor or record", ErrInvalidDeletionPreview)
	}
	object := recordplatform.ObjectRef{ProjectID: string(actor.ProjectID), ObjectKind: "record", ObjectID: recordID}
	if err := object.Validate(); err != nil {
		return recordauth.ActorScope{}, recordplatform.ObjectRef{}, fmt.Errorf("%w: object", ErrInvalidDeletionPreview)
	}
	return actor, object, nil
}

func (service *Service) currentAuthorizedSnapshot(ctx context.Context, actor recordauth.ActorScope, recordID string) (DeletionRecordSnapshot, error) {
	snapshot, err := service.records.CurrentDeletionSnapshot(ctx, actor.Clone(), recordID)
	if err != nil {
		return DeletionRecordSnapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return DeletionRecordSnapshot{}, err
	}
	if err := snapshot.Validate(); err != nil || snapshot.RecordID != recordID || snapshot.Authorization.ProjectID != actor.ProjectID {
		return DeletionRecordSnapshot{}, fmt.Errorf("%w: invalid current record snapshot", ErrDeletionSafetyUnavailable)
	}
	if err := recordauth.Authorize(actor, recordauth.CapabilityRecordPermanentDelete, snapshot.Authorization); err != nil {
		return DeletionRecordSnapshot{}, err
	}
	return snapshot, nil
}

func (service *Service) requestFingerprint(actor recordauth.ActorScope, object recordplatform.ObjectRef, binding [sha256.Size]byte) (recordplatform.RequestFingerprintV1, error) {
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordPermanentDelete,
		ProjectID:          recordplatform.ProjectID(actor.ProjectID),
		ActorScopeDigest:   actor.CanonicalHash(),
		RequestScopeDigest: digestDeletionRequestScope(service.deploymentID, object),
		PayloadDigest:      binding,
	})
	if err != nil {
		return recordplatform.RequestFingerprintV1{}, fmt.Errorf("%w: request fingerprint", ErrInvalidDeletionPreview)
	}
	return fingerprint, nil
}

type adapterPreviewAggregate struct {
	digest            [sha256.Size]byte
	onlinePurgeScopes []AdapterName
	survivingCopies   []SurvivingCopySummary
}

func (registry Registry) previewSnapshot(ctx context.Context, target PreviewTarget) (adapterPreviewAggregate, error) {
	if err := target.Validate(); err != nil {
		return adapterPreviewAggregate{}, err
	}
	payload := make([]byte, 0, 1024)
	payload = appendLengthPrefixed(payload, deletionAdapterPreviewDomainV1)
	payload = appendUint64(payload, 1)
	aggregate := adapterPreviewAggregate{
		onlinePurgeScopes: make([]AdapterName, 0, len(requiredAdapterNames)),
		survivingCopies:   make([]SurvivingCopySummary, 0),
	}
	for _, name := range requiredAdapterNames {
		registered, ok := registry.adapters[name]
		if !ok {
			return adapterPreviewAggregate{}, ErrDeletionSafetyUnavailable
		}
		adapter, ok := registered.adapter.(DeletionPreviewAdapter)
		if !ok || nilDeletionServiceDependency(adapter) {
			return adapterPreviewAggregate{}, fmt.Errorf("%w: adapter %q preview contract", ErrDeletionSafetyUnavailable, name)
		}
		if err := ctx.Err(); err != nil {
			return adapterPreviewAggregate{}, err
		}
		snapshot, err := adapter.PreviewDeletion(ctx, target)
		if err != nil {
			return adapterPreviewAggregate{}, fmt.Errorf("%w: adapter %q preview: %w", ErrDeletionSafetyUnavailable, name, err)
		}
		if err := ctx.Err(); err != nil {
			return adapterPreviewAggregate{}, err
		}
		if err := snapshot.Validate(); err != nil {
			return adapterPreviewAggregate{}, fmt.Errorf("%w: adapter %q preview snapshot", ErrDeletionSafetyUnavailable, name)
		}
		aggregate.onlinePurgeScopes = append(aggregate.onlinePurgeScopes, name)
		payload = appendLengthPrefixed(payload, string(name))
		payload = append(payload, snapshot.DependencyDigest[:]...)
		payload = append(payload, snapshot.ImpactDigest[:]...)
		payload = appendUint64(payload, uint64(len(snapshot.SurvivingCopies)))
		for _, surviving := range snapshot.SurvivingCopies {
			payload = appendLengthPrefixed(payload, string(surviving.Kind))
			payload = appendUint64(payload, surviving.CopyCount)
			aggregate.survivingCopies = append(aggregate.survivingCopies, SurvivingCopySummary{
				Scope:     name,
				Kind:      surviving.Kind,
				CopyCount: surviving.CopyCount,
			})
		}
	}
	aggregate.digest = sha256.Sum256(payload)
	return aggregate, nil
}

func previewTarget(object recordplatform.ObjectRef, snapshot DeletionRecordSnapshot) PreviewTarget {
	return PreviewTarget{
		Object:                object,
		CurrentRevisionID:     snapshot.CurrentRevisionID,
		LockVersion:           snapshot.LockVersion,
		AuthorizationEpoch:    snapshot.AuthorizationEpoch,
		ContentDeliveryEpoch:  snapshot.ContentDeliveryEpoch,
		DependencyGraphDigest: snapshot.DependencyGraphDigest,
	}
}

func digestPreviewBinding(
	actor recordauth.ActorScope,
	object recordplatform.ObjectRef,
	record DeletionRecordSnapshot,
	readinessDigest [sha256.Size]byte,
	adapterDigest [sha256.Size]byte,
	witness WitnessHead,
) [sha256.Size]byte {
	payload := make([]byte, 0, 512)
	payload = appendLengthPrefixed(payload, deletionPreviewBindingDomainV1)
	payload = appendUint64(payload, 1)
	actorDigest := actor.CanonicalHash()
	payload = append(payload, actorDigest[:]...)
	payload = appendLengthPrefixed(payload, object.ProjectID)
	payload = appendLengthPrefixed(payload, object.ObjectKind)
	payload = appendLengthPrefixed(payload, object.ObjectID)
	payload = appendLengthPrefixed(payload, record.CurrentRevisionID)
	payload = appendUint64(payload, record.LockVersion)
	payload = appendUint64(payload, record.AuthorizationEpoch)
	payload = appendUint64(payload, uint64(record.ContentDeliveryEpoch))
	authorizationDigest := digestDeletionAuthorization(record.Authorization)
	payload = append(payload, authorizationDigest[:]...)
	payload = append(payload, record.DependencyGraphDigest[:]...)
	payload = append(payload, record.BackupInventoryDigest[:]...)
	payload = append(payload, record.ProcessorInventoryDigest[:]...)
	payload = appendUint64(payload, record.ManagedBackup.RetainedCopyCount)
	payload = appendUint64(payload, uint64(record.ManagedBackup.MaximumRetentionDays))
	if record.ManagedBackup.LatestExpiresAt.IsZero() {
		payload = append(payload, 0)
	} else {
		payload = append(payload, 1)
		payload = appendUint64(payload, uint64(record.ManagedBackup.LatestExpiresAt.UTC().UnixNano()))
	}
	payload = append(payload, readinessDigest[:]...)
	payload = append(payload, adapterDigest[:]...)
	payload = appendUint64(payload, witness.Sequence)
	payload = append(payload, witness.EntryHash[:]...)
	return sha256.Sum256(payload)
}

func digestDeletionAuthorization(scope recordauth.ResourceScope) [sha256.Size]byte {
	payload := make([]byte, 0, 256)
	payload = appendLengthPrefixed(payload, "houfeng.record-deletion.authorization.v1")
	payload = appendUint64(payload, uint64(scope.Version))
	payload = appendLengthPrefixed(payload, string(scope.ProjectID))
	payload = append(payload, scope.Visibility.CanonicalHash[:]...)
	payload = appendUint64(payload, uint64(len(scope.Sources)))
	for _, source := range scope.Sources {
		payload = appendLengthPrefixed(payload, string(source.Kind))
		payload = appendLengthPrefixed(payload, source.SourceID)
		payload = appendLengthPrefixed(payload, string(source.State))
		payload = append(payload, source.Digest[:]...)
	}
	return sha256.Sum256(payload)
}

func digestDeletionRequestScope(deploymentID recordplatform.DeploymentID, object recordplatform.ObjectRef) [sha256.Size]byte {
	payload := make([]byte, 0, 256)
	payload = appendLengthPrefixed(payload, deletionRequestScopeDomainV1)
	payload = appendUint64(payload, 1)
	payload = appendLengthPrefixed(payload, string(deploymentID))
	payload = appendLengthPrefixed(payload, object.ProjectID)
	payload = appendLengthPrefixed(payload, "record_permanent_delete")
	payload = appendLengthPrefixed(payload, object.ObjectKind)
	payload = appendLengthPrefixed(payload, object.ObjectID)
	return sha256.Sum256(payload)
}

func validateCreatedPreview(stored StoredPreview, command CreatePreviewCommand) error {
	if err := stored.Validate(); err != nil || stored.Operation != nil || stored.Object != command.Object ||
		stored.ActorScopeDigest != command.ActorScopeDigest || stored.TokenCommitment != command.TokenCommitment ||
		stored.BindingDigest != command.BindingDigest || stored.WitnessHead != command.WitnessHead ||
		!command.RequestFingerprint.MatchesPersisted(stored.RequestFingerprint) {
		return ErrInvalidDeletionPreview
	}
	return nil
}

func knownDeletionState(state DeletionState) bool {
	switch state {
	case DeletionStateProvisionalFenced, DeletionStateLedgerCommitUnknown, DeletionStateWitnessPending,
		DeletionStateDeleteRequested, DeletionStateFencePropagating, DeletionStateReadFenced,
		DeletionStateOnlinePurging, DeletionStateOnlinePurged, DeletionStateReleasePending,
		DeletionStateNotCommitted, DeletionStateRetryRequired:
		return true
	default:
		return false
	}
}

func validRecordDeletionReason(reason DeletionReasonCode) bool {
	switch reason {
	case DeletionReasonUserConfirmed, DeletionReasonSourceRemoved, DeletionReasonRetentionReplay:
		return true
	default:
		return false
	}
}

func validDeletionRecordID(value string) bool {
	return validPrefixedDeletionID(value, "rec_", 64)
}

func validDeletionRevisionID(value string) bool {
	return validPrefixedDeletionID(value, "rrv_", 64)
}

func validDeletionOperationID(value string) bool {
	return validPrefixedDeletionID(value, "rpo_", 64)
}

func validDeletionReservationID(value string) bool {
	return validPrefixedDeletionID(value, "drs_", 64)
}

func validPrefixedDeletionID(value, prefix string, maximumSuffix int) bool {
	if len(value) <= len(prefix) || len(value) > len(prefix)+maximumSuffix || value[:len(prefix)] != prefix {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func zeroDeletionDigest(digest [sha256.Size]byte) bool {
	return digest == [sha256.Size]byte{}
}

func nilDeletionServiceDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}
