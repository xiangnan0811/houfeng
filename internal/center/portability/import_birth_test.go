package portability

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"testing"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

// The memory APP implements the real coordinator contract, including immutable
// request bindings. Tests observe allocation and content effects separately.
type memoryImportBirth struct {
	mu     sync.Mutex
	byKey  map[string]recordbirth.Allocation
	byID   map[string]recordbirth.Allocation
	begins int
	finds  int
	gets   int
}

func newMemoryImportBirth() *memoryImportBirth {
	return &memoryImportBirth{byKey: make(map[string]recordbirth.Allocation), byID: make(map[string]recordbirth.Allocation)}
}

func importBirthKey(request recordbirth.Request) string {
	return request.ProjectID + "\x00" + request.ActorID + "\x00" + string(request.Purpose) + "\x00" + request.IdempotencyKey
}

func (memory *memoryImportBirth) FindAllocation(_ context.Context, request recordbirth.Request) (recordbirth.Allocation, bool, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	memory.finds++
	allocation, found := memory.byKey[importBirthKey(request)]
	if found && allocation.Request != request {
		return recordbirth.Allocation{}, false, recordbirth.ErrConflict
	}
	return allocation, found, nil
}

func (*memoryImportBirth) CurrentBinding(context.Context) (recordbirth.Binding, bool, error) {
	return recordbirth.Binding{}, false, nil
}

func (*memoryImportBirth) ContinuityBinding(context.Context) (recordbirth.Binding, bool, error) {
	return recordbirth.Binding{}, false, nil
}

func (*memoryImportBirth) CheckpointProof(context.Context, recordbirth.Request, recordbirth.CheckpointHead) ([]byte, error) {
	return nil, recordbirth.ErrUnavailable
}

func (memory *memoryImportBirth) BeginAllocation(_ context.Context, request recordbirth.Request, binding *recordbirth.Binding, proof []byte) (recordbirth.Allocation, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	if binding != nil || len(proof) != 0 {
		return recordbirth.Allocation{}, recordbirth.ErrInvalid
	}
	if existing, ok := memory.byKey[importBirthKey(request)]; ok {
		if existing.Request != request {
			return recordbirth.Allocation{}, recordbirth.ErrConflict
		}
		return existing, nil
	}
	memory.begins++
	value := fmt.Sprintf("%032x", memory.begins)
	allocation := recordbirth.Allocation{
		AllocationID: "rqa_" + value, ReservationID: "rba_" + value, RecordID: "rec2_" + value,
		Domain: recordidentity.Ordinary, Request: request, State: recordbirth.StateAllocated,
	}
	memory.byKey[importBirthKey(request)] = allocation
	memory.byID[allocation.AllocationID] = allocation
	return allocation, nil
}

func (*memoryImportBirth) RegisterReceipt(context.Context, recordbirth.Request, []byte, []byte) (recordbirth.Allocation, error) {
	return recordbirth.Allocation{}, recordbirth.ErrUnavailable
}
func (*memoryImportBirth) PrepareAllocation(context.Context, recordbirth.Request, recordbirth.InitialCreation, []byte) (recordbirth.Allocation, error) {
	return recordbirth.Allocation{}, recordbirth.ErrUnavailable
}
func (*memoryImportBirth) CommittedCreation(context.Context, string) (recordbirth.Allocation, error) {
	return recordbirth.Allocation{}, recordbirth.ErrUnavailable
}
func (*memoryImportBirth) VerifyCertification(context.Context, string, recordbirth.Observation) (recordbirth.Allocation, error) {
	return recordbirth.Allocation{}, recordbirth.ErrUnavailable
}
func (memory *memoryImportBirth) GetAllocation(_ context.Context, actor recordauth.ActorScope, allocationID string) (recordbirth.Allocation, error) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	memory.gets++
	allocation, ok := memory.byID[allocationID]
	if !ok || allocation.Request.ActorID != actor.UserID || allocation.Request.ProjectID != string(actor.ProjectID) {
		return recordbirth.Allocation{}, recordbirth.ErrUnavailable
	}
	return allocation, nil
}

