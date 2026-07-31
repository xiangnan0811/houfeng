package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"

	appaclr2migrations "houfeng/db/appaclr2/migrations"
)

func TestAppACLR2FinalizeRejectsExactR1BeforeM2Mutation(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	dependencies.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
		fixture.trace = append(fixture.trace, "classifier")
		return AppACLR2StateR1, nil
	}

	err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
	if err == nil {
		t.Fatal("finalizeAppACLR2InTx() error = nil, want exact-R1 rejection")
	}
	assertAppACLR2FinalizeTraceOrder(t, fixture.trace, "actor", "classifier")
	assertAppACLR2FinalizeTraceAbsent(t, fixture.trace,
		"catalog-predicate", "verify-frozen", "bootstrap-catalog", "receipt-surface", "receipt", "pre-mutation", "source-preflight",
	)
	assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
}

func TestAppACLR2FinalizeRejectsNonExactPreparedPredicateBeforeM2Mutation(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	fixture.predicates.HasUnknownReservedObjects = true

	err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
	if err == nil {
		t.Fatal("finalizeAppACLR2InTx() error = nil, want non-exact PREPARED predicate rejection")
	}
	assertAppACLR2FinalizeTraceOrder(t, fixture.trace, "actor", "classifier", "catalog-predicate")
	assertAppACLR2FinalizeTraceAbsent(t, fixture.trace,
		"verify-frozen", "bootstrap-catalog", "receipt-surface", "receipt", "pre-mutation", "source-preflight",
	)
	assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
}

func TestAppACLR2FinalizeRejectsWrongDirectMigratorActorBeforeM2Mutation(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	actorErr := errors.New("direct migrator session identity does not match receipt binding")
	dependencies.requireDirectMigratorActor = func(context.Context, pgx.Tx) error {
		fixture.trace = append(fixture.trace, "actor")
		return actorErr
	}

	err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
	if !errors.Is(err, actorErr) {
		t.Fatalf("finalizeAppACLR2InTx() error = %v, want preserved actor error %v", err, actorErr)
	}
	assertAppACLR2FinalizeTraceOrder(t, fixture.trace, "actor")
	assertAppACLR2FinalizeTraceAbsent(t, fixture.trace,
		"classifier", "catalog-predicate", "verify-frozen", "bootstrap-catalog", "receipt-surface", "receipt", "pre-mutation", "source-preflight",
	)
	assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
}

func TestAppACLR2FinalizeRejectsFrozenM1DriftBeforeM2Mutation(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	fixture.frozen.SourceSetDigest[0] ^= 0xff
	fixture.predicates.FrozenState = fixture.frozen

	err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
	if err == nil {
		t.Fatal("finalizeAppACLR2InTx() error = nil, want frozen M1 drift rejection")
	}
	assertAppACLR2FinalizeTraceOrder(t, fixture.trace,
		"actor", "classifier", "catalog-predicate", "receipt", "source-preflight",
	)
	assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
}

func TestAppACLR2FinalizeRejectsSharedContinuityErrorBeforeM2Mutation(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	continuityErr := errors.New("shared constrained receipt continuity rejected")
	dependencies.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
		fixture.trace = append(fixture.trace, "classifier")
		return AppACLR2StateCorrupt, continuityErr
	}

	err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
	if !errors.Is(err, continuityErr) {
		t.Fatalf("finalizeAppACLR2InTx() error = %v, want shared continuity error %v", err, continuityErr)
	}
	assertAppACLR2FinalizeTraceOrder(t, fixture.trace, "actor", "classifier")
	assertAppACLR2FinalizeTraceAbsent(t, fixture.trace, "catalog-predicate", "receipt", "source-preflight")
	assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
}

func TestAppACLR2FinalizeRejectsNonExactSharedPredicateBeforeM2Mutation(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	fixture.predicates.ExactL1M1 = false

	err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
	if err == nil {
		t.Fatal("finalizeAppACLR2InTx() error = nil, want reusable catalog-predicate rejection")
	}
	assertAppACLR2FinalizeTraceOrder(t, fixture.trace, "actor", "classifier", "catalog-predicate")
	assertAppACLR2FinalizeTraceAbsent(t, fixture.trace, "receipt", "source-preflight")
	assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
}

func TestAppACLR2FinalizeExactPreparedCreatesM2ThenReadsExactFinalizedState(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)

	if err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies); err != nil {
		t.Fatalf("finalizeAppACLR2InTx() error = %v", err)
	}
	want := []string{
		"search-path", "actor", "locks", "classifier", "catalog-predicate", "receipt",
		"source-preflight", "finalizer-ddl", "m2-revision-insert", "m2-head-cas", "finalizer-dcl", "post-write-readback",
	}
	if !reflect.DeepEqual(fixture.trace, want) {
		t.Fatalf("finalizer exact PREPARED trace = %#v, want exact mandatory stage order %#v", fixture.trace, want)
	}
}

func TestAppACLR2FinalizeNormalFinalizedRepeatSkipsM2Mutation(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	dependencies.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
		fixture.trace = append(fixture.trace, "classifier")
		return AppACLR2StateFinalized, nil
	}

	if err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies); err != nil {
		t.Fatalf("finalizeAppACLR2InTx() error = %v", err)
	}
	assertAppACLR2FinalizeTraceOrder(t, fixture.trace, "actor", "classifier")
	assertAppACLR2FinalizeTraceAbsent(t, fixture.trace,
		"catalog-predicate", "verify-frozen", "bootstrap-catalog", "receipt-surface", "receipt", "pre-mutation", "source-preflight",
	)
	assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
}

func TestAppACLR2FinalizeDependenciesRequireAcknowledgementSafetyHooks(t *testing.T) {
	_, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	dependencies.safeToRetry = nil
	if err := dependencies.validate(); err == nil {
		t.Fatal("dependencies.validate() error = nil with safeToRetry missing")
	}

	dependencies.safeToRetry = func(error) bool { return false }
	dependencies.recoverCommitAcknowledgement = nil
	if err := dependencies.validate(); err == nil {
		t.Fatal("dependencies.validate() error = nil with recoverCommitAcknowledgement missing")
	}
}

func TestAppACLR2FinalizeProductionEntryRejectsNilPool(t *testing.T) {
	if err := FinalizeAppACLR2(t.Context(), nil); err == nil {
		t.Fatal("FinalizeAppACLR2(nil) error = nil, want rejection")
	}
}

func TestAppACLR2FinalizeProductionEntryUsesTransitionLockedReservedConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	serverErr := make(chan error, 1)
	config, err := pgxpool.ParseConfig("postgres://direct_migrator@127.0.0.1:5432/houfeng?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	config.MaxConns = 1
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go func() {
			serverErr <- serveAppACLR2FinalizeProductionRoute(server)
		}()
		return client, nil
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(func() {
		closed := make(chan struct{})
		go func() {
			pool.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Error("production finalizer test pool did not close; a reserved connection may not have been returned")
		}
	})

	err = FinalizeAppACLR2(ctx, pool)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" || pgErr.Message != "production finalizer route sentinel" {
		var routeErr error
		select {
		case routeErr = <-serverErr:
		default:
		}
		t.Fatalf("FinalizeAppACLR2() error = %v, want production wire sentinel SQLSTATE 42501 (wire route error = %v)", err, routeErr)
	}
	stats := pool.Stat()
	if stats.TotalConns() != 1 || stats.AcquiredConns() != 0 || stats.IdleConns() != 1 {
		t.Fatalf("production finalizer pool after rollback = total:%d acquired:%d idle:%d, want 1/0/1 reserved connection returned exactly once", stats.TotalConns(), stats.AcquiredConns(), stats.IdleConns())
	}

	pool.Close()
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("production finalizer wire route error = %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("production finalizer wire route did not close: %v", ctx.Err())
	}
}

func serveAppACLR2FinalizeProductionRoute(conn net.Conn) error {
	defer conn.Close()
	backend := pgproto3.NewBackend(conn, conn)
	startup, err := backend.ReceiveStartupMessage()
	if err != nil {
		return fmt.Errorf("receive production finalizer startup: %w", err)
	}
	if _, ok := startup.(*pgproto3.StartupMessage); !ok {
		return fmt.Errorf("production finalizer startup message = %T, want *pgproto3.StartupMessage", startup)
	}
	send := func(messages ...pgproto3.BackendMessage) error {
		for _, message := range messages {
			backend.Send(message)
		}
		return backend.Flush()
	}
	if err := send(
		&pgproto3.AuthenticationOk{},
		&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"},
		&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: "on"},
		&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 1}},
		&pgproto3.ReadyForQuery{TxStatus: 'I'},
	); err != nil {
		return fmt.Errorf("send production finalizer startup response: %w", err)
	}

	expected := []struct {
		label    string
		exactSQL string
		contains []string
	}{
		{
			label:    "session advisory lock",
			exactSQL: "select pg_catalog.pg_advisory_lock(pg_catalog.hashtextextended( 'houfeng.app-acl-r2-privileged-transition.v1' , 0))",
		},
		{label: "serializable read-write begin", exactSQL: "begin isolation level serializable read write"},
		{
			label:    "transaction advisory lock",
			exactSQL: "select pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended( 'houfeng.app-acl-r2-privileged-transition.v1' , 0))",
		},
		{
			label:    "session advisory unlock",
			exactSQL: "select pg_catalog.pg_advisory_unlock(pg_catalog.hashtextextended( 'houfeng.app-acl-r2-privileged-transition.v1' , 0))",
		},
		{label: "finalizer search path", exactSQL: "set local search_path = pg_catalog, public"},
		{
			label:    "finalizer actor gate",
			contains: []string{"select session_user::text, current_user::text", "from pg_catalog.pg_roles role", "where role.rolname = current_user"},
		},
		{label: "rollback", exactSQL: "rollback"},
	}
	for index, want := range expected {
		message, err := backend.Receive()
		if err != nil {
			return fmt.Errorf("receive production finalizer %s: %w", want.label, err)
		}
		query, ok := message.(*pgproto3.Query)
		if !ok {
			return fmt.Errorf("production finalizer %s frontend message = %T, want *pgproto3.Query", want.label, message)
		}
		normalizedSQL := strings.Join(strings.Fields(query.String), " ")
		if want.exactSQL != "" && normalizedSQL != want.exactSQL {
			return fmt.Errorf("production finalizer %s SQL = %q, want %q", want.label, normalizedSQL, want.exactSQL)
		}
		for _, fragment := range want.contains {
			if !strings.Contains(normalizedSQL, fragment) {
				return fmt.Errorf("production finalizer %s SQL = %q, want fragment %q", want.label, normalizedSQL, fragment)
			}
		}

		switch index {
		case 0:
			err = send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")}, &pgproto3.ReadyForQuery{TxStatus: 'I'})
		case 1:
			err = send(&pgproto3.CommandComplete{CommandTag: []byte("BEGIN")}, &pgproto3.ReadyForQuery{TxStatus: 'T'})
		case 2:
			err = send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")}, &pgproto3.ReadyForQuery{TxStatus: 'T'})
		case 3:
			err = send(
				&pgproto3.RowDescription{Fields: []pgproto3.FieldDescription{{
					Name: []byte("pg_advisory_unlock"), DataTypeOID: 16, DataTypeSize: 1, TypeModifier: -1,
				}}},
				&pgproto3.DataRow{Values: [][]byte{[]byte("t")}},
				&pgproto3.CommandComplete{CommandTag: []byte("SELECT 1")},
				&pgproto3.ReadyForQuery{TxStatus: 'T'},
			)
		case 4:
			err = send(&pgproto3.CommandComplete{CommandTag: []byte("SET")}, &pgproto3.ReadyForQuery{TxStatus: 'T'})
		case 5:
			err = send(
				&pgproto3.ErrorResponse{Severity: "ERROR", Code: "42501", Message: "production finalizer route sentinel"},
				&pgproto3.ReadyForQuery{TxStatus: 'E'},
			)
		case 6:
			err = send(&pgproto3.CommandComplete{CommandTag: []byte("ROLLBACK")}, &pgproto3.ReadyForQuery{TxStatus: 'I'})
		}
		if err != nil {
			return fmt.Errorf("send production finalizer %s response: %w", want.label, err)
		}
	}

	message, err := backend.Receive()
	if err != nil {
		return fmt.Errorf("receive production finalizer termination: %w", err)
	}
	if _, ok := message.(*pgproto3.Terminate); !ok {
		return fmt.Errorf("production finalizer terminal frontend message = %T, want *pgproto3.Terminate", message)
	}
	return nil
}

