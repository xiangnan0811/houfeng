package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

type draftReleaseUploadState string

const (
	draftReleaseCreated     draftReleaseUploadState = "created"
	draftReleaseUploading   draftReleaseUploadState = "uploading"
	draftReleaseQuarantined draftReleaseUploadState = "quarantined"
	draftReleaseAvailable   draftReleaseUploadState = "available"
	draftReleaseRejected    draftReleaseUploadState = "rejected"
	draftReleaseExpired     draftReleaseUploadState = "expired"
)

const draftReleaseSize = int64(9)

func TestPostgresIntegrationRecordDraftDiscardReleasesOwnedAttachmentsInEveryState(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-release", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)

	draft := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleaseall", recordsPostgresDraftPayload(t, "Release"))
	states := []draftReleaseUploadState{
		draftReleaseCreated, draftReleaseUploading, draftReleaseQuarantined,
		draftReleaseAvailable, draftReleaseRejected, draftReleaseExpired,
	}
	for index, state := range states {
		seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, index, state)
	}
	// 未完成的三项占预留，可用项计逻辑与物理字节，已拒绝/已过期的早已退回。
	assertDraftReleaseQuota(t, ctx, fixture, draftReleaseSize, 3*draftReleaseSize, draftReleaseSize)

	if err := draftRepository.DeleteDraft(ctx, records.DraftDeleteCommand{
		DraftID: draft.DraftID, AuthorID: draft.AuthorID, Reason: records.DraftDeleteDiscarded,
	}); err != nil {
		t.Fatalf("DeleteDraft(discard with attachments) error = %v", err)
	}

	assertDraftReleaseRowsGone(t, ctx, fixture, draft.DraftID)
	// 隔离中那份的分片对象登记为 Blob，物理字节随之计入，留给 GC 回收；可用附件的原件早已登记。
	assertDraftReleaseQuota(t, ctx, fixture, 0, 0, 2*draftReleaseSize)
	quarantinedKey := draftReleaseObject(draft.DraftID, 2).Key
	var registered int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.blob_objects where blob_key = $1`, quarantinedKey).Scan(&registered); err != nil {
		t.Fatalf("read registered part Blob: %v", err)
	}
	if registered != 1 {
		t.Fatalf("registered part Blob rows = %d, want 1", registered)
	}
	assertRecordsPostgresDraftFormalSideEffects(t, ctx, fixture.db, 0, 0)
}

func TestPostgresIntegrationRecordDraftDiscardWaitsForLiveAttachmentProcessing(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-busy", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)

	draft := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleasebusy", recordsPostgresDraftPayload(t, "Busy"))
	seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, 0, draftReleaseQuarantined)
	claim, err := attachmentRepository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_release_busy", OwnerLeaseDuration: 5 * time.Minute,
	})
	if err != nil || claim == nil {
		t.Fatalf("ClaimProcessorJob() = (%#v, %v)", claim, err)
	}

	err = draftRepository.DeleteDraft(ctx, records.DraftDeleteCommand{
		DraftID: draft.DraftID, AuthorID: draft.AuthorID, Reason: records.DraftDeleteDiscarded,
	})
	if !errors.Is(err, records.ErrDraftAttachmentsBusy) {
		t.Fatalf("DeleteDraft(live claim) error = %v, want ErrDraftAttachmentsBusy", err)
	}
	var drafts, attachmentRows, jobs int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_drafts where draft_id = $1),
		       (select count(*)::int from public.record_attachments where draft_id = $1),
		       (select count(*)::int from public.attachment_processor_jobs where processor_state = 'claimed')`,
		draft.DraftID).Scan(&drafts, &attachmentRows, &jobs); err != nil {
		t.Fatalf("read busy discard rows: %v", err)
	}
	if drafts != 1 || attachmentRows != 1 || jobs != 1 {
		t.Fatalf("busy discard rows = drafts %d attachments %d claimed jobs %d, want 1/1/1", drafts, attachmentRows, jobs)
	}
	assertDraftReleaseQuota(t, ctx, fixture, 0, draftReleaseSize, 0)

	// 租约过期且没有未清工作区时不再算忙，可以释放。
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_processor_jobs
		set lease_expires_at = transaction_timestamp() - interval '1 second'
		where processor_job_id = $1`, claim.ProcessorJobID); err != nil {
		t.Fatalf("expire processor lease: %v", err)
	}
	if err := draftRepository.DeleteDraft(ctx, records.DraftDeleteCommand{
		DraftID: draft.DraftID, AuthorID: draft.AuthorID, Reason: records.DraftDeleteDiscarded,
	}); err != nil {
		t.Fatalf("DeleteDraft(expired lease) error = %v", err)
	}
	assertDraftReleaseRowsGone(t, ctx, fixture, draft.DraftID)
	assertDraftReleaseQuota(t, ctx, fixture, 0, 0, draftReleaseSize)
}

func TestPostgresIntegrationRecordDraftDiscardWaitsForUnfinishedPublication(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-publication", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)

	draft := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleaseintent", recordsPostgresDraftPayload(t, "Intent"))
	seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, 0, draftReleaseUploading)
	object := draftReleaseObject(draft.DraftID, 0)
	// 本地 PUT 已登记写入意图、尚未记录分片：此时删掉上传行，intent 过期清理可能误删去重共用的对象。
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_publication_intents (
			publication_id, owner_kind, owner_id, owner_generation, blob_key, sha256_digest,
			size_bytes, backend_kind, publication_state, publish_expires_at
		) values ('bpi_pgreleaseintent', 'upload', $1, 1, $2, $3, $4, 'local', 'prepared',
			transaction_timestamp() + interval '15 minutes')`,
		fmt.Sprintf("aup_%s0", draft.DraftID[len("rdf_"):]), object.Key, object.SHA256[:], object.SizeBytes); err != nil {
		t.Fatalf("seed prepared publication intent: %v", err)
	}
	discard := records.DraftDeleteCommand{DraftID: draft.DraftID, AuthorID: draft.AuthorID, Reason: records.DraftDeleteDiscarded}
	if err := draftRepository.DeleteDraft(ctx, discard); !errors.Is(err, records.ErrDraftAttachmentsBusy) {
		t.Fatalf("DeleteDraft(unfinished publication) error = %v, want ErrDraftAttachmentsBusy", err)
	}
	assertDraftReleaseQuota(t, ctx, fixture, 0, draftReleaseSize, 0)

	if _, err := fixture.db.Exec(ctx, `
		update public.blob_publication_intents
		set publication_state = 'completed', completion_outcome = 'already_absent',
		    cleanup_owner_id = 'publication_reconciler', cleanup_generation = 1, attempt = 1,
		    cleanup_lease_expires_at = transaction_timestamp(),
		    receipt_digest = decode(repeat('ef', 32), 'hex'), completed_at = transaction_timestamp()
		where publication_id = 'bpi_pgreleaseintent'`); err != nil {
		t.Fatalf("complete publication intent: %v", err)
	}
	if err := draftRepository.DeleteDraft(ctx, discard); err != nil {
		t.Fatalf("DeleteDraft(after publication completed) error = %v", err)
	}
	assertDraftReleaseRowsGone(t, ctx, fixture, draft.DraftID)
	assertDraftReleaseQuota(t, ctx, fixture, 0, 0, 0)
}

