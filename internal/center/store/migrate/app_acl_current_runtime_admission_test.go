package migrate

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5"
)

func TestAdmitAppACLCurrentRuntimeRejectsMissingFragmentBeforeBeginTx(t *testing.T) {
	futureFS := appACLCurrentTestMigrationFS(t)
	futureFS["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}
	beginCalls := 0

	err := admitAppACLCurrentRuntimeWithDependencies(
		context.Background(),
		futureFS,
		nil,
		appACLCurrentRuntimeAdmissionDependencies{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				beginCalls++
				return nil, errors.New("begin must not run")
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), `migration "0052_future.sql" has no current APP ACL fragment`) {
		t.Fatalf("admitAppACLCurrentRuntimeWithDependencies() error = %v, want missing-fragment rejection", err)
	}
	if beginCalls != 0 {
		t.Fatalf("current runtime BeginTx calls = %d, want 0", beginCalls)
	}
}

func TestAdmitAppACLCurrentRuntimeUsesOneRepeatableReadOnlySnapshot(t *testing.T) {
	futureFS, fragments := appACLCurrentRuntimeAdmissionExtendedSource(t)
	manifestSnapshot, catalog, catalogSnapshot := appACLCurrentRuntimeAdmissionFixture(t, futureFS, fragments)
	expectedInput, err := newAppACLEffectiveCatalogVerifierInput(catalog, "houfeng_migrator")
	if err != nil {
		t.Fatal(err)
	}
	tx := &fakeAppACLRuntimeAdmissionTx{}
	var beginOptions []pgx.TxOptions
	var readTransactions []pgx.Tx

	err = admitAppACLCurrentRuntimeWithDependencies(
		context.Background(),
		futureFS,
		fragments,
		appACLCurrentRuntimeAdmissionDependencies{
			beginTx: func(_ context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				beginOptions = append(beginOptions, options)
				return tx, nil
			},
			readManifest: func(_ context.Context, gotTx pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
				readTransactions = append(readTransactions, gotTx)
				return manifestSnapshot, nil
			},
			readCatalog: func(_ context.Context, gotTx pgx.Tx, input appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
				readTransactions = append(readTransactions, gotTx)
				if !reflect.DeepEqual(input, expectedInput) {
					t.Fatalf("current catalog input = %#v, want %#v", input, expectedInput)
				}
				return catalogSnapshot, nil
			},
			verifyCatalog: verifyAppACLEffectiveCatalogSnapshot,
		},
	)
	if err != nil {
		t.Fatalf("admitAppACLCurrentRuntimeWithDependencies() error = %v", err)
	}
	if len(beginOptions) != 1 || beginOptions[0].IsoLevel != pgx.RepeatableRead || beginOptions[0].AccessMode != pgx.ReadOnly {
		t.Fatalf("current runtime transaction options = %#v, want one REPEATABLE READ READ ONLY transaction", beginOptions)
	}
	if len(readTransactions) != 2 || readTransactions[0] != tx || readTransactions[1] != tx {
		t.Fatalf("current runtime reader transactions = %#v, want one transaction", readTransactions)
	}
	if tx.commitCalls != 1 || tx.rollbackCalls != 1 {
		t.Fatalf("current runtime lifecycle = commit %d rollback %d, want 1/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func TestAdmitAppACLCurrentRuntimeSourceMismatchRequiresRebuildBeforeCatalogRead(t *testing.T) {
	priorFS := appACLCurrentTestMigrationFS(t)
	priorManifest, _, _ := appACLCurrentRuntimeAdmissionFixture(t, priorFS, nil)
	futureFS, fragments := appACLCurrentConvergenceFutureSource(t)
	tx := &fakeAppACLRuntimeAdmissionTx{}

	err := admitAppACLCurrentRuntimeWithDependencies(
		context.Background(),
		futureFS,
		fragments,
		appACLCurrentRuntimeAdmissionDependencies{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
				return priorManifest, nil
			},
			readCatalog: func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
				t.Fatal("source mismatch must fail before catalog read")
				return AppACLEffectiveCatalogSnapshotR1{}, nil
			},
			verifyCatalog: verifyAppACLEffectiveCatalogSnapshot,
		},
	)
	if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("current source mismatch error = %v, want rebuild-required sentinel", err)
	}
	if tx.commitCalls != 0 || tx.rollbackCalls != 1 {
		t.Fatalf("source mismatch lifecycle = commit %d rollback %d, want 0/1", tx.commitCalls, tx.rollbackCalls)
	}
}

