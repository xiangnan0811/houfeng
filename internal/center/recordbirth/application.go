package recordbirth

import (
	"context"

	"houfeng/internal/center/recordidentity"
)

type State string

const (
	StatePending    State = "pending"
	StateAllocated  State = "allocated"
	StatePreparing  State = "preparing"
	StatePrepared   State = "prepared"
	StateConsumed   State = "consumed"
	StateFinished   State = "finished"
	StateConfirmed  State = "confirmed"
	StateTombstoned State = "tombstoned"
)

// Allocation is the durable APP request and its monotonic birth projection.
// Pending certified requests already have a domain and may never fall back.
type Allocation struct {
	AllocationID   string
	Domain         recordidentity.Domain
	Request        Request
	Binding        Binding
	ReservationID  string
	RecordID       string
	State          State
	Creation       InitialCreation
	ReserveReceipt []byte
	PrepareReceipt []byte
	CreatedReceipt []byte
	// ContinuityProof is an ephemeral observation for the next admitted writer.
	// It is never part of the durable allocation projection.
	ContinuityProof []byte `json:"-"`
}

func (allocation Allocation) Validate() error {
	if !hexIdentity(allocation.AllocationID, "rqa_", 32) || allocation.Request.Validate() != nil {
		return ErrInvalid
	}
	switch allocation.Domain {
	case recordidentity.Ordinary:
		if allocation.Binding != (Binding{}) || len(allocation.ReserveReceipt) != 0 ||
			len(allocation.PrepareReceipt) != 0 || len(allocation.CreatedReceipt) != 0 {
			return ErrInvalid
		}
	case recordidentity.Certifiable:
		if allocation.Binding.Validate() != nil || allocation.Binding.ProjectID != allocation.Request.ProjectID {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if allocation.State == StatePending {
		if allocation.Domain != recordidentity.Certifiable || allocation.ReservationID != "" || allocation.RecordID != "" ||
			allocation.Creation != (InitialCreation{}) || len(allocation.ReserveReceipt) != 0 ||
			len(allocation.PrepareReceipt) != 0 || len(allocation.CreatedReceipt) != 0 {
			return ErrInvalid
		}
		return nil
	}
	if !hexIdentity(allocation.ReservationID, "rba_", 32) || recordidentity.Classify(allocation.RecordID) != allocation.Domain {
		return ErrInvalid
	}
	if allocation.Domain == recordidentity.Certifiable {
		if !receiptMatchesAllocation(allocation.ReserveReceipt, allocation, EventReserved) {
			return ErrInvalid
		}
	}
	switch allocation.State {
	case StateAllocated:
		if allocation.Creation != (InitialCreation{}) || len(allocation.PrepareReceipt) != 0 || len(allocation.CreatedReceipt) != 0 {
			return ErrInvalid
		}
	case StatePreparing, StatePrepared, StateConsumed, StateFinished, StateConfirmed, StateTombstoned:
		if allocation.Creation.Validate() != nil || allocation.Creation.ReservationID != allocation.ReservationID ||
			allocation.Creation.RecordID != allocation.RecordID || allocation.Creation.AuthorID != allocation.Request.ActorID {
			return ErrInvalid
		}
		if allocation.State == StatePreparing {
			if allocation.Domain != recordidentity.Certifiable || len(allocation.PrepareReceipt) != 0 || len(allocation.CreatedReceipt) != 0 {
				return ErrInvalid
			}
		} else if allocation.Domain == recordidentity.Certifiable && !receiptMatchesAllocation(allocation.PrepareReceipt, allocation, EventPrepared) {
			return ErrInvalid
		}
		if allocation.State == StateConfirmed && (allocation.Domain != recordidentity.Certifiable || !receiptMatchesAllocation(allocation.CreatedReceipt, allocation, EventCreated)) {
			return ErrInvalid
		}
		if len(allocation.CreatedReceipt) != 0 && !receiptMatchesAllocation(allocation.CreatedReceipt, allocation, EventCreated) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Application implements fixed APP commands under current admission. Lookup
// alone does not prove request absence: the coordinator establishes continuity
// before it chooses a domain or carries out any pending effect.
type Application interface {
	FindAllocation(context.Context, Request) (Allocation, bool, error)
	CurrentBinding(context.Context) (Binding, bool, error)
	ContinuityBinding(context.Context) (Binding, bool, error)
	CheckpointProof(context.Context, Request, CheckpointHead) ([]byte, error)
	BeginAllocation(context.Context, Request, *Binding, []byte) (Allocation, error)
	RegisterReceipt(context.Context, Request, []byte, []byte) (Allocation, error)
	PrepareAllocation(context.Context, Request, InitialCreation, []byte) (Allocation, error)
	CommittedCreation(context.Context, string) (Allocation, error)
	VerifyCertification(context.Context, string, Observation) (Allocation, error)
}

// ContinuitySource obtains a fresh observation outside an APP writer transaction.
// A nil proof denotes an explicitly verified never-installed lineage.
type ContinuitySource interface {
	ReadContinuity(context.Context) ([]byte, error)
}

// Permit is carried into the existing ranked writer transaction. The APP
// consume command verifies it against durable state and current admission;
// possession of this Go value does not authorize a root insert.
type Permit struct {
	AllocationID    string
	ReservationID   string
	RecordID        string
	Creation        InitialCreation
	PrepareReceipt  []byte
	ContinuityProof []byte `json:"-"`
}

func receiptMatchesAllocation(raw []byte, allocation Allocation, kind EventKind) bool {
	event, err := DecodeReceipt(raw)
	if err != nil || event.Kind != kind || event.Binding.AuthorityID != allocation.Binding.AuthorityID ||
		event.Binding.LineageID != allocation.Binding.LineageID || event.Binding.ProjectID != allocation.Binding.ProjectID ||
		event.Request != allocation.Request || event.ReservationID != allocation.ReservationID || event.RecordID != allocation.RecordID {
		return false
	}
	if kind != EventReserved {
		commitment, err := allocation.Creation.Commitment()
		return err == nil && commitment == event.InitialCommitment
	}
	return true
}
