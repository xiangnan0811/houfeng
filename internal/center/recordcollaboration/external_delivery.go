package recordcollaboration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"net/url"
	"reflect"
	"strings"
	"time"

	"houfeng/internal/center/notify"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

const safeExternalDeliverySummary = "A Houfeng Record collaboration update is available."

const notificationDeliveryIdentityDomainV1 = "houfeng.record-collaboration.notification-delivery.v1"

const notificationDeliveryAttemptIdentityDomainV1 = "houfeng.record-collaboration.notification-delivery-attempt.v1"

const MaxExternalDeliverySendTimeout = 30 * time.Second

const externalDeliveryLeaseSafetyMargin = time.Millisecond

var (
	ErrInvalidScopedTransportBinding    = errors.New("invalid scoped record notification transport binding")
	ErrInvalidSafeExternalDelivery      = errors.New("invalid safe external record notification delivery")
	ErrInvalidExternalDeliveryResult    = errors.New("invalid external record notification delivery result")
	ErrInvalidExternalDeliveryProcessor = errors.New("invalid external record notification delivery processor")
	ErrExternalDeliveryUnavailable      = errors.New("external record notification delivery dependency unavailable")
	ErrScopedTransportBindingNotFound   = errors.New("scoped record notification transport binding not found")
)

type SafeExternalDelivery struct {
	Summary string
	Link    string
}

func (delivery SafeExternalDelivery) Validate() error {
	if delivery.Summary != safeExternalDeliverySummary || len(delivery.Link) == 0 || len(delivery.Link) > 2048 {
		return ErrInvalidSafeExternalDelivery
	}
	parsed, err := url.Parse(delivery.Link)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.Opaque != "" || parsed.RawPath != "" || parsed.ForceQuery ||
		!strings.HasPrefix(parsed.Path, "/records/") || len(strings.TrimPrefix(parsed.Path, "/records/")) == 0 {
		return ErrInvalidSafeExternalDelivery
	}
	recordID := strings.TrimPrefix(parsed.Path, "/records/")
	values := parsed.Query()
	notificationValues, exists := values["notification"]
	if !validRecordID(recordID) || !exists || len(values) != 1 || len(notificationValues) != 1 ||
		!validNotificationID(notificationValues[0]) {
		return ErrInvalidSafeExternalDelivery
	}
	canonicalQuery := url.Values{"notification": []string{notificationValues[0]}}.Encode()
	if parsed.Path != "/records/"+recordID || parsed.RawQuery != canonicalQuery || parsed.String() != delivery.Link {
		return ErrInvalidSafeExternalDelivery
	}
	return nil
}

type ExternalDeliveryProviderOutcome string

const (
	ExternalDeliveryProviderSent             ExternalDeliveryProviderOutcome = "sent"
	ExternalDeliveryProviderTemporaryFailure ExternalDeliveryProviderOutcome = "temporary_failure"
	ExternalDeliveryProviderPermanentFailure ExternalDeliveryProviderOutcome = "permanent_failure"
	ExternalDeliveryProviderUnknownOutcome   ExternalDeliveryProviderOutcome = "unknown_outcome"
)

func (outcome ExternalDeliveryProviderOutcome) Validate() error {
	switch outcome {
	case ExternalDeliveryProviderSent, ExternalDeliveryProviderTemporaryFailure,
		ExternalDeliveryProviderPermanentFailure, ExternalDeliveryProviderUnknownOutcome:
		return nil
	default:
		return ErrInvalidExternalDeliveryResult
	}
}

type ExternalDeliveryProvider interface {
	SendExternalDelivery(context.Context, SafeExternalDelivery) ExternalDeliveryProviderOutcome
}

type SafeSummaryNotifier interface {
	Send(context.Context, string) error
}

type ScopedNotifierProvider struct {
	notifier SafeSummaryNotifier
}

func NewScopedNotifierProvider(notifier SafeSummaryNotifier) (ScopedNotifierProvider, error) {
	if nilExternalDeliveryDependency(notifier) {
		return ScopedNotifierProvider{}, ErrInvalidScopedTransportBinding
	}
	return ScopedNotifierProvider{notifier: notifier}, nil
}

func (provider ScopedNotifierProvider) SendExternalDelivery(ctx context.Context, delivery SafeExternalDelivery) ExternalDeliveryProviderOutcome {
	if ctx == nil || nilExternalDeliveryDependency(provider.notifier) || delivery.Validate() != nil {
		return ExternalDeliveryProviderUnknownOutcome
	}
	if err := provider.notifier.Send(ctx, delivery.Summary+"\n"+delivery.Link); err != nil {
		failureClass, classified := notify.ClassifySendFailure(err)
		if !classified {
			return ExternalDeliveryProviderUnknownOutcome
		}
		switch failureClass {
		case notify.SendFailureTemporary:
			return ExternalDeliveryProviderTemporaryFailure
		case notify.SendFailurePermanent:
			return ExternalDeliveryProviderPermanentFailure
		default:
			return ExternalDeliveryProviderUnknownOutcome
		}
	}
	return ExternalDeliveryProviderSent
}

