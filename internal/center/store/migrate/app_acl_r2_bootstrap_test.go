package migrate

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestBootstrapAppACLR2OrdersActorInventoryBeforeClassifier(t *testing.T) {
	trace := make([]string, 0, 16)
	tx := &fakeAppACLR2BootstrapTx{}
	deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StateR1, AppACLR2StatePrepared})

	err := bootstrapAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		return tx, nil
	}, deps)
	if err != nil {
		t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v", err)
	}
	assertAppACLR2BootstrapTraceOrder(t, trace, "actor", "inventory", "classifier")
	if got := appACLR2BootstrapTraceCount(trace, "classifier"); got != 2 {
		t.Fatalf("classifier calls = %d, want exact R1 and post-write PREPARED proofs", got)
	}
}

func TestBootstrapAppACLR2RejectsM2OrUnknownInventoryWithoutClassifierOrM2Access(t *testing.T) {
	unknown := AppACLR2ReservedCatalogObjectV1{Kind: "relation", Schema: "public", OID: 1, Identity: "app_acl_r2_unknown", Detail: "r"}
	for _, tc := range []struct {
		name    string
		objects []AppACLR2ReservedCatalogObjectV1
	}{
		{name: "M2 relation", objects: []AppACLR2ReservedCatalogObjectV1{appACLR2M2RevisionsRelation()}},
		{name: "unknown reserved object", objects: []AppACLR2ReservedCatalogObjectV1{unknown}},
		{name: "bounded inventory overflow", objects: append(appACLR2BootstrapInventoryWithOIDs(appACLR2KnownReservedObjects()), AppACLR2ReservedCatalogObjectV1{Kind: "relation", Schema: "public", OID: 99, Identity: "app_acl_r2_extra", Detail: "r"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trace := make([]string, 0, 8)
			deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StateR1})
			deps.readReservedObjects = func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
				trace = append(trace, "inventory")
				return tc.objects, nil
			}
			classifierCalls := 0
			deps.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
				classifierCalls++
				return AppACLR2StateFinalized, nil
			}
			locks := 0
			deps.lockStateTables = func(context.Context, pgx.Tx, bool) error {
				locks++
				return nil
			}

			err := bootstrapAppACLR2WithDependencies(t.Context(), oneAppACLR2BootstrapTx, deps)
			if err == nil {
				t.Fatal("bootstrapAppACLR2WithDependencies() error = nil, want fail-closed inventory rejection")
			}
			assertAppACLR2BootstrapTraceOrder(t, trace, "actor", "inventory")
			if classifierCalls != 0 {
				t.Fatalf("classifier calls = %d, want zero after M2/unknown inventory", classifierCalls)
			}
			if locks != 0 {
				t.Fatalf("state/M2 table locks = %d, want zero after M2/unknown inventory", locks)
			}
			for _, forbidden := range []string{"preflight", "bootstrap-sql", "l2-acl", "receipt-surface", "compile-receipt", "insert-receipt", "m2-content", "m2-scan", "m2-aggregate", "finalized"} {
				if appACLR2BootstrapTraceCount(trace, forbidden) != 0 {
					t.Fatalf("trace = %#v, unexpectedly contains forbidden %q access", trace, forbidden)
				}
			}
		})
	}
}

func TestBootstrapAppACLR2ExactR1ClassifierResultContinuesToVerifierPreflightAndL2DDL(t *testing.T) {
	trace := make([]string, 0, 16)
	deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StateR1, AppACLR2StatePrepared})

	if err := bootstrapAppACLR2WithDependencies(t.Context(), oneAppACLR2BootstrapTx, deps); err != nil {
		t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v", err)
	}
	assertAppACLR2BootstrapTraceOrder(t, trace,
		"classifier", "verify-frozen", "preflight", "pre-mutation-evidence", "source-preflight", "bootstrap-sql", "l2-acl", "receipt-surface", "compile-receipt", "encode-receipt", "insert-receipt", "classifier",
	)
	for _, required := range []string{"verify-frozen", "preflight", "source-preflight", "bootstrap-sql", "l2-acl", "insert-receipt"} {
		if appACLR2BootstrapTraceCount(trace, required) != 1 {
			t.Fatalf("trace = %#v, want one %q call", trace, required)
		}
	}
}

func TestBootstrapAppACLR2RejectsInvalidPreMutationEvidenceBeforeL2Writes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*FrozenAppACLR1StateV1, *AppACLR2BootstrapCatalogSnapshotV1)
	}{
		{
			name: "frozen source evidence",
			mutate: func(frozen *FrozenAppACLR1StateV1, _ *AppACLR2BootstrapCatalogSnapshotV1) {
				frozen.SourceSetDigest[0] ^= 0xff
			},
		},
		{
			name: "PG16 catalog evidence",
			mutate: func(_ *FrozenAppACLR1StateV1, catalog *AppACLR2BootstrapCatalogSnapshotV1) {
				catalog.ServerVersionNum = 150014
			},
		},
		{
			name: "role membership",
			mutate: func(_ *FrozenAppACLR1StateV1, catalog *AppACLR2BootstrapCatalogSnapshotV1) {
				catalog.Roles[2].RecursiveMembershipCount = 1
			},
		},
		{
			name: "bootstrap default ACL",
			mutate: func(_ *FrozenAppACLR1StateV1, catalog *AppACLR2BootstrapCatalogSnapshotV1) {
				catalog.BootstrapDefaultACLCount = 1
			},
		},
		{
			name: "domain identity",
			mutate: func(_ *FrozenAppACLR1StateV1, catalog *AppACLR2BootstrapCatalogSnapshotV1) {
				catalog.Domains[0].DatabaseOID++
			},
		},
		{
			name: "pgcrypto member dependency",
			mutate: func(_ *FrozenAppACLR1StateV1, catalog *AppACLR2BootstrapCatalogSnapshotV1) {
				catalog.Members[0].ExtensionDependencyType = "n"
			},
		},
		{
			name: "pgcrypto member owner",
			mutate: func(_ *FrozenAppACLR1StateV1, catalog *AppACLR2BootstrapCatalogSnapshotV1) {
				catalog.Members[0].OwnerOID = 20
			},
		},
		{
			name: "equal cardinality member substitution",
			mutate: func(_ *FrozenAppACLR1StateV1, catalog *AppACLR2BootstrapCatalogSnapshotV1) {
				catalog.Members[0].Name = "substituted"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trace := make([]string, 0, 16)
			frozen := validFrozenAppACLR1StateFixture(t)
			catalog, _ := validAppACLR2CatalogSnapshotFixture(t, frozen)
			tc.mutate(&frozen, &catalog)
			deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StateR1, AppACLR2StatePrepared})
			deps.verifyFrozen = func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error) {
				trace = append(trace, "verify-frozen")
				return frozen, nil
			}
			deps.readBootstrapCatalog = func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLR2BootstrapCatalogSnapshotV1, error) {
				trace = append(trace, "preflight")
				return catalog, nil
			}
			deps.validatePreMutationEvidence = func(gotCatalog AppACLR2BootstrapCatalogSnapshotV1, gotFrozen FrozenAppACLR1StateV1) error {
				trace = append(trace, "pre-mutation-evidence")
				return validateAppACLR2BootstrapPreMutationEvidence(gotCatalog, gotFrozen)
			}
			deps.compileReceipt = CompileAppACLR2BootstrapReceiptFromCatalogV1

			err := bootstrapAppACLR2WithDependencies(t.Context(), oneAppACLR2BootstrapTx, deps)
			if err == nil {
				t.Fatal("bootstrapAppACLR2WithDependencies() error = nil, want invalid pre-mutation evidence rejection")
			}
			assertAppACLR2BootstrapTraceOrder(t, trace, "classifier", "verify-frozen", "preflight", "pre-mutation-evidence")
			assertAppACLR2BootstrapTraceAbsent(t, trace,
				"source-preflight", "bootstrap-sql", "l2-acl", "receipt-surface", "compile-receipt", "encode-receipt", "insert-receipt",
			)
			if got := appACLR2BootstrapTraceCount(trace, "classifier"); got != 1 {
				t.Fatalf("classifier calls = %d, want one pre-write exact-R1 classifier call", got)
			}
		})
	}
}

