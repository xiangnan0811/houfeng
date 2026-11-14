package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/recordplatform"
)

// Outbox subjects carry no record authority. Resolve their persisted owner in
// an unlocked read, then repeat this read after acquiring that record's ranks.
func resolveOutboxRecordRoute(ctx context.Context, tx pgx.Tx, event recordplatform.OutboxEvent) (string, error) {
	var query string
	switch event.SubjectKind {
	case recordplatform.OutboxSubjectKindRecord:
		if !recordidentity.IsValid(event.SubjectID) {
			return "", recordplatform.ErrInvalidOutboxEvent
		}
		query = `select record_id from public.records where project_id=$1 and record_id=$2`
	case recordplatform.OutboxSubjectKindAction:
		query = `select record_id from public.record_actions where project_id=$1 and action_id=$2`
	case recordplatform.OutboxSubjectKindComment:
		query = `select record_id from public.record_comments where project_id=$1 and comment_id=$2`
	case recordplatform.OutboxSubjectKindDelivery:
		query = `select delivery.record_id from public.record_notification_deliveries delivery
			join public.record_notifications notification on notification.record_id=delivery.record_id and notification.notification_id=delivery.notification_id
			where notification.project_id=$1 and delivery.delivery_id=$2`
	default:
		return "", recordplatform.ErrInvalidOutboxEvent
	}
	var recordID string
	if err := tx.QueryRow(ctx, query, event.ProjectID, event.SubjectID).Scan(&recordID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", recordplatform.ErrLostOwnerLease
		}
		return "", fmt.Errorf("resolve outbox record route: %w", err)
	}
	if !recordidentity.IsValid(recordID) {
		return "", recordplatform.ErrInvalidOutboxEvent
	}
	return recordID, nil
}

func (transaction *RecordPlatformTransaction) requireAcquiredOutboxRoute(ctx context.Context, event recordplatform.OutboxEvent) (string, error) {
	recordID, err := resolveOutboxRecordRoute(ctx, transaction.tx, event)
	if err != nil {
		return "", err
	}
	if _, acquired := transaction.mutationRecords[event.ProjectID+"\x00"+recordID]; !acquired {
		return "", recordplatform.ErrLostOwnerLease
	}
	return recordID, nil
}

type outboxCandidate struct {
	event           recordplatform.OutboxEvent
	recordID        *string
	status          string
	ownerID         string
	ownerGeneration int64
	ownerExpiresAt  *time.Time
	attemptCount    int64
	nextAttemptAt   time.Time
	expiresAt       time.Time
}

func readOutboxCandidate(ctx context.Context, tx pgx.Tx, rowID int64, lock bool) (outboxCandidate, error) {
	if lock {
		if err := lockRecordMutationProtectedRow(ctx, tx, map[string]any{"family": "outbox", "outbox_row_id": rowID}); err != nil {
			return outboxCandidate{}, err
		}
	}
	const query = `select outbox_row_id,project_id,event_kind,subject_kind,subject_id,
		source_version,authorization_epoch,record_fence_epoch,record_id,status,owner_id,owner_generation,
		owner_expires_at,attempt_count,next_attempt_at,expires_at
		from public.record_outbox where outbox_row_id=$1`
	var candidate outboxCandidate
	var sourceVersion, authorizationEpoch, fenceEpoch int64
	err := tx.QueryRow(ctx, query, rowID).Scan(
		&candidate.event.RowID, &candidate.event.ProjectID, &candidate.event.EventKind,
		&candidate.event.SubjectKind, &candidate.event.SubjectID, &sourceVersion, &authorizationEpoch, &fenceEpoch,
		&candidate.recordID, &candidate.status, &candidate.ownerID, &candidate.ownerGeneration, &candidate.ownerExpiresAt,
		&candidate.attemptCount, &candidate.nextAttemptAt, &candidate.expiresAt)
	if err != nil {
		return candidate, err
	}
	if sourceVersion < 0 || authorizationEpoch < 0 || fenceEpoch < 0 || candidate.ownerGeneration < 0 || candidate.attemptCount < 0 {
		return candidate, recordplatform.ErrInvalidOutboxClaim
	}
	candidate.event.SourceVersion = uint64(sourceVersion)
	candidate.event.AuthorizationEpoch = uint64(authorizationEpoch)
	candidate.event.RecordFenceEpoch = uint64(fenceEpoch)
	if err := candidate.event.Validate(); err != nil {
		return candidate, err
	}
	return candidate, nil
}

