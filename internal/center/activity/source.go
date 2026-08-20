package activity

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"sort"
	"time"

	"houfeng/internal/center/recordauth"
)

var (
	ErrInvalidSourceHead   = errors.New("invalid activity source head")
	ErrSourceNotReady      = errors.New("activity source is not ready for export")
	ErrIncompleteReadiness = errors.New("incomplete activity readiness vector")
)

const (
	// DefaultSourceSafetyLag is how far behind the clock an incremental scan
	// stops. A transaction that began before the watermark can still commit
	// after we read it, so scanning up to "now" would step over rows that were
	// never visible to us.
	DefaultSourceSafetyLag = 30 * time.Second

	// DefaultReprojectionWindow is how far back each pass re-reads. This is what
	// turns "eventually visible" into "eventually projected": a row that
	// committed just under the previous watermark is seen again on the next
	// pass. Re-reading is free of duplicates because projection is keyed on
	// source identity and classified before any sequence is allocated.
	DefaultReprojectionWindow = 15 * time.Minute
)

// SourceHead is a content-free, comparable upper bound on what one scan may
// read. It deliberately distinguishes two strengths, because the projection and
// the export have different needs: incremental projection tolerates a late row
// arriving after the watermark (it is re-scanned and keeps its true event time),
// while an export that claimed completeness on the same evidence would be
// asserting something it cannot know.
type SourceHead struct {
	Kind SourceKind
	// RecordedThrough bounds the scan by the source's own recorded time.
	RecordedThrough time.Time
	// TransactionHorizon is the transaction id horizon below which every
	// transaction has finished. Zero means this head was taken for incremental
	// projection only and must not back a completeness claim.
	TransactionHorizon uint64
}

// NewIncrementalSourceHead returns the lagging head used to keep projecting.
func NewIncrementalSourceHead(kind SourceKind, now time.Time, lag time.Duration) SourceHead {
	return SourceHead{
		Kind:            kind,
		RecordedThrough: now.UTC().Add(-lag),
	}
}

// NewSettledSourceHead returns a head that carries a transaction horizon and can
// therefore be used as export completeness evidence.
func NewSettledSourceHead(kind SourceKind, recordedThrough time.Time, horizon uint64) SourceHead {
	return SourceHead{
		Kind:               kind,
		RecordedThrough:    recordedThrough.UTC(),
		TransactionHorizon: horizon,
	}
}

// SupportsCompletenessClaim reports whether this head proves no earlier
// transaction can still become visible.
func (head SourceHead) SupportsCompletenessClaim() bool {
	return head.TransactionHorizon != 0 && !head.RecordedThrough.IsZero() && ValidSourceKind(head.Kind)
}

func (head SourceHead) validate() error {
	if !ValidSourceKind(head.Kind) || head.RecordedThrough.IsZero() {
		return ErrInvalidSourceHead
	}
	return nil
}

// ScanWindow is the half-open range one pass reads.
type ScanWindow struct {
	From    time.Time
	Through time.Time
}

// SourceCheckpoint is the projector's durable position in one source.
type SourceCheckpoint struct {
	Kind            SourceKind
	RecordedThrough time.Time
	CaughtUp        bool
	Attempt         uint64
	LastErrorCode   string
}

// FrontierWindow is the forward range that makes progress. Both bounds are
// inclusive: a page cut off in the middle of a group of rows sharing one
// timestamp can only advance the position to that timestamp, and an exclusive
// lower bound would then drop the rest of the group. Re-reading the boundary
// instant instead costs one classification per row and no sequence numbers.
//
// A first run has no position, so the window opens at the zero time and covers
// all history; starting at "now minus a window" would silently skip everything
// older.
func (checkpoint SourceCheckpoint) FrontierWindow(head SourceHead) ScanWindow {
	return ScanWindow{From: checkpoint.RecordedThrough, Through: head.RecordedThrough}
}

