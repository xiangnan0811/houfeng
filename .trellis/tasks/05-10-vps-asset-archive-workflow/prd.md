# VPS asset archive workflow

## Goal

为 VPS 详情页补齐显式的资产归档与恢复工作流，让已不在当前工作集中的 VPS 可以安全退出日常决策视图，同时保留历史、关联和台账记录。该任务只使用现有 `PATCH /api/vps/:id` 生命周期语义，不新增物理删除、不执行真实数据导入。

## What I Already Know

- 根计划 `houfeng_codex_下一步开发计划.md` 的第一阶段能力已经覆盖 VPS、Provider、Subscription、VPS-Node link、Dashboard asset summary、资产决策工作台与当前状态编辑。
- 用户已明确暂不处理真实 40+ VPS 数据导入问题。
- 后端 `vps_assets.lifecycle_status` 已支持 `archived`，数据库与 store 负责在切到 `archived` 时派生 `archived_at`，从 `archived` 切出时清空 `archived_at`。
- VPS 列表已有 `lifecycle_status=archived` 筛选能力。
- VPS 详情页当前可通过“基础信息”表单间接改生命周期，但缺少清晰的“归档不是删除 / 可恢复”的安全操作。
- Target 详情页已有 `ActionConfirmationCard` 归档确认模式，可作为交互参考。

## Requirements

- 在 `VPSDetailPage` 的资产操作区增加生命周期操作卡片。
- 对非归档 VPS：
  - 显示当前生命周期、用途状态与归档说明。
  - 点击“归档 VPS”后先展示确认卡片。
  - 确认后通过 `updateVPSAsset(vps_id, { lifecycle_status: 'archived' })` 写入。
  - 成功后刷新详情与资产历史，展示归档状态与 `archived_at`。
- 对已归档 VPS：
  - 显示归档状态、归档时间与恢复说明。
  - 提供“恢复为闲置”操作，通过 `updateVPSAsset(vps_id, { lifecycle_status: 'idle' })` 写入。
  - 成功后刷新详情与资产历史，并清空 `archived_at` 展示。
- 错误信息必须局限在生命周期操作卡片内，不影响续费决策、Node 关联或基础信息编辑表单。
- 新 UI 使用现有 atoms、BEM 类名与 token，不新增依赖、不新建 page 局部 CSS。

## Acceptance Criteria

- [x] VPS 详情页对 active/idle/testing/to_migrate/to_cancel/cancelled 状态展示“归档 VPS”入口。
- [x] 点击归档先出现确认区域，文案明确“不是删除、保留历史、可恢复”。
- [x] 确认归档发送 `PATCH /api/vps/:id`，payload 只包含 `{ "lifecycle_status": "archived" }`。
- [x] 归档成功后页面刷新，header badge 显示“已归档”，详情展示归档时间。
- [x] 已归档 VPS 展示“恢复为闲置”入口。
- [x] 恢复发送 `PATCH /api/vps/:id`，payload 只包含 `{ "lifecycle_status": "idle" }`。
- [x] 归档或恢复失败时显示卡片内错误，归档确认保持可见以便重试。
- [x] `VPSDetailPage.test.tsx` 覆盖归档、恢复和失败场景的 UI 与请求形态。

## Out Of Scope

- 真实 40+ VPS 数据 dry-run/import 执行或生产数据写入。
- 物理删除 VPS、Provider、Subscription 或 link。
- 批量归档 / 批量恢复。
- 新增后端 endpoint、schema、权限模型或专用 lifecycle handler。
- Provider API 同步、DNS 同步、Web SSH、服务发现、Service/Domain 管理。
- 修改 Dashboard、资产决策工作台或订阅页口径，除非为编译和测试必要。

## Technical Notes

- 主要文件：
  - `web/src/pages/VPSDetailPage.tsx`
  - `web/src/pages/VPSDetailPage.test.tsx`
  - `web/src/styles/pages.css`
- 交互参考：Target 归档使用确认模式；本任务在资产操作卡片内使用轻量确认区域，避免 `page-panel` 卡片嵌套。
- 可复用 API：`updateVPSAsset` in `web/src/lib/api.ts`。
- 可复用类型与标签：`VPSLifecycleStatus`、`VPS_LIFECYCLE_STATUS_LABELS`、`LifecycleBadge`。
- 需要遵守 `.trellis/spec/web/{component-conventions,state-and-data,styling-guidelines,quality-guidelines}.md`。
- 本任务没有新增 API、schema、命令或跨层 contract；现有 spec 已覆盖 API client、页面状态、BEM/token 样式与测试要求，本轮无需更新 `.trellis/spec`。

## Definition Of Done

- 代码在 `feat/vps-archive-workflow` 分支完成，不直接改本地 `main`。
- Trellis context 已配置并通过 `task.py validate`。
- 本地至少运行：
  - `git diff --check`
  - `cd web && npm run lint`
  - `cd web && npm run test -- --run src/pages/VPSDetailPage.test.tsx`
  - `cd web && npm run build`
  - `make verify-web`
- 提交工作 commit，执行 Trellis archive/journal。
- 推送分支，创建 PR，监控 CI 全绿后合并。
- 合并后监控 `main` CI，更新本地 `main` 并清理本地任务分支。
