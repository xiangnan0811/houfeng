# 候风 / Houfeng Fleet Control Plane

This is the V1 implementation repository for 候风 / Houfeng Fleet Control Plane. It exists to implement the frozen V1 baseline package, not to redefine it during delivery.

## Implementation rule

不要在实现阶段重新设计 V1 一级能力。If implementation work discovers a mismatch, record the gap against the frozen baseline before changing behavior.

## Frozen V1 baseline package

The following files are the source of truth for the V1 baseline and must remain available in version control:

1. `docs/design/v1-baseline/README.md`
2. `docs/design/v1-baseline/architecture-data-model.md`
3. `docs/design/v1-baseline/rules-and-interaction.md`
4. `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md`
5. `docs/design/v1-baseline/ui-ux-spec.md`
6. `docs/design/v1-baseline/visual-review-round2.md`
7. `docs/design/v1-baseline/tech-selection.md`
8. `docs/design/v1-baseline/handoff.md`
9. `docs/design/v1-baseline/baseline-screens.md`

## Guardrails

- Product name stays: `候风 / Houfeng Fleet Control Plane`
- Visual authority stays: Unified / Baseline Stitch screens only
- Tech direction stays: Go center + Go agent + React/Vite web + PostgreSQL
- Scope stays: single-user, monolith center, systemd agent fleet
- If implementation diverges from design, report the gap before changing behavior

当前唯一冻结入口是 `docs/design/v1-baseline/`，实现结构以后续代码落地为准。
