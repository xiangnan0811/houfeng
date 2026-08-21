package portability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordmarkdown"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

func TestPortabilitySensitiveTopologyRequiresIndependentCapability(t *testing.T) {
	t.Parallel()

	service, _ := mustPortabilityService(t, portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_export1", RevisionID: "rrv_export1", Title: "Disk notes",
			BodyMarkdown: "# Body\n", AuthorizationEpoch: 1, LockVersion: 1,
		},
	})
	viewer, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_0123456789abcdef01234567", Role: recordauth.RoleViewer, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	if _, err := service.Preview(context.Background(), PreviewRequest{
		Actor: viewer, IdempotencyKey: "export-sensitive-viewer",
		RecordID: "rec_export1", ExportKind: ExportKindMarkdown, ExportMode: ExportModeSensitiveTopo,
	}); !errors.Is(err, ErrExportUnauthorized) {
		t.Fatalf("viewer sensitive Preview() = %v", err)
	}
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-sensitive-admin",
		RecordID: "rec_export1", ExportKind: ExportKindMarkdown, ExportMode: ExportModeSensitiveTopo,
	})
	if err != nil {
		t.Fatalf("admin sensitive Preview() error = %v", err)
	}
	if preview.ConfirmToken == "" {
		t.Fatal("sensitive preview omitted confirm token")
	}
	if _, err := service.Create(context.Background(), CreateRequest{
		Actor: portabilityTestActor(t), PreviewID: preview.PreviewID,
		PreviewToken: preview.PreviewToken, InventoryDigest: preview.InventoryDigest,
	}); !errors.Is(err, ErrExportUnauthorized) {
		t.Fatalf("Create without confirm token = %v", err)
	}
}

func TestPortabilityPreviewAndCreateRejectDisabledCapability(t *testing.T) {
	t.Parallel()

	service, _ := mustPortabilityService(t, portabilityHarness{enabled: false})
	actor := portabilityTestActor(t)
	if _, err := service.Preview(context.Background(), PreviewRequest{
		Actor: actor, IdempotencyKey: "export-1", RecordID: "rec_export1",
		ExportKind: ExportKindMarkdown, ExportMode: ExportModeSafe,
	}); !errors.Is(err, ErrPortabilityDisabled) {
		t.Fatalf("Preview() error = %v, want ErrPortabilityDisabled", err)
	}
	if _, err := service.Create(context.Background(), CreateRequest{
		Actor: actor, PreviewID: "rej_x", PreviewToken: "aa", InventoryDigest: "bb",
	}); !errors.Is(err, ErrPortabilityDisabled) {
		t.Fatalf("Create() error = %v, want ErrPortabilityDisabled", err)
	}
}

func TestPortabilityMarkdownPreviewNamesUnauthorizedEvidence(t *testing.T) {
	t.Parallel()

	denied := "evs_deniedexport01"
	allowed := "evs_allowedexport1"
	harness := portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_export1", RevisionID: "rrv_export1", Title: "Disk notes",
			BodyMarkdown: "# Body\n", AuthorizationEpoch: 3, LockVersion: 2,
			EvidenceSnapshotIDs: []string{allowed, denied},
		},
		evidence: map[string]error{denied: recordauth.ErrDenied},
	}
	service, _ := mustPortabilityService(t, harness)
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-md-1",
		RecordID: "rec_export1", ExportKind: ExportKindMarkdown, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(preview.Unavailable) != 1 || preview.Unavailable[0].ID != denied || preview.Unavailable[0].Reason != "unauthorized" {
		t.Fatalf("unavailable = %#v, want named unauthorized evidence", preview.Unavailable)
	}
	payload := string(mustReadPreviewPayload(t, service, preview))
	if !strings.Contains(payload, "不可用材料") || !strings.Contains(payload, denied) {
		t.Fatal("markdown omitted unauthorized evidence instead of naming it")
	}
	if strings.Contains(payload, "as if present") {
		t.Fatal("markdown invented presence wording")
	}
}

