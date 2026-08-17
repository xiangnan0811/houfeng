package migrate

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func assertRecordCollaborationConcurrentRedaction(
	t *testing.T,
	ctx context.Context,
	fixture *appACLConvergencePostgresFixture,
	runtimeDB *pgxpool.Pool,
) {
	t.Helper()

	t.Run("redaction wins parent lock", func(t *testing.T) {
		const (
			commentID   = "rcm_concurrentredact"
			revisionID  = "rcr_concurrentredact"
			tombstoneID = "rct_concurrentredact"
		)
		prepareRecordCollaborationRedactionRace(t, ctx, fixture, runtimeDB, commentID, revisionID, tombstoneID)
		peerDB := fixture.openDirectRolePool(t, ctx, fixture.runtime)
		raceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		redactionTx, err := runtimeDB.Begin(raceCtx)
		if err != nil {
			t.Fatalf("begin winning comment redaction transaction: %v", err)
		}
		defer func() { _ = redactionTx.Rollback(ctx) }()
		if _, err := redactionTx.Exec(raceCtx, redactCollaborationRaceCommentSQL, commentID, tombstoneID); err != nil {
			t.Fatalf("stage winning comment redaction: %v", err)
		}

		insertTx, err := peerDB.Begin(raceCtx)
		if err != nil {
			t.Fatalf("begin blocked revision insert transaction: %v", err)
		}
		defer func() { _ = insertTx.Rollback(ctx) }()
		insertPID := postgresTransactionBackendPID(t, raceCtx, insertTx)
		insertResult := make(chan error, 1)
		go func() {
			_, execErr := insertTx.Exec(raceCtx, insertCollaborationRaceRevisionSQL,
				"rcr_concurrentredactnew", commentID, int64(2))
			insertResult <- execErr
		}()

		requirePostgresBackendBlocked(t, raceCtx, fixture.db, insertPID)
		if err := redactionTx.Commit(raceCtx); err != nil {
			t.Fatalf("commit winning comment redaction: %v", err)
		}
		requirePostgresSQLState(t, awaitPostgresConflictResult(t, raceCtx, insertResult), "55000")
		assertRecordCollaborationRaceInvariant(t, ctx, fixture, commentID, "redacted", 0)
	})

	t.Run("revision insert wins parent lock", func(t *testing.T) {
		const (
			commentID   = "rcm_concurrentinsert"
			revisionID  = "rcr_concurrentinsert"
			tombstoneID = "rct_concurrentinsert"
		)
		prepareRecordCollaborationRedactionRace(t, ctx, fixture, runtimeDB, commentID, revisionID, tombstoneID)
		peerDB := fixture.openDirectRolePool(t, ctx, fixture.runtime)
		raceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		insertTx, err := runtimeDB.Begin(raceCtx)
		if err != nil {
			t.Fatalf("begin winning revision insert transaction: %v", err)
		}
		defer func() { _ = insertTx.Rollback(ctx) }()
		if _, err := insertTx.Exec(raceCtx, insertCollaborationRaceRevisionSQL,
			"rcr_concurrentinsertnew", commentID, int64(2)); err != nil {
			t.Fatalf("stage winning revision insert: %v", err)
		}

		redactionTx, err := peerDB.Begin(raceCtx)
		if err != nil {
			t.Fatalf("begin blocked comment redaction transaction: %v", err)
		}
		defer func() { _ = redactionTx.Rollback(ctx) }()
		redactionPID := postgresTransactionBackendPID(t, raceCtx, redactionTx)
		redactionResult := make(chan error, 1)
		go func() {
			_, execErr := redactionTx.Exec(raceCtx, redactCollaborationRaceCommentSQL, commentID, tombstoneID)
			redactionResult <- execErr
		}()

		requirePostgresBackendBlocked(t, raceCtx, fixture.db, redactionPID)
		if err := insertTx.Commit(raceCtx); err != nil {
			t.Fatalf("commit winning revision insert: %v", err)
		}
		requirePostgresSQLState(t, awaitPostgresConflictResult(t, raceCtx, redactionResult), "55000")
		assertRecordCollaborationRaceInvariant(t, ctx, fixture, commentID, "active", 1)
	})
}

const redactCollaborationRaceCommentSQL = `
	update public.record_comments
	set comment_state = 'redacted', comment_version = 2, body_markdown = null,
		render_contract_version = null, render_model = null, body_digest = null,
		tombstone_id = $2,
		redacted_at = (select deleted_at from public.record_comment_tombstones where tombstone_id = $2),
		updated_at = clock_timestamp()
	where comment_id = $1
`

const insertCollaborationRaceRevisionSQL = `
	insert into public.record_comment_revisions (
		comment_revision_id, record_id, comment_id, comment_version, edited_by,
		body_markdown, render_contract_version, render_model, body_digest,
		record_fence_epoch
	) values ($1, 'rec_acl', $2, $3, 'usr_acl', 'concurrent content',
		'comment_markdown/v1', '{"type":"paragraph"}'::jsonb,
		decode(repeat('56', 32), 'hex'), 0)
`

