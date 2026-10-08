package portability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
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
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-1", Archive: archive})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if preview.DestinationSubject.SubjectKind != testImportDestination().Kind ||
		preview.DestinationSubject.SubjectID != testImportDestination().SourceID {
		t.Fatalf("destination subject = %#v", preview.DestinationSubject)
	}
	if importer.writes != 0 {
		t.Fatalf("DryRun wrote %d domain rows", importer.writes)
	}
	if len(preview.Remaps) != 1 || preview.Remaps[0].SourceID != "rec_source01" ||
		preview.Remaps[0].TargetID == "rec_source01" {
		t.Fatalf("remaps = %#v", preview.Remaps)
	}
	replay, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-1", Archive: archive})
	if err != nil || replay.PlanID != preview.PlanID {
		t.Fatalf("DryRun replay = %#v %v", replay, err)
	}
	if len(replay.Remaps) != 1 || replay.Remaps[0].TargetID != preview.Remaps[0].TargetID {
		t.Fatalf("DryRun replay remaps drifted: first=%#v replay=%#v", preview.Remaps, replay.Remaps)
	}
}

func TestPortabilityDryRunEncodesEmptyOptionalCollectionsAsArrays(t *testing.T) {
	t.Parallel()

	service, _, _ := mustImportService(t)
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\n\nRecovered.\n"),
	}})
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-empty-collections", Archive: archive})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if preview.Quarantine == nil || len(preview.Quarantine) != 0 || preview.Remaps == nil {
		t.Fatalf("DryRun collections = remaps:%#v quarantine:%#v", preview.Remaps, preview.Quarantine)
	}
	remapsJSON, quarantineJSON := importPlanCollectionJSON(t, preview)
	if quarantineJSON != "[]" || len(remapsJSON) == 0 || remapsJSON[0] != '[' {
		t.Fatalf("DryRun JSON remaps=%s quarantine=%s", remapsJSON, quarantineJSON)
	}
	replay, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-empty-collections", Archive: archive})
	if err != nil {
		t.Fatalf("DryRun(replay) error = %v", err)
	}
	if replay.Quarantine == nil || len(replay.Quarantine) != 0 {
		t.Fatalf("replay quarantine = %#v", replay.Quarantine)
	}
	_, replayQuarantineJSON := importPlanCollectionJSON(t, replay)
	if replayQuarantineJSON != "[]" {
		t.Fatalf("replay quarantine JSON = %s", replayQuarantineJSON)
	}

	expiresAt := time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC)
	job := store.RecordImportJob{JobState: store.RecordImportJobStatePlanned, LockVersion: 2}
	nilView := service.planView(job, store.RecordImportPlan{
		ImportPlanID: "rip_nil", ExpiresAt: expiresAt,
	}, nil)
	if nilView.Remaps == nil || nilView.Quarantine == nil {
		t.Fatalf("nil inputs = %#v", nilView)
	}
	nilRemaps, nilQuarantine := importPlanCollectionJSON(t, nilView)
	if nilRemaps != "[]" || nilQuarantine != "[]" {
		t.Fatalf("nil inputs JSON remaps=%s quarantine=%s", nilRemaps, nilQuarantine)
	}
	emptyView := service.planView(job, store.RecordImportPlan{
		ImportPlanID: "rip_empty",
		Remaps:       []store.ImportRemap{},
		ExpiresAt:    expiresAt,
	}, []QuarantinedEvidence{})
	emptyRemaps, emptyQuarantine := importPlanCollectionJSON(t, emptyView)
	if emptyRemaps != "[]" || emptyQuarantine != "[]" {
		t.Fatalf("empty inputs JSON remaps=%s quarantine=%s", emptyRemaps, emptyQuarantine)
	}
	populated := service.planView(job, store.RecordImportPlan{
		ImportPlanID: "rip_items",
		Remaps:       []store.ImportRemap{{EntityKind: "record", SourceID: "rec_source01", TargetID: "rec_local01"}},
		ExpiresAt:    expiresAt,
	}, []QuarantinedEvidence{{
		Kind: "vendor.unknown", Schema: "vendor.unknown/v1", Digest: "aa", ByteSize: 8, Reason: "cannot interpret",
	}})
	if len(populated.Remaps) != 1 || len(populated.Quarantine) != 1 || populated.Quarantine[0].Digest != "aa" {
		t.Fatalf("populated = %#v", populated)
	}
	populatedRemaps, populatedQuarantine := importPlanCollectionJSON(t, populated)
	if populatedRemaps == "[]" || populatedRemaps == "null" || populatedQuarantine == "[]" || populatedQuarantine == "null" {
		t.Fatalf("populated JSON remaps=%s quarantine=%s", populatedRemaps, populatedQuarantine)
	}
}