func TestPortabilityCreateRejectsInventoryDriftWithoutStaging(t *testing.T) {
	t.Parallel()

	docs := &documentStub{document: records.ExportDocument{
		RecordID: "rec_export1", RevisionID: "rrv_export1", Title: "Stable",
		BodyMarkdown: "ok\n", AuthorizationEpoch: 1, LockVersion: 1,
	}}
	harness := portabilityHarness{enabled: true, documents: docs}
	service, jobs := mustPortabilityService(t, harness)
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-drift-1",
		RecordID: "rec_export1", ExportKind: ExportKindMarkdown, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	docs.mu.Lock()
	docs.document.BodyMarkdown = "changed\n"
	docs.mu.Unlock()
	if _, err := service.Create(context.Background(), CreateRequest{
		Actor: portabilityTestActor(t), PreviewID: preview.PreviewID,
		PreviewToken: preview.PreviewToken, InventoryDigest: preview.InventoryDigest,
	}); !errors.Is(err, ErrExportInventoryDrift) {
		t.Fatalf("Create() error = %v, want ErrExportInventoryDrift", err)
	}
	if jobs.artifactCount() != 0 {
		t.Fatal("drift published an artifact")
	}
}

func TestPortabilityComparisonExportBytesEqualKindExportAndOmitForbiddenFields(t *testing.T) {
	t.Parallel()

	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	snapshot := mustPortabilityComparisonSnapshot(t, kind)
	want := kind.Export(snapshot, evidence.ExportModeSafe)
	if !bytes.Equal(want.Bytes, snapshot.Bytes()) {
		t.Fatal("kind.Export bytes drifted from canonical snapshot")
	}

	harness := portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_compare1", RevisionID: "rrv_compare1", Title: "Saved comparison",
			BodyMarkdown: "saved\n", AuthorizationEpoch: 2, LockVersion: 1,
			EvidenceSnapshotIDs: []string{"evs_comparison01"},
		},
		snapshot: evidence.AuthorizedSnapshot{
			RecordID: "rec_compare1", SnapshotID: "evs_comparison01",
			Key: evidence.ComparisonResultV1Key(), Snapshot: snapshot,
		},
		comparison: kind,
	}
	service, _ := mustPortabilityService(t, harness)
	actor := portabilityTestActor(t)
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: actor, IdempotencyKey: "export-cmp-1", RecordID: "rec_compare1",
		SnapshotID: "evs_comparison01", ExportKind: ExportKindComparisonJSON, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if containsForbiddenComparisonField(mustJSON(preview.ComparisonSummary)) {
		t.Fatalf("Summarize leaked forbidden fields: %#v", preview.ComparisonSummary)
	}
	created, err := service.Create(context.Background(), CreateRequest{
		Actor: actor, PreviewID: preview.PreviewID,
		PreviewToken: preview.PreviewToken, InventoryDigest: preview.InventoryDigest,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	content, err := service.OpenContent(context.Background(), actor, created.ExportID)
	if err != nil {
		t.Fatalf("OpenContent() error = %v", err)
	}
	defer content.Body.Close()
	got, err := io.ReadAll(content.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, want.Bytes) {
		t.Fatal("comparison download bytes != ComparisonResultKind.Export")
	}
	if containsForbiddenComparisonField(got) {
		t.Fatalf("export bytes contained forbidden fields: %s", got)
	}
}

func TestPortabilityOpenContentStopsAfterRevoke(t *testing.T) {
	t.Parallel()

	harness := portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_export1", RevisionID: "rrv_export1", Title: "Revoke me",
			BodyMarkdown: "body\n", AuthorizationEpoch: 1, LockVersion: 1,
		},
	}
	service, _ := mustPortabilityService(t, harness)
	actor := portabilityTestActor(t)
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: actor, IdempotencyKey: "export-revoke-1", RecordID: "rec_export1",
		ExportKind: ExportKindMarkdown, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	created, err := service.Create(context.Background(), CreateRequest{
		Actor: actor, PreviewID: preview.PreviewID,
		PreviewToken: preview.PreviewToken, InventoryDigest: preview.InventoryDigest,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := service.Revoke(context.Background(), actor, created.ExportID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if _, err := service.OpenContent(context.Background(), actor, created.ExportID); !errors.Is(err, ErrExportLeaseRevoked) {
		t.Fatalf("OpenContent after revoke error = %v, want ErrExportLeaseRevoked", err)
	}
}

func TestPortabilityArchiveExportIncludesAuthorizedFilesAndNamesUnavailable(t *testing.T) {
	t.Parallel()

	denied := "evs_deniedarchive1"
	allowed := "evs_allowedarchive"
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	snapshot := mustPortabilityComparisonSnapshot(t, kind)
	wantComparison := kind.Export(snapshot, evidence.ExportModeSafe)
	harness := portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_archive1", RevisionID: "rrv_archive1", Title: "Archive notes",
			BodyMarkdown: "# Body\n", AuthorizationEpoch: 2, LockVersion: 1,
			EvidenceSnapshotIDs: []string{allowed, denied},
		},
		evidence: map[string]error{denied: recordauth.ErrDenied},
		snapshot: evidence.AuthorizedSnapshot{
			RecordID: "rec_archive1", SnapshotID: "evs_comparison01",
			Key: evidence.ComparisonResultV1Key(), Snapshot: snapshot,
		},
		comparison: kind,
	}
	service, _ := mustPortabilityService(t, harness)
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-archive-1",
		RecordID: "rec_archive1", SnapshotID: "evs_comparison01",
		ExportKind: ExportKindArchive, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview(archive) error = %v", err)
	}
	if preview.ExpectedFiles[0].MediaType != "application/zip" {
		t.Fatalf("expected media = %q, want application/zip", preview.ExpectedFiles[0].MediaType)
	}
	if len(preview.Unavailable) != 1 || preview.Unavailable[0].ID != denied {
		t.Fatalf("unavailable = %#v, want named unauthorized evidence", preview.Unavailable)
	}
	raw := mustReadPreviewPayload(t, service, preview)
	manifest, entries, err := ReadArchiveV1(raw)
	if err != nil {
		t.Fatalf("ReadArchiveV1() error = %v", err)
	}
	if manifest.Format != ArchiveFormatV1 || len(entries) != 3 {
		t.Fatalf("archive membership = %#v %d", manifest, len(entries))
	}
	byPath := map[string]ArchiveEntry{}
	for _, entry := range entries {
		byPath[entry.Path] = entry
	}
	document := byPath["records/rec_archive1/document.md"]
	if !strings.Contains(string(document.Payload), "不可用材料") || !strings.Contains(string(document.Payload), denied) {
		t.Fatalf("archive markdown omitted unauthorized material: %s", document.Payload)
	}
	if !bytes.Contains(byPath["records/rec_archive1/evidence/"+allowed+".json"].Payload, []byte(`{"ok":true}`)) {
		t.Fatal("archive omitted authorized evidence bytes")
	}
	if !bytes.Equal(byPath["records/rec_archive1/comparison.result_v1.json"].Payload, wantComparison.Bytes) {
		t.Fatal("archive comparison bytes != ComparisonResultKind.Export")
	}
}

