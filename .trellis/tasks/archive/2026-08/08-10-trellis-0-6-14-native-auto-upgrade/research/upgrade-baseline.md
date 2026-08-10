# Research: Trellis 0.6.14 Upgrade Baseline

- Query: Which Trellis files are genuinely customized, and which operating mode should the upgrade preserve?
- Scope: local project configuration and installed CLI
- Date: 2026-08-10

## Confirmed Baseline

- The project started this task on Trellis `0.6.4`; no global `trellis` executable was installed.
- The selected target is stable `0.6.14`, not `0.7.0-beta.3`.
- The repository was clean at `f6ad0f5d` on a non-main branch before this maintenance task was created.
- VPS detail refactoring remains at 3/11 children complete. Evidence Platform (Child 4) is planning-only and is outside this task.

## Approved Operating Mode

- Keep the bundled `native` workflow and its human review/start/commit gates.
- Set `codex.dispatch_mode: auto` so bounded implement/check/research work can use native Codex subagents.
- Keep `trellis channel` available but optional. It is reserved for durable multi-turn or cross-provider collaboration, not default orchestration.
- Do not automatically advance to the next VPS child.

## Customization Audit

The 0.6.4 dry run reported twelve user-modified template files. Eleven `.agents/skills/trellis-*` files were byte-equivalent to the official 0.6.4 templates and were false positives caused by missing historical hash entries; they can adopt 0.6.14 templates. `.codex/hooks.json` is the only genuine customization.

The hook customization resolves scripts from the parent of `git rev-parse --path-format=absolute --git-common-dir`. This is required because `.codex/` is ignored and lives in the root checkout while feature work may run from linked worktrees. The 0.6.14 merge must preserve that path strategy for both `UserPromptSubmit` and `SubagentStart`.

## Safety Constraints

- Use `trellis update --create-new`; never use `--force`.
- Preserve `.trellis/tasks/`, `.trellis/spec/`, `.trellis/workspace/`, and `.trellis/.developer`.
- Leave changes uncommitted and do not push or create a PR without a later explicit confirmation.
