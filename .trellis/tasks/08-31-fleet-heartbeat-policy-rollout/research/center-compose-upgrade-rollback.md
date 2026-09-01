# Research: fleet Center v0.79.4 to fixed-patch Compose upgrade and rollback

- Query: What repository-supported sequence can upgrade the production fleet Center from v0.79.4 past the blocked v0.79.5 to the fixed v0.79.6 patch, prove the running image and migration, and roll back safely?
- Scope: internal
- Date: 2026-08-31

## Findings

### Files found

- `compose.yaml` — production eight-service Compose graph, bind-mounted durable state, one-shot initialization, runtime admission, and Center health check.
- `docs/deploy/local-and-systemd.md` — release-asset, cold-backup, restore, upgrade, health, and rollback operator contract.
- `internal/center/deploy/compose_init.go` — database/Records authority initialization sequence.
- `internal/center/store/migrate/app_acl_current_convergence.go` — current Compose migration/ACL convergence state machine.
- `cmd/houfeng-center/bootstrap.go` — legacy migration versus Records runtime-admission startup split.
- `internal/center/config/config.go` — `HOUFENG_RECORDS_ENABLED=true` selects runtime-admission mode.
- `internal/center/store/migrate/migrate.go` — legacy forward migration runner and checksum ledger.
- `db/migrations/0063_tune_heartbeat_incident_policy.sql` — v0.79.5 schema/data/index change.
- `.github/workflows/publish-images.yml` — release image labels, multi-architecture manifest, agent assets, and exact-tag deployment assets.
- `.trellis/tasks/archive/2026-08/08-31-vps-heartbeat-notification-policy/implement.md` — repository-persisted v0.79.5 release evidence.

### Current Compose contract

- `compose.yaml:1-120` makes storage/secrets/database initialization explicit one-shot gates. `houfeng-db-init` runs `houfeng-record-platform-admin deploy-init --scope compose`; Center cannot start until it exits successfully, and the Records authority cannot become healthy before it.
- All Houfeng services consume the single `HOUFENG_IMAGE` value (`compose.yaml:1-3`, `19-20`, `71-74`, `93-96`, `139-140`, `218-220`). PostgreSQL is fixed separately at `postgres:16.12` (`compose.yaml:53-69`).
- The current release Compose hard-codes `HOUFENG_RECORDS_ENABLED=true` (`compose.yaml:170-177`). That selects runtime admission rather than Center-owned forward migration (`internal/center/config/config.go:82-113`; `cmd/houfeng-center/bootstrap.go:136-149`).
- Durable local recovery state is the complete deployment directory: PostgreSQL, attachments, Records authority state, generated center configuration, staged service secrets, optional secrets, `.env`, common Compose, and both proxy overlays (`docs/deploy/local-and-systemd.md:301-338`, `371-400`). A database-only or live unordered copy is explicitly incomplete.
- Center health returns the build-time version, and the container carries OCI version/revision labels. Publication stamps `VERSION=v<release>`, `org.opencontainers.image.version`, and `org.opencontainers.image.revision` (`.github/workflows/publish-images.yml:246-259`, `301-333`). Health, image identity, and database migration are three separate evidence items.

### Critical incompatibility: the repository does not currently support this in-place Compose upgrade

v0.79.5 adds the 64th embedded migration, `0063_tune_heartbeat_incident_policy.sql`; its repository checksum is `5d30d2eab8a362f691bcfa6d802f7e7474757739260ed20aba7a4b618a011545`. The migration changes only a column default, global persisted values equal to `3`, and a partial covering index (`db/migrations/0063_tune_heartbeat_incident_policy.sql:1-13`).

However, the current production Compose initializer does not run the ordinary forward-migration path over an existing Records deployment. `ConvergeAppACLCurrent` classifies an existing ledger plus manifest tables as an `exactCandidate` (`internal/center/store/migrate/app_acl_current_convergence.go:178-200`). In that branch it compares the entire applied ledger with the current embedded set before any pending migration is applied (`app_acl_current_convergence.go:204-245`). A migration-count mismatch returns `ErrDevelopmentDatabaseRebuildRequired`; the exact diagnostic is built at `app_acl_current_convergence.go:349-369`. Only the fresh branch calls `applyPending` (`app_acl_current_convergence.go:272-320`).

Therefore a normal v0.79.4 current-Compose database whose exact manifest ends at `0062_create_vps_create_idempotency.sql` will be rejected by v0.79.5 db-init because the new build requires `0063`. The documented statement that db-init applies a supported forward transition (`docs/deploy/local-and-systemd.md:402-425`) is not implemented for an existing exact-current manifest in this release. Do not run `docker compose pull/up` on production until one of these is supplied and rehearsed:

1. a repository/release-supported successor-manifest convergence path that applies `0063`, updates ACL evidence, and publishes the next manifest revision; or
2. an explicit, reviewed one-off migration/manifest bridge with exact preconditions and rollback evidence.

Do not bypass db-init, run ad hoc SQL, delete manifest rows, switch Records off, or start Center manually. Those actions fall outside the repository contract and can split schema, ACL, and Records authority identity.

### Release identity available in repository evidence

- The archived delivery record says release `v0.79.5` points to source `e427f41b`, with release-main and publish-images successful. It records the multi-architecture image index `sha256:a3c75cab7538d6b601a48d7d6a26db1ea1c4658ba14edc528663cff6c9e8ab6e`, arm64 manifest `sha256:24b081a5af62c204474f0608ceb27a7fde59bd480a31cc88c2553ad8b166b911`, and amd64 manifest `sha256:937f720cd3a12e26e5f2465981973c5bc30bbc8eb8e8cd2959b804980bf68d99` (`.trellis/tasks/archive/2026-08/08-31-vps-heartbeat-notification-policy/implement.md:129-135`).
- Publication validates one exact release tag across both proxy modes and uploads byte-identical Compose assets (`.github/workflows/publish-images.yml:357-480`). This proves the public artifacts, not the live host.

### Safe read-only preflight commands on the Center host

Run only from the owner-confirmed absolute deployment directory. These commands avoid printing `.env` or secret values:

```bash
set -euo pipefail
DEPLOY_DIR=/absolute/owner-confirmed/fleet-deployment
PUBLIC_BASE_URL=https://fleet.yading.de
DOCKER_HOST=unix:///run/docker.sock
DOCKER_CONFIG=/root/houfeng-rollout/docker-empty-config
test -d "$DEPLOY_DIR" && test -f "$DEPLOY_DIR/compose.yaml" && test -f "$DEPLOY_DIR/.env"
cd -- "$DEPLOY_DIR"
docker_clean() {
  timeout --signal=TERM --kill-after=1s 10s env -i \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    HOME=/root USER=root LOGNAME=root DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
    docker "$@"
}
compose_clean() {
  timeout --signal=TERM --kill-after=1s 10s env -i \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
    DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
    docker compose --env-file "$DEPLOY_DIR/.env" \
    -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" "$@"
}
compose_clean config >/dev/null
compose_clean config --services
compose_clean config --images
compose_clean ps -a
center_id="$(compose_clean ps -q houfeng)"
test -n "$center_id"
docker_clean inspect --format '{{.Config.Image}} {{.Image}} {{index .Config.Labels "org.opencontainers.image.version"}} {{index .Config.Labels "org.opencontainers.image.revision"}}' "$center_id"
center_image_id="$(docker_clean inspect --format '{{.Image}}' "$center_id")"
docker_clean image inspect "$center_image_id" --format '{{json .RepoDigests}}'
curl --connect-timeout 2 --max-time 5 -fsS "$PUBLIC_BASE_URL/api/healthz"
compose_clean exec -T db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc \
  "select count(*)::text || ' ' || max(name) from public.schema_migrations"
compose_clean exec -T db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc \
  "select manifest_revision::text from public.app_acl_manifest_head"
```

Stop if the actual directory, `COMPOSE_FILE`, image, bind mounts, database name, ledger location, migration tail, or authority topology differs. The live inventory supplied to this research says the target Center is v0.79.4 at `fleet.yading.de`; this researcher did not access any host.

### Cold recovery point before any approved bridge or upgrade

The repository's safest supported rollback anchor is a cold copy. Use owner-approved absolute paths, ensure the backup parent is outside the deployment directory, and keep the copy private:

The executable command sheet must be generated with the revalidated image ID and UTC backup name frozen as literals, then shellchecked, independently reviewed, and failure-injected before production use. It is never invoked directly by shebang: a SHA-frozen root wrapper opens and locks the canonical file descriptor, then enters through `env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root USER=root LOGNAME=root HOUFENG_ROLLOUT_LOCK_FD=<inherited-fd> /bin/bash --noprofile --norc <canonical-script>`, before any rollout write. The child mechanically checks/acquires that inherited descriptor, rejects concurrent package/updater/rollout jobs, and the wrapper holds the descriptor through child exit, execution-receipt publication and cleanup. Cold restore and cutover use the same lock identity. The following is a deliberately non-executable template; it rejects the image placeholder and demonstrates the required error semantics:

```bash
#!/bin/bash
set -Eeuo pipefail
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
unset BASH_ENV ENV CDPATH
umask 077

DEPLOY_PARENT=/root/data/docker_data
DEPLOY_NAME=houfeng
DEPLOY_DIR=/root/data/docker_data/houfeng
EXPECTED_COMPOSE_PROJECT=houfeng
EXPECTED_HOSTNAME=REPLACE_WITH_REVALIDATED_REMOTE_HOSTNAME
EXPECTED_MACHINE_ID_SHA256=REPLACE_WITH_REVALIDATED_MACHINE_ID_SHA256
EXPECTED_ARCH=x86_64
HOST_LOCK_PATH=/run/lock/houfeng-rollout.lock
EXPECTED_HOST_LOCK_IDENTITY=REPLACE_WITH_LOCK_PATH_DEVICE_INODE_OWNER_MODE
: "${HOUFENG_ROLLOUT_LOCK_FD:?wrapper must pass the held rollout lock fd}"
BACKUP_PARENT=/root/data/houfeng-backups
BACKUP_NAME=fleet-pre-fixed-patch-YYYYMMDDTHHMMSSZ
BACKUP_DIR="$BACKUP_PARENT/$BACKUP_NAME"
OLD_HOUFENG_IMAGE_ID=REPLACE_WITH_REVALIDATED_V0794_SHA256_IMAGE_ID
OLD_POSTGRES_IMAGE_ID=REPLACE_WITH_REVALIDATED_POSTGRES_SHA256_IMAGE_ID
OLD_CLAMAV_IMAGE_ID=REPLACE_WITH_REVALIDATED_CLAMAV_SHA256_IMAGE_ID
OLD_POSTGRES_IMAGE_REF=postgres:16.12
OLD_CLAMAV_IMAGE_REF=clamav/clamav:1.4.3
FIXED_IMAGE_ID=REPLACE_WITH_REVALIDATED_V0796_SHA256_IMAGE_ID
FIXED_REVISION=REPLACE_WITH_REVALIDATED_V0796_SOURCE_SHA
OLD_HOUFENG_AMD64_MANIFEST_DIGEST=REPLACE_WITH_REVALIDATED_V0794_AMD64_MANIFEST_DIGEST
FIXED_HOUFENG_AMD64_MANIFEST_DIGEST=REPLACE_WITH_REVALIDATED_V0796_AMD64_MANIFEST_DIGEST
OLD_REVISION=1481a558b136c2e6e00e59d523fe281acd655ae8
PUBLIC_BASE_URL=https://fleet.yading.de
AGENT1_MONITORING_INSTANCE_ID=REPLACE_WITH_REVALIDATED_NETCUP_STABLE_ID
AGENT2_MONITORING_INSTANCE_ID=REPLACE_WITH_REVALIDATED_INFORMATEN_STABLE_ID
PRIVATE_INVARIANT_VERIFIER=/root/houfeng-rollout/REPLACE.center-old-private-verifier
PRIVATE_INVARIANT_VERIFIER_SHA256=REPLACE_WITH_PRIVATE_VERIFIER_SHA256
PRIVATE_MOUNT_CLOSURE_VERIFIER=/root/houfeng-rollout/REPLACE.center-mount-closure-verifier
PRIVATE_MOUNT_CLOSURE_VERIFIER_SHA256=REPLACE_WITH_MOUNT_CLOSURE_VERIFIER_SHA256
PRIVATE_IMAGE_PROVENANCE_RECEIPT=/root/houfeng-rollout/REPLACE.center-image-provenance-receipt
PRIVATE_IMAGE_PROVENANCE_RECEIPT_SHA256=REPLACE_WITH_IMAGE_PROVENANCE_RECEIPT_SHA256
SAFE_PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
DOCKER_HOST=unix:///run/docker.sock
DOCKER_CONFIG=/root/houfeng-rollout/docker-empty-config
EXPECTED_DOCKER_DAEMON_ID=REPLACE_WITH_REVALIDATED_LOCAL_DOCKER_DAEMON_ID
EXPECTED_DOCKER_SOCKET_METADATA=REPLACE_WITH_REVALIDATED_SOCKET_OWNER_GROUP_MODE
export DOCKER_HOST DOCKER_CONFIG
RECOVERY_ARMED=0
OLD_STACK_RUNTIME_HEALTH_PROVEN=0
ROOT_BASHPID=$BASHPID
CENTER_TOTAL_DEADLINE_SECONDS=0
CENTER_VALIDATION_DEADLINE_SECONDS=0

monotonic_seconds() {
  local uptime_seconds
  local ignored
  IFS=' ' read -r uptime_seconds ignored </proc/uptime || return 1
  uptime_seconds="${uptime_seconds%%.*}"
  [[ "$uptime_seconds" =~ ^[0-9]+$ ]] || return 1
  printf '%s\n' "$uptime_seconds"
}

expect_command_output() {
  local expected="$1"
  local actual
  shift
  actual="$("$@")" || return "$?"
  test "$actual" = "$expected"
}

bounded_sha256_file() {
  timeout --signal=KILL 3s sha256sum "$1" | awk '{print $1}'
}

deadline_has_time() {
  local now
  now="$(monotonic_seconds)" || return 1
  (( now < CENTER_VALIDATION_DEADLINE_SECONDS ))
}

compose_clean() {
  env -i PATH="$SAFE_PATH" HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
    DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
    docker compose --env-file "$DEPLOY_DIR/.env" \
    -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" "$@"
}

compose_old() {
  env -i PATH="$SAFE_PATH" HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
    DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
    HOUFENG_IMAGE="$OLD_HOUFENG_IMAGE_ID" docker compose --env-file "$DEPLOY_DIR/.env" \
    -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" "$@"
}

verify_saved_image_archive() {
  OLD_HOUFENG_IMAGE_ID="$OLD_HOUFENG_IMAGE_ID" \
  OLD_POSTGRES_IMAGE_ID="$OLD_POSTGRES_IMAGE_ID" \
  OLD_CLAMAV_IMAGE_ID="$OLD_CLAMAV_IMAGE_ID" \
  OLD_POSTGRES_IMAGE_REF="$OLD_POSTGRES_IMAGE_REF" \
  OLD_CLAMAV_IMAGE_REF="$OLD_CLAMAV_IMAGE_REF" \
  IMAGE_ARCHIVE="$BACKUP_DIR/stack-v0.79.4-images.tar" python3 - <<'PY'
import hashlib
import json
import os
import tarfile

expected_ids = {
    os.environ["OLD_HOUFENG_IMAGE_ID"].removeprefix("sha256:"),
    os.environ["OLD_POSTGRES_IMAGE_ID"].removeprefix("sha256:"),
    os.environ["OLD_CLAMAV_IMAGE_ID"].removeprefix("sha256:"),
}
required_refs = {
    os.environ["OLD_POSTGRES_IMAGE_REF"]: os.environ["OLD_POSTGRES_IMAGE_ID"].removeprefix("sha256:"),
    os.environ["OLD_CLAMAV_IMAGE_REF"]: os.environ["OLD_CLAMAV_IMAGE_ID"].removeprefix("sha256:"),
}
with tarfile.open(os.environ["IMAGE_ARCHIVE"], "r") as archive:
    manifest_member = archive.extractfile("manifest.json")
    if manifest_member is None:
        raise SystemExit(66)
    manifest = json.load(manifest_member)
    actual_ids = set()
    ref_to_id = {}
    for entry in manifest:
        config_name = entry.get("Config", "")
        if not config_name.endswith(".json"):
            raise SystemExit(66)
        config_id = config_name[:-5]
        config_member = archive.extractfile(config_name)
        if config_member is None or hashlib.sha256(config_member.read()).hexdigest() != config_id:
            raise SystemExit(66)
        actual_ids.add(config_id)
        for ref in entry.get("RepoTags") or []:
            ref_to_id[ref] = config_id
if actual_ids != expected_ids:
    raise SystemExit(66)
if any(ref_to_id.get(ref) != image_id for ref, image_id in required_refs.items()):
    raise SystemExit(66)
PY
}

run_with_remaining_timeout() {
  local requested_seconds="$1"
  local now
  local remaining_seconds
  shift
  now="$(monotonic_seconds)" || return 124
  remaining_seconds=$(( CENTER_VALIDATION_DEADLINE_SECONDS - now - 1 ))
  (( remaining_seconds > 0 )) || return 124
  if (( requested_seconds > remaining_seconds )); then requested_seconds="$remaining_seconds"; fi
  timeout --signal=TERM --kill-after=1s "${requested_seconds}s" "$@"
}

compose_with_remaining_timeout() {
  local requested_seconds="$1"
  shift
  run_with_remaining_timeout "$requested_seconds" env -i \
    PATH="$SAFE_PATH" HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
    DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
    docker compose --env-file "$DEPLOY_DIR/.env" \
    -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" "$@"
}

cleanup_cold_marker_bounded() {
  local now
  local remaining_seconds
  if test -e "$BACKUP_DIR/COLD_BACKUP_COMPLETE" || test -L "$BACKUP_DIR/COLD_BACKUP_COMPLETE"; then
    now="$(monotonic_seconds)" || return 124
    remaining_seconds=$(( CENTER_TOTAL_DEADLINE_SECONDS - now ))
    (( remaining_seconds > 0 )) || return 124
    if (( remaining_seconds > 1 )); then remaining_seconds=1; fi
    timeout --signal=KILL "${remaining_seconds}s" unlink -- "$BACKUP_DIR/COLD_BACKUP_COMPLETE" || return $?
  fi
  now="$(monotonic_seconds)" || return 124
  remaining_seconds=$(( CENTER_TOTAL_DEADLINE_SECONDS - now ))
  (( remaining_seconds > 0 )) || return 124
  if (( remaining_seconds > 1 )); then remaining_seconds=1; fi
  timeout --signal=KILL "${remaining_seconds}s" sync -f "$BACKUP_DIR"
}

old_stack_runtime_is_still_healthy_bounded() {
  local now
  local remaining_seconds
  now="$(monotonic_seconds)" || return 1
  remaining_seconds=$(( CENTER_TOTAL_DEADLINE_SECONDS - now - 22 ))
  (( remaining_seconds > 0 )) || return 1
  if (( remaining_seconds > 6 )); then remaining_seconds=6; fi
  timeout --signal=TERM --kill-after=1s "${remaining_seconds}s" \
    "$PRIVATE_INVARIANT_VERIFIER" --runtime-health-only "$DEPLOY_DIR" \
    "$EXPECTED_COMPOSE_PROJECT" "$OLD_HOUFENG_IMAGE_ID" "$OLD_POSTGRES_IMAGE_ID" \
    "$OLD_CLAMAV_IMAGE_ID" "$AGENT1_MONITORING_INSTANCE_ID" "$AGENT2_MONITORING_INSTANCE_ID"
}

recover_exact_old_stack() {
  trap '' INT TERM
  trap - ERR
  local original_rc="$1"
  local old_images_ready=0
  local recovery_rc=0
  local marker_cleanup_rc=0
  local now=0
  local remaining_seconds=0
  if (( BASHPID != ROOT_BASHPID )); then return "$original_rc"; fi
  if (( RECOVERY_ARMED == 1 )); then
    set +e
    if (( OLD_STACK_RUNTIME_HEALTH_PROVEN == 1 )) && \
      ! old_stack_runtime_is_still_healthy_bounded; then
      OLD_STACK_RUNTIME_HEALTH_PROVEN=0
    fi
    if (( OLD_STACK_RUNTIME_HEALTH_PROVEN == 0 )); then
      cd -- "$DEPLOY_DIR"
      recovery_rc=$?
      if (( recovery_rc == 0 )); then
      if now="$(monotonic_seconds)"; then
        remaining_seconds=$(( CENTER_TOTAL_DEADLINE_SECONDS - now - 10 ))
      else
        remaining_seconds=0
      fi
      if (( remaining_seconds > 3 )); then remaining_seconds=3; fi
      if (( remaining_seconds > 0 )) && timeout --signal=KILL "${remaining_seconds}s" /bin/bash --noprofile --norc -c '
        set -euo pipefail
        actual_postgres_id="$(docker image inspect --format "{{.Id}}" "$2")"
        actual_clamav_id="$(docker image inspect --format "{{.Id}}" "$4")"
        docker image inspect "$1" >/dev/null
        test "$actual_postgres_id" = "$3"
        test "$actual_clamav_id" = "$5"
      ' _ "$OLD_HOUFENG_IMAGE_ID" "$OLD_POSTGRES_IMAGE_REF" "$OLD_POSTGRES_IMAGE_ID" \
        "$OLD_CLAMAV_IMAGE_REF" "$OLD_CLAMAV_IMAGE_ID"; then
          old_images_ready=1
      fi
      if (( old_images_ready == 0 )); then
        if now="$(monotonic_seconds)"; then
          remaining_seconds=$(( CENTER_TOTAL_DEADLINE_SECONDS - now - 10 ))
        else
          remaining_seconds=0
        fi
        if (( remaining_seconds > 0 )); then
          timeout --signal=TERM --kill-after=2s "${remaining_seconds}s" \
            docker load --input "$BACKUP_DIR/stack-v0.79.4-images.tar" >/dev/null
          recovery_rc=$?
          if (( recovery_rc == 0 )); then
            if now="$(monotonic_seconds)"; then
              remaining_seconds=$(( CENTER_TOTAL_DEADLINE_SECONDS - now - 7 ))
            else
              remaining_seconds=0
            fi
            if (( remaining_seconds > 3 )); then remaining_seconds=3; fi
            if (( remaining_seconds <= 0 )) || ! timeout --signal=KILL "${remaining_seconds}s" /bin/bash --noprofile --norc -c '
              set -euo pipefail
              actual_postgres_id="$(docker image inspect --format "{{.Id}}" "$2")"
              actual_clamav_id="$(docker image inspect --format "{{.Id}}" "$4")"
              docker image inspect "$1" >/dev/null
              test "$actual_postgres_id" = "$3"
              test "$actual_clamav_id" = "$5"
            ' _ "$OLD_HOUFENG_IMAGE_ID" "$OLD_POSTGRES_IMAGE_REF" "$OLD_POSTGRES_IMAGE_ID" \
              "$OLD_CLAMAV_IMAGE_REF" "$OLD_CLAMAV_IMAGE_ID"; then
              recovery_rc=125
            fi
          fi
        else
          recovery_rc=124
        fi
      fi
      fi
      if (( recovery_rc == 0 )); then
      if now="$(monotonic_seconds)"; then
        remaining_seconds=$(( CENTER_TOTAL_DEADLINE_SECONDS - now - 5 ))
      else
        remaining_seconds=0
      fi
      if (( remaining_seconds > 0 )); then
        timeout --signal=TERM --kill-after=2s "${remaining_seconds}s" env -i \
          PATH="$SAFE_PATH" HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
          DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
          HOUFENG_IMAGE="$OLD_HOUFENG_IMAGE_ID" docker compose --env-file "$DEPLOY_DIR/.env" \
          -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" \
          up --pull never -d --wait --wait-timeout "$remaining_seconds"
        recovery_rc=$?
      else
        recovery_rc=124
      fi
      fi
    fi
    cleanup_cold_marker_bounded || marker_cleanup_rc=$?
    if (( recovery_rc != 0 )); then
      printf 'old stack recovery could not be proven; preserve evidence and isolate the service/host for investigation\n' >&2
    fi
    if (( marker_cleanup_rc != 0 )); then
      printf 'cold marker cleanup/directory sync could not be proven; promotion is forbidden and host evidence must be isolated\n' >&2
    fi
  fi
  exit "$original_rc"
}
trap 'recover_exact_old_stack "$?"' ERR
trap 'recover_exact_old_stack 130' INT
trap 'recover_exact_old_stack 143' TERM

(( EUID == 0 ))
test "$DEPLOY_DIR" = /root/data/docker_data/houfeng
test "$BACKUP_PARENT" != "$DEPLOY_DIR"
for image_id in "$OLD_HOUFENG_IMAGE_ID" "$OLD_POSTGRES_IMAGE_ID" "$OLD_CLAMAV_IMAGE_ID" "$FIXED_IMAGE_ID"; do
  [[ "$image_id" =~ ^sha256:[0-9a-f]{64}$ ]] || exit 64
done
case "$EXPECTED_HOSTNAME:$EXPECTED_MACHINE_ID_SHA256:$EXPECTED_HOST_LOCK_IDENTITY:$FIXED_REVISION:$OLD_HOUFENG_AMD64_MANIFEST_DIGEST:$FIXED_HOUFENG_AMD64_MANIFEST_DIGEST:$EXPECTED_DOCKER_DAEMON_ID:$EXPECTED_DOCKER_SOCKET_METADATA" in *REPLACE*) exit 64 ;; esac
case "$AGENT1_MONITORING_INSTANCE_ID:$AGENT2_MONITORING_INSTANCE_ID:$PRIVATE_INVARIANT_VERIFIER:$PRIVATE_INVARIANT_VERIFIER_SHA256:$PRIVATE_MOUNT_CLOSURE_VERIFIER:$PRIVATE_MOUNT_CLOSURE_VERIFIER_SHA256:$PRIVATE_IMAGE_PROVENANCE_RECEIPT:$PRIVATE_IMAGE_PROVENANCE_RECEIPT_SHA256" in *REPLACE*) exit 64 ;; esac
[[ "$EXPECTED_MACHINE_ID_SHA256" =~ ^[0-9a-f]{64}$ ]] || exit 64
[[ "$FIXED_REVISION" =~ ^[0-9a-f]{40}$ ]] || exit 64
[[ "$PRIVATE_INVARIANT_VERIFIER_SHA256" =~ ^[0-9a-f]{64}$ ]] || exit 64
[[ "$PRIVATE_MOUNT_CLOSURE_VERIFIER_SHA256" =~ ^[0-9a-f]{64}$ ]] || exit 64
[[ "$PRIVATE_IMAGE_PROVENANCE_RECEIPT_SHA256" =~ ^[0-9a-f]{64}$ ]] || exit 64
[[ "$OLD_HOUFENG_AMD64_MANIFEST_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || exit 64
[[ "$FIXED_HOUFENG_AMD64_MANIFEST_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]] || exit 64
expect_command_output "$EXPECTED_HOSTNAME" hostname -s
expect_command_output "$EXPECTED_MACHINE_ID_SHA256" bounded_sha256_file /etc/machine-id
expect_command_output "$EXPECTED_ARCH" uname -m
test "$EXPECTED_ARCH" = x86_64
command -v flock >/dev/null
[[ "$HOUFENG_ROLLOUT_LOCK_FD" =~ ^[0-9]+$ ]]
test -e "/proc/$BASHPID/fd/$HOUFENG_ROLLOUT_LOCK_FD"
expect_command_output "$HOST_LOCK_PATH" readlink -f -- "/proc/$BASHPID/fd/$HOUFENG_ROLLOUT_LOCK_FD"
test -f "$HOST_LOCK_PATH"
test ! -L "$HOST_LOCK_PATH"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$HOST_LOCK_PATH"
host_lock_identity="$(stat -c '%n|%d|%i|%U:%G|%a' "$HOST_LOCK_PATH")"
test "$host_lock_identity" = "$EXPECTED_HOST_LOCK_IDENTITY"
flock -n "$HOUFENG_ROLLOUT_LOCK_FD"
test -S /run/docker.sock
test ! -L /run/docker.sock
expect_command_output "$EXPECTED_DOCKER_SOCKET_METADATA" stat -c '%U:%G %a' /run/docker.sock
test -d "$DOCKER_CONFIG"
test ! -L "$DOCKER_CONFIG"
expect_command_output "$DOCKER_CONFIG" readlink -f -- "$DOCKER_CONFIG"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$DOCKER_CONFIG"
docker_config_entry="$(find "$DOCKER_CONFIG" -mindepth 1 -maxdepth 1 -print -quit)" || exit 65
test -z "$docker_config_entry"
expect_command_output "$EXPECTED_DOCKER_DAEMON_ID" timeout --signal=KILL 3s docker info --format '{{.ID}}'
expect_command_output linux/x86_64 timeout --signal=KILL 3s docker info --format '{{.OSType}}/{{.Architecture}}'
test "$AGENT1_MONITORING_INSTANCE_ID" != "$AGENT2_MONITORING_INSTANCE_ID"
case "$BACKUP_NAME" in fleet-pre-fixed-patch-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z) ;; *) exit 64 ;; esac
test ! -L "$DEPLOY_DIR"
test -d "$DEPLOY_DIR/data/postgres"
test -d "$DEPLOY_DIR/data/attachments"
test -f "$DEPLOY_DIR/.env"
test -f "$DEPLOY_DIR/compose.yaml"
test -f "$DEPLOY_DIR/compose.proxy-host.yaml"
test -f "$DEPLOY_DIR/compose.proxy-network.yaml"
test ! -L "$DEPLOY_DIR/.env"
test ! -L "$DEPLOY_DIR/compose.yaml"
test ! -L "$DEPLOY_DIR/compose.proxy-host.yaml"
test ! -L "$DEPLOY_DIR/compose.proxy-network.yaml"
test ! -L "$PRIVATE_INVARIANT_VERIFIER"
test -f "$PRIVATE_INVARIANT_VERIFIER"
test -x "$PRIVATE_INVARIANT_VERIFIER"
expect_command_output "$PRIVATE_INVARIANT_VERIFIER" readlink -f -- "$PRIVATE_INVARIANT_VERIFIER"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$PRIVATE_INVARIANT_VERIFIER"
expect_command_output "$PRIVATE_INVARIANT_VERIFIER_SHA256" bounded_sha256_file "$PRIVATE_INVARIANT_VERIFIER"
test ! -L "$PRIVATE_MOUNT_CLOSURE_VERIFIER"
test -f "$PRIVATE_MOUNT_CLOSURE_VERIFIER"
test -x "$PRIVATE_MOUNT_CLOSURE_VERIFIER"
expect_command_output "$PRIVATE_MOUNT_CLOSURE_VERIFIER" readlink -f -- "$PRIVATE_MOUNT_CLOSURE_VERIFIER"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$PRIVATE_MOUNT_CLOSURE_VERIFIER"
expect_command_output "$PRIVATE_MOUNT_CLOSURE_VERIFIER_SHA256" bounded_sha256_file "$PRIVATE_MOUNT_CLOSURE_VERIFIER"
test ! -L "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
test -f "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
expect_command_output "$PRIVATE_IMAGE_PROVENANCE_RECEIPT" readlink -f -- "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
expect_command_output "$PRIVATE_IMAGE_PROVENANCE_RECEIPT_SHA256" bounded_sha256_file "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
test ! -e "$BACKUP_DIR"
test ! -L /root/data
expect_command_output /root/data readlink -f -- /root/data
if test -e "$BACKUP_PARENT" || test -L "$BACKUP_PARENT"; then
  test -d "$BACKUP_PARENT"
  test ! -L "$BACKUP_PARENT"
  expect_command_output /root/data/houfeng-backups readlink -f -- "$BACKUP_PARENT"
  expect_command_output 'root:root 700' stat -c '%U:%G %a' "$BACKUP_PARENT"
else
  install -d -o root -g root -m 0700 "$BACKUP_PARENT"
fi
test ! -e "$BACKUP_DIR"
test ! -L "$BACKUP_DIR"
install -d -o root -g root -m 0700 "$BACKUP_DIR"
test ! -L "$BACKUP_DIR"
expect_command_output /root/data/docker_data/houfeng readlink -f -- "$DEPLOY_DIR"
expect_command_output /root/data/houfeng-backups readlink -f -- "$BACKUP_PARENT"
expect_command_output "/root/data/houfeng-backups/$BACKUP_NAME" readlink -f -- "$BACKUP_DIR"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$BACKUP_PARENT"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$BACKUP_DIR"
resolved_backup_parent="$(readlink -f -- "$BACKUP_PARENT")" || exit 65
resolved_deploy_dir="$(readlink -f -- "$DEPLOY_DIR")" || exit 65
case "$resolved_backup_parent/" in "$resolved_deploy_dir/"*) exit 64 ;; esac

cd -- "$DEPLOY_DIR"
compose_clean config --quiet
command -v python3 findmnt timeout >/dev/null
expected_services=(clamav db houfeng houfeng-content-processor houfeng-db-init houfeng-record-authority houfeng-secrets-init houfeng-storage-init)
configured_services="$(compose_clean config --services | sort | tr '\n' ' ')"
test "$configured_services" = 'clamav db houfeng houfeng-content-processor houfeng-db-init houfeng-record-authority houfeng-secrets-init houfeng-storage-init '
service_container_map="$BACKUP_DIR/service-container-map.txt"
configured_state_sources="$BACKUP_DIR/configured-state-sources.txt"
resolved_bind_sources="$BACKUP_DIR/resolved-bind-sources.txt"
mount_closure_receipt="$BACKUP_DIR/.private-mount-closure-receipt"
fs_metadata_receipt="$BACKUP_DIR/.private-fs-metadata-receipt"
for output_path in "$service_container_map" "$configured_state_sources" "$resolved_bind_sources" \
  "$mount_closure_receipt" "$fs_metadata_receipt"; do
  test ! -e "$output_path"
  test ! -L "$output_path"
done
: >"$service_container_map"
compose_ids=()
for service in "${expected_services[@]}"; do
  service_id="$(compose_clean ps -aq "$service")"
  test -n "$service_id"
  service_id_line_count="$(printf '%s\n' "$service_id" | wc -l)" || exit 65
  test "$service_id_line_count" = 1
  compose_ids+=("$service_id")
  printf '%s\t%s\n' "$service" "$service_id" >>"$service_container_map"
done
service_container_count="$(wc -l <"$service_container_map" | tr -d ' ')" || exit 65
test "$service_container_count" = 8
for container_id in "${compose_ids[@]}"; do
  expect_command_output "$EXPECTED_COMPOSE_PROJECT" docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$container_id"
done
project_container_ids="$(docker ps -aq --filter "label=com.docker.compose.project=$EXPECTED_COMPOSE_PROJECT")"
test -n "$project_container_ids"
expected_container_ids="$(printf '%s\n' "${compose_ids[@]}" | sort)"
actual_container_ids="$(printf '%s\n' "$project_container_ids" | sort)"
test "$actual_container_ids" = "$expected_container_ids"
for service_index in "${!expected_services[@]}"; do
  service="${expected_services[$service_index]}"
  container_id="${compose_ids[$service_index]}"
  case "$service" in
    db) expected_service_image="$OLD_POSTGRES_IMAGE_ID" ;;
    clamav) expected_service_image="$OLD_CLAMAV_IMAGE_ID" ;;
    *) expected_service_image="$OLD_HOUFENG_IMAGE_ID" ;;
  esac
  expect_command_output "$expected_service_image" docker inspect --format '{{.Image}}' "$container_id"
  case "$service" in
    houfeng-storage-init|houfeng-secrets-init|houfeng-db-init)
      expect_command_output exited docker inspect --format '{{.State.Status}}' "$container_id"
      expect_command_output 0 docker inspect --format '{{.State.ExitCode}}' "$container_id"
      ;;
    *) expect_command_output running docker inspect --format '{{.State.Status}}' "$container_id" ;;
  esac
  case "$service" in
    db|clamav|houfeng|houfeng-record-authority)
      expect_command_output healthy docker inspect --format '{{.State.Health.Status}}' "$container_id"
      ;;
  esac
done
timeout --signal=TERM --kill-after=2s 20s "$PRIVATE_MOUNT_CLOSURE_VERIFIER" \
  "$DEPLOY_DIR" "$EXPECTED_COMPOSE_PROJECT" "$service_container_map" \
  "$configured_state_sources" "$resolved_bind_sources" "$mount_closure_receipt" "$fs_metadata_receipt"
for verifier_output in "$service_container_map" "$configured_state_sources" "$resolved_bind_sources" \
  "$mount_closure_receipt" "$fs_metadata_receipt"; do
  test -f "$verifier_output"
  test ! -L "$verifier_output"
  expect_command_output 'root:root 600' stat -c '%U:%G %a' "$verifier_output"
done
test -s "$configured_state_sources"
test -s "$resolved_bind_sources"
mount_receipt_line_count="$(wc -l <"$mount_closure_receipt" | tr -d ' ')" || exit 65
test "$mount_receipt_line_count" = 1
MOUNT_CLOSURE_RECEIPT_SHA256="$(tr -d '\n' <"$mount_closure_receipt")"
[[ "$MOUNT_CLOSURE_RECEIPT_SHA256" =~ ^[0-9a-f]{64}$ ]]
fs_metadata_receipt_line_count="$(wc -l <"$fs_metadata_receipt" | tr -d ' ')" || exit 65
test "$fs_metadata_receipt_line_count" = 1
FS_METADATA_RECEIPT_SHA256="$(tr -d '\n' <"$fs_metadata_receipt")"
[[ "$FS_METADATA_RECEIPT_SHA256" =~ ^[0-9a-f]{64}$ ]]
unlink -- "$service_container_map"
for source_list in "$configured_state_sources" "$resolved_bind_sources"; do
  test -s "$source_list"
  while IFS= read -r state_source; do
    test -n "$state_source"
    test -e "$state_source"
    test ! -L "$state_source"
    test -r "$state_source"
    state_source="$(readlink -f -- "$state_source")"
    case "$state_source/" in "$DEPLOY_DIR/"*) ;; *) exit 65 ;; esac
    if test -d "$state_source"; then
      tree_problem="$(find "$state_source" -xdev -type l -print -quit)" || exit 65
      test -z "$tree_problem"
      tree_problem="$(find "$state_source" -xdev ! -type d ! -type f -print -quit)" || exit 65
      test -z "$tree_problem"
      mount_targets="$(findmnt -Rrn -o TARGET --target "$state_source")" || exit 65
      while IFS= read -r mount_target; do
        case "$mount_target" in "$state_source"|"$state_source"/*) exit 65 ;; esac
      done <<<"$mount_targets"
    else
      test -f "$state_source"
    fi
  done <"$source_list"
done

center_id_before="$(compose_clean ps -q houfeng)"
db_id_before="$(compose_clean ps -q db)"
clamav_id_before="$(compose_clean ps -q clamav)"
test -n "$center_id_before" && test -n "$db_id_before" && test -n "$clamav_id_before"
expect_command_output "$OLD_HOUFENG_IMAGE_ID" docker inspect --format '{{.Image}}' "$center_id_before"
expect_command_output "$OLD_POSTGRES_IMAGE_ID" docker inspect --format '{{.Image}}' "$db_id_before"
expect_command_output "$OLD_CLAMAV_IMAGE_ID" docker inspect --format '{{.Image}}' "$clamav_id_before"
expect_command_output "$OLD_POSTGRES_IMAGE_REF" docker inspect --format '{{.Config.Image}}' "$db_id_before"
expect_command_output "$OLD_CLAMAV_IMAGE_REF" docker inspect --format '{{.Config.Image}}' "$clamav_id_before"
center_image_ref="$(docker inspect --format '{{.Config.Image}}' "$center_id_before")" || exit 65
db_image_ref="$(docker inspect --format '{{.Config.Image}}' "$db_id_before")" || exit 65
clamav_image_ref="$(docker inspect --format '{{.Config.Image}}' "$clamav_id_before")" || exit 65
expect_command_output "$OLD_HOUFENG_IMAGE_ID" docker image inspect --format '{{.Id}}' "$center_image_ref"
expect_command_output "$OLD_POSTGRES_IMAGE_ID" docker image inspect --format '{{.Id}}' "$db_image_ref"
expect_command_output "$OLD_CLAMAV_IMAGE_ID" docker image inspect --format '{{.Id}}' "$clamav_image_ref"
expect_command_output v0.79.4 docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$OLD_HOUFENG_IMAGE_ID"
expect_command_output "$OLD_REVISION" docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$OLD_HOUFENG_IMAGE_ID"
expect_command_output v0.79.6 docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$FIXED_IMAGE_ID"
expect_command_output "$FIXED_REVISION" docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$FIXED_IMAGE_ID"
for image_id in "$OLD_HOUFENG_IMAGE_ID" "$OLD_POSTGRES_IMAGE_ID" "$OLD_CLAMAV_IMAGE_ID" "$FIXED_IMAGE_ID"; do
  expect_command_output linux/amd64 docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image_id"
done
image_provenance_line_count="$(wc -l <"$PRIVATE_IMAGE_PROVENANCE_RECEIPT" | tr -d ' ')" || exit 65
test "$image_provenance_line_count" = 9
grep -Fxq "host=$EXPECTED_HOSTNAME" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
grep -Fxq "machine_id_sha256=$EXPECTED_MACHINE_ID_SHA256" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
grep -Fxq "arch=$EXPECTED_ARCH" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
grep -Fxq "old_houfeng_image_id=$OLD_HOUFENG_IMAGE_ID" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
grep -Fxq "old_houfeng_amd64_manifest_digest=$OLD_HOUFENG_AMD64_MANIFEST_DIGEST" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
grep -Fxq "old_revision=$OLD_REVISION" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
grep -Fxq "fixed_houfeng_image_id=$FIXED_IMAGE_ID" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
grep -Fxq "fixed_houfeng_amd64_manifest_digest=$FIXED_HOUFENG_AMD64_MANIFEST_DIGEST" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
grep -Fxq "fixed_revision=$FIXED_REVISION" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT"
deploy_bytes="$(du -sb -- "$DEPLOY_DIR" | awk '{print $1}')"
old_stack_image_bytes=0
for image_id in "$OLD_HOUFENG_IMAGE_ID" "$OLD_POSTGRES_IMAGE_ID" "$OLD_CLAMAV_IMAGE_ID"; do
  image_size="$(docker image inspect --format '{{.Size}}' "$image_id")"
  old_stack_image_bytes=$(( old_stack_image_bytes + image_size ))
done
fixed_image_bytes="$(docker image inspect --format '{{.Size}}' "$FIXED_IMAGE_ID")"
live_env_sha_before="$(sha256sum "$DEPLOY_DIR/.env" | awk '{print $1}')"
live_compose_sha_before="$(sha256sum "$DEPLOY_DIR/compose.yaml" | awk '{print $1}')"
live_proxy_sha_before="$(sha256sum "$DEPLOY_DIR/compose.proxy-host.yaml" | awk '{print $1}')"
live_proxy_network_sha_before="$(sha256sum "$DEPLOY_DIR/compose.proxy-network.yaml" | awk '{print $1}')"
deploy_device="$(stat -c '%d' "$DEPLOY_DIR")" || exit 65
backup_device="$(stat -c '%d' "$BACKUP_PARENT")" || exit 65
test "$deploy_device" = "$backup_device"
free_bytes="$(df --output=avail -B1 "$BACKUP_PARENT" | tail -n 1 | tr -d ' ')"
capacity_basis=$(( 2 * deploy_bytes + old_stack_image_bytes + fixed_image_bytes ))
required_bytes=$(( capacity_basis + capacity_basis / 4 + 1073741824 ))
(( free_bytes >= required_bytes ))
docker image save --output "$BACKUP_DIR/stack-v0.79.4-images.tar" \
  "$OLD_HOUFENG_IMAGE_ID" "$OLD_POSTGRES_IMAGE_REF" "$OLD_CLAMAV_IMAGE_REF"
verify_saved_image_archive
image_archive_sha_before="$(sha256sum "$BACKUP_DIR/stack-v0.79.4-images.tar" | awk '{print $1}')"

command -v timeout >/dev/null
test -r /proc/uptime
center_outage_start="$(monotonic_seconds)"
CENTER_TOTAL_DEADLINE_SECONDS=$(( center_outage_start + 85 ))
CENTER_VALIDATION_DEADLINE_SECONDS=$(( center_outage_start + 55 ))
RECOVERY_ARMED=1
DEPLOY_PARENT="$DEPLOY_PARENT" DEPLOY_NAME="$DEPLOY_NAME" DEPLOY_DIR="$DEPLOY_DIR" \
BACKUP_DIR="$BACKUP_DIR" OLD_HOUFENG_IMAGE_ID="$OLD_HOUFENG_IMAGE_ID" SAFE_PATH="$SAFE_PATH" \
  timeout --signal=TERM --kill-after=3s 30s /bin/bash --noprofile --norc -c '
    set -euo pipefail
    cd -- "$DEPLOY_DIR"
    env -i PATH="$SAFE_PATH" HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
      DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
      docker compose --env-file "$DEPLOY_DIR/.env" \
      -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" down
    tar --acls --xattrs --numeric-owner \
      -C "$DEPLOY_PARENT" -cpf "$BACKUP_DIR/deployment.tar" "$DEPLOY_NAME"
    env -i PATH="$SAFE_PATH" HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
      DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
      HOUFENG_IMAGE="$OLD_HOUFENG_IMAGE_ID" docker compose --env-file "$DEPLOY_DIR/.env" \
      -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" \
      up --pull never -d
  '
run_with_remaining_timeout 8 sha256sum \
  "$BACKUP_DIR/deployment.tar" "$BACKUP_DIR/stack-v0.79.4-images.tar" \
  >"$BACKUP_DIR/SHA256SUMS"
run_with_remaining_timeout 8 sha256sum -c "$BACKUP_DIR/SHA256SUMS"
image_archive_sha_after="$(run_with_remaining_timeout 3 sha256sum "$BACKUP_DIR/stack-v0.79.4-images.tar" | awk '{print $1}')" || recover_exact_old_stack "$?"
test "$image_archive_sha_after" = "$image_archive_sha_before"
run_with_remaining_timeout 8 tar -tf "$BACKUP_DIR/deployment.tar" \
  >"$BACKUP_DIR/archive-list.txt"
test -s "$BACKUP_DIR/stack-v0.79.4-images.tar"
archive_metadata_receipt="$BACKUP_DIR/.private-archive-metadata-receipt"
test ! -e "$archive_metadata_receipt"
test ! -L "$archive_metadata_receipt"
run_with_remaining_timeout 8 "$PRIVATE_MOUNT_CLOSURE_VERIFIER" --verify-archive-metadata \
  "$BACKUP_DIR/deployment.tar" "$DEPLOY_NAME" "$configured_state_sources" \
  "$resolved_bind_sources" "$fs_metadata_receipt" "$archive_metadata_receipt"
test -f "$archive_metadata_receipt"
test ! -L "$archive_metadata_receipt"
expect_command_output 'root:root 600' run_with_remaining_timeout 2 stat -c '%U:%G %a' "$archive_metadata_receipt"
archive_metadata_line_count="$(run_with_remaining_timeout 2 wc -l "$archive_metadata_receipt" | tr -d ' ')" || recover_exact_old_stack "$?"
test "$archive_metadata_line_count" = 1
ARCHIVE_METADATA_RECEIPT_SHA256="$(run_with_remaining_timeout 2 tr -d '\n' <"$archive_metadata_receipt")"
test "$ARCHIVE_METADATA_RECEIPT_SHA256" = "$FS_METADATA_RECEIPT_SHA256"
archive_env_sha="$(run_with_remaining_timeout 4 tar -xOf "$BACKUP_DIR/deployment.tar" "$DEPLOY_NAME/.env" | sha256sum | awk '{print $1}')"
archive_compose_sha="$(run_with_remaining_timeout 4 tar -xOf "$BACKUP_DIR/deployment.tar" "$DEPLOY_NAME/compose.yaml" | sha256sum | awk '{print $1}')"
archive_proxy_sha="$(run_with_remaining_timeout 4 tar -xOf "$BACKUP_DIR/deployment.tar" "$DEPLOY_NAME/compose.proxy-host.yaml" | sha256sum | awk '{print $1}')"
archive_proxy_network_sha="$(run_with_remaining_timeout 4 tar -xOf "$BACKUP_DIR/deployment.tar" "$DEPLOY_NAME/compose.proxy-network.yaml" | sha256sum | awk '{print $1}')"
test "$archive_env_sha" = "$live_env_sha_before"
test "$archive_compose_sha" = "$live_compose_sha_before"
test "$archive_proxy_sha" = "$live_proxy_sha_before"
test "$archive_proxy_network_sha" = "$live_proxy_network_sha_before"
live_env_sha_after="$(run_with_remaining_timeout 3 sha256sum "$DEPLOY_DIR/.env" | awk '{print $1}')" || recover_exact_old_stack "$?"
live_compose_sha_after="$(run_with_remaining_timeout 3 sha256sum "$DEPLOY_DIR/compose.yaml" | awk '{print $1}')" || recover_exact_old_stack "$?"
live_proxy_sha_after="$(run_with_remaining_timeout 3 sha256sum "$DEPLOY_DIR/compose.proxy-host.yaml" | awk '{print $1}')" || recover_exact_old_stack "$?"
live_proxy_network_sha_after="$(run_with_remaining_timeout 3 sha256sum "$DEPLOY_DIR/compose.proxy-network.yaml" | awk '{print $1}')" || recover_exact_old_stack "$?"
test "$live_env_sha_after" = "$live_env_sha_before"
test "$live_compose_sha_after" = "$live_compose_sha_before"
test "$live_proxy_sha_after" = "$live_proxy_sha_before"
test "$live_proxy_network_sha_after" = "$live_proxy_network_sha_before"
for source_list in "$BACKUP_DIR/configured-state-sources.txt" "$BACKUP_DIR/resolved-bind-sources.txt"; do
  while IFS= read -r state_source; do
    state_source="$(run_with_remaining_timeout 2 readlink -f -- "$state_source")"
    relative_source="${state_source#"$DEPLOY_PARENT"/}"
    test "$relative_source" != "$state_source"
    if test -d "$state_source"; then
      run_with_remaining_timeout 2 grep -Fq -- "${relative_source%/}/" "$BACKUP_DIR/archive-list.txt"
    else
      run_with_remaining_timeout 2 grep -Fxq -- "$relative_source" "$BACKUP_DIR/archive-list.txt"
    fi
  done <"$source_list"
done

for service in "${expected_services[@]}"; do
  restarted_id="$(compose_with_remaining_timeout 3 ps -aq "$service")"
  test -n "$restarted_id"
  restarted_id_line_count="$(printf '%s\n' "$restarted_id" | wc -l)" || recover_exact_old_stack "$?"
  test "$restarted_id_line_count" = 1
  case "$service" in
    db) expected_service_image="$OLD_POSTGRES_IMAGE_ID" ;;
    clamav) expected_service_image="$OLD_CLAMAV_IMAGE_ID" ;;
    *) expected_service_image="$OLD_HOUFENG_IMAGE_ID" ;;
  esac
  expect_command_output "$expected_service_image" run_with_remaining_timeout 3 docker inspect --format '{{.Image}}' "$restarted_id"
  case "$service" in
    houfeng-storage-init|houfeng-secrets-init|houfeng-db-init)
      expect_command_output exited run_with_remaining_timeout 3 docker inspect --format '{{.State.Status}}' "$restarted_id"
      expect_command_output 0 run_with_remaining_timeout 3 docker inspect --format '{{.State.ExitCode}}' "$restarted_id"
      ;;
    *) expect_command_output running run_with_remaining_timeout 3 docker inspect --format '{{.State.Status}}' "$restarted_id" ;;
  esac
  case "$service" in
    db|clamav|houfeng|houfeng-record-authority)
      expect_command_output healthy run_with_remaining_timeout 3 docker inspect --format '{{.State.Health.Status}}' "$restarted_id"
      ;;
  esac
done
center_ready=0
while deadline_has_time; do
  if run_with_remaining_timeout 3 curl --connect-timeout 1 --max-time 2 -fsS \
    "$PUBLIC_BASE_URL/api/healthz" \
    | grep -Eq '"version"[[:space:]]*:[[:space:]]*"v0\.79\.4"'; then
    center_ready=1
    break
  fi
  run_with_remaining_timeout 2 sleep 2
done
test "$center_ready" = 1
expect_command_output 63 compose_with_remaining_timeout 4 exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc "select count(*) from public.schema_migrations"
expect_command_output 1 compose_with_remaining_timeout 4 exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc "select count(*) from public.schema_migrations where name='0062_create_vps_create_idempotency.sql'"
expect_command_output 1 compose_with_remaining_timeout 4 exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc "select manifest_revision from public.app_acl_manifest_head"
post_restart_watermark="$(compose_with_remaining_timeout 4 exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc 'select clock_timestamp()')"
fresh_agent_count=0
while deadline_has_time; do
  fresh_agent_count=0
  for monitoring_instance_id in "$AGENT1_MONITORING_INSTANCE_ID" "$AGENT2_MONITORING_INSTANCE_ID"; do
    latest_agent_version=''
    if latest_agent_version="$(compose_with_remaining_timeout 4 exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -v mi_id="$monitoring_instance_id" -v watermark="$post_restart_watermark" -U postgres -d houfeng -Atc "with recent_by_observed as materialized (select received_at,id,agent_version,is_backfilled from monitoring_instance_heartbeats where monitoring_instance_id=:'mi_id' order by observed_at desc limit 768) select agent_version from recent_by_observed where received_at>:'watermark'::timestamptz and is_backfilled=false order by received_at desc,id desc limit 1")" && test "$latest_agent_version" = v0.79.4; then
      fresh_agent_count=$(( fresh_agent_count + 1 ))
    fi
  done
  test "$fresh_agent_count" = 2 && break
  run_with_remaining_timeout 2 sleep 2
done
test "$fresh_agent_count" = 2
deadline_has_time
running_services="$(compose_with_remaining_timeout 3 ps --status running --services | sort | tr '\n' ' ')"
test "$running_services" = 'clamav db houfeng houfeng-content-processor houfeng-record-authority '
for init_service in houfeng-storage-init houfeng-secrets-init houfeng-db-init; do
  init_id="$(compose_with_remaining_timeout 3 ps -aq "$init_service")"
  test -n "$init_id"
  expect_command_output exited run_with_remaining_timeout 3 docker inspect --format '{{.State.Status}}' "$init_id"
  expect_command_output 0 run_with_remaining_timeout 3 docker inspect --format '{{.State.ExitCode}}' "$init_id"
done

OLD_STACK_RUNTIME_HEALTH_PROVEN=1
private_receipt_file="$BACKUP_DIR/.private-invariant-receipt"
private_verifier_log="$BACKUP_DIR/private-invariant-verifier.log"
test ! -e "$private_receipt_file"
test ! -L "$private_receipt_file"
test ! -e "$private_verifier_log"
test ! -L "$private_verifier_log"
run_with_remaining_timeout 8 "$PRIVATE_INVARIANT_VERIFIER" \
  "$DEPLOY_DIR" "$BACKUP_DIR" "$post_restart_watermark" "$private_receipt_file" \
  >>"$private_verifier_log" 2>&1
test ! -L "$private_receipt_file"
test ! -L "$private_verifier_log"
expect_command_output 'root:root 600' run_with_remaining_timeout 2 stat -c '%U:%G %a' "$private_receipt_file"
expect_command_output 'root:root 600' run_with_remaining_timeout 2 stat -c '%U:%G %a' "$private_verifier_log"
private_receipt_line_count="$(run_with_remaining_timeout 2 wc -l "$private_receipt_file" | tr -d ' ')" || recover_exact_old_stack "$?"
test "$private_receipt_line_count" = 1
PRIVATE_INVARIANT_RECEIPT_SHA256="$(run_with_remaining_timeout 2 tr -d '\n' <"$private_receipt_file")"
[[ "$PRIVATE_INVARIANT_RECEIPT_SHA256" =~ ^[0-9a-f]{64}$ ]]

checksums_sha="$(run_with_remaining_timeout 2 sha256sum "$BACKUP_DIR/SHA256SUMS" | awk '{print $1}')"
configured_sources_sha="$(run_with_remaining_timeout 2 sha256sum "$configured_state_sources" | awk '{print $1}')"
resolved_sources_sha="$(run_with_remaining_timeout 2 sha256sum "$resolved_bind_sources" | awk '{print $1}')"
mount_receipt_file_sha="$(run_with_remaining_timeout 2 sha256sum "$mount_closure_receipt" | awk '{print $1}')"
sources_sha="$(run_with_remaining_timeout 2 /bin/bash --noprofile --norc -c 'set -euo pipefail; printf "configured=%s\nresolved=%s\nclosure=%s\n" "$1" "$2" "$3" | sha256sum | awk "{print \\$1}"' _ \
  "$configured_sources_sha" "$resolved_sources_sha" "$mount_receipt_file_sha")"
deadline_has_time
compose_sources_sha="$(run_with_remaining_timeout 2 /bin/bash --noprofile --norc -c \
  'set -euo pipefail; printf "%s\n" "$1" "$2" "$3" "$4" | sha256sum | awk "{print \\$1}"' _ \
  "$archive_env_sha" "$archive_compose_sha" "$archive_proxy_sha" "$archive_proxy_network_sha")"
marker_tmp="$BACKUP_DIR/.cold-backup.complete.tmp"
test ! -e "$marker_tmp"
test ! -L "$marker_tmp"
deadline_has_time
printf 'host=%s\nmachine_id_sha256=%s\narch=%s\nlock_identity=%s\ndocker_daemon_id=%s\ndocker_socket_metadata=%s\nold_version=v0.79.4\nold_revision=%s\nold_houfeng_image=%s\nold_houfeng_amd64_manifest_digest=%s\nold_postgres_image=%s\nold_clamav_image=%s\nfixed_houfeng_image=%s\nfixed_houfeng_amd64_manifest_digest=%s\nledger_tail=0062\nmanifest_head=1\nsha256sums_sha=%s\nstate_sources_sha=%s\ncompose_sources_sha=%s\nmount_verifier_sha=%s\nmount_closure_sha=%s\nfs_metadata_sha=%s\narchive_metadata_sha=%s\nimage_provenance_receipt_sha=%s\nprivate_verifier_sha=%s\nprivate_invariants_sha=%s\n' \
  "$EXPECTED_HOSTNAME" "$EXPECTED_MACHINE_ID_SHA256" "$EXPECTED_ARCH" "$EXPECTED_HOST_LOCK_IDENTITY" \
  "$EXPECTED_DOCKER_DAEMON_ID" "$EXPECTED_DOCKER_SOCKET_METADATA" "$OLD_REVISION" \
  "$OLD_HOUFENG_IMAGE_ID" "$OLD_HOUFENG_AMD64_MANIFEST_DIGEST" "$OLD_POSTGRES_IMAGE_ID" \
  "$OLD_CLAMAV_IMAGE_ID" "$FIXED_IMAGE_ID" "$FIXED_HOUFENG_AMD64_MANIFEST_DIGEST" \
  "$checksums_sha" "$sources_sha" "$compose_sources_sha" "$PRIVATE_MOUNT_CLOSURE_VERIFIER_SHA256" \
  "$MOUNT_CLOSURE_RECEIPT_SHA256" "$FS_METADATA_RECEIPT_SHA256" \
  "$ARCHIVE_METADATA_RECEIPT_SHA256" "$PRIVATE_IMAGE_PROVENANCE_RECEIPT_SHA256" \
  "$PRIVATE_INVARIANT_VERIFIER_SHA256" "$PRIVATE_INVARIANT_RECEIPT_SHA256" >"$marker_tmp"
deadline_has_time
run_with_remaining_timeout 2 sync -f "$BACKUP_DIR"
run_with_remaining_timeout 2 sync "$marker_tmp"
run_with_remaining_timeout 2 mv -T -- "$marker_tmp" "$BACKUP_DIR/COLD_BACKUP_COMPLETE"
run_with_remaining_timeout 2 sync -f "$BACKUP_DIR"
deadline_has_time
exit 0
```

