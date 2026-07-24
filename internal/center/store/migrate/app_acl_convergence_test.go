package migrate

import (
	"context"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/db/migrations"
	"houfeng/internal/center/platformmigrate"
)

func TestDirectAppACLRoleSetRequiresMatchingDirectMigratorIdentity(t *testing.T) {
	roles, err := directAppACLRoleSet("houfeng_center_runtime", "houfeng_platform_admin", "houfeng_migrator", "houfeng_migrator")
	if err != nil {
		t.Fatalf("directAppACLRoleSet() valid direct identity error = %v", err)
	}
	if roles.CenterRuntime != "houfeng_center_runtime" || roles.PlatformAdmin != "houfeng_platform_admin" || roles.Migrator != "houfeng_migrator" {
		t.Fatalf("directAppACLRoleSet() = %#v, want direct migrator role binding", roles)
	}

	for _, tc := range []struct {
		name        string
		sessionUser string
		currentUser string
		want        string
	}{
		{name: "set_role", sessionUser: "member_login", currentUser: "houfeng_migrator", want: "session user"},
		{name: "unrelated_current_role", sessionUser: "houfeng_migrator", currentUser: "other_role", want: "session user"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := directAppACLRoleSet("houfeng_center_runtime", "houfeng_platform_admin", tc.sessionUser, tc.currentUser)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("directAppACLRoleSet() error = %v, want %q rejection", err, tc.want)
			}
		})
	}
}

func TestRetryAppACLConvergenceRetriesOnlyFullSerializationClosure(t *testing.T) {
	attempts := 0
	err := retryAppACLConvergence(context.Background(), func() error {
		attempts++
		if attempts == 1 {
			return &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryAppACLConvergence() serialization retry error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("serialization closure attempts = %d, want 2", attempts)
	}

	drift := errors.New("catalog drift")
	attempts = 0
	err = retryAppACLConvergence(context.Background(), func() error {
		attempts++
		return drift
	})
	if !errors.Is(err, drift) {
		t.Fatalf("retryAppACLConvergence() ordinary drift error = %v, want wrapped original", err)
	}
	if attempts != 1 {
		t.Fatalf("ordinary drift closure attempts = %d, want 1", attempts)
	}
}

func TestDefaultAppACLConvergenceDependenciesUseNonlockingPhaseHeadInspection(t *testing.T) {
	dependencies := defaultAppACLConvergenceDependencies()
	tx := &recordingAppACLHeadQueryTx{}
	if _, err := dependencies.readHead(context.Background(), tx); err == nil {
		t.Fatal("phase-head reader unexpectedly accepted a synthetic no-row result")
	}
	if strings.Contains(strings.ToLower(tx.query), "for update") {
		t.Fatalf("phase-head query = %q, must not lock before ledger validation", tx.query)
	}
}

func TestReadEmbeddedMigrationNameFromNonPublicLedgerInTxReleasesSavepointAfterPermissionDeniedProbe(t *testing.T) {
	probeErr := &pgconn.PgError{Code: "42501", Message: "permission denied for table schema_migrations"}
	tx := &nonPublicLedgerProbeErrorTx{queryErr: probeErr}

	_, err := readEmbeddedMigrationNameFromNonPublicLedgerInTx(
		context.Background(),
		tx,
		"third_party_private_history",
		migrationSourceSnapshot{},
	)
	if !isAppACLConvergencePermissionDenied(err) {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, want preserved SQLSTATE 42501 probe error", err)
	}
	var gotProbeErr *pgconn.PgError
	if !errors.As(err, &gotProbeErr) || gotProbeErr != probeErr {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, want original probe error %v", err, probeErr)
	}
	if want := []string{
		"savepoint app_acl_non_public_ledger_probe",
		"rollback to savepoint app_acl_non_public_ledger_probe",
		"release savepoint app_acl_non_public_ledger_probe",
	}; !equalStringSlices(tx.execSQL, want) {
		t.Fatalf("non-public ledger probe savepoint SQL = %#v, want %#v", tx.execSQL, want)
	}
}

func TestReadEmbeddedMigrationNameFromNonPublicLedgerInTxFailsClosedWhenErrorSavepointReleaseFails(t *testing.T) {
	probeErr := &pgconn.PgError{Code: "42501", Message: "permission denied for table schema_migrations"}
	releaseErr := errors.New("release savepoint failed")
	tx := &nonPublicLedgerProbeErrorTx{
		queryErr:   probeErr,
		releaseErr: releaseErr,
	}

	_, err := readEmbeddedMigrationNameFromNonPublicLedgerInTx(
		context.Background(),
		tx,
		"third_party_private_history",
		migrationSourceSnapshot{},
	)
	if err == nil || !strings.Contains(err.Error(), "release non-public migration ledger probe") {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, want contextual release failure", err)
	}
	if !errors.Is(err, releaseErr) {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, want wrapped release failure %v", err, releaseErr)
	}
	if isAppACLConvergencePermissionDenied(err) {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, must not leave a permission error that caller would ignore after failed cleanup", err)
	}
}

func TestReadEmbeddedMigrationNameFromNonPublicLedgerInTxFailsClosedWhenErrorSavepointRollbackFails(t *testing.T) {
	probeErr := &pgconn.PgError{Code: "42501", Message: "permission denied for table schema_migrations"}
	rollbackErr := &pgconn.PgError{Code: "42501", Message: "permission denied to roll back savepoint"}
	tx := &nonPublicLedgerProbeErrorTx{
		queryErr:    probeErr,
		rollbackErr: rollbackErr,
	}

	_, err := readEmbeddedMigrationNameFromNonPublicLedgerInTx(
		context.Background(),
		tx,
		"third_party_private_history",
		migrationSourceSnapshot{},
	)
	if err == nil || !strings.Contains(err.Error(), "rollback non-public migration ledger probe") {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, want contextual rollback failure", err)
	}
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, want wrapped rollback failure %v", err, rollbackErr)
	}
	if !isAppACLConvergencePermissionDenied(err) {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, want rollback SQLSTATE 42501 preserved", err)
	}
	if isAppACLNonPublicLedgerProbePermissionDenied(err) {
		t.Fatalf("readEmbeddedMigrationNameFromNonPublicLedgerInTx() error = %v, must not mark an uncleaned probe error as ignorable", err)
	}
	if want := []string{
		"savepoint app_acl_non_public_ledger_probe",
		"rollback to savepoint app_acl_non_public_ledger_probe",
	}; !equalStringSlices(tx.execSQL, want) {
		t.Fatalf("non-public ledger probe savepoint SQL = %#v, want %#v", tx.execSQL, want)
	}
}

func TestRejectNonPublicAppACLLegacyLedgerInTxIgnoresOnlyCleanedUpProbePermissionDenied(t *testing.T) {
	probeErr := &pgconn.PgError{Code: "42501", Message: "permission denied for table schema_migrations"}
	for _, tc := range []struct {
		name             string
		rollbackErr      error
		releaseErr       error
		wantErrorContext string
		wantExecSQL      []string
	}{
		{
			name: "probe_permission_denied_after_successful_cleanup_is_ignored",
			wantExecSQL: []string{
				"savepoint app_acl_non_public_ledger_probe",
				"rollback to savepoint app_acl_non_public_ledger_probe",
				"release savepoint app_acl_non_public_ledger_probe",
			},
		},
		{
			name:             "release_permission_denied_fails_closed",
			releaseErr:       &pgconn.PgError{Code: "42501", Message: "permission denied to release savepoint"},
			wantErrorContext: "release non-public migration ledger probe",
			wantExecSQL: []string{
				"savepoint app_acl_non_public_ledger_probe",
				"rollback to savepoint app_acl_non_public_ledger_probe",
				"release savepoint app_acl_non_public_ledger_probe",
			},
		},
		{
			name:             "rollback_permission_denied_fails_closed_without_release",
			rollbackErr:      &pgconn.PgError{Code: "42501", Message: "permission denied to roll back savepoint"},
			wantErrorContext: "rollback non-public migration ledger probe",
			wantExecSQL: []string{
				"savepoint app_acl_non_public_ledger_probe",
				"rollback to savepoint app_acl_non_public_ledger_probe",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &nonPublicLedgerContinuationTx{
				probeErr:    probeErr,
				rollbackErr: tc.rollbackErr,
				releaseErr:  tc.releaseErr,
			}
			err := rejectNonPublicAppACLLegacyLedgerInTx(
				context.Background(),
				tx,
				migrationSourceSnapshot{},
				"houfeng",
				"houfeng_migrator",
			)
			if tc.wantErrorContext != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrorContext) {
					t.Fatalf("rejectNonPublicAppACLLegacyLedgerInTx() error = %v, want fail-closed %s error", err, tc.wantErrorContext)
				}
				if tc.rollbackErr != nil && !errors.Is(err, tc.rollbackErr) {
					t.Fatalf("rejectNonPublicAppACLLegacyLedgerInTx() error = %v, want wrapped rollback error %v", err, tc.rollbackErr)
				}
				if tc.releaseErr != nil && !errors.Is(err, tc.releaseErr) {
					t.Fatalf("rejectNonPublicAppACLLegacyLedgerInTx() error = %v, want wrapped release error %v", err, tc.releaseErr)
				}
			} else if err != nil {
				t.Fatalf("rejectNonPublicAppACLLegacyLedgerInTx() cleaned-up probe error = %v, want tolerated foreign ledger", err)
			}
			if !equalStringSlices(tx.execSQL, tc.wantExecSQL) {
				t.Fatalf("%s savepoint SQL = %#v, want %#v", tc.name, tx.execSQL, tc.wantExecSQL)
			}
		})
	}
}

