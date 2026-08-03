package migrate

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/auth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/store"
)

const postgresIntegrationFlag = "HOUFENG_POSTGRES_INTEGRATION"

func TestPostgresIntegrationAppACLManifestRuntimeReader(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	var databaseName, sessionUser, currentUser string
	if err := db.QueryRow(ctx, `select current_database(), session_user, current_user`).Scan(&databaseName, &sessionUser, &currentUser); err != nil {
		t.Fatalf("read runtime database and identities: %v", err)
	}
	reader := NewPostgresAppACLManifestRuntimeReader(db)
	fresh, err := reader.ReadAppACLManifestRuntimeSnapshotV1(ctx)
	if err != nil {
		t.Fatalf("ReadAppACLManifestRuntimeSnapshotV1() fresh error = %v", err)
	}
	if fresh.Head != nil || len(fresh.Manifests) != 0 || len(fresh.AppliedMigrations) != currentRootSourceCount {
		t.Fatalf("fresh snapshot = %#v, want null head, zero revisions, and %d migrations", fresh, currentRootSourceCount)
	}
	if fresh.DatabaseName != databaseName || fresh.SessionUser != sessionUser || fresh.CurrentUser != currentUser {
		t.Fatalf("fresh runtime snapshot = (%q, %q, %q), want (%q, %q, %q)", fresh.DatabaseName, fresh.SessionUser, fresh.CurrentUser, databaseName, sessionUser, currentUser)
	}

	migrationBody, err := CanonicalMigrationSetFromFS(migrations.FS)
	if err != nil {
		t.Fatalf("CanonicalMigrationSetFromFS() error = %v", err)
	}
	privilegeBody, err := CompileAppACLPrivilegeSetR1(databaseName,
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: currentUser},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
	)
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() error = %v", err)
	}
	manifest, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}
	if _, err := db.Exec(ctx, `
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
		int64(manifest.ManifestRevision),
		manifest.MigratorCatalogRole,
		manifest.PreviousManifestDigest[:],
		manifest.CanonicalMigrationSet,
		manifest.MigrationSetDigest[:],
		manifest.CanonicalPrivilegeSet,
		manifest.PrivilegeSetDigest[:],
		manifest.ManifestDigest[:],
	); err != nil {
		t.Fatalf("insert app ACL manifest revision: %v", err)
	}
	if _, err := db.Exec(ctx, `
		update public.app_acl_manifest_head
		set manifest_revision = $1, manifest_digest = $2
		where singleton
	`, int64(manifest.ManifestRevision), manifest.ManifestDigest[:]); err != nil {
		t.Fatalf("update app ACL manifest head: %v", err)
	}
	var persistedMigratorCatalogRole string
	if err := db.QueryRow(ctx, `
		select migrator_catalog_role
		from public.app_acl_manifest_revisions
		where manifest_revision = $1
	`, int64(manifest.ManifestRevision)).Scan(&persistedMigratorCatalogRole); err != nil {
		t.Fatalf("read persisted migrator catalog role: %v", err)
	}
	if persistedMigratorCatalogRole != manifest.MigratorCatalogRole {
		t.Fatalf("persisted migrator catalog role = %q, want %q", persistedMigratorCatalogRole, manifest.MigratorCatalogRole)
	}

	snapshot, err := reader.ReadAppACLManifestRuntimeSnapshotV1(ctx)
	if err != nil {
		t.Fatalf("ReadAppACLManifestRuntimeSnapshotV1() error = %v", err)
	}
	if snapshot.Head == nil || snapshot.Head.ManifestRevision != manifest.ManifestRevision || snapshot.Head.ManifestDigest != manifest.ManifestDigest {
		t.Fatalf("snapshot head = %#v, want revision %d digest %x", snapshot.Head, manifest.ManifestRevision, manifest.ManifestDigest)
	}
	if len(snapshot.Manifests) != 1 || snapshot.Manifests[0].ManifestDigest != manifest.ManifestDigest ||
		snapshot.Manifests[0].MigratorCatalogRole != persistedMigratorCatalogRole ||
		!bytes.Equal(snapshot.Manifests[0].CanonicalMigrationSet, manifest.CanonicalMigrationSet) ||
		!bytes.Equal(snapshot.Manifests[0].CanonicalPrivilegeSet, manifest.CanonicalPrivilegeSet) {
		t.Fatalf("snapshot manifests = %#v, want %#v", snapshot.Manifests, manifest)
	}
	if snapshot.DatabaseName != databaseName || snapshot.SessionUser != sessionUser || snapshot.CurrentUser != currentUser {
		t.Fatalf("runtime snapshot = (%q, %q, %q), want (%q, %q, %q)", snapshot.DatabaseName, snapshot.SessionUser, snapshot.CurrentUser, databaseName, sessionUser, currentUser)
	}
	if _, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, reader, migrations.FS); err != nil {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v", err)
	}

	t.Run("rejects SET ROLE identity bypass", func(t *testing.T) {
		suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), os.Getpid())
		runtimeRole := "houfeng_runtime_" + suffix
		memberLogin := "houfeng_runtime_member_" + suffix
		quotedRuntimeRole := quotePostgresIdentifier(runtimeRole)
		quotedMemberLogin := quotePostgresIdentifier(memberLogin)
		var memberPasswordEntropy [32]byte
		if _, err := rand.Read(memberPasswordEntropy[:]); err != nil {
			t.Fatalf("generate temporary member login password: %v", err)
		}
		memberPassword := "test-" + hex.EncodeToString(memberPasswordEntropy[:])
		runtimeRoleCreated := false
		memberLoginCreated := false
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if runtimeRoleCreated && memberLoginCreated {
				if _, err := db.Exec(cleanupCtx, `revoke `+quotedRuntimeRole+` from `+quotedMemberLogin); err != nil {
					t.Errorf("revoke temporary runtime role %q from member login %q: %v", runtimeRole, memberLogin, err)
				}
			}
			for _, role := range []struct {
				name    string
				quoted  string
				created bool
			}{
				{name: memberLogin, quoted: quotedMemberLogin, created: memberLoginCreated},
				{name: runtimeRole, quoted: quotedRuntimeRole, created: runtimeRoleCreated},
			} {
				if !role.created {
					continue
				}
				if _, err := db.Exec(cleanupCtx, `reassign owned by `+role.quoted+` to `+quotePostgresIdentifier(currentUser)); err != nil {
					t.Errorf("reassign temporary role %q ownership: %v", role.name, err)
				}
				if _, err := db.Exec(cleanupCtx, `drop owned by `+role.quoted); err != nil {
					t.Errorf("drop temporary role %q dependencies: %v", role.name, err)
				}
				if _, err := db.Exec(cleanupCtx, `drop role if exists `+role.quoted); err != nil {
					t.Errorf("drop temporary role %q: %v", role.name, err)
				}
			}
		})
		if _, err := db.Exec(ctx, `create role `+quotedRuntimeRole+` nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`); err != nil {
			t.Fatalf("create temporary runtime role %q: %v", runtimeRole, err)
		}
		runtimeRoleCreated = true
		if _, err := db.Exec(ctx, `create role `+quotedMemberLogin+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+memberPassword+`'`); err != nil {
			t.Fatalf("create temporary member login %q: %v", memberLogin, err)
		}
		memberLoginCreated = true
		if _, err := db.Exec(ctx, `grant `+quotedRuntimeRole+` to `+quotedMemberLogin); err != nil {
			t.Fatalf("grant temporary runtime role %q to member login %q: %v", runtimeRole, memberLogin, err)
		}
		databaseName := currentPostgresDatabaseName(t, ctx, db)
		if _, err := db.Exec(ctx, `grant connect on database `+quotePostgresIdentifier(databaseName)+` to `+quotedMemberLogin); err != nil {
			t.Fatalf("grant member login database connect: %v", err)
		}
		if _, err := db.Exec(ctx, `grant usage on schema public to `+quotedRuntimeRole); err != nil {
			t.Fatalf("grant runtime role schema usage: %v", err)
		}
		if _, err := db.Exec(ctx, `grant select on table public.app_acl_manifest_revisions, public.app_acl_manifest_head, public.schema_migrations to `+quotedRuntimeRole); err != nil {
			t.Fatalf("grant runtime role manifest reads: %v", err)
		}

		runtimePrivilegeBody, err := CompileAppACLPrivilegeSetR1(databaseName,
			[]AppACLRoleBinding{
				{Subject: AppACLSubjectCenterRuntime, CatalogRole: runtimeRole},
				{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
			},
		)
		if err != nil {
			t.Fatalf("CompileAppACLPrivilegeSetR1() runtime role error = %v", err)
		}
		runtimeManifest, err := NewAppACLManifestPersistedV1(2, "houfeng_migrator", manifest.ManifestDigest, migrationBody, runtimePrivilegeBody)
		if err != nil {
			t.Fatalf("NewAppACLManifestPersistedV1() runtime role error = %v", err)
		}
		if _, err := db.Exec(ctx, `
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
			int64(runtimeManifest.ManifestRevision),
			runtimeManifest.MigratorCatalogRole,
			runtimeManifest.PreviousManifestDigest[:],
			runtimeManifest.CanonicalMigrationSet,
			runtimeManifest.MigrationSetDigest[:],
			runtimeManifest.CanonicalPrivilegeSet,
			runtimeManifest.PrivilegeSetDigest[:],
			runtimeManifest.ManifestDigest[:],
		); err != nil {
			t.Fatalf("insert runtime role manifest revision: %v", err)
		}
		if _, err := db.Exec(ctx, `
			update public.app_acl_manifest_head
			set manifest_revision = $1, manifest_digest = $2
			where singleton
		`, int64(runtimeManifest.ManifestRevision), runtimeManifest.ManifestDigest[:]); err != nil {
			t.Fatalf("advance app ACL manifest head for runtime role: %v", err)
		}

		runtimePoolConfig := db.Config().Copy()
		runtimePoolConfig.MaxConns = 1
		runtimePoolConfig.MinConns = 0
		runtimePoolConfig.ConnConfig.User = memberLogin
		runtimePoolConfig.ConnConfig.Password = memberPassword
		runtimePoolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, `set role `+quotedRuntimeRole)
			return err
		}
		runtimePool, err := pgxpool.NewWithConfig(ctx, runtimePoolConfig)
		if err != nil {
			t.Fatalf("open SET ROLE runtime pool: %v", err)
		}
		t.Cleanup(runtimePool.Close)

		runtimeReader := NewPostgresAppACLManifestRuntimeReader(runtimePool)
		runtimeSnapshot, err := runtimeReader.ReadAppACLManifestRuntimeSnapshotV1(ctx)
		if err != nil {
			t.Fatalf("ReadAppACLManifestRuntimeSnapshotV1() SET ROLE error = %v", err)
		}
		if runtimeSnapshot.SessionUser != memberLogin || runtimeSnapshot.CurrentUser != runtimeRole {
			t.Fatalf("SET ROLE runtime identities = (%q, %q), want (%q, %q)", runtimeSnapshot.SessionUser, runtimeSnapshot.CurrentUser, memberLogin, runtimeRole)
		}
		if _, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, runtimeReader, migrations.FS); err == nil || err.Error() != fmt.Sprintf("session user %q does not match current user %q", memberLogin, runtimeRole) {
			t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() SET ROLE error = %v, want exact member-session identity rejection", err)
		}
	})
}

func TestPostgresIntegrationAppACLManifestRevisionSQLCheckBindsMigratorCatalogRole(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	migrationBody, err := CanonicalMigrationSetFromFS(migrations.FS)
	if err != nil {
		t.Fatalf("CanonicalMigrationSetFromFS() error = %v", err)
	}
	privilegeBody, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	manifest, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}

	_, err = db.Exec(ctx, `
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
		int64(manifest.ManifestRevision),
		"houfeng_tampered_migrator",
		manifest.PreviousManifestDigest[:],
		manifest.CanonicalMigrationSet,
		manifest.MigrationSetDigest[:],
		manifest.CanonicalPrivilegeSet,
		manifest.PrivilegeSetDigest[:],
		manifest.ManifestDigest[:],
	)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "app_acl_manifest_digest_matches" {
		t.Fatalf("tampered migrator catalog role insert error = %v, want app_acl_manifest_digest_matches SQLSTATE 23514", err)
	}
}

