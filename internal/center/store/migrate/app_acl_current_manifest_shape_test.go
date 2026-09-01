package migrate

import (
	"errors"
	"testing"

	"houfeng/db/migrations"
)

func TestClassifyAppACLCurrentManifestShapeMatrix(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	transition := transitions[0]
	privileges := append([]byte(nil), transition.predecessorPrivilegeBody...)
	predecessor, err := NewAppACLManifestPersistedV1(1, appACLCurrentTransitionMigrator, [32]byte{}, transition.predecessor.sources.canonicalSet, privileges)
	if err != nil {
		t.Fatal(err)
	}
	if predecessor.ManifestDigest != transition.predecessorManifestDigest {
		t.Fatal("predecessor fixture does not match registered released digest")
	}
	currentGenesis, err := NewAppACLManifestPersistedV1(1, appACLCurrentTransitionMigrator, [32]byte{}, current.sources.canonicalSet, privileges)
	if err != nil {
		t.Fatal(err)
	}
	successor, err := NewAppACLManifestPersistedV1(2, appACLCurrentTransitionMigrator, predecessor.ManifestDigest, current.sources.canonicalSet, privileges)
	if err != nil {
		t.Fatal(err)
	}
	predecessorApplied, err := ParseCanonicalMigrationSetBodyV1(transition.predecessor.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}
	currentApplied, err := ParseCanonicalMigrationSetBodyV1(current.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name      string
		applied   []MigrationChecksumEntry
		manifests []AppACLManifestPersistedV1
		head      AppACLManifestHeadV1
		wantKind  appACLCurrentManifestShapeKind
	}{
		{
			name:      "fresh current genesis",
			applied:   currentApplied,
			manifests: []AppACLManifestPersistedV1{currentGenesis},
			head:      AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: currentGenesis.ManifestDigest},
			wantKind:  appACLCurrentManifestShapeGenesis,
		},
		{
			name:      "registered predecessor",
			applied:   predecessorApplied,
			manifests: []AppACLManifestPersistedV1{predecessor},
			head:      AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: predecessor.ManifestDigest},
			wantKind:  appACLCurrentManifestShapePredecessor,
		},
		{
			name:      "registered successor",
			applied:   currentApplied,
			manifests: []AppACLManifestPersistedV1{predecessor, successor},
			head:      AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: successor.ManifestDigest},
			wantKind:  appACLCurrentManifestShapeSuccessor,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			shape, err := classifyAppACLCurrentManifestShape(
				current, transitions, tc.applied, tc.manifests, &tc.head,
				privileges, appACLCurrentTransitionMigrator,
			)
			if err != nil {
				t.Fatal(err)
			}
			if shape.kind != tc.wantKind || shape.latest.ManifestDigest != tc.manifests[len(tc.manifests)-1].ManifestDigest {
				t.Fatalf("shape = %#v, want kind %d/latest %x", shape, tc.wantKind, tc.manifests[len(tc.manifests)-1].ManifestDigest)
			}
			if tc.wantKind == appACLCurrentManifestShapePredecessor || tc.wantKind == appACLCurrentManifestShapeSuccessor {
				if shape.transition == nil || shape.transition.predecessorManifestDigest != transition.predecessorManifestDigest {
					t.Fatalf("registered shape transition = %#v", shape.transition)
				}
			}
		})
	}
}

func TestClassifyAppACLCurrentManifestShapeRejectsUnknownAndMalformedState(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	transition := transitions[0]
	privileges := transition.predecessorPrivilegeBody
	predecessor, err := NewAppACLManifestPersistedV1(1, appACLCurrentTransitionMigrator, [32]byte{}, transition.predecessor.sources.canonicalSet, privileges)
	if err != nil {
		t.Fatal(err)
	}
	currentApplied, err := ParseCanonicalMigrationSetBodyV1(current.sources.canonicalSet)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("partial ledger", func(t *testing.T) {
		head := &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: predecessor.ManifestDigest}
		_, err := classifyAppACLCurrentManifestShape(current, transitions, currentApplied, []AppACLManifestPersistedV1{predecessor}, head, privileges, appACLCurrentTransitionMigrator)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("partial state error = %v, want rebuild-required", err)
		}
	})

	t.Run("wrong previous digest", func(t *testing.T) {
		var wrong [32]byte
		wrong[0] = 1
		successor, buildErr := NewAppACLManifestPersistedV1(2, appACLCurrentTransitionMigrator, wrong, current.sources.canonicalSet, privileges)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		head := &AppACLManifestHeadV1{ManifestRevision: 2, ManifestDigest: successor.ManifestDigest}
		_, err := classifyAppACLCurrentManifestShape(current, transitions, currentApplied, []AppACLManifestPersistedV1{predecessor, successor}, head, privileges, appACLCurrentTransitionMigrator)
		if err == nil || errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("malformed chain error = %v, want retained chain error", err)
		}
	})

	t.Run("unknown migration shape wins over privilege mismatch", func(t *testing.T) {
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
			privileges,
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
		)
		if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
			t.Fatalf("unknown migration shape error = %v, want rebuild-required before privilege comparison", err)
		}
	})
}
