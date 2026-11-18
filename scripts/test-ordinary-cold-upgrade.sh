#!/usr/bin/env bash
set -euo pipefail

# Test-only nested rootful Docker host. This is not a production-host install.
# All image builds and tests below run only when this script is explicitly invoked.
if [ "$#" -eq 0 ]; then
  command -v timeout >/dev/null || { printf 'ordinary cold: timeout is required\n' >&2; exit 1; }
  exec timeout --signal=TERM --kill-after=120s 120m bash "$0" --deadline-child
fi
if [ "$#" -ne 1 ] || [ "$1" != --deadline-child ]; then
  printf 'usage: %s\n' "$0" >&2
  exit 2
fi
root=$(cd -- "$(dirname -- "$0")/.." && pwd -P)
scratch="$root/tmp/agent"
if [ ! -d "$scratch" ] || [ -L "$root/tmp" ] || [ -L "$scratch" ] || [ "$(realpath "$scratch")" != "$scratch" ]; then
  printf 'ordinary cold: expected a canonical checkout tmp/agent directory\n' >&2
  exit 1
fi
for tool in docker go jq mktemp realpath sha256sum stat sync tar; do
  command -v "$tool" >/dev/null || { printf 'ordinary cold: missing %s\n' "$tool" >&2; exit 1; }
done
if [ "$(go env GOHOSTOS)" != linux ] || [ "$(docker info --format '{{.OSType}}')" != linux ]; then
  printf 'ordinary cold: Linux Docker and Go are required\n' >&2
  exit 1
fi
security=$(docker info --format '{{json .SecurityOptions}}')
if ! jq -e 'type == "array" and all(.[]; (contains("rootless") or contains("userns")) | not)' <<<"$security" >/dev/null; then
  printf 'ordinary cold: outer Docker must be rootful and without user namespaces\n' >&2
  exit 1
fi
# Supplying known real ClamAV signature databases avoids a registry or database
# update during the fixture. They are copied into the isolated daemon host.
clamdb=${HOUFENG_ORDINARY_COLD_CLAMDB-}
if [ -z "$clamdb" ] || [ ! -d "$clamdb" ] || [ -L "$clamdb" ]; then
  printf 'ordinary cold: set HOUFENG_ORDINARY_COLD_CLAMDB to a real local signature directory\n' >&2
  exit 1
fi
for signature in main.cvd daily.cvd bytecode.cvd; do
  if [ ! -f "$clamdb/$signature" ] || [ -L "$clamdb/$signature" ]; then
    printf 'ordinary cold: missing real ClamAV signature %s\n' "$signature" >&2
    exit 1
  fi
done
for image in docker:29-dind postgres:16.12 clamav/clamav:1.4.3; do
  docker image inspect "$image" >/dev/null 2>&1 || { printf 'ordinary cold: required local image missing: %s\n' "$image" >&2; exit 1; }
done
if [[ $(docker image inspect --format '{{.Id}}' docker:29-dind) != sha256:9a5d43f7d029* ]]; then
  printf 'ordinary cold: unexpected docker:29-dind image identity\n' >&2
  exit 1