func TestPortabilityImportPreservesUnsupportedReferenceWarning(t *testing.T) {
	t.Parallel()

	service, importer, _ := mustImportService(t)
	const sourceAttachmentID = "att_2d42319db90a83ee"
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_2b7e5ea569779425/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Records 附件评论与迁移验收\n\n# 真实链路验收\n\n本记录验证附件扫描、读取、评论和导入导出。\n\n<!-- houfeng-ref:v1 attachment " + sourceAttachmentID + " -->\n[records-attachment-fixture.zip](houfeng-attachment:" + sourceAttachmentID + ")\n\n## 不可用材料\n\n- attachment `" + sourceAttachmentID + "`：unsupported\n"),
	}})
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-unsupported-reference", Archive: archive})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if importer.writes != 0 {
		t.Fatalf("DryRun wrote %d domain rows", importer.writes)
	}
	body := service.importPlans[preview.PlanID].documents[0].Body
	if !strings.Contains(body, "records-attachment-fixture.zip") ||
		!strings.Contains(body, "## 不可用材料") ||
		!strings.Contains(body, "unsupported") {
		t.Fatalf("unsupported material text was lost: %q", body)
	}
	if strings.Contains(body, "houfeng-ref:v1 attachment "+sourceAttachmentID) ||
		strings.Contains(body, "houfeng-attachment:"+sourceAttachmentID) {
		t.Fatalf("unsupported reference kept an active source binding: %q", body)
	}
}

func TestPortabilityImportRebindsIncludedAttachmentReference(t *testing.T) {
	t.Parallel()

	service, _, _ := mustImportService(t)
	const sourceAttachmentID = "att_source00001"
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\n\n<!-- houfeng-ref:v1 attachment " + sourceAttachmentID + " -->\n[notes.txt](houfeng-attachment:" + sourceAttachmentID + ")\n"),
	}, {
		Path:           "records/rec_source01/attachments/" + sourceAttachmentID + "/notes.txt",
		Classification: ArchiveClassAttachment, Payload: []byte("notes"),
	}})
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-included-reference", Archive: archive})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	var targetAttachmentID string
	remaps := make([]store.ImportRemap, 0, len(preview.Remaps))
	for _, remap := range preview.Remaps {
		remaps = append(remaps, store.ImportRemap{
			EntityKind: remap.EntityKind, SourceID: remap.SourceID, TargetID: remap.TargetID,
		})
		if remap.EntityKind == "attachment" && remap.SourceID == sourceAttachmentID {
			targetAttachmentID = remap.TargetID
		}
	}
	if targetAttachmentID == "" {
		t.Fatalf("attachment remap missing: %#v", preview.Remaps)
	}
	body := service.importPlans[preview.PlanID].documents[0].Body
	if !strings.Contains(body, "<!-- houfeng-ref:v1 attachment "+targetAttachmentID+" -->") ||
		!strings.Contains(body, "[notes.txt](houfeng-attachment:"+targetAttachmentID+")") {
		t.Fatalf("included reference was not rebound: %q", body)
	}
	if strings.Contains(body, "houfeng-ref:v1 attachment "+sourceAttachmentID) ||
		strings.Contains(body, "houfeng-attachment:"+sourceAttachmentID) {
		t.Fatalf("included reference kept source binding: %q", body)
	}
	rebound, err := rebindArchive(archive, remaps, testImportDestination())
	if err != nil {
		t.Fatalf("rebindArchive() error = %v", err)
	}
	if got := rebound.documents[0].Body; got != body {
		t.Fatalf("rebound body = %q, cached plan body = %q", got, body)
	}
}

