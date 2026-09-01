# Research: dual Agent v0.79.4 to fixed-patch canary upgrade and rollback

- Query: How can the two production Agents be upgraded one at a time, with verifiable version/liveness evidence and a recoverable rollback point?
- Scope: internal
- Date: 2026-08-31

## Findings

### Files found

- `internal/center/installer/houfeng-agent-install.sh` — signed release download, existing credential preservation, filesystem writes, and service restart sequence.
- `docs/deploy/local-and-systemd.md` — generated installer and systemd upgrade contract.
- `docs/deploy/systemd/houfeng-agent.service` — service identity and runtime paths.
- `Makefile` — release agent build/version stamping.
- `agent/runtime/runtime.go` — version stamped into all live carriers.
- `.github/workflows/publish-images.yml` — static amd64/arm64 builds, checksum generation, clean VCS metadata, minisign signature, and release upload.
- `internal/center/store/incidents.go` and `internal/center/store/sync_batches.go` — persisted live carrier evidence.

### Artifact and version contract

- `make build-agent-release VERSION=<fixed-release>` produces static Linux amd64 and arm64 binaries and stamps `houfeng/agent/runtime.agentVersion` (`Makefile:67-86`). The runtime default is `dev`, and the stamped value is carried in heartbeats, host samples, probe observations, and IP-quality reports (`agent/runtime/runtime.go:31`, `440`, `861-904`). The fixed release must be the exact next v0.79.x patch, v0.79.6; production Agents skip the blocked release and must stop if Release Please produces any other version or source range.
- The release workflow independently reconstructs the SHA256 manifest, requires clean VCS metadata and no dynamic libc, signs the manifest with minisign, and publishes exactly both binaries plus `sha256sums.txt` and its signature (`.github/workflows/publish-images.yml:76-176`).
- The installer verifies the signed manifest and exact asset checksum before changing Agent files (`internal/center/installer/houfeng-agent-install.sh:233-247`). It supports Linux systemd on amd64/arm64 (`docs/deploy/local-and-systemd.md:579-589`).
- There is no `houfeng-agent --version` command in `cmd/houfeng-agent/main.go`. The authoritative post-install version is the latest accepted non-backfilled live heartbeat's `agent_version`, joined with the installed binary checksum and service status.

### Installer mutation and rollback gap

The installer is not transactional. After verification it replaces `/usr/local/bin/houfeng-agent`, rewrites `/etc/houfeng-agent/agent.env`, may rewrite `/etc/houfeng-agent/token`, replaces the systemd unit, reloads systemd, and restarts/starts the service (`internal/center/installer/houfeng-agent-install.sh:249-318`). It preserves an existing token only when the file looks like post-enrollment JSON containing a monitoring-instance/node ID plus `sync_token` (`houfeng-agent-install.sh:271-280`). It creates no backup and has no automatic rollback if a later write or restart fails.

Consequently each host needs an operator-created local rollback bundle before running the generated command. Do not print, upload, or paste the token or generated command into evidence; the command contains a fresh 30-minute one-time enrollment token (`docs/deploy/local-and-systemd.md:570-587`).

### Per-host preflight and rollback bundle

Run on exactly one owner-confirmed Agent host at a time. Choose a private absolute backup directory outside `/tmp`:

The actual host-specific script is generated only after freezing `netcup` or `informaten`, its architecture and a unique root-only path. It is never invoked directly by shebang: a SHA-frozen root wrapper opens and locks the canonical file descriptor, then enters through `env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root USER=root LOGNAME=root HOUFENG_ROLLOUT_LOCK_FD=<inherited-fd> /bin/bash --noprofile --norc <canonical-script>`, before any rollout write. The child mechanically checks/acquires that inherited descriptor, rejects any other updater/rollout unit, and the wrapper holds the descriptor through child exit, execution-receipt publication and cleanup. Manual rollback uses the same lock and sanitized entry. The generated script must be shellchecked, independently reviewed and failure-injected. This deliberately non-executable root-script template shows the required fail-fast/restart-old behavior:

```bash
#!/bin/bash
set -Eeuo pipefail
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
unset BASH_ENV ENV CDPATH
umask 077

EXPECTED_HOSTNAME=REPLACE_WITH_REVALIDATED_REMOTE_HOSTNAME
EXPECTED_MACHINE_ID_SHA256=REPLACE_WITH_REVALIDATED_MACHINE_ID_SHA256
EXPECTED_ARCH=REPLACE_WITH_AARCH64_OR_X86_64
HOST_LOCK_PATH=/run/lock/houfeng-rollout.lock
EXPECTED_HOST_LOCK_IDENTITY=REPLACE_WITH_LOCK_PATH_DEVICE_INODE_OWNER_MODE
: "${HOUFENG_ROLLOUT_LOCK_FD:?wrapper must pass the held rollout lock fd}"
BACKUP_PARENT=/root/houfeng-agent-backups
BACKUP_NAME=houfeng-agent-pre-fixed-patch-YYYYMMDDTHHMMSSZ
BACKUP_DIR="$BACKUP_PARENT/$BACKUP_NAME"
ROLLBACK_SCRIPT=/root/houfeng-agent-rollout/REPLACE.rollback.sh
EXPECTED_ROLLBACK_SCRIPT_SHA256=REPLACE_WITH_REVIEWED_ROLLBACK_SCRIPT_SHA256
STATE_METADATA_VERIFIER=/root/houfeng-agent-rollout/REPLACE.state-metadata-verifier.sh
EXPECTED_STATE_METADATA_VERIFIER_SHA256=REPLACE_WITH_STATE_METADATA_VERIFIER_SHA256
EXPECTED_OLD_UNIT_SHA256=dd8eb92954d4e1dc9ddaf472eaa8864c9425ff08aa0272961b098f7727052954
RESTART_OLD=0
ROOT_BASHPID=$BASHPID
BACKUP_OUTAGE_DEADLINE_SECONDS=0

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

restart_old_on_failure() {
  trap '' INT TERM
  trap - ERR
  local original_rc="$1"
  local active_state=''
  local active_state_rc=0
  local enabled_rc=0
  local now=0
  local remaining_seconds=0
  local restart_rc=0
  local unit_file_state=''
  if (( BASHPID != ROOT_BASHPID )); then return "$original_rc"; fi
  if (( RESTART_OLD == 1 )); then
    set +e
    if now="$(monotonic_seconds)"; then
      remaining_seconds=$(( BACKUP_OUTAGE_DEADLINE_SECONDS - now - 8 ))
    fi
    if (( remaining_seconds > 15 )); then remaining_seconds=15; fi
    if (( remaining_seconds > 0 )); then
      timeout --signal=TERM --kill-after=2s "${remaining_seconds}s" systemctl start houfeng-agent
      restart_rc=$?
    else
      restart_rc=124
    fi
    unit_file_state="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p UnitFileState --value)"
    enabled_rc=$?
    if test "$unit_file_state" != enabled; then enabled_rc=125; fi
    active_state="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p ActiveState --value)"
    active_state_rc=$?
    if (( active_state_rc != 0 )) || test "$active_state" != active; then restart_rc=125; fi
    if (( restart_rc != 0 || enabled_rc != 0 )); then
      printf 'old Agent restart could not be proven; preserve evidence and isolate the host for investigation\n' >&2
    fi
  fi
  exit "$original_rc"
}

discard_incomplete_backup_marker() {
  trap '' INT TERM
  trap - ERR
  local original_rc="$1"
  local cleanup_rc=0
  if (( BASHPID != ROOT_BASHPID )); then return "$original_rc"; fi
  set +e
  if test -e "$BACKUP_DIR/AGENT_BACKUP_COMPLETE" || test -L "$BACKUP_DIR/AGENT_BACKUP_COMPLETE"; then
    timeout --signal=KILL 2s unlink -- "$BACKUP_DIR/AGENT_BACKUP_COMPLETE" || cleanup_rc=$?
  fi
  timeout --signal=KILL 2s sync -f "$BACKUP_DIR" || cleanup_rc=$?
  if (( cleanup_rc != 0 )); then
    printf 'backup marker cleanup/directory sync could not be proven; promotion is forbidden and host evidence must be isolated\n' >&2
  fi
  exit "$original_rc"
}

trap 'restart_old_on_failure "$?"' ERR
trap 'restart_old_on_failure 130' INT
trap 'restart_old_on_failure 143' TERM

(( EUID == 0 ))
case "$EXPECTED_HOSTNAME:$EXPECTED_MACHINE_ID_SHA256:$EXPECTED_ARCH:$EXPECTED_HOST_LOCK_IDENTITY:$ROLLBACK_SCRIPT:$EXPECTED_ROLLBACK_SCRIPT_SHA256:$STATE_METADATA_VERIFIER:$EXPECTED_STATE_METADATA_VERIFIER_SHA256" in *REPLACE*) exit 64 ;; esac
[[ "$EXPECTED_ROLLBACK_SCRIPT_SHA256" =~ ^[0-9a-f]{64}$ ]] || exit 64
[[ "$EXPECTED_STATE_METADATA_VERIFIER_SHA256" =~ ^[0-9a-f]{64}$ ]] || exit 64
case "$BACKUP_NAME" in houfeng-agent-pre-fixed-patch-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z) ;; *) exit 64 ;; esac
expect_command_output "$EXPECTED_HOSTNAME" hostname -s
expect_command_output "$EXPECTED_MACHINE_ID_SHA256" bounded_sha256_file /etc/machine-id
expect_command_output "$EXPECTED_ARCH" uname -m
case "$EXPECTED_ARCH" in
  aarch64) EXPECTED_OLD_BINARY_SHA256=450a25c705f54371e8f44f649f63df244314ecf6d75809ce82cddc7306d6ea67 ;;
  x86_64) EXPECTED_OLD_BINARY_SHA256=e608b8c8efe020d77783943e996b8cb47facfc19363588f4a4c5fe833537eef7 ;;
  *) exit 64 ;;
esac
command -v timeout flock >/dev/null
command -v findmnt >/dev/null
test -r /proc/uptime
[[ "$HOUFENG_ROLLOUT_LOCK_FD" =~ ^[0-9]+$ ]]
test -e "/proc/$BASHPID/fd/$HOUFENG_ROLLOUT_LOCK_FD"
expect_command_output "$HOST_LOCK_PATH" readlink -f -- "/proc/$BASHPID/fd/$HOUFENG_ROLLOUT_LOCK_FD"
test -f "$HOST_LOCK_PATH"
test ! -L "$HOST_LOCK_PATH"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$HOST_LOCK_PATH"
host_lock_identity="$(stat -c '%n|%d|%i|%U:%G|%a' "$HOST_LOCK_PATH")"
test "$host_lock_identity" = "$EXPECTED_HOST_LOCK_IDENTITY"
flock -n "$HOUFENG_ROLLOUT_LOCK_FD"
test ! -e "$BACKUP_DIR"
test ! -L "$BACKUP_DIR"
for live_file in /usr/local/bin/houfeng-agent /etc/houfeng-agent/agent.env /etc/houfeng-agent/token /etc/systemd/system/houfeng-agent.service; do
  test -f "$live_file"
  test ! -L "$live_file"
  test -s "$live_file"
done
test -x /usr/local/bin/houfeng-agent
for canonical_parent in /usr/local/bin /etc/houfeng-agent /etc/systemd/system /var/lib/houfeng-agent; do
  test -d "$canonical_parent"
  test ! -L "$canonical_parent"
  expect_command_output "$canonical_parent" readlink -f -- "$canonical_parent"
done
for live_file in /usr/local/bin/houfeng-agent /etc/houfeng-agent/agent.env /etc/houfeng-agent/token /etc/systemd/system/houfeng-agent.service; do
  expect_command_output "$live_file" readlink -f -- "$live_file"
done
expect_command_output enabled systemctl show houfeng-agent.service -p UnitFileState --value
systemctl is-active --quiet houfeng-agent
expect_command_output /etc/systemd/system/houfeng-agent.service systemctl show houfeng-agent.service -p FragmentPath --value
drop_in_paths="$(systemctl show houfeng-agent.service -p DropInPaths --value)"
test -z "$drop_in_paths"
expect_command_output "$EXPECTED_OLD_BINARY_SHA256" bounded_sha256_file /usr/local/bin/houfeng-agent
expect_command_output "$EXPECTED_OLD_UNIT_SHA256" bounded_sha256_file /etc/systemd/system/houfeng-agent.service
expect_command_output 'root:root 755' stat -c '%U:%G %a' /usr/local/bin/houfeng-agent
expect_command_output 'root:houfeng-agent 640' stat -c '%U:%G %a' /etc/houfeng-agent/agent.env
expect_command_output 'houfeng-agent:houfeng-agent 600' stat -c '%U:%G %a' /etc/houfeng-agent/token
expect_command_output 'root:root 644' stat -c '%U:%G %a' /etc/systemd/system/houfeng-agent.service
test ! -L "$ROLLBACK_SCRIPT"
test -f "$ROLLBACK_SCRIPT"
test -x "$ROLLBACK_SCRIPT"
expect_command_output "$ROLLBACK_SCRIPT" readlink -f -- "$ROLLBACK_SCRIPT"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$ROLLBACK_SCRIPT"
expect_command_output "$EXPECTED_ROLLBACK_SCRIPT_SHA256" bounded_sha256_file "$ROLLBACK_SCRIPT"
test ! -L "$STATE_METADATA_VERIFIER"
test -f "$STATE_METADATA_VERIFIER"
test -x "$STATE_METADATA_VERIFIER"
expect_command_output "$STATE_METADATA_VERIFIER" readlink -f -- "$STATE_METADATA_VERIFIER"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$STATE_METADATA_VERIFIER"
expect_command_output "$EXPECTED_STATE_METADATA_VERIFIER_SHA256" bounded_sha256_file "$STATE_METADATA_VERIFIER"
test ! -L /var/lib/houfeng-agent
expect_command_output /var/lib/houfeng-agent readlink -f -- /var/lib/houfeng-agent
mount_targets="$(findmnt -Rrn -o TARGET --target /var/lib/houfeng-agent)" || exit 65
while IFS= read -r mount_target; do
  case "$mount_target" in /var/lib/houfeng-agent|/var/lib/houfeng-agent/*) exit 65 ;; esac
done <<<"$mount_targets"
tree_problem="$(find /var/lib/houfeng-agent -xdev -type l -print -quit)" || exit 65
test -z "$tree_problem"
tree_problem="$(find /var/lib/houfeng-agent -xdev ! -type d ! -type f -print -quit)" || exit 65
test -z "$tree_problem"

state_bytes="$(du -sb -- /var/lib/houfeng-agent /usr/local/bin/houfeng-agent /etc/houfeng-agent /etc/systemd/system/houfeng-agent.service | awk '{sum += $1} END {print sum}')"
test ! -L /root
expect_command_output /root readlink -f -- /root
if test -e "$BACKUP_PARENT" || test -L "$BACKUP_PARENT"; then
  test -d "$BACKUP_PARENT"
  test ! -L "$BACKUP_PARENT"
  expect_command_output /root/houfeng-agent-backups readlink -f -- "$BACKUP_PARENT"
  expect_command_output 'root:root 700' stat -c '%U:%G %a' "$BACKUP_PARENT"
else
  install -d -o root -g root -m 0700 "$BACKUP_PARENT"
fi
free_bytes="$(df --output=avail -B1 "$BACKUP_PARENT" | tail -n 1 | tr -d ' ')"
required_bytes=$(( state_bytes + state_bytes / 4 + 67108864 ))
(( free_bytes >= required_bytes ))
install -d -o root -g root -m 0700 "$BACKUP_DIR"
test ! -L "$BACKUP_DIR"
expect_command_output "/root/houfeng-agent-backups/$BACKUP_NAME" readlink -f -- "$BACKUP_DIR"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$BACKUP_DIR"
: >"$BACKUP_DIR/LIVE_PATH_METADATA"
for canonical_parent in /usr/local/bin /etc/houfeng-agent /etc/systemd/system /var/lib/houfeng-agent; do
  parent_metadata="$(stat -c '%n|%d|%U:%G|%a' "$canonical_parent")"
  printf 'parent=%s\n' "$parent_metadata" >>"$BACKUP_DIR/LIVE_PATH_METADATA"
done
for live_file in /usr/local/bin/houfeng-agent /etc/houfeng-agent/agent.env /etc/houfeng-agent/token /etc/systemd/system/houfeng-agent.service; do
  resolved_live_file="$(readlink -f -- "$live_file")" || exit 65
  printf 'file=%s\n' "$resolved_live_file" >>"$BACKUP_DIR/LIVE_PATH_METADATA"
done
old_binary_sha="$EXPECTED_OLD_BINARY_SHA256"
old_env_sha="$(sha256sum /etc/houfeng-agent/agent.env | awk '{print $1}')"
old_token_sha="$(sha256sum /etc/houfeng-agent/token | awk '{print $1}')"
printf 'binary=root:root 755\nenv=root:houfeng-agent 640\ntoken=houfeng-agent:houfeng-agent 600\nunit=root:root 644\n' \
  >"$BACKUP_DIR/BUNDLE_METADATA"
for property in FragmentPath DropInPaths ExecStart EnvironmentFiles User Group StateDirectory ReadWritePaths; do
  property_value="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p "$property" --value)"
  printf '%s=%s\n' "$property" "$property_value"
done >"$BACKUP_DIR/EFFECTIVE_UNIT_METADATA"
effective_metadata_line_count="$(wc -l <"$BACKUP_DIR/EFFECTIVE_UNIT_METADATA" | tr -d ' ')" || exit 65
test "$effective_metadata_line_count" = 8
for property in FragmentPath DropInPaths ExecStart EnvironmentFiles User Group StateDirectory ReadWritePaths; do
  property_count="$(grep -c "^${property}=" "$BACKUP_DIR/EFFECTIVE_UNIT_METADATA")" || exit 65
  test "$property_count" = 1
done

backup_outage_start="$(monotonic_seconds)"
BACKUP_OUTAGE_DEADLINE_SECONDS=$(( backup_outage_start + 75 ))
RESTART_OLD=1
BACKUP_DIR="$BACKUP_DIR" STATE_METADATA_VERIFIER="$STATE_METADATA_VERIFIER" \
  timeout --signal=TERM --kill-after=5s 45s /bin/bash --noprofile --norc -c '
  set -euo pipefail
  systemctl stop houfeng-agent
  "$STATE_METADATA_VERIFIER" capture /var/lib/houfeng-agent "$BACKUP_DIR/LIVE_STATE_METADATA"
  cp -a -- /usr/local/bin/houfeng-agent "$BACKUP_DIR/houfeng-agent"
  cp -a -- /etc/houfeng-agent/agent.env "$BACKUP_DIR/agent.env"
  cp -a -- /etc/houfeng-agent/token "$BACKUP_DIR/token"
  cp -a -- /etc/systemd/system/houfeng-agent.service "$BACKUP_DIR/houfeng-agent.service"
  cp -a -- /var/lib/houfeng-agent "$BACKUP_DIR/state"
  "$STATE_METADATA_VERIFIER" capture "$BACKUP_DIR/state" "$BACKUP_DIR/STATE_METADATA"
  cmp -s -- "$BACKUP_DIR/LIVE_STATE_METADATA" "$BACKUP_DIR/STATE_METADATA"
  unlink -- "$BACKUP_DIR/LIVE_STATE_METADATA"
  cd -- "$BACKUP_DIR"
  test ! -L state
  test -d state
  for bundle_file in houfeng-agent agent.env token houfeng-agent.service BUNDLE_METADATA EFFECTIVE_UNIT_METADATA LIVE_PATH_METADATA STATE_METADATA; do
    test -f "$bundle_file"
    test ! -L "$bundle_file"
  done
  tree_problem="$(find state -xdev -type l -print -quit)" || exit 65
  test -z "$tree_problem"
  tree_problem="$(find state -xdev ! -type d ! -type f -print -quit)" || exit 65
  test -z "$tree_problem"
  find houfeng-agent agent.env token houfeng-agent.service state BUNDLE_METADATA EFFECTIVE_UNIT_METADATA LIVE_PATH_METADATA STATE_METADATA -type f -print0 \
    | sort -z | xargs -0 sha256sum >SHA256SUMS
  sha256sum -c SHA256SUMS
  for required_file in houfeng-agent agent.env token houfeng-agent.service STATE_METADATA; do
    grep -Eq "^[0-9a-f]{64}  ${required_file}$" SHA256SUMS
  done
  systemctl start houfeng-agent
'
expect_command_output enabled timeout --signal=KILL 2s systemctl show houfeng-agent.service -p UnitFileState --value
expect_command_output active timeout --signal=KILL 2s systemctl show houfeng-agent.service -p ActiveState --value
for property in FragmentPath DropInPaths ExecStart EnvironmentFiles User Group StateDirectory ReadWritePaths; do
  property_value="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p "$property" --value)"
  grep -Fxq "$property=$property_value" "$BACKUP_DIR/EFFECTIVE_UNIT_METADATA"
done
expect_command_output "$old_binary_sha" bounded_sha256_file /usr/local/bin/houfeng-agent
expect_command_output "$old_binary_sha" bounded_sha256_file "$BACKUP_DIR/houfeng-agent"
expect_command_output "$EXPECTED_OLD_UNIT_SHA256" bounded_sha256_file "$BACKUP_DIR/houfeng-agent.service"
expect_command_output "$old_env_sha" bounded_sha256_file "$BACKUP_DIR/agent.env"
expect_command_output "$old_token_sha" bounded_sha256_file "$BACKUP_DIR/token"
cmp -s -- /usr/local/bin/houfeng-agent "$BACKUP_DIR/houfeng-agent"
cmp -s -- /etc/houfeng-agent/agent.env "$BACKUP_DIR/agent.env"
cmp -s -- /etc/houfeng-agent/token "$BACKUP_DIR/token"
cmp -s -- /etc/systemd/system/houfeng-agent.service "$BACKUP_DIR/houfeng-agent.service"
for bundle_file in "$BACKUP_DIR/houfeng-agent" "$BACKUP_DIR/agent.env" "$BACKUP_DIR/token" "$BACKUP_DIR/houfeng-agent.service" \
  "$BACKUP_DIR/LIVE_PATH_METADATA"; do
  test -f "$bundle_file"
  test ! -L "$bundle_file"
done
expect_command_output 'root:root 755' stat -c '%U:%G %a' "$BACKUP_DIR/houfeng-agent"
expect_command_output 'root:houfeng-agent 640' stat -c '%U:%G %a' "$BACKUP_DIR/agent.env"
expect_command_output 'houfeng-agent:houfeng-agent 600' stat -c '%U:%G %a' "$BACKUP_DIR/token"
expect_command_output 'root:root 644' stat -c '%U:%G %a' "$BACKUP_DIR/houfeng-agent.service"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_DIR/BUNDLE_METADATA"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_DIR/EFFECTIVE_UNIT_METADATA"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_DIR/LIVE_PATH_METADATA"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_DIR/STATE_METADATA"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_DIR/SHA256SUMS"
RESTART_OLD=0
trap 'discard_incomplete_backup_marker "$?"' ERR
trap 'discard_incomplete_backup_marker 130' INT
trap 'discard_incomplete_backup_marker 143' TERM

verify_private_center_old_agent_receipt() {
  printf 'replace this fail-closed stub with a post-restart stable-instance live-batch and queue-drain receipt\n' >&2
  return 64
}
CENTER_OLD_AGENT_RECEIPT_SHA256=''
verify_private_center_old_agent_receipt
[[ "$CENTER_OLD_AGENT_RECEIPT_SHA256" =~ ^[0-9a-f]{64}$ ]]

sha256sums_sha="$(sha256sum "$BACKUP_DIR/SHA256SUMS" | awk '{print $1}')"
backup_env_sha="$(sha256sum "$BACKUP_DIR/agent.env" | awk '{print $1}')"
backup_token_sha="$(sha256sum "$BACKUP_DIR/token" | awk '{print $1}')"
effective_unit_sha="$(sha256sum "$BACKUP_DIR/EFFECTIVE_UNIT_METADATA" | awk '{print $1}')"
live_path_metadata_sha="$(sha256sum "$BACKUP_DIR/LIVE_PATH_METADATA" | awk '{print $1}')"
state_metadata_sha="$(sha256sum "$BACKUP_DIR/STATE_METADATA" | awk '{print $1}')"
marker_tmp="$BACKUP_DIR/.agent-backup.complete.tmp"
printf 'host=%s\nmachine_id_sha256=%s\narch=%s\nbundle_dir=%s\nrollback_script_path=%s\nrollback_script_sha256=%s\nstate_metadata_verifier_path=%s\nstate_metadata_verifier_sha256=%s\nold_version=v0.79.4\nold_binary_sha256=%s\nold_unit_sha256=%s\nenv_sha256=%s\ntoken_sha256=%s\neffective_unit_sha256=%s\nlive_path_metadata_sha256=%s\nstate_metadata_sha256=%s\nsha256sums_sha=%s\ncenter_receipt_sha256=%s\n' \
  "$EXPECTED_HOSTNAME" "$EXPECTED_MACHINE_ID_SHA256" "$EXPECTED_ARCH" "$BACKUP_DIR" \
  "$ROLLBACK_SCRIPT" "$EXPECTED_ROLLBACK_SCRIPT_SHA256" "$STATE_METADATA_VERIFIER" \
  "$EXPECTED_STATE_METADATA_VERIFIER_SHA256" "$old_binary_sha" \
  "$EXPECTED_OLD_UNIT_SHA256" "$old_env_sha" "$old_token_sha" "$effective_unit_sha" \
  "$live_path_metadata_sha" "$state_metadata_sha" "$sha256sums_sha" \
  "$CENTER_OLD_AGENT_RECEIPT_SHA256" >"$marker_tmp"
sync -f "$BACKUP_DIR"
sync "$marker_tmp"
mv -- "$marker_tmp" "$BACKUP_DIR/AGENT_BACKUP_COMPLETE"
sync -f "$BACKUP_DIR"
exit 0
```

