#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s --profile local|s3\n' "$0" >&2
}

profile=""
while [ "$#" -gt 0 ]
do
  case "$1" in
    --profile)
      profile=${2-}
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

case "$profile" in
  local|s3)
    ;;
  *)
    usage
    exit 2
    ;;
esac

if ! command -v docker >/dev/null 2>&1
then
  printf 'docker is required for records integration profiles\n' >&2
  exit 1
fi

root=$(cd "$(dirname "$0")/.." && pwd)
records_runner_kind=integration
records_run_id=
workspace=
containers=()
volumes=()
source "$root/scripts/lib/records-runner-lifecycle.sh"
records_runner_install_cleanup
records_runner_validate_run_id_override
records_runner_require_signal_tools
workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-records-integration.XXXXXX")
records_runner_prepare_run_id
selected_ports=()
picked_port=

random_password() {
  od -An -N18 -tx1 /dev/urandom | tr -d ' \n'
}

pick_free_port() {
  start_port=$1
  for candidate in $(seq "$start_port" 57999)
  do
    case " ${selected_ports[*]} " in
      *" ${candidate} "*)
        continue
        ;;
    esac
    if ! ss -ltn "sport = :${candidate}" | grep -q LISTEN
    then
      selected_ports+=("$candidate")
      picked_port=$candidate
      return 0
    fi
  done
  return 1
}

start_minio() {
  name=$1
  port=$2
  access=$3
  secret=$4
  containers+=("$name")
  docker run -d \
    --name "$name" \
    --label "com.houfeng.records.runner=$records_runner_kind" \
    --label "com.houfeng.records.run=$records_run_id" \
    --label "com.houfeng.records.owner=$records_owner_id" \
    --network=host \
    -e MINIO_ROOT_USER="$access" \
    -e MINIO_ROOT_PASSWORD="$secret" \
    --mount "type=volume,source=$minio_volume,target=/data" \
    minio/minio:RELEASE.2024-12-18T13-15-44Z \
    server /data --address "127.0.0.1:${port}" \
    --console-address "127.0.0.1:0" >/dev/null
}

wait_for_minio() {
  name=$1
  port=$2
  for attempt in $(seq 1 30)
  do
    if docker exec "$name" curl -fsS "http://127.0.0.1:${port}/minio/health/live" >/dev/null 2>&1
    then
      return 0
    fi
    sleep 1
  done
  docker logs "$name" >&2
  return 1
}

minio_args=()
if [ "$profile" = "s3" ]
then
  pick_free_port 57000
  minio_port=$picked_port
  minio_access=houfengminio
  minio_secret=$(random_password)
  minio_name="houfeng-records-minio-${records_run_id}-${RANDOM}${RANDOM}"
  minio_volume="houfeng-records-integration-minio-${records_owner_id}"
  volumes+=("$minio_volume")
  docker volume create \
    --label "com.houfeng.records.runner=$records_runner_kind" \
    --label "com.houfeng.records.run=$records_run_id" \
    --label "com.houfeng.records.owner=$records_owner_id" \
    "$minio_volume" >/dev/null
  records_runner_verify_volume_ownership "$minio_volume"
  start_minio "$minio_name" "$minio_port" "$minio_access" "$minio_secret"
  wait_for_minio "$minio_name" "$minio_port"
  minio_args=(
    env
    HOUFENG_MINIO_INTEGRATION=1
    HOUFENG_MINIO_ENDPOINT="127.0.0.1:${minio_port}"
    HOUFENG_MINIO_ACCESS_KEY="$minio_access"
    HOUFENG_MINIO_SECRET_KEY="$minio_secret"
    HOUFENG_MINIO_BUCKET="houfeng-records"
    HOUFENG_MINIO_SECURE=false
  )
fi

