package recordcollaboration

import (
	"context"
	"errors"
	"math"
	"time"

	"houfeng/internal/center/recordauth"
)

var (
	ErrInvalidInboxRequest = errors.New("invalid record inbox request")
	ErrInboxNotFound       = errors.New("record inbox item not found")
	ErrInboxUnavailable    = errors.New("record inbox unavailable")
)

type InboxItem struct {
	NotificationID string
	RecordID       string
	EventKind      NotificationEventKind
	SubjectKind    NotificationSubjectKind
	SubjectID      string
	SourceVersion  uint64
	Reason         NotificationReason
	Mandatory      bool
	EventAt        time.Time
	ReadAt         *time.Time
	DismissedAt    *time.Time
}

func ValidateInboxNotificationID(value string) error {
	if !validNotificationID(value) {
		return ErrInvalidInboxRequest
	}
	return nil
}

func (item InboxItem) Validate() error {
	expected, ok := notificationSubjectForEvent(item.EventKind)
	if !ok || item.SubjectKind != expected || !validNotificationID(item.NotificationID) || !validRecordID(item.RecordID) ||
		!validNotificationSubjectIdentity(item.SubjectKind, item.SubjectID) || item.SourceVersion == 0 || item.SourceVersion > math.MaxInt64 ||
		ValidateNotificationReason(item.Reason) != nil || item.Mandatory != item.Reason.Mandatory() || item.EventAt.IsZero() {
		return ErrInvalidInboxRequest
	}
	if item.ReadAt != nil && item.ReadAt.Before(item.EventAt) {
		return ErrInvalidInboxRequest
	}
	if item.DismissedAt != nil && (item.ReadAt == nil || item.DismissedAt.Before(*item.ReadAt)) {
		return ErrInvalidInboxRequest
	}
	return nil
}

type InboxTransitionKind string

const (
	InboxTransitionUnread  InboxTransitionKind = "unread"
	InboxTransitionRead    InboxTransitionKind = "read"
	InboxTransitionDismiss InboxTransitionKind = "dismiss"
)

func ValidateInboxTransitionKind(kind InboxTransitionKind) error {
	switch kind {
	case InboxTransitionUnread, InboxTransitionRead, InboxTransitionDismiss:
		return nil
	default:
		return ErrInvalidInboxRequest
	}
}

type InboxDeepLinkTarget struct {
	RecordID    string
	SubjectKind NotificationSubjectKind
	SubjectID   string
}

func (target InboxDeepLinkTarget) Validate() error {
	if !validRecordID(target.RecordID) || !validNotificationSubjectIdentity(target.SubjectKind, target.SubjectID) {
		return ErrInvalidInboxRequest
	}
	return nil
}

type InboxListRequest struct {
	Actor recordauth.ActorScope
	Limit int
}

func (request InboxListRequest) Validate() error {
	if _, err := recordauth.NormalizeActorScope(request.Actor); err != nil || request.Limit < 1 || request.Limit > 100 {
		return ErrInvalidInboxRequest
	}
	return nil
}

type InboxItemRequest struct {
	Actor          recordauth.ActorScope
	NotificationID string
}

func (request InboxItemRequest) Validate() error {
	if _, err := recordauth.NormalizeActorScope(request.Actor); err != nil || !validNotificationID(request.NotificationID) {
		return ErrInvalidInboxRequest
	}
	return nil
}

type InboxTransitionRequest struct {
	Actor          recordauth.ActorScope
	NotificationID string
	Kind           InboxTransitionKind
}

func (request InboxTransitionRequest) Validate() error {
	if (InboxItemRequest{Actor: request.Actor, NotificationID: request.NotificationID}).Validate() != nil || ValidateInboxTransitionKind(request.Kind) != nil {
		return ErrInvalidInboxRequest
	}
	return nil
}

type InboxStore interface {
	ListInbox(context.Context, InboxListRequest) ([]InboxItem, error)
	GetInboxItem(context.Context, InboxItemRequest) (InboxItem, error)
	GetInboxDeepLink(context.Context, InboxItemRequest) (InboxDeepLinkTarget, error)
	TransitionInbox(context.Context, InboxTransitionRequest) (InboxItem, error)
	CountUnreadInbox(context.Context, InboxListRequest) (int, error)
}