func (memory *memoryImportBirth) finish(requests []records.ImportDocumentRequest) {
	memory.mu.Lock()
	defer memory.mu.Unlock()
	for _, request := range requests {
		if request.Allocation == nil {
			continue
		}
		allocation := memory.byID[request.Allocation.AllocationID]
		allocation.Creation = recordbirth.InitialCreation{
			ReservationID: allocation.ReservationID, RecordID: allocation.RecordID,
			RevisionID: "rrv_imported", AuthorID: allocation.Request.ActorID,
			CanonicalHash: sha256.Sum256([]byte("imported revision")), RequestFingerprint: sha256.Sum256([]byte("imported request")),
		}
		allocation.State = recordbirth.StateFinished
		memory.byID[allocation.AllocationID] = allocation
		memory.byKey[importBirthKey(allocation.Request)] = allocation
	}
}

type importReplayAuthorizationStub struct{}

func (importReplayAuthorizationStub) ResolveCurrentRecordAuthorization(ctx context.Context, actor recordauth.ActorScope, recordID string) (records.CurrentRecordAuthorization, error) {
	values, err := records.ImportedRevisionValues(actor, "title", "body")
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}
	resolved, err := (importTestSubjectAdapter{}).Resolve(ctx, actor, testImportDestination())
	if err != nil {
		return records.CurrentRecordAuthorization{}, err
	}
	return records.CurrentRecordAuthorization{
		RecordID: recordID, CurrentRevisionID: "rrv_imported", LockVersion: 1,
		AuthorizationEpoch: 1, Lifecycle: records.LifecycleActive,
		Evidence: records.RecordAuthorizationEvidence{ProjectID: actor.ProjectID, Visibility: values.VisibilityScope, Sources: []recordauth.SourceAuthorization{resolved.CaptureAuthorization}},
	}, nil
}

