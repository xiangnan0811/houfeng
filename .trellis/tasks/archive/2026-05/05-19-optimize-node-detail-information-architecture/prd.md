# 优化节点详情页信息结构

## Goal

让节点详情页更像工程控制面板而不是信息堆叠：保留上方 8 个趋势图的监控主路径，并在内存/磁盘趋势图中补充容量上下文；移除下方重复、默认折叠且低价值的信息区，把节点操作集中到右上角 `watchtower-actions-menu`，让用户无需展开次级卡片即可找到关键动作。

## Requirements

- 保留节点详情页当前 8 个趋势图布局与阈值优先级排序。
- 内存使用率趋势图底部右侧展示机器内存总大小（`mem_total_bytes`），同时保留已有 swap / 可用内存信息。
- 磁盘使用率趋势图底部右侧展示根文件系统磁盘总大小（`disk_total_bytes`），同时保留已有 busy 与读写速率信息。
- 容量字段必须来自 agent 真实采样，而不是前端根据百分比和可用值猜测。
- 补齐容量字段的跨层链路：agent host sample → `agentapi.HostSamplePayload` → center sync ingest → PostgreSQL `host_samples` → runtime facts read model → frontend `HostSample` type → `NodeWatchtowerMetrics`。
- 取消节点详情页下方“生命周期”信息区；其中“退役节点”/“恢复到观察中”操作移入右上角 `watchtower-actions-menu`。
- 取消节点详情页下方“接入凭证状态”信息区；其中“打开接入工作台”移入右上角 `watchtower-actions-menu`，绑定状态不重复展示。
- 取消节点详情页下方“标签与备注”信息区；标签、备注只沿用页面上方已有展示，不在节点详情页提供编辑标签与备注按钮。需要修改时交给 VPS/资产侧流程。
- 保留下方必要数据区：关联 VPS、容器列表。它们不应放入横向折叠属性列表，也不应与生命周期/凭证/标签备注混在一起。
- 绑定冲突处理区、危险异常提示、时间窗口切换、历史抽屉、命令抽屉等现有工作流继续可用。

## Acceptance Criteria

- [ ] `/nodes/:nodeId` 的内存趋势图在底部元信息中展示总内存大小，测试 fixture 覆盖该字段。
- [ ] `/nodes/:nodeId` 的磁盘趋势图在底部元信息中展示磁盘总大小，测试 fixture 覆盖该字段。
- [ ] 新 agent 采样 payload 包含 `mem_total_bytes` 和 `disk_total_bytes`，Linux 与 Darwin 采样测试覆盖。
- [ ] `host_samples` 新增容量列并通过 runtime facts API 返回；旧数据缺失时不破坏页面渲染。
- [ ] 节点详情页不再渲染“标签与备注”“生命周期”“接入凭证状态”三个下方折叠区。
- [ ] 右上角 `watchtower-actions-menu` 包含运行控制、打开接入工作台、执行命令、退役/恢复等节点操作入口。
- [ ] 容器列表作为独立非折叠数据区展示，空态仍清晰表达“暂无容器数据”。
- [ ] 关联 VPS 作为独立非折叠数据区保留，空态和跳转到 VPS 库存的入口继续可用。
- [ ] 更新 Go 与前端测试，至少覆盖容量字段跨层输出和节点详情页操作/信息区变化。

## Definition of Done

- Go 代码通过 `make verify-go` 或必要的定向 Go 测试后再跑完整验证。
- 前端通过 `cd web && npm run lint && npm run test -- --run && npm run build`；跨前后端最终优先跑 `./scripts/verify.sh`。
- UI 变更对照 `docs/design/v2-houfeng/design-language.md` 与 `docs/design/v2-houfeng/component-spec.md`，并尽量启动 dev server 做浏览器 sanity；如果本环境无法完成浏览器验证，最终报告明确说明。
- 不提交截图、过程归档或过度文档；PRD 保留在 Trellis 任务目录。

