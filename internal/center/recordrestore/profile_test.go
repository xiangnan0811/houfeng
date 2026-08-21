package recordrestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"testing"

	"houfeng/internal/center/recordbackup"
	"houfeng/internal/center/recordreadiness"
)

func TestLocalProfileBackupRestoreRoundTrip(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := recordbackup.NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	database, err := recordbackup.NewArtifactRef(
		"postgres_dump", "db.v1",
		sha256.Sum256([]byte("database-artifact")), uint64(len("database-artifact")),
		recordbackup.ClassificationDatabase,
	)
	if err != nil {
		t.Fatalf("NewArtifactRef(database) error = %v", err)
	}
	object, err := recordbackup.NewArtifactRef(
		"record_attachments", "blob.v1",
		sha256.Sum256([]byte("object-artifact")), uint64(len("object-artifact")),
		recordbackup.ClassificationObject,
	)
	if err != nil {
		t.Fatalf("NewArtifactRef(object) error = %v", err)
	}
	build := restoreBuild(t)
	backup, err := recordbackup.NewService(recordbackup.Options{
		Store:    store,
		Database: &restoreDatabaseStub{artifact: database, payload: []byte("database-artifact")},
		Objects:  &restoreObjectStub{artifacts: []recordbackup.ArtifactRef{object}, payload: []byte("object-artifact")},
		Build:    build,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	manifest, _, err := backup.Create(context.Background(), recordbackup.Request{Profile: recordbackup.ProfileLocal})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	target := &profileRestoreTarget{empty: true}
	restore, err := NewService(Options{
		Target:      target,
		Source:      store,
		Replay:      []ReplayAdapter{profileReplayStub{}},
		Projections: profileProjectionStub{},
		ACL:         profileNoop{},
		Verifier:    profileNoop{},
		Readiness:   profileNoop{},
		Current:     build,
	})
	if err != nil {
		t.Fatalf("restore NewService() error = %v", err)
	}
	result, _, err := restore.Apply(context.Background(), Request{Manifest: manifest})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Ready() {
		t.Fatal("Apply() ready = false")
	}
	if string(target.database) != "database-artifact" || string(target.object) != "object-artifact" {
		t.Fatalf("restored bytes database=%q object=%q", target.database, target.object)
	}
}

func restoreBuild(t *testing.T) recordbackup.BuildIdentity {
	t.Helper()
	adapters := make([]recordbackup.AdapterRef, 0, len(recordreadiness.RequiredCapabilityKinds()))
	for _, kind := range recordreadiness.RequiredCapabilityKinds() {
		ref, err := recordbackup.NewAdapterRef(kind, recordbackup.CapabilityContractVersionV1)
		if err != nil {
			t.Fatalf("NewAdapterRef(%q) error = %v", kind, err)
		}
		adapters = append(adapters, ref)
	}
	deletion, err := recordbackup.NewDeletionWatermark(7, sha256.Sum256([]byte("deletion-watermark")))
	if err != nil {
		t.Fatalf("NewDeletionWatermark() error = %v", err)
	}
	return recordbackup.BuildIdentity{
		Commit:          "6a37448ddeadbeef",
		Version:         "0.73.1",
		MigrationDigest: sha256.Sum256([]byte("migration-digest-fixture")),
		AppACLDigest:    sha256.Sum256([]byte("app-acl-digest-fixture")),
		Adapters:        adapters,
		Deletion:        deletion,
		Profile:         recordbackup.ProfileLocal,
	}
}

type restoreDatabaseStub struct {
	artifact recordbackup.ArtifactRef
	payload  []byte
}

func (source *restoreDatabaseStub) Dump(context.Context) (io.ReadCloser, recordbackup.ArtifactRef, error) {
	return io.NopCloser(bytes.NewReader(source.payload)), source.artifact, nil
}

type restoreObjectStub struct {
	artifacts []recordbackup.ArtifactRef
	payload   []byte
}

func (source *restoreObjectStub) List(context.Context) ([]recordbackup.ArtifactRef, error) {
	return append([]recordbackup.ArtifactRef(nil), source.artifacts...), nil
}

func (source *restoreObjectStub) Open(context.Context, recordbackup.ArtifactRef) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(source.payload)), nil
}

type profileRestoreTarget struct {
	empty    bool
	database []byte
	object   []byte
}

func (target *profileRestoreTarget) Empty(context.Context) (bool, error) { return target.empty, nil }

func (target *profileRestoreTarget) RestoreDatabase(_ context.Context, body io.Reader) error {
	payload, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	target.database = payload
	target.empty = false
	return nil
}

func (target *profileRestoreTarget) RestoreObject(_ context.Context, _ recordbackup.ArtifactRef, body io.Reader) error {
	payload, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	target.object = payload
	return nil
}

func (target *profileRestoreTarget) Serving(context.Context) bool { return false }

func (target *profileRestoreTarget) Workers(context.Context) bool { return false }

type profileReplayStub struct{}

func (profileReplayStub) Kind() recordreadiness.CapabilityKind {
	return recordreadiness.CapabilityRecoveryRecordCore
}

func (profileReplayStub) ReplayDeletions(context.Context, recordbackup.DeletionWatermark) error {
	return nil
}

type profileProjectionStub struct{}

func (profileProjectionStub) RebuildSearch(context.Context) error { return nil }

func (profileProjectionStub) RebuildActivity(context.Context) error { return nil }

type profileNoop struct{}

func (profileNoop) Converge(context.Context) error { return nil }

func (profileNoop) Verify(context.Context) error { return nil }

func (profileNoop) Publish(context.Context, recordreadiness.StatusMatrix) error { return nil }