# The postgres fixture runner exports HOUFENG_POSTGRES_INTEGRATION=1 and
# fails the child on any `--- SKIP:`. This script keeps that contract.
export HOUFENG_POSTGRES_INTEGRATION=1
stdout_file="$workspace/child-stdout.log"
stderr_file="$workspace/child-stderr.log"
report_file="$workspace/profile-report.json"

# Select the acceptance parents explicitly: process helper entrypoints are not
# acceptance tests, even when they return successfully without their child env.
required_tests=(
  TestPostgresIntegrationAttachmentProcessorCrashCutpointsConvergeAfterRestart
  TestLocalBlobManagedNamespaceAndGuard
  TestPostgresIntegrationLocalBlobObligationLifecycle
  TestPostgresIntegrationRecordDeletionV2WorkerLifecycle
  TestPostgresIntegrationRecordDeletionV2FinishResponsibilities
  TestPostgresIntegrationRecordDeletionV2LocalAuthority
  TestPostgresIntegrationRecordDeletionV2TransactionLocks
  TestPostgresIntegrationRecordDeletionV2LocalDispatch
  TestPostgresIntegrationRecordDeletionV2Issuer
  TestPostgresIntegrationRecordDeletionV1ClientAuthority
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation
  TestPostgresIntegrationLocalDraftContract
  TestPostgresIntegrationLocalDraftCleanupContract
  TestPostgresIntegrationLocalPublicationContract
  TestPostgresIntegrationLocalWriteCommitLoss
  TestPostgresIntegrationLocalMutationAuthority
  TestPostgresIntegrationLocalPublicationRepeatableRead
)
required_subtests=(
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/client_event_and_outbox
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/known_local_and_shared_handoff
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/unresolved_inventory_no_dispatch
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/unresolved_inventory_no_dispatch/historical_timestamp/finite_producer
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/unresolved_inventory_no_dispatch/historical_timestamp/historical_negative_infinity
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/unresolved_inventory_no_dispatch/nonfinite_sources/artifact_negative_infinity
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/unresolved_inventory_no_dispatch/nonfinite_sources/obligation_positive_infinity
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/unresolved_inventory_no_dispatch/nonfinite_sources/obligation_negative_infinity
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/unresolved_inventory_no_dispatch/nonfinite_sources/persisted_manifest_nonfinite_artifact
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/client_event_and_outbox/late_provider_and_rollback/started_late_provider_root_independent
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/client_event_and_outbox/late_provider_and_rollback/client_comparison_rollback_atomic
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/physical_crash_cuts/claim_commit_unknown
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/physical_crash_cuts/bind_commit_unknown
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/physical_crash_cuts/physical_return_loss_restart
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/physical_crash_cuts/owner_expiry_receipt_rollback
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/physical_crash_cuts/closed_no_final_sources
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/physical_crash_cuts/resolve_no_lease_extension
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/command_authority_matrix/all_wrapper_acl
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/command_authority_matrix/all_wrong_authority_no_effects
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/command_authority_matrix/local_claim_tuple_tamper
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/authority_and_crash_cuts/command_authority_matrix/verify_readonly_missing_proof
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/genuine_shared_archive/same_statement_last_owners
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/genuine_shared_archive/retained_artifact_and_obligation_identity
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/attachment_only
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/upload_part_only
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/artifact_only
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/obligation_only
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/handoff_order_and_prerequisites
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/last_owner_serialization
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/parent_route_and_early_constraint_invalidation
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/pending_key_and_distinct_identity
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/creator_first
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/deduplicated_first
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/concurrent_distinct_owners
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/concurrent_distinct_owners/read_committed
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/concurrent_distinct_owners/repeatable_read
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/concurrent_independent_exports
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/concurrent_independent_exports/read_committed
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/concurrent_independent_exports/repeatable_read
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/unknown_writer
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/isolated_owner_handoffs/independent_same_version_local/parent_route_and_creator_identity
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/no_manifest_default_repeatable_read
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/snapshot_before_prepare_repeatable_read
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/snapshot_before_prepare_read_committed
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/prepare_before_writer/default_read_committed
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/prepare_before_writer/default_repeatable_read
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/writer_before_prepare/default_read_committed
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/writer_before_prepare/default_repeatable_read
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/ordinary_gc_unrelated_waits/default_read_committed
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/epoch_serialization/ordinary_gc_unrelated_waits/default_repeatable_read
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_duplicate_target
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_extra_artifact
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/orphan_pending_task
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/duplicate_identity_task
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/missing_portability_receipt
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/forged_portability_receipt
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/physical_proof_tamper
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/task_inventory
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/task_command
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/registered_task
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/source_snapshot
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/artifact_snapshot
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/sealed_link
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/target
  TestPostgresIntegrationRecordDeletionV1AdapterContinuation/reference_serialization_and_terminal_sets/nine_receipt_terminal_control/manifest
  TestPostgresIntegrationLocalWriteCommitLoss/dispatch_ack_lost_resolved
  TestPostgresIntegrationLocalWriteCommitLoss/dispatch_ack_lost_resolve_unavailable
  TestPostgresIntegrationLocalWriteCommitLoss/return_ack_lost_resolved
  TestPostgresIntegrationLocalWriteCommitLoss/return_ack_lost_resolve_unavailable
)
if [ "$profile" = "local" ]; then
  required_tests+=(
    TestLocalBlobStorePutRejectsTypedNilReaderBeforeFilesystemSideEffects
    TestLocalBlobStoreUsesPrivateModesAndSingleConditionalPublish
    TestLocalBlobStoreFailureCutpointsPreserveOwnedRecoveryTargets
    TestLocalBlobStoreRejectsSymlinkRoot
    TestLocalBlobStoreResolvesFinalPublicationByExactDigestKey
    TestLocalBlobStorePublicationResolverRejectsMissingSizeAndHashDrift
    TestPostgresIntegrationLocalBlobProcessRecovery
    TestPostgresIntegrationLocalNamespaceAdminLifecycle
    TestPostgresIntegrationLocalBlobNamespaceAuthority
    TestPostgresIntegrationLocalRecoveryInventoryRetainsOwnerlessResponsibilities
    TestPostgresIntegrationLocalPortabilitySharedArchiveHandoff
    TestPostgresIntegrationLocalPublicationConsumptionRollbackAndReplay
    TestPostgresIntegrationLocalPreviewTakeoverRetainsExactPublishedVersion
    TestPostgresIntegrationAttachmentBlobPublicationPrepareReplayUsesCurrentActiveIntent
    TestPostgresIntegrationAttachmentUploadServiceLocalWorkflow
    TestPostgresIntegrationAttachmentBlobGCLocalWorkerWorkflow
    TestPostgresIntegrationAttachmentDeletionPreservesSharedAndPurgesExclusiveBlob
    TestPostgresIntegrationRevisionBatchPreservesSequentialSemantics
    TestPostgresIntegrationRevisionBatchAcquiresRootsBeforeBlobParticipants
  )
  required_subtests+=(
    TestLocalBlobStoreFailureCutpointsPreserveOwnedRecoveryTargets/after-temp-create
    TestLocalBlobStoreFailureCutpointsPreserveOwnedRecoveryTargets/after-copy
    TestLocalBlobStoreFailureCutpointsPreserveOwnedRecoveryTargets/after-file-sync
    TestLocalBlobStoreFailureCutpointsPreserveOwnedRecoveryTargets/before-publish
    TestLocalBlobStoreFailureCutpointsPreserveOwnedRecoveryTargets/after-publish
    TestLocalBlobManagedNamespaceAndGuard/raw_mutation_has_no_authority
    TestLocalBlobManagedNamespaceAndGuard/runtime_never_provisions
    TestLocalBlobManagedNamespaceAndGuard/guard_cancellation_independent_keys_and_revocation
    TestLocalBlobManagedNamespaceAndGuard/registered_anchor_is_never_recreated
    TestLocalBlobManagedNamespaceAndGuard/directory_replacement_prevents_write
    TestLocalBlobManagedNamespaceAndGuard/unknown_dispatch_prevents_bytes
    TestLocalBlobManagedNamespaceAndGuard/committed_dispatch_readback_allows_one_attempt
    TestLocalBlobManagedNamespaceAndGuard/unknown_return_keeps_both_owned_names
    TestPostgresIntegrationLocalBlobObligationLifecycle/unpublished_orphan_after_origin_deletion_and_reopen
    TestPostgresIntegrationLocalBlobProcessRecovery/two_process_same_key_wait_cancel_and_independent_key
    TestPostgresIntegrationLocalBlobProcessRecovery/after-temp-open
    TestPostgresIntegrationLocalBlobProcessRecovery/after-temp-identity
    TestPostgresIntegrationLocalBlobProcessRecovery/after-temp-registration
    TestPostgresIntegrationLocalBlobProcessRecovery/after-temp-link
    TestPostgresIntegrationLocalBlobProcessRecovery/after-file-sync
    TestPostgresIntegrationLocalBlobProcessRecovery/after-publish
    TestPostgresIntegrationLocalBlobProcessRecovery/after-delete
  )