func TestAppACLR2FinalizeDefaultDependenciesIncludeAcknowledgementRecovery(t *testing.T) {
	dependencies := defaultAppACLR2FinalizeDependencies(func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		return nil, errors.New("test finalizer transaction opener must not run")
	})
	if err := dependencies.validate(); err != nil {
		t.Fatalf("defaultAppACLR2FinalizeDependencies().validate() error = %v", err)
	}
}

func TestAppACLR2FinalizeDefaultCatalogPathsUseSharedConstrainedReader(t *testing.T) {
	dependencies := defaultAppACLR2FinalizeDependencies(func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		return nil, errors.New("test finalizer transaction opener must not run")
	})
	assertSameFunction := func(label string, got, want any) {
		t.Helper()
		if reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
			t.Fatalf("default finalizer %s does not use the shared constrained production path", label)
		}
	}
	assertSameFunction("classifier", dependencies.classify, ClassifyAppACLR2State)
	assertSameFunction("catalog predicate reader", dependencies.readCatalogPredicates, ReadAppACLR2CatalogPredicatesInTx)
	assertSameFunction("acknowledgement classifier", defaultAppACLR2FinalizeACKDependencies().classify, ClassifyAppACLR2State)
}

func TestAppACLR2FinalizeDefaultAcknowledgementRecoveryUsesNativeM2Reclassification(t *testing.T) {
	permissionErr := &pgconn.PgError{Code: "42501", Message: "permission denied for table app_acl_r2_manifest_revisions"}
	base := newAppACLR2PublicR1Tx(t, "direct_migrator", "direct_migrator", []AppACLR2ReservedCatalogObjectV1{{
		OID: 2004, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r",
	}})
	base.queryErrorFragment = "from public.app_acl_r2_manifest_revisions"
	base.queryError = permissionErr
	tx := &fakeAppACLR2FinalizeDefaultACKTx{appACLR2PublicR1Tx: base, m2RevisionsPresent: true}

	var options []pgx.TxOptions
	dependencies := defaultAppACLR2FinalizeDependencies(func(_ context.Context, got pgx.TxOptions) (pgx.Tx, error) {
		options = append(options, got)
		return tx, nil
	})
	state, err := dependencies.recoverCommitAcknowledgement(t.Context())
	if state != AppACLR2StateCorrupt {
		t.Fatalf("default acknowledgement recovery state = %v, want error-only CORRUPT zero value", state)
	}
	var gotPgErr *pgconn.PgError
	if !errors.As(err, &gotPgErr) || gotPgErr != permissionErr {
		t.Fatalf("default acknowledgement recovery error = %v, want original native M2 read error %#v", err, permissionErr)
	}
	assertAppACLR2FinalizeSerializableReadWriteOptions(t, options, 1)
	if !base.queried("select * from public.app_acl_r2_manifest_revisions limit 0") {
		t.Fatal("default acknowledgement recovery did not reach the production native M2 authority read")
	}
	if tx.actorCalls != 1 || tx.presenceCalls != 1 || tx.commits != 0 || tx.rollbacks != 1 {
		t.Fatalf("default acknowledgement recovery calls = actor:%d presence:%d commit:%d rollback:%d, want 1/1/0/1", tx.actorCalls, tx.presenceCalls, tx.commits, tx.rollbacks)
	}
}

func TestAppACLR2FinalizeUsesFrozenEvidenceFromSharedPreparedPredicate(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	fixture.predicates.FrozenState.SourceSetDigest[0] ^= 0xff

	err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
	if err == nil {
		t.Fatal("finalizeAppACLR2InTx() error = nil with frozen evidence drift from the shared predicate")
	}
	assertAppACLR2FinalizeTraceOrder(t, fixture.trace, "actor", "classifier", "catalog-predicate", "receipt", "source-preflight")
	assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
}

func TestAppACLR2FinalizeAcknowledgementObserverAcceptsOnlyExactPreparedOrFinalized(t *testing.T) {
	for _, tt := range []struct {
		name    string
		state   AppACLR2State
		wantErr bool
	}{
		{name: "prepared", state: AppACLR2StatePrepared},
		{name: "finalized", state: AppACLR2StateFinalized},
		{name: "r1", state: AppACLR2StateR1, wantErr: true},
		{name: "corrupt", state: AppACLR2StateCorrupt, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			trace := make([]string, 0, 4)
			dependencies := appACLR2FinalizeACKDependencies{
				hardenSearchPath: func(context.Context, pgx.Tx) error {
					trace = append(trace, "search-path")
					return nil
				},
				requireDirectMigratorActor: func(context.Context, pgx.Tx) error {
					trace = append(trace, "actor")
					return nil
				},
				lockStateTables: func(context.Context, pgx.Tx) error {
					trace = append(trace, "locks")
					return nil
				},
				classify: func(context.Context, pgx.Tx) (AppACLR2State, error) {
					trace = append(trace, "classifier")
					return tt.state, nil
				},
			}
			state, err := observeAppACLR2FinalizeACKRecoveryInTxWithDependencies(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
			if (err != nil) != tt.wantErr {
				t.Fatalf("observeAppACLR2FinalizeACKRecoveryInTxWithDependencies() = (%v, %v), want error=%t", state, err, tt.wantErr)
			}
			if !tt.wantErr && state != tt.state {
				t.Fatalf("recovery state = %v, want %v", state, tt.state)
			}
			assertAppACLR2FinalizeTraceOrder(t, trace, "search-path", "actor", "locks", "classifier")
		})
	}
}

func TestRecoverAppACLR2FinalizeACKUsesSerializableReadWriteTransaction(t *testing.T) {
	var options []pgx.TxOptions
	dependencies := appACLR2FinalizeACKDependencies{
		hardenSearchPath:           func(context.Context, pgx.Tx) error { return nil },
		requireDirectMigratorActor: func(context.Context, pgx.Tx) error { return nil },
		lockStateTables:            func(context.Context, pgx.Tx) error { return nil },
		classify: func(context.Context, pgx.Tx) (AppACLR2State, error) {
			return AppACLR2StateFinalized, nil
		},
	}

	state, err := recoverAppACLR2FinalizeACKWithDependencies(t.Context(), func(_ context.Context, got pgx.TxOptions) (pgx.Tx, error) {
		options = append(options, got)
		return &fakeAppACLR2FinalizeRunTx{}, nil
	}, dependencies)
	if err != nil || state != AppACLR2StateFinalized {
		t.Fatalf("recoverAppACLR2FinalizeACKWithDependencies() state/error = %v/%v, want FINALIZED/nil", state, err)
	}
	want := pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite}
	if len(options) != 1 || !reflect.DeepEqual(options[0], want) {
		t.Fatalf("acknowledgement recovery transaction options = %#v, want %#v", options, want)
	}
}

func TestAppACLR2FinalizeUsesTransitionLockedReservedConnectionLifecycle(t *testing.T) {
	for _, tt := range []struct {
		name          string
		wantErr       bool
		wantCommits   int
		wantRollbacks int
		wantTrace     []string
	}{
		{
			name:        "success",
			wantCommits: 1,
			wantTrace: []string{
				"session-lock", "begin", "transaction-lock", "session-unlock",
				"search-path", "actor", "locks", "classifier", "catalog-predicate", "receipt", "source-preflight",
				"finalizer-ddl", "m2-revision-insert", "m2-head-cas", "finalizer-dcl", "post-write-readback",
				"commit", "release",
			},
		},
		{
			name:          "non-retryable finalizer failure",
			wantErr:       true,
			wantRollbacks: 1,
			wantTrace: []string{
				"session-lock", "begin", "transaction-lock", "session-unlock",
				"search-path", "actor", "locks", "classifier", "catalog-predicate", "receipt", "source-preflight",
				"finalizer-ddl", "rollback", "release",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
			failure := errors.New("finalizer lifecycle failure")
			if tt.wantErr {
				dependencies.executeFinalizeSection = func(context.Context, pgx.Tx) error {
					fixture.trace = append(fixture.trace, "finalizer-ddl")
					return failure
				}
			}

			handoffTx := &fakeAppACLR2BootstrapLockHandoffTx{trace: &fixture.trace}
			var options []pgx.TxOptions
			acquires := 0
			conn := &fakeAppACLR2BootstrapReservedConn{
				trace: &fixture.trace,
				tx:    handoffTx,
				onBegin: func(got pgx.TxOptions) {
					options = append(options, got)
				},
			}
			begin := newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
				acquires++
				return conn, nil
			})

			err := finalizeAppACLR2WithDependencies(t.Context(), begin, dependencies)
			if tt.wantErr {
				if !errors.Is(err, failure) {
					t.Fatalf("finalizeAppACLR2WithDependencies() error = %v, want %v", err, failure)
				}
			} else if err != nil {
				t.Fatalf("finalizeAppACLR2WithDependencies() error = %v", err)
			}
			assertAppACLR2FinalizeSerializableReadWriteOptions(t, options, 1)
			if acquires != 1 {
				t.Fatalf("reserved connection acquires = %d, want one whole-closure reservation", acquires)
			}
			if conn.releases != 1 || conn.discards != 0 {
				t.Fatalf("reserved connection finishes = release:%d discard:%d, want one release and no discard", conn.releases, conn.discards)
			}
			if got := appACLR2BootstrapTraceCount(fixture.trace, "commit"); got != tt.wantCommits {
				t.Fatalf("finalizer commits = %d, want %d", got, tt.wantCommits)
			}
			if got := appACLR2BootstrapTraceCount(fixture.trace, "rollback"); got != tt.wantRollbacks {
				t.Fatalf("finalizer rollbacks = %d, want %d", got, tt.wantRollbacks)
			}
			if got := appACLR2BootstrapTraceCount(fixture.trace, "release"); got != 1 {
				t.Fatalf("reserved connection releases in trace = %d, want exactly one", got)
			}
			if !reflect.DeepEqual(fixture.trace, tt.wantTrace) {
				t.Fatalf("finalizer reserved-connection lifecycle = %#v, want %#v", fixture.trace, tt.wantTrace)
			}
		})
	}
}

