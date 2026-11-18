package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordcollaboration"
)

// These tests exercise the restricted SQL commands and role boundaries. The
// predecessor rows are fixtures, not evidence of an authorized deletion or W1
// ledger operation.
func testPostgresExternalDeliveryFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP80Migration)
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	owner := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentReleasedGenesis(t, ctx, fixture, owner, profile)
	seedDeliveryFactsRoots(t, ctx, owner)

	// Seed an actual predecessor processing row before C81. There is no start
	// or capability to recover for this legacy attempt.
	legacy := seedDeliveryFactsPending(t, ctx, owner, 1, "rec_delivery", true, 10*time.Minute, time.Hour)
	var legacyStarted time.Time
	if err := owner.QueryRow(ctx, `update public.record_notification_deliveries set delivery_state='processing',
		attempt_count=1, attempt_started_at=clock_timestamp() where delivery_id=$1 returning attempt_started_at`, legacy["delivery_id"]).Scan(&legacyStarted); err != nil {
		t.Fatalf("seed predecessor processing delivery: %v", err)
	}
	if _, err := ConvergeAppACLCurrent(ctx, owner, fixture.runtimeRole, fixture.adminRole, fixture.deletionWorkerRole); err != nil {
		t.Fatal(err)
	}
	runtime := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	admin := fixture.openRolePool(t, ctx, fixture.adminRole)
	worker := fixture.openRolePool(t, ctx, fixture.deletionWorkerRole)
	if err := AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		t.Fatalf("admit runtime login: %v", err)
	}
	if err := AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
		t.Fatalf("admit deletion worker login: %v", err)
	}

	t.Run("retained_catalog_contract", func(t *testing.T) {
		assertDeliveryFactsCatalog(t, ctx, owner, fixture.migratorRole, fixture.runtimeRole, fixture.adminRole, fixture.deletionWorkerRole)
	})
	t.Run("retained_immutable_statements", func(t *testing.T) {
		assertDeliveryFactsImmutableStatements(t, ctx, owner)
	})
	t.Run("direct_roles_and_publication_shapes", func(t *testing.T) {
		assertDeliveryFactsRoleBoundaries(t, ctx, runtime, admin, worker)
	})
	t.Run("runtime_delivery_insert_guard", func(t *testing.T) {
		assertDeliveryFactsRuntimeInsertGuard(t, ctx, owner, runtime, fixture.db)
	})
	t.Run("cancel_and_predecessor_without_start", func(t *testing.T) {
		assertDeliveryFactsCancelAndLegacy(t, ctx, owner, runtime, legacy, legacyStarted)
	})
	t.Run("capability_and_command_shapes", func(t *testing.T) {
		assertDeliveryFactsStartAndShapes(t, ctx, owner, runtime, admin, worker)
	})
	t.Run("outcome_and_retry_replay", func(t *testing.T) {
		assertDeliveryFactsNormalOutcomes(t, ctx, owner, runtime, fixture.db)
	})
	t.Run("takeover_consumes_report", func(t *testing.T) {
		assertDeliveryFactsTakeover(t, ctx, owner, runtime, fixture.db)
	})
	t.Run("conflict_is_unknown", func(t *testing.T) {
		assertDeliveryFactsConflict(t, ctx, owner, runtime)
	})
	t.Run("late_conflict_closes_retry", func(t *testing.T) {
		contender := fixture.openRolePool(t, ctx, fixture.runtimeRole)
		if err := AdmitAppACLCurrentRuntime(ctx, contender); err != nil {
			t.Fatalf("admit retry contender login: %v", err)
		}
		assertDeliveryFactsLateConflictClosesRetry(t, ctx, owner, runtime, contender, fixture.db)
	})
	t.Run("late_fact_does_not_reopen_unknown", func(t *testing.T) {
		assertDeliveryFactsLateUnknown(t, ctx, owner, runtime)
	})
	t.Run("owner_expires_while_waiting", func(t *testing.T) {
		assertDeliveryFactsWaitExpiry(t, ctx, owner, runtime, fixture.db)
	})
	t.Run("commit_after_owner_expiry", func(t *testing.T) {
		assertDeliveryFactsCommitExpiry(t, ctx, owner, runtime, fixture.db)
	})
	t.Run("epoch_drift_and_missing_epoch", func(t *testing.T) {
		assertDeliveryFactsEpochBoundaries(t, ctx, owner, runtime, fixture.db)
	})
	t.Run("event_expires_after_start", func(t *testing.T) {
		assertDeliveryFactsEventExpiry(t, ctx, owner, runtime, fixture.db)
	})
	t.Run("late_report_after_root_and_queue_removal", func(t *testing.T) {
		assertDeliveryFactsLateAfterRemoval(t, ctx, owner, runtime, fixture.db)
	})
}

type deliveryFactsStart struct {
	claim      map[string]any
	attempt    map[string]any
	startBody  []byte
	capability []byte
}

type deliveryFactsFinalization struct {
	disposition   string
	delayUS       int64
	nextAttemptAt *time.Time
}

