package portability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

func TestPortabilityDryRunWritesNoDomainRowsAndRemaps(t *testing.T) {
	t.Parallel()

	service, importer, _ := mustImportService(t)
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\n\nRecovered.\n"),
	}})
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-1", Archive: archive,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if importer.writes != 0 {
		t.Fatalf("DryRun wrote %d domain rows", importer.writes)
	}
	if len(preview.Remaps) != 1 || preview.Remaps[0].SourceID != "rec_source01" ||
		preview.Remaps[0].TargetID == "rec_source01" {
		t.Fatalf("remaps = %#v", preview.Remaps)
	}
	replay, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-1", Archive: archive,
	})
	if err != nil || replay.PlanID != preview.PlanID {
		t.Fatalf("DryRun replay = %#v %v", replay, err)
	}
	if len(replay.Remaps) != 1 || replay.Remaps[0].TargetID != preview.Remaps[0].TargetID {
		t.Fatalf("DryRun replay remaps drifted: first=%#v replay=%#v", preview.Remaps, replay.Remaps)
	}
}

func TestPortabilityImportRejectsHostileAndUntrustedMembers(t *testing.T) {
	t.Parallel()

	service, _, _ := mustImportService(t)
	actor := portabilityTestActor(t)
	if _, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: actor, IdempotencyKey: "import-hostile", Archive: []byte("PK\x03\x04truncated"),
	}); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("hostile zip error = %v", err)
	}
	untrusted, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n"),
	}, {
		Path: "records/rec_source01/grant.json", Classification: ArchiveClassEvidenceJSON,
		Payload: []byte(`{"authorization":"admin","role":"root"}`),
	}})
	if err != nil {
		t.Fatalf("WriteArchiveV1() error = %v", err)
	}
	if _, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: actor, IdempotencyKey: "import-auth", Archive: untrusted,
	}); !errors.Is(err, ErrUntrustedImportContent) {
		t.Fatalf("untrusted error = %v", err)
	}
	checkpoint, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n"),
	}, {
		Path: "records/rec_source01/search.checkpoint.json", Classification: ArchiveClassEvidenceJSON,
		Payload: []byte(`{"optional":true,"schema":"search.checkpoint/v1"}`),
	}})
	if err != nil {
		t.Fatalf("WriteArchiveV1(checkpoint) error = %v", err)
	}
	if _, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: actor, IdempotencyKey: "import-checkpoint", Archive: checkpoint,
	}); !errors.Is(err, ErrUntrustedImportContent) {
		t.Fatalf("checkpoint error = %v", err)
	}
}

func TestPortabilityImportQuarantinesOptionalEvidenceAndBlocksRequiredUnknown(t *testing.T) {
	t.Parallel()

	service, _, _ := mustImportService(t)
	actor := portabilityTestActor(t)
	blocked, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n"),
	}, {
		Path: "records/rec_source01/evidence/evs_unknown.json", Classification: ArchiveClassEvidenceJSON,
		Payload: []byte(`{"required":true,"schema":"vendor.secret/v1","kind":"vendor.secret"}`),
	}})
	if err != nil {
		t.Fatalf("WriteArchiveV1() error = %v", err)
	}
	if _, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: actor, IdempotencyKey: "import-required", Archive: blocked,
	}); !errors.Is(err, ErrImportSchemaBlocked) {
		t.Fatalf("required unknown error = %v", err)
	}
	optional, err := WriteArchiveV1([]ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# A\n"),
	}, {
		Path: "records/rec_source01/evidence/evs_optional.json", Classification: ArchiveClassEvidenceJSON,
		Payload: []byte(`{"optional":true,"schema":"vendor.unknown/v1","kind":"vendor.unknown","observed_at":"2026-08-21T12:00:00Z"}`),
	}})
	if err != nil {
		t.Fatalf("WriteArchiveV1(optional) error = %v", err)
	}
	if _, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: actor, IdempotencyKey: "import-optional", Archive: optional,
	}); !errors.Is(err, ErrImportSchemaBlocked) {
		t.Fatalf("archive optional:true unknown schema error = %v, want ErrImportSchemaBlocked", err)
	}
}

