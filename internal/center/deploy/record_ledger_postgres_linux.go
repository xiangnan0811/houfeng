//go:build linux

package deploy

import (
	"context"
	"net"
	"sort"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recorddeletion"
)

type ledgerQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type ledgerDatabase struct {
	request    RecordLedgerDomainRequest
	admin      *pgxpool.Pool
	reader     *pgxpool.Pool
	writer     *pgxpool.Pool
	writerPID  atomic.Int32
	adminPID   int32
	adminRole  string
	readerRole string
	writerRole string
	previous   []string
}

func ledgerParsePool(url string) (*pgxpool.Config, error) {
	config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(url)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	config.MaxConns = 1
	config.MinConns = 0
	return config, nil
}

func ledgerSafeRole(name string) bool {
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := range name {
		c := name[i]
		if !(c == '_' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func ledgerDomainIdentity(ctx context.Context, q ledgerQueryer, expected recorddeletion.LedgerPhysicalIdentity) error {
	var actual recorddeletion.LedgerPhysicalIdentity
	var mode, liveSystem, liveName string
	var epoch int64
	var liveOID uint32
	var one bool
	err := q.QueryRow(ctx, `select d.domain_id,d.domain_kind,d.identity_epoch,d.identity_mode,
		coalesce(d.postgres_system_identifier,''),d.database_oid,d.database_name,
		s.system_identifier::text,db.oid,db.datname,
		(select count(*)=1 from public.record_platform_domain_identity)
		from public.record_platform_domain_identity d cross join pg_catalog.pg_control_system() s
		join pg_catalog.pg_database db on db.datname=pg_catalog.current_database()`).Scan(
		&actual.ID, &actual.Kind, &epoch, &mode, &actual.SystemIdentifier, &actual.DatabaseOID, &actual.DatabaseName,
		&liveSystem, &liveOID, &liveName, &one)
	if err != nil || epoch <= 0 {
		return ErrRecordLedgerFence
	}
	actual.Epoch = uint64(epoch)
	if !one || mode != "postgres_system" || actual != expected || liveSystem != actual.SystemIdentifier || liveOID != actual.DatabaseOID || liveName != actual.DatabaseName {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerAppIdentity(ctx context.Context, q ledgerQueryer, expected RecordLedgerApplicationIdentity) error {
	var id, kind, mode, system, name, liveSystem, liveName string
	var epoch int64
	var oid, liveOID uint32
	var one bool
	err := q.QueryRow(ctx, `select d.domain_id,d.domain_kind,d.identity_epoch,d.identity_mode,
		coalesce(d.postgres_system_identifier,''),d.database_oid,d.database_name,
		s.system_identifier::text,db.oid,db.datname,
		(select count(*)=1 from public.record_platform_domain_identity)
		from public.record_platform_domain_identity d cross join pg_catalog.pg_control_system() s
		join pg_catalog.pg_database db on db.datname=pg_catalog.current_database()`).Scan(
		&id, &kind, &epoch, &mode, &system, &oid, &name, &liveSystem, &liveOID, &liveName, &one)
	if err != nil || epoch <= 0 || !one || kind != "application" || mode != "postgres_system" ||
		id != expected.ID || system != expected.SystemIdentifier || oid != expected.DatabaseOID || name != expected.DatabaseName || uint64(epoch) != expected.Epoch ||
		liveSystem != system || liveOID != oid || liveName != name {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerDirectRole(ctx context.Context, q ledgerQueryer, expected string, bootstrap bool) error {
	var session, current, roleSetting string
	var oid uint32
	var super, login, inherit, createRole, createDB, replication, bypass, member bool
	err := q.QueryRow(ctx, `select session_user::text,current_user::text,pg_catalog.current_setting('role'),r.oid,
		r.rolsuper,r.rolcanlogin,r.rolinherit,r.rolcreaterole,r.rolcreatedb,r.rolreplication,r.rolbypassrls,
		exists(select 1 from pg_catalog.pg_auth_members m where m.roleid=r.oid or m.member=r.oid)
		from pg_catalog.pg_roles r where r.rolname=session_user`).Scan(&session, &current, &roleSetting, &oid, &super, &login, &inherit, &createRole, &createDB, &replication, &bypass, &member)
	if err != nil || session != expected || current != expected || roleSetting != "none" || !login {
		return ErrRecordLedgerFence
	}
	if bootstrap {
		if !super || oid != 10 {
			return ErrRecordLedgerFence
		}
	} else if super || inherit || createRole || createDB || replication || bypass || member {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerOpenDomain(ctx context.Context, request RecordLedgerDomainRequest) (*ledgerDatabase, error) {
	if request.Identity.Validate() != nil {
		return nil, ErrRecordLedgerFence
	}
	admin, err := ledgerParsePool(request.BootstrapURL)
	if err != nil {
		return nil, err
	}
	reader, err := ledgerParsePool(request.ReaderURL)
	if err != nil {
		return nil, err
	}
	writer, err := ledgerParsePool(request.WriterURL)
	if err != nil {
		return nil, err
	}
	roles := []string{admin.ConnConfig.User, reader.ConnConfig.User, writer.ConnConfig.User}
	for _, role := range roles {
		if !ledgerSafeRole(role) {
			return nil, ErrRecordLedgerFence
		}
	}
	if roles[0] == roles[1] || roles[0] == roles[2] || roles[1] == roles[2] ||
		admin.ConnConfig.Database != request.Identity.DatabaseName || reader.ConnConfig.Database != request.Identity.DatabaseName || writer.ConnConfig.Database != request.Identity.DatabaseName ||
		admin.ConnConfig.Host != reader.ConnConfig.Host || admin.ConnConfig.Host != writer.ConnConfig.Host || admin.ConnConfig.Port != reader.ConnConfig.Port || admin.ConnConfig.Port != writer.ConnConfig.Port {
		return nil, ErrRecordLedgerFence
	}
	previous := append([]string(nil), request.PreviousWriters...)
	sort.Strings(previous)
	for i, role := range previous {
		if !ledgerSafeRole(role) || role == roles[0] || role == roles[1] || role == roles[2] || i > 0 && role == previous[i-1] {
			return nil, ErrRecordLedgerFence
		}
	}
	admin.AfterConnect = func(c context.Context, conn *pgx.Conn) error {
		if ledgerDirectRole(c, conn, roles[0], true) != nil {
			return ErrRecordLedgerFence
		}
		return ledgerDomainIdentity(c, conn, request.Identity)
	}
	pool, err := pgxpool.NewWithConfig(ctx, admin)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, ErrRecordLedgerFence
	}
	var pid int32
	if pool.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&pid) != nil || pid == 0 {
		pool.Close()
		return nil, ErrRecordLedgerFence
	}
	return &ledgerDatabase{request: request, admin: pool, adminPID: pid, adminRole: roles[0], readerRole: roles[1], writerRole: roles[2], previous: previous}, nil
}

func (db *ledgerDatabase) close() {
	if db == nil {
		return
	}
	if db.writer != nil {
		db.writer.Close()
	}
	if db.reader != nil {
		db.reader.Close()
	}
	if db.admin != nil {
		db.admin.Close()
	}
}

func (db *ledgerDatabase) tables() (string, string) {
	if db.request.Identity.Kind == "deletion_witness" {
		return "public.deletion_witness_entries", "public.deletion_witness_head"
	}
	return "public.deletion_ledger_entries", "public.deletion_ledger_head"
}

func (db *ledgerDatabase) checkOldRole(ctx context.Context, name string) error {
	var safe bool
	err := db.admin.QueryRow(ctx, `select not r.rolsuper and not r.rolinherit and not r.rolcreaterole
		and not r.rolcreatedb and not r.rolreplication and not r.rolbypassrls
		and not exists(select 1 from pg_catalog.pg_auth_members m where m.member=r.oid or m.roleid=r.oid)
		and not exists(select 1 from pg_catalog.pg_shdepend d where d.refclassid='pg_catalog.pg_authid'::regclass and d.refobjid=r.oid and d.deptype='o')
		from pg_catalog.pg_roles r where r.rolname=$1`, name).Scan(&safe)
	if err != nil || !safe {
		return ErrRecordLedgerFence
	}
	return nil
}

func (db *ledgerDatabase) checkWriterInventory(ctx context.Context, fenced bool) error {
	entries, head := db.tables()
	rows, err := db.admin.Query(ctx, `with recursive member_roles(login_oid,role_oid) as (
		select oid,oid from pg_catalog.pg_roles where rolcanlogin
		union select m.login_oid,a.roleid from member_roles m join pg_catalog.pg_auth_members a on a.member=m.role_oid
	) select distinct login.rolname from member_roles m
	join pg_catalog.pg_roles login on login.oid=m.login_oid join pg_catalog.pg_roles role on role.oid=m.role_oid
	where role.rolsuper or role.rolcreaterole or role.rolreplication
	or pg_catalog.has_table_privilege(role.oid,$1,'INSERT') or pg_catalog.has_any_column_privilege(role.oid,$1,'INSERT')
	or pg_catalog.has_table_privilege(role.oid,$2,'UPDATE') or pg_catalog.has_any_column_privilege(role.oid,$2,'UPDATE')`, entries, head)
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer rows.Close()
	allowed := map[string]bool{db.adminRole: true, db.writerRole: true}
	if !fenced {
		for _, role := range db.previous {
			allowed[role] = true
		}
	}
	for rows.Next() {
		var role string
		if rows.Scan(&role) != nil || !allowed[role] {
			return ErrRecordLedgerFence
		}
	}
	if rows.Err() != nil {
		return ErrRecordLedgerFence
	}
	rows.Close()
	// A SECURITY DEFINER entry point can confer a writer's authority even when
	// the calling login has no table grant. Inspect every non-system function
	// whose owner can write either protected relation, including PUBLIC grants.
	rows, err = db.admin.Query(ctx, `with recursive member_roles(login_oid,role_oid) as (
		select oid,oid from pg_catalog.pg_roles where rolcanlogin
		union select m.login_oid,a.roleid from member_roles m join pg_catalog.pg_auth_members a on a.member=m.role_oid
	) select distinct login.rolname from member_roles m
	join pg_catalog.pg_roles login on login.oid=m.login_oid
	join pg_catalog.pg_proc p on p.prosecdef
	join pg_catalog.pg_namespace n on n.oid=p.pronamespace
	where n.nspname not in ('pg_catalog','information_schema') and
	(pg_catalog.has_table_privilege(p.proowner,$1,'INSERT') or pg_catalog.has_any_column_privilege(p.proowner,$1,'INSERT')
	 or pg_catalog.has_table_privilege(p.proowner,$2,'UPDATE') or pg_catalog.has_any_column_privilege(p.proowner,$2,'UPDATE'))
	and pg_catalog.has_function_privilege(m.role_oid,p.oid,'EXECUTE')`, entries, head)
	if err != nil {
		return ErrRecordLedgerFence
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if rows.Scan(&role) != nil || !allowed[role] {
			return ErrRecordLedgerFence
		}
	}
	if rows.Err() != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func (db *ledgerDatabase) oldWriterFunctions(ctx context.Context, role string) ([]string, error) {
	entries, head := db.tables()
	rows, err := db.admin.Query(ctx, `select p.oid::pg_catalog.regprocedure::text from pg_catalog.pg_proc p
		join pg_catalog.pg_namespace n on n.oid=p.pronamespace
		where p.prosecdef and n.nspname not in ('pg_catalog','information_schema')
		and (pg_catalog.has_table_privilege(p.proowner,$2,'INSERT') or pg_catalog.has_any_column_privilege(p.proowner,$2,'INSERT')
		 or pg_catalog.has_table_privilege(p.proowner,$3,'UPDATE') or pg_catalog.has_any_column_privilege(p.proowner,$3,'UPDATE'))
		and pg_catalog.has_function_privilege($1,p.oid,'EXECUTE')`, role, entries, head)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) != nil {
			rows.Close()
			return nil, ErrRecordLedgerFence
		}
		names = append(names, name)
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, ErrRecordLedgerFence
	}
	rows.Close()
	return names, nil
}

func (db *ledgerDatabase) fenceWriters(ctx context.Context) error {
	if err := db.checkWriterInventory(ctx, false); err != nil {
		return err
	}
	entries, head := db.tables()
	for _, role := range db.previous {
		if err := db.checkOldRole(ctx, role); err != nil {
			return err
		}
		// Commit NOLOGIN separately so a later ACL/drain error cannot reopen it.
		if _, err := db.admin.Exec(ctx, "alter role "+pgx.Identifier{role}.Sanitize()+" nologin"); err != nil {
			return ErrRecordLedgerFence
		}
		if _, err := db.admin.Exec(ctx, "revoke all privileges on table "+entries+", "+head+" from "+pgx.Identifier{role}.Sanitize()); err != nil {
			return ErrRecordLedgerFence
		}
		functions, err := db.oldWriterFunctions(ctx, role)
		if err != nil {
			return err
		}
		for _, name := range functions {
			if _, err := db.admin.Exec(ctx, "revoke execute on function "+name+" from "+pgx.Identifier{role}.Sanitize()); err != nil {
				return ErrRecordLedgerFence
			}
		}
	}
	roles := append(append([]string(nil), db.previous...), db.writerRole, db.adminRole)
	if err := db.terminateSessions(ctx, roles); err != nil {
		return err
	}
	return db.verifyFenced(ctx)
}

func (db *ledgerDatabase) terminateSessions(ctx context.Context, roles []string) error {
	rows, err := db.admin.Query(ctx, `select pg_catalog.pg_terminate_backend(pid) from pg_catalog.pg_stat_activity
		where usename=any($1::name[]) and pid<>pg_catalog.pg_backend_pid()`, roles)
	if err != nil {
		return ErrRecordLedgerFence
	}
	for rows.Next() {
		var stopped bool
		if rows.Scan(&stopped) != nil || !stopped {
			rows.Close()
			return ErrRecordLedgerFence
		}
	}
	if rows.Err() != nil {
		rows.Close()
		return ErrRecordLedgerFence
	}
	rows.Close()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		if db.admin.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where usename=any($1::name[]) and pid<>pg_catalog.pg_backend_pid()`, roles).Scan(&count) != nil {
			return ErrRecordLedgerFence
		}
		if count == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (db *ledgerDatabase) verifyFenced(ctx context.Context) error {
	if err := ledgerDomainIdentity(ctx, db.admin, db.request.Identity); err != nil {
		return err
	}
	if err := db.checkWriterInventory(ctx, true); err != nil {
		return err
	}
	entries, head := db.tables()
	for _, role := range db.previous {
		if err := db.checkOldRole(ctx, role); err != nil {
			return err
		}
		var safe bool
		err := db.admin.QueryRow(ctx, `select not rolcanlogin and not pg_catalog.has_table_privilege(oid,$2,'INSERT')
			and not pg_catalog.has_any_column_privilege(oid,$2,'INSERT') and not pg_catalog.has_table_privilege(oid,$3,'UPDATE')
			and not pg_catalog.has_any_column_privilege(oid,$3,'UPDATE') from pg_catalog.pg_roles where rolname=$1`, role, entries, head).Scan(&safe)
		if err != nil || !safe {
			return ErrRecordLedgerFence
		}
		functions, err := db.oldWriterFunctions(ctx, role)
		if err != nil || len(functions) != 0 {
			return ErrRecordLedgerFence
		}
	}
	var sessions, prepared int
	if db.admin.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where usename=any($1::name[])`, db.previous).Scan(&sessions) != nil || sessions != 0 {
		return ErrRecordLedgerFence
	}
	if db.admin.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where usename=$1 and pid<>$2`, db.writerRole, db.writerPID.Load()).Scan(&sessions) != nil || sessions != 0 {
		return ErrRecordLedgerFence
	}
	if db.admin.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity where usename=$1 and pid<>$2`, db.adminRole, db.adminPID).Scan(&sessions) != nil || sessions != 0 {
		return ErrRecordLedgerFence
	}
	if db.admin.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts where database=pg_catalog.current_database() or owner=any($1::name[])`, db.previous).Scan(&prepared) != nil || prepared != 0 {
		return ErrRecordLedgerFence
	}
	return nil
}

func (db *ledgerDatabase) openClients(ctx context.Context) error {
	for _, item := range []struct {
		url, role   string
		destination **pgxpool.Pool
	}{
		{db.request.ReaderURL, db.readerRole, &db.reader}, {db.request.WriterURL, db.writerRole, &db.writer},
	} {
		config, err := ledgerParsePool(item.url)
		if err != nil {
			return err
		}
		role := item.role
		config.AfterConnect = func(c context.Context, conn *pgx.Conn) error {
			if ledgerDirectRole(c, conn, role, false) != nil {
				return ErrRecordLedgerFence
			}
			return ledgerDomainIdentity(c, conn, db.request.Identity)
		}
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			return ErrRecordLedgerFence
		}
		*item.destination = pool
		if pool.Ping(ctx) != nil {
			return ErrRecordLedgerFence
		}
	}
	var pid int32
	if db.writer.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&pid) != nil || pid == 0 {
		return ErrRecordLedgerFence
	}
	db.writerPID.Store(pid)
	entries, head := db.tables()
	var safe bool
	err := db.admin.QueryRow(ctx, `select
		pg_catalog.has_table_privilege($1,$3,'SELECT') and pg_catalog.has_table_privilege($1,$4,'SELECT')
		and not pg_catalog.has_table_privilege($1,$3,'INSERT') and not pg_catalog.has_any_column_privilege($1,$3,'INSERT')
		and not pg_catalog.has_table_privilege($1,$4,'UPDATE') and not pg_catalog.has_any_column_privilege($1,$4,'UPDATE')
		and pg_catalog.has_table_privilege($2,$3,'INSERT') and pg_catalog.has_table_privilege($2,$4,'UPDATE')`, db.readerRole, db.writerRole, entries, head).Scan(&safe)
	if err != nil || !safe {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerPinApp(config *pgxpool.Config, address string) error {
	if config == nil || config.ConnConfig == nil || net.ParseIP(address) == nil {
		return ErrRecordLedgerFence
	}
	config.ConnConfig.Host = address
	config.ConnConfig.Port = 5432
	for _, fallback := range config.ConnConfig.Fallbacks {
		fallback.Host = address
		fallback.Port = 5432
	}
	return nil
}

// A migration's bootstrap session is authority bound for the entire invocation.
// Refuse every subsequent physical connection attempt, even after a network loss.
func ledgerLimitManagementConnections(config *pgxpool.Config) {
	var attempted atomic.Bool
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		if !attempted.CompareAndSwap(false, true) {
			return ErrRecordLedgerFence
		}
		return nil
	}
}

func ledgerOpenApp(ctx context.Context, url string, address string, expected RecordLedgerApplicationIdentity, bootstrap bool, bootstrapPool *pgxpool.Pool, port uint16, singleConnection bool) (*pgxpool.Pool, error) {
	config, err := ledgerParsePool(url)
	if err != nil || config.ConnConfig.Database != expected.DatabaseName || !ledgerSafeRole(config.ConnConfig.User) || (!bootstrap && (bootstrapPool == nil || port != 5432)) || ledgerPinApp(config, address) != nil {
		return nil, ErrRecordLedgerFence
	}
	if singleConnection {
		ledgerLimitManagementConnections(config)
	}
	role := config.ConnConfig.User
	config.AfterConnect = func(c context.Context, conn *pgx.Conn) error {
		if ledgerDirectRole(c, conn, role, bootstrap) != nil {
			return ErrRecordLedgerFence
		}
		if bootstrap {
			return ledgerAppIdentity(c, conn, expected)
		}
		var oid uint32
		var name string
		if conn.QueryRow(c, `select oid,datname from pg_catalog.pg_database where datname=pg_catalog.current_database()`).Scan(&oid, &name) != nil || oid != expected.DatabaseOID || name != expected.DatabaseName {
			return ErrRecordLedgerFence
		}
		return ledgerAppIdentity(c, bootstrapPool, expected)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	if pool.Ping(ctx) != nil {
		pool.Close()
		return nil, ErrRecordLedgerFence
	}
	return pool, nil
}

func ledgerRecoveryBinding(ctx context.Context, app *pgxpool.Pool, witness *ledgerDatabase, cfg SourceMaintenanceConfig, rid string) (uint64, error) {
	admission, err := readSourceAdmission(ctx, app)
	if err != nil {
		return 0, ErrRecordLedgerFence
	}
	if cfg.Witness.ID != witness.request.Identity.ID || cfg.Witness.SystemIdentifier != witness.request.Identity.SystemIdentifier || cfg.Witness.DatabaseOID != witness.request.Identity.DatabaseOID || cfg.Witness.DatabaseName != witness.request.Identity.DatabaseName || cfg.Witness.Epoch != witness.request.Identity.Epoch {
		return 0, ErrRecordLedgerFence
	}
	if admission != nil {
		var generation int64
		var mode, witnessRID string
		err := witness.admin.QueryRow(ctx, `select generation,mode,coalesce(recovery_id,'') from public.source_authority_state where singleton`).Scan(&generation, &mode, &witnessRID)
		if err != nil || admission.Mode != "closed" || admission.RecoveryID != rid || admission.Generation <= 0 || generation != admission.Generation || mode != "recovering" || witnessRID != rid || admission.Witness != cfg.Witness {
			return 0, ErrRecordLedgerFence
		}
		return uint64(generation), nil
	}
	// A missing APP admission alone cannot establish a never-enrolled lineage.
	// Require independently readable W2 enrollment history to be empty as well.
	var empty bool
	err = witness.admin.QueryRow(ctx, `select not exists(select 1 from public.source_application_enrollments)
		and not exists(select 1 from public.source_application_activations)
		and not exists(select 1 from public.source_retirement_prepares)
		and not exists(select 1 from public.source_authority_recoveries)`).Scan(&empty)
	if err != nil || !empty {
		return 0, ErrRecordLedgerFence
	}
	return 0, nil
}
