package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
)

var (
	ErrInvalidActivityFact        = errors.New("invalid collaboration activity fact")
	ErrInvalidActivityProvider    = errors.New("invalid collaboration activity provider")
	ErrInvalidPortabilitySnapshot = errors.New("invalid collaboration portability snapshot")
	ErrInvalidPortabilityAdapter  = errors.New("invalid collaboration portability adapter")
)

const (
	CollaborationActivityContractVersionV1    uint64 = 1
	CollaborationPortabilityContractVersionV1 uint64 = 1
	MaxCollaborationActivityFacts                    = 4096
	MaxCollaborationActivityFactBytes                = 4 * 1024 * 1024
	MaxCollaborationPortabilityRowsPerSurface        = 4096
	MaxCollaborationPortabilityBytes                 = 32 * 1024 * 1024
)

type ActivityFactKind string

const (
	ActivityFactRecordOwnerChanged       ActivityFactKind = "record_owner_changed"
	ActivityFactRecordParticipantChanged ActivityFactKind = "record_participant_changed"
	ActivityFactRecordFollowUpChanged    ActivityFactKind = "record_follow_up_changed"
	ActivityFactActionCreated            ActivityFactKind = "action_created"
	ActivityFactActionUpdated            ActivityFactKind = "action_updated"
	ActivityFactActionCompleted          ActivityFactKind = "action_completed"
	ActivityFactActionCancelled          ActivityFactKind = "action_cancelled"
	ActivityFactActionReopened           ActivityFactKind = "action_reopened"
	ActivityFactCommentCreated           ActivityFactKind = "comment_created"
	ActivityFactCommentEdited            ActivityFactKind = "comment_edited"
	ActivityFactCommentRedacted          ActivityFactKind = "comment_redacted"
)

type ActivityFact struct {
	ActivityID         string
	RecordID           string
	RevisionID         string
	Kind               ActivityFactKind
	SourceEventID      string
	SourceVersion      uint64
	ActorID            string
	AuthorizationEpoch uint64
	RecordLockVersion  uint64
	EventAt            time.Time
}

func (fact ActivityFact) Validate() error {
	if !validPortableID(fact.ActivityID, "rac_") || !validRecordID(fact.RecordID) ||
		(fact.RevisionID != "" && !validCollaborationRevisionIdentity(fact.RevisionID)) ||
		!validActivityFactKind(fact.Kind) || !validPortableText(fact.SourceEventID, 256, false) ||
		fact.SourceVersion == 0 || fact.SourceVersion > math.MaxInt64 ||
		recordauth.ValidateActorUserID(fact.ActorID) != nil || fact.AuthorizationEpoch == 0 || fact.AuthorizationEpoch > math.MaxInt64 ||
		fact.RecordLockVersion == 0 || fact.RecordLockVersion > math.MaxInt64 ||
		!portableTimeCanonical(fact.EventAt) {
		return ErrInvalidActivityFact
	}
	if !validActivityFactSource(fact) {
		return ErrInvalidActivityFact
	}
	return nil
}

type ActivityFactSource interface {
	ReadCollaborationActivityFacts(context.Context, pgx.Tx, RecordFenceBinding) ([]ActivityFact, error)
}

type ActivityProvider struct {
	source ActivityFactSource
}

func NewActivityProvider(source ActivityFactSource) (*ActivityProvider, error) {
	if nilActionDependency(source) {
		return nil, ErrInvalidActivityProvider
	}
	return &ActivityProvider{source: source}, nil
}

func (provider *ActivityProvider) ContractVersion() uint64 {
	return CollaborationActivityContractVersionV1
}

