// Package activity projects authoritative record, evidence, asset, monitoring
// and command events into one rebuildable read model. It owns no authoritative
// state: every row here can be dropped and rebuilt from the sources, so any
// fact that exists only in this projection is a bug.
package activity

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

const (
	// CursorVersionV1 matches the search cursor version so both surfaces
	// reject a token from the other by version alone.
	CursorVersionV1 uint64 = 1

	// PresentationVersionV1 is the only registered presentation shape.
	PresentationVersionV1 uint64 = 1

	// MaxPresentationTitleRunes and MaxPresentationSummaryRunes bound what an
	// adapter may put on a timeline row. They are display budgets, not content
	// storage: the authoritative text stays in its owning table.
	MaxPresentationTitleRunes   = 256
	MaxPresentationSummaryRunes = 512

	// DefaultPageSize and MaxPageSize mirror recordsearch so a caller moving
	// between search and activity does not have to learn two limits.
	DefaultPageSize = 50
	MaxPageSize     = 100
)

var (
	ErrInvalidSourceIdentity = errors.New("invalid activity source identity")
	ErrInvalidEventKind      = errors.New("invalid activity event kind")
	ErrInvalidNamespace      = errors.New("invalid activity namespace")
	ErrInvalidPresentation   = errors.New("invalid activity presentation")
	ErrInvalidCursor         = errors.New("invalid activity cursor")
)

// EventKind is the projected event vocabulary. Every constant here is emitted
// by a real writer; adding one that nothing writes yields a silently empty
// timeline rather than a failure, so the set is verified against the writers.
type EventKind string

const (
	EventKindRecordCreated            EventKind = "record_created"
	EventKindRecordRevised            EventKind = "record_revised"
	EventKindRecordRestored           EventKind = "record_restored"
	EventKindRecordArchived           EventKind = "record_archived"
	EventKindRecordUnarchived         EventKind = "record_unarchived"
	EventKindRecordOwnerChanged       EventKind = "record_owner_changed"
	EventKindRecordParticipantChanged EventKind = "record_participant_changed"
	EventKindRecordFollowUpChanged    EventKind = "record_follow_up_changed"
	EventKindCommentCreated           EventKind = "comment_created"
	EventKindCommentEdited            EventKind = "comment_edited"
	EventKindCommentRedacted          EventKind = "comment_redacted"
	EventKindActionCreated            EventKind = "action_created"
	EventKindActionUpdated            EventKind = "action_updated"
	EventKindActionCompleted          EventKind = "action_completed"
	EventKindActionCancelled          EventKind = "action_cancelled"
	EventKindActionReopened           EventKind = "action_reopened"
	EventKindEvidenceCaptured         EventKind = "evidence_captured"
	EventKindAssetFactChanged         EventKind = "asset_fact_changed"
	EventKindMonitoringStateChanged   EventKind = "monitoring_state_changed"
	EventKindCommandExecuted          EventKind = "command_executed"
)

var eventKinds = map[EventKind]bool{
	EventKindRecordCreated:            true,
	EventKindRecordRevised:            true,
	EventKindRecordRestored:           true,
	EventKindRecordArchived:           true,
	EventKindRecordUnarchived:         true,
	EventKindRecordOwnerChanged:       true,
	EventKindRecordParticipantChanged: true,
	EventKindRecordFollowUpChanged:    true,
	EventKindCommentCreated:           true,
	EventKindCommentEdited:            true,
	EventKindCommentRedacted:          true,
	EventKindActionCreated:            true,
	EventKindActionUpdated:            true,
	EventKindActionCompleted:          true,
	EventKindActionCancelled:          true,
	EventKindActionReopened:           true,
	EventKindEvidenceCaptured:         true,
	EventKindAssetFactChanged:         true,
	EventKindMonitoringStateChanged:   true,
	EventKindCommandExecuted:          true,
}

func ValidEventKind(kind EventKind) bool {
	return eventKinds[kind]
}

// recordsViewEventKinds is the server-side predicate behind view=records. It
// holds the human-authored record events; comments and action items belong to
// the full activity view, not to "records".
var recordsViewEventKinds = []EventKind{
	EventKindRecordCreated,
	EventKindRecordRevised,
	EventKindRecordRestored,
	EventKindRecordArchived,
	EventKindRecordUnarchived,
	EventKindRecordOwnerChanged,
	EventKindRecordParticipantChanged,
	EventKindRecordFollowUpChanged,
}

func RecordsViewEventKinds() []EventKind {
	kinds := make([]EventKind, len(recordsViewEventKinds))
	copy(kinds, recordsViewEventKinds)
	return kinds
}

// SourceKind names an authoritative producer. It is part of the projection
// unique key, so renaming one is a data migration and not a refactor.
type SourceKind string

const (
	SourceKindRecordDomain     SourceKind = "record_domain"
	SourceKindEvidenceSnapshot SourceKind = "evidence_snapshot"
	SourceKindAssetHistory     SourceKind = "asset_history"
	SourceKindMonitoringEvent  SourceKind = "monitoring_event"
	SourceKindCommandAudit     SourceKind = "command_audit"
)

var sourceKinds = map[SourceKind]bool{
	SourceKindRecordDomain:     true,
	SourceKindEvidenceSnapshot: true,
	SourceKindAssetHistory:     true,
	SourceKindMonitoringEvent:  true,
	SourceKindCommandAudit:     true,
}

