# SettingsPage limited information architecture cleanup

## Goal

对 `SettingsPage` 做一轮有限、低风险的信息架构清理，让设置页默认视图更清楚地区分“当前运行配置判断”“通知/保留策略配置”“保存状态与风险边界”，同时严格保留通知密钥、全局保存和后端契约。

## Requirements

- 仅纳入 `SettingsPage`、现有 `web/src/pages/settings/*` 子组件、`web/src/styles/pages.css` 与必要页面测试。
- 保持现有三组 tab：`通用与外观`、`通知与告警`、`高级与策略`，不把本任务扩展成 Settings 模型或频道 CRUD 重构。
- 默认层级要减少同权平铺，让用户先理解当前设置分组、通知通道状态、保存反馈与保留策略风险边界。
- 可以优化 hero/tab context、通知渠道管理、save footer/status 的结构和文案，但全局保存仍是单个页面级 submit。
- 清理 Settings 相关 business inline styles 到 `pages.css`，使用 BEM-ish 命名和 design tokens；不引入新 CSS 文件、CSS-in-JS、Tailwind、图表库或依赖。
- 保留 Telegram/Feishu secret masking、masked token summary、unchanged token omit payload、runtime delivery toggles、global save payload semantics、错误提示与保存成功语义。
- 保留 channel modal draft 语义：关闭/取消新增通知渠道时，modal 草稿不得写入主 form 或保存 payload。

## Frozen Contracts

- `getSettings()` 继续读取 `GET /api/settings`；`updateSettings()` 继续通过 `PUT /api/settings` 发送 JSON。
- 不修改 `SettingsRecord`、`SettingsUpdateInput`、`SettingsTelegramInput` 或后端/API/data model。
- 持久化 Telegram token 只能通过 `token_present` + `token_masked_summary` 展示；原始 token 不得出现在普通页面文本、摘要、日志或保存反馈里。
- 已存在 token 且用户未输入替换 token 时，保存 payload 必须省略 `telegram.bot_token`。
- 用户输入替换 token 时，payload 才包含 `telegram.bot_token`，并保留当前 `runtime_managed` toggle 状态。
- runtime-managed toggle、runtime disabled/active/not-managed 三态说明与禁用 Telegram delivery 的现有行为保持不变。
- Feishu 只保持现有 `enabled` + `webhook_url` 字段和展示/保存语义。
- 保留一个 form 的全量保存；不做 per-tab save、PATCH、autosave、乐观保存或独立资源拆分。
- 保留 numeric validation、override JSON validation、retention/frequency/incident defaults 语义。

## Acceptance Criteria

- [ ] `SettingsPage` 默认层级更清楚，保存状态和风险边界不再像孤立按钮/提示一样悬在页面末尾。
- [ ] 通知通道区域更清楚地区分“已配置/可编辑通道”“新增渠道”“incident 默认通知策略”。
- [ ] 运行/保留策略、高级 override、通知配置仍全部可达，tab 和表单字段无无意删除。
- [ ] Settings 相关 business inline styles 尽量迁移到 `pages.css`，遵循 BEM/tokens，不新增 CSS 系统。
- [ ] Telegram/Feishu secret masking、unchanged token omit payload、replacement token payload、runtime managed disabled state、dismissed modal draft、JSON preview、numeric validation 均有测试覆盖且通过。
- [ ] 前端验证通过：`npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- [ ] 最终完整验证优先通过：`TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- [ ] UI/browser sanity 覆盖 SettingsPage golden path；如果无法连真实后端，记录 fixture/mock caveat。

## Technical Approach

- 以当前 tabbed Settings IA 为事实基础，不在本任务中切换到设计文档里的线性 Settings 模板。
- 在 `SettingsPage.tsx` 增加轻量结构层：hero tab context、class-based tab panel、通知通道管理说明、统一 save/status footer。
- 在 `TelegramSettingsSection`、`FeishuSettingsSection`、`SectionIntro`、`OverrideRulesSection`、`IncidentDefaultsSection` 中只做展示/样式 class 化，不改表单 state、字段名、label、按钮语义或保存路径。
- 在 `pages.css` 复用/扩展现有 `.settings-*`、`.settings-channel-option*`、`.override-rule-*` hooks，新增类只服务当前 Settings IA。
- 更新 `SettingsPage.test.tsx` 时优先保留现有 contract assertions；只为新增 IA 文案/结构补充断言，不放松敏感 payload 断言。

## Decision (ADR-lite)

**Context**: `SettingsPage` 同时管理主题/频率、通知密钥、incident 默认值、override JSON 与 retention policy。页面风险高于纯展示页，尤其 Telegram token masking、未修改 token omit payload、runtime toggle 与全局保存 payload 不能被 UI 重排破坏。

