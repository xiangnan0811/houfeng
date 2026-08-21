#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s [--profile local|s3]\n' "$0" >&2
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
  ""|local|s3)
    ;;
  *)
    usage
    exit 2
    ;;
esac

root=$(cd "$(dirname "$0")/.." && pwd)
workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-records-capacity.XXXXXX")

cleanup() {
  status=$?
  if [ "${HOUFENG_RECORDS_KEEP_WORKSPACE-}" != "1" ]
  then
    rm -rf "$workspace"
  fi
  exit "$status"
}
trap cleanup EXIT

stdout_file="$workspace/child-stdout.log"
stderr_file="$workspace/child-stderr.log"
unit_run='TestComparisonDetailPerformanceRecordsQuantiles|TestEvidenceCapacityPolicyEvaluationMatrix|TestEvidenceMaintenanceWorkerRunOnceIsBoundedAndPublishesAggregateMetrics|TestRestoreFailureRetryUsesFreshTargetAndBoundedWorkspace|TestProjectorTruncatedPageStopsAtTheLastRowItActuallyRead'
postgres_run='TestEvidenceComparisonCandidatePostgresQueryIsBounded|TestPostgresIntegrationEvidenceCapacityExactBoundaryAndAccounting|TestPostgresIntegrationRecordActivityPerformance|TestPostgresIntegrationVPSOverviewPerformance'

set +e
(
  set -euo pipefail
  cd "$root"
  go test \
    ./internal/center/evidence \
    ./internal/center/recordrestore \
    ./internal/center/activity \
    -run "$unit_run" \
    -count=1
  if [ -n "$profile" ]
  then
    export HOUFENG_POSTGRES_INTEGRATION=1
    export HOUFENG_ACTIVITY_PERF_SCALE=0.001
    "$root/scripts/test-record-platform-integration.sh" postgres -- \
      go test ./internal/center/store ./internal/center/http/handlers \
      -run "$postgres_run" \
      -count=1
  fi
) >"$stdout_file" 2>"$stderr_file"
command_status=$?
set -e

if grep -Fq -- '--- SKIP:' "$stdout_file" "$stderr_file"
then
  printf 'records capacity command skipped a test\n' >&2
  exit 1
fi

if [ "$command_status" -ne 0 ]
then
  cat "$stdout_file"
  cat "$stderr_file" >&2
  exit "$command_status"
fi

cat "$stdout_file"