The production script requires two exact root-owned private verifiers whose SHAs are frozen before any outage. The invariant verifier proves all five long-running services are running, the four declared healthchecks (DB, ClamAV, Center and Records authority) are healthy, all three init services exited zero, the two exact stable MonitoringInstance IDs produced live rows after the restart watermark, Records authority identity, representative Records/attachment readback, and a tight-window no-new-heartbeat-incident/event/notification result. The mount-closure verifier rerenders the exact `env -i` Compose closure in memory and compares every configured service with its frozen container by exact expected image ID plus normalized mount type, canonical source, target, read-only/read-write flag, propagation and supported bind/tmpfs options. It rejects missing/extra targets, mixed Houfeng images, named/unknown mounts, unknown options, service/container drift and orphans. For the six environment-backed Compose secrets it matches each expected target and privately verifies the archived `.env` source bytes against live and staged consumers before `down`; it also privately compares the complete normalized configured-versus-live service environment, including Telegram routing, public URL, trusted proxy, attachment and comparison settings. Secret values are compared only in memory and never emitted. Docker ephemeral secret sources are neither archive state nor shared evidence. The verifier emits only root-owned `0600` configured-source/resolved-persistent-source files plus one-line safe closure and canonical filesystem-metadata digests. The latter covers every root, directory, regular file and empty directory's relative path/type/uid/gid/mode/ACL/xattr and is rederived from the tar before the marker. The transient service-to-container-ID map is verified, consumed and deleted before backup and is never part of a persistent digest, so the closure remains reproducible after container recreation. `COLD_BACKUP_COMPLETE` binds both verifier SHAs and receipts. Dummy real-Compose tests cover image/source/target/mode/options drift, post-`up` `.env` drift against live direct environment and secret bytes, metadata/tar drift, producer expected-output-then-nonzero/timeout failures and non-disclosure.