This is a future operator template, not an executed command. The fail-closed Center receipt stub must be replaced by the private orchestrator; therefore the shared template cannot mint a marker. The complete stop/copy/checksum/restart section has a 45-second watchdog inside a 75-second monotonic backup outage deadline. Timeout returns nonzero and the root handler restarts the old binary, then proves persisted `UnitFileState=enabled` plus exact `ActiveState=active`; a `systemctl start` return code alone is insufficient. The SHA-frozen state-metadata verifier records every state root/directory/regular file/empty directory's relative path, type, uid/gid and mode, proves `cp -a` preserved that canonical manifest, and binds it into both `SHA256SUMS` and the backup marker; it also rejects the state root or any descendant mount. `LIVE_PATH_METADATA` binds each canonical live path and every canonical parent directory's device/owner/mode. Rollback revalidates the same authorities and same-filesystem stage names, then groups all target stages by `st_dev` and proves aggregate bytes plus headroom fit on each device before writing. After the old service is already restored and verified, the script switches directly from restart-old traps to marker-cleanup traps without ever ignoring INT/TERM; those cleanup traps remain installed through the actual successful exit, so no signal cutpoint can leave an authoritative marker with a failed status. Rollback verification checks the state snapshot but never overwrites newer live state by default. Recovery ignores additional INT/TERM only while a bounded recovery proof is running and preserves the already selected status. The sanitized root wrapper holds the host lock throughout and publishes `AGENT_BACKUP_EXECUTION_RECEIPT` only after directly observing backup-script exit `0`; the synchronized atomic receipt binds host/machine, lock identity, canonical backup-script path+SHA and exact backup-marker path+SHA. The supervisor revalidates both receipt and marker, so neither can authorize installation alone.

### Secret installer supervisor

The Center-generated command is written only to a host-local root-owned `0600` file over the private SSH session; its value is never an argument, environment variable, task artifact or shared log. A SHA-frozen private parser binds the exact command-file digest to a root-only Center issuance data receipt containing host, machine-id, architecture, stable MonitoringInstance ID, exact v0.79.6 revision, expiry and issuance identity. It also proves the unique HTTPS installer URL, public server URL, release repository, one token heredoc, `--enrollment-token-stdin`, `--install-missing-deps`, and absence of `--insecure-allow-http`, duplicate/conflicting flags or secret argv. This receipt proves signed issuance data rather than script execution; while holding the same host lock, the supervisor rehashes both command and receipt and reruns the parser immediately before launch, closing the replacement window without inventing a third outer execution receipt. The existing canonical minisign binary must already be a regular root-owned executable whose byte digest and package/upstream provenance receipt are frozen; the preflight also proves the actual sanitized/root `sudo sh` resolution sees that same binary and no required dependency/user/group is missing. Missing or unknown prerequisites block rollout, so automatic dependency installation is not authorized despite the generated compatibility flag. A reviewed root-shell supervisor launches only the command-file path through a unique prevalidated systemd transient service with sanitized `env -i`, cgroup v2, `Type=exec`, `ExitType=cgroup`, `RemainAfterExit=yes` and `KillMode=control-group`. Immediately before any transient-unit launch attempt it unconditionally freezes an 85-second total deadline and a 15-second success-validation cutoff, leaving 70 seconds for cgroup kill, exact rollback, local receipt, cleanup and the first Center-accepted heartbeat; the isolated clone must prove that the real signed download, install and fixed-local/first-live success path fits inside 15 seconds. Before dispatch acceptance is established, exact `LoadState=not-found` plus absence of the expected cgroup path proves that no installer unit exists, so pre-launch cancellation or an explicitly failed dispatch preserves the old Agent as persisted-enabled and active without invoking its disable/stop path. Once `systemd-run` returns zero or a loaded unit/cgroup is observed, the acceptance latch is permanent; rollback starts only for a loaded exact unit whose `ControlGroup` equals `/system.slice/<unit>` and whose regular readable `cgroup.procs` is empty. After acceptance, `LoadState=not-found`, an empty/drifted ControlGroup, or missing/unreadable/nonempty membership is unproven and requires emergency host isolation rather than racing rollback against a live installer. This structural template contains no command/token and remains non-executable until exact private paths are frozen:

