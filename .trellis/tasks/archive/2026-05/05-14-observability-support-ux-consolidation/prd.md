# Observability Support UX Consolidation

## Goal

把 Nodes / Targets / Events 从同级资源中心收敛为“观测证据系统”：保留现有观测能力和 URL-state 深链，但降低筛选器与资源管理感，强化它们对资产判断、异常处理和跨页证据链路的支撑。

## Requirements

- NodesPage 首屏要表达“节点观测是资产判断证据”，并把 linked VPS / 接入状态 / 维护暂停 / 异常作为扫描入口。
- TargetsPage 要表达“入口探测是服务/资产可达性证据”，保留已有筛选和 URL-state，但避免普通 CRUD 资源表感。
- EventsPage 要表达“事件流是审计与诊断时间线”，支持从 Dashboard、VPS Detail、Node Detail 等入口带筛选进入后可见承接。
- 三个页面都要收敛筛选权重：常用状态用 compact chips / quick controls，复杂筛选继续使用现有 drawer 或既有筛选模式，不新增状态库。
- 列表主体保持高密度扫描：对象 identity、状态 glyph/badge、freshness、当前问题、关联上下文和处理入口优先于说明文案。
- 保留现有 API contract、URL query contract、测试数据和路由，不新增后端字段，不引入图表库、CSS 框架或状态管理库。
- 与 AppShell/Dashboard/VPS Detail 已完成的资产决策心智保持一致，Node/Target/Event 不抢占资产主体。

## Acceptance Criteria

- [ ] NodesPage 文案和首屏结构突出资产判断支撑，并保留异常、待接入、暂停/维护等深链入口。
- [ ] TargetsPage 文案和首屏结构突出入口探测证据，并保留异常、暂停、归档等深链入口。
- [ ] EventsPage 文案和首屏结构突出诊断时间线，并可见承接 severity / time_range / maintenance / object 相关筛选状态。
- [ ] 现有 Nodes / Targets / Events 页面测试更新并通过，新增断言覆盖 UX 文案、深链承接和主要队列/列表扫描元素。
- [ ] 不出现 page/component 直连 `fetch()`；业务请求继续走 `web/src/lib/api.ts`。
- [ ] CSS 仍使用 `web/src/styles/pages.css` + BEM + token，不引入 page-local CSS 或新依赖。
- [ ] `npm --prefix web run lint`、相关 focused tests、`npm --prefix web run build` 通过。
- [ ] 启动 dev server 并尝试浏览器 sanity；如本机 Playwright 缺失，记录为环境限制，不新增 repo 依赖。

## Definition of Done

- Trellis implement/check 流程完成。
- 可复用页面/数据 contract 如有变化，同步 `.trellis/spec/` 或 `docs/design/v2-houfeng/component-spec.md`。
- 工作提交完成后再运行 finish-work；finish-work 完成后再 PR、CI、合并、更新本地主分支。

## Technical Approach

- 先读取现有 NodesPage、TargetsPage、EventsPage、相关测试和 v2 component spec，识别已有 shared components（如 ObservabilityEvidenceLead / Focus、EventList、DataTable、FilterBar）可复用点。
- 优先调整页面信息架构、heading、support/evidence surfaces、compact controls 和测试断言；避免大规模业务逻辑重写。
- 对 URL-state 和 API query 只做保持/承接，不扩展后端 contract。
- 三页可以分批实现，但同一 PR 内保持体验语言一致。

## Out of Scope

- 不重写观测系统数据模型。
- 不做大屏监控中心。
- 不新增 Node/Target/Event API 字段。
- 不实现完整服务注册表、DNS 管理或真实数据 import。
- 不做 NodeDetail / TargetDetail 深度重构，除非为列表入口文案或链接一致性做极小调整。

## Technical Notes

- 父级规划：`docs/release/core-pages-product-ux-replan.md` UX-5。
- 视觉权威：`docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`。
- 相关 spec：`.trellis/spec/web/index.md`、component conventions、state/data、styling、quality guidelines。