func TestBootstrapAppACLR2RejectsInvalidSourcePreflightBeforeL2Writes(t *testing.T) {
	trace := make([]string, 0, 16)
	sourceErr := errors.New("isolated R2 source set or marker evidence is invalid")
	deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StateR1, AppACLR2StatePrepared})
	deps.preflightSourceEvidence = func() error {
		trace = append(trace, "source-preflight")
		return sourceErr
	}

	err := bootstrapAppACLR2WithDependencies(t.Context(), oneAppACLR2BootstrapTx, deps)
	if err != sourceErr {
		t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v, want source preflight error %v", err, sourceErr)
	}
	assertAppACLR2BootstrapTraceOrder(t, trace,
		"classifier", "verify-frozen", "preflight", "pre-mutation-evidence", "source-preflight",
	)
	assertAppACLR2BootstrapTraceAbsent(t, trace,
		"bootstrap-sql", "l2-acl", "receipt-surface", "compile-receipt", "encode-receipt", "insert-receipt",
	)
	if got := appACLR2BootstrapTraceCount(trace, "classifier"); got != 1 {
		t.Fatalf("classifier calls = %d, want one pre-write exact-R1 classifier call", got)
	}
}

func TestDefaultAppACLR2BootstrapSourcePreflightUsesCompilerAndMarkerEvidence(t *testing.T) {
	deps := defaultAppACLR2BootstrapDependencies()
	if deps.preflightSourceEvidence == nil {
		t.Fatal("default APP ACL R2 bootstrap source preflight is nil")
	}
	if err := deps.preflightSourceEvidence(); err != nil {
		t.Fatalf("default APP ACL R2 bootstrap source preflight error = %v", err)
	}
}

func TestBootstrapAppACLR2PreparedClassifierResultIsNoMutationRepeat(t *testing.T) {
	trace := make([]string, 0, 8)
	deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StatePrepared})

	if err := bootstrapAppACLR2WithDependencies(t.Context(), oneAppACLR2BootstrapTx, deps); err != nil {
		t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v", err)
	}
	assertAppACLR2BootstrapTraceOrder(t, trace, "classifier", "live-verify")
	if got := appACLR2BootstrapTraceCount(trace, "live-verify"); got != 1 {
		t.Fatalf("bootstrap-only live verification calls = %d, want one for PREPARED target state", got)
	}
	assertAppACLR2BootstrapTraceAbsent(t, trace, "verify-frozen", "preflight", "bootstrap-sql", "l2-acl", "receipt-surface", "compile-receipt", "encode-receipt", "insert-receipt")
}

func TestDefaultAppACLR2BootstrapPreparedSuccessPathsUseBootstrapOnlyLiveVerifier(t *testing.T) {
	assertSameFunction := func(label string, got, want any) {
		t.Helper()
		if reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
			t.Fatalf("default bootstrap %s does not use the bootstrap-only live verifier", label)
		}
	}
	assertSameFunction(
		"PREPARED repeat verifier",
		defaultAppACLR2BootstrapDependencies().verifyPreparedLive,
		verifyAppACLR2BootstrapPreparedLiveInTx,
	)
	assertSameFunction(
		"PREPARED acknowledgement verifier",
		defaultAppACLR2BootstrapACKObserverDependencies().verifyPreparedLive,
		verifyAppACLR2BootstrapLiveL2EvidenceInTx,
	)
}

func TestBootstrapAppACLR2CorruptClassifierResultRejectsWithoutPostClassifierWork(t *testing.T) {
	trace := make([]string, 0, 8)
	deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StateCorrupt})

	err := bootstrapAppACLR2WithDependencies(t.Context(), oneAppACLR2BootstrapTx, deps)
	if err == nil {
		t.Fatal("bootstrapAppACLR2WithDependencies() error = nil, want CORRUPT rejection")
	}
	assertAppACLR2BootstrapTraceAbsent(t, trace, "verify-frozen", "preflight", "bootstrap-sql", "l2-acl", "receipt-surface", "compile-receipt", "encode-receipt", "insert-receipt")
}

func TestBootstrapAppACLR2ClassifierErrorPropagatesWithoutPostClassifierWork(t *testing.T) {
	trace := make([]string, 0, 8)
	classifierErr := errors.New("classifier operational error")
	deps := newAppACLR2BootstrapTraceDependencies(&trace, nil)
	deps.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
		trace = append(trace, "classifier")
		return AppACLR2StateCorrupt, classifierErr
	}

	err := bootstrapAppACLR2WithDependencies(t.Context(), oneAppACLR2BootstrapTx, deps)
	if err != classifierErr {
		t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v, want original classifier error %v", err, classifierErr)
	}
	assertAppACLR2BootstrapTraceAbsent(t, trace, "verify-frozen", "preflight", "bootstrap-sql", "l2-acl", "receipt-surface", "compile-receipt", "encode-receipt", "insert-receipt")
}

func TestBootstrapAppACLR2RejectsActorBeforeInventory(t *testing.T) {
	trace := make([]string, 0, 4)
	actorErr := errors.New("bootstrap actor is not OID 10")
	deps := newAppACLR2BootstrapTraceDependencies(&trace, nil)
	deps.requireBootstrapActor = func(context.Context, pgx.Tx) error {
		trace = append(trace, "actor")
		return actorErr
	}

	err := bootstrapAppACLR2WithDependencies(t.Context(), oneAppACLR2BootstrapTx, deps)
	if err != actorErr {
		t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v, want actor error %v", err, actorErr)
	}
	assertAppACLR2BootstrapTraceAbsent(t, trace, "inventory", "classifier", "locks")
}

