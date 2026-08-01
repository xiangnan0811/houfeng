package migrate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
)

const appACLR2PGCryptoCatalogDigestPG16 = "57e7ac6a986705d8fa1e5b2260c1836b74dffe1b33bee00d65d1b275284e8196"

// TestPostgresIntegrationAppACLR2 is the strict PG16 catalog-lane anchor.
// Direct invocation remains optional; the strict runner supplies the fixture
// environment and rejects any emitted skip.
func TestPostgresIntegrationAppACLR2(t *testing.T) {
	t.Run("strict runner validates images before side effects and cleans every fixture", func(t *testing.T) {
		assertAppACLR2StrictRunnerBehavior(t)
	})
	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skipf("%s=1 is required for PostgreSQL APP ACL R2 integration tests", postgresIntegrationFlag)
	}
	if strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL")) == "" {
		t.Fatal("HOUFENG_DATABASE_URL is required when PostgreSQL integration is enabled")
	}
	wantServerVersion := requireAppACLR2PG16CatalogImage(t)

	ctx := context.Background()
	t.Run("strict runner propagates one exact image to four isolated fixtures", func(t *testing.T) {
		assertAppACLR2PG16RunnerFixtureDatabases(t, ctx, wantServerVersion)
	})
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	t.Run("deployment pre-R1 pg_control_system provisioning", func(t *testing.T) {
		assertAppACLR2StockControlSystemACL(t, ctx, fixture.db)
		provisionAppACLR2ControlSystemACL(t, ctx, fixture.db)
		assertAppACLR2ProvisionedControlSystemACL(t, ctx, fixture.db)
	})
	unrelatedRole := addAppACLR2PostgresUnrelatedRole(t, ctx, fixture)
	unrelated := fixture.openDirectRolePool(t, ctx, unrelatedRole)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	admin := fixture.openDirectRolePool(t, ctx, fixture.admin)

	if _, err := ConvergeAppACLR1(ctx, migrator, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLR1() error = %v", err)
	}
	if _, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, fixture.db, platformmigrate.DomainKindApplication); err != nil {
		t.Fatalf("ProvisionPostgresDomainIdentity() error = %v", err)
	}

	t.Run("PG16 catalog and bootstrap-only physical identity", func(t *testing.T) {
		assertAppACLR2PG16Catalog(t, ctx, fixture.db, wantServerVersion)
		assertAppACLR2NoControlSystemAuthority(t, ctx, migrator, fixture.migrator)
		assertAppACLR2NoControlSystemAuthority(t, ctx, runtime, fixture.runtime)
		assertAppACLR2NoControlSystemAuthority(t, ctx, admin, fixture.admin)
		assertAppACLR2NoControlSystemAuthority(t, ctx, unrelated, unrelatedRole)
	})

	t.Run("R1 reader authority matrix and runtime routing", func(t *testing.T) {
		for name, db := range map[string]*pgxpool.Pool{
			"bootstrap": fixture.db,
			"migrator":  migrator,
			"runtime":   runtime,
			"admin":     admin,
		} {
			t.Run(name, func(t *testing.T) {
				assertAppACLR2PostgresState(t, ctx, db, AppACLR2StateR1)
			})
		}
		assertAppACLR2PostgresSQLState(t, ctx, unrelated, "42501")

		if err := AdmitAppACLR1OnlyRuntime(ctx, runtime); err != nil {
			t.Fatalf("AdmitAppACLR1OnlyRuntime(R1) error = %v", err)
		}
		if err := AdmitAppACLRuntime(ctx, runtime); err != nil {
			t.Fatalf("frozen AdmitAppACLRuntime(R1) error = %v", err)
		}
		trace := &appACLR2PostgresQueryTrace{}
		state, err := admitAppACLR2RuntimeWithDependencies(
			ctx,
			traceAppACLR2RuntimeBegin(
				newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(runtime)),
				trace,
			),
			defaultAppACLR2RuntimeAdmissionDependencies(),
		)
		if err != nil || state != AppACLR2StateR1 {
			t.Fatalf("AdmitAppACLR2Runtime(R1) = %v, %v, want R1, nil", state, err)
		}
		assertAppACLR2TraceOmitsControlSystem(t, trace)
		if state, err := StartAppACLR2Runtime(ctx, runtime); err != nil || state != AppACLR2StateR1 {
			t.Fatalf("StartAppACLR2Runtime(R1) = %v, %v, want R1, nil", state, err)
		}
	})

	t.Run("R1 rejects finalizer", func(t *testing.T) {
		if err := FinalizeAppACLR2(ctx, migrator); err == nil {
			t.Fatal("FinalizeAppACLR2(R1) succeeded, want exact-state rejection")
		}
	})

	t.Run("R1 to PREPARED race holds the real transition advisory lock", func(t *testing.T) {
		raceFixture, _, baseRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
		const (
			runtimeApplicationName   = "houfeng-app-acl-r2-race-runtime"
			bootstrapApplicationName = "houfeng-app-acl-r2-race-bootstrap"
		)
		raceRuntime := openAppACLR2PostgresNamedPool(t, ctx, baseRuntime, runtimeApplicationName)
		raceBootstrap := openAppACLR2PostgresNamedPool(t, ctx, raceFixture.db, bootstrapApplicationName)

		raceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		classifiedR1 := make(chan struct{})
		releaseFrozenVerification := make(chan struct{})
		var classifiedOnce, releaseOnce sync.Once
		t.Cleanup(func() { releaseOnce.Do(func() { close(releaseFrozenVerification) }) })

		dependencies := defaultAppACLR2RuntimeAdmissionDependencies()
		classify := dependencies.classify
		dependencies.classify = func(callCtx context.Context, tx pgx.Tx) (AppACLR2State, error) {
			state, err := classify(callCtx, tx)
			if err == nil && state == AppACLR2StateR1 {
				classifiedOnce.Do(func() { close(classifiedR1) })
			}
			return state, err
		}
		verifyFrozen := dependencies.verifyFrozen
		dependencies.verifyFrozen = func(callCtx context.Context, tx pgx.Tx) (FrozenAppACLR1StateV1, error) {
			select {
			case <-releaseFrozenVerification:
				return verifyFrozen(callCtx, tx)
			case <-callCtx.Done():
				return FrozenAppACLR1StateV1{}, callCtx.Err()
			}
		}

		type admissionResult struct {
			state AppACLR2State
			err   error
		}
		admissionDone := make(chan admissionResult, 1)
		go func() {
			state, err := admitAppACLR2RuntimeWithDependencies(
				raceCtx,
				newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(raceRuntime)),
				dependencies,
			)
			admissionDone <- admissionResult{state: state, err: err}
		}()

		select {
		case <-classifiedR1:
		case result := <-admissionDone:
			t.Fatalf("R1 admission finished before the locked verification pause: state=%v error=%v", result.state, result.err)
		case <-raceCtx.Done():
			t.Fatalf("R1 admission did not classify under the shared transition lock: %v", raceCtx.Err())
		}

		bootstrapDone := make(chan error, 1)
		go func() {
			bootstrapDone <- BootstrapAppACLR2(raceCtx, raceBootstrap)
		}()
		waitForAppACLR2PostgresAdvisoryRace(
			t,
			raceCtx,
			raceFixture.db,
			runtimeApplicationName,
			bootstrapApplicationName,
			bootstrapDone,
		)
		select {
		case err := <-bootstrapDone:
			t.Fatalf("bootstrap finished while runtime held the shared transition lock: %v", err)
		default:
		}

		releaseOnce.Do(func() { close(releaseFrozenVerification) })
		select {
		case result := <-admissionDone:
			if result.err != nil || result.state != AppACLR2StateR1 {
				t.Fatalf("locked R1 admission = %v, %v, want R1, nil", result.state, result.err)
			}
		case <-raceCtx.Done():
			t.Fatalf("locked R1 admission did not finish: %v", raceCtx.Err())
		}
		select {
		case err := <-bootstrapDone:
			if err != nil {
				t.Fatalf("bootstrap after R1 admission released the shared lock: %v", err)
			}
		case <-raceCtx.Done():
			t.Fatalf("bootstrap did not commit PREPARED after R1 verification: %v", raceCtx.Err())
		}
		assertAppACLR2PostgresAdvisoryRaceLocksReleased(t, ctx, raceFixture.db, runtimeApplicationName, bootstrapApplicationName)

		freshTrace := &appACLR2PostgresQueryTrace{}
		freshDependencies := defaultAppACLR2RuntimeAdmissionDependencies()
		freshClassify := freshDependencies.classify
		freshClassifiedState := AppACLR2StateCorrupt
		freshDependencies.classify = func(callCtx context.Context, tx pgx.Tx) (AppACLR2State, error) {
			state, err := freshClassify(callCtx, tx)
			freshClassifiedState = state
			return state, err
		}
		frozenVerificationCalls := 0
		freshVerifyFrozen := freshDependencies.verifyFrozen
		freshDependencies.verifyFrozen = func(callCtx context.Context, tx pgx.Tx) (FrozenAppACLR1StateV1, error) {
			frozenVerificationCalls++
			return freshVerifyFrozen(callCtx, tx)
		}
		runtimePredicateCalls := 0
		freshRuntimePredicate := freshDependencies.requireDirectRuntime
		freshDependencies.requireDirectRuntime = func(callCtx context.Context, tx pgx.Tx, frozen FrozenAppACLR1StateV1) error {
			runtimePredicateCalls++
			return freshRuntimePredicate(callCtx, tx, frozen)
		}
		state, err := admitAppACLR2RuntimeWithDependencies(
			ctx,
			traceAppACLR2RuntimeBegin(
				newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(raceRuntime)),
				freshTrace,
			),
			freshDependencies,
		)
		if err == nil || state != AppACLR2StateCorrupt || freshClassifiedState != AppACLR2StatePrepared {
			t.Fatalf("fresh admission after PREPARED commit = returned:%v classified:%v error:%v, want CORRUPT/PREPARED/non-nil", state, freshClassifiedState, err)
		}
		if frozenVerificationCalls != 0 || runtimePredicateCalls != 0 {
			t.Fatalf("fresh PREPARED admission called frozen verifier/runtime predicate %d/%d times, want 0/0", frozenVerificationCalls, runtimePredicateCalls)
		}
		assertAppACLR2TraceOmitsControlSystem(t, freshTrace)
	})

	t.Run("bootstrap retries only server retry SQLSTATEs with a bounded whole closure", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			code         string
			failures     int
			wantAttempts int
			wantSuccess  bool
		}{
			{name: "serialization failure", code: "40001", failures: 1, wantAttempts: 2, wantSuccess: true},
			{name: "deadlock detected", code: "40P01", failures: 1, wantAttempts: 2, wantSuccess: true},
			{name: "non retryable lock unavailable", code: "55P03", failures: 1, wantAttempts: 1},
			{name: "retry budget exhausted", code: "40001", failures: appACLR2BootstrapMaxAttempts, wantAttempts: appACLR2BootstrapMaxAttempts},
		} {
			t.Run(tt.name, func(t *testing.T) {
				retryFixture, _, _ := newAppACLR2PostgresR1Fixture(t, ctx)
				baseBegin := newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(retryFixture.db))
				beginCalls := 0
				begin := func(callCtx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
					beginCalls++
					if options.IsoLevel != pgx.Serializable || (options.AccessMode != "" && options.AccessMode != pgx.ReadWrite) {
						t.Fatalf("bootstrap retry transaction options = %#v, want SERIALIZABLE READ WRITE", options)
					}
					return baseBegin(callCtx, options)
				}
				dependencies := defaultAppACLR2BootstrapDependencies()
				executeBootstrapSection := dependencies.executeBootstrapSection
				executeCalls := 0
				dependencies.executeBootstrapSection = func(callCtx context.Context, tx pgx.Tx) error {
					executeCalls++
					if err := executeBootstrapSection(callCtx, tx); err != nil {
						return err
					}
					if executeCalls <= tt.failures {
						return raiseAppACLR2PostgresSQLState(callCtx, tx, tt.code)
					}
					return nil
				}
				recoveryCalls := 0
				dependencies.recoverCommitAcknowledgement = func(context.Context) (appACLR2BootstrapACKOutcome, error) {
					recoveryCalls++
					return appACLR2BootstrapACKOutcomeNone, fmt.Errorf("unexpected bootstrap ACK recovery during explicit SQLSTATE retry")
				}

				err := bootstrapAppACLR2WithDependencies(ctx, begin, dependencies)
				if tt.wantSuccess {
					if err != nil {
						t.Fatalf("bootstrap retry for SQLSTATE %s: %v", tt.code, err)
					}
					assertAppACLR2PostgresState(t, ctx, retryFixture.db, AppACLR2StatePrepared)
				} else {
					if err == nil {
						t.Fatalf("bootstrap SQLSTATE %s failure succeeded", tt.code)
					}
					requirePostgresSQLState(t, err, tt.code)
					assertAppACLR2PostgresState(t, ctx, retryFixture.db, AppACLR2StateR1)
				}
				if beginCalls != tt.wantAttempts || executeCalls != tt.wantAttempts || recoveryCalls != 0 {
					t.Fatalf("bootstrap SQLSTATE %s attempts = begin:%d execute:%d recovery:%d, want %d/%d/0", tt.code, beginCalls, executeCalls, recoveryCalls, tt.wantAttempts, tt.wantAttempts)
				}
			})
		}
	})

	t.Run("finalizer retries the whole SERIALIZABLE closure after server retry SQLSTATEs", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			code string
		}{
			{name: "serialization failure", code: "40001"},
			{name: "deadlock detected", code: "40P01"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				retryFixture, retryMigrator, retryRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
				if err := BootstrapAppACLR2(ctx, retryFixture.db); err != nil {
					t.Fatalf("prepare finalizer retry fixture: %v", err)
				}
				receiptBody, receiptDigest := readAppACLR2PostgresReceipt(t, ctx, retryFixture.db)
				baseBegin := newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(retryMigrator))
				beginCalls := 0
				begin := func(callCtx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
					beginCalls++
					if options.IsoLevel != pgx.Serializable || options.AccessMode != pgx.ReadWrite {
						t.Fatalf("finalizer retry transaction options = %#v, want SERIALIZABLE READ WRITE", options)
					}
					return baseBegin(callCtx, options)
				}
				dependencies := defaultAppACLR2FinalizeDependencies(begin)
				executeFinalizeSection := dependencies.executeFinalizeSection
				ddlCalls := 0
				dependencies.executeFinalizeSection = func(callCtx context.Context, tx pgx.Tx) error {
					ddlCalls++
					return executeFinalizeSection(callCtx, tx)
				}
				readFinalized := dependencies.readFinalized
				readbackCalls := 0
				dependencies.readFinalized = func(callCtx context.Context, tx pgx.Tx) error {
					readbackCalls++
					if err := readFinalized(callCtx, tx); err != nil {
						return err
					}
					if readbackCalls == 1 {
						return raiseAppACLR2PostgresSQLState(callCtx, tx, tt.code)
					}
					return nil
				}
				recoveryCalls := 0
				dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
					recoveryCalls++
					return AppACLR2StateCorrupt, fmt.Errorf("unexpected finalizer ACK recovery during explicit SQLSTATE retry")
				}

				if err := finalizeAppACLR2WithDependencies(ctx, begin, dependencies); err != nil {
					t.Fatalf("finalizer retry for SQLSTATE %s: %v", tt.code, err)
				}
				if beginCalls != 2 || ddlCalls != 2 || readbackCalls != 2 || recoveryCalls != 0 {
					t.Fatalf("finalizer SQLSTATE %s attempts = begin:%d DDL:%d readback:%d recovery:%d, want 2/2/2/0", tt.code, beginCalls, ddlCalls, readbackCalls, recoveryCalls)
				}
				assertAppACLR2PostgresReceipt(t, ctx, retryFixture.db, receiptBody, receiptDigest)
				assertAppACLR2PostgresState(t, ctx, retryMigrator, AppACLR2StateFinalized)
				assertAppACLR2PostgresState(t, ctx, retryRuntime, AppACLR2StateFinalized)
				assertAppACLR2M2ExactSelfACLEvidence(t, ctx, retryMigrator, retryRuntime, retryFixture.migrator, retryFixture.runtime)
			})
		}
	})

	t.Run("stale M2 head CAS rejects and rolls the full finalizer back to PREPARED", func(t *testing.T) {
		casFixture, casMigrator, casRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
		if err := BootstrapAppACLR2(ctx, casFixture.db); err != nil {
			t.Fatalf("prepare stale-head CAS fixture: %v", err)
		}
		receiptBody, receiptDigest := readAppACLR2PostgresReceipt(t, ctx, casFixture.db)
		baseBegin := newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(casMigrator))
		beginCalls := 0
		begin := func(callCtx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			beginCalls++
			return baseBegin(callCtx, options)
		}
		dependencies := defaultAppACLR2FinalizeDependencies(begin)
		insertM2Revision := dependencies.insertM2Revision
		revisionInsertCalls := 0
		dependencies.insertM2Revision = func(callCtx context.Context, tx pgx.Tx, manifest AppACLManifestR2V1, body []byte, digest [32]byte) error {
			revisionInsertCalls++
			return insertM2Revision(callCtx, tx, manifest, body, digest)
		}
		compareAndSwapM2Head := dependencies.compareAndSwapM2Head
		casCalls := 0
		competingHeadObserved := false
		dependencies.compareAndSwapM2Head = func(callCtx context.Context, tx pgx.Tx, manifest AppACLManifestR2V1, digest [32]byte) error {
			casCalls++
			tag, err := tx.Exec(callCtx, `
				insert into public.app_acl_r2_manifest_head (
					singleton,
					protocol_version,
					manifest_revision,
					manifest_digest
				) values (true, $1::smallint, $2::bigint, $3::bytea)
			`, int16(manifest.ProtocolVersion), int64(manifest.ManifestRevision), digest[:])
			if err != nil {
				return fmt.Errorf("insert competing APP ACL R2 M2 head: %w", err)
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("competing APP ACL R2 M2 head did not insert exactly one row")
			}
			var revisionCount, headCount int
			if err := tx.QueryRow(callCtx, `
				select (select pg_catalog.count(*)::int from public.app_acl_r2_manifest_revisions),
				       (select pg_catalog.count(*)::int from public.app_acl_r2_manifest_head)
			`).Scan(&revisionCount, &headCount); err != nil {
				return fmt.Errorf("read competing APP ACL R2 M2 CAS rows: %w", err)
			}
			competingHeadObserved = revisionCount == 1 && headCount == 1
			return compareAndSwapM2Head(callCtx, tx, manifest, digest)
		}
		recoveryCalls := 0
		dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
			recoveryCalls++
			return AppACLR2StateCorrupt, fmt.Errorf("unexpected finalizer ACK recovery after stale-head CAS")
		}

		err := finalizeAppACLR2WithDependencies(ctx, begin, dependencies)
		if err == nil || !strings.Contains(err.Error(), "M2 head CAS did not insert exactly one row") {
			t.Fatalf("stale M2 head CAS error = %v, want exact CAS rejection", err)
		}
		if beginCalls != 1 || revisionInsertCalls != 1 || casCalls != 1 || recoveryCalls != 0 || !competingHeadObserved {
			t.Fatalf("stale M2 head CAS evidence = begin:%d revision:%d CAS:%d recovery:%d rows:%t, want 1/1/1/0/true", beginCalls, revisionInsertCalls, casCalls, recoveryCalls, competingHeadObserved)
		}
		assertAppACLR2PostgresReceipt(t, ctx, casFixture.db, receiptBody, receiptDigest)
		assertAppACLR2PostgresState(t, ctx, casMigrator, AppACLR2StatePrepared)
		assertAppACLR2PostgresState(t, ctx, casRuntime, AppACLR2StatePrepared)
		assertAppACLR2PostgresM2Absent(t, ctx, casFixture.db)

		if err := FinalizeAppACLR2(ctx, casMigrator); err != nil {
			t.Fatalf("normal finalizer after rolled-back stale-head CAS: %v", err)
		}
		assertAppACLR2PostgresState(t, ctx, casRuntime, AppACLR2StateFinalized)
	})

	bootstrapTrace := &appACLR2PostgresQueryTrace{}
	t.Run("bootstrap advances R1 to PREPARED with one live system binding", func(t *testing.T) {
		begin := traceAppACLR2BootstrapBegin(
			newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(fixture.db)),
			bootstrapTrace,
		)
		dependencies := defaultAppACLR2BootstrapDependencies()
		dependencies.recoverCommitAcknowledgement = func(ctx context.Context) (appACLR2BootstrapACKOutcome, error) {
			return recoverAppACLR2BootstrapACKWithDependencies(ctx, begin, defaultAppACLR2BootstrapACKObserverDependencies())
		}
		if err := bootstrapAppACLR2WithDependencies(ctx, begin, dependencies); err != nil {
			t.Fatalf("BootstrapAppACLR2() error = %v", err)
		}
		if got := bootstrapTrace.countContaining("from pg_catalog.pg_control_system()"); got != 1 {
			t.Fatalf("bootstrap pg_control_system() invocation count = %d, want 1", got)
		}
	})

	t.Run("ordinary exact PREPARED bootstrap repeat revalidates live binding without mutation", func(t *testing.T) {
		repeatFixture, repeatMigrator, repeatRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
		if err := BootstrapAppACLR2(ctx, repeatFixture.db); err != nil {
			t.Fatalf("prepare ordinary bootstrap repeat fixture: %v", err)
		}

		beforeReceiptBody, beforeReceiptDigest := readAppACLR2PostgresReceipt(t, ctx, repeatFixture.db)
		beforePredicates := readAppACLR2PostgresPredicates(t, ctx, repeatMigrator)
		assertAppACLR2PostgresExactPreparedPredicates(t, beforePredicates)
		beforeInventory := readAppACLR2PostgresBootstrapReservedInventory(t, ctx, repeatFixture.db)
		if len(beforeInventory) != len(appACLR2L2ReservedObjects()) ||
			!appACLR2ReservedObjectsContainOnlyKnown(beforeInventory, appACLR2L2ReservedObjects()) ||
			!appACLR2ReservedObjectsContain(beforeInventory, appACLR2L2ReservedObjects()) {
			t.Fatalf("ordinary bootstrap repeat pre-state inventory = %#v, want exact L2", beforeInventory)
		}
		assertAppACLR2PostgresReceiptLiveDatabaseBinding(t, ctx, repeatFixture.db, beforeReceiptBody)
		assertAppACLR2PostgresM2Absent(t, ctx, repeatFixture.db)

		repeatTrace := &appACLR2PostgresQueryTrace{}
		tracedBootstrap := openAppACLR2PostgresTracedPool(t, ctx, repeatFixture.db, repeatTrace)
		if err := BootstrapAppACLR2(ctx, tracedBootstrap); err != nil {
			t.Fatalf("BootstrapAppACLR2(exact PREPARED repeat) error = %v", err)
		}
		if got := repeatTrace.countContaining("from pg_catalog.pg_control_system()"); got != 1 {
			t.Fatalf("ordinary PREPARED repeat pg_control_system() invocation count = %d, want 1", got)
		}
		assertAppACLR2BootstrapRepeatTraceOmitsMutation(t, repeatTrace)

		assertAppACLR2PostgresReceipt(t, ctx, repeatFixture.db, beforeReceiptBody, beforeReceiptDigest)
		afterPredicates := readAppACLR2PostgresPredicates(t, ctx, repeatMigrator)
		assertAppACLR2PostgresExactPreparedPredicates(t, afterPredicates)
		if !reflect.DeepEqual(afterPredicates, beforePredicates) {
			t.Fatalf("ordinary bootstrap repeat changed L1/M1/L2 predicates: before=%#v after=%#v", beforePredicates, afterPredicates)
		}
		afterInventory := readAppACLR2PostgresBootstrapReservedInventory(t, ctx, repeatFixture.db)
		if !reflect.DeepEqual(afterInventory, beforeInventory) {
			t.Fatalf("ordinary bootstrap repeat changed L2 reserved inventory: before=%#v after=%#v", beforeInventory, afterInventory)
		}
		assertAppACLR2PostgresReceiptLiveDatabaseBinding(t, ctx, repeatFixture.db, beforeReceiptBody)
		assertAppACLR2PostgresM2Absent(t, ctx, repeatFixture.db)

		for _, actor := range []struct {
			name string
			role string
			db   *pgxpool.Pool
		}{
			{name: "direct-migrator", role: repeatFixture.migrator, db: repeatMigrator},
			{name: "runtime", role: repeatFixture.runtime, db: repeatRuntime},
		} {
			t.Run(actor.name+" constrained classifier omits physical identity", func(t *testing.T) {
				trace := &appACLR2PostgresQueryTrace{}
				tracedActor := openAppACLR2PostgresTracedPool(t, ctx, actor.db, trace)
				state, err := classifyAppACLR2PostgresState(ctx, tracedActor)
				if err != nil || state != AppACLR2StatePrepared {
					t.Fatalf("ClassifyAppACLR2State(PREPARED) = %v, %v, want PREPARED, nil", state, err)
				}
				assertAppACLR2TraceOmitsControlSystem(t, trace)
				assertAppACLR2NoControlSystemAuthority(t, ctx, actor.db, actor.role)
			})
		}
	})

	t.Run("shared constrained continuity rejects reversible database source and receipt domain drift", func(t *testing.T) {
		preparedFixture, preparedMigrator, preparedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
		if err := BootstrapAppACLR2(ctx, preparedFixture.db); err != nil {
			t.Fatalf("prepare shared-continuity PREPARED fixture: %v", err)
		}
		finalizedFixture, finalizedMigrator, finalizedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
		if err := BootstrapAppACLR2(ctx, finalizedFixture.db); err != nil {
			t.Fatalf("prepare shared-continuity FINALIZED fixture: %v", err)
		}
		if err := FinalizeAppACLR2(ctx, finalizedMigrator); err != nil {
			t.Fatalf("finalize shared-continuity FINALIZED fixture: %v", err)
		}

		preparedBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, preparedMigrator)
		finalizedBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, finalizedMigrator)
		assertAppACLR2PostgresState(t, ctx, preparedMigrator, AppACLR2StatePrepared)
		assertAppACLR2PostgresState(t, ctx, preparedRuntime, AppACLR2StatePrepared)
		assertAppACLR2PostgresState(t, ctx, finalizedMigrator, AppACLR2StateFinalized)
		assertAppACLR2PostgresState(t, ctx, finalizedRuntime, AppACLR2StateFinalized)

		type driftCase struct {
			name  string
			apply func(*testing.T, context.Context, *pgxpool.Pool, appACLR2PostgresContinuityBaseline) func()
		}
		cases := []driftCase{
			{
				name: "fresh database OID disagreement",
				apply: func(t *testing.T, ctx context.Context, db *pgxpool.Pool, baseline appACLR2PostgresContinuityBaseline) func() {
					t.Helper()
					domain := baseline.singleDomain(t)
					driftOID := domain.DatabaseOID + 1
					if driftOID == 0 {
						driftOID = domain.DatabaseOID - 1
					}
					rewriteAppACLR2PostgresDomainDatabaseIdentity(t, ctx, db, driftOID, domain.DatabaseName)
					return func() {
						rewriteAppACLR2PostgresDomainDatabaseIdentity(t, ctx, db, domain.DatabaseOID, domain.DatabaseName)
					}
				},
			},
			{
				name: "fresh database name disagreement",
				apply: func(t *testing.T, ctx context.Context, db *pgxpool.Pool, baseline appACLR2PostgresContinuityBaseline) func() {
					t.Helper()
					domain := baseline.singleDomain(t)
					driftName := "slice7_database_name_drift"
					if domain.DatabaseName == driftName {
						driftName = "slice7_database_name_other"
					}
					rewriteAppACLR2PostgresDomainDatabaseIdentity(t, ctx, db, domain.DatabaseOID, driftName)
					return func() {
						rewriteAppACLR2PostgresDomainDatabaseIdentity(t, ctx, db, domain.DatabaseOID, domain.DatabaseName)
					}
				},
			},
			{
				name: "application source checksum binding drift",
				apply: func(t *testing.T, ctx context.Context, db *pgxpool.Pool, baseline appACLR2PostgresContinuityBaseline) func() {
					t.Helper()
					driftChecksum := strings.Repeat("0", 64)
					if baseline.SourceChecksum == driftChecksum {
						driftChecksum = strings.Repeat("1", 64)
					}
					rewriteAppACLR2PostgresSourceChecksum(t, ctx, db, driftChecksum)
					return func() {
						rewriteAppACLR2PostgresSourceChecksum(t, ctx, db, baseline.SourceChecksum)
					}
				},
			},
			{
				name: "receipt and immutable domain disagreement",
				apply: func(t *testing.T, ctx context.Context, db *pgxpool.Pool, baseline appACLR2PostgresContinuityBaseline) func() {
					t.Helper()
					row := baseline.singleReceipt(t)
					receipt, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1(row.Body)
					if err != nil {
						t.Fatalf("parse baseline receipt for domain drift: %v", err)
					}
					domain, err := ParseCanonicalAppACLDomainR2BodyV1(receipt.DomainBody)
					if err != nil {
						t.Fatalf("parse baseline domain for receipt drift: %v", err)
					}
					domain.DomainID = "rd-" + strings.Repeat("0", 64)
					if baseline.singleDomain(t).DomainID == domain.DomainID {
						domain.DomainID = "rd-" + strings.Repeat("1", 64)
					}
					receipt.DomainBody, err = CanonicalAppACLDomainR2BodyV1(domain)
					if err != nil {
						t.Fatalf("encode receipt-domain drift: %v", err)
					}
					receipt.DomainDigest = sha256.Sum256(receipt.DomainBody)
					driftBody, err := CanonicalAppACLR2BootstrapReceiptBodyV1(receipt)
					if err != nil {
						t.Fatalf("encode canonical receipt-domain drift: %v", err)
					}
					rewriteAppACLR2PostgresReceipt(t, ctx, db, driftBody)
					return func() {
						rewriteAppACLR2PostgresReceipt(t, ctx, db, row.Body)
					}
				},
			},
		}

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				preparedRestored := false
				restorePrepared := tt.apply(t, ctx, preparedFixture.db, preparedBaseline)
				t.Cleanup(func() {
					if !preparedRestored {
						restorePrepared()
					}
				})
				finalizedRestored := false
				restoreFinalized := tt.apply(t, ctx, finalizedFixture.db, finalizedBaseline)
				t.Cleanup(func() {
					if !finalizedRestored {
						restoreFinalized()
					}
				})

				preparedDriftBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, preparedMigrator)
				finalizedDriftBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, finalizedMigrator)

				bootstrapTrace := &appACLR2PostgresQueryTrace{}
				tracedBootstrap := openAppACLR2PostgresTracedPool(t, ctx, preparedFixture.db, bootstrapTrace)
				if err := BootstrapAppACLR2(ctx, tracedBootstrap); err == nil {
					t.Fatal("BootstrapAppACLR2(PREPARED continuity drift) succeeded")
				}
				if got := bootstrapTrace.countContaining("from pg_catalog.pg_control_system()"); got != 0 {
					t.Fatalf("rejected PREPARED continuity drift invoked pg_control_system() %d times", got)
				}
				assertAppACLR2BootstrapRepeatTraceOmitsMutation(t, bootstrapTrace)

				for _, actor := range []struct {
					name string
					db   *pgxpool.Pool
				}{
					{name: "PREPARED/direct-migrator", db: preparedMigrator},
					{name: "PREPARED/runtime", db: preparedRuntime},
					{name: "FINALIZED/direct-migrator", db: finalizedMigrator},
					{name: "FINALIZED/runtime", db: finalizedRuntime},
				} {
					t.Run(actor.name, func(t *testing.T) {
						assertAppACLR2PostgresContinuityDriftRejected(t, ctx, actor.db)
					})
				}

				if after := readAppACLR2PostgresContinuityBaseline(t, ctx, preparedMigrator); !reflect.DeepEqual(after, preparedDriftBaseline) {
					t.Fatalf("rejected PREPARED continuity drift mutated evidence: before=%#v after=%#v", preparedDriftBaseline, after)
				}
				if after := readAppACLR2PostgresContinuityBaseline(t, ctx, finalizedMigrator); !reflect.DeepEqual(after, finalizedDriftBaseline) {
					t.Fatalf("rejected FINALIZED continuity drift mutated evidence: before=%#v after=%#v", finalizedDriftBaseline, after)
				}

				restoreFinalized()
				finalizedRestored = true
				restorePrepared()
				preparedRestored = true
				if restored := readAppACLR2PostgresContinuityBaseline(t, ctx, preparedMigrator); !reflect.DeepEqual(restored, preparedBaseline) {
					t.Fatalf("PREPARED continuity cleanup did not restore exact baseline: want=%#v got=%#v", preparedBaseline, restored)
				}
				if restored := readAppACLR2PostgresContinuityBaseline(t, ctx, finalizedMigrator); !reflect.DeepEqual(restored, finalizedBaseline) {
					t.Fatalf("FINALIZED continuity cleanup did not restore exact baseline: want=%#v got=%#v", finalizedBaseline, restored)
				}
				assertAppACLR2PostgresState(t, ctx, preparedMigrator, AppACLR2StatePrepared)
				assertAppACLR2PostgresState(t, ctx, preparedRuntime, AppACLR2StatePrepared)
				assertAppACLR2PostgresState(t, ctx, finalizedMigrator, AppACLR2StateFinalized)
				assertAppACLR2PostgresState(t, ctx, finalizedRuntime, AppACLR2StateFinalized)
			})
		}
	})

	t.Run("shared constrained continuity rejects transition-role attribute and recursive membership drift", func(t *testing.T) {
		roleIndex := func(t *testing.T, roles []AppACLR2CatalogRoleStateV1, name string) int {
			t.Helper()
			for index := range roles {
				if roles[index].Name == name {
					return index
				}
			}
			t.Fatalf("transition-role evidence has no role %q", name)
			return -1
		}
		type driftCase struct {
			name   string
			apply  func(*testing.T, context.Context, appACLConvergencePostgresFixture) func()
			verify func(*testing.T, appACLConvergencePostgresFixture, []AppACLR2CatalogRoleStateV1, []AppACLR2CatalogRoleStateV1)
		}
		cases := []driftCase{
			{
				name: "platform-admin CREATEDB attribute",
				apply: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) func() {
					t.Helper()
					quotedAdmin := quotePostgresIdentifier(fixture.admin)
					if _, err := fixture.db.Exec(ctx, "alter role "+quotedAdmin+" createdb"); err != nil {
						t.Fatalf("add transition-role CREATEDB drift: %v", err)
					}
					return func() {
						if _, err := fixture.db.Exec(ctx, "alter role "+quotedAdmin+" nocreatedb"); err != nil {
							t.Fatalf("restore transition-role NOCREATEDB attribute: %v", err)
						}
					}
				},
				verify: func(t *testing.T, fixture appACLConvergencePostgresFixture, before, after []AppACLR2CatalogRoleStateV1) {
					t.Helper()
					want := append([]AppACLR2CatalogRoleStateV1(nil), before...)
					want[roleIndex(t, want, fixture.admin)].CreateDatabase = true
					if !reflect.DeepEqual(after, want) {
						t.Fatalf("transition-role CREATEDB injection = %#v, want exact single-attribute drift %#v", after, want)
					}
				},
			},
			{
				name: "platform-admin recursive membership",
				apply: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) func() {
					t.Helper()
					parent := fixture.admin + "_parent"
					if !validAppACLR2RoleName(parent) {
						t.Fatalf("transition-role membership parent %q is not canonical", parent)
					}
					quotedParent := quotePostgresIdentifier(parent)
					quotedAdmin := quotePostgresIdentifier(fixture.admin)
					if _, err := fixture.db.Exec(ctx, "create role "+quotedParent+" nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls"); err != nil {
						t.Fatalf("create transition-role membership parent %q: %v", parent, err)
					}
					fixture.dropRole(t, parent)
					if _, err := fixture.db.Exec(ctx, "grant "+quotedParent+" to "+quotedAdmin); err != nil {
						t.Fatalf("add transition-role recursive-membership drift: %v", err)
					}
					return func() {
						if _, err := fixture.db.Exec(ctx, "revoke "+quotedParent+" from "+quotedAdmin); err != nil {
							t.Fatalf("restore transition-role recursive membership: %v", err)
						}
					}
				},
				verify: func(t *testing.T, fixture appACLConvergencePostgresFixture, before, after []AppACLR2CatalogRoleStateV1) {
					t.Helper()
					want := append([]AppACLR2CatalogRoleStateV1(nil), before...)
					want[roleIndex(t, want, fixture.admin)].RecursiveMembershipCount = 1
					if !reflect.DeepEqual(after, want) {
						t.Fatalf("transition-role membership injection = %#v, want exact single-membership drift %#v", after, want)
					}
				},
			},
		}

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				preparedFixture, preparedMigrator, preparedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
				if err := BootstrapAppACLR2(ctx, preparedFixture.db); err != nil {
					t.Fatalf("prepare transition-role drift PREPARED fixture: %v", err)
				}
				finalizedFixture, finalizedMigrator, finalizedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
				if err := BootstrapAppACLR2(ctx, finalizedFixture.db); err != nil {
					t.Fatalf("prepare transition-role drift FINALIZED fixture receipt: %v", err)
				}
				if err := FinalizeAppACLR2(ctx, finalizedMigrator); err != nil {
					t.Fatalf("prepare transition-role drift FINALIZED fixture M2: %v", err)
				}

				preparedFrozen := readAppACLR2PostgresFrozenR1State(t, ctx, preparedMigrator)
				finalizedFrozen := readAppACLR2PostgresFrozenR1State(t, ctx, finalizedMigrator)
				preparedRoles := readAppACLR2PostgresTransitionRoleEvidence(t, ctx, preparedMigrator, preparedFrozen)
				finalizedRoles := readAppACLR2PostgresTransitionRoleEvidence(t, ctx, finalizedMigrator, finalizedFrozen)
				preparedBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, preparedMigrator).withoutPredicates()
				finalizedBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, finalizedMigrator).withoutPredicates()

				preparedRestored := false
				restorePrepared := tt.apply(t, ctx, preparedFixture)
				t.Cleanup(func() {
					if !preparedRestored {
						restorePrepared()
					}
				})
				finalizedRestored := false
				restoreFinalized := tt.apply(t, ctx, finalizedFixture)
				t.Cleanup(func() {
					if !finalizedRestored {
						restoreFinalized()
					}
				})

				preparedDriftRoles := readAppACLR2PostgresTransitionRoleEvidence(t, ctx, preparedMigrator, preparedFrozen)
				finalizedDriftRoles := readAppACLR2PostgresTransitionRoleEvidence(t, ctx, finalizedMigrator, finalizedFrozen)
				tt.verify(t, preparedFixture, preparedRoles, preparedDriftRoles)
				tt.verify(t, finalizedFixture, finalizedRoles, finalizedDriftRoles)
				if drift := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false); !reflect.DeepEqual(drift, preparedBaseline) {
					t.Fatalf("PREPARED transition-role injection changed non-role evidence: before=%#v after=%#v", preparedBaseline, drift)
				}
				if drift := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false); !reflect.DeepEqual(drift, finalizedBaseline) {
					t.Fatalf("FINALIZED transition-role injection changed non-role evidence: before=%#v after=%#v", finalizedBaseline, drift)
				}

				bootstrapTrace := &appACLR2PostgresQueryTrace{}
				if err := BootstrapAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, preparedFixture.db, bootstrapTrace)); err == nil {
					t.Fatal("BootstrapAppACLR2(PREPARED transition-role drift) succeeded")
				}
				assertAppACLR2TraceOmitsControlSystem(t, bootstrapTrace)
				assertAppACLR2BootstrapRepeatTraceOmitsMutation(t, bootstrapTrace)

				preparedFinalizerTrace := &appACLR2PostgresQueryTrace{}
				if err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, preparedMigrator, preparedFinalizerTrace)); err == nil {
					t.Fatal("FinalizeAppACLR2(PREPARED transition-role drift) succeeded")
				}
				assertAppACLR2TraceOmitsControlSystem(t, preparedFinalizerTrace)
				for _, actor := range []struct {
					name string
					db   *pgxpool.Pool
				}{
					{name: "PREPARED/direct-migrator", db: preparedMigrator},
					{name: "PREPARED/runtime", db: preparedRuntime},
					{name: "FINALIZED/direct-migrator", db: finalizedMigrator},
					{name: "FINALIZED/runtime", db: finalizedRuntime},
				} {
					t.Run(actor.name, func(t *testing.T) {
						assertAppACLR2PostgresContinuityDriftRejected(t, ctx, actor.db)
					})
				}
				if state, err := AdmitAppACLR2Runtime(ctx, preparedRuntime); err == nil || state != AppACLR2StateCorrupt {
					t.Fatalf("AdmitAppACLR2Runtime(PREPARED transition-role drift) = %v, %v, want error-only CORRUPT", state, err)
				}

				finalizedFinalizerTrace := &appACLR2PostgresQueryTrace{}
				if err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, finalizedMigrator, finalizedFinalizerTrace)); err == nil {
					t.Fatal("FinalizeAppACLR2(FINALIZED transition-role drift) succeeded")
				}
				assertAppACLR2TraceOmitsControlSystem(t, finalizedFinalizerTrace)
				if state, err := AdmitAppACLR2Runtime(ctx, finalizedRuntime); err == nil || state != AppACLR2StateCorrupt {
					t.Fatalf("AdmitAppACLR2Runtime(FINALIZED transition-role drift) = %v, %v, want error-only CORRUPT", state, err)
				}
				if after := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false); !reflect.DeepEqual(after, preparedBaseline) {
					t.Fatalf("rejected PREPARED transition-role drift mutated state evidence: before=%#v after=%#v", preparedBaseline, after)
				}
				if after := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false); !reflect.DeepEqual(after, finalizedBaseline) {
					t.Fatalf("rejected FINALIZED transition-role drift mutated state evidence: before=%#v after=%#v", finalizedBaseline, after)
				}
				if after := readAppACLR2PostgresTransitionRoleEvidence(t, ctx, preparedMigrator, preparedFrozen); !reflect.DeepEqual(after, preparedDriftRoles) {
					t.Fatalf("rejected PREPARED transition-role drift repaired or further mutated role evidence: before=%#v after=%#v", preparedDriftRoles, after)
				}
				if after := readAppACLR2PostgresTransitionRoleEvidence(t, ctx, finalizedMigrator, finalizedFrozen); !reflect.DeepEqual(after, finalizedDriftRoles) {
					t.Fatalf("rejected FINALIZED transition-role drift repaired or further mutated role evidence: before=%#v after=%#v", finalizedDriftRoles, after)
				}

				restoreFinalized()
				finalizedRestored = true
				restorePrepared()
				preparedRestored = true
				if restored := readAppACLR2PostgresTransitionRoleEvidence(t, ctx, preparedMigrator, preparedFrozen); !reflect.DeepEqual(restored, preparedRoles) {
					t.Fatalf("PREPARED transition-role cleanup did not restore exact role evidence: want=%#v got=%#v", preparedRoles, restored)
				}
				if restored := readAppACLR2PostgresTransitionRoleEvidence(t, ctx, finalizedMigrator, finalizedFrozen); !reflect.DeepEqual(restored, finalizedRoles) {
					t.Fatalf("FINALIZED transition-role cleanup did not restore exact role evidence: want=%#v got=%#v", finalizedRoles, restored)
				}
				if restored := readAppACLR2PostgresContinuityBaseline(t, ctx, preparedMigrator).withoutPredicates(); !reflect.DeepEqual(restored, preparedBaseline) {
					t.Fatalf("PREPARED transition-role cleanup did not restore exact state evidence: want=%#v got=%#v", preparedBaseline, restored)
				}
				if restored := readAppACLR2PostgresContinuityBaseline(t, ctx, finalizedMigrator).withoutPredicates(); !reflect.DeepEqual(restored, finalizedBaseline) {
					t.Fatalf("FINALIZED transition-role cleanup did not restore exact state evidence: want=%#v got=%#v", finalizedBaseline, restored)
				}
				assertAppACLR2PostgresState(t, ctx, preparedMigrator, AppACLR2StatePrepared)
				assertAppACLR2PostgresState(t, ctx, preparedRuntime, AppACLR2StatePrepared)
				assertAppACLR2PostgresState(t, ctx, finalizedMigrator, AppACLR2StateFinalized)
				assertAppACLR2PostgresState(t, ctx, finalizedRuntime, AppACLR2StateFinalized)
			})
		}
	})

	t.Run("shared constrained continuity rejects extra L2 raw ACL grants", func(t *testing.T) {
		objectIndex := func(t *testing.T, surface AppACLR2ReceiptCatalogSnapshotV1, kind AppACLControlObjectKindR2, identity string) int {
			t.Helper()
			for index := range surface.ACL.Objects {
				object := surface.ACL.Objects[index]
				if object.Kind == kind && object.Identity == identity {
					return index
				}
			}
			t.Fatalf("APP ACL R2 L2 catalog has no object kind=%d identity=%q", kind, identity)
			return -1
		}
		type driftCase struct {
			name           string
			apply          func(*testing.T, context.Context, appACLConvergencePostgresFixture) func()
			mutateExpected func(*testing.T, *AppACLR2ReceiptCatalogSnapshotV1)
		}
		cases := []driftCase{
			{
				name: "platform-admin receipt SELECT grant",
				apply: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) func() {
					t.Helper()
					quotedAdmin := quotePostgresIdentifier(fixture.admin)
					if _, err := fixture.db.Exec(ctx, "grant select on table public.app_acl_r2_bootstrap_receipt to "+quotedAdmin); err != nil {
						t.Fatalf("grant platform-admin APP ACL R2 receipt SELECT: %v", err)
					}
					return func() {
						if _, err := fixture.db.Exec(ctx, "revoke select on table public.app_acl_r2_bootstrap_receipt from "+quotedAdmin); err != nil {
							t.Fatalf("revoke platform-admin APP ACL R2 receipt SELECT: %v", err)
						}
					}
				},
				mutateExpected: func(t *testing.T, surface *AppACLR2ReceiptCatalogSnapshotV1) {
					t.Helper()
					index := objectIndex(t, *surface, AppACLControlObjectTableR2, "app_acl_r2_bootstrap_receipt")
					surface.ACL.Objects[index].ExplicitGrants = append(surface.ACL.Objects[index].ExplicitGrants, AppACLControlGrantR2V1{
						GranteeRole: AppACLControlRolePlatformAdminR2,
						Privilege:   AppACLControlPrivilegeSelectR2,
					})
					surface.ACL.Objects[index].EffectiveRelevantPrivilegeMask |= uint8(1 << (AppACLControlRolePlatformAdminR2 - 1))
				},
			},
			{
				name: "platform-admin receipt-insert helper EXECUTE grant",
				apply: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) func() {
					t.Helper()
					quotedAdmin := quotePostgresIdentifier(fixture.admin)
					if _, err := fixture.db.Exec(ctx, "grant execute on function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea) to "+quotedAdmin); err != nil {
						t.Fatalf("grant platform-admin APP ACL R2 receipt-insert helper EXECUTE: %v", err)
					}
					return func() {
						if _, err := fixture.db.Exec(ctx, "revoke execute on function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea) from "+quotedAdmin); err != nil {
							t.Fatalf("revoke platform-admin APP ACL R2 receipt-insert helper EXECUTE: %v", err)
						}
					}
				},
				mutateExpected: func(t *testing.T, surface *AppACLR2ReceiptCatalogSnapshotV1) {
					t.Helper()
					index := objectIndex(t, *surface, AppACLControlObjectFunctionR2, "record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)")
					surface.ACL.Objects[index].ExplicitGrants = append(surface.ACL.Objects[index].ExplicitGrants, AppACLControlGrantR2V1{
						GranteeRole: AppACLControlRolePlatformAdminR2,
						Privilege:   AppACLControlPrivilegeExecuteR2,
					})
					surface.ACL.Objects[index].EffectiveRelevantPrivilegeMask |= uint8(1 << (AppACLControlRolePlatformAdminR2 - 1))
				},
			},
		}

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				preparedFixture, preparedMigrator, preparedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
				if err := BootstrapAppACLR2(ctx, preparedFixture.db); err != nil {
					t.Fatalf("prepare L2 ACL drift PREPARED fixture: %v", err)
				}
				finalizedFixture, finalizedMigrator, finalizedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
				if err := BootstrapAppACLR2(ctx, finalizedFixture.db); err != nil {
					t.Fatalf("prepare L2 ACL drift FINALIZED fixture receipt: %v", err)
				}
				if err := FinalizeAppACLR2(ctx, finalizedMigrator); err != nil {
					t.Fatalf("prepare L2 ACL drift FINALIZED fixture M2: %v", err)
				}

				preparedFrozen := readAppACLR2PostgresFrozenR1State(t, ctx, preparedMigrator)
				finalizedFrozen := readAppACLR2PostgresFrozenR1State(t, ctx, finalizedMigrator)
				preparedSurface := readAppACLR2PostgresReceiptCatalog(t, ctx, preparedMigrator, preparedFrozen)
				finalizedSurface := readAppACLR2PostgresReceiptCatalog(t, ctx, finalizedMigrator, finalizedFrozen)
				if !reflect.DeepEqual(preparedSurface.ACL, appACLR2L2ACLContract()) {
					t.Fatalf("PREPARED L2 ACL baseline = %#v, want exact fixed contract", preparedSurface.ACL)
				}
				if !reflect.DeepEqual(finalizedSurface.ACL, appACLR2L2ACLContract()) {
					t.Fatalf("FINALIZED L2 ACL baseline = %#v, want exact fixed contract", finalizedSurface.ACL)
				}
				preparedBaseline := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false)
				finalizedBaseline := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false)

				preparedRestored := false
				restorePrepared := tt.apply(t, ctx, preparedFixture)
				t.Cleanup(func() {
					if !preparedRestored {
						restorePrepared()
					}
				})
				finalizedRestored := false
				restoreFinalized := tt.apply(t, ctx, finalizedFixture)
				t.Cleanup(func() {
					if !finalizedRestored {
						restoreFinalized()
					}
				})

				wantPreparedDrift := cloneAppACLR2ReceiptCatalogSnapshot(preparedSurface)
				tt.mutateExpected(t, &wantPreparedDrift)
				wantFinalizedDrift := cloneAppACLR2ReceiptCatalogSnapshot(finalizedSurface)
				tt.mutateExpected(t, &wantFinalizedDrift)
				preparedDriftSurface := readAppACLR2PostgresReceiptCatalog(t, ctx, preparedMigrator, preparedFrozen)
				finalizedDriftSurface := readAppACLR2PostgresReceiptCatalog(t, ctx, finalizedMigrator, finalizedFrozen)
				if !reflect.DeepEqual(preparedDriftSurface, wantPreparedDrift) {
					t.Fatalf("PREPARED L2 ACL injection = %#v, want exact single-grant drift %#v", preparedDriftSurface, wantPreparedDrift)
				}
				if !reflect.DeepEqual(finalizedDriftSurface, wantFinalizedDrift) {
					t.Fatalf("FINALIZED L2 ACL injection = %#v, want exact single-grant drift %#v", finalizedDriftSurface, wantFinalizedDrift)
				}
				if after := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false); !reflect.DeepEqual(after, preparedBaseline) {
					t.Fatalf("PREPARED L2 ACL injection changed non-ACL state evidence: before=%#v after=%#v", preparedBaseline, after)
				}
				if after := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false); !reflect.DeepEqual(after, finalizedBaseline) {
					t.Fatalf("FINALIZED L2 ACL injection changed non-ACL state evidence: before=%#v after=%#v", finalizedBaseline, after)
				}

				bootstrapTrace := &appACLR2PostgresQueryTrace{}
				if err := BootstrapAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, preparedFixture.db, bootstrapTrace)); err == nil {
					t.Fatal("BootstrapAppACLR2(PREPARED L2 ACL drift) succeeded")
				}
				assertAppACLR2TraceOmitsControlSystem(t, bootstrapTrace)
				assertAppACLR2BootstrapRepeatTraceOmitsMutation(t, bootstrapTrace)

				preparedFinalizerTrace := &appACLR2PostgresQueryTrace{}
				if err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, preparedMigrator, preparedFinalizerTrace)); err == nil {
					t.Fatal("FinalizeAppACLR2(PREPARED L2 ACL drift) succeeded")
				}
				assertAppACLR2TraceOmitsControlSystem(t, preparedFinalizerTrace)
				assertAppACLR2FinalizerTraceOmitsMutation(t, preparedFinalizerTrace)

				for _, actor := range []struct {
					name string
					db   *pgxpool.Pool
				}{
					{name: "PREPARED/direct-migrator", db: preparedMigrator},
					{name: "PREPARED/runtime", db: preparedRuntime},
					{name: "FINALIZED/direct-migrator", db: finalizedMigrator},
					{name: "FINALIZED/runtime", db: finalizedRuntime},
				} {
					t.Run(actor.name, func(t *testing.T) {
						assertAppACLR2PostgresContinuityDriftRejected(t, ctx, actor.db)
					})
				}
				if state, err := AdmitAppACLR2Runtime(ctx, preparedRuntime); err == nil || state != AppACLR2StateCorrupt {
					t.Fatalf("AdmitAppACLR2Runtime(PREPARED L2 ACL drift) = %v, %v, want error-only CORRUPT", state, err)
				}

				finalizedFinalizerTrace := &appACLR2PostgresQueryTrace{}
				if err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, finalizedMigrator, finalizedFinalizerTrace)); err == nil {
					t.Fatal("FinalizeAppACLR2(FINALIZED L2 ACL drift) succeeded")
				}
				assertAppACLR2TraceOmitsControlSystem(t, finalizedFinalizerTrace)
				assertAppACLR2FinalizerTraceOmitsMutation(t, finalizedFinalizerTrace)
				if state, err := AdmitAppACLR2Runtime(ctx, finalizedRuntime); err == nil || state != AppACLR2StateCorrupt {
					t.Fatalf("AdmitAppACLR2Runtime(FINALIZED L2 ACL drift) = %v, %v, want error-only CORRUPT", state, err)
				}

				if after := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false); !reflect.DeepEqual(after, preparedBaseline) {
					t.Fatalf("rejected PREPARED L2 ACL drift mutated non-ACL state evidence: before=%#v after=%#v", preparedBaseline, after)
				}
				if after := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false); !reflect.DeepEqual(after, finalizedBaseline) {
					t.Fatalf("rejected FINALIZED L2 ACL drift mutated non-ACL state evidence: before=%#v after=%#v", finalizedBaseline, after)
				}
				if after := readAppACLR2PostgresReceiptCatalog(t, ctx, preparedMigrator, preparedFrozen); !reflect.DeepEqual(after, preparedDriftSurface) {
					t.Fatalf("rejected PREPARED L2 ACL drift repaired or further mutated raw ACL evidence: before=%#v after=%#v", preparedDriftSurface, after)
				}
				if after := readAppACLR2PostgresReceiptCatalog(t, ctx, finalizedMigrator, finalizedFrozen); !reflect.DeepEqual(after, finalizedDriftSurface) {
					t.Fatalf("rejected FINALIZED L2 ACL drift repaired or further mutated raw ACL evidence: before=%#v after=%#v", finalizedDriftSurface, after)
				}

				restoreFinalized()
				finalizedRestored = true
				restorePrepared()
				preparedRestored = true
				if restored := readAppACLR2PostgresReceiptCatalog(t, ctx, preparedMigrator, preparedFrozen); !reflect.DeepEqual(restored, preparedSurface) {
					t.Fatalf("PREPARED L2 ACL cleanup did not restore exact catalog surface: want=%#v got=%#v", preparedSurface, restored)
				}
				if restored := readAppACLR2PostgresReceiptCatalog(t, ctx, finalizedMigrator, finalizedFrozen); !reflect.DeepEqual(restored, finalizedSurface) {
					t.Fatalf("FINALIZED L2 ACL cleanup did not restore exact catalog surface: want=%#v got=%#v", finalizedSurface, restored)
				}
				if restored := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false); !reflect.DeepEqual(restored, preparedBaseline) {
					t.Fatalf("PREPARED L2 ACL cleanup changed non-ACL state evidence: want=%#v got=%#v", preparedBaseline, restored)
				}
				if restored := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false); !reflect.DeepEqual(restored, finalizedBaseline) {
					t.Fatalf("FINALIZED L2 ACL cleanup changed non-ACL state evidence: want=%#v got=%#v", finalizedBaseline, restored)
				}
				if restored := readAppACLR2PostgresFrozenR1State(t, ctx, preparedMigrator); !reflect.DeepEqual(restored, preparedFrozen) {
					t.Fatalf("PREPARED L2 ACL cleanup did not restore frozen L1/M1 evidence: want=%#v got=%#v", preparedFrozen, restored)
				}
				if restored := readAppACLR2PostgresFrozenR1State(t, ctx, finalizedMigrator); !reflect.DeepEqual(restored, finalizedFrozen) {
					t.Fatalf("FINALIZED L2 ACL cleanup did not restore frozen L1/M1 evidence: want=%#v got=%#v", finalizedFrozen, restored)
				}
				assertAppACLR2PostgresState(t, ctx, preparedMigrator, AppACLR2StatePrepared)
				assertAppACLR2PostgresState(t, ctx, preparedRuntime, AppACLR2StatePrepared)
				assertAppACLR2PostgresState(t, ctx, finalizedMigrator, AppACLR2StateFinalized)
				assertAppACLR2PostgresState(t, ctx, finalizedRuntime, AppACLR2StateFinalized)
			})
		}
	})

	t.Run("FINALIZED shared continuity rejects an internally canonical M2 domain disagreement", func(t *testing.T) {
		driftFixture, driftMigrator, driftRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
		if err := BootstrapAppACLR2(ctx, driftFixture.db); err != nil {
			t.Fatalf("prepare M2-domain drift PREPARED fixture: %v", err)
		}
		if err := FinalizeAppACLR2(ctx, driftMigrator); err != nil {
			t.Fatalf("prepare M2-domain drift FINALIZED fixture: %v", err)
		}
		baseline := readAppACLR2PostgresContinuityBaseline(t, ctx, driftMigrator)
		if len(baseline.M2Revisions) != 1 || len(baseline.M2Heads) != 1 {
			t.Fatalf("M2-domain drift baseline rows = revisions:%d heads:%d, want 1/1", len(baseline.M2Revisions), len(baseline.M2Heads))
		}
		baselineControlACL := readAppACLR2PostgresM2ControlACL(t, ctx, driftMigrator)
		var bootstrapSessionUser, bootstrapCurrentUser string
		var bootstrapRoleOID int64
		var bootstrapSuperuser bool
		if err := driftFixture.db.QueryRow(ctx, `
			select session_user::text,
			       current_user::text,
			       role.oid::bigint,
			       role.rolsuper
			from pg_catalog.pg_roles role
			where role.rolname = current_user
		`).Scan(&bootstrapSessionUser, &bootstrapCurrentUser, &bootstrapRoleOID, &bootstrapSuperuser); err != nil {
			t.Fatalf("read M2-domain drift bootstrap identity: %v", err)
		}
		if bootstrapSessionUser != driftFixture.bootstrapOwner || bootstrapCurrentUser != driftFixture.bootstrapOwner ||
			bootstrapRoleOID != 10 || !bootstrapSuperuser {
			t.Fatalf("M2-domain drift bootstrap identity = %q/%q oid=%d superuser=%t, want direct %q/%q oid=10 superuser=true",
				bootstrapSessionUser, bootstrapCurrentUser, bootstrapRoleOID, bootstrapSuperuser,
				driftFixture.bootstrapOwner, driftFixture.bootstrapOwner,
			)
		}
		assertAppACLR2PostgresState(t, ctx, driftMigrator, AppACLR2StateFinalized)
		assertAppACLR2PostgresState(t, ctx, driftRuntime, AppACLR2StateFinalized)

		driftRevision := baseline.M2Revisions[0]
		driftDomain, err := ParseCanonicalAppACLDomainR2BodyV1(driftRevision.Manifest.DomainBody)
		if err != nil {
			t.Fatalf("parse M2-domain drift baseline domain: %v", err)
		}
		driftDomain.DomainID = "rd-" + strings.Repeat("0", 64)
		if driftDomain.DomainID == baseline.singleDomain(t).DomainID {
			driftDomain.DomainID = "rd-" + strings.Repeat("1", 64)
		}
		driftRevision.Manifest.DomainBody, err = CanonicalAppACLDomainR2BodyV1(driftDomain)
		if err != nil {
			t.Fatalf("encode M2-domain drift body: %v", err)
		}
		driftRevision.Manifest.DomainDigest = sha256.Sum256(driftRevision.Manifest.DomainBody)
		driftManifestBody, err := CanonicalAppACLManifestR2BodyV1(driftRevision.Manifest)
		if err != nil {
			t.Fatalf("encode internally canonical M2-domain drift manifest: %v", err)
		}
		driftRevision.ManifestDigest = sha256.Sum256(driftManifestBody)
		if reflect.DeepEqual(driftRevision.Manifest.DomainBody, baseline.M2Revisions[0].Manifest.DomainBody) {
			t.Fatal("M2-domain drift body equals the receipt-bound baseline")
		}

		restored := false
		rewriteAppACLR2PostgresM2DomainEvidence(t, ctx, driftMigrator, driftRevision)
		t.Cleanup(func() {
			if !restored {
				rewriteAppACLR2PostgresM2DomainEvidence(t, ctx, driftMigrator, baseline.M2Revisions[0])
			}
		})
		driftBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, driftMigrator)
		if driftBaseline.SourceChecksum != baseline.SourceChecksum ||
			!reflect.DeepEqual(driftBaseline.Domains, baseline.Domains) ||
			driftBaseline.M1RevisionEvidence != baseline.M1RevisionEvidence ||
			driftBaseline.M1HeadEvidence != baseline.M1HeadEvidence ||
			!reflect.DeepEqual(driftBaseline.ReservedObjects, baseline.ReservedObjects) ||
			!reflect.DeepEqual(driftBaseline.ReceiptRows, baseline.ReceiptRows) {
			t.Fatalf("M2-domain injection changed non-M2 continuity evidence: before=%#v after=%#v", baseline, driftBaseline)
		}
		wantDriftHead := baseline.M2Heads[0]
		wantDriftHead.ManifestDigest = driftRevision.ManifestDigest
		if !reflect.DeepEqual(driftBaseline.M2Revisions, []appACLR2ManifestRowV1{driftRevision}) ||
			!reflect.DeepEqual(driftBaseline.M2Heads, []appACLR2ManifestHeadRowV1{wantDriftHead}) {
			t.Fatalf("M2-domain injection is not the exact canonical revision/head rewrite: revision=%#v head=%#v", driftBaseline.M2Revisions, driftBaseline.M2Heads)
		}
		if !driftBaseline.Predicates.ExactL1M1 || !driftBaseline.Predicates.ExactL2 ||
			driftBaseline.Predicates.ExactM2 || driftBaseline.Predicates.M2Absent ||
			driftBaseline.Predicates.HasUnknownReservedObjects {
			t.Fatalf("M2-domain disagreement predicates = %#v, want exact L1/M1/L2, present non-exact M2, no unknown objects", driftBaseline.Predicates)
		}
		if driftControlACL := readAppACLR2PostgresM2ControlACL(t, ctx, driftMigrator); !reflect.DeepEqual(driftControlACL, baselineControlACL) {
			t.Fatalf("M2-domain injection changed relation/helper/trigger/ACL evidence: before=%#v after=%#v", baselineControlACL, driftControlACL)
		}
		assertAppACLR2M2ExactSelfACLEvidence(t, ctx, driftMigrator, driftRuntime, driftFixture.migrator, driftFixture.runtime)

		finalizerTrace := &appACLR2PostgresQueryTrace{}
		tracedMigrator := openAppACLR2PostgresTracedPool(t, ctx, driftMigrator, finalizerTrace)
		if err := FinalizeAppACLR2(ctx, tracedMigrator); err == nil {
			t.Fatal("FinalizeAppACLR2(internally canonical M2-domain disagreement) succeeded")
		}
		assertAppACLR2TraceOmitsControlSystem(t, finalizerTrace)
		for _, actor := range []struct {
			name string
			db   *pgxpool.Pool
		}{
			{name: "direct-migrator", db: driftMigrator},
			{name: "runtime", db: driftRuntime},
		} {
			t.Run(actor.name, func(t *testing.T) {
				assertAppACLR2PostgresContinuityDriftRejected(t, ctx, actor.db)
			})
		}
		if state, err := AdmitAppACLR2Runtime(ctx, driftRuntime); err == nil || state != AppACLR2StateCorrupt {
			t.Fatalf("AdmitAppACLR2Runtime(M2-domain disagreement) = %v, %v, want error-only CORRUPT", state, err)
		}
		if after := readAppACLR2PostgresContinuityBaseline(t, ctx, driftMigrator); !reflect.DeepEqual(after, driftBaseline) {
			t.Fatalf("rejected M2-domain disagreement mutated evidence: before=%#v after=%#v", driftBaseline, after)
		}

		rewriteAppACLR2PostgresM2DomainEvidence(t, ctx, driftMigrator, baseline.M2Revisions[0])
		restored = true
		if after := readAppACLR2PostgresContinuityBaseline(t, ctx, driftMigrator); !reflect.DeepEqual(after, baseline) {
			t.Fatalf("M2-domain cleanup did not restore exact FINALIZED baseline: want=%#v got=%#v", baseline, after)
		}
		if restoredControlACL := readAppACLR2PostgresM2ControlACL(t, ctx, driftMigrator); !reflect.DeepEqual(restoredControlACL, baselineControlACL) {
			t.Fatalf("M2-domain cleanup did not restore relation/helper/trigger/ACL evidence: want=%#v got=%#v", baselineControlACL, restoredControlACL)
		}
		assertAppACLR2M2ExactSelfACLEvidence(t, ctx, driftMigrator, driftRuntime, driftFixture.migrator, driftFixture.runtime)
		assertAppACLR2PostgresState(t, ctx, driftMigrator, AppACLR2StateFinalized)
		assertAppACLR2PostgresState(t, ctx, driftRuntime, AppACLR2StateFinalized)
	})

	t.Run("shared constrained continuity rejects real pgcrypto member dependency owner and ACL drift", func(t *testing.T) {
		memberIndex := func(t *testing.T, members []AppACLR2PGCryptoMemberCatalogV1, oid uint32) int {
			t.Helper()
			for index := range members {
				if members[index].OID == oid {
					return index
				}
			}
			t.Fatalf("pgcrypto drift evidence has no member OID %d", oid)
			return -1
		}
		targetMember := func(t *testing.T, evidence appACLR2PostgresPGCryptoEvidence) AppACLR2PGCryptoMemberCatalogV1 {
			t.Helper()
			for _, member := range evidence.Members {
				if member.Name == "pgp_armor_headers" && member.IdentityArguments == "text, OUT key text, OUT value text" {
					return member
				}
			}
			t.Fatal("pgcrypto drift baseline has no exact pgp_armor_headers member")
			return AppACLR2PGCryptoMemberCatalogV1{}
		}
		type driftCase struct {
			name   string
			mutate func(*testing.T, context.Context, appACLConvergencePostgresFixture)
			verify func(*testing.T, appACLR2PostgresPGCryptoEvidence, appACLR2PostgresPGCryptoEvidence)
		}
		cases := []driftCase{
			{
				name: "equal-cardinality server-formatted member identity",
				mutate: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) {
					t.Helper()
					if _, err := fixture.db.Exec(ctx, "alter function record_platform_internal.pgp_armor_headers(text) rename to slice7_pgp_armor_headers_drift"); err != nil {
						t.Fatalf("rename pgcrypto member without changing extension cardinality: %v", err)
					}
				},
				verify: func(t *testing.T, before, after appACLR2PostgresPGCryptoEvidence) {
					t.Helper()
					target := targetMember(t, before)
					want := before
					want.Members = append([]AppACLR2PGCryptoMemberCatalogV1(nil), before.Members...)
					want.Members[memberIndex(t, want.Members, target.OID)].Name = "slice7_pgp_armor_headers_drift"
					if !reflect.DeepEqual(after, want) {
						t.Fatalf("equal-cardinality pgcrypto member injection = %#v, want exact single-name drift %#v", after, want)
					}
				},
			},
			{
				name: "missing extension dependency",
				mutate: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) {
					t.Helper()
					if _, err := fixture.db.Exec(ctx, "alter extension pgcrypto drop function record_platform_internal.pgp_armor_headers(text)"); err != nil {
						t.Fatalf("detach pgcrypto member dependency: %v", err)
					}
				},
				verify: func(t *testing.T, before, after appACLR2PostgresPGCryptoEvidence) {
					t.Helper()
					target := targetMember(t, before)
					want := before
					want.Members = make([]AppACLR2PGCryptoMemberCatalogV1, 0, len(before.Members)-1)
					for _, member := range before.Members {
						if member.OID != target.OID {
							want.Members = append(want.Members, member)
						}
					}
					if !reflect.DeepEqual(after, want) {
						t.Fatalf("pgcrypto dependency injection = %#v, want exact missing member dependency %#v", after, want)
					}
				},
			},
			{
				name: "member owner",
				mutate: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) {
					t.Helper()
					if _, err := fixture.db.Exec(ctx, "alter function record_platform_internal.pgp_armor_headers(text) owner to "+quotePostgresIdentifier(fixture.migrator)); err != nil {
						t.Fatalf("change pgcrypto member owner: %v", err)
					}
				},
				verify: func(t *testing.T, before, after appACLR2PostgresPGCryptoEvidence) {
					t.Helper()
					target := targetMember(t, before)
					want := before
					want.Members = append([]AppACLR2PGCryptoMemberCatalogV1(nil), before.Members...)
					index := memberIndex(t, want.Members, target.OID)
					want.Members[index].OwnerName = before.Extension.OwnerName
					want.Members[index].OwnerOID = before.Extension.OwnerOID
					if !reflect.DeepEqual(after, want) {
						t.Fatalf("pgcrypto member-owner injection = %#v, want exact owner drift %#v", after, want)
					}
				},
			},
			{
				name: "member ACL",
				mutate: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) {
					t.Helper()
					if _, err := fixture.db.Exec(ctx, "revoke execute on function record_platform_internal.pgp_armor_headers(text) from public"); err != nil {
						t.Fatalf("change pgcrypto member ACL: %v", err)
					}
				},
				verify: func(t *testing.T, before, after appACLR2PostgresPGCryptoEvidence) {
					t.Helper()
					target := targetMember(t, before)
					want := before
					want.Members = append([]AppACLR2PGCryptoMemberCatalogV1(nil), before.Members...)
					want.Members[memberIndex(t, want.Members, target.OID)].ACLIsDefault = false
					if !reflect.DeepEqual(after, want) {
						t.Fatalf("pgcrypto member-ACL injection = %#v, want exact proacl drift %#v", after, want)
					}
				},
			},
		}

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				preparedFixture, preparedMigrator, preparedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
				if err := BootstrapAppACLR2(ctx, preparedFixture.db); err != nil {
					t.Fatalf("prepare pgcrypto-drift PREPARED fixture: %v", err)
				}
				finalizedFixture, finalizedMigrator, finalizedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
				if err := BootstrapAppACLR2(ctx, finalizedFixture.db); err != nil {
					t.Fatalf("prepare pgcrypto-drift FINALIZED fixture receipt: %v", err)
				}
				if err := FinalizeAppACLR2(ctx, finalizedMigrator); err != nil {
					t.Fatalf("prepare pgcrypto-drift FINALIZED fixture M2: %v", err)
				}

				preparedBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, preparedMigrator).withoutPredicates()
				finalizedBaseline := readAppACLR2PostgresContinuityBaseline(t, ctx, finalizedMigrator).withoutPredicates()
				preparedPGCrypto := readAppACLR2PostgresPGCryptoEvidence(t, ctx, preparedMigrator)
				finalizedPGCrypto := readAppACLR2PostgresPGCryptoEvidence(t, ctx, finalizedMigrator)
				tt.mutate(t, ctx, preparedFixture)
				tt.mutate(t, ctx, finalizedFixture)
				preparedDriftPGCrypto := readAppACLR2PostgresPGCryptoEvidence(t, ctx, preparedMigrator)
				finalizedDriftPGCrypto := readAppACLR2PostgresPGCryptoEvidence(t, ctx, finalizedMigrator)
				tt.verify(t, preparedPGCrypto, preparedDriftPGCrypto)
				tt.verify(t, finalizedPGCrypto, finalizedDriftPGCrypto)

				preparedFinalizerTrace := &appACLR2PostgresQueryTrace{}
				if err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, preparedMigrator, preparedFinalizerTrace)); err == nil {
					t.Fatal("FinalizeAppACLR2(PREPARED pgcrypto drift) succeeded")
				}
				assertAppACLR2TraceOmitsControlSystem(t, preparedFinalizerTrace)
				for _, actor := range []struct {
					name string
					db   *pgxpool.Pool
				}{
					{name: "PREPARED/direct-migrator", db: preparedMigrator},
					{name: "PREPARED/runtime", db: preparedRuntime},
					{name: "FINALIZED/direct-migrator", db: finalizedMigrator},
					{name: "FINALIZED/runtime", db: finalizedRuntime},
				} {
					t.Run(actor.name, func(t *testing.T) {
						assertAppACLR2PostgresContinuityDriftRejected(t, ctx, actor.db)
					})
				}
				if state, err := AdmitAppACLR2Runtime(ctx, preparedRuntime); err == nil || state != AppACLR2StateCorrupt {
					t.Fatalf("AdmitAppACLR2Runtime(PREPARED pgcrypto drift) = %v, %v, want error-only CORRUPT", state, err)
				}

				finalizedFinalizerTrace := &appACLR2PostgresQueryTrace{}
				if err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, finalizedMigrator, finalizedFinalizerTrace)); err == nil {
					t.Fatal("FinalizeAppACLR2(FINALIZED pgcrypto drift) succeeded")
				}
				assertAppACLR2TraceOmitsControlSystem(t, finalizedFinalizerTrace)
				if state, err := AdmitAppACLR2Runtime(ctx, finalizedRuntime); err == nil || state != AppACLR2StateCorrupt {
					t.Fatalf("AdmitAppACLR2Runtime(FINALIZED pgcrypto drift) = %v, %v, want error-only CORRUPT", state, err)
				}
				if after := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false); !reflect.DeepEqual(after, preparedBaseline) {
					t.Fatalf("rejected PREPARED pgcrypto drift mutated state evidence: before=%#v after=%#v", preparedBaseline, after)
				}
				if after := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false); !reflect.DeepEqual(after, finalizedBaseline) {
					t.Fatalf("rejected FINALIZED pgcrypto drift mutated state evidence: before=%#v after=%#v", finalizedBaseline, after)
				}
				if after := readAppACLR2PostgresPGCryptoEvidence(t, ctx, preparedMigrator); !reflect.DeepEqual(after, preparedDriftPGCrypto) {
					t.Fatalf("rejected PREPARED pgcrypto drift repaired or further mutated catalog evidence: before=%#v after=%#v", preparedDriftPGCrypto, after)
				}
				if after := readAppACLR2PostgresPGCryptoEvidence(t, ctx, finalizedMigrator); !reflect.DeepEqual(after, finalizedDriftPGCrypto) {
					t.Fatalf("rejected FINALIZED pgcrypto drift repaired or further mutated catalog evidence: before=%#v after=%#v", finalizedDriftPGCrypto, after)
				}
			})
		}
	})

	t.Run("direct role switching and bound pgcrypto drop-recreate are rejected", func(t *testing.T) {
		t.Run("SET ROLE cannot satisfy the direct finalizer actor gate", func(t *testing.T) {
			switchFixture, switchMigrator, switchRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
			if err := BootstrapAppACLR2(ctx, switchFixture.db); err != nil {
				t.Fatalf("prepare SET ROLE PREPARED fixture: %v", err)
			}
			baseline := readAppACLR2PostgresContinuityBaseline(t, ctx, switchMigrator).withoutPredicates()
			setRoleMigrator := openPostgresPoolWithRole(t, ctx, switchFixture.db, switchFixture.migrator)
			var sessionUser, currentUser string
			if err := setRoleMigrator.QueryRow(ctx, `select session_user::text, current_user::text`).Scan(&sessionUser, &currentUser); err != nil {
				t.Fatalf("read SET ROLE finalizer identities: %v", err)
			}
			if sessionUser != switchFixture.bootstrapOwner || currentUser != switchFixture.migrator || sessionUser == currentUser {
				t.Fatalf("SET ROLE finalizer identities = %q/%q, want bootstrap session %q and current migrator %q", sessionUser, currentUser, switchFixture.bootstrapOwner, switchFixture.migrator)
			}
			assertAppACLR2PostgresState(t, ctx, setRoleMigrator, AppACLR2StatePrepared)

			trace := &appACLR2PostgresQueryTrace{}
			err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, setRoleMigrator, trace))
			wantErr := fmt.Sprintf("APP ACL R2 finalizer requires direct migrator role %q, got %q/%q", switchFixture.migrator, switchFixture.bootstrapOwner, switchFixture.migrator)
			if err == nil || err.Error() != wantErr {
				t.Fatalf("FinalizeAppACLR2(SET ROLE migrator) error = %v, want %q", err, wantErr)
			}
			assertAppACLR2TraceOmitsControlSystem(t, trace)
			assertAppACLR2FinalizerTraceOmitsMutation(t, trace)
			if got := trace.countContaining("lock table public.app_acl_manifest_head"); got != 0 {
				t.Fatalf("SET ROLE finalizer reached state-table locking %d times, want actor-gate rejection first", got)
			}
			if after := readAppACLR2PostgresContinuityBaseline(t, ctx, switchMigrator).withoutPredicates(); !reflect.DeepEqual(after, baseline) {
				t.Fatalf("SET ROLE finalizer rejection mutated PREPARED evidence: before=%#v after=%#v", baseline, after)
			}
			assertAppACLR2PostgresM2Absent(t, ctx, switchMigrator)
			assertAppACLR2PostgresState(t, ctx, switchMigrator, AppACLR2StatePrepared)
			assertAppACLR2PostgresState(t, ctx, switchRuntime, AppACLR2StatePrepared)
		})

		t.Run("pgcrypto drop-recreate cannot replace receipt-bound catalog identity", func(t *testing.T) {
			preparedFixture, preparedMigrator, preparedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
			if err := BootstrapAppACLR2(ctx, preparedFixture.db); err != nil {
				t.Fatalf("prepare pgcrypto drop-recreate PREPARED fixture: %v", err)
			}
			finalizedFixture, finalizedMigrator, finalizedRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
			if err := BootstrapAppACLR2(ctx, finalizedFixture.db); err != nil {
				t.Fatalf("prepare pgcrypto drop-recreate FINALIZED fixture receipt: %v", err)
			}
			if err := FinalizeAppACLR2(ctx, finalizedMigrator); err != nil {
				t.Fatalf("prepare pgcrypto drop-recreate FINALIZED fixture M2: %v", err)
			}

			preparedReceiptBody, preparedReceiptDigest := readAppACLR2PostgresReceipt(t, ctx, preparedMigrator)
			finalizedReceiptBody, finalizedReceiptDigest := readAppACLR2PostgresReceipt(t, ctx, finalizedMigrator)
			preparedPGCrypto := readAppACLR2PostgresPGCryptoEvidence(t, ctx, preparedMigrator)
			finalizedPGCrypto := readAppACLR2PostgresPGCryptoEvidence(t, ctx, finalizedMigrator)

			recreate := func(t *testing.T, migrator *pgxpool.Pool) appACLR2PostgresPGCryptoEvidence {
				t.Helper()
				if _, err := migrator.Exec(ctx, `drop extension pgcrypto cascade`); err != nil {
					t.Fatalf("drop receipt-bound pgcrypto extension: %v", err)
				}
				if _, err := migrator.Exec(ctx, `create extension pgcrypto with schema record_platform_internal`); err != nil {
					t.Fatalf("recreate receipt-bound pgcrypto extension: %v", err)
				}
				return readAppACLR2PostgresPGCryptoEvidence(t, ctx, migrator)
			}
			preparedDriftPGCrypto := recreate(t, preparedMigrator)
			finalizedDriftPGCrypto := recreate(t, finalizedMigrator)

			assertReplacement := func(t *testing.T, receiptBody string, before, after appACLR2PostgresPGCryptoEvidence) {
				t.Helper()
				if before.Extension.OID == after.Extension.OID {
					t.Fatalf("pgcrypto drop-recreate retained extension OID %d", before.Extension.OID)
				}
				wantExtension := before.Extension
				wantExtension.OID = after.Extension.OID
				if !reflect.DeepEqual(after.Extension, wantExtension) {
					t.Fatalf("recreated pgcrypto extension = %#v, want exact baseline except fresh OID %#v", after.Extension, wantExtension)
				}
				if len(after.Members) != len(before.Members) || len(after.Members) != 36 {
					t.Fatalf("recreated pgcrypto member count = %d, want baseline %d and contract 36", len(after.Members), len(before.Members))
				}
				beforeByIdentity := make(map[string]AppACLR2PGCryptoMemberCatalogV1, len(before.Members))
				for _, member := range before.Members {
					identity := member.Schema + "." + member.Name + "|" + member.IdentityArguments
					beforeByIdentity[identity] = member
				}
				afterByIdentity := make(map[string]AppACLR2PGCryptoMemberCatalogV1, len(after.Members))
				identities := make([]string, 0, len(after.Members))
				for _, member := range after.Members {
					identity := member.Schema + "." + member.Name + "|" + member.IdentityArguments
					baselineMember, ok := beforeByIdentity[identity]
					if !ok {
						t.Fatalf("recreated pgcrypto member identity %q is outside the receipt-bound baseline", identity)
					}
					if member.OID == baselineMember.OID {
						t.Fatalf("recreated pgcrypto member %q retained OID %d", identity, member.OID)
					}
					wantMember := baselineMember
					wantMember.OID = member.OID
					wantMember.ExtensionOID = after.Extension.OID
					if !reflect.DeepEqual(member, wantMember) {
						t.Fatalf("recreated pgcrypto member %q = %#v, want exact baseline except fresh member/extension OIDs %#v", identity, member, wantMember)
					}
					afterByIdentity[identity] = member
					identities = append(identities, identity)
				}
				sort.Strings(identities)
				if got := fmt.Sprintf("%x", appACLR2PGCryptoIdentitySetDigest(identities)); got != appACLR2PGCryptoCatalogDigestPG16 {
					t.Fatalf("recreated pgcrypto identity digest = %s, want unchanged %s", got, appACLR2PGCryptoCatalogDigestPG16)
				}
				receipt, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1([]byte(receiptBody))
				if err != nil {
					t.Fatalf("parse receipt-bound pgcrypto identity: %v", err)
				}
				if receipt.ExtensionOID != before.Extension.OID || receipt.ExtensionOID == after.Extension.OID {
					t.Fatalf("receipt pgcrypto extension OID = %d, want original %d and not replacement %d", receipt.ExtensionOID, before.Extension.OID, after.Extension.OID)
				}
				if len(receipt.Members) != len(before.Members) {
					t.Fatalf("receipt pgcrypto member count = %d, want original %d", len(receipt.Members), len(before.Members))
				}
				for _, member := range receipt.Members {
					identity := member.Schema + "." + member.Name + "|" + member.IdentityArguments
					baselineMember, ok := beforeByIdentity[identity]
					if !ok || member.OID != baselineMember.OID {
						t.Fatalf("receipt pgcrypto member %q OID = %d, want original catalog binding", identity, member.OID)
					}
					if replacement, ok := afterByIdentity[identity]; !ok || member.OID == replacement.OID {
						t.Fatalf("receipt pgcrypto member %q unexpectedly matches replacement OID %d", identity, member.OID)
					}
				}
			}
			assertReplacement(t, preparedReceiptBody, preparedPGCrypto, preparedDriftPGCrypto)
			assertReplacement(t, finalizedReceiptBody, finalizedPGCrypto, finalizedDriftPGCrypto)
			preparedDriftState := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false)
			finalizedDriftState := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false)
			assertAppACLR2PostgresReceipt(t, ctx, preparedMigrator, preparedReceiptBody, preparedReceiptDigest)
			assertAppACLR2PostgresReceipt(t, ctx, finalizedMigrator, finalizedReceiptBody, finalizedReceiptDigest)

			bootstrapTrace := &appACLR2PostgresQueryTrace{}
			if err := BootstrapAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, preparedFixture.db, bootstrapTrace)); err == nil {
				t.Fatal("BootstrapAppACLR2(PREPARED pgcrypto drop-recreate) succeeded")
			}
			assertAppACLR2TraceOmitsControlSystem(t, bootstrapTrace)
			assertAppACLR2BootstrapRepeatTraceOmitsMutation(t, bootstrapTrace)

			preparedFinalizerTrace := &appACLR2PostgresQueryTrace{}
			if err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, preparedMigrator, preparedFinalizerTrace)); err == nil {
				t.Fatal("FinalizeAppACLR2(PREPARED pgcrypto drop-recreate) succeeded")
			}
			assertAppACLR2TraceOmitsControlSystem(t, preparedFinalizerTrace)
			assertAppACLR2FinalizerTraceOmitsMutation(t, preparedFinalizerTrace)
			for _, actor := range []struct {
				name string
				db   *pgxpool.Pool
			}{
				{name: "PREPARED/direct-migrator", db: preparedMigrator},
				{name: "PREPARED/runtime", db: preparedRuntime},
				{name: "FINALIZED/direct-migrator", db: finalizedMigrator},
				{name: "FINALIZED/runtime", db: finalizedRuntime},
			} {
				t.Run(actor.name, func(t *testing.T) {
					assertAppACLR2PostgresContinuityDriftRejected(t, ctx, actor.db)
				})
			}
			if state, err := AdmitAppACLR2Runtime(ctx, preparedRuntime); err == nil || state != AppACLR2StateCorrupt {
				t.Fatalf("AdmitAppACLR2Runtime(PREPARED pgcrypto drop-recreate) = %v, %v, want error-only CORRUPT", state, err)
			}

			finalizedFinalizerTrace := &appACLR2PostgresQueryTrace{}
			if err := FinalizeAppACLR2(ctx, openAppACLR2PostgresTracedPool(t, ctx, finalizedMigrator, finalizedFinalizerTrace)); err == nil {
				t.Fatal("FinalizeAppACLR2(FINALIZED pgcrypto drop-recreate) succeeded")
			}
			assertAppACLR2TraceOmitsControlSystem(t, finalizedFinalizerTrace)
			assertAppACLR2FinalizerTraceOmitsMutation(t, finalizedFinalizerTrace)
			if state, err := AdmitAppACLR2Runtime(ctx, finalizedRuntime); err == nil || state != AppACLR2StateCorrupt {
				t.Fatalf("AdmitAppACLR2Runtime(FINALIZED pgcrypto drop-recreate) = %v, %v, want error-only CORRUPT", state, err)
			}

			if after := readAppACLR2PostgresContinuityEvidence(t, ctx, preparedMigrator, false); !reflect.DeepEqual(after, preparedDriftState) {
				t.Fatalf("rejected PREPARED pgcrypto drop-recreate mutated drift evidence: before=%#v after=%#v", preparedDriftState, after)
			}
			if after := readAppACLR2PostgresContinuityEvidence(t, ctx, finalizedMigrator, false); !reflect.DeepEqual(after, finalizedDriftState) {
				t.Fatalf("rejected FINALIZED pgcrypto drop-recreate mutated drift evidence: before=%#v after=%#v", finalizedDriftState, after)
			}
			if after := readAppACLR2PostgresPGCryptoEvidence(t, ctx, preparedMigrator); !reflect.DeepEqual(after, preparedDriftPGCrypto) {
				t.Fatalf("rejected PREPARED pgcrypto drop-recreate repaired or further mutated catalog evidence: before=%#v after=%#v", preparedDriftPGCrypto, after)
			}
			if after := readAppACLR2PostgresPGCryptoEvidence(t, ctx, finalizedMigrator); !reflect.DeepEqual(after, finalizedDriftPGCrypto) {
				t.Fatalf("rejected FINALIZED pgcrypto drop-recreate repaired or further mutated catalog evidence: before=%#v after=%#v", finalizedDriftPGCrypto, after)
			}
			assertAppACLR2PostgresReceipt(t, ctx, preparedMigrator, preparedReceiptBody, preparedReceiptDigest)
			assertAppACLR2PostgresReceipt(t, ctx, finalizedMigrator, finalizedReceiptBody, finalizedReceiptDigest)
			assertAppACLR2PostgresM2Absent(t, ctx, preparedMigrator)
		})
	})

	t.Run("PREPARED reader authority matrix and runtime rejection", func(t *testing.T) {
		t.Run("bootstrap-owner", func(t *testing.T) {
			assertAppACLR2PostgresState(t, ctx, fixture.db, AppACLR2StatePrepared)
			assertAppACLR2NativeTablePrivilege(t, ctx, fixture.db, "public.app_acl_r2_bootstrap_receipt", "SELECT", true)
		})
		for name, db := range map[string]*pgxpool.Pool{
			"migrator": migrator,
			"runtime":  runtime,
		} {
			t.Run(name, func(t *testing.T) {
				assertAppACLR2PostgresState(t, ctx, db, AppACLR2StatePrepared)
				assertAppACLR2NativeTablePrivilegeVector(t, ctx, db, "public.app_acl_r2_bootstrap_receipt", true)
			})
		}
		for name, db := range map[string]*pgxpool.Pool{
			"admin":     admin,
			"unrelated": unrelated,
		} {
			t.Run(name, func(t *testing.T) {
				assertAppACLR2NativeTablePrivilegeVector(t, ctx, db, "public.app_acl_r2_bootstrap_receipt", false)
				assertAppACLR2PostgresSQLState(t, ctx, db, "42501")
			})
		}
		if state, err := AdmitAppACLR2Runtime(ctx, runtime); err == nil || state != AppACLR2StateCorrupt {
			t.Fatalf("AdmitAppACLR2Runtime(PREPARED) = %v, %v, want error-only CORRUPT", state, err)
		}
		if state, err := StartAppACLR2Runtime(ctx, runtime); err == nil || state != AppACLR2StateCorrupt {
			t.Fatalf("StartAppACLR2Runtime(PREPARED) = %v, %v, want error-only CORRUPT", state, err)
		}
		if err := AdmitAppACLR1OnlyRuntime(ctx, runtime); err == nil {
			t.Fatal("AdmitAppACLR1OnlyRuntime(PREPARED) succeeded")
		}
	})

	receiptBody, receiptDigest := readAppACLR2PostgresReceipt(t, ctx, fixture.db)
	t.Run("PREPARED receipt is immutable under bootstrap-owner mutation attempts", func(t *testing.T) {
		for _, attempt := range []struct {
			name         string
			statement    string
			wantSQLState string
		}{
			{
				name: "duplicate insert",
				statement: `insert into public.app_acl_r2_bootstrap_receipt (singleton, receipt_body, receipt_digest)
					select singleton, receipt_body, receipt_digest from public.app_acl_r2_bootstrap_receipt`,
				wantSQLState: "23505",
			},
			{name: "update", statement: `update public.app_acl_r2_bootstrap_receipt set receipt_body = receipt_body`, wantSQLState: "55000"},
			{name: "delete", statement: `delete from public.app_acl_r2_bootstrap_receipt`, wantSQLState: "55000"},
			{name: "truncate", statement: `truncate table public.app_acl_r2_bootstrap_receipt`, wantSQLState: "55000"},
		} {
			t.Run(attempt.name, func(t *testing.T) {
				if _, err := fixture.db.Exec(ctx, attempt.statement); err == nil {
					t.Fatalf("receipt %s succeeded", attempt.name)
				} else {
					requirePostgresSQLState(t, err, attempt.wantSQLState)
				}
				assertAppACLR2PostgresReceipt(t, ctx, fixture.db, receiptBody, receiptDigest)
				assertAppACLR2PostgresState(t, ctx, fixture.db, AppACLR2StatePrepared)
			})
		}
	})

	finalizerTrace := &appACLR2PostgresQueryTrace{}
	t.Run("finalizer advances PREPARED to FINALIZED without physical-system query", func(t *testing.T) {
		begin := traceAppACLR2BootstrapBegin(
			newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(migrator)),
			finalizerTrace,
		)
		dependencies := defaultAppACLR2FinalizeDependencies(begin)
		readFinalized := dependencies.readFinalized
		dependencies.readFinalized = func(ctx context.Context, tx pgx.Tx) error {
			if err := readFinalized(ctx, tx); err != nil {
				predicates, diagnosticErr := ReadAppACLR2CatalogPredicatesInTx(ctx, tx)
				revisions, revisionsErr := readAppACLR2ManifestRowsInTx(ctx, tx)
				heads, headsErr := readAppACLR2ManifestHeadRowsInTx(ctx, tx)
				controlACL, controlACLErr := readAppACLR2M2ControlACLInTx(ctx, tx, predicates.FrozenState)
				var privilegeVector []bool
				privilegeErr := tx.QueryRow(ctx, `
					select array[
						pg_catalog.has_table_privilege($1, relation.oid, 'SELECT'),
						pg_catalog.has_table_privilege($1, relation.oid, 'INSERT'),
						pg_catalog.has_table_privilege($1, relation.oid, 'UPDATE'),
						pg_catalog.has_table_privilege($1, relation.oid, 'DELETE'),
						pg_catalog.has_table_privilege($1, relation.oid, 'TRUNCATE'),
						pg_catalog.has_table_privilege($1, relation.oid, 'REFERENCES'),
						pg_catalog.has_table_privilege($1, relation.oid, 'TRIGGER'),
						pg_catalog.has_table_privilege($2, relation.oid, 'SELECT'),
						pg_catalog.has_table_privilege($2, relation.oid, 'INSERT'),
						pg_catalog.has_table_privilege($2, relation.oid, 'UPDATE'),
						pg_catalog.has_table_privilege($2, relation.oid, 'DELETE'),
						pg_catalog.has_table_privilege($2, relation.oid, 'TRUNCATE'),
						pg_catalog.has_table_privilege($2, relation.oid, 'REFERENCES'),
						pg_catalog.has_table_privilege($2, relation.oid, 'TRIGGER'),
						pg_catalog.has_table_privilege($3, relation.oid, 'SELECT'),
						pg_catalog.has_table_privilege($3, relation.oid, 'INSERT'),
						pg_catalog.has_table_privilege($3, relation.oid, 'UPDATE'),
						pg_catalog.has_table_privilege($3, relation.oid, 'DELETE'),
						pg_catalog.has_table_privilege($3, relation.oid, 'TRUNCATE'),
						pg_catalog.has_table_privilege($3, relation.oid, 'REFERENCES'),
						pg_catalog.has_table_privilege($3, relation.oid, 'TRIGGER')
					]
					from pg_catalog.pg_class relation
					join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
					where namespace.nspname = 'public' and relation.relname = 'app_acl_r2_manifest_head'
				`,
					predicates.FrozenState.DirectMigratorRole,
					predicates.FrozenState.CenterRuntimeRole,
					predicates.FrozenState.PlatformAdminRole,
				).Scan(&privilegeVector)
				return fmt.Errorf(
					"%w (diagnostic predicates L1M1=%t L2=%t L2Absent=%t M2=%t M2Absent=%t unknown=%t err=%v; revisions=%d err=%v; heads=%d err=%v; control objects=%d triggers=%d defaults=%d err=%v; privileges=%v err=%v)",
					err,
					predicates.ExactL1M1,
					predicates.ExactL2,
					predicates.L2Absent,
					predicates.ExactM2,
					predicates.M2Absent,
					predicates.HasUnknownReservedObjects,
					diagnosticErr,
					len(revisions),
					revisionsErr,
					len(heads),
					headsErr,
					len(controlACL.Objects),
					len(controlACL.Triggers),
					len(controlACL.DefaultACLAssertions),
					controlACLErr,
					privilegeVector,
					privilegeErr,
				)
			}
			return nil
		}
		if err := finalizeAppACLR2WithDependencies(ctx, begin, dependencies); err != nil {
			t.Fatalf("FinalizeAppACLR2() error = %v", err)
		}
		assertAppACLR2PostgresReceipt(t, ctx, fixture.db, receiptBody, receiptDigest)
		assertAppACLR2TraceOmitsControlSystem(t, finalizerTrace)
	})

	t.Run("FINALIZED M2 raw ACL and effective authority baseline", func(t *testing.T) {
		for _, relation := range []string{
			"public.app_acl_r2_manifest_revisions",
			"public.app_acl_r2_manifest_head",
		} {
			t.Run(relation, func(t *testing.T) {
				assertAppACLR2M2RawTableACL(t, ctx, migrator, relation, fixture.migrator, fixture.runtime)
				assertAppACLR2NativeTablePrivilegeVector(t, ctx, migrator, relation, true)
				assertAppACLR2NativeTablePrivilegeVector(t, ctx, runtime, relation, true)
				assertAppACLR2NativeTablePrivilegeVector(t, ctx, admin, relation, false)
				assertAppACLR2NativeTablePrivilegeVector(t, ctx, unrelated, relation, false)
			})
		}

		assertAppACLR2M2RawHelperACL(t, ctx, migrator, fixture.migrator)
		assertAppACLR2NativeM2HelperExecute(t, ctx, migrator, true)
		assertAppACLR2NativeM2HelperExecute(t, ctx, runtime, false)
		assertAppACLR2NativeM2HelperExecute(t, ctx, admin, false)
		assertAppACLR2NativeM2HelperExecute(t, ctx, unrelated, false)
	})

	t.Run("FINALIZED reader authority matrix and runtime routing", func(t *testing.T) {
		for name, db := range map[string]*pgxpool.Pool{
			"direct-owner": migrator,
			"runtime":      runtime,
		} {
			t.Run(name, func(t *testing.T) {
				assertAppACLR2PostgresState(t, ctx, db, AppACLR2StateFinalized)
				for _, relation := range []string{
					"public.app_acl_r2_manifest_revisions",
					"public.app_acl_r2_manifest_head",
				} {
					assertAppACLR2NativeTablePrivilege(t, ctx, db, relation, "SELECT", true)
				}
			})
		}
		for name, db := range map[string]*pgxpool.Pool{
			"admin":     admin,
			"unrelated": unrelated,
		} {
			t.Run(name, func(t *testing.T) {
				for _, relation := range []string{
					"public.app_acl_r2_manifest_revisions",
					"public.app_acl_r2_manifest_head",
				} {
					assertAppACLR2NativeTablePrivilege(t, ctx, db, relation, "SELECT", false)
				}
				assertAppACLR2PostgresSQLState(t, ctx, db, "42501")
			})
		}

		trace := &appACLR2PostgresQueryTrace{}
		state, err := admitAppACLR2RuntimeWithDependencies(
			ctx,
			traceAppACLR2RuntimeBegin(
				newAppACLR2RuntimeAdmissionSharedTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(runtime)),
				trace,
			),
			defaultAppACLR2RuntimeAdmissionDependencies(),
		)
		if err != nil || state != AppACLR2StateFinalized {
			t.Fatalf("AdmitAppACLR2Runtime(FINALIZED) = %v, %v, want FINALIZED, nil", state, err)
		}
		assertAppACLR2TraceOmitsControlSystem(t, trace)
		if state, err := StartAppACLR2Runtime(ctx, runtime); err != nil || state != AppACLR2StateFinalized {
			t.Fatalf("StartAppACLR2Runtime(FINALIZED) = %v, %v, want FINALIZED, nil", state, err)
		}
		if err := AdmitAppACLR1OnlyRuntime(ctx, runtime); err == nil {
			t.Fatal("AdmitAppACLR1OnlyRuntime(FINALIZED) succeeded")
		}
	})

	t.Run("FINALIZED normal repeat uses read-compatible M2 locks", func(t *testing.T) {
		for _, relation := range []string{
			"public.app_acl_r2_manifest_revisions",
			"public.app_acl_r2_manifest_head",
		} {
			tx, err := migrator.Begin(ctx)
			if err != nil {
				t.Fatalf("begin direct-migrator M2 ACCESS SHARE probe for %q: %v", relation, err)
			}
			if _, err := tx.Exec(ctx, "lock table "+relation+" in access share mode"); err != nil {
				_ = tx.Rollback(ctx)
				t.Fatalf("direct migrator SELECT-only ACCESS SHARE lock for %q: %v", relation, err)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatalf("rollback direct-migrator ACCESS SHARE probe for %q: %v", relation, err)
			}

			tx, err = migrator.Begin(ctx)
			if err != nil {
				t.Fatalf("begin direct-migrator superseded SRE probe for %q: %v", relation, err)
			}
			_, lockErr := tx.Exec(ctx, "lock table "+relation+" in share row exclusive mode")
			if lockErr == nil {
				_ = tx.Rollback(ctx)
				t.Fatalf("direct migrator SELECT-only SHARE ROW EXCLUSIVE lock for %q succeeded", relation)
			}
			requirePostgresSQLState(t, lockErr, "42501")
			if err := tx.Rollback(ctx); err != nil {
				t.Fatalf("rollback direct-migrator superseded SRE probe for %q: %v", relation, err)
			}
		}

		repeatTrace := &appACLR2PostgresQueryTrace{}
		repeatMigrator := openAppACLR2PostgresTracedPool(t, ctx, migrator, repeatTrace)
		if err := FinalizeAppACLR2(ctx, repeatMigrator); err != nil {
			t.Fatalf("normal exact-FINALIZED repeat: %v", err)
		}
		for _, relation := range []string{
			"public.app_acl_r2_manifest_revisions",
			"public.app_acl_r2_manifest_head",
		} {
			accessShare := "lock table " + relation + " in access share mode"
			if got := repeatTrace.countContaining(accessShare); got != 1 {
				t.Fatalf("normal repeat %q ACCESS SHARE lock count = %d, want 1", relation, got)
			}
			shareRowExclusive := "lock table " + relation + " in share row exclusive mode"
			if got := repeatTrace.countContaining(shareRowExclusive); got != 0 {
				t.Fatalf("normal repeat %q superseded SRE lock count = %d, want 0", relation, got)
			}
		}
		assertAppACLR2FinalizerTraceOmitsMutation(t, repeatTrace)
		assertAppACLR2PostgresState(t, ctx, migrator, AppACLR2StateFinalized)
		assertAppACLR2M2ExactSelfACLEvidence(t, ctx, migrator, runtime, fixture.migrator, fixture.runtime)
	})

	t.Run("bootstrap rejects FINALIZED without classifying through owner bypass", func(t *testing.T) {
		if err := BootstrapAppACLR2(ctx, fixture.db); err == nil {
			t.Fatal("BootstrapAppACLR2(FINALIZED) succeeded")
		}
	})

	t.Run("FINALIZED rejects revoked owner self SELECT", func(t *testing.T) {
		const relation = "public.app_acl_r2_manifest_revisions"
		owner := quotePostgresIdentifier(fixture.migrator)
		if _, err := migrator.Exec(ctx, "revoke select on table "+relation+" from "+owner); err != nil {
			t.Fatalf("revoke M2 owner self SELECT: %v", err)
		}
		t.Cleanup(func() {
			if _, err := migrator.Exec(ctx, "grant select on table "+relation+" to "+owner); err != nil {
				t.Fatalf("restore M2 owner self SELECT: %v", err)
			}
			assertAppACLR2M2RawTableACL(t, ctx, migrator, relation, fixture.migrator, fixture.runtime)
			assertAppACLR2NativeTablePrivilegeVector(t, ctx, migrator, relation, true)
			assertAppACLR2PostgresState(t, ctx, runtime, AppACLR2StateFinalized)
		})

		assertAppACLR2M2RevokedOwnerTableSelfSelect(t, ctx, migrator, relation, fixture.migrator, fixture.runtime)
		assertAppACLR2PostgresSQLState(t, ctx, migrator, "42501")
		assertAppACLR2PostgresState(t, ctx, runtime, AppACLR2StateCorrupt)
		if err := FinalizeAppACLR2(ctx, migrator); err == nil {
			t.Fatal("FinalizeAppACLR2() repeat accepted revoked owner self SELECT")
		} else {
			requirePostgresSQLState(t, err, "42501")
		}
		assertAppACLR2FinalizeACKRecoveryRejects(t, ctx, migrator, "42501")
		assertAppACLR2M2RevokedOwnerTableSelfSelect(t, ctx, migrator, relation, fixture.migrator, fixture.runtime)
	})

	t.Run("FINALIZED rejects revoked owner self EXECUTE", func(t *testing.T) {
		const function = "record_platform_internal.app_acl_r2_reject_manifest_mutation()"
		owner := quotePostgresIdentifier(fixture.migrator)
		if _, err := migrator.Exec(ctx, "revoke execute on function "+function+" from "+owner); err != nil {
			t.Fatalf("revoke M2 owner self EXECUTE: %v", err)
		}
		t.Cleanup(func() {
			if _, err := migrator.Exec(ctx, "grant execute on function "+function+" to "+owner); err != nil {
				t.Fatalf("restore M2 owner self EXECUTE: %v", err)
			}
			assertAppACLR2M2RawHelperACL(t, ctx, migrator, fixture.migrator)
			assertAppACLR2NativeM2HelperExecute(t, ctx, migrator, true)
			assertAppACLR2PostgresState(t, ctx, runtime, AppACLR2StateFinalized)
		})

		assertAppACLR2M2RevokedOwnerHelperSelfExecute(t, ctx, migrator, fixture.migrator)
		assertAppACLR2PostgresState(t, ctx, migrator, AppACLR2StateCorrupt)
		assertAppACLR2PostgresState(t, ctx, runtime, AppACLR2StateCorrupt)
		if err := FinalizeAppACLR2(ctx, migrator); err == nil {
			t.Fatal("FinalizeAppACLR2() repeat accepted revoked owner self EXECUTE")
		}
		assertAppACLR2FinalizeACKRecoveryRejects(t, ctx, migrator, "")
		assertAppACLR2M2RevokedOwnerHelperSelfExecute(t, ctx, migrator, fixture.migrator)
	})

	t.Run("bootstrap lost ACK rejects already FINALIZED through reserved inventory only", func(t *testing.T) {
		ackFixture, ackMigrator, ackRuntime := newAppACLR2PostgresR1Fixture(t, ctx)

		r1ObserverTrace := &appACLR2PostgresQueryTrace{}
		r1Outcome, err := observeAppACLR2PostgresBootstrapACK(ctx, ackFixture.db, r1ObserverTrace)
		if err != nil || r1Outcome != appACLR2BootstrapACKOutcomeR1 {
			t.Fatalf("bootstrap ACK observer on exact R1 = %v, %v, want R1, nil", r1Outcome, err)
		}
		assertAppACLR2BootstrapACKTraceOmitsM2Access(t, r1ObserverTrace, 1)

		bootstrapMutationTrace := &appACLR2PostgresQueryTrace{}
		bootstrapObserverTrace := &appACLR2PostgresQueryTrace{}
		baseBegin := newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(ackFixture.db))
		fault := newAppACLR2PostgresCommitACKFault(func(callbackCtx context.Context) error {
			return FinalizeAppACLR2(callbackCtx, ackMigrator)
		})
		begin := fault.wrap(traceAppACLR2BootstrapBegin(baseBegin, bootstrapMutationTrace))
		dependencies := defaultAppACLR2BootstrapDependencies()
		var recoveryCalls int
		var recoveryOutcome appACLR2BootstrapACKOutcome
		var recoveryErr error
		dependencies.recoverCommitAcknowledgement = func(recoveryCtx context.Context) (appACLR2BootstrapACKOutcome, error) {
			recoveryCalls++
			recoveryOutcome, recoveryErr = recoverAppACLR2BootstrapACKWithDependencies(
				recoveryCtx,
				traceAppACLR2BootstrapBegin(baseBegin, bootstrapObserverTrace),
				defaultAppACLR2BootstrapACKObserverDependencies(),
			)
			return recoveryOutcome, recoveryErr
		}
		bootstrapErr := bootstrapAppACLR2WithDependencies(ctx, begin, dependencies)
		if bootstrapErr == nil {
			t.Fatal("bootstrap with lost commit ACK accepted already FINALIZED recovery state")
		}
		injections, callbackErr := fault.evidence()
		if injections != 1 || callbackErr != nil {
			t.Fatalf("bootstrap commit ACK fault evidence = injections:%d callback-error:%v, want 1/nil", injections, callbackErr)
		}
		if recoveryCalls != 1 || recoveryOutcome != appACLR2BootstrapACKOutcomeNone || recoveryErr == nil || bootstrapErr != recoveryErr {
			t.Fatalf("bootstrap ACK recovery = calls:%d outcome:%v error:%v propagated:%t, want 1/none/non-nil/true", recoveryCalls, recoveryOutcome, recoveryErr, bootstrapErr == recoveryErr)
		}
		if got := bootstrapMutationTrace.countContaining("from pg_catalog.pg_control_system()"); got != 1 {
			t.Fatalf("lost-ACK bootstrap pg_control_system() invocation count = %d, want 1", got)
		}
		assertAppACLR2BootstrapACKReservedInventoryOnlyTrace(t, bootstrapObserverTrace)
		assertAppACLR2PostgresState(t, ctx, ackMigrator, AppACLR2StateFinalized)
		assertAppACLR2PostgresState(t, ctx, ackRuntime, AppACLR2StateFinalized)
	})

	t.Run("bootstrap ACK observer rejects every non-exact L2 reserved inventory", func(t *testing.T) {
		r1Fixture, _, _ := newAppACLR2PostgresR1Fixture(t, ctx)
		preparedFixture, _, _ := newAppACLR2PostgresR1Fixture(t, ctx)
		if err := BootstrapAppACLR2(ctx, preparedFixture.db); err != nil {
			t.Fatalf("prepare reserved-inventory ACK observer fixture: %v", err)
		}

		m2Fixtures := map[string]appACLR2PostgresReservedInventoryFixture{
			"record_platform_internal.app_acl_r2_reject_manifest_mutation()": {
				setupSQL: []string{`create function record_platform_internal.app_acl_r2_reject_manifest_mutation()
returns trigger
language plpgsql
as $slice7$
begin
  return null;
end
$slice7$`},
				cleanupSQL:         []string{`drop function if exists record_platform_internal.app_acl_r2_reject_manifest_mutation()`},
				wantInventoryCount: 6,
			},
			"app_acl_r2_manifest_head": {
				setupSQL:           []string{`create table public.app_acl_r2_manifest_head (fixture integer)`},
				cleanupSQL:         []string{`drop table if exists public.app_acl_r2_manifest_head`},
				wantInventoryCount: 6,
			},
			"app_acl_r2_manifest_head_pkey": {
				setupSQL:           []string{`create table public.app_acl_r2_manifest_head (fixture integer primary key)`},
				cleanupSQL:         []string{`drop table if exists public.app_acl_r2_manifest_head`},
				wantInventoryCount: 7,
			},
			"app_acl_r2_manifest_revisions": {
				setupSQL:           []string{`create table public.app_acl_r2_manifest_revisions (fixture integer)`},
				cleanupSQL:         []string{`drop table if exists public.app_acl_r2_manifest_revisions`},
				wantInventoryCount: 6,
			},
			"app_acl_r2_manifest_revisions_pkey": {
				setupSQL:           []string{`create table public.app_acl_r2_manifest_revisions (fixture integer primary key)`},
				cleanupSQL:         []string{`drop table if exists public.app_acl_r2_manifest_revisions`},
				wantInventoryCount: 7,
			},
			"app_acl_r2_manifest_revisions_protocol_version_manifest_rev_key": {
				setupSQL: []string{`create table public.app_acl_r2_manifest_revisions (
  protocol_version integer not null,
  manifest_revision bigint not null,
  constraint app_acl_r2_manifest_revisions_protocol_version_manifest_rev_key unique (protocol_version, manifest_revision)
)`},
				cleanupSQL:         []string{`drop table if exists public.app_acl_r2_manifest_revisions`},
				wantInventoryCount: 7,
			},
			"app_acl_r2_manifest_head.app_acl_r2_manifest_head_immutable": {
				setupSQL: []string{
					`create function record_platform_internal.slice7_trigger_fixture()
returns trigger
language plpgsql
as $slice7$
begin
  return null;
end
$slice7$`,
					`create table public.app_acl_r2_manifest_head (fixture integer)`,
					`create trigger app_acl_r2_manifest_head_immutable
before insert on public.app_acl_r2_manifest_head
for each statement execute function record_platform_internal.slice7_trigger_fixture()`,
				},
				cleanupSQL: []string{
					`drop table if exists public.app_acl_r2_manifest_head`,
					`drop function if exists record_platform_internal.slice7_trigger_fixture()`,
				},
				wantInventoryCount: 7,
			},
			"app_acl_r2_manifest_revisions.app_acl_r2_manifest_revisions_immutable": {
				setupSQL: []string{
					`create function record_platform_internal.slice7_trigger_fixture()
returns trigger
language plpgsql
as $slice7$
begin
  return null;
end
$slice7$`,
					`create table public.app_acl_r2_manifest_revisions (fixture integer)`,
					`create trigger app_acl_r2_manifest_revisions_immutable
before insert on public.app_acl_r2_manifest_revisions
for each statement execute function record_platform_internal.slice7_trigger_fixture()`,
				},
				cleanupSQL: []string{
					`drop table if exists public.app_acl_r2_manifest_revisions`,
					`drop function if exists record_platform_internal.slice7_trigger_fixture()`,
				},
				wantInventoryCount: 7,
			},
		}

		type rejectionCase struct {
			name        string
			db          *pgxpool.Pool
			fixture     appACLR2PostgresReservedInventoryFixture
			wantObjects []AppACLR2ReservedCatalogObjectV1
			wantExactL2 bool
		}
		unknownObject := AppACLR2ReservedCatalogObjectV1{
			Kind: "relation", Schema: "public", Identity: "app_acl_r2_unknown_inventory", Detail: "r",
		}
		excessObject := AppACLR2ReservedCatalogObjectV1{
			Kind: "relation", Schema: "public", Identity: "app_acl_r2_excessive_l2_inventory", Detail: "r",
		}
		cases := []rejectionCase{
			{
				name: "unknown inventory",
				db:   r1Fixture.db,
				fixture: appACLR2PostgresReservedInventoryFixture{
					setupSQL:           []string{`create table public.app_acl_r2_unknown_inventory (fixture integer)`},
					cleanupSQL:         []string{`drop table if exists public.app_acl_r2_unknown_inventory`},
					wantInventoryCount: 1,
				},
				wantObjects: []AppACLR2ReservedCatalogObjectV1{unknownObject},
			},
			{
				name: "partial L2 inventory",
				db:   r1Fixture.db,
				fixture: appACLR2PostgresReservedInventoryFixture{
					setupSQL:           []string{`create table public.app_acl_r2_bootstrap_receipt (fixture integer)`},
					cleanupSQL:         []string{`drop table if exists public.app_acl_r2_bootstrap_receipt`},
					wantInventoryCount: 1,
				},
				wantObjects: []AppACLR2ReservedCatalogObjectV1{appACLR2L2ReceiptRelation()},
			},
			{
				name: "excessive L2 inventory",
				db:   preparedFixture.db,
				fixture: appACLR2PostgresReservedInventoryFixture{
					setupSQL:           []string{`create table public.app_acl_r2_excessive_l2_inventory (fixture integer)`},
					cleanupSQL:         []string{`drop table if exists public.app_acl_r2_excessive_l2_inventory`},
					wantInventoryCount: 6,
				},
				wantObjects: []AppACLR2ReservedCatalogObjectV1{excessObject},
				wantExactL2: true,
			},
			{
				name: "mixed L2 and M2 inventory",
				db:   preparedFixture.db,
				fixture: appACLR2PostgresReservedInventoryFixture{
					setupSQL: []string{
						`create table public.app_acl_r2_manifest_head (fixture integer)`,
						`create table public.app_acl_r2_manifest_revisions (fixture integer)`,
					},
					cleanupSQL: []string{
						`drop table if exists public.app_acl_r2_manifest_head`,
						`drop table if exists public.app_acl_r2_manifest_revisions`,
					},
					wantInventoryCount: 7,
				},
				wantObjects: []AppACLR2ReservedCatalogObjectV1{appACLR2M2HeadRelation(), appACLR2M2RevisionsRelation()},
				wantExactL2: true,
			},
		}

		seenM2Identities := make(map[string]struct{}, len(m2Fixtures))
		for _, object := range appACLR2M2ReservedObjects() {
			if _, duplicate := seenM2Identities[object.Identity]; duplicate {
				t.Fatalf("duplicate M2 reserved identity %q", object.Identity)
			}
			fixture, ok := m2Fixtures[object.Identity]
			if !ok {
				t.Fatalf("M2 reserved identity %q has no PostgreSQL rejection fixture", object.Identity)
			}
			seenM2Identities[object.Identity] = struct{}{}
			cases = append(cases, rejectionCase{
				name:        "M2 reserved identity " + object.Identity,
				db:          preparedFixture.db,
				fixture:     fixture,
				wantObjects: []AppACLR2ReservedCatalogObjectV1{object},
				wantExactL2: true,
			})
		}
		if len(seenM2Identities) != len(m2Fixtures) {
			extra := make([]string, 0, len(m2Fixtures)-len(seenM2Identities))
			for identity := range m2Fixtures {
				if _, ok := seenM2Identities[identity]; !ok {
					extra = append(extra, identity)
				}
			}
			sort.Strings(extra)
			t.Fatalf("PostgreSQL M2 rejection fixtures contain non-contract identities: %v", extra)
		}

		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				assertAppACLR2PostgresBootstrapACKRejectsReservedInventory(
					t, ctx, tt.db, tt.fixture, tt.wantObjects, tt.wantExactL2,
				)
			})
		}
	})

	t.Run("finalizer lost ACK recovers exact FINALIZED with owner self ACL", func(t *testing.T) {
		ackFixture, ackMigrator, ackRuntime := newAppACLR2PostgresR1Fixture(t, ctx)
		if err := BootstrapAppACLR2(ctx, ackFixture.db); err != nil {
			t.Fatalf("prepare finalizer ACK-loss fixture: %v", err)
		}

		preparedObserverTrace := &appACLR2PostgresQueryTrace{}
		preparedOutcome, err := observeAppACLR2PostgresBootstrapACK(ctx, ackFixture.db, preparedObserverTrace)
		if err != nil || preparedOutcome != appACLR2BootstrapACKOutcomePrepared {
			t.Fatalf("bootstrap ACK observer on exact PREPARED = %v, %v, want PREPARED, nil", preparedOutcome, err)
		}
		if got := preparedObserverTrace.countContaining("from pg_catalog.pg_control_system()"); got != 1 {
			t.Fatalf("PREPARED ACK observer pg_control_system() invocation count = %d, want 1", got)
		}
		assertAppACLR2BootstrapACKTraceOmitsM2Access(t, preparedObserverTrace, 3)

		finalizerTrace := &appACLR2PostgresQueryTrace{}
		baseBegin := traceAppACLR2BootstrapBegin(
			newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(ackMigrator)),
			finalizerTrace,
		)
		fault := newAppACLR2PostgresCommitACKFault(nil)
		begin := fault.wrap(baseBegin)
		dependencies := defaultAppACLR2FinalizeDependencies(begin)
		recoverCommitAcknowledgement := dependencies.recoverCommitAcknowledgement
		var recoveryCalls int
		var recoveryState AppACLR2State
		var recoveryErr error
		dependencies.recoverCommitAcknowledgement = func(recoveryCtx context.Context) (AppACLR2State, error) {
			recoveryCalls++
			recoveryState, recoveryErr = recoverCommitAcknowledgement(recoveryCtx)
			return recoveryState, recoveryErr
		}
		if err := finalizeAppACLR2WithDependencies(ctx, begin, dependencies); err != nil {
			t.Fatalf("finalizer lost commit ACK recovery error = %v", err)
		}
		injections, callbackErr := fault.evidence()
		if injections != 1 || callbackErr != nil {
			t.Fatalf("finalizer commit ACK fault evidence = injections:%d callback-error:%v, want 1/nil", injections, callbackErr)
		}
		if recoveryCalls != 1 || recoveryState != AppACLR2StateFinalized || recoveryErr != nil {
			t.Fatalf("finalizer ACK recovery = calls:%d state:%v error:%v, want 1/FINALIZED/nil", recoveryCalls, recoveryState, recoveryErr)
		}
		assertAppACLR2TraceOmitsControlSystem(t, finalizerTrace)
		assertAppACLR2PostgresState(t, ctx, ackMigrator, AppACLR2StateFinalized)
		assertAppACLR2PostgresState(t, ctx, ackRuntime, AppACLR2StateFinalized)
		assertAppACLR2M2ExactSelfACLEvidence(t, ctx, ackMigrator, ackRuntime, ackFixture.migrator, ackFixture.runtime)
	})
}

