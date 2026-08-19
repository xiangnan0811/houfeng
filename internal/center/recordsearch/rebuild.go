package recordsearch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"
)

var (
	// ErrInvalidRebuild reports unusable rebuild input.
	ErrInvalidRebuild = errors.New("invalid record search rebuild")
	// ErrRebuildStalled reports a batch that neither advanced its checkpoint nor
	// reported completion. Continuing would hold the building generation forever
	// while making no progress.
	ErrRebuildStalled = errors.New("record search rebuild stalled")
)

const (
	// maxRebuildBatchSize bounds one transaction's worth of projection work so a
	// rebuild never holds row locks across an unbounded scan.
	maxRebuildBatchSize uint32 = 500
	// maxRebuildBatchesPerPass bounds one pass. A rebuild that cannot finish
	// within it resumes from its checkpoint on the next pass, which is also what
	// makes a crash mid-rebuild recoverable.
	maxRebuildBatchesPerPass int = 10_000

	// DefaultRebuildLeaseDuration must outlast one batch transaction, since the
	// lease is renewed per batch rather than on a separate heartbeat.
	DefaultRebuildLeaseDuration = time.Minute
	DefaultRebuildBatchSize     = uint32(200)
	// DefaultRebuildPollInterval only governs how soon a rebuild that became
	// necessary while the center was running is noticed. Startup does not wait for
	// it, so this stays coarse enough that the coverage probe is not a background
	// load of its own.
	DefaultRebuildPollInterval = time.Minute
)

var rebuildOwnerPattern = regexp.MustCompile(`^[a-z0-9_-]{1,128}$`)

// RebuildLease is one claimed rebuild job. ResumeAfter and Projected come from
// the persisted job, so a worker that takes over an expired lease continues the
// previous attempt instead of restarting it.
type RebuildLease struct {
	JobID       string
	Generation  uint64
	ResumeAfter string
	Projected   uint64
}

type RebuildClaim struct {
	OwnerID       string
	LeaseDuration time.Duration
}

type RebuildBatch struct {
	JobID       string
	Generation  uint64
	OwnerID     string
	ResumeAfter string
	Limit       uint32
	// LeaseDuration renews the lease in the same transaction that projects the
	// batch, so a long rebuild cannot lose its claim while it is working.
	LeaseDuration time.Duration
}

type RebuildBatchResult struct {
	Projected   uint64
	ResumeAfter string
	Drained     bool
}

type RebuildPublish struct {
	JobID      string
	Generation uint64
	OwnerID    string
	Projected  uint64
}

// RebuildCoverage is the audit trail a publish leaves: how many documents the
// generation holds and a digest over their identities.
type RebuildCoverage struct {
	DocumentCount  uint64
	CoverageDigest [32]byte
}

type RebuildFailure struct {
	JobID      string
	Generation uint64
	OwnerID    string
	Reason     string
}

type RebuildStore interface {
	RecordSearchRebuildNeeded(context.Context) (bool, error)
	ClaimRecordSearchRebuild(context.Context, RebuildClaim) (RebuildLease, error)
	ProjectRecordSearchRebuildBatch(context.Context, RebuildBatch) (RebuildBatchResult, error)
	PublishRecordSearchRebuild(context.Context, RebuildPublish) (RebuildCoverage, error)
	FailRecordSearchRebuild(context.Context, RebuildFailure) error
}

type RebuildWorkerOptions struct {
	OwnerID            string
	OwnerLeaseDuration time.Duration
	BatchSize          uint32
	PollInterval       time.Duration
	Logger             *slog.Logger
}

func (options RebuildWorkerOptions) validate() error {
	if !rebuildOwnerPattern.MatchString(options.OwnerID) || options.OwnerLeaseDuration <= 0 ||
		options.BatchSize == 0 || options.BatchSize > maxRebuildBatchSize || options.PollInterval <= 0 {
		return ErrInvalidRebuild
	}
	return nil
}

// RebuildWorker builds a complete index generation beside the one serving
// queries, then publishes it. A shadow generation is what lets the index be
// rebuilt without a window where search answers nothing: readers stay on the
// published generation until the new one is complete.
//
// Live commits write the building generation too, under a lock-version fence, so
// a record changed mid-rebuild keeps its newest projection rather than the older
// snapshot this worker replays.
type RebuildWorker struct {
	store   RebuildStore
	options RebuildWorkerOptions
}

