package portability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"path"
	"strings"
	"unicode/utf8"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/ids"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/store"
)

type AttachmentSource interface {
	OpenAuthorized(context.Context, recordauth.ActorScope, string) (AttachmentMaterial, error)
}

type downloadAttachmentSource struct {
	downloads *attachments.DownloadService
}

func NewDownloadAttachmentSource(downloads *attachments.DownloadService) AttachmentSource {
	return downloadAttachmentSource{downloads: downloads}
}

func (source downloadAttachmentSource) OpenAuthorized(
	ctx context.Context,
	actor recordauth.ActorScope,
	attachmentID string,
) (AttachmentMaterial, error) {
	if source.downloads == nil {
		return AttachmentMaterial{}, ErrExportUnavailable
	}
	stream, err := source.downloads.Open(ctx, attachments.DownloadRequest{
		Actor: actor, AttachmentID: attachmentID,
	})
	if err != nil {
		return AttachmentMaterial{}, err
	}
	defer stream.Close(ctx)
	var body bytes.Buffer
	if _, err := stream.WriteTo(ctx, &body); err != nil {
		return AttachmentMaterial{}, err
	}
	return AttachmentMaterial{
		AttachmentID: attachmentID,
		DisplayName:  stream.Metadata().DisplayName,
		Bytes:        body.Bytes(),
	}, nil
}

type AttachmentMaterial struct {
	AttachmentID string
	DisplayName  string
	Bytes        []byte
}

type importedAttachmentPlan struct {
	SourceID       string
	TargetID       string
	RecordSourceID string
	DisplayName    string
	Payload        []byte
}

func (service *Service) evaluateExportAttachments(
	ctx context.Context,
	request PreviewRequest,
	recordID string,
	attachmentIDs []string,
	enforceArchiveLimits bool,
	currentEntries int,
	currentBytes int,
) (entries []ArchiveEntry, included []string, unavailable []UnavailableMaterial) {
	for _, attachmentID := range attachmentIDs {
		if service == nil || service.attachments == nil {
			unavailable = append(unavailable, UnavailableMaterial{Kind: "attachment", ID: attachmentID, Reason: "unavailable"})
			continue
		}
		material, err := service.attachments.OpenAuthorized(ctx, request.Actor, attachmentID)
		if err != nil || len(material.Bytes) == 0 {
			unavailable = append(unavailable, UnavailableMaterial{
				Kind: "attachment", ID: attachmentID, Reason: materialReason(err),
			})
			continue
		}
		if enforceArchiveLimits &&
			(uint64(len(material.Bytes)) > archiveV1MaxEntryBytes ||
				currentEntries+1 > archiveV1MaxEntries ||
				currentBytes+len(material.Bytes) > archiveV1MaxTotalBytes) {
			unavailable = append(unavailable, UnavailableMaterial{
				Kind: "attachment", ID: attachmentID, Reason: "over_archive_limit",
			})
			continue
		}
		included = append(included, "attachment:"+attachmentID)
		if enforceArchiveLimits {
			displayName := officialAttachmentDisplayName(material.DisplayName, material.Bytes)
			entries = append(entries, ArchiveEntry{
				Path:           "records/" + recordID + "/attachments/" + attachmentID + "/" + displayName,
				Classification: ArchiveClassAttachment,
				Payload:        append([]byte(nil), material.Bytes...),
			})
			currentEntries++
			currentBytes += len(material.Bytes)
		}
	}
	return entries, included, unavailable
}

func officialAttachmentDisplayName(preferred string, content []byte) string {
	displayName, _, err := officialAttachmentAdmission(preferred, content)
	if err != nil {
		return "attachment.bin"
	}
	return displayName
}