func deliveryFactsCopy(values map[string]any) map[string]any {
	copyValues := make(map[string]any, len(values)+5)
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func deliveryFactsJSON(t *testing.T, values map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func deliveryFactsTimestamp(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func seedDeliveryFactsRoots(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `insert into public.users (user_id,username,password_hash,display_name,role)
		values ('usr_deliveryauthor','delivery-author','fixture','Delivery author','admin')`); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"delivery", "deliverylate", "deliveryepoch"} {
		recordID, revisionID := "rec_"+suffix, "rrv_"+suffix
		notificationID := deliveryFactsNotification(recordID)
		statements := []struct {
			sql  string
			args []any
		}{
			{`insert into public.records (record_id,lock_version,authorization_epoch) values ($1,1,1)`, []any{recordID}},
			{`insert into public.record_revisions (revision_id,record_id,revision_no,title,body_markdown,markdown_dialect_version,record_type,impact_level,visibility_scope,visibility_digest,author_id,canonical_hash)
			 values ($1,$2,1,'delivery','',1,'note','informational','{}'::jsonb,decode(repeat('41',32),'hex'),'usr_deliveryauthor',decode(repeat('42',32),'hex'))`, []any{revisionID, recordID}},
			{`insert into public.record_revision_subjects (revision_id,ordinal,registry_version,subject_kind,relation_role,source_id,is_primary,identity_snapshot,capture_authorization,capture_authorization_digest)
			 values ($1,0,1,'vps','affected','vps_delivery',true,'{}'::jsonb,'{}'::jsonb,decode(repeat('43',32),'hex'))`, []any{revisionID}},
			{`update public.records set current_revision_id=$2,current_title='delivery',current_record_type='note',current_impact_level='informational',current_visibility_scope='{}'::jsonb,current_visibility_digest=decode(repeat('41',32),'hex') where record_id=$1`, []any{recordID, revisionID}},
			{`insert into public.content_delivery_epochs (project_id,object_kind,object_id,delivery_epoch) values ('default','record',$1,4)`, []any{recordID}},
			{`insert into public.record_notifications (notification_id,record_id,event_kind,subject_kind,subject_id,source_version,actor_id,authorization_epoch,record_fence_epoch,event_at,details_delete_after)
			 values ($1,$2,'record_owner_changed','record',$2,1,'usr_deliveryauthor',1,4,clock_timestamp(),clock_timestamp()+interval '1 day')`, []any{notificationID, recordID}},
		}
		for _, statement := range statements {
			if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
				t.Fatalf("seed %s predecessor route: %v", recordID, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit delivery predecessor routes: %v", err)
	}
}

func deliveryFactsNotification(recordID string) string {
	if recordID == "rec_deliverylate" {
		return "rnt_" + strings.Repeat("cd", 32)
	}
	if recordID == "rec_deliveryepoch" {
		return "rnt_" + strings.Repeat("ef", 32)
	}
	return "rnt_" + strings.Repeat("ab", 32)
}

func seedDeliveryFactsPending(t *testing.T, ctx context.Context, db *pgxpool.Pool, index int, recordID string, predecessor bool, ownerTTL, eventTTL time.Duration) map[string]any {
	t.Helper()
	deliveryID := fmt.Sprintf("rnd_delivery%d", index)
	recipientID := fmt.Sprintf("usr_delivery%d", index)
	notificationID := deliveryFactsNotification(recordID)
	if _, err := db.Exec(ctx, `insert into public.users (user_id,username,password_hash,display_name,role) values ($1,$1,'fixture','Delivery recipient','admin')`, recipientID); err != nil {
		t.Fatalf("seed delivery recipient user: %v", err)
	}
	if _, err := db.Exec(ctx, `insert into public.record_notification_recipients
		(notification_id,record_id,recipient_user_id,reason_kind,mandatory,authorization_epoch,record_fence_epoch)
		values ($1,$2,$3,'mention',true,1,4)`, notificationID, recordID, recipientID); err != nil {
		t.Fatalf("seed delivery recipient: %v", err)
	}
	if _, err := db.Exec(ctx, `insert into public.record_notification_deliveries
		(delivery_id,record_id,notification_id,recipient_user_id,channel,binding_id,authorization_epoch,record_fence_epoch)
		values ($1,$2,$3,$4,'telegram','binding_delivery',1,4)`, deliveryID, recordID, notificationID, recipientID); err != nil {
		t.Fatalf("seed pending delivery: %v", err)
	}
	outboxSQL := `insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,source_version,authorization_epoch,record_fence_epoch,status,owner_id,owner_generation,owner_expires_at,expires_at,record_id)
		values ('default','record_notification_delivery','delivery',$1,1,1,4,'processing','delivery_owner',1,
		clock_timestamp()+$2::bigint*interval '1 microsecond',clock_timestamp()+$3::bigint*interval '1 microsecond',$4)
		returning outbox_row_id,owner_expires_at,expires_at`
	args := []any{deliveryID, ownerTTL.Microseconds(), eventTTL.Microseconds(), recordID}
	if predecessor {
		outboxSQL = `insert into public.record_outbox
			(project_id,event_kind,subject_kind,subject_id,source_version,authorization_epoch,record_fence_epoch,status,owner_id,owner_generation,owner_expires_at,expires_at)
			values ('default','record_notification_delivery','delivery',$1,1,1,4,'processing','delivery_owner',1,
			clock_timestamp()+$2::bigint*interval '1 microsecond',clock_timestamp()+$3::bigint*interval '1 microsecond')
			returning outbox_row_id,owner_expires_at,expires_at`
		args = args[:3]
	}
	var rowID int64
	var ownerExpiry, eventExpiry time.Time
	if err := db.QueryRow(ctx, outboxSQL, args...).Scan(&rowID, &ownerExpiry, &eventExpiry); err != nil {
		t.Fatalf("seed live outbox claim: %v", err)
	}
	return map[string]any{
		"outbox_row_id": rowID, "owner_id": "delivery_owner", "owner_generation": int64(1),
		"owner_expires_at": deliveryFactsTimestamp(ownerExpiry), "event_expires_at": deliveryFactsTimestamp(eventExpiry),
		"project_id": "default", "event_kind": "record_notification_delivery", "subject_kind": "delivery",
		"delivery_id": deliveryID, "record_id": recordID, "notification_id": notificationID,
		"recipient_user_id": recipientID, "channel": "telegram", "binding_id": "binding_delivery",
		"source_version": int64(1), "authorization_epoch": int64(1), "record_fence_epoch": int64(4),
	}
}

func startDeliveryFacts(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, claim map[string]any, state string, previous int64) deliveryFactsStart {
	t.Helper()
	command := deliveryFactsCopy(claim)
	command["expected_state"] = state
	command["expected_attempt_count"] = previous
	body := deliveryFactsJSON(t, command)
	var number int64
	var started time.Time
	var capability []byte
	if err := runtime.QueryRow(ctx, `select attempt_no,started_at,reporting_capability from public.record_external_delivery_start($1::bytea)`, body).Scan(&number, &started, &capability); err != nil {
		t.Fatalf("start delivery attempt %d: %v", previous+1, err)
	}
	if number != previous+1 || len(capability) != 32 {
		t.Fatalf("start returned attempt %d and %d capability bytes; want %d and 32", number, len(capability), previous+1)
	}
	attempt := deliveryFactsCopy(claim)
	attempt["attempt_no"] = number
	attempt["started_at"] = deliveryFactsTimestamp(started)
	attempt["attempt_id"] = recordcollaboration.NotificationDeliveryAttemptID(claim["delivery_id"].(string), uint8(number))
	return deliveryFactsStart{claim: deliveryFactsCopy(claim), attempt: attempt, startBody: body, capability: capability}
}

func deliveryFactsReportCommand(start deliveryFactsStart, outcome string) map[string]any {
	command := deliveryFactsCopy(start.attempt)
	command["reporting_capability"] = hex.EncodeToString(start.capability)
	command["outcome"] = outcome
	return command
}

func deliveryFactsReport(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, start deliveryFactsStart, outcome string) []byte {
	t.Helper()
	body := deliveryFactsJSON(t, deliveryFactsReportCommand(start, outcome))
	var attemptID string
	var digest []byte
	if err := runtime.QueryRow(ctx, `select attempt_id,evidence_digest from public.record_external_delivery_outcome($1::bytea)`, body).Scan(&attemptID, &digest); err != nil {
		t.Fatalf("report %s provider fact: %v", outcome, err)
	}
	if attemptID != start.attempt["attempt_id"] || len(digest) != 32 {
		t.Fatalf("report returned invalid fact locator for %s", outcome)
	}
	return digest
}

func deliveryFactsFinalizeCommand(start deliveryFactsStart, currentClaim map[string]any, digest []byte, delayUS int64) map[string]any {
	command := deliveryFactsCopy(currentClaim)
	command["attempt_no"] = start.attempt["attempt_no"]
	command["started_at"] = start.attempt["started_at"]
	command["attempt_id"] = start.attempt["attempt_id"]
	command["evidence_digest"] = hex.EncodeToString(digest)
	command["retry_after_us"] = delayUS
	return command
}

func deliveryFactsReconcileCommand(start deliveryFactsStart, currentClaim map[string]any, delayUS int64) map[string]any {
	command := deliveryFactsFinalizeCommand(start, currentClaim, nil, delayUS)
	delete(command, "evidence_digest")
	return command
}

func deliveryFactsFinalize(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, command map[string]any) deliveryFactsFinalization {
	t.Helper()
	var result deliveryFactsFinalization
	if err := runtime.QueryRow(ctx, `select disposition,retry_after_us,next_attempt_at from public.record_external_delivery_finalize($1::bytea)`, deliveryFactsJSON(t, command)).Scan(&result.disposition, &result.delayUS, &result.nextAttemptAt); err != nil {
		t.Fatalf("finalize provider fact: %v", err)
	}
	return result
}

func deliveryFactsReconcile(t *testing.T, ctx context.Context, runtime *pgxpool.Pool, command map[string]any) deliveryFactsFinalization {
	t.Helper()
	var result deliveryFactsFinalization
	if err := runtime.QueryRow(ctx, `select disposition,retry_after_us,next_attempt_at from public.record_external_delivery_reconcile($1::bytea)`, deliveryFactsJSON(t, command)).Scan(&result.disposition, &result.delayUS, &result.nextAttemptAt); err != nil {
		t.Fatalf("reconcile delivery: %v", err)
	}
	return result
}

// Claim through the same restricted command used by the worker, using the
// actual persisted queue tuple after expiration or after a retry becomes due.
func deliveryFactsClaim(t *testing.T, ctx context.Context, owner, runtime, observer *pgxpool.Pool, claim map[string]any, newOwner string) map[string]any {
	t.Helper()
	return deliveryFactsClaimWithLease(t, ctx, owner, runtime, observer, claim, newOwner, 10*time.Minute)
}

func deliveryFactsClaimWithLease(t *testing.T, ctx context.Context, owner, runtime, observer *pgxpool.Pool, claim map[string]any, newOwner string, lease time.Duration) map[string]any {
	t.Helper()
	var status, ownerID string
	var generation, attempts int64
	var ownerExpiry *time.Time
	var nextAttempt, eventExpiry time.Time
	if err := owner.QueryRow(ctx, `select status,owner_id,owner_generation,owner_expires_at,attempt_count,next_attempt_at,expires_at
		from public.record_outbox where outbox_row_id=$1`, claim["outbox_row_id"]).Scan(&status, &ownerID, &generation, &ownerExpiry, &attempts, &nextAttempt, &eventExpiry); err != nil {
		t.Fatalf("read claim candidate: %v", err)
	}
	if status == "processing" && ownerExpiry != nil {
		deliveryFactsWaitClock(t, ctx, observer, *ownerExpiry)
	} else if status == "pending" {
		deliveryFactsWaitClock(t, ctx, observer, nextAttempt)
	}
	command := map[string]any{
		"project_id": claim["project_id"], "event_kind": claim["event_kind"], "subject_kind": claim["subject_kind"],
		"subject_id": claim["delivery_id"], "record_id": claim["record_id"], "source_version": claim["source_version"],
		"authorization_epoch": claim["authorization_epoch"], "record_fence_epoch": claim["record_fence_epoch"],
		"outbox_row_id": claim["outbox_row_id"], "status": status, "owner_id": ownerID,
		"owner_generation": generation, "owner_expires_at": nil, "attempt_count": attempts,
		"next_attempt_at": deliveryFactsTimestamp(nextAttempt), "event_expires_at": deliveryFactsTimestamp(eventExpiry),
		"new_owner_id": newOwner, "delay_us": lease.Microseconds(),
	}
	if ownerExpiry != nil {
		command["owner_expires_at"] = deliveryFactsTimestamp(*ownerExpiry)
	}
	var acquiredOwner string
	var acquiredGeneration int64
	var acquiredExpiry time.Time
	if err := runtime.QueryRow(ctx, `select owner_id,owner_generation,owner_expires_at from public.record_outbox_claim($1::bytea)`, deliveryFactsJSON(t, command)).Scan(&acquiredOwner, &acquiredGeneration, &acquiredExpiry); err != nil {
		t.Fatalf("claim eligible outbox row: %v", err)
	}
	if acquiredOwner != newOwner || acquiredGeneration != generation+1 {
		t.Fatalf("claimed owner = (%s,%d), want (%s,%d)", acquiredOwner, acquiredGeneration, newOwner, generation+1)
	}
	current := deliveryFactsCopy(claim)
	current["owner_id"] = acquiredOwner
	current["owner_generation"] = acquiredGeneration
	current["owner_expires_at"] = deliveryFactsTimestamp(acquiredExpiry)
	return current
}

func deliveryFactsWaitClock(t *testing.T, ctx context.Context, observer *pgxpool.Pool, target time.Time) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var reached bool
		if err := observer.QueryRow(ctx, `select clock_timestamp() >= $1::timestamptz`, target).Scan(&reached); err != nil {
			t.Fatal(err)
		}
		if reached {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("database clock did not reach deadline: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

// These expectations come from the delivery protocol, independently of the
// migration and ACL compiler. In particular, an absent online row must not
// remove or prevent retention of a start, fact, completion, or handoff.
func assertDeliveryFactsCatalog(t *testing.T, ctx context.Context, db *pgxpool.Pool, owner, runtime, admin, worker string) {
	t.Helper()
	t.Run("foreign_keys", func(t *testing.T) {
		assertDeliveryFactsForeignKeys(t, ctx, db)
	})
	t.Run("indexes", func(t *testing.T) {
		assertDeliveryFactsIndexes(t, ctx, db)
	})
	t.Run("triggers", func(t *testing.T) {
		assertDeliveryFactsTriggers(t, ctx, db)
	})
	t.Run("functions_and_private_access", func(t *testing.T) {
		assertDeliveryFactsFunctionCatalog(t, ctx, db, owner, runtime, admin, worker)
	})
}

func assertDeliveryFactsForeignKeys(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	type foreignKey struct {
		childSchema, child, childColumns    string
		parentSchema, parent, parentColumns string
		deleteAction, updateAction, match   string
		deferrable, deferred, validated     bool
	}
	expected := map[foreignKey]bool{
		{"public", "record_notification_delivery_facts", "attempt_id", "public", "record_notification_delivery_starts", "attempt_id", "r", "a", "s", false, false, true}:                                                          true,
		{"public", "record_notification_delivery_completions", "attempt_id", "public", "record_notification_delivery_starts", "attempt_id", "r", "a", "s", false, false, true}:                                                    true,
		{"public", "record_notification_delivery_completions", "attempt_id,fact_kind,evidence_digest", "public", "record_notification_delivery_facts", "attempt_id,fact_kind,evidence_digest", "r", "a", "s", false, false, true}: true,
		{"public", "record_notification_delivery_handoffs", "attempt_id", "public", "record_notification_delivery_starts", "attempt_id", "r", "a", "s", false, false, true}:                                                       true,
	}
	rows, err := db.Query(ctx, `select child_ns.nspname,child.relname,
		array_to_string(array(select a.attname::text from unnest(c.conkey) with ordinality k(attnum,position)
		  join pg_catalog.pg_attribute a on a.attrelid=c.conrelid and a.attnum=k.attnum order by k.position),','),
		parent_ns.nspname,parent.relname,
		array_to_string(array(select a.attname::text from unnest(c.confkey) with ordinality k(attnum,position)
		  join pg_catalog.pg_attribute a on a.attrelid=c.confrelid and a.attnum=k.attnum order by k.position),','),
		c.confdeltype::text,c.confupdtype::text,c.confmatchtype::text,c.condeferrable,c.condeferred,c.convalidated
		from pg_catalog.pg_constraint c
		join pg_catalog.pg_class child on child.oid=c.conrelid
		join pg_catalog.pg_namespace child_ns on child_ns.oid=child.relnamespace
		join pg_catalog.pg_class parent on parent.oid=c.confrelid
		join pg_catalog.pg_namespace parent_ns on parent_ns.oid=parent.relnamespace
		where c.contype='f' and ((child_ns.nspname='public' and child.relname=any($1::text[]))
		  or (child_ns.nspname='record_platform_internal' and child.relname='record_external_delivery_commit_checks'))`,
		[]string{"record_notification_delivery_starts", "record_notification_delivery_facts", "record_notification_delivery_completions", "record_notification_delivery_handoffs"})
	if err != nil {
		t.Fatalf("read retained delivery foreign keys: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var actual foreignKey
		if err := rows.Scan(&actual.childSchema, &actual.child, &actual.childColumns, &actual.parentSchema, &actual.parent,
			&actual.parentColumns, &actual.deleteAction, &actual.updateAction, &actual.match,
			&actual.deferrable, &actual.deferred, &actual.validated); err != nil {
			t.Fatalf("scan retained delivery foreign key: %v", err)
		}
		if !expected[actual] {
			t.Errorf("unexpected retained delivery foreign key: %+v", actual)
			continue
		}
		delete(expected, actual)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read retained delivery foreign keys: %v", err)
	}
	if len(expected) != 0 {
		t.Fatalf("missing retained delivery foreign keys: %+v", expected)
	}
}

func assertDeliveryFactsIndexes(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	for _, expected := range []struct {
		schema, table, name, columns string
		unique, primary              bool
	}{
		{"public", "record_notification_delivery_starts", "record_notification_delivery_starts_pkey", "delivery_id,attempt_no", true, true},
		{"public", "record_notification_delivery_starts", "record_notification_delivery_starts_attempt_id_key", "attempt_id", true, false},
		{"public", "record_notification_delivery_starts", "record_notification_delivery_starts_record_idx", "project_id,record_id,delivery_id,attempt_no", false, false},
		{"public", "record_notification_delivery_facts", "record_notification_delivery_facts_pkey", "attempt_id,fact_kind,evidence_digest", true, true},
		{"public", "record_notification_delivery_facts", "record_notification_delivery_facts_record_idx", "project_id,record_id,attempt_id", false, false},
		{"public", "record_notification_delivery_completions", "record_notification_delivery_completions_pkey", "delivery_id,attempt_no", true, true},
		{"public", "record_notification_delivery_completions", "record_notification_delivery_completions_attempt_id_key", "attempt_id", true, false},
		{"public", "record_notification_delivery_completions", "record_notification_delivery_completions_record_idx", "project_id,record_id,delivery_id,attempt_no", false, false},
		{"public", "record_notification_delivery_handoffs", "record_notification_delivery_handoffs_pkey", "operation_id,delivery_id,attempt_no", true, true},
		{"public", "record_notification_delivery_handoffs", "record_notification_delivery_handoffs_record_idx", "project_id,record_id,delivery_id,attempt_no", false, false},
		{"record_platform_internal", "record_external_delivery_commit_checks", "record_external_delivery_commit_checks_pkey", "transaction_id,backend_pid,outbox_row_id", true, true},
	} {
		var table, columns, method string
		var unique, primary, valid, ready, plain, defaultNulls bool
		err := db.QueryRow(ctx, `select tbl.relname,
			array_to_string(array(select a.attname::text from unnest(i.indkey::smallint[]) with ordinality k(attnum,position)
			  join pg_catalog.pg_attribute a on a.attrelid=i.indrelid and a.attnum=k.attnum order by k.position),','),
			am.amname,i.indisunique,i.indisprimary,i.indisvalid,i.indisready,
			(i.indnkeyatts=i.indnatts and i.indexprs is null and i.indpred is null),not i.indnullsnotdistinct
			from pg_catalog.pg_index i join pg_catalog.pg_class idx on idx.oid=i.indexrelid
			join pg_catalog.pg_namespace n on n.oid=idx.relnamespace
			join pg_catalog.pg_class tbl on tbl.oid=i.indrelid
			join pg_catalog.pg_am am on am.oid=idx.relam
			where n.nspname=$1 and idx.relname=$2`, expected.schema, expected.name).Scan(
			&table, &columns, &method, &unique, &primary, &valid, &ready, &plain, &defaultNulls)
		if err != nil || table != expected.table || columns != expected.columns || method != "btree" ||
			unique != expected.unique || primary != expected.primary || !valid || !ready || !plain || !defaultNulls {
			t.Fatalf("delivery index %s.%s: table=%q columns=%q method=%q unique=%t primary=%t valid=%t ready=%t plain=%t default_nulls=%t error=%v",
				expected.schema, expected.name, table, columns, method, unique, primary, valid, ready, plain, defaultNulls, err)
		}
	}
}

func assertDeliveryFactsTriggers(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	for _, expected := range []struct {
		schema, table, name, function string
		typeBits                      int
		deferrable, deferred          bool
		constraint, definer           bool
	}{
		{"public", "record_notification_delivery_starts", "record_notification_delivery_starts_immutable", "record_delivery_retained_immutable", 58, false, false, false, true},
		{"public", "record_notification_delivery_facts", "record_notification_delivery_facts_immutable", "record_delivery_retained_immutable", 58, false, false, false, true},
		{"public", "record_notification_delivery_completions", "record_notification_delivery_completions_immutable", "record_delivery_retained_immutable", 58, false, false, false, true},
		{"public", "record_notification_delivery_handoffs", "record_notification_delivery_handoffs_immutable", "record_delivery_retained_immutable", 58, false, false, false, true},
		{"public", "record_notification_deliveries", "record_notification_delivery_insert_guard", "record_external_delivery_insert_guard", 7, false, false, false, true},
		{"record_platform_internal", "record_external_delivery_commit_checks", "record_external_delivery_commit_check", "record_external_delivery_commit_trigger", 5, true, true, true, true},
		{"public", "record_outbox", "record_outbox_route_immutable", "record_outbox_route_immutable", 19, false, false, false, false},
		{"record_platform_internal", "record_deletion_owner_commit_checks", "record_deletion_owner_commit_check", "record_deletion_owner_commit_trigger", 5, true, true, true, true},
	} {
		var enabled, functionSchema, function, volatility string
		var typeBits int
		var deferrable, deferred, constraint, ordinary, unconditional, noColumns, noArgs, definer, returnsTrigger, publicExecute bool
		var config []string
		err := db.QueryRow(ctx, `select t.tgenabled::text,t.tgtype::int,t.tgdeferrable,t.tginitdeferred,t.tgconstraint<>0,
			not t.tgisinternal,t.tgqual is null,t.tgattr=''::pg_catalog.int2vector,t.tgnargs=0,
			fn_ns.nspname,p.proname,p.prosecdef,p.provolatile::text,coalesce(p.proconfig,array[]::text[]),
			p.pronargs=0 and p.prorettype='pg_catalog.trigger'::regtype and not p.proretset,
			exists(select 1 from pg_catalog.aclexplode(coalesce(p.proacl,pg_catalog.acldefault('f',p.proowner))) a
			  where a.grantee=0 and a.privilege_type='EXECUTE')
			from pg_catalog.pg_trigger t join pg_catalog.pg_class rel on rel.oid=t.tgrelid
			join pg_catalog.pg_namespace rel_ns on rel_ns.oid=rel.relnamespace
			join pg_catalog.pg_proc p on p.oid=t.tgfoid join pg_catalog.pg_namespace fn_ns on fn_ns.oid=p.pronamespace
			where rel_ns.nspname=$1 and rel.relname=$2 and t.tgname=$3`, expected.schema, expected.table, expected.name).Scan(
			&enabled, &typeBits, &deferrable, &deferred, &constraint, &ordinary, &unconditional, &noColumns, &noArgs,
			&functionSchema, &function, &definer, &volatility, &config, &returnsTrigger, &publicExecute)
		if err != nil || enabled != "O" || typeBits != expected.typeBits || deferrable != expected.deferrable ||
			deferred != expected.deferred || constraint != expected.constraint || !ordinary || !unconditional || !noColumns || !noArgs ||
			functionSchema != "record_platform_internal" || function != expected.function || definer != expected.definer ||
			volatility != "v" || len(config) != 1 || config[0] != "search_path=pg_catalog" || !returnsTrigger || publicExecute {
			t.Fatalf("delivery trigger %s.%s: enabled=%q type=%d deferrable=%t deferred=%t constraint=%t ordinary=%t unconditional=%t no_columns=%t no_args=%t function=%s.%s definer=%t volatility=%q config=%q returns_trigger=%t public_execute=%t error=%v",
				expected.table, expected.name, enabled, typeBits, deferrable, deferred, constraint, ordinary, unconditional, noColumns, noArgs,
				functionSchema, function, definer, volatility, config, returnsTrigger, publicExecute, err)
		}
	}
}

func assertDeliveryFactsFunctionCatalog(t *testing.T, ctx context.Context, db *pgxpool.Pool, owner, runtime, admin, worker string) {
	t.Helper()
	for _, expected := range []struct {
		identity, result string
		returnsSet       bool
	}{
		{"public.record_external_delivery_start(bytea)", "TABLE(attempt_no bigint, started_at timestamp with time zone, reporting_capability bytea)", true},
		{"public.record_external_delivery_cancel(bytea)", "void", false},
		{"public.record_external_delivery_outcome(bytea)", "TABLE(attempt_id text, evidence_digest bytea)", true},
		{"public.record_external_delivery_finalize(bytea)", "TABLE(disposition text, retry_after_us bigint, next_attempt_at timestamp with time zone)", true},
		{"public.record_external_delivery_reconcile(bytea)", "TABLE(disposition text, retry_after_us bigint, next_attempt_at timestamp with time zone)", true},
		{"public.record_collaboration_start_inventory(text,bigint)", "TABLE(start_digest bytea, start_count bigint, mismatched boolean)", true},
		{"public.record_collaboration_delivery_inventory(text,bigint)", "TABLE(notification_id text, delivery_id text, classification text, evidence_digest bytea, mismatched boolean)", true},
		{"public.record_outbox_drain_v2(bytea)", "TABLE(drained_count bigint, remaining boolean)", true},
	} {
		var result, functionOwner, kind, volatility string
		var definer, returnsSet, publicExecute, soleOverload bool
		var config []string
		err := db.QueryRow(ctx, `select pg_catalog.pg_get_function_result(p.oid),pg_catalog.pg_get_userbyid(p.proowner),
			p.prokind::text,p.provolatile::text,p.prosecdef,p.proretset,coalesce(p.proconfig,array[]::text[]),
			exists(select 1 from pg_catalog.aclexplode(coalesce(p.proacl,pg_catalog.acldefault('f',p.proowner))) a
			  where a.grantee=0 and a.privilege_type='EXECUTE'),
			(select count(*)=1 from pg_catalog.pg_proc other where other.pronamespace=p.pronamespace and other.proname=p.proname)
			from pg_catalog.pg_proc p where p.oid=pg_catalog.to_regprocedure($1)`, expected.identity).Scan(
			&result, &functionOwner, &kind, &volatility, &definer, &returnsSet, &config, &publicExecute, &soleOverload)
		if err != nil || result != expected.result || functionOwner != owner || kind != "f" || volatility != "v" ||
			!definer || returnsSet != expected.returnsSet || len(config) != 1 || config[0] != "search_path=pg_catalog" || publicExecute || !soleOverload {
			t.Fatalf("delivery function %s: result=%q owner=%q kind=%q volatility=%q definer=%t returns_set=%t config=%q public_execute=%t sole_overload=%t error=%v",
				expected.identity, result, functionOwner, kind, volatility, definer, returnsSet, config, publicExecute, soleOverload, err)
		}
	}
	for _, identity := range []string{
		"record_platform_internal.record_delivery_retained_immutable()",
		"record_platform_internal.record_external_delivery_insert_guard()",
		"record_platform_internal.record_external_delivery_commit_trigger()",
		"record_platform_internal.record_external_delivery_queue_commit_check(jsonb,text,bigint,timestamptz)",
	} {
		var functionOwner, volatility string
		var definer, runtimeExecute, adminExecute, workerExecute, publicExecute bool
		var config []string
		err := db.QueryRow(ctx, `select pg_catalog.pg_get_userbyid(p.proowner),p.provolatile::text,p.prosecdef,
			coalesce(p.proconfig,array[]::text[]),pg_catalog.has_function_privilege($2::name,p.oid,'EXECUTE'),
			pg_catalog.has_function_privilege($3::name,p.oid,'EXECUTE'),pg_catalog.has_function_privilege($4::name,p.oid,'EXECUTE'),
			exists(select 1 from pg_catalog.aclexplode(coalesce(p.proacl,pg_catalog.acldefault('f',p.proowner))) a
			  where a.grantee=0 and a.privilege_type='EXECUTE')
			from pg_catalog.pg_proc p where p.oid=pg_catalog.to_regprocedure($1)`, identity, runtime, admin, worker).Scan(
			&functionOwner, &volatility, &definer, &config, &runtimeExecute, &adminExecute, &workerExecute, &publicExecute)
		if err != nil || functionOwner != owner || volatility != "v" || !definer || len(config) != 1 || config[0] != "search_path=pg_catalog" ||
			runtimeExecute || adminExecute || workerExecute || publicExecute {
			t.Fatalf("private delivery function %s: owner=%q volatility=%q definer=%t config=%q runtime=%t admin=%t worker=%t public=%t error=%v",
				identity, functionOwner, volatility, definer, config, runtimeExecute, adminExecute, workerExecute, publicExecute, err)
		}
	}
	var tableOwner string
	var otherTableGrants, otherColumnGrants bool
	err := db.QueryRow(ctx, `select pg_catalog.pg_get_userbyid(c.relowner),
		exists(select 1 from pg_catalog.aclexplode(coalesce(c.relacl,pg_catalog.acldefault('r',c.relowner))) a where a.grantee<>c.relowner),
		exists(select 1 from pg_catalog.pg_attribute col cross join lateral pg_catalog.aclexplode(col.attacl) a
		  where col.attrelid=c.oid and a.grantee<>c.relowner)
		from pg_catalog.pg_class c where c.oid=pg_catalog.to_regclass('record_platform_internal.record_external_delivery_commit_checks')`).Scan(
		&tableOwner, &otherTableGrants, &otherColumnGrants)
	if err != nil || tableOwner != owner || otherTableGrants || otherColumnGrants {
		t.Fatalf("delivery commit-check table: owner=%q other_table_grants=%t other_column_grants=%t error=%v",
			tableOwner, otherTableGrants, otherColumnGrants, err)
	}
	for _, role := range []string{runtime, worker} {
		for _, table := range []string{
			"public.record_notification_delivery_starts", "public.record_notification_delivery_facts",
			"public.record_notification_delivery_completions", "public.record_notification_delivery_handoffs",
		} {
			var readable bool
			if err := db.QueryRow(ctx, `select pg_catalog.has_any_column_privilege($1::name,$2::regclass,'SELECT')`, role, table).Scan(&readable); err != nil || readable {
				t.Fatalf("retained delivery read privilege role=%q table=%q: readable=%t error=%v", role, table, readable, err)
			}
		}
	}
}

func assertDeliveryFactsImmutableStatements(t *testing.T, ctx context.Context, owner *pgxpool.Pool) {
	t.Helper()
	tables := []string{
		"public.record_notification_delivery_starts", "public.record_notification_delivery_facts",
		"public.record_notification_delivery_completions", "public.record_notification_delivery_handoffs",
	}
	for first, table := range tables {
		t.Run(table, func(t *testing.T) {
			// Even a zero-row statement must be rejected. All retained tables are
			// included in TRUNCATE so the retained FKs do not reject it first.
			for _, statement := range []string{
				`update ` + table + ` set record_id=record_id where false`,
				`delete from ` + table + ` where false`,
			} {
				_, err := owner.Exec(ctx, statement)
				requirePostgresSQLState(t, err, "55000")
			}
			targets := []string{table}
			for index, other := range tables {
				if index != first {
					targets = append(targets, other)
				}
			}
			_, err := owner.Exec(ctx, `truncate table `+strings.Join(targets, ","))
			requirePostgresSQLState(t, err, "55000")
		})
	}
}

func assertDeliveryFactsRoleBoundaries(t *testing.T, ctx context.Context, runtime, admin, worker *pgxpool.Pool) {
	t.Helper()
	for _, db := range []*pgxpool.Pool{admin, worker} {
		for _, function := range []string{"record_external_delivery_start", "record_external_delivery_outcome", "record_external_delivery_finalize", "record_external_delivery_reconcile", "record_external_delivery_cancel"} {
			_, err := db.Exec(ctx, `select public.`+function+`($1::bytea)`, []byte(`{}`))
			requirePostgresSQLState(t, err, "42501")
		}
	}
	for _, statement := range []string{
		`update public.record_notification_deliveries set reason_code=reason_code where false`,
		`insert into public.record_notification_delivery_attempts select * from public.record_notification_delivery_attempts where false`,
		`insert into public.record_notification_delivery_facts select * from public.record_notification_delivery_facts where false`,
		`insert into public.record_notification_delivery_completions select * from public.record_notification_delivery_completions where false`,
		`insert into public.record_collaboration_purge_receipts select * from public.record_collaboration_purge_receipts where false`,
		`select public.record_collaboration_purge('{}'::bytea)`,
		`select public.record_collaboration_purge_v2('{}'::bytea)`,
		`select public.record_collaboration_purge_verify_v2('{}'::bytea)`,
	} {
		_, err := runtime.Exec(ctx, statement)
		requirePostgresSQLState(t, err, "42501")
	}
	for _, db := range []*pgxpool.Pool{runtime, worker} {
		for _, table := range []string{
			"record_notification_delivery_starts", "record_notification_delivery_facts",
			"record_notification_delivery_completions", "record_notification_delivery_handoffs",
		} {
			_, err := db.Exec(ctx, `select * from public.`+table+` where false`)
			requirePostgresSQLState(t, err, "42501")
		}
	}
	for _, db := range []*pgxpool.Pool{runtime, admin} {
		_, err := db.Exec(ctx, `select * from public.record_outbox_drain_v2('{}'::bytea)`)
		requirePostgresSQLState(t, err, "42501")
	}
	for _, db := range []*pgxpool.Pool{admin, worker} {
		for _, function := range []string{"record_collaboration_start_inventory", "record_collaboration_delivery_inventory"} {
			_, err := db.Exec(ctx, `select * from public.`+function+`('rec_x',0)`)
			requirePostgresSQLState(t, err, "42501")
		}
	}
	legacyPurge := `{"operation_id":"rpo_absent","record_id":"rec_absent","reservation_id":"drs_absent","fence_epoch":1,"owner_id":"worker","owner_generation":1,"owner_expires_at":"2026-08-17T17:30:00Z","surface_digest":"0ec43f91ba1d5df5d95dbdd90378afeb5e95901eec2f4c4d31bab06f0be83a07"}`
	for _, function := range []string{"record_collaboration_purge_v2", "record_collaboration_purge_verify_v2"} {
		_, err := worker.Exec(ctx, `select * from public.`+function+`($1::bytea)`, []byte(legacyPurge))
		requirePostgresSQLState(t, err, "22023")
	}
	// These processor publication cases were part of the earlier role test and
	// continue to cover a separate restricted entry point.
	processor := `{"job_id":"apj_absent","owner_id":"worker","owner_generation":1,"lease_expires_at":"2026-08-17T17:30:00Z","attempt":1,"preview_publication_id":null,"preview_media_type":null,"source_obligation_id":"rwo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","preview_obligation_id":null,"source_namespace_id":null,"source_incarnation_id":null,"attachment_id":"att_absent","upload_id":"upl_absent","draft_id":"drf_absent","record_id":null}`
	for _, function := range []string{"record_processor_publication_lock", "record_processor_publication_finish"} {
		query := `select public.` + function + `($1::bytea)`
		for _, malformed := range []string{
			strings.Replace(processor, `"job_id":"apj_absent"`, `"job_id":"apj_absent","job_id":"apj_other"`, 1),
			strings.Replace(processor, `"source_obligation_id":"rwo_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, `"source_obligation_id":"rwo_bad"`, 1),
			strings.Replace(processor, `"owner_generation":1`, `"owner_generation":"1"`, 1),
			strings.Replace(processor, `"preview_obligation_id":null`, `"preview_obligation_id":"rwo_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"`, 1),
		} {
			_, err := runtime.Exec(ctx, query, []byte(malformed))
			requirePostgresSQLState(t, err, "22023")
		}
		_, err := runtime.Exec(ctx, query, []byte(processor))
		requirePostgresSQLState(t, err, "40001")
		for _, db := range []*pgxpool.Pool{admin, worker} {
			_, err = db.Exec(ctx, query, []byte(processor))
			requirePostgresSQLState(t, err, "42501")
		}
	}
}

func assertDeliveryFactsCancelAndLegacy(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool, legacy map[string]any, legacyStarted time.Time) {
	t.Helper()
	pending := seedDeliveryFactsPending(t, ctx, owner, 2, "rec_delivery", false, 10*time.Minute, time.Hour)
	cancel := deliveryFactsCopy(pending)
	cancel["expected_state"] = "pending"
	cancel["expected_attempt_count"] = int64(0)
	cancel["reason"] = "authorization_revoked"
	body := deliveryFactsJSON(t, cancel)
	if _, err := runtime.Exec(ctx, `select public.record_external_delivery_cancel($1::bytea)`, body); err != nil {
		t.Fatalf("cancel never-started delivery: %v", err)
	}
	var state, outcome, reason string
	var starts int
	if err := owner.QueryRow(ctx, `select d.delivery_state,a.outcome,a.reason_code,
		(select count(*)::int from public.record_notification_delivery_starts where delivery_id=d.delivery_id)
		from public.record_notification_deliveries d join public.record_notification_delivery_attempts a on a.delivery_id=d.delivery_id
		where d.delivery_id=$1`, pending["delivery_id"]).Scan(&state, &outcome, &reason, &starts); err != nil ||
		state != "cancelled" || outcome != "cancelled" || reason != "authorization_revoked" || starts != 0 {
		t.Fatalf("never-started cancellation = (%q,%q,%q,%d), %v", state, outcome, reason, starts, err)
	}
	_, err := runtime.Exec(ctx, `select public.record_external_delivery_cancel($1::bytea)`, body)
	requirePostgresSQLState(t, err, "55000")

	attempt := deliveryFactsCopy(legacy)
	attempt["attempt_no"] = int64(1)
	attempt["started_at"] = deliveryFactsTimestamp(legacyStarted)
	attempt["attempt_id"] = recordcollaboration.NotificationDeliveryAttemptID(legacy["delivery_id"].(string), 1)
	forged := deliveryFactsCopy(attempt)
	forged["reporting_capability"] = strings.Repeat("00", 32)
	forged["outcome"] = "sent"
	_, err = runtime.Exec(ctx, `select * from public.record_external_delivery_outcome($1::bytea)`, deliveryFactsJSON(t, forged))
	requirePostgresSQLState(t, err, "55000")
	attempt["retry_after_us"] = int64(250000)
	result := deliveryFactsReconcile(t, ctx, runtime, attempt)
	if result.disposition != "complete" || result.delayUS != 0 || result.nextAttemptAt != nil {
		t.Fatalf("legacy reconciliation disposition = %#v", result)
	}
	if err := owner.QueryRow(ctx, `select d.delivery_state,a.outcome,a.reason_code,
		(select count(*)::int from public.record_notification_delivery_starts where delivery_id=d.delivery_id)
		from public.record_notification_deliveries d join public.record_notification_delivery_attempts a on a.delivery_id=d.delivery_id
		where d.delivery_id=$1`, legacy["delivery_id"]).Scan(&state, &outcome, &reason, &starts); err != nil ||
		state != "unknown_outcome" || outcome != "unknown_outcome" || reason != "legacy_processing_unknown" || starts != 0 {
		t.Fatalf("legacy processing reconciliation = (%q,%q,%q,%d), %v", state, outcome, reason, starts, err)
	}
}

func assertDeliveryFactsStartAndShapes(t *testing.T, ctx context.Context, owner, runtime, admin, worker *pgxpool.Pool) {
	t.Helper()
	claim := seedDeliveryFactsPending(t, ctx, owner, 3, "rec_delivery", false, 10*time.Minute, time.Hour)
	start := startDeliveryFacts(t, ctx, runtime, claim, "pending", 0)
	hash := sha256.Sum256(start.capability)
	var storedHash []byte
	var startDocument string
	if err := owner.QueryRow(ctx, `select reporting_capability_hash,to_jsonb(s)::text from public.record_notification_delivery_starts s where attempt_id=$1`, start.attempt["attempt_id"]).Scan(&storedHash, &startDocument); err != nil {
		t.Fatalf("read persisted start binding: %v", err)
	}
	if !bytes.Equal(storedHash, hash[:]) || bytes.Equal(storedHash, start.capability) || strings.Contains(startDocument, hex.EncodeToString(start.capability)) {
		t.Fatal("start did not retain only the capability hash")
	}
	_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_start($1::bytea)`, start.startBody)
	requirePostgresSQLState(t, err, "55000")
	var starts int
	if err := owner.QueryRow(ctx, `select count(*)::int from public.record_notification_delivery_starts where delivery_id=$1`, claim["delivery_id"]).Scan(&starts); err != nil || starts != 1 {
		t.Fatalf("start replay created %d start bindings: %v", starts, err)
	}
	// The generic queue commands must not dispose of an attempt whose provider
	// result has to be reported and finalized through the retained protocol.
	var queueAttempts int64
	var nextAttempt time.Time
	if err := owner.QueryRow(ctx, `select attempt_count,next_attempt_at from public.record_outbox where outbox_row_id=$1`, claim["outbox_row_id"]).Scan(&queueAttempts, &nextAttempt); err != nil {
		t.Fatalf("read started delivery queue tuple: %v", err)
	}
	queueCommand := map[string]any{
		"project_id": claim["project_id"], "event_kind": claim["event_kind"], "subject_kind": claim["subject_kind"],
		"subject_id": claim["delivery_id"], "record_id": claim["record_id"], "source_version": claim["source_version"],
		"authorization_epoch": claim["authorization_epoch"], "record_fence_epoch": claim["record_fence_epoch"],
		"outbox_row_id": claim["outbox_row_id"], "status": "processing", "owner_id": claim["owner_id"],
		"owner_generation": claim["owner_generation"], "owner_expires_at": claim["owner_expires_at"],
		"attempt_count": queueAttempts, "next_attempt_at": deliveryFactsTimestamp(nextAttempt),
		"event_expires_at": claim["event_expires_at"], "new_owner_id": "", "delay_us": int64(0),
	}
	for _, function := range []string{"record_outbox_retry", "record_outbox_sent", "record_outbox_cancel"} {
		command := deliveryFactsCopy(queueCommand)
		if function == "record_outbox_retry" {
			command["delay_us"] = int64(250000)
		}
		_, err := runtime.Exec(ctx, `select * from public.`+function+`($1::bytea)`, deliveryFactsJSON(t, command))
		requirePostgresSQLState(t, err, "55000")
	}
	var queueStatus, deliveryState string
	if err := owner.QueryRow(ctx, `select o.status,d.delivery_state from public.record_outbox o
		join public.record_notification_deliveries d on d.delivery_id=o.subject_id where o.outbox_row_id=$1`, claim["outbox_row_id"]).Scan(&queueStatus, &deliveryState); err != nil || queueStatus != "processing" || deliveryState != "processing" {
		t.Fatalf("generic queue commands changed started delivery: queue=%q delivery=%q error=%v", queueStatus, deliveryState, err)
	}
	for _, malformed := range []string{`null`, `[]`, `{}`, `{"owner_id":"a","owner_id":"b"}`} {
		_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_outcome($1::bytea)`, []byte(malformed))
		requirePostgresSQLState(t, err, "22023")
	}
	valid := deliveryFactsReportCommand(start, "sent")
	body := deliveryFactsJSON(t, valid)
	for _, db := range []*pgxpool.Pool{admin, worker} {
		_, err := db.Exec(ctx, `select * from public.record_external_delivery_outcome($1::bytea)`, body)
		requirePostgresSQLState(t, err, "42501")
	}
	for _, malformed := range [][]byte{
		append(append([]byte(nil), body[:len(body)-1]...), []byte(`,"owner_id":"delivery_owner"}`)...),
		append(append([]byte(nil), body[:len(body)-1]...), []byte(`,"unexpected":true}`)...),
	} {
		_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_outcome($1::bytea)`, malformed)
		requirePostgresSQLState(t, err, "22023")
	}
	for _, tc := range []struct {
		field string
		value any
		state string
	}{
		{"owner_generation", "1", "22023"},
		{"attempt_id", "rna_" + strings.Repeat("00", 32), "22023"},
		{"reporting_capability", "bad", "22023"},
		{"outcome", "provider_raw_response", "22023"},
		{"reporting_capability", strings.Repeat("00", 32), "55000"},
		{"owner_id", "wrong_owner", "55000"},
		{"owner_generation", int64(2), "55000"},
		{"recipient_user_id", "usr_wrong", "55000"},
	} {
		candidate := deliveryFactsCopy(valid)
		candidate[tc.field] = tc.value
		_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_outcome($1::bytea)`, deliveryFactsJSON(t, candidate))
		requirePostgresSQLState(t, err, tc.state)
	}
	var facts int
	if err := owner.QueryRow(ctx, `select count(*)::int from public.record_notification_delivery_facts where attempt_id=$1`, start.attempt["attempt_id"]).Scan(&facts); err != nil || facts != 0 {
		t.Fatalf("rejected provider reports persisted %d facts: %v", facts, err)
	}
	digest := deliveryFactsReport(t, ctx, runtime, start, "sent")
	correctFinalization := deliveryFactsFinalizeCommand(start, claim, digest, 0)
	expiry, err := time.Parse(time.RFC3339Nano, claim["owner_expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		field string
		value any
		state string
	}{
		{"owner_id", "wrong_owner", "55000"},
		{"owner_generation", int64(2), "55000"},
		{"owner_expires_at", deliveryFactsTimestamp(expiry.Add(-time.Microsecond)), "55000"},
		{"evidence_digest", strings.Repeat("00", 32), "55000"},
		{"evidence_digest", "not-a-digest", "22023"},
	} {
		candidate := deliveryFactsCopy(correctFinalization)
		candidate[tc.field] = tc.value
		_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_finalize($1::bytea)`, deliveryFactsJSON(t, candidate))
		requirePostgresSQLState(t, err, tc.state)
	}
	var attempts int
	if err := owner.QueryRow(ctx, `select count(*)::int from public.record_notification_delivery_attempts where delivery_id=$1`, claim["delivery_id"]).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("unauthorized finalization created %d attempts: %v", attempts, err)
	}
	cancel := deliveryFactsCopy(claim)
	cancel["expected_state"] = "pending"
	cancel["expected_attempt_count"] = int64(0)
	cancel["reason"] = "authorization_revoked"
	_, err = runtime.Exec(ctx, `select public.record_external_delivery_cancel($1::bytea)`, deliveryFactsJSON(t, cancel))
	requirePostgresSQLState(t, err, "55000")
}

func assertDeliveryFactsNormalOutcomes(t *testing.T, ctx context.Context, owner, runtime, observer *pgxpool.Pool) {
	t.Helper()
	for index, tc := range []struct {
		name, outcome, state, attemptReason, deliveryReason, disposition, queueStatus string
		attemptNo, delayUS                                                            int64
	}{
		{"sent", "sent", "sent", "", "", "complete", "sent", 1, 0},
		{"permanent", "permanent_failure", "permanent_failure", "provider_permanent_failure", "provider_permanent_failure", "complete", "sent", 1, 0},
		{"unknown", "unknown_outcome", "unknown_outcome", "provider_unknown_outcome", "provider_unknown_outcome", "complete", "sent", 1, 0},
		{"temporary", "temporary_failure", "retry_wait", "provider_temporary_failure", "provider_temporary_failure", "retry", "pending", 1, 250000},
		{"exhausted", "temporary_failure", "permanent_failure", "provider_temporary_failure", "attempts_exhausted", "complete", "sent", 8, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claim := seedDeliveryFactsPending(t, ctx, owner, 10+index, "rec_delivery", false, 10*time.Minute, time.Hour)
			state := "pending"
			var start deliveryFactsStart
			for previous := int64(0); previous < tc.attemptNo; previous++ {
				start = startDeliveryFacts(t, ctx, runtime, claim, state, previous)
				if previous+1 < tc.attemptNo {
					priorDigest := deliveryFactsReport(t, ctx, runtime, start, "temporary_failure")
					prior := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(start, claim, priorDigest, 1))
					if prior.disposition != "retry" || prior.delayUS != 1 || prior.nextAttemptAt == nil {
						t.Fatalf("prepare next attempt: %#v", prior)
					}
					claim = deliveryFactsClaim(t, ctx, owner, runtime, observer, claim, fmt.Sprintf("retry_owner%d", previous+1))
					state = "retry_wait"
				}
			}
			digest := deliveryFactsReport(t, ctx, runtime, start, tc.outcome)
			repeatedDigest := deliveryFactsReport(t, ctx, runtime, start, tc.outcome)
			if !bytes.Equal(digest, repeatedDigest) {
				t.Fatal("exact provider report returned a different fact locator")
			}
			var beforeState, beforeQueue string
			var beforeAttempts, providerFacts int
			if err := owner.QueryRow(ctx, `select d.delivery_state,o.status,
				(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
				(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='provider_report')
				from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$3 where d.delivery_id=$1`,
				claim["delivery_id"], start.attempt["attempt_id"], claim["outbox_row_id"]).Scan(&beforeState, &beforeQueue, &beforeAttempts, &providerFacts); err != nil ||
				beforeState != "processing" || beforeQueue != "processing" || int64(beforeAttempts) != tc.attemptNo-1 || providerFacts != 1 {
				t.Fatalf("report before finalize = (%q,%q,%d,%d), %v", beforeState, beforeQueue, beforeAttempts, providerFacts, err)
			}
			command := deliveryFactsFinalizeCommand(start, claim, digest, tc.delayUS)
			result := deliveryFactsFinalize(t, ctx, runtime, command)
			if result.disposition != tc.disposition || result.delayUS != tc.delayUS || (result.nextAttemptAt != nil) != (tc.disposition == "retry") {
				t.Fatalf("finalized %s = %#v", tc.outcome, result)
			}
			if tc.name == "sent" {
				var factDocument, completionDocument string
				if err := owner.QueryRow(ctx, `select to_jsonb(f)::text,to_jsonb(c)::text from public.record_notification_delivery_facts f
					join public.record_notification_delivery_completions c on c.attempt_id=f.attempt_id
					where f.attempt_id=$1 and f.fact_kind='provider_report'`, start.attempt["attempt_id"]).Scan(&factDocument, &completionDocument); err != nil {
					t.Fatalf("read retained delivery evidence: %v", err)
				}
				capabilityHex := hex.EncodeToString(start.capability)
				if strings.Contains(factDocument, capabilityHex) || strings.Contains(completionDocument, capabilityHex) {
					t.Fatal("retained fact or completion contains the raw reporting capability")
				}
			}
			var actualState, actualOutcome, attemptReason, deliveryReason, queueStatus string
			var attempts, completions int
			var deliveryNext, queueNext *time.Time
			if err := owner.QueryRow(ctx, `select d.delivery_state,a.outcome,a.reason_code,d.reason_code,o.status,
				d.next_attempt_at,case when o.status='pending' then o.next_attempt_at end,
				(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
				(select count(*)::int from public.record_notification_delivery_completions where delivery_id=d.delivery_id and attempt_no=$3)
				from public.record_notification_deliveries d join public.record_notification_delivery_attempts a on a.delivery_id=d.delivery_id
				join public.record_outbox o on o.outbox_row_id=$4 where d.delivery_id=$1 and a.attempt_id=$2`,
				claim["delivery_id"], start.attempt["attempt_id"], tc.attemptNo, claim["outbox_row_id"]).Scan(
				&actualState, &actualOutcome, &attemptReason, &deliveryReason, &queueStatus, &deliveryNext, &queueNext, &attempts, &completions); err != nil {
				t.Fatalf("read finalized outcome: %v", err)
			}
			if actualState != tc.state || actualOutcome != tc.outcome || attemptReason != tc.attemptReason || deliveryReason != tc.deliveryReason ||
				queueStatus != tc.queueStatus || int64(attempts) != tc.attemptNo || completions != 1 {
				t.Fatalf("finalized projection = (%q,%q,%q,%q,%q,%d,%d)", actualState, actualOutcome, attemptReason, deliveryReason, queueStatus, attempts, completions)
			}
			if result.nextAttemptAt != nil && (deliveryNext == nil || queueNext == nil || !result.nextAttemptAt.Equal(*deliveryNext) || !result.nextAttemptAt.Equal(*queueNext)) {
				t.Fatal("delivery and outbox did not persist the same retry deadline")
			}
			replayCommand := deliveryFactsCopy(command)
			if tc.disposition == "retry" {
				replayCommand["retry_after_us"] = int64(9000000)
			}
			replay := deliveryFactsFinalize(t, ctx, runtime, replayCommand)
			if replay.disposition != result.disposition || replay.delayUS != result.delayUS ||
				(replay.nextAttemptAt == nil) != (result.nextAttemptAt == nil) ||
				(result.nextAttemptAt != nil && !replay.nextAttemptAt.Equal(*result.nextAttemptAt)) {
				t.Fatalf("completion replay moved the result: first=%#v replay=%#v", result, replay)
			}
			if err := owner.QueryRow(ctx, `select count(*)::int from public.record_notification_delivery_attempts where delivery_id=$1`, claim["delivery_id"]).Scan(&attempts); err != nil || int64(attempts) != tc.attemptNo {
				t.Fatalf("completion replay changed attempt count: %d, %v", attempts, err)
			}
			if tc.disposition == "retry" {
				// A retry is a fresh queue claim and a fresh start, not a replay of
				// the prior provider call or an extension of its retry time.
				current := deliveryFactsClaim(t, ctx, owner, runtime, observer, claim, "retry_after_replay")
				_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_finalize($1::bytea)`, deliveryFactsJSON(t, command))
				requirePostgresSQLState(t, err, "55000")
				next := startDeliveryFacts(t, ctx, runtime, current, "retry_wait", 1)
				nextDigest := deliveryFactsReport(t, ctx, runtime, next, "sent")
				nextResult := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(next, current, nextDigest, 0))
				if nextResult.disposition != "complete" {
					t.Fatalf("fresh retry outcome = %#v", nextResult)
				}
			}
		})
	}
}

func assertDeliveryFactsTakeover(t *testing.T, ctx context.Context, owner, runtime, observer *pgxpool.Pool) {
	t.Helper()
	sentClaim := seedDeliveryFactsPending(t, ctx, owner, 20, "rec_delivery", false, 4*time.Second, time.Hour)
	retryClaim := seedDeliveryFactsPending(t, ctx, owner, 21, "rec_delivery", false, 4*time.Second, time.Hour)
	sentStart := startDeliveryFacts(t, ctx, runtime, sentClaim, "pending", 0)
	retryStart := startDeliveryFacts(t, ctx, runtime, retryClaim, "pending", 0)
	// The first report commits before the original owner expires. Recovery
	// must consume it, not turn it into an unknown or resend the provider call.
	sentDigest := deliveryFactsReport(t, ctx, runtime, sentStart, "sent")
	sentCurrent := deliveryFactsClaim(t, ctx, owner, runtime, observer, sentClaim, "takeover_sent")
	retryCurrent := deliveryFactsClaim(t, ctx, owner, runtime, observer, retryClaim, "takeover_retry")
	if again := deliveryFactsReport(t, ctx, runtime, sentStart, "sent"); !bytes.Equal(sentDigest, again) {
		t.Fatal("original sender could not exactly report after takeover")
	}
	// This fact is first reported by the original sender after takeover.
	retryDigest := deliveryFactsReport(t, ctx, runtime, retryStart, "temporary_failure")
	for _, item := range []struct {
		start  deliveryFactsStart
		digest []byte
		delay  int64
	}{{sentStart, sentDigest, 0}, {retryStart, retryDigest, 250000}} {
		old := deliveryFactsFinalizeCommand(item.start, item.start.claim, item.digest, item.delay)
		_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_finalize($1::bytea)`, deliveryFactsJSON(t, old))
		requirePostgresSQLState(t, err, "55000")
	}
	sentResult := deliveryFactsReconcile(t, ctx, runtime, deliveryFactsReconcileCommand(sentStart, sentCurrent, 250000))
	if sentResult.disposition != "complete" || sentResult.delayUS != 0 {
		t.Fatalf("takeover did not consume committed sent fact: %#v", sentResult)
	}
	retryResult := deliveryFactsReconcile(t, ctx, runtime, deliveryFactsReconcileCommand(retryStart, retryCurrent, 250000))
	if retryResult.disposition != "retry" || retryResult.delayUS != 250000 || retryResult.nextAttemptAt == nil {
		t.Fatalf("takeover lost temporary failure retry: %#v", retryResult)
	}
	for _, tc := range []struct {
		start                  deliveryFactsStart
		wantState, wantOutcome string
	}{{sentStart, "sent", "sent"}, {retryStart, "retry_wait", "temporary_failure"}} {
		var state, outcome, originalOwner string
		var generation int64
		var attempts, facts int
		if err := owner.QueryRow(ctx, `select d.delivery_state,a.outcome,s.owner_id,s.owner_generation,
			(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
			(select count(*)::int from public.record_notification_delivery_facts where attempt_id=s.attempt_id and fact_kind='provider_report')
			from public.record_notification_deliveries d join public.record_notification_delivery_attempts a on a.delivery_id=d.delivery_id
			join public.record_notification_delivery_starts s on s.attempt_id=a.attempt_id where d.delivery_id=$1`, tc.start.claim["delivery_id"]).Scan(
			&state, &outcome, &originalOwner, &generation, &attempts, &facts); err != nil || state != tc.wantState || outcome != tc.wantOutcome ||
			originalOwner != "delivery_owner" || generation != 1 || attempts != 1 || facts != 1 {
			t.Fatalf("takeover projection = (%q,%q,%q,%d,%d,%d), %v", state, outcome, originalOwner, generation, attempts, facts, err)
		}
	}
	replay := deliveryFactsReconcile(t, ctx, runtime, deliveryFactsReconcileCommand(retryStart, retryCurrent, 9000000))
	if replay.disposition != "retry" || replay.delayUS != retryResult.delayUS || replay.nextAttemptAt == nil || !replay.nextAttemptAt.Equal(*retryResult.nextAttemptAt) {
		t.Fatalf("takeover retry replay changed the deadline: %#v", replay)
	}
}

func assertDeliveryFactsLateConflictClosesRetry(t *testing.T, parent context.Context, owner, runtime, contender, observer *pgxpool.Pool) {
	t.Helper()
	for index, outcome := range []string{"sent", "unknown_outcome"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(parent, 20*time.Second)
			defer cancel()
			claim := seedDeliveryFactsPending(t, ctx, owner, 70+index, "rec_delivery", false, 10*time.Minute, time.Hour)
			start := startDeliveryFacts(t, ctx, runtime, claim, "pending", 0)
			temporary := deliveryFactsReport(t, ctx, runtime, start, "temporary_failure")
			finalized := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(start, claim, temporary, 250000))
			if finalized.disposition != "retry" || finalized.delayUS != 250000 || finalized.nextAttemptAt == nil {
				t.Fatalf("temporary outcome did not schedule retry: %#v", finalized)
			}
			var completionBefore string
			var completionDue time.Time
			if err := owner.QueryRow(ctx, `select to_jsonb(c)::text,c.next_attempt_at
				from public.record_notification_delivery_completions c where c.delivery_id=$1 and c.attempt_no=1`, claim["delivery_id"]).Scan(&completionBefore, &completionDue); err != nil || !completionDue.Equal(*finalized.nextAttemptAt) {
				t.Fatalf("read original retry completion: due=%v error=%v", completionDue, err)
			}
			current := deliveryFactsClaim(t, ctx, owner, runtime, observer, claim, "conflicting_retry_owner")
			lateDigest, closed := deliveryFactsRaceLateReportAndRetry(t, ctx, runtime, contender, observer, start, current, outcome)
			startedAt, err := time.Parse(time.RFC3339Nano, start.attempt["started_at"].(string))
			if err != nil {
				t.Fatal(err)
			}
			if closed.number != 1 || !closed.startedAt.Equal(startedAt) || closed.capability != nil {
				t.Fatalf("conflicting retry returned attempt=%d started=%v capability_bytes=%d; want original attempt and NULL capability",
					closed.number, closed.startedAt, len(closed.capability))
			}
			if len(lateDigest) != 32 || bytes.Equal(lateDigest, temporary) {
				t.Fatal("conflicting provider report did not retain a distinct fact")
			}
			checkRetained := func() {
				t.Helper()
				var state, queue, attemptOutcome, completionAfter string
				var generation, deliveryAttempts int64
				var queueDue time.Time
				var starts, attempts, completions, reports, originalFacts, lateFacts int
				err := owner.QueryRow(ctx, `select d.delivery_state,d.attempt_count,o.status,o.owner_generation,o.next_attempt_at,
					(select count(*)::int from public.record_notification_delivery_starts where delivery_id=d.delivery_id),
					(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
					(select count(*)::int from public.record_notification_delivery_completions where delivery_id=d.delivery_id),
					(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='provider_report'),
					(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='provider_report' and evidence_digest=$3 and outcome='temporary_failure'),
					(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='provider_report' and evidence_digest=$4 and outcome=$5),
					(select a.outcome from public.record_notification_delivery_attempts a where a.delivery_id=d.delivery_id and a.attempt_no=1),
					(select to_jsonb(c)::text from public.record_notification_delivery_completions c where c.delivery_id=d.delivery_id and c.attempt_no=1)
					from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$6 where d.delivery_id=$1`,
					claim["delivery_id"], start.attempt["attempt_id"], temporary, lateDigest, outcome, claim["outbox_row_id"]).Scan(
					&state, &deliveryAttempts, &queue, &generation, &queueDue, &starts, &attempts, &completions, &reports,
					&originalFacts, &lateFacts, &attemptOutcome, &completionAfter)
				if err != nil || state != "unknown_outcome" || deliveryAttempts != 1 || queue != "sent" ||
					generation != current["owner_generation"].(int64) || !queueDue.Equal(completionDue) ||
					starts != 1 || attempts != 1 || completions != 1 || reports != 2 || originalFacts != 1 || lateFacts != 1 ||
					attemptOutcome != "temporary_failure" || completionAfter != completionBefore {
					t.Fatalf("conflicting retry changed retained evidence: state=%q delivery_attempts=%d queue=%q generation=%d due=%v starts=%d attempts=%d completions=%d reports=%d original=%d late=%d outcome=%q completion_unchanged=%t error=%v",
						state, deliveryAttempts, queue, generation, queueDue, starts, attempts, completions, reports, originalFacts, lateFacts,
						attemptOutcome, completionAfter == completionBefore, err)
				}
			}
			checkRetained()
			if repeated := deliveryFactsReport(t, ctx, runtime, start, outcome); !bytes.Equal(repeated, lateDigest) {
				t.Fatal("exact late report changed its fact locator")
			}
			checkRetained()
			var classification string
			var mismatched bool
			if err := runtime.QueryRow(ctx, `select classification,mismatched from public.record_collaboration_delivery_inventory($1,$2) where delivery_id=$3`,
				claim["record_id"], claim["record_fence_epoch"], claim["delivery_id"]).Scan(&classification, &mismatched); err != nil || classification != "possible_external" || mismatched {
				t.Fatalf("conflicting retry disclosure = (%q,%t), %v", classification, mismatched, err)
			}
		})
	}
}

