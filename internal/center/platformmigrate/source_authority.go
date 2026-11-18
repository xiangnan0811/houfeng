package platformmigrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SourceAuthorityRoles names pre-created, purpose-separated W2 principals.
// Provisioning neither creates roles nor changes their credentials or attributes.
type SourceAuthorityRoles struct {
	Owner         string
	Reader        string
	Producer      string
	Provisioner   string
	BirthProducer string
	BirthReader   string
}

func (roles SourceAuthorityRoles) names() []string {
	names := []string{roles.Owner, roles.Reader, roles.Producer, roles.Provisioner}
	if roles.BirthProducer != "" {
		names = append(names, roles.BirthProducer)
	}
	if roles.BirthReader != "" {
		names = append(names, roles.BirthReader)
	}
	return names
}

func (roles SourceAuthorityRoles) Validate() error {
	if (roles.BirthProducer == "") != (roles.BirthReader == "") {
		return fmt.Errorf("birth producer and reader roles must be supplied together")
	}
	seen := make(map[string]bool, 4)
	for _, name := range roles.names() {
		if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
			return fmt.Errorf("invalid source authority role identifier %q", name)
		}
		for _, ch := range name {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
				return fmt.Errorf("invalid source authority role identifier %q", name)
			}
		}
		if seen[name] {
			return fmt.Errorf("source authority roles must be distinct: %q", name)
		}
		seen[name] = true
	}
	return nil
}

// These are the objects introduced by witness migration 0002, not a prefix-based
// ownership grant. A new source entrypoint must be added here deliberately.
var sourceAuthorityTables = []string{
	"source_authority_state", "source_authority_principals",
	"source_retirement_prepares", "source_retirement_terminals",
	"source_retirement_head", "source_retirement_unknown",
	"source_authority_recoveries", "source_authority_credentials",
	"source_authority_handoff_events",
	"source_application_enrollments", "source_application_activations",
}

var sourceAuthorityFunctions = []string{
	"record_platform_internal.source_bytes(bytea)",
	"record_platform_internal.source_authority_lock(text,text)",
	"record_platform_internal.source_retirement_append(text,bytea,bytea,bytea,text)",
	"public.source_retirement_legacy_blocked(text,text,text,text)",
	"public.source_retirement_prepare(text,text,text,text,text,bytea)",
	"public.source_retirement_append(text,bytea,bytea,bytea)",
	"public.source_retirement_finalize(text,text,bytea,bytea,bytea)",
	"public.source_recovery_unknown(text,text)",
	"public.source_recovery_seal(text,bytea)",
	"public.source_authority_bind(name,text,bigint,text)",
	"public.source_recovery_begin(text,bytea,name)",
	"public.source_recovery_credentials(text,text[],text[],bytea[],bytea[],bytea)",
	"public.source_recovery_advance(text,text,text,bytea,bytea,bytea)",
	"public.source_authority_snapshot()",
	"public.source_enrollment_credentials(text[],text[],bytea[],bytea[])",
	"public.source_application_enroll(text,text,text,oid,text,bigint)",
	"public.source_application_activate(bytea)",
}

// The successor is an exact extension of the frozen 0002 inventory.
var recordBirthTables = []string{
	"record_birth_keys", "record_birth_lineage", "record_birth_bindings", "record_birth_enabled",
	"record_birth_principals", "record_birth_requests", "record_birth_head", "record_birth_events",
	"record_birth_checkpoint_anchor", "record_birth_request_coverage",
}

var recordBirthFunctions = []string{
	"record_platform_internal.record_birth_command(bytea,text[])",
	"record_platform_internal.record_birth_admit(jsonb,boolean)",
	"record_platform_internal.record_birth_checkpoint_decode(bytea)",
	"record_platform_internal.record_birth_checkpoint_accept(jsonb,public.record_birth_bindings)",
	"record_platform_internal.record_birth_binding_bytes(public.record_birth_bindings)",
	"record_platform_internal.record_birth_encode(public.record_birth_bindings,public.record_birth_requests,text,bigint,bytea,bytea)",
	"record_platform_internal.record_birth_verify(public.record_birth_events)",
	"record_platform_internal.record_birth_append(public.record_birth_bindings,public.record_birth_requests,text,bytea)",
	"record_platform_internal.record_birth_transition(bytea,text)",
	"public.record_birth_reserve(bytea)", "public.record_birth_prepare(bytea)",
	"public.record_birth_confirm(bytea)", "public.record_birth_observe(bytea)",
	"public.record_birth_continuity(bytea)", "public.record_birth_observe_request(bytea)",
}

