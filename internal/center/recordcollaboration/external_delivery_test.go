package recordcollaboration

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/notify"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

const (
	externalDeliveryTestUserID       = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
	externalDeliveryTestRecordID     = "rec_aaaaaaaaaaaaaaaaaaaaaaaa"
	externalDeliveryTestNotification = "rnt_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

type externalDeliveryTestProvider struct{}

func (externalDeliveryTestProvider) SendExternalDelivery(context.Context, SafeExternalDelivery) ExternalDeliveryProviderOutcome {
	return ExternalDeliveryProviderSent
}

type recordingExternalDeliveryProvider struct {
	outcome  ExternalDeliveryProviderOutcome
	messages []SafeExternalDelivery
}

func (provider *recordingExternalDeliveryProvider) SendExternalDelivery(_ context.Context, delivery SafeExternalDelivery) ExternalDeliveryProviderOutcome {
	provider.messages = append(provider.messages, delivery)
	return provider.outcome
}

type deadlineRecordingExternalDeliveryProvider struct {
	deadline time.Time
	outcome  ExternalDeliveryProviderOutcome
}

func (provider *deadlineRecordingExternalDeliveryProvider) SendExternalDelivery(ctx context.Context, _ SafeExternalDelivery) ExternalDeliveryProviderOutcome {
	provider.deadline, _ = ctx.Deadline()
	return provider.outcome
}

func TestScopedTransportBindingRequiresExactDefaultProjectUserChannelAndBinding(t *testing.T) {
	valid := ScopedTransportBinding{
		ProjectID: recordauth.ProjectIDDefault, RecipientUserID: externalDeliveryTestUserID,
		Channel: NotificationDeliveryTelegram, BindingID: "user_telegram_primary",
		Provider: externalDeliveryTestProvider{},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ScopedTransportBinding)
	}{
		{name: "unknown project", mutate: func(value *ScopedTransportBinding) { value.ProjectID = "other" }},
		{name: "malformed user", mutate: func(value *ScopedTransportBinding) { value.RecipientUserID = "admin" }},
		{name: "unknown channel", mutate: func(value *ScopedTransportBinding) { value.Channel = "email" }},
		{name: "malformed binding", mutate: func(value *ScopedTransportBinding) { value.BindingID = "telegram/default" }},
		{name: "missing provider", mutate: func(value *ScopedTransportBinding) { value.Provider = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if err := candidate.Validate(); err != ErrInvalidScopedTransportBinding {
				t.Fatalf("Validate() error = %v, want ErrInvalidScopedTransportBinding", err)
			}
		})
	}
}

func TestRenderSafeExternalDeliveryHasClosedContentFreeShape(t *testing.T) {
	delivery, err := RenderSafeExternalDelivery(
		"https://houfeng.example/",
		externalDeliveryTestRecordID,
		externalDeliveryTestNotification,
	)
	if err != nil {
		t.Fatalf("RenderSafeExternalDelivery() error = %v", err)
	}
	if delivery.Summary != "A Houfeng Record collaboration update is available." {
		t.Fatalf("Summary = %q, want fixed safe summary", delivery.Summary)
	}
	if delivery.Link != "https://houfeng.example/records/rec_aaaaaaaaaaaaaaaaaaaaaaaa?notification=rnt_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Fatalf("Link = %q, want canonical deep link", delivery.Link)
	}
	typeOfDelivery := reflect.TypeOf(delivery)
	if typeOfDelivery.NumField() != 2 || typeOfDelivery.Field(0).Name != "Summary" || typeOfDelivery.Field(1).Name != "Link" {
		t.Fatalf("SafeExternalDelivery fields = %#v, want only Summary and Link", typeOfDelivery)
	}

	for _, invalid := range []string{
		"http://houfeng.example", "https://user:secret@houfeng.example", "https://houfeng.example/base",
		"https://houfeng.example/?token=secret", "https://houfeng.example/#fragment",
	} {
		if _, err := RenderSafeExternalDelivery(invalid, externalDeliveryTestRecordID, externalDeliveryTestNotification); err != ErrInvalidSafeExternalDelivery {
			t.Errorf("RenderSafeExternalDelivery(%q) error = %v, want ErrInvalidSafeExternalDelivery", invalid, err)
		}
	}
}

