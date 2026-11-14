package recordbirth

import (
	"context"
	"errors"
	"fmt"

	"houfeng/internal/center/recordidentity"
)

// Coordinator performs external W2 calls between committed APP commands, never
// inside an APP writer transaction. The continuity reader is independent of the
// optional producer and remains required after a lineage is disabled.
type Coordinator struct {
	application Application
	reader      ContinuityReader
	authority   Authority
}

func NewCoordinator(application Application, reader ContinuityReader, authority Authority) (*Coordinator, error) {
	if application == nil {
		return nil, ErrInvalid
	}
	return &Coordinator{application: application, reader: reader, authority: authority}, nil
}

var _ ContinuitySource = (*Coordinator)(nil)

// ReadContinuity supplies a fresh token to a writer before it opens its APP
// transaction. Only a verified never-installed lineage has an empty token.
func (coordinator *Coordinator) ReadContinuity(ctx context.Context) ([]byte, error) {
	observation, _, err := coordinator.readContinuity(ctx)
	if err != nil {
		return nil, err
	}
	return observation.Proof, nil
}

func (coordinator *Coordinator) readContinuity(ctx context.Context) (ContinuityObservation, bool, error) {
	if ctx == nil || coordinator == nil || coordinator.application == nil {
		return ContinuityObservation{}, false, ErrInvalid
	}
	binding, installed, err := coordinator.application.ContinuityBinding(ctx)
	if err != nil {
		return ContinuityObservation{}, false, err
	}
	if !installed {
		if binding != (Binding{}) || coordinator.reader != nil {
			return ContinuityObservation{}, false, ErrUnavailable
		}
		return ContinuityObservation{}, false, nil
	}
	if binding.Validate() != nil || coordinator.reader == nil {
		return ContinuityObservation{}, false, ErrUnavailable
	}
	observation, err := coordinator.reader.Continuity(ctx, binding)
	if err != nil {
		return ContinuityObservation{}, false, fmt.Errorf("%w: read birth continuity: %w", ErrUnavailable, err)
	}
	decoded, err := DecodeContinuityProof(observation.Proof)
	if err != nil || observation.Binding != binding || decoded.Binding != observation.Binding ||
		decoded.Anchor != observation.Anchor || decoded.Enabled != observation.Enabled {
		return ContinuityObservation{}, false, fmt.Errorf("%w: inconsistent birth continuity observation", ErrUnavailable)
	}
	return decoded, true, nil
}

func currentAllocationBinding(allocation Allocation, observation ContinuityObservation, installed bool) bool {
	if allocation.Domain != recordidentity.Certifiable {
		return true
	}
	return installed && sameBirthLineage(allocation.Binding, observation.Binding) &&
		allocation.Binding.DeploymentID == observation.Binding.DeploymentID &&
		allocation.Binding.Generation <= observation.Binding.Generation
}

