#!/usr/bin/env bash
# Idempotent dependency/build bootstrap for the Houfeng dev environment.
# Runs after the repository is checked out. Must terminate and be safe to re-run.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "[install] repo root: $REPO_ROOT"

# 1. System dependency: PostgreSQL (only install if missing).
if ! command -v pg_ctlcluster >/dev/null 2>&1; then
  echo "[install] installing PostgreSQL server + client"
  export DEBIAN_FRONTEND=noninteractive
  sudo apt-get update -qq
  sudo apt-get install -y -qq postgresql postgresql-client
else
  echo "[install] PostgreSQL already present: $(pg_lsclusters -h 2>/dev/null | awk '{print $1}' | head -1)"
fi

# 2. Go module cache.
echo "[install] downloading Go modules"
go mod download

# 3. Web dependencies + production SPA build (center serves web/dist).
echo "[install] installing web dependencies (npm ci)"
npm --prefix web ci
echo "[install] building web SPA"
npm --prefix web run build

# 4. Center binary (-> bin/houfeng-center).
echo "[install] building houfeng-center"
make build-center

echo "[install] done"