func TestPostgresIntegrationRecordDraftReleaseFencesLatePublicationIntent(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-late-intent", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)

	draft := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleaselate", recordsPostgresDraftPayload(t, "Late"))
	seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, 0, draftReleaseCreated)
	uploadID := fmt.Sprintf("aup_%s0", draft.DraftID[len("rdf_"):])
	// PUT 已通过 PrepareUpload、草稿锁已释放，尚未登记写入意图。
	if _, err := attachmentRepository.PrepareUpload(ctx, attachments.PrepareUploadCommand{
		ProjectID: "default", UploadID: uploadID, AuthorID: recordsPostgresDraftAuthorID,
	}); err != nil {
		t.Fatalf("PrepareUpload() error = %v", err)
	}
	if err := draftRepository.DeleteDraft(ctx, records.DraftDeleteCommand{
		DraftID: draft.DraftID, AuthorID: draft.AuthorID, Reason: records.DraftDeleteDiscarded,
	}); err != nil {
		t.Fatalf("DeleteDraft() error = %v", err)
	}

	object := draftReleaseObject(draft.DraftID, 0)
	_, err := attachmentRepository.PrepareBlobPublication(ctx, attachments.BlobPublicationPrepareRequest{
		ProjectID: "default", OwnerKind: attachments.BlobPublicationOwnerUpload, OwnerID: uploadID, OwnerGeneration: 1,
		Target: attachments.BlobPublicationTarget{
			Key: object.Key, SHA256: object.SHA256, SizeBytes: object.SizeBytes, BackendKind: attachments.BackendKindLocal,
		},
		PublishExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	// 所有者已随草稿释放：迟到的写入意图必须被拒绝，不能留下没有所有者、过期后会误删共用对象的意图。
	if !errors.Is(err, attachments.ErrAttachmentOwnerNotFound) {
		t.Fatalf("PrepareBlobPublication(after release) error = %v, want ErrAttachmentOwnerNotFound", err)
	}
	var intents int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.blob_publication_intents where owner_id = $1`, uploadID).Scan(&intents); err != nil {
		t.Fatalf("count late publication intents: %v", err)
	}
	if intents != 0 {
		t.Fatalf("late publication intents = %d, want 0", intents)
	}
}

func TestPostgresIntegrationBlobPublicationRefusesNonWritableOwnerWithoutWaitingForTableLocks(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-owner-state", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)

	draft := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleasestate", recordsPostgresDraftPayload(t, "State"))
	seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, 0, draftReleaseExpired)
	uploadID := fmt.Sprintf("aup_%s0", draft.DraftID[len("rdf_"):])

	// 模拟永久删除的第一步：先持有 blob_objects 表锁，随后才会删除终态上传行。
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin table lock holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `lock table public.blob_objects in share row exclusive mode`); err != nil {
		t.Fatalf("hold Blob table lock: %v", err)
	}

	// 已过期的上传不可再写入：必须立即拒绝，不能拿着上传行锁去等表锁（否则与永久删除成环）。
	deadline, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	object := draftReleaseObject(draft.DraftID, 0)
	_, err = attachmentRepository.PrepareBlobPublication(deadline, attachments.BlobPublicationPrepareRequest{
		ProjectID: "default", OwnerKind: attachments.BlobPublicationOwnerUpload, OwnerID: uploadID, OwnerGeneration: 1,
		Target: attachments.BlobPublicationTarget{
			Key: object.Key, SHA256: object.SHA256, SizeBytes: object.SizeBytes, BackendKind: attachments.BackendKindLocal,
		},
		PublishExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if !errors.Is(err, attachments.ErrUploadExpired) {
		t.Fatalf("PrepareBlobPublication(expired owner, table lock held) error = %v, want immediate ErrUploadExpired", err)
	}
}

func TestPostgresIntegrationRecordDraftPublishReleasesUnreferencedAttachments(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-publish", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)
	repository := newRecordsPostgresRepository(t, runtimePool, NewRecordAttachmentRevisionParticipant())

	draft := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleasepublish", recordsPostgresDraftPayload(t, "Publish"))
	kept := seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, 0, draftReleaseAvailable)
	dropped := seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, 1, draftReleaseAvailable)
	pending := seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, 2, draftReleaseUploading)
	assertDraftReleaseQuota(t, ctx, fixture, 2*draftReleaseSize, draftReleaseSize, 2*draftReleaseSize)

	command := recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_pgreleasepublish", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Publish with one attachment", kept), "records-release-publish",
	)
	command.DraftID = draft.DraftID
	command.DraftETag = draft.ETag
	committed, err := repository.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("CommitRevision(publish with unreferenced attachments) error = %v", err)
	}

	var keptRecord *string
	var droppedRows, pendingRows int
	if err := fixture.db.QueryRow(ctx, `
		select (select record_id from public.record_attachments where attachment_id = $1),
		       (select count(*)::int from public.record_attachments where attachment_id = $2),
		       (select count(*)::int from public.record_attachments where attachment_id = $3)`,
		kept, dropped, pending).Scan(&keptRecord, &droppedRows, &pendingRows); err != nil {
		t.Fatalf("read published attachment ownership: %v", err)
	}
	if keptRecord == nil || *keptRecord != committed.RecordID || droppedRows != 0 || pendingRows != 0 {
		t.Fatalf("published ownership = kept %v dropped %d pending %d", keptRecord, droppedRows, pendingRows)
	}
	assertDraftReleaseRowsGone(t, ctx, fixture, draft.DraftID)
	// 只剩被引用那份的逻辑字节；两份原件的物理字节等 GC 回收。
	assertDraftReleaseQuota(t, ctx, fixture, draftReleaseSize, 0, 2*draftReleaseSize)
}

func TestPostgresIntegrationRecordDraftExpiryReleasesAttachmentsAndSkipsBusyDrafts(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-expiry", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)

	idle := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleaseidle", recordsPostgresDraftPayload(t, "Idle"))
	busy := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleasebusyexp", recordsPostgresDraftPayload(t, "Busy"))
	seedDraftReleaseUpload(t, ctx, attachmentRepository, idle.DraftID, 0, draftReleaseQuarantined)
	seedDraftReleaseUpload(t, ctx, attachmentRepository, busy.DraftID, 1, draftReleaseQuarantined)
	// 只认领 busy 草稿那份：先把 idle 的任务推迟到以后重试。
	if _, err := fixture.db.Exec(ctx, `
		update public.attachment_processor_jobs
		set processor_state = 'retry_wait', retry_at = transaction_timestamp() + interval '1 hour',
		    result_code = 'timeout', result_digest = decode(repeat('ab', 32), 'hex'),
		    result_owner_id = 'processor_seed', result_lease_expires_at = transaction_timestamp()
		where attachment_id = $1`, draftReleaseAttachmentID(idle.DraftID, 0)); err != nil {
		t.Fatalf("defer idle processor job: %v", err)
	}
	claim, err := attachmentRepository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{
		OwnerID: "processor_release_expiry", OwnerLeaseDuration: 5 * time.Minute,
	})
	if err != nil || claim == nil || claim.AttachmentID != draftReleaseAttachmentID(busy.DraftID, 1) {
		t.Fatalf("ClaimProcessorJob() = (%#v, %v), want busy draft job", claim, err)
	}
	// busy 草稿过期更早，排在批次最前；limit=1 时它不能占住名额饿死 idle 草稿。
	expireRecordsPostgresDraft(t, ctx, fixture, busy.DraftID, "2 days")
	expireRecordsPostgresDraft(t, ctx, fixture, idle.DraftID, "1 day")

	claimed, err := draftRepository.ClaimExpiredDrafts(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimExpiredDrafts() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0] != idle.DraftID {
		t.Fatalf("ClaimExpiredDrafts() = %#v, want only idle draft", claimed)
	}
	assertDraftReleaseRowsGone(t, ctx, fixture, idle.DraftID)
	var busyDrafts, busyAttachments int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_drafts where draft_id = $1),
		       (select count(*)::int from public.record_attachments where draft_id = $1)`,
		busy.DraftID).Scan(&busyDrafts, &busyAttachments); err != nil {
		t.Fatalf("read skipped busy draft: %v", err)
	}
	if busyDrafts != 1 || busyAttachments != 1 {
		t.Fatalf("busy expired draft rows = %d/%d, want kept 1/1", busyDrafts, busyAttachments)
	}
	// idle 的预留退回、分片登记为 Blob；busy 那份的预留原样保留，savepoint 撤销了它的任何改动。
	assertDraftReleaseQuota(t, ctx, fixture, 0, draftReleaseSize, draftReleaseSize)
}

