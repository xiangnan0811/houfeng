#!/usr/bin/env bash
set -euo pipefail

if ! command -v docker >/dev/null 2>&1
then
  printf 'docker is required for the records S3 lifecycle gate\n' >&2
  exit 1
fi

root=$(cd "$(dirname "$0")/.." && pwd)
records_runner_kind=records-s3-lifecycle
source "$root/scripts/lib/records-runner-lifecycle.sh"
records_runner_require_signal_tools
lifecycle_tmp=
registered_run_ids=()
registered_tmpdirs=()

emergency_cleanup() {
  local body_status=$1
  local cleanup_failed=0
  local run_id
  local container
  local volume
  local tmpdir
  local container_output
  local volume_output
  trap - EXIT
  records_runner_arm_cleanup_signals
  set +e

  for run_id in "${registered_run_ids[@]}"
  do
    if container_output=$(docker ps -aq --filter "label=com.houfeng.records.run=$run_id")
    then
      if [ -n "$container_output" ]
      then
        while IFS= read -r container
        do
          if ! docker rm -f "$container" >/dev/null
          then
            printf 'records S3 lifecycle emergency cleanup failed: container %s\n' "$container" >&2
            cleanup_failed=1
          fi
        done <<< "$container_output"
      fi
    else
      printf 'records S3 lifecycle emergency cleanup failed: query containers for run %s\n' "$run_id" >&2
      cleanup_failed=1
    fi
  done

  for run_id in "${registered_run_ids[@]}"
  do
    if volume_output=$(docker volume ls -q --filter "label=com.houfeng.records.run=$run_id")
    then
      if [ -n "$volume_output" ]
      then
        while IFS= read -r volume
        do
          if ! docker volume rm "$volume" >/dev/null
          then
            printf 'records S3 lifecycle emergency cleanup failed: volume %s\n' "$volume" >&2
            cleanup_failed=1
          fi
        done <<< "$volume_output"
      fi
    else
      printf 'records S3 lifecycle emergency cleanup failed: query volumes for run %s\n' "$run_id" >&2
      cleanup_failed=1
    fi
  done

  for tmpdir in "${registered_tmpdirs[@]}"
  do
    if [ -n "$tmpdir" ] && ! rm -rf "$tmpdir"
    then
      printf 'records S3 lifecycle emergency cleanup failed: TMPDIR %s\n' "$tmpdir" >&2
      cleanup_failed=1
    fi
  done
  if [ -n "$lifecycle_tmp" ] && ! rm -rf "$lifecycle_tmp"
  then
    printf 'records S3 lifecycle emergency cleanup failed: workspace %s\n' "$lifecycle_tmp" >&2
    cleanup_failed=1
  fi

  if [ "$body_status" -ne 0 ]
  then
    exit "$body_status"
  fi
  if [ "$records_runner_pending_signal_status" -ne 0 ]
  then
    exit "$records_runner_pending_signal_status"
  fi
  if [ "$cleanup_failed" -ne 0 ]
  then
    exit 1
  fi
  exit 0
}

trap 'emergency_cleanup "$?"' EXIT
trap 'records_runner_signal 130' INT
trap 'records_runner_signal 143' TERM

lifecycle_tmp=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-records-s3-lifecycle.XXXXXX")
root_owned_before="$lifecycle_tmp/root-owned-before.txt"
root_owned_after="$lifecycle_tmp/root-owned-after.txt"

snapshot_root_owned_records_tmp() {
  local -a roots=()
  shopt -s nullglob
  roots=(/tmp/houfeng-records-*)
  shopt -u nullglob
  if [ "${#roots[@]}" -eq 0 ]
  then
    return 0
  fi
  find "${roots[@]}" -xdev -uid 0 -printf '%p\t%U:%G\t%s\t%T@\n' | sort
}