func importPlanCollectionJSON(t *testing.T, view ImportPlanView) (remaps, quarantine string) {
	t.Helper()
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var body struct {
		Remaps     json.RawMessage `json:"remaps"`
		Quarantine json.RawMessage `json:"quarantine"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return string(body.Remaps), string(body.Quarantine)
}

func TestPortabilityImportRejectsHostileAndUntrustedMembers(t *testing.T) {
	t.Parallel()

	service, _, _ := mustImportService(t)
	actor := portabilityTestActor(t)
	if _, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: actor, IdempotencyKey: "import-hostile", Archive: []byte("PK\x03\x04truncated")}); !errors.Is(err, ErrInvalidArchive) {
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
	if _, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: actor, IdempotencyKey: "import-auth", Archive: untrusted}); !errors.Is(err, ErrUntrustedImportContent) {
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
	if _, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: actor, IdempotencyKey: "import-checkpoint", Archive: checkpoint}); !errors.Is(err, ErrUntrustedImportContent) {
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
	if _, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: actor, IdempotencyKey: "import-required", Archive: blocked}); !errors.Is(err, ErrImportSchemaBlocked) {
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
	if _, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: actor, IdempotencyKey: "import-optional", Archive: optional}); !errors.Is(err, ErrImportSchemaBlocked) {
		t.Fatalf("archive optional:true unknown schema error = %v, want ErrImportSchemaBlocked", err)
	}
}

func TestPortabilityApplyIsIdempotentAndHonorsCASAndRebuild(t *testing.T) {
	t.Parallel()

	service, importer, _ := mustImportService(t)
	rebuilder := service.rebuilder.(*importRebuildStub)
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-apply",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# Disk notes\n"),
		}})})
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
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-staged", Archive: archive})
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
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-tombstone",
		Archive: archive})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	imports.tombstone(sha256.Sum256(archive))
	if _, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-tombstone-again",
		Archive: archive}); !errors.Is(err, ErrOriginTombstoned) {
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
	if _, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-origin-preview",
		Archive: archive}); !errors.Is(err, ErrImportOriginConflict) {
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

func testImportDestination() records.SubjectReference {
	return records.SubjectReference{
		RegistryVersion: records.SubjectRegistryVersionV1,
		Kind:            records.SubjectKindTarget,
		Role:            records.RelationRoleAffected,
		SourceID:        "tg_0123456789abcdef",
		Primary:         true,
	}
}

type importTestSubjectAdapter struct{}

func (importTestSubjectAdapter) Kind() records.SubjectKind {
	return records.SubjectKindTarget
}

func (importTestSubjectAdapter) Resolve(
	_ context.Context,
	actor recordauth.ActorScope,
	reference records.SubjectReference,
) (records.ResolvedSubject, error) {
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      actor.ProjectID,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		return records.ResolvedSubject{}, err
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindTarget,
		SourceID:     reference.SourceID,
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		return records.ResolvedSubject{}, err
	}
	identity, err := records.NewSubjectIdentitySnapshot(reference.Kind, map[string]string{
		"display_name": "test target",
	})
	if err != nil {
		return records.ResolvedSubject{}, err
	}
	return records.ResolvedSubject{
		ProjectID:            actor.ProjectID,
		StableID:             reference.SourceID,
		IdentitySnapshot:     identity,
		LiveRoute:            "/targets/" + reference.SourceID,
		CaptureAuthorization: authorization,
	}, nil
}

func mustTestImportSubjectRegistry(t *testing.T) records.SubjectAdapterRegistry {
	t.Helper()
	registry, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{
		importTestSubjectAdapter{},
	})
	if err != nil {
		t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
	}
	return registry
}

func mustImportService(t *testing.T) (*Service, *importWriterStub, *memoryImportRepository) {
	t.Helper()
	base, _ := mustPortabilityService(t, portabilityHarness{enabled: true, document: records.ExportDocument{
		RecordID: "rec_export1", RevisionID: "rrv_export1", Title: "x", BodyMarkdown: "x\n",
		AuthorizationEpoch: 1, LockVersion: 1,
	}})
	base.subjects = mustTestImportSubjectRegistry(t)
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
	mu           sync.Mutex
	writes       int
	failOn       int
	repo         *memoryImportRepository
	preparations []evidence.RevisionPreparation
	attachments  [][]string
	importedAtts [][]attachments.ImportedAvailableAttachment
	lastFinish   records.RevisionCommitFinish
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
	stub.preparations = stub.preparations[:0]
	stub.attachments = stub.attachments[:0]
	stub.importedAtts = stub.importedAtts[:0]
	stub.lastFinish = finish
	written := make([]records.ImportedDocument, 0, len(requests))
	for _, request := range requests {
		stub.writes++
		stub.preparations = append(stub.preparations, request.EvidencePreparation)
		stub.attachments = append(stub.attachments, append([]string(nil), request.AttachmentIDs...))
		stub.importedAtts = append(stub.importedAtts, append([]attachments.ImportedAvailableAttachment(nil), request.ImportedAttachments...))
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
		if job.ArchiveDigest != input.ArchiveDigest || job.DestinationSubject != input.DestinationSubject {
			return store.RecordImportJob{}, store.ErrRecordImportCASConflict
		}
		return job, nil
	}
	job := store.RecordImportJob{
		ImportJobID: "rij_memory1", ActorID: input.ActorID, JobState: store.RecordImportJobStateQuarantined,
		LockVersion: 1, ArchiveDigest: input.ArchiveDigest, DestinationSubject: input.DestinationSubject,
		ExpiresAt: input.ExpiresAt,
	}
	repository.jobs[job.ImportJobID] = job
	repository.byKey[input.ActorID+"/"+input.IdempotencyKey] = job.ImportJobID
	return job, nil
}

func (repository *memoryImportRepository) SaveImportPlan(_ context.Context, input store.SaveRecordImportPlanInput) (store.RecordImportPlan, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	job, ok := repository.jobs[input.ImportJobID]
	if !ok || job.DestinationSubject != input.DestinationSubject {
		return store.RecordImportPlan{}, store.ErrRecordImportCASConflict
	}
	plan := store.RecordImportPlan{
		ImportPlanID: "rip_memory1", ImportJobID: input.ImportJobID, PlanDigest: input.PlanDigest,
		ObjectCount: input.ObjectCount, RemapCount: input.RemapCount, Remaps: input.Remaps,
		Documents: input.Documents, DestinationSubject: input.DestinationSubject, ExpiresAt: input.ExpiresAt,
	}
	repository.plans[plan.ImportPlanID] = plan
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

	plan, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "official-import", Archive: raw})
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
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-atomic",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}, {
			Path: "records/rec_source02/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# B\n"),
		}})})
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
func TestPortabilityApplyMapsFinalRevisionDestinationErrorsWithoutAdvancement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		resolveErr error
		wantErr    error
	}{
		{name: "missing", resolveErr: store.ErrRecordSubjectNotFound, wantErr: ErrExportUnauthorized},
		{name: "unavailable", resolveErr: store.ErrRecordSubjectUnavailable, wantErr: ErrExportUnavailable},
		{name: "denied", resolveErr: recordauth.ErrDenied, wantErr: ErrExportUnauthorized},
		{name: "invalid", resolveErr: records.ErrInvalidResolvedSubject, wantErr: ErrExportUnavailable},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			actor := portabilityTestActor(t)
			destination := testImportDestination()
			subjectAdapter := newFinalRecheckImportSubjectAdapter(t, actor, destination, test.resolveErr)
			subjects, err := records.NewSubjectAdapterRegistry([]records.SubjectSourceAdapter{subjectAdapter})
			if err != nil {
				t.Fatalf("NewSubjectAdapterRegistry() error = %v", err)
			}
			service, _, imports := mustImportService(t)
			service.subjects = subjects

			revisionStore := &finalRecheckRevisionCommitStore{}
			revisionService, err := records.NewRevisionService(
				subjects,
				finalRecheckCurrentAuthorizationStub{},
				revisionStore,
			)
			if err != nil {
				t.Fatalf("NewRevisionService() error = %v", err)
			}
			application, err := records.NewApplication(
				finalRecheckReadStub{},
				revisionService,
				finalRecheckLifecycleStub{},
				finalRecheckDraftStub{},
				records.ApplicationOptions{
					IdempotencyOwnerID: "portability-final-recheck-test",
					OwnerLeaseDuration: time.Minute,
					IdempotencyTTL:     time.Hour,
					OutboxTTL:          time.Hour,
				},
			)
			if err != nil {
				t.Fatalf("NewApplication() error = %v", err)
			}
			service.importer = application

			archive := mustImportArchive(t, []ArchiveEntry{
				{
					Path:           "records/rec_source_final_recheck/document.md",
					Classification: ArchiveClassMarkdown,
					Payload:        []byte("# Final recheck\n"),
				},
			})
			preview, err := service.DryRun(context.Background(), DryRunRequest{
				DestinationSubject: destination,
				Actor:              actor,
				IdempotencyKey:     "final-recheck-" + test.name,
				Archive:            archive,
			})
			if err != nil {
				t.Fatalf("DryRun() error = %v", err)
			}
			result, err := service.Apply(context.Background(), ApplyRequest{
				Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion,
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Apply() error = %v, want %v", err, test.wantErr)
			}
			if len(result.RecordIDs) != 0 {
				t.Fatalf("Apply() result = %#v, want no record IDs", result)
			}
			if subjectAdapter.calls != 3 {
				t.Fatalf("subject Resolve calls = %d, want dry-run, apply, and final revision recheck", subjectAdapter.calls)
			}
			if revisionStore.calls != 0 {
				t.Fatalf("revision commits = %d, want zero after final recheck failure", revisionStore.calls)
			}

			plan, err := imports.LoadImportPlan(context.Background(), preview.PlanID)
			if err != nil {
				t.Fatalf("LoadImportPlan() error = %v", err)
			}
			job, err := imports.LoadImportJob(context.Background(), plan.ImportJobID)
			if err != nil {
				t.Fatalf("LoadImportJob() error = %v", err)
			}
			if job.JobState != store.RecordImportJobStatePlanned {
				t.Fatalf("job state = %q, want planned", job.JobState)
			}
			if _, err := imports.LoadOrigin(context.Background(), sha256.Sum256(archive)); !errors.Is(err, store.ErrRecordImportNotFound) {
				t.Fatalf("LoadOrigin() error = %v, want no origin", err)
			}
			service.mu.Lock()
			cached := service.importPlans[preview.PlanID]
			service.mu.Unlock()
			if cached.jobState != store.RecordImportJobStatePlanned || len(cached.applied) != 0 {
				t.Fatalf("cached import state = %#v, want planned with no applied records", cached)
			}
		})
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
	authorized := evidence.AuthorizedSnapshot{
		RecordID: "rec_roundtrip1", SnapshotID: "evs_allowedarchive",
		Key: evidence.ComparisonResultV1Key(), Snapshot: snapshot,
	}
	service.evidence = &evidenceStub{snapshot: authorized}
	service.comparison = kind
	service.snapshots = &snapshotStub{snapshot: authorized}
	service.kinds = mustOfficialImportKinds(t, officialProbeDescriptor())

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

	plan, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "official-evidence-import", Archive: raw})
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
	if len(importer.preparations) != 1 || len(importer.preparations[0].Imported()) != 1 {
		t.Fatalf("imported snapshots = %#v, want persistable wrapper", importer.preparations)
	}
}

func TestOfficialArchiveRejectsRawEvidenceJSONThatApplyCannotPersist(t *testing.T) {
	t.Parallel()

	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	snapshot := mustPortabilityComparisonSnapshot(t, kind)
	exported := kind.Export(snapshot, evidence.ExportModeSafe)
	service, importer, _ := mustImportService(t)
	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\n"),
	}, {
		Path: "records/rec_source01/evidence/evs_rawexport01.json", Classification: ArchiveClassEvidenceJSON,
		Payload: exported.Bytes,
	}})
	if _, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-raw-evidence", Archive: archive}); !errors.Is(err, ErrUntrustedImportContent) {
		t.Fatalf("DryRun(raw evidence) = %v, want ErrUntrustedImportContent", err)
	}
	if importer.writes != 0 {
		t.Fatalf("raw evidence wrote %d domain rows", importer.writes)
	}
}

func TestOfficialArchiveWrapMissAppliesWithoutEmptySuccessSnapshots(t *testing.T) {
	t.Parallel()

	service, importer, _ := mustImportService(t)
	docs := service.documents.(*documentStub)
	docs.mu.Lock()
	docs.document = records.ExportDocument{
		RecordID: "rec_wrapmiss1", RevisionID: "rrv_wrapmiss1", Title: "Wrap miss",
		BodyMarkdown: "# Body\n", AuthorizationEpoch: 1, LockVersion: 1,
		EvidenceSnapshotIDs: []string{"evs_wrapmiss00001"},
	}
	docs.mu.Unlock()
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-wrap-miss-apply",
		RecordID: "rec_wrapmiss1", ExportKind: ExportKindArchive, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	raw := mustReadPreviewPayload(t, service, preview)
	plan, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-wrap-miss", Archive: raw})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: plan.PlanID, LockVersion: plan.LockVersion,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if importer.writes != 1 || len(importer.preparations) != 1 || len(importer.preparations[0].Imported()) != 0 {
		t.Fatalf("wrap-miss apply wrote snapshots=%#v writes=%d", importer.preparations, importer.writes)
	}
}

func TestOfficialArchiveApplyPutsKnownEvidenceOnFinishingRequest(t *testing.T) {
	t.Parallel()

	comparisonKind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	comparisonSnapshot := mustPortabilityComparisonSnapshot(t, comparisonKind)
	probeDescriptor, probeSnapshot := mustPortabilityProbeSnapshot(t)
	comparisonExport := comparisonKind.Export(comparisonSnapshot, evidence.ExportModeSafe)
	probeExport := officialProbeKind{descriptor: probeDescriptor}.Export(probeSnapshot, evidence.ExportModeSafe)
	service, importer, _ := mustOfficialFidelityImportService(t, comparisonSnapshot, probeSnapshot, probeDescriptor)

	raw := mustExportOfficialFidelityArchive(t, service)
	plan, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "official-fidelity-import", Archive: raw})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	applied, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: plan.PlanID, LockVersion: plan.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if importer.writes != 1 || len(applied.RecordIDs) != 1 || len(importer.preparations) != 1 {
		t.Fatalf("writes=%d applied=%#v preparations=%d", importer.writes, applied, len(importer.preparations))
	}
	imported := importer.preparations[0].Imported()
	if len(imported) != 2 {
		t.Fatalf("Imported() = %#v, want comparison + probe", imported)
	}
	exports := map[string][]byte{}
	for _, item := range imported {
		kind, lookupErr := service.kinds.LookupKey(item.Snapshot().Envelope().Key)
		if lookupErr != nil {
			t.Fatalf("LookupKey(%s) error = %v", item.Snapshot().Envelope().Key, lookupErr)
		}
		exports[kind.Descriptor().Key.String()] = kind.Export(item.Snapshot(), evidence.ExportModeSafe).Bytes
	}
	if !bytes.Equal(exports[evidence.ComparisonResultV1Key().String()], comparisonExport.Bytes) {
		t.Fatal("restored comparison Export bytes drifted")
	}
	if !bytes.Equal(exports[evidence.MonitoringProbeV2Key().String()], probeExport.Bytes) {
		t.Fatal("restored probe Export bytes drifted")
	}

	second, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: plan.PlanID, LockVersion: plan.LockVersion,
	})
	if err != nil || second.JobState != store.RecordImportJobStateApplied || second.RecordIDs[0] != applied.RecordIDs[0] {
		t.Fatalf("second Apply() = %#v %v", second, err)
	}
	if importer.writes != 1 {
		t.Fatalf("second apply wrote again: writes=%d", importer.writes)
	}

	blocked, importerBlocked, _ := mustOfficialFidelityImportService(t, comparisonSnapshot, probeSnapshot, probeDescriptor)
	blockedPlan, err := blocked.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "official-fidelity-tombstone-preview", Archive: raw})
	if err != nil {
		t.Fatalf("DryRun(pre-tombstone) error = %v", err)
	}
	blocked.imports.(*memoryImportRepository).tombstone(sha256.Sum256(raw))
	if _, err := blocked.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: blockedPlan.PlanID, LockVersion: blockedPlan.LockVersion,
	}); !errors.Is(err, ErrOriginTombstoned) {
		t.Fatalf("Apply(tombstone) error = %v", err)
	}
	if importerBlocked.writes != 0 || len(importerBlocked.preparations) != 0 {
		t.Fatalf("tombstone apply persisted evidence: writes=%d preparations=%d", importerBlocked.writes, len(importerBlocked.preparations))
	}
}

func TestOfficialEvidenceRestoreMemberRoundTripsKnownKinds(t *testing.T) {
	t.Parallel()

	comparisonKind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	comparisonSnapshot := mustPortabilityComparisonSnapshot(t, comparisonKind)
	probeDescriptor, probeSnapshot := mustPortabilityProbeSnapshot(t)
	kinds := mustOfficialImportKinds(t, probeDescriptor)

	for _, snapshot := range []evidence.CanonicalSnapshot{comparisonSnapshot, probeSnapshot} {
		exported := snapshot.Bytes()
		wrapped, err := encodeOfficialEvidenceRestoreMember(snapshot, exported)
		if err != nil {
			t.Fatalf("encodeOfficialEvidenceRestoreMember() error = %v", err)
		}
		restored, isWrapper, err := restoreOfficialEvidenceSnapshot(kinds, wrapped)
		if err != nil || !isWrapper {
			t.Fatalf("restoreOfficialEvidenceSnapshot() = (%v, %v) wrapper=%t", restored, err, isWrapper)
		}
		if restored.Hash() != snapshot.Hash() || !bytes.Equal(restored.Bytes(), snapshot.Bytes()) {
			t.Fatal("restored snapshot drifted from official export")
		}
	}
	_, isWrapper, err := restoreOfficialEvidenceSnapshot(kinds, comparisonKind.Export(comparisonSnapshot, evidence.ExportModeSafe).Bytes)
	if err != nil || isWrapper {
		t.Fatalf("raw comparison Export treated as wrapper: wrapper=%t err=%v", isWrapper, err)
	}
}

func TestPortabilityApplyFailClosedWhenLoadedActorMissing(t *testing.T) {
	t.Parallel()

	service, importer, imports := mustImportService(t)
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-missing-actor",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}})})
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
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-applied-actor",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}})})
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
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-origin-fail",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}})})
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
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-origin-exists",
		Archive: archive})
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
	preview, err := service.DryRun(context.Background(), DryRunRequest{DestinationSubject: testImportDestination(), Actor: portabilityTestActor(t), IdempotencyKey: "import-guards",
		Archive: mustImportArchive(t, []ArchiveEntry{{
			Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
			Payload: []byte("# A\n"),
		}})})
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

func officialProbeDescriptor() evidence.Descriptor {
	return evidence.Descriptor{
		Key: evidence.MonitoringProbeV2Key(),
		Fields: []evidence.FieldDefinition{
			{Path: "metric_name", Sensitivity: evidence.SensitivityNormal},
			{Path: "metric_value", Sensitivity: evidence.SensitivityNormal},
		},
		Conformance: evidence.ConformanceMetadata{
			CanonicalizationVersion: evidence.CanonicalizationVersionV1,
			ForbiddenCorpusVersion:  evidence.ForbiddenCorpusVersionV1,
			RendererVersion:         "renderer.v1",
			MaxCanonicalBytes:       evidence.MaxCanonicalPayloadBytes,
		},
	}
}

type officialProbeKind struct {
	descriptor evidence.Descriptor
}

func (kind officialProbeKind) Descriptor() evidence.Descriptor { return kind.descriptor }

func (officialProbeKind) ValidateSelection(context.Context, evidence.ActorScope, evidence.Selection) error {
	return evidence.ErrInvalidCanonicalPayload
}

func (officialProbeKind) PreviewCapture(context.Context, evidence.ActorScope, evidence.Selection) (evidence.Preview, error) {
	return evidence.Preview{}, evidence.ErrInvalidCanonicalPayload
}

func (officialProbeKind) Capture(context.Context, evidence.ActorScope, evidence.Intent) (evidence.CanonicalSnapshot, error) {
	return evidence.CanonicalSnapshot{}, evidence.ErrInvalidCanonicalPayload
}

func (officialProbeKind) Authorize(context.Context, evidence.ActorScope, evidence.Selection) (evidence.AuthorizationScope, error) {
	return evidence.AuthorizationScope{}, evidence.ErrInvalidCanonicalPayload
}

func (kind officialProbeKind) Summarize(evidence.CanonicalSnapshot) evidence.Summary {
	return evidence.Summary{Key: kind.descriptor.Key, RendererVersion: kind.descriptor.Conformance.RendererVersion}
}

func (kind officialProbeKind) Compare(evidence.CanonicalSnapshot, evidence.CanonicalSnapshot, evidence.Alignment) evidence.Comparison {
	return evidence.Comparison{Key: kind.descriptor.Key, Compatible: true}
}

func (kind officialProbeKind) Export(snapshot evidence.CanonicalSnapshot, _ evidence.ExportMode) evidence.ExportMaterial {
	return evidence.ExportMaterial{Key: kind.descriptor.Key, MediaType: "application/json", Bytes: snapshot.Bytes()}
}

func mustOfficialImportKinds(t *testing.T, probe evidence.Descriptor) evidence.Registry {
	t.Helper()
	comparison, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	registry, err := evidence.NewRegistry([]evidence.Kind{comparison, officialProbeKind{descriptor: probe}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

func mustPortabilityProbeSnapshot(t *testing.T) (evidence.Descriptor, evidence.CanonicalSnapshot) {
	t.Helper()
	descriptor := officialProbeDescriptor()
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
		Key: evidence.MonitoringProbeV2Key(),
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
		ProducerVersion:    "producer-1",
		CalculationVersion: "calculation-1",
		Units:              evidence.UnitsSemantics{Status: evidence.UnitsApplicable, Values: map[string]string{"latency_ms": "ms"}},
		Quality:            evidence.Quality{Status: evidence.QualityComplete, SampleCount: 60},
		Sensitivity:        evidence.SensitivityNormal,
		ActualPrecision:    evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		BucketWidth:        evidence.DurationSemantics{Applicable: true, Value: time.Minute},
		QuotaOutcome:       evidence.QuotaOutcome{Status: evidence.QuotaAllowed},
		Retention: evidence.RetentionSemantics{
			Immutable: true, Scope: evidence.RetentionScopeRecordRevision,
			SourceDeletion: evidence.SourceDeletionSnapshotRetained,
		},
	}
	snapshot, _, err := evidence.NewCanonicalSnapshot(descriptor, envelope, map[string]any{
		"metric_name": "latency_ms", "metric_value": "12",
	}, evidence.RedactionNormalOnly)
	if err != nil {
		t.Fatalf("NewCanonicalSnapshot(probe) error = %v", err)
	}
	return descriptor, snapshot
}

func mustOfficialFidelityImportService(
	t *testing.T,
	comparisonSnapshot evidence.CanonicalSnapshot,
	probeSnapshot evidence.CanonicalSnapshot,
	probeDescriptor evidence.Descriptor,
) (*Service, *importWriterStub, *memoryImportRepository) {
	t.Helper()
	service, importer, imports := mustImportService(t)
	docs := service.documents.(*documentStub)
	docs.mu.Lock()
	docs.document = records.ExportDocument{
		RecordID: "rec_fidelity1", RevisionID: "rrv_fidelity1", Title: "Disk notes",
		BodyMarkdown:       "See https://example.com and do not delete this.\n",
		AuthorizationEpoch: 2, LockVersion: 1,
		EvidenceSnapshotIDs: []string{"evs_comparison01", "evs_probe0000001"},
	}
	docs.mu.Unlock()
	snapshots := map[string]evidence.AuthorizedSnapshot{
		"evs_comparison01": {
			RecordID: "rec_fidelity1", SnapshotID: "evs_comparison01",
			Key: evidence.ComparisonResultV1Key(), Snapshot: comparisonSnapshot,
		},
		"evs_probe0000001": {
			RecordID: "rec_fidelity1", SnapshotID: "evs_probe0000001",
			Key: evidence.MonitoringProbeV2Key(), Snapshot: probeSnapshot,
		},
	}
	comparisonKind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatalf("NewComparisonResultKind() error = %v", err)
	}
	service.evidence = &evidenceStub{snapshots: snapshots}
	service.snapshots = &snapshotStub{snapshots: snapshots}
	service.comparison = comparisonKind
	service.kinds = mustOfficialImportKinds(t, probeDescriptor)
	return service, importer, imports
}

func mustExportOfficialFidelityArchive(t *testing.T, service *Service) []byte {
	t.Helper()
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "official-fidelity-export",
		RecordID: "rec_fidelity1", SnapshotID: "evs_comparison01",
		ExportKind: ExportKindArchive, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	return mustReadPreviewPayload(t, service, preview)
}

type finalRecheckImportSubjectAdapter struct {
	reference records.SubjectReference
	resolved  records.ResolvedSubject
	finalErr  error
	calls     int
}

func newFinalRecheckImportSubjectAdapter(
	t *testing.T,
	actor recordauth.ActorScope,
	reference records.SubjectReference,
	finalErr error,
) *finalRecheckImportSubjectAdapter {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      actor.ProjectID,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindTarget,
		SourceID:     reference.SourceID,
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	identity, err := records.NewSubjectIdentitySnapshot(reference.Kind, map[string]string{
		"display_name": "final recheck target",
	})
	if err != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", err)
	}
	return &finalRecheckImportSubjectAdapter{
		reference: reference,
		resolved: records.ResolvedSubject{
			ProjectID:            actor.ProjectID,
			StableID:             reference.SourceID,
			IdentitySnapshot:     identity,
			LiveRoute:            "/targets/" + reference.SourceID,
			CaptureAuthorization: authorization,
		},
		finalErr: finalErr,
	}
}

func (adapter *finalRecheckImportSubjectAdapter) Kind() records.SubjectKind {
	return records.SubjectKindTarget
}

func (adapter *finalRecheckImportSubjectAdapter) Resolve(
	_ context.Context,
	_ recordauth.ActorScope,
	reference records.SubjectReference,
) (records.ResolvedSubject, error) {
	adapter.calls++
	if reference != adapter.reference {
		return records.ResolvedSubject{}, records.ErrInvalidSubjectReference
	}
	if adapter.calls >= 3 {
		return records.ResolvedSubject{}, adapter.finalErr
	}
	return adapter.resolved, nil
}

type finalRecheckCurrentAuthorizationStub struct{}

func (finalRecheckCurrentAuthorizationStub) ResolveCurrentRecordAuthorization(
	context.Context,
	recordauth.ActorScope,
	string,
) (records.CurrentRecordAuthorization, error) {
	return records.CurrentRecordAuthorization{}, records.ErrRecordNotFound
}

type finalRecheckRevisionCommitStore struct {
	calls int
}

func (stub *finalRecheckRevisionCommitStore) CommitRevision(
	ctx context.Context,
	command records.RevisionCommitCommand,
) (records.RevisionCommitResult, error) {
	results, err := stub.CommitRevisions(ctx, []records.RevisionCommitCommand{command})
	if err != nil {
		return records.RevisionCommitResult{}, err
	}
	return results[0], nil
}

func (stub *finalRecheckRevisionCommitStore) CommitRevisions(
	_ context.Context,
	commands []records.RevisionCommitCommand,
) ([]records.RevisionCommitResult, error) {
	stub.calls++
	results := make([]records.RevisionCommitResult, 0, len(commands))
	for _, command := range commands {
		results = append(results, records.RevisionCommitResult{
			RecordID: command.RecordID, RevisionID: "rrv_final_recheck", RevisionNo: 1, Created: true,
		})
	}
	return results, nil
}

func (stub *finalRecheckRevisionCommitStore) CommitRevisionsFinishing(
	ctx context.Context,
	commands []records.RevisionCommitCommand,
	_ records.RevisionCommitFinish,
) ([]records.RevisionCommitResult, error) {
	return stub.CommitRevisions(ctx, commands)
}

type finalRecheckReadStub struct{}

func (finalRecheckReadStub) GetRecord(context.Context, records.RecordGetRequest) (records.Record, error) {
	return records.Record{}, records.ErrInvalidApplicationRequest
}

func (finalRecheckReadStub) ListRecords(context.Context, records.RecordListRequest) (records.RecordListResult, error) {
	return records.RecordListResult{}, records.ErrInvalidApplicationRequest
}

func (finalRecheckReadStub) GetRevision(context.Context, records.RecordRevisionGetRequest) (records.RecordRevision, error) {
	return records.RecordRevision{}, records.ErrInvalidApplicationRequest
}

func (finalRecheckReadStub) ListRevisions(context.Context, records.RecordRevisionListRequest) ([]records.RecordRevision, error) {
	return nil, records.ErrInvalidApplicationRequest
}

type finalRecheckLifecycleStub struct{}

func (finalRecheckLifecycleStub) ChangeLifecycle(
	context.Context,
	records.RecordLifecycleRequest,
) (records.RecordLifecycleResult, error) {
	return records.RecordLifecycleResult{}, records.ErrInvalidApplicationRequest
}

type finalRecheckDraftStub struct{}

func (finalRecheckDraftStub) ReadDraft(context.Context, records.DraftReadRequest) (records.Draft, error) {
	return records.Draft{}, records.ErrInvalidApplicationRequest
}

func (finalRecheckDraftStub) ListDrafts(context.Context, records.DraftListRequest) (records.DraftListResult, error) {
	return records.DraftListResult{}, records.ErrInvalidApplicationRequest
}

func (finalRecheckDraftStub) CreateDraft(context.Context, records.DraftCreateRequest) (records.Draft, error) {
	return records.Draft{}, records.ErrInvalidApplicationRequest
}

func (finalRecheckDraftStub) PatchDraft(context.Context, records.DraftPatchRequest) (records.Draft, error) {
	return records.Draft{}, records.ErrInvalidApplicationRequest
}

func (finalRecheckDraftStub) DiscardDraft(context.Context, records.DraftDiscardRequest) error {
	return records.ErrInvalidApplicationRequest
}

func (finalRecheckDraftStub) PreparePublish(context.Context, records.DraftPublishRequest) (records.Draft, error) {
	return records.Draft{}, records.ErrInvalidApplicationRequest
}