func (coordinator *Coordinator) Reserve(ctx context.Context, request Request) (Allocation, error) {
	if ctx == nil || coordinator == nil || coordinator.application == nil || request.Validate() != nil {
		return Allocation{}, ErrInvalid
	}
	allocation, found, err := coordinator.application.FindAllocation(ctx, request)
	if err != nil {
		return Allocation{}, err
	}
	if found {
		if allocation.Validate() != nil || allocation.Request != request {
			return Allocation{}, ErrInvalid
		}
		switch allocation.State {
		case StateFinished, StateConfirmed, StateTombstoned:
			// These are authorized read-only results. No downstream write may
			// reuse a stored or stale observation from the durable projection.
			allocation.ContinuityProof = nil
			return allocation, nil
		}
	}
	continuity, installed, err := coordinator.readContinuity(ctx)
	if err != nil {
		return Allocation{}, err
	}
	if !found {
		var selected *Binding
		binding, enabled, err := coordinator.application.CurrentBinding(ctx)
		if err != nil {
			return Allocation{}, err
		}
		if enabled {
			if !installed || !continuity.Enabled || binding != continuity.Binding || binding.Validate() != nil {
				return Allocation{}, ErrUnavailable
			}
			if coordinator.authority != nil {
				// Availability observation never allocates an identity. A failed
				// producer may select ordinary only after continuity succeeded.
				observation, err := coordinator.authority.Observe(ctx, binding, "")
				if err == nil && observation.Binding == binding {
					selected = &binding
				}
			}
		} else if installed && continuity.Enabled {
			return Allocation{}, ErrUnavailable
		}
		allocation, err = coordinator.application.BeginAllocation(ctx, request, selected, continuity.Proof)
		if err != nil {
			return Allocation{}, err
		}
	}
	if allocation.Validate() != nil || allocation.Request != request {
		return Allocation{}, ErrInvalid
	}
	if !currentAllocationBinding(allocation, continuity, installed) {
		return Allocation{}, ErrUnavailable
	}
	if allocation.State != StatePending {
		allocation.ContinuityProof = continuity.Proof
		return allocation, nil
	}
	if coordinator.authority == nil {
		return Allocation{}, ErrPending
	}
	// BeginAllocation has committed before the separate read-only proof query.
	// Anchor CAS conflicts retry only the same immutable winning allocation.
	for range 3 {
		continuity, installed, err = coordinator.readContinuity(ctx)
		if err != nil {
			return Allocation{}, err
		}
		if !currentAllocationBinding(allocation, continuity, installed) {
			return Allocation{}, ErrUnavailable
		}
		if !continuity.Enabled {
			return Allocation{}, ErrPending
		}
		rawProof, err := coordinator.application.CheckpointProof(ctx, request, continuity.Anchor)
		if err != nil {
			return Allocation{}, pending(err)
		}
		proof, err := DecodeCheckpointProof(rawProof)
		if err != nil || proof.Binding != continuity.Binding || proof.ExpectedAnchor != continuity.Anchor ||
			proof.Checkpoint.Request != request || proof.Checkpoint.AllocationID != allocation.AllocationID ||
			proof.Checkpoint.Binding != allocation.Binding {
			return Allocation{}, fmt.Errorf("%w: inconsistent APP checkpoint proof", ErrUnavailable)
		}
		receipt, err := coordinator.authority.Reserve(ctx, proof.Binding, request, rawProof)
		if errors.Is(err, ErrCheckpointChanged) {
			continue
		}
		if err != nil {
			return Allocation{}, pending(err)
		}
		event, err := DecodeReceipt(receipt)
		if err != nil || event.Kind != EventReserved || event.Request != request ||
			!sameBirthLineage(event.Binding, allocation.Binding) || event.Binding.DeploymentID != allocation.Binding.DeploymentID ||
			event.Binding.Generation > proof.Binding.Generation {
			return Allocation{}, ErrInvalid
		}
		continuity, installed, err = coordinator.readContinuity(ctx)
		if err != nil {
			return Allocation{}, err
		}
		if !currentAllocationBinding(allocation, continuity, installed) {
			return Allocation{}, ErrUnavailable
		}
		registered, err := coordinator.application.RegisterReceipt(ctx, request, receipt, continuity.Proof)
		if err != nil {
			return Allocation{}, err
		}
		if registered.Validate() != nil || registered.Request != request || registered.AllocationID != allocation.AllocationID ||
			registered.Binding != allocation.Binding || registered.Domain != recordidentity.Certifiable ||
			registered.RecordID != event.RecordID || registered.ReservationID != event.ReservationID {
			return Allocation{}, ErrInvalid
		}
		registered.ContinuityProof = continuity.Proof
		return registered, nil
	}
	return Allocation{}, ErrPending
}

