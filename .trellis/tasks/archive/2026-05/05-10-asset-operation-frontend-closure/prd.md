# Asset operation frontend closure

## Goal

Close the remaining frontend operation gaps in the VPS Asset Ledger flow without touching the real-data import work. The current backend already supports VPS-to-Node links, Node-to-VPS summaries, VPS PATCH renewal decision changes, and timeline history. This task exposes those existing contracts through focused UI and API helpers so operators can complete common asset decisions from the web app.

## What I Already Know

* `houfeng_codex_下一步开发计划.md` first-phase criteria still require Node-side linked VPS visibility, VPS-to-Node linking, and renewal decision marking.
* Backend endpoints already exist for `GET /api/nodes/{node_id}/vps`, `POST /api/vps/{vps_id}/link-node`, `POST /api/vps/{vps_id}/unlink-node`, `PATCH /api/vps/{vps_id}`, and `GET /api/vps/{vps_id}/timeline`.
* `web/src/lib/api.ts` currently has `listVPSForNode`, `listVPSNodes`, `getVPSAsset`, and `getVPSTimeline`, but lacks frontend helpers for VPS patch/link/unlink.
* `VPSDetailPage` shows linked Node summaries and timeline, but has no link/unlink or renewal decision change operation.
* `NodeDetailPage` does not consume `listVPSForNode`, so Node-side linked VPS visibility is missing.
* Real VPS JSON data verification/import remains explicitly deferred by the user.
* No subagents will be used for this task.

## Requirements

* Add typed frontend API helpers for:
  * `PATCH /api/vps/{vps_id}` renewal decision updates.
  * `POST /api/vps/{vps_id}/link-node`.
  * `POST /api/vps/{vps_id}/unlink-node`.
* Extend `VPSDetailPage` with a compact operation panel:
  * Change renewal decision with an optional reason.
  * Refresh VPS detail and timeline after a successful decision change.
  * Link a Node by `node_id` and optional note.
  * Unlink an active Node while preserving backend history.
  * Refresh linked Node summaries after link/unlink.
* Extend `NodeDetailPage` with a read-only linked VPS summary section:
  * Use `listVPSForNode(node_id)`.
  * Show lifecycle, usage, renewal decision, provider/location, link time, and note.
  * Keep Node monitoring/runtime semantics unchanged.
* Keep all new UI requests centralized in `web/src/lib/api.ts`; no page-level direct `fetch`.
* Preserve existing machine-value API contracts and Chinese display labels.
* Keep UI dense and operational, consistent with existing asset pages.

## Acceptance Criteria

* [ ] Node detail shows a linked VPS section populated from `/api/nodes/{node_id}/vps`.
* [ ] VPS detail can link a Node by `node_id` and updates the active link table after success.
* [ ] VPS detail can unlink an active Node and updates the active link table after success.
* [ ] VPS detail can change renewal decision, send an optional `renewal_reason`, and refresh timeline history after success.
* [ ] API helpers and types are covered by `web/src/lib/api.test.ts`.
* [ ] Page tests cover linked VPS rendering, link/unlink behavior, and renewal decision behavior.
* [ ] `git diff --check`, lint, focused Vitest, build, and `make verify-web` pass before commit.

## Out of Scope

* Real 40+ VPS JSON dry-run/import execution or production data import.
* A standalone decision workbench page.
* Provider/subscription edit forms beyond the operation closures above.
* Backend endpoint or schema changes unless a real frontend-blocking contract bug is discovered.
* Provider API sync, DNS sync, Web SSH, service discovery, RBAC, exchange-rate conversion, scoring algorithms, or Agent remote-command expansion.

## Technical Notes

* Plan anchors: `houfeng_codex_下一步开发计划.md` Task 5, Task 8, and first-phase criteria 5, 7, 10.
* Relevant frontend specs: `.trellis/spec/web/state-and-data.md`, `.trellis/spec/web/component-conventions.md`, `.trellis/spec/web/styling-guidelines.md`, `.trellis/spec/web/quality-guidelines.md`.
* Shared thinking: `.trellis/spec/guides/branch-workflow-governance.md`, `.trellis/spec/guides/cross-layer-thinking-guide.md`, `.trellis/spec/guides/code-reuse-thinking-guide.md`.
* Local test temp-dir workaround: use `TMPDIR=/Users/weibo/Code/houfeng/.tmp/tmp` for Vitest when the system temp dir is problematic.
