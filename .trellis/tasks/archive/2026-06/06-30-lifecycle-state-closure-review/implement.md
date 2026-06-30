# Lifecycle state closure final review implementation plan

## 1. Planning and Memory

- [x] Create Trellis task and write PRD/design/implement artifacts.
- [x] Persist project-level preauthorization memory for key complex state reviews.
- [x] Start the task after artifacts are ready.

## 2. Evidence Collection

- [x] Inspect `ec00ea3` diff and current HEAD files touched by the previous fix.
- [x] Read archived task review and updated specs.
- [x] Search for stale state values, old labels, old scope usage, and migration-workbench wording.
- [x] Map affected state flows across backend, store, API, frontend, tests, and specs.

## 3. Review Dimensions

- [x] VPS lifecycle / usage / renewal decision matrix.
- [x] Asset scope historical vs archived semantics.
- [x] Subscription renewal mode gift / lottery / bonus / legacy flags.
- [x] Incident inactive convergence for MonitoringInstance and Target.
- [x] UI and browser sanity implications.
- [x] Destructive-change opportunities under “no current users” assumption.

## 4. Verification

- [x] Run targeted or full backend tests relevant to previous changes.
- [x] Run frontend lint/tests/build or justified subset.
- [x] Run browser sanity for affected routes or justify why recent evidence remains sufficient.
- [x] Record command results in the review report.

## 5. Report and Finish

- [x] Write `research/state-closure-final-review.md`.
- [x] If no implementation fix is made, commit review/task/spec-memory artifacts only.
- [ ] Archive task and record journal.