```bash
#!/bin/bash
set -Eeuo pipefail
set +x
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
unset BASH_ENV ENV CDPATH
umask 077

ROLLOUT_DIR=/root/houfeng-agent-rollout
EXPECTED_HOSTNAME=REPLACE_WITH_REVALIDATED_REMOTE_HOSTNAME
EXPECTED_MACHINE_ID_SHA256=REPLACE_WITH_REVALIDATED_MACHINE_ID_SHA256
EXPECTED_ARCH=REPLACE_WITH_AARCH64_OR_X86_64
EXPECTED_MONITORING_INSTANCE_ID=REPLACE_WITH_REVALIDATED_STABLE_MONITORING_INSTANCE_ID
HOST_LOCK_PATH=/run/lock/houfeng-rollout.lock
EXPECTED_HOST_LOCK_IDENTITY=REPLACE_WITH_LOCK_PATH_DEVICE_INODE_OWNER_MODE
: "${HOUFENG_ROLLOUT_LOCK_FD:?wrapper must pass the held rollout lock fd}"
SECRET_INSTALLER_FILE="$ROLLOUT_DIR/REPLACE.secret-installer.sh"
EXPECTED_SECRET_INSTALLER_SHA256=REPLACE_WITH_SECRET_INSTALLER_SHA256
INSTALLER_ISSUANCE_RECEIPT="$ROLLOUT_DIR/REPLACE.installer-issuance-receipt"
EXPECTED_INSTALLER_ISSUANCE_RECEIPT_SHA256=REPLACE_WITH_INSTALLER_ISSUANCE_RECEIPT_SHA256
INSTALLER_ISSUANCE_VERIFIER="$ROLLOUT_DIR/REPLACE.installer-issuance-verifier.sh"
EXPECTED_INSTALLER_ISSUANCE_VERIFIER_SHA256=REPLACE_WITH_INSTALLER_ISSUANCE_VERIFIER_SHA256
EXPECTED_INSTALLER_URL=https://fleet.yading.de/api/agent/install.sh
EXPECTED_PUBLIC_SERVER_URL=https://fleet.yading.de
EXPECTED_RELEASE_REPOSITORY=xiangnan0811/houfeng
MINISIGN_BIN=REPLACE_WITH_CANONICAL_EXISTING_MINISIGN_PATH
EXPECTED_MINISIGN_SHA256=REPLACE_WITH_VERIFIED_MINISIGN_SHA256
MINISIGN_PROVENANCE_RECEIPT="$ROLLOUT_DIR/REPLACE.minisign-provenance-receipt"
EXPECTED_MINISIGN_PROVENANCE_RECEIPT_SHA256=REPLACE_WITH_MINISIGN_PROVENANCE_RECEIPT_SHA256
PRIVATE_INSTALL_LOG="$ROLLOUT_DIR/REPLACE.private-installer.log"
ROLLBACK_SCRIPT="$ROLLOUT_DIR/REPLACE.rollback.sh"
BACKUP_MARKER=/root/houfeng-agent-backups/REPLACE/AGENT_BACKUP_COMPLETE
EXPECTED_BACKUP_MARKER_SHA256=REPLACE_WITH_AGENT_BACKUP_MARKER_SHA256
BACKUP_EXECUTION_RECEIPT=/root/houfeng-agent-backups/REPLACE/AGENT_BACKUP_EXECUTION_RECEIPT
EXPECTED_BACKUP_EXECUTION_RECEIPT_SHA256=REPLACE_WITH_BACKUP_EXECUTION_RECEIPT_SHA256
EXPECTED_BACKUP_SCRIPT_PATH=/root/houfeng-agent-rollout/REPLACE.backup.sh
EXPECTED_BACKUP_SCRIPT_SHA256=REPLACE_WITH_BACKUP_SCRIPT_SHA256
EXPECTED_ROLLBACK_SCRIPT_SHA256=REPLACE_WITH_REVIEWED_ROLLBACK_SCRIPT_SHA256
EXPECTED_FIXED_BINARY_SHA256=REPLACE_WITH_ARCH_SPECIFIC_FIXED_BINARY_SHA256
EXPECTED_FIXED_UNIT_SHA256=REPLACE_WITH_FIXED_UNIT_SHA256
EXPECTED_FIXED_REVISION=REPLACE_WITH_FIXED_RELEASE_SOURCE_SHA
FIXED_LOCAL_VERIFIER="$ROLLOUT_DIR/REPLACE.fixed-local-verifier.sh"
EXPECTED_FIXED_LOCAL_VERIFIER_SHA256=REPLACE_WITH_FIXED_LOCAL_VERIFIER_SHA256
FIRST_LIVE_RECEIPT_FILE="$ROLLOUT_DIR/REPLACE.first-live-receipt"
FIXED_LOCAL_RECEIPT_FILE="$ROLLOUT_DIR/REPLACE.fixed-local-receipt"
ROLLBACK_LOCAL_RECEIPT_FILE="$ROLLOUT_DIR/REPLACE.rollback-local-receipt"
SUPERVISOR_EXECUTION_RECEIPT_FILE="$ROLLOUT_DIR/REPLACE.supervisor-execution-receipt"
INSTALLER_UNIT=REPLACE_WITH_UNIQUE_HOST_INSTALLER_TRANSIENT_UNIT.service
INSTALLER_CONTROL_GROUP="/system.slice/$INSTALLER_UNIT"
INSTALLER_UNIT_STARTED=0
INSTALLER_DISPATCH_ACCEPTED=0
SUPERVISOR_ARMED=0
SECRET_CLEANUP_ARMED=0
ROOT_BASHPID=$BASHPID
OUTAGE_DEADLINE_SECONDS=0
OUTAGE_VALIDATION_DEADLINE_SECONDS=0
LAST_ACTIVE_SECONDS=0
PENDING_SIGNAL=0

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

run_with_outage_timeout() {
  local requested_seconds="$1"
  local kill_grace_seconds="$2"
  local now
  local remaining_seconds
  shift 2
  now="$(monotonic_seconds)" || return 124
  remaining_seconds=$(( OUTAGE_DEADLINE_SECONDS - now - kill_grace_seconds ))
  (( remaining_seconds > 0 )) || return 124
  if (( requested_seconds > remaining_seconds )); then requested_seconds="$remaining_seconds"; fi
  timeout --signal=TERM --kill-after="${kill_grace_seconds}s" "${requested_seconds}s" "$@"
}

run_with_optional_outage_timeout() {
  local requested_seconds="$1"
  local kill_grace_seconds="$2"
  shift 2
  if (( OUTAGE_DEADLINE_SECONDS > 0 )); then
    run_with_outage_timeout "$requested_seconds" "$kill_grace_seconds" "$@"
  else
    timeout --signal=TERM --kill-after="${kill_grace_seconds}s" "${requested_seconds}s" "$@"
  fi
}

run_with_optional_validation_timeout() {
  local requested_seconds="$1"
  local kill_grace_seconds="$2"
  local now
  local remaining_seconds
  shift 2
  if (( OUTAGE_VALIDATION_DEADLINE_SECONDS == 0 )); then
    timeout --signal=TERM --kill-after="${kill_grace_seconds}s" "${requested_seconds}s" "$@"
    return
  fi
  now="$(monotonic_seconds)" || return 124
  remaining_seconds=$(( OUTAGE_VALIDATION_DEADLINE_SECONDS - now - kill_grace_seconds ))
  (( remaining_seconds > 0 )) || return 124
  if (( requested_seconds > remaining_seconds )); then requested_seconds="$remaining_seconds"; fi
  timeout --signal=TERM --kill-after="${kill_grace_seconds}s" "${requested_seconds}s" "$@"
}

outage_deadline_has_time() {
  local now
  (( OUTAGE_DEADLINE_SECONDS == 0 )) && return 0
  now="$(monotonic_seconds)" || return 1
  (( now < OUTAGE_DEADLINE_SECONDS ))
}

validation_deadline_has_time() {
  local now
  (( OUTAGE_VALIDATION_DEADLINE_SECONDS == 0 )) && return 0
  now="$(monotonic_seconds)" || return 1
  (( now < OUTAGE_VALIDATION_DEADLINE_SECONDS ))
}

agent_is_proven_fail_stopped() {
  local unit_file_state
  local active_state
  unit_file_state="$(run_with_outage_timeout 2 1 systemctl show houfeng-agent.service -p UnitFileState --value)" || return 1
  active_state="$(run_with_outage_timeout 2 1 systemctl show houfeng-agent.service -p ActiveState --value)" || return 1
  run_with_outage_timeout 2 1 sync -f /etc/systemd/system || return 1
  test "$unit_file_state" = disabled && \
    { test "$active_state" = inactive || test "$active_state" = failed; }
}

contain_untrusted_agent() {
  local disable_rc=0
  local sync_rc=0
  local stop_rc=0
  run_with_outage_timeout 2 1 systemctl disable houfeng-agent || disable_rc=$?
  run_with_outage_timeout 2 1 sync -f /etc/systemd/system || sync_rc=$?
  run_with_outage_timeout 2 1 systemctl stop houfeng-agent || stop_rc=$?
  if ! agent_is_proven_fail_stopped; then
    run_with_outage_timeout 2 1 systemctl kill --kill-whom=all --signal=KILL houfeng-agent || true
    stop_rc=0
    run_with_outage_timeout 2 1 systemctl stop houfeng-agent || stop_rc=$?
  fi
  (( disable_rc == 0 && sync_rc == 0 && stop_rc == 0 )) && agent_is_proven_fail_stopped
}

installer_unit_is_quiesced() {
  local active_state
  local control_group
  local deadline_scope="${1:-outage}"
  local load_state
  local procs
  local sub_state
  local procs_file
  local timeout_runner=run_with_optional_outage_timeout
  case "$deadline_scope" in
    outage) ;;
    validation) timeout_runner=run_with_optional_validation_timeout ;;
    *) return 1 ;;
  esac
  if ! load_state="$("$timeout_runner" 2 1 systemctl show "$INSTALLER_UNIT" -p LoadState --value)"; then
    INSTALLER_DISPATCH_ACCEPTED=1
    return 1
  fi
  if test "$load_state" = not-found; then
    if test -e "/sys/fs/cgroup${INSTALLER_CONTROL_GROUP}"; then
      INSTALLER_DISPATCH_ACCEPTED=1
      return 1
    fi
    (( INSTALLER_DISPATCH_ACCEPTED == 0 ))
    return
  fi
  INSTALLER_DISPATCH_ACCEPTED=1
  test "$load_state" = loaded || return 1
  active_state="$("$timeout_runner" 2 1 systemctl show "$INSTALLER_UNIT" -p ActiveState --value)" || return 1
  sub_state="$("$timeout_runner" 2 1 systemctl show "$INSTALLER_UNIT" -p SubState --value)" || return 1
  control_group="$("$timeout_runner" 2 1 systemctl show "$INSTALLER_UNIT" -p ControlGroup --value)" || return 1
  test "$control_group" = "$INSTALLER_CONTROL_GROUP" || return 1
  procs_file="/sys/fs/cgroup${control_group}/cgroup.procs"
  test -f "$procs_file" && test -r "$procs_file" || return 1
  procs="$("$timeout_runner" 2 1 tr -d '[:space:]' <"$procs_file")" || return 1
  test -z "$procs" || return 1
  { test "$active_state" = inactive || test "$active_state" = failed || \
    { test "$active_state" = active && test "$sub_state" = exited; }; }
}

installer_unit_is_pristine_unaccepted() {
  local load_state
  if ! load_state="$(run_with_optional_outage_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p LoadState --value)"; then
    INSTALLER_DISPATCH_ACCEPTED=1
    return 1
  fi
  if test "$load_state" != not-found || test -e "/sys/fs/cgroup${INSTALLER_CONTROL_GROUP}"; then
    INSTALLER_DISPATCH_ACCEPTED=1
    return 1
  fi
  return 0
}

terminate_installer_unit_bounded() {
  local now
  local reap_deadline
  (( INSTALLER_UNIT_STARTED == 1 )) || return 0
  run_with_outage_timeout 2 1 systemctl kill --kill-whom=all --signal=TERM "$INSTALLER_UNIT" || true
  run_with_outage_timeout 2 1 sleep 2 || true
  run_with_outage_timeout 2 1 systemctl kill --kill-whom=all --signal=KILL "$INSTALLER_UNIT" || true
  now="$(monotonic_seconds)" || return 1
  reap_deadline=$(( now + 3 ))
  if (( reap_deadline > OUTAGE_DEADLINE_SECONDS - 1 )); then
    reap_deadline=$(( OUTAGE_DEADLINE_SECONDS - 1 ))
  fi
  while :; do
    if installer_unit_is_quiesced; then
      break
    fi
    now="$(monotonic_seconds)" || return 1
    (( now < reap_deadline )) || return 1
    run_with_outage_timeout 1 1 sleep 1 || return 1
  done
  run_with_outage_timeout 2 1 systemctl stop "$INSTALLER_UNIT" || return 1
  INSTALLER_UNIT_STARTED=0
}

rollback_and_exit() {
  trap '' INT TERM
  trap - ERR
  local original_rc="$1"
  local cleanup_rc=0
  local rollback_rc=0
  local containment_rc=0
  local installer_quiesced=1
  local local_restore_proven=0
  local now=0
  if (( BASHPID != ROOT_BASHPID )); then return "$original_rc"; fi
  if (( PENDING_SIGNAL != 0 )); then original_rc="$PENDING_SIGNAL"; fi
  if (( SUPERVISOR_ARMED == 0 && SECRET_CLEANUP_ARMED == 0 )); then
    exit "$original_rc"
  fi
  set +e
  if (( OUTAGE_DEADLINE_SECONDS == 0 )); then
    if now="$(monotonic_seconds)"; then
      if (( LAST_ACTIVE_SECONDS > 0 )); then
        OUTAGE_DEADLINE_SECONDS=$(( LAST_ACTIVE_SECONDS + 85 ))
      else
        OUTAGE_DEADLINE_SECONDS=$(( now + 85 ))
      fi
    else
      OUTAGE_DEADLINE_SECONDS=1
    fi
  fi
  if (( SUPERVISOR_ARMED == 1 && INSTALLER_UNIT_STARTED == 1 )); then
    if (( INSTALLER_DISPATCH_ACCEPTED == 0 )) && installer_unit_is_pristine_unaccepted; then
      INSTALLER_UNIT_STARTED=0
      SUPERVISOR_ARMED=0
    elif ! terminate_installer_unit_bounded; then
      installer_quiesced=0
    fi
  fi
  if (( SUPERVISOR_ARMED == 1 && installer_quiesced == 1 )); then
    run_with_outage_timeout 42 3 \
      "$ROLLBACK_SCRIPT" restore "$BACKUP_MARKER" "$EXPECTED_BACKUP_MARKER_SHA256" \
      "$ROLLBACK_LOCAL_RECEIPT_FILE"
    rollback_rc=$?
    if (( rollback_rc != 0 )); then
      if run_with_outage_timeout 8 1 "$ROLLBACK_SCRIPT" --verify-local-restore \
        "$BACKUP_MARKER" "$EXPECTED_BACKUP_MARKER_SHA256" "$ROLLBACK_LOCAL_RECEIPT_FILE"; then
        local_restore_proven=1
      elif ! agent_is_proven_fail_stopped; then
        contain_untrusted_agent
        containment_rc=$?
      fi
    fi
  elif (( SUPERVISOR_ARMED == 1 )); then
    rollback_rc=126
    if ! agent_is_proven_fail_stopped; then
      contain_untrusted_agent
      containment_rc=$?
    fi
  fi
  if (( SECRET_CLEANUP_ARMED == 1 )) && { test -e "$SECRET_INSTALLER_FILE" || test -L "$SECRET_INSTALLER_FILE"; }; then
    run_with_outage_timeout 2 1 unlink -- "$SECRET_INSTALLER_FILE" || cleanup_rc=$?
  fi
  if (( SUPERVISOR_ARMED == 1 )); then
    for failed_receipt in "$FIRST_LIVE_RECEIPT_FILE" "$FIXED_LOCAL_RECEIPT_FILE"; do
      if test -e "$failed_receipt" || test -L "$failed_receipt"; then
        run_with_outage_timeout 2 1 unlink -- "$failed_receipt" || cleanup_rc=$?
      fi
    done
  fi
  run_with_outage_timeout 2 1 sync -f "$ROLLOUT_DIR" || cleanup_rc=$?
  if (( rollback_rc != 0 )); then
    printf 'automatic rollback failed with status %s; preserving original supervisor status %s\n' \
      "$rollback_rc" "$original_rc" >&2
    if (( installer_quiesced == 0 )); then
      printf 'installer transient-cgroup quiescence could not be proven; containment is not durable against later writes and emergency host isolation is required\n' >&2
    elif (( local_restore_proven == 1 )); then
      printf 'exact old Agent is locally verified enabled and active; Center receipt/rollback marker is missing, so promotion remains blocked\n' >&2
    elif (( containment_rc == 0 )); then
      printf 'the untrusted Agent is verified disabled and stopped; promotion remains blocked\n' >&2
    else
      printf 'Agent containment could not be proven; emergency host isolation is required\n' >&2
    fi
  fi
  if (( cleanup_rc != 0 )); then
    printf 'secret/receipt cleanup or rollout-directory sync could not be proven; isolate the host evidence and forbid promotion\n' >&2
  fi
  if (( original_rc == 0 )); then original_rc=1; fi
  exit "$original_rc"
}
trap 'rollback_and_exit "$?"' ERR
trap 'rollback_and_exit 130' INT
trap 'rollback_and_exit 143' TERM

(( EUID == 0 ))
case "$EXPECTED_HOSTNAME:$EXPECTED_MACHINE_ID_SHA256:$EXPECTED_ARCH:$EXPECTED_MONITORING_INSTANCE_ID:$EXPECTED_HOST_LOCK_IDENTITY:$SECRET_INSTALLER_FILE:$EXPECTED_SECRET_INSTALLER_SHA256:$INSTALLER_ISSUANCE_RECEIPT:$EXPECTED_INSTALLER_ISSUANCE_RECEIPT_SHA256:$INSTALLER_ISSUANCE_VERIFIER:$EXPECTED_INSTALLER_ISSUANCE_VERIFIER_SHA256:$EXPECTED_INSTALLER_URL:$EXPECTED_PUBLIC_SERVER_URL:$EXPECTED_RELEASE_REPOSITORY:$MINISIGN_BIN:$EXPECTED_MINISIGN_SHA256:$MINISIGN_PROVENANCE_RECEIPT:$EXPECTED_MINISIGN_PROVENANCE_RECEIPT_SHA256" in *REPLACE*) exit 64 ;; esac
case "$PRIVATE_INSTALL_LOG:$ROLLBACK_SCRIPT:$BACKUP_MARKER:$EXPECTED_BACKUP_MARKER_SHA256:$BACKUP_EXECUTION_RECEIPT:$EXPECTED_BACKUP_EXECUTION_RECEIPT_SHA256:$EXPECTED_BACKUP_SCRIPT_PATH:$EXPECTED_BACKUP_SCRIPT_SHA256:$EXPECTED_ROLLBACK_SCRIPT_SHA256:$EXPECTED_FIXED_BINARY_SHA256:$EXPECTED_FIXED_UNIT_SHA256:$EXPECTED_FIXED_REVISION:$FIXED_LOCAL_VERIFIER:$EXPECTED_FIXED_LOCAL_VERIFIER_SHA256:$FIRST_LIVE_RECEIPT_FILE:$FIXED_LOCAL_RECEIPT_FILE:$ROLLBACK_LOCAL_RECEIPT_FILE:$SUPERVISOR_EXECUTION_RECEIPT_FILE:$INSTALLER_UNIT" in *REPLACE*) exit 64 ;; esac
for expected_sha in "$EXPECTED_MACHINE_ID_SHA256" "$EXPECTED_SECRET_INSTALLER_SHA256" \
  "$EXPECTED_INSTALLER_ISSUANCE_RECEIPT_SHA256" "$EXPECTED_INSTALLER_ISSUANCE_VERIFIER_SHA256" \
  "$EXPECTED_MINISIGN_SHA256" "$EXPECTED_MINISIGN_PROVENANCE_RECEIPT_SHA256" \
  "$EXPECTED_BACKUP_MARKER_SHA256" "$EXPECTED_BACKUP_EXECUTION_RECEIPT_SHA256" \
  "$EXPECTED_BACKUP_SCRIPT_SHA256" "$EXPECTED_ROLLBACK_SCRIPT_SHA256" \
  "$EXPECTED_FIXED_BINARY_SHA256" "$EXPECTED_FIXED_UNIT_SHA256" \
  "$EXPECTED_FIXED_LOCAL_VERIFIER_SHA256"; do
  [[ "$expected_sha" =~ ^[0-9a-f]{64}$ ]] || exit 64
done
[[ "$EXPECTED_FIXED_REVISION" =~ ^[0-9a-f]{40}$ ]] || exit 64
command -v timeout systemd-run systemctl install env flock curl mktemp sudo getent >/dev/null
expect_command_output /usr/bin/sudo command -v sudo
test -x /usr/bin/head
test -r /proc/uptime
test -f /sys/fs/cgroup/cgroup.controllers
test -r /sys/fs/cgroup/cgroup.controllers
expect_command_output "$EXPECTED_HOSTNAME" hostname -s
expect_command_output "$EXPECTED_MACHINE_ID_SHA256" bounded_sha256_file /etc/machine-id
expect_command_output "$EXPECTED_ARCH" uname -m
case "$EXPECTED_ARCH" in aarch64|x86_64) ;; *) exit 64 ;; esac
[[ "$HOUFENG_ROLLOUT_LOCK_FD" =~ ^[0-9]+$ ]]
test -e "/proc/$BASHPID/fd/$HOUFENG_ROLLOUT_LOCK_FD"
expect_command_output "$HOST_LOCK_PATH" readlink -f -- "/proc/$BASHPID/fd/$HOUFENG_ROLLOUT_LOCK_FD"
test -f "$HOST_LOCK_PATH"
test ! -L "$HOST_LOCK_PATH"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$HOST_LOCK_PATH"
host_lock_identity="$(stat -c '%n|%d|%i|%U:%G|%a' "$HOST_LOCK_PATH")"
test "$host_lock_identity" = "$EXPECTED_HOST_LOCK_IDENTITY"
flock -n "$HOUFENG_ROLLOUT_LOCK_FD"
test ! -L "$ROLLOUT_DIR"
expect_command_output /root/houfeng-agent-rollout readlink -f -- "$ROLLOUT_DIR"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$ROLLOUT_DIR"
test ! -L "$SECRET_INSTALLER_FILE"
test ! -L "$INSTALLER_ISSUANCE_RECEIPT"
test ! -L "$INSTALLER_ISSUANCE_VERIFIER"
test ! -L "$MINISIGN_BIN"
test ! -L "$MINISIGN_PROVENANCE_RECEIPT"
test ! -L "$ROLLBACK_SCRIPT"
test ! -L "$FIXED_LOCAL_VERIFIER"
for required_regular in "$SECRET_INSTALLER_FILE" "$INSTALLER_ISSUANCE_RECEIPT" \
  "$INSTALLER_ISSUANCE_VERIFIER" "$MINISIGN_BIN" "$MINISIGN_PROVENANCE_RECEIPT" \
  "$ROLLBACK_SCRIPT" "$FIXED_LOCAL_VERIFIER"; do
  test -f "$required_regular"
done
for required_executable in "$INSTALLER_ISSUANCE_VERIFIER" "$MINISIGN_BIN" \
  "$ROLLBACK_SCRIPT" "$FIXED_LOCAL_VERIFIER"; do
  test -x "$required_executable"
done
expect_command_output "$ROLLOUT_DIR" dirname -- "$SECRET_INSTALLER_FILE"
expect_command_output "$ROLLOUT_DIR" dirname -- "$INSTALLER_ISSUANCE_RECEIPT"
expect_command_output "$ROLLOUT_DIR" dirname -- "$INSTALLER_ISSUANCE_VERIFIER"
expect_command_output "$ROLLOUT_DIR" dirname -- "$MINISIGN_PROVENANCE_RECEIPT"
expect_command_output "$ROLLOUT_DIR" dirname -- "$PRIVATE_INSTALL_LOG"
expect_command_output "$ROLLOUT_DIR" dirname -- "$ROLLBACK_SCRIPT"
expect_command_output "$ROLLOUT_DIR" dirname -- "$FIXED_LOCAL_VERIFIER"
expect_command_output "$ROLLOUT_DIR" dirname -- "$FIRST_LIVE_RECEIPT_FILE"
expect_command_output "$ROLLOUT_DIR" dirname -- "$FIXED_LOCAL_RECEIPT_FILE"
expect_command_output "$ROLLOUT_DIR" dirname -- "$ROLLBACK_LOCAL_RECEIPT_FILE"
expect_command_output "$ROLLOUT_DIR" dirname -- "$SUPERVISOR_EXECUTION_RECEIPT_FILE"
expect_command_output "$SECRET_INSTALLER_FILE" readlink -f -- "$SECRET_INSTALLER_FILE"
expect_command_output "$INSTALLER_ISSUANCE_RECEIPT" readlink -f -- "$INSTALLER_ISSUANCE_RECEIPT"
expect_command_output "$INSTALLER_ISSUANCE_VERIFIER" readlink -f -- "$INSTALLER_ISSUANCE_VERIFIER"
expect_command_output "$MINISIGN_BIN" readlink -f -- "$MINISIGN_BIN"
expect_command_output "$MINISIGN_PROVENANCE_RECEIPT" readlink -f -- "$MINISIGN_PROVENANCE_RECEIPT"
expect_command_output "$ROLLBACK_SCRIPT" readlink -f -- "$ROLLBACK_SCRIPT"
expect_command_output "$FIXED_LOCAL_VERIFIER" readlink -f -- "$FIXED_LOCAL_VERIFIER"
expect_command_output "$PRIVATE_INSTALL_LOG" readlink -m -- "$PRIVATE_INSTALL_LOG"
expect_command_output "$FIRST_LIVE_RECEIPT_FILE" readlink -m -- "$FIRST_LIVE_RECEIPT_FILE"
expect_command_output "$FIXED_LOCAL_RECEIPT_FILE" readlink -m -- "$FIXED_LOCAL_RECEIPT_FILE"
expect_command_output "$ROLLBACK_LOCAL_RECEIPT_FILE" readlink -m -- "$ROLLBACK_LOCAL_RECEIPT_FILE"
expect_command_output "$SUPERVISOR_EXECUTION_RECEIPT_FILE" readlink -m -- "$SUPERVISOR_EXECUTION_RECEIPT_FILE"
test ! -e "$PRIVATE_INSTALL_LOG"
test ! -L "$PRIVATE_INSTALL_LOG"
test ! -e "$FIRST_LIVE_RECEIPT_FILE"
test ! -L "$FIRST_LIVE_RECEIPT_FILE"
test ! -e "$FIXED_LOCAL_RECEIPT_FILE"
test ! -L "$FIXED_LOCAL_RECEIPT_FILE"
test ! -e "$ROLLBACK_LOCAL_RECEIPT_FILE"
test ! -L "$ROLLBACK_LOCAL_RECEIPT_FILE"
test ! -e "$SUPERVISOR_EXECUTION_RECEIPT_FILE"
test ! -L "$SUPERVISOR_EXECUTION_RECEIPT_FILE"
case "$INSTALLER_UNIT" in houfeng-agent-rollout-*.service) ;; *) exit 64 ;; esac
expect_command_output not-found timeout --signal=KILL 2s systemctl show "$INSTALLER_UNIT" -p LoadState --value
test ! -e "/sys/fs/cgroup${INSTALLER_CONTROL_GROUP}"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$SECRET_INSTALLER_FILE"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$INSTALLER_ISSUANCE_RECEIPT"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$INSTALLER_ISSUANCE_VERIFIER"
expect_command_output 'root:root 755' stat -c '%U:%G %a' "$MINISIGN_BIN"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$MINISIGN_PROVENANCE_RECEIPT"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$ROLLBACK_SCRIPT"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$FIXED_LOCAL_VERIFIER"
test ! -L "$BACKUP_MARKER"
test -f "$BACKUP_MARKER"
test ! -L "$BACKUP_EXECUTION_RECEIPT"
test -f "$BACKUP_EXECUTION_RECEIPT"
backup_marker_dir="$(dirname -- "$BACKUP_MARKER")" || exit 65
expect_command_output "$backup_marker_dir" dirname -- "$BACKUP_EXECUTION_RECEIPT"
expect_command_output "$BACKUP_EXECUTION_RECEIPT" readlink -f -- "$BACKUP_EXECUTION_RECEIPT"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_EXECUTION_RECEIPT"
test ! -L "$EXPECTED_BACKUP_SCRIPT_PATH"
test -f "$EXPECTED_BACKUP_SCRIPT_PATH"
test -x "$EXPECTED_BACKUP_SCRIPT_PATH"
expect_command_output "$EXPECTED_BACKUP_SCRIPT_PATH" readlink -f -- "$EXPECTED_BACKUP_SCRIPT_PATH"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$EXPECTED_BACKUP_SCRIPT_PATH"
expect_command_output "$EXPECTED_BACKUP_SCRIPT_SHA256" bounded_sha256_file "$EXPECTED_BACKUP_SCRIPT_PATH"
expect_command_output "$EXPECTED_BACKUP_MARKER_SHA256" bounded_sha256_file "$BACKUP_MARKER"
expect_command_output "$EXPECTED_BACKUP_EXECUTION_RECEIPT_SHA256" bounded_sha256_file "$BACKUP_EXECUTION_RECEIPT"
backup_receipt_line_count="$(wc -l <"$BACKUP_EXECUTION_RECEIPT" | tr -d ' ')" || exit 65
test "$backup_receipt_line_count" = 8
grep -Fxq "host=$EXPECTED_HOSTNAME" "$BACKUP_EXECUTION_RECEIPT"
grep -Fxq "machine_id_sha256=$EXPECTED_MACHINE_ID_SHA256" "$BACKUP_EXECUTION_RECEIPT"
grep -Fxq "lock_identity=$EXPECTED_HOST_LOCK_IDENTITY" "$BACKUP_EXECUTION_RECEIPT"
grep -Fxq "script_path=$EXPECTED_BACKUP_SCRIPT_PATH" "$BACKUP_EXECUTION_RECEIPT"
grep -Fxq "script_sha256=$EXPECTED_BACKUP_SCRIPT_SHA256" "$BACKUP_EXECUTION_RECEIPT"
grep -Fxq "marker_path=$BACKUP_MARKER" "$BACKUP_EXECUTION_RECEIPT"
grep -Fxq "marker_sha256=$EXPECTED_BACKUP_MARKER_SHA256" "$BACKUP_EXECUTION_RECEIPT"
grep -Fxq 'exit_status=0' "$BACKUP_EXECUTION_RECEIPT"
expect_command_output "$EXPECTED_SECRET_INSTALLER_SHA256" bounded_sha256_file "$SECRET_INSTALLER_FILE"
expect_command_output "$EXPECTED_INSTALLER_ISSUANCE_RECEIPT_SHA256" bounded_sha256_file "$INSTALLER_ISSUANCE_RECEIPT"
expect_command_output "$EXPECTED_INSTALLER_ISSUANCE_VERIFIER_SHA256" bounded_sha256_file "$INSTALLER_ISSUANCE_VERIFIER"
expect_command_output "$EXPECTED_MINISIGN_SHA256" bounded_sha256_file "$MINISIGN_BIN"
expect_command_output "$EXPECTED_MINISIGN_PROVENANCE_RECEIPT_SHA256" bounded_sha256_file "$MINISIGN_PROVENANCE_RECEIPT"
expect_command_output "$MINISIGN_BIN" env -i \
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  /bin/sh -c 'command -v minisign'
expect_command_output "$MINISIGN_BIN" timeout --signal=KILL 3s env -i \
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOME=/root USER=root LOGNAME=root \
  /usr/bin/sudo -n /bin/sh -c 'command -v minisign'
getent group houfeng-agent >/dev/null
getent passwd houfeng-agent >/dev/null
expect_command_output "$EXPECTED_ROLLBACK_SCRIPT_SHA256" bounded_sha256_file "$ROLLBACK_SCRIPT"
expect_command_output "$EXPECTED_FIXED_LOCAL_VERIFIER_SHA256" bounded_sha256_file "$FIXED_LOCAL_VERIFIER"
timeout --signal=TERM --kill-after=2s 10s "$INSTALLER_ISSUANCE_VERIFIER" \
  "$INSTALLER_ISSUANCE_RECEIPT" "$EXPECTED_INSTALLER_ISSUANCE_RECEIPT_SHA256" \
  "$SECRET_INSTALLER_FILE" "$EXPECTED_SECRET_INSTALLER_SHA256" \
  "$EXPECTED_HOSTNAME" "$EXPECTED_MACHINE_ID_SHA256" "$EXPECTED_ARCH" \
  "$EXPECTED_MONITORING_INSTANCE_ID" v0.79.6 "$EXPECTED_FIXED_REVISION" \
  "$EXPECTED_INSTALLER_URL" "$EXPECTED_PUBLIC_SERVER_URL" "$EXPECTED_RELEASE_REPOSITORY" \
  --require-enrollment-token-stdin --require-install-missing-deps --forbid-insecure-allow-http \
  "$MINISIGN_BIN" "$EXPECTED_MINISIGN_SHA256" "$MINISIGN_PROVENANCE_RECEIPT" \
  "$EXPECTED_MINISIGN_PROVENANCE_RECEIPT_SHA256"
SECRET_CLEANUP_ARMED=1
install -o root -g root -m 0600 /dev/null "$PRIVATE_INSTALL_LOG"
record_pending_signal() {
  if (( BASHPID != ROOT_BASHPID )); then return 0; fi
  if test "$1" = 130 || (( PENDING_SIGNAL == 0 )); then PENDING_SIGNAL="$1"; fi
  return 0
}
handle_runtime_signal() {
  trap '' INT TERM
  trap - ERR
  local signal_rc="$1"
  if (( PENDING_SIGNAL != 0 )); then
    signal_rc="$PENDING_SIGNAL"
  else
    PENDING_SIGNAL="$signal_rc"
  fi
  rollback_and_exit "$signal_rc"
}
trap 'record_pending_signal 130' INT
trap 'record_pending_signal 143' TERM
if (( PENDING_SIGNAL != 0 )); then rollback_and_exit "$PENDING_SIGNAL"; fi
expect_command_output "$EXPECTED_SECRET_INSTALLER_SHA256" bounded_sha256_file "$SECRET_INSTALLER_FILE"
expect_command_output "$EXPECTED_INSTALLER_ISSUANCE_RECEIPT_SHA256" bounded_sha256_file "$INSTALLER_ISSUANCE_RECEIPT"
timeout --signal=TERM --kill-after=2s 10s \
  "$ROLLBACK_SCRIPT" --verify-only "$BACKUP_MARKER" "$EXPECTED_BACKUP_MARKER_SHA256" \
  "$ROLLBACK_LOCAL_RECEIPT_FILE"
timeout --signal=TERM --kill-after=2s 10s "$INSTALLER_ISSUANCE_VERIFIER" \
  "$INSTALLER_ISSUANCE_RECEIPT" "$EXPECTED_INSTALLER_ISSUANCE_RECEIPT_SHA256" \
  "$SECRET_INSTALLER_FILE" "$EXPECTED_SECRET_INSTALLER_SHA256" \
  "$EXPECTED_HOSTNAME" "$EXPECTED_MACHINE_ID_SHA256" "$EXPECTED_ARCH" \
  "$EXPECTED_MONITORING_INSTANCE_ID" v0.79.6 "$EXPECTED_FIXED_REVISION" \
  "$EXPECTED_INSTALLER_URL" "$EXPECTED_PUBLIC_SERVER_URL" "$EXPECTED_RELEASE_REPOSITORY" \
  --require-enrollment-token-stdin --require-install-missing-deps --forbid-insecure-allow-http \
  "$MINISIGN_BIN" "$EXPECTED_MINISIGN_SHA256" "$MINISIGN_PROVENANCE_RECEIPT" \
  "$EXPECTED_MINISIGN_PROVENANCE_RECEIPT_SHA256"
expect_command_output "$EXPECTED_SECRET_INSTALLER_SHA256" bounded_sha256_file "$SECRET_INSTALLER_FILE"
expect_command_output "$EXPECTED_INSTALLER_ISSUANCE_RECEIPT_SHA256" bounded_sha256_file "$INSTALLER_ISSUANCE_RECEIPT"
if (( PENDING_SIGNAL != 0 )); then rollback_and_exit "$PENDING_SIGNAL"; fi
outage_start="$(monotonic_seconds)"
OUTAGE_DEADLINE_SECONDS=$(( outage_start + 85 ))
OUTAGE_VALIDATION_DEADLINE_SECONDS=$(( outage_start + 15 ))
SUPERVISOR_ARMED=1
INSTALLER_UNIT_STARTED=1
if (( PENDING_SIGNAL != 0 )); then rollback_and_exit "$PENDING_SIGNAL"; fi
installer_launch_rc=0
if run_with_optional_validation_timeout 5 2 systemd-run --quiet --no-block \
  --unit="$INSTALLER_UNIT" --service-type=exec --expand-environment=no \
  --property=ExitType=cgroup --property=RemainAfterExit=yes \
  --property=KillMode=control-group --property=TimeoutStopSec=2s \
  --property="StandardOutput=append:$PRIVATE_INSTALL_LOG" \
  --property="StandardError=append:$PRIVATE_INSTALL_LOG" \
  /usr/bin/env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    HOME=/root USER=root LOGNAME=root /bin/bash --noprofile --norc "$SECRET_INSTALLER_FILE"; then
  installer_launch_rc=0
else
  installer_launch_rc=$?
fi
if (( installer_launch_rc == 0 )); then INSTALLER_DISPATCH_ACCEPTED=1; fi
trap 'handle_runtime_signal 130' INT
trap 'handle_runtime_signal 143' TERM
if (( PENDING_SIGNAL != 0 )); then rollback_and_exit "$PENDING_SIGNAL"; fi
if (( installer_launch_rc != 0 )); then rollback_and_exit "$installer_launch_rc"; fi
expect_command_output exec run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p Type --value
expect_command_output cgroup run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p ExitType --value
expect_command_output yes run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p RemainAfterExit --value
expect_command_output control-group run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p KillMode --value
expect_command_output 2s run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p TimeoutStopUSec --value
expect_command_output "append:$PRIVATE_INSTALL_LOG" run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p StandardOutput --value
expect_command_output "append:$PRIVATE_INSTALL_LOG" run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p StandardError --value
expect_command_output "$INSTALLER_CONTROL_GROUP" run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p ControlGroup --value
inactive_since=0
overall_start="$(monotonic_seconds)"
overall_deadline="$OUTAGE_DEADLINE_SECONDS"
LAST_ACTIVE_SECONDS="$overall_start"
watchdog_rc=0
installer_finished=0
while (( installer_finished == 0 )); do
  now="$(monotonic_seconds)"
  if (( now >= overall_deadline )); then
    watchdog_rc=124
    break
  fi
  if (( OUTAGE_VALIDATION_DEADLINE_SECONDS > 0 && now >= OUTAGE_VALIDATION_DEADLINE_SECONDS )); then
    watchdog_rc=124
    break
  fi
  if (( OUTAGE_DEADLINE_SECONDS > 0 && now >= OUTAGE_DEADLINE_SECONDS )); then
    watchdog_rc=124
    break
  fi
  installer_active_state="$(run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p ActiveState --value)" || {
    watchdog_rc=125
    break
  }
  installer_sub_state="$(run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p SubState --value)" || {
    watchdog_rc=125
    break
  }
  case "$installer_active_state:$installer_sub_state" in
    active:exited|inactive:*|failed:*) installer_finished=1 ;;
    activating:*|active:running|active:start|deactivating:*) ;;
    *) watchdog_rc=125; break ;;
  esac
  agent_active_state="$(run_with_optional_validation_timeout 2 1 systemctl show houfeng-agent.service -p ActiveState --value)" || {
    watchdog_rc=125
    break
  }
  if test "$agent_active_state" = active; then
    inactive_since=0
    LAST_ACTIVE_SECONDS="$now"
  else
    case "$agent_active_state" in inactive|failed|activating|deactivating) ;; *) watchdog_rc=125; break ;; esac
    if (( inactive_since == 0 )); then
      inactive_since="$now"
    fi
    if (( now - inactive_since >= 15 )); then
      watchdog_rc=124
      break
    fi
  fi
  if (( installer_finished == 0 )); then run_with_optional_validation_timeout 1 1 sleep 1; fi
done
if (( watchdog_rc != 0 )); then rollback_and_exit "$watchdog_rc"; fi
installer_result="$(run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p Result --value)"
installer_status="$(run_with_optional_validation_timeout 2 1 systemctl show "$INSTALLER_UNIT" -p ExecMainStatus --value)"
[[ "$installer_status" =~ ^[0-9]+$ ]] || rollback_and_exit 125
case "$installer_result" in
  success) test "$installer_status" = 0 || rollback_and_exit 125; installer_rc=0 ;;
  exit-code) (( installer_status > 0 )) || rollback_and_exit 125; installer_rc="$installer_status" ;;
  signal|core-dump) installer_rc=$(( 128 + installer_status )) ;;
  timeout|watchdog) installer_rc=124 ;;
  *) installer_rc=125 ;;
esac
if (( installer_rc != 0 )); then rollback_and_exit "$installer_rc"; fi
if ! installer_unit_is_quiesced validation; then rollback_and_exit 125; fi
run_with_optional_validation_timeout 2 1 systemctl stop "$INSTALLER_UNIT"
INSTALLER_UNIT_STARTED=0
if ! run_with_optional_validation_timeout 2 1 systemctl is-active --quiet houfeng-agent; then
  rollback_and_exit 126
fi
validation_deadline_has_time
test ! -L "$PRIVATE_INSTALL_LOG"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$PRIVATE_INSTALL_LOG"

run_with_optional_validation_timeout 5 1 sync /usr/local/bin/houfeng-agent /etc/houfeng-agent/agent.env \
  /etc/houfeng-agent/token /etc/systemd/system/houfeng-agent.service
run_with_optional_validation_timeout 2 1 sync -f /usr/local/bin
run_with_optional_validation_timeout 2 1 sync -f /etc/houfeng-agent
run_with_optional_validation_timeout 2 1 sync -f /etc/systemd/system
for live_file in /usr/local/bin/houfeng-agent /etc/houfeng-agent/agent.env /etc/houfeng-agent/token /etc/systemd/system/houfeng-agent.service; do
  run_with_optional_validation_timeout 2 1 /usr/bin/test -f "$live_file"
  run_with_optional_validation_timeout 2 1 /usr/bin/test ! -L "$live_file"
done
fixed_binary_sha="$(run_with_optional_validation_timeout 2 1 sha256sum /usr/local/bin/houfeng-agent | awk '{print $1}')" || rollback_and_exit 125
test "$fixed_binary_sha" = "$EXPECTED_FIXED_BINARY_SHA256"
fixed_unit_sha="$(run_with_optional_validation_timeout 2 1 sha256sum /etc/systemd/system/houfeng-agent.service | awk '{print $1}')" || rollback_and_exit 125
test "$fixed_unit_sha" = "$EXPECTED_FIXED_UNIT_SHA256"
expect_command_output 'root:root 755' run_with_optional_validation_timeout 2 1 stat -c '%U:%G %a' /usr/local/bin/houfeng-agent
expect_command_output 'root:houfeng-agent 640' run_with_optional_validation_timeout 2 1 stat -c '%U:%G %a' /etc/houfeng-agent/agent.env
expect_command_output 'houfeng-agent:houfeng-agent 600' run_with_optional_validation_timeout 2 1 stat -c '%U:%G %a' /etc/houfeng-agent/token
expect_command_output 'root:root 644' run_with_optional_validation_timeout 2 1 stat -c '%U:%G %a' /etc/systemd/system/houfeng-agent.service
expect_command_output enabled run_with_optional_validation_timeout 2 1 systemctl show houfeng-agent.service -p UnitFileState --value
expect_command_output active run_with_optional_validation_timeout 2 1 systemctl show houfeng-agent.service -p ActiveState --value
expect_command_output /etc/systemd/system/houfeng-agent.service run_with_optional_validation_timeout 2 1 systemctl show houfeng-agent.service -p FragmentPath --value
fixed_drop_in_paths="$(run_with_optional_validation_timeout 2 1 systemctl show houfeng-agent.service -p DropInPaths --value)" || rollback_and_exit 125
test -z "$fixed_drop_in_paths"
backup_dir="$(dirname -- "$BACKUP_MARKER")"
run_with_optional_validation_timeout 2 1 /usr/bin/test -f "$backup_dir/token"
run_with_optional_validation_timeout 2 1 /usr/bin/test ! -L "$backup_dir/token"
run_with_optional_validation_timeout 2 1 cmp -s -- /etc/houfeng-agent/token "$backup_dir/token"
run_with_optional_validation_timeout 12 2 "$FIXED_LOCAL_VERIFIER" \
  "$EXPECTED_FIXED_BINARY_SHA256" "$EXPECTED_FIXED_UNIT_SHA256" \
  "$EXPECTED_FIXED_REVISION" "$EXPECTED_HOSTNAME" "$EXPECTED_MACHINE_ID_SHA256" \
  "$EXPECTED_ARCH" "$EXPECTED_MONITORING_INSTANCE_ID" "$BACKUP_MARKER" \
  "$INSTALLER_ISSUANCE_RECEIPT" "$FIRST_LIVE_RECEIPT_FILE" "$FIXED_LOCAL_RECEIPT_FILE" \
  >>"$PRIVATE_INSTALL_LOG" 2>&1
run_with_optional_validation_timeout 2 1 /usr/bin/test -f "$FIRST_LIVE_RECEIPT_FILE"
run_with_optional_validation_timeout 2 1 /usr/bin/test ! -L "$FIRST_LIVE_RECEIPT_FILE"
expect_command_output 'root:root 600' run_with_optional_validation_timeout 2 1 stat -c '%U:%G %a' "$FIRST_LIVE_RECEIPT_FILE"
FIRST_LIVE_RECEIPT_SHA256="$(run_with_optional_validation_timeout 2 1 sha256sum "$FIRST_LIVE_RECEIPT_FILE" | awk '{print $1}')" || rollback_and_exit 125
[[ "$FIRST_LIVE_RECEIPT_SHA256" =~ ^[0-9a-f]{64}$ ]]
run_with_optional_validation_timeout 2 1 /usr/bin/test ! -L "$FIXED_LOCAL_RECEIPT_FILE"
run_with_optional_validation_timeout 2 1 /usr/bin/test -f "$FIXED_LOCAL_RECEIPT_FILE"
expect_command_output 'root:root 600' run_with_optional_validation_timeout 2 1 stat -c '%U:%G %a' "$FIXED_LOCAL_RECEIPT_FILE"
fixed_local_receipt_lines="$(run_with_optional_validation_timeout 2 1 wc -l "$FIXED_LOCAL_RECEIPT_FILE" | tr -d ' ')" || rollback_and_exit 125
test "$fixed_local_receipt_lines" = 1
FIXED_LOCAL_RECEIPT_SHA256="$(run_with_optional_validation_timeout 2 1 /usr/bin/head -n 1 -- "$FIXED_LOCAL_RECEIPT_FILE")" || rollback_and_exit 125
[[ "$FIXED_LOCAL_RECEIPT_SHA256" =~ ^[0-9a-f]{64}$ ]]
run_with_optional_validation_timeout 2 1 sync "$FIRST_LIVE_RECEIPT_FILE" "$FIXED_LOCAL_RECEIPT_FILE"
run_with_optional_validation_timeout 2 1 sync -f "$ROLLOUT_DIR"
validation_deadline_has_time
outage_deadline_has_time
SUPERVISOR_ARMED=0
run_with_outage_timeout 2 1 unlink -- "$SECRET_INSTALLER_FILE"
run_with_outage_timeout 2 1 sync -f "$ROLLOUT_DIR"
outage_deadline_has_time
SECRET_CLEANUP_ARMED=0
exit 0
```