func TestRecoverAppACLR2FinalizeACKUsesTransitionLockedReservedConnectionLifecycle(t *testing.T) {
	for _, tt := range []struct {
		name          string
		wantErr       bool
		wantState     AppACLR2State
		wantCommits   int
		wantRollbacks int
		wantTrace     []string
	}{
		{
			name:        "finalized",
			wantState:   AppACLR2StateFinalized,
			wantCommits: 1,
			wantTrace: []string{
				"session-lock", "begin", "transaction-lock", "session-unlock",
				"observer-search-path", "observer-actor", "observer-locks", "observer-classifier",
				"commit", "release",
			},
		},
		{
			name:          "observer error",
			wantErr:       true,
			wantState:     AppACLR2StateCorrupt,
			wantRollbacks: 1,
			wantTrace: []string{
				"session-lock", "begin", "transaction-lock", "session-unlock",
				"observer-search-path", "observer-actor", "observer-locks", "observer-classifier",
				"rollback", "release",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			trace := make([]string, 0, len(tt.wantTrace))
			failure := errors.New("acknowledgement observer failure")
			dependencies := appACLR2FinalizeACKDependencies{
				hardenSearchPath: func(context.Context, pgx.Tx) error {
					trace = append(trace, "observer-search-path")
					return nil
				},
				requireDirectMigratorActor: func(context.Context, pgx.Tx) error {
					trace = append(trace, "observer-actor")
					return nil
				},
				lockStateTables: func(context.Context, pgx.Tx) error {
					trace = append(trace, "observer-locks")
					return nil
				},
				classify: func(context.Context, pgx.Tx) (AppACLR2State, error) {
					trace = append(trace, "observer-classifier")
					if tt.wantErr {
						return AppACLR2StateCorrupt, failure
					}
					return AppACLR2StateFinalized, nil
				},
			}

			handoffTx := &fakeAppACLR2BootstrapLockHandoffTx{trace: &trace}
			var options []pgx.TxOptions
			acquires := 0
			conn := &fakeAppACLR2BootstrapReservedConn{
				trace: &trace,
				tx:    handoffTx,
				onBegin: func(got pgx.TxOptions) {
					options = append(options, got)
				},
			}
			begin := newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
				acquires++
				return conn, nil
			})

			state, err := recoverAppACLR2FinalizeACKWithDependencies(t.Context(), begin, dependencies)
			if state != tt.wantState {
				t.Fatalf("recoverAppACLR2FinalizeACKWithDependencies() state = %v, want %v", state, tt.wantState)
			}
			if tt.wantErr {
				if !errors.Is(err, failure) {
					t.Fatalf("recoverAppACLR2FinalizeACKWithDependencies() error = %v, want %v", err, failure)
				}
			} else if err != nil {
				t.Fatalf("recoverAppACLR2FinalizeACKWithDependencies() error = %v", err)
			}
			assertAppACLR2FinalizeSerializableReadWriteOptions(t, options, 1)
			if acquires != 1 {
				t.Fatalf("reserved connection acquires = %d, want one whole-closure reservation", acquires)
			}
			if conn.releases != 1 || conn.discards != 0 {
				t.Fatalf("reserved connection finishes = release:%d discard:%d, want one release and no discard", conn.releases, conn.discards)
			}
			if got := appACLR2BootstrapTraceCount(trace, "commit"); got != tt.wantCommits {
				t.Fatalf("acknowledgement recovery commits = %d, want %d", got, tt.wantCommits)
			}
			if got := appACLR2BootstrapTraceCount(trace, "rollback"); got != tt.wantRollbacks {
				t.Fatalf("acknowledgement recovery rollbacks = %d, want %d", got, tt.wantRollbacks)
			}
			if got := appACLR2BootstrapTraceCount(trace, "release"); got != 1 {
				t.Fatalf("reserved connection releases in trace = %d, want exactly one", got)
			}
			if !reflect.DeepEqual(trace, tt.wantTrace) {
				t.Fatalf("acknowledgement recovery reserved-connection lifecycle = %#v, want %#v", trace, tt.wantTrace)
			}
		})
	}
}

func TestAppACLR2FinalizeDefaultReadbackRunsProductionPredicatesAfterDCL(t *testing.T) {
	permissionErr := &pgconn.PgError{Code: "42501", Message: "permission denied for table app_acl_r2_manifest_revisions"}
	tx := newAppACLR2PublicR1Tx(t, "direct_migrator", "direct_migrator", []AppACLR2ReservedCatalogObjectV1{{
		OID: 2004, Kind: "relation", Schema: "public", Identity: "app_acl_r2_manifest_revisions", Detail: "r",
	}})
	tx.queryErrorFragment = "from public.app_acl_r2_manifest_revisions"
	tx.queryError = permissionErr

	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	production := defaultAppACLR2FinalizeDependencies(func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		return nil, errors.New("post-DCL readback must stay in the caller transaction")
	})
	dependencies.readFinalized = func(ctx context.Context, gotTx pgx.Tx) error {
		fixture.trace = append(fixture.trace, "production-readback")
		return production.readFinalized(ctx, gotTx)
	}

	err := finalizeAppACLR2InTx(t.Context(), tx, dependencies)
	var gotPgErr *pgconn.PgError
	if !errors.As(err, &gotPgErr) || gotPgErr != permissionErr {
		t.Fatalf("finalizeAppACLR2InTx() error = %v, want original production readback error %#v", err, permissionErr)
	}
	wantTrace := []string{
		"search-path", "actor", "locks", "classifier", "catalog-predicate", "receipt",
		"source-preflight", "finalizer-ddl", "m2-revision-insert", "m2-head-cas", "finalizer-dcl", "production-readback",
	}
	if !reflect.DeepEqual(fixture.trace, wantTrace) {
		t.Fatalf("finalizer production readback trace = %#v, want exact post-DCL order %#v", fixture.trace, wantTrace)
	}
	if !tx.queried("select * from public.app_acl_r2_manifest_revisions limit 0") {
		t.Fatal("default post-DCL readback did not reach the production native M2 authority read")
	}
}

func TestAppACLR2FinalizeExecutesMarkerExcludedM2SectionOnly(t *testing.T) {
	tx := &fakeAppACLR2BootstrapTx{}
	if err := executeAppACLR2FinalizeSectionInTx(t.Context(), tx); err != nil {
		t.Fatalf("executeAppACLR2FinalizeSectionInTx() error = %v", err)
	}
	if len(tx.execSQL) != 1 {
		t.Fatalf("finalizer SQL executions = %d, want one", len(tx.execSQL))
	}
	payload := tx.execSQL[0]
	for _, forbidden := range []string{
		appACLR2BootstrapBeginMarker,
		appACLR2BootstrapEndMarker,
		appACLR2FinalizeBeginMarker,
		appACLR2FinalizeEndMarker,
	} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("finalizer SQL payload contains marker %q", forbidden)
		}
	}
	if strings.Contains(payload, "app_acl_r2_finalize_authority") {
		t.Fatalf("direct-owner finalizer SQL payload contains obsolete bootstrap-owned finalization authority: %q", payload)
	}
	for _, required := range []string{
		"create table public.app_acl_r2_manifest_revisions",
		"create table public.app_acl_r2_manifest_head",
		"create function record_platform_internal.app_acl_r2_reject_manifest_mutation()",
	} {
		if !strings.Contains(payload, required) {
			t.Fatalf("direct-migrator finalizer SQL payload is missing direct-owner DDL %q", required)
		}
	}
}

func TestAppACLR2FinalizeSectionRequiresFixedBoundsAndTopLevelDDLShape(t *testing.T) {
	section := appACLR2FinalizeCanonicalSection(t)

	t.Run("canonical", func(t *testing.T) {
		sql, err := appACLR2FinalizeExecutableSQL(section)
		if err != nil {
			t.Fatalf("appACLR2FinalizeExecutableSQL(canonical) error = %v", err)
		}
		// appACLR2SourceSection starts at the bare BEGIN marker and retains the
		// physical "-- " prefix before the END marker. The fixed logical marker
		// range excludes that three-byte comment prefix.
		if got := len(section) - len("-- "); got != 4528 {
			t.Fatalf("canonical finalizer section bytes = %d, want literal 4528", got)
		}
		if got := len(sql); got != 4462 {
			t.Fatalf("canonical finalizer executable bytes = %d, want literal 4462", got)
		}
	})

	for _, tt := range appACLR2InvalidFinalizeSections(t, section) {
		t.Run(tt.name, func(t *testing.T) {
			if tt.sameSize && len(tt.section) != len(section) {
				t.Fatalf("%s finalizer section bytes = %d, want unchanged cardinality-case size %d", tt.name, len(tt.section), len(section))
			}
			if _, err := appACLR2FinalizeExecutableSQL(tt.section); err == nil {
				t.Fatal("appACLR2FinalizeExecutableSQL() error = nil, want fixed finalizer bounds or DDL-shape rejection")
			}
		})
	}
}

func TestAppACLR2FinalizeRejectsInvalidSectionBeforeM2Mutation(t *testing.T) {
	section := appACLR2FinalizeCanonicalSection(t)
	for _, tt := range appACLR2InvalidFinalizeSections(t, section) {
		t.Run(tt.name, func(t *testing.T) {
			fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
			dependencies.preflightSourceEvidence = func() error {
				fixture.trace = append(fixture.trace, "source-preflight")
				_, err := appACLR2FinalizeExecutableSQL(tt.section)
				return err
			}

			err := finalizeAppACLR2InTx(t.Context(), &fakeAppACLR2FinalizeTx{}, dependencies)
			if err == nil {
				t.Fatal("finalizeAppACLR2InTx() error = nil, want finalizer-section preflight rejection")
			}
			assertAppACLR2FinalizeTraceOrder(t, fixture.trace,
				"actor", "classifier", "catalog-predicate", "receipt", "source-preflight",
			)
			assertAppACLR2FinalizeTraceAbsent(t, fixture.trace,
				"finalizer-ddl", "m2-revision-insert", "m2-head-cas", "finalizer-dcl", "post-write-readback",
			)
			assertAppACLR2FinalizePreMutationOnly(t, fixture.trace)
		})
	}
}

