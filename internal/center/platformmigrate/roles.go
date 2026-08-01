package platformmigrate

import (
	"context"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/unicode/norm"
)

// AppRoleSetV1 names the pre-created application runtime, administrator, and
// migrator roles. These names are catalog identifiers, not connection strings
// or credentials.
type AppRoleSetV1 struct {
	CenterRuntime string
	PlatformAdmin string
	Migrator      string
}

// Validate rejects ambiguous or unsafe role identities before PostgreSQL is
// queried. PostgreSQL grants must quote these identifiers when used in DDL.
func (roles AppRoleSetV1) Validate() error {
	entries := []struct {
		kind string
		name string
	}{
		{kind: "center runtime", name: roles.CenterRuntime},
		{kind: "platform admin", name: roles.PlatformAdmin},
		{kind: "migrator", name: roles.Migrator},
	}
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		if !validAppRoleName(entry.name) {
			return fmt.Errorf("invalid %s role name", entry.kind)
		}
		if existing, ok := seen[entry.name]; ok {
			return fmt.Errorf("%s role reuses %s role name", entry.kind, existing)
		}
		seen[entry.name] = entry.kind
	}
	return nil
}

func (roles AppRoleSetV1) names() []string {
	return []string{roles.CenterRuntime, roles.PlatformAdmin, roles.Migrator}
}

func validAppRoleName(name string) bool {
	if len(name) < 1 || len(name) > 63 || !utf8.ValidString(name) || !norm.NFC.IsNormalString(name) {
		return false
	}
	for _, value := range name {
		if value == 0 || unicode.IsControl(value) {
			return false
		}
	}
	return true
}