func TestPortabilityApplyIsIdempotentAndHonorsCASAndRebuild(t *testing.T) {
	t.Parallel()

	service, importer, _ := mustImportService(t)
	rebuilder := service.rebuilder.(*importRebuildStub)
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-apply",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# Disk notes\n"),
		}}),
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion - 1,
	}); !errors.Is(err, ErrImportCASConflict) {
		t.Fatalf("Apply(cas) error = %v", err)
	}
	if importer.writes != 0 {
		t.Fatal("CAS drift wrote domain rows")
	}
	first, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	second, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply(replay) error = %v", err)
	}
	if importer.writes != 1 || rebuilder.calls != 1 || first.RecordIDs[0] != second.RecordIDs[0] {
		t.Fatalf("writes=%d rebuilds=%d first=%#v second=%#v", importer.writes, rebuilder.calls, first, second)
	}
}

func TestPortabilityApplyReadsStagedArchiveAfterCacheAndLeaseDrop(t *testing.T) {
	t.Parallel()

	service, importer, imports := mustImportService(t)
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\n"),
	}})
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-staged", Archive: archive,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, ok := imports.artifacts["rij_memory1"]; !ok {
		t.Fatal("DryRun did not persist an import archive artifact")
	}
	service.mu.Lock()
	service.importPlans = map[string]cachedImportPlan{}
	service.mu.Unlock()
	service.staging.dropLease("rij_memory1")

	applied, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply after lease drop error = %v", err)
	}
	if importer.writes != 1 || len(applied.RecordIDs) != 1 {
		t.Fatalf("writes=%d applied=%#v", importer.writes, applied)
	}
}

func TestAuthoritativeProjectionRebuilderRejectsEmptyRecordID(t *testing.T) {
	t.Parallel()

	rebuilder := NewAuthoritativeProjectionRebuilder()
	if err := rebuilder.RebuildImportedRecord(context.Background(), ""); !errors.Is(err, ErrInvalidImportRequest) {
		t.Fatalf("empty record error = %v", err)
	}
	if err := rebuilder.RebuildImportedRecord(context.Background(), "rec_imported1"); err != nil {
		t.Fatalf("RebuildImportedRecord() error = %v", err)
	}
}

func TestPortabilityApplyRejectsOriginTombstone(t *testing.T) {
	t.Parallel()

	service, importer, imports := mustImportService(t)
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\nSee https://example.com and do not delete this.\n"),
	}})
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-tombstone",
		Archive: archive,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	imports.tombstone(sha256.Sum256(archive))
	if _, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-tombstone-again",
		Archive: archive,
	}); !errors.Is(err, ErrOriginTombstoned) {
		t.Fatalf("DryRun(tombstone) error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	}); !errors.Is(err, ErrOriginTombstoned) {
		t.Fatalf("Apply(tombstone) error = %v", err)
	}
	if importer.writes != 0 {
		t.Fatal("tombstone apply wrote domain rows")
	}
}

func TestPortabilityDryRunRejectsExistingOrigin(t *testing.T) {
	t.Parallel()

	service, importer, imports := mustImportService(t)
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\nSee https://example.com and do not delete this.\n"),
	}})
	if _, err := imports.InsertOrigin(context.Background(), store.InsertRecordOriginInput{
		OriginKind:   "import",
		OriginDigest: sha256.Sum256(archive),
		SourceRecord: "rec_otherorigin1",
	}); err != nil {
		t.Fatalf("InsertOrigin() error = %v", err)
	}
	if _, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-origin-preview",
		Archive: archive,
	}); !errors.Is(err, ErrImportOriginConflict) {
		t.Fatalf("DryRun(existing origin) error = %v", err)
	}
	if importer.writes != 0 {
		t.Fatal("origin conflict dry-run wrote domain rows")
	}
}

