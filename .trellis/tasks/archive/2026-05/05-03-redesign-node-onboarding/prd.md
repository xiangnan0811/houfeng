# 重设计接入工作台（NodeOnboardingPage）

## Goal

把节点接入工作台从"3 KPI 卡 + 静态文字描述 + 普通 token 文本"改造为 v2 spec 描述的"4-phase Stepper 进度可视 + token mono+复制按钮+保存关闭+dim card 警示 + 安装步骤模板替换 + 绑定冲突 ActionConfirmationCard"形态。

**核心矛盾**：
1. **接入流程不可视**：4 个 phase 当前只用文字描述，用户看不到自己在哪一步
2. **Token UX 落后**：纯文本无复制按钮；component-spec 写的"复制 + 保存关闭 + dim card"全部缺失
3. **安装步骤无用**：当前是静态 markdown 列表，不替换 token / server_url，用户看完还要去查文档
4. **绑定冲突动作无确认**：当前是裸 button 三连，无 ActionConfirmationCard，与节点详情运行控制风格不一致

## Background

- 详见 [`research/codebase-onboarding.md`](research/codebase-onboarding.md)：现状审计、后端 onboarding 数据契约、设计权威、原子库存、token 安全约束分析
- 前置任务 `05-03-redesign-node-pages` 已完成，建立的 v2 模式（`<Hostname>` `<Timestamp>` `<MonoDigits>`、`Card cardRole='warning'`/`'dim'`、`DetailSection ribbon`、`ActionConfirmationCard`）可直接复用
- 后端 onboarding 契约（**本任务不改**）：`GET /api/nodes/{id}/onboarding` 返回 `NodeOnboardingState`（含 phase / has_host_sample / has_accepted_observation / enrollment_token_issued_at / current_binding_fingerprint_summary / pending_binding 等），`POST /enrollment-token` 返回 `{token, issued_at}`
- 设计权威：`docs/design/v2-houfeng/component-spec.md` §五 NodeOnboardingPage（"cardRole='warning' ribbon top critical / token mono + 复制 + 倒计时 + 保存关闭 / dim card 关闭后警示"）— 本任务对其做修订（见 ADR-lite）
- 业务规则：`docs/design/v1-baseline/rules-and-interaction.md` §7.1（Node 创建后进入接入准备页，展示 token / 绑定状态 / 接入指引 / 最近一次接入尝试状态）

## Decisions (resolved Open Questions)

- **Q-BACKEND 后端是否改动** — **1（完全不改后端）**
  - `server_url` 用 `window.location.origin` 前端派生（候风是 reverse proxy 后单用户工具）
  - 不做 token 倒计时（无 TTL，会话级一次性语义无法支撑倒计时）
  - component-spec §五"倒计时"那条修订为"会话级一次性 + 关闭/折叠"语义说明
- **Q-STEPPER 4-phase 形态** — **1（新建通用 `<Stepper>` 原子）**
  - 落 `web/src/components/atoms/Stepper.tsx` + 测试 + atoms.css
  - props：`steps: { label: string; state: 'pending' | 'current' | 'done' | 'error' }[]`
  - MVP 只实现 horizontal + non-clickable；未来加 vertical / clickable 再扩 props
- **Q-CLOSE token 关闭按钮语义** — **1（仅 UI 折叠 + dim card，cache 不清）**
  - 点"已保存，关闭" → token 明文区块折叠为 dim card + 警示"已隐藏，本会话内可重新展开"
  - 默认折叠态时显示 ghost button "重新展开 token 明文"
  - cache 不清（避免误点代价：旧 token 作废 + agent 重发）

## Requirements

### Stepper 原子（新增）

1. 文件：`web/src/components/atoms/Stepper.tsx` + `Stepper.test.tsx`
2. 通过 `web/src/components/atoms/index.ts` barrel 导出
3. props：
   ```ts
   type StepState = 'pending' | 'current' | 'done' | 'error'
   type StepperStep = { label: string; state: StepState }
   interface StepperProps {
     steps: StepperStep[]
     ariaLabel?: string
     className?: string
   }
   ```
4. 视觉（horizontal）：每个 step = 圆点 + 下方 label；step 之间用 1px 连接线（连接线左半反映前一步、右半反映后一步状态色，简化为整段取较前的颜色）
5. 状态色映射（按设计语言 §6）：
   - `pending` → 远岚灰 (`--color-state-offline`)
   - `current` → 晨晖金 (`--accent`) + 1.5px stroke 强调
   - `done` → 松青 (`--color-state-normal`)
   - `error` → 绛红 (`--color-state-critical`)