// ProvisionRoles performs the non-mutating preflight for pre-created app
// roles. It intentionally creates no roles and makes no ACL changes; later
// manifest convergence owns the grants and revocations.
func ProvisionRoles(ctx context.Context, db *pgxpool.Pool, roles AppRoleSetV1) (err error) {
	if db == nil {
		return fmt.Errorf("app role preflight has no PostgreSQL pool")
	}
	if err := roles.Validate(); err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return fmt.Errorf("begin app role preflight: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var currentUser string
	if err := tx.QueryRow(ctx, `select current_user`).Scan(&currentUser); err != nil {
		return fmt.Errorf("read app role preflight current user: %w", err)
	}
	if currentUser == roles.CenterRuntime || currentUser == roles.PlatformAdmin {
		return fmt.Errorf("app role preflight current migrator reuses runtime or admin role %q", currentUser)
	}

	attributes, err := readAppRoleAttributes(ctx, tx, roles.names())
	if err != nil {
		return err
	}
	memberships, err := readAppRoleMemberships(ctx, tx, roles.names())
	if err != nil {
		return err
	}
	if err := validateAppRoleCatalogState(roles, attributes, memberships); err != nil {
		return err
	}
	if err := rejectAppRoleOwnership(ctx, tx, []string{roles.CenterRuntime, roles.PlatformAdmin}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit app role preflight: %w", err)
	}
	return nil
}

// ProvisionRolesInTx performs the strict direct-login application-role
// preflight on a caller-owned transaction. It neither begins nor commits a
// transaction, so a scoped migrator can prove its identity and all role facts
// in the same SERIALIZABLE closure that changes the APP catalog.
func ProvisionRolesInTx(ctx context.Context, tx pgx.Tx, roles AppRoleSetV1) error {
	if tx == nil {
		return fmt.Errorf("app role preflight has no PostgreSQL transaction")
	}
	if err := roles.Validate(); err != nil {
		return err
	}

	var sessionUser, currentUser string
	if err := tx.QueryRow(ctx, `select session_user, current_user`).Scan(&sessionUser, &currentUser); err != nil {
		return fmt.Errorf("read app role preflight session and current user: %w", err)
	}
	attributes, err := readAppRoleAttributes(ctx, tx, roles.names())
	if err != nil {
		return err
	}
	memberships, err := readAppRoleMemberships(ctx, tx, roles.names())
	if err != nil {
		return err
	}
	if err := validateDirectRolePreflight(roles, sessionUser, currentUser, attributes, memberships); err != nil {
		return err
	}
	return nil
}

type appRoleAttributes struct {
	CanLogin    bool
	Inherit     bool
	Superuser   bool
	CreateDB    bool
	CreateRole  bool
	Replication bool
	BypassRLS   bool
}

func (attributes appRoleAttributes) validateConstrainedAppRole(role string) error {
	if !attributes.CanLogin || attributes.Inherit || attributes.Superuser || attributes.CreateDB ||
		attributes.CreateRole || attributes.Replication || attributes.BypassRLS {
		return fmt.Errorf("pre-created app role %q must be LOGIN, NOINHERIT, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION, and NOBYPASSRLS", role)
	}
	return nil
}

func readAppRoleAttributes(ctx context.Context, tx pgx.Tx, names []string) (map[string]appRoleAttributes, error) {
	rows, err := tx.Query(ctx, `
		select rolname,
		       rolcanlogin,
		       rolinherit,
		       rolsuper,
		       rolcreatedb,
		       rolcreaterole,
		       rolreplication,
		       rolbypassrls
		from pg_roles
		where rolname = any($1::name[])
	`, names)
	if err != nil {
		return nil, fmt.Errorf("read pre-created app roles: %w", err)
	}
	defer rows.Close()

	attributes := make(map[string]appRoleAttributes, len(names))
	for rows.Next() {
		var name string
		var role appRoleAttributes
		if err := rows.Scan(
			&name,
			&role.CanLogin,
			&role.Inherit,
			&role.Superuser,
			&role.CreateDB,
			&role.CreateRole,
			&role.Replication,
			&role.BypassRLS,
		); err != nil {
			return nil, fmt.Errorf("scan pre-created app role: %w", err)
		}
		attributes[name] = role
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pre-created app roles: %w", err)
	}
	return attributes, nil
}

type appRoleMembership struct {
	MemberRole string
	ParentRole string
}

func readAppRoleMemberships(ctx context.Context, tx pgx.Tx, names []string) ([]appRoleMembership, error) {
	rows, err := tx.Query(ctx, `
		with recursive membership_paths(member_oid, parent_oid, path) as (
			select membership.member,
		       membership.roleid,
		       array[membership.member, membership.roleid]::oid[]
			from pg_catalog.pg_auth_members membership
			union all
			select membership_paths.member_oid,
		       next_membership.roleid,
		       membership_paths.path || next_membership.roleid
			from membership_paths
			join pg_catalog.pg_auth_members next_membership
			  on next_membership.member = membership_paths.parent_oid
			where not next_membership.roleid = any(membership_paths.path)
		)
		select member_role.rolname, parent_role.rolname
		from membership_paths
		join pg_catalog.pg_roles member_role on member_role.oid = membership_paths.member_oid
		join pg_catalog.pg_roles parent_role on parent_role.oid = membership_paths.parent_oid
		where member_role.rolname = any($1::name[])
		   or parent_role.rolname = any($1::name[])
		order by member_role.rolname, parent_role.rolname
	`, names)
	if err != nil {
		return nil, fmt.Errorf("read app role memberships: %w", err)
	}
	defer rows.Close()
	memberships := make([]appRoleMembership, 0)
	for rows.Next() {
		var membership appRoleMembership
		if err := rows.Scan(&membership.MemberRole, &membership.ParentRole); err != nil {
			return nil, fmt.Errorf("scan app role membership: %w", err)
		}
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app role memberships: %w", err)
	}
	return memberships, nil
}

func validateAppRoleCatalogState(
	roles AppRoleSetV1,
	attributes map[string]appRoleAttributes,
	memberships []appRoleMembership,
) error {
	for _, role := range roles.names() {
		attributes, ok := attributes[role]
		if !ok {
			return fmt.Errorf("missing pre-created app role %q", role)
		}
		if err := attributes.validateConstrainedAppRole(role); err != nil {
			return err
		}
	}
	if len(memberships) != 0 {
		membership := memberships[0]
		return fmt.Errorf("app role membership is forbidden: %q -> %q", membership.MemberRole, membership.ParentRole)
	}
	return nil
}

func validateDirectRolePreflight(
	roles AppRoleSetV1,
	sessionUser string,
	currentUser string,
	attributes map[string]appRoleAttributes,
	memberships []appRoleMembership,
) error {
	if err := roles.Validate(); err != nil {
		return err
	}
	if sessionUser != roles.Migrator {
		return fmt.Errorf("app role preflight session user %q does not match configured migrator role %q", sessionUser, roles.Migrator)
	}
	if currentUser != roles.Migrator {
		return fmt.Errorf("app role preflight current user %q does not match configured migrator role %q", currentUser, roles.Migrator)
	}
	if sessionUser != currentUser {
		return fmt.Errorf("app role preflight session user %q does not match current user %q", sessionUser, currentUser)
	}
	if err := validateAppRoleCatalogState(roles, attributes, memberships); err != nil {
		return err
	}
	return nil
}

func rejectAppRoleOwnership(ctx context.Context, tx pgx.Tx, names []string) error {
	var role, objectKind, objectIdentity string
	err := tx.QueryRow(ctx, `
		select owner.rolname, objects.object_kind, objects.object_identity
		from (
			select datdba as owner_oid, 'database'::text as object_kind, datname as object_identity
			from pg_database
			where datname = current_database()
			union all
			select nspowner, 'schema'::text, nspname
			from pg_namespace
			union all
			select class.relowner, 'relation'::text, namespace.nspname || '.' || class.relname
			from pg_class class
			join pg_namespace namespace on namespace.oid = class.relnamespace
			union all
			select procedure.proowner, 'function'::text,
			       namespace.nspname || '.' || procedure.proname || '(' || pg_get_function_identity_arguments(procedure.oid) || ')'
			from pg_proc procedure
			join pg_namespace namespace on namespace.oid = procedure.pronamespace
		) objects
		join pg_roles owner on owner.oid = objects.owner_oid
		where owner.rolname = any($1::name[])
		order by owner.rolname, objects.object_kind, objects.object_identity
		limit 1
	`, names).Scan(&role, &objectKind, &objectIdentity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read app role ownership: %w", err)
	}
	return fmt.Errorf("app runtime/admin role %q owns %s %q", role, objectKind, objectIdentity)
}