func attachImportBirth(t *testing.T, service *Service, repository *memoryImportRepository) {
	t.Helper()
	coordinator, err := recordbirth.NewCoordinator(repository.birth, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.birthCoordinator = coordinator
	service.birthAllocations = repository.birth
	service.importAuthorization = importReplayAuthorizationStub{}
}

type importOrderingWriter struct {
	attachments.BlobWriter
	repository *memoryImportRepository
	calls      int
}

func (writer *importOrderingWriter) Put(ctx context.Context, request attachments.WriteRequest, reader io.Reader) (attachments.WrittenBlob, error) {
	writer.repository.mu.Lock()
	job, ok := writer.repository.jobs[request.Origin.ID]
	plan, planned := writer.repository.plans[job.PlanID]
	valid := ok && planned
	targets := make(map[string]bool)
	for _, remap := range plan.Remaps {
		if remap.EntityKind == "record" {
			valid = valid && remap.BirthAllocationID != ""
			targets[remap.TargetID] = true
		}
	}
	for _, remap := range plan.Remaps {
		if remap.EntityKind == "evidence" || remap.EntityKind == "attachment" {
			valid = valid && targets[remap.OwnerTargetRecordID]
		}
	}
	writer.repository.mu.Unlock()
	if !valid {
		return attachments.WrittenBlob{}, fmt.Errorf("content write before durable allocation remaps")
	}
	writer.calls++
	return writer.BlobWriter.Put(ctx, request, reader)
}

func TestImportReservationSurvivesUnknownPlanCommitAndAppliedReplay(t *testing.T) {
	service, importer, repository := mustImportService(t)
	writer := &importOrderingWriter{BlobWriter: service.attachmentWriter, repository: repository}
	service.attachmentWriter = writer
	service.staging = NewLeasedBlobStore(service.attachmentBlobs, writer)
	archive := mustImportArchive(t, []ArchiveEntry{
		{Path: "records/rec_sourceone/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# One\n")},
		{Path: "records/rec_sourcetwo/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Two\n")},
		{Path: "records/rec_sourcetwo/attachments/att_source/notes.txt", Classification: ArchiveClassAttachment, Payload: []byte("notes\n")},
	})
	actor := portabilityTestActor(t)
	request := DryRunRequest{Actor: actor, DestinationSubject: testImportDestination(), IdempotencyKey: "unknown-import-plan", Archive: archive}
	repository.failSaveAfterPersist = true
	if _, err := service.DryRun(context.Background(), request); err == nil {
		t.Fatal("expected lost plan commit response")
	}
	if writer.calls != 0 || repository.birth.begins != 2 || len(repository.plans) != 1 {
		t.Fatalf("before retry: writes=%d allocations=%d plans=%d", writer.calls, repository.birth.begins, len(repository.plans))
	}
	firstPlan := repository.plans["rip_memory1"]
	firstIDs := appliedImportRecordIDs(firstPlan.Remaps)
	if len(firstIDs) != 2 {
		t.Fatalf("persisted targets = %v", firstIDs)
	}
	var sourceTwoTarget, attachmentOwner string
	for _, remap := range firstPlan.Remaps {
		if remap.EntityKind == "record" && remap.SourceID == "rec_sourcetwo" {
			sourceTwoTarget = remap.TargetID
		}
		if remap.EntityKind == "attachment" && remap.SourceID == "att_source" {
			attachmentOwner = remap.OwnerTargetRecordID
		}
	}
	if sourceTwoTarget == "" || attachmentOwner != sourceTwoTarget {
		t.Fatalf("persisted attachment owner=%q, target=%q", attachmentOwner, sourceTwoTarget)
	}
	wantAllocations := make([]string, 0, 2)
	for _, remap := range firstPlan.Remaps {
		if remap.EntityKind == "record" {
			wantAllocations = append(wantAllocations, remap.BirthAllocationID)
		}
	}
	preview, err := service.DryRun(context.Background(), request)
	if err != nil {
		t.Fatalf("retry DryRun: %v", err)
	}
	if repository.birth.begins != 2 || writer.calls != 1 || !reflect.DeepEqual(firstIDs, appliedImportRecordIDs(repository.plans[preview.PlanID].Remaps)) {
		t.Fatalf("retry changed mapping or allocation: begins=%d writes=%d plan=%+v", repository.birth.begins, writer.calls, repository.plans[preview.PlanID])
	}
	result, err := service.Apply(context.Background(), ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.CertificationPending || !reflect.DeepEqual(result.RecordIDs, firstIDs) || importer.writes != 2 || !reflect.DeepEqual(importer.allocationIDs, wantAllocations) {
		t.Fatalf("applied records=%v allocations=%v writes=%d", result.RecordIDs, importer.allocationIDs, importer.writes)
	}
	beforeFind, beforeGet, beforeWrites, beforeReads := repository.birth.finds, repository.birth.gets, writer.calls, repository.artifactReads
	service.mu.Lock()
	service.importPlans = map[string]cachedImportPlan{}
	service.mu.Unlock()
	delete(repository.artifacts, "rij_memory1")
	service.staging = nil
	service.birthCoordinator = nil
	// Applied replay still checks the original committed APP allocations.
	service.importer = nil
	replayed, err := service.Apply(context.Background(), ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion})
	if err != nil || replayed.CertificationPending || !reflect.DeepEqual(replayed.RecordIDs, result.RecordIDs) {
		t.Fatalf("applied replay = %+v, %v; original %+v", replayed, err, result)
	}
	replayedPlan, err := service.DryRun(context.Background(), request)
	if err != nil || replayedPlan.CertificationPending || replayedPlan.PlanID != preview.PlanID || replayedPlan.JobState != "applied" {
		t.Fatalf("applied DryRun replay = %+v, %v", replayedPlan, err)
	}
	if repository.birth.finds != beforeFind || repository.birth.gets-beforeGet != 4 || writer.calls != beforeWrites || repository.artifactReads != beforeReads || importer.writes != 2 {
		t.Fatalf("replay made allocation/content effects: find=%d get=%d writes=%d artifactReads=%d", repository.birth.finds-beforeFind, repository.birth.gets-beforeGet, writer.calls-beforeWrites, repository.artifactReads-beforeReads)
	}
}

