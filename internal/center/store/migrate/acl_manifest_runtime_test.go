package migrate

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestVerifyPersistedAppACLManifestRuntimeV1AcceptsExactChainAndMigrationMaps(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	manifest := snapshot.Manifests[0]

	reader := fakeAppACLManifestRuntimeReader{
		snapshot: snapshot,
	}
	latest, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, reader, fsys)
	if err != nil {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v", err)
	}
	if latest.ManifestRevision != manifest.ManifestRevision || latest.ManifestDigest != manifest.ManifestDigest ||
		!bytes.Equal(latest.CanonicalMigrationSet, manifest.CanonicalMigrationSet) ||
		!bytes.Equal(latest.CanonicalPrivilegeSet, manifest.CanonicalPrivilegeSet) {
		t.Fatalf("latest manifest = %#v, want %#v", latest, manifest)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsPrivilegeSetCompiledForDifferentDatabase(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	manifest := snapshot.Manifests[0]
	privilegeSet, err := ParseCanonicalPrivilegeSetBodyV1(manifest.CanonicalPrivilegeSet)
	if err != nil {
		t.Fatalf("ParseCanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	differentDatabaseBody, err := CompileAppACLPrivilegeSetR1("different_database", privilegeSet.RoleBindings)
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() different database error = %v", err)
	}
	differentDatabaseManifest, err := NewAppACLManifestPersistedV1(
		manifest.ManifestRevision,
		manifest.MigratorCatalogRole,
		manifest.PreviousManifestDigest,
		manifest.CanonicalMigrationSet,
		differentDatabaseBody,
	)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() different database manifest error = %v", err)
	}
	snapshot.Manifests = []AppACLManifestPersistedV1{differentDatabaseManifest}
	snapshot.Head = &AppACLManifestHeadV1{
		ManifestRevision: differentDatabaseManifest.ManifestRevision,
		ManifestDigest:   differentDatabaseManifest.ManifestDigest,
	}
	_, err = VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{
		snapshot: snapshot,
	}, fsys)
	if err == nil || !strings.Contains(err.Error(), "frozen r1 compiler output") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want frozen r1 compiler rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsSessionUserBeforePrivilegeSetDrift(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	manifest := snapshot.Manifests[0]
	privilegeSet, err := ParseCanonicalPrivilegeSetBodyV1(manifest.CanonicalPrivilegeSet)
	if err != nil {
		t.Fatalf("ParseCanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	driftingPrivilegeBody, err := CompileAppACLPrivilegeSetR1("different_database", privilegeSet.RoleBindings)
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() drifting privilege body error = %v", err)
	}
	driftingManifest, err := NewAppACLManifestPersistedV1(
		manifest.ManifestRevision,
		manifest.MigratorCatalogRole,
		manifest.PreviousManifestDigest,
		manifest.CanonicalMigrationSet,
		driftingPrivilegeBody,
	)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() drifting manifest error = %v", err)
	}
	snapshot.SessionUser = "delegated_member"
	snapshot.Manifests = []AppACLManifestPersistedV1{driftingManifest}
	snapshot.Head = &AppACLManifestHeadV1{
		ManifestRevision: driftingManifest.ManifestRevision,
		ManifestDigest:   driftingManifest.ManifestDigest,
	}

	_, err = VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys)
	if err == nil || err.Error() != `session user "delegated_member" does not match current user "houfeng_center_runtime"` {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want exact session-user identity rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsCanonicalZeroTuplePrivilegeSet(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	manifest := snapshot.Manifests[0]
	zeroTupleBody, err := CanonicalPrivilegeSetBodyV1(
		[]AppACLRoleBinding{
			{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
			{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("CanonicalPrivilegeSetBodyV1() zero tuple body error = %v", err)
	}
	zeroTupleManifest, err := NewAppACLManifestPersistedV1(
		manifest.ManifestRevision,
		manifest.MigratorCatalogRole,
		manifest.PreviousManifestDigest,
		manifest.CanonicalMigrationSet,
		zeroTupleBody,
	)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() zero tuple manifest error = %v", err)
	}
	snapshot.Manifests = []AppACLManifestPersistedV1{zeroTupleManifest}
	snapshot.Head = &AppACLManifestHeadV1{
		ManifestRevision: zeroTupleManifest.ManifestRevision,
		ManifestDigest:   zeroTupleManifest.ManifestDigest,
	}

	_, err = VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{
		snapshot: snapshot,
	}, fsys)
	if err == nil || !strings.Contains(err.Error(), "frozen r1 compiler output") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want frozen r1 compiler rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsProjectorExecutePrivileges(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, privilegeBody := validAppACLManifestRuntimeFixture(t)
	manifest := snapshot.Manifests[0]
	privilegeSet, err := ParseCanonicalPrivilegeSetBodyV1(privilegeBody)
	if err != nil {
		t.Fatalf("ParseCanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	projectors := appACLProjectorFunctionsR1()
	tests := []struct {
		name      string
		subject   AppACLSubject
		projector appACLManagedFunctionR1
	}{
		{
			name:      "runtime activation projector",
			subject:   AppACLSubjectCenterRuntime,
			projector: projectors[0],
		},
		{
			name:      "runtime rotation projector",
			subject:   AppACLSubjectCenterRuntime,
			projector: projectors[1],
		},
		{
			name:      "admin activation projector",
			subject:   AppACLSubjectPlatformAdmin,
			projector: projectors[0],
		},
		{
			name:      "admin rotation projector",
			subject:   AppACLSubjectPlatformAdmin,
			projector: projectors[1],
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			privileges := append([]AppACLPrivilege(nil), privilegeSet.Privileges...)
			privileges = append(privileges, AppACLPrivilege{
				Subject:        tt.subject,
				ObjectClass:    AppACLObjectClassFunction,
				ObjectIdentity: tt.projector.schemaName + "." + tt.projector.identity,
				Privilege:      AppACLPrivilegeExecute,
			})
			projectorPrivilegeBody, err := CanonicalPrivilegeSetBodyV1(privilegeSet.RoleBindings, privileges)
			if err != nil {
				t.Fatalf("CanonicalPrivilegeSetBodyV1() projector privilege body error = %v", err)
			}
			projectorManifest, err := NewAppACLManifestPersistedV1(
				manifest.ManifestRevision,
				manifest.MigratorCatalogRole,
				manifest.PreviousManifestDigest,
				manifest.CanonicalMigrationSet,
				projectorPrivilegeBody,
			)
			if err != nil {
				t.Fatalf("NewAppACLManifestPersistedV1() projector manifest error = %v", err)
			}
			projectorSnapshot := snapshot
			projectorSnapshot.Manifests = []AppACLManifestPersistedV1{projectorManifest}
			projectorSnapshot.Head = &AppACLManifestHeadV1{
				ManifestRevision: projectorManifest.ManifestRevision,
				ManifestDigest:   projectorManifest.ManifestDigest,
			}

			_, err = VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{
				snapshot: projectorSnapshot,
			}, fsys)
			if err == nil || !strings.Contains(err.Error(), "frozen r1 compiler output") {
				t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want frozen r1 compiler rejection", err)
			}
		})
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsCurrentUserOutsideLatestRuntimeBinding(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	snapshot.SessionUser = "unexpected_runtime"
	snapshot.CurrentUser = "unexpected_runtime"

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys)
	if err == nil || !strings.Contains(err.Error(), "does not match latest app ACL manifest center runtime binding") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want runtime-binding rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsMigratorCatalogRoleThatReusesAppRole(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	manifest := snapshot.Manifests[0]
	rebound, err := NewAppACLManifestPersistedV1(
		manifest.ManifestRevision,
		"houfeng_center_runtime",
		manifest.PreviousManifestDigest,
		manifest.CanonicalMigrationSet,
		manifest.CanonicalPrivilegeSet,
	)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}
	snapshot.Manifests = []AppACLManifestPersistedV1{rebound}
	snapshot.Head = &AppACLManifestHeadV1{ManifestRevision: rebound.ManifestRevision, ManifestDigest: rebound.ManifestDigest}

	_, err = VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys)
	if err == nil || !strings.Contains(err.Error(), "reuses") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want migrator-role reuse rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsSessionUserThatDiffersFromCurrentUser(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	snapshot.SessionUser = "houfeng_platform_admin"

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys)
	if err == nil || !strings.Contains(err.Error(), "session user") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want session-user identity rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsNullHead(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	snapshot.Head = nil

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys)
	if err == nil || !strings.Contains(err.Error(), "head is null") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want null head rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsAppliedMigrationLedgerDrift(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	snapshot.AppliedMigrations[0].Checksum[0] ^= 0xff

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys)
	if err == nil || !strings.Contains(err.Error(), "does not match applied migration ledger") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want applied ledger drift rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1WrapsReaderFailure(t *testing.T) {
	ctx := context.Background()
	fsys, _, _ := validAppACLManifestRuntimeFixture(t)

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{err: context.DeadlineExceeded}, fsys)
	if err == nil || !strings.Contains(err.Error(), "read persisted app ACL manifest snapshot") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want reader failure", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsNilEmbeddedMigrationFS(t *testing.T) {
	ctx := context.Background()
	_, snapshot, _ := validAppACLManifestRuntimeFixture(t)

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, nil)
	if err == nil || !strings.Contains(err.Error(), "embedded migration filesystem is nil") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want nil embedded migration filesystem rejection", err)
	}
}

func validAppACLManifestRuntimeFixture(t *testing.T) (fstest.MapFS, AppACLManifestRuntimeSnapshotV1, []byte) {
	t.Helper()
	fsys := fstest.MapFS{
		"0001_create_schema.sql": {Data: []byte("create table example (id bigint primary key);\n")},
	}
	migrationBody, err := CanonicalMigrationSetFromFS(fsys)
	if err != nil {
		t.Fatalf("CanonicalMigrationSetFromFS() error = %v", err)
	}
	appliedMigrations, err := ParseCanonicalMigrationSetBodyV1(migrationBody)
	if err != nil {
		t.Fatalf("ParseCanonicalMigrationSetBodyV1() error = %v", err)
	}
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: "houfeng_center_runtime"},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: "houfeng_platform_admin"},
	}
	privilegeBody, err := CompileAppACLPrivilegeSetR1("houfeng", bindings)
	if err != nil {
		t.Fatalf("CompileAppACLPrivilegeSetR1() error = %v", err)
	}
	privilegeSet, err := ParseCanonicalPrivilegeSetBodyV1(privilegeBody)
	if err != nil {
		t.Fatalf("ParseCanonicalPrivilegeSetBodyV1() error = %v", err)
	}
	if len(privilegeSet.Privileges) != 204 {
		t.Fatalf("frozen r1 privilege tuple count = %d, want 204", len(privilegeSet.Privileges))
	}
	for _, privilege := range privilegeSet.Privileges {
		if privilege.ObjectClass == AppACLObjectClassFunction {
			t.Fatalf("frozen r1 privilege set unexpectedly grants persistent function %#v", privilege)
		}
	}
	manifest, err := NewAppACLManifestPersistedV1(1, "houfeng_migrator", [32]byte{}, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}
	return fsys, AppACLManifestRuntimeSnapshotV1{
		DatabaseName:      "houfeng",
		SessionUser:       "houfeng_center_runtime",
		CurrentUser:       "houfeng_center_runtime",
		Manifests:         []AppACLManifestPersistedV1{manifest},
		Head:              &AppACLManifestHeadV1{ManifestRevision: 1, ManifestDigest: manifest.ManifestDigest},
		AppliedMigrations: appliedMigrations,
	}, privilegeBody
}

type fakeAppACLManifestRuntimeReader struct {
	snapshot AppACLManifestRuntimeSnapshotV1
	err      error
}

func (reader fakeAppACLManifestRuntimeReader) ReadAppACLManifestRuntimeSnapshotV1(context.Context) (AppACLManifestRuntimeSnapshotV1, error) {
	return reader.snapshot, reader.err
}
