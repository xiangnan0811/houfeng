package store

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
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

type externalDeliveryAttemptCommand struct {
	externalDeliveryAuthorityCommand
	AttemptNo uint8  `json:"attempt_no"`
	StartedAt string `json:"started_at"`
	AttemptID string `json:"attempt_id"`
}

type externalDeliveryOutcomeCommand struct {
	externalDeliveryAttemptCommand
	ReportingCapability string `json:"reporting_capability"`
	Outcome             string `json:"outcome"`
}

type externalDeliveryFinalizeCommand struct {
	externalDeliveryAttemptCommand
	EvidenceDigest string `json:"evidence_digest"`
	RetryAfterUS   int64  `json:"retry_after_us"`
}

type externalDeliveryReconcileCommand struct {
	externalDeliveryAttemptCommand
	RetryAfterUS int64 `json:"retry_after_us"`
}

func (repository *PostgresRecordNotificationRepository) PrepareExternalDelivery(
	ctx context.Context,
	claim recordplatform.ClaimedOutboxEventV1,
	publicBaseURL string,
	retryBaseDelay time.Duration,
) (recordcollaboration.ExternalDeliveryPreparation, error) {
	if ctx == nil || repository == nil || repository.platform == nil || nilCollaborationDependency(repository.members) ||
		nilRecordSubjectDependency(repository.authorization) ||
		nilCollaborationDependency(repository.bindings) || claim.Validate() != nil ||
		claim.Event.EventKind != recordplatform.OutboxEventKindRecordNotificationDelivery ||
		claim.Event.SubjectKind != recordplatform.OutboxSubjectKindDelivery {
		return recordcollaboration.ExternalDeliveryPreparation{}, recordcollaboration.ErrInvalidExternalDeliveryProcessor
	}
	if _, ok := recordcollaboration.ExternalDeliveryRetryDelay(retryBaseDelay, recordcollaboration.MaxNotificationDeliveryAttempts-1); !ok {
		return recordcollaboration.ExternalDeliveryPreparation{}, recordcollaboration.ErrInvalidExternalDeliveryProcessor
	}
	authorization, preparedRecordID, processing, err := repository.prepareExternalDeliveryAuthority(ctx, claim)
	if err != nil {
		return recordcollaboration.ExternalDeliveryPreparation{}, recordcollaboration.ErrExternalDeliveryUnavailable
	}
	if processing {
		var result recordcollaboration.ExternalDeliveryOutboxResult
		err := repository.platform.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
			result = recordcollaboration.ExternalDeliveryOutboxResult{}
			row, err := loadExternalDelivery(ctx, transaction.tx, claim.Event.SubjectID)
			if errors.Is(err, pgx.ErrNoRows) {
				return recordplatform.ErrLostOwnerLease
			}
			if err != nil {
				return err
			}
			if row.recordID != preparedRecordID || !row.matchesClaim(claim.Event) || row.state != recordcollaboration.NotificationDeliveryProcessing {
				return recordplatform.ErrLostOwnerLease
			}
			result, err = reconcileExternalDeliveryTakeover(ctx, transaction.tx, claim, row, retryBaseDelay)
			return err
		})
		if err != nil {
			return recordcollaboration.ExternalDeliveryPreparation{}, err
		}
		return recordcollaboration.ExternalDeliveryPreparation{Result: result}, nil
	}
	var preparation recordcollaboration.ExternalDeliveryPreparation
	err = repository.platform.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		preparation = recordcollaboration.ExternalDeliveryPreparation{}
		observed, err := loadExternalDelivery(ctx, transaction.tx, claim.Event.SubjectID)
		if errors.Is(err, pgx.ErrNoRows) {
			if preparedRecordID != "" {
				return &pgconn.PgError{Code: "40001", Message: "delivery route disappeared; restart transaction"}
			}
			preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxCancel}}
			return nil
		}
		if err != nil {
			return err
		}
		if observed.recordID != preparedRecordID {
			return &pgconn.PgError{Code: "40001", Message: "delivery record route changed; restart transaction"}
		}
		acquired, deletionReserved, err := acquireExternalDeliveryResources(ctx, transaction.tx, observed)
		if err != nil {
			return err
		}
		transaction.registerAcquiredRecordMutationResources(acquired)
		if err := transaction.AssertOutboxClaim(ctx, claim); err != nil {
			return err
		}
		row, err := loadExternalDelivery(ctx, transaction.tx, claim.Event.SubjectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return &pgconn.PgError{Code: "40001", Message: "delivery route disappeared; restart transaction"}
		}
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(observed, row) {
			return &pgconn.PgError{Code: "40001", Message: "delivery route changed; restart transaction"}
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
			result, err := reconcileExternalDeliveryTakeover(ctx, transaction.tx, claim, row, retryBaseDelay)
			if err != nil {
				return err
			}
			preparation = recordcollaboration.ExternalDeliveryPreparation{Result: result}
			return nil
		case recordcollaboration.NotificationDeliveryPending, recordcollaboration.NotificationDeliveryRetryWait:
			if row.state == recordcollaboration.NotificationDeliveryRetryWait && !row.retryDue {
				return recordcollaboration.ErrInvalidExternalDeliveryResult
			}
		default:
			return recordcollaboration.ErrInvalidExternalDeliveryResult
		}
		if deletionReserved {
			// A live claim may record that this delivery is no longer authorized;
			// the restricted command rechecks the exact owner and delivery tuple.
			return prepareCancelledExternalDelivery(ctx, transaction.tx, claim, row, "authorization_revoked", &preparation)
		}

		candidate, err := queryInboxCandidate(ctx, transaction.tx, row.recipientUserID, row.notificationID, false)
		if errors.Is(err, recordcollaboration.ErrInboxNotFound) {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, claim, row, "authorization_revoked", &preparation)
		}
		if err != nil {
			return fmt.Errorf("load external delivery recipient: %w", err)
		}
		candidate, err = repository.authorizeInboxCandidate(
			ctx, transaction.tx,
			recordauth.ActorScope{UserID: row.recipientUserID, ProjectID: recordauth.ProjectIDDefault},
			candidate, newInboxAuthorizationCache(authorization),
		)
		if errors.Is(err, recordcollaboration.ErrInboxNotFound) {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, claim, row, "authorization_revoked", &preparation)
		}
		if err != nil {
			return recordcollaboration.ErrExternalDeliveryUnavailable
		}
		if candidate.item.RecordID != row.recordID || candidate.item.NotificationID != row.notificationID ||
			candidate.item.SourceVersion != row.sourceVersion || candidate.authorizationEpoch != row.authorizationEpoch ||
			candidate.recordFenceEpoch != row.recordFenceEpoch {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, claim, row, "source_stale", &preparation)
		}

		ref := recordcollaboration.ScopedTransportBindingRef{
			ProjectID: recordauth.ProjectID(row.projectID), RecipientUserID: row.recipientUserID,
			Channel: row.channel, BindingID: row.bindingID,
		}
		binding, err := repository.bindings.ResolveScopedTransportBinding(ctx, transaction.tx, ref)
		if errors.Is(err, recordcollaboration.ErrScopedTransportBindingNotFound) {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, claim, row, "binding_unbound", &preparation)
		}
		if err != nil {
			return recordcollaboration.ErrExternalDeliveryUnavailable
		}
		if binding.Validate() != nil || binding.Ref() != ref {
			return prepareCancelledExternalDelivery(ctx, transaction.tx, claim, row, "binding_invalid", &preparation)
		}
		message, err := recordcollaboration.RenderSafeExternalDelivery(publicBaseURL, row.recordID, row.notificationID)
		if err != nil {
			return err
		}
		attempt, capability, queueFinalized, err := startExternalDeliveryAttempt(ctx, transaction.tx, claim, row)
		if err != nil {
			return err
		}
		if queueFinalized {
			preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{
				Disposition: recordcollaboration.ExternalDeliveryOutboxComplete, QueueFinalized: true,
			}}
			return nil
		}
		preparation = recordcollaboration.ExternalDeliveryPreparation{Prepared: &recordcollaboration.PreparedExternalDelivery{
			Attempt: attempt, ReportingCapability: capability, Binding: binding, Message: message,
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

// The outbox is already an effect of an existing record. Its preparation may
// lock an epoch but must never create one if the existing scope is missing.
func acquireExternalDeliveryResources(ctx context.Context, tx pgx.Tx, row externalDeliveryRow) (recordMutationResources, bool, error) {
	before, err := discoverEffectRecordResources(ctx, tx, []string{row.recordID}, []string{row.recipientUserID})
	if err != nil {
		return recordMutationResources{}, false, err
	}
	acquired, err := normalizeRecordMutationDeclarations(before)
	if err != nil {
		return recordMutationResources{}, false, err
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		return recordMutationResources{}, false, err
	}
	if err := acquireOutboxRecordPrefix(ctx, tx, acquired, row.recordID, row.recordFenceEpoch, true); err != nil {
		return recordMutationResources{}, false, err
	}
	var epoch int64
	if err := tx.QueryRow(ctx, `select delivery_epoch from public.content_delivery_epochs
		where project_id=$1 and object_kind='record' and object_id=$2`, row.projectID, row.recordID).Scan(&epoch); errors.Is(err, pgx.ErrNoRows) {
		return recordMutationResources{}, false, recordplatform.ErrContentDeliveryEpochMissing
	} else if err != nil {
		return recordMutationResources{}, false, fmt.Errorf("read external delivery epoch: %w", err)
	}
	if err := acquireRecordMutationResourceRange(ctx, tx, acquired, 61, 75); err != nil {
		return recordMutationResources{}, false, err
	}
	after, err := discoverEffectRecordResources(ctx, tx, []string{row.recordID}, []string{row.recipientUserID})
	if err != nil {
		return recordMutationResources{}, false, err
	}
	if !reflect.DeepEqual(before, after) {
		return recordMutationResources{}, false, &pgconn.PgError{Code: "40001", Message: "delivery record or authorization route changed; restart transaction"}
	}
	if err := recheckRecordMutationFenceState(ctx, tx, acquired.resources, false); err != nil {
		if errors.Is(err, records.ErrRecordDeletionReserved) {
			return acquired, true, nil
		}
		return recordMutationResources{}, false, err
	}
	return acquired, false, nil
}

func (repository *PostgresRecordNotificationRepository) prepareExternalDeliveryAuthority(
	ctx context.Context, claim recordplatform.ClaimedOutboxEventV1,
) (currentRecordAuthorizationTransactionResolver, string, bool, error) {
	var recordID string
	var needsAuthorization bool
	var processing bool
	err := repository.platform.runSourceAuthorizedTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		recordID = ""
		needsAuthorization = false
		processing = false
		row, err := loadExternalDelivery(ctx, transaction.tx, claim.Event.SubjectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		recordID = row.recordID
		needsAuthorization = row.matchesClaim(claim.Event) && (row.state == recordcollaboration.NotificationDeliveryPending ||
			row.state == recordcollaboration.NotificationDeliveryRetryWait)
		processing = row.matchesClaim(claim.Event) && row.state == recordcollaboration.NotificationDeliveryProcessing
		return nil
	})
	if err != nil || recordID == "" || !needsAuthorization {
		return nil, recordID, processing, err
	}
	authorization, err := repository.authorization.prepareCurrentAuthorization(ctx, []string{recordID})
	return authorization, recordID, processing, err
}

func (repository *PostgresRecordNotificationRepository) ReportExternalDeliveryOutcome(
	ctx context.Context,
	claim recordplatform.ClaimedOutboxEventV1,
	attempt recordcollaboration.ExternalDeliveryAttempt,
	capability recordcollaboration.ExternalDeliveryReportingCapability,
	outcome recordcollaboration.ExternalDeliveryProviderOutcome,
) (recordcollaboration.ExternalDeliveryFactLocator, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		!validExternalDeliveryClaimAttempt(claim, attempt) || capability.Validate() != nil || outcome.Validate() != nil {
		return recordcollaboration.ExternalDeliveryFactLocator{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	command := externalDeliveryOutcomeCommand{
		externalDeliveryAttemptCommand: externalDeliveryAttemptForClaim(claim, attempt),
		ReportingCapability:            capability.SQLHex(), Outcome: string(outcome),
	}
	encoded, err := encodeRecordPurgeCommand(command)
	if err != nil || command.AttemptID == "" {
		return recordcollaboration.ExternalDeliveryFactLocator{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	var fact recordcollaboration.ExternalDeliveryFactLocator
	err = repository.platform.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		fact = recordcollaboration.ExternalDeliveryFactLocator{}
		var digest []byte
		if err := transaction.tx.QueryRow(ctx, `select attempt_id,evidence_digest from public.record_external_delivery_outcome($1)`, encoded).Scan(&fact.AttemptID, &digest); err != nil {
			return fmt.Errorf("record external delivery outcome: %w", externalDeliveryAuthorityError(err))
		}
		if len(digest) != len(fact.EvidenceDigest) || fact.AttemptID != command.AttemptID {
			return recordcollaboration.ErrInvalidExternalDeliveryResult
		}
		copy(fact.EvidenceDigest[:], digest)
		return fact.Validate()
	})
	if err != nil {
		return recordcollaboration.ExternalDeliveryFactLocator{}, err
	}
	return fact, nil
}

func (repository *PostgresRecordNotificationRepository) FinalizeExternalDelivery(
	ctx context.Context,
	claim recordplatform.ClaimedOutboxEventV1,
	attempt recordcollaboration.ExternalDeliveryAttempt,
	fact recordcollaboration.ExternalDeliveryFactLocator,
	retryAfter time.Duration,
) (recordcollaboration.ExternalDeliveryOutboxResult, error) {
	if ctx == nil || repository == nil || repository.platform == nil ||
		!validExternalDeliveryClaimAttempt(claim, attempt) || fact.Validate() != nil ||
		fact.AttemptID != recordcollaboration.NotificationDeliveryAttemptID(attempt.DeliveryID, attempt.Attempt) ||
		retryAfter < 0 || (retryAfter > 0 && retryAfter.Microseconds() <= 0) ||
		(attempt.Attempt == recordcollaboration.MaxNotificationDeliveryAttempts && retryAfter != 0) {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	command := externalDeliveryFinalizeCommand{
		externalDeliveryAttemptCommand: externalDeliveryAttemptForClaim(claim, attempt),
		EvidenceDigest:                 hex.EncodeToString(fact.EvidenceDigest[:]), RetryAfterUS: retryAfter.Microseconds(),
	}
	encoded, err := encodeRecordPurgeCommand(command)
	if err != nil {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	var result recordcollaboration.ExternalDeliveryOutboxResult
	err = repository.platform.runRecordMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		result = recordcollaboration.ExternalDeliveryOutboxResult{}
		var err error
		result, err = scanExternalDeliveryFinalization(transaction.tx.QueryRow(ctx, `select disposition,retry_after_us,next_attempt_at from public.record_external_delivery_finalize($1)`, encoded))
		if err != nil {
			return fmt.Errorf("finalize external delivery: %w", externalDeliveryAuthorityError(err))
		}
		return nil
	})
	if err != nil {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, err
	}
	return result, nil
}

func validExternalDeliveryClaimAttempt(claim recordplatform.ClaimedOutboxEventV1, attempt recordcollaboration.ExternalDeliveryAttempt) bool {
	return claim.Validate() == nil && attempt.Validate() == nil &&
		claim.Event.EventKind == recordplatform.OutboxEventKindRecordNotificationDelivery &&
		claim.Event.SubjectKind == recordplatform.OutboxSubjectKindDelivery && attempt.DeliveryID == claim.Event.SubjectID &&
		attempt.SourceVersion == claim.Event.SourceVersion && attempt.AuthorizationEpoch == claim.Event.AuthorizationEpoch &&
		attempt.RecordFenceEpoch == claim.Event.RecordFenceEpoch && claim.Owner.ExpiresAt.Nanosecond()%1000 == 0 &&
		claim.ExpiresAt.Nanosecond()%1000 == 0 && attempt.StartedAt.Nanosecond()%1000 == 0
}

func externalDeliveryAttemptForClaim(claim recordplatform.ClaimedOutboxEventV1, attempt recordcollaboration.ExternalDeliveryAttempt) externalDeliveryAttemptCommand {
	row := externalDeliveryRow{
		deliveryID: attempt.DeliveryID, projectID: claim.Event.ProjectID, recordID: attempt.RecordID,
		notificationID: attempt.NotificationID, recipientUserID: attempt.RecipientUserID,
		channel: attempt.Channel, bindingID: attempt.BindingID, sourceVersion: attempt.SourceVersion,
		authorizationEpoch: attempt.AuthorizationEpoch, recordFenceEpoch: attempt.RecordFenceEpoch,
	}
	return externalDeliveryAttemptCommand{
		externalDeliveryAuthorityCommand: externalDeliveryAuthority(claim, row),
		AttemptNo:                        attempt.Attempt, StartedAt: attempt.StartedAt.UTC().Format(time.RFC3339Nano),
		AttemptID: recordcollaboration.NotificationDeliveryAttemptID(attempt.DeliveryID, attempt.Attempt),
	}
}

func scanExternalDeliveryFinalization(row pgx.Row) (recordcollaboration.ExternalDeliveryOutboxResult, error) {
	var disposition string
	var retryAfterUS int64
	var nextAttemptAt *time.Time
	if err := row.Scan(&disposition, &retryAfterUS, &nextAttemptAt); err != nil {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, err
	}
	if retryAfterUS < 0 || retryAfterUS > math.MaxInt64/int64(time.Microsecond) {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	result := recordcollaboration.ExternalDeliveryOutboxResult{
		Disposition: recordcollaboration.ExternalDeliveryOutboxDisposition(disposition),
		RetryAfter:  time.Duration(retryAfterUS) * time.Microsecond, QueueFinalized: true,
	}
	if nextAttemptAt != nil {
		result.NextAttemptAt = nextAttemptAt.UTC()
	}
	return result, result.Validate()
}

func loadExternalDelivery(ctx context.Context, tx pgx.Tx, deliveryID string) (externalDeliveryRow, error) {
	row := externalDeliveryRow{deliveryID: deliveryID}
	var channel, state string
	var sourceVersion, authorizationEpoch, recordFenceEpoch, attemptCount int64
	query := `
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
		where deliveries.delivery_id = $1`
	err := tx.QueryRow(ctx, query, deliveryID).Scan(
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

type externalDeliveryAuthorityCommand struct {
	OutboxRowID        int64  `json:"outbox_row_id"`
	OwnerID            string `json:"owner_id"`
	OwnerGeneration    uint64 `json:"owner_generation"`
	OwnerExpiresAt     string `json:"owner_expires_at"`
	EventExpiresAt     string `json:"event_expires_at"`
	ProjectID          string `json:"project_id"`
	EventKind          string `json:"event_kind"`
	SubjectKind        string `json:"subject_kind"`
	DeliveryID         string `json:"delivery_id"`
	RecordID           string `json:"record_id"`
	NotificationID     string `json:"notification_id"`
	RecipientUserID    string `json:"recipient_user_id"`
	Channel            string `json:"channel"`
	BindingID          string `json:"binding_id"`
	SourceVersion      uint64 `json:"source_version"`
	AuthorizationEpoch uint64 `json:"authorization_epoch"`
	RecordFenceEpoch   uint64 `json:"record_fence_epoch"`
}

func externalDeliveryAuthority(claim recordplatform.ClaimedOutboxEventV1, row externalDeliveryRow) externalDeliveryAuthorityCommand {
	return externalDeliveryAuthorityCommand{
		OutboxRowID: claim.Event.RowID, OwnerID: claim.Owner.OwnerID, OwnerGeneration: claim.Owner.Generation,
		OwnerExpiresAt: claim.Owner.ExpiresAt.UTC().Format(time.RFC3339Nano), EventExpiresAt: claim.ExpiresAt.UTC().Format(time.RFC3339Nano),
		ProjectID: claim.Event.ProjectID, EventKind: claim.Event.EventKind, SubjectKind: claim.Event.SubjectKind,
		DeliveryID: row.deliveryID, RecordID: row.recordID, NotificationID: row.notificationID,
		RecipientUserID: row.recipientUserID, Channel: string(row.channel), BindingID: row.bindingID,
		SourceVersion: row.sourceVersion, AuthorizationEpoch: row.authorizationEpoch, RecordFenceEpoch: row.recordFenceEpoch,
	}
}

func externalDeliveryAuthorityError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55000" && pgErr.Message == "external delivery outcome authority unavailable" {
		return recordplatform.ErrLostOwnerLease
	}
	return err
}

func startExternalDeliveryAttempt(ctx context.Context, tx pgx.Tx, claim recordplatform.ClaimedOutboxEventV1, row externalDeliveryRow) (recordcollaboration.ExternalDeliveryAttempt, recordcollaboration.ExternalDeliveryReportingCapability, bool, error) {
	if row.attemptCount >= recordcollaboration.MaxNotificationDeliveryAttempts {
		return recordcollaboration.ExternalDeliveryAttempt{}, recordcollaboration.ExternalDeliveryReportingCapability{}, false, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	command := struct {
		externalDeliveryAuthorityCommand
		ExpectedState        string `json:"expected_state"`
		ExpectedAttemptCount uint8  `json:"expected_attempt_count"`
	}{externalDeliveryAuthority(claim, row), string(row.state), row.attemptCount}
	encoded, err := encodeRecordPurgeCommand(command)
	if err != nil {
		return recordcollaboration.ExternalDeliveryAttempt{}, recordcollaboration.ExternalDeliveryReportingCapability{}, false, err
	}
	var attemptNo int64
	var startedAt time.Time
	var rawCapability []byte
	err = tx.QueryRow(ctx, `select attempt_no,started_at,reporting_capability from public.record_external_delivery_start($1)`, encoded).Scan(&attemptNo, &startedAt, &rawCapability)
	if err != nil {
		return recordcollaboration.ExternalDeliveryAttempt{}, recordcollaboration.ExternalDeliveryReportingCapability{}, false, fmt.Errorf("start external delivery attempt: %w", externalDeliveryAuthorityError(err))
	}
	queueFinalized := rawCapability == nil
	expectedAttempt := int64(row.attemptCount) + 1
	if queueFinalized {
		if row.state != recordcollaboration.NotificationDeliveryRetryWait || row.attemptCount == 0 {
			return recordcollaboration.ExternalDeliveryAttempt{}, recordcollaboration.ExternalDeliveryReportingCapability{}, false, recordcollaboration.ErrInvalidExternalDeliveryResult
		}
		expectedAttempt = int64(row.attemptCount)
	}
	if attemptNo != expectedAttempt {
		return recordcollaboration.ExternalDeliveryAttempt{}, recordcollaboration.ExternalDeliveryReportingCapability{}, false, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	attempt := recordcollaboration.ExternalDeliveryAttempt{
		DeliveryID: row.deliveryID, RecordID: row.recordID, NotificationID: row.notificationID,
		RecipientUserID: row.recipientUserID, Channel: row.channel, BindingID: row.bindingID,
		SourceVersion: row.sourceVersion, AuthorizationEpoch: row.authorizationEpoch,
		RecordFenceEpoch: row.recordFenceEpoch, Attempt: uint8(attemptNo), StartedAt: startedAt.UTC(),
	}
	if queueFinalized && attempt.Validate() == nil {
		return attempt, recordcollaboration.ExternalDeliveryReportingCapability{}, true, nil
	}
	capability, err := recordcollaboration.ParseExternalDeliveryReportingCapability(rawCapability)
	if attempt.Validate() != nil || err != nil {
		return recordcollaboration.ExternalDeliveryAttempt{}, recordcollaboration.ExternalDeliveryReportingCapability{}, false, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	return attempt, capability, false, nil
}

func prepareCancelledExternalDelivery(ctx context.Context, tx pgx.Tx, claim recordplatform.ClaimedOutboxEventV1, row externalDeliveryRow, reason string, preparation *recordcollaboration.ExternalDeliveryPreparation) error {
	command := struct {
		externalDeliveryAuthorityCommand
		ExpectedState        string `json:"expected_state"`
		ExpectedAttemptCount uint8  `json:"expected_attempt_count"`
		Reason               string `json:"reason"`
	}{externalDeliveryAuthority(claim, row), string(row.state), row.attemptCount, reason}
	encoded, err := encodeRecordPurgeCommand(command)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select public.record_external_delivery_cancel($1)`, encoded); err != nil {
		return fmt.Errorf("cancel external delivery: %w", externalDeliveryAuthorityError(err))
	}
	*preparation = recordcollaboration.ExternalDeliveryPreparation{Result: recordcollaboration.ExternalDeliveryOutboxResult{Disposition: recordcollaboration.ExternalDeliveryOutboxCancel}}
	return nil
}

func reconcileExternalDeliveryTakeover(ctx context.Context, tx pgx.Tx, claim recordplatform.ClaimedOutboxEventV1, row externalDeliveryRow, retryBaseDelay time.Duration) (recordcollaboration.ExternalDeliveryOutboxResult, error) {
	if row.attemptCount == 0 || row.attemptStartedAt == nil {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	retryAfter := time.Duration(0)
	if row.attemptCount < recordcollaboration.MaxNotificationDeliveryAttempts {
		var retryable bool
		retryAfter, retryable = recordcollaboration.ExternalDeliveryRetryDelay(retryBaseDelay, row.attemptCount)
		if !retryable {
			return recordcollaboration.ExternalDeliveryOutboxResult{}, recordcollaboration.ErrInvalidExternalDeliveryResult
		}
	}
	command := externalDeliveryReconcileCommand{
		externalDeliveryAttemptCommand: externalDeliveryAttemptCommand{
			externalDeliveryAuthorityCommand: externalDeliveryAuthority(claim, row),
			AttemptNo:                        row.attemptCount, StartedAt: row.attemptStartedAt.UTC().Format(time.RFC3339Nano),
			AttemptID: recordcollaboration.NotificationDeliveryAttemptID(row.deliveryID, row.attemptCount),
		},
		RetryAfterUS: retryAfter.Microseconds(),
	}
	encoded, err := encodeRecordPurgeCommand(command)
	if err != nil || command.AttemptID == "" {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, recordcollaboration.ErrInvalidExternalDeliveryResult
	}
	result, err := scanExternalDeliveryFinalization(tx.QueryRow(ctx, `select disposition,retry_after_us,next_attempt_at from public.record_external_delivery_reconcile($1)`, encoded))
	if err != nil {
		return recordcollaboration.ExternalDeliveryOutboxResult{}, fmt.Errorf("reconcile external delivery takeover: %w", externalDeliveryAuthorityError(err))
	}
	return result, nil
}

var _ recordcollaboration.ExternalDeliveryStore = (*PostgresRecordNotificationRepository)(nil)