func mustImportArchive(t *testing.T, entries []ArchiveEntry) []byte {
	t.Helper()
	raw, err := WriteArchiveV1(entries)
	if err != nil {
		t.Fatalf("WriteArchiveV1() error = %v", err)
	}
	return raw
}

func mustImportService(t *testing.T) (*Service, *importWriterStub, *memoryImportRepository) {
	t.Helper()
	base, _ := mustPortabilityService(t, portabilityHarness{enabled: true, document: records.ExportDocument{
		RecordID: "rec_export1", RevisionID: "rrv_export1", Title: "x", BodyMarkdown: "x\n",
		AuthorizationEpoch: 1, LockVersion: 1,
	}})
	writer := &importWriterStub{}
	rebuilder := &importRebuildStub{}
	imports := newMemoryImportRepository()
	writer.repo = imports
	base.imports = imports
	base.importer = writer
	base.evidenceImports = &evidenceImportStub{}
	base.rebuilder = rebuilder
	return base, writer, imports
}

type importWriterStub struct {
	mu     sync.Mutex
	writes int
	failOn int
	repo   *memoryImportRepository
}

func (stub *importWriterStub) ImportDocuments(ctx context.Context, requests []records.ImportDocumentRequest) ([]records.ImportedDocument, error) {
	return stub.ImportDocumentsFinishing(ctx, requests, records.RevisionCommitFinish{})
}

func (stub *importWriterStub) ImportDocumentsFinishing(
	ctx context.Context,
	requests []records.ImportDocumentRequest,
	finish records.RevisionCommitFinish,
) ([]records.ImportedDocument, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.failOn > 0 && stub.failOn <= len(requests) {
		return nil, errors.New("import document failed")
	}
	if stub.repo != nil && finish.OriginDigest != [32]byte{} {
		if stub.repo.originInsertBlocked() {
			return nil, errors.New("origin insert failed")
		}
		if _, err := stub.repo.LoadOrigin(ctx, finish.OriginDigest); err == nil {
			return nil, ErrImportOriginConflict
		}
	}
	written := make([]records.ImportedDocument, 0, len(requests))
	for _, request := range requests {
		stub.writes++
		written = append(written, records.ImportedDocument{RecordID: request.RecordID, RevisionID: "rrv_imported"})
	}
	if stub.repo != nil && finish.ImportJobID != "" {
		if _, err := stub.repo.InsertOrigin(ctx, store.InsertRecordOriginInput{
			OriginKind:   finish.OriginKind,
			OriginDigest: finish.OriginDigest,
			SourceRecord: finish.SourceRecord,
		}); err != nil {
			return nil, err
		}
		if err := stub.repo.AdvanceImportJob(ctx, store.AdvanceRecordImportJobInput{
			ImportJobID: finish.ImportJobID,
			LockVersion: finish.JobLockVersion,
			JobState:    store.RecordImportJobStateApplied,
		}); err != nil {
			return nil, err
		}
	}
	return written, nil
}

type evidenceImportStub struct {
	mu    sync.Mutex
	calls []ImportedEvidenceRequest
}

func (stub *evidenceImportStub) ImportExportedEvidence(_ context.Context, request ImportedEvidenceRequest) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.calls = append(stub.calls, request)
	return nil
}

type importRebuildStub struct {
	mu    sync.Mutex
	calls int
}

func (stub *importRebuildStub) RebuildImportedRecord(context.Context, string) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.calls++
	return nil
}

type memoryImportRepository struct {
	mu               sync.Mutex
	jobs             map[string]store.RecordImportJob
	byKey            map[string]string
	plans            map[string]store.RecordImportPlan
	artifacts        map[string]store.RecordImportArtifact
	tombstones       map[[32]byte]struct{}
	origins          map[[32]byte]store.RecordOrigin
	failInsertOrigin bool
	omitActorOnLoad  bool
}

