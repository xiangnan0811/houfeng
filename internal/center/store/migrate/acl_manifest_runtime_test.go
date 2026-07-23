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
	fsys, snapshot, privilegeBody := validAppACLManifestRuntimeFixture(t)
	manifest := snapshot.Manifests[0]

	reader := fakeAppACLManifestRuntimeReader{
		snapshot: snapshot,
	}
	latest, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, reader, fsys, privilegeBody)
	if err != nil {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v", err)
	}
	if latest.ManifestRevision != manifest.ManifestRevision || latest.ManifestDigest != manifest.ManifestDigest ||
		!bytes.Equal(latest.CanonicalMigrationSet, manifest.CanonicalMigrationSet) ||
		!bytes.Equal(latest.CanonicalPrivilegeSet, manifest.CanonicalPrivilegeSet) {
		t.Fatalf("latest manifest = %#v, want %#v", latest, manifest)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsCompiledPrivilegeSetDrift(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, _ := validAppACLManifestRuntimeFixture(t)
	expectedPrivilegeBody, err := CanonicalPrivilegeSetBodyV1(
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
		t.Fatalf("CanonicalPrivilegeSetBodyV1() expected error = %v", err)
	}
	_, err = VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{
		snapshot: snapshot,
	}, fsys, expectedPrivilegeBody)
	if err == nil || !strings.Contains(err.Error(), "does not match compiled privilege set") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want compiled privilege set drift rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsCurrentUserOutsideLatestRuntimeBinding(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, privilegeBody := validAppACLManifestRuntimeFixture(t)
	snapshot.CurrentUser = "unexpected_runtime"

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys, privilegeBody)
	if err == nil || !strings.Contains(err.Error(), "current user") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want current-user binding rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsNullHead(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, privilegeBody := validAppACLManifestRuntimeFixture(t)
	snapshot.Head = nil

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys, privilegeBody)
	if err == nil || !strings.Contains(err.Error(), "head is null") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want null head rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsAppliedMigrationLedgerDrift(t *testing.T) {
	ctx := context.Background()
	fsys, snapshot, privilegeBody := validAppACLManifestRuntimeFixture(t)
	snapshot.AppliedMigrations[0].Checksum[0] ^= 0xff

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, fsys, privilegeBody)
	if err == nil || !strings.Contains(err.Error(), "does not match applied migration ledger") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want applied ledger drift rejection", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1WrapsReaderFailure(t *testing.T) {
	ctx := context.Background()
	fsys, _, privilegeBody := validAppACLManifestRuntimeFixture(t)

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{err: context.DeadlineExceeded}, fsys, privilegeBody)
	if err == nil || !strings.Contains(err.Error(), "read persisted app ACL manifest snapshot") {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() error = %v, want reader failure", err)
	}
}

func TestVerifyPersistedAppACLManifestRuntimeV1RejectsNilEmbeddedMigrationFS(t *testing.T) {
	ctx := context.Background()
	_, snapshot, privilegeBody := validAppACLManifestRuntimeFixture(t)

	_, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, fakeAppACLManifestRuntimeReader{snapshot: snapshot}, nil, privilegeBody)
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
	manifest, err := NewAppACLManifestPersistedV1(1, [32]byte{}, migrationBody, privilegeBody)
	if err != nil {
		t.Fatalf("NewAppACLManifestPersistedV1() error = %v", err)
	}
	return fsys, AppACLManifestRuntimeSnapshotV1{
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