else
  required_tests+=(
    TestPostgresMinIOIntegrationPublicationCleanupContract
    TestPostgresMinIOIntegrationManagedWriteSourceBinding
    TestPostgresMinIOIntegrationRecordDeletionV1Portability
  )
  required_subtests+=(
    TestPostgresMinIOIntegrationManagedWriteSourceBinding/artifact
    TestPostgresMinIOIntegrationManagedWriteSourceBinding/obligation
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/historical_version_preserves_current
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/historical_version_preserves_current/artifact
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/historical_version_preserves_current/obligation_only
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/same_object_artifact_dedup
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners/carry_and_delete
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners/prepare_commit_unknown
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners/guarded_negative_inputs
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners/guarded_negative_inputs/forged_predecessor_evidence
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners/guarded_negative_inputs/altered_binding_or_version
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners/guarded_negative_inputs/assigned_elsewhere
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners/guarded_negative_inputs/absent_carried_source
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/independent_same_version_owners/concurrent_successor_prepares
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/shared_archive_final_handoff
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/unresolved_sources_no_dispatch
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/unresolved_sources_no_dispatch/legacy_null_route_and_version
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/unresolved_sources_no_dispatch/remote_unknown
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/claim_readback_and_tamper
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/prepare_commit_unknown
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/prepare_commit_unknown/prepare
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/prepare_commit_unknown/claim
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/unconfirmed_candidate_no_io
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/unconfirmed_candidate_no_io/prepare
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/unconfirmed_candidate_no_io/claim
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/delete_ack_loss_process_restart
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/route_binding_mismatch
    TestPostgresMinIOIntegrationRecordDeletionV1Portability/route_post_io_identity_mismatch
  )
