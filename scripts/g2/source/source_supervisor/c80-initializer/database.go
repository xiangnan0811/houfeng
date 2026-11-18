package main

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	storemigrate "houfeng/internal/center/store/migrate"
)

const authorityRole = "houfeng_records_authority"

type applicationIdentity struct {
	ID               string `json:"ID"`
	SystemIdentifier string `json:"SystemIdentifier"`
	DatabaseName     string `json:"DatabaseName"`
	DatabaseOID      uint32 `json:"DatabaseOID"`
	Epoch            int64  `json:"Epoch"`
}

type initializeOutput struct {
	Version             uint64              `json:"version"`
	Case                string              `json:"case"`
	Application         applicationIdentity `json:"application"`
	DeploymentID        string              `json:"deployment_id"`
	ActorUserID         string              `json:"actor_user_id"`
	GroupID             string              `json:"group_id"`
	RuntimeRole         string              `json:"runtime_role"`
	Endpoint            endpoint            `json:"endpoint"`
	Records             fixtureRecords      `json:"records"`
	MembershipExpiresAt time.Time           `json:"membership_expires_at"`
}

type heartbeatOutput struct {
	Version             uint64    `json:"version"`
	Case                string    `json:"case"`
	DeploymentID        string    `json:"deployment_id"`
	MembershipExpiresAt time.Time `json:"membership_expires_at"`
}

// Errors deliberately identify only the fixed step; URLs, credentials, SQL
// errors, authority material and record content never reach process output.
type stageError string

func (e stageError) Error() string { return string(e) }

func checkAuthorityParent() error {
	if os.Getuid() != 10002 || os.Geteuid() != 10002 || os.Getgid() != 10002 || os.Getegid() != 10002 {
		return errors.New("invalid initializer identity")
	}
	info, err := os.Lstat("/var/lib/g2/control/maintenance")
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid authority parent")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 10002 || stat.Gid != 10002 {
		return errors.New("invalid authority parent")
	}
	return nil
}

func checkAuthorityRootOwner() error {
	info, err := os.Lstat(authorityRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid authority state")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 10002 || stat.Gid != 10002 {
		return errors.New("invalid authority state")
	}
	return nil
}

// All pools originate in the same explicit guest endpoint. The environment is
// cleared before parsing any pgx configuration, and every new connection is
// independently checked for direct-login and actual server identity.
func openPool(ctx context.Context, ep endpoint, role, password string, bootstrap bool) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(connectionURL(ep, role, password))
	if err != nil || config.ConnConfig.Host != ep.Host || config.ConnConfig.Port != ep.Port ||
		config.ConnConfig.Database != applicationDB || config.ConnConfig.User != role ||
		config.ConnConfig.Password != password || config.ConnConfig.TLSConfig != nil || len(config.ConnConfig.Fallbacks) != 0 {
		return nil, errors.New("invalid PostgreSQL configuration")
	}
	config.MaxConns = 1
	config.MinConns = 0
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		var version int
		var sessionRole, currentRole, database, host string
		var oid int64
		var superuser bool
		var port int
		err := conn.QueryRow(ctx, `
			select current_setting('server_version_num')::integer, session_user, current_user,
			       r.oid::bigint, r.rolsuper, current_database(), host(inet_server_addr()), inet_server_port()
			from pg_catalog.pg_roles r where r.rolname=current_user`).Scan(
			&version, &sessionRole, &currentRole, &oid, &superuser, &database, &host, &port)
		if err != nil || version < 160000 || version >= 170000 || sessionRole != role || currentRole != role ||
			database != applicationDB || port != int(ep.Port) || !net.ParseIP(host).Equal(net.ParseIP(ep.Host)) {
			return errors.New("unexpected PostgreSQL identity")
		}
		if bootstrap {
			if oid != 10 || !superuser {
				return errors.New("unexpected bootstrap identity")
			}
		} else if oid == 10 || superuser {
			return errors.New("unexpected direct role identity")
		}
		return nil
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("open PostgreSQL unavailable")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL unavailable")
	}
	return pool, nil
}

