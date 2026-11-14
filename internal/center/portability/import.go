package portability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

var (
	importedReferenceCommentPattern = regexp.MustCompile(`^<!-- houfeng-ref:v1 (evidence|attachment) ([A-Za-z0-9][A-Za-z0-9_-]{0,127}) -->$`)
	importedReferenceLinkPattern    = regexp.MustCompile(`^\[(.+)\]\(houfeng-(evidence|attachment):([A-Za-z0-9][A-Za-z0-9_-]{0,127})\)$`)
)

func importRemapKey(entityKind, sourceID string) string {
	return entityKind + "\x00" + sourceID
}

func importRemapTable(remaps []store.ImportRemap) (map[string]store.ImportRemap, error) {
	table := make(map[string]store.ImportRemap, len(remaps))
	for _, remap := range remaps {
		if remap.EntityKind == "" || remap.SourceID == "" || remap.TargetID == "" {
			return nil, ErrInvalidImportRequest
		}
		key := importRemapKey(remap.EntityKind, remap.SourceID)
		if _, ok := table[key]; ok {
			return nil, ErrInvalidImportRequest
		}
		table[key] = remap
	}
	return table, nil
}

func sameImportRemaps(left, right []store.ImportRemap) bool {
	if len(left) != len(right) {
		return false
	}
	items := make(map[string]store.ImportRemap, len(left))
	for _, remap := range left {
		key := importRemapKey(remap.EntityKind, remap.SourceID)
		if _, exists := items[key]; exists {
			return false
		}
		items[key] = remap
	}
	for _, remap := range right {
		key := importRemapKey(remap.EntityKind, remap.SourceID)
		if items[key] != remap {
			return false
		}
		delete(items, key)
	}
	return len(items) == 0
}

// rebindImportedMarkdownReferences keeps archive links readable while ensuring
// custom evidence/attachment links never retain source IDs or point at an
// unavailable member. Included members are rewritten to their planned target
// IDs; unavailable members keep only their visible label.
func rebindImportedMarkdownReferences(body string, remaps map[string]store.ImportRemap) string {
	lines := strings.Split(body, "\n")
	rewritten := make([]string, 0, len(lines))
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		comment := importedReferenceCommentPattern.FindStringSubmatch(strings.TrimSpace(line))
		if comment != nil {
			if index+1 < len(lines) {
				link := importedReferenceLinkPattern.FindStringSubmatch(strings.TrimSpace(lines[index+1]))
				if link != nil && comment[1] == link[2] && comment[2] == link[3] {
					if remap, ok := remaps[importRemapKey(comment[1], comment[2])]; ok {
						rewritten = append(rewritten,
							"<!-- houfeng-ref:v1 "+comment[1]+" "+remap.TargetID+" -->",
							"["+link[1]+"](houfeng-"+link[2]+":"+remap.TargetID+")",
						)
					} else {
						rewritten = append(rewritten, link[1])
					}
					index++
					continue
				}
			}
			continue
		}
		if link := importedReferenceLinkPattern.FindStringSubmatch(strings.TrimSpace(line)); link != nil {
			rewritten = append(rewritten, link[1])
			continue
		}
		rewritten = append(rewritten, line)
	}
	return strings.Join(rewritten, "\n")
}

type DocumentImporter interface {
	ImportDocuments(context.Context, []records.ImportDocumentRequest) ([]records.ImportedDocument, error)
	ImportDocumentsFinishing(context.Context, []records.ImportDocumentRequest, records.RevisionCommitFinish) ([]records.ImportedDocument, error)
}

type ImportedEvidenceRequest struct {
	Actor            recordauth.ActorScope
	TargetRecordID   string
	TargetSnapshotID string
	Schema           string
	Payload          []byte
}

type EvidenceImporter interface {
	ImportExportedEvidence(context.Context, ImportedEvidenceRequest) error
}

type ImportProjectionRebuilder interface {
	RebuildImportedRecord(context.Context, string) error
}

type ImportRebuildFunc func(context.Context, string) error

func (function ImportRebuildFunc) RebuildImportedRecord(ctx context.Context, recordID string) error {
	if function == nil {
		return nil
	}
	return function(ctx, recordID)
}

// authoritativeProjectionRebuilder is the production import rebuild hook.
// Search and activity already project inside the same SaveRevision transaction
// as ImportDocument; this named type keeps that contract grep-able and refuses
// empty record IDs. Checkpoints are never imported.
type authoritativeProjectionRebuilder struct{}

type knownKindEvidenceImporter struct{}

func NewKnownKindEvidenceImporter() EvidenceImporter {
	return knownKindEvidenceImporter{}
}

func (knownKindEvidenceImporter) ImportExportedEvidence(_ context.Context, request ImportedEvidenceRequest) error {
	if request.TargetRecordID == "" || request.TargetSnapshotID == "" || len(request.Payload) == 0 ||
		!knownLocalImportSchema(request.Schema) {
		return ErrImportSchemaBlocked
	}
	return nil
}

func NewAuthoritativeProjectionRebuilder() ImportProjectionRebuilder {
	return authoritativeProjectionRebuilder{}
}

func (authoritativeProjectionRebuilder) RebuildImportedRecord(ctx context.Context, recordID string) error {
	if ctx == nil || recordID == "" {
		return ErrInvalidImportRequest
	}
	return nil
}

type ImportRepository interface {
	ClaimImportJob(context.Context, store.ClaimRecordImportJobInput) (store.RecordImportJob, error)
	SaveImportPlan(context.Context, store.SaveRecordImportPlanInput) (store.RecordImportPlan, error)
	LoadImportPlan(context.Context, string) (store.RecordImportPlan, error)
	LoadImportJob(context.Context, string) (store.RecordImportJob, error)
	CheckImportReplayReadFence(context.Context, recordauth.ActorScope, records.SubjectReference, []records.CurrentRecordAuthorization) error
	PublishImportArtifact(context.Context, store.PublishRecordImportArtifactInput) (store.RecordImportArtifact, error)
	LoadImportArtifact(context.Context, string) (store.RecordImportArtifact, error)
	AdvanceImportJob(context.Context, store.AdvanceRecordImportJobInput) error
	LoadOriginTombstone(context.Context, [32]byte) (store.RecordOriginTombstone, error)
	LoadOrigin(context.Context, [32]byte) (store.RecordOrigin, error)
	InsertOrigin(context.Context, store.InsertRecordOriginInput) (store.RecordOrigin, error)
}

type DryRunRequest struct {
	Actor              recordauth.ActorScope
	IdempotencyKey     string
	Archive            []byte
	DestinationSubject records.SubjectReference
}

type ImportRemap struct {
	EntityKind string `json:"entity_kind"`
	SourceID   string `json:"source_id"`
	TargetID   string `json:"target_id"`
}

type QuarantinedEvidence struct {
	Kind       string `json:"kind"`
	Schema     string `json:"schema"`
	Digest     string `json:"digest"`
	ByteSize   int64  `json:"byte_size"`
	Reason     string `json:"reason"`
	ObservedAt string `json:"observed_at,omitempty"`
}

type ImportDestinationSubjectView struct {
	SubjectKind records.SubjectKind `json:"subject_kind"`
	SubjectID   string              `json:"subject_id"`
}

type ImportPlanView struct {
	PlanID               string                       `json:"plan_id"`
	JobState             string                       `json:"job_state"`
	LockVersion          uint64                       `json:"lock_version"`
	Remaps               []ImportRemap                `json:"remaps"`
	Quarantine           []QuarantinedEvidence        `json:"quarantine"`
	ObjectCount          int                          `json:"object_count"`
	ExpiresAt            string                       `json:"expires_at"`
	DestinationSubject   ImportDestinationSubjectView `json:"destination_subject"`
	CertificationPending bool                         `json:"certification_pending,omitempty"`
}