fi
extra_test_regex="^($(IFS='|'; printf '%s' "${required_tests[*]}"))$"

set +e
(
  cd "$root"
  if [ "$profile" = "s3" ]
  then
    exec setsid env \
      --default-signal=INT \
      --default-signal=TERM \
      HOUFENG_RECORDS_KEEP_WORKSPACE=0 \
      "$root/scripts/test-record-platform-integration.sh" postgres -- \
      "${minio_args[@]}" \
      go test -v -p 1 -timeout=30m ./internal/center/recordbackup ./internal/center/recordrestore ./internal/center/store ./internal/center/portability ./internal/center/attachments \
      -run "LocalProfile|WitnessedRecordSubject|RecordPortabilityDeletion|RecordWatchVersionedDefaultAnchor|MinIO|AttachmentDeletionPurgesExclusiveS3BlobExactlyOnce|AttachmentUploadServiceS3DirectWorkflow|S3BlobStore|PostgresIntegrationAttachmentBlobGC|PostgresIntegrationAttachmentDeletion|PostgresIntegrationImportAttachmentGCFence|PostgresIntegrationRecordDeletionClientAndComparison|PostgresIntegrationRevisionMutationDiscovery|TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocal|^TestPostgresIntegrationSourceRetirementAuthority$|${extra_test_regex}" \
      -count=1
  else
    exec setsid env \
      --default-signal=INT \
      --default-signal=TERM \
      HOUFENG_RECORDS_KEEP_WORKSPACE=0 \
      "$root/scripts/test-record-platform-integration.sh" postgres -- \
      go test -v -p 1 -timeout=30m ./internal/center/recordbackup ./internal/center/recordrestore ./internal/center/store ./internal/center/attachments ./internal/center/store/migrate \
      -run "LocalProfile|WitnessedRecordSubject|RecordPortabilityDeletion|RecordWatchVersionedDefaultAnchor|PostgresIntegrationImportAttachmentGCFence|PostgresIntegrationRecordDeletionClientAndComparison|PostgresIntegrationRevisionMutationDiscovery|TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocal|^TestPostgresIntegrationSourceRetirementAuthority$|${extra_test_regex}" \
      -count=1
  fi
) >"$stdout_file" 2>"$stderr_file" &
records_runner_body_pid=$!
wait "$records_runner_body_pid"
command_status=$?
records_runner_body_pid=
set -e

