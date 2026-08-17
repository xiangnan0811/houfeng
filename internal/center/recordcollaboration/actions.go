package recordcollaboration

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
)

const (
	MaxActionTitleRunes   = 512
	MaxActionDetailsRunes = 4096
	// MaxActionVersion is the largest action version representable by the
	// signed PostgreSQL bigint persistence contract.
	MaxActionVersion uint64 = math.MaxInt64
)

var (
	ErrInvalidActionFields = errors.New("invalid record action fields")
)

// ActionRecord is the bounded current-state read model used by the Web
// collaboration surface. It deliberately omits action details and immutable
// event history; downstream activity consumers use typed activity facts.
type ActionRecord struct {
	ActionID          string
	RecordID          string
	Version           uint64
	Status            ActionStatus
	Title             string
	AssigneeID        string
	DueAt             *time.Time
	CompletedAt       *time.Time
	SubjectRevisionID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (record ActionRecord) Validate() error {
	if ValidateActionID(record.ActionID) != nil || !validRecordID(record.RecordID) ||
		record.Version == 0 || record.Version > MaxActionVersion || !validActionStatus(record.Status) ||
		!validActionText(record.Title, MaxActionTitleRunes, false) ||
		(record.AssigneeID != "" && recordauth.ValidateActorUserID(record.AssigneeID) != nil) ||
		(record.SubjectRevisionID != "" && !validCollaborationRevisionIdentity(record.SubjectRevisionID)) ||
		record.CreatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) ||
		(record.Status == ActionStatusCompleted) != (record.CompletedAt != nil) {
		return ErrInvalidActionFields
	}
	if record.DueAt != nil && normalizeActionTime(record.DueAt) == nil {
		return ErrInvalidActionFields
	}
	if record.CompletedAt != nil && (record.CompletedAt.Before(record.CreatedAt) || normalizeActionTime(record.CompletedAt) == nil) {
		return ErrInvalidActionFields
	}
	return nil
}

func (record ActionRecord) Clone() ActionRecord {
	cloned := record
	cloned.DueAt = normalizeActionTime(record.DueAt)
	cloned.CompletedAt = normalizeActionTime(record.CompletedAt)
	return cloned
}

// ActionActivityKind is the closed typed fact registry consumed by the later
// Activity child. The action service only appends facts to the existing Core
// activity relation; it creates no projection or downstream job.
type ActionActivityKind string

const (
	ActionActivityCreated   ActionActivityKind = "action_created"
	ActionActivityUpdated   ActionActivityKind = "action_updated"
	ActionActivityCompleted ActionActivityKind = "action_completed"
	ActionActivityCancelled ActionActivityKind = "action_cancelled"
	ActionActivityReopened  ActionActivityKind = "action_reopened"
)

func ActivityKindForActionMutation(kind ActionMutationKind) (ActionActivityKind, error) {
	switch kind {
	case ActionMutationCreate:
		return ActionActivityCreated, nil
	case ActionMutationUpdate:
		return ActionActivityUpdated, nil
	case ActionMutationComplete:
		return ActionActivityCompleted, nil
	case ActionMutationCancel:
		return ActionActivityCancelled, nil
	case ActionMutationReopen:
		return ActionActivityReopened, nil
	default:
		return "", ErrInvalidActionStateTransition
	}
}

// ActionMutationKind is the closed command and event registry for actions.
type ActionMutationKind string

const (
	ActionMutationCreate   ActionMutationKind = "created"
	ActionMutationUpdate   ActionMutationKind = "updated"
	ActionMutationComplete ActionMutationKind = "completed"
	ActionMutationCancel   ActionMutationKind = "cancelled"
	ActionMutationReopen   ActionMutationKind = "reopened"
)

type ActionFieldValues struct {
	Title             string
	Details           string
	AssigneeID        string
	DueAt             *time.Time
	SubjectRevisionID string
}

// ActionFields is an immutable, normalized action-content value. It is never
// used as an idempotency result fingerprint or outbox payload.
type ActionFields struct {
	title             string
	details           string
	assigneeID        string
	dueAt             *time.Time
	subjectRevisionID string
}