func TestPostgresIntegrationRecordDraftExpirySavepointUndoesPartialRelease(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-savepoint", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)

	fenced := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleasefence", recordsPostgresDraftPayload(t, "Fenced"))
	idle := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleasezidle", recordsPostgresDraftPayload(t, "Idle"))
	seedDraftReleaseUpload(t, ctx, attachmentRepository, fenced.DraftID, 0, draftReleaseUploading)
	seedDraftReleaseUpload(t, ctx, attachmentRepository, fenced.DraftID, 1, draftReleaseUploading)
	seedDraftReleaseUpload(t, ctx, attachmentRepository, idle.DraftID, 0, draftReleaseUploading)
	// 上传中但已有分片：释放时要把分片对象登记为 Blob。给第二份分片的对象挂上进行中的 GC 删除，
	// 让第一份登记成功之后才遇到 busy，验证 savepoint 撤销已经写入的登记。
	for _, seed := range []struct {
		draftID string
		index   int
	}{{fenced.DraftID, 0}, {fenced.DraftID, 1}, {idle.DraftID, 0}} {
		seedDraftReleasePart(t, ctx, fixture, seed.draftID, seed.index)
	}
	second := draftReleaseObject(fenced.DraftID, 1)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.blob_gc_deletions (
			deletion_id, purge_mode, blob_key, sha256_digest, object_version, size_bytes, backend_kind,
			blob_created_at, deletion_state, owner_id, owner_generation, attempt, lease_expires_at
		) values (
			'bgd_pgreleasefence', 'ordinary', $1, $2, $3, $4, 'local',
			transaction_timestamp() - interval '2 days', 'claimed', 'blob_gc_worker', 1, 1,
			transaction_timestamp() + interval '5 minutes'
		)`, second.Key, second.SHA256[:], second.VersionID, second.SizeBytes); err != nil {
		t.Fatalf("seed active GC deletion: %v", err)
	}
	expireRecordsPostgresDraft(t, ctx, fixture, fenced.DraftID, "2 days")
	expireRecordsPostgresDraft(t, ctx, fixture, idle.DraftID, "1 day")

	claimed, err := draftRepository.ClaimExpiredDrafts(ctx, 10)
	if err != nil {
		t.Fatalf("ClaimExpiredDrafts() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0] != idle.DraftID {
		t.Fatalf("ClaimExpiredDrafts() = %#v, want only idle draft", claimed)
	}
	var firstRegistered, fencedAttachments int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.blob_objects where blob_key = $1),
		       (select count(*)::int from public.record_attachments where draft_id = $2)`,
		draftReleaseObject(fenced.DraftID, 0).Key, fenced.DraftID).Scan(&firstRegistered, &fencedAttachments); err != nil {
		t.Fatalf("read fenced draft state: %v", err)
	}
	if firstRegistered != 0 || fencedAttachments != 2 {
		t.Fatalf("fenced draft after skip = registered %d attachments %d, want savepoint rollback 0/2", firstRegistered, fencedAttachments)
	}
	assertDraftReleaseRowsGone(t, ctx, fixture, idle.DraftID)
	// idle 那份退预留并登记分片 Blob；fenced 两份的预留原样保留。
	assertDraftReleaseQuota(t, ctx, fixture, 0, 2*draftReleaseSize, draftReleaseSize)
}

