package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
)

type externalDeliveryRow struct {
	deliveryID         string
	projectID          string
	recordID           string
	notificationID     string
	recipientUserID    string
	channel            recordcollaboration.NotificationDeliveryChannel
	bindingID          string
	state              recordcollaboration.NotificationDeliveryState
	sourceVersion      uint64
	authorizationEpoch uint64
	recordFenceEpoch   uint64
	attemptCount       uint8
	attemptStartedAt   *time.Time
	retryDue           bool
}

func (repository *PostgresRecordNotificationRepository) PrepareExternalDelivery(
	ctx context.Context,
	claim recordplatform.ClaimedOutboxEventV1,
	publicBaseURL string,
) (recordcollaboration.ExternalDeliveryPreparation, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) ||
		repository.authorization == nil || nilRecordSubjectDependency(repository.authorization.resolver) ||
		nilCollaborationDependency(repository.bindings) || claim.Validate() != nil ||
		claim.Event.EventKind != recordplatform.OutboxEventKindRecordNotificationDelivery ||
		claim.Event.SubjectKind != recordplatform.OutboxSubjectKindDelivery {
		return recordcollaboration.ExternalDeliveryPreparation{}, recordcollaboration.ErrInvalidExternalDeliveryProcessor
	}
	var preparation recordcollaboration.ExternalDeliveryPreparation
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := transaction.AssertOutboxClaim(ctx, claim); err != nil {
			return err
		}
		row, err := loadExternalDeliveryForUpdate(ctx, transaction.tx, claim.Event.SubjectID)
		if errors.Is(err, pgx.ErrNoRows) {
			preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxCancel}}
			return nil
		}
		if err != nil {
			return err
		}
		if !row.matchesClaim(claim.Event) {
			preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxCancel}}
			return nil
		}
		switch row.state {
		case recordcollaboration.NotificationDeliverySent,
			recordcollaboration.NotificationDeliveryPermanentFailure,
			recordcollaboration.NotificationDeliveryUnknownOutcome:
			preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxComplete}}
			return nil
		case recordcollaboration.NotificationDeliveryCancelled:
			preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxCancel}}
			return nil
		case recordcollaboration.NotificationDeliveryProcessing:
			if err := finalizeUnknownExternalDeliveryTakeover(ctx, transaction.tx, row); err != nil {
				return err
			}
			preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxComplete}}
			return nil
		case recordcollaboration.NotificationDeliveryPending, recordcollaboration.NotificationDeliveryRetryWait:
			if row.state == recordcollaboration.NotificationDeliveryRetryWait && !row.retryDue {
				return recordcollaboration.ErrInvalidExternalDeliveryResult
			}
		default:
			return recordcollaboration.ErrInvalidExternalDeliveryResult
		}

		candidate, err := queryInboxCandidate(ctx, transaction.tx, row.recipientUserID, row.notificationID, false)
		if errors.Is(err, recordcollaboration.ErrInboxNotFound) {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, row, "authorization_revoked", &preparation)
		}
		if err != nil {
			return fmt.Errorf("load external delivery recipient: %w", err)
		}
		candidate, err = repository.authorizeInboxCandidate(
			ctx, transaction.tx,
			recordauth.ActorScope{UserID: row.recipientUserID, ProjectID: recordauth.ProjectIDDefault},
			candidate, newInboxAuthorizationCache(),
		)
		if errors.Is(err, recordcollaboration.ErrInboxNotFound) {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, row, "authorization_revoked", &preparation)
		}
		if err != nil {
			return recordcollaboration.ErrExternalDeliveryUnavailable
		}
		if candidate.item.RecordID != row.recordID || candidate.item.NotificationID != row.notificationID ||
			candidate.item.SourceVersion != row.sourceVersion || candidate.authorizationEpoch != row.authorizationEpoch ||
			candidate.recordFenceEpoch != row.recordFenceEpoch {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, row, "source_stale", &preparation)
		}

		ref := recordcollaboration.ScopedTransportBindingRef{
			ProjectID: recordauth.ProjectID(row.projectID), RecipientUserID: row.recipientUserID,
			Channel: row.channel, BindingID: row.bindingID,
		}
		binding, err := repository.bindings.ResolveScopedTransportBinding(ctx, transaction.tx, ref)
		if errors.Is(err, recordcollaboration.ErrScopedTransportBindingNotFound) {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, row, "binding_unbound", &preparation)
		}
		if err != nil {
			return recordcollaboration.ErrExternalDeliveryUnavailable
		}
		if binding.Validate() != nil || binding.Ref() != ref {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, row, "binding_invalid", &preparation)
		}
		message, err := recordcollaboration.RenderSafeExternalDelivery(publicBaseURL, row.recordID, row.notificationID)
		if err != nil {
			return err
		}
		attempt, err := startExternalDeliveryAttempt(ctx, transaction.tx, row)
		if err != nil {
			return err
		}
		preparation = recordcollaboration.ExternalDeliveryPreparation{Prepared: &recordcollaboration.PreparedExternalDelivery{
			Attempt: attempt, Binding: binding, Message: message,
		}}
		return nil
	})
	if err != nil {
		return recordcollaboration.ExternalDeliveryPreparation{}, err
	}
	if preparation.Validate() != nil {
		return recordcollaboration.ExternalDeliveryPreparation{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	return preparation, nil
}

func (repository *PostgresRecordNotificationRepository) FinalizeExternalDelivery(
	ctx context.Context,
	claim recordplatform.ClaimedOutboxEventV1,
	attempt recordcollaboration.ExternalDeliveryAttempt,
	outcome recordcollaboration.ExternalDeliveryProviderOutcome,
	retryAfter time.Duration,
) (recordcollaboration.ExternalDeliveryOutboxResult, error) {
	if ctx == nil || repository == nil || repository.platform == nil || claim.Validate() != nil || attempt.Validate() != nil ||
		outcome.Validate() != nil || attempt.DeliveryID != claim.Event.SubjectID ||
		attempt.SourceVersion != claim.Event.SourceVersion || attempt.AuthorizationEpoch != claim.Event.AuthorizationEpoch ||
		attempt.RecordFenceEpoch != claim.Event.RecordFenceEpoch ||
		(outcome != recordcollaboration.ExternalDeliveryProviderTemporaryFailure && retryAfter != 0) ||
		(outcome == recordcollaboration.ExternalDeliveryProviderTemporaryFailure && attempt.Attempt < recordcollaboration.MaxNotificationDeliveryAttempts && retryAfter.Microseconds() <= 0) ||
		(outcome == recordcollaboration.ExternalDeliveryProviderTemporaryFailure && attempt.Attempt == recordcollaboration.MaxNotificationDeliveryAttempts && retryAfter != 0) {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	var result recordcollaboration.ExternalDeliveryOutboxResult
	err := repository.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		if err := transaction.AssertOutboxClaim(ctx, claim); err != nil {
			return err
		}
		row, err := loadExternalDeliveryForUpdate(ctx, transaction.tx, attempt.DeliveryID)
		if err != nil {
			return fmt.Errorf("load external delivery finalizer: %w", err)
		}
		if !row.matchesAttempt(attempt) || row.state != recordcollaboration.NotificationDeliveryProcessing {
			return recordplatform.ErrLostOwnerLease
		}
		result, err = finalizeExternalDeliveryAttempt(ctx, transaction.tx, row, attempt, outcome, retryAfter)
		return err
	})
	if err != nil {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, err
	}
	return result, result.Validate()
}

