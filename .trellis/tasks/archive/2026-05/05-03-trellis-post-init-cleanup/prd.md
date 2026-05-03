# Trellis post-init cleanup

## Goal

Finish the immediate cleanup after Trellis initialization so the repository's
Trellis metadata does not mislead future agents. The post-init state should
accurately show that backend/web specs are populated, avoid template-hash noise
from runtime cache files, and leave a normal Trellis task record for this work.

## What I already know

- `trellis init` has already completed the bootstrap guideline task:
  `.trellis/tasks/archive/2026-05/00-bootstrap-guidelines/task.json` is
  `completed`.
- `.trellis/spec/backend/*.md` contains project-specific Chinese guidelines
  with real file paths and conventions.
- `.trellis/spec/backend/index.md` still has the generated placeholder copy:
  English overview text, `To fill` statuses, and a language note saying docs
  should be English.
- `.trellis/spec/web/index.md` has already been localized and marks all web
  guides as filled.
- Git currently shows only Trellis initialization residue:
  `.trellis/.template-hashes.json` modified and root `.agents/` untracked.
- `.trellis/.template-hashes.json` picked up Python `__pycache__` entries under
  `.trellis/scripts/common/__pycache__/`, even though `.trellis/.gitignore`
  ignores `**/__pycache__/` and `**/*.pyc`.
- Root `.codex/` files exist but are intentionally ignored by the repo's current
  `.gitignore`; this task will not change that policy.

## Requirements

- Update `.trellis/spec/backend/index.md` so it matches the actual populated
  backend spec files.
- Keep backend index language and tone aligned with the existing backend spec
  files and web index: Chinese prose, real project context, and filled statuses.
- Remove Python cache entries from `.trellis/.template-hashes.json` so template
  state tracks managed templates, not runtime artifacts.
- Do not edit business code.
- Do not change the root `.gitignore` policy for `.codex/`.
- Keep root `.agents/` as an untracked initialization artifact for the user to
  review/commit after this task.

## Acceptance Criteria

- [x] `.trellis/spec/backend/index.md` no longer contains template placeholders
  such as `To fill` or "All documentation should be written in English".
- [x] Backend index marks all existing backend guide files as filled.
- [x] `.trellis/.template-hashes.json` is valid JSON after cleanup.
- [x] `.trellis/.template-hashes.json` no longer contains `__pycache__` or
  `.pyc` paths.
- [x] `git status --short` shows only expected Trellis initialization/task
  changes and the existing untracked `.agents/` directory.

## Definition of Done

- Run a targeted validation for JSON validity and placeholder removal.
- Run `python3 ./.trellis/scripts/task.py current --source` to confirm the task
  lifecycle is coherent.
- Report which files changed and which initialization artifacts remain for
  commit review.

## Out of Scope

- No product feature work.
- No backend/frontend code changes.
- No global Trellis package or npm cache changes.
- No changes to ignored `.codex/` platform files unless the user separately
  decides those should be versioned.

## Technical Notes

- Trellis local architecture docs identify `.trellis/.template-hashes.json` as
  template-management state and `.trellis/spec/` as the project-specific source
  of truth for agent coding conventions.
- Trellis install/first-task documentation describes first-time init as creating
  a bootstrap task to populate project guidelines; this repository's bootstrap
  task is already archived.
- This task is a narrow post-init consistency cleanup, not a new implementation
  milestone.

## Verification

- `python3 -m json.tool .trellis/.template-hashes.json >/dev/null`
- `rg -n "To fill|All documentation should be written in English|__pycache__|\\.pyc" .trellis/spec/backend/index.md .trellis/.template-hashes.json || true`
- `python3 ./.trellis/scripts/task.py current --source`