## Technical Approach

- **数据链路**：新增 `mem_total_bytes` / `disk_total_bytes` 为 host sample 原始事实，使用 `bigint` 存储；历史样本没有该字段时用 `0` 表示未知，前端格式化为 `—` 或隐藏具体值，不做反推。
- **采样语义**：Linux 从 `/proc/meminfo` 的 `MemTotal` 得到总内存；Darwin 复用 `sysctl -n hw.memsize`；磁盘容量使用 `statfs("/")` 的 `Blocks` 作为根文件系统总块数。现有 `FilesystemStats` 只有 block count，没有 block size，需要补充 block size 字段以正确计算字节数。
- **UI 信息架构**：`NodeWatchtowerHeader` 扩展为节点操作收口点。菜单项统一在 `watchtower-actions-menu__panel` 内显示；`查看历史` 仍可保留为旁侧高频动作。`NodeDetailPageBody` 删除三个重复 CollapsibleSection，仅保留关联 VPS与容器列表等数据区。
- **测试策略**：Go 侧更新 hostsample/provider、agent handler/store/runtime facts 相关测试；前端更新 `NodeDetailPage.test.tsx` fixtures 和断言，确保移除旧区块并新增菜单操作。

## Decision (ADR-lite)

**Context**: 现状下方信息区把生命周期、接入凭证、标签备注和容器列表横向/折叠排布，重复了 header 已展示的状态信息，并隐藏了“打开接入工作台”“退役节点”等关键动作。趋势图显示百分比但缺少容量上下文，用户难以判断实际资源压力。

**Decision**: 把节点操作集中到右上角 `watchtower-actions-menu`；下方不再作为“属性操作区”，只保留必要数据展示区。容量上下文走真实 agent 采样和 API 链路，不在前端猜测。

**Consequences**: 需要跨 agent、contract、数据库、store、runtime facts、前端类型和 UI 一起修改；历史样本容量未知时前端必须优雅降级。页面更短、更聚焦，代价是右上角菜单承担更多操作，需要测试保证动作仍可发现和可点击。

## Out of Scope

- 不改变 8 个趋势图的数量、排序规则或阈值语义。
- 不新增磁盘分区列表、挂载点切换或多磁盘明细。
- 不在节点详情页编辑标签、备注或资产信息。
- 不改变 Node/VPS 关联模型、Node lifecycle 后端状态机或 agent onboarding 业务语义。
- 不新增第三方前端依赖、CSS 框架或图表库。

## Technical Notes

- 相关前端入口：`web/src/pages/NodeDetailPage.tsx`、`web/src/pages/node-detail/NodeDetailPageBody.tsx`、`web/src/components/node-detail/NodeWatchtowerHeader.tsx`、`web/src/components/node-detail/NodeWatchtowerMetrics.tsx`。
- 右上角菜单现有类名：`watchtower-actions-menu` / `watchtower-actions-menu__panel`。
- 现有下方信息区来自 `NodeDetailPageBody` 的 `watchtower-property-list` + `CollapsibleSection`。
- 容量字段目前不存在于 `web/src/lib/types.ts`、`internal/contracts/agentapi/types.go`、`internal/center/runtimefacts/types.go`、`db/migrations/0001_initial_schema.sql` 和 store 查询中。
- 需要新增迁移号：当前最大为 `0026_tune_observability_cadence.sql`，下一个迁移应为 `0027_*`。
- 相关规范：`.trellis/spec/backend/directory-structure.md`、`.trellis/spec/backend/database-guidelines.md`、`.trellis/spec/backend/quality-guidelines.md`、`.trellis/spec/web/component-conventions.md`、`.trellis/spec/web/state-and-data.md`、`.trellis/spec/web/styling-guidelines.md`、`.trellis/spec/web/quality-guidelines.md`、`.trellis/spec/guides/cross-layer-thinking-guide.md`、`.trellis/spec/guides/branch-workflow-governance.md`。
