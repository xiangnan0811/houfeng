#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s postgres -- <command> [args...]\n' "$0" >&2
  printf '       %s postgres-v2-locks -- <command> [args...]\n' "$0" >&2
  printf '       %s pg16-catalog -- <command> [args...]\n' "$0" >&2
}

if [ "$#" -lt 3 ] || [ "$2" != "--" ]
then
  usage
  exit 2
fi

mode=$1
case "$mode" in
  postgres|postgres-v2-locks)
    postgres_image=postgres:16-alpine
    ;;
  pg16-catalog)
    if [ -z "${3-}" ]
    then
      usage
      exit 2
    fi
    postgres_image=${HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE-}
    case "$postgres_image" in
      postgres:16.0|postgres:16.6|postgres:16.12)
        ;;
      *)
        usage
        exit 2
        ;;
    esac
    ;;
  *)
    usage
    exit 2
    ;;
esac
shift 2

v2_coordinate_names=(
  HOUFENG_RECORD_PLATFORM_V2_LOCKS
  HOUFENG_RECORD_PLATFORM_APP_CONTAINER_ID
  HOUFENG_RECORD_PLATFORM_APP_VOLUME
  HOUFENG_RECORD_PLATFORM_APP_SYSTEM_IDENTIFIER
)
if [ "$mode" = postgres-v2-locks ]
then
  for coordinate in "${v2_coordinate_names[@]}"
  do
    if [[ -v $coordinate ]]
    then
      printf 'postgres-v2-locks refuses prefilled %s\n' "$coordinate" >&2
      exit 2
    fi
  done
fi

