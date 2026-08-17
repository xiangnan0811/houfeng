package recordcollaboration

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"time"

	"houfeng/internal/center/recordauth"
)

var (
	ErrInvalidRevisionFilterFacts       = errors.New("invalid collaboration revision filter facts")
	ErrMembershipDenied                 = errors.New("record collaboration membership denied")
	ErrMembershipUnavailable            = errors.New("record collaboration membership unavailable")
	ErrRevisionParticipationUnavailable = errors.New("record collaboration revision participation unavailable")
)

// RevisionFieldKind is the closed revision-owned collaboration filter and
// activity field registry. Its order is intentionally stable for downstream
// Search and Activity adapters.
type RevisionFieldKind string

const (
	RevisionFieldOwner        RevisionFieldKind = "owner"
	RevisionFieldParticipants RevisionFieldKind = "participants"
	RevisionFieldFollowUp     RevisionFieldKind = "follow_up"
)

// RevisionActivityKind is the closed content-free activity fact registry
// emitted by the revision participant for later Activity projection.
type RevisionActivityKind string

const (
	RevisionActivityRecordOwnerChanged       RevisionActivityKind = "record_owner_changed"
	RevisionActivityRecordParticipantChanged RevisionActivityKind = "record_participant_changed"
	RevisionActivityRecordFollowUpChanged    RevisionActivityKind = "record_follow_up_changed"
)

type RevisionFilterFactValues struct {
	OwnerID        string
	ParticipantIDs []string
	FollowUpAt     *time.Time
}

// RevisionFilterFacts is a normalized, content-free set of collaboration
// filters derived from one immutable complete revision.
type RevisionFilterFacts struct {
	ownerID        string
	participantIDs []string
	followUpAt     *time.Time
}

func NormalizeRevisionFilterFacts(values RevisionFilterFactValues) (RevisionFilterFacts, error) {
	if values.OwnerID != "" && recordauth.ValidateActorUserID(values.OwnerID) != nil {
		return RevisionFilterFacts{}, fmt.Errorf("%w: owner", ErrInvalidRevisionFilterFacts)
	}
	participantIDs := append([]string(nil), values.ParticipantIDs...)
	for _, participantID := range participantIDs {
		if recordauth.ValidateActorUserID(participantID) != nil {
			return RevisionFilterFacts{}, fmt.Errorf("%w: participant", ErrInvalidRevisionFilterFacts)
		}
	}
	sort.Strings(participantIDs)
	participantIDs = slices.Compact(participantIDs)
	return RevisionFilterFacts{
		ownerID:        values.OwnerID,
		participantIDs: participantIDs,
		followUpAt:     collaborationUTCTimePointer(values.FollowUpAt),
	}, nil
}

func (facts RevisionFilterFacts) OwnerID() string {
	return facts.ownerID
}

func (facts RevisionFilterFacts) ParticipantIDs() []string {
	return append([]string(nil), facts.participantIDs...)
}

func (facts RevisionFilterFacts) FollowUpAt() *time.Time {
	return collaborationUTCTimePointer(facts.followUpAt)
}

// DiffRevisionFilterFacts returns changed fields in the only supported
// deterministic emission order.
func DiffRevisionFilterFacts(previous, current RevisionFilterFacts) []RevisionFieldKind {
	changed := make([]RevisionFieldKind, 0, 3)
	if previous.ownerID != current.ownerID {
		changed = append(changed, RevisionFieldOwner)
	}
	if !slices.Equal(previous.participantIDs, current.participantIDs) {
		changed = append(changed, RevisionFieldParticipants)
	}
	if !reflect.DeepEqual(previous.followUpAt, current.followUpAt) {
		changed = append(changed, RevisionFieldFollowUp)
	}
	return changed
}

func collaborationUTCTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC().Truncate(time.Microsecond)
	return &utc
}
