package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	storemigrate "houfeng/internal/center/store/migrate"
)

// This test exercises only rejection at the successor's direct-role boundary.
// The deliberately fabricated APP projection is an attack fixture: it is not
// a witnessed ledger entry and must never be treated as a successful V1 purge.
func TestPostgresIntegrationLegacyAdaptersRejectUntrustedAuthority(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRolePool(t, ctx, fixture.runtime, "legacy-adapter-runtime", 1)
	admin := fixture.openDirectRolePool(t, ctx, fixture.admin, "legacy-adapter-admin", 1)
	worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "legacy-adapter-worker", 1)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
		t.Fatalf("admit direct deletion worker: %v", err)
	}
	var sourceGeneration int64
	if err := worker.QueryRow(ctx, `select public.source_application_admit()`).Scan(&sourceGeneration); err != nil {
		t.Fatalf("admit worker source access: %v", err)
	}

	commands := []struct {
		name    string
		surface [sha256.Size]byte
		payload bool
		gc      bool
	}{
		{"record_activity_purge_v1", recorddeletion.RecordActivitySurfaceDigest(), false, false},
		{"record_activity_purge_verify_v1", recorddeletion.RecordActivitySurfaceDigest(), false, false},
		{"record_search_purge_v1", recorddeletion.RecordSearchSurfaceDigest(), false, false},
		{"record_search_purge_verify_v1", recorddeletion.RecordSearchSurfaceDigest(), false, false},
		{"record_evidence_purge_prepare_v1", recorddeletion.RecordEvidenceSurfaceDigest(), false, false},
		{"record_evidence_purge_payload_v1", recorddeletion.RecordEvidenceSurfaceDigest(), true, false},
		{"record_evidence_purge_v1", recorddeletion.RecordEvidenceSurfaceDigest(), false, false},
		{"record_evidence_purge_verify_v1", recorddeletion.RecordEvidenceSurfaceDigest(), false, false},
		{"record_attachment_purge_prepare_v1", recorddeletion.RecordAttachmentsSurfaceDigest(), false, false},
		{"record_attachment_purge_verify_v1", recorddeletion.RecordAttachmentsSurfaceDigest(), false, false},
		{"record_attachment_gc_claim_v1", recorddeletion.RecordAttachmentsSurfaceDigest(), false, true},
		{"record_attachment_gc_complete_v1", recorddeletion.RecordAttachmentsSurfaceDigest(), false, true},
		{"record_attachment_gc_retry_v1", recorddeletion.RecordAttachmentsSurfaceDigest(), false, true},
		{"record_attachment_gc_resolve_v1", recorddeletion.RecordAttachmentsSurfaceDigest(), false, true},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			query := `select * from public.` + command.name + `($1::bytea)`
			for _, caller := range []struct {
				name string
				pool *pgxpool.Pool
			}{{"runtime", runtime}, {"admin", admin}} {
				_, err := caller.pool.Exec(ctx, query, []byte(`{}`))
				assertLegacyAdapterRejection(t, err, "42501", "")
			}
			_, err := worker.Exec(ctx, query, []byte(`{}`))
			assertLegacyAdapterRejection(t, err, "22023", "")
		})
	}
	for _, name := range []string{"record_activity_purge", "record_search_purge"} {
		_, err := runtime.Exec(ctx, `select public.`+name+`($1::bytea)`, []byte(`{}`))
		assertLegacyAdapterRejection(t, err, "42501", "")
	}
	for _, table := range []string{"record_activity_purge_receipts", "record_search_purge_receipts", "evidence_purge_receipts"} {
		for _, caller := range []*pgxpool.Pool{runtime, worker} {
			_, err := caller.Exec(ctx, `insert into public.`+table+` default values`)
			assertLegacyAdapterRejection(t, err, "42501", "")
		}
	}
	for _, table := range []string{"record_deletion_verified_entries", "record_deletion_effect_scopes", "record_deletion_owner_commit_checks"} {
		for _, caller := range []*pgxpool.Pool{runtime, worker} {
			_, err := caller.Exec(ctx, `insert into record_platform_internal.`+table+` default values`)
			assertLegacyAdapterRejection(t, err, "42501", "")
		}
	}
	_, err := worker.Exec(ctx, `select public.record_deletion_verify_legacy_entry($1::bytea)`, []byte(`{}`))
	assertLegacyAdapterRejection(t, err, "42501", "")

	// Superuser fixture writes below model a forged APP projection. In particular,
	// the synthetic ledger hash and witness digest have never been installed by
	// the independent verifier. Even an exact match to this projection must fail.
	parent := seedRecordWithoutSearchProjection(t, ctx, fixture, "legacy adapter rejection target")
	contentDigest := sha256.Sum256([]byte("untrusted projection content"))
	if _, err := fixture.db.Exec(ctx, `insert into public.record_search_generations(generation,generation_state,published_at)
		values(1,'published',transaction_timestamp())`); err != nil {
		t.Fatalf("seed search generation: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_search_documents
		(generation,record_id,current_revision_id,record_lock_version,authorization_epoch,record_fence_epoch,
		 lifecycle,record_type,impact_level,title,plain_text,visibility_kind,visibility_digest,
		 record_created_at,record_updated_at,document_digest)
		values(1,$1,$2,1,1,0,'active','note','informational','protected search','private content',
		 'project',$3,transaction_timestamp(),transaction_timestamp(),$3)`, parent.RecordID, parent.RevisionID, contentDigest[:]); err != nil {
		t.Fatalf("seed protected search document: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_activity_projection_heads
		(project_id,projection_generation,head_state) values('default',1,'active')`); err != nil {
		t.Fatalf("seed activity projection head: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_activity_projection
		(activity_id,project_id,projection_generation,ingest_sequence,event_kind,event_at,recorded_at,
		 source_kind,source_event_id,source_version,record_id,revision_id,presentation_version,
		 presentation_json,auth_scope_digest,canonical_hash)
		values('act_legacyforged','default',1,1,'record_created',transaction_timestamp(),transaction_timestamp(),
		 'record_domain','event_legacyforged',1,$1,$2,1,'{}'::jsonb,$3,$3)`, parent.RecordID, parent.RevisionID, contentDigest[:]); err != nil {
		t.Fatalf("seed protected activity: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.evidence_capture_intents
		(intent_id,record_id,kind,schema_version,preview_digest,source_digest,selection,preview,snapshot_id,
		 estimated_size_bytes,created_at,valid_until)
		values('evi_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'monitoring.test',1,$2,$2,'{}'::jsonb,'{}'::jsonb,
		 'evs_legacyforged',1,transaction_timestamp(),transaction_timestamp()+interval '15 minutes')`, parent.RecordID, contentDigest[:]); err != nil {
		t.Fatalf("seed protected evidence intent: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.content_delivery_epochs(project_id,object_kind,object_id,delivery_epoch)
		values('default','record',$1,7) on conflict(project_id,object_kind,object_id) do update set delivery_epoch=excluded.delivery_epoch`, parent.RecordID); err != nil {
		t.Fatalf("seed forged operation fence epoch: %v", err)
	}
	operation := recorddeletion.DeletionOperation{
		ContractVersion: recorddeletion.DeletionContractV1,
		OperationID:     "rpo_legacyforged",
		ReservationID:   "drs_legacyforged",
		Object: recordplatform.ObjectRef{
			ProjectID: "default", ObjectKind: "record", ObjectID: parent.RecordID,
		},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed,
		State:      recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7, LedgerSequence: 11,
		LedgerEntryHash: sha256.Sum256([]byte("untrusted projected ledger entry")),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, parent.RevisionID)
	var ownerID string
	var ownerGeneration int64
	var ownerExpiry time.Time
	var witnessDigest []byte
	if err := fixture.db.QueryRow(ctx, `select owner_id,owner_generation,owner_expires_at,witness_proof_digest
		from public.record_purge_operations where operation_id=$1`, operation.OperationID).
		Scan(&ownerID, &ownerGeneration, &ownerExpiry, &witnessDigest); err != nil {
		t.Fatalf("read forged projection identity: %v", err)
	}
	var installed int
	if err := fixture.db.QueryRow(ctx, `select count(*) from record_platform_internal.record_deletion_verified_entries`).Scan(&installed); err != nil || installed != 0 {
		t.Fatalf("verified entries before forged calls = %d, error = %v; want none", installed, err)
	}
	domain := sha256.Sum256([]byte("untrusted ledger domain"))
	for _, command := range commands {
		t.Run(command.name+"_forged_projection", func(t *testing.T) {
			envelope := corePurgeV1Envelope{
				clientComparisonPurgeEnvelope: clientComparisonPurgeEnvelope{
					OperationID: operation.OperationID, RecordID: operation.Object.ObjectID,
					ReservationID: operation.ReservationID, FenceEpoch: uint64(operation.FenceEpoch),
					OwnerID: ownerID, OwnerGeneration: uint64(ownerGeneration),
					OwnerExpiresAt: ownerExpiry.UTC().Format(time.RFC3339Nano), SurfaceDigest: hex.EncodeToString(command.surface[:]),
				},
				LedgerDomain: hex.EncodeToString(domain[:]), LedgerSequence: operation.LedgerSequence,
				LedgerEntryHash: hex.EncodeToString(operation.LedgerEntryHash[:]), WitnessProofDigest: hex.EncodeToString(witnessDigest),
			}
			call := func(candidate corePurgeV1Envelope) error {
				body, err := encodeRecordPurgeCommand(candidate)
				if err != nil {
					t.Fatalf("encode legacy adapter envelope: %v", err)
				}
				if command.payload {
					body, err = encodeRecordPurgeCommand(struct {
						Authority  []byte `json:"authority"`
						SnapshotID string `json:"snapshot_id"`
					}{body, "evs_legacyforged"})
					if err != nil {
						t.Fatalf("encode legacy evidence payload envelope: %v", err)
					}
				}
				if command.gc {
					request := map[string]any{"authority": candidate}
					switch command.name {
					case "record_attachment_gc_claim_v1":
						request["deletion_id"], request["cleanup_owner_id"], request["cleanup_lease_ms"] = "bgd_denied", "denied_worker", 60000
					case "record_attachment_gc_retry_v1":
						request["claim"], request["retry_at"] = map[string]any{}, ownerExpiry.UTC().Format(time.RFC3339Nano)
					default:
						request["claim"], request["receipt"] = map[string]any{}, map[string]any{}
					}
					body, err = encodeRecordPurgeCommand(request)
					if err != nil {
						t.Fatal(err)
					}
				}
				_, err = worker.Exec(ctx, `select * from public.`+command.name+`($1::bytea)`, body)
				return err
			}
			wrongOwner := envelope
			wrongOwner.OwnerID = "another_worker"
			assertLegacyAdapterRejection(t, call(wrongOwner), "42501", "legacy adapter caller owner unavailable")
			wrongGeneration := envelope
			wrongGeneration.OwnerGeneration++
			assertLegacyAdapterRejection(t, call(wrongGeneration), "42501", "legacy adapter caller owner unavailable")
			wrongExpiry := envelope
			wrongExpiry.OwnerExpiresAt = ownerExpiry.Add(time.Microsecond).UTC().Format(time.RFC3339Nano)
			assertLegacyAdapterRejection(t, call(wrongExpiry), "42501", "legacy adapter caller owner unavailable")
			wrongSurface := envelope
			wrongSurface.SurfaceDigest = hex.EncodeToString(domain[:])
			assertLegacyAdapterRejection(t, call(wrongSurface), "22023", "invalid legacy adapter identity")
			submicrosecondExpiry := envelope
			submicrosecondExpiry.OwnerExpiresAt = ownerExpiry.Add(time.Nanosecond).UTC().Format(time.RFC3339Nano)
			assertLegacyAdapterRejection(t, call(submicrosecondExpiry), "22023", "invalid legacy adapter identity")
			assertLegacyAdapterRejection(t, call(envelope), "P0002", "query returned no rows")
		})
	}
	var operationCount, activityReceipts, searchReceipts, evidenceReceipts, scopes int
	var activityRows, searchRows, evidenceRows int
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.record_purge_operations where operation_id=$1 and operation_state='online_purging'),
		(select count(*) from public.record_activity_purge_receipts where operation_id=$1),
		(select count(*) from public.record_search_purge_receipts where operation_id=$1),
		(select count(*) from public.evidence_purge_receipts where operation_id=$1),
		(select count(*) from record_platform_internal.record_deletion_effect_scopes where operation_id=$1),
		(select count(*) from public.record_activity_projection where record_id=$2),
		(select count(*) from public.record_search_documents where record_id=$2),
		(select count(*) from public.evidence_capture_intents where record_id=$2)`, operation.OperationID, parent.RecordID).
		Scan(&operationCount, &activityReceipts, &searchReceipts, &evidenceReceipts, &scopes, &activityRows, &searchRows, &evidenceRows); err != nil {
		t.Fatalf("read rejected projection effects: %v", err)
	}
	if operationCount != 1 || activityReceipts != 0 || searchReceipts != 0 || evidenceReceipts != 0 || scopes != 0 ||
		activityRows != 1 || searchRows != 1 || evidenceRows != 1 {
		t.Fatalf("rejected operation/activity/search/evidence/scope counts = %d/%d/%d/%d/%d, surviving surfaces = %d/%d/%d",
			operationCount, activityReceipts, searchReceipts, evidenceReceipts, scopes, activityRows, searchRows, evidenceRows)
	}
}

func assertLegacyAdapterRejection(t *testing.T, err error, code, message string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || (message != "" && pgErr.Message != message) {
		t.Fatalf("PostgreSQL rejection = %v, want SQLSTATE %s message %q", err, code, message)
	}
}