func ValidSourceKind(kind SourceKind) bool {
	return sourceKinds[kind]
}

// Namespace scopes identifiers to one deployment and project so two
// deployments projecting the same source event never mint the same activity id.
type Namespace struct {
	ProjectID string
}

// SourceIdentity is the stable coordinate of one authoritative event. The
// version distinguishes a correction from the fact it corrects.
type SourceIdentity struct {
	Kind    SourceKind
	EventID string
	Version uint64
}

var sourceEventIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,128}$`)

func ValidateSourceIdentity(source SourceIdentity) error {
	if !ValidSourceKind(source.Kind) {
		return ErrInvalidSourceIdentity
	}
	if !sourceEventIDPattern.MatchString(source.EventID) {
		return ErrInvalidSourceIdentity
	}
	if source.Version == 0 {
		return ErrInvalidSourceIdentity
	}
	return nil
}

var activityIDPattern = regexp.MustCompile(`^act_[a-z0-9]{1,64}$`)

func ValidActivityID(value string) bool {
	return activityIDPattern.MatchString(value)
}

var activityIDEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewActivityID derives the identifier from the namespace and the complete
// source key rather than minting a random one. That is what lets a retry, an
// incremental batch, and a rebuild from an empty projection agree on the same
// row: the identifier is a function of the fact, not of when we happened to see
// it. Components are length-prefixed so adjacent field values cannot hash
// alike.
func NewActivityID(namespace Namespace, source SourceIdentity, kind EventKind) (string, error) {
	if namespace.ProjectID == "" {
		return "", ErrInvalidNamespace
	}
	if err := ValidateSourceIdentity(source); err != nil {
		return "", err
	}
	if !ValidEventKind(kind) {
		return "", ErrInvalidEventKind
	}

	digest := sha256.New()
	digest.Write([]byte("houfeng.record-activity.id.v1"))
	for _, component := range []string{
		namespace.ProjectID,
		string(source.Kind),
		source.EventID,
		string(kind),
	} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(component)))
		digest.Write(length[:])
		digest.Write([]byte(component))
	}
	var version [8]byte
	binary.BigEndian.PutUint64(version[:], source.Version)
	digest.Write(version[:])

	sum := digest.Sum(nil)
	return "act_" + strings.ToLower(activityIDEncoding.EncodeToString(sum[:16])), nil
}

// Presentation is the registered display shape for a timeline row. It refuses
// arbitrary payloads on purpose: an adapter that could pass a map through would
// be able to carry command output or a Markdown body onto a system fact.
type Presentation struct {
	Version uint64 `json:"version"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

func ValidatePresentation(presentation Presentation) error {
	if presentation.Version != PresentationVersionV1 {
		return ErrInvalidPresentation
	}
	title := strings.TrimSpace(presentation.Title)
	if title == "" || utf8.RuneCountInString(title) > MaxPresentationTitleRunes {
		return ErrInvalidPresentation
	}
	if utf8.RuneCountInString(presentation.Summary) > MaxPresentationSummaryRunes {
		return ErrInvalidPresentation
	}
	return nil
}

// ActorSnapshot is the display identity of whoever caused the event. System
// facts have no actor, so it is always addressed through a pointer.
type ActorSnapshot struct {
	ActorID     string `json:"actor_id"`
	DisplayName string `json:"display_name,omitempty"`
}

// SubjectSnapshot is the identity a subject had when the event happened.
// Keeping the snapshot is what lets a deleted subject stay readable without
// reconnecting anything by name.
type SubjectSnapshot struct {
	Kind       records.SubjectKind  `json:"kind"`
	SourceID   string               `json:"source_id"`
	Role       records.RelationRole `json:"role"`
	Primary    bool                 `json:"primary"`
	Identity   map[string]string    `json:"identity"`
	LiveRoute  string               `json:"live_route,omitempty"`
	Tombstoned bool                 `json:"tombstoned"`
}

// Event is the canonical projected row. IngestSequence and AuthScope are
// server-side controls: both carry `json:"-"` so no handler can accidentally
// serialize the global watermark into a response and tell one authorization
// scope that activity it cannot see is advancing.
type Event struct {
	ActivityID   string            `json:"activity_id"`
	EventKind    EventKind         `json:"event_kind"`
	EventAt      time.Time         `json:"event_at"`
	RecordedAt   time.Time         `json:"recorded_at"`
	Source       SourceIdentity    `json:"-"`
	SourceKind   SourceKind        `json:"source_kind"`
	Backfilled   bool              `json:"backfilled"`
	Actor        *ActorSnapshot    `json:"actor,omitempty"`
	Subjects     []SubjectSnapshot `json:"subjects"`
	Presentation Presentation      `json:"presentation"`
	Corrects     string            `json:"corrects_activity_id,omitempty"`

	IngestSequence uint64                   `json:"-"`
	AuthScope      recordauth.ResourceScope `json:"-"`
}

// MarshalJSON keeps every collection an array. A nil slice marshals to null,
// and a null items list forces every caller to special-case the empty timeline.
func (event Event) MarshalJSON() ([]byte, error) {
	type wire Event
	clone := wire(event)
	if clone.Subjects == nil {
		clone.Subjects = []SubjectSnapshot{}
	}
	if clone.SourceKind == "" {
		clone.SourceKind = event.Source.Kind
	}
	return marshalWire(clone)
}
