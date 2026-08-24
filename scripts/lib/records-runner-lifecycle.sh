#!/usr/bin/env bash

records_run_id=
records_owner_id=
records_runner_body_pid=
records_runner_pending_signal_status=0

records_runner_cleanup_container() {
  local container=$1
  local inspection
  local container_id
  local runner_label
  local run_label
  local owner_label

  if ! inspection=$(docker container inspect \
    --format '{{.Id}}|{{index .Config.Labels "com.houfeng.records.runner"}}|{{index .Config.Labels "com.houfeng.records.run"}}|{{index .Config.Labels "com.houfeng.records.owner"}}' \
    "$container")
  then
    printf 'records runner cleanup failed: cannot verify container ownership %s\n' "$container" >&2
    return 1
  fi
  IFS='|' read -r container_id runner_label run_label owner_label <<< "$inspection"
  if [ -z "$container_id" ]
  then
    printf 'records runner cleanup failed: invalid container identity %s\n' "$container" >&2
    return 1
  fi
  if [ "$runner_label" != "$records_runner_kind" ] || \
    [ "$run_label" != "$records_run_id" ] || \
    [ "$owner_label" != "$records_owner_id" ]
  then
    printf 'records runner cleanup failed: skipped unowned container candidate %s\n' "$container" >&2
    return 1
  fi
  if ! docker rm -f "$container_id" >/dev/null
  then
    printf 'records runner cleanup failed: container %s\n' "$container" >&2
    return 1
  fi
}

records_runner_verify_volume_ownership() {
  local volume=$1
  local inspection
  local runner_label
  local run_label
  local owner_label

  if ! inspection=$(docker volume inspect \
    --format '{{index .Labels "com.houfeng.records.runner"}}|{{index .Labels "com.houfeng.records.run"}}|{{index .Labels "com.houfeng.records.owner"}}' \
    "$volume")
  then
    printf 'records runner volume ownership verification failed: cannot inspect %s\n' "$volume" >&2
    return 1
  fi
  IFS='|' read -r runner_label run_label owner_label <<< "$inspection"
  if [ "$runner_label" != "$records_runner_kind" ] || \
    [ "$run_label" != "$records_run_id" ] || \
    [ "$owner_label" != "$records_owner_id" ]
  then
    printf 'records runner volume ownership verification failed: unowned candidate %s\n' "$volume" >&2
    return 1
  fi
}

records_runner_cleanup_volume() {
  local volume=$1
  if ! records_runner_verify_volume_ownership "$volume"
  then
    return 1
  fi
  if ! docker volume rm "$volume" >/dev/null
  then
    printf 'records runner cleanup failed: volume %s\n' "$volume" >&2
    return 1
  fi
}

records_runner_arm_cleanup_signals() {
  trap '' INT TERM
}

records_runner_signal() {
  local signal_status=$1
  local body_pid
  local body_running
  local finished_pid
  local running_pid
  local signal_name=TERM
  local signal_timeout=30
  local watchdog_pid
  records_runner_pending_signal_status=$signal_status
  trap '' INT TERM
  if [ "$signal_status" -eq 130 ]
  then
    signal_name=INT
  fi
  if [ "$records_runner_kind" = "record-platform" ]
  then
    signal_timeout=5
  elif [ "$records_runner_kind" = "records-s3-lifecycle" ]
  then
    signal_timeout=60
  fi
  if [ -n "$records_runner_body_pid" ]
  then
    set +e
    body_pid=$records_runner_body_pid
    records_runner_body_pid=
    kill -s "$signal_name" -- "-$body_pid"
    body_running=0
    for running_pid in $(jobs -pr)
    do
      if [ "$running_pid" = "$body_pid" ]
      then
        body_running=1
      fi
    done
    if [ "$body_running" -eq 0 ]
    then
      wait "$body_pid"
    else
      sleep "$signal_timeout" &
      watchdog_pid=$!
      wait -n -p finished_pid "$body_pid" "$watchdog_pid"
      if [ "$finished_pid" = "$watchdog_pid" ]
      then
        printf 'records runner signal wait timed out: pid %s\n' "$body_pid" >&2
        kill -KILL -- "-$body_pid"
        wait "$body_pid"
      else
        kill -KILL "$watchdog_pid"
        wait "$watchdog_pid"
      fi
    fi
  fi
  exit "$signal_status"
}

