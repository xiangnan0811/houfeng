package platformmigrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	witnessMigrations "houfeng/db/deletionwitness/migrations"
)

const recordBirthMigration = "0003_record_birth_authority.sql"

// convergeSourceAuthority never exposes an intermediate successor: source
// enrollment, ownership, grants, the migration ledger and final validation are
// committed together. The SQL lock serializes this operation with source
// mutations; it does not replace the operator's external offline fence.
func convergeSourceAuthority(ctx context.Context, db *pgxpool.Pool, roles SourceAuthorityRoles) (DomainIdentity, error) {
	if db == nil {
		return DomainIdentity{}, fmt.Errorf("source authority provisioning has no PostgreSQL pool")
	}
	if err := roles.Validate(); err != nil {
		return DomainIdentity{}, err
	}
	sources, err := migrationSources(witnessMigrations.FS)
	if err != nil {
		return DomainIdentity{}, err
	}
	names := []string{"0001_create_full_witness.sql", "0002_source_retirement_authority.sql", recordBirthMigration}
	if len(sources) != len(names) {
		return DomainIdentity{}, fmt.Errorf("unexpected witness migration source set")
	}
	for _, name := range names {
		if _, ok := sources[name]; !ok {
			return DomainIdentity{}, fmt.Errorf("missing witness migration %q", name)
		}
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return DomainIdentity{}, fmt.Errorf("begin source authority convergence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(1213154899, 83)`); err != nil {
		return DomainIdentity{}, fmt.Errorf("lock witness convergence: %w", err)
	}
	var session, current string
	var dedicated bool
	if err := tx.QueryRow(ctx, `select session_user, current_user,
		not exists (select 1 from pg_catalog.pg_class c
		join pg_catalog.pg_namespace n on n.oid = c.relnamespace
		where n.nspname not in ('pg_catalog','information_schema') and n.nspname !~ '^pg_'
		and c.relkind in ('r','p','S','v','m','f')
		and not (n.nspname = 'public' and c.relname = any($1::text[]))
		and not exists (select 1 from pg_depend d where d.classid = 'pg_class'::regclass
			and d.objid = c.oid and d.deptype = 'e'))`, sourceAuthorityAllowedRelations()).Scan(&session, &current, &dedicated); err != nil {
		return DomainIdentity{}, fmt.Errorf("inspect dedicated witness database: %w", err)
	}
	if session != current || !dedicated {
		return DomainIdentity{}, fmt.Errorf("source authority requires a direct bootstrap connection to a dedicated witness database")
	}
	for _, role := range roles.names() {
		if role == session {
			return DomainIdentity{}, fmt.Errorf("source authority bootstrap cannot reuse managed role %q", role)
		}
	}
	if err := sourceAuthorityCheckRoles(ctx, tx, roles); err != nil {
		return DomainIdentity{}, err
	}
	var ledgerExists bool
	if err := tx.QueryRow(ctx, `select to_regclass('public.schema_migrations') is not null`).Scan(&ledgerExists); err != nil {
		return DomainIdentity{}, fmt.Errorf("inspect witness migration ledger: %w", err)
	}
	var applied map[string]string
	if !ledgerExists {
		var empty bool
		if err := tx.QueryRow(ctx, `select not exists (
			select 1 from pg_class c join pg_namespace n on n.oid=c.relnamespace
			where n.nspname not in ('pg_catalog','information_schema') and n.nspname !~ '^pg_'
			and c.relkind in ('r','p','S','v','m','f')
			and not exists (select 1 from pg_depend d where d.classid='pg_class'::regclass and d.objid=c.oid and d.deptype='e')
		) and not exists (
			select 1 from pg_proc p join pg_namespace n on n.oid=p.pronamespace
			where n.nspname not in ('pg_catalog','information_schema') and n.nspname !~ '^pg_'
			and not exists (select 1 from pg_depend d where d.classid='pg_proc'::regclass and d.objid=p.oid and d.deptype='e')
		)`).Scan(&empty); err != nil {
			return DomainIdentity{}, fmt.Errorf("inspect fresh witness catalog: %w", err)
		}
		if !empty {
			return DomainIdentity{}, fmt.Errorf("mixed witness catalog without migration ledger")
		}
		if _, err := tx.Exec(ctx, ensureSchemaMigrationsSQL); err != nil {
			return DomainIdentity{}, fmt.Errorf("create witness migration ledger: %w", err)
		}
		for _, name := range names[:2] {
			if err := applyWitnessMigrationTx(ctx, tx, name, sources[name]); err != nil {
				return DomainIdentity{}, err
			}
		}
		applied = map[string]string{names[0]: sources[names[0]].checksum, names[1]: sources[names[1]].checksum}
	} else {
		applied, err = witnessLedgerTx(ctx, tx)
		if err != nil {
			return DomainIdentity{}, err
		}
	}
	if len(applied) != 2 && len(applied) != 3 {
		return DomainIdentity{}, fmt.Errorf("mixed witness migration ledger")
	}
	for index, name := range names {
		checksum, present := applied[name]
		if index < 2 && !present || present && checksum != sources[name].checksum {
			return DomainIdentity{}, fmt.Errorf("witness migration checksum or predecessor mismatch for %q", name)
		}
	}
	for name := range applied {
		if _, ok := sources[name]; !ok {
			return DomainIdentity{}, fmt.Errorf("unknown witness migration %q", name)
		}
	}
	_, successor := applied[recordBirthMigration]
	if len(applied) == 3 && !successor {
		return DomainIdentity{}, fmt.Errorf("mixed witness successor ledger")
	}
	if _, err := tx.Exec(ctx, `lock table public.source_authority_state in exclusive mode`); err != nil {
		return DomainIdentity{}, fmt.Errorf("lock source authority enrollment: %w", err)
	}
	fresh, err := sourceAuthorityEnrollment(ctx, tx, roles)
	if err != nil {
		return DomainIdentity{}, err
	}
	if successor && fresh {
		return DomainIdentity{}, fmt.Errorf("mixed witness successor lacks source enrollment")
	}
	objects, err := sourceAuthorityObjects(ctx, tx, successor)
	if err != nil {
		return DomainIdentity{}, err
	}
	if err := sourceAuthorityCheckOwnership(ctx, tx, roles, objects, fresh, session); err != nil {
		return DomainIdentity{}, err
	}
	oldGrants := sourceAuthorityGrants(roles, objects)
	if err := sourceAuthorityCheckACL(ctx, tx, roles, objects, oldGrants, fresh); err != nil {
		return DomainIdentity{}, err
	}
	if err := checkWitnessImmutableFunction(ctx, tx, sources[names[0]].sql); err != nil {
		return DomainIdentity{}, err
	}
	foundation := []string{"record_platform_domain_identity", "record_platform_domain_attestations",
		"deletion_witness_entries", "deletion_witness_head", "recovery_trust_witness_entries", "recovery_trust_witness_head"}
	if err := checkWitnessCatalog(ctx, tx, sources[names[0]].sql, "witness_foundation", foundation, nil); err != nil {
		return DomainIdentity{}, err
	}
	if err := checkWitnessCatalog(ctx, tx, sources[names[1]].sql, "source_", sourceAuthorityTables, sourceAuthorityFunctions); err != nil {
		return DomainIdentity{}, err
	}
	identity, err := provisionPostgresDomainIdentityTx(ctx, tx, DomainKindDeletionWitness, fresh)
	if err != nil {
		return DomainIdentity{}, err
	}
	if fresh {
		if _, err := tx.Exec(ctx, `insert into public.source_authority_state(singleton,generation,mode) values (true,1,'active')`); err != nil {
			return DomainIdentity{}, fmt.Errorf("enroll source authority state: %w", err)
		}
		if _, err := tx.Exec(ctx, `insert into public.source_authority_principals(principal,purpose,generation)
			select principal::name,purpose,1 from unnest($1::text[],$2::text[]) p(principal,purpose)`,
			[]string{roles.Reader, roles.Producer, roles.Provisioner}, []string{"reader", "producer", "provisioner"}); err != nil {
			return DomainIdentity{}, fmt.Errorf("enroll source authority principals: %w", err)
		}
	}
	if !successor {
		if err := applyWitnessMigrationTx(ctx, tx, recordBirthMigration, sources[recordBirthMigration]); err != nil {
			return DomainIdentity{}, err
		}
		objects, err = sourceAuthorityObjects(ctx, tx, true)
		if err != nil {
			return DomainIdentity{}, err
		}
		for _, object := range objects {
			if object.managed && object.kind != "sequence" && (fresh || isRecordBirthObject(object)) {
				if object.owner != session {
					return DomainIdentity{}, fmt.Errorf("unexpected new witness object owner for %s", object.identity)
				}
				if _, err := tx.Exec(ctx, "alter "+object.kind+" "+object.identity+" owner to "+pgx.Identifier{roles.Owner}.Sanitize()); err != nil {
					return DomainIdentity{}, fmt.Errorf("transfer source authority %s: %w", object.identity, err)
				}
			}
		}
		oldSet := make(map[sourceAuthorityGrant]bool, len(oldGrants))
		if !fresh {
			for _, grant := range oldGrants {
				oldSet[grant] = true
			}
		}
		for _, grant := range sourceAuthorityGrants(roles, objects) {
			if oldSet[grant] {
				continue
			}
			object := sourceAuthorityFindObject(objects, grant.kind, grant.oid)
			if _, err := tx.Exec(ctx, "grant "+grant.privilege+" on "+grant.kind+" "+object.identity+" to "+pgx.Identifier{grant.role}.Sanitize()); err != nil {
				return DomainIdentity{}, fmt.Errorf("grant source authority %s on %s: %w", grant.privilege, object.identity, err)
			}
		}
	}
	if err := sourceAuthorityCheckOwnership(ctx, tx, roles, objects, false, session); err != nil {
		return DomainIdentity{}, err
	}
	if err := sourceAuthorityCheckACL(ctx, tx, roles, objects, sourceAuthorityGrants(roles, objects), false); err != nil {
		return DomainIdentity{}, err
	}
	if err := checkWitnessCatalog(ctx, tx, sources[recordBirthMigration].sql, "record_birth_", recordBirthTables, recordBirthFunctions); err != nil {
		return DomainIdentity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DomainIdentity{}, fmt.Errorf("commit source authority convergence (resolve exact successor before retry): %w", err)
	}
	return identity, nil
}

func isRecordBirthObject(object sourceAuthorityObject) bool {
	for _, name := range recordBirthTables {
		if object.kind == "table" && object.identity == "public."+name {
			return true
		}
	}
	for _, name := range recordBirthFunctions {
		if object.kind == "function" && object.identity == name {
			return true
		}
	}
	return false
}

func applyWitnessMigrationTx(ctx context.Context, tx pgx.Tx, name string, source migrationSource) error {
	if _, err := tx.Exec(ctx, source.sql); err != nil {
		return fmt.Errorf("apply witness migration %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx, `insert into public.schema_migrations(name,checksum) values($1,$2)`, name, source.checksum); err != nil {
		return fmt.Errorf("record witness migration %q: %w", name, err)
	}
	return nil
}

func witnessLedgerTx(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	rows, err := tx.Query(ctx, `select name,checksum from public.schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read witness migration ledger: %w", err)
	}
	defer rows.Close()
	applied := make(map[string]string)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, err
		}
		applied[name] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}