type deliveryFactsRetryStartResult struct {
	number     int64
	startedAt  time.Time
	capability []byte
	err        error
}

// Hold the original sender's fact uncommitted while the new queue owner starts
// its retry. The start must wait for that exact report or request a restart.
func deliveryFactsRaceLateReportAndRetry(t *testing.T, ctx context.Context, runtime, contender, observer *pgxpool.Pool, start deliveryFactsStart, current map[string]any, outcome string) ([]byte, deliveryFactsRetryStartResult) {
	t.Helper()
	reportTx, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reportTx.Rollback(context.Background()) }()
	reportPID := postgresTransactionBackendPID(t, ctx, reportTx)
	var attemptID string
	var digest []byte
	if err := reportTx.QueryRow(ctx, `select attempt_id,evidence_digest from public.record_external_delivery_outcome($1::bytea)`,
		deliveryFactsJSON(t, deliveryFactsReportCommand(start, outcome))).Scan(&attemptID, &digest); err != nil || attemptID != start.attempt["attempt_id"] {
		t.Fatalf("write concurrent late report: attempt=%q error=%v", attemptID, err)
	}
	startTx, err := contender.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	startPID := postgresTransactionBackendPID(t, ctx, startTx)
	command := deliveryFactsCopy(current)
	command["expected_state"] = "retry_wait"
	command["expected_attempt_count"] = int64(1)
	body := deliveryFactsJSON(t, command)
	results := make(chan deliveryFactsRetryStartResult, 1)
	pending := true
	defer func() {
		// Release the report lock before trying to close a blocked start.
		_ = reportTx.Rollback(context.Background())
		if pending {
			select {
			case <-results:
			case <-ctx.Done():
			}
		}
		_ = startTx.Rollback(context.Background())
	}()
	go func() {
		var result deliveryFactsRetryStartResult
		result.err = startTx.QueryRow(ctx, `select attempt_no,started_at,reporting_capability from public.record_external_delivery_start($1::bytea)`, body).Scan(
			&result.number, &result.startedAt, &result.capability)
		results <- result
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var result deliveryFactsRetryStartResult
	early := false
waitForSerialization:
	for {
		var blockedByReport bool
		if err := observer.QueryRow(ctx, `select $1::int=any(pg_catalog.pg_blocking_pids($2::int))`, reportPID, startPID).Scan(&blockedByReport); err != nil {
			t.Fatal(err)
		}
		if blockedByReport {
			break
		}
		select {
		case result = <-results:
			pending = false
			early = true
			break waitForSerialization
		case <-ctx.Done():
			t.Fatalf("retry did not reach the original report lock: %v", ctx.Err())
		case <-ticker.C:
		}
	}
	if early {
		var state interface{ SQLState() string }
		if !errors.As(result.err, &state) || (state.SQLState() != "40001" && state.SQLState() != "55P03") {
			t.Fatalf("retry proceeded before the competing report committed: attempt=%d capability_bytes=%d error=%v",
				result.number, len(result.capability), result.err)
		}
		if err := startTx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := reportTx.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent late report: %v", err)
	}
	if early {
		result.err = contender.QueryRow(ctx, `select attempt_no,started_at,reporting_capability from public.record_external_delivery_start($1::bytea)`, body).Scan(
			&result.number, &result.startedAt, &result.capability)
	} else {
		select {
		case result = <-results:
			pending = false
		case <-ctx.Done():
			t.Fatalf("retry did not finish after the report committed: %v", ctx.Err())
		}
		if result.err == nil {
			result.err = startTx.Commit(ctx)
		}
	}
	if result.err != nil {
		t.Fatalf("close retry after committed report: %v", result.err)
	}
	return digest, result
}

