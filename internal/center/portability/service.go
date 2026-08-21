package portability

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordmarkdown"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

type DocumentSource interface {
	ExportDocument(context.Context, records.ExportDocumentRequest) (records.ExportDocument, error)
}

type EvidenceExporter interface {
	Export(context.Context, evidence.ExportRequest) (evidence.ExportMaterial, error)
}

type SnapshotSource interface {
	LoadAuthorizedEvidenceSnapshot(context.Context, evidence.ActorScope, string) (evidence.AuthorizedSnapshot, error)
}

type ComparisonExporter interface {
	Export(evidence.CanonicalSnapshot, evidence.ExportMode) evidence.ExportMaterial
	Summarize(evidence.CanonicalSnapshot) evidence.Summary
}

type KindSource interface {
	LookupKey(evidence.KindKey) (evidence.Kind, error)
}

type JobRepository interface {
	ClaimExportJob(context.Context, store.ClaimRecordExportJobInput) (store.RecordExportJob, error)
	AdvanceExportJob(context.Context, store.AdvanceRecordExportJobInput) error
	LoadExportJob(context.Context, string) (store.RecordExportJob, error)
	PublishExportArtifact(context.Context, store.PublishRecordExportArtifactInput) (store.RecordExportArtifact, error)
	LoadExportArtifact(context.Context, string) (store.RecordExportArtifact, error)
	RevokeExport(context.Context, string, uint64) error
}

type Options struct {
	Enabled         bool
	BackendKind     string
	Documents       DocumentSource
	Jobs            JobRepository
	Evidence        EvidenceExporter
	Snapshots       SnapshotSource
	Comparison      ComparisonExporter
	Activity        activity.ActivityExportReader
	PDF             DocumentPDFRenderer
	Imports         ImportRepository
	Importer        DocumentImporter
	EvidenceImports EvidenceImporter
	Kinds           KindSource
	Attachments     AttachmentSource
	AttachmentBlobs attachments.BlobStore
	Rebuilder       ImportProjectionRebuilder
	Staging         *LeasedBlobStore
	Now             func() time.Time
	PreviewTTL      time.Duration
}

type Service struct {
	enabled         bool
	backendKind     string
	documents       DocumentSource
	jobs            JobRepository
	evidence        EvidenceExporter
	snapshots       SnapshotSource
	comparison      ComparisonExporter
	activity        activity.ActivityExportReader
	pdf             DocumentPDFRenderer
	imports         ImportRepository
	importer        DocumentImporter
	evidenceImports EvidenceImporter
	kinds           KindSource
	attachments     AttachmentSource
	attachmentBlobs attachments.BlobStore
	rebuilder       ImportProjectionRebuilder
	staging         *LeasedBlobStore
	now             func() time.Time
	previewTTL      time.Duration
	mu              sync.Mutex
	previews        map[string]PreviewRequest
	confirmTokens   map[string]string
	importPlans     map[string]cachedImportPlan
}

type cachedImportPlan struct {
	jobID         string
	lockVersion   uint64
	jobState      string
	actorID       string
	expiresAt     time.Time
	archiveDigest [32]byte
	documents     []store.ImportDocumentPlan
	evidence      []importedEvidencePlan
	attachments   []importedAttachmentPlan
	remaps        []ImportRemap
	quarantine    []QuarantinedEvidence
	digest        [32]byte
	applied       []string
}

type importedEvidencePlan struct {
	SourceID       string
	TargetID       string
	RecordSourceID string
	Schema         string
	Payload        []byte
}

