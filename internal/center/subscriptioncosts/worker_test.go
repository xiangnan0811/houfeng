package subscriptioncosts

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	centersettings "houfeng/internal/center/settings"
)

type scriptedExchangeRateStep struct {
	started chan<- struct{}
	release <-chan struct{}
	rate    FetchedExchangeRate
	err     error
}

type scriptedExchangeRateProvider struct {
	mu    sync.Mutex
	steps []scriptedExchangeRateStep
	calls int
}

func (p *scriptedExchangeRateProvider) FetchRate(ctx context.Context, _, _ string) (FetchedExchangeRate, error) {
	p.mu.Lock()
	index := p.calls
	p.calls++
	step := p.steps[index]
	p.mu.Unlock()
	if step.started != nil {
		select {
		case step.started <- struct{}{}:
		default:
		}
	}
	if step.release != nil {
		select {
		case <-step.release:
		case <-ctx.Done():
			return FetchedExchangeRate{}, ctx.Err()
		}
	}
	if step.err != nil {
		return FetchedExchangeRate{}, step.err
	}
	if step.rate.Rate == 0 {
		step.rate.Rate = 7.2
	}
	return step.rate, nil
}

func (p *scriptedExchangeRateProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type countingExchangeRateRepository struct {
	*fakeSubscriptionCostRepo
	mu        sync.Mutex
	listed    chan<- struct{}
	listCalls int
}

func (r *countingExchangeRateRepository) ListActiveExchangeRatePairs(ctx context.Context, settings centersettings.SubscriptionCostSettings) ([]ExchangeRatePair, error) {
	r.mu.Lock()
	r.listCalls++
	r.mu.Unlock()
	if r.listed != nil {
		select {
		case r.listed <- struct{}{}:
		default:
		}
	}
	return r.fakeSubscriptionCostRepo.ListActiveExchangeRatePairs(ctx, settings)
}

func (r *countingExchangeRateRepository) listCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listCalls
}

func waitForExchangeRateState(t *testing.T, worker *ExchangeRateWorker, key string, want func(exchangeRateTaskState) bool) exchangeRateTaskState {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		worker.mu.Lock()
		state, ok := worker.states[key]
		var snapshot exchangeRateTaskState
		if ok {
			snapshot = *state
		}
		worker.mu.Unlock()
		if ok && want(snapshot) {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for exchange-rate state %s: %#v", key, snapshot)
		}
		runtime.Gosched()
	}
}

func forceExchangeRateRetryDue(t *testing.T, worker *ExchangeRateWorker, key string) {
	t.Helper()
	worker.mu.Lock()
	state := worker.states[key]
	if state == nil || state.nextRetryAt == nil {
		worker.mu.Unlock()
		t.Fatalf("exchange-rate retry is not scheduled for %s: %#v", key, state)
	}
	due := time.Now().UTC().Add(-time.Millisecond)
	state.nextRetryAt = &due
	worker.mu.Unlock()
	worker.RequestRefresh(false)
}

type countingExchangeRateProvider struct {
	mu            sync.Mutex
	calls         int
	rate          FetchedExchangeRate
	err           error
	started       chan struct{}
	release       chan struct{}
	waitForCancel bool
}

func (p *countingExchangeRateProvider) FetchRate(ctx context.Context, _, _ string) (FetchedExchangeRate, error) {
	p.mu.Lock()
	p.calls++
	if p.started != nil {
		select {
		case p.started <- struct{}{}:
		default:
		}
	}
	p.mu.Unlock()
	if p.release != nil {
		select {
		case <-p.release:
		case <-ctx.Done():
			return FetchedExchangeRate{}, ctx.Err()
		}
	} else if p.waitForCancel {
		<-ctx.Done()
		return FetchedExchangeRate{}, ctx.Err()
	}
	if p.err != nil {
		return FetchedExchangeRate{}, p.err
	}
	return p.rate, nil
}

