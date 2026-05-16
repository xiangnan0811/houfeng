#!/bin/sh
set -eu

usage() {
  cat >&2 <<'USAGE'
Usage: sh -s -- --server-url URL --enrollment-token TOKEN --version VERSION [--release-repo OWNER/REPO]

Installs houfeng-agent on Linux systemd hosts. The enrollment token is sensitive
and will be written to /etc/houfeng-agent/token with restrictive permissions.
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
AGENT_VERSION=""
RELEASE_REPO="xiangnan0811/houfeng"

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
[ -n "$ENROLLMENT_TOKEN" ] || fail "--enrollment-token is required"
[ -n "$AGENT_VERSION" ] || fail "--version is required"
[ "$AGENT_VERSION" != "dev" ] || fail "release version must not be dev; publish a release and regenerate the command"

case "$SERVER_URL" in
  http://*|https://*) ;;
  *) fail "--server-url must be an absolute http(s) URL" ;;
esac

OS="$(uname -s 2>/dev/null || true)"
[ "$OS" = "Linux" ] || fail "unsupported OS: $OS (Linux required)"

ARCH="$(uname -m 2>/dev/null || true)"
case "$ARCH" in
  x86_64|amd64) ASSET_ARCH="amd64" ;;
  aarch64|arm64) ASSET_ARCH="arm64" ;;
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

info "detected linux/${ASSET_ARCH} with systemd"
info "downloading release asset ${ASSET} from ${RELEASE_REPO}"
download "${BASE_URL}/${ASSET}" "${TMPDIR}/${ASSET}"
download "${BASE_URL}/sha256sums.txt" "${TMPDIR}/sha256sums.txt"

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
HOUFENG_AGENT_BUFFER_MAX_ENTRIES=2048
HOUFENG_AGENT_BUFFER_MAX_AGE=72h
EOF_ENV
chown root:houfeng-agent /etc/houfeng-agent/agent.env
chmod 0640 /etc/houfeng-agent/agent.env

if [ -f /etc/houfeng-agent/token ] && grep -q '"node_id"' /etc/houfeng-agent/token 2>/dev/null && grep -q '"sync_token"' /etc/houfeng-agent/token 2>/dev/null; then
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
systemctl enable --now houfeng-agent
info "houfeng-agent service enabled and started"
