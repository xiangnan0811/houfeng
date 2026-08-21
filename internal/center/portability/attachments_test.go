package portability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

func TestEvaluateExportAttachmentsNamesUnauthorizedAndOverLimit(t *testing.T) {
	t.Parallel()

	authorized := "att_notes00000001"
	denied := "att_denied0000001"
	overLimit := "att_toolarge00001"
	notes := []byte("hello notes\n")
	service := &Service{attachments: attachmentMapSource{
		authorized: map[string]AttachmentMaterial{
			authorized: {AttachmentID: authorized, DisplayName: "notes.txt", Bytes: notes},
			overLimit:  {AttachmentID: overLimit, DisplayName: "huge.txt", Bytes: notes},
		},
		denied: map[string]error{denied: recordauth.ErrDenied},
	}}
	entries, included, unavailable := service.evaluateExportAttachments(
		context.Background(),
		PreviewRequest{Actor: portabilityTestActor(t)},
		"rec_attach1",
		[]string{authorized, denied, overLimit},
		true,
		1,
		archiveV1MaxTotalBytes-20,
	)
	if len(entries) != 1 || !bytes.Equal(entries[0].Payload, notes) {
		t.Fatalf("entries = %#v, want authorized notes only", entries)
	}
	if len(included) != 1 || included[0] != "attachment:"+authorized {
		t.Fatalf("included = %#v", included)
	}
	if len(unavailable) != 2 ||
		unavailable[0].ID != denied || unavailable[0].Reason != "unauthorized" ||
		unavailable[1].ID != overLimit || unavailable[1].Reason != "over_archive_limit" {
		t.Fatalf("unavailable = %#v", unavailable)
	}
}

func TestEvaluateExportAttachmentsNamesUnsupportedAndOmitsBytes(t *testing.T) {
	t.Parallel()

	gzipID := "att_gzip000000001"
	gzipBytes := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff}
	service := &Service{attachments: attachmentMapSource{
		authorized: map[string]AttachmentMaterial{
			gzipID: {AttachmentID: gzipID, DisplayName: "bundle.gz", Bytes: gzipBytes},
		},
	}}
	entries, included, unavailable := service.evaluateExportAttachments(
		context.Background(),
		PreviewRequest{Actor: portabilityTestActor(t)},
		"rec_attach1",
		[]string{gzipID},
		true,
		1,
		0,
	)
	if len(entries) != 0 || len(included) != 0 {
		t.Fatalf("entries=%#v included=%#v, want omitted unsupported archive", entries, included)
	}
	if len(unavailable) != 1 || unavailable[0].ID != gzipID || unavailable[0].Reason != "unsupported" {
		t.Fatalf("unavailable = %#v, want named unsupported attachment", unavailable)
	}
}

func TestOfficialArchivePreviewNamesAndRestoresAuthorizedAttachmentBytes(t *testing.T) {
	t.Parallel()

	authorized := "att_notes00000001"
	denied := "att_denied0000001"
	notes := []byte("hello notes\n")
	service, _ := mustPortabilityService(t, portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_attach1", RevisionID: "rrv_attach1", Title: "Disk notes",
			BodyMarkdown: "# Body\n", AuthorizationEpoch: 1, LockVersion: 1,
			AttachmentIDs: []string{authorized, denied},
		},
		attachments: attachmentMapSource{
			authorized: map[string]AttachmentMaterial{
				authorized: {AttachmentID: authorized, DisplayName: "notes.txt", Bytes: notes},
			},
			denied: map[string]error{denied: recordauth.ErrDenied},
		},
	})
	capture := &capturingBlobStore{inner: service.attachmentBlobs}
	service.attachmentBlobs = capture
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-att-1",
		RecordID: "rec_attach1", ExportKind: ExportKindArchive, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(preview.Unavailable) != 1 || preview.Unavailable[0].ID != denied || preview.Unavailable[0].Reason != "unauthorized" {
		t.Fatalf("unavailable = %#v, want named unauthorized attachment", preview.Unavailable)
	}
	raw := mustReadPreviewPayload(t, service, preview)
	_, entries, err := ReadArchiveV1(raw)
	if err != nil {
		t.Fatalf("ReadArchiveV1() error = %v", err)
	}
	var found []byte
	for _, entry := range entries {
		if entry.Classification == ArchiveClassAttachment {
			found = entry.Payload
			if entry.Path != "records/rec_attach1/attachments/"+authorized+"/notes.txt" {
				t.Fatalf("attachment path = %q", entry.Path)
			}
		}
	}
	if !bytes.Equal(found, notes) {
		t.Fatal("archive omitted authorized attachment bytes")
	}

	importer := &importWriterStub{}
	imports := newMemoryImportRepository()
	importer.repo = imports
	service.imports = imports
	service.importer = importer
	service.evidenceImports = &evidenceImportStub{}
	service.rebuilder = &importRebuildStub{}
	plan, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-att-1", Archive: raw,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: plan.PlanID, LockVersion: plan.LockVersion,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if importer.writes != 1 || len(importer.attachments) != 1 || len(importer.attachments[0]) != 1 {
		t.Fatalf("attachments = %#v writes=%d", importer.attachments, importer.writes)
	}
	if len(importer.importedAtts) != 1 || len(importer.importedAtts[0]) != 1 {
		t.Fatalf("imported attachments = %#v", importer.importedAtts)
	}
	restored := importer.importedAtts[0][0]
	if restored.DisplayName != "notes.txt" || restored.MediaType != "text/plain" ||
		restored.LogicalSizeBytes != int64(len(notes)) || restored.Validate() != nil {
		t.Fatalf("imported attachment = %#v", restored)
	}
	opened, err := service.attachmentBlobs.Open(context.Background(), restored.Object, attachments.FullByteRange())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer opened.Close()
	got, err := io.ReadAll(opened)
	if err != nil || !bytes.Equal(got, notes) {
		t.Fatalf("restored blob = %q err=%v", got, err)
	}
	if restored.Object.SHA256 != sha256.Sum256(notes) {
		t.Fatal("restored object digest drifted")
	}
	if len(capture.puts) != 1 {
		t.Fatalf("blob puts = %d, want 1", len(capture.puts))
	}
	requireBlobTemporaryKey(t, capture.puts[0].TemporaryKey)
}

