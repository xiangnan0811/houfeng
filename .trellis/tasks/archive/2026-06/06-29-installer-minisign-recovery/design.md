# Installer minisign dependency recovery design

## Architecture

The change stays inside the center-served shell installer plus the center endpoint that generates the command. The agent binary, release pipeline, and signed checksum contract remain unchanged.

`internal/center/installer/houfeng-agent-install.sh` will gain a preflight helper for `minisign`:

1. Validate root, server URL, OS, architecture, systemd, basic tools, downloader, and checksum tool.
2. Resolve the expected `minisign` architecture path from `uname -m`.
3. If `minisign` exists, continue without user interaction.
4. If missing, decide whether installation is allowed:
   - `--install-missing-deps`: install without prompting.
   - `--no-install-missing-deps`: fail immediately.
   - default with TTY: explain and prompt.
   - default without TTY: fail with instructions.
5. Download upstream `minisign-0.12-linux.tar.gz` into the existing temporary directory.
6. Verify the tarball against the pinned SHA256 before extracting.
7. Extract and install `minisign-linux/<arch>/minisign` to `/usr/local/bin/minisign`.
8. Re-check `command -v minisign` and then run the existing signed manifest verification.

The center-generated command will pass `--install-missing-deps` so the UI workflow remains a single copy/paste command. Operators who fetch the script manually can still choose prompt mode or `--no-install-missing-deps`.

## CLI Contract

New installer flags:

- `--install-missing-deps`: grant consent for installer-managed dependency recovery. Currently this only covers `minisign`.
- `--no-install-missing-deps`: forbid installer-managed dependency recovery. If `minisign` is missing, installation fails before release download or local writes.

Both flags are mutually exclusive. Passing both is an error.

Existing flags and token source behavior remain unchanged. The token still travels through `--enrollment-token-stdin` in generated commands; dependency prompts must read from `/dev/tty`, never from stdin.

## User Experience

Interactive missing `minisign` message:

- State that `minisign` is required for signed release checksum verification.
- State that the installer can install a pinned upstream static `minisign` binary into `/usr/local/bin/minisign`.
- State that the tarball SHA256 will be checked before installation.
- State that choosing no stops the install/upgrade before modifying the agent.
- Ask `Install minisign now? [y/N]`.

Non-interactive missing `minisign` message:

- State that `minisign` is missing and cannot be installed without explicit consent.
- Tell the operator to rerun with `--install-missing-deps` to allow installing the verifier, or install `minisign` manually and rerun.

Generated command:

- Includes `--install-missing-deps` because the user already initiated the agent install/upgrade from the authenticated center UI.
- Keeps `--enrollment-token-stdin` and heredoc behavior.

## Security Model

The trusted root stays the installer-embedded Houfeng release minisign public key. `minisign` itself is bootstrapped by downloading an upstream static tarball and verifying a pinned SHA256 embedded in the installer script. This does not add a checksum-only fallback for Houfeng release assets; it only verifies the verifier binary before using it.

No package-manager repository is enabled or modified. The installer writes only `/usr/local/bin/minisign` during dependency recovery, followed by existing agent paths after release verification succeeds.

## Compatibility

Supported installer runtime remains Linux systemd on `amd64` and `arm64`.

Architecture mapping:

- `x86_64|amd64` -> agent asset `amd64`, minisign tarball path `x86_64`.
- `aarch64|arm64` -> agent asset `arm64`, minisign tarball path `aarch64`.

The upstream minisign tarball is statically linked for both supported architectures, avoiding distro glibc/lib dependency differences. If the tarball format changes, the pinned SHA256 or expected path check fails closed.

## Error Handling

The installer remains `set -eu` and uses existing `fail` / `info` helpers. New failure messages must be actionable and must not print enrollment tokens.

Important fail-closed points:

- Unsupported architecture fails before dependency recovery.
- Missing `curl`/`wget` or `sha256sum`/`shasum` fails before dependency recovery because those tools are needed to download and verify `minisign`.
- Missing `tar` fails only if `minisign` recovery is needed.
- SHA256 mismatch fails before extracting or installing.
- Missing expected tarball binary path fails before installing.
- `minisign` command still missing after install fails before release download.
- `minisign -Vm ...` remains unchecked by `|| true`; failure aborts the installer.

## Documentation and UI

Update deployment docs and onboarding drawer text so operators understand:

- The installer verifies signed release checksums.
- If `minisign` is missing, the generated command allows the installer to install the pinned verifier binary.
- Manual users can deny or disable this, but the consequence is that the agent install/upgrade will stop.

## Rollback

Rollback is simple because behavior is isolated to the installer script and command generation. Reverting the change restores the old hard failure on missing `minisign`.

If an operator wants to remove the installed verifier manually, they can remove `/usr/local/bin/minisign`; future generated commands may reinstall it when missing.