func newMemoryImportRepository() *memoryImportRepository {
	return &memoryImportRepository{
		jobs: make(map[string]store.RecordImportJob), byKey: make(map[string]string),
		plans: make(map[string]store.RecordImportPlan), artifacts: make(map[string]store.RecordImportArtifact),
		tombstones: make(map[[32]byte]struct{}), origins: make(map[[32]byte]store.RecordOrigin),
	}
}

func (repository *memoryImportRepository) originInsertBlocked() bool {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return repository.failInsertOrigin
}

func (repository *memoryImportRepository) tombstone(digest [32]byte) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.tombstones[digest] = struct{}{}
}

func (repository *memoryImportRepository) ClaimImportJob(_ context.Context, input store.ClaimRecordImportJobInput) (store.RecordImportJob, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existingID, ok := repository.byKey[input.ActorID+"/"+input.IdempotencyKey]; ok {
		job := repository.jobs[existingID]
		if job.ArchiveDigest != input.ArchiveDigest {
			return store.RecordImportJob{}, store.ErrRecordImportCASConflict
		}
		return job, nil
	}
	job := store.RecordImportJob{
		ImportJobID: "rij_memory1", ActorID: input.ActorID, JobState: store.RecordImportJobStateQuarantined,
		LockVersion: 1, ArchiveDigest: input.ArchiveDigest, ExpiresAt: input.ExpiresAt,
	}
	repository.jobs[job.ImportJobID] = job
	repository.byKey[input.ActorID+"/"+input.IdempotencyKey] = job.ImportJobID
	return job, nil
}

func (repository *memoryImportRepository) SaveImportPlan(_ context.Context, input store.SaveRecordImportPlanInput) (store.RecordImportPlan, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	plan := store.RecordImportPlan{
		ImportPlanID: "rip_memory1", ImportJobID: input.ImportJobID, PlanDigest: input.PlanDigest,
		ObjectCount: input.ObjectCount, RemapCount: input.RemapCount, Remaps: input.Remaps,
		Documents: input.Documents, ExpiresAt: input.ExpiresAt,
	}
	repository.plans[plan.ImportPlanID] = plan
	job := repository.jobs[input.ImportJobID]
	job.PlanID = plan.ImportPlanID
	repository.jobs[input.ImportJobID] = job
	return plan, nil
}

func (repository *memoryImportRepository) LoadImportPlan(_ context.Context, planID string) (store.RecordImportPlan, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	plan, ok := repository.plans[planID]
	if !ok {
		return store.RecordImportPlan{}, store.ErrRecordImportNotFound
	}
	return plan, nil
}

func (repository *memoryImportRepository) LoadImportJob(_ context.Context, jobID string) (store.RecordImportJob, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	job, ok := repository.jobs[jobID]
	if !ok {
		return store.RecordImportJob{}, store.ErrRecordImportNotFound
	}
	if repository.omitActorOnLoad {
		job.ActorID = ""
	}
	return job, nil
}

func (repository *memoryImportRepository) PublishImportArtifact(_ context.Context, input store.PublishRecordImportArtifactInput) (store.RecordImportArtifact, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.artifacts[input.ImportJobID]; ok {
		if existing.SHA256 != input.SHA256 || existing.BlobKey != input.BlobKey {
			return store.RecordImportArtifact{}, store.ErrRecordImportCASConflict
		}
		return existing, nil
	}
	artifact := store.RecordImportArtifact{
		ArtifactID: "ria_memory1", ImportJobID: input.ImportJobID, ArtifactRole: input.ArtifactRole,
		BackendKind: input.BackendKind, BlobKey: input.BlobKey, ObjectVersionID: input.ObjectVersionID,
		SHA256: input.SHA256, ByteSize: input.ByteSize, ExpiresAt: input.ExpiresAt,
	}
	repository.artifacts[input.ImportJobID] = artifact
	return artifact, nil
}

