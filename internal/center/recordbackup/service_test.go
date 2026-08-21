package recordbackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordreadiness"
)

func TestCanonicalManifestEncodeIsDeterministicAndContentSafe(t *testing.T) {
	t.Parallel()

	first, err := NewManifest(fixtureManifestInput(t, ProfileLocal))
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	second, err := NewManifest(fixtureManifestInput(t, ProfileLocal))
	if err != nil {
		t.Fatalf("NewManifest() second error = %v", err)
	}
	left, err := first.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	right, err := second.Encode()
	if err != nil {
		t.Fatalf("Encode() second error = %v", err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("Encode() is not deterministic:\n%s\n%s", left, right)
	}
	if !json.Valid(left) {
		t.Fatalf("Encode() is not JSON: %s", left)
	}
	text := string(left)
	for _, leaked := range []string{
		"# title",
		"comment body",
		"evidence payload",
		"attachment bytes",
		"archive content",
		"password=secret",
		"postgres://",
		"DATABASE_URL",
		"houfeng:secret",
		`"note"`,
		"filename.md",
		"record.md",
	} {
		if strings.Contains(text, leaked) {
			t.Fatalf("Encode() leaked %q: %s", leaked, text)
		}
	}
	for _, required := range []string{
		`"format":"houfeng-record-backup/v1"`,
		`"min_reader_version":1`,
		`"profile":"local"`,
		`"completion_digest"`,
		`"migration_digest"`,
		`"app_acl_digest"`,
		`"adapters"`,
		`"database"`,
		`"objects"`,
		`"deletion"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Encode() missing %s: %s", required, text)
		}
	}
}

func TestDecodeManifestRejectsUnknownVersionAndTamperedDigest(t *testing.T) {
	t.Parallel()

	manifest, err := NewManifest(fixtureManifestInput(t, ProfileLocal))
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	encoded, err := manifest.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	tests := []struct {
		name    string
		payload []byte
		want    error
	}{
		{
			name:    "unknown format",
			payload: []byte(`{"format":"houfeng-record-backup/v2","min_reader_version":1}`),
			want:    ErrUnknownManifestVersion,
		},
		{
			name:    "future reader",
			payload: []byte(`{"format":"houfeng-record-backup/v1","min_reader_version":99}`),
			want:    ErrUnknownManifestVersion,
		},
		{
			name:    "tampered completion digest",
			payload: tamperJSONField(t, encoded, "completion_digest"),
			want:    ErrTamperedManifest,
		},
		{
			name:    "tampered database digest",
			payload: tamperJSONField(t, encoded, "digest"),
			want:    ErrTamperedManifest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := DecodeManifest(tt.payload)
			if !errors.Is(err, tt.want) {
				t.Fatalf("DecodeManifest() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPlanPerformsNoWrites(t *testing.T) {
	t.Parallel()

	store := &artifactStoreStub{}
	service, err := NewService(fixtureOptions(t, store, ProfileLocal))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	plan, err := service.Plan(context.Background(), Request{Profile: ProfileLocal})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got := len(plan.Artifacts()); got == 0 {
		t.Fatal("Plan() artifacts = 0")
	}
	if store.stages != 0 || store.publishes != 0 || store.aborts != 0 || store.multipart != 0 || store.pins != 0 || store.workspaces != 0 {
		t.Fatalf("Plan() performed writes: %+v", store)
	}
}

func TestCreatePublishesManifestOnlyAfterDurableSuccess(t *testing.T) {
	t.Parallel()

	store := &artifactStoreStub{}
	service, err := NewService(fixtureOptions(t, store, ProfileLocal))
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	manifest, receipt, err := service.Create(context.Background(), Request{Profile: ProfileLocal})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if manifest.Format() != ManifestFormatV1 {
		t.Fatalf("Create() format = %q", manifest.Format())
	}
	if manifest.CompletionDigest() == ([sha256.Size]byte{}) {
		t.Fatal("Create() missing completion digest")
	}
	if store.publishes == 0 || store.manifestPublishes != 1 {
		t.Fatalf("Create() publish counts = %+v", store)
	}
	if store.manifestPublishes != 0 && store.lastPublishKind != "manifest" {
		t.Fatalf("Create() last publish = %q, want manifest", store.lastPublishKind)
	}
	if len(receipt.AbortedArtifacts()) != 0 {
		t.Fatalf("successful Create() cleanup = %#v", receipt.AbortedArtifacts())
	}
	if err := service.Verify(context.Background(), manifest); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestCreateFailureCutpointsEmitCleanupReceipts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fail failPoint
	}{
		{name: "after database stage", fail: failAfterDatabaseStage},
		{name: "after object stage", fail: failAfterObjectStage},
		{name: "multipart complete", fail: failMultipart},
		{name: "pin release", fail: failPin},
		{name: "before manifest publish", fail: failBeforeManifestPublish},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := &artifactStoreStub{fail: tt.fail}
			service, err := NewService(fixtureOptions(t, store, ProfileS3))
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			manifest, receipt, err := service.Create(context.Background(), Request{Profile: ProfileS3})
			if err == nil {
				t.Fatal("Create() error = nil, want cleanup failure")
			}
			if !errors.Is(err, ErrBackupCleanupRequired) && !errors.Is(err, ErrBackupIncomplete) {
				t.Fatalf("Create() error = %v, want cleanup or incomplete", err)
			}
			if manifest.Format() != "" {
				t.Fatalf("failed Create() published format %q", manifest.Format())
			}
			if store.manifestPublishes != 0 {
				t.Fatal("failed Create() published manifest")
			}
			if len(receipt.AbortedArtifacts()) == 0 && receipt.AbortedMultipart() == 0 &&
				receipt.ReleasedPins() == 0 && receipt.ReleasedWorkspaces() == 0 {
				t.Fatalf("failed Create() empty cleanup receipt: %+v", receipt)
			}
		})
	}
}

func TestLocalAndS3ShareManifestConformance(t *testing.T) {
	t.Parallel()

	for _, profile := range []Profile{ProfileLocal, ProfileS3} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			store := &artifactStoreStub{}
			service, err := NewService(fixtureOptions(t, store, profile))
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			manifest, _, err := service.Create(context.Background(), Request{Profile: profile})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			encoded, err := manifest.Encode()
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			decoded, err := DecodeManifest(encoded)
			if err != nil {
				t.Fatalf("DecodeManifest() error = %v", err)
			}
			if decoded.Format() != ManifestFormatV1 || decoded.Profile() != profile {
				t.Fatalf("decoded format/profile = %q %q", decoded.Format(), decoded.Profile())
			}
			if !strings.Contains(string(encoded), `"profile":"`+string(profile)+`"`) {
				t.Fatalf("Encode() missing profile %q: %s", profile, encoded)
			}
		})
	}
}

func fixtureOptions(t *testing.T, store ArtifactStore, profile Profile) Options {
	t.Helper()
	input := fixtureManifestInput(t, profile)
	return Options{
		Store:    store,
		Database: &databaseSourceStub{artifact: input.Database, payload: []byte("database-artifact")},
		Objects:  &objectInventoryStub{artifacts: input.Objects, payload: []byte("object-artifact")},
		Now:      func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		Build: BuildIdentity{
			Commit:          input.BuildCommit,
			Version:         input.BuildVersion,
			MigrationDigest: input.MigrationDigest,
			AppACLDigest:    input.AppACLDigest,
			Adapters:        input.Adapters,
			Deletion:        input.Deletion,
			Profile:         profile,
		},
	}
}

func fixtureManifestInput(t *testing.T, profile Profile) ManifestInput {
	t.Helper()
	migration := sha256.Sum256([]byte("migration-digest-fixture"))
	acl := sha256.Sum256([]byte("app-acl-digest-fixture"))
	databaseDigest := sha256.Sum256([]byte("database-artifact"))
	objectDigest := sha256.Sum256([]byte("object-artifact"))
	deletionDigest := sha256.Sum256([]byte("deletion-watermark"))
	database, err := NewArtifactRef("postgres_dump", "db.v1", databaseDigest, uint64(len("database-artifact")), ClassificationDatabase)
	if err != nil {
		t.Fatalf("NewArtifactRef(database) error = %v", err)
	}
	object, err := NewArtifactRef("record_attachments", "blob.v1", objectDigest, uint64(len("object-artifact")), ClassificationObject)
	if err != nil {
		t.Fatalf("NewArtifactRef(object) error = %v", err)
	}
	adapters := make([]AdapterRef, 0, len(recordreadiness.RequiredCapabilityKinds()))
	for _, kind := range recordreadiness.RequiredCapabilityKinds() {
		ref, err := NewAdapterRef(kind, CapabilityContractVersionV1)
		if err != nil {
			t.Fatalf("NewAdapterRef(%q) error = %v", kind, err)
		}
		adapters = append(adapters, ref)
	}
	deletion, err := NewDeletionWatermark(7, deletionDigest)
	if err != nil {
		t.Fatalf("NewDeletionWatermark() error = %v", err)
	}
	return ManifestInput{
		BuildCommit:     "6a37448ddeadbeef",
		BuildVersion:    "0.73.1",
		MigrationDigest: migration,
		AppACLDigest:    acl,
		Adapters:        adapters,
		Database:        database,
		Objects:         []ArtifactRef{object},
		Deletion:        deletion,
		CreatedAt:       time.Unix(1_700_000_000, 0).UTC(),
		Profile:         profile,
	}
}

func tamperJSONField(t *testing.T, encoded []byte, field string) []byte {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v payload=%s", err, encoded)
	}
	switch field {
	case "completion_digest":
		payload["completion_digest"] = strings.Repeat("0", 64)
	case "digest":
		database, _ := payload["database"].(map[string]any)
		if database == nil {
			t.Fatalf("tamper digest: database missing in %s", encoded)
		}
		database["digest"] = strings.Repeat("f", 64)
		payload["database"] = database
	}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return out
}

type failPoint int

const (
	failNone failPoint = iota
	failAfterDatabaseStage
	failAfterObjectStage
	failMultipart
	failPin
	failBeforeManifestPublish
)

type artifactStoreStub struct {
	fail              failPoint
	stages            int
	publishes         int
	aborts            int
	multipart         int
	pins              int
	workspaces        int
	manifestPublishes int
	lastPublishKind   string
	staged            []string
}

func (store *artifactStoreStub) Stage(_ context.Context, artifact ArtifactRef, body io.Reader) error {
	if _, err := io.Copy(io.Discard, body); err != nil {
		return err
	}
	store.stages++
	store.staged = append(store.staged, artifact.Kind())
	switch {
	case store.fail == failAfterDatabaseStage && artifact.Kind() == "postgres_dump":
		return errors.New("injected database stage failure")
	case store.fail == failAfterObjectStage && artifact.Classification() == ClassificationObject:
		return errors.New("injected object stage failure")
	case store.fail == failMultipart && artifact.Classification() == ClassificationObject:
		return errors.New("injected multipart failure")
	}
	return nil
}

func (store *artifactStoreStub) Publish(_ context.Context, artifact ArtifactRef) error {
	if store.fail == failBeforeManifestPublish && artifact.Kind() == "manifest" {
		return errors.New("injected manifest publish failure")
	}
	store.publishes++
	store.lastPublishKind = artifact.Kind()
	if artifact.Kind() == "manifest" {
		store.manifestPublishes++
	}
	return nil
}

func (store *artifactStoreStub) Abort(context.Context, ArtifactRef) error {
	store.aborts++
	return nil
}

func (store *artifactStoreStub) AbortMultipart(context.Context, ArtifactRef) error {
	store.multipart++
	if store.fail == failMultipart {
		return nil
	}
	return nil
}

func (store *artifactStoreStub) ReleasePin(context.Context, ArtifactRef) error {
	store.pins++
	if store.fail == failPin {
		return errors.New("injected pin failure")
	}
	return nil
}

func (store *artifactStoreStub) ReleaseWorkspace(context.Context) error {
	store.workspaces++
	return nil
}

type databaseSourceStub struct {
	artifact ArtifactRef
	payload  []byte
}

func (source *databaseSourceStub) Dump(context.Context) (io.ReadCloser, ArtifactRef, error) {
	return io.NopCloser(bytes.NewReader(source.payload)), source.artifact, nil
}

type objectInventoryStub struct {
	artifacts []ArtifactRef
	payload   []byte
}

func (source *objectInventoryStub) List(context.Context) ([]ArtifactRef, error) {
	return append([]ArtifactRef(nil), source.artifacts...), nil
}

func (source *objectInventoryStub) Open(context.Context, ArtifactRef) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(source.payload)), nil
}