func (p *countingExchangeRateProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type forceRaceExchangeRateProvider struct {
	mu             sync.Mutex
	calls          map[string]int
	startedB       chan struct{}
	startedASecond chan struct{}
	releaseB       chan struct{}
	releaseASecond chan struct{}
}

type forceRaceRepository struct {
	*fakeSubscriptionCostRepo
	afterBUpsert func()
}
type periodicExchangeRateRepository struct {
	*fakeSubscriptionCostRepo
	listed chan struct{}
}

func (r *periodicExchangeRateRepository) ListActiveExchangeRatePairs(ctx context.Context, settings centersettings.SubscriptionCostSettings) ([]ExchangeRatePair, error) {
	select {
	case r.listed <- struct{}{}:
	default:
	}
	return r.fakeSubscriptionCostRepo.ListActiveExchangeRatePairs(ctx, settings)
}

func (r *forceRaceRepository) UpsertExchangeRate(ctx context.Context, input ExchangeRateUpsert) (ExchangeRateRecord, error) {
	record, err := r.fakeSubscriptionCostRepo.UpsertExchangeRate(ctx, input)
	if err == nil && strings.EqualFold(input.QuoteCurrency, "EUR") && r.afterBUpsert != nil {
		r.afterBUpsert()
	}
	return record, err
}

func (p *forceRaceExchangeRateProvider) FetchRate(_ context.Context, quoteCurrency, _ string) (FetchedExchangeRate, error) {
	quoteCurrency = strings.ToUpper(strings.TrimSpace(quoteCurrency))
	p.mu.Lock()
	if p.calls == nil {
		p.calls = make(map[string]int)
	}
	p.calls[quoteCurrency]++
	call := p.calls[quoteCurrency]
	p.mu.Unlock()

	switch {
	case quoteCurrency == "EUR" && call == 1:
		select {
		case p.startedB <- struct{}{}:
		default:
		}
		<-p.releaseB
	case quoteCurrency == "USD" && call == 2:
		select {
		case p.startedASecond <- struct{}{}:
		default:
		}
		<-p.releaseASecond
	}
	rate := 7.2
	if quoteCurrency == "EUR" {
		rate = 1.08
	}
	return FetchedExchangeRate{Rate: rate}, nil
}

func (p *forceRaceExchangeRateProvider) callCount(quoteCurrency string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[strings.ToUpper(strings.TrimSpace(quoteCurrency))]
}

func waitForWorkerBarrier(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for worker barrier")
	}
}

func TestExchangeRateWorkerForceRefreshesFreshPairAfterOtherPairCompletes(t *testing.T) {
	service, repo := newTestService()
	provider := &forceRaceExchangeRateProvider{
		startedB:       make(chan struct{}, 1),
		startedASecond: make(chan struct{}, 1),
		releaseB:       make(chan struct{}),
		releaseASecond: make(chan struct{}),
	}
	service.providers["frankfurter"] = provider
	repo.pairs = []ExchangeRatePair{
		workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusMissing),
		workerTestPair("frankfurter", "CNY", "EUR", ExchangeRateStatusMissing),
	}
	service.repo = &forceRaceRepository{
		fakeSubscriptionCostRepo: repo,
		afterBUpsert: func() {
			repo.pairs[0].RateStatus = ExchangeRateStatusFresh
			repo.pairs[1].RateStatus = ExchangeRateStatusFresh
		},
	}
	worker := NewExchangeRateWorker(service, nil, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var releaseBOnce, releaseASecondOnce sync.Once
	releaseB := func() { releaseBOnce.Do(func() { close(provider.releaseB) }) }
	releaseASecond := func() { releaseASecondOnce.Do(func() { close(provider.releaseASecond) }) }
	defer releaseB()
	defer releaseASecond()

	result := make(chan error, 1)
	go func() { result <- worker.Run(ctx) }()
	waitForWorkerBarrier(t, provider.startedB)
	worker.RequestRefresh(true)
	worker.RequestRefresh(true)
	releaseB()
	waitForWorkerBarrier(t, provider.startedASecond)
	if repo.pairs[0].RateStatus != ExchangeRateStatusFresh || repo.pairs[1].RateStatus != ExchangeRateStatusFresh {
		t.Fatalf("repository statuses after B completes = %#v, want both fresh", repo.pairs)
	}
	if calls := provider.callCount("USD"); calls != 2 {
		t.Fatalf("USD provider calls while A2 is running = %d, want 2", calls)
	}
	if calls := provider.callCount("EUR"); calls != 1 {
		t.Fatalf("EUR provider calls while B is complete = %d, want 1", calls)
	}
	releaseASecond()
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker error = %v, want context canceled", err)
	}
	if calls := provider.callCount("USD"); calls != 2 {
		t.Fatalf("USD provider calls after worker cancellation = %d, want 2", calls)
	}
	if calls := provider.callCount("EUR"); calls != 1 {
		t.Fatalf("EUR provider calls after worker cancellation = %d, want 1", calls)
	}
	worker.mu.Lock()
	defer worker.mu.Unlock()
	for _, pair := range repo.pairs {
		state := worker.states[exchangeRatePairKey(pair)]
		if state == nil || state.refreshStatus != ExchangeRateRefreshStatusIdle {
			t.Fatalf("final state for %s = %#v, want idle", exchangeRatePairKey(pair), state)
		}
	}
}

