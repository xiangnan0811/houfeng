# Branch Workflow Governance

> Purpose: keep local and remote main branches stable so all implementation work is reviewable and recoverable.

---

## Policy

- Local `main` and `master` are read-only for development work. Do not commit, merge, amend, squash, reset, or otherwise directly modify them.
- Start every feature, bug fix, documentation update, and agent implementation task on a new non-main branch.
- Do not use `git worktree` for this repository workflow. Use the single checkout and switch branches normally.
- Remote `main` and `master` must be protected by the Git host to reject direct pushes and force pushes by everyone.
- Pull requests from feature branches are the normal path for landing changes.

---

## Local Hook Contract

The repository ships versioned hooks in `.githooks/`:

- `.githooks/pre-commit` rejects commits while the current branch is `main` or `master`.
- `.githooks/pre-merge-commit` rejects direct merge commits while the current branch is `main` or `master`.
- `.githooks/pre-rebase` rejects rebasing local `main` or `master`.
- `.githooks/pre-push` rejects any push whose remote ref is `refs/heads/main` or `refs/heads/master`, including delete and force-push attempts.

Enable them once per clone:

```bash
sh scripts/setup-git-hooks.sh
```

The setup script configures:

```bash
git config core.hooksPath .githooks
```

Hooks are local guardrails, not a replacement for remote branch protection. Git does not expose a reliable hook for every destructive local operation, so do not use commands such as direct resets on local `main` / `master` even though the hook set focuses on commit, merge, rebase, and push paths. Do not bypass hooks with `--no-verify` to land ordinary work.

---

## Required Agent Workflow

Before making code or documentation changes:

1. Check the current branch:

   ```bash
   git branch --show-current
   ```

2. If the current branch is `main` or `master`, create a new branch before editing:

   ```bash
   git switch -c <type>/<short-description>
   ```

3. Keep all changes for the task on that feature branch.
4. Do not create a worktree as a shortcut around branch state.
5. Push only the feature branch and open a pull request.

When syncing with the remote default branch, fetch first and only update local `main` / `master` from the protected remote branch. Do not merge feature work into local `main` / `master`.

---

## Remote Protection Requirements

The repository host must protect `main` and `master` when either branch exists:

- reject direct pushes;
- reject force pushes;
- require changes to come from pull requests;
- include maintainers/admins in the restriction when the host supports that setting.

Configuring remote branch protection is an owner or main-session operation. Implementation agents must document the requirement but must not silently change remote repository policy.
