# Branch Workflow Governance

> Purpose: keep local and remote main branches stable so all implementation work is reviewable and recoverable.

---

## Policy

- Local `main` and `master` are read-only for development work. Do not commit, merge, amend, squash, reset, or otherwise directly modify them.
- Start every feature, bug fix, documentation update, and agent implementation task on a non-main branch.
- The agent may choose either a normal branch in the current checkout or a dedicated `git worktree`, based on task risk, duration, dirty state, and whether parallel work is active.
- `git worktree` is allowed and encouraged when it reduces branch-switching risk, isolates unrelated dirty states, enables parallel tasks, or supports long-running verification.
- Do not use `git worktree` reflexively for tiny edits, clean single-threaded work, or when a normal feature branch is simpler and safer.
- The default local worktree parent directory is `<project-root>/.worktree/`; each worktree must have its own non-main branch.
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

## Checkout Choice

Before making code or documentation changes, decide whether to use the current checkout or a dedicated worktree.

Use the current checkout when:

- The working tree is clean and the task is small or single-threaded.
- Switching to a new branch will not disrupt other local work.
- The task does not require long-running commands that should remain isolated.

Use a dedicated worktree when:

- The current checkout has unrelated dirty files or another task in progress.
- Multiple agents or humans may work in parallel.
- The task is broad, risky, or expected to run long verification.
- You need to keep the primary checkout stable while testing a branch.

Default worktree layout:

```bash
mkdir -p .worktree
git worktree add .worktree/<task-or-branch-name> -b <type>/<short-description>
```

If the branch already exists, use the existing-branch form intentionally:

```bash
git worktree add .worktree/<task-or-branch-name> <existing-branch>
```

Do not create a worktree on `main` or `master` for implementation work. If you use a worktree, run setup and verification commands from inside that worktree when they affect the working copy, hooks, build cache, or dependencies.

## Required Agent Workflow

Before changing files:

1. Check the current branch:

   ```bash
   git branch --show-current
   ```

2. Inspect the dirty state:

   ```bash
   git status --short
   ```

3. Choose the working location:
   - If staying in the current checkout, create or switch to a non-main branch before editing.
   - If using a worktree, create it under `.worktree/` and continue the task from that worktree path.

4. If the current branch is `main` or `master` and you are staying in the current checkout, create a new branch before editing:

   ```bash
   git switch -c <type>/<short-description>
   ```

5. Enable hooks in the checkout/worktree where commits will happen:

   ```bash
   sh scripts/setup-git-hooks.sh
   ```

6. Keep all changes for the task on the selected feature branch.
7. Push only the feature branch and open a pull request.

When syncing with the remote default branch, fetch first and only update local `main` / `master` from the protected remote branch. Do not merge feature work into local `main` / `master`.

---

## PR And Post-Merge Workflow

When the user asks to continue through delivery, or task requirements include PR delivery:

1. Push the selected feature branch from the current checkout or worktree.
2. Open a pull request targeting the protected base branch.
3. Monitor required PR checks until they pass, fail, or are clearly blocked.
4. Fix failures on the same feature branch and re-run the relevant local checks before waiting for CI again.
5. Merge only after required checks pass.
6. After merge, monitor post-merge automation that is relevant to the change, such as main CI, Release Please, GitHub Release, Docker/image publishing, or deploy jobs.
7. Sync or clean up the local working location after merge. Do not directly commit, merge, reset, or push local/remote `main` as a shortcut.

If no release or post-merge automation is expected, record that explicitly in the final report instead of leaving the status ambiguous.

---

## Remote Protection Requirements

The repository host must protect `main` and `master` when either branch exists:

- reject direct pushes;
- reject force pushes;
- require changes to come from pull requests;
- include maintainers/admins in the restriction when the host supports that setting.

Configuring remote branch protection is an owner or main-session operation. Implementation agents must document the requirement but must not silently change remote repository policy.