func TestPostgresIntegrationRecordDraftReleaseLocksWorkspacesBeforeJobs(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-draft-attachment-lock-order", 2)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	attachmentRepository := NewPostgresAttachmentRepository(runtimePool)

	draft := createRecordsPostgresDraft(t, ctx, draftRepository, "rdf_pgreleaseorder", recordsPostgresDraftPayload(t, "Order"))
	seedDraftReleaseUpload(t, ctx, attachmentRepository, draft.DraftID, 0, draftReleaseQuarantined)
	jobID := fmt.Sprintf("apj_%s0", draft.DraftID[len("rdf_"):])
	if _, err := fixture.db.Exec(ctx, `
		insert into public.content_processor_workspaces (
			workspace_id, processor_job_id, attempt, workspace_state, workspace_path_digest, expires_at, purged_at
		) values ('cpw_pgreleaseorder', $1, 1, 'purged', decode(repeat('cd', 32), 'hex'),
			transaction_timestamp() + interval '1 hour', transaction_timestamp())`, jobID); err != nil {
		t.Fatalf("seed purged workspace: %v", err)
	}

	// 模拟工作区回执重放：先锁工作区行，再去锁任务行（workspace -> job）。
	holder, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin workspace holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `
		select 1 from public.content_processor_workspaces where workspace_id = 'cpw_pgreleaseorder' for update`); err != nil {
		t.Fatalf("hold workspace row: %v", err)
	}
	outcome := make(chan error, 1)
	go func() {
		outcome <- draftRepository.DeleteDraft(context.Background(), records.DraftDeleteCommand{
			DraftID: draft.DraftID, AuthorID: draft.AuthorID, Reason: records.DraftDeleteDiscarded,
		})
	}()
	waitForRecordsPostgresLockWaiter(t, ctx, fixture.db, "%from public.content_processor_workspaces%for update of workspace%")
	// 释放事务此刻在等工作区，还没碰任务行：重放一侧能立即锁到任务行，不会成环。
	if _, err := holder.Exec(ctx, `
		select 1 from public.attachment_processor_jobs where processor_job_id = $1 for update nowait`, jobID); err != nil {
		t.Fatalf("job row locked before workspace row: %v", err)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("release workspace holder: %v", err)
	}
	if err := <-outcome; err != nil {
		t.Fatalf("DeleteDraft() after workspace holder released error = %v", err)
	}
	assertDraftReleaseRowsGone(t, ctx, fixture, draft.DraftID)
}

