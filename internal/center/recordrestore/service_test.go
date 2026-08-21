package recordrestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"houfeng/internal/center/recordbackup"
	"houfeng/internal/center/recordreadiness"
)

func TestRestorePlanPerformsNoWritesAndListsExactSteps(t *testing.T) {
	t.Parallel()

	target := &targetStub{empty: true}
	service, err := NewService(fixtureOptions(t, target, nil))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	plan, err := service.Plan(context.Background(), Request{Manifest: fixtureManifest(t)})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !reflect.DeepEqual(plan.Steps(), requiredSteps()) {
		t.Fatalf("Plan() steps = %#v, want %#v", plan.Steps(), requiredSteps())
	}
	if target.writes != 0 || target.serving || target.workers {
		t.Fatalf("Plan() mutated target: writes=%d serving=%v workers=%v", target.writes, target.serving, target.workers)
	}
}

func TestRestoreApplyRejectsUnsafeTargetsAndContracts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options func(*testing.T, *targetStub) Options
		want    error
	}{
		{
			name: "non-empty target",
			options: func(t *testing.T, target *targetStub) Options {
				target.empty = false
				return fixtureOptions(t, target, nil)
			},
			want: ErrTargetNotEmpty,
		},
		{
			name: "incompatible build",
			options: func(t *testing.T, target *targetStub) Options {
				options := fixtureOptions(t, target, nil)
				options.Current.Version = "0.0.0"
				return options
			},
			want: ErrIncompatibleRestore,
		},
		{
			name: "missing artifact",
			options: func(t *testing.T, target *targetStub) Options {
				return fixtureOptions(t, target, errors.New("object missing"))
			},
			want: ErrMissingArtifact,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := &targetStub{empty: true}
			service, err := NewService(tt.options(t, target))
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			result, receipt, err := service.Apply(context.Background(), Request{Manifest: fixtureManifest(t)})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Apply() error = %v, want %v", err, tt.want)
			}
			if result.Ready() {
				t.Fatal("rejected Apply() reported ready")
			}
			if target.serving || target.workers {
				t.Fatal("rejected Apply() started serving or workers")
			}
			if tt.want != ErrTargetNotEmpty && receipt.ReleasedWorkspaces() == 0 && len(receipt.AbortedSteps()) == 0 {
				t.Fatalf("rejected Apply() empty cleanup: %+v", receipt)
			}
		})
	}
}

func TestRestoreApplyReplaysDeletionsBeforeProjectionRebuild(t *testing.T) {
	t.Parallel()

	target := &targetStub{empty: true}
	clock := &stepClock{}
	rebuilder := &projectionStub{clock: clock}
	replay := &replayStub{kind: recordreadiness.CapabilityRecoveryRecordCore, clock: clock}
	options := fixtureOptions(t, target, nil)
	options.Replay = []ReplayAdapter{replay}
	options.Projections = rebuilder
	service, err := NewService(options)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	result, receipt, err := service.Apply(context.Background(), Request{Manifest: fixtureManifest(t)})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if !result.Ready() {
		t.Fatal("Apply() ready = false")
	}
	if len(receipt.AbortedSteps()) != 0 {
		t.Fatalf("successful Apply() cleanup = %#v", receipt.AbortedSteps())
	}
	if !reflect.DeepEqual(result.Steps(), requiredSteps()) {
		t.Fatalf("Apply() steps = %#v", result.Steps())
	}
	if replay.calls == 0 {
		t.Fatal("Apply() did not replay deletions")
	}
	if rebuilder.search == 0 || rebuilder.activity == 0 {
		t.Fatal("Apply() did not rebuild projections")
	}
	if replay.lastSeq >= rebuilder.firstSeq && rebuilder.firstSeq != 0 {
		// order is tracked on a shared clock
	}
	if replay.order >= rebuilder.searchOrder || rebuilder.searchOrder >= rebuilder.activityOrder {
		t.Fatalf("replay/rebuild order = replay %d search %d activity %d", replay.order, rebuilder.searchOrder, rebuilder.activityOrder)
	}
	if target.serving || target.workers {
		t.Fatal("Apply() enabled serving or workers before verify")
	}
}

