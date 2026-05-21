# SettingsPage section hierarchy IA micro-polish

## Goal

在 `AssetDecisionsPage` IA micro-polish 已发布为 v0.15.0 后，本轮选择 `SettingsPage` 做一轮极窄、security-frozen 的 frontend-only IA micro-polish：保留现有三分组 tabs、通知通道管理 modal、单页统一保存和所有 settings payload/secret/runtime 语义，只补齐 v2 Settings 模板里仍残留的 section hierarchy / DetailSection ribbon / 风险边界 framing 对齐。

## What I already know

- 用户明确要求“优选下一批页面，然后继续推进”。
- 用户已授权页面 IA 批次可由我自主选择最优顺序推进，无需确认页面顺序。
- 最近已完成 IA polish 的范围包括 NodeDetail、VPSDetail、Settings limited cleanup、Targets+Nodes 列表、NodeCompare、VPSPage inventory、NodeOnboarding、TargetDetailPage、ProvidersPage + SubscriptionsPage、AssetDecisionsPage。
- 两份 research 均确认剩余 broad IA 价值已经很低；`SettingsPage` 是唯一还存在明确 v2 component-spec residual gap 的 routed page，但必须保持窄范围。
- 该 residual gap 是：v2 Settings 模板要求更清晰的 section hierarchy 和 `DetailSection` ribbon 语义；当前实现保留了合理的 tabs/channel-manager IA，但多数 settings sections 没有显式 ribbon，页面顶部/页尾对“本地主题、运行时接管、持久化策略、统一保存”的风险边界仍可更清楚。
- Settings 涉及通知 token/webhook 和 runtime-managed delivery，是安全敏感页面；本轮只做视觉/IA/copy/test polish，不改变行为。
- 当前开发在 feature 分支 `feature/next-page-ia-batch-4-20260521`。

## Requirements

- 将本轮范围限定为 `SettingsPage` 窄范围视觉/IA 对齐，不做第二次 Settings 重写。
- 保留现有三分组 tabs：`通用与外观`、`通知与告警`、`高级与策略`。
- 保留通知通道管理 modal、Telegram/Feishu add/edit draft 行为、channel expand/collapse、底部统一保存区。
- 在现有 IA 内强化 section hierarchy：
  - `主题` 明确为本地浏览器偏好，使用 v2 `notice` ribbon。
  - `Telegram` 使用 `accent-2` ribbon，并继续只展示 masked token summary。
  - `飞书` 保留现有 `accent-2` channel styling，不扩大 provider semantics。
  - `频率档位` 使用 `normal` ribbon，区分“实时规划链”与“仅持久化策略”。
  - `全局默认`、`覆盖规则`、`保留策略` 使用 `notice` ribbon，强调运行时/持久化/清理边界。
  - `渠道管理` 和底部保存区 copy 可微调以说明草稿/统一保存风险边界。
- 可增加极少量 Settings page summary/framing copy 或 CSS hook，但不得把页面改成低密度卡片墙或完整 vertical rewrite。
- 新增/更新测试覆盖新增 IA 文案/结构，并继续覆盖 Settings 现有 token masking、payload omission、tab/channel modal、draft discard、save payload 等冻结契约。
- 修改范围默认限定在：
  - `web/src/pages/SettingsPage.tsx`
  - `web/src/pages/SettingsPage.test.tsx`
  - `web/src/pages/settings/*Section.tsx` 中必要的 ribbon/copy/class hook
  - `web/src/styles/pages.css`（仅必要 token/BEM 样式）

## Frozen Contracts

- 保留 API helper usage：`getSettings()`、`updateSettings()`。
- 保留 `/api/settings` request/response shape、`SettingsRecord`、`SettingsUpdateInput` 和所有 snake_case 字段语义。
- 保留页面初始加载、loading/error PageState、`buildFormState`、`buildUpdateInput`、validation error 文案和提交流程。
- 保留单个全量保存 action；不拆分独立保存 API，不新增 autosave，不新增局部 mutation。
- 保留 Telegram secret handling：
  - API 不回显明文 token 的事实不变；页面不得展示或伪造明文 token。
  - 已保存 token 只显示 `token_masked_summary`，仍使用 mono/numeric presentation。
  - 用户未输入新 token 时，PATCH payload 继续省略 `bot_token`。
  - 新 token 只存在于 deliberate password input/edit draft 中。
- 保留 Feishu webhook handling；不得把 webhook 值展示到 page summary、AppShell、Dashboard 或全局状态面。
- 保留 dismissed add-channel modal draft 不进入主 form / save payload 的行为。
- 保留 runtime semantics：不得改变 `runtime_managed` / `runtime_apply_active` 解释，不声称真实通知投递成功或 center health。
- 保留 incident defaults、override rules、retention policy 的 payload shape、JSON textarea、positive number validation 和语义边界。
- 保留 settings tabs、modal close/cancel/back behavior、channel expand/collapse behavior。
- 不新增 notification providers/channels，不修改 auth/session/installer/agent token/security flows。
- 不新增依赖、路由、CSS system、page-local CSS 或 backend changes。

