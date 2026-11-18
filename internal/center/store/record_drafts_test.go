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
	want := records.DraftRouting{
		DraftID: tx.storedDraft.DraftID, ProjectID: recordauth.ProjectIDDefault,
		RecordID: tx.storedDraft.RecordID, BaseRevisionID: tx.storedDraft.BaseRevisionID,
		AuthorID: tx.storedDraft.AuthorID, UpdatedAt: now,
	}
	if routing != want || routing.Validate() != nil {
		t.Fatalf("GetDraftRouting() = %#v, want %#v", routing, want)
	}
	foreign, err := repository.GetDraftRouting(context.Background(), tx.storedDraft.DraftID, "usr_89abcdef0123456701234567")
	if !errors.Is(err, records.ErrDraftNotFound) || foreign != (records.DraftRouting{}) {
		t.Fatalf("GetDraftRouting(foreign) = (%#v, %v), want opaque draft not found", foreign, err)
	}
	tx.gatedRecordIDs = map[string]bool{tx.storedDraft.RecordID: true}
	gated, err := repository.GetDraftRouting(context.Background(), tx.storedDraft.DraftID, tx.storedDraft.AuthorID)
	if !errors.Is(err, records.ErrRecordDeletionReserved) || gated != (records.DraftRouting{}) {
		t.Fatalf("GetDraftRouting(gated) = (%#v, %v), want deletion reserved without metadata", gated, err)
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

	routing, err := repository.GetDraftRouting(context.Background(), tx.storedDraft.DraftID, ownerID)
	if !errors.Is(err, records.ErrDraftNotFound) || routing != (records.DraftRouting{}) {
		t.Fatalf("GetDraftRouting(reserved) = (%#v, %v), want opaque draft not found", routing, err)
	}
}