func NewRebuildWorker(store RebuildStore, options RebuildWorkerOptions) (*RebuildWorker, error) {
	if nilSearchDependency(store) {
		return nil, fmt.Errorf("%w: store", ErrInvalidRebuild)
	}
	if err := options.validate(); err != nil {
		return nil, err
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &RebuildWorker{store: store, options: options}, nil
}

// RunOnce claims a rebuild when one is needed, drains it in bounded batches, and
// publishes. It reports whether it published a generation.
func (worker *RebuildWorker) RunOnce(ctx context.Context) (bool, error) {
	if ctx == nil || worker == nil || nilSearchDependency(worker.store) {
		return false, ErrInvalidRebuild
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	needed, err := worker.store.RecordSearchRebuildNeeded(ctx)
	if err != nil || !needed {
		return false, err
	}
	lease, err := worker.store.ClaimRecordSearchRebuild(ctx, RebuildClaim{
		OwnerID: worker.options.OwnerID, LeaseDuration: worker.options.OwnerLeaseDuration,
	})
	if err != nil {
		return false, err
	}
	if lease.JobID == "" || lease.Generation == 0 {
		return false, fmt.Errorf("%w: lease", ErrInvalidRebuild)
	}

	projected, err := worker.drain(ctx, lease)
	if err != nil {
		worker.failRebuild(ctx, lease, rebuildFailureReason(err))
		return false, err
	}
	if _, err := worker.store.PublishRecordSearchRebuild(ctx, RebuildPublish{
		JobID: lease.JobID, Generation: lease.Generation,
		OwnerID: worker.options.OwnerID, Projected: projected,
	}); err != nil {
		worker.failRebuild(ctx, lease, rebuildFailureReason(err))
		return false, err
	}
	return true, nil
}

// drain returns the total projected count including whatever an earlier attempt
// already did, because the published coverage describes the generation rather
// than this pass.
func (worker *RebuildWorker) drain(ctx context.Context, lease RebuildLease) (uint64, error) {
	projected := lease.Projected
	resumeAfter := lease.ResumeAfter
	for pass := 0; pass < maxRebuildBatchesPerPass; pass++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		batch, err := worker.store.ProjectRecordSearchRebuildBatch(ctx, RebuildBatch{
			JobID: lease.JobID, Generation: lease.Generation, OwnerID: worker.options.OwnerID,
			ResumeAfter: resumeAfter, Limit: worker.options.BatchSize,
			LeaseDuration: worker.options.OwnerLeaseDuration,
		})
		if err != nil {
			return 0, err
		}
		projected += batch.Projected
		if batch.Drained {
			return projected, nil
		}
		if batch.Projected == 0 || batch.ResumeAfter == "" || batch.ResumeAfter == resumeAfter {
			return 0, ErrRebuildStalled
		}
		resumeAfter = batch.ResumeAfter
	}
	return 0, ErrRebuildStalled
}

// failRebuild releases the building generation so a later pass can start over.
// A failure to record the failure is logged rather than returned: the caller is
// already reporting the original fault, and replacing it would hide the cause.
func (worker *RebuildWorker) failRebuild(ctx context.Context, lease RebuildLease, reason string) {
	if err := worker.store.FailRecordSearchRebuild(ctx, RebuildFailure{
		JobID: lease.JobID, Generation: lease.Generation,
		OwnerID: worker.options.OwnerID, Reason: reason,
	}); err != nil && !errors.Is(err, context.Canceled) {
		worker.options.Logger.Error("record search rebuild job failure was not recorded")
	}
}

func (worker *RebuildWorker) Run(ctx context.Context) error {
	if ctx == nil || worker == nil || nilSearchDependency(worker.store) ||
		worker.options.Logger == nil || worker.options.PollInterval <= 0 {
		return ErrInvalidRebuild
	}
	if ctx.Err() != nil {
		return nil
	}
	ticker := time.NewTicker(worker.options.PollInterval)
	defer ticker.Stop()
	for {
		if _, err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			worker.options.Logger.Error("record search rebuild pass failed")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// rebuildFailureReason keeps the persisted reason inside the schema's
// lowercase-token shape without leaking error text into a stored column.
func rebuildFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrRebuildStalled):
		return "rebuild_stalled"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "rebuild_interrupted"
	default:
		return "rebuild_failed"
	}
}
