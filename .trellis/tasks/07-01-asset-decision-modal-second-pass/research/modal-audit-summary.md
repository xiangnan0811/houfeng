# Current modal audit summary

## Method

- Baseline: current branch from released `v0.56.3`.
- Preview: Vite at `http://127.0.0.1:5188/`.
- Data source: local `asset-workflows` fixture from `scripts/visual_evidence.py`.
- Browser: Chromium CDP.
- Viewports:
  - Desktop `1440x1000`
  - Mobile `390x900`
- Raw audit: `research/current-modal-audit.txt`.

## Findings

The previous fix removed old report markers and score bars, but it did not remove the default report shape. Default modals still show four layers at once:

1. fact rail with multi-field explanations;
2. current judgement with actions;
3. full member / saved-evidence preview rows;
4. nav buttons for secondary panels.

This means the user still sees a compressed report, not a focused decision card.

## Representative metrics

| Surface | Viewport | Text | Sections | Badges | Buttons | Main noise |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| cancel group | desktop | 347 | 3 | 6 | 9 | 161-char member row with provider, geo, service, target, monitoring, cost, product and status |
| renewal group | desktop | 514 | 4 | 8 | 11 | 315-char member preview section; two full member rows |
| region group | desktop | 507 | 4 | 7 | 11 | same full member preview pattern |
| budget/cost group | mobile | 505 | 4 | 8 | 11 | 315-char member preview section; two full member rows |
| saved record | mobile | 457 | 2 | 13 | 6 | 189-char saved evidence section plus long saved judgement |
| main page | mobile | 1351 | 10 | 34 | 30 | group cards still include multi-line facts and explanatory page copy |

## Root cause

- **Incomplete scope**: previous implementation targeted old labels, score bars and panel summaries, but preserved full member previews and fact rails.
- **Wrong default unit**: default modal still treats member rows as proof objects. The default unit should be one decision sentence plus minimal status, with proof objects behind panels.
- **Duplicated semantics**: cost, role, action, provider, geo, service, target, monitoring and current lifecycle state appear together in the default member row.
- **Action overload**: automatic group default exposes two primary-level actions plus per-member `处理` plus panel nav. The user cannot tell what the main action is.
- **Page body still contributes to the same problem**: main group cards remain compact reports with 178-223 char cards and 34 visible badges on mobile.

## New bar

Default modal target:

- automatic group default text <= 260 chars on desktop and mobile fixture data;
- automatic group default sections <= 2 after header;
- automatic group default buttons <= 5 including close and panel nav triggers;
- member rows not visible by default;
- no full provider/geo/product/status/cost member sentence in default view;
- saved record default text <= 300 chars and no per-member saved evidence list by default.

The secondary panels keep the full detail.
