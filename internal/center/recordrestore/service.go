package recordrestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"reflect"

	"houfeng/internal/center/recordbackup"
	"houfeng/internal/center/recordreadiness"
)

func NewService(options Options) (*Service, error) {
	if nilRestoreDep(options.Target) || nilRestoreDep(options.Source) ||
		nilRestoreDep(options.Projections) || nilRestoreDep(options.ACL) ||
		nilRestoreDep(options.Verifier) || nilRestoreDep(options.Readiness) {
		return nil, fmt.Errorf("%w: dependencies", ErrInvalidRestoreRequest)
	}
	if options.Current.Commit == "" || options.Current.Version == "" ||
		options.Current.MigrationDigest == ([sha256.Size]byte{}) ||
		options.Current.AppACLDigest == ([sha256.Size]byte{}) {
		return nil, fmt.Errorf("%w: current build", ErrInvalidRestoreRequest)
	}
	for _, adapter := range options.Replay {
		if nilRestoreDep(adapter) {
			return nil, fmt.Errorf("%w: replay adapter", ErrInvalidRestoreRequest)
		}
	}
	return &Service{options: options}, nil
}

type Service struct {
	options Options
	ready   bool
}

func (service *Service) Kind() recordreadiness.CapabilityKind {
	return recordreadiness.CapabilityRestoreReplay
}

func (service *Service) Version() uint32 {
	return recordbackup.CapabilityContractVersionV1
}

func (service *Service) Health(ctx context.Context) error {
	if ctx == nil || service == nil || nilRestoreDep(service.options.Target) {
		return ErrRestoreUnavailable
	}
	return nil
}

func (service *Service) Plan(ctx context.Context, request Request) (Plan, error) {
	if err := service.validateManifest(ctx, request.Manifest); err != nil {
		return Plan{}, err
	}
	return Plan{steps: requiredSteps()}, nil
}

func (service *Service) Apply(ctx context.Context, request Request) (Result, CleanupReceipt, error) {
	if ctx == nil || service == nil {
		return Result{}, CleanupReceipt{}, ErrRestoreUnavailable
	}
	service.ready = false
	done := make([]Step, 0, len(requiredSteps()))

	empty, err := service.options.Target.Empty(ctx)
	if err != nil {
		return Result{}, service.cleanup(done), fmt.Errorf("%w: empty", ErrRestoreUnavailable)
	}
	done = append(done, StepValidateTarget)
	if !empty {
		return Result{steps: done}, CleanupReceipt{}, ErrTargetNotEmpty
	}

	if err := service.validateManifest(ctx, request.Manifest); err != nil {
		return Result{steps: done}, service.cleanup(done), err
	}
	done = append(done, StepVerifyManifest)

	database, objects, err := service.stageBytes(ctx, request.Manifest)
	if err != nil {
		return Result{steps: done}, service.cleanup(done), err
	}
	done = append(done, StepStageBytes)

	if err := service.options.Target.RestoreDatabase(ctx, bytes.NewReader(database)); err != nil {
		return Result{steps: done}, service.cleanup(done), fmt.Errorf("%w: database", ErrRestoreIncomplete)
	}
	done = append(done, StepRestoreDatabase)

	for _, object := range objects {
		if err := service.options.Target.RestoreObject(ctx, object.ref, bytes.NewReader(object.payload)); err != nil {
			return Result{steps: done}, service.cleanup(done), fmt.Errorf("%w: object", ErrRestoreIncomplete)
		}
	}
	done = append(done, StepRestoreObjects)

	for _, adapter := range service.options.Replay {
		if err := adapter.ReplayDeletions(ctx, request.Manifest.Deletion()); err != nil {
			return Result{steps: done}, service.cleanup(done), fmt.Errorf("%w: replay", ErrRestoreIncomplete)
		}
	}
	done = append(done, StepReplayDeletions)
	if err := service.rejectResurrection(); err != nil {
		return Result{steps: done}, service.cleanup(done), err
	}

	if err := service.options.Projections.RebuildSearch(ctx); err != nil {
		return Result{steps: done}, service.cleanup(done), fmt.Errorf("%w: search", ErrRestoreIncomplete)
	}
	done = append(done, StepRebuildSearch)

	if err := service.options.Projections.RebuildActivity(ctx); err != nil {
		return Result{steps: done}, service.cleanup(done), fmt.Errorf("%w: activity", ErrRestoreIncomplete)
	}
	done = append(done, StepRebuildActivity)

	if err := service.options.ACL.Converge(ctx); err != nil {
		return Result{steps: done}, service.cleanup(done), fmt.Errorf("%w: acl", ErrRestoreIncomplete)
	}
	done = append(done, StepConvergeACL)

	if err := service.options.Verifier.Verify(ctx); err != nil {
		return Result{steps: done}, service.cleanup(done), fmt.Errorf("%w: verify", ErrRestoreIncomplete)
	}
	done = append(done, StepVerifyAdapters)

	if service.options.Target.Serving(ctx) || service.options.Target.Workers(ctx) {
		return Result{steps: done}, service.cleanup(done), ErrRestoreNotReady
	}
	if err := service.options.Readiness.Publish(ctx, recordreadiness.StatusMatrix{}); err != nil {
		return Result{steps: done}, service.cleanup(done), fmt.Errorf("%w: readiness", ErrRestoreNotReady)
	}
	done = append(done, StepPublishReadiness)
	service.ready = true
	return Result{ready: true, steps: done}, CleanupReceipt{}, nil
}

