package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/attachments"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"
)

func requireAuthorityTestBtrfs(t *testing.T, root string) {
	t.Helper()
	var stat unix.Statfs_t
	if err := unix.Statfs(root, &stat); err != nil {
		t.Fatalf("stat actual local Blob root: %v", err)
	}
	if stat.Type != unix.BTRFS_SUPER_MAGIC {
		t.Fatalf("actual Blob root filesystem type=%x, want btrfs", stat.Type)
	}
}

// Roll back even an unexpectedly successful attack so one failure cannot
// manufacture the evidence used by a subsequent assertion.
func assertLocalAuthorityDenied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string, args ...any) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin runtime mutation: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, statement, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("runtime mutation error = %v, want PostgreSQL insufficient_privilege (42501)", err)
	}
}

func TestPostgresIntegrationLocalMutationAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newLocalPublicationFixture(t, ctx)
	requireAuthorityTestBtrfs(t, f.root)
	writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
	if err != nil {
		t.Fatal(err)
	}

	orphanContent := []byte("local orphan guarded by a real cleanup claim")
	orphan := seedPostgresManagedLocalBlob(t, ctx, f.fixture, f.blob, orphanContent)
	orphanObject := attachmentBlobObjectFromVersion(orphan.Version, attachments.BackendKindLocal)
	orphanObject.NamespaceID = orphan.NamespaceID
	seedAttachmentBlobGCObject(t, ctx, f.fixture, orphanObject, time.Now().UTC().Add(-time.Hour))
	other := seedPostgresManagedLocalBlob(t, ctx, f.fixture, f.blob, []byte("different local object for forged task identities"))

	reserved := draftReleaseReservation("rdf_authorityreserved", 0)
	seedAttachmentDraft(t, ctx, f.fixture, reserved.DraftID, reserved.AuthorID)
	if _, err := f.repository.ReserveUpload(ctx, reserved); err != nil {
		t.Fatalf("reserve attachment: %v", err)
	}
	sourceContent := []byte("original available attachment content")
	previewContent := []byte("original available attachment preview")
	available := seedManagedAttachmentProcessor(t, ctx, f.fixture, f.repository, writer, "authorityavailable", sourceContent)
	if result := completeManagedAttachmentProcessor(t, ctx, f.repository, writer, available.UploadID, previewContent, attachments.DefaultLimits()); result.UploadState != attachments.UploadStateAvailable {
		t.Fatalf("complete available attachment: %#v", result)
	}

	request := attachments.LocalCleanupPreparation{
		DeletionID: "bgd_authorityclaim", NamespaceID: orphan.NamespaceID,
		IncarnationID: f.blob.Namespace().IncarnationID, Key: orphan.Version.Key,
		ExpectedVersion: &orphan.Version.VersionID,
	}
	interrupted := &localPublicationInterruption{PostgresLocalBlobRepository: f.local, beforeBinding: true}
	coordinator, err := attachments.NewLocalCleanupCoordinator(f.blob, interrupted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Cleanup(ctx, request, "authority_first"); !errors.Is(err, errLocalPublicationInterrupted) || !interrupted.blocked {
		t.Fatalf("stop after real prepare and claim: %v", err)
	}
	var claimState, claimOwner string
	var claimGeneration int64
	var metadataCount, orphanRefs int
	if err := f.fixture.db.QueryRow(ctx, `select g.deletion_state,g.owner_id,g.owner_generation,
		(select count(*) from public.blob_objects b where b.blob_key=$2 and b.object_version=$3 and b.namespace_id=$4::uuid),
		(select count(*) from public.record_attachments a where a.blob_key=$2 or a.preview_blob_key=$2)
		from public.blob_gc_deletions g where g.deletion_id=$1`, request.DeletionID, orphan.Version.Key, orphan.Version.VersionID, orphan.NamespaceID).
		Scan(&claimState, &claimOwner, &claimGeneration, &metadataCount, &orphanRefs); err != nil {
		t.Fatalf("read real cleanup claim: %v", err)
	}
	if claimState != "claimed" || claimOwner != "authority_first" || claimGeneration < 1 || metadataCount != 1 || orphanRefs != 0 {
		t.Fatalf("claim setup: state=%q owner=%q generation=%d metadata=%d refs=%d", claimState, claimOwner, claimGeneration, metadataCount, orphanRefs)
	}

	// This anomalous legacy row is only an adversarial database fixture. Its
	// contents never authorize a cleanup or stand in for a physical observation.
	owner := f.fixture.openDirectRolePool(t, ctx, f.fixture.migrator, "local-authority-fixture-owner", 1)
	if _, err := owner.Exec(ctx, `insert into public.blob_gc_deletions
		(deletion_id,purge_mode,blob_key,sha256_digest,object_version,size_bytes,backend_kind,blob_created_at,
		 deletion_state,owner_id,owner_generation,attempt,lease_expires_at,namespace_id,incarnation_id,local_contract_version)
		values ('bgd_authoritylegacyflagged','ordinary',$1,$2,$3,$4,'local',clock_timestamp()-interval '1 hour',
		 'claimed','legacy_owner',1,1,clock_timestamp()+interval '1 hour',$5::uuid,$6::uuid,1)`,
		other.Version.Key, other.Version.SHA256[:], other.Version.VersionID, other.Version.SizeBytes, other.NamespaceID, request.IncarnationID); err != nil {
		t.Fatalf("seed legacy row carrying local identity: %v", err)
	}
	legacyDigest := sha256.Sum256([]byte("legacy S3 GC task without local state"))
	legacyKey := fmt.Sprintf("sha256/%x", legacyDigest)
	if _, err := f.pool.Exec(ctx, `insert into public.blob_gc_deletions
		(deletion_id,purge_mode,blob_key,sha256_digest,object_version,size_bytes,backend_kind,blob_created_at,
		 deletion_state,owner_id,owner_generation,attempt,lease_expires_at)
		values ('bgd_authoritylegacy','ordinary',$1,$2,'legacy-s3-version',1,'s3',clock_timestamp()-interval '1 hour',
		 'claimed','legacy_owner',1,1,clock_timestamp()+interval '1 hour')`, legacyKey, legacyDigest[:]); err != nil {
		t.Fatalf("runtime legacy task insertion: %v", err)
	}

	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.fixture.db.QueryRow(ctx, `select jsonb_build_object(
			'claim',(select to_jsonb(g) from public.blob_gc_deletions g where g.deletion_id=$1),
			'obligation',(select to_jsonb(o) from public.record_write_obligations o where o.obligation_id=$2),
			'attachments',(select jsonb_agg(to_jsonb(a) order by a.attachment_id) from public.record_attachments a where a.attachment_id=any($3::text[])),
			'metadata',(select to_jsonb(b) from public.blob_objects b where b.blob_key=$4 and b.object_version=$5),
			'quota',(select to_jsonb(q) from public.attachment_quota_accounts q where q.project_id='default'))::text`,
			request.DeletionID, orphan.ObligationID, []string{reserved.AttachmentID, available.AttachmentID}, orphan.Version.Key, orphan.Version.VersionID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	for _, test := range []struct {
		name string
		id   string
	}{
		{"reserved", reserved.AttachmentID},
		{"available", available.AttachmentID},
	} {
		t.Run("attachment_"+test.name, func(t *testing.T) {
			assertLocalAuthorityDenied(t, ctx, f.pool, `update public.record_attachments
				set attachment_state='available',blob_key=$2,blob_object_version=$3 where attachment_id=$1`,
				test.id, orphan.Version.Key, orphan.Version.VersionID)
		})
	}

	forgedDigest := sha256.Sum256([]byte("different key for a forged version-one task"))
	forgedKey := fmt.Sprintf("sha256/%x", forgedDigest)
	attacks := []struct {
		name string
		sql  string
		args []any
	}{
		{"insert_v2_claim", `insert into public.blob_gc_deletions
			(deletion_id,project_id,purge_mode,blob_key,sha256_digest,backend_kind,deletion_state,namespace_id,incarnation_id,
			 local_contract_version,owner_id,owner_generation,attempt,lease_expires_at,cleanup_dispatch_id)
			select 'bgd_authorityforged',project_id,purge_mode,$2,$3,backend_kind,deletion_state,namespace_id,incarnation_id,
			 local_contract_version,owner_id,owner_generation,attempt,lease_expires_at,cleanup_dispatch_id
			from public.blob_gc_deletions where deletion_id=$1`, []any{request.DeletionID, other.Version.Key, other.Version.SHA256[:]}},
		{"delete_v2_claim", `delete from public.blob_gc_deletions where deletion_id=$1`, []any{request.DeletionID}},
		{"forge_already_absent_receipt", `update public.blob_gc_deletions set deletion_state='completed',physical_delete_result='already_absent',
			receipt_digest=decode(repeat('ab',32),'hex'),completion_command=convert_to('forged','UTF8'),completed_at=clock_timestamp() where deletion_id=$1`, []any{request.DeletionID}},
		{"change_owner", `update public.blob_gc_deletions set owner_id='authority_forged' where deletion_id=$1`, []any{request.DeletionID}},
		{"change_generation", `update public.blob_gc_deletions set owner_generation=owner_generation+1 where deletion_id=$1`, []any{request.DeletionID}},
		{"change_dispatch", `update public.blob_gc_deletions set cleanup_dispatch_id=$2::uuid where deletion_id=$1`, []any{request.DeletionID, localBlobFixtureUUID(t)}},
		{"change_incarnation", `update public.blob_gc_deletions set incarnation_id=null where deletion_id=$1`, []any{request.DeletionID}},
		{"forge_final_identity", `update public.blob_gc_deletions set final_identity_bound=true,final_was_present=true,
			object_version=$2,size_bytes=$3,final_device=1,final_inode=1,inventory_digest=decode(repeat('ac',32),'hex') where deletion_id=$1`, []any{request.DeletionID, orphan.Version.VersionID, orphan.Version.SizeBytes}},
		{"v2_to_v1", `update public.blob_gc_deletions set local_contract_version=1,object_version=$2,size_bytes=$3,
			blob_created_at=clock_timestamp()-interval '1 hour' where deletion_id=$1`, []any{request.DeletionID, orphan.Version.VersionID, orphan.Version.SizeBytes}},
		{"v1_to_v2", `update public.blob_gc_deletions set local_contract_version=2 where deletion_id='bgd_authoritylegacyflagged'`, nil},
		{"update_v1_already_carrying_local_identity", `update public.blob_gc_deletions set owner_id='authority_forged' where deletion_id='bgd_authoritylegacyflagged'`, nil},
		{"insert_v1_with_local_identity", `insert into public.blob_gc_deletions
			(deletion_id,purge_mode,blob_key,sha256_digest,object_version,size_bytes,backend_kind,blob_created_at,
			 deletion_state,owner_id,owner_generation,attempt,lease_expires_at,namespace_id,incarnation_id,cleanup_dispatch_id,local_contract_version)
			values ('bgd_authorityforgedv1','ordinary',$1,$2,'forged-version',1,'local',clock_timestamp()-interval '1 hour',
			 'claimed','legacy_owner',1,1,clock_timestamp()+interval '1 hour',$3::uuid,$4::uuid,$5::uuid,1)`, []any{forgedKey, forgedDigest[:], request.NamespaceID, request.IncarnationID, localBlobFixtureUUID(t)}},
		{"insert_v1_with_namespace_only", `insert into public.blob_gc_deletions
			(deletion_id,purge_mode,blob_key,sha256_digest,object_version,size_bytes,backend_kind,blob_created_at,
			 deletion_state,owner_id,owner_generation,attempt,lease_expires_at,namespace_id,local_contract_version)
			values ('bgd_authorityforgednamespace','ordinary',$1,$2,'forged-version',1,'local',clock_timestamp()-interval '1 hour',
			 'claimed','legacy_owner',1,1,clock_timestamp()+interval '1 hour',$3::uuid,1)`, []any{forgedKey, forgedDigest[:], request.NamespaceID}},
		{"v1_add_namespace", `update public.blob_gc_deletions set namespace_id=$1::uuid where deletion_id='bgd_authoritylegacy'`, []any{request.NamespaceID}},
		{"v1_add_incarnation", `update public.blob_gc_deletions set incarnation_id=$1::uuid where deletion_id='bgd_authoritylegacy'`, []any{request.IncarnationID}},
		{"v1_add_dispatch", `update public.blob_gc_deletions set cleanup_dispatch_id=$1::uuid where deletion_id='bgd_authoritylegacy'`, []any{localBlobFixtureUUID(t)}},
		{"v1_add_inventory", `update public.blob_gc_deletions set inventory_digest=decode(repeat('ad',32),'hex') where deletion_id='bgd_authoritylegacy'`, nil},
		{"v1_add_final_binding", `update public.blob_gc_deletions set final_identity_bound=true where deletion_id='bgd_authoritylegacy'`, nil},
		{"v1_add_final_presence", `update public.blob_gc_deletions set final_was_present=true where deletion_id='bgd_authoritylegacy'`, nil},
		{"v1_add_final_device", `update public.blob_gc_deletions set final_device=1 where deletion_id='bgd_authoritylegacy'`, nil},
		{"v1_add_final_inode", `update public.blob_gc_deletions set final_inode=1 where deletion_id='bgd_authoritylegacy'`, nil},
		{"v1_add_completion_command", `update public.blob_gc_deletions set completion_command=convert_to('forged','UTF8') where deletion_id='bgd_authoritylegacy'`, nil},
	}
	for _, attack := range attacks {
		t.Run(attack.name, func(t *testing.T) {
			assertLocalAuthorityDenied(t, ctx, f.pool, attack.sql, attack.args...)
		})
	}
	if after := snapshot(); after != before {
		t.Fatalf("denied runtime mutations changed durable claim, references, metadata or quota:\nbefore %s\nafter %s", before, after)
	}
	var reservedState, availableState, sourceKey, sourceVersion, previewKey, previewVersion string
	if err := f.fixture.db.QueryRow(ctx, `select r.attachment_state,a.attachment_state,a.blob_key,a.blob_object_version,
		a.preview_blob_key,a.preview_blob_object_version from public.record_attachments r cross join public.record_attachments a
		where r.attachment_id=$1 and a.attachment_id=$2`, reserved.AttachmentID, available.AttachmentID).
		Scan(&reservedState, &availableState, &sourceKey, &sourceVersion, &previewKey, &previewVersion); err != nil {
		t.Fatal(err)
	}
	previewDigest := sha256.Sum256(previewContent)
	if reservedState != "created" || availableState != "available" || sourceKey != available.Source.Key || sourceVersion != available.Source.ObjectVersion || previewKey != fmt.Sprintf("sha256/%x", previewDigest) {
		t.Fatalf("unexpected attachment fixture state: reserved=%s available=%s source=%s@%s preview=%s", reservedState, availableState, sourceKey, sourceVersion, previewKey)
	}
	checks := []struct {
		name    string
		version attachments.ObjectVersion
		want    []byte
	}{
		{"orphan", orphan.Version, orphanContent},
		{"available source", attachments.ObjectVersion{Key: available.Source.Key, VersionID: available.Source.ObjectVersion, SHA256: available.Source.SHA256, SizeBytes: available.Source.SizeBytes}, sourceContent},
		{"available preview", attachments.ObjectVersion{Key: previewKey, VersionID: previewVersion, SHA256: previewDigest, SizeBytes: int64(len(previewContent))}, previewContent},
	}
	for _, check := range checks {
		got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, check.version)
		if err != nil || !bytes.Equal(got, check.want) {
			t.Fatalf("%s bytes after denied mutations = %q, %v", check.name, got, err)
		}
	}

	// A clean legacy V1 task must retain its existing direct-runtime lifecycle,
	// both for local historical data and the S3 path.
	for _, backend := range []string{"local", "s3"} {
		digest := sha256.Sum256([]byte("legacy lifecycle " + backend))
		key, id := fmt.Sprintf("sha256/%x", digest), "bgd_authoritylegacy"+backend
		if _, err := f.pool.Exec(ctx, `insert into public.blob_gc_deletions
			(deletion_id,purge_mode,blob_key,sha256_digest,object_version,size_bytes,backend_kind,blob_created_at,
			 deletion_state,owner_id,owner_generation,attempt,lease_expires_at)
			values ($1,'ordinary',$2,$3,'legacy-version',1,$4,clock_timestamp()-interval '1 hour',
			 'claimed','legacy_owner',1,1,clock_timestamp()+interval '1 hour')`, id, key, digest[:], backend); err != nil {
			t.Fatalf("insert clean legacy %s task: %v", backend, err)
		}
		if _, err := f.pool.Exec(ctx, `update public.blob_gc_deletions set deletion_state='retry_wait',retry_at=clock_timestamp()
			where deletion_id=$1`, id); err != nil {
			t.Fatalf("retry clean legacy %s task: %v", backend, err)
		}
		if _, err := f.pool.Exec(ctx, `update public.blob_gc_deletions set deletion_state='claimed',retry_at=null,
			owner_id='legacy_takeover',owner_generation=owner_generation+1,attempt=attempt+1 where deletion_id=$1`, id); err != nil {
			t.Fatalf("reclaim clean legacy %s task: %v", backend, err)
		}
		if _, err := f.pool.Exec(ctx, `update public.blob_gc_deletions set deletion_state='completed',physical_delete_result='already_absent',
			receipt_digest=decode(repeat('ae',32),'hex'),completed_at=clock_timestamp() where deletion_id=$1`, id); err != nil {
			t.Fatalf("complete clean legacy %s task: %v", backend, err)
		}
	}

	var quotaBefore, quotaVersionBefore int64
	if err := f.fixture.db.QueryRow(ctx, `select physical_bytes,quota_version from public.attachment_quota_accounts where project_id='default'`).Scan(&quotaBefore, &quotaVersionBefore); err != nil {
		t.Fatal(err)
	}
	// The real first claim is expired by the actual table owner solely to make
	// a different coordinator perform a genuine, immediately testable takeover.
	if _, err := owner.Exec(ctx, `update public.blob_gc_deletions set lease_expires_at=clock_timestamp()-interval '1 second' where deletion_id=$1`, request.DeletionID); err != nil {
		t.Fatalf("expire first claim for takeover: %v", err)
	}
	completed := f.cleanup(t, ctx, request, "authority_takeover")
	if *completed.Outcome != "deleted" || *completed.Generation <= claimGeneration {
		t.Fatalf("legitimate takeover completion = %#v", completed)
	}
	if _, err := f.blob.Stat(ctx, orphan.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("legitimate cleanup left orphan bytes: %v", err)
	}
	for _, check := range checks[1:] {
		got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, check.version)
		if err != nil || !bytes.Equal(got, check.want) {
			t.Fatalf("%s bytes after cleanup = %q, %v", check.name, got, err)
		}
	}
	var settled bool
	var completionCount int
	var quotaAfter, quotaVersionAfter int64
	if err := f.fixture.db.QueryRow(ctx, `select
		(select cleanup_outcome='deleted' and cleanup_receipt=decode($2,'hex') and cleanup_completed_at is not null and quota_settled_at is not null
		 from public.record_write_obligations where obligation_id=$1),
		(select count(*) from public.blob_gc_deletions where deletion_id=$3 and deletion_state='completed' and receipt_digest=decode($2,'hex')),
		physical_bytes,quota_version from public.attachment_quota_accounts where project_id='default'`,
		orphan.ObligationID, *completed.ReceiptDigest, request.DeletionID).Scan(&settled, &completionCount, &quotaAfter, &quotaVersionAfter); err != nil {
		t.Fatal(err)
	}
	if !settled || completionCount != 1 || quotaAfter != quotaBefore-orphan.Version.SizeBytes || quotaVersionAfter != quotaVersionBefore+1 {
		t.Fatalf("cleanup settlement: settled=%t completions=%d quota=%d->%d version=%d->%d", settled, completionCount, quotaBefore, quotaAfter, quotaVersionBefore, quotaVersionAfter)
	}
	terminal := snapshot()
	assertLocalAuthorityDenied(t, ctx, f.pool, `update public.blob_gc_deletions set physical_delete_result='already_absent',
		receipt_digest=decode(repeat('af',32),'hex'),completion_command=convert_to('forged terminal','UTF8') where deletion_id=$1`, request.DeletionID)
	assertLocalAuthorityDenied(t, ctx, f.pool, `update public.blob_gc_deletions set final_inode=final_inode+1 where deletion_id=$1`, request.DeletionID)
	if after := snapshot(); after != terminal {
		t.Fatalf("denied terminal mutation changed completed receipt: %s", after)
	}
	replayed := f.cleanup(t, ctx, request, "authority_takeover")
	if *replayed.ReceiptDigest != *completed.ReceiptDigest || *replayed.Generation != *completed.Generation || *replayed.Outcome != *completed.Outcome {
		t.Fatalf("cleanup replay changed completion: first=%#v replay=%#v", completed, replayed)
	}
	if after := snapshot(); after != terminal {
		t.Fatalf("cleanup replay changed terminal state or settled quota: %s", after)
	}
}