func TestSafeExternalDeliveryRejectsNonCanonicalOrOversizedLinks(t *testing.T) {
	canonical, err := RenderSafeExternalDelivery("https://houfeng.example", externalDeliveryTestRecordID, externalDeliveryTestNotification)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		link string
	}{
		{name: "duplicate notification", link: canonical.Link + "&notification=" + externalDeliveryTestNotification},
		{name: "escaped path", link: strings.Replace(canonical.Link, "/records/rec_", "/records/%72ec_", 1)},
		{name: "escaped query", link: strings.Replace(canonical.Link, "notification=rnt_", "notification=%72nt_", 1)},
		{name: "oversized", link: "https://" + strings.Repeat("a", 2048) + "/records/" + externalDeliveryTestRecordID + "?notification=" + externalDeliveryTestNotification},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := SafeExternalDelivery{Summary: canonical.Summary, Link: test.link}
			if err := candidate.Validate(); !errors.Is(err, ErrInvalidSafeExternalDelivery) {
				t.Fatalf("Validate(%s) error = %v, want ErrInvalidSafeExternalDelivery", test.name, err)
			}
		})
	}
}

func TestExternalDeliveryRetryDelayIsDeterministicAndAttemptBounded(t *testing.T) {
	base := 5 * time.Second
	for attempt := uint8(1); attempt < MaxNotificationDeliveryAttempts; attempt++ {
		got, retry := ExternalDeliveryRetryDelay(base, attempt)
		want := base * time.Duration(uint64(1)<<(attempt-1))
		if !retry || got != want {
			t.Fatalf("ExternalDeliveryRetryDelay(%d) = (%s,%t), want (%s,true)", attempt, got, retry, want)
		}
	}
	if got, retry := ExternalDeliveryRetryDelay(base, MaxNotificationDeliveryAttempts); retry || got != 0 {
		t.Fatalf("ExternalDeliveryRetryDelay(max) = (%s,%t), want (0,false)", got, retry)
	}
	for _, invalid := range uint8Slice(0, MaxNotificationDeliveryAttempts+1) {
		if got, retry := ExternalDeliveryRetryDelay(base, invalid); retry || got != 0 {
			t.Errorf("ExternalDeliveryRetryDelay(%d) = (%s,%t), want (0,false)", invalid, got, retry)
		}
	}
}

func TestScopedExternalDeliveryProcessorRejectsRetryBackoffThatOverflowsBeforeAttemptLimit(t *testing.T) {
	processor, err := NewScopedExternalDeliveryProcessor(&externalDeliveryStoreStub{}, ScopedExternalDeliveryProcessorOptions{
		PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Duration(math.MaxInt64),
		SendTimeout: time.Second, Clock: fixedExternalDeliveryClock{now: time.Now().UTC()},
	})
	if processor != nil || !errors.Is(err, ErrInvalidExternalDeliveryProcessor) {
		t.Fatalf("NewScopedExternalDeliveryProcessor(overflow) = (%#v, %v), want (nil, ErrInvalidExternalDeliveryProcessor)", processor, err)
	}
}

func TestScopedExternalDeliveryProcessorRequiresBoundedSendTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, MaxExternalDeliverySendTimeout + time.Nanosecond} {
		processor, err := NewScopedExternalDeliveryProcessor(&externalDeliveryStoreStub{}, ScopedExternalDeliveryProcessorOptions{
			PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Second,
			SendTimeout: timeout, Clock: fixedExternalDeliveryClock{now: time.Now().UTC()},
		})
		if processor != nil || !errors.Is(err, ErrInvalidExternalDeliveryProcessor) {
			t.Fatalf("NewScopedExternalDeliveryProcessor(timeout=%s) = (%#v, %v), want invalid", timeout, processor, err)
		}
	}
}