func assertDeliveryFactsConflict(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool) {
	t.Helper()
	claim := seedDeliveryFactsPending(t, ctx, owner, 30, "rec_delivery", false, 10*time.Minute, time.Hour)
	start := startDeliveryFacts(t, ctx, runtime, claim, "pending", 0)
	sent := deliveryFactsReport(t, ctx, runtime, start, "sent")
	temporary := deliveryFactsReport(t, ctx, runtime, start, "temporary_failure")
	if bytes.Equal(sent, temporary) {
		t.Fatal("contradictory provider reports shared a fact locator")
	}
	result := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(start, claim, sent, 250000))
	if result.disposition != "complete" || result.delayUS != 0 || result.nextAttemptAt != nil {
		t.Fatalf("conflicting provider reports finalized as %#v", result)
	}
	var state, outcome, reason, classification string
	var mismatched bool
	var attempts, reports, observations int
	if err := owner.QueryRow(ctx, `select d.delivery_state,a.outcome,a.reason_code,
		(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='provider_report'),
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='drain_observation')
		from public.record_notification_deliveries d join public.record_notification_delivery_attempts a on a.delivery_id=d.delivery_id
		where d.delivery_id=$1 and a.attempt_id=$2`, claim["delivery_id"], start.attempt["attempt_id"]).Scan(
		&state, &outcome, &reason, &attempts, &reports, &observations); err != nil || state != "unknown_outcome" ||
		outcome != "unknown_outcome" || reason != "lease_takeover_unknown" || attempts != 1 || reports != 2 || observations != 1 {
		t.Fatalf("conflicting facts = (%q,%q,%q,%d,%d,%d), %v", state, outcome, reason, attempts, reports, observations, err)
	}
	if err := runtime.QueryRow(ctx, `select classification,mismatched from public.record_collaboration_delivery_inventory($1,$2) where delivery_id=$3`,
		claim["record_id"], claim["record_fence_epoch"], claim["delivery_id"]).Scan(&classification, &mismatched); err != nil || classification != "possible_external" || mismatched {
		t.Fatalf("conflicting fact disclosure = (%q,%t), %v", classification, mismatched, err)
	}
	if again := deliveryFactsReport(t, ctx, runtime, start, "sent"); !bytes.Equal(again, sent) {
		t.Fatal("exact report after conflict did not retain its original fact")
	}
	for _, statement := range []string{
		`update public.record_notification_delivery_starts set owner_id='replacement' where attempt_id=$1`,
		`delete from public.record_notification_delivery_starts where attempt_id=$1`,
		`update public.record_notification_delivery_facts set outcome='sent' where attempt_id=$1`,
		`delete from public.record_notification_delivery_facts where attempt_id=$1`,
		`delete from public.record_notification_delivery_completions where attempt_id=$1`,
	} {
		_, err := owner.Exec(ctx, statement, start.attempt["attempt_id"])
		requirePostgresSQLState(t, err, "55000")
	}
	replay := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(start, claim, sent, 9000000))
	if replay.disposition != "complete" || replay.delayUS != 0 || replay.nextAttemptAt != nil {
		t.Fatalf("conflict completion replay changed outcome: %#v", replay)
	}
}

