package activity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"houfeng/internal/center/records"
)

var (
	// ErrInactiveGeneration means the caller named a generation that is not the
	// one being published into.
	ErrInactiveGeneration = errors.New("activity projection generation is not active")
	// ErrForeignSourceCandidate means an adapter returned an event stamped with
	// another source's kind. Accepting it would let one adapter mint identifiers
	// inside another's namespace and overwrite events it does not own.
	ErrForeignSourceCandidate = errors.New("activity candidate belongs to another source")
	// ErrCandidateOutsideWindow means an adapter returned a row recorded after
	// the bound it was given. Trusting it would let the checkpoint advance over
	// ground the scan never covered consistently.
	ErrCandidateOutsideWindow = errors.New("activity candidate was recorded after the requested window")
	// ErrUndeterminedActivityID means the identifier is not the derivation of the
	// event's own source identity, so a retry or a rebuild would not agree on it.
	ErrUndeterminedActivityID = errors.New("activity candidate id is not derived from its source identity")
	// ErrCandidateHashMismatch means the declared canonical hash does not cover
	// the content. That hash is how publication tells a retry apart from a source
	// changing history, so a wrong one disarms the check.
	ErrCandidateHashMismatch = errors.New("activity candidate hash does not cover its content")
	// ErrUnreachableCandidate means no subject-scoped query could ever return the
	// row, which makes projecting it pure cost.
	ErrUnreachableCandidate = errors.New("activity candidate has no reachable subject")
)

// maxScanPagesPerPass bounds one source's work in one pass. Draining across
// several pages is what lets a first run over all history finish, while the bound
// keeps one busy source from starving the others.
const maxScanPagesPerPass = 64

// PublishOutcome is what one published batch did. Only inserted rows consume
// sequence numbers, so the two counts stay separate.
type PublishOutcome struct {
	Inserted         int
	AlreadyPresent   int
	PublishedThrough uint64
}

// Publisher writes a classified batch into the projection. The projector holds
// this interface rather than the store type so the ordering guarantee stays
// testable without a database, and so this package keeps no dependency on store.
type Publisher interface {
	PublishBatch(ctx context.Context, generation uint64, candidates []CandidateEvent) (PublishOutcome, error)
}

// CheckpointStore persists each source's position.
type CheckpointStore interface {
	LoadCheckpoint(ctx context.Context, generation uint64, kind SourceKind) (SourceCheckpoint, error)
	SaveCheckpoint(ctx context.Context, generation uint64, checkpoint SourceCheckpoint) error
}

// ProjectorOptions configures one projector.
type ProjectorOptions struct {
	Namespace          Namespace
	Adapters           []SourceAdapter
	Checkpoints        CheckpointStore
	Publisher          Publisher
	BatchSize          int
	ReprojectionWindow time.Duration
}

// Projector turns authoritative source events into projected activity. It owns
// ordering and position; adapters only scan and normalize.
type Projector struct {
	namespace   Namespace
	adapters    []SourceAdapter
	checkpoints CheckpointStore
	publisher   Publisher
	batchSize   int
	overlap     time.Duration
}

// SourceOutcome is one source's result in one pass.
type SourceOutcome struct {
	Kind              SourceKind
	Scanned           int
	Inserted          int
	AlreadyPresent    int
	CaughtUp          bool
	Stalled           bool
	CheckpointThrough time.Time
	Err               error
}

// PassReport collects every source's outcome. A pass reports per-source failures
// instead of stopping at the first one, because one unavailable source must not
// freeze the other four.
type PassReport struct {
	Sources []SourceOutcome
}

// Source returns one source's outcome.
func (report PassReport) Source(kind SourceKind) (SourceOutcome, bool) {
	for _, outcome := range report.Sources {
		if outcome.Kind == kind {
			return outcome, true
		}
	}
	return SourceOutcome{}, false
}

