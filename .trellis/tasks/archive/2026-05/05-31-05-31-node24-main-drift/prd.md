# Resolve Node 24 workflow compatibility and local main drift

## Goal

Post-release follow-up: update GitHub Actions workflow compatibility for Node.js 24 deprecation warnings, and audit the local main checkout modifications left behind after the asset lifecycle release so they are either confirmed as released duplicates or safely handled without touching local main.

## Requirements

- Update the release image publishing workflow so it no longer emits GitHub Actions Node.js 20 runtime deprecation annotations for artifact upload/download steps.
- Keep the Houfeng web runtime contract unchanged: CI and `web/package.json` remain on Node 22.x for project dependency compatibility.
- Use official action releases that declare `runs.using: node24` rather than masking the annotation with temporary environment variables.
- Audit the dirty local `/Users/weibo/Code/houfeng` `main` checkout without mutating it.
- Explain whether the six dirty files on local `main` are related to the already released asset lifecycle coordination work, and whether they should be preserved or discarded by the human operator.

## Main Checkout Drift Audit

- Local checkout `/Users/weibo/Code/houfeng` is on `main` at `fa937a0`, while `origin/main` is now `d996ef9` after the asset lifecycle release and Release Please merge.
- The six dirty files on local `main` are:
  - `internal/center/http/handlers/dashboard_test.go`
  - `internal/center/incidents/types.go`
  - `internal/center/store/dashboard.go`
  - `internal/center/store/dashboard_test.go`
  - `internal/center/store/vps_assets.go`
  - `internal/center/store/vps_assets_test.go`
- Those files overlap with the released asset lifecycle coordination commit `5d6539c`, so they are related to the same functional area.
- They are not safe to carry forward as-is: the local diff is a stale partial/alternate implementation compared with the released code. Examples include:
  - local `main` still has `EventNodeLifecycleUpdated`, while the released code removed it;
  - local `main` includes older dashboard query shapes and test counts that differ from the released implementation;
  - local `main` contains older VPS subscription linkage test/query details that the released implementation superseded.
- Conclusion: the local `main` dirty files should be treated as stale release-work residue. Do not merge them into a new branch. After the operator confirms there is no unrelated manual work inside those hunks, the safe cleanup path is to discard only those six local changes or recreate `main` from `origin/main` using a non-destructive backup/stash first.

## Acceptance Criteria

- [ ] `.github/workflows/publish-images.yml` uses official artifact upload/download actions that run on Node 24.
- [ ] Project Node version remains Node 22.x; no web dependency/runtime migration is included in this task.
- [ ] Local verification passes for workflow syntax-sensitive checks and repository quality gates relevant to this YAML-only change.
- [ ] PR CI passes and no Node.js 20 deprecation annotation remains for artifact upload/download steps.
- [ ] The local `main` drift is documented clearly and the task does not mutate `/Users/weibo/Code/houfeng` `main`.

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