const sourceAuthoritySequence = "public.source_retirement_prepares_prepare_sequence_seq"

type sourceAuthorityObject struct {
	kind     string
	identity string
	oid      uint32
	owner    string
	managed  bool
}

type sourceAuthorityGrant struct {
	kind      string
	oid       uint32
	role      string
	privilege string
}

// ProvisionSourceAuthority atomically converges a dedicated W2 witness to the
// exact disabled birth successor. It never enables a birth binding or revives a
// recovery generation; the caller remains responsible for the offline fence.
func ProvisionSourceAuthority(ctx context.Context, db *pgxpool.Pool, roles SourceAuthorityRoles) (DomainIdentity, error) {
	return convergeSourceAuthority(ctx, db, roles)
}

func sourceAuthorityAllowedRelations() []string {
	names := append([]string{}, sourceAuthorityTables...)
	names = append(names, recordBirthTables...)
	return append(names, "source_retirement_prepares_prepare_sequence_seq", "schema_migrations",
		"record_platform_domain_identity", "record_platform_domain_attestations",
		"deletion_witness_entries", "deletion_witness_head", "recovery_trust_witness_entries", "recovery_trust_witness_head")
}

func sourceAuthorityEnrollment(ctx context.Context, tx pgx.Tx, roles SourceAuthorityRoles) (bool, error) {
	var generation int64
	var mode string
	var safe bool
	err := tx.QueryRow(ctx, `select generation, mode, recovery_id is null or exists (
		select 1 from public.source_authority_recoveries r where r.recovery_id = s.recovery_id and r.phase = 'completed')
		from public.source_authority_state s where singleton`).Scan(&generation, &mode, &safe)
	fresh := errors.Is(err, pgx.ErrNoRows)
	if err != nil && !fresh {
		return false, fmt.Errorf("read source authority enrollment: %w", err)
	}
	if !fresh && (mode != "active" || !safe) {
		return false, fmt.Errorf("source authority recovery handoff interlock forbids ordinary provisioning")
	}
	var matches, initial, total int
	if err := tx.QueryRow(ctx, `select count(*) filter (where generation = 1 and recovery_id is null and
		((principal = $1 and purpose = 'reader') or (principal = $2 and purpose = 'producer')
		or (principal = $3 and purpose = 'provisioner'))),
		count(*) filter (where generation = 1), count(*) from public.source_authority_principals`,
		roles.Reader, roles.Producer, roles.Provisioner).Scan(&matches, &initial, &total); err != nil {
		return false, fmt.Errorf("read original source authority bindings: %w", err)
	}
	if fresh {
		var empty bool
		if err := tx.QueryRow(ctx, `select not exists (select 1 from public.source_retirement_prepares)
			and not exists (select 1 from public.source_retirement_terminals)
			and not exists (select 1 from public.source_retirement_unknown)
			and not exists (select 1 from public.source_authority_recoveries)
			and not exists (select 1 from public.source_authority_credentials)
			and not exists (select 1 from public.source_authority_handoff_events)
			and not exists (select 1 from public.source_application_enrollments)
			and not exists (select 1 from public.source_application_activations)
			and (select count(*) = 1 and bool_and(sequence = 0 and entry_hash = decode(repeat('00',32),'hex'))
			from public.source_retirement_head)`).Scan(&empty); err != nil {
			return false, fmt.Errorf("inspect fresh source authority state: %w", err)
		}
		if total != 0 || !empty {
			return false, fmt.Errorf("unenrolled source authority contains preexisting history or principals")
		}
	} else if matches != 3 || initial != 3 {
		return false, fmt.Errorf("source authority original principal mapping does not exactly match")
	}
	return fresh, nil
}

