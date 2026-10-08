package subscriptioncosts

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/subscriptions"
)

const DefaultExchangeRateWorkerInterval = 12 * time.Hour

const (
	exchangeRateReconcileInterval = 30 * time.Second
	exchangeRateFetchTimeout      = 10 * time.Second
	exchangeRateMaxAttempts       = 4 // initial attempt plus the three automatic retries
)

var exchangeRateRetryDelays = [...]time.Duration{5 * time.Second, 30 * time.Second, 120 * time.Second}

type ExchangeRateWorker struct {
	service                   *Service
	interval                  time.Duration
	logger                    *slog.Logger
	mu                        sync.Mutex
	states                    map[string]*exchangeRateTaskState
	dirty                     bool
	forceDiscoveryPending     bool
	settingsSignature         string
	observedSettingsSignature string
	settingsRoundPending      bool
	wake                      chan struct{}
}

type exchangeRateTaskState struct {
	pair             ExchangeRatePair
	rateStatus       ExchangeRateStatus
	refreshStatus    ExchangeRateRefreshStatus
	forcePending     bool
	workerDiscovered bool
	lastAttemptAt    *time.Time
	nextRetryAt      *time.Time
	attemptCount     int
	errorSummary     string
}

type exchangeRateRoundTrigger uint8

const (
	exchangeRateRoundReconcile exchangeRateRoundTrigger = iota
	exchangeRateRoundRetry
	exchangeRateRoundNew
)

func NewExchangeRateWorker(service *Service, logger *slog.Logger, interval time.Duration) *ExchangeRateWorker {
	if interval <= 0 {
		interval = DefaultExchangeRateWorkerInterval
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ExchangeRateWorker{
		service:  service,
		interval: interval,
		logger:   logger,
		states:   make(map[string]*exchangeRateTaskState),
		wake:     make(chan struct{}, 1),
	}
}

// RequestRefresh queues a refresh request without blocking the caller. Requests
// received while a pair is being fetched are coalesced into the next worker
// round. A forced request marks each eligible pair for one fresh fetch without
// making an entire ordinary worker round force all pairs.
func (w *ExchangeRateWorker) RequestRefresh(force bool) {
	if w == nil || w.service == nil {
		return
	}
	w.mu.Lock()
	w.dirty = true
	if force {
		w.forceDiscoveryPending = true
	}
	for _, state := range w.states {
		if state.rateStatus == ExchangeRateStatusIdentity || state.refreshStatus == ExchangeRateRefreshStatusRunning {
			continue
		}
		if force {
			state.forcePending = true
			state.refreshStatus = ExchangeRateRefreshStatusQueued
			continue
		}
		if state.rateStatus != ExchangeRateStatusMissing && state.rateStatus != ExchangeRateStatusStale {
			continue
		}
		if state.refreshStatus == ExchangeRateRefreshStatusFailed && state.nextRetryAt == nil {
			continue
		}
		state.refreshStatus = ExchangeRateRefreshStatusQueued
	}
	w.mu.Unlock()
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Status returns the current active-pair snapshot. It only reads settings,
// active subscriptions, and the successful rate cache; it never calls a
// provider or otherwise starts an external request.
func (w *ExchangeRateWorker) Status(ctx context.Context) ([]ExchangeRatePairStatus, error) {
	if w == nil || w.service == nil {
		return []ExchangeRatePairStatus{}, nil
	}
	settings, err := w.service.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	pairs, err := w.service.repo.ListActiveExchangeRatePairs(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("list active exchange rate pairs: %w", err)
	}
	now := w.service.now().UTC()
	signature := exchangeRateSettingsSignature(settings)

	w.mu.Lock()
	settingsChanged := w.observedSettingsSignature != signature
	w.syncActivePairsLocked(pairs, settings, now, false, false)
	if settingsChanged {
		if w.observedSettingsSignature != "" && w.settingsSignature != signature {
			w.settingsRoundPending = true
			for _, state := range w.states {
				if state.rateStatus == ExchangeRateStatusIdentity || state.refreshStatus == ExchangeRateRefreshStatusRunning {
					continue
				}
				if state.rateStatus != ExchangeRateStatusMissing && state.rateStatus != ExchangeRateStatusStale {
					continue
				}
				state.attemptCount = 0
				state.lastAttemptAt = nil
				state.nextRetryAt = nil
				state.errorSummary = ""
				state.refreshStatus = ExchangeRateRefreshStatusQueued
			}
		}
		w.observedSettingsSignature = signature
	}
	if w.forceDiscoveryPending {
		for _, state := range w.states {
			if state.workerDiscovered || state.rateStatus == ExchangeRateStatusIdentity ||
				state.refreshStatus == ExchangeRateRefreshStatusRunning {
				continue
			}
			state.refreshStatus = ExchangeRateRefreshStatusQueued
		}
	}
	status := make([]ExchangeRatePairStatus, 0, len(pairs))
	for _, pair := range pairs {
		key := exchangeRatePairKey(pair)
		state := w.states[key]
		if state == nil {
			continue
		}
		status = append(status, state.status())
	}
	w.mu.Unlock()

	sort.Slice(status, func(i, j int) bool {
		left := status[i]
		right := status[j]
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.BaseCurrency != right.BaseCurrency {
			return left.BaseCurrency < right.BaseCurrency
		}
		return left.QuoteCurrency < right.QuoteCurrency
	})
	return status, nil
}

func (w *ExchangeRateWorker) Run(ctx context.Context) error {
	if w == nil || w.service == nil {
		return nil
	}
	// Startup reconcile always opens a fresh round, so requests queued before
	// Run do not cause a duplicate pass immediately afterward.
	w.takeRefreshRequest()
	if err := w.runRound(ctx, exchangeRateRoundNew); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.logger.Warn("subscription exchange rate startup reconcile failed", "err", err)
	}

	reconcileTicker := time.NewTicker(exchangeRateReconcileInterval)
	defer reconcileTicker.Stop()
	periodicTicker := time.NewTicker(w.interval)
	defer periodicTicker.Stop()

	var retryTimer *time.Timer
	defer func() {
		if retryTimer != nil {
			retryTimer.Stop()
		}
	}()

	for {
		var retryC <-chan time.Time
		if retryTimer != nil {
			retryTimer.Stop()
			retryTimer = nil
		}
		if next, ok := w.nextRetryAt(); ok {
			delay := time.Until(next)
			if delay < 0 {
				delay = 0
			}
			retryTimer = time.NewTimer(delay)
			retryC = retryTimer.C
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.wake:
			w.takeRefreshRequest()
			if err := w.runRound(ctx, exchangeRateRoundReconcile); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.logger.Warn("subscription exchange rate refresh failed", "err", err)
			}
		case <-periodicTicker.C:
			if err := w.runRound(ctx, exchangeRateRoundNew); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.logger.Warn("subscription exchange rate periodic refresh failed", "err", err)
			}
		case <-reconcileTicker.C:
			if err := w.runRound(ctx, exchangeRateRoundReconcile); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.logger.Warn("subscription exchange rate reconcile failed", "err", err)
			}
		case <-retryC:
			if err := w.runRound(ctx, exchangeRateRoundRetry); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				w.logger.Warn("subscription exchange rate retry failed", "err", err)
			}
		}
	}
}

