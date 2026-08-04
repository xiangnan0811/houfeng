package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
)

func TestPostgresIntegrationAttachmentConcurrentReservationsRespectEffectiveRecordQuota(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_attquota", "usr_attquota")
	limits := attachments.Limits{
		MaxFileBytes:              1024,
		MaxRecordBytes:            1536,
		MaxProjectBytes:           4096,
		WarningPercent:            80,
		MaxInlineTextPreviewBytes: 512,
	}

	type outcome struct {
		result attachments.UploadReservationResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	start := make(chan struct{})
	for index := 0; index < 2; index++ {
		repository := NewPostgresAttachmentRepository(
			fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("attachment-reserve-%d", index), 1),
		)
		command := attachments.ReserveUploadCommand{
			ProjectID:         "default",
			UploadID:          fmt.Sprintf("aup_quota%d", index),
			AttachmentID:      fmt.Sprintf("att_quota%d", index),
			DraftID:           "rdf_attquota",
			AuthorID:          "usr_attquota",
			DisplayName:       fmt.Sprintf("quota-%d.txt", index),
			MediaType:         "text/plain",
			TransportKind:     attachments.TransportKindLocal,
			DeclaredSizeBytes: 1024,
			ExpiresAt:         time.Now().UTC().Add(time.Hour),
			Limits:            limits,
		}
		go func() {
			<-start
			result, err := repository.ReserveUpload(context.Background(), command)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)

	var winnerCount, quotaCount int
	for range 2 {
		result := waitForRecordPlatformResult(t, outcomes, "concurrent attachment reservation")
		switch {
		case result.err == nil:
			winnerCount++
		case errors.Is(result.err, attachments.ErrQuotaExceeded):
			quotaCount++
		default:
			t.Fatalf("concurrent ReserveUpload() = (%#v, %v)", result.result, result.err)
		}
	}
	if winnerCount != 1 || quotaCount != 1 {
		t.Fatalf("concurrent reservation outcomes winners/quota = %d/%d, want 1/1", winnerCount, quotaCount)
	}

	var attachmentsCount, uploadsCount int
	var logicalBytes, reservedBytes, physicalBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*)::int from public.record_attachments where draft_id = 'rdf_attquota'),
		  (select count(*)::int from public.attachment_uploads where origin_draft_id = 'rdf_attquota'),
		  logical_bytes, reserved_bytes, physical_bytes
		from public.attachment_quota_accounts
		where project_id = 'default'
	`).Scan(&attachmentsCount, &uploadsCount, &logicalBytes, &reservedBytes, &physicalBytes); err != nil {
		t.Fatalf("read concurrent attachment reservation result: %v", err)
	}
	if attachmentsCount != 1 || uploadsCount != 1 || logicalBytes != 0 || reservedBytes != 1024 || physicalBytes != 0 {
		t.Fatalf("concurrent reservation rows = attachments/uploads %d/%d quota %d/%d/%d", attachmentsCount, uploadsCount, logicalBytes, reservedBytes, physicalBytes)
	}
}

func TestPostgresIntegrationAttachmentQuotaLifecycleAndPhysicalDedupe(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedAttachmentDraft(t, ctx, fixture, "rdf_attlifecycle", "usr_attlifecycle")
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-quota-lifecycle", 2),
	)

	var digest [32]byte
	for index := range digest {
		digest[index] = 0xbb
	}
	var completionFingerprint [32]byte
	for index := range completionFingerprint {
		completionFingerprint[index] = 0xcc
	}
	blob := attachments.BlobObject{
		Key:           "sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SHA256:        digest,
		ObjectVersion: "local-v1",
		SizeBytes:     9,
		BackendKind:   attachments.BackendKindLocal,
	}
	limits := attachments.Limits{
		MaxFileBytes:              10,
		MaxRecordBytes:            30,
		MaxProjectBytes:           30,
		WarningPercent:            60,
		MaxInlineTextPreviewBytes: 5,
	}

	for index := 0; index < 2; index++ {
		command := attachmentLifecycleReservationCommand(index)
		command.Limits = limits
		if _, err := repository.ReserveUpload(ctx, command); err != nil {
			t.Fatalf("ReserveUpload(%d) error = %v", index, err)
		}
		if _, err := repository.StartUpload(ctx, attachments.UploadMutationCommand{
			ProjectID: "default", UploadID: command.UploadID, AuthorID: command.AuthorID,
		}); err != nil {
			t.Fatalf("StartUpload(%d) error = %v", index, err)
		}
		completeCommand := attachments.CompleteUploadContentCommand{
			ProjectID:              "default",
			UploadID:               command.UploadID,
			AuthorID:               command.AuthorID,
			ActualSizeBytes:        9,
			ActualSHA256:           digest,
			TemporaryObjectKey:     fmt.Sprintf("tmp/%d", index),
			TemporaryObjectVersion: "tmp-v1",
			CompletionFingerprint:  completionFingerprint,
		}
		completed, err := repository.CompleteUploadContent(ctx, completeCommand)
		if err != nil {
			t.Fatalf("CompleteUploadContent(%d) error = %v", index, err)
		}
		completedReplay, err := repository.CompleteUploadContent(ctx, completeCommand)
		if err != nil || completedReplay != completed {
			t.Fatalf("CompleteUploadContent(%d) replay = (%#v, %v), want %#v", index, completedReplay, err, completed)
		}
		admitCommand := attachments.AdmitUploadCommand{
			ProjectID: "default", UploadID: command.UploadID, AuthorID: command.AuthorID, Blob: blob, Limits: limits,
		}
		admitted, err := repository.AdmitUpload(ctx, admitCommand)
		if err != nil {
			t.Fatalf("AdmitUpload(%d) error = %v", index, err)
		}
		admittedReplay, err := repository.AdmitUpload(ctx, admitCommand)
		if err != nil || admittedReplay != admitted {
			t.Fatalf("AdmitUpload(%d) replay = (%#v, %v), want %#v", index, admittedReplay, err, admitted)
		}
		if index == 1 && !admitted.Quota.ProjectWarning {
			t.Fatalf("AdmitUpload(%d) custom 60%% warning = %#v", index, admitted.Quota)
		}
	}

	rejected := attachmentLifecycleReservationCommand(2)
	rejected.Limits = limits
	if _, err := repository.ReserveUpload(ctx, rejected); err != nil {
		t.Fatalf("ReserveUpload(rejected) error = %v", err)
	}
	if _, err := repository.StartUpload(ctx, attachments.UploadMutationCommand{
		ProjectID: "default", UploadID: rejected.UploadID, AuthorID: rejected.AuthorID,
	}); err != nil {
		t.Fatalf("StartUpload(rejected) error = %v", err)
	}
	rejectCommand := attachments.FailUploadCommand{
		ProjectID: "default", UploadID: rejected.UploadID, AuthorID: rejected.AuthorID,
		TargetState: attachments.UploadStateRejected, Limits: limits,
	}
	rejectedResult, err := repository.FailUpload(ctx, rejectCommand)
	if err != nil {
		t.Fatalf("FailUpload(rejected) error = %v", err)
	}
	rejectedReplay, err := repository.FailUpload(ctx, rejectCommand)
	if err != nil || rejectedReplay != rejectedResult {
		t.Fatalf("FailUpload(rejected) replay = (%#v, %v), want %#v", rejectedReplay, err, rejectedResult)
	}

	expired := attachmentLifecycleReservationCommand(3)
	expired.Limits = limits
	if _, err := repository.ReserveUpload(ctx, expired); err != nil {
		t.Fatalf("ReserveUpload(expired) error = %v", err)
	}
	expireCommand := attachments.FailUploadCommand{
		ProjectID: "default", UploadID: expired.UploadID, AuthorID: expired.AuthorID,
		TargetState: attachments.UploadStateExpired, Limits: limits,
	}
	expiredResult, err := repository.FailUpload(ctx, expireCommand)
	if err != nil {
		t.Fatalf("FailUpload(expired) error = %v", err)
	}
	expiredReplay, err := repository.FailUpload(ctx, expireCommand)
	if err != nil || expiredReplay != expiredResult {
		t.Fatalf("FailUpload(expired) replay = (%#v, %v), want %#v", expiredReplay, err, expiredResult)
	}

	var logicalBytes, reservedBytes, physicalBytes int64
	var availableCount, rejectedCount, expiredCount, blobCount int
	if err := fixture.db.QueryRow(ctx, `
		select quota.logical_bytes, quota.reserved_bytes, quota.physical_bytes,
		  (select count(*)::int from public.attachment_uploads where upload_state = 'available'),
		  (select count(*)::int from public.attachment_uploads where upload_state = 'rejected'),
		  (select count(*)::int from public.attachment_uploads where upload_state = 'expired'),
		  (select count(*)::int from public.blob_objects)
		from public.attachment_quota_accounts quota
		where quota.project_id = 'default'
	`).Scan(&logicalBytes, &reservedBytes, &physicalBytes, &availableCount, &rejectedCount, &expiredCount, &blobCount); err != nil {
		t.Fatalf("read attachment quota lifecycle: %v", err)
	}
	if logicalBytes != 18 || reservedBytes != 0 || physicalBytes != 9 ||
		availableCount != 2 || rejectedCount != 1 || expiredCount != 1 || blobCount != 1 {
		t.Fatalf("attachment quota lifecycle = quota %d/%d/%d states %d/%d/%d blobs %d", logicalBytes, reservedBytes, physicalBytes, availableCount, rejectedCount, expiredCount, blobCount)
	}
	quotaSnapshot, err := repository.GetProjectQuotaSnapshot(ctx, attachments.ProjectQuotaSnapshotCommand{
		ProjectID: "default", Limits: limits,
	})
	if err != nil {
		t.Fatalf("GetProjectQuotaSnapshot() error = %v", err)
	}
	if quotaSnapshot.Usage != (attachments.QuotaUsage{LogicalBytes: 18, PhysicalBytes: 9}) || !quotaSnapshot.ProjectWarning {
		t.Fatalf("GetProjectQuotaSnapshot() = %#v", quotaSnapshot)
	}
}

func TestPostgresIntegrationAttachmentExistingRecordDraftUsesEffectiveRecordQuota(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-effective-record", 2),
	)
	limits := attachments.Limits{
		MaxFileBytes:              10,
		MaxRecordBytes:            10,
		MaxProjectBytes:           100,
		WarningPercent:            80,
		MaxInlineTextPreviewBytes: 5,
	}

	if _, err := fixture.db.Exec(ctx, `
		insert into public.records (record_id) values ('rec_effective1');
		insert into public.record_revisions (
			revision_id, record_id, revision_no, title, body_markdown,
			markdown_dialect_version, record_type, impact_level, visibility_scope,
			visibility_digest, author_id, canonical_hash
		) values (
			'rrv_effective1', 'rec_effective1', 1, 'Effective', '', 1, 'note',
			'informational', '{}'::jsonb, decode(repeat('51', 32), 'hex'),
			'usr_effective1', decode(repeat('52', 32), 'hex')
		);
		insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values (
			'rrv_effective1', 0, 1, 'vps', 'affected', 'vps_effective1', true,
			'{}'::jsonb, '{}'::jsonb, decode(repeat('53', 32), 'hex')
		);
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values (
			'sha256/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
			decode(repeat('dd', 32), 'hex'), 'local-existing-v1', 5, 'local'
		);
		insert into public.record_attachments (
			attachment_id, record_id, attachment_state, display_name, media_type,
			logical_size_bytes, blob_key, blob_object_version, created_by
		) values (
			'att_effectiveold', 'rec_effective1', 'available', 'old.txt', 'text/plain', 5,
			'sha256/dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd',
			'local-existing-v1', 'usr_effective1'
		);
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, physical_bytes
		) values ('default', 5, 5);
		insert into public.record_drafts (
			draft_id, record_id, base_revision_id, author_id, payload, payload_hash,
			draft_version, etag_digest, warning_at, expires_at
		) values (
			'rdf_effective1', 'rec_effective1', 'rrv_effective1', 'usr_effective1',
			'{}'::jsonb, decode(repeat('54', 32), 'hex'), 1,
			decode(repeat('55', 32), 'hex'), now() + interval '1 day', now() + interval '2 days'
		)
	`); err != nil {
		t.Fatalf("seed existing-record attachment quota: %v", err)
	}

	reserve := attachments.ReserveUploadCommand{
		ProjectID: "default", UploadID: "aup_effective1", AttachmentID: "att_effectivenew",
		DraftID: "rdf_effective1", AuthorID: "usr_effective1", DisplayName: "new.txt",
		MediaType: "text/plain", TransportKind: attachments.TransportKindLocal,
		DeclaredSizeBytes: 4, ExpiresAt: time.Now().UTC().Add(time.Hour), Limits: limits,
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload(existing record) error = %v", err)
	}
	if _, err := repository.StartUpload(ctx, attachments.UploadMutationCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
	}); err != nil {
		t.Fatalf("StartUpload(existing record) error = %v", err)
	}
	var digest [32]byte
	for index := range digest {
		digest[index] = 0xee
	}
	if _, err := repository.CompleteUploadContent(ctx, attachments.CompleteUploadContentCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		ActualSizeBytes: 4, ActualSHA256: digest, TemporaryObjectKey: "tmp/effective1",
		TemporaryObjectVersion: "tmp-v1", CompletionFingerprint: [32]byte{1},
	}); err != nil {
		t.Fatalf("CompleteUploadContent(existing record) error = %v", err)
	}
	admitted, err := repository.AdmitUpload(ctx, attachments.AdmitUploadCommand{
		ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
		Blob: attachments.BlobObject{
			Key:    "sha256/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			SHA256: digest, ObjectVersion: "local-new-v1", SizeBytes: 4, BackendKind: attachments.BackendKindLocal,
		},
		Limits: limits,
	})
	if err != nil {
		t.Fatalf("AdmitUpload(existing record) error = %v", err)
	}
	if admitted.Quota.EffectiveRecordBytes != 9 {
		t.Fatalf("AdmitUpload(existing record) effective bytes = %d, want 9", admitted.Quota.EffectiveRecordBytes)
	}

	reserve.UploadID = "aup_effective2"
	reserve.AttachmentID = "att_effective2"
	reserve.DeclaredSizeBytes = 2
	if _, err := repository.ReserveUpload(ctx, reserve); !errors.Is(err, attachments.ErrQuotaExceeded) {
		t.Fatalf("ReserveUpload(over effective record quota) error = %v, want ErrQuotaExceeded", err)
	}
}

func TestPostgresIntegrationAttachmentCrossRecordCopyRetainsBlobAndChargesLogicalQuota(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-cross-record-copy", 2),
	)

	if _, err := fixture.db.Exec(ctx, `
		insert into public.records (record_id) values ('rec_copysource'), ('rec_copytarget');
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values (
			'sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
			decode(repeat('bb', 32), 'hex'), 'local-v1', 9, 'local'
		);
		insert into public.record_attachments (
			attachment_id, record_id, attachment_state, display_name, media_type,
			logical_size_bytes, blob_key, blob_object_version, created_by
		) values (
			'att_copysource', 'rec_copysource', 'available', 'source.txt', 'text/plain',
			9, 'sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
			'local-v1', 'usr_copysource'
		);
		insert into public.attachment_quota_accounts (
			project_id, logical_bytes, physical_bytes
		) values ('default', 9, 9)
	`); err != nil {
		t.Fatalf("seed cross-record attachment copy: %v", err)
	}

	result, err := repository.CopyAttachment(ctx, attachments.CopyAttachmentCommand{
		ProjectID:          "default",
		SourceRecordID:     "rec_copysource",
		TargetRecordID:     "rec_copytarget",
		SourceAttachmentID: "att_copysource",
		TargetAttachmentID: "att_copytarget",
		ActorID:            "usr_copyactor",
		Limits:             attachments.DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("CopyAttachment() error = %v", err)
	}
	if result.Quota.Usage != (attachments.QuotaUsage{LogicalBytes: 18, PhysicalBytes: 9}) ||
		result.Quota.EffectiveRecordBytes != 9 {
		t.Fatalf("CopyAttachment() quota = %#v", result.Quota)
	}

	var recordID, copiedFromID, blobKey, blobVersion string
	var logicalBytes, projectLogicalBytes, projectPhysicalBytes int64
	if err := fixture.db.QueryRow(ctx, `
		select attachment.record_id, attachment.copied_from_attachment_id,
		       attachment.blob_key, attachment.blob_object_version,
		       attachment.logical_size_bytes, quota.logical_bytes, quota.physical_bytes
		from public.record_attachments attachment
		join public.attachment_quota_accounts quota on quota.project_id = attachment.project_id
		where attachment.attachment_id = 'att_copytarget'
	`).Scan(
		&recordID, &copiedFromID, &blobKey, &blobVersion,
		&logicalBytes, &projectLogicalBytes, &projectPhysicalBytes,
	); err != nil {
		t.Fatalf("read cross-record attachment copy: %v", err)
	}
	if recordID != "rec_copytarget" || copiedFromID != "att_copysource" ||
		blobKey != "sha256/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		blobVersion != "local-v1" || logicalBytes != 9 || projectLogicalBytes != 18 || projectPhysicalBytes != 9 {
		t.Fatalf("cross-record copy = %s/%s %s@%s logical=%d project=%d/%d",
			recordID, copiedFromID, blobKey, blobVersion, logicalBytes, projectLogicalBytes, projectPhysicalBytes)
	}

	if _, err := fixture.db.Exec(ctx, `delete from public.record_attachments where attachment_id = 'att_copysource'`); err != nil {
		t.Fatalf("delete copied attachment source: %v", err)
	}
	var survivingLineage string
	if err := fixture.db.QueryRow(ctx, `
		select copied_from_attachment_id from public.record_attachments
		where attachment_id = 'att_copytarget'
	`).Scan(&survivingLineage); err != nil {
		t.Fatalf("read detached copied attachment lineage: %v", err)
	}
	if survivingLineage != "att_copysource" {
		t.Fatalf("detached copied attachment lineage = %q", survivingLineage)
	}
}

func TestPostgresIntegrationAttachmentBlobPinAndReferenceProtection(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repository := NewPostgresAttachmentRepository(
		fixture.openDirectRuntimePool(t, ctx, "attachment-blob-protection", 2),
	)
	blobKey := "sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	blobVersion := "local-protected-v1"

	if _, err := fixture.db.Exec(ctx, `
		insert into public.records (record_id) values ('rec_protecta'), ('rec_protectb');
		insert into public.record_revisions (
			revision_id, record_id, revision_no, title, body_markdown,
			markdown_dialect_version, record_type, impact_level, visibility_scope,
			visibility_digest, author_id, canonical_hash
		) values (
			'rrv_protecta', 'rec_protecta', 1, 'Protected', '', 1, 'note',
			'informational', '{}'::jsonb, decode(repeat('41', 32), 'hex'),
			'usr_protecta', decode(repeat('42', 32), 'hex')
		);
		insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values (
			'rrv_protecta', 0, 1, 'vps', 'affected', 'vps_protecta', true,
			'{}'::jsonb, '{}'::jsonb, decode(repeat('43', 32), 'hex')
		);
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values (
			'sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
			decode(repeat('cc', 32), 'hex'), 'local-protected-v1', 11, 'local'
		);
		insert into public.record_attachments (
			attachment_id, record_id, copied_from_attachment_id, attachment_state,
			display_name, media_type, logical_size_bytes, blob_key,
			blob_object_version, created_by
		) values
			('att_protecta', 'rec_protecta', null, 'available', 'a.txt', 'text/plain', 11,
			 'sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
			 'local-protected-v1', 'usr_protecta'),
			('att_protectb', 'rec_protectb', 'att_protecta', 'available', 'b.txt', 'text/plain', 11,
			 'sha256/cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',
			 'local-protected-v1', 'usr_protectb');
		insert into public.record_revision_attachments (
			record_id, revision_id, ordinal, attachment_id
		) values ('rec_protecta', 'rrv_protecta', 0, 'att_protecta')
	`); err != nil {
		t.Fatalf("seed Blob protection references: %v", err)
	}

	create := attachments.CreateBlobGCPinCommand{
		PinID:             "bgp_protected1",
		OwnerKind:         attachments.BlobGCPinOwnerBackupManifest,
		OwnerID:           "backup_protected1",
		BlobKey:           blobKey,
		BlobObjectVersion: blobVersion,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
	}
	protection, err := repository.CreateBlobGCPin(ctx, create)
	if err != nil {
		t.Fatalf("CreateBlobGCPin() error = %v", err)
	}
	if protection.BlobKey != blobKey || protection.BlobObjectVersion != blobVersion ||
		protection.LogicalAttachmentCount != 2 || protection.RevisionReferenceCount != 1 ||
		protection.ActivePinCount != 1 || !protection.Protected {
		t.Fatalf("CreateBlobGCPin() protection = %#v", protection)
	}

	protection, err = repository.ReleaseBlobGCPin(ctx, attachments.ReleaseBlobGCPinCommand{
		PinID:             create.PinID,
		OwnerKind:         create.OwnerKind,
		OwnerID:           create.OwnerID,
		BlobKey:           create.BlobKey,
		BlobObjectVersion: create.BlobObjectVersion,
	})
	if err != nil {
		t.Fatalf("ReleaseBlobGCPin() error = %v", err)
	}
	if protection.ActivePinCount != 0 || !protection.Protected ||
		protection.LogicalAttachmentCount != 2 || protection.RevisionReferenceCount != 1 {
		t.Fatalf("ReleaseBlobGCPin() referenced protection = %#v", protection)
	}

	if _, err := fixture.db.Exec(ctx, `
		delete from public.record_revision_attachments where revision_id = 'rrv_protecta';
		delete from public.record_attachments where attachment_id in ('att_protecta', 'att_protectb')
	`); err != nil {
		t.Fatalf("remove Blob protection references: %v", err)
	}
	protection, err = repository.GetBlobProtection(ctx, attachments.BlobProtectionCommand{
		BlobKey: blobKey, BlobObjectVersion: blobVersion,
	})
	if err != nil {
		t.Fatalf("GetBlobProtection() error = %v", err)
	}
	if protection.Protected || protection.LogicalAttachmentCount != 0 ||
		protection.RevisionReferenceCount != 0 || protection.ActivePinCount != 0 {
		t.Fatalf("GetBlobProtection() unprotected = %#v", protection)
	}
}

func seedAttachmentDraft(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, draftID, authorID string) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_drafts (
			draft_id, author_id, payload, payload_hash, draft_version,
			etag_digest, warning_at, expires_at
		) values (
			$1, $2, '{}'::jsonb, decode(repeat('31', 32), 'hex'), 1,
			decode(repeat('32', 32), 'hex'), now() + interval '1 day', now() + interval '2 days'
		)
	`, draftID, authorID); err != nil {
		t.Fatalf("seed attachment draft %q: %v", draftID, err)
	}
}

func attachmentLifecycleReservationCommand(index int) attachments.ReserveUploadCommand {
	return attachments.ReserveUploadCommand{
		ProjectID:         "default",
		UploadID:          fmt.Sprintf("aup_lifecycle%d", index),
		AttachmentID:      fmt.Sprintf("att_lifecycle%d", index),
		DraftID:           "rdf_attlifecycle",
		AuthorID:          "usr_attlifecycle",
		DisplayName:       fmt.Sprintf("lifecycle-%d.txt", index),
		MediaType:         "text/plain",
		TransportKind:     attachments.TransportKindLocal,
		DeclaredSizeBytes: 9,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		Limits:            attachments.DefaultLimits(),
	}
}
