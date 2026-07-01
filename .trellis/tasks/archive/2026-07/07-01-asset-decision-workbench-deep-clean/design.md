# Asset decision workbench deep clean design

## Scope

This task is a frontend information-architecture refactor for `/asset-decisions`. It does not change backend APIs, database schema, route contracts, or frontend dependencies.

The changed surface is the asset decision page and its colocated tests/styles:

- `web/src/pages/AssetDecisionsPage.tsx`
- `web/src/pages/AssetDecisionsPage.test.tsx`
- `web/src/styles/pages.css`

## Current problem

The page already removed some old report markers, but the actual disclosure model is still wrong:

- Automatic group `查看详情` goes directly from cover to `members`.
- `members` renders `renderMemberDecisionCards`, where each member becomes a large report card with identity, facts, status, evidence, strengths, risks, chips, and actions.
- Manual groups reuse the same heavy member renderer.
- Template detail opens with all summary/nav affordances visible instead of a quiet cover.

This means the first non-default interaction still floods the operator with a long report. The requirement is not to hide risk; it is to make risk and next action visible while moving full evidence, member actions, save forms, and raw tables behind explicit second-level choices.

## Target interaction model

All asset decision detail objects use the same disclosure ladder:

1. **Decision cover**
   - Shows object title via the modal title, a single current judgement, 1-2 risk chips, compact metadata, and the primary safe action.
   - Does not show member names, VPS IDs, member actions, save forms, raw tables, detail nav, or report headings.

2. **Detail directory**
   - Opened by `查看详情`.
   - Shows a small second-level workbench with entry tiles only.
   - Still does not expand members, save forms, raw tables, or single-VPS processing.
   - Each tile has a short label, count/status, and bounded summary.

3. **Single panel**
   - Clicking one directory/nav entry opens exactly one panel: member decisions, save record, edit group, add member, raw data, single VPS processing, execution follow-up, source review, template create/status, etc.
   - Existing write flows remain available from these explicit panels.

## Data flow and compatibility

All data keeps using existing API responses:

- `AssetDecisionGroupDetail`
- `AssetDecisionManualGroupDetail`
- `AssetDecisionRecordDetail`
- `AssetDecisionScenarioTemplateDetail`

No new fields are required. Existing payload builders for saving records, creating manual groups, editing manual groups, template creation, record follow-up, and VPS decision submission stay unchanged.

Panel state changes are frontend-only:

- Add a `directory` panel state for automatic groups.
- Add a `directory` panel state for manual groups.
- Add a `directory` panel state for templates.
- Record detail already has a cover-first pattern; keep its existing API and tighten tests only where needed.

## Member panel design

Replace the heavy default member card presentation in asset decision detail flows with compact decision rows:

- One row per member.
- Columns: lane/rank + identity, role/action badges, one decision sentence, one compact evidence/risk chip group, optional actions.
- Do not show four fact boxes, separate strengths/risks sections, source facts grid, or large card copy.
- Actions remain available where they existed, but only inside the explicit member panel.

The existing heavy renderer can be removed if no callers remain. If retained temporarily, it must not be the first detail view for any asset decision object.

## Styling

Use existing BEM and token patterns in `web/src/styles/pages.css`.

New/changed blocks:

- `asset-decision-detail-directory`
- `asset-decision-detail-directory__item`
- `asset-decision-member-rows`
- `asset-decision-member-row`

Layout must remain dense and quiet:

- No nested card stacks.
- No large hero treatment in modals.
- Mobile width uses one-column rows with no document/body horizontal overflow.
- Raw tables may keep internal horizontal scroll.

## Testing approach

Tests are the primary regression guard:

- Strengthen automatic group default-cover assertions to forbid detail directory labels and member/report content.
- Add assertions that `查看详情` opens a detail directory, not the member list.
- Add assertions that members appear only after clicking the member entry.
- Add the same directory-first checks to cost and non-cost automatic groups, manual groups, saved-record source review, and templates.
- Preserve write-flow tests by navigating through the directory before clicking save/edit/add/raw panels.

Browser validation uses local mock API/dev server/CDP only; no new e2e dependencies.

## Risks and mitigations

- **Risk: tests rely on old direct-to-members flow.** Update tests to express the new interaction ladder and preserve existing payload assertions.
- **Risk: hiding actions breaks workflows.** Every existing write panel remains reachable through the directory/nav path.
- **Risk: mobile layout overflows.** Add responsive grid rules and verify at `390x900`.
- **Risk: default view hides meaningful risk.** Keep compact risk chips and summary text sourced from existing evidence/recommendation fields.