func workerTestPair(provider, base, quote string, status ExchangeRateStatus) ExchangeRatePair {
	return ExchangeRatePair{Provider: provider, BaseCurrency: base, QuoteCurrency: quote, RateStatus: status}
}

func TestExchangeRateWorkerStatusQueuesWithoutNetwork(t *testing.T) {
	now := time.Date(2026, time.June, 2, 9, 0, 0, 0, time.UTC)
	service, repo := newTestService()
	service.now = func() time.Time { return now }
	service.providers["frankfurter"] = &countingExchangeRateProvider{}
	repo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusMissing)}
	worker := NewExchangeRateWorker(service, nil, time.Hour)

	before, err := worker.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() before refresh: %v", err)
	}
	if len(before) != 1 || before[0].RateStatus != ExchangeRateStatusMissing || before[0].RefreshStatus != ExchangeRateRefreshStatusIdle {
		t.Fatalf("before status = %#v, want missing/idle", before)
	}
	worker.RequestRefresh(true)
	after, err := worker.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after refresh request: %v", err)
	}
	if len(after) != 1 || after[0].RefreshStatus != ExchangeRateRefreshStatusQueued {
		t.Fatalf("after status = %#v, want queued acceptance", after)
	}
	if calls := service.providers["frankfurter"].(*countingExchangeRateProvider).callCount(); calls != 0 {
		t.Fatalf("provider calls = %d, want no network from RequestRefresh/Status", calls)
	}
}

func TestExchangeRateWorkerRetriesWithRedactedFailure(t *testing.T) {
	now := time.Date(2026, time.June, 2, 9, 0, 0, 0, time.UTC)
	service, repo := newTestService()
	service.now = func() time.Time { return now }
	provider := &countingExchangeRateProvider{err: errors.New("upstream https://data.fixer.io/api/latest?access_key=super-secret-value")}
	service.providers["frankfurter"] = provider
	repo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusMissing)}
	worker := NewExchangeRateWorker(service, nil, time.Hour)

	if err := worker.runRound(context.Background(), exchangeRateRoundNew); err != nil {
		t.Fatalf("initial round: %v", err)
	}
	for range 3 {
		worker.mu.Lock()
		state := worker.states[exchangeRatePairKey(repo.pairs[0])]
		if state == nil || state.nextRetryAt == nil {
			worker.mu.Unlock()
			t.Fatalf("retry did not schedule next attempt: %#v", state)
		}
		past := now.Add(-time.Second)
		state.nextRetryAt = &past
		worker.mu.Unlock()
		if err := worker.runRound(context.Background(), exchangeRateRoundRetry); err != nil {
			t.Fatalf("retry round: %v", err)
		}
	}
	status, err := worker.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after retries: %v", err)
	}
	if len(status) != 1 || status[0].RefreshStatus != ExchangeRateRefreshStatusFailed || status[0].AttemptCount != exchangeRateMaxAttempts {
		t.Fatalf("status after retries = %#v, want failed/%d", status, exchangeRateMaxAttempts)
	}
	if strings.Contains(status[0].ErrorSummary, "super-secret-value") || strings.Contains(status[0].ErrorSummary, "https://") || !strings.Contains(status[0].ErrorSummary, "[provider request redacted]") {
		t.Fatalf("error summary = %q, want redacted provider details", status[0].ErrorSummary)
	}
	if provider.callCount() != exchangeRateMaxAttempts {
		t.Fatalf("provider calls = %d, want initial plus three retries", provider.callCount())
	}
}

