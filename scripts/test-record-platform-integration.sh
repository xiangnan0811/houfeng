#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s postgres -- <command> [args...]\n' "$0" >&2
}

if [ "$#" -lt 3 ] || [ "$1" != "postgres" ] || [ "$2" != "--" ]
then
  usage
  exit 2
fi
shift 2

workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-record-platform.XXXXXX")
containers=()
selected_ports=()
picked_port=

cleanup() {
  status=$?
  for container in "${containers[@]}"
  do
    docker rm -f "$container" >/dev/null 2>&1 || true
  done
  rm -rf "$workspace"
  exit "$status"
}
trap cleanup EXIT

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
    if ! ss -ltn "sport = :${candidate}" | rg -q LISTEN
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
  docker run --rm -d \
    --name "$name" \
    --network=host \
    --tmpfs /var/lib/postgresql/data:rw,noexec,nosuid,size=512m \
    -e POSTGRES_PASSWORD="$password" \
    postgres:16-alpine \
    -c port="$port" >/dev/null
  containers+=("$name")
}

wait_for_postgres() {
  name=$1
  port=$2
  for attempt in $(seq 1 30)
  do
    if docker exec "$name" pg_isready -U postgres -d postgres -p "$port" >/dev/null 2>&1
    then
      return 0
    fi
    sleep 1
  done
  docker logs "$name" >&2
  return 1
}

app_name="houfeng-rp-app-${RANDOM}${RANDOM}"
ledger_name="houfeng-rp-ledger-${RANDOM}${RANDOM}"
witness_name="houfeng-rp-witness-${RANDOM}${RANDOM}"
recovery_name="houfeng-rp-recovery-${RANDOM}${RANDOM}"
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

start_postgres "$app_name" "$app_port" "$app_password"
start_postgres "$ledger_name" "$ledger_port" "$ledger_password"
start_postgres "$witness_name" "$witness_port" "$witness_password"
start_postgres "$recovery_name" "$recovery_port" "$recovery_password"

wait_for_postgres "$app_name" "$app_port"
wait_for_postgres "$ledger_name" "$ledger_port"
wait_for_postgres "$witness_name" "$witness_port"
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

output_file="$workspace/child-output.log"
set +e
HOUFENG_POSTGRES_INTEGRATION=1 \
HOUFENG_DATABASE_URL="postgres://postgres:${app_password}@127.0.0.1:${app_port}/postgres?sslmode=disable" \
HOUFENG_DELETION_LEDGER_DATABASE_URL="postgres://postgres:${ledger_password}@127.0.0.1:${ledger_port}/postgres?sslmode=disable" \
HOUFENG_DELETION_WITNESS_DATABASE_URL="postgres://postgres:${witness_password}@127.0.0.1:${witness_port}/postgres?sslmode=disable" \
HOUFENG_RECOVERY_CONTROL_DATABASE_URL="postgres://postgres:${recovery_password}@127.0.0.1:${recovery_port}/postgres?sslmode=disable" \
"$@" 2>&1 | tee "$output_file"
command_status=${PIPESTATUS[0]}
set -e

if rg -F --quiet -- '--- SKIP:' "$output_file"
then
  printf 'record-platform integration command skipped a test\n' >&2
  exit 1
fi

exit "$command_status"
