# v0.79.0 首次 Agent 接入链路审计

## 版本与复现边界

- 当前工作树基于 `main` 的 `d77abc69`；`v0.79.0` 为 `5eceb02f`。
- `v0.79.0..d77abc69` 之间没有修改 VPS 详情或监控接入产品代码，因此当前代码可用于定位部署版本中的缺陷。
- 用户截图对应“已有 VPS、监控实例为 0、VPS Overview capability 开启”的状态。

## 根因

1. `web/src/pages/VPSDetailPage.tsx:124-131` 在 overview 响应含 `records_v2_read` 时挂载新版 `VPSOverviewRoute`，不再挂载 `LegacyVPSDetail`。
2. `internal/center/vpsoverview/anomalies.go:69-76` 把 `monitoring.unlinked.v1` 的主动作固定为 `open_monitoring_instances`，用户文案为“关联监控”。
3. `web/src/pages/vps-detail/vpsOverviewDestination.ts:77-80` 把该异常动作映射为 `open_monitoring_instances`；`VPSOverviewPageView.tsx:73-75` 只打开 `monitoring-instance-evidence`。
4. `VPSOverviewRelationPanels.tsx:143-160` 又把 monitoring relation section 以 `readOnly` 和全部 `noop` callback 渲染，因此弹层只能查看现有关系，既不能创建监控实例，也不能启动 Agent onboarding。
5. 旧详情页已经拥有完整正确流程：`LegacyVPSDetail.tsx:823-833` 按 active link 数量在创建、复用现有实例、人工处理重复链接之间分流；`LegacyVPSDetail.tsx:1198-1233` 通过既有 VPS-scoped create API 创建并进入 onboarding。

结论：这是新版 Overview 管理动作迁移遗漏，不是缺少后端创建 API，也不是用户配置错误。

## 既有合同

- `.trellis/spec/web/state-and-data.md:313-357` 要求普通 VPS Agent 接入按 0/1/多 active links 分流：0 条创建并接入，1 条复用升级，多条人工核对。
- `.trellis/spec/web/state-and-data.md:908` 要求 VPS 详情是补齐监控接入的主路径，成功后进入 `/monitoring/{id}?onboarding=1&return_vps={vps_id}`。
- `.trellis/spec/web/state-and-data.md:984-1002` 明确把“无监控实例时创建并接入”列为 Good case，并禁止让用户先去监控列表创建再回 VPS 关联。
- `docs/design/current/component-patterns.md:44-54` 将 VPS 详情定义为资产事实、关联和本地工作台 owner；Monitoring 列表负责运行对象扫描，不应复制资产创建工作流。

## 监控页现状

- `MonitoringHero.tsx:32-36` 的“从 VPS 接入 agent”只导航 `/vps`。
- `MonitoringInstancesListSection.tsx:97-108` 在监控列表为空时无条件显示“创建第一台 VPS”，即使库存中已经存在 VPS。
- Monitoring page 不需要新增 MonitoringInstance 创建 owner；更一致的做法是把两个入口统一导向未关联 VPS 库存，再由 VPS 详情承接创建/onboarding。

## 测试缺口

- Legacy tests 已覆盖 0/1/多 active links、创建成功、刷新失败后继续 onboarding 和写入 ownership。
- Overview tests 只断言 `open_monitoring_instances` 打开 read-only evidence panel；没有覆盖 0-link 创建、1-link 复用或多-link fail-closed。
- 需要从用户任务建立 RED：在 Overview capability 开启时，从“未关联监控实例”主动作可以完成创建并进入 onboarding，且 Monitoring 空状态不会误导已有 VPS 用户重复创建。

## 基线验证

- Node 22.23.1 下 Web 全量 Vitest：205 个测试文件、1570 项测试通过。
- `go test ./...` 的监控相关包通过；全量仅有无关的 `internal/center/attachments` PNG golden digest 失败（实际 `0d749f...`，期望 `dac4e6...`），应作为预存基线问题与本任务验证分开记录。
