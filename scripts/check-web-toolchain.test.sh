#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="$repo_root/scripts/check-web-toolchain.sh"
fake_bin="$(mktemp -d)"
trap 'rm -rf "$fake_bin"' EXIT

if [[ ! -f "$repo_root/.node-version" ]]; then
  echo 'expected .node-version to pin the web runtime' >&2
  exit 1
fi

REPO_ROOT="$repo_root" node <<'EOF'
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')

const root = process.env.REPO_ROOT
const readJSON = (relativePath) => JSON.parse(
  fs.readFileSync(path.join(root, relativePath), 'utf8'),
)
const readJSONC = (relativePath) => {
  const source = fs.readFileSync(path.join(root, relativePath), 'utf8')
  const withoutComments = source
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/^\s*\/\/.*$/gm, '')
    .replace(/,\s*([}\]])/g, '$1')
  return JSON.parse(withoutComments)
}

assert.equal(fs.readFileSync(path.join(root, '.node-version'), 'utf8').trim(), '22.23.1')
assert.equal(readJSON('web/package.json').devDependencies['@types/node'], '^22')
assert.equal(readJSONC('web/tsconfig.app.json').compilerOptions.strict, true)
assert.equal(readJSONC('web/tsconfig.node.json').compilerOptions.strict, true)
EOF

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

if ! grep -F 'node-version-file: .node-version' "$repo_root/.github/workflows/ci.yml" >/dev/null; then
  echo 'expected CI to read the pinned .node-version file' >&2
  exit 1
fi
if grep -F 'node-version: 22' "$repo_root/.github/workflows/ci.yml" >/dev/null; then
  echo 'CI must use .node-version as the runtime source of truth' >&2
  exit 1
fi

if grep -F '@sh scripts/check-web-toolchain' "$repo_root/Makefile" >/dev/null; then
  echo 'Make must execute toolchain scripts directly so their Bash shebang is honored' >&2
  exit 1
fi