6. label 字体：sans 11px，状态对应色；当前 step 加粗
7. 测试用例 ≥ 4：默认渲染、状态色映射、错误态、空 steps 占位

### NodeOnboardingPage 重构

1. **Hero 区**保留现有节点身份 + 状态 badges 行（`<Hostname>` 包 node_id）
2. **Phase 进度区**（新增）：`<Stepper>` 显示 4 个固定步骤：
   - 「未开始接入」/「等待绑定」/「等待稳定观测」/「接入完成」
   - 当前 phase 计算逻辑：
     - `binding_status === '指纹变更待确认'` → 第 2 步标 `error`、其后 pending
     - `binding_status === '未绑定'` → 第 1 步 `current`、其后 pending
     - `binding_status === '已绑定' && !has_accepted_observation` → 第 1-2 步 done、第 3 步 `current`
     - `binding_status === '已绑定' && has_accepted_observation` → 全 done
3. **绑定冲突 section**（条件性渲染，最高优先级）：
   - 用 `<DetailSection ribbon="critical">` 包
   - 内部结构：
     - 当前已绑定指纹 / 待确认指纹 / 首次出现 / 最近出现 / 尝试次数 — 全部用 `<Hostname>` `<Timestamp>` `<MonoDigits>` 包装
     - 三个动作（确认重绑定 / 拒绝新指纹 / 重置绑定）改用 `<ActionConfirmationCard>` 包装，每个动作展示「当前 → 操作后」状态迁移、「会发生」「不变」两行 callout
4. **Token 区块**：
   - 用 `<DetailSection>`（ribbon='accent'）+ 内层 `cardRole='warning'` Card（critical ribbon top）
   - **明文展开态**：
     - `<MonoDigits>` 包 token 字符（mono 字体 + tabular-nums）
     - 复制按钮：复用或新增 `<CopyButton>`（不抽原子；inline 在 token 区块内）— 内部用 `navigator.clipboard.writeText` + fallback 到 `document.execCommand('copy')` + textarea 选中
     - 复制反馈：按钮文字临时变 "已复制 ✓" 持续 1.5s
     - 「已保存，关闭」按钮：点击后切换为折叠态，本会话内 cache 不清
     - aside meta：`<Timestamp value={issued_at} mode="relative">` 显示生成时间（hover 显 absolute）
     - 文案："请在本次会话内完成安装。关闭后本会话仍可重新展开。"
   - **折叠态（dim）**：
     - `cardRole='dim'` 弱化背景
     - critical 文案："Token 明文已隐藏。本会话内可重新展开；离开页面后需要重新生成。"
     - ghost button "重新展开 token 明文"（点击恢复展开态）
   - **未生成 token 时**：empty-state + 「生成接入 Token」primary button
   - **生成失败时**：`card--warning` 错误卡（按 design-language §7.2）+ mono 错误摘要 + 重试按钮
5. **安装步骤区**：
   - 模板化展示，**前端派生** `serverUrl = window.location.origin`、`token = tokenIssue?.token`
   - 步骤改为：
     ```
     1. 在服务器上安装 houfeng-agent（参考部署文档）
     2. 创建 systemd 环境文件 /etc/houfeng-agent/agent.env：
        HOUFENG_AGENT_SERVER_URL=<serverUrl>
        HOUFENG_AGENT_TOKEN_FILE=/etc/houfeng-agent/token
     3. 把 token 写入 /etc/houfeng-agent/token：
        echo '<token 或占位 ###TOKEN###>' > /etc/houfeng-agent/token
     4. 启动服务：systemctl enable --now houfeng-agent
     5. 返回本页面，等待首次同步与绑定完成（约 1-5 分钟）
     ```
   - env 行 / token 行用 mono 等宽 + 复制按钮（每段一个）
   - token 在折叠态或未生成态时，模板里的 token 位置显示占位 `###TOKEN###` + 灰提示"生成 token 后此处自动填入"
6. **状态反馈区**：删除（信息已被 Stepper + section aside 取代）
7. **页面状态机**：
   - Loading：mono `"正在加载…"` 文案（按 §7.1 不做骨架屏）
   - Error：`card--warning` + 重试
   - 404：empty-state + 返回节点列表 link
   - 节点状态外部变化（agent 已 enroll 但本页未刷新）：不做实时 polling；在底部加 mono 小字 "数据快照时间：YYYY/MM/DD HH:mm，刷新页面获取最新"