func TestBootstrapAppACLR2UsesSerializableSearchPathAdvisoryAndStateLockOrder(t *testing.T) {
	trace := make([]string, 0, 16)
	var options []pgx.TxOptions
	deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StatePrepared})
	tx := &fakeAppACLR2BootstrapLockHandoffTx{trace: &trace}
	conn := &fakeAppACLR2BootstrapReservedConn{
		trace: &trace,
		tx:    tx,
		onBegin: func(optionsArg pgx.TxOptions) {
			options = append(options, optionsArg)
		},
	}
	err := bootstrapAppACLR2WithDependencies(t.Context(), newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
		return conn, nil
	}), deps)
	if err != nil {
		t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v", err)
	}
	if len(options) != 1 || options[0].IsoLevel != pgx.Serializable {
		t.Fatalf("transaction options = %#v, want one SERIALIZABLE transaction", options)
	}
	assertAppACLR2BootstrapTraceOrder(t, trace, "session-lock", "begin", "transaction-lock", "session-unlock", "search-path", "actor", "inventory", "locks", "classifier", "commit", "release")
}

func TestAppACLR2BootstrapLockedBeginDiscardsConnectionWhenSessionUnlockFails(t *testing.T) {
	beginErr := errors.New("begin failed after session lock")
	unlockErr := errors.New("session unlock failed")
	conn := &fakeAppACLR2BootstrapReservedConn{
		beginErr:  beginErr,
		unlockErr: unlockErr,
	}
	begin := newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
		return conn, nil
	})

	tx, err := begin(t.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
	if tx != nil || !errors.Is(err, beginErr) || !errors.Is(err, unlockErr) {
		t.Fatalf("locked begin transaction/error = %v/%v, want nil with begin and unlock identities", tx, err)
	}
	if conn.releases != 0 || conn.discards != 1 {
		t.Fatalf("reserved connection releases/discards = %d/%d, want 0/1 after failed unlock", conn.releases, conn.discards)
	}
}

func TestAppACLR2BootstrapLockedBeginDiscardsPostHandoffUnlockFailureWithBoundedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	unlockErr := errors.New("session unlock failed after transaction-lock handoff")
	tx := &fakeAppACLR2BootstrapLockHandoffTx{
		unlockErr:         unlockErr,
		onTransactionLock: cancel,
	}
	conn := &fakeAppACLR2BootstrapReservedConn{tx: tx}
	begin := newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
		return conn, nil
	})

	gotTx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if gotTx != nil || !errors.Is(err, unlockErr) {
		t.Fatalf("locked begin transaction/error = %v/%v, want nil with post-handoff unlock identity", gotTx, err)
	}
	if conn.releases != 0 || conn.discards != 1 {
		t.Fatalf("reserved connection releases/discards = %d/%d, want 0/1 after post-handoff unlock failure", conn.releases, conn.discards)
	}
	if err := conn.discardCtxErr; err != nil {
		t.Fatalf("discard context error = %v, want cleanup independent of caller cancellation", err)
	}
	if !conn.discardHasDeadline {
		t.Fatal("discard context has no deadline")
	}
	if remaining := time.Until(conn.discardDeadline); remaining <= 0 || remaining > 10*time.Second {
		t.Fatalf("discard context remaining duration = %s, want bounded positive duration no greater than 10s", remaining)
	}
}

func TestAppACLR2BootstrapRejectsNilTransactionsFromAllBeginSeams(t *testing.T) {
	t.Run("transition-locked opener", func(t *testing.T) {
		trace := make([]string, 0, 4)
		conn := &fakeAppACLR2BootstrapReservedConn{trace: &trace}
		begin := newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
			return conn, nil
		})

		err := appACLR2BootstrapErrorWithoutPanic(t, func() error {
			tx, err := begin(t.Context(), pgx.TxOptions{IsoLevel: pgx.Serializable})
			if tx != nil {
				t.Fatalf("locked begin transaction = %v, want nil", tx)
			}
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "returned nil transaction") {
			t.Fatalf("locked begin error = %v, want deterministic nil-transaction error", err)
		}
		if conn.releases != 1 || conn.discards != 0 {
			t.Fatalf("reserved connection releases/discards = %d/%d, want 1/0 after nil transaction", conn.releases, conn.discards)
		}
		assertAppACLR2BootstrapTraceOrder(t, trace, "session-lock", "begin", "session-unlock-cleanup", "release")
	})

	t.Run("bootstrap", func(t *testing.T) {
		trace := make([]string, 0, 4)
		err := appACLR2BootstrapErrorWithoutPanic(t, func() error {
			return bootstrapAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				return nil, nil
			}, newAppACLR2BootstrapTraceDependencies(&trace, nil))
		})
		if err == nil || !strings.Contains(err.Error(), "returned nil transaction") {
			t.Fatalf("bootstrap error = %v, want deterministic nil-transaction error", err)
		}
		if len(trace) != 0 {
			t.Fatalf("bootstrap trace = %#v, want no transaction work after nil begin", trace)
		}
	})

	t.Run("acknowledgement observer", func(t *testing.T) {
		outcome := appACLR2BootstrapACKOutcomePrepared
		err := appACLR2BootstrapErrorWithoutPanic(t, func() error {
			var err error
			outcome, err = recoverAppACLR2BootstrapACKWithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				return nil, nil
			}, appACLR2BootstrapACKObserverDependencies{})
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "returned nil transaction") {
			t.Fatalf("acknowledgement observer error = %v, want deterministic nil-transaction error", err)
		}
		if outcome != appACLR2BootstrapACKOutcomeNone {
			t.Fatalf("acknowledgement observer outcome = %v, want none", outcome)
		}
	})
}

func TestAppACLR2BootstrapStateTableLockOrderNeverLocksM2(t *testing.T) {
	tx := &fakeAppACLR2BootstrapTx{}
	if err := lockAppACLR2BootstrapStateTablesInTx(t.Context(), tx, true); err != nil {
		t.Fatalf("lockAppACLR2BootstrapStateTablesInTx() error = %v", err)
	}
	want := []string{
		"lock table public.app_acl_manifest_head in share row exclusive mode",
		"lock table public.app_acl_manifest_revisions in share row exclusive mode",
		"lock table public.record_platform_domain_identity in share row exclusive mode",
		"lock table public.schema_migrations in share row exclusive mode",
		"lock table public.app_acl_r2_bootstrap_receipt in share row exclusive mode",
	}
	if !reflect.DeepEqual(tx.execSQL, want) {
		t.Fatalf("table lock SQL = %#v, want %#v", tx.execSQL, want)
	}
	for _, sql := range tx.execSQL {
		if strings.Contains(sql, "app_acl_r2_manifest") {
			t.Fatalf("table lock SQL unexpectedly touches M2: %q", sql)
		}
	}
}