func (repository *memoryImportRepository) LoadImportArtifact(_ context.Context, jobID string) (store.RecordImportArtifact, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	artifact, ok := repository.artifacts[jobID]
	if !ok {
		return store.RecordImportArtifact{}, store.ErrRecordImportNotFound
	}
	return artifact, nil
}

func (repository *memoryImportRepository) AdvanceImportJob(_ context.Context, input store.AdvanceRecordImportJobInput) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	job, ok := repository.jobs[input.ImportJobID]
	if !ok || job.LockVersion != input.LockVersion {
		return store.ErrRecordImportCASConflict
	}
	job.JobState = input.JobState
	job.LockVersion++
	repository.jobs[input.ImportJobID] = job
	return nil
}

func (repository *memoryImportRepository) LoadOriginTombstone(_ context.Context, digest [32]byte) (store.RecordOriginTombstone, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, ok := repository.tombstones[digest]; !ok {
		return store.RecordOriginTombstone{}, store.ErrRecordImportNotFound
	}
	return store.RecordOriginTombstone{OriginDigest: digest}, nil
}

func (repository *memoryImportRepository) LoadOrigin(_ context.Context, digest [32]byte) (store.RecordOrigin, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	origin, ok := repository.origins[digest]
	if !ok {
		return store.RecordOrigin{}, store.ErrRecordImportNotFound
	}
	return origin, nil
}

func (repository *memoryImportRepository) InsertOrigin(_ context.Context, input store.InsertRecordOriginInput) (store.RecordOrigin, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.failInsertOrigin {
		return store.RecordOrigin{}, errors.New("origin insert failed")
	}
	if _, ok := repository.origins[input.OriginDigest]; ok {
		return store.RecordOrigin{}, store.ErrRecordOriginConflict
	}
	origin := store.RecordOrigin{OriginID: "ror_memory1", OriginDigest: input.OriginDigest}
	repository.origins[input.OriginDigest] = origin
	return origin, nil
}

func TestOfficialArchiveRoundTripAllowsDocumentURLsAndWritesKnownEvidence(t *testing.T) {
	t.Parallel()

	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	snapshot := mustPortabilityComparisonSnapshot(t, kind)
	exported := kind.Export(snapshot, evidence.ExportModeSafe)
	service, importer, _ := mustImportService(t)
	evidenceWriter := &evidenceImportStub{}
	service.evidenceImports = evidenceWriter
	docs := service.documents.(*documentStub)
	docs.mu.Lock()
	docs.document = records.ExportDocument{
		RecordID: "rec_roundtrip1", RevisionID: "rrv_roundtrip1", Title: "Disk notes",
		BodyMarkdown:       "See https://example.com and do not delete this.\nmetadata: kept\n",
		AuthorizationEpoch: 2, LockVersion: 1,
	}
	docs.mu.Unlock()
	service.comparison = kind
	service.snapshots = &snapshotStub{snapshot: evidence.AuthorizedSnapshot{
		RecordID: "rec_roundtrip1", SnapshotID: "evs_comparison01",
		Key: evidence.ComparisonResultV1Key(), Snapshot: snapshot,
	}}

	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "official-roundtrip",
		RecordID: "rec_roundtrip1", SnapshotID: "evs_comparison01",
		ExportKind: ExportKindArchive, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
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
	raw, err := io.ReadAll(content.Body)
	_ = content.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	plan, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "official-import", Archive: raw,
	})
	if err != nil {
		t.Fatalf("DryRun(official archive) error = %v", err)
	}
	if len(plan.Remaps) < 2 {
		t.Fatalf("remaps = %#v, want record + evidence", plan.Remaps)
	}
	applied, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: plan.PlanID, LockVersion: plan.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply(official archive) error = %v", err)
	}
	if importer.writes != 1 || len(applied.RecordIDs) != 1 {
		t.Fatalf("writes=%d applied=%#v", importer.writes, applied)
	}
	if len(evidenceWriter.calls) != 1 || !bytes.Equal(evidenceWriter.calls[0].Payload, exported.Bytes) {
		t.Fatalf("evidence imports = %#v, want official comparison bytes", evidenceWriter.calls)
	}
	if !strings.Contains(string(service.importPlans[plan.PlanID].documents[0].Body), "https://example.com") {
		t.Fatal("imported body dropped the document URL")
	}
	if strings.Contains(string(service.importPlans[plan.PlanID].documents[0].Body), "已授权材料") {
		t.Fatal("imported body kept export chrome")
	}
}