Every persistent source must be readable, regular-file-or-directory, non-symlinked, recursively free of symlinks/special files/nested mounts, contained in the deployment tree and present in the tar listing. Both proxy overlays are archived and hashed even though production uses only the exact common plus proxy-host pair. The image-provenance receipt binds hostname, machine-id, linux/amd64 platform, local image IDs/revisions and public amd64 manifest digests. The saved-image manifest separately binds the frozen three config digests plus exact PostgreSQL/ClamAV refs, and its tar hash must remain byte-identical. The stop/tar/restart section is capped at 30 seconds plus a three-second kill grace. Successful validation has a separate monotonic 55-second cutoff, while the root recovery handler alone retains the full 85-second deadline, leaving a hard 30-second reserve. Every timeout subtracts its kill grace; clone failure injection must prove the critical, validation, image-load, old-Compose readiness and first-heartbeat paths within those bounds. PostgreSQL probes have a statement timeout. At old 0062, each identity-bound heartbeat query first takes at most 768 rows through the existing `(monitoring_instance_id, observed_at desc)` index and only then filters the post-watermark live row; strict PostgreSQL 16 evidence rejects Seq/Bitmap paths and bounds actual rows. The exact 0063 partial-index plan is a separate post-upgrade/Agent gate. Capacity reserves the backup archive, an additional restored/rehearsal deployment, all three old stack images, the fixed image, 25% headroom and 1 GiB; differing filesystems need a separately reviewed calculation. Never start a restored copy concurrently with its source. If attachments use S3, this template is insufficient and execution stops; live inventory currently identifies a local backend, but execution must reverify it.

