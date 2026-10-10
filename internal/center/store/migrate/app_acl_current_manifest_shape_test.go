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
	currentPrivileges, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil {
		t.Fatal(err)
	}
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
	currentApplied := appACLCurrentShapeApplied(t, current)

	t.Run("current target genesis", func(t *testing.T) {
		shape, err := appACLCurrentClassifyShape(
			t,
			current,
			transitions,
			currentApplied,
			[]AppACLManifestPersistedV1{currentGenesis},
			"houfeng",
			appACLCurrentTransitionBindings,
			currentPrivileges,
		)
		if err != nil {
			t.Fatal(err)
		}
		if shape.kind != appACLCurrentManifestShapeGenesis ||
			shape.latest.ManifestDigest != currentGenesis.ManifestDigest {
			t.Fatalf("shape = %#v, want current genesis/latest %x", shape, currentGenesis.ManifestDigest)
		}
		if shape.transition != nil {
			t.Fatalf("genesis shape transition = %#v, want nil", shape.transition)
		}
	})

	findCompiledTransition := func(profile appACLCurrentProfileID) *appACLCurrentTransition {
		for index := range transitions {
			if transitions[index].profile == profile {
				return &transitions[index]
			}
		}
		t.Fatalf("compiled transition profile %d not found", profile)
		return nil
	}

	// Keep this list independent from the production acceptance table.
	// Each case is a published predecessor history that must be recognized by
	// the classifier, rather than a reflection of the implementation registry.
	testChains := []struct {
		name     string
		profiles []appACLCurrentProfileID
	}{
		{name: "P62", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62}},
		{name: "P63", profiles: []appACLCurrentProfileID{appACLCurrentProfileP63}},
		{name: "P64", profiles: []appACLCurrentProfileID{appACLCurrentProfileP64}},
		{name: "P66", profiles: []appACLCurrentProfileID{appACLCurrentProfileP66}},
		{name: "P62-P63", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP63}},
		{name: "P62-P64", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP64}},
		{name: "P62-P66", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP66}},
		{name: "P64-P66", profiles: []appACLCurrentProfileID{appACLCurrentProfileP64, appACLCurrentProfileP66}},
		{name: "P63-P66", profiles: []appACLCurrentProfileID{appACLCurrentProfileP63, appACLCurrentProfileP66}},
		{name: "P62-P64-P66", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66}},
		{name: "P62-P63-P66", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66}},
		{name: "P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP67}},
		{name: "P62-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP67}},
		{name: "P64-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP64, appACLCurrentProfileP67}},
		{name: "P62-P64-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP67}},
		{name: "P63-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP63, appACLCurrentProfileP67}},
		{name: "P62-P63-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP67}},
		{name: "P66-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP66, appACLCurrentProfileP67}},
		{name: "P62-P66-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP66, appACLCurrentProfileP67}},
		{name: "P64-P66-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67}},
		{name: "P62-P64-P66-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67}},
		{name: "P63-P66-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67}},
		{name: "P62-P63-P66-P67", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67}},
		{name: "P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP68}},
		{name: "P62-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP68}},
		{name: "P63-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP63, appACLCurrentProfileP68}},
		{name: "P64-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP64, appACLCurrentProfileP68}},
		{name: "P66-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP66, appACLCurrentProfileP68}},
		{name: "P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P62-P63-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP68}},
		{name: "P62-P64-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP68}},
		{name: "P62-P66-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP66, appACLCurrentProfileP68}},
		{name: "P64-P66-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP68}},
		{name: "P63-P66-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP68}},
		{name: "P62-P64-P66-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP68}},
		{name: "P62-P63-P66-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP68}},
		{name: "P62-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P64-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP64, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P62-P64-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P63-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP63, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P62-P63-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P66-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P62-P66-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P64-P66-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P62-P64-P66-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP64, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P63-P66-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68}},
		{name: "P62-P63-P66-P67-P68", profiles: []appACLCurrentProfileID{appACLCurrentProfileP62, appACLCurrentProfileP63, appACLCurrentProfileP66, appACLCurrentProfileP67, appACLCurrentProfileP68}},
	}
	if len(testChains) != 47 {
		t.Fatalf("published predecessor chain cases = %d, want 47", len(testChains))
	}
	allTestChains := append([]struct {
		name     string
		profiles []appACLCurrentProfileID
	}{}, testChains...)
	for _, tc := range testChains {
		allTestChains = append(allTestChains, struct {
			name     string
			profiles []appACLCurrentProfileID
		}{
			name:     "C69-" + tc.name,
			profiles: append(append([]appACLCurrentProfileID(nil), tc.profiles...), appACLCurrentProfileP69),
		})
	}
	allTestChains = append(allTestChains, struct {
		name     string
		profiles []appACLCurrentProfileID
	}{
		name:     "P69",
		profiles: []appACLCurrentProfileID{appACLCurrentProfileP69},
	})
	if len(allTestChains) != 95 {
		t.Fatalf("independent C70 predecessor chain cases = %d, want 95", len(allTestChains))
	}
	c71Chains := append([]struct {
		name     string
		profiles []appACLCurrentProfileID
	}{}, allTestChains...)
	allTestChains = append(allTestChains, struct {
		name     string
		profiles []appACLCurrentProfileID
	}{
		name:     "P70",
		profiles: []appACLCurrentProfileID{appACLCurrentProfileP70},
	})
	for _, tc := range c71Chains {
		allTestChains = append(allTestChains, struct {
			name     string
			profiles []appACLCurrentProfileID
		}{
			name:     "C70-" + tc.name,
			profiles: append(append([]appACLCurrentProfileID(nil), tc.profiles...), appACLCurrentProfileP70),
		})
	}
	if len(allTestChains) != 191 {
		t.Fatalf("independent C71 predecessor chain cases = %d, want 191", len(allTestChains))
	}
	c72Chains := append([]struct {
		name     string
		profiles []appACLCurrentProfileID
	}{}, allTestChains...)
	allTestChains = append(allTestChains, struct {
		name     string
		profiles []appACLCurrentProfileID
	}{
		name:     "P71",
		profiles: []appACLCurrentProfileID{appACLCurrentProfileP71},
	})
	for _, tc := range c72Chains {
		allTestChains = append(allTestChains, struct {
			name     string
			profiles []appACLCurrentProfileID
		}{
			name:     "C71-" + tc.name,
			profiles: append(append([]appACLCurrentProfileID(nil), tc.profiles...), appACLCurrentProfileP71),
		})
	}
	if len(allTestChains) != 383 {
		t.Fatalf("independent C72 predecessor chain cases = %d, want 383", len(allTestChains))
	}
	c73Chains := append([]struct {
		name     string
		profiles []appACLCurrentProfileID
	}{}, allTestChains...)
	allTestChains = append(allTestChains, struct {
		name     string
		profiles []appACLCurrentProfileID
	}{
		name:     "P72",
		profiles: []appACLCurrentProfileID{appACLCurrentProfileP72},
	})
	for _, tc := range c73Chains {
		allTestChains = append(allTestChains, struct {
			name     string
			profiles []appACLCurrentProfileID
		}{
			name:     "C72-" + tc.name,
			profiles: append(append([]appACLCurrentProfileID(nil), tc.profiles...), appACLCurrentProfileP72),
		})
	}
	if len(allTestChains) != 767 {
		t.Fatalf("independent C73 predecessor chain cases = %d, want 767", len(allTestChains))
	}
	if len(allTestChains) != len(appACLCurrentAcceptedPredecessorProfileChains) {
		t.Fatalf("test chain cases = %d, production accepted chains = %d", len(allTestChains), len(appACLCurrentAcceptedPredecessorProfileChains))
	}

	for _, tc := range allTestChains {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			manifests := make([]AppACLManifestPersistedV1, 0, len(tc.profiles))
			previous := [32]byte{}
			for revision, profile := range tc.profiles {
				transition := findCompiledTransition(profile)
				manifest := appACLCurrentShapeManifest(
					t,
					uint64(revision+1),
					previous,
					transition.predecessor,
					transition.predecessorPrivilegeBody,
				)
				manifests = append(manifests, manifest)
				previous = manifest.ManifestDigest
			}
			lastProfile := tc.profiles[len(tc.profiles)-1]
			lastTransition := findCompiledTransition(lastProfile)
			appliedPredecessor := appACLCurrentShapeApplied(t, lastTransition.predecessor)

			predecessorShape, err := appACLCurrentClassifyShape(
				t,
				current,
				transitions,
				appliedPredecessor,
				manifests,
				"houfeng",
				appACLCurrentTransitionBindings,
				currentPrivileges,
			)
			if err != nil {
				t.Fatalf("classify predecessor: %v", err)
			}
			if predecessorShape.kind != appACLCurrentManifestShapePredecessor ||
				predecessorShape.latest.ManifestDigest != manifests[len(manifests)-1].ManifestDigest ||
				predecessorShape.transition == nil ||
				predecessorShape.transition.profile != lastProfile {
				t.Fatalf(
					"predecessor shape = %#v, want kind %d/latest %x/profile %d",
					predecessorShape,
					appACLCurrentManifestShapePredecessor,
					manifests[len(manifests)-1].ManifestDigest,
					lastProfile,
				)
			}

			successor := appACLCurrentShapeManifest(
				t,
				uint64(len(manifests)+1),
				previous,
				current,
				currentPrivileges,
			)
			successorManifests := append(append([]AppACLManifestPersistedV1(nil), manifests...), successor)
			successorShape, err := appACLCurrentClassifyShape(
				t,
				current,
				transitions,
				currentApplied,
				successorManifests,
				"houfeng",
				appACLCurrentTransitionBindings,
				currentPrivileges,
			)
			if err != nil {
				t.Fatalf("classify C73 successor: %v", err)
			}
			if successorShape.kind != appACLCurrentManifestShapeSuccessor ||
				successorShape.latest.ManifestDigest != successor.ManifestDigest ||
				successorShape.transition == nil ||
				successorShape.transition.profile != lastProfile {
				t.Fatalf(
					"C73 successor shape = %#v, want kind %d/latest %x/profile %d",
					successorShape,
					appACLCurrentManifestShapeSuccessor,
					successor.ManifestDigest,
					lastProfile,
				)
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
	p62, p64, p68 := transitions[0], transitions[1], transitions[5]
	p62Manifest := appACLCurrentShapeManifest(t, 1, [32]byte{}, p62.predecessor, p62.predecessorPrivilegeBody)
	p68Manifest := appACLCurrentShapeManifest(t, 1, [32]byte{}, p68.predecessor, p68.predecessorPrivilegeBody)
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
	t.Run("P68 duplicate predecessor", func(t *testing.T) {
		duplicate, buildErr := NewAppACLManifestPersistedV1(
			2,
			appACLCurrentTransitionMigrator,
			p68Manifest.ManifestDigest,
			p68.predecessor.sources.canonicalSet,
			p68.predecessorPrivilegeBody,
		)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		successor, buildErr := NewAppACLManifestPersistedV1(
			3,
			appACLCurrentTransitionMigrator,
			duplicate.ManifestDigest,
			current.sources.canonicalSet,
			currentPrivileges,
		)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		_, err := appACLCurrentClassifyShape(
			t,
			current,
			transitions,
			currentApplied,
			[]AppACLManifestPersistedV1{p68Manifest, duplicate, successor},
			"houfeng",
			appACLCurrentTransitionBindings,
			currentPrivileges,
		)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("duplicate P68 chain error = %v, want rebuild-required", err)
		}
	})

	t.Run("P68 successor has an extra revision", func(t *testing.T) {
		first, buildErr := NewAppACLManifestPersistedV1(
			2,
			appACLCurrentTransitionMigrator,
			p68Manifest.ManifestDigest,
			current.sources.canonicalSet,
			currentPrivileges,
		)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		second, buildErr := NewAppACLManifestPersistedV1(
			3,
			appACLCurrentTransitionMigrator,
			first.ManifestDigest,
			current.sources.canonicalSet,
			currentPrivileges,
		)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		_, err := appACLCurrentClassifyShape(
			t,
			current,
			transitions,
			currentApplied,
			[]AppACLManifestPersistedV1{p68Manifest, first, second},
			"houfeng",
			appACLCurrentTransitionBindings,
			currentPrivileges,
		)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("extra C71 revision error = %v, want rebuild-required", err)
		}
	})

	t.Run("unknown P63-P64-P68 predecessor chain", func(t *testing.T) {
		p63 := transitions[2]
		p63Manifest := appACLCurrentShapeManifest(t, 1, [32]byte{}, p63.predecessor, p63.predecessorPrivilegeBody)
		p64Manifest := appACLCurrentShapeManifest(t, 2, p63Manifest.ManifestDigest, p64.predecessor, p64.predecessorPrivilegeBody)
		p68Manifest := appACLCurrentShapeManifest(t, 3, p64Manifest.ManifestDigest, p68.predecessor, p68.predecessorPrivilegeBody)
		successor := appACLCurrentShapeManifest(t, 4, p68Manifest.ManifestDigest, current, currentPrivileges)
		_, err := appACLCurrentClassifyShape(
			t,
			current,
			transitions,
			currentApplied,
			[]AppACLManifestPersistedV1{p63Manifest, p64Manifest, p68Manifest, successor},
			"houfeng",
			appACLCurrentTransitionBindings,
			currentPrivileges,
		)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("unknown P63-P64-P68 chain error = %v, want rebuild-required", err)
		}
	})

	t.Run("P68 predecessor checksum drift", func(t *testing.T) {
		driftedApplied := appACLCurrentShapeApplied(t, p68.predecessor)
		driftedApplied[0].Checksum[0] ^= 0xff
		_, err := appACLCurrentClassifyShape(
			t,
			current,
			transitions,
			driftedApplied,
			[]AppACLManifestPersistedV1{p68Manifest},
			"houfeng",
			appACLCurrentTransitionBindings,
			currentPrivileges,
		)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("P68 checksum drift error = %v, want rebuild-required", err)
		}
	})

	t.Run("P68 predecessor privilege drift", func(t *testing.T) {
		privilegeSet, parseErr := ParseCanonicalPrivilegeSetBodyV1(p68.predecessorPrivilegeBody)
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
		drifted, buildErr := NewAppACLManifestPersistedV1(
			1,
			appACLCurrentTransitionMigrator,
			[32]byte{},
			p68.predecessor.sources.canonicalSet,
			driftedPrivileges,
		)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		_, err := appACLCurrentClassifyShape(
			t,
			current,
			transitions,
			appACLCurrentShapeApplied(t, p68.predecessor),
			[]AppACLManifestPersistedV1{drifted},
			"houfeng",
			appACLCurrentTransitionBindings,
			currentPrivileges,
		)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("P68 privilege drift error = %v, want rebuild-required", err)
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