// The persisted route wins over a subsequently changed subject. A missing
// subject is usable only for a terminal fact that already has this route.
func resolveOutboxCandidateRoute(ctx context.Context, tx pgx.Tx, candidate outboxCandidate, terminal bool) (string, error) {
	actual, err := resolveOutboxRecordRoute(ctx, tx, candidate.event)
	if candidate.recordID == nil {
		return actual, err
	}
	if !recordidentity.IsValid(*candidate.recordID) {
		return "", recordplatform.ErrInvalidOutboxEvent
	}
	if errors.Is(err, recordplatform.ErrLostOwnerLease) && terminal {
		return *candidate.recordID, nil
	}
	if err != nil {
		return "", err
	}
	if actual != *candidate.recordID {
		return "", &pgconn.PgError{Code: "40001", Message: "outbox persisted subject route changed; restart transaction"}
	}
	return actual, nil
}

func outboxEventCommand(event recordplatform.OutboxEvent, recordID string) map[string]any {
	return map[string]any{
		"project_id": event.ProjectID, "event_kind": event.EventKind, "subject_kind": event.SubjectKind,
		"subject_id": event.SubjectID, "source_version": event.SourceVersion,
		"authorization_epoch": event.AuthorizationEpoch, "record_fence_epoch": event.RecordFenceEpoch,
		"record_id": recordID,
	}
}

func outboxCandidateCommand(candidate outboxCandidate, recordID, newOwner string, delay time.Duration) ([]byte, error) {
	command := outboxEventCommand(candidate.event, recordID)
	command["outbox_row_id"] = candidate.event.RowID
	command["status"] = candidate.status
	command["owner_id"] = candidate.ownerID
	command["owner_generation"] = candidate.ownerGeneration
	command["owner_expires_at"] = nil
	if candidate.ownerExpiresAt != nil {
		command["owner_expires_at"] = candidate.ownerExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	command["attempt_count"] = candidate.attemptCount
	command["next_attempt_at"] = candidate.nextAttemptAt.UTC().Format(time.RFC3339Nano)
	command["event_expires_at"] = candidate.expiresAt.UTC().Format(time.RFC3339Nano)
	command["new_owner_id"] = newOwner
	command["delay_us"] = delay.Microseconds()
	return json.Marshal(command)
}

// The outbox gate lock is a nonblocking protected row family. A missing epoch
// is accepted only for a terminal observation backed by the retained V2 gate.
func acquireOutboxRecordPrefix(ctx context.Context, tx pgx.Tx, acquired recordMutationResources, recordID string, fenceEpoch uint64, terminal bool) error {
	err := acquireRecordMutationResourceRangeMode(ctx, tx, acquired, 10, 40, true)
	missingEpoch := errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing)
	if err != nil && (!terminal || !missingEpoch) {
		return err
	}
	if missingEpoch {
		// Acquisition stopped at rank 30, so the lease scopes still precede the gate.
		if err := acquireRecordMutationResourceRangeMode(ctx, tx, acquired, 40, 40, true); err != nil {
			return err
		}
	}
	if err := lockRecordMutationProtectedRow(ctx, tx, map[string]any{
		"family": "outbox_gate", "project_id": "default", "record_id": recordID,
	}); err != nil {
		return err
	}
	if missingEpoch {
		var retained bool
		if err := tx.QueryRow(ctx, `select not exists (
			select 1 from public.content_delivery_epochs e where e.project_id='default' and e.object_kind='record' and e.object_id=$1
		) and exists (select 1 from public.record_deletion_gates g
			join public.record_purge_operations o on o.operation_id=g.operation_id
			join public.deletion_reservations r on r.reservation_id=o.reservation_id
			where g.project_id='default' and g.record_id=$1 and g.fence_epoch >= $2
			  and o.deletion_contract_version=2 and o.project_id=g.project_id
			  and r.deletion_contract_version=2 and r.project_id=g.project_id and r.object_kind='record'
			  and r.object_id=g.record_id and r.fence_epoch=g.fence_epoch and r.state in ('fenced','committed'))`, recordID, int64(fenceEpoch)).Scan(&retained); err != nil {
			return err
		} else if !retained {
			return recordplatform.ErrContentDeliveryEpochMissing
		}
	}
	if err := acquireRecordMutationResourceRangeMode(ctx, tx, acquired, 60, 60, true); err != nil {
		return err
	}
	return recheckRecordMutationFenceState(ctx, tx, acquired.resources, terminal)
}