func assertDeliveryFactsWaitExpiry(t *testing.T, parent context.Context, owner, runtime, observer *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	claim := seedDeliveryFactsPending(t, ctx, owner, 31, "rec_delivery", false, 5*time.Second, time.Hour)
	start := startDeliveryFacts(t, ctx, runtime, claim, "pending", 0)
	digest := deliveryFactsReport(t, ctx, runtime, start, "sent")
	body := deliveryFactsJSON(t, deliveryFactsFinalizeCommand(start, claim, digest, 0))
	expires, err := time.Parse(time.RFC3339Nano, claim["owner_expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	holder, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	var locked int
	if err := holder.QueryRow(ctx, `select 1 from public.record_notification_deliveries where delivery_id=$1 for update`, claim["delivery_id"]).Scan(&locked); err != nil || locked != 1 {
		t.Fatalf("lock delivery before finalization: value=%d error=%v", locked, err)
	}
	finisher, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = finisher.Rollback(ctx) }()
	pid := postgresTransactionBackendPID(t, ctx, finisher)
	result := make(chan error, 1)
	go func() {
		var disposition string
		var delay int64
		var next *time.Time
		result <- finisher.QueryRow(ctx, `select disposition,retry_after_us,next_attempt_at from public.record_external_delivery_finalize($1::bytea)`, body).Scan(&disposition, &delay, &next)
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := observer.QueryRow(ctx, `select cardinality(pg_blocking_pids($1))>0`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("finalization returned before reaching the locked delivery: %v", err)
		case <-ctx.Done():
			t.Fatalf("finalization did not reach the delivery lock: %v", ctx.Err())
		case <-ticker.C:
		}
	}
	deliveryFactsWaitClock(t, ctx, observer, expires)
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	requirePostgresSQLState(t, awaitPostgresConflictResult(t, ctx, result), "55000")
	if err := finisher.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var state, queue string
	var attempts, facts int
	if err := owner.QueryRow(ctx, `select d.delivery_state,o.status,
		(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='provider_report')
		from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$3 where d.delivery_id=$1`,
		claim["delivery_id"], start.attempt["attempt_id"], claim["outbox_row_id"]).Scan(&state, &queue, &attempts, &facts); err != nil ||
		state != "processing" || queue != "processing" || attempts != 0 || facts != 1 {
		t.Fatalf("expired finalizer changed projection or lost fact: (%q,%q,%d,%d), %v", state, queue, attempts, facts, err)
	}
}

func assertDeliveryFactsEventExpiry(t *testing.T, ctx context.Context, owner, runtime, observer *pgxpool.Pool) {
	t.Helper()
	sentClaim := seedDeliveryFactsPending(t, ctx, owner, 32, "rec_delivery", false, 10*time.Minute, 5*time.Second)
	retryClaim := seedDeliveryFactsPending(t, ctx, owner, 33, "rec_delivery", false, 10*time.Minute, 5*time.Second)
	sentStart := startDeliveryFacts(t, ctx, runtime, sentClaim, "pending", 0)
	retryStart := startDeliveryFacts(t, ctx, runtime, retryClaim, "pending", 0)
	expires, err := time.Parse(time.RFC3339Nano, retryClaim["event_expires_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	deliveryFactsWaitClock(t, ctx, observer, expires)
	for _, tc := range []struct {
		start                                      deliveryFactsStart
		claim                                      map[string]any
		outcome, disposition, state, queue, reason string
		delay                                      int64
	}{
		{sentStart, sentClaim, "sent", "complete", "sent", "sent", "", 0},
		{retryStart, retryClaim, "temporary_failure", "cancel", "cancelled", "cancelled", "event_expired", 250000},
	} {
		digest := deliveryFactsReport(t, ctx, runtime, tc.start, tc.outcome)
		result := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(tc.start, tc.claim, digest, tc.delay))
		if result.disposition != tc.disposition || result.delayUS != 0 || result.nextAttemptAt != nil {
			t.Fatalf("expired event %s result = %#v", tc.outcome, result)
		}
		var state, queue, reason, actualOutcome string
		if err := owner.QueryRow(ctx, `select d.delivery_state,o.status,d.reason_code,a.outcome from public.record_notification_deliveries d
			join public.record_notification_delivery_attempts a on a.delivery_id=d.delivery_id
			join public.record_outbox o on o.outbox_row_id=$3 where d.delivery_id=$1 and a.attempt_id=$2`,
			tc.claim["delivery_id"], tc.start.attempt["attempt_id"], tc.claim["outbox_row_id"]).Scan(&state, &queue, &reason, &actualOutcome); err != nil ||
			state != tc.state || queue != tc.queue || reason != tc.reason || actualOutcome != tc.outcome {
			t.Fatalf("expired event projection = (%q,%q,%q,%q), %v", state, queue, reason, actualOutcome, err)
		}
	}
}

func assertDeliveryFactsEpochBoundaries(t *testing.T, ctx context.Context, owner, runtime, maintenance *pgxpool.Pool) {
	t.Helper()
	staleClaim := seedDeliveryFactsPending(t, ctx, owner, 40, "rec_deliveryepoch", false, 10*time.Minute, time.Hour)
	missingClaim := seedDeliveryFactsPending(t, ctx, owner, 41, "rec_deliveryepoch", false, 10*time.Minute, time.Hour)
	pending := seedDeliveryFactsPending(t, ctx, owner, 42, "rec_deliveryepoch", false, 10*time.Minute, time.Hour)
	stale := startDeliveryFacts(t, ctx, runtime, staleClaim, "pending", 0)
	missing := startDeliveryFacts(t, ctx, runtime, missingClaim, "pending", 0)
	// This fixture transition is an epoch-drift control, not an issuer or
	// authorized purge. It must prevent new dispatch and retry scheduling.
	if _, err := maintenance.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=5
		where project_id='default' and object_kind='record' and object_id='rec_deliveryepoch'`); err != nil {
		t.Fatal(err)
	}
	startCommand := deliveryFactsCopy(pending)
	startCommand["expected_state"] = "pending"
	startCommand["expected_attempt_count"] = int64(0)
	_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_start($1::bytea)`, deliveryFactsJSON(t, startCommand))
	requirePostgresSQLState(t, err, "55000")
	digest := deliveryFactsReport(t, ctx, runtime, stale, "temporary_failure")
	result := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(stale, staleClaim, digest, 250000))
	if result.disposition != "cancel" || result.delayUS != 0 || result.nextAttemptAt != nil {
		t.Fatalf("stale epoch scheduled a retry: %#v", result)
	}
	var state, outcome, attemptReason, deliveryReason, queue string
	if err := owner.QueryRow(ctx, `select d.delivery_state,a.outcome,a.reason_code,d.reason_code,o.status
		from public.record_notification_deliveries d join public.record_notification_delivery_attempts a on a.delivery_id=d.delivery_id
		join public.record_outbox o on o.outbox_row_id=$2 where d.delivery_id=$1`, staleClaim["delivery_id"], staleClaim["outbox_row_id"]).Scan(
		&state, &outcome, &attemptReason, &deliveryReason, &queue); err != nil || state != "cancelled" || outcome != "temporary_failure" ||
		attemptReason != "provider_temporary_failure" || deliveryReason != "record_deleted" || queue != "cancelled" {
		t.Fatalf("stale epoch outcome = (%q,%q,%q,%q,%q), %v", state, outcome, attemptReason, deliveryReason, queue, err)
	}
	// A missing epoch does not invalidate the original capability's truthful
	// fact, but cannot authorize finalization or recreate epoch state.
	if _, err := maintenance.Exec(ctx, `delete from public.content_delivery_epochs
		where project_id='default' and object_kind='record' and object_id='rec_deliveryepoch'`); err != nil {
		t.Fatal(err)
	}
	missingDigest := deliveryFactsReport(t, ctx, runtime, missing, "sent")
	_, err = runtime.Exec(ctx, `select * from public.record_external_delivery_finalize($1::bytea)`, deliveryFactsJSON(t, deliveryFactsFinalizeCommand(missing, missingClaim, missingDigest, 0)))
	requirePostgresSQLState(t, err, "55000")
	var epochs, attempts, facts int
	if err := owner.QueryRow(ctx, `select
		(select count(*)::int from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id='rec_deliveryepoch'),
		(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=$1),
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='provider_report')`,
		missingClaim["delivery_id"], missing.attempt["attempt_id"]).Scan(&epochs, &attempts, &facts); err != nil || epochs != 0 || attempts != 0 || facts != 1 {
		t.Fatalf("missing epoch affected authority or fact: epochs=%d attempts=%d facts=%d error=%v", epochs, attempts, facts, err)
	}
}