func TestImportPlanDigestConflictAcrossReplayPaths(t *testing.T) {
	for _, path := range []string{"cached_apply", "restarted_apply", "dry_run_replay"} {
		t.Run(path, func(t *testing.T) {
			service, importer, repository := mustImportService(t)
			request := DryRunRequest{
				Actor: portabilityTestActor(t), DestinationSubject: testImportDestination(),
				IdempotencyKey: "plan-digest-conflict",
				Archive:        mustImportArchive(t, []ArchiveEntry{{Path: "records/rec_source/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Source\n")}}),
			}
			preview, err := service.DryRun(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			plan := repository.plans[preview.PlanID]
			plan.PlanDigest[0] ^= 0xff
			repository.plans[preview.PlanID] = plan
			beforeBegins := repository.birth.begins
			if path == "restarted_apply" {
				service.importPlans = map[string]cachedImportPlan{}
			}
			if path == "dry_run_replay" {
				_, err = service.DryRun(context.Background(), request)
			} else {
				_, err = service.Apply(context.Background(), ApplyRequest{Actor: request.Actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion})
			}
			if !errors.Is(err, ErrImportCASConflict) {
				t.Fatalf("plan digest conflict = %v, want %v", err, ErrImportCASConflict)
			}
			if repository.birth.begins != beforeBegins || importer.writes != 0 || len(repository.origins) != 0 {
				t.Fatal("conflicting plan allocated or published content")
			}
		})
	}
}

func TestImportRejectsMismatchedOrMissingBirthBeforeContent(t *testing.T) {
	for _, scenario := range []string{"owner", "archive", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			service, importer, repository := mustImportService(t)
			actor := portabilityTestActor(t)
			archive := mustImportArchive(t, []ArchiveEntry{{Path: "records/rec_source/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Source\n")}})
			preview, err := service.DryRun(context.Background(), DryRunRequest{Actor: actor, DestinationSubject: testImportDestination(), IdempotencyKey: "binding-check", Archive: archive})
			if err != nil {
				t.Fatal(err)
			}
			plan := repository.plans[preview.PlanID]
			switch scenario {
			case "owner":
				id := plan.Remaps[0].BirthAllocationID
				allocation := repository.birth.byID[id]
				allocation.Request.ActorID = "usr_different"
				repository.birth.byID[id] = allocation
			case "archive":
				job := repository.jobs[plan.ImportJobID]
				job.ArchiveDigest = sha256.Sum256([]byte("another archive"))
				repository.jobs[job.ImportJobID] = job
			case "legacy":
				plan.Remaps[0].BirthAllocationID = ""
				plan.Remaps[0].TargetID = "rec_legacy"
				repository.plans[preview.PlanID] = plan
			}
			beforeReads, beforeBegins := repository.artifactReads, repository.birth.begins
			_, err = service.Apply(context.Background(), ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion})
			if err == nil || (scenario == "legacy" && !errors.Is(err, ErrImportCASConflict)) {
				t.Fatalf("Apply(%s) error = %v", scenario, err)
			}
			if repository.artifactReads != beforeReads || repository.birth.begins != beforeBegins || importer.writes != 0 {
				t.Fatalf("rejected binding performed content or allocation effects")
			}
		})
	}
}

func TestImportAppliedLegacyPlanReplaysWithoutBirthOrArchive(t *testing.T) {
	service, importer, repository := mustImportService(t)
	actor := portabilityTestActor(t)
	destination := testImportDestination()
	repository.jobs["rij_legacy"] = store.RecordImportJob{
		ImportJobID: "rij_legacy", PlanID: "rip_legacy", ActorID: actor.UserID,
		JobState: store.RecordImportJobStateApplied, LockVersion: 3,
		ArchiveDigest: sha256.Sum256([]byte("legacy archive")), DestinationSubject: destination,
	}
	repository.plans["rip_legacy"] = store.RecordImportPlan{
		ImportPlanID: "rip_legacy", ImportJobID: "rij_legacy", PlanDigest: sha256.Sum256([]byte("legacy plan")),
		RemapCount: 3, DestinationSubject: destination,
		Remaps: []store.ImportRemap{
			{EntityKind: "record", SourceID: "rec_original", TargetID: "rec_existing"},
			{EntityKind: "evidence", SourceID: "evs_original", TargetID: "evs_existing"},
			{EntityKind: "attachment", SourceID: "att_original", TargetID: "att_existing"},
		},
	}
	service.staging = nil
	service.birthCoordinator = nil
	service.birthAllocations = nil
	service.importer = nil
	result, err := service.Apply(context.Background(), ApplyRequest{Actor: actor, PlanID: "rip_legacy", LockVersion: 2})
	if err != nil || result.CertificationPending || !reflect.DeepEqual(result.RecordIDs, []string{"rec_existing"}) {
		t.Fatalf("legacy replay = %+v, %v", result, err)
	}
	if importer.writes != 0 || repository.birth.finds != 0 || repository.birth.gets != 0 || repository.artifactReads != 0 {
		t.Fatal("legacy replay allocated or loaded content")
	}
}

