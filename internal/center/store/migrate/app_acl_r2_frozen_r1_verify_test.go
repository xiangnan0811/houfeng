package migrate

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestFrozenAppACLR1StateVerifierIsCredentialNeutralAndTransactionBound(t *testing.T) {
	evidence, input, catalog, wantState := validFrozenAppACLR1VerifyFixture(t)
	identities := []struct {
		name        string
		sessionUser string
		currentUser string
	}{
		{name: "center runtime", sessionUser: wantState.CenterRuntimeRole, currentUser: wantState.CenterRuntimeRole},
		{name: "direct migrator", sessionUser: wantState.DirectMigratorRole, currentUser: wantState.DirectMigratorRole},
		{name: "bootstrap", sessionUser: "postgres", currentUser: "postgres"},
		{name: "platform admin", sessionUser: wantState.PlatformAdminRole, currentUser: wantState.PlatformAdminRole},
		{name: "unrelated direct role", sessionUser: "unrelated_login", currentUser: "unrelated_login"},
		{name: "distinct pair", sessionUser: "member_login", currentUser: wantState.CenterRuntimeRole},
	}
	var first FrozenAppACLR1StateV1
	for index, identity := range identities {
		t.Run(identity.name, func(t *testing.T) {
			tx := &fakeFrozenAppACLR1IdentityTx{sessionUser: identity.sessionUser, currentUser: identity.currentUser}
			readCount := 0
			state, err := verifyFrozenAppACLR1StateInTxWithDependencies(context.Background(), tx, frozenAppACLR1VerifyDependencies{
				loadSources: func() (migrationSourceSnapshot, error) {
					return frozenAppACLR1SourceSnapshotFixture(t), nil
				},
				readEvidence: func(_ context.Context, gotTx pgx.Tx) (frozenAppACLR1EvidenceV1, error) {
					readCount++
					if gotTx != tx {
						t.Fatalf("evidence transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
					}
					return cloneFrozenAppACLR1Evidence(evidence), nil
				},
				readCatalog: func(_ context.Context, gotTx pgx.Tx, gotInput AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
					readCount++
					if gotTx != tx {
						t.Fatalf("catalog transaction = %T %p, want caller transaction %p", gotTx, gotTx, tx)
					}
					if gotInput != input {
						t.Fatalf("catalog input = %#v, want %#v", gotInput, input)
					}
					return catalog, nil
				},
				verifyCatalog: VerifyAppACLEffectiveCatalogSnapshotR1,
			})
			if err != nil {
				t.Fatalf("verifyFrozenAppACLR1StateInTxWithDependencies() error = %v", err)
			}
			if readCount != 2 {
				t.Fatalf("transaction-bound reads = %d, want 2", readCount)
			}
			if len(tx.queries) != 0 {
				t.Fatalf("credential-neutral verifier queried actor identity: %q", tx.queries)
			}
			if !reflect.DeepEqual(state, wantState) {
				t.Fatalf("verified state = %#v, want %#v", state, wantState)
			}
			if index == 0 {
				first = cloneFrozenAppACLR1State(state)
			} else if !reflect.DeepEqual(state, first) {
				t.Fatalf("verified state changed with caller identity: %#v vs %#v", state, first)
			}
		})
	}
}

func TestFrozenAppACLR1StateVerifierRejectsEvidenceDrift(t *testing.T) {
	validEvidence, _, validCatalog, _ := validFrozenAppACLR1VerifyFixture(t)
	tests := []struct {
		name   string
		mutate func(*migrationSourceSnapshot, *frozenAppACLR1EvidenceV1, *AppACLEffectiveCatalogSnapshotR1)
		want   string
	}{
		{name: "embedded source", mutate: func(value *migrationSourceSnapshot, _ *frozenAppACLR1EvidenceV1, _ *AppACLEffectiveCatalogSnapshotR1) {
			value.sources[value.names[0]] = migrationSource{checksum: strings.Repeat("0", 64), sql: "select 1"}
		}, want: "frozen r1"},
		{name: "ledger checksum", mutate: func(_ *migrationSourceSnapshot, value *frozenAppACLR1EvidenceV1, _ *AppACLEffectiveCatalogSnapshotR1) {
			value.AppliedMigrations[0].Checksum[0] ^= 0xff
		}, want: "ledger"},
		{name: "manifest head", mutate: func(_ *migrationSourceSnapshot, value *frozenAppACLR1EvidenceV1, _ *AppACLEffectiveCatalogSnapshotR1) {
			value.Head.ManifestDigest[0] ^= 0xff
		}, want: "chain"},
		{name: "extra manifest revision", mutate: func(_ *migrationSourceSnapshot, value *frozenAppACLR1EvidenceV1, _ *AppACLEffectiveCatalogSnapshotR1) {
			value.Manifests = append(value.Manifests, value.Manifests[0])
		}, want: "one revision"},
		{name: "privilege contract", mutate: func(_ *migrationSourceSnapshot, value *frozenAppACLR1EvidenceV1, _ *AppACLEffectiveCatalogSnapshotR1) {
			value.Manifests[0].CanonicalPrivilegeSet[0] ^= 0xff
		}, want: "manifest"},
		{name: "catalog", mutate: func(_ *migrationSourceSnapshot, _ *frozenAppACLR1EvidenceV1, value *AppACLEffectiveCatalogSnapshotR1) {
			value.PGCryptoExtension.SchemaName = "public"
		}, want: "catalog"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources := frozenAppACLR1SourceSnapshotFixture(t)
			evidence := cloneFrozenAppACLR1Evidence(validEvidence)
			catalog := validCatalog
			tt.mutate(&sources, &evidence, &catalog)
			_, err := verifyFrozenAppACLR1StateInTxWithDependencies(context.Background(), &fakeFrozenAppACLR1IdentityTx{}, frozenAppACLR1VerifyDependencies{
				loadSources:  func() (migrationSourceSnapshot, error) { return sources, nil },
				readEvidence: func(context.Context, pgx.Tx) (frozenAppACLR1EvidenceV1, error) { return evidence, nil },
				readCatalog: func(context.Context, pgx.Tx, AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
					return catalog, nil
				},
				verifyCatalog: VerifyAppACLEffectiveCatalogSnapshotR1,
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("verifyFrozenAppACLR1StateInTxWithDependencies() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestFrozenAppACLR1StateVerifierDelegatesOnlyExactR2ReservedFunctions(t *testing.T) {
	l2ReservedFunctions := appACLR2ReservedFunctionIdentitiesForFrozenR1Test(t, appACLR2L2ReservedObjects())
	knownReservedFunctions := appACLR2ReservedFunctionIdentitiesForFrozenR1Test(t, appACLR2KnownReservedObjects())
	genericScope, err := newAppACLManagedSurfaceScopeR1("houfeng_app")
	if err != nil {
		t.Fatalf("newAppACLManagedSurfaceScopeR1() error = %v", err)
	}
	for _, identity := range knownReservedFunctions {
		schema, functionIdentity, found := appACLFunctionIdentityFromQualifiedIdentityR1(identity)
		if !found {
			t.Fatalf("reserved function identity %q is invalid", identity)
		}
		owner := AppACLEffectiveCatalogObjectOwnerR1{
			ObjectClass:    AppACLObjectClassFunction,
			SchemaName:     schema,
			ObjectIdentity: functionIdentity,
		}
		if !genericScope.containsOwner(owner) {
			t.Fatalf("generic frozen R1 scope no longer retains reserved function %q; frozen R1 admission must stay closed", identity)
		}
	}

	tests := []struct {
		name      string
		functions []string
		mutate    func(*testing.T, *appACLR2PublicR1Tx)
		want      string
	}{
		{name: "exact L2 reserved functions", functions: l2ReservedFunctions},
		{name: "exact L2 and M2 reserved functions", functions: knownReservedFunctions},
		{
			name:      "reserved-name extra overload",
			functions: l2ReservedFunctions,
			mutate: func(t *testing.T, tx *appACLR2PublicR1Tx) {
				appendFrozenAppACLR1CatalogFunctionForTest(
					t,
					tx,
					"record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, text)",
					"postgres",
				)
			},
			want: "unexpected managed object owner",
		},
		{
			name:      "unknown internal reserved object",
			functions: l2ReservedFunctions,
			mutate: func(t *testing.T, tx *appACLR2PublicR1Tx) {
				appendFrozenAppACLR1CatalogFunctionForTest(
					t,
					tx,
					"record_platform_internal.app_acl_r2_unexpected_helper()",
					"postgres",
				)
			},
			want: "unexpected managed object owner",
		},
		{
			name:      "R1 managed owner drift",
			functions: l2ReservedFunctions,
			mutate: func(t *testing.T, tx *appACLR2PublicR1Tx) {
				for _, row := range tx.queryRows[4] {
					if len(row) == 4 && row[0] == "table" && row[1] == "public" && row[2] == "schema_migrations" {
						row[3] = "postgres"
						return
					}
				}
				t.Fatal("frozen R1 owner fixture is missing public.schema_migrations")
			},
			want: "does not match migrator role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newAppACLR2PublicR1Tx(t, "postgres", "postgres", appACLR2L2ReservedObjects())
			for _, identity := range tt.functions {
				appendFrozenAppACLR1CatalogFunctionForTest(t, tx, identity, "postgres")
			}
			if tt.mutate != nil {
				tt.mutate(t, tx)
			}

			state, err := VerifyFrozenAppACLR1StateInTx(context.Background(), tx)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("VerifyFrozenAppACLR1StateInTx() error = %v, want exact frozen R1 proof with independently verified R2 functions", err)
				}
				if state.ManifestRevision != 1 || state.DirectMigratorRole != "direct_migrator" {
					t.Fatalf("VerifyFrozenAppACLR1StateInTx() state = %#v, want exact frozen R1 state", state)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("VerifyFrozenAppACLR1StateInTx() error = %v, want %q rejection", err, tt.want)
			}
		})
	}
}

func TestFrozenAppACLR2DelegatedFunctionIdentityProtocolIsExact(t *testing.T) {
	want := map[string]struct{}{
		"record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)": {},
		"record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()":           {},
		"record_platform_internal.app_acl_r2_reject_manifest_mutation()":                    {},
	}

	got, err := frozenAppACLR2DelegatedFunctionIdentities()
	if err != nil {
		t.Fatalf("frozenAppACLR2DelegatedFunctionIdentities() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delegated R2 function identities = %#v, want independent protocol-2 literal set %#v", got, want)
	}
}

func TestFrozenAppACLR2DelegatedFunctionSelectorRejectsInventoryExpansion(t *testing.T) {
	objects := append([]AppACLR2ReservedCatalogObjectV1(nil), appACLR2KnownReservedObjects()...)
	objects = append(objects, AppACLR2ReservedCatalogObjectV1{
		OID:      9001,
		Kind:     "function",
		Schema:   "record_platform_internal",
		Identity: "record_platform_internal.app_acl_r2_future_helper()",
		Detail:   "f",
	})

	_, err := selectFrozenAppACLR2DelegatedFunctionIdentities(objects)
	if err == nil || !strings.Contains(err.Error(), "has 4 functions, want exactly 3") {
		t.Fatalf("selectFrozenAppACLR2DelegatedFunctionIdentities() error = %v, want protocol-2 function-count rejection", err)
	}
}

func appACLR2ReservedFunctionIdentitiesForFrozenR1Test(t *testing.T, objects []AppACLR2ReservedCatalogObjectV1) []string {
	t.Helper()
	identities := make([]string, 0, len(objects))
	for _, object := range objects {
		if object.Kind == "function" {
			identities = append(identities, object.Identity)
		}
	}
	if len(identities) == 0 {
		t.Fatal("APP ACL R2 reserved fixture has no functions")
	}
	return identities
}

func appendFrozenAppACLR1CatalogFunctionForTest(t *testing.T, tx *appACLR2PublicR1Tx, identity, ownerRole string) {
	t.Helper()
	if len(tx.queryRows) <= 9 {
		t.Fatalf("frozen R1 catalog fixture has %d query result sets, want owner and function sets", len(tx.queryRows))
	}
	schema, functionIdentity, found := appACLFunctionIdentityFromQualifiedIdentityR1(identity)
	if !found {
		t.Fatalf("function identity %q is invalid", identity)
	}
	name, arguments, found := strings.Cut(functionIdentity, "(")
	if !found || !strings.HasSuffix(arguments, ")") {
		t.Fatalf("function identity %q has invalid arguments", identity)
	}
	arguments = strings.TrimSuffix(arguments, ")")
	tx.queryRows[4] = append(tx.queryRows[4], []any{"function", schema, functionIdentity, ownerRole})
	tx.queryRows[9] = append(tx.queryRows[9], []any{
		schema,
		name,
		arguments,
		ownerRole,
		"f",
		false,
		[]string{"search_path=pg_catalog"},
	})
}

func TestFrozenAppACLR1StateVerifierSourceContainsNoCredentialOrPoolAdmissionDependency(t *testing.T) {
	source, err := os.ReadFile("app_acl_r2_frozen_r1_verify.go")
	if err != nil {
		t.Fatalf("read verifier source: %v", err)
	}
	text := string(source)
	requireIndex := strings.Index(text, "func RequireDirectFrozenAppACLR1RuntimeInTx")
	if requireIndex < 0 {
		t.Fatal("verifier source is missing separate runtime predicate")
	}
	verifierSource := text[:requireIndex]
	for _, forbidden := range []string{"session_user", "current_user", "pgxpool", "AdmitAppACLRuntime("} {
		if strings.Contains(verifierSource, forbidden) {
			t.Fatalf("credential-neutral verifier source contains forbidden dependency %q", forbidden)
		}
	}
}

func TestRequireDirectFrozenAppACLR1RuntimeInTxIdentityMatrix(t *testing.T) {
	state := validFrozenAppACLR1StateFixture(t)
	tests := []struct {
		name        string
		sessionUser string
		currentUser string
		wantError   bool
	}{
		{name: "center runtime", sessionUser: state.CenterRuntimeRole, currentUser: state.CenterRuntimeRole},
		{name: "direct migrator", sessionUser: state.DirectMigratorRole, currentUser: state.DirectMigratorRole, wantError: true},
		{name: "bootstrap", sessionUser: "postgres", currentUser: "postgres", wantError: true},
		{name: "platform admin", sessionUser: state.PlatformAdminRole, currentUser: state.PlatformAdminRole, wantError: true},
		{name: "unrelated", sessionUser: "unrelated_login", currentUser: "unrelated_login", wantError: true},
		{name: "distinct pair", sessionUser: "member_login", currentUser: state.CenterRuntimeRole, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeFrozenAppACLR1IdentityTx{sessionUser: tt.sessionUser, currentUser: tt.currentUser}
			err := RequireDirectFrozenAppACLR1RuntimeInTx(context.Background(), tx, state)
			if tt.wantError && err == nil {
				t.Fatal("RequireDirectFrozenAppACLR1RuntimeInTx() error = nil, want identity rejection")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("RequireDirectFrozenAppACLR1RuntimeInTx() error = %v, want nil", err)
			}
			if len(tx.queries) != 1 || !strings.Contains(tx.queries[0], "session_user") || !strings.Contains(tx.queries[0], "current_user") {
				t.Fatalf("runtime predicate queries = %q, want one direct identity query", tx.queries)
			}
		})
	}
}

func validFrozenAppACLR1VerifyFixture(t *testing.T) (frozenAppACLR1EvidenceV1, AppACLEffectiveCatalogVerifierInputR1, AppACLEffectiveCatalogSnapshotR1, FrozenAppACLR1StateV1) {
	t.Helper()
	const databaseName = "houfeng_app"
	const runtimeRole = "center_runtime"
	const adminRole = "platform_admin"
	const migratorRole = "direct_migrator"
	sourceBody, err := CanonicalMigrationSetBodyV1(appACLR1MigrationSourceContract[:])
	if err != nil {
		t.Fatalf("CanonicalMigrationSetBodyV1() error = %v", err)
	}
	privilegeBody, err := CompileAppACLPrivilegeSetR1(databaseName, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: runtimeRole},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: adminRole},
	})
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() error = %v", err)
	}
	manifest, err := NewAppACLManifestPersistedV1(1, migratorRole, [32]byte{}, sourceBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}
	evidence := frozenAppACLR1EvidenceV1{
		DatabaseName:      databaseName,
		Manifests:         []AppACLManifestPersistedV1{manifest},
		Head:              &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: manifest.ManifestDigest},
		AppliedMigrations: append([]MigrationChecksumEntry(nil), appACLR1MigrationSourceContract[:]...),
	}
	contract, err := CompileAppACLEffectiveCatalogContractR1(databaseName, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: runtimeRole},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: adminRole},
	})
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	input, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, migratorRole)
	if err != nil {
		t.Fatalf("NewAppACLEffectiveCatalogVerifierInputR1() error = %v", err)
	}
	fixtureInput, catalog := validAppACLEffectiveCatalogVerifierFixture(t)
	_ = fixtureInput
	catalog.DatabaseName = databaseName
	for index := range catalog.Roles {
		switch catalog.Roles[index].Name {
		case "houfeng_center_runtime":
			catalog.Roles[index].Name = runtimeRole
		case "houfeng_platform_admin":
			catalog.Roles[index].Name = adminRole
		case "houfeng_migrator":
			catalog.Roles[index].Name = migratorRole
		}
	}
	for index := range catalog.DirectPrivileges {
		switch catalog.DirectPrivileges[index].Grantee {
		case "houfeng_center_runtime":
			catalog.DirectPrivileges[index].Grantee = runtimeRole
		case "houfeng_platform_admin":
			catalog.DirectPrivileges[index].Grantee = adminRole
		}
		if catalog.DirectPrivileges[index].ObjectClass == AppACLObjectClassDatabase {
			catalog.DirectPrivileges[index].ObjectIdentity = databaseName
		}
	}
	for index := range catalog.EffectivePrivileges {
		switch catalog.EffectivePrivileges[index].Grantee {
		case "houfeng_center_runtime":
			catalog.EffectivePrivileges[index].Grantee = runtimeRole
		case "houfeng_platform_admin":
			catalog.EffectivePrivileges[index].Grantee = adminRole
		}
		if catalog.EffectivePrivileges[index].ObjectClass == AppACLObjectClassDatabase {
			catalog.EffectivePrivileges[index].ObjectIdentity = databaseName
		}
	}
	for index := range catalog.Owners {
		catalog.Owners[index].OwnerRole = migratorRole
		if catalog.Owners[index].ObjectClass == AppACLObjectClassDatabase {
			catalog.Owners[index].ObjectIdentity = databaseName
		}
	}
	for index := range catalog.Functions {
		catalog.Functions[index].OwnerRole = migratorRole
	}
	state := FrozenAppACLR1StateV1{
		DatabaseName:     databaseName,
		ManifestRevision: 1, ManifestDigest: manifest.ManifestDigest,
		SourceSetBody: append([]byte(nil), sourceBody...), SourceSetDigest: manifest.MigrationSetDigest,
		PrivilegeSetBody: append([]byte(nil), privilegeBody...), PrivilegeSetDigest: manifest.PrivilegeSetDigest,
		CenterRuntimeRole: runtimeRole, PlatformAdminRole: adminRole, DirectMigratorRole: migratorRole,
	}
	return evidence, input, catalog, state
}