func TestPostgresRecordDraftRepositoryListsAuthorRoutingMetadataInStableOrderAndSkipsFencedRecords(t *testing.T) {
	now := time.Date(2026, time.August, 3, 13, 0, 0, 0, time.UTC)
	ownerID := "usr_0123456789abcdef01234567"
	tx := newFakeRecordDraftTx(now)
	tx.routingDrafts = []records.Draft{
		{
			DraftID:   "rdf_0000000000000005",
			ProjectID: recordauth.ProjectIDDefault,
			AuthorID:  "usr_89abcdef0123456701234567",
			UpdatedAt: now.Add(time.Minute),
		},
		{
			DraftID:   "rdf_0000000000000003",
			ProjectID: recordauth.ProjectIDDefault,
			AuthorID:  ownerID,
			UpdatedAt: now,
		},
		{
			DraftID:        "rdf_0000000000000004",
			ProjectID:      recordauth.ProjectIDDefault,
			RecordID:       "rec_0000000000000004",
			BaseRevisionID: "rrv_0000000000000004",
			AuthorID:       ownerID,
			UpdatedAt:      now.Add(-30 * time.Second),
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
	tx.gatedRecordIDs = map[string]bool{"rec_0000000000000004": true}
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	routings, err := repository.ListDraftRoutings(context.Background(), ownerID, records.DraftCandidatePage{Limit: 5})
	if err != nil {
		t.Fatalf("ListDraftRoutings() error = %v", err)
	}
	want := []records.DraftRouting{
		{DraftID: "rdf_0000000000000003", ProjectID: recordauth.ProjectIDDefault, AuthorID: ownerID, UpdatedAt: now},
		{DraftID: "rdf_0000000000000001", ProjectID: recordauth.ProjectIDDefault, RecordID: "rec_0000000000000001",
			BaseRevisionID: "rrv_0000000000000001", AuthorID: ownerID, UpdatedAt: now.Add(-2 * time.Minute)},
	}
	if len(routings) != len(want) || routings[0] != want[0] || routings[1] != want[1] {
		t.Fatalf("ListDraftRoutings() = %#v, want %#v", routings, want)
	}
}

// The keyset predicate has to compare the same tuple the ordering uses. A filter
// on updated_at alone would drop drafts that share a timestamp, and a different
// ordering would make the index useless.
func TestPostgresRecordDraftRepositoryResumesFromCursorOnTheOrderedTuple(t *testing.T) {
	now := time.Date(2026, time.August, 3, 13, 0, 0, 0, time.UTC)
	ownerID := "usr_0123456789abcdef01234567"
	tx := newFakeRecordDraftTx(now)
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }

	after := records.DraftCursor{UpdatedAt: now, DraftID: "rdf_0000000000000003"}
	if _, err := repository.ListDraftRoutings(context.Background(), ownerID, records.DraftCandidatePage{
		After: &after, Limit: 3,
	}); err != nil {
		t.Fatalf("ListDraftRoutings() error = %v", err)
	}
	listSQL := strings.ToLower(strings.Join(strings.Fields(tx.querySQL[0]), " "))
	if !strings.Contains(listSQL, "(drafts.updated_at, drafts.draft_id) < ($4::timestamptz, $5::text)") {
		t.Fatalf("routing list SQL missing the ordered-tuple keyset predicate:\n%s", listSQL)
	}
	args := tx.queryArgs[0]
	if len(args) != 5 || args[3] != now.UTC() || args[4] != after.DraftID {
		t.Fatalf("routing list cursor args = %#v", args)
	}
}

// A candidate batch above the store's ceiling is a caller mistake, not something
// to silently clamp: a clamped read would look drained and end the page early.
func TestPostgresRecordDraftRepositoryRejectsUnusableCandidatePages(t *testing.T) {
	ownerID := "usr_0123456789abcdef01234567"
	repository := NewPostgresRecordDraftRepository(nil, allowRecordPlatformAdmissionGate)
	repository.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		t.Fatal("ListDraftRoutings() opened a transaction for an unusable page")
		return nil, nil
	}
	for _, testCase := range []struct {
		name string
		page records.DraftCandidatePage
	}{
		{name: "zero limit", page: records.DraftCandidatePage{}},
		{name: "limit above ceiling", page: records.DraftCandidatePage{Limit: maxDraftRoutingCandidates + 1}},
		{
			name: "cursor without draft",
			page: records.DraftCandidatePage{
				Limit: 10, After: &records.DraftCursor{UpdatedAt: time.Now().UTC()},
			},
		},
		{
			name: "cursor without timestamp",
			page: records.DraftCandidatePage{
				Limit: 10, After: &records.DraftCursor{DraftID: "rdf_0000000000000003"},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := repository.ListDraftRoutings(
				context.Background(), ownerID, testCase.page,
			); !errors.Is(err, records.ErrInvalidDraftCommand) {
				t.Fatalf("ListDraftRoutings(%s) error = %v, want ErrInvalidDraftCommand", testCase.name, err)
			}
		})
	}
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

type fakeRecordDraftTx struct {
	*fakeRecordRevisionTx
	now               time.Time
	querySQL          []string
	execSQL           []string
	commitCalls       int
	rollbackCalls     int
	currentRevisionID string
	currentLifecycle  string
	storedDraft       *records.Draft
	routingDrafts     []records.Draft
	fencedRecordIDs   map[string]bool
	gatedRecordIDs    map[string]bool
	queryArgs         [][]any
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
		if strings.Contains(compact, "from public.record_deletion_gates") {
			if len(args) != 2 {
				return errors.New("invalid record deletion gate lookup")
			}
			recordID, ok := args[1].(string)
			if !ok {
				return errors.New("invalid record deletion gate record")
			}
			*(dest[0].(*bool)) = tx.gatedRecordIDs[recordID]
			return nil
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
	if len(args) == 5 && (strings.Contains(compact, "select draft_id") || strings.Contains(compact, "select drafts.draft_id")) &&
		strings.Contains(compact, "from public.record_drafts") {
		authorID, _ := args[0].(string)
		limit, _ := args[1].(int64)
		afterUpdatedAt, hasCursor := args[3].(time.Time)
		afterDraftID, _ := args[4].(string)
		routings := make([]records.DraftRouting, 0, len(tx.routingDrafts))
		for _, draft := range tx.routingDrafts {
			if draft.AuthorID != authorID ||
				(strings.Contains(compact, "deletion_reservations") && tx.fencedRecordIDs[draft.RecordID]) ||
				int64(len(routings)) >= limit {
				continue
			}
			// Mirrors the keyset predicate on the descending (updated_at, draft_id)
			// tuple so the fake cannot pass a query the database would reject.
			if hasCursor && (draft.UpdatedAt.After(afterUpdatedAt) ||
				(draft.UpdatedAt.Equal(afterUpdatedAt) && draft.DraftID >= afterDraftID)) {
				continue
			}
			routings = append(routings, records.DraftRoutingFromDraft(draft))
		}
		return &fakeRecordDraftRows{routings: routings}, nil
	}
	return &fakeRecordDraftRows{}, nil
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
	routings []records.DraftRouting
	index    int
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
	length := len(rows.routings)
	if rows.index >= length {
		return false
	}
	rows.index++
	return true
}
func (rows *fakeRecordDraftRows) Scan(dest ...any) error {
	if len(dest) != 6 || rows.index == 0 || rows.index > len(rows.routings) {
		return errors.New("invalid record draft routing scan")
	}
	routing := rows.routings[rows.index-1]
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