func (w *ExchangeRateWorker) runRound(ctx context.Context, trigger exchangeRateRoundTrigger) error {
	settings, err := w.service.GetSettings(ctx)
	if err != nil {
		return fmt.Errorf("get subscription cost settings: %w", err)
	}
	pairs, err := w.service.repo.ListActiveExchangeRatePairs(ctx, settings)
	if err != nil {
		return fmt.Errorf("list active exchange rate pairs: %w", err)
	}
	now := w.service.now().UTC()
	signature := exchangeRateSettingsSignature(settings)
	w.mu.Lock()
	newRound := trigger == exchangeRateRoundNew || w.settingsRoundPending
	if w.settingsSignature != "" && w.settingsSignature != signature {
		newRound = true
	}
	w.settingsSignature = signature
	w.observedSettingsSignature = signature
	w.settingsRoundPending = false
	w.syncActivePairsLocked(pairs, settings, now, newRound, true)
	w.mu.Unlock()
	fetchTrigger := trigger
	if newRound {
		fetchTrigger = exchangeRateRoundNew
	}

	for _, pair := range pairs {
		if err := ctx.Err(); err != nil {
			return err
		}
		pair = normalizeExchangeRatePair(pair, settings, now)
		key := exchangeRatePairKey(pair)
		if !w.claimPairFetch(key, pair, now, fetchTrigger) {
			continue
		}
		if err := w.fetchPair(ctx, key, pair, settings); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			w.recordFailure(key, pair, err)
		}
	}
	return nil
}