type ScopedTransportBindingRef struct {
	ProjectID       recordauth.ProjectID
	RecipientUserID string
	Channel         NotificationDeliveryChannel
	BindingID       string
}

func (binding ScopedTransportBindingRef) Validate() error {
	if binding.ProjectID != recordauth.ProjectIDDefault ||
		recordauth.ValidateActorUserID(binding.RecipientUserID) != nil ||
		ValidateNotificationDeliveryChannel(binding.Channel) != nil ||
		!validScopedTransportBindingID(binding.BindingID) {
		return ErrInvalidScopedTransportBinding
	}
	return nil
}

type ScopedTransportBinding struct {
	ProjectID       recordauth.ProjectID
	RecipientUserID string
	Channel         NotificationDeliveryChannel
	BindingID       string
	Provider        ExternalDeliveryProvider
}

func (binding ScopedTransportBinding) Validate() error {
	if binding.Ref().Validate() != nil || nilNotificationDependency(binding.Provider) {
		return ErrInvalidScopedTransportBinding
	}
	return nil
}

func (binding ScopedTransportBinding) Ref() ScopedTransportBindingRef {
	return ScopedTransportBindingRef{
		ProjectID: binding.ProjectID, RecipientUserID: binding.RecipientUserID,
		Channel: binding.Channel, BindingID: binding.BindingID,
	}
}

func validScopedTransportBindingID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func RenderSafeExternalDelivery(baseURL, recordID, notificationID string) (SafeExternalDelivery, error) {
	if !validRecordID(recordID) || !validNotificationID(notificationID) {
		return SafeExternalDelivery{}, ErrInvalidSafeExternalDelivery
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.Opaque != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return SafeExternalDelivery{}, ErrInvalidSafeExternalDelivery
	}
	parsed.Path = "/records/" + recordID
	query := url.Values{}
	query.Set("notification", notificationID)
	parsed.RawQuery = query.Encode()
	delivery := SafeExternalDelivery{Summary: safeExternalDeliverySummary, Link: parsed.String()}
	if delivery.Validate() != nil {
		return SafeExternalDelivery{}, ErrInvalidSafeExternalDelivery
	}
	return delivery, nil
}

func NotificationDeliveryID(notificationID, recipientUserID string, channel NotificationDeliveryChannel, bindingID string) string {
	ref := ScopedTransportBindingRef{
		ProjectID: recordauth.ProjectIDDefault, RecipientUserID: recipientUserID,
		Channel: channel, BindingID: bindingID,
	}
	if !validNotificationID(notificationID) || ref.Validate() != nil {
		return ""
	}
	encoder := actionCanonicalEncoder{}
	encoder.string(notificationDeliveryIdentityDomainV1)
	encoder.string(notificationID)
	encoder.string(recipientUserID)
	encoder.string(string(channel))
	encoder.string(bindingID)
	digest := sha256.Sum256(encoder.bytes)
	return "rnd_" + hex.EncodeToString(digest[:])
}

func NotificationDeliveryAttemptID(deliveryID string, attempt uint8) string {
	if ValidateNotificationDeliveryID(deliveryID) != nil || ValidateNotificationDeliveryAttempt(attempt) != nil {
		return ""
	}
	encoder := actionCanonicalEncoder{}
	encoder.string(notificationDeliveryAttemptIdentityDomainV1)
	encoder.string(deliveryID)
	encoder.uint64(uint64(attempt))
	digest := sha256.Sum256(encoder.bytes)
	return "rna_" + hex.EncodeToString(digest[:])
}

func ExternalDeliveryRetryDelay(base time.Duration, attempt uint8) (time.Duration, bool) {
	if base.Microseconds() <= 0 || attempt == 0 || attempt >= MaxNotificationDeliveryAttempts {
		return 0, false
	}
	multiplier := uint64(1) << (attempt - 1)
	if uint64(base) > uint64(math.MaxInt64)/multiplier {
		return 0, false
	}
	return base * time.Duration(multiplier), true
}

type ExternalDeliveryOutboxDisposition string

const (
	ExternalDeliveryOutboxComplete ExternalDeliveryOutboxDisposition = "complete"
	ExternalDeliveryOutboxRetry    ExternalDeliveryOutboxDisposition = "retry"
	ExternalDeliveryOutboxCancel   ExternalDeliveryOutboxDisposition = "cancel"
)