func TestExchangeRateWorkerCancellationStopsProviderAndLeavesCache(t *testing.T) {
	service, repo := newTestService()
	service.providers["frankfurter"] = &countingExchangeRateProvider{waitForCancel: true, started: make(chan struct{}, 1)}
	repo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusMissing)}
	worker := NewExchangeRateWorker(service, nil, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- worker.Run(ctx) }()
	<-service.providers["frankfurter"].(*countingExchangeRateProvider).started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled round error = %v, want context canceled", err)
	}
	if len(repo.upserts) != 0 {
		t.Fatalf("upserts after cancellation = %#v, want none", repo.upserts)
	}
}
func TestExchangeRateWorkerFreshPairsSkipStartupPeriodicAndSettingsRounds(t *testing.T) {
	t.Run("startup", func(t *testing.T) {
		service, repo := newTestService()
		provider := &countingExchangeRateProvider{rate: FetchedExchangeRate{Rate: 7.2}}
		service.providers["frankfurter"] = provider
		repo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusFresh)}
		worker := NewExchangeRateWorker(service, nil, time.Hour)
		if err := worker.runRound(context.Background(), exchangeRateRoundNew); err != nil {
			t.Fatalf("startup round: %v", err)
		}
		if calls := provider.callCount(); calls != 0 {
			t.Fatalf("startup provider calls = %d, want 0 for fresh pair", calls)
		}
	})

	t.Run("periodic", func(t *testing.T) {
		service, repo := newTestService()
		provider := &countingExchangeRateProvider{rate: FetchedExchangeRate{Rate: 7.2}}
		service.providers["frankfurter"] = provider
		repo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusFresh)}
		periodicRepo := &periodicExchangeRateRepository{
			fakeSubscriptionCostRepo: repo,
			listed:                   make(chan struct{}, 4),
		}
		service.repo = periodicRepo
		worker := NewExchangeRateWorker(service, nil, time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() { result <- worker.Run(ctx) }()
		waitForWorkerBarrier(t, periodicRepo.listed)
		waitForWorkerBarrier(t, periodicRepo.listed)
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) {
			t.Fatalf("periodic worker error = %v, want context canceled", err)
		}
		if calls := provider.callCount(); calls != 0 {
			t.Fatalf("periodic provider calls = %d, want 0 for fresh pair", calls)
		}
	})

	t.Run("settings signature", func(t *testing.T) {
		service, repo := newTestService()
		provider := &countingExchangeRateProvider{rate: FetchedExchangeRate{Rate: 7.2}}
		service.providers["frankfurter"] = provider
		settingsRepo := service.settingsRepo.(*fakeSettingsRepo)
		repo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusFresh)}
		worker := NewExchangeRateWorker(service, nil, time.Hour)
		if err := worker.runRound(context.Background(), exchangeRateRoundNew); err != nil {
			t.Fatalf("initial settings round: %v", err)
		}
		settingsRepo.settings.SubscriptionCost.FixerAPIKey = "changed-secret"
		if err := worker.runRound(context.Background(), exchangeRateRoundReconcile); err != nil {
			t.Fatalf("credential settings round: %v", err)
		}
		if calls := provider.callCount(); calls != 0 {
			t.Fatalf("settings provider calls = %d, want 0 for fresh pair", calls)
		}
	})
}