func TestAppACLR2FinalizeInvalidSectionRejectsBeforeTxExec(t *testing.T) {
	section := appACLR2FinalizeCanonicalSection(t)
	for _, tt := range appACLR2InvalidFinalizeSections(t, section) {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeAppACLR2BootstrapTx{}
			err := executeAppACLR2FinalizeSQLInTx(t.Context(), tx, tt.section)
			if err == nil {
				t.Fatal("executeAppACLR2FinalizeSQLInTx() error = nil, want invalid section rejection")
			}
			if len(tx.execSQL) != 0 {
				t.Fatalf("invalid finalizer section executed %d SQL statements, want zero", len(tx.execSQL))
			}
		})
	}
}

func TestAppACLR2FinalizeAppliesExactRevokeFirstM2ControlACL(t *testing.T) {
	tx := &fakeAppACLR2FinalizeDCLTx{bootstrapRole: "bootstrap_oid10"}
	state := FrozenAppACLR1StateV1{
		DirectMigratorRole: "direct_migrator",
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
	}
	if err := applyAppACLR2FinalizeM2ControlACLInTx(t.Context(), tx, state); err != nil {
		t.Fatalf("applyAppACLR2FinalizeM2ControlACLInTx() error = %v", err)
	}
	want := []string{
		"revoke all privileges on table public.app_acl_r2_manifest_revisions from public, \"bootstrap_oid10\", \"direct_migrator\", \"center_runtime\", \"platform_admin\"",
		"revoke all privileges on table public.app_acl_r2_manifest_head from public, \"bootstrap_oid10\", \"direct_migrator\", \"center_runtime\", \"platform_admin\"",
		"grant select on table public.app_acl_r2_manifest_revisions to \"direct_migrator\"",
		"grant select on table public.app_acl_r2_manifest_revisions to \"center_runtime\"",
		"grant select on table public.app_acl_r2_manifest_head to \"direct_migrator\"",
		"grant select on table public.app_acl_r2_manifest_head to \"center_runtime\"",
		"revoke all privileges on function record_platform_internal.app_acl_r2_reject_manifest_mutation() from public, \"bootstrap_oid10\", \"direct_migrator\", \"center_runtime\", \"platform_admin\"",
		"grant execute on function record_platform_internal.app_acl_r2_reject_manifest_mutation() to \"direct_migrator\"",
	}
	if !reflect.DeepEqual(tx.execSQL, want) {
		t.Fatalf("M2 DCL statements = %#v, want exact revoke-first sequence %#v", tx.execSQL, want)
	}
}

func TestAppACLR2FinalizeInsertsM2RevisionWithFrozenM1CASAndSingletonHeadCAS(t *testing.T) {
	fixture, _ := newAppACLR2FinalizePreMutationFixture(t)
	manifest, body, digest, err := compileAppACLR2FinalizeManifest(fixture.frozen, fixture.receiptRow)
	if err != nil {
		t.Fatalf("compileAppACLR2FinalizeManifest() error = %v", err)
	}
	tx := &fakeAppACLR2FinalizeWriteTx{tags: []pgconn.CommandTag{
		pgconn.NewCommandTag("INSERT 0 1"),
		pgconn.NewCommandTag("INSERT 0 1"),
	}}
	if err := insertAppACLR2FinalizeM2RevisionInTx(t.Context(), tx, manifest, body, digest); err != nil {
		t.Fatalf("insertAppACLR2FinalizeM2RevisionInTx() error = %v", err)
	}
	if err := compareAndSwapAppACLR2FinalizeM2HeadInTx(t.Context(), tx, manifest, digest); err != nil {
		t.Fatalf("compareAndSwapAppACLR2FinalizeM2HeadInTx() error = %v", err)
	}
	if len(tx.execSQL) != 2 {
		t.Fatalf("M2 write SQL executions = %d, want revision plus head", len(tx.execSQL))
	}
	if !strings.Contains(tx.execSQL[0], "from public.app_acl_manifest_revisions as frozen_m1") ||
		!strings.Contains(tx.execSQL[0], "frozen_m1.manifest_revision = $1") ||
		!strings.Contains(tx.execSQL[0], "frozen_m1.manifest_digest = $2") {
		t.Fatalf("M2 revision SQL does not bind frozen M1 CAS: %q", tx.execSQL[0])
	}
	if !strings.Contains(tx.execSQL[1], "where not exists") || !strings.Contains(tx.execSQL[1], "public.app_acl_r2_manifest_head") {
		t.Fatalf("M2 head SQL does not use empty-head CAS: %q", tx.execSQL[1])
	}
}

func TestAppACLR2FinalizeRejectsStaleM2HeadCAS(t *testing.T) {
	fixture, _ := newAppACLR2FinalizePreMutationFixture(t)
	manifest, _, digest, err := compileAppACLR2FinalizeManifest(fixture.frozen, fixture.receiptRow)
	if err != nil {
		t.Fatalf("compileAppACLR2FinalizeManifest() error = %v", err)
	}
	tx := &fakeAppACLR2FinalizeWriteTx{tags: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 0")}}
	if err := compareAndSwapAppACLR2FinalizeM2HeadInTx(t.Context(), tx, manifest, digest); err == nil {
		t.Fatal("compareAndSwapAppACLR2FinalizeM2HeadInTx() error = nil, want stale CAS rejection")
	}
}

func TestAppACLR2FinalizeRetriesOnlyWholeSerializableClosures(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		message string
	}{
		{name: "serialization failure", code: "40001", message: "serialization failure"},
		{name: "deadlock detected", code: "40P01", message: "deadlock detected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
			classifierCalls := 0
			dependencies.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
				classifierCalls++
				fixture.trace = append(fixture.trace, "classifier")
				if classifierCalls == 1 {
					return AppACLR2StateCorrupt, &pgconn.PgError{Code: tt.code, Message: tt.message}
				}
				return AppACLR2StatePrepared, nil
			}
			transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, 2)
			transactionOptions := make([]pgx.TxOptions, 0, 2)
			err := finalizeAppACLR2WithDependencies(t.Context(), func(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				transactionOptions = append(transactionOptions, options)
				tx := &fakeAppACLR2FinalizeRunTx{}
				transactions = append(transactions, tx)
				return tx, nil
			}, dependencies)
			if err != nil {
				t.Fatalf("finalizeAppACLR2WithDependencies() error = %v", err)
			}
			if len(transactions) != 2 {
				t.Fatalf("transaction attempts = %d, want two complete serializable closures", len(transactions))
			}
			if len(transactionOptions) != len(transactions) {
				t.Fatalf("transaction options captured = %d, want one per attempt", len(transactionOptions))
			}
			for attempt, options := range transactionOptions {
				if options.IsoLevel != pgx.Serializable || options.AccessMode != pgx.ReadWrite {
					t.Fatalf("transaction attempt %d options = %#v, want Serializable ReadWrite", attempt+1, options)
				}
			}
			if transactions[0].rollbacks != 1 || transactions[0].commits != 0 || transactions[1].rollbacks != 0 || transactions[1].commits != 1 {
				t.Fatalf("transaction finishes = first(%d rollback, %d commit) second(%d rollback, %d commit), want rollback then commit", transactions[0].rollbacks, transactions[0].commits, transactions[1].rollbacks, transactions[1].commits)
			}
			if got := appACLR2FinalizeTraceCount(fixture.trace, "actor"); got != 2 {
				t.Fatalf("trace = %#v, actor gate count = %d, want whole-closure retry", fixture.trace, got)
			}
		})
	}
}

func TestAppACLR2FinalizeRetriesWholeClosureAtDownstreamStages(t *testing.T) {
	for _, stage := range appACLR2FinalizeDownstreamFailureStages() {
		for _, retryable := range []struct {
			name string
			code string
		}{
			{name: "serialization_failure", code: "40001"},
			{name: "deadlock_detected", code: "40P01"},
		} {
			t.Run(stage.name+"/"+retryable.name, func(t *testing.T) {
				fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
				failure := &pgconn.PgError{Code: retryable.code, Message: retryable.name}
				if !stage.commit {
					injectAppACLR2FinalizeStageFailures(t, &dependencies, stage.name, failure, 1)
				}
				recoveryCalls := 0
				dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
					recoveryCalls++
					return AppACLR2StateFinalized, nil
				}

				transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, 2)
				options := make([]pgx.TxOptions, 0, 2)
				err := finalizeAppACLR2WithDependencies(t.Context(), func(_ context.Context, got pgx.TxOptions) (pgx.Tx, error) {
					options = append(options, got)
					tx := &fakeAppACLR2FinalizeRunTx{}
					if stage.commit && len(transactions) == 0 {
						tx.commitErr = failure
					}
					transactions = append(transactions, tx)
					return tx, nil
				}, dependencies)
				if err != nil {
					t.Fatalf("finalizeAppACLR2WithDependencies() error = %v", err)
				}
				if len(transactions) != 2 {
					t.Fatalf("transaction attempts = %d, want one failed attempt plus one complete retry", len(transactions))
				}
				assertAppACLR2FinalizeSerializableReadWriteOptions(t, options, 2)
				if transactions[0].rollbacks != 1 || transactions[1].rollbacks != 0 || transactions[1].commits != 1 {
					t.Fatalf("transaction finishes = first(%d rollback, %d commit) second(%d rollback, %d commit), want rollback then successful commit", transactions[0].rollbacks, transactions[0].commits, transactions[1].rollbacks, transactions[1].commits)
				}
				wantFirstCommits := 0
				if stage.commit {
					wantFirstCommits = 1
				}
				if transactions[0].commits != wantFirstCommits {
					t.Fatalf("first-attempt commits = %d, want %d", transactions[0].commits, wantFirstCommits)
				}
				for _, label := range []string{"search-path", "actor", "locks", "finalizer-ddl", stage.traceLabel} {
					if got := appACLR2FinalizeTraceCount(fixture.trace, label); got != 2 {
						t.Fatalf("trace = %#v, %s count = %d, want whole-closure replay count 2", fixture.trace, label, got)
					}
				}
				if recoveryCalls != 0 {
					t.Fatalf("acknowledgement recovery calls = %d, want zero for explicit SQLSTATE retry", recoveryCalls)
				}
			})
		}
	}
}