func TestImportPersistsArchiveDerivedContentOwners(t *testing.T) {
	service, importer, repository := mustImportService(t)
	writer := &importOrderingWriter{BlobWriter: service.attachmentWriter, repository: repository}
	service.attachmentWriter = writer
	service.staging = NewLeasedBlobStore(service.attachmentBlobs, writer)
	evidenceWriter := &evidenceImportStub{}
	service.evidenceImports = evidenceWriter
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := mustPortabilityComparisonSnapshot(t, kind)
	wrapped, err := encodeOfficialEvidenceRestoreMember(snapshot, snapshot.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	archive := mustImportArchive(t, []ArchiveEntry{
		{Path: "records/rec_alpha/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Alpha\n")},
		{Path: "records/rec_beta/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Beta\n")},
		{Path: "records/rec_alpha/evidence/evs_source.json", Classification: ArchiveClassEvidenceJSON, Payload: wrapped},
		{Path: "records/rec_beta/attachments/att_source/notes.txt", Classification: ArchiveClassAttachment, Payload: []byte("notes\n")},
	})
	preview, err := service.DryRun(context.Background(), DryRunRequest{Actor: portabilityTestActor(t), DestinationSubject: testImportDestination(), IdempotencyKey: "content-owners", Archive: archive})
	if err != nil {
		t.Fatal(err)
	}
	plan := repository.plans[preview.PlanID]
	targets := make(map[string]string)
	owners := make(map[string]string)
	for _, remap := range plan.Remaps {
		switch remap.EntityKind {
		case "record":
			targets[remap.SourceID] = remap.TargetID
		case "evidence", "attachment":
			owners[remap.SourceID] = remap.OwnerTargetRecordID
		}
	}
	if targets["rec_alpha"] == "" || targets["rec_beta"] == "" ||
		owners["evs_source"] != targets["rec_alpha"] || owners["att_source"] != targets["rec_beta"] {
		t.Fatalf("targets=%v content owners=%v", targets, owners)
	}
	cached := service.importPlans[preview.PlanID]
	planned := plannedArchive{destination: cached.destination, documents: cached.documents, evidence: cached.evidence,
		attachments: cached.attachments, remaps: cached.remaps, quarantine: cached.quarantine,
		allocationIDs: cached.allocationIDs, ownerTargetIDs: cached.ownerTargetIDs}
	before := importPlanDigest(planned)
	planned.ownerTargetIDs = map[string]string{"evidence\x00evs_source": targets["rec_beta"]}
	planned.evidence = append([]importedEvidencePlan(nil), planned.evidence...)
	planned.evidence[0].ownerOmitted = !planned.evidence[0].ownerOmitted
	if after := importPlanDigest(planned); before != plan.PlanDigest || after != before {
		t.Fatal("internal owner metadata changed the frozen plan digest")
	}
	plan.Remaps = append([]store.ImportRemap(nil), plan.Remaps...)
	for index := range plan.Remaps {
		if plan.Remaps[index].EntityKind == "evidence" {
			plan.Remaps[index].OwnerTargetRecordID = targets["rec_beta"]
		}
	}
	repository.plans[preview.PlanID] = plan
	beforeWrites := writer.calls
	if _, err := service.Apply(context.Background(), ApplyRequest{Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion}); err == nil {
		t.Fatal("Apply accepted evidence reparented to another record")
	}
	if writer.calls != beforeWrites || importer.writes != 0 || len(evidenceWriter.calls) != 0 {
		t.Fatal("reparented evidence produced an import effect")
	}
}

