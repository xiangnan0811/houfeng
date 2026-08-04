package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

const recordsPostgresDraftAuthorID = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"

func TestPostgresIntegrationRecordDraftConcurrentPatchHasOneExactETagWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedRepository := newRecordsPostgresDraftRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-draft-race-seed", 1),
	)
	original := recordsPostgresDraftPayload(t, "Original")
	draft := createRecordsPostgresDraft(t, ctx, seedRepository, "rdf_pgrace", original)

	commands := []records.DraftPatchCommand{
		{
			DraftID:  draft.DraftID,
			AuthorID: draft.AuthorID,
			IfMatch:  draft.ETag,
			Payload:  recordsPostgresDraftPayload(t, "Client A"),
			Policy:   records.DefaultDraftRetentionPolicy(),
		},
		{
			DraftID:  draft.DraftID,
			AuthorID: draft.AuthorID,
			IfMatch:  draft.ETag,
			Payload:  recordsPostgresDraftPayload(t, "Client B"),
			Policy:   records.DefaultDraftRetentionPolicy(),
		},
	}
	type patchOutcome struct {
		draft records.Draft
		err   error
	}
	outcomes := make(chan patchOutcome, len(commands))
	start := make(chan struct{})
	for index, command := range commands {
		repository := newRecordsPostgresDraftRepository(
			fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("record-draft-race-%d", index), 1),
		)
		command := command
		go func() {
			<-start
			patched, err := repository.PatchDraft(context.Background(), command)
			outcomes <- patchOutcome{draft: patched, err: err}
		}()
	}
	close(start)

	var winner records.Draft
	var winnerCount int
	var conflictCount int
	for range commands {
		outcome := waitForRecordPlatformResult(t, outcomes, "concurrent record draft patch")
		switch {
		case outcome.err == nil:
			winner = outcome.draft
			winnerCount++
		case errors.Is(outcome.err, records.ErrDraftConflict):
			var conflict *records.DraftConflictError
			if !errors.As(outcome.err, &conflict) || conflict.Server.Version != 2 {
				t.Fatalf("concurrent draft conflict = %T %#v", outcome.err, conflict)
			}
			conflictCount++
		default:
			t.Fatalf("concurrent PatchDraft() = (%#v, %v)", outcome.draft, outcome.err)
		}
	}
	if winnerCount != 1 || conflictCount != 1 || winner.Version != 2 || winner.ETag == draft.ETag {
		t.Fatalf("concurrent outcomes = winner %#v, winners/conflicts %d/%d", winner, winnerCount, conflictCount)
	}

	var persistedVersion int64
	var checkpointCount int
	if err := fixture.db.QueryRow(ctx, `
		select draft_version,
		       (select count(*)::int from public.record_draft_checkpoints where draft_id = $1)
		from public.record_drafts
		where draft_id = $1`, draft.DraftID).Scan(&persistedVersion, &checkpointCount); err != nil {
		t.Fatalf("read concurrent draft result: %v", err)
	}
	if persistedVersion != 2 || checkpointCount != 1 {
		t.Fatalf("concurrent persisted draft = version %d checkpoints %d, want 2/1", persistedVersion, checkpointCount)
	}
	assertRecordsPostgresDraftFormalSideEffects(t, ctx, fixture.db, 0, 0)
}