func TestDefaultAppACLR2BootstrapReservedObjectReaderUsesKnownPlusOneBound(t *testing.T) {
	assertAppACLR2BootstrapBoundedInventoryReader(t, defaultAppACLR2BootstrapDependencies().readReservedObjects)
}

func TestDefaultAppACLR2BootstrapACKObserverReservedObjectReaderUsesKnownPlusOneBound(t *testing.T) {
	assertAppACLR2BootstrapBoundedInventoryReader(t, defaultAppACLR2BootstrapACKObserverDependencies().readReservedObjects)
}

func assertAppACLR2BootstrapBoundedInventoryReader(t *testing.T, reader func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error)) {
	t.Helper()
	if reader == nil {
		t.Fatal("bootstrap reserved-object inventory reader is nil")
	}
	tx := &recordingAppACLR2BootstrapInventoryTx{}
	_, err := reader(t.Context(), tx)
	if err == nil {
		t.Fatal("bootstrap reserved-object inventory reader error = nil, want query sentinel")
	}
	query := strings.Join(strings.Fields(tx.query), " ")
	if !strings.Contains(query, "order by object_kind, schema_name, object_identity, object_detail limit $1") {
		t.Fatalf("bootstrap reserved-object inventory query = %q, want known-plus-one LIMIT", query)
	}
	wantLimit := len(appACLR2KnownReservedObjects()) + 1
	if !reflect.DeepEqual(tx.arguments, []any{wantLimit}) {
		t.Fatalf("bootstrap reserved-object inventory query arguments = %#v, want %#v", tx.arguments, []any{wantLimit})
	}
	for _, forbidden := range []string{"lock table", "from public.app_acl_r2", "app_acl_r2_manifest_revisions", "app_acl_r2_manifest_head"} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("bootstrap reserved-object inventory query unexpectedly reads non-metadata evidence %q: %q", forbidden, query)
		}
	}
}

func TestAppACLR2BootstrapExecutesMarkerExcludedBootstrapSectionOnly(t *testing.T) {
	tx := &fakeAppACLR2BootstrapTx{}
	if err := executeAppACLR2BootstrapSectionInTx(t.Context(), tx); err != nil {
		t.Fatalf("executeAppACLR2BootstrapSectionInTx() error = %v", err)
	}
	if len(tx.execSQL) != 1 {
		t.Fatalf("bootstrap SQL executions = %d, want one", len(tx.execSQL))
	}
	payload := tx.execSQL[0]
	for _, forbidden := range []string{
		appACLR2BootstrapBeginMarker,
		appACLR2BootstrapEndMarker,
		appACLR2FinalizeBeginMarker,
		appACLR2FinalizeEndMarker,
		"Finalize implementation is owned by Slice 5",
	} {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("bootstrap SQL payload contains forbidden marker/finalize bytes %q", forbidden)
		}
	}
	if !strings.Contains(payload, "create table public.app_acl_r2_bootstrap_receipt") {
		t.Fatalf("bootstrap SQL payload does not contain the L2 receipt DDL: %q", payload)
	}
}

func TestAppACLR2BootstrapAppliesExactL2ACLBeforeReceiptInsert(t *testing.T) {
	tx := &fakeAppACLR2BootstrapTx{}
	state := FrozenAppACLR1StateV1{
		DirectMigratorRole: "direct_migrator",
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
	}
	if err := applyAppACLR2BootstrapL2ACLInTx(t.Context(), tx, state); err != nil {
		t.Fatalf("applyAppACLR2BootstrapL2ACLInTx() error = %v", err)
	}
	if len(tx.execSQL) != 5 {
		t.Fatalf("L2 DCL executions = %d, want five revoke/grant statements: %#v", len(tx.execSQL), tx.execSQL)
	}
	joined := strings.Join(tx.execSQL, "\n")
	for _, required := range []string{
		"revoke all privileges on table public.app_acl_r2_bootstrap_receipt from public, \"direct_migrator\", \"center_runtime\", \"platform_admin\"",
		"grant select on table public.app_acl_r2_bootstrap_receipt to \"direct_migrator\"",
		"grant select on table public.app_acl_r2_bootstrap_receipt to \"center_runtime\"",
		"app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea) from public, \"direct_migrator\", \"center_runtime\", \"platform_admin\"",
		"app_acl_r2_reject_bootstrap_receipt_mutation() from public, \"direct_migrator\", \"center_runtime\", \"platform_admin\"",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("L2 DCL = %q, missing %q", joined, required)
		}
	}
	if strings.Contains(joined, "grant execute") {
		t.Fatalf("L2 DCL grants helper EXECUTE: %q", joined)
	}
}

func TestAppACLR2BootstrapInsertsCanonicalReceiptBodyAndDigest(t *testing.T) {
	tx := &fakeAppACLR2BootstrapTx{}
	body := []byte("canonical receipt body")
	digest := [32]byte{1, 2, 3}
	if err := insertAppACLR2BootstrapReceiptInTx(t.Context(), tx, body, digest); err != nil {
		t.Fatalf("insertAppACLR2BootstrapReceiptInTx() error = %v", err)
	}
	if len(tx.execSQL) != 1 || !strings.Contains(tx.execSQL[0], "insert into public.app_acl_r2_bootstrap_receipt") {
		t.Fatalf("receipt insert SQL = %#v, want canonical L2 insert", tx.execSQL)
	}
	if len(tx.execArgs) != 1 || len(tx.execArgs[0]) != 2 {
		t.Fatalf("receipt insert arguments = %#v, want body and digest", tx.execArgs)
	}
	if got, ok := tx.execArgs[0][0].([]byte); !ok || !reflect.DeepEqual(got, body) {
		t.Fatalf("receipt body argument = %#v, want %#v", tx.execArgs[0][0], body)
	}
	if got, ok := tx.execArgs[0][1].([]byte); !ok || !reflect.DeepEqual(got, digest[:]) {
		t.Fatalf("receipt digest argument = %#v, want %#v", tx.execArgs[0][1], digest[:])
	}
}