func TestScopedExternalDeliveryProcessorSendsOnlyPreparedSafeMessageAndFinalizes(t *testing.T) {
	startedAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	claim := externalDeliveryTestClaim(startedAt.Add(time.Minute))
	provider := &recordingExternalDeliveryProvider{outcome: ExternalDeliveryProviderSent}
	attempt := ExternalDeliveryAttempt{
		DeliveryID: claim.Event.SubjectID, RecordID: externalDeliveryTestRecordID,
		NotificationID: externalDeliveryTestNotification, RecipientUserID: externalDeliveryTestUserID,
		Channel: NotificationDeliveryTelegram, BindingID: "telegram_primary",
		SourceVersion: 7, AuthorizationEpoch: 11, RecordFenceEpoch: 13,
		Attempt: 1, StartedAt: startedAt,
	}
	message, err := RenderSafeExternalDelivery("https://houfeng.example", attempt.RecordID, attempt.NotificationID)
	if err != nil {
		t.Fatal(err)
	}
	store := &externalDeliveryStoreStub{
		preparation: ExternalDeliveryPreparation{Prepared: &PreparedExternalDelivery{
			Attempt: attempt,
			Binding: ScopedTransportBinding{
				ProjectID: recordauth.ProjectIDDefault, RecipientUserID: attempt.RecipientUserID,
				Channel: attempt.Channel, BindingID: attempt.BindingID, Provider: provider,
			},
			Message: message,
		}},
		finalResult: ExternalDeliveryOutboxResult{Disposition: ExternalDeliveryOutboxComplete},
	}
	processor, err := NewScopedExternalDeliveryProcessor(store, ScopedExternalDeliveryProcessorOptions{
		PublicBaseURL: "https://houfeng.example", RetryBaseDelay: 5 * time.Second,
		SendTimeout: 10 * time.Second, Clock: fixedExternalDeliveryClock{now: startedAt},
	})
	if err != nil {
		t.Fatalf("NewScopedExternalDeliveryProcessor() error = %v", err)
	}
	result, err := processor.ProcessExternalDelivery(context.Background(), claim)
	if err != nil || result != store.finalResult {
		t.Fatalf("ProcessExternalDelivery() = (%#v, %v)", result, err)
	}
	if !reflect.DeepEqual(store.steps, []string{"prepare", "finalize"}) || len(provider.messages) != 1 || provider.messages[0] != message || store.finalOutcome != ExternalDeliveryProviderSent {
		t.Fatalf("steps=%#v messages=%#v outcome=%q", store.steps, provider.messages, store.finalOutcome)
	}
}

func TestScopedExternalDeliveryProcessorNeverSendsCancelledOrUnknownPreparation(t *testing.T) {
	claim := externalDeliveryTestClaim(time.Date(2026, 8, 17, 12, 1, 0, 0, time.UTC))
	for _, disposition := range []ExternalDeliveryOutboxDisposition{ExternalDeliveryOutboxCancel, ExternalDeliveryOutboxComplete} {
		store := &externalDeliveryStoreStub{preparation: ExternalDeliveryPreparation{
			Result: ExternalDeliveryOutboxResult{Disposition: disposition},
		}}
		processor, err := NewScopedExternalDeliveryProcessor(store, ScopedExternalDeliveryProcessorOptions{
			PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Second,
			SendTimeout: time.Second, Clock: fixedExternalDeliveryClock{now: claim.Owner.ExpiresAt.Add(-time.Second)},
		})
		if err != nil {
			t.Fatal(err)
		}
		result, err := processor.ProcessExternalDelivery(context.Background(), claim)
		if err != nil || result.Disposition != disposition || !reflect.DeepEqual(store.steps, []string{"prepare"}) {
			t.Fatalf("disposition %q result/error/steps = %#v/%v/%#v", disposition, result, err, store.steps)
		}
	}
}