func TestAppACLR2FinalizeBoundsDownstreamRetryableFailuresAndPreservesIdentity(t *testing.T) {
	for _, stage := range appACLR2FinalizeDownstreamFailureStages() {
		for _, retryable := range []struct {
			name string
			code string
		}{
			{name: "serialization_failure", code: "40001"},
			{name: "deadlock_detected", code: "40P01"},
		} {
			t.Run(stage.name+"/"+retryable.name, func(t *testing.T) {
				fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
				failure := &pgconn.PgError{Code: retryable.code, Message: retryable.name}
				if !stage.commit {
					injectAppACLR2FinalizeStageFailures(t, &dependencies, stage.name, failure, appACLR2BootstrapMaxAttempts)
				}
				recoveryCalls := 0
				dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
					recoveryCalls++
					return AppACLR2StateFinalized, nil
				}

				transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, appACLR2BootstrapMaxAttempts)
				options := make([]pgx.TxOptions, 0, appACLR2BootstrapMaxAttempts)
				err := finalizeAppACLR2WithDependencies(t.Context(), func(_ context.Context, got pgx.TxOptions) (pgx.Tx, error) {
					options = append(options, got)
					tx := &fakeAppACLR2FinalizeRunTx{}
					if stage.commit {
						tx.commitErr = failure
					}
					transactions = append(transactions, tx)
					return tx, nil
				}, dependencies)
				if !errors.Is(err, failure) {
					t.Fatalf("finalizeAppACLR2WithDependencies() error = %v, want preserved exhausted retry error %v", err, failure)
				}
				if len(transactions) != appACLR2BootstrapMaxAttempts {
					t.Fatalf("transaction attempts = %d, want bounded maximum %d", len(transactions), appACLR2BootstrapMaxAttempts)
				}
				assertAppACLR2FinalizeSerializableReadWriteOptions(t, options, appACLR2BootstrapMaxAttempts)
				for attempt, tx := range transactions {
					wantCommits := 0
					if stage.commit {
						wantCommits = 1
					}
					if tx.rollbacks != 1 || tx.commits != wantCommits {
						t.Fatalf("attempt %d finishes = %d rollbacks, %d commits, want 1/%d", attempt+1, tx.rollbacks, tx.commits, wantCommits)
					}
				}
				for _, label := range []string{"search-path", "actor", "locks", "finalizer-ddl", stage.traceLabel} {
					if got := appACLR2FinalizeTraceCount(fixture.trace, label); got != appACLR2BootstrapMaxAttempts {
						t.Fatalf("trace = %#v, %s count = %d, want bounded whole-closure count %d", fixture.trace, label, got, appACLR2BootstrapMaxAttempts)
					}
				}
				if recoveryCalls != 0 {
					t.Fatalf("acknowledgement recovery calls = %d, want zero for explicit SQLSTATE retry", recoveryCalls)
				}
			})
		}
	}
}

func TestAppACLR2FinalizeDownstreamNonRetryableFailuresPreserveIdentityWithoutReplay(t *testing.T) {
	for _, stage := range appACLR2FinalizeDownstreamFailureStages() {
		t.Run(stage.name, func(t *testing.T) {
			fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
			failure := &pgconn.PgError{Code: "23505", Message: "non-retryable " + stage.name}
			if !stage.commit {
				injectAppACLR2FinalizeStageFailures(t, &dependencies, stage.name, failure, 1)
			}
			recoveryCalls := 0
			dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
				recoveryCalls++
				return AppACLR2StateFinalized, nil
			}

			transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, 1)
			options := make([]pgx.TxOptions, 0, 1)
			err := finalizeAppACLR2WithDependencies(t.Context(), func(_ context.Context, got pgx.TxOptions) (pgx.Tx, error) {
				options = append(options, got)
				tx := &fakeAppACLR2FinalizeRunTx{}
				if stage.commit {
					tx.commitErr = failure
				}
				transactions = append(transactions, tx)
				return tx, nil
			}, dependencies)
			if !errors.Is(err, failure) {
				t.Fatalf("finalizeAppACLR2WithDependencies() error = %v, want preserved non-retryable error %v", err, failure)
			}
			if len(transactions) != 1 {
				t.Fatalf("transaction attempts = %d, want no replay", len(transactions))
			}
			assertAppACLR2FinalizeSerializableReadWriteOptions(t, options, 1)
			wantCommits := 0
			if stage.commit {
				wantCommits = 1
			}
			if transactions[0].rollbacks != 1 || transactions[0].commits != wantCommits {
				t.Fatalf("transaction finishes = %d rollbacks, %d commits, want 1/%d", transactions[0].rollbacks, transactions[0].commits, wantCommits)
			}
			for _, label := range []string{"search-path", "actor", "locks", "finalizer-ddl", stage.traceLabel} {
				if got := appACLR2FinalizeTraceCount(fixture.trace, label); got != 1 {
					t.Fatalf("trace = %#v, %s count = %d, want one non-replayed closure", fixture.trace, label, got)
				}
			}
			if recoveryCalls != 0 {
				t.Fatalf("acknowledgement recovery calls = %d, want zero for definite PostgreSQL failure", recoveryCalls)
			}
		})
	}
}

func TestAppACLR2FinalizeAcknowledgementLossAcceptsOnlyFinalizedWithoutReplay(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	uncertainCommit := errors.New("connection closed after COMMIT")
	recoveryCalls := 0
	dependencies.safeToRetry = func(error) bool { return false }
	dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
		recoveryCalls++
		return AppACLR2StateFinalized, nil
	}
	transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, 1)
	err := finalizeAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		tx := &fakeAppACLR2FinalizeRunTx{commitErr: uncertainCommit}
		transactions = append(transactions, tx)
		return tx, nil
	}, dependencies)
	if err != nil {
		t.Fatalf("finalizeAppACLR2WithDependencies() error = %v, want acknowledgement recovery success", err)
	}
	if len(transactions) != 1 || transactions[0].commits != 1 || transactions[0].rollbacks != 1 {
		t.Fatalf("transaction finishes = %#v, want one uncertain commit then rollback", transactions)
	}
	if recoveryCalls != 1 {
		t.Fatalf("acknowledgement recovery calls = %d, want one", recoveryCalls)
	}
	if got := appACLR2FinalizeTraceCount(fixture.trace, "finalizer-ddl"); got != 1 {
		t.Fatalf("trace = %#v, M2 DDL count = %d, want no replay", fixture.trace, got)
	}
}

func TestAppACLR2FinalizeAcknowledgementRecoveryPreparedRetriesWholeClosure(t *testing.T) {
	fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	uncertainCommit := errors.New("connection closed after COMMIT")
	recoveryCalls := 0
	dependencies.safeToRetry = func(error) bool { return false }
	dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
		recoveryCalls++
		return AppACLR2StatePrepared, nil
	}
	transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, 2)
	err := finalizeAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		tx := &fakeAppACLR2FinalizeRunTx{}
		if len(transactions) == 0 {
			tx.commitErr = uncertainCommit
		}
		transactions = append(transactions, tx)
		return tx, nil
	}, dependencies)
	if err != nil {
		t.Fatalf("finalizeAppACLR2WithDependencies() error = %v", err)
	}
	if len(transactions) != 2 || recoveryCalls != 1 {
		t.Fatalf("transactions=%d recoveryCalls=%d, want two transactions and one recovery", len(transactions), recoveryCalls)
	}
	if got := appACLR2FinalizeTraceCount(fixture.trace, "finalizer-ddl"); got != 2 {
		t.Fatalf("trace = %#v, M2 DDL count = %d, want a whole-closure retry", fixture.trace, got)
	}
}

func TestAppACLR2FinalizeAcknowledgementRecoveryPreparedExhaustsAttempts(t *testing.T) {
	_, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	uncertainCommit := errors.New("connection closed after COMMIT")
	recoveryCalls := 0
	dependencies.safeToRetry = func(error) bool { return false }
	dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
		recoveryCalls++
		return AppACLR2StatePrepared, nil
	}
	transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, appACLR2BootstrapMaxAttempts)
	err := finalizeAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		tx := &fakeAppACLR2FinalizeRunTx{commitErr: uncertainCommit}
		transactions = append(transactions, tx)
		return tx, nil
	}, dependencies)
	if err == nil || !strings.Contains(err.Error(), "remained at exact PREPARED") {
		t.Fatalf("finalizeAppACLR2WithDependencies() error = %v, want exhausted exact-PREPARED recovery error", err)
	}
	if len(transactions) != appACLR2BootstrapMaxAttempts || recoveryCalls != appACLR2BootstrapMaxAttempts {
		t.Fatalf("transactions=%d recoveryCalls=%d, want %d exhausted attempts", len(transactions), recoveryCalls, appACLR2BootstrapMaxAttempts)
	}
}

func TestAppACLR2FinalizeAcknowledgementRecoveryPropagatesErrorBeforeState(t *testing.T) {
	_, dependencies := newAppACLR2FinalizePreMutationFixture(t)
	uncertainCommit := errors.New("connection closed after COMMIT")
	recoveryErr := errors.New("recovery cannot read M2 evidence")
	dependencies.safeToRetry = func(error) bool { return false }
	dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
		return AppACLR2StateCorrupt, recoveryErr
	}
	transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, 1)
	err := finalizeAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		tx := &fakeAppACLR2FinalizeRunTx{commitErr: uncertainCommit}
		transactions = append(transactions, tx)
		return tx, nil
	}, dependencies)
	if !errors.Is(err, recoveryErr) {
		t.Fatalf("finalizeAppACLR2WithDependencies() error = %v, want recovery error %v", err, recoveryErr)
	}
	if len(transactions) != 1 {
		t.Fatalf("transactions = %d, want error-first no-replay behavior", len(transactions))
	}
}

func TestAppACLR2FinalizeRetryableAcknowledgementRecoveryErrorsUseOnlyRemainingWholeClosureAttempts(t *testing.T) {
	for _, tt := range []struct {
		name string
		code string
	}{
		{name: "serialization_failure", code: "40001"},
		{name: "deadlock_detected", code: "40P01"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture, dependencies := newAppACLR2FinalizePreMutationFixture(t)
			preflightFailure := &pgconn.PgError{Code: tt.code, Message: "preflight retry"}
			recoveryFailure := &pgconn.PgError{Code: tt.code, Message: "acknowledgement recovery retry"}
			classifierCalls := 0
			dependencies.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
				classifierCalls++
				fixture.trace = append(fixture.trace, "classifier")
				if classifierCalls == 1 {
					return AppACLR2StateCorrupt, preflightFailure
				}
				return AppACLR2StatePrepared, nil
			}
			dependencies.safeToRetry = func(error) bool { return false }
			recoveryCalls := 0
			dependencies.recoverCommitAcknowledgement = func(context.Context) (AppACLR2State, error) {
				recoveryCalls++
				return AppACLR2StateCorrupt, recoveryFailure
			}

			transactions := make([]*fakeAppACLR2FinalizeRunTx, 0, appACLR2BootstrapMaxAttempts)
			err := finalizeAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				tx := &fakeAppACLR2FinalizeRunTx{}
				if len(transactions) == 1 {
					tx.commitErr = errors.New("connection closed after COMMIT")
				}
				transactions = append(transactions, tx)
				return tx, nil
			}, dependencies)
			if err != nil {
				t.Fatalf("finalizeAppACLR2WithDependencies() error = %v, want final remaining attempt to succeed", err)
			}
			if len(transactions) != appACLR2BootstrapMaxAttempts {
				t.Fatalf("transaction attempts = %d, want one preflight retry, one recovery retry, and one success", len(transactions))
			}
			if recoveryCalls != 1 {
				t.Fatalf("acknowledgement recovery calls = %d, want one", recoveryCalls)
			}
			if got := appACLR2FinalizeTraceCount(fixture.trace, "finalizer-ddl"); got != 2 {
				t.Fatalf("trace = %#v, M2 DDL count = %d, want only the two post-preflight whole closures", fixture.trace, got)
			}
		})
	}
}

