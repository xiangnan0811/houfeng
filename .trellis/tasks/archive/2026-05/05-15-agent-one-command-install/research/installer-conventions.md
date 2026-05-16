# Research: One-line Linux agent installer conventions

- **Query**: Research common conventions for one-line Linux agent installers that download release binaries and install systemd services. Compare 2-4 tools/patterns (e.g. tailscale, node_exporter/prometheus exporters, cloudflared, netdata or similar). Focus on shell command UX, OS/arch detection, checksum/signature, systemd service/env file layout, idempotency, and uninstall/upgrade boundaries.
- **Scope**: external
- **Date**: 2026-05-15

## Findings

### Tools / Patterns Compared

| Tool / pattern | Primary install UX | Binary/package acquisition | Service model | Verification model | Upgrade/uninstall boundary |
|---|---|---|---|---|---|
| Tailscale | `curl -fsSL https://tailscale.com/install.sh \| sh`; env flags `TAILSCALE_VERSION=...`, `TRACK=unstable` | Adds vendor package repo and installs distro package via apt/yum/dnf/zypper/pacman/pkg | Package-provided `tailscaled`; installer runs `systemctl enable --now tailscaled` on most systemd distros | Package repository signing key; apt keyring or rpm repo GPG | Upgrade/uninstall delegated to OS package manager; version pin via package version |
| cloudflared | Download binary/package; then `cloudflared service install`; docs also support apt/yum/deb/rpm and direct GitHub binary links | Package repository, `.deb` / `.rpm`, or raw GitHub binary per arch | CLI writes `/etc/systemd/system/cloudflared.service` plus optional update timer; config copied to `/etc/cloudflared/config.yml` | Package repo signing key for apt; raw binaries are linked directly in docs; source code service install does not add checksum validation | `cloudflared service uninstall` removes units; `cloudflared update` for raw/source installs; package-manager installs must upgrade with same package manager |
| Netdata | `wget -O /tmp/netdata-kickstart.sh https://get.netdata.cloud/kickstart.sh && sh /tmp/netdata-kickstart.sh` or equivalent curl | Chooses native packages first, static `.gz.run` fallback, source build last resort | Installs Netdata service plus updater service/timer/cron depending environment | Static/source builds fetch `sha256sums.txt` and validate SHA256; docs also publish md5 integrity check for kickstart script | Existing install detected; default attempts update, explicit `--reinstall`, `--reinstall-clean`, `--uninstall`, `--claim-only`; unsafe transitions gated by prompts/non-interactive failures |
| Prometheus node_exporter / exporter pattern | Not a one-line official installer; docs show download tarball, extract, run; automation commonly managed by distro packages or config management | Static tarballs per OS/arch from GitHub releases plus `sha256sums.txt` and download-page digests | Example systemd unit and socket in repo; service uses dedicated user and `/etc/sysconfig/node_exporter` env/options file | Release assets include `sha256sums.txt`; Prometheus download page lists digests | Manual/package/config-management boundary; systemd examples document layout but not upgrade/uninstall logic |

### Tailscale

**Shell command UX**

Tailscale exposes a concise pipe-to-shell entry point and documents env-variable controls in the script header:

```sh
curl -fsSL https://tailscale.com/install.sh | sh
curl -fsSL https://tailscale.com/install.sh | TAILSCALE_VERSION=1.88.4 sh
curl -fsSL https://tailscale.com/install.sh | TRACK=unstable sh
```

Source: `https://tailscale.com/install.sh`, lines 8-15.

The script wraps execution in `main()` so a truncated partial download does not execute half a script, and runs with `set -eu`.

Source: `https://tailscale.com/install.sh`, lines 17-22.

**OS / arch / distro detection**

Tailscale detects distro through `/etc/os-release`, with explicit per-distro mapping to package manager and repo path. It populates `OS`, `VERSION`, `PACKAGETYPE`, and apt-key mode. Examples:

- Ubuntu/Pop/Neon/Tuxedo normalize to `OS="ubuntu"`, choose `VERSION_CODENAME` or `UBUNTU_CODENAME`, and use apt.
- Debian normalizes to Debian codename and uses apt.
- Fallback `uname` handles non-Linux/macOS/other-Linux cases.

Source: `https://tailscale.com/install.sh`, lines 23-35, 48-96, 380-392.

The installer checks `curl` then `wget`, tests connectivity to `https://pkgs.tailscale.com/`, and checks whether the selected OS/version is supported by fetching an `installer-supported` marker.

Source: `https://tailscale.com/install.sh`, lines 397-430.

**Checksum / signature**

Tailscale does not download a standalone release binary in this script; it installs from vendor package repositories. Verification is therefore delegated to package manager repo signing:

- Apt keyring path: `/usr/share/keyrings/tailscale-archive-keyring.gpg`.
- Apt source list: `/etc/apt/sources.list.d/tailscale.list`.
- Legacy apt uses `apt-key add`.
- RPM-family installs import repo GPG or add signed repo files.

Source: `https://tailscale.com/install.sh`, lines 551-570, 647-649.

**systemd service / env file layout**

The service is package-owned; the installer starts/enables the package's service rather than writing a unit file itself. Most Linux package-manager branches run:

```sh
systemctl enable --now tailscaled
```

Source: `https://tailscale.com/install.sh`, lines 587, 631, 642, 655, 666. Apt/Kali special case uses lines 572-574.

**Idempotency**

The script repeatedly creates/overwrites deterministic repo files and then invokes package manager install. Re-running tends to converge through package manager behavior. Notable idempotent-ish operations:

- `mkdir -p --mode=0755 /usr/share/keyrings`.
- Overwrites `/etc/apt/sources.list.d/tailscale.list`.
- Package manager handles already-installed package state.
- `systemctl enable --now` is safe on already-enabled/already-running service.

Source: `https://tailscale.com/install.sh`, lines 551-570 and service enable lines above.

**Uninstall / upgrade boundaries**

The install script is install-focused. Upgrade is package-manager upgrade or re-run with optional `TAILSCALE_VERSION`; uninstall is package-manager removal. This boundary is implied by the script's package-manager approach and version pin behavior:

```sh
apt-get install -y "tailscale=$TAILSCALE_VERSION" tailscale-archive-keyring
apt-get install -y tailscale tailscale-archive-keyring
```

Source: `https://tailscale.com/install.sh`, lines 536-570.

### cloudflared

**Shell command UX**

Cloudflare separates binary/package installation from service registration. Public docs list direct Linux downloads for raw binaries, `.deb`, and `.rpm` by architecture, and also direct users to the Cloudflare Package Repository.

Source: Cloudflare docs partial `src/content/partials/cloudflare-one/tunnel/downloads.mdx`, lines showing Linux table with:

- `https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64`
- `cloudflared-linux-arm64`
- `.deb` and `.rpm` variants.

Cloudflare's Debian package-repository docs use explicit keyring and apt source setup:

```sh
sudo mkdir -p --mode=0755 /usr/share/keyrings
curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main" | sudo tee /etc/apt/sources.list.d/cloudflared.list
sudo apt-get update && sudo apt-get install cloudflared
```

Source: `https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/partials/cloudflare-one/tunnel/cloudflared-debian-install.mdx`.

For service installation, docs show:

```sh
cloudflared service install
sudo cloudflared --config /home/<USER>/.cloudflared/config.yml service install
systemctl start cloudflared
systemctl status cloudflared
systemctl restart cloudflared
```

Source: `https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/partials/cloudflare-one/tunnel/locally-managed/as-a-service/linux.mdx`.

**OS / arch / distro detection**

cloudflared's public download matrix names arch-specific artifacts instead of providing a one script that detects arch. Examples from the latest release API:

- `cloudflared-linux-amd64`
- `cloudflared-linux-arm64`
- `cloudflared-linux-amd64.deb`
- `cloudflared-linux-arm64.deb`
- `cloudflared-linux-x86_64.rpm`
- `cloudflared-linux-aarch64.rpm`

Source: GitHub releases API for `cloudflare/cloudflared`, latest tag `2026.5.0` on 2026-05-15.

Service-manager detection happens inside the `cloudflared service install` command: it checks `/run/systemd/system` to choose systemd, otherwise falls back to SysV.

Source: `https://raw.githubusercontent.com/cloudflare/cloudflared/master/cmd/cloudflared/linux_service.go`, lines 196-202 and 230-237.

**Checksum / signature**

For apt, Cloudflare uses a package signing key and signed-by keyring path. For raw GitHub binaries, the docs and release assets observed do not expose a separate checksum file in the listed latest-release assets. The service installer source writes service files but does not validate a downloaded binary because it assumes `cloudflared` is already installed and running from the local executable path.

Sources:

- Package keyring: Cloudflare Debian install partial above.
- Release assets: GitHub releases API for `cloudflare/cloudflared`, latest tag `2026.5.0`, assets include binaries/deb/rpm but no `sha256sums.txt` observed.
- Service install executable path: `linux_service.go`, lines 205-211.

**systemd service / env file layout**

cloudflared writes units directly under `/etc/systemd/system`:

- `/etc/systemd/system/cloudflared.service`
- `/etc/systemd/system/cloudflared-update.service`
- `/etc/systemd/system/cloudflared-update.timer`

The main unit template:

```ini
[Unit]
Description=cloudflared
After=network-online.target
Wants=network-online.target

[Service]
TimeoutStartSec=15
Type=notify
ExecStart={{ .Path }} --no-autoupdate{{ range .ExtraArgs }} {{ . }}{{ end }}
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
```

Source: `linux_service.go`, lines 45-71.

The update timer/service uses a daily timer and restarts `cloudflared` when `cloudflared update` exits with code 11:

```ini
ExecStart=/bin/bash -c '{{ .Path }} update; code=$?; if [ $code -eq 11 ]; then systemctl restart cloudflared; exit 0; fi; exit $code'
OnCalendar=daily
```

Source: `linux_service.go`, lines 73-93.

Config layout is `/etc/cloudflared/config.yml`; `service install` copies the current config there unless already present/conflicting:

- `serviceConfigDir = "/etc/cloudflared"`
- `serviceConfigPath = "/etc/cloudflared/config.yml"`
- extra args: `--config /etc/cloudflared/config.yml tunnel run`

Source: `linux_service.go`, lines 45-49 and 245-278.

No `EnvironmentFile=` is used in the systemd unit. The SysV template optionally sources `/etc/sysconfig/$name`.

Source: `linux_service.go`, lines 98-123.

**Idempotency**

Service install handles config conflicts explicitly:

- If source config is not `/etc/cloudflared/config.yml` and destination exists, it errors with a conflict message rather than overwriting.
- It generates service templates each time, enables the service, optionally starts update timer, reloads daemon, then starts service.

Source: `linux_service.go`, lines 266-318.

Uninstall checks installed services and skips missing ones:

```go
log.Info().Msgf("Service '%s' not installed, skipping its uninstall", serviceName)
```

Source: `linux_service.go`, lines 370-379.

**Uninstall / upgrade boundaries**

cloudflared has a distinct CLI uninstall path:

- `cloudflared service uninstall` calls systemd or SysV uninstall.
- systemd uninstall disables/stops `cloudflared`, stops update timer if present, removes installed unit files, and runs `systemctl daemon-reload`.

Source: `linux_service.go`, lines 351-409.

Cloudflare update docs distinguish install source:

- Apt: `sudo apt-get update && sudo apt-get install --only-upgrade cloudflared`, then `sudo systemctl restart cloudflared.service`.
- `dpkg -i`: download latest arch-matched deb with `dpkg --print-architecture`, install it, restart service.
- Red Hat: `sudo yum update cloudflared`, restart service.
- Raw/source: run `cloudflared update`.
- Package-manager installs must be updated using the same package manager.

Source: `https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/partials/cloudflare-one/tunnel/update-cloudflared.mdx`.

### Netdata

**Shell command UX**

Netdata documents a one-line script download followed by execution rather than direct pipe-to-shell:

```sh
wget -O /tmp/netdata-kickstart.sh https://get.netdata.cloud/kickstart.sh && sh /tmp/netdata-kickstart.sh
curl https://get.netdata.cloud/kickstart.sh > /tmp/netdata-kickstart.sh && sh /tmp/netdata-kickstart.sh
```

Source: `https://learn.netdata.cloud/docs/netdata-agent/installation/linux/`.

The kickstart script has extensive CLI flags. Relevant options include:

- `--non-interactive`, `--interactive`, `--dry-run`, `--dont-start-it`
- `--release-channel`, `--stable-channel`, `--nightly-channel`, `--install-version`
- `--auto-update`, `--no-updates`, `--auto-update-type systemd|interval|crontab`
- `--claim-token`, `--claim-rooms`
- `--reinstall`, `--reinstall-even-if-unsafe`, `--reinstall-clean`, `--uninstall`, `--claim-only`
- `--native-only`, `--static-only`, `--build-only`, `--install-type`

Source: `https://raw.githubusercontent.com/netdata/netdata/master/packaging/installer/kickstart.sh`, lines 180-220 and 2475-2510.

**OS / arch / distro detection**

Netdata's script adjusts PATH for distro differences, stores `SYSARCH="$(uname -m)"`, supports static arches `x86_64 armv7l armv6l aarch64`, and detects Linux/BSD/macOS plus distro via os-release logic.

Source: `kickstart.sh`, lines 26, 29-41, 777-785.

Install method selection is layered:

1. Native packages when available/preferred.
2. Static build fallback.
3. Build from source fallback.

The docs describe the same behavior: detects OS/environment, checks existing install, installs using native packages, static build fallback, build from source last resort, installs auto-update unless disabled, optionally connects node to Netdata Cloud.

Source: `https://learn.netdata.cloud/docs/netdata-agent/installation/linux/`.

**Checksum / signature**

For static installs, Netdata downloads both the arch-specific `.gz.run` and `sha256sums.txt`, then validates:

```sh
grep "${netdata_agent}" ./sha256sum.txt | safe_sha256sum -c -
```

`safe_sha256sum()` uses `shasum -a 256` or `sha256sum` and fails if neither exists.

Source: `kickstart.sh`, lines 756-763, 1854-1935.

For source tarballs and offline install sources, the script also downloads/verifies checksum files.

Source: `kickstart.sh`, lines 1979-2102 and 2183-2207.

The docs also provide an integrity check for the installer script itself using md5:

```sh
[ "1f92a740bd8857893d4d66e5887acd16" = "$(curl -Ss https://get.netdata.cloud/kickstart.sh | md5sum | cut -d ' ' -f 1)" ] && echo "OK, VALID" || echo "FAILED, INVALID"
```

Source: `https://learn.netdata.cloud/docs/netdata-agent/installation/linux/`.

**systemd service / env file layout**

Netdata service unit templates live in the repo under `system/systemd/`:

- `netdata.service.in`
- `netdata-updater.service.in`
- `netdata-updater.timer`

Main service template highlights:

```ini
[Unit]
Description=Netdata, X-Ray Vision for your infrastructure!
After=systemd-tmpfiles-setup.service network.target network-online.target nss-lookup.target
Wants=systemd-tmpfiles-setup.service network-online.target nss-lookup.target

[Service]
LogNamespace=netdata
Type=simple
User=root
Group=netdata
RuntimeDirectory=netdata
RuntimeDirectoryMode=0775
PIDFile=/run/netdata/netdata.pid
ExecStart=@sbindir_POST@/netdata -P /run/netdata/netdata.pid -D
Restart=on-failure
RestartSec=30
ProtectSystem=full
ProtectHome=read-only
ReadWriteDirectories=/run/netdata

[Install]
WantedBy=multi-user.target
```