func TestPortabilityPDFUsesSameRenderModelAndIsNotAuthority(t *testing.T) {
	t.Parallel()

	denied := "evs_deniedpdf0001"
	harness := portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_pdf1", RevisionID: "rrv_pdf1", Title: "Disk notes",
			BodyMarkdown: "# Body\n", AuthorizationEpoch: 1, LockVersion: 1,
			EvidenceSnapshotIDs: []string{denied},
		},
		evidence: map[string]error{denied: recordauth.ErrDenied},
	}
	service, _ := mustPortabilityService(t, harness)
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-pdf-1",
		RecordID: "rec_pdf1", ExportKind: ExportKindPDF, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview(pdf) error = %v", err)
	}
	if preview.ExpectedFiles[0].MediaType != "application/pdf" || preview.RenderStatus != "ready" {
		t.Fatalf("preview = %#v", preview)
	}
	if len(preview.Unavailable) != 1 || preview.Unavailable[0].ID != denied {
		t.Fatalf("unavailable = %#v", preview.Unavailable)
	}
	raw := mustReadPreviewPayload(t, service, preview)
	html, err := recordmarkdown.ExtractDerivedHTML(raw)
	if err != nil {
		t.Fatalf("ExtractDerivedHTML() error = %v", err)
	}
	if !strings.Contains(html, "Disk notes") || !strings.Contains(html, "不可用材料") || !strings.Contains(html, denied) {
		t.Fatalf("pdf drifted from Markdown RenderModel: %s", html)
	}
	if _, _, err := ReadArchiveV1(raw); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("PDF was accepted as machine archive: %v", err)
	}
}