func TestConvergeAppACLR1WithDependenciesRejectsNonR1SourceSetBeforeBeginningTransaction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(fstest.MapFS)
	}{
		{
			name: "future_migration",
			mutate: func(fsys fstest.MapFS) {
				fsys["0052_future_r1_escape.sql"] = &fstest.MapFile{Data: []byte("select 'future migration';")}
			},
		},
		{
			name: "missing_r1_migration",
			mutate: func(fsys fstest.MapFS) {
				delete(fsys, "0051_create_record_platform_foundation.sql")
			},
		},
		{
			name: "changed_r1_migration_bytes",
			mutate: func(fsys fstest.MapFS) {
				fsys["0001_initial_schema.sql"].Data = append(
					append([]byte(nil), fsys["0001_initial_schema.sql"].Data...),
					[]byte("\n-- changed after r1 freeze\n")...,
				)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := appACLR1InjectedMigrationFS(t)
			tc.mutate(fsys)
			sources, err := snapshotMigrationSources(fsys)
			if err != nil {
				t.Fatalf("snapshotMigrationSources() error = %v", err)
			}

			beginCalls := 0
			_, err = convergeAppACLR1WithDependencies(
				context.Background(),
				func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					beginCalls++
					return nil, errors.New("begin must not run for a non-r1 source set")
				},
				"houfeng_center_runtime",
				"houfeng_platform_admin",
				sources,
				defaultAppACLConvergenceDependencies(),
			)
			if err == nil || !strings.Contains(err.Error(), "frozen r1 migration source") {
				t.Fatalf("convergeAppACLR1WithDependencies() error = %v, want frozen r1 source rejection", err)
			}
			if beginCalls != 0 {
				t.Fatalf("BeginTx calls = %d, want rejection before beginning a transaction", beginCalls)
			}
		})
	}
}

func TestConvergeAppACLR1WithDependenciesAcceptsExactR1SourceSetAtTransactionBoundary(t *testing.T) {
	sources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatalf("snapshotMigrationSources() error = %v", err)
	}

	beginCalls := 0
	_, err = convergeAppACLR1WithDependencies(
		context.Background(),
		func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			beginCalls++
			return nil, errors.New("begin sentinel")
		},
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		sources,
		defaultAppACLConvergenceDependencies(),
	)
	if err == nil || !strings.Contains(err.Error(), "begin sentinel") {
		t.Fatalf("convergeAppACLR1WithDependencies() error = %v, want exact r1 source set to reach BeginTx", err)
	}
	if beginCalls != 1 {
		t.Fatalf("BeginTx calls = %d, want 1 for the exact r1 source set", beginCalls)
	}
}

func appACLR1InjectedMigrationFS(t *testing.T) fstest.MapFS {
	t.Helper()
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatalf("read embedded migration entries: %v", err)
	}
	fsys := make(fstest.MapFS, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		data, err := migrations.FS.ReadFile(entry.Name())
		if err != nil {
			t.Fatalf("read embedded migration %q: %v", entry.Name(), err)
		}
		fsys[entry.Name()] = &fstest.MapFile{Data: data}
	}
	return fsys
}

func TestPendingMigrationSourceNamesRequireExactAppliedPrefix(t *testing.T) {
	checksumOne := appACLConvergenceChecksum(t, "11")
	checksumTwo := appACLConvergenceChecksum(t, "22")
	checksumThree := appACLConvergenceChecksum(t, "33")
	snapshot := migrationSourceSnapshot{
		names: []string{
			"0001_initial_schema.sql",
			"0002_second.sql",
			"0003_third.sql",
		},
		sources: map[string]migrationSource{
			"0001_initial_schema.sql": {checksum: strings.Repeat("11", 32)},
			"0002_second.sql":         {checksum: strings.Repeat("22", 32)},
			"0003_third.sql":          {checksum: strings.Repeat("33", 32)},
		},
	}

	pending, err := pendingMigrationSourceNames(snapshot, []MigrationChecksumEntry{
		{Filename: "0001_initial_schema.sql", Checksum: checksumOne},
		{Filename: "0002_second.sql", Checksum: checksumTwo},
	})
	if err != nil {
		t.Fatalf("pendingMigrationSourceNames() valid prefix error = %v", err)
	}
	if want := []string{"0003_third.sql"}; !equalStringSlices(pending, want) {
		t.Fatalf("pending migration names = %#v, want %#v", pending, want)
	}

	for _, tc := range []struct {
		name    string
		applied []MigrationChecksumEntry
		want    string
	}{
		{
			name: "ledger_hole",
			applied: []MigrationChecksumEntry{
				{Filename: "0001_initial_schema.sql", Checksum: checksumOne},
				{Filename: "0003_third.sql", Checksum: checksumThree},
			},
			want: "exact prefix",
		},
		{
			name:    "unknown_source",
			applied: []MigrationChecksumEntry{{Filename: "0000_unknown.sql", Checksum: checksumOne}},
			want:    "exact prefix",
		},
		{
			name:    "checksum_drift",
			applied: []MigrationChecksumEntry{{Filename: "0001_initial_schema.sql", Checksum: checksumTwo}},
			want:    "checksum mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pendingMigrationSourceNames(snapshot, tc.applied)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("pendingMigrationSourceNames() error = %v, want %q rejection", err, tc.want)
			}
		})
	}
}

