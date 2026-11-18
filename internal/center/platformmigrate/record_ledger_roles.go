package platformmigrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RecordLedgerScopeRoles identifies two pre-created, independently credentialed
// direct logins. The writer is for the trusted management coordinator only.
type RecordLedgerScopeRoles struct {
	Reader SourceRoleCredential
	Writer SourceRoleCredential
}

func (roles RecordLedgerScopeRoles) Validate() error {
	if !validRecordLedgerRoleName(roles.Reader.Principal) || !validRecordLedgerRoleName(roles.Writer.Principal) ||
		roles.Reader.Principal == roles.Writer.Principal || roles.Reader.Password == "" ||
		roles.Writer.Password == "" || roles.Reader.Password == roles.Writer.Password {
		return fmt.Errorf("invalid record ledger scope roles")
	}
	return nil
}

func validRecordLedgerRoleName(name string) bool {
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		ch := name[i]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return false
		}
	}
	return true
}

type recordLedgerGrant struct {
	role      string
	kind      string
	object    string
	privilege string
}

func recordLedgerScopeGrants(kind DomainKind, database string, roles RecordLedgerScopeRoles) []recordLedgerGrant {
	prefix := "deletion_ledger"
	if kind == DomainKindDeletionWitness {
		prefix = "deletion_witness"
	}
	result := make([]recordLedgerGrant, 0, 20)
	for _, role := range []string{roles.Reader.Principal, roles.Writer.Principal} {
		result = append(result,
			recordLedgerGrant{role, "database", database, "CONNECT"},
			recordLedgerGrant{role, "schema", "public", "USAGE"},
			recordLedgerGrant{role, "function", "pg_catalog.pg_control_system()", "EXECUTE"},
			recordLedgerGrant{role, "table", "public.record_platform_domain_identity", "SELECT"},
			recordLedgerGrant{role, "table", "public." + prefix + "_entries", "SELECT"},
			recordLedgerGrant{role, "table", "public." + prefix + "_head", "SELECT"},
		)
		if kind == DomainKindDeletionLedger {
			result = append(result, recordLedgerGrant{role, "table", "public.deletion_ledger_checkpoints", "SELECT"})
		}
	}
	return append(result,
		recordLedgerGrant{roles.Writer.Principal, "schema", "record_platform_internal", "USAGE"},
		recordLedgerGrant{roles.Writer.Principal, "function", "record_platform_internal.digest(bytea,text)", "EXECUTE"},
		recordLedgerGrant{roles.Writer.Principal, "table", "public." + prefix + "_entries", "INSERT"},
		recordLedgerGrant{roles.Writer.Principal, "table", "public." + prefix + "_head", "UPDATE"},
	)
}

