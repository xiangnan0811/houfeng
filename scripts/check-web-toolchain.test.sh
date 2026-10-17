#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="$repo_root/scripts/check-web-toolchain.sh"
# Public Make targets must work with an ordinary system TMPDIR too. This test
# always owns checkout-local scratch rather than adopting the caller's temp root.
scratch_root="$repo_root/tmp/agent"
umask 077
mkdir -p "$scratch_root"
scratch_root=$(realpath "$scratch_root")
case "$scratch_root" in
  "$repo_root"/*) ;;
  *) printf 'TMPDIR resolves outside the checkout\n' >&2; exit 2 ;;
esac
fake_bin="$(mktemp -d "$scratch_root/web-toolchain.XXXXXX")"
trap 'rm -rf -- "$fake_bin"' EXIT

if [[ ! -f "$repo_root/.node-version" ]]; then
  echo 'expected .node-version to pin the web runtime' >&2
  exit 1
fi

cat > "$fake_bin/node" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-}" in
  -p)
    printf '%s\n' "${FAKE_NODE_VERSION%%.*}"
    ;;
  --version)
    printf 'v%s\n' "$FAKE_NODE_VERSION"
    ;;
  *)
    printf 'unexpected fake node arguments: %s\n' "$*" >&2
    exit 2
    ;;
esac
EOF
chmod +x "$fake_bin/node"

FAKE_NODE_VERSION=22.23.1 PATH="$fake_bin:$PATH" "$checker"

error_file="$fake_bin/error.log"
if FAKE_NODE_VERSION=24.18.0 PATH="$fake_bin:$PATH" "$checker" 2>"$error_file"; then
  echo 'expected Node 24 to be rejected' >&2
  exit 1
fi

grep -F 'web requires Node 22.x; found v24.18.0' "$error_file" >/dev/null
