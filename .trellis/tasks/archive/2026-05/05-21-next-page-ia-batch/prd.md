# TargetDetailPage information architecture polish

## Goal

对 `TargetDetailPage` 做一轮有限、低风险的信息架构 polish，让目标详情页默认视图更清楚地表达“这个入口当前是否健康、由哪些 ProbeItem 支撑、最近观测/异常/事件证据在哪里、运行控制和资料维护如何处理”，同时严格保留 Target、ProbeItem、runtime action、metadata、incident/event/history 的现有 API 与交互契约。

## Requirements

- 仅纳入 `web/src/pages/TargetDetailPage.tsx`、`web/src/pages/TargetDetailPage.test.tsx`、`web/src/pages/target-detail/*`、`web/src/styles/pages.css` 与必要的现有 target-detail 展示组件；不扩展到其他页面。
- 保留当前 `/targets/:targetId` 数据模型：Target identity、ProbeItem 列表与 CRUD Drawer、runtime facts/time window、runtime actions、metadata edit、active incidents、events、history Drawer。
- 默认扫描路径要更清楚地区分：目标身份/健康判断入口、运行控制状态、近期延迟趋势、ProbeItem 工作区、标签备注/生命周期维护、当前异常与事件证据边界。
- 使用现有 Target/Probe/runtime/incident/event 数据改善层级、文案和密度，不新增后端字段、图表库、API 请求或真实服务注册能力。
- 强化 `ProbeItem 列表` 和 `近期延迟趋势` 作为目标观测证据主工作区；不改 ProbeItem 卡片动作、删除确认、编辑/创建 Drawer payload 或 observation 显示语义。
- 使 active incidents / events 在默认页上成为一等证据区；使用现有已加载 `incidents`、`events`、`eventsError`，不新增历史 incident eager fetch，不改变 history Drawer lazy-load 模型。
- 样式改动进入 `pages.css`，使用 BEM-ish 命名和 design tokens；不新增 CSS 系统、page-local CSS 或依赖。

## Frozen Contracts

- API request shapes 保持不变：initial Target detail load 仍使用 `/api/targets/:id`、`/api/targets/:id/probe-items`、`/api/targets/:id/runtime-facts?window=24h`，activity load 仍使用 target-scoped incidents/events；time-window 切换只 refetch runtime facts。
- Target runtime actions 保持不变：进入维护、退出维护、暂停、恢复、归档、恢复到暂停继续调用现有 API helper；暂停/归档的二次确认与 focus restore 语义不变。
- Archive truthfulness 保持不变：归档只退出当前工作集，不删除历史事件、观测记录或 ProbeItem 配置；copy 不得暗示删除。
- ProbeItem create/edit/delete/toggle 合同保持不变：Drawer 打开/关闭重建 draft，payload shape 不变，不支持字段仍阻止安全编辑，删除继续走二次确认。
- Metadata edit 合同保持不变：`updateTargetMetadata` 继续携带 `expectedUpdatedAt` 乐观锁；取消不提交 draft，保存成功只更新 group/labels/note/updated_at。
- History Drawer 合同保持不变：事件 tab 使用已加载 events；历史异常 tab 继续在首次切换时 lazy-load `include_resolved` 数据；关闭/切换节点缓存行为不变。
- Runtime facts evidence boundary 保持不变：没有 latency sample 时显示未知/暂无观测，不把缺样本表达成目标健康事实；维护态只影响视觉 tone，不改判定。
- Active incidents/events failure boundary 保持不变：active incidents/events 加载失败不得阻塞 Target identity、ProbeItem 和 runtime facts 主视图；events error 只在事件证据区/Drawer 里表达。
- 不修改 `web/src/lib/api.ts`、`web/src/lib/types.ts`、后端/API/data model/import flow。

## Acceptance Criteria

- [x] `TargetDetailPage` 默认扫描路径更清楚：用户能先看到 target identity/health/current issue，再进入运行控制、近期延迟、ProbeItem 工作区、当前异常与事件证据。
- [x] `近期延迟趋势` / `ProbeItem 列表` 的 framing 使用现有 count/time-window/sample 信息增强上下文，但 ProbeItem cards、row actions、Drawer、payload 不变。
- [x] 当前异常与事件成为默认可见证据区；空态、错误态和 history Drawer 入口表达清楚，不新增请求或 eager-load 历史异常。
- [x] Runtime actions、archive/restore、ProbeItem CRUD/toggle/delete confirmation、metadata optimistic-lock、history Drawer lazy-load、time-window refetch 行为保持不变并有测试覆盖。
- [x] 样式改动只进入 `pages.css`，遵循 BEM/tokens，不新增 CSS 系统、page-local CSS 或依赖。
- [x] `TargetDetailPage.test.tsx` 更新覆盖新增 IA 文案/结构，同时保留 frozen contract 断言。
- [x] 前端验证通过：`npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- [x] 最终完整验证优先通过：`TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- [x] UI/browser sanity 覆盖 `/targets/:targetId` golden path；使用 local Playwright route fixtures，已明确 caveat。

## Technical Approach