func assertDeliveryFactsLateAfterRemoval(t *testing.T, ctx context.Context, owner, runtime, maintenance *pgxpool.Pool) {
	t.Helper()
	claim := seedDeliveryFactsPending(t, ctx, owner, 50, "rec_deliverylate", false, 4*time.Second, time.Hour)
	start := startDeliveryFacts(t, ctx, runtime, claim, "pending", 0)
	current := deliveryFactsClaim(t, ctx, owner, runtime, maintenance, claim, "takeover_late")
	unknown := deliveryFactsReconcile(t, ctx, runtime, deliveryFactsReconcileCommand(start, current, 250000))
	if unknown.disposition != "complete" || unknown.delayUS != 0 || unknown.nextAttemptAt != nil {
		t.Fatalf("takeover without fact did not terminalize unknown: %#v", unknown)
	}
	var drainFacts, providerFacts, completions int
	if err := owner.QueryRow(ctx, `select
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$1 and fact_kind='drain_observation'),
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$1 and fact_kind='provider_report'),
		(select count(*)::int from public.record_notification_delivery_completions where attempt_id=$1)`, start.attempt["attempt_id"]).Scan(
		&drainFacts, &providerFacts, &completions); err != nil || drainFacts != 1 || providerFacts != 0 || completions != 1 {
		t.Fatalf("unknown observation before late report = (%d,%d,%d), %v", drainFacts, providerFacts, completions, err)
	}

	// Construct an absent online projection with the isolated maintenance
	// connection. This tests only retained-command behavior; it is not evidence
	// that a deletion operation, collaboration drain or W1 issuer was run.
	tx, err := maintenance.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`delete from public.record_outbox where outbox_row_id=$1`, []any{claim["outbox_row_id"]}},
		{`delete from public.record_notification_delivery_attempts where delivery_id=$1`, []any{claim["delivery_id"]}},
		{`delete from public.record_notification_deliveries where delivery_id=$1`, []any{claim["delivery_id"]}},
		{`delete from public.record_notification_recipients where notification_id=$1`, []any{claim["notification_id"]}},
		{`delete from public.record_notifications where notification_id=$1`, []any{claim["notification_id"]}},
		{`update public.records set current_revision_id=null,current_title=null,current_record_type=null,current_impact_level=null,
		  current_visibility_scope=null,current_visibility_digest=null where record_id='rec_deliverylate'`, nil},
		{`delete from public.record_revision_subjects where revision_id='rrv_deliverylate'`, nil},
		{`delete from public.record_revisions where revision_id='rrv_deliverylate'`, nil},
		{`delete from public.records where record_id='rec_deliverylate'`, nil},
		{`delete from public.content_delivery_epochs where project_id='default' and object_kind='record' and object_id='rec_deliverylate'`, nil},
	} {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("construct absent online delivery projection: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit absent online delivery projection: %v", err)
	}
	var roots, epochs, queues, deliveries, starts int
	if err := owner.QueryRow(ctx, `select
		(select count(*)::int from public.records where record_id='rec_deliverylate'),
		(select count(*)::int from public.content_delivery_epochs where object_kind='record' and object_id='rec_deliverylate'),
		(select count(*)::int from public.record_outbox where outbox_row_id=$1),
		(select count(*)::int from public.record_notification_deliveries where delivery_id=$2),
		(select count(*)::int from public.record_notification_delivery_starts where attempt_id=$3)`,
		claim["outbox_row_id"], claim["delivery_id"], start.attempt["attempt_id"]).Scan(&roots, &epochs, &queues, &deliveries, &starts); err != nil ||
		roots != 0 || epochs != 0 || queues != 0 || deliveries != 0 || starts != 1 {
		t.Fatalf("absent online projection = (%d,%d,%d,%d,%d), %v", roots, epochs, queues, deliveries, starts, err)
	}
	// The old sender's capability appends a first provider fact after takeover,
	// unknown terminalization, and removal of all original online rows.
	digest := deliveryFactsReport(t, ctx, runtime, start, "sent")
	if again := deliveryFactsReport(t, ctx, runtime, start, "sent"); !bytes.Equal(again, digest) {
		t.Fatal("late exact provider report was not idempotent")
	}
	forged := deliveryFactsReportCommand(start, "sent")
	forged["reporting_capability"] = strings.Repeat("00", 32)
	_, err = runtime.Exec(ctx, `select * from public.record_external_delivery_outcome($1::bytea)`, deliveryFactsJSON(t, forged))
	requirePostgresSQLState(t, err, "55000")
	_, err = runtime.Exec(ctx, `select * from public.record_external_delivery_finalize($1::bytea)`, deliveryFactsJSON(t, deliveryFactsFinalizeCommand(start, current, digest, 0)))
	requirePostgresSQLState(t, err, "55000")
	if err := owner.QueryRow(ctx, `select
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$1 and fact_kind='drain_observation'),
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$1 and fact_kind='provider_report' and outcome='sent'),
		(select count(*)::int from public.record_notification_delivery_completions where attempt_id=$1),
		(select count(*)::int from public.record_outbox where outbox_row_id=$2),
		(select count(*)::int from public.record_notification_deliveries where delivery_id=$3),
		(select count(*)::int from public.records where record_id='rec_deliverylate')`,
		start.attempt["attempt_id"], claim["outbox_row_id"], claim["delivery_id"]).Scan(&drainFacts, &providerFacts, &completions, &queues, &deliveries, &roots); err != nil ||
		drainFacts != 1 || providerFacts != 1 || completions != 1 || queues != 0 || deliveries != 0 || roots != 0 {
		t.Fatalf("late report altered online queue or lost evidence: (%d,%d,%d,%d,%d,%d), %v", drainFacts, providerFacts, completions, queues, deliveries, roots, err)
	}
}