func sourceAuthorityCheckRoles(ctx context.Context, tx pgx.Tx, roles SourceAuthorityRoles) error {
	attributes, err := readAppRoleAttributes(ctx, tx, roles.names())
	if err != nil {
		return err
	}
	for _, name := range roles.names() {
		a, ok := attributes[name]
		if !ok {
			return fmt.Errorf("missing pre-created source authority role %q", name)
		}
		if a.CanLogin != (name != roles.Owner) || a.Inherit || a.Superuser || a.CreateDB || a.CreateRole || a.Replication || a.BypassRLS {
			return fmt.Errorf("source authority role %q must be constrained NOINHERIT with owner NOLOGIN and principals LOGIN", name)
		}
	}
	var memberships bool
	if err := tx.QueryRow(ctx, `select exists (select 1 from pg_catalog.pg_auth_members m
		join pg_catalog.pg_roles r on r.oid = m.member or r.oid = m.roleid
		where r.rolname = any($1::name[]))`, roles.names()).Scan(&memberships); err != nil {
		return fmt.Errorf("inspect source authority memberships: %w", err)
	}
	if memberships {
		return fmt.Errorf("source authority role membership in either direction is forbidden")
	}
	return nil
}

func sourceAuthorityObjects(ctx context.Context, tx pgx.Tx, successor bool) ([]sourceAuthorityObject, error) {
	objects := make([]sourceAuthorityObject, 0, len(sourceAuthorityTables)+len(sourceAuthorityFunctions)+len(recordBirthTables)+len(recordBirthFunctions)+12)
	for _, name := range sourceAuthorityTables {
		objects = append(objects, sourceAuthorityObject{kind: "table", identity: "public." + name, managed: true})
	}
	objects = append(objects, sourceAuthorityObject{kind: "sequence", identity: sourceAuthoritySequence, managed: true})
	for _, name := range sourceAuthorityFunctions {
		objects = append(objects, sourceAuthorityObject{kind: "function", identity: name, managed: true})
	}
	if successor {
		for _, name := range recordBirthTables {
			objects = append(objects, sourceAuthorityObject{kind: "table", identity: "public." + name, managed: true})
		}
		for _, name := range recordBirthFunctions {
			objects = append(objects, sourceAuthorityObject{kind: "function", identity: name, managed: true})
		}
	}
	for _, name := range []string{"public.record_platform_domain_identity", "public.deletion_witness_entries"} {
		objects = append(objects, sourceAuthorityObject{kind: "table", identity: name})
	}
	for _, name := range []string{"pg_catalog.pg_control_system()", "record_platform_internal.reject_immutable_mutation()", "record_platform_internal.digest(bytea,text)"} {
		objects = append(objects, sourceAuthorityObject{kind: "function", identity: name})
	}
	if successor {
		for _, name := range []string{"record_platform_internal.hmac(bytea,bytea,text)", "record_platform_internal.gen_random_bytes(integer)"} {
			objects = append(objects, sourceAuthorityObject{kind: "function", identity: name})
		}
	}
	for _, name := range []string{"public", "record_platform_internal"} {
		objects = append(objects, sourceAuthorityObject{kind: "schema", identity: name})
	}
	var database string
	if err := tx.QueryRow(ctx, `select current_database()`).Scan(&database); err != nil {
		return nil, fmt.Errorf("read witness database name: %w", err)
	}
	objects = append(objects, sourceAuthorityObject{kind: "database", identity: pgx.Identifier{database}.Sanitize()})
	for i := range objects {
		o := &objects[i]
		var err error
		switch o.kind {
		case "table", "sequence":
			err = tx.QueryRow(ctx, `select c.oid, r.rolname from pg_catalog.pg_class c join pg_catalog.pg_roles r on r.oid = c.relowner
				where c.oid = to_regclass($1) and c.relkind = $2`, o.identity, map[string]string{"table": "r", "sequence": "S"}[o.kind]).Scan(&o.oid, &o.owner)
		case "function":
			err = tx.QueryRow(ctx, `select p.oid, r.rolname from pg_catalog.pg_proc p join pg_catalog.pg_roles r on r.oid = p.proowner
				where p.oid = to_regprocedure($1) and (not $2 or p.proconfig = array['search_path=pg_catalog']::text[])
				and (not $2 or p.proname = 'source_bytes' or p.prosecdef)`, o.identity, o.managed).Scan(&o.oid, &o.owner)
		case "schema":
			err = tx.QueryRow(ctx, `select n.oid, r.rolname from pg_catalog.pg_namespace n join pg_catalog.pg_roles r on r.oid = n.nspowner where n.nspname = $1`, o.identity).Scan(&o.oid, &o.owner)
		case "database":
			err = tx.QueryRow(ctx, `select d.oid, r.rolname from pg_catalog.pg_database d join pg_catalog.pg_roles r on r.oid = d.datdba where d.datname = current_database()`).Scan(&o.oid, &o.owner)
		}
		if err != nil {
			return nil, fmt.Errorf("inspect exact source authority %s %s: %w", o.kind, o.identity, err)
		}
	}
	// Unknown overloads or objects must never accidentally acquire owner authority.
	var relationCount, functionCount, birthRelations, birthFunctions int
	if err := tx.QueryRow(ctx, `select
		(select count(*) from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid = c.relnamespace
		 where n.nspname in ('public','record_platform_internal') and starts_with(c.relname, 'source_') and c.relkind in ('r','p','S','v','m','f')),
		(select count(*) from pg_catalog.pg_proc p join pg_catalog.pg_namespace n on n.oid = p.pronamespace
		 where n.nspname in ('public','record_platform_internal') and starts_with(p.proname, 'source_')),
		(select count(*) from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid = c.relnamespace
		 where n.nspname in ('public','record_platform_internal') and starts_with(c.relname, 'record_birth_') and c.relkind in ('r','p','S','v','m','f')),
		(select count(*) from pg_catalog.pg_proc p join pg_catalog.pg_namespace n on n.oid = p.pronamespace
		 where n.nspname in ('public','record_platform_internal') and starts_with(p.proname, 'record_birth_'))`).Scan(&relationCount, &functionCount, &birthRelations, &birthFunctions); err != nil {
		return nil, fmt.Errorf("inspect managed source authority catalog coverage: %w", err)
	}
	if relationCount != len(sourceAuthorityTables)+1 || functionCount != len(sourceAuthorityFunctions) {
		return nil, fmt.Errorf("source authority managed catalog set differs from witness 0002")
	}
	wantBirthRelations, wantBirthFunctions := 0, 0
	if successor {
		wantBirthRelations, wantBirthFunctions = len(recordBirthTables), len(recordBirthFunctions)
	}
	if birthRelations != wantBirthRelations || birthFunctions != wantBirthFunctions {
		return nil, fmt.Errorf("record birth managed catalog set differs from witness migration ledger")
	}
	return objects, nil
}