func TestExchangeRateWorkerFetchesMissingAndStaleOnOrdinaryRounds(t *testing.T) {
	service, repo := newTestService()
	provider := &countingExchangeRateProvider{rate: FetchedExchangeRate{Rate: 7.2}}
	service.providers["frankfurter"] = provider
	repo.pairs = []ExchangeRatePair{
		workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusMissing),
		workerTestPair("frankfurter", "CNY", "EUR", ExchangeRateStatusStale),
	}
	worker := NewExchangeRateWorker(service, nil, time.Hour)
	if err := worker.runRound(context.Background(), exchangeRateRoundReconcile); err != nil {
		t.Fatalf("ordinary round: %v", err)
	}
	if calls := provider.callCount(); calls != 2 {
		t.Fatalf("ordinary provider calls = %d, want missing and stale pairs", calls)
	}
}
func TestExchangeRateWorkerNewAndSettingsRoundsReopenFailureBudget(t *testing.T) {
	tests := []struct {
		name          string
		status        ExchangeRateStatus
		changeSetting bool
	}{
		{name: "new round missing", status: ExchangeRateStatusMissing},
		{name: "settings round stale", status: ExchangeRateStatusStale, changeSetting: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			service, repo := newTestService()
			provider := &countingExchangeRateProvider{err: errors.New("temporary provider failure")}
			service.providers["frankfurter"] = provider
			settingsRepo := service.settingsRepo.(*fakeSettingsRepo)
			repo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", tt.status)}
			worker := NewExchangeRateWorker(service, nil, time.Hour)
			if err := worker.runRound(context.Background(), exchangeRateRoundReconcile); err != nil {
				t.Fatalf("initial failing round: %v", err)
			}
			worker.mu.Lock()
			state := worker.states[exchangeRatePairKey(repo.pairs[0])]
			if state == nil {
				worker.mu.Unlock()
				t.Fatal("initial failing round did not create task state")
			}
			state.attemptCount = exchangeRateMaxAttempts
			state.refreshStatus = ExchangeRateRefreshStatusFailed
			state.nextRetryAt = nil
			worker.mu.Unlock()

			provider.err = nil
			provider.rate = FetchedExchangeRate{Rate: 7.2}
			trigger := exchangeRateRoundNew
			if tt.changeSetting {
				settingsRepo.settings.SubscriptionCost.FixerAPIKey = "changed-secret"
				trigger = exchangeRateRoundReconcile
			}
			if err := worker.runRound(context.Background(), trigger); err != nil {
				t.Fatalf("budget reopening round: %v", err)
			}
			if calls := provider.callCount(); calls != 2 {
				t.Fatalf("provider calls after budget reopening = %d, want 2", calls)
			}
			worker.mu.Lock()
			state = worker.states[exchangeRatePairKey(repo.pairs[0])]
			defer worker.mu.Unlock()
			if state == nil || state.refreshStatus != ExchangeRateRefreshStatusIdle || state.attemptCount != 0 {
				t.Fatalf("reopened task state = %#v, want idle with reset attempts", state)
			}
		})
	}
}
func TestExchangeRateWorkerRunForcedFreshFailureRetriesToSuccess(t *testing.T) {
	firstStarted := make(chan struct{}, 1)
	firstRelease := make(chan struct{})
	secondStarted := make(chan struct{}, 1)
	secondRelease := make(chan struct{})
	provider := &scriptedExchangeRateProvider{
		steps: []scriptedExchangeRateStep{
			{started: firstStarted, release: firstRelease, err: errors.New("temporary provider failure")},
			{started: secondStarted, release: secondRelease, rate: FetchedExchangeRate{Rate: 7.2}},
		},
	}
	service, baseRepo := newTestService()
	baseRepo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusFresh)}
	listed := make(chan struct{}, 8)
	repo := &countingExchangeRateRepository{
		fakeSubscriptionCostRepo: baseRepo,
		listed:                   listed,
	}
	service.repo = repo
	service.providers["frankfurter"] = provider
	worker := NewExchangeRateWorker(service, nil, time.Hour)
	key := exchangeRatePairKey(baseRepo.pairs[0])
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- worker.Run(ctx) }()
	defer cancel()

	waitForWorkerBarrier(t, listed)
	worker.RequestRefresh(true)
	waitForWorkerBarrier(t, firstStarted)
	close(firstRelease)
	failed := waitForExchangeRateState(t, worker, key, func(state exchangeRateTaskState) bool {
		return state.refreshStatus == ExchangeRateRefreshStatusQueued &&
			state.attemptCount == 1 && state.nextRetryAt != nil
	})
	if failed.lastAttemptAt == nil || !failed.nextRetryAt.After(*failed.lastAttemptAt) {
		t.Fatalf("failed forced refresh retry deadline = %v, last attempt = %v; want backoff", failed.nextRetryAt, failed.lastAttemptAt)
	}

	forceExchangeRateRetryDue(t, worker, key)
	waitForWorkerBarrier(t, secondStarted)
	waitForExchangeRateState(t, worker, key, func(state exchangeRateTaskState) bool {
		return state.refreshStatus == ExchangeRateRefreshStatusRunning && state.attemptCount == 2
	})
	close(secondRelease)
	waitForExchangeRateState(t, worker, key, func(state exchangeRateTaskState) bool {
		return state.refreshStatus == ExchangeRateRefreshStatusIdle &&
			state.rateStatus == ExchangeRateStatusFresh && state.attemptCount == 0
	})

	if got := provider.callCount(); got != 2 {
		t.Fatalf("provider calls = %d, want one forced attempt plus one scheduled retry", got)
	}
	if got := repo.listCallCount(); got > 6 {
		t.Fatalf("repository pair-list calls = %d, want bounded calls after retry success", got)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker error after cancellation = %v, want context canceled", err)
	}
}