func assertDeliveryFactsLateUnknown(t *testing.T, ctx context.Context, owner, runtime *pgxpool.Pool) {
	t.Helper()
	claim := seedDeliveryFactsPending(t, ctx, owner, 34, "rec_delivery", false, 10*time.Minute, time.Hour)
	start := startDeliveryFacts(t, ctx, runtime, claim, "pending", 0)
	result := deliveryFactsReconcile(t, ctx, runtime, deliveryFactsReconcileCommand(start, claim, 250000))
	if result.disposition != "complete" || result.delayUS != 0 || result.nextAttemptAt != nil {
		t.Fatalf("unreported attempt reconciliation = %#v", result)
	}
	digest := deliveryFactsReport(t, ctx, runtime, start, "sent")
	var state, queue, attemptOutcome string
	var attempts, reports, observations int
	if err := owner.QueryRow(ctx, `select d.delivery_state,o.status,a.outcome,
		(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='provider_report'),
		(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$2 and fact_kind='drain_observation')
		from public.record_notification_deliveries d join public.record_notification_delivery_attempts a on a.delivery_id=d.delivery_id
		join public.record_outbox o on o.outbox_row_id=$3 where d.delivery_id=$1 and a.attempt_id=$2`,
		claim["delivery_id"], start.attempt["attempt_id"], claim["outbox_row_id"]).Scan(
		&state, &queue, &attemptOutcome, &attempts, &reports, &observations); err != nil || state != "unknown_outcome" ||
		queue != "sent" || attemptOutcome != "unknown_outcome" || attempts != 1 || reports != 1 || observations != 1 {
		t.Fatalf("late provider fact reopened unknown queue: (%q,%q,%q,%d,%d,%d), %v", state, queue, attemptOutcome, attempts, reports, observations, err)
	}
	_, err := runtime.Exec(ctx, `select * from public.record_external_delivery_finalize($1::bytea)`, deliveryFactsJSON(t, deliveryFactsFinalizeCommand(start, claim, digest, 0)))
	requirePostgresSQLState(t, err, "55000")
}

func assertDeliveryFactsRuntimeInsertGuard(t *testing.T, ctx context.Context, owner, runtime, observer *pgxpool.Pool) {
	t.Helper()
	const recipientID = "usr_dddddddddddddddddddddddd"
	const recordID = "rec_delivery"
	const bindingID = "binding_insert"
	notificationID := deliveryFactsNotification(recordID)
	if _, err := owner.Exec(ctx, `insert into public.users (user_id,username,password_hash,display_name,role)
		values ($1,$1,'fixture','Delivery insert recipient','admin')`, recipientID); err != nil {
		t.Fatalf("seed insert-guard recipient user: %v", err)
	}
	if _, err := owner.Exec(ctx, `insert into public.record_notification_recipients
		(notification_id,record_id,recipient_user_id,reason_kind,mandatory,authorization_epoch,record_fence_epoch)
		values ($1,$2,$3,'mention',true,1,4)`, notificationID, recordID, recipientID); err != nil {
		t.Fatalf("seed insert-guard recipient: %v", err)
	}
	deliveryID := recordcollaboration.NotificationDeliveryID(notificationID, recipientID, recordcollaboration.NotificationDeliveryTelegram, bindingID)
	if deliveryID == "" {
		t.Fatal("ordinary delivery identity was not constructed")
	}
	// These rows satisfy the predecessor's state/timestamp checks, but a
	// runtime INSERT must not manufacture a sent or already-started outcome.
	for _, statement := range []string{
		`insert into public.record_notification_deliveries
		 (delivery_id,record_id,notification_id,recipient_user_id,channel,binding_id,authorization_epoch,record_fence_epoch,delivery_state,attempt_count,sent_at)
		 values ($1,$2,$3,$4,'telegram',$5,1,4,'sent',1,clock_timestamp())`,
		`insert into public.record_notification_deliveries
		 (delivery_id,record_id,notification_id,recipient_user_id,channel,binding_id,authorization_epoch,record_fence_epoch,delivery_state,attempt_count)
		 values ($1,$2,$3,$4,'telegram',$5,1,4,'unknown_outcome',1)`,
		`insert into public.record_notification_deliveries
		 (delivery_id,record_id,notification_id,recipient_user_id,channel,binding_id,authorization_epoch,record_fence_epoch,delivery_state,attempt_count,attempt_started_at)
		 values ($1,$2,$3,$4,'telegram',$5,1,4,'processing',1,clock_timestamp())`,
	} {
		_, err := runtime.Exec(ctx, statement, deliveryID, recordID, notificationID, recipientID, bindingID)
		requirePostgresSQLState(t, err, "42501")
	}
	var forgedRows int
	if err := owner.QueryRow(ctx, `select count(*)::int from public.record_notification_deliveries where delivery_id=$1`, deliveryID).Scan(&forgedRows); err != nil || forgedRows != 0 {
		t.Fatalf("rejected terminal insert persisted %d deliveries: %v", forgedRows, err)
	}
	const pendingInsert = `insert into public.record_notification_deliveries
		(delivery_id,record_id,notification_id,recipient_user_id,channel,binding_id,authorization_epoch,record_fence_epoch)
		values ($1,$2,$3,$4,'telegram',$5,$6,$7)`
	for _, epochs := range [][2]int64{{2, 4}, {1, 3}} {
		_, err := runtime.Exec(ctx, pendingInsert, deliveryID, recordID, notificationID, recipientID, bindingID, epochs[0], epochs[1])
		requirePostgresSQLState(t, err, "55000")
	}
	// Holding the real epoch row is a deterministic barrier: the insert guard
	// must fail with a restartable error rather than wait while holding a later rank.
	holder, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	var lockedEpoch int64
	if err := holder.QueryRow(ctx, `select delivery_epoch from public.content_delivery_epochs
		where project_id='default' and object_kind='record' and object_id=$1 for update`, recordID).Scan(&lockedEpoch); err != nil || lockedEpoch != 4 {
		t.Fatalf("lock delivery insert epoch: epoch=%d error=%v", lockedEpoch, err)
	}
	_, err = runtime.Exec(ctx, pendingInsert, deliveryID, recordID, notificationID, recipientID, bindingID, int64(1), int64(4))
	requirePostgresSQLState(t, err, "40001")
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Match the production scheduler's pending INSERT and outbox enqueue in
	// the same runtime transaction, then exercise the resulting queue claim.
	tx, err := runtime.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `insert into public.record_notification_deliveries
		(delivery_id,record_id,notification_id,recipient_user_id,channel,binding_id,authorization_epoch,record_fence_epoch)
		values ($1,$2,$3,$4,'telegram',$5,1,4) on conflict (notification_id,recipient_user_id,channel) do nothing`,
		deliveryID, recordID, notificationID, recipientID, bindingID); err != nil {
		t.Fatalf("insert ordinary pending delivery: %v", err)
	}
	var pendingState string
	var pendingAttempts int
	if err := tx.QueryRow(ctx, `select delivery_state,attempt_count from public.record_notification_deliveries where delivery_id=$1`, deliveryID).Scan(&pendingState, &pendingAttempts); err != nil || pendingState != "pending" || pendingAttempts != 0 {
		t.Fatalf("ordinary pending delivery = (%q,%d), %v", pendingState, pendingAttempts, err)
	}
	enqueue := map[string]any{
		"project_id": "default", "event_kind": "record_notification_delivery", "subject_kind": "delivery", "subject_id": deliveryID,
		"source_version": int64(1), "authorization_epoch": int64(1), "record_fence_epoch": int64(4), "record_id": recordID,
		"expires_after_us": int64(time.Hour / time.Microsecond),
	}
	var rowID int64
	var eventExpiry time.Time
	if err := tx.QueryRow(ctx, `select outbox_row_id,expires_at from public.record_outbox_enqueue($1::bytea)`, deliveryFactsJSON(t, enqueue)).Scan(&rowID, &eventExpiry); err != nil {
		t.Fatalf("enqueue ordinary delivery: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit ordinary scheduling: %v", err)
	}
	claim := map[string]any{
		"outbox_row_id": rowID, "event_expires_at": deliveryFactsTimestamp(eventExpiry), "project_id": "default",
		"event_kind": "record_notification_delivery", "subject_kind": "delivery", "delivery_id": deliveryID,
		"record_id": recordID, "notification_id": notificationID, "recipient_user_id": recipientID,
		"channel": "telegram", "binding_id": bindingID, "source_version": int64(1),
		"authorization_epoch": int64(1), "record_fence_epoch": int64(4),
	}
	current := deliveryFactsClaim(t, ctx, owner, runtime, observer, claim, "insert_guard_owner")
	start := startDeliveryFacts(t, ctx, runtime, current, "pending", 0)
	digest := deliveryFactsReport(t, ctx, runtime, start, "sent")
	result := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(start, current, digest, 0))
	if result.disposition != "complete" || result.delayUS != 0 || result.nextAttemptAt != nil {
		t.Fatalf("ordinary scheduled delivery finalization = %#v", result)
	}
	var state, queue string
	if err := owner.QueryRow(ctx, `select d.delivery_state,o.status from public.record_notification_deliveries d
		join public.record_outbox o on o.outbox_row_id=$2 where d.delivery_id=$1`, deliveryID, rowID).Scan(&state, &queue); err != nil || state != "sent" || queue != "sent" {
		t.Fatalf("ordinary scheduled delivery = (%q,%q), %v", state, queue, err)
	}
}

