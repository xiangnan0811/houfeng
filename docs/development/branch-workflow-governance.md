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

- `.githooks/pre-commit` and `.githooks/pre-merge-commit` reject local `main` / `master`, then invoke the root's `scripts/verify-candidate.mjs --staged` with Node while preserving the original cwd/index context.
- `.githooks/pre-rebase` rejects rebasing local `main` / `master`; the resulting candidate is revalidated on push.
- `.githooks/pre-push` passes its complete stdin to `node scripts/verify-candidate.mjs --push`. Every protected remote `refs/heads/main` / `refs/heads/master` update, deletion or force push is rejected before any lane starts, including direct runner invocation.

Enable them once per clone:

```bash
sh scripts/setup-git-hooks.sh
```

The setup script configures:

```bash
git config core.hooksPath .githooks
```

Candidate validation is part of these hooks, not an optional developer-selected subset:

- Commit/merge validates the complete effective index. Unstaged tracked changes and nonignored untracked files block before lanes start; there is no partial-staging snapshot and no automatic stage, stash, cleanup or rollback. Ignored caches/scratch are not candidates.
- Resolve the root, effective index (including relative `GIT_INDEX_FILE`), HEAD OID and symbolic branch from the original invocation context. Detached HEAD, conflicts, sparse checkout, skip-worktree/assume-unchanged entries, gitlinks and unsupported tracked modes are rejected; restore a normal complete checkout rather than hiding files.
- Record the raw NUL-delimited `git ls-files --stage` digest, HEAD and branch. Before/after every lane and at completion, recheck identity and clean-state conditions. Candidate/HEAD/branch changes invalidate checks. Unexpected tracked build/format changes remain available for inspection; hooks never undo or stage them.
- Classification covers the entire candidate from the unique merge-base of `refs/remotes/origin/main` and HEAD, not just this commit's additions. No fetch is performed. Missing baseline, shallow history, no common ancestor or multiple merge-bases selects every lane; Git query errors fail.
- Push parses every `local_ref local_oid remote_ref remote_oid` line before gates. Nondeleted OIDs must peel to the current HEAD commit; old/other commits and tree/blob tags are rejected. Switch to the target branch and push separately; the hook never switches checkout. Index and working tree must match HEAD. Multiple refs to that HEAD are checked once; nonprotected ref deletions need no gates.
- Git may describe a push source as a full object ID or revision expression such as `HEAD~0`; those descriptions are accepted when the supplied OID satisfies the same HEAD boundary. Only destination refs require ref-name syntax.
- Push classifies the actual stdin OID, not a mutable ref name, and reruns applicable gates without persisting/reusing commit success. Native Go/npm/BuildKit caches remain usable.

`scripts/ci-scope.mjs` selects complete applicable lanes; see the [classification table](../../CONTRIBUTING.md#影响范围与轻量门禁). Docs/tool tests and strict release-manifest validation always run, including metadata-only candidates. PostgreSQL selection runs both the three-version catalog matrix and business tests. Unknown paths or undecodable filenames select all heavy lanes. There is no hook environment variable for skipping gates.

Missing Node or any selected lane's prerequisites blocks commit/push. This includes unavailable Docker daemon/buildx, database tools/images or Chromium/system libraries; CI is an independent safeguard, not a fallback for bypassing local validation. `make verify-ci` explicitly runs every lane; `make verify` retains docs/Go/Web only.

Gate subprocesses clear target-changing ambient overrides (`HOUFENG_*`, database/PG, Vite, Node/Go/Make selectors) and Git-local variables/config injection, while candidate Git queries retain the effective index. Go uses `GOTOOLCHAIN=local`, `GOWORK=off`, `GOENV=off`; strict runners create isolated database URLs. PATH, HOME, normal caches and necessary proxy/network settings remain available: this is verification hygiene, **not a malicious-code sandbox**. The caller's environment/index is not rewritten. Untracked `web/.env`, `.env.local`, `.env.test*` or `.env.production*` files, including ignored ones, block Web/browser lanes with path-only diagnostics; their contents are not read or deleted.

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
- retain required `go`, `web`, `web-browser`, `docker-image` and the exact catalog contexts `record-platform-pg16-catalog (postgres:16.0)`, `record-platform-pg16-catalog (postgres:16.6)`, `record-platform-pg16-catalog (postgres:16.12)`; verify the live required set rather than inferring it from workflow YAML.

The catalog matrix must expand on every candidate, including metadata-only release PRs. Each instance first requires successful classification and an exact string `postgres=true|false`. A selected instance retains checkout, Go/Node setup and the strict PostgreSQL runner. An unselected instance explicitly succeeds as not applicable without checkout, toolchain setup or database execution; that is not PostgreSQL acceptance evidence. Required `go` demands catalog success in both modes, while business PostgreSQL must succeed when selected and be skipped when unselected (Go check follows its own selection). Catalog failure, cancellation, skipped or missing results must never produce a green aggregate.

Missing required contexts must be repaired in workflow execution, not by weakening protection, renaming required contexts or using a bypass.

Configuring remote branch protection is an owner or main-session operation. Implementation agents must document the requirement but must not silently change remote repository policy.
