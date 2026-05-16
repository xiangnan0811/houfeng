# Documentation refresh after agent install

## Goal

Refresh Houfeng's public and internal documentation so it accurately reflects the current open-source project state after recent agent/onboarding work, while removing or consolidating stale process documents and relaxing unnecessary early-stage constraints that could block future development.

## What I already know

* The project has advanced through multiple iterations, including a newly merged one-command agent install flow.
* README.md and docs/ have not been comprehensively updated to match current implementation and open-source positioning.
* The user wants outdated/process documents distilled for useful information, then deleted where appropriate.
* The user wants a clearer documentation structure, with public help/guides refined and development-only documents removed from open-source-facing docs.
* The project is still early-stage; docs should avoid over-strict, unnecessary constraints that make future development feel frozen.
* Documentation must be true to the current repo and must not invent features, claims, or guarantees.

## Assumptions (temporary)

* README.md should become the primary public entry point for what Houfeng is, how to run it, and where to find deeper docs.
* docs/ should distinguish public user/operator docs from historical/internal planning artifacts.
* Frozen baseline design history may still be useful as traceability only when it remains a current factual reference; completed process/evidence documents should not remain in the tracked public docs tree.
* Large-scale docs deletion should preserve useful durable facts in current docs, then delete stale/process originals from the remote repository instead of moving them under `docs/_archive/`.
* Visual evidence directories and bulk screenshots are process artifacts and should be deleted from the tracked repository; future images are ignored by default unless explicitly selected for README/public presentation.

## Scope Decision

Default MVP audience boundary: public user/operator first. README.md, a top-level docs index, deployment/quickstart/operator guidance, and factual current-state notes should become clean and trustworthy. Contributor/developer/process material should only be retained when it is useful to open-source readers; otherwise it should be distilled into current docs and deleted from the tracked repository. A full contributor handbook rewrite is out of scope for this pass.

## Inspection Findings

* README.md still presents the repository as a frozen V1 implementation repo and over-emphasizes internal delivery rules instead of open-source user/operator entry points.
* `docs/` mixes deploy guides, design baselines, operational evidence, release plans, screenshots, sample data, and audit/process documents at the same level, making current guidance hard to find.
* `docs/operations/v2-visual-evidence/` and committed screenshot sets are still process/evidence artifacts; they should not remain in the remote repository unless individual images are explicitly chosen later for README/public display.
* `docs/deploy/local-and-systemd.md` already contains the newest one-command agent install flow and should be the factual source for operator install guidance.
* `docs/operations/v1-smoke-run.md` still centers the older manual enrollment-token flow and needs to reflect the current onboarding/install behavior.
* Release/process docs contain useful context, but several are historical, already completed, or too process-heavy for the open-source repository; useful durable facts should be copied into current docs, then originals deleted from tracked docs rather than archived in-place.
* Real-data validation material contains important security/data handling constraints and should not be converted into claims that the project has completed real-world validation.

## Requirements

* Audit README.md and docs/ against current code, deployment flow, and recent agent onboarding behavior.
* Rewrite README.md as the public entry point for what Houfeng is, current status, quick start, components, and documentation map.
* Add or update a docs index so current public guidance is easy to find and historical/process material is not on the primary path.
* Remove or consolidate outdated/process-heavy/development-only documents after extracting still-useful information; do not keep deleted process documents under `docs/_archive/` in the tracked remote repository.
* Delete tracked visual evidence directories and bulk screenshots, including `docs/operations/v2-visual-evidence/`, unless an image is explicitly approved later for README/public presentation.
* Add `.gitignore` rules that ignore most raster screenshot/image files by default, with explicit allowlist exceptions reserved for approved public assets.
* Update operational guidance to reflect the current one-command agent install flow and explicit `HOUFENG_PUBLIC_BASE_URL` requirement.
* Remove or soften unnecessary strict constraints that are not true product/architecture boundaries.
* Preserve current hard constraints only when backed by code, deployment topology, security model, or accepted current plans.
* Keep documented claims verifiable from current code, Makefile, env examples, migrations, or accepted current plans.

## Acceptance Criteria

* [ ] README.md accurately explains the current project purpose, early-stage status, quick start, key components, verification commands, and documentation map.
* [ ] docs/ has a clearer top-level index that separates current user/operator guidance from design baselines, historical release planning, and evidence/process material.
* [ ] Current deployment/agent installation docs match the merged implementation, including center-generated one-command install, release artifacts, checksum verification, token secrecy, and `HOUFENG_PUBLIC_BASE_URL`.
* [ ] Fresh-install/smoke guidance no longer treats the old manual enrollment-token flow as the primary path unless clearly labeled as troubleshooting or API-level verification.
* [ ] Outdated/process docs are removed from the tracked repository after useful extraction; no new `docs/_archive/` copies are introduced for completed process artifacts.
* [ ] Bulk screenshots and visual-evidence directories are removed from the tracked repository, and `.gitignore` prevents accidental future image commits except explicit public README assets.
* [ ] Docs avoid invented features and explicitly mark early-stage limitations where needed.
* [ ] Overly strict constraints are removed or reframed unless they are still required by current architecture/code.

## Definition of Done

* Documentation changes are reviewed for factual consistency against current repository state.
* Broken internal links introduced by the refresh are avoided or fixed.
* Relevant docs/spec context is updated if the documentation organization introduces reusable project conventions.
* PR summary includes what was deleted, moved, consolidated, and intentionally left for later.

## Out of Scope

* Implementing new product features while refreshing docs.
* Full contributor/developer handbook rewrite in this pass.
* Rewriting frozen product/design baselines as new product decisions without user approval.
* Claiming production readiness, package manager support, Docker/Kubernetes support, automatic upgrades, completed real-data validation, or provider/billing truth unless verified in code/evidence.
* Committing real VPS/customer/provider data unless explicitly redacted and authorized.

## Technical Approach

* Treat README.md and `docs/README.md` as the public navigation layer.
* Keep `docs/deploy/local-and-systemd.md` as the canonical deployment recipe, revising it only where wording is too V1/internal or incomplete.
* Update `docs/operations/v1-smoke-run.md` so it follows current auth and one-command onboarding reality.
* Delete process-heavy release/operations documents from tracked docs after distilling durable facts into current docs; do not replace them with tracked `docs/_archive/` copies.
* Delete visual evidence directories and bulk screenshots from tracked docs; keep future images only through an explicit public-asset allowlist.
* Keep design baselines only when they remain current factual product/design references; delete completed planning, roadmap, evidence, and UI-process documents when their tasks are done and their durable facts have been extracted.

## Technical Notes

* Task directory: `.trellis/tasks/05-16-docs-refresh-after-agent-install`.
* Inspected: `README.md`, `docs/` inventory, `docs/deploy/local-and-systemd.md`, `docs/operations/v1-smoke-run.md`, `docs/operations/asset-ledger-real-data-validation-readiness.md`, `docs/release/docs-audit.md`, `docs/release/next-phase-plan.md`, and `docs/release/current-state-and-next-stage-plan.md`.
* Need verify against: `.env.example`, `Makefile`, `internal/contracts/agentapi/routes.go`, current auth/onboarding handlers, and docs links before finalizing.
