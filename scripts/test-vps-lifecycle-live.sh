#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
workspace=$(mktemp -d "${TMPDIR:-/tmp}/houfeng-lifecycle-live.XXXXXX")
chmod 700 "$workspace"
cd "$root"
go build -o "$workspace/houfeng-center" ./cmd/houfeng-center
go build -o "$workspace/houfeng-agent" ./cmd/houfeng-agent
exec python3 "$root/scripts/test-vps-lifecycle-live.py" --workspace "$workspace" "$@"
