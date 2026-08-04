package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAppACLRuntimeAdmissionUsesOneRepeatableReadOnlySnapshot(t *testing.T) {
	embeddedMigrations, manifestSnapshot, _ := validAppACLManifestRuntimeFixture(t)
	input, catalogSnapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	catalogSnapshot.DatabaseName = manifestSnapshot.DatabaseName
	catalogSnapshot.SessionUser = manifestSnapshot.SessionUser
	catalogSnapshot.CurrentUser = manifestSnapshot.CurrentUser
	tx := &fakeAppACLRuntimeAdmissionTx{}
	var beginOptions []pgx.TxOptions
	var readTransactions []pgx.Tx

	err := admitAppACLRuntimeWithDependencies(context.Background(), appACLRuntimeAdmissionDependencies{
		beginTx: func(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			beginOptions = append(beginOptions, options)
			return tx, nil
		},
		readManifest: func(_ context.Context, gotTx pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
			readTransactions = append(readTransactions, gotTx)
			return manifestSnapshot, nil
		},
		verifyManifest: func(snapshot AppACLManifestRuntimeSnapshotV1) (AppACLManifestPersistedV1, error) {
			return verifyAppACLManifestRuntimeSnapshotV1(snapshot, embeddedMigrations)
		},
		readCatalog: func(_ context.Context, gotTx pgx.Tx, gotInput AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			readTransactions = append(readTransactions, gotTx)
			if gotInput != input {
				t.Fatalf("catalog input = %#v, want manifest-derived input %#v", gotInput, input)
			}
			return catalogSnapshot, nil
		},
		verifyCatalog: VerifyAppACLEffectiveCatalogSnapshotR1,
	})
	if err != nil {
		t.Fatalf("admitAppACLRuntimeWithDependencies() error = %v, want nil", err)
	}
	if len(beginOptions) != 1 {
		t.Fatalf("transaction begins = %d, want 1", len(beginOptions))
	}
	if got := beginOptions[0]; got.IsoLevel != pgx.RepeatableRead || got.AccessMode != pgx.ReadOnly {
		t.Fatalf("transaction options = %#v, want REPEATABLE READ READ ONLY", got)
	}
	if len(readTransactions) != 2 || readTransactions[0] != tx || readTransactions[1] != tx {
		t.Fatalf("reader transactions = %#v, want the one admission transaction", readTransactions)
	}
	if tx.commitCalls != 1 {
		t.Fatalf("transaction commits = %d, want 1", tx.commitCalls)
	}
	if tx.rollbackCalls != 1 {
		t.Fatalf("transaction rollbacks = %d, want deferred cleanup once", tx.rollbackCalls)
	}
}

func TestAppACLRuntimeAdmissionRejectsLedgerDriftBeforeCatalogRead(t *testing.T) {
	embeddedMigrations, manifestSnapshot, _ := validAppACLManifestRuntimeFixture(t)
	manifestSnapshot.AppliedMigrations = append(manifestSnapshot.AppliedMigrations, MigrationChecksumEntry{Filename: "9999_unknown.sql"})
	tx := &fakeAppACLRuntimeAdmissionTx{}
	catalogRead := false

	err := admitAppACLRuntimeWithDependencies(context.Background(), appACLRuntimeAdmissionDependencies{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
		readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
			return manifestSnapshot, nil
		},
		verifyManifest: func(snapshot AppACLManifestRuntimeSnapshotV1) (AppACLManifestPersistedV1, error) {
			return verifyAppACLManifestRuntimeSnapshotV1(snapshot, embeddedMigrations)
		},
		readCatalog: func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			catalogRead = true
			return AppACLEffectiveCatalogSnapshotR1{}, nil
		},
		verifyCatalog: VerifyAppACLEffectiveCatalogSnapshotR1,
	})
	if err == nil || !strings.Contains(err.Error(), "applied migration ledger") {
		t.Fatalf("admitAppACLRuntimeWithDependencies() error = %v, want ledger drift rejection", err)
	}
	if catalogRead {
		t.Fatal("runtime admission read the catalog after rejecting manifest ledger drift")
	}
	if tx.commitCalls != 0 {
		t.Fatalf("transaction commits = %d, want 0", tx.commitCalls)
	}
	if tx.rollbackCalls != 1 {
		t.Fatalf("transaction rollbacks = %d, want 1", tx.rollbackCalls)
	}
}

