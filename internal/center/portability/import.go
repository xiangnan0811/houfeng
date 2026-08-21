package portability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

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
	PublishImportArtifact(context.Context, store.PublishRecordImportArtifactInput) (store.RecordImportArtifact, error)
	LoadImportArtifact(context.Context, string) (store.RecordImportArtifact, error)
	AdvanceImportJob(context.Context, store.AdvanceRecordImportJobInput) error
	LoadOriginTombstone(context.Context, [32]byte) (store.RecordOriginTombstone, error)
	LoadOrigin(context.Context, [32]byte) (store.RecordOrigin, error)
	InsertOrigin(context.Context, store.InsertRecordOriginInput) (store.RecordOrigin, error)
}

type DryRunRequest struct {
	Actor          recordauth.ActorScope
	IdempotencyKey string
	Archive        []byte
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

type ImportPlanView struct {
	PlanID      string                `json:"plan_id"`
	JobState    string                `json:"job_state"`
	LockVersion uint64                `json:"lock_version"`
	Remaps      []ImportRemap         `json:"remaps"`
	Quarantine  []QuarantinedEvidence `json:"quarantine"`
	ObjectCount int                   `json:"object_count"`
	ExpiresAt   string                `json:"expires_at"`
}

type ApplyRequest struct {
	Actor       recordauth.ActorScope
	PlanID      string
	LockVersion uint64
}

type ApplyResult struct {
	PlanID    string   `json:"plan_id"`
	JobState  string   `json:"job_state"`
	RecordIDs []string `json:"record_ids"`
}

func (service *Service) DryRun(ctx context.Context, request DryRunRequest) (ImportPlanView, error) {
	if err := service.requireEnabled(); err != nil {
		return ImportPlanView{}, err
	}
	if service.imports == nil || ctx == nil || request.IdempotencyKey == "" || len(request.Archive) == 0 {
		return ImportPlanView{}, ErrInvalidImportRequest
	}
	if _, err := recordauth.NormalizeActorScope(request.Actor); err != nil {
		return ImportPlanView{}, ErrInvalidImportRequest
	}
	planned, err := service.planArchive(request.Archive)
	if err != nil {
		return ImportPlanView{}, err
	}
	digest := sha256.Sum256(request.Archive)
	if err := service.rejectArchiveOrigin(ctx, digest); err != nil {
		return ImportPlanView{}, err
	}
	expiresAt := service.now().Add(service.previewTTL)
	job, err := service.imports.ClaimImportJob(ctx, store.ClaimRecordImportJobInput{
		ActorID:        request.Actor.UserID,
		IdempotencyKey: request.IdempotencyKey,
		ArchiveDigest:  digest,
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return ImportPlanView{}, mapImportError(err)
	}
	if job.PlanID != "" {
		existing, err := service.imports.LoadImportPlan(ctx, job.PlanID)
		if err != nil {
			return ImportPlanView{}, mapImportError(err)
		}
		rebound, err := rebindArchive(request.Archive, existing.Remaps)
		if err != nil {
			return ImportPlanView{}, err
		}
		if rebound.digest != existing.PlanDigest {
			return ImportPlanView{}, ErrInvalidArchive
		}
		if err := service.stageImportArchive(ctx, job, request.Archive); err != nil {
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
		return service.planView(job, existing, rebound.quarantine), nil
	}
	plan, err := service.imports.SaveImportPlan(ctx, store.SaveRecordImportPlanInput{
		ImportJobID: job.ImportJobID,
		PlanDigest:  planned.digest,
		ObjectCount: uint64(len(planned.documents) + len(planned.evidence) + len(planned.attachments) + len(planned.quarantine)),
		RemapCount:  uint64(len(planned.remaps)),
		Remaps:      planned.storeRemaps(),
		Documents:   planned.documents,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		return ImportPlanView{}, mapImportError(err)
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
	plan.LockVersion = job.LockVersion
	plan.JobState = job.JobState
	return service.planView(job, plan, planned.quarantine), nil
}

func (service *Service) Apply(ctx context.Context, request ApplyRequest) (ApplyResult, error) {
	if err := service.requireEnabled(); err != nil {
		return ApplyResult{}, err
	}
	if service.imports == nil || service.importer == nil || ctx == nil || request.PlanID == "" {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	cached, err := service.resolveImportPlan(ctx, request.PlanID)
	if err != nil {
		return ApplyResult{}, err
	}
	if cached.actorID == "" || cached.actorID != actor.UserID {
		return ApplyResult{}, ErrExportUnauthorized
	}
	if cached.jobState == store.RecordImportJobStateApplied {
		return ApplyResult{PlanID: request.PlanID, JobState: cached.jobState, RecordIDs: cached.applied}, nil
	}
	if !cached.expiresAt.IsZero() && !service.now().Before(cached.expiresAt) {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	if cached.jobState != store.RecordImportJobStatePlanned {
		return ApplyResult{}, ErrInvalidImportRequest
	}
	if request.LockVersion != cached.lockVersion {
		return ApplyResult{}, ErrImportCASConflict
	}
	plan := store.RecordImportPlan{
		ImportPlanID: request.PlanID, ImportJobID: cached.jobID,
		PlanDigest: cached.digest, Documents: cached.documents, LockVersion: cached.lockVersion,
	}
	originDigest := cached.archiveDigest
	if originDigest == [32]byte{} {
		originDigest = cached.digest
	}
	if err := service.rejectArchiveOrigin(ctx, originDigest); err != nil {
		return ApplyResult{}, err
	}
	if err := service.applyImportedEvidence(ctx, actor, cached); err != nil {
		return ApplyResult{}, err
	}
	preparations, err := service.prepareImportedEvidence(cached)
	if err != nil {
		return ApplyResult{}, err
	}
	importedAttachments, attachmentIDs, err := service.restoreImportedAttachments(ctx, actor, cached)
	if err != nil {
		return ApplyResult{}, err
	}
	importRequests := make([]records.ImportDocumentRequest, 0, len(plan.Documents))
	for _, document := range plan.Documents {
		importRequests = append(importRequests, records.ImportDocumentRequest{
			Actor:               actor,
			RecordID:            document.TargetID,
			Title:               document.Title,
			BodyMarkdown:        document.Body,
			IdempotencyKey:      "import-" + plan.ImportPlanID + "-" + document.TargetID,
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
		ActorID:        actor.UserID,
	})
	if err != nil {
		return ApplyResult{}, mapImportDocumentError(err)
	}
	recordIDs := make([]string, 0, len(written))
	for _, document := range written {
		recordIDs = append(recordIDs, document.RecordID)
	}
	service.mu.Lock()
	cached.jobState = store.RecordImportJobStateApplied
	cached.applied = recordIDs
	service.importPlans[request.PlanID] = cached
	service.mu.Unlock()
	if service.rebuilder != nil {
		for _, recordID := range recordIDs {
			if err := service.rebuilder.RebuildImportedRecord(ctx, recordID); err != nil {
				return ApplyResult{PlanID: plan.ImportPlanID, JobState: store.RecordImportJobStateApplied, RecordIDs: recordIDs}, err
			}
		}
	}
	return ApplyResult{PlanID: plan.ImportPlanID, JobState: store.RecordImportJobStateApplied, RecordIDs: recordIDs}, nil
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
	service.mu.Lock()
	defer service.mu.Unlock()
	service.importPlans[job.PlanID] = cachedImportPlan{
		jobID: job.ImportJobID, lockVersion: job.LockVersion, jobState: job.JobState,
		actorID: job.ActorID, expiresAt: job.ExpiresAt, archiveDigest: job.ArchiveDigest,
		documents: planned.documents, evidence: planned.evidence, attachments: planned.attachments,
		remaps: planned.remaps, quarantine: planned.quarantine, digest: planned.digest,
	}
}

func (service *Service) stageImportArchive(ctx context.Context, job store.RecordImportJob, archive []byte) error {
	if service == nil || service.staging == nil || job.ImportJobID == "" || len(archive) == 0 {
		return ErrExportUnavailable
	}
	version, err := service.staging.StageImport(ctx, job.ImportJobID, archive)
	if err != nil {
		return err
	}
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
		SHA256:          version.SHA256,
		ByteSize:        uint64(version.SizeBytes),
		ExpiresAt:       job.ExpiresAt,
	})
	return mapImportError(err)
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
	job, err := service.imports.LoadImportJob(ctx, plan.ImportJobID)
	if err != nil {
		return cachedImportPlan{}, mapImportError(err)
	}
	artifact, err := service.imports.LoadImportArtifact(ctx, job.ImportJobID)
	if err != nil {
		return cachedImportPlan{}, mapImportError(err)
	}
	raw, err := service.readStagedImport(ctx, job.ImportJobID, artifact)
	if err != nil {
		return cachedImportPlan{}, err
	}
	if sha256.Sum256(raw) != job.ArchiveDigest {
		return cachedImportPlan{}, ErrInvalidArchive
	}
	planned, err := rebindArchive(raw, plan.Remaps)
	if err != nil {
		return cachedImportPlan{}, err
	}
	if planned.digest != plan.PlanDigest {
		return cachedImportPlan{}, ErrInvalidArchive
	}
	cached := cachedImportPlan{
		jobID: job.ImportJobID, lockVersion: job.LockVersion, jobState: job.JobState,
		actorID: job.ActorID, expiresAt: job.ExpiresAt, archiveDigest: job.ArchiveDigest,
		documents: planned.documents, evidence: planned.evidence, attachments: planned.attachments,
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
	reader, _, err := service.staging.OpenPublished(ctx, jobID, version)
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

func rebindArchive(raw []byte, remaps []store.ImportRemap) (plannedArchive, error) {
	bySource := make(map[string]store.ImportRemap, len(remaps))
	for _, remap := range remaps {
		if remap.SourceID == "" || remap.TargetID == "" {
			return plannedArchive{}, ErrInvalidImportRequest
		}
		bySource[remap.SourceID] = remap
	}
	_, entries, err := ReadArchiveV1(raw)
	if err != nil {
		return plannedArchive{}, err
	}
	planned := plannedArchive{}
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
			remap, ok := bySource[sourceID]
			if !ok || remap.EntityKind != "record" {
				return plannedArchive{}, ErrInvalidImportRequest
			}
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
	planned.digest = importPlanDigest(planned)
	return planned, nil
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
	documents   []store.ImportDocumentPlan
	evidence    []importedEvidencePlan
	attachments []importedAttachmentPlan
	remaps      []ImportRemap
	quarantine  []QuarantinedEvidence
	digest      [32]byte
}

func (planned plannedArchive) storeRemaps() []store.ImportRemap {
	out := make([]store.ImportRemap, 0, len(planned.remaps))
	for _, remap := range planned.remaps {
		out = append(out, store.ImportRemap{EntityKind: remap.EntityKind, SourceID: remap.SourceID, TargetID: remap.TargetID})
	}
	return out
}

func (service *Service) planArchive(raw []byte) (plannedArchive, error) {
	_, entries, err := ReadArchiveV1(raw)
	if err != nil {
		return plannedArchive{}, err
	}
	planned := plannedArchive{}
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
			targetID, err := ids.New("rec")
			if err != nil {
				return plannedArchive{}, ErrInvalidImportRequest
			}
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
	planned.digest = importPlanDigest(planned)
	return planned, nil
}

func (service *Service) planView(job store.RecordImportJob, plan store.RecordImportPlan, quarantine []QuarantinedEvidence) ImportPlanView {
	remaps := make([]ImportRemap, 0, len(plan.Remaps))
	for _, remap := range plan.Remaps {
		remaps = append(remaps, ImportRemap{EntityKind: remap.EntityKind, SourceID: remap.SourceID, TargetID: remap.TargetID})
	}
	return ImportPlanView{
		PlanID:      plan.ImportPlanID,
		JobState:    job.JobState,
		LockVersion: job.LockVersion,
		Remaps:      remaps,
		Quarantine:  quarantine,
		ObjectCount: int(plan.ObjectCount),
		ExpiresAt:   plan.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

func parseImportedMarkdown(entry ArchiveEntry) (sourceID, title, body string, err error) {
	parts := strings.Split(entry.Path, "/")
	if len(parts) < 3 || parts[0] != "records" || !strings.HasPrefix(parts[1], "rec_") {
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
	for _, heading := range []string{"\n## 已授权材料\n", "\n## 不可用材料\n"} {
		if index := strings.Index(body, heading); index >= 0 {
			body = strings.TrimRight(body[:index], "\n") + "\n"
		}
	}
	return body
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
	targetID := sourceID
	if remaps != nil {
		remap, ok := remaps[sourceID]
		if !ok || remap.EntityKind != "evidence" {
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
	recordSource := recordSourceFromArchivePath(entry.Path)
	planned.evidence = append(planned.evidence, importedEvidencePlan{
		SourceID: sourceID, TargetID: targetID, RecordSourceID: recordSource,
		Schema: schema, Payload: append([]byte(nil), entry.Payload...),
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
	if len(parts) >= 2 && strings.HasPrefix(parts[1], "rec_") {
		return parts[1]
	}
	return ""
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
	recordBySource := map[string]string{}
	for _, document := range cached.documents {
		recordBySource[document.SourceID] = document.TargetID
	}
	for _, item := range cached.evidence {
		targetRecord := recordBySource[item.RecordSourceID]
		if targetRecord == "" && len(cached.documents) == 1 {
			targetRecord = cached.documents[0].TargetID
		}
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

func (service *Service) prepareImportedEvidence(cached cachedImportPlan) (map[string]evidence.RevisionPreparation, error) {
	if service == nil {
		return nil, ErrInvalidImportRequest
	}
	recordBySource := map[string]string{}
	for _, document := range cached.documents {
		recordBySource[document.SourceID] = document.TargetID
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
		targetRecord := recordBySource[item.RecordSourceID]
		if targetRecord == "" && len(cached.documents) == 1 {
			targetRecord = cached.documents[0].TargetID
		}
		if targetRecord == "" || item.TargetID == "" {
			return nil, ErrInvalidImportRequest
		}
		identity := targetRecord + "\x00" + item.TargetID
		if _, exists := seen[identity]; exists {
			return nil, ErrInvalidImportRequest
		}
		seen[identity] = struct{}{}
		prepared, err := evidence.NewPreparedImportedSnapshot(targetRecord, item.TargetID, snapshot)
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

func importPlanDigest(planned plannedArchive) [32]byte {
	return sha256.Sum256(mustJSON(map[string]any{
		"documents":   planned.documents,
		"evidence":    planned.evidence,
		"attachments": planned.attachments,
		"remaps":      planned.remaps,
		"quarantine":  planned.quarantine,
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
	default:
		return err
	}
}

func mapImportError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrRecordImportCASConflict):
		return ErrImportCASConflict
	case errors.Is(err, store.ErrRecordOriginTombstoned):
		return ErrOriginTombstoned
	case errors.Is(err, store.ErrRecordOriginConflict):
		return ErrImportOriginConflict
	case errors.Is(err, store.ErrRecordImportNotFound):
		return ErrInvalidImportRequest
	default:
		return err
	}
}