func TestAppACLR2FinalizeProductionContinuityReadsStayInsideConstrainedCatalogPath(t *testing.T) {
	frozen := validFrozenAppACLR1StateFixture(t)
	newConstrainedTx := func() *appACLR2FinalizeConstrainedCatalogTx {
		return &appACLR2FinalizeConstrainedCatalogTx{scriptedAppACLR2ReceiptTx: newScriptedAppACLR2BootstrapCatalogTx()}
	}
	readContinuity := func(ctx context.Context, tx pgx.Tx, state AppACLR2State) (AppACLR2State, error) {
		if _, err := ReadAppACLR2PostBootstrapCatalogSnapshotInTx(ctx, tx, frozen); err != nil {
			return AppACLR2StateCorrupt, err
		}
		return state, nil
	}
	assertConstrained := func(t *testing.T, tx *appACLR2FinalizeConstrainedCatalogTx) {
		t.Helper()
		if len(tx.deniedQueries) != 0 {
			t.Fatalf("constrained finalizer path attempted bootstrap-only queries: %#v", tx.deniedQueries)
		}
		if len(tx.queryTexts)+len(tx.queryRowTexts) == 0 {
			t.Fatal("constrained finalizer path did not execute the production post-bootstrap catalog reader")
		}
	}

	t.Run("prepared preflight", func(t *testing.T) {
		tx := newConstrainedTx()
		_, dependencies := newAppACLR2FinalizePreMutationFixture(t)
		dependencies.classify = func(ctx context.Context, gotTx pgx.Tx) (AppACLR2State, error) {
			return readContinuity(ctx, gotTx, AppACLR2StatePrepared)
		}
		if err := finalizeAppACLR2InTx(t.Context(), tx, dependencies); err != nil {
			t.Fatalf("finalizeAppACLR2InTx() constrained preflight error = %v", err)
		}
		assertConstrained(t, tx)
	})

	t.Run("finalized readback", func(t *testing.T) {
		tx := newConstrainedTx()
		_, dependencies := newAppACLR2FinalizePreMutationFixture(t)
		dependencies.readFinalized = func(ctx context.Context, gotTx pgx.Tx) error {
			state, err := readContinuity(ctx, gotTx, AppACLR2StateFinalized)
			if err != nil {
				return err
			}
			if state != AppACLR2StateFinalized {
				return fmt.Errorf("constrained readback state = %v", state)
			}
			return nil
		}
		if err := finalizeAppACLR2InTx(t.Context(), tx, dependencies); err != nil {
			t.Fatalf("finalizeAppACLR2InTx() constrained readback error = %v", err)
		}
		assertConstrained(t, tx)
	})

	t.Run("acknowledgement recovery", func(t *testing.T) {
		tx := newConstrainedTx()
		dependencies := appACLR2FinalizeACKDependencies{
			hardenSearchPath:           func(context.Context, pgx.Tx) error { return nil },
			requireDirectMigratorActor: func(context.Context, pgx.Tx) error { return nil },
			lockStateTables:            func(context.Context, pgx.Tx) error { return nil },
			classify: func(ctx context.Context, gotTx pgx.Tx) (AppACLR2State, error) {
				return readContinuity(ctx, gotTx, AppACLR2StateFinalized)
			},
		}
		state, err := observeAppACLR2FinalizeACKRecoveryInTxWithDependencies(t.Context(), tx, dependencies)
		if err != nil || state != AppACLR2StateFinalized {
			t.Fatalf("constrained acknowledgement recovery state/error = %v/%v, want FINALIZED/nil", state, err)
		}
		assertConstrained(t, tx)
	})
}

func TestRequireAppACLR2DirectMigratorActorRequiresExactConstrainedDirectIdentity(t *testing.T) {
	tests := []struct {
		name string
		row  appACLR2FinalizeActorRow
		want bool
	}{
		{name: "exact direct migrator", row: appACLR2FinalizeActorRow{sessionUser: "direct_migrator", currentUser: "direct_migrator", expectedMigrator: "direct_migrator", login: true}, want: false},
		{name: "set role", row: appACLR2FinalizeActorRow{sessionUser: "other_role", currentUser: "direct_migrator", expectedMigrator: "direct_migrator", login: true}, want: true},
		{name: "wrong direct role", row: appACLR2FinalizeActorRow{sessionUser: "other_role", currentUser: "other_role", expectedMigrator: "direct_migrator", login: true}, want: true},
		{name: "inheriting role", row: appACLR2FinalizeActorRow{sessionUser: "direct_migrator", currentUser: "direct_migrator", expectedMigrator: "direct_migrator", login: true, inherit: true}, want: true},
		{name: "superuser", row: appACLR2FinalizeActorRow{sessionUser: "direct_migrator", currentUser: "direct_migrator", expectedMigrator: "direct_migrator", login: true, superuser: true}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeAppACLR2FinalizeActorTx{row: tt.row}
			err := requireAppACLR2DirectMigratorActorInTx(t.Context(), tx)
			if (err != nil) != tt.want {
				t.Fatalf("requireAppACLR2DirectMigratorActorInTx() error = %v, want error=%t", err, tt.want)
			}
		})
	}
}

func TestAppACLR2FinalizeStateTableLockOrderIncludesPresentM2Relations(t *testing.T) {
	tx := &fakeAppACLR2FinalizeLockTx{receipt: true, m2Revisions: true, m2Head: true}
	if err := lockAppACLR2FinalizeStateTablesInTx(t.Context(), tx); err != nil {
		t.Fatalf("lockAppACLR2FinalizeStateTablesInTx() error = %v", err)
	}
	want := []string{
		"lock table public.app_acl_manifest_head in share row exclusive mode",
		"lock table public.app_acl_manifest_revisions in share row exclusive mode",
		"lock table public.record_platform_domain_identity in share row exclusive mode",
		"lock table public.schema_migrations in share row exclusive mode",
		"lock table public.app_acl_r2_bootstrap_receipt in access share mode",
		"lock table public.app_acl_r2_manifest_revisions in share row exclusive mode",
		"lock table public.app_acl_r2_manifest_head in share row exclusive mode",
	}
	if !reflect.DeepEqual(tx.execSQL, want) {
		t.Fatalf("finalizer state locks = %#v, want %#v", tx.execSQL, want)
	}
}

func TestAppACLR2FinalizeStateTableLockOrderSkipsAbsentReceiptAndM2Relations(t *testing.T) {
	tx := &fakeAppACLR2FinalizeLockTx{}
	if err := lockAppACLR2FinalizeStateTablesInTx(t.Context(), tx); err != nil {
		t.Fatalf("lockAppACLR2FinalizeStateTablesInTx() error = %v", err)
	}
	want := []string{
		"lock table public.app_acl_manifest_head in share row exclusive mode",
		"lock table public.app_acl_manifest_revisions in share row exclusive mode",
		"lock table public.record_platform_domain_identity in share row exclusive mode",
		"lock table public.schema_migrations in share row exclusive mode",
	}
	if !reflect.DeepEqual(tx.execSQL, want) {
		t.Fatalf("finalizer state locks = %#v, want %#v", tx.execSQL, want)
	}
}

func TestAppACLR2FinalizeRollsBackEveryPostDDLError(t *testing.T) {
	tests := []struct {
		name   string
		inject func(*appACLR2FinalizeDependencies, error)
	}{
		{
			name: "finalizer_ddl",
			inject: func(dependencies *appACLR2FinalizeDependencies, failure error) {
				dependencies.executeFinalizeSection = func(context.Context, pgx.Tx) error { return failure }
			},
		},
		{
			name: "finalizer_dcl",
			inject: func(dependencies *appACLR2FinalizeDependencies, failure error) {
				dependencies.applyM2ControlACL = func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) error { return failure }
			},
		},
		{
			name: "m2_revision",
			inject: func(dependencies *appACLR2FinalizeDependencies, failure error) {
				dependencies.insertM2Revision = func(context.Context, pgx.Tx, AppACLManifestR2V1, []byte, [32]byte) error { return failure }
			},
		},
		{
			name: "m2_head",
			inject: func(dependencies *appACLR2FinalizeDependencies, failure error) {
				dependencies.compareAndSwapM2Head = func(context.Context, pgx.Tx, AppACLManifestR2V1, [32]byte) error { return failure }
			},
		},
		{
			name: "finalized_readback",
			inject: func(dependencies *appACLR2FinalizeDependencies, failure error) {
				dependencies.readFinalized = func(context.Context, pgx.Tx) error { return failure }
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, dependencies := newAppACLR2FinalizePreMutationFixture(t)
			failure := errors.New(tt.name)
			tt.inject(&dependencies, failure)
			tx := &fakeAppACLR2FinalizeRunTx{}
			err := finalizeAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				return tx, nil
			}, dependencies)
			if !errors.Is(err, failure) {
				t.Fatalf("finalizeAppACLR2WithDependencies() error = %v, want %v", err, failure)
			}
			if tx.rollbacks != 1 || tx.commits != 0 {
				t.Fatalf("transaction finishes = %d rollbacks, %d commits, want rollback only", tx.rollbacks, tx.commits)
			}
		})
	}
}

func TestAppACLR2FinalizeRejectsStaleFrozenM1RevisionCAS(t *testing.T) {
	fixture, _ := newAppACLR2FinalizePreMutationFixture(t)
	manifest, body, digest, err := compileAppACLR2FinalizeManifest(fixture.frozen, fixture.receiptRow)
	if err != nil {
		t.Fatalf("compileAppACLR2FinalizeManifest() error = %v", err)
	}
	tx := &fakeAppACLR2FinalizeWriteTx{tags: []pgconn.CommandTag{pgconn.NewCommandTag("INSERT 0 0")}}
	if err := insertAppACLR2FinalizeM2RevisionInTx(t.Context(), tx, manifest, body, digest); err == nil {
		t.Fatal("insertAppACLR2FinalizeM2RevisionInTx() error = nil, want stale frozen-M1 CAS rejection")
	}
}