Source: `https://raw.githubusercontent.com/netdata/netdata/master/system/systemd/netdata.service.in`.

Updater units:

```ini
[Service]
Type=oneshot
ExecStart=@pkglibexecdir_POST@/netdata-updater.sh

[Timer]
Persistent=false
OnCalendar=daily
Unit=netdata-updater.service
```

Source: `system/systemd/netdata-updater.service.in` and `system/systemd/netdata-updater.timer`.

The service installer detects service managers (`systemd openrc lsb initd runit dinit`) and for systemd installs into the detected service directory, runs `systemctl daemon-reload`, and enables/disables based on prior state and flags.

Source: `https://raw.githubusercontent.com/netdata/netdata/master/system/install-service.sh.in`, lines 29-37, 231-304, 770-777.

Netdata stores install metadata under config paths such as:

- `/etc/netdata/.install-type` or `/opt/netdata/etc/netdata/.install-type`
- `/etc/netdata/.environment`
- `/etc/netdata/.opt-out-from-anonymous-statistics`

Source: `kickstart.sh`, lines 1038-1056, 1818-1829, 1955-1968.

**Idempotency**

Netdata is explicit about existing installation handling:

- Detects existing installs and install type.
- If found and no reinstall requested, attempts update via `netdata-updater.sh`.
- If install type unknown, non-interactive mode fails for unsafe update/reinstall paths; interactive mode prompts.
- `--claim-only` updates cloud claiming without attempting software update.

Source: `kickstart.sh`, lines 908-941 and 1068-1150.

Service installer also preserves enable/disable intent: if Netdata was already disabled, `ENABLE="disable"`; otherwise `ENABLE="enable"`.

Source: `system/install-service.sh.in`, lines 255-304.

**Uninstall / upgrade boundaries**

Netdata has first-class installer actions:

- `--uninstall`: uninstall existing installation.
- `--reinstall`: reinstall existing install if safe.
- `--reinstall-even-if-unsafe`: proceed even when known unsafe.
- `--reinstall-clean`: uninstall then install fresh; fails if no existing install.
- `--claim-only`: only claim existing install or install-and-claim if missing.

Source: `kickstart.sh`, lines 110-130, 212-220, 2500-2504.

Auto-update is enabled by default unless offline install or exact version/major version pinning disables it. The updater can be enabled/disabled via `netdata-updater.sh --enable-auto-updates` or `--disable-auto-updates`; systemd/interval/crontab update methods are supported.

Source: `kickstart.sh`, lines 1424-1451 and 2448-2460.

### Prometheus node_exporter / exporter pattern

**Shell command UX**

Prometheus node_exporter does not provide an official one-line Linux installer. The guide describes it as a single static binary installed via tarball:

```sh
# Node Exporter is available for multiple OS targets and architectures.
# Downloads are available at:
# https://github.com/prometheus/node_exporter/releases/download/v<VERSION>/node_exporter-<VERSION>.<OS>-<ARCH>.tar.gz
wget https://github.com/prometheus/node_exporter/releases/download/v1.10.2/node_exporter-1.10.2.linux-amd64.tar.gz
tar xvfz node_exporter-1.10.2.linux-amd64.tar.gz
cd node_exporter-1.10.2.linux-amd64
./node_exporter
```

Source: `https://prometheus.io/docs/guides/node-exporter/`.

The README points users to a step-by-step guide and an Ansible role for automated installs.

Source: `https://raw.githubusercontent.com/prometheus/node_exporter/master/README.md`, lines 18-26.

**OS / arch / distro detection**