func TestBootstrapAppACLR2RetriesOnlyWholeSerializableClosures(t *testing.T) {
	trace := make([]string, 0, 20)
	deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StateR1, AppACLR2StatePrepared})
	classifierCalls := 0
	deps.classify = func(context.Context, pgx.Tx) (AppACLR2State, error) {
		classifierCalls++
		trace = append(trace, "classifier")
		if classifierCalls == 1 {
			return AppACLR2StateCorrupt, &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		}
		if classifierCalls == 2 {
			return AppACLR2StateR1, nil
		}
		return AppACLR2StatePrepared, nil
	}
	begins := 0
	err := bootstrapAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
		begins++
		return &fakeAppACLR2BootstrapTx{}, nil
	}, deps)
	if err != nil {
		t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v", err)
	}
	if begins != 2 {
		t.Fatalf("transaction attempts = %d, want two whole SERIALIZABLE closures", begins)
	}
	if appACLR2BootstrapTraceCount(trace, "inventory") != 2 {
		t.Fatalf("trace = %#v, want inventory rerun for whole transaction retry", trace)
	}
}

func TestBootstrapAppACLR2CommitAcknowledgementDistinguishesSafeRetryAndUncertainRecovery(t *testing.T) {
	safeBeforeSend := appACLR2BootstrapSafeBeforeSendError{}
	serializationFailure := &pgconn.PgError{Code: "40001", Message: "serialization failure"}
	deadlockFailure := &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
	nonRetryablePGError := &pgconn.PgError{Code: "23505", Message: "unique violation"}
	finalRetryableError := &pgconn.PgError{Code: "40001", Message: "retry exhausted"}
	observerError := errors.New("observer inventory corrupt")
	for _, tc := range []struct {
		name         string
		commitErrors []error
		recovery     []appACLR2BootstrapACKOutcome
		recoveryErr  error
		wantBegins   int
		wantRecovery int
		wantError    error
	}{
		{
			name:         "committed acknowledgement",
			commitErrors: []error{nil},
			wantBegins:   1,
		},
		{
			name:         "guaranteed before send returns without replay or observer",
			commitErrors: []error{safeBeforeSend, nil},
			wantBegins:   1,
			wantError:    safeBeforeSend,
		},
		{
			name:         "serialization failure retries whole closure",
			commitErrors: []error{serializationFailure, nil},
			wantBegins:   2,
		},
		{
			name:         "deadlock retries whole closure",
			commitErrors: []error{deadlockFailure, nil},
			wantBegins:   2,
		},
		{
			name:         "non retryable PostgreSQL error returns without observer",
			commitErrors: []error{nonRetryablePGError},
			wantBegins:   1,
			wantError:    nonRetryablePGError,
		},
		{
			name:         "retry exhaustion returns final PostgreSQL error",
			commitErrors: []error{serializationFailure, deadlockFailure, finalRetryableError},
			wantBegins:   3,
			wantError:    finalRetryableError,
		},
		{
			name:         "uncertain acknowledgement prepared observer succeeds",
			commitErrors: []error{errors.New("lost commit acknowledgement")},
			recovery:     []appACLR2BootstrapACKOutcome{appACLR2BootstrapACKOutcomePrepared},
			wantBegins:   1,
			wantRecovery: 1,
		},
		{
			name:         "uncertain acknowledgement R1 observer retries whole closure",
			commitErrors: []error{errors.New("lost commit acknowledgement"), nil},
			recovery:     []appACLR2BootstrapACKOutcome{appACLR2BootstrapACKOutcomeR1},
			wantBegins:   2,
			wantRecovery: 1,
		},
		{
			name:         "uncertain acknowledgement observer error fails",
			commitErrors: []error{errors.New("lost commit acknowledgement")},
			recoveryErr:  observerError,
			wantBegins:   1,
			wantRecovery: 1,
			wantError:    observerError,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trace := make([]string, 0, 20)
			deps := newAppACLR2BootstrapTraceDependencies(&trace, []AppACLR2State{AppACLR2StatePrepared, AppACLR2StatePrepared, AppACLR2StatePrepared})
			outcomes := append([]appACLR2BootstrapACKOutcome(nil), tc.recovery...)
			recoveryCalls := 0
			deps.recoverCommitAcknowledgement = func(context.Context) (appACLR2BootstrapACKOutcome, error) {
				recoveryCalls++
				trace = append(trace, "ack-observer")
				if tc.recoveryErr != nil {
					return appACLR2BootstrapACKOutcomeNone, tc.recoveryErr
				}
				if len(outcomes) == 0 {
					t.Fatal("unexpected ACK observer call")
				}
				outcome := outcomes[0]
				outcomes = outcomes[1:]
				return outcome, nil
			}
			begins := 0
			err := bootstrapAppACLR2WithDependencies(t.Context(), func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				commitErr := tc.commitErrors[begins]
				begins++
				return &fakeAppACLR2BootstrapTx{commitErr: commitErr}, nil
			}, deps)
			if tc.wantError == nil && err != nil {
				t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v", err)
			}
			if tc.wantError != nil && !errors.Is(err, tc.wantError) {
				t.Fatalf("bootstrapAppACLR2WithDependencies() error = %v, want error identity %v", err, tc.wantError)
			}
			if begins != tc.wantBegins || recoveryCalls != tc.wantRecovery {
				t.Fatalf("begins=%d recoveryCalls=%d, want %d and %d", begins, recoveryCalls, tc.wantBegins, tc.wantRecovery)
			}
		})
	}
}

func TestRecoverAppACLR2BootstrapACKUsesSerializableReadOnlyTransaction(t *testing.T) {
	var options []pgx.TxOptions
	dependencies := appACLR2BootstrapACKObserverDependencies{
		hardenSearchPath:      func(context.Context, pgx.Tx) error { return nil },
		requireBootstrapActor: func(context.Context, pgx.Tx) error { return nil },
		readReservedObjects: func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
			return nil, nil
		},
		verifyFrozen: func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error) {
			return appACLR2BootstrapFrozenStateFixture(), nil
		},
		readL2Rows: func(context.Context, pgx.Tx) ([]appACLR2ReceiptRowV1, error) {
			t.Fatal("exact R1 acknowledgement observer read L2 rows")
			return nil, nil
		},
		exactL2Rows: func([]appACLR2ReceiptRowV1) bool { return false },
		verifyL2Evidence: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error {
			t.Fatal("exact R1 acknowledgement observer verified L2 evidence")
			return nil
		},
		verifyPreparedLive: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error {
			t.Fatal("exact R1 acknowledgement observer verified bootstrap live evidence")
			return nil
		},
	}

	trace := make([]string, 0, 8)
	conn := &fakeAppACLR2BootstrapReservedConn{
		trace: &trace,
		tx:    &fakeAppACLR2BootstrapLockHandoffTx{trace: &trace},
		onBegin: func(got pgx.TxOptions) {
			options = append(options, got)
		},
	}
	outcome, err := recoverAppACLR2BootstrapACKWithDependencies(t.Context(), newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
		return conn, nil
	}), dependencies)
	if err != nil || outcome != appACLR2BootstrapACKOutcomeR1 {
		t.Fatalf("recoverAppACLR2BootstrapACKWithDependencies() outcome/error = %v/%v, want R1/nil", outcome, err)
	}
	want := pgx.TxOptions{IsoLevel: pgx.Serializable, AccessMode: pgx.ReadOnly}
	if len(options) != 1 || !reflect.DeepEqual(options[0], want) {
		t.Fatalf("acknowledgement observer transaction options = %#v, want %#v", options, want)
	}
	assertAppACLR2BootstrapTraceOrder(t, trace, "session-lock", "begin", "transaction-lock", "session-unlock", "commit", "release")
}

