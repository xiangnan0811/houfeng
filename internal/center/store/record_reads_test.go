package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/records"
)

func TestPostgresRecordReadRequiresAdmissionBeforeAnyFenceOrContentRead(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordReadFenceTx{}
	repository := &PostgresRecordRepository{platform: &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error {
			return ErrRecordPlatformAdmissionUnavailable
		}),
	}}

	_, err := repository.ReadRecordRevision(context.Background(), records.StoredRecordRevisionRequest{
		RecordID:           "rec_read1",
		RevisionID:         "rrv_current1",
		CurrentRevisionID:  "rrv_current1",
		LockVersion:        7,
		AuthorizationEpoch: 5,
	})
	if !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("ReadRecordRevision() error = %v, want admission unavailable", err)
	}
	if tx.queryRowCalls != 0 || tx.queryCalls != 0 || tx.commitCalls != 0 {
		t.Fatalf("database calls after denied admission = row:%d rows:%d commit:%d", tx.queryRowCalls, tx.queryCalls, tx.commitCalls)
	}
}

func TestPostgresRecordReadRejectsDeletionReservationBeforeContentRead(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordReadFenceTx{reservationState: "fenced"}
	repository := &PostgresRecordRepository{platform: &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    AdmissionGateFunc(func(context.Context, pgx.Tx) error { return nil }),
	}}

	_, err := repository.ReadRecordRevision(context.Background(), records.StoredRecordRevisionRequest{
		RecordID:           "rec_read1",
		RevisionID:         "rrv_current1",
		CurrentRevisionID:  "rrv_current1",
		LockVersion:        7,
		AuthorizationEpoch: 5,
	})
	if !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ReadRecordRevision() error = %v, want ErrRecordDeletionReserved", err)
	}
	if tx.contentQueries != 0 || tx.commitCalls != 0 {
		t.Fatalf("reserved read reached content/commit = %d/%d", tx.contentQueries, tx.commitCalls)
	}
}

func TestPostgresRecordReadCandidateQueryExcludesReservedRecordsAtomically(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordReadFenceTx{}
	repository := &PostgresRecordRepository{platform: &PostgresRecordPlatformRepository{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
		gate:    allowRecordPlatformAdmissionGate,
	}}

	candidates, err := repository.ListRecordCandidates(context.Background(), records.RecordCandidatePage{
		Sort: records.RecordSortUpdatedDesc, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListRecordCandidates() error = %v", err)
	}
	if len(candidates) != 0 || tx.queryCalls != 1 {
		t.Fatalf("ListRecordCandidates() = %#v queries=%d", candidates, tx.queryCalls)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL), " "))
	for _, fragment := range []string{
		"not exists",
		"from public.deletion_reservations reservations",
		"reservations.object_id = records.record_id",
		"reservations.state in ('fenced', 'committed')",
	} {
		if !strings.Contains(compact, fragment) {
			t.Fatalf("candidate query missing %q:\n%s", fragment, tx.querySQL)
		}
	}
}

func TestLoadStoredRecordRevisionAttachmentIDsPreservesOrdinalOrder(t *testing.T) {
	t.Parallel()

	tx := &fakeRecordRevisionAttachmentReadTx{rows: &fakeRecordRevisionAttachmentRows{
		attachmentIDs: []string{"att_readfirst", "att_readsecond"},
	}}
	got, err := loadStoredRecordRevisionAttachmentIDs(context.Background(), tx, "rrv_readattachments")
	if err != nil {
		t.Fatalf("loadStoredRecordRevisionAttachmentIDs() error = %v", err)
	}
	if !reflect.DeepEqual(got, []string{"att_readfirst", "att_readsecond"}) {
		t.Fatalf("loadStoredRecordRevisionAttachmentIDs() = %#v", got)
	}
	compact := strings.ToLower(strings.Join(strings.Fields(tx.querySQL), " "))
	for _, fragment := range []string{
		"from public.record_revision_attachments",
		"where revision_id = $1",
		"order by ordinal asc",
	} {
		if !strings.Contains(compact, fragment) {
			t.Fatalf("attachment query missing %q: %s", fragment, tx.querySQL)
		}
	}
}

type fakeRecordReadFenceTx struct {
	pgx.Tx
	reservationState string
	queryRowCalls    int
	queryCalls       int
	contentQueries   int
	commitCalls      int
	querySQL         string
}

func (tx *fakeRecordReadFenceTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.queryRowCalls++
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	if strings.Contains(compact, "from public.deletion_reservations") {
		if tx.reservationState == "" {
			return fakeRecordReadRow{err: pgx.ErrNoRows}
		}
		return fakeRecordReadRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = tx.reservationState
			return nil
		}}
	}
	if strings.Contains(compact, "from public.content_delivery_epochs") {
		return fakeRecordReadRow{scan: func(dest ...any) error {
			*(dest[0].(*int64)) = 0
			return nil
		}}
	}
	if strings.Contains(compact, "from public.deletion_fence_leases") {
		return fakeRecordReadRow{err: pgx.ErrNoRows}
	}
	tx.contentQueries++
	return fakeRecordReadRow{err: errors.New("unexpected content query")}
}

func (tx *fakeRecordReadFenceTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	tx.queryCalls++
	tx.querySQL = sql
	if strings.Contains(strings.ToLower(sql), "from public.records") {
		return &fakeRecordDraftRows{}, nil
	}
	tx.contentQueries++
	return nil, errors.New("unexpected content rows query")
}

func (tx *fakeRecordReadFenceTx) Commit(context.Context) error {
	tx.commitCalls++
	return nil
}

func (*fakeRecordReadFenceTx) Rollback(context.Context) error { return nil }

func (*fakeRecordReadFenceTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

type fakeRecordReadRow struct {
	scan func(...any) error
	err  error
}

type fakeRecordRevisionAttachmentReadTx struct {
	pgx.Tx
	rows     pgx.Rows
	querySQL string
}

func (tx *fakeRecordRevisionAttachmentReadTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	tx.querySQL = sql
	return tx.rows, nil
}

type fakeRecordRevisionAttachmentRows struct {
	attachmentIDs []string
	index         int
}

func (*fakeRecordRevisionAttachmentRows) Close()                                       {}
func (*fakeRecordRevisionAttachmentRows) Err() error                                   { return nil }
func (*fakeRecordRevisionAttachmentRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (*fakeRecordRevisionAttachmentRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (*fakeRecordRevisionAttachmentRows) RawValues() [][]byte                          { return nil }
func (*fakeRecordRevisionAttachmentRows) Values() ([]any, error)                       { return nil, nil }
func (*fakeRecordRevisionAttachmentRows) Conn() *pgx.Conn                              { return nil }
func (rows *fakeRecordRevisionAttachmentRows) Next() bool {
	if rows.index >= len(rows.attachmentIDs) {
		return false
	}
	rows.index++
	return true
}
func (rows *fakeRecordRevisionAttachmentRows) Scan(dest ...any) error {
	if len(dest) != 2 || rows.index == 0 || rows.index > len(rows.attachmentIDs) {
		return errors.New("invalid record revision attachment scan")
	}
	*(dest[0].(*int64)) = int64(rows.index - 1)
	*(dest[1].(*string)) = rows.attachmentIDs[rows.index-1]
	return nil
}

func (row fakeRecordReadRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	return row.scan(dest...)
}
