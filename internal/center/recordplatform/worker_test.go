package recordplatform

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestOutboxWorkerCommitsClaimBeforeFreshAuthorizationAndSend(t *testing.T) {
	claim := testClaimedOutboxEvent(1)
	repository := &fakeOutboxRepository{claims: []*ClaimedOutboxEventV1{&claim}}
	var calls []string
	worker := NewOutboxWorker(
		repository,
		freshOutboxAuthorizerFunc(func(_ context.Context, event OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
			if !repository.claimCommitted || repository.transactionActive {
				return nil, FreshAuthDecision{}, errors.New("authorization ran before the claim transaction committed")
			}
			if event.RowID != claim.Event.RowID {
				return nil, FreshAuthDecision{}, errors.New("authorizer did not receive durable row identity")
			}
			calls = append(calls, "authorize-and-render")
			return testRenderedDelivery{value: "fresh"}, FreshAuthDecision{Allowed: true, CurrentEpoch: ContentEpoch(event.AuthorizationEpoch)}, nil
		}),
		outboxSenderFunc(func(_ context.Context, delivery RenderedDelivery) error {
			if repository.transactionActive {
				return errors.New("sender ran inside a repository transaction")
			}
			if delivery != (testRenderedDelivery{value: "fresh"}) {
				return errors.New("sender received unexpected rendered delivery")
			}
			calls = append(calls, "send")
			return nil
		}),
		OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
	)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if got, want := repository.calls, []string{"claim", "sent"}; !equalStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
	if got, want := calls, []string{"authorize-and-render", "send"}; !equalStrings(got, want) {
		t.Fatalf("delivery calls = %#v, want %#v", got, want)
	}
	if len(repository.sent) != 1 || repository.sent[0].Owner != claim.Owner {
		t.Fatalf("sent fences = %#v, want claim owner %#v", repository.sent, claim.Owner)
	}
}

func TestOutboxWorkerCancelsDeniedEpochMismatchAndMissingHandlerWithoutSending(t *testing.T) {
	for _, test := range []struct {
		name     string
		decision FreshAuthDecision
		authErr  error
	}{
		{name: "authorization denied", decision: FreshAuthDecision{Allowed: false, CurrentEpoch: 3}},
		{name: "authorization epoch mismatch", decision: FreshAuthDecision{Allowed: true, CurrentEpoch: 4}},
		{name: "missing handler", authErr: ErrOutboxHandlerMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := testClaimedOutboxEvent(1)
			repository := &fakeOutboxRepository{claims: []*ClaimedOutboxEventV1{&claim}}
			authorizeCalls := 0
			sendCalls := 0
			worker := NewOutboxWorker(
				repository,
				freshOutboxAuthorizerFunc(func(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
					authorizeCalls++
					return testRenderedDelivery{value: "must-not-send"}, test.decision, test.authErr
				}),
				outboxSenderFunc(func(context.Context, RenderedDelivery) error {
					sendCalls++
					return nil
				}),
				OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
			)

			if err := worker.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if authorizeCalls != 1 || sendCalls != 0 || len(repository.sent) != 0 || len(repository.retries) != 0 {
				t.Fatalf("attempt state auth=%d send=%d sent=%d retries=%d, want 1,0,0,0", authorizeCalls, sendCalls, len(repository.sent), len(repository.retries))
			}
			if len(repository.cancelled) != 1 || repository.cancelled[0].Owner != claim.Owner {
				t.Fatalf("cancelled fences = %#v, want claim owner %#v", repository.cancelled, claim.Owner)
			}
		})
	}
}