func TestOfficialArchiveRestoresWebPAndNamesUnsupportedOnPreview(t *testing.T) {
	t.Parallel()

	webpID := "att_webp000000001"
	gzipID := "att_gzip000000001"
	webp := mustOfficialWebP(t)
	gzipBytes := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xff}
	service, _ := mustPortabilityService(t, portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_webp1", RevisionID: "rrv_webp1", Title: "Screen",
			BodyMarkdown: "# Body\n", AuthorizationEpoch: 1, LockVersion: 1,
			AttachmentIDs: []string{webpID, gzipID},
		},
		attachments: attachmentMapSource{
			authorized: map[string]AttachmentMaterial{
				webpID: {AttachmentID: webpID, DisplayName: "screen.webp", Bytes: webp},
				gzipID: {AttachmentID: gzipID, DisplayName: "bundle.gz", Bytes: gzipBytes},
			},
		},
	})
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-webp-1",
		RecordID: "rec_webp1", ExportKind: ExportKindArchive, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(preview.Unavailable) != 1 || preview.Unavailable[0].ID != gzipID || preview.Unavailable[0].Reason != "unsupported" {
		t.Fatalf("unavailable = %#v, want named unsupported gzip", preview.Unavailable)
	}
	raw := mustReadPreviewPayload(t, service, preview)
	_, entries, err := ReadArchiveV1(raw)
	if err != nil {
		t.Fatalf("ReadArchiveV1() error = %v", err)
	}
	var found []byte
	for _, entry := range entries {
		if entry.Classification == ArchiveClassAttachment {
			found = entry.Payload
			if entry.Path != "records/rec_webp1/attachments/"+webpID+"/screen.webp" {
				t.Fatalf("attachment path = %q", entry.Path)
			}
		}
		if entry.Classification == ArchiveClassMarkdown {
			if !strings.Contains(string(entry.Payload), gzipID) || strings.Contains(string(entry.Payload), "attachment:"+gzipID) {
				t.Fatalf("archive markdown mishandled unsupported gzip: %s", entry.Payload)
			}
		}
	}
	if !bytes.Equal(found, webp) {
		t.Fatal("archive omitted authorized WebP bytes")
	}

	importer := &importWriterStub{}
	imports := newMemoryImportRepository()
	importer.repo = imports
	service.imports = imports
	service.importer = importer
	service.evidenceImports = &evidenceImportStub{}
	service.rebuilder = &importRebuildStub{}
	plan, err := service.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-webp-1", Archive: raw,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if _, err := service.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: plan.PlanID, LockVersion: plan.LockVersion,
	}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(importer.importedAtts) != 1 || len(importer.importedAtts[0]) != 1 {
		t.Fatalf("imported attachments = %#v", importer.importedAtts)
	}
	restored := importer.importedAtts[0][0]
	if restored.DisplayName != "screen.webp" || restored.MediaType != "image/webp" {
		t.Fatalf("imported attachment = %#v", restored)
	}
}