func TestExchangeRateWorkerRunForcedFreshFailureExhaustsWithoutBusyLoop(t *testing.T) {
	const attempts = exchangeRateMaxAttempts
	started := make([]chan struct{}, attempts)
	releases := make([]chan struct{}, attempts)
	steps := make([]scriptedExchangeRateStep, attempts)
	for i := range steps {
		started[i] = make(chan struct{}, 1)
		releases[i] = make(chan struct{})
		steps[i] = scriptedExchangeRateStep{
			started: started[i],
			release: releases[i],
			err:     errors.New("temporary provider failure"),
		}
	}
	provider := &scriptedExchangeRateProvider{steps: steps}
	service, baseRepo := newTestService()
	baseRepo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusFresh)}
	listed := make(chan struct{}, attempts+4)
	repo := &countingExchangeRateRepository{
		fakeSubscriptionCostRepo: baseRepo,
		listed:                   listed,
	}
	service.repo = repo
	service.providers["frankfurter"] = provider
	worker := NewExchangeRateWorker(service, nil, time.Hour)
	key := exchangeRatePairKey(baseRepo.pairs[0])
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- worker.Run(ctx) }()
	defer cancel()

	waitForWorkerBarrier(t, listed)
	worker.RequestRefresh(true)
	for attempt := range attempts {
		waitForWorkerBarrier(t, started[attempt])
		close(releases[attempt])
		if attempt == attempts-1 {
			waitForExchangeRateState(t, worker, key, func(state exchangeRateTaskState) bool {
				return state.refreshStatus == ExchangeRateRefreshStatusFailed &&
					state.attemptCount == attempts && state.nextRetryAt == nil
			})
			continue
		}
		waitForExchangeRateState(t, worker, key, func(state exchangeRateTaskState) bool {
			return state.refreshStatus == ExchangeRateRefreshStatusQueued &&
				state.attemptCount == attempt+1 && state.nextRetryAt != nil
		})
		forceExchangeRateRetryDue(t, worker, key)
	}

	if got := provider.callCount(); got != attempts {
		t.Fatalf("provider calls = %d, want exhausted budget %d", got, attempts)
	}
	if got := repo.listCallCount(); got > attempts+3 {
		t.Fatalf("repository pair-list calls = %d, want bounded retry rounds", got)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker error after cancellation = %v, want context canceled", err)
	}
}