// Normal application transactions leave constraints deferred. Hold each
// transaction after the command returns, wait for the database clock to pass
// the observed owner expiry, and verify that COMMIT rejects its projection.
func assertDeliveryFactsCommitExpiry(t *testing.T, parent context.Context, owner, runtime, observer *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, time.Minute)
	defer cancel()
	t.Run("start", func(t *testing.T) {
		claim := seedDeliveryFactsPending(t, ctx, owner, 60, "rec_delivery", false, 4*time.Second, time.Hour)
		command := deliveryFactsCopy(claim)
		command["expected_state"] = "pending"
		command["expected_attempt_count"] = int64(0)
		tx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var number int64
		var started time.Time
		var capability []byte
		if err := tx.QueryRow(ctx, `select attempt_no,started_at,reporting_capability from public.record_external_delivery_start($1::bytea)`, deliveryFactsJSON(t, command)).Scan(&number, &started, &capability); err != nil || number != 1 || len(capability) != 32 {
			t.Fatalf("start before owner expiry: attempt=%d capability_bytes=%d error=%v", number, len(capability), err)
		}
		expires, err := time.Parse(time.RFC3339Nano, claim["owner_expires_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		deliveryFactsWaitClock(t, ctx, observer, expires)
		requirePostgresSQLState(t, tx.Commit(ctx), "55000")
		var state, queue string
		var attempts, starts, count int
		if err := owner.QueryRow(ctx, `select d.delivery_state,d.attempt_count,o.status,
			(select count(*)::int from public.record_notification_delivery_starts where delivery_id=d.delivery_id),
			(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id)
			from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$2 where d.delivery_id=$1`,
			claim["delivery_id"], claim["outbox_row_id"]).Scan(&state, &attempts, &queue, &starts, &count); err != nil ||
			state != "pending" || attempts != 0 || queue != "processing" || starts != 0 || count != 0 {
			t.Fatalf("expired start committed effects: (%q,%d,%q,%d,%d), %v", state, attempts, queue, starts, count, err)
		}
		attempt := deliveryFactsCopy(claim)
		attempt["attempt_no"] = number
		attempt["started_at"] = deliveryFactsTimestamp(started)
		attempt["attempt_id"] = recordcollaboration.NotificationDeliveryAttemptID(claim["delivery_id"].(string), uint8(number))
		uncommitted := deliveryFactsStart{claim: claim, attempt: attempt, capability: capability}
		_, err = runtime.Exec(ctx, `select * from public.record_external_delivery_outcome($1::bytea)`, deliveryFactsJSON(t, deliveryFactsReportCommand(uncommitted, "sent")))
		requirePostgresSQLState(t, err, "55000")
	})
	t.Run("cancel", func(t *testing.T) {
		claim := seedDeliveryFactsPending(t, ctx, owner, 61, "rec_delivery", false, 4*time.Second, time.Hour)
		command := deliveryFactsCopy(claim)
		command["expected_state"] = "pending"
		command["expected_attempt_count"] = int64(0)
		command["reason"] = "authorization_revoked"
		tx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `select public.record_external_delivery_cancel($1::bytea)`, deliveryFactsJSON(t, command)); err != nil {
			t.Fatalf("cancel before owner expiry: %v", err)
		}
		expires, err := time.Parse(time.RFC3339Nano, claim["owner_expires_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		deliveryFactsWaitClock(t, ctx, observer, expires)
		requirePostgresSQLState(t, tx.Commit(ctx), "55000")
		var state, queue string
		var attempts, count int
		if err := owner.QueryRow(ctx, `select d.delivery_state,d.attempt_count,o.status,
			(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id)
			from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$2 where d.delivery_id=$1`,
			claim["delivery_id"], claim["outbox_row_id"]).Scan(&state, &attempts, &queue, &count); err != nil ||
			state != "pending" || attempts != 0 || queue != "processing" || count != 0 {
			t.Fatalf("expired cancellation committed effects: (%q,%d,%q,%d), %v", state, attempts, queue, count, err)
		}
	})
	t.Run("finalize_preserves_committed_fact", func(t *testing.T) {
		claim := seedDeliveryFactsPending(t, ctx, owner, 62, "rec_delivery", false, 4*time.Second, time.Hour)
		start := startDeliveryFacts(t, ctx, runtime, claim, "pending", 0)
		digest := deliveryFactsReport(t, ctx, runtime, start, "sent")
		tx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var disposition string
		var delay int64
		var next *time.Time
		if err := tx.QueryRow(ctx, `select disposition,retry_after_us,next_attempt_at from public.record_external_delivery_finalize($1::bytea)`,
			deliveryFactsJSON(t, deliveryFactsFinalizeCommand(start, claim, digest, 0))).Scan(&disposition, &delay, &next); err != nil || disposition != "complete" || delay != 0 || next != nil {
			t.Fatalf("finalize before owner expiry: (%q,%d,%v), %v", disposition, delay, next, err)
		}
		expires, err := time.Parse(time.RFC3339Nano, claim["owner_expires_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		deliveryFactsWaitClock(t, ctx, observer, expires)
		requirePostgresSQLState(t, tx.Commit(ctx), "55000")
		var state, queue string
		var facts, attempts, completions int
		if err := owner.QueryRow(ctx, `select d.delivery_state,o.status,
			(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$3 and fact_kind='provider_report' and outcome='sent'),
			(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
			(select count(*)::int from public.record_notification_delivery_completions where delivery_id=d.delivery_id)
			from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$2 where d.delivery_id=$1`,
			claim["delivery_id"], claim["outbox_row_id"], start.attempt["attempt_id"]).Scan(&state, &queue, &facts, &attempts, &completions); err != nil ||
			state != "processing" || queue != "processing" || facts != 1 || attempts != 0 || completions != 0 {
			t.Fatalf("expired finalizer committed effects or lost fact: (%q,%q,%d,%d,%d), %v", state, queue, facts, attempts, completions, err)
		}
		current := deliveryFactsClaim(t, ctx, owner, runtime, observer, claim, "commit_takeover")
		result := deliveryFactsReconcile(t, ctx, runtime, deliveryFactsReconcileCommand(start, current, 250000))
		if result.disposition != "complete" || result.delayUS != 0 || result.nextAttemptAt != nil {
			t.Fatalf("takeover failed to consume retained sent fact: %#v", result)
		}
		if err := owner.QueryRow(ctx, `select d.delivery_state,o.status,
			(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id)
			from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$2 where d.delivery_id=$1`,
			claim["delivery_id"], claim["outbox_row_id"]).Scan(&state, &queue, &attempts); err != nil || state != "sent" || queue != "sent" || attempts != 1 {
			t.Fatalf("takeover did not finalize retained fact: (%q,%q,%d), %v", state, queue, attempts, err)
		}
	})
	t.Run("retry_closure_owner_expires_before_commit", func(t *testing.T) {
		claim := seedDeliveryFactsPending(t, ctx, owner, 72, "rec_delivery", false, 10*time.Minute, time.Hour)
		start := startDeliveryFacts(t, ctx, runtime, claim, "pending", 0)
		temporary := deliveryFactsReport(t, ctx, runtime, start, "temporary_failure")
		finalized := deliveryFactsFinalize(t, ctx, runtime, deliveryFactsFinalizeCommand(start, claim, temporary, 1))
		if finalized.disposition != "retry" || finalized.delayUS != 1 || finalized.nextAttemptAt == nil {
			t.Fatalf("prepare retry before commit-expiry case: %#v", finalized)
		}
		deliveryFactsReport(t, ctx, runtime, start, "sent")
		var completionBefore string
		if err := owner.QueryRow(ctx, `select to_jsonb(c)::text from public.record_notification_delivery_completions c
			where c.delivery_id=$1 and c.attempt_no=1`, claim["delivery_id"]).Scan(&completionBefore); err != nil {
			t.Fatalf("read original retry completion: %v", err)
		}
		current := deliveryFactsClaimWithLease(t, ctx, owner, runtime, observer, claim, "retry_commit_expiry", 5*time.Second)
		command := deliveryFactsCopy(current)
		command["expected_state"] = "retry_wait"
		command["expected_attempt_count"] = int64(1)
		body := deliveryFactsJSON(t, command)
		tx, err := runtime.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		var number int64
		var started time.Time
		var capability []byte
		originalStarted, err := time.Parse(time.RFC3339Nano, start.attempt["started_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, `select attempt_no,started_at,reporting_capability from public.record_external_delivery_start($1::bytea)`, body).Scan(&number, &started, &capability); err != nil ||
			number != 1 || !started.Equal(originalStarted) || capability != nil {
			t.Fatalf("retry closure before owner expiry: attempt=%d started=%v capability_bytes=%d error=%v", number, started, len(capability), err)
		}
		expires, err := time.Parse(time.RFC3339Nano, current["owner_expires_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		deliveryFactsWaitClock(t, ctx, observer, expires)
		requirePostgresSQLState(t, tx.Commit(ctx), "55000")
		var state, queue, completionAfter string
		var starts, attempts, facts, completions int
		if err := owner.QueryRow(ctx, `select d.delivery_state,o.status,
			(select count(*)::int from public.record_notification_delivery_starts where delivery_id=d.delivery_id),
			(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id),
			(select count(*)::int from public.record_notification_delivery_facts where attempt_id=$3 and fact_kind='provider_report'),
			(select count(*)::int from public.record_notification_delivery_completions where delivery_id=d.delivery_id),
			(select to_jsonb(c)::text from public.record_notification_delivery_completions c where c.delivery_id=d.delivery_id and c.attempt_no=1)
			from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$2 where d.delivery_id=$1`,
			claim["delivery_id"], claim["outbox_row_id"], start.attempt["attempt_id"]).Scan(
			&state, &queue, &starts, &attempts, &facts, &completions, &completionAfter); err != nil ||
			state != "retry_wait" || queue != "processing" || starts != 1 || attempts != 1 || facts != 2 || completions != 1 || completionAfter != completionBefore {
			t.Fatalf("expired retry closure changed projection or evidence: state=%q queue=%q starts=%d attempts=%d facts=%d completions=%d completion_unchanged=%t error=%v",
				state, queue, starts, attempts, facts, completions, completionAfter == completionBefore, err)
		}
		takeover := deliveryFactsClaim(t, ctx, owner, runtime, observer, current, "retry_commit_takeover")
		command = deliveryFactsCopy(takeover)
		command["expected_state"] = "retry_wait"
		command["expected_attempt_count"] = int64(1)
		capability = nil
		if err := runtime.QueryRow(ctx, `select attempt_no,started_at,reporting_capability from public.record_external_delivery_start($1::bytea)`,
			deliveryFactsJSON(t, command)).Scan(&number, &started, &capability); err != nil || number != 1 || !started.Equal(originalStarted) || capability != nil {
			t.Fatalf("takeover after expired retry closure: attempt=%d started=%v capability_bytes=%d error=%v", number, started, len(capability), err)
		}
		if err := owner.QueryRow(ctx, `select d.delivery_state,o.status,
			(select count(*)::int from public.record_notification_delivery_starts where delivery_id=d.delivery_id),
			(select count(*)::int from public.record_notification_delivery_attempts where delivery_id=d.delivery_id)
			from public.record_notification_deliveries d join public.record_outbox o on o.outbox_row_id=$2 where d.delivery_id=$1`,
			claim["delivery_id"], claim["outbox_row_id"]).Scan(&state, &queue, &starts, &attempts); err != nil ||
			state != "unknown_outcome" || queue != "sent" || starts != 1 || attempts != 1 {
			t.Fatalf("takeover did not close conflicting retry: state=%q queue=%q starts=%d attempts=%d error=%v", state, queue, starts, attempts, err)
		}
	})
}