The official download pattern encodes OS/arch in filenames, e.g. `node_exporter-<version>.linux-amd64.tar.gz`. The latest release assets observed include:

- `node_exporter-1.11.1.linux-amd64.tar.gz`
- `node_exporter-1.11.1.linux-arm64.tar.gz`
- `sha256sums.txt`

Source: GitHub releases API for `prometheus/node_exporter`, latest tag `v1.11.1` on 2026-05-15.

No official shell script was found that maps `uname -m` to release asset names. Automation commonly has to provide that mapping itself or use packages/config-management.

**Checksum / signature**

GitHub release assets include `sha256sums.txt`. The Prometheus downloads page also lists a digest beside `node_exporter-1.11.1.linux-amd64.tar.gz`.

Sources:

- GitHub releases API for `prometheus/node_exporter`, latest tag `v1.11.1`.
- `https://prometheus.io/download/#node_exporter`, observed digest listing for `node_exporter-1.11.1.linux-amd64.tar.gz`.

**systemd service / env file layout**

node_exporter ships example systemd files rather than an installer. The README for examples states:

- Unit files go into `/etc/systemd/system`.
- It needs a user named `node_exporter` with `/sbin/nologin` and no special privileges.
- It needs a sysconfig file in `/etc/sysconfig/node_exporter`.
- It needs `/var/lib/node_exporter/textfile_collector` owned by `node_exporter:node_exporter`.

Source: `https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/systemd/README.md`.

Example service:

```ini
[Unit]
Description=Node Exporter
Requires=node_exporter.socket

[Service]
User=node_exporter
# Fallback when environment file does not exist
Environment=OPTIONS=
EnvironmentFile=-/etc/sysconfig/node_exporter
ExecStart=/usr/sbin/node_exporter --web.systemd-socket $OPTIONS

[Install]
WantedBy=multi-user.target
```

Source: `https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/systemd/node_exporter.service`.

Example socket:

```ini
[Socket]
ListenStream=9100

[Install]
WantedBy=sockets.target
```

Source: `https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/systemd/node_exporter.socket`.

Example sysconfig:

```sh
OPTIONS="--collector.textfile.directory /var/lib/node_exporter/textfile_collector"
```

Source: `https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/systemd/sysconfig.node_exporter`.

**Idempotency**

The official node_exporter materials do not define installer idempotency. The pattern delegates idempotency to package managers, config management (the README references the Prometheus Community Ansible role), or a user-authored shell script.

Source: node_exporter README lines 24-26 and systemd examples above.

**Uninstall / upgrade boundaries**

The official node_exporter tarball flow is manual: replace the binary/tarball and restart the service if using systemd. No official uninstall/upgrade script was found in the referenced docs/repo examples. Package-manager and Ansible installs own that lifecycle when used.

## Cross-cutting Conventions

### 1. Command UX patterns

Observed installer UX clusters:

1. **Pipe-to-shell package bootstrap**: Tailscale uses `curl -fsSL ... | sh`, then relies on distro packages.
2. **Download script then execute**: Netdata documents saving to `/tmp/...` then `sh`, which makes optional integrity checking and inspection easier.
3. **Install binary/package, then run built-in service command**: cloudflared separates artifact installation from `cloudflared service install`.
4. **Static tarball plus example units**: node_exporter exposes release assets and systemd examples but leaves automation to users/packages.

Common flags/env surfaces:

- Release track/channel: Tailscale `TRACK=stable|unstable`; Netdata `--stable-channel`, `--nightly-channel`, `--release-channel`.
- Version pinning: Tailscale `TAILSCALE_VERSION`; Netdata `--install-version`; node_exporter URL embeds version.
- Non-interactive automation: Netdata `--non-interactive`; Tailscale package manager runs use `-y` / `DEBIAN_FRONTEND=noninteractive`.
- Cloud enrollment token: Netdata `--claim-token`; cloudflared remote/Docker docs use `--token <TOKEN>` for tunnel run paths, while locally-managed service install uses config/credential files.

