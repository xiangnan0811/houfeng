package migrate

import (
	"errors"
	"testing"

	"houfeng/db/migrations"
)

func TestClassifyAppACLCurrentManifestShapeRegisteredChains(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	p62, p64 := transitions[0], transitions[1]
	currentPrivileges, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil {
		t.Fatal(err)
	}
	p62Manifest := appACLCurrentShapeManifest(t, 1, [32]byte{}, p62.predecessor, p62.predecessorPrivilegeBody)
	p64Manifest := appACLCurrentShapeManifest(t, 1, [32]byte{}, p64.predecessor, p64.predecessorPrivilegeBody)
	p62P64Manifest := appACLCurrentShapeManifest(t, 2, p62Manifest.ManifestDigest, p64.predecessor, p64.predecessorPrivilegeBody)
	currentGenesis, err := NewAppACLManifestPersistedV1(
		1,
		appACLCurrentTransitionMigrator,
		[32]byte{},
		current.sources.canonicalSet,
		currentPrivileges,
	)
	if err != nil {
		t.Fatal(err)
	}
	p62Current := appACLCurrentShapeManifest(t, 2, p62Manifest.ManifestDigest, current, currentPrivileges)
	p64Current := appACLCurrentShapeManifest(t, 2, p64Manifest.ManifestDigest, current, currentPrivileges)
	p62P64Current := appACLCurrentShapeManifest(t, 3, p62P64Manifest.ManifestDigest, current, currentPrivileges)

	p62Applied := appACLCurrentShapeApplied(t, p62.predecessor)
	p64Applied := appACLCurrentShapeApplied(t, p64.predecessor)
	currentApplied := appACLCurrentShapeApplied(t, current)
	for _, tc := range []struct {
		name        string
		applied     []MigrationChecksumEntry
		manifests   []AppACLManifestPersistedV1
		wantKind    appACLCurrentManifestShapeKind
		wantProfile appACLCurrentProfileID
	}{
		{
			name:      "current target genesis",
			applied:   currentApplied,
			manifests: []AppACLManifestPersistedV1{currentGenesis},
			wantKind:  appACLCurrentManifestShapeGenesis,
		},
		{
			name:        "P62 predecessor",
			applied:     p62Applied,
			manifests:   []AppACLManifestPersistedV1{p62Manifest},
			wantKind:    appACLCurrentManifestShapePredecessor,
			wantProfile: appACLCurrentProfileP62,
		},
		{
			name:        "P64 predecessor",
			applied:     p64Applied,
			manifests:   []AppACLManifestPersistedV1{p64Manifest},
			wantKind:    appACLCurrentManifestShapePredecessor,
			wantProfile: appACLCurrentProfileP64,
		},
		{
			name:        "P62 to P64 predecessor chain",
			applied:     p64Applied,
			manifests:   []AppACLManifestPersistedV1{p62Manifest, p62P64Manifest},
			wantKind:    appACLCurrentManifestShapePredecessor,
			wantProfile: appACLCurrentProfileP64,
		},
		{
			name:        "P62 successor",
			applied:     currentApplied,
			manifests:   []AppACLManifestPersistedV1{p62Manifest, p62Current},
			wantKind:    appACLCurrentManifestShapeSuccessor,
			wantProfile: appACLCurrentProfileP62,
		},
		{
			name:        "P64 successor",
			applied:     currentApplied,
			manifests:   []AppACLManifestPersistedV1{p64Manifest, p64Current},
			wantKind:    appACLCurrentManifestShapeSuccessor,
			wantProfile: appACLCurrentProfileP64,
		},
		{
			name:        "three-revision P62 to P64 successor",
			applied:     currentApplied,
			manifests:   []AppACLManifestPersistedV1{p62Manifest, p62P64Manifest, p62P64Current},
			wantKind:    appACLCurrentManifestShapeSuccessor,
			wantProfile: appACLCurrentProfileP64,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shape, err := appACLCurrentClassifyShape(
				t,
				current,
				transitions,
				tc.applied,
				tc.manifests,
				"houfeng",
				appACLCurrentTransitionBindings,
				currentPrivileges,
			)
			if err != nil {
				t.Fatal(err)
			}
			if shape.kind != tc.wantKind || shape.latest.ManifestDigest != tc.manifests[len(tc.manifests)-1].ManifestDigest {
				t.Fatalf("shape = %#v, want kind %d/latest %x", shape, tc.wantKind, tc.manifests[len(tc.manifests)-1].ManifestDigest)
			}
			if tc.wantProfile == 0 {
				if shape.transition != nil {
					t.Fatalf("genesis shape transition = %#v, want nil", shape.transition)
				}
			} else if shape.transition == nil || shape.transition.profile != tc.wantProfile {
				t.Fatalf("shape transition = %#v, want profile %d", shape.transition, tc.wantProfile)
			}
		})
	}
}