// Err aggregates per-source failures so a caller that only wants "did the pass
// fully succeed" does not have to walk the report.
func (report PassReport) Err() error {
	failed := make([]error, 0, len(report.Sources))
	for _, outcome := range report.Sources {
		if outcome.Err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", outcome.Kind, outcome.Err))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return errors.Join(failed...)
}

// CaughtUp reports whether every source has read everything up to its head. It
// is the projector-side precondition for any completeness claim.
func (report PassReport) CaughtUp() bool {
	if len(report.Sources) == 0 {
		return false
	}
	for _, outcome := range report.Sources {
		if !outcome.CaughtUp {
			return false
		}
	}
	return true
}

func NewProjector(options ProjectorOptions) (*Projector, error) {
	if options.Namespace.ProjectID == "" {
		return nil, ErrInvalidNamespace
	}
	if len(options.Adapters) == 0 {
		return nil, errors.New("new activity projector: no source adapters")
	}
	if options.Checkpoints == nil || options.Publisher == nil {
		return nil, errors.New("new activity projector: nil dependency")
	}

	adapters := make([]SourceAdapter, len(options.Adapters))
	copy(adapters, options.Adapters)
	seen := make(map[SourceKind]bool, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			return nil, errors.New("new activity projector: nil source adapter")
		}
		kind := adapter.Kind()
		if !ValidSourceKind(kind) {
			return nil, fmt.Errorf("new activity projector: unknown source kind %q", kind)
		}
		// Two adapters on one source would race for the same identifiers and
		// fight over one checkpoint.
		if seen[kind] {
			return nil, fmt.Errorf("new activity projector: duplicate adapter for %s", kind)
		}
		seen[kind] = true
	}
	// Sorting makes a pass reproducible: an ordering that depended on wiring
	// order would number two otherwise identical runs differently.
	sort.Slice(adapters, func(i, j int) bool { return adapters[i].Kind() < adapters[j].Kind() })

	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultPageSize
	}
	overlap := options.ReprojectionWindow
	if overlap <= 0 {
		overlap = DefaultReprojectionWindow
	}

	return &Projector{
		namespace:   options.Namespace,
		adapters:    adapters,
		checkpoints: options.Checkpoints,
		publisher:   options.Publisher,
		batchSize:   batchSize,
		overlap:     overlap,
	}, nil
}

// ProjectOnce runs one pass over every source.
func (projector *Projector) ProjectOnce(ctx context.Context, generation uint64) (PassReport, error) {
	if ctx == nil {
		return PassReport{}, errors.New("project activity: nil context")
	}
	if generation == 0 {
		return PassReport{}, ErrInactiveGeneration
	}
	report := PassReport{Sources: make([]SourceOutcome, 0, len(projector.adapters))}
	for _, adapter := range projector.adapters {
		report.Sources = append(report.Sources, projector.projectSource(ctx, generation, adapter))
	}
	return report, nil
}

