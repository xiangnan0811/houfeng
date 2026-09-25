#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
root=$(cd -- "$script_dir/.." && pwd)
node_bin=/home/murray/.nvm/versions/node/v22.23.1/bin/node
if [[ ! -x "$node_bin" ]]; then
  node_bin=$(command -v node || true)
fi
if [[ -z "$node_bin" || ! -x "$node_bin" ]]; then
  printf 'Node.js 22 is required for local-live acceptance\n' >&2
  exit 2
fi
node_version=$("$node_bin" --version)
if [[ ! "$node_version" =~ ^v22\. ]]; then
  printf 'Node.js 22 is required; found %s\n' "$node_version" >&2
  exit 2
fi
node_dir=${node_bin%/*}
path="$node_dir:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
playwright="$root/web/node_modules/.bin/playwright"
config="$root/web/playwright.local-live.config.ts"
if [[ ! -x "$playwright" ]]; then
  printf 'Playwright is unavailable at %s; this launcher does not install packages\n' "$playwright" >&2
  exit 2
fi

output_root="$root/tmp/vps-state-local-live"
mkdir -p "$output_root"

if [[ -n "${LOCAL_LIVE_CENTER_URL:-}" ]]; then
  center_url=$LOCAL_LIVE_CENTER_URL
  username=${LOCAL_LIVE_USERNAME:-}
  password=${LOCAL_LIVE_PASSWORD:-}
  if [[ -z "$username" || -z "$password" ]]; then
    printf 'LOCAL_LIVE_USERNAME and LOCAL_LIVE_PASSWORD are required with LOCAL_LIVE_CENTER_URL\n' >&2
    exit 2
  fi
  exec env -i \
    PATH="$path" \
    HOME="${HOME:-/tmp}" \
    TMPDIR="${TMPDIR:-/tmp}" \
    LOCAL_LIVE_CENTER_URL="$center_url" \
    LOCAL_LIVE_USERNAME="$username" \
    LOCAL_LIVE_PASSWORD="$password" \
    LOCAL_LIVE_CENTER_OWNER=external \
    "$playwright" test --config "$config" "$@"
fi

# The self-managed mode owns only the center process it starts. Its four
# disposable PostgreSQL databases are provided and cleaned by the official runner.
workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-vps-state-live.XXXXXX")
mkdir -p "$workspace/home"
cleanup_workspace() {
  rm -rf -- "$workspace"
}
trap cleanup_workspace EXIT

npm_bin=$(command -v npm || true)
go_bin=$(command -v go || true)
if [[ -z "$npm_bin" || -z "$go_bin" ]]; then
  printf 'npm and Go are required for the self-managed local-live mode\n' >&2
  exit 2
fi
gomodcache=${GOMODCACHE:-$("$go_bin" env GOMODCACHE)}
gocache=${GOCACHE:-$("$go_bin" env GOCACHE)}
playwright_browsers_path=${PLAYWRIGHT_BROWSERS_PATH:-"${HOME:-/tmp}/.cache/ms-playwright"}
if [[ ! -d "$playwright_browsers_path" ]]; then
  printf 'existing Playwright browser cache not found: %s\n' "$playwright_browsers_path" >&2
  exit 2
fi

# Refresh the served bundle from the current worktree before starting the center.
env -i PATH="$path" HOME="$workspace/home" TMPDIR="$workspace" \
  "$npm_bin" --prefix "$root/web" run build
(
  cd "$root"
  env -i PATH="$path" HOME="$workspace/home" TMPDIR="$workspace" \
    GOMODCACHE="$gomodcache" GOCACHE="$gocache" GOPROXY=off \
    "$go_bin" build -o "$workspace/houfeng-center" ./cmd/houfeng-center
)

port=$("$node_bin" -e 'const net=require("node:net");const server=net.createServer();server.listen(0,"127.0.0.1",()=>{console.log(server.address().port);server.close();});')
run_id=$(od -An -N18 -tx1 /dev/urandom | tr -d ' \n')
username="vps-live-$run_id"
password=$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
session_key=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
center_url="http://127.0.0.1:$port"

env -i \
  PATH="$path" \
  HOME="$workspace/home" \
  TMPDIR="$workspace" \
  LOCAL_LIVE_CENTER_URL="$center_url" \
  LOCAL_LIVE_USERNAME="$username" \
  LOCAL_LIVE_PASSWORD="$password" \
  LOCAL_LIVE_ROOT="$root" \
  LOCAL_LIVE_WORKSPACE="$workspace" \
  LOCAL_LIVE_PORT="$port" \
  LOCAL_LIVE_PLAYWRIGHT_BROWSERS_PATH="$playwright_browsers_path" \
  HOUFENG_INITIAL_USERNAME="$username" \
  HOUFENG_INITIAL_PASSWORD="$password" \
  HOUFENG_SESSION_HMAC_KEY="$session_key" \
  LOCAL_LIVE_CENTER_OWNER=launcher-managed \
  "$root/scripts/test-record-platform-integration.sh" postgres -- \
  bash -c '
    set -euo pipefail
    playwright_browsers_path=$LOCAL_LIVE_PLAYWRIGHT_BROWSERS_PATH
    unset LOCAL_LIVE_PLAYWRIGHT_BROWSERS_PATH
    center_pid=
    cleanup() {
      if [[ -n "$center_pid" ]]; then
        kill "$center_pid" 2>/dev/null || true
        wait "$center_pid" 2>/dev/null || true
      fi
    }
    trap cleanup EXIT
    trap "exit 130" INT
    trap "exit 143" TERM


    env \
      HOUFENG_HTTP_ADDR="127.0.0.1:$LOCAL_LIVE_PORT" \
      HOUFENG_WEB_DIST_DIR="$LOCAL_LIVE_ROOT/web/dist" \
      HOUFENG_PUBLIC_BASE_URL="$LOCAL_LIVE_CENTER_URL" \
      HOUFENG_RECORDS_ENABLED=false \
      HOUFENG_TELEGRAM_BOT_TOKEN= \
      HOUFENG_TELEGRAM_CHAT_ID= \
      "$LOCAL_LIVE_WORKSPACE/houfeng-center" \
      >"$LOCAL_LIVE_WORKSPACE/center.log" 2>&1 &
    center_pid=$!

    ready=false
    for _ in {1..60}; do
      if curl --silent --show-error --fail --max-time 2 "$LOCAL_LIVE_CENTER_URL/api/healthz" >/dev/null 2>&1; then
        ready=true
        break
      fi
      if ! kill -0 "$center_pid" 2>/dev/null; then
        break
      fi
      sleep 1
    done
    if [[ "$ready" != true ]]; then
      printf "The isolated Center did not become ready; startup log follows.\n" >&2
      cat "$LOCAL_LIVE_WORKSPACE/center.log" >&2
      exit 1
    fi

    env -i PATH="$PATH" HOME="$HOME" TMPDIR="$TMPDIR" \
      LOCAL_LIVE_CENTER_URL="$LOCAL_LIVE_CENTER_URL" \
      LOCAL_LIVE_USERNAME="$LOCAL_LIVE_USERNAME" \
      PLAYWRIGHT_BROWSERS_PATH="$playwright_browsers_path" \
      LOCAL_LIVE_PASSWORD="$LOCAL_LIVE_PASSWORD" \
      LOCAL_LIVE_CENTER_OWNER=launcher-managed \
      "$LOCAL_LIVE_ROOT/web/node_modules/.bin/playwright" test \
      --config "$LOCAL_LIVE_ROOT/web/playwright.local-live.config.ts" "$@"
  ' local-live "$@"
