# Update Trellis to latest

## Goal

Upgrade this repository's Trellis-managed local runtime and AI-facing configuration from `0.6.0-beta.22` to the current npm latest, while preserving project-specific configuration and governance.

## Requirements

- Upgrade the globally installed `@mindfoldhq/trellis` CLI to npm `latest`, currently `0.6.4`.
- Run the project-level Trellis update and migration flow on a non-main branch.
- Preserve local customization outside Trellis-managed blocks, including repository branch governance in `AGENTS.md`.
- Preserve project-owned Trellis data and conventions:
  - `.trellis/config.yaml`
  - `.trellis/spec/`
  - `.trellis/tasks/`
  - `.trellis/workspace/`
- Accept upstream Trellis bundled-skill updates where local files only diverged from previous Trellis templates.
- Migrate the bundled skill typo directory from `trellis-spec-bootstarp` to `trellis-spec-bootstrap`.
- Add new Trellis `0.6.x` local assets installed by `trellis update`, including channel/runtime agent and session-insight files.
- Treat this as configuration/documentation maintenance only. Do not change application code and do not initiate a release.

## Acceptance Criteria

- [ ] `trellis --version` reports `0.6.4`.
- [ ] `.trellis/.version` records `0.6.4`.
- [ ] `trellis update --dry-run` reports the project is already up to date.
- [ ] No `.new` conflict files remain.
- [ ] `.trellis/config.yaml`, `.trellis/spec/`, `.trellis/tasks/`, and `.trellis/workspace/` are not overwritten by the Trellis update itself.
- [ ] Template hash manifest entries match the files currently on disk.
- [ ] `.agents/skills/trellis-spec-bootstrap/SKILL.md` exists and the old typo directory is absent.
- [ ] `python3 -m compileall -q .trellis/scripts` passes.
- [ ] `python3 ./.trellis/scripts/get_context.py` and phase context commands run successfully.
- [ ] `trellis channel --help` and `trellis mem help` run successfully.
- [ ] Work is committed on `chore/update-trellis`, the Trellis task is archived, the session journal is recorded, and a PR is opened.
- [ ] PR checks are monitored and passing before reporting completion. Since this is documentation/configuration maintenance, release and image publishing workflows are out of scope.

## Notes

- This is a lightweight maintenance task; `prd.md` is sufficient.
- The upgrade command created `.trellis/.backup-2026-06-22T14-22-03/`. It is ignored by git and retained for local comparison only.
- Local verification on 2026-06-22:
  - Trellis CLI/project/latest are all `0.6.4`.
  - `trellis update --dry-run` reports "Already up to date".
  - Trellis script compile/context/hash/asset checks pass.
  - `make verify-web` passes: lint, 65 test files / 520 tests, and production build.
  - `make verify` fails in `houfeng/internal/center/store` asset decision repository tests. The same package fails on a clean `origin/main` worktree at commit `6b624db`, so this is a pre-existing baseline failure outside this documentation/configuration task.