func (w *ExchangeRateWorker) syncActivePairsLocked(pairs []ExchangeRatePair, settings centersettings.SubscriptionCostSettings, now time.Time, newRound, workerDiscovery bool) {
	active := make(map[string]struct{}, len(pairs))
	forceNewPairs := workerDiscovery && w.forceDiscoveryPending
	if workerDiscovery {
		w.forceDiscoveryPending = false
	}
	for _, pair := range pairs {
		pair = normalizeExchangeRatePair(pair, settings, now)
		key := exchangeRatePairKey(pair)
		active[key] = struct{}{}
		state := w.states[key]
		stateWasNew := state == nil
		if state == nil {
			state = &exchangeRateTaskState{refreshStatus: ExchangeRateRefreshStatusIdle}
			w.states[key] = state
		}
		state.pair = pair
		state.rateStatus = pair.RateStatus
		if state.rateStatus == "" {
			state.rateStatus = deriveExchangeRateStatus(pair, settings, now)
		}
		if workerDiscovery && (stateWasNew || !state.workerDiscovered) {
			if forceNewPairs && state.rateStatus != ExchangeRateStatusIdentity {
				state.forcePending = true
			}
			state.workerDiscovered = true
		}
		if stateWasNew && w.dirty && state.rateStatus != ExchangeRateStatusIdentity &&
			(state.rateStatus == ExchangeRateStatusMissing || state.rateStatus == ExchangeRateStatusStale) {
			state.refreshStatus = ExchangeRateRefreshStatusQueued
		}
		if newRound {
			state.attemptCount = 0
			state.lastAttemptAt = nil
			state.nextRetryAt = nil
			state.errorSummary = ""
			state.refreshStatus = ExchangeRateRefreshStatusIdle
		}
		if state.rateStatus == ExchangeRateStatusIdentity {
			state.forcePending = false
			state.refreshStatus = ExchangeRateRefreshStatusIdle
			state.attemptCount = 0
			state.lastAttemptAt = nil
			state.nextRetryAt = nil
			state.errorSummary = ""
		} else if state.forcePending && state.refreshStatus != ExchangeRateRefreshStatusRunning {
			state.refreshStatus = ExchangeRateRefreshStatusQueued
		}
	}
	for key := range w.states {
		if _, ok := active[key]; !ok {
			delete(w.states, key)
		}
	}
}

func normalizeExchangeRatePair(pair ExchangeRatePair, settings centersettings.SubscriptionCostSettings, now time.Time) ExchangeRatePair {
	if strings.TrimSpace(pair.Provider) == "" {
		pair.Provider = settings.ExchangeRateProvider
	}
	pair.Provider = strings.ToLower(strings.TrimSpace(pair.Provider))
	if strings.TrimSpace(pair.BaseCurrency) == "" {
		pair.BaseCurrency = settings.BaseCurrency
	}
	pair.BaseCurrency = strings.ToUpper(strings.TrimSpace(pair.BaseCurrency))
	pair.QuoteCurrency = strings.ToUpper(strings.TrimSpace(pair.QuoteCurrency))
	if pair.RateStatus == "" {
		pair.RateStatus = deriveExchangeRateStatus(pair, settings, now)
	}
	return pair
}

func deriveExchangeRateStatus(pair ExchangeRatePair, settings centersettings.SubscriptionCostSettings, now time.Time) ExchangeRateStatus {
	if pair.QuoteCurrency == pair.BaseCurrency {
		return ExchangeRateStatusIdentity
	}
	if pair.LatestRate == nil {
		return ExchangeRateStatusMissing
	}
	if pair.LatestRate.FetchedAt.Before(now.Add(-time.Duration(settings.ExchangeRateStaleAfterHours) * time.Hour)) {
		return ExchangeRateStatusStale
	}
	return ExchangeRateStatusFresh
}

