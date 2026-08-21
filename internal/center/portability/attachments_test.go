package portability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
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
}

func TestOfficialAttachmentAdmissionDoesNotTrustArchivePath(t *testing.T) {
	t.Parallel()

	displayName, mediaType, err := officialAttachmentAdmission("claimed.png", []byte("hello notes\n"))
	if err != nil || displayName != "claimed.txt" || mediaType != "text/plain; charset=utf-8" {
		t.Fatalf("officialAttachmentAdmission() = (%q, %q, %v)", displayName, mediaType, err)
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