func TestPostgresIntegrationEnsureAppACLManifestGenesisV1(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	var databaseName, sessionUser, currentUser string
	if err := db.QueryRow(ctx, `select current_database(), session_user, current_user`).Scan(&databaseName, &sessionUser, &currentUser); err != nil {
		t.Fatalf("read direct runtime database and identity: %v", err)
	}
	if sessionUser != currentUser {
		t.Fatalf("genesis fixture session user %q does not match current user %q", sessionUser, currentUser)
	}
	privilegeBody, err := CompileAppACLPrivilegeSetR1(databaseName,
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: currentUser},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
	)
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() error = %v", err)
	}

	first, err := EnsureAppACLManifestGenesisV1(ctx, db, migrations.FS, privilegeBody, "houfeng_migrator")
	if err != nil {
		t.Fatalf("EnsureAppACLManifestGenesisV1() first error = %v", err)
	}
	if first.ManifestRevision != 1 || first.PreviousManifestDigest != [32]byte{} {
		t.Fatalf("first manifest = %#v, want genesis revision", first)
	}
	var persistedRevision int64
	var persistedMigratorCatalogRole string
	var persistedPreviousDigest, persistedMigrationSet, persistedMigrationSetDigest, persistedPrivilegeSet, persistedPrivilegeSetDigest, persistedManifestDigest []byte
	if err := db.QueryRow(ctx, `
		select manifest_revision,
		       migrator_catalog_role,
		       previous_manifest_digest,
		       canonical_migration_set,
		       sorted_migration_set_digest,
		       canonical_privilege_set,
		       privilege_set_digest,
		       manifest_digest
		from public.app_acl_manifest_revisions
		where manifest_revision = 1
	`).Scan(
		&persistedRevision,
		&persistedMigratorCatalogRole,
		&persistedPreviousDigest,
		&persistedMigrationSet,
		&persistedMigrationSetDigest,
		&persistedPrivilegeSet,
		&persistedPrivilegeSetDigest,
		&persistedManifestDigest,
	); err != nil {
		t.Fatalf("read all persisted genesis manifest fields: %v", err)
	}
	if persistedRevision != int64(first.ManifestRevision) ||
		persistedMigratorCatalogRole != first.MigratorCatalogRole ||
		!bytes.Equal(persistedPreviousDigest, first.PreviousManifestDigest[:]) ||
		!bytes.Equal(persistedMigrationSet, first.CanonicalMigrationSet) ||
		!bytes.Equal(persistedMigrationSetDigest, first.MigrationSetDigest[:]) ||
		!bytes.Equal(persistedPrivilegeSet, first.CanonicalPrivilegeSet) ||
		!bytes.Equal(persistedPrivilegeSetDigest, first.PrivilegeSetDigest[:]) ||
		!bytes.Equal(persistedManifestDigest, first.ManifestDigest[:]) {
		t.Fatal("persisted genesis manifest fields do not exactly match the eight-field canonical manifest")
	}
	var beforeUpdatedAt time.Time
	var headRevision int64
	var headDigest []byte
	if err := db.QueryRow(ctx, `
		select manifest_revision, manifest_digest, updated_at
		from public.app_acl_manifest_head
		where singleton
	`).Scan(&headRevision, &headDigest, &beforeUpdatedAt); err != nil {
		t.Fatalf("read genesis manifest head: %v", err)
	}
	if headRevision != 1 || !bytes.Equal(headDigest, first.ManifestDigest[:]) {
		t.Fatalf("genesis head = (%d, %x), want (1, %x)", headRevision, headDigest, first.ManifestDigest)
	}
	if _, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, NewPostgresAppACLManifestRuntimeReader(db), migrations.FS); err != nil {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() after genesis error = %v", err)
	}

	second, err := EnsureAppACLManifestGenesisV1(ctx, db, migrations.FS, privilegeBody, "houfeng_migrator")
	if err != nil {
		t.Fatalf("EnsureAppACLManifestGenesisV1() repeat error = %v", err)
	}
	if second.ManifestDigest != first.ManifestDigest {
		t.Fatalf("repeat manifest digest = %x, want %x", second.ManifestDigest, first.ManifestDigest)
	}
	var afterUpdatedAt time.Time
	if err := db.QueryRow(ctx, `select updated_at from public.app_acl_manifest_head where singleton`).Scan(&afterUpdatedAt); err != nil {
		t.Fatalf("read repeated manifest head timestamp: %v", err)
	}
	if !afterUpdatedAt.Equal(beforeUpdatedAt) {
		t.Fatalf("repeat manifest head updated_at = %s, want %s", afterUpdatedAt, beforeUpdatedAt)
	}

	driftingPrivilegeBody, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
		[]AppACLPrivilege{{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassDatabase,
			ObjectIdentity: "houfeng",
			Privilege:      AppACLPrivilegeConnect,
		}},
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() drifting error = %v", err)
	}
	if _, err := EnsureAppACLManifestGenesisV1(ctx, db, migrations.FS, driftingPrivilegeBody, "houfeng_migrator"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("EnsureAppACLManifestGenesisV1() drifting privilege error = %v, want rejection", err)
	}
	assertSingleIntValue(t, ctx, db, `select count(*)::int from public.app_acl_manifest_revisions`, 1)
}

func TestPostgresIntegrationEnsureAppACLManifestGenesisV1RejectsLedgerAndAdvancedChain(t *testing.T) {
	ctx := context.Background()
	privilegeBody, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() error = %v", err)
	}

	t.Run("rejects applied ledger drift before genesis", func(t *testing.T) {
		db := openTemporaryPostgresDatabase(t, ctx)
		if err := Apply(ctx, db); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if _, err := db.Exec(ctx, `delete from public.schema_migrations where name = '0051_create_record_platform_foundation.sql'`); err != nil {
			t.Fatalf("delete applied migration to simulate drift: %v", err)
		}

		if _, err := EnsureAppACLManifestGenesisV1(ctx, db, migrations.FS, privilegeBody, "houfeng_migrator"); err == nil || !strings.Contains(err.Error(), "does not match embedded migrations") {
			t.Fatalf("EnsureAppACLManifestGenesisV1() ledger drift error = %v, want embedded-map rejection", err)
		}
		assertSingleIntValue(t, ctx, db, `select count(*)::int from public.app_acl_manifest_revisions`, 0)
	})

	t.Run("rejects an already advanced manifest chain", func(t *testing.T) {
		db := openTemporaryPostgresDatabase(t, ctx)
		if err := Apply(ctx, db); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		first, err := EnsureAppACLManifestGenesisV1(ctx, db, migrations.FS, privilegeBody, "houfeng_migrator")
		if err != nil {
			t.Fatalf("EnsureAppACLManifestGenesisV1() first error = %v", err)
		}
		advanced, err := NewAppACLManifestPersistedV1(
			2,
			first.MigratorCatalogRole,
			first.ManifestDigest,
			first.CanonicalMigrationSet,
			first.CanonicalPrivilegeSet,
		)
		if err != nil {
			t.Fatalf("NewAppACLManifestPersistedV1() advanced error = %v", err)
		}
		if _, err := db.Exec(ctx, `
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
			int64(advanced.ManifestRevision),
			advanced.MigratorCatalogRole,
			advanced.PreviousManifestDigest[:],
			advanced.CanonicalMigrationSet,
			advanced.MigrationSetDigest[:],
			advanced.CanonicalPrivilegeSet,
			advanced.PrivilegeSetDigest[:],
			advanced.ManifestDigest[:],
		); err != nil {
			t.Fatalf("insert advanced app ACL manifest revision: %v", err)
		}
		if _, err := db.Exec(ctx, `
			update public.app_acl_manifest_head
			set manifest_revision = $1, manifest_digest = $2
			where singleton
		`, int64(advanced.ManifestRevision), advanced.ManifestDigest[:]); err != nil {
			t.Fatalf("advance app ACL manifest head: %v", err)
		}

		if _, err := EnsureAppACLManifestGenesisV1(ctx, db, migrations.FS, privilegeBody, "houfeng_migrator"); err == nil || !strings.Contains(err.Error(), "already advanced") {
			t.Fatalf("EnsureAppACLManifestGenesisV1() advanced-chain error = %v, want rejection", err)
		}
		assertSingleIntValue(t, ctx, db, `select count(*)::int from public.app_acl_manifest_revisions`, 2)
	})
}

func TestPostgresIntegrationAppliesFreshMigrations(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)

	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() on fresh postgres error = %v", err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() second run on fresh postgres error = %v", err)
	}

	assertSingleStringValue(t, ctx, db, "select to_regclass('public.monitoring_instances')::text", "monitoring_instances")
	assertSingleStringValue(t, ctx, db, "select to_regclass('public.vps_monitoring_instance_links')::text", "vps_monitoring_instance_links")
	assertSingleIntValue(t, ctx, db, "select count(*)::int from schema_migrations where name = '0030_vps_first_status_semantics.sql'", 1)
	assertSingleIntValue(t, ctx, db, "select count(*)::int from schema_migrations where name = '0050_extend_command_action_audit.sql'", 1)
	assertSingleIntValue(t, ctx, db, "select count(*)::int from schema_migrations where name = '0052_create_records_core.sql'", 1)
	for _, indexName := range []string{
		"idx_monitoring_instance_command_action_audit_instance_time",
		"idx_monitoring_instance_command_action_audit_action_time",
		"idx_monitoring_instance_command_action_audit_global_time",
	} {
		assertSingleStringValue(t, ctx, db, "select to_regclass('public."+indexName+"')::text", indexName)
	}
}

func TestPostgresIntegrationLegacyApplyKeepsLedgerAndDDLInAmbientSearchPath(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)

	schemaName := fmt.Sprintf("legacy_apply_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !isSafePostgresIdentifier(schemaName) {
		t.Fatalf("unsafe generated legacy apply schema name %q", schemaName)
	}
	execSQL(t, ctx, db, `create schema `+quotePostgresIdentifier(schemaName))

	legacyConfig := db.Config().Copy()
	if legacyConfig.ConnConfig.RuntimeParams == nil {
		legacyConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	legacyConfig.ConnConfig.RuntimeParams["search_path"] = `"$user", ` + quotePostgresIdentifier(schemaName)
	legacyConfig.MaxConns = 1
	legacyDB, err := pgxpool.NewWithConfig(ctx, legacyConfig)
	if err != nil {
		t.Fatalf("open legacy migration pool: %v", err)
	}
	t.Cleanup(legacyDB.Close)

	var currentSchema string
	if err := legacyDB.QueryRow(ctx, `select current_schema()`).Scan(&currentSchema); err != nil {
		t.Fatalf("read legacy migration current schema: %v", err)
	}
	if currentSchema != schemaName {
		t.Fatalf("legacy migration current schema = %q, want custom ambient schema %q", currentSchema, schemaName)
	}

	files := fstest.MapFS{
		"0001_legacy_ledger.sql": {Data: []byte(`select 1;`)},
		"0002_legacy_search_path_probe.sql": {Data: []byte(`
			create table legacy_apply_search_path_probe (
				id integer primary key
			)
		`)},
	}
	sources, err := migrationSources(files)
	if err != nil {
		t.Fatalf("migrationSources() error = %v", err)
	}

	// This is the historical pre-checksum ledger shape. The legacy runner must
	// adopt it in the same ambient schema as its unqualified migration DDL.
	execSQL(t, ctx, legacyDB, `
		create table schema_migrations (
			name text primary key,
			applied_at timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, legacyDB, `insert into schema_migrations (name) values ('0001_legacy_ledger.sql')`)

	if err := applyFS(ctx, poolStore{db: legacyDB}, files); err != nil {
		t.Fatalf("legacy applyFS() error = %v", err)
	}

	quotedSchema := quotePostgresIdentifier(schemaName)
	var legacyLedgerExists, migrationObjectExists, publicLedgerExists bool
	if err := legacyDB.QueryRow(ctx, `
		select pg_catalog.to_regclass($1) is not null,
		       pg_catalog.to_regclass($2) is not null,
		       pg_catalog.to_regclass('public.schema_migrations') is not null
	`, schemaName+`.schema_migrations`, schemaName+`.legacy_apply_search_path_probe`).Scan(
		&legacyLedgerExists,
		&migrationObjectExists,
		&publicLedgerExists,
	); err != nil {
		t.Fatalf("read legacy apply relation locations: %v", err)
	}
	if !legacyLedgerExists || !migrationObjectExists || publicLedgerExists {
		t.Fatalf("legacy apply relation locations = ledger:%t migration:%t public-ledger:%t, want true:true:false", legacyLedgerExists, migrationObjectExists, publicLedgerExists)
	}

	var backfilledChecksum, recordedChecksum string
	if err := legacyDB.QueryRow(ctx, `
		select (select checksum from `+quotedSchema+`.schema_migrations where name = '0001_legacy_ledger.sql'),
		       (select checksum from `+quotedSchema+`.schema_migrations where name = '0002_legacy_search_path_probe.sql')
	`).Scan(&backfilledChecksum, &recordedChecksum); err != nil {
		t.Fatalf("read legacy apply checksums: %v", err)
	}
	if backfilledChecksum != sources["0001_legacy_ledger.sql"].checksum || recordedChecksum != sources["0002_legacy_search_path_probe.sql"].checksum {
		t.Fatalf("legacy apply checksums = (%q, %q), want (%q, %q)", backfilledChecksum, recordedChecksum, sources["0001_legacy_ledger.sql"].checksum, sources["0002_legacy_search_path_probe.sql"].checksum)
	}
}

func TestPostgresIntegrationRecordPlatformPgcryptoInstallsWithConstrainedDirectMigrator(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	databaseName := currentPostgresDatabaseName(t, ctx, db)

	var bootstrapOwner string
	if err := db.QueryRow(ctx, `
		select pg_catalog.pg_get_userbyid(database.datdba)
		from pg_catalog.pg_database database
		where database.datname = $1
	`, databaseName).Scan(&bootstrapOwner); err != nil {
		t.Fatalf("read temporary database owner: %v", err)
	}
	migratorRole := fmt.Sprintf("houfeng_direct_migrator_%d_%d", time.Now().UnixNano(), os.Getpid())
	migratorPassword := appACLEffectiveCatalogTemporaryPassword(t)
	quotedMigrator := quotePostgresIdentifier(migratorRole)
	quotedBootstrapOwner := quotePostgresIdentifier(bootstrapOwner)
	if _, err := db.Exec(ctx, `create role `+quotedMigrator+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+migratorPassword+`'`); err != nil {
		t.Fatalf("create constrained direct migrator %q: %v", migratorRole, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.Exec(cleanupCtx, `reassign owned by `+quotedMigrator+` to `+quotedBootstrapOwner); err != nil {
			t.Errorf("reassign constrained direct migrator %q ownership: %v", migratorRole, err)
		}
		if _, err := db.Exec(cleanupCtx, `drop owned by `+quotedMigrator); err != nil {
			t.Errorf("drop constrained direct migrator %q dependencies: %v", migratorRole, err)
		}
		if _, err := db.Exec(cleanupCtx, `drop role if exists `+quotedMigrator); err != nil {
			t.Errorf("drop constrained direct migrator %q: %v", migratorRole, err)
		}
	})
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.Exec(cleanupCtx, `alter database `+quotePostgresIdentifier(databaseName)+` owner to `+quotedBootstrapOwner); err != nil {
			t.Errorf("restore temporary database owner %q: %v", bootstrapOwner, err)
		}
	})
	if _, err := db.Exec(ctx, `alter database `+quotePostgresIdentifier(databaseName)+` owner to `+quotedMigrator); err != nil {
		t.Fatalf("assign temporary database to constrained direct migrator: %v", err)
	}

	migratorFixture := appACLEffectiveCatalogPostgresFixture{
		db:            db,
		rolePasswords: map[string]string{migratorRole: migratorPassword},
	}
	migratorDB := migratorFixture.openDirectRolePool(t, ctx, migratorRole)
	if err := Apply(ctx, migratorDB); err != nil {
		t.Fatalf("Apply() with constrained direct migrator error = %v", err)
	}

	var extensionSchema string
	if err := db.QueryRow(ctx, `
		select namespace.nspname
		from pg_catalog.pg_extension installed_extension
		join pg_catalog.pg_namespace namespace on namespace.oid = installed_extension.extnamespace
		where installed_extension.extname = 'pgcrypto'
	`).Scan(&extensionSchema); err != nil {
		t.Fatalf("read pgcrypto extension after constrained direct migrator apply: %v", err)
	}
	if extensionSchema != appACLManagedInternalSchemaR1 {
		t.Fatalf("pgcrypto extension schema after constrained direct migrator apply = %q, want %q", extensionSchema, appACLManagedInternalSchemaR1)
	}
}