func newAppACLR2PostgresR1Fixture(
	t *testing.T,
	ctx context.Context,
) (appACLConvergencePostgresFixture, *pgxpool.Pool, *pgxpool.Pool) {
	t.Helper()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	provisionAppACLR2ControlSystemACL(t, ctx, fixture.db)
	assertAppACLR2ProvisionedControlSystemACL(t, ctx, fixture.db)
	migrator := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	if _, err := ConvergeAppACLR1(ctx, migrator, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLR1() for ACK-loss fixture error = %v", err)
	}
	if _, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, fixture.db, platformmigrate.DomainKindApplication); err != nil {
		t.Fatalf("ProvisionPostgresDomainIdentity() for ACK-loss fixture error = %v", err)
	}
	assertAppACLR2PostgresState(t, ctx, fixture.db, AppACLR2StateR1)
	return fixture, migrator, runtime
}

func assertAppACLR2PG16RunnerFixtureDatabases(t *testing.T, ctx context.Context, wantServerVersion uint32) {
	t.Helper()
	fixtures := []struct {
		name string
		env  string
	}{
		{name: "application", env: "HOUFENG_DATABASE_URL"},
		{name: "deletion-ledger", env: "HOUFENG_DELETION_LEDGER_DATABASE_URL"},
		{name: "deletion-witness", env: "HOUFENG_DELETION_WITNESS_DATABASE_URL"},
		{name: "recovery-control", env: "HOUFENG_RECOVERY_CONTROL_DATABASE_URL"},
	}
	identifiers := make(map[string]string, len(fixtures))
	for _, fixture := range fixtures {
		dsn := strings.TrimSpace(os.Getenv(fixture.env))
		if dsn == "" {
			t.Fatalf("%s is required for the strict four-fixture image propagation check", fixture.env)
		}
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect %s fixture: %v", fixture.name, err)
		}
		var gotServerVersion uint32
		var systemIdentifier string
		queryErr := conn.QueryRow(ctx, `
			select current_setting('server_version_num')::int,
			       system_identifier::text
			from pg_catalog.pg_control_system()
		`).Scan(&gotServerVersion, &systemIdentifier)
		closeErr := conn.Close(ctx)
		if queryErr != nil {
			t.Fatalf("read %s fixture PG16 identity: %v", fixture.name, queryErr)
		}
		if closeErr != nil {
			t.Fatalf("close %s fixture connection: %v", fixture.name, closeErr)
		}
		if gotServerVersion != wantServerVersion {
			t.Fatalf("%s fixture server_version_num = %d, want %d", fixture.name, gotServerVersion, wantServerVersion)
		}
		if systemIdentifier == "" {
			t.Fatalf("%s fixture returned an empty PostgreSQL system identifier", fixture.name)
		}
		if previous, duplicate := identifiers[systemIdentifier]; duplicate {
			t.Fatalf("%s and %s fixtures share PostgreSQL system identifier %q", fixture.name, previous, systemIdentifier)
		}
		identifiers[systemIdentifier] = fixture.name
	}
	if len(identifiers) != len(fixtures) {
		t.Fatalf("strict four-fixture system identifier count = %d, want %d", len(identifiers), len(fixtures))
	}
}