func officialAttachmentAdmission(preferred string, content []byte) (displayName, mediaType string, err error) {
	if len(content) == 0 || !utf8.ValidString(preferred) {
		return "", "", ErrUntrustedImportContent
	}
	mediaType = "text/plain; charset=utf-8"
	extension := ".txt"
	switch {
	case bytes.HasPrefix(content, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		mediaType = "image/png"
		extension = ".png"
	case bytes.HasPrefix(content, []byte{0xff, 0xd8, 0xff}):
		mediaType = "image/jpeg"
		extension = ".jpg"
	case bytes.HasPrefix(content, []byte("%PDF-")):
		mediaType = "application/pdf"
		extension = ".pdf"
	case !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0:
		return "", "", ErrUntrustedImportContent
	}
	stem := strings.TrimSpace(strings.TrimSuffix(path.Base(preferred), path.Ext(preferred)))
	if stem == "" || stem == "." || stem == ".." || strings.ContainsAny(stem, "/\\:") {
		stem = "attachment"
	}
	displayName = stem + extension
	if !utf8.ValidString(displayName) || len(displayName) > 255 {
		return "", "", ErrUntrustedImportContent
	}
	return displayName, mediaType, nil
}

func planImportedAttachment(planned *plannedArchive, entry ArchiveEntry, remaps map[string]store.ImportRemap) error {
	sourceID, displayName, err := importedAttachmentIdentity(entry)
	if err != nil {
		return err
	}
	targetID := sourceID
	if remaps != nil {
		remap, ok := remaps[sourceID]
		if !ok || remap.EntityKind != "attachment" {
			return ErrInvalidImportRequest
		}
		targetID = remap.TargetID
	} else {
		allocated, err := ids.New("att")
		if err != nil {
			return ErrInvalidImportRequest
		}
		targetID = allocated
	}
	planned.attachments = append(planned.attachments, importedAttachmentPlan{
		SourceID: sourceID, TargetID: targetID, RecordSourceID: recordSourceFromArchivePath(entry.Path),
		DisplayName: displayName, Payload: append([]byte(nil), entry.Payload...),
	})
	planned.remaps = append(planned.remaps, ImportRemap{EntityKind: "attachment", SourceID: sourceID, TargetID: targetID})
	return nil
}

func importedAttachmentIdentity(entry ArchiveEntry) (sourceID, displayName string, err error) {
	parts := strings.Split(entry.Path, "/")
	if len(parts) < 5 || parts[0] != "records" || parts[2] != "attachments" || !strings.HasPrefix(parts[3], "att_") {
		return "", "", ErrUntrustedImportContent
	}
	displayName, _, err = officialAttachmentAdmission(parts[len(parts)-1], entry.Payload)
	if err != nil {
		return "", "", err
	}
	return parts[3], displayName, nil
}

func (service *Service) restoreImportedAttachments(
	ctx context.Context,
	actor recordauth.ActorScope,
	cached cachedImportPlan,
) (map[string][]attachments.ImportedAvailableAttachment, map[string][]string, error) {
	if len(cached.attachments) == 0 {
		return map[string][]attachments.ImportedAvailableAttachment{}, map[string][]string{}, nil
	}
	if service == nil || service.attachmentBlobs == nil {
		return nil, nil, ErrExportUnavailable
	}
	recordBySource := map[string]string{}
	for _, document := range cached.documents {
		recordBySource[document.SourceID] = document.TargetID
	}
	imported := make(map[string][]attachments.ImportedAvailableAttachment)
	idsByRecord := make(map[string][]string)
	backend := attachments.BackendKind(service.backendKind)
	if backend != attachments.BackendKindLocal && backend != attachments.BackendKindS3 {
		backend = attachments.BackendKindLocal
	}
	for _, item := range cached.attachments {
		targetRecord := recordBySource[item.RecordSourceID]
		if targetRecord == "" && len(cached.documents) == 1 {
			targetRecord = cached.documents[0].TargetID
		}
		if targetRecord == "" {
			return nil, nil, ErrInvalidImportRequest
		}
		displayName, mediaType, err := officialAttachmentAdmission(item.DisplayName, item.Payload)
		if err != nil {
			return nil, nil, err
		}
		admitted, err := attachments.AdmitContent(ctx, attachments.AdmissionRequest{
			DisplayName:       displayName,
			DeclaredMediaType: mediaType,
			SizeBytes:         int64(len(item.Payload)),
			Content:           bytes.NewReader(item.Payload),
		}, attachments.DefaultAdmissionLimits(attachments.DefaultLimits()))
		if err != nil {
			return nil, nil, err
		}
		digest := sha256.Sum256(item.Payload)
		object, err := service.attachmentBlobs.Put(ctx, attachments.PutRequest{
			ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(item.Payload)),
		}, bytes.NewReader(item.Payload))
		if err != nil {
			return nil, nil, err
		}
		imported[targetRecord] = append(imported[targetRecord], attachments.ImportedAvailableAttachment{
			AttachmentID:     item.TargetID,
			DisplayName:      displayName,
			MediaType:        admitted.MediaType,
			LogicalSizeBytes: int64(len(item.Payload)),
			Object:           object,
			BackendKind:      backend,
			CreatedBy:        actor.UserID,
		})
		idsByRecord[targetRecord] = append(idsByRecord[targetRecord], item.TargetID)
	}
	return imported, idsByRecord, nil
}