type ExternalDeliveryOutboxResult struct {
	Disposition ExternalDeliveryOutboxDisposition
	RetryAfter  time.Duration
}

func (result ExternalDeliveryOutboxResult) Validate() error {
	switch result.Disposition {
	case ExternalDeliveryOutboxComplete, ExternalDeliveryOutboxCancel:
		if result.RetryAfter != 0 {
			return ErrInvalidExternalDeliveryResult
		}
	case ExternalDeliveryOutboxRetry:
		if result.RetryAfter.Microseconds() <= 0 {
			return ErrInvalidExternalDeliveryResult
		}
	default:
		return ErrInvalidExternalDeliveryResult
	}
	return nil
}

type ExternalDeliveryProcessor interface {
	ProcessExternalDelivery(context.Context, recordplatform.ClaimedOutboxEventV1) (ExternalDeliveryOutboxResult, error)
}

type ExternalDeliveryAttempt struct {
	DeliveryID         string
	RecordID           string
	NotificationID     string
	RecipientUserID    string
	Channel            NotificationDeliveryChannel
	BindingID          string
	SourceVersion      uint64
	AuthorizationEpoch uint64
	RecordFenceEpoch   uint64
	Attempt            uint8
	StartedAt          time.Time
}

func (attempt ExternalDeliveryAttempt) Validate() error {
	if ValidateNotificationDeliveryID(attempt.DeliveryID) != nil || !validRecordID(attempt.RecordID) ||
		!validNotificationID(attempt.NotificationID) || recordauth.ValidateActorUserID(attempt.RecipientUserID) != nil ||
		ValidateNotificationDeliveryChannel(attempt.Channel) != nil || !validScopedTransportBindingID(attempt.BindingID) ||
		attempt.SourceVersion == 0 || attempt.SourceVersion > math.MaxInt64 ||
		attempt.AuthorizationEpoch == 0 || attempt.AuthorizationEpoch > math.MaxInt64 ||
		attempt.RecordFenceEpoch > math.MaxInt64 || ValidateNotificationDeliveryAttempt(attempt.Attempt) != nil ||
		attempt.StartedAt.IsZero() || attempt.StartedAt.Location() != time.UTC {
		return ErrInvalidExternalDeliveryResult
	}
	return nil
}

type PreparedExternalDelivery struct {
	Attempt ExternalDeliveryAttempt
	Binding ScopedTransportBinding
	Message SafeExternalDelivery
}

func (prepared PreparedExternalDelivery) Validate() error {
	if prepared.Attempt.Validate() != nil || prepared.Binding.Validate() != nil || prepared.Message.Validate() != nil {
		return ErrInvalidExternalDeliveryResult
	}
	if prepared.Binding.ProjectID != recordauth.ProjectIDDefault ||
		prepared.Binding.RecipientUserID != prepared.Attempt.RecipientUserID ||
		prepared.Binding.Channel != prepared.Attempt.Channel || prepared.Binding.BindingID != prepared.Attempt.BindingID {
		return ErrInvalidExternalDeliveryResult
	}
	return nil
}

type ExternalDeliveryPreparation struct {
	Prepared *PreparedExternalDelivery
	Result   ExternalDeliveryOutboxResult
}

func (preparation ExternalDeliveryPreparation) Validate() error {
	if preparation.Prepared != nil {
		if preparation.Result != (ExternalDeliveryOutboxResult{}) || preparation.Prepared.Validate() != nil {
			return ErrInvalidExternalDeliveryResult
		}
		return nil
	}
	return preparation.Result.Validate()
}

type ExternalDeliveryStore interface {
	PrepareExternalDelivery(context.Context, recordplatform.ClaimedOutboxEventV1, string) (ExternalDeliveryPreparation, error)
	FinalizeExternalDelivery(context.Context, recordplatform.ClaimedOutboxEventV1, ExternalDeliveryAttempt, ExternalDeliveryProviderOutcome, time.Duration) (ExternalDeliveryOutboxResult, error)
}

type ScopedExternalDeliveryProcessorOptions struct {
	PublicBaseURL  string
	RetryBaseDelay time.Duration
	SendTimeout    time.Duration
	Clock          recordplatform.Clock
}

type ScopedExternalDeliveryProcessor struct {
	store   ExternalDeliveryStore
	options ScopedExternalDeliveryProcessorOptions
}

