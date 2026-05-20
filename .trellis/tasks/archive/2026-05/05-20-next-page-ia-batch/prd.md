# NodeOnboardingPage safety-frozen information architecture polish

## Goal

对 `NodeOnboardingPage` 做一轮有限、低风险的信息架构 polish，让节点接入页更清楚地表达“安全地把 Node 从待接入推进到可信 agent sync”的主任务：一键安装保持主路径，绑定冲突成为最高优先级条件工作项，手工回退降级为排障路径，同时严格冻结安装命令、token、绑定冲突和后端 API 契约。

## Selection rationale

- 用户已授权页面 IA 批次由我直接选择最优范围推进，无需确认页面顺序。
- 最近已完成并发布：NodeDetail、VPSDetail、Providers/Subscriptions、TargetDetail、TargetsPage/NodesPage、NodeComparePage、SettingsPage、VPSPage。
- 审计结论：`DashboardPage`、`EventsPage`、`AssetDecisionsPage` 已与 v2 工作台/队列/证据结构高度对齐；`LoginPage` 太小，不适合作为 IA 批次。
- `NodeOnboardingPage` 仍有明确可改善 seam：绑定冲突在进度和样本摘要后才出现；手工回退视觉权重接近主安装路径；样本/观测摘要与 Stepper 语义重复。
- 该页安全敏感，因此本轮只做 display/composition/copy/CSS/test，不改命令生成、token 暴露、绑定状态机或后端数据流。

## Requirements

- 仅纳入 `web/src/pages/NodeOnboardingPage.tsx`、`web/src/pages/NodeOnboardingPage.test.tsx`、`web/src/styles/pages.css` 与必要现有共享组件；不扩展到其他页面。
- 保留当前页面模型：hero、phase Stepper、summary evidence、binding conflict actions、one-command install panel、installer behavior checklist、manual fallback、snapshot meta。
- 默认扫描路径调整为：page identity → highest-priority onboarding work（binding conflict if present, otherwise one-command install）→ progress/evidence context → installer behavior → manual fallback/troubleshooting。
- 当 `binding_status === '指纹变更待确认'` 时，绑定冲突处置必须比普通样本摘要/安装说明更靠前、更显眼，并继续使用 masked fingerprint 和二次确认。
- 一键安装继续作为主路径：生成/重新生成按钮、backend-issued command、expiry/metadata、hide/reveal/copy 操作保持不变。
- 手工回退继续可见但降级为排障/兜底说明，不与一键安装形成同权主路径；placeholder snippets 保持不变。
- 样本/观测摘要应作为 Stepper 后的证据 context，不制造“接入完成”之外的新事实。
- 样式改动进入 `pages.css`，使用 BEM-ish 命名和 tokens；不新增 CSS 系统、page-local CSS 或依赖。

## Frozen Contracts

- Generated install command 只能来自 center：`POST /api/nodes/{node_id}/install-command`。
- Browser 不得从 `window.location.origin`、route params 或 request metadata 拼接生产 install command。
- Frontend 只展示后端返回的 `issue.command` 作为可复制生产命令。
- Full enrollment token 只能出现在用户主动生成并展开的 command reveal/copy surface 内；不得出现在普通摘要、错误、冲突文案、手工回退、日志或截图说明中。
- 手工回退 snippets 继续使用 placeholders：`<30-minute enrollment token>` 与 `<center public base URL>`；不得替换成真实 token、backend command 或浏览器 origin。
- Backend 409 配置错误（例如 public base URL / release version 未配置）继续作为部署配置错误展示，不合成 fallback command。
- Generate/regenerate/hide/reveal/copy 行为保持不变；regenerate 后旧 command 文本必须被替换。
- Binding conflict confirm/reject/reset endpoint、二次确认、pending choice、error handling 和 disabled 行为保持不变。
- Binding conflict copy 继续说明 pending fingerprint attempt 可能已消耗一次性 token，确认/拒绝后可能需要重新生成命令。
- Masked fingerprint display 保持 masked；不展示完整新指纹。
- 不修改 `web/src/lib/api.ts`、`web/src/lib/types.ts`、后端 handlers、token issuing、enrollment semantics、installer contract、data model 或 migrations。

## Acceptance Criteria

- [x] `NodeOnboardingPage` 首屏/默认扫描路径更清楚：用户先看到当前节点身份和最优先接入工作，再看到进度/证据/安装说明。
- [x] 指纹变更待确认时，绑定冲突处置被提升为最高优先级条件区，但确认/拒绝/重置 API 与二次确认语义不变。
- [x] 一键安装仍是主安装路径；生成、重新生成、隐藏、重新展开、复制、metadata 展示和错误处理保持不变。
- [x] 手工回退保留 placeholder snippets，并作为 troubleshooting/fallback 低权重区域展示；不得出现真实 token 或 browser-origin command。
- [x] 样本/观测证据作为 context 表达，不引入新的接入状态或完成判断。
- [x] `NodeOnboardingPage.test.tsx` 更新覆盖新增 IA 文案/结构，同时保留安全契约断言。
- [x] 前端验证通过：focused onboarding test、web lint、full web tests、web build。
- [x] 最终完整验证通过 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- [x] UI/browser sanity 覆盖 `/nodes/:nodeId/onboarding` normal pending、generated command、binding conflict、manual fallback；使用 mock API/inline mocked harness，已明确 caveat。