// Unlike creation of a new record, an outbox effect cannot initialize an
// absent epoch. Acquire the existing serialization scope and then recheck the
// open gate, root and discovered source route before taking later ranks.
func acquireOutboxOpenRecordResources(ctx context.Context, tx pgx.Tx, recordID string) (recordMutationResources, error) {
	before, err := discoverEffectRecordResources(ctx, tx, []string{recordID}, nil)
	if err != nil {
		return recordMutationResources{}, err
	}
	acquired, err := normalizeRecordMutationDeclarations(before)
	if err != nil {
		return recordMutationResources{}, err
	}
	if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
		return recordMutationResources{}, err
	}
	if err := acquireOutboxRecordPrefix(ctx, tx, acquired, recordID, 0, false); err != nil {
		return recordMutationResources{}, err
	}
	if len(acquired.resources.RootObservations) != 1 || !acquired.resources.RootObservations[0].Exists || acquired.resources.RootObservations[0].CurrentRevisionID == "" {
		return recordMutationResources{}, recordplatform.ErrLostOwnerLease
	}
	after, err := discoverEffectRecordResources(ctx, tx, []string{recordID}, nil)
	if err != nil {
		return recordMutationResources{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return recordMutationResources{}, &pgconn.PgError{Code: "40001", Message: "outbox open record route changed; restart transaction"}
	}
	if err := recheckRecordMutationFenceState(ctx, tx, acquired.resources, false); err != nil {
		return recordMutationResources{}, err
	}
	return acquired, nil
}