func checkFreshAPP(ctx context.Context, pool *pgxpool.Pool) error {
	var publicObjects, publicFunctions, conflictingRoles int64
	err := pool.QueryRow(ctx, `
		select (select count(*) from pg_catalog.pg_class c
		        join pg_catalog.pg_namespace n on n.oid=c.relnamespace
		        where n.nspname='public'),
		       (select count(*) from pg_catalog.pg_proc p
		        join pg_catalog.pg_namespace n on n.oid=p.pronamespace
		        where n.nspname='public'),
		       (select count(*) from pg_catalog.pg_roles
		        where rolname=any($1::text[]))`,
		[]string{runtimeRole, adminRole, migratorRole, authorityRole},
	).Scan(&publicObjects, &publicFunctions, &conflictingRoles)
	if err != nil || publicObjects != 0 || publicFunctions != 0 || conflictingRoles != 0 {
		return errors.New("APP is not fresh")
	}
	return nil
}

func distinctPasswords(passwords ...string) bool {
	seen := make(map[string]struct{}, len(passwords))
	for _, password := range passwords {
		if password == "" {
			return false
		}
		if _, found := seen[password]; found {
			return false
		}
		seen[password] = struct{}{}
	}
	return true
}

func initialize(ctx context.Context, input privateInput, connection bootstrapConnection) (initializeOutput, error) {
	var out initializeOutput
	if err := checkAuthorityParent(); err != nil {
		return out, stageError("authority_parent_unavailable")
	}
	if !distinctPasswords(connection.password, input.RuntimePassword, input.AdminPassword, input.MigratorPassword) {
		return out, stageError("credentials_unavailable")
	}
	bootstrap, err := openPool(ctx, connection.endpoint, connection.role, connection.password, true)
	if err != nil {
		return out, stageError("bootstrap_unavailable")
	}
	defer bootstrap.Close()
	if err := checkFreshAPP(ctx, bootstrap); err != nil {
		return out, stageError("fresh_app_unavailable")
	}
	state, err := recordauthority.CreateComposeState(authorityRoot)
	if err != nil || checkAuthorityRootOwner() != nil {
		return out, stageError("authority_state_unavailable")
	}
	if !distinctPasswords(connection.password, input.RuntimePassword, input.AdminPassword, input.MigratorPassword, state.DatabasePassword()) {
		return out, stageError("credentials_unavailable")
	}
	config := platformmigrate.ComposeBootstrapConfig{
		DatabaseName: applicationDB, BootstrapRole: connection.role, AuthorityRole: authorityRole,
		Roles: platformmigrate.AppRoleSetV1{CenterRuntime: runtimeRole, PlatformAdmin: adminRole, Migrator: migratorRole},
		Passwords: platformmigrate.ComposeRolePasswords{
			Runtime: input.RuntimePassword, PlatformAdmin: input.AdminPassword,
			Migrator: input.MigratorPassword, Authority: state.DatabasePassword(),
		},
	}
	if err := platformmigrate.ProvisionComposeBootstrap(ctx, bootstrap, config); err != nil {
		return out, stageError("role_bootstrap_unavailable")
	}
	migrator, err := openPool(ctx, connection.endpoint, migratorRole, input.MigratorPassword, false)
	if err != nil {
		return out, stageError("migrator_unavailable")
	}
	defer migrator.Close()
	if _, err := storemigrate.ConvergeAppACLCurrent(ctx, migrator, runtimeRole, adminRole); err != nil {
		return out, stageError("c80_catalog_unavailable")
	}
	identity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, bootstrap, platformmigrate.DomainKindApplication)
	if err != nil || identity.ID == "" || identity.Kind != platformmigrate.DomainKindApplication ||
		identity.Mode != "postgres_system" || identity.DatabaseName != applicationDB || identity.DatabaseOID == 0 || identity.IdentityEpoch != 1 {
		return out, stageError("app_identity_unavailable")
	}
	systemID, err := strconv.ParseUint(identity.PostgresSystemIdentifier, 10, 64)
	if err != nil || systemID == 0 || strconv.FormatUint(systemID, 10) != identity.PostgresSystemIdentifier {
		return out, stageError("app_identity_unavailable")
	}
	command, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		return out, stageError("activation_unavailable")
	}
	var receipt []byte
	if migrator.QueryRow(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, command).Scan(&receipt) != nil ||
		recordauthority.PersistActivationReceipt(authorityRoot, state, receipt) != nil ||
		recordauthority.VerifyActivationReceipt(authorityRoot, state) != nil {
		return out, stageError("activation_unavailable")
	}
	migrator.Close()
	bootstrap.Close()
	authority, err := openPool(ctx, connection.endpoint, authorityRole, state.DatabasePassword(), false)
	if err != nil {
		return out, stageError("authority_connection_unavailable")
	}
	defer authority.Close()
	if _, err := heartbeat(ctx, authority, state); err != nil {
		return out, stageError("membership_unavailable")
	}
	runtime, err := openPool(ctx, connection.endpoint, runtimeRole, input.RuntimePassword, false)
	if err != nil {
		return out, stageError("runtime_unavailable")
	}
	defer runtime.Close()
	if err := storemigrate.AdmitAppACLCurrentRuntime(ctx, runtime); err != nil {
		return out, stageError("runtime_admission_unavailable")
	}
	fixture, err := seedAPP(ctx, runtime, state.DeploymentID, input.Case, input.FixtureVariant)
	if err != nil {
		return out, stageError("fixture_unavailable")
	}
	membershipExpiry, err := heartbeat(ctx, authority, state)
	if err != nil {
		return out, stageError("membership_unavailable")
	}
	out = initializeOutput{
		Version: 1, Case: input.Case,
		Application: applicationIdentity{ID: identity.ID, SystemIdentifier: identity.PostgresSystemIdentifier,
			DatabaseName: identity.DatabaseName, DatabaseOID: identity.DatabaseOID, Epoch: identity.IdentityEpoch},
		DeploymentID: string(state.DeploymentID), ActorUserID: fixture.actorID, GroupID: fixture.groupID,
		RuntimeRole: runtimeRole, Endpoint: connection.endpoint, Records: fixture.records,
		MembershipExpiresAt: membershipExpiry,
	}
	return out, nil
}