// ProvisionRecordLedgerScope grants the exact record-ledger privileges to two
// existing direct logins. It neither creates domains/roles nor changes passwords,
// object ownership, source grants, or migration bytes. The domain must already
// have a provisioned physical identity. Its trusted bootstrap connection owns
// the entire preflight, grant and readback transaction.
func ProvisionRecordLedgerScope(ctx context.Context, db *pgxpool.Pool, kind DomainKind, roles RecordLedgerScopeRoles) (DomainIdentity, error) {
	if ctx == nil || db == nil || (kind != DomainKindDeletionLedger && kind != DomainKindDeletionWitness) || roles.Validate() != nil {
		return DomainIdentity{}, fmt.Errorf("invalid record ledger provisioning configuration")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return DomainIdentity{}, fmt.Errorf("begin record ledger scope provisioning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := verifySourceBootstrap(ctx, tx); err != nil {
		return DomainIdentity{}, err
	}
	if _, err := tx.Exec(ctx, `select pg_catalog.pg_advisory_xact_lock(1213154899, 84)`); err != nil {
		return DomainIdentity{}, fmt.Errorf("lock record ledger scope provisioning: %w", err)
	}
	if err := VerifySourceRoleCredentials(ctx, tx, []SourceRoleCredential{roles.Reader, roles.Writer}); err != nil {
		return DomainIdentity{}, fmt.Errorf("record ledger role preflight: %w", err)
	}
	// Never let the bootstrap, object owner, source or birth principal be reused
	// as the coordinator's narrow reader/writer login.
	var bootstrap string
	if err := tx.QueryRow(ctx, `select session_user`).Scan(&bootstrap); err != nil || bootstrap == roles.Reader.Principal || bootstrap == roles.Writer.Principal {
		return DomainIdentity{}, fmt.Errorf("record ledger roles must be separate from bootstrap")
	}
	if err := checkRecordLedgerRoleSeparation(ctx, tx, kind, roles); err != nil {
		return DomainIdentity{}, err
	}
	identity, err := provisionPostgresDomainIdentityTx(ctx, tx, kind, false)
	if err != nil {
		return DomainIdentity{}, fmt.Errorf("record ledger physical identity: %w", err)
	}
	grants := recordLedgerScopeGrants(kind, identity.DatabaseName, roles)
	if err := checkRecordLedgerScopeACL(ctx, tx, grants, false); err != nil {
		return DomainIdentity{}, err
	}
	for _, grant := range grants {
		object := grant.object
		switch grant.kind {
		case "database", "schema":
			object = pgx.Identifier{object}.Sanitize()
		case "table":
			// All table identifiers in this closed manifest are hard-coded above.
		case "function":
			// All function signatures in this closed manifest are hard-coded above.
		default:
			return DomainIdentity{}, fmt.Errorf("invalid record ledger grant class")
		}
		statement := "grant " + grant.privilege + " on " + grant.kind + " " + object + " to " + pgx.Identifier{grant.role}.Sanitize()
		if _, err := tx.Exec(ctx, statement); err != nil {
			return DomainIdentity{}, fmt.Errorf("grant record ledger %s privilege: %w", grant.privilege, err)
		}
	}
	if err := checkRecordLedgerScopeACL(ctx, tx, grants, true); err != nil {
		return DomainIdentity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DomainIdentity{}, fmt.Errorf("record ledger provisioning commit uncertain: %w", err)
	}
	return identity, nil
}

func checkRecordLedgerRoleSeparation(ctx context.Context, tx pgx.Tx, kind DomainKind, roles RecordLedgerScopeRoles) error {
	names := []string{roles.Reader.Principal, roles.Writer.Principal}
	var owns bool
	if err := tx.QueryRow(ctx, `select exists (
		select 1 from pg_catalog.pg_shdepend d join pg_catalog.pg_roles r on r.oid=d.refobjid
		where d.refclassid='pg_catalog.pg_authid'::regclass and d.deptype='o' and r.rolname=any($1::name[]))`, names).Scan(&owns); err != nil || owns {
		return fmt.Errorf("record ledger login owns an object or ownership cannot be checked")
	}
	if kind != DomainKindDeletionWitness {
		return nil
	}
	for _, table := range []string{"source_authority_principals", "record_birth_principals"} {
		var exists bool
		if err := tx.QueryRow(ctx, `select pg_catalog.to_regclass($1) is not null`, "public."+table).Scan(&exists); err != nil {
			return fmt.Errorf("inspect witness principal registry")
		}
		if !exists {
			continue
		}
		var overlaps bool
		if err := tx.QueryRow(ctx, "select exists (select 1 from public."+table+" where principal=any($1::name[]))", names).Scan(&overlaps); err != nil || overlaps {
			return fmt.Errorf("record ledger role overlaps source or birth authority")
		}
	}
	return nil
}

// Inspect direct ACLs on every catalog object, not just the desired tables. A
// pre-existing subset is permitted for crash-safe exact retries, but unrelated
// or grantable privileges are never silently revoked or widened.
func checkRecordLedgerScopeACL(ctx context.Context, tx pgx.Tx, grants []recordLedgerGrant, requireAll bool) error {
	expected := make(map[recordLedgerGrant]bool, len(grants))
	var names []string
	for _, grant := range grants {
		if _, seen := expected[grant]; seen {
			return fmt.Errorf("duplicate record ledger ACL manifest entry")
		}
		expected[grant] = false
		if len(names) == 0 || names[0] != grant.role && (len(names) == 1 || names[1] != grant.role) {
			names = append(names, grant.role)
		}
	}
	rows, err := tx.Query(ctx, `with acl_objects(kind, object, owner, acl) as (
		select 'table', n.nspname||'.'||c.relname, c.relowner, c.relacl from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		union all select 'schema', n.nspname, n.nspowner, n.nspacl from pg_catalog.pg_namespace n
		union all select 'database', d.datname, d.datdba, d.datacl from pg_catalog.pg_database d
		union all select 'function', case when p.oid='pg_catalog.pg_control_system()'::regprocedure then 'pg_catalog.pg_control_system()'
			when p.oid=pg_catalog.to_regprocedure('record_platform_internal.digest(bytea,text)') then 'record_platform_internal.digest(bytea,text)'
			else p.oid::regprocedure::text end, p.proowner, p.proacl from pg_catalog.pg_proc p
	) select r.rolname, o.kind, o.object, x.privilege_type, x.is_grantable, x.grantor=o.owner
	from acl_objects o cross join lateral pg_catalog.aclexplode(o.acl) x
	join pg_catalog.pg_roles r on r.oid=x.grantee where r.rolname=any($1::name[])`, names)
	if err != nil {
		return fmt.Errorf("inspect record ledger ACLs: %w", err)
	}
	for rows.Next() {
		var grant recordLedgerGrant
		var grantable, ownerGranted bool
		if err := rows.Scan(&grant.role, &grant.kind, &grant.object, &grant.privilege, &grantable, &ownerGranted); err != nil {
			rows.Close()
			return fmt.Errorf("scan record ledger ACL: %w", err)
		}
		if _, ok := expected[grant]; !ok || grantable || !ownerGranted {
			rows.Close()
			return fmt.Errorf("record ledger principal has unexpected or grantable privilege")
		}
		expected[grant] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate record ledger ACLs: %w", err)
	}
	rows.Close()
	if requireAll {
		for _, found := range expected {
			if !found {
				return fmt.Errorf("missing record ledger privilege")
			}
		}
	}
	var unsafe bool
	if err := tx.QueryRow(ctx, `select
		exists (select 1 from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid=c.relnamespace
			cross join lateral pg_catalog.aclexplode(c.relacl) x
			where n.nspname='public' and c.relname=any($1::text[]) and x.grantee=0)
		or exists (select 1 from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid=c.relnamespace
			cross join lateral pg_catalog.aclexplode(c.relacl) x where n.nspname not in ('pg_catalog','information_schema')
			and n.nspname !~ '^pg_' and x.grantee=0)
		or exists (select 1 from pg_catalog.pg_attribute a cross join lateral pg_catalog.aclexplode(a.attacl) x
			join pg_catalog.pg_class c on c.oid=a.attrelid join pg_catalog.pg_namespace n on n.oid=c.relnamespace
			left join pg_catalog.pg_roles r on r.oid=x.grantee where r.rolname=any($2::name[])
			or (x.grantee=0 and n.nspname not in ('pg_catalog','information_schema') and n.nspname !~ '^pg_'))
		or exists (select 1 from pg_catalog.pg_roles r where r.rolname=any($2::name[]) and
			(pg_catalog.has_schema_privilege(r.oid,'public','CREATE') or pg_catalog.has_database_privilege(r.oid,pg_catalog.current_database(),'CREATE')))
		or exists (select 1 from pg_catalog.pg_proc p cross join lateral
			pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f',p.proowner))) x
			where p.oid='pg_catalog.pg_control_system()'::regprocedure and x.grantee=0 and x.privilege_type='EXECUTE')
		or exists (select 1 from pg_catalog.pg_proc p join pg_catalog.pg_namespace n on n.oid=p.pronamespace
			cross join lateral pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f',p.proowner))) x
			where n.nspname not in ('pg_catalog','information_schema') and n.nspname !~ '^pg_' and p.prosecdef
			and x.grantee=0 and x.privilege_type='EXECUTE')
		or exists (select 1 from pg_catalog.pg_shdepend d join pg_catalog.pg_roles r on r.oid=d.refobjid
			where d.refclassid='pg_catalog.pg_authid'::regclass and d.deptype='a' and r.rolname=any($2::name[])
			and (d.classid not in ('pg_catalog.pg_class'::regclass,'pg_catalog.pg_proc'::regclass,'pg_catalog.pg_namespace'::regclass,'pg_catalog.pg_database'::regclass)
			or d.objsubid<>0 or (d.dbid<>0 and d.dbid<>(select oid from pg_catalog.pg_database where datname=pg_catalog.current_database()))))`,
		[]string{"record_platform_domain_identity", "deletion_ledger_entries", "deletion_ledger_head", "deletion_ledger_checkpoints", "deletion_witness_entries", "deletion_witness_head"}, names).Scan(&unsafe); err != nil || unsafe {
		return fmt.Errorf("record ledger scope has unsafe effective privileges")
	}
	return nil
}
