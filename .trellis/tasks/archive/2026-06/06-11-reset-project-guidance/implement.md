# Implementation Plan

## Pre-implementation

- [x] Confirm work is on non-main branch `docs/reset-project-guidance`.
- [x] Enable repository hooks with `sh scripts/setup-git-hooks.sh`.
- [x] Read Trellis workflow, branch governance, shared guide index, backend/web spec indexes, and relevant current docs.
- [x] Create this Trellis task.

## Planning Review

- [x] Review `prd.md` and `design.md` scope before starting implementation.
- [x] Start the task with `python3 ./.trellis/scripts/task.py start 06-11-reset-project-guidance` after approval to implement.

## Documentation Changes

- [x] Rename active operation workflow docs with `git mv`:
  - `docs/operations/v1-smoke-run.md` -> `docs/operations/fresh-install-smoke-run.md`
  - `docs/operations/v2-visual-evidence.md` -> `docs/operations/ui-preview-and-browser-sanity.md`
- [x] Add/curate non-versioned current design guidance under `docs/design/current/`.
- [x] Update `README.md` and `docs/README.md` to point to current docs and mark V1/V2 folders as historical/reference only.
- [x] Update historical design folder READMEs/front matter to say they are not active authority.
- [x] Update former V2 design docs or redirects so they no longer present themselves as current hard authority.
- [x] Update cross-references in maintained operation/deploy docs.
- [x] Update `.trellis/spec/backend/*.md` and `.trellis/spec/web/*.md` authority headers and references to current guidance.
- [x] Preserve safety and evidence guidance in specs while removing stale version/freeze language.

## Verification

- [x] `git diff --check`
- [x] `rg -n "frozen and authoritative|视觉权威|Visual authority|current visual authority|active .*v2|V1 结构层冻结|v1 到此为止|v2-houfeng|v1-baseline|v1-smoke-run|v2-visual-evidence" README.md docs .trellis/spec --glob '!docs/design/v1-baseline/**' --glob '!docs/design/v2-houfeng/**'`
- [x] `rg -n "docs/operations/v1-smoke-run.md|docs/operations/v2-visual-evidence.md|operations/v1-smoke-run.md|operations/v2-visual-evidence.md" README.md docs .trellis/spec`
- [x] Link/path spot check for all renamed docs.
- [x] Run any available docs-related verification if present; otherwise record that this is documentation-only and no doc build exists.
