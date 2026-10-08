package portability

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

func TestPortabilityImportRestartMinIOPreservesStagedVersionAndPlanDigest(t *testing.T) {
	requireImportRestartMinIO(t)
	fixture := newImportRestartMinIOFixture(t)

	// A restarted service has neither the old Service cache nor the old lease.
	fixture.initial.staging.dropLease(fixture.job.ImportJobID)
	fixture.initial.mu.Lock()
	fixture.initial.importPlans = map[string]cachedImportPlan{}
	fixture.initial.mu.Unlock()

	delegate := &importWriterStub{repo: fixture.imports}
	writer := &importRestartDocumentImporter{delegate: delegate}
	restarted := newImportRestartMinIOService(t, fixture.imports, fixture.blob, writer)
	materialized, err := restarted.resolveImportPlan(context.Background(), fixture.preview.PlanID)
	if err != nil {
		t.Fatalf("resolveImportPlan() after service restart error = %v", err)
	}
	restarted.staging.mu.Lock()
	restartedVersion, leaseOK := attachments.ObjectVersion{}, false
	if lease := restarted.staging.leases[fixture.job.ImportJobID]; lease != nil {
		restartedVersion, leaseOK = lease.version, true
	}
	restarted.staging.mu.Unlock()
	expectedVersion := attachments.ObjectVersion{
		Key: fixture.artifact.BlobKey, VersionID: fixture.artifact.ObjectVersionID,
		SHA256: fixture.artifact.SHA256, SizeBytes: int64(fixture.artifact.ByteSize),
	}
	if !leaseOK || restartedVersion != expectedVersion {
		t.Fatalf("restarted staged version = %#v, want durable S3 version %#v", restartedVersion, expectedVersion)
	}
	if materialized.destination != fixture.destination {
		t.Fatalf("materialized destination = %#v, want canonical %#v", materialized.destination, fixture.destination)
	}
	if materialized.digest != fixture.plan.PlanDigest || materialized.digest == ([32]byte{}) {
		t.Fatalf("materialized v2 digest = %x, persisted plan digest = %x", materialized.digest, fixture.plan.PlanDigest)
	}
	if len(materialized.remaps) != len(fixture.plan.Remaps) {
		t.Fatalf("materialized remaps = %#v, persisted remaps = %#v", materialized.remaps, fixture.plan.Remaps)
	}
	for index, remap := range materialized.remaps {
		persisted := fixture.plan.Remaps[index]
		if remap.EntityKind != persisted.EntityKind || remap.SourceID != persisted.SourceID || remap.TargetID != persisted.TargetID {
			t.Fatalf("materialized remap[%d] = %#v, persisted = %#v", index, remap, persisted)
		}
	}
	if len(materialized.documents) != len(fixture.plan.Documents) || len(materialized.documents) != 1 {
		t.Fatalf("materialized documents = %#v, persisted = %#v", materialized.documents, fixture.plan.Documents)
	}
	if materialized.documents[0] != fixture.plan.Documents[0] {
		t.Fatalf("materialized document = %#v, persisted = %#v", materialized.documents[0], fixture.plan.Documents[0])
	}
	targetAttachmentID := importRestartRemapTarget(t, fixture.plan.Remaps, "attachment", importRestartSourceAttachmentID)
	if len(materialized.attachments) != 1 || materialized.attachments[0].TargetID != targetAttachmentID {
		t.Fatalf("materialized attachments = %#v, want target %q", materialized.attachments, targetAttachmentID)
	}
	if strings.Contains(materialized.documents[0].Body, importRestartSourceAttachmentID) ||
		!strings.Contains(materialized.documents[0].Body, targetAttachmentID) {
		t.Fatalf("materialized body did not preserve rebound attachment reference: %q", materialized.documents[0].Body)
	}

	applied, err := restarted.Apply(context.Background(), ApplyRequest{
		Actor: fixture.actor, PlanID: fixture.preview.PlanID, LockVersion: fixture.preview.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply() after service restart error = %v", err)
	}
	if applied.JobState != store.RecordImportJobStateApplied || len(applied.RecordIDs) != 1 || delegate.writes != 1 {
		t.Fatalf("Apply() = %#v, writes=%d, want one applied record and one write", applied, delegate.writes)
	}
	if len(writer.requests) != 1 || len(writer.requests[0].SubjectReferences) != 1 ||
		writer.requests[0].SubjectReferences[0] != fixture.destination {
		t.Fatalf("import request destination = %#v, want canonical %#v", writer.requests, fixture.destination)
	}
	if len(delegate.importedAtts) != 1 || len(delegate.importedAtts[0]) != 1 ||
		delegate.importedAtts[0][0].AttachmentID != targetAttachmentID {
		t.Fatalf("imported attachment = %#v, want rebound target %q", delegate.importedAtts, targetAttachmentID)
	}
	if delegate.lastFinish.OriginDigest != fixture.archiveDigest {
		t.Fatalf("origin digest = %x, want archive digest %x", delegate.lastFinish.OriginDigest, fixture.archiveDigest)
	}

	replayDelegate := &importWriterStub{repo: fixture.imports}
	replayWriter := &importRestartDocumentImporter{delegate: replayDelegate}
	replayService := newImportRestartMinIOService(t, fixture.imports, fixture.blob, replayWriter)
	replayed, err := replayService.Apply(context.Background(), ApplyRequest{
		Actor: fixture.actor, PlanID: fixture.preview.PlanID, LockVersion: fixture.preview.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply() replay after second service restart error = %v", err)
	}
	if replayed.PlanID != applied.PlanID || replayed.JobState != applied.JobState ||
		len(replayed.RecordIDs) != len(applied.RecordIDs) || replayed.RecordIDs[0] != applied.RecordIDs[0] {
		t.Fatalf("replay = %#v, first apply = %#v", replayed, applied)
	}
	if replayDelegate.writes != 0 || len(replayWriter.requests) != 0 {
		t.Fatalf("replay performed writes: writes=%d requests=%d", replayDelegate.writes, len(replayWriter.requests))
	}
}

func TestPortabilityImportRestartMinIORejectsAlteredDestinationBeforeWrite(t *testing.T) {
	requireImportRestartMinIO(t)
	fixture := newImportRestartMinIOFixture(t)
	alternate := fixture.destination
	alternate.SourceID = "tg_abcdef0123456789"

	fixture.imports.mu.Lock()
	job := fixture.imports.jobs[fixture.job.ImportJobID]
	job.DestinationSubject = alternate
	fixture.imports.jobs[job.ImportJobID] = job
	plan := fixture.imports.plans[fixture.preview.PlanID]
	plan.DestinationSubject = alternate
	fixture.imports.plans[fixture.preview.PlanID] = plan
	fixture.imports.mu.Unlock()

	rejectImportRestartMinIOApply(t, fixture, ErrInvalidArchive)
}

func TestPortabilityImportRestartMinIORejectsAlteredPlanDigestBeforeWrite(t *testing.T) {
	requireImportRestartMinIO(t)
	fixture := newImportRestartMinIOFixture(t)

	fixture.imports.mu.Lock()
	plan := fixture.imports.plans[fixture.preview.PlanID]
	plan.PlanDigest[0] ^= 0xff
	fixture.imports.plans[fixture.preview.PlanID] = plan
	fixture.imports.mu.Unlock()

	rejectImportRestartMinIOApply(t, fixture, ErrInvalidArchive)
}

func TestPortabilityImportRestartMinIORejectsAlteredArchiveDigestBeforeWrite(t *testing.T) {
	requireImportRestartMinIO(t)
	fixture := newImportRestartMinIOFixture(t)

	fixture.imports.mu.Lock()
	job := fixture.imports.jobs[fixture.job.ImportJobID]
	job.ArchiveDigest[0] ^= 0xff
	fixture.imports.jobs[job.ImportJobID] = job
	fixture.imports.mu.Unlock()

	rejectImportRestartMinIOApply(t, fixture, ErrInvalidArchive)
}

func TestPortabilityImportRestartMinIORejectsLegacyMissingDestinationBeforeRestage(t *testing.T) {
	requireImportRestartMinIO(t)
	fixture := newImportRestartMinIOFixture(t)

	fixture.imports.mu.Lock()
	job := fixture.imports.jobs[fixture.job.ImportJobID]
	job.DestinationSubject = records.SubjectReference{}
	fixture.imports.jobs[job.ImportJobID] = job
	plan := fixture.imports.plans[fixture.preview.PlanID]
	plan.DestinationSubject = records.SubjectReference{}
	fixture.imports.plans[fixture.preview.PlanID] = plan
	fixture.imports.mu.Unlock()
	restartDelegate := &importWriterStub{repo: fixture.imports}
	restartWriter := &importRestartDocumentImporter{delegate: restartDelegate}
	restarted := newImportRestartMinIOService(t, fixture.imports, fixture.blob, restartWriter)
	if _, err := restarted.DryRun(context.Background(), DryRunRequest{
		DestinationSubject: fixture.destination,
		Actor:              fixture.actor,
		IdempotencyKey:     "import-restart-minio",
		Archive:            fixture.archive,
	}); !errors.Is(err, ErrImportCASConflict) {
		t.Fatalf("DryRun() for legacy destination error = %v, want %v", err, ErrImportCASConflict)
	}
	if restartDelegate.writes != 0 || len(restartWriter.requests) != 0 {
		t.Fatalf("legacy DryRun wrote domain rows: writes=%d requests=%d", restartDelegate.writes, len(restartWriter.requests))
	}

	rejectImportRestartMinIOApply(t, fixture, ErrImportCASConflict)
}

type importRestartMinIOFixture struct {
	initial       *Service
	blob          *attachments.S3BlobStore
	imports       *memoryImportRepository
	archive       []byte
	archiveDigest [32]byte
	actor         recordauth.ActorScope
	destination   records.SubjectReference
	preview       ImportPlanView
	job           store.RecordImportJob
	plan          store.RecordImportPlan
	artifact      store.RecordImportArtifact
}

const importRestartSourceAttachmentID = "att_source00001"

func newImportRestartMinIOFixture(t *testing.T) importRestartMinIOFixture {
	t.Helper()
	client, bucket := newPortabilityMinIOFixture(t)
	blob, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	imports := newMemoryImportRepository()
	initialWriter := &importWriterStub{repo: imports}
	initial := newImportRestartMinIOService(t, imports, blob, initialWriter)
	actor := portabilityTestActor(t)
	destination := testImportDestination()
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path:           "records/rec_restart01/document.md",
		Classification: ArchiveClassMarkdown,
		Payload:        []byte("# Restart import\n\n<!-- houfeng-ref:v1 attachment " + importRestartSourceAttachmentID + " -->\n[notes.txt](houfeng-attachment:" + importRestartSourceAttachmentID + ")\n"),
	}, {
		Path:           "records/rec_restart01/attachments/" + importRestartSourceAttachmentID + "/notes.txt",
		Classification: ArchiveClassAttachment,
		Payload:        []byte("restart attachment\n"),
	}})
	preview, err := initial.DryRun(context.Background(), DryRunRequest{
		DestinationSubject: destination, Actor: actor, IdempotencyKey: "import-restart-minio", Archive: archive,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if preview.DestinationSubject.SubjectKind != destination.Kind || preview.DestinationSubject.SubjectID != destination.SourceID {
		t.Fatalf("dry-run destination = %#v, want canonical %#v", preview.DestinationSubject, destination)
	}
	initial.mu.Lock()
	cached := initial.importPlans[preview.PlanID]
	initial.mu.Unlock()
	if cached.digest == ([32]byte{}) {
		t.Fatal("DryRun() cached an empty v2 plan digest")
	}

	imports.mu.Lock()
	var job store.RecordImportJob
	var jobOK bool
	for _, candidate := range imports.jobs {
		if candidate.PlanID == preview.PlanID {
			job = candidate
			jobOK = true
			break
		}
	}
	plan, planOK := imports.plans[preview.PlanID]
	artifact, artifactOK := imports.artifacts[job.ImportJobID]
	imports.mu.Unlock()
	if !jobOK || !planOK || !artifactOK {
		t.Fatalf("durable import metadata missing: job=%v plan=%v artifact=%v", jobOK, planOK, artifactOK)
	}
	if plan.PlanDigest != cached.digest || plan.DestinationSubject != destination || job.DestinationSubject != destination {
		t.Fatalf("durable metadata digest/destination mismatch: plan=%#v job=%#v cached digest=%x", plan, job, cached.digest)
	}
	if artifact.BackendKind != "s3" || artifact.ObjectVersionID == "" || artifact.BlobKey == "" {
		t.Fatalf("staged S3 artifact = %#v", artifact)
	}
	staged := attachments.ObjectVersion{
		Key: artifact.BlobKey, VersionID: artifact.ObjectVersionID,
		SHA256: artifact.SHA256, SizeBytes: int64(artifact.ByteSize),
	}
	if _, err := blob.Stat(context.Background(), staged); err != nil {
		t.Fatalf("Stat(exact staged S3 version) error = %v", err)
	}

	return importRestartMinIOFixture{
		initial: initial, blob: blob, imports: imports, archive: archive,
		archiveDigest: sha256.Sum256(archive), actor: actor, destination: destination,
		preview: preview, job: job, plan: plan, artifact: artifact,
	}
}

func newImportRestartMinIOService(
	t *testing.T,
	imports *memoryImportRepository,
	blob *attachments.S3BlobStore,
	importer DocumentImporter,
) *Service {
	t.Helper()
	service, _ := mustPortabilityService(t, portabilityHarness{enabled: true})
	service.backendKind = "s3"
	service.imports = imports
	service.importer = importer
	service.attachmentBlobs = blob
	service.rebuilder = &importRebuildStub{}
	service.staging = NewLeasedBlobStore(blob)
	return service
}

func rejectImportRestartMinIOApply(t *testing.T, fixture importRestartMinIOFixture, want error) {
	t.Helper()
	delegate := &importWriterStub{repo: fixture.imports}
	writer := &importRestartDocumentImporter{delegate: delegate}
	restarted := newImportRestartMinIOService(t, fixture.imports, fixture.blob, writer)
	_, err := restarted.Apply(context.Background(), ApplyRequest{
		Actor: fixture.actor, PlanID: fixture.preview.PlanID, LockVersion: fixture.preview.LockVersion,
	})
	if !errors.Is(err, want) {
		t.Fatalf("Apply() error = %v, want %v", err, want)
	}
	if delegate.writes != 0 || len(writer.requests) != 0 {
		t.Fatalf("rejected Apply() wrote domain rows: writes=%d requests=%d", delegate.writes, len(writer.requests))
	}
	fixture.imports.mu.Lock()
	defer fixture.imports.mu.Unlock()
	if len(fixture.imports.origins) != 0 {
		t.Fatalf("rejected Apply() inserted origins: %#v", fixture.imports.origins)
	}
	if artifact := fixture.imports.artifacts[fixture.job.ImportJobID]; artifact != fixture.artifact {
		t.Fatalf("rejected Apply() changed staged artifact: before=%#v after=%#v", fixture.artifact, artifact)
	}
}

func importRestartRemapTarget(t *testing.T, remaps []store.ImportRemap, kind, source string) string {
	t.Helper()
	for _, remap := range remaps {
		if remap.EntityKind == kind && remap.SourceID == source {
			return remap.TargetID
		}
	}
	t.Fatalf("missing %s remap for %s: %#v", kind, source, remaps)
	return ""
}

type importRestartDocumentImporter struct {
	delegate *importWriterStub
	requests []records.ImportDocumentRequest
}

func (writer *importRestartDocumentImporter) ImportDocuments(
	ctx context.Context,
	requests []records.ImportDocumentRequest,
) ([]records.ImportedDocument, error) {
	return writer.ImportDocumentsFinishing(ctx, requests, records.RevisionCommitFinish{})
}

func (writer *importRestartDocumentImporter) ImportDocumentsFinishing(
	ctx context.Context,
	requests []records.ImportDocumentRequest,
	finish records.RevisionCommitFinish,
) ([]records.ImportedDocument, error) {
	for _, request := range requests {
		request.SubjectReferences = append([]records.SubjectReference(nil), request.SubjectReferences...)
		writer.requests = append(writer.requests, request)
	}
	return writer.delegate.ImportDocumentsFinishing(ctx, requests, finish)
}

func requireImportRestartMinIO(t *testing.T) {
	t.Helper()
	if os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_MINIO_INTEGRATION=1 to run the real MinIO suite")
	}
}