func TestPortabilityPDFWithoutRendererStaysUnsupported(t *testing.T) {
	t.Parallel()

	service, _ := mustPortabilityService(t, portabilityHarness{
		enabled: true,
		omitPDF: true,
		document: records.ExportDocument{
			RecordID: "rec_export1", RevisionID: "rrv_export1", Title: "x",
			BodyMarkdown: "x\n", AuthorizationEpoch: 1, LockVersion: 1,
		},
	})
	if _, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-pdf-none",
		RecordID: "rec_export1", ExportKind: ExportKindPDF, ExportMode: ExportModeSafe,
	}); !errors.Is(err, ErrUnsupportedExportKind) {
		t.Fatalf("Preview(pdf) error = %v, want ErrUnsupportedExportKind", err)
	}
}

type portabilityHarness struct {
	enabled     bool
	document    records.ExportDocument
	documents   *documentStub
	evidence    map[string]error
	snapshot    evidence.AuthorizedSnapshot
	comparison  ComparisonExporter
	attachments AttachmentSource
	omitPDF     bool
	jobs        *memoryJobRepository
}

func mustPortabilityService(t *testing.T, harness portabilityHarness) (*Service, *memoryJobRepository) {
	t.Helper()
	blob, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	docs := harness.documents
	if docs == nil {
		docs = &documentStub{document: harness.document}
	}
	jobs := newMemoryJobRepository()
	harness.jobs = jobs
	options := Options{
		Enabled:         harness.enabled,
		BackendKind:     "local",
		Documents:       docs,
		Jobs:            jobs,
		Evidence:        &evidenceStub{deny: harness.evidence, snapshot: harness.snapshot},
		Snapshots:       &snapshotStub{snapshot: harness.snapshot},
		Comparison:      harness.comparison,
		Attachments:     harness.attachments,
		AttachmentBlobs: blob,
		Staging:         NewLeasedBlobStore(blob),
		Now:             func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) },
	}
	if !harness.omitPDF {
		options.PDF = NewIsolatedDocumentPDFRenderer("")
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service, jobs
}