func TestClassifyAppACLCurrentManifestShapeAcceptsCustomP64Bindings(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	databaseName := "houfeng_profile_test"
	migratorRole := "profile_test_migrator"
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "profile_test_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "profile_test_admin"},
	}
	p64 := transitions[1]
	oldPrivileges, err := appACLCurrentTransitionPrivilegeBodyFor(p64.predecessor, databaseName, bindings, migratorRole)
	if err != nil {
		t.Fatal(err)
	}
	currentPrivileges, err := appACLCurrentTransitionPrivilegeBodyFor(current, databaseName, bindings, migratorRole)
	if err != nil {
		t.Fatal(err)
	}
	predecessor := appACLCurrentShapeManifestWithMigrator(t, 1, [32]byte{}, p64.predecessor, oldPrivileges, migratorRole)
	successor := appACLCurrentShapeManifestWithMigrator(t, 2, predecessor.ManifestDigest, current, currentPrivileges, migratorRole)

	for _, tc := range []struct {
		name      string
		manifests []AppACLManifestPersistedV1
		want      appACLCurrentManifestShapeKind
	}{
		{
			name:      "custom-name P64 predecessor",
			manifests: []AppACLManifestPersistedV1{predecessor},
			want:      appACLCurrentManifestShapePredecessor,
		},
		{
			name:      "custom-name P64 target successor",
			manifests: []AppACLManifestPersistedV1{predecessor, successor},
			want:      appACLCurrentManifestShapeSuccessor,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shape, err := appACLCurrentClassifyShape(
				t,
				current,
				transitions,
				appACLCurrentShapeApplied(t, func() appACLCurrentSourceContract {
					if len(tc.manifests) == 1 {
						return p64.predecessor
					}
					return current
				}()),
				tc.manifests,
				databaseName,
				bindings,
				currentPrivileges,
			)
			if err != nil {
				t.Fatal(err)
			}
			if shape.kind != tc.want || shape.transition == nil || shape.transition.profile != appACLCurrentProfileP64 {
				t.Fatalf("custom P64 shape = %#v, want kind %d/profile P64", shape, tc.want)
			}
		})
	}
}

