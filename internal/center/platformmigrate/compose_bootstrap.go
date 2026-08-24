package platformmigrate

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/secure/precis"
)

const composePreR1ValidateSQL = `
DO $app_acl_r2_pre_r1$
DECLARE
    function_count bigint;
    function_owner bigint;
    function_identity_arguments text;
    actor_authorized boolean;
BEGIN
    SELECT pg_catalog.count(*)::bigint,
           pg_catalog.min(procedure.proowner::bigint),
           pg_catalog.min(pg_catalog.pg_get_function_identity_arguments(procedure.oid))
      INTO function_count, function_owner, function_identity_arguments
      FROM pg_catalog.pg_proc procedure
      JOIN pg_catalog.pg_namespace namespace
        ON namespace.oid = procedure.pronamespace
     WHERE namespace.nspname = 'pg_catalog'
       AND procedure.proname = 'pg_control_system'
       AND procedure.pronargs = 0;

    IF function_count <> 1 THEN
        RAISE EXCEPTION 'expected exactly one pg_catalog.pg_control_system() function, found %', function_count;
    END IF;
    IF function_owner <> 10 THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() owner is %, expected bootstrap OID 10', function_owner;
    END IF;
    IF function_identity_arguments IS DISTINCT FROM 'OUT pg_control_version integer, OUT catalog_version_no integer, OUT system_identifier bigint, OUT pg_control_last_modified timestamp with time zone' THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() identity arguments are %, expected PostgreSQL 16 catalog shape', function_identity_arguments;
    END IF;

    SELECT role.rolsuper OR role.oid = function_owner
      INTO actor_authorized
      FROM pg_catalog.pg_roles role
     WHERE role.rolname = current_user;
    IF actor_authorized IS DISTINCT FROM true THEN
        RAISE EXCEPTION 'pre-R1 provisioning must run as a superuser or pg_control_system() owner';
    END IF;
END
$app_acl_r2_pre_r1$`

const composePreR1RevokeSQL = `REVOKE EXECUTE ON FUNCTION pg_catalog.pg_control_system() FROM PUBLIC`

const composePreR1VerifySQL = `
DO $app_acl_r2_pre_r1_verify$
DECLARE
    exact_acl_count bigint;
    function_identity_arguments text;
BEGIN
    SELECT pg_catalog.count(*)::bigint,
           pg_catalog.min(pg_catalog.pg_get_function_identity_arguments(procedure.oid))
      INTO exact_acl_count, function_identity_arguments
      FROM pg_catalog.pg_proc procedure
      JOIN pg_catalog.pg_namespace namespace
        ON namespace.oid = procedure.pronamespace
      CROSS JOIN LATERAL pg_catalog.aclexplode(procedure.proacl) acl_grant
     WHERE namespace.nspname = 'pg_catalog'
       AND procedure.proname = 'pg_control_system'
       AND procedure.pronargs = 0
       AND procedure.proowner = 10
       AND acl_grant.grantor = procedure.proowner
       AND acl_grant.grantee = procedure.proowner
       AND acl_grant.privilege_type = 'EXECUTE'
       AND NOT acl_grant.is_grantable;

    IF function_identity_arguments IS DISTINCT FROM 'OUT pg_control_version integer, OUT catalog_version_no integer, OUT system_identifier bigint, OUT pg_control_last_modified timestamp with time zone' THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() identity arguments are %, expected PostgreSQL 16 catalog shape', function_identity_arguments;
    END IF;

    IF exact_acl_count <> 1 OR EXISTS (
        SELECT 1
          FROM pg_catalog.pg_proc procedure
          JOIN pg_catalog.pg_namespace namespace
            ON namespace.oid = procedure.pronamespace
          CROSS JOIN LATERAL pg_catalog.aclexplode(procedure.proacl) acl_grant
         WHERE namespace.nspname = 'pg_catalog'
           AND procedure.proname = 'pg_control_system'
           AND procedure.pronargs = 0
           AND (acl_grant.grantor <> procedure.proowner
                OR acl_grant.grantee <> procedure.proowner
                OR acl_grant.privilege_type <> 'EXECUTE'
                OR acl_grant.is_grantable)
    ) THEN
        RAISE EXCEPTION 'pg_catalog.pg_control_system() ACL is not explicit owner-only EXECUTE';
    END IF;
END
$app_acl_r2_pre_r1_verify$`

