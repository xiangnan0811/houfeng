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
	for _, role := range roles.names() {
		if _, ok := attributes[role]; !ok {
			return fmt.Errorf("missing pre-created app role %q", role)
		}
	}
	if attributes[roles.Migrator].Inherit {
		return fmt.Errorf("pre-created app migrator role %q must be NOINHERIT", roles.Migrator)
	}
	for _, role := range []string{roles.CenterRuntime, roles.PlatformAdmin} {
		if err := attributes[role].validateRuntimeOrAdmin(role); err != nil {
			return err
		}
	}
	if err := rejectAppRoleMembership(ctx, tx, []string{roles.CenterRuntime, roles.PlatformAdmin}); err != nil {
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

type appRoleAttributes struct {
	CanLogin    bool
	Inherit     bool
	Superuser   bool
	CreateDB    bool
	CreateRole  bool
	Replication bool
	BypassRLS   bool
}

func (attributes appRoleAttributes) validateRuntimeOrAdmin(role string) error {
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

func rejectAppRoleMembership(ctx context.Context, tx pgx.Tx, names []string) error {
	rows, err := tx.Query(ctx, `
		select member_role.rolname, parent_role.rolname
		from pg_auth_members membership
		join pg_roles member_role on member_role.oid = membership.member
		join pg_roles parent_role on parent_role.oid = membership.roleid
		where member_role.rolname = any($1::name[])
		   or parent_role.rolname = any($1::name[])
		order by member_role.rolname, parent_role.rolname
	`, names)
	if err != nil {
		return fmt.Errorf("read app role memberships: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		var member, parent string
		if err := rows.Scan(&member, &parent); err != nil {
			return fmt.Errorf("scan app role membership: %w", err)
		}
		return fmt.Errorf("app runtime/admin role membership is forbidden: %q -> %q", member, parent)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate app role memberships: %w", err)
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
