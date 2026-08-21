package recordrestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"houfeng/internal/center/recordbackup"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordreadiness"
)

func TestRestoreBlocksResurrectionWhenReplayLeavesPurgedArtifact(t *testing.T) {
	t.Parallel()

	manifest, store := mustRecoveryBackup(t)
	target := newRecoveryTarget()
	restore, err := NewService(Options{
		Target:      target,
		Source:      store,
		Replay:      []ReplayAdapter{profileReplayStub{}},
		Projections: profileProjectionStub{},
		ACL:         profileNoop{},
		Verifier:    profileNoop{},
		Readiness:   profileNoop{},
		Current:     restoreBuild(t),
		PurgedKinds: []string{"record_evidence"},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, receipt, err := restore.Apply(context.Background(), Request{Manifest: manifest})
	if !errors.Is(err, ErrResurrectionBlocked) {
		t.Fatalf("Apply() error = %v, want ErrResurrectionBlocked", err)
	}
	if result.Ready() {
		t.Fatal("resurrection left restore ready")
	}
	if target.Serving(context.Background()) || target.Workers(context.Background()) {
		t.Fatal("resurrection started serving or workers")
	}
	if receipt.ReleasedWorkspaces() == 0 && len(receipt.AbortedSteps()) == 0 {
		t.Fatalf("blocked restore empty cleanup: %+v", receipt)
	}
}

func TestRestoreBackupDeleteReplayLeavesZeroResurrection(t *testing.T) {
	t.Parallel()

	manifest, store := mustRecoveryBackup(t)
	target := newRecoveryTarget()
	rebuilder := &recoveryProjectionStub{target: target}
	replay := &droppingReplay{target: target, kind: "record_evidence"}
	restore, err := NewService(Options{
		Target:      target,
		Source:      store,
		Replay:      []ReplayAdapter{replay},
		Projections: rebuilder,
		ACL:         profileNoop{},
		Verifier:    profileNoop{},
		Readiness:   profileNoop{},
		Current:     restoreBuild(t),
		PurgedKinds: []string{"record_evidence"},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, receipt, err := restore.Apply(context.Background(), Request{Manifest: manifest})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Ready() || len(receipt.AbortedSteps()) != 0 {
		t.Fatalf("Apply() ready=%v cleanup=%#v", result.Ready(), receipt.AbortedSteps())
	}
	if target.HasArtifact("record_evidence") || rebuilder.saw["record_evidence"] {
		t.Fatal("purged evidence resurrected after restore")
	}
	if !target.HasArtifact("record_attachments") || !rebuilder.saw["record_attachments"] {
		t.Fatal("survivor attachment missing after restore")
	}
	if replay.calls == 0 || rebuilder.search == 0 {
		t.Fatal("restore skipped replay or search rebuild")
	}
}

func TestRestoreFailureRetryUsesFreshTargetAndBoundedWorkspace(t *testing.T) {
	t.Parallel()

	manifest, store := mustRecoveryBackup(t)
	failing := newRecoveryTarget()
	failing.fail = StepRestoreDatabase
	first, err := NewService(Options{
		Target:      failing,
		Source:      store,
		Replay:      []ReplayAdapter{profileReplayStub{}},
		Projections: profileProjectionStub{},
		ACL:         profileNoop{},
		Verifier:    profileNoop{},
		Readiness:   profileNoop{},
		Current:     restoreBuild(t),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, receipt, err := first.Apply(context.Background(), Request{Manifest: manifest})
	if err == nil || result.Ready() {
		t.Fatal("injected failure must not report ready")
	}
	if receipt.ReleasedWorkspaces() == 0 {
		t.Fatal("injected failure released no workspace")
	}

	retryTarget := newRecoveryTarget()
	retry, err := NewService(Options{
		Target:      retryTarget,
		Source:      store,
		Replay:      []ReplayAdapter{&droppingReplay{target: retryTarget, kind: "record_evidence"}},
		Projections: profileProjectionStub{},
		ACL:         profileNoop{},
		Verifier:    profileNoop{},
		Readiness:   profileNoop{},
		Current:     restoreBuild(t),
		PurgedKinds: []string{"record_evidence"},
	})
	if err != nil {
		t.Fatalf("retry NewService() error = %v", err)
	}
	retryResult, retryReceipt, err := retry.Apply(context.Background(), Request{Manifest: manifest})
	if err != nil || !retryResult.Ready() {
		t.Fatalf("retry Apply() = ready %v err %v", retryResult.Ready(), err)
	}
	if len(retryReceipt.AbortedSteps()) != 0 {
		t.Fatalf("retry cleanup = %#v", retryReceipt.AbortedSteps())
	}
	if retryTarget.HasArtifact("record_evidence") {
		t.Fatal("retry resurrected purged evidence")
	}
}

func TestRecoveryKeepsPermanentDeleteDisabledWhenExactRowsMissing(t *testing.T) {
	t.Parallel()

	registry, err := recordreadiness.NewRegistry(recordreadiness.RegistryInput{})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	matrix, err := registry.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if matrix.PermanentDelete() != recordreadiness.PermanentDeleteDisabled {
		t.Fatalf("incomplete recovery matrix enabled permanent delete")
	}
	encoded, err := matrix.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"permanent_delete":"disabled"`)) {
		t.Fatalf("Encode() missing disabled decision: %s", encoded)
	}
}

func TestExternalCopyDisclosureIsContentSafe(t *testing.T) {
	t.Parallel()

	encoded, err := EncodeExternalCopies([]recorddeletion.SurvivingCopySummary{{
		Scope:     recorddeletion.AdapterNameRecordPortability,
		Kind:      recorddeletion.SurvivingCopyKindDeliveredExport,
		CopyCount: 1,
	}})
	if err != nil {
		t.Fatalf("EncodeExternalCopies() error = %v", err)
	}
	text := string(encoded)
	for _, leaked := range []string{"# title", "postgres://", "password=secret", "filename.md", `"note"`} {
		if bytes.Contains(encoded, []byte(leaked)) {
			t.Fatalf("EncodeExternalCopies() leaked %q: %s", leaked, text)
		}
	}
	if !bytes.Contains(encoded, []byte(`"kind":"delivered_export"`)) || !bytes.Contains(encoded, []byte(`"copy_count":1`)) {
		t.Fatalf("EncodeExternalCopies() missing allowlisted fields: %s", text)
	}
}

func mustRecoveryBackup(t *testing.T) (recordbackup.Manifest, *recordbackup.LocalStore) {
	t.Helper()
	store, err := recordbackup.NewLocalStore(t.TempDir())
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
	attachment, err := recordbackup.NewArtifactRef(
		"record_attachments", "blob.v1",
		sha256.Sum256([]byte("survivor-attachment")), uint64(len("survivor-attachment")),
		recordbackup.ClassificationObject,
	)
	if err != nil {
		t.Fatalf("NewArtifactRef(attachment) error = %v", err)
	}
	evidence, err := recordbackup.NewArtifactRef(
		"record_evidence", "evs.v1",
		sha256.Sum256([]byte("purged-evidence")), uint64(len("purged-evidence")),
		recordbackup.ClassificationObject,
	)
	if err != nil {
		t.Fatalf("NewArtifactRef(evidence) error = %v", err)
	}
	payloads := map[string][]byte{
		"postgres_dump":      []byte("database-artifact"),
		"record_attachments": []byte("survivor-attachment"),
		"record_evidence":    []byte("purged-evidence"),
	}
	backup, err := recordbackup.NewService(recordbackup.Options{
		Store:    store,
		Database: &restoreDatabaseStub{artifact: database, payload: payloads["postgres_dump"]},
		Objects: &keyedObjectStub{
			artifacts: []recordbackup.ArtifactRef{attachment, evidence},
			payloads:  payloads,
		},
		Build: restoreBuild(t),
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	manifest, _, err := backup.Create(context.Background(), recordbackup.Request{Profile: recordbackup.ProfileLocal})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return manifest, store
}

type keyedObjectStub struct {
	artifacts []recordbackup.ArtifactRef
	payloads  map[string][]byte
}

func (source *keyedObjectStub) List(context.Context) ([]recordbackup.ArtifactRef, error) {
	return append([]recordbackup.ArtifactRef(nil), source.artifacts...), nil
}

func (source *keyedObjectStub) Open(_ context.Context, artifact recordbackup.ArtifactRef) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(source.payloads[artifact.Kind()])), nil
}

type recoveryTarget struct {
	empty    bool
	fail     Step
	database []byte
	objects  map[string][]byte
}

func newRecoveryTarget() *recoveryTarget {
	return &recoveryTarget{empty: true, objects: map[string][]byte{}}
}

func (target *recoveryTarget) Empty(context.Context) (bool, error) { return target.empty, nil }

func (target *recoveryTarget) RestoreDatabase(_ context.Context, body io.Reader) error {
	if target.fail == StepRestoreDatabase {
		return errors.New("injected database restore failure")
	}
	payload, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	target.database = payload
	target.empty = false
	return nil
}

func (target *recoveryTarget) RestoreObject(_ context.Context, artifact recordbackup.ArtifactRef, body io.Reader) error {
	payload, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	target.objects[artifact.Kind()] = payload
	return nil
}

func (target *recoveryTarget) HasArtifact(kind string) bool {
	_, ok := target.objects[kind]
	return ok
}

func (target *recoveryTarget) drop(kind string) { delete(target.objects, kind) }

func (target *recoveryTarget) Serving(context.Context) bool { return false }

func (target *recoveryTarget) Workers(context.Context) bool { return false }

type droppingReplay struct {
	target *recoveryTarget
	kind   string
	calls  int
}

func (adapter *droppingReplay) Kind() recordreadiness.CapabilityKind {
	return recordreadiness.CapabilityRecoveryRecordEvidence
}

func (adapter *droppingReplay) ReplayDeletions(context.Context, recordbackup.DeletionWatermark) error {
	adapter.calls++
	adapter.target.drop(adapter.kind)
	return nil
}

type recoveryProjectionStub struct {
	target *recoveryTarget
	saw    map[string]bool
	search int
}

func (stub *recoveryProjectionStub) RebuildSearch(context.Context) error {
	stub.search++
	stub.saw = map[string]bool{}
	for kind := range stub.target.objects {
		stub.saw[kind] = true
	}
	return nil
}

func (stub *recoveryProjectionStub) RebuildActivity(context.Context) error { return nil }