func assertAppACLR2StrictRunnerBehavior(t *testing.T) {
	t.Helper()
	runner := appACLR2StrictRunnerPath(t)
	for _, image := range []string{
		"postgres",
		"postgres:16",
		"postgres:16-alpine",
		"postgres:17",
		"latest",
		" postgres:16.0",
		"postgres:16.0 ",
	} {
		t.Run("rejects "+image+" before fixture setup", func(t *testing.T) {
			fake := newAppACLR2FakeRunnerToolchain(t)
			code, output := runAppACLR2StrictRunner(t, runner, fake, &image, []string{"/bin/true"})
			if code != 2 {
				t.Fatalf("invalid pg16-catalog image %q exit code = %d, output %q, want 2", image, code, output)
			}
			assertAppACLR2FakeRunnerNoSideEffects(t, fake)
		})
	}
	t.Run("rejects an unset image before fixture setup", func(t *testing.T) {
		fake := newAppACLR2FakeRunnerToolchain(t)
		code, output := runAppACLR2StrictRunner(t, runner, fake, nil, []string{"/bin/true"})
		if code != 2 {
			t.Fatalf("unset pg16-catalog image exit code = %d, output %q, want 2", code, output)
		}
		assertAppACLR2FakeRunnerNoSideEffects(t, fake)
	})
	t.Run("rejects an empty child command before fixture setup", func(t *testing.T) {
		fake := newAppACLR2FakeRunnerToolchain(t)
		image := "postgres:16.12"
		code, output := runAppACLR2StrictRunner(t, runner, fake, &image, []string{""})
		if code != 2 {
			t.Fatalf("empty child command exit code = %d, output %q, want 2", code, output)
		}
		assertAppACLR2FakeRunnerNoSideEffects(t, fake)
	})

	for _, image := range []string{"postgres:16.0", "postgres:16.6", "postgres:16.12"} {
		t.Run("propagates "+image+" to all fixtures and cleans up", func(t *testing.T) {
			fake := newAppACLR2FakeRunnerToolchain(t)
			code, output := runAppACLR2StrictRunner(t, runner, fake, &image, []string{"/bin/true"})
			if code != 0 {
				dockerLog, _ := os.ReadFile(fake.dockerLog)
				t.Fatalf("accepted pg16-catalog image %q exit code = %d, output %q, fake Docker log %q", image, code, output, dockerLog)
			}
			assertAppACLR2FakeRunnerLifecycle(t, fake, image)
		})
	}
	t.Run("turns a child skip into failure and still cleans up", func(t *testing.T) {
		fake := newAppACLR2FakeRunnerToolchain(t)
		image := "postgres:16.12"
		code, output := runAppACLR2StrictRunner(t, runner, fake, &image, []string{
			"/bin/sh",
			"-c",
			"printf '%s\\n' '--- SKIP: nested strict-runner fixture'",
		})
		if code != 1 {
			t.Fatalf("child skip strict-runner exit code = %d, output %q, want 1", code, output)
		}
		if !strings.Contains(string(output), "--- SKIP: nested strict-runner fixture") {
			t.Fatalf("child skip output = %q, want the emitted skip marker", output)
		}
		assertAppACLR2FakeRunnerLifecycle(t, fake, image)
	})
}