func TestOutboxWorkerRetriesTransientFailureAndReauthorizesOnNextClaim(t *testing.T) {
	firstClaim := testClaimedOutboxEvent(1)
	secondClaim := testClaimedOutboxEvent(2)
	repository := &fakeOutboxRepository{claims: []*ClaimedOutboxEventV1{&firstClaim, &secondClaim}}
	authorizeCalls := 0
	sendCalls := 0
	worker := NewOutboxWorker(
		repository,
		freshOutboxAuthorizerFunc(func(_ context.Context, event OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
			authorizeCalls++
			return testRenderedDelivery{value: event.RowID}, FreshAuthDecision{Allowed: true, CurrentEpoch: ContentEpoch(event.AuthorizationEpoch)}, nil
		}),
		outboxSenderFunc(func(context.Context, RenderedDelivery) error {
			sendCalls++
			if sendCalls == 1 {
				return errors.New("transient sender failure")
			}
			return nil
		}),
		OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
	)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("second RunOnce() error = %v", err)
	}
	if authorizeCalls != 2 || sendCalls != 2 {
		t.Fatalf("attempt counts authorize=%d send=%d, want two each", authorizeCalls, sendCalls)
	}
	if len(repository.retries) != 1 || repository.retries[0].Owner != firstClaim.Owner {
		t.Fatalf("retry fences = %#v, want first claim owner %#v", repository.retries, firstClaim.Owner)
	}
	if len(repository.sent) != 1 || repository.sent[0].Owner != secondClaim.Owner {
		t.Fatalf("sent fences = %#v, want second claim owner %#v", repository.sent, secondClaim.Owner)
	}
}

func TestOutboxWorkerRetriesTransientAuthorizationAndRenderFailure(t *testing.T) {
	for _, test := range []struct {
		name    string
		authErr error
	}{
		{name: "authorization failure", authErr: errors.New("authorization backend unavailable")},
		{name: "render failure", authErr: errors.New("rendering backend unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := testClaimedOutboxEvent(1)
			repository := &fakeOutboxRepository{claims: []*ClaimedOutboxEventV1{&claim}}
			sendCalls := 0
			worker := NewOutboxWorker(
				repository,
				freshOutboxAuthorizerFunc(func(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
					return nil, FreshAuthDecision{}, test.authErr
				}),
				outboxSenderFunc(func(context.Context, RenderedDelivery) error {
					sendCalls++
					return nil
				}),
				OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
			)

			if err := worker.RunOnce(context.Background()); err != nil {
				t.Fatalf("RunOnce() error = %v", err)
			}
			if sendCalls != 0 || len(repository.retries) != 1 || len(repository.cancelled) != 0 {
				t.Fatalf("failure state send=%d retries=%d cancelled=%d, want 0,1,0", sendCalls, len(repository.retries), len(repository.cancelled))
			}
		})
	}
}

func TestOutboxWorkerDoesNotCompensateForLostOwnerLease(t *testing.T) {
	claim := testClaimedOutboxEvent(1)
	repository := &fakeOutboxRepository{
		claims:  []*ClaimedOutboxEventV1{&claim},
		sentErr: ErrLostOwnerLease,
	}
	worker := NewOutboxWorker(
		repository,
		freshOutboxAuthorizerFunc(func(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
			return testRenderedDelivery{value: "fresh"}, FreshAuthDecision{Allowed: true, CurrentEpoch: 3}, nil
		}),
		outboxSenderFunc(func(context.Context, RenderedDelivery) error { return nil }),
		OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
	)

	err := worker.RunOnce(context.Background())
	if !errors.Is(err, ErrLostOwnerLease) {
		t.Fatalf("RunOnce() error = %v, want ErrLostOwnerLease", err)
	}
	if got, want := repository.calls, []string{"claim", "sent"}; !equalStrings(got, want) {
		t.Fatalf("repository calls = %#v, want %#v", got, want)
	}
}