The generated supervisor freezes one exact backup marker+execution-receipt pair, rollback-script path+digest, installer command digest+issuance-data-receipt pair, existing minisign path+digest+provenance, canonical live-parent metadata and transient-unit identity. Under the still-held host lock it rehashes both issuance inputs and reruns their SHA-frozen parser immediately before it starts the unconditional 15/85-second clock and attempts launch. `--verify-only`, restore and `--verify-local-restore` pass the same marker identity plus one canonical root-only local-receipt path. The rollback script independently requires its canonical path/digest, bundle/state/effective/path metadata and stable-ID receipt to match the marker; cross-pairings fail before mutation. The secret-cleanup flag is armed only after all read-only preflight checks pass, so an existing private-log file or live/dangling log symlink exits without truncation, rollback or cleanup. Installer exit plus `active` is not success: the isolated clone first proves the actual signed download/install path can complete, then exact loaded transient-unit/ControlGroup/empty-member properties, fixed bytes/modes/unit/token semantics and the SHA-frozen fixed-local/first-live verifier all run before the 15-second validation cutoff. Before dispatch acceptance is established, exact `LoadState=not-found` plus absence of the expected cgroup path proves that no installer unit exists; a pre-launch cancellation or explicitly failed dispatch leaves the old Agent persisted-enabled and active and never invokes its disable/stop path. Once `systemd-run` returns zero or a loaded unit/cgroup is observed, the acceptance latch is permanent; later unit GC/`not-found`, empty or drifted ControlGroup, and missing/unreadable/nonempty `cgroup.procs` are treated as unproven containment and require host isolation. A rollback that locally restores exact old enabled+active state but later lacks the Center receipt returns nonzero; the supervisor verifies the durable local receipt, leaves healthy old running and blocks promotion. Failure injection covers expected-output-then-nonzero/timeouts for every identity, state and hash producer, hostile `PATH`/`BASH_ENV`/manager environment, concurrent lock/updater rejection, unknown or replaced minisign, installer URL/repository/flag/issuance mismatch, replacement or expiry, backup marker signal cutpoints, `sudo use_pty`, transient-unit properties plus record-only INT/TERM at every cutpoint before dispatch, during `systemd-run`, and between return and state publication (proving rc=0 permanently latches acceptance before pending-signal consumption, while an explicitly failed dispatch with exact not-found/no-cgroup leaves old Agent enabled+active with zero disable/stop calls), deterministic record-window signal precedence (any INT selects 130, TERM-only selects 143, and pending signal overrides later ERR), during-dispatch first loaded/cgroup observation followed by termination and GC/`not-found` with acceptance still latched and isolation required, ControlGroup and cgroup-v2 membership/residual processes, unit result/status, repeated recovery signals, real-install 15/85-second clocks, aggregate-per-device capacity, disabled readback, every durable-local-receipt/phase cutpoint, rollback/verify-local/cleanup budget, local mixed state, receipt-only failure and marker durability. The Center three-batch receipt and additional queue soak remain mandatory later gates.