A root-owned wrapper, still holding the canonical host lock, publishes `COLD_BACKUP_EXECUTION_RECEIPT` only after directly observing the child script's real exit status `0`. Its fixed schema binds hostname, machine-id digest, lock identity, canonical child-script path+SHA, canonical cold-marker path+SHA and `exit_status=0`; it uses synchronized temp-write, atomic rename and directory sync, and its error path removes/synchronizes any stale receipt. The next go/no-go parses and revalidates both artifacts. The marker cannot bind a future receipt, so the receipt binds the marker. `OLD_STACK_RUNTIME_HEALTH_PROVEN` is set after the explicit exact eight-image/status/health/init/ledger/head/two-live-heartbeat checks. If the subsequent full private verifier fails, the root handler first runs its SHA-frozen `--runtime-health-only` mode within the recovery reserve: only a still-exact, healthy old runtime may remain running while authority/Records/attachment/quiet-window evidence blocks marker/receipt publication; any runtime-health failure clears the flag and enters the bounded old-stack recovery path. If recovery cannot be proven, the script reports an unproven state and requires service/host isolation; it never labels that state fail-stopped without exact inactive/authority proof.

### Upgrade ordering after the blocker is resolved

1. Freeze unrelated changes; record current health, image ID/labels/digest, Compose rendering, migration ledger, manifest head, settings fingerprints, and agent liveness.
2. Take and rehearse the complete cold recovery point.
3. After the bridge ships, download all deployment assets from the exact next-patch release v0.79.6; compare templates without printing secrets; preserve the existing proxy mode and private `.env` values.
4. Pull and inspect the fixed image and confirm its published index/platform digest and OCI revision/version. Do not reuse the blocked v0.79.5 image digest as the target.
5. Run the reviewed migration bridge on an isolated restored copy first. Require `0063` ledger checksum, settings conversion/preservation, index definition, manifest successor, runtime admission, Records write, and attachment read/write evidence.
6. In the production maintenance window, stop application writes, run the exact reviewed bridge/Compose sequence, and stop immediately on any initializer failure. Never bypass dependencies (`docs/deploy/local-and-systemd.md:278-285`, `340-362`).
7. Prove initializer exit 0, authority/DB/ClamAV/Center health, processor running, public `/api/healthz.version=<fixed-release>`, exact image ID/labels/digest, `0063`, settings preservation, a representative admitted Records write, and attachment access.
8. Upgrade one Agent canary and verify three distinct live batches before upgrading the second Agent; see `agent-canary-upgrade-rollback.md`.
9. Keep the cold recovery point until the full acceptance window closes.

