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
		displayName, _, err := admitOfficialAttachment(ctx, material.DisplayName, material.Bytes)
		if err != nil {
			unavailable = append(unavailable, UnavailableMaterial{
				Kind: "attachment", ID: attachmentID, Reason: "unsupported",
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

func admitOfficialAttachment(ctx context.Context, preferred string, content []byte) (displayName, mediaType string, err error) {
	displayName, mediaType, err = officialAttachmentAdmission(preferred, content)
	if err != nil {
		return "", "", err
	}
	admitted, err := attachments.AdmitContent(ctx, attachments.AdmissionRequest{
		DisplayName:       displayName,
		DeclaredMediaType: mediaType,
		SizeBytes:         int64(len(content)),
		Content:           bytes.NewReader(content),
	}, attachments.DefaultAdmissionLimits(attachments.DefaultLimits()))
	if err != nil {
		return "", "", err
	}
	return displayName, admitted.MediaType, nil
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
	case len(content) >= 12 && bytes.Equal(content[:4], []byte("RIFF")) && bytes.Equal(content[8:12], []byte("WEBP")):
		mediaType = "image/webp"
		extension = ".webp"
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
		remap, ok := remaps[importRemapKey("attachment", sourceID)]
		if !ok {
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
	displayName, _, err = admitOfficialAttachment(context.Background(), parts[len(parts)-1], entry.Payload)
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
	if service == nil || service.attachmentBlobs == nil || service.importBlobGC == nil {
		return nil, nil, ErrExportUnavailable
	}
	recordBySource := make(map[string]string, len(cached.documents))
	for _, document := range cached.documents {
		recordBySource[document.SourceID] = document.TargetID
	}
	type preparedAttachment struct {
		targetRecord string
		displayName  string
		mediaType    string
	}
	prepared := make([]preparedAttachment, len(cached.attachments))
	digests := make([][sha256.Size]byte, len(cached.attachments))
	for index, item := range cached.attachments {
		targetRecord := recordBySource[item.RecordSourceID]
		if targetRecord == "" && len(cached.documents) == 1 {
			targetRecord = cached.documents[0].TargetID
		}
		if targetRecord == "" {
			return nil, nil, ErrInvalidImportRequest
		}
		displayName, mediaType, err := admitOfficialAttachment(ctx, item.DisplayName, item.Payload)
		if err != nil {
			return nil, nil, err
		}
		prepared[index] = preparedAttachment{
			targetRecord: targetRecord, displayName: displayName, mediaType: mediaType,
		}
		digests[index] = sha256.Sum256(item.Payload)
	}
	// Observe every key before the first Put. Never refresh these baselines
	// after network I/O, even when an earlier GC lifecycle has completed.
	observations, err := service.importBlobGC.ObserveImportBlobGC(ctx, digests)
	if err != nil {
		return nil, nil, err
	}
	if len(observations) != len(cached.attachments) {
		return nil, nil, attachments.ErrInvalidAttachmentCommand
	}
	imported := make(map[string][]attachments.ImportedAvailableAttachment)
	idsByRecord := make(map[string][]string)
	backend := attachments.BackendKind(service.backendKind)
	if backend != attachments.BackendKindLocal && backend != attachments.BackendKindS3 {
		backend = attachments.BackendKindLocal
	}
	for index, item := range cached.attachments {
		temporaryKey, err := attachments.NewBlobTemporaryKey()
		if err != nil {
			return nil, nil, err
		}
		object, err := service.attachmentBlobs.Put(ctx, attachments.PutRequest{
			ExpectedSHA256:    digests[index],
			ExpectedSizeBytes: int64(len(item.Payload)),
			TemporaryKey:      temporaryKey,
		}, bytes.NewReader(item.Payload))
		if err != nil {
			return nil, nil, err
		}
		admitted := prepared[index]
		available := attachments.ImportedAvailableAttachment{
			AttachmentID:     item.TargetID,
			DisplayName:      admitted.displayName,
			MediaType:        admitted.mediaType,
			LogicalSizeBytes: int64(len(item.Payload)),
			Object:           object,
			BackendKind:      backend,
			CreatedBy:        actor.UserID,
			GCObservation:    observations[index],
		}
		if err := available.Validate(); err != nil {
			return nil, nil, err
		}
		imported[admitted.targetRecord] = append(imported[admitted.targetRecord], available)
		idsByRecord[admitted.targetRecord] = append(idsByRecord[admitted.targetRecord], item.TargetID)
	}
	return imported, idsByRecord, nil
}