func TestExchangeRateWorkerStatusProjectsPendingForcedDiscoveryForFreshPair(t *testing.T) {
	now := time.Date(2026, time.June, 2, 9, 0, 0, 0, time.UTC)
	service, repo := newTestService()
	service.now = func() time.Time { return now }
	provider := &countingExchangeRateProvider{
		rate:    FetchedExchangeRate{Rate: 7.2},
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	service.providers["frankfurter"] = provider
	worker := NewExchangeRateWorker(service, nil, time.Hour)

	if _, err := worker.Status(context.Background()); err != nil {
		t.Fatalf("initial Status() with no pairs: %v", err)
	}
	worker.RequestRefresh(true)
	repo.pairs = []ExchangeRatePair{workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusFresh)}
	beforeWorker, err := worker.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() before worker discovery: %v", err)
	}
	if len(beforeWorker) != 1 || beforeWorker[0].RefreshStatus != ExchangeRateRefreshStatusQueued {
		t.Fatalf("status before worker discovery = %#v, want fresh/queued", beforeWorker)
	}
	if got := provider.callCount(); got != 0 {
		t.Fatalf("provider calls before worker discovery = %d, want 0", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- worker.Run(ctx) }()
	defer cancel()
	waitForWorkerBarrier(t, provider.started)
	running, err := worker.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() while forced discovery is running: %v", err)
	}
	if len(running) != 1 || running[0].RefreshStatus != ExchangeRateRefreshStatusRunning {
		t.Fatalf("status while forced discovery runs = %#v, want running", running)
	}
	close(provider.release)
	key := exchangeRatePairKey(repo.pairs[0])
	waitForExchangeRateState(t, worker, key, func(state exchangeRateTaskState) bool {
		return state.refreshStatus == ExchangeRateRefreshStatusIdle &&
			state.rateStatus == ExchangeRateStatusFresh
	})
	if got := provider.callCount(); got != 1 {
		t.Fatalf("provider calls after forced discovery = %d, want 1", got)
	}
	worker.mu.Lock()
	discoveryPending := worker.forceDiscoveryPending
	worker.mu.Unlock()
	if discoveryPending {
		t.Fatal("worker discovery marker remains pending after worker consumed it")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("worker error after cancellation = %v, want context canceled", err)
	}
}

func TestExchangeRateWorkerSettingsSignatureChangeLeavesFreshIdleAndQueuesEligiblePairs(t *testing.T) {
	service, repo := newTestService()
	provider := &countingExchangeRateProvider{err: errors.New("temporary provider failure")}
	service.providers["frankfurter"] = provider
	settingsRepo := service.settingsRepo.(*fakeSettingsRepo)
	repo.pairs = []ExchangeRatePair{
		workerTestPair("frankfurter", "CNY", "USD", ExchangeRateStatusFresh),
		workerTestPair("frankfurter", "CNY", "EUR", ExchangeRateStatusMissing),
		workerTestPair("frankfurter", "CNY", "GBP", ExchangeRateStatusStale),
	}
	worker := NewExchangeRateWorker(service, nil, time.Hour)
	if err := worker.runRound(context.Background(), exchangeRateRoundNew); err != nil {
		t.Fatalf("initial round: %v", err)
	}
	if got := provider.callCount(); got != 2 {
		t.Fatalf("initial provider calls = %d, want missing and stale pairs only", got)
	}
	settingsRepo.settings.SubscriptionCost.FixerAPIKey = "changed-secret"
	status, err := worker.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() after credential-only settings change: %v", err)
	}
	byQuote := make(map[string]ExchangeRatePairStatus, len(status))
	for _, item := range status {
		byQuote[item.QuoteCurrency] = item
	}
	if got := byQuote["USD"].RefreshStatus; got != ExchangeRateRefreshStatusIdle {
		t.Fatalf("fresh USD status after credential-only change = %q, want idle", got)
	}
	for _, quote := range []string{"EUR", "GBP"} {
		if got := byQuote[quote].RefreshStatus; got != ExchangeRateRefreshStatusQueued {
			t.Fatalf("%s status after credential-only change = %q, want queued", quote, got)
		}
	}
	if got := provider.callCount(); got != 2 {
		t.Fatalf("provider calls from Status() = %d, want unchanged at 2", got)
	}
}