func (projector *Projector) projectSource(ctx context.Context, generation uint64, adapter SourceAdapter) SourceOutcome {
	kind := adapter.Kind()
	outcome := SourceOutcome{Kind: kind}

	checkpoint, err := projector.checkpoints.LoadCheckpoint(ctx, generation, kind)
	if err != nil {
		outcome.Err = fmt.Errorf("load checkpoint: %w", err)
		return outcome
	}
	if checkpoint.Kind == "" {
		checkpoint.Kind = kind
	}
	if checkpoint.Kind != kind {
		outcome.Err = fmt.Errorf("load checkpoint: stored kind %s does not belong to %s", checkpoint.Kind, kind)
		return outcome
	}
	outcome.CheckpointThrough = checkpoint.RecordedThrough

	head, err := adapter.IncrementalHead(ctx)
	if err != nil {
		return projector.recordFailure(ctx, generation, checkpoint, outcome, fmt.Errorf("read head: %w", err))
	}
	if err := checkpoint.ValidateHead(head); err != nil {
		return projector.recordFailure(ctx, generation, checkpoint, outcome, err)
	}

	// Read forward first so the checkpoint keeps moving even when the trailing
	// window is busy.
	frontier, err := projector.drain(ctx, generation, adapter, checkpoint.FrontierWindow(head))
	outcome.Scanned += frontier.scanned
	outcome.Inserted += frontier.inserted
	outcome.AlreadyPresent += frontier.alreadyPresent
	if err != nil {
		return projector.recordFailure(ctx, generation, checkpoint, outcome, err)
	}

	trailingComplete := true
	if window, ok := checkpoint.TrailingWindow(projector.overlap); ok {
		trailing, err := projector.drain(ctx, generation, adapter, window)
		outcome.Scanned += trailing.scanned
		outcome.Inserted += trailing.inserted
		outcome.AlreadyPresent += trailing.alreadyPresent
		if err != nil {
			return projector.recordFailure(ctx, generation, checkpoint, outcome, err)
		}
		trailingComplete = trailing.complete
		outcome.Stalled = outcome.Stalled || trailing.stalled
	}
	outcome.Stalled = outcome.Stalled || frontier.stalled

	next := checkpoint
	next.Attempt = 0
	next.LastErrorCode = ""
	if frontier.complete {
		next.RecordedThrough = head.RecordedThrough
	} else {
		// The forward window still holds rows, so the position may only claim
		// what was actually read. Zero rows read leaves it exactly where it was.
		next.RecordedThrough = laterTime(checkpoint.RecordedThrough, frontier.maxRecordedAt)
	}
	// Caught up has to mean "nothing unread anywhere", including behind the
	// watermark, because an export reads it as a completeness precondition.
	next.CaughtUp = frontier.complete && trailingComplete

	if err := projector.checkpoints.SaveCheckpoint(ctx, generation, next); err != nil {
		outcome.Err = fmt.Errorf("save checkpoint: %w", err)
		return outcome
	}
	outcome.CaughtUp = next.CaughtUp
	outcome.CheckpointThrough = next.RecordedThrough
	return outcome
}

// recordFailure persists the attempt without moving the position. Re-reading a
// window is free; skipping one is not.
func (projector *Projector) recordFailure(
	ctx context.Context,
	generation uint64,
	checkpoint SourceCheckpoint,
	outcome SourceOutcome,
	cause error,
) SourceOutcome {
	outcome.Err = cause
	outcome.CaughtUp = false
	outcome.CheckpointThrough = checkpoint.RecordedThrough

	failed := checkpoint
	failed.CaughtUp = false
	failed.Attempt = checkpoint.Attempt + 1
	failed.LastErrorCode = failureCode(cause)
	if err := projector.checkpoints.SaveCheckpoint(ctx, generation, failed); err != nil {
		outcome.Err = errors.Join(cause, fmt.Errorf("save checkpoint: %w", err))
	}
	return outcome
}

// failureCode reduces a cause to a stable, content-free label. The full error
// goes to the caller's log; the checkpoint keeps only something an operator can
// group by, since it must not carry record text or command output.
func failureCode(cause error) string {
	switch {
	case errors.Is(cause, ErrInvalidSourceHead):
		return "invalid_source_head"
	case errors.Is(cause, ErrCandidateOutsideWindow):
		return "candidate_outside_window"
	case errors.Is(cause, ErrForeignSourceCandidate):
		return "foreign_source_candidate"
	case errors.Is(cause, ErrUndeterminedActivityID):
		return "undetermined_activity_id"
	case errors.Is(cause, ErrCandidateHashMismatch):
		return "candidate_hash_mismatch"
	case errors.Is(cause, ErrUnreachableCandidate):
		return "unreachable_candidate"
	case errors.Is(cause, ErrInvalidPresentation):
		return "invalid_presentation"
	case errors.Is(cause, ErrInvalidSourceIdentity):
		return "invalid_source_identity"
	case errors.Is(cause, ErrInvalidEventKind):
		return "invalid_event_kind"
	case cause != nil:
		return "source_unavailable"
	default:
		return ""
	}
}

type drainResult struct {
	scanned        int
	inserted       int
	alreadyPresent int
	maxRecordedAt  time.Time
	// complete means the window was read to its end.
	complete bool
	// stalled means a full page could not be paged past, because every row in it
	// shares the lower bound's timestamp.
	stalled bool
}

