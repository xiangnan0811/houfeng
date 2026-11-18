package platformmigrate

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SourceRoleCredential supplies a direct principal and its actual password for
// trusted bootstrap verification. Neither value is persisted by these checks.
type SourceRoleCredential struct {
	Principal string
	Password  string
}

// VerifySourceRoleCredentials checks existing constrained direct-login roles and
// their SCRAM passwords. It creates no roles and changes no grants or settings.
// The caller must supply a direct PostgreSQL 16 bootstrap transaction.
func VerifySourceRoleCredentials(ctx context.Context, tx pgx.Tx, credentials []SourceRoleCredential) error {
	if tx == nil {
		return fmt.Errorf("source credential verification requires a PostgreSQL transaction")
	}
	if len(credentials) == 0 {
		return fmt.Errorf("source credential verification requires at least one role")
	}
	names := make([]string, 0, len(credentials))
	seen := make(map[string]struct{}, len(credentials))
	for _, credential := range credentials {
		if !validAppRoleName(credential.Principal) || credential.Password == "" {
			return fmt.Errorf("invalid source role credential")
		}
		if _, exists := seen[credential.Principal]; exists {
			return fmt.Errorf("duplicate source role credential")
		}
		seen[credential.Principal] = struct{}{}
		names = append(names, credential.Principal)
	}
	if err := verifySourceBootstrap(ctx, tx); err != nil {
		return err
	}

	// Settings may be scoped to any database, so inspect all pg_db_role_setting
	// rows rather than only the current database's effective settings.
	rows, err := tx.Query(ctx, `
		select role.rolname, role.rolcanlogin, role.rolinherit, role.rolsuper,
		       role.rolcreatedb, role.rolcreaterole, role.rolreplication, role.rolbypassrls,
		       (role.rolvaliduntil is null or role.rolvaliduntil > pg_catalog.clock_timestamp()),
		       not exists (select 1 from pg_catalog.pg_db_role_setting setting where setting.setrole = role.oid)
		from pg_catalog.pg_authid role
		where role.rolname = any($1::name[])
	`, names)
	if err != nil {
		return fmt.Errorf("cannot inspect source role attributes")
	}
	attributes := make(map[string]appRoleAttributes, len(names))
	validity := make(map[string]bool, len(names))
	for rows.Next() {
		var name string
		var role appRoleAttributes
		var unexpired, noSettings bool
		if err := rows.Scan(&name, &role.CanLogin, &role.Inherit, &role.Superuser,
			&role.CreateDB, &role.CreateRole, &role.Replication, &role.BypassRLS, &unexpired, &noSettings); err != nil {
			rows.Close()
			return fmt.Errorf("cannot inspect source role attributes")
		}
		attributes[name] = role
		validity[name] = unexpired && noSettings
	}
	if rows.Err() != nil {
		rows.Close()
		return fmt.Errorf("cannot inspect source role attributes")
	}
	rows.Close()
	for _, name := range names {
		role, exists := attributes[name]
		if !exists || !validity[name] || role.validateConstrainedAppRole(name) != nil {
			return fmt.Errorf("source role is missing, expired, configured, or unconstrained")
		}
	}
	memberships, err := readAppRoleMemberships(ctx, tx, names)
	if err != nil {
		return fmt.Errorf("cannot inspect source role memberships")
	}
	if len(memberships) != 0 {
		return fmt.Errorf("source role membership in either direction is forbidden")
	}
	verifiers, err := readComposeRolePasswordVerifiers(ctx, tx, names)
	if err != nil {
		return fmt.Errorf("cannot inspect source role credentials")
	}
	for _, credential := range credentials {
		if !composeRolePasswordMatches(verifiers[credential.Principal], credential.Password) {
			return fmt.Errorf("source role credential does not match its SCRAM verifier")
		}
	}
	return nil
}

