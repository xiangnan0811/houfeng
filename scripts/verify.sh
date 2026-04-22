#!/usr/bin/env bash
set -euo pipefail

make verify-go

if [ -f web/package.json ]; then
  pushd web >/dev/null
  npm ci
  npm run test -- --run
  npm run build
  popd >/dev/null
fi