func TestPostgresIntegrationRecordDraftReservationFiltersExistingDraftRoutingAndOperations(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-fence", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	recordID := "rec_pgdraftfence"

	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		recordID,
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Draft fence record"),
		"record-draft-fence-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	payload := recordsPostgresDraftPayload(t, "Existing reserved draft")
	draft, err := draftRepository.CreateDraft(ctx, records.DraftCreateCommand{
		DraftID:        "rdf_pgdraftfence",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       recordID,
		BaseRevisionID: created.RevisionID,
		AuthorID:       recordsPostgresDraftAuthorID,
		Payload:        payload,
		Policy:         records.DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := runtimePool.Exec(ctx, `
		insert into public.deletion_reservations (
			reservation_id, project_id, object_kind, object_id,
			deletion_token_commitment, request_fingerprint,
			actor_scope_digest, preview_binding_digest,
			preview_current_revision_id, preview_lock_version,
			preview_authorization_epoch, preview_content_delivery_epoch,
			preview_dependency_graph_digest, preview_backup_inventory_digest,
			preview_processor_inventory_digest, adapter_readiness_digest,
			adapter_preview_digest, preview_witness_sequence,
			preview_witness_entry_hash, state, expires_at, completed_at
		) values (
			'drs_pgdraftfence', 'default', 'record', $1,
			decode(repeat('41', 32), 'hex'), decode(repeat('42', 32), 'hex'),
			decode(repeat('43', 32), 'hex'), decode(repeat('44', 32), 'hex'),
			$2, $3, $4, 0,
			decode(repeat('45', 32), 'hex'), decode(repeat('46', 32), 'hex'),
			decode(repeat('47', 32), 'hex'), decode(repeat('48', 32), 'hex'),
			decode(repeat('49', 32), 'hex'), 1,
			decode(repeat('4a', 32), 'hex'), 'committed',
			transaction_timestamp() + interval '5 minutes', transaction_timestamp()
		)`, recordID, created.RevisionID, created.LockVersion, created.AuthorizationEpoch); err != nil {
		t.Fatalf("seed committed deletion reservation: %v", err)
	}

	if _, err := draftRepository.GetDraftRouting(ctx, draft.DraftID, draft.AuthorID); !errors.Is(err, records.ErrDraftNotFound) {
		t.Fatalf("GetDraftRouting() error = %v, want ErrDraftNotFound", err)
	}
	routings, err := draftRepository.ListDraftRoutings(ctx, draft.AuthorID, 10)
	if err != nil || len(routings) != 0 {
		t.Fatalf("ListDraftRoutings() = %#v, %v, want empty", routings, err)
	}
	if _, err := draftRepository.GetDraft(ctx, draft.DraftID, draft.AuthorID); !errors.Is(err, records.ErrDraftNotFound) {
		t.Fatalf("GetDraft() error = %v, want ErrDraftNotFound", err)
	}
	if _, err := draftRepository.PatchDraft(ctx, records.DraftPatchCommand{
		DraftID: draft.DraftID, AuthorID: draft.AuthorID, IfMatch: draft.ETag,
		Payload: payload, Policy: records.DefaultDraftRetentionPolicy(),
	}); !errors.Is(err, records.ErrDraftNotFound) {
		t.Fatalf("PatchDraft() error = %v, want ErrDraftNotFound", err)
	}
	if err := draftRepository.DeleteDraft(ctx, records.DraftDeleteCommand{
		DraftID: draft.DraftID, AuthorID: draft.AuthorID, Reason: records.DraftDeleteDiscarded,
	}); !errors.Is(err, records.ErrDraftNotFound) {
		t.Fatalf("DeleteDraft() error = %v, want ErrDraftNotFound", err)
	}
	var persisted int
	if err := fixture.db.QueryRow(ctx, `select count(*)::int from public.record_drafts where draft_id = $1`, draft.DraftID).Scan(&persisted); err != nil {
		t.Fatalf("count reserved draft: %v", err)
	}
	if persisted != 1 {
		t.Fatalf("reserved draft rows = %d, want preserved until purge", persisted)
	}
}

func TestPostgresIntegrationRecordDraftCheckpointRetentionUsesFiveMinuteNewestTwentySevenDays(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := newRecordsPostgresDraftRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-draft-retention", 1),
	)
	original := recordsPostgresDraftPayload(t, "Retention original")
	draft := createRecordsPostgresDraft(t, ctx, repository, "rdf_pgretention", original)
	payloadHash := original.Hash()
	now := time.Now().UTC()
	for index := 0; index < 25; index++ {
		createdAt := now.Add(-time.Duration(index+1) * 5 * time.Minute)
		expiresAt := createdAt.Add(7 * 24 * time.Hour)
		if index < 3 {
			createdAt = now.Add(-time.Duration(8*24+index) * time.Hour)
			expiresAt = createdAt.Add(7 * 24 * time.Hour)
		}
		if _, err := fixture.db.Exec(ctx, `
			insert into public.record_draft_checkpoints (
				checkpoint_id, draft_id, checkpoint_bucket,
				checkpoint_payload, checkpoint_payload_hash, checkpoint_draft_version,
				created_at, checkpoint_expires_at
			) values ($1, $2, $3, $4::jsonb, $5, 1, $6, $7)`,
			fmt.Sprintf("rdc_pgretention%02d", index),
			draft.DraftID,
			createdAt,
			string(original.JSON()),
			payloadHash[:],
			createdAt,
			expiresAt,
		); err != nil {
			t.Fatalf("seed checkpoint %d: %v", index, err)
		}
	}

	patched, err := repository.PatchDraft(ctx, records.DraftPatchCommand{
		DraftID:  draft.DraftID,
		AuthorID: draft.AuthorID,
		IfMatch:  draft.ETag,
		Payload:  recordsPostgresDraftPayload(t, "Retention changed"),
		Policy:   records.DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		t.Fatalf("PatchDraft() error = %v", err)
	}
	if patched.Version != 2 {
		t.Fatalf("PatchDraft().Version = %d, want 2", patched.Version)
	}

	var total int
	var expired int
	var currentVersion int
	var currentBucketCanonical bool
	var currentTTLSeconds int64
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int,
		       count(*) filter (where checkpoint_expires_at <= transaction_timestamp())::int,
		       count(*) filter (where checkpoint_draft_version = 2)::int,
		       coalesce(bool_and(
		         checkpoint_bucket = date_bin(
		           interval '5 minutes', created_at, timestamptz '2000-01-01 00:00:00+00'
		         )
		       ) filter (where checkpoint_draft_version = 2), false),
		       coalesce((max(extract(epoch from checkpoint_expires_at - created_at))
		         filter (where checkpoint_draft_version = 2))::bigint, 0)
		from public.record_draft_checkpoints
		where draft_id = $1`, draft.DraftID).Scan(
		&total,
		&expired,
		&currentVersion,
		&currentBucketCanonical,
		&currentTTLSeconds,
	); err != nil {
		t.Fatalf("read retained checkpoints: %v", err)
	}
	if total != 20 || expired != 0 || currentVersion != 1 || !currentBucketCanonical || currentTTLSeconds != int64((7*24*time.Hour).Seconds()) {
		t.Fatalf("retained checkpoints = total %d expired %d v2 %d bucket %t ttl %d",
			total, expired, currentVersion, currentBucketCanonical, currentTTLSeconds)
	}
	assertRecordsPostgresDraftFormalSideEffects(t, ctx, fixture.db, 0, 0)
}

func TestPostgresIntegrationRecordDraftConcurrentExpiredCleanupClaimsAreDisjoint(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedRepository := newRecordsPostgresDraftRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-draft-cleanup-seed", 1),
	)
	wantIDs := make([]string, 0, 6)
	for index := 0; index < 6; index++ {
		draftID := fmt.Sprintf("rdf_pgcleanup%d", index)
		draft := createRecordsPostgresDraft(
			t,
			ctx,
			seedRepository,
			draftID,
			recordsPostgresDraftPayload(t, fmt.Sprintf("Expired %d", index)),
		)
		payloadHash := draft.Payload.Hash()
		if _, err := fixture.db.Exec(ctx, `
			update public.record_drafts
			set updated_at = transaction_timestamp() - interval '91 days',
			    warning_at = transaction_timestamp() - interval '8 days',
			    expires_at = transaction_timestamp() - interval '1 day'
			where draft_id = $1`, draftID); err != nil {
			t.Fatalf("expire draft %q: %v", draftID, err)
		}
		if _, err := fixture.db.Exec(ctx, `
			insert into public.record_draft_checkpoints (
				checkpoint_id, draft_id, checkpoint_bucket,
				checkpoint_payload, checkpoint_payload_hash, checkpoint_draft_version,
				created_at, checkpoint_expires_at
			) values (
				$1, $2, transaction_timestamp() - interval '8 days',
				$3::jsonb, $4, 1,
				transaction_timestamp() - interval '8 days',
				transaction_timestamp() - interval '1 day'
			)`,
			fmt.Sprintf("rdc_pgcleanup%d", index),
			draftID,
			string(draft.Payload.JSON()),
			payloadHash[:],
		); err != nil {
			t.Fatalf("seed expired checkpoint %q: %v", draftID, err)
		}
		wantIDs = append(wantIDs, draftID)
	}

	type cleanupOutcome struct {
		ids []string
		err error
	}
	outcomes := make(chan cleanupOutcome, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		repository := newRecordsPostgresDraftRepository(
			fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("record-draft-cleanup-%d", index), 1),
		)
		go func() {
			<-start
			ids, err := repository.ClaimExpiredDrafts(context.Background(), 3)
			outcomes <- cleanupOutcome{ids: ids, err: err}
		}()
	}
	close(start)

	claimed := make([]string, 0, 6)
	seen := make(map[string]struct{}, 6)
	for range 2 {
		outcome := waitForRecordPlatformResult(t, outcomes, "concurrent expired draft cleanup")
		if outcome.err != nil {
			t.Fatalf("ClaimExpiredDrafts() error = %v", outcome.err)
		}
		if len(outcome.ids) != 3 {
			t.Fatalf("ClaimExpiredDrafts() = %#v, want three IDs", outcome.ids)
		}
		for _, draftID := range outcome.ids {
			if _, exists := seen[draftID]; exists {
				t.Fatalf("concurrent cleanup claimed %q twice", draftID)
			}
			seen[draftID] = struct{}{}
			claimed = append(claimed, draftID)
		}
	}
	sort.Strings(claimed)
	sort.Strings(wantIDs)
	if fmt.Sprint(claimed) != fmt.Sprint(wantIDs) {
		t.Fatalf("claimed IDs = %#v, want %#v", claimed, wantIDs)
	}

	var draftCount int
	var checkpointCount int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_drafts),
		       (select count(*)::int from public.record_draft_checkpoints)`).Scan(&draftCount, &checkpointCount); err != nil {
		t.Fatalf("count expired cleanup rows: %v", err)
	}
	if draftCount != 0 || checkpointCount != 0 {
		t.Fatalf("expired cleanup rows = drafts %d checkpoints %d, want 0/0", draftCount, checkpointCount)
	}
	assertRecordsPostgresDraftFormalSideEffects(t, ctx, fixture.db, 0, 0)
}

func TestPostgresIntegrationRecordDraftPublishDiscardAndRevokeCleanupAreAtomic(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-lifecycle", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)

	for _, tt := range []struct {
		draftID string
		reason  records.DraftDeleteReason
	}{
		{draftID: "rdf_pgdiscard", reason: records.DraftDeleteDiscarded},
		{draftID: "rdf_pgrevoke", reason: records.DraftDeleteRevoked},
	} {
		draft := createRecordsPostgresDraft(t, ctx, draftRepository, tt.draftID, recordsPostgresDraftPayload(t, tt.draftID))
		patched, err := draftRepository.PatchDraft(ctx, records.DraftPatchCommand{
			DraftID:  draft.DraftID,
			AuthorID: draft.AuthorID,
			IfMatch:  draft.ETag,
			Payload:  recordsPostgresDraftPayload(t, tt.draftID+" changed"),
			Policy:   records.DefaultDraftRetentionPolicy(),
		})
		if err != nil {
			t.Fatalf("PatchDraft(%s) error = %v", tt.reason, err)
		}
		if err := draftRepository.DeleteDraft(ctx, records.DraftDeleteCommand{
			DraftID:  patched.DraftID,
			AuthorID: patched.AuthorID,
			Reason:   tt.reason,
		}); err != nil {
			t.Fatalf("DeleteDraft(%s) error = %v", tt.reason, err)
		}
	}
	assertRecordsPostgresDraftFormalSideEffects(t, ctx, fixture.db, 0, 0)

	publishDraft := createRecordsPostgresDraft(
		t,
		ctx,
		draftRepository,
		"rdf_pgpublish",
		recordsPostgresDraftPayload(t, "Publish"),
	)
	patchedPublish, err := draftRepository.PatchDraft(ctx, records.DraftPatchCommand{
		DraftID:  publishDraft.DraftID,
		AuthorID: publishDraft.AuthorID,
		IfMatch:  publishDraft.ETag,
		Payload:  recordsPostgresDraftPayload(t, "Publish changed"),
		Policy:   records.DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		t.Fatalf("PatchDraft(publish) error = %v", err)
	}
	input := recordsPostgresCompleteRevisionInput(t, "Published record")
	staleCommand := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgdraftpublish",
		"",
		0,
		0,
		input,
		"records-draft-publish-stale",
	)
	staleCommand.DraftID = publishDraft.DraftID
	staleCommand.DraftETag = publishDraft.ETag
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	if result, err := recordRepository.CommitRevision(ctx, staleCommand); !errors.Is(err, records.ErrDraftConflict) {
		t.Fatalf("CommitRevision(stale draft) = (%#v, %v), want ErrDraftConflict", result, err)
	}
	var preservedDrafts int
	var preservedCheckpoints int
	var rolledBackRoots int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_drafts where draft_id = $1),
		       (select count(*)::int from public.record_draft_checkpoints where draft_id = $1),
		       (select count(*)::int from public.records where record_id = $2)`,
		publishDraft.DraftID, staleCommand.RecordID).Scan(&preservedDrafts, &preservedCheckpoints, &rolledBackRoots); err != nil {
		t.Fatalf("read stale publish rollback: %v", err)
	}
	if preservedDrafts != 1 || preservedCheckpoints != 1 || rolledBackRoots != 0 {
		t.Fatalf("stale publish rows = drafts %d checkpoints %d roots %d, want 1/1/0",
			preservedDrafts, preservedCheckpoints, rolledBackRoots)
	}

	publishCommand := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		staleCommand.RecordID,
		"",
		0,
		0,
		input,
		"records-draft-publish",
	)
	publishCommand.DraftID = patchedPublish.DraftID
	publishCommand.DraftETag = patchedPublish.ETag
	published, err := recordRepository.CommitRevision(ctx, publishCommand)
	if err != nil {
		t.Fatalf("CommitRevision(publish) error = %v", err)
	}
	replayCommand := publishCommand
	replayCommand.Idempotency.OwnerID = "records_pg_draft_replay"
	replayed, err := recordRepository.CommitRevision(ctx, replayCommand)
	if err != nil {
		t.Fatalf("CommitRevision(publish replay) error = %v", err)
	}
	if !published.Created || published.Replayed || !replayed.Created || !replayed.Replayed ||
		published.RevisionID != replayed.RevisionID {
		t.Fatalf("publish results = first %#v replay %#v", published, replayed)
	}

	var draftCount int
	var checkpointCount int
	var rootCount int
	var revisionCount int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_drafts),
		       (select count(*)::int from public.record_draft_checkpoints),
		       (select count(*)::int from public.records where record_id = $1),
		       (select count(*)::int from public.record_revisions where record_id = $1)`, published.RecordID).Scan(
		&draftCount,
		&checkpointCount,
		&rootCount,
		&revisionCount,
	); err != nil {
		t.Fatalf("count published draft rows: %v", err)
	}
	if draftCount != 0 || checkpointCount != 0 || rootCount != 1 || revisionCount != 1 {
		t.Fatalf("published rows = drafts %d checkpoints %d roots %d revisions %d, want 0/0/1/1",
			draftCount, checkpointCount, rootCount, revisionCount)
	}
	assertRecordsPostgresDraftFormalSideEffects(t, ctx, fixture.db, 1, 1)
}

