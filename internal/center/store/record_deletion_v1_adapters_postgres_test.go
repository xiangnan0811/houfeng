package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
	storemigrate "houfeng/internal/center/store/migrate"
)

func RunV1AdapterContinuation(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error)) {
	t.Run("client_event_and_outbox", func(t *testing.T) {
		t.Run("client_authority", TestPostgresIntegrationRecordDeletionV1ClientAuthority)
		t.Run("outbox_batches", TestPostgresIntegrationRecordDeletionV1OutboxBatchHandoff)
		t.Run("late_provider_and_rollback", runV1ClientLateProvider)
	})
	t.Run("known_local_and_shared_handoff", func(t *testing.T) {
		t.Run("materialized_and_takeover", TestPostgresIntegrationRecordDeletionV1PrePlanArchiveHandoff)
		t.Run("obligation_only_absent", TestPostgresIntegrationRecordDeletionV1ObligationOnlyAbsent)
		t.Run("processed_shared_archive", TestPostgresIntegrationRecordDeletionV1KnownLocalShared)
	})
	t.Run("unresolved_inventory_no_dispatch", func(t *testing.T) {
		t.Run("legacy_unknown", TestPostgresIntegrationRecordDeletionV1UnknownInventoryReplay)
		t.Run("historical_timestamp", TestPostgresIntegrationRecordDeletionV1HistoricalTimestampProducer)
		t.Run("ordinary_and_v1_alternatives", TestPostgresIntegrationRecordDeletionV1UnknownLocalAlternativeGC)
		t.Run("nonfinite_sources", v1RetainedTerminalNonfiniteSources)
	})
	t.Run("authority_and_crash_cuts", func(t *testing.T) {
		t.Run("empty_inventory_commit_loss", TestPostgresIntegrationRecordDeletionV1EmptyInventoryCommitRecovery)
		t.Run("physical_crash_cuts", runV1PortabilityPhysicalCrashCuts)
		t.Run("command_authority_matrix", runV1AdapterAuthorityMatrix)
	})
	t.Run("reference_serialization_and_terminal_sets", func(t *testing.T) {
		t.Run("genuine_shared_archive", TestPostgresIntegrationRecordDeletionV1PrePlanSharedArchive)
		t.Run("isolated_owner_handoffs", runV1RetainedOwnerHandoffs)
		t.Run("epoch_serialization", runV1EpochSerialization)
		t.Run("nine_receipt_terminal_control", func(t *testing.T) { v1RetainedTerminalNineReceiptControl(t, newPortabilityAdapter, "") })
		t.Run("nine_receipt_duplicate_target", func(t *testing.T) { v1RetainedTerminalNineReceiptControl(t, newPortabilityAdapter, "duplicate_target") })
		t.Run("nine_receipt_extra_artifact", func(t *testing.T) { v1RetainedTerminalNineReceiptControl(t, newPortabilityAdapter, "extra_artifact") })
	})
}

