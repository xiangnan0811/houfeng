package activity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"
)

// DefaultWorkerInterval is how often a pass runs when nothing wakes it sooner.
//
// A minute rather than seconds: every pass re-reads a trailing window on each
// source, so a short interval mostly re-reads rows it has already published.
const DefaultWorkerInterval = time.Minute

// DefaultSourceLeaseTTL bounds how long a dead worker can hold a source.
//
// It is several passes long on purpose. A lease that expired while its holder was
// mid-pass would let a second worker start scanning the same window, which is the
// waste the lease exists to prevent.
const DefaultSourceLeaseTTL = 5 * time.Minute

// SourceLeases hands out the right to project one source at a time.
//
// This is a scheduling contract, not a safety one. Concurrent projection of one
// source is already correct: allocation is contiguous under the head lock,
// publication is keyed on source identity, and a checkpoint refuses to move
// backwards. The lease exists so two processes do not spend their passes scanning
// the same window and contending on the same head row.
type SourceLeases interface {
	AcquireSourceLease(ctx context.Context, generation uint64, kind SourceKind, ownerID string, ttl time.Duration) (bool, error)
	ReleaseSourceLease(ctx context.Context, generation uint64, kind SourceKind, ownerID string) error
}

// GenerationStore reports which generation is currently being published into.
type GenerationStore interface {
	ActiveGeneration(ctx context.Context) (uint64, error)
}

var ownerIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,128}$`)

// ValidOwnerID matches the shape the platform's lease columns accept. Owner ids
// end up in SQL predicates and log lines, so the shape is constrained rather
// than trusted.
func ValidOwnerID(value string) bool {
	return ownerIDPattern.MatchString(value)
}

// WorkerOptions configures the projection worker.
type WorkerOptions struct {
	Projector   *Projector
	Leases      SourceLeases
	Generations GenerationStore
	// OwnerID identifies this process in the lease. It must be stable for the
	// lifetime of the process and distinct per process, so a restarted worker
	// does not silently inherit its own expired lease mid-pass.
	OwnerID  string
	Logger   *slog.Logger
	Interval time.Duration
	LeaseTTL time.Duration
}

// Worker runs the projector on a schedule, one source at a time, holding a lease
// on each source while it works on it.
type Worker struct {
	projector   *Projector
	leases      SourceLeases
	generations GenerationStore
	ownerID     string
	logger      *slog.Logger
	interval    time.Duration
	leaseTTL    time.Duration
}

func NewWorker(options WorkerOptions) (*Worker, error) {
	if options.Projector == nil {
		return nil, errors.New("new activity worker: nil projector")
	}
	if options.Leases == nil {
		return nil, errors.New("new activity worker: nil leases")
	}
	if options.Generations == nil {
		return nil, errors.New("new activity worker: nil generations")
	}
	if !ValidOwnerID(options.OwnerID) {
		return nil, fmt.Errorf("new activity worker: invalid owner id %q", options.OwnerID)
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := options.Interval
	if interval <= 0 {
		interval = DefaultWorkerInterval
	}
	leaseTTL := options.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = DefaultSourceLeaseTTL
	}
	// A lease shorter than the interval would routinely expire between passes and
	// let another worker take a source this one is about to work on again.
	if leaseTTL <= interval {
		return nil, fmt.Errorf("new activity worker: lease ttl %s must outlast the interval %s", leaseTTL, interval)
	}
	return &Worker{
		projector:   options.Projector,
		leases:      options.Leases,
		generations: options.Generations,
		ownerID:     options.OwnerID,
		logger:      logger,
		interval:    interval,
		leaseTTL:    leaseTTL,
	}, nil
}

// Run projects until the context is cancelled.
//
// A failing pass is logged and retried on the next tick rather than returned: a
// source that cannot be read right now is a reason to fall behind, not a reason
// to take the center down.
func (worker *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()

	worker.runPass(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			worker.runPass(ctx)
		}
	}
}

// runPass projects every source this worker can take a lease on.
func (worker *Worker) runPass(ctx context.Context) {
	generation, err := worker.generations.ActiveGeneration(ctx)
	if err != nil {
		if isContextDone(ctx) {
			return
		}
		worker.logger.Error("activity projection: read generation", "error", err)
		return
	}
	// No active generation means nothing has been published yet. Waiting is the
	// honest response; picking a generation would create a projection nobody reads.
	if generation == 0 {
		return
	}

	for _, kind := range worker.projector.SourceKinds() {
		if isContextDone(ctx) {
			return
		}
		worker.projectOneSource(ctx, generation, kind)
	}
}

func (worker *Worker) projectOneSource(ctx context.Context, generation uint64, kind SourceKind) {
	acquired, err := worker.leases.AcquireSourceLease(ctx, generation, kind, worker.ownerID, worker.leaseTTL)
	if err != nil {
		if isContextDone(ctx) {
			return
		}
		worker.logger.Error("activity projection: acquire lease", "source", string(kind), "error", err)
		return
	}
	// Another worker owns it. That is the normal outcome with more than one
	// process, and not worth logging on every tick.
	if !acquired {
		return
	}
	defer func() {
		// Released even on failure, so a source that errored is retried by whichever
		// worker gets there first rather than parked until the lease expires. The
		// release runs on a context that may already be cancelled, so it gets its
		// own short-lived one.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := worker.leases.ReleaseSourceLease(releaseCtx, generation, kind, worker.ownerID); err != nil {
			worker.logger.Error("activity projection: release lease", "source", string(kind), "error", err)
		}
	}()

	outcome, err := worker.projector.ProjectSource(ctx, generation, kind)
	if err != nil {
		if isContextDone(ctx) {
			return
		}
		worker.logger.Error("activity projection: project source", "source", string(kind), "error", err)
		return
	}
	if outcome.Err != nil {
		if isContextDone(ctx) {
			return
		}
		// The checkpoint already carries the reason code; this is the operator-facing
		// copy of it.
		worker.logger.Error("activity projection: source pass failed",
			"source", string(kind), "error", outcome.Err)
		return
	}
	if outcome.Inserted > 0 {
		worker.logger.Info("activity projection",
			"source", string(kind),
			"inserted", outcome.Inserted,
			"already_present", outcome.AlreadyPresent,
			"caught_up", outcome.CaughtUp,
		)
	}
}

func isContextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