func TestRecoverAppACLR2BootstrapACKRejectsConcurrentFinalizedAfterLockWait(t *testing.T) {
	committedState := AppACLR2StatePrepared
	snapshotState := AppACLR2StateCorrupt
	trace := make([]string, 0, 16)
	dependencies := appACLR2BootstrapACKObserverDependencies{
		hardenSearchPath: func(context.Context, pgx.Tx) error { return nil },
		requireBootstrapActor: func(context.Context, pgx.Tx) error {
			trace = append(trace, "actor")
			return nil
		},
		readReservedObjects: func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
			trace = append(trace, "inventory")
			if snapshotState == AppACLR2StatePrepared {
				return appACLR2BootstrapInventoryWithOIDs(appACLR2L2ReservedObjects()), nil
			}
			return appACLR2BootstrapInventoryWithOIDs(appACLR2KnownReservedObjects()), nil
		},
		verifyFrozen: func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error) {
			return appACLR2BootstrapFrozenStateFixture(), nil
		},
		readL2Rows: func(context.Context, pgx.Tx) ([]appACLR2ReceiptRowV1, error) {
			return []appACLR2ReceiptRowV1{{Singleton: true}}, nil
		},
		exactL2Rows: func(rows []appACLR2ReceiptRowV1) bool {
			return len(rows) == 1 && rows[0].Singleton
		},
		verifyL2Evidence: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error {
			return nil
		},
		verifyPreparedLive: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error {
			return nil
		},
	}
	tx := &fakeAppACLR2BootstrapLockHandoffTx{trace: &trace}
	conn := &fakeAppACLR2BootstrapReservedConn{
		trace: &trace,
		tx:    tx,
		onSessionLock: func() {
			// Model the observer waiting while a concurrent transition commits
			// FINALIZED, then receiving the conflicting session lock.
			trace = append(trace, "finalized-commit")
			committedState = AppACLR2StateFinalized
		},
		onBegin: func(pgx.TxOptions) {
			snapshotState = committedState
		},
	}

	outcome, err := recoverAppACLR2BootstrapACKWithDependencies(t.Context(), newAppACLR2BootstrapTransitionLockedBegin(func(context.Context) (appACLR2BootstrapReservedConn, error) {
		return conn, nil
	}), dependencies)
	if err == nil || outcome != appACLR2BootstrapACKOutcomeNone {
		t.Fatalf("recovery outcome/error = %v/%v after concurrent FINALIZED commit, want none/error", outcome, err)
	}
	assertAppACLR2BootstrapTraceOrder(t, trace, "session-lock", "finalized-commit", "begin", "transaction-lock", "session-unlock", "actor", "inventory", "rollback", "release")
	assertAppACLR2BootstrapTraceAbsent(t, trace, "read-l2", "verify-l2")
}

func TestObserveAppACLR2BootstrapACKRecoveryOnlyProvesR1OrPrepared(t *testing.T) {
	for _, tc := range []struct {
		name        string
		objects     []AppACLR2ReservedCatalogObjectV1
		rows        []appACLR2ReceiptRowV1
		wantOutcome appACLR2BootstrapACKOutcome
		wantError   bool
		exactRows   bool
		wantRows    int
		wantVerify  int
		wantLive    int
	}{
		{name: "exact R1", wantOutcome: appACLR2BootstrapACKOutcomeR1},
		{
			name:        "exact prepared",
			objects:     appACLR2BootstrapInventoryWithOIDs(appACLR2L2ReservedObjects()),
			rows:        []appACLR2ReceiptRowV1{{Singleton: true}},
			wantOutcome: appACLR2BootstrapACKOutcomePrepared,
			exactRows:   true,
			wantRows:    1,
			wantVerify:  1,
			wantLive:    1,
		},
		{
			name:       "invalid L2 receipt evidence",
			objects:    appACLR2BootstrapInventoryWithOIDs(appACLR2L2ReservedObjects()),
			rows:       []appACLR2ReceiptRowV1{{Singleton: true}},
			wantError:  true,
			wantRows:   1,
			wantVerify: 0,
		},
		{name: "partial L2", objects: appACLR2BootstrapInventoryWithOIDs(appACLR2L2ReservedObjects())[:1], wantError: true},
		{
			name:      "excess L2 inventory",
			objects:   append(appACLR2BootstrapInventoryWithOIDs(appACLR2L2ReservedObjects()), AppACLR2ReservedCatalogObjectV1{Kind: "relation", Schema: "public", OID: 99, Identity: "app_acl_r2_extra", Detail: "r"}),
			wantError: true,
		},
		{
			name:      "bounded inventory overflow",
			objects:   append(appACLR2BootstrapInventoryWithOIDs(appACLR2KnownReservedObjects()), AppACLR2ReservedCatalogObjectV1{Kind: "relation", Schema: "public", OID: 99, Identity: "app_acl_r2_extra", Detail: "r"}),
			wantError: true,
		},
		{name: "unknown inventory", objects: []AppACLR2ReservedCatalogObjectV1{{Kind: "relation", Schema: "public", OID: 99, Identity: "app_acl_r2_unknown", Detail: "r"}}, wantError: true},
		{name: "M2 inventory", objects: []AppACLR2ReservedCatalogObjectV1{appACLR2M2HeadRelation()}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trace := make([]string, 0, 10)
			l2RowsReads := 0
			l2VerificationCalls := 0
			liveVerificationCalls := 0
			deps := appACLR2BootstrapACKObserverDependencies{
				hardenSearchPath: func(context.Context, pgx.Tx) error {
					trace = append(trace, "search-path")
					return nil
				},
				requireBootstrapActor: func(context.Context, pgx.Tx) error {
					trace = append(trace, "actor")
					return nil
				},
				readReservedObjects: func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
					trace = append(trace, "inventory")
					return tc.objects, nil
				},
				verifyFrozen: func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error) {
					trace = append(trace, "verify-frozen")
					return appACLR2BootstrapFrozenStateFixture(), nil
				},
				readL2Rows: func(context.Context, pgx.Tx) ([]appACLR2ReceiptRowV1, error) {
					l2RowsReads++
					trace = append(trace, "read-l2")
					return tc.rows, nil
				},
				exactL2Rows: func(rows []appACLR2ReceiptRowV1) bool {
					return tc.exactRows && len(rows) == 1 && rows[0].Singleton
				},
				verifyL2Evidence: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error {
					l2VerificationCalls++
					trace = append(trace, "verify-l2")
					return nil
				},
				verifyPreparedLive: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1, appACLR2ReceiptRowV1) error {
					liveVerificationCalls++
					trace = append(trace, "live-verify")
					return nil
				},
			}
			outcome, err := observeAppACLR2BootstrapACKRecoveryInTxWithDependencies(t.Context(), &fakeAppACLR2BootstrapTx{}, deps)
			if tc.wantError {
				if err == nil {
					t.Fatalf("observer outcome = %v, error = nil, want inventory failure", outcome)
				}
				if l2RowsReads != tc.wantRows || l2VerificationCalls != tc.wantVerify || liveVerificationCalls != tc.wantLive {
					t.Fatalf("invalid observer L2 reads/shared/live verification = %d/%d/%d, want %d/%d/%d; trace=%#v", l2RowsReads, l2VerificationCalls, liveVerificationCalls, tc.wantRows, tc.wantVerify, tc.wantLive, trace)
				}
				assertAppACLR2BootstrapTraceAbsent(t, trace, "classifier", "m2-content", "m2-scan", "m2-aggregate", "finalized")
				return
			}
			if err != nil || outcome != tc.wantOutcome {
				t.Fatalf("observer outcome/error = %v/%v, want %v/nil", outcome, err, tc.wantOutcome)
			}
			assertAppACLR2BootstrapTraceOrder(t, trace, "actor", "inventory")
			if l2RowsReads != tc.wantRows || l2VerificationCalls != tc.wantVerify || liveVerificationCalls != tc.wantLive {
				t.Fatalf("observer L2 reads/shared/live verification = %d/%d/%d, want %d/%d/%d", l2RowsReads, l2VerificationCalls, liveVerificationCalls, tc.wantRows, tc.wantVerify, tc.wantLive)
			}
		})
	}
}

