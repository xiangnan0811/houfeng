package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func TestPostgresRecordDraftRepositoryCreatesNewDraftWithoutFormalSideEffects(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	tx := newFakeRecordDraftTx(now)
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		return tx, nil
	}
	payload, err := records.NewDraftPayload([]byte(`{"title":"Draft","body_markdown":"# Notes\n"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	command := records.DraftCreateCommand{
		DraftID:   "rdf_0123456789abcdef",
		ProjectID: recordauth.ProjectIDDefault,
		AuthorID:  "usr_0123456789abcdef01234567",
		Payload:   payload,
		Policy:    records.DefaultDraftRetentionPolicy(),
	}

	draft, err := repository.CreateDraft(context.Background(), command)
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if draft.DraftID != command.DraftID || draft.AuthorID != command.AuthorID || draft.RecordID != "" ||
		draft.BaseRevisionID != "" || draft.Version != 1 || draft.Payload.Hash() != payload.Hash() ||
		!draft.CreatedAt.Equal(now) || !draft.UpdatedAt.Equal(now) ||
		!draft.WarningAt.Equal(now.Add(83*24*time.Hour)) || !draft.ExpiresAt.Equal(now.Add(90*24*time.Hour)) {
		t.Fatalf("CreateDraft() = %#v", draft)
	}
	if draft.Validate() != nil {
		t.Fatalf("CreateDraft() returned invalid draft: %v", draft.Validate())
	}
	if tx.commitCalls != 1 || tx.rollbackCalls != 1 {
		t.Fatalf("transaction calls = commit %d rollback %d", tx.commitCalls, tx.rollbackCalls)
	}
	if len(tx.querySQL) != 1 || !strings.Contains(tx.querySQL[0], "insert into public.record_drafts") ||
		!strings.Contains(tx.querySQL[0], "transaction_timestamp()") ||
		!strings.Contains(tx.querySQL[0], "$9::bigint - $10::bigint") {
		t.Fatalf("CreateDraft() SQL = %#v", tx.querySQL)
	}
	for _, sql := range tx.allSQL() {
		compact := strings.ToLower(sql)
		for _, forbidden := range []string{
			"record_revisions",
			"record_revision_participants",
			"record_domain_activities",
			"record_outbox",
			"search",
			"notification",
		} {
			if strings.Contains(compact, forbidden) {
				t.Fatalf("draft transaction contains forbidden formal side effect %q:\n%s", forbidden, sql)
			}
		}
	}
}

func TestPostgresRecordDraftRoutingReadsNoPayloadAndChecksReadFence(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	payload, err := records.NewDraftPayload([]byte(`{"title":"Private routing"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := records.NewDraftETag("rdf_0123456789abcdef", "usr_0123456789abcdef01234567", 1, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	tx := newFakeRecordDraftTx(now)
	tx.storedDraft = &records.Draft{
		DraftID:        "rdf_0123456789abcdef",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       "rec_0123456789abcdef",
		BaseRevisionID: "rrv_0123456789abcdef",
		AuthorID:       "usr_0123456789abcdef01234567",
		Payload:        payload,
		Version:        1,
		ETag:           etag,
		CreatedAt:      now,
		UpdatedAt:      now,
		WarningAt:      now.Add(83 * 24 * time.Hour),
		ExpiresAt:      now.Add(90 * 24 * time.Hour),
	}
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	routing, err := repository.GetDraftRouting(context.Background(), tx.storedDraft.DraftID, tx.storedDraft.AuthorID)
	if err != nil {
		t.Fatalf("GetDraftRouting() error = %v", err)
	}
	if routing.DraftID != tx.storedDraft.DraftID || routing.RecordID != tx.storedDraft.RecordID ||
		!routing.UpdatedAt.Equal(now) || routing.Validate() != nil {
		t.Fatalf("GetDraftRouting() = %#v", routing)
	}
	joined := strings.ToLower(strings.Join(tx.querySQL, "\n"))
	if strings.Contains(joined, "payload_hash") || strings.Contains(joined, "etag_digest") ||
		!strings.Contains(joined, "not exists") || !strings.Contains(joined, "content_delivery_epochs") ||
		!strings.Contains(joined, "deletion_fence_leases") {
		t.Fatalf("routing SQL does not preserve metadata/fence boundary:\n%s", joined)
	}
}

func TestPostgresRecordDraftRepositoryAuthorizesAttachmentUploadForExactAuthor(t *testing.T) {
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	ownerID := "usr_0123456789abcdef01234567"
	tx := newFakeRecordDraftTx(now)
	tx.storedDraft = &records.Draft{
		DraftID:   "rdf_0123456789abcdef",
		ProjectID: recordauth.ProjectIDDefault,
		AuthorID:  ownerID,
		UpdatedAt: now,
	}
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: ownerID, Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}

	if err := repository.AuthorizeDraftAttachmentUpload(context.Background(), actor, tx.storedDraft.DraftID); err != nil {
		t.Fatalf("AuthorizeDraftAttachmentUpload() error = %v", err)
	}

	foreignActor := actor.Clone()
	foreignActor.UserID = "usr_abcdef0123456789abcdef01"
	if err := repository.AuthorizeDraftAttachmentUpload(context.Background(), foreignActor, tx.storedDraft.DraftID); !errors.Is(err, records.ErrDraftNotFound) {
		t.Fatalf("foreign AuthorizeDraftAttachmentUpload() error = %v, want opaque draft not found", err)
	}
	if err := repository.AuthorizeDraftAttachmentUpload(context.Background(), recordauth.ActorScope{}, tx.storedDraft.DraftID); !errors.Is(err, recordauth.ErrDenied) {
		t.Fatalf("invalid actor AuthorizeDraftAttachmentUpload() error = %v, want ErrDenied", err)
	}
}

func TestPostgresRecordDraftRoutingFiltersReservedRecordBeforeReturningMetadata(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	ownerID := "usr_0123456789abcdef01234567"
	tx := newFakeRecordDraftTx(now)
	tx.storedDraft = &records.Draft{
		DraftID:        "rdf_0123456789abcdef",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       "rec_0123456789abcdef",
		BaseRevisionID: "rrv_0123456789abcdef",
		AuthorID:       ownerID,
		UpdatedAt:      now,
	}
	tx.fencedRecordIDs = map[string]bool{tx.storedDraft.RecordID: true}
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	_, err := repository.GetDraftRouting(context.Background(), tx.storedDraft.DraftID, ownerID)
	if !errors.Is(err, records.ErrDraftNotFound) {
		t.Fatalf("GetDraftRouting() error = %v, want ErrDraftNotFound", err)
	}
	if len(tx.querySQL) != 1 {
		t.Fatalf("reserved routing queries = %#v, want one atomic lookup", tx.querySQL)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	for _, fragment := range []string{
		"author_id = $2",
		"not exists",
		"from public.deletion_reservations reservations",
		"reservations.object_id = drafts.record_id",
	} {
		if !strings.Contains(compact, fragment) {
			t.Fatalf("reserved routing query missing %q:\n%s", fragment, tx.querySQL[0])
		}
	}
}

func TestPostgresRecordDraftExistingOperationsFilterReservationInRoutingQuery(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	payload, err := records.NewDraftPayload([]byte(`{"title":"Reserved draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := records.NewDraftETag("rdf_0123456789abcdef", "usr_0123456789abcdef01234567", 1, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	stored := records.Draft{
		DraftID:        "rdf_0123456789abcdef",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       "rec_0123456789abcdef",
		BaseRevisionID: "rrv_0123456789abcdef",
		AuthorID:       "usr_0123456789abcdef01234567",
		Payload:        payload,
		Version:        1,
		ETag:           etag,
		WarningAt:      now.Add(83 * 24 * time.Hour),
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(90 * 24 * time.Hour),
	}

	tests := map[string]func(*PostgresRecordDraftRepository) error{
		"get": func(repository *PostgresRecordDraftRepository) error {
			_, err := repository.GetDraft(context.Background(), stored.DraftID, stored.AuthorID)
			return err
		},
		"patch": func(repository *PostgresRecordDraftRepository) error {
			_, err := repository.PatchDraft(context.Background(), records.DraftPatchCommand{
				DraftID: stored.DraftID, AuthorID: stored.AuthorID, IfMatch: stored.ETag,
				Payload: stored.Payload, Policy: records.DefaultDraftRetentionPolicy(),
			})
			return err
		},
		"delete": func(repository *PostgresRecordDraftRepository) error {
			return repository.DeleteDraft(context.Background(), records.DraftDeleteCommand{
				DraftID: stored.DraftID, AuthorID: stored.AuthorID, Reason: records.DraftDeleteDiscarded,
			})
		},
	}
	for name, call := range tests {
		t.Run(name, func(t *testing.T) {
			draft := stored
			tx := newFakeRecordDraftTx(now)
			tx.storedDraft = &draft
			tx.fencedRecordIDs = map[string]bool{stored.RecordID: true}
			repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
			repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

			err := call(repository)
			if !errors.Is(err, records.ErrDraftNotFound) {
				t.Fatalf("operation error = %v, want ErrDraftNotFound", err)
			}
			if len(tx.querySQL) != 1 || len(tx.execSQL) != 0 {
				t.Fatalf("reserved operation SQL = queries %#v exec %#v", tx.querySQL, tx.execSQL)
			}
			compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
			if !strings.Contains(compact, "not exists") ||
				!strings.Contains(compact, "from public.deletion_reservations reservations") ||
				strings.Contains(compact, "payload_hash") || strings.Contains(compact, "etag_digest") {
				t.Fatalf("reserved operation routing query =\n%s", tx.querySQL[0])
			}
		})
	}
}

func TestPostgresRecordDraftRepositoryListsAuthorRoutingMetadataInStableOrderAndSkipsFencedRecords(t *testing.T) {
	now := time.Date(2026, time.August, 3, 13, 0, 0, 0, time.UTC)
	ownerID := "usr_0123456789abcdef01234567"
	tx := newFakeRecordDraftTx(now)
	tx.routingDrafts = []records.Draft{
		{
			DraftID:   "rdf_0000000000000003",
			ProjectID: recordauth.ProjectIDDefault,
			AuthorID:  ownerID,
			UpdatedAt: now,
		},
		{
			DraftID:        "rdf_0000000000000002",
			ProjectID:      recordauth.ProjectIDDefault,
			RecordID:       "rec_0000000000000002",
			BaseRevisionID: "rrv_0000000000000002",
			AuthorID:       ownerID,
			UpdatedAt:      now.Add(-time.Minute),
		},
		{
			DraftID:        "rdf_0000000000000001",
			ProjectID:      recordauth.ProjectIDDefault,
			RecordID:       "rec_0000000000000001",
			BaseRevisionID: "rrv_0000000000000001",
			AuthorID:       ownerID,
			UpdatedAt:      now.Add(-2 * time.Minute),
		},
	}
	tx.fencedRecordIDs = map[string]bool{"rec_0000000000000002": true}
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	routings, err := repository.ListDraftRoutings(context.Background(), ownerID, 3)
	if err != nil {
		t.Fatalf("ListDraftRoutings() error = %v", err)
	}
	if len(routings) != 2 || routings[0].DraftID != "rdf_0000000000000003" ||
		routings[1].DraftID != "rdf_0000000000000001" {
		t.Fatalf("ListDraftRoutings() = %#v", routings)
	}
	if len(tx.querySQL) == 0 || len(tx.queryArgs) == 0 || len(tx.queryArgs[0]) != 3 ||
		tx.queryArgs[0][0] != ownerID || tx.queryArgs[0][1] != int64(3) || tx.queryArgs[0][2] != recordObjectKind {
		t.Fatalf("routing list query args = %#v", tx.queryArgs)
	}
	listSQL := strings.ToLower(tx.querySQL[0])
	for _, fragment := range []string{
		"where drafts.author_id = $1",
		"not exists",
		"from public.deletion_reservations reservations",
		"order by drafts.updated_at desc, drafts.draft_id desc",
		"limit $2",
	} {
		if !strings.Contains(listSQL, fragment) {
			t.Fatalf("routing list SQL missing %q:\n%s", fragment, listSQL)
		}
	}
	if tx.routingScans != 2 {
		t.Fatalf("routing rows scanned = %d, want only the two unfenced rows", tx.routingScans)
	}
	for _, forbidden := range []string{"payload", "payload_hash", "draft_version", "etag_digest"} {
		if strings.Contains(listSQL, forbidden) {
			t.Fatalf("routing list SQL reads private content field %q:\n%s", forbidden, listSQL)
		}
	}
}

func TestPostgresRecordDraftRepositoryExistingDraftChecksFenceAndCurrentBase(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	payload, err := records.NewDraftPayload([]byte(`{"title":"Existing draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	command := records.DraftCreateCommand{
		DraftID:        "rdf_0123456789abcdef",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       "rec_0123456789abcdef",
		BaseRevisionID: "rrv_0123456789abcdef",
		AuthorID:       "usr_0123456789abcdef01234567",
		Payload:        payload,
		Policy:         records.DefaultDraftRetentionPolicy(),
	}

	t.Run("matching active base", func(t *testing.T) {
		tx := newFakeRecordDraftTx(now)
		tx.currentRevisionID = command.BaseRevisionID
		tx.currentLifecycle = string(records.LifecycleActive)
		repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
		repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

		draft, err := repository.CreateDraft(context.Background(), command)
		if err != nil {
			t.Fatalf("CreateDraft() error = %v", err)
		}
		if draft.RecordID != command.RecordID || draft.BaseRevisionID != command.BaseRevisionID {
			t.Fatalf("CreateDraft() = %#v", draft)
		}
		rootIndex := sqlIndexContaining(tx.querySQL, "from public.records")
		insertIndex := sqlIndexContaining(tx.querySQL, "insert into public.record_drafts")
		reservationIndex := sqlIndexContaining(tx.querySQL, "from public.deletion_reservations")
		if reservationIndex < 0 || rootIndex <= reservationIndex || insertIndex <= rootIndex {
			t.Fatalf("existing draft SQL order = %#v", tx.querySQL)
		}
	})

	t.Run("stale base", func(t *testing.T) {
		tx := newFakeRecordDraftTx(now)
		tx.currentRevisionID = "rrv_fedcba9876543210"
		tx.currentLifecycle = string(records.LifecycleActive)
		repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
		repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

		_, err := repository.CreateDraft(context.Background(), command)
		if !errors.Is(err, records.ErrDraftRevisionConflict) {
			t.Fatalf("CreateDraft() error = %v, want ErrDraftRevisionConflict", err)
		}
		if sqlIndexContaining(tx.querySQL, "insert into public.record_drafts") >= 0 {
			t.Fatalf("stale base inserted a draft: %#v", tx.querySQL)
		}
	})
}

func TestPostgresRecordDraftRepositoryGetIsAuthorScopedBeforePayloadRead(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	payload, err := records.NewDraftPayload([]byte(`{"title":"Private"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := records.NewDraftETag("rdf_0123456789abcdef", "usr_0123456789abcdef01234567", 3, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	stored := records.Draft{
		DraftID:   "rdf_0123456789abcdef",
		ProjectID: recordauth.ProjectIDDefault,
		AuthorID:  "usr_0123456789abcdef01234567",
		Payload:   payload,
		Version:   3,
		ETag:      etag,
		WarningAt: now.Add(83 * 24 * time.Hour),
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now,
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}
	tx := newFakeRecordDraftTx(now)
	tx.storedDraft = &stored
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	got, err := repository.GetDraft(context.Background(), stored.DraftID, stored.AuthorID)
	if err != nil {
		t.Fatalf("GetDraft(owner) error = %v", err)
	}
	if got.DraftID != stored.DraftID || got.Payload.Hash() != stored.Payload.Hash() || got.ETag != stored.ETag {
		t.Fatalf("GetDraft(owner) = %#v", got)
	}
	queryCountBeforeOther := len(tx.querySQL)
	_, err = repository.GetDraft(context.Background(), stored.DraftID, "usr_89abcdef0123456701234567")
	if !errors.Is(err, records.ErrDraftNotFound) {
		t.Fatalf("GetDraft(other) error = %v, want ErrDraftNotFound", err)
	}
	otherQueries := tx.querySQL[queryCountBeforeOther:]
	if len(otherQueries) != 1 || !strings.Contains(otherQueries[0], "select drafts.record_id") {
		t.Fatalf("GetDraft(other) queried beyond routing metadata: %#v", otherQueries)
	}
	for _, sql := range tx.querySQL {
		if strings.Contains(sql, "record_drafts") && !strings.Contains(sql, "author_id = $2") {
			t.Fatalf("draft read is not author scoped:\n%s", sql)
		}
	}
}

func TestPostgresRecordDraftRepositoryPatchHasOneExactETagWinnerAndBoundedCheckpoint(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 6, 0, 0, time.UTC)
	originalPayload, err := records.NewDraftPayload([]byte(`{"title":"Original"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload(original) error = %v", err)
	}
	localA, err := records.NewDraftPayload([]byte(`{"title":"Client A"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload(client A) error = %v", err)
	}
	localB, err := records.NewDraftPayload([]byte(`{"title":"Client B"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload(client B) error = %v", err)
	}
	originalETag, err := records.NewDraftETag("rdf_0123456789abcdef", "usr_0123456789abcdef01234567", 1, originalPayload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	stored := records.Draft{
		DraftID:   "rdf_0123456789abcdef",
		ProjectID: recordauth.ProjectIDDefault,
		AuthorID:  "usr_0123456789abcdef01234567",
		Payload:   originalPayload,
		Version:   1,
		ETag:      originalETag,
		WarningAt: now.Add(83 * 24 * time.Hour),
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(90 * 24 * time.Hour),
	}
	tx := newFakeRecordDraftTx(now)
	tx.storedDraft = &stored
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.newCheckpointID = func() (string, error) { return "rdc_0123456789abcdef", nil }
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	winner, err := repository.PatchDraft(context.Background(), records.DraftPatchCommand{
		DraftID:  stored.DraftID,
		AuthorID: stored.AuthorID,
		IfMatch:  originalETag,
		Payload:  localA,
		Policy:   records.DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		t.Fatalf("PatchDraft(client A) error = %v", err)
	}
	if winner.Version != 2 || winner.Payload.Hash() != localA.Hash() || winner.ETag == originalETag {
		t.Fatalf("PatchDraft(client A) = %#v", winner)
	}
	writesAfterWinner := len(tx.execSQL) + sqlCountContaining(tx.querySQL, "update public.record_drafts")

	_, err = repository.PatchDraft(context.Background(), records.DraftPatchCommand{
		DraftID:  stored.DraftID,
		AuthorID: stored.AuthorID,
		IfMatch:  originalETag,
		Payload:  localB,
		Policy:   records.DefaultDraftRetentionPolicy(),
	})
	if !errors.Is(err, records.ErrDraftConflict) {
		t.Fatalf("PatchDraft(client B) error = %v, want ErrDraftConflict", err)
	}
	var conflict *records.DraftConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("PatchDraft(client B) error type = %T, want *DraftConflictError", err)
	}
	if conflict.Server.Version != 2 || conflict.Server.Payload.Hash() != localA.Hash() || conflict.LocalPayload.Hash() != localB.Hash() {
		t.Fatalf("PatchDraft(client B) conflict = %#v", conflict)
	}
	writesAfterConflict := len(tx.execSQL) + sqlCountContaining(tx.querySQL, "update public.record_drafts")
	if writesAfterConflict != writesAfterWinner {
		t.Fatalf("stale client writes = %d after winner %d", writesAfterConflict, writesAfterWinner)
	}

	joinedSQL := strings.ToLower(strings.Join(tx.allSQL(), "\n"))
	for _, fragment := range []string{
		"insert into public.record_draft_checkpoints",
		"date_bin",
		"interval '1 microsecond'",
		"$8::bigint - $9::bigint",
		"checkpoint_expires_at <= transaction_timestamp()",
		"offset $",
	} {
		if !strings.Contains(joinedSQL, fragment) {
			t.Fatalf("PATCH SQL missing %q:\n%s", fragment, joinedSQL)
		}
	}
	if got := strings.Count(joinedSQL, "insert into public.record_draft_checkpoints"); got != 1 {
		t.Fatalf("checkpoint insert count = %d, want 1", got)
	}
	for _, forbidden := range []string{"record_domain_activities", "record_outbox", "search", "notification"} {
		if strings.Contains(joinedSQL, forbidden) {
			t.Fatalf("PATCH SQL contains forbidden side effect %q", forbidden)
		}
	}
}

func TestPostgresRecordDraftRepositoryPatchUnchangedPayloadOnlyRefreshesRetention(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 6, 0, 0, time.UTC)
	payload, err := records.NewDraftPayload([]byte(`{"title":"Unchanged"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := records.NewDraftETag("rdf_0123456789abcdef", "usr_0123456789abcdef01234567", 3, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	stored := records.Draft{
		DraftID:   "rdf_0123456789abcdef",
		ProjectID: recordauth.ProjectIDDefault,
		AuthorID:  "usr_0123456789abcdef01234567",
		Payload:   payload,
		Version:   3,
		ETag:      etag,
		WarningAt: now.Add(82 * 24 * time.Hour),
		CreatedAt: now.Add(-time.Hour),
		UpdatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(89 * 24 * time.Hour),
	}
	tx := newFakeRecordDraftTx(now)
	tx.storedDraft = &stored
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	checkpointIDCalls := 0
	repository.newCheckpointID = func() (string, error) {
		checkpointIDCalls++
		return "rdc_0123456789abcdef", nil
	}
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	updated, err := repository.PatchDraft(context.Background(), records.DraftPatchCommand{
		DraftID:  stored.DraftID,
		AuthorID: stored.AuthorID,
		IfMatch:  etag,
		Payload:  payload,
		Policy:   records.DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		t.Fatalf("PatchDraft() error = %v", err)
	}
	if updated.Version != stored.Version || updated.ETag != etag || updated.Payload.Hash() != payload.Hash() {
		t.Fatalf("PatchDraft() changed content authority = %#v", updated)
	}
	if !updated.UpdatedAt.Equal(now) || !updated.WarningAt.Equal(now.Add(83*24*time.Hour)) ||
		!updated.ExpiresAt.Equal(now.Add(90*24*time.Hour)) {
		t.Fatalf("PatchDraft() retention = updated %v warning %v expiry %v", updated.UpdatedAt, updated.WarningAt, updated.ExpiresAt)
	}
	if checkpointIDCalls != 0 {
		t.Fatalf("checkpoint ID calls = %d, want 0", checkpointIDCalls)
	}
	joinedSQL := strings.ToLower(strings.Join(tx.allSQL(), "\n"))
	if got := strings.Count(joinedSQL, "update public.record_drafts"); got != 1 {
		t.Fatalf("record draft update count = %d, want 1\n%s", got, joinedSQL)
	}
	for _, forbidden := range []string{
		"insert into public.record_draft_checkpoints",
		"draft_version =",
		"payload =",
		"record_domain_activities",
		"record_outbox",
		"search",
		"notification",
	} {
		if strings.Contains(joinedSQL, forbidden) {
			t.Fatalf("unchanged PATCH contains forbidden SQL %q:\n%s", forbidden, joinedSQL)
		}
	}
}

func TestPostgresRecordDraftRepositoryDeleteIsAuthorScopedAndCleansCheckpointsBeforeDraft(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 6, 0, 0, time.UTC)
	payload, err := records.NewDraftPayload([]byte(`{"title":"Cleanup"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := records.NewDraftETag("rdf_0123456789abcdef", "usr_0123456789abcdef01234567", 3, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	staleETag, err := records.NewDraftETag("rdf_0123456789abcdef", "usr_0123456789abcdef01234567", 2, payload)
	if err != nil {
		t.Fatalf("NewDraftETag(stale) error = %v", err)
	}
	baseDraft := records.Draft{
		DraftID:        "rdf_0123456789abcdef",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       "rec_0123456789abcdef",
		BaseRevisionID: "rrv_0123456789abcdef",
		AuthorID:       "usr_0123456789abcdef01234567",
		Payload:        payload,
		Version:        3,
		ETag:           etag,
		WarningAt:      now.Add(83 * 24 * time.Hour),
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now,
		ExpiresAt:      now.Add(90 * 24 * time.Hour),
	}

	t.Run("other author is not found before payload read", func(t *testing.T) {
		tx := newFakeRecordDraftTx(now)
		tx.storedDraft = &baseDraft
		repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
		repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

		err := repository.DeleteDraft(context.Background(), records.DraftDeleteCommand{
			DraftID:  baseDraft.DraftID,
			AuthorID: "usr_89abcdef0123456701234567",
			Reason:   records.DraftDeleteDiscarded,
		})
		if !errors.Is(err, records.ErrDraftNotFound) {
			t.Fatalf("DeleteDraft() error = %v, want ErrDraftNotFound", err)
		}
		if len(tx.querySQL) != 1 || !strings.Contains(strings.ToLower(tx.querySQL[0]), "select drafts.record_id") || len(tx.execSQL) != 0 {
			t.Fatalf("other-author cleanup SQL = queries %#v exec %#v", tx.querySQL, tx.execSQL)
		}
	})

	t.Run("stale publish etag has zero writes", func(t *testing.T) {
		tx := newFakeRecordDraftTx(now)
		tx.storedDraft = &baseDraft
		repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
		repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

		err := repository.DeleteDraft(context.Background(), records.DraftDeleteCommand{
			DraftID:  baseDraft.DraftID,
			AuthorID: baseDraft.AuthorID,
			Reason:   records.DraftDeletePublished,
			IfMatch:  staleETag,
		})
		if !errors.Is(err, records.ErrDraftConflict) {
			t.Fatalf("DeleteDraft() error = %v, want ErrDraftConflict", err)
		}
		for _, sql := range tx.execSQL {
			if strings.Contains(strings.ToLower(sql), "record_draft") {
				t.Fatalf("stale publish attempted draft cleanup:\n%s", sql)
			}
		}
	})

	tests := []struct {
		name    string
		reason  records.DraftDeleteReason
		ifMatch records.DraftETag
	}{
		{name: "publish", reason: records.DraftDeletePublished, ifMatch: etag},
		{name: "discard", reason: records.DraftDeleteDiscarded},
		{name: "revoke", reason: records.DraftDeleteRevoked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			draft := baseDraft
			tx := newFakeRecordDraftTx(now)
			tx.storedDraft = &draft
			repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
			repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

			err := repository.DeleteDraft(context.Background(), records.DraftDeleteCommand{
				DraftID:  draft.DraftID,
				AuthorID: draft.AuthorID,
				Reason:   tt.reason,
				IfMatch:  tt.ifMatch,
			})
			if err != nil {
				t.Fatalf("DeleteDraft() error = %v", err)
			}
			fenceIndex := sqlIndexContaining(tx.querySQL, "from public.deletion_reservations")
			lockIndex := sqlIndexContaining(tx.querySQL, "select draft_id")
			if fenceIndex < 0 || lockIndex <= fenceIndex {
				t.Fatalf("cleanup read order = %#v", tx.querySQL)
			}
			checkpointDeleteIndex := sqlIndexContaining(tx.execSQL, "delete from public.record_draft_checkpoints")
			draftDeleteIndex := sqlIndexContaining(tx.execSQL, "delete from public.record_drafts")
			if checkpointDeleteIndex < 0 || draftDeleteIndex <= checkpointDeleteIndex {
				t.Fatalf("cleanup write order = %#v", tx.execSQL)
			}
			if !strings.Contains(strings.ToLower(tx.execSQL[draftDeleteIndex]), "author_id = $2") {
				t.Fatalf("draft cleanup is not author scoped:\n%s", tx.execSQL[draftDeleteIndex])
			}
			joinedSQL := strings.ToLower(strings.Join(tx.allSQL(), "\n"))
			for _, forbidden := range []string{"record_domain_activities", "record_outbox", "search", "notification"} {
				if strings.Contains(joinedSQL, forbidden) {
					t.Fatalf("cleanup contains forbidden side effect %q", forbidden)
				}
			}
		})
	}
}

func TestPostgresRecordDraftRepositoryClaimsExpiredDraftsInBoundedSkipLockedBatch(t *testing.T) {
	now := time.Date(2026, time.August, 3, 12, 6, 0, 0, time.UTC)

	for _, limit := range []uint64{0, 101} {
		t.Run("invalid limit", func(t *testing.T) {
			repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
			repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				t.Fatal("invalid cleanup limit began a transaction")
				return nil, nil
			}
			_, err := repository.ClaimExpiredDrafts(context.Background(), limit)
			if !errors.Is(err, records.ErrInvalidDraftCommand) {
				t.Fatalf("ClaimExpiredDrafts(%d) error = %v, want ErrInvalidDraftCommand", limit, err)
			}
		})
	}

	t.Run("empty batch commits without deletes", func(t *testing.T) {
		tx := newFakeRecordDraftTx(now)
		repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
		repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

		claimed, err := repository.ClaimExpiredDrafts(context.Background(), 10)
		if err != nil {
			t.Fatalf("ClaimExpiredDrafts() error = %v", err)
		}
		if claimed == nil || len(claimed) != 0 {
			t.Fatalf("ClaimExpiredDrafts() = %#v, want non-nil empty", claimed)
		}
		if len(tx.execSQL) != 0 {
			t.Fatalf("empty cleanup writes = %#v", tx.execSQL)
		}
	})

	t.Run("reserved existing draft is not claimed or deleted", func(t *testing.T) {
		const (
			draftID  = "rdf_0000000000000001"
			recordID = "rec_0000000000000001"
		)
		tx := newFakeRecordDraftTx(now)
		tx.expiredDraftIDs = []string{draftID}
		tx.expiredDraftRecordIDs = map[string]string{draftID: recordID}
		tx.fencedRecordIDs = map[string]bool{recordID: true}
		repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
		repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

		claimed, err := repository.ClaimExpiredDrafts(context.Background(), 10)
		if err != nil {
			t.Fatalf("ClaimExpiredDrafts() error = %v", err)
		}
		if len(claimed) != 0 {
			t.Fatalf("ClaimExpiredDrafts() = %#v, want reserved draft omitted", claimed)
		}
		if len(tx.execSQL) != 0 {
			t.Fatalf("reserved cleanup writes = %#v, want none", tx.execSQL)
		}
		claimSQL := strings.ToLower(tx.querySQL[0])
		for _, fragment := range []string{
			"drafts.record_id is null",
			"not exists",
			"from public.deletion_reservations",
			"state in ('fenced', 'committed')",
		} {
			if !strings.Contains(claimSQL, fragment) {
				t.Errorf("reserved cleanup claim SQL missing %q:\n%s", fragment, claimSQL)
			}
		}
	})

	t.Run("reservation racing after claim aborts before cleanup writes", func(t *testing.T) {
		const (
			draftID  = "rdf_0000000000000002"
			recordID = "rec_0000000000000002"
		)
		tx := newFakeRecordDraftTx(now)
		tx.expiredDraftIDs = []string{draftID}
		tx.expiredDraftRecordIDs = map[string]string{draftID: recordID}
		tx.fencedRecordIDs = make(map[string]bool)
		tx.afterExpiredDraftScan = func() { tx.fencedRecordIDs[recordID] = true }
		repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
		repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

		_, err := repository.ClaimExpiredDrafts(context.Background(), 10)
		if !errors.Is(err, records.ErrRecordDeletionReserved) {
			t.Fatalf("ClaimExpiredDrafts() error = %v, want ErrRecordDeletionReserved", err)
		}
		for _, sql := range tx.execSQL {
			if strings.Contains(strings.ToLower(sql), "delete from public.record_draft") {
				t.Fatalf("racing reservation allowed cleanup write:\n%s", sql)
			}
		}
	})

	t.Run("claims and deletes atomically", func(t *testing.T) {
		tx := newFakeRecordDraftTx(now)
		tx.expiredDraftIDs = []string{"rdf_0000000000000001", "rdf_0000000000000002"}
		tx.expiredDraftRecordIDs = map[string]string{
			"rdf_0000000000000001": "rec_0000000000000001",
		}
		repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
		repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

		claimed, err := repository.ClaimExpiredDrafts(context.Background(), 2)
		if err != nil {
			t.Fatalf("ClaimExpiredDrafts() error = %v", err)
		}
		if strings.Join(claimed, ",") != strings.Join(tx.expiredDraftIDs, ",") {
			t.Fatalf("ClaimExpiredDrafts() = %#v, want %#v", claimed, tx.expiredDraftIDs)
		}
		if len(tx.queryArgs) != 1 || len(tx.queryArgs[0]) != 2 ||
			tx.queryArgs[0][0] != int64(2) || tx.queryArgs[0][1] != recordObjectKind {
			t.Fatalf("cleanup claim query = sql %#v args %#v", tx.querySQL, tx.queryArgs)
		}
		claimSQL := strings.ToLower(tx.querySQL[0])
		for _, fragment := range []string{
			"select drafts.draft_id, drafts.record_id",
			"expires_at <= transaction_timestamp()",
			"drafts.record_id is null",
			"not exists",
			"from public.deletion_reservations",
			"state in ('fenced', 'committed')",
			"order by drafts.expires_at, drafts.draft_id",
			"for update skip locked",
			"limit $1",
		} {
			if !strings.Contains(claimSQL, fragment) {
				t.Fatalf("cleanup claim SQL missing %q:\n%s", fragment, claimSQL)
			}
		}
		checkpointDeleteIndex := sqlIndexContaining(tx.execSQL, "delete from public.record_draft_checkpoints")
		draftDeleteIndex := sqlIndexContaining(tx.execSQL, "delete from public.record_drafts")
		if checkpointDeleteIndex < 0 || draftDeleteIndex <= checkpointDeleteIndex {
			t.Fatalf("cleanup writes = %#v", tx.execSQL)
		}
		for _, sql := range tx.execSQL {
			if strings.Contains(strings.ToLower(sql), "delete from public.record_draft") &&
				!strings.Contains(strings.ToLower(sql), "any($1::text[])") {
				t.Fatalf("cleanup delete is not bound to claimed IDs:\n%s", sql)
			}
		}
		joinedQueries := strings.ToLower(strings.Join(tx.querySQL, "\n"))
		for _, fragment := range []string{
			"from public.deletion_reservations",
			"from public.content_delivery_epochs",
			"from public.deletion_fence_leases",
		} {
			if !strings.Contains(joinedQueries, fragment) {
				t.Errorf("expired cleanup mutation fence missing %q:\n%s", fragment, joinedQueries)
			}
		}
		joinedSQL := strings.ToLower(strings.Join(tx.allSQL(), "\n"))
		for _, forbidden := range []string{"record_domain_activities", "record_outbox", "search", "notification"} {
			if strings.Contains(joinedSQL, forbidden) {
				t.Fatalf("expired cleanup contains forbidden side effect %q", forbidden)
			}
		}
	})
}

type fakeRecordDraftTx struct {
	*fakeRecordRevisionTx
	now                   time.Time
	querySQL              []string
	execSQL               []string
	commitCalls           int
	rollbackCalls         int
	currentRevisionID     string
	currentLifecycle      string
	storedDraft           *records.Draft
	routingDrafts         []records.Draft
	routingScans          int
	fencedRecordIDs       map[string]bool
	expiredDraftIDs       []string
	expiredDraftRecordIDs map[string]string
	afterExpiredDraftScan func()
	queryArgs             [][]any
}

func (tx *fakeRecordDraftTx) Commit(context.Context) error {
	tx.commitCalls++
	return nil
}

func (tx *fakeRecordDraftTx) Rollback(context.Context) error {
	tx.rollbackCalls++
	return nil
}

func newFakeRecordDraftTx(now time.Time) *fakeRecordDraftTx {
	return &fakeRecordDraftTx{
		fakeRecordRevisionTx: &fakeRecordRevisionTx{},
		now:                  now,
	}
}

func (tx *fakeRecordDraftTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.querySQL = append(tx.querySQL, sql)
	return fakeRecordDraftRow{scan: func(dest ...any) error {
		compact := strings.ToLower(sql)
		if strings.Contains(sql, "insert into public.record_drafts") {
			*(dest[0].(*time.Time)) = tx.now
			*(dest[1].(*time.Time)) = tx.now
			*(dest[2].(*time.Time)) = tx.now.Add(83 * 24 * time.Hour)
			*(dest[3].(*time.Time)) = tx.now.Add(90 * 24 * time.Hour)
			return nil
		}
		if (strings.Contains(compact, "select record_id") || strings.Contains(compact, "select drafts.record_id")) &&
			strings.Contains(compact, "from public.record_drafts") {
			if tx.storedDraft == nil || len(args) < 2 || args[0] != tx.storedDraft.DraftID || args[1] != tx.storedDraft.AuthorID {
				return pgx.ErrNoRows
			}
			if strings.Contains(compact, "deletion_reservations") && tx.fencedRecordIDs[tx.storedDraft.RecordID] {
				return pgx.ErrNoRows
			}
			if tx.storedDraft.RecordID == "" {
				*(dest[0].(**string)) = nil
			} else {
				recordID := tx.storedDraft.RecordID
				*(dest[0].(**string)) = &recordID
			}
			return nil
		}
		if (strings.Contains(compact, "select draft_id") || strings.Contains(compact, "select drafts.draft_id")) &&
			strings.Contains(compact, "from public.record_drafts") {
			if tx.storedDraft == nil || len(args) < 2 || args[0] != tx.storedDraft.DraftID || args[1] != tx.storedDraft.AuthorID {
				return pgx.ErrNoRows
			}
			if strings.Contains(compact, "deletion_reservations") && tx.fencedRecordIDs[tx.storedDraft.RecordID] {
				return pgx.ErrNoRows
			}
			if len(dest) == 6 {
				return scanFakeStoredDraftRouting(*tx.storedDraft, dest...)
			}
			return scanFakeStoredDraft(*tx.storedDraft, dest...)
		}
		if strings.Contains(compact, "from public.deletion_reservations") {
			if len(args) >= 3 {
				recordID, _ := args[2].(string)
				if tx.fencedRecordIDs[recordID] {
					*(dest[0].(*string)) = "fenced"
					return nil
				}
			}
			return pgx.ErrNoRows
		}
		if strings.Contains(compact, "update public.record_drafts") {
			if tx.storedDraft == nil || len(args) < 5 || args[0] != tx.storedDraft.DraftID || args[1] != tx.storedDraft.AuthorID {
				return pgx.ErrNoRows
			}
			if len(args) == 5 {
				tx.storedDraft.UpdatedAt = tx.now
				tx.storedDraft.WarningAt = tx.now.Add(83 * 24 * time.Hour)
				tx.storedDraft.ExpiresAt = tx.now.Add(90 * 24 * time.Hour)
				*(dest[0].(*time.Time)) = tx.storedDraft.UpdatedAt
				*(dest[1].(*time.Time)) = tx.storedDraft.WarningAt
				*(dest[2].(*time.Time)) = tx.storedDraft.ExpiresAt
				return nil
			}
			if len(args) < 9 {
				return pgx.ErrNoRows
			}
			payload, err := records.NewDraftPayload(args[2].([]byte))
			if err != nil {
				return err
			}
			version := uint64(args[4].(int64))
			etag, err := records.NewDraftETag(tx.storedDraft.DraftID, tx.storedDraft.AuthorID, version, payload)
			if err != nil {
				return err
			}
			tx.storedDraft.Payload = payload
			tx.storedDraft.Version = version
			tx.storedDraft.ETag = etag
			tx.storedDraft.UpdatedAt = tx.now
			tx.storedDraft.WarningAt = tx.now.Add(83 * 24 * time.Hour)
			tx.storedDraft.ExpiresAt = tx.now.Add(90 * 24 * time.Hour)
			*(dest[0].(*time.Time)) = tx.storedDraft.UpdatedAt
			*(dest[1].(*time.Time)) = tx.storedDraft.WarningAt
			*(dest[2].(*time.Time)) = tx.storedDraft.ExpiresAt
			return nil
		}
		if strings.Contains(compact, "from public.content_delivery_epochs") {
			*(dest[0].(*int64)) = 0
			return nil
		}
		if strings.Contains(compact, "from public.records") {
			*(dest[0].(*string)) = tx.currentRevisionID
			*(dest[1].(*string)) = tx.currentLifecycle
			return nil
		}
		return pgx.ErrNoRows
	}}
}

func (tx *fakeRecordDraftTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.querySQL = append(tx.querySQL, sql)
	tx.queryArgs = append(tx.queryArgs, append([]any(nil), args...))
	compact := strings.ToLower(sql)
	if len(args) == 3 && (strings.Contains(compact, "select draft_id") || strings.Contains(compact, "select drafts.draft_id")) &&
		strings.Contains(compact, "from public.record_drafts") {
		authorID, _ := args[0].(string)
		limit, _ := args[1].(int64)
		routings := make([]records.DraftRouting, 0, len(tx.routingDrafts))
		for _, draft := range tx.routingDrafts {
			if draft.AuthorID != authorID ||
				(strings.Contains(compact, "deletion_reservations") && tx.fencedRecordIDs[draft.RecordID]) ||
				int64(len(routings)) >= limit {
				continue
			}
			routings = append(routings, records.DraftRoutingFromDraft(draft))
		}
		return &fakeRecordDraftRows{routings: routings, routingScans: &tx.routingScans}, nil
	}
	draftIDs := make([]string, 0, len(tx.expiredDraftIDs))
	for _, draftID := range tx.expiredDraftIDs {
		recordID := tx.expiredDraftRecordIDs[draftID]
		if recordID != "" && strings.Contains(compact, "deletion_reservations") && tx.fencedRecordIDs[recordID] {
			continue
		}
		draftIDs = append(draftIDs, draftID)
	}
	return &fakeRecordDraftRows{
		draftIDs:              draftIDs,
		draftRecordIDs:        tx.expiredDraftRecordIDs,
		afterExpiredDraftScan: tx.afterExpiredDraftScan,
	}, nil
}

func scanFakeStoredDraft(draft records.Draft, dest ...any) error {
	*(dest[0].(*string)) = draft.DraftID
	*(dest[1].(*string)) = string(draft.ProjectID)
	if draft.RecordID == "" {
		*(dest[2].(**string)) = nil
		*(dest[3].(**string)) = nil
	} else {
		recordID := draft.RecordID
		baseRevisionID := draft.BaseRevisionID
		*(dest[2].(**string)) = &recordID
		*(dest[3].(**string)) = &baseRevisionID
	}
	*(dest[4].(*string)) = draft.AuthorID
	*(dest[5].(*[]byte)) = draft.Payload.JSON()
	payloadHash := draft.Payload.Hash()
	*(dest[6].(*[]byte)) = append([]byte(nil), payloadHash[:]...)
	*(dest[7].(*int64)) = int64(draft.Version)
	etagDigest, err := draft.ETag.Digest()
	if err != nil {
		return err
	}
	*(dest[8].(*[]byte)) = append([]byte(nil), etagDigest[:]...)
	*(dest[9].(*time.Time)) = draft.WarningAt
	*(dest[10].(*time.Time)) = draft.CreatedAt
	*(dest[11].(*time.Time)) = draft.UpdatedAt
	*(dest[12].(*time.Time)) = draft.ExpiresAt
	return nil
}

func scanFakeStoredDraftRouting(draft records.Draft, dest ...any) error {
	*(dest[0].(*string)) = draft.DraftID
	*(dest[1].(*string)) = string(draft.ProjectID)
	if draft.RecordID == "" {
		*(dest[2].(**string)) = nil
		*(dest[3].(**string)) = nil
	} else {
		recordID := draft.RecordID
		baseRevisionID := draft.BaseRevisionID
		*(dest[2].(**string)) = &recordID
		*(dest[3].(**string)) = &baseRevisionID
	}
	*(dest[4].(*string)) = draft.AuthorID
	*(dest[5].(*time.Time)) = draft.UpdatedAt
	return nil
}

func (tx *fakeRecordDraftTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.execSQL = append(tx.execSQL, sql)
	if strings.Contains(strings.ToLower(sql), "delete from public.record_drafts") && len(args) == 1 {
		if draftIDs, ok := args[0].([]string); ok {
			return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", len(draftIDs))), nil
		}
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (tx *fakeRecordDraftTx) allSQL() []string {
	result := append([]string(nil), tx.querySQL...)
	return append(result, tx.execSQL...)
}

type fakeRecordDraftRow struct {
	scan func(...any) error
}

type fakeRecordDraftRows struct {
	draftIDs              []string
	draftRecordIDs        map[string]string
	afterExpiredDraftScan func()
	routings              []records.DraftRouting
	routingScans          *int
	index                 int
}

func (*fakeRecordDraftRows) Close() {}
func (*fakeRecordDraftRows) Err() error {
	return nil
}
func (*fakeRecordDraftRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}
func (*fakeRecordDraftRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}
func (*fakeRecordDraftRows) RawValues() [][]byte {
	return nil
}
func (*fakeRecordDraftRows) Values() ([]any, error) {
	return nil, nil
}
func (*fakeRecordDraftRows) Conn() *pgx.Conn {
	return nil
}
func (rows *fakeRecordDraftRows) Next() bool {
	length := len(rows.draftIDs)
	if rows.routings != nil {
		length = len(rows.routings)
	}
	if rows.index >= length {
		return false
	}
	rows.index++
	return true
}
func (rows *fakeRecordDraftRows) Scan(dest ...any) error {
	if rows.routings != nil {
		if len(dest) != 6 || rows.index == 0 || rows.index > len(rows.routings) {
			return errors.New("invalid record draft routing scan")
		}
		routing := rows.routings[rows.index-1]
		if rows.routingScans != nil {
			*rows.routingScans++
		}
		*(dest[0].(*string)) = routing.DraftID
		*(dest[1].(*string)) = string(routing.ProjectID)
		if routing.RecordID == "" {
			*(dest[2].(**string)) = nil
			*(dest[3].(**string)) = nil
		} else {
			recordID := routing.RecordID
			baseRevisionID := routing.BaseRevisionID
			*(dest[2].(**string)) = &recordID
			*(dest[3].(**string)) = &baseRevisionID
		}
		*(dest[4].(*string)) = routing.AuthorID
		*(dest[5].(*time.Time)) = routing.UpdatedAt
		return nil
	}
	if (len(dest) != 1 && len(dest) != 2) || rows.index == 0 || rows.index > len(rows.draftIDs) {
		return errors.New("invalid record draft cleanup scan")
	}
	draftID := rows.draftIDs[rows.index-1]
	*(dest[0].(*string)) = draftID
	if len(dest) == 2 {
		recordID := rows.draftRecordIDs[draftID]
		if recordID == "" {
			*(dest[1].(**string)) = nil
		} else {
			*(dest[1].(**string)) = &recordID
		}
	}
	if rows.index == len(rows.draftIDs) && rows.afterExpiredDraftScan != nil {
		rows.afterExpiredDraftScan()
		rows.afterExpiredDraftScan = nil
	}
	return nil
}

func (row fakeRecordDraftRow) Scan(dest ...any) error {
	return row.scan(dest...)
}

func sqlIndexContaining(values []string, fragment string) int {
	for index, value := range values {
		if strings.Contains(strings.ToLower(value), strings.ToLower(fragment)) {
			return index
		}
	}
	return -1
}

func sqlCountContaining(values []string, fragment string) int {
	count := 0
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), strings.ToLower(fragment)) {
			count++
		}
	}
	return count
}
