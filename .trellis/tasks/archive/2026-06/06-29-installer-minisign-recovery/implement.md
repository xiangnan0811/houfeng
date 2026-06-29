# Installer minisign dependency recovery implementation plan

## Branch and setup

- Work on `fix/installer-minisign-recovery`.
- Hooks must be enabled with `sh scripts/setup-git-hooks.sh`.
- Do not commit or merge on local `main`.

## Files

- Modify `internal/center/installer/houfeng-agent-install.sh`
  - Add dependency consent flags and usage text.
  - Add upstream minisign constants.
  - Detect downloader/checksum before `minisign` recovery.
  - Add prompt and install helpers.
  - Keep signed manifest verification mandatory.
- Modify `internal/center/installer/embed_test.go`
  - Assert new flags, pinned SHA256, TTY prompt, auto-install path, fail-closed behavior, and signed verification ordering.
  - Update old hard-required minisign assertion.
- Modify `internal/center/http/handlers/monitoring_instance_onboarding.go`
  - Add `--install-missing-deps` to generated commands.
- Modify `internal/center/http/handlers/monitoring_instance_onboarding_test.go`
  - Assert generated commands include the new explicit consent flag.
- Modify `docs/deploy/local-and-systemd.md`
  - Update command shape and installer behavior.
- Modify `docs/operations/fresh-install-smoke-run.md`
  - Update installer behavior notes.
- Modify `web/src/pages/monitoring-detail/MonitoringInstanceOnboardingDrawer.tsx`
  - Update checklist/manual text to reflect signed manifest and missing verifier recovery.

## Ordered checklist

1. Update installer CLI parsing and usage.
   - Add `INSTALL_MISSING_DEPS=""`.
   - Parse `--install-missing-deps` and `--no-install-missing-deps`.
   - Reject both by treating the variable as a single enum.
2. Move downloader and checksum detection before `minisign` check.
3. Add constants:
   - `HOUFENG_MINISIGN_BOOTSTRAP_VERSION="0.12"`
   - `HOUFENG_MINISIGN_BOOTSTRAP_SHA256="9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73"`
   - `HOUFENG_MINISIGN_BOOTSTRAP_URL="https://github.com/jedisct1/minisign/releases/download/0.12/minisign-0.12-linux.tar.gz"`
4. Track `MINISIGN_ARCH` alongside `ASSET_ARCH`.
5. Add helper `ask_yes_no_from_tty`.
   - Return failure when `/dev/tty` is unavailable.
   - Default no on empty answer.
6. Add helper `ensure_minisign`.
   - If command exists, return.
   - Print explanation.
   - Apply consent enum:
     - `yes`: install.
     - `no`: fail.
     - empty + tty yes: install.
     - empty + tty no/no tty: fail.
   - Download tarball, verify SHA256, extract, install expected binary, verify command exists.
7. Call `ensure_minisign` before release asset downloads.
8. Update center-generated command to include `--install-missing-deps`.
9. Update tests.
10. Update docs and onboarding text.
11. Run focused tests:
    - `go test ./internal/center/installer ./internal/center/http/handlers`
    - `cd web && npm run test -- --run src/lib/api.test.ts` only if TypeScript fixture expectations are affected.
12. Run full backend verification:
    - `make verify-go`
13. Run frontend verification if web changed:
    - `make verify-web`
14. Review diffs for token leakage, checksum-only fallback, and accidental main-branch work.

## Review gates

- The installer must never proceed to release asset install without `minisign -Vm` success.
- The prompt must never read from stdin because stdin may contain the enrollment token.
- Generated command must remain safe for command history/process list: token stays in heredoc stdin, not argv.
- User-facing text must be concise and actionable, not a wall of explanation.

## Validation evidence to collect

- Test output from focused Go tests.
- `make verify-go` output.
- `make verify-web` output if web text or tests changed.
- `git diff --check`.
- `git status --short --branch`.