type ApplyRequest struct {
	Actor       recordauth.ActorScope
	PlanID      string
	LockVersion uint64
}

type ApplyResult struct {
	PlanID               string   `json:"plan_id"`
	JobState             string   `json:"job_state"`
	RecordIDs            []string `json:"record_ids"`
	CertificationPending bool     `json:"certification_pending,omitempty"`
}

func (service *Service) DryRun(ctx context.Context, request DryRunRequest) (ImportPlanView, error) {
	if err := service.requireEnabled(); err != nil {
		return ImportPlanView{}, err
	}
	if service.imports == nil || ctx == nil || request.IdempotencyKey == "" || len(request.Archive) == 0 {
		return ImportPlanView{}, ErrInvalidImportRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return ImportPlanView{}, ErrInvalidImportRequest
	}
	destination := request.DestinationSubject
	if err := service.authorizeImportDestination(ctx, actor, destination); err != nil {
		return ImportPlanView{}, err
	}
	digest := sha256.Sum256(request.Archive)
	expiresAt := service.now().Add(service.previewTTL)
	job, err := service.imports.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
		ActorID:            actor.UserID,
		IdempotencyKey:     request.IdempotencyKey,
		ArchiveDigest:      digest,
		DestinationSubject: destination,
		ExpiresAt:          expiresAt,
	})
	if err != nil {
		return ImportPlanView{}, mapImportError(err)
	}
	if job.ActorID != actor.UserID || job.ArchiveDigest != digest || job.DestinationSubject != destination {
		return ImportPlanView{}, ErrImportCASConflict
	}
	if job.PlanID != "" {
		existing, err := service.imports.LoadImportPlan(ctx, job.PlanID)
		if err != nil {
			return ImportPlanView{}, mapImportError(err)
		}
		if existing.ImportPlanID != job.PlanID || existing.ImportJobID != job.ImportJobID || existing.DestinationSubject != destination || uint64(len(existing.Remaps)) != existing.RemapCount {
			return ImportPlanView{}, ErrImportCASConflict
		}
		if job.JobState == store.RecordImportJobStateApplied {
			if err := service.authorizeAppliedImport(ctx, actor, destination, existing.Remaps); err != nil {
				return ImportPlanView{}, err
			}
			pending, err := service.confirmAppliedImport(ctx, actor, job, existing.Remaps)
			if err != nil {
				return ImportPlanView{}, err
			}
			view := service.planView(job, existing, nil)
			view.CertificationPending = pending
			return view, nil
		}
		if job.JobState != store.RecordImportJobStateQuarantined && job.JobState != store.RecordImportJobStatePlanned {
			return ImportPlanView{}, ErrImportCASConflict
		}
		if !job.ExpiresAt.IsZero() && !service.now().Before(job.ExpiresAt) {
			return ImportPlanView{}, ErrInvalidImportRequest
		}
		if _, err := service.importAllocations(ctx, actor, job, existing.Remaps); err != nil {
			return ImportPlanView{}, err
		}
		rebound, err := rebindArchive(request.Archive, existing.Remaps, destination)
		if err != nil {
			return ImportPlanView{}, err
		}
		if rebound.digest != existing.PlanDigest || !sameImportRemaps(rebound.storeRemaps(), existing.Remaps) {
			return ImportPlanView{}, ErrImportCASConflict
		}
		if err := service.ensureImportArchive(ctx, job, request.Archive); err != nil {
			return ImportPlanView{}, err
		}
		if job.JobState == store.RecordImportJobStateQuarantined {
			if err := service.imports.AdvanceImportJob(ctx, store.AdvanceRecordImportJobInput{
				ImportJobID: job.ImportJobID,
				LockVersion: job.LockVersion,
				JobState:    store.RecordImportJobStatePlanned,
			}); err != nil {
				return ImportPlanView{}, mapImportError(err)
			}
			job.JobState = store.RecordImportJobStatePlanned
			job.LockVersion++
		}
		service.cacheImportPlan(job, rebound)
		existing.Remaps = rebound.storeRemaps()
		existing.Documents = rebound.documents
		existing.DestinationSubject = destination
		return service.planView(job, existing, rebound.quarantine), nil
	}
	if job.JobState != store.RecordImportJobStateQuarantined {
		return ImportPlanView{}, ErrImportCASConflict
	}
	if !job.ExpiresAt.IsZero() && !service.now().Before(job.ExpiresAt) {
		return ImportPlanView{}, ErrInvalidImportRequest
	}
	if err := service.rejectArchiveOrigin(ctx, digest); err != nil {
		return ImportPlanView{}, err
	}
	planned, err := service.planArchive(ctx, request.Archive, actor, job)
	if err != nil {
		return ImportPlanView{}, err
	}
	trusted, err := service.importAllocations(ctx, actor, job, planned.storeRemaps())
	if err != nil {
		return ImportPlanView{}, err
	}
	planAllocations := make([]recordbirth.Allocation, 0, len(planned.documents))
	for _, document := range planned.documents {
		allocation, ok := trusted[document.SourceID]
		if !ok || allocation.RecordID != document.TargetID {
			return ImportPlanView{}, ErrImportCASConflict
		}
		planAllocations = append(planAllocations, allocation)
	}
	plan, err := service.imports.SaveImportPlan(ctx, store.SaveRecordImportPlanInput{
		Actor:              actor,
		ArchiveDigest:      digest,
		ImportJobID:        job.ImportJobID,
		PlanDigest:         planned.digest,
		ObjectCount:        uint64(len(planned.documents) + len(planned.evidence) + len(planned.attachments) + len(planned.quarantine)),
		RemapCount:         uint64(len(planned.remaps)),
		Remaps:             planned.storeRemaps(),
		Allocations:        planAllocations,
		Documents:          planned.documents,
		DestinationSubject: destination,
		ExpiresAt:          expiresAt,
	})
	if err != nil {
		return ImportPlanView{}, mapImportError(err)
	}
	stored, err := service.imports.LoadImportPlan(ctx, plan.ImportPlanID)
	if err != nil {
		return ImportPlanView{}, mapImportError(err)
	}
	if stored.ImportJobID != job.ImportJobID || stored.PlanDigest != planned.digest ||
		stored.DestinationSubject != destination || !sameImportRemaps(stored.Remaps, planned.storeRemaps()) {
		return ImportPlanView{}, ErrImportCASConflict
	}
	if _, err := service.importAllocations(ctx, actor, job, stored.Remaps); err != nil {
		return ImportPlanView{}, err
	}
	job.PlanID = plan.ImportPlanID
	if err := service.stageImportArchive(ctx, job, request.Archive); err != nil {
		return ImportPlanView{}, err
	}
	if err := service.imports.AdvanceImportJob(ctx, store.AdvanceRecordImportJobInput{
		ImportJobID: job.ImportJobID,
		LockVersion: job.LockVersion,
		JobState:    store.RecordImportJobStatePlanned,
	}); err != nil {
		return ImportPlanView{}, mapImportError(err)
	}
	job.JobState = store.RecordImportJobStatePlanned
	job.LockVersion++
	service.cacheImportPlan(job, planned)
	plan.Documents = planned.documents
	plan.Remaps = planned.storeRemaps()
	plan.DestinationSubject = destination
	plan.LockVersion = job.LockVersion
	plan.JobState = job.JobState
	return service.planView(job, plan, planned.quarantine), nil
}