func newRecordsPostgresDraftRepository(pool *pgxpool.Pool) *PostgresRecordDraftRepository {
	return NewPostgresRecordDraftRepository(pool, allowRecordPlatformAdmissionGate)
}

func recordsPostgresDraftPayload(t *testing.T, title string) records.DraftPayload {
	t.Helper()
	payload, err := records.NewDraftPayload([]byte(fmt.Sprintf(`{"title":%q}`, title)))
	if err != nil {
		t.Fatalf("NewDraftPayload(%q) error = %v", title, err)
	}
	return payload
}

func createRecordsPostgresDraft(
	t *testing.T,
	ctx context.Context,
	repository *PostgresRecordDraftRepository,
	draftID string,
	payload records.DraftPayload,
) records.Draft {
	t.Helper()
	draft, err := repository.CreateDraft(ctx, records.DraftCreateCommand{
		DraftID:   draftID,
		ProjectID: recordauth.ProjectIDDefault,
		AuthorID:  recordsPostgresDraftAuthorID,
		Payload:   payload,
		Policy:    records.DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		t.Fatalf("CreateDraft(%q) error = %v", draftID, err)
	}
	return draft
}

func assertRecordsPostgresDraftFormalSideEffects(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	wantActivities int,
	wantOutbox int,
) {
	t.Helper()
	var activityCount int
	var outboxCount int
	if err := db.QueryRow(ctx, `
		select (select count(*)::int from public.record_domain_activities),
		       (select count(*)::int from public.record_outbox)`).Scan(&activityCount, &outboxCount); err != nil {
		t.Fatalf("count draft formal side effects: %v", err)
	}
	if activityCount != wantActivities || outboxCount != wantOutbox {
		t.Fatalf("draft formal side effects = activities %d outbox %d, want %d/%d",
			activityCount, outboxCount, wantActivities, wantOutbox)
	}
}