func TestImportRejectsChangedContentOwnersBeforeWrites(t *testing.T) {
	for _, scenario := range []string{"missing", "other-record-cached", "other-record-restarted", "other-record-dryrun"} {
		t.Run(scenario, func(t *testing.T) {
			service, importer, repository := mustImportService(t)
			writer := &importOrderingWriter{BlobWriter: service.attachmentWriter, repository: repository}
			service.attachmentWriter = writer
			service.staging = NewLeasedBlobStore(service.attachmentBlobs, writer)
			actor := portabilityTestActor(t)
			archive := mustImportArchive(t, []ArchiveEntry{
				{Path: "records/rec_alpha/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Alpha\n")},
				{Path: "records/rec_beta/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Beta\n")},
				{Path: "records/rec_beta/attachments/att_source/notes.txt", Classification: ArchiveClassAttachment, Payload: []byte("notes\n")},
			})
			request := DryRunRequest{Actor: actor, DestinationSubject: testImportDestination(), IdempotencyKey: "tampered-owner", Archive: archive}
			preview, err := service.DryRun(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			plan := repository.plans[preview.PlanID]
			plan.Remaps = append([]store.ImportRemap(nil), plan.Remaps...)
			other := ""
			for _, remap := range plan.Remaps {
				if remap.EntityKind == "record" && remap.SourceID == "rec_alpha" {
					other = remap.TargetID
				}
			}
			for index := range plan.Remaps {
				if plan.Remaps[index].EntityKind == "attachment" {
					plan.Remaps[index].OwnerTargetRecordID = other
					if scenario == "missing" {
						plan.Remaps[index].OwnerTargetRecordID = ""
					}
				}
			}
			repository.plans[preview.PlanID] = plan
			if scenario == "other-record-restarted" {
				service.importPlans = map[string]cachedImportPlan{}
			}
			beforeWrites := writer.calls
			if scenario == "other-record-dryrun" {
				if _, err := service.DryRun(context.Background(), request); err == nil {
					t.Fatal("DryRun replay accepted changed content owner")
				}
			} else if _, err := service.Apply(context.Background(), ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion}); err == nil {
				t.Fatal("Apply accepted changed content owner")
			}
			if writer.calls != beforeWrites || importer.writes != 0 {
				t.Fatalf("changed owner wrote content: blobs=%d records=%d", writer.calls-beforeWrites, importer.writes)
			}
		})
	}
}

func TestImportOwnerlessLegacyEvidenceRequiresOneDocument(t *testing.T) {
	kind, err := evidence.NewComparisonResultKind()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := mustPortabilityComparisonSnapshot(t, kind)
	wrapped, err := encodeOfficialEvidenceRestoreMember(snapshot, snapshot.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name       string
		path       string
		twoRecords bool
		wantOK     bool
	}{
		{name: "legacy one record", path: "evidence/evs_source.json", wantOK: true},
		{name: "legacy ambiguous", path: "evidence/evs_source.json", twoRecords: true},
		{name: "different record", path: "records/rec_other/evidence/evs_source.json"},
		{name: "malformed record", path: "records/rec_bad_/evidence/evs_source.json"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			service, importer, repository := mustImportService(t)
			service.kinds = mustOfficialImportKinds(t, officialProbeDescriptor())
			entries := []ArchiveEntry{{Path: "records/rec_alpha/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Alpha\n")},
				{Path: scenario.path, Classification: ArchiveClassEvidenceJSON, Payload: wrapped}}
			if scenario.twoRecords {
				entries = append(entries, ArchiveEntry{Path: "records/rec_beta/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Beta\n")})
			}
			preview, err := service.DryRun(context.Background(), DryRunRequest{Actor: portabilityTestActor(t), DestinationSubject: testImportDestination(), IdempotencyKey: "legacy-owner", Archive: mustImportArchive(t, entries)})
			if scenario.wantOK {
				if err != nil {
					t.Fatal(err)
				}
				var target, owner string
				for _, remap := range repository.plans[preview.PlanID].Remaps {
					if remap.EntityKind == "record" {
						target = remap.TargetID
					}
					if remap.EntityKind == "evidence" {
						owner = remap.OwnerTargetRecordID
					}
				}
				if target == "" || owner != target {
					t.Fatalf("owner=%q target=%q", owner, target)
				}
				applied, err := service.Apply(context.Background(), ApplyRequest{Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion})
				if err != nil || len(applied.RecordIDs) != 1 || applied.RecordIDs[0] != target || len(importer.preparations) != 1 || len(importer.preparations[0].Imported()) != 1 {
					t.Fatalf("legacy evidence apply=%+v preparations=%v err=%v", applied, importer.preparations, err)
				}
			} else if err == nil || len(repository.plans) != 0 {
				t.Fatalf("invalid owner produced plan: preview=%+v err=%v", preview, err)
			}
		})
	}
}

