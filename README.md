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
- Visual authority: V1.x frontend redesign at `docs/design/v1.x-frontend-redesign/` (the earlier Unified / Baseline Stitch screens under `docs/design/v1-baseline/` are historical and no longer the development reference; the structural V1 baseline remains frozen)
- Tech direction stays: Go center + Go agent + React/Vite web + PostgreSQL
- Scope: V1.x adds username/password login + sessions; product remains a tightly bounded operator tool
- If implementation diverges from design, report the gap before changing behavior

当前实施入口 / 初始实现落位可先围绕以下路径展开，其中 `docs/design/v1-baseline/` 仍是唯一冻结设计入口：

- `cmd/houfeng-center`
- `cmd/houfeng-agent`
- `internal/center`
- `agent`
- `db/migrations`
- `web`
- `docs/design/v1-baseline`

这不是冻结最终目录名，最终结构以后续代码落地为准。

## Delivery and V1 verification artifacts

- Local/systemd deployment: `docs/deploy/local-and-systemd.md`
- Center systemd example: `docs/deploy/systemd/houfeng-center.service`
- Agent systemd example: `docs/deploy/systemd/houfeng-agent.service`
- Fresh-install smoke run: `docs/operations/v1-smoke-run.md`
- Visual verification record: `docs/operations/v1-visual-verification.md`
- Final V1 gap checklist: `docs/release/v1-gap-checklist.md`

Automated verification:

```bash
go test ./...
./scripts/verify.sh
cd web && npm run build
```

Live PostgreSQL smoke, Telegram delivery, and screenshot comparison evidence are tracked separately in the operation/release documents because they require environment-specific runtime setup.