func TestClassifyAppACLCurrentManifestShapeRejectsUnregisteredOrMalformedState(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	currentPrivileges, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil {
		t.Fatal(err)
	}
	p62, p64 := transitions[0], transitions[1]
	p62Manifest := appACLCurrentShapeManifest(t, 1, [32]byte{}, p62.predecessor, p62.predecessorPrivilegeBody)
	currentApplied := appACLCurrentShapeApplied(t, current)
	currentTarget := appACLCurrentShapeManifest(t, 1, [32]byte{}, current, currentPrivileges)

	t.Run("partial ledger", func(t *testing.T) {
		partial := appACLCurrentShapeApplied(t, p62.predecessor)
		partial = partial[:len(partial)-1]
		_, err := appACLCurrentClassifyShape(t, current, transitions, partial, []AppACLManifestPersistedV1{p62Manifest}, "houfeng", appACLCurrentTransitionBindings, currentPrivileges)
		if err == nil {
			t.Fatal("partial predecessor ledger was admitted")
		}
	})

	t.Run("wrong previous digest", func(t *testing.T) {
		wrong := [32]byte{1}
		successor, buildErr := NewAppACLManifestPersistedV1(2, appACLCurrentTransitionMigrator, wrong, current.sources.canonicalSet, currentPrivileges)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		_, err := appACLCurrentClassifyShape(t, current, transitions, currentApplied, []AppACLManifestPersistedV1{p62Manifest, successor}, "houfeng", appACLCurrentTransitionBindings, currentPrivileges)
		if err == nil || errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("malformed chain error = %v, want retained chain error", err)
		}
	})

	t.Run("unregistered three-entry chain", func(t *testing.T) {
		duplicateP62, buildErr := NewAppACLManifestPersistedV1(2, appACLCurrentTransitionMigrator, p62Manifest.ManifestDigest, p62.predecessor.sources.canonicalSet, p62.predecessorPrivilegeBody)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		third, buildErr := NewAppACLManifestPersistedV1(3, appACLCurrentTransitionMigrator, duplicateP62.ManifestDigest, current.sources.canonicalSet, currentPrivileges)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		_, err := appACLCurrentClassifyShape(t, current, transitions, currentApplied, []AppACLManifestPersistedV1{p62Manifest, duplicateP62, third}, "houfeng", appACLCurrentTransitionBindings, currentPrivileges)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("unregistered three-entry chain error = %v, want rebuild-required", err)
		}
	})

	t.Run("P64 old privilege drift", func(t *testing.T) {
		privilegeSet, parseErr := ParseCanonicalPrivilegeSetBodyV1(p64.predecessorPrivilegeBody)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		privilegeSet.Privileges = append(privilegeSet.Privileges, AppACLPrivilege{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: "asset_services",
			Privilege:      AppACLPrivilegeDelete,
		})
		driftedPrivileges, encodeErr := CanonicalPrivilegeSetBodyV1(privilegeSet.RoleBindings, privilegeSet.Privileges)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		drifted, buildErr := NewAppACLManifestPersistedV1(1, appACLCurrentTransitionMigrator, [32]byte{}, p64.predecessor.sources.canonicalSet, driftedPrivileges)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		_, err := appACLCurrentClassifyShape(t, current, transitions, appACLCurrentShapeApplied(t, p64.predecessor), []AppACLManifestPersistedV1{drifted}, "houfeng", appACLCurrentTransitionBindings, currentPrivileges)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("P64 predecessor privilege drift error = %v, want rebuild-required", err)
		}
	})

	t.Run("unknown migrations beat privilege mismatch", func(t *testing.T) {
		unknownApplied := append([]MigrationChecksumEntry(nil), currentApplied...)
		unknownApplied = append(unknownApplied, MigrationChecksumEntry{Filename: "9999_unknown.sql", Checksum: [32]byte{1}})
		unknownMigrations, buildErr := CanonicalMigrationSetBodyV1(unknownApplied)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		unknown, buildErr := NewAppACLManifestPersistedV1(
			1,
			appACLCurrentTransitionMigrator,
			[32]byte{},
			unknownMigrations,
			[]byte("different privileges"),
		)
		if buildErr == nil {
			t.Fatal("invalid privilege bytes should be rejected while building manifest")
		}
		unknown, buildErr = NewAppACLManifestPersistedV1(
			1,
			appACLCurrentTransitionMigrator,
			[32]byte{},
			unknownMigrations,
			currentPrivileges,
		)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		head := &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: unknown.ManifestDigest}
		_, err := classifyAppACLCurrentManifestShape(
			current,
			transitions,
			unknownApplied,
			[]AppACLManifestPersistedV1{unknown},
			head,
			[]byte("different current privileges"),
			appACLCurrentTransitionMigrator,
			"houfeng",
			appACLCurrentTransitionBindings,
		)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("unknown migration shape error = %v, want rebuild-required before privilege comparison", err)
		}
	})

	t.Run("null head", func(t *testing.T) {
		_, err := classifyAppACLCurrentManifestShape(
			current, transitions, currentApplied, []AppACLManifestPersistedV1{currentTarget}, nil,
			currentPrivileges, appACLCurrentTransitionMigrator, "houfeng", appACLCurrentTransitionBindings,
		)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("null head error = %v, want rebuild-required", err)
		}
	})
}

func appACLCurrentShapeManifest(
	t *testing.T,
	revision uint64,
	previous [32]byte,
	source appACLCurrentSourceContract,
	privileges []byte,
) AppACLManifestPersistedV1 {
	t.Helper()
	manifest, err := NewAppACLManifestPersistedV1(revision, appACLCurrentTransitionMigrator, previous, source.sources.canonicalSet, privileges)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func appACLCurrentShapeManifestWithMigrator(
	t *testing.T,
	revision uint64,
	previous [32]byte,
	source appACLCurrentSourceContract,
	privileges []byte,
	migratorRole string,
) AppACLManifestPersistedV1 {
	t.Helper()
	manifest, err := NewAppACLManifestPersistedV1(revision, migratorRole, previous, source.sources.canonicalSet, privileges)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func appACLCurrentShapeApplied(t *testing.T, source appACLCurrentSourceContract) []MigrationChecksumEntry {
	t.Helper()
	applied, err := ParseCanonicalMigrationSetBodyV1(source.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	return applied
}

func appACLCurrentClassifyShape(
	t *testing.T,
	current appACLCurrentSourceContract,
	transitions []appACLCurrentTransition,
	applied []MigrationChecksumEntry,
	manifests []AppACLManifestPersistedV1,
	databaseName string,
	bindings []AppACLRoleBinding,
	compiledPrivileges []byte,
) (appACLCurrentManifestShape, error) {
	t.Helper()
	latest := manifests[len(manifests)-1]
	head := &AppACLManifestHeadV1{ManifestRevision: latest.ManifestRevision, ManifestDigest: latest.ManifestDigest}
	return classifyAppACLCurrentManifestShape(
		current,
		transitions,
		applied,
		manifests,
		head,
		compiledPrivileges,
		latest.MigratorCatalogRole,
		databaseName,
		bindings,
	)
}