var ErrInvalidComposeBootstrapConfig = errors.New("Compose database bootstrap configuration is invalid")

type ComposeRolePasswords struct {
	Runtime       string
	PlatformAdmin string
	Migrator      string
	Authority     string
}

type ComposeBootstrapConfig struct {
	DatabaseName  string
	BootstrapRole string
	AuthorityRole string
	Roles         AppRoleSetV1
	Passwords     ComposeRolePasswords
}

type composeBootstrapIdentity struct {
	ServerMajor      int
	SessionUser      string
	CurrentUser      string
	CurrentUserOID   uint32
	CurrentUserSuper bool
	DatabaseName     string
}

func (config ComposeBootstrapConfig) Validate() error {
	if config.DatabaseName == "" || !validAppRoleName(config.DatabaseName) ||
		config.BootstrapRole == "" || !validAppRoleName(config.BootstrapRole) ||
		config.BootstrapRole == config.Roles.CenterRuntime ||
		config.BootstrapRole == config.Roles.PlatformAdmin ||
		config.BootstrapRole == config.Roles.Migrator ||
		!validAppRoleName(config.AuthorityRole) ||
		config.AuthorityRole == config.BootstrapRole ||
		config.AuthorityRole == config.Roles.CenterRuntime ||
		config.AuthorityRole == config.Roles.PlatformAdmin ||
		config.AuthorityRole == config.Roles.Migrator ||
		!validComposeRolePassword(config.Passwords.Runtime) ||
		!validComposeRolePassword(config.Passwords.PlatformAdmin) ||
		!validComposeRolePassword(config.Passwords.Migrator) ||
		!validComposeRolePassword(config.Passwords.Authority) ||
		!distinctComposeRolePasswords(config.Passwords) ||
		config.Roles.Validate() != nil {
		return ErrInvalidComposeBootstrapConfig
	}
	return nil
}

func distinctComposeRolePasswords(passwords ComposeRolePasswords) bool {
	seen := make(map[string]struct{}, 4)
	for _, password := range []string{passwords.Runtime, passwords.PlatformAdmin, passwords.Migrator, passwords.Authority} {
		if _, duplicate := seen[password]; duplicate {
			return false
		}
		seen[password] = struct{}{}
	}
	return true
}

func (config ComposeBootstrapConfig) roleNames() []string {
	return append(config.Roles.names(), config.AuthorityRole)
}

func (config ComposeBootstrapConfig) rolePasswords() map[string]string {
	return map[string]string{
		config.Roles.CenterRuntime: config.Passwords.Runtime,
		config.Roles.PlatformAdmin: config.Passwords.PlatformAdmin,
		config.Roles.Migrator:      config.Passwords.Migrator,
		config.AuthorityRole:       config.Passwords.Authority,
	}
}

func validComposeRolePassword(password string) bool {
	if password == "" || !utf8.ValidString(password) {
		return false
	}
	for _, value := range password {
		if value == 0 || unicode.IsControl(value) {
			return false
		}
	}
	return true
}

// ProvisionComposeBootstrap owns only the PostgreSQL-bootstrap-authority
// transaction. It does not apply application migrations.
func ProvisionComposeBootstrap(ctx context.Context, db *pgxpool.Pool, config ComposeBootstrapConfig) (err error) {
	if err := config.Validate(); err != nil {
		return err
	}
	if db == nil {
		return fmt.Errorf("Compose database bootstrap has no PostgreSQL pool")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin Compose database bootstrap transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if err := provisionComposeBootstrapInTx(ctx, tx, config); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit Compose database bootstrap transaction: %w", err)
	}
	return nil
}

