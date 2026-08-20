package activity

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

var (
	// ErrInvalidRecordSelection means an export named records this grammar does
	// not recognize, or named none at all. An export of "everything the reader
	// could reach" is a different feature and must not be reachable by omission.
	ErrInvalidRecordSelection = errors.New("activity export record selection is invalid")
	// ErrSelectionNotNormalized means a selection reached a digest or a query
	// without going through NormalizeRecordSelection, so two callers asking the
	// same question could produce different proofs.
	ErrSelectionNotNormalized = errors.New("activity export record selection is not normalized")
	// ErrReadinessBindingMismatch means a readiness vector does not belong to the
	// scope and selection it was presented for. An export that accepted it would
	// be proving completeness of a different question.
	ErrReadinessBindingMismatch = errors.New("activity readiness digest does not bind this scope and selection")
	// ErrPageCursorMismatch means a page position was minted against a different
	// snapshot, actor or selection. Continuing from it would splice two different
	// reads into one archive.
	ErrPageCursorMismatch = errors.New("activity export page cursor belongs to another read")
)

// ExportPageLimit bounds one export page. Export is a background archive rather
// than an interactive list, so the limit exists to bound memory and statement
// duration, not to shape a response.
const ExportPageLimit = 500

// RecordSelection is the set of records an export covers. It is explicit by
// design: an archive has to say which records it claims to contain, and a reader
// verifying it later has to be able to ask the same question and get the same
// answer.
type RecordSelection struct {
	RecordIDs []string

	normalized bool
}

// NormalizeRecordSelection is the only way to obtain a selection a digest will
// accept. Sorting and de-duplicating here is what makes the proof independent of
// the order a caller happened to list records in.
func NormalizeRecordSelection(input RecordSelection) (RecordSelection, error) {
	if len(input.RecordIDs) == 0 {
		return RecordSelection{}, fmt.Errorf("%w: no records selected", ErrInvalidRecordSelection)
	}
	unique := make(map[string]bool, len(input.RecordIDs))
	ordered := make([]string, 0, len(input.RecordIDs))
	for _, recordID := range input.RecordIDs {
		if !records.ValidRecordRootID(recordID) {
			return RecordSelection{}, fmt.Errorf("%w: record id %q", ErrInvalidRecordSelection, recordID)
		}
		if unique[recordID] {
			continue
		}
		unique[recordID] = true
		ordered = append(ordered, recordID)
	}
	sort.Strings(ordered)
	return RecordSelection{RecordIDs: ordered, normalized: true}, nil
}

// Normalized reports whether this value came from NormalizeRecordSelection.
func (selection RecordSelection) Normalized() bool { return selection.normalized }

// Digest fingerprints the selection. It refuses to answer for a value that was
// never normalized, because a digest over an arbitrary ordering would make two
// identical selections look like different ones.
func (selection RecordSelection) Digest() ([sha256.Size]byte, error) {
	if !selection.normalized {
		return [sha256.Size]byte{}, ErrSelectionNotNormalized
	}
	digest := sha256.New()
	digest.Write([]byte("houfeng.record-activity.export-selection.v1"))
	var scratch [8]byte
	binary.BigEndian.PutUint64(scratch[:], uint64(len(selection.RecordIDs)))
	digest.Write(scratch[:])
	for _, recordID := range selection.RecordIDs {
		binary.BigEndian.PutUint64(scratch[:], uint64(len(recordID)))
		digest.Write(scratch[:])
		digest.Write([]byte(recordID))
	}
	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum, nil
}

// Contains reports whether a record is in the selection.
func (selection RecordSelection) Contains(recordID string) bool {
	for _, selected := range selection.RecordIDs {
		if selected == recordID {
			return true
		}
	}
	return false
}

// ExportReadinessDigest binds a completeness claim to the question it answers:
// this actor, these records, this generation and published head, and this
// per-source vector. Any of those changing has to invalidate the proof, which is
// why they are all in one digest rather than compared field by field at each
// call site.
func ExportReadinessDigest(
	scope ExportScope,
	selection RecordSelection,
	snapshot ActivitySnapshot,
	sources []SourceReadiness,
) ([sha256.Size]byte, error) {
	selectionDigest, err := selection.Digest()
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	// Normalizing rather than hashing the value as given is what makes the proof
	// actor-specific: CanonicalHash degrades to hashing nothing for a scope it
	// cannot normalize, and a proof bound to "no actor" would be interchangeable
	// between readers.
	actor, err := recordauth.NormalizeActorScope(scope.Actor)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	actorDigest := actor.CanonicalHash()

	vector := ReadinessVector{
		Snapshot: ActivitySnapshot{
			ProjectionGeneration:    snapshot.ProjectionGeneration,
			PublishedIngestSequence: snapshot.PublishedIngestSequence,
		},
		Sources: sources,
	}
	vectorDigest := vector.Digest()

	digest := sha256.New()
	digest.Write([]byte("houfeng.record-activity.export-readiness.v1"))
	digest.Write(actorDigest[:])
	digest.Write(selectionDigest[:])
	digest.Write(vectorDigest[:])
	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum, nil
}