// ReadSourceApplicationIdentity verifies an already-provisioned APP identity
// against the physical PostgreSQL system and current database. It never adopts
// an unprovisioned domain or changes the pg_control_system privilege boundary.
func ReadSourceApplicationIdentity(ctx context.Context, tx pgx.Tx) (DomainIdentity, error) {
	if tx == nil {
		return DomainIdentity{}, fmt.Errorf("source application identity requires a PostgreSQL transaction")
	}
	if err := verifySourceBootstrap(ctx, tx); err != nil {
		return DomainIdentity{}, err
	}
	var systemIdentifier, databaseName string
	var databaseOID uint32
	if err := tx.QueryRow(ctx, `
		select control.system_identifier::text, database.oid, database.datname
		from pg_catalog.pg_control_system() control
		join pg_catalog.pg_database database on database.datname = pg_catalog.current_database()
	`).Scan(&systemIdentifier, &databaseOID, &databaseName); err != nil {
		return DomainIdentity{}, fmt.Errorf("read physical source application identity: %w", err)
	}
	rows, err := tx.Query(ctx, `
		select domain_id, domain_kind, identity_epoch, identity_mode,
		       coalesce(postgres_system_identifier, ''), database_oid, database_name
		from public.record_platform_domain_identity
	`)
	if err != nil {
		return DomainIdentity{}, fmt.Errorf("read provisioned source application identity: %w", err)
	}
	defer rows.Close()
	var identity DomainIdentity
	count := 0
	for rows.Next() {
		count++
		if count > 1 {
			return DomainIdentity{}, fmt.Errorf("source application identity must have exactly one provisioned row")
		}
		var kind string
		if err := rows.Scan(&identity.ID, &kind, &identity.IdentityEpoch, &identity.Mode,
			&identity.PostgresSystemIdentifier, &identity.DatabaseOID, &identity.DatabaseName); err != nil {
			return DomainIdentity{}, fmt.Errorf("scan provisioned source application identity: %w", err)
		}
		identity.Kind = DomainKind(kind)
	}
	if err := rows.Err(); err != nil {
		return DomainIdentity{}, fmt.Errorf("iterate provisioned source application identity: %w", err)
	}
	if count != 1 || !validSourceApplicationIdentity(identity, systemIdentifier, databaseOID, databaseName) {
		return DomainIdentity{}, fmt.Errorf("source application identity is unprovisioned, noncanonical, or does not match this PostgreSQL domain")
	}
	return identity, nil
}

func verifySourceBootstrap(ctx context.Context, tx pgx.Tx) error {
	var major int
	var session, current string
	var oid uint32
	var superuser bool
	if err := tx.QueryRow(ctx, `
		select pg_catalog.current_setting('server_version_num')::integer / 10000,
		       session_user, current_user, role.oid, role.rolsuper
		from pg_catalog.pg_roles role where role.rolname = current_user
	`).Scan(&major, &session, &current, &oid, &superuser); err != nil {
		return fmt.Errorf("cannot inspect source bootstrap identity")
	}
	if major != 16 || session != current || oid != 10 || !superuser {
		return fmt.Errorf("source enrollment requires direct PostgreSQL 16 bootstrap superuser OID 10")
	}
	return nil
}

func validSourceApplicationIdentity(identity DomainIdentity, systemIdentifier string, databaseOID uint32, databaseName string) bool {
	if len(identity.ID) != 67 || !strings.HasPrefix(identity.ID, "rd-") {
		return false
	}
	for _, c := range identity.ID[3:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	system, err := strconv.ParseUint(identity.PostgresSystemIdentifier, 10, 64)
	return err == nil && system != 0 && strconv.FormatUint(system, 10) == identity.PostgresSystemIdentifier &&
		identity.Kind == DomainKindApplication && identity.IdentityEpoch == 1 && identity.Mode == "postgres_system" &&
		identity.PostgresSystemIdentifier == systemIdentifier && identity.DatabaseOID != 0 && identity.DatabaseOID == databaseOID &&
		validAppRoleName(identity.DatabaseName) && identity.DatabaseName == databaseName
}