func TestOutboxWorkerRunLogsOnlySafeFixedFailureMessage(t *testing.T) {
	var logs bytes.Buffer
	secret := "drt1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	repository := &fakeOutboxRepository{claimErr: errors.New("dependency failure carries " + secret)}
	worker := NewOutboxWorker(
		repository,
		freshOutboxAuthorizerFunc(func(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
			return nil, FreshAuthDecision{}, nil
		}),
		outboxSenderFunc(func(context.Context, RenderedDelivery) error { return nil }),
		OutboxWorkerOptions{
			OwnerID:            "worker_01",
			OwnerLeaseDuration: time.Minute,
			RetryDelay:         time.Second,
			Logger:             slog.New(slog.NewTextHandler(&logs, nil)),
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := logs.String(); strings.Contains(got, secret) || !strings.Contains(got, "msg=\"record outbox pass failed\"") {
		t.Fatalf("Run() log = %q, want only the fixed safe failure message", got)
	}
}

func TestOutboxWorkerRejectsNilContext(t *testing.T) {
	worker := NewOutboxWorker(
		&fakeOutboxRepository{},
		freshOutboxAuthorizerFunc(func(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
			return nil, FreshAuthDecision{}, nil
		}),
		outboxSenderFunc(func(context.Context, RenderedDelivery) error { return nil }),
		OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
	)
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{name: "run once", run: func() error { return worker.RunOnce(nil) }},
		{name: "run", run: func() error { return worker.Run(nil) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			err, panicked := runOutboxWorker(test.run)
			if panicked != nil {
				t.Fatalf("worker panicked with nil context: %v", panicked)
			}
			if !errors.Is(err, ErrInvalidOutboxWorker) {
				t.Fatalf("worker error = %v, want ErrInvalidOutboxWorker", err)
			}
		})
	}
}

func TestOutboxWorkerRejectsTypedNilDependenciesBeforeDispatch(t *testing.T) {
	tests := []struct {
		name      string
		newWorker func(*fakeOutboxRepository, *int, *int) *OutboxWorker
	}{
		{
			name: "repository",
			newWorker: func(_ *fakeOutboxRepository, authorizeCalls, sendCalls *int) *OutboxWorker {
				var repository *fakeOutboxRepository
				return NewOutboxWorker(
					repository,
					freshOutboxAuthorizerFunc(func(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
						(*authorizeCalls)++
						return testRenderedDelivery{value: "fresh"}, FreshAuthDecision{Allowed: true, CurrentEpoch: 3}, nil
					}),
					outboxSenderFunc(func(context.Context, RenderedDelivery) error {
						(*sendCalls)++
						return nil
					}),
					OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
				)
			},
		},
		{
			name: "authorizer",
			newWorker: func(repository *fakeOutboxRepository, _ *int, sendCalls *int) *OutboxWorker {
				var authorizer *freshOutboxAuthorizerFunc
				return NewOutboxWorker(
					repository,
					authorizer,
					outboxSenderFunc(func(context.Context, RenderedDelivery) error {
						(*sendCalls)++
						return nil
					}),
					OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
				)
			},
		},
		{
			name: "sender",
			newWorker: func(repository *fakeOutboxRepository, authorizeCalls, _ *int) *OutboxWorker {
				var sender *outboxSenderFunc
				return NewOutboxWorker(
					repository,
					freshOutboxAuthorizerFunc(func(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
						(*authorizeCalls)++
						return testRenderedDelivery{value: "fresh"}, FreshAuthDecision{Allowed: true, CurrentEpoch: 3}, nil
					}),
					sender,
					OutboxWorkerOptions{OwnerID: "worker_01", OwnerLeaseDuration: time.Minute, RetryDelay: time.Second},
				)
			},
		},
	}
	operations := []struct {
		name string
		run  func(*OutboxWorker) error
	}{
		{name: "run once", run: func(worker *OutboxWorker) error { return worker.RunOnce(context.Background()) }},
		{
			name: "run",
			run: func(worker *OutboxWorker) error {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return worker.Run(ctx)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, operation := range operations {
				t.Run(operation.name, func(t *testing.T) {
					claim := testClaimedOutboxEvent(1)
					repository := &fakeOutboxRepository{claims: []*ClaimedOutboxEventV1{&claim}}
					authorizeCalls := 0
					sendCalls := 0
					worker := test.newWorker(repository, &authorizeCalls, &sendCalls)

					err, panicked := runOutboxWorker(func() error { return operation.run(worker) })
					if panicked != nil {
						t.Fatalf("worker panicked: %v", panicked)
					}
					if !errors.Is(err, ErrInvalidOutboxWorker) {
						t.Fatalf("worker error = %v, want ErrInvalidOutboxWorker", err)
					}
					if len(repository.calls) != 0 || authorizeCalls != 0 || sendCalls != 0 {
						t.Fatalf("dependency calls repository=%#v authorize=%d send=%d, want none", repository.calls, authorizeCalls, sendCalls)
					}
				})
			}
		})
	}
}

func runOutboxWorker(run func() error) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return run(), nil
}