func NewService(options Options) (*Service, error) {
	if options.BackendKind != "local" && options.BackendKind != "s3" {
		return nil, ErrExportUnavailable
	}
	if options.Documents == nil || options.Jobs == nil || options.Staging == nil {
		return nil, ErrExportUnavailable
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	ttl := options.PreviewTTL
	if ttl <= 0 {
		ttl = defaultPreviewTTL
	}
	return &Service{
		enabled:         options.Enabled,
		backendKind:     options.BackendKind,
		documents:       options.Documents,
		jobs:            options.Jobs,
		evidence:        options.Evidence,
		snapshots:       options.Snapshots,
		comparison:      options.Comparison,
		activity:        options.Activity,
		pdf:             options.PDF,
		imports:         options.Imports,
		importer:        options.Importer,
		evidenceImports: options.EvidenceImports,
		kinds:           options.Kinds,
		attachments:     options.Attachments,
		attachmentBlobs: options.AttachmentBlobs,
		rebuilder:       options.Rebuilder,
		staging:         options.Staging,
		now:             now,
		previewTTL:      ttl,
		previews:        make(map[string]PreviewRequest),
		confirmTokens:   make(map[string]string),
		importPlans:     make(map[string]cachedImportPlan),
	}, nil
}

func (service *Service) Preview(ctx context.Context, request PreviewRequest) (Preview, error) {
	if err := service.requireEnabled(); err != nil {
		return Preview{}, err
	}
	if request.IdempotencyKey == "" {
		return Preview{}, ErrInvalidExportRequest
	}
	material, err := service.buildMaterial(ctx, request)
	if err != nil {
		return Preview{}, err
	}
	confirmToken, err := service.issueSensitiveConfirmToken(request.ExportMode)
	if err != nil {
		return Preview{}, err
	}
	expiresAt := service.now().Add(service.previewTTL)
	job, err := service.jobs.ClaimExportJob(ctx, store.ClaimRecordExportJobInput{
		ActorID:            request.Actor.UserID,
		IdempotencyKey:     request.IdempotencyKey,
		ExportKind:         request.ExportKind,
		ExportMode:         request.ExportMode,
		RequestFingerprint: material.fingerprint,
		InventoryDigest:    material.inventory,
		AuthorizationEpoch: material.document.AuthorizationEpoch,
		RecordID:           material.document.RecordID,
		RevisionID:         material.document.RevisionID,
		ExpiresAt:          expiresAt,
	})
	if err != nil {
		return Preview{}, mapJobError(err)
	}
	service.mu.Lock()
	service.previews[job.ExportJobID] = request
	if confirmToken != "" {
		service.confirmTokens[job.ExportJobID] = confirmToken
	}
	service.mu.Unlock()
	return Preview{
		PreviewID:         job.ExportJobID,
		PreviewToken:      hex.EncodeToString(material.fingerprint[:]),
		ExportKind:        job.ExportKind,
		ExportMode:        job.ExportMode,
		InventoryDigest:   hex.EncodeToString(material.inventory[:]),
		ExpectedFiles:     []ExpectedFile{material.file},
		Unavailable:       material.unavailable,
		RenderStatus:      material.renderStatus,
		ComparisonSummary: material.comparisonSummary,
		ConfirmToken:      confirmToken,
		ExpiresAt:         job.ExpiresAt.UTC(),
	}, nil
}

func (service *Service) Create(ctx context.Context, request CreateRequest) (ExportView, error) {
	if err := service.requireEnabled(); err != nil {
		return ExportView{}, err
	}
	if request.PreviewID == "" || request.PreviewToken == "" || request.InventoryDigest == "" {
		return ExportView{}, ErrInvalidExportRequest
	}
	job, err := service.jobs.LoadExportJob(ctx, request.PreviewID)
	if err != nil {
		return ExportView{}, mapJobError(err)
	}
	if job.ActorID != request.Actor.UserID {
		return ExportView{}, ErrExportUnauthorized
	}
	if !job.ExpiresAt.IsZero() && !service.now().Before(job.ExpiresAt) {
		return ExportView{}, ErrInvalidExportRequest
	}
	if job.JobState == store.RecordExportJobStatePublished {
		return service.Get(ctx, request.Actor, job.ExportJobID)
	}
	if job.JobState != store.RecordExportJobStatePreviewed {
		return ExportView{}, ErrExportUnauthorized
	}
	if err := service.requireSensitiveConfirm(job.ExportJobID, job.ExportMode, request.ConfirmToken); err != nil {
		return ExportView{}, err
	}
	if hex.EncodeToString(job.RequestFingerprint[:]) != request.PreviewToken ||
		hex.EncodeToString(job.InventoryDigest[:]) != request.InventoryDigest {
		return ExportView{}, ErrExportInventoryDrift
	}
	material, err := service.rebuildFromJob(ctx, request.Actor, job)
	if err != nil {
		return ExportView{}, err
	}
	if material.inventory != job.InventoryDigest {
		return ExportView{}, ErrExportInventoryDrift
	}
	if err := service.jobs.AdvanceExportJob(ctx, store.AdvanceRecordExportJobInput{
		ExportJobID: job.ExportJobID,
		LockVersion: job.LockVersion,
		JobState:    store.RecordExportJobStateStaging,
		ExpiresAt:   job.ExpiresAt,
	}); err != nil {
		return ExportView{}, mapJobError(err)
	}
	job.LockVersion++
	if _, err := service.staging.Stage(ctx, job.ExportJobID, material.payload); err != nil {
		_ = service.jobs.AdvanceExportJob(ctx, store.AdvanceRecordExportJobInput{
			ExportJobID: job.ExportJobID,
			LockVersion: job.LockVersion,
			JobState:    store.RecordExportJobStateFailed,
			FailureCode: "staging_failed",
			ExpiresAt:   job.ExpiresAt,
		})
		return ExportView{}, ErrExportUnavailable
	}
	artifact, err := service.jobs.PublishExportArtifact(ctx, store.PublishRecordExportArtifactInput{
		ExportJobID:  job.ExportJobID,
		ArtifactKind: job.ExportKind,
		ContentType:  material.file.MediaType,
		BackendKind:  service.backendKind,
		BlobKey:      "sha256/" + hex.EncodeToString(hashBytes(material.payload)),
		SHA256:       sha256.Sum256(material.payload),
		ByteSize:     uint64(len(material.payload)),
		ExpiresAt:    job.ExpiresAt,
	})
	if err != nil {
		return ExportView{}, mapJobError(err)
	}
	if err := service.jobs.AdvanceExportJob(ctx, store.AdvanceRecordExportJobInput{
		ExportJobID: job.ExportJobID,
		LockVersion: job.LockVersion,
		JobState:    store.RecordExportJobStatePublished,
		ExpiresAt:   job.ExpiresAt,
	}); err != nil {
		return ExportView{}, mapJobError(err)
	}
	return ExportView{
		ExportID:   job.ExportJobID,
		JobState:   store.RecordExportJobStatePublished,
		ExportKind: job.ExportKind,
		MediaType:  artifact.ContentType,
		ByteSize:   artifact.ByteSize,
		ExpiresAt:  job.ExpiresAt.UTC(),
	}, nil
}

func (service *Service) Get(ctx context.Context, actor recordauth.ActorScope, exportID string) (ExportView, error) {
	if err := service.requireEnabled(); err != nil {
		return ExportView{}, err
	}
	job, artifact, err := service.loadAuthorizedPublished(ctx, actor, exportID)
	if err != nil {
		return ExportView{}, err
	}
	return ExportView{
		ExportID:   job.ExportJobID,
		JobState:   job.JobState,
		ExportKind: job.ExportKind,
		MediaType:  artifact.ContentType,
		ByteSize:   artifact.ByteSize,
		ExpiresAt:  job.ExpiresAt.UTC(),
	}, nil
}

func (service *Service) OpenContent(ctx context.Context, actor recordauth.ActorScope, exportID string) (Content, error) {
	if err := service.requireEnabled(); err != nil {
		return Content{}, err
	}
	job, artifact, err := service.loadAuthorizedPublished(ctx, actor, exportID)
	if err != nil {
		return Content{}, err
	}
	if artifact.RevokedAt != nil {
		return Content{}, ErrExportLeaseRevoked
	}
	if _, err := service.documents.ExportDocument(ctx, records.ExportDocumentRequest{
		Actor: actor, RecordID: job.RecordID, RevisionID: job.RevisionID,
	}); err != nil {
		return Content{}, mapDocumentError(err)
	}
	if err := authorizeExportCapabilities(actor, job.ExportMode); err != nil {
		return Content{}, err
	}
	body, _, err := service.staging.OpenLeased(ctx, job.ExportJobID)
	if err != nil {
		return Content{}, err
	}
	return Content{
		MediaType: artifact.ContentType,
		Filename:  expectedFilename(job.ExportKind),
		ByteSize:  int64(artifact.ByteSize),
		Body:      body,
	}, nil
}

func (service *Service) Revoke(ctx context.Context, actor recordauth.ActorScope, exportID string) error {
	if err := service.requireEnabled(); err != nil {
		return err
	}
	job, err := service.jobs.LoadExportJob(ctx, exportID)
	if err != nil {
		return mapJobError(err)
	}
	if job.ActorID != actor.UserID {
		return ErrExportUnauthorized
	}
	service.staging.Revoke(exportID)
	return mapJobError(service.jobs.RevokeExport(ctx, exportID, job.LockVersion))
}

func (service *Service) requireEnabled() error {
	if service == nil {
		return ErrExportUnavailable
	}
	if !service.enabled {
		return ErrPortabilityDisabled
	}
	return nil
}

func (service *Service) loadAuthorizedPublished(
	ctx context.Context,
	actor recordauth.ActorScope,
	exportID string,
) (store.RecordExportJob, store.RecordExportArtifact, error) {
	job, err := service.jobs.LoadExportJob(ctx, exportID)
	if err != nil {
		return store.RecordExportJob{}, store.RecordExportArtifact{}, mapJobError(err)
	}
	if job.ActorID != actor.UserID {
		return store.RecordExportJob{}, store.RecordExportArtifact{}, ErrExportUnauthorized
	}
	if job.JobState == store.RecordExportJobStateRevoked || service.now().After(job.ExpiresAt) {
		return store.RecordExportJob{}, store.RecordExportArtifact{}, ErrExportLeaseRevoked
	}
	if job.JobState != store.RecordExportJobStatePublished {
		return store.RecordExportJob{}, store.RecordExportArtifact{}, ErrExportNotFound
	}
	artifact, err := service.jobs.LoadExportArtifact(ctx, exportID)
	if err != nil {
		return store.RecordExportJob{}, store.RecordExportArtifact{}, mapJobError(err)
	}
	return job, artifact, nil
}

type preparedMaterial struct {
	document          records.ExportDocument
	payload           []byte
	file              ExpectedFile
	unavailable       []UnavailableMaterial
	renderStatus      recordmarkdown.DocumentRenderStatus
	comparisonSummary map[string]any
	fingerprint       [32]byte
	inventory         [32]byte
	snapshotID        string
	includeActivity   bool
}

func (service *Service) buildMaterial(ctx context.Context, request PreviewRequest) (preparedMaterial, error) {
	if ctx == nil || request.RecordID == "" {
		return preparedMaterial{}, ErrInvalidExportRequest
	}
	if !supportedExportKind(request.ExportKind) || !supportedExportMode(request.ExportMode) {
		return preparedMaterial{}, ErrInvalidExportRequest
	}
	if _, err := recordauth.NormalizeActorScope(request.Actor); err != nil {
		return preparedMaterial{}, ErrInvalidExportRequest
	}
	if err := authorizeExportCapabilities(request.Actor, request.ExportMode); err != nil {
		return preparedMaterial{}, err
	}
	document, err := service.documents.ExportDocument(ctx, records.ExportDocumentRequest{
		Actor: request.Actor, RecordID: request.RecordID, RevisionID: request.RevisionID,
	})
	if err != nil {
		return preparedMaterial{}, mapDocumentError(err)
	}
	material := preparedMaterial{document: document, snapshotID: request.SnapshotID, includeActivity: request.IncludeActivity}
	switch request.ExportKind {
	case ExportKindMarkdown:
		if err := service.fillMarkdown(ctx, request, &material); err != nil {
			return preparedMaterial{}, err
		}
	case ExportKindComparisonJSON:
		if err := service.fillComparison(ctx, request, &material); err != nil {
			return preparedMaterial{}, err
		}
	case ExportKindEvidenceJSON:
		if err := service.fillEvidence(ctx, request, &material); err != nil {
			return preparedMaterial{}, err
		}
	case ExportKindArchive:
		if err := service.fillArchive(ctx, request, &material); err != nil {
			return preparedMaterial{}, err
		}
	case ExportKindPDF:
		if err := service.fillPDF(ctx, request, &material); err != nil {
			return preparedMaterial{}, err
		}
	default:
		return preparedMaterial{}, ErrUnsupportedExportKind
	}
	if len(material.payload) == 0 || uint64(len(material.payload)) > maxPayloadBytesForKind(request.ExportKind) {
		return preparedMaterial{}, ErrInvalidExportRequest
	}
	material.file = ExpectedFile{
		Name: expectedFilename(request.ExportKind), MediaType: expectedMediaType(request.ExportKind),
		ByteSize: int64(len(material.payload)),
	}
	material.fingerprint = requestFingerprint(request)
	material.inventory = inventoryDigest(request, document, material)
	return material, nil
}

func (service *Service) rebuildFromJob(
	ctx context.Context,
	actor recordauth.ActorScope,
	job store.RecordExportJob,
) (preparedMaterial, error) {
	service.mu.Lock()
	cached, ok := service.previews[job.ExportJobID]
	service.mu.Unlock()
	if !ok {
		return preparedMaterial{}, ErrExportUnavailable
	}
	cached.Actor = actor
	cached.RecordID = job.RecordID
	cached.RevisionID = job.RevisionID
	cached.ExportKind = job.ExportKind
	cached.ExportMode = job.ExportMode
	cached.IdempotencyKey = job.IdempotencyKey
	return service.buildMaterial(ctx, cached)
}

func (service *Service) fillMarkdown(ctx context.Context, request PreviewRequest, material *preparedMaterial) error {
	_, status, err := recordmarkdown.SafeDocumentHTML(material.document.BodyMarkdown, nil)
	if err != nil {
		return ErrExportUnavailable
	}
	material.renderStatus = status
	unavailable := make([]UnavailableMaterial, 0)
	included := make([]string, 0, len(material.document.EvidenceSnapshotIDs))
	for _, snapshotID := range material.document.EvidenceSnapshotIDs {
		if service.evidence == nil {
			unavailable = append(unavailable, UnavailableMaterial{Kind: "evidence", ID: snapshotID, Reason: "unavailable"})
			continue
		}
		if _, err := service.evidence.Export(ctx, evidence.ExportRequest{
			Actor: request.Actor, SnapshotID: snapshotID, Mode: evidenceExportMode(request.ExportMode),
		}); err != nil {
			unavailable = append(unavailable, UnavailableMaterial{
				Kind: "evidence", ID: snapshotID, Reason: materialReason(err),
			})
			continue
		}
		included = append(included, snapshotID)
	}
	_, attachmentIncluded, attachmentUnavailable := service.evaluateExportAttachments(
		ctx, request, material.document.RecordID, material.document.AttachmentIDs, false, 0, 0,
	)
	included = append(included, attachmentIncluded...)
	unavailable = append(unavailable, attachmentUnavailable...)
	if request.IncludeActivity {
		if err := service.includeActivity(ctx, request.Actor, material.document.RecordID); err != nil {
			unavailable = append(unavailable, UnavailableMaterial{Kind: "activity", ID: material.document.RecordID, Reason: materialReason(err)})
		} else {
			included = append(included, "activity:"+material.document.RecordID)
		}
	}
	material.unavailable = unavailable
	material.payload = renderMarkdownExport(material.document, included, unavailable)
	return nil
}

func (service *Service) fillComparison(ctx context.Context, request PreviewRequest, material *preparedMaterial) error {
	if service.comparison == nil || service.snapshots == nil || request.SnapshotID == "" {
		return ErrInvalidExportRequest
	}
	authorized, err := service.snapshots.LoadAuthorizedEvidenceSnapshot(ctx, request.Actor, request.SnapshotID)
	if err != nil {
		return mapDocumentError(err)
	}
	if authorized.Key != evidence.ComparisonResultV1Key() {
		return ErrInvalidExportRequest
	}
	exported := service.comparison.Export(authorized.Snapshot, evidenceExportMode(request.ExportMode))
	if exported.MediaType != "application/json" || len(exported.Bytes) == 0 {
		return ErrExportUnavailable
	}
	if !bytes.Equal(exported.Bytes, authorized.Snapshot.Bytes()) {
		return ErrExportUnavailable
	}
	if containsForbiddenComparisonField(exported.Bytes) {
		return ErrExportUnavailable
	}
	summary := service.comparison.Summarize(authorized.Snapshot)
	if containsForbiddenComparisonField(mustJSON(summary.ReadModel)) {
		return ErrExportUnavailable
	}
	material.payload = append([]byte(nil), exported.Bytes...)
	material.comparisonSummary = summary.ReadModel
	material.snapshotID = request.SnapshotID
	return nil
}

func (service *Service) fillEvidence(ctx context.Context, request PreviewRequest, material *preparedMaterial) error {
	if service.evidence == nil || request.SnapshotID == "" {
		return ErrInvalidExportRequest
	}
	exported, err := service.evidence.Export(ctx, evidence.ExportRequest{
		Actor: request.Actor, SnapshotID: request.SnapshotID, Mode: evidenceExportMode(request.ExportMode),
	})
	if err != nil {
		return mapDocumentError(err)
	}
	if len(exported.Bytes) == 0 {
		return ErrExportUnavailable
	}
	material.payload = append([]byte(nil), exported.Bytes...)
	material.snapshotID = request.SnapshotID
	return nil
}

func (service *Service) fillArchive(ctx context.Context, request PreviewRequest, material *preparedMaterial) error {
	if err := service.fillMarkdown(ctx, request, material); err != nil {
		return err
	}
	recordID := material.document.RecordID
	evidenceEntries, evidenceIncluded, wrapUnavailable := service.evaluateOfficialArchiveEvidence(ctx, request, material.document)
	unavailable := append(keepNonAttachmentUnavailable(material.unavailable), wrapUnavailable...)
	var comparisonEntries []ArchiveEntry
	if request.SnapshotID != "" && service.comparison != nil && service.snapshots != nil {
		comparison := *material
		if err := service.fillComparison(ctx, request, &comparison); err != nil {
			if !errors.Is(err, ErrInvalidExportRequest) {
				unavailable = append(unavailable, UnavailableMaterial{
					Kind: "comparison", ID: request.SnapshotID, Reason: materialReason(err),
				})
			}
		} else {
			comparisonEntries = append(comparisonEntries, ArchiveEntry{
				Path:           "records/" + recordID + "/comparison.result_v1.json",
				Classification: ArchiveClassComparisonJSON,
				Payload:        append([]byte(nil), comparison.payload...),
			})
			material.comparisonSummary = comparison.comparisonSummary
			material.snapshotID = request.SnapshotID
		}
	}
	draftMarkdown := renderMarkdownExport(material.document, evidenceIncluded, unavailable)
	archiveBytes := len(draftMarkdown)
	for _, entry := range evidenceEntries {
		archiveBytes += len(entry.Payload)
	}
	for _, entry := range comparisonEntries {
		archiveBytes += len(entry.Payload)
	}
	attachmentEntries, attachmentIncluded, attachmentUnavailable := service.evaluateExportAttachments(
		ctx, request, recordID, material.document.AttachmentIDs, true,
		1+len(evidenceEntries)+len(comparisonEntries), archiveBytes,
	)
	included := append(append([]string(nil), evidenceIncluded...), attachmentIncluded...)
	unavailable = append(unavailable, attachmentUnavailable...)
	entries := []ArchiveEntry{{
		Path:           "records/" + recordID + "/document.md",
		Classification: ArchiveClassMarkdown,
		Payload:        renderMarkdownExport(material.document, included, unavailable),
	}}
	entries = append(entries, evidenceEntries...)
	entries = append(entries, comparisonEntries...)
	entries = append(entries, attachmentEntries...)
	raw, err := WriteArchiveV1(entries)
	if err != nil {
		return ErrInvalidExportRequest
	}
	material.unavailable = unavailable
	material.payload = raw
	return nil
}

func (service *Service) evaluateOfficialArchiveEvidence(
	ctx context.Context,
	request PreviewRequest,
	document records.ExportDocument,
) (entries []ArchiveEntry, included []string, unavailable []UnavailableMaterial) {
	recordID := document.RecordID
	for _, snapshotID := range document.EvidenceSnapshotIDs {
		if service == nil || service.evidence == nil {
			continue
		}
		exported, err := service.evidence.Export(ctx, evidence.ExportRequest{
			Actor: request.Actor, SnapshotID: snapshotID, Mode: evidenceExportMode(request.ExportMode),
		})
		if err != nil || len(exported.Bytes) == 0 {
			continue
		}
		if service.snapshots == nil {
			unavailable = append(unavailable, UnavailableMaterial{Kind: "evidence", ID: snapshotID, Reason: "unavailable"})
			continue
		}
		authorized, loadErr := service.snapshots.LoadAuthorizedEvidenceSnapshot(ctx, request.Actor, snapshotID)
		if loadErr != nil || authorized.Snapshot.Size() == 0 {
			unavailable = append(unavailable, UnavailableMaterial{Kind: "evidence", ID: snapshotID, Reason: "unavailable"})
			continue
		}
		wrapped, wrapErr := encodeOfficialEvidenceRestoreMember(authorized.Snapshot, exported.Bytes)
		if wrapErr != nil {
			unavailable = append(unavailable, UnavailableMaterial{Kind: "evidence", ID: snapshotID, Reason: "unavailable"})
			continue
		}
		included = append(included, snapshotID)
		entries = append(entries, ArchiveEntry{
			Path:           "records/" + recordID + "/evidence/" + snapshotID + ".json",
			Classification: ArchiveClassEvidenceJSON,
			Payload:        wrapped,
		})
	}
	return entries, included, unavailable
}

func (service *Service) fillPDF(ctx context.Context, request PreviewRequest, material *preparedMaterial) error {
	if service.pdf == nil {
		return ErrUnsupportedExportKind
	}
	if err := service.fillMarkdown(ctx, request, material); err != nil {
		return err
	}
	html, status, err := recordmarkdown.SafeDocumentHTML(string(material.payload), nil)
	if err != nil {
		return ErrExportUnavailable
	}
	pdf, err := service.pdf.Render(ctx, html)
	if err != nil {
		return ErrExportUnavailable
	}
	extracted, err := recordmarkdown.ExtractDerivedHTML(pdf)
	if err != nil || extracted != html {
		return ErrExportUnavailable
	}
	material.renderStatus = status
	material.payload = pdf
	return nil
}

func (service *Service) includeActivity(ctx context.Context, actor recordauth.ActorScope, recordID string) error {
	if service.activity == nil {
		return ErrExportUnavailable
	}
	selection, err := activity.NormalizeRecordSelection(activity.RecordSelection{RecordIDs: []string{recordID}})
	if err != nil {
		return err
	}
	readiness, err := service.activity.Readiness(ctx, actor, selection)
	if err != nil {
		return err
	}
	if err := readiness.ValidateForExport(nil); err != nil {
		return err
	}
	_, err = service.activity.ScanRecordPage(ctx, actor, selection, readiness.Snapshot, activity.PageCursor{})
	return err
}

func renderMarkdownExport(document records.ExportDocument, included []string, unavailable []UnavailableMaterial) []byte {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(document.Title)
	builder.WriteString("\n\n")
	builder.WriteString(document.BodyMarkdown)
	if !strings.HasSuffix(document.BodyMarkdown, "\n") {
		builder.WriteString("\n")
	}
	if len(included) > 0 {
		builder.WriteString("\n## 已授权材料\n\n")
		for _, id := range included {
			builder.WriteString("- ")
			builder.WriteString(id)
			builder.WriteString("\n")
		}
	}
	if len(unavailable) > 0 {
		builder.WriteString("\n## 不可用材料\n\n")
		for _, item := range unavailable {
			fmt.Fprintf(&builder, "- %s `%s`：%s\n", item.Kind, item.ID, item.Reason)
		}
	}
	return []byte(builder.String())
}

func requestFingerprint(request PreviewRequest) [32]byte {
	return sha256.Sum256(mustJSON(map[string]any{
		"actor_id":         request.Actor.UserID,
		"export_kind":      request.ExportKind,
		"export_mode":      request.ExportMode,
		"include_activity": request.IncludeActivity,
		"record_id":        request.RecordID,
		"revision_id":      request.RevisionID,
		"snapshot_id":      request.SnapshotID,
	}))
}

func inventoryDigest(request PreviewRequest, document records.ExportDocument, material preparedMaterial) [32]byte {
	items := make([]map[string]string, 0, len(material.unavailable))
	for _, item := range material.unavailable {
		items = append(items, map[string]string{"id": item.ID, "kind": item.Kind, "reason": item.Reason})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i]["kind"] != items[j]["kind"] {
			return items[i]["kind"] < items[j]["kind"]
		}
		return items[i]["id"] < items[j]["id"]
	})
	return sha256.Sum256(mustJSON(map[string]any{
		"authorization_epoch": document.AuthorizationEpoch,
		"export_kind":         request.ExportKind,
		"export_mode":         request.ExportMode,
		"include_activity":    request.IncludeActivity,
		"lock_version":        document.LockVersion,
		"payload_sha256":      hex.EncodeToString(hashBytes(material.payload)),
		"record_id":           document.RecordID,
		"revision_id":         document.RevisionID,
		"snapshot_id":         material.snapshotID,
		"unavailable":         items,
	}))
}