func provisionComposeBootstrapInTx(ctx context.Context, tx pgx.Tx, config ComposeBootstrapConfig) error {
	if _, err := tx.Exec(ctx, `select pg_catalog.set_config('search_path', 'pg_catalog', true)`); err != nil {
		return fmt.Errorf("set Compose database bootstrap search path: %w", err)
	}
	if _, err := tx.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended($1, 0))`, "houfeng/compose-database-bootstrap/v1"); err != nil {
		return fmt.Errorf("lock Compose database bootstrap: %w", err)
	}

	var identity composeBootstrapIdentity
	if err := tx.QueryRow(ctx, `
		select current_setting('server_version_num')::integer / 10000,
		       session_user,
		       current_user,
		       role.oid,
		       role.rolsuper,
		       current_database()
		from pg_catalog.pg_roles role
		where role.rolname = current_user
	`).Scan(
		&identity.ServerMajor,
		&identity.SessionUser,
		&identity.CurrentUser,
		&identity.CurrentUserOID,
		&identity.CurrentUserSuper,
		&identity.DatabaseName,
	); err != nil {
		return fmt.Errorf("read Compose database bootstrap identity: %w", err)
	}
	if err := validateComposeBootstrapIdentity(config, identity); err != nil {
		return err
	}

	attributes, err := readAppRoleAttributes(ctx, tx, config.roleNames())
	if err != nil {
		return err
	}
	memberships, err := readAppRoleMemberships(ctx, tx, config.roleNames())
	if err != nil {
		return err
	}
	if len(memberships) != 0 {
		membership := memberships[0]
		return fmt.Errorf("Compose app role membership is forbidden: %q -> %q", membership.MemberRole, membership.ParentRole)
	}
	if err := rejectAppRoleOwnership(ctx, tx, []string{config.Roles.CenterRuntime, config.Roles.PlatformAdmin, config.AuthorityRole}); err != nil {
		return err
	}
	passwordVerifiers, err := readComposeRolePasswordVerifiers(ctx, tx, config.roleNames())
	if err != nil {
		return err
	}

	for _, statement := range []string{composePreR1ValidateSQL, composePreR1RevokeSQL, composePreR1VerifySQL} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply Compose PostgreSQL 16 pre-R1 contract: %w", err)
		}
	}

	rolePasswords := config.rolePasswords()
	for _, role := range config.roleNames() {
		verb := "CREATE"
		_, exists := attributes[role]
		if exists {
			verb = "ALTER"
		}
		var roleDDL string
		passwordMatches := exists && composeRolePasswordMatches(passwordVerifiers[role], rolePasswords[role])
		var formatErr error
		if passwordMatches {
			formatErr = tx.QueryRow(ctx, `
				select pg_catalog.format(
					$ddl$`+verb+` ROLE %I WITH LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT -1 VALID UNTIL 'infinity'$ddl$,
					$1::text
				)
			`, role).Scan(&roleDDL)
		} else {
			formatErr = tx.QueryRow(ctx, `
				select pg_catalog.format(
					$ddl$`+verb+` ROLE %I WITH LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS CONNECTION LIMIT -1 PASSWORD %L VALID UNTIL 'infinity'$ddl$,
					$1::text,
					$2::text
				)
			`, role, rolePasswords[role]).Scan(&roleDDL)
		}
		if formatErr != nil {
			return fmt.Errorf("format constrained Compose role %q: %w", role, formatErr)
		}
		if _, err := tx.Exec(ctx, roleDDL); err != nil {
			return fmt.Errorf("converge constrained Compose role %q: %w", role, err)
		}
		var resetDDL string
		if err := tx.QueryRow(ctx, `select pg_catalog.format('ALTER ROLE %I RESET ALL', $1::text)`, role).Scan(&resetDDL); err != nil {
			return fmt.Errorf("format Compose role settings reset %q: %w", role, err)
		}
		if _, err := tx.Exec(ctx, resetDDL); err != nil {
			return fmt.Errorf("reset Compose role settings %q: %w", role, err)
		}
	}

	var ownerDDL string
	if err := tx.QueryRow(ctx, `select pg_catalog.format('ALTER DATABASE %I OWNER TO %I', $1::text, $2::text)`, config.DatabaseName, config.Roles.Migrator).Scan(&ownerDDL); err != nil {
		return fmt.Errorf("format Compose database ownership: %w", err)
	}
	if _, err := tx.Exec(ctx, ownerDDL); err != nil {
		return fmt.Errorf("converge Compose database ownership: %w", err)
	}

	attributes, err = readAppRoleAttributes(ctx, tx, config.roleNames())
	if err != nil {
		return err
	}
	memberships, err = readAppRoleMemberships(ctx, tx, config.roleNames())
	if err != nil {
		return err
	}
	if err := validateComposeRoleCatalogState(config, attributes, memberships); err != nil {
		return err
	}
	if err := rejectAppRoleOwnership(ctx, tx, []string{config.Roles.CenterRuntime, config.Roles.PlatformAdmin, config.AuthorityRole}); err != nil {
		return err
	}
	var databaseOwner string
	if err := tx.QueryRow(ctx, `
		select owner.rolname
		from pg_catalog.pg_database database
		join pg_catalog.pg_roles owner on owner.oid = database.datdba
		where database.datname = current_database()
	`).Scan(&databaseOwner); err != nil {
		return fmt.Errorf("read Compose database owner: %w", err)
	}
	if databaseOwner != config.Roles.Migrator {
		return fmt.Errorf("Compose database owner is not the configured direct migrator")
	}
	return nil
}

func validateComposeRoleCatalogState(config ComposeBootstrapConfig, attributes map[string]appRoleAttributes, memberships []appRoleMembership) error {
	for _, role := range config.roleNames() {
		roleAttributes, ok := attributes[role]
		if !ok {
			return fmt.Errorf("missing constrained Compose role %q", role)
		}
		if err := roleAttributes.validateConstrainedAppRole(role); err != nil {
			return err
		}
	}
	if len(memberships) != 0 {
		membership := memberships[0]
		return fmt.Errorf("Compose app role membership is forbidden: %q -> %q", membership.MemberRole, membership.ParentRole)
	}
	return nil
}

func validateComposeBootstrapIdentity(config ComposeBootstrapConfig, identity composeBootstrapIdentity) error {
	if identity.ServerMajor != 16 || identity.SessionUser != config.BootstrapRole ||
		identity.CurrentUser != config.BootstrapRole || identity.CurrentUserOID != 10 ||
		!identity.CurrentUserSuper || identity.DatabaseName != config.DatabaseName {
		return fmt.Errorf("Compose database bootstrap identity does not match PostgreSQL 16 fixed topology")
	}
	return nil
}

func readComposeRolePasswordVerifiers(ctx context.Context, tx pgx.Tx, roles []string) (map[string]string, error) {
	rows, err := tx.Query(ctx, `
		select rolname, rolpassword
		from pg_catalog.pg_authid
		where rolname = any($1::name[])
	`, roles)
	if err != nil {
		return nil, fmt.Errorf("read Compose role password verifiers: %w", err)
	}
	defer rows.Close()
	verifiers := make(map[string]string, len(roles))
	for rows.Next() {
		var role string
		var verifier *string
		if err := rows.Scan(&role, &verifier); err != nil {
			return nil, fmt.Errorf("scan Compose role password verifier: %w", err)
		}
		if verifier != nil {
			verifiers[role] = *verifier
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Compose role password verifiers: %w", err)
	}
	return verifiers, nil
}

func composeRolePasswordMatches(verifier, password string) bool {
	parts := strings.Split(verifier, "$")
	if len(parts) != 3 || parts[0] != "SCRAM-SHA-256" {
		return false
	}
	iterationAndSalt := strings.Split(parts[1], ":")
	keys := strings.Split(parts[2], ":")
	if len(iterationAndSalt) != 2 || len(keys) != 2 {
		return false
	}
	iterations, err := strconv.Atoi(iterationAndSalt[0])
	if err != nil || iterations < 1 || iterations > 1_000_000 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(iterationAndSalt[1])
	if err != nil || len(salt) == 0 {
		return false
	}
	wantStoredKey, err := base64.StdEncoding.DecodeString(keys[0])
	if err != nil || len(wantStoredKey) != sha256.Size {
		return false
	}
	wantServerKey, err := base64.StdEncoding.DecodeString(keys[1])
	if err != nil || len(wantServerKey) != sha256.Size {
		return false
	}
	preparedPassword, err := precis.OpaqueString.String(password)
	if err != nil {
		preparedPassword = password
	}
	saltedPassword := pbkdf2.Key([]byte(preparedPassword), salt, iterations, sha256.Size, sha256.New)
	clientMAC := hmac.New(sha256.New, saltedPassword)
	_, _ = clientMAC.Write([]byte("Client Key"))
	clientKey := clientMAC.Sum(nil)
	storedKey := sha256.Sum256(clientKey)
	serverMAC := hmac.New(sha256.New, saltedPassword)
	_, _ = serverMAC.Write([]byte("Server Key"))
	serverKey := serverMAC.Sum(nil)
	return subtle.ConstantTimeCompare(storedKey[:], wantStoredKey) == 1 &&
		subtle.ConstantTimeCompare(serverKey, wantServerKey) == 1
}
