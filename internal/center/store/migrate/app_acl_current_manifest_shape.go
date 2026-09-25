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
	databaseName string,
	roleBindings []AppACLRoleBinding,
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
	if len(transitions) == 0 && len(manifests) == 1 && bytes.Equal(latest.CanonicalMigrationSet, current.sources.canonicalSet) {
		if !bytes.Equal(latest.CanonicalPrivilegeSet, compiledPrivileges) {
			return appACLCurrentManifestShape{}, fmt.Errorf("current app ACL manifest privilege set does not match current compiler output")
		}
		if err := compareAppACLCurrentMigrationEntries(current.sources.canonicalSet, applied, "applied migration ledger"); err != nil {
			return appACLCurrentManifestShape{}, err
		}
		return appACLCurrentManifestShape{kind: appACLCurrentManifestShapeGenesis, latest: latest}, nil
	}
	if len(transitions) != 2 ||
		transitions[0].profile != appACLCurrentProfileP62 ||
		transitions[1].profile != appACLCurrentProfileP64 {
		return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError("APP transition registry is not the exact P62/P64 profile set")
	}
	for _, manifest := range manifests[:len(manifests)-1] {
		if manifest.MigratorCatalogRole != migratorRole {
			return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError("registered APP manifest chain changes migrator role")
		}
	}

	isCurrent := bytes.Equal(latest.CanonicalMigrationSet, current.sources.canonicalSet)
	var predecessorProfiles []appACLCurrentProfileID
	var predecessorTransition *appACLCurrentTransition
	switch {
	case isCurrent && len(manifests) == 1:
		// The complete current target may be installed directly as a genesis.
	case isCurrent:
		for _, profileChain := range appACLCurrentAcceptedPredecessorProfileChains {
			if appACLCurrentManifestMatchesProfileChain(manifests[:len(manifests)-1], profileChain, transitions) {
				predecessorProfiles = profileChain
				break
			}
		}
		if len(predecessorProfiles) == 0 {
			return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError(
				"APP manifest and migration ledger do not match a registered current shape",
			)
		}
		predecessorTransition = appACLCurrentTransitionForProfile(transitions, predecessorProfiles[len(predecessorProfiles)-1])
	default:
		for _, profileChain := range appACLCurrentAcceptedPredecessorProfileChains {
			if appACLCurrentManifestMatchesProfileChain(manifests, profileChain, transitions) {
				predecessorProfiles = profileChain
				break
			}
		}
		if len(predecessorProfiles) == 0 {
			return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError(
				"APP manifest and migration ledger do not match a registered current shape",
			)
		}
		predecessorTransition = appACLCurrentTransitionForProfile(transitions, predecessorProfiles[len(predecessorProfiles)-1])
	}

	for index, profile := range predecessorProfiles {
		transition := appACLCurrentTransitionForProfile(transitions, profile)
		expectedPrivileges, err := appACLCurrentTransitionPrivilegeBodyFor(
			transition.predecessor,
			databaseName,
			roleBindings,
			migratorRole,
		)
		if err != nil {
			return appACLCurrentManifestShape{}, fmt.Errorf("compile registered APP predecessor profile %d privileges: %w", profile, err)
		}
		manifest := manifests[index]
		if !bytes.Equal(manifest.CanonicalPrivilegeSet, expectedPrivileges) {
			return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError(
				"registered APP predecessor privilege or role binding drifted",
			)
		}
		if profile == appACLCurrentProfileP62 && manifest.ManifestDigest != appACLCurrentV0794ManifestDigestGolden {
			return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError(
				"registered P62 APP manifest does not match the released manifest golden",
			)
		}
	}

	if isCurrent {
		if !bytes.Equal(latest.CanonicalPrivilegeSet, compiledPrivileges) {
			return appACLCurrentManifestShape{}, fmt.Errorf("current app ACL manifest privilege set does not match current compiler output")
		}
		if err := compareAppACLCurrentMigrationEntries(current.sources.canonicalSet, applied, "applied migration ledger"); err != nil {
			return appACLCurrentManifestShape{}, err
		}
		if len(predecessorProfiles) == 0 {
			return appACLCurrentManifestShape{kind: appACLCurrentManifestShapeGenesis, latest: latest}, nil
		}
		return appACLCurrentManifestShape{
			kind:       appACLCurrentManifestShapeSuccessor,
			latest:     latest,
			transition: predecessorTransition,
		}, nil
	}

	if !bytes.Equal(latest.CanonicalMigrationSet, predecessorTransition.predecessor.sources.canonicalSet) {
		return appACLCurrentManifestShape{}, appACLDevelopmentDatabaseRebuildError(
			"registered APP predecessor does not match its exact source profile",
		)
	}
	if err := compareAppACLCurrentMigrationEntries(
		predecessorTransition.predecessor.sources.canonicalSet,
		applied,
		"registered predecessor migration ledger",
	); err != nil {
		return appACLCurrentManifestShape{}, err
	}
	return appACLCurrentManifestShape{
		kind:       appACLCurrentManifestShapePredecessor,
		latest:     latest,
		transition: predecessorTransition,
	}, nil
}

func appACLCurrentManifestMatchesProfileChain(
	manifests []AppACLManifestPersistedV1,
	profiles []appACLCurrentProfileID,
	transitions []appACLCurrentTransition,
) bool {
	if len(manifests) != len(profiles) {
		return false
	}
	for index, profile := range profiles {
		transition := appACLCurrentTransitionForProfile(transitions, profile)
		if transition == nil || !bytes.Equal(manifests[index].CanonicalMigrationSet, transition.predecessor.sources.canonicalSet) {
			return false
		}
	}
	return true
}

func appACLCurrentTransitionForProfile(
	transitions []appACLCurrentTransition,
	profile appACLCurrentProfileID,
) *appACLCurrentTransition {
	for index := range transitions {
		if transitions[index].profile == profile {
			return &transitions[index]
		}
	}
	return nil
}
