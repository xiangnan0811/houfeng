package migrate

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func assertRecordCollaborationAppACLCurrentRolePrivileges(
	t *testing.T,
	ctx context.Context,
	fixture *appACLConvergencePostgresFixture,
	runtimeDB *pgxpool.Pool,
) {
	t.Helper()
	const redactCommentSQL = `
		update public.record_comments
		set comment_state = 'redacted', comment_version = 2, body_markdown = null,
			render_contract_version = null, render_model = null, body_digest = null,
			tombstone_id = 'rct_acl',
			redacted_at = (select deleted_at from public.record_comment_tombstones where tombstone_id = 'rct_acl'),
			updated_at = clock_timestamp()
		where comment_id = 'rcm_acl'
	`

	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_actions (
			action_id, record_id, subject_revision_id, action_version, title,
			created_by, updated_by, record_fence_epoch
		) values ('ract_acl', 'rec_acl', 'rrv_acl', 1, 'Verify collaboration',
			'usr_acl', 'usr_acl', 0)
	`); err != nil {
		t.Fatalf("runtime insert collaboration action: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_action_events (
			action_event_id, record_id, action_id, action_version, event_kind,
			previous_status, current_status, actor_id, record_fence_epoch, occurred_at
		) values ('raev_acl', 'rec_acl', 'ract_acl', 1, 'created', null, 'open',
			'usr_acl', 0, clock_timestamp())
	`); err != nil {
		t.Fatalf("runtime insert collaboration action event: %v", err)
	}
	_, err := runtimeDB.Exec(ctx, `
		insert into public.record_action_events (
			action_event_id, record_id, action_id, action_version, event_kind,
			previous_status, current_status, actor_id, record_fence_epoch, occurred_at
		) values ('raev_invalid', 'rec_acl', 'ract_acl', 2, 'updated', 'open',
			'completed', 'usr_acl', 0, clock_timestamp())
	`)
	requirePostgresSQLState(t, err, "23514")
	t.Run("non-created action event rejects null previous status", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin null previous-status action event transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_action_events (
				action_event_id, record_id, action_id, action_version, event_kind,
				previous_status, current_status, actor_id, record_fence_epoch, occurred_at
			) values ('raev_nullprevious', 'rec_acl', 'ract_acl', 2, 'completed', null,
				'completed', 'usr_acl', 0, clock_timestamp())
		`)
		requirePostgresSQLState(t, err, "23514")
	})

	t.Run("active comment rejects null render contract", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin null render-contract comment transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_comments (
				comment_id, record_id, author_id, comment_version, body_markdown,
				render_contract_version, render_model, body_digest, record_fence_epoch
			) values ('rcm_nullrender', 'rec_acl', 'usr_acl', 1, 'unsafe shape', null,
				'{"type":"paragraph"}'::jsonb, decode(repeat('50', 32), 'hex'), 0)
		`)
		requirePostgresSQLState(t, err, "23514")
	})

	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_comments (
			comment_id, record_id, author_id, comment_version, body_markdown,
			render_contract_version, render_model, body_digest, record_fence_epoch
		) values ('rcm_acl', 'rec_acl', 'usr_acl', 1, 'safe',
			'comment_markdown/v1', '{"type":"paragraph"}'::jsonb,
			decode(repeat('51', 32), 'hex'), 0)
	`); err != nil {
		t.Fatalf("runtime insert collaboration comment: %v", err)
	}
	t.Run("active comment revision rejects null render contract", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin null render-contract comment revision transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_comment_revisions (
				comment_revision_id, record_id, comment_id, comment_version, edited_by,
				body_markdown, render_contract_version, render_model, body_digest,
				record_fence_epoch
			) values ('rcr_nullrender', 'rec_acl', 'rcm_acl', 1, 'usr_acl', 'unsafe shape',
				null, '{"type":"paragraph"}'::jsonb, decode(repeat('50', 32), 'hex'), 0)
		`)
		requirePostgresSQLState(t, err, "23514")
	})
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_comment_revisions (
			comment_revision_id, record_id, comment_id, comment_version, edited_by,
			body_markdown, render_contract_version, render_model, body_digest,
			record_fence_epoch
		) values ('rcr_acl', 'rec_acl', 'rcm_acl', 1, 'usr_acl', 'safe',
			'comment_markdown/v1', '{"type":"paragraph"}'::jsonb,
			decode(repeat('51', 32), 'hex'), 0)
	`); err != nil {
		t.Fatalf("runtime insert collaboration comment revision: %v", err)
	}
	_, err = runtimeDB.Exec(ctx, `
		update public.record_comments
		set body_markdown = 'stale edit', updated_at = clock_timestamp()
		where comment_id = 'rcm_acl'
	`)
	requirePostgresSQLState(t, err, "55000")

	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_comment_tombstones (
			tombstone_id, record_id, comment_id, tombstone_version, deleted_by,
			reason_code, deleted_at, record_fence_epoch
		) values ('rct_acl', 'rec_acl', 'rcm_acl', 2, 'usr_acl',
			'author_deleted', clock_timestamp(), 0)
	`); err != nil {
		t.Fatalf("runtime insert collaboration comment tombstone: %v", err)
	}
	t.Run("parent redaction rejects unredacted history", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin parent-redaction bypass transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, redactCommentSQL)
		requirePostgresSQLState(t, err, "55000")
	})
	if _, err := runtimeDB.Exec(ctx, `
		update public.record_comment_revisions
		set body_markdown = null, render_contract_version = null, render_model = null,
			body_digest = null, tombstone_id = 'rct_acl',
			redacted_at = (select deleted_at from public.record_comment_tombstones where tombstone_id = 'rct_acl')
		where comment_revision_id = 'rcr_acl'
	`); err != nil {
		t.Fatalf("runtime redact collaboration comment revision: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, redactCommentSQL); err != nil {
		t.Fatalf("runtime redact collaboration comment: %v", err)
	}
	t.Run("redacted parent rejects new active revision", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin post-redaction revision bypass transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_comment_revisions (
				comment_revision_id, record_id, comment_id, comment_version, edited_by,
				body_markdown, render_contract_version, render_model, body_digest,
				record_fence_epoch
			) values ('rcr_afterredaction', 'rec_acl', 'rcm_acl', 2, 'usr_acl', 'restored',
				'comment_markdown/v1', '{"type":"paragraph"}'::jsonb,
				decode(repeat('53', 32), 'hex'), 0)
		`)
		requirePostgresSQLState(t, err, "55000")
	})
	t.Run("runtime cannot delete and reinsert redacted history", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin delete-reinsert bypass transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, `delete from public.record_comment_revisions where comment_revision_id = 'rcr_acl'`); err != nil {
			requirePostgresSQLState(t, err, "42501")
			return
		}
		_, err = tx.Exec(ctx, `
			insert into public.record_comment_revisions (
				comment_revision_id, record_id, comment_id, comment_version, edited_by,
				body_markdown, render_contract_version, render_model, body_digest,
				record_fence_epoch
			) values ('rcr_acl', 'rec_acl', 'rcm_acl', 1, 'usr_acl', 'restored',
				'comment_markdown/v1', '{"type":"paragraph"}'::jsonb,
				decode(repeat('54', 32), 'hex'), 0)
		`)
		if err == nil {
			t.Fatal("runtime DELETE plus INSERT restored redacted revision content")
		}
		t.Fatalf("runtime DELETE was allowed before replacement failed: %v", err)
	})
	_, err = runtimeDB.Exec(ctx, `
		update public.record_comments
		set comment_state = 'active', comment_version = 3, body_markdown = 'restore',
			render_contract_version = 'comment_markdown/v1', render_model = '{"type":"paragraph"}'::jsonb,
			body_digest = decode(repeat('52', 32), 'hex'), tombstone_id = null,
			redacted_at = null, updated_at = clock_timestamp()
		where comment_id = 'rcm_acl'
	`)
	requirePostgresSQLState(t, err, "55000")

	_, err = runtimeDB.Exec(ctx, `
		insert into public.record_followers (
			record_id, user_id, follower_version, manual_preference, record_fence_epoch
		) values ('rec_acl', 'usr_acl', 1, 'watching', -1)
	`)
	requirePostgresSQLState(t, err, "23514")
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_followers (
			record_id, user_id, follower_version, manual_preference, record_fence_epoch
		) values ('rec_acl', 'usr_acl', 1, 'watching', 0)
	`); err != nil {
		t.Fatalf("runtime insert collaboration follower: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `insert into public.records (record_id) values ('rec_aclother')`); err != nil {
		t.Fatalf("runtime insert collaboration cross-record root: %v", err)
	}
	t.Run("mention rejects cross-record and cross-fence revision", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin cross-record mention transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_comment_mentions (
				record_id, comment_id, comment_version, mentioned_user_id,
				record_fence_epoch
			) values ('rec_aclother', 'rcm_acl', 1, 'usr_recipient', 1)
		`)
		requirePostgresSQLState(t, err, "23503")
	})
	t.Run("mention rejects same-record wrong-fence revision", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin wrong-fence mention transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_comment_mentions (
				record_id, comment_id, comment_version, mentioned_user_id,
				record_fence_epoch
			) values ('rec_acl', 'rcm_acl', 1, 'usr_other', 1)
		`)
		requirePostgresSQLState(t, err, "23503")
	})

	assertRecordCollaborationConcurrentRedaction(t, ctx, fixture, runtimeDB)

	_, err = runtimeDB.Exec(ctx, `
		insert into public.record_notifications (
			notification_id, record_id, event_kind, subject_kind, subject_id,
			source_version, actor_id, authorization_epoch, record_fence_epoch,
			event_at, created_at, details_delete_after
		) values ('rnt_0000000000000000000000000000000000000000000000000000000000000000', 'rec_acl', 'comment_mentioned', 'comment', 'rcm_acl',
			2, 'usr_acl', 0, 0, clock_timestamp(), statement_timestamp(), statement_timestamp())
	`)
	requirePostgresSQLState(t, err, "23514")
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_notifications (
			notification_id, record_id, event_kind, subject_kind, subject_id,
			source_version, actor_id, authorization_epoch, record_fence_epoch,
			event_at, details_delete_after
		) values ('rnt_0000000000000000000000000000000000000000000000000000000000000001', 'rec_acl', 'comment_mentioned', 'comment', 'rcm_acl',
			2, 'usr_acl', 0, 0, clock_timestamp(), clock_timestamp() + interval '1 hour')
	`); err != nil {
		t.Fatalf("runtime insert collaboration notification: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_notification_recipients (
			notification_id, record_id, recipient_user_id, reason_kind, mandatory,
			authorization_epoch, record_fence_epoch
		) values ('rnt_0000000000000000000000000000000000000000000000000000000000000001', 'rec_acl', 'usr_recipient', 'mention', true, 0, 0)
	`); err != nil {
		t.Fatalf("runtime insert collaboration notification recipient: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_notification_deliveries (
			delivery_id, record_id, notification_id, recipient_user_id, channel,
			binding_id, authorization_epoch, record_fence_epoch
		) values ('rnd_acl', 'rec_acl', 'rnt_0000000000000000000000000000000000000000000000000000000000000001', 'usr_recipient', 'telegram',
			'binding_acl', 0, 0)
	`); err != nil {
		t.Fatalf("runtime insert collaboration notification delivery: %v", err)
	}
	t.Run("delivery rejects cross-record and cross-fence recipient", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin cross-record delivery transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_notification_deliveries (
				delivery_id, record_id, notification_id, recipient_user_id, channel,
				binding_id, authorization_epoch, record_fence_epoch
			) values ('rnd_cross', 'rec_aclother', 'rnt_0000000000000000000000000000000000000000000000000000000000000001', 'usr_recipient', 'feishu',
				'binding_cross', 0, 1)
		`)
		requirePostgresSQLState(t, err, "23503")
	})
	t.Run("delivery rejects same-record wrong-fence recipient", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin wrong-fence delivery transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_notification_deliveries (
				delivery_id, record_id, notification_id, recipient_user_id, channel,
				binding_id, authorization_epoch, record_fence_epoch
			) values ('rnd_wrongfence', 'rec_acl', 'rnt_0000000000000000000000000000000000000000000000000000000000000001', 'usr_recipient', 'feishu',
				'binding_wrongfence', 0, 1)
		`)
		requirePostgresSQLState(t, err, "23503")
	})
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_notifications (
			notification_id, record_id, event_kind, subject_kind, subject_id,
			source_version, actor_id, authorization_epoch, record_fence_epoch,
			event_at, details_delete_after
		) values ('rnt_0000000000000000000000000000000000000000000000000000000000000002', 'rec_acl', 'comment_mentioned', 'comment', 'rcm_acl',
			3, 'usr_acl', 0, 0, clock_timestamp(), clock_timestamp() + interval '1 hour')
	`); err != nil {
		t.Fatalf("runtime insert second collaboration notification: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_notification_recipients (
			notification_id, record_id, recipient_user_id, reason_kind, mandatory,
			authorization_epoch, record_fence_epoch
		) values ('rnt_0000000000000000000000000000000000000000000000000000000000000002', 'rec_acl', 'usr_other', 'mention', true, 0, 0)
	`); err != nil {
		t.Fatalf("runtime insert second collaboration notification recipient: %v", err)
	}
	t.Run("attempt rejects tuple from another notification recipient", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin cross-delivery attempt transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_notification_delivery_attempts (
				attempt_id, record_id, delivery_id, notification_id, recipient_user_id,
				attempt_no, outcome, authorization_epoch, record_fence_epoch,
				started_at, completed_at
			) values ('rna_cross', 'rec_acl', 'rnd_acl', 'rnt_0000000000000000000000000000000000000000000000000000000000000002', 'usr_other',
				2, 'temporary_failure', 0, 0, statement_timestamp(), statement_timestamp())
		`)
		requirePostgresSQLState(t, err, "23503")
	})
	t.Run("attempt rejects exact delivery tuple with wrong fence", func(t *testing.T) {
		tx, err := runtimeDB.Begin(ctx)
		if err != nil {
			t.Fatalf("begin wrong-fence delivery attempt transaction: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, `
			insert into public.record_notification_delivery_attempts (
				attempt_id, record_id, delivery_id, notification_id, recipient_user_id,
				attempt_no, outcome, authorization_epoch, record_fence_epoch,
				started_at, completed_at
			) values ('rna_wrongfence', 'rec_acl', 'rnd_acl', 'rnt_0000000000000000000000000000000000000000000000000000000000000001', 'usr_recipient',
				2, 'temporary_failure', 0, 1, statement_timestamp(), statement_timestamp())
		`)
		requirePostgresSQLState(t, err, "23503")
	})
	_, err = runtimeDB.Exec(ctx, `
		insert into public.record_notification_delivery_attempts (
			attempt_id, record_id, delivery_id, notification_id, recipient_user_id,
			attempt_no, outcome, authorization_epoch, record_fence_epoch,
			started_at, completed_at
		) values ('rna_invalid', 'rec_acl', 'rnd_acl', 'rnt_0000000000000000000000000000000000000000000000000000000000000001', 'usr_recipient',
			9, 'temporary_failure', 0, 0, clock_timestamp(), clock_timestamp())
	`)
	requirePostgresSQLState(t, err, "23514")
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_notification_delivery_attempts (
			attempt_id, record_id, delivery_id, notification_id, recipient_user_id,
			attempt_no, outcome, authorization_epoch, record_fence_epoch,
			started_at, completed_at
		) values ('rna_acl', 'rec_acl', 'rnd_acl', 'rnt_0000000000000000000000000000000000000000000000000000000000000001', 'usr_recipient',
			1, 'temporary_failure', 0, 0, statement_timestamp(), statement_timestamp())
	`); err != nil {
		t.Fatalf("runtime insert collaboration notification delivery attempt: %v", err)
	}
	deletedRecipient, err := runtimeDB.Exec(ctx, `
		delete from public.record_notification_recipients
		where notification_id = 'rnt_0000000000000000000000000000000000000000000000000000000000000002' and recipient_user_id = 'usr_other'`)
	if err != nil || deletedRecipient.RowsAffected() != 1 {
		t.Fatalf("runtime narrow recipient reconciliation delete = %d/%v, want 1/nil", deletedRecipient.RowsAffected(), err)
	}
	_, err = runtimeDB.Exec(ctx, `delete from public.record_comments where comment_id = 'rcm_acl'`)
	requirePostgresSQLState(t, err, "42501")

	_, err = runtimeDB.Exec(ctx, `update public.record_action_events set actor_id = 'usr_changed' where action_event_id = 'raev_acl'`)
	requirePostgresSQLState(t, err, "42501")
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	_, err = migratorDB.Exec(ctx, `update public.record_action_events set actor_id = 'usr_changed' where action_event_id = 'raev_acl'`)
	requirePostgresSQLState(t, err, "55000")
	_, err = migratorDB.Exec(ctx, `update public.record_notification_delivery_attempts set reason_code = 'changed' where attempt_id = 'rna_acl'`)
	requirePostgresSQLState(t, err, "55000")

	if _, err := runtimeDB.Exec(ctx, `insert into public.records (record_id) values ('rec_collabdelete')`); err != nil {
		t.Fatalf("runtime insert collaboration delete-fence root: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_followers (
			record_id, user_id, follower_version, manual_preference, record_fence_epoch
		) values ('rec_collabdelete', 'usr_acl', 1, 'watching', 0)
	`); err != nil {
		t.Fatalf("runtime insert collaboration delete-fence follower: %v", err)
	}
	_, err = runtimeDB.Exec(ctx, `delete from public.records where record_id = 'rec_collabdelete'`)
	requirePostgresSQLState(t, err, "23503")
	for _, statement := range []string{
		`delete from public.record_followers where record_id = 'rec_collabdelete'`,
		`delete from public.records where record_id = 'rec_collabdelete'`,
	} {
		if _, err := runtimeDB.Exec(ctx, statement); err != nil {
			t.Fatalf("runtime clean collaboration no-cascade fixture with %q: %v", statement, err)
		}
	}

	adminDB := fixture.openDirectRolePool(t, ctx, fixture.admin)
	var receiptCount int
	if err := adminDB.QueryRow(ctx, `select count(*)::int from public.record_collaboration_purge_receipts`).Scan(&receiptCount); err != nil {
		t.Fatalf("platform admin read content-free collaboration purge receipts: %v", err)
	}
	if receiptCount != 0 {
		t.Fatalf("fresh collaboration purge receipt count = %d, want 0", receiptCount)
	}
	_, err = adminDB.Exec(ctx, `select action_id from public.record_actions limit 1`)
	requirePostgresSQLState(t, err, "42501")

	for _, statement := range []string{
		`delete from public.record_notification_delivery_attempts where attempt_id = 'rna_acl'`,
		`delete from public.record_notification_deliveries where delivery_id = 'rnd_acl'`,
		`delete from public.record_notification_recipients where notification_id in ('rnt_0000000000000000000000000000000000000000000000000000000000000001', 'rnt_0000000000000000000000000000000000000000000000000000000000000002')`,
		`delete from public.record_notifications where notification_id in ('rnt_0000000000000000000000000000000000000000000000000000000000000001', 'rnt_0000000000000000000000000000000000000000000000000000000000000002')`,
		`delete from public.record_followers where record_id = 'rec_acl'`,
		`delete from public.record_comment_revisions where comment_revision_id = 'rcr_acl'`,
		`delete from public.record_comment_tombstones where tombstone_id = 'rct_acl'`,
		`delete from public.record_comments where comment_id = 'rcm_acl'`,
		`delete from public.record_action_events where action_event_id = 'raev_acl'`,
		`delete from public.record_actions where action_id = 'ract_acl'`,
	} {
		if _, err := migratorDB.Exec(ctx, statement); err != nil {
			t.Fatalf("migrator clean collaboration ACL fixture with %q: %v", statement, err)
		}
	}
	if _, err := runtimeDB.Exec(ctx, `delete from public.records where record_id = 'rec_aclother'`); err != nil {
		t.Fatalf("runtime clean collaboration cross-record root: %v", err)
	}
}