### Mono 包装

- 节点 ID `<Hostname truncate>` 包
- 所有时间戳 `<Timestamp>` 包
- token / fingerprint / 数字（attempt_count 等）`<MonoDigits>` 包
- 安装步骤的 env 行 / shell 命令保持已有 mono 字体

## Acceptance Criteria

- [ ] 新增 `Stepper` 原子，至少 4 个测试用例覆盖（pending / current / done / error / empty）
- [ ] NodeOnboardingPage 顶部显示 4-phase Stepper，状态映射逻辑正确（4 种 binding/observation 组合）
- [ ] 绑定冲突 section 三个动作改用 `ActionConfirmationCard`
- [ ] Token 明文区块包含复制按钮，点击后浏览器剪贴板含 token，UI 反馈"已复制"
- [ ] 「已保存，关闭」按钮点击后区块折叠为 dim card + 警示文案，可重新展开
- [ ] 安装步骤的 server_url 自动填 `window.location.origin`；token 行在 token 已生成时显示真值，否则显示 `###TOKEN###` 占位
- [ ] 节点 ID / token / fingerprint / 时间戳 / 数字全部 mono 包装
- [ ] 复制按钮在不支持 `navigator.clipboard` 的环境降级到 `document.execCommand('copy')` 不抛错
- [ ] Token 生成 API 失败显示 `card--warning` 错误卡 + 重试按钮
- [ ] 节点 404 显示 empty-state + 返回链接
- [ ] 现有功能零回归：token 缓存策略、绑定冲突状态机、phase 计算
- [ ] `make verify-go`（应无影响）+ `cd web && npm run lint && npm run test && npm run build` 全绿

## Definition of Done

- TypeScript strict 0 error
- ESLint 0 warning
- vitest 全 pass，新增覆盖：Stepper 单元、Token 折叠/展开切换、复制按钮、模板替换正确性、绑定冲突 ActionConfirmationCard 集成
- `docs/design/v2-houfeng/component-spec.md` §五 NodeOnboardingPage 段落同步实施细节（特别是 token "倒计时 → 折叠/展开" 修订）
- `docs/release/v1-gap-checklist.md` gap #17 标 closed

## Out of Scope

- 后端任何改动（不加 server_url / 不加 TTL / 不改 onboarding handler）
- 多种安装方式（docker / k8s helm / manual binary）— 只做 systemd 主路径
- 自动跳转节点详情（接入完成后保留 link，不做自动 redirect）
- Stepper vertical / clickable 模式（留给未来真有需求时扩 props）
- 节点列表 / 节点详情 / TargetsPage / Dashboard 改造（其他 follow-up gap）
- 引入图表库
- 实时 polling onboarding 状态（保持当前"页面打开 = 静态快照"的简单模型）
- 国际化 / 移动端响应式

## Technical Approach

**实现路径（小 PR 拆分）**：

- **PR1：Stepper 原子 + Phase 进度区**
  - 新建 `Stepper.tsx` + `Stepper.test.tsx` + `atoms.css` Stepper 段
  - 加到 atoms barrel
  - NodeOnboardingPage 顶部加 Phase 进度区，删除"当前阶段"KPI 卡（信息冗余）
  - 测试：Stepper 单元 + Page 集成（4 种 phase 状态映射）

- **PR2：Token 区块 + 复制按钮 + 折叠/展开 + 安装步骤模板**
  - Token 明文区块改用 `Card cardRole='warning' ribbonPlacement='top'`
  - 内联 CopyButton（非原子，token 区块和安装步骤共享一个组件）
  - 「已保存，关闭」按钮 + 折叠态 dim card + ghost "重新展开"
  - 安装步骤模板：`window.location.origin` + token 占位
  - 边界态：复制 fallback、生成失败错误卡
  - 测试：折叠/展开切换、复制按钮调用 `navigator.clipboard.writeText` mock、模板替换

- **PR3：绑定冲突 ActionConfirmationCard 重构 + 文档同步**
  - 三个动作（confirm/reject/reset）各包一个 ActionConfirmationCard，描述状态迁移与影响
  - mono 包装兜底（fingerprint / first_seen / last_seen / attempt_count）
  - 同步 `component-spec.md` §五（修订 token 倒计时为折叠/展开 + 加 ADR-lite 链接）
  - 关闭 `v1-gap-checklist.md` gap #17
  - 测试：绑定冲突动作触发 + ActionConfirmationCard 渲染

> 注：实际是否拆 3 个 PR 取决于实施时验证粒度，最终可能合一两个 PR 提交。