// TrailingWindow is the backward range re-read on every pass, and it is the only
// reason a late commit is not lost. None of the five sources orders rows by
// commit, so a transaction that began under the previous watermark can commit
// after it; its row then appears below the position the forward scan has already
// passed. Re-reading is safe because publication is keyed on source identity.
//
// The second return is false on a first run, where the forward window already
// covers all history and there is nothing behind it.
func (checkpoint SourceCheckpoint) TrailingWindow(overlap time.Duration) (ScanWindow, bool) {
	if checkpoint.RecordedThrough.IsZero() {
		return ScanWindow{}, false
	}
	return ScanWindow{
		From:    checkpoint.RecordedThrough.Add(-overlap),
		Through: checkpoint.RecordedThrough,
	}, true
}

// ValidateHead refuses a head that moved backwards or belongs to another source.
// Accepting one would advance the checkpoint over a range that was never read.
func (checkpoint SourceCheckpoint) ValidateHead(head SourceHead) error {
	if err := head.validate(); err != nil {
		return err
	}
	if head.Kind != checkpoint.Kind {
		return ErrInvalidSourceHead
	}
	if !checkpoint.RecordedThrough.IsZero() && head.RecordedThrough.Before(checkpoint.RecordedThrough) {
		return ErrInvalidSourceHead
	}
	return nil
}

// SourceReadiness is one source's contribution to an export completeness proof.
type SourceReadiness struct {
	Kind     SourceKind
	Head     SourceHead
	CaughtUp bool

	// ExcludedRows counts source rows this adapter deliberately does not project
	// because it cannot establish their provenance. The monitoring log is the real
	// case: it holds rows written before the metadata contract existed, and those
	// rows are permanent history, so blocking on them would stall the source
	// forever while projecting them would put unprovable claims on a timeline.
	//
	// The count is part of the proof rather than a log line. An export that says
	// "complete" while an adapter silently skipped rows is making a false claim;
	// with the count, it can say what it left out and why.
	ExcludedRows uint64
}

// ActivitySnapshot pins the projection an export reads. Child 7 freezes this
// type; Child 10 consumes it and must not need to change activity code to add to
// it.
type ActivitySnapshot struct {
	ProjectionGeneration    uint64
	PublishedIngestSequence uint64
	ReadinessDigest         [32]byte
}

// ReadinessVector is the whole completeness proof: one snapshot plus a per-source
// head and catch-up state.
type ReadinessVector struct {
	Snapshot ActivitySnapshot
	Sources  []SourceReadiness
}

// ValidateForExport fails closed. Every required source must be present, caught
// up, and backed by a head strong enough to prove nothing earlier can still
// appear. Anything weaker would let an incomplete archive look complete, which is
// worse than refusing to produce one.
func (vector ReadinessVector) ValidateForExport(required []SourceKind) error {
	if vector.Snapshot.ProjectionGeneration == 0 || vector.Snapshot.PublishedIngestSequence == 0 {
		return ErrIncompleteReadiness
	}
	present := make(map[SourceKind]SourceReadiness, len(vector.Sources))
	for _, source := range vector.Sources {
		if !ValidSourceKind(source.Kind) || source.Kind != source.Head.Kind {
			return ErrIncompleteReadiness
		}
		present[source.Kind] = source
	}
	for _, kind := range required {
		source, ok := present[kind]
		if !ok {
			return ErrIncompleteReadiness
		}
		if !source.CaughtUp || !source.Head.SupportsCompletenessClaim() {
			return ErrSourceNotReady
		}
	}
	return nil
}