func appACLR2StrictRunnerPath(t *testing.T) string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("read APP ACL R2 integration test working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			path := filepath.Join(directory, "scripts", "test-record-platform-integration.sh")
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("locate strict record-platform runner %q: %v", path, err)
			}
			return path
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("could not locate repository root for strict record-platform runner")
		}
		directory = parent
	}
}

type appACLR2FakeRunnerToolchain struct {
	bin           string
	tmpParent     string
	dockerLog     string
	sideEffectLog string
}

func newAppACLR2FakeRunnerToolchain(t *testing.T) *appACLR2FakeRunnerToolchain {
	t.Helper()
	root := t.TempDir()
	toolchain := &appACLR2FakeRunnerToolchain{
		bin:           filepath.Join(root, "bin"),
		tmpParent:     filepath.Join(root, "tmp"),
		dockerLog:     filepath.Join(root, "docker.log"),
		sideEffectLog: filepath.Join(root, "side-effects.log"),
	}
	if err := os.MkdirAll(toolchain.bin, 0o755); err != nil {
		t.Fatalf("create fake runner bin: %v", err)
	}
	if err := os.MkdirAll(toolchain.tmpParent, 0o755); err != nil {
		t.Fatalf("create fake runner TMPDIR: %v", err)
	}
	for _, command := range []string{"od", "seq", "rg", "sort", "wc", "tr", "tee", "rm", "sleep"} {
		target, err := exec.LookPath(command)
		if err != nil {
			t.Fatalf("locate host command %q for fake runner: %v", command, err)
		}
		if err := os.Symlink(target, filepath.Join(toolchain.bin, command)); err != nil {
			t.Fatalf("link host command %q into fake runner: %v", command, err)
		}
	}
	writeExecutable := func(name, body string) {
		if err := os.WriteFile(filepath.Join(toolchain.bin, name), []byte(body), 0o755); err != nil {
			t.Fatalf("write fake runner command %q: %v", name, err)
		}
	}
	writeExecutable("mktemp", `#!/usr/bin/bash
set -euo pipefail
printf 'mktemp\n' >> "${HOUFENG_APP_ACLR2_FAKE_SIDE_EFFECT_LOG:?}"
exec /usr/bin/mktemp "$@"
`)
	writeExecutable("ss", "#!/usr/bin/bash\nexit 1\n")
	writeExecutable("docker", `#!/usr/bin/bash
set -euo pipefail
log=${HOUFENG_APP_ACLR2_FAKE_DOCKER_LOG:?}
command=$1
shift
case "$command" in
  run)
    name=
    image=
    while (($#)); do
      case "$1" in
        --name)
          name=$2
          shift 2
          ;;
        --tmpfs|-e)
          shift 2
          ;;
        --rm|-d|--network=host)
          shift
          ;;
        -c)
          shift 2
          ;;
        postgres:*)
          image=$1
          shift
          ;;
        *)
          shift
          ;;
      esac
    done
    printf 'run\t%s\t%s\n' "$name" "$image" >> "$log"
    printf 'fake-container\n'
    ;;
  exec)
    container=$1
    shift
    case "${1-}" in
      pg_isready)
        ;;
      psql)
        printf '%s\n' "${container}-system-identifier"
        ;;
    esac
    printf 'exec\t%s\t%s\n' "$container" "${1-}" >> "$log"
    ;;
  rm)
    printf 'rm\t%s\n' "${2-}" >> "$log"
    ;;
  logs)
    ;;
  *)
    ;;
esac
`)
	return toolchain
}