func TestPortabilityApplyIsAtomicAcrossDocuments(t *testing.T) {
	t.Parallel()

	service, importer, _ := mustImportService(t)
	importer.failOn = 2
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-atomic",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}, {
			Path: "records/rec_source02/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# B\n"),
		}}),
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	}); err == nil {
		t.Fatal("Apply() error = nil, want document failure")
	}
	if importer.writes != 0 {
		t.Fatalf("partial apply wrote %d records", importer.writes)
	}
}

func TestImportedEvidenceIdentityReadsOfficialExportEnvelope(t *testing.T) {
	t.Parallel()

	schema, sourceID, err := importedEvidenceIdentity(ArchiveEntry{
		Path:    "records/rec_source01/evidence/evs_probe01.json",
		Payload: []byte(`{"canonicalization_version":1,"kind":"monitoring.probe","schema_version":2,"payload":{}}`),
	})
	if err != nil {
		t.Fatalf("importedEvidenceIdentity() error = %v", err)
	}
	if schema != "monitoring.probe/v2" {
		t.Fatalf("schema = %q, want monitoring.probe/v2", schema)
	}
	if sourceID != "evs_probe01" {
		t.Fatalf("sourceID = %q, want evs_probe01", sourceID)
	}
}

func TestOfficialArchiveWithEvidenceSnapshotIDsDryRunsAndApplies(t *testing.T) {
	t.Parallel()

	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	snapshot := mustPortabilityComparisonSnapshot(t, kind)
	exported := kind.Export(snapshot, evidence.ExportModeSafe)
	service, importer, _ := mustImportService(t)
	evidenceWriter := &evidenceImportStub{}
	service.evidenceImports = evidenceWriter
	docs := service.documents.(*documentStub)
	docs.mu.Lock()
	docs.document = records.ExportDocument{
		RecordID: "rec_roundtrip1", RevisionID: "rrv_roundtrip1", Title: "Disk notes",
		BodyMarkdown:       "See https://example.com and do not delete this.\n",
		AuthorizationEpoch: 2, LockVersion: 1,
		EvidenceSnapshotIDs: []string{"evs_allowedarchive"},
	}
	docs.mu.Unlock()
	service.evidence = &evidenceStub{snapshot: evidence.AuthorizedSnapshot{
		RecordID: "rec_roundtrip1", SnapshotID: "evs_allowedarchive",
		Key: evidence.ComparisonResultV1Key(), Snapshot: snapshot,
	}}
	service.comparison = kind
	service.snapshots = &snapshotStub{snapshot: evidence.AuthorizedSnapshot{
		RecordID: "rec_roundtrip1", SnapshotID: "evs_allowedarchive",
		Key: evidence.ComparisonResultV1Key(), Snapshot: snapshot,
	}}

	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "official-evidence-export",
		RecordID: "rec_roundtrip1", SnapshotID: "evs_allowedarchive",
		ExportKind: ExportKindArchive, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
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
	raw, err := io.ReadAll(content.Body)
	_ = content.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}

	plan, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "official-evidence-import", Archive: raw,
	})
	if err != nil {
		t.Fatalf("DryRun(official evidence archive) error = %v", err)
	}
	applied, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: plan.PlanID, LockVersion: plan.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply(official evidence archive) error = %v", err)
	}
	if importer.writes != 1 || len(applied.RecordIDs) != 1 {
		t.Fatalf("writes=%d applied=%#v", importer.writes, applied)
	}
	if len(evidenceWriter.calls) == 0 {
		t.Fatal("official evidence member was not imported")
	}
	sawOfficial := false
	for _, call := range evidenceWriter.calls {
		if bytes.Equal(call.Payload, exported.Bytes) {
			sawOfficial = true
			break
		}
	}
	if !sawOfficial {
		t.Fatalf("evidence imports = %#v, want official Export bytes", evidenceWriter.calls)
	}
}

