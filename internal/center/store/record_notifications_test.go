package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
)

func TestQueryInboxCandidatePageUsesStableKeysetAndSQLLimit(t *testing.T) {
	cursor := inboxQueryCursor{
		EventAt:        time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC),
		NotificationID: "rnt_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	tx := &inboxCandidateQueryTx{}
	items, err := queryInboxCandidatePage(context.Background(), tx, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", inboxQueryUnread, cursor, 17)
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("queryInboxCandidatePage() = (%#v, %v), want nonnil empty", items, err)
	}
	for _, fragment := range []string{
		"recipients.read_at is null", "recipients.dismissed_at is null",
		"notifications.event_at < $2", "notifications.event_at = $2", "notifications.notification_id > $3",
		"order by notifications.event_at desc, notifications.notification_id", "limit $4",
	} {
		if !strings.Contains(tx.sql, fragment) {
			t.Fatalf("inbox page SQL missing %q:\n%s", fragment, tx.sql)
		}
	}
	wantArgs := []any{"usr_aaaaaaaaaaaaaaaaaaaaaaaa", cursor.EventAt, cursor.NotificationID, 17}
	if !reflect.DeepEqual(tx.args, wantArgs) {
		t.Fatalf("inbox page args = %#v, want %#v", tx.args, wantArgs)
	}
}

func TestAuthorizeInboxSubjectIdentitySeparatesMissingFromDependencyFailure(t *testing.T) {
	dependencyErr := errors.New("subject query transport unavailable")
	candidate := inboxCandidate{item: recordcollaboration.InboxItem{
		RecordID: "rec_inbox", SubjectKind: recordcollaboration.NotificationSubjectAction, SubjectID: "ract_inbox",
	}}
	for _, test := range []struct {
		name    string
		scan    func(...any) error
		wantErr error
	}{
		{name: "missing", scan: func(...any) error { return pgx.ErrNoRows }, wantErr: recordcollaboration.ErrInboxNotFound},
		{name: "dependency failure", scan: func(...any) error { return dependencyErr }, wantErr: dependencyErr},
		{name: "malformed present value", scan: func(dest ...any) error { *(dest[0].(*int)) = 0; return nil }, wantErr: recordcollaboration.ErrInboxNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := inboxSubjectQueryTx{Tx: &fakeRecordPlatformTx{}, row: fakeRecordPlatformRow{scan: test.scan}}
			err := authorizeInboxSubjectIdentity(context.Background(), tx, candidate, 0)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("authorizeInboxSubjectIdentity() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == dependencyErr && errors.Is(err, recordcollaboration.ErrInboxNotFound) {
				t.Fatalf("dependency failure collapsed to opaque not found: %v", err)
			}
		})
	}
}

func TestScanInboxCandidateRejectsCorruptNonCanonicalPersistedNotificationID(t *testing.T) {
	eventAt := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
	_, err := scanInboxCandidate(fakeRecordPlatformRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = "rnt_short"
		*(dest[1].(*string)) = "rec_inbox"
		*(dest[2].(*string)) = string(recordcollaboration.NotificationEventActionAssigned)
		*(dest[3].(*string)) = string(recordcollaboration.NotificationSubjectAction)
		*(dest[4].(*string)) = "ract_inbox"
		*(dest[5].(*int64)) = 1
		*(dest[6].(*string)) = string(recordcollaboration.NotificationReasonAssignee)
		*(dest[7].(*bool)) = true
		*(dest[8].(*time.Time)) = eventAt
		*(dest[9].(**time.Time)) = nil
		*(dest[10].(**time.Time)) = nil
		*(dest[11].(*int64)) = 1
		*(dest[12].(*int64)) = 0
		return nil
	}})
	if !errors.Is(err, recordcollaboration.ErrInvalidInboxRequest) {
		t.Fatalf("scan corrupt notification id error = %v, want ErrInvalidInboxRequest", err)
	}
}

type inboxSubjectQueryTx struct {
	pgx.Tx
	row pgx.Row
}

func (tx inboxSubjectQueryTx) QueryRow(context.Context, string, ...any) pgx.Row { return tx.row }

type inboxCandidateQueryTx struct {
	pgx.Tx
	sql  string
	args []any
}

func (tx *inboxCandidateQueryTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	tx.sql = sql
	tx.args = append([]any(nil), args...)
	return emptyInboxRows{}, nil
}

type emptyInboxRows struct{ pgx.Rows }

func (emptyInboxRows) Close()     {}
func (emptyInboxRows) Err() error { return nil }
func (emptyInboxRows) Next() bool { return false }
