package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/accessadmin"
	"houfeng/internal/center/auth"
)

// PostgresAccessAdminRepository owns the transactional account and
// record-access-group management operations. It is constructed with the APP
// runtime pool; authorization is checked again inside every write transaction.
type PostgresAccessAdminRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresAccessAdminRepository(pool *pgxpool.Pool) *PostgresAccessAdminRepository {
	return &PostgresAccessAdminRepository{pool: pool}
}

func (r *PostgresAccessAdminRepository) AuthorizeSupervisor(ctx context.Context, actorID string) error {
	if r == nil || r.pool == nil {
		return accessadmin.ErrManagementUnavailable
	}
	var role string
	var supervisor bool
	var disabledAt *time.Time
	err := r.pool.QueryRow(ctx, `
		select role, is_supervisor, disabled_at
		from users
		where user_id = $1`, actorID).Scan(&role, &supervisor, &disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return accessadmin.ErrManagementForbidden
	}
	if err != nil {
		return unavailableAccessAdminError("authorize supervisor", err)
	}
	if role != auth.RoleAdmin || !supervisor || disabledAt != nil {
		return accessadmin.ErrManagementForbidden
	}
	return nil
}

func (r *PostgresAccessAdminRepository) AuthorizeActiveUser(ctx context.Context, actorID string) error {
	if r == nil || r.pool == nil {
		return accessadmin.ErrManagementUnavailable
	}
	var userID string
	err := r.pool.QueryRow(ctx, `
		select user_id
		from users
		where user_id = $1 and disabled_at is null`, actorID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return accessadmin.ErrManagementForbidden
	}
	if err != nil {
		return unavailableAccessAdminError("authorize active user", err)
	}
	return nil
}