func TestRestoreApplyFailureCutpointsKeepTargetUnready(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fail Step
	}{
		{name: "restore database", fail: StepRestoreDatabase},
		{name: "replay deletions", fail: StepReplayDeletions},
		{name: "rebuild search", fail: StepRebuildSearch},
		{name: "converge acl", fail: StepConvergeACL},
		{name: "publish readiness", fail: StepPublishReadiness},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			target := &targetStub{empty: true, fail: tt.fail}
			clock := &stepClock{}
			rebuilder := &projectionStub{fail: tt.fail, clock: clock}
			replay := &replayStub{kind: recordreadiness.CapabilityRecoveryRecordCore, fail: tt.fail, clock: clock}
			acl := &aclStub{fail: tt.fail}
			ready := &readinessStub{fail: tt.fail}
			options := fixtureOptions(t, target, nil)
			options.Replay = []ReplayAdapter{replay}
			options.Projections = rebuilder
			options.ACL = acl
			options.Readiness = ready
			service, err := NewService(options)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			result, receipt, err := service.Apply(context.Background(), Request{Manifest: fixtureManifest(t)})
			if err == nil {
				t.Fatal("Apply() error = nil")
			}
			if !errors.Is(err, ErrRestoreIncomplete) && !errors.Is(err, ErrRestoreCleanupRequired) && !errors.Is(err, ErrRestoreNotReady) {
				t.Fatalf("Apply() error = %v", err)
			}
			if result.Ready() {
				t.Fatal("failed Apply() reported ready")
			}
			if target.serving || target.workers {
				t.Fatal("failed Apply() started serving or workers")
			}
			if receipt.ReleasedWorkspaces() == 0 && len(receipt.AbortedSteps()) == 0 {
				t.Fatalf("failed Apply() empty cleanup: %+v", receipt)
			}
		})
	}
}

