# Branch Workflow Governance

> Purpose: keep local and remote main branches stable so all implementation work is reviewable and recoverable.

---

## Policy

- Local `main` and `master` are read-only for development work. Do not commit, merge, amend, squash, reset, or otherwise directly modify them.
- Start every feature, bug fix, documentation update, and agent implementation task on a non-main branch.
- The agent may choose either a normal branch in the current checkout or a dedicated `git worktree`, based on task risk, duration, dirty state, and whether parallel work is active.
- `git worktree` is allowed and encouraged when it reduces branch-switching risk, isolates unrelated dirty states, enables parallel tasks, or supports long-running verification.
- Do not use `git worktree` reflexively for tiny edits, clean single-threaded work, or when a normal feature branch is simpler and safer.
- The default local worktree parent directory is `<project-root>/.worktree/`; explicit user paths take precedence. Each worktree must have its own non-main branch. Worktrees do not isolate user configuration, memory, databases or shared Git configuration.
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
   - If using a worktree, create it under `.worktree/` or the explicitly authorized location and continue from that path.

4. If the current branch is `main` or `master` and you are staying in the current checkout, create a new branch before editing:

   ```bash
   git switch -c <type>/<short-description>
   ```

5. Enable hooks in the checkout/worktree where commits will happen:

   ```bash
   sh scripts/setup-git-hooks.sh
   ```

6. Keep all changes for the task on the selected feature branch.
7. Only when delivery is authorized, push the feature branch and open a pull request; implementation alone does not authorize either action.

When syncing with the remote default branch, fetch first and only update local `main` / `master` from the protected remote branch. Do not merge feature work into local `main` / `master`.

---

## PR And Post-Merge Workflow

When the user asks to continue through delivery, or task requirements include PR delivery:

1. Push the selected feature branch from the current checkout or worktree.
2. Open a pull request targeting the protected base branch.
3. Monitor required PR checks until they pass, fail, or are clearly blocked. Creating the PR is not a completion point.
4. Fix failures on the same feature branch and re-run the relevant local checks before waiting for CI again.
5. Merge only with merge authorization and after required checks pass. PR delivery alone does not authorize merge, release or deployment.
6. After merge, monitor post-merge automation that is relevant to the change, such as main CI, Release Please, GitHub Release, Docker/image publishing, or deploy jobs.
7. For release-worthy changes, watch the Release Please PR lifecycle as part of the same delivery flow:
   - wait for the release PR to be created or updated;
   - monitor its PR checks;
   - merge it only with explicit authorization covering release-PR merge and publication, after required checks pass and repository policy permits it; green checks alone never authorize release;
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

> **Release Please gotcha:** this repository can create or update a patch release PR for a `docs(...)` commit. A green release PR is not by itself authorization or a reason to publish. Inspect its body and changed files first; when the post-merge diff is documentation/process only and no product release is intended, leave the release PR unmerged and record its URL, state, latest published tag, and why it is not release-worthy. Do not close it reflexively, because a later release-worthy commit may update the same rolling PR.

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
   - Useful follow-up work remains on an appropriate non-main branch within its own authorization; no task directory is required.
   - Remove only this session's disposable residue. Preserve other work, unmerged candidates and rollback backups; never use broad reset/clean/stash to hide dirty state.
   - Completed/cancelled work is not an active recovery target.

3. If a PR was replaced by a clean branch, close the superseded PR and delete its branches only after verifying the replacement merged, no useful work remains, and cleanup is authorized:

   ```bash
   gh pr close <old-pr-number> --comment "<replacement reason>"
   git push origin --delete <old-branch>
   git branch -D <old-branch>
   git fetch origin --prune
   ```

4. Remove only disposable worktrees created for this delivery, after confirming no unmerged candidate or useful dirty content remains:

   ```bash
   git worktree remove .worktree/<task-or-branch-name>
   ```

5. Only when checkout synchronization is authorized and will not disrupt other work, return it to the protected remote baseline. Do not switch another active checkout automatically:

   ```bash
   git switch main
   git pull --ff-only origin main
   ```

6. Final completion evidence must include:
   - `git status --short --branch` with paths classified: this delivery's disposable residue is gone and unrelated dirty/untracked work is preserved; require a fully clean status only when no unrelated work was present;
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
# report completion after classifying/preserving unrelated work and confirming the delivery baseline
```

---

## Remote Protection Requirements

The repository host must protect `main` and `master` when either branch exists:

- reject direct pushes;
- reject force pushes;
- require changes to come from pull requests;
- include maintainers/admins in the restriction when the host supports that setting.

Configuring remote branch protection is an owner or main-session operation. Implementation agents must document the requirement but must not silently change remote repository policy.
