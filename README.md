# 候风 / Houfeng Fleet Control Plane

This is the V1 implementation repository for 候风 / Houfeng Fleet Control Plane. It exists to implement the frozen V1 baseline package, not to redefine it during delivery.

## Implementation rule

不要在实现阶段重新设计 V1 一级能力。If implementation work discovers a mismatch, record the gap against the frozen baseline before changing behavior.

## Branch workflow governance

Houfeng uses a protected-branch workflow:

- Local `main` / `master` must stay read-only for development work. Do not commit, merge, amend, squash, reset, or otherwise directly modify those branches.
- Create a new branch for every feature, bug fix, documentation update, or agent implementation task.
- Do not use `git worktree` in this repository workflow.
- Enable the versioned local hooks once per clone:

  ```bash
  sh scripts/setup-git-hooks.sh
  ```

  This sets `core.hooksPath=.githooks` and activates hooks that reject commits on local `main` / `master` and pushes to remote `main` / `master`.
- Remote `main` / `master` must be protected in the Git host to reject direct pushes and force pushes by everyone. Changes should land through pull requests from feature branches.

## V1 baseline 文档（部分 frozen / 部分 superseded）

V1 业务结构 frozen 在 v1-baseline 的 4 份子集（加 README，共 5 份保留在原路径）：

1. `docs/design/v1-baseline/README.md`
2. `docs/design/v1-baseline/architecture-data-model.md`
3. `docs/design/v1-baseline/rules-and-interaction.md`
4. `docs/design/v1-baseline/interactive-prototype-and-operation-flow.md`
5. `docs/design/v1-baseline/tech-selection.md`

视觉部分已 unfrozen，权威指向 v2-houfeng：

- `docs/design/v2-houfeng/design-language.md`
- `docs/design/v2-houfeng/component-spec.md`

早期视觉文档（`ui-ux-spec.md` / `baseline-screens.md` / `visual-review-round2.md` / `handoff.md` / `stitch/*`）以及整个 `v1.x-frontend-redesign/` 已 archive 至 `docs/_archive/design/`，仅作历史记录。

## Guardrails

- Product name 不变：`候风 / Houfeng Fleet Control Plane`
- Visual authority：`docs/design/v2-houfeng/`（`design-language.md` + `component-spec.md`）。已 supersede 早期 v1-baseline 视觉部分（stitch / ui-ux-spec / baseline-screens / visual-review-round2 / handoff）和整个 `v1.x-frontend-redesign/`，这两批历史材料已迁至 `docs/_archive/design/`。
- Tech direction 不变：Go center + Go agent + React/Vite web + PostgreSQL
- Scope：V1.x 已加 username/password login + sessions；产品仍是 tightly bounded 单用户 operator tool
- 实施与设计 mismatch：先在 `docs/release/v1-gap-checklist.md` 登记 gap，再参考 `docs/release/next-phase-plan.md` 决定优先级
- **当前阶段**：V1 收口期（详见 `docs/release/next-phase-plan.md`）；**V1 ≠ MVP**——用户心目中的 MVP 范围比 v1-baseline 大

当前实施入口 / 初始实现落位可先围绕以下路径展开，其中 `docs/design/v1-baseline/` 是 V1 业务结构权威（4 份 frozen 子集），`docs/design/v2-houfeng/` 是视觉权威：

- `cmd/houfeng-center`
- `cmd/houfeng-agent`
- `internal/center`
- `agent`
- `db/migrations`
- `web`
- `docs/design/v1-baseline`

这不是冻结最终目录名，最终结构以后续代码落地为准。

## Delivery and V1 verification artifacts

- 部署 recipe：`docs/deploy/local-and-systemd.md` + `docs/deploy/systemd/houfeng-center.service` + `docs/deploy/systemd/houfeng-agent.service`
- 真实环境冒烟脚本：`docs/operations/v1-smoke-run.md`
- v2 视觉预览与证据流程：`docs/operations/v2-visual-evidence.md`
- gap 清单（含 V1 release gate 与 12 条 2026-05-02 新增 gap）：`docs/release/v1-gap-checklist.md`
- docs 审计与 archive 决策：`docs/release/docs-audit.md`
- 下一阶段开发计划（Stage 1/2/3）：`docs/release/next-phase-plan.md`

注：早期 `docs/operations/v1-visual-verification.md` 与 `docs/operations/visual-evidence/` 与 v1-baseline/stitch 视觉强绑定，已迁至 `docs/_archive/operations/`。当前 v2 预览、浏览器 sanity 与截图证据流程见 `docs/operations/v2-visual-evidence.md`；一次性历史截图仍保留在 `docs/operations/*.jpg`。

Automated verification:

```bash
go test ./...
./scripts/verify.sh
cd web && npm run build
```

Live PostgreSQL smoke 与 Telegram delivery 证据 require environment-specific runtime setup，分别记录在 `docs/operations/v1-smoke-run.md` 与 `docs/release/v1-gap-checklist.md`。
