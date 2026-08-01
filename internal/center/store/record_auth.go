package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
)

type recordAuthorizationQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// PostgresRecordAuthorizationRepository reads only stable access-group IDs
// needed to construct a trusted recordauth.ActorScope.
type PostgresRecordAuthorizationRepository struct {
	db recordAuthorizationQueryer
}

// NewPostgresRecordAuthorizationRepository constructs the production scope
// repository from the already-admitted APP runtime pool.
func NewPostgresRecordAuthorizationRepository(pool *pgxpool.Pool) *PostgresRecordAuthorizationRepository {
	return newPostgresRecordAuthorizationRepository(pool)
}

func newPostgresRecordAuthorizationRepository(db recordAuthorizationQueryer) *PostgresRecordAuthorizationRepository {
	return &PostgresRecordAuthorizationRepository{db: db}
}

const recordAuthorizationGroupIDsSQL = `
select g.group_id
from public.record_access_groups g
join public.record_access_group_members m on m.group_id = g.group_id
where g.project_id = $1 and m.user_id = $2
order by g.group_id asc`

// ListActorGroupIDs implements recordauth.ScopeRepository. It preserves the
// database's explicit stable ordering and rejects malformed persisted values
// before they can reach request context.
func (repository *PostgresRecordAuthorizationRepository) ListActorGroupIDs(ctx context.Context, projectID recordauth.ProjectID, userID string) ([]string, error) {
	if err := recordauth.ValidateProjectID(projectID); err != nil {
		return nil, err
	}
	if err := recordauth.ValidateActorUserID(userID); err != nil {
		return nil, err
	}
	if repository == nil || repository.db == nil {
		return nil, fmt.Errorf("record authorization repository unavailable")
	}

	rows, err := repository.db.Query(ctx, recordAuthorizationGroupIDsSQL, projectID, userID)
	if err != nil {
		return nil, fmt.Errorf("query record authorization groups: %w", err)
	}
	defer rows.Close()

	groupIDs := make([]string, 0)
	for rows.Next() {
		var groupID string
		if err := rows.Scan(&groupID); err != nil {
			return nil, fmt.Errorf("scan record authorization group: %w", err)
		}
		if err := recordauth.ValidateGroupID(groupID); err != nil {
			return nil, err
		}
		groupIDs = append(groupIDs, groupID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate record authorization groups: %w", err)
	}
	return groupIDs, nil
}

var _ recordauth.ScopeRepository = (*PostgresRecordAuthorizationRepository)(nil)