func (w *ExchangeRateWorker) claimPairFetch(key string, pair ExchangeRatePair, now time.Time, trigger exchangeRateRoundTrigger) bool {
	if pair.QuoteCurrency == pair.BaseCurrency {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.states[key]
	if state == nil || state.refreshStatus == ExchangeRateRefreshStatusRunning {
		return false
	}
	if state.rateStatus == ExchangeRateStatusIdentity {
		state.nextRetryAt = nil
		state.refreshStatus = ExchangeRateRefreshStatusIdle
		return false
	}
	retryDue := state.nextRetryAt != nil && !state.nextRetryAt.After(now)
	if state.forcePending {
		state.attemptCount = 0
		state.lastAttemptAt = nil
		state.nextRetryAt = nil
		state.errorSummary = ""
	} else if !retryDue {
		if state.rateStatus != ExchangeRateStatusMissing && state.rateStatus != ExchangeRateStatusStale {
			return false
		}
		if state.refreshStatus == ExchangeRateRefreshStatusFailed && state.nextRetryAt == nil {
			return false
		}
		if state.nextRetryAt != nil {
			return false
		}
		if trigger == exchangeRateRoundRetry {
			return false
		}
	}
	state.forcePending = false
	state.refreshStatus = ExchangeRateRefreshStatusRunning
	state.nextRetryAt = nil
	state.attemptCount++
	state.lastAttemptAt = timePtr(now)
	return true
}

func (w *ExchangeRateWorker) fetchPair(ctx context.Context, key string, pair ExchangeRatePair, settings centersettings.SubscriptionCostSettings) error {
	providerName := pair.Provider
	if providerName == "" {
		providerName = settings.ExchangeRateProvider
	}
	now := w.service.now().UTC()

	provider := w.service.providers[providerName]
	if provider == nil {
		return fmt.Errorf("exchange rate provider %q is not configured", providerName)
	}
	requestCtx, cancel := context.WithTimeout(ctx, exchangeRateFetchTimeout)
	fetched, err := provider.FetchRate(requestCtx, pair.QuoteCurrency, pair.BaseCurrency)
	cancel()
	if err != nil {
		return err
	}
	if fetched.Rate <= 0 || math.IsNaN(fetched.Rate) || math.IsInf(fetched.Rate, 0) {
		return fmt.Errorf("exchange rate must be a finite positive number")
	}
	if fetched.RateDate.Time.IsZero() {
		fetched.RateDate = subscriptions.NewDate(now)
	}
	if _, err := w.service.repo.UpsertExchangeRate(ctx, ExchangeRateUpsert{
		Provider:      providerName,
		BaseCurrency:  pair.BaseCurrency,
		QuoteCurrency: pair.QuoteCurrency,
		Rate:          fetched.Rate,
		RateDate:      fetched.RateDate,
		FetchedAt:     now,
	}); err != nil {
		return err
	}

	w.mu.Lock()
	if state := w.states[key]; state != nil {
		state.pair.LatestRate = &ExchangeRateRecord{
			Provider:      providerName,
			BaseCurrency:  pair.BaseCurrency,
			QuoteCurrency: pair.QuoteCurrency,
			Rate:          fetched.Rate,
			RateDate:      fetched.RateDate,
			FetchedAt:     now,
		}
		state.rateStatus = ExchangeRateStatusFresh
		state.refreshStatus = ExchangeRateRefreshStatusIdle
		state.attemptCount = 0
		state.lastAttemptAt = timePtr(now)
		state.nextRetryAt = nil
		state.errorSummary = ""
	}
	w.mu.Unlock()
	return nil
}

func (w *ExchangeRateWorker) recordFailure(key string, pair ExchangeRatePair, err error) {
	now := w.service.now().UTC()
	summary := sanitizeProviderError(err)
	w.mu.Lock()
	defer w.mu.Unlock()
	state := w.states[key]
	if state == nil {
		return
	}
	state.pair = pair
	state.errorSummary = summary
	state.lastAttemptAt = timePtr(now)
	if state.attemptCount == 0 {
		state.attemptCount = 1
	}
	if state.attemptCount < exchangeRateMaxAttempts {
		delay := exchangeRateRetryDelays[state.attemptCount-1]
		next := now.Add(delay)
		state.nextRetryAt = timePtr(next)
		state.refreshStatus = ExchangeRateRefreshStatusQueued
		return
	}
	state.nextRetryAt = nil
	state.refreshStatus = ExchangeRateRefreshStatusFailed
}

func (w *ExchangeRateWorker) takeRefreshRequest() {
	w.mu.Lock()
	w.dirty = false
	w.mu.Unlock()
}

func (w *ExchangeRateWorker) nextRetryAt() (time.Time, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var next time.Time
	for _, state := range w.states {
		if state.nextRetryAt == nil {
			continue
		}
		if next.IsZero() || state.nextRetryAt.Before(next) {
			next = *state.nextRetryAt
		}
	}
	return next, !next.IsZero()
}

func (s *exchangeRateTaskState) status() ExchangeRatePairStatus {
	return ExchangeRatePairStatus{
		Provider:      s.pair.Provider,
		BaseCurrency:  s.pair.BaseCurrency,
		QuoteCurrency: s.pair.QuoteCurrency,
		RateStatus:    s.rateStatus,
		RefreshStatus: s.refreshStatus,
		LastAttemptAt: cloneTimePtr(s.lastAttemptAt),
		NextRetryAt:   cloneTimePtr(s.nextRetryAt),
		AttemptCount:  s.attemptCount,
		ErrorSummary:  s.errorSummary,
	}
}

func exchangeRatePairKey(pair ExchangeRatePair) string {
	return strings.ToLower(strings.TrimSpace(pair.Provider)) + ":" +
		strings.ToUpper(strings.TrimSpace(pair.BaseCurrency)) + ":" +
		strings.ToUpper(strings.TrimSpace(pair.QuoteCurrency))
}

func exchangeRateSettingsSignature(settings centersettings.SubscriptionCostSettings) string {
	sum := sha256.Sum256([]byte(
		strings.ToLower(strings.TrimSpace(settings.ExchangeRateProvider)) + "\x00" +
			strings.ToUpper(strings.TrimSpace(settings.BaseCurrency)) + "\x00" +
			settings.FixerAPIKey,
	))
	return fmt.Sprintf("%x", sum)
}

func timePtr(value time.Time) *time.Time {
	value = value.UTC()
	return &value
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := value.UTC()
	return &copy
}