// Digest binds an export to exactly this projection and this set of source
// heads. Sources are sorted first so two equivalent vectors cannot disagree
// merely because a registry iterated in a different order.
func (vector ReadinessVector) Digest() [sha256.Size]byte {
	digest := sha256.New()
	digest.Write([]byte("houfeng.record-activity.readiness.v1"))

	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], vector.Snapshot.ProjectionGeneration)
	digest.Write(scratch[:])
	binary.BigEndian.PutUint64(scratch[:], vector.Snapshot.PublishedIngestSequence)
	digest.Write(scratch[:])

	sorted := make([]SourceReadiness, len(vector.Sources))
	copy(sorted, vector.Sources)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Kind < sorted[j].Kind })
	binary.BigEndian.PutUint64(scratch[:], uint64(len(sorted)))
	digest.Write(scratch[:])
	for _, source := range sorted {
		binary.BigEndian.PutUint64(scratch[:], uint64(len(source.Kind)))
		digest.Write(scratch[:])
		digest.Write([]byte(source.Kind))
		binary.BigEndian.PutUint64(scratch[:], uint64(source.Head.RecordedThrough.UnixMicro()))
		digest.Write(scratch[:])
		binary.BigEndian.PutUint64(scratch[:], source.Head.TransactionHorizon)
		digest.Write(scratch[:])
		binary.BigEndian.PutUint64(scratch[:], source.ExcludedRows)
		digest.Write(scratch[:])
		if source.CaughtUp {
			digest.Write([]byte{1})
		} else {
			digest.Write([]byte{0})
		}
	}

	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum
}

// ExportScope narrows a readiness question to one actor's authorization.
type ExportScope struct {
	Actor recordauth.ActorScope
}

// CandidateEvent is one normalized source event awaiting publication. It has no
// ingest sequence yet: sequence numbers are allocated only inside the final
// publish transaction, under the generation head lock.
type CandidateEvent struct {
	ActivityID    string
	Source        SourceIdentity
	EventKind     EventKind
	EventAt       time.Time
	RecordedAt    time.Time
	Backfilled    bool
	Actor         *ActorSnapshot
	Subjects      []SubjectSnapshot
	Presentation  Presentation
	Corrects      string
	Severity      string
	RecordID      string
	RevisionID    string
	EvidenceID    string
	AuthScope     recordauth.ResourceScope
	CanonicalHash [32]byte
}

// ComputeCanonicalHash fingerprints everything the projection stores about this
// event. It is what makes a retry provable: the same source identity must
// produce the same bytes, and a source that changes them is reporting a
// different fact under an identity it already used.
func (candidate CandidateEvent) ComputeCanonicalHash() [sha256.Size]byte {
	digest := sha256.New()
	digest.Write([]byte("houfeng.record-activity.canonical.v1"))
	writeLengthPrefixed(digest, candidate.ActivityID)
	writeLengthPrefixed(digest, string(candidate.Source.Kind))
	writeLengthPrefixed(digest, candidate.Source.EventID)
	writeUint64(digest, candidate.Source.Version)
	writeLengthPrefixed(digest, string(candidate.EventKind))
	writeUint64(digest, uint64(candidate.EventAt.UTC().UnixMicro()))
	writeUint64(digest, uint64(candidate.RecordedAt.UTC().UnixMicro()))
	writeLengthPrefixed(digest, candidate.RecordID)
	writeLengthPrefixed(digest, candidate.RevisionID)
	writeLengthPrefixed(digest, candidate.EvidenceID)
	writeLengthPrefixed(digest, candidate.Corrects)
	writeLengthPrefixed(digest, candidate.Severity)
	if candidate.Backfilled {
		digest.Write([]byte{1})
	} else {
		digest.Write([]byte{0})
	}
	if candidate.Actor != nil {
		writeLengthPrefixed(digest, candidate.Actor.ActorID)
		writeLengthPrefixed(digest, candidate.Actor.DisplayName)
	} else {
		writeUint64(digest, 0)
	}
	writeUint64(digest, candidate.Presentation.Version)
	writeLengthPrefixed(digest, candidate.Presentation.Title)
	writeLengthPrefixed(digest, candidate.Presentation.Summary)

	writeUint64(digest, uint64(len(candidate.Subjects)))
	for _, subject := range candidate.Subjects {
		digest.Write(subjectSnapshotBytes(subject))
	}

	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum
}