func expireRecordsPostgresDraft(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, draftID, ago string) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		update public.record_drafts
		set updated_at = transaction_timestamp() - interval '91 days',
		    warning_at = transaction_timestamp() - interval '8 days',
		    expires_at = transaction_timestamp() - $2::interval
		where draft_id = $1`, draftID, ago); err != nil {
		t.Fatalf("expire draft %s: %v", draftID, err)
	}
}

// seedDraftReleasePart 给上传中的草稿附件补一份已写入的分片，模拟本地 PUT 已完成、尚未 complete。
func seedDraftReleasePart(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, draftID string, index int) {
	t.Helper()
	object := draftReleaseObject(draftID, index)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.attachment_upload_parts (upload_id, part_number, size_bytes, sha256_digest, object_version)
		values ($1, 1, $2, $3, $4)`,
		fmt.Sprintf("aup_%s%d", draftID[len("rdf_"):], index), object.SizeBytes, object.SHA256[:], object.VersionID); err != nil {
		t.Fatalf("seed upload part for %s/%d: %v", draftID, index, err)
	}
}

func draftReleaseAttachmentID(draftID string, index int) string {
	return fmt.Sprintf("att_%s%d", draftID[len("rdf_"):], index)
}

func draftReleaseObject(draftID string, index int) attachments.ObjectVersion {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", draftID, index)))
	return attachments.ObjectVersion{
		Key:       "sha256/" + hex.EncodeToString(digest[:]),
		VersionID: "local-v1-" + hex.EncodeToString(digest[:]),
		SHA256:    digest,
		SizeBytes: draftReleaseSize,
	}
}

