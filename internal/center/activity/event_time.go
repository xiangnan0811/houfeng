package activity

import (
	"errors"
	"time"
)

var ErrInvalidEventTime = errors.New("invalid activity event time")

// EventTimeInput is what an adapter knows about when something happened. It is
// deliberately explicit about lateness rather than letting the resolver guess:
// the only honest source of "this arrived late" is the producer, not a timestamp
// comparison.
type EventTimeInput struct {
	Kind       EventKind
	RevisionNo uint64

	// OccurredAt is the operator-confirmed or source-authoritative moment the
	// fact happened.
	OccurredAt time.Time
	// SavedAt is when Houfeng accepted the event.
	SavedAt time.Time
	// ObservationEnd is the end of the window an evidence snapshot covers.
	ObservationEnd time.Time

	// Authoritative marks a system fact whose producer owns the occurrence
	// time. Manual record events leave it false.
	Authoritative bool
	// SourceIsLate is the producer stating this fact arrived after the window
	// it describes, e.g. a delayed agent batch or a correction.
	SourceIsLate bool
	// CaptureIsLate is the evidence equivalent: the capture landed well after
	// the observation it summarizes.
	CaptureIsLate bool
}

// ResolvedEventTime is the pair the projection stores, plus the honest
// backfilled flag.
type ResolvedEventTime struct {
	EventAt    time.Time
	RecordedAt time.Time
	Backfilled bool
}

// ResolveEventTime applies the fixed event-time rules. The whole point is that
// one column answers "when did this happen" and another answers "when did we
// learn about it", so a late fact can sort into its real position on the
// timeline while still being visibly late.
func ResolveEventTime(input EventTimeInput) (ResolvedEventTime, error) {
	if !ValidEventKind(input.Kind) {
		return ResolvedEventTime{}, ErrInvalidEventKind
	}
	if input.SavedAt.IsZero() {
		return ResolvedEventTime{}, ErrInvalidEventTime
	}
	recordedAt := input.SavedAt.UTC()

	var eventAt time.Time
	switch {
	case input.Kind == EventKindEvidenceCaptured:
		if input.ObservationEnd.IsZero() {
			return ResolvedEventTime{}, ErrInvalidEventTime
		}
		eventAt = input.ObservationEnd.UTC()
	case isFirstRecordRevision(input):
		if input.OccurredAt.IsZero() {
			return ResolvedEventTime{}, ErrInvalidEventTime
		}
		eventAt = input.OccurredAt.UTC()
	case input.Authoritative:
		if input.OccurredAt.IsZero() {
			return ResolvedEventTime{}, ErrInvalidEventTime
		}
		eventAt = input.OccurredAt.UTC()
	default:
		// Later revisions, state changes and visibility changes are edits made
		// at save time; they describe now, not the original occurrence.
		eventAt = recordedAt
	}

	return ResolvedEventTime{
		EventAt:    eventAt,
		RecordedAt: recordedAt,
		Backfilled: input.SourceIsLate || input.CaptureIsLate,
	}, nil
}

// isFirstRecordRevision folds record creation and revision 1 into one event.
// Emitting both would put two rows on the timeline for a single act.
func isFirstRecordRevision(input EventTimeInput) bool {
	switch input.Kind {
	case EventKindRecordCreated, EventKindRecordRevised:
		return input.RevisionNo == 1
	default:
		return false
	}
}
