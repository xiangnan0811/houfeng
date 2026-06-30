# Current branch multi-task review

## Goal

Review the full current branch against `origin/main` after multiple lifecycle-state Trellis tasks, without making code changes, and report any correctness, consistency, migration, UX, test, or process issues that remain.

## Requirements

- Treat this as a review-only task: inspect and verify, but do not implement fixes unless the user explicitly redirects.
- Cover the complete branch diff, not only the latest commit.
- Pay special attention to VPS lifecycle and usage states, subscription renewal modes, import/export behavior, monitoring and archival semantics, frontend affordances, database constraints, tests, Trellis task artifacts, and project specs.
- Evaluate destructive or compatibility-breaking behavior explicitly.
- Use code-review output style: findings first with file/line references, then residual risks and verification notes.

## Acceptance Criteria

- [x] Branch base and commit range are identified.
- [x] Diff is reviewed by affected subsystem and task history.
- [x] Any blocking, important, or minor issues are reported with concrete file/line references and feasible fixes.
- [x] If no issues are found, the report says so clearly and names remaining risks or test gaps.
- [x] Review result is recorded in the Trellis task before wrap-up.

## Notes

- This task is lightweight and PRD-only because it does not authorize implementation.