func sourceAuthorityFindObject(objects []sourceAuthorityObject, kind string, oid uint32) sourceAuthorityObject {
	for _, object := range objects {
		if object.kind == kind && object.oid == oid {
			return object
		}
	}
	return sourceAuthorityObject{}
}

func sourceAuthorityCheckOwnership(ctx context.Context, tx pgx.Tx, roles SourceAuthorityRoles, objects []sourceAuthorityObject, fresh bool, bootstrap string) error {
	var classes []string
	var ids []uint32
	for _, object := range objects {
		if !object.managed {
			continue
		}
		class := "pg_catalog.pg_class"
		query := `select r.rolname from pg_catalog.pg_class c join pg_catalog.pg_roles r on r.oid = c.relowner where c.oid = $1`
		if object.kind == "function" {
			class = "pg_catalog.pg_proc"
			query = `select r.rolname from pg_catalog.pg_proc p join pg_catalog.pg_roles r on r.oid = p.proowner where p.oid = $1`
		}
		var owner string
		if err := tx.QueryRow(ctx, query, object.oid).Scan(&owner); err != nil {
			return fmt.Errorf("read source authority object owner: %w", err)
		}
		expected := roles.Owner
		if fresh {
			expected = bootstrap
		}
		if owner != expected {
			return fmt.Errorf("source authority owner drift on %s: got %q, want %q", object.identity, owner, expected)
		}
		classes = append(classes, class)
		ids = append(ids, object.oid)
	}
	// Include only PostgreSQL's internal/automatic dependents (row/array types,
	// indexes, TOAST, identity sequence), not unrelated objects with normal refs.
	var invalid bool
	if err := tx.QueryRow(ctx, `with recursive managed(classid, objid) as (
		select class::regclass::oid, id from unnest($1::text[], $2::oid[]) as x(class,id)
		union
		select d.classid, d.objid from pg_catalog.pg_depend d join managed m
		on d.refclassid = m.classid and d.refobjid = m.objid where d.deptype in ('i','a')
	) select exists (select 1 from pg_catalog.pg_shdepend d join pg_catalog.pg_roles r on r.oid = d.refobjid
		where d.refclassid = 'pg_catalog.pg_authid'::regclass and d.deptype = 'o'
		and r.rolname = any($3::name[]) and not (r.rolname = $4 and d.dbid = (select oid from pg_database where datname = current_database())
		and exists (select 1 from managed m where m.classid = d.classid and m.objid = d.objid)))`,
		classes, ids, roles.names(), roles.Owner).Scan(&invalid); err != nil {
		return fmt.Errorf("inspect source authority catalog ownership: %w", err)
	}
	if invalid {
		return fmt.Errorf("source authority role owns an unrelated object or a login principal owns an object")
	}
	return nil
}