func runAppACLR2StrictRunner(
	t *testing.T,
	runner string,
	fake *appACLR2FakeRunnerToolchain,
	image *string,
	child []string,
) (int, []byte) {
	t.Helper()
	args := append([]string{runner, "pg16-catalog", "--"}, child...)
	command := exec.Command("/usr/bin/bash", args...)
	command.Env = appACLR2StrictRunnerEnvironment(fake, image)
	output, err := command.CombinedOutput()
	if err == nil {
		return 0, output
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), output
	}
	t.Fatalf("run strict record-platform runner: %v", err)
	return -1, output
}

func appACLR2StrictRunnerEnvironment(fake *appACLR2FakeRunnerToolchain, image *string) []string {
	blocked := map[string]struct{}{
		"HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE": {},
		"HOUFENG_APP_ACLR2_FAKE_DOCKER_LOG":      {},
		"HOUFENG_APP_ACLR2_FAKE_SIDE_EFFECT_LOG": {},
		"PATH":                                   {},
		"TMPDIR":                                 {},
	}
	environment := make([]string, 0, len(os.Environ())+4)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if _, skip := blocked[key]; !skip {
			environment = append(environment, entry)
		}
	}
	environment = append(environment,
		"PATH="+fake.bin,
		"TMPDIR="+fake.tmpParent,
		"HOUFENG_APP_ACLR2_FAKE_DOCKER_LOG="+fake.dockerLog,
		"HOUFENG_APP_ACLR2_FAKE_SIDE_EFFECT_LOG="+fake.sideEffectLog,
	)
	if image != nil {
		environment = append(environment, "HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE="+*image)
	}
	return environment
}