The final pre-dispatch closure reruns `--verify-only` to prove that the live old binary, unit, env and token bytes still match the frozen rollback bundle, that their owner/mode and effective systemd properties still match the v0.79.4 snapshot, and that the service remains persisted-enabled and active. Only after that potentially long check does it rerun the SHA-frozen issuance verifier, including freshness/expiry and exact non-secret command semantics, then rehash both installer inputs one final time, check pending signal, freeze the 85/15 clocks and immediately invoke `systemd-run`. Any backup-receipt-to-launch drift, expiry during the live-old check, or installer/receipt replacement fails before dispatch and leaves the old Agent untouched. Failure injection covers each live-old drift, receipt expiry during `--verify-only`, command/receipt replacement during the full live-old check, and the complete final-gate-to-dispatch sequence; the 15-second success clock begins only after these read-only gates pass.

### Canary ordering

1. Do not touch either Agent until the Center is healthy at the released fixed patch and migration/runtime admission evidence is complete. Direct v0.79.5 deployment remains blocked; see `center-compose-upgrade-rollback.md`.
2. Record both MonitoringInstance IDs, display names, last live batch times, current `agent_version`, service status and installed binary checksum without reading token/queue contents. Independently resolve the live `FragmentPath`, drop-ins, `ExecStart`, `EnvironmentFile`, user/group, `StateDirectory`, `ReadWritePaths`, token and queue paths plus owner/mode; compare the sanitized effective unit/env semantics with the canonical v0.79.4 contract. Any live drift blocks the generic paths and requires a host-specific reviewed backup/installer/rollback plus nonempty-queue rehearsal even when the release source diff is empty.
3. Create the rollback bundle on Agent A.
4. From the fixed Center UI, generate Agent A's fresh one-command installer. Confirm its version is the exact fixed release, with the expected release repo and `--install-missing-deps`; run it without copying the secret command into logs or chat.
5. Require installer exit 0, active service, no restart loop, correct installed binary SHA from the fixed release's signed public manifest, and at least three distinct accepted non-backfilled live `sync_batch_id` values carrying the exact fixed `agent_version` with normal cadence. Three live batches are both a liveness soak and the policy's minimum stable-recovery evidence.
6. Observe one additional bounded interval for queue growth/retries and current health. Stop on any authentication, exact-duplicate-only, backfill-only, oversized batch, clock-gap, or notification anomaly.
7. Only then repeat the backup/install/evidence sequence on Agent B.
8. Keep both rollback bundles private until the acceptance window closes.