script_dir=${0%/*}
if [ "$script_dir" = "$0" ]
then
  script_dir=.
fi
root=$(cd "$script_dir/.." && pwd)
records_runner_kind=record-platform
records_run_id=
workspace=
containers=()
volumes=()
source "$root/scripts/lib/records-runner-lifecycle.sh"
records_runner_install_cleanup
records_runner_validate_run_id_override
records_runner_require_signal_tools
workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-record-platform.XXXXXX")
records_runner_prepare_run_id
selected_ports=()
picked_port=

random_password() {
  od -An -N18 -tx1 /dev/urandom | tr -d ' \n'
}

pick_free_port() {
  start_port=$1
  for candidate in $(seq "$start_port" 56999)
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

start_postgres() {
  name=$1
  port=$2
  password=$3
  local -a storage_args postgres_args
  if [ "$mode" = postgres-v2-locks ] && [ "$name" = "$app_name" ]
  then
    storage_args=(-v "$app_volume:/var/lib/postgresql/data")
    postgres_args=(-c max_prepared_transactions=16)
  else
    storage_args=(--tmpfs /var/lib/postgresql/data:rw,noexec,nosuid,size=4g)
    postgres_args=()
  fi
  containers+=("$name")
  docker run -d \
    --name "$name" \
    --label "com.houfeng.records.runner=$records_runner_kind" \
    --label "com.houfeng.records.run=$records_run_id" \
    --label "com.houfeng.records.owner=$records_owner_id" \
    --network=host \
    "${storage_args[@]}" \
    -e POSTGRES_PASSWORD="$password" \
    -e POSTGRES_INITDB_ARGS="--auth-local=trust --auth-host=scram-sha-256" \
    "$postgres_image" \
    -c port="$port" -c listen_addresses=127.0.0.1 "${postgres_args[@]}" >/dev/null
}

wait_for_postgres() {
  name=$1
  port=$2
  for attempt in $(seq 1 30)
  do
    if docker exec "$name" pg_isready -h 127.0.0.1 -U postgres -d postgres -p "$port" >/dev/null 2>&1
    then
      return 0
    fi
    sleep 1
  done
  docker logs "$name" >&2
  return 1
}

app_name="houfeng-rp-app-${records_run_id}-${RANDOM}${RANDOM}"
ledger_name="houfeng-rp-ledger-${records_run_id}-${RANDOM}${RANDOM}"
witness_name="houfeng-rp-witness-${records_run_id}-${RANDOM}${RANDOM}"
recovery_name="houfeng-rp-recovery-${records_run_id}-${RANDOM}${RANDOM}"
pick_free_port 56000
app_port=$picked_port
pick_free_port "$((app_port + 1))"
ledger_port=$picked_port
pick_free_port "$((ledger_port + 1))"
witness_port=$picked_port
pick_free_port "$((witness_port + 1))"
recovery_port=$picked_port
app_password=$(random_password)
ledger_password=$(random_password)
witness_password=$(random_password)
recovery_password=$(random_password)

if [ "$mode" = postgres-v2-locks ]
then
  app_volume="houfeng-rp-v2locks-${records_run_id}-${RANDOM}${RANDOM}"
  volumes+=("$app_volume")
  docker volume create \
    --label "com.houfeng.records.runner=$records_runner_kind" \
    --label "com.houfeng.records.run=$records_run_id" \
    --label "com.houfeng.records.owner=$records_owner_id" \
    "$app_volume" >/dev/null
  records_runner_verify_volume_ownership "$app_volume"
fi

start_postgres "$app_name" "$app_port" "$app_password"
wait_for_postgres "$app_name" "$app_port"
start_postgres "$ledger_name" "$ledger_port" "$ledger_password"
wait_for_postgres "$ledger_name" "$ledger_port"
start_postgres "$witness_name" "$witness_port" "$witness_password"
wait_for_postgres "$witness_name" "$witness_port"
start_postgres "$recovery_name" "$recovery_port" "$recovery_password"
wait_for_postgres "$recovery_name" "$recovery_port"

system_identifiers=$(for pair in \
  "$app_name:$app_port" \
  "$ledger_name:$ledger_port" \
  "$witness_name:$witness_port" \
  "$recovery_name:$recovery_port"
do
  container=${pair%%:*}
  port=${pair##*:}
  docker exec "$container" psql -U postgres -d postgres -p "$port" -Atqc \
    'select system_identifier from pg_control_system()'
done)
unique_system_identifier_count=$(printf '%s\n' "$system_identifiers" | sort -u | wc -l | tr -d ' ')
if [ "$unique_system_identifier_count" != "4" ]
then
  printf 'fixture PostgreSQL system identifiers are not pairwise distinct\n' >&2
  exit 1
fi

child_coordinate_args=()
for coordinate in "${v2_coordinate_names[@]}"
do
  child_coordinate_args+=(-u "$coordinate")
done
if [ "$mode" = postgres-v2-locks ]
then
  app_container_id=$(docker container inspect --format '{{.Id}}' "$app_name")
  app_system_identifier=${system_identifiers%%$'\n'*}
  if [[ ! $app_container_id =~ ^[0-9a-f]{64}$ ]] || [[ ! $app_system_identifier =~ ^[0-9]+$ ]]
  then
    printf 'postgres-v2-locks could not identify the APP container and PostgreSQL system\n' >&2
    exit 1
  fi
  child_coordinate_args+=(
    "HOUFENG_RECORD_PLATFORM_V2_LOCKS=1"
    "HOUFENG_RECORD_PLATFORM_APP_CONTAINER_ID=$app_container_id"
    "HOUFENG_RECORD_PLATFORM_APP_VOLUME=$app_volume"
    "HOUFENG_RECORD_PLATFORM_APP_SYSTEM_IDENTIFIER=$app_system_identifier"
  )
fi

stdout_file="$workspace/child-stdout.log"
stderr_file="$workspace/child-stderr.log"
exec {stdout_tee_fd}> >(tee "$stdout_file")
stdout_tee_pid=$!
exec {stderr_tee_fd}> >(tee "$stderr_file" >&2)
stderr_tee_pid=$!
set +e
setsid env \
  --default-signal=INT \
  --default-signal=TERM \
  "${child_coordinate_args[@]}" \
  HOUFENG_POSTGRES_INTEGRATION=1 \
  HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER="$records_owner_id" \
  HOUFENG_DATABASE_URL="postgres://postgres:${app_password}@127.0.0.1:${app_port}/postgres?sslmode=disable" \
  HOUFENG_DELETION_LEDGER_DATABASE_URL="postgres://postgres:${ledger_password}@127.0.0.1:${ledger_port}/postgres?sslmode=disable" \
  HOUFENG_DELETION_WITNESS_DATABASE_URL="postgres://postgres:${witness_password}@127.0.0.1:${witness_port}/postgres?sslmode=disable" \
  HOUFENG_RECOVERY_CONTROL_DATABASE_URL="postgres://postgres:${recovery_password}@127.0.0.1:${recovery_port}/postgres?sslmode=disable" \
  "$@" \
  >&"$stdout_tee_fd" 2>&"$stderr_tee_fd" \
  {stdout_tee_fd}>&- {stderr_tee_fd}>&- &
records_runner_body_pid=$!
wait "$records_runner_body_pid"
command_status=$?
records_runner_body_pid=
exec {stdout_tee_fd}>&-
exec {stderr_tee_fd}>&-
wait "$stdout_tee_pid"
stdout_tee_status=$?
wait "$stderr_tee_pid"
stderr_tee_status=$?
set -e

evidence_sink_status=0
if [ "$stdout_tee_status" -ne 0 ]
then
  printf 'record-platform integration evidence sink failed: stdout tee status %s\n' "$stdout_tee_status" >&2
  evidence_sink_status=1
fi
if [ "$stderr_tee_status" -ne 0 ]
then
  printf 'record-platform integration evidence sink failed: stderr tee status %s\n' "$stderr_tee_status" >&2
  evidence_sink_status=1
fi

if records_runner_finish_evidence \
  "$command_status" \
  "$evidence_sink_status" \
  "$stdout_file" \
  "$stderr_file" \
  'record-platform integration command skipped a test'
then
  final_status=0
else
  final_status=$?
fi
if [ "$mode" = postgres-v2-locks ] && [ "$final_status" -eq 0 ]
then
  if ! grep -Eq '^=== RUN[[:space:]]+TestRecordDeletionV2PreparedLocks[[:space:]]*$' "$stdout_file" || \
     ! grep -Eq '^[[:space:]]*--- PASS: TestRecordDeletionV2PreparedLocks \(' "$stdout_file"
  then
    printf 'postgres-v2-locks requires RUN/PASS evidence for TestRecordDeletionV2PreparedLocks\n' >&2
    final_status=1
  fi
fi
exit "$final_status"