func TestAppACLR2FinalizeRejects205And207TupleM2BodiesBeforeInsert(t *testing.T) {
	fixture, _ := newAppACLR2FinalizePreMutationFixture(t)
	manifest, body, digest, err := compileAppACLR2FinalizeManifest(fixture.frozen, fixture.receiptRow)
	if err != nil {
		t.Fatalf("compileAppACLR2FinalizeManifest() error = %v", err)
	}
	bindings := appACLR2PrivilegeVectorBindings()
	tuples := appACLR2PrivilegeVectorTuples("houfeng_app")
	for _, tt := range []struct {
		name string
		body []byte
	}{
		{name: "205_tuples", body: rawAppACLR2PrivilegeBody(bindings, tuples[:205], 3, 205)},
		{name: "207_tuples", body: rawAppACLR2PrivilegeBody(bindings, append(tuples, tuples[len(tuples)-1]), 3, 207)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			malformed := manifest
			malformed.R2PrivilegeSetBody = tt.body
			malformed.R2PrivilegeSetDigest = sha256.Sum256(tt.body)
			tx := &fakeAppACLR2FinalizeWriteTx{}
			if err := insertAppACLR2FinalizeM2RevisionInTx(t.Context(), tx, malformed, body, digest); err == nil {
				t.Fatal("insertAppACLR2FinalizeM2RevisionInTx() error = nil, want non-206 M2 rejection")
			}
			if len(tx.execSQL) != 0 {
				t.Fatalf("non-206 M2 body executed %d writes, want zero", len(tx.execSQL))
			}
		})
	}
}

type appACLR2FinalizeDownstreamFailureStage struct {
	name       string
	traceLabel string
	commit     bool
}

func appACLR2FinalizeDownstreamFailureStages() []appACLR2FinalizeDownstreamFailureStage {
	return []appACLR2FinalizeDownstreamFailureStage{
		{name: "finalizer_ddl", traceLabel: "finalizer-ddl"},
		{name: "m2_revision", traceLabel: "m2-revision-insert"},
		{name: "m2_head", traceLabel: "m2-head-cas"},
		{name: "finalizer_dcl", traceLabel: "finalizer-dcl"},
		{name: "finalized_readback", traceLabel: "post-write-readback"},
		{name: "commit", traceLabel: "post-write-readback", commit: true},
	}
}

func injectAppACLR2FinalizeStageFailures(
	t *testing.T,
	dependencies *appACLR2FinalizeDependencies,
	stage string,
	failure error,
	failures int,
) {
	t.Helper()
	shouldFail := func() bool {
		if failures <= 0 {
			return false
		}
		failures--
		return true
	}
	switch stage {
	case "finalizer_ddl":
		original := dependencies.executeFinalizeSection
		dependencies.executeFinalizeSection = func(ctx context.Context, tx pgx.Tx) error {
			if err := original(ctx, tx); err != nil {
				return err
			}
			if shouldFail() {
				return failure
			}
			return nil
		}
	case "m2_revision":
		original := dependencies.insertM2Revision
		dependencies.insertM2Revision = func(ctx context.Context, tx pgx.Tx, manifest AppACLManifestR2V1, body []byte, digest [32]byte) error {
			if err := original(ctx, tx, manifest, body, digest); err != nil {
				return err
			}
			if shouldFail() {
				return failure
			}
			return nil
		}
	case "m2_head":
		original := dependencies.compareAndSwapM2Head
		dependencies.compareAndSwapM2Head = func(ctx context.Context, tx pgx.Tx, manifest AppACLManifestR2V1, digest [32]byte) error {
			if err := original(ctx, tx, manifest, digest); err != nil {
				return err
			}
			if shouldFail() {
				return failure
			}
			return nil
		}
	case "finalizer_dcl":
		original := dependencies.applyM2ControlACL
		dependencies.applyM2ControlACL = func(ctx context.Context, tx pgx.Tx, frozen FrozenAppACLR1StateV1) error {
			if err := original(ctx, tx, frozen); err != nil {
				return err
			}
			if shouldFail() {
				return failure
			}
			return nil
		}
	case "finalized_readback":
		original := dependencies.readFinalized
		dependencies.readFinalized = func(ctx context.Context, tx pgx.Tx) error {
			if err := original(ctx, tx); err != nil {
				return err
			}
			if shouldFail() {
				return failure
			}
			return nil
		}
	default:
		t.Fatalf("unsupported APP ACL R2 finalizer downstream failure stage %q", stage)
	}
}

func assertAppACLR2FinalizeSerializableReadWriteOptions(t *testing.T, options []pgx.TxOptions, want int) {
	t.Helper()
	if len(options) != want {
		t.Fatalf("transaction options captured = %d, want %d", len(options), want)
	}
	wantOptions := pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadWrite}
	for attempt, got := range options {
		if !reflect.DeepEqual(got, wantOptions) {
			t.Fatalf("transaction attempt %d options = %#v, want %#v", attempt+1, got, wantOptions)
		}
	}
}

type appACLR2FinalizePreMutationFixture struct {
	trace      []string
	predicates AppACLR2CatalogPredicates
	frozen     FrozenAppACLR1StateV1
	receipt    AppACLR2BootstrapReceiptV1
	receiptRow appACLR2ReceiptRowV1
}

func newAppACLR2FinalizePreMutationFixture(t *testing.T) (*appACLR2FinalizePreMutationFixture, appACLR2FinalizeDependencies) {
	t.Helper()
	frozen := validFrozenAppACLR1StateFixture(t)
	receipt := validAppACLR2BootstrapReceiptFixture(t)
	fixture := &appACLR2FinalizePreMutationFixture{
		trace:  make([]string, 0, 16),
		frozen: frozen,
		predicates: AppACLR2CatalogPredicates{
			FrozenState: frozen,
			ExactL1M1:   true,
			ExactL2:     true,
			M2Absent:    true,
		},
		receipt: receipt,
	}
	fixture.setReceiptRow(t)

	dependencies := appACLR2FinalizeDependencies{
		hardenSearchPath: func(context.Context, pgx.Tx) error {
			fixture.trace = append(fixture.trace, "search-path")
			return nil
		},
		requireDirectMigratorActor: func(context.Context, pgx.Tx) error {
			fixture.trace = append(fixture.trace, "actor")
			return nil
		},
		lockStateTables: func(context.Context, pgx.Tx) error {
			fixture.trace = append(fixture.trace, "locks")
			return nil
		},
		classify: func(context.Context, pgx.Tx) (AppACLR2State, error) {
			fixture.trace = append(fixture.trace, "classifier")
			return AppACLR2StatePrepared, nil
		},
		readCatalogPredicates: func(context.Context, pgx.Tx) (AppACLR2CatalogPredicates, error) {
			fixture.trace = append(fixture.trace, "catalog-predicate")
			return fixture.predicates, nil
		},
		readReceipt: func(context.Context, pgx.Tx) (appACLR2ReceiptRowV1, error) {
			fixture.trace = append(fixture.trace, "receipt")
			return fixture.receiptRow, nil
		},
		preflightSourceEvidence: func() error {
			fixture.trace = append(fixture.trace, "source-preflight")
			return nil
		},
		executeFinalizeSection: func(context.Context, pgx.Tx) error {
			fixture.trace = append(fixture.trace, "finalizer-ddl")
			return nil
		},
		applyM2ControlACL: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) error {
			fixture.trace = append(fixture.trace, "finalizer-dcl")
			return nil
		},
		insertM2Revision: func(context.Context, pgx.Tx, AppACLManifestR2V1, []byte, [32]byte) error {
			fixture.trace = append(fixture.trace, "m2-revision-insert")
			return nil
		},
		compareAndSwapM2Head: func(context.Context, pgx.Tx, AppACLManifestR2V1, [32]byte) error {
			fixture.trace = append(fixture.trace, "m2-head-cas")
			return nil
		},
		readFinalized: func(context.Context, pgx.Tx) error {
			fixture.trace = append(fixture.trace, "post-write-readback")
			return nil
		},
		recoverCommitAcknowledgement: func(context.Context) (AppACLR2State, error) {
			return AppACLR2StateFinalized, nil
		},
		safeToRetry: func(error) bool { return false },
	}
	return fixture, dependencies
}

func (fixture *appACLR2FinalizePreMutationFixture) setReceiptRow(t *testing.T) {
	t.Helper()
	body, err := CanonicalAppACLR2BootstrapReceiptBodyV1(fixture.receipt)
	if err != nil {
		t.Fatalf("encode finalizer receipt fixture: %v", err)
	}
	fixture.receiptRow = appACLR2ReceiptRowV1{
		Singleton: true,
		Body:      body,
		Digest:    sha256.Sum256(body),
	}
}

func assertAppACLR2FinalizePreMutationOnly(t *testing.T, trace []string) {
	t.Helper()
	assertAppACLR2FinalizeTraceAbsent(t, trace,
		"finalizer-ddl", "finalizer-dcl", "m2-revision-insert", "m2-head-cas", "post-write-readback",
	)
}

func assertAppACLR2FinalizeTraceOrder(t *testing.T, trace []string, labels ...string) {
	t.Helper()
	last := -1
	for _, label := range labels {
		index := -1
		for candidate := last + 1; candidate < len(trace); candidate++ {
			if trace[candidate] == label {
				index = candidate
				break
			}
		}
		if index < 0 {
			t.Fatalf("trace = %#v, missing ordered label %q", trace, label)
		}
		last = index
	}
}

func assertAppACLR2FinalizeTraceAbsent(t *testing.T, trace []string, labels ...string) {
	t.Helper()
	for _, label := range labels {
		for _, value := range trace {
			if value == label {
				t.Fatalf("trace = %#v, unexpectedly contains %q", trace, label)
			}
		}
	}
}

type appACLR2FinalizeConstrainedCatalogTx struct {
	*scriptedAppACLR2ReceiptTx
	deniedQueries []string
}

func (tx *appACLR2FinalizeConstrainedCatalogTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if appACLR2FinalizeBootstrapOnlyProbe(sql) {
		tx.deniedQueries = append(tx.deniedQueries, sql)
		return nil, &pgconn.PgError{Code: "42501", Message: "permission denied for bootstrap-only PostgreSQL probe"}
	}
	return tx.scriptedAppACLR2ReceiptTx.Query(ctx, sql, args...)
}

func (tx *appACLR2FinalizeConstrainedCatalogTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if appACLR2FinalizeBootstrapOnlyProbe(sql) {
		tx.deniedQueries = append(tx.deniedQueries, sql)
		return scriptedAppACLR2ReceiptRow{err: &pgconn.PgError{Code: "42501", Message: "permission denied for bootstrap-only PostgreSQL probe"}}
	}
	return tx.scriptedAppACLR2ReceiptTx.QueryRow(ctx, sql, args...)
}

func appACLR2FinalizeBootstrapOnlyProbe(sql string) bool {
	normalized := strings.ToLower(sql)
	for _, forbidden := range []string{"pg_control_system()", "from pg_catalog.pg_control_", "pg_read_file(", "pg_read_binary_file(", "pg_stat_file(", "pg_ls_dir("} {
		if strings.Contains(normalized, forbidden) {
			return true
		}
	}
	return false
}

