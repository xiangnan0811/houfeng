#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s <postgres:16.0|postgres:16.6|postgres:16.12>\n' "$0" >&2
}

if [ "$#" -ne 1 ]; then
  usage
  exit 2
fi
postgres_image=$1
case "$postgres_image" in
  postgres:16.0|postgres:16.6|postgres:16.12) ;;
  *) usage; exit 2 ;;
esac

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

anchors=(TestPostgresIntegrationAppACLR2 TestPostgresIntegrationAppACLCurrent)
selector="^($(IFS='|'; printf '%s' "${anchors[*]}"))$"
anchors_json=$(printf '%s\n' "${anchors[@]}" | jq -R . | jq -s .)

# Match the business runner: keep private events and lifecycle scratch in the
# checkout, and only remove the unique workspace owned by this invocation.
scratch_root=${TMPDIR:-"$root/tmp/agent"}
case "$scratch_root" in
  "$root"/*) ;;
  *) printf 'TMPDIR must be an absolute directory inside %s\n' "$root" >&2; exit 2 ;;
esac
umask 077
mkdir -p "$scratch_root"
scratch_root=$(realpath "$scratch_root")
case "$scratch_root" in
  "$root"/*) ;;
  *) printf 'TMPDIR resolves outside the checkout\n' >&2; exit 2 ;;
esac
workspace=$(mktemp -d "$scratch_root/pg16-catalog.XXXXXX")
trap 'rm -rf -- "$workspace"' EXIT
events="$workspace/events.json"

TMPDIR="$workspace" HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE="$postgres_image" \
  scripts/test-record-platform-integration.sh pg16-catalog -- \
  go test -json -count=1 -timeout=30m ./internal/center/store/migrate \
    -run "$selector" | tee "$events"

# Descendant events cannot stand in for an exact top-level anchor. Require
# package start/pass too, and reject every skip/fail, including descendants.
# Slurping with jq also fails on malformed JSON; event validation rejects valid
# JSON that is not a Go test event from this package.
if ! jq -se --argjson anchors "$anchors_json" \
  --arg package 'houfeng/internal/center/store/migrate' '
  all(.[];
    type == "object"
    and .Package == $package
    and (.Action as $action
      | ["start", "run", "pause", "cont", "pass", "bench", "output"]
      | index($action) != null)
    and ((has("Test") | not) or (.Test | type == "string"))
  )
  and (any(.[]; (.Test // "") == "" and .Action == "start"))
  and (any(.[]; (.Test // "") == "" and .Action == "pass"))
  and (. as $events
    | all($anchors[];
        . as $anchor
        | any($events[]; .Test == $anchor and .Action == "run")
          and any($events[]; .Test == $anchor and .Action == "pass")
      ))
' "$events" >/dev/null; then
  printf 'PG16 catalog evidence is invalid or missing required package/anchor RUN/PASS events; skips and failures are forbidden\n' >&2
  exit 1
fi