### Production cutover fail-stop closure

The backup script's restart-old trap is not the cutover rollback: after 0063/revision 2 may have committed, rollback must replace the entire deployment tree. Generate a separate host-specific cutover script and failure-inject it on the isolated clone. Before its first asset write, image switch, service stop or database mutation it must:

1. Recompute `SHA256SUMS`, canonical configured/runtime closure, filesystem/archive metadata and private invariant receipts; parse `COLD_BACKUP_COMPLETE` as data and require every bound host/Docker-daemon/image/revision/ledger/head field to equal the frozen preflight. Separately parse the exact-path `COLD_BACKUP_EXECUTION_RECEIPT`, rehash its wrapper-bound child script and cold marker, require the same host-lock identity and recorded child exit status exactly zero; neither artifact alone authorizes cutover.
2. Freeze the exact current deployment path, old/target project/container identities and a unique nonexisting production failed-state quarantine child under a canonical, non-symlinked, root-owned `0700` parent. Require the quarantine parent and live deployment parent to have the same device ID before any mutation, recheck free space for the retained failed tree plus a full cold extraction and headroom, and reject any preexisting/dangling child. Cross-filesystem `mv`/copy-delete is forbidden. The quarantine is never deleted by this script or the continuous rollout authorization.
3. Arm `ERR`, `INT=130` and `TERM=143` handlers that preserve the original nonzero code. Once recovery begins, the root handler disables only recursive `ERR`, ignores/defer-records further INT/TERM until the bounded recovery/containment proof finishes, and then exits with the selected original nonzero failure/signal status. It never continues after a failed `cd`, stops only the frozen target project/containers, atomically renames the failed tree once to the exact same-filesystem quarantine and fsyncs both parent directories, revalidates the cold archive, loads the archived Houfeng/PostgreSQL/ClamAV images, and extracts with `tar --acls --xattrs --numeric-owner -xpf` under the prevalidated canonical deployment parent. Before startup it rechecks every required path's owner/mode plus the frozen ACL/xattr metadata receipt and proves that no restored path reaches the quarantine or another authority tree; only then may exact v0.79.4 start with `--pull never`.
4. On any restore substep failure, retain both cold point and failed quarantine, run a bounded stop/kill attempt, and require exact target inactivity plus absence of any partial authority before calling it fail-stopped. If either proof fails, report recovery as unproven and require service/host isolation; never run Compose from another working directory, start a partial tree, or replace the original failure with exit 0.
5. Before considering rollback successful, re-prove v0.79.4 OCI revision/image, 63/0062, revision 1, all init exits/running health, Records authority identity, representative Records/attachment readback, the two stable Agent IDs with post-restore live watermarks, and no new heartbeat incident/event/notification in the tight window.