preflight_run_id() {
  local run_id=$1
  local containers
  local volumes
  if ! containers=$(docker ps -aq --filter "label=com.houfeng.records.run=$run_id")
  then
    printf 'records S3 lifecycle preflight failed: query containers for run %s\n' "$run_id" >&2
    return 1
  fi
  if ! volumes=$(docker volume ls -q --filter "label=com.houfeng.records.run=$run_id")
  then
    printf 'records S3 lifecycle preflight failed: query volumes for run %s\n' "$run_id" >&2
    return 1
  fi
  if [ -n "$containers" ] || [ -n "$volumes" ]
  then
    printf 'records S3 lifecycle preflight refused non-empty run label: %s\n' "$run_id" >&2
    return 1
  fi
  registered_run_ids+=("$run_id")
}

assert_no_residue() {
  local run_id=$1
  local run_tmp=$2
  local containers
  local volumes
  local -a entries=()
  local failed=0
  if ! containers=$(docker ps -aq --filter "label=com.houfeng.records.run=$run_id")
  then
    printf 'records S3 lifecycle residue check failed: query containers for run %s\n' "$run_id" >&2
    failed=1
  elif [ -n "$containers" ]
  then
    printf 'records S3 lifecycle leaked containers for run %s: %s\n' "$run_id" "$containers" >&2
    failed=1
  fi
  if ! volumes=$(docker volume ls -q --filter "label=com.houfeng.records.run=$run_id")
  then
    printf 'records S3 lifecycle residue check failed: query volumes for run %s\n' "$run_id" >&2
    failed=1
  elif [ -n "$volumes" ]
  then
    printf 'records S3 lifecycle leaked volumes for run %s: %s\n' "$run_id" "$volumes" >&2
    failed=1
  fi
  shopt -s nullglob dotglob
  entries=("$run_tmp"/*)
  shopt -u nullglob dotglob
  if [ "${#entries[@]}" -ne 0 ]
  then
    printf 'records S3 lifecycle leaked workspace entries for run %s in %s\n' "$run_id" "$run_tmp" >&2
    failed=1
  fi
  return "$failed"
}

run_profile() {
  local profile_name=$1
  local run_id=$2
  shift 2
  local run_tmp="$lifecycle_tmp/$profile_name"
  local runner_status=0
  local residue_status=0

  if ! preflight_run_id "$run_id"
  then
    return 1
  fi
  mkdir -p "$run_tmp"
  registered_tmpdirs+=("$run_tmp")

  set +e
  setsid env \
    --default-signal=INT \
    --default-signal=TERM \
    -u HOUFENG_RECORDS_KEEP_WORKSPACE \
    HOUFENG_RECORDS_RUN_ID="$run_id" \
    TMPDIR="$run_tmp" \
    "$@" &
  records_runner_body_pid=$!
  wait "$records_runner_body_pid"
  runner_status=$?
  records_runner_body_pid=
  set -e
  if assert_no_residue "$run_id" "$run_tmp"
  then
    residue_status=0
  else
    residue_status=$?
  fi
  if [ "$runner_status" -ne 0 ]
  then
    printf 'records S3 lifecycle runner failed: %s status %s\n' "$profile_name" "$runner_status" >&2
    return "$runner_status"
  fi
  if [ "$residue_status" -ne 0 ]
  then
    return "$residue_status"
  fi
  printf 'records S3 lifecycle clean: run=%s TMPDIR=%s\n' "$run_id" "$run_tmp"
}

snapshot_root_owned_records_tmp > "$root_owned_before"
base_run_id=${lifecycle_tmp##*/}
overall_status=0

if run_profile integration "${base_run_id}-integration" \
  "$root/scripts/run-records-integration.sh" --profile s3
then
  :
else
  overall_status=$?
fi

if run_profile recovery "${base_run_id}-recovery" \
  "$root/scripts/run-records-recovery.sh" --profile s3 --all
then
  :
else
  recovery_status=$?
  if [ "$overall_status" -eq 0 ]
  then
    overall_status=$recovery_status
  fi
fi

snapshot_root_owned_records_tmp > "$root_owned_after"
if cmp -s "$root_owned_before" "$root_owned_after"
then
  printf 'records S3 lifecycle root-owned /tmp snapshot unchanged\n'
else
  printf 'records S3 lifecycle changed root-owned /tmp/houfeng-records-* state\n' >&2
  if [ "$overall_status" -eq 0 ]
  then
    overall_status=1
  fi
fi

exit "$overall_status"