func supportedExportKind(kind string) bool {
	return kind == ExportKindMarkdown || kind == ExportKindComparisonJSON ||
		kind == ExportKindEvidenceJSON || kind == ExportKindArchive || kind == ExportKindPDF
}

func maxPayloadBytesForKind(kind string) uint64 {
	switch kind {
	case ExportKindArchive:
		return archiveV1MaxTotalBytes
	case ExportKindPDF:
		return 2 << 20
	default:
		return maxExportPayloadBytes
	}
}

func supportedExportMode(mode string) bool {
	return mode == ExportModeSafe || mode == ExportModeSensitiveTopo
}

func expectedMediaType(kind string) string {
	switch kind {
	case ExportKindMarkdown:
		return "text/markdown"
	case ExportKindArchive:
		return "application/zip"
	case ExportKindPDF:
		return "application/pdf"
	default:
		return "application/json"
	}
}

func expectedFilename(kind string) string {
	switch kind {
	case ExportKindMarkdown:
		return "record.md"
	case ExportKindComparisonJSON:
		return "comparison.result_v1.json"
	case ExportKindEvidenceJSON:
		return "evidence.json"
	case ExportKindArchive:
		return "houfeng-record-archive-v1.zip"
	case ExportKindPDF:
		return "record.pdf"
	default:
		return "export.bin"
	}
}

