#!/bin/sh
set -eu

usage() {
  cat >&2 <<'USAGE'
Usage: sh houfeng-agent-install.sh --server-url URL (--enrollment-token TOKEN | --enrollment-token-file PATH | --enrollment-token-stdin) --version VERSION [--release-repo OWNER/REPO] [--insecure-allow-http] [--install-missing-deps | --no-install-missing-deps]

Installs houfeng-agent on Linux systemd hosts. The enrollment token is sensitive
and will be written to /etc/houfeng-agent/token with restrictive permissions.
Prefer --enrollment-token-file or --enrollment-token-stdin. Passing
--enrollment-token can expose the secret through shell history and process list
inspection while the installer is running.

If minisign is missing, --install-missing-deps allows this installer to install
a pinned upstream minisign verifier into /usr/local/bin after checking its
tarball SHA256. Without that flag, interactive runs ask before installing and
non-interactive runs fail closed.
USAGE
}

fail() {
  printf '%s\n' "houfeng-agent install: $*" >&2
  exit 1
}

info() {
  printf '%s\n' "houfeng-agent install: $*"
}

SERVER_URL=""
ENROLLMENT_TOKEN=""
ENROLLMENT_TOKEN_FILE=""
READ_ENROLLMENT_TOKEN_STDIN=0
AGENT_VERSION=""
RELEASE_REPO="xiangnan0811/houfeng"
INSECURE_ALLOW_HTTP=0
INSTALL_MISSING_DEPS=""
HOUFENG_CHECKSUM_MINISIGN_PUBLIC_KEY="RWS4uZTCLx9cUtaBrFBtbPxBmqIcEPiKAcQcAD4M63rnLndpdC/KvYNz"
HOUFENG_MINISIGN_BOOTSTRAP_VERSION="0.12"
HOUFENG_MINISIGN_BOOTSTRAP_SHA256="9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73"
HOUFENG_MINISIGN_BOOTSTRAP_URL="https://github.com/jedisct1/minisign/releases/download/0.12/minisign-0.12-linux.tar.gz"

while [ "$#" -gt 0 ]; do
  case "$1" in
    --server-url)
      [ "$#" -ge 2 ] || fail "--server-url requires a value"
      SERVER_URL="$2"
      shift 2
      ;;
    --enrollment-token)
      [ "$#" -ge 2 ] || fail "--enrollment-token requires a value"
      ENROLLMENT_TOKEN="$2"
      shift 2
      ;;
    --enrollment-token-file)
      [ "$#" -ge 2 ] || fail "--enrollment-token-file requires a value"
      ENROLLMENT_TOKEN_FILE="$2"
      shift 2
      ;;
    --enrollment-token-stdin)
      READ_ENROLLMENT_TOKEN_STDIN=1
      shift
      ;;
    --version)
      [ "$#" -ge 2 ] || fail "--version requires a value"
      AGENT_VERSION="$2"
      shift 2
      ;;
    --release-repo)
      [ "$#" -ge 2 ] || fail "--release-repo requires a value"
      RELEASE_REPO="$2"
      shift 2
      ;;
    --insecure-allow-http)
      INSECURE_ALLOW_HTTP=1
      shift
      ;;
    --install-missing-deps)
      [ "$INSTALL_MISSING_DEPS" != "no" ] || fail "--install-missing-deps and --no-install-missing-deps are mutually exclusive"
      INSTALL_MISSING_DEPS="yes"
      shift
      ;;
    --no-install-missing-deps)
      [ "$INSTALL_MISSING_DEPS" != "yes" ] || fail "--install-missing-deps and --no-install-missing-deps are mutually exclusive"
      INSTALL_MISSING_DEPS="no"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

[ "$(id -u)" -eq 0 ] || fail "must run as root (use sudo)"
[ -n "$SERVER_URL" ] || fail "--server-url is required"
[ -n "$AGENT_VERSION" ] || fail "--version is required"
[ "$AGENT_VERSION" != "dev" ] || fail "release version must not be dev; publish a release and regenerate the command"

TOKEN_SOURCE_COUNT=0
[ -n "$ENROLLMENT_TOKEN" ] && TOKEN_SOURCE_COUNT=$((TOKEN_SOURCE_COUNT + 1))
[ -n "$ENROLLMENT_TOKEN_FILE" ] && TOKEN_SOURCE_COUNT=$((TOKEN_SOURCE_COUNT + 1))
[ "$READ_ENROLLMENT_TOKEN_STDIN" = "1" ] && TOKEN_SOURCE_COUNT=$((TOKEN_SOURCE_COUNT + 1))
[ "$TOKEN_SOURCE_COUNT" -eq 1 ] || fail "exactly one enrollment token source is required"

if [ -n "$ENROLLMENT_TOKEN_FILE" ]; then
  [ -r "$ENROLLMENT_TOKEN_FILE" ] || fail "--enrollment-token-file is not readable"
  ENROLLMENT_TOKEN="$(tr -d '\r\n' < "$ENROLLMENT_TOKEN_FILE")"