### 2. OS / arch detection conventions

Common approaches:

- Use `/etc/os-release` for distro family/codename when installing package repositories (Tailscale, Netdata native package path).
- Use `uname -s` and `uname -m` for OS/arch static asset naming (Netdata static path; node_exporter manual URL pattern).
- Normalize architectures to release asset names (`x86_64`, `aarch64`, `amd64`, `arm64`) when downloading raw binaries. Netdata supports `x86_64 armv7l armv6l aarch64`; cloudflared release artifacts use both `amd64/arm64` binary names and `x86_64/aarch64` rpm names; node_exporter uses Go-style `linux-amd64`, `linux-arm64`.
- Detect service manager separately from OS: cloudflared checks `/run/systemd/system`; Netdata service installer checks systemd/openrc/lsb/initd/runit/dinit.

### 3. Verification conventions

Observed verification levels:

- **Package repositories**: use GPG signing keys and package manager trust (`signed-by=/usr/share/keyrings/...`, repo GPG import).
- **Static release binaries**: fetch checksum manifest (`sha256sums.txt`) and validate selected artifact (Netdata, node_exporter assets).
- **Installer script integrity**: Netdata docs include a separate script integrity check before execution, though it is md5 in the currently observed docs.
- **No checksum in service command**: cloudflared `service install` uses the currently running executable path and writes service files; artifact verification must happen before that via package repo or user download choice.

### 4. systemd / env file layout conventions

Observed layouts:

- **Package-owned service**: Tailscale installer does not write unit file; package owns `tailscaled.service`; script only enables/starts.
- **CLI-generated service under `/etc/systemd/system`**: cloudflared writes service and optional update timer directly to `/etc/systemd/system`.
- **Template-installed service plus updater timer**: Netdata installs templated units, service manager-specific files, and updater service/timer.
- **Example service + env/sysconfig file**: node_exporter example uses `EnvironmentFile=-/etc/sysconfig/node_exporter` and keeps runtime options out of the unit.

Common systemd fields for agents:

- `After=network-online.target` / `Wants=network-online.target` for network agents (cloudflared, Netdata).
- `Restart=on-failure` and `RestartSec=...` (cloudflared, Netdata).
- Dedicated user/group when possible (node_exporter user; Netdata group but service runs root for host visibility; cloudflared template does not set user).
- Optional updater timer for self-updating agents (cloudflared, Netdata).
- Config in `/etc/<agent>/...` or `/etc/sysconfig/<agent>`; cloudflared uses `/etc/cloudflared/config.yml`, node_exporter uses `/etc/sysconfig/node_exporter`, Netdata uses `/etc/netdata` or `/opt/netdata/etc/netdata`.

### 5. Idempotency conventions

Observed idempotency mechanics:

- Deterministic file paths and overwrites for repo setup (Tailscale apt list/keyring).
- Package manager install handles already-installed state (Tailscale, package-based cloudflared, Netdata native package path).
- `systemctl enable --now` / `daemon-reload` / restart operations are safe to repeat for package-owned service flows.
- Existing config conflict protection rather than overwrite (cloudflared refuses to overwrite `/etc/cloudflared/config.yml` when a different source config is used).
- Existing install detection with explicit update/reinstall/uninstall action model (Netdata).
- Skip missing units during uninstall (cloudflared).

### 6. Upgrade / uninstall boundary conventions

Observed boundary styles:

- **Package manager owns lifecycle**: Tailscale, cloudflared package repo/deb/rpm. Installer only bootstraps repo and starts service.
- **Agent CLI owns service lifecycle**: cloudflared `service install` / `service uninstall` handles unit files; `cloudflared update` is only for raw/source installs.
- **Installer script owns lifecycle**: Netdata has explicit installer actions for update/reinstall/uninstall and internal updater.
- **Manual/config-management owns lifecycle**: node_exporter tarball + examples.

## External References