func (service *Service) Apply(ctx context.Context, request ApplyRequest) (ApplyResult, error) {
	if err := service.requireEnabled(); err != nil {
		return ApplyResult{}, err
	}
	if service.imports == nil || ctx == nil || request.PlanID == "" {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	persisted, err := service.imports.LoadImportPlan(ctx, request.PlanID)
	if err != nil {
		return ApplyResult{}, mapImportError(err)
	}
	job, err := service.imports.LoadImportJob(ctx, persisted.ImportJobID)
	if err != nil {
		return ApplyResult{}, mapImportError(err)
	}
	if job.ActorID == "" || job.ActorID != actor.UserID {
		return ApplyResult{}, ErrExportUnauthorized
	}
	if persisted.ImportPlanID != request.PlanID || job.PlanID != request.PlanID || isZeroImportDestination(persisted.DestinationSubject) ||
		job.DestinationSubject != persisted.DestinationSubject || uint64(len(persisted.Remaps)) != persisted.RemapCount {
		return ApplyResult{}, ErrImportCASConflict
	}
	if err := service.authorizeImportDestination(ctx, actor, job.DestinationSubject); err != nil {
		return ApplyResult{}, err
	}
	if job.JobState == store.RecordImportJobStateApplied {
		if err := service.authorizeAppliedImport(ctx, actor, job.DestinationSubject, persisted.Remaps); err != nil {
			return ApplyResult{}, err
		}
		pending, err := service.confirmAppliedImport(ctx, actor, job, persisted.Remaps)
		if err != nil {
			return ApplyResult{}, err
		}
		return ApplyResult{PlanID: request.PlanID, JobState: job.JobState, RecordIDs: appliedImportRecordIDs(persisted.Remaps), CertificationPending: pending}, nil
	}
	if service.importer == nil {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	if !job.ExpiresAt.IsZero() && !service.now().Before(job.ExpiresAt) {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	if job.JobState != store.RecordImportJobStatePlanned {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	if request.LockVersion != job.LockVersion {
		return ApplyResult{}, ErrImportCASConflict
	}
	allocations, err := service.importAllocations(ctx, actor, job, persisted.Remaps)
	if err != nil {
		return ApplyResult{}, err
	}
	cached, err := service.resolveImportPlan(ctx, request.PlanID)
	if err != nil {
		return ApplyResult{}, err
	}
	if cached.jobID != job.ImportJobID || cached.actorID != job.ActorID || cached.destination != job.DestinationSubject ||
		cached.archiveDigest != job.ArchiveDigest || cached.digest != persisted.PlanDigest ||
		!sameImportRemaps((plannedArchive{remaps: cached.remaps, allocationIDs: cached.allocationIDs, ownerTargetIDs: cached.ownerTargetIDs}).storeRemaps(), persisted.Remaps) {
		return ApplyResult{}, ErrImportCASConflict
	}
	if len(allocations) != len(cached.documents) {
		return ApplyResult{}, ErrImportCASConflict
	}
	for _, document := range cached.documents {
		allocation, ok := allocations[document.SourceID]
		if !ok || allocation.RecordID != document.TargetID {
			return ApplyResult{}, ErrImportCASConflict
		}
	}
	cached.lockVersion = job.LockVersion
	cached.jobState = job.JobState
	plan := store.RecordImportPlan{
		ImportPlanID:       request.PlanID,
		ImportJobID:        cached.jobID,
		PlanDigest:         cached.digest,
		Documents:          cached.documents,
		LockVersion:        job.LockVersion,
		DestinationSubject: cached.destination,
	}
	originDigest := cached.archiveDigest
	if originDigest == [32]byte{} {
		return ApplyResult{}, ErrImportCASConflict
	}
	if err := service.rejectArchiveOrigin(ctx, originDigest); err != nil {
		return ApplyResult{}, err
	}
	if err := service.applyImportedEvidence(ctx, actor, cached); err != nil {
		return ApplyResult{}, err
	}
	preparations, err := service.prepareImportedEvidence(cached, request.PlanID)
	if err != nil {
		return ApplyResult{}, err
	}
	importedAttachments, attachmentIDs, err := service.restoreImportedAttachments(ctx, actor, cached)
	if err != nil {
		if errors.Is(err, attachments.ErrBlobGCProtected) && !errors.Is(err, attachments.ErrWriteOutcomeUnknown) {
			return ApplyResult{}, ErrImportCASConflict
		}
		return ApplyResult{}, err
	}
	importRequests := make([]records.ImportDocumentRequest, 0, len(plan.Documents))
	for _, document := range plan.Documents {
		allocation := allocations[document.SourceID]
		importRequests = append(importRequests, records.ImportDocumentRequest{
			Actor:               actor,
			RecordID:            document.TargetID,
			Allocation:          &allocation,
			Title:               document.Title,
			BodyMarkdown:        document.Body,
			IdempotencyKey:      "import-" + plan.ImportPlanID + "-" + document.TargetID,
			SubjectReferences:   []records.SubjectReference{cached.destination},
			EvidencePreparation: preparations[document.TargetID],
			AttachmentIDs:       attachmentIDs[document.TargetID],
			ImportedAttachments: importedAttachments[document.TargetID],
		})
	}
	written, err := service.importer.ImportDocumentsFinishing(ctx, importRequests, records.RevisionCommitFinish{
		OriginKind:     "import",
		OriginDigest:   originDigest,
		SourceRecord:   firstLocalRecord(nil, plan.Documents),
		ImportJobID:    plan.ImportJobID,
		JobLockVersion: plan.LockVersion,
		Actor:          actor.Clone(),
	})
	if err != nil {
		if errors.Is(err, attachments.ErrBlobGCProtected) && !errors.Is(err, attachments.ErrWriteOutcomeUnknown) {
			return ApplyResult{}, ErrImportCASConflict
		}
		return ApplyResult{}, mapImportDocumentError(err)
	}
	recordIDs := make([]string, 0, len(written))
	certificationPending := false
	for _, document := range written {
		recordIDs = append(recordIDs, document.RecordID)
		certificationPending = certificationPending || document.CertificationPending
	}
	service.mu.Lock()
	cached.jobState = store.RecordImportJobStateApplied
	cached.applied = recordIDs
	service.importPlans[request.PlanID] = cached
	service.mu.Unlock()
	if service.rebuilder != nil {
		for _, recordID := range recordIDs {
			if err := service.rebuilder.RebuildImportedRecord(ctx, recordID); err != nil {
				return ApplyResult{PlanID: plan.ImportPlanID, JobState: store.RecordImportJobStateApplied, RecordIDs: recordIDs, CertificationPending: certificationPending}, err
			}
		}
	}
	return ApplyResult{PlanID: plan.ImportPlanID, JobState: store.RecordImportJobStateApplied, RecordIDs: recordIDs, CertificationPending: certificationPending}, nil
}

func isZeroImportDestination(destination records.SubjectReference) bool {
	return destination.RegistryVersion == 0 &&
		destination.Kind == "" &&
		destination.Role == "" &&
		destination.SourceID == "" &&
		!destination.Primary
}

func classifyImportDestinationError(err error) (error, bool) {
	switch {
	case errors.Is(err, records.ErrInvalidSubjectReference):
		return ErrInvalidImportRequest, true
	case errors.Is(err, store.ErrRecordSubjectNotFound),
		errors.Is(err, recordauth.ErrDenied):
		return ErrExportUnauthorized, true
	case errors.Is(err, store.ErrRecordSubjectUnavailable),
		errors.Is(err, records.ErrSubjectAdapterNotFound),
		errors.Is(err, records.ErrInvalidResolvedSubject):
		return ErrExportUnavailable, true
	default:
		return nil, false
	}
}

func mapImportDestinationError(err error) error {
	if err == nil {
		return nil
	}
	if mapped, ok := classifyImportDestinationError(err); ok {
		return mapped
	}
	return ErrExportUnavailable
}

func (service *Service) authorizeAppliedImport(ctx context.Context, actor recordauth.ActorScope, destination records.SubjectReference, remaps []store.ImportRemap) error {
	if service.importAuthorization == nil {
		return ErrExportUnavailable
	}
	ids := appliedImportRecordIDs(remaps)
	if len(ids) == 0 {
		return ErrImportCASConflict
	}
	current := make([]records.CurrentRecordAuthorization, 0, len(ids))
	for _, recordID := range ids {
		authorization, err := service.importAuthorization.ResolveCurrentRecordAuthorization(ctx, actor.Clone(), recordID)
		if err != nil {
			return mapImportDestinationError(err)
		}
		if authorization.RecordID != recordID || !records.ValidRevisionID(authorization.CurrentRevisionID) ||
			authorization.LockVersion == 0 || authorization.AuthorizationEpoch == 0 ||
			records.ValidateLifecycle(authorization.Lifecycle) != nil || authorization.Evidence.ProjectID != actor.ProjectID {
			return ErrExportUnauthorized
		}
		if err := records.AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, authorization.Evidence); err != nil {
			return mapImportDestinationError(err)
		}
		current = append(current, authorization)
	}
	sort.Slice(current, func(i, j int) bool { return current[i].RecordID < current[j].RecordID })
	if err := service.imports.CheckImportReplayReadFence(ctx, actor, destination, current); err != nil {
		return mapImportDocumentError(err)
	}
	return nil
}

func (service *Service) authorizeImportDestination(
	ctx context.Context,
	actor recordauth.ActorScope,
	destination records.SubjectReference,
) error {
	if service == nil || ctx == nil || len(service.subjects.Kinds()) == 0 {
		return ErrExportUnavailable
	}
	if destination.RegistryVersion != records.SubjectRegistryVersionV1 ||
		!records.ValidSubjectKind(destination.Kind) ||
		destination.Role != records.RelationRoleAffected ||
		!destination.Primary ||
		!records.ValidSubjectSourceID(destination.Kind, destination.SourceID) {
		return ErrInvalidImportRequest
	}
	normalizedActor, err := recordauth.NormalizeActorScope(actor)
	if err != nil {
		return ErrInvalidImportRequest
	}
	resolved, err := service.subjects.Resolve(ctx, normalizedActor, destination)
	if err != nil {
		return mapImportDestinationError(err)
	}
	values, err := records.ImportedRevisionValues(normalizedActor, "", "")
	if err != nil {
		return ErrExportUnavailable
	}
	if err := records.AuthorizeRecordResource(normalizedActor, recordauth.CapabilityImport, records.RecordAuthorizationEvidence{
		ProjectID:  resolved.ProjectID,
		Visibility: values.VisibilityScope,
		Sources:    []recordauth.SourceAuthorization{resolved.CaptureAuthorization},
	}); err != nil {
		return mapImportDestinationError(err)
	}
	return nil
}

func (service *Service) rejectArchiveOrigin(ctx context.Context, digest [32]byte) error {
	if service == nil || service.imports == nil || digest == [32]byte{} {
		return ErrInvalidImportRequest
	}
	if _, tombstoneErr := service.imports.LoadOriginTombstone(ctx, digest); tombstoneErr == nil {
		return ErrOriginTombstoned
	} else if tombstoneErr != nil && !errors.Is(tombstoneErr, store.ErrRecordImportNotFound) {
		return mapImportError(tombstoneErr)
	}
	if _, originErr := service.imports.LoadOrigin(ctx, digest); originErr == nil {
		return ErrImportOriginConflict
	} else if originErr != nil && !errors.Is(originErr, store.ErrRecordImportNotFound) {
		return mapImportError(originErr)
	}
	return nil
}

func (service *Service) cacheImportPlan(job store.RecordImportJob, planned plannedArchive) {
	if service == nil || job.PlanID == "" {
		return
	}
	destination := job.DestinationSubject
	if isZeroImportDestination(destination) {
		destination = planned.destination
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.importPlans[job.PlanID] = cachedImportPlan{
		jobID: job.ImportJobID, lockVersion: job.LockVersion, jobState: job.JobState,
		actorID: job.ActorID, expiresAt: job.ExpiresAt, archiveDigest: job.ArchiveDigest,
		destination: destination,
		documents:   planned.documents, evidence: planned.evidence, attachments: planned.attachments,
		allocationIDs: planned.allocationIDs, ownerTargetIDs: planned.ownerTargetIDs,
		remaps: planned.remaps, quarantine: planned.quarantine, digest: planned.digest,
	}
}

func (service *Service) stageImportArchive(ctx context.Context, job store.RecordImportJob, archive []byte) error {
	if service == nil || service.staging == nil || job.ImportJobID == "" || len(archive) == 0 {
		return ErrExportUnavailable
	}
	archiveDigest := hex.EncodeToString(job.ArchiveDigest[:])
	written, err := service.staging.StageImport(ctx, attachments.WriteRequest{
		ProjectID: string(recordauth.ProjectIDDefault),
		Origin:    attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &archiveDigest},
	}, archive)
	if err != nil {
		return err
	}
	version := written.Version
	digest := sha256.Sum256(archive)
	if version.SHA256 != digest || version.SHA256 != job.ArchiveDigest {
		return ErrInvalidArchive
	}
	_, err = service.imports.PublishImportArtifact(ctx, store.PublishRecordImportArtifactInput{
		ImportJobID:     job.ImportJobID,
		ArtifactRole:    "archive",
		BackendKind:     service.backendKind,
		BlobKey:         version.Key,
		ObjectVersionID: version.VersionID,
		NamespaceID:     written.NamespaceID,
		ObligationID:    written.ObligationID,
		SHA256:          version.SHA256,
		ByteSize:        uint64(version.SizeBytes),
		ExpiresAt:       job.ExpiresAt,
	})
	return mapImportError(err)
}

func (service *Service) ensureImportArchive(
	ctx context.Context,
	job store.RecordImportJob,
	archive []byte,
) error {
	if service == nil || service.imports == nil || job.ImportJobID == "" || len(archive) == 0 {
		return ErrExportUnavailable
	}
	digest := sha256.Sum256(archive)
	if job.ArchiveDigest == [32]byte{} || digest != job.ArchiveDigest {
		return ErrInvalidArchive
	}
	artifact, err := service.imports.LoadImportArtifact(ctx, job.ImportJobID)
	switch {
	case err == nil:
		if artifact.SHA256 != digest || artifact.ByteSize != uint64(len(archive)) {
			return ErrInvalidArchive
		}
		raw, err := service.readStagedImport(ctx, job.ImportJobID, artifact)
		if err != nil {
			return err
		}
		if len(raw) != len(archive) || sha256.Sum256(raw) != digest {
			return ErrInvalidArchive
		}
		return nil
	case errors.Is(err, store.ErrRecordImportNotFound):
		return service.stageImportArchive(ctx, job, archive)
	default:
		return mapImportError(err)
	}
}

func (service *Service) resolveImportPlan(ctx context.Context, planID string) (cachedImportPlan, error) {
	service.mu.Lock()
	cached, ok := service.importPlans[planID]
	service.mu.Unlock()
	if ok {
		return cached, nil
	}
	return service.materializeImportPlan(ctx, planID)
}

func (service *Service) materializeImportPlan(ctx context.Context, planID string) (cachedImportPlan, error) {
	plan, err := service.imports.LoadImportPlan(ctx, planID)
	if err != nil {
		return cachedImportPlan{}, mapImportError(err)
	}
	if isZeroImportDestination(plan.DestinationSubject) {
		return cachedImportPlan{}, ErrImportCASConflict
	}
	job, err := service.imports.LoadImportJob(ctx, plan.ImportJobID)
	if err != nil {
		return cachedImportPlan{}, mapImportError(err)
	}
	if isZeroImportDestination(job.DestinationSubject) || job.DestinationSubject != plan.DestinationSubject {
		return cachedImportPlan{}, ErrImportCASConflict
	}
	if job.JobState == store.RecordImportJobStateApplied {
		return cachedImportPlan{
			jobID: job.ImportJobID, lockVersion: job.LockVersion, jobState: job.JobState,
			actorID: job.ActorID, expiresAt: job.ExpiresAt, archiveDigest: job.ArchiveDigest,
			destination: plan.DestinationSubject, digest: plan.PlanDigest, applied: appliedImportRecordIDs(plan.Remaps),
		}, nil
	}
	artifact, err := service.imports.LoadImportArtifact(ctx, job.ImportJobID)
	if err != nil {
		return cachedImportPlan{}, mapImportError(err)
	}
	raw, err := service.readStagedImport(ctx, job.ImportJobID, artifact)
	if err != nil {
		return cachedImportPlan{}, err
	}
	if sha256.Sum256(raw) != job.ArchiveDigest || job.ArchiveDigest == [32]byte{} {
		return cachedImportPlan{}, ErrInvalidArchive
	}
	planned, err := rebindArchive(raw, plan.Remaps, plan.DestinationSubject)
	if err != nil {
		return cachedImportPlan{}, err
	}
	if planned.digest != plan.PlanDigest {
		return cachedImportPlan{}, ErrImportCASConflict
	}
	cached := cachedImportPlan{
		jobID: job.ImportJobID, lockVersion: job.LockVersion, jobState: job.JobState,
		actorID: job.ActorID, expiresAt: job.ExpiresAt, archiveDigest: job.ArchiveDigest,
		destination: plan.DestinationSubject,
		documents:   planned.documents, evidence: planned.evidence, attachments: planned.attachments,
		allocationIDs: planned.allocationIDs, ownerTargetIDs: planned.ownerTargetIDs,
		remaps: planned.remaps, quarantine: planned.quarantine, digest: planned.digest,
	}
	if job.JobState == store.RecordImportJobStateApplied {
		cached.applied = recordIDsFromRemaps(planned.remaps)
	}
	service.mu.Lock()
	service.importPlans[planID] = cached
	service.mu.Unlock()
	return cached, nil
}

func (service *Service) readStagedImport(
	ctx context.Context,
	jobID string,
	artifact store.RecordImportArtifact,
) ([]byte, error) {
	if service == nil || service.staging == nil {
		return nil, ErrExportUnavailable
	}
	version := attachments.ObjectVersion{
		Key:       artifact.BlobKey,
		VersionID: artifact.ObjectVersionID,
		SHA256:    artifact.SHA256,
		SizeBytes: int64(artifact.ByteSize),
	}
	reader, _, err := service.staging.OpenPublished(ctx, jobID, version, artifact.NamespaceID)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func rebindArchive(raw []byte, remaps []store.ImportRemap, destination records.SubjectReference) (plannedArchive, error) {
	bySource, err := importRemapTable(remaps)
	if err != nil {
		return plannedArchive{}, err
	}
	_, entries, err := ReadArchiveV1(raw)
	if err != nil {
		return plannedArchive{}, err
	}
	allocationIDs := make(map[string]string)
	planned := plannedArchive{destination: destination, allocationIDs: allocationIDs}
	for _, entry := range entries {
		if err := scanImportedMember(entry); err != nil {
			return plannedArchive{}, err
		}
		switch entry.Classification {
		case ArchiveClassMarkdown:
			sourceID, title, body, err := parseImportedMarkdown(entry)
			if err != nil {
				return plannedArchive{}, err
			}
			remap, ok := bySource[importRemapKey("record", sourceID)]
			if !ok {
				return plannedArchive{}, ErrInvalidImportRequest
			}
			allocationIDs[sourceID] = remap.BirthAllocationID
			planned.documents = append(planned.documents, store.ImportDocumentPlan{
				SourceID: sourceID, TargetID: remap.TargetID, Title: title, Body: body,
			})
			planned.remaps = append(planned.remaps, ImportRemap{
				EntityKind: "record", SourceID: sourceID, TargetID: remap.TargetID,
			})
		case ArchiveClassEvidenceJSON, ArchiveClassComparisonJSON:
			if err := planImportedEvidence(&planned, entry, bySource); err != nil {
				return plannedArchive{}, err
			}
		case ArchiveClassAttachment:
			if err := planImportedAttachment(&planned, entry, bySource); err != nil {
				return plannedArchive{}, err
			}
		}
	}
	if len(planned.documents) == 0 {
		return plannedArchive{}, ErrInvalidImportRequest
	}
	if err := planned.bindOwnerTargets(); err != nil {
		return plannedArchive{}, err
	}
	for index := range planned.documents {
		planned.documents[index].Body = rebindImportedMarkdownReferences(planned.documents[index].Body, bySource)
	}
	if !sameImportRemaps(planned.storeRemaps(), remaps) {
		return plannedArchive{}, ErrImportCASConflict
	}
	planned.digest = importPlanDigest(planned)
	return planned, nil
}

// The idempotency identity is stable before a target ID exists. The binding
// also fixes the archive and destination so a changed request cannot reuse it.
func importAllocationIdentity(job store.RecordImportJob, sourceID string) (string, [32]byte) {
	identity := sha256.Sum256(mustJSON(struct {
		Domain   string
		JobID    string
		SourceID string
	}{"houfeng.record-import-allocation-key.v1", job.ImportJobID, sourceID}))
	fingerprint := sha256.Sum256(mustJSON(struct {
		Domain        string
		JobID         string
		ArchiveDigest [32]byte
		SourceID      string
		Destination   records.SubjectReference
	}{"houfeng.record-import-allocation.v1", job.ImportJobID, job.ArchiveDigest, sourceID, job.DestinationSubject}))
	return "import-" + hex.EncodeToString(identity[:]), fingerprint
}

func importAllocationRequest(actor recordauth.ActorScope, job store.RecordImportJob, sourceID string) recordbirth.Request {
	key, fingerprint := importAllocationIdentity(job, sourceID)
	return recordbirth.Request{
		ProjectID: string(actor.ProjectID), ActorID: actor.UserID, ActorScopeDigest: actor.CanonicalHash(),
		Purpose: recordbirth.PurposeImport, IdempotencyKey: key, RequestFingerprint: fingerprint,
	}
}

// importAllocations resolves only existing durable mappings. It never reserves
// new targets while resuming a plan or before an import effect.
func (service *Service) importAllocations(ctx context.Context, actor recordauth.ActorScope, job store.RecordImportJob, remaps []store.ImportRemap) (map[string]recordbirth.Allocation, error) {
	if service.birthAllocations == nil || job.ActorID != actor.UserID || job.ArchiveDigest == ([32]byte{}) {
		return nil, ErrImportCASConflict
	}
	allocations := make(map[string]recordbirth.Allocation)
	targets := make(map[string]bool)
	for _, remap := range remaps {
		if remap.EntityKind != "record" {
			if remap.BirthAllocationID != "" || ((remap.EntityKind == "evidence" || remap.EntityKind == "attachment") != (remap.OwnerTargetRecordID != "")) {
				return nil, ErrImportCASConflict
			}
			continue
		}
		if remap.SourceID == "" || remap.BirthAllocationID == "" || remap.OwnerTargetRecordID != "" || targets[remap.TargetID] {
			return nil, ErrImportCASConflict
		}
		if _, exists := allocations[remap.SourceID]; exists {
			return nil, ErrImportCASConflict
		}
		allocation, err := service.birthAllocations.GetAllocation(ctx, actor, remap.BirthAllocationID)
		if err != nil {
			return nil, err
		}
		if allocation.Validate() != nil || allocation.AllocationID != remap.BirthAllocationID ||
			allocation.RecordID != remap.TargetID || allocation.Request != importAllocationRequest(actor, job, remap.SourceID) {
			return nil, ErrImportCASConflict
		}
		switch allocation.State {
		case recordbirth.StateAllocated, recordbirth.StatePreparing, recordbirth.StatePrepared:
		default:
			return nil, ErrImportCASConflict
		}
		targets[remap.TargetID] = true
		allocations[remap.SourceID] = allocation
	}
	if len(allocations) == 0 {
		return nil, ErrImportCASConflict
	}
	for _, remap := range remaps {
		if remap.OwnerTargetRecordID != "" && !targets[remap.OwnerTargetRecordID] {
			return nil, ErrImportCASConflict
		}
	}
	return allocations, nil
}

// confirmAppliedImport only resumes confirmation of committed births. The
// allocation's original scope digest is immutable; current permissions are
// checked separately and are not substituted into that original request.
func (service *Service) confirmAppliedImport(ctx context.Context, actor recordauth.ActorScope, job store.RecordImportJob, remaps []store.ImportRemap) (bool, error) {
	if job.ActorID != actor.UserID || job.ImportJobID == "" || job.ArchiveDigest == ([32]byte{}) || isZeroImportDestination(job.DestinationSubject) {
		return false, ErrImportCASConflict
	}
	if _, err := importRemapTable(remaps); err != nil {
		return false, ErrImportCASConflict
	}
	confirmationTargets := make([]string, 0, len(remaps))
	targets := make(map[string]bool, len(remaps))
	pending := false
	for _, remap := range remaps {
		if remap.EntityKind != "record" {
			continue
		}
		domain := recordidentity.Classify(remap.TargetID)
		if !recordidentity.IsValid(remap.SourceID) || domain == recordidentity.Invalid || remap.OwnerTargetRecordID != "" || targets[remap.TargetID] {
			return false, ErrImportCASConflict
		}
		targets[remap.TargetID] = true
		if domain == recordidentity.Legacy {
			if remap.BirthAllocationID != "" {
				return false, ErrImportCASConflict
			}
			continue
		}
		if remap.BirthAllocationID == "" {
			return false, ErrImportCASConflict
		}
		if service.birthAllocations == nil {
			if domain == recordidentity.Certifiable {
				pending = true
				continue
			}
			return false, ErrExportUnavailable
		}
		allocation, err := service.birthAllocations.GetAllocation(ctx, actor, remap.BirthAllocationID)
		if err != nil {
			if domain == recordidentity.Certifiable {
				pending = true
				continue
			}
			return false, err
		}
		// These fields derive from the durable job and source remap. The
		// original actor-scope digest comes only from the stored request.
		key, fingerprint := importAllocationIdentity(job, remap.SourceID)
		original := allocation.Request
		if allocation.Validate() != nil || allocation.AllocationID != remap.BirthAllocationID || allocation.RecordID != remap.TargetID ||
			original.ProjectID != string(actor.ProjectID) || original.ActorID != actor.UserID || original.Purpose != recordbirth.PurposeImport ||
			original.IdempotencyKey != key || original.RequestFingerprint != fingerprint ||
			(allocation.State != recordbirth.StateFinished && allocation.State != recordbirth.StateConfirmed) {
			return false, ErrImportCASConflict
		}
		if allocation.Domain == recordidentity.Certifiable && allocation.State == recordbirth.StateFinished {
			confirmationTargets = append(confirmationTargets, allocation.RecordID)
		}
	}
	if len(targets) == 0 {
		return false, ErrImportCASConflict
	}
	for _, recordID := range confirmationTargets {
		if service.birthCoordinator == nil {
			pending = true
			continue
		}
		// Allocation reads and earlier confirmations can wait. Refresh both
		// destination authority and the applied-record read fence before each
		// subsequent confirmation.
		if err := service.authorizeImportDestination(ctx, actor, job.DestinationSubject); err != nil {
			return false, err
		}
		if err := service.authorizeAppliedImport(ctx, actor, job.DestinationSubject, remaps); err != nil {
			return false, err
		}
		if err := service.birthCoordinator.ConfirmCreate(ctx, recordID); err != nil {
			pending = true
		}
	}
	if err := service.authorizeImportDestination(ctx, actor, job.DestinationSubject); err != nil {
		return false, err
	}
	if err := service.authorizeAppliedImport(ctx, actor, job.DestinationSubject, remaps); err != nil {
		return false, err
	}
	return pending, nil
}

func appliedImportRecordIDs(remaps []store.ImportRemap) []string {
	records := make([]store.ImportRemap, 0, len(remaps))
	for _, remap := range remaps {
		if remap.EntityKind == "record" && remap.TargetID != "" {
			records = append(records, remap)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SourceID < records[j].SourceID })
	ids := make([]string, 0, len(records))
	for _, remap := range records {
		ids = append(ids, remap.TargetID)
	}
	return ids
}

func recordIDsFromRemaps(remaps []ImportRemap) []string {
	ids := make([]string, 0, len(remaps))
	for _, remap := range remaps {
		if remap.EntityKind == "record" && remap.TargetID != "" {
			ids = append(ids, remap.TargetID)
		}
	}
	return ids
}

type plannedArchive struct {
	destination    records.SubjectReference
	documents      []store.ImportDocumentPlan
	evidence       []importedEvidencePlan
	attachments    []importedAttachmentPlan
	remaps         []ImportRemap
	allocationIDs  map[string]string
	ownerTargetIDs map[string]string
	quarantine     []QuarantinedEvidence
	digest         [32]byte
}

func (planned plannedArchive) storeRemaps() []store.ImportRemap {
	out := make([]store.ImportRemap, 0, len(planned.remaps))
	for _, remap := range planned.remaps {
		stored := store.ImportRemap{EntityKind: remap.EntityKind, SourceID: remap.SourceID, TargetID: remap.TargetID}
		if remap.EntityKind == "record" {
			stored.BirthAllocationID = planned.allocationIDs[remap.SourceID]
		}
		stored.OwnerTargetRecordID = planned.ownerTargetIDs[importRemapKey(remap.EntityKind, remap.SourceID)]
		out = append(out, stored)
	}
	return out
}

// bindOwnerTargets derives each content owner's target from the archive's
// source record and this plan's record remaps. Archive IDs remain provenance;
// only the allocated local target is persisted as the owner.
func (planned *plannedArchive) bindOwnerTargets() error {
	recordsBySource := make(map[string]string, len(planned.documents))
	for _, document := range planned.documents {
		if document.SourceID == "" || document.TargetID == "" || recordsBySource[document.SourceID] != "" {
			return ErrInvalidImportRequest
		}
		recordsBySource[document.SourceID] = document.TargetID
	}
	planned.ownerTargetIDs = make(map[string]string, len(planned.evidence)+len(planned.attachments))
	bind := func(kind, sourceID, ownerSource string, ownerOmitted bool) error {
		owner := recordsBySource[ownerSource]
		if ownerSource == "" && ownerOmitted && len(planned.documents) == 1 {
			owner = planned.documents[0].TargetID
		}
		key := importRemapKey(kind, sourceID)
		if sourceID == "" || owner == "" || planned.ownerTargetIDs[key] != "" {
			return ErrInvalidImportRequest
		}
		planned.ownerTargetIDs[key] = owner
		return nil
	}
	for _, item := range planned.evidence {
		if err := bind("evidence", item.SourceID, item.RecordSourceID, item.ownerOmitted); err != nil {
			return err
		}
	}
	for _, item := range planned.attachments {
		if err := bind("attachment", item.SourceID, item.RecordSourceID, false); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) planArchive(ctx context.Context, raw []byte, actor recordauth.ActorScope, job store.RecordImportJob) (plannedArchive, error) {
	_, entries, err := ReadArchiveV1(raw)
	if err != nil {
		return plannedArchive{}, err
	}
	if service.birthCoordinator == nil || service.birthAllocations == nil {
		return plannedArchive{}, ErrExportUnavailable
	}
	planned := plannedArchive{destination: job.DestinationSubject, allocationIDs: make(map[string]string)}
	for _, entry := range entries {
		if err := scanImportedMember(entry); err != nil {
			return plannedArchive{}, err
		}
		switch entry.Classification {
		case ArchiveClassMarkdown:
			sourceID, title, body, err := parseImportedMarkdown(entry)
			if err != nil {
				return plannedArchive{}, err
			}
			if _, exists := planned.allocationIDs[sourceID]; exists {
				return plannedArchive{}, ErrInvalidArchive
			}
			allocationRequest := importAllocationRequest(actor, job, sourceID)
			allocation, err := service.birthCoordinator.Reserve(ctx, allocationRequest)
			if err != nil {
				return plannedArchive{}, err
			}
			if allocation.Validate() != nil || allocation.Request != allocationRequest || allocation.State != recordbirth.StateAllocated {
				return plannedArchive{}, ErrImportCASConflict
			}
			planned.allocationIDs[sourceID] = allocation.AllocationID
			targetID := allocation.RecordID
			planned.documents = append(planned.documents, store.ImportDocumentPlan{
				SourceID: sourceID, TargetID: targetID, Title: title, Body: body,
			})
			planned.remaps = append(planned.remaps, ImportRemap{EntityKind: "record", SourceID: sourceID, TargetID: targetID})
		case ArchiveClassEvidenceJSON, ArchiveClassComparisonJSON:
			if err := planImportedEvidence(&planned, entry, nil); err != nil {
				return plannedArchive{}, err
			}
		case ArchiveClassAttachment:
			if err := planImportedAttachment(&planned, entry, nil); err != nil {
				return plannedArchive{}, err
			}
		}
	}
	if len(planned.documents) == 0 {
		return plannedArchive{}, ErrInvalidImportRequest
	}
	if err := planned.bindOwnerTargets(); err != nil {
		return plannedArchive{}, err
	}
	bySource, err := importRemapTable(planned.storeRemaps())
	if err != nil {
		return plannedArchive{}, err
	}
	for index := range planned.documents {
		planned.documents[index].Body = rebindImportedMarkdownReferences(planned.documents[index].Body, bySource)
	}
	planned.digest = importPlanDigest(planned)
	return planned, nil
}

func (service *Service) planView(job store.RecordImportJob, plan store.RecordImportPlan, quarantine []QuarantinedEvidence) ImportPlanView {
	remaps := make([]ImportRemap, 0, len(plan.Remaps))
	for _, remap := range plan.Remaps {
		remaps = append(remaps, ImportRemap{EntityKind: remap.EntityKind, SourceID: remap.SourceID, TargetID: remap.TargetID})
	}
	// A nil quarantine is an empty result. The response uses a non-nil slice so
	// JSON encodes an array. The planner slice stays as planned; the digest
	// distinguishes a nil slice from an empty one.
	quarantined := make([]QuarantinedEvidence, 0, len(quarantine))
	quarantined = append(quarantined, quarantine...)
	destination := plan.DestinationSubject
	if isZeroImportDestination(destination) {
		destination = job.DestinationSubject
	}
	return ImportPlanView{
		PlanID:      plan.ImportPlanID,
		JobState:    job.JobState,
		LockVersion: job.LockVersion,
		Remaps:      remaps,
		Quarantine:  quarantined,
		ObjectCount: int(plan.ObjectCount),
		ExpiresAt:   plan.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		DestinationSubject: ImportDestinationSubjectView{
			SubjectKind: destination.Kind,
			SubjectID:   destination.SourceID,
		},
	}
}

func parseImportedMarkdown(entry ArchiveEntry) (sourceID, title, body string, err error) {
	parts := strings.Split(entry.Path, "/")
	if len(parts) < 3 || parts[0] != "records" || !recordidentity.IsValid(parts[1]) {
		return "", "", "", ErrUntrustedImportContent
	}
	body = stripExportMarkdownChrome(string(entry.Payload))
	title = "imported record"
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "# ") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
			break
		}
	}
	return parts[1], title, body, nil
}

func stripExportMarkdownChrome(body string) string {
	const authorizedHeading = "\n## 已授权材料\n"
	index := strings.Index(body, authorizedHeading)
	if index < 0 {
		return body
	}
	before := body[:index]
	after := body[index+len(authorizedHeading):]
	if nextHeading := strings.Index(after, "\n## "); nextHeading >= 0 {
		return before + after[nextHeading:]
	}
	return strings.TrimRight(before, "\n") + "\n"
}

func planImportedEvidence(planned *plannedArchive, entry ArchiveEntry, remaps map[string]store.ImportRemap) error {
	schema, sourceID, err := importedEvidenceIdentity(entry)
	if err != nil {
		return err
	}
	if entry.Classification == ArchiveClassComparisonJSON {
		schema = evidence.ComparisonResultV1Key().String()
	}
	if !knownLocalImportSchema(schema) {
		return ErrImportSchemaBlocked
	}
	if entry.Classification == ArchiveClassEvidenceJSON {
		if _, ok := decodeOfficialEvidenceRestoreMember(entry.Payload); !ok {
			return ErrUntrustedImportContent
		}
	}
	targetID := sourceID
	if remaps != nil {
		remap, ok := remaps[importRemapKey("evidence", sourceID)]
		if !ok {
			return ErrInvalidImportRequest
		}
		targetID = remap.TargetID
	} else {
		allocated, err := ids.New("evs")
		if err != nil {
			return ErrInvalidImportRequest
		}
		targetID = allocated
	}
	recordSource, ownerOmitted, err := evidenceArchiveOwner(entry.Path)
	if err != nil {
		return err
	}
	planned.evidence = append(planned.evidence, importedEvidencePlan{
		SourceID: sourceID, TargetID: targetID, RecordSourceID: recordSource,
		Schema: schema, Payload: append([]byte(nil), entry.Payload...), ownerOmitted: ownerOmitted,
	})
	planned.remaps = append(planned.remaps, ImportRemap{EntityKind: "evidence", SourceID: sourceID, TargetID: targetID})
	return nil
}

func importedEvidenceIdentity(entry ArchiveEntry) (schema, sourceID string, err error) {
	var payload map[string]any
	if json.Unmarshal(entry.Payload, &payload) != nil {
		return "", "", ErrUntrustedImportContent
	}
	schema = officialImportSchema(payload)
	sourceID = evidenceSourceFromPath(entry.Path)
	if sourceID == "" {
		if id, ok := payload["snapshot_id"].(string); ok {
			sourceID = id
		}
	}
	if sourceID == "" {
		sum := sha256.Sum256(entry.Payload)
		sourceID = "evs_" + hex.EncodeToString(sum[:8])
	}
	return schema, sourceID, nil
}

func officialImportSchema(payload map[string]any) string {
	if schema, _ := payload["schema"].(string); schema != "" {
		return schema
	}
	kind, _ := payload["kind"].(string)
	if kind == "" {
		if version, _ := payload["version"].(string); version != "" {
			return version
		}
		return ""
	}
	switch version := payload["schema_version"].(type) {
	case float64:
		if version > 0 && version == float64(int64(version)) {
			return kind + "/v" + strconv.FormatInt(int64(version), 10)
		}
	case json.Number:
		if parsed, err := version.Int64(); err == nil && parsed > 0 {
			return kind + "/v" + strconv.FormatInt(parsed, 10)
		}
	}
	if version, _ := payload["version"].(string); version != "" {
		if strings.HasPrefix(version, "v") {
			return kind + "/" + version
		}
		return version
	}
	return kind
}

func evidenceSourceFromPath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[0] != "records" {
		return ""
	}
	name := parts[len(parts)-1]
	name = strings.TrimSuffix(name, ".json")
	if strings.HasPrefix(name, "evs_") {
		return name
	}
	if strings.HasPrefix(name, "comparison.result") {
		return ""
	}
	return ""
}

func recordSourceFromArchivePath(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 2 && parts[0] == "records" && recordidentity.IsValid(parts[1]) {
		return parts[1]
	}
	return ""
}

// V1 permits an ownerless evidence member. A records route, or any path
// containing a record identity, is instead an explicit owner and cannot use
// the single-document fallback when it is malformed or mismatched.
func evidenceArchiveOwner(path string) (string, bool, error) {
	parts := strings.Split(path, "/")
	if parts[0] == "records" {
		if len(parts) < 3 || !recordidentity.IsValid(parts[1]) {
			return "", false, ErrInvalidImportRequest
		}
		return parts[1], false, nil
	}
	for _, part := range parts {
		if strings.HasPrefix(part, "rec_") || strings.HasPrefix(part, "rec2_") {
			return "", false, ErrInvalidImportRequest
		}
	}
	return "", true, nil
}

func knownLocalImportSchema(schema string) bool {
	if schema == "" {
		return false
	}
	if schema == evidence.ComparisonResultV1Key().String() ||
		schema == "comparison_result/v1" || schema == "comparison.result/v1" {
		return true
	}
	key, err := evidence.ParseKindKey(schema)
	if err != nil {
		return false
	}
	for _, known := range evidence.KnownKindKeys() {
		if key == known {
			return true
		}
	}
	return false
}

func (service *Service) applyImportedEvidence(ctx context.Context, actor recordauth.ActorScope, cached cachedImportPlan) error {
	if len(cached.evidence) == 0 {
		return nil
	}
	if service.evidenceImports == nil {
		return ErrImportSchemaBlocked
	}
	for _, item := range cached.evidence {
		targetRecord := cached.ownerTargetIDs[importRemapKey("evidence", item.SourceID)]
		if targetRecord == "" {
			return ErrInvalidImportRequest
		}
		if err := service.evidenceImports.ImportExportedEvidence(ctx, ImportedEvidenceRequest{
			Actor: actor, TargetRecordID: targetRecord, TargetSnapshotID: item.TargetID,
			Schema: item.Schema, Payload: item.Payload,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) prepareImportedEvidence(cached cachedImportPlan, planID string) (map[string]evidence.RevisionPreparation, error) {
	if service == nil || cached.jobID == "" || planID == "" {
		return nil, ErrInvalidImportRequest
	}
	importedByRecord := map[string][]evidence.PreparedImportedSnapshot{}
	seen := map[string]struct{}{}
	for _, item := range cached.evidence {
		snapshot, isWrapper, err := restoreOfficialEvidenceSnapshot(service.kinds, item.Payload)
		if err != nil {
			return nil, err
		}
		if !isWrapper {
			continue
		}
		targetRecord := cached.ownerTargetIDs[importRemapKey("evidence", item.SourceID)]
		if targetRecord == "" || item.TargetID == "" {
			return nil, ErrInvalidImportRequest
		}
		identity := targetRecord + "\x00" + item.TargetID
		if _, exists := seen[identity]; exists {
			return nil, ErrInvalidImportRequest
		}
		seen[identity] = struct{}{}
		prepared, err := evidence.NewPreparedImportedSnapshot(targetRecord, item.TargetID, snapshot, evidence.ImportedSnapshotRoute{
			ImportJobID: cached.jobID, ImportPlanID: planID,
		})
		if err != nil {
			return nil, ErrUntrustedImportContent
		}
		importedByRecord[targetRecord] = append(importedByRecord[targetRecord], prepared)
	}
	preparations := make(map[string]evidence.RevisionPreparation, len(importedByRecord))
	for recordID, items := range importedByRecord {
		ordered := make([]string, 0, len(items))
		for _, item := range items {
			ordered = append(ordered, item.SnapshotID())
		}
		prepared, err := evidence.NewRevisionPreparation(recordID, evidence.RevisionPreparationValues{
			Imported:           items,
			OrderedSnapshotIDs: ordered,
		})
		if err != nil {
			return nil, ErrUntrustedImportContent
		}
		preparations[recordID] = prepared
	}
	return preparations, nil
}

type importPlanDigestDestination struct {
	RegistryVersion uint64               `json:"registry_version"`
	Kind            records.SubjectKind  `json:"kind"`
	Role            records.RelationRole `json:"role"`
	SourceID        string               `json:"source_id"`
	Primary         bool                 `json:"primary"`
}

type importPlanDigestPayloadV2 struct {
	Domain             string                      `json:"domain"`
	DestinationSubject importPlanDigestDestination `json:"destination_subject"`
	Documents          []store.ImportDocumentPlan  `json:"documents"`
	Evidence           []importedEvidencePlan      `json:"evidence"`
	Attachments        []importedAttachmentPlan    `json:"attachments"`
	Remaps             []ImportRemap               `json:"remaps"`
	Quarantine         []QuarantinedEvidence       `json:"quarantine"`
}

func importPlanDigest(planned plannedArchive) [32]byte {
	return sha256.Sum256(mustJSON(importPlanDigestPayloadV2{
		Domain: "houfeng.record-import-plan.v2",
		DestinationSubject: importPlanDigestDestination{
			RegistryVersion: planned.destination.RegistryVersion,
			Kind:            planned.destination.Kind,
			Role:            planned.destination.Role,
			SourceID:        planned.destination.SourceID,
			Primary:         planned.destination.Primary,
		},
		Documents:   planned.documents,
		Evidence:    planned.evidence,
		Attachments: planned.attachments,
		Remaps:      planned.remaps,
		Quarantine:  planned.quarantine,
	}))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstLocalRecord(recordIDs []string, documents []store.ImportDocumentPlan) string {
	if len(recordIDs) > 0 {
		return recordIDs[0]
	}
	if len(documents) == 0 {
		return ""
	}
	return documents[0].TargetID
}

func mapImportDocumentError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, records.ErrUntrustedImportIdentity),
		errors.Is(err, records.ErrInvalidApplicationRequest):
		return ErrInvalidImportRequest
	case errors.Is(err, store.ErrRecordOriginConflict):
		return ErrImportOriginConflict
	case errors.Is(err, store.ErrRecordImportCASConflict):
		return ErrImportCASConflict
	case errors.Is(err, store.ErrRecordPortabilityUnavailable), errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable):
		return ErrExportUnavailable
	default:
		if mapped, ok := classifyImportDestinationError(err); ok {
			return mapped
		}
		return err
	}
}

func mapImportError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrRecordImportCASConflict):
		return ErrImportCASConflict
	case errors.Is(err, ErrOriginTombstoned):
		return ErrOriginTombstoned
	case errors.Is(err, store.ErrRecordOriginConflict):
		return ErrImportOriginConflict
	case errors.Is(err, store.ErrRecordImportNotFound):
		return ErrInvalidImportRequest
	case errors.Is(err, store.ErrRecordPortabilityUnavailable),
		errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable):
		return ErrExportUnavailable
	default:
		return err
	}
}
