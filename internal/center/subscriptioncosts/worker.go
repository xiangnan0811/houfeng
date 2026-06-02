package subscriptioncosts

import (
	"context"
	"log/slog"
	"time"
)

const DefaultExchangeRateWorkerInterval = 12 * time.Hour

type ExchangeRateWorker struct {
	service  *Service
	interval time.Duration
	logger   *slog.Logger
}

func NewExchangeRateWorker(service *Service, logger *slog.Logger, interval time.Duration) *ExchangeRateWorker {
	if interval <= 0 {
		interval = DefaultExchangeRateWorkerInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ExchangeRateWorker{service: service, interval: interval, logger: logger}
}

func (w *ExchangeRateWorker) Run(ctx context.Context) error {
	if w.service == nil {
		return nil
	}
	if _, err := w.service.RefreshExchangeRates(ctx); err != nil {
		w.logger.Warn("subscription exchange rate refresh failed", "err", err)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := w.service.RefreshExchangeRates(ctx); err != nil {
				w.logger.Warn("subscription exchange rate refresh failed", "err", err)
			}
		}
	}
}