// drain reads one window to its end, publishing page by page. Each page is its
// own publication, so a failure halfway through leaves the earlier pages durably
// projected and simply does not advance the position: the next pass re-reads
// them and publication classifies them as already present.
func (projector *Projector) drain(
	ctx context.Context,
	generation uint64,
	adapter SourceAdapter,
	window ScanWindow,
) (drainResult, error) {
	result := drainResult{}
	from := window.From
	for page := 0; page < maxScanPagesPerPass; page++ {
		candidates, err := adapter.ScanAfter(ctx, ScanWindow{From: from, Through: window.Through}, projector.batchSize)
		if err != nil {
			return result, fmt.Errorf("scan %s: %w", adapter.Kind(), err)
		}
		if len(candidates) == 0 {
			result.complete = true
			return result, nil
		}
		if err := projector.validateCandidates(adapter.Kind(), window, candidates); err != nil {
			return result, err
		}

		outcome, err := projector.publisher.PublishBatch(ctx, generation, candidates)
		if err != nil {
			return result, fmt.Errorf("publish %s: %w", adapter.Kind(), err)
		}
		result.scanned += len(candidates)
		result.inserted += outcome.Inserted
		result.alreadyPresent += outcome.AlreadyPresent

		pageMax := maxRecordedAt(candidates)
		result.maxRecordedAt = laterTime(result.maxRecordedAt, pageMax)

		if len(candidates) < projector.batchSize {
			result.complete = true
			return result, nil
		}
		if !pageMax.After(from) {
			// Every row in a full page sits at or below the lower bound, so
			// moving the bound to pageMax would return the same page forever.
			result.stalled = true
			return result, nil
		}
		// Re-reading the boundary instant is deliberate: rows sharing pageMax may
		// have been cut off by the page limit, and publication is idempotent.
		from = pageMax
	}
	return result, nil
}

func (projector *Projector) validateCandidates(kind SourceKind, window ScanWindow, candidates []CandidateEvent) error {
	for _, candidate := range candidates {
		if err := projector.validateCandidate(kind, window, candidate); err != nil {
			return fmt.Errorf("candidate %s from %s: %w", candidate.Source.EventID, kind, err)
		}
	}
	return nil
}

// validateCandidate treats adapter output as untrusted input. Every check here
// guards something a wrong adapter could otherwise do to rows it does not own.
func (projector *Projector) validateCandidate(kind SourceKind, window ScanWindow, candidate CandidateEvent) error {
	if candidate.Source.Kind != kind {
		return ErrForeignSourceCandidate
	}
	if err := ValidateSourceIdentity(candidate.Source); err != nil {
		return err
	}
	if !ValidEventKind(candidate.EventKind) {
		return ErrInvalidEventKind
	}
	if err := ValidatePresentation(candidate.Presentation); err != nil {
		return err
	}
	if candidate.EventAt.IsZero() || candidate.RecordedAt.IsZero() {
		return ErrInvalidEventTime
	}
	if !window.Through.IsZero() && candidate.RecordedAt.After(window.Through) {
		return ErrCandidateOutsideWindow
	}
	if len(candidate.Subjects) == 0 {
		return ErrUnreachableCandidate
	}
	for _, subject := range candidate.Subjects {
		if !records.ValidSubjectKind(subject.Kind) ||
			!records.ValidRelationRole(subject.Role) ||
			!records.ValidSubjectSourceID(subject.Kind, subject.SourceID) {
			return ErrUnreachableCandidate
		}
	}

	expectedID, err := NewActivityID(projector.namespace, candidate.Source, candidate.EventKind)
	if err != nil {
		return err
	}
	if candidate.ActivityID != expectedID {
		return ErrUndeterminedActivityID
	}
	if candidate.CanonicalHash != candidate.ComputeCanonicalHash() {
		return ErrCandidateHashMismatch
	}
	return nil
}

func maxRecordedAt(candidates []CandidateEvent) time.Time {
	var latest time.Time
	for _, candidate := range candidates {
		latest = laterTime(latest, candidate.RecordedAt.UTC())
	}
	return latest
}

func laterTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}