The canary is promotable only after the sanitized outer root wrapper, still holding the canonical host-wide lock, has observed supervisor exit `0` and atomically published the exact `SUPERVISOR_EXECUTION_RECEIPT`, then a private orchestrator atomically publishes root-owned `0600` `AGENT_INSTALL_COMPLETE`. The execution receipt binds host/machine, lock identity, canonical supervisor-script path+SHA, backup marker+execution-receipt digests, installer-command SHA, issuance-data-receipt SHA, minisign provenance receipt, first-live receipt SHA, fixed-local receipt SHA and `exit_status=0`. The final marker binds that execution-receipt path+digest plus hostname, machine-id, architecture, exact fixed release source SHA, architecture-specific binary/unit SHAs, Center three-live-batch receipt and quiet-window receipt. All payloads and temporary markers are synchronized before atomic rename and directory sync; any error/signal removes the incomplete artifact and syncs the directory. Before Agent B, re-read both exact paths+digests, validate every field, and re-prove current service/binary/Center health. A marker, local receipt, active service or Center rows alone never substitutes for the complete chain.

### Center-side read-only Agent evidence

From the owner-confirmed Center deployment directory, use each validated MonitoringInstance ID separately:

```bash
set -euo pipefail
MI_ID=mi_replace_with_validated_id
SINCE_UTC=replace_with_pre_install_utc
DEPLOY_DIR=/absolute/owner-confirmed/fleet-deployment
DOCKER_HOST=unix:///run/docker.sock
DOCKER_CONFIG=/root/houfeng-rollout/docker-empty-config
timeout --signal=TERM --kill-after=1s 5s env -i \
  PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
  HOME=/root USER=root LOGNAME=root COMPOSE_DISABLE_ENV_FILE=1 \
  DOCKER_HOST="$DOCKER_HOST" DOCKER_CONFIG="$DOCKER_CONFIG" \
  docker compose --env-file "$DEPLOY_DIR/.env" \
  -f "$DEPLOY_DIR/compose.yaml" -f "$DEPLOY_DIR/compose.proxy-host.yaml" \
  exec -T -e PGOPTIONS='-c statement_timeout=2000ms -c default_transaction_read_only=on' db \
  psql -X -v ON_ERROR_STOP=1 -v mi_id="$MI_ID" -v since_utc="$SINCE_UTC" -U postgres -d houfeng -P pager=off -c \
  "with recent_live as materialized (select sync_batch_id, received_at, id, agent_version from monitoring_instance_heartbeats where monitoring_instance_id=:'mi_id' and received_at>=:'since_utc'::timestamptz and is_backfilled=false order by received_at desc, id desc limit 768), ranked as (select sync_batch_id, received_at, id, agent_version, row_number() over (partition by sync_batch_id order by received_at desc, id desc) as batch_rank from recent_live) select sync_batch_id, received_at, agent_version from ranked where batch_rank=1 order by received_at desc, id desc limit 3"
```

All three rows must have distinct nonempty batch IDs, `agent_version=<fixed-release>`, and gaps no greater than twice the configured heartbeat interval. The candidate CTE intentionally does not filter by target version: the actual latest three live batches must all prove the target instead of allowing SQL to skip newer wrong-version rows. Before rollout, execute the production reader SQL with its real `$1/$2/$3` arguments against large history under strict PostgreSQL 16 using `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`; require the exact 0063 ordered partial index through Index/Index Only Scan, reject Seq/Bitmap paths, and bound scan rows/loops/shared blocks before `WindowAgg`. Also inspect `systemctl status houfeng-agent` and a narrow private `journalctl -u houfeng-agent --since ...`; do not paste raw operational logs into shared evidence.

### Manual rollback of one Agent

Rollback only the failed canary; leave the other host untouched. Stop the service, restore the complete bundle, reload the unit, and start it:

The reviewed rollback script must stage and validate every old file before stopping the failed target. If any rename or post-restore check fails, its trap keeps the service stopped; it must never start a mixed binary/env/token/unit set:

```bash
#!/bin/bash
set -Eeuo pipefail
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
unset BASH_ENV ENV CDPATH
umask 077

EXPECTED_HOSTNAME=REPLACE_WITH_REVALIDATED_REMOTE_HOSTNAME
EXPECTED_MACHINE_ID_SHA256=REPLACE_WITH_REVALIDATED_MACHINE_ID_SHA256
EXPECTED_ARCH=REPLACE_WITH_AARCH64_OR_X86_64
HOST_LOCK_PATH=/run/lock/houfeng-rollout.lock
EXPECTED_HOST_LOCK_IDENTITY=REPLACE_WITH_LOCK_PATH_DEVICE_INODE_OWNER_MODE
: "${HOUFENG_ROLLOUT_LOCK_FD:?wrapper must pass the held rollout lock fd}"
BACKUP_PARENT=/root/houfeng-agent-backups
BACKUP_NAME=houfeng-agent-pre-fixed-patch-YYYYMMDDTHHMMSSZ
BACKUP_DIR="$BACKUP_PARENT/$BACKUP_NAME"
EXPECTED_OLD_UNIT_SHA256=dd8eb92954d4e1dc9ddaf472eaa8864c9425ff08aa0272961b098f7727052954
STATE_METADATA_VERIFIER=/root/houfeng-agent-rollout/REPLACE.state-metadata-verifier.sh
EXPECTED_STATE_METADATA_VERIFIER_SHA256=REPLACE_WITH_STATE_METADATA_VERIFIER_SHA256
MODE="${1:-restore}"
EXPECTED_BACKUP_MARKER="${2:-}"
EXPECTED_BACKUP_MARKER_SHA256="${3:-}"
LOCAL_RESTORE_RECEIPT="${4:-}"
ROLLOUT_DIR=/root/houfeng-agent-rollout
FINAL_MARKER="$BACKUP_DIR/AGENT_ROLLBACK_COMPLETE"
FINAL_MARKER_TMP="$BACKUP_DIR/.agent-rollback.complete.tmp"
ROLLBACK_PHASE=0
RESTORE_CLEANUP_ARMED=0
ROOT_BASHPID=$BASHPID
suffix="rollback.$$"
binary_stage="/usr/local/bin/.houfeng-agent.$suffix"
env_stage="/etc/houfeng-agent/.agent.env.$suffix"
token_stage="/etc/houfeng-agent/.token.$suffix"
unit_stage="/etc/systemd/system/.houfeng-agent.service.$suffix"
local_receipt_tmp="$ROLLOUT_DIR/.rollback-local-receipt.$suffix"

bounded_sha256_file() {
  timeout --signal=KILL 3s sha256sum "$1" | awk '{print $1}'
}

expect_command_output() {
  local expected="$1"
  local actual
  shift
  actual="$("$@")" || return "$?"
  test "$actual" = "$expected"
}

leave_stopped_on_failure() {
  trap '' INT TERM
  trap - ERR
  local original_rc="$1"
  local cleanup_rc=0
  local disable_rc=0
  local sync_rc=0
  local stop_rc=0
  local state_rc=0
  local unit_state_rc=0
  local active_state=''
  local unit_file_state=''
  if (( BASHPID != ROOT_BASHPID )); then return "$original_rc"; fi
  if test "$MODE" != restore; then exit "$original_rc"; fi
  set +e
  if (( ROLLBACK_PHASE == 1 )) && test -f "$LOCAL_RESTORE_RECEIPT" && \
    test ! -L "$LOCAL_RESTORE_RECEIPT"; then
    if timeout --signal=KILL 2s sync -f "$ROLLOUT_DIR" && \
      timeout --signal=TERM --kill-after=2s 8s /bin/bash --noprofile --norc \
        "$rollback_script_path" --verify-local-restore "$EXPECTED_BACKUP_MARKER" \
        "$EXPECTED_BACKUP_MARKER_SHA256" "$LOCAL_RESTORE_RECEIPT"; then
      ROLLBACK_PHASE=2
    fi
  fi
  if (( ROLLBACK_PHASE == 1 )); then
    timeout --signal=KILL 2s systemctl disable houfeng-agent
    disable_rc=$?
    timeout --signal=KILL 2s sync -f /etc/systemd/system
    sync_rc=$?
    unit_file_state="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p UnitFileState --value)"
    unit_state_rc=$?
    timeout --signal=KILL 2s systemctl stop houfeng-agent
    stop_rc=$?
    active_state="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p ActiveState --value)"
    state_rc=$?
    if (( state_rc != 0 )) || { test "$active_state" != inactive && test "$active_state" != failed; }; then
      timeout --signal=KILL 2s systemctl kill --kill-whom=all --signal=KILL houfeng-agent
      timeout --signal=KILL 2s systemctl stop houfeng-agent
      stop_rc=$?
      active_state="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p ActiveState --value)"
      state_rc=$?
    fi
    if (( disable_rc == 0 && sync_rc == 0 && unit_state_rc == 0 && stop_rc == 0 && state_rc == 0 )) && \
      test "$unit_file_state" = disabled && \
      { test "$active_state" = inactive || test "$active_state" = failed; }; then
      printf 'rollback incomplete; Agent disabled and intentionally left stopped\n' >&2
    else
      printf 'rollback incomplete and fail-stop could not be proven; emergency host isolation is required\n' >&2
    fi
  fi
  if (( RESTORE_CLEANUP_ARMED == 1 )); then
    for cleanup_path in "$binary_stage" "$env_stage" "$token_stage" "$unit_stage" \
      "$local_receipt_tmp" "$FINAL_MARKER_TMP" "$FINAL_MARKER"; do
      if test -e "$cleanup_path" || test -L "$cleanup_path"; then
        timeout --signal=KILL 2s unlink -- "$cleanup_path" || cleanup_rc=$?
      fi
    done
    if (( ROLLBACK_PHASE != 2 )) && \
      { test -e "$LOCAL_RESTORE_RECEIPT" || test -L "$LOCAL_RESTORE_RECEIPT"; }; then
      timeout --signal=KILL 2s unlink -- "$LOCAL_RESTORE_RECEIPT" || cleanup_rc=$?
    fi
    timeout --signal=KILL 2s sync -f "$ROLLOUT_DIR" || cleanup_rc=$?
    timeout --signal=KILL 2s sync -f "$BACKUP_DIR" || cleanup_rc=$?
  fi
  if (( cleanup_rc != 0 )); then
    printf 'rollback marker/stage cleanup or directory sync could not be proven; preserve evidence, forbid promotion and isolate the host evidence\n' >&2
  fi
  exit "$original_rc"
}
trap 'leave_stopped_on_failure "$?"' ERR
trap 'leave_stopped_on_failure 130' INT
trap 'leave_stopped_on_failure 143' TERM

(( EUID == 0 ))
command -v timeout findmnt flock >/dev/null
case "$MODE" in --verify-only|--verify-local-restore|restore) ;; *) exit 64 ;; esac
case "$EXPECTED_HOSTNAME:$EXPECTED_MACHINE_ID_SHA256:$EXPECTED_ARCH:$EXPECTED_HOST_LOCK_IDENTITY:$STATE_METADATA_VERIFIER:$EXPECTED_STATE_METADATA_VERIFIER_SHA256" in *REPLACE*) exit 64 ;; esac
[[ "$EXPECTED_STATE_METADATA_VERIFIER_SHA256" =~ ^[0-9a-f]{64}$ ]] || exit 64
test "$EXPECTED_BACKUP_MARKER" = "$BACKUP_DIR/AGENT_BACKUP_COMPLETE"
[[ "$EXPECTED_BACKUP_MARKER_SHA256" =~ ^[0-9a-f]{64}$ ]] || exit 64
expect_command_output "$ROLLOUT_DIR" dirname -- "$LOCAL_RESTORE_RECEIPT"
expect_command_output "$LOCAL_RESTORE_RECEIPT" readlink -m -- "$LOCAL_RESTORE_RECEIPT"
expect_command_output "$local_receipt_tmp" readlink -m -- "$local_receipt_tmp"
case "$BACKUP_NAME" in houfeng-agent-pre-fixed-patch-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]T[0-9][0-9][0-9][0-9][0-9][0-9]Z) ;; *) exit 64 ;; esac
expect_command_output "$EXPECTED_HOSTNAME" hostname -s
expect_command_output "$EXPECTED_MACHINE_ID_SHA256" bounded_sha256_file /etc/machine-id
expect_command_output "$EXPECTED_ARCH" uname -m
case "$EXPECTED_ARCH" in
  aarch64) EXPECTED_OLD_BINARY_SHA256=450a25c705f54371e8f44f649f63df244314ecf6d75809ce82cddc7306d6ea67 ;;
  x86_64) EXPECTED_OLD_BINARY_SHA256=e608b8c8efe020d77783943e996b8cb47facfc19363588f4a4c5fe833537eef7 ;;
  *) exit 64 ;;
esac
[[ "$HOUFENG_ROLLOUT_LOCK_FD" =~ ^[0-9]+$ ]]
test -e "/proc/$BASHPID/fd/$HOUFENG_ROLLOUT_LOCK_FD"
expect_command_output "$HOST_LOCK_PATH" readlink -f -- "/proc/$BASHPID/fd/$HOUFENG_ROLLOUT_LOCK_FD"
test -f "$HOST_LOCK_PATH"
test ! -L "$HOST_LOCK_PATH"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$HOST_LOCK_PATH"
host_lock_identity="$(stat -c '%n|%d|%i|%U:%G|%a' "$HOST_LOCK_PATH")"
test "$host_lock_identity" = "$EXPECTED_HOST_LOCK_IDENTITY"
flock -n "$HOUFENG_ROLLOUT_LOCK_FD"
test ! -L "$BACKUP_PARENT"
test ! -L "$BACKUP_DIR"
test ! -L "$ROLLOUT_DIR"
expect_command_output /root/houfeng-agent-backups readlink -f -- "$BACKUP_PARENT"
expect_command_output "/root/houfeng-agent-backups/$BACKUP_NAME" readlink -f -- "$BACKUP_DIR"
expect_command_output "$ROLLOUT_DIR" readlink -f -- "$ROLLOUT_DIR"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$BACKUP_PARENT"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$BACKUP_DIR"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$ROLLOUT_DIR"
test ! -L "$STATE_METADATA_VERIFIER"
test -f "$STATE_METADATA_VERIFIER"
test -x "$STATE_METADATA_VERIFIER"
expect_command_output "$STATE_METADATA_VERIFIER" readlink -f -- "$STATE_METADATA_VERIFIER"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$STATE_METADATA_VERIFIER"
expect_command_output "$EXPECTED_STATE_METADATA_VERIFIER_SHA256" bounded_sha256_file "$STATE_METADATA_VERIFIER"
test ! -L "$EXPECTED_BACKUP_MARKER"
test -f "$EXPECTED_BACKUP_MARKER"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$EXPECTED_BACKUP_MARKER"
expect_command_output "$EXPECTED_BACKUP_MARKER_SHA256" bounded_sha256_file "$EXPECTED_BACKUP_MARKER"
rollback_script_path="$(readlink -f -- "$0")"
test -n "$rollback_script_path"
test ! -L "$rollback_script_path"
test -f "$rollback_script_path"
expect_command_output 'root:root 700' stat -c '%U:%G %a' "$rollback_script_path"
rollback_script_sha="$(bounded_sha256_file "$rollback_script_path")"
test -d "$BACKUP_DIR/state"
test ! -L "$BACKUP_DIR/state"
mount_targets="$(findmnt -Rrn -o TARGET --target "$BACKUP_DIR/state")" || exit 65
while IFS= read -r mount_target; do
  case "$mount_target" in "$BACKUP_DIR/state"|"$BACKUP_DIR/state"/*) exit 65 ;; esac
done <<<"$mount_targets"
for bundle_file in houfeng-agent agent.env token houfeng-agent.service BUNDLE_METADATA EFFECTIVE_UNIT_METADATA LIVE_PATH_METADATA STATE_METADATA SHA256SUMS; do
  test -f "$BACKUP_DIR/$bundle_file"
  test ! -L "$BACKUP_DIR/$bundle_file"
done
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_DIR/BUNDLE_METADATA"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_DIR/EFFECTIVE_UNIT_METADATA"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$BACKUP_DIR/SHA256SUMS"
(
  cd -- "$BACKUP_DIR"
  sha256sum -c SHA256SUMS
)
sha256sums_sha="$(bounded_sha256_file "$BACKUP_DIR/SHA256SUMS")"
backup_env_sha="$(bounded_sha256_file "$BACKUP_DIR/agent.env")"
backup_token_sha="$(bounded_sha256_file "$BACKUP_DIR/token")"
effective_unit_sha="$(bounded_sha256_file "$BACKUP_DIR/EFFECTIVE_UNIT_METADATA")"
live_path_metadata_sha="$(bounded_sha256_file "$BACKUP_DIR/LIVE_PATH_METADATA")"
state_metadata_sha="$(bounded_sha256_file "$BACKUP_DIR/STATE_METADATA")"
for required_file in houfeng-agent agent.env token houfeng-agent.service BUNDLE_METADATA EFFECTIVE_UNIT_METADATA LIVE_PATH_METADATA STATE_METADATA; do
  grep -Eq "^[0-9a-f]{64}  ${required_file}$" "$BACKUP_DIR/SHA256SUMS"
done
grep -Fxq "host=$EXPECTED_HOSTNAME" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "machine_id_sha256=$EXPECTED_MACHINE_ID_SHA256" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "arch=$EXPECTED_ARCH" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "bundle_dir=$BACKUP_DIR" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "rollback_script_path=$rollback_script_path" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "rollback_script_sha256=$rollback_script_sha" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "state_metadata_verifier_path=$STATE_METADATA_VERIFIER" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "state_metadata_verifier_sha256=$EXPECTED_STATE_METADATA_VERIFIER_SHA256" "$EXPECTED_BACKUP_MARKER"
grep -Fxq 'old_version=v0.79.4' "$EXPECTED_BACKUP_MARKER"
grep -Fxq "old_binary_sha256=$EXPECTED_OLD_BINARY_SHA256" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "old_unit_sha256=$EXPECTED_OLD_UNIT_SHA256" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "sha256sums_sha=$sha256sums_sha" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "env_sha256=$backup_env_sha" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "token_sha256=$backup_token_sha" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "effective_unit_sha256=$effective_unit_sha" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "live_path_metadata_sha256=$live_path_metadata_sha" "$EXPECTED_BACKUP_MARKER"
grep -Fxq "state_metadata_sha256=$state_metadata_sha" "$EXPECTED_BACKUP_MARKER"
grep -Eq '^center_receipt_sha256=[0-9a-f]{64}$' "$EXPECTED_BACKUP_MARKER"
backup_marker_line_count="$(wc -l <"$EXPECTED_BACKUP_MARKER" | tr -d ' ')" || exit 65
test "$backup_marker_line_count" = 18
timeout --signal=TERM --kill-after=2s 10s "$STATE_METADATA_VERIFIER" verify \
  "$BACKUP_DIR/state" "$BACKUP_DIR/STATE_METADATA"
expect_command_output "$EXPECTED_OLD_BINARY_SHA256" bounded_sha256_file "$BACKUP_DIR/houfeng-agent"
expect_command_output "$EXPECTED_OLD_UNIT_SHA256" bounded_sha256_file "$BACKUP_DIR/houfeng-agent.service"
expect_command_output 'root:root 755' stat -c '%U:%G %a' "$BACKUP_DIR/houfeng-agent"
expect_command_output 'root:houfeng-agent 640' stat -c '%U:%G %a' "$BACKUP_DIR/agent.env"
expect_command_output 'houfeng-agent:houfeng-agent 600' stat -c '%U:%G %a' "$BACKUP_DIR/token"
expect_command_output 'root:root 644' stat -c '%U:%G %a' "$BACKUP_DIR/houfeng-agent.service"
expect_command_output 4 grep -Ec '^(binary=root:root 755|env=root:houfeng-agent 640|token=houfeng-agent:houfeng-agent 600|unit=root:root 644)$' "$BACKUP_DIR/BUNDLE_METADATA"
bundle_metadata_line_count="$(wc -l <"$BACKUP_DIR/BUNDLE_METADATA" | tr -d ' ')" || exit 65
test "$bundle_metadata_line_count" = 4
effective_metadata_line_count="$(wc -l <"$BACKUP_DIR/EFFECTIVE_UNIT_METADATA" | tr -d ' ')" || exit 65
test "$effective_metadata_line_count" = 8
for property in FragmentPath DropInPaths ExecStart EnvironmentFiles User Group StateDirectory ReadWritePaths; do
  property_count="$(grep -c "^${property}=" "$BACKUP_DIR/EFFECTIVE_UNIT_METADATA")" || exit 65
  test "$property_count" = 1
done
for canonical_parent in /usr/local/bin /etc/houfeng-agent /etc/systemd/system /var/lib/houfeng-agent; do
  test -d "$canonical_parent"
  test ! -L "$canonical_parent"
  expect_command_output "$canonical_parent" readlink -f -- "$canonical_parent"
  canonical_parent_metadata="$(stat -c '%n|%d|%U:%G|%a' "$canonical_parent")" || exit 65
  grep -Fxq "parent=$canonical_parent_metadata" "$BACKUP_DIR/LIVE_PATH_METADATA"
done
for live_file in /usr/local/bin/houfeng-agent /etc/houfeng-agent/agent.env /etc/houfeng-agent/token /etc/systemd/system/houfeng-agent.service; do
  expect_command_output "$live_file" readlink -m -- "$live_file"
  grep -Fxq "file=$live_file" "$BACKUP_DIR/LIVE_PATH_METADATA"
done
for stage in "$binary_stage" "$env_stage" "$token_stage" "$unit_stage"; do
  expect_command_output "$stage" readlink -m -- "$stage"
done
declare -A stage_bytes_by_device=()
declare -A free_bytes_by_device=()
for bundle_and_parent in \
  "$BACKUP_DIR/houfeng-agent|/usr/local/bin" \
  "$BACKUP_DIR/agent.env|/etc/houfeng-agent" \
  "$BACKUP_DIR/token|/etc/houfeng-agent" \
  "$BACKUP_DIR/houfeng-agent.service|/etc/systemd/system"; do
  bundle_file="${bundle_and_parent%%|*}"
  target_parent="${bundle_and_parent#*|}"
  stage_bytes="$(stat -c '%s' "$bundle_file")"
  target_device="$(stat -c '%d' "$target_parent")"
  free_bytes="$(df --output=avail -B1 "$target_parent" | tail -n 1 | tr -d ' ')"
  stage_bytes_by_device[$target_device]=$(( ${stage_bytes_by_device[$target_device]:-0} + stage_bytes ))
  if test -z "${free_bytes_by_device[$target_device]+set}" || \
    (( free_bytes < free_bytes_by_device[$target_device] )); then
    free_bytes_by_device[$target_device]="$free_bytes"
  fi
done
for target_device in "${!stage_bytes_by_device[@]}"; do
  required_stage_bytes=$(( stage_bytes_by_device[$target_device] * 2 + 16777216 ))
  (( free_bytes_by_device[$target_device] >= required_stage_bytes ))
done

verify_exact_old_live() {
  local active_state
  local drop_in_paths
  local property
  local property_value
  for live_file in /usr/local/bin/houfeng-agent /etc/houfeng-agent/agent.env /etc/houfeng-agent/token /etc/systemd/system/houfeng-agent.service; do
    test -f "$live_file" || return 1
    test ! -L "$live_file" || return 1
  done
  cmp -s -- /usr/local/bin/houfeng-agent "$BACKUP_DIR/houfeng-agent" || return 1
  cmp -s -- /etc/houfeng-agent/agent.env "$BACKUP_DIR/agent.env" || return 1
  cmp -s -- /etc/houfeng-agent/token "$BACKUP_DIR/token" || return 1
  cmp -s -- /etc/systemd/system/houfeng-agent.service "$BACKUP_DIR/houfeng-agent.service" || return 1
  expect_command_output "$EXPECTED_OLD_BINARY_SHA256" bounded_sha256_file /usr/local/bin/houfeng-agent || return 1
  expect_command_output "$EXPECTED_OLD_UNIT_SHA256" bounded_sha256_file /etc/systemd/system/houfeng-agent.service || return 1
  expect_command_output 'root:root 755' stat -c '%U:%G %a' /usr/local/bin/houfeng-agent || return 1
  expect_command_output 'root:houfeng-agent 640' stat -c '%U:%G %a' /etc/houfeng-agent/agent.env || return 1
  expect_command_output 'houfeng-agent:houfeng-agent 600' stat -c '%U:%G %a' /etc/houfeng-agent/token || return 1
  expect_command_output 'root:root 644' stat -c '%U:%G %a' /etc/systemd/system/houfeng-agent.service || return 1
  expect_command_output enabled timeout --signal=KILL 2s systemctl show houfeng-agent.service -p UnitFileState --value || return 1
  active_state="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p ActiveState --value)" || return 1
  test "$active_state" = active || return 1
  expect_command_output /etc/systemd/system/houfeng-agent.service timeout --signal=KILL 2s systemctl show houfeng-agent.service -p FragmentPath --value || return 1
  drop_in_paths="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p DropInPaths --value)" || return 1
  test -z "$drop_in_paths" || return 1
  for property in FragmentPath DropInPaths ExecStart EnvironmentFiles User Group StateDirectory ReadWritePaths; do
    property_value="$(timeout --signal=KILL 2s systemctl show houfeng-agent.service -p "$property" --value)" || return 1
    grep -Fxq "$property=$property_value" "$BACKUP_DIR/EFFECTIVE_UNIT_METADATA" || return 1
  done
  test ! -L /var/lib/houfeng-agent || return 1
  expect_command_output /var/lib/houfeng-agent readlink -f -- /var/lib/houfeng-agent || return 1
}

verify_local_restore_receipt() {
  test -f "$LOCAL_RESTORE_RECEIPT" || return 1
  test ! -L "$LOCAL_RESTORE_RECEIPT" || return 1
  expect_command_output 'root:root 600' stat -c '%U:%G %a' "$LOCAL_RESTORE_RECEIPT" || return 1
  local receipt_line_count
  receipt_line_count="$(wc -l <"$LOCAL_RESTORE_RECEIPT" | tr -d ' ')" || return 1
  test "$receipt_line_count" = 15 || return 1
  grep -Fxq "host=$EXPECTED_HOSTNAME" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "machine_id_sha256=$EXPECTED_MACHINE_ID_SHA256" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "arch=$EXPECTED_ARCH" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "lock_identity=$EXPECTED_HOST_LOCK_IDENTITY" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "backup_marker_path=$EXPECTED_BACKUP_MARKER" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "backup_marker_sha256=$EXPECTED_BACKUP_MARKER_SHA256" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "rollback_script_sha256=$rollback_script_sha" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq 'restored_version=v0.79.4' "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "old_binary_sha256=$EXPECTED_OLD_BINARY_SHA256" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "old_unit_sha256=$EXPECTED_OLD_UNIT_SHA256" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "env_sha256=$backup_env_sha" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "token_sha256=$backup_token_sha" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "effective_unit_sha256=$effective_unit_sha" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "live_path_metadata_sha256=$live_path_metadata_sha" "$LOCAL_RESTORE_RECEIPT" || return 1
  grep -Fxq "sha256sums_sha=$sha256sums_sha" "$LOCAL_RESTORE_RECEIPT" || return 1
  verify_exact_old_live || return 1
}

if test "$MODE" = --verify-local-restore; then
  verify_local_restore_receipt
  exit 0
fi
test ! -e "$LOCAL_RESTORE_RECEIPT"
test ! -L "$LOCAL_RESTORE_RECEIPT"
test ! -e "$FINAL_MARKER"
test ! -L "$FINAL_MARKER"
test ! -e "$FINAL_MARKER_TMP"
test ! -L "$FINAL_MARKER_TMP"
test ! -e "$local_receipt_tmp"
test ! -L "$local_receipt_tmp"
if test "$MODE" = --verify-only; then
  verify_exact_old_live
  exit 0
fi
for stage in "$binary_stage" "$env_stage" "$token_stage" "$unit_stage"; do
  test ! -e "$stage"
  test ! -L "$stage"
done
RESTORE_CLEANUP_ARMED=1
cp -a -- "$BACKUP_DIR/houfeng-agent" "$binary_stage"
cp -a -- "$BACKUP_DIR/agent.env" "$env_stage"
cp -a -- "$BACKUP_DIR/token" "$token_stage"
cp -a -- "$BACKUP_DIR/houfeng-agent.service" "$unit_stage"
cmp -s -- "$binary_stage" "$BACKUP_DIR/houfeng-agent"
cmp -s -- "$env_stage" "$BACKUP_DIR/agent.env"
cmp -s -- "$token_stage" "$BACKUP_DIR/token"
cmp -s -- "$unit_stage" "$BACKUP_DIR/houfeng-agent.service"
for stage in "$binary_stage" "$env_stage" "$token_stage" "$unit_stage"; do
  test -f "$stage"
  test ! -L "$stage"
done
expect_command_output 'root:root 755' stat -c '%U:%G %a' "$binary_stage"
expect_command_output 'root:houfeng-agent 640' stat -c '%U:%G %a' "$env_stage"
expect_command_output 'houfeng-agent:houfeng-agent 600' stat -c '%U:%G %a' "$token_stage"
expect_command_output 'root:root 644' stat -c '%U:%G %a' "$unit_stage"
sync "$binary_stage" "$env_stage" "$token_stage" "$unit_stage"
sync -f /usr/local/bin
sync -f /etc/houfeng-agent
sync -f /etc/systemd/system

ROLLBACK_PHASE=1
BINARY_STAGE="$binary_stage" ENV_STAGE="$env_stage" TOKEN_STAGE="$token_stage" UNIT_STAGE="$unit_stage" \
  timeout --signal=TERM --kill-after=5s 30s /bin/bash --noprofile --norc -c '
    set -euo pipefail
    systemctl disable houfeng-agent
    sync -f /etc/systemd/system
    unit_file_state="$(systemctl show houfeng-agent.service -p UnitFileState --value)"
    test "$unit_file_state" = disabled
    systemctl stop houfeng-agent
    active_state="$(systemctl show houfeng-agent.service -p ActiveState --value)"
    test "$active_state" = inactive || test "$active_state" = failed
    mv -T -- "$BINARY_STAGE" /usr/local/bin/houfeng-agent
    mv -T -- "$ENV_STAGE" /etc/houfeng-agent/agent.env
    mv -T -- "$TOKEN_STAGE" /etc/houfeng-agent/token
    mv -T -- "$UNIT_STAGE" /etc/systemd/system/houfeng-agent.service
    sync /usr/local/bin/houfeng-agent /etc/houfeng-agent/agent.env /etc/houfeng-agent/token /etc/systemd/system/houfeng-agent.service
    sync -f /usr/local/bin
    sync -f /etc/houfeng-agent
    sync -f /etc/systemd/system
    systemctl daemon-reload
    systemctl enable houfeng-agent
    sync -f /etc/systemd/system
    systemctl start houfeng-agent
  '
verify_exact_old_live
printf 'host=%s\nmachine_id_sha256=%s\narch=%s\nlock_identity=%s\nbackup_marker_path=%s\nbackup_marker_sha256=%s\nrollback_script_sha256=%s\nrestored_version=v0.79.4\nold_binary_sha256=%s\nold_unit_sha256=%s\nenv_sha256=%s\ntoken_sha256=%s\neffective_unit_sha256=%s\nlive_path_metadata_sha256=%s\nsha256sums_sha=%s\n' \
  "$EXPECTED_HOSTNAME" "$EXPECTED_MACHINE_ID_SHA256" "$EXPECTED_ARCH" "$EXPECTED_HOST_LOCK_IDENTITY" \
  "$EXPECTED_BACKUP_MARKER" "$EXPECTED_BACKUP_MARKER_SHA256" "$rollback_script_sha" \
  "$EXPECTED_OLD_BINARY_SHA256" "$EXPECTED_OLD_UNIT_SHA256" "$backup_env_sha" \
  "$backup_token_sha" "$effective_unit_sha" "$live_path_metadata_sha" "$sha256sums_sha" >"$local_receipt_tmp"
test ! -L "$local_receipt_tmp"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$local_receipt_tmp"
sync "$local_receipt_tmp"
mv -T -- "$local_receipt_tmp" "$LOCAL_RESTORE_RECEIPT"
sync -f "$ROLLOUT_DIR"
verify_local_restore_receipt
ROLLBACK_PHASE=2

verify_private_center_rollback_receipt() {
  printf 'replace this fail-closed stub with stable-instance v0.79.4 live-batch, token identity and queue-drain evidence\n' >&2
  return 64
}
CENTER_ROLLBACK_RECEIPT_SHA256=''
verify_private_center_rollback_receipt
[[ "$CENTER_ROLLBACK_RECEIPT_SHA256" =~ ^[0-9a-f]{64}$ ]]

local_restore_receipt_sha="$(sha256sum "$LOCAL_RESTORE_RECEIPT" | awk '{print $1}')"
printf 'host=%s\nmachine_id_sha256=%s\narch=%s\nlock_identity=%s\nrestored_version=v0.79.4\nold_binary_sha256=%s\nold_unit_sha256=%s\nbackup_marker_sha256=%s\nsha256sums_sha=%s\nlocal_restore_receipt_sha256=%s\ncenter_receipt_sha256=%s\n' \
  "$EXPECTED_HOSTNAME" "$EXPECTED_MACHINE_ID_SHA256" "$EXPECTED_ARCH" "$EXPECTED_HOST_LOCK_IDENTITY" "$EXPECTED_OLD_BINARY_SHA256" \
  "$EXPECTED_OLD_UNIT_SHA256" "$EXPECTED_BACKUP_MARKER_SHA256" "$sha256sums_sha" \
  "$local_restore_receipt_sha" "$CENTER_ROLLBACK_RECEIPT_SHA256" >"$FINAL_MARKER_TMP"
test ! -L "$FINAL_MARKER_TMP"
expect_command_output 'root:root 600' stat -c '%U:%G %a' "$FINAL_MARKER_TMP"
sync "$FINAL_MARKER_TMP"
sync -f "$BACKUP_DIR"
mv -T -- "$FINAL_MARKER_TMP" "$FINAL_MARKER"
sync -f "$BACKUP_DIR"
exit 0
```

