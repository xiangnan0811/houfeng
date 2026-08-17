package recordcollaboration

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"houfeng/internal/center/recordauth"
)

const (
	MaxCommentVersion  uint64 = math.MaxInt64
	MaxCommentMentions        = 512
)

var (
	ErrInvalidCommentContent = errors.New("invalid record comment content")
	ErrInvalidCommentRequest = errors.New("invalid record comment request")
	ErrInvalidCommentCommand = errors.New("invalid record comment command")
	ErrCommentNotFound       = errors.New("record comment not found")
	ErrCommentConflict       = errors.New("record comment conflict")
	ErrCommentPolicyDenied   = errors.New("record comment policy denied")
)

type CommentMutationKind string

const (
	CommentMutationCreate CommentMutationKind = "created"
	CommentMutationEdit   CommentMutationKind = "edited"
	CommentMutationRedact CommentMutationKind = "redacted"
)

type CommentActivityKind string

const (
	CommentActivityCreated  CommentActivityKind = "comment_created"
	CommentActivityEdited   CommentActivityKind = "comment_edited"
	CommentActivityRedacted CommentActivityKind = "comment_redacted"
)

func ActivityKindForCommentMutation(kind CommentMutationKind) (CommentActivityKind, error) {
	switch kind {
	case CommentMutationCreate:
		return CommentActivityCreated, nil
	case CommentMutationEdit:
		return CommentActivityEdited, nil
	case CommentMutationRedact:
		return CommentActivityRedacted, nil
	default:
		return "", ErrInvalidCommentCommand
	}
}

type CommentContent struct {
	source string
	model  CommentRenderModel
	digest [sha256.Size]byte
}

func NewCommentContent(source string) (CommentContent, error) {
	model, err := ParseCommentMarkdownV1(source)
	if err != nil {
		return CommentContent{}, err
	}
	return CommentContent{source: source, model: model.Clone(), digest: sha256.Sum256([]byte(source))}, nil
}

func (content CommentContent) Source() string            { return content.source }
func (content CommentContent) Model() CommentRenderModel { return content.model.Clone() }
func (content CommentContent) Digest() [sha256.Size]byte { return content.digest }
func (content CommentContent) Empty() bool {
	return content.source == "" && content.model.Version == "" && len(content.model.Nodes) == 0 &&
		content.digest == ([sha256.Size]byte{})
}
func (content CommentContent) Validate() error {
	if content.source == "" || content.model.Validate() != nil || content.digest != sha256.Sum256([]byte(content.source)) {
		return ErrInvalidCommentContent
	}
	parsed, err := ParseCommentMarkdownV1(content.source)
	if err != nil || !parsed.Equal(content.model) {
		return ErrInvalidCommentContent
	}
	return nil
}

func NormalizeCommentMentionUserIDs(values []string) ([]string, error) {
	if len(values) > MaxCommentMentions {
		return nil, ErrInvalidCommentContent
	}
	normalized := append([]string(nil), values...)
	for _, userID := range normalized {
		if recordauth.ValidateActorUserID(userID) != nil {
			return nil, fmt.Errorf("%w: mention", ErrInvalidCommentContent)
		}
	}
	sort.Strings(normalized)
	result := normalized[:0]
	for _, userID := range normalized {
		if len(result) == 0 || result[len(result)-1] != userID {
			result = append(result, userID)
		}
	}
	return append([]string(nil), result...), nil
}

func IsIncrementableCommentVersion(version uint64) bool {
	return version > 0 && version < MaxCommentVersion
}

type CommentMutationResult struct {
	CommentID string
	RecordID  string
	Version   uint64
	State     CommentState
	EventKind CommentMutationKind
	Replayed  bool
	ChangedAt time.Time
}

func (result CommentMutationResult) Validate() error {
	if ValidateCommentID(result.CommentID) != nil || !validRecordID(result.RecordID) || result.Version == 0 ||
		result.Version > MaxCommentVersion || !validCommentMutationKind(result.EventKind) || result.ChangedAt.IsZero() ||
		(result.EventKind == CommentMutationRedact) != (result.State == CommentStateRedacted) ||
		(result.EventKind != CommentMutationRedact) != (result.State == CommentStateActive) {
		return ErrInvalidCommentCommand
	}
	return nil
}

type CommentRecord struct {
	CommentID        string
	RecordID         string
	AuthorID         string
	Version          uint64
	State            CommentState
	BodyMarkdown     string
	RenderModel      CommentRenderModel
	ReplyToCommentID string
	MentionUserIDs   []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	RedactedAt       *time.Time
}

func (record CommentRecord) Validate() error {
	if ValidateCommentID(record.CommentID) != nil || !validRecordID(record.RecordID) ||
		recordauth.ValidateActorUserID(record.AuthorID) != nil || record.Version == 0 || record.Version > MaxCommentVersion ||
		record.CreatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) ||
		(record.ReplyToCommentID != "" && ValidateCommentID(record.ReplyToCommentID) != nil) {
		return ErrInvalidCommentCommand
	}
	mentions, err := NormalizeCommentMentionUserIDs(record.MentionUserIDs)
	if err != nil || !equalCommentStrings(mentions, record.MentionUserIDs) {
		return ErrInvalidCommentCommand
	}
	switch record.State {
	case CommentStateActive:
		content, err := NewCommentContent(record.BodyMarkdown)
		if err != nil || !content.Model().Equal(record.RenderModel) || record.RedactedAt != nil {
			return ErrInvalidCommentCommand
		}
	case CommentStateRedacted:
		if record.BodyMarkdown != "" || record.RenderModel.Version != "" || len(record.RenderModel.Nodes) != 0 ||
			record.RedactedAt == nil || record.RedactedAt.Before(record.CreatedAt) || len(record.MentionUserIDs) != 0 {
			return ErrInvalidCommentCommand
		}
	default:
		return ErrInvalidCommentCommand
	}
	return nil
}

func (record CommentRecord) Clone() CommentRecord {
	cloned := record
	cloned.RenderModel = record.RenderModel.Clone()
	cloned.MentionUserIDs = append([]string(nil), record.MentionUserIDs...)
	if record.RedactedAt != nil {
		value := record.RedactedAt.UTC()
		cloned.RedactedAt = &value
	}
	return cloned
}

func validCommentMutationKind(kind CommentMutationKind) bool {
	return kind == CommentMutationCreate || kind == CommentMutationEdit || kind == CommentMutationRedact
}

func equalCommentStrings(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}