// ValidateBinding proves a vector belongs to this question. Child 10 calls it on
// a vector it was handed rather than trusting the caller that handed it over.
func (vector ReadinessVector) ValidateBinding(scope ExportScope, selection RecordSelection) error {
	expected, err := ExportReadinessDigest(scope, selection, vector.Snapshot, vector.Sources)
	if err != nil {
		return err
	}
	if vector.Snapshot.ReadinessDigest != expected {
		return ErrReadinessBindingMismatch
	}
	return nil
}

// PageCursor is an export's position in a selection. It is not the browser
// cursor: this seam is internal, so the position is a typed value rather than an
// encrypted token. What it does share is the refusal to accept a bare sequence —
// it carries the digests of the read it was minted in, so a position cannot be
// replayed against another snapshot, actor or selection.
type PageCursor struct {
	SnapshotDigest  [sha256.Size]byte
	ActorDigest     [sha256.Size]byte
	SelectionDigest [sha256.Size]byte
	Position        SortKey
}

// NewPageCursor mints the position after the last row of a page.
func NewPageCursor(
	scope ExportScope,
	selection RecordSelection,
	snapshot ActivitySnapshot,
	position SortKey,
) (PageCursor, error) {
	selectionDigest, err := selection.Digest()
	if err != nil {
		return PageCursor{}, err
	}
	if err := validateSortKey(position); err != nil {
		return PageCursor{}, err
	}
	actor, err := recordauth.NormalizeActorScope(scope.Actor)
	if err != nil {
		return PageCursor{}, err
	}
	return PageCursor{
		SnapshotDigest:  snapshot.ReadinessDigest,
		ActorDigest:     actor.CanonicalHash(),
		SelectionDigest: selectionDigest,
		Position:        position,
	}, nil
}

// FirstPage reports whether this is the zero position, which means "start at the
// beginning" rather than "an empty position that failed to bind".
func (cursor PageCursor) FirstPage() bool {
	return cursor == PageCursor{}
}

// Validate checks a position against the read it is being used in. A first page
// needs no binding; anything else must match all three digests.
func (cursor PageCursor) Validate(
	scope ExportScope,
	selection RecordSelection,
	snapshot ActivitySnapshot,
) error {
	if cursor.FirstPage() {
		return nil
	}
	expected, err := NewPageCursor(scope, selection, snapshot, cursor.Position)
	if err != nil {
		return err
	}
	if cursor.SnapshotDigest != expected.SnapshotDigest ||
		cursor.ActorDigest != expected.ActorDigest ||
		cursor.SelectionDigest != expected.SelectionDigest {
		return ErrPageCursorMismatch
	}
	return nil
}

// ActivityEnvelope is one projected activity as an export reads it. It is the
// stored fact plus the sequence it was published at, and deliberately nothing
// else: no record body, no comment text, no command output, because none of that
// is in the projection to begin with.
type ActivityEnvelope struct {
	ActivityID     string
	IngestSequence uint64
	EventKind      EventKind
	EventAt        time.Time
	RecordedAt     time.Time
	Backfilled     bool
	Severity       string
	Source         SourceIdentity
	Presentation   Presentation
	Actor          *ActorSnapshot
	Subjects       []SubjectSnapshot
	RecordID       string
	RevisionID     string
	RevisionNo     uint64
	EvidenceID     string
	Corrects       string
	CanonicalHash  [32]byte
}

// SortKeyValue is the envelope's position in the canonical order, so a caller
// paging forward does not have to reassemble the tuple itself and risk using a
// different one than the query ordered by.
func (envelope ActivityEnvelope) SortKeyValue() SortKey {
	return SortKey{
		EventAt:    envelope.EventAt,
		RecordedAt: envelope.RecordedAt,
		SourceKind: envelope.Source.Kind,
		ActivityID: envelope.ActivityID,
	}
}

// ActivityPage is one page of an export read, bound to the snapshot it came from.
type ActivityPage struct {
	Snapshot   ActivitySnapshot
	Envelopes  []ActivityEnvelope
	NextCursor PageCursor
	HasMore    bool
}

// ActivityExportReader is the seam Child 10 consumes to freeze an activity
// archive. Child 7 owns both methods, and the shape is fixed here so building an
// export never requires reaching back into projection internals.
//
// Readiness answers "is this projection complete enough to archive", and
// ScanRecordPage reads only what the actor may see of the selected records at
// exactly the snapshot readiness returned.
type ActivityExportReader interface {
	Readiness(context.Context, recordauth.ActorScope, RecordSelection) (ReadinessVector, error)
	ScanRecordPage(
		context.Context,
		recordauth.ActorScope,
		RecordSelection,
		ActivitySnapshot,
		PageCursor,
	) (ActivityPage, error)
}