func assertAppACLR2FakeRunnerNoSideEffects(t *testing.T, fake *appACLR2FakeRunnerToolchain) {
	t.Helper()
	if _, err := os.Stat(fake.dockerLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid strict runner docker log stat = %v, want absent", err)
	}
	if _, err := os.Stat(fake.sideEffectLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid strict runner side-effect log stat = %v, want absent before mktemp", err)
	}
	entries, err := os.ReadDir(fake.tmpParent)
	if err != nil {
		t.Fatalf("read invalid strict runner TMPDIR: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("invalid strict runner TMPDIR entries = %v, want none", entries)
	}
}

func assertAppACLR2FakeRunnerLifecycle(t *testing.T, fake *appACLR2FakeRunnerToolchain, wantImage string) {
	t.Helper()
	contents, err := os.ReadFile(fake.dockerLog)
	if err != nil {
		t.Fatalf("read fake strict runner Docker log: %v", err)
	}
	var runNames, removeNames []string
	seenPrefixes := make(map[string]bool, 4)
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		switch fields[0] {
		case "run":
			if len(fields) != 3 || fields[2] != wantImage {
				t.Fatalf("fake strict runner run record = %#v, want image %q", fields, wantImage)
			}
			runNames = append(runNames, fields[1])
			for _, prefix := range []string{
				"houfeng-rp-app-",
				"houfeng-rp-ledger-",
				"houfeng-rp-witness-",
				"houfeng-rp-recovery-",
			} {
				if strings.HasPrefix(fields[1], prefix) {
					if seenPrefixes[prefix] {
						t.Fatalf("fake strict runner repeated fixture prefix %q", prefix)
					}
					seenPrefixes[prefix] = true
				}
			}
		case "rm":
			if len(fields) != 2 {
				t.Fatalf("fake strict runner rm record = %#v", fields)
			}
			removeNames = append(removeNames, fields[1])
		}
	}
	if len(runNames) != 4 || len(removeNames) != 4 || len(seenPrefixes) != 4 {
		t.Fatalf("fake strict runner lifecycle = runs:%v removes:%v prefixes:%v, want four/four/all", runNames, removeNames, seenPrefixes)
	}
	for _, name := range runNames {
		found := false
		for _, removed := range removeNames {
			if removed == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("fake strict runner did not clean container %q", name)
		}
	}
	entries, err := os.ReadDir(fake.tmpParent)
	if err != nil {
		t.Fatalf("read cleaned strict runner TMPDIR: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("cleaned strict runner TMPDIR entries = %v, want none", entries)
	}
}

type appACLR2PostgresReservedInventoryFixture struct {
	setupSQL           []string
	cleanupSQL         []string
	wantInventoryCount int
}

func assertAppACLR2PostgresBootstrapACKRejectsReservedInventory(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	fixture appACLR2PostgresReservedInventoryFixture,
	wantObjects []AppACLR2ReservedCatalogObjectV1,
	wantExactL2 bool,
) {
	t.Helper()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, statement := range fixture.cleanupSQL {
			if _, err := db.Exec(cleanupCtx, statement); err != nil {
				t.Errorf("clean rejected APP ACL R2 reserved inventory fixture: %v", err)
			}
		}
	})
	for _, statement := range fixture.setupSQL {
		if _, err := db.Exec(ctx, statement); err != nil {
			t.Fatalf("create rejected APP ACL R2 reserved inventory fixture: %v", err)
		}
	}

	objects := readAppACLR2PostgresBootstrapReservedInventory(t, ctx, db)
	if len(objects) != fixture.wantInventoryCount {
		t.Fatalf("reserved inventory count = %d, want %d; objects=%#v", len(objects), fixture.wantInventoryCount, objects)
	}
	if !appACLR2ReservedObjectsContain(objects, wantObjects) {
		t.Fatalf("reserved inventory %#v does not contain exact targets %#v", objects, wantObjects)
	}
	if gotExactL2 := appACLR2ReservedObjectsContain(objects, appACLR2L2ReservedObjects()); gotExactL2 != wantExactL2 {
		t.Fatalf("reserved inventory exact-L2 presence = %t, want %t; objects=%#v", gotExactL2, wantExactL2, objects)
	}

	trace := &appACLR2PostgresQueryTrace{}
	outcome, err := observeAppACLR2PostgresBootstrapACK(ctx, db, trace)
	if err == nil || outcome != appACLR2BootstrapACKOutcomeNone {
		t.Fatalf("bootstrap ACK observer rejected inventory outcome/error = %v/%v, want none/non-nil", outcome, err)
	}
	if !strings.Contains(err.Error(), "inventory is not exact L2") {
		t.Fatalf("bootstrap ACK observer rejected inventory error = %v, want exact-L2 inventory rejection", err)
	}
	assertAppACLR2BootstrapACKReservedInventoryOnlyTrace(t, trace)
}

func readAppACLR2PostgresBootstrapReservedInventory(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
) []AppACLR2ReservedCatalogObjectV1 {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin APP ACL R2 reserved inventory evidence transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	objects, err := readAppACLR2BootstrapReservedCatalogObjectsInTx(ctx, tx)
	if err != nil {
		t.Fatalf("read APP ACL R2 reserved inventory evidence: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 reserved inventory evidence transaction: %v", err)
	}
	return objects
}

func openAppACLR2PostgresNamedPool(
	t *testing.T,
	ctx context.Context,
	source *pgxpool.Pool,
	applicationName string,
) *pgxpool.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.MaxConns = 1
	config.MinConns = 0
	runtimeParams := make(map[string]string, len(config.ConnConfig.RuntimeParams)+1)
	for key, value := range config.ConnConfig.RuntimeParams {
		runtimeParams[key] = value
	}
	runtimeParams["application_name"] = applicationName
	config.ConnConfig.RuntimeParams = runtimeParams
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open named APP ACL R2 PostgreSQL pool %q: %v", applicationName, err)
	}
	t.Cleanup(pool.Close)
	var gotApplicationName string
	if err := pool.QueryRow(ctx, `select pg_catalog.current_setting('application_name')`).Scan(&gotApplicationName); err != nil {
		t.Fatalf("read APP ACL R2 PostgreSQL application_name %q: %v", applicationName, err)
	}
	if gotApplicationName != applicationName {
		t.Fatalf("APP ACL R2 PostgreSQL application_name = %q, want %q", gotApplicationName, applicationName)
	}
	return pool
}

func openAppACLR2PostgresTracedPool(
	t *testing.T,
	ctx context.Context,
	source *pgxpool.Pool,
	trace *appACLR2PostgresQueryTrace,
) *pgxpool.Pool {
	t.Helper()
	if source == nil || trace == nil {
		t.Fatal("APP ACL R2 PostgreSQL traced pool requires a source and query trace")
	}
	config := source.Config().Copy()
	config.MaxConns = 1
	config.MinConns = 0
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open traced APP ACL R2 PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func waitForAppACLR2PostgresAdvisoryRace(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	runtimeApplicationName string,
	bootstrapApplicationName string,
	bootstrapDone <-chan error,
) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var sameLockConflict bool
		if err := db.QueryRow(ctx, `
			select exists (
				select 1
				from pg_catalog.pg_locks runtime_lock
				join pg_catalog.pg_stat_activity runtime_activity
				  on runtime_activity.pid = runtime_lock.pid
				join pg_catalog.pg_locks bootstrap_lock
				  on bootstrap_lock.locktype = runtime_lock.locktype
				 and bootstrap_lock.database is not distinct from runtime_lock.database
				 and bootstrap_lock.classid = runtime_lock.classid
				 and bootstrap_lock.objid = runtime_lock.objid
				 and bootstrap_lock.objsubid = runtime_lock.objsubid
				join pg_catalog.pg_stat_activity bootstrap_activity
				  on bootstrap_activity.pid = bootstrap_lock.pid
				where runtime_activity.datname = pg_catalog.current_database()
				  and bootstrap_activity.datname = pg_catalog.current_database()
				  and runtime_activity.application_name = $1
				  and bootstrap_activity.application_name = $2
				  and runtime_lock.locktype = 'advisory'
				  and runtime_lock.mode = 'ShareLock'
				  and runtime_lock.granted
				  and bootstrap_lock.mode = 'ExclusiveLock'
				  and not bootstrap_lock.granted
			)
		`, runtimeApplicationName, bootstrapApplicationName).Scan(&sameLockConflict); err != nil {
			t.Fatalf("observe APP ACL R2 PostgreSQL advisory-lock race: %v", err)
		}
		if sameLockConflict {
			return
		}
		select {
		case err := <-bootstrapDone:
			t.Fatalf("bootstrap ended before waiting on the runtime transition lock: %v", err)
		case <-ctx.Done():
			t.Fatalf("bootstrap did not wait on the runtime transition lock: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func assertAppACLR2PostgresAdvisoryRaceLocksReleased(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	runtimeApplicationName string,
	bootstrapApplicationName string,
) {
	t.Helper()
	var lockCount int
	if err := db.QueryRow(ctx, `
		select pg_catalog.count(*)::int
		from pg_catalog.pg_locks lock
		join pg_catalog.pg_stat_activity activity on activity.pid = lock.pid
		where activity.datname = pg_catalog.current_database()
		  and activity.application_name = any($1::text[])
		  and lock.locktype = 'advisory'
	`, []string{runtimeApplicationName, bootstrapApplicationName}).Scan(&lockCount); err != nil {
		t.Fatalf("read released APP ACL R2 PostgreSQL advisory locks: %v", err)
	}
	if lockCount != 0 {
		t.Fatalf("APP ACL R2 PostgreSQL advisory locks after race = %d, want 0", lockCount)
	}
}

func raiseAppACLR2PostgresSQLState(ctx context.Context, tx pgx.Tx, code string) error {
	var statement string
	switch code {
	case "40001":
		statement = `do $app_acl_r2_retry$
		begin
		  raise sqlstate '40001' using message = 'APP ACL R2 PostgreSQL serialization retry fixture';
		end;
		$app_acl_r2_retry$`
	case "40P01":
		statement = `do $app_acl_r2_retry$
		begin
		  raise sqlstate '40P01' using message = 'APP ACL R2 PostgreSQL deadlock retry fixture';
		end;
		$app_acl_r2_retry$`
	case "55P03":
		statement = `do $app_acl_r2_retry$
		begin
		  raise sqlstate '55P03' using message = 'APP ACL R2 PostgreSQL non-retryable fixture';
		end;
		$app_acl_r2_retry$`
	default:
		return fmt.Errorf("unsupported APP ACL R2 PostgreSQL fixture SQLSTATE %q", code)
	}
	_, err := tx.Exec(ctx, statement)
	if err == nil {
		return fmt.Errorf("APP ACL R2 PostgreSQL fixture SQLSTATE %s did not fail", code)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		return fmt.Errorf("APP ACL R2 PostgreSQL fixture error = %v, want SQLSTATE %s", err, code)
	}
	return err
}

func assertAppACLR2PostgresM2Absent(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var revisionsAbsent, headAbsent, helperAbsent bool
	if err := db.QueryRow(ctx, `
		select pg_catalog.to_regclass('public.app_acl_r2_manifest_revisions') is null,
		       pg_catalog.to_regclass('public.app_acl_r2_manifest_head') is null,
		       pg_catalog.to_regprocedure('record_platform_internal.app_acl_r2_reject_manifest_mutation()') is null
	`).Scan(&revisionsAbsent, &headAbsent, &helperAbsent); err != nil {
		t.Fatalf("read rolled-back APP ACL R2 M2 surface: %v", err)
	}
	if !revisionsAbsent || !headAbsent || !helperAbsent {
		t.Fatalf("rolled-back APP ACL R2 M2 absence = revisions:%t head:%t helper:%t, want true/true/true", revisionsAbsent, headAbsent, helperAbsent)
	}
}

func assertAppACLR2StockControlSystemACL(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var aclIsNull, publicExecute, bootstrapExecute bool
	if err := db.QueryRow(ctx, `
		select procedure.proacl is null,
		       exists (
			select 1
			from pg_catalog.aclexplode(pg_catalog.acldefault('f', procedure.proowner)) acl
			where acl.grantee = 0
			  and acl.privilege_type = 'EXECUTE'
		       ),
		       pg_catalog.has_function_privilege(current_user, procedure.oid, 'EXECUTE')
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		where namespace.nspname = 'pg_catalog'
		  and procedure.proname = 'pg_control_system'
		  and procedure.pronargs = 0
	`).Scan(&aclIsNull, &publicExecute, &bootstrapExecute); err != nil {
		t.Fatalf("read stock pg_control_system() ACL: %v", err)
	}
	if !aclIsNull || !publicExecute || !bootstrapExecute {
		t.Fatalf("stock pg_control_system() ACL = null:%t public:%t bootstrap:%t, want true/true/true RED baseline", aclIsNull, publicExecute, bootstrapExecute)
	}
}

func provisionAppACLR2ControlSystemACL(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "docs", "deploy", "app-acl-r2-pre-r1-provisioning.sql")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read APP ACL R2 pre-R1 provisioning SQL: %v", err)
	}
	const psqlDirective = "\\set ON_ERROR_STOP on\n"
	script := strings.ReplaceAll(string(payload), "\r\n", "\n")
	if !strings.HasPrefix(script, psqlDirective) {
		t.Fatal("APP ACL R2 pre-R1 provisioning SQL is missing the exact psql fail-stop directive")
	}
	script = strings.TrimPrefix(script, psqlDirective)
	if _, err := db.Exec(ctx, script); err != nil {
		t.Fatalf("execute APP ACL R2 pre-R1 provisioning SQL: %v", err)
	}
}

func assertAppACLR2ProvisionedControlSystemACL(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var functionCount, grantCount int
	var identityArguments string
	var ownerOID uint32
	var aclIsNull, bootstrapExecute bool
	if err := db.QueryRow(ctx, `
		with target as (
			select procedure.oid,
			       procedure.proowner,
			       procedure.proacl,
			       pg_catalog.pg_get_function_identity_arguments(procedure.oid) as identity_arguments
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			where namespace.nspname = 'pg_catalog'
			  and procedure.proname = 'pg_control_system'
			  and procedure.pronargs = 0
		)
		select pg_catalog.count(*)::int,
		       pg_catalog.min(target.proowner)::bigint,
		       pg_catalog.min(target.identity_arguments),
		       pg_catalog.bool_or(target.proacl is null),
		       coalesce(pg_catalog.sum((
			select pg_catalog.count(*)
			from pg_catalog.aclexplode(target.proacl) acl
			where acl.grantor = target.proowner
			  and acl.grantee = target.proowner
			  and acl.privilege_type = 'EXECUTE'
			  and not acl.is_grantable
		       )), 0::numeric)::int,
		       pg_catalog.bool_and(pg_catalog.has_function_privilege(current_user, target.oid, 'EXECUTE'))
		from target
	`).Scan(&functionCount, &ownerOID, &identityArguments, &aclIsNull, &grantCount, &bootstrapExecute); err != nil {
		t.Fatalf("read provisioned pg_control_system() ACL: %v", err)
	}
	if functionCount != 1 || ownerOID != 10 || identityArguments != appACLR2PGControlSystemIdentityArgumentsPG16 || aclIsNull || grantCount != 1 || !bootstrapExecute {
		t.Fatalf("provisioned pg_control_system() = functions:%d owner:%d identity:%q null:%t grants:%d bootstrap:%t, want exact owner-only PostgreSQL 16 ACL", functionCount, ownerOID, identityArguments, aclIsNull, grantCount, bootstrapExecute)
	}
	var extraGrantCount int
	if err := db.QueryRow(ctx, `
		select pg_catalog.count(*)::int
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		cross join lateral pg_catalog.aclexplode(procedure.proacl) acl
		where namespace.nspname = 'pg_catalog'
		  and procedure.proname = 'pg_control_system'
		  and procedure.pronargs = 0
		  and (acl.grantor <> procedure.proowner
		       or acl.grantee <> procedure.proowner
		       or acl.privilege_type <> 'EXECUTE'
		       or acl.is_grantable)
	`).Scan(&extraGrantCount); err != nil {
		t.Fatalf("read provisioned pg_control_system() extra ACL rows: %v", err)
	}
	if extraGrantCount != 0 {
		t.Fatalf("provisioned pg_control_system() extra ACL row count = %d, want 0", extraGrantCount)
	}
}

func requireAppACLR2PG16CatalogImage(t *testing.T) uint32 {
	t.Helper()
	versions := map[string]uint32{
		"postgres:16.0":  160000,
		"postgres:16.6":  160006,
		"postgres:16.12": 160012,
	}
	image := strings.TrimSpace(os.Getenv("HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE"))
	version, ok := versions[image]
	if !ok {
		t.Fatalf("HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE = %q, want an exact PG16 catalog image", image)
	}
	return version
}

func addAppACLR2PostgresUnrelatedRole(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) string {
	t.Helper()
	role := fmt.Sprintf("houfeng_r2_other_%d_%d", time.Now().UnixNano(), os.Getpid())
	password := appACLEffectiveCatalogTemporaryPassword(t)
	fixture.rolePasswords[role] = password
	if _, err := fixture.db.Exec(ctx, `create role `+quotePostgresIdentifier(role)+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+password+`'`); err != nil {
		t.Fatalf("create unrelated APP ACL R2 role %q: %v", role, err)
	}
	fixture.dropRole(t, role)
	return role
}

