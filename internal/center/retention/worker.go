package retention

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type Worker struct {
	repo         Repository
	settingsRepo SettingsRepository
	logger       *slog.Logger
	interval     time.Duration
	now          func() time.Time
	afterPass    func()
}

func NewWorker(repo Repository, settingsRepo SettingsRepository, logger *slog.Logger, interval time.Duration) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = DefaultWorkerInterval
	}
	return &Worker{
		repo:         repo,
		settingsRepo: settingsRepo,
		logger:       logger,
		interval:     interval,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.repo == nil || w.settingsRepo == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		w.runOnce(ctx)
		if w.afterPass != nil {
			w.afterPass()
		}

		timer := time.NewTimer(w.interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) {
	record, err := w.settingsRepo.GetSettings(ctx)
	if err != nil {
		if isContextDone(err) {
			return
		}
		w.logger.Error("load retention settings failed", "error", err)
		return
	}

	policy, err := PolicyFromSettings(record)
	if err != nil {
		w.logger.Error("validate retention settings failed", "error", err)
		return
	}

	result, err := w.repo.ApplyRetention(ctx, policy, w.now().UTC())
	if err != nil {
		if isContextDone(err) {
			return
		}
		w.logger.Error("apply retention failed", "error", err)
		return
	}

	w.logger.Info(
		"retention pass completed",
		"node_aggregate_rows", result.NodeAggregateRows,
		"target_aggregate_rows", result.TargetAggregateRows,
		"deleted_heartbeats", result.DeletedHeartbeats,
		"deleted_host_samples", result.DeletedHostSamples,
		"deleted_probe_observations", result.DeletedProbeObservations,
		"deleted_node_aggregates", result.DeletedNodeAggregates,
		"deleted_target_aggregates", result.DeletedTargetAggregates,
		"deleted_events", result.DeletedEvents,
		"deleted_notifications", result.DeletedNotifications,
	)
}

func isContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