func TestAppACLRuntimeAdmissionRejectsSetRoleIdentityBeforeCatalogRead(t *testing.T) {
	embeddedMigrations, manifestSnapshot, _ := validAppACLManifestRuntimeFixture(t)
	manifestSnapshot.SessionUser = "member_login"
	tx := &fakeAppACLRuntimeAdmissionTx{}
	catalogRead := false

	err := admitAppACLRuntimeWithDependencies(context.Background(), appACLRuntimeAdmissionDependencies{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
		readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
			return manifestSnapshot, nil
		},
		verifyManifest: func(snapshot AppACLManifestRuntimeSnapshotV1) (AppACLManifestPersistedV1, error) {
			return verifyAppACLManifestRuntimeSnapshotV1(snapshot, embeddedMigrations)
		},
		readCatalog: func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			catalogRead = true
			return AppACLEffectiveCatalogSnapshotR1{}, nil
		},
		verifyCatalog: VerifyAppACLEffectiveCatalogSnapshotR1,
	})
	if err == nil || err.Error() != `verify app ACL runtime manifest: session user "member_login" does not match current user "houfeng_center_runtime"` {
		t.Fatalf("admitAppACLRuntimeWithDependencies() error = %v, want direct SET ROLE identity rejection", err)
	}
	if catalogRead {
		t.Fatal("runtime admission read the catalog after rejecting SET ROLE identity")
	}
	if tx.commitCalls != 0 || tx.rollbackCalls != 1 {
		t.Fatalf("transaction lifecycle = commit %d rollback %d, want 0/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func TestAppACLRuntimeAdmissionRejectsProjectorExecuteCatalogDrift(t *testing.T) {
	embeddedMigrations, manifestSnapshot, _ := validAppACLManifestRuntimeFixture(t)
	_, catalogSnapshot := validAppACLEffectiveCatalogVerifierFixture(t)
	catalogSnapshot.DatabaseName = manifestSnapshot.DatabaseName
	catalogSnapshot.SessionUser = manifestSnapshot.SessionUser
	catalogSnapshot.CurrentUser = manifestSnapshot.CurrentUser
	catalogSnapshot.DirectPrivileges = append(catalogSnapshot.DirectPrivileges, AppACLEffectiveCatalogPrivilegeObservationR1{
		Grantee:        "houfeng_center_runtime",
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     "public",
		ObjectIdentity: "record_platform_cas_contract_activation_projection(bytea)",
		Privilege:      AppACLPrivilegeExecute,
	})
	tx := &fakeAppACLRuntimeAdmissionTx{}

	err := admitAppACLRuntimeWithDependencies(context.Background(), appACLRuntimeAdmissionDependencies{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
		readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
			return manifestSnapshot, nil
		},
		verifyManifest: func(snapshot AppACLManifestRuntimeSnapshotV1) (AppACLManifestPersistedV1, error) {
			return verifyAppACLManifestRuntimeSnapshotV1(snapshot, embeddedMigrations)
		},
		readCatalog: func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			return catalogSnapshot, nil
		},
		verifyCatalog: VerifyAppACLEffectiveCatalogSnapshotR1,
	})
	if err == nil || !strings.Contains(err.Error(), "verify app ACL runtime catalog") {
		t.Fatalf("admitAppACLRuntimeWithDependencies() error = %v, want projector EXECUTE catalog rejection", err)
	}
	if tx.commitCalls != 0 || tx.rollbackCalls != 1 {
		t.Fatalf("transaction lifecycle = commit %d rollback %d, want 0/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func TestPostgresIntegrationAppACLRuntimeAdmissionDirectLoginAndSetRole(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLR1() error = %v", err)
	}

	runtimeDB := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	if err := AdmitAppACLRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("AdmitAppACLRuntime() direct runtime error = %v", err)
	}
	var ledgerCount int
	if err := runtimeDB.QueryRow(ctx, `select count(*)::int from public.schema_migrations`).Scan(&ledgerCount); err != nil {
		t.Fatalf("count admitted runtime migration ledger: %v", err)
	}
	if ledgerCount != 52 {
		t.Fatalf("admitted runtime migration ledger count = %d, want 52", ledgerCount)
	}
	for _, role := range []string{fixture.runtime, fixture.admin} {
		for _, projector := range []string{
			"public.record_platform_cas_contract_activation_projection(bytea)",
			"public.record_platform_cas_domain_rotation_projection(bytea)",
		} {
			var executable bool
			if err := fixture.db.QueryRow(ctx, `select has_function_privilege($1, $2::regprocedure, 'EXECUTE')`, role, projector).Scan(&executable); err != nil {
				t.Fatalf("read projector EXECUTE for role %q function %q: %v", role, projector, err)
			}
			if executable {
				t.Fatalf("role %q has unexpected EXECUTE on projector %q", role, projector)
			}
		}
	}

	memberLogin := "member_" + fixture.runtime
	memberPassword := appACLEffectiveCatalogTemporaryPassword(t)
	quotedMember := quotePostgresIdentifier(memberLogin)
	quotedRuntime := quotePostgresIdentifier(fixture.runtime)
	quotedDatabase := quotePostgresIdentifier(fixture.databaseName)
	if _, err := fixture.db.Exec(ctx, `create role `+quotedMember+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+memberPassword+`'`); err != nil {
		t.Fatalf("create restricted member login %q: %v", memberLogin, err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		if _, err := fixture.db.Exec(cleanupCtx, `revoke `+quotedRuntime+` from `+quotedMember); err != nil {
			t.Errorf("revoke runtime role %q from member %q: %v", fixture.runtime, memberLogin, err)
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop owned by `+quotedMember); err != nil {
			t.Errorf("drop member-login dependencies %q: %v", memberLogin, err)
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop role if exists `+quotedMember); err != nil {
			t.Errorf("drop member login %q: %v", memberLogin, err)
		}
	})
	if _, err := fixture.db.Exec(ctx, `grant `+quotedRuntime+` to `+quotedMember); err != nil {
		t.Fatalf("grant runtime role %q to member %q: %v", fixture.runtime, memberLogin, err)
	}
	if _, err := fixture.db.Exec(ctx, `grant connect on database `+quotedDatabase+` to `+quotedMember); err != nil {
		t.Fatalf("grant database connect to member %q: %v", memberLogin, err)
	}

	memberConfig := fixture.db.Config().Copy()
	memberConfig.MaxConns = 1
	memberConfig.MinConns = 0
	memberConfig.ConnConfig.User = memberLogin
	memberConfig.ConnConfig.Password = memberPassword
	memberConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, `set role `+quotedRuntime)
		return err
	}
	memberRuntimeDB, err := pgxpool.NewWithConfig(ctx, memberConfig)
	if err != nil {
		t.Fatalf("open member-login runtime pool: %v", err)
	}
	t.Cleanup(memberRuntimeDB.Close)

	var sessionUser, currentUser string
	if err := memberRuntimeDB.QueryRow(ctx, `select session_user, current_user`).Scan(&sessionUser, &currentUser); err != nil {
		t.Fatalf("read member-login runtime identities: %v", err)
	}
	if sessionUser != memberLogin || currentUser != fixture.runtime {
		t.Fatalf("member-login runtime identities = (%q, %q), want (%q, %q)", sessionUser, currentUser, memberLogin, fixture.runtime)
	}
	err = AdmitAppACLRuntime(ctx, memberRuntimeDB)
	wantErr := `verify app ACL runtime manifest: session user "` + memberLogin + `" does not match current user "` + fixture.runtime + `"`
	if err == nil || err.Error() != wantErr {
		t.Fatalf("AdmitAppACLRuntime() SET ROLE error = %v, want %q", err, wantErr)
	}
}

type fakeAppACLRuntimeAdmissionTx struct {
	pgx.Tx
	commitCalls   int
	rollbackCalls int
}

func (tx *fakeAppACLRuntimeAdmissionTx) Commit(context.Context) error {
	tx.commitCalls++
	return nil
}

func (tx *fakeAppACLRuntimeAdmissionTx) Rollback(context.Context) error {
	tx.rollbackCalls++
	return nil
}
