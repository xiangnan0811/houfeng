package recordbackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"reflect"
	"time"

	"houfeng/internal/center/recordreadiness"
)

func NewService(options Options) (*Service, error) {
	if nilBackupDep(options.Store) || nilBackupDep(options.Database) || nilBackupDep(options.Objects) {
		return nil, fmt.Errorf("%w: dependencies", ErrInvalidBackupRequest)
	}
	if options.Build.Commit == "" || options.Build.Version == "" ||
		options.Build.MigrationDigest == ([sha256.Size]byte{}) ||
		options.Build.AppACLDigest == ([sha256.Size]byte{}) ||
		(options.Build.Profile != ProfileLocal && options.Build.Profile != ProfileS3) ||
		len(options.Build.Adapters) == 0 ||
		options.Build.Deletion.sequence == 0 {
		return nil, fmt.Errorf("%w: build", ErrInvalidBackupRequest)
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store:    options.Store,
		database: options.Database,
		objects:  options.Objects,
		now:      now,
		build:    options.Build,
	}, nil
}

type Service struct {
	store    ArtifactStore
	database DatabaseSource
	objects  ObjectInventory
	now      func() time.Time
	build    BuildIdentity
}

func (service *Service) Kind() recordreadiness.CapabilityKind {
	return recordreadiness.CapabilityBackupOrchestration
}

func (service *Service) Version() uint32 { return CapabilityContractVersionV1 }

func (service *Service) Health(ctx context.Context) error {
	if ctx == nil || service == nil || nilBackupDep(service.store) {
		return ErrBackupUnavailable
	}
	return nil
}

func (service *Service) Plan(ctx context.Context, request Request) (Plan, error) {
	if err := service.validateRequest(ctx, request); err != nil {
		return Plan{}, err
	}
	reader, database, err := service.database.Dump(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: plan database", ErrBackupUnavailable)
	}
	if reader != nil {
		_ = reader.Close()
	}
	objects, err := service.objects.List(ctx)
	if err != nil {
		return Plan{}, fmt.Errorf("%w: plan objects", ErrBackupUnavailable)
	}
	return Plan{artifacts: append([]ArtifactRef{database}, objects...)}, nil
}

func (service *Service) Create(ctx context.Context, request Request) (Manifest, CleanupReceipt, error) {
	if err := service.validateRequest(ctx, request); err != nil {
		return Manifest{}, CleanupReceipt{}, err
	}

	staged := make([]ArtifactRef, 0, 4)
	reader, database, err := service.database.Dump(ctx)
	if err != nil {
		return Manifest{}, service.cleanup(ctx, staged), fmt.Errorf("%w: dump", ErrBackupIncomplete)
	}
	if err := service.stage(ctx, database, reader, &staged); err != nil {
		receipt := service.cleanup(ctx, staged)
		return Manifest{}, receipt, err
	}

	objects, err := service.objects.List(ctx)
	if err != nil {
		return Manifest{}, service.cleanup(ctx, staged), fmt.Errorf("%w: list objects", ErrBackupIncomplete)
	}
	for _, object := range objects {
		body, openErr := service.objects.Open(ctx, object)
		if openErr != nil {
			return Manifest{}, service.cleanup(ctx, staged), fmt.Errorf("%w: open object", ErrBackupIncomplete)
		}
		if err := service.stage(ctx, object, body, &staged); err != nil {
			return Manifest{}, service.cleanup(ctx, staged), err
		}
		if err := service.store.ReleasePin(ctx, object); err != nil {
			return Manifest{}, service.cleanup(ctx, staged), fmt.Errorf("%w: pin", ErrBackupCleanupRequired)
		}
	}

	for _, artifact := range staged {
		if err := service.store.Publish(ctx, artifact); err != nil {
			return Manifest{}, service.cleanup(ctx, staged), fmt.Errorf("%w: publish", ErrBackupIncomplete)
		}
	}

	manifest, err := NewManifest(ManifestInput{
		BuildCommit:     service.build.Commit,
		BuildVersion:    service.build.Version,
		MigrationDigest: service.build.MigrationDigest,
		AppACLDigest:    service.build.AppACLDigest,
		Adapters:        service.build.Adapters,
		Database:        database,
		Objects:         objects,
		Deletion:        service.build.Deletion,
		CreatedAt:       service.now(),
		Profile:         service.profile(request),
	})
	if err != nil {
		return Manifest{}, service.cleanup(ctx, staged), err
	}
	manifest, err = bindCompletion(manifest)
	if err != nil {
		return Manifest{}, service.cleanup(ctx, staged), err
	}
	encoded, err := manifest.Encode()
	if err != nil {
		return Manifest{}, service.cleanup(ctx, staged), err
	}
	manifestRef, err := NewArtifactRef(
		"manifest",
		"manifest.v1",
		sha256.Sum256(encoded),
		uint64(len(encoded)),
		ClassificationManifest,
	)
	if err != nil {
		return Manifest{}, service.cleanup(ctx, staged), err
	}
	if err := service.stage(ctx, manifestRef, io.NopCloser(bytes.NewReader(encoded)), &staged); err != nil {
		return Manifest{}, service.cleanup(ctx, staged), err
	}
	if err := service.store.Publish(ctx, manifestRef); err != nil {
		return Manifest{}, service.cleanup(ctx, staged), fmt.Errorf("%w: manifest", ErrBackupIncomplete)
	}
	return manifest, CleanupReceipt{}, nil
}

