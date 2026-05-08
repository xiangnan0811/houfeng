# Check and update Trellis integration

## Goal

Confirm whether the project Trellis integration is behind the official Trellis release track and, if so, update it using the official documented sequence while preserving local Houfeng customizations.

## What I already know

- User authorized running upgrade/update commands when needed.
- User allowed using the Trellis workflow for this task.
- Current Codex session is bound to `.trellis/tasks/05-08-trellis-update-check`.
- The project already has Codex hooks, Trellis skills, Trellis agents, workflow, specs, tasks, and workspace files.
- Official docs describe a two-layer update model: upgrade the global CLI package first, then run `trellis update` in the project.

## Assumptions

- Use the official npm package `@mindfoldhq/trellis`.
- Prefer preview/dry-run before applying project template changes.
- Preserve project-local customizations and unrelated dirty files.

## Requirements

- Check current local Trellis CLI version and project `.trellis/.version`.
- Check current official npm dist-tags / latest available version.
- Consult official Trellis docs for the update sequence and migration safety behavior.
- If an update is available, upgrade the global CLI through npm.
- Run project update preview before applying.
- Apply project update only after understanding the dry-run output.
- Validate Trellis still loads in Codex after update.
- Record changed files and any manual follow-up needed.

## Acceptance Criteria

- [x] Local CLI version is compared against official npm version information.
- [x] Project Trellis version is checked before and after update.
- [x] Project update uses the official `trellis update` flow, with `--migrate` if required.
- [x] Codex UserPromptSubmit hook still outputs valid Trellis context.
- [x] No unrelated dirty user files are modified or committed.

## Verification

- `npm view @mindfoldhq/trellis version dist-tags --json` showed `latest = 0.5.7`.
- Before update: `trellis --version` and `.trellis/.version` were both `0.5.0-rc.1`.
- Ran `npm install -g @mindfoldhq/trellis@latest`; after update, global CLI is `0.5.7`.
- Ran `trellis update --dry-run`; it reported project upgrade `0.5.0-rc.1 -> 0.5.7`.
- Ran `trellis update`; it completed and created `.trellis/.backup-2026-05-08T09-23-56/`.
- Ran `trellis update --migrate --dry-run`; it reported already up to date, so no extra migration step was needed.
- After update: `trellis --version` and `.trellis/.version` are both `0.5.7`.
- Ran `python3 -m py_compile .codex/hooks/inject-workflow-state.py .trellis/scripts/task.py .trellis/scripts/get_context.py .trellis/scripts/common/*.py`.
- Ran `printf ... | python3 .codex/hooks/inject-workflow-state.py`; it emitted valid `UserPromptSubmit` hook JSON for this task.
- Ran `codex features list`; after removing the stale user-level `agents.max_threads` setting, Codex config parses and reports `multi_agent_v2 = true`.
- Ran `trellis update --dry-run`; it reports already up to date.
- Ran `git diff --check`; it passed with no output.

## Out of Scope

- Changing Houfeng application behavior.
- Rewriting project specs unrelated to Trellis update compatibility.
- Pushing to remote.

## Technical Notes

- Official docs consulted:
  - `https://docs.trytrellis.app/guides/faq`
  - `https://docs.trytrellis.app/advanced/appendix-f`
  - `https://docs.trytrellis.app/start/install-and-first-task`
- Local meta guidance consulted:
  - `.agents/skills/trellis-meta/SKILL.md`
  - `.agents/skills/trellis-meta/references/local-architecture/overview.md`
  - `.agents/skills/trellis-meta/references/local-architecture/generated-files.md`
