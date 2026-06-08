<!-- TRELLIS:START -->
# Trellis Instructions

These instructions are for AI assistants working in this project.

This project is managed by Trellis. The working knowledge you need lives under `.trellis/`:

- `.trellis/workflow.md` — development phases, when to create tasks, skill routing
- `.trellis/spec/` — package- and layer-scoped coding guidelines (read before writing code in a given layer)
- `.trellis/workspace/` — per-developer journals and session traces
- `.trellis/tasks/` — active and archived tasks (PRDs, research, jsonl context)

If a Trellis command is available on your platform (e.g. `/trellis:finish-work`, `/trellis:continue`), prefer it over manual steps. Not every platform exposes every command.

If you're using Codex or another agent-capable tool, additional project-scoped helpers may live in:
- `.agents/skills/` — reusable Trellis skills
- `.codex/agents/` — optional custom subagents

Managed by Trellis. Edits outside this block are preserved; edits inside may be overwritten by a future `trellis update`.

<!-- TRELLIS:END -->

## Repository Branch Governance

- Do not commit, merge, amend, squash, reset, or otherwise directly modify local `main` or `master`.
- All feature work, bug fixes, documentation changes, and agent implementation work must happen on a non-main branch, either in the primary checkout or in a dedicated git worktree.
- `git worktree` is allowed and encouraged when it reduces branch-switching risk, enables parallel work, keeps unrelated dirty states isolated, or supports long-running verification. Do not use it reflexively for tiny edits, clean single-threaded work, or when a normal feature branch in the current checkout is simpler and safer.
- The default local worktree parent directory is `<project-root>/.worktree/`. Name worktrees after the task or branch, and keep each worktree on its own non-main branch.
- The agent may choose between a normal new branch and a worktree based on task risk, duration, current dirty state, and whether parallel work is active. The choice does not relax main-branch protections.
- Enable the versioned local hooks with `sh scripts/setup-git-hooks.sh` before making changes in the checkout or worktree where commits will be made. The hooks block commits on local `main` / `master` and pushes to remote `main` / `master`.
- Remote `main` / `master` must be protected in the Git host to reject direct pushes and force pushes by everyone. Use pull requests from feature branches instead of pushing to protected branches directly.
- When the task continues through PR delivery, creating the PR is not the finish line: push the selected feature branch, open a pull request, monitor required CI, fix failures on that same branch or worktree, merge only after required checks pass, then monitor post-merge automation such as main CI, Release Please, release jobs, and image publishing when relevant. For release-worthy changes, continue through the release PR and publishing workflow until the Docker image or release artifact is verified, then sync or clean up the chosen working location without directly modifying local or remote `main` outside the protected update path.