## Acceptance Criteria

- [ ] Research 文件说明候选页审计、设计/spec 对照和选择 `SettingsPage` narrow pass 的理由。
- [ ] PRD 明确本轮页面范围、目标、冻结契约、验收标准和 out-of-scope。
- [ ] 实现改动只触及 SettingsPage、Settings section components、对应测试、`pages.css` 与必要既有组件。
- [ ] Settings section hierarchy/ribbon/copy 对齐 v2 Settings 模板，同时保留三分组 tabs 和 channel-manager IA。
- [ ] 测试覆盖新增 IA 文案/结构，并继续覆盖 token masking、payload omission、save payload、tab 切换、channel modal draft discard、runtime copy truthfulness。
- [ ] `npm --prefix web run lint` 通过。
- [ ] `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run` 通过。
- [ ] `npm --prefix web run build` 通过。
- [ ] `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` 通过。
- [ ] UI/browser sanity 覆盖 `/settings` golden path，并明确 local/mock/auth caveat。
- [ ] Trellis task 归档，feature 分支 clean，按既定 PR/release flow 跟进完成。

## Definition of Done

- Trellis research、PRD、implement/check jsonl 完整且 validate 通过。
- `trellis-implement` 完成实现并自测。
- `trellis-check` 独立检查并修复问题。
- 本地验证、PR checks、main CI、Release Please、release assets/publish-images 全部完成。
- 无 active Trellis task、无 open PR、本地 `main` clean 且同步远端。

## Technical Approach

1. 在现有 `SettingsPage` 架构内做微调，不改变数据流、tabs、modal、form state、payload builder 或 save model。
2. 优先在 settings section components 上补 `DetailSection` ribbon props，并微调 section intro/copy，让 v2 模板层级更清晰。
3. 必要时在 `SettingsPage.tsx` 调整 hero/tab/save/channel-manager 文案，强调“分组编辑、页尾统一保存、secret 不外泄、草稿确认后才进入主表单”。
4. 必要时在 `pages.css` 添加少量 token/BEM Settings class，避免 inline style 和新 CSS 系统。
5. 更新 `SettingsPage.test.tsx`：为新增 IA copy/ribbon structure 加断言，同时保留现有 settings secret/payload/modal/draft/save 行为断言。

## Decision (ADR-lite)

**Context**: AssetDecisionsPage v0.15.0 后，两份 research 均未发现新的 broad high-value/low-risk 页面。多数 routed pages 已近期优化或已符合 v2 模板；`LoginPage` 极安全但价值很小；Providers/Subscriptions 只剩 atom-level consistency；Dashboard/Events 回归风险高。

**Decision**: 选择 `SettingsPage`，但限定为 security-frozen section hierarchy IA micro-polish，不做页面重写。

**Consequences**: 本轮收益集中在 Settings 的 v2 section hierarchy 与风险边界表达；风险通过冻结 `/api/settings`、secret handling、runtime semantics、modal draft、tabs 和统一保存契约控制。后续若没有具体 real-use findings，应停止 speculative 页面 IA 批次，转向真实使用反馈或功能交付。

## Out of Scope

- 不修改 backend、database migration、API request/response shape 或 settings 数据模型。
- 不新增或删除 notification provider/channel，不改变 Telegram/Feishu runtime delivery 语义。
- 不展示明文 token/webhook，不把 secret summary 移到 Dashboard/AppShell/global UI。
- 不把 Settings 改回完整 vertical rewrite，不移除 tabs，不拆分保存流。
- 不修改 auth/session、Node onboarding installer、agent token、安全敏感流程。
- 不引入新依赖、图表库、CSS framework、page-local CSS、新路由或导航分组。
- 不重做 DashboardPage、EventsPage、LoginPage、AssetDecisionsPage 或近期完成页面。

## Research References

- [`research/remaining-page-ia-audit.md`](research/remaining-page-ia-audit.md) — 剩余 route/page 与近期 IA 归档审计，指出 broad IA 价值低，推荐 SettingsPage 窄范围对齐。
- [`research/design-spec-candidate-audit.md`](research/design-spec-candidate-audit.md) — v2 design/spec fit 审计，确认 SettingsPage section hierarchy/ribbon 是唯一有意义的 residual spec gap。

## Technical Notes

- Current task: `.trellis/tasks/05-21-next-page-ia-batch-4`。
- Feature branch: `feature/next-page-ia-batch-4-20260521`。
- Current implementation files inspected: `SettingsPage.tsx`、`SettingsPage.test.tsx`、`settings/*Section.tsx`、`pages.css`。
- Relevant specs: `.trellis/spec/web/component-conventions.md`、`.trellis/spec/web/styling-guidelines.md`、`.trellis/spec/web/state-and-data.md`、`.trellis/spec/web/quality-guidelines.md`、`docs/design/v2-houfeng/design-language.md`、`docs/design/v2-houfeng/component-spec.md`、`docs/operations/v2-visual-evidence.md`。
