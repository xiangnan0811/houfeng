# Asset Decision Workbench Deep Clean Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use inline execution in this Codex session. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/asset-decisions` detail dialogs cover-first and directory-first, with compact member panels and preserved write flows.

**Architecture:** Keep existing API contracts and page ownership. Add frontend-only directory panel states and replace heavy member detail rendering with compact rows. Tests describe the disclosure ladder before implementation.

**Tech Stack:** React 19, TypeScript, Vite, Vitest, Testing Library, pure CSS with existing tokens/BEM.

---

## Files

- Modify: `web/src/pages/AssetDecisionsPage.test.tsx`
- Modify: `web/src/pages/AssetDecisionsPage.tsx`
- Modify: `web/src/styles/pages.css`
- Add: `.trellis/tasks/07-01-asset-decision-workbench-deep-clean/audit.md`

## Task 1: Record Current Audit

- [ ] Inspect current code paths for automatic groups, manual groups, records, templates, and source review reopening.
- [ ] Write `audit.md` with the current issue map and target checks.

## Task 2: Failing Tests For Directory-First Detail

- [ ] Update `expectAutomaticGroupDefaultCover` to forbid detail directory labels and member list labels by default.
- [ ] Add helper assertions:
  - default cover exists and detail nav does not;
  - clicking `查看详情` shows a detail directory;
  - the directory does not show member names, `处理`, raw table regions, or save forms;
  - clicking the member entry reveals compact members.
- [ ] Update automatic group tests:
  - non-cost group;
  - cost group;
  - URL/source reopened group if covered by existing tests.
- [ ] Update manual group tests to navigate cover -> directory -> selected panel.
- [ ] Update template tests to require cover -> directory before create/members/status panels.
- [ ] Run `cd web && npm run test -- --run AssetDecisionsPage.test.tsx` and confirm failure is caused by the missing directory-first behavior.

## Task 3: Implement Directory Panels

- [ ] Extend panel types with `directory` for automatic, manual, and template detail modals.
- [ ] Change `查看详情` buttons to set `directory` instead of direct member panels.
- [ ] Add directory render helpers that produce compact entry tiles and call existing panel state transitions.
- [ ] Keep save-record state initialization in existing `startRecordSave` / `startManualRecordSave` flows when the user chooses save from the directory or nav.
- [ ] Keep raw table and VPS work panel access only after explicit member/raw/work-panel entry.

## Task 4: Compact Member Rows

- [ ] Replace the heavy `renderMemberDecisionCards` usage with a compact row renderer.
- [ ] Keep role/action badges, rank/lane, one summary sentence, limited risk/evidence chips, and optional actions.
- [ ] Ensure automatic member actions still call `selectVPS` or link to VPS/cancellation workbench only inside the member panel.
- [ ] Ensure manual member intent match remains visible only inside the manual member panel.

## Task 5: Template Cover

- [ ] Convert template modal default state to a quiet current-judgement cover using `renderDetailCommand`.
- [ ] Move existing template summary/nav into the `directory` and explicit panels.
- [ ] Preserve create-from-template, member blueprint, and status maintenance behavior.

## Task 6: Styling

- [ ] Add BEM/token CSS for directory tiles and compact member rows.
- [ ] Add responsive rules for `920px` and `640px` breakpoints.
- [ ] Do not add new CSS files or hard-coded colors.

## Task 7: Verification

- [ ] `git diff --check`
- [ ] `python3 ./.trellis/scripts/task.py validate 07-01-asset-decision-workbench-deep-clean`
- [ ] `cd web && npm run test -- --run AssetDecisionsPage.test.tsx`
- [ ] `cd web && npm run lint`
- [ ] `cd web && npm run test -- --run`
- [ ] `cd web && npm run build`
- [ ] Browser validation with mock API/dev server at desktop `1440x1000` and mobile `390x900` for automatic cost, automatic non-cost, manual, record/source review, and template dialogs.

## Task 8: Finish Flow

- [ ] Complete Trellis Phase 3.3 spec decision.
- [ ] Commit implementation.
- [ ] Archive task and record journal.
- [ ] Only after the branch is clean, push branch, create PR, monitor PR checks, fix if needed, merge when green, monitor main CI, Release Please, release/image publishing if triggered, then clean local state.
