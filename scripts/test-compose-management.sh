#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s --outer | --internal\n' "$0" >&2
}

if [ "$#" -ne 1 ] || { [ "$1" != --outer ] && [ "$1" != --internal ]; }; then
  usage
  exit 2
fi

script_dir=$(cd -- "$(dirname -- "$0")" && pwd -P)
root=$(cd -- "$script_dir/.." && pwd -P)
script_path="$script_dir/$(basename -- "$0")"

base_image=${HOUFENG_COMPOSE_TEST_BASE_IMAGE_ID-}
if [[ ! "$base_image" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  printf 'compose management: provide a local immutable HOUFENG_COMPOSE_TEST_BASE_IMAGE_ID\n' >&2
  exit 2
fi

if [ "$1" = --outer ]; then
  exec "$root/scripts/test-record-platform-integration.sh" postgres -- bash "$script_path" --internal
fi

# The internal entry point is only for the ownership-checked outer runner.
owner=${HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER-}
run_id=${HOUFENG_RECORDS_RUN_ID-}
if [ "${HOUFENG_POSTGRES_INTEGRATION-}" != 1 ] ||
   [[ ! "$owner" =~ ^houfeng-record-platform\.[A-Za-z0-9]+$ ]] ||
   [[ ! "$run_id" =~ ^[A-Za-z0-9_.-]{1,80}$ ]]; then
  printf 'compose management: ownership-checked postgres runner is required\n' >&2
  exit 2
fi


for command_name in docker go jq mktemp stat realpath; do
  if ! command -v "$command_name" >/dev/null; then
    printf 'compose management: required command is unavailable: %s\n' "$command_name" >&2
    exit 1
  fi
done
if [ "$(go env GOHOSTOS)" != linux ] || [ "$(docker info --format '{{.OSType}}')" != linux ]; then
  printf 'compose management: Linux host and Docker daemon are required\n' >&2
  exit 1
fi
security_options=$(docker info --format '{{json .SecurityOptions}}')
if ! jq -e 'type == "array" and all(.[]; contains("rootless") | not)' <<<"$security_options" >/dev/null; then
  printf 'compose management: a rootful Docker daemon is required\n' >&2
  exit 1
fi
host_arch=$(go env GOHOSTARCH)
base_info=$(docker image inspect --format '{{.Id}}|{{.Os}}|{{.Architecture}}' "$base_image")
if [ "$base_info" != "$base_image|linux|$host_arch" ]; then
  printf 'compose management: local base image identity, OS, or architecture does not match\n' >&2
  exit 1
fi

# Check that all four loopback endpoints are the PostgreSQL containers carrying
# this outer runner's ownership labels. Do not print URLs or credentials.
endpoint_names=(HOUFENG_DATABASE_URL HOUFENG_DELETION_LEDGER_DATABASE_URL HOUFENG_DELETION_WITNESS_DATABASE_URL HOUFENG_RECOVERY_CONTROL_DATABASE_URL)
endpoint_ports=()
for endpoint_name in "${endpoint_names[@]}"; do
  endpoint_value=${!endpoint_name-}
  if [[ ! "$endpoint_value" =~ ^postgres://postgres:[0-9a-f]{36}@127\.0\.0\.1:([0-9]{1,5})/postgres\?sslmode=disable$ ]]; then
    printf 'compose management: invalid runner PostgreSQL endpoint: %s\n' "$endpoint_name" >&2
    exit 2
  fi
  endpoint_ports+=("${BASH_REMATCH[1]}")
done
mapfile -t postgres_containers < <(docker ps -q \
  --filter 'label=com.houfeng.records.runner=record-platform' \
  --filter "label=com.houfeng.records.run=$run_id" \
  --filter "label=com.houfeng.records.owner=$owner")
if [ "${#postgres_containers[@]}" -ne 4 ]; then
  printf 'compose management: four owned running PostgreSQL containers are required\n' >&2
  exit 1
fi
for port in "${endpoint_ports[@]}"; do
  matches=0
  for postgres_container in "${postgres_containers[@]}"; do
    container_info=$(docker inspect --format '{{.HostConfig.NetworkMode}}|{{json .Args}}' "$postgres_container")
    network=${container_info%%|*}
    args=${container_info#*|}
    if [ "$network" = host ] && jq -e --arg port "port=$port" 'type == "array" and index($port) != null' <<<"$args" >/dev/null; then
      matches=$((matches + 1))
    fi
  done
  if [ "$matches" -ne 1 ]; then
    printf 'compose management: runner PostgreSQL endpoint ownership mismatch\n' >&2
    exit 1
  fi
done
if [ "$(printf '%s\n' "${endpoint_ports[@]}" | sort -u | wc -l)" -ne 4 ]; then
  printf 'compose management: runner PostgreSQL endpoints are not distinct\n' >&2
  exit 1
fi

scratch_base="$root/tmp/agent"
if [ ! -d "$scratch_base" ] || [ -L "$root/tmp" ] || [ -L "$scratch_base" ] || [ "$(realpath "$scratch_base")" != "$scratch_base" ]; then
  printf 'compose management: checkout tmp/agent directory is unavailable or unsafe\n' >&2
  exit 1
fi
umask 077
workspace=$(mktemp -d "$scratch_base/compose-management.XXXXXXXX")
workspace_identity=$(stat -c '%d:%i:%u' "$workspace")
name_suffix=${workspace##*/}
container_name="houfeng-${name_suffix}"
bootstrap_name="houfeng-${name_suffix}-bootstrap"
volume_name="houfeng-${name_suffix}-maintenance"
image_tag="houfeng-compose-management:${name_suffix}"
base_tag="houfeng-compose-management-base:${name_suffix}"
base_tag_created=
container_id=
bootstrap_id=
volume_created=
image_id=
runner_kind=compose-management
cleanup() {
  status=$?
  trap - EXIT INT TERM
  set +e
  cleanup_error=0
  for candidate in "${container_id:-$container_name}" "${bootstrap_id:-$bootstrap_name}"; do
    if inspection=$(docker container inspect --format '{{.Id}}|{{index .Config.Labels "com.houfeng.records.runner"}}|{{index .Config.Labels "com.houfeng.records.run"}}|{{index .Config.Labels "com.houfeng.records.owner"}}' "$candidate" 2>/dev/null); then
      IFS='|' read -r found_id found_runner found_run found_owner <<<"$inspection"
      if [[ "$found_id" =~ ^[0-9a-f]{64}$ ]] && [ "$found_runner" = "$runner_kind" ] && [ "$found_run" = "$run_id" ] && [ "$found_owner" = "$owner" ]; then
        docker rm -f "$found_id" >/dev/null || cleanup_error=1
      else
        printf 'compose management: refusing to remove unowned container\n' >&2
        cleanup_error=1
      fi
    elif [ "$candidate" = "$container_id" ] && [ -n "$container_id" ] || [ "$candidate" = "$bootstrap_id" ] && [ -n "$bootstrap_id" ]; then
      printf 'compose management: cannot verify created container for cleanup\n' >&2
      cleanup_error=1
    fi
  done
  if inspection=$(docker volume inspect --format '{{.Name}}|{{index .Labels "com.houfeng.records.runner"}}|{{index .Labels "com.houfeng.records.run"}}|{{index .Labels "com.houfeng.records.owner"}}' "$volume_name" 2>/dev/null); then
    IFS='|' read -r found_name found_runner found_run found_owner <<<"$inspection"
    if [ "$found_name" = "$volume_name" ] && [ "$found_runner" = "$runner_kind" ] && [ "$found_run" = "$run_id" ] && [ "$found_owner" = "$owner" ]; then
      docker volume rm "$volume_name" >/dev/null || cleanup_error=1
    else
      printf 'compose management: refusing to remove unowned volume\n' >&2
      cleanup_error=1
    fi
  elif [ -n "$volume_created" ]; then
    printf 'compose management: cannot verify created volume for cleanup\n' >&2
    cleanup_error=1
  fi
  candidate=${image_id:-$image_tag}
  if inspection=$(docker image inspect --format '{{.Id}}|{{index .Config.Labels "com.houfeng.records.runner"}}|{{index .Config.Labels "com.houfeng.records.run"}}|{{index .Config.Labels "com.houfeng.records.owner"}}' "$candidate" 2>/dev/null); then
    IFS='|' read -r found_id found_runner found_run found_owner <<<"$inspection"
    if [[ "$found_id" =~ ^sha256:[0-9a-f]{64}$ ]] && [ "$found_runner" = "$runner_kind" ] && [ "$found_run" = "$run_id" ] && [ "$found_owner" = "$owner" ]; then
      docker image rm "$found_id" >/dev/null || cleanup_error=1
    else
      printf 'compose management: refusing to remove unowned image\n' >&2
      cleanup_error=1
    fi
  elif [ -n "$image_id" ]; then
    printf 'compose management: cannot verify created image for cleanup\n' >&2
    cleanup_error=1
  fi
  if [ -n "$base_tag_created" ]; then
    if inspection=$(docker image inspect --format '{{.Id}}' "$base_tag" 2>/dev/null); then
      if [ "$inspection" = "$base_image" ]; then
        docker image rm --no-prune "$base_tag" >/dev/null || cleanup_error=1
      else
        printf 'compose management: refusing to remove changed base image tag\n' >&2
        cleanup_error=1
      fi
    else
      printf 'compose management: cannot verify base image tag for cleanup\n' >&2
      cleanup_error=1
    fi
  fi
  if [ "$status" -eq 0 ] && [ "$cleanup_error" -ne 0 ]; then
    status=1
  fi
  if [ -d "$workspace" ] && [ ! -L "$workspace" ] && [ "$(realpath "$workspace")" = "$workspace" ] && [ "$(stat -c '%d:%i:%u' "$workspace")" = "$workspace_identity" ]; then
    if [ "$status" -eq 0 ]; then
      if ! rm -rf -- "$workspace"; then
        printf 'compose management: workspace cleanup failed\n' >&2
        status=1
      fi
    else
      for evidence_name in events.jsonl test-stdout.log docker-stderr.log converter-stderr.log container-state.json container-state-stderr.log execution-status.log bootstrap.log build.log; do
        evidence_path="$workspace/$evidence_name"
        if [ -e "$evidence_path" ] || [ -L "$evidence_path" ]; then
          if [ ! -f "$evidence_path" ] || [ -L "$evidence_path" ] || ! chmod 0600 "$evidence_path"; then
            printf 'compose management: cannot secure failure evidence\n' >&2
          fi
        fi
      done
      if [ "$cleanup_error" -eq 0 ] && [ -z "$(jobs -pr)" ] && [ -d "$workspace/context" ] && [ ! -L "$workspace/context" ] && [ "$(realpath "$workspace/context")" = "$workspace/context" ]; then
        if ! rm -rf -- "$workspace/context"; then
          printf 'compose management: cannot remove compiled fixture context\n' >&2
        fi
      fi
      printf 'compose management: private failure evidence retained at %s\n' "$workspace" >&2
    fi
  else
    printf 'compose management: refusing to remove changed workspace\n' >&2
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$workspace/context/fixture-root" "$workspace/context/fixture-tmp" "$workspace/context/maintenance"
: > "$workspace/context/fixture-root/.keep"
: > "$workspace/context/fixture-tmp/.keep"
: > "$workspace/context/maintenance/.keep"
cd "$root"
printf 'compose management: compiling current static executables and test binary\n'
CGO_ENABLED=0 GOOS=linux GOARCH="$host_arch" go build -o "$workspace/context/houfeng-center" ./cmd/houfeng-center
CGO_ENABLED=0 GOOS=linux GOARCH="$host_arch" go build -o "$workspace/context/houfeng-content-processor" ./cmd/houfeng-content-processor
CGO_ENABLED=0 GOOS=linux GOARCH="$host_arch" go build -o "$workspace/context/houfeng-record-platform-admin" ./cmd/houfeng-record-platform-admin
CGO_ENABLED=0 GOOS=linux GOARCH="$host_arch" go test -c -o "$workspace/context/deploy.test" ./internal/center/deploy
if docker image inspect "$base_tag" >/dev/null 2>&1; then
  printf 'compose management: temporary base image tag already exists\n' >&2
  exit 1
fi
docker image tag "$base_image" "$base_tag"
base_tag_created=1
if [ "$(docker image inspect --format '{{.Id}}' "$base_tag")" != "$base_image" ]; then
  printf 'compose management: temporary base image tag changed before build\n' >&2
  exit 1
fi
cat > "$workspace/context/Dockerfile" <<EOF
FROM $base_tag
COPY --chown=10001:10001 --chmod=0755 houfeng-center /usr/local/bin/houfeng-center
COPY --chown=10001:10001 --chmod=0755 houfeng-content-processor /usr/local/bin/houfeng-content-processor
COPY --chown=10001:10001 --chmod=0755 houfeng-record-platform-admin /usr/local/bin/houfeng-record-platform-admin
COPY --chown=0:0 --chmod=0555 deploy.test /fixture/deploy.test
COPY fixture-root /fixture/root
COPY fixture-tmp /fixture/tmp
COPY maintenance /var/lib/houfeng-maintenance
EOF
printf 'compose management: building private fixture image\n'
docker build --quiet --pull=false --network=none --iidfile "$workspace/image-id" \
  --label "com.houfeng.records.runner=$runner_kind" \
  --label "com.houfeng.records.run=$run_id" \
  --label "com.houfeng.records.owner=$owner" \
  --tag "$image_tag" "$workspace/context" > "$workspace/build.log" 2>&1
if [ "$(docker image inspect --format '{{.Id}}' "$base_tag")" != "$base_image" ]; then
  printf 'compose management: temporary base image tag changed during build\n' >&2
  exit 1
fi
image_id=$(cat "$workspace/image-id")
if [[ ! "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  printf 'compose management: fixture image identity is invalid\n' >&2
  exit 1
fi

volume_created=$(docker volume create \
  --label "com.houfeng.records.runner=$runner_kind" \
  --label "com.houfeng.records.run=$run_id" \
  --label "com.houfeng.records.owner=$owner" "$volume_name")
volume_info=$(docker volume inspect --format '{{.Name}}|{{index .Labels "com.houfeng.records.runner"}}|{{index .Labels "com.houfeng.records.run"}}|{{index .Labels "com.houfeng.records.owner"}}' "$volume_name")
if [ "$volume_created" != "$volume_name" ] || [ "$volume_info" != "$volume_name|$runner_kind|$run_id|$owner" ]; then
  printf 'compose management: fixture volume ownership mismatch\n' >&2
  exit 1
fi
printf 'compose management: preparing the protected ordinary guard as root\n'
bootstrap_id=$(docker create --name "$bootstrap_name" \
  --label "com.houfeng.records.runner=$runner_kind" \
  --label "com.houfeng.records.run=$run_id" \
  --label "com.houfeng.records.owner=$owner" \
  --user 0:0 --cap-drop ALL --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER \
  --security-opt no-new-privileges --read-only --network none \
  --mount "type=volume,source=$volume_name,destination=/var/lib/houfeng-maintenance,volume-nocopy" \
  --entrypoint /usr/local/bin/houfeng-record-platform-admin "$image_id" deploy-prepare-storage --scope compose)
if [[ ! "$bootstrap_id" =~ ^[0-9a-f]{64}$ ]]; then
  printf 'compose management: bootstrap container identity is invalid\n' >&2
  exit 1
fi
if ! docker start -a "$bootstrap_id" > "$workspace/bootstrap.log" 2>&1; then
  printf 'compose management: root guard preparation failed\n' >&2
  exit 1
fi

export HOUFENG_BIRTH_LAUNCH_FIXTURE_ROOT=/fixture/root
export HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT=/var/lib/houfeng-maintenance/ordinary
container_id=$(docker create --name "$container_name" \
  --label "com.houfeng.records.runner=$runner_kind" \
  --label "com.houfeng.records.run=$run_id" \
  --label "com.houfeng.records.owner=$owner" \
  --user 10002:10001 --cap-drop ALL --security-opt no-new-privileges \
  --read-only --network host \
  --mount "type=volume,source=$volume_name,destination=/var/lib/houfeng-maintenance,readonly,volume-nocopy" \
  --tmpfs /fixture/root:rw,nosuid,nodev,noexec,uid=10002,gid=10001,mode=0750 \
  --tmpfs /fixture/tmp:rw,nosuid,nodev,noexec,uid=10002,gid=10001,mode=0700 \
  --env HOUFENG_POSTGRES_INTEGRATION --env HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER \
  --env HOUFENG_RECORDS_RUN_ID --env HOUFENG_DATABASE_URL \
  --env HOUFENG_DELETION_LEDGER_DATABASE_URL --env HOUFENG_DELETION_WITNESS_DATABASE_URL \
  --env HOUFENG_RECOVERY_CONTROL_DATABASE_URL --env HOUFENG_BIRTH_LAUNCH_FIXTURE_ROOT \
  --env HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT --env TMPDIR=/fixture/tmp \
  --entrypoint /fixture/deploy.test "$image_id" \
  -test.v -test.count=1 -test.timeout=45m \
  '-test.run=^(TestPostgresIntegrationComposeInitialize|TestPostgresIntegrationComposeBirthLaunchPublication|TestPostgresIntegrationComposeLocalNamespace|TestPostgresIntegrationSourceMaintenanceInterlock|TestPostgresIntegrationRecordLedgerApplicationRoleFence)$')
if [[ ! "$container_id" =~ ^[0-9a-f]{64}$ ]]; then
  printf 'compose management: fixture container identity is invalid\n' >&2
  exit 1
fi
printf 'compose management: running protected producer and database fixtures\n'
set +e
docker start -a "$container_id" > "$workspace/test-stdout.log" 2> "$workspace/docker-stderr.log"
docker_status=$?
TMPDIR="$workspace" go tool test2json -t -p houfeng/internal/center/deploy < "$workspace/test-stdout.log" > "$workspace/events.jsonl" 2> "$workspace/converter-stderr.log"
converter_status=$?
docker container inspect --format '{{json .State}}' "$container_id" > "$workspace/container-state.json" 2> "$workspace/container-state-stderr.log"
state_status=$?
set -e
printf 'docker_status=%s\nconverter_status=%s\nstate_status=%s\n' "$docker_status" "$converter_status" "$state_status" > "$workspace/execution-status.log"
if [ "$docker_status" -ne 0 ] || [ "$converter_status" -ne 0 ] || [ "$state_status" -ne 0 ]; then
  printf 'compose management: fixture execution, evidence conversion, or state inspection failed (docker=%s converter=%s state=%s)\n' "$docker_status" "$converter_status" "$state_status" >&2
  jq -r 'select(.Action == "fail" or .Action == "skip") | .Test // empty' "$workspace/events.jsonl" 2>/dev/null >&2 || true
  exit 1
fi
if ! jq -se '
  ["TestPostgresIntegrationComposeInitialize", "TestPostgresIntegrationComposeBirthLaunchPublication", "TestPostgresIntegrationComposeLocalNamespace", "TestPostgresIntegrationSourceMaintenanceInterlock", "TestPostgresIntegrationRecordLedgerApplicationRoleFence"] as $required |
  . as $events |
  (all($required[]; . as $name | any($events[]; .Test == $name and .Action == "run") and any($events[]; .Test == $name and .Action == "pass"))) and
  (all($events[]; .Action != "skip" and .Action != "fail")) and
  ([$events[] | select(.Test != null and .Action == "run") | .Test] | unique | all(.[]; . as $name | any($events[]; .Test == $name and .Action == "pass")))
' "$workspace/events.jsonl" >/dev/null; then
  printf 'compose management: missing RUN/PASS evidence or a test skipped or failed\n' >&2
  exit 1
fi
jq -r 'select(.Test != null and (.Action == "run" or .Action == "pass")) | "\(.Action) \(.Test)"' "$workspace/events.jsonl"
printf 'compose management: all selected tests and observed subtests passed\n'