func authorizeExportCapabilities(actor recordauth.ActorScope, mode string) error {
	if err := recordauth.AllowsCapability(actor, recordauth.CapabilityExport); err != nil {
		return ErrExportUnauthorized
	}
	if mode == ExportModeSensitiveTopo {
		if err := recordauth.AllowsCapability(actor, recordauth.CapabilityExportSensitiveTopology); err != nil {
			return ErrExportUnauthorized
		}
	}
	return nil
}

func (service *Service) issueSensitiveConfirmToken(mode string) (string, error) {
	if mode != ExportModeSensitiveTopo {
		return "", nil
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", ErrExportUnavailable
	}
	return hex.EncodeToString(raw[:]), nil
}

func (service *Service) requireSensitiveConfirm(previewID, mode, token string) error {
	if mode != ExportModeSensitiveTopo {
		return nil
	}
	if service == nil || strings.TrimSpace(token) == "" {
		return ErrExportUnauthorized
	}
	service.mu.Lock()
	want := service.confirmTokens[previewID]
	service.mu.Unlock()
	if want == "" || want != token {
		return ErrExportUnauthorized
	}
	return nil
}

func evidenceExportMode(mode string) evidence.ExportMode {
	if mode == ExportModeSensitiveTopo {
		return evidence.ExportModeSensitiveTopology
	}
	return evidence.ExportModeSafe
}