// seedDraftReleaseUpload 通过仓库真实流程把一份上传推进到指定状态，配额随之按生产规则记账。
func seedDraftReleaseUpload(
	t *testing.T,
	ctx context.Context,
	repository *PostgresAttachmentRepository,
	draftID string,
	index int,
	state draftReleaseUploadState,
) string {
	t.Helper()
	attachmentID := draftReleaseAttachmentID(draftID, index)
	reserve := attachments.ReserveUploadCommand{
		ProjectID:         "default",
		UploadID:          fmt.Sprintf("aup_%s%d", draftID[len("rdf_"):], index),
		AttachmentID:      attachmentID,
		DraftID:           draftID,
		AuthorID:          recordsPostgresDraftAuthorID,
		DisplayName:       fmt.Sprintf("release-%d.txt", index),
		MediaType:         "text/plain",
		TransportKind:     attachments.TransportKindLocal,
		DeclaredSizeBytes: draftReleaseSize,
		ExpiresAt:         time.Now().UTC().Add(time.Hour),
		Limits:            attachments.DefaultLimits(),
	}
	if _, err := repository.ReserveUpload(ctx, reserve); err != nil {
		t.Fatalf("ReserveUpload(%s) error = %v", attachmentID, err)
	}
	mutation := attachments.UploadMutationCommand{ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID}
	object := draftReleaseObject(draftID, index)
	var fingerprint [sha256.Size]byte
	fingerprint[0] = byte(index + 1)
	switch state {
	case draftReleaseCreated:
	case draftReleaseUploading:
		if _, err := repository.StartUpload(ctx, mutation); err != nil {
			t.Fatalf("StartUpload(%s) error = %v", attachmentID, err)
		}
	case draftReleaseQuarantined:
		completeAttachmentIntegrationUpload(t, ctx, repository, reserve, object, fingerprint, fmt.Sprintf("apj_%s%d", draftID[len("rdf_"):], index))
	case draftReleaseAvailable:
		completeAttachmentIntegrationUpload(t, ctx, repository, reserve, object, fingerprint, fmt.Sprintf("apj_%s%d", draftID[len("rdf_"):], index))
		if _, err := repository.AdmitUpload(ctx, attachments.AdmitUploadCommand{
			ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID, Limits: reserve.Limits,
			Blob: attachments.BlobObject{
				Key: object.Key, SHA256: object.SHA256, ObjectVersion: object.VersionID,
				SizeBytes: object.SizeBytes, BackendKind: attachments.BackendKindLocal,
			},
		}); err != nil {
			t.Fatalf("AdmitUpload(%s) error = %v", attachmentID, err)
		}
	case draftReleaseRejected, draftReleaseExpired:
		target := attachments.UploadStateRejected
		if state == draftReleaseExpired {
			target = attachments.UploadStateExpired
		} else if _, err := repository.StartUpload(ctx, mutation); err != nil {
			t.Fatalf("StartUpload(%s) error = %v", attachmentID, err)
		}
		if _, err := repository.FailUpload(ctx, attachments.FailUploadCommand{
			ProjectID: "default", UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
			TargetState: target, Limits: reserve.Limits,
		}); err != nil {
			t.Fatalf("FailUpload(%s) error = %v", attachmentID, err)
		}
	default:
		t.Fatalf("unknown draft release state %q", state)
	}
	return attachmentID
}