func assertAppACLR2PG16Catalog(t *testing.T, ctx context.Context, db *pgxpool.Pool, wantVersion uint32) {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin PG16 catalog transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	frozen, err := VerifyFrozenAppACLR1StateInTx(ctx, tx)
	if err != nil {
		t.Fatalf("VerifyFrozenAppACLR1StateInTx() error = %v", err)
	}
	snapshot, err := ReadAppACLR2BootstrapCatalogSnapshotInTx(ctx, tx, frozen)
	if err != nil {
		t.Fatalf("ReadAppACLR2BootstrapCatalogSnapshotInTx() error = %v", err)
	}
	if snapshot.ServerVersionNum != wantVersion || !appACLR2AllowedServerVersion(snapshot.ServerVersionNum) {
		t.Fatalf("server_version_num = %d, want allowed %d", snapshot.ServerVersionNum, wantVersion)
	}
	if snapshot.Extension.Name != "pgcrypto" || snapshot.Extension.Version != "1.3" || snapshot.Extension.Schema != "record_platform_internal" {
		t.Fatalf("pgcrypto extension = %#v, want record_platform_internal pgcrypto 1.3", snapshot.Extension)
	}
	if snapshot.Extension.OwnerName != frozen.DirectMigratorRole || snapshot.Extension.OwnerOID == 0 {
		t.Fatalf("pgcrypto owner = %q/%d, want direct migrator %q", snapshot.Extension.OwnerName, snapshot.Extension.OwnerOID, frozen.DirectMigratorRole)
	}
	if len(snapshot.Members) != 36 {
		t.Fatalf("pgcrypto member count = %d, want 36", len(snapshot.Members))
	}

	identities := make([]string, 0, len(snapshot.Members))
	for index, member := range snapshot.Members {
		identity := member.Schema + "." + member.Name + "|" + member.IdentityArguments
		identities = append(identities, identity)
		if member.ExtensionOID != snapshot.Extension.OID || member.ExtensionDependencyClass != "pg_catalog.pg_proc" ||
			member.ExtensionDependencyObjectSubID != 0 || member.ExtensionDependencyReferenceObjectSubID != 0 ||
			member.ExtensionDependencyType != "e" || member.ExtensionDependencyCount != 1 ||
			member.OwnerOID != 10 || member.RoutineKind != "f" || !member.ACLIsDefault {
			t.Fatalf("pgcrypto member %d catalog drift: %#v", index, member)
		}
	}
	wantIdentities := append([]string(nil), appACLR2PGCryptoIdentityContract[:]...)
	sort.Strings(identities)
	sort.Strings(wantIdentities)
	for index := range wantIdentities {
		if identities[index] != wantIdentities[index] {
			t.Fatalf("pgcrypto identity %d = %q, want %q", index, identities[index], wantIdentities[index])
		}
	}
	if got := fmt.Sprintf("%x", appACLR2PGCryptoIdentitySetDigest(identities)); got != appACLR2PGCryptoCatalogDigestPG16 {
		t.Fatalf("pgcrypto identity digest = %s, want %s", got, appACLR2PGCryptoCatalogDigestPG16)
	}
	if got := appACLR2PGCryptoIdentityContract[16]; got != "record_platform_internal.pgp_armor_headers|text, OUT key text, OUT value text" {
		t.Fatalf("pgcrypto contract member 16 = %q, want full pgp_armor_headers OUT signature", got)
	}
	member16Index := -1
	for index, member := range snapshot.Members {
		if member.Name == "pgp_armor_headers" {
			member16Index = index
			if member.IdentityArguments != "text, OUT key text, OUT value text" {
				t.Fatalf("pgcrypto pgp_armor_headers identity arguments = %q, want full OUT signature", member.IdentityArguments)
			}
		}
	}
	if member16Index < 0 {
		t.Fatal("pgcrypto catalog is missing pgp_armor_headers")
	}

	var liveSystemIdentifier string
	if err := tx.QueryRow(ctx, `select system_identifier::text from pg_catalog.pg_control_system()`).Scan(&liveSystemIdentifier); err != nil {
		t.Fatalf("read live PG16 system identifier: %v", err)
	}
	if snapshot.PostgresSystemIdentifier == "" || snapshot.PostgresSystemIdentifier != liveSystemIdentifier {
		t.Fatalf("bootstrap system identifier = %q, want live %q", snapshot.PostgresSystemIdentifier, liveSystemIdentifier)
	}
	bootstrapRoleFound := false
	for _, role := range snapshot.Roles {
		if role.ControlRole == AppACLControlRoleBootstrapSuperuserR2 {
			bootstrapRoleFound = role.OID == 10 && role.Superuser
		}
	}
	if !bootstrapRoleFound {
		t.Fatal("bootstrap catalog does not bind the direct OID-10 superuser")
	}
	if _, _, err := validateAppACLR2BootstrapCatalog(snapshot, frozen); err != nil {
		t.Fatalf("validateAppACLR2BootstrapCatalog() error = %v", err)
	}

	for name, identityArguments := range map[string]string{
		"input-only substitution":        "text",
		"result-shape substitution":      "text, OUT value text, OUT key text",
		"equal-cardinality substitution": "text, OUT key text, OUT changed text",
	} {
		t.Run(name, func(t *testing.T) {
			mutated := snapshot
			mutated.Members = append([]AppACLR2PGCryptoMemberCatalogV1(nil), snapshot.Members...)
			mutated.Members[member16Index].IdentityArguments = identityArguments
			if _, _, err := validateAppACLR2BootstrapCatalog(mutated, frozen); err == nil {
				t.Fatalf("validateAppACLR2BootstrapCatalog() accepted member 16 %q", identityArguments)
			}
		})
	}
}

func assertAppACLR2NoControlSystemAuthority(t *testing.T, ctx context.Context, db *pgxpool.Pool, role string) {
	t.Helper()
	var effectiveExecute, directExecute, directMonitorMembership, effectiveMonitorMembership bool
	var functionIdentity, functionACL string
	if err := db.QueryRow(ctx, `
		with target_role as (
			select oid from pg_catalog.pg_roles where rolname = $1
		), target_function as (
			select procedure.oid,
			       procedure.proowner,
			       procedure.proacl,
			       pg_catalog.pg_get_function_identity_arguments(procedure.oid) as identity_arguments
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			where namespace.nspname = 'pg_catalog'
			  and procedure.proname = 'pg_control_system'
			  and procedure.pronargs = 0
		), monitor_role as (
			select oid from pg_catalog.pg_roles where rolname = 'pg_monitor'
		)
		select pg_catalog.has_function_privilege(target_role.oid, target_function.oid, 'EXECUTE'),
		       exists (
			select 1
			from pg_catalog.aclexplode(coalesce(target_function.proacl, pg_catalog.acldefault('f', target_function.proowner))) acl
			where acl.grantee = target_role.oid and acl.privilege_type = 'EXECUTE'
		       ),
		       exists (
			select 1 from pg_catalog.pg_auth_members membership
			where membership.member = target_role.oid and membership.roleid = monitor_role.oid
		       ),
		       pg_catalog.pg_has_role(target_role.oid, monitor_role.oid, 'MEMBER')
		       , target_function.identity_arguments
		       , coalesce(target_function.proacl::text, '<NULL>')
		from target_role, target_function, monitor_role
	`, role).Scan(
		&effectiveExecute,
		&directExecute,
		&directMonitorMembership,
		&effectiveMonitorMembership,
		&functionIdentity,
		&functionACL,
	); err != nil {
		t.Fatalf("read %q pg_control_system authority: %v", role, err)
	}
	var ignoredSystemIdentifier string
	callErr := db.QueryRow(ctx, `select system_identifier::text from pg_catalog.pg_control_system()`).Scan(&ignoredSystemIdentifier)
	if callErr == nil {
		t.Fatalf("role %q invoked pg_control_system() with identity %q ACL %q", role, functionIdentity, functionACL)
	}
	requirePostgresSQLState(t, callErr, "42501")
	if effectiveExecute || directExecute || directMonitorMembership || effectiveMonitorMembership {
		t.Fatalf("role %q pg_control_system authority = effective:%t direct:%t pg_monitor_direct:%t pg_monitor_effective:%t identity:%q ACL:%q, want all false", role, effectiveExecute, directExecute, directMonitorMembership, effectiveMonitorMembership, functionIdentity, functionACL)
	}
}

func assertAppACLR2NativeTablePrivilege(t *testing.T, ctx context.Context, db *pgxpool.Pool, relation, privilege string, want bool) {
	t.Helper()
	schema, name, ok := strings.Cut(relation, ".")
	if !ok || schema == "" || name == "" {
		t.Fatalf("invalid qualified relation %q", relation)
	}
	var got bool
	if err := db.QueryRow(ctx, `
		select pg_catalog.has_table_privilege(current_user, relation.oid, $3)
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = $1 and relation.relname = $2
	`, schema, name, privilege).Scan(&got); err != nil {
		t.Fatalf("read native %s on %s: %v", privilege, relation, err)
	}
	if got != want {
		t.Fatalf("native %s on %s = %t, want %t", privilege, relation, got, want)
	}
}

func assertAppACLR2NativeTablePrivilegeVector(t *testing.T, ctx context.Context, db *pgxpool.Pool, relation string, selectOnly bool) {
	t.Helper()
	for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
		assertAppACLR2NativeTablePrivilege(t, ctx, db, relation, privilege, selectOnly && privilege == "SELECT")
	}
}

func assertAppACLR2M2RawTableACL(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	relation string,
	directMigratorRole string,
	runtimeRole string,
) {
	t.Helper()
	schema, name, ok := strings.Cut(relation, ".")
	if !ok || schema == "" || name == "" {
		t.Fatalf("invalid qualified relation %q", relation)
	}
	var relationCount, grantCount, ownerSelectCount, runtimeSelectCount int
	if err := db.QueryRow(ctx, `
		select pg_catalog.count(distinct relation.oid)::int,
		       pg_catalog.count(acl_grant.grantor)::int,
		       pg_catalog.count(acl_grant.grantor) filter (
			where owner_role.rolname = $3
			  and acl_grant.grantor = relation.relowner
			  and acl_grant.grantee = relation.relowner
			  and acl_grant.privilege_type = 'SELECT'
			  and not acl_grant.is_grantable
		       )::int,
		       pg_catalog.count(acl_grant.grantor) filter (
			where owner_role.rolname = $3
			  and grantee_role.rolname = $4
			  and acl_grant.grantor = relation.relowner
			  and acl_grant.privilege_type = 'SELECT'
			  and not acl_grant.is_grantable
		       )::int
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		left join lateral pg_catalog.aclexplode(relation.relacl) acl_grant on true
		left join pg_catalog.pg_roles owner_role on owner_role.oid = relation.relowner
		left join pg_catalog.pg_roles grantee_role on grantee_role.oid = acl_grant.grantee
		where namespace.nspname = $1 and relation.relname = $2
	`, schema, name, directMigratorRole, runtimeRole).Scan(
		&relationCount,
		&grantCount,
		&ownerSelectCount,
		&runtimeSelectCount,
	); err != nil {
		t.Fatalf("read raw M2 table ACL on %s: %v", relation, err)
	}
	if relationCount != 1 || grantCount != 2 || ownerSelectCount != 1 || runtimeSelectCount != 1 {
		t.Fatalf("raw M2 table ACL on %s = relations:%d grants:%d owner-select:%d runtime-select:%d, want 1/2/1/1 exact non-grantable rows", relation, relationCount, grantCount, ownerSelectCount, runtimeSelectCount)
	}
}

func assertAppACLR2M2RawHelperACL(t *testing.T, ctx context.Context, db *pgxpool.Pool, directMigratorRole string) {
	t.Helper()
	var functionCount, grantCount, ownerExecuteCount int
	if err := db.QueryRow(ctx, `
		select pg_catalog.count(distinct procedure.oid)::int,
		       pg_catalog.count(acl_grant.grantor)::int,
		       pg_catalog.count(acl_grant.grantor) filter (
			where owner_role.rolname = $1
			  and acl_grant.grantor = procedure.proowner
			  and acl_grant.grantee = procedure.proowner
			  and acl_grant.privilege_type = 'EXECUTE'
			  and not acl_grant.is_grantable
		       )::int
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		left join lateral pg_catalog.aclexplode(procedure.proacl) acl_grant on true
		left join pg_catalog.pg_roles owner_role on owner_role.oid = procedure.proowner
		where namespace.nspname = 'record_platform_internal'
		  and procedure.proname = 'app_acl_r2_reject_manifest_mutation'
		  and procedure.prokind = 'f'
		  and pg_catalog.pg_get_function_identity_arguments(procedure.oid) = ''
	`, directMigratorRole).Scan(&functionCount, &grantCount, &ownerExecuteCount); err != nil {
		t.Fatalf("read raw M2 helper ACL: %v", err)
	}
	if functionCount != 1 || grantCount != 1 || ownerExecuteCount != 1 {
		t.Fatalf("raw M2 helper ACL = functions:%d grants:%d owner-execute:%d, want 1/1/1 exact non-grantable row", functionCount, grantCount, ownerExecuteCount)
	}
}

func assertAppACLR2M2ExactSelfACLEvidence(
	t *testing.T,
	ctx context.Context,
	migrator *pgxpool.Pool,
	runtime *pgxpool.Pool,
	directMigratorRole string,
	runtimeRole string,
) {
	t.Helper()
	for _, relation := range []string{
		"public.app_acl_r2_manifest_revisions",
		"public.app_acl_r2_manifest_head",
	} {
		assertAppACLR2M2RawTableACL(t, ctx, migrator, relation, directMigratorRole, runtimeRole)
		assertAppACLR2NativeTablePrivilegeVector(t, ctx, migrator, relation, true)
		assertAppACLR2NativeTablePrivilegeVector(t, ctx, runtime, relation, true)
	}
	assertAppACLR2M2RawHelperACL(t, ctx, migrator, directMigratorRole)
	assertAppACLR2NativeM2HelperExecute(t, ctx, migrator, true)
	assertAppACLR2NativeM2HelperExecute(t, ctx, runtime, false)
}

func assertAppACLR2NativeM2HelperExecute(t *testing.T, ctx context.Context, db *pgxpool.Pool, want bool) {
	t.Helper()
	var got bool
	if err := db.QueryRow(ctx, `
		select pg_catalog.has_function_privilege(current_user, procedure.oid, 'EXECUTE')
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		where namespace.nspname = 'record_platform_internal'
		  and procedure.proname = 'app_acl_r2_reject_manifest_mutation'
		  and procedure.prokind = 'f'
		  and pg_catalog.pg_get_function_identity_arguments(procedure.oid) = ''
	`).Scan(&got); err != nil {
		t.Fatalf("read native EXECUTE on record_platform_internal.app_acl_r2_reject_manifest_mutation(): %v", err)
	}
	if got != want {
		t.Fatalf("native EXECUTE on record_platform_internal.app_acl_r2_reject_manifest_mutation() = %t, want %t", got, want)
	}
}

func assertAppACLR2M2RevokedOwnerTableSelfSelect(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	relation string,
	directMigratorRole string,
	runtimeRole string,
) {
	t.Helper()
	schema, name, ok := strings.Cut(relation, ".")
	if !ok || schema == "" || name == "" {
		t.Fatalf("invalid qualified relation %q", relation)
	}
	var ownerIsDirectMigrator, ownerHasSelect bool
	var grantCount, ownerSelectCount, runtimeSelectCount int
	if err := db.QueryRow(ctx, `
		select owner_role.rolname = $3,
		       pg_catalog.has_table_privilege(relation.relowner, relation.oid, 'SELECT'),
		       (select pg_catalog.count(*)::int from pg_catalog.aclexplode(relation.relacl)),
		       (select pg_catalog.count(*)::int
			from pg_catalog.aclexplode(relation.relacl) acl_grant
			where acl_grant.grantor = relation.relowner
			  and acl_grant.grantee = relation.relowner
			  and acl_grant.privilege_type = 'SELECT'
			  and not acl_grant.is_grantable),
		       (select pg_catalog.count(*)::int
			from pg_catalog.aclexplode(relation.relacl) acl_grant
			join pg_catalog.pg_roles grantee_role on grantee_role.oid = acl_grant.grantee
			where acl_grant.grantor = relation.relowner
			  and grantee_role.rolname = $4
			  and acl_grant.privilege_type = 'SELECT'
			  and not acl_grant.is_grantable)
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		join pg_catalog.pg_roles owner_role on owner_role.oid = relation.relowner
		where namespace.nspname = $1 and relation.relname = $2
	`, schema, name, directMigratorRole, runtimeRole).Scan(
		&ownerIsDirectMigrator,
		&ownerHasSelect,
		&grantCount,
		&ownerSelectCount,
		&runtimeSelectCount,
	); err != nil {
		t.Fatalf("read revoked M2 owner self SELECT on %s: %v", relation, err)
	}
	if !ownerIsDirectMigrator || ownerHasSelect || grantCount != 1 || ownerSelectCount != 0 || runtimeSelectCount != 1 {
		t.Fatalf("revoked M2 owner self SELECT on %s = owner:%t effective:%t grants:%d owner-select:%d runtime-select:%d, want true/false/1/0/1", relation, ownerIsDirectMigrator, ownerHasSelect, grantCount, ownerSelectCount, runtimeSelectCount)
	}
}

func assertAppACLR2M2RevokedOwnerHelperSelfExecute(t *testing.T, ctx context.Context, db *pgxpool.Pool, directMigratorRole string) {
	t.Helper()
	var ownerIsDirectMigrator, ownerHasExecute bool
	var grantCount, ownerExecuteCount int
	if err := db.QueryRow(ctx, `
		select owner_role.rolname = $1,
		       pg_catalog.has_function_privilege(procedure.proowner, procedure.oid, 'EXECUTE'),
		       (select pg_catalog.count(*)::int from pg_catalog.aclexplode(procedure.proacl)),
		       (select pg_catalog.count(*)::int
			from pg_catalog.aclexplode(procedure.proacl) acl_grant
			where acl_grant.grantor = procedure.proowner
			  and acl_grant.grantee = procedure.proowner
			  and acl_grant.privilege_type = 'EXECUTE'
			  and not acl_grant.is_grantable)
		from pg_catalog.pg_proc procedure
		join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
		join pg_catalog.pg_roles owner_role on owner_role.oid = procedure.proowner
		where namespace.nspname = 'record_platform_internal'
		  and procedure.proname = 'app_acl_r2_reject_manifest_mutation'
		  and procedure.prokind = 'f'
		  and pg_catalog.pg_get_function_identity_arguments(procedure.oid) = ''
	`, directMigratorRole).Scan(&ownerIsDirectMigrator, &ownerHasExecute, &grantCount, &ownerExecuteCount); err != nil {
		t.Fatalf("read revoked M2 owner self EXECUTE: %v", err)
	}
	if !ownerIsDirectMigrator || ownerHasExecute || grantCount != 0 || ownerExecuteCount != 0 {
		t.Fatalf("revoked M2 owner self EXECUTE = owner:%t effective:%t grants:%d owner-execute:%d, want true/false/0/0", ownerIsDirectMigrator, ownerHasExecute, grantCount, ownerExecuteCount)
	}
}

func assertAppACLR2FinalizeACKRecoveryRejects(t *testing.T, ctx context.Context, db *pgxpool.Pool, wantSQLState string) {
	t.Helper()
	begin := newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(db))
	state, err := recoverAppACLR2FinalizeACKWithDependencies(ctx, begin, defaultAppACLR2FinalizeACKDependencies())
	if err == nil || state != AppACLR2StateCorrupt {
		t.Fatalf("finalizer ACK recovery = %v, %v, want error-only CORRUPT", state, err)
	}
	if wantSQLState != "" {
		requirePostgresSQLState(t, err, wantSQLState)
	}
}

func readAppACLR2PostgresReceipt(t *testing.T, ctx context.Context, db *pgxpool.Pool) (string, string) {
	t.Helper()
	var body, digest []byte
	if err := db.QueryRow(ctx, `
		select receipt_body, receipt_digest
		from public.app_acl_r2_bootstrap_receipt
		where singleton
	`).Scan(&body, &digest); err != nil {
		t.Fatalf("read APP ACL R2 bootstrap receipt: %v", err)
	}
	return string(body), string(digest)
}

func assertAppACLR2PostgresReceipt(t *testing.T, ctx context.Context, db *pgxpool.Pool, wantBody, wantDigest string) {
	t.Helper()
	body, digest := readAppACLR2PostgresReceipt(t, ctx, db)
	if body != wantBody || digest != wantDigest {
		t.Fatalf("APP ACL R2 bootstrap receipt changed: body-bytes=%d want=%d digest=%x want=%x", len(body), len(wantBody), digest, wantDigest)
	}
}

func assertAppACLR2PostgresReceiptLiveDatabaseBinding(t *testing.T, ctx context.Context, db *pgxpool.Pool, receiptBody string) {
	t.Helper()
	receipt, err := ParseCanonicalAppACLR2BootstrapReceiptBodyV1([]byte(receiptBody))
	if err != nil {
		t.Fatalf("parse APP ACL R2 bootstrap receipt for live database binding: %v", err)
	}
	domain, err := ParseCanonicalAppACLDomainR2BodyV1(receipt.DomainBody)
	if err != nil {
		t.Fatalf("parse APP ACL R2 domain for live database binding: %v", err)
	}
	var freshDatabaseOID int64
	var freshDatabaseName string
	if err := db.QueryRow(ctx, `
		select database.oid::bigint,
		       pg_catalog.current_database()
		from pg_catalog.pg_database database
		where database.datname = pg_catalog.current_database()
	`).Scan(&freshDatabaseOID, &freshDatabaseName); err != nil {
		t.Fatalf("read fresh APP ACL R2 database identity: %v", err)
	}
	if freshDatabaseOID != int64(domain.DatabaseOID) || freshDatabaseName != domain.DatabaseName {
		t.Fatalf("APP ACL R2 receipt database binding = %d/%q, want fresh %d/%q", domain.DatabaseOID, domain.DatabaseName, freshDatabaseOID, freshDatabaseName)
	}
}

func readAppACLR2PostgresPredicates(t *testing.T, ctx context.Context, db *pgxpool.Pool) AppACLR2CatalogPredicates {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin APP ACL R2 predicate evidence transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	predicates, err := ReadAppACLR2CatalogPredicatesInTx(ctx, tx)
	if err != nil {
		t.Fatalf("ReadAppACLR2CatalogPredicatesInTx() error = %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 predicate evidence transaction: %v", err)
	}
	return predicates
}

func assertAppACLR2PostgresExactPreparedPredicates(t *testing.T, predicates AppACLR2CatalogPredicates) {
	t.Helper()
	if !predicates.ExactPrepared() || !predicates.ExactL1M1 || !predicates.ExactL2 || predicates.L2Absent ||
		!predicates.M2Absent || predicates.ExactM2 || predicates.HasUnknownReservedObjects {
		t.Fatalf("APP ACL R2 PREPARED predicates = L1M1:%t L2:%t L2Absent:%t M2:%t M2Absent:%t unknown:%t, want exact PREPARED",
			predicates.ExactL1M1,
			predicates.ExactL2,
			predicates.L2Absent,
			predicates.ExactM2,
			predicates.M2Absent,
			predicates.HasUnknownReservedObjects,
		)
	}
}

type appACLR2PostgresContinuityBaseline struct {
	SourceChecksum     string
	Domains            []AppACLDomainR2V1
	M1RevisionEvidence string
	M1HeadEvidence     string
	ReservedObjects    []AppACLR2ReservedCatalogObjectV1
	ReceiptRows        []appACLR2ReceiptRowV1
	Predicates         AppACLR2CatalogPredicates
	M2Revisions        []appACLR2ManifestRowV1
	M2Heads            []appACLR2ManifestHeadRowV1
}

type appACLR2PostgresPGCryptoEvidence struct {
	Extension AppACLR2PGCryptoExtensionCatalogV1
	Members   []AppACLR2PGCryptoMemberCatalogV1
}

func readAppACLR2PostgresFrozenR1State(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
) FrozenAppACLR1StateV1 {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin frozen APP ACL R1 evidence transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := VerifyFrozenAppACLR1StateInTx(ctx, tx)
	if err != nil {
		t.Fatalf("verify frozen APP ACL R1 evidence: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit frozen APP ACL R1 evidence transaction: %v", err)
	}
	return state
}

func readAppACLR2PostgresReceiptCatalog(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	frozen FrozenAppACLR1StateV1,
) AppACLR2ReceiptCatalogSnapshotV1 {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin APP ACL R2 receipt catalog evidence transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	surface, err := ReadAppACLR2ReceiptCatalogSnapshotInTx(ctx, tx, frozen)
	if err != nil {
		t.Fatalf("read APP ACL R2 receipt catalog evidence: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 receipt catalog evidence transaction: %v", err)
	}
	return surface
}

func readAppACLR2PostgresTransitionRoleEvidence(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	frozen FrozenAppACLR1StateV1,
) []AppACLR2CatalogRoleStateV1 {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin APP ACL R2 transition-role evidence transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	roles, err := readAppACLR2TransitionRolesInTx(ctx, tx, frozen)
	if err != nil {
		t.Fatalf("read APP ACL R2 transition-role evidence: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 transition-role evidence transaction: %v", err)
	}
	return roles
}

