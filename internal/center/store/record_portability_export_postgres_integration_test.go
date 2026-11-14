package store_test

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/portability"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

// This exercises the actual consumer, record authorization, direct-runtime
// PostgreSQL repository and existing managed staging path. It does not claim
// independent physical-drain or issued-deletion acceptance.
func TestPostgresIntegrationExportConsumerWorkflow(t *testing.T) {
	ctx, fixture := newImportGCTestFixture(t, attachments.BackendKindLocal, false)
	parent, actor := store.SeedExportWorkflowRecord(t, ctx, fixture)
	witness, err := store.NewWitnessedRecordSubjectTombstoneReader("", recordauth.ProjectIDDefault, nil, fixture.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		store.NewVPSRecordSubjectAdapter(store.NewPostgresVPSAssetRepository(fixture.Runtime)),
		store.NewMonitoringInstanceRecordSubjectAdapter(store.NewPostgresMonitoringInstanceRepository(fixture.Runtime)),
		store.NewTargetRecordSubjectAdapter(store.NewPostgresTargetRepository(fixture.Runtime)),
	})
	if err != nil {
		t.Fatal(err)
	}
	authorization := store.NewPostgresCurrentRecordAuthorizationSource(
		fixture.Runtime, store.NewRecordSubjectReadResolver(subjects, witness), fixture.Gate,
	)
	repository, err := store.NewPostgresRecordRepository(fixture.Runtime, fixture.Gate, nil, authorization, nil)
	if err != nil {
		t.Fatal(err)
	}
	read, err := records.NewRecordReadService(authorization, authorization, repository)
	if err != nil {
		t.Fatal(err)
	}
	birth, err := recordbirth.NewCoordinator(store.NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := records.NewRevisionService(subjects, authorization, repository, birth)
	if err != nil {
		t.Fatal(err)
	}
	application, err := records.NewApplication(read, revisions, importDestinationLifecycleStub{}, importDestinationDraftStub{}, records.ApplicationOptions{
		IdempotencyOwnerID: "export_workflow_test", OwnerLeaseDuration: time.Minute, IdempotencyTTL: time.Hour, OutboxTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs := store.NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, witness, nil)
	service, err := portability.NewService(portability.Options{
		Enabled: true, BackendKind: string(attachments.BackendKindLocal), Documents: application, Jobs: jobs,
		Staging: portability.NewLeasedBlobStore(fixture.Blobs, fixture.Writer),
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.Preview(ctx, portability.PreviewRequest{
		Actor: actor, RecordID: parent.RecordID, IdempotencyKey: "export-real-workflow",
		ExportKind: portability.ExportKindMarkdown, ExportMode: portability.ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("preview real export: %v", err)
	}
	created, err := service.Create(ctx, portability.CreateRequest{
		Actor: actor, PreviewID: preview.PreviewID, PreviewToken: preview.PreviewToken, InventoryDigest: preview.InventoryDigest,
	})
	if err != nil || created.ExportID != preview.PreviewID || created.JobState != store.RecordExportJobStatePublished {
		t.Fatalf("create real export = %#v, %v", created, err)
	}
	content, err := service.OpenContent(ctx, actor, created.ExportID)
	if err != nil {
		t.Fatalf("open real export: %v", err)
	}
	defer content.Body.Close()
	payload, err := io.ReadAll(content.Body)
	if err != nil || content.MediaType != "text/markdown" || int64(len(payload)) != content.ByteSize ||
		!strings.Contains(string(payload), "Export workflow") {
		t.Fatalf("read real export: media=%q size=%d bytes=%d err=%v", content.MediaType, content.ByteSize, len(payload), err)
	}
	if err := content.Body.Close(); err != nil {
		t.Fatal(err)
	}
	blocked, err := service.Preview(ctx, portability.PreviewRequest{
		Actor: actor, RecordID: parent.RecordID, IdempotencyKey: "export-real-fenced",
		ExportKind: portability.ExportKindMarkdown, ExportMode: portability.ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("prepare export before fence: %v", err)
	}
	fences := store.NewPostgresRecordPlatformRepository(fixture.Runtime, fixture.Gate)
	object := recordplatform.ObjectRef{ProjectID: string(recordplatform.ProjectIDDefault), ObjectKind: "record", ObjectID: parent.RecordID}
	lease, err := fences.AcquireDeletionFenceLease(ctx, object, recordplatform.LeaseClaimInputV1{
		OwnerID: "export_consumer_fence", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("acquire consumer deletion fence: %v", err)
	}
	_, err = service.Create(ctx, portability.CreateRequest{
		Actor: actor, PreviewID: blocked.PreviewID, PreviewToken: blocked.PreviewToken, InventoryDigest: blocked.InventoryDigest,
	})
	if !errors.Is(err, portability.ErrExportUnavailable) {
		t.Fatalf("create after deletion fence = %v", err)
	}
	job, err := jobs.LoadExportJob(ctx, blocked.PreviewID)
	if err != nil || job.JobState != store.RecordExportJobStatePreviewed {
		t.Fatalf("fenced consumer job = %#v, %v", job, err)
	}
	if _, err := jobs.LoadExportArtifact(ctx, blocked.PreviewID); !errors.Is(err, store.ErrRecordExportNotFound) {
		t.Fatalf("fenced consumer artifact lookup = %v", err)
	}
	if err := fences.ReleaseDeletionFenceLease(ctx, object, lease.Owner); err != nil {
		t.Fatalf("release consumer deletion fence: %v", err)
	}
}