func mustReadPreviewPayload(t *testing.T, service *Service, preview Preview) []byte {
	t.Helper()
	created, err := service.Create(context.Background(), CreateRequest{
		Actor: portabilityTestActor(t), PreviewID: preview.PreviewID,
		PreviewToken: preview.PreviewToken, InventoryDigest: preview.InventoryDigest,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	content, err := service.OpenContent(context.Background(), portabilityTestActor(t), created.ExportID)
	if err != nil {
		t.Fatalf("OpenContent() error = %v", err)
	}
	defer content.Body.Close()
	payload, err := io.ReadAll(content.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	return payload
}

func portabilityTestActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_0123456789abcdef01234567",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

type documentStub struct {
	mu       sync.Mutex
	document records.ExportDocument
	err      error
}

func (stub *documentStub) ExportDocument(_ context.Context, _ records.ExportDocumentRequest) (records.ExportDocument, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.err != nil {
		return records.ExportDocument{}, stub.err
	}
	return stub.document, nil
}

type evidenceStub struct {
	deny      map[string]error
	snapshot  evidence.AuthorizedSnapshot
	snapshots map[string]evidence.AuthorizedSnapshot
}

func (stub *evidenceStub) Export(_ context.Context, request evidence.ExportRequest) (evidence.ExportMaterial, error) {
	if stub != nil && stub.deny != nil {
		if err := stub.deny[request.SnapshotID]; err != nil {
			return evidence.ExportMaterial{}, err
		}
	}
	if stub != nil && stub.snapshots != nil {
		if loaded, ok := stub.snapshots[request.SnapshotID]; ok && loaded.Snapshot.Size() > 0 {
			return evidence.ExportMaterial{MediaType: "application/json", Bytes: loaded.Snapshot.Bytes()}, nil
		}
	}
	if stub != nil && stub.snapshot.SnapshotID == request.SnapshotID && len(stub.snapshot.Snapshot.Bytes()) > 0 {
		return evidence.ExportMaterial{MediaType: "application/json", Bytes: stub.snapshot.Snapshot.Bytes()}, nil
	}
	return evidence.ExportMaterial{MediaType: "application/json", Filename: "ok.json", Bytes: []byte(`{"ok":true}`)}, nil
}

type snapshotStub struct {
	snapshot  evidence.AuthorizedSnapshot
	snapshots map[string]evidence.AuthorizedSnapshot
	err       error
}

func (stub *snapshotStub) LoadAuthorizedEvidenceSnapshot(
	_ context.Context,
	_ evidence.ActorScope,
	snapshotID string,
) (evidence.AuthorizedSnapshot, error) {
	if stub.err != nil {
		return evidence.AuthorizedSnapshot{}, stub.err
	}
	if stub.snapshots != nil {
		if loaded, ok := stub.snapshots[snapshotID]; ok {
			return loaded, nil
		}
	}
	if stub.snapshot.SnapshotID != snapshotID {
		return evidence.AuthorizedSnapshot{}, recordauth.ErrDenied
	}
	return stub.snapshot, nil
}

type memoryJobRepository struct {
	mu        sync.Mutex
	jobs      map[string]store.RecordExportJob
	byKey     map[string]string
	artifacts map[string]store.RecordExportArtifact
}

func newMemoryJobRepository() *memoryJobRepository {
	return &memoryJobRepository{
		jobs:      make(map[string]store.RecordExportJob),
		byKey:     make(map[string]string),
		artifacts: make(map[string]store.RecordExportArtifact),
	}
}

func (repository *memoryJobRepository) ClaimExportJob(
	_ context.Context,
	input store.ClaimRecordExportJobInput,
) (store.RecordExportJob, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existingID, ok := repository.byKey[input.ActorID+"/"+input.IdempotencyKey]; ok {
		existing := repository.jobs[existingID]
		if existing.RequestFingerprint != input.RequestFingerprint || existing.InventoryDigest != input.InventoryDigest {
			return store.RecordExportJob{}, store.ErrRecordExportJobConflict
		}
		return existing, nil
	}
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	job := store.RecordExportJob{
		ExportJobID:        "rej_" + hex.EncodeToString(input.RequestFingerprint[:8]),
		ActorID:            input.ActorID,
		IdempotencyKey:     input.IdempotencyKey,
		ExportKind:         input.ExportKind,
		ExportMode:         input.ExportMode,
		JobState:           store.RecordExportJobStatePreviewed,
		LockVersion:        1,
		RequestFingerprint: input.RequestFingerprint,
		InventoryDigest:    input.InventoryDigest,
		AuthorizationEpoch: input.AuthorizationEpoch,
		RecordID:           input.RecordID,
		RevisionID:         input.RevisionID,
		ExpiresAt:          input.ExpiresAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	repository.jobs[job.ExportJobID] = job
	repository.byKey[input.ActorID+"/"+input.IdempotencyKey] = job.ExportJobID
	return job, nil
}

func (repository *memoryJobRepository) AdvanceExportJob(_ context.Context, input store.AdvanceRecordExportJobInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	job, ok := repository.jobs[input.ExportJobID]
	if !ok || job.LockVersion != input.LockVersion {
		return store.ErrRecordExportJobCASConflict
	}
	job.JobState = input.JobState
	job.FailureCode = input.FailureCode
	job.ExpiresAt = input.ExpiresAt
	job.LockVersion++
	repository.jobs[input.ExportJobID] = job
	return nil
}

func (repository *memoryJobRepository) LoadExportJob(_ context.Context, exportJobID string) (store.RecordExportJob, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	job, ok := repository.jobs[exportJobID]
	if !ok {
		return store.RecordExportJob{}, store.ErrRecordExportNotFound
	}
	return job, nil
}

func (repository *memoryJobRepository) PublishExportArtifact(
	_ context.Context,
	input store.PublishRecordExportArtifactInput,
) (store.RecordExportArtifact, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	artifact := store.RecordExportArtifact{
		ArtifactID:   "rxa_memory1",
		ExportJobID:  input.ExportJobID,
		ArtifactKind: input.ArtifactKind,
		ContentType:  input.ContentType,
		BackendKind:  input.BackendKind,
		BlobKey:      input.BlobKey,
		SHA256:       input.SHA256,
		ByteSize:     input.ByteSize,
		ExpiresAt:    input.ExpiresAt,
		CreatedAt:    time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
	}
	repository.artifacts[input.ExportJobID] = artifact
	return artifact, nil
}

func (repository *memoryJobRepository) LoadExportArtifact(_ context.Context, exportJobID string) (store.RecordExportArtifact, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	artifact, ok := repository.artifacts[exportJobID]
	if !ok {
		return store.RecordExportArtifact{}, store.ErrRecordExportNotFound
	}
	return artifact, nil
}

func (repository *memoryJobRepository) RevokeExport(_ context.Context, exportJobID string, lockVersion uint64) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	job, ok := repository.jobs[exportJobID]
	if !ok || job.LockVersion != lockVersion {
		return store.ErrRecordExportJobCASConflict
	}
	job.JobState = store.RecordExportJobStateRevoked
	job.LockVersion++
	repository.jobs[exportJobID] = job
	if artifact, ok := repository.artifacts[exportJobID]; ok {
		now := time.Date(2026, 8, 21, 12, 5, 0, 0, time.UTC)
		artifact.RevokedAt = &now
		repository.artifacts[exportJobID] = artifact
	}
	return nil
}

func (repository *memoryJobRepository) artifactCount() int {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return len(repository.artifacts)
}

func mustPortabilityComparisonSnapshot(t *testing.T, kind *evidence.ComparisonResultKind) evidence.CanonicalSnapshot {
	t.Helper()
	window := evidence.TimeWindow{
		Start: time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      recordauth.ProjectIDDefault,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindTarget,
		SourceID:     "tg_0123456789abcdef",
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	envelope := evidence.SnapshotEnvelope{
		Key: evidence.ComparisonResultV1Key(),
		Subject: evidence.IdentitySnapshot{
			Type: "target", ID: "tg_0123456789abcdef",
			Fields: map[string]string{"display_name": "edge probe"},
		},
		Source: evidence.IdentitySnapshot{
			Type: string(recordauth.SourceKindTarget), ID: "tg_0123456789abcdef",
			Fields: map[string]string{"display_name": "edge probe"},
		},
		Authorization:      authorization,
		SourceDigest:       sha256.Sum256([]byte("source")),
		RequestedWindow:    window,
		ActualWindow:       window,
		ObservedAt:         window.End,
		CapturedAt:         window.End.Add(time.Minute),
		ReferencedAt:       window.End.Add(2 * time.Minute),
		SourceRevision:     "revision-1",
		SourceWatermark:    "watermark-1",
		ProducerVersion:    "comparison-result/v1",
		CalculationVersion: evidence.ComparisonCalculationVersion,
		Units:              evidence.UnitsSemantics{Status: evidence.UnitsNotApplicable, Reason: "comparison result metadata"},
		Quality:            evidence.Quality{Status: evidence.QualityComplete, SampleCount: 60},
		Sensitivity:        evidence.SensitivityNormal,
		ActualPrecision:    evidence.DurationSemantics{Applicable: false, Reason: "comparison result metadata"},
		BucketWidth:        evidence.DurationSemantics{Applicable: false, Reason: "comparison result metadata"},
		QuotaOutcome:       evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention: evidence.RetentionSemantics{
			Immutable: true, Scope: evidence.RetentionScopeRecordRevision,
			SourceDeletion: evidence.SourceDeletionSnapshotRetained,
		},
	}
	payload := map[string]any{
		"version":             "comparison_result/v1",
		"baseline_index":      0,
		"alignment":           string(evidence.CoverageActual),
		"requested_from":      "2026-08-10T11:00:00Z",
		"requested_to":        "2026-08-10T12:00:00Z",
		"tolerance_seconds":   60,
		"digest":              strings.Repeat("ab", 32),
		"registry_version":    "evidence-kinds/v1",
		"calculation_version": evidence.ComparisonCalculationVersion,
		"items": []any{
			map[string]any{
				"original_snapshot_id": "evs_left",
				"copied_snapshot_id":   "evs_leftcopy",
				"hash":                 strings.Repeat("11", 32),
				"kind":                 evidence.CommandAuditV1Key().String(),
				"revision_context":     string(evidence.RevisionContextBound),
				"record_type":          "incident",
				"business_status":      "open",
				"status_group":         "active",
				"impact_level":         "high",
				"occurred_at":          "2026-08-01T00:00:00Z",
			},
		},
		"warnings":           []any{},
		"system_differences": []any{},
		"available_kinds":    []any{evidence.CommandAuditV1Key().String()},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(kind.Descriptor(), envelope, payload, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot() error = %v", err)
	}
	return snapshot
}