func heartbeat(ctx context.Context, pool *pgxpool.Pool, state recordauthority.VerifiedComposeState) (time.Time, error) {
	command, expected, err := recordauthority.MarshalMembershipHeartbeatCommandV1(state, time.Now().UTC())
	if err != nil {
		return time.Time{}, err
	}
	var actual, dbNow time.Time
	if err := pool.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, command).Scan(&actual); err != nil {
		return time.Time{}, err
	}
	if err := pool.QueryRow(ctx, `select pg_catalog.clock_timestamp()`).Scan(&dbNow); err != nil {
		return time.Time{}, err
	}
	if !actual.UTC().Equal(expected) || actual.Sub(dbNow) < 60*time.Second {
		return time.Time{}, errors.New("membership expiry unavailable")
	}
	return actual.UTC(), nil
}

func refreshHeartbeat(ctx context.Context, input privateInput, connection bootstrapConnection) (heartbeatOutput, error) {
	var out heartbeatOutput
	if err := checkAuthorityParent(); err != nil || checkAuthorityRootOwner() != nil {
		return out, stageError("authority_state_unavailable")
	}
	state, err := recordauthority.LoadComposeState(authorityRoot)
	if err != nil || recordauthority.VerifyActivationReceipt(authorityRoot, state) != nil {
		return out, stageError("authority_state_unavailable")
	}
	authority, err := openPool(ctx, connection.endpoint, authorityRole, connection.password, false)
	if err != nil {
		return out, stageError("authority_connection_unavailable")
	}
	defer authority.Close()
	expiresAt, err := heartbeat(ctx, authority, state)
	if err != nil {
		return out, stageError("membership_unavailable")
	}
	return heartbeatOutput{Version: 1, Case: input.Case, DeploymentID: string(state.DeploymentID), MembershipExpiresAt: expiresAt}, nil
}
