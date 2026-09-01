package migrate

import (
	"bytes"
	"fmt"
)

type appACLCurrentManifestShapeKind uint8

const (
	appACLCurrentManifestShapeGenesis appACLCurrentManifestShapeKind = iota + 1
	appACLCurrentManifestShapePredecessor
	appACLCurrentManifestShapeSuccessor
)

type appACLCurrentManifestShape struct {
	kind       appACLCurrentManifestShapeKind
	latest     AppACLManifestPersistedV1
	transition *appACLCurrentTransition
}

func classifyAppACLCurrentManifestShape(
	current appACLCurrentSourceContract,
	transitions []appACLCurrentTransition,
	applied []MigrationChecksumEntry,
	manifests []AppACLManifestPersistedV1,
	head *AppACLManifestHeadV1,
	compiledPrivileges []byte,
	migratorRole string,
) (appACLCurrentManifestShape, error) {
	if head == nil {
		return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError("APP manifest head is null")
	}
	if err := ValidateAppACLManifestChainV1(manifests, *head); err != nil {
		return appACLCurrentManifestShape{}, fmt.Errorf("validate current app ACL manifest chain: %w", err)
	}
	latest := manifests[len(manifests)-1]
	if latest.MigratorCatalogRole != migratorRole {
		return appACLCurrentManifestShape{}, fmt.Errorf("current app ACL manifest migrator role does not match resolved role")
	}
	if len(manifests) == 1 && bytes.Equal(latest.CanonicalMigrationSet, current.sources.canonicalSet) {
		if !bytes.Equal(latest.CanonicalPrivilegeSet, compiledPrivileges) {
			return appACLCurrentManifestShape{}, fmt.Errorf("current app ACL manifest privilege set does not match current compiler output")
		}
		if err := compareAppACLCurrentMigrationEntries(current.sources.canonicalSet, applied, "applied migration ledger"); err != nil {
			return appACLCurrentManifestShape{}, err
		}
		return appACLCurrentManifestShape{kind: appACLCurrentManifestShapeGenesis, latest: latest}, nil
	}

	if len(manifests) == 1 {
		for index := range transitions {
			transition := &transitions[index]
			if !bytes.Equal(latest.CanonicalMigrationSet, transition.predecessor.sources.canonicalSet) {
				continue
			}
			if latest.ManifestDigest != transition.predecessorManifestDigest ||
				!bytes.Equal(latest.CanonicalPrivilegeSet, transition.predecessorPrivilegeBody) ||
				!bytes.Equal(latest.CanonicalPrivilegeSet, compiledPrivileges) {
				return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError("registered APP predecessor does not match released manifest golden")
			}
			if err := compareAppACLCurrentMigrationEntries(transition.predecessor.sources.canonicalSet, applied, "registered predecessor migration ledger"); err != nil {
				return appACLCurrentManifestShape{}, err
			}
			return appACLCurrentManifestShape{
				kind:       appACLCurrentManifestShapePredecessor,
				latest:     latest,
				transition: transition,
			}, nil
		}
	}

	if len(manifests) == 2 {
		predecessor := manifests[0]
		for index := range transitions {
			transition := &transitions[index]
			if predecessor.ManifestDigest != transition.predecessorManifestDigest ||
				!bytes.Equal(predecessor.CanonicalMigrationSet, transition.predecessor.sources.canonicalSet) {
				continue
			}
			if predecessor.MigratorCatalogRole != migratorRole ||
				!bytes.Equal(predecessor.CanonicalPrivilegeSet, transition.predecessorPrivilegeBody) ||
				!bytes.Equal(predecessor.CanonicalPrivilegeSet, compiledPrivileges) {
				return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError("registered APP predecessor privilege or role drifted")
			}
			if !bytes.Equal(latest.CanonicalMigrationSet, current.sources.canonicalSet) {
				return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError("registered APP successor does not match current migrations")
			}
			if !bytes.Equal(latest.CanonicalPrivilegeSet, compiledPrivileges) {
				return appACLCurrentManifestShape{}, fmt.Errorf("current app ACL manifest privilege set does not match current compiler output")
			}
			if err := compareAppACLCurrentMigrationEntries(current.sources.canonicalSet, applied, "registered successor migration ledger"); err != nil {
				return appACLCurrentManifestShape{}, err
			}
			return appACLCurrentManifestShape{
				kind:       appACLCurrentManifestShapeSuccessor,
				latest:     latest,
				transition: transition,
			}, nil
		}
	}

	return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError(
		"APP manifest and migration ledger do not match a registered current shape",
	)
}
