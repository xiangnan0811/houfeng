package ipquality

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"houfeng/internal/center/ids"
	centersettings "houfeng/internal/center/settings"
)

const IntelligenceWorkerInterval = 15 * time.Second

type intelligenceSettingsReader interface {
	GetSettings(context.Context) (centersettings.CenterSettings, error)
}

type IntelligenceWorker struct {
	repo     IntelligenceRepository
	settings intelligenceSettingsReader
	collect  func(context.Context, IntelligenceClaim, centersettings.IPQualitySettings) IntelligenceSnapshot
	ownerID  string
	logger   *slog.Logger
	interval time.Duration
	wake     chan struct{}
}

func NewIntelligenceWorker(repo IntelligenceRepository, settings intelligenceSettingsReader, logger *slog.Logger) (*IntelligenceWorker, error) {
	owner, err := ids.New("ipqi")
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	collector := NewIntelligenceCollector(repo)
	return &IntelligenceWorker{repo: repo, settings: settings, collect: collector.Collect, ownerID: owner, logger: logger, interval: IntelligenceWorkerInterval, wake: make(chan struct{}, 1)}, nil
}

// Wake coalesces notifications; polling still guarantees recovery after a lost
// signal or process restart. Run remains the single serial claim consumer.
func (w *IntelligenceWorker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *IntelligenceWorker) Run(ctx context.Context) error {
	for {
		for ctx.Err() == nil {
			claim, err := w.repo.ClaimIPQualityIntelligence(ctx, w.ownerID)
			if err != nil {
				if ctx.Err() == nil {
					w.logger.Warn("IP quality intelligence claim failed", "error_code", "claim_failed")
				}
				break
			}
			if claim == nil {
				break
			}
			w.runClaim(ctx, *claim)
		}
		if ctx.Err() != nil {
			return nil
		}
		timer := time.NewTimer(w.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-w.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (w *IntelligenceWorker) runClaim(ctx context.Context, claim IntelligenceClaim) {
	deadline := time.Now().Add(60 * time.Second)
	if claim.DeadlineAt.Before(deadline) {
		deadline = claim.DeadlineAt
	}
	if claim.LeaseUntil.Before(deadline) {
		deadline = claim.LeaseUntil
	}
	jobCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	settings, err := w.settings.GetSettings(jobCtx)
	if err != nil {
		if ctx.Err() == nil {
			w.logger.Warn("IP quality intelligence settings unavailable", "error_code", "settings_unavailable")
		}
		return
	}
	eligible, err := w.repo.IPQualityIntelligenceEligible(jobCtx, claim)
	if err != nil {
		return
	}
	result := IntelligenceSnapshot{State: "skipped"}
	if settings.IPQuality.Enabled && eligible {
		result = w.collect(jobCtx, claim, settings.IPQuality)
	}
	cancel()
	// A shutdown must leave the lease recoverable, not falsely mark success.
	if ctx.Err() != nil {
		return
	}
	// The collection budget may have expired; finalize uses the parent context
	// and the repository independently enforces the DB lease and job deadline.
	if err = w.repo.CompleteIPQualityIntelligence(ctx, claim, result); err != nil && !errors.Is(err, ErrIntelligenceOwnership) && ctx.Err() == nil {
		w.logger.Warn("IP quality intelligence finalize failed", "error_code", "finalize_failed")
	}
}
