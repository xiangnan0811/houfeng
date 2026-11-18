package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type localPublicationFixture struct {
	fixture    recordPlatformPostgresFixture
	pool       *pgxpool.Pool
	repository *PostgresAttachmentRepository
	local      *PostgresLocalBlobRepository
	blob       *attachments.LocalBlobStore
	root       string
}

type localPublicationAttempt struct {
	request attachments.BlobPublicationPrepareRequest
	upload  attachmentPublicationCrashUpload
	intent  attachments.BlobPublicationIntent
	written attachments.WrittenBlob
}

func newLocalPublicationFixture(t *testing.T, ctx context.Context) *localPublicationFixture {
	t.Helper()
	fixture := newRecordsPostgresFixture(t, ctx)
	return newLocalPublicationFixtureFor(t, ctx, fixture, allowRecordPlatformAdmissionGate)
}

func newLocalPublicationFixtureFor(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, gate AdmissionGate) *localPublicationFixture {
	t.Helper()
	pool := fixture.openDirectRuntimePool(t, ctx, "local-publication", 2)
	root := filepath.Join(t.TempDir(), "blob-root")
	blob, err := newPostgresLocalBlobStore(t, ctx, fixture, root)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresAttachmentRepository(pool)
	if err := repository.ConfigureRecordMutationAuthorization(NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, pool, gate)); err != nil {
		t.Fatal(err)
	}
	return &localPublicationFixture{fixture: fixture, pool: pool, repository: repository, local: NewPostgresLocalBlobRepository(pool, nil, nil), blob: blob, root: root}
}