func TestScopedExternalDeliveryProcessorTreatsInvalidProviderOutcomeAsUnknown(t *testing.T) {
	startedAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	claim := externalDeliveryTestClaim(startedAt.Add(time.Minute))
	provider := &recordingExternalDeliveryProvider{outcome: "provider_body: secret"}
	store := externalDeliveryPreparedStore(t, claim, startedAt, provider)
	store.finalResult = ExternalDeliveryOutboxResult{Disposition: ExternalDeliveryOutboxComplete}
	processor, err := NewScopedExternalDeliveryProcessor(store, ScopedExternalDeliveryProcessorOptions{
		PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Second,
		SendTimeout: 10 * time.Second, Clock: fixedExternalDeliveryClock{now: startedAt},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := processor.ProcessExternalDelivery(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if store.finalOutcome != ExternalDeliveryProviderUnknownOutcome {
		t.Fatalf("final outcome = %q, want unknown_outcome", store.finalOutcome)
	}
}

func TestScopedExternalDeliveryProcessorStopsBeforeNetworkWhenClaimIsLocallyStale(t *testing.T) {
	startedAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	claim := externalDeliveryTestClaim(startedAt.Add(time.Minute))
	provider := &recordingExternalDeliveryProvider{outcome: ExternalDeliveryProviderSent}
	store := externalDeliveryPreparedStore(t, claim, startedAt, provider)
	processor, err := NewScopedExternalDeliveryProcessor(store, ScopedExternalDeliveryProcessorOptions{
		PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Second,
		SendTimeout: 10 * time.Second, Clock: fixedExternalDeliveryClock{now: claim.Owner.ExpiresAt},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := processor.ProcessExternalDelivery(context.Background(), claim); !errors.Is(err, recordplatform.ErrLeaseRenewalStopped) {
		t.Fatalf("ProcessExternalDelivery(stale) error = %v, want ErrLeaseRenewalStopped", err)
	}
	if len(provider.messages) != 0 || !reflect.DeepEqual(store.steps, []string{"prepare"}) {
		t.Fatalf("stale provider messages/steps = %#v/%#v", provider.messages, store.steps)
	}
}

func TestScopedExternalDeliveryProcessorBoundsSendDeadlineBeforeOwnerLeaseExpiry(t *testing.T) {
	now := time.Now().UTC()
	claim := externalDeliveryTestClaim(now.Add(5 * time.Second))
	provider := &deadlineRecordingExternalDeliveryProvider{outcome: ExternalDeliveryProviderSent}
	store := externalDeliveryPreparedStore(t, claim, now, provider)
	store.finalResult = ExternalDeliveryOutboxResult{Disposition: ExternalDeliveryOutboxComplete}
	processor, err := NewScopedExternalDeliveryProcessor(store, ScopedExternalDeliveryProcessorOptions{
		PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Second,
		SendTimeout: 30 * time.Second, Clock: fixedExternalDeliveryClock{now: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := processor.ProcessExternalDelivery(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if provider.deadline.IsZero() || !provider.deadline.After(now) || !provider.deadline.Before(claim.Owner.ExpiresAt) {
		t.Fatalf("provider deadline = %s, want (%s,%s)", provider.deadline, now, claim.Owner.ExpiresAt)
	}
}

func TestScopedExternalDeliveryProcessorRejectsPreparedMessageIdentityOrOriginDriftBeforeNetwork(t *testing.T) {
	startedAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	claim := externalDeliveryTestClaim(startedAt.Add(time.Minute))
	for _, test := range []struct {
		name           string
		baseURL        string
		recordID       string
		notificationID string
	}{
		{name: "record", baseURL: "https://houfeng.example", recordID: "rec_bbbbbbbbbbbbbbbbbbbbbbbb", notificationID: externalDeliveryTestNotification},
		{name: "notification", baseURL: "https://houfeng.example", recordID: externalDeliveryTestRecordID, notificationID: "rnt_1111111111111111111111111111111111111111111111111111111111111111"},
		{name: "origin", baseURL: "https://other.example", recordID: externalDeliveryTestRecordID, notificationID: externalDeliveryTestNotification},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &recordingExternalDeliveryProvider{outcome: ExternalDeliveryProviderSent}
			store := externalDeliveryPreparedStore(t, claim, startedAt, provider)
			drifted, err := RenderSafeExternalDelivery(test.baseURL, test.recordID, test.notificationID)
			if err != nil {
				t.Fatal(err)
			}
			store.preparation.Prepared.Message = drifted
			processor, err := NewScopedExternalDeliveryProcessor(store, ScopedExternalDeliveryProcessorOptions{
				PublicBaseURL: "https://houfeng.example", RetryBaseDelay: time.Second,
				SendTimeout: 10 * time.Second, Clock: fixedExternalDeliveryClock{now: startedAt},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := processor.ProcessExternalDelivery(context.Background(), claim); !errors.Is(err, ErrInvalidExternalDeliveryResult) {
				t.Fatalf("ProcessExternalDelivery(drifted %s) error = %v, want ErrInvalidExternalDeliveryResult", test.name, err)
			}
			if len(provider.messages) != 0 || !reflect.DeepEqual(store.steps, []string{"prepare"}) {
				t.Fatalf("drifted %s provider messages/steps = %#v/%#v, want zero network", test.name, provider.messages, store.steps)
			}
		})
	}
}

func TestScopedNotifierProviderUsesOnlySafeSummaryAndDiscardsProviderError(t *testing.T) {
	message, err := RenderSafeExternalDelivery("https://houfeng.example", externalDeliveryTestRecordID, externalDeliveryTestNotification)
	if err != nil {
		t.Fatal(err)
	}
	notifier := &externalDeliveryNotifierStub{err: errors.New("provider response body: token=secret")}
	provider, err := NewScopedNotifierProvider(notifier)
	if err != nil {
		t.Fatalf("NewScopedNotifierProvider() error = %v", err)
	}
	if outcome := provider.SendExternalDelivery(context.Background(), message); outcome != ExternalDeliveryProviderUnknownOutcome {
		t.Fatalf("SendExternalDelivery() outcome = %q, want unknown_outcome", outcome)
	}
	if notifier.received != message.Summary+"\n"+message.Link {
		t.Fatalf("notifier received = %q, want exact safe allowlist", notifier.received)
	}
	if reflect.TypeOf(provider).NumField() != 1 {
		t.Fatalf("scoped notifier adapter fields = %d, want only notifier", reflect.TypeOf(provider).NumField())
	}
}

func TestScopedNotifierProviderMapsTypedContentFreeFailureClasses(t *testing.T) {
	message, err := RenderSafeExternalDelivery("https://houfeng.example", externalDeliveryTestRecordID, externalDeliveryTestNotification)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		err  error
		want ExternalDeliveryProviderOutcome
	}{
		{name: "temporary", err: notify.NewSendFailure(notify.SendFailureTemporary), want: ExternalDeliveryProviderTemporaryFailure},
		{name: "permanent", err: notify.NewSendFailure(notify.SendFailurePermanent), want: ExternalDeliveryProviderPermanentFailure},
		{name: "unknown", err: notify.NewSendFailure(notify.SendFailureUnknown), want: ExternalDeliveryProviderUnknownOutcome},
		{name: "untyped", err: errors.New("unsafe provider detail"), want: ExternalDeliveryProviderUnknownOutcome},
	} {
		t.Run(test.name, func(t *testing.T) {
			notifier := &externalDeliveryNotifierStub{err: test.err}
			provider, err := NewScopedNotifierProvider(notifier)
			if err != nil {
				t.Fatal(err)
			}
			if got := provider.SendExternalDelivery(context.Background(), message); got != test.want {
				t.Fatalf("SendExternalDelivery() = %q, want %q", got, test.want)
			}
		})
	}
}

type externalDeliveryStoreStub struct {
	preparation  ExternalDeliveryPreparation
	prepareErr   error
	finalResult  ExternalDeliveryOutboxResult
	finalErr     error
	finalOutcome ExternalDeliveryProviderOutcome
	steps        []string
}

func (store *externalDeliveryStoreStub) PrepareExternalDelivery(_ context.Context, _ recordplatform.ClaimedOutboxEventV1, _ string) (ExternalDeliveryPreparation, error) {
	store.steps = append(store.steps, "prepare")
	return store.preparation, store.prepareErr
}

func (store *externalDeliveryStoreStub) FinalizeExternalDelivery(
	_ context.Context,
	_ recordplatform.ClaimedOutboxEventV1,
	_ ExternalDeliveryAttempt,
	outcome ExternalDeliveryProviderOutcome,
	_ time.Duration,
) (ExternalDeliveryOutboxResult, error) {
	store.steps = append(store.steps, "finalize")
	store.finalOutcome = outcome
	return store.finalResult, store.finalErr
}

func externalDeliveryPreparedStore(t *testing.T, claim recordplatform.ClaimedOutboxEventV1, startedAt time.Time, provider ExternalDeliveryProvider) *externalDeliveryStoreStub {
	t.Helper()
	message, err := RenderSafeExternalDelivery("https://houfeng.example", externalDeliveryTestRecordID, externalDeliveryTestNotification)
	if err != nil {
		t.Fatal(err)
	}
	attempt := ExternalDeliveryAttempt{
		DeliveryID: claim.Event.SubjectID, RecordID: externalDeliveryTestRecordID,
		NotificationID: externalDeliveryTestNotification, RecipientUserID: externalDeliveryTestUserID,
		Channel: NotificationDeliveryTelegram, BindingID: "telegram_primary",
		SourceVersion: 7, AuthorizationEpoch: 11, RecordFenceEpoch: 13,
		Attempt: 1, StartedAt: startedAt,
	}
	return &externalDeliveryStoreStub{preparation: ExternalDeliveryPreparation{Prepared: &PreparedExternalDelivery{
		Attempt: attempt,
		Binding: ScopedTransportBinding{
			ProjectID: recordauth.ProjectIDDefault, RecipientUserID: attempt.RecipientUserID,
			Channel: attempt.Channel, BindingID: attempt.BindingID, Provider: provider,
		},
		Message: message,
	}}}
}

type fixedExternalDeliveryClock struct{ now time.Time }

func (clock fixedExternalDeliveryClock) Now() time.Time { return clock.now }

type externalDeliveryNotifierStub struct {
	received string
	err      error
}

func (notifier *externalDeliveryNotifierStub) Send(_ context.Context, summary string) error {
	notifier.received = summary
	return notifier.err
}

func externalDeliveryTestClaim(ownerExpiresAt time.Time) recordplatform.ClaimedOutboxEventV1 {
	return recordplatform.ClaimedOutboxEventV1{
		Event: recordplatform.OutboxEvent{
			RowID: 61, ProjectID: string(recordplatform.ProjectIDDefault),
			EventKind:   recordplatform.OutboxEventKindRecordNotificationDelivery,
			SubjectKind: recordplatform.OutboxSubjectKindDelivery, SubjectID: "rnd_0123456789abcdef",
			SourceVersion: 7, AuthorizationEpoch: 11, RecordFenceEpoch: 13,
		},
		Owner:     recordplatform.OwnerLease{OwnerID: "notification_worker", Generation: 1, ExpiresAt: ownerExpiresAt},
		ExpiresAt: ownerExpiresAt.Add(time.Hour),
	}
}

func uint8Slice(values ...uint8) []uint8 { return values }