func TestApplyPendingMigrationSourcesInTxSwitchesToPublicOnlyForMissingHistoricalSQL(t *testing.T) {
	checksumOne := strings.Repeat("11", 32)
	checksumTwo := strings.Repeat("22", 32)
	checksumThree := strings.Repeat("33", 32)
	snapshot := migrationSourceSnapshot{
		names: []string{"0001_initial_schema.sql", "0002_second.sql", "0003_third.sql"},
		sources: map[string]migrationSource{
			"0001_initial_schema.sql": {checksum: checksumOne, sql: "select 'first migration';"},
			"0002_second.sql":         {checksum: checksumTwo, sql: "select 'second migration';"},
			"0003_third.sql":          {checksum: checksumThree, sql: "select 'third migration';"},
		},
	}
	tx := &fakeMigrationLedgerTx{execSQL: []string{
		`set local search_path = pg_catalog, public`,
	}}
	applied := []MigrationChecksumEntry{{
		Filename: "0001_initial_schema.sql",
		Checksum: appACLConvergenceChecksum(t, "11"),
	}}

	if err := applyPendingMigrationSourcesInTx(context.Background(), tx, snapshot, applied); err != nil {
		t.Fatalf("applyPendingMigrationSourcesInTx() error = %v", err)
	}
	wantSQL := []string{
		`set local search_path = pg_catalog, public`,
		`set local search_path = public`,
		`select 'second migration';`,
		`insert into public.schema_migrations (name, checksum) values ($1, $2)`,
		`set local search_path = pg_catalog, public`,
		`set local search_path = public`,
		`select 'third migration';`,
		`insert into public.schema_migrations (name, checksum) values ($1, $2)`,
		`set local search_path = pg_catalog, public`,
	}
	if !equalStringSlices(tx.execSQL, wantSQL) {
		t.Fatalf("pending migration SQL = %#v, want exact public-only migration scope %#v", tx.execSQL, wantSQL)
	}
	joined := strings.Join(tx.execSQL, "\n")
	if strings.Contains(joined, "first migration") || strings.Contains(joined, "$user") {
		t.Fatalf("pending migration SQL = %q, applied prefix or caller schema leaked into migration scope", joined)
	}
	if tx.beginCalled {
		t.Fatal("applyPendingMigrationSourcesInTx() started a nested transaction")
	}
}

func TestCheckAppACLManifestGenesisStateAllowsOnlyFreshOrExactR1(t *testing.T) {
	_, snapshot, compiledPrivileges := validAppACLManifestRuntimeFixture(t)
	genesis := snapshot.Manifests[0]
	head := *snapshot.Head

	fresh, err := checkAppACLManifestGenesisStateV1(nil, nil, genesis.CanonicalMigrationSet, compiledPrivileges, genesis.MigratorCatalogRole)
	if err != nil {
		t.Fatalf("checkAppACLManifestGenesisStateV1() fresh error = %v", err)
	}
	if fresh != nil {
		t.Fatalf("fresh manifest state = %#v, want nil genesis precondition", fresh)
	}

	existing, err := checkAppACLManifestGenesisStateV1([]AppACLManifestPersistedV1{genesis}, &head, genesis.CanonicalMigrationSet, compiledPrivileges, genesis.MigratorCatalogRole)
	if err != nil {
		t.Fatalf("checkAppACLManifestGenesisStateV1() exact r1 error = %v", err)
	}
	if existing == nil || existing.ManifestDigest != genesis.ManifestDigest {
		t.Fatalf("exact r1 manifest = %#v, want %#v", existing, genesis)
	}

	for _, tc := range []struct {
		name      string
		manifests []AppACLManifestPersistedV1
		head      *AppACLManifestHeadV1
		migrator  string
		want      string
	}{
		{name: "orphan_revision", manifests: []AppACLManifestPersistedV1{genesis}, want: "null head"},
		{name: "migrator_drift", manifests: []AppACLManifestPersistedV1{genesis}, head: &head, migrator: "other_migrator", want: "migrator catalog role"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrator := tc.migrator
			if migrator == "" {
				migrator = genesis.MigratorCatalogRole
			}
			_, err := checkAppACLManifestGenesisStateV1(tc.manifests, tc.head, genesis.CanonicalMigrationSet, compiledPrivileges, migrator)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("checkAppACLManifestGenesisStateV1() error = %v, want %q rejection", err, tc.want)
			}
		})
	}
}