func TestPostgresIntegrationRecordPlatformPgcryptoWrongSchemaRejectsApplyWithoutRecording0051(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	if _, err := db.Exec(ctx, `create extension pgcrypto with schema public`); err != nil {
		t.Fatalf("preinstall pgcrypto in public: %v", err)
	}

	err := Apply(ctx, db)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" || pgErr.Message != "pgcrypto must be installed in record_platform_internal" {
		t.Fatalf("Apply() with pgcrypto in public error = %v, want 0051 SQLSTATE 55000 pgcrypto schema rejection", err)
	}
	if !strings.Contains(err.Error(), "apply migration 0051_create_record_platform_foundation.sql") {
		t.Fatalf("Apply() with pgcrypto in public error = %v, want 0051 migration context", err)
	}
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from public.schema_migrations
		where name = '0051_create_record_platform_foundation.sql'
	`, 0)
}

func TestPostgresIntegrationRecordPlatformFoundationSchema(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)

	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() record-platform foundation error = %v", err)
	}

	for _, table := range []string{
		"record_access_groups",
		"record_access_group_members",
		"record_outbox",
		"record_idempotency_keys",
		"identity_mutation_guards",
		"deletion_reservations",
		"record_purge_operations",
		"record_deletion_audits",
		"deletion_fence_leases",
		"object_content_leases",
		"client_content_leases",
		"content_delivery_epochs",
		"backup_epochs",
		"recovery_inventory_projection",
		"deletion_replay_state",
		"deployment_membership",
		"source_deletion_tombstones",
		"deployment_contract_state",
		"record_platform_domain_identity",
		"record_platform_domain_attestations",
		"app_acl_manifest_revisions",
		"app_acl_manifest_head",
	} {
		assertSingleStringValue(t, ctx, db, "select to_regclass('public."+table+"')::text", table)
	}
	assertSingleIntValue(t, ctx, db,
		"select count(*)::int from schema_migrations where name = '0051_create_record_platform_foundation.sql'", 1)

	var extensionSchema string
	if err := db.QueryRow(ctx, `
		select n.nspname
		from pg_extension e
		join pg_namespace n on n.oid = e.extnamespace
		where e.extname = 'pgcrypto'
	`).Scan(&extensionSchema); err != nil {
		t.Fatalf("query pgcrypto extension schema: %v", err)
	}
	if extensionSchema != "record_platform_internal" {
		t.Fatalf("pgcrypto extension schema = %q, want record_platform_internal", extensionSchema)
	}

	for _, column := range []string{
		"activation_sequence",
		"activation_mutation_id",
		"activation_plan_digest",
		"activation_authorization_artifact_digest",
		"activation_bundle_digest",
		"last_domain_identity_sequence",
		"last_domain_identity_entry_hash",
	} {
		var found int
		if err := db.QueryRow(ctx, `
			select count(*)::int
			from information_schema.columns
			where table_schema = 'public'
				and table_name = 'deployment_contract_state'
				and column_name = $1
		`, column).Scan(&found); err != nil {
			t.Fatalf("query deployment contract state column %q: %v", column, err)
		}
		if found != 1 {
			t.Fatalf("deployment contract state column %q count = %d, want 1", column, found)
		}
	}

	_, err := db.Exec(ctx, "delete from public.record_platform_domain_identity")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("delete immutable domain identity error = %v, want SQLSTATE 55000", err)
	}
}

func TestPostgresIntegrationRecordsCoreSchema(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)

	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() records-core migration error = %v", err)
	}
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() records-core exact repeat error = %v", err)
	}

	for _, table := range []string{
		"records",
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_drafts",
		"record_draft_checkpoints",
		"record_domain_activities",
		"record_core_purge_receipts",
	} {
		assertSingleStringValue(t, ctx, db, "select to_regclass('public."+table+"')::text", table)
	}
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from public.schema_migrations
		where name = '0052_create_records_core.sql'
	`, 1)
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_catalog.pg_constraint
		where conname = 'records_current_revision_same_record_fk'
		  and conrelid = 'public.records'::regclass
		  and condeferrable
		  and condeferred
		  and confdeltype = 'r'
	`, 1)
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_catalog.pg_indexes
		where schemaname = 'public'
		  and indexname = 'uq_record_revision_subjects_primary'
		  and lower(indexdef) like '%unique%'
		  and lower(indexdef) like '%where is_primary%'
	`, 1)
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_catalog.pg_constraint
		where contype = 'f'
		  and connamespace = 'public'::regnamespace
		  and conrelid in (
		    'public.records'::regclass,
		    'public.record_revisions'::regclass,
		    'public.record_revision_subjects'::regclass,
		    'public.record_revision_tags'::regclass,
		    'public.record_revision_participants'::regclass,
		    'public.record_drafts'::regclass,
		    'public.record_draft_checkpoints'::regclass,
		    'public.record_domain_activities'::regclass,
		    'public.record_core_purge_receipts'::regclass
		  )
		  and confdeltype <> 'r'
	`, 0)

	for _, table := range []string{
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_draft_checkpoints",
		"record_domain_activities",
		"record_core_purge_receipts",
	} {
		var triggerCount int
		if err := db.QueryRow(ctx, `
			select count(*)::int
			from pg_catalog.pg_trigger trigger_catalog
			join pg_catalog.pg_class relation on relation.oid = trigger_catalog.tgrelid
			join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
			join pg_catalog.pg_proc procedure on procedure.oid = trigger_catalog.tgfoid
			join pg_catalog.pg_namespace procedure_namespace on procedure_namespace.oid = procedure.pronamespace
			where namespace.nspname = 'public'
			  and relation.relname = $1
			  and trigger_catalog.tgname = $1 || '_reject_update'
			  and not trigger_catalog.tgisinternal
			  and procedure_namespace.nspname = 'record_platform_internal'
			  and procedure.proname = 'reject_immutable_mutation'
		`, table).Scan(&triggerCount); err != nil {
			t.Fatalf("query immutable trigger for %q: %v", table, err)
		}
		if triggerCount != 1 {
			t.Fatalf("immutable trigger count for %q = %d, want 1", table, triggerCount)
		}
	}

	execSQL(t, ctx, db, `
		insert into public.records (record_id) values ('rec_a'), ('rec_b'), ('rec_missingprimary')
	`)

	missingPrimaryTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin missing-primary revision transaction: %v", err)
	}
	if _, err := missingPrimaryTx.Exec(ctx, `
		insert into public.record_revisions (
			revision_id, record_id, revision_no, title, body_markdown,
			markdown_dialect_version, record_type, impact_level,
			visibility_scope, visibility_digest, author_id, canonical_hash
		) values (
			'rrv_missingprimary', 'rec_missingprimary', 1, 'Missing', '# Missing',
			1, 'note', 'informational', '{}'::jsonb,
			decode(repeat('10', 32), 'hex'), 'usr_author', decode(repeat('20', 32), 'hex')
		)
	`); err != nil {
		_ = missingPrimaryTx.Rollback(ctx)
		t.Fatalf("insert missing-primary revision before deferred check: %v", err)
	}
	err = missingPrimaryTx.Commit(ctx)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("missing-primary revision commit error = %v, want SQLSTATE 23514", err)
	}

	revisionTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin complete revision transaction: %v", err)
	}
	defer func() { _ = revisionTx.Rollback(ctx) }()
	if _, err := revisionTx.Exec(ctx, `
		insert into public.record_revisions (
			revision_id, record_id, revision_no, title, body_markdown,
			markdown_dialect_version, record_type, impact_level,
			visibility_scope, visibility_digest, author_id, canonical_hash
		) values
			('rrv_a', 'rec_a', 1, 'A', '# A', 1, 'note', 'informational',
			 '{}'::jsonb, decode(repeat('11', 32), 'hex'), 'usr_author', decode(repeat('21', 32), 'hex')),
			('rrv_b', 'rec_b', 1, 'B', '# B', 1, 'note', 'informational',
			 '{}'::jsonb, decode(repeat('12', 32), 'hex'), 'usr_author', decode(repeat('22', 32), 'hex'))
	`); err != nil {
		t.Fatalf("insert complete revisions: %v", err)
	}
	if _, err := revisionTx.Exec(ctx, `
		insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values
			('rrv_a', 0, 1, 'vps', 'affected', 'vps_a', true,
			 '{}'::jsonb, '{}'::jsonb, decode(repeat('31', 32), 'hex')),
			('rrv_b', 0, 1, 'vps', 'affected', 'vps_b', true,
			 '{}'::jsonb, '{}'::jsonb, decode(repeat('33', 32), 'hex'))
	`); err != nil {
		t.Fatalf("insert complete revision primary subjects: %v", err)
	}
	if err := revisionTx.Commit(ctx); err != nil {
		t.Fatalf("commit complete revisions: %v", err)
	}
	execSQL(t, ctx, db, `
		update public.records
		set current_revision_id = 'rrv_a',
		    current_title = 'A',
		    current_record_type = 'note',
		    current_impact_level = 'informational',
		    current_visibility_scope = '{}'::jsonb,
		    current_visibility_digest = decode(repeat('11', 32), 'hex'),
		    lock_version = 1
		where record_id = 'rec_a'
	`)

	_, err = db.Exec(ctx, `
		update public.records
		set current_revision_id = 'rrv_b'
		where record_id = 'rec_a'
	`)
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" || pgErr.ConstraintName != "records_current_revision_same_record_fk" {
		t.Fatalf("cross-record current revision update error = %v, want same-record foreign-key rejection", err)
	}

	_, err = db.Exec(ctx, `
		insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values (
			'rrv_a', 1, 1, 'target', 'context', 'target_a', true,
			'{}'::jsonb, '{}'::jsonb, decode(repeat('32', 32), 'hex')
		)
	`)
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" || pgErr.ConstraintName != "uq_record_revision_subjects_primary" {
		t.Fatalf("second primary subject insert error = %v, want partial-unique rejection", err)
	}

	_, err = db.Exec(ctx, `update public.record_revisions set title = 'mutated' where revision_id = 'rrv_a'`)
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" {
		t.Fatalf("immutable revision update error = %v, want SQLSTATE 55000", err)
	}

	_, err = db.Exec(ctx, `delete from public.records where record_id = 'rec_b'`)
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("record delete with owned revision error = %v, want explicit-cleanup foreign-key rejection", err)
	}
	purgeTx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin explicit records-core purge transaction: %v", err)
	}
	defer func() { _ = purgeTx.Rollback(ctx) }()
	for _, statement := range []string{
		`delete from public.record_revision_subjects where revision_id = 'rrv_b'`,
		`delete from public.record_revisions where revision_id = 'rrv_b'`,
		`delete from public.records where record_id = 'rec_b'`,
	} {
		if _, err := purgeTx.Exec(ctx, statement); err != nil {
			t.Fatalf("execute explicit records-core purge statement %q: %v", statement, err)
		}
	}
	if err := purgeTx.Commit(ctx); err != nil {
		t.Fatalf("commit explicit records-core purge transaction: %v", err)
	}
}

func TestPostgresIntegrationRecordPlatformProjectorFunctions(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	roles := createProjectorCASTestRoles(t, ctx, db)
	migratorDB := openPostgresPoolWithRole(t, ctx, db, roles.migrator)
	if err := Apply(ctx, migratorDB); err != nil {
		t.Fatalf("Apply() as migrator error = %v", err)
	}

	databaseName := currentPostgresDatabaseName(t, ctx, db)
	runtimeDB := openPostgresPoolWithRole(t, ctx, db, roles.runtime)
	adminDB := openPostgresPoolWithRole(t, ctx, db, roles.admin)
	projectorDB := migratorDB
	configureProjectorCASPrivileges(t, ctx, db, databaseName, roles)
	assertProjectorCASFunctionCatalog(t, ctx, db, roles.migrator)

	activation := projectorCASActivationCommand()
	activationBytes, err := activation.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal activation command: %v", err)
	}

	if _, err := runtimeDB.Exec(ctx, `
		update public.deployment_contract_state
		set minimum_fence_contract_version = 1
		where project_id = 'default'
	`); !isPostgresInsufficientPrivilege(err) {
		t.Fatalf("runtime direct deployment_contract_state update error = %v, want SQLSTATE 42501", err)
	}
	if _, err := adminDB.Exec(ctx, `select public.record_platform_cas_contract_activation_projection($1)`, activationBytes); !isPostgresInsufficientPrivilege(err) {
		t.Fatalf("admin activation invoke error = %v, want SQLSTATE 42501", err)
	}
	if _, err := runtimeDB.Exec(ctx, `select public.record_platform_cas_contract_activation_projection($1)`, activationBytes); !isPostgresInsufficientPrivilege(err) {
		t.Fatalf("runtime activation invoke error = %v, want SQLSTATE 42501", err)
	}

	malformed := append([]byte(nil), activationBytes...)
	malformed[0] ^= 0xff
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "activation malformed command", "public.record_platform_cas_contract_activation_projection", malformed)
	invalidDeployment := append([]byte(nil), activationBytes...)
	invalidDeployment[37+3] = 'A'
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "activation invalid deployment token", "public.record_platform_cas_contract_activation_projection", invalidDeployment)
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "activation trailing command", "public.record_platform_cas_contract_activation_projection", append(append([]byte(nil), activationBytes...), 0))

	activationReceipt := invokeProjectorCASFunction(t, ctx, projectorDB, "public.record_platform_cas_contract_activation_projection", activationBytes)
	assertProjectorCASReceipt(t, activationBytes, activationReceipt)
	activationRetry := invokeProjectorCASFunction(t, ctx, projectorDB, "public.record_platform_cas_contract_activation_projection", activationBytes)
	if !bytes.Equal(activationReceipt, activationRetry) {
		t.Fatalf("activation retry receipt = %x, want %x", activationRetry, activationReceipt)
	}

	differentActivation := activation
	differentActivation.PlanDigest[0] ^= 0xff
	differentActivationBytes, err := differentActivation.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal different activation command: %v", err)
	}
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "different active activation", "public.record_platform_cas_contract_activation_projection", differentActivationBytes)

	rotation := projectorCASRotationFromActivation(activation)
	rotationBytes, err := rotation.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal rotation command: %v", err)
	}
	if _, err := adminDB.Exec(ctx, `select public.record_platform_cas_domain_rotation_projection($1)`, rotationBytes); !isPostgresInsufficientPrivilege(err) {
		t.Fatalf("admin rotation invoke error = %v, want SQLSTATE 42501", err)
	}
	if _, err := runtimeDB.Exec(ctx, `select public.record_platform_cas_domain_rotation_projection($1)`, rotationBytes); !isPostgresInsufficientPrivilege(err) {
		t.Fatalf("runtime rotation invoke error = %v, want SQLSTATE 42501", err)
	}

	malformedRotation := append([]byte(nil), rotationBytes...)
	malformedRotation[0] ^= 0xff
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "rotation malformed command", "public.record_platform_cas_domain_rotation_projection", malformedRotation)
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "rotation trailing command", "public.record_platform_cas_domain_rotation_projection", append(append([]byte(nil), rotationBytes...), 0))

	profileMismatch := rotation
	profileMismatch.ActiveProfile = recordplatform.ProjectionProfileS3WORM
	profileMismatchBytes, err := profileMismatch.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal profile-mismatch rotation: %v", err)
	}
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "profile mismatch", "public.record_platform_cas_domain_rotation_projection", profileMismatchBytes)

	staleState := append([]byte(nil), rotationBytes...)
	staleState[180] ^= 0xff
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "stale witnessed hash", "public.record_platform_cas_domain_rotation_projection", staleState)

	lowerFence := append([]byte(nil), rotationBytes...)
	binary.BigEndian.PutUint64(lowerFence[460:468], rotation.ExpectedMinimumFenceContractVersion-1)
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "lower fence", "public.record_platform_cas_domain_rotation_projection", lowerFence)

	rotationReceipt := invokeProjectorCASFunction(t, ctx, projectorDB, "public.record_platform_cas_domain_rotation_projection", rotationBytes)
	assertProjectorCASReceipt(t, rotationBytes, rotationReceipt)
	rotationRetry := invokeProjectorCASFunction(t, ctx, projectorDB, "public.record_platform_cas_domain_rotation_projection", rotationBytes)
	if !bytes.Equal(rotationReceipt, rotationRetry) {
		t.Fatalf("rotation retry receipt = %x, want %x", rotationRetry, rotationReceipt)
	}

	nextRotation := projectorCASNextRotation(rotation, 15, 16)
	staleSequence := nextRotation
	staleSequence.ExpectedWitnessedLedgerSequence = rotation.ExpectedWitnessedLedgerSequence
	staleSequenceBytes, err := staleSequence.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal stale expected witnessed ledger sequence rotation: %v", err)
	}
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "stale expected witnessed ledger sequence", "public.record_platform_cas_domain_rotation_projection", staleSequenceBytes)

	staleIdentityEpoch := nextRotation
	staleIdentityEpoch.ExpectedIdentitySetEpoch = rotation.ExpectedIdentitySetEpoch
	staleIdentityEpoch.ExpectedIdentitySetDigest = rotation.ExpectedIdentitySetDigest
	staleIdentityEpoch.NextIdentitySetEpoch = rotation.NextIdentitySetEpoch
	staleIdentityEpoch.NextIdentitySetDigest = rotation.NextIdentitySetDigest
	staleIdentityEpochBytes, err := staleIdentityEpoch.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal stale expected identity-set epoch rotation: %v", err)
	}
	assertProjectorCASInvocationFails(t, ctx, projectorDB, "stale expected identity-set epoch", "public.record_platform_cas_domain_rotation_projection", staleIdentityEpochBytes)

	assertProjectorCASRotationState(t, ctx, db, rotation)
	assertProjectorCASConcurrentContenders(t, ctx, db, projectorDB, rotation)
}

type projectorCASTestRoles struct {
	migrator string
	runtime  string
	admin    string
	grantor  string
}

func createProjectorCASTestRoles(t *testing.T, ctx context.Context, db *pgxpool.Pool) projectorCASTestRoles {
	t.Helper()

	var grantor string
	if err := db.QueryRow(ctx, `select current_user`).Scan(&grantor); err != nil {
		t.Fatalf("read role grantor: %v", err)
	}
	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), os.Getpid())
	roles := projectorCASTestRoles{
		migrator: "houfeng_projector_migrator_" + suffix,
		runtime:  "houfeng_projector_runtime_" + suffix,
		admin:    "houfeng_projector_admin_" + suffix,
		grantor:  grantor,
	}

	execSQL(t, ctx, db, `create role `+quotePostgresIdentifier(roles.migrator)+` nologin noinherit superuser`)
	execSQL(t, ctx, db, `create role `+quotePostgresIdentifier(roles.runtime)+` nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`)
	execSQL(t, ctx, db, `create role `+quotePostgresIdentifier(roles.admin)+` nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls`)
	for _, role := range []string{roles.migrator, roles.runtime, roles.admin} {
		execSQL(t, ctx, db, `grant `+quotePostgresIdentifier(role)+` to `+quotePostgresIdentifier(grantor))
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, role := range []string{roles.migrator, roles.runtime, roles.admin} {
			quotedRole := quotePostgresIdentifier(role)
			quotedGrantor := quotePostgresIdentifier(grantor)
			if _, err := db.Exec(cleanupCtx, `reassign owned by `+quotedRole+` to `+quotedGrantor); err != nil {
				t.Errorf("reassign temporary projector role %q ownership: %v", role, err)
			}
			if _, err := db.Exec(cleanupCtx, `drop owned by `+quotedRole); err != nil {
				t.Errorf("drop temporary projector role %q ownership: %v", role, err)
			}
			if _, err := db.Exec(cleanupCtx, `revoke `+quotedRole+` from `+quotedGrantor); err != nil {
				t.Errorf("revoke temporary projector role %q: %v", role, err)
			}
			if _, err := db.Exec(cleanupCtx, `drop role `+quotedRole); err != nil {
				t.Errorf("drop temporary projector role %q: %v", role, err)
			}
		}
	})
	return roles
}

func openPostgresPoolWithRole(t *testing.T, ctx context.Context, base *pgxpool.Pool, role string) *pgxpool.Pool {
	t.Helper()

	config := base.Config().Copy()
	config.MaxConns = 4
	quotedRole := quotePostgresIdentifier(role)
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `set role `+quotedRole)
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open pool as role %q: %v", role, err)
	}
	t.Cleanup(pool.Close)

	var currentUser string
	if err := pool.QueryRow(ctx, `select current_user`).Scan(&currentUser); err != nil {
		t.Fatalf("read current user as %q: %v", role, err)
	}
	if currentUser != role {
		t.Fatalf("current user = %q, want role %q", currentUser, role)
	}
	return pool
}

func currentPostgresDatabaseName(t *testing.T, ctx context.Context, db *pgxpool.Pool) string {
	t.Helper()

	var databaseName string
	if err := db.QueryRow(ctx, `select current_database()`).Scan(&databaseName); err != nil {
		t.Fatalf("read current database name: %v", err)
	}
	if !isSafePostgresIdentifier(databaseName) {
		t.Fatalf("unsafe current database name %q", databaseName)
	}
	return databaseName
}

func configureProjectorCASPrivileges(t *testing.T, ctx context.Context, db *pgxpool.Pool, databaseName string, roles projectorCASTestRoles) {
	t.Helper()

	quotedDatabase := quotePostgresIdentifier(databaseName)
	quotedRuntime := quotePostgresIdentifier(roles.runtime)
	quotedAdmin := quotePostgresIdentifier(roles.admin)
	execSQL(t, ctx, db, `revoke all on database `+quotedDatabase+` from public`)
	execSQL(t, ctx, db, `grant connect on database `+quotedDatabase+` to `+quotedRuntime)
	execSQL(t, ctx, db, `grant connect on database `+quotedDatabase+` to `+quotedAdmin)
	execSQL(t, ctx, db, `revoke all on schema public from public`)
	execSQL(t, ctx, db, `grant usage on schema public to `+quotedRuntime)
	execSQL(t, ctx, db, `grant usage on schema public to `+quotedAdmin)
}

func assertProjectorCASFunctionCatalog(t *testing.T, ctx context.Context, db *pgxpool.Pool, migrator string) {
	t.Helper()

	for _, functionName := range []string{
		"record_platform_cas_contract_activation_projection",
		"record_platform_cas_domain_rotation_projection",
	} {
		var overloadCount int
		if err := db.QueryRow(ctx, `
			select count(*)::int
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			where namespace.nspname = 'public'
			  and procedure.proname = $1
		`, functionName).Scan(&overloadCount); err != nil {
			t.Fatalf("count %q overloads: %v", functionName, err)
		}
		if overloadCount != 1 {
			t.Fatalf("%q overload count = %d, want 1", functionName, overloadCount)
		}

		var identityArguments, owner, kind string
		var securityDefiner, publicExecute bool
		var config []string
		if err := db.QueryRow(ctx, `
			select pg_catalog.pg_get_function_identity_arguments(procedure.oid),
			       owner.rolname,
			       procedure.prokind::text,
			       procedure.prosecdef,
			       coalesce(procedure.proconfig, array[]::text[]),
			       exists (
			         select 1
			         from pg_catalog.aclexplode(
			           coalesce(procedure.proacl, pg_catalog.acldefault('f', procedure.proowner))
			         ) as acl_entry
			         where acl_entry.grantee = 0
			           and acl_entry.privilege_type = 'EXECUTE'
			       )
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			join pg_catalog.pg_roles owner on owner.oid = procedure.proowner
			where namespace.nspname = 'public'
			  and procedure.proname = $1
		`, functionName).Scan(&identityArguments, &owner, &kind, &securityDefiner, &config, &publicExecute); err != nil {
			t.Fatalf("read %q catalog row: %v", functionName, err)
		}
		if identityArguments != "bytea" || owner != migrator || kind != "f" || !securityDefiner {
			t.Fatalf("%q catalog = identity=%q owner=%q kind=%q security_definer=%t, want bytea/%q/f/true", functionName, identityArguments, owner, kind, securityDefiner, migrator)
		}
		if len(config) != 1 || config[0] != "search_path=pg_catalog" {
			t.Fatalf("%q proconfig = %#v, want [search_path=pg_catalog]", functionName, config)
		}
		if publicExecute {
			t.Fatalf("%q retains PUBLIC EXECUTE", functionName)
		}
	}
}

func invokeProjectorCASFunction(t *testing.T, ctx context.Context, db *pgxpool.Pool, functionIdentity string, command []byte) []byte {
	t.Helper()

	var receipt []byte
	if err := db.QueryRow(ctx, `select `+functionIdentity+`($1)`, command).Scan(&receipt); err != nil {
		t.Fatalf("invoke %s: %v", functionIdentity, err)
	}
	return receipt
}

func assertProjectorCASInvocationFails(t *testing.T, ctx context.Context, db *pgxpool.Pool, name, functionIdentity string, command []byte) {
	t.Helper()

	var receipt []byte
	if err := db.QueryRow(ctx, `select `+functionIdentity+`($1)`, command).Scan(&receipt); err == nil {
		t.Fatalf("%s unexpectedly returned receipt %x", name, receipt)
	}
}

func assertProjectorCASReceipt(t *testing.T, command, receipt []byte) {
	t.Helper()

	want := recordplatform.ProjectionCASReceiptDigestV1(command)
	if !bytes.Equal(receipt, want[:]) {
		t.Fatalf("receipt = %x, want %x", receipt, want)
	}
}

func projectorCASActivationCommand() recordplatform.ContractActivationProjectionCommandV1 {
	return recordplatform.ContractActivationProjectionCommandV1{
		DeploymentID:                "dp-" + strings.Repeat("a", 64),
		ActiveProfile:               recordplatform.ProjectionProfilePostgresSync,
		ActivationMutationID:        "tm-" + strings.Repeat("b", 64),
		WitnessedLedgerSequence:     1,
		WitnessedLedgerHash:         projectorCASDigest(1),
		PlanDigest:                  projectorCASDigest(2),
		AuthorizationArtifactDigest: projectorCASDigest(3),
		ActivationBundleDigest:      projectorCASDigest(4),
		TrustRevision:               1,
		TrustHeadHash:               projectorCASDigest(5),
		InventoryDigest:             projectorCASDigest(6),
		ApprovalPolicyDigest:        projectorCASDigest(7),
		AdapterPolicyGeneration:     1,
		AdapterPolicyDigest:         projectorCASDigest(8),
		DrainReceiptDigest:          projectorCASDigest(9),
		IdentitySetEpoch:            1,
		IdentitySetDigest:           projectorCASDigest(10),
		MinimumFenceContractVersion: 4,
	}
}

func projectorCASRotationFromActivation(activation recordplatform.ContractActivationProjectionCommandV1) recordplatform.DomainRotationProjectionCommandV1 {
	return recordplatform.DomainRotationProjectionCommandV1{
		DeploymentID:                        activation.DeploymentID,
		ActiveProfile:                       activation.ActiveProfile,
		RotationMutationID:                  "tm-" + strings.Repeat("c", 64),
		ExpectedWitnessedLedgerSequence:     activation.WitnessedLedgerSequence,
		ExpectedWitnessedLedgerHash:         activation.WitnessedLedgerHash,
		ExpectedIdentitySetEpoch:            activation.IdentitySetEpoch,
		ExpectedIdentitySetDigest:           activation.IdentitySetDigest,
		ExpectedAdapterPolicyGeneration:     activation.AdapterPolicyGeneration,
		ExpectedAdapterPolicyDigest:         activation.AdapterPolicyDigest,
		ExpectedMinimumFenceContractVersion: activation.MinimumFenceContractVersion,
		ExpectedTrustRevision:               activation.TrustRevision,
		ExpectedTrustHeadHash:               activation.TrustHeadHash,
		NextWitnessedLedgerSequence:         activation.WitnessedLedgerSequence + 1,
		NextWitnessedLedgerHash:             projectorCASDigest(11),
		NextIdentitySetEpoch:                activation.IdentitySetEpoch + 1,
		NextIdentitySetDigest:               projectorCASDigest(12),
		NextAdapterPolicyGeneration:         activation.AdapterPolicyGeneration + 1,
		NextAdapterPolicyDigest:             projectorCASDigest(13),
		NextMinimumFenceContractVersion:     activation.MinimumFenceContractVersion + 1,
		NextTrustRevision:                   activation.TrustRevision + 1,
		NextTrustHeadHash:                   projectorCASDigest(14),
	}
}

func assertProjectorCASRotationState(t *testing.T, ctx context.Context, db *pgxpool.Pool, rotation recordplatform.DomainRotationProjectionCommandV1) {
	t.Helper()

	var witnessedSequence, lastIdentitySequence, identityEpoch, policyGeneration, fence, trustRevision int64
	var witnessedHash, lastIdentityHash, identityDigest, policyDigest, trustHash []byte
	if err := db.QueryRow(ctx, `
		select witnessed_ledger_sequence,
		       witnessed_ledger_hash,
		       last_domain_identity_sequence,
		       last_domain_identity_entry_hash,
		       active_domain_identity_epoch,
		       active_domain_identity_set_digest,
		       active_adapter_policy_generation,
		       active_adapter_policy_digest,
		       minimum_fence_contract_version,
		       trust_revision,
		       trust_head_hash
		from public.deployment_contract_state
		where project_id = 'default'
	`).Scan(
		&witnessedSequence,
		&witnessedHash,
		&lastIdentitySequence,
		&lastIdentityHash,
		&identityEpoch,
		&identityDigest,
		&policyGeneration,
		&policyDigest,
		&fence,
		&trustRevision,
		&trustHash,
	); err != nil {
		t.Fatalf("read rotated deployment contract state: %v", err)
	}
	if witnessedSequence != int64(rotation.NextWitnessedLedgerSequence) || lastIdentitySequence != int64(rotation.NextWitnessedLedgerSequence) || identityEpoch != int64(rotation.NextIdentitySetEpoch) || policyGeneration != int64(rotation.NextAdapterPolicyGeneration) || fence != int64(rotation.NextMinimumFenceContractVersion) || trustRevision != int64(rotation.NextTrustRevision) {
		t.Fatalf("rotated numeric state = witness=%d last=%d identity=%d policy=%d fence=%d trust=%d, want %d/%d/%d/%d/%d/%d", witnessedSequence, lastIdentitySequence, identityEpoch, policyGeneration, fence, trustRevision, rotation.NextWitnessedLedgerSequence, rotation.NextWitnessedLedgerSequence, rotation.NextIdentitySetEpoch, rotation.NextAdapterPolicyGeneration, rotation.NextMinimumFenceContractVersion, rotation.NextTrustRevision)
	}
	for _, check := range []struct {
		name string
		got  []byte
		want [32]byte
	}{
		{name: "witnessed hash", got: witnessedHash, want: rotation.NextWitnessedLedgerHash},
		{name: "last identity hash", got: lastIdentityHash, want: rotation.NextWitnessedLedgerHash},
		{name: "identity digest", got: identityDigest, want: rotation.NextIdentitySetDigest},
		{name: "policy digest", got: policyDigest, want: rotation.NextAdapterPolicyDigest},
		{name: "trust hash", got: trustHash, want: rotation.NextTrustHeadHash},
	} {
		if !bytes.Equal(check.got, check.want[:]) {
			t.Fatalf("%s = %x, want %x", check.name, check.got, check.want)
		}
	}
}

func assertProjectorCASConcurrentContenders(t *testing.T, ctx context.Context, db, runtimeDB *pgxpool.Pool, previous recordplatform.DomainRotationProjectionCommandV1) {
	t.Helper()

	first := projectorCASNextRotation(previous, 21, 22)
	second := projectorCASNextRotation(previous, 31, 32)
	firstBytes, err := first.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal first concurrent rotation: %v", err)
	}
	secondBytes, err := second.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal second concurrent rotation: %v", err)
	}

	results := invokeProjectorCASConcurrently(t, ctx, runtimeDB, firstBytes, secondBytes)
	var winner projectorCASConcurrentResult
	winnerCount := 0
	for _, result := range results {
		if result.err == nil {
			winner = result
			winnerCount++
			continue
		}
		var pgErr *pgconn.PgError
		if !errors.As(result.err, &pgErr) || pgErr.Code != "55000" {
			t.Fatalf("concurrent distinct rotation error = %v, want SQLSTATE 55000", result.err)
		}
	}
	if winnerCount != 1 {
		t.Fatalf("concurrent distinct rotation successes = %d, want 1", winnerCount)
	}

	var winningRotation recordplatform.DomainRotationProjectionCommandV1
	switch {
	case bytes.Equal(winner.command, firstBytes):
		winningRotation = first
	case bytes.Equal(winner.command, secondBytes):
		winningRotation = second
	default:
		t.Fatalf("concurrent winner command = %x, want one submitted command", winner.command)
	}
	assertProjectorCASReceipt(t, winner.command, winner.receipt)
	assertProjectorCASRotationState(t, ctx, db, winningRotation)
	winningRetry := invokeProjectorCASFunction(t, ctx, runtimeDB, "public.record_platform_cas_domain_rotation_projection", winner.command)
	if !bytes.Equal(winningRetry, winner.receipt) {
		t.Fatalf("concurrent winner retry receipt = %x, want %x", winningRetry, winner.receipt)
	}

	sameCommand := projectorCASNextRotation(winningRotation, 41, 42)
	sameBytes, err := sameCommand.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal same-byte concurrent rotation: %v", err)
	}
	sameResults := invokeProjectorCASConcurrently(t, ctx, runtimeDB, sameBytes, sameBytes)
	var sameReceipt []byte
	for _, result := range sameResults {
		if result.err != nil {
			t.Fatalf("concurrent same-byte rotation error = %v", result.err)
		}
		assertProjectorCASReceipt(t, sameBytes, result.receipt)
		if sameReceipt == nil {
			sameReceipt = result.receipt
			continue
		}
		if !bytes.Equal(result.receipt, sameReceipt) {
			t.Fatalf("concurrent same-byte receipt = %x, want %x", result.receipt, sameReceipt)
		}
	}
	if len(sameResults) != 2 || sameReceipt == nil {
		t.Fatalf("concurrent same-byte result count = %d, want 2 successful results", len(sameResults))
	}
	assertProjectorCASRotationState(t, ctx, db, sameCommand)
}

type projectorCASConcurrentResult struct {
	command []byte
	receipt []byte
	err     error
}

func invokeProjectorCASConcurrently(t *testing.T, ctx context.Context, runtimeDB *pgxpool.Pool, commands ...[]byte) []projectorCASConcurrentResult {
	t.Helper()

	start := make(chan struct{})
	results := make(chan projectorCASConcurrentResult, len(commands))
	var wait sync.WaitGroup
	for _, command := range commands {
		command := append([]byte(nil), command...)
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result := projectorCASConcurrentResult{command: command}
			result.err = runtimeDB.QueryRow(ctx, `select public.record_platform_cas_domain_rotation_projection($1)`, command).Scan(&result.receipt)
			results <- result
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	output := make([]projectorCASConcurrentResult, 0, len(commands))
	for result := range results {
		output = append(output, result)
	}
	return output
}

func projectorCASNextRotation(previous recordplatform.DomainRotationProjectionCommandV1, ledgerDigest, identityDigest byte) recordplatform.DomainRotationProjectionCommandV1 {
	return recordplatform.DomainRotationProjectionCommandV1{
		DeploymentID:                        previous.DeploymentID,
		ActiveProfile:                       previous.ActiveProfile,
		RotationMutationID:                  "tm-" + strings.Repeat(string(rune('a'+ledgerDigest%6)), 64),
		ExpectedWitnessedLedgerSequence:     previous.NextWitnessedLedgerSequence,
		ExpectedWitnessedLedgerHash:         previous.NextWitnessedLedgerHash,
		ExpectedIdentitySetEpoch:            previous.NextIdentitySetEpoch,
		ExpectedIdentitySetDigest:           previous.NextIdentitySetDigest,
		ExpectedAdapterPolicyGeneration:     previous.NextAdapterPolicyGeneration,
		ExpectedAdapterPolicyDigest:         previous.NextAdapterPolicyDigest,
		ExpectedMinimumFenceContractVersion: previous.NextMinimumFenceContractVersion,
		ExpectedTrustRevision:               previous.NextTrustRevision,
		ExpectedTrustHeadHash:               previous.NextTrustHeadHash,
		NextWitnessedLedgerSequence:         previous.NextWitnessedLedgerSequence + 1,
		NextWitnessedLedgerHash:             projectorCASDigest(ledgerDigest),
		NextIdentitySetEpoch:                previous.NextIdentitySetEpoch + 1,
		NextIdentitySetDigest:               projectorCASDigest(identityDigest),
		NextAdapterPolicyGeneration:         previous.NextAdapterPolicyGeneration,
		NextAdapterPolicyDigest:             previous.NextAdapterPolicyDigest,
		NextMinimumFenceContractVersion:     previous.NextMinimumFenceContractVersion,
		NextTrustRevision:                   previous.NextTrustRevision,
		NextTrustHeadHash:                   previous.NextTrustHeadHash,
	}
}

func projectorCASDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}

func TestPostgresIntegrationAdoptsNameOnlyMigrationLedger(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)

	applyPostgresMigrationsThrough(t, ctx, db, "0050_extend_command_action_audit.sql")
	var before time.Time
	if err := db.QueryRow(ctx, `
		select applied_at
		from schema_migrations
		where name = '0001_initial_schema.sql'
	`).Scan(&before); err != nil {
		t.Fatalf("read pre-adoption applied_at: %v", err)
	}
	if _, err := db.Exec(ctx, `alter table schema_migrations drop column checksum`); err != nil {
		t.Fatalf("simulate 0.59 name-only migration ledger: %v", err)
	}

	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() after name-only migration ledger error = %v", err)
	}

	var checksum string
	var after time.Time
	if err := db.QueryRow(ctx, `
		select checksum, applied_at
		from schema_migrations
		where name = '0001_initial_schema.sql'
	`).Scan(&checksum, &after); err != nil {
		t.Fatalf("read adopted migration ledger row: %v", err)
	}
	if len(checksum) != 64 {
		t.Fatalf("adopted checksum length = %d, want 64", len(checksum))
	}
	if !after.Equal(before) {
		t.Fatalf("adopted applied_at = %s, want preserved %s", after, before)
	}
	assertSingleIntValue(t, ctx, db, `select count(*)::int from schema_migrations`, currentRootSourceCount)
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from information_schema.columns
		where table_schema = 'public'
		  and table_name = 'schema_migrations'
		  and column_name = 'checksum'
		  and is_nullable = 'NO'
	`, 1)
}