func (coordinator *Coordinator) PrepareCreate(ctx context.Context, request Request, creation InitialCreation) (Permit, error) {
	if ctx == nil || coordinator == nil || coordinator.application == nil || request.Validate() != nil || creation.Validate() != nil {
		return Permit{}, ErrInvalid
	}
	continuity, installed, err := coordinator.readContinuity(ctx)
	if err != nil {
		return Permit{}, err
	}
	allocation, err := coordinator.application.PrepareAllocation(ctx, request, creation, continuity.Proof)
	if err != nil {
		return Permit{}, err
	}
	if allocation.Validate() != nil || allocation.Request != request || allocation.Creation != creation {
		return Permit{}, ErrInvalid
	}
	if !currentAllocationBinding(allocation, continuity, installed) {
		return Permit{}, ErrUnavailable
	}
	if allocation.State == StateTombstoned {
		return Permit{}, ErrConflict
	}
	if allocation.State == StatePreparing {
		if coordinator.authority == nil {
			return Permit{}, ErrPending
		}
		continuity, installed, err = coordinator.readContinuity(ctx)
		if err != nil {
			return Permit{}, err
		}
		if !currentAllocationBinding(allocation, continuity, installed) {
			return Permit{}, ErrUnavailable
		}
		if !continuity.Enabled {
			return Permit{}, ErrPending
		}
		commitment, err := creation.Commitment()
		if err != nil {
			return Permit{}, err
		}
		receipt, err := coordinator.authority.Prepare(ctx, continuity.Binding, allocation.ReservationID, commitment)
		if err != nil {
			return Permit{}, pending(err)
		}
		if !receiptMatchesAllocation(receipt, allocation, EventPrepared) {
			return Permit{}, ErrInvalid
		}
		continuity, installed, err = coordinator.readContinuity(ctx)
		if err != nil {
			return Permit{}, err
		}
		if !currentAllocationBinding(allocation, continuity, installed) {
			return Permit{}, ErrUnavailable
		}
		registered, err := coordinator.application.RegisterReceipt(ctx, request, receipt, continuity.Proof)
		if err != nil {
			return Permit{}, err
		}
		if registered.AllocationID != allocation.AllocationID || registered.Binding != allocation.Binding ||
			registered.Domain != allocation.Domain || registered.ReservationID != allocation.ReservationID || registered.RecordID != allocation.RecordID {
			return Permit{}, ErrInvalid
		}
		allocation = registered
	}
	if allocation.Validate() != nil || allocation.Request != request || allocation.Creation != creation {
		return Permit{}, ErrInvalid
	}
	if !currentAllocationBinding(allocation, continuity, installed) {
		return Permit{}, ErrUnavailable
	}
	switch allocation.State {
	case StatePrepared, StateFinished, StateConfirmed:
		return Permit{
			AllocationID: allocation.AllocationID, ReservationID: allocation.ReservationID,
			RecordID: allocation.RecordID, Creation: creation, PrepareReceipt: allocation.PrepareReceipt,
			ContinuityProof: continuity.Proof,
		}, nil
	default:
		return Permit{}, ErrPending
	}
}

// ConfirmCreate requires an actual admitted, committed APP first revision. An
// uncertain confirmation does not undo that success or authorize another create.
func (coordinator *Coordinator) ConfirmCreate(ctx context.Context, recordID string) error {
	if ctx == nil || coordinator == nil || coordinator.application == nil || !recordidentity.IsValid(recordID) {
		return ErrInvalid
	}
	allocation, err := coordinator.application.CommittedCreation(ctx, recordID)
	if err != nil {
		return err
	}
	if allocation.Validate() != nil || allocation.RecordID != recordID {
		return ErrInvalid
	}
	if allocation.State != StateFinished && allocation.State != StateConfirmed {
		return ErrPending
	}
	if allocation.Domain == recordidentity.Ordinary || allocation.State == StateConfirmed {
		return nil
	}
	continuity, installed, err := coordinator.readContinuity(ctx)
	if err != nil {
		return err
	}
	if !currentAllocationBinding(allocation, continuity, installed) {
		return ErrUnavailable
	}
	if coordinator.authority == nil || !continuity.Enabled {
		return ErrPending
	}
	commitment, err := allocation.Creation.Commitment()
	if err != nil {
		return err
	}
	receipt, err := coordinator.authority.Confirm(ctx, continuity.Binding, allocation.ReservationID, commitment)
	if err != nil {
		return pending(err)
	}
	if !receiptMatchesAllocation(receipt, allocation, EventCreated) {
		return ErrInvalid
	}
	continuity, installed, err = coordinator.readContinuity(ctx)
	if err != nil {
		return err
	}
	if !currentAllocationBinding(allocation, continuity, installed) {
		return ErrUnavailable
	}
	registered, err := coordinator.application.RegisterReceipt(ctx, allocation.Request, receipt, continuity.Proof)
	if err != nil {
		return err
	}
	if registered.Validate() != nil || registered.AllocationID != allocation.AllocationID || registered.State != StateConfirmed ||
		registered.Binding != allocation.Binding || registered.Creation != allocation.Creation || registered.Request != allocation.Request {
		return ErrInvalid
	}
	return nil
}

