#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s --profile local|s3 --all\n' "$0" >&2
}

profile=""
run_all=0
while [ "$#" -gt 0 ]
do
  case "$1" in
    --profile)
      profile=${2-}
      shift 2
      ;;
    --all)
      run_all=1
      shift
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

if [ "$run_all" -ne 1 ]
then
  usage
  exit 2
fi

if ! command -v docker >/dev/null 2>&1
then
  printf 'docker is required for records recovery profiles\n' >&2
  exit 1
fi

root=$(cd "$(dirname "$0")/.." && pwd)
workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-records-recovery.XXXXXX")
containers=()
selected_ports=()
picked_port=

cleanup() {
  status=$?
  for container in "${containers[@]}"
  do
    docker rm -f "$container" >/dev/null 2>&1 || true
  done
  if [ "${HOUFENG_RECORDS_KEEP_WORKSPACE-}" != "1" ]
  then
    rm -rf "$workspace" || true
  fi
  exit "$status"
}
trap cleanup EXIT

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
  data="$workspace/minio"
  mkdir -p "$data"
  docker run --rm -d \
    --name "$name" \
    --network=host \
    -e MINIO_ROOT_USER="$access" \
    -e MINIO_ROOT_PASSWORD="$secret" \
    -v "$data:/data" \
    minio/minio:RELEASE.2024-12-18T13-15-44Z \
    server /data --address "127.0.0.1:${port}" >/dev/null
  containers+=("$name")
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
  minio_name="houfeng-records-recovery-minio-${RANDOM}${RANDOM}"
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
# fails the child on any `--- SKIP:`. Recovery keeps permanent delete
# disabled while markdown/comparison adapters remain missing.
export HOUFENG_POSTGRES_INTEGRATION=1
stdout_file="$workspace/child-stdout.log"
stderr_file="$workspace/child-stderr.log"
report_file="$workspace/profile-report.json"

set +e
(
  cd "$root"
  if [ "$profile" = "s3" ]
  then
    "$root/scripts/test-record-platform-integration.sh" postgres -- \
      "${minio_args[@]}" \
      go test ./internal/center/recordbackup ./internal/center/recordrestore ./internal/center/store ./internal/center/recordreadiness \
      -run 'Resurrection|PermanentDeleteDisabled|ExternalCopy|LocalProfile|WitnessedRecordSubject' \
      -count=1
  else
    "$root/scripts/test-record-platform-integration.sh" postgres -- \
      go test ./internal/center/recordbackup ./internal/center/recordrestore ./internal/center/store ./internal/center/recordreadiness \
      -run 'Resurrection|PermanentDeleteDisabled|ExternalCopy|LocalProfile|WitnessedRecordSubject' \
      -count=1
  fi
) >"$stdout_file" 2>"$stderr_file"
command_status=$?
set -e

if grep -Fq -- '--- SKIP:' "$stdout_file" "$stderr_file"
then
  printf 'records recovery command skipped a test\n' >&2
  exit 1
fi

if [ "$command_status" -ne 0 ]
then
  cat "$stdout_file"
  cat "$stderr_file" >&2
  exit "$command_status"
fi

commit=$(git -C "$root" rev-parse HEAD)
config_material="profile=${profile};postgres=postgres:16-alpine;suites=Resurrection,PermanentDeleteDisabled,ExternalCopy,LocalProfile,WitnessedRecordSubject"
if [ "$profile" = "s3" ]
then
  config_material="${config_material};object=s3"
else
  config_material="${config_material};object=local"
fi
config_digest=$(printf '%s' "$config_material" | sha256sum | awk '{print $1}')

python3 - "$report_file" "$profile" "$commit" "$config_digest" <<'PY'
import json
import sys

path, profile, commit, digest = sys.argv[1:5]
payload = {
    "format": "houfeng-record-profile-report/v1",
    "profile": profile,
    "commit": commit,
    "config_digest": digest,
    "suites": [
        "recordrestore.Resurrection",
        "recordreadiness.PermanentDeleteDisabled",
        "recordrestore.ExternalCopy",
        "recordbackup.local",
        "store.WitnessedRecordSubject",
    ],
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
