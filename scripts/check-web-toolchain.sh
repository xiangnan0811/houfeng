#!/usr/bin/env bash
set -euo pipefail

major="$(node -p 'process.versions.node.split(".")[0]')"
if [[ "$major" != "22" ]]; then
  echo "web requires Node 22.x; found $(node --version)" >&2
  exit 1
fi