fi
umask 077
workspace=$(mktemp -d "$scratch/ordinary-cold.XXXXXXXX")
workspace_identity=$(stat -c '%d:%i:%u' "$workspace")
run=${workspace##*/}
name="houfeng-$run"
volume="$name-docker"
runner=ordinary-cold
owner=$(id -u)
old_tag="houfeng-ordinary-cold:$run-old"
new_tag="houfeng-ordinary-cold:$run-new"
old_base_tag="houfeng-ordinary-cold:$run-old-base"
new_base_tag="houfeng-ordinary-cold:$run-new-base"
released_tag="houfeng-ordinary-cold:$run-released"
container_id=
volume_created=
old_image=
new_image=
timed_out=0
persist_success_evidence() {
  local evidence_dir witness retry source
  local -a before files
  shopt -s nullglob
  before=("$workspace"/*-pre-kill.json)
  if [ "${#before[@]}" -lt 10 ]; then
    printf 'ordinary cold: missing successful pre-kill boundary witnesses\n' >&2
    return 1
  fi
  for witness in "${before[@]}"; do
    retry=${witness%-pre-kill.json}-pre-retry.json
    if [ ! -f "$witness" ] || [ -L "$witness" ] || [ ! -f "$retry" ] || [ -L "$retry" ]; then
      printf 'ordinary cold: missing paired private boundary witness\n' >&2
      return 1
    fi
  done
  evidence_dir=$(mktemp -d "$scratch/ordinary-cold-evidence.XXXXXXXX") || return 1
  files=("$workspace"/*-pre-kill.json "$workspace"/*-pre-retry.json "$workspace"/*-cli.log "$workspace"/*-timeout.json "$workspace"/test.log "$workspace"/fence-test.log "$workspace"/inner-docker-info.json "$workspace"/postgres-statements.log "$workspace"/released-image-id)
  for source in "${files[@]}"; do
    if [ ! -f "$source" ] || [ -L "$source" ] || ! cp -- "$source" "$evidence_dir/"; then
      printf 'ordinary cold: cannot preserve successful private evidence at %s\n' "$evidence_dir" >&2
      return 1
    fi
  done
  chmod -R go-rwx "$evidence_dir" || return 1
  sync -f "$evidence_dir" || return 1
  printf 'ordinary cold: private boundary evidence retained at %s\n' "$evidence_dir"
}
cleanup() {
  local status=$? inspection id found_runner found_run found_owner candidate preserve=0 stopped=0
  trap - EXIT INT TERM
  set +e
  if [ "$status" -ne 0 ] && [ "$timed_out" -ne 1 ] && [ -n "$container_id" ]; then preserve=1; fi
  if [ -n "$container_id" ]; then
    inspection=$(timeout 8s docker container inspect --format '{{.Id}}|{{index .Config.Labels "com.houfeng.records.runner"}}|{{index .Config.Labels "com.houfeng.records.run"}}|{{index .Config.Labels "com.houfeng.records.owner"}}' "$container_id" 2>/dev/null)
    IFS='|' read -r id found_runner found_run found_owner <<<"$inspection"
    if [ "$id" = "$container_id" ] && [ "$found_runner" = "$runner" ] && [ "$found_run" = "$run" ] && [ "$found_owner" = "$owner" ]; then
      if [ "$timed_out" -eq 1 ]; then
        if timeout 15s docker stop --time 5 "$container_id" >/dev/null; then stopped=1; else preserve=1; status=1; fi
      fi
      if [ "$timed_out" -eq 1 ] && [ "$stopped" -ne 1 ]; then
        printf 'ordinary cold: could not stop owned host %s; retaining it and volume %s for evidence\n' "$container_id" "$volume" >&2
      elif [ -d "$workspace" ] && [ ! -L "$workspace" ] && [ "$(realpath "$workspace")" = "$workspace" ] && [ "$(stat -c '%d:%i:%u' "$workspace")" = "$workspace_identity" ]; then
        if ! timeout 15s docker cp "$container_id:/root/fixture-evidence/." "$workspace/" >/dev/null 2>&1; then
          status=1
          preserve=1
          printf 'ordinary cold: private evidence copy failed; retaining owned host %s and volume %s\n' "$container_id" "$volume" >&2
        fi
      else
        status=1; preserve=1
        printf 'ordinary cold: evidence destination changed; retaining owned nested host\n' >&2
      fi
      if [ "$preserve" -eq 0 ]; then
        timeout 10s docker rm -f "$container_id" >/dev/null || status=1
      elif [ "$timed_out" -ne 1 ]; then
        timeout 15s docker stop --time 5 "$container_id" >/dev/null || status=1
        printf 'ordinary cold: retained owned host %s and volume %s for private failure evidence\n' "$container_id" "$volume" >&2
      fi
    else
      preserve=1; status=1
      printf 'ordinary cold: refusing to remove unverified controller container %s\n' "$container_id" >&2
    fi
  fi
  if [ -n "$volume_created" ]; then
    inspection=$(timeout 8s docker volume inspect --format '{{.Name}}|{{index .Labels "com.houfeng.records.runner"}}|{{index .Labels "com.houfeng.records.run"}}|{{index .Labels "com.houfeng.records.owner"}}' "$volume" 2>/dev/null)
    IFS='|' read -r id found_runner found_run found_owner <<<"$inspection"
    if [ "$id" = "$volume" ] && [ "$found_runner" = "$runner" ] && [ "$found_run" = "$run" ] && [ "$found_owner" = "$owner" ]; then
      if [ "$preserve" -eq 0 ]; then timeout 10s docker volume rm "$volume" >/dev/null || status=1; fi
    else
      printf 'ordinary cold: refusing to remove unverified Docker volume\n' >&2
      status=1
    fi
  fi
  for candidate in "$old_tag" "$new_tag" "$old_base_tag" "$new_base_tag" "$released_tag"; do
    inspection=$(timeout 8s docker image inspect --format '{{.Id}}|{{index .Config.Labels "com.houfeng.records.runner"}}|{{index .Config.Labels "com.houfeng.records.run"}}|{{index .Config.Labels "com.houfeng.records.owner"}}' "$candidate" 2>/dev/null)
    if [ -n "$inspection" ]; then
      IFS='|' read -r id found_runner found_run found_owner <<<"$inspection"
      if [ "$found_runner" = "$runner" ] && [ "$found_run" = "$run" ] && [ "$found_owner" = "$owner" ]; then
        timeout 10s docker image rm "$candidate" >/dev/null || status=1
      else
        printf 'ordinary cold: refusing to remove unverified image\n' >&2
        status=1
      fi
    fi
  done
  if [ -d "$workspace" ] && [ ! -L "$workspace" ] && [ "$(realpath "$workspace")" = "$workspace" ] && [ "$(stat -c '%d:%i:%u' "$workspace")" = "$workspace_identity" ]; then
    if [ "$status" -eq 0 ]; then
      if persist_success_evidence; then rm -rf -- "$workspace" || status=1; else status=1; printf 'ordinary cold: retaining full workspace after evidence-copy failure at %s\n' "$workspace" >&2; fi
    else
      chmod -R go-rwx "$workspace"
      printf 'ordinary cold: private failure evidence retained at %s\n' "$workspace" >&2
    fi
  else
    printf 'ordinary cold: refusing to remove changed workspace\n' >&2
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'timed_out=1; printf "ordinary cold: overall deadline expired\\n" >&2; exit 124' TERM
cd "$root"
printf 'RUN ordinary-cold image builds\n'
# The Go module proxy intermittently resets long HTTP/2 downloads, and Docker
# Hub intermittently times out resolving the pinned Dockerfile syntax frontend.
# go.sum, package-lock and image digests still verify every byte, so retry only
# those exact transient failures, at most three attempts per build.
build_with_proxy_retry() {
  local log=$1 attempt
  shift
  for attempt in 1 2 3; do
    if docker build "$@" >"$log.attempt$attempt" 2>&1; then
      cat "$log.attempt$attempt" >>"$log"
      return 0
    fi
    cat "$log.attempt$attempt" >>"$log"
    if ! grep -Eq 'proxy\.golang\.org.*stream error.*INTERNAL_ERROR|failed to resolve source metadata for docker\.io/docker/dockerfile:1: .*timeout awaiting response headers' "$log.attempt$attempt"; then
      return 1
    fi
    printf 'ordinary cold: transient network failure during image build (attempt %s of 3)\n' "$attempt" >&2
  done
  return 1
}
for variant in old new; do
  if [ "$variant" = old ]; then tag=$old_tag; base_tag=$old_base_tag; else tag=$new_tag; base_tag=$new_base_tag; fi
  # Pinned module/npm downloads over the default bridge repeatedly lost the
  # 287 MB pdfcpu stream; host networking matches the released-source build.
  build_with_proxy_retry "$workspace/build-$variant.log" --pull=false --network=host --label "com.houfeng.records.runner=$runner" --label "com.houfeng.records.run=$run" --label "com.houfeng.records.owner=$owner" --build-arg "VERSION=ordinary-$run-$variant" -t "$base_tag" .
  mkdir "$workspace/$variant"
  CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -buildid=ordinary-$run-$variant" -o "$workspace/$variant/processor" ./cmd/houfeng-content-processor >>"$workspace/build-$variant.log" 2>&1
  printf 'FROM %s\nCOPY --chown=10001:10001 --chmod=0755 processor /usr/local/bin/houfeng-content-processor\n' "$base_tag" >"$workspace/$variant/Dockerfile"
  docker build --pull=false --network=none --label "com.houfeng.records.runner=$runner" --label "com.houfeng.records.run=$run" --label "com.houfeng.records.owner=$owner" -t "$tag" "$workspace/$variant" >>"$workspace/build-$variant.log" 2>&1
done
released_tar="$scratch/workline00-ZUszrs4P/released-source/v1.27.5.tar"
released_image=
released_save=()
if [ -f "$released_tar" ] && [ ! -L "$released_tar" ] && [ "$(sha256sum "$released_tar" | cut -d ' ' -f 1)" = ab4ec6eb2256cb2dd893c4b29424dcf866f818e8007b29c845705eb6fc61c7a7 ]; then
  mkdir "$workspace/released"
  tar -xf "$released_tar" -C "$workspace/released"
  if build_with_proxy_retry "$workspace/build-released.log" --pull=false --network=host --label "com.houfeng.records.runner=$runner" --label "com.houfeng.records.run=$run" --label "com.houfeng.records.owner=$owner" -t "$released_tag" "$workspace/released"; then
    released_image=$(docker image inspect --format '{{.Id}}' "$released_tag")
    released_save=("$released_tag")
    printf '%s\n' "$released_image" >"$workspace/released-image-id"
    printf 'released v1.27.5 image ID %s\n' "$released_image"
  else
    printf 'FAIL ordinary-cold released v1.27.5 image build; exact output retained privately; continuing current-image cases\n' >&2
  fi
else
  printf 'FAIL ordinary-cold released source archive missing or digest mismatch; continuing current-image cases\n' >&2
fi
old_image=$(docker image inspect --format '{{.Id}}' "$old_tag")
new_image=$(docker image inspect --format '{{.Id}}' "$new_tag")
old_base_image=$(docker image inspect --format '{{.Id}}' "$old_base_tag")
new_base_image=$(docker image inspect --format '{{.Id}}' "$new_base_tag")
if [[ ! "$old_image" =~ ^sha256:[0-9a-f]{64}$ ]] || [[ ! "$new_image" =~ ^sha256:[0-9a-f]{64}$ ]] || [ "$old_image" = "$new_image" ]; then
  printf 'ordinary cold: image identities are invalid or identical\n' >&2
  exit 1
fi
CGO_ENABLED=0 GOOS=linux go test -c -o "$workspace/deploy.test" ./internal/center/deploy >"$workspace/build-test.log" 2>&1
docker save -o "$workspace/images.tar" "$old_tag" "$new_tag" "$old_base_tag" "$new_base_tag" "${released_save[@]}" postgres:16.12 clamav/clamav:1.4.3
volume_created=$(docker volume create --label "com.houfeng.records.runner=$runner" --label "com.houfeng.records.run=$run" --label "com.houfeng.records.owner=$owner" "$volume")
[ "$volume_created" = "$volume" ] || exit 1
container_id=$(docker create --name "$name" --privileged --network none --label "com.houfeng.records.runner=$runner" --label "com.houfeng.records.run=$run" --label "com.houfeng.records.owner=$owner" --mount "type=volume,source=$volume,destination=/var/lib/docker" docker:29-dind --feature=containerd-snapshotter=false)
[[ "$container_id" =~ ^[0-9a-f]{64}$ ]] || exit 1
docker start "$container_id" >/dev/null
docker exec "$container_id" mkdir -p /root/fixture-input /root/fixture-evidence /root/fixture-input/clamdb
docker cp "$workspace/images.tar" "$container_id:/root/fixture-input/images.tar"
docker cp "$workspace/deploy.test" "$container_id:/root/fixture-input/deploy.test"
# The controller re-executes itself (/proc/self/exe) as UID10002 for the trusted
# preparation stage, so its own binary must sit on a root-controlled path that
# UID10002 can traverse and execute, as /usr/local/bin does in production.
docker exec "$container_id" sh -ec 'install -d -o 0 -g 0 -m 0755 /usr/local/libexec/houfeng-fixture; install -o 0 -g 10001 -m 0550 /root/fixture-input/deploy.test /usr/local/libexec/houfeng-fixture/deploy.test; rm /root/fixture-input/deploy.test'
docker cp "$root/compose.yaml" "$container_id:/root/fixture-input/compose.yaml"
docker cp "$root/compose.proxy-network.yaml" "$container_id:/root/fixture-input/compose.proxy-network.yaml"
docker cp "$root/compose.proxy-host.yaml" "$container_id:/root/fixture-input/compose.proxy-host.yaml"
if [ -f "$workspace/released/compose.yaml" ]; then
  for recipe in compose.yaml compose.proxy-host.yaml compose.proxy-network.yaml; do
    docker cp "$workspace/released/$recipe" "$container_id:/root/fixture-input/released-$recipe"
  done
fi
for signature in main.cvd daily.cvd bytecode.cvd; do
  docker cp "$clamdb/$signature" "$container_id:/root/fixture-input/clamdb/$signature"
done
# The daemon startup is bounded and failures stay in the private evidence file.
ready=0
for _ in {1..120}; do
  if docker exec "$container_id" docker info >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
if [ "$ready" != 1 ]; then printf 'ordinary cold: nested Docker did not become ready\n' >&2; exit 1; fi
inner_driver=$(docker exec "$container_id" docker info --format '{{.Driver}}')
inner_driver_status=$(docker exec "$container_id" docker info --format '{{json .DriverStatus}}')
jq -n --arg driver "$inner_driver" --arg configured 'containerd-snapshotter=false' --argjson driver_status "$inner_driver_status" '{storage_driver:$driver,image_store_configuration:$configured,driver_status:$driver_status}' >"$workspace/inner-docker-info.json"
docker cp "$workspace/inner-docker-info.json" "$container_id:/root/fixture-evidence/inner-docker-info.json"
if jq -e 'any((.driver_status // [])[]; (.[1] | strings | startswith("io.containerd.snapshotter")))' "$workspace/inner-docker-info.json" >/dev/null; then
  printf 'ordinary cold: nested daemon reports a containerd snapshotter despite the classic-store setting\n' >&2
  exit 1
fi
docker exec "$container_id" sh -ec 'if [ -L /usr/bin/docker ]; then source=$(readlink -f /usr/bin/docker); rm /usr/bin/docker; cp "$source" /usr/bin/docker; elif [ ! -e /usr/bin/docker ]; then cp "$(command -v docker)" /usr/bin/docker; fi; chmod 0755 /usr/bin/docker; test -f /usr/bin/docker'
if ! docker exec "$container_id" /usr/bin/docker compose version --short >/dev/null 2>&1; then
  plugin=$(docker info --format '{{json .ClientInfo.Plugins}}' | jq -er '.[] | select(.Name == "compose") | .Path' | head -n 1)
  if [ -z "$plugin" ] || [ ! -f "$plugin" ]; then printf 'ordinary cold: local Compose plugin missing\n' >&2; exit 1; fi
  docker exec "$container_id" mkdir -p /usr/local/lib/docker/cli-plugins
  docker cp "$plugin" "$container_id:/usr/local/lib/docker/cli-plugins/docker-compose"
  docker exec "$container_id" chmod 0755 /usr/local/lib/docker/cli-plugins/docker-compose
fi
docker exec "$container_id" sh -ec '/usr/bin/docker compose version --short >/root/fixture-evidence/compose-version.log'
docker exec "$container_id" sh -ec 'docker load -i /root/fixture-input/images.tar >/root/fixture-evidence/load.log 2>&1; rm /root/fixture-input/images.tar'
if [ "$(docker exec "$container_id" docker image inspect --format '{{.Id}}' "$old_tag")" != "$old_image" ] || [ "$(docker exec "$container_id" docker image inspect --format '{{.Id}}' "$new_tag")" != "$new_image" ] || [ "$(docker exec "$container_id" docker image inspect --format '{{.Id}}' "$old_base_tag")" != "$old_base_image" ] || [ "$(docker exec "$container_id" docker image inspect --format '{{.Id}}' "$new_base_tag")" != "$new_base_image" ] || { [ -n "$released_image" ] && [ "$(docker exec "$container_id" docker image inspect --format '{{.Id}}' "$released_tag")" != "$released_image" ]; }; then
  printf 'ordinary cold: loaded image IDs changed\n' >&2
  exit 1
fi
jq '. + {image_store_mode:"classic",host_image_ids_match:true}' "$workspace/inner-docker-info.json" >"$workspace/inner-docker-info.next"
mv "$workspace/inner-docker-info.next" "$workspace/inner-docker-info.json"
docker cp "$workspace/inner-docker-info.json" "$container_id:/root/fixture-evidence/inner-docker-info.json"
printf 'nested Docker storage driver=%s image-store=classic host-image-IDs=matched\n' "$inner_driver"
fence_image=$(docker exec "$container_id" docker image inspect --format '{{.Id}}' postgres:16.12)
if [[ ! "$fence_image" =~ ^sha256:[0-9a-f]{64}$ ]]; then printf 'ordinary cold: nested fence image ID is invalid\n' >&2; exit 1; fi
docker exec "$container_id" sh -ec 'mkdir -p /root/fixture-tmp; chmod 0700 /root/fixture-tmp'
printf 'RUN ordinary-fence Docker network restoration\n'
docker exec -e TMPDIR=/root/fixture-tmp -e "HOUFENG_ORDINARY_FENCE_TEST_IMAGE_ID=$fence_image" "$container_id" /usr/local/libexec/houfeng-fixture/deploy.test -test.v -test.count=1 -test.timeout=10m -test.run '^TestOrdinaryFenceDockerRestoresDatabaseNetworks$' >"$workspace/fence-test.log" 2>&1 || { printf 'ordinary cold: nested fence test failed; see private evidence\n' >&2; exit 1; }
if ! grep -q '^=== RUN   TestOrdinaryFenceDockerRestoresDatabaseNetworks$' "$workspace/fence-test.log" || ! grep -q '^--- PASS: TestOrdinaryFenceDockerRestoresDatabaseNetworks ' "$workspace/fence-test.log" || grep -q '^[[:space:]]*--- SKIP:' "$workspace/fence-test.log"; then
  printf 'ordinary cold: nested fence test did not run and pass\n' >&2
  exit 1
fi
printf 'PASS ordinary-fence Docker network restoration\n'
printf 'RUN ordinary-cold e2e\n'
cold_tests=(HTTPSegment ProxyOverlays InvalidComposeFileRefused WrongC81ChecksumRefused CrashRecovery ForeignOverrideRefused ReleasedPredecessor NamespacePreflightRefusesUnboundTree NamespaceMissingAndEmptyRefused NamespaceMarkerProcessKill NamespaceLocalRowsRefused NamespaceInterruptedInitializerReplay)
docker exec -e HOUFENG_ORDINARY_COLD_E2E=1 -e "HOUFENG_ORDINARY_COLD_OLD_IMAGE=$old_image" -e "HOUFENG_ORDINARY_COLD_TARGET_IMAGE=$new_image" -e "HOUFENG_ORDINARY_COLD_OLD_BASE_IMAGE=$old_base_image" -e "HOUFENG_ORDINARY_COLD_TARGET_BASE_IMAGE=$new_base_image" -e "HOUFENG_ORDINARY_COLD_RELEASE_IMAGE=$released_image" "$container_id" /usr/local/libexec/houfeng-fixture/deploy.test -test.v -test.count=1 -test.timeout=90m -test.run "^TestOrdinaryCold($(IFS='|'; printf '%s' "${cold_tests[*]}"))\$" >"$workspace/test.log" 2>&1 || { grep -E '^[[:space:]]*(=== RUN|--- PASS:|--- FAIL:)' "$workspace/test.log" || true; printf 'ordinary cold: fixture failed; see private evidence\n' >&2; exit 1; }
missing=0
for name in "${cold_tests[@]}"; do
  grep -q "^--- PASS: TestOrdinaryCold$name " "$workspace/test.log" || missing=1
done
for case_name in override intent active fence participant staged mixed restoring ready runtime; do
  grep -q "^[[:space:]]*--- PASS: TestOrdinaryColdCrashRecovery/$case_name " "$workspace/test.log" || missing=1
done
for case_name in network host; do
  grep -q "^[[:space:]]*--- PASS: TestOrdinaryColdProxyOverlays/$case_name " "$workspace/test.log" || missing=1
done
for case_name in missing empty; do
  grep -q "^[[:space:]]*--- PASS: TestOrdinaryColdNamespaceMissingAndEmptyRefused/$case_name " "$workspace/test.log" || missing=1
done
if [ "$missing" != 0 ] || grep -q '^[[:space:]]*--- SKIP:' "$workspace/test.log"; then
  printf 'ordinary cold: selected test did not pass\n' >&2
  exit 1
fi
grep -E '^[[:space:]]*(=== RUN|--- PASS:)|image IDs old=' "$workspace/test.log"
printf 'PASS ordinary-cold e2e\n'