func (provider *ActivityProvider) ListFacts(
	ctx context.Context,
	tx pgx.Tx,
	binding RecordFenceBinding,
) ([]ActivityFact, error) {
	if ctx == nil || provider == nil || nilActionDependency(provider.source) ||
		nilActionDependency(tx) || binding.Validate() != nil {
		return nil, ErrInvalidActivityProvider
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	facts, err := provider.source.ReadCollaborationActivityFacts(ctx, tx, binding)
	if err != nil {
		return nil, err
	}
	if facts == nil || len(facts) > MaxCollaborationActivityFacts {
		return nil, ErrInvalidActivityFact
	}
	bytes := 0
	for index, fact := range facts {
		if fact.Validate() != nil || fact.RecordID != binding.RecordID() ||
			(index > 0 && compareActivityFacts(facts[index-1], fact) >= 0) {
			return nil, ErrInvalidActivityFact
		}
		bytes += len(fact.ActivityID) + len(fact.RecordID) + len(fact.RevisionID) + len(fact.Kind) + len(fact.SourceEventID) + len(fact.ActorID) + 64
		if bytes > MaxCollaborationActivityFactBytes {
			return nil, ErrInvalidActivityFact
		}
	}
	return append([]ActivityFact(nil), facts...), nil
}

type PortableAction struct {
	ActionID          string
	SubjectRevisionID string
	Version           uint64
	Title             string
	Details           string
	Status            ActionStatus
	AssigneeID        string
	DueAt             *time.Time
	CompletedAt       *time.Time
	CreatedBy         string
	UpdatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type PortableActionEvent struct {
	EventID        string
	ActionID       string
	Version        uint64
	Kind           ActionMutationKind
	PreviousStatus *ActionStatus
	CurrentStatus  ActionStatus
	ActorID        string
	AssigneeID     string
	OccurredAt     time.Time
	CreatedAt      time.Time
}

type PortableComment struct {
	CommentID    string
	AuthorID     string
	Version      uint64
	State        CommentState
	BodyMarkdown string
	RenderModel  CommentRenderModel
	BodyDigest   [sha256.Size]byte
	TombstoneID  string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	RedactedAt   *time.Time
}

type PortableCommentRevision struct {
	RevisionID   string
	CommentID    string
	Version      uint64
	EditedBy     string
	BodyMarkdown string
	RenderModel  CommentRenderModel
	BodyDigest   [sha256.Size]byte
	TombstoneID  string
	CreatedAt    time.Time
	RedactedAt   *time.Time
}

type PortableTombstoneReason string

const (
	PortableTombstoneAuthorDeleted    PortableTombstoneReason = "author_deleted"
	PortableTombstoneModeratorDeleted PortableTombstoneReason = "moderator_deleted"
	PortableTombstoneRecordDeleted    PortableTombstoneReason = "record_deleted"
)

type PortableCommentTombstone struct {
	TombstoneID string
	CommentID   string
	Version     uint64
	DeletedBy   string
	ReasonCode  PortableTombstoneReason
	DeletedAt   time.Time
}

type PortableCommentReply struct {
	ChildCommentID  string
	ParentCommentID string
	CreatedAt       time.Time
}

type PortableCommentMention struct {
	CommentID      string
	CommentVersion uint64
	MentionedUser  string
	CreatedAt      time.Time
}

type PortableFollower struct {
	UserID     string
	Version    uint64
	Preference FollowerPreference
	Sources    FollowerSources
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PortableNotificationAudit is deliberately content-free. It preserves only
// bounded delivery counts; binding identifiers, credentials, provider
// responses, subject content, and recipient identities never enter a backup.
type PortableNotificationAudit struct {
	NotificationID  string
	Kind            NotificationEventKind
	SubjectKind     NotificationSubjectKind
	SourceVersion   uint64
	EventAt         time.Time
	RecipientCount  uint64
	DeliveryCount   uint64
	SentCount       uint64
	UnknownCount    uint64
	PermanentFailed uint64
}

type PortabilitySnapshot struct {
	Actions            []PortableAction
	ActionEvents       []PortableActionEvent
	Comments           []PortableComment
	CommentRevisions   []PortableCommentRevision
	Tombstones         []PortableCommentTombstone
	Replies            []PortableCommentReply
	Mentions           []PortableCommentMention
	Followers          []PortableFollower
	NotificationAudits []PortableNotificationAudit
}

func (snapshot PortabilitySnapshot) Validate() error {
	if snapshot.Actions == nil || snapshot.ActionEvents == nil || snapshot.Comments == nil ||
		snapshot.CommentRevisions == nil || snapshot.Tombstones == nil || snapshot.Replies == nil ||
		snapshot.Mentions == nil || snapshot.Followers == nil || snapshot.NotificationAudits == nil {
		return ErrInvalidPortabilitySnapshot
	}
	for _, rows := range []int{
		len(snapshot.Actions), len(snapshot.ActionEvents), len(snapshot.Comments), len(snapshot.CommentRevisions),
		len(snapshot.Tombstones), len(snapshot.Replies), len(snapshot.Mentions), len(snapshot.Followers), len(snapshot.NotificationAudits),
	} {
		if rows > MaxCollaborationPortabilityRowsPerSurface {
			return ErrInvalidPortabilitySnapshot
		}
	}
	for index, action := range snapshot.Actions {
		if validatePortableAction(action) != nil || (index > 0 && snapshot.Actions[index-1].ActionID >= action.ActionID) {
			return ErrInvalidPortabilitySnapshot
		}
	}
	for index, event := range snapshot.ActionEvents {
		if validatePortableActionEvent(event) != nil || (index > 0 && snapshot.ActionEvents[index-1].EventID >= event.EventID) {
			return ErrInvalidPortabilitySnapshot
		}
	}
	tombstones := make(map[string]PortableCommentTombstone, len(snapshot.Tombstones))
	for index, tombstone := range snapshot.Tombstones {
		if validatePortableCommentTombstone(tombstone) != nil ||
			(index > 0 && snapshot.Tombstones[index-1].TombstoneID >= tombstone.TombstoneID) {
			return ErrInvalidPortabilitySnapshot
		}
		tombstones[tombstone.TombstoneID] = tombstone
	}
	for index, comment := range snapshot.Comments {
		if validatePortableComment(comment) != nil ||
			(index > 0 && snapshot.Comments[index-1].CommentID >= comment.CommentID) {
			return ErrInvalidPortabilitySnapshot
		}
		if comment.State == CommentStateRedacted {
			tombstone, exists := tombstones[comment.TombstoneID]
			if !exists || tombstone.CommentID != comment.CommentID || tombstone.Version != comment.Version ||
				comment.RedactedAt == nil || !tombstone.DeletedAt.Equal(*comment.RedactedAt) {
				return ErrInvalidPortabilitySnapshot
			}
		}
	}
	for index, revision := range snapshot.CommentRevisions {
		if validatePortableCommentRevision(revision) != nil ||
			(index > 0 && snapshot.CommentRevisions[index-1].RevisionID >= revision.RevisionID) {
			return ErrInvalidPortabilitySnapshot
		}
		if revision.RedactedAt != nil {
			tombstone, exists := tombstones[revision.TombstoneID]
			if !exists || tombstone.CommentID != revision.CommentID || !tombstone.DeletedAt.Equal(*revision.RedactedAt) {
				return ErrInvalidPortabilitySnapshot
			}
		}
	}
	for index, reply := range snapshot.Replies {
		if ValidateCommentID(reply.ChildCommentID) != nil || ValidateCommentID(reply.ParentCommentID) != nil ||
			reply.ChildCommentID == reply.ParentCommentID || !portableTimeCanonical(reply.CreatedAt) ||
			(index > 0 && snapshot.Replies[index-1].ChildCommentID >= reply.ChildCommentID) {
			return ErrInvalidPortabilitySnapshot
		}
	}
	for index, mention := range snapshot.Mentions {
		if ValidateCommentID(mention.CommentID) != nil || mention.CommentVersion == 0 || mention.CommentVersion > math.MaxInt64 ||
			recordauth.ValidateActorUserID(mention.MentionedUser) != nil || !portableTimeCanonical(mention.CreatedAt) ||
			(index > 0 && comparePortableMentions(snapshot.Mentions[index-1], mention) >= 0) {
			return ErrInvalidPortabilitySnapshot
		}
	}
	for index, follower := range snapshot.Followers {
		if validatePortableFollower(follower) != nil ||
			(index > 0 && snapshot.Followers[index-1].UserID >= follower.UserID) {
			return ErrInvalidPortabilitySnapshot
		}
	}
	for index, audit := range snapshot.NotificationAudits {
		if validatePortableNotificationAudit(audit) != nil ||
			(index > 0 && snapshot.NotificationAudits[index-1].NotificationID >= audit.NotificationID) {
			return ErrInvalidPortabilitySnapshot
		}
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || len(encoded) > MaxCollaborationPortabilityBytes {
		return ErrInvalidPortabilitySnapshot
	}
	return nil
}

func (snapshot PortabilitySnapshot) Clone() PortabilitySnapshot {
	cloned := PortabilitySnapshot{
		Actions:            make([]PortableAction, len(snapshot.Actions)),
		ActionEvents:       make([]PortableActionEvent, len(snapshot.ActionEvents)),
		Comments:           make([]PortableComment, len(snapshot.Comments)),
		CommentRevisions:   make([]PortableCommentRevision, len(snapshot.CommentRevisions)),
		Tombstones:         append(make([]PortableCommentTombstone, 0, len(snapshot.Tombstones)), snapshot.Tombstones...),
		Replies:            append(make([]PortableCommentReply, 0, len(snapshot.Replies)), snapshot.Replies...),
		Mentions:           append(make([]PortableCommentMention, 0, len(snapshot.Mentions)), snapshot.Mentions...),
		Followers:          append(make([]PortableFollower, 0, len(snapshot.Followers)), snapshot.Followers...),
		NotificationAudits: append(make([]PortableNotificationAudit, 0, len(snapshot.NotificationAudits)), snapshot.NotificationAudits...),
	}
	for index, action := range snapshot.Actions {
		cloned.Actions[index] = action
		cloned.Actions[index].DueAt = clonePortableTime(action.DueAt)
		cloned.Actions[index].CompletedAt = clonePortableTime(action.CompletedAt)
	}
	for index, event := range snapshot.ActionEvents {
		cloned.ActionEvents[index] = event
		if event.PreviousStatus != nil {
			value := *event.PreviousStatus
			cloned.ActionEvents[index].PreviousStatus = &value
		}
	}
	for index, comment := range snapshot.Comments {
		cloned.Comments[index] = comment
		cloned.Comments[index].RenderModel = comment.RenderModel.Clone()
		cloned.Comments[index].RedactedAt = clonePortableTime(comment.RedactedAt)
	}
	for index, revision := range snapshot.CommentRevisions {
		cloned.CommentRevisions[index] = revision
		cloned.CommentRevisions[index].RenderModel = revision.RenderModel.Clone()
		cloned.CommentRevisions[index].RedactedAt = clonePortableTime(revision.RedactedAt)
	}
	return cloned
}

type PortabilityStore interface {
	BackupCollaboration(context.Context, pgx.Tx, RecordFenceBinding) (PortabilitySnapshot, error)
	RestoreCollaboration(context.Context, pgx.Tx, RecordFenceBinding, PortabilitySnapshot) error
}

type PortabilityAdapter struct {
	store PortabilityStore
}

func NewPortabilityAdapter(store PortabilityStore) (*PortabilityAdapter, error) {
	if nilActionDependency(store) {
		return nil, ErrInvalidPortabilityAdapter
	}
	return &PortabilityAdapter{store: store}, nil
}

func (adapter *PortabilityAdapter) ContractVersion() uint64 {
	return CollaborationPortabilityContractVersionV1
}

func (adapter *PortabilityAdapter) Backup(
	ctx context.Context,
	tx pgx.Tx,
	binding RecordFenceBinding,
) (PortabilitySnapshot, error) {
	if ctx == nil || adapter == nil || nilActionDependency(adapter.store) ||
		nilActionDependency(tx) || binding.Validate() != nil {
		return PortabilitySnapshot{}, ErrInvalidPortabilityAdapter
	}
	if err := ctx.Err(); err != nil {
		return PortabilitySnapshot{}, err
	}
	snapshot, err := adapter.store.BackupCollaboration(ctx, tx, binding)
	if err != nil {
		return PortabilitySnapshot{}, err
	}
	if err := snapshot.Validate(); err != nil {
		return PortabilitySnapshot{}, err
	}
	return snapshot.Clone(), nil
}

func (adapter *PortabilityAdapter) Restore(
	ctx context.Context,
	tx pgx.Tx,
	binding RecordFenceBinding,
	snapshot PortabilitySnapshot,
) error {
	if ctx == nil || adapter == nil || nilActionDependency(adapter.store) ||
		nilActionDependency(tx) || binding.Validate() != nil {
		return ErrInvalidPortabilityAdapter
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	return adapter.store.RestoreCollaboration(ctx, tx, binding, snapshot.Clone())
}

func validatePortableAction(action PortableAction) error {
	if ValidateActionID(action.ActionID) != nil || action.Version == 0 || action.Version > math.MaxInt64 ||
		!validActionStatus(action.Status) || recordauth.ValidateActorUserID(action.CreatedBy) != nil ||
		recordauth.ValidateActorUserID(action.UpdatedBy) != nil || !portableTimeCanonical(action.CreatedAt) ||
		!portableTimeCanonical(action.UpdatedAt) || action.UpdatedAt.Before(action.CreatedAt) ||
		(action.Status == ActionStatusCompleted) != (action.CompletedAt != nil) {
		return ErrInvalidPortabilitySnapshot
	}
	if _, err := NormalizeActionFields(ActionFieldValues{
		Title: action.Title, Details: action.Details, AssigneeID: action.AssigneeID,
		DueAt: action.DueAt, SubjectRevisionID: action.SubjectRevisionID,
	}); err != nil {
		return ErrInvalidPortabilitySnapshot
	}
	if action.CompletedAt != nil && (!portableTimeCanonical(*action.CompletedAt) || action.CompletedAt.Before(action.CreatedAt)) {
		return ErrInvalidPortabilitySnapshot
	}
	return nil
}

func validatePortableActionEvent(event PortableActionEvent) error {
	if !validPortableID(event.EventID, "raev_") || ValidateActionID(event.ActionID) != nil ||
		event.Version == 0 || event.Version > math.MaxInt64 || !validActionMutationKindPortable(event.Kind) ||
		!validActionStatus(event.CurrentStatus) || recordauth.ValidateActorUserID(event.ActorID) != nil ||
		(event.AssigneeID != "" && recordauth.ValidateActorUserID(event.AssigneeID) != nil) ||
		!portableTimeCanonical(event.OccurredAt) || !portableTimeCanonical(event.CreatedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	if event.PreviousStatus == nil {
		if event.Kind != ActionMutationCreate || event.Version != 1 || event.CurrentStatus != ActionStatusOpen {
			return ErrInvalidPortabilitySnapshot
		}
	} else {
		if !validActionStatus(*event.PreviousStatus) {
			return ErrInvalidPortabilitySnapshot
		}
		switch event.Kind {
		case ActionMutationUpdate:
			if *event.PreviousStatus != event.CurrentStatus {
				return ErrInvalidPortabilitySnapshot
			}
		case ActionMutationComplete, ActionMutationCancel, ActionMutationReopen:
			if ValidateActionMutationTransition(event.Kind, *event.PreviousStatus, event.CurrentStatus) != nil {
				return ErrInvalidPortabilitySnapshot
			}
		default:
			return ErrInvalidPortabilitySnapshot
		}
	}
	return nil
}

func validatePortableComment(comment PortableComment) error {
	if ValidateCommentID(comment.CommentID) != nil || recordauth.ValidateActorUserID(comment.AuthorID) != nil ||
		comment.Version == 0 || comment.Version > math.MaxInt64 || !portableTimeCanonical(comment.CreatedAt) ||
		!portableTimeCanonical(comment.UpdatedAt) || comment.UpdatedAt.Before(comment.CreatedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	if comment.State == CommentStateActive {
		content, err := NewCommentContent(comment.BodyMarkdown)
		if err != nil || !content.Model().Equal(comment.RenderModel) || content.Digest() != comment.BodyDigest ||
			comment.TombstoneID != "" || comment.RedactedAt != nil {
			return ErrInvalidPortabilitySnapshot
		}
		return nil
	}
	if comment.State != CommentStateRedacted || comment.BodyMarkdown != "" || comment.RenderModel.Version != "" ||
		len(comment.RenderModel.Nodes) != 0 || comment.BodyDigest != ([sha256.Size]byte{}) ||
		comment.Version < 2 || !validPortableID(comment.TombstoneID, "rct_") || comment.RedactedAt == nil ||
		!portableTimeCanonical(*comment.RedactedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	if comment.RedactedAt.Before(comment.CreatedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	return nil
}

func validatePortableCommentRevision(revision PortableCommentRevision) error {
	if !validPortableID(revision.RevisionID, "rcr_") || ValidateCommentID(revision.CommentID) != nil ||
		revision.Version == 0 || revision.Version > math.MaxInt64 || recordauth.ValidateActorUserID(revision.EditedBy) != nil ||
		!portableTimeCanonical(revision.CreatedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	if revision.RedactedAt == nil {
		content, err := NewCommentContent(revision.BodyMarkdown)
		if err != nil || !content.Model().Equal(revision.RenderModel) || content.Digest() != revision.BodyDigest ||
			revision.TombstoneID != "" {
			return ErrInvalidPortabilitySnapshot
		}
		return nil
	}
	if revision.BodyMarkdown != "" || revision.RenderModel.Version != "" || len(revision.RenderModel.Nodes) != 0 ||
		revision.BodyDigest != ([sha256.Size]byte{}) || !validPortableID(revision.TombstoneID, "rct_") ||
		!portableTimeCanonical(*revision.RedactedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	if revision.RedactedAt.Before(revision.CreatedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	return nil
}

func validatePortableCommentTombstone(tombstone PortableCommentTombstone) error {
	if !validPortableID(tombstone.TombstoneID, "rct_") || ValidateCommentID(tombstone.CommentID) != nil ||
		tombstone.Version == 0 || tombstone.Version > math.MaxInt64 ||
		recordauth.ValidateActorUserID(tombstone.DeletedBy) != nil || !portableTimeCanonical(tombstone.DeletedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	switch tombstone.ReasonCode {
	case PortableTombstoneAuthorDeleted, PortableTombstoneModeratorDeleted, PortableTombstoneRecordDeleted:
		return nil
	default:
		return ErrInvalidPortabilitySnapshot
	}
}

func validatePortableFollower(follower PortableFollower) error {
	if recordauth.ValidateActorUserID(follower.UserID) != nil || follower.Version == 0 || follower.Version > math.MaxInt64 ||
		ValidateFollowerPreference(follower.Preference) != nil ||
		(follower.Preference == FollowerPreferenceDefault && !follower.Sources.Any()) ||
		!portableTimeCanonical(follower.CreatedAt) || !portableTimeCanonical(follower.UpdatedAt) ||
		follower.UpdatedAt.Before(follower.CreatedAt) {
		return ErrInvalidPortabilitySnapshot
	}
	return nil
}

func validatePortableNotificationAudit(audit PortableNotificationAudit) error {
	expectedSubject, known := notificationSubjectForEvent(audit.Kind)
	if ValidateNotificationID(audit.NotificationID) != nil || !known ||
		!validNotificationEventKindPortable(audit.Kind) || audit.SubjectKind != expectedSubject ||
		!validNotificationSubjectKindPortable(audit.SubjectKind) || audit.SourceVersion == 0 ||
		audit.SourceVersion > math.MaxInt64 || !portableTimeCanonical(audit.EventAt) ||
		audit.SentCount+audit.UnknownCount+audit.PermanentFailed > audit.DeliveryCount {
		return ErrInvalidPortabilitySnapshot
	}
	return nil
}

func validActivityFactSource(fact ActivityFact) bool {
	switch fact.Kind {
	case ActivityFactRecordOwnerChanged:
		return fact.RevisionID != "" && fact.SourceEventID == fact.RevisionID+":"+string(RevisionFieldOwner)
	case ActivityFactRecordParticipantChanged:
		return fact.RevisionID != "" && fact.SourceEventID == fact.RevisionID+":"+string(RevisionFieldParticipants)
	case ActivityFactRecordFollowUpChanged:
		return fact.RevisionID != "" && fact.SourceEventID == fact.RevisionID+":"+string(RevisionFieldFollowUp)
	case ActivityFactActionCreated, ActivityFactActionUpdated, ActivityFactActionCompleted,
		ActivityFactActionCancelled, ActivityFactActionReopened:
		return validPortableID(fact.SourceEventID, "raev_")
	case ActivityFactCommentCreated, ActivityFactCommentEdited:
		return validPortableID(fact.SourceEventID, "rcr_")
	case ActivityFactCommentRedacted:
		return validPortableID(fact.SourceEventID, "rct_")
	default:
		return false
	}
}

func validActivityFactKind(kind ActivityFactKind) bool {
	switch kind {
	case ActivityFactRecordOwnerChanged, ActivityFactRecordParticipantChanged, ActivityFactRecordFollowUpChanged,
		ActivityFactActionCreated, ActivityFactActionUpdated, ActivityFactActionCompleted,
		ActivityFactActionCancelled, ActivityFactActionReopened,
		ActivityFactCommentCreated, ActivityFactCommentEdited, ActivityFactCommentRedacted:
		return true
	default:
		return false
	}
}

func validActionMutationKindPortable(kind ActionMutationKind) bool {
	return kind == ActionMutationCreate || kind == ActionMutationUpdate || kind == ActionMutationComplete ||
		kind == ActionMutationCancel || kind == ActionMutationReopen
}

func validNotificationEventKindPortable(kind NotificationEventKind) bool {
	switch kind {
	case NotificationEventRecordOwnerChanged, NotificationEventRecordParticipantChanged,
		NotificationEventRecordFollowUpDue, NotificationEventActionAssigned,
		NotificationEventActionCompleted, NotificationEventActionCancelled,
		NotificationEventCommentReplied, NotificationEventCommentMentioned,
		NotificationEventSecurityAccessRevoked:
		return true
	default:
		return false
	}
}

func validNotificationSubjectKindPortable(kind NotificationSubjectKind) bool {
	return kind == NotificationSubjectRecord || kind == NotificationSubjectAction || kind == NotificationSubjectComment
}

func validPortableID(value, prefix string) bool {
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

func validPortableText(value string, maximum int, allowEmpty bool) bool {
	if (!allowEmpty && value == "") || len(value) > maximum || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func portableTimeCanonical(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value == value.Round(0) &&
		value.Nanosecond()%int(time.Microsecond) == 0
}

func clonePortableTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func compareActivityFacts(left, right ActivityFact) int {
	if left.EventAt.Before(right.EventAt) {
		return -1
	}
	if left.EventAt.After(right.EventAt) {
		return 1
	}
	return strings.Compare(left.ActivityID, right.ActivityID)
}

func comparePortableMentions(left, right PortableCommentMention) int {
	leftKey := left.CommentID + "\x00" + portableVersionKey(left.CommentVersion) + "\x00" + left.MentionedUser
	rightKey := right.CommentID + "\x00" + portableVersionKey(right.CommentVersion) + "\x00" + right.MentionedUser
	return strings.Compare(leftKey, rightKey)
}

func portableVersionKey(version uint64) string {
	const width = 20
	value := strings.Repeat("0", width) + strings.TrimSpace(stringVersion(version))
	return value[len(value)-width:]
}

func stringVersion(version uint64) string {
	if version == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for version > 0 {
		index--
		digits[index] = byte('0' + version%10)
		version /= 10
	}
	return string(digits[index:])
}