type Certification struct {
	Binding           Binding
	AllocationID      string
	ReservationID     string
	RecordID          string
	InitialCommitment [32]byte
	CreatedReceipt    []byte
}

// ReadCertificationObservation reads fresh W2 continuity and the exact record
// observation. APP certification must still be verified in the caller's transaction.
func (coordinator *Coordinator) ReadCertificationObservation(ctx context.Context, recordID string) (Observation, error) {
	if ctx == nil || coordinator == nil || coordinator.application == nil || !recordidentity.IsValid(recordID) {
		return Observation{}, ErrInvalid
	}
	if recordidentity.Classify(recordID) != recordidentity.Certifiable {
		return Observation{}, ErrUncertified
	}
	reader, ok := coordinator.reader.(interface {
		Observe(context.Context, Binding, string) (Observation, error)
	})
	if !ok {
		return Observation{}, ErrUnavailable
	}
	continuity, installed, err := coordinator.readContinuity(ctx)
	if err != nil {
		return Observation{}, err
	}
	binding, enabled, err := coordinator.application.CurrentBinding(ctx)
	if err != nil {
		return Observation{}, err
	}
	if !installed || !enabled || !continuity.Enabled || binding != continuity.Binding || binding.Validate() != nil {
		return Observation{}, ErrUncertified
	}
	observation, err := reader.Observe(ctx, binding, recordID)
	if err != nil {
		return Observation{}, err
	}
	if observation.Binding != binding || len(observation.Created) == 0 {
		return Observation{}, ErrUncertified
	}
	return observation, nil
}

// ReadCertification joins current W2 observation with APP-verified exact birth
// state. The issuer still rechecks its current binding and fences in its own
// transaction; this value cannot replace writer or physical-drain authority.
func (coordinator *Coordinator) ReadCertification(ctx context.Context, recordID string) (Certification, error) {
	observation, err := coordinator.ReadCertificationObservation(ctx, recordID)
	if err != nil {
		return Certification{}, err
	}
	allocation, err := coordinator.application.VerifyCertification(ctx, recordID, observation)
	if err != nil {
		return Certification{}, err
	}
	if allocation.Validate() != nil || allocation.State != StateConfirmed || allocation.Domain != recordidentity.Certifiable ||
		allocation.RecordID != recordID || !receiptMatchesAllocation(observation.Reserved, allocation, EventReserved) ||
		!receiptMatchesAllocation(observation.Prepared, allocation, EventPrepared) || !receiptMatchesAllocation(observation.Created, allocation, EventCreated) {
		return Certification{}, ErrUncertified
	}
	commitment, err := allocation.Creation.Commitment()
	if err != nil {
		return Certification{}, err
	}
	return Certification{
		Binding: observation.Binding, AllocationID: allocation.AllocationID, ReservationID: allocation.ReservationID,
		RecordID: recordID, InitialCommitment: commitment, CreatedReceipt: observation.Created,
	}, nil
}

func pending(err error) error {
	if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrPending, err)
}
