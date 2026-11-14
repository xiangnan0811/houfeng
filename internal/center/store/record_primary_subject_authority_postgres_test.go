package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recorddeletion"
)

func TestPostgresIntegrationPrimarySubjectDeferredAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixture, gate, runtime := newAdmittedOutboxFixture(t, ctx)
	created := seedAdmittedOutboxRecord(t, ctx, runtime, gate, "primary-subject-authority")

	begin := func() pgx.Tx {
		t.Helper()
		tx, err := runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		if err := gate.Admit(ctx, tx); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		return tx
	}
	insertRevision := func(tx pgx.Tx, revisionID string, number int) {
		t.Helper()
		_, err := tx.Exec(ctx, `insert into public.record_revisions
			(revision_id,record_id,revision_no,title,body_markdown,markdown_dialect_version,record_type,impact_level,visibility_scope,visibility_digest,author_id,canonical_hash)
			values ($1,$2,$3,'subject test','',1,'note','informational','{}'::jsonb,decode(repeat('41',32),'hex'),'usr_subjecttest',decode(repeat('42',32),'hex'))`, revisionID, created.RecordID, number)
		if err != nil {
			t.Fatalf("insert revision before deferred check: %v", err)
		}
	}
	insertSubject := func(tx pgx.Tx, revisionID string, ordinal int, source string) error {
		_, err := tx.Exec(ctx, `insert into public.record_revision_subjects
			(revision_id,ordinal,registry_version,subject_kind,relation_role,source_id,is_primary,identity_snapshot,capture_authorization,capture_authorization_digest)
			values ($1,$2,1,'vps','affected',$3,true,'{}'::jsonb,'{}'::jsonb,decode(repeat('43',32),'hex'))`, revisionID, ordinal, source)
		return err
	}
	requireState := func(err error, state string) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != state {
			t.Fatalf("SQL error = %v, want SQLSTATE %s", err, state)
		}
	}

	t.Run("valid revision and subject commit", func(t *testing.T) {
		tx := begin()
		defer func() { _ = tx.Rollback(ctx) }()
		insertRevision(tx, "rrv_subjectauthorityvalid", 2)
		if err := insertSubject(tx, "rrv_subjectauthorityvalid", 0, "vps_subjectvalid"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit revision and primary: %v", err)
		}
	})
	t.Run("missing primary rejected at commit", func(t *testing.T) {
		tx := begin()
		defer func() { _ = tx.Rollback(ctx) }()
		insertRevision(tx, "rrv_subjectauthoritymissing", 3)
		requireState(tx.Commit(ctx), "23514")
	})
	t.Run("delete sole primary rejected at commit", func(t *testing.T) {
		tx := begin()
		defer func() { _ = tx.Rollback(ctx) }()
		tag, err := tx.Exec(ctx, `delete from public.record_revision_subjects where revision_id=$1 and is_primary`, "rrv_subjectauthorityvalid")
		if err != nil || tag.RowsAffected() != 1 {
			t.Fatalf("delete primary: rows=%d error=%v", tag.RowsAffected(), err)
		}
		requireState(tx.Commit(ctx), "23514")
	})
	t.Run("two primaries rejected", func(t *testing.T) {
		tx := begin()
		defer func() { _ = tx.Rollback(ctx) }()
		err := insertSubject(tx, "rrv_subjectauthorityvalid", 1, "vps_subjectother")
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "uq_record_revision_subjects_primary" {
			t.Fatalf("second primary error = %v", err)
		}
	})
	for _, role := range []struct{ name, role string }{{"admin", fixture.admin}, {"worker", fixture.deletionWorker}} {
		t.Run(role.name+" cannot read content or execute validator", func(t *testing.T) {
			pool := fixture.openDirectRolePool(t, ctx, role.role, "primary-subject-"+role.name, 1)
			for _, statement := range []string{
				`select record_id from public.records limit 1`,
				`select revision_id from public.record_revisions limit 1`,
				`select revision_id from public.record_revision_subjects limit 1`,
				`select record_platform_internal.validate_record_revision_primary_subject()`,
			} {
				_, err := pool.Exec(ctx, statement)
				requireState(err, "42501")
			}
		})
	}
	var primaryCount int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_revision_subjects where revision_id=$1 and is_primary`, "rrv_subjectauthorityvalid").Scan(&primaryCount); err != nil || primaryCount != 1 {
		t.Fatalf("primary after failed transactions = %d, error %v", primaryCount, err)
	}
}

func TestPostgresIntegrationPrimarySubjectRestrictedRecoveryPurge(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	legacy, created := newRecordsPostgresLegacyDeletionFixture(t, ctx, "rec_primaryrecover", "Primary recovery", "primary-recovery-create")
	fixture := legacy.platform
	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	ledger := newRecordsPostgresRecoveryLedger(t, ctx, legacy)
	cursor := ledger.prior(t, ctx, deploymentID)
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry, _ := ledger.record(t, ctx, recordsPostgresRecoveryRequest(t, created, "rpo_primaryrecover", deploymentID, recorddeletion.LedgerEntryDeleteCommit, 0))
	receipt, err := ledger.adapter.Replay(ctx, ledger.replayRequest(cursor, entry))
	if err != nil || !receipt.ContentPurged {
		t.Fatalf("restricted verified recovery purge = %#v, error %v", receipt, err)
	}
	var roots, revisions, subjects int
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.records where record_id=$1),
		(select count(*) from public.record_revisions where record_id=$1),
		(select count(*) from public.record_revision_subjects where revision_id=$2)`, created.RecordID, created.RevisionID).Scan(&roots, &revisions, &subjects); err != nil {
		t.Fatal(err)
	}
	if roots != 0 || revisions != 0 || subjects != 0 {
		t.Fatalf("purge left root/revision/subject = %d/%d/%d", roots, revisions, subjects)
	}
}