type fakeOutboxRepository struct {
	claims            []*ClaimedOutboxEventV1
	calls             []string
	claimCommitted    bool
	transactionActive bool
	cancelled         []ClaimedOutboxEventV1
	retries           []ClaimedOutboxEventV1
	sent              []ClaimedOutboxEventV1
	cancelErr         error
	retryErr          error
	sentErr           error
	claimErr          error
}

func (repository *fakeOutboxRepository) ClaimOutbox(context.Context, OutboxClaimInputV1) (*ClaimedOutboxEventV1, error) {
	repository.transactionActive = true
	repository.calls = append(repository.calls, "claim")
	repository.transactionActive = false
	repository.claimCommitted = true
	if repository.claimErr != nil {
		return nil, repository.claimErr
	}
	if len(repository.claims) == 0 {
		return nil, nil
	}
	claim := repository.claims[0]
	repository.claims = repository.claims[1:]
	return claim, nil
}

func (repository *fakeOutboxRepository) CancelOutbox(_ context.Context, claim ClaimedOutboxEventV1) error {
	repository.calls = append(repository.calls, "cancel")
	repository.cancelled = append(repository.cancelled, claim)
	return repository.cancelErr
}

func (repository *fakeOutboxRepository) RetryOutbox(_ context.Context, claim ClaimedOutboxEventV1, _ time.Duration) error {
	repository.calls = append(repository.calls, "retry")
	repository.retries = append(repository.retries, claim)
	return repository.retryErr
}

func (repository *fakeOutboxRepository) MarkOutboxSent(_ context.Context, claim ClaimedOutboxEventV1) error {
	repository.calls = append(repository.calls, "sent")
	repository.sent = append(repository.sent, claim)
	return repository.sentErr
}

type freshOutboxAuthorizerFunc func(context.Context, OutboxEvent) (RenderedDelivery, FreshAuthDecision, error)

func (authorizer freshOutboxAuthorizerFunc) AuthorizeAndRender(ctx context.Context, event OutboxEvent) (RenderedDelivery, FreshAuthDecision, error) {
	return authorizer(ctx, event)
}

type outboxSenderFunc func(context.Context, RenderedDelivery) error

func (sender outboxSenderFunc) SendOutbox(ctx context.Context, delivery RenderedDelivery) error {
	return sender(ctx, delivery)
}

type testRenderedDelivery struct {
	value any
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func testClaimedOutboxEvent(generation uint64) ClaimedOutboxEventV1 {
	return ClaimedOutboxEventV1{
		Event: OutboxEvent{
			RowID:              42,
			ProjectID:          string(ProjectIDDefault),
			EventKind:          string(OutboxEventKindRecordCreated),
			SubjectKind:        string(OutboxSubjectKindRecord),
			SubjectID:          "rec_01",
			AuthorizationEpoch: 3,
		},
		Owner: OwnerLease{
			OwnerID:    "worker_01",
			Generation: generation,
			ExpiresAt:  time.Date(2026, time.July, 24, 13, 0, 0, 0, time.UTC),
		},
		ExpiresAt: time.Date(2026, time.July, 24, 14, 0, 0, 0, time.UTC),
	}
}