records_runner_cleanup() {
  local body_status=$1
  local cleanup_failed=0
  local container
  local volume
  trap - EXIT
  records_runner_arm_cleanup_signals
  set +e

  for container in "${containers[@]}"
  do
    if ! records_runner_cleanup_container "$container"
    then
      cleanup_failed=1
    fi
  done

  if [ "${HOUFENG_RECORDS_KEEP_WORKSPACE-}" = "1" ]
  then
    for volume in "${volumes[@]}"
    do
      printf 'records runner retained volume: %s\n' "$volume" >&2
    done
    if [ -n "${workspace-}" ]
    then
      printf 'records runner retained workspace: %s\n' "$workspace" >&2
    fi
  else
    for volume in "${volumes[@]}"
    do
      if ! records_runner_cleanup_volume "$volume"
      then
        cleanup_failed=1
      fi
    done
    if [ -n "${workspace-}" ] && ! rm -rf "$workspace"
    then
      printf 'records runner cleanup failed: workspace %s\n' "$workspace" >&2
      cleanup_failed=1
    fi
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

records_runner_install_cleanup() {
  trap 'records_runner_cleanup "$?"' EXIT
  trap 'records_runner_signal 130' INT
  trap 'records_runner_signal 143' TERM
}

records_runner_require_signal_tools() {
  if ! command -v setsid >/dev/null
  then
    printf 'setsid is required for records runner signal isolation\n' >&2
    return 1
  fi
  if ! command -v env >/dev/null
  then
    printf 'env is required for records runner signal isolation\n' >&2
    return 1
  fi
  if ! env --default-signal=INT --default-signal=TERM /usr/bin/true
  then
    printf 'env --default-signal support is required for records runner signal isolation\n' >&2
    return 1
  fi
}

records_runner_finish_evidence() {
  local body_status=$1
  local sink_status=$2
  local stdout_file=$3
  local stderr_file=$4
  local skip_message=$5
  local scan_status

  if [ "$sink_status" -ne 0 ]
  then
    if [ "$body_status" -ne 0 ]
    then
      return "$body_status"
    fi
    return 1
  fi

  if grep -Fq -- '--- SKIP:' "$stdout_file" "$stderr_file"
  then
    scan_status=0
  else
    scan_status=$?
  fi
  case "$scan_status" in
    0)
      printf '%s\n' "$skip_message" >&2
      if [ "$body_status" -ne 0 ]
      then
        return "$body_status"
      fi
      return 1
      ;;
    1)
      return "$body_status"
      ;;
    *)
      printf 'records runner evidence scan failed: status %s\n' "$scan_status" >&2
      if [ "$body_status" -ne 0 ]
      then
        return "$body_status"
      fi
      return 1
      ;;
  esac
}

records_runner_prepare_run_id() {
  local candidate
  records_owner_id=${workspace##*/}
  records_runner_validate_run_id_value "$records_owner_id" || return $?
  if [ "${HOUFENG_RECORDS_RUN_ID+x}" = "x" ]
  then
    candidate=$HOUFENG_RECORDS_RUN_ID
  else
    candidate=${workspace##*/}
  fi
  records_runner_validate_run_id_value "$candidate" || return $?
  records_run_id=$candidate
  export HOUFENG_RECORDS_RUN_ID=$records_run_id
}

records_runner_validate_run_id_override() {
  if [ "${HOUFENG_RECORDS_RUN_ID+x}" = "x" ]
  then
    records_runner_validate_run_id_value "$HOUFENG_RECORDS_RUN_ID"
  fi
}

records_runner_validate_run_id_value() {
  local value=$1
  if [ "${#value}" -gt 80 ] || [[ ! "$value" =~ ^[A-Za-z0-9_.-]+$ ]]
  then
    printf 'invalid HOUFENG_RECORDS_RUN_ID: expected 1-80 characters from [A-Za-z0-9_.-]\n' >&2
    return 2
  fi
}
