//go:build linux

package main

import (
	"context"
	"errors"
	"net/netip"
	"strconv"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
)

var errDomain = errors.New("domain unavailable")

type physicalIdentity struct {
	systemID string
	database string
	oid      uint32
}

type domainDB struct {
	connection bootstrapConnection
	pool       *pgxpool.Pool
	physical   atomic.Pointer[physicalIdentity]
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Every newly established connection must actually be the fixed PG16 direct
// bootstrap LOGIN on the requested database and endpoint. After the preflight,
// reconnects must also match the independently observed physical identity.
func observePhysical(ctx context.Context, db rowQuerier, connection bootstrapConnection) (physicalIdentity, error) {
	var identity physicalIdentity
	var version, port int
	var session, current, host string
	var roleOID uint32
	var superuser, login bool
	err := db.QueryRow(ctx, `
		select pg_catalog.current_setting('server_version_num')::integer,
		       session_user, current_user, r.oid, r.rolsuper, r.rolcanlogin,
		       d.datname, d.oid, pg_catalog.host(pg_catalog.inet_server_addr()),
		       pg_catalog.inet_server_port(),
		       (select system_identifier::text from pg_catalog.pg_control_system())
		from pg_catalog.pg_roles r
		join pg_catalog.pg_database d on d.datname = pg_catalog.current_database()
		where r.rolname = current_user`).Scan(&version, &session, &current, &roleOID,
		&superuser, &login, &identity.database, &identity.oid, &host, &port, &identity.systemID)
	if err != nil || version < 160000 || version >= 170000 ||
		session != connection.role || current != session || roleOID != 10 || !superuser || !login ||
		identity.database != connection.endpoint.database || identity.oid == 0 || port != int(connection.endpoint.port) {
		return physicalIdentity{}, errDomain
	}
	actualHost, err := netip.ParseAddr(host)
	if err != nil || actualHost.String() != connection.endpoint.host {
		return physicalIdentity{}, errDomain
	}
	systemID, err := strconv.ParseUint(identity.systemID, 10, 64)
	if err != nil || systemID == 0 || strconv.FormatUint(systemID, 10) != identity.systemID {
		return physicalIdentity{}, errDomain
	}
	return identity, nil
}

func openDomain(ctx context.Context, connection bootstrapConnection) (*domainDB, error) {
	config, err := pgxpool.ParseConfig(connectionURL(connection))
	if err != nil || config.ConnConfig.Host != connection.endpoint.host ||
		config.ConnConfig.Port != connection.endpoint.port || config.ConnConfig.Database != connection.endpoint.database ||
		config.ConnConfig.User != connection.role || config.ConnConfig.Password != connection.password ||
		config.ConnConfig.TLSConfig != nil || len(config.ConnConfig.Fallbacks) != 0 {
		return nil, errDomain
	}
	db := &domainDB{connection: connection}
	config.MinConns = 0
	config.MaxConns = 1
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		actual, err := observePhysical(ctx, conn, connection)
		if err != nil {
			return errDomain
		}
		if pinned := db.physical.Load(); pinned != nil && *pinned != actual {
			return errDomain
		}
		return nil
	}
	db.pool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errDomain
	}
	if err := db.pool.Ping(ctx); err != nil {
		db.pool.Close()
		return nil, errDomain
	}
	return db, nil
}