- [Tailscale install script](https://tailscale.com/install.sh) — pipe-to-shell package bootstrap, distro detection, package repo signing, service enable/start.
- [Tailscale Linux install docs](https://tailscale.com/kb/1031/install-linux) — public one-line `curl -fsSL https://tailscale.com/install.sh | sh` command.
- [Cloudflare cloudflared downloads docs source](https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/partials/cloudflare-one/tunnel/downloads.mdx) — Linux binary/deb/rpm download matrix.
- [Cloudflare Debian package install docs source](https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/partials/cloudflare-one/tunnel/cloudflared-debian-install.mdx) — keyring and apt repository setup.
- [Cloudflare cloudflared Linux service docs source](https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/partials/cloudflare-one/tunnel/locally-managed/as-a-service/linux.mdx) — `cloudflared service install` and `systemctl` flow.
- [cloudflared Linux service source](https://raw.githubusercontent.com/cloudflare/cloudflared/master/cmd/cloudflared/linux_service.go) — systemd/SysV service templates, install/uninstall behavior, config path.
- [Cloudflare cloudflared update docs source](https://raw.githubusercontent.com/cloudflare/cloudflare-docs/production/src/content/partials/cloudflare-one/tunnel/update-cloudflared.mdx) — upgrade boundaries by install source.
- [Netdata Linux install docs](https://learn.netdata.cloud/docs/netdata-agent/installation/linux/) — one-line installer commands, options, installer integrity check, high-level install behavior.
- [Netdata kickstart script](https://raw.githubusercontent.com/netdata/netdata/master/packaging/installer/kickstart.sh) — static/native/source selection, checksums, existing install handling, claim/uninstall/reinstall actions.
- [Netdata systemd service template](https://raw.githubusercontent.com/netdata/netdata/master/system/systemd/netdata.service.in) — service fields and sandboxing layout.
- [Netdata updater service/timer templates](https://raw.githubusercontent.com/netdata/netdata/master/system/systemd/netdata-updater.service.in) and [timer](https://raw.githubusercontent.com/netdata/netdata/master/system/systemd/netdata-updater.timer) — daily updater timer.
- [Netdata install-service script](https://raw.githubusercontent.com/netdata/netdata/master/system/install-service.sh.in) — service manager detection and systemd unit install logic.
- [Prometheus node_exporter guide](https://prometheus.io/docs/guides/node-exporter/) — static tarball install pattern and OS/arch filename convention.
- [Prometheus node_exporter README](https://raw.githubusercontent.com/prometheus/node_exporter/master/README.md) — install guide and Ansible automation pointer.
- [node_exporter systemd examples README](https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/systemd/README.md) — `/etc/systemd/system`, dedicated user, sysconfig, textfile directory requirements.
- [node_exporter systemd service](https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/systemd/node_exporter.service), [socket](https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/systemd/node_exporter.socket), and [sysconfig](https://raw.githubusercontent.com/prometheus/node_exporter/master/examples/systemd/sysconfig.node_exporter) — service/env file pattern.
- [Prometheus downloads page](https://prometheus.io/download/#node_exporter) — release digest listing for node_exporter artifacts.

### Related Specs

No internal code/spec search was needed for this external convention research. The requested output path is under the active task:

- `.trellis/tasks/05-15-agent-one-command-install/research/installer-conventions.md`

## Caveats / Not Found

- No official node_exporter one-line Linux installer was found in the upstream README/guide/examples; node_exporter is included as the common static-exporter pattern rather than a one-command installer.
- cloudflared latest GitHub release assets observed on 2026-05-15 did not include a checksum manifest in the GitHub API asset list; package repository installs rely on repo signing instead.
- Tailscale's installer is package-repository based, not a release-binary downloader. It is still relevant for one-line command UX, distro detection, package signing, and systemd enable/start conventions.
- Netdata docs currently show an md5 installer-script integrity check; the kickstart script itself uses SHA256 for downloaded release/source/offline artifacts.