// This separate registry fixture leaves the operation at online_purging after
// all nine real adapters have committed their receipts, before final completion.
func v1RetainedTerminalNineReceiptControl(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error), corruption string) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	local, neighborID, _, _, _, _, measured, _ := seedV2ProcessedWorkspace(t, ctx, fixture)
	birthFixture := ImportAttachmentGCFixture{Runtime: local.pool, Gate: fixture.admissionGate}
	recordRepository := newRevisionMutationAdmittedRepository(t, birthFixture)
	record, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birthFixture,
		recordsPostgresCompleteRevisionInput(t, "V1 terminal proof target"), "v1-terminal-proof-target"))
	if err != nil {
		t.Fatalf("create terminal proof target: %v", err)
	}
	recordID := record.RecordID
	var neighborAttachments, targetAttachments int
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.record_revision_attachments where record_id=$1),
		(select count(*) from public.record_revision_attachments where record_id=$2)`, neighborID, recordID).
		Scan(&neighborAttachments, &targetAttachments); err != nil || neighborAttachments == 0 || targetAttachments != 0 {
		t.Fatalf("fixture neighbor/target attachments=%d/%d err=%v", neighborAttachments, targetAttachments, err)
	}
	archive := seedV2ExportArtifact(t, ctx, fixture, local, recordID)
	if _, err := fixture.db.Exec(ctx, `insert into public.record_actions
		(action_id,record_id,action_version,title,status,created_by,updated_by,record_fence_epoch)
		values ('ract_v1terminalproof',$1,1,'Terminal handoff','open','usr_records1','usr_records1',0)`, recordID); err != nil {
		t.Fatal(err)
	}
	var outboxID int64
	if err := fixture.db.QueryRow(ctx, `insert into public.record_outbox(project_id,event_kind,subject_kind,subject_id,authorization_epoch,expires_at)
		values ('default','record_action_created','action','ract_v1terminalproof',0,clock_timestamp()+interval '1 hour') returning outbox_row_id`).Scan(&outboxID); err != nil {
		t.Fatal(err)
	}
	// The independently witnessed V1 operation targets the attachment-free record.
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1terminalproof", ReservationID: "drs_v1terminalproof",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: recordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	seeded := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
	pool := openV2LaunchBoundPool(t, ctx, fixture, fixture.deletionWorker, "v1-terminal-proof-worker", measured)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	attachmentRepository := NewPostgresAttachmentRepository(pool)
	if err := attachmentRepository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	workerEvidence := NewPostgresEvidenceRepository(pool, allowRecordPlatformAdmissionGate, nil)
	runtimeEvidence := NewPostgresEvidenceRepository(local.pool, allowRecordPlatformAdmissionGate, nil)
	comparison, err := NewPostgresComparisonDeletionStore(runtimeEvidence, workerEvidence)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := worker.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: seeded.OwnerClaim.Owner.OwnerID, OwnerLeaseDuration: 2 * time.Minute})
	if err != nil || claim == nil || claim.Stage != recorddeletion.DeletionWorkPurgeOnline {
		t.Fatalf("claim terminal work = %#v, %v", claim, err)
	}
	target := recorddeletion.PurgeTarget{Operation: claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}}
	var adapters []recorddeletion.Adapter
	add := func(adapter recorddeletion.Adapter, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		adapters = append(adapters, adapter)
	}
	core, err := recorddeletion.NewCoreAdapter(worker)
	if err != nil {
		t.Fatal(err)
	}
	coreCalled := false
	add(v2CoreHookAdapter{DeletionPurgeAdapter: core, hook: func(target recorddeletion.PurgeTarget) {
		coreCalled = true
		var ready bool
		err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.records where record_id=$2)
			and (select count(*) from public.attachment_purge_receipts where operation_id=$1)=3
			and exists(select 1 from public.evidence_purge_receipts where operation_id=$1 and surface_kind='record_evidence')
			and exists(select 1 from public.record_markdown_client_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_search_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_activity_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_comparison_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_collaboration_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_portability_purge_receipts where operation_id=$1)`, target.Operation.OperationID, recordID).Scan(&ready)
		if err != nil || !ready {
			t.Fatalf("core preceded other receipts: ready=%v err=%v", ready, err)
		}
	}}, nil)
	add(attachments.NewDeletionAdapter(attachmentRepository))
	add(evidence.NewDeletionAdapter(workerEvidence))
	add(recorddeletion.NewMarkdownClientAdapter(worker))
	add(recordsearch.NewDeletionAdapter(worker))
	add(activity.NewDeletionAdapter(worker))
	add(recorddeletion.NewComparisonAdapter(comparison))
	add(recordcollaboration.NewDeletionAdapter(worker))
	add(newPortabilityAdapter(worker))
	registry, err := recorddeletion.NewRegistry(adapters)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := recorddeletion.NewRegistryPurger(registry).PurgeOnline(ctx, target)
	if err != nil || !coreCalled {
		t.Fatalf("real nine-adapter purge: core=%v receipt=%#v err=%v", coreCalled, receipt, err)
	}
	var neighborIntact bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.records where record_id=$1)
		and (select count(*) from public.record_revision_attachments where record_id=$1)=$2`, neighborID, neighborAttachments).
		Scan(&neighborIntact); err != nil || !neighborIntact {
		t.Fatalf("processed workspace neighbor changed: intact=%v err=%v", neighborIntact, err)
	}
	if _, err := local.blob.Stat(ctx, archive.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("physical archive survived: %v", err)
	}
	var taskID string
	var complete bool
	err = fixture.db.QueryRow(ctx, `select t.local_deletion_id, g.deletion_state='completed' and l.seal_obligation
		and exists(select 1 from public.browser_record_cleanup_events where operation_id=t.operation_id and record_id=$3 and cleanup_epoch=12
		 and scopes=array['markdown_draft_buffer','markdown_workspace','comparison_workbench','comparison_save_attempt']::text[])
		and exists(select 1 from public.record_outbox where outbox_row_id=$4 and record_id=$3 and status='cancelled')
		and exists(select 1 from public.record_core_purge_receipts where operation_id=t.operation_id)
		from public.record_portability_v1_targets t join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.obligation_id=t.obligation_id
		where t.operation_id=$1 and t.obligation_id=$2`, op.OperationID, archive.ObligationID, recordID, outboxID).Scan(&taskID, &complete)
	if err != nil || !complete {
		t.Fatalf("pre-terminal physical proof=%v err=%v", complete, err)
	}
	assertOracle := func(t *testing.T, want bool) {
		t.Helper()
		var got bool
		if err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_deletion_verify_online_receipts($1,$2,1)`, op.OperationID, receipt.ReceiptDigest[:]).Scan(&got); err != nil || got != want {
			t.Fatalf("terminal receipt oracle=%v want=%v err=%v", got, want, err)
		}
	}
	assertOwner := func(t *testing.T) {
		t.Helper()
		var state, owner string
		var generation, fence int64
		var expiry time.Time
		var saved []byte
		err := fixture.db.QueryRow(ctx, `select o.operation_state,o.owner_id,o.owner_generation,o.owner_expires_at,r.fence_epoch,o.receipt_digest
			from public.record_purge_operations o join public.deletion_reservations r using(reservation_id) where o.operation_id=$1`, op.OperationID).
			Scan(&state, &owner, &generation, &expiry, &fence, &saved)
		if err != nil || state != "online_purging" || owner != claim.Owner.OwnerID || generation != int64(claim.Owner.Generation) || !expiry.Equal(claim.Owner.ExpiresAt) || fence != int64(op.FenceEpoch) || len(saved) != 0 {
			t.Fatalf("rejection changed owner/state/fence/receipt: %s %s %d %v %d %x err=%v", state, owner, generation, expiry, fence, saved, err)
		}
	}
	assertDenied := func(t *testing.T) {
		t.Helper()
		var denied *pgconn.PgError
		if err := worker.CompleteOnlinePurge(ctx, *claim, receipt); !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatalf("complete with corrupt physical proof = %v, want 42501", err)
		}
		assertOwner(t)
	}
	// Establish that all nine independently committed receipts and the actual
	// aggregate pass before arranging any owner-only historical fault.
	assertOracle(t, true)
	if corruption != "" {
		var result pgconn.CommandTag
		switch corruption {
		case "duplicate_target":
			// INSERT is allowed, while the original target and preparation are
			// immutable. This creates a second target pointing at the same task.
			result, err = fixture.db.Exec(ctx, `insert into public.record_portability_v1_targets
				(operation_id,target_id,backend_kind,blob_key,object_version_id,sha256,byte_size,blob_created_at,
				 namespace_id,incarnation_id,obligation_id,storage_binding,disposition,retained_evidence,local_deletion_id)
				select operation_id,record_platform_internal.record_portability_v1_target_id(operation_id,backend_kind,blob_key,
				 object_version_id||'-duplicate',namespace_id,incarnation_id,storage_binding),backend_kind,blob_key,
				 object_version_id||'-duplicate',sha256,byte_size,blob_created_at,namespace_id,incarnation_id,
				 obligation_id,storage_binding,disposition,retained_evidence,local_deletion_id
				from public.record_portability_v1_targets where operation_id=$1 and local_deletion_id=$2`, op.OperationID, taskID)
		case "extra_artifact":
			// The original immutable manifest cannot be amended to cover this
			// later extra source; its saved source-to-target set is inconsistent.
			result, err = fixture.db.Exec(ctx, `insert into public.record_portability_v1_artifacts
				(operation_id,source_kind,artifact_id,job_id,artifact_role,backend_kind,blob_key,object_version_id,
				 sha256,byte_size,created_at,namespace_id,incarnation_id,obligation_id,target_id)
				select operation_id,source_kind,'rxa_v1terminalextra',job_id,artifact_role,backend_kind,blob_key,object_version_id,
				 sha256,byte_size,created_at,namespace_id,incarnation_id,obligation_id,target_id
				from public.record_portability_v1_artifacts where operation_id=$1 and artifact_id='rxa_v2exclusive'`, op.OperationID)
		default:
			t.Fatalf("unknown terminal fault %q", corruption)
		}
		if err != nil || result.RowsAffected() != 1 {
			t.Fatalf("arrange %s: rows=%d err=%v", corruption, result.RowsAffected(), err)
		}
		var eligible bool
		var members int
		if corruption == "duplicate_target" {
			err = fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_eligible($1),count(*)
				from public.record_portability_v1_targets where operation_id=$1 and local_deletion_id=$2`, op.OperationID, taskID).Scan(&eligible, &members)
		} else {
			err = fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_eligible($1),count(*)
				from public.record_portability_v1_artifacts where operation_id=$1`, op.OperationID).Scan(&eligible, &members)
		}
		if err != nil || eligible || members != 2 {
			t.Fatalf("corrupt %s eligible=%v members=%d err=%v", corruption, eligible, members, err)
		}
		assertOracle(t, false)
		assertDenied(t)
		// The inserted manifest rows are immutable: the separate clean control
		// fixture proves successful completion without bypassing those guards.
		return
	}
	for _, probe := range []struct {
		name, id string
		sameKey  bool
	}{
		{"orphan_pending_task", "bgd_v1terminalorphan", false},
		{"duplicate_identity_task", "bgd_v1terminalduplicate", true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			result, err := fixture.db.Exec(ctx, `insert into public.blob_gc_deletions
				(deletion_id,project_id,purge_mode,blob_key,sha256_digest,backend_kind,deletion_state,
				 operation_id,local_contract_version,namespace_id,incarnation_id)
				select $2,project_id,purge_mode,
				 case when $3 then blob_key else 'sha256/'||encode(sha256(convert_to('v1 terminal unrelated orphan','UTF8')),'hex') end,
				 case when $3 then sha256_digest else sha256(convert_to('v1 terminal unrelated orphan','UTF8')) end,
				 'local','pending',operation_id,2,namespace_id,incarnation_id
				from public.blob_gc_deletions where deletion_id=$1`, taskID, probe.id, probe.sameKey)
			if err != nil || result.RowsAffected() != 1 {
				t.Fatalf("insert extra GC row: rows=%d err=%v", result.RowsAffected(), err)
			}
			t.Cleanup(func() {
				_, _ = fixture.db.Exec(context.Background(), `delete from public.blob_gc_deletions where deletion_id=$1`, probe.id)
			})
			assertOracle(t, false)
			assertDenied(t)
			result, err = fixture.db.Exec(ctx, `delete from public.blob_gc_deletions where deletion_id=$1`, probe.id)
			if err != nil || result.RowsAffected() != 1 {
				t.Fatalf("remove extra GC row: rows=%d err=%v", result.RowsAffected(), err)
			}
			assertOracle(t, true)
			assertOwner(t)
		})
	}
	for _, probe := range []struct {
		name  string
		forge bool
	}{
		{"missing_portability_receipt", false},
		{"forged_portability_receipt", true},
	} {
		t.Run(probe.name, func(t *testing.T) {
			var original []byte
			if err := fixture.db.QueryRow(ctx, `select to_jsonb(r) from public.record_portability_purge_receipts r where operation_id=$1`, op.OperationID).Scan(&original); err != nil {
				t.Fatal(err)
			}
			restored := false
			restore := func() error {
				if _, err := fixture.db.Exec(context.Background(), `delete from public.record_portability_purge_receipts where operation_id=$1`, op.OperationID); err != nil {
					return err
				}
				_, err := fixture.db.Exec(context.Background(), `insert into public.record_portability_purge_receipts
					select (jsonb_populate_record(null::public.record_portability_purge_receipts,$1::jsonb)).*`, original)
				return err
			}
			t.Cleanup(func() {
				if !restored {
					if err := restore(); err != nil {
						t.Errorf("restore captured receipt: %v", err)
					}
				}
			})
			result, err := fixture.db.Exec(ctx, `delete from public.record_portability_purge_receipts where operation_id=$1`, op.OperationID)
			if err != nil || result.RowsAffected() != 1 {
				t.Fatalf("remove original receipt: rows=%d err=%v", result.RowsAffected(), err)
			}
			if probe.forge {
				result, err = fixture.db.Exec(ctx, `insert into public.record_portability_purge_receipts
					select (jsonb_populate_record(null::public.record_portability_purge_receipts,
					 jsonb_set($1::jsonb,'{receipt_digest}',to_jsonb(decode(repeat('ff',32),'hex'))))).*`, original)
				if err != nil || result.RowsAffected() != 1 {
					t.Fatalf("insert forged receipt: rows=%d err=%v", result.RowsAffected(), err)
				}
			}
			assertOracle(t, false)
			assertDenied(t)
			if err := restore(); err != nil {
				t.Fatalf("restore exact receipt: %v", err)
			}
			restored = true
			var identical bool
			if err := fixture.db.QueryRow(ctx, `select to_jsonb(r)=$2::jsonb from public.record_portability_purge_receipts r where operation_id=$1`, op.OperationID, original).Scan(&identical); err != nil || !identical {
				t.Fatalf("captured receipt restoration identical=%v err=%v", identical, err)
			}
			assertOracle(t, true)
			assertOwner(t)
		})
	}
	// The completed proof and inventory are immutable even to the fixture owner.
	for _, probe := range []struct {
		name, sql string
		args      []any
	}{
		{"task_inventory", `update public.blob_gc_deletions set inventory_digest=decode(repeat('ff',32),'hex') where deletion_id=$1`, []any{taskID}},
		{"task_command", `update public.blob_gc_deletions set completion_command=convert_to('{}','UTF8') where deletion_id=$1`, []any{taskID}},
		{"registered_task", `delete from public.blob_gc_deletions where deletion_id=$1`, []any{taskID}},
		{"sealed_link", `delete from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2`, []any{taskID, archive.ObligationID}},
		{"target", `delete from public.record_portability_v1_targets where operation_id=$1`, []any{op.OperationID}},
		{"source_snapshot", `delete from public.record_portability_v1_obligations where operation_id=$1 and obligation_id=$2`, []any{op.OperationID, archive.ObligationID}},
		{"artifact_snapshot", `delete from public.record_portability_v1_artifacts where operation_id=$1`, []any{op.OperationID}},
		{"manifest", `update public.record_portability_v1_preparations set inventory_digest=decode(repeat('ff',32),'hex') where operation_id=$1`, []any{op.OperationID}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			_, err := fixture.db.Exec(ctx, probe.sql, probe.args...)
			var rejected *pgconn.PgError
			if !errors.As(err, &rejected) || rejected.Code != "55000" {
				t.Fatalf("immutable proof mutation = %v, want 55000", err)
			}
		})
	}
	t.Run("physical_proof_tamper", func(t *testing.T) {
		var actual, badCommand, badInventory bool
		err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_task_proof(g),
			record_platform_internal.record_portability_v1_local_task_proof(jsonb_populate_record(g,jsonb_build_object(
			 'completion_command',to_jsonb(convert_to('{}','UTF8')),
			 'receipt_digest',to_jsonb(sha256(convert_to('HOUFENG-LOCAL-GC-V2','UTF8')||convert_to('{}','UTF8')))))),
			record_platform_internal.record_portability_v1_local_task_proof(jsonb_populate_record(g,jsonb_build_object(
			 'inventory_digest',to_jsonb(decode(repeat('ff',32),'hex')))))
			from public.blob_gc_deletions g where g.deletion_id=$1`, taskID).Scan(&actual, &badCommand, &badInventory)
		if err != nil || !actual || badCommand || badInventory {
			t.Fatalf("local proof actual=%v forged-command=%v forged-inventory=%v err=%v", actual, badCommand, badInventory, err)
		}
		assertOracle(t, true)
		assertOwner(t)
	})
	bad := receipt
	bad.ReceiptDigest[0] ^= 0xff
	var denied *pgconn.PgError
	if err := worker.CompleteOnlinePurge(ctx, *claim, bad); !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("forged aggregate completion = %v", err)
	}
	var state, owner string
	var generation, fence int64
	var expiry time.Time
	if err := fixture.db.QueryRow(ctx, `select o.operation_state,o.owner_id,o.owner_generation,o.owner_expires_at,r.fence_epoch
		from public.record_purge_operations o join public.deletion_reservations r using(reservation_id) where o.operation_id=$1`, op.OperationID).Scan(&state, &owner, &generation, &expiry, &fence); err != nil || state != "online_purging" || owner != claim.Owner.OwnerID || generation != int64(claim.Owner.Generation) || !expiry.Equal(claim.Owner.ExpiresAt) || fence != int64(op.FenceEpoch) {
		t.Fatalf("rejection changed owner/state/fence: %s %s %d %v %d err=%v", state, owner, generation, expiry, fence, err)
	}
	if err := worker.CompleteOnlinePurge(ctx, *claim, receipt); err != nil {
		t.Fatalf("complete exact nine-adapter aggregate: %v", err)
	}
	var stored []byte
	if err := fixture.db.QueryRow(ctx, `select operation_state,receipt_digest from public.record_purge_operations where operation_id=$1`, op.OperationID).Scan(&state, &stored); err != nil || state != "online_purged" || !bytes.Equal(stored, receipt.ReceiptDigest[:]) {
		t.Fatalf("terminal state=%s digest=%x err=%v", state, stored, err)
	}
}

// This exercises the worker login and an independently installed delete_commit;
// the administrative connection is used only to arrange and inspect fixtures.
func TestPostgresIntegrationRecordDeletionV1ClientAuthority(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "v1-client-fixture", 2)
	birth := ImportAttachmentGCFixture{Runtime: runtime, Gate: fixture.admissionGate}
	records := newRevisionMutationAdmittedRepository(t, birth)
	record, err := records.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birth,
		recordsPostgresCompleteRevisionInput(t, "V1 client authority"), "v1-client-authority"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.markdown_client_draft_buffers
		(user_id,browser_id,record_id,project_id,session_generation,cleanup_generation)
		values('usr_records1','v1client',$1,'default',1,0)`, record.RecordID); err != nil {
		t.Fatalf("insert markdown buffer: %v", err)
	}
	operation := recorddeletion.DeletionOperation{
		ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_v1clientauthority", ReservationID: "drs_v1clientauthority",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 12, LedgerSequence: 1, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4),
	}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, operation)
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-client-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workerPool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
	command := recorddeletion.MarkdownClientPurgeCommand{Operation: target.Operation, OwnerClaim: target.OwnerClaim,
		SurfaceDigest: recorddeletion.RecordMarkdownClientSurfaceDigest()}
	verifyErr := worker.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, tx *RecordPlatformTransaction) error {
		body, err := legacyAdapterPurgeEnvelope(ctx, tx.tx, target, command.SurfaceDigest)
		if err != nil {
			return err
		}
		var surface, receipt []byte
		var removed int64
		var verifiedAt any
		return tx.tx.QueryRow(ctx, `select * from public.record_markdown_client_purge_verify_v1($1)`, body).
			Scan(&surface, &receipt, &removed, &verifiedAt)
	})
	var missingEvent *pgconn.PgError
	if !errors.As(verifyErr, &missingEvent) || missingEvent.Code != "55000" {
		t.Fatalf("verify absent V1 event = %v, want 55000", verifyErr)
	}
	wrong := command
	wrong.OwnerClaim.Owner.Generation++
	if _, err := worker.PurgeRecordMarkdownClient(ctx, wrong); err == nil {
		t.Fatal("stale owner purged client metadata")
	}
	var eventCount, bufferCount int
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.browser_record_cleanup_events where operation_id=$1),
		(select count(*) from public.markdown_client_draft_buffers where record_id=$2)`,
		target.Operation.OperationID, record.RecordID).Scan(&eventCount, &bufferCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 0 || bufferCount != 1 {
		t.Fatalf("stale owner changed event/buffer: %d/%d", eventCount, bufferCount)
	}
	var directCount int
	err = workerPool.QueryRow(ctx, `select count(*) from public.markdown_client_draft_buffers`).Scan(&directCount)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("worker raw table read = %v, want 42501", err)
	}
	receipt, err := worker.PurgeRecordMarkdownClient(ctx, command)
	if err != nil || receipt.RemovedRowCount != 1 {
		t.Fatalf("V1 markdown purge = %#v, %v", receipt, err)
	}
	if err := worker.VerifyRecordMarkdownClientPurge(ctx, command, receipt); err != nil {
		t.Fatalf("verify V1 markdown purge: %v", err)
	}
	replayed, err := worker.PurgeRecordMarkdownClient(ctx, command)
	if err != nil || replayed != receipt {
		t.Fatalf("replay V1 markdown purge = %#v, %v", replayed, err)
	}
	var eventValid bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.browser_record_cleanup_events
		where operation_id=$1 and project_id='default' and record_id=$2 and cleanup_epoch=$3
		and scopes=array['markdown_draft_buffer','markdown_workspace','comparison_workbench','comparison_save_attempt']::text[])
		and not exists(select 1 from public.record_deletion_gates where operation_id=$1)`,
		target.Operation.OperationID, record.RecordID, int64(target.Operation.FenceEpoch)).Scan(&eventValid); err != nil {
		t.Fatal(err)
	}
	if !eventValid {
		t.Fatal("V1 cleanup event missing, mismatched, or accompanied by a V2 gate")
	}
}
func TestPortabilityV1PreparationProjectionValidation(t *testing.T) {
	digest := strings.Repeat("a", 64)
	localID := "bgd_" + strings.Repeat("b", 64)
	s3ID := "bgd_" + strings.Repeat("c", 64)
	version := "local-v2-rwo_" + strings.Repeat("d", 32)
	local := portabilityV1LocalTarget{DeletionID: localID, NamespaceID: "00000000-0000-0000-0000-000000000001",
		IncarnationID: "00000000-0000-0000-0000-000000000002", Key: "sha256/" + digest, ExpectedVersion: version}
	base := portabilityV1Preparation{InventoryDigest: digest, RequiredBackends: []attachments.BackendKind{attachments.BackendKindLocal, attachments.BackendKindS3},
		LocalTargets: []portabilityV1LocalTarget{local}, S3Targets: []string{s3ID}}
	tests := []struct {
		name   string
		mutate func(*portabilityV1Preparation)
		valid  bool
	}{
		{"fresh", func(*portabilityV1Preparation) {}, true},
		{"replay", func(p *portabilityV1Preparation) { p.RequiredBackends = []attachments.BackendKind{} }, true},
		{"missing fresh backend", func(p *portabilityV1Preparation) {
			p.RequiredBackends = []attachments.BackendKind{attachments.BackendKindLocal}
		}, false},
		{"unresolved with targets", func(p *portabilityV1Preparation) { p.UnresolvedCount = 1 }, false},
		{"unresolved inventory", func(p *portabilityV1Preparation) {
			p.UnresolvedCount = 1
			p.RequiredBackends = []attachments.BackendKind{}
			p.LocalTargets = []portabilityV1LocalTarget{}
			p.S3Targets = []string{}
		}, true},
		{"bad namespace", func(p *portabilityV1Preparation) { p.LocalTargets[0].NamespaceID = "unknown" }, false},
		{"bad expected identity", func(p *portabilityV1Preparation) { p.LocalTargets[0].ExpectedVersion = "local-v2-rwo_unknown" }, false},
		{"duplicate target", func(p *portabilityV1Preparation) { p.S3Targets = []string{localID} }, false},
		{"unordered s3 targets", func(p *portabilityV1Preparation) { p.S3Targets = []string{s3ID, "bgd_" + strings.Repeat("1", 64)} }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			p.LocalTargets = append([]portabilityV1LocalTarget(nil), base.LocalTargets...)
			p.S3Targets = append([]string(nil), base.S3Targets...)
			tc.mutate(&p)
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodePortabilityV1Preparation(raw)
			if (err == nil) != tc.valid {
				t.Fatalf("decode = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

// Owner-only fixture corruption exercises source-to-target validation itself;
// these rows do not establish writer provenance or authorize a cleanup.
func TestPostgresIntegrationRecordDeletionV1InventorySourceLinks(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "v1-inventory-link-fixture", 2)
	birth := ImportAttachmentGCFixture{Runtime: runtime, Gate: fixture.admissionGate}
	record, err := newRevisionMutationAdmittedRepository(t, birth).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birth,
		recordsPostgresCompleteRevisionInput(t, "V1 inventory linkage"), "v1-inventory-link"))
	if err != nil {
		t.Fatal(err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1inventorylink", ReservationID: "drs_v1inventorylink",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 12, LedgerSequence: 1, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4)}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, operation)
	_, err = fixture.db.Exec(ctx, `insert into public.record_portability_v1_preparations
		(operation_id,project_id,record_id,surface_digest,ledger_domain,ledger_sequence,ledger_entry_hash,
		 witness_proof_digest,owners,inventory_digest,removed_rows,pdf_pending)
		select po.operation_id,po.project_id,$2,record_platform_internal.legacy_adapter_surface('record_portability'),v.ledger_domain,
		 po.ledger_sequence,po.ledger_entry_hash,po.witness_proof_digest,
		 '{"export_jobs":[],"import_jobs":[],"import_plans":[],"mappings":[],"origins":[]}'::jsonb,
		 record_platform_internal.record_portability_v1_frame_digest(po.operation_id,po.project_id,$2,
		   record_platform_internal.legacy_adapter_surface('record_portability'),v.ledger_domain,po.ledger_sequence,po.ledger_entry_hash,po.witness_proof_digest,
		   '{"export_jobs":[],"import_jobs":[],"import_plans":[],"mappings":[],"origins":[]}'::jsonb,0,false,
		   jsonb_build_array(jsonb_build_array(po.operation_id,'export','rxa_valid','rej_inventory','archive','s3','legacy/key',null,
		     repeat('11',32),1,to_jsonb('-infinity'::timestamptz at time zone 'UTC'),null,null,null,
		     record_platform_internal.record_portability_v1_target_id(po.operation_id,'s3','legacy/key',null,null,null,null))),
		   '[]'::jsonb,jsonb_build_array(jsonb_build_array(po.operation_id,
		     record_platform_internal.record_portability_v1_target_id(po.operation_id,'s3','legacy/key',null,null,null,null),
		     's3','legacy/key',null,null,null,to_jsonb('-infinity'::timestamptz at time zone 'UTC'),
		     null,null,null,null,'unresolved','[]'::jsonb,null))),0,false
		 from public.record_purge_operations po join record_platform_internal.record_deletion_verified_entries v
		   on v.operation_id=po.operation_id and v.ledger_sequence=po.ledger_sequence and v.ledger_entry_hash=po.ledger_entry_hash
		 where po.operation_id=$1`,
		target.Operation.OperationID, record.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	var targetID string
	err = fixture.db.QueryRow(ctx, `insert into public.record_portability_v1_targets
		(operation_id,target_id,backend_kind,blob_key,object_version_id,sha256,byte_size,blob_created_at,disposition,retained_evidence)
		values($1,record_platform_internal.record_portability_v1_target_id($1,'s3','legacy/key',null,null,null,null),
		 's3','legacy/key',null,null,null,'-infinity'::timestamptz,'unresolved','[]'::jsonb) returning target_id`, target.Operation.OperationID).Scan(&targetID)
	if err != nil {
		t.Fatal(err)
	}
	insertArtifact := `insert into public.record_portability_v1_artifacts
		(operation_id,source_kind,artifact_id,job_id,artifact_role,backend_kind,blob_key,object_version_id,
		 sha256,byte_size,created_at,target_id)
		values($1,'export',$2,'rej_inventory','archive','s3',$3,null,decode(repeat('11',32),'hex'),1,'-infinity'::timestamptz,$4)`
	if _, err := fixture.db.Exec(ctx, insertArtifact, target.Operation.OperationID, "rxa_valid", "legacy/key", targetID); err != nil {
		t.Fatal(err)
	}
	var valid bool
	if err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_source_links($1)`, target.Operation.OperationID).Scan(&valid); err != nil || !valid {
		t.Fatalf("valid unresolved source link: %v, %v", valid, err)
	}
	var savedTime string
	var eligible, digestMatches bool
	var removed, tasks int64
	err = fixture.db.QueryRow(ctx, `select t.blob_created_at::text,
		record_platform_internal.record_portability_v1_eligible(p.operation_id),
		p.inventory_digest=record_platform_internal.record_portability_v1_inventory_digest(p.operation_id),p.removed_rows,
		(select count(*) from public.blob_gc_deletions g where g.operation_id=p.operation_id)
		from public.record_portability_v1_preparations p join public.record_portability_v1_targets t using(operation_id)
		where p.operation_id=$1`, target.Operation.OperationID).Scan(&savedTime, &eligible, &digestMatches, &removed, &tasks)
	if err != nil || savedTime != "-infinity" || eligible || !digestMatches || removed != 0 || tasks != 0 {
		t.Fatalf("nonfinite inventory time=%q eligible=%v digest=%v removed=%d tasks=%d err=%v", savedTime, eligible, digestMatches, removed, tasks, err)
	}
	var digests [][]byte
	for _, zone := range []string{"UTC", "America/New_York"} {
		tx, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `select set_config('TimeZone',$1,true)`, zone); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		var digest []byte
		err = tx.QueryRow(ctx, `select record_platform_internal.record_portability_v1_inventory_digest($1)`, target.Operation.OperationID).Scan(&digest)
		if rollbackErr := tx.Rollback(ctx); err != nil || rollbackErr != nil {
			t.Fatalf("digest in %s: %v; rollback: %v", zone, err, rollbackErr)
		}
		digests = append(digests, digest)
	}
	if len(digests[0]) != sha256.Size || !bytes.Equal(digests[0], digests[1]) {
		t.Fatalf("inventory digest changed with TimeZone: %x / %x", digests[0], digests[1])
	}
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-inventory-readback", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workerPool); err != nil {
		t.Fatal(err)
	}
	var projections [][]byte
	for _, zone := range []string{"UTC", "America/New_York"} {
		tx, err := workerPool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `select set_config('TimeZone',$1,true)`, zone); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		authority, err := legacyAdapterPurgeEnvelope(ctx, tx, target, recorddeletion.RecordPortabilitySurfaceDigest())
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		body, err := json.Marshal(struct {
			Authority      json.RawMessage `json:"authority"`
			StorageBinding *string         `json:"storage_binding"`
		}{Authority: authority})
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		var raw []byte
		err = tx.QueryRow(ctx, `select public.record_portability_purge_prepare_resolve_v1($1)`, body).Scan(&raw)
		if rollbackErr := tx.Rollback(ctx); err != nil || rollbackErr != nil {
			t.Fatalf("readback in %s: %v; rollback: %v", zone, err, rollbackErr)
		}
		projection, err := decodePortabilityV1Preparation(raw)
		if err != nil || projection.UnresolvedCount != 1 || len(projection.LocalTargets) != 0 || len(projection.S3Targets) != 0 {
			t.Fatalf("nonfinite readback in %s: %#v, %v", zone, projection, err)
		}
		projections = append(projections, raw)
	}
	if !bytes.Equal(projections[0], projections[1]) {
		t.Fatalf("readback changed with TimeZone: %s / %s", projections[0], projections[1])
	}
	if _, err := fixture.db.Exec(ctx, insertArtifact, target.Operation.OperationID, "rxa_wrong", "legacy/other", targetID); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_source_links($1)`, target.Operation.OperationID).Scan(&valid); err != nil || valid {
		t.Fatalf("mismatched source link: %v, %v", valid, err)
	}
}

// The expected bytes use an independently constructed PostgreSQL JSON frame;
// hashing that frame in Go catches changes in framing and field ordering.
func TestPostgresIntegrationRecordDeletionV1InventoryDigestFrame(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	var actual []byte
	var frame string
	err := fixture.db.QueryRow(ctx, `select
		record_platform_internal.record_portability_v1_frame_digest('rpo_frame','default','rec_frame',
		 decode(repeat('01',32),'hex'),decode(repeat('02',32),'hex'),7,
		 decode(repeat('03',32),'hex'),decode(repeat('04',32),'hex'),
		 '{"export_jobs":[],"import_jobs":[],"import_plans":[],"mappings":[],"origins":[]}'::jsonb,
		 9,false,'[]'::jsonb,'[]'::jsonb,'[]'::jsonb),
		jsonb_build_array('HOUFENG-PORTABILITY-INVENTORY-V1','rpo_frame','default','rec_frame',
		 repeat('01',32),repeat('02',32),7,repeat('03',32),repeat('04',32),
		 '{"export_jobs":[],"import_jobs":[],"import_plans":[],"mappings":[],"origins":[]}'::jsonb,
		 9,false,'[]'::jsonb,'[]'::jsonb,'[]'::jsonb)::text`).Scan(&actual, &frame)
	if err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(frame))
	if !bytes.Equal(actual, expected[:]) {
		t.Fatalf("inventory digest = %x, want %x", actual, expected)
	}
}

func TestPostgresIntegrationRecordDeletionV1PrepareCommandShape(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"null binding", `{"authority":{},"storage_binding":null}`, true},
		{"binding", `{"authority":{},"storage_binding":"` + strings.Repeat("a", 64) + `"}`, true},
		{"duplicate outer", `{"authority":{},"storage_binding":null,"storage_binding":null}`, false},
		{"duplicate authority", `{"authority":{"owner_id":"one","owner_id":"two"},"storage_binding":null}`, false},
		{"missing binding", `{"authority":{}}`, false},
		{"invalid binding", `{"authority":{},"storage_binding":"abc"}`, false},
		{"non-object authority", `{"authority":[],"storage_binding":null}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var parsed []byte
			err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_prepare_command($1)`, []byte(tc.body)).Scan(&parsed)
			if (err == nil) != tc.valid {
				t.Fatalf("parse = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestPostgresIntegrationRecordDeletionV1ObligationOnlyExpectedVersion(t *testing.T) {
	ctx := context.Background()
	fixture := newImportWriteTestFixture(t, ctx, false)
	content := []byte("import-dispatch-test-archive")
	digest := sha256.Sum256(content)
	hexDigest := hex.EncodeToString(digest[:])
	namespace := fixture.blob.Namespace()
	request := fixture.request(content, false)
	registration := attachments.WriteRegistration{WriteOrigin: request.Origin,
		ObligationID:   "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", ""),
		WriterInstance: localBlobFixtureUUID(t), RequestSeq: 1, ProjectID: "default",
		NamespaceID: &namespace.NamespaceID, IncarnationID: &namespace.IncarnationID,
		BackendKind: attachments.BackendKindLocal, Key: "sha256/" + hexDigest, SHA256: hexDigest, Size: int64(len(content))}
	if _, err := fixture.repository.RegisterWrite(ctx, registration); err != nil {
		t.Fatal(err)
	}
	var sourceCount, artifactCount int
	if err := fixture.platform.db.QueryRow(ctx, `select count(*),count(*) filter(where source_kind<>'obligation')
		from record_platform_internal.record_portability_v1_sources($1,array[]::text[],array[$2]::text[])`,
		"rec_inventory", fixture.job.ImportJobID).Scan(&sourceCount, &artifactCount); err != nil || sourceCount != 1 || artifactCount != 0 {
		t.Fatalf("obligation-only discovery sources=%d artifacts=%d err=%v", sourceCount, artifactCount, err)
	}
	readSource := func() (*string, *string) {
		t.Helper()
		var actual, expected *string
		err := fixture.platform.db.QueryRow(ctx, `select snapshot->>'object_version',
			record_platform_internal.record_portability_v1_local_source_expected(snapshot,obligation_id)
			from record_platform_internal.record_portability_v1_sources($1,array[]::text[],array[$2]::text[])
			where source_kind='obligation' and obligation_id=$3`, "rec_inventory", fixture.job.ImportJobID, registration.ObligationID).Scan(&actual, &expected)
		if err != nil {
			t.Fatal(err)
		}
		return actual, expected
	}
	actual, expected := readSource()
	if actual != nil || expected != nil {
		t.Fatalf("open source actual=%v expected=%v", actual, expected)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='failed' where import_job_id=$1`, fixture.job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	actual, expected = readSource()
	if actual != nil || expected == nil || *expected != "local-v2-"+registration.ObligationID {
		t.Fatalf("closed source actual=%v expected=%v", actual, expected)
	}
	var snapshot []byte
	if err := fixture.platform.db.QueryRow(ctx, `select to_jsonb(o) from public.record_write_obligations o where obligation_id=$1`, registration.ObligationID).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	tx, err := fixture.platform.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select set_config('TimeZone','America/New_York',true)`); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	var replay *string
	err = tx.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_source_expected($1::jsonb,$2)`, string(snapshot), registration.ObligationID).Scan(&replay)
	if rollbackErr := tx.Rollback(ctx); err != nil || rollbackErr != nil {
		t.Fatalf("source replay: %v; rollback: %v", err, rollbackErr)
	}
	if replay == nil || *replay != *expected {
		t.Fatalf("source replay expected=%v, want %q", replay, *expected)
	}
}

func TestPostgresIntegrationRecordDeletionV1UnknownInventoryReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "v1-unknown-inventory-fixture", 2)
	birth := ImportAttachmentGCFixture{Runtime: runtime, Gate: fixture.admissionGate}
	record, err := newRevisionMutationAdmittedRepository(t, birth).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birth,
		recordsPostgresCompleteRevisionInput(t, "V1 unknown inventory"), "v1-unknown-inventory"))
	if err != nil {
		t.Fatal(err)
	}
	seedPortabilityExport(t, ctx, fixture, "rej_v1unknown", "rxa_v1unknown", record.RecordID, record.RevisionID,
		testStoreRecordPlatformDigest(0x47), "published")
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1unknowninventory", ReservationID: "drs_v1unknowninventory",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 12, LedgerSequence: 1, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4)}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, operation)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-unknown-inventory-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	first, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil || first.UnresolvedCount != 1 || len(first.RequiredBackends) != 0 || len(first.LocalTargets) != 0 || len(first.S3Targets) != 0 {
		t.Fatalf("fresh unknown preparation = %#v, %v", first, err)
	}
	second, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil || second.InventoryDigest != first.InventoryDigest || second.UnresolvedCount != first.UnresolvedCount ||
		len(second.RequiredBackends) != 0 || len(second.LocalTargets) != 0 || len(second.S3Targets) != 0 {
		t.Fatalf("replay unknown preparation = %#v, %v", second, err)
	}
	for _, verify := range []bool{false, true} {
		if _, err := worker.portabilityV1Finish(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest(), verify); err == nil {
			t.Fatalf("unknown inventory accepted finish verify=%v", verify)
		}
	}
	var receipts int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_portability_purge_receipts where operation_id=$1`, target.Operation.OperationID).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("unknown inventory created receipts=%d err=%v", receipts, err)
	}
	var sourceKey, savedKey string
	var namespace *string
	var removed, tasks int64
	err = fixture.db.QueryRow(ctx, `select a.blob_key,c.blob_key,c.namespace_id::text,p.removed_rows,
		(select count(*) from public.blob_gc_deletions g where g.operation_id=p.operation_id)
		from public.record_export_artifacts a join public.record_portability_v1_artifacts c on c.artifact_id=a.export_artifact_id
		join public.record_portability_v1_preparations p on p.operation_id=c.operation_id
		where p.operation_id=$1 and a.export_artifact_id='rxa_v1unknown'`, target.Operation.OperationID).
		Scan(&sourceKey, &savedKey, &namespace, &removed, &tasks)
	if err != nil || sourceKey != "sha256/rej_v1unknown" || savedKey != sourceKey || namespace != nil || removed != 0 || tasks != 0 {
		t.Fatalf("unknown source=%q saved=%q namespace=%v removed=%d tasks=%d err=%v", sourceKey, savedKey, namespace, removed, tasks, err)
	}
}

type portabilityV1CommitResponseLossTx struct {
	pgx.Tx
	dropped *bool
}

func (tx *portabilityV1CommitResponseLossTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	if !*tx.dropped {
		*tx.dropped = true
		return errors.New("test: portability preparation commit response lost")
	}
	return nil
}

func TestPostgresIntegrationRecordDeletionV1EmptyInventoryCommitRecovery(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "v1-empty-inventory-fixture", 2)
	birth := ImportAttachmentGCFixture{Runtime: runtime, Gate: fixture.admissionGate}
	record, err := newRevisionMutationAdmittedRepository(t, birth).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birth,
		recordsPostgresCompleteRevisionInput(t, "V1 empty inventory"), "v1-empty-inventory"))
	if err != nil {
		t.Fatal(err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1emptyinventory", ReservationID: "drs_v1emptyinventory",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 12, LedgerSequence: 1, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4)}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, operation)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-empty-inventory-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	if _, err := worker.portabilityV1Finish(ctx, target, surface, true); err == nil {
		t.Fatal("verify created a missing preparation")
	}
	begin := worker.platform.beginTx
	dropped := false
	worker.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := begin(ctx, options)
		if err != nil || dropped {
			return tx, err
		}
		return &portabilityV1CommitResponseLossTx{Tx: tx, dropped: &dropped}, nil
	}
	prepared, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	worker.platform.beginTx = begin
	if !dropped || err != nil || prepared.UnresolvedCount != 0 || len(prepared.LocalTargets) != 0 || len(prepared.S3Targets) != 0 {
		t.Fatalf("empty preparation commit recovery dropped=%v preparation=%#v err=%v", dropped, prepared, err)
	}
	replayed, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil || replayed.InventoryDigest != prepared.InventoryDigest || replayed.UnresolvedCount != 0 || len(replayed.RequiredBackends) != 0 {
		t.Fatalf("empty preparation replay = %#v, %v", replayed, err)
	}
	var authority []byte
	err = worker.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, tx *RecordPlatformTransaction) error {
		var err error
		authority, err = legacyAdapterPurgeEnvelope(ctx, tx.tx, target, surface)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		field string
		value any
	}{
		{"operation_id", "rpo_wrongowner"}, {"reservation_id", "drs_wrongowner"}, {"record_id", "rec_wrongowner"},
		{"fence_epoch", 13}, {"owner_id", "wrong_worker"}, {"owner_generation", 2},
		{"owner_expires_at", time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond).Format(time.RFC3339Nano)},
		{"ledger_domain", strings.Repeat("11", 32)}, {"ledger_sequence", 999},
		{"ledger_entry_hash", strings.Repeat("22", 32)}, {"witness_proof_digest", strings.Repeat("33", 32)},
	} {
		t.Run("wrong_"+tc.field, func(t *testing.T) {
			var command map[string]any
			if err := json.Unmarshal(authority, &command); err != nil {
				t.Fatal(err)
			}
			command[tc.field] = tc.value
			body, err := json.Marshal(command)
			if err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{`select * from public.record_portability_purge_v1($1)`, `select * from public.record_portability_purge_verify_v1($1)`} {
				var savedSurface, savedReceipt []byte
				var removed int64
				var verifiedAt time.Time
				if err := pool.QueryRow(ctx, statement, body).Scan(&savedSurface, &savedReceipt, &removed, &verifiedAt); err == nil {
					t.Fatalf("wrong %s acquired finish authority", tc.field)
				}
			}
		})
	}
	if _, err := worker.portabilityV1Finish(ctx, target, surface, true); err == nil {
		t.Fatal("verify created a missing receipt")
	}
	var receiptCount int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_portability_purge_receipts where operation_id=$1`, target.Operation.OperationID).Scan(&receiptCount); err != nil || receiptCount != 0 {
		t.Fatalf("verify changed receipt count=%d err=%v", receiptCount, err)
	}
	receipt, err := worker.portabilityV1Finish(ctx, target, surface, false)
	if err != nil || receipt.RemovedRowCount != 0 {
		t.Fatalf("empty finish=%#v err=%v", receipt, err)
	}
	verified, err := worker.portabilityV1Finish(ctx, target, surface, true)
	if err != nil || verified != receipt {
		t.Fatalf("empty verify=%#v err=%v", verified, err)
	}
	receiptReplay, err := worker.portabilityV1Finish(ctx, target, surface, false)
	if err != nil || receiptReplay != receipt {
		t.Fatalf("empty receipt replay=%#v err=%v", receiptReplay, err)
	}
}

func TestPostgresIntegrationRecordDeletionV1PrePlanArchiveHandoff(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	actor := storeEvidenceParticipantActor(t)
	witness := localBlobFixtureWitness(t, fixture.Runtime)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, witness, nil)
	content := []byte("V1 archive registered before import plan")
	archiveDigest := sha256.Sum256(content)
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1,
		Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	expires := time.Now().UTC().Add(time.Hour)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "v1-preplan-archive",
		ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	archiveHex := hex.EncodeToString(archiveDigest[:])
	written, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &archiveHex},
		Content: attachments.PutRequest{ExpectedSHA256: archiveDigest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	var originalPlan *string
	if err := fixture.platform.db.QueryRow(ctx, `select encode(plan_digest,'hex') from public.record_write_obligations where obligation_id=$1`, written.ObligationID).Scan(&originalPlan); err != nil || originalPlan != nil {
		t.Fatalf("pre-plan archive registration digest=%v err=%v", originalPlan, err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1preplan")
	planDigest := sha256.Sum256([]byte("V1 certified pre-plan archive mapping"))
	plan, err := imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: actor,
		ArchiveDigest: archiveDigest, PlanDigest: planDigest, ObjectCount: 1, RemapCount: 1,
		Allocations: []recordbirth.Allocation{allocation}, Remaps: []ImportRemap{{EntityKind: "record", SourceID: "rec_v1preplan",
			TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID}},
		Documents:          []ImportDocumentPlan{{SourceID: "rec_v1preplan", TargetID: allocation.RecordID, Title: "Pre-plan archive", Body: "Body"}},
		DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevision", [][]attachments.ImportedAvailableAttachment{{}}, []recordbirth.Allocation{allocation}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	var record records.RevisionCommitResult
	var lockVersion, authorizationEpoch int64
	record.RecordID = allocation.RecordID
	if err := fixture.platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, record.RecordID).
		Scan(&record.RevisionID, &lockVersion, &authorizationEpoch); err != nil {
		t.Fatal(err)
	}
	record.LockVersion, record.AuthorizationEpoch = uint64(lockVersion), uint64(authorizationEpoch)
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1preplanarchive", ReservationID: "drs_v1preplanarchive",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 12, LedgerSequence: 1, LedgerEntryHash: testStoreRecordPlatformDigest(0xa4)}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture.platform, record, operation)
	pool := fixture.platform.openDirectRolePool(t, ctx, fixture.platform.deletionWorker, "v1-preplan-archive-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(fixture.LocalCleanup); err != nil {
		t.Fatal(err)
	}
	workerTx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := legacyAdapterPurgeEnvelope(ctx, workerTx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if rollbackErr := workerTx.Rollback(ctx); err != nil || rollbackErr != nil {
		t.Fatalf("capture authority: %v; rollback: %v", err, rollbackErr)
	}

	// Each owner-only transaction constructs the candidate from the real saved
	// source. Forged owner tuples and target rows exercise the exact scope
	// predicate without mutating the actual plan, source or handoff.
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"exact", true}, {"wrong_plan", false}, {"wrong_job", false}, {"wrong_digest", false},
		{"wrong_record", false}, {"wrong_obligation", false}, {"missing_plan", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := fixture.platform.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			var candidate []byte
			err = tx.QueryRow(ctx, `select record_platform_internal.record_portability_v1_inventory_candidate($1::jsonb,array[]::text[],array[$2]::text[],array[$2]::text[],false)`,
				string(authority), job.ImportJobID).Scan(&candidate)
			if err != nil {
				t.Fatal(err)
			}
			var inventory map[string]json.RawMessage
			if err := json.Unmarshal(candidate, &inventory); err != nil {
				t.Fatal(err)
			}
			var unresolved int
			if err := json.Unmarshal(inventory["unresolved_count"], &unresolved); err != nil || unresolved != 0 {
				t.Fatalf("candidate unresolved=%d err=%v", unresolved, err)
			}
			var owners map[string]json.RawMessage
			if err := json.Unmarshal(inventory["owners"], &owners); err != nil {
				t.Fatal(err)
			}
			if tc.name == "wrong_plan" || tc.name == "wrong_job" {
				planID, jobID := plan.ImportPlanID, job.ImportJobID
				if tc.name == "wrong_plan" {
					planID = "rip_wrong"
				} else {
					jobID = "rij_wrong"
				}
				owners["import_plans"], err = json.Marshal([][]string{{planID, jobID}})
				if err != nil {
					t.Fatal(err)
				}
			}
			ownersBytes, err := json.Marshal(owners)
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `with input as (select $1::jsonb v,$2::jsonb inventory,$3::jsonb owners)
				insert into public.record_portability_v1_preparations(operation_id,project_id,record_id,surface_digest,ledger_domain,
				 ledger_sequence,ledger_entry_hash,witness_proof_digest,owners,inventory_digest,removed_rows,pdf_pending)
				select v->>'operation_id','default',v->>'record_id',decode(v->>'surface_digest','hex'),decode(v->>'ledger_domain','hex'),
				 (v->>'ledger_sequence')::bigint,decode(v->>'ledger_entry_hash','hex'),decode(v->>'witness_proof_digest','hex'),owners,
				 record_platform_internal.record_portability_v1_frame_digest(v->>'operation_id','default',v->>'record_id',
				  decode(v->>'surface_digest','hex'),decode(v->>'ledger_domain','hex'),(v->>'ledger_sequence')::bigint,
				  decode(v->>'ledger_entry_hash','hex'),decode(v->>'witness_proof_digest','hex'),owners,(inventory->>'removed_rows')::bigint,false,
				  inventory->'artifacts',inventory->'obligations',coalesce((select jsonb_agg(jsonb_build_array(x.operation_id,x.target_id,x.backend_kind,
				    x.blob_key,x.object_version_id,encode(x.sha256,'hex'),x.byte_size,to_jsonb(x.blob_created_at at time zone 'UTC'),
				    x.namespace_id,x.incarnation_id,x.obligation_id,encode(x.storage_binding,'hex'),x.disposition,x.retained_evidence,x.local_deletion_id)
				    order by x.target_id collate "C") from jsonb_populate_recordset(null::public.record_portability_v1_targets,inventory->'targets') x),'[]'::jsonb)),
				 (inventory->>'removed_rows')::bigint,false from input`, string(authority), string(candidate), string(ownersBytes))
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `insert into public.record_portability_v1_targets(operation_id,target_id,backend_kind,blob_key,object_version_id,
				sha256,byte_size,blob_created_at,namespace_id,incarnation_id,obligation_id,storage_binding,disposition,retained_evidence,local_deletion_id,deletion_state)
				select x.operation_id,x.target_id,x.backend_kind,x.blob_key,x.object_version_id,x.sha256,x.byte_size,x.blob_created_at,
				 x.namespace_id,x.incarnation_id,x.obligation_id,x.storage_binding,x.disposition,x.retained_evidence,x.local_deletion_id,x.deletion_state
				from jsonb_populate_recordset(null::public.record_portability_v1_targets,($1::jsonb)->'targets') x`, string(candidate))
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `insert into public.record_portability_v1_obligations(operation_id,obligation_id,snapshot,target_id)
				select x.value->>0,x.value->>1,x.value->2,x.value->>3 from jsonb_array_elements(($1::jsonb)->'obligations') x(value)`, string(candidate))
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(ctx, `insert into record_platform_internal.record_deletion_effect_scopes
				(transaction_id,backend_pid,record_id,operation_id,authority_kind,authority_digest,effect_kind)
				select pg_current_xact_id(),pg_backend_pid(),record_id,operation_id,'online_v1',inventory_digest,'portability'
				from public.record_portability_v1_preparations where operation_id=$1`, target.Operation.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "missing_plan" {
				if _, err := tx.Exec(ctx, `delete from public.record_import_entity_mappings where import_plan_id=$1`, plan.ImportPlanID); err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(ctx, `delete from public.record_import_plans where import_plan_id=$1`, plan.ImportPlanID); err != nil {
					t.Fatal(err)
				}
			}
			var member, unchanged bool
			err = tx.QueryRow(ctx, `select record_platform_internal.record_portability_v1_scope_member('record_write_obligation_targets',
				case $3 when 'wrong_digest' then to_jsonb(ot)||jsonb_build_object('plan_digest','\x'||repeat('ff',32))
				 when 'wrong_record' then to_jsonb(ot)||jsonb_build_object('record_id','rec_wrong')
				 when 'wrong_obligation' then to_jsonb(ot)||jsonb_build_object('obligation_id','rwo_00000000000000000000000000000000')
				 else to_jsonb(ot) end,t),ot.handed_off_at is null and o.operation_id is null
				from public.record_write_obligation_targets ot join public.record_write_obligations o using(obligation_id)
				join public.record_portability_v1_targets t on t.operation_id=$1 and t.obligation_id=ot.obligation_id
				where ot.obligation_id=$2 and ot.record_id=$4`, target.Operation.OperationID, written.ObligationID, tc.name, record.RecordID).Scan(&member, &unchanged)
			if err != nil || member != tc.want || !unchanged {
				t.Fatalf("scope member=%v want=%v unchanged=%v err=%v", member, tc.want, unchanged, err)
			}
			if tc.name == "wrong_plan" || tc.name == "wrong_job" || tc.name == "wrong_digest" {
				if _, err := tx.Exec(ctx, `savepoint rejected_handoff`); err != nil {
					t.Fatal(err)
				}
				statement := `update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`
				if tc.name == "wrong_digest" {
					statement = `update public.record_write_obligation_targets set plan_digest=decode(repeat('ff',32),'hex'),handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`
				}
				_, mutationErr := tx.Exec(ctx, statement, written.ObligationID, record.RecordID)
				var denied *pgconn.PgError
				if !errors.As(mutationErr, &denied) || denied.Code != "55000" {
					t.Fatalf("forged handoff = %v, want 55000", mutationErr)
				}
				if _, err := tx.Exec(ctx, `rollback to savepoint rejected_handoff`); err != nil {
					t.Fatal(err)
				}
				if err := tx.QueryRow(ctx, `select ot.handed_off_at is null and o.operation_id is null
					from public.record_write_obligation_targets ot join public.record_write_obligations o using(obligation_id)
					where ot.obligation_id=$1 and ot.record_id=$2`, written.ObligationID, record.RecordID).Scan(&unchanged); err != nil || !unchanged {
					t.Fatalf("rejected handoff changed source: %v, %v", unchanged, err)
				}
			}
		})
		var sourceIntact bool
		if err := fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.record_import_plans where import_plan_id=$1 and import_job_id=$2)
			and exists(select 1 from public.record_write_obligation_targets where obligation_id=$3 and record_id=$4 and handed_off_at is null)
			and exists(select 1 from public.record_write_obligations where obligation_id=$3 and operation_id is null and plan_digest is null)`,
			plan.ImportPlanID, job.ImportJobID, written.ObligationID, record.RecordID).Scan(&sourceIntact); err != nil || !sourceIntact {
			t.Fatalf("scope probe persisted changes: %v, %v", sourceIntact, err)
		}
	}
	pendingJob, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "v1-preplan-pending-writer",
		ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	prepareTx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = prepareTx.Rollback(context.Background()) }()
	prepareAuthority, err := legacyAdapterPurgeEnvelope(ctx, prepareTx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if err != nil {
		t.Fatal(err)
	}
	prepareBody, err := json.Marshal(struct {
		Authority      json.RawMessage `json:"authority"`
		StorageBinding *string         `json:"storage_binding"`
	}{prepareAuthority, nil})
	if err != nil {
		t.Fatal(err)
	}
	var prepareRaw []byte
	if err := prepareTx.QueryRow(ctx, `select public.record_portability_purge_prepare_v1($1)`, prepareBody).Scan(&prepareRaw); err != nil {
		t.Fatal(err)
	}
	prepared, err := decodePortabilityV1Preparation(prepareRaw)
	if err != nil || prepared.UnresolvedCount != 0 || len(prepared.LocalTargets) != 1 ||
		prepared.LocalTargets[0].NamespaceID != fixture.LocalCleanup.NamespaceID() || prepared.LocalTargets[0].IncarnationID != fixture.LocalCleanup.IncarnationID() {
		t.Fatalf("prepare pre-plan archive = %#v, %v", prepared, err)
	}
	// The worker holds the prepared transaction open. A real writer for the
	// same key must wait, then fail without publishing a new responsibility.
	writerDone := make(chan error, 1)
	go func() {
		_, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
			Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: pendingJob.ImportJobID, Generation: int64(pendingJob.LockVersion), ArchiveDigest: &archiveHex},
			Content: attachments.PutRequest{ExpectedSHA256: archiveDigest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
		writerDone <- err
	}()
	waitCtx, cancelWait := context.WithTimeout(ctx, 15*time.Second)
	if err := fixture.WaitForGCSerialization(waitCtx); err != nil {
		cancelWait()
		t.Fatalf("writer did not wait for preparation: %v", err)
	}
	cancelWait()
	if err := prepareTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-writerDone; err == nil {
		t.Fatal("pending V1 cleanup allowed a same-key managed writer")
	}
	var writerSources int
	if err := fixture.platform.db.QueryRow(ctx, `select count(*) from public.record_write_obligations where origin_kind='import_archive' and origin_id=$1`, pendingJob.ImportJobID).Scan(&writerSources); err != nil || writerSources != 0 {
		t.Fatalf("blocked writer persisted sources=%d err=%v", writerSources, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture.platform, target.Operation.OperationID, prepared.InventoryDigest)
	var intact bool
	err = fixture.platform.db.QueryRow(ctx, `select c.snapshot->>'plan_digest' is null and ot.handed_off_at is not null
		and o.operation_id=$1 and o.object_version=$3 and not exists(select 1 from public.record_import_plans where import_plan_id=$4)
		and not exists(select 1 from public.record_portability_v1_artifacts a where a.operation_id=c.operation_id)
		and c.snapshot->>'object_version'=$3 and c.snapshot->>'outcome'='materialized'
		and exists(select 1 from public.record_portability_v1_targets t join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		 join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.obligation_id=o.obligation_id and l.seal_obligation
		 where t.operation_id=$1 and t.obligation_id=o.obligation_id and g.operation_id=$1)
		from public.record_portability_v1_obligations c join public.record_write_obligations o using(obligation_id)
		join public.record_write_obligation_targets ot using(obligation_id)
		where c.operation_id=$1 and c.obligation_id=$2 and ot.record_id=$5`, target.Operation.OperationID, written.ObligationID,
		written.Version.VersionID, plan.ImportPlanID, record.RecordID).Scan(&intact)
	if err != nil || !intact {
		t.Fatalf("archive handoff intact=%v err=%v", intact, err)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); err != nil {
		t.Fatalf("prepare changed archive bytes: %v", err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	receipt, err := worker.PurgeRecordPortability(ctx, target, surface)
	if err != nil {
		t.Fatalf("purge pre-plan archive: %v", err)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("pre-plan archive remains after cleanup: %v", err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
		t.Fatalf("verify pre-plan archive receipt: %v", err)
	}
	replayed, err := worker.PurgeRecordPortability(ctx, target, surface)
	if err != nil || replayed != receipt {
		t.Fatalf("pre-plan receipt replay=%#v err=%v", replayed, err)
	}
	var sealed bool
	err = fixture.platform.db.QueryRow(ctx, `select c.snapshot->>'plan_digest' is null and
		g.deletion_state='completed' and l.seal_obligation and o.cleanup_completed_at is not null
		and o.quota_settled_at is not null and o.cleanup_receipt=g.receipt_digest
		and (g.physical_delete_result='deleted' or g.physical_delete_result='already_absent')
		from public.record_portability_v1_obligations c join public.record_write_obligations o using(obligation_id)
		join public.record_portability_v1_targets t on t.operation_id=c.operation_id and t.target_id=c.target_id
		join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.obligation_id=o.obligation_id
		where c.operation_id=$1 and c.obligation_id=$2`, target.Operation.OperationID, written.ObligationID).Scan(&sealed)
	if err != nil || !sealed {
		t.Fatalf("pre-plan physical proof sealed=%v err=%v", sealed, err)
	}
	var realProof, fakeCommand, fakeInventory bool
	err = fixture.platform.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_task_proof(g),
		record_platform_internal.record_portability_v1_local_task_proof(jsonb_populate_record(g,jsonb_build_object(
		 'completion_command',to_jsonb(convert_to('{}','UTF8')),
		 'receipt_digest',to_jsonb(sha256(convert_to('HOUFENG-LOCAL-GC-V2','UTF8')||convert_to('{}','UTF8')))))),
		record_platform_internal.record_portability_v1_local_task_proof(jsonb_populate_record(g,jsonb_build_object(
		 'inventory_digest',to_jsonb(decode(repeat('ff',32),'hex')))))
		from public.blob_gc_deletions g where g.deletion_id=$1`, prepared.LocalTargets[0].DeletionID).
		Scan(&realProof, &fakeCommand, &fakeInventory)
	if err != nil || !realProof || fakeCommand || fakeInventory {
		t.Fatalf("physical proof real=%v fakeCommand=%v fakeInventory=%v err=%v", realProof, fakeCommand, fakeInventory, err)
	}
	for _, statement := range []string{
		`update public.blob_gc_deletions set inventory_digest=decode(repeat('ff',32),'hex') where deletion_id=$1`,
		`delete from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2`,
	} {
		var mutationErr error
		if strings.Contains(statement, "obligation_id=$2") {
			_, mutationErr = fixture.platform.db.Exec(ctx, statement, prepared.LocalTargets[0].DeletionID, written.ObligationID)
		} else {
			_, mutationErr = fixture.platform.db.Exec(ctx, statement, prepared.LocalTargets[0].DeletionID)
		}
		var denied *pgconn.PgError
		if !errors.As(mutationErr, &denied) || denied.Code != "55000" {
			t.Fatalf("completed proof mutation = %v, want 55000", mutationErr)
		}
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
		t.Fatalf("receipt changed after rejected mutation: %v", err)
	}
	var beforeTask string
	if err := fixture.platform.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, prepared.LocalTargets[0].DeletionID).Scan(&beforeTask); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.platform.db.Exec(ctx, `update public.record_purge_operations set owner_expires_at=clock_timestamp()-interval '1 second'
		where operation_id=$1 and owner_id=$2 and owner_generation=$3 and owner_expires_at=$4`, target.Operation.OperationID,
		target.OwnerClaim.Owner.OwnerID, target.OwnerClaim.Owner.Generation, target.OwnerClaim.Owner.ExpiresAt)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("expire completed operation owner: rows=%d err=%v", result.RowsAffected(), err)
	}
	work, err := worker.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v1_local_successor", OwnerLeaseDuration: 2 * time.Minute})
	if err != nil || work == nil || work.Operation.OperationID != target.Operation.OperationID || work.Owner.Generation != target.OwnerClaim.Owner.Generation+1 {
		t.Fatalf("claim completed proof takeover=%#v err=%v", work, err)
	}
	successor := recorddeletion.PurgeTarget{Operation: work.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: work.Operation.OperationID, Owner: work.Owner}}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err == nil {
		t.Fatal("expired owner verified completed receipt")
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, successor, surface, receipt); err != nil {
		t.Fatalf("successor verify: %v", err)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, successor, surface); err != nil || replay != receipt {
		t.Fatalf("successor replay=%#v err=%v", replay, err)
	}
	var afterTask string
	if err := fixture.platform.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, prepared.LocalTargets[0].DeletionID).Scan(&afterTask); err != nil || afterTask != beforeTask {
		t.Fatalf("takeover changed completed physical task: equal=%v err=%v", afterTask == beforeTask, err)
	}
	newJob, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "v1-preplan-later-source",
		ArchiveDigest: archiveDigest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	later, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: newJob.ImportJobID, Generation: int64(newJob.LockVersion), ArchiveDigest: &archiveHex},
		Content: attachments.PutRequest{ExpectedSHA256: archiveDigest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("later same-key managed write: %v", err)
	}
	if later.Version.Key != written.Version.Key || later.Version.VersionID == written.Version.VersionID || later.ObligationID == written.ObligationID {
		t.Fatalf("later source did not get a distinct incarnation: old=%#v new=%#v", written, later)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, successor, surface); err != nil || replay != receipt {
		t.Fatalf("later-source replay=%#v err=%v", replay, err)
	}
	var preserved bool
	if err := fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.record_write_obligations o where o.obligation_id=$2
		and o.cleanup_completed_at is null and o.quota_settled_at is null and o.operation_id is null)
		and not exists(select 1 from public.local_blob_gc_obligations l where l.deletion_id=$1 and l.obligation_id=$2)
		and not exists(select 1 from public.record_portability_v1_obligations c where c.operation_id=$3 and c.obligation_id=$2)`,
		prepared.LocalTargets[0].DeletionID, later.ObligationID, target.Operation.OperationID).Scan(&preserved); err != nil || !preserved {
		t.Fatalf("later source borrowed old completion: preserved=%v err=%v", preserved, err)
	}
	_, err = fixture.platform.db.Exec(ctx, `insert into public.local_blob_gc_obligations(deletion_id,obligation_id,seal_obligation)
		values($1,$2,true)`, prepared.LocalTargets[0].DeletionID, later.ObligationID)
	var linkDenied *pgconn.PgError
	if !errors.As(err, &linkDenied) || linkDenied.Code != "55000" {
		t.Fatalf("completed task accepted a later source link: %v", err)
	}
	if err := fixture.platform.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, prepared.LocalTargets[0].DeletionID).Scan(&afterTask); err != nil || afterTask != beforeTask {
		t.Fatalf("later source changed completed task: equal=%v err=%v", afterTask == beforeTask, err)
	}
	if _, err := fixture.Blobs.Stat(ctx, later.Version); err != nil {
		t.Fatalf("later source bytes lost: %v", err)
	}
}

// The archive is registered before its plan by the managed writer. Both
// records use independent V1 delete-commit evidence and worker authority.
func TestPostgresIntegrationRecordDeletionV1KnownLocalShared(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	local, firstID, _, _, _, _, measured, _ := seedV2ProcessedWorkspace(t, ctx, fixture)
	lastID, written, content := seedV2SharedArchive(t, ctx, fixture, local, firstID)
	loadRecord := func(id string) records.RevisionCommitResult {
		t.Helper()
		r := records.RevisionCommitResult{RecordID: id}
		var lock, epoch int64
		if err := fixture.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, id).
			Scan(&r.RevisionID, &lock, &epoch); err != nil {
			t.Fatal(err)
		}
		r.LockVersion, r.AuthorizationEpoch = uint64(lock), uint64(epoch)
		return r
	}
	targetFor := func(id, suffix string) recorddeletion.PurgeTarget {
		t.Helper()
		op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
			OperationID: "rpo_v1localshared" + suffix, ReservationID: "drs_v1localshared" + suffix,
			Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: id},
			ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
		return seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, loadRecord(id), op)
	}
	first, last := targetFor(firstID, "first"), targetFor(lastID, "last")
	pool := openV2LaunchBoundPool(t, ctx, fixture, fixture.deletionWorker, "v1-shared-local-worker", measured)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	var initialQuota int64
	if err := fixture.db.QueryRow(ctx, `select physical_bytes from public.attachment_quota_accounts where project_id='default'`).Scan(&initialQuota); err != nil {
		t.Fatal(err)
	}
	firstReceipt, err := worker.PurgeRecordPortability(ctx, first, surface)
	if err != nil {
		t.Fatalf("first shared purge: %v", err)
	}
	var retained bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_targets t
		join public.record_portability_v1_obligations o using(operation_id,target_id)
		where t.operation_id=$1 and t.disposition='retained_shared' and o.obligation_id=$2
		and o.snapshot->>'plan_digest' is null and jsonb_array_length(t.retained_evidence)>0)
		and exists(select 1 from public.record_import_entity_mappings where target_id=$3)
		and exists(select 1 from public.record_write_obligations where obligation_id=$2 and cleanup_completed_at is null)
		and (select physical_bytes from public.attachment_quota_accounts where project_id='default')=$4`,
		first.Operation.OperationID, written.ObligationID, lastID, initialQuota).Scan(&retained); err != nil || !retained {
		t.Fatalf("first shared handoff retained=%v err=%v", retained, err)
	}
	reader, err := local.blob.Open(ctx, written.Version, attachments.FullByteRange())
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(data, content) {
		t.Fatalf("shared archive bytes=%q read=%v close=%v", data, readErr, closeErr)
	}
	prepared, err := worker.preparePortabilityV1(ctx, last, surface)
	if err != nil || prepared.UnresolvedCount != 0 || len(prepared.LocalTargets) != 1 {
		t.Fatalf("last shared prepare=%#v err=%v", prepared, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture, last.Operation.OperationID, prepared.InventoryDigest)
	if replay, err := worker.PurgeRecordPortability(ctx, first, surface); err != nil || replay != firstReceipt {
		t.Fatalf("pending handoff replay=%#v err=%v", replay, err)
	}
	lastReceipt, err := worker.PurgeRecordPortability(ctx, last, surface)
	if err != nil {
		t.Fatalf("last shared purge: %v", err)
	}
	if _, err := local.blob.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("shared archive survived: %v", err)
	}
	var settled bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_targets t
		join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id
		join public.record_write_obligations o on o.obligation_id=l.obligation_id
		where t.operation_id=$1 and t.obligation_id=$2 and l.obligation_id=$2 and l.seal_obligation
		and g.deletion_state='completed' and o.cleanup_completed_at is not null and o.quota_settled_at is not null
		and o.cleanup_receipt=g.receipt_digest)
		and (select physical_bytes from public.attachment_quota_accounts where project_id='default')=$3`,
		last.Operation.OperationID, written.ObligationID, initialQuota-int64(len(content))).Scan(&settled); err != nil || !settled {
		t.Fatalf("last shared settlement=%v err=%v", settled, err)
	}
	for _, item := range []struct {
		target  recorddeletion.PurgeTarget
		receipt recorddeletion.AdapterPurgeReceipt
	}{{first, firstReceipt}, {last, lastReceipt}} {
		if replay, err := worker.PurgeRecordPortability(ctx, item.target, surface); err != nil || replay != item.receipt {
			t.Fatalf("completed shared replay=%#v err=%v", replay, err)
		}
		if err := worker.VerifyRecordPortabilityPurge(ctx, item.target, surface, item.receipt); err != nil {
			t.Fatalf("shared verify: %v", err)
		}
	}
}

