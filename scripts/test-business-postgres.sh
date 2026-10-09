#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

anchors=(
  TestPostgresIntegrationAuthPasswordRotationAtomicBoundary
  TestPostgresIntegrationSettingsMutationSerializesConcurrentInitializersAndPartitions
  TestArchiveSafetyPostgresConcurrentRetryAndRollback
  TestArchiveSafetyPostgresWaitsForConcurrentHeartbeat
  TestArchiveSafetyPostgresFinalHealthRecheck
  TestArchiveSafetyPostgresRuntimeRoleEndToEnd
  TestVPSMaintenancePostgresSharedOverlapAndManualOverride
  TestVPSMaintenancePostgresArchiveNeverResumes
  TestVPSMaintenancePostgresFailureRollsBackControlsAndAudit
  TestPostgresIntegrationAttachmentDirectCompletionAtomicallyPersistsPartAndJob
  TestPostgresIntegrationAttachmentFinalIdentityCompletionAndProcessorReplay
  TestPostgresIntegrationAttachmentDeletionPreservesSharedAndPurgesExclusiveBlob
  TestPostgresIntegrationRecordPlatformServingLeaseUsesDatabaseState
  TestPostgresIntegrationRecordPlatformReservationFenceCancelsServingRenewal
  TestPostgresIntegrationRecordPlatformRenewalRejectsPreRenewExpiryTokens
  TestPostgresIntegrationIPQualitySyncMetadataRoundTrip
  TestPostgresIntegrationIPQualitySyncRollbackAndReplay
  TestPostgresIntegrationIPQualityAddressIdentity
  TestPostgresIntegrationIPQualityReadSnapshot
  TestPostgresIntegrationRuntimeStreamSessionRevocation
  TestPostgresIntegrationSubscriptionCostHF17HF18
  TestPostgresIntegrationSubscriptionCostHF19
  TestPostgresIntegrationCPUSamplingValidity
  TestPostgresIntegrationResourcePressureWindows
  TestVPSAddressValidationPostgres
  TestPostgresIntegrationSubscriptionCostBackfilledStartMonth
  TestPostgresIntegrationSubscriptionCostEstimatesPreRateMonths
  TestCreateSubscriptionIdempotentReplayAfterLostResponseKeepsOneRow
  TestCreateSubscriptionIdempotentDisplayNameIsPartOfTheRequest
)
selector="^($(IFS='|'; printf '%s' "${anchors[*]}"))$"
anchors_json=$(printf '%s\n' "${anchors[@]}" | jq -R . | jq -s .)

# Keep runner state and captured events in the checkout, never a system temp dir.
scratch_root=${TMPDIR:-"$root/tmp/agent"}
case "$scratch_root" in
  "$root"/*) ;;
  *) printf 'TMPDIR must be an absolute directory inside %s\n' "$root" >&2; exit 2 ;;
esac
umask 077
mkdir -p "$scratch_root"
scratch_root=$(realpath "$scratch_root")
case "$scratch_root" in
  "$root"/*) ;;
  *) printf 'TMPDIR resolves outside the checkout\n' >&2; exit 2 ;;
esac
workspace=$(mktemp -d "$scratch_root/business-postgres.XXXXXX")
trap 'rm -rf -- "$workspace"' EXIT
events="$workspace/events.json"

TMPDIR="$workspace" scripts/test-record-platform-integration.sh postgres -- \
  go test -json -race -count=1 ./internal/center/store -run "$selector" | tee "$events"

# A subtest pass is not its parent's pass. Every selected top-level anchor and
# the package itself must pass; any skipped/failed descendant rejects the run.
jq -se --argjson anchors "$anchors_json" '
  [.[] | select(.Package == "houfeng/internal/center/store")] as $events
  | all($anchors[];
      . as $anchor
      | any($events[]; .Test == $anchor and .Action == "run")
        and any($events[]; .Test == $anchor and .Action == "pass")
    )
    and any($events[]; (.Test // "") == "" and .Action == "pass")
    and all($events[]; .Action != "skip" and .Action != "fail")
' "$events" >/dev/null
