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
3. Monitor required PR checks until they pass, fail, or are clearly blocked. Creating the PR is not a completion point.
4. Fix failures on the same feature branch and re-run the relevant local checks before waiting for CI again.
5. Merge only after required checks pass.
6. After merge, monitor post-merge automation that is relevant to the change, such as main CI, Release Please, GitHub Release, Docker/image publishing, or deploy jobs.
7. For release-worthy changes, watch the Release Please PR lifecycle as part of the same delivery flow:
   - wait for the release PR to be created or updated;
   - monitor its PR checks;
   - merge it only when checks pass and repository policy allows the agent to merge;
   - monitor the GitHub Release and any image or artifact publishing jobs triggered by that release.
8. Verify the final release artifact before declaring completion. For this repository, `publish-images` publishes `docker.io/linnea7171/houfeng` and its success evidence must include the successful workflow run and image inspection/published tag evidence from that run.
9. Sync or clean up the local working location after merge. Do not directly commit, merge, reset, or push local/remote `main` as a shortcut.

Use concrete GitHub checks instead of assumptions:

```bash
gh pr checks <pr-number> --watch
gh pr view <pr-number> --json state,mergeable,statusCheckRollup
gh run list --branch main --workflow ci --limit 5
gh pr list --head release-please--branches--main --state open
gh run list --workflow publish-images --limit 5
```

If no release PR, GitHub Release, image workflow, or deploy job is expected for the change, record the exact evidence for that conclusion in the final report instead of leaving the status ambiguous.

---

## Post-Release Cleanup

After a release-worthy change has been merged, released, and verified in Docker Hub or other final artifact storage, do not stop with the artifact evidence. Clean the local and remote delivery surface before reporting completion.

Required cleanup checklist:

1. Re-check every checkout/worktree involved in the delivery:

   ```bash
   git status --short --branch
   git worktree list --porcelain
   ```

2. Classify any dirty path before ending the session:
   - Useful follow-up work belongs in a new Trellis task and a new non-main branch.
   - Useless local residue, such as failed experiment edits, generated build output, or a superseded PR fixture change, must be removed before completion.
   - Do not append new follow-up work to a task that has already been completed, archived, released, or used to publish an image.

3. If a PR was replaced by a clean branch, close the superseded PR and delete its local and remote branch after the replacement PR has merged:

   ```bash
   gh pr close <old-pr-number> --comment "<replacement reason>"
   git push origin --delete <old-branch>
   git branch -D <old-branch>
   git fetch origin --prune
   ```

4. Remove temporary worktrees that were only needed for the delivery path:

   ```bash
   git worktree remove .worktree/<task-or-branch-name>
   ```

5. Return the primary checkout to the intended next-task baseline. Usually this is the protected remote `main` after the release PR merge:

   ```bash
   git switch main
   git pull --ff-only origin main
   ```

6. Final completion evidence must include:
   - `git status --short --branch` showing no dirty paths;
   - `git worktree list --porcelain` showing no stale temporary worktrees;
   - branch list evidence that stale replaced branches are gone when branch deletion was part of the cleanup;
   - the final artifact evidence, such as `publish-images` success plus Docker Hub manifest/tag inspection.

Wrong pattern:

```bash
# Wrong: image is published, but local release leftovers remain for the next task.
gh run watch <publish-images-run> --exit-status
# report completion without checking git status/worktrees/branches
```

Correct pattern:

```bash
gh run watch <publish-images-run> --exit-status
git status --short --branch
git worktree list --porcelain
git branch -vv
# report completion only after the workspace is clean and the next-task baseline is clear
```

---

## Remote Protection Requirements

The repository host must protect `main` and `master` when either branch exists:

- reject direct pushes;
- reject force pushes;
- require changes to come from pull requests;
- include maintainers/admins in the restriction when the host supports that setting.

Configuring remote branch protection is an owner or main-session operation. Implementation agents must document the requirement but must not silently change remote repository policy.