func (service *Service) Verify(ctx context.Context, manifest Manifest) error {
	if err := service.validateRequest(ctx, Request{Profile: manifest.profile}); err != nil {
		return err
	}
	encoded, err := manifest.Encode()
	if err != nil {
		return err
	}
	decoded, err := DecodeManifest(encoded)
	if err != nil {
		return err
	}
	if decoded.CompletionDigest() != manifest.CompletionDigest() {
		return ErrTamperedManifest
	}
	return nil
}

func (service *Service) stage(
	ctx context.Context,
	artifact ArtifactRef,
	body io.ReadCloser,
	staged *[]ArtifactRef,
) error {
	if body == nil {
		*staged = append(*staged, artifact)
		return fmt.Errorf("%w: empty artifact", ErrBackupIncomplete)
	}
	defer body.Close()
	payload, err := io.ReadAll(body)
	if err != nil {
		*staged = append(*staged, artifact)
		return fmt.Errorf("%w: read", ErrBackupIncomplete)
	}
	if sha256.Sum256(payload) != artifact.digest {
		*staged = append(*staged, artifact)
		return fmt.Errorf("%w: digest", ErrBackupIncomplete)
	}
	if err := service.store.Stage(ctx, artifact, bytes.NewReader(payload)); err != nil {
		*staged = append(*staged, artifact)
		if artifact.classification == ClassificationObject {
			_ = service.store.AbortMultipart(ctx, artifact)
		}
		return fmt.Errorf("%w: stage", ErrBackupIncomplete)
	}
	*staged = append(*staged, artifact)
	return nil
}

func (service *Service) cleanup(ctx context.Context, staged []ArtifactRef) CleanupReceipt {
	receipt := CleanupReceipt{}
	for _, artifact := range staged {
		_ = service.store.Abort(ctx, artifact)
		receipt.abortedArtifacts = append(receipt.abortedArtifacts, artifact.kind)
		if artifact.classification == ClassificationObject {
			_ = service.store.AbortMultipart(ctx, artifact)
			receipt.abortedMultipart++
			_ = service.store.ReleasePin(ctx, artifact)
			receipt.releasedPins++
		}
	}
	_ = service.store.ReleaseWorkspace(ctx)
	receipt.releasedWorkspaces++
	return receipt
}

func (service *Service) validateRequest(ctx context.Context, request Request) error {
	if ctx == nil || service == nil {
		return ErrBackupUnavailable
	}
	profile := service.profile(request)
	if profile != service.build.Profile {
		return fmt.Errorf("%w: profile", ErrInvalidBackupRequest)
	}
	return nil
}

func (service *Service) profile(request Request) Profile {
	if request.Profile == "" {
		return service.build.Profile
	}
	return request.Profile
}

func unixTime(value int64) time.Time {
	return time.Unix(value, 0).UTC()
}

func nilBackupDep(value any) bool {
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
