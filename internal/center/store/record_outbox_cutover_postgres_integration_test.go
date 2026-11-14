package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationOutboxRestrictedCommands(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	first := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-sql-a")
	second := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-sql-b")
	queue := NewPostgresRecordPlatformRepository(runtime, gate)

	for _, statement := range []string{
		`insert into public.record_outbox (project_id,event_kind,subject_kind,subject_id,authorization_epoch,expires_at)
		 select 'default','record_created','record','rec_denied',0,clock_timestamp()+interval '1 hour' where false`,
		`update public.record_outbox set status=status where false`,
		`delete from public.record_outbox where false`,
	} {
		_, err := runtime.Exec(ctx, statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("raw outbox DML %q = %v, want insufficient privilege", statement, err)
		}
	}

	// Removing UPDATE must not remove the narrowly scoped row-lock operation.
	var existingID int64
	if err := runtime.QueryRow(ctx, `select outbox_row_id from public.record_outbox where subject_id=$1`, first.RecordID).Scan(&existingID); err != nil {
		t.Fatal(err)
	}
	locker, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locker.Rollback(context.Background()) }()
	if _, err := locker.Exec(ctx, `select pg_advisory_xact_lock_shared(1213154899,81)`); err != nil {
		t.Fatal(err)
	}
	lockBody, err := json.Marshal(map[string]any{"family": "outbox", "outbox_row_id": existingID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Exec(ctx, `select public.record_mutation_rows_lock($1::bytea)`, lockBody); err != nil {
		t.Fatalf("protected outbox lock: %v", err)
	}
	probe, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = probe.Exec(ctx, `select 1 from public.record_outbox where outbox_row_id=$1 for update nowait`, existingID)
	var lockErr *pgconn.PgError
	if !errors.As(err, &lockErr) || lockErr.Code != "55P03" {
		_ = probe.Rollback(ctx)
		t.Fatalf("competing outbox lock = %v, want lock unavailable", err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := locker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.db.Exec(ctx, `insert into public.record_actions
		(action_id,record_id,action_version,title,status,created_by,updated_by,record_fence_epoch)
		values ('ract_sqlroute',$1,1,'SQL route','open','usr_aaaaaaaaaaaaaaaaaaaaaaaa','usr_aaaaaaaaaaaaaaaaaaaaaaaa',0)`, first.RecordID); err != nil {
		t.Fatal(err)
	}
	event := recordplatform.OutboxEvent{ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordActionCreated,
		SubjectKind: recordplatform.OutboxSubjectKindAction, SubjectID: "ract_sqlroute", AuthorizationEpoch: first.AuthorizationEpoch}
	command := outboxEventCommand(event, second.RecordID)
	command["expires_after_us"] = time.Hour.Microseconds()
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	var rowID int64
	err = runtime.QueryRow(ctx, `select outbox_row_id from public.record_outbox_enqueue($1::bytea)`, body).Scan(&rowID)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
		t.Fatalf("enqueue against another record's route = %v, want serialization failure", err)
	}
	var rows int
	if err := runtime.QueryRow(ctx, `select count(*) from public.record_outbox where subject_id='ract_sqlroute'`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("wrong-route event count = %d, %v", rows, err)
	}

	if _, err := queue.AcquireDeletionFenceLease(ctx, recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
		recordplatform.LeaseClaimInputV1{OwnerID: "outbox_sql_fence", LeaseDuration: time.Minute}); err != nil {
		t.Fatal(err)
	}
	command["record_id"] = first.RecordID
	body, err = json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	err = runtime.QueryRow(ctx, `select outbox_row_id from public.record_outbox_enqueue($1::bytea)`, body).Scan(&rowID)
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("enqueue behind real deletion fence = %v, want unavailable", err)
	}
	// Duplicate fields must be rejected before jsonb can discard one value.
	duplicate := append([]byte(`{"project_id":"default",`), body[1:]...)
	err = runtime.QueryRow(ctx, `select outbox_row_id from public.record_outbox_enqueue($1::bytea)`, duplicate).Scan(&rowID)
	if !errors.As(err, &pgErr) || pgErr.Code != "22023" {
		t.Fatalf("duplicate command fields = %v, want invalid parameter", err)
	}
}

func TestPostgresIntegrationOutboxDirectOwnerAndTerminalRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	parent := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-terminal-parent")
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	if _, err := fixture.db.Exec(ctx, `insert into public.record_actions
		(action_id,record_id,action_version,title,status,created_by,updated_by,record_fence_epoch)
		values ('ract_terminalroute',$1,1,'Terminal route','open','usr_aaaaaaaaaaaaaaaaaaaaaaaa','usr_aaaaaaaaaaaaaaaaaaaaaaaa',0)`, parent.RecordID); err != nil {
		t.Fatal(err)
	}
	if err := queue.runSourceAuthorizedMutationTransaction(ctx, func(ctx context.Context, transaction *RecordPlatformTransaction) error {
		acquired, err := acquireEffectMutationResources(ctx, transaction.tx, []string{parent.RecordID}, nil, records.RevisionMutationResources{})
		if err != nil {
			return err
		}
		transaction.registerAcquiredRecordMutationResources(acquired)
		_, err = transaction.EnqueueOutbox(ctx, recordplatform.OutboxEnqueueInputV1{Event: recordplatform.OutboxEvent{
			ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordActionAssigned,
			SubjectKind: recordplatform.OutboxSubjectKindAction, SubjectID: "ract_terminalroute",
			SourceVersion: 1, AuthorizationEpoch: parent.AuthorizationEpoch,
		}, ExpiresAfter: time.Hour})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	claim := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionAssigned, "terminal_route_owner")
	tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := readOutboxCandidate(ctx, tx, claim.Event.RowID, false)
	if rollbackErr := tx.Rollback(ctx); err == nil {
		err = rollbackErr
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, alter := range []struct {
		name string
		edit func(*outboxCandidate)
	}{
		{name: "owner", edit: func(c *outboxCandidate) { c.ownerID = "other_owner" }},
		{name: "generation", edit: func(c *outboxCandidate) { c.ownerGeneration++ }},
		{name: "observed expiry", edit: func(c *outboxCandidate) { expiry := c.ownerExpiresAt.Add(time.Microsecond); c.ownerExpiresAt = &expiry }},
		{name: "event expiry", edit: func(c *outboxCandidate) { c.expiresAt = c.expiresAt.Add(time.Microsecond) }},
	} {
		wrong := candidate
		alter.edit(&wrong)
		body, err := outboxCandidateCommand(wrong, parent.RecordID, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		var rowID int64
		err = runtime.QueryRow(ctx, `select outbox_row_id from public.record_outbox_sent($1::bytea)`, body).Scan(&rowID)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("sent with wrong %s = %v, want no transition", alter.name, err)
		}
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.record_actions where action_id='ract_terminalroute'`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.content_delivery_epochs
		where project_id='default' and object_kind='record' and object_id=$1`, parent.RecordID); err != nil {
		t.Fatal(err)
	}
	if err := queue.CancelOutbox(ctx, claim); !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		t.Fatalf("terminal outcome without epoch or retained V2 gate = %v, want missing scope", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch)
		values ('default','record',$1,0)`, parent.RecordID); err != nil {
		t.Fatal(err)
	}
	if err := queue.CancelOutbox(ctx, claim); err != nil {
		t.Fatalf("cancel after subject removal: %v", err)
	}
	var status, route string
	if err := runtime.QueryRow(ctx, `select status,record_id from public.record_outbox where outbox_row_id=$1`, claim.Event.RowID).Scan(&status, &route); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || route != parent.RecordID {
		t.Fatalf("terminal route = %s/%s, want cancelled/%s", status, route, parent.RecordID)
	}
	if err := queue.RetryOutbox(ctx, claim, time.Second); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("reopen cancelled outbox = %v, want lost owner", err)
	}

	// A predecessor row with no persisted route and no subject cannot borrow
	// a current record's scope or be treated as an already-drained effect.
	var legacyID int64
	var ownerExpiry, eventExpiry time.Time
	if err := fixture.db.QueryRow(ctx, `insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,source_version,authorization_epoch,record_fence_epoch,
		 status,owner_id,owner_generation,owner_expires_at,expires_at)
		values ('default','record_action_assigned','action','ract_legacy_missing',1,$1,0,
		 'processing','legacy_owner',1,clock_timestamp()+interval '1 minute',clock_timestamp()+interval '1 hour')
		returning outbox_row_id,owner_expires_at,expires_at`, int64(parent.AuthorizationEpoch)).Scan(&legacyID, &ownerExpiry, &eventExpiry); err != nil {
		t.Fatal(err)
	}
	legacy := recordplatform.ClaimedOutboxEventV1{Event: recordplatform.OutboxEvent{
		RowID: legacyID, ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordActionAssigned,
		SubjectKind: recordplatform.OutboxSubjectKindAction, SubjectID: "ract_legacy_missing",
		SourceVersion: 1, AuthorizationEpoch: parent.AuthorizationEpoch,
	}, Owner: recordplatform.OwnerLease{OwnerID: "legacy_owner", Generation: 1, ExpiresAt: ownerExpiry}, ExpiresAt: eventExpiry}
	if err := queue.CancelOutbox(ctx, legacy); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("unroutable predecessor outcome = %v, want lost owner", err)
	}
	var legacyStatus string
	var legacyRoute *string
	if err := runtime.QueryRow(ctx, `select status,record_id from public.record_outbox where outbox_row_id=$1`, legacyID).Scan(&legacyStatus, &legacyRoute); err != nil || legacyStatus != "processing" || legacyRoute != nil {
		t.Fatalf("unroutable predecessor = %q/%v, %v", legacyStatus, legacyRoute, err)
	}

	if _, err := fixture.db.Exec(ctx, `insert into public.record_actions
		(action_id,record_id,action_version,title,status,created_by,updated_by,record_fence_epoch)
		values ('ract_legacylive',$1,1,'Legacy live','open','usr_aaaaaaaaaaaaaaaaaaaaaaaa','usr_aaaaaaaaaaaaaaaaaaaaaaaa',0)`, parent.RecordID); err != nil {
		t.Fatal(err)
	}
	var liveLegacyID int64
	if err := fixture.db.QueryRow(ctx, `insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,source_version,authorization_epoch,record_fence_epoch,expires_at)
		values ('default','record_action_assigned','action','ract_legacylive',1,$1,0,clock_timestamp()+interval '1 hour')
		returning outbox_row_id`, int64(parent.AuthorizationEpoch)).Scan(&liveLegacyID); err != nil {
		t.Fatal(err)
	}
	liveLegacy := claimNotificationOutboxKind(t, ctx, queue, recordplatform.OutboxEventKindRecordActionAssigned, "legacy_live_owner")
	if liveLegacy.Event.RowID != liveLegacyID {
		t.Fatalf("claimed legacy row %d, want %d", liveLegacy.Event.RowID, liveLegacyID)
	}
	if err := runtime.QueryRow(ctx, `select record_id from public.record_outbox where outbox_row_id=$1`, liveLegacyID).Scan(&route); err != nil || route != parent.RecordID {
		t.Fatalf("live predecessor route = %q, %v; want %q", route, err, parent.RecordID)
	}
}

// The synthetic gates below exercise the root-independent scope check only;
// they do not represent successful issuer or birth admission.
func TestPostgresIntegrationOutboxTerminalRequiresMatchingRetainedGate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	first := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-retained-a")
	second := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-retained-b")
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "retained_gate_owner", OwnerLeaseDuration: time.Minute})
	if err != nil || claim == nil || claim.Event.SubjectID != first.RecordID {
		t.Fatalf("claim before gate = %#v, %v", claim, err)
	}
	seedLocalAttachmentDeletionOperation(t, ctx, fixture, recorddeletion.DeletionOperation{
		OperationID: "rpo_outboxscopeb", ReservationID: "drs_outboxscopeb", FenceEpoch: 1,
		Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: second.RecordID},
	}, second.RevisionID)
	if _, err := fixture.db.Exec(ctx, `delete from public.content_delivery_epochs
		where project_id='default' and object_kind='record' and object_id=$1`, first.RecordID); err != nil {
		t.Fatal(err)
	}
	if err := queue.CancelOutbox(ctx, *claim); !errors.Is(err, recordplatform.ErrContentDeliveryEpochMissing) {
		t.Fatalf("another record's gate authorized terminal outcome: %v", err)
	}
	seedLocalAttachmentDeletionOperation(t, ctx, fixture, recorddeletion.DeletionOperation{
		OperationID: "rpo_outboxscopea", ReservationID: "drs_outboxscopea", FenceEpoch: 1,
		Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: first.RecordID},
	}, first.RevisionID)
	if err := queue.CancelOutbox(ctx, *claim); err != nil {
		t.Fatalf("terminal outcome with matching retained scope: %v", err)
	}
	var status string
	if err := runtime.QueryRow(ctx, `select status from public.record_outbox where outbox_row_id=$1`, claim.Event.RowID).Scan(&status); err != nil || status != "cancelled" {
		t.Fatalf("terminal outcome with retained gate = %q, %v", status, err)
	}
}

func TestPostgresIntegrationOutboxClaimSkipsContendedRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	blocked := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-contended-z")
	available := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "outbox-contended-a")
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(context.Background()) }()
	if _, err := holder.Exec(ctx, `select 1 from public.content_delivery_epochs where project_id='default'
		and object_kind='record' and object_id=$1 for update`, blocked.RecordID); err != nil {
		t.Fatal(err)
	}
	queue := NewPostgresRecordPlatformRepository(runtime, gate)
	claim, err := queue.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{OwnerID: "contended_candidate", OwnerLeaseDuration: time.Minute})
	if err != nil || claim == nil || claim.Event.SubjectID != available.RecordID {
		t.Fatalf("claim past contended earlier candidate = %#v, %v; want %s", claim, err, available.RecordID)
	}
	var state string
	if err := runtime.QueryRow(ctx, `select status from public.record_outbox where subject_id=$1`, blocked.RecordID).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("contended candidate state = %s, error = %v", state, err)
	}
}
