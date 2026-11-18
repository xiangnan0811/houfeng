//go:build linux

package deploy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/platformmigrate"
)

type ledgerRoleReplayRequest struct {
	Request RecordLedgerMaintenanceRequest `json:"request"`
	Address string                         `json:"address"`
}

// This child uses the actual protected journal and a new bootstrap backend. It
// models the next invocation, including the ordinary closeScope admin fence.
func TestRecordLedgerRoleReplayChild(t *testing.T) {
	journalPath := os.Getenv("HOUFENG_TEST_LEDGER_ROLE_JOURNAL")
	if journalPath == "" {
		return
	}
	fd, err := unix.Open(journalPath, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatal("open protected role journal")
	}
	defer unix.Close(fd)
	data, exists, err := fenceReadFile(fd, "test-replay-request.json")
	var input ledgerRoleReplayRequest
	if err != nil || !exists || fenceDecode(data, &input) != nil {
		t.Fatal("read protected replay request")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	config, err := ledgerParsePool(input.Request.AppBootstrapURL)
	if err != nil {
		t.Fatal("parse bootstrap endpoint")
	}
	app, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open bootstrap pool")
	}
	defer app.Close()
	control, err := ledgerControlDir(input.Request.Compose.ControlConfigPath)
	if err != nil {
		t.Fatal("open control directory")
	}
	defer unix.Close(control)
	session, err := ledgerSessionDir(control, input.Request.Compose.RecoveryID)
	if err != nil {
		t.Fatal("open existing session")
	}
	defer unix.Close(session)
	inventory, _, err := ledgerPrepareApplication(ctx, app, input.Request, config.ConnConfig.User, control, session, input.Address, nil)
	if err != nil {
		t.Fatal("same-RID replay did not recover the protected role transition")
	}
	if err := ledgerDisableAdmin(ctx, app, inventory.Admin); err != nil {
		t.Fatal("close replay admin session")
	}
}