type fakeAppACLR2FinalizeTx struct {
	pgx.Tx
}

type fakeAppACLR2FinalizeDefaultACKTx struct {
	*appACLR2PublicR1Tx
	receiptPresent     bool
	m2RevisionsPresent bool
	m2HeadPresent      bool
	actorCalls         int
	presenceCalls      int
	commits            int
	rollbacks          int
	lockSQL            []string
}

func (tx *fakeAppACLR2FinalizeDefaultACKTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if strings.HasPrefix(strings.TrimSpace(sql), "lock table ") {
		tx.lockSQL = append(tx.lockSQL, sql)
		return pgconn.NewCommandTag("LOCK TABLE"), nil
	}
	if strings.EqualFold(strings.TrimSpace(sql), "set local search_path = pg_catalog, public") {
		tx.execSQL = append(tx.execSQL, sql)
		tx.searchPath = "pg_catalog, public"
		return pgconn.NewCommandTag("SET"), nil
	}
	return tx.appACLR2PublicR1Tx.Exec(ctx, sql, arguments...)
}

func (tx *fakeAppACLR2FinalizeDefaultACKTx) QueryRow(ctx context.Context, query string, arguments ...any) pgx.Row {
	switch {
	case strings.Contains(query, "session_user") && strings.Contains(query, "migrator_catalog_role"):
		tx.actorCalls++
		return appACLR2FinalizeActorRow{
			sessionUser:      "direct_migrator",
			currentUser:      "direct_migrator",
			expectedMigrator: "direct_migrator",
			login:            true,
		}
	case strings.Contains(query, "to_regclass('public.app_acl_r2_bootstrap_receipt')"):
		tx.presenceCalls++
		return appACLR2FinalizeBoolTripleRow{
			first:  tx.receiptPresent,
			second: tx.m2RevisionsPresent,
			third:  tx.m2HeadPresent,
		}
	default:
		return tx.appACLR2PublicR1Tx.QueryRow(ctx, query, arguments...)
	}
}

func (tx *fakeAppACLR2FinalizeDefaultACKTx) Commit(context.Context) error {
	tx.commits++
	return nil
}

func (tx *fakeAppACLR2FinalizeDefaultACKTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type fakeAppACLR2FinalizeDCLTx struct {
	fakeAppACLR2BootstrapTx
	bootstrapRole string
}

func (tx *fakeAppACLR2FinalizeDCLTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if !strings.Contains(query, "where role.oid = 10") {
		return appACLR2BootstrapErrorRow{err: fmt.Errorf("unexpected finalizer DCL query %q", query)}
	}
	return appACLR2FinalizeStringRow(tx.bootstrapRole)
}

type appACLR2FinalizeStringRow string

func (row appACLR2FinalizeStringRow) Scan(destinations ...any) error {
	if len(destinations) != 1 {
		return fmt.Errorf("finalizer string row destination count = %d", len(destinations))
	}
	destination, ok := destinations[0].(*string)
	if !ok {
		return fmt.Errorf("finalizer string row destination has type %T", destinations[0])
	}
	*destination = string(row)
	return nil
}

type fakeAppACLR2FinalizeWriteTx struct {
	fakeAppACLR2BootstrapTx
	tags  []pgconn.CommandTag
	index int
}

func (tx *fakeAppACLR2FinalizeWriteTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if _, err := tx.fakeAppACLR2BootstrapTx.Exec(ctx, sql, arguments...); err != nil {
		return pgconn.CommandTag{}, err
	}
	if tx.index >= len(tx.tags) {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected finalizer write execution %d", tx.index)
	}
	tag := tx.tags[tx.index]
	tx.index++
	return tag, nil
}

type fakeAppACLR2FinalizeRunTx struct {
	pgx.Tx
	rollbacks int
	commits   int
	commitErr error
}

type fakeAppACLR2FinalizeActorTx struct {
	pgx.Tx
	row appACLR2FinalizeActorRow
}

type fakeAppACLR2FinalizeLockTx struct {
	fakeAppACLR2BootstrapTx
	receipt     bool
	m2Revisions bool
	m2Head      bool
}

func (tx *fakeAppACLR2FinalizeLockTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if !strings.Contains(query, "to_regclass") {
		return appACLR2BootstrapErrorRow{err: fmt.Errorf("unexpected finalizer lock query %q", query)}
	}
	return appACLR2FinalizeBoolTripleRow{first: tx.receipt, second: tx.m2Revisions, third: tx.m2Head}
}

type appACLR2FinalizeBoolTripleRow struct {
	first  bool
	second bool
	third  bool
}

func (row appACLR2FinalizeBoolTripleRow) Scan(destinations ...any) error {
	if len(destinations) != 3 {
		return fmt.Errorf("finalizer lock destination count = %d, want three", len(destinations))
	}
	first, firstOK := destinations[0].(*bool)
	second, secondOK := destinations[1].(*bool)
	third, thirdOK := destinations[2].(*bool)
	if !firstOK || !secondOK || !thirdOK {
		return fmt.Errorf("finalizer lock destinations = %T/%T/%T, want *bool/*bool/*bool", destinations[0], destinations[1], destinations[2])
	}
	*first = row.first
	*second = row.second
	*third = row.third
	return nil
}

func (tx *fakeAppACLR2FinalizeActorTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	if !strings.Contains(query, "session_user") || !strings.Contains(query, "migrator_catalog_role") {
		return appACLR2BootstrapErrorRow{err: fmt.Errorf("unexpected finalizer actor query %q", query)}
	}
	return tx.row
}

type appACLR2FinalizeActorRow struct {
	sessionUser      string
	currentUser      string
	expectedMigrator string
	login            bool
	inherit          bool
	superuser        bool
	createDatabase   bool
	createRole       bool
	replication      bool
	bypassRLS        bool
}

func (row appACLR2FinalizeActorRow) Scan(destinations ...any) error {
	values := []any{
		row.sessionUser,
		row.currentUser,
		row.login,
		row.inherit,
		row.superuser,
		row.createDatabase,
		row.createRole,
		row.replication,
		row.bypassRLS,
		row.expectedMigrator,
	}
	if len(destinations) != len(values) {
		return fmt.Errorf("finalizer actor destination count = %d, want %d", len(destinations), len(values))
	}
	for index, value := range values {
		switch destination := destinations[index].(type) {
		case *string:
			got, ok := value.(string)
			if !ok {
				return fmt.Errorf("finalizer actor destination %d has string type mismatch", index)
			}
			*destination = got
		case *bool:
			got, ok := value.(bool)
			if !ok {
				return fmt.Errorf("finalizer actor destination %d has boolean type mismatch", index)
			}
			*destination = got
		default:
			return fmt.Errorf("finalizer actor destination %d has type %T", index, destinations[index])
		}
	}
	return nil
}

func (tx *fakeAppACLR2FinalizeRunTx) Commit(context.Context) error {
	tx.commits++
	return tx.commitErr
}

func (tx *fakeAppACLR2FinalizeRunTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

func appACLR2FinalizeTraceCount(trace []string, label string) int {
	count := 0
	for _, value := range trace {
		if value == label {
			count++
		}
	}
	return count
}

func appACLR2FinalizeCanonicalSection(t *testing.T) []byte {
	t.Helper()
	payload, err := fs.ReadFile(appaclr2migrations.FS, appACLR2MigrationName)
	if err != nil {
		t.Fatalf("ReadFile(isolated APP ACL R2 migration) error = %v", err)
	}
	section, err := appACLR2SourceSection(payload, appACLR2FinalizeBeginMarker, appACLR2FinalizeEndMarker)
	if err != nil {
		t.Fatalf("appACLR2SourceSection(finalizer) error = %v", err)
	}
	return section
}

type appACLR2FinalizeInvalidSection struct {
	name     string
	section  []byte
	sameSize bool
}

func appACLR2InvalidFinalizeSections(t *testing.T, section []byte) []appACLR2FinalizeInvalidSection {
	t.Helper()
	body := appACLR2FinalizeSectionBody(t, section)
	if len(body) == 0 {
		t.Fatal("canonical finalizer body is empty")
	}

	wrongTopLevelDDL := bytes.Replace(
		append([]byte(nil), body...),
		[]byte("create trigger app_acl_r2_manifest_head_immutable"),
		[]byte("create table   app_acl_r2_manifest_head_immutable"),
		1,
	)
	if bytes.Equal(wrongTopLevelDDL, body) || len(wrongTopLevelDDL) != len(body) {
		t.Fatal("could not create same-size wrong top-level DDL cardinality fixture")
	}
	wrongFunctionShape := bytes.Replace(
		append([]byte(nil), body...),
		[]byte("returns trigger"),
		[]byte("returns triggeR"),
		1,
	)
	if bytes.Equal(wrongFunctionShape, body) || len(wrongFunctionShape) != len(body) {
		t.Fatal("could not create same-size wrong finalizer function-shape fixture")
	}

	return []appACLR2FinalizeInvalidSection{
		{
			name:    "too_small",
			section: appACLR2FinalizeSectionWithBody(t, section, body[:len(body)-1]),
		},
		{
			name:    "too_large",
			section: appACLR2FinalizeSectionWithBody(t, section, append(append([]byte(nil), body...), '\n')),
		},
		{
			name:     "wrong_top_level_ddl_cardinality",
			section:  appACLR2FinalizeSectionWithBody(t, section, wrongTopLevelDDL),
			sameSize: true,
		},
		{
			name:     "wrong_function_shape",
			section:  appACLR2FinalizeSectionWithBody(t, section, wrongFunctionShape),
			sameSize: true,
		},
	}
}

func appACLR2FinalizeSectionBody(t *testing.T, section []byte) []byte {
	t.Helper()
	beginLine := []byte(appACLR2FinalizeBeginMarker + "\n")
	endLine := []byte("-- " + appACLR2FinalizeEndMarker + "\n")
	if !bytes.HasPrefix(section, beginLine) || !bytes.HasSuffix(section, endLine) {
		t.Fatalf("finalizer section does not retain the canonical marker slice shape")
	}
	return append([]byte(nil), section[len(beginLine):len(section)-len(endLine)]...)
}

func appACLR2FinalizeSectionWithBody(t *testing.T, section, body []byte) []byte {
	t.Helper()
	beginLine := []byte(appACLR2FinalizeBeginMarker + "\n")
	endLine := []byte("-- " + appACLR2FinalizeEndMarker + "\n")
	if !bytes.HasPrefix(section, beginLine) || !bytes.HasSuffix(section, endLine) {
		t.Fatalf("finalizer section does not retain the canonical marker slice shape")
	}
	result := append([]byte(nil), beginLine...)
	result = append(result, body...)
	return append(result, endLine...)
}