**Decision**: 采用“保留 tabbed model + 清理层级/样式债务”的有限 IA 方案。只调整 Settings 页面结构、说明、save/status footer 与 CSS class 化，不改变 API、数据模型、notification delivery 或保存模型。

**Consequences**: 本轮可以降低默认视图混乱度和 inline-style debt，风险可控；但不会彻底拆分 oversized SettingsPage、不会引入 per-tab save，也不会解决当前代码与线性 Settings 设计模板的长期差异。若后续要把 tabbed Settings 作为正式设计权威，应另走 spec-update/task 流程。

## Research References

- [`research/settings-page-ia-audit.md`](research/settings-page-ia-audit.md) — 静态审计确认 Settings 当前结构、安全冻结契约、可安全 class 化/层级优化 seam 与验证重点。
- [`research/browser-sanity.md`](research/browser-sanity.md) — mock-backed Chrome DevTools browser sanity 覆盖 `/settings` 默认、通知、高级 tab 与保存 footer；非真实 center/PostgreSQL。

## Verification Evidence

- `trellis-implement` 完成 SettingsPage limited IA cleanup，并通过 focused SettingsPage test、lint、full web tests、build、full verify。
- `trellis-check` 独立检查通过，未发现需要修复的问题；确认 scope、token/save/runtime frozen contracts、global full-save、modal draft discard 和 inline-style cleanup 均保持。
- 主会话复跑 `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run src/pages/SettingsPage.test.tsx`：1 file / 9 tests passed。
- 主会话复跑 `npm --prefix web run lint`：passed。
- 主会话复跑 `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`：63 files / 488 tests passed。
- 主会话复跑 `npm --prefix web run build`：passed。
- 主会话复跑 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`：passed。
- `rg "style=\\{\\{" web/src/pages/SettingsPage.tsx web/src/pages/settings` 无输出，Settings 相关业务 inline style 已清理到 class/CSS。
- Browser sanity：local Vite + mock API + Chrome DevTools，`/settings` 在 1440x1100 和 390x844 通过；覆盖 `设置 / Settings`、tab context、`通知通道状态`、masked Telegram token、runtime toggle、channel add modal、advanced override/retention、save footer，无 console/runtime/network/HTTP error。
- Caveat：浏览器 sanity 使用 mock `/api/auth/me`、`/api/dashboard`、`/api/settings`，未连接真实 authenticated center/PostgreSQL；本机 verify 保留既有 npm `EBADENGINE`（Node v24.14.1 vs web requires 22.x）和 1 moderate npm audit warning。

## Definition of Done

- PRD、research、implement/check JSONL 完成并归档。
- 任务通过 `trellis-implement` 实现与 `trellis-check` 独立检查。
- 前端通过 `npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- 最终完整验证优先跑 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- UI/浏览器 sanity 覆盖 SettingsPage golden path；如果无法连真实后端，明确 fixture/mock caveat。
- 按分支/PR/release 约定完成后续流程。

## Out of Scope

- 不改后端模型/API、settings repository、settings migrations 或 notification delivery worker。
- 不改 Telegram/Feishu secret 持久化语义、masked token summary、unchanged `bot_token` omit payload 行为或 runtime delivery toggle 语义。
- 不改通知实际发送、测试发送、incident delivery 行为。
- 不做 per-tab save、partial PATCH、autosave、optimistic save、频道 CRUD 模型或新状态管理库。
- 不新增/删除通知通道类型。
- 不改 incident threshold、override rule schema、frequency tier、retention policy 的业务语义。
- 不纳入 EventsPage、DashboardPage、AssetDecisionsPage、VPSPage、NodeOnboardingPage 或其他 IA 页面。
- 不更新公开设计/spec 文档；如需把 tabbed Settings 设为权威，另开 spec-update。

## Technical Notes

- Current task: `.trellis/tasks/05-20-settings-page-limited-ia-cleanup`。
- Likely files: `web/src/pages/SettingsPage.tsx`, `web/src/pages/SettingsPage.test.tsx`, `web/src/pages/settings/TelegramSettingsSection.tsx`, `web/src/pages/settings/FeishuSettingsSection.tsx`, `web/src/pages/settings/SectionIntro.tsx`, `web/src/pages/settings/OverrideRulesSection.tsx`, `web/src/pages/settings/IncidentDefaultsSection.tsx`, `web/src/styles/pages.css`。
- Settings inline style debt is explicitly called out in `.trellis/spec/web/styling-guidelines.md` and should be reduced without creating a new styling system.