func validFrozenAppACLR1StateFixture(t *testing.T) FrozenAppACLR1StateV1 {
	t.Helper()
	_, _, _, state := validFrozenAppACLR1VerifyFixture(t)
	return state
}

func frozenAppACLR1SourceSnapshotFixture(t *testing.T) migrationSourceSnapshot {
	t.Helper()
	sources := make(map[string]migrationSource, len(appACLR1MigrationSourceContract))
	names := make([]string, 0, len(appACLR1MigrationSourceContract))
	for _, entry := range appACLR1MigrationSourceContract {
		names = append(names, entry.Filename)
		sources[entry.Filename] = migrationSource{checksum: hexChecksum(entry.Checksum), sql: string(mustReadFrozenAppACLR1Source(t, entry.Filename))}
	}
	canonical, err := CanonicalMigrationSetBodyV1(appACLR1MigrationSourceContract[:])
	if err != nil {
		t.Fatalf("CanonicalMigrationSetBodyV1() error = %v", err)
	}
	return migrationSourceSnapshot{sources: sources, names: names, canonicalSet: canonical}
}

func mustReadFrozenAppACLR1Source(t *testing.T, name string) []byte {
	t.Helper()
	payload, err := os.ReadFile("../../../../db/migrations/" + name)
	if err != nil {
		t.Fatalf("read frozen R1 source %q: %v", name, err)
	}
	return payload
}