// The strict runner supplies a private PostgreSQL instance. The test runs the
// real C81 catalog reader, role transaction, protected journal and a separate
// replay process; the loopback relay only maps the production port to the
// runner's isolated PostgreSQL port.
func TestPostgresIntegrationRecordLedgerApplicationRoleFence(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires the ownership-checked PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("role fence requires the ownership-checked PostgreSQL runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	baseConfig, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil || baseConfig.ConnConfig.User != "postgres" {
		t.Fatal("missing direct isolated bootstrap endpoint")
	}
	base, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil || base.Ping(ctx) != nil {
		t.Fatal("open isolated bootstrap database")
	}
	t.Cleanup(base.Close)
	assertComposeIntegrationFixtureHasNoProductionState(t, ctx, base)
	config := uniqueComposeIntegrationConfig(t, baseConfig.ConnConfig.Password)
	config.Passwords.Runtime = "a previous runtime secret long enough to resemble an admin secret"
	if _, err := base.Exec(ctx, formatComposeIntegrationDDL(t, ctx, base, `CREATE DATABASE %I`, config.DatabaseName)); err != nil {
		t.Fatal("create isolated application database")
	}
	t.Cleanup(func() { cleanupComposeIntegrationDatabase(t, context.Background(), base, config) })
	open := func(ctx context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
		connection, err := composeIntegrationEndpointConfig(baseConfig, endpoint)
		if err != nil {
			return nil, err
		}
		pool, err := pgxpool.NewWithConfig(ctx, connection)
		if err != nil {
			return nil, err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		return pool, nil
	}
	if err := initializeComposeWithDependencies(ctx, config, composeLaunchIsolatedInitDependencies(open)); err != nil {
		t.Fatal("initialize real current APP catalog")
	}
	bootstrapURL := ledgerRoleTestURL(baseConfig, config.DatabaseName, config.BootstrapRole, config.Passwords.Bootstrap)
	oldURL := ledgerRoleTestURL(baseConfig, config.DatabaseName, config.Roles.PlatformAdmin, config.Passwords.PlatformAdmin)
	newPassword := "a freshly generated management secret for the role fence fixture"
	newURL := ledgerRoleTestURL(baseConfig, config.DatabaseName, config.Roles.PlatformAdmin, newPassword)
	app := ledgerRoleTestPool(t, ctx, bootstrapURL)
	t.Cleanup(app.Close)
	identity, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, app, platformmigrate.DomainKindApplication)
	if err != nil {
		t.Fatal("provision actual APP identity")
	}
	address := ledgerRoleTestRelay(t, net.JoinHostPort(baseConfig.ConnConfig.Host, strconv.Itoa(int(baseConfig.ConnConfig.Port))))
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal("protect fixture directory")
	}
	request := RecordLedgerMaintenanceRequest{
		Compose:         ComposeOfflineFenceRequest{RecoveryID: "role_reentry", ControlConfigPath: filepath.Join(root, "source.json")},
		AppBootstrapURL: bootstrapURL, AppPreviousAdminURL: oldURL, AppAdminURL: newURL,
		AppIdentity: RecordLedgerApplicationIdentity{ID: identity.ID, SystemIdentifier: identity.PostgresSystemIdentifier,
			DatabaseName: identity.DatabaseName, DatabaseOID: identity.DatabaseOID, Epoch: uint64(identity.IdentityEpoch)},
		AppAdminRole:      config.Roles.PlatformAdmin,
		AppPreviousLogins: []string{config.Roles.CenterRuntime, config.Roles.DeletionWorker, config.Roles.Migrator, config.AuthorityRole},
	}
	control, err := ledgerControlDir(request.Compose.ControlConfigPath)
	if err != nil {
		t.Fatal("create protected control directory")
	}
	defer unix.Close(control)
	session, err := ledgerSessionDir(control, request.Compose.RecoveryID)
	if err != nil {
		t.Fatal("create protected role session")
	}
	defer unix.Close(session)
	oldRuntime := ledgerRoleTestConnect(t, ctx, baseConfig, config.DatabaseName, config.Roles.CenterRuntime, config.Passwords.Runtime)
	defer oldRuntime.Close(context.Background())
	oldAdmin := ledgerRoleTestConnect(t, ctx, baseConfig, config.DatabaseName, config.Roles.PlatformAdmin, config.Passwords.PlatformAdmin)
	defer oldAdmin.Close(context.Background())
	badRequest := request
	badRequest.AppPreviousAdminURL = ledgerRoleTestURL(baseConfig, config.DatabaseName, config.Roles.PlatformAdmin, "wrong previous management password")
	if _, _, err := ledgerPrepareApplication(ctx, app, badRequest, config.BootstrapRole, control, session, address, nil); err == nil {
		t.Fatal("accepted the wrong previous management credential")
	}
	badRequest = request
	badRequest.AppAdminURL = ledgerRoleTestURL(baseConfig, config.DatabaseName, config.Roles.PlatformAdmin, config.Passwords.Runtime)
	if _, _, err := ledgerPrepareApplication(ctx, app, badRequest, config.BootstrapRole, control, session, address, nil); err == nil {
		t.Fatal("accepted a previous runtime secret as the new administrator credential")
	}
	if _, exists, err := fenceReadFile(session, "app-inventory.json"); err != nil || exists {
		t.Fatal("rejected first entry published a role journal")
	}
	if oldRuntime.Ping(ctx) != nil || oldAdmin.Ping(ctx) != nil {
		t.Fatal("rejected first entry changed live role sessions")
	}
	inventory, credential, err := ledgerPrepareApplication(ctx, app, request, config.BootstrapRole, control, session, address, nil)
	if err != nil || inventory.LegacyC80 {
		t.Fatalf("first entry did not fence exact current APP: %v", err)
	}
	var ownerFenced bool
	if app.QueryRow(ctx, `select d.datdba=r.oid and not r.rolcanlogin and
		not pg_catalog.has_database_privilege(r.oid,d.oid,'CONNECT')
		from pg_catalog.pg_database d join pg_catalog.pg_authid r on r.rolname=$1
		where d.datname=pg_catalog.current_database()`, config.Roles.Migrator).Scan(&ownerFenced) != nil || !ownerFenced {
		t.Fatal("migrator database owner retained login or CONNECT")
	}
	if oldRuntime.Ping(ctx) == nil || oldAdmin.Ping(ctx) == nil {
		t.Fatal("pre-fence application sessions survived")
	}
	runtimeName := pgx.Identifier{config.Roles.CenterRuntime}.Sanitize()
	baselineRuntime, found := ledgerAppRoleByName(inventory.RoleBaseline, config.Roles.CenterRuntime)
	if !found {
		t.Fatal("missing runtime role baseline")
	}
	if _, err := app.Exec(ctx, "alter role "+runtimeName+" connection limit 7"); err != nil {
		t.Fatal("introduce active role drift")
	}
	if ledgerCheckAppFenced(ctx, app, config.BootstrapRole, inventory, credential) == nil {
		t.Fatal("active fence accepted a changed role attribute")
	}
	if _, err := app.Exec(ctx, "alter role "+runtimeName+" connection limit "+strconv.FormatInt(int64(baselineRuntime.ConnLimit), 10)); err != nil {
		t.Fatal("restore active role attribute")
	}
	if err := ledgerCheckAppFenced(ctx, app, config.BootstrapRole, inventory, credential); err != nil {
		t.Fatal("exact active role fence was not restored")
	}
	ledgerRoleTestRejectLogin(t, ctx, baseConfig, config.DatabaseName, config.Roles.CenterRuntime, config.Passwords.Runtime)
	ledgerRoleTestRejectLogin(t, ctx, baseConfig, config.DatabaseName, config.Roles.PlatformAdmin, config.Passwords.PlatformAdmin)
	freshAdmin := ledgerRoleTestConnect(t, ctx, baseConfig, config.DatabaseName, config.Roles.PlatformAdmin, newPassword)
	freshAdmin.Close(ctx)
	if len(inventory.Targets) != len(request.AppPreviousLogins)+1 {
		t.Fatal("missing fresh role targets")
	}
	for _, target := range inventory.Targets {
		if target.Role != inventory.Admin {
			ledgerRoleTestRejectLogin(t, ctx, baseConfig, config.DatabaseName, target.Role, target.Secret)
		}
	}
	data, exists, err := fenceReadFile(session, "app-inventory.json")
	var persisted ledgerAppInventory
	if err != nil || !exists || fenceDecode(data, &persisted) != nil || !reflect.DeepEqual(persisted, inventory) {
		t.Fatal("protected role journal did not preserve the original and target facts")
	}
	t.Run("bounded cleanup revokes the live admin", func(t *testing.T) {
		live := ledgerRoleTestConnect(t, ctx, baseConfig, config.DatabaseName, inventory.Admin, newPassword)
		defer live.Close(context.Background())

		expired, cancelExpired := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer cancelExpired()
		if err := ledgerDisableAdmin(expired, app, inventory.Admin); err == nil {
			t.Fatal("expired cleanup revoked the administrator")
		}
		var login bool
		if app.QueryRow(ctx, `select rolcanlogin from pg_catalog.pg_authid where rolname=$1`, inventory.Admin).Scan(&login) != nil || !login || live.Ping(ctx) != nil {
			t.Fatal("expired cleanup changed the live administrator")
		}

		work, cancelEntry, err := ledgerMaintenanceWorkContext(ctx)
		if err != nil {
			t.Fatal("start bounded maintenance")
		}
		defer cancelEntry()
		interrupted, cancelWork := context.WithCancel(work)
		cancelWork()
		cleanup, cancelCleanup, err := ledgerMaintenanceCleanupContext(interrupted)
		if err != nil {
			t.Fatal("obtain reserved cleanup time")
		}
		defer cancelCleanup()
		if err := ledgerDisableAdmin(cleanup, app, inventory.Admin); err != nil {
			t.Fatal("reserved cleanup did not revoke the administrator")
		}
		if app.QueryRow(ctx, `select rolcanlogin from pg_catalog.pg_authid where rolname=$1`, inventory.Admin).Scan(&login) != nil || login || live.Ping(ctx) == nil {
			t.Fatal("reserved cleanup left the administrator login or session active")
		}
		ledgerRoleTestRejectLogin(t, ctx, baseConfig, config.DatabaseName, inventory.Admin, newPassword)
	})
	app.Close()

	// A separate executable reads exactly the same protected journal. The
	// private request file is published through the same 0600 journal helper.
	if _, err := ledgerPublishExact(session, "test-replay-request.json", ledgerRoleReplayRequest{Request: request, Address: address}); err != nil {
		t.Fatal("publish private child request")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate role replay child")
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestRecordLedgerRoleReplayChild$", "-test.count=1")
	child.Env = append(os.Environ(), "HOUFENG_TEST_LEDGER_ROLE_JOURNAL="+filepath.Join(root, "fences", "record-ledger", "sessions", request.Compose.RecoveryID))
	if err := child.Run(); err != nil {
		t.Fatal("separate same-RID role replay failed")
	}
	app = ledgerRoleTestPool(t, ctx, bootstrapURL)
	defer app.Close()
	replayed, replayedCredential, err := ledgerPrepareApplication(ctx, app, request, config.BootstrapRole, control, session, address, nil)
	if err != nil || !reflect.DeepEqual(replayed, inventory) || !reflect.DeepEqual(replayedCredential, credential) {
		t.Fatal("same-RID replay changed the journal or fresh credentials")
	}
	if err := ledgerDisableAdmin(ctx, app, inventory.Admin); err != nil {
		t.Fatal("close replayed admin")
	}

	// A non-journaled role change must not be interpreted as an interrupted
	// transaction or reopen the admin, even when every other role is exact.
	role := pgx.Identifier{config.Roles.CenterRuntime}.Sanitize()
	if _, err := app.Exec(ctx, "alter role "+role+" connection limit 7"); err != nil {
		t.Fatal("introduce role attribute drift")
	}
	before, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(inventory))
	if err != nil {
		t.Fatal("read drifted role state")
	}
	if _, _, err := ledgerPrepareApplication(ctx, app, request, config.BootstrapRole, control, session, address, nil); err == nil {
		t.Fatal("accepted a mixed role state")
	}
	after, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("rejected mixed state was mutated")
	}
	original, found := ledgerAppRoleByName(inventory.RoleBaseline, config.Roles.CenterRuntime)
	if !found {
		t.Fatal("runtime baseline missing")
	}
	if _, err := app.Exec(ctx, "alter role "+role+" connection limit "+strconv.FormatInt(int64(original.ConnLimit), 10)); err != nil {
		t.Fatal("restore fixture role limit")
	}
	if _, err := app.Exec(ctx, "alter role "+role+" login"); err != nil {
		t.Fatal("introduce partial login transition")
	}
	if _, _, err := ledgerPrepareApplication(ctx, app, request, config.BootstrapRole, control, session, address, nil); err == nil {
		t.Fatal("accepted a partially reenabled previous role")
	}
	if _, err := app.Exec(ctx, "alter role "+role+" nologin"); err != nil {
		t.Fatal("restore previous role fence")
	}
	if original.Verifier == nil {
		t.Fatal("runtime original verifier missing")
	}
	ledgerRoleTestSetVerifier(t, ctx, app, config.Roles.CenterRuntime, *original.Verifier)
	if _, _, err := ledgerPrepareApplication(ctx, app, request, config.BootstrapRole, control, session, address, nil); err == nil {
		t.Fatal("accepted an original verifier mixed into the target")
	}
	var runtimeTarget string
	for _, target := range inventory.Targets {
		if target.Role == config.Roles.CenterRuntime {
			runtimeTarget = target.Verifier
		}
	}
	if runtimeTarget == "" {
		t.Fatal("runtime target verifier missing")
	}
	ledgerRoleTestSetVerifier(t, ctx, app, config.Roles.CenterRuntime, runtimeTarget)
	if _, err := app.Exec(ctx, "grant create on schema public to "+role); err != nil {
		t.Fatal("introduce catalog grant drift")
	}
	if _, _, err := ledgerPrepareApplication(ctx, app, request, config.BootstrapRole, control, session, address, nil); err == nil {
		t.Fatal("accepted a non-journaled catalog grant")
	}
	var adminLogin bool
	if app.QueryRow(ctx, `select rolcanlogin from pg_catalog.pg_authid where rolname=$1`, inventory.Admin).Scan(&adminLogin) != nil || adminLogin {
		t.Fatal("catalog drift reopened the admin")
	}
	if _, err := app.Exec(ctx, "revoke create on schema public from "+role); err != nil {
		t.Fatal("restore fixture catalog")
	}

	// Model an unresolved transaction which left every original fact intact.
	// A replay must use the already protected targets, not generate credentials.
	ledgerRoleTestRestoreOriginal(t, ctx, app, inventory)
	originalState, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(inventory))
	if err != nil || !reflect.DeepEqual(originalState, inventory.RoleBaseline) {
		t.Fatal("fixture did not restore the exact original role state")
	}
	replayed, replayedCredential, err = ledgerPrepareApplication(ctx, app, request, config.BootstrapRole, control, session, address, nil)
	if err != nil || !reflect.DeepEqual(replayed, inventory) || !reflect.DeepEqual(replayedCredential, credential) {
		t.Fatal("all-original recovery did not reuse the protected targets")
	}
	if err := ledgerDisableAdmin(ctx, app, inventory.Admin); err != nil {
		t.Fatal("close recovered admin")
	}
}