func TestPostgresIntegrationLocalPublicationRepeatableRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newLocalPublicationFixture(t, ctx)
	requireAuthorityTestBtrfs(t, f.root)
	config := f.fixture.directRoleConfig(t, f.fixture.runtime, "local-publication-default-rr")
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	config.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var sessionUser, currentUser, isolation string
	if err := pool.QueryRow(ctx, `select session_user,current_user,current_setting('default_transaction_isolation')`).Scan(&sessionUser, &currentUser, &isolation); err != nil {
		t.Fatal(err)
	}
	if sessionUser != f.fixture.runtime || currentUser != f.fixture.runtime || isolation != "repeatable read" {
		t.Fatalf("runtime pool identity/isolation = %q/%q/%q", sessionUser, currentUser, isolation)
	}
	f.pool, f.repository, f.local = pool, NewPostgresAttachmentRepository(pool), NewPostgresLocalBlobRepository(pool, nil, nil)
	writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
	if err != nil {
		t.Fatal(err)
	}

	sourceContent := []byte("upload via default repeatable read runtime")
	a := f.prepare(t, ctx, "aup_rrpublication", sourceContent)
	prepared := a.intent
	f.put(t, ctx, &a, sourceContent)
	f.bind(t, ctx, &a)
	if a.intent.NamespaceID != a.written.NamespaceID || a.intent.ObligationID != a.written.ObligationID || a.intent.ObjectVersion != a.written.Version.VersionID {
		t.Fatalf("upload publication lost exact identity: intent=%#v write=%#v", a.intent, a.written)
	}
	if replay, err := f.repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{Intent: prepared, Object: a.written.Version, ObligationID: a.written.ObligationID}); err != nil || replay != a.intent {
		t.Fatalf("upload version replay on RR-default pool: %#v %v", replay, err)
	}
	f.consume(t, ctx, a)
	enqueueManagedAttachmentUpload(t, ctx, f.repository, a.upload.reserve, a.written, "apj_rrpublication")
	claim, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "rr_publication_processor", OwnerLeaseDuration: 5 * time.Minute})
	if err != nil || claim == nil || claim.UploadID != a.upload.reserve.UploadID {
		t.Fatalf("claim uploaded source: %#v %v", claim, err)
	}
	previewContent := []byte("preview via default repeatable read runtime")
	preview, previewIntent := prepareManagedAttachmentPreview(t, ctx, f.repository, writer, *claim, previewContent)
	if previewIntent.NamespaceID != f.blob.Namespace().NamespaceID || previewIntent.ObligationID == "" || previewIntent.ObjectVersion != preview.ObjectVersion || previewIntent.Target.Key != preview.Key {
		t.Fatalf("preview publication lost exact identity: intent=%#v preview=%#v", previewIntent, preview)
	}
	previewVersion := attachments.ObjectVersion{Key: preview.Key, VersionID: preview.ObjectVersion, SHA256: preview.SHA256, SizeBytes: preview.SizeBytes}
	previewPrepared := previewIntent
	previewPrepared.State, previewPrepared.ObjectVersion, previewPrepared.ObligationID = attachments.BlobPublicationStatePrepared, "", ""
	if replay, err := f.repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{Intent: previewPrepared, Object: previewVersion, ObligationID: previewIntent.ObligationID}); err != nil || replay != previewIntent {
		t.Fatalf("preview version replay on RR-default pool: %#v %v", replay, err)
	}
	for _, check := range []struct {
		name    string
		version attachments.ObjectVersion
		want    []byte
	}{
		{"upload", a.written.Version, sourceContent},
		{"preview", previewVersion, previewContent},
	} {
		got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, check.version)
		if err != nil || !bytes.Equal(got, check.want) {
			t.Fatalf("%s exact bytes = %q, %v", check.name, got, err)
		}
	}
	input := attachments.ProcessorCompletionInput{Claim: *claim, Limits: attachments.DefaultLimits(), PreviewPublicationIntent: previewIntent,
		Result: attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean, HasPreview: true,
			Preview: attachments.ManagedPreview{Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8}}}
	result, err := f.repository.CompleteProcessorJob(ctx, input)
	if err != nil || result.UploadState != attachments.UploadStateAvailable {
		t.Fatalf("complete processor on RR-default pool: %#v %v", result, err)
	}
	if replay, err := f.repository.CompleteProcessorJob(ctx, input); err != nil || replay != result {
		t.Fatalf("processor completion replay on RR-default pool: %#v %v", replay, err)
	}
	var state, sourceKey, sourceVersion, previewKey, previewVersionID string
	if err := f.fixture.db.QueryRow(ctx, `select attachment_state,blob_key,blob_object_version,preview_blob_key,preview_blob_object_version
		from public.record_attachments where attachment_id=$1`, a.upload.reserve.AttachmentID).
		Scan(&state, &sourceKey, &sourceVersion, &previewKey, &previewVersionID); err != nil {
		t.Fatal(err)
	}
	if state != "available" || sourceKey != a.written.Version.Key || sourceVersion != a.written.Version.VersionID || previewKey != preview.Key || previewVersionID != preview.ObjectVersion {
		t.Fatalf("completed attachment lost exact source/preview identity: %s %s@%s %s@%s", state, sourceKey, sourceVersion, previewKey, previewVersionID)
	}
}