## Technical Approach

- 保持 `NodeOnboardingPage` 单页本地状态和现有 API client 调用，只调整 JSX composition、section framing、copy 和 CSS class。
- 在 page body 内引入更明确的 onboarding workbench/framing：冲突存在时优先渲染冲突处置；否则让一键安装成为第一主工作区。
- 将样本/观测 summary 从同权 summary cards 调整为 Stepper 附近的 evidence context，避免和 phase Stepper 竞争。
- 将手工回退包装成低权重 troubleshooting/fallback section，保持 snippets 与 copy placeholders 原样。
- 测试以安全契约为主，新增断言只覆盖用户可感知 IA 与 frozen contract 不回退。

## Decision (ADR-lite)

**Context**: 剩余页面中 Dashboard、Events、AssetDecisions 已高度对齐，Login 价值过低；NodeOnboardingPage 是唯一仍有明显 operator workflow 层级改善空间的页面，但它也是安装命令和 token 安全边界页面。

**Decision**: 选择 `NodeOnboardingPage` 做安全冻结的 display-only IA cleanup。提升绑定冲突优先级，强化一键安装主路径，降低手工回退权重，整理进度/证据层级；不改变任何 API、token、binding 或 installer 行为。

**Consequences**: 本轮能减少接入页误读和操作路径混乱，风险由冻结安全契约和测试控制；不会引入新的 onboarding capability、真实安装验证、自动排障或后端状态。

## Research References

- [`research/remaining-page-ia-audit.md`](research/remaining-page-ia-audit.md) — 候选页面矩阵、推荐 NodeOnboardingPage、风险边界和测试建议。
- [`research/design-spec-candidate-audit.md`](research/design-spec-candidate-audit.md) — v2 视觉/IA 标准下的剩余页面对齐度与 NodeOnboardingPage 推荐理由。

## Definition of Done

- [x] PRD、research、implement/check JSONL 完成。
- [x] 任务通过 `trellis-implement` 实现与 `trellis-check` 独立检查。
- [x] 前端通过 focused test、lint、full web tests、build。
- [x] 最终完整验证通过 `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh`。
- [x] UI/浏览器 sanity 覆盖 NodeOnboardingPage golden paths；使用 mock API/inline mocked harness，已明确非真实 authenticated center/PostgreSQL caveat。
- [ ] 按分支/PR/release 约定完成后续流程。

## Out of Scope

- 不改后端模型/API、Node onboarding handlers、agent installer、enrollment token issuing、binding semantics、migrations 或 deploy docs。
- 不改 `web/src/lib/api.ts`、`web/src/lib/types.ts`、NodesPage 创建后跳转 onboarding 流程或 install command request/response shape。
- 不新增真实安装验证、SSH 检查、agent live polling、自动故障诊断、token 管理能力或新安全策略。
- 不把 manual fallback 变成生产 install command，不替换 placeholders，不展示完整 token。
- 不纳入 DashboardPage、EventsPage、AssetDecisionsPage、LoginPage 或其他页面。
- 不引入新依赖、图表库、设计系统、CSS 框架、CSS Modules、CSS-in-JS 或 page-local CSS。

## Technical Notes

- Current task: `.trellis/tasks/05-20-next-page-ia-batch`。
- Target files: `web/src/pages/NodeOnboardingPage.tsx`, `web/src/pages/NodeOnboardingPage.test.tsx`, `web/src/styles/pages.css`。
- Existing tests already cover backend-issued command, no `window.location.origin`, config error, regenerate replacement, hidden command, conflict actions, and manual placeholders; implementation preserved and strengthened them.
- `trellis-implement` completed the display-only IA changes, updated tests, and ran focused test, lint, full web tests, build; full verify initially saw unrelated timeout behavior after `npm ci`, then standalone full web tests passed.
- `trellis-check` fixed three issues: moved mobile onboarding CSS overrides after base rules, added visible conflict warning that pending fingerprint attempts may consume the one-time token, and strengthened tests to assert the full pending fingerprint is not rendered while the masked value is.
- Main-session verification passed:
  - `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run src/pages/NodeOnboardingPage.test.tsx` — PASS, 1 file / 21 tests.
  - `npm --prefix web run lint` — PASS.
  - `TMPDIR="$PWD/.tmp/vitest" npm --prefix web run test -- --run` — PASS, 63 files / 489 tests.
  - `npm --prefix web run build` — PASS.
  - `TMPDIR="$PWD/.tmp/verify-tmp" GOCACHE="$PWD/.tmp/go-cache" ./scripts/verify.sh` — PASS.
- Local verification caveats: npm emitted the existing engine warning because local Node is `v24.14.1` while `web/package.json` requires `22.x`; npm audit still reports 1 moderate vulnerability.
- Browser sanity evidence: `trellis-check` used mock-backed `/nodes/:nodeId/onboarding` states via repo helper and inline mocked harness for pending, generated command, binding conflict, and manual fallback at `1440x1000` and `390x900`; this did not use a real authenticated center/PostgreSQL.
