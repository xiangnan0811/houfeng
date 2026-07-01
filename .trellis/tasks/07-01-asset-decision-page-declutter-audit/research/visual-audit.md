# Visual audit evidence

## Method

- Current branch at audit start: `main` release `v0.56.2`, then feature branch `ux/asset-decision-page-declutter-audit`.
- Data source: local Vite app at `http://127.0.0.1:5179/asset-decisions` with `scripts/visual_evidence.py` asset-workflows mock API served locally.
- Browser: Chromium through CDP.
- Viewports checked:
  - Desktop: `1440x1000`
  - Mobile: `390x900`
- User-provided reference screenshot: `/home/murray/.codex/attachments/17ffe930-1848-4cf8-ab30-2f74f10adef4/image-1.png`

## Quantitative Findings

### Main Page

- Desktop default page visible text: about `1136` viewport characters.
- Desktop default page visible badge count: `63`.
- Desktop default page visible button/link count: `18`.
- Mobile default page viewport still shows dense summary, secondary surfaces, tabs, and long workbench explanation before the user reaches the first group content.

### Automatic Group Modals

Default group detail modals still behave like report pages:

| View | Sections | Badges | Buttons | Problem |
| --- | ---: | ---: | ---: | --- |
| cancel linkage group | 5 | 15 | 9 | Summary cards, decision block, score bars, risk chips, member preview, panel nav all visible at once |
| renewal portfolio group | 7 | 24 | 13 | Three member rows plus score bars and multiple repeated role/action chips |
| budget pressure group | 6 | 19 | 11 | Budget risk is visible, but buried among scores, chips, member rows, panel summaries |
| evidence gap group | 5 | 17 | 9 | Evidence gap is visible, but repeated as chips, score status, summary text, member action |

Mobile group detail starts with stacked summary cards and decision evidence; before reaching actual member context, the user already sees summary cards, score bars, chips, risk text, and primary actions.

### Other Detail Modals

- Record detail default still exposes saved evidence score bars and a large saved-evidence block; it is better than the original group modal but still report-like.
- Manual group default still exposes readiness checklist cards, score bars, and member rows simultaneously.
- Template detail is comparatively closer to the desired direction: short summary, limited chips, simple navigation.

## Root Causes

1. **No text budget**: Default surfaces have no cap on visible paragraphs, chips, score bars, or action buttons.
2. **Report-first rendering**: `renderDetailCommand`, `renderMemberDecisionPreview`, and group cards render evidence explanation by default instead of only the decision headline.
3. **Repeated semantics**: The same concept appears as summary text, badge, score bar, paragraph, and member chip.
4. **Page body still overloaded**: The previous fix focused on modal secondary panels but left the main workbench group cards dense.
5. **Secondary navigation is too verbose**: Panel nav includes labels plus summaries plus counts; it behaves like another content section.
6. **English eyebrow labels add noise**: `GROUP DECISION`, `MEMBERS`, `PORTFOLIO WORKBENCH`, `NEXT STEP`, `COMPARISON` are low-value visual clutter in the default path.

## Affected Code Surfaces

- `web/src/pages/AssetDecisionsPage.tsx`
  - group card list around `asset-decision-group-card`
  - `renderDetailCommand`
  - `renderMemberDecisionPreview`
  - `renderDetailPanelNav`
  - group, manual group, record, and template modal default bodies
- `web/src/index.css`
  - `.asset-decision-command-summary`
  - `.asset-decision-group-card`
  - `.asset-decision-detail__summary`
  - `.asset-decision-detail-command`
  - `.asset-decision-member-preview`
  - `.asset-decision-detail-nav`

## Design Implication

The next implementation must not only move heavy content behind secondary panels. It must reduce the default visible layer itself: fewer blocks, fewer words, fewer chips, no score bars by default, and a single compact decision summary contract shared by main cards and detail modals.