func prepareRecordCollaborationRedactionRace(
	t *testing.T,
	ctx context.Context,
	fixture *appACLConvergencePostgresFixture,
	runtimeDB *pgxpool.Pool,
	commentID string,
	revisionID string,
	tombstoneID string,
) {
	t.Helper()

	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, statement := range []string{
			`delete from public.record_comment_revisions where comment_id = $1`,
			`delete from public.record_comment_tombstones where comment_id = $1`,
			`delete from public.record_comments where comment_id = $1`,
		} {
			if _, err := migratorDB.Exec(cleanupCtx, statement, commentID); err != nil {
				t.Errorf("clean concurrent collaboration fixture with %q: %v", statement, err)
			}
		}
	})

	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_comments (
			comment_id, record_id, author_id, comment_version, body_markdown,
			render_contract_version, render_model, body_digest, record_fence_epoch
		) values ($1, 'rec_acl', 'usr_acl', 1, 'initial content',
			'comment_markdown/v1', '{"type":"paragraph"}'::jsonb,
			decode(repeat('55', 32), 'hex'), 0)
	`, commentID); err != nil {
		t.Fatalf("insert concurrent collaboration comment fixture: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_comment_revisions (
			comment_revision_id, record_id, comment_id, comment_version, edited_by,
			body_markdown, render_contract_version, render_model, body_digest,
			record_fence_epoch
		) values ($1, 'rec_acl', $2, 1, 'usr_acl', 'initial content',
			'comment_markdown/v1', '{"type":"paragraph"}'::jsonb,
			decode(repeat('55', 32), 'hex'), 0)
	`, revisionID, commentID); err != nil {
		t.Fatalf("insert concurrent collaboration revision fixture: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_comment_tombstones (
			tombstone_id, record_id, comment_id, tombstone_version, deleted_by,
			reason_code, deleted_at, record_fence_epoch
		) values ($1, 'rec_acl', $2, 2, 'usr_acl', 'author_deleted', clock_timestamp(), 0)
	`, tombstoneID, commentID); err != nil {
		t.Fatalf("insert concurrent collaboration tombstone fixture: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		update public.record_comment_revisions
		set body_markdown = null, render_contract_version = null, render_model = null,
			body_digest = null, tombstone_id = $2,
			redacted_at = (select deleted_at from public.record_comment_tombstones where tombstone_id = $2)
		where comment_revision_id = $1
	`, revisionID, tombstoneID); err != nil {
		t.Fatalf("redact concurrent collaboration revision fixture: %v", err)
	}
}

func postgresTransactionBackendPID(t *testing.T, ctx context.Context, tx pgx.Tx) int32 {
	t.Helper()
	var pid int32
	if err := tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read PostgreSQL transaction backend PID: %v", err)
	}
	return pid
}

func requirePostgresBackendBlocked(
	t *testing.T,
	ctx context.Context,
	observer *pgxpool.Pool,
	pid int32,
) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := observer.QueryRow(ctx, `select cardinality(pg_blocking_pids($1)) > 0`, pid).Scan(&blocked); err != nil {
			t.Fatalf("observe PostgreSQL backend %d blocking state: %v", pid, err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("PostgreSQL backend %d was not observed blocked: %v", pid, ctx.Err())
		case <-ticker.C:
		}
	}
}

func awaitPostgresConflictResult(t *testing.T, ctx context.Context, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		t.Fatalf("timed out awaiting concurrent PostgreSQL statement: %v", ctx.Err())
		return ctx.Err()
	}
}

func assertRecordCollaborationRaceInvariant(
	t *testing.T,
	ctx context.Context,
	fixture *appACLConvergencePostgresFixture,
	commentID string,
	wantState string,
	wantActiveRevisions int,
) {
	t.Helper()
	var (
		state           string
		parentHasBody   bool
		activeRevisions int
	)
	if err := fixture.db.QueryRow(ctx, `
		select comment.comment_state,
			comment.body_markdown is not null,
			count(revision.comment_revision_id) filter (where revision.redacted_at is null)::int
		from public.record_comments as comment
		left join public.record_comment_revisions as revision
			on revision.record_id = comment.record_id and revision.comment_id = comment.comment_id
		where comment.comment_id = $1
		group by comment.comment_state, comment.body_markdown
	`, commentID).Scan(&state, &parentHasBody, &activeRevisions); err != nil {
		t.Fatalf("read concurrent collaboration invariant: %v", err)
	}
	if state != wantState || activeRevisions != wantActiveRevisions {
		t.Fatalf("concurrent collaboration state/active revisions = %q/%d, want %q/%d",
			state, activeRevisions, wantState, wantActiveRevisions)
	}
	if parentHasBody != (wantState == "active") {
		t.Fatalf("concurrent collaboration parent body present = %t for state %q", parentHasBody, state)
	}
}