func TestPortabilityApplyFailClosedWhenLoadedActorMissing(t *testing.T) {
	t.Parallel()

	service, importer, imports := mustImportService(t)
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-missing-actor",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}}),
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	imports.omitActorOnLoad = true
	service.mu.Lock()
	service.importPlans = map[string]cachedImportPlan{}
	service.mu.Unlock()
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	}); !errors.Is(err, ErrExportUnauthorized) {
		t.Fatalf("Apply(empty actor) error = %v, want ErrExportUnauthorized", err)
	}
	if importer.writes != 0 {
		t.Fatal("empty loaded actor wrote domain rows")
	}
}

func TestPortabilityApplyRejectsForeignActorOnAppliedReplay(t *testing.T) {
	t.Parallel()

	service, _, _ := mustImportService(t)
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-applied-actor",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}}),
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	other, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_abcdef0123456789abcdef01", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: other, PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	}); !errors.Is(err, ErrExportUnauthorized) {
		t.Fatalf("applied replay foreign actor error = %v", err)
	}
}

func TestPortabilityApplyOriginFailureLeavesNoRecordsAndStaysRetryable(t *testing.T) {
	t.Parallel()

	service, importer, imports := mustImportService(t)
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-origin-fail",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}}),
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	imports.failInsertOrigin = true
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	}); err == nil {
		t.Fatal("Apply() error = nil, want origin failure")
	}
	if importer.writes != 0 {
		t.Fatalf("origin failure left %d records", importer.writes)
	}
	imports.failInsertOrigin = false
	applied, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	})
	if err != nil {
		t.Fatalf("retry after origin failure error = %v", err)
	}
	if importer.writes != 1 || len(applied.RecordIDs) != 1 {
		t.Fatalf("retry writes=%d applied=%#v", importer.writes, applied)
	}
}

func TestPortabilityApplyRejectsExistingOriginBeforeWriting(t *testing.T) {
	t.Parallel()

	service, importer, imports := mustImportService(t)
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# A\n"),
	}})
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-origin-exists",
		Archive: archive,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, err := imports.InsertOrigin(context.Background(), store.InsertRecordOriginInput{
		OriginKind:   "import",
		OriginDigest: sha256.Sum256(archive),
		SourceRecord: "rec_otherorigin1",
	}); err != nil {
		t.Fatalf("InsertOrigin() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	}); !errors.Is(err, ErrImportOriginConflict) {
		t.Fatalf("Apply(existing origin) error = %v", err)
	}
	if importer.writes != 0 {
		t.Fatal("existing origin wrote domain rows")
	}
}

func TestPortabilityApplyRejectsZeroLockAndForeignActor(t *testing.T) {
	t.Parallel()

	service, _, _ := mustImportService(t)
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-guards",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}}),
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: 0,
	}); !errors.Is(err, ErrImportCASConflict) {
		t.Fatalf("lock 0 error = %v", err)
	}
	other, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_abcdef0123456789abcdef01", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: other, PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	}); !errors.Is(err, ErrExportUnauthorized) {
		t.Fatalf("foreign actor error = %v", err)
	}
}