if records_runner_finish_evidence \
  "$command_status" \
  0 \
  "$stdout_file" \
  "$stderr_file" \
  'records integration command skipped a test'
then
  final_status=0
else
  final_status=$?
fi

# Preserve the complete child evidence before cleanup for both profiles, while
# keeping stdout reserved for the machine-readable profile report.
cat "$stdout_file" >&2
cat "$stderr_file" >&2
if [ "$final_status" -ne 0 ]
then
  exit "$final_status"
fi

commit=$(git -C "$root" rev-parse HEAD)
config_material="profile=${profile};postgres=postgres:16-alpine;suites=LocalProfile,WitnessedRecordSubject,RecordPortabilityDeletion,RecordWatchVersionedDefaultAnchor,TestPostgresIntegrationImportAttachmentGCFence,TestPostgresIntegrationRecordDeletionClientAndComparison,TestPostgresIntegrationRevisionMutationDiscoveryDraftAndStaleSets,TestPostgresIntegrationRevisionMutationDiscoveryCertifiedCopies,TestPostgresIntegrationRevisionMutationDiscoveryCaptureIntentIsNotConsumed,TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocal,TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocalReferenceFences,TestPostgresIntegrationSourceRetirementAuthority"
if [ "$profile" = "s3" ]
then
  config_material="${config_material},TestPostgresMinIOIntegrationImportAttachmentGCFence"
  config_material="${config_material},MinIO,TestPostgresIntegrationAttachmentDeletionPurgesExclusiveS3BlobExactlyOnce,TestPostgresIntegrationAttachmentUploadServiceS3DirectWorkflow,TestPostgresMinIOIntegrationAttachmentDeletionPurgesExclusiveExactVersion,TestPostgresMinIOIntegrationAttachmentBlobGCExactVersionWorkflow,TestPostgresMinIOIntegrationAttachmentProcessorS3WorkspaceWorkflow,TestPostgresMinIOIntegrationAttachmentBlobPublicationCrashRestartS3,TestPostgresMinIOIntegrationAttachmentBlobGCHistoricalVersionWorkflow,TestPostgresMinIOIntegrationAttachmentDeletionPurgesHistoricalExactVersion,TestS3BlobStoreDeleteGCVersionExactVersion,TestS3BlobStoreDeleteGCVersionFailsClosed,store.PostgresIntegrationAttachmentBlobGC,store.PostgresIntegrationAttachmentDeletion;object=s3"
else
  config_material="${config_material};object=local"