func TestOfficialArchiveDocumentMarkdownNamesOverLimitAttachments(t *testing.T) {
	t.Parallel()

	authorized := "att_notes00000001"
	overLimit := "att_toolarge00001"
	notes := []byte("hello notes\n")
	huge := bytes.Repeat([]byte("x"), int(archiveV1MaxEntryBytes)+1)
	service, _ := mustPortabilityService(t, portabilityHarness{
		enabled: true,
		document: records.ExportDocument{
			RecordID: "rec_over1", RevisionID: "rrv_over1", Title: "Disk notes",
			BodyMarkdown: "# Body\n", AuthorizationEpoch: 1, LockVersion: 1,
			AttachmentIDs: []string{authorized, overLimit},
		},
		attachments: attachmentMapSource{
			authorized: map[string]AttachmentMaterial{
				authorized: {AttachmentID: authorized, DisplayName: "notes.txt", Bytes: notes},
				overLimit:  {AttachmentID: overLimit, DisplayName: "huge.txt", Bytes: huge},
			},
		},
	})
	preview, err := service.Preview(context.Background(), PreviewRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "export-over-1",
		RecordID: "rec_over1", ExportKind: ExportKindArchive, ExportMode: ExportModeSafe,
	})
	if err != nil {
		t.Fatalf("Preview() error = %v", err)
	}
	if len(preview.Unavailable) != 1 || preview.Unavailable[0].ID != overLimit || preview.Unavailable[0].Reason != "over_archive_limit" {
		t.Fatalf("unavailable = %#v, want named over-limit attachment", preview.Unavailable)
	}
	raw := mustReadPreviewPayload(t, service, preview)
	_, entries, err := ReadArchiveV1(raw)
	if err != nil {
		t.Fatalf("ReadArchiveV1() error = %v", err)
	}
	for _, entry := range entries {
		if entry.Classification == ArchiveClassMarkdown {
			body := string(entry.Payload)
			if !strings.Contains(body, overLimit) || !strings.Contains(body, "over_archive_limit") ||
				strings.Contains(body, "attachment:"+overLimit) {
				t.Fatalf("archive markdown mishandled over-limit attachment: %s", body)
			}
		}
		if entry.Classification == ArchiveClassAttachment && strings.Contains(entry.Path, overLimit) {
			t.Fatal("archive packed over-limit attachment bytes")
		}
	}
}

func TestOfficialAttachmentAdmissionDoesNotTrustArchivePath(t *testing.T) {
	t.Parallel()

	displayName, mediaType, err := officialAttachmentAdmission("claimed.png", []byte("hello notes\n"))
	if err != nil || displayName != "claimed.txt" || mediaType != "text/plain; charset=utf-8" {
		t.Fatalf("officialAttachmentAdmission() = (%q, %q, %v)", displayName, mediaType, err)
	}
	webp := mustOfficialWebP(t)
	displayName, mediaType, err = officialAttachmentAdmission("claimed.png", webp)
	if err != nil || displayName != "claimed.webp" || mediaType != "image/webp" {
		t.Fatalf("officialAttachmentAdmission(webp) = (%q, %q, %v)", displayName, mediaType, err)
	}
}

type attachmentMapSource struct {
	authorized map[string]AttachmentMaterial
	denied     map[string]error
}

func (source attachmentMapSource) OpenAuthorized(
	_ context.Context,
	_ recordauth.ActorScope,
	attachmentID string,
) (AttachmentMaterial, error) {
	if err := source.denied[attachmentID]; err != nil {
		return AttachmentMaterial{}, err
	}
	if material, ok := source.authorized[attachmentID]; ok {
		return material, nil
	}
	return AttachmentMaterial{}, recordauth.ErrDenied
}

type capturingBlobStore struct {
	inner attachments.BlobStore
	puts  []attachments.PutRequest
}

func (store *capturingBlobStore) Put(
	ctx context.Context,
	request attachments.PutRequest,
	reader io.Reader,
) (attachments.ObjectVersion, error) {
	store.puts = append(store.puts, request)
	return store.inner.Put(ctx, request, reader)
}

func (store *capturingBlobStore) Open(
	ctx context.Context,
	version attachments.ObjectVersion,
	byteRange attachments.ByteRange,
) (io.ReadCloser, error) {
	return store.inner.Open(ctx, version, byteRange)
}

func (store *capturingBlobStore) Stat(ctx context.Context, version attachments.ObjectVersion) (attachments.ObjectInfo, error) {
	return store.inner.Stat(ctx, version)
}

func (store *capturingBlobStore) Delete(ctx context.Context, version attachments.ObjectVersion) (attachments.DeletionReceipt, error) {
	return store.inner.Delete(ctx, version)
}

func requireBlobTemporaryKey(t *testing.T, key string) {
	t.Helper()
	if len(key) != len("temporary/")+64 || !strings.HasPrefix(key, "temporary/") {
		t.Fatalf("TemporaryKey = %q, want temporary/<64 hex>", key)
	}
	for _, character := range key[len("temporary/"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			t.Fatalf("TemporaryKey = %q, want lowercase hex digest", key)
		}
	}
}

func mustOfficialWebP(t *testing.T) []byte {
	t.Helper()
	content, err := base64.StdEncoding.DecodeString("UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAUAmJaQAA3AA/vuUAAA=")
	if err != nil {
		t.Fatalf("decode WebP fixture: %v", err)
	}
	return content
}