func TestPostgresIntegrationCommandActionAuditUpgrade(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresSchema(t, ctx)

	execSQL(t, ctx, db, `
		create table users (
			user_id text primary key,
			username text not null unique,
			password_hash text not null,
			display_name text not null default '',
			role text not null default 'admin',
			created_at timestamptz not null default now(),
			password_changed_at timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, db, `
		create table monitoring_instances (
			monitoring_instance_id text primary key,
			display_name text not null
		)
	`)
	execSQL(t, ctx, db, `
		insert into users (user_id, username, password_hash, display_name)
		values ('usr_audit', 'audit-admin', 'hash', '审计管理员')
	`)
	execSQL(t, ctx, db, `
		insert into monitoring_instances (monitoring_instance_id, display_name)
		values ('mi_audit', 'Tokyo Audit')
	`)

	legacyMigration, err := fs.ReadFile(migrations.FS, "0046_create_command_action_audit.sql")
	if err != nil {
		t.Fatalf("read 0046 migration: %v", err)
	}
	execSQL(t, ctx, db, string(legacyMigration))
	execSQL(t, ctx, db, `
		create table command_action_audit_external_refs (
			external_ref_id text primary key
		)
	`)
	execSQL(t, ctx, db, `
		alter table monitoring_instance_command_action_audit
			add column external_ref_id text,
			add constraint command_action_audit_external_ref_fkey
			foreign key (external_ref_id)
			references command_action_audit_external_refs(external_ref_id)
	`)
	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, actor_user_id, source, occurred_at
		) values (
			'cmd_aud_legacy', 'act_legacy', 'mi_audit', 'uptime',
			'standard', 'queued', 'usr_audit', 'web', '2026-07-01T00:00:00Z'
		)
	`)

	extensionMigration, err := fs.ReadFile(migrations.FS, "0050_extend_command_action_audit.sql")
	if err != nil {
		t.Fatalf("read 0050 migration: %v", err)
	}
	execSQL(t, ctx, db, string(extensionMigration))
	execSQL(t, ctx, db, string(extensionMigration))

	var instanceName, actorUsername, actorDisplayName string
	if err := db.QueryRow(ctx, `
		select monitoring_instance_name_snapshot, actor_username_snapshot, actor_display_name_snapshot
		from monitoring_instance_command_action_audit
		where audit_id = 'cmd_aud_legacy'
	`).Scan(&instanceName, &actorUsername, &actorDisplayName); err != nil {
		t.Fatalf("query backfilled command audit snapshots: %v", err)
	}
	if instanceName != "Tokyo Audit" || actorUsername != "audit-admin" || actorDisplayName != "审计管理员" {
		t.Fatalf("backfilled snapshots = (%q, %q, %q)", instanceName, actorUsername, actorDisplayName)
	}

	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, actor_user_id, source, occurred_at
		) values (
			'cmd_aud_rollback', 'act_rollback', 'mi_audit', 'uptime',
			'standard', 'queued', 'usr_audit', 'web', '2026-07-01T00:01:00Z'
		)
	`)
	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, occurred_at
		) values (
			'cmd_aud_rollback_dispatched', 'act_rollback', 'mi_audit', 'uptime',
			'standard', 'dispatched', 'agent_sync', '2026-07-01T00:01:01Z'
		)
	`)
	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, exit_code, occurred_at
		) values (
			'cmd_aud_rollback_completed', 'act_rollback', 'mi_audit', 'uptime',
			'standard', 'completed', 'agent_sync', 0, '2026-07-01T00:01:02Z'
		)
	`)
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from monitoring_instance_command_action_audit
		where action_id = 'act_rollback'
			and monitoring_instance_name_snapshot = ''
			and actor_username_snapshot = ''
			and actor_display_name_snapshot = ''
	`, 3)

	execSQL(t, ctx, db, `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, actor_user_id, source, occurred_at, details
		) values (
			'cmd_aud_rejected', null, 'mi_audit', 'systemctl_status',
			'sensitive', 'rejected', 'usr_audit', 'web', '2026-07-01T00:02:00Z',
			'{"reason":"sensitive_confirmation_required"}'::jsonb
		)
	`)

	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_action_identity_valid", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, details
		) values (
			'cmd_aud_bad_rejected_action', 'act_bad', 'mi_audit', 'systemctl_status',
			'sensitive', 'rejected', 'web', '{"reason":"sensitive_confirmation_required"}'::jsonb
		)
	`)
	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_action_identity_valid", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source
		) values ('cmd_aud_bad_queued_action', null, 'mi_audit', 'uptime', 'standard', 'queued', 'web')
	`)
	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_rejected_source_valid", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, details
		) values (
			'cmd_aud_bad_rejected_source', null, 'mi_audit', 'systemctl_status',
			'sensitive', 'rejected', 'agent_sync', '{"reason":"sensitive_confirmation_required"}'::jsonb
		)
	`)
	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_rejected_reason_valid", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, details
		) values (
			'cmd_aud_bad_rejected_reason', null, 'mi_audit', 'systemctl_status',
			'sensitive', 'rejected', 'web', '{"reason":"other"}'::jsonb
		)
	`)
	expectSQLConstraintFailure(t, ctx, db, "command_action_audit_details_metadata_only", `
		insert into monitoring_instance_command_action_audit (
			audit_id, action_id, monitoring_instance_id, command_id,
			sensitivity, event_type, source, details
		) values (
			'cmd_aud_bad_output', 'act_output', 'mi_audit', 'uptime',
			'standard', 'queued', 'web', '{"nested":{"stdout":"must-not-persist"}}'::jsonb
		)
	`)

	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_constraint
		where conrelid = 'monitoring_instance_command_action_audit'::regclass
			and contype = 'f'
			and confrelid in ('monitoring_instances'::regclass, 'users'::regclass)
	`, 0)
	assertSingleIntValue(t, ctx, db, `
		select count(*)::int
		from pg_constraint
		where conrelid = 'monitoring_instance_command_action_audit'::regclass
			and conname = 'command_action_audit_external_ref_fkey'
	`, 1)
	execSQL(t, ctx, db, `delete from users where user_id = 'usr_audit'`)
	execSQL(t, ctx, db, `delete from monitoring_instances where monitoring_instance_id = 'mi_audit'`)
	assertSingleIntValue(t, ctx, db, `select count(*)::int from monitoring_instance_command_action_audit`, 5)
	assertSingleStringValue(t, ctx, db, `
		select actor_user_id
		from monitoring_instance_command_action_audit
		where audit_id = 'cmd_aud_legacy'
	`, "usr_audit")
}

func TestPostgresIntegrationVPSFirstUpgradeNormalizesLegacyState(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresSchema(t, ctx)

	createMinimalPost0029Schema(t, ctx, db)
	seedVPSFirstLegacyData(t, ctx, db)

	migrationSQL, err := fs.ReadFile(migrations.FS, "0030_vps_first_status_semantics.sql")
	if err != nil {
		t.Fatalf("read 0030 migration: %v", err)
	}
	if _, err := db.Exec(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("exec 0030 upgrade error = %v", err)
	}
	if _, err := db.Exec(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("exec 0030 upgrade second run error = %v", err)
	}

	assertVPSBusinessState(t, ctx, db, "vps_auto_cancel", "to_cancel", "unknown", "auto_renew_cancelled")
	assertVPSBusinessState(t, ctx, db, "vps_expired_running", "to_cancel", "unknown", "cancel")
	assertVPSBusinessState(t, ctx, db, "vps_expired_stopped", "cancelled", "unknown", "cancel")
	assertVPSBusinessState(t, ctx, db, "vps_paused_review", "active", "unknown", "observe")
	assertVPSBusinessState(t, ctx, db, "vps_invalid_review", "active", "unknown", "observe")
	assertVPSBusinessState(t, ctx, db, "vps_active_clean", "active", "unknown", "unreviewed")

	assertSingleStringValue(t, ctx, db, "select status from subscriptions where subscription_id = 'sub_invalid'", "unknown")
	assertSingleStringValue(t, ctx, db, "select lifecycle_status from monitoring_instances where monitoring_instance_id = 'mi_invalid'", "待接入")

	assertSingleIntValue(t, ctx, db, "select count(*)::int from asset_lifecycle_actions where action_id like 'ala_mig0030_%'", 5)
	assertSingleIntValue(t, ctx, db, "select count(*)::int from asset_lifecycle_action_steps where step_id like 'als_mig0030_%'", 5)
	assertSingleIntValue(t, ctx, db, "select count(*)::int from renewal_decisions where decision_id like 'rdec_mig0030_%'", 5)
}

func TestPostgresIntegrationUpgradePreservesExistingLogin(t *testing.T) {
	ctx := context.Background()
	db := openTemporaryPostgresDatabase(t, ctx)
	const (
		existingPassword    = "Legacy-Login-42!"
		replacementPassword = "Replacement-Seed-42!"
	)

	applyPostgresMigrationsThrough(t, ctx, db, "0029_rename_nodes_to_monitoring_instances.sql")
	legacyHash, err := auth.HashPassword(existingPassword)
	if err != nil {
		t.Fatalf("HashPassword legacy: %v", err)
	}
	execSQL(t, ctx, db, `
		insert into users (user_id, username, password_hash, display_name, role, created_at, password_changed_at)
		values ('usr_existing', 'admin', $1, '管理员', 'admin', now() - interval '1 day', now() - interval '1 day')
	`, legacyHash)
	if err := Apply(ctx, db); err != nil {
		t.Fatalf("Apply() upgrade with existing user error = %v", err)
	}

	users := store.NewPostgresUserRepository(db)
	if err := auth.SeedInitialUser(ctx, users, "admin", replacementPassword, "管理员", func() time.Time {
		return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	}); err != nil {
		t.Fatalf("SeedInitialUser: %v", err)
	}
	u, err := users.FindByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("FindByUsername admin: %v", err)
	}
	if u.PasswordHash != legacyHash {
		t.Fatal("existing password hash changed during upgrade/bootstrap")
	}

	sessions, err := store.NewPostgresSessionRepository(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewPostgresSessionRepository: %v", err)
	}
	svc := auth.New(users, sessions, auth.Options{
		SessionTTL: time.Hour,
		Now: func() time.Time {
			return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
		},
	})
	sess, err := svc.Login(ctx, "admin", existingPassword, "ua", "127.0.0.1")
	if err != nil {
		t.Fatalf("Login with existing password after upgrade: %v", err)
	}
	if sess.UserID != "usr_existing" {
		t.Fatalf("session user = %q, want usr_existing", sess.UserID)
	}
	_, err = svc.Login(ctx, "admin", replacementPassword, "", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Login with seed replacement password = %v, want ErrInvalidCredentials", err)
	}
}

func applyPostgresMigrationsThrough(t *testing.T, ctx context.Context, db *pgxpool.Pool, throughName string) {
	t.Helper()
	names, err := Names()
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	files := fstest.MapFS{}
	found := false
	for _, name := range names {
		payload, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		files[name] = &fstest.MapFile{Data: payload}
		if name == throughName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migration %q not found", throughName)
	}
	if err := applyFS(ctx, poolStore{db: db}, files); err != nil {
		t.Fatalf("apply migrations through %s: %v", throughName, err)
	}
}

func openTemporaryPostgresDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skipf("%s=1 is required for postgres integration tests", postgresIntegrationFlag)
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for postgres integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	testDatabaseName := fmt.Sprintf("houfeng_test_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !isSafePostgresIdentifier(testDatabaseName) {
		t.Fatalf("unsafe generated database name %q", testDatabaseName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open admin postgres pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	if _, err := adminPool.Exec(ctx, `create database `+quotePostgresIdentifier(testDatabaseName)); err != nil {
		if isPostgresInsufficientPrivilege(err) {
			t.Skipf("temporary database creation requires CREATEDB privilege: %v", err)
		}
		t.Fatalf("create temporary postgres database %q: %v", testDatabaseName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop database if exists `+quotePostgresIdentifier(testDatabaseName)+` with (force)`); err != nil {
			t.Errorf("drop temporary postgres database %q: %v", testDatabaseName, err)
		}
	})

	testConfig := adminConfig.Copy()
	testConfig.ConnConfig.Database = testDatabaseName
	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres database %q: %v", testDatabaseName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary postgres database %q: %v", testDatabaseName, err)
	}
	return testPool
}