func readAppACLR2PostgresPGCryptoEvidence(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
) appACLR2PostgresPGCryptoEvidence {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin APP ACL R2 pgcrypto evidence transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	extension, err := readAppACLR2PGCryptoExtensionInTx(ctx, tx)
	if err != nil {
		t.Fatalf("read APP ACL R2 pgcrypto extension evidence: %v", err)
	}
	members, err := readAppACLR2PGCryptoMembersInTx(ctx, tx)
	if err != nil {
		t.Fatalf("read APP ACL R2 pgcrypto member evidence: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 pgcrypto evidence transaction: %v", err)
	}
	return appACLR2PostgresPGCryptoEvidence{Extension: extension, Members: members}
}

func (baseline appACLR2PostgresContinuityBaseline) singleDomain(t *testing.T) AppACLDomainR2V1 {
	t.Helper()
	if len(baseline.Domains) != 1 {
		t.Fatalf("APP ACL R2 continuity baseline domains = %#v, want one", baseline.Domains)
	}
	return baseline.Domains[0]
}

func (baseline appACLR2PostgresContinuityBaseline) singleReceipt(t *testing.T) appACLR2ReceiptRowV1 {
	t.Helper()
	if len(baseline.ReceiptRows) != 1 || !baseline.ReceiptRows[0].Singleton {
		t.Fatalf("APP ACL R2 continuity baseline receipts = %#v, want one true singleton", baseline.ReceiptRows)
	}
	return baseline.ReceiptRows[0]
}

func (baseline appACLR2PostgresContinuityBaseline) withoutPredicates() appACLR2PostgresContinuityBaseline {
	baseline.Predicates = AppACLR2CatalogPredicates{}
	return baseline
}

func readAppACLR2PostgresContinuityBaseline(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
) appACLR2PostgresContinuityBaseline {
	t.Helper()
	return readAppACLR2PostgresContinuityEvidence(t, ctx, db, true)
}

func readAppACLR2PostgresContinuityEvidence(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	includePredicates bool,
) appACLR2PostgresContinuityBaseline {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin APP ACL R2 continuity baseline transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var baseline appACLR2PostgresContinuityBaseline
	if err := tx.QueryRow(ctx, `
		select checksum
		from public.schema_migrations
		where name = '0051_create_record_platform_foundation.sql'
	`).Scan(&baseline.SourceChecksum); err != nil {
		t.Fatalf("read APP ACL R2 continuity source checksum: %v", err)
	}
	baseline.Domains, err = readAppACLR2DomainsInTx(ctx, tx)
	if err != nil {
		t.Fatalf("read APP ACL R2 continuity domains: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		select pg_catalog.row_to_json(revision)::text
		from public.app_acl_manifest_revisions revision
		where revision.manifest_revision = 1
	`).Scan(&baseline.M1RevisionEvidence); err != nil {
		t.Fatalf("read APP ACL R2 continuity M1 revision evidence: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		select pg_catalog.row_to_json(head)::text
		from public.app_acl_manifest_head head
		where head.singleton
	`).Scan(&baseline.M1HeadEvidence); err != nil {
		t.Fatalf("read APP ACL R2 continuity M1 head evidence: %v", err)
	}
	baseline.ReservedObjects, err = readAppACLR2ReservedCatalogObjectsInTx(ctx, tx)
	if err != nil {
		t.Fatalf("read APP ACL R2 continuity reserved objects: %v", err)
	}
	baseline.ReceiptRows, err = readAppACLR2ReceiptRowsInTx(ctx, tx)
	if err != nil {
		t.Fatalf("read APP ACL R2 continuity receipt rows: %v", err)
	}
	var revisionsExist, headExists bool
	if err := tx.QueryRow(ctx, `
		select pg_catalog.to_regclass('public.app_acl_r2_manifest_revisions') is not null,
		       pg_catalog.to_regclass('public.app_acl_r2_manifest_head') is not null
	`).Scan(&revisionsExist, &headExists); err != nil {
		t.Fatalf("read APP ACL R2 continuity M2 presence: %v", err)
	}
	if revisionsExist != headExists {
		t.Fatalf("APP ACL R2 continuity M2 presence = revisions:%t head:%t, want paired", revisionsExist, headExists)
	}
	if revisionsExist {
		baseline.M2Revisions, err = readAppACLR2ManifestRowsInTx(ctx, tx)
		if err != nil {
			t.Fatalf("read APP ACL R2 continuity M2 revisions: %v", err)
		}
		baseline.M2Heads, err = readAppACLR2ManifestHeadRowsInTx(ctx, tx)
		if err != nil {
			t.Fatalf("read APP ACL R2 continuity M2 heads: %v", err)
		}
	}
	if includePredicates {
		baseline.Predicates, err = ReadAppACLR2CatalogPredicatesInTx(ctx, tx)
		if err != nil {
			t.Fatalf("read APP ACL R2 continuity predicates: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 continuity baseline transaction: %v", err)
	}
	return baseline
}

func rewriteAppACLR2PostgresDomainDatabaseIdentity(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	databaseOID uint32,
	databaseName string,
) {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin APP ACL R2 domain identity rewrite: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `alter table public.record_platform_domain_identity disable trigger rp_domain_identity_immutable`); err != nil {
		t.Fatalf("disable APP ACL R2 domain identity immutable trigger: %v", err)
	}
	tag, err := tx.Exec(ctx, `
		update public.record_platform_domain_identity
		set database_oid = $1::oid,
		    database_name = $2
		where domain_kind = 'application'
	`, int64(databaseOID), databaseName)
	if err != nil {
		t.Fatalf("rewrite APP ACL R2 domain database identity: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("rewrite APP ACL R2 domain database identity affected %d rows, want 1", tag.RowsAffected())
	}
	if _, err := tx.Exec(ctx, `alter table public.record_platform_domain_identity enable trigger rp_domain_identity_immutable`); err != nil {
		t.Fatalf("enable APP ACL R2 domain identity immutable trigger: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 domain identity rewrite: %v", err)
	}
}

func rewriteAppACLR2PostgresSourceChecksum(t *testing.T, ctx context.Context, db *pgxpool.Pool, checksum string) {
	t.Helper()
	tag, err := db.Exec(ctx, `
		update public.schema_migrations
		set checksum = $1
		where name = '0051_create_record_platform_foundation.sql'
	`, checksum)
	if err != nil {
		t.Fatalf("rewrite APP ACL R2 application source checksum: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("rewrite APP ACL R2 application source checksum affected %d rows, want 1", tag.RowsAffected())
	}
}

func rewriteAppACLR2PostgresReceipt(t *testing.T, ctx context.Context, db *pgxpool.Pool, body []byte) {
	t.Helper()
	digest := sha256.Sum256(body)
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin APP ACL R2 receipt rewrite: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `alter table public.app_acl_r2_bootstrap_receipt disable trigger app_acl_r2_bootstrap_receipt_immutable`); err != nil {
		t.Fatalf("disable APP ACL R2 receipt immutable trigger: %v", err)
	}
	tag, err := tx.Exec(ctx, `
		update public.app_acl_r2_bootstrap_receipt
		set receipt_body = $1,
		    receipt_digest = $2
		where singleton
	`, body, digest[:])
	if err != nil {
		t.Fatalf("rewrite APP ACL R2 receipt: %v", err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("rewrite APP ACL R2 receipt affected %d rows, want 1", tag.RowsAffected())
	}
	if _, err := tx.Exec(ctx, `alter table public.app_acl_r2_bootstrap_receipt enable trigger app_acl_r2_bootstrap_receipt_immutable`); err != nil {
		t.Fatalf("enable APP ACL R2 receipt immutable trigger: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 receipt rewrite: %v", err)
	}
}

func rewriteAppACLR2PostgresM2DomainEvidence(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	revision appACLR2ManifestRowV1,
) {
	t.Helper()
	relations := []string{
		"public.app_acl_r2_manifest_revisions",
		"public.app_acl_r2_manifest_head",
	}
	for _, relation := range relations {
		assertAppACLR2NativeTablePrivilegeVector(t, ctx, db, relation, true)
	}
	manifestBody, err := CanonicalAppACLManifestR2BodyV1(revision.Manifest)
	if err != nil {
		t.Fatalf("validate APP ACL R2 M2 domain-evidence rewrite: %v", err)
	}
	if sha256.Sum256(manifestBody) != revision.ManifestDigest {
		t.Fatal("APP ACL R2 M2 domain-evidence rewrite digest is not canonical")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin APP ACL R2 M2 domain-evidence rewrite: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sessionUser, currentUser string
	var roleOID int64
	var superuser bool
	var relationCount, ownedRelationCount int
	if err := tx.QueryRow(ctx, `
		select session_user::text,
		       current_user::text,
		       role.oid::bigint,
		       role.rolsuper,
		       (
			select pg_catalog.count(*)::int
			from pg_catalog.pg_class relation
			join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
			where namespace.nspname = 'public'
			  and relation.relname = any($1::name[])
		       ),
		       (
			select pg_catalog.count(*)::int
			from pg_catalog.pg_class relation
			join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
			where namespace.nspname = 'public'
			  and relation.relname = any($1::name[])
			  and relation.relowner = role.oid
		       )
		from pg_catalog.pg_roles role
		where role.rolname = current_user
	`, []string{"app_acl_r2_manifest_revisions", "app_acl_r2_manifest_head"}).Scan(
		&sessionUser,
		&currentUser,
		&roleOID,
		&superuser,
		&relationCount,
		&ownedRelationCount,
	); err != nil {
		t.Fatalf("read APP ACL R2 M2 domain-evidence rewrite identity: %v", err)
	}
	if sessionUser != revision.Manifest.DirectMigratorName || currentUser != revision.Manifest.DirectMigratorName ||
		roleOID != int64(revision.Manifest.DirectMigratorOID) || superuser || relationCount != 2 || ownedRelationCount != 2 {
		t.Fatalf("APP ACL R2 M2 domain-evidence rewrite identity = %q/%q oid=%d superuser=%t relations=%d owned=%d, want direct owner %q/%q oid=%d superuser=false relations=2 owned=2",
			sessionUser, currentUser, roleOID, superuser, relationCount, ownedRelationCount,
			revision.Manifest.DirectMigratorName, revision.Manifest.DirectMigratorName, revision.Manifest.DirectMigratorOID,
		)
	}
	quotedRole := quotePostgresIdentifier(currentUser)
	temporaryDML := "grant insert, update, delete on table public.app_acl_r2_manifest_revisions, public.app_acl_r2_manifest_head to " + quotedRole
	if _, err := tx.Exec(ctx, temporaryDML); err != nil {
		t.Fatalf("grant temporary APP ACL R2 M2 owner DML for domain-evidence rewrite: %v", err)
	}
	for _, statement := range []string{
		`alter table public.app_acl_r2_manifest_head disable trigger app_acl_r2_manifest_head_immutable`,
		`alter table public.app_acl_r2_manifest_revisions disable trigger app_acl_r2_manifest_revisions_immutable`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("disable APP ACL R2 M2 immutable trigger: %v", err)
		}
	}
	if tag, err := tx.Exec(ctx, `delete from public.app_acl_r2_manifest_head where singleton`); err != nil {
		t.Fatalf("delete APP ACL R2 M2 head for domain-evidence rewrite: %v", err)
	} else if tag.RowsAffected() != 1 {
		t.Fatalf("delete APP ACL R2 M2 head for domain-evidence rewrite affected %d rows, want 1", tag.RowsAffected())
	}
	if tag, err := tx.Exec(ctx, `
		update public.app_acl_r2_manifest_revisions
		set domain_body = $1,
		    domain_digest = $2,
		    manifest_digest = $3
		where protocol_version = 2 and manifest_revision = 2
	`, revision.Manifest.DomainBody, revision.Manifest.DomainDigest[:], revision.ManifestDigest[:]); err != nil {
		t.Fatalf("rewrite APP ACL R2 M2 domain evidence: %v", err)
	} else if tag.RowsAffected() != 1 {
		t.Fatalf("rewrite APP ACL R2 M2 domain evidence affected %d rows, want 1", tag.RowsAffected())
	}
	if tag, err := tx.Exec(ctx, `
		insert into public.app_acl_r2_manifest_head (
			singleton,
			protocol_version,
			manifest_revision,
			manifest_digest
		) values (true, 2, 2, $1)
	`, revision.ManifestDigest[:]); err != nil {
		t.Fatalf("restore APP ACL R2 M2 head after domain-evidence rewrite: %v", err)
	} else if tag.RowsAffected() != 1 {
		t.Fatalf("restore APP ACL R2 M2 head after domain-evidence rewrite affected %d rows, want 1", tag.RowsAffected())
	}
	for _, statement := range []string{
		`alter table public.app_acl_r2_manifest_revisions enable trigger app_acl_r2_manifest_revisions_immutable`,
		`alter table public.app_acl_r2_manifest_head enable trigger app_acl_r2_manifest_head_immutable`,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			t.Fatalf("enable APP ACL R2 M2 immutable trigger: %v", err)
		}
	}
	temporaryDML = "revoke insert, update, delete on table public.app_acl_r2_manifest_revisions, public.app_acl_r2_manifest_head from " + quotedRole
	if _, err := tx.Exec(ctx, temporaryDML); err != nil {
		t.Fatalf("revoke temporary APP ACL R2 M2 owner DML after domain-evidence rewrite: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 M2 domain-evidence rewrite: %v", err)
	}
	for _, relation := range relations {
		assertAppACLR2NativeTablePrivilegeVector(t, ctx, db, relation, true)
	}
}

func readAppACLR2PostgresM2ControlACL(t *testing.T, ctx context.Context, db *pgxpool.Pool) AppACLControlACLBodyR2V1 {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin APP ACL R2 M2 control-ACL read: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	frozen, err := VerifyFrozenAppACLR1StateInTx(ctx, tx)
	if err != nil {
		t.Fatalf("verify frozen APP ACL R1 for M2 control-ACL read: %v", err)
	}
	controlACL, err := readAppACLR2M2ControlACLInTx(ctx, tx, frozen)
	if err != nil {
		t.Fatalf("read APP ACL R2 M2 control ACL: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit APP ACL R2 M2 control-ACL read: %v", err)
	}
	return controlACL
}

func assertAppACLR2PostgresContinuityDriftRejected(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	trace := &appACLR2PostgresQueryTrace{}
	traced := openAppACLR2PostgresTracedPool(t, ctx, db, trace)
	state, err := classifyAppACLR2PostgresState(ctx, traced)
	if got := trace.countContaining("set local search_path = pg_catalog, public"); got != 1 {
		t.Fatalf("continuity drift shared-reader search-path count = %d, want 1", got)
	}
	assertAppACLR2TraceOmitsControlSystem(t, trace)
	if err != nil {
		if state != AppACLR2StateCorrupt {
			t.Fatalf("continuity drift classifier error returned state %v, want error-only zero CORRUPT: %v", state, err)
		}
		return
	}
	if state != AppACLR2StateCorrupt {
		t.Fatalf("continuity drift classifier = %v, nil, want CORRUPT or error-only CORRUPT", state)
	}
}

func assertAppACLR2PostgresState(t *testing.T, ctx context.Context, db *pgxpool.Pool, want AppACLR2State) {
	t.Helper()
	got, err := classifyAppACLR2PostgresState(ctx, db)
	if err != nil {
		t.Fatalf("ClassifyAppACLR2State() error = %v", err)
	}
	if got != want {
		t.Fatalf("ClassifyAppACLR2State() = %v, want %v", got, want)
	}
}

func assertAppACLR2PostgresSQLState(t *testing.T, ctx context.Context, db *pgxpool.Pool, want string) {
	t.Helper()
	state, err := classifyAppACLR2PostgresState(ctx, db)
	if err == nil {
		t.Fatalf("ClassifyAppACLR2State() = %v, want SQLSTATE %s", state, want)
	}
	if state != AppACLR2StateCorrupt {
		t.Fatalf("ClassifyAppACLR2State() state with error = %v, want error-only CORRUPT", state)
	}
	requirePostgresSQLState(t, err, want)
}

func classifyAppACLR2PostgresState(ctx context.Context, db *pgxpool.Pool) (AppACLR2State, error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return AppACLR2StateCorrupt, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return ClassifyAppACLR2State(ctx, tx)
}

type appACLR2PostgresQueryTrace struct {
	mu         sync.Mutex
	statements []string
}

var _ pgx.QueryTracer = (*appACLR2PostgresQueryTrace)(nil)

func (trace *appACLR2PostgresQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.record(data.SQL)
	return ctx
}

func (*appACLR2PostgresQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *appACLR2PostgresQueryTrace) record(statement string) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.statements = append(trace.statements, strings.ToLower(strings.Join(strings.Fields(statement), " ")))
}

func (trace *appACLR2PostgresQueryTrace) countContaining(fragment string) int {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	count := 0
	fragment = strings.ToLower(fragment)
	for _, statement := range trace.statements {
		if strings.Contains(statement, fragment) {
			count++
		}
	}
	return count
}

func (trace *appACLR2PostgresQueryTrace) snapshot() []string {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return append([]string(nil), trace.statements...)
}

type appACLR2PostgresCommitACKFault struct {
	mu          sync.Mutex
	armed       bool
	injections  int
	callbackErr error
	afterCommit func(context.Context) error
}

func newAppACLR2PostgresCommitACKFault(afterCommit func(context.Context) error) *appACLR2PostgresCommitACKFault {
	return &appACLR2PostgresCommitACKFault{armed: true, afterCommit: afterCommit}
}

func (fault *appACLR2PostgresCommitACKFault) wrap(begin appACLR2BootstrapBeginTx) appACLR2BootstrapBeginTx {
	return func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := begin(ctx, options)
		if err != nil || tx == nil {
			return tx, err
		}
		fault.mu.Lock()
		inject := fault.armed
		fault.armed = false
		fault.mu.Unlock()
		if !inject {
			return tx, nil
		}
		return &appACLR2PostgresLostCommitACKTx{Tx: tx, fault: fault}, nil
	}
}

func (fault *appACLR2PostgresCommitACKFault) evidence() (int, error) {
	fault.mu.Lock()
	defer fault.mu.Unlock()
	return fault.injections, fault.callbackErr
}

type appACLR2PostgresLostCommitACKTx struct {
	pgx.Tx
	fault *appACLR2PostgresCommitACKFault
}

func (tx *appACLR2PostgresLostCommitACKTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	var callbackErr error
	if tx.fault.afterCommit != nil {
		callbackErr = tx.fault.afterCommit(ctx)
	}
	tx.fault.mu.Lock()
	tx.fault.injections++
	tx.fault.callbackErr = callbackErr
	tx.fault.mu.Unlock()
	return fmt.Errorf("APP ACL R2 PostgreSQL fixture lost commit acknowledgement")
}

type appACLR2PostgresTracingTx struct {
	pgx.Tx
	trace *appACLR2PostgresQueryTrace
}

func (tx *appACLR2PostgresTracingTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tx.trace.record(sql)
	return tx.Tx.Exec(ctx, sql, arguments...)
}

func (tx *appACLR2PostgresTracingTx) Query(ctx context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	tx.trace.record(sql)
	return tx.Tx.Query(ctx, sql, arguments...)
}

func (tx *appACLR2PostgresTracingTx) QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row {
	tx.trace.record(sql)
	return tx.Tx.QueryRow(ctx, sql, arguments...)
}

func traceAppACLR2BootstrapBegin(begin appACLR2BootstrapBeginTx, trace *appACLR2PostgresQueryTrace) appACLR2BootstrapBeginTx {
	return func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := begin(ctx, options)
		if err != nil || tx == nil {
			return tx, err
		}
		return &appACLR2PostgresTracingTx{Tx: tx, trace: trace}, nil
	}
}

func traceAppACLR2RuntimeBegin(begin appACLR2RuntimeAdmissionBeginTx, trace *appACLR2PostgresQueryTrace) appACLR2RuntimeAdmissionBeginTx {
	return func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := begin(ctx, options)
		if err != nil || tx == nil {
			return tx, err
		}
		return &appACLR2PostgresTracingTx{Tx: tx, trace: trace}, nil
	}
}

func observeAppACLR2PostgresBootstrapACK(
	ctx context.Context,
	db *pgxpool.Pool,
	trace *appACLR2PostgresQueryTrace,
) (appACLR2BootstrapACKOutcome, error) {
	begin := traceAppACLR2BootstrapBegin(
		newAppACLR2BootstrapTransitionLockedBegin(appACLR2RuntimeAdmissionPoolAcquire(db)),
		trace,
	)
	return recoverAppACLR2BootstrapACKWithDependencies(ctx, begin, defaultAppACLR2BootstrapACKObserverDependencies())
}

func assertAppACLR2BootstrapACKTraceOmitsM2Access(t *testing.T, trace *appACLR2PostgresQueryTrace, wantReservedInventoryCount int) {
	t.Helper()
	if got := trace.countContaining("set local search_path = pg_catalog, public"); got != 1 {
		t.Fatalf("bootstrap ACK observer search-path statement count = %d, want 1 without full-classifier repetition", got)
	}
	if got := trace.countContaining("select object_kind, schema_name, object_oid, object_identity, object_detail from ("); got != wantReservedInventoryCount {
		t.Fatalf("bootstrap ACK observer reserved-inventory statement count = %d, want %d for this observer outcome", got, wantReservedInventoryCount)
	}
	for _, fragment := range []string{
		"from public.app_acl_r2_manifest_revisions",
		"from public.app_acl_r2_manifest_head",
		"lock table public.app_acl_r2_manifest_revisions",
		"lock table public.app_acl_r2_manifest_head",
		"select * from public.app_acl_r2_manifest_revisions",
		"select * from public.app_acl_r2_manifest_head",
	} {
		if got := trace.countContaining(fragment); got != 0 {
			t.Fatalf("bootstrap ACK observer trace contains %d forbidden %q statements", got, fragment)
		}
	}
}

func assertAppACLR2BootstrapACKReservedInventoryOnlyTrace(t *testing.T, trace *appACLR2PostgresQueryTrace) {
	t.Helper()
	statements := trace.snapshot()
	if len(statements) != 3 {
		t.Fatalf("rejected-inventory bootstrap ACK observer statements = %#v, want exactly search-path/actor/inventory", statements)
	}
	for index, fragment := range []string{
		"set local search_path = pg_catalog, public",
		"from pg_catalog.pg_roles role where role.rolname = current_user",
		"select object_kind, schema_name, object_oid, object_identity, object_detail from (",
	} {
		if !strings.Contains(statements[index], fragment) {
			t.Fatalf("rejected-inventory bootstrap ACK observer statement %d = %q, want fragment %q", index, statements[index], fragment)
		}
	}
	assertAppACLR2BootstrapACKTraceOmitsM2Access(t, trace, 1)
	if got := trace.countContaining("from pg_catalog.pg_control_system()"); got != 0 {
		t.Fatalf("rejected-inventory bootstrap ACK observer invoked pg_control_system() %d times", got)
	}
}

func assertAppACLR2TraceOmitsControlSystem(t *testing.T, trace *appACLR2PostgresQueryTrace) {
	t.Helper()
	if got := trace.countContaining("from pg_catalog.pg_control_system()"); got != 0 {
		t.Fatalf("post-bootstrap trace contains %d pg_control_system() invocations", got)
	}
}

func assertAppACLR2BootstrapRepeatTraceOmitsMutation(t *testing.T, trace *appACLR2PostgresQueryTrace) {
	t.Helper()
	for _, fragment := range []string{
		"create function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert",
		"create table public.app_acl_r2_bootstrap_receipt",
		"insert into public.app_acl_r2_bootstrap_receipt",
		"revoke all privileges on table public.app_acl_r2_bootstrap_receipt",
		"grant select on table public.app_acl_r2_bootstrap_receipt",
		"revoke all privileges on function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert",
		"revoke all privileges on function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation",
	} {
		if got := trace.countContaining(fragment); got != 0 {
			t.Fatalf("ordinary PREPARED bootstrap repeat trace contains %d forbidden mutation statements matching %q", got, fragment)
		}
	}
}

func assertAppACLR2FinalizerTraceOmitsMutation(t *testing.T, trace *appACLR2PostgresQueryTrace) {
	t.Helper()
	for _, fragment := range []string{
		"create function record_platform_internal.app_acl_r2_reject_manifest_mutation",
		"create table public.app_acl_r2_manifest_revisions",
		"create table public.app_acl_r2_manifest_head",
		"insert into public.app_acl_r2_manifest_revisions",
		"insert into public.app_acl_r2_manifest_head",
		"revoke all privileges on table public.app_acl_r2_manifest_revisions",
		"revoke all privileges on table public.app_acl_r2_manifest_head",
		"grant select on table public.app_acl_r2_manifest_revisions",
		"grant select on table public.app_acl_r2_manifest_head",
		"revoke all privileges on function record_platform_internal.app_acl_r2_reject_manifest_mutation",
	} {
		if got := trace.countContaining(fragment); got != 0 {
			t.Fatalf("rejected finalizer trace contains %d forbidden mutation statements matching %q", got, fragment)
		}
	}
}