func TestRestoreVerifyRequiresReadyIsolatedTarget(t *testing.T) {
	t.Parallel()

	target := &targetStub{empty: true}
	service, err := NewService(fixtureOptions(t, target, nil))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	manifest := fixtureManifest(t)
	if _, _, err := service.Apply(context.Background(), Request{Manifest: manifest}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if err := service.Verify(context.Background(), Request{Manifest: manifest}); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func fixtureOptions(t *testing.T, target Target, openErr error) Options {
	t.Helper()
	manifest := fixtureManifest(t)
	return Options{
		Target:      target,
		Source:      &sourceStub{manifest: manifest, err: openErr},
		Replay:      []ReplayAdapter{&replayStub{kind: recordreadiness.CapabilityRecoveryRecordCore, clock: &stepClock{}}},
		Projections: &projectionStub{clock: &stepClock{}},
		ACL:         &aclStub{},
		Verifier:    &verifierStub{},
		Readiness:   &readinessStub{},
		Current: recordbackup.BuildIdentity{
			Commit:          "6a37448ddeadbeef",
			Version:         "0.73.1",
			MigrationDigest: sha256.Sum256([]byte("migration-digest-fixture")),
			AppACLDigest:    sha256.Sum256([]byte("app-acl-digest-fixture")),
			Adapters:        fixtureAdapters(t),
			Deletion:        fixtureDeletion(t),
			Profile:         recordbackup.ProfileLocal,
		},
	}
}

func fixtureManifest(t *testing.T) recordbackup.Manifest {
	t.Helper()
	database, err := recordbackup.NewArtifactRef(
		"postgres_dump",
		"db.v1",
		sha256.Sum256([]byte("database-artifact")),
		uint64(len("database-artifact")),
		recordbackup.ClassificationDatabase,
	)
	if err != nil {
		t.Fatalf("NewArtifactRef(database) error = %v", err)
	}
	object, err := recordbackup.NewArtifactRef(
		"record_attachments",
		"blob.v1",
		sha256.Sum256([]byte("object-artifact")),
		uint64(len("object-artifact")),
		recordbackup.ClassificationObject,
	)
	if err != nil {
		t.Fatalf("NewArtifactRef(object) error = %v", err)
	}
	manifest, err := recordbackup.NewManifest(recordbackup.ManifestInput{
		BuildCommit:     "6a37448ddeadbeef",
		BuildVersion:    "0.73.1",
		MigrationDigest: sha256.Sum256([]byte("migration-digest-fixture")),
		AppACLDigest:    sha256.Sum256([]byte("app-acl-digest-fixture")),
		Adapters:        fixtureAdapters(t),
		Database:        database,
		Objects:         []recordbackup.ArtifactRef{object},
		Deletion:        fixtureDeletion(t),
		CreatedAt:       time.Unix(1_700_000_000, 0).UTC(),
		Profile:         recordbackup.ProfileLocal,
	})
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	return manifest
}

func fixtureAdapters(t *testing.T) []recordbackup.AdapterRef {
	t.Helper()
	adapters := make([]recordbackup.AdapterRef, 0, len(recordreadiness.RequiredCapabilityKinds()))
	for _, kind := range recordreadiness.RequiredCapabilityKinds() {
		ref, err := recordbackup.NewAdapterRef(kind, recordbackup.CapabilityContractVersionV1)
		if err != nil {
			t.Fatalf("NewAdapterRef(%q) error = %v", kind, err)
		}
		adapters = append(adapters, ref)
	}
	return adapters
}

func fixtureDeletion(t *testing.T) recordbackup.DeletionWatermark {
	t.Helper()
	mark, err := recordbackup.NewDeletionWatermark(7, sha256.Sum256([]byte("deletion-watermark")))
	if err != nil {
		t.Fatalf("NewDeletionWatermark() error = %v", err)
	}
	return mark
}

type targetStub struct {
	empty   bool
	fail    Step
	writes  int
	serving bool
	workers bool
}

func (target *targetStub) Empty(context.Context) (bool, error) { return target.empty, nil }

func (target *targetStub) RestoreDatabase(context.Context, io.Reader) error {
	target.writes++
	if target.fail == StepRestoreDatabase {
		return errors.New("injected database restore failure")
	}
	return nil
}

func (target *targetStub) RestoreObject(context.Context, recordbackup.ArtifactRef, io.Reader) error {
	target.writes++
	return nil
}

func (target *targetStub) Serving(context.Context) bool { return target.serving }

func (target *targetStub) Workers(context.Context) bool { return target.workers }

type sourceStub struct {
	manifest recordbackup.Manifest
	err      error
}

func (source *sourceStub) Open(_ context.Context, artifact recordbackup.ArtifactRef) (io.ReadCloser, error) {
	if source.err != nil {
		return nil, source.err
	}
	switch artifact.Classification() {
	case recordbackup.ClassificationDatabase:
		return io.NopCloser(bytes.NewReader([]byte("database-artifact"))), nil
	default:
		return io.NopCloser(bytes.NewReader([]byte("object-artifact"))), nil
	}
}

type stepClock struct {
	mu sync.Mutex
	n  int
}

func (clock *stepClock) next() int {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.n++
	return clock.n
}

type replayStub struct {
	kind    recordreadiness.CapabilityKind
	fail    Step
	clock   *stepClock
	calls   int
	order   int
	lastSeq int
}

func (adapter *replayStub) Kind() recordreadiness.CapabilityKind { return adapter.kind }

func (adapter *replayStub) ReplayDeletions(context.Context, recordbackup.DeletionWatermark) error {
	adapter.calls++
	if adapter.clock != nil {
		adapter.order = adapter.clock.next()
	}
	if adapter.fail == StepReplayDeletions {
		return errors.New("injected replay failure")
	}
	return nil
}

type projectionStub struct {
	fail          Step
	clock         *stepClock
	search        int
	activity      int
	searchOrder   int
	activityOrder int
	firstSeq      int
}

func (stub *projectionStub) RebuildSearch(context.Context) error {
	stub.search++
	if stub.clock != nil {
		stub.searchOrder = stub.clock.next()
	}
	if stub.firstSeq == 0 {
		stub.firstSeq = stub.searchOrder
	}
	if stub.fail == StepRebuildSearch {
		return errors.New("injected search rebuild failure")
	}
	return nil
}

func (stub *projectionStub) RebuildActivity(context.Context) error {
	stub.activity++
	if stub.clock != nil {
		stub.activityOrder = stub.clock.next()
	}
	if stub.fail == StepRebuildActivity {
		return errors.New("injected activity rebuild failure")
	}
	return nil
}

type aclStub struct{ fail Step }

func (stub *aclStub) Converge(context.Context) error {
	if stub.fail == StepConvergeACL {
		return errors.New("injected acl failure")
	}
	return nil
}

type verifierStub struct{}

func (verifierStub) Verify(context.Context) error { return nil }

type readinessStub struct{ fail Step }

func (stub *readinessStub) Publish(context.Context, recordreadiness.StatusMatrix) error {
	if stub.fail == StepPublishReadiness {
		return errors.New("injected readiness failure")
	}
	return nil
}

func TestRestoreContentSafeErrors(t *testing.T) {
	t.Parallel()
	for _, leaked := range []string{"postgres://", "password=secret", "# title", "filename.md"} {
		if strings.Contains(ErrTargetNotEmpty.Error(), leaked) {
			t.Fatalf("sentinel leaked %q", leaked)
		}
	}
}