func (f *localPublicationFixture) prepare(t *testing.T, ctx context.Context, owner string, content []byte) localPublicationAttempt {
	t.Helper()
	upload := prepareAttachmentPublicationCrashUpload(t, ctx, f.fixture, f.repository, attachmentPublicationCrashHarness{backendKind: attachments.BackendKindLocal, transportKind: attachments.TransportKindLocal}, owner, content)
	request := attachmentPublicationCrashPrepareRequest(content, owner, attachments.BackendKindLocal)
	request.NamespaceID = f.blob.Namespace().NamespaceID
	request.PublishExpiresAt = time.Now().UTC().Add(30 * time.Minute)
	intent, err := f.repository.PrepareBlobPublication(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	return localPublicationAttempt{request: request, upload: upload, intent: intent}
}

func (f *localPublicationFixture) put(t *testing.T, ctx context.Context, a *localPublicationAttempt, content []byte) {
	t.Helper()
	writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
	if err != nil {
		t.Fatal(err)
	}
	a.written, err = writer.Put(ctx, attachments.WriteRequest{ProjectID: "default", Origin: attachments.WriteOrigin{Kind: "upload", ID: a.upload.reserve.UploadID, Generation: 1, DraftID: &a.upload.reserve.DraftID, PublicationID: &a.intent.PublicationID}, Content: attachments.PutRequest{ExpectedSHA256: a.request.Target.SHA256, ExpectedSizeBytes: a.request.Target.SizeBytes}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := f.blob.ResolveBlobPublicationObject(ctx, a.request.Target)
	if err != nil || resolved != a.written.Version {
		t.Fatalf("managed publication final: %#v %v", resolved, err)
	}
}

func (f *localPublicationFixture) bind(t *testing.T, ctx context.Context, a *localPublicationAttempt) {
	t.Helper()
	published, err := f.repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{Intent: a.intent, Object: a.written.Version, ObligationID: a.written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if published.ObjectVersion != a.written.Version.VersionID || published.ObligationID != a.written.ObligationID {
		t.Fatal("publication lost exact managed write identity")
	}
	a.intent = published
}

func (f *localPublicationFixture) consume(t *testing.T, ctx context.Context, a localPublicationAttempt) {
	t.Helper()
	_, err := f.repository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{ProjectID: "default", UploadID: a.upload.reserve.UploadID, AuthorID: a.upload.reserve.AuthorID, Object: a.written.Version, PublicationIntent: a.intent})
	if err != nil {
		t.Fatal(err)
	}
}

func (f *localPublicationFixture) expire(t *testing.T, ctx context.Context, a localPublicationAttempt) {
	t.Helper()
	if _, err := f.fixture.db.Exec(ctx, `update public.blob_publication_intents set publish_expires_at=clock_timestamp()-interval '1 minute' where publication_id=$1`, a.intent.PublicationID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.fixture.db.Exec(ctx, `update public.attachment_uploads set created_at=least(created_at,clock_timestamp()-interval '2 minutes'),expires_at=clock_timestamp()-interval '1 minute' where upload_id=$1`, a.upload.reserve.UploadID); err != nil {
		t.Fatal(err)
	}
}

func (f *localPublicationFixture) discover(t *testing.T, ctx context.Context, a localPublicationAttempt, owner string) attachments.LocalCleanupPreparation {
	t.Helper()
	request, err := f.repository.DiscoverLocalPublicationCleanup(ctx, attachments.BlobPublicationCleanupClaimRequest{ProjectID: "default", BackendKind: attachments.BackendKindLocal, CleanupOwnerID: owner, OwnerLeaseDuration: attachments.DefaultBlobPublicationCleanupLeaseDuration}, f.blob.Namespace().NamespaceID)
	if err != nil || request == nil || request.PublicationID == nil || *request.PublicationID != a.intent.PublicationID {
		t.Fatalf("discover local publication: %#v %v", request, err)
	}
	return *request
}

func (f *localPublicationFixture) restart(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := f.blob.Close(); err != nil {
		t.Fatal(err)
	}
	blob, err := newPostgresLocalBlobStore(t, ctx, f.fixture, f.root)
	if err != nil {
		t.Fatal(err)
	}
	f.blob = blob
}

func (f *localPublicationFixture) cleanup(t *testing.T, ctx context.Context, request attachments.LocalCleanupPreparation, owner string) attachments.LocalCleanupTask {
	t.Helper()
	coordinator, err := attachments.NewLocalCleanupCoordinator(f.blob, f.local)
	if err != nil {
		t.Fatal(err)
	}
	task, err := coordinator.Cleanup(ctx, request, owner)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != "completed" || task.ReceiptDigest == nil || task.InventoryDigest == nil || task.Outcome == nil {
		t.Fatalf("missing completed task evidence: %#v", task)
	}
	return task
}

// Interrupt only the database boundary after a real claim or physical cleanup.
// All successful protocol operations still use the direct runtime repository.
// Separate SIGKILL tests cover actual process death at filesystem cutpoints.
type localPublicationInterruption struct {
	*PostgresLocalBlobRepository
	beforeBinding bool
	blocked       bool
	completion    attachments.LocalCleanupCompletion
}

var errLocalPublicationInterrupted = errors.New("publication database boundary unavailable")

func (r *localPublicationInterruption) BindLocalCleanupFinal(ctx context.Context, request attachments.LocalCleanupFinalBinding) (attachments.LocalCleanupSnapshot, error) {
	if r.beforeBinding {
		r.blocked = true
		return attachments.LocalCleanupSnapshot{}, errLocalPublicationInterrupted
	}
	return r.PostgresLocalBlobRepository.BindLocalCleanupFinal(ctx, request)
}
func (r *localPublicationInterruption) CompleteLocalCleanup(_ context.Context, request attachments.LocalCleanupCompletion) (attachments.LocalCleanupSnapshot, error) {
	r.completion = request
	r.blocked = true
	return attachments.LocalCleanupSnapshot{}, errLocalPublicationInterrupted
}
func (r *localPublicationInterruption) ResolveLocalCleanup(ctx context.Context, request attachments.LocalCleanupIdentity) (attachments.LocalCleanupSnapshot, error) {
	if r.blocked {
		return attachments.LocalCleanupSnapshot{}, errLocalPublicationInterrupted
	}
	return r.PostgresLocalBlobRepository.ResolveLocalCleanup(ctx, request)
}

func runLocalPublicationRestart(t *testing.T) {
	for _, boundary := range []string{"before_publish", "after_publish", "after_version_cas", "after_metadata_commit", "after_physical_cleanup"} {
		t.Run(boundary, func(t *testing.T) {
			ctx := context.Background()
			f := newLocalPublicationFixture(t, ctx)
			content := []byte("managed publication restart " + boundary)
			a := f.prepare(t, ctx, "aup_localrestart", content)
			if boundary != "before_publish" {
				f.put(t, ctx, &a, content)
			}
			if boundary == "after_version_cas" || boundary == "after_metadata_commit" || boundary == "after_physical_cleanup" {
				f.bind(t, ctx, &a)
			}
			if boundary == "after_metadata_commit" {
				f.consume(t, ctx, a)
				f.restart(t, ctx)
				assertAttachmentPublicationCrashTerminal(t, readAttachmentPublicationCrashRow(t, ctx, f.fixture, a.intent.PublicationID), attachments.BlobPublicationCompletionOutcomeConsumed, &a.written.Version.VersionID)
				assertAttachmentPublicationCrashReference(t, ctx, f.fixture, a.upload.reserve.UploadID, a.written.Version)
				got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, a.written.Version)
				if err != nil || !bytes.Equal(got, content) {
					t.Fatalf("consumed content lost on restart: %q %v", got, err)
				}
				return
			}
			f.expire(t, ctx, a)
			request := f.discover(t, ctx, a, "local_restart_first")
			var interrupted *localPublicationInterruption
			if boundary == "after_physical_cleanup" {
				interrupted = &localPublicationInterruption{PostgresLocalBlobRepository: f.local}
				coordinator, err := attachments.NewLocalCleanupCoordinator(f.blob, interrupted)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := coordinator.Cleanup(ctx, request, "local_restart_first"); !errors.Is(err, errLocalPublicationInterrupted) {
					t.Fatalf("physical completion interruption: %v", err)
				}
				assertAttachmentPublicationCrashObjectAbsent(t, ctx, f.blob, a.written.Version)
				var claimed bool
				if err := f.fixture.db.QueryRow(ctx, `select deletion_state='claimed' and receipt_digest is null and owner_id=$2 and owner_generation=$3 from public.blob_gc_deletions where deletion_id=$1`, request.DeletionID, "local_restart_first", interrupted.completion.Generation).Scan(&claimed); err != nil || !claimed {
					t.Fatalf("physical absence prematurely completed: %t %v", claimed, err)
				}
				owner := f.fixture.openDirectRolePool(t, ctx, f.fixture.migrator, "local-publication-expiry-owner", 1)
				if _, err := owner.Exec(ctx, `update public.blob_gc_deletions set lease_expires_at=clock_timestamp()-interval '1 second' where deletion_id=$1`, request.DeletionID); err != nil {
					t.Fatal(err)
				}
			}
			f.restart(t, ctx)
			task := f.cleanup(t, ctx, request, "local_restart_second")
			want := "deleted"
			if boundary == "before_publish" || boundary == "after_physical_cleanup" {
				want = "already_absent"
			}
			if *task.Outcome != want {
				t.Fatalf("cleanup outcome %q, want %q", *task.Outcome, want)
			}
			var version *string
			if boundary != "before_publish" {
				version = &a.written.Version.VersionID
			}
			assertAttachmentPublicationCrashTerminal(t, readAttachmentPublicationCrashRow(t, ctx, f.fixture, a.intent.PublicationID), attachments.BlobPublicationCompletionOutcome(want), version)
			assertAttachmentPublicationCrashNoReferences(t, ctx, f.fixture, a.request.Target, "")
			if _, err := f.blob.ResolveBlobPublicationObject(ctx, a.request.Target); !errors.Is(err, attachments.ErrBlobNotFound) {
				t.Fatalf("final key not absent: %v", err)
			}
			if boundary != "before_publish" {
				var settled bool
				if err := f.fixture.db.QueryRow(ctx, `select cleanup_outcome=$2 and cleanup_receipt is not null and quota_settled_at is not null from public.record_write_obligations where obligation_id=$1`, a.written.ObligationID, want).Scan(&settled); err != nil || !settled {
					t.Fatalf("managed write not settled: %t %v", settled, err)
				}
			}
			assertNoLocalBlobTemporaryResidue(t, f.root)
			replay := f.cleanup(t, ctx, request, "local_restart_second")
			if *replay.ReceiptDigest != *task.ReceiptDigest || *replay.Generation != *task.Generation {
				t.Fatal("cleanup replay changed receipt or owner generation")
			}
			if interrupted != nil {
				if *task.Generation <= interrupted.completion.Generation {
					t.Fatal("takeover reused old owner generation")
				}
				if _, err := f.local.CompleteLocalCleanup(ctx, interrupted.completion); err == nil {
					t.Fatal("stale physical-delete owner completed after takeover")
				}
			}
		})
	}
	t.Run("prepared_unbound_shared_object_survives", func(t *testing.T) { runLocalPublicationShared(t, false) })
}

func runLocalPublicationShared(t *testing.T, part bool) {
	t.Helper()
	ctx := context.Background()
	f := newLocalPublicationFixture(t, ctx)
	content := []byte("shared local publication remains retained")
	var version attachments.ObjectVersion
	if part {
		first := f.prepare(t, ctx, "aup_localretained", content)
		f.put(t, ctx, &first, content)
		f.bind(t, ctx, &first)
		f.consume(t, ctx, first)
		version = first.written.Version
	} else {
		version = seedPostgresManagedLocalBlob(t, ctx, f.fixture, f.blob, content).Version
		object := attachmentBlobObjectFromVersion(version, attachments.BackendKindLocal)
		object.NamespaceID = f.blob.Namespace().NamespaceID
		seedAttachmentBlobGCObject(t, ctx, f.fixture, object, time.Now().UTC())
		seedAttachmentBlobGCOriginalReference(t, ctx, f.fixture, "localretained", object)
		if _, err := f.repository.CreateBlobGCPin(ctx, attachments.CreateBlobGCPinCommand{PinID: "bgp_localretained", OwnerKind: attachments.BlobGCPinOwnerBackupManifest, OwnerID: "backup_localretained", BlobKey: version.Key, BlobObjectVersion: version.VersionID, ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	a := f.prepare(t, ctx, "aup_localshared", content)
	f.put(t, ctx, &a, content)
	if a.written.Version != version {
		t.Fatal("managed same-key write changed the retained materialization")
	}
	before := observeAttachmentPublicationCrashObject(t, ctx, f.fixture, "before local shared cleanup", a.intent, version)
	if !before.publicationMatches || before.ownerParts != 0 || before.revisionRefs != 0 || (part && (before.parts != 1 || before.metadataCount != 0 || before.logicalRefs != 0 || before.activePins != 0)) || (!part && (before.parts != 0 || before.metadataCount != 1 || before.exactMetadataCount != 1 || before.logicalRefs != 1 || before.activePins != 1)) {
		t.Fatalf("invalid shared baseline: %+v", before)
	}
	var physical, quota int64
	if err := f.fixture.db.QueryRow(ctx, `select physical_bytes,quota_version from public.attachment_quota_accounts where project_id='default'`).Scan(&physical, &quota); err != nil {
		t.Fatal(err)
	}
	f.expire(t, ctx, a)
	request := f.discover(t, ctx, a, "local_shared_cleanup")
	f.restart(t, ctx)
	task := f.cleanup(t, ctx, request, "local_shared_cleanup")
	after := observeAttachmentPublicationCrashObject(t, ctx, f.fixture, "after local shared cleanup", a.intent, version)
	got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, version)
	if *task.Outcome != "retained_referenced" || after != before || err != nil || !bytes.Equal(got, content) {
		t.Fatalf("shared bytes/references lost: task=%#v after=%+v bytes=%q err=%v", task, after, got, err)
	}
	var afterPhysical, afterQuota int64
	var settled bool
	if err := f.fixture.db.QueryRow(ctx, `select physical_bytes,quota_version,exists(select 1 from public.record_write_obligations where obligation_id=$1 and cleanup_outcome='retained_referenced' and cleanup_completed_at is not null and quota_settled_at is null) from public.attachment_quota_accounts where project_id='default'`, a.written.ObligationID).Scan(&afterPhysical, &afterQuota, &settled); err != nil || !settled || physical != afterPhysical || quota != afterQuota {
		t.Fatalf("retained responsibility changed physical quota: %d/%d -> %d/%d, settled=%t err=%v", physical, quota, afterPhysical, afterQuota, settled, err)
	}
	assertNoLocalBlobTemporaryResidue(t, f.root)
}

func runLocalPublicationReferenceFences(t *testing.T) {
	for _, bound := range []bool{false, true} {
		for _, state := range []string{"cleanup_claimed", "expired_cleanup_claimed", "retry_wait"} {
			t.Run(fmt.Sprintf("bound_%t/%s", bound, state), func(t *testing.T) {
				ctx := context.Background()
				f := newLocalPublicationFixture(t, ctx)
				content := []byte("managed late reference " + t.Name())
				retained := f.prepare(t, ctx, "aup_localretained", content)
				f.put(t, ctx, &retained, content)
				f.bind(t, ctx, &retained)
				f.consume(t, ctx, retained)
				a := f.prepare(t, ctx, "aup_locallate", content)
				f.put(t, ctx, &a, content)
				if bound {
					f.bind(t, ctx, &a)
				}
				f.expire(t, ctx, a)
				request := f.discover(t, ctx, a, "local_late_reference")
				interruption := &localPublicationInterruption{PostgresLocalBlobRepository: f.local, beforeBinding: true}
				coordinator, err := attachments.NewLocalCleanupCoordinator(f.blob, interruption)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := coordinator.Cleanup(ctx, request, "local_late_reference"); !errors.Is(err, errLocalPublicationInterrupted) || !interruption.blocked {
					t.Fatalf("claim boundary not reached: %v", err)
				}
				switch state {
				case "expired_cleanup_claimed":
					owner := f.fixture.openDirectRolePool(t, ctx, f.fixture.migrator, "local-publication-claim-owner", 1)
					if _, err := owner.Exec(ctx, `update public.blob_gc_deletions set lease_expires_at=clock_timestamp()-interval '1 second' where deletion_id=$1`, request.DeletionID); err != nil {
						t.Fatal(err)
					}
				case "retry_wait":
					owner := f.fixture.openDirectRolePool(t, ctx, f.fixture.migrator, "local-publication-retry-owner", 1)
					if _, err := owner.Exec(ctx, `update public.blob_gc_deletions set deletion_state='retry_wait',retry_at=clock_timestamp()+interval '1 hour' where deletion_id=$1`, request.DeletionID); err != nil {
						t.Fatal(err)
					}
				}
				var before, after string
				if err := f.fixture.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, request.DeletionID).Scan(&before); err != nil {
					t.Fatal(err)
				}
				for _, surface := range []string{"metadata", "upload_part"} {
					tx, err := f.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
					if err != nil {
						t.Fatal(err)
					}
					var physical int64
					var publishErr error
					if surface == "metadata" {
						physical, publishErr = registerDraftUploadPartBlobs(ctx, tx, retained.upload.reserve.DraftID)
					} else {
						_, publishErr = tx.Exec(ctx, `select public.record_upload_part_publish(convert_to(jsonb_build_object('publication_id',$1::text,'obligation_id',$2::text,'upload_id',$3::text)::text,'UTF8'))`, a.intent.PublicationID, a.written.ObligationID, a.upload.reserve.UploadID)
					}
					rollbackErr := tx.Rollback(ctx)
					if physical != 0 || publishErr == nil || rollbackErr != nil {
						t.Fatalf("late %s: physical=%d error=%v rollback=%v", surface, physical, publishErr, rollbackErr)
					}
					if surface == "metadata" && !errors.Is(publishErr, records.ErrDraftAttachmentsBusy) {
						t.Fatalf("draft metadata did not observe cleanup fence: %v", publishErr)
					}
				}
				if err := f.fixture.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, request.DeletionID).Scan(&after); err != nil {
					t.Fatal(err)
				}
				assertAttachmentPublicationCrashReference(t, ctx, f.fixture, retained.upload.reserve.UploadID, retained.written.Version)
				var newParts int
				if err := f.fixture.db.QueryRow(ctx, `select count(*) from public.attachment_upload_parts where upload_id=$1`, a.upload.reserve.UploadID).Scan(&newParts); err != nil || newParts != 0 {
					t.Fatalf("late owner gained parts: %d %v", newParts, err)
				}
				_, rawErr := f.pool.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id) values($1,$2,$3,$4,'local',$5::uuid)`, a.written.Version.Key, a.written.Version.SHA256[:], a.written.Version.VersionID, a.written.Version.SizeBytes, a.written.NamespaceID)
				var permissionError *pgconn.PgError
				if !errors.As(rawErr, &permissionError) || permissionError.Code != "42501" {
					t.Fatalf("raw metadata insertion was not denied by SQL permissions: %v", rawErr)
				}
				got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, a.written.Version)
				if before != after || err != nil || !bytes.Equal(got, content) {
					t.Fatalf("late publication changed durable claim or bytes: %q %v", got, err)
				}
			})
		}
	}
	t.Run("prepared_unbound_shared_upload_part", func(t *testing.T) { runLocalPublicationShared(t, true) })
	for _, surface := range []string{"metadata", "upload_part"} {
		t.Run("repeatable_read_pool_late_"+surface, func(t *testing.T) { runAttachmentPublicationCrashReferenceLockWait(t, surface) })
	}
}

type localPublicationClaimBarrier struct {
	*localPublicationInterruption
	entered chan struct{}
	release chan struct{}
}

func (r *localPublicationClaimBarrier) ClaimLocalCleanup(ctx context.Context, request attachments.LocalCleanupClaim) (attachments.LocalCleanupSnapshot, error) {
	var snapshot attachments.LocalCleanupSnapshot
	body, err := json.Marshal(request)
	if err != nil {
		return snapshot, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return snapshot, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var encoded []byte
	if err := tx.QueryRow(ctx, `select public.local_blob_gc_claim($1)`, body).Scan(&encoded); err != nil {
		return snapshot, err
	}
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return snapshot, err
	}
	if err := normalizeLocalCleanupDigests(&snapshot.Task); err != nil {
		return snapshot, err
	}
	close(r.entered)
	select {
	case <-r.release:
	case <-ctx.Done():
		return snapshot, ctx.Err()
	}
	if err := tx.Commit(ctx); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func TestPostgresIntegrationLocalPublicationConsumptionRollbackAndReplay(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprintf("complete_%t", complete), func(t *testing.T) {
			ctx := context.Background()
			f := newLocalPublicationFixture(t, ctx)
			content := []byte("atomic managed publication consumption")
			a := f.prepare(t, ctx, "aup_consumeatomic", content)
			f.put(t, ctx, &a, content)
			f.bind(t, ctx, &a)
			consume := func(intent attachments.BlobPublicationIntent) error {
				if complete {
					_, err := f.repository.CompleteUploadAndEnqueue(ctx, attachments.CompleteUploadAndEnqueueCommand{
						ProjectID: "default", UploadID: a.upload.reserve.UploadID, AuthorID: a.upload.reserve.AuthorID,
						ActualSizeBytes: a.written.Version.SizeBytes, ActualSHA256: a.written.Version.SHA256, Object: a.written.Version,
						CompletionFingerprint: a.written.Version.SHA256, PublicationIntent: intent,
						ProcessorJobID: "apj_consumeatomic", ProcessorProfile: attachments.ProcessorProfileText,
						ProcessorMaxAttempts: 3, ProcessorExpiresAt: a.upload.reserve.ExpiresAt,
					})
					return err
				}
				_, err := f.repository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{
					ProjectID: "default", UploadID: a.upload.reserve.UploadID, AuthorID: a.upload.reserve.AuthorID,
					Object: a.written.Version, PublicationIntent: intent,
				})
				return err
			}
			assertUnconsumed := func() {
				t.Helper()
				var parts, jobs int
				var state string
				if err := f.fixture.db.QueryRow(ctx, `select publication_state,
     (select count(*)::int from public.attachment_upload_parts where upload_id=$2),
     (select count(*)::int from public.attachment_processor_jobs where upload_id=$2)
     from public.blob_publication_intents where publication_id=$1`, a.intent.PublicationID, a.upload.reserve.UploadID).Scan(&state, &parts, &jobs); err != nil {
					t.Fatal(err)
				}
				if state != "published" || parts != 0 || jobs != 0 {
					t.Fatalf("failed consumption leaked state: %s parts=%d jobs=%d", state, parts, jobs)
				}
			}
			if err := consume(attachments.BlobPublicationIntent{}); err == nil {
				t.Fatal("missing publication was accepted")
			}
			assertUnconsumed()
			if _, err := f.fixture.db.Exec(ctx, `create function public.fail_publication_consume() returns trigger language plpgsql as $$
    begin raise exception 'publication consume cutpoint'; end $$;
    create trigger fail_publication_consume before update on public.blob_publication_intents
    for each row when (new.publication_state='completed') execute function public.fail_publication_consume()`); err != nil {
				t.Fatal(err)
			}
			if err := consume(a.intent); err == nil || !strings.Contains(err.Error(), "publication consume cutpoint") {
				t.Fatalf("missing injected failure: %v", err)
			}
			assertUnconsumed()
			if _, err := f.fixture.db.Exec(ctx, `drop trigger fail_publication_consume on public.blob_publication_intents; drop function public.fail_publication_consume()`); err != nil {
				t.Fatal(err)
			}
			if err := consume(a.intent); err != nil {
				t.Fatal(err)
			}
			if err := consume(a.intent); err != nil {
				t.Fatal(err)
			}
			var obligation string
			var jobs int
			if err := f.fixture.db.QueryRow(ctx, `select obligation_id,(select count(*)::int from public.attachment_processor_jobs where upload_id=$1)
    from public.attachment_upload_parts where upload_id=$1 and part_number=1`, a.upload.reserve.UploadID).Scan(&obligation, &jobs); err != nil {
				t.Fatal(err)
			}
			wantJobs := 0
			if complete {
				wantJobs = 1
			}
			if obligation != a.written.ObligationID || jobs != wantJobs {
				t.Fatalf("consumed provenance/jobs = %s/%d", obligation, jobs)
			}
			got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, a.written.Version)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("consumption changed bytes: %q %v", got, err)
			}
		})
	}
}

func TestPostgresIntegrationLocalPreviewTakeoverRetainsExactPublishedVersion(t *testing.T) {
	ctx := context.Background()
	f := newLocalPublicationFixture(t, ctx)
	writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
	if err != nil {
		t.Fatal(err)
	}
	seed := seedManagedAttachmentProcessor(t, ctx, f.fixture, f.repository, writer, "previewtakeover", []byte("preview takeover source"))
	first, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "preview_old_owner", OwnerLeaseDuration: time.Minute})
	if err != nil || first == nil {
		t.Fatalf("first claim: %#v %v", first, err)
	}
	content := []byte("retained takeover preview")
	preview, oldIntent := prepareManagedAttachmentPreview(t, ctx, f.repository, writer, *first, content)
	if _, err := f.fixture.db.Exec(ctx, `update public.attachment_processor_jobs set lease_expires_at=clock_timestamp()-interval '1 second' where processor_job_id=$1`, seed.ProcessorJobID); err != nil {
		t.Fatal(err)
	}
	second, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "preview_new_owner", OwnerLeaseDuration: time.Minute})
	if err != nil || second == nil || second.OwnerGeneration <= first.OwnerGeneration {
		t.Fatalf("takeover claim: %#v %v", second, err)
	}
	request := attachments.BlobPublicationPrepareRequest{ProjectID: "default", NamespaceID: writer.NamespaceID(), OwnerKind: attachments.BlobPublicationOwnerProcessorPreview,
		OwnerID: second.ProcessorJobID, OwnerGeneration: second.OwnerGeneration, ClaimOwnerID: second.OwnerID, ClaimExpiresAt: second.LeaseExpiresAt,
		Target: oldIntent.Target, PublishExpiresAt: second.ExpiresAt}
	rebound, err := f.repository.PrepareBlobPublication(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.PublicationID != oldIntent.PublicationID || rebound.ObjectVersion != oldIntent.ObjectVersion || rebound.ObligationID != oldIntent.ObligationID || rebound.OwnerGeneration != second.OwnerGeneration {
		t.Fatalf("takeover lost exact published identity: %#v", rebound)
	}
	object := attachments.ObjectVersion{Key: preview.Key, VersionID: preview.ObjectVersion, SHA256: preview.SHA256, SizeBytes: preview.SizeBytes}
	prepared := oldIntent
	prepared.State, prepared.ObjectVersion, prepared.ObligationID = attachments.BlobPublicationStatePrepared, "", ""
	if _, err := f.repository.RecordBlobPublicationVersion(ctx, attachments.BlobPublicationVersionRequest{Intent: prepared, Object: object, ObligationID: oldIntent.ObligationID}); !errors.Is(err, attachments.ErrBlobPublicationConflict) {
		t.Fatalf("stale publisher: %v", err)
	}
	result := attachments.ProcessorResult{Source: second.Source, Profile: second.Profile, Code: attachments.ProcessorResultCodeClean, HasPreview: true, Preview: attachments.ManagedPreview{Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8}}
	if _, err := f.repository.CompleteProcessorJob(ctx, attachments.ProcessorCompletionInput{Claim: *first, Result: result, PreviewPublicationIntent: oldIntent, Limits: attachments.DefaultLimits()}); err == nil {
		t.Fatal("stale processor completed")
	}
	input := attachments.ProcessorCompletionInput{Claim: *second, Result: result, PreviewPublicationIntent: rebound, Limits: attachments.DefaultLimits()}
	completed, err := f.repository.CompleteProcessorJob(ctx, input)
	if err != nil || completed.UploadState != attachments.UploadStateAvailable {
		t.Fatalf("current completion: %#v %v", completed, err)
	}
	replay, err := f.repository.CompleteProcessorJob(ctx, input)
	if err != nil || replay != completed {
		t.Fatalf("current replay: %#v %v", replay, err)
	}
	got, err := readAttachmentPublicationCrashBytes(ctx, f.blob, object)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("takeover changed preview bytes: %q %v", got, err)
	}
}

func TestPostgresIntegrationLocalCleanupDefersToLiveUnpublishedWriter(t *testing.T) {
	ctx := context.Background()
	f := newLocalPublicationFixture(t, ctx)
	content := []byte("live publication shares an expired attempt")
	seedPostgresManagedLocalBlob(t, ctx, f.fixture, f.blob, content)
	live := f.prepare(t, ctx, "aup_cleanuplive", content)
	f.put(t, ctx, &live, content)
	f.bind(t, ctx, &live)
	namespace := f.blob.Namespace()
	request := attachments.LocalCleanupPreparation{DeletionID: "bgd_cleanupdefers", NamespaceID: namespace.NamespaceID, IncarnationID: namespace.IncarnationID, Key: live.written.Version.Key}
	coordinator, err := attachments.NewLocalCleanupCoordinator(f.blob, f.local)
	if err != nil {
		t.Fatal(err)
	}
	_, cleanupErr := coordinator.Cleanup(ctx, request, "cleanup_defers")
	var conflict *pgconn.PgError
	if !errors.As(cleanupErr, &conflict) || conflict.Code != "55000" || conflict.Message != "writable responsibility prevents local GC" {
		t.Fatalf("live writer cleanup refusal: %v", cleanupErr)
	}
	var active int
	if err := f.pool.QueryRow(ctx, `select count(*) from public.blob_gc_deletions where blob_key=$1 and deletion_state<>'completed'`, live.written.Version.Key).Scan(&active); err != nil || active != 0 {
		t.Fatalf("refused cleanup left %d active tasks: %v", active, err)
	}
	if _, err := f.repository.RecordUploadedContent(ctx, attachments.RecordUploadedContentCommand{ProjectID: "default", UploadID: live.upload.reserve.UploadID, AuthorID: live.upload.reserve.AuthorID, Object: live.written.Version, PublicationIntent: live.intent}); err != nil {
		t.Fatalf("refused cleanup left a fence blocking the live publisher: %v", err)
	}
	assertAttachmentPublicationCrashReference(t, ctx, f.fixture, live.upload.reserve.UploadID, live.written.Version)
	actual, err := readAttachmentPublicationCrashBytes(ctx, f.blob, live.written.Version)
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatalf("live publication bytes changed: %q %v", actual, err)
	}
}

func TestPostgresIntegrationProcessorCompletionLocksBothObligationsBeforePublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	f := newLocalPublicationFixture(t, ctx)
	writer, err := attachments.NewManagedBlobWriter(f.blob, attachments.BackendKindLocal, f.local)
	if err != nil {
		t.Fatal(err)
	}
	seed := seedManagedAttachmentProcessor(t, ctx, f.fixture, f.repository, writer, "obligationlocks", []byte("processor source for lock ordering"))
	claim, err := f.repository.ClaimProcessorJob(ctx, attachments.ProcessorClaimInput{OwnerID: "processor_obligation_test", OwnerLeaseDuration: 5 * time.Minute})
	if err != nil || claim == nil {
		t.Fatalf("claim processor: %#v %v", claim, err)
	}
	preview, intent := prepareManagedAttachmentPreview(t, ctx, f.repository, writer, *claim, []byte("processor preview for lock ordering"))
	var sourceID string
	if err := f.fixture.db.QueryRow(ctx, `select obligation_id from public.attachment_upload_parts where upload_id=$1 and part_number=1`, seed.UploadID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	ids := []string{sourceID, intent.ObligationID}
	if ids[0] == ids[1] || ids[0] == "" || ids[1] == "" {
		t.Fatalf("source and preview need separate obligations: %#v", ids)
	}
	sort.Strings(ids)
	first, err := f.fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Rollback(context.Background()) }()
	second, err := f.fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Rollback(context.Background()) }()
	var firstPID, secondPID int32
	if err := first.QueryRow(ctx, `select pg_backend_pid()`).Scan(&firstPID); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRow(ctx, `select pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Exec(ctx, `select 1 from public.record_write_obligations where obligation_id=$1 for update`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Exec(ctx, `select 1 from public.record_write_obligations where obligation_id=$1 for update`, ids[1]); err != nil {
		t.Fatal(err)
	}
	input := attachments.ProcessorCompletionInput{Claim: *claim, Limits: attachments.DefaultLimits(), PreviewPublicationIntent: intent,
		Result: attachments.ProcessorResult{Source: claim.Source, Profile: claim.Profile, Code: attachments.ProcessorResultCodeClean,
			HasPreview: true, Preview: attachments.ManagedPreview{Blob: preview, MediaType: attachments.ManagedPreviewMediaTypeTextUTF8}}}
	type outcome struct {
		result attachments.ProcessorCompletionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := f.repository.CompleteProcessorJob(ctx, input)
		done <- outcome{result: result, err: err}
	}()
	waitForRecordPlatformBackendLock(t, ctx, f.fixture.db, "local-publication")
	var workerPID int32
	if err := f.fixture.db.QueryRow(ctx, `select pid from pg_stat_activity where application_name='local-publication' and wait_event_type='Lock' order by pid limit 1`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	waitForPostgresCollaborationBlocker(t, ctx, f.fixture, workerPID, firstPID)
	if _, err := second.Exec(ctx, `select 1 from public.blob_publication_intents where publication_id=$1 for update nowait`, intent.PublicationID); err != nil {
		t.Fatalf("publication locked before the obligations: %v", err)
	}
	if err := first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	waitForPostgresCollaborationBlocker(t, ctx, f.fixture, workerPID, secondPID)
	if err := second.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got := waitForRecordPlatformResult(t, done, "processor obligation locks")
	if got.err != nil || got.result.UploadState != attachments.UploadStateAvailable {
		t.Fatalf("processor completion after both obligation locks: %#v %v", got.result, got.err)
	}
	if replay, err := f.repository.CompleteProcessorJob(ctx, input); err != nil || replay != got.result {
		t.Fatalf("processor completion replay: %#v %v", replay, err)
	}
}