func newAppACLR2BootstrapTraceDependencies(trace *[]string, states []AppACLR2State) appACLR2BootstrapDependencies {
	stateIndex := 0
	return appACLR2BootstrapDependencies{
		hardenSearchPath: func(context.Context, pgx.Tx) error {
			*trace = append(*trace, "search-path")
			return nil
		},
		requireBootstrapActor: func(context.Context, pgx.Tx) error {
			*trace = append(*trace, "actor")
			return nil
		},
		readReservedObjects: func(context.Context, pgx.Tx) ([]AppACLR2ReservedCatalogObjectV1, error) {
			*trace = append(*trace, "inventory")
			return nil, nil
		},
		lockStateTables: func(context.Context, pgx.Tx, bool) error {
			*trace = append(*trace, "locks")
			return nil
		},
		classify: func(context.Context, pgx.Tx) (AppACLR2State, error) {
			*trace = append(*trace, "classifier")
			if stateIndex >= len(states) {
				return AppACLR2StateCorrupt, errors.New("unexpected classifier call")
			}
			state := states[stateIndex]
			stateIndex++
			return state, nil
		},
		verifyPreparedLive: func(context.Context, pgx.Tx) error {
			*trace = append(*trace, "live-verify")
			return nil
		},
		verifyFrozen: func(context.Context, pgx.Tx) (FrozenAppACLR1StateV1, error) {
			*trace = append(*trace, "verify-frozen")
			return appACLR2BootstrapFrozenStateFixture(), nil
		},
		readBootstrapCatalog: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLR2BootstrapCatalogSnapshotV1, error) {
			*trace = append(*trace, "preflight")
			return AppACLR2BootstrapCatalogSnapshotV1{}, nil
		},
		validatePreMutationEvidence: func(AppACLR2BootstrapCatalogSnapshotV1, FrozenAppACLR1StateV1) error {
			*trace = append(*trace, "pre-mutation-evidence")
			return nil
		},
		preflightSourceEvidence: func() error {
			*trace = append(*trace, "source-preflight")
			return nil
		},
		executeBootstrapSection: func(context.Context, pgx.Tx) error {
			*trace = append(*trace, "bootstrap-sql")
			return nil
		},
		applyL2ACL: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) error {
			*trace = append(*trace, "l2-acl")
			return nil
		},
		readReceiptSurface: func(context.Context, pgx.Tx, FrozenAppACLR1StateV1) (AppACLR2ReceiptCatalogSnapshotV1, error) {
			*trace = append(*trace, "receipt-surface")
			return AppACLR2ReceiptCatalogSnapshotV1{}, nil
		},
		compileReceipt: func(AppACLR2BootstrapCatalogSnapshotV1, AppACLR2ReceiptCatalogSnapshotV1, FrozenAppACLR1StateV1) (AppACLR2BootstrapReceiptV1, error) {
			*trace = append(*trace, "compile-receipt")
			return AppACLR2BootstrapReceiptV1{}, nil
		},
		encodeReceipt: func(AppACLR2BootstrapReceiptV1) ([]byte, error) {
			*trace = append(*trace, "encode-receipt")
			return []byte("canonical receipt"), nil
		},
		insertReceipt: func(context.Context, pgx.Tx, []byte, [32]byte) error {
			*trace = append(*trace, "insert-receipt")
			return nil
		},
		recoverCommitAcknowledgement: func(context.Context) (appACLR2BootstrapACKOutcome, error) {
			return appACLR2BootstrapACKOutcomeNone, errors.New("unexpected commit acknowledgement recovery")
		},
		retryable:   isAppACLR2BootstrapRetryable,
		safeToRetry: pgconn.SafeToRetry,
	}
}

func appACLR2BootstrapFrozenStateFixture() FrozenAppACLR1StateV1 {
	return FrozenAppACLR1StateV1{
		DatabaseName:       "record_platform",
		ManifestRevision:   1,
		CenterRuntimeRole:  "center_runtime",
		PlatformAdminRole:  "platform_admin",
		DirectMigratorRole: "direct_migrator",
	}
}

func appACLR2BootstrapInventoryWithOIDs(objects []AppACLR2ReservedCatalogObjectV1) []AppACLR2ReservedCatalogObjectV1 {
	result := append([]AppACLR2ReservedCatalogObjectV1(nil), objects...)
	for index := range result {
		result[index].OID = uint32(index + 1)
	}
	return result
}

func oneAppACLR2BootstrapTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return &fakeAppACLR2BootstrapTx{}, nil
}

func appACLR2BootstrapErrorWithoutPanic(t *testing.T, call func() error) (err error) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("call panicked: %v", recovered)
		}
	}()
	return call()
}

