#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-records-browser.XXXXXX")

cleanup() {
  status=$?
  if [ "${HOUFENG_RECORDS_KEEP_WORKSPACE-}" != "1" ]
  then
    rm -rf "$workspace"
  fi
  exit "$status"
}
trap cleanup EXIT

"$root/scripts/check-web-toolchain.sh"

if [ -d "${HOME}/.cache/ms-playwright" ]
then
  export PLAYWRIGHT_BROWSERS_PATH="${HOME}/.cache/ms-playwright"
fi

stdout_file="$workspace/child-stdout.log"
stderr_file="$workspace/child-stderr.log"

set +e
(
  cd "$root"
  npm --prefix web run test:e2e -- \
    e2e/visual-contracts.spec.ts \
    e2e/page-states.spec.ts \
    e2e/accessibility.spec.ts \
    e2e/comparison-workbench.spec.ts \
    e2e/record-workspace.spec.ts \
    e2e/record-portability.spec.ts
) >"$stdout_file" 2>"$stderr_file"
command_status=$?
set -e

if [ "$command_status" -ne 0 ]
then
  cat "$stdout_file"
  cat "$stderr_file" >&2
  exit "$command_status"
fi

if [ ! -d "$root/web/dist" ]
then
  printf 'web/dist is required after the browser production preview\n' >&2
  exit 1
fi

if grep -RFq -- 'dashboardTestFixtures' "$root/web/dist" || \
   grep -RFq -- 'coreRouteProfile' "$root/web/dist" || \
   grep -RFq -- 'vpsOverviewProfile' "$root/web/dist" || \
   grep -RFq -- '@axe-core/playwright' "$root/web/dist"
then
  printf 'production bundle contains a browser fixture or helper\n' >&2
  exit 1
fi

cat "$stdout_file"
