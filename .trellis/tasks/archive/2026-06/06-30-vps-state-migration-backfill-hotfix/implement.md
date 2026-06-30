# Implementation Plan

1. Create and work on a non-main hotfix branch.
2. Add a failing migration test proving 0049 must normalize existing conflicting
   state combinations before adding the check constraint.
3. Update 0049 with idempotent normalization statements before the constraint.
4. Run the focused test and then `make verify-go`.
5. Review the diff for data safety and project database guidelines.
6. Commit, push, open PR, monitor PR checks, merge, monitor main CI, Release
   Please, GitHub Release, image publishing, and cleanup.

## Rollback Point

Before merge, the branch can be abandoned. After release, rollback means
publishing a newer fix because the old `v0.55.6` image cannot bootstrap on
databases containing affected rows.