// This is a census of the current catalog, not proof that history was never
// deleted. The root supervisor's pre-initdb genesis and continuous lifecycle
// supply that independent history; this process cannot issue that authority.
func (db *domainDB) preflight(ctx context.Context) (physicalIdentity, error) {
	identity, err := observePhysical(ctx, db.pool, db.connection)
	if err != nil {
		return physicalIdentity{}, errDomain
	}
	var fresh bool
	err = db.pool.QueryRow(ctx, `
		select
		  (select count(*) = 4 and bool_and(datname::text = any(array['postgres','template0','template1',$1::text]))
		     from pg_catalog.pg_database)
		  and not exists (select 1 from pg_catalog.pg_roles r
		       where r.oid::bigint >= 16384 or (r.rolname !~ '^pg_' and r.oid <> 10)
		          or (r.oid <> 10 and (r.rolcanlogin or r.rolsuper or r.rolcreatedb
		              or r.rolcreaterole or r.rolreplication or r.rolbypassrls)))
		  and not exists (select 1 from pg_catalog.pg_auth_members where member = 10 or roleid = 10)
		  and not exists (select 1 from pg_catalog.pg_db_role_setting)
		  and not exists (select 1 from pg_catalog.pg_default_acl)
		  and not exists (select 1 from pg_catalog.pg_tablespace where spcname not in ('pg_default','pg_global'))
		  and not exists (select 1 from pg_catalog.pg_namespace
		       where nspname not in ('pg_catalog','pg_toast','information_schema','public'))
		  and not exists (select 1 from pg_catalog.pg_depend d
		       join pg_catalog.pg_namespace n on n.oid=d.refobjid
		       where d.refclassid='pg_catalog.pg_namespace'::pg_catalog.regclass and n.nspname='public')
		  and not exists (select 1 from pg_catalog.pg_class c join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		       where n.nspname='public')
		  and not exists (select 1 from pg_catalog.pg_proc p join pg_catalog.pg_namespace n on n.oid=p.pronamespace
		       where n.nspname='public')
		  and not exists (select 1 from pg_catalog.pg_type t join pg_catalog.pg_namespace n on n.oid=t.typnamespace
		       where n.nspname='public')
		  and not exists (select 1 from pg_catalog.pg_operator o join pg_catalog.pg_namespace n on n.oid=o.oprnamespace
		       where n.nspname='public')
		  and not exists (select 1 from pg_catalog.pg_collation c join pg_catalog.pg_namespace n on n.oid=c.collnamespace
		       where n.nspname='public')
		  and not exists (select 1 from pg_catalog.pg_conversion c join pg_catalog.pg_namespace n on n.oid=c.connamespace
		       where n.nspname='public')
		  and not exists (select 1 from pg_catalog.pg_extension where extname <> 'plpgsql')
		  and not exists (select 1 from pg_catalog.pg_language where lanname not in ('internal','c','sql','plpgsql'))
		  and not exists (select 1 from pg_catalog.pg_event_trigger)
		  and not exists (select 1 from pg_catalog.pg_foreign_data_wrapper)
		  and not exists (select 1 from pg_catalog.pg_publication)
		  and not exists (select 1 from pg_catalog.pg_subscription)
		  and not exists (select 1 from pg_catalog.pg_largeobject_metadata)
		  and not exists (select 1 from pg_catalog.pg_replication_slots)
		  and not exists (select 1 from pg_catalog.pg_prepared_xacts)
		  and not exists (select 1 from pg_catalog.pg_stat_activity
		       where backend_type='client backend' and pid <> pg_catalog.pg_backend_pid())`, db.connection.endpoint.database).Scan(&fresh)
	if err != nil || !fresh {
		return physicalIdentity{}, errDomain
	}
	db.physical.Store(&identity)
	return identity, nil
}

func (db *domainDB) verifyPhysical(ctx context.Context) error {
	pinned := db.physical.Load()
	if pinned == nil {
		return errDomain
	}
	actual, err := observePhysical(ctx, db.pool, db.connection)
	if err != nil || actual != *pinned {
		return errDomain
	}
	return nil
}

// Read back the one actual persisted row and compare it to the actual server
// identity; provisioning return values alone are not taken as physical proof.
func (db *domainDB) readDomainIdentity(ctx context.Context, kind platformmigrate.DomainKind) (platformmigrate.DomainIdentity, error) {
	var identity platformmigrate.DomainIdentity
	var storedKind string
	var count int64
	if err := db.verifyPhysical(ctx); err != nil {
		return identity, errDomain
	}
	err := db.pool.QueryRow(ctx, `
		select domain_id, domain_kind, identity_epoch, identity_mode,
		       coalesce(postgres_system_identifier, ''), database_oid, database_name,
		       (select count(*) from public.record_platform_domain_identity)
		from public.record_platform_domain_identity`).Scan(&identity.ID, &storedKind,
		&identity.IdentityEpoch, &identity.Mode, &identity.PostgresSystemIdentifier,
		&identity.DatabaseOID, &identity.DatabaseName, &count)
	identity.Kind = platformmigrate.DomainKind(storedKind)
	pinned := db.physical.Load()
	if err != nil || count != 1 || pinned == nil || identity.Kind != kind || identity.IdentityEpoch != 1 ||
		identity.Mode != "postgres_system" || identity.PostgresSystemIdentifier != pinned.systemID ||
		identity.DatabaseName != pinned.database || identity.DatabaseOID != pinned.oid || !validDomainID(identity.ID) {
		return platformmigrate.DomainIdentity{}, errDomain
	}
	return identity, nil
}

func validDomainID(id string) bool {
	if len(id) != 67 || id[:3] != "rd-" {
		return false
	}
	for i := 3; i < len(id); i++ {
		if (id[i] < '0' || id[i] > '9') && (id[i] < 'a' || id[i] > 'f') {
			return false
		}
	}
	return true
}