func loadExternalDeliveryForUpdate(ctx context.Context, tx pgx.Tx, deliveryID string) (externalDeliveryRow, error) {
	row := externalDeliveryRow{deliveryID: deliveryID}
	var channel, state string
	var sourceVersion, authorizationEpoch, recordFenceEpoch, attemptCount int64
	err := tx.QueryRow(ctx, `
		select notifications.project_id, deliveries.record_id, deliveries.notification_id,
		       deliveries.recipient_user_id, deliveries.channel, deliveries.binding_id,
		       deliveries.delivery_state, notifications.source_version,
		       deliveries.authorization_epoch, deliveries.record_fence_epoch,
		       deliveries.attempt_count, deliveries.attempt_started_at,
		       (deliveries.next_attempt_at is null or deliveries.next_attempt_at <= transaction_timestamp())
		from public.record_notification_deliveries deliveries
		join public.record_notifications notifications
		  on notifications.record_id = deliveries.record_id
		 and notifications.notification_id = deliveries.notification_id
		where deliveries.delivery_id = $1
		for update of deliveries`, deliveryID).Scan(
		&row.projectID, &row.recordID, &row.notificationID, &row.recipientUserID,
		&channel, &row.bindingID, &state, &sourceVersion, &authorizationEpoch,
		&recordFenceEpoch, &attemptCount, &row.attemptStartedAt, &row.retryDue,
	)
	if err != nil {
		return externalDeliveryRow{}, err
	}
	if sourceVersion <= 0 || authorizationEpoch <= 0 || recordFenceEpoch < 0 || attemptCount < 0 || attemptCount > int64(recordcollaboration.MaxNotificationDeliveryAttempts) {
		return externalDeliveryRow{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	row.channel = recordcollaboration.NotificationDeliveryChannel(channel)
	row.state = recordcollaboration.NotificationDeliveryState(state)
	row.sourceVersion = uint64(sourceVersion)
	row.authorizationEpoch = uint64(authorizationEpoch)
	row.recordFenceEpoch = uint64(recordFenceEpoch)
	row.attemptCount = uint8(attemptCount)
	if row.attemptStartedAt != nil {
		value := row.attemptStartedAt.UTC()
		row.attemptStartedAt = &value
	}
	return row, nil
}

func (row externalDeliveryRow) matchesClaim(event recordplatform.OutboxEvent) bool {
	return row.deliveryID == event.SubjectID && row.projectID == event.ProjectID &&
		row.sourceVersion == event.SourceVersion && row.authorizationEpoch == event.AuthorizationEpoch &&
		row.recordFenceEpoch == event.RecordFenceEpoch && recordauth.ValidateActorUserID(row.recipientUserID) == nil &&
		recordcollaboration.ValidateNotificationDeliveryChannel(row.channel) == nil && row.attemptCount <= recordcollaboration.MaxNotificationDeliveryAttempts
}

func (row externalDeliveryRow) matchesAttempt(attempt recordcollaboration.ExternalDeliveryAttempt) bool {
	return row.deliveryID == attempt.DeliveryID && row.recordID == attempt.RecordID && row.notificationID == attempt.NotificationID &&
		row.recipientUserID == attempt.RecipientUserID && row.channel == attempt.Channel && row.bindingID == attempt.BindingID &&
		row.sourceVersion == attempt.SourceVersion && row.authorizationEpoch == attempt.AuthorizationEpoch &&
		row.recordFenceEpoch == attempt.RecordFenceEpoch && row.attemptCount == attempt.Attempt &&
		row.attemptStartedAt != nil && row.attemptStartedAt.Equal(attempt.StartedAt)
}

func startExternalDeliveryAttempt(ctx context.Context, tx pgx.Tx, row externalDeliveryRow) (recordcollaboration.ExternalDeliveryAttempt, error) {
	if row.attemptCount >= recordcollaboration.MaxNotificationDeliveryAttempts {
		return recordcollaboration.ExternalDeliveryAttempt{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	var attemptNo int64
	var startedAt time.Time
	err := tx.QueryRow(ctx, `
		update public.record_notification_deliveries
		set delivery_state = 'processing', attempt_count = attempt_count + 1,
		    attempt_started_at = transaction_timestamp(), next_attempt_at = null,
		    reason_code = '', updated_at = transaction_timestamp()
		where delivery_id = $1 and delivery_state in ('pending', 'retry_wait')
		  and attempt_count < 8
		returning attempt_count, attempt_started_at`, row.deliveryID).Scan(&attemptNo, &startedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordcollaboration.ExternalDeliveryAttempt{}, recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return recordcollaboration.ExternalDeliveryAttempt{}, fmt.Errorf("start external delivery attempt: %w", err)
	}
	attempt := recordcollaboration.ExternalDeliveryAttempt{
		DeliveryID: row.deliveryID, RecordID: row.recordID, NotificationID: row.notificationID,
		RecipientUserID: row.recipientUserID, Channel: row.channel, BindingID: row.bindingID,
		SourceVersion: row.sourceVersion, AuthorizationEpoch: row.authorizationEpoch,
		RecordFenceEpoch: row.recordFenceEpoch, Attempt: uint8(attemptNo), StartedAt: startedAt.UTC(),
	}
	if attempt.Validate() != nil {
		return recordcollaboration.ExternalDeliveryAttempt{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	return attempt, nil
}

func prepareCancelledExternalDelivery(
	ctx context.Context,
	tx pgx.Tx,
	row externalDeliveryRow,
	reason string,
	preparation *recordcollaboration.ExternalDeliveryPreparation,
) error {
	if row.attemptCount >= recordcollaboration.MaxNotificationDeliveryAttempts {
		return recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	attemptNo := row.attemptCount + 1
	if err := insertExternalDeliveryAttempt(ctx, tx, row, attemptNo, "cancelled", reason, nil); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		update public.record_notification_deliveries
		set delivery_state = 'cancelled', attempt_count = $2,
		    attempt_started_at = null, next_attempt_at = null, reason_code = $3,
		    cancelled_at = transaction_timestamp(), updated_at = transaction_timestamp()
		where delivery_id = $1 and delivery_state in ('pending', 'retry_wait')`, row.deliveryID, int64(attemptNo), reason)
	if err != nil {
		return fmt.Errorf("cancel external delivery: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	*preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxCancel}}
	return nil
}

func finalizeUnknownExternalDeliveryTakeover(ctx context.Context, tx pgx.Tx, row externalDeliveryRow) error {
	if row.attemptCount == 0 || row.attemptStartedAt == nil {
		return recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	if err := insertExternalDeliveryAttempt(ctx, tx, row, row.attemptCount, "unknown_outcome", "lease_takeover_unknown", row.attemptStartedAt); err != nil {
		return err
	}
	command, err := tx.Exec(ctx, `
		update public.record_notification_deliveries
		set delivery_state = 'unknown_outcome', attempt_started_at = null,
		    next_attempt_at = null, reason_code = 'lease_takeover_unknown',
		    updated_at = transaction_timestamp()
		where delivery_id = $1 and delivery_state = 'processing'
		  and attempt_count = $2 and attempt_started_at = $3`,
		row.deliveryID, int64(row.attemptCount), *row.attemptStartedAt,
	)
	if err != nil {
		return fmt.Errorf("finalize unknown external delivery takeover: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordplatform.ErrLostOwnerLease
	}
	return nil
}

func finalizeExternalDeliveryAttempt(
	ctx context.Context,
	tx pgx.Tx,
	row externalDeliveryRow,
	attempt recordcollaboration.ExternalDeliveryAttempt,
	outcome recordcollaboration.ExternalDeliveryProviderOutcome,
	retryAfter time.Duration,
) (recordcollaboration.ExternalDeliveryOutboxResult, error) {
	attemptOutcome := string(outcome)
	reason := ""
	state := recordcollaboration.NotificationDeliverySent
	result := recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxComplete}
	switch outcome {
	case recordcollaboration.ExternalDeliveryProviderSent:
	case recordcollaboration.ExternalDeliveryProviderTemporaryFailure:
		reason = "provider_temporary_failure"
		if attempt.Attempt < recordcollaboration.MaxNotificationDeliveryAttempts {
			state = recordcollaboration.NotificationDeliveryRetryWait
			result = recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxRetry, RetryAfter: retryAfter}
		} else {
			state = recordcollaboration.NotificationDeliveryPermanentFailure
			reason = "attempts_exhausted"
		}
	case recordcollaboration.ExternalDeliveryProviderPermanentFailure:
		state = recordcollaboration.NotificationDeliveryPermanentFailure
		reason = "provider_permanent_failure"
	case recordcollaboration.ExternalDeliveryProviderUnknownOutcome:
		state = recordcollaboration.NotificationDeliveryUnknownOutcome
		reason = "provider_unknown_outcome"
	default:
		return recordcollaboration.ExternalDeliveryOutboxResult{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	if err := insertExternalDeliveryAttempt(ctx, tx, row, attempt.Attempt, attemptOutcome, reason, &attempt.StartedAt); err != nil {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, err
	}
	var command pgconn.CommandTag
	var err error
	switch state {
	case recordcollaboration.NotificationDeliverySent:
		command, err = tx.Exec(ctx, `
			update public.record_notification_deliveries
			set delivery_state = 'sent', attempt_started_at = null, next_attempt_at = null,
			    reason_code = '', sent_at = transaction_timestamp(), updated_at = transaction_timestamp()
			where delivery_id = $1 and delivery_state = 'processing'
			  and attempt_count = $2 and attempt_started_at = $3`, row.deliveryID, int64(attempt.Attempt), attempt.StartedAt)
	case recordcollaboration.NotificationDeliveryRetryWait:
		command, err = tx.Exec(ctx, `
			update public.record_notification_deliveries
			set delivery_state = 'retry_wait', attempt_started_at = null,
			    next_attempt_at = transaction_timestamp() + ($4 * interval '1 microsecond'),
			    reason_code = $5, updated_at = transaction_timestamp()
			where delivery_id = $1 and delivery_state = 'processing'
			  and attempt_count = $2 and attempt_started_at = $3`, row.deliveryID, int64(attempt.Attempt), attempt.StartedAt, retryAfter.Microseconds(), reason)
	default:
		command, err = tx.Exec(ctx, `
			update public.record_notification_deliveries
			set delivery_state = $4, attempt_started_at = null, next_attempt_at = null,
			    reason_code = $5, updated_at = transaction_timestamp()
			where delivery_id = $1 and delivery_state = 'processing'
			  and attempt_count = $2 and attempt_started_at = $3`, row.deliveryID, int64(attempt.Attempt), attempt.StartedAt, string(state), reason)
	}
	if err != nil {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, fmt.Errorf("finalize external delivery attempt: %w", err)
	}
	if command.RowsAffected() != 1 {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, recordplatform.ErrLostOwnerLease
	}
	return result, nil
}

func insertExternalDeliveryAttempt(
	ctx context.Context,
	tx pgx.Tx,
	row externalDeliveryRow,
	attempt uint8,
	outcome, reason string,
	startedAt *time.Time,
) error {
	attemptID := recordcollaboration.NotificationDeliveryAttemptID(row.deliveryID, attempt)
	if attemptID == "" || uint64(row.authorizationEpoch) > math.MaxInt64 || uint64(row.recordFenceEpoch) > math.MaxInt64 {
		return recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	if startedAt == nil {
		_, err := tx.Exec(ctx, `
			insert into public.record_notification_delivery_attempts (
				attempt_id, record_id, delivery_id, notification_id, recipient_user_id,
				attempt_no, outcome, reason_code, authorization_epoch, record_fence_epoch,
				started_at, completed_at
			) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			          transaction_timestamp(), transaction_timestamp())`,
			attemptID, row.recordID, row.deliveryID, row.notificationID, row.recipientUserID,
			int64(attempt), outcome, reason, int64(row.authorizationEpoch), int64(row.recordFenceEpoch),
		)
		if err != nil {
			return fmt.Errorf("insert external delivery attempt: %w", err)
		}
		return nil
	}
	_, err := tx.Exec(ctx, `
		insert into public.record_notification_delivery_attempts (
			attempt_id, record_id, delivery_id, notification_id, recipient_user_id,
			attempt_no, outcome, reason_code, authorization_epoch, record_fence_epoch,
			started_at, completed_at
		) values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, transaction_timestamp())`,
		attemptID, row.recordID, row.deliveryID, row.notificationID, row.recipientUserID,
		int64(attempt), outcome, reason, int64(row.authorizationEpoch), int64(row.recordFenceEpoch), startedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert external delivery attempt: %w", err)
	}
	return nil
}

var _ recordcollaboration.ExternalDeliveryStore = (*PostgresRecordNotificationRepository)(nil)
