#!/usr/bin/env bash
set -euo pipefail

make verify-docs
make verify-go
make verify-web