The rollback verifies the complete marker/script/bundle cross-pair, rejects stale/dangling completion or local-receipt paths, and requires the manifest, every core bundle file and every stage to be regular and non-symlinked before arming live mutation. It revalidates canonical live parents, device/owner/mode metadata, stage lexical paths and state mount closure, groups all stages by target `st_dev`, and proves aggregate bytes plus headroom fit on each device before any stage write. Once armed, it runs `systemctl disable`, synchronizes the systemd directory, reads back exact `UnitFileState=disabled`, and only then stops the service and performs the first rename; every staged/final file and systemd directory is synchronized, and enable/start occurs only after the complete old set is in place. Any mutation/local-invariant failure makes a bounded disable/sync/stop/kill attempt, ignores further signals until that proof ends, and never claims fail-stopped success if durable disabled+inactive cannot be proven. If preflight/staging fails while the supervisor is handling an untrusted installer state, the supervisor performs its separate bounded containment proof. After exact local old bytes/modes/effective unit, path-metadata digest plus persisted enabled+active pass, the script atomically publishes and synchronizes a marker-bound local-restore receipt. If ERR/INT/TERM lands before the in-memory phase advances, the root handler synchronizes the receipt directory and invokes this same SHA-bound script's `--verify-local-restore`; a valid durable receipt plus exact live old state advances to `ROLLBACK_PHASE=2` and is not stopped. A later Center receipt timeout/mismatch/signal likewise leaves the healthy old Agent running but publishes no final marker. The supervisor independently revalidates both the receipt and current live state, and preserves the original installer/signal status even when rollback separately fails.

Preserve the current `/var/lib/houfeng-agent` state by default: discarding it could lose facts accepted after the backup. Before any canary, run `CGO_ENABLED=0 GOOS=linux GOARCH=<amd64|arm64> go list -deps -json ./cmd/houfeng-agent` at both v0.79.4 and fixed SHA. Freeze, for all four runs, every repo-local package/directory and actually selected Go/Cgo/embed file, compare each architecture and their union, then add the Center installer-command generator, installer, unit/env template, `go.mod`, `go.sum`, `Makefile` and `.github/workflows/publish-images.yml`. Audit the full range diff plus queue serialization/retry/discard and token argv/stdin/privacy semantics. If any closure, state, installer, env, unit, build or publication semantics changed, rollout remains blocked until an isolated nonempty-v0.79.4-queue upgrade and v0.79.4 downgrade/recovery rehearsal passes; if unchanged, retain the two-SHA/two-architecture closure and diff-empty/source evidence. Restore backed-up state only after a separately reviewed corruption diagnosis and host-specific quarantine plan. After install or rollback, compare unit/env semantically with secrets suppressed, compare token bytes privately, and prove new accepted live batches report the expected version and queue drain recovered.

### Related specs

- `.trellis/spec/backend/logging-guidelines.md` — Agent logs may include only safe stable identity/origin fields; never expose sync/enrollment tokens or payloads.
- `.trellis/spec/backend/database-guidelines.md` — recovery requires three distinct, post-incident, non-backfilled live batches using server `received_at` and bounded gaps.
- `.trellis/spec/backend/quality-guidelines.md` — focused evidence does not replace whole rollout/integration evidence.

## Caveats / Not Found

- Separate read-only production inventory confirmed the two hosts, architectures, official v0.79.4 binary/unit identities and current empty-queue/service state. Private MonitoringInstance IDs and execution-time path/lock/provenance receipts intentionally remain outside shared task artifacts and must be freshly revalidated before script generation.
- No installer dry-run, transaction, previous-binary slot, automatic rollback, or local version CLI exists.
- No explicit fixed-release-to-v0.79.4 Agent queue-format backward-compatibility statement exists yet. The actual fixed-release diff must prove the Agent state contract unchanged; otherwise queue handling becomes a new blocker and needs a separate rehearsal.
- Any exceptional state restoration must be converted into an owner-reviewed, host-specific command sheet with a unique nonexisting quarantine path; no cleanup/delete command belongs in the rollback closure.
- The generated installer preserves credentials based on token-file shape, not a server round trip before replacing files; preflight token/state backup remains required.