func TestPostgresIntegrationRecordDeletionV1OutboxBatchHandoff(t *testing.T) {
	harness := newPostgresExternalDeliveryHarness(t)
	ctx, fixture, record := harness.ctx, harness.fixture, harness.created
	neighbor := seedPostgresActionParent(t, ctx, fixture, "v1-outbox-neighbor")
	if _, err := fixture.db.Exec(ctx, `update public.record_notification_deliveries
		set delivery_state='processing',attempt_count=1,attempt_started_at=clock_timestamp(),updated_at=clock_timestamp()
		where delivery_id=$1`, harness.claim.Event.SubjectID); err != nil {
		t.Fatal(err)
	}
	var routed []int64
	if err := fixture.db.QueryRow(ctx, `with inserted as (insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,authorization_epoch,record_id,created_at,expires_at,next_attempt_at)
		select 'default','record_created','record',$1,0,$1,clock_timestamp()-interval '2 hours',
		clock_timestamp()-interval '1 hour',clock_timestamp()+interval '1 day' from generate_series(1,103)
		returning outbox_row_id) select array_agg(outbox_row_id) from inserted`, record.RecordID).Scan(&routed); err != nil {
		t.Fatal(err)
	}
	var neighborRow int64
	if err := fixture.db.QueryRow(ctx, `insert into public.record_outbox
		(project_id,event_kind,subject_kind,subject_id,authorization_epoch,record_id,expires_at)
		values('default','record_created','record',$1,0,$1,clock_timestamp()+interval '1 hour') returning outbox_row_id`, neighbor.RecordID).Scan(&neighborRow); err != nil {
		t.Fatal(err)
	}
	routed = append(routed, harness.claim.Event.RowID)
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1outboxbatch", ReservationID: "drs_v1outboxbatch",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-outbox-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	adapter, err := recordcollaboration.NewDeletionAdapter(worker)
	if err != nil {
		t.Fatal(err)
	}
	wrong := target
	wrong.OwnerClaim.Owner.Generation++
	if _, err := adapter.PurgeDeletion(ctx, wrong); err == nil {
		t.Fatal("stale V1 owner drained outbox")
	}
	var pending int64
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_outbox where outbox_row_id=any($1::bigint[]) and status in ('pending','processing')`, routed).Scan(&pending); err != nil || pending != int64(len(routed)) {
		t.Fatalf("stale V1 owner changed routed rows=%d err=%v", pending, err)
	}
	var firstBatch int64
	var remaining bool
	err = worker.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, tx *RecordPlatformTransaction) error {
		body, err := legacyAdapterPurgeEnvelope(ctx, tx.tx, target, recorddeletion.RecordCollaborationSurfaceDigest())
		if err != nil {
			return err
		}
		return tx.tx.QueryRow(ctx, `select drained_count,remaining from public.record_outbox_drain_v1($1)`, body).Scan(&firstBatch, &remaining)
	})
	if err != nil || firstBatch != 100 || !remaining {
		t.Fatalf("first committed V1 drain batch=%d remaining=%v err=%v", firstBatch, remaining, err)
	}
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_outbox where outbox_row_id=any($1::bigint[]) and status in ('pending','processing')`, routed).Scan(&pending); err != nil || pending == 0 {
		t.Fatalf("V1 drain did not retain later batch: pending=%d err=%v", pending, err)
	}
	receipt, err := adapter.PurgeDeletion(ctx, target)
	if err != nil {
		t.Fatalf("V1 outbox drain: %v", err)
	}
	var predecessor, starts int64
	var neighborStatus string
	err = fixture.db.QueryRow(ctx, `select
		(select count(*) from public.record_outbox where outbox_row_id=any($1::bigint[]) and status in ('pending','processing')),
		(select count(*) from public.record_notification_delivery_handoffs where operation_id=$2 and delivery_id=$3 and attempt_id is null
		 and legacy_observation_kind='predecessor' and observed_status='possible_external' and legacy_outbox_row_id=$6
		 and octet_length(legacy_observation_digest)=32 and octet_length(legacy_observation)>0),
		(select count(*) from public.record_notification_delivery_starts where record_id=$4),
		(select status from public.record_outbox where outbox_row_id=$5)`, routed, target.Operation.OperationID,
		harness.claim.Event.SubjectID, record.RecordID, neighborRow, harness.claim.Event.RowID).
		Scan(&pending, &predecessor, &starts, &neighborStatus)
	if err != nil || pending != 0 || predecessor != 1 || starts != 0 || neighborStatus != "pending" {
		t.Fatalf("V1 outbox pending=%d predecessor=%d starts=%d neighbor=%q err=%v", pending, predecessor, starts, neighborStatus, err)
	}
	var eventValid bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.browser_record_cleanup_events
		where operation_id=$1 and project_id='default' and record_id=$2 and cleanup_epoch=$3
		and scopes=array['markdown_draft_buffer','markdown_workspace','comparison_workbench','comparison_save_attempt']::text[])
		and not exists(select 1 from public.record_deletion_gates where operation_id=$1)`,
		target.Operation.OperationID, record.RecordID, int64(target.Operation.FenceEpoch)).Scan(&eventValid); err != nil || !eventValid {
		t.Fatalf("V1 collaboration cleanup event valid=%v err=%v", eventValid, err)
	}
	if replay, err := adapter.PurgeDeletion(ctx, target); err != nil || replay != receipt {
		t.Fatalf("V1 outbox receipt replay=%#v err=%v", replay, err)
	}
	if err := adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		t.Fatalf("V1 outbox receipt verify: %v", err)
	}
	if err := adapter.VerifyDeletion(ctx, wrong, receipt); err == nil {
		t.Fatal("stale V1 owner verified receipt")
	}
}

func TestPostgresIntegrationRecordDeletionV1PrePlanSharedArchive(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	actor := storeEvidenceParticipantActor(t)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, localBlobFixtureWitness(t, fixture.Runtime), nil)
	content := []byte("V1 shared archive registered before certified import plan")
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1,
		Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	expires := time.Now().UTC().Add(time.Hour)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "v1-preplan-shared",
		ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	written, err := fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &digestText},
		Content: attachments.PutRequest{ExpectedSHA256: digest, ExpectedSizeBytes: int64(len(content))}}, bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	var expiry time.Time
	var namespace string
	if err := fixture.platform.db.QueryRow(ctx, `select j.expires_at,o.namespace_id::text from public.record_import_jobs j
		join public.record_write_obligations o on o.origin_id=j.import_job_id and o.obligation_id=$2
		where j.import_job_id=$1`, job.ImportJobID, written.ObligationID).Scan(&expiry, &namespace); err != nil {
		t.Fatal(err)
	}
	publication, err := json.Marshal(map[string]any{"artifact_id": "ria_v1preplanshared", "job_id": job.ImportJobID, "kind": "archive",
		"backend_kind": "local", "namespace_id": namespace, "key": written.Version.Key,
		"version": written.Version.VersionID, "sha256": digestText, "size": len(content), "expires_at": expiry, "obligation_id": written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Runtime.Exec(ctx, `select public.record_import_artifact_publish($1)`, publication); err != nil {
		t.Fatal(err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1sharedfirst")
	last := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1sharedlast")
	planDigest := sha256.Sum256([]byte("V1 shared certified archive plan"))
	plan, err := imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: actor,
		ArchiveDigest: digest, PlanDigest: planDigest, ObjectCount: 2, RemapCount: 2,
		Allocations: []recordbirth.Allocation{first, last}, Remaps: []ImportRemap{
			{EntityKind: "record", SourceID: "rec_v1sharedfirst", TargetID: first.RecordID, BirthAllocationID: first.AllocationID},
			{EntityKind: "record", SourceID: "rec_v1sharedlast", TargetID: last.RecordID, BirthAllocationID: last.AllocationID}},
		Documents: []ImportDocumentPlan{{SourceID: "rec_v1sharedfirst", TargetID: first.RecordID, Title: "First", Body: "Body"},
			{SourceID: "rec_v1sharedlast", TargetID: last.RecordID, Title: "Last", Body: "Body"}},
		DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevisions", [][]attachments.ImportedAvailableAttachment{{}, {}}, []recordbirth.Allocation{first, last}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id)
		values($1,$2,$3,$4,'local',$5)`, written.Version.Key, digest[:], written.Version.VersionID, len(content), namespace); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.attachment_quota_accounts set physical_bytes=physical_bytes+$1 where project_id='default'`, len(content)); err != nil {
		t.Fatal(err)
	}
	makeTarget := func(id, suffix string) recorddeletion.PurgeTarget {
		t.Helper()
		record := records.RevisionCommitResult{RecordID: id}
		var lock, epoch int64
		if err := fixture.platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, id).Scan(&record.RevisionID, &lock, &epoch); err != nil {
			t.Fatal(err)
		}
		record.LockVersion, record.AuthorizationEpoch = uint64(lock), uint64(epoch)
		op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
			OperationID: "rpo_v1planshared" + suffix, ReservationID: "drs_v1planshared" + suffix,
			Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: id},
			ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
		return seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture.platform, record, op)
	}
	firstTarget, lastTarget := makeTarget(first.RecordID, "first"), makeTarget(last.RecordID, "last")
	pool := fixture.platform.openDirectRolePool(t, ctx, fixture.platform.deletionWorker, "v1-preplan-shared-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(fixture.LocalCleanup); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	// Hold a real mapping writer open while the worker prepares. The worker
	// must observe the committed mapping after waiting for the same source lock.
	thirdRepository := newRevisionMutationAdmittedRepository(t, fixture)
	third, err := thirdRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, fixture,
		recordsPostgresCompleteRevisionInput(t, "V1 serialized shared owner"), "v1-serialized-shared-owner"))
	if err != nil {
		t.Fatalf("create serialized shared owner: %v", err)
	}
	workerConn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer workerConn.Release()
	workerTx, err := workerConn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = workerTx.Rollback(context.Background()) }()
	var workerPID int32
	if err := workerTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
		t.Fatal(err)
	}
	authority, err := legacyAdapterPurgeEnvelope(ctx, workerTx, firstTarget, surface)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(struct {
		Authority      json.RawMessage `json:"authority"`
		StorageBinding *string         `json:"storage_binding"`
	}{authority, nil})
	if err != nil {
		t.Fatal(err)
	}
	writerTx, err := fixture.platform.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writerTx.Rollback(context.Background()) }()
	var writerPID int32
	if err := writerTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatal(err)
	}
	thirdSource := "rec_v1sharedrace"
	thirdDigest := sha256.Sum256([]byte(thirdSource))
	if _, err := writerTx.Exec(ctx, `insert into public.record_import_entity_mappings
		(import_plan_id,entity_kind,source_id,source_identity_digest,target_id) values($1,'record',$2,$3,$4)`,
		plan.ImportPlanID, thirdSource, thirdDigest[:], third.RecordID); err != nil {
		t.Fatal(err)
	}
	type prepareResult struct {
		projection []byte
		err        error
	}
	preparedFirst := make(chan prepareResult, 1)
	go func() {
		var projection []byte
		err := workerTx.QueryRow(ctx, `select public.record_portability_purge_prepare_v1($1)`, body).Scan(&projection)
		if err == nil {
			err = workerTx.Commit(ctx)
		} else {
			_ = workerTx.Rollback(ctx)
		}
		preparedFirst <- prepareResult{projection: projection, err: err}
	}()
	waitCtx, cancelWait := context.WithTimeout(ctx, 15*time.Second)
	waitForPostgresCollaborationBlocker(t, waitCtx, fixture.platform, workerPID, writerPID)
	cancelWait()
	if err := writerTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	firstResult := <-preparedFirst
	if firstResult.err != nil {
		t.Fatalf("serialized writer-first prepare: %v", firstResult.err)
	}
	firstPrep, err := decodePortabilityV1Preparation(firstResult.projection)
	if err != nil || firstPrep.UnresolvedCount != 0 {
		t.Fatalf("serialized writer-first projection=%#v err=%v", firstPrep, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture.platform, firstTarget.Operation.OperationID, firstPrep.InventoryDigest)
	var writerCaptured bool
	if err := fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_preparations p,
		jsonb_array_elements(p.owners->'mappings') m(value) where p.operation_id=$1 and m.value->>3=$2 and m.value->>4=$3)`,
		firstTarget.Operation.OperationID, thirdSource, third.RecordID).Scan(&writerCaptured); err != nil || !writerCaptured {
		t.Fatalf("committed writer missing from inventory: %v err=%v", writerCaptured, err)
	}
	firstReceipt, err := worker.PurgeRecordPortability(ctx, firstTarget, surface)
	if err != nil {
		t.Fatalf("first real shared archive purge: %v", err)
	}
	t.Run("same_statement_last_owners", func(t *testing.T) {
		tx, err := fixture.platform.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp()
			where obligation_id=$1 and record_id=any($2::text[])`, written.ObligationID, []string{last.RecordID, third.RecordID}); err != nil {
			t.Fatalf("stage target handoff: %v", err)
		}
		result, err := tx.Exec(ctx, `delete from public.record_import_entity_mappings where import_plan_id=$1
			and entity_kind='record' and target_id=any($2::text[])`, plan.ImportPlanID, []string{last.RecordID, third.RecordID})
		if err != nil {
			var denied *pgconn.PgError
			if !errors.As(err, &denied) || (denied.Code != "55000" && denied.Code != "40001") {
				t.Fatalf("remove both mappings: %v", err)
			}
			return
		}
		if result.RowsAffected() != 2 {
			t.Fatalf("same statement removed %d mappings, want 2", result.RowsAffected())
		}
		var removalErr error
		for _, step := range []struct {
			sql  string
			args []any
		}{
			{`delete from public.record_import_artifacts where import_job_id=$1`, []any{job.ImportJobID}},
			{`delete from public.record_import_plans where import_plan_id=$1`, []any{plan.ImportPlanID}},
			{`delete from public.record_import_jobs where import_job_id=$1`, []any{job.ImportJobID}},
			{`delete from public.blob_objects where namespace_id=$1 and blob_key=$2`, []any{namespace, written.Version.Key}},
			{`set constraints all immediate`, nil},
		} {
			if _, removalErr = tx.Exec(ctx, step.sql, step.args...); removalErr != nil {
				break
			}
		}
		var denied *pgconn.PgError
		if !errors.As(removalErr, &denied) || (denied.Code != "55000" && denied.Code != "40001") {
			t.Fatalf("removing both owners without successor = %v", removalErr)
		}
	})
	var stillPresent int
	if err := fixture.platform.db.QueryRow(ctx, `select count(*) from public.record_import_entity_mappings where import_plan_id=$1
		and entity_kind='record' and target_id=any($2::text[])`, plan.ImportPlanID, []string{last.RecordID, third.RecordID}).Scan(&stillPresent); err != nil || stillPresent != 2 {
		t.Fatalf("rolled-back owner mappings=%d err=%v", stillPresent, err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_write_obligation_targets set handed_off_at=clock_timestamp()
		where obligation_id=$1 and record_id=$2`, written.ObligationID, third.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `delete from public.record_import_entity_mappings
		where import_plan_id=$1 and entity_kind='record' and source_id=$2`, plan.ImportPlanID, thirdSource); err != nil {
		t.Fatal(err)
	}
	var retained bool
	err = fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_obligations c
		join public.record_portability_v1_targets t using(operation_id,target_id)
		join public.record_write_obligation_targets ot on ot.obligation_id=c.obligation_id
		where c.operation_id=$1 and c.obligation_id=$2 and c.snapshot->>'plan_digest' is null and t.disposition='retained_shared'
		and ot.record_id=$3 and ot.handed_off_at is null)
		and exists(select 1 from public.record_import_artifacts where import_artifact_id='ria_v1preplanshared')`,
		firstTarget.Operation.OperationID, written.ObligationID, last.RecordID).Scan(&retained)
	if err != nil || !retained {
		t.Fatalf("real shared source retained=%v err=%v", retained, err)
	}
	t.Run("retained_artifact_and_obligation_identity", func(t *testing.T) {
		var artifactFrame, obligationFrame, artifactSaved, obligationSaved string
		err := fixture.platform.db.QueryRow(ctx, `select
			jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','record_import_artifacts',jsonb_build_array(a.import_artifact_id),
			 jsonb_build_array(a.backend_kind,a.blob_key,a.object_version_id,encode(a.sha256,'hex'),a.byte_size,
			 to_char(a.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),a.namespace_id,o.incarnation_id,encode(o.storage_binding,'hex'),a.obligation_id),
			 jsonb_build_array(a.import_job_id,a.artifact_role,jsonb_build_array(j.import_job_id,j.project_id)))::text,
			jsonb_build_array('HOUFENG-PORTABILITY-OWNER-V1','record_write_obligations',jsonb_build_array(o.obligation_id),
			 jsonb_build_array(o.backend_kind,o.canonical_key,o.object_version,encode(o.sha256_digest,'hex'),o.size_bytes,
			 to_char(o.created_at at time zone 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),o.namespace_id,o.incarnation_id,encode(o.storage_binding,'hex')),
			 jsonb_build_array(o.project_id,o.origin_kind,o.origin_id,o.origin_generation,o.record_id,o.record_epoch,o.publication_id))::text,
			(select e.value->>2 from jsonb_array_elements(t.retained_evidence) e(value) where e.value->>0='import_artifact' and e.value->>1=a.import_artifact_id),
			(select e.value->>2 from jsonb_array_elements(t.retained_evidence) e(value) where e.value->>0='write_obligation' and e.value->>1=o.obligation_id)
			from public.record_portability_v1_targets t join public.record_write_obligations o on o.obligation_id=$2
			join public.record_import_artifacts a on a.obligation_id=o.obligation_id
			join public.record_import_jobs j on j.import_job_id=a.import_job_id
			where t.operation_id=$1 and t.disposition='retained_shared' and a.import_artifact_id='ria_v1preplanshared'`, firstTarget.Operation.OperationID, written.ObligationID).
			Scan(&artifactFrame, &obligationFrame, &artifactSaved, &obligationSaved)
		if err != nil {
			t.Fatalf("read retained live owners and parent route: %v", err)
		}
		artifactHash, obligationHash := sha256.Sum256([]byte(artifactFrame)), sha256.Sum256([]byte(obligationFrame))
		if artifactSaved != hex.EncodeToString(artifactHash[:]) || obligationSaved != hex.EncodeToString(obligationHash[:]) {
			t.Fatalf("retained owner digests differ from live source and parent: artifact=%s obligation=%s", artifactSaved, obligationSaved)
		}
	})
	if _, err := fixture.Blobs.Stat(ctx, written.Version); err != nil {
		t.Fatalf("shared bytes lost before final owner: %v", err)
	}
	// Removing every remaining source without a successor must fail at a
	// guard or deferred responsibility check, and leave the live owner intact.
	cut, err := fixture.platform.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		sql  string
		args []any
	}{
		{`update public.record_write_obligation_targets set handed_off_at=clock_timestamp() where obligation_id=$1 and record_id=$2`, []any{written.ObligationID, last.RecordID}},
		{`delete from public.record_import_entity_mappings where import_plan_id=$1`, []any{plan.ImportPlanID}},
		{`delete from public.record_import_artifacts where import_job_id=$1`, []any{job.ImportJobID}},
		{`delete from public.record_import_plans where import_plan_id=$1`, []any{plan.ImportPlanID}},
		{`delete from public.record_import_jobs where import_job_id=$1`, []any{job.ImportJobID}},
		{`delete from public.blob_objects where namespace_id=$1 and blob_key=$2`, []any{namespace, written.Version.Key}},
		{`set constraints all immediate`, nil},
	}
	var cutErr error
	for _, step := range steps {
		if _, cutErr = cut.Exec(ctx, step.sql, step.args...); cutErr != nil {
			break
		}
	}
	if rollbackErr := cut.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback unhanded owner removal: %v", rollbackErr)
	}
	var rejected *pgconn.PgError
	if !errors.As(cutErr, &rejected) || (rejected.Code != "55000" && rejected.Code != "40001") {
		t.Fatalf("last owner removal without handoff=%v", cutErr)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); err != nil {
		t.Fatalf("rejected owner removal changed bytes: %v", err)
	}
	if replay, err := worker.PurgeRecordPortability(ctx, firstTarget, surface); err != nil || replay != firstReceipt {
		t.Fatalf("rejected owner removal changed first receipt=%#v err=%v", replay, err)
	}
	prepared, err := worker.preparePortabilityV1(ctx, lastTarget, surface)
	if err != nil || prepared.UnresolvedCount != 0 || len(prepared.LocalTargets) != 1 {
		t.Fatalf("final real shared preparation=%#v err=%v", prepared, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture.platform, lastTarget.Operation.OperationID, prepared.InventoryDigest)
	if replay, err := worker.PurgeRecordPortability(ctx, firstTarget, surface); err != nil || replay != firstReceipt {
		t.Fatalf("pending shared handoff replay=%#v err=%v", replay, err)
	}
	checkTx, err := fixture.platform.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := checkTx.Exec(ctx, `set constraints all immediate`); err != nil {
		_ = checkTx.Rollback(ctx)
		t.Fatalf("valid pending handoff failed immediate checks: %v", err)
	}
	_, invalidated := checkTx.Exec(ctx, `delete from public.local_blob_gc_obligations where deletion_id=$1 and obligation_id=$2`,
		prepared.LocalTargets[0].DeletionID, written.ObligationID)
	if rollbackErr := checkTx.Rollback(ctx); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	var invalidationError *pgconn.PgError
	if !errors.As(invalidated, &invalidationError) || invalidationError.Code != "55000" {
		t.Fatalf("invalidated pending handoff=%v", invalidated)
	}
	lastReceipt, err := worker.PurgeRecordPortability(ctx, lastTarget, surface)
	if err != nil {
		t.Fatalf("final real shared archive purge: %v", err)
	}
	if _, err := fixture.Blobs.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("final shared archive still exists: %v", err)
	}
	for _, item := range []struct {
		target  recorddeletion.PurgeTarget
		receipt recorddeletion.AdapterPurgeReceipt
	}{{firstTarget, firstReceipt}, {lastTarget, lastReceipt}} {
		if replay, err := worker.PurgeRecordPortability(ctx, item.target, surface); err != nil || replay != item.receipt {
			t.Fatalf("completed real shared handoff replay=%#v err=%v", replay, err)
		}
		if err := worker.VerifyRecordPortabilityPurge(ctx, item.target, surface, item.receipt); err != nil {
			t.Fatal(err)
		}
	}
}

func v1RetainedTerminalNonfiniteSources(t *testing.T) {
	for _, tc := range []struct {
		name, artifactTime, obligationTime string
	}{
		{"artifact_negative_infinity", "-infinity", ""},
		{"obligation_positive_infinity", "", "infinity"},
		{"obligation_negative_infinity", "", "-infinity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			record := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 nonfinite source target")
			neighbor := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 nonfinite source neighbor")
			local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
			written := seedV2ExportArtifact(t, ctx, fixture, local, record.RecordID)
			if tc.artifactTime != "" {
				if _, err := fixture.db.Exec(ctx, `update public.record_export_artifacts set created_at=$1::timestamptz where export_artifact_id='rxa_v2exclusive'`, tc.artifactTime); err != nil {
					t.Fatal(err)
				}
			}
			historicalID := ""
			if tc.obligationTime != "" {
				// Owner-arranged historical metadata: the current managed writer
				// cannot create a nonfinite created_at, and the identity guard
				// correctly forbids changing an existing writer's timestamp.
				// Preserve the real creator's materialized version and copy its
				// registration/return shape for a distinct historical writer.
				historicalID = "rwo_88888888888888888888888888888888"
				writerID := localBlobFixtureUUID(t)
				result, err := fixture.db.Exec(ctx, `insert into public.record_write_obligations
					select (jsonb_populate_record(o,jsonb_build_object(
					 'obligation_id',$2::text,'writer_instance',$3::text,'temporary_key','sha256/.blob-tmp-'||$2::text,
					 'quota_settlement_id',$2::text,'created_at',$4::text,
					 'registration_command',to_jsonb(convert_to((convert_from(o.registration_command,'UTF8')::jsonb ||
					   jsonb_build_object('obligation_id',$2::text,'writer_instance',$3::text))::text,'UTF8')),
					 'return_command',to_jsonb(convert_to((convert_from(o.return_command,'UTF8')::jsonb ||
					   jsonb_build_object('obligation_id',$2::text,'writer_instance',$3::text))::text,'UTF8'))))).*
					from public.record_write_obligations o where o.obligation_id=$1`, written.ObligationID, historicalID, writerID, tc.obligationTime)
				if err != nil || result.RowsAffected() != 1 {
					t.Fatalf("arrange historical obligation: rows=%d err=%v", result.RowsAffected(), err)
				}
				var valid bool
				if err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_local_owner_version(o)
					from public.record_write_obligations o where o.obligation_id=$1`, historicalID).Scan(&valid); err != nil || !valid {
					t.Fatalf("historical source identity valid=%v err=%v", valid, err)
				}
			}
			const sourceSnapshot = `select jsonb_build_array(to_jsonb(a),to_jsonb(j),
				(select jsonb_agg(to_jsonb(w) order by w.obligation_id) from public.record_write_obligations w
				 where w.origin_kind='export_artifact' and w.origin_id=j.export_job_id))::text
				from public.record_export_artifacts a join public.record_export_jobs j using(export_job_id)
				where a.export_artifact_id='rxa_v2exclusive'`
			var beforeSource string
			if err := fixture.db.QueryRow(ctx, sourceSnapshot).Scan(&beforeSource); err != nil {
				t.Fatal(err)
			}
			op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
				OperationID: "rpo_v1nonfinitesource", ReservationID: "drs_v1nonfinitesource",
				Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
				ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
			target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
			pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-nonfinite-worker", 2)
			if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
				t.Fatal(err)
			}
			worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
			coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
				t.Fatal(err)
			}
			surface := recorddeletion.RecordPortabilitySurfaceDigest()
			prep, err := worker.preparePortabilityV1(ctx, target, surface)
			if err != nil || prep.UnresolvedCount != 1 || len(prep.LocalTargets) != 0 || len(prep.S3Targets) != 0 || len(prep.RequiredBackends) != 0 {
				t.Fatalf("nonfinite preparation=%#v err=%v", prep, err)
			}
			assertV1SavedInventoryDigest(t, ctx, fixture, op.OperationID, prep.InventoryDigest)
			var targetID, artifactTime, savedTime, targetTime string
			var removed int64
			var sources, tasks, receipts int
			var valid bool
			checkID := written.ObligationID
			if historicalID != "" {
				checkID = historicalID
			}
			err = fixture.db.QueryRow(ctx, `select t.target_id,a.created_at::text,c.snapshot->>'created_at',t.blob_created_at::text,p.removed_rows,
				(select count(*) from public.record_portability_v1_obligations x where x.operation_id=p.operation_id and x.target_id=t.target_id),
				(select count(*) from public.blob_gc_deletions g where g.blob_key=t.blob_key),
				(select count(*) from public.record_portability_purge_receipts r where r.operation_id=p.operation_id),
				c.snapshot=to_jsonb(w) and t.disposition='unresolved' and t.local_deletion_id is null and t.deletion_state is null
				 and exists(select 1 from public.record_portability_v1_obligations original
					join public.record_write_obligations live on live.obligation_id=original.obligation_id
					where (original.operation_id,original.target_id,original.obligation_id)=(t.operation_id,t.target_id,$3)
					and original.snapshot=to_jsonb(live) and isfinite((original.snapshot->>'created_at')::timestamptz))
				 and record_platform_internal.record_portability_v1_source_links(p.operation_id)
				 and not record_platform_internal.record_portability_v1_eligible(p.operation_id)
				from public.record_portability_v1_preparations p join public.record_portability_v1_targets t on t.operation_id=p.operation_id
				join public.record_portability_v1_artifacts a on (a.operation_id,a.target_id)=(t.operation_id,t.target_id)
				join public.record_portability_v1_obligations c on (c.operation_id,c.target_id)=(t.operation_id,t.target_id)
				join public.record_write_obligations w on w.obligation_id=c.obligation_id
				where p.operation_id=$1 and c.obligation_id=$2`, op.OperationID, checkID, written.ObligationID).
				Scan(&targetID, &artifactTime, &savedTime, &targetTime, &removed, &sources, &tasks, &receipts, &valid)
			wantSources := 1
			if historicalID != "" {
				wantSources = 2
			}
			if err != nil || !valid || removed != 0 || sources != wantSources || tasks != 0 || receipts != 0 ||
				(tc.artifactTime != "" && (artifactTime != tc.artifactTime || targetTime != tc.artifactTime || savedTime == "infinity" || savedTime == "-infinity")) ||
				(tc.obligationTime != "" && (savedTime != tc.obligationTime || artifactTime == "infinity" || artifactTime == "-infinity" ||
					(tc.obligationTime == "infinity" && (targetTime == "infinity" || targetTime == "-infinity")) ||
					(tc.obligationTime == "-infinity" && targetTime != "-infinity"))) {
				t.Fatalf("saved nonfinite source artifact=%s obligation=%s target=%s removed=%d sources=%d tasks=%d receipts=%d valid=%v err=%v", artifactTime, savedTime, targetTime, removed, sources, tasks, receipts, valid, err)
			}
			v1RetainedTerminalRejectNonfiniteAuthorities(t, ctx, fixture, local, worker, target, neighbor, written, targetID)
			var afterSource string
			if err := fixture.db.QueryRow(ctx, sourceSnapshot).Scan(&afterSource); err != nil || afterSource != beforeSource {
				t.Fatalf("source changed after rejected authority: equal=%v err=%v", afterSource == beforeSource, err)
			}
			reader, err := local.blob.Open(ctx, written.Version, attachments.FullByteRange())
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil || !bytes.Equal(data, []byte("exclusive export artifact for V2 deletion")) {
				t.Fatalf("source bytes=%q read=%v close=%v", data, readErr, closeErr)
			}
		})
	}
	t.Run("persisted_manifest_nonfinite_artifact", v1RetainedTerminalNonfiniteManifest)
}