func cloneFrozenAppACLR1Evidence(value frozenAppACLR1EvidenceV1) frozenAppACLR1EvidenceV1 {
	value.Manifests = append([]AppACLManifestPersistedV1(nil), value.Manifests...)
	for index := range value.Manifests {
		value.Manifests[index].CanonicalMigrationSet = append([]byte(nil), value.Manifests[index].CanonicalMigrationSet...)
		value.Manifests[index].CanonicalPrivilegeSet = append([]byte(nil), value.Manifests[index].CanonicalPrivilegeSet...)
	}
	if value.Head != nil {
		head := *value.Head
		value.Head = &head
	}
	value.AppliedMigrations = append([]MigrationChecksumEntry(nil), value.AppliedMigrations...)
	return value
}

func cloneFrozenAppACLR1State(value FrozenAppACLR1StateV1) FrozenAppACLR1StateV1 {
	value.SourceSetBody = append([]byte(nil), value.SourceSetBody...)
	value.PrivilegeSetBody = append([]byte(nil), value.PrivilegeSetBody...)
	return value
}

type fakeFrozenAppACLR1IdentityTx struct {
	pgx.Tx
	sessionUser string
	currentUser string
	queries     []string
}

func (tx *fakeFrozenAppACLR1IdentityTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	return fakeFrozenAppACLR1IdentityRow{sessionUser: tx.sessionUser, currentUser: tx.currentUser}
}

type fakeFrozenAppACLR1IdentityRow struct {
	sessionUser string
	currentUser string
}

func (row fakeFrozenAppACLR1IdentityRow) Scan(destinations ...any) error {
	if len(destinations) != 2 {
		return nil
	}
	*(destinations[0].(*string)) = row.sessionUser
	*(destinations[1].(*string)) = row.currentUser
	return nil
}