func openTemporaryPostgresSchema(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	if os.Getenv(postgresIntegrationFlag) != "1" {
		t.Skipf("%s=1 is required for postgres integration tests", postgresIntegrationFlag)
	}
	databaseURL := strings.TrimSpace(os.Getenv("HOUFENG_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("HOUFENG_DATABASE_URL is required for postgres integration tests")
	}

	adminConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse HOUFENG_DATABASE_URL: %v", err)
	}
	schemaName := fmt.Sprintf("houfeng_it_%d_%d", time.Now().UnixNano(), os.Getpid())
	if !isSafePostgresIdentifier(schemaName) {
		t.Fatalf("unsafe generated schema name %q", schemaName)
	}

	adminPool, err := pgxpool.NewWithConfig(ctx, adminConfig)
	if err != nil {
		t.Fatalf("open postgres pool for schema setup: %v", err)
	}
	t.Cleanup(adminPool.Close)

	if _, err := adminPool.Exec(ctx, `create schema `+quotePostgresIdentifier(schemaName)); err != nil {
		t.Fatalf("create temporary postgres schema %q: %v", schemaName, err)
	}
	t.Cleanup(func() {
		dropCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := adminPool.Exec(dropCtx, `drop schema if exists `+quotePostgresIdentifier(schemaName)+` cascade`); err != nil {
			t.Logf("drop temporary postgres schema %q: %v", schemaName, err)
		}
	})

	testConfig := adminConfig.Copy()
	if testConfig.ConnConfig.RuntimeParams == nil {
		testConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	testConfig.ConnConfig.RuntimeParams["search_path"] = schemaName

	testPool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		t.Fatalf("open temporary postgres schema %q: %v", schemaName, err)
	}
	t.Cleanup(testPool.Close)
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping temporary postgres schema %q: %v", schemaName, err)
	}
	return testPool
}