func (service *Service) Verify(ctx context.Context, request Request) error {
	if err := service.validateManifest(ctx, request.Manifest); err != nil {
		return err
	}
	if !service.ready || service.options.Target.Serving(ctx) || service.options.Target.Workers(ctx) {
		return ErrRestoreNotReady
	}
	return service.options.Verifier.Verify(ctx)
}

func (service *Service) rejectResurrection() error {
	if len(service.options.PurgedKinds) == 0 {
		return nil
	}
	inspector, ok := service.options.Target.(ArtifactPresence)
	if !ok {
		return ErrResurrectionBlocked
	}
	for _, kind := range service.options.PurgedKinds {
		if inspector.HasArtifact(kind) {
			return ErrResurrectionBlocked
		}
	}
	return nil
}

func (service *Service) validateManifest(ctx context.Context, manifest recordbackup.Manifest) error {
	if ctx == nil || service == nil {
		return ErrRestoreUnavailable
	}
	current := service.options.Current
	if manifest.Format() != recordbackup.ManifestFormatV1 ||
		manifest.BuildCommit() != current.Commit ||
		manifest.BuildVersion() != current.Version ||
		manifest.MigrationDigest() != current.MigrationDigest ||
		manifest.AppACLDigest() != current.AppACLDigest {
		return ErrIncompatibleRestore
	}
	return nil
}

type stagedObject struct {
	ref     recordbackup.ArtifactRef
	payload []byte
}

func (service *Service) stageBytes(ctx context.Context, manifest recordbackup.Manifest) ([]byte, []stagedObject, error) {
	database, err := service.readArtifact(ctx, manifest.Database())
	if err != nil {
		return nil, nil, err
	}
	objects := make([]stagedObject, 0, len(manifest.Objects()))
	for _, object := range manifest.Objects() {
		payload, err := service.readArtifact(ctx, object)
		if err != nil {
			return nil, nil, err
		}
		objects = append(objects, stagedObject{ref: object, payload: payload})
	}
	return database, objects, nil
}

func (service *Service) readArtifact(ctx context.Context, artifact recordbackup.ArtifactRef) ([]byte, error) {
	reader, err := service.options.Source.Open(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMissingArtifact, err)
	}
	defer reader.Close()
	payload, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: read", ErrRestoreIncomplete)
	}
	if sha256.Sum256(payload) != artifact.Digest() {
		return nil, fmt.Errorf("%w: digest", ErrRestoreIncomplete)
	}
	return payload, nil
}

func (service *Service) cleanup(done []Step) CleanupReceipt {
	aborted := make([]Step, 0)
	remaining := requiredSteps()[len(done):]
	aborted = append(aborted, remaining...)
	return CleanupReceipt{abortedSteps: aborted, workspaces: 1}
}

func nilRestoreDep(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