fi
if [ "$READ_ENROLLMENT_TOKEN_STDIN" = "1" ]; then
  ENROLLMENT_TOKEN="$(tr -d '\r\n')"
fi
[ -n "$ENROLLMENT_TOKEN" ] || fail "enrollment token must not be empty"

case "$SERVER_URL" in
  https://*) ;;
  http://*)
    [ "$INSECURE_ALLOW_HTTP" = "1" ] || fail "--server-url http requires --insecure-allow-http"
    ;;
  *) fail "--server-url must be an absolute http(s) URL" ;;
esac

OS="$(uname -s 2>/dev/null || true)"
[ "$OS" = "Linux" ] || fail "unsupported OS: $OS (Linux required)"

ARCH="$(uname -m 2>/dev/null || true)"
case "$ARCH" in
  x86_64|amd64)
    ASSET_ARCH="amd64"
    MINISIGN_ARCH="x86_64"
    ;;
  aarch64|arm64)
    ASSET_ARCH="arm64"
    MINISIGN_ARCH="aarch64"
    ;;
  *) fail "unsupported architecture: $ARCH (linux/amd64 or linux/arm64 required)" ;;
esac

command -v systemctl >/dev/null 2>&1 || fail "systemctl not found; systemd is required"
[ -d /run/systemd/system ] || fail "systemd does not appear to be running on this host"
for required_cmd in awk grep getent groupadd useradd install chown chmod mktemp; do
  command -v "$required_cmd" >/dev/null 2>&1 || fail "$required_cmd is required"
done

DOWNLOADER=""
if command -v curl >/dev/null 2>&1; then
  DOWNLOADER="curl"
elif command -v wget >/dev/null 2>&1; then
  DOWNLOADER="wget"
else
  fail "curl or wget is required"
fi

if command -v sha256sum >/dev/null 2>&1; then
  SHA256="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  SHA256="shasum -a 256"
else
  fail "sha256sum or shasum is required"
fi

ASSET="houfeng-agent_${AGENT_VERSION}_linux_${ASSET_ARCH}"
BASE_URL="https://github.com/${RELEASE_REPO}/releases/download/${AGENT_VERSION}"
TMPDIR="$(mktemp -d)"
cleanup() {
  rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

download() {
  url="$1"
  dest="$2"
  if [ "$DOWNLOADER" = "curl" ]; then
    curl -fsSL "$url" -o "$dest"
  else
    wget -q "$url" -O "$dest"
  fi
}

ask_yes_no_from_tty() {
  prompt="$1"
  [ -r /dev/tty ] && [ -w /dev/tty ] || return 1
  printf '%s' "$prompt" > /dev/tty
  IFS= read answer < /dev/tty || return 1
  case "$answer" in
    y|Y|yes|YES|Yes) return 0 ;;
    *) return 1 ;;
  esac
}

ensure_minisign() {
  if command -v minisign >/dev/null 2>&1; then
    return 0
  fi

  info "minisign is required to verify release checksums but is not installed"
  info "the installer can install minisign ${HOUFENG_MINISIGN_BOOTSTRAP_VERSION} to /usr/local/bin/minisign after checking the tarball SHA256"
  info "if you decline, the agent install/upgrade will stop before changing the agent binary, config, token, or systemd unit"

  case "$INSTALL_MISSING_DEPS" in
    yes) ;;
    no)
      fail "minisign is required to verify release checksums; dependency installation was disabled by --no-install-missing-deps"
      ;;
    "")
      if ! ask_yes_no_from_tty "Install minisign now? [y/N] "; then
        fail "minisign is required to verify release checksums; rerun with --install-missing-deps or install minisign manually"
      fi
      ;;
    *) fail "invalid dependency installation mode" ;;
  esac

  command -v tar >/dev/null 2>&1 || fail "tar is required to install missing minisign"
  info "downloading minisign ${HOUFENG_MINISIGN_BOOTSTRAP_VERSION}"
  download "$HOUFENG_MINISIGN_BOOTSTRAP_URL" "${TMPDIR}/minisign-${HOUFENG_MINISIGN_BOOTSTRAP_VERSION}-linux.tar.gz"
  ACTUAL_MINISIGN_BOOTSTRAP_SUM="$($SHA256 "${TMPDIR}/minisign-${HOUFENG_MINISIGN_BOOTSTRAP_VERSION}-linux.tar.gz" | awk '{print $1}')"
  [ "$HOUFENG_MINISIGN_BOOTSTRAP_SHA256" = "$ACTUAL_MINISIGN_BOOTSTRAP_SUM" ] || fail "minisign bootstrap checksum mismatch"
  tar -xzf "${TMPDIR}/minisign-${HOUFENG_MINISIGN_BOOTSTRAP_VERSION}-linux.tar.gz" -C "$TMPDIR"
  [ -f "${TMPDIR}/minisign-linux/${MINISIGN_ARCH}/minisign" ] || fail "minisign bootstrap archive does not contain linux/${MINISIGN_ARCH} binary"
  install -o root -g root -m 0755 "${TMPDIR}/minisign-linux/${MINISIGN_ARCH}/minisign" /usr/local/bin/minisign
  case ":$PATH:" in
    *:/usr/local/bin:*) ;;
    *) PATH="/usr/local/bin:$PATH" ;;
  esac
  command -v minisign >/dev/null 2>&1 || fail "minisign installation did not make minisign available"
  info "minisign installed to /usr/local/bin/minisign"
}