func assertAppACLR2BootstrapTraceOrder(t *testing.T, trace []string, labels ...string) {
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

func assertAppACLR2BootstrapTraceAbsent(t *testing.T, trace []string, labels ...string) {
	t.Helper()
	for _, label := range labels {
		if appACLR2BootstrapTraceCount(trace, label) != 0 {
			t.Fatalf("trace = %#v, unexpectedly contains %q", trace, label)
		}
	}
}

func appACLR2BootstrapTraceCount(trace []string, label string) int {
	count := 0
	for _, value := range trace {
		if value == label {
			count++
		}
	}
	return count
}

type appACLR2BootstrapSafeBeforeSendError struct{}

func (appACLR2BootstrapSafeBeforeSendError) Error() string { return "commit not sent" }

func (appACLR2BootstrapSafeBeforeSendError) SafeToRetry() bool { return true }

type fakeAppACLR2BootstrapTx struct {
	commitErr error
	execSQL   []string
	execArgs  [][]any
}

type recordingAppACLR2BootstrapInventoryTx struct {
	fakeAppACLR2BootstrapTx
	query     string
	arguments []any
}

type fakeAppACLR2BootstrapReservedConn struct {
	trace              *[]string
	tx                 pgx.Tx
	onSessionLock      func()
	onBegin            func(pgx.TxOptions)
	beginErr           error
	unlockErr          error
	releases           int
	discards           int
	discardCtxErr      error
	discardDeadline    time.Time
	discardHasDeadline bool
}

func (conn *fakeAppACLR2BootstrapReservedConn) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if sql != appACLR2BootstrapSessionTransitionLockSQL {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected reserved-connection SQL %q", sql)
	}
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "session-lock")
	}
	if conn.onSessionLock != nil {
		conn.onSessionLock()
	}
	return pgconn.CommandTag{}, nil
}

func (conn *fakeAppACLR2BootstrapReservedConn) BeginTx(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "begin")
	}
	if conn.onBegin != nil {
		conn.onBegin(options)
	}
	return conn.tx, conn.beginErr
}

func (conn *fakeAppACLR2BootstrapReservedConn) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if sql != appACLR2BootstrapSessionTransitionUnlockSQL {
		return appACLR2BootstrapErrorRow{err: fmt.Errorf("unexpected reserved-connection query %q", sql)}
	}
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "session-unlock-cleanup")
	}
	if conn.unlockErr != nil {
		return appACLR2BootstrapErrorRow{err: conn.unlockErr}
	}
	return appACLR2BootstrapBoolRow(true)
}

func (conn *fakeAppACLR2BootstrapReservedConn) Release() {
	conn.releases++
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "release")
	}
}

func (conn *fakeAppACLR2BootstrapReservedConn) Discard(ctx context.Context) error {
	conn.discards++
	conn.discardCtxErr = ctx.Err()
	conn.discardDeadline, conn.discardHasDeadline = ctx.Deadline()
	if conn.trace != nil {
		*conn.trace = append(*conn.trace, "discard")
	}
	return nil
}

type fakeAppACLR2BootstrapLockHandoffTx struct {
	fakeAppACLR2BootstrapTx
	trace             *[]string
	onTransactionLock func()
	unlockErr         error
}

func (tx *fakeAppACLR2BootstrapLockHandoffTx) Commit(ctx context.Context) error {
	if tx.trace != nil {
		*tx.trace = append(*tx.trace, "commit")
	}
	return tx.fakeAppACLR2BootstrapTx.Commit(ctx)
}

func (tx *fakeAppACLR2BootstrapLockHandoffTx) Rollback(ctx context.Context) error {
	if tx.trace != nil {
		*tx.trace = append(*tx.trace, "rollback")
	}
	return tx.fakeAppACLR2BootstrapTx.Rollback(ctx)
}

func (tx *fakeAppACLR2BootstrapLockHandoffTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	if sql == appACLR2BootstrapTransactionTransitionLockSQL {
		if tx.trace != nil {
			*tx.trace = append(*tx.trace, "transaction-lock")
		}
		if tx.onTransactionLock != nil {
			tx.onTransactionLock()
		}
		return pgconn.CommandTag{}, nil
	}
	return tx.fakeAppACLR2BootstrapTx.Exec(ctx, sql, arguments...)
}

func (tx *fakeAppACLR2BootstrapLockHandoffTx) QueryRow(ctx context.Context, sql string, arguments ...any) pgx.Row {
	if sql == appACLR2BootstrapSessionTransitionUnlockSQL {
		if tx.trace != nil {
			*tx.trace = append(*tx.trace, "session-unlock")
		}
		if tx.unlockErr != nil {
			return appACLR2BootstrapErrorRow{err: tx.unlockErr}
		}
		return appACLR2BootstrapBoolRow(true)
	}
	return tx.fakeAppACLR2BootstrapTx.QueryRow(ctx, sql, arguments...)
}

func (tx *recordingAppACLR2BootstrapInventoryTx) Query(_ context.Context, query string, arguments ...any) (pgx.Rows, error) {
	tx.query = query
	tx.arguments = append([]any(nil), arguments...)
	return nil, errors.New("reserved-object inventory query sentinel")
}

func (tx *fakeAppACLR2BootstrapTx) Begin(context.Context) (pgx.Tx, error) { return tx, nil }

func (tx *fakeAppACLR2BootstrapTx) Commit(context.Context) error { return tx.commitErr }

func (*fakeAppACLR2BootstrapTx) Rollback(context.Context) error { return nil }

func (*fakeAppACLR2BootstrapTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected CopyFrom")
}

func (*fakeAppACLR2BootstrapTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }

func (*fakeAppACLR2BootstrapTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }

func (*fakeAppACLR2BootstrapTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected Prepare")
}

func (tx *fakeAppACLR2BootstrapTx) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	tx.execSQL = append(tx.execSQL, sql)
	tx.execArgs = append(tx.execArgs, append([]any(nil), arguments...))
	return pgconn.CommandTag{}, nil
}

func (*fakeAppACLR2BootstrapTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (*fakeAppACLR2BootstrapTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return appACLR2BootstrapErrorRow{err: errors.New("unexpected QueryRow")}
}

func (*fakeAppACLR2BootstrapTx) Conn() *pgx.Conn { return nil }

type appACLR2BootstrapErrorRow struct{ err error }

func (row appACLR2BootstrapErrorRow) Scan(...any) error { return row.err }

type appACLR2BootstrapBoolRow bool

func (row appACLR2BootstrapBoolRow) Scan(destinations ...any) error {
	if len(destinations) != 1 {
		return fmt.Errorf("boolean row destination count = %d", len(destinations))
	}
	destination, ok := destinations[0].(*bool)
	if !ok {
		return fmt.Errorf("boolean row destination has type %T", destinations[0])
	}
	*destination = bool(row)
	return nil
}
