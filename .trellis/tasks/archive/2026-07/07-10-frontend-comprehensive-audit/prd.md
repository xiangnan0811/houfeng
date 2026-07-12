# 前端全方位审查与修复规划

## Goal

对 `web/` 前端开展一次覆盖代码、运行态和交付链路的深度审查，识别近期视觉与结构优化后仍然存在的系统性问题，并形成一份可按优先级、依赖关系和验证标准执行的修复方案。

本任务只产出审查证据与修复规划，不修改前端业务代码。

## Confirmed Facts

- 当前前端是 React 19 + TypeScript + Vite 的单页应用，源码位于 `web/`。
- 近期工作集中在视觉重构、资产决策工作台、设置与登录页修补，以及大型 CSS 文件的语义拆分。
- 本任务已在非 main 分支 `codex/frontend-comprehensive-audit` 上开展；业务代码保持未修改，任务目录是当前唯一 Git 变更。
- 用户明确要求“全方位审查、全面排查潜在问题、详细修复方案”，未要求本轮直接实施修复。
- 质量基线：`NODE_ENV=test npm run lint` 通过；74 个测试文件、578 个测试通过；production build 通过；`npm audit --include=dev` 为 0 vulnerabilities。
- 已用真实 Center CSP、仓库 mock contract 和 Chromium 抽查 13 条核心路由，覆盖 `1440x1000` 与 `390x900`；路由均非空白且无 document 级横向溢出，但每条路由都有 CSP violation。
- 已确认 7 个 P1：嵌套 Modal 栈行为、Dashboard 异常重复计数、Dashboard false-empty、Shell 健康语义与 freshness、Dashboard 信息架构/测试契约漂移、CSP 资源不兼容、`verify-web` 环境继承。
- 前端规模为 237 个生产 TS/TSX 文件、45,609 行；测试 27,957 行；全局 CSS 源码 435,865 bytes（加 Login route CSS 后约 440 KB）、3,044 rules、11,892 declarations；生产主 CSS 415,864 bytes。

## Requirements

- 审查前端架构、组件边界、状态与数据流、API 契约、类型安全、错误与并发处理。
- 审查视觉一致性、响应式布局、交互反馈、空/错/加载状态、键盘操作、焦点管理和读屏语义。
- 审查 CSS 可维护性、设计令牌使用、重复/冲突规则、硬编码和遗留兼容层。
- 审查测试有效性与盲区、lint/type-check/build、依赖安全、产物体积和运行时性能。
- 结合桌面与移动视口执行浏览器运行态检查，记录控制台、网络、布局和核心流程问题。
- 所有结论必须区分“已证实问题”“风险/建议”和“受环境限制未验证项”，并提供文件或命令证据。
- 修复方案必须包含严重度、影响、根因、修复边界、依赖顺序、验证方式、回滚点与分阶段路线图。

## Scope

- `web/src/**`、前端配置、依赖与构建产物。
- 与前端契约直接相关的 center HTTP API、认证、SPA 托管和安全响应头。
- 当前设计规范、近期前端任务与相关提交。

## Implementation Program Governance

- 用户已于 2026-07-10 审阅并批准实施方案，同意使用本任务作为 parent、十个 independently verifiable child tasks 承载修复。
- 本 parent 保存源需求、问题映射、依赖、Gate A/B/C 与最终集成证据；不直接承载业务代码修改。
- children 按质量门、Modal、Dashboard、Shell、CSP、可访问性、响应式、Asset 领域拆分、CSS owner、质量 ratchet 划分。
- child 依赖写入各自 `prd.md` / `implement.md`；只有前置任务合并并验证后才可启动依赖任务。

## Out of Scope

- 本轮不修改业务代码，不提交功能修复。
- 不对与前端无直接契约关系的后端内部实现做全面审计。
- 不把纯主观审美偏好当作缺陷；视觉结论需对应一致性、可用性或既有设计契约。

## Acceptance Criteria

- [x] 前端 lint、TypeScript build、单元测试、生产构建与依赖审计均有可复现结果。
- [x] 所有路由和关键交互至少完成代码级审查；核心页面完成桌面与移动端运行态抽查。
- [x] 覆盖功能正确性、架构、可维护性、可访问性、响应式、性能、安全、测试与交付链路。
- [x] 每个确认问题包含严重度、证据、用户/工程影响、根因和可执行修复建议。
- [x] 提供按 P0/P1/P2/P3 排序、带依赖关系与验证门槛的分阶段修复路线图。
- [x] 明确残余风险、环境限制和需要在真实部署环境补做的检查。
- [x] 复杂任务的 `design.md` 与 `implement.md` 完整、无占位项、无相互矛盾。
- [x] 十个 child task 已定义唯一问题映射、依赖、验收和回滚边界。
- [x] 所有 child task 完成后，Gate A/B/C 在同一集成版本通过并保存 staging 证据。

## Notes

- Keep `prd.md` focused on requirements, constraints, and acceptance criteria.
- Lightweight tasks can remain PRD-only.
- For complex tasks, add `design.md` for technical design and `implement.md` for execution planning before `task.py start`.
