# Second-pass Asset Decision Modal Declutter Design

## Problem

The current `v0.56.3` UI still renders detail modals as compact reports. Removing English eyebrows and score bars was not enough because the default layer still combines fact rail, current judgement, full member preview, panel nav, and multiple action choices.

The new design changes the default layer contract from "summary + preview" to "decision cover".

## Decision Cover Contract

Default modal content must answer only:

1. What is this decision target?
2. What is the current recommendation?
3. What is the single most important risk or blocker?
4. What is the next primary action?
5. Where can I inspect details?

Default modal content must not include:

- full member rows;
- saved evidence member list;
- full provider / region / product / status / cost sentences;
- score bars;
- multi-card readiness checks;
- raw source text or table rows;
- explanatory panel summaries.

## Automatic Group Modal

Default overview will render:

- Header: group title + close.
- Thin fact strip: `VPS N`, `cost`, `evidence tier`, `risk count` only.
- Decision cover:
  - one sentence from recommendation / insight;
  - at most two risk chips;
  - one primary action: `创建自定义组合`;
  - one secondary action: `保存记录`.
- Detail nav:
  - compact iconless button row with only labels/counts.

Removed from default:

- `renderMemberDecisionPreview`.
- per-member `处理` buttons.
- provider/geo/product/cost member prose.
- detailed confidence/pressure/readiness numbers.

Members remain available in the `成员明细` panel.

## Manual Group Modal

Default overview will render:

- Header + thin fact strip.
- Decision cover with readiness badge and one primary action.
- Intent/facts readiness is a single compact status line or moved into edit/members panels if too long.

Removed from default:

- member preview rows.
- multi-item readiness footer.

## Saved Record Modal

Default overview will render:

- Header + status/follow-up facts.
- saved judgement sentence, capped to a short line.
- compact evidence status and key blocker count.
- panel nav.

Removed from default:

- saved evidence member list;
- per-member saved reasons;
- execution plan breakdown details.

These remain in `执行跟进`, `成员跟进`, `来源复核`, and `成员底稿`.

## Main Page

Main page is a secondary target in this task because user feedback centers on modals, but audit shows the main queue also contributes to cognitive overload. This pass should at minimum:

- remove explanatory page copy in decision queue header;
- reduce automatic group card facts to title, one short issue/risk line, 2 chips, and primary action;
- keep detailed metrics in the modal panels, not card body.

## Compatibility

- No backend API changes.
- No schema changes.
- No new dependencies.
- Existing deep links remain valid.
- Existing write workflows stay in secondary panels.

## Test Strategy

Add failing tests before implementation for:

- automatic group default does not render member preview or member names before opening `成员明细`;
- budget/cost and renewal groups both satisfy the same default contract;
- saved record default does not render saved evidence member list before opening detail panels;
- secondary panels still expose member details and write actions.

Browser verification must capture:

- desktop and mobile metrics for automatic groups: cancel, renewal, region, provider, cost, evidence;
- manual group, saved record, template;
- no document/body horizontal overflow;
- default text/button/badge counts below the new bar.
