# Design

## Reset Strategy

This task should change the maintained documentation architecture, not just edit wording in place.

The current problem is that versioned design folders and operation filenames have become agent-facing authority. The reset should introduce current, non-versioned entry points and demote the old V1/V2 material to historical/reference status:

- Keep useful operator workflows under `docs/operations/`, but rename active V1/V2 workflow files to current purpose names.
- Keep useful design guidance, but expose it through current/living names rather than `v1-baseline` / `v2-houfeng` as the active authority.
- Keep old versioned design files only as history/reference, with clear notes that they are not a freeze on future product direction.
- Update `.trellis/spec/` so future agents follow current code and current living guidance first.

## Documentation Boundaries

Current public docs:

- `README.md`
- `docs/README.md`
- current files under `docs/operations/`
- current design guidance under `docs/design/`

Agent-facing docs:

- `.trellis/spec/backend/*.md`
- `.trellis/spec/web/*.md`
- shared `.trellis/spec/guides/*.md` when relevant

Historical material:

- archived Trellis tasks under `.trellis/tasks/archive/`
- original V1 design prose that is useful as traceability but should not control future direction

Archived tasks should not be rewritten. Current specs may mention archives only as historical evidence.

## Proposed Structure

Create non-versioned current design entry points:

- `docs/design/current/README.md`
- `docs/design/current/product-and-architecture.md`
- `docs/design/current/interface-language.md`
- `docs/design/current/component-patterns.md`

These files can initially be curated from the useful parts of the former V1/V2 docs instead of inventing a new product plan. The tone should be "current guidance" and "default unless current task evidence justifies changing it", not "frozen contract".

Keep versioned design folders for traceability:

- `docs/design/v1-baseline/` remains historical.
- `docs/design/v2-houfeng/` remains historical/source material or can redirect readers to `docs/design/current/`.

Rename active operation docs if practical:

- `docs/operations/v1-smoke-run.md` -> `docs/operations/fresh-install-smoke-run.md`
- `docs/operations/v2-visual-evidence.md` -> `docs/operations/ui-preview-and-browser-sanity.md`

Use `git mv` for these moves so review history is preserved.

## Language Rules

Keep hard words only for real invariants:

- security and privacy;
- token/credential handling;
- database migration and transaction integrity;
- evidence honesty;
- current API contracts that code actually implements;
- dangerous lifecycle actions.

Soften historical design conclusions:

- "frozen", "authoritative", "must not add", "V1 stops here", and similar wording should become historical context or current default guidance.
- UI page descriptions should say what the current implementation and desired direction are, while allowing future tasks to change them through evidence and updated docs.

## Compatibility

Renaming docs requires updating references in:

- README/docs indexes;
- `.trellis/spec/` quality and styling references;
- deployment and validation docs that link to smoke/browser-sanity workflows.

The codebase should not need functional changes. If scripts or tests hard-code old doc paths, update them as documentation references only.

## Validation

Use focused repository searches as the primary proof:

- no maintained docs present `v1-baseline` as frozen authoritative;
- no maintained docs present `v2-houfeng` as current visual authority;
- old operation filenames are not referenced except in history or compatibility notes;
- current safety/evidence docs remain discoverable from README and `docs/README.md`.

If doc tooling exists, run it. Otherwise run `rg` checks and a lightweight `git diff --check`.
