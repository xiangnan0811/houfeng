package migrate

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type appACLManifestSuccessorWriterDependencies struct {
	readManifests func(context.Context, pgx.Tx) ([]AppACLManifestPersistedV1, error)
	readHead      func(context.Context, pgx.Tx) (*AppACLManifestHeadV1, error)
}

func defaultAppACLManifestSuccessorWriterDependencies() appACLManifestSuccessorWriterDependencies {
	return appACLManifestSuccessorWriterDependencies{
		readManifests: readAppACLManifestRevisionsV1,
		readHead:      readAppACLManifestHeadV1,
	}
}

func insertAppACLManifestSuccessorV1(
	ctx context.Context,
	tx pgx.Tx,
	previous AppACLManifestPersistedV1,
	canonicalMigrationSet []byte,
	canonicalPrivilegeSet []byte,
) (AppACLManifestPersistedV1, error) {
	return insertAppACLManifestSuccessorV1WithDependencies(
		ctx,
		tx,
		previous,
		canonicalMigrationSet,
		canonicalPrivilegeSet,
		defaultAppACLManifestSuccessorWriterDependencies(),
	)
}

func insertAppACLManifestSuccessorV1WithDependencies(
	ctx context.Context,
	tx pgx.Tx,
	previous AppACLManifestPersistedV1,
	canonicalMigrationSet []byte,
	canonicalPrivilegeSet []byte,
	dependencies appACLManifestSuccessorWriterDependencies,
) (AppACLManifestPersistedV1, error) {
	if tx == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest successor has no PostgreSQL transaction")
	}
	if dependencies.readManifests == nil || dependencies.readHead == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest successor readback dependency is nil")
	}
	if err := previous.Validate(); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("validate app ACL manifest predecessor: %w", err)
	}
	if previous.ManifestRevision >= 999999 {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest successor revision exceeds v1 bounds")
	}

	successor, err := NewAppACLManifestPersistedV1(
		previous.ManifestRevision+1,
		previous.MigratorCatalogRole,
		previous.ManifestDigest,
		canonicalMigrationSet,
		canonicalPrivilegeSet,
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("build app ACL manifest successor: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.app_acl_manifest_revisions (
			manifest_revision,
			migrator_catalog_role,
			previous_manifest_digest,
			canonical_migration_set,
			sorted_migration_set_digest,
			canonical_privilege_set,
			privilege_set_digest,
			manifest_digest
		) values ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		int64(successor.ManifestRevision),
		successor.MigratorCatalogRole,
		successor.PreviousManifestDigest[:],
		successor.CanonicalMigrationSet,
		successor.MigrationSetDigest[:],
		successor.CanonicalPrivilegeSet,
		successor.PrivilegeSetDigest[:],
		successor.ManifestDigest[:],
	); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("insert app ACL manifest successor revision: %w", err)
	}

	result, err := tx.Exec(ctx, `
		update public.app_acl_manifest_head
		set manifest_revision = $1, manifest_digest = $2
		where singleton
		  and manifest_revision = $3
		  and manifest_digest = $4
	`,
		int64(successor.ManifestRevision),
		successor.ManifestDigest[:],
		int64(previous.ManifestRevision),
		previous.ManifestDigest[:],
	)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("cas app ACL manifest successor head: %w", err)
	}
	if result.RowsAffected() != 1 {
		return AppACLManifestPersistedV1{}, fmt.Errorf("app ACL manifest head changed concurrently")
	}

	manifests, err := dependencies.readManifests(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("read back app ACL manifest successor revisions: %w", err)
	}
	if len(manifests) != int(successor.ManifestRevision) ||
		manifests[len(manifests)-1].ManifestDigest != successor.ManifestDigest {
		return AppACLManifestPersistedV1{}, fmt.Errorf("read back app ACL manifest successor revision did not match inserted value")
	}
	head, err := dependencies.readHead(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("read back app ACL manifest successor head: %w", err)
	}
	if head == nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("read back app ACL manifest successor head is null")
	}
	if err := ValidateAppACLManifestChainV1(manifests, *head); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("validate read back app ACL manifest successor: %w", err)
	}
	return successor, nil
}
