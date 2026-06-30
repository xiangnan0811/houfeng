# Fix incident threshold order validation

## Goal

Close the monitoring incident threshold ordering gap found during the current-branch review. Invalid threshold configurations must be rejected before they can affect incident classification or user-facing monitoring decisions.

## Requirements

- Incident default thresholds with multiple severity levels must be strictly ordered by severity.
- CPU, memory, disk, and inode thresholds must satisfy `warning < alert < critical`.
- IO wait and load 5-minute thresholds must satisfy `warning < critical`.
- Backend settings validation must reject inverted or equal threshold levels so invalid persisted or API-submitted settings cannot enter the system.
- Frontend settings submission must reject the same invalid ordering before sending the update and show a clear Chinese error.
- Existing valid default settings and valid user edits must continue to pass.

## Acceptance Criteria

- [x] Backend tests cover inverted and equal threshold ordering for three-level and two-level incident defaults.
- [x] Frontend tests cover invalid ordering and prove no settings update request is sent.
- [x] Backend settings validation enforces the required strict ordering.
- [x] Frontend settings form enforces the same ordering with actionable Chinese copy.
- [x] Targeted backend and frontend tests pass.
- [x] Full relevant quality checks pass before commit.

## Notes

- Source finding: `.trellis/tasks/archive/2026-06/06-30-current-branch-multi-task-review/research/current-branch-review.md`.
- This task is intentionally scoped to the threshold-order finding; broader lifecycle state changes were handled in earlier tasks on the same branch.