func v1RetainedTerminalRejectNonfiniteAuthorities(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture,
	local *localPublicationFixture, worker *PostgresRecordDeletionRepository, target recorddeletion.PurgeTarget,
	neighbor records.RevisionCommitResult, written attachments.WrittenBlob, targetID string) {
	t.Helper()
	requireCode := func(name string, err error, code string) {
		t.Helper()
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != code {
			t.Fatalf("%s = %v, want SQLSTATE %s", name, err, code)
		}
	}
	n := local.blob.Namespace()
	version := written.Version.VersionID
	request := attachments.LocalCleanupPreparation{DeletionID: "bgd_" + strings.Repeat("ab", 32), NamespaceID: n.NamespaceID,
		IncarnationID: n.IncarnationID, Key: written.Version.Key, ExpectedVersion: &version}
	ordinary := NewPostgresLocalBlobRepository(local.pool, nil, nil)
	_, err := ordinary.PrepareLocalCleanup(ctx, request)
	requireCode("ordinary local prepare", err, "55000")
	v2op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_v2nonfinitealternative",
		ReservationID: "drs_v2nonfinitealternative", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: neighbor.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	v2claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, v2op, neighbor.RevisionID)
	v2id, v2owner, v2generation, v2expiry := v2op.OperationID, v2claim.Owner.OwnerID, int64(v2claim.Owner.Generation), v2claim.Owner.ExpiresAt
	v2request := request
	v2request.DeletionID = "bgd_" + strings.Repeat("cd", 32)
	v2request.LocalCleanupOperation = attachments.LocalCleanupOperation{OperationID: &v2id, OwnerID: &v2owner, Generation: &v2generation, ExpiresAt: &v2expiry}
	v2body, err := json.Marshal(v2request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = worker.pool.Exec(ctx, `select public.local_blob_purge_prepare($1)`, v2body)
	requireCode("V2 local prepare", err, "55000")
	owner := target.OwnerClaim.Owner
	opID, ownerID, generation, expiry := target.Operation.OperationID, owner.OwnerID, int64(owner.Generation), owner.ExpiresAt
	request.LocalCleanupOperation = attachments.LocalCleanupOperation{OperationID: &opID, OwnerID: &ownerID, Generation: &generation, ExpiresAt: &expiry}
	v1 := &portabilityV1LocalCleanupRepository{repository: worker, target: target, surface: recorddeletion.RecordPortabilitySurfaceDigest()}
	if _, err := v1.PrepareLocalCleanup(ctx, request); err == nil {
		t.Fatal("nonfinite inventory authorized V1 local prepare")
	}
	identity := attachments.LocalCleanupIdentity{LocalCleanupOperation: request.LocalCleanupOperation, DeletionID: request.DeletionID,
		NamespaceID: n.NamespaceID, IncarnationID: n.IncarnationID, OwnerID: ownerID, DispatchID: localBlobFixtureUUID(t)}
	current := attachments.LocalCleanupCurrent{LocalCleanupIdentity: identity, Generation: 1, ExpiresAt: &expiry}
	if _, err := v1.ClaimLocalCleanup(ctx, attachments.LocalCleanupClaim{LocalCleanupIdentity: identity, AnchorInode: 1, AnchorNonce: localBlobFixtureUUID(t), Generation: 1, ExpiresAt: &expiry}); err == nil {
		t.Fatal("nonfinite inventory authorized V1 claim")
	}
	if _, err := v1.ResolveLocalCleanup(ctx, identity); err == nil {
		t.Fatal("nonfinite inventory resolved a dispatch")
	}
	if _, err := v1.BindLocalCleanupFinal(ctx, attachments.LocalCleanupFinalBinding{LocalCleanupCurrent: current, InventoryDigest: strings.Repeat("11", 32)}); err == nil {
		t.Fatal("nonfinite inventory authorized final binding")
	}
	if _, err := v1.CompleteLocalCleanup(ctx, attachments.LocalCleanupCompletion{LocalCleanupCurrent: current, Outcome: "already_absent", InventoryDigest: strings.Repeat("11", 32)}); err == nil {
		t.Fatal("nonfinite inventory authorized completion")
	}
	claimRequest := struct {
		DeletionID     string `json:"deletion_id"`
		CleanupOwnerID string `json:"cleanup_owner_id"`
		CleanupLeaseMS int64  `json:"cleanup_lease_ms"`
	}{targetID, ownerID, 60000}
	_, err = worker.portabilityV1S3Command(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest(), `select public.record_portability_gc_claim_v1($1)`, claimRequest)
	requireCode("V1 private S3 claim", err, "55000")
	if _, err := worker.PurgeRecordPortability(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest()); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("nonfinite purge = %v", err)
	}
	for _, verify := range []bool{false, true} {
		if _, err := worker.portabilityV1Finish(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest(), verify); err == nil {
			t.Fatalf("nonfinite inventory authorized finish verify=%v", verify)
		}
	}
	var tasks, receipts, claims int
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.blob_gc_deletions where blob_key=$2),
		(select count(*) from public.record_portability_purge_receipts where operation_id=$1),
		(select count(*) from public.record_portability_v1_targets where operation_id=$1 and deletion_state is not null)`, opID, written.Version.Key).
		Scan(&tasks, &receipts, &claims); err != nil || tasks != 0 || receipts != 0 || claims != 0 {
		t.Fatalf("nonfinite rejection residue tasks=%d receipts=%d claims=%d err=%v", tasks, receipts, claims, err)
	}
}

// The owner arranges a historical saved-manifest fault without changing a
// source row or disabling immutable guards. Its frame hash is internally
// consistent, so eligibility must inspect the captured source timestamp.
func v1RetainedTerminalNonfiniteManifest(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	record := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 nonfinite manifest target")
	neighbor := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 nonfinite manifest neighbor")
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	written := seedV2ExportArtifact(t, ctx, fixture, local, record.RecordID)
	const sourceSnapshot = `select jsonb_build_array(to_jsonb(a),to_jsonb(j),to_jsonb(o))::text,
		isfinite(a.created_at) and isfinite(o.created_at)
		from public.record_export_artifacts a join public.record_export_jobs j using(export_job_id)
		join public.record_write_obligations o on o.obligation_id=a.obligation_id where a.export_artifact_id='rxa_v2exclusive'`
	var beforeSource string
	var finiteSource bool
	if err := fixture.db.QueryRow(ctx, sourceSnapshot).Scan(&beforeSource, &finiteSource); err != nil || !finiteSource {
		t.Fatalf("original source finite=%v err=%v", finiteSource, err)
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_v1nonfinitemanifest",
		ReservationID: "drs_v1nonfinitemanifest", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-nonfinite-manifest-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := legacyAdapterPurgeEnvelope(ctx, tx, target, recorddeletion.RecordPortabilitySurfaceDigest())
	if rollbackErr := tx.Rollback(ctx); err != nil || rollbackErr != nil {
		t.Fatalf("capture caller authority: %v rollback: %v", err, rollbackErr)
	}
	var candidate []byte
	if err := fixture.db.QueryRow(ctx, `select record_platform_internal.record_portability_v1_inventory_candidate($1::jsonb,array['rej_v2exclusive']::text[],array[]::text[],array[]::text[],false)`, string(authority)).Scan(&candidate); err != nil {
		t.Fatal(err)
	}
	var inventory map[string]json.RawMessage
	if err := json.Unmarshal(candidate, &inventory); err != nil {
		t.Fatal(err)
	}
	var unresolved int
	if err := json.Unmarshal(inventory["unresolved_count"], &unresolved); err != nil || unresolved != 0 {
		t.Fatalf("finite source candidate unresolved=%d err=%v", unresolved, err)
	}
	var artifacts [][]json.RawMessage
	if err := json.Unmarshal(inventory["artifacts"], &artifacts); err != nil || len(artifacts) != 1 || len(artifacts[0]) != 15 {
		t.Fatalf("candidate artifact frame=%s err=%v", inventory["artifacts"], err)
	}
	artifacts[0][10] = json.RawMessage(`"infinity"`)
	inventory["artifacts"], err = json.Marshal(artifacts)
	if err != nil {
		t.Fatal(err)
	}
	tampered, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	adminTx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = adminTx.Rollback(context.Background()) }()
	_, err = adminTx.Exec(ctx, `with input as (select $1::jsonb v,$2::jsonb inventory)
		insert into public.record_portability_v1_preparations(operation_id,project_id,record_id,surface_digest,ledger_domain,
		 ledger_sequence,ledger_entry_hash,witness_proof_digest,owners,inventory_digest,removed_rows,pdf_pending)
		select v->>'operation_id','default',v->>'record_id',decode(v->>'surface_digest','hex'),decode(v->>'ledger_domain','hex'),
		 (v->>'ledger_sequence')::bigint,decode(v->>'ledger_entry_hash','hex'),decode(v->>'witness_proof_digest','hex'),inventory->'owners',
		 record_platform_internal.record_portability_v1_frame_digest(v->>'operation_id','default',v->>'record_id',
		  decode(v->>'surface_digest','hex'),decode(v->>'ledger_domain','hex'),(v->>'ledger_sequence')::bigint,
		  decode(v->>'ledger_entry_hash','hex'),decode(v->>'witness_proof_digest','hex'),inventory->'owners',
		  (inventory->>'removed_rows')::bigint,false,inventory->'artifacts',inventory->'obligations',
		  coalesce((select jsonb_agg(jsonb_build_array(x.operation_id,x.target_id,x.backend_kind,x.blob_key,x.object_version_id,
		    encode(x.sha256,'hex'),x.byte_size,to_jsonb(x.blob_created_at at time zone 'UTC'),x.namespace_id,x.incarnation_id,
		    x.obligation_id,encode(x.storage_binding,'hex'),x.disposition,x.retained_evidence,x.local_deletion_id)
		    order by x.target_id collate "C") from jsonb_populate_recordset(null::public.record_portability_v1_targets,inventory->'targets') x),'[]'::jsonb)),
		 (inventory->>'removed_rows')::bigint,false from input`, string(authority), string(tampered))
	if err != nil {
		t.Fatal(err)
	}
	_, err = adminTx.Exec(ctx, `insert into public.record_portability_v1_targets(operation_id,target_id,backend_kind,blob_key,object_version_id,
		sha256,byte_size,blob_created_at,namespace_id,incarnation_id,obligation_id,storage_binding,disposition,retained_evidence,local_deletion_id,deletion_state)
		select x.operation_id,x.target_id,x.backend_kind,x.blob_key,x.object_version_id,x.sha256,x.byte_size,x.blob_created_at,
		 x.namespace_id,x.incarnation_id,x.obligation_id,x.storage_binding,x.disposition,x.retained_evidence,x.local_deletion_id,x.deletion_state
		from jsonb_populate_recordset(null::public.record_portability_v1_targets,($1::jsonb)->'targets') x`, string(tampered))
	if err != nil {
		t.Fatal(err)
	}
	_, err = adminTx.Exec(ctx, `insert into public.record_portability_v1_obligations(operation_id,obligation_id,snapshot,target_id)
		select x.value->>0,x.value->>1,x.value->2,x.value->>3 from jsonb_array_elements(($1::jsonb)->'obligations') x(value)`, string(tampered))
	if err != nil {
		t.Fatal(err)
	}
	_, err = adminTx.Exec(ctx, `insert into public.record_portability_v1_artifacts(operation_id,source_kind,artifact_id,job_id,artifact_role,
		backend_kind,blob_key,object_version_id,sha256,byte_size,created_at,namespace_id,incarnation_id,obligation_id,target_id)
		select x.value->>0,x.value->>1,x.value->>2,x.value->>3,x.value->>4,x.value->>5,x.value->>6,x.value->>7,
		 decode(x.value->>8,'hex'),(x.value->>9)::bigint,(x.value->>10)::timestamp at time zone 'UTC',
		 (x.value->>11)::uuid,(x.value->>12)::uuid,x.value->>13,x.value->>14
		from jsonb_array_elements(($1::jsonb)->'artifacts') x(value)`, string(tampered))
	if err != nil {
		t.Fatal(err)
	}
	result, err := adminTx.Exec(ctx, `insert into public.blob_gc_deletions(deletion_id,project_id,purge_mode,blob_key,sha256_digest,
		backend_kind,deletion_state,operation_id,local_contract_version,namespace_id,incarnation_id)
		select t.local_deletion_id,'default','permanent',t.blob_key,t.sha256,'local','pending',t.operation_id,2,t.namespace_id,t.incarnation_id
		from public.record_portability_v1_targets t where t.operation_id=$1 and t.disposition='local'`, op.OperationID)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("insert historical pending task: rows=%d err=%v", result.RowsAffected(), err)
	}
	if err := adminTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var targetID, taskID, digest string
	var consistent bool
	if err := fixture.db.QueryRow(ctx, `select t.target_id,t.local_deletion_id,encode(p.inventory_digest,'hex'),
		isfinite(t.blob_created_at) and a.created_at='infinity'::timestamptz and t.disposition='local'
		and p.inventory_digest=record_platform_internal.record_portability_v1_inventory_digest(p.operation_id)
		and record_platform_internal.record_portability_v1_source_links(p.operation_id)
		and not record_platform_internal.record_portability_v1_eligible(p.operation_id)
		from public.record_portability_v1_preparations p join public.record_portability_v1_targets t on t.operation_id=p.operation_id
		join public.record_portability_v1_artifacts a on (a.operation_id,a.target_id)=(t.operation_id,t.target_id)
		where p.operation_id=$1`, op.OperationID).Scan(&targetID, &taskID, &digest, &consistent); err != nil || !consistent {
		t.Fatalf("finite target / nonfinite saved artifact consistent=%v err=%v", consistent, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture, op.OperationID, digest)
	v1RetainedTerminalRejectTamperedManifest(t, ctx, fixture, local, worker, target, neighbor, written, targetID, taskID)
	var afterSource string
	if err := fixture.db.QueryRow(ctx, sourceSnapshot).Scan(&afterSource, &finiteSource); err != nil || !finiteSource || beforeSource != afterSource {
		t.Fatalf("tamper changed original source: equal=%v finite=%v err=%v", beforeSource == afterSource, finiteSource, err)
	}
}

func v1RetainedTerminalRejectTamperedManifest(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture,
	local *localPublicationFixture, worker *PostgresRecordDeletionRepository, target recorddeletion.PurgeTarget,
	neighbor records.RevisionCommitResult, written attachments.WrittenBlob, targetID, taskID string) {
	t.Helper()
	requireCode := func(name string, err error, code string) {
		t.Helper()
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != code {
			t.Fatalf("%s = %v, want SQLSTATE %s", name, err, code)
		}
	}
	var before string
	if err := fixture.db.QueryRow(ctx, `select to_jsonb(g)::text from public.blob_gc_deletions g where deletion_id=$1`, taskID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	n := local.blob.Namespace()
	version := written.Version.VersionID
	owner := target.OwnerClaim.Owner
	opID, ownerID, generation, expiry := target.Operation.OperationID, owner.OwnerID, int64(owner.Generation), owner.ExpiresAt
	opFields := attachments.LocalCleanupOperation{OperationID: &opID, OwnerID: &ownerID, Generation: &generation, ExpiresAt: &expiry}
	request := attachments.LocalCleanupPreparation{LocalCleanupOperation: opFields, DeletionID: taskID, NamespaceID: n.NamespaceID,
		IncarnationID: n.IncarnationID, Key: written.Version.Key, ExpectedVersion: &version}
	v1 := &portabilityV1LocalCleanupRepository{repository: worker, target: target, surface: recorddeletion.RecordPortabilitySurfaceDigest()}
	_, err := v1.PrepareLocalCleanup(ctx, request)
	requireCode("tampered V1 local prepare", err, "42501")
	identity := attachments.LocalCleanupIdentity{LocalCleanupOperation: opFields, DeletionID: taskID, NamespaceID: n.NamespaceID,
		IncarnationID: n.IncarnationID, OwnerID: ownerID, DispatchID: localBlobFixtureUUID(t)}
	current := attachments.LocalCleanupCurrent{LocalCleanupIdentity: identity, Generation: 1, ExpiresAt: &expiry}
	_, err = v1.ClaimLocalCleanup(ctx, attachments.LocalCleanupClaim{LocalCleanupIdentity: identity, AnchorInode: 1, AnchorNonce: localBlobFixtureUUID(t)})
	requireCode("tampered V1 local claim", err, "42501")
	_, err = v1.ResolveLocalCleanup(ctx, identity)
	requireCode("tampered V1 local resolve", err, "42501")
	_, err = v1.BindLocalCleanupFinal(ctx, attachments.LocalCleanupFinalBinding{LocalCleanupCurrent: current, InventoryDigest: strings.Repeat("11", 32)})
	requireCode("tampered V1 local bind", err, "42501")
	_, err = v1.CompleteLocalCleanup(ctx, attachments.LocalCleanupCompletion{LocalCleanupCurrent: current, Outcome: "already_absent", InventoryDigest: strings.Repeat("11", 32)})
	requireCode("tampered V1 local complete", err, "42501")
	ordinary := request
	ordinary.LocalCleanupOperation = attachments.LocalCleanupOperation{}
	ordinary.DeletionID = "bgd_" + strings.Repeat("ab", 32)
	_, err = NewPostgresLocalBlobRepository(local.pool, nil, nil).PrepareLocalCleanup(ctx, ordinary)
	requireCode("tampered ordinary local prepare", err, "55000")
	v2op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_v2nonfinitemanifest",
		ReservationID: "drs_v2nonfinitemanifest", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: neighbor.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	v2claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, v2op, neighbor.RevisionID)
	v2id, v2owner, v2generation, v2expiry := v2op.OperationID, v2claim.Owner.OwnerID, int64(v2claim.Owner.Generation), v2claim.Owner.ExpiresAt
	v2request := ordinary
	v2request.LocalCleanupOperation = attachments.LocalCleanupOperation{OperationID: &v2id, OwnerID: &v2owner, Generation: &v2generation, ExpiresAt: &v2expiry}
	body, err := json.Marshal(v2request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = worker.pool.Exec(ctx, `select public.local_blob_purge_prepare($1)`, body)
	requireCode("tampered V2 local prepare", err, "55000")
	claim := map[string]any{"operation_id": opID, "deletion_id": targetID, "project_id": "default", "backend_kind": "s3",
		"key": written.Version.Key, "version_id": version, "sha256": hex.EncodeToString(written.Version.SHA256[:]),
		"size_bytes": written.Version.SizeBytes, "blob_created_at": time.Now().UTC(), "cleanup_owner_id": ownerID,
		"cleanup_owner_generation": 1, "attempt": 1, "cleanup_expires_at": expiry, "storage_binding": strings.Repeat("11", 32)}
	receipt := map[string]any{"key": written.Version.Key, "version_id": version, "sha256": hex.EncodeToString(written.Version.SHA256[:]),
		"size_bytes": written.Version.SizeBytes, "deleted": true, "storage_binding": strings.Repeat("11", 32)}
	for _, probe := range []struct {
		name, query string
		extra       any
	}{
		{"claim", `select public.record_portability_gc_claim_v1($1)`, map[string]any{"deletion_id": targetID, "cleanup_owner_id": ownerID, "cleanup_lease_ms": 60000}},
		{"claim_resolve", `select public.record_portability_gc_claim_resolve_v1($1)`, map[string]any{"claim": claim}},
		{"complete", `select public.record_portability_gc_complete_v1($1)`, map[string]any{"claim": claim, "receipt": receipt}},
		{"retry", `select public.record_portability_gc_retry_v1($1)`, map[string]any{"claim": claim, "retry_at": expiry}},
		{"resolve", `select public.record_portability_gc_resolve_v1($1)`, map[string]any{"claim": claim, "receipt": receipt}},
	} {
		_, err := worker.portabilityV1Command(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest(), probe.query, probe.extra, nil)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "55000" || !strings.Contains(pgerr.Message, "inventory unavailable") {
			t.Fatalf("tampered S3 %s = %v", probe.name, err)
		}
	}
	for _, verify := range []bool{false, true} {
		_, err := worker.portabilityV1Finish(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest(), verify)
		requireCode("tampered portability finish", err, "55000")
	}
	if _, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest()); err == nil {
		t.Fatal("tampered manifest authorized prepare replay")
	}
	var after string
	var intact bool
	err = fixture.db.QueryRow(ctx, `select to_jsonb(g)::text,
		exists(select 1 from public.record_export_artifacts where export_artifact_id='rxa_v2exclusive')
		and exists(select 1 from public.record_write_obligations where obligation_id=$2 and cleanup_completed_at is null and quota_settled_at is null)
		and not exists(select 1 from public.record_portability_purge_receipts where operation_id=$3)
		and not exists(select 1 from public.local_blob_gc_obligations where deletion_id=$1)
		from public.blob_gc_deletions g where deletion_id=$1`, taskID, written.ObligationID, opID).Scan(&after, &intact)
	if err != nil || !intact || after != before {
		t.Fatalf("tampered authority changed task/sources: equal=%v intact=%v err=%v", after == before, intact, err)
	}
	reader, err := local.blob.Open(ctx, written.Version, attachments.FullByteRange())
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(data, []byte("exclusive export artifact for V2 deletion")) {
		t.Fatalf("tampered source bytes=%q read=%v close=%v", data, readErr, closeErr)
	}
}

func TestPostgresIntegrationRecordDeletionV1HistoricalTimestampProducer(t *testing.T) {
	for _, historical := range []bool{false, true} {
		name := "finite_producer"
		if historical {
			name = "historical_negative_infinity"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			record := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 timestamp source")
			local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
			written := seedV2ExportArtifact(t, ctx, fixture, local, record.RecordID)
			if historical {
				if _, err := fixture.db.Exec(ctx, `update public.record_export_artifacts set created_at='-infinity'::timestamptz where export_artifact_id='rxa_v2exclusive'`); err != nil {
					t.Fatal(err)
				}
			}
			const sourceSnapshot = `select jsonb_build_array(to_jsonb(a),to_jsonb(j),to_jsonb(w))::text
				from public.record_export_artifacts a join public.record_export_jobs j using(export_job_id)
				join public.record_write_obligations w on w.obligation_id=a.obligation_id
				where a.export_artifact_id='rxa_v2exclusive'`
			var beforeSource string
			if err := fixture.db.QueryRow(ctx, sourceSnapshot).Scan(&beforeSource); err != nil {
				t.Fatal(err)
			}
			op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
				OperationID: "rpo_v1timestamp", ReservationID: "drs_v1timestamp",
				Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
				ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
			target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
			pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-timestamp-worker", 2)
			if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
				t.Fatal(err)
			}
			coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
			if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
				t.Fatal(err)
			}
			surface := recorddeletion.RecordPortabilitySurfaceDigest()
			prep, err := worker.preparePortabilityV1(ctx, target, surface)
			if err != nil {
				t.Fatalf("timestamp preparation: %v", err)
			}
			assertV1SavedInventoryDigest(t, ctx, fixture, target.Operation.OperationID, prep.InventoryDigest)
			var artifactTime, targetTime, disposition string
			var removed int64
			var sourceCount, taskCount, receiptCount int
			err = fixture.db.QueryRow(ctx, `select a.created_at::text,t.blob_created_at::text,t.disposition,p.removed_rows,
				(select count(*) from public.record_export_artifacts where export_artifact_id='rxa_v2exclusive'),
				(select count(*) from public.blob_gc_deletions where operation_id=p.operation_id),
				(select count(*) from public.record_portability_purge_receipts where operation_id=p.operation_id)
				from public.record_portability_v1_preparations p join public.record_portability_v1_artifacts a using(operation_id)
				join public.record_portability_v1_targets t using(operation_id,target_id)
				where p.operation_id=$1 and a.artifact_id='rxa_v2exclusive'`, target.Operation.OperationID).
				Scan(&artifactTime, &targetTime, &disposition, &removed, &sourceCount, &taskCount, &receiptCount)
			if err != nil {
				t.Fatal(err)
			}
			if historical {
				if artifactTime != "-infinity" || targetTime != "-infinity" || disposition != "unresolved" || removed != 0 || sourceCount != 1 || taskCount != 0 || receiptCount != 0 || prep.UnresolvedCount != 1 || len(prep.LocalTargets) != 0 || len(prep.S3Targets) != 0 {
					t.Fatalf("nonfinite inventory time=%q/%q disposition=%q removed=%d source=%d tasks=%d receipts=%d projection=%#v", artifactTime, targetTime, disposition, removed, sourceCount, taskCount, receiptCount, prep)
				}
				if _, err := worker.PurgeRecordPortability(ctx, target, surface); !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
					t.Fatalf("nonfinite purge=%v", err)
				}
				if _, err := worker.portabilityV1Finish(ctx, target, surface, false); err == nil {
					t.Fatal("nonfinite inventory authorized receipt")
				}
				var afterSource string
				var keyTasks int
				if err := fixture.db.QueryRow(ctx, sourceSnapshot).Scan(&afterSource); err != nil || afterSource != beforeSource {
					t.Fatalf("nonfinite source changed: equal=%v err=%v", afterSource == beforeSource, err)
				}
				if err := fixture.db.QueryRow(ctx, `select count(*) from public.blob_gc_deletions where blob_key=$1`, written.Version.Key).Scan(&keyTasks); err != nil || keyTasks != 0 {
					t.Fatalf("nonfinite key tasks=%d err=%v", keyTasks, err)
				}
			} else if disposition != "local" || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 || sourceCount != 0 || taskCount != 1 || receiptCount != 0 {
				t.Fatalf("finite inventory disposition=%q source=%d tasks=%d receipts=%d projection=%#v", disposition, sourceCount, taskCount, receiptCount, prep)
			}
			if _, err := local.blob.Stat(ctx, written.Version); err != nil {
				t.Fatalf("preparation changed source bytes: %v", err)
			}
		})
	}
}

// Render the documented JSON frame from saved columns and hash it in Go,
// independently of the producer's digest function.
func assertV1SavedInventoryDigest(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, operationID, projected string) {
	t.Helper()
	var frame string
	var stored []byte
	err := fixture.db.QueryRow(ctx, `select jsonb_build_array('HOUFENG-PORTABILITY-INVENTORY-V1',p.operation_id,p.project_id,p.record_id,
		encode(p.surface_digest,'hex'),encode(p.ledger_domain,'hex'),p.ledger_sequence,encode(p.ledger_entry_hash,'hex'),encode(p.witness_proof_digest,'hex'),
		p.owners,p.removed_rows,p.pdf_pending,
		coalesce((select jsonb_agg(jsonb_build_array(a.operation_id,a.source_kind,a.artifact_id,a.job_id,a.artifact_role,a.backend_kind,a.blob_key,a.object_version_id,encode(a.sha256,'hex'),a.byte_size,to_jsonb(a.created_at at time zone 'UTC'),a.namespace_id,a.incarnation_id,a.obligation_id,a.target_id) order by a.source_kind collate "C",a.artifact_id collate "C") from public.record_portability_v1_artifacts a where a.operation_id=p.operation_id),'[]'::jsonb),
		coalesce((select jsonb_agg(jsonb_build_array(o.operation_id,o.obligation_id,o.snapshot,o.target_id) order by o.obligation_id collate "C") from public.record_portability_v1_obligations o where o.operation_id=p.operation_id),'[]'::jsonb),
		coalesce((select jsonb_agg(jsonb_build_array(t.operation_id,t.target_id,t.backend_kind,t.blob_key,t.object_version_id,encode(t.sha256,'hex'),t.byte_size,to_jsonb(t.blob_created_at at time zone 'UTC'),t.namespace_id,t.incarnation_id,t.obligation_id,encode(t.storage_binding,'hex'),t.disposition,t.retained_evidence,t.local_deletion_id) order by t.target_id collate "C") from public.record_portability_v1_targets t where t.operation_id=p.operation_id),'[]'::jsonb))::text,p.inventory_digest
		from public.record_portability_v1_preparations p where p.operation_id=$1`, operationID).Scan(&frame, &stored)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(frame))
	if !bytes.Equal(digest[:], stored) || hex.EncodeToString(digest[:]) != projected {
		t.Fatal("saved inventory frame does not match projected digest")
	}
}

func TestPostgresIntegrationRecordDeletionV1ObligationOnlyAbsent(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	actor := storeEvidenceParticipantActor(t)
	witness := localBlobFixtureWitness(t, fixture.Runtime)
	imports := NewPostgresRecordPortabilityRepository(fixture.Runtime, fixture.Gate, witness, nil)
	content := []byte("registered V1 archive with no final object")
	digest := sha256.Sum256(content)
	hexDigest := hex.EncodeToString(digest[:])
	destination := records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1,
		Kind: records.SubjectKindVPS, Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	expires := time.Now().UTC().Add(time.Hour)
	job, err := imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: actor.UserID, IdempotencyKey: "v1-obligation-absent",
		ArchiveDigest: digest, DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	namespace, incarnation := fixture.LocalCleanup.NamespaceID(), fixture.LocalCleanup.IncarnationID()
	registration := attachments.WriteRegistration{WriteOrigin: attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID,
		Generation: int64(job.LockVersion), ArchiveDigest: &hexDigest}, ObligationID: "rwo_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", ""),
		WriterInstance: localBlobFixtureUUID(t), RequestSeq: 1, ProjectID: "default", NamespaceID: &namespace, IncarnationID: &incarnation,
		BackendKind: attachments.BackendKindLocal, Key: "sha256/" + hexDigest, SHA256: hexDigest, Size: int64(len(content))}
	if _, err := NewPostgresLocalBlobRepository(fixture.Runtime, witness, nil).RegisterWrite(ctx, registration); err != nil {
		t.Fatal(err)
	}
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(fixture.Runtime, fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, actor, job, "rec_v1obligationonly")
	planDigest := sha256.Sum256([]byte("V1 registered obligation archive plan"))
	_, err = imports.SaveImportPlan(ctx, SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: actor, ArchiveDigest: digest,
		PlanDigest: planDigest, ObjectCount: 1, RemapCount: 1, Allocations: []recordbirth.Allocation{allocation},
		Remaps:             []ImportRemap{{EntityKind: "record", SourceID: "rec_v1obligationonly", TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID}},
		Documents:          []ImportDocumentPlan{{SourceID: "rec_v1obligationonly", TargetID: allocation.RecordID, Title: "No final", Body: "Body"}},
		DestinationSubject: destination, ExpiresAt: expires})
	if err != nil {
		t.Fatal(err)
	}
	fixture.RefreshHeartbeat()
	if err := fixture.CommitImported(ctx, "CommitRevision", [][]attachments.ImportedAvailableAttachment{{}}, []recordbirth.Allocation{allocation}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	record := records.RevisionCommitResult{RecordID: allocation.RecordID}
	var lock, epoch int64
	if err := fixture.platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, record.RecordID).
		Scan(&record.RevisionID, &lock, &epoch); err != nil {
		t.Fatal(err)
	}
	record.LockVersion, record.AuthorizationEpoch = uint64(lock), uint64(epoch)
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_v1absentobligation",
		ReservationID: "drs_v1absentobligation", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture.platform, record, op)
	pool := fixture.platform.openDirectRolePool(t, ctx, fixture.platform.deletionWorker, "v1-obligation-absent-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(fixture.LocalCleanup); err != nil {
		t.Fatal(err)
	}
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	prep, err := worker.preparePortabilityV1(ctx, target, surface)
	if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 || prep.LocalTargets[0].ExpectedVersion != "local-v2-"+registration.ObligationID {
		t.Fatalf("absent obligation prepare=%#v err=%v", prep, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture.platform, target.Operation.OperationID, prep.InventoryDigest)
	var actualNull, snapshotNull, noArtifact bool
	err = fixture.platform.db.QueryRow(ctx, `select t.object_version_id is null,c.snapshot->>'object_version' is null,
		not exists(select 1 from public.record_portability_v1_artifacts where operation_id=t.operation_id)
		from public.record_portability_v1_targets t join public.record_portability_v1_obligations c using(operation_id,target_id)
		where t.operation_id=$1 and c.obligation_id=$2`, target.Operation.OperationID, registration.ObligationID).Scan(&actualNull, &snapshotNull, &noArtifact)
	if err != nil || !actualNull || !snapshotNull || !noArtifact {
		t.Fatalf("absent original identity actual=%v snapshot=%v noArtifact=%v err=%v", actualNull, snapshotNull, noArtifact, err)
	}
	receipt, err := worker.PurgeRecordPortability(ctx, target, surface)
	if err != nil {
		t.Fatal(err)
	}
	var absent bool
	err = fixture.platform.db.QueryRow(ctx, `select g.deletion_state='completed' and g.final_identity_bound and not g.final_was_present
		and g.object_version is null and g.size_bytes is null and g.final_device is null and g.final_inode is null
		and g.physical_delete_result='already_absent' and l.seal_obligation and o.cleanup_completed_at is not null
		and o.cleanup_receipt=g.receipt_digest and c.snapshot->>'object_version' is null and t.object_version_id is null
		from public.record_portability_v1_targets t join public.record_portability_v1_obligations c using(operation_id,target_id)
		join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.local_blob_gc_obligations l on l.deletion_id=g.deletion_id and l.obligation_id=c.obligation_id
		join public.record_write_obligations o on o.obligation_id=c.obligation_id
		where t.operation_id=$1 and c.obligation_id=$2`, target.Operation.OperationID, registration.ObligationID).Scan(&absent)
	if err != nil || !absent {
		t.Fatalf("absent physical binding=%v err=%v", absent, err)
	}
	if err := worker.VerifyRecordPortabilityPurge(ctx, target, surface, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresIntegrationRecordDeletionV1UnknownLocalAlternativeGC(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	record := seedRecordWithoutSearchProjection(t, ctx, fixture, "V1 unknown local alternative")
	neighbor := seedRecordWithoutSearchProjection(t, ctx, fixture, "V2 alternative cleanup owner")
	local := newLocalPublicationFixtureFor(t, ctx, fixture, fixture.admissionGate)
	digest := sha256.Sum256([]byte("unadopted historical local artifact"))
	key := "sha256/" + hex.EncodeToString(digest[:])
	seedPortabilityExport(t, ctx, fixture, "rej_v1unknownlocal", "rxa_v1unknownlocal", record.RecordID, record.RevisionID, digest, "published")
	if _, err := fixture.db.Exec(ctx, `update public.record_export_artifacts set blob_key=$1,object_version_id='local-v1-historical' where export_artifact_id='rxa_v1unknownlocal'`, key); err != nil {
		t.Fatal(err)
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_v1unknownlocal",
		ReservationID: "drs_v1unknownlocal", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-unknown-local-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	surface := recorddeletion.RecordPortabilitySurfaceDigest()
	prep, err := worker.preparePortabilityV1(ctx, target, surface)
	if err != nil || prep.UnresolvedCount != 1 || len(prep.LocalTargets) != 0 || len(prep.RequiredBackends) != 0 {
		t.Fatalf("unknown local inventory=%#v err=%v", prep, err)
	}
	assertV1SavedInventoryDigest(t, ctx, fixture, target.Operation.OperationID, prep.InventoryDigest)
	n := local.blob.Namespace()
	request := attachments.LocalCleanupPreparation{DeletionID: "bgd_" + strings.Repeat("ab", 32), NamespaceID: n.NamespaceID,
		IncarnationID: n.IncarnationID, Key: key}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var task []byte
	err = local.pool.QueryRow(ctx, `select to_jsonb(public.local_blob_gc_prepare($1))`, body).Scan(&task)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "55000" {
		t.Fatalf("ordinary local alternative=%v, want 55000", err)
	}
	v2op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV2, OperationID: "rpo_v2unknownalternate",
		ReservationID: "drs_v2unknownalternate", Object: recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: neighbor.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 7}
	v2claim := seedLocalAttachmentDeletionOperation(t, ctx, fixture, v2op, neighbor.RevisionID)
	v2id, v2owner, v2generation, v2expiry := v2op.OperationID, v2claim.Owner.OwnerID, int64(v2claim.Owner.Generation), v2claim.Owner.ExpiresAt
	v2request := request
	v2request.DeletionID = "bgd_" + strings.Repeat("cd", 32)
	v2request.LocalCleanupOperation = attachments.LocalCleanupOperation{OperationID: &v2id, OwnerID: &v2owner, Generation: &v2generation, ExpiresAt: &v2expiry}
	v2body, err := json.Marshal(v2request)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `select to_jsonb(public.local_blob_purge_prepare($1))`, v2body).Scan(&task)
	denied = nil
	if !errors.As(err, &denied) || denied.Code != "55000" {
		t.Fatalf("V2 local alternative=%v, want 55000", err)
	}
	owner := target.OwnerClaim.Owner
	operationID, ownerID, generation, expiry := target.Operation.OperationID, owner.OwnerID, int64(owner.Generation), owner.ExpiresAt
	request.LocalCleanupOperation = attachments.LocalCleanupOperation{OperationID: &operationID, OwnerID: &ownerID, Generation: &generation, ExpiresAt: &expiry}
	v1 := &portabilityV1LocalCleanupRepository{repository: worker, target: target, surface: surface}
	if _, err := v1.PrepareLocalCleanup(ctx, request); err == nil {
		t.Fatal("unresolved manifest authorized V1 local task")
	}
	identity := attachments.LocalCleanupIdentity{LocalCleanupOperation: request.LocalCleanupOperation, DeletionID: request.DeletionID,
		NamespaceID: n.NamespaceID, IncarnationID: n.IncarnationID, OwnerID: ownerID, DispatchID: localBlobFixtureUUID(t)}
	current := attachments.LocalCleanupCurrent{LocalCleanupIdentity: identity, Generation: 1, ExpiresAt: &expiry}
	if _, err := v1.ClaimLocalCleanup(ctx, attachments.LocalCleanupClaim{LocalCleanupIdentity: identity, AnchorInode: 1, AnchorNonce: localBlobFixtureUUID(t), Generation: 1, ExpiresAt: &expiry}); err == nil {
		t.Fatal("unresolved manifest authorized V1 local claim")
	}
	if _, err := v1.BindLocalCleanupFinal(ctx, attachments.LocalCleanupFinalBinding{LocalCleanupCurrent: current, InventoryDigest: strings.Repeat("11", 32)}); err == nil {
		t.Fatal("unresolved manifest authorized V1 final binding")
	}
	if _, err := v1.CompleteLocalCleanup(ctx, attachments.LocalCleanupCompletion{LocalCleanupCurrent: current, Outcome: "already_absent", InventoryDigest: strings.Repeat("11", 32)}); err == nil {
		t.Fatal("unresolved manifest authorized V1 local completion")
	}
	var preserved bool
	err = fixture.db.QueryRow(ctx, `select exists(select 1 from public.record_export_artifacts where export_artifact_id='rxa_v1unknownlocal'
		and namespace_id is null and blob_key=$2 and object_version_id='local-v1-historical')
		and exists(select 1 from public.record_portability_v1_targets where operation_id=$1 and namespace_id is null
		and blob_key=$2 and object_version_id='local-v1-historical' and disposition='unresolved' and local_deletion_id is null)
		and not exists(select 1 from public.blob_gc_deletions where blob_key=$2)
		and not exists(select 1 from public.record_portability_purge_receipts where operation_id=$1)`, target.Operation.OperationID, key).Scan(&preserved)
	if err != nil || !preserved {
		t.Fatalf("unknown alternative changed evidence=%v err=%v", preserved, err)
	}
}