The target-success path keeps that full-restore handler armed through fixed-image init, 0063/revision-2, settings/index/catalog/authority/Records/attachment and passive notification checks. It disables the handler only after all checks succeed, then atomically publishes and filesystem-syncs a target completion marker bound to the release SHA/digest, cold marker digest and private quiet-window receipt. The same sanitized, lock-holding outer wrapper must then directly observe cutover-script exit `0` and atomically publish `CENTER_CUTOVER_EXECUTION_RECEIPT`, binding host/machine, lock identity, canonical cutover-script path+SHA, completion-marker path+SHA, cold-backup receipt digest and `exit_status=0`. Agent rollout requires both target marker and cutover execution receipt, parsed field by field. Failure injection covers every mutation/cutpoint plus restore failure, signal delivery, delayed readiness and marker-after-exit receipt ordering. Thus neither a health-only target nor an unverified old restart can authorize Agent rollout.

### Post-upgrade database evidence

These read-only queries prove the migration and avoid emitting secrets or full configuration JSON:

```bash
set -euo pipefail
DEPLOY_DIR=/absolute/owner-confirmed/fleet-deployment
DOCKER_HOST=unix:///run/docker.sock
DOCKER_CONFIG=/root/houfeng-rollout/docker-empty-config
compose_clean() {
  timeout --signal=TERM --kill-after=1s 10s env -i \
    PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
    DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
    docker compose --env-file "$DEPLOY_DIR/.env" \
    -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" "$@"
}
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc \
  "select name || ' ' || checksum from public.schema_migrations where name='0063_tune_heartbeat_incident_policy.sql'"
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc \
  "select incident_defaults->>'stale_threshold_intervals', md5(override_rules::text) from center_settings where settings_id='center'"
compose_clean exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db psql -X -v ON_ERROR_STOP=1 -U postgres -d houfeng -Atc \
  "select indexdef from pg_indexes where schemaname='public' and indexname='idx_monitoring_instance_heartbeats_live_received'"
```