func NewScopedExternalDeliveryProcessor(store ExternalDeliveryStore, options ScopedExternalDeliveryProcessorOptions) (*ScopedExternalDeliveryProcessor, error) {
	if nilExternalDeliveryDependency(store) || nilExternalDeliveryDependency(options.Clock) || options.RetryBaseDelay.Microseconds() <= 0 ||
		options.SendTimeout.Microseconds() <= 0 || options.SendTimeout > MaxExternalDeliverySendTimeout {
		return nil, ErrInvalidExternalDeliveryProcessor
	}
	if _, retryable := ExternalDeliveryRetryDelay(options.RetryBaseDelay, MaxNotificationDeliveryAttempts-1); !retryable {
		return nil, ErrInvalidExternalDeliveryProcessor
	}
	if _, err := RenderSafeExternalDelivery(options.PublicBaseURL, "rec_validation", "rnt_0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"); err != nil {
		return nil, ErrInvalidExternalDeliveryProcessor
	}
	return &ScopedExternalDeliveryProcessor{store: store, options: options}, nil
}

func (processor *ScopedExternalDeliveryProcessor) ProcessExternalDelivery(ctx context.Context, claim recordplatform.ClaimedOutboxEventV1) (ExternalDeliveryOutboxResult, error) {
	if ctx == nil || processor == nil || nilExternalDeliveryDependency(processor.store) ||
		nilExternalDeliveryDependency(processor.options.Clock) || claim.Validate() != nil ||
		claim.Event.EventKind != recordplatform.OutboxEventKindRecordNotificationDelivery ||
		claim.Event.SubjectKind != recordplatform.OutboxSubjectKindDelivery {
		return ExternalDeliveryOutboxResult{}, ErrInvalidExternalDeliveryProcessor
	}
	preparation, err := processor.store.PrepareExternalDelivery(ctx, claim, processor.options.PublicBaseURL)
	if err != nil {
		return ExternalDeliveryOutboxResult{}, err
	}
	if preparation.Validate() != nil {
		return ExternalDeliveryOutboxResult{}, ErrInvalidExternalDeliveryResult
	}
	if preparation.Prepared == nil {
		return preparation.Result, nil
	}
	prepared := *preparation.Prepared
	if prepared.Attempt.DeliveryID != claim.Event.SubjectID || prepared.Attempt.SourceVersion != claim.Event.SourceVersion ||
		prepared.Attempt.AuthorizationEpoch != claim.Event.AuthorizationEpoch || prepared.Attempt.RecordFenceEpoch != claim.Event.RecordFenceEpoch {
		return ExternalDeliveryOutboxResult{}, ErrInvalidExternalDeliveryResult
	}
	sealedMessage, err := RenderSafeExternalDelivery(
		processor.options.PublicBaseURL, prepared.Attempt.RecordID, prepared.Attempt.NotificationID,
	)
	if err != nil || prepared.Message != sealedMessage {
		return ExternalDeliveryOutboxResult{}, ErrInvalidExternalDeliveryResult
	}
	guard, err := recordplatform.NewLeaseWorkGuardV1(processor.options.Clock, claim.Owner)
	if err != nil || !guard.CanContinue() {
		return ExternalDeliveryOutboxResult{}, recordplatform.ErrLeaseRenewalStopped
	}
	now := processor.options.Clock.Now()
	leaseDeadline := claim.Owner.ExpiresAt.Add(-externalDeliveryLeaseSafetyMargin)
	if !leaseDeadline.After(now) {
		return ExternalDeliveryOutboxResult{}, recordplatform.ErrLeaseRenewalStopped
	}
	sendDeadline := now.Add(processor.options.SendTimeout)
	if sendDeadline.After(leaseDeadline) {
		sendDeadline = leaseDeadline
	}
	sendContext, cancelSend := context.WithDeadline(ctx, sendDeadline)
	outcome := prepared.Binding.Provider.SendExternalDelivery(sendContext, sealedMessage)
	cancelSend()
	if outcome.Validate() != nil {
		outcome = ExternalDeliveryProviderUnknownOutcome
	}
	retryAfter := time.Duration(0)
	if outcome == ExternalDeliveryProviderTemporaryFailure && prepared.Attempt.Attempt < MaxNotificationDeliveryAttempts {
		var retryable bool
		retryAfter, retryable = ExternalDeliveryRetryDelay(processor.options.RetryBaseDelay, prepared.Attempt.Attempt)
		if !retryable {
			return ExternalDeliveryOutboxResult{}, ErrInvalidExternalDeliveryResult
		}
	}
	return processor.store.FinalizeExternalDelivery(ctx, claim, prepared.Attempt, outcome, retryAfter)
}

func nilExternalDeliveryDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return value.IsNil()
	default:
		return false
	}
}

var _ ExternalDeliveryProcessor = (*ScopedExternalDeliveryProcessor)(nil)