**关键技术点**：

- Phase 计算逻辑封装为局部纯函数 `derivePhaseSteps(onboarding) → StepperStep[]`，方便单元测试
- CopyButton 不抽原子，但用 hook `useCopyToClipboard()` 封装 navigator.clipboard / execCommand fallback + copied 状态
- Stepper 视觉：用 CSS grid 4 列等宽，圆点用 SVG（与 StatusGlyph 一致）
- Token 折叠/展开用本地 useState (`tokenCollapsed: boolean`)，初始 false；不持久化到 localStorage（关闭语义只在 visit 内）
- 安装步骤的 token 占位：`token ?? '###TOKEN###'`，灰色显示

## Decision (ADR-lite)

**Context**：候风 NodeOnboardingPage 当前与 v2 spec §五描述存在显著漂移。但 spec 中"token 倒计时"在当前架构下无法实现 —— 后端没有 token TTL 概念，token 是"会话级一次性"（localStorage cache + issued_at 匹配检查）。此外 spec 写的"关闭后无法再获取"如严格执行（清 cache）会导致误点代价过高（旧 token 作废 → agent 重新部署）。

**Decision**：
1. 不改后端。`server_url` 用 `window.location.origin` 派生；token TTL 不引入。
2. v2 spec §五 NodeOnboardingPage 段"token 倒计时 + 关闭后无法再获取"修订为"token 会话级一次性 + UI 折叠 dim card 警示"，由本任务 PR3 同步到 spec 文档。
3. 新增通用 `<Stepper>` 原子（仅 horizontal + non-clickable，YAGNI 不预留 vertical/clickable）。
4. 绑定冲突动作改用 `ActionConfirmationCard`，与节点详情运行控制风格一致。

**Consequences**：
- 收益：纯前端任务、PR 量小、风险低；token UX 实质改善（复制按钮 + dim card 警示 + 模板替换让用户体验对齐 v2 主旨）；新增 Stepper 原子奠定未来 wizard 流程基础
- 取舍：spec 字面"倒计时"未实现；折叠态 cache 不清，与 spec "关闭后无法再获取" 不严格匹配
- 风险：`window.location.origin` 在某些反代下可能与 agent 实际能访问的 URL 不一致（在 localhost 测试时 origin 会是 `http://localhost:5173`），需要 page 上加一行小提示说明"请按实际公网 URL 调整"
- 未来：如真出现多 center 部署 / 严格 token TTL 安全需求，再发起 follow-up 任务加 `HOUFENG_PUBLIC_URL` env + token TTL（届时 token UX 可加真倒计时）

## Technical Notes

**关键文件**：
- 改造：`web/src/pages/NodeOnboardingPage.tsx`（445 行）
- 新增：`web/src/components/atoms/Stepper.tsx` + `Stepper.test.tsx`
- 调整：`web/src/components/atoms/index.ts`（barrel 加 Stepper 导出）
- 调整：`web/src/styles/atoms.css`（加 `.stepper` 样式段）
- 复用：`web/src/components/ActionConfirmationCard.tsx` / `web/src/components/atoms/{Mono,Card,Badge,Button}.tsx` / `web/src/lib/onboardingTokenCache.ts`
- 同步：`docs/design/v2-houfeng/component-spec.md` §五 + `docs/release/v1-gap-checklist.md` #17

**测试文件**：
- `web/src/components/atoms/Stepper.test.tsx`（新增）
- `web/src/pages/NodeOnboardingPage.test.tsx`（已有，需扩展）

**设计权威**：
- `docs/design/v2-houfeng/design-language.md`（§3.2 字体强制 / §6 状态优先级 / §7 三态规范 / §12 不做的事）
- `docs/design/v2-houfeng/component-spec.md`（§五 NodeOnboardingPage，本任务做修订）

**业务约束**：
- `docs/design/v1-baseline/architecture-data-model.md`（节点 / agent token / fingerprint 业务模型）
- `docs/design/v1-baseline/rules-and-interaction.md` §7.1（Node 创建后接入流程）

**后端契约（不改，仅消费）**：
- `internal/center/http/handlers/node_onboarding.go`
- `internal/center/nodes/types.go`（OnboardingState / EnrollmentTokenIssue / PendingBindingMetadata）

## Research References

- [`research/codebase-onboarding.md`](research/codebase-onboarding.md) — 现状 / 数据契约 / 设计权威 / 原子库存 / token 安全约束分析（630 行结构化报告）