The `0063` row must carry checksum `5d30d2eab8a362f691bcfa6d802f7e7474757739260ed20aba7a4b618a011545`. Compare the override hash with the pre-upgrade value. A former global value `3` must become `12`; any other global value and every explicit override must remain unchanged. Do not treat health version alone as migration evidence.

### Rollback points

- Before the migration bridge: no production mutation; leave v0.79.4 running.
- After a fail-closed rehearsal: discard only the isolated clone; do not alter production.
- After production initialization starts: absent explicit backward-compatibility evidence, rollback is the complete cold restore with its matching v0.79.4 Compose/env/image and authority state, not merely changing the image tag (`docs/deploy/local-and-systemd.md:418-426`).
- If the target initializer fails, preserve its logs and state for diagnosis. Do not delete/recreate `data/`, edit authority files, or advance to Agent upgrades.

### Related specs

- `.trellis/spec/backend/database-guidelines.md` — migrations are authoritative; heartbeat migration and strict PostgreSQL evidence contract.
- `.trellis/spec/backend/logging-guidelines.md` — do not expose DSNs, provider credentials, session/enrollment/sync tokens, or notification contents in shared evidence.
- `.trellis/spec/backend/quality-guidelines.md` — strict PostgreSQL checks must actually run and must not be counted through skips.

## Caveats / Not Found

- No repository deployment workflow or host-execution automation was found; the release workflow publishes artifacts only.
- No supported v0.79.4 exact-current manifest successor path for `0063` was found. This is a hard production rollout blocker.
- No automated cold-backup validation or restore drill exists in the repository.
- Separate read-only production inventory confirmed `hostcram:/root/data/docker_data/houfeng`, amd64, proxy-host assets, local PostgreSQL 16.12/ClamAV 1.4.3, v0.79.4/0062/revision 1 and current capacity. Exact private mount/environment/authority receipts still require fresh execution-time revalidation and intentionally are not copied into shared task artifacts.
- The archived v0.79.5 digest/source evidence proves the blocked release only. The fixed release will have different source/artifact digests and must be checked independently against the registry and live pulled image before execution.
