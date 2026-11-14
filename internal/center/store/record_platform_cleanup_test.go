package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationOutboxCleanupRetainsUnresolvedEffects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	repository := NewPostgresRecordPlatformRepository(runtime, gate)
	recordIDs := make(map[string]string, 4)
	for _, state := range []string{"sent", "cancelled", "processing", "pending"} {
		recordID := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "retention-"+state).RecordID
		recordIDs[state] = recordID
		if state == "pending" {
			continue
		}
		claim, err := repository.ClaimOutbox(ctx, recordplatform.OutboxClaimInputV1{
			OwnerID: "retention_worker", OwnerLeaseDuration: time.Minute,
		})
		if err != nil || claim == nil || claim.Event.SubjectID != recordID {
			t.Fatalf("claim %s: %#v, %v", state, claim, err)
		}
		switch state {
		case "sent":
			err = repository.MarkOutboxSent(ctx, *claim)
		case "cancelled":
			err = repository.CancelOutbox(ctx, *claim)
		}
		if err != nil {
			t.Fatalf("finalize %s: %v", state, err)
		}
	}
	// Age all rows and the in-flight owner without changing their actual state.
	if _, err := fixture.db.Exec(ctx, `update public.record_outbox
		set created_at=clock_timestamp()-interval '3 minutes',
		    expires_at=clock_timestamp()-interval '1 minute',
		    owner_expires_at=case when owner_expires_at is not null
		      then clock_timestamp()-interval '2 minutes' end`); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"pending", "processing"} {
		recordID := recordIDs[state]
		var rowID int64
		if err := runtime.QueryRow(ctx, `select outbox_row_id from public.record_outbox where subject_id=$1`, recordID).Scan(&rowID); err != nil {
			t.Fatal(err)
		}
		tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		candidate, err := readOutboxCandidate(ctx, tx, rowID, false)
		if rollbackErr := tx.Rollback(ctx); err == nil {
			err = rollbackErr
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := outboxCandidateCommand(candidate, recordID, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		var deletedID int64
		err = runtime.QueryRow(ctx, `select outbox_row_id from public.record_outbox_cleanup($1::bytea)`, body).Scan(&deletedID)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("direct cleanup of expired %s = %d, %v; want no deletion", state, deletedID, err)
		}
	}
	result, err := repository.CleanupExpiredRecordPlatformPrimitives(ctx)
	if err != nil || result.OutboxEvents != 2 {
		t.Fatalf("cleanup terminal rows = %#v, %v", result, err)
	}
	rows, err := runtime.Query(ctx, `select subject_id,status,owner_id,owner_generation
		from public.record_outbox order by subject_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := make(map[string]bool)
	for rows.Next() {
		var recordID, state, owner string
		var generation int64
		if err := rows.Scan(&recordID, &state, &owner, &generation); err != nil {
			t.Fatal(err)
		}
		switch recordID {
		case recordIDs["pending"]:
			if state != "pending" || owner != "" || generation != 0 {
				t.Fatalf("pending responsibility changed: %s %s %d", state, owner, generation)
			}
		case recordIDs["processing"]:
			if state != "processing" || owner != "retention_worker" || generation != 1 {
				t.Fatalf("in-flight responsibility changed: %s %s %d", state, owner, generation)
			}
		default:
			t.Fatalf("expired terminal row retained: %s", recordID)
		}
		seen[recordID] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !seen[recordIDs["pending"]] || !seen[recordIDs["processing"]] {
		t.Fatalf("cleanup erased unresolved responsibilities: %v", seen)
	}
}