func TestAdmitAppACLCurrentRuntimeRejectsSetRoleBeforeCatalogRead(t *testing.T) {
	futureFS, fragments := appACLCurrentConvergenceFutureSource(t)
	manifestSnapshot, _, _ := appACLCurrentRuntimeAdmissionFixture(t, futureFS, fragments)
	manifestSnapshot.SessionUser = "member_login"
	tx := &fakeAppACLRuntimeAdmissionTx{}

	err := admitAppACLCurrentRuntimeWithDependencies(
		context.Background(),
		futureFS,
		fragments,
		appACLCurrentRuntimeAdmissionDependencies{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
				return manifestSnapshot, nil
			},
			readCatalog: func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
				t.Fatal("SET ROLE mismatch must fail before catalog read")
				return AppACLEffectiveCatalogSnapshotR1{}, nil
			},
			verifyCatalog: verifyAppACLEffectiveCatalogSnapshot,
		},
	)
	want := `verify current app ACL runtime manifest: session user "member_login" does not match current user "houfeng_center_runtime"`
	if err == nil || err.Error() != want {
		t.Fatalf("current SET ROLE error = %v, want %q", err, want)
	}
	if errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("current SET ROLE error = %v, must not be rebuild-required", err)
	}
}

func TestAdmitAppACLCurrentRuntimeCatalogDriftIsNotRebuildRequired(t *testing.T) {
	futureFS, fragments := appACLCurrentRuntimeAdmissionExtendedSource(t)
	manifestSnapshot, _, catalogSnapshot := appACLCurrentRuntimeAdmissionFixture(t, futureFS, fragments)
	catalogSnapshot.DirectPrivileges = append(catalogSnapshot.DirectPrivileges, AppACLEffectiveCatalogPrivilegeObservationR1{
		Grantee:        "houfeng_center_runtime",
		ObjectClass:    AppACLObjectClassFunction,
		SchemaName:     "public",
		ObjectIdentity: "record_platform_cas_contract_activation_projection(bytea)",
		Privilege:      AppACLPrivilegeExecute,
	})
	tx := &fakeAppACLRuntimeAdmissionTx{}

	err := admitAppACLCurrentRuntimeWithDependencies(
		context.Background(),
		futureFS,
		fragments,
		appACLCurrentRuntimeAdmissionDependencies{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil },
			readManifest: func(context.Context, pgx.Tx) (AppACLManifestRuntimeSnapshotV1, error) {
				return manifestSnapshot, nil
			},
			readCatalog: func(context.Context, pgx.Tx, appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
				return catalogSnapshot, nil
			},
			verifyCatalog: verifyAppACLEffectiveCatalogSnapshot,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "verify current app ACL runtime catalog") {
		t.Fatalf("current catalog drift error = %v, want catalog rejection", err)
	}
	if errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("current catalog drift error = %v, must not be rebuild-required", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RetainsFrozenR1CompilerForCurrentFragment(t *testing.T) {
	futureFS, fragments := appACLCurrentRuntimeAdmissionExtendedSource(t)
	manifestSnapshot, _, _ := appACLCurrentRuntimeAdmissionFixture(t, futureFS, fragments)

	_, err := VerifyPersistedAppACLManifestRuntimeV1(
		context.Background(),
		fakeAppACLManifestRuntimeReader{snapshot: manifestSnapshot},
		futureFS,
	)
	if err == nil || !strings.Contains(err.Error(), "frozen r1 compiler output") {
		t.Fatalf("frozen runtime verifier error = %v, want current fragment privilege rejection", err)
	}
}

func appACLCurrentRuntimeAdmissionExtendedSource(t *testing.T) (fs.FS, []AppACLCurrentMigrationFragment) {
	t.Helper()
	newTable, newFunction, privilege, function := appACLCurrentCatalogTestExtension()
	futureFS := appACLCurrentTestMigrationFS(t)
	futureFS["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}
	return futureFS, []AppACLCurrentMigrationFragment{{
		Migration: "0052_future.sql",
		Objects:   []AppACLManagedObjectR1{newTable, newFunction},
		Privileges: func(string) []AppACLPrivilege {
			return []AppACLPrivilege{privilege}
		},
		Functions: []AppACLCurrentFunctionContract{function},
	}}
}

func appACLCurrentRuntimeAdmissionFixture(
	t *testing.T,
	futureFS fs.FS,
	fragments []AppACLCurrentMigrationFragment,
) (AppACLManifestRuntimeSnapshotV1, appACLEffectiveCatalogContract, AppACLEffectiveCatalogSnapshotR1) {
	t.Helper()
	source, catalog, privileges := appACLCurrentConvergenceExpected(t, futureFS, fragments)
	manifest, err := NewAppACLManifestPersistedV1(
		1,
		"houfeng_migrator",
		[32]byte{},
		source.sources.canonicalSet,
		privileges,
	)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := ParseCanonicalMigrationSetBodyV1(source.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	roleBySubject := make(map[AppACLSubject]string, len(catalog.RoleBindings))
	for _, binding := range catalog.RoleBindings {
		roleBySubject[binding.Subject] = binding.CatalogRole
	}
	catalogSnapshot := AppACLEffectiveCatalogSnapshotR1{
		DatabaseName: "houfeng",
		SessionUser:  "houfeng_center_runtime",
		CurrentUser:  "houfeng_center_runtime",
		PGCryptoExtension: AppACLEffectiveCatalogExtensionR1{
			ExtensionName: "pgcrypto",
			SchemaName:    appACLManagedInternalSchemaR1,
		},
		Roles: []AppACLEffectiveCatalogRoleStateR1{
			{Name: roleBySubject[AppACLSubjectCenterRuntime], Login: true},
			{Name: roleBySubject[AppACLSubjectPlatformAdmin], Login: true},
			{Name: "houfeng_migrator", Login: true},
		},
	}
	for _, privilege := range catalog.Privileges {
		observation := AppACLEffectiveCatalogPrivilegeObservationR1{
			Grantee:        roleBySubject[privilege.Subject],
			ObjectClass:    privilege.ObjectClass,
			SchemaName:     privilege.SchemaName,
			ObjectIdentity: privilege.ObjectIdentity,
			ColumnName:     privilege.ColumnName,
			Privilege:      privilege.Privilege,
		}
		catalogSnapshot.DirectPrivileges = append(catalogSnapshot.DirectPrivileges, observation)
		catalogSnapshot.EffectivePrivileges = append(catalogSnapshot.EffectivePrivileges, observation)
	}
	for _, function := range catalog.ExpectedFunctions {
		name, arguments, found := strings.Cut(function.Identity, "(")
		if !found {
			t.Fatalf("expected function identity %q has no arguments", function.Identity)
		}
		catalogSnapshot.Functions = append(catalogSnapshot.Functions, AppACLEffectiveCatalogFunctionR1{
			SchemaName:        function.SchemaName,
			Name:              name,
			IdentityArguments: strings.TrimSuffix(arguments, ")"),
			Identity:          function.SchemaName + "." + function.Identity,
			OwnerRole:         function.OwnerRole,
			Kind:              function.Kind,
			SecurityDefiner:   function.SecurityDefiner,
			Config:            append([]string(nil), function.Config...),
		})
	}
	for _, object := range catalog.ManagedObjects {
		catalogSnapshot.Owners = append(catalogSnapshot.Owners, AppACLEffectiveCatalogObjectOwnerR1{
			ObjectClass:    object.ObjectClass,
			SchemaName:     object.SchemaName,
			ObjectIdentity: object.ObjectIdentity,
			OwnerRole:      "houfeng_migrator",
		})
	}
	return AppACLManifestRuntimeSnapshotV1{
		DatabaseName:      "houfeng",
		SessionUser:       "houfeng_center_runtime",
		CurrentUser:       "houfeng_center_runtime",
		Manifests:         []AppACLManifestPersistedV1{manifest},
		Head:              &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: manifest.ManifestDigest},
		AppliedMigrations: applied,
	}, catalog, catalogSnapshot
}