info "detected linux/${ASSET_ARCH} with systemd"
ensure_minisign
info "downloading release asset ${ASSET} from ${RELEASE_REPO}"
download "${BASE_URL}/${ASSET}" "${TMPDIR}/${ASSET}"
download "${BASE_URL}/sha256sums.txt" "${TMPDIR}/sha256sums.txt"
download "${BASE_URL}/sha256sums.txt.minisig" "${TMPDIR}/sha256sums.txt.minisig"

minisign -Vm "${TMPDIR}/sha256sums.txt" -P "$HOUFENG_CHECKSUM_MINISIGN_PUBLIC_KEY" -x "${TMPDIR}/sha256sums.txt.minisig"
info "checksum manifest signature verified"

EXPECTED_SUM="$(awk -v asset="$ASSET" '$2 == asset { print $1; found = 1; exit } END { if (!found) exit 1 }' "${TMPDIR}/sha256sums.txt" || true)"
[ -n "$EXPECTED_SUM" ] || fail "sha256sums.txt does not contain ${ASSET}"
ACTUAL_SUM="$($SHA256 "${TMPDIR}/${ASSET}" | awk '{print $1}')"
[ "$EXPECTED_SUM" = "$ACTUAL_SUM" ] || fail "checksum mismatch for ${ASSET}"

info "checksum verified"

if ! getent group houfeng-agent >/dev/null 2>&1; then
  groupadd --system houfeng-agent
fi
if ! getent passwd houfeng-agent >/dev/null 2>&1; then
  useradd --system --gid houfeng-agent --home-dir /var/lib/houfeng-agent --shell /usr/sbin/nologin houfeng-agent
fi

install -d -o root -g houfeng-agent -m 0750 /etc/houfeng-agent
install -d -o houfeng-agent -g houfeng-agent -m 0750 /var/lib/houfeng-agent
install -o root -g root -m 0755 "${TMPDIR}/${ASSET}" /usr/local/bin/houfeng-agent

cat > /etc/houfeng-agent/agent.env <<EOF_ENV
HOUFENG_AGENT_SERVER_URL=${SERVER_URL}
HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
HOUFENG_AGENT_BUFFER_FILE=/var/lib/houfeng-agent/sync-buffer.json
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=65536
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
HOUFENG_AGENT_BUFFER_MAX_BYTES=67108864
EOF_ENV
chown root:houfeng-agent /etc/houfeng-agent/agent.env
chmod 0640 /etc/houfeng-agent/agent.env

if [ -f /etc/houfeng-agent/token ] && grep -Eq '"(monitoring_instance_id|node_id)"' /etc/houfeng-agent/token 2>/dev/null && grep -q '"sync_token"' /etc/houfeng-agent/token 2>/dev/null; then
  info "preserving existing post-enrollment token file"
  chown houfeng-agent:houfeng-agent /etc/houfeng-agent/token
  chmod 0600 /etc/houfeng-agent/token
else
  umask 077
  printf '%s' "$ENROLLMENT_TOKEN" > /etc/houfeng-agent/token
  chown houfeng-agent:houfeng-agent /etc/houfeng-agent/token
  chmod 0600 /etc/houfeng-agent/token
fi

cat > /etc/systemd/system/houfeng-agent.service <<'EOF_UNIT'
[Unit]
Description=Houfeng Fleet Control Plane agent
Documentation=file:/opt/houfeng/docs/deploy/local-and-systemd.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=houfeng-agent
Group=houfeng-agent
EnvironmentFile=/etc/houfeng-agent/agent.env
ExecStart=/usr/local/bin/houfeng-agent
Restart=always
RestartSec=10s
StateDirectory=houfeng-agent
ReadWritePaths=/var/lib/houfeng-agent /etc/houfeng-agent/token
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=full

[Install]
WantedBy=multi-user.target
EOF_UNIT
chmod 0644 /etc/systemd/system/houfeng-agent.service

info "installed houfeng-agent binary, config, token file, and systemd unit"
systemctl daemon-reload
systemctl enable houfeng-agent
if systemctl is-active --quiet houfeng-agent; then
  systemctl restart houfeng-agent
  info "houfeng-agent service enabled and restarted"
else
  systemctl start houfeng-agent
  info "houfeng-agent service enabled and started"
fi