func TestAppACLConvergenceDCLStatementsRevokeFixedSurfaceAndGrantOnlyCompilerTuples(t *testing.T) {
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
	contract, err := CompileAppACLEffectiveCatalogContractR1("houfeng", bindings)
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}

	statements, err := appACLConvergenceDCLStatements(contract)
	if err != nil {
		t.Fatalf("appACLConvergenceDCLStatements() error = %v", err)
	}
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		`revoke all privileges on database "houfeng" from PUBLIC`,
		`revoke all privileges on schema "record_platform_internal" from "houfeng_center_runtime"`,
		`revoke all privileges on table "public"."schema_migrations" from "houfeng_platform_admin"`,
		`revoke all privileges on sequence "public"."host_samples_id_seq" from "houfeng_center_runtime"`,
		`revoke all privileges on function "public"."record_platform_cas_contract_activation_projection"(bytea) from PUBLIC`,
		`grant CONNECT on database "houfeng" to "houfeng_center_runtime"`,
		`grant SELECT on table "public"."schema_migrations" to "houfeng_platform_admin"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("DCL statements do not contain %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "grant EXECUTE on function") {
		t.Fatalf("DCL statements grant persistent function execute:\n%s", joined)
	}

	grantCount := 0
	for _, statement := range statements {
		if strings.HasPrefix(statement, "grant ") {
			grantCount++
		}
	}
	if grantCount != appACLEffectiveCatalogR1PrivilegeCount {
		t.Fatalf("DCL grant count = %d, want %d compiler tuples", grantCount, appACLEffectiveCatalogR1PrivilegeCount)
	}
}

func TestConvergeAppACLR1WithDependenciesRetriesWholeSerializableTransaction(t *testing.T) {
	_, runtimeSnapshot, compiledPrivileges := validAppACLManifestRuntimeFixture(t)
	sources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatalf("snapshotMigrationSources() error = %v", err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(sources.canonicalSet)
	if err != nil {
		t.Fatalf("ParseCanonicalMigrationSetBodyV1() error = %v", err)
	}
	genesis, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, sources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}
	runtimeSnapshot.Manifests = []AppACLManifestPersistedV1{genesis}
	runtimeSnapshot.Head = &AppACLManifestHeadV1{ManifestRevision: genesis.ManifestRevision, ManifestDigest: genesis.ManifestDigest}
	runtimeSnapshot.AppliedMigrations = applied

	var txs []*fakeAppACLConvergenceTx
	var beginOptions []pgx.TxOptions
	begin := func(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		beginOptions = append(beginOptions, options)
		tx := &fakeAppACLConvergenceTx{}
		if len(txs) == 0 {
			tx.commitErr = &pgconn.PgError{Code: "40001", Message: "serialization failure"}
		}
		txs = append(txs, tx)
		return tx, nil
	}

	steps := make([]string, 0, 16)
	dependencies := appACLConvergenceDependencies{
		readDatabaseName: func(context.Context, pgx.Tx) (string, error) {
			steps = append(steps, "database")
			return "houfeng", nil
		},
		resolveRoles: func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
			steps = append(steps, "roles")
			return platformmigrate.AppRoleSetV1{
				CenterRuntime: "houfeng_center_runtime",
				PlatformAdmin: "houfeng_platform_admin",
				Migrator:      "houfeng_migrator",
			}, nil
		},
		readPhaseState: func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
			steps = append(steps, "phase")
			return appACLConvergencePhaseState{
				LedgerExists:            true,
				ManifestRevisionsExists: true,
				ManifestHeadExists:      true,
			}, nil
		},
		rejectMisplaced: func(context.Context, pgx.Tx, string) error {
			steps = append(steps, "placement")
			return nil
		},
		lockLedger: func(ctx context.Context, tx pgx.Tx) error {
			steps = append(steps, "ledger-lock")
			_, err := tx.Exec(ctx, `lock table public.schema_migrations in share row exclusive mode`)
			return err
		},
		rejectLegacy: func(context.Context, pgx.Tx, migrationSourceSnapshot, string, string) error {
			steps = append(steps, "legacy")
			return nil
		},
		rejectFresh: func(context.Context, pgx.Tx, string) error {
			t.Fatal("exact persisted r1 repeat must not enter fresh-state preflight")
			return nil
		},
		ensureLedger: func(context.Context, pgx.Tx, map[string]migrationSource) error {
			t.Fatal("exact persisted r1 repeat must not mutate the ledger")
			return nil
		},
		readApplied: func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
			steps = append(steps, "applied")
			return runtimeSnapshot.AppliedMigrations, nil
		},
		applyPending: func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
			t.Fatal("exact persisted r1 repeat must not apply pending migrations")
			return nil
		},
		readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
			steps = append(steps, "revisions")
			return []AppACLManifestPersistedV1{genesis}, nil
		},
		readHead: func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
			steps = append(steps, "head-inspect")
			if _, err := tx.Exec(ctx, `select 'app ACL phase head inspect sentinel'`); err != nil {
				return nil, err
			}
			head := *runtimeSnapshot.Head
			return &head, nil
		},
		readHeadForUpdate: func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
			steps = append(steps, "head-lock")
			if _, err := tx.Exec(ctx, `select 'app ACL final head lock sentinel' for update`); err != nil {
				return nil, err
			}
			head := *runtimeSnapshot.Head
			return &head, nil
		},
		applyDCL: func(context.Context, pgx.Tx, AppACLEffectiveCatalogContractR1) error {
			t.Fatal("exact persisted r1 repeat must not apply DCL")
			return nil
		},
		readCatalog: func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			steps = append(steps, "catalog")
			return AppACLEffectiveCatalogSnapshotR1{}, nil
		},
		verifyCatalog: func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error {
			steps = append(steps, "verify")
			return nil
		},
		insertGenesis: func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
			t.Fatal("exact persisted r1 repeat must not insert a manifest")
			return AppACLManifestPersistedV1{}, nil
		},
	}

	manifest, err := convergeAppACLR1WithDependencies(
		context.Background(),
		begin,
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		sources,
		dependencies,
	)
	if err != nil {
		t.Fatalf("convergeAppACLR1WithDependencies() error = %v", err)
	}
	if manifest.ManifestDigest != genesis.ManifestDigest {
		t.Fatalf("converged manifest digest = %x, want %x", manifest.ManifestDigest, genesis.ManifestDigest)
	}
	if len(txs) != 2 {
		t.Fatalf("SERIALIZABLE transaction attempts = %d, want 2", len(txs))
	}
	if len(beginOptions) != 2 || beginOptions[0].IsoLevel != pgx.Serializable || beginOptions[1].IsoLevel != pgx.Serializable {
		t.Fatalf("begin options = %#v, want two SERIALIZABLE transactions", beginOptions)
	}
	for attempt, tx := range txs {
		joined := strings.Join(tx.execSQL, "\n")
		searchPath := strings.Index(joined, "set local search_path = pg_catalog, public")
		advisoryLock := strings.Index(joined, "pg_advisory_xact_lock")
		phaseHeadInspect := strings.Index(joined, "select 'app ACL phase head inspect sentinel'")
		ledgerLock := strings.Index(joined, "lock table public.schema_migrations in share row exclusive mode")
		revisionsLock := strings.Index(joined, "lock table public.app_acl_manifest_revisions in share row exclusive mode")
		headLock := strings.Index(joined, "select 'app ACL final head lock sentinel' for update")
		if searchPath < 0 || advisoryLock < 0 || phaseHeadInspect < 0 || ledgerLock < 0 || revisionsLock < 0 || headLock < 0 || searchPath > advisoryLock || advisoryLock > phaseHeadInspect || phaseHeadInspect > ledgerLock || ledgerLock > revisionsLock || revisionsLock > headLock {
			t.Fatalf("attempt %d setup SQL order = %q, want advisory -> nonlocking phase head -> ledger -> revisions -> final head lock", attempt+1, joined)
		}
		if strings.Contains(joined, "app ACL phase head inspect sentinel' for update") {
			t.Fatalf("attempt %d phase-head inspection unexpectedly locks: %q", attempt+1, joined)
		}
		if tx.nestedBeginCalls != 0 {
			t.Fatalf("attempt %d nested transaction begins = %d, want 0", attempt+1, tx.nestedBeginCalls)
		}
	}
	wantSteps := []string{
		"roles", "database", "placement", "legacy", "phase", "head-inspect", "ledger-lock", "applied", "head-lock", "revisions", "catalog", "verify",
		"roles", "database", "placement", "legacy", "phase", "head-inspect", "ledger-lock", "applied", "head-lock", "revisions", "catalog", "verify",
	}
	if !equalStringSlices(steps, wantSteps) {
		t.Fatalf("convergence operation order = %#v, want %#v", steps, wantSteps)
	}
}

func TestConvergeAppACLR1InTxRejectsPhaseHeadChangeAfterLedgerProof(t *testing.T) {
	_, _, compiledPrivileges := validAppACLManifestRuntimeFixture(t)
	sources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatalf("snapshotMigrationSources() error = %v", err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(sources.canonicalSet)
	if err != nil {
		t.Fatalf("ParseCanonicalMigrationSetBodyV1() error = %v", err)
	}
	genesis, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, sources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}
	existingHead := &AppACLManifestHeadV1{
		ManifestRevision: genesis.ManifestRevision,
		ManifestDigest:   genesis.ManifestDigest,
	}
	changedHead := cloneAppACLManifestHeadForTest(existingHead)
	changedHead.ManifestDigest = appACLConvergenceChecksum(t, "ab")

	for _, tt := range []struct {
		name       string
		phaseHead  *AppACLManifestHeadV1
		finalHead  *AppACLManifestHeadV1
		ledgerStep string
	}{
		{
			name:       "null_head_adoption_becomes_existing_r1",
			phaseHead:  nil,
			finalHead:  existingHead,
			ledgerStep: "ledger-adoption",
		},
		{
			name:       "existing_r1_repeat_loses_its_head",
			phaseHead:  existingHead,
			finalHead:  nil,
			ledgerStep: "ledger-lock",
		},
		{
			name:       "existing_r1_repeat_head_digest_changes",
			phaseHead:  existingHead,
			finalHead:  changedHead,
			ledgerStep: "ledger-lock",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeAppACLConvergenceTx{}
			steps := make([]string, 0, 12)
			dependencies := appACLConvergenceDependencies{
				readDatabaseName: func(context.Context, pgx.Tx) (string, error) {
					steps = append(steps, "database")
					return "houfeng", nil
				},
				resolveRoles: func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
					steps = append(steps, "roles")
					return platformmigrate.AppRoleSetV1{
						CenterRuntime: "houfeng_center_runtime",
						PlatformAdmin: "houfeng_platform_admin",
						Migrator:      "houfeng_migrator",
					}, nil
				},
				readPhaseState: func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
					steps = append(steps, "phase")
					return appACLConvergencePhaseState{
						LedgerExists:            true,
						ManifestRevisionsExists: true,
						ManifestHeadExists:      true,
					}, nil
				},
				rejectMisplaced: func(context.Context, pgx.Tx, string) error {
					steps = append(steps, "placement")
					return nil
				},
				lockLedger: func(ctx context.Context, tx pgx.Tx) error {
					if tt.phaseHead == nil {
						t.Fatal("null-head adoption must validate and backfill the ledger through ensureLedger")
					}
					steps = append(steps, "ledger-lock")
					_, err := tx.Exec(ctx, `lock table public.schema_migrations in share row exclusive mode`)
					return err
				},
				rejectLegacy: func(context.Context, pgx.Tx, migrationSourceSnapshot, string, string) error {
					steps = append(steps, "legacy")
					return nil
				},
				rejectFresh: func(context.Context, pgx.Tx, string) error {
					t.Fatal("manifest-table branches must not enter fresh-state preflight")
					return nil
				},
				ensureLedger: func(ctx context.Context, tx pgx.Tx, _ map[string]migrationSource) error {
					if tt.phaseHead != nil {
						t.Fatal("existing r1 repeat must not mutate the ledger")
					}
					steps = append(steps, "ledger-adoption")
					if _, err := tx.Exec(ctx, `lock table public.schema_migrations in share row exclusive mode`); err != nil {
						return err
					}
					_, err := tx.Exec(ctx, `select 'app ACL ledger checksum backfill sentinel'`)
					return err
				},
				readApplied: func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
					steps = append(steps, "applied")
					return applied, nil
				},
				applyPending: func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
					t.Fatal("manifest-table branches must not apply pending migrations")
					return nil
				},
				readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
					t.Fatal("final head change must be rejected before manifest-state handling")
					return nil, nil
				},
				readHead: func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
					steps = append(steps, "head-inspect")
					if _, err := tx.Exec(ctx, `select 'app ACL phase head inspect sentinel'`); err != nil {
						return nil, err
					}
					return cloneAppACLManifestHeadForTest(tt.phaseHead), nil
				},
				readHeadForUpdate: func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
					steps = append(steps, "head-lock")
					if _, err := tx.Exec(ctx, `select 'app ACL final head lock sentinel' for update`); err != nil {
						return nil, err
					}
					return cloneAppACLManifestHeadForTest(tt.finalHead), nil
				},
				applyDCL: func(context.Context, pgx.Tx, AppACLEffectiveCatalogContractR1) error {
					t.Fatal("final head change must be rejected before ACL convergence")
					return nil
				},
				readCatalog: func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
					t.Fatal("final head change must be rejected before catalog verification")
					return AppACLEffectiveCatalogSnapshotR1{}, nil
				},
				verifyCatalog: func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error {
					t.Fatal("final head change must be rejected before catalog verification")
					return nil
				},
				insertGenesis: func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
					t.Fatal("final head change must be rejected before manifest insertion")
					return AppACLManifestPersistedV1{}, nil
				},
			}

			_, err := convergeAppACLR1InTx(
				context.Background(),
				tx,
				"houfeng_center_runtime",
				"houfeng_platform_admin",
				sources,
				dependencies,
			)
			if err == nil || !strings.Contains(err.Error(), "app ACL manifest head changed after phase inspection") {
				t.Fatalf("convergeAppACLR1InTx() error = %v, want final-head consistency rejection", err)
			}

			wantSteps := []string{"roles", "database", "placement", "legacy", "phase", "head-inspect", tt.ledgerStep, "applied", "head-lock"}
			if !equalStringSlices(steps, wantSteps) {
				t.Fatalf("%s operation order = %#v, want %#v", tt.name, steps, wantSteps)
			}
			assertSQLFragmentsInOrder(t, tx.execSQL, []string{
				appACLConvergenceHardenedSearchPathSQL,
				"pg_advisory_xact_lock",
				"app ACL phase head inspect sentinel",
				"lock table public.schema_migrations in share row exclusive mode",
				"lock table public.app_acl_manifest_revisions in share row exclusive mode",
				"app ACL final head lock sentinel' for update",
			})
			if strings.Contains(strings.Join(tx.execSQL, "\n"), "app ACL phase head inspect sentinel' for update") {
				t.Fatalf("%s phase-head inspection unexpectedly locks: %#v", tt.name, tx.execSQL)
			}
		})
	}
}

func cloneAppACLManifestHeadForTest(head *AppACLManifestHeadV1) *AppACLManifestHeadV1 {
	if head == nil {
		return nil
	}
	copy := *head
	return &copy
}

func TestConvergeAppACLR1InTxRejectsFreshPhaseHeadChangeBeforeDCL(t *testing.T) {
	_, runtimeSnapshot, _ := validAppACLManifestRuntimeFixture(t)
	genesis := runtimeSnapshot.Manifests[0]
	sources, err := snapshotMigrationSources(fstest.MapFS{
		"0001_initial_schema.sql": {Data: []byte("select 'fresh phase pending migration sentinel';")},
	})
	if err != nil {
		t.Fatalf("snapshotMigrationSources() error = %v", err)
	}
	sources.canonicalSet = genesis.CanonicalMigrationSet

	tx := &fakeAppACLConvergenceTx{}
	steps := make([]string, 0, 14)
	dependencies := appACLConvergenceDependencies{
		readDatabaseName: func(context.Context, pgx.Tx) (string, error) {
			steps = append(steps, "database")
			return "houfeng", nil
		},
		resolveRoles: func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
			steps = append(steps, "roles")
			return platformmigrate.AppRoleSetV1{
				CenterRuntime: "houfeng_center_runtime",
				PlatformAdmin: "houfeng_platform_admin",
				Migrator:      "houfeng_migrator",
			}, nil
		},
		readPhaseState: func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
			steps = append(steps, "phase")
			return appACLConvergencePhaseState{}, nil
		},
		rejectMisplaced: func(context.Context, pgx.Tx, string) error {
			steps = append(steps, "placement")
			return nil
		},
		lockLedger: func(context.Context, pgx.Tx) error {
			t.Fatal("fresh state must create and lock its ledger through ensureLedger")
			return nil
		},
		rejectLegacy: func(context.Context, pgx.Tx, migrationSourceSnapshot, string, string) error {
			steps = append(steps, "legacy")
			return nil
		},
		rejectFresh: func(context.Context, pgx.Tx, string) error {
			steps = append(steps, "fresh")
			return nil
		},
		ensureLedger: func(ctx context.Context, tx pgx.Tx, _ map[string]migrationSource) error {
			steps = append(steps, "ledger")
			_, err := tx.Exec(ctx, `lock table public.schema_migrations in share row exclusive mode`)
			return err
		},
		readApplied: func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
			steps = append(steps, "applied")
			return nil, nil
		},
		applyPending: func(ctx context.Context, tx pgx.Tx, snapshot migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
			steps = append(steps, "pending")
			return applyPendingMigrationSourcesInTx(ctx, tx, snapshot, applied)
		},
		readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
			t.Fatal("fresh phase head change must be rejected before manifest-state handling")
			return nil, nil
		},
		readHead: func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
			t.Fatal("fresh state must not read a phase head before pending migrations create it")
			return nil, nil
		},
		readHeadForUpdate: func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
			steps = append(steps, "head-lock")
			if _, err := tx.Exec(ctx, `select 'app ACL final head lock sentinel' for update`); err != nil {
				return nil, err
			}
			return cloneAppACLManifestHeadForTest(&AppACLManifestHeadV1{
				ManifestRevision: genesis.ManifestRevision,
				ManifestDigest:   genesis.ManifestDigest,
			}), nil
		},
		applyDCL: func(context.Context, pgx.Tx, AppACLEffectiveCatalogContractR1) error {
			t.Fatal("fresh phase head change must be rejected before ACL convergence")
			return nil
		},
		readCatalog: func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			t.Fatal("fresh phase head change must be rejected before catalog verification")
			return AppACLEffectiveCatalogSnapshotR1{}, nil
		},
		verifyCatalog: func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error {
			t.Fatal("fresh phase head change must be rejected before catalog verification")
			return nil
		},
		insertGenesis: func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
			t.Fatal("fresh phase head change must be rejected before manifest insertion")
			return AppACLManifestPersistedV1{}, nil
		},
	}

	_, err = convergeAppACLR1InTx(
		context.Background(),
		tx,
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		sources,
		dependencies,
	)
	if err == nil || !strings.Contains(err.Error(), "app ACL manifest head changed after phase inspection") {
		t.Fatalf("convergeAppACLR1InTx() error = %v, want fresh final-head consistency rejection", err)
	}
	wantSteps := []string{"roles", "database", "placement", "legacy", "phase", "fresh", "ledger", "applied", "pending", "head-lock"}
	if !equalStringSlices(steps, wantSteps) {
		t.Fatalf("fresh head-change operation order = %#v, want %#v", steps, wantSteps)
	}
	assertSQLFragmentsInOrder(t, tx.execSQL, []string{
		appACLConvergenceHardenedSearchPathSQL,
		"pg_advisory_xact_lock",
		"lock table public.schema_migrations in share row exclusive mode",
		`set local search_path = public`,
		"fresh phase pending migration sentinel",
		"insert into public.schema_migrations (name, checksum) values ($1, $2)",
		appACLConvergenceHardenedSearchPathSQL,
		"lock table public.app_acl_manifest_revisions in share row exclusive mode",
		"app ACL final head lock sentinel' for update",
	})
}

func TestConvergeAppACLR1InTxAdoptsNullHeadAfterLedgerProof(t *testing.T) {
	_, _, compiledPrivileges := validAppACLManifestRuntimeFixture(t)
	sources, err := snapshotMigrationSources(migrations.FS)
	if err != nil {
		t.Fatalf("snapshotMigrationSources() error = %v", err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(sources.canonicalSet)
	if err != nil {
		t.Fatalf("ParseCanonicalMigrationSetBodyV1() error = %v", err)
	}
	genesis, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, sources.canonicalSet, compiledPrivileges)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}

	tx := &fakeAppACLConvergenceTx{}
	var manifests []AppACLManifestPersistedV1
	var head *AppACLManifestHeadV1
	steps := make([]string, 0, 20)
	dependencies := appACLConvergenceDependencies{
		readDatabaseName: func(context.Context, pgx.Tx) (string, error) {
			steps = append(steps, "database")
			return "houfeng", nil
		},
		resolveRoles: func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
			steps = append(steps, "roles")
			return platformmigrate.AppRoleSetV1{
				CenterRuntime: "houfeng_center_runtime",
				PlatformAdmin: "houfeng_platform_admin",
				Migrator:      "houfeng_migrator",
			}, nil
		},
		readPhaseState: func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
			steps = append(steps, "phase")
			return appACLConvergencePhaseState{
				LedgerExists:            true,
				ManifestRevisionsExists: true,
				ManifestHeadExists:      true,
			}, nil
		},
		rejectMisplaced: func(context.Context, pgx.Tx, string) error {
			steps = append(steps, "placement")
			return nil
		},
		lockLedger: func(context.Context, pgx.Tx) error {
			t.Fatal("null-head adoption must validate and backfill the ledger through ensureLedger")
			return nil
		},
		rejectLegacy: func(context.Context, pgx.Tx, migrationSourceSnapshot, string, string) error {
			steps = append(steps, "legacy")
			return nil
		},
		rejectFresh: func(context.Context, pgx.Tx, string) error {
			t.Fatal("null-head adoption must not enter fresh-state preflight")
			return nil
		},
		ensureLedger: func(ctx context.Context, tx pgx.Tx, _ map[string]migrationSource) error {
			steps = append(steps, "ledger-adoption")
			if _, err := tx.Exec(ctx, `lock table public.schema_migrations in share row exclusive mode`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `select 'app ACL ledger checksum backfill sentinel'`)
			return err
		},
		readApplied: func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
			steps = append(steps, "applied")
			return applied, nil
		},
		applyPending: func(context.Context, pgx.Tx, migrationSourceSnapshot, []MigrationChecksumEntry) error {
			t.Fatal("null-head adoption requires an exact ledger and must not apply pending migrations")
			return nil
		},
		readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
			steps = append(steps, "revisions")
			return append([]AppACLManifestPersistedV1(nil), manifests...), nil
		},
		readHead: func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
			steps = append(steps, "head-inspect")
			if _, err := tx.Exec(ctx, `select 'app ACL phase head inspect sentinel'`); err != nil {
				return nil, err
			}
			return nil, nil
		},
		readHeadForUpdate: func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
			steps = append(steps, "head-lock")
			if _, err := tx.Exec(ctx, `select 'app ACL final head lock sentinel' for update`); err != nil {
				return nil, err
			}
			return cloneAppACLManifestHeadForTest(head), nil
		},
		applyDCL: func(ctx context.Context, tx pgx.Tx, _ AppACLEffectiveCatalogContractR1) error {
			steps = append(steps, "dcl")
			_, err := tx.Exec(ctx, `select 'app ACL DCL sentinel'`)
			return err
		},
		readCatalog: func(ctx context.Context, tx pgx.Tx, _ AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			steps = append(steps, "catalog")
			if _, err := tx.Exec(ctx, `select 'app ACL catalog sentinel'`); err != nil {
				return AppACLEffectiveCatalogSnapshotR1{}, err
			}
			return AppACLEffectiveCatalogSnapshotR1{}, nil
		},
		verifyCatalog: func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error {
			steps = append(steps, "verify")
			return nil
		},
		insertGenesis: func(ctx context.Context, tx pgx.Tx, _ []byte, _ []byte, _ string) (AppACLManifestPersistedV1, error) {
			steps = append(steps, "insert")
			if _, err := tx.Exec(ctx, `select 'app ACL manifest insert sentinel'`); err != nil {
				return AppACLManifestPersistedV1{}, err
			}
			manifests = []AppACLManifestPersistedV1{genesis}
			head = &AppACLManifestHeadV1{ManifestRevision: genesis.ManifestRevision, ManifestDigest: genesis.ManifestDigest}
			return genesis, nil
		},
	}

	manifest, err := convergeAppACLR1InTx(
		context.Background(),
		tx,
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		sources,
		dependencies,
	)
	if err != nil {
		t.Fatalf("convergeAppACLR1InTx() null-head adoption error = %v", err)
	}
	if manifest.ManifestDigest != genesis.ManifestDigest {
		t.Fatalf("adopted manifest digest = %x, want %x", manifest.ManifestDigest, genesis.ManifestDigest)
	}
	wantSteps := []string{
		"roles", "database", "placement", "legacy", "phase", "head-inspect", "ledger-adoption", "applied", "head-lock", "revisions",
		"dcl", "catalog", "verify", "insert", "head-lock", "revisions", "catalog", "verify",
	}
	if !equalStringSlices(steps, wantSteps) {
		t.Fatalf("null-head adoption operation order = %#v, want %#v", steps, wantSteps)
	}
	assertSQLFragmentsInOrder(t, tx.execSQL, []string{
		appACLConvergenceHardenedSearchPathSQL,
		"pg_advisory_xact_lock",
		"app ACL phase head inspect sentinel",
		"lock table public.schema_migrations in share row exclusive mode",
		"app ACL ledger checksum backfill sentinel",
		"lock table public.app_acl_manifest_revisions in share row exclusive mode",
		"app ACL final head lock sentinel' for update",
		"app ACL DCL sentinel",
		"app ACL catalog sentinel",
		"app ACL manifest insert sentinel",
		"app ACL final head lock sentinel' for update",
	})
	joined := strings.Join(tx.execSQL, "\n")
	if strings.Contains(joined, "app ACL phase head inspect sentinel' for update") {
		t.Fatalf("null-head phase inspection unexpectedly locks: %q", joined)
	}
	if strings.Contains(joined, "$user") {
		t.Fatalf("null-head adoption SQL = %q, must not select the caller schema", joined)
	}
}

func TestConvergeAppACLR1InTxRestrictsPublicFirstSearchPathToPendingMigrations(t *testing.T) {
	_, runtimeSnapshot, _ := validAppACLManifestRuntimeFixture(t)
	genesis := runtimeSnapshot.Manifests[0]
	sources, err := snapshotMigrationSources(fstest.MapFS{
		"0001_initial_schema.sql": {Data: []byte("select 'pending migration sentinel';")},
	})
	if err != nil {
		t.Fatalf("snapshotMigrationSources() error = %v", err)
	}
	sources.canonicalSet = genesis.CanonicalMigrationSet
	tx := &fakeAppACLConvergenceTx{}
	var manifests []AppACLManifestPersistedV1
	var head *AppACLManifestHeadV1
	steps := make([]string, 0, 20)
	dependencies := appACLConvergenceDependencies{
		readDatabaseName: func(context.Context, pgx.Tx) (string, error) {
			steps = append(steps, "database")
			return "houfeng", nil
		},
		resolveRoles: func(context.Context, pgx.Tx, string, string) (platformmigrate.AppRoleSetV1, error) {
			steps = append(steps, "roles")
			return platformmigrate.AppRoleSetV1{
				CenterRuntime: "houfeng_center_runtime",
				PlatformAdmin: "houfeng_platform_admin",
				Migrator:      "houfeng_migrator",
			}, nil
		},
		readPhaseState: func(context.Context, pgx.Tx) (appACLConvergencePhaseState, error) {
			steps = append(steps, "phase")
			return appACLConvergencePhaseState{}, nil
		},
		rejectMisplaced: func(context.Context, pgx.Tx, string) error {
			steps = append(steps, "placement")
			return nil
		},
		lockLedger: func(context.Context, pgx.Tx) error {
			t.Fatal("fresh convergence must not lock an existing ledger")
			return nil
		},
		rejectLegacy: func(context.Context, pgx.Tx, migrationSourceSnapshot, string, string) error {
			steps = append(steps, "legacy")
			return nil
		},
		rejectFresh: func(context.Context, pgx.Tx, string) error {
			steps = append(steps, "fresh")
			return nil
		},
		ensureLedger: func(ctx context.Context, tx pgx.Tx, _ map[string]migrationSource) error {
			steps = append(steps, "ledger")
			_, err := tx.Exec(ctx, `lock table public.schema_migrations in share row exclusive mode`)
			return err
		},
		readApplied: func(context.Context, pgx.Tx) ([]MigrationChecksumEntry, error) {
			steps = append(steps, "applied")
			return nil, nil
		},
		applyPending: func(ctx context.Context, tx pgx.Tx, snapshot migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
			steps = append(steps, "pending")
			return applyPendingMigrationSourcesInTx(ctx, tx, snapshot, applied)
		},
		readManifests: func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error) {
			steps = append(steps, "revisions")
			return append([]AppACLManifestPersistedV1(nil), manifests...), nil
		},
		readHead: func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error) {
			t.Fatal("fresh convergence must not inspect a phase head before migrations create it")
			return nil, nil
		},
		readHeadForUpdate: func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
			steps = append(steps, "head-lock")
			if _, err := tx.Exec(ctx, `select 'app ACL final head lock sentinel' for update`); err != nil {
				return nil, err
			}
			if head == nil {
				return nil, nil
			}
			copy := *head
			return &copy, nil
		},
		applyDCL: func(ctx context.Context, tx pgx.Tx, _ AppACLEffectiveCatalogContractR1) error {
			steps = append(steps, "dcl")
			_, err := tx.Exec(ctx, `select 'DCL sentinel'`)
			return err
		},
		readCatalog: func(ctx context.Context, tx pgx.Tx, _ AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
			steps = append(steps, "catalog")
			if _, err := tx.Exec(ctx, `select 'catalog sentinel'`); err != nil {
				return AppACLEffectiveCatalogSnapshotR1{}, err
			}
			return AppACLEffectiveCatalogSnapshotR1{}, nil
		},
		verifyCatalog: func(AppACLEffectiveCatalogSnapshotR1, AppACLEffectiveCatalogVerifierInputR1) error {
			steps = append(steps, "verify")
			return nil
		},
		insertGenesis: func(context.Context, pgx.Tx, []byte, []byte, string) (AppACLManifestPersistedV1, error) {
			steps = append(steps, "insert")
			manifests = []AppACLManifestPersistedV1{genesis}
			head = &AppACLManifestHeadV1{ManifestRevision: genesis.ManifestRevision, ManifestDigest: genesis.ManifestDigest}
			return genesis, nil
		},
	}

	manifest, err := convergeAppACLR1InTx(
		context.Background(),
		tx,
		"houfeng_center_runtime",
		"houfeng_platform_admin",
		sources,
		dependencies,
	)
	if err != nil {
		t.Fatalf("convergeAppACLR1InTx() error = %v", err)
	}
	if manifest.ManifestDigest != genesis.ManifestDigest {
		t.Fatalf("converged manifest digest = %x, want %x", manifest.ManifestDigest, genesis.ManifestDigest)
	}
	wantSteps := []string{
		"roles", "database", "placement", "legacy", "phase", "fresh", "ledger", "applied", "pending", "head-lock", "revisions",
		"dcl", "catalog", "verify", "insert", "head-lock", "revisions", "catalog", "verify",
	}
	if !equalStringSlices(steps, wantSteps) {
		t.Fatalf("genesis convergence operation order = %#v, want %#v", steps, wantSteps)
	}
	assertSQLFragmentsInOrder(t, tx.execSQL, []string{
		appACLConvergenceHardenedSearchPathSQL,
		"pg_advisory_xact_lock",
		"lock table public.schema_migrations in share row exclusive mode",
		`set local search_path = public`,
		"select 'pending migration sentinel'",
		"insert into public.schema_migrations (name, checksum) values ($1, $2)",
		appACLConvergenceHardenedSearchPathSQL,
		"lock table public.app_acl_manifest_revisions in share row exclusive mode",
		"select 'app ACL final head lock sentinel' for update",
		"select 'DCL sentinel'",
		"select 'catalog sentinel'",
	})
	if strings.Contains(strings.Join(tx.execSQL, "\n"), "$user") {
		t.Fatalf("convergence SQL = %#v, must never select the caller schema", tx.execSQL)
	}
	if tx.nestedBeginCalls != 0 {
		t.Fatalf("nested transaction begins = %d, want 0", tx.nestedBeginCalls)
	}
}

func assertSQLFragmentsInOrder(t *testing.T, statements []string, fragments []string) {
	t.Helper()
	joined := strings.Join(statements, "\n")
	offset := 0
	for _, fragment := range fragments {
		index := strings.Index(joined[offset:], fragment)
		if index < 0 {
			t.Fatalf("SQL order = %q, missing ordered fragment %q after offset %d", joined, fragment, offset)
		}
		offset += index + len(fragment)
	}
}

type fakeAppACLConvergenceTx struct {
	pgx.Tx
	execSQL          []string
	nestedBeginCalls int
	commitErr        error
}

type nonPublicLedgerProbeErrorTx struct {
	pgx.Tx
	execSQL     []string
	queryErr    error
	rollbackErr error
	releaseErr  error
}

type nonPublicLedgerContinuationTx struct {
	pgx.Tx
	execSQL     []string
	probeErr    error
	rollbackErr error
	releaseErr  error
}

type nonPublicLedgerCandidateRows struct {
	returned bool
}

type emptyAppACLConvergenceRows struct{}

type recordingAppACLHeadQueryTx struct {
	pgx.Tx
	query string
}

func (tx *recordingAppACLHeadQueryTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.query = query
	return appACLConvergenceErrorRow{err: pgx.ErrNoRows}
}

type appACLConvergenceErrorRow struct {
	err error
}

func (row appACLConvergenceErrorRow) Scan(...any) error {
	return row.err
}

func (tx *fakeAppACLConvergenceTx) Begin(context.Context) (pgx.Tx, error) {
	tx.nestedBeginCalls++
	return tx, nil
}

func (tx *fakeAppACLConvergenceTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.execSQL = append(tx.execSQL, sql)
	return pgconn.NewCommandTag("OK"), nil
}

func (tx *fakeAppACLConvergenceTx) Commit(context.Context) error {
	return tx.commitErr
}

func (tx *fakeAppACLConvergenceTx) Rollback(context.Context) error {
	return nil
}

func (tx *nonPublicLedgerProbeErrorTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.execSQL = append(tx.execSQL, sql)
	if sql == "rollback to savepoint app_acl_non_public_ledger_probe" {
		return pgconn.CommandTag{}, tx.rollbackErr
	}
	if sql == "release savepoint app_acl_non_public_ledger_probe" {
		return pgconn.CommandTag{}, tx.releaseErr
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (tx *nonPublicLedgerProbeErrorTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, tx.queryErr
}

func (tx *nonPublicLedgerContinuationTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	tx.execSQL = append(tx.execSQL, sql)
	if sql == "rollback to savepoint app_acl_non_public_ledger_probe" {
		return pgconn.CommandTag{}, tx.rollbackErr
	}
	if sql == "release savepoint app_acl_non_public_ledger_probe" {
		return pgconn.CommandTag{}, tx.releaseErr
	}
	return pgconn.NewCommandTag("OK"), nil
}

func (tx *nonPublicLedgerContinuationTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "relation.relname = 'schema_migrations'"):
		return &nonPublicLedgerCandidateRows{}, nil
	case strings.Contains(sql, "select relation.relkind::text"):
		return emptyAppACLConvergenceRows{}, nil
	case strings.Contains(sql, "select procedure.proname::text"):
		return emptyAppACLConvergenceRows{}, nil
	case strings.Contains(sql, "select name from"):
		return nil, tx.probeErr
	default:
		return nil, errors.New("unexpected non-public ledger test query")
	}
}

func (rows *nonPublicLedgerCandidateRows) Close()                                       {}
func (rows *nonPublicLedgerCandidateRows) Err() error                                   { return nil }
func (rows *nonPublicLedgerCandidateRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *nonPublicLedgerCandidateRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *nonPublicLedgerCandidateRows) Values() ([]any, error)                       { return nil, nil }
func (rows *nonPublicLedgerCandidateRows) RawValues() [][]byte                          { return nil }
func (rows *nonPublicLedgerCandidateRows) Conn() *pgx.Conn                              { return nil }
func (rows *nonPublicLedgerCandidateRows) Next() bool {
	if rows.returned {
		return false
	}
	rows.returned = true
	return true
}
func (rows *nonPublicLedgerCandidateRows) Scan(dest ...any) error {
	if len(dest) != 4 {
		return errors.New("unexpected non-public ledger candidate scan destination count")
	}
	schemaName, ok := dest[0].(*string)
	if !ok {
		return errors.New("non-public ledger candidate schema destination is not *string")
	}
	ownerRole, ok := dest[1].(*string)
	if !ok {
		return errors.New("non-public ledger candidate owner destination is not *string")
	}
	rowSecurityEnabled, ok := dest[2].(*bool)
	if !ok {
		return errors.New("non-public ledger candidate row-security destination is not *bool")
	}
	rowSecurityForced, ok := dest[3].(*bool)
	if !ok {
		return errors.New("non-public ledger candidate forced-row-security destination is not *bool")
	}
	*schemaName = "third_party_private_history"
	*ownerRole = "third_party_owner"
	*rowSecurityEnabled = false
	*rowSecurityForced = false
	return nil
}

func (emptyAppACLConvergenceRows) Close()                                       {}
func (emptyAppACLConvergenceRows) Err() error                                   { return nil }
func (emptyAppACLConvergenceRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (emptyAppACLConvergenceRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyAppACLConvergenceRows) Values() ([]any, error)                       { return nil, nil }
func (emptyAppACLConvergenceRows) RawValues() [][]byte                          { return nil }
func (emptyAppACLConvergenceRows) Conn() *pgx.Conn                              { return nil }
func (emptyAppACLConvergenceRows) Next() bool                                   { return false }
func (emptyAppACLConvergenceRows) Scan(...any) error {
	return errors.New("scan called for empty app ACL convergence rows")
}

func appACLConvergenceChecksum(t *testing.T, pair string) [32]byte {
	t.Helper()
	if len(pair) != 2 {
		t.Fatalf("checksum pair length = %d, want 2", len(pair))
	}
	bytes, err := hex.DecodeString(strings.Repeat(pair, 32))
	if err != nil {
		t.Fatalf("decode test checksum pair %q: %v", pair, err)
	}
	var checksum [32]byte
	copy(checksum[:], bytes)
	return checksum
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