type partlyPendingImportWriter struct {
	*importWriterStub
}

func (writer *partlyPendingImportWriter) ImportDocumentsFinishing(ctx context.Context, requests []records.ImportDocumentRequest, finish records.RevisionCommitFinish) ([]records.ImportedDocument, error) {
	written, err := writer.importWriterStub.ImportDocumentsFinishing(ctx, requests, finish)
	if err == nil && len(written) > 1 {
		written[1].CertificationPending = true
	}
	return written, err
}

func TestImportApplyReportsPendingFromAnyImportedDocument(t *testing.T) {
	service, importer, _ := mustImportService(t)
	service.importer = &partlyPendingImportWriter{importer}
	actor := portabilityTestActor(t)
	preview, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: actor, DestinationSubject: testImportDestination(), IdempotencyKey: "partly-pending-import",
		Archive: mustImportArchive(t, []ArchiveEntry{
			{Path: "records/rec_alpha/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Alpha\n")},
			{Path: "records/rec_beta/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Beta\n")},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion})
	if err != nil || !result.CertificationPending || len(result.RecordIDs) != 2 {
		t.Fatalf("partly pending Apply = %+v, %v", result, err)
	}
}

func TestImportAppliedReplayRejectsChangedAllocationIdentity(t *testing.T) {
	for _, scenario := range []string{"job", "archive", "source", "destination", "target", "purpose", "owner", "fingerprint", "allocation"} {
		t.Run(scenario, func(t *testing.T) {
			service, importer, repository := mustImportService(t)
			actor := portabilityTestActor(t)
			request := DryRunRequest{
				Actor: actor, DestinationSubject: testImportDestination(), IdempotencyKey: "applied-binding-" + scenario,
				Archive: mustImportArchive(t, []ArchiveEntry{{Path: "records/rec_source/document.md", Classification: ArchiveClassMarkdown, Payload: []byte("# Source\n")}}),
			}
			preview, err := service.DryRun(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			apply := ApplyRequest{Actor: actor, PlanID: preview.PlanID, LockVersion: preview.LockVersion}
			if _, err := service.Apply(context.Background(), apply); err != nil {
				t.Fatal(err)
			}
			plan := repository.plans[preview.PlanID]
			plan.Remaps = append([]store.ImportRemap(nil), plan.Remaps...)
			job := repository.jobs[plan.ImportJobID]
			allocation := repository.birth.byID[plan.Remaps[0].BirthAllocationID]
			switch scenario {
			case "job":
				job.ImportJobID = "rij_changed"
			case "archive":
				job.ArchiveDigest = sha256.Sum256([]byte("different archive"))
			case "source":
				plan.Remaps[0].SourceID = "rec_other"
			case "destination":
				job.DestinationSubject.SourceID = "tg_0123456789abcdea"
				plan.DestinationSubject = job.DestinationSubject
				request.DestinationSubject = job.DestinationSubject
			case "target":
				plan.Remaps[0].TargetID = "rec2_000000000000000000000000000000ff"
			case "purpose":
				allocation.Request.Purpose = recordbirth.PurposeCreate
			case "owner":
				allocation.Request.ActorID = "usr_different"
			case "fingerprint":
				allocation.Request.RequestFingerprint = sha256.Sum256([]byte("different request"))
			case "allocation":
				plan.Remaps[0].BirthAllocationID = "rqa_000000000000000000000000000000ff"
			}
			repository.plans[preview.PlanID] = plan
			repository.jobs[plan.ImportJobID] = job
			repository.birth.byID[allocation.AllocationID] = allocation
			beforeReads, beforeBegins, beforeWrites := repository.artifactReads, repository.birth.begins, importer.writes
			if _, err := service.Apply(context.Background(), apply); err == nil {
				t.Fatalf("Apply accepted changed %s", scenario)
			}
			if _, err := service.DryRun(context.Background(), request); err == nil {
				t.Fatalf("DryRun accepted changed %s", scenario)
			}
			if repository.artifactReads != beforeReads || repository.birth.begins != beforeBegins || importer.writes != beforeWrites {
				t.Fatalf("changed %s caused archive, allocation or content I/O", scenario)
			}
		})
	}
}