- 保持 `TargetDetailPage` 单页面数据流、state refs、mutation handlers 和 Drawer 状态机不变，只调整 `TargetDetailPageBody` composition、局部 target-detail section framing、copy 与 CSS。
- 在现有 `TargetWatchtowerHeader` 后补强 target-centric overview/summary：健康状态、ProbeItem 数量、当前主问题、最近成功/失败/执行标签等来自 `target` 和 `probeItems` 的既有事实。
- 将 runtime controls、time-window tabs、latency trends、ProbeItem list 组织成更清楚的观测工作区；保留 `TargetLatencyTrends` 和 `TargetProbeListSection` 的内部证据语义。
- 把 metadata/lifecycle 从同权 property-list 改成较低权重但明确的维护区；保留编辑、归档、恢复按钮和二次确认语义。
- 新增/调整当前异常与事件 `DetailSection`，复用 `IncidentList`、`EventList` 与 `onOpenHistory`，只消费已经加载的数据和错误。
- CSS 复用并扩展现有 `.watchtower-*`、`.detail-section*`、`.probe-*`、`.empty-state` hooks，新增类名保持 page-level BEM 命名。

## Decision (ADR-lite)

**Context**: 剩余页面审计和设计/spec 对照都显示 `TargetDetailPage` 是当前最高价值的下一批：它拥有丰富 Target/Probe/runtime/activity 数据，但默认页仍偏 Node/watchtower 混合结构，当前异常与事件证据不够一等，且 v2 component spec 已给出明确 TargetDetailPage section contract。

**Decision**: 采用“Target detail composition/copy/CSS polish only”的有限方案。只强化目标详情页默认扫描路径、观测证据工作区、运行/资料维护层级和 activity 证据区，不改变 API/data/mutation/security 行为。

**Consequences**: 本轮能提升目标详情页可读性和证据边界表达，风险集中在复杂现有状态机的 UI composition；通过冻结请求/payload/focus/drawer/confirmation 合同和强化测试降低回归风险。不会扩展 Target 服务注册、真实业务拓扑、历史查询模型或后端健康判定。

## Research References

- [`research/remaining-page-ia-audit.md`](research/remaining-page-ia-audit.md) — 当前 routes/pages 与近期 IA 归档审计，推荐 `TargetDetailPage` 作为最高价值剩余页面。
- [`research/design-spec-candidate-audit.md`](research/design-spec-candidate-audit.md) — v2 design/spec 对照，确认 `TargetDetailPage` 有明确 contract 且可做 API-preserving polish。
- [`research/browser-sanity.md`](research/browser-sanity.md) — Local Playwright/browser sanity 覆盖 TargetDetailPage desktop 与 narrow viewport golden path。

## Definition of Done

- [x] PRD、research、implement/check JSONL 完成并准备归档。
- [x] 任务通过 `trellis-implement` 实现与 `trellis-check` 独立检查。
- [x] 前端通过 `npm --prefix web run lint`、`TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run`、`npm --prefix web run build`。
- [x] 最终完整验证通过 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- [x] UI/浏览器 sanity 覆盖 TargetDetailPage golden path；明确 mock/local/auth caveat。
- [ ] PR 合并、main CI、Release Please、GitHub release/assets 与 publish-images follow-through 完成。

## Out of Scope

- 不改后端模型/API、Target/ProbeItem repository、runtime facts、incident/event handlers、migrations 或 imports。
- 不改 `web/src/lib/api.ts`、`web/src/lib/types.ts`、API request shapes、query param names 或 payload shapes。
- 不改 ProbeItem kind/config semantics、unsupported config edit boundary、observation/result semantics、runtime health derivation或 notification/incident 规则。
- 不新增 service registry、target dependency map、真实业务拓扑、SLA、执行节点健康 join、跨页 asset service join 或新图表能力。
- 不纳入 TargetsPage、NodeDetailPage、EventsPage、DashboardPage、VPS/VPSDetail、Providers/Subscriptions、Settings 或 docs/specs 更新。
- 不引入新依赖、图表库、设计系统、CSS 框架、CSS Modules、CSS-in-JS 或 page-local CSS。

## Technical Notes

- Current task: `.trellis/tasks/05-21-next-page-ia-batch`。
- Feature branch: `feature/next-page-ia-batch-20260521`。
- Candidate selection completed by two independent `trellis-research` audits; both converged on `TargetDetailPage`.
- Changed files: `web/src/pages/TargetDetailPage.tsx`, `web/src/pages/TargetDetailPage.test.tsx`, `web/src/pages/target-detail/TargetDetailPageBody.tsx`, `web/src/pages/target-detail/TargetProbeListSection.tsx`, `web/src/components/target-detail/TargetActiveIncidents.tsx`, `web/src/components/target-detail/TargetRecentEvents.tsx`, `web/src/components/target-detail/TargetLatencyTrends.tsx`, `web/src/components/target-detail/TargetLabelsAndNote.tsx`, `web/src/styles/pages.css`.
- `trellis-implement` completed the TargetDetailPage composition/CSS/test changes and verified focused TargetDetail tests, lint, full web tests, build, and full repo verify.
- `trellis-check` found and fixed metadata local state consistency for `group`, strengthened optimistic-lock/time-window/history lazy-load contract tests, and removed touched inline styles from `TargetLabelsAndNote`.
- Main-session verification passed:
  - `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run src/pages/TargetDetailPage.test.tsx` — PASS, 1 file / 42 tests.
  - `npm --prefix web run lint` — PASS.
  - `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run` — PASS, 63 files / 490 tests.
  - `npm --prefix web run build` — PASS.
  - `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` — PASS.
- Local verification caveats: npm emitted the existing engine warning because local Node is `v24.14.1` while `web/package.json` requires `22.x`; npm audit still reports 1 moderate vulnerability.
- Browser sanity evidence: `research/browser-sanity.md` PASS via local Playwright route fixtures at `1440x1000` and `390x900`; real authenticated center/PostgreSQL was unavailable, and repo helper's built-in `observability-support` profile does not cover TargetDetail detail endpoints.