func (r *PostgresAccessAdminRepository) ListUsers(ctx context.Context, actorID string) ([]accessadmin.UserSummary, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		select user_id, username, display_name, role, is_supervisor, disabled_at, created_at
		from users
		order by username asc, user_id asc`)
	if err != nil {
		return nil, unavailableAccessAdminError("list users", err)
	}
	users := make([]accessadmin.UserSummary, 0)
	for rows.Next() {
		var user accessadmin.UserSummary
		if err := rows.Scan(
			&user.UserID,
			&user.Username,
			&user.DisplayName,
			&user.Role,
			&user.IsSupervisor,
			&user.DisabledAt,
			&user.CreatedAt,
		); err != nil {
			rows.Close()
			return nil, unavailableAccessAdminError("scan users", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, unavailableAccessAdminError("iterate users", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailableAccessAdminError("commit list users", err)
	}
	return users, nil
}

func (r *PostgresAccessAdminRepository) CreateUser(ctx context.Context, actorID string, user auth.User) (accessadmin.UserSummary, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return accessadmin.UserSummary{}, err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return accessadmin.UserSummary{}, err
	}
	row := tx.QueryRow(ctx, `
		insert into users (
			user_id, username, password_hash, display_name, role,
			is_supervisor, disabled_at, created_at, password_changed_at
		) values ($1, $2, $3, $4, 'admin', false, null, $5, $6)
		returning user_id, username, display_name, role, is_supervisor, disabled_at, created_at`,
		user.UserID,
		user.Username,
		user.PasswordHash,
		user.DisplayName,
		user.CreatedAt,
		user.PasswordChangedAt,
	)
	created, err := scanAccessAdminUserSummary(row)
	if err != nil {
		return accessadmin.UserSummary{}, mapAccessAdminWriteError("create user", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return accessadmin.UserSummary{}, unavailableAccessAdminError("commit create user", err)
	}
	return created, nil
}

func (r *PostgresAccessAdminRepository) SetUserEnabled(ctx context.Context, actorID, targetID string, enabled bool, now time.Time) (accessadmin.UserSummary, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return accessadmin.UserSummary{}, err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return accessadmin.UserSummary{}, err
	}
	var isSupervisor bool
	if targetID == actorID {
		// lockAccessAdminSupervisor already locked and validated this row.
		isSupervisor = true
	} else {
		err = tx.QueryRow(ctx, `
			select is_supervisor
			from users
			where user_id = $1
			for update`, targetID).Scan(&isSupervisor)
		if errors.Is(err, pgx.ErrNoRows) {
			return accessadmin.UserSummary{}, accessadmin.ErrResourceNotFound
		}
		if err != nil {
			return accessadmin.UserSummary{}, unavailableAccessAdminError("lock target user", err)
		}
	}
	if isSupervisor && !enabled {
		return accessadmin.UserSummary{}, accessadmin.ErrSupervisorProtected
	}
	if enabled {
		if _, err := tx.Exec(ctx, `update users set disabled_at = null where user_id = $1`, targetID); err != nil {
			return accessadmin.UserSummary{}, unavailableAccessAdminError("enable user", err)
		}
	} else {
		// Keep the original disabled timestamp on repeated disable requests.
		if _, err := tx.Exec(ctx, `
			update users
			set disabled_at = coalesce(disabled_at, $2)
			where user_id = $1`, targetID, now.UTC()); err != nil {
			return accessadmin.UserSummary{}, unavailableAccessAdminError("disable user", err)
		}
		if _, err := tx.Exec(ctx, `delete from sessions where user_id = $1`, targetID); err != nil {
			return accessadmin.UserSummary{}, unavailableAccessAdminError("revoke disabled user sessions", err)
		}
	}
	row := tx.QueryRow(ctx, `
		select user_id, username, display_name, role, is_supervisor, disabled_at, created_at
		from users where user_id = $1`, targetID)
	updated, err := scanAccessAdminUserSummary(row)
	if err != nil {
		return accessadmin.UserSummary{}, unavailableAccessAdminError("read updated user", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return accessadmin.UserSummary{}, unavailableAccessAdminError("commit user state", err)
	}
	return updated, nil
}

func (r *PostgresAccessAdminRepository) ResetUserPassword(ctx context.Context, actorID, targetID, newHash string, now time.Time) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return err
	}
	if targetID == actorID {
		// lockAccessAdminSupervisor already proved this is the protected row.
		return accessadmin.ErrSupervisorProtected
	}
	var isSupervisor bool
	err = tx.QueryRow(ctx, `
		select is_supervisor
		from users
		where user_id = $1
		for update`, targetID).Scan(&isSupervisor)
	if errors.Is(err, pgx.ErrNoRows) {
		return accessadmin.ErrResourceNotFound
	}
	if err != nil {
		return unavailableAccessAdminError("lock password target", err)
	}
	if isSupervisor {
		return accessadmin.ErrSupervisorProtected
	}
	changedAt := now.UTC()
	if _, err := tx.Exec(ctx, `
		update users
		set password_hash = $2, password_changed_at = $3
		where user_id = $1`, targetID, newHash, changedAt); err != nil {
		return unavailableAccessAdminError("reset user password", err)
	}
	if _, err := tx.Exec(ctx, `delete from sessions where user_id = $1`, targetID); err != nil {
		return unavailableAccessAdminError("revoke reset user sessions", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailableAccessAdminError("commit reset user password", err)
	}
	return nil
}

func (r *PostgresAccessAdminRepository) ListGroups(ctx context.Context, actorID string) ([]accessadmin.GroupSummary, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		select group_id, display_name
		from public.record_access_groups
		where project_id = 'default'
		order by display_name asc, group_id asc`)
	if err != nil {
		return nil, unavailableAccessAdminError("list record access groups", err)
	}
	groups := make([]accessadmin.GroupSummary, 0)
	for rows.Next() {
		var group accessadmin.GroupSummary
		if err := rows.Scan(&group.GroupID, &group.DisplayName); err != nil {
			rows.Close()
			return nil, unavailableAccessAdminError("scan record access groups", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, unavailableAccessAdminError("iterate record access groups", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailableAccessAdminError("commit list record access groups", err)
	}
	return groups, nil
}

func (r *PostgresAccessAdminRepository) CreateGroup(ctx context.Context, actorID, groupID, displayName string, now time.Time) (accessadmin.GroupSummary, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return accessadmin.GroupSummary{}, err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return accessadmin.GroupSummary{}, err
	}
	row := tx.QueryRow(ctx, `
		insert into public.record_access_groups (group_id, project_id, display_name, created_at, updated_at)
		values ($1, 'default', $2, $3, $3)
		returning group_id, display_name`, groupID, displayName, now.UTC())
	var group accessadmin.GroupSummary
	if err := row.Scan(&group.GroupID, &group.DisplayName); err != nil {
		return accessadmin.GroupSummary{}, mapAccessAdminWriteError("create record access group", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return accessadmin.GroupSummary{}, unavailableAccessAdminError("commit create record access group", err)
	}
	return group, nil
}

func (r *PostgresAccessAdminRepository) RenameGroup(ctx context.Context, actorID, groupID, displayName string, now time.Time) (accessadmin.GroupSummary, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return accessadmin.GroupSummary{}, err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return accessadmin.GroupSummary{}, err
	}
	var lockedGroupID string
	err = tx.QueryRow(ctx, `
		select group_id
		from public.record_access_groups
		where group_id = $1 and project_id = 'default'
		for update`, groupID).Scan(&lockedGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return accessadmin.GroupSummary{}, accessadmin.ErrResourceNotFound
	}
	if err != nil {
		return accessadmin.GroupSummary{}, unavailableAccessAdminError("lock record access group", err)
	}
	row := tx.QueryRow(ctx, `
		update public.record_access_groups
		set display_name = $2, updated_at = $3
		where group_id = $1 and project_id = 'default'
		returning group_id, display_name`, groupID, displayName, now.UTC())
	var group accessadmin.GroupSummary
	if err := row.Scan(&group.GroupID, &group.DisplayName); err != nil {
		return accessadmin.GroupSummary{}, mapAccessAdminWriteError("rename record access group", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return accessadmin.GroupSummary{}, unavailableAccessAdminError("commit rename record access group", err)
	}
	_ = lockedGroupID
	return group, nil
}

func (r *PostgresAccessAdminRepository) ListMembers(ctx context.Context, actorID, groupID string) ([]accessadmin.MemberSummary, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return nil, err
	}
	var lockedGroupID string
	err = tx.QueryRow(ctx, `
		select group_id
		from public.record_access_groups
		where group_id = $1 and project_id = 'default'
		for update`, groupID).Scan(&lockedGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, accessadmin.ErrResourceNotFound
	}
	if err != nil {
		return nil, unavailableAccessAdminError("lock member group", err)
	}
	rows, err := tx.Query(ctx, `
		select m.user_id, u.username, u.display_name, u.role,
		       u.is_supervisor, u.disabled_at, u.created_at
		from public.record_access_group_members m
		left join users u on u.user_id = m.user_id
		where m.group_id = $1
		order by coalesce(u.username, ''), m.user_id`, groupID)
	if err != nil {
		return nil, unavailableAccessAdminError("list record access group members", err)
	}
	members := make([]accessadmin.MemberSummary, 0)
	for rows.Next() {
		var (
			memberID     string
			username     *string
			displayName  *string
			role         *string
			isSupervisor *bool
			disabledAt   *time.Time
			createdAt    *time.Time
		)
		if err := rows.Scan(&memberID, &username, &displayName, &role, &isSupervisor, &disabledAt, &createdAt); err != nil {
			rows.Close()
			return nil, unavailableAccessAdminError("scan record access group member", err)
		}
		member := accessadmin.MemberSummary{UserSummary: accessadmin.UserSummary{UserID: memberID}}
		if username == nil {
			member.Missing = true
		} else {
			member.Username = *username
			member.DisplayName = dereferenceString(displayName)
			member.Role = dereferenceString(role)
			if isSupervisor != nil {
				member.IsSupervisor = *isSupervisor
			}
			member.DisabledAt = disabledAt
			member.CreatedAt = dereferenceTime(createdAt)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, unavailableAccessAdminError("iterate record access group members", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailableAccessAdminError("commit list record access group members", err)
	}
	_ = lockedGroupID
	return members, nil
}

func (r *PostgresAccessAdminRepository) SetMember(ctx context.Context, actorID, groupID, userID string, present bool, now time.Time) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	if err := lockAccessAdminSupervisor(ctx, tx, actorID); err != nil {
		return err
	}
	if present && userID != actorID {
		var (
			role       string
			disabledAt *time.Time
		)
		err = tx.QueryRow(ctx, `
			select role, disabled_at
			from users
			where user_id = $1
			for update`, userID).Scan(&role, &disabledAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return accessadmin.ErrResourceNotFound
		}
		if err != nil {
			return unavailableAccessAdminError("lock membership target", err)
		}
		if disabledAt != nil {
			return accessadmin.ErrUserDisabled
		}
		if role != auth.RoleAdmin {
			return accessadmin.ErrResourceNotFound
		}
	}
	var lockedGroupID string
	err = tx.QueryRow(ctx, `
		select group_id
		from public.record_access_groups
		where group_id = $1 and project_id = 'default'
		for update`, groupID).Scan(&lockedGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return accessadmin.ErrResourceNotFound
	}
	if err != nil {
		return unavailableAccessAdminError("lock membership group", err)
	}
	if present {
		_, err = tx.Exec(ctx, `
			insert into public.record_access_group_members (group_id, user_id, created_at)
			values ($1, $2, $3)
			on conflict (group_id, user_id) do nothing`, groupID, userID, now.UTC())
	} else {
		_, err = tx.Exec(ctx, `
			delete from public.record_access_group_members
			where group_id = $1 and user_id = $2`, groupID, userID)
	}
	if err != nil {
		return unavailableAccessAdminError("set record access group member", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailableAccessAdminError("commit record access group member", err)
	}
	_ = lockedGroupID
	return nil
}

func (r *PostgresAccessAdminRepository) ListMyGroups(ctx context.Context, actorID string) ([]accessadmin.GroupSummary, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackAccessAdminTx(ctx, tx)
	var activeUserID string
	err = tx.QueryRow(ctx, `
		select user_id
		from users
		where user_id = $1 and disabled_at is null
		for update`, actorID).Scan(&activeUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, accessadmin.ErrManagementForbidden
	}
	if err != nil {
		return nil, unavailableAccessAdminError("lock active user", err)
	}
	rows, err := tx.Query(ctx, `
		select g.group_id, g.display_name
		from public.record_access_groups g
		join public.record_access_group_members m on m.group_id = g.group_id
		where g.project_id = 'default' and m.user_id = $1
		order by g.display_name asc, g.group_id asc`, actorID)
	if err != nil {
		return nil, unavailableAccessAdminError("list user record access groups", err)
	}
	groups := make([]accessadmin.GroupSummary, 0)
	for rows.Next() {
		var group accessadmin.GroupSummary
		if err := rows.Scan(&group.GroupID, &group.DisplayName); err != nil {
			rows.Close()
			return nil, unavailableAccessAdminError("scan user record access groups", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, unavailableAccessAdminError("iterate user record access groups", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return nil, unavailableAccessAdminError("commit list user record access groups", err)
	}
	_ = activeUserID
	return groups, nil
}

func (r *PostgresAccessAdminRepository) begin(ctx context.Context) (pgx.Tx, error) {
	if r == nil || r.pool == nil {
		return nil, accessadmin.ErrManagementUnavailable
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, unavailableAccessAdminError("begin access-management transaction", err)
	}
	return tx, nil
}

func lockAccessAdminSupervisor(ctx context.Context, tx pgx.Tx, actorID string) error {
	var (
		role       string
		supervisor bool
		disabledAt *time.Time
	)
	err := tx.QueryRow(ctx, `
		select role, is_supervisor, disabled_at
		from users
		where user_id = $1
		for update`, actorID).Scan(&role, &supervisor, &disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return accessadmin.ErrManagementForbidden
	}
	if err != nil {
		return unavailableAccessAdminError("lock supervisor", err)
	}
	if role != auth.RoleAdmin || !supervisor || disabledAt != nil {
		return accessadmin.ErrManagementForbidden
	}
	return nil
}

func scanAccessAdminUserSummary(row pgx.Row) (accessadmin.UserSummary, error) {
	var user accessadmin.UserSummary
	err := row.Scan(
		&user.UserID,
		&user.Username,
		&user.DisplayName,
		&user.Role,
		&user.IsSupervisor,
		&user.DisabledAt,
		&user.CreatedAt,
	)
	return user, err
}

func rollbackAccessAdminTx(ctx context.Context, tx pgx.Tx) {
	if tx != nil {
		_ = tx.Rollback(ctx)
	}
}

func unavailableAccessAdminError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", accessadmin.ErrManagementUnavailable, operation)
}

func mapAccessAdminWriteError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			switch pgErr.ConstraintName {
			case "users_username_key":
				return accessadmin.ErrUsernameTaken
			case "record_access_groups_project_display_name_key":
				return accessadmin.ErrGroupNameTaken
			}
		case "23503":
			return accessadmin.ErrResourceNotFound
		case "23514":
			return accessadmin.ErrInvalidRequest
		}
	}
	return unavailableAccessAdminError(operation, err)
}

func dereferenceString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func dereferenceTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

var _ accessadmin.Repository = (*PostgresAccessAdminRepository)(nil)
