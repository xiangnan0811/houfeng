# Record release cleanup guidance

## Goal

Capture the release-flow cleanup lesson from the `v0.54.3` publishing session so future release-worthy tasks do not leave stale worktrees, replacement branches, or uncommitted local edits behind after Docker Hub verification succeeds.

## Requirements

- Update project guidance in `.trellis/spec/` rather than editing the already archived security-remediation task.
- Document that release completion is not only "image pushed": the selected checkout/worktree must be returned to a clean, unambiguous state for the next task.
- Cover replaced-PR branches, temporary worktrees, local/remote branch cleanup, syncing local `main` after protected merge/release, and final `git status` / `git worktree list` checks.
- State that any new follow-up work discovered after image publication must start as a new Trellis task, not be appended to the completed/archived release task.

## Acceptance Criteria

- [x] Branch/release governance guide includes a concrete post-release cleanup checklist.
- [x] Guidance distinguishes useful follow-up work from useless local residue.
- [x] Guidance requires final evidence that the workspace is clean and on the intended branch before reporting completion.
- [x] No production code changes are made.

## Notes

- Lightweight documentation/spec task; PRD-only is sufficient.
