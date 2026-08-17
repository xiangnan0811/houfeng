package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/auth"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
)

// CollaborationMembershipReader resolves one assignment candidate from the
// caller-owned transaction. Access-group rows contribute authorization scope
// only after the users row has independently proved membership.
type CollaborationMembershipReader interface {
	ReadMemberActor(context.Context, pgx.Tx, recordauth.ProjectID, string) (recordauth.ActorScope, error)
}

type postgresCollaborationMembershipReader struct{}

func NewPostgresCollaborationMembershipReader() CollaborationMembershipReader {
	return &postgresCollaborationMembershipReader{}
}

func (reader *postgresCollaborationMembershipReader) ReadMemberActor(
	ctx context.Context,
	tx pgx.Tx,
	projectID recordauth.ProjectID,
	userID string,
) (recordauth.ActorScope, error) {
	if reader == nil {
		return recordauth.ActorScope{}, recordcollaboration.ErrMembershipUnavailable
	}
	if ctx == nil || recordauth.ValidateProjectID(projectID) != nil ||
		projectID != recordauth.ProjectIDDefault || recordauth.ValidateActorUserID(userID) != nil {
		return recordauth.ActorScope{}, recordcollaboration.ErrMembershipDenied
	}
	if nilCollaborationMembershipTx(tx) {
		return recordauth.ActorScope{}, recordcollaboration.ErrMembershipUnavailable
	}

	var role string
	err := tx.QueryRow(ctx, `
		select role
		from public.users
		where user_id = $1`, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return recordauth.ActorScope{}, recordcollaboration.ErrMembershipDenied
	}
	if err != nil {
		return recordauth.ActorScope{}, fmt.Errorf("%w: read persisted role: %w", recordcollaboration.ErrMembershipUnavailable, err)
	}
	if role != auth.RoleAdmin {
		return recordauth.ActorScope{}, recordcollaboration.ErrMembershipDenied
	}

	groups, err := newPostgresRecordAuthorizationRepository(tx).ListActorGroupIDs(ctx, projectID, userID)
	if err != nil {
		return recordauth.ActorScope{}, fmt.Errorf("%w: read authorization groups: %w", recordcollaboration.ErrMembershipUnavailable, err)
	}
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: userID, Role: recordauth.RoleProjectAdmin, ProjectID: projectID, GroupIDs: groups,
	})
	if err != nil {
		return recordauth.ActorScope{}, fmt.Errorf("%w: normalize member actor", recordcollaboration.ErrMembershipUnavailable)
	}
	return actor, nil
}

func nilCollaborationMembershipTx(tx pgx.Tx) bool {
	if tx == nil {
		return true
	}
	value := reflect.ValueOf(tx)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ CollaborationMembershipReader = (*postgresCollaborationMembershipReader)(nil)