func ledgerRoleTestSetVerifier(t *testing.T, ctx context.Context, app *pgxpool.Pool, role, verifier string) {
	t.Helper()
	var ddl string
	if app.QueryRow(ctx, `select pg_catalog.format('alter role %I password %L',$1::text,$2::text)`, role, verifier).Scan(&ddl) != nil {
		t.Fatal("format verifier fixture change")
	}
	if _, err := app.Exec(ctx, ddl); err != nil {
		t.Fatal("apply verifier fixture change")
	}
}

func ledgerRoleTestRestoreOriginal(t *testing.T, ctx context.Context, app *pgxpool.Pool, inventory ledgerAppInventory) {
	t.Helper()
	tx, err := app.Begin(ctx)
	if err != nil {
		t.Fatal("begin original-state fixture transaction")
	}
	defer tx.Rollback(ctx)
	for _, target := range inventory.Targets {
		original, ok := ledgerAppRoleByName(inventory.RoleBaseline, target.Role)
		if !ok || original.Verifier == nil {
			t.Fatal("missing original fixture verifier")
		}
		var ddl string
		if tx.QueryRow(ctx, `select pg_catalog.format('alter role %I password %L',$1::text,$2::text)`, target.Role, *original.Verifier).Scan(&ddl) != nil {
			t.Fatal("format original verifier")
		}
		if _, err := tx.Exec(ctx, ddl); err != nil {
			t.Fatal("restore original verifier")
		}
		login := " nologin"
		if original.Login {
			login = " login"
		}
		if _, err := tx.Exec(ctx, "alter role "+pgx.Identifier{target.Role}.Sanitize()+login); err != nil {
			t.Fatal("restore original login")
		}
		if target.Role == inventory.Admin {
			continue
		}
		for _, grant := range inventory.RoleBaseline.DatabaseACL {
			if grant.GranteeOID != original.OID || grant.Privilege != "CONNECT" {
				continue
			}
			option := ""
			if grant.Grantable {
				option = " with grant option"
			}
			if _, err := tx.Exec(ctx, "grant connect on database "+pgx.Identifier{inventory.Application.DatabaseName}.Sanitize()+" to "+pgx.Identifier{target.Role}.Sanitize()+option); err != nil {
				t.Fatal("restore original database CONNECT")
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit original-state fixture")
	}
}

func ledgerRoleTestURL(base *pgxpool.Config, database, role, password string) string {
	return (&url.URL{Scheme: "postgres", User: url.UserPassword(role, password),
		Host: net.JoinHostPort(base.ConnConfig.Host, strconv.Itoa(int(base.ConnConfig.Port))), Path: "/" + database,
		RawQuery: "sslmode=disable"}).String()
}

func ledgerRoleTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	config, err := ledgerParsePool(databaseURL)
	if err != nil {
		t.Fatal("parse fixture bootstrap URL")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil || pool.Ping(ctx) != nil {
		t.Fatal("open fixture APP bootstrap")
	}
	return pool
}

func ledgerRoleTestConnect(t *testing.T, ctx context.Context, base *pgxpool.Config, database, role, password string) *pgx.Conn {
	t.Helper()
	config := base.ConnConfig.Copy()
	config.Database, config.User, config.Password = database, role, password
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal("open fixture role connection")
	}
	return conn
}

func ledgerRoleTestRejectLogin(t *testing.T, ctx context.Context, base *pgxpool.Config, database, role, password string) {
	t.Helper()
	config := base.ConnConfig.Copy()
	config.Database, config.User, config.Password = database, role, password
	conn, err := pgx.ConnectConfig(ctx, config)
	if err == nil {
		conn.Close(ctx)
		t.Fatal("retired credential unexpectedly authenticated")
	}
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || (pgerr.Code != "28000" && pgerr.Code != "28P01") {
		t.Fatal("role login failed without a PostgreSQL authentication rejection")
	}
}

func ledgerRoleTestRelay(t *testing.T, target string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.2:5432")
	if err != nil {
		t.Fatal("bind isolated role-fence relay")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer client.Close()
				server, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer server.Close()
				finished := make(chan struct{})
				go func() {
					_, _ = io.Copy(server, client)
					if tcp, ok := server.(*net.TCPConn); ok {
						_ = tcp.CloseWrite()
					}
					close(finished)
				}()
				_, _ = io.Copy(client, server)
				_ = client.Close()
				<-finished
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	return "127.0.0.2"
}