func claimOutboxInTransaction(ctx context.Context, transaction *RecordPlatformTransaction, input recordplatform.OutboxClaimInputV1, rowID int64) (*recordplatform.ClaimedOutboxEventV1, error) {
	tx := transaction.tx
	observed, err := readOutboxCandidate(ctx, tx, rowID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	recordID, err := resolveOutboxCandidateRoute(ctx, tx, observed, false)
	if errors.Is(err, recordplatform.ErrLostOwnerLease) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A busy record must not starve unrelated queue entries. The timeout is
	// local to this transaction; failure rolls back the entire candidate.
	if _, err := tx.Exec(ctx, `select pg_catalog.set_config('lock_timeout','25ms',true)`); err != nil {
		return nil, err
	}
	acquired, err := acquireOutboxOpenRecordResources(ctx, tx, recordID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `select pg_catalog.set_config('lock_timeout','0',true)`); err != nil {
		return nil, err
	}
	if len(acquired.resources.RootObservations) != 1 || !acquired.resources.RootObservations[0].Exists {
		return nil, nil
	}
	currentRecordID, err := resolveOutboxCandidateRoute(ctx, tx, observed, false)
	if err != nil && !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		return nil, err
	}
	if err != nil || currentRecordID != recordID {
		return nil, &pgconn.PgError{Code: "40001", Message: "outbox subject route changed; restart claim"}
	}
	locked, err := readOutboxCandidate(ctx, tx, rowID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(observed, locked) {
		return nil, &pgconn.PgError{Code: "40001", Message: "outbox candidate changed; restart claim"}
	}
	currentRecordID, err = resolveOutboxCandidateRoute(ctx, tx, locked, false)
	if err != nil || currentRecordID != recordID {
		return nil, &pgconn.PgError{Code: "40001", Message: "outbox subject route changed after row lock"}
	}
	body, err := outboxCandidateCommand(locked, recordID, input.OwnerID, input.OwnerLeaseDuration)
	if err != nil {
		return nil, err
	}
	var row observedOutboxClaimRow
	err = tx.QueryRow(ctx, `select outbox_row_id,project_id,event_kind,subject_kind,subject_id,
		source_version,authorization_epoch,record_fence_epoch,owner_id,owner_generation,owner_expires_at,expires_at
		from public.record_outbox_claim($1::bytea)`, body).Scan(
		&row.rowID, &row.projectID, &row.eventKind, &row.subjectKind, &row.subjectID,
		&row.sourceVersion, &row.authorizationEpoch, &row.recordFenceEpoch,
		&row.ownerID, &row.ownerGeneration, &row.ownerExpiresAt, &row.expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim outbox row: %w", err)
	}
	var live bool
	if err := tx.QueryRow(ctx, `select owner_expires_at>clock_timestamp() and expires_at>clock_timestamp()
		from public.record_outbox where outbox_row_id=$1`, rowID).Scan(&live); err != nil {
		return nil, err
	}
	if !live {
		return nil, recordplatform.ErrLostOwnerLease
	}
	claim, err := row.claim()
	if err != nil {
		return nil, err
	}
	return &claim, nil
}

// Terminal outcomes can use the immutable route after the subject is removed.
// Nonterminal effects still require the actual live subject and open gate.
func acquireOutboxFinalizationRoute(ctx context.Context, transaction *RecordPlatformTransaction, claim recordplatform.ClaimedOutboxEventV1, terminal bool) (outboxCandidate, string, error) {
	observed, err := readOutboxCandidate(ctx, transaction.tx, claim.Event.RowID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return outboxCandidate{}, "", recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return outboxCandidate{}, "", err
	}
	if !observed.matchesClaim(claim) {
		return outboxCandidate{}, "", recordplatform.ErrLostOwnerLease
	}
	return acquireOutboxCandidateRoute(ctx, transaction, observed, terminal)
}

func acquireOutboxCandidateRoute(ctx context.Context, transaction *RecordPlatformTransaction, observed outboxCandidate, terminal bool) (outboxCandidate, string, error) {
	tx := transaction.tx
	recordID, err := resolveOutboxCandidateRoute(ctx, tx, observed, terminal)
	if err != nil {
		return outboxCandidate{}, "", err
	}
	var acquired recordMutationResources
	if terminal {
		before, err := discoverEffectRecordResources(ctx, tx, []string{recordID}, nil)
		if err != nil {
			return outboxCandidate{}, "", err
		}
		acquired, err = normalizeRecordMutationDeclarations(before)
		if err != nil {
			return outboxCandidate{}, "", err
		}
		if err := lockComparisonDecisionForWriter(ctx, tx); err != nil {
			return outboxCandidate{}, "", err
		}
		if err := acquireOutboxRecordPrefix(ctx, tx, acquired, recordID, observed.event.RecordFenceEpoch, true); err != nil {
			return outboxCandidate{}, "", err
		}
		after, err := discoverEffectRecordResources(ctx, tx, []string{recordID}, nil)
		if err != nil {
			return outboxCandidate{}, "", err
		}
		if !reflect.DeepEqual(before, after) {
			return outboxCandidate{}, "", &pgconn.PgError{Code: "40001", Message: "outbox terminal record route changed; restart transaction"}
		}
	} else {
		acquired, err = acquireOutboxOpenRecordResources(ctx, tx, recordID)
		if err != nil {
			return outboxCandidate{}, "", err
		}
	}
	transaction.registerAcquiredRecordMutationResources(acquired)
	current, err := readOutboxCandidate(ctx, tx, observed.event.RowID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return outboxCandidate{}, "", recordplatform.ErrLostOwnerLease
	}
	if err != nil {
		return outboxCandidate{}, "", err
	}
	if !reflect.DeepEqual(observed, current) {
		return outboxCandidate{}, "", recordplatform.ErrLostOwnerLease
	}
	currentRecordID, err := resolveOutboxCandidateRoute(ctx, tx, current, terminal)
	if err != nil || currentRecordID != recordID {
		return outboxCandidate{}, "", &pgconn.PgError{Code: "40001", Message: "outbox subject route changed; restart finalization"}
	}
	return current, recordID, nil
}

func (candidate outboxCandidate) matchesClaim(claim recordplatform.ClaimedOutboxEventV1) bool {
	return candidate.event == claim.Event && candidate.status == "processing" &&
		candidate.ownerID == claim.Owner.OwnerID && candidate.ownerGeneration >= 0 &&
		uint64(candidate.ownerGeneration) == claim.Owner.Generation && candidate.ownerExpiresAt != nil &&
		candidate.ownerExpiresAt.Equal(claim.Owner.ExpiresAt) && candidate.expiresAt.Equal(claim.ExpiresAt)
}