func NormalizeActionFields(values ActionFieldValues) (ActionFields, error) {
	title := strings.TrimSpace(values.Title)
	if !validActionText(title, MaxActionTitleRunes, false) ||
		!validActionText(values.Details, MaxActionDetailsRunes, true) {
		return ActionFields{}, ErrInvalidActionFields
	}
	if values.AssigneeID != "" && recordauth.ValidateActorUserID(values.AssigneeID) != nil {
		return ActionFields{}, fmt.Errorf("%w: assignee", ErrInvalidActionFields)
	}
	if values.SubjectRevisionID != "" && !validCollaborationRevisionIdentity(values.SubjectRevisionID) {
		return ActionFields{}, fmt.Errorf("%w: subject revision", ErrInvalidActionFields)
	}
	dueAt := normalizeActionTime(values.DueAt)
	if values.DueAt != nil && dueAt == nil {
		return ActionFields{}, fmt.Errorf("%w: due time", ErrInvalidActionFields)
	}
	return ActionFields{
		title: title, details: values.Details, assigneeID: values.AssigneeID,
		dueAt: dueAt, subjectRevisionID: values.SubjectRevisionID,
	}, nil
}

func (fields ActionFields) Title() string             { return fields.title }
func (fields ActionFields) Details() string           { return fields.details }
func (fields ActionFields) AssigneeID() string        { return fields.assigneeID }
func (fields ActionFields) DueAt() *time.Time         { return normalizeActionTime(fields.dueAt) }
func (fields ActionFields) SubjectRevisionID() string { return fields.subjectRevisionID }

func validActionText(value string, maximum int, multiline bool) bool {
	if value == "" && !multiline {
		return false
	}
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) && !(multiline && (character == '\n' || character == '\t')) {
			return false
		}
	}
	return true
}

func validCollaborationRevisionIdentity(value string) bool {
	const prefix = "rrv_"
	if len(value) <= len(prefix) || len(value) > len(prefix)+64 || !strings.HasPrefix(value, prefix) {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func normalizeActionTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}

type ActionFilterFacts struct {
	status     ActionStatus
	assigneeID string
	dueAt      *time.Time
}

func NewActionFilterFacts(status ActionStatus, assigneeID string, dueAt *time.Time) (ActionFilterFacts, error) {
	if !validActionStatus(status) || (assigneeID != "" && recordauth.ValidateActorUserID(assigneeID) != nil) {
		return ActionFilterFacts{}, ErrInvalidActionFields
	}
	normalizedDue := normalizeActionTime(dueAt)
	if dueAt != nil && normalizedDue == nil {
		return ActionFilterFacts{}, ErrInvalidActionFields
	}
	return ActionFilterFacts{status: status, assigneeID: assigneeID, dueAt: normalizedDue}, nil
}

func (facts ActionFilterFacts) Status() ActionStatus { return facts.status }
func (facts ActionFilterFacts) AssigneeID() string   { return facts.assigneeID }
func (facts ActionFilterFacts) DueAt() *time.Time    { return normalizeActionTime(facts.dueAt) }

func ValidateActionMutationTransition(kind ActionMutationKind, from, to ActionStatus) error {
	if (kind == ActionMutationComplete && from == ActionStatusOpen && to == ActionStatusCompleted) ||
		(kind == ActionMutationCancel && from == ActionStatusOpen && to == ActionStatusCancelled) ||
		(kind == ActionMutationReopen &&
			(from == ActionStatusCompleted || from == ActionStatusCancelled) && to == ActionStatusOpen) {
		return nil
	}
	return fmt.Errorf("%w: %q from %q to %q", ErrInvalidActionStateTransition, kind, from, to)
}

func validActionStatus(status ActionStatus) bool {
	switch status {
	case ActionStatusOpen, ActionStatusCompleted, ActionStatusCancelled:
		return true
	default:
		return false
	}
}

// IsIncrementableActionVersion accepts only positive persisted versions that
// can advance once without crossing the signed bigint boundary.
func IsIncrementableActionVersion(version uint64) bool {
	return version > 0 && version < MaxActionVersion
}
