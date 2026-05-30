# update local and project trellis

## Goal

Update the local Trellis CLI/runtime, then refresh this repository's Trellis-managed project files so Houfeng uses the current Trellis behavior. The user is willing to try the beta release if beta exposes newer functionality and is installable in the local environment.

## What I already know

* Repository root is `/Users/weibo/Code/houfeng`.
* Repository governance requires all changes on a non-main branch and local hooks enabled through `sh scripts/setup-git-hooks.sh`.
* Local hooks are enabled with `core.hooksPath=.githooks`.
* The work branch is `worktree/update-local-trellis`; Git rejected `.worktree/update-local-trellis` because branch path components cannot start with `.`.
* No active Trellis task existed before this work.
* Local `trellis` binary is resolved from the fnm Node v24.14.1 global prefix.
* Existing project Trellis version is `.trellis/.version = 0.5.7`.
* Existing global package is `@mindfoldhq/trellis@0.5.15`.
* npm registry dist-tags observed on 2026-05-30: `latest = 0.5.19`, `beta = 0.6.0-beta.21`, `rc = 0.5.0-rc.7`.
* On 2026-05-30, the user requested updating workflow constraints as part of this same Trellis upgrade task: `git worktree` should no longer be forbidden, the default worktree parent should be `<project-root>/.worktree/`, agents should decide between worktree and normal branch by task context, and main-branch commit/push protections remain mandatory.

## Assumptions

* Installing `@mindfoldhq/trellis@beta` globally is acceptable because the user explicitly said beta is acceptable if it has new functionality.
* Project-level updates should be produced by `trellis update` where possible, then inspected and reconciled instead of manually overwriting local customizations.
* Existing Houfeng customizations in AGENTS.md, `.trellis/spec/`, `.agents/skills/`, `.codex/agents/`, `.codex/hooks*`, and `.codex/config.toml` should be preserved unless the new Trellis templates require a compatible update.
* Workflow-policy changes that affect branch/worktree/PR delivery belong in this task because they are Trellis governance, even though they do not change product logic.

## Requirements

* Update the local Trellis CLI/runtime to the most useful current release, preferring beta if it is newer and functional.
* Update this repository's Trellis-managed files with the upgraded CLI.
* Preserve project-specific instructions, specs, task history, workspace journals, and local customizations.
* Inspect generated changes before finalizing and avoid silently accepting destructive overwrites.
* Run enough validation to prove the updated Trellis project files still load context and manage the task lifecycle.
* Record version evidence and any beta-specific observations.
* Explain the practical differences between project Trellis 0.5.7 and 0.6.0-beta.21 before committing.
* Replace the old "no worktree" policy with a context-sensitive branch/worktree policy.
* Add `.worktree/` as the default ignored local worktree directory while preserving existing `.worktrees/` compatibility.
* Preserve main/master protections for local commits, merges, rebases, pushes, and remote branch protection.
* Update the PR / merge / post-merge monitoring guidance so it works whether the task used a normal feature branch or a worktree-backed branch.
* Record the user's updated worktree/PR workflow preference into Codex memory through an ad hoc note.

## Acceptance Criteria

* [ ] `trellis --version` reports the selected upgraded version.
* [ ] `.trellis/.version` matches the selected project Trellis template version after project update.
* [ ] `python3 ./.trellis/scripts/get_context.py`, `--mode phase`, and `--mode packages` run successfully after update.
* [ ] Existing project guidance under AGENTS.md and relevant `.trellis/spec/` files remains present.
* [ ] Git diff is reviewed for unexpected deletions or broad unrelated churn.
* [ ] Final report states what version was installed, what project files changed, and any validation limitations.
* [ ] AGENTS.md and branch workflow guidance allow worktrees with `.worktree/` as the default parent directory.
* [ ] AGENTS.md and branch workflow guidance still forbid direct local or remote main/master modification.
* [ ] The PR lifecycle guidance explicitly covers pushing the feature branch, monitoring CI, merging only when green, and monitoring relevant post-merge automation.
* [ ] `.gitignore` ignores `.worktree/`.

## Definition of Done

* Hooks enabled.
* Work performed on a non-main branch.
* Local Trellis updated.
* Project Trellis update applied and inspected.
* Trellis context commands pass.
* Any necessary docs/spec notes are updated or explicitly judged unnecessary.
* Worktree/branch governance updated and validated.

## Out of Scope

* Changing Houfeng product behavior.
* Rewriting project specs unrelated to Trellis update compatibility or branch/worktree workflow governance.
* Committing, pushing, merging, or opening a PR unless the user asks later.

## Technical Notes

* Use the `trellis-meta` skill because this changes `.trellis/`, platform hooks/config, and agent/skill scaffolding.
* Use `trellis update --dry-run` before applying project updates.
* Prefer `trellis update --migrate` if the upgraded CLI reports pending migrations.
* Use `trellis update --skip-all` or `.new` copies if the updater would overwrite local customizations without a clear merge path.