// AuthScopeDigest is the stored authorization fingerprint. Every projection and
// relation row carries it so an authorization filter can run before ORDER BY
// rather than after, which is what keeps a sparse filter from returning short
// pages.
//
// It fingerprints the visibility scope specifically, because visibility is what
// decides who may see the row; the wider ResourceScope also names the sources the
// event came from, which is not an access decision.
func (candidate CandidateEvent) AuthScopeDigest() [sha256.Size]byte {
	return candidate.AuthScope.Visibility.CanonicalHashValue()
}

// RelationHash proves a redundant relation row still agrees with its projection
// parent. The relation columns exist so filters apply before LIMIT; this hash is
// what keeps that denormalization from drifting into a second source of truth.
func (candidate CandidateEvent) RelationHash(subject SubjectSnapshot) [sha256.Size]byte {
	digest := sha256.New()
	digest.Write([]byte("houfeng.record-activity.relation.v1"))
	writeLengthPrefixed(digest, candidate.ActivityID)
	writeLengthPrefixed(digest, string(candidate.EventKind))
	writeLengthPrefixed(digest, string(candidate.Source.Kind))
	writeUint64(digest, uint64(candidate.EventAt.UTC().UnixMicro()))
	writeUint64(digest, uint64(candidate.RecordedAt.UTC().UnixMicro()))
	writeLengthPrefixed(digest, candidate.RecordID)
	writeLengthPrefixed(digest, candidate.RevisionID)
	writeLengthPrefixed(digest, candidate.EvidenceID)
	digest.Write(subjectSnapshotBytes(subject))

	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum
}

func subjectSnapshotBytes(subject SubjectSnapshot) []byte {
	digest := sha256.New()
	writeLengthPrefixed(digest, string(subject.Kind))
	writeLengthPrefixed(digest, subject.SourceID)
	writeLengthPrefixed(digest, string(subject.Role))
	writeLengthPrefixed(digest, subject.LiveRoute)
	if subject.Primary {
		digest.Write([]byte{1})
	} else {
		digest.Write([]byte{0})
	}
	if subject.Tombstoned {
		digest.Write([]byte{1})
	} else {
		digest.Write([]byte{0})
	}
	keys := make([]string, 0, len(subject.Identity))
	for key := range subject.Identity {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	writeUint64(digest, uint64(len(keys)))
	for _, key := range keys {
		writeLengthPrefixed(digest, key)
		writeLengthPrefixed(digest, subject.Identity[key])
	}
	return digest.Sum(nil)
}

func writeLengthPrefixed(digest io.Writer, value string) {
	writeUint64(digest, uint64(len(value)))
	_, _ = digest.Write([]byte(value))
}

func writeUint64(digest io.Writer, value uint64) {
	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], value)
	_, _ = digest.Write(scratch[:])
}

// SourceAdapter is how one authoritative producer exposes itself to the
// projector. It scans and normalizes; it never decides ordering or allocates
// sequence numbers.
type SourceAdapter interface {
	Kind() SourceKind
	// ScanAfter reads the window and returns normalized candidates. It must not
	// return anything recorded after the window's Through bound.
	ScanAfter(context.Context, ScanWindow, int) ([]CandidateEvent, error)
	// IncrementalHead is the lagging bound used to keep projecting.
	IncrementalHead(context.Context) (SourceHead, error)
}

// ExportReadySourceAdapter adds the stronger evidence an export needs. Keeping it
// separate from SourceAdapter makes the weaker guarantee impossible to pass off
// as the stronger one by accident.
type ExportReadySourceAdapter interface {
	SourceAdapter
	AuthoritativeHead(context.Context, ExportScope) (SourceHead, error)
	Readiness(context.Context, ExportScope, SourceHead) (SourceReadiness, error)
}
