# Local Evidence Workflow Audit

## Current assets

* `docs/operations/v2-visual-evidence.md` defines the active v2 preview, browser sanity, and screenshot evidence workflow.
* `docs/operations/v2-visual-evidence/manifest.md` already has rows for UX-2, UX-3, UX-4, UX-5, UX-6A, UX-6B, and UX-6C.
* Screenshot files referenced by current rows exist under per-task subdirectories such as `ux2-page-body-responsive-hierarchy/`, `ux3-dashboard-command-surface-polish/`, and `ux6c-events-timeline-evidence/`.

## Gap

The workflow has strong human guidance but weak executable guardrails:

* Nothing checks that a manifest row's `File` value exists.
* Nothing checks table shape, supported verdict values, route formatting, or viewport formatting.
* Browser sanity is repeatedly implemented as one-off local scripts during UI tasks.
* Existing project specs require visual evidence for user-visible UI changes, but they do not point to a reusable local helper yet.

## Recommended slice

Implement UX-7B as evidence governance, not page redesign:

* Add a small manifest validator under `scripts/`.
* Add a local-only browser sanity helper under `scripts/`.
* Keep both outside CI and outside `make verify-web`.
* Update `docs/operations/v2-visual-evidence.md`, the UI roadmap, and Trellis web quality guidance so future UI tasks can reuse the same checks.

## Constraints

* Do not add repository e2e dependencies.
* Do not add visual regression CI.
* Existing screenshots remain `Needs review`.
* If the local browser helper depends on system-installed Playwright, document it as local evidence only and fail with a clear install/tooling message when unavailable.

