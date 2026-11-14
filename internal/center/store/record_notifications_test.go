package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
)

// This private fixture rejects preparation while discovery is still open.
type notificationUnitPrefetcher struct {
	tx  *fakeRecordPlatformTx
	err error
}

func (fixture notificationUnitPrefetcher) prepareCurrentAuthorization(_ context.Context, recordIDs []string) (currentRecordAuthorizationTransactionResolver, error) {
	if !fixture.tx.committed || len(recordIDs) != 1 || recordIDs[0] != "rec_notification" {
		return nil, errors.New("invalid notification discovery")
	}
	return nil, fixture.err
}

func TestNotificationPreparationFailsClosedWhenPrefetchUnavailable(t *testing.T) {
	unavailable := errors.New("source prefetch unavailable")
	tx := &fakeRecordPlatformTx{queryRow: func(context.Context, string, ...any) pgx.Row {
		return fakeRecordPlatformRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = "rev_notification"
			*(dest[1].(*string)) = "rec_notification"
			*(dest[2].(*string)) = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
			*(dest[3].(**string)) = nil
			*(dest[4].(*time.Time)) = time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)
			return nil
		}}
	}}
	repository := &PostgresRecordNotificationRepository{
		platform: &PostgresRecordPlatformRepository{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			gate:    allowRecordPlatformAdmissionGate,
		},
		authorization: notificationUnitPrefetcher{tx: tx, err: unavailable},
	}
	event := recordplatform.OutboxEvent{
		ProjectID: string(recordplatform.ProjectIDDefault), SubjectKind: recordplatform.OutboxSubjectKindRecord,
		SubjectID: "rec_notification", SourceVersion: 1, AuthorizationEpoch: 1,
	}
	_, _, err := repository.prepareNotificationAuthorization(context.Background(), event, recordcollaboration.NotificationEventRecordOwnerChanged)
	if !errors.Is(err, unavailable) {
		t.Fatalf("prepareNotificationAuthorization error = %v, want prefetch unavailable", err)
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