func createLegacyAuthSchema(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	execSQL(t, ctx, db, `
		create table users (
		  user_id              text primary key,
		  username             text not null unique,
		  password_hash        text not null,
		  display_name         text not null default '',
		  role                 text not null default 'admin',
		  created_at           timestamptz not null default now(),
		  password_changed_at  timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, db, `
		create table sessions (
		  session_id    text primary key,
		  user_id       text not null references users(user_id) on delete cascade,
		  issued_at     timestamptz not null default now(),
		  last_seen_at  timestamptz not null default now(),
		  expires_at    timestamptz not null,
		  user_agent    text not null default '',
		  client_ip     text not null default ''
		)
	`)
	execSQL(t, ctx, db, `create index sessions_user_idx on sessions(user_id)`)
	execSQL(t, ctx, db, `create index sessions_expires_idx on sessions(expires_at)`)
}

func markMigrationsAppliedThrough(t *testing.T, ctx context.Context, db *pgxpool.Pool, throughName string) {
	t.Helper()

	if _, err := db.Exec(ctx, ensureSchemaMigrationsSQL); err != nil {
		t.Fatalf("ensure schema_migrations: %v", err)
	}
	names, err := Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	for _, name := range names {
		execSQL(t, ctx, db, `insert into schema_migrations (name) values ($1)`, name)
		if name == throughName {
			return
		}
	}
	t.Fatalf("migration %q not found", throughName)
}

func createMinimalPost0029Schema(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	execSQL(t, ctx, db, `
		create table providers (
			provider_id text primary key,
			name text not null,
			labels text[] not null default '{}',
			note text not null default ''
		)
	`)
	execSQL(t, ctx, db, `
		create table vps_assets (
			vps_id text primary key,
			display_name text not null,
			provider_id text references providers(provider_id) on delete set null,
			provider_name text not null default '',
			lifecycle_status text not null check (lifecycle_status in ('active', 'idle', 'testing', 'to_migrate', 'to_cancel', 'cancelled', 'archived')),
			usage_status text not null check (usage_status in ('in_use', 'idle', 'standby', 'testing', 'unknown')),
			renewal_decision text not null default 'unreviewed' check (renewal_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')),
			ssh_port integer not null default 22,
			labels text[] not null default '{}',
			note text not null default '',
			updated_at timestamptz not null default now(),
			archived_at timestamptz
		)
	`)
	execSQL(t, ctx, db, `
		create table subscriptions (
			subscription_id text primary key,
			vps_id text not null references vps_assets(vps_id) on delete cascade,
			price numeric(12, 2) not null,
			currency text not null,
			billing_cycle text not null default '',
			billing_months integer not null,
			monthly_price numeric(12, 4) not null,
			renew_at date,
			auto_renew boolean not null default false,
			auto_renew_cancelled boolean not null default false,
			status text not null default 'active',
			payment_method text not null default '',
			note text not null default '',
			updated_at timestamptz not null default now(),
			constraint subscriptions_status_allowed check (status in ('active', 'paused', 'cancelled', 'expired', 'unknown'))
		)
	`)
	execSQL(t, ctx, db, `
		create table monitoring_instances (
			monitoring_instance_id text primary key,
			display_name text not null,
			region text not null,
			city text not null,
			provider text not null,
			lifecycle_status text not null,
			monitoring_status text not null default '启用',
			binding_status text not null default '未绑定',
			labels text[] not null default '{}',
			note text not null default '',
			current_health_status text not null default '正常',
			current_active_incident_count integer not null default 0,
			current_primary_issue_summary text not null default '',
			updated_at timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, db, `
		create table vps_monitoring_instance_links (
			link_id text primary key,
			vps_id text not null references vps_assets(vps_id) on delete cascade,
			monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade,
			linked_at timestamptz not null default now(),
			unlinked_at timestamptz,
			note text not null default ''
		)
	`)
	execSQL(t, ctx, db, `
		create table asset_lifecycle_actions (
			action_id text primary key,
			vps_id text not null references vps_assets(vps_id) on delete cascade,
			action_type text not null check (action_type in ('cancel_vps')),
			status text not null default 'completed' check (status in ('pending', 'completed', 'failed')),
			reason text not null default '',
			summary jsonb not null default '{}'::jsonb,
			created_at timestamptz not null default now(),
			confirmed_at timestamptz,
			completed_at timestamptz
		)
	`)
	execSQL(t, ctx, db, `
		create table asset_lifecycle_action_steps (
			step_id text primary key,
			action_id text not null references asset_lifecycle_actions(action_id) on delete cascade,
			object_type text not null check (object_type in ('vps', 'subscription', 'monitoring_instance', 'target')),
			object_id text not null,
			step_type text not null check (step_type in ('vps_lifecycle', 'subscription_status', 'monitoring_instance_lifecycle', 'monitoring_instance_monitoring', 'target_run_status')),
			status text not null check (status in ('completed', 'skipped', 'failed')),
			before_state jsonb not null default '{}'::jsonb,
			after_state jsonb not null default '{}'::jsonb,
			message text not null default '',
			executed_at timestamptz,
			created_at timestamptz not null default now()
		)
	`)
	execSQL(t, ctx, db, `
		create table renewal_decisions (
			decision_id text primary key,
			vps_id text not null references vps_assets(vps_id) on delete cascade,
			from_decision text,
			to_decision text not null check (to_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')),
			reason text not null default '',
			decided_at timestamptz not null default now(),
			created_at timestamptz not null default now()
		)
	`)
}

func seedVPSFirstLegacyData(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	execSQL(t, ctx, db, `
		insert into providers (provider_id, name, labels, note)
		values ('pv_pgtest', 'Postgres Test Provider', '{}', '')
	`)
	execSQL(t, ctx, db, `
		insert into vps_assets (
			vps_id, display_name, provider_id, provider_name, lifecycle_status,
			usage_status, renewal_decision, ssh_port, labels, note
		) values
			('vps_auto_cancel', 'Auto Cancel', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_expired_running', 'Expired Running', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_expired_stopped', 'Expired Stopped', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_paused_review', 'Paused Review', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_invalid_review', 'Invalid Review', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', ''),
			('vps_active_clean', 'Active Clean', 'pv_pgtest', 'Postgres Test Provider', 'active', 'unknown', 'unreviewed', 22, '{}', '')
	`)
	execSQL(t, ctx, db, `
		insert into subscriptions (
			subscription_id, vps_id, price, currency, billing_cycle, billing_months,
			monthly_price, renew_at, auto_renew, auto_renew_cancelled, status, payment_method, note
		) values
			('sub_auto_cancel', 'vps_auto_cancel', 12, 'USD', 'monthly', 1, 12, current_date + 20, false, true, 'active', '', ''),
			('sub_expired_running', 'vps_expired_running', 12, 'USD', 'monthly', 1, 12, current_date - 1, false, false, 'expired', '', ''),
			('sub_expired_stopped', 'vps_expired_stopped', 12, 'USD', 'monthly', 1, 12, current_date - 1, false, false, 'expired', '', ''),
			('sub_paused_review', 'vps_paused_review', 12, 'USD', 'monthly', 1, 12, current_date + 20, false, false, 'paused', '', ''),
			('sub_invalid', 'vps_invalid_review', 12, 'USD', 'monthly', 1, 12, current_date + 20, false, false, 'active', '', ''),
			('sub_active_clean', 'vps_active_clean', 12, 'USD', 'monthly', 1, 12, current_date + 20, true, false, 'active', '', '')
	`)
	execSQL(t, ctx, db, `alter table subscriptions drop constraint subscriptions_status_allowed`)
	execSQL(t, ctx, db, `update subscriptions set status = 'legacy-bad' where subscription_id = 'sub_invalid'`)

	execSQL(t, ctx, db, `
		insert into monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider, lifecycle_status,
			monitoring_status, binding_status, labels, note, current_health_status,
			current_active_incident_count, current_primary_issue_summary
		) values
			('mi_running', 'Running MI', 'Tokyo', 'Tokyo', 'Postgres Test Provider', '在用', '启用', '未绑定', '{}', '', '正常', 0, ''),
			('mi_retired', 'Retired MI', 'Tokyo', 'Tokyo', 'Postgres Test Provider', '已退役', '暂停', '未绑定', '{}', '', '正常', 0, ''),
			('mi_invalid', 'Invalid MI', 'Tokyo', 'Tokyo', 'Postgres Test Provider', '在用', '启用', '未绑定', '{}', '', '正常', 0, '')
	`)
	execSQL(t, ctx, db, `update monitoring_instances set lifecycle_status = 'legacy-bad' where monitoring_instance_id = 'mi_invalid'`)
	execSQL(t, ctx, db, `
		insert into vps_monitoring_instance_links (link_id, vps_id, monitoring_instance_id, note)
		values
			('vnl_running', 'vps_expired_running', 'mi_running', ''),
			('vnl_retired', 'vps_expired_stopped', 'mi_retired', ''),
			('vnl_invalid', 'vps_active_clean', 'mi_invalid', '')
	`)
}

func execSQL(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec sql %q error = %v", oneLineSQL(sql), err)
	}
}

func expectSQLConstraintFailure(t *testing.T, ctx context.Context, db *pgxpool.Pool, constraintName, sql string) {
	t.Helper()
	_, err := db.Exec(ctx, sql)
	if err == nil {
		t.Fatalf("exec sql %q succeeded, want constraint %q failure", oneLineSQL(sql), constraintName)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("exec sql %q error = %T %v, want postgres error", oneLineSQL(sql), err, err)
	}
	if pgErr.ConstraintName != constraintName {
		t.Fatalf("exec sql %q constraint = %q, want %q", oneLineSQL(sql), pgErr.ConstraintName, constraintName)
	}
}

func assertVPSBusinessState(t *testing.T, ctx context.Context, db *pgxpool.Pool, vpsID, lifecycle, usage, renewal string) {
	t.Helper()
	var gotLifecycle, gotUsage, gotRenewal string
	if err := db.QueryRow(ctx, `
		select lifecycle_status, usage_status, renewal_decision
		from vps_assets
		where vps_id = $1`, vpsID).Scan(&gotLifecycle, &gotUsage, &gotRenewal); err != nil {
		t.Fatalf("query vps %q business state: %v", vpsID, err)
	}
	if gotLifecycle != lifecycle || gotUsage != usage || gotRenewal != renewal {
		t.Fatalf("vps %q state = (%q, %q, %q), want (%q, %q, %q)", vpsID, gotLifecycle, gotUsage, gotRenewal, lifecycle, usage, renewal)
	}
}

func assertSingleStringValue(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(ctx, sql).Scan(&got); err != nil {
		t.Fatalf("query %q error = %v", oneLineSQL(sql), err)
	}
	if got != want {
		t.Fatalf("query %q = %q, want %q", oneLineSQL(sql), got, want)
	}
}

func assertSingleIntValue(t *testing.T, ctx context.Context, db *pgxpool.Pool, sql string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(ctx, sql).Scan(&got); err != nil {
		t.Fatalf("query %q error = %v", oneLineSQL(sql), err)
	}
	if got != want {
		t.Fatalf("query %q = %d, want %d", oneLineSQL(sql), got, want)
	}
}

func isSafePostgresIdentifier(value string) bool {
	return regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(value)
}

func quotePostgresIdentifier(value string) string {
	return pgx.Identifier{value}.Sanitize()
}

func isPostgresInsufficientPrivilege(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func oneLineSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
