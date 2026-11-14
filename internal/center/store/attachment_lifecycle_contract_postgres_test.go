package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
)

// All repository operations use the direct runtime role and an admitted local
// namespace. Administrative SQL below only arranges exceptional test states.
func TestPostgresIntegrationAttachmentLifecycleContract(t *testing.T) {
	t.Run("project_quota_rejection_preserves_rows", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		first := draftReleaseReservation("rdf_contractquotaone", 0)
		second := draftReleaseReservation("rdf_contractquotatwo", 0)
		limits := attachments.Limits{
			MaxFileBytes: 10, MaxRecordBytes: 10, MaxProjectBytes: 10,
			WarningPercent: 80, MaxInlineTextPreviewBytes: 5,
		}
		first.DeclaredSizeBytes, first.Limits = 7, limits
		second.DeclaredSizeBytes, second.Limits = 4, limits
		seedAttachmentDraft(t, ctx, f.fixture, first.DraftID, first.AuthorID)
		seedAttachmentDraft(t, ctx, f.fixture, second.DraftID, second.AuthorID)
		reserved, err := f.repository.ReserveUpload(ctx, first)
		if err != nil || reserved.State != attachments.UploadStateCreated || reserved.Quota.Usage.ReservedBytes != 7 {
			t.Fatalf("first reservation = (%#v, %v)", reserved, err)
		}
		before := attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
		_, err = f.repository.ReserveUpload(ctx, second)
		var exceeded *attachments.QuotaExceededError
		if !errors.As(err, &exceeded) || exceeded.Scope != attachments.QuotaScopeProject {
			t.Fatalf("project quota rejection = %v, want project quota exceeded", err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
	})

	t.Run("opposite_record_copies_preserve_managed_bytes", func(t *testing.T) {
		ctx := context.Background()
		fixture := newAdmittedCoreRecordFixture(t, ctx)
		f := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
		writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
		if err != nil {
			t.Fatal(err)
		}
		drafts := NewPostgresRecordDraftRepository(f.pool, fixture.admissionGate)
		birthFixture := ImportAttachmentGCFixture{Runtime: f.pool, Gate: fixture.admissionGate}
		records := newRevisionMutationAdmittedRepository(t, birthFixture, NewRecordAttachmentRevisionParticipant())
		sources := []struct {
			draftID  string
			recordID string
			title    string
			jobID    string
			content  []byte
			reserve  attachments.ReserveUploadCommand
			written  attachments.WrittenBlob
		}{
			{draftID: "rdf_contractcopya", title: "Copy source A", jobID: "apj_contractcopya", content: []byte("managed copy source A")},
			{draftID: "rdf_contractcopyb", title: "Copy source B", jobID: "apj_contractcopyb", content: []byte("managed copy source B")},
		}
		for index := range sources {
			source := &sources[index]
			draft := createRecordsPostgresDraft(t, ctx, drafts, source.draftID, recordsPostgresDraftPayload(t, source.title))
			source.reserve = draftReleaseReservation(draft.DraftID, 0)
			source.reserve.DeclaredSizeBytes = int64(len(source.content))
			if _, err := f.repository.ReserveUpload(ctx, source.reserve); err != nil {
				t.Fatal(err)
			}
			source.written = putManagedAttachmentUpload(t, ctx, f.repository, writer, source.reserve, source.content)
			enqueueManagedAttachmentUpload(t, ctx, f.repository, source.reserve, source.written, source.jobID)
			completeManagedAttachmentProcessor(t, ctx, f.repository, writer, source.reserve.UploadID, source.content, source.reserve.Limits)
			command := revisionBirthBatchCommand(t, ctx, birthFixture,
				recordsPostgresCompleteRevisionInput(t, source.title, source.reserve.AttachmentID), "attachment-contract-copy-"+source.draftID)
			command.DraftID, command.DraftETag = draft.DraftID, draft.ETag
			created, err := records.CommitRevision(ctx, command)
			if err != nil {
				t.Fatalf("publish source %s: %v", source.draftID, err)
			}
			source.recordID = created.RecordID
		}
		before, err := f.repository.GetProjectQuotaSnapshot(ctx, attachments.ProjectQuotaSnapshotCommand{ProjectID: "default", Limits: attachments.DefaultLimits()})
		if err != nil {
			t.Fatal(err)
		}
		blocker, err := f.fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = blocker.Rollback(context.Background()) }()
		var lockedID string
		if err := blocker.QueryRow(ctx, `select object_id from public.content_delivery_epochs where project_id = 'default' and object_kind = 'record' and object_id = $1 for update`, sources[0].recordID).Scan(&lockedID); err != nil {
			t.Fatalf("lock first record epoch: %v", err)
		}
		type copyResult struct {
			index  int
			result attachments.CopyAttachmentResult
			err    error
		}
		outcomes := make(chan copyResult, 2)
		copyIDs := []string{"att_contractcopyab", "att_contractcopyba"}
		appNames := []string{"attachment-copy-ab", "attachment-copy-ba"}
		raceCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		defer cancel()
		for index := range sources {
			pool := f.fixture.openDirectRuntimePool(t, ctx, appNames[index], 2)
			repository := NewPostgresAttachmentRepository(pool)
			if err := repository.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, pool, fixture.admissionGate)); err != nil {
				t.Fatal(err)
			}
			command := attachments.CopyAttachmentCommand{
				ProjectID: "default", SourceRecordID: sources[index].recordID, TargetRecordID: sources[1-index].recordID,
				SourceAttachmentID: sources[index].reserve.AttachmentID, TargetAttachmentID: copyIDs[index],
				ActorID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Limits: attachments.DefaultLimits(),
			}
			go func() {
				result, err := repository.CopyAttachment(raceCtx, command)
				outcomes <- copyResult{index: index, result: result, err: err}
			}()
		}
		for _, name := range appNames {
			waitForRecordPlatformBackendLock(t, ctx, f.fixture.db, name)
		}
		if err := blocker.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		for range sources {
			outcome := waitForRecordPlatformResult(t, outcomes, "opposite attachment copies")
			if outcome.err != nil || outcome.result.AttachmentID != copyIDs[outcome.index] || outcome.result.CopiedFromAttachmentID != sources[outcome.index].reserve.AttachmentID {
				t.Fatalf("copy %d = (%#v, %v)", outcome.index, outcome.result, outcome.err)
			}
		}
		for index, source := range sources {
			var recordID, sourceID, key, version, namespace string
			if err := f.fixture.db.QueryRow(ctx, `select a.record_id, a.copied_from_attachment_id, a.blob_key, a.blob_object_version, b.namespace_id::text
				from public.record_attachments a join public.blob_objects b on b.blob_key = a.blob_key and b.object_version = a.blob_object_version
				where a.attachment_id = $1`, copyIDs[index]).Scan(&recordID, &sourceID, &key, &version, &namespace); err != nil {
				t.Fatal(err)
			}
			if recordID != sources[1-index].recordID || sourceID != source.reserve.AttachmentID || key != source.written.Version.Key || version != source.written.Version.VersionID || namespace != source.written.NamespaceID {
				t.Fatalf("copied identity = %s/%s %s@%s namespace %s", recordID, sourceID, key, version, namespace)
			}
			got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, source.written.Version)
			if err != nil || !bytes.Equal(got, source.content) {
				t.Fatalf("copied source bytes = %q, %v", got, err)
			}
		}
		after, err := f.repository.GetProjectQuotaSnapshot(ctx, attachments.ProjectQuotaSnapshotCommand{ProjectID: "default", Limits: attachments.DefaultLimits()})
		if err != nil {
			t.Fatal(err)
		}
		want := before.Usage
		want.LogicalBytes += int64(len(sources[0].content) + len(sources[1].content))
		if after.Usage != want {
			t.Fatalf("copied quota = %#v, want %#v", after.Usage, want)
		}
	})

	t.Run("invalid_persisted_quota_and_version_overflow", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		first := draftReleaseReservation("rdf_contractinvalidquota", 0)
		seedAttachmentDraft(t, ctx, f.fixture, first.DraftID, first.AuthorID)
		if _, err := f.repository.ReserveUpload(ctx, first); err != nil {
			t.Fatal(err)
		}
		var usage attachments.QuotaUsage
		var quotaVersion int64
		if err := f.fixture.db.QueryRow(ctx, `select logical_bytes, reserved_bytes, physical_bytes, quota_version from public.attachment_quota_accounts where project_id = 'default'`).Scan(&usage.LogicalBytes, &usage.ReservedBytes, &usage.PhysicalBytes, &quotaVersion); err != nil {
			t.Fatal(err)
		}
		invalid := draftReleaseReservation(first.DraftID, 1)
		columns := []struct {
			column string
			value  int64
		}{
			{"logical_bytes", usage.LogicalBytes},
			{"reserved_bytes", usage.ReservedBytes},
			{"physical_bytes", usage.PhysicalBytes},
			{"quota_version", quotaVersion},
		}
		before := attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
		for _, column := range columns {
			_, err := f.fixture.db.Exec(ctx, `update public.attachment_quota_accounts set `+pgx.Identifier{column.column}.Sanitize()+` = -1 where project_id = 'default'`)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("negative %s accepted by schema: %v", column.column, err)
			}
			assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
		}

		// The production checks above remain intact in migrations. Drop them only
		// in this disposable database to exercise runtime handling of corrupt or
		// legacy persisted quota values, which normal writes cannot create.
		for _, column := range columns {
			constraint := "attachment_quota_accounts_" + column.column + "_check"
			if _, err := f.fixture.db.Exec(ctx, `alter table public.attachment_quota_accounts drop constraint `+pgx.Identifier{constraint}.Sanitize()); err != nil {
				t.Fatal(err)
			}
		}
		for _, column := range columns {
			t.Run(column.column, func(t *testing.T) {
				identifier := pgx.Identifier{column.column}.Sanitize()
				if _, err := f.fixture.db.Exec(ctx, `update public.attachment_quota_accounts set `+identifier+` = -1 where project_id = 'default'`); err != nil {
					t.Fatal(err)
				}
				before := attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
				if _, err := f.repository.ReserveUpload(ctx, invalid); !errors.Is(err, attachments.ErrInvalidQuotaUsage) {
					t.Fatalf("reservation with negative %s = %v", column.column, err)
				}
				assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
				if _, err := f.fixture.db.Exec(ctx, `update public.attachment_quota_accounts set `+identifier+` = $1 where project_id = 'default'`, column.value); err != nil {
					t.Fatal(err)
				}
			})
		}
		if _, err := f.fixture.db.Exec(ctx, `update public.attachment_quota_accounts set quota_version = $1 where project_id = 'default'`, int64(math.MaxInt64)); err != nil {
			t.Fatal(err)
		}
		before = attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
		if _, err := f.repository.ReserveUpload(ctx, invalid); !errors.Is(err, attachments.ErrQuotaOverflow) {
			t.Fatalf("reservation with exhausted quota version = %v", err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
	})

	t.Run("upload_owner_route_drift", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		reserve := draftReleaseReservation("rdf_contractroute", 0)
		seedAttachmentDraft(t, ctx, f.fixture, reserve.DraftID, reserve.AuthorID)
		if _, err := f.repository.ReserveUpload(ctx, reserve); err != nil {
			t.Fatal(err)
		}
		const replacementAttachment = "att_contractroutereplacement"
		const replacementDraft = "rdf_contractrouteother"
		seedAttachmentDraft(t, ctx, f.fixture, replacementDraft, reserve.AuthorID)
		if _, err := f.fixture.db.Exec(ctx, `
			insert into public.record_attachments
			  (attachment_id, project_id, draft_id, origin_draft_id, attachment_state,
			   display_name, media_type, logical_size_bytes, created_by)
			values ($1, $2, $3, $3, 'uploading', $4, $5, $6, $7)`,
			replacementAttachment, reserve.ProjectID, replacementDraft, reserve.DisplayName,
			reserve.MediaType, reserve.DeclaredSizeBytes, reserve.AuthorID); err != nil {
			t.Fatal(err)
		}
		blocker, err := f.fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = blocker.Rollback(context.Background()) }()
		if _, err := blocker.Exec(ctx, `select draft_id from public.record_drafts where draft_id = $1 for update`, reserve.DraftID); err != nil {
			t.Fatal(err)
		}
		// Move the composite route together. Its replacement attachment is in a
		// different state, so reselecting it must report the existing conflict.
		if _, err := blocker.Exec(ctx, `update public.attachment_uploads set attachment_id = $2, origin_draft_id = $3 where upload_id = $1`, reserve.UploadID, replacementAttachment, replacementDraft); err != nil {
			t.Fatal(err)
		}
		before := attachmentLifecycleSnapshot(t, ctx, blocker)
		const appName = "attachment-contract-route"
		workerPool := f.fixture.openDirectRuntimePool(t, ctx, appName, 1)
		repository := NewPostgresAttachmentRepository(workerPool)
		var holderPID, workerPID int
		if err := blocker.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&holderPID); err != nil {
			t.Fatal(err)
		}
		if err := workerPool.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&workerPID); err != nil {
			t.Fatal(err)
		}
		outcome := make(chan error, 1)
		raceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		go func() {
			_, err := repository.StartUpload(raceCtx, attachments.UploadMutationCommand{
				ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
			})
			outcome <- err
		}()
		waitForMutationTestBlocker(t, raceCtx, f.fixture.db, workerPID, holderPID, outcome)
		if err := blocker.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := waitForRecordPlatformResult(t, outcome, "upload route drift"); !errors.Is(err, attachments.ErrAttachmentConflict) {
			t.Fatalf("StartUpload after route drift = %v, want attachment conflict", err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
	})

	t.Run("upload_owner_route_reselection", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		reserve := draftReleaseReservation("rdf_contractrouteretry", 0)
		seedAttachmentDraft(t, ctx, f.fixture, reserve.DraftID, reserve.AuthorID)
		reserved, err := f.repository.ReserveUpload(ctx, reserve)
		if err != nil {
			t.Fatal(err)
		}
		const replacementAttachment = "att_contractrouteretry"
		if _, err := f.fixture.db.Exec(ctx, `
			insert into public.record_attachments
			  (attachment_id, project_id, draft_id, origin_draft_id, attachment_state,
			   display_name, media_type, logical_size_bytes, created_by)
			values ($1, $2, $3, $3, 'created', $4, $5, $6, $7)`,
			replacementAttachment, reserve.ProjectID, reserve.DraftID, reserve.DisplayName,
			reserve.MediaType, reserve.DeclaredSizeBytes, reserve.AuthorID); err != nil {
			t.Fatal(err)
		}
		var quotaVersion int64
		if err := f.fixture.db.QueryRow(ctx, `select quota_version from public.attachment_quota_accounts where project_id = $1`, reserve.ProjectID).Scan(&quotaVersion); err != nil {
			t.Fatal(err)
		}
		blocker, err := f.fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = blocker.Rollback(context.Background()) }()
		if _, err := blocker.Exec(ctx, `select draft_id from public.record_drafts where draft_id = $1 for update`, reserve.DraftID); err != nil {
			t.Fatal(err)
		}
		if _, err := blocker.Exec(ctx, `update public.attachment_uploads set attachment_id = $2 where upload_id = $1`, reserve.UploadID, replacementAttachment); err != nil {
			t.Fatal(err)
		}
		const appName = "attachment-contract-route-retry"
		workerPool := f.fixture.openDirectRuntimePool(t, ctx, appName, 1)
		repository := NewPostgresAttachmentRepository(workerPool)
		var holderPID, workerPID int
		if err := blocker.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&holderPID); err != nil {
			t.Fatal(err)
		}
		if err := workerPool.QueryRow(ctx, `select pg_catalog.pg_backend_pid()`).Scan(&workerPID); err != nil {
			t.Fatal(err)
		}
		type startResult struct {
			result attachments.UploadMutationResult
			err    error
		}
		outcome := make(chan startResult, 1)
		raceCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		go func() {
			result, err := repository.StartUpload(raceCtx, attachments.UploadMutationCommand{
				ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
			})
			outcome <- startResult{result: result, err: err}
		}()
		waitForMutationTestBlocker(t, raceCtx, f.fixture.db, workerPID, holderPID, outcome)
		if err := blocker.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		started := waitForRecordPlatformResult(t, outcome, "upload route reselection")
		if started.err != nil || started.result.AttachmentID != replacementAttachment || started.result.State != attachments.UploadStateUploading {
			t.Fatalf("StartUpload after authorized reselection = (%#v, %v)", started.result, started.err)
		}
		var oldState, newState attachments.UploadState
		var afterQuota attachments.QuotaUsage
		var afterVersion int64
		if err := f.fixture.db.QueryRow(ctx, `
			select old.attachment_state, replacement.attachment_state, q.logical_bytes, q.reserved_bytes, q.physical_bytes, q.quota_version
			from public.record_attachments old, public.record_attachments replacement, public.attachment_quota_accounts q
			where old.attachment_id = $1 and replacement.attachment_id = $2 and q.project_id = $3`,
			reserve.AttachmentID, replacementAttachment, reserve.ProjectID).Scan(&oldState, &newState,
			&afterQuota.LogicalBytes, &afterQuota.ReservedBytes, &afterQuota.PhysicalBytes, &afterVersion); err != nil {
			t.Fatal(err)
		}
		if oldState != attachments.UploadStateCreated || newState != attachments.UploadStateUploading || afterQuota != reserved.Quota.Usage || afterVersion != quotaVersion {
			t.Fatalf("reselected route states %s/%s quota %#v@%d, want created/uploading %#v@%d", oldState, newState, afterQuota, afterVersion, reserved.Quota.Usage, quotaVersion)
		}
	})

	t.Run("legacy_completion_cannot_skip_part_or_processor", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
		if err != nil {
			t.Fatal(err)
		}
		reserve := draftReleaseReservation("rdf_contractlegacy", 0)
		content := []byte("legacy completion contract")
		reserve.DeclaredSizeBytes = int64(len(content))
		seedAttachmentDraft(t, ctx, f.fixture, reserve.DraftID, reserve.AuthorID)
		if _, err := f.repository.ReserveUpload(ctx, reserve); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repository.StartUpload(ctx, attachments.UploadMutationCommand{ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID}); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		legacy := attachments.CompleteUploadContentCommand{
			ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
			ActualSizeBytes: int64(len(content)), ActualSHA256: digest,
			TemporaryObjectKey: "temporary/legacy-contract", TemporaryObjectVersion: "legacy-version",
			CompletionFingerprint: digest,
		}
		var written attachments.WrittenBlob
		for _, stage := range []string{"without_part", "without_processor"} {
			if stage == "without_processor" {
				written = putManagedAttachmentUpload(t, ctx, f.repository, writer, reserve, content)
			}
			before := attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
			if _, err := f.repository.CompleteUploadContent(ctx, legacy); !errors.Is(err, attachments.ErrAttachmentConflict) {
				t.Fatalf("legacy completion %s = %v", stage, err)
			}
			assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
		}
		enqueueManagedAttachmentUpload(t, ctx, f.repository, reserve, written, "apj_contractlegacy")
		got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, written.Version)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("managed source bytes after completion = %q, %v", got, err)
		}
	})

	for _, table := range []string{"attachment_uploads", "record_attachments", "attachment_quota_accounts"} {
		t.Run("failed_upload_rollback_"+table, func(t *testing.T) {
			ctx := context.Background()
			f := newLocalPublicationFixture(t, ctx)
			reserve := draftReleaseReservation("rdf_contractrollback", 0)
			seedAttachmentDraft(t, ctx, f.fixture, reserve.DraftID, reserve.AuthorID)
			if _, err := f.repository.ReserveUpload(ctx, reserve); err != nil {
				t.Fatal(err)
			}
			if _, err := f.repository.StartUpload(ctx, attachments.UploadMutationCommand{ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID}); err != nil {
				t.Fatal(err)
			}
			if _, err := f.fixture.db.Exec(ctx, `create function public.fail_attachment_lifecycle_contract() returns trigger language plpgsql as $$ begin raise exception 'attachment lifecycle contract cutpoint'; end $$;
				create trigger fail_attachment_lifecycle_contract before update on public.`+pgx.Identifier{table}.Sanitize()+` for each row execute function public.fail_attachment_lifecycle_contract()`); err != nil {
				t.Fatal(err)
			}
			before := attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
			command := attachments.FailUploadCommand{
				ProjectID: reserve.ProjectID, UploadID: reserve.UploadID, AuthorID: reserve.AuthorID,
				TargetState: attachments.UploadStateRejected, Limits: reserve.Limits,
			}
			if _, err := f.repository.FailUpload(ctx, command); err == nil || !strings.Contains(err.Error(), "attachment lifecycle contract cutpoint") {
				t.Fatalf("FailUpload(%s cutpoint) = %v", table, err)
			}
			assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
			if _, err := f.fixture.db.Exec(ctx, `drop trigger fail_attachment_lifecycle_contract on public.`+pgx.Identifier{table}.Sanitize()); err != nil {
				t.Fatal(err)
			}
			result, err := f.repository.FailUpload(ctx, command)
			if err != nil || result.State != attachments.UploadStateRejected || result.Quota.Usage.ReservedBytes != 0 {
				t.Fatalf("FailUpload after rollback = (%#v, %v)", result, err)
			}
		})
	}

	t.Run("processor_claim_order_and_skip_locked", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
		if err != nil {
			t.Fatal(err)
		}
		contents := [][]byte{[]byte("claim source A"), []byte("claim source B"), []byte("claim source C")}
		suffixes := []string{"contractclaima", "contractclaimb", "contractclaimc"}
		seeds := make([]attachmentProcessorSeedResult, len(suffixes))
		jobIDs := make([]string, len(suffixes))
		for index, suffix := range suffixes {
			seeds[index] = seedManagedAttachmentProcessor(t, ctx, f.fixture, f.repository, writer, suffix, contents[index])
			jobIDs[index] = seeds[index].ProcessorJobID
		}
		if _, err := f.fixture.db.Exec(ctx, `update public.attachment_processor_jobs set created_at = transaction_timestamp() - interval '1 minute' where processor_job_id = any($1::text[])`, jobIDs); err != nil {
			t.Fatal(err)
		}
		quota, err := f.repository.GetProjectQuotaSnapshot(ctx, attachments.ProjectQuotaSnapshotCommand{ProjectID: "default", Limits: attachments.DefaultLimits()})
		if err != nil {
			t.Fatal(err)
		}
		assertClaim := func(index int, owner string) {
			t.Helper()
			claimCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			claim, err := f.repository.ClaimProcessorJob(claimCtx, attachments.ProcessorClaimInput{OwnerID: owner, OwnerLeaseDuration: 2 * time.Minute})
			seed := seeds[index]
			if err != nil || claim == nil {
				t.Fatalf("claim %s = (%#v, %v)", seed.ProcessorJobID, claim, err)
			}
			if claim.ProcessorJobID != seed.ProcessorJobID || claim.UploadID != seed.UploadID || claim.AttachmentID != seed.AttachmentID || claim.DraftID != seed.DraftID ||
				claim.DisplayName != seed.DisplayName || claim.DeclaredMediaType != seed.DeclaredMediaType || claim.Source != seed.Source || claim.Profile != seed.Profile ||
				claim.Attempt != 1 || claim.MaxAttempts != seed.MaxAttempts || claim.OwnerID != owner || claim.OwnerGeneration != 1 ||
				!claim.ExpiresAt.Equal(seed.ExpiresAt.UTC().Truncate(time.Microsecond)) || !claim.LeaseExpiresAt.After(time.Now()) || claim.LeaseExpiresAt.After(claim.ExpiresAt) {
				t.Fatalf("claim %s lost its durable identity: %#v, seed %#v", seed.ProcessorJobID, claim, seed)
			}
		}
		assertClaim(0, "processor_contract_first")
		blocker, err := f.fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = blocker.Rollback(context.Background()) }()
		var lockedID string
		if err := blocker.QueryRow(ctx, `select processor_job_id from public.attachment_processor_jobs where processor_job_id = $1 for update`, jobIDs[1]).Scan(&lockedID); err != nil {
			t.Fatal(err)
		}
		assertClaim(2, "processor_contract_skipped")
		if err := blocker.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		assertClaim(1, "processor_contract_released")
		if claim, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "processor_contract_empty", OwnerLeaseDuration: time.Minute}); err != nil || claim != nil {
			t.Fatalf("claim with no eligible work = (%#v, %v)", claim, err)
		}
		after, err := f.repository.GetProjectQuotaSnapshot(ctx, attachments.ProjectQuotaSnapshotCommand{ProjectID: "default", Limits: attachments.DefaultLimits()})
		if err != nil || after.Usage != quota.Usage {
			t.Fatalf("claim changed quota: before %#v, after %#v, error %v", quota.Usage, after.Usage, err)
		}
		for index, seed := range seeds {
			object := attachments.ObjectVersion{Key: seed.Source.Key, VersionID: seed.Source.ObjectVersion, SHA256: seed.Source.SHA256, SizeBytes: seed.Source.SizeBytes}
			got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, object)
			if err != nil || !bytes.Equal(got, contents[index]) {
				t.Fatalf("claim source %d bytes = %q, %v", index, got, err)
			}
		}
	})

	t.Run("processor_expiry_requires_quota_and_no_work_is_read_only", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		input := attachments.ProcessorExpiryInput{ProjectID: "default", OwnerID: "processor_contract_expiry", Limits: attachments.DefaultLimits()}
		if _, err := f.fixture.db.Exec(ctx, `delete from public.attachment_quota_accounts where project_id = 'default'`); err != nil {
			t.Fatal(err)
		}
		before := attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
		if result, err := f.repository.ExpireBoundedProcessorJob(ctx, input); err != nil || result != nil {
			t.Fatalf("empty expiry = (%#v, %v)", result, err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
		writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("processor expiry source")
		seed := seedManagedAttachmentProcessor(t, ctx, f.fixture, f.repository, writer, "contractexpiry", content)
		before = attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
		if result, err := f.repository.ExpireBoundedProcessorJob(ctx, input); err != nil || result != nil {
			t.Fatalf("live job expiry = (%#v, %v)", result, err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
		if _, err := f.fixture.db.Exec(ctx, `delete from public.attachment_quota_accounts where project_id = 'default'`); err != nil {
			t.Fatal(err)
		}
		before = attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
		if result, err := f.repository.ExpireBoundedProcessorJob(ctx, input); result != nil || !errors.Is(err, attachments.ErrAttachmentConflict) {
			t.Fatalf("expiry with active job and missing quota = (%#v, %v)", result, err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
		object := attachments.ObjectVersion{Key: seed.Source.Key, VersionID: seed.Source.ObjectVersion, SHA256: seed.Source.SHA256, SizeBytes: seed.Source.SizeBytes}
		got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, object)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("source bytes after failed expiry = %q, %v", got, err)
		}
	})

	t.Run("processor_stale_claim_and_past_retry_preserve_state", func(t *testing.T) {
		ctx := context.Background()
		f := newLocalPublicationFixture(t, ctx)
		writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
		if err != nil {
			t.Fatal(err)
		}
		content := []byte("processor claim contract")
		seed := seedManagedAttachmentProcessor(t, ctx, f.fixture, f.repository, writer, "contractclaim", content)
		claim, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "processor_contract_owner", OwnerLeaseDuration: time.Minute})
		if err != nil || claim == nil || claim.ProcessorJobID != seed.ProcessorJobID {
			t.Fatalf("processor claim = (%#v, %v)", claim, err)
		}
		renewed, err := f.repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: *claim, OwnerLeaseDuration: 5 * time.Minute})
		if err != nil || !renewed.LeaseExpiresAt.After(claim.LeaseExpiresAt) {
			t.Fatalf("processor renewal = (%#v, %v)", renewed, err)
		}
		result := attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeScannerUnavailable}
		before := attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
		if _, err := f.repository.RenewProcessorClaim(ctx, attachments.ProcessorRenewInput{Claim: *claim, OwnerLeaseDuration: time.Minute}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
			t.Fatalf("stale claim renewal = %v", err)
		}
		workspaceDigest := sha256.Sum256([]byte("contract stale workspace"))
		if _, err := f.repository.RegisterProcessorWorkspace(ctx, attachments.ProcessorWorkspaceRegistration{
			Claim: *claim, WorkspaceID: "cpw_contractstale", WorkspacePathDigest: workspaceDigest, ExpiresAt: claim.LeaseExpiresAt,
		}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
			t.Fatalf("stale workspace registration = %v", err)
		}
		if _, err := f.repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{
			Claim: *claim, Result: result, RetryAt: time.Now().UTC().Add(time.Minute), Limits: attachments.DefaultLimits(),
		}); !errors.Is(err, attachments.ErrProcessorClaimLost) {
			t.Fatalf("stale processor completion = %v", err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
		var past time.Time
		if err := f.fixture.db.QueryRow(ctx, `select clock_timestamp() - interval '1 minute'`).Scan(&past); err != nil {
			t.Fatal(err)
		}
		input := attachments.ProcessorCompletionInput{Claim: renewed, Result: result, RetryAt: past, Limits: attachments.DefaultLimits()}
		if _, err := f.repository.CompleteProcessorJob(ctx, input); !errors.Is(err, attachments.ErrInvalidProcessorCommand) {
			t.Fatalf("past retry completion = %v", err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
		input.RetryAt = time.Now().UTC().Add(time.Minute)
		completed, err := f.repository.CompleteProcessorJob(ctx, input)
		if err != nil || completed.ProcessorState != attachments.ProcessorStateRetryWait || completed.UploadState != attachments.UploadStateQuarantined {
			t.Fatalf("valid retry completion = (%#v, %v)", completed, err)
		}
		replayed, err := f.repository.CompleteProcessorJob(ctx, input)
		if err != nil || replayed != completed {
			t.Fatalf("valid retry replay = (%#v, %v), want %#v", replayed, err, completed)
		}
		if _, err := f.fixture.db.Exec(ctx, `update public.attachment_processor_jobs set retry_at = clock_timestamp() - interval '1 second' where processor_job_id = $1`, seed.ProcessorJobID); err != nil {
			t.Fatal(err)
		}
		takeover, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "processor_contract_takeover", OwnerLeaseDuration: time.Minute})
		if err != nil || takeover == nil || takeover.ProcessorJobID != renewed.ProcessorJobID || takeover.Attempt != renewed.Attempt+1 || takeover.OwnerGeneration != renewed.OwnerGeneration+1 {
			t.Fatalf("processor takeover = (%#v, %v)", takeover, err)
		}
		before = attachmentLifecycleSnapshot(t, ctx, f.fixture.db)
		input.RetryAt = time.Now().UTC().Add(time.Minute)
		if _, err := f.repository.CompleteProcessorJob(ctx, input); !errors.Is(err, attachments.ErrProcessorClaimLost) {
			t.Fatalf("completion by superseded owner = %v", err)
		}
		assertAttachmentLifecycleSnapshot(t, ctx, f.fixture.db, before)
		input.Claim = *takeover
		if afterTakeover, err := f.repository.CompleteProcessorJob(ctx, input); err != nil || afterTakeover.ProcessorState != attachments.ProcessorStateRetryWait {
			t.Fatalf("takeover completion = (%#v, %v)", afterTakeover, err)
		}
		object := attachments.ObjectVersion{Key: seed.Source.Key, VersionID: seed.Source.ObjectVersion, SHA256: seed.Source.SHA256, SizeBytes: seed.Source.SizeBytes}
		got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, object)
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("processor source bytes = %q, %v", got, err)
		}
	})
}

type attachmentLifecycleQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Capture consumer-visible persistence, including the original row versions and
// quota, so rolled-back operations cannot silently change a related surface.
func attachmentLifecycleSnapshot(t *testing.T, ctx context.Context, db attachmentLifecycleQueryRower) string {
	t.Helper()
	var snapshot string
	err := db.QueryRow(ctx, `select jsonb_build_object(
		'uploads', coalesce((select jsonb_agg(to_jsonb(u) order by u.upload_id) from public.attachment_uploads u), '[]'::jsonb),
		'attachments', coalesce((select jsonb_agg(to_jsonb(a) order by a.attachment_id) from public.record_attachments a), '[]'::jsonb),
		'parts', coalesce((select jsonb_agg(to_jsonb(p) order by p.upload_id, p.part_number) from public.attachment_upload_parts p), '[]'::jsonb),
		'jobs', coalesce((select jsonb_agg(to_jsonb(j) order by j.processor_job_id) from public.attachment_processor_jobs j), '[]'::jsonb),
		'blobs', coalesce((select jsonb_agg(to_jsonb(b) order by b.blob_key, b.object_version) from public.blob_objects b), '[]'::jsonb),
		'publications', coalesce((select jsonb_agg(to_jsonb(i) order by i.publication_id) from public.blob_publication_intents i), '[]'::jsonb),
		'obligations', coalesce((select jsonb_agg(to_jsonb(o) order by o.obligation_id) from public.record_write_obligations o), '[]'::jsonb),
		'workspaces', coalesce((select jsonb_agg(to_jsonb(w) order by w.workspace_id) from public.content_processor_workspaces w), '[]'::jsonb),
		'quota', coalesce((select to_jsonb(q) from public.attachment_quota_accounts q where q.project_id = 'default'), 'null'::jsonb)
	)::text`).Scan(&snapshot)
	if err != nil {
		t.Fatalf("read attachment lifecycle snapshot: %v", err)
	}
	return snapshot
}

func assertAttachmentLifecycleSnapshot(t *testing.T, ctx context.Context, db attachmentLifecycleQueryRower, before string) {
	t.Helper()
	if after := attachmentLifecycleSnapshot(t, ctx, db); after != before {
		t.Fatalf("attachment lifecycle state changed:\nbefore %s\nafter  %s", before, after)
	}
}
