package recordclientcleanup

import (
	"context"
	"errors"
	"regexp"
	"strconv"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordidentity"
)

var (
	ErrInvalidRequest         = errors.New("invalid record client cleanup request")
	ErrReconcileRequired      = errors.New("record client cleanup reconciliation required")
	ErrInvalidAcknowledgement = errors.New("invalid record client cleanup acknowledgement")
	browserPattern            = regexp.MustCompile(`^[a-z0-9_-]{1,128}$`)
	allocationPattern         = regexp.MustCompile(`^rqa_[0-9a-f]{32}$`)
	operationPattern          = regexp.MustCompile(`^rpo_[a-z0-9]{1,64}$`)
)

const MaxReconcileRecords = 100

type Event struct {
	Version      int      `json:"version"`
	Cursor       string   `json:"cursor"`
	OperationID  string   `json:"operation_id"`
	RecordID     string   `json:"record_id"`
	CleanupEpoch string   `json:"cleanup_epoch"`
	Scopes       []string `json:"scopes"`
}

type Acknowledgement struct {
	Cursor       string `json:"cursor"`
	OperationID  string `json:"operation_id"`
	RecordID     string `json:"record_id"`
	CleanupEpoch string `json:"cleanup_epoch"`
}

func (event Event) Acknowledgement() Acknowledgement {
	return Acknowledgement{Cursor: event.Cursor, OperationID: event.OperationID, RecordID: event.RecordID, CleanupEpoch: event.CleanupEpoch}
}

func PositiveBigint(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, ErrInvalidRequest
	}
	return parsed, nil
}

func ValidateBrowserID(browserID string) error {
	if !browserPattern.MatchString(browserID) {
		return ErrInvalidRequest
	}
	return nil
}

func (ack Acknowledgement) Validate() error {
	if _, err := PositiveBigint(ack.Cursor); err != nil {
		return err
	}
	if _, err := PositiveBigint(ack.CleanupEpoch); err != nil {
		return err
	}
	if !operationPattern.MatchString(ack.OperationID) || !recordidentity.IsValid(ack.RecordID) {
		return ErrInvalidRequest
	}
	return nil
}

func (event Event) Validate() error {
	if event.Version != 1 || len(event.Scopes) < 1 || len(event.Scopes) > 4 {
		return ErrInvalidRequest
	}
	if err := event.Acknowledgement().Validate(); err != nil {
		return err
	}
	var seen uint8
	for _, scope := range event.Scopes {
		var bit uint8
		switch scope {
		case "markdown_draft_buffer":
			bit = 1
		case "markdown_workspace":
			bit = 2
		case "comparison_workbench":
			bit = 4
		case "comparison_save_attempt":
			bit = 8
		default:
			return ErrInvalidRequest
		}
		if seen&bit != 0 {
			return ErrInvalidRequest
		}
		seen |= bit
	}
	return nil
}

// AllocationReference identifies retry metadata for a prospective record; it
// does not authorize reads or writes of that record.
type AllocationReference struct {
	RecordID     string `json:"record_id"`
	AllocationID string `json:"allocation_id"`
}

type ReconcileRequest struct {
	BrowserID            string                `json:"browser_id"`
	RecordIDs            []string              `json:"record_ids"`
	AllocationReferences []AllocationReference `json:"allocation_references,omitempty"`
}

func (request ReconcileRequest) Validate() error {
	if err := ValidateBrowserID(request.BrowserID); err != nil {
		return err
	}
	if len(request.RecordIDs)+len(request.AllocationReferences) > MaxReconcileRecords {
		return ErrInvalidRequest
	}
	ordinary := make(map[string]struct{}, len(request.RecordIDs))
	for _, id := range request.RecordIDs {
		if !recordidentity.IsValid(id) {
			return ErrInvalidRequest
		}
		ordinary[id] = struct{}{}
	}
	byRecord := make(map[string]string, len(request.AllocationReferences))
	byAllocation := make(map[string]string, len(request.AllocationReferences))
	for _, reference := range request.AllocationReferences {
		domain := recordidentity.Classify(reference.RecordID)
		if (domain != recordidentity.Ordinary && domain != recordidentity.Certifiable) ||
			!allocationPattern.MatchString(reference.AllocationID) {
			return ErrInvalidRequest
		}
		if _, exists := ordinary[reference.RecordID]; exists {
			return ErrInvalidRequest
		}
		if allocation, exists := byRecord[reference.RecordID]; exists && allocation != reference.AllocationID {
			return ErrInvalidRequest
		}
		if record, exists := byAllocation[reference.AllocationID]; exists && record != reference.RecordID {
			return ErrInvalidRequest
		}
		byRecord[reference.RecordID] = reference.AllocationID
		byAllocation[reference.AllocationID] = reference.RecordID
	}
	return nil
}

type ReconcileResult struct {
	Allowed    []string `json:"allowed"`
	Pending    []string `json:"pending"`
	Invalidate []string `json:"invalidate"`
}

// Implementations retain targets after root deletion. Next returns the oldest
// unacknowledged targeted event, never skipping it merely because a client
// reports a later cursor (the previous acknowledgement may have been lost).
// ValidateCursor accepts only an actual targeted event, not a numeric gap.
// Every operation is protected by the current deployment admission gate.
type Repository interface {
	Reconcile(context.Context, recordauth.ActorScope, ReconcileRequest) (ReconcileResult, error)
	ValidateCursor(context.Context, recordauth.ActorScope, string, string) error
	Next(context.Context, recordauth.ActorScope, string) (*Event, error)
	Acknowledge(context.Context, recordauth.ActorScope, string, Acknowledgement) error
}
