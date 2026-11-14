package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceretirement"
)

func TestPostgresWitnessedRecordSubjectTombstoneSourceRejectsDigestOnlyLocalProjection(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "witnessed-subject-tombstone", 1)

	identity := testDeploymentMembershipIdentity()
	reader, err := NewWitnessedRecordSubjectTombstoneReader(
		recordplatform.DeploymentID(identity.DeploymentID),
		recordauth.ProjectID(identity.ProjectID),
		nil,
		pool,
	)
	if err != nil {
		t.Fatalf("NewWitnessedRecordSubjectTombstoneReader() error = %v", err)
	}
	_, err = reader.ResolveWitnessedRecordSubjectTombstone(
		ctx,
		recordauth.ProjectIDDefault,
		recordauth.SourceKindVPS,
		testStoreRecordVPSID,
	)
	if !errors.Is(err, ErrWitnessedRecordSubjectTombstoneNotFound) {
		t.Fatalf("clean unenrolled Resolve() error = %v, want tombstone not found", err)
	}

	// Seed legacy history through the bootstrap connection, not runtime DML.
	floor := mustStoreProjectVisibility(t)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.source_deletion_tombstones (
			project_id, source_kind, source_id, authorization_floor_digest
		) values ($1, $2, $3, $4)
	`, string(recordauth.ProjectIDDefault), string(recordauth.SourceKindVPS), testStoreRecordVPSID, floor.CanonicalHash[:]); err != nil {
		t.Fatalf("insert digest-only source_deletion_tombstones: %v", err)
	}

	_, err = reader.ResolveWitnessedRecordSubjectTombstone(
		ctx,
		recordauth.ProjectIDDefault,
		recordauth.SourceKindVPS,
		testStoreRecordVPSID,
	)
	if !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("digest-only Resolve() error = %v, want unavailable", err)
	}
}

func TestPostgresWitnessedRecordSubjectTombstoneSourcePreparedUnenrolledHistory(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "prepared-subject-history", 2)
	reader := mustWitnessedRecordSubjectTombstoneReader(t, pool)
	key := reader.sourceKey(recordauth.ProjectIDDefault, recordauth.SourceKindVPS, testStoreRecordVPSID)

	_, err := reader.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, key.ID)
	if !errors.Is(err, ErrWitnessedRecordSubjectTombstoneNotFound) {
		t.Fatalf("clean unenrolled source: %v, want proven live eligibility", err)
	}
	prepared, err := reader.Prefetch(ctx, []sourceretirement.SourceKey{key, key})
	if err != nil {
		t.Fatalf("prefetch clean unenrolled source: %v", err)
	}
	if len(prepared.observations) != 1 || prepared.observations[key].external {
		t.Fatal("prefetch must retain one explicit no-external observation")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("begin APP transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	checker := prepared.ForTransaction(tx)
	_, err = checker.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, key.ID)
	if !errors.Is(err, ErrWitnessedRecordSubjectTombstoneNotFound) {
		t.Fatalf("prepared clean unenrolled source: %v", err)
	}
	_, err = checker.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, "vps_ffffffffffffffff")
	if !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("unprepared key: %v, want unavailable", err)
	}

	// Commit legacy history independently after the runtime transaction's first read.
	floor := mustStoreProjectVisibility(t)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.source_deletion_tombstones (
			project_id, source_kind, source_id, authorization_floor_digest
		) values ($1, $2, $3, $4)
	`, string(key.ProjectID), string(key.Kind), key.ID, floor.CanonicalHash[:]); err != nil {
		t.Fatalf("insert legacy history after prefetch: %v", err)
	}
	_, err = checker.ResolveWitnessedRecordSubjectTombstone(ctx, key.ProjectID, key.Kind, key.ID)
	if !errors.Is(err, ErrRecordSubjectUnavailable) {
		t.Fatalf("fresh transaction history must override prefetched absence: %v", err)
	}
}