func assertDraftReleaseQuota(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, logical, reserved, physical int64) {
	t.Helper()
	var gotLogical, gotReserved, gotPhysical int64
	if err := fixture.db.QueryRow(ctx, `
		select logical_bytes, reserved_bytes, physical_bytes
		from public.attachment_quota_accounts where project_id = 'default'`).Scan(
		&gotLogical, &gotReserved, &gotPhysical,
	); err != nil {
		t.Fatalf("read attachment quota: %v", err)
	}
	if gotLogical != logical || gotReserved != reserved || gotPhysical != physical {
		t.Fatalf("attachment quota = logical %d reserved %d physical %d, want %d/%d/%d",
			gotLogical, gotReserved, gotPhysical, logical, reserved, physical)
	}
}

func assertDraftReleaseRowsGone(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, draftID string) {
	t.Helper()
	// 转给记录的附件保留上传行（origin_draft_id 不变）；其余上传、分片、任务都不能脱离附件残留。
	var drafts, attachmentRows, orphanUploads, orphanParts, orphanJobs int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_drafts where draft_id = $1),
		       (select count(*)::int from public.record_attachments where draft_id = $1),
		       (select count(*)::int from public.attachment_uploads as upload
		          where upload.origin_draft_id = $1
		            and not exists (select 1 from public.record_attachments as attachment
		              where attachment.attachment_id = upload.attachment_id)),
		       (select count(*)::int from public.attachment_upload_parts as part
		          where not exists (select 1 from public.attachment_uploads as upload
		            where upload.upload_id = part.upload_id)),
		       (select count(*)::int from public.attachment_processor_jobs as job
		          where not exists (select 1 from public.record_attachments as attachment
		            where attachment.attachment_id = job.attachment_id))`,
		draftID).Scan(&drafts, &attachmentRows, &orphanUploads, &orphanParts, &orphanJobs); err != nil {
		t.Fatalf("read released draft rows: %v", err)
	}
	if drafts != 0 || attachmentRows != 0 || orphanUploads != 0 || orphanParts != 0 || orphanJobs != 0 {
		t.Fatalf("released draft %s rows = drafts %d attachments %d orphan uploads %d parts %d jobs %d, want all 0",
			draftID, drafts, attachmentRows, orphanUploads, orphanParts, orphanJobs)
	}
}