func sourceAuthorityGrants(roles SourceAuthorityRoles, objects []sourceAuthorityObject) []sourceAuthorityGrant {
	var grants []sourceAuthorityGrant
	add := func(object sourceAuthorityObject, role, privilege string) {
		grants = append(grants, sourceAuthorityGrant{kind: object.kind, oid: object.oid, role: role, privilege: privilege})
	}
	successor := false
	for _, object := range objects {
		if object.identity == "public.record_birth_observe(bytea)" {
			successor = true
		}
	}
	for _, object := range objects {
		if object.kind == "database" {
			for _, role := range []string{roles.Reader, roles.Producer, roles.Provisioner} {
				add(object, role, "CONNECT")
			}
			if successor && roles.BirthProducer != "" {
				add(object, roles.BirthProducer, "CONNECT")
				add(object, roles.BirthReader, "CONNECT")
			}
		}
		if object.kind == "schema" {
			add(object, roles.Owner, "USAGE")
			if object.identity == "public" {
				for _, role := range []string{roles.Reader, roles.Producer, roles.Provisioner} {
					add(object, role, "USAGE")
				}
				if successor && roles.BirthProducer != "" {
					add(object, roles.BirthProducer, "USAGE")
					add(object, roles.BirthReader, "USAGE")
				}
			}
		}
		if object.kind == "table" && (object.identity == "public.record_platform_domain_identity" || object.identity == "public.deletion_witness_entries") {
			add(object, roles.Owner, "SELECT")
		}
		if object.kind == "table" && (object.identity == "public.source_retirement_prepares" || object.identity == "public.source_retirement_terminals" || object.identity == "public.source_retirement_unknown" || object.identity == "public.deletion_witness_entries" || object.identity == "public.source_application_enrollments" || object.identity == "public.source_application_activations") {
			for _, role := range []string{roles.Reader, roles.Producer, roles.Provisioner} {
				add(object, role, "SELECT")
			}
		}
		if object.kind == "table" && (object.identity == "public.source_authority_credentials" || object.identity == "public.source_authority_principals") {
			add(object, roles.Reader, "SELECT")
		}
		if object.kind == "function" && !object.managed {
			add(object, roles.Owner, "EXECUTE")
		}
		if object.identity == "public.source_authority_snapshot()" || object.identity == "public.source_retirement_legacy_blocked(text,text,text,text)" {
			for _, role := range []string{roles.Reader, roles.Producer, roles.Provisioner} {
				add(object, role, "EXECUTE")
			}
		}
		if object.identity == "public.source_retirement_prepare(text,text,text,text,text,bytea)" || object.identity == "public.source_retirement_append(text,bytea,bytea,bytea)" {
			add(object, roles.Producer, "EXECUTE")
		}
		if successor && roles.BirthProducer != "" {
			switch object.identity {
			case "public.record_birth_reserve(bytea)", "public.record_birth_prepare(bytea)", "public.record_birth_confirm(bytea)":
				add(object, roles.BirthProducer, "EXECUTE")
			case "public.record_birth_observe(bytea)", "public.record_birth_observe_request(bytea)":
				add(object, roles.BirthProducer, "EXECUTE")
				add(object, roles.BirthReader, "EXECUTE")
			case "public.record_birth_continuity(bytea)":
				add(object, roles.BirthReader, "EXECUTE")
			}
		}
		if object.identity == "public.source_authority_bind(name,text,bigint,text)" ||
			object.identity == "public.source_recovery_begin(text,bytea,name)" ||
			object.identity == "public.source_enrollment_credentials(text[],text[],bytea[],bytea[])" ||
			object.identity == "public.source_application_enroll(text,text,text,oid,text,bigint)" ||
			object.identity == "public.source_application_activate(bytea)" {
			add(object, roles.Provisioner, "EXECUTE")
		}
	}
	return grants
}