fi
config_material="${config_material};required=${required_tests[*]};subtests=${required_subtests[*]};require_btrfs=${HOUFENG_RECORDS_REQUIRE_BTRFS:-0}"
config_digest=$(printf '%s' "$config_material" | sha256sum | awk '{print $1}')

python3 - "$report_file" "$profile" "$commit" "$config_digest" "$stdout_file" "${required_tests[@]}" "${required_subtests[@]}" <<'PY'
import json
import re
import sys

path, profile, commit, digest, stdout_path = sys.argv[1:6]
required = sys.argv[6:]
suites = [
    "recordbackup.local",
    "store.WitnessedRecordSubject",
    "store.RecordPortabilityDeletion",
    "store.RecordWatchVersionedDefaultAnchor",
    "store.TestPostgresIntegrationImportAttachmentGCFence",
    "store.TestPostgresIntegrationRecordDeletionClientAndComparison",
    "store.TestPostgresIntegrationRevisionMutationDiscoveryDraftAndStaleSets",
    "store.TestPostgresIntegrationRevisionMutationDiscoveryCertifiedCopies",
    "store.TestPostgresIntegrationRevisionMutationDiscoveryCaptureIntentIsNotConsumed",
    "store.TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocal",
    "store.TestPostgresIntegrationAttachmentBlobPublicationCrashRestartLocalReferenceFences",
    "store.TestPostgresIntegrationSourceRetirementAuthority",
]
if profile == "s3":
    suites.append("portability.MinIO")
    suites.append("store.TestPostgresMinIOIntegrationImportAttachmentGCFence")
    suites.extend([
        "store.TestPostgresIntegrationAttachmentDeletionPurgesExclusiveS3BlobExactlyOnce",
        "store.TestPostgresIntegrationAttachmentUploadServiceS3DirectWorkflow",
        "store.TestPostgresMinIOIntegrationAttachmentDeletionPurgesExclusiveExactVersion",
        "store.TestPostgresMinIOIntegrationAttachmentBlobGCExactVersionWorkflow",
        "store.TestPostgresMinIOIntegrationAttachmentProcessorS3WorkspaceWorkflow",
        "store.TestPostgresMinIOIntegrationAttachmentBlobPublicationCrashRestartS3",
        "store.TestPostgresMinIOIntegrationAttachmentBlobGCHistoricalVersionWorkflow",
        "store.TestPostgresMinIOIntegrationAttachmentDeletionPurgesHistoricalExactVersion",
        "attachments.TestS3BlobStoreDeleteGCVersionExactVersion",
        "attachments.TestS3BlobStoreDeleteGCVersionFailsClosed",
        "store.PostgresIntegrationAttachmentBlobGC",
        "store.PostgresIntegrationAttachmentDeletion",
    ])
with open(stdout_path, encoding="utf-8") as handle:
    evidence = handle.read()
runs = set(re.findall(r"^=== RUN\s+(\S+)\s*$", evidence, re.MULTILINE))
passes = set(re.findall(r"^\s*--- PASS: (\S+) \(", evidence, re.MULTILINE))
named_suites = [suite.rsplit(".", 1)[-1] for suite in suites if suite.rsplit(".", 1)[-1].startswith("Test")]
required = sorted(set(required + named_suites))
missing = [name for name in required if name not in runs or name not in passes]
if missing:
    raise SystemExit("required integration RUN/PASS evidence missing: " + ", ".join(missing))
suites.extend(name for name in required if name not in named_suites)
payload = {
    "format": "houfeng-record-profile-report/v1",
    "profile": profile,
    "commit": commit,
    "config_digest": digest,
    "suites": suites,
    "permanent_delete": "disabled",
    "missing": [
        "deletion.record_markdown_client",
        "deletion.record_comparison",
    ],
}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(payload, handle, separators=(",", ":"), sort_keys=False)
    handle.write("\n")
PY

cat "$report_file"