func keepNonAttachmentUnavailable(items []UnavailableMaterial) []UnavailableMaterial {
	kept := make([]UnavailableMaterial, 0, len(items))
	for _, item := range items {
		if item.Kind != "attachment" {
			kept = append(kept, item)
		}
	}
	return kept
}

func materialReason(err error) string {
	if errors.Is(err, recordauth.ErrDenied) {
		return "unauthorized"
	}
	if errors.Is(err, activity.ErrExportNotReady) || errors.Is(err, activity.ErrIncompleteReadiness) {
		return "not_ready"
	}
	return "unavailable"
}

func mapDocumentError(err error) error {
	if errors.Is(err, recordauth.ErrDenied) {
		return ErrExportUnauthorized
	}
	if errors.Is(err, records.ErrInvalidApplicationRequest) || errors.Is(err, evidence.ErrInvalidExportRequest) {
		return ErrInvalidExportRequest
	}
	return ErrExportUnavailable
}

func mapJobError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrRecordExportNotFound) {
		return ErrExportNotFound
	}
	if errors.Is(err, store.ErrRecordExportJobConflict) || errors.Is(err, store.ErrRecordExportJobCASConflict) {
		return ErrExportInventoryDrift
	}
	if errors.Is(err, store.ErrRecordPlatformAdmissionUnavailable) {
		return ErrExportUnavailable
	}
	return ErrExportUnavailable
}

func containsForbiddenComparisonField(raw []byte) bool {
	lower := strings.ToLower(string(raw))
	for _, forbidden := range []string{"\"conclusion\"", "\"markdown\"", "\"body_markdown\""} {
		if strings.Contains(lower, forbidden) {
			return true
		}
	}
	return false
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

func hashBytes(payload []byte) []byte {
	sum := sha256.Sum256(payload)
	return sum[:]
}

var _ io.ReadCloser
var _ = attachments.BackendKindLocal