// Inspect direct ACLs instead of treating PostgreSQL's ordinary PUBLIC TEMP,
// CONNECT, schema USAGE and built-in EXECUTE as dedicated role grants. PUBLIC
// managed privileges and any effective schema/database CREATE remain forbidden.
func sourceAuthorityCheckACL(ctx context.Context, tx pgx.Tx, roles SourceAuthorityRoles, objects []sourceAuthorityObject, grants []sourceAuthorityGrant, fresh bool) error {
	expected := make(map[sourceAuthorityGrant]bool, len(grants))
	if !fresh {
		for _, grant := range grants {
			expected[grant] = false
		}
	}
	rows, err := tx.Query(ctx, `with acl_objects(kind,oid,owner,acl,managed_namespace,definer) as (
		select 'table', c.oid, c.relowner, c.relacl, n.nspname not in ('pg_catalog','information_schema') and n.nspname !~ '^pg_toast', false
		from pg_class c join pg_namespace n on n.oid=c.relnamespace where c.relkind <> 'S'
		union all select 'sequence', c.oid,c.relowner,c.relacl,false,false from pg_class c where c.relkind='S'
		union all select 'function',p.oid,p.proowner,p.proacl,n.nspname not in ('pg_catalog','information_schema'),p.prosecdef
		from pg_proc p join pg_namespace n on n.oid=p.pronamespace
		union all select 'schema',oid,nspowner,nspacl,nspname not in ('pg_catalog','information_schema') and nspname !~ '^pg_',false from pg_namespace
		union all select 'database',oid,datdba,datacl,false,false from pg_database
	) select a.kind,a.oid,coalesce(r.rolname::text,'PUBLIC'),x.privilege_type,x.is_grantable,a.owner,
		a.managed_namespace,a.definer from acl_objects a cross join lateral aclexplode(a.acl) x
		left join pg_roles r on r.oid=x.grantee where x.grantee=0 or r.rolname=any($1::name[])`, roles.names())
	if err != nil {
		return fmt.Errorf("inspect source authority ACLs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var grant sourceAuthorityGrant
		var grantable, userNamespace, definer bool
		var owner uint32
		if err := rows.Scan(&grant.kind, &grant.oid, &grant.role, &grant.privilege, &grantable, &owner, &userNamespace, &definer); err != nil {
			return fmt.Errorf("scan source authority ACL: %w", err)
		}
		object := sourceAuthorityFindObject(objects, grant.kind, grant.oid)
		if grant.role == "PUBLIC" {
			if object.managed || (object.oid != 0 && (object.kind == "table" || (object.kind == "function" && userNamespace))) ||
				(grant.kind == "table" && userNamespace) ||
				(object.kind == "schema" && object.identity == "record_platform_internal") ||
				(grant.kind == "schema" && userNamespace && grant.privilege == "CREATE") ||
				(grant.kind == "database" && object.oid != 0 && grant.privilege == "CREATE") ||
				(grant.kind == "function" && userNamespace && definer) {
				return fmt.Errorf("source authority has unsafe PUBLIC %s privilege on %s %s (OID %d)", grant.privilege, grant.kind, object.identity, grant.oid)
			}
			continue
		}
		if object.managed && grant.role == roles.Owner && !fresh {
			continue // Ownership supplies the managed object's inherent privileges.
		}
		if _, ok := expected[grant]; !ok || grantable {
			return fmt.Errorf("unexpected source authority privilege: %s %s for %q", grant.kind, grant.privilege, grant.role)
		}
		expected[grant] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source authority ACLs: %w", err)
	}
	rows.Close()
	for grant, found := range expected {
		if !found {
			return fmt.Errorf("missing source authority privilege: %s %s for %q", grant.kind, grant.privilege, grant.role)
		}
	}
	// Column ACLs, default privileges, foreign servers, types and other grant
	// classes are not part of this boundary. Reject them, rather than erase them.
	var unexpected bool
	var protectedRelations []uint32
	for _, object := range objects {
		if object.kind == "table" {
			protectedRelations = append(protectedRelations, object.oid)
		}
	}
	if err := tx.QueryRow(ctx, `select exists (
		select 1 from pg_shdepend d join pg_roles r on r.oid=d.refobjid
		where d.refclassid='pg_authid'::regclass and d.deptype='a' and r.rolname=any($1::name[])
		and (d.classid not in ('pg_class'::regclass,'pg_proc'::regclass,'pg_namespace'::regclass,'pg_database'::regclass)
		or d.objsubid<>0 or (d.dbid<>0 and d.dbid<>(select oid from pg_database where datname=current_database())))
		) or exists (select 1 from pg_attribute a cross join lateral aclexplode(a.attacl) x
		join pg_class c on c.oid=a.attrelid join pg_namespace n on n.oid=c.relnamespace
		left join pg_roles r on r.oid=x.grantee
		where r.rolname=any($1::name[]) or a.attrelid=any($2::oid[])
		or (x.grantee=0 and n.nspname not in ('pg_catalog','information_schema') and n.nspname !~ '^pg_'))`,
		roles.names(), protectedRelations).Scan(&unexpected); err != nil {
		return fmt.Errorf("inspect nonstandard source authority grants: %w", err)
	}
	if unexpected {
		return fmt.Errorf("source authority has unexpected column, cross-database or nonstandard privileges")
	}
	// No third party may retain access to a W2 object after its ownership moves.
	var managedRelations, managedFunctions []uint32
	for _, object := range objects {
		if object.managed {
			if object.kind == "function" {
				managedFunctions = append(managedFunctions, object.oid)
			} else {
				managedRelations = append(managedRelations, object.oid)
			}
		}
	}
	if err := tx.QueryRow(ctx, `select exists (
		select 1 from (select relowner as owner,relacl as acl from pg_class where oid=any($1::oid[])
		union all select proowner,proacl from pg_proc where oid=any($2::oid[])) o
		cross join lateral aclexplode(o.acl) x left join pg_roles r on r.oid=x.grantee
		where x.grantee<>o.owner and (r.rolname is null or r.rolname<>all($3::name[])))`, managedRelations, managedFunctions, roles.names()).Scan(&unexpected); err != nil {
		return fmt.Errorf("inspect third-party source authority grants: %w", err)
	}
	if unexpected {
		return fmt.Errorf("source authority managed objects have unexpected third-party privileges")
	}
	return nil
}
