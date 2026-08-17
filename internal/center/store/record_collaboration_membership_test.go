package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
)

const collaborationMemberTestUserID = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"

func TestPostgresCollaborationMembershipReaderAcceptsOnlyPersistedAdminInDefaultProject(t *testing.T) {
	t.Parallel()

	tx := &fakeCollaborationMembershipTx{role: auth.RoleAdmin, groupIDs: []string{"rag_beta", "rag_alpha", "rag_beta"}}
	reader := NewPostgresCollaborationMembershipReader()
	actor, err := reader.ReadMemberActor(context.Background(), tx, recordauth.ProjectIDDefault, collaborationMemberTestUserID)
	if err != nil {
		t.Fatalf("ReadMemberActor() error = %v", err)
	}
	if actor.UserID != collaborationMemberTestUserID || actor.ProjectID != recordauth.ProjectIDDefault || actor.Role != recordauth.RoleProjectAdmin {
		t.Fatalf("ReadMemberActor() = %#v", actor)
	}
	if got, want := actor.GroupIDs, []string{"rag_alpha", "rag_beta"}; !sameStrings(got, want) {
		t.Fatalf("actor groups = %#v, want %#v", got, want)
	}
	if tx.queryRowCalls != 1 || tx.queryCalls != 1 {
		t.Fatalf("transaction query calls = row:%d rows:%d, want 1/1", tx.queryRowCalls, tx.queryCalls)
	}
	if !strings.Contains(strings.ToLower(tx.queryRowSQL), "from public.users") ||
		strings.Contains(strings.ToLower(tx.queryRowSQL), "record_access_group") {
		t.Fatalf("membership SQL = %q, want users-only authority", tx.queryRowSQL)
	}
}

func TestPostgresCollaborationMembershipReaderMatrixFailsClosed(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("query unavailable")
	scanErr := errors.New("scan unavailable")
	groupErr := errors.New("group query unavailable")
	var typedNilTx *fakeCollaborationMembershipTx
	tests := []struct {
		name      string
		projectID recordauth.ProjectID
		userID    string
		tx        pgx.Tx
		wantErr   error
	}{
		{name: "missing user", projectID: recordauth.ProjectIDDefault, userID: collaborationMemberTestUserID, tx: &fakeCollaborationMembershipTx{missing: true}, wantErr: recordcollaboration.ErrMembershipDenied},
		{name: "other role", projectID: recordauth.ProjectIDDefault, userID: collaborationMemberTestUserID, tx: &fakeCollaborationMembershipTx{role: "viewer"}, wantErr: recordcollaboration.ErrMembershipDenied},
		{name: "unknown role", projectID: recordauth.ProjectIDDefault, userID: collaborationMemberTestUserID, tx: &fakeCollaborationMembershipTx{role: "future_role"}, wantErr: recordcollaboration.ErrMembershipDenied},
		{name: "other project", projectID: "other", userID: collaborationMemberTestUserID, tx: &fakeCollaborationMembershipTx{role: auth.RoleAdmin}, wantErr: recordcollaboration.ErrMembershipDenied},
		{name: "malformed user id", projectID: recordauth.ProjectIDDefault, userID: "usr_invalid", tx: &fakeCollaborationMembershipTx{role: auth.RoleAdmin}, wantErr: recordcollaboration.ErrMembershipDenied},
		{name: "query error", projectID: recordauth.ProjectIDDefault, userID: collaborationMemberTestUserID, tx: &fakeCollaborationMembershipTx{queryRowErr: queryErr}, wantErr: recordcollaboration.ErrMembershipUnavailable},
		{name: "scan error", projectID: recordauth.ProjectIDDefault, userID: collaborationMemberTestUserID, tx: &fakeCollaborationMembershipTx{scanErr: scanErr}, wantErr: recordcollaboration.ErrMembershipUnavailable},
		{name: "group error", projectID: recordauth.ProjectIDDefault, userID: collaborationMemberTestUserID, tx: &fakeCollaborationMembershipTx{role: auth.RoleAdmin, queryErr: groupErr}, wantErr: recordcollaboration.ErrMembershipUnavailable},
		{name: "nil transaction", projectID: recordauth.ProjectIDDefault, userID: collaborationMemberTestUserID, tx: nil, wantErr: recordcollaboration.ErrMembershipUnavailable},
		{name: "typed nil transaction", projectID: recordauth.ProjectIDDefault, userID: collaborationMemberTestUserID, tx: typedNilTx, wantErr: recordcollaboration.ErrMembershipUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := NewPostgresCollaborationMembershipReader()
			_, err := reader.ReadMemberActor(context.Background(), tt.tx, tt.projectID, tt.userID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReadMemberActor() error = %v, want errors.Is(%v)", err, tt.wantErr)
			}
		})
	}
}

func TestPostgresCollaborationMembershipReaderTypedNilReceiverFailsClosed(t *testing.T) {
	t.Parallel()

	var reader *postgresCollaborationMembershipReader
	tx := &fakeCollaborationMembershipTx{role: auth.RoleAdmin}
	_, err := reader.ReadMemberActor(context.Background(), tx, recordauth.ProjectIDDefault, collaborationMemberTestUserID)
	if !errors.Is(err, recordcollaboration.ErrMembershipUnavailable) {
		t.Fatalf("ReadMemberActor() error = %v, want errors.Is(%v)", err, recordcollaboration.ErrMembershipUnavailable)
	}
	if tx.queryRowCalls != 0 || tx.queryCalls != 0 {
		t.Fatalf("typed-nil reader used transaction: row:%d rows:%d", tx.queryRowCalls, tx.queryCalls)
	}
}

type fakeCollaborationMembershipTx struct {
	pgx.Tx
	role          string
	missing       bool
	queryRowErr   error
	scanErr       error
	queryErr      error
	groupIDs      []string
	queryRowCalls int
	queryCalls    int
	queryRowSQL   string
}

func (tx *fakeCollaborationMembershipTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.queryRowCalls++
	tx.queryRowSQL = sql
	if tx.queryRowErr != nil {
		return fakeRecordReadRow{err: tx.queryRowErr}
	}
	if tx.missing {
		return fakeRecordReadRow{err: pgx.ErrNoRows}
	}
	if tx.scanErr != nil {
		return fakeRecordReadRow{err: tx.scanErr}
	}
	return fakeRecordReadRow{scan: func(dest ...any) error {
		*(dest[0].(*string)) = tx.role
		return nil
	}}
}

func (tx *fakeCollaborationMembershipTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	tx.queryCalls++
	if tx.queryErr != nil {
		return nil, tx.queryErr
	}
	scans := make([]fakeRecordAuthorizationScan, 0, len(tx.groupIDs))
	for _, groupID := range tx.groupIDs {
		scans = append(scans, recordAuthorizationGroupIDScan(groupID))
	}
	return &fakeRecordAuthorizationRows{scans: scans}, nil
}
