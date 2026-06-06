# Design

## Architecture

本阶段在既有三层模型上增加一个轻量模板层和一个只读解释层：

- 自动组 read model：继续由当前 VPS、订阅、服务、域名、Target、监控 facts 派生。
- 场景模板 layer：负责启动场景，不负责表达当前资产事实。
- 手工组合 scenario layer：表达用户正在比较的真实问题篮子。
- 决策记录 memory layer：保存一次判断、证据快照和执行回读。
- `decision_recommendation`：只读解释层，把 evidence assessment 和 chips 转成中文理由、阻塞点、下一步。

模板不能越过手工组合直接创建决策记录。所有创建 manual group 的路径都必须重新读取当前 facts。

## Backend Contracts

- 新增模板类型：
  - `ScenarioTemplateSummary`
  - `ScenarioTemplateDetail`
  - `ScenarioTemplateMember`
  - `CreateScenarioTemplateInput`
  - `PatchScenarioTemplateInput`
  - `CreateManualGroupFromTemplateInput`
- 模板 ID：
  - 内置模板使用确定性 ID：`adt_builtin_<scenario>`。
  - 自定义模板使用 `ids.New("adt")`。
- 新增 API：
  - `GET /api/asset-decisions/scenario-templates`
  - `POST /api/asset-decisions/scenario-templates`
  - `GET /api/asset-decisions/scenario-templates/{template_id}`
  - `PATCH /api/asset-decisions/scenario-templates/{template_id}`
  - `POST /api/asset-decisions/scenario-templates/{template_id}/manual-groups`
- 自定义模板表：
  - 保存 status、scenario、title、goal、note、source_manual_group_id、created_at、updated_at、archived_at。
  - 成员 blueprint 保存 optional vps_id、intended_role、intended_action、reason、note、sort_order。
  - 不保存当前月成本、订阅状态、监控健康、服务数量等实时事实。
- 扩展列表筛选：
  - `ListFilters` 增加 `ProviderID`、`VPSID`、`Country`、`Region`、`City`、`Scenario`。
  - 筛选应用于列表候选组和 overview 统计；group detail 查找仍用未裁剪的派生组，以保留完整组合上下文。
  - manual groups / records 列表支持相同上下文筛选，筛选方式基于当前 facts 和成员关系。
- `decision_recommendation` 字段：
  - `summary`
  - `next_step`
  - `reasons[] {kind,label,tone,details?}`
  - `blockers[]`
  - `priority_vps_ids[]`
  - `confidence_label`
  - 仅消费 `EvidenceAssessment`、`EvidenceChip`、成员事实计数、scenario 和 group type。

## Frontend Contracts

- API helper 增加 template helpers，并扩展 asset decision filter 类型。
- `/asset-decisions` URL-state：
  - 主筛选：`view`、`renew_within_days`、`provider_id`、`vps_id`、`country`、`region`、`city`、`scenario`。
  - 打开对象：`group_id`、`manual_group_id`、`record_id`、`template_id`。
- 页面行为：
  - URL 打开参数只触发读取和打开 drawer/modal，不触发创建或 PATCH。
  - 清除筛选 chip 时只移除对应 query，不重置其他上下文。
  - 从模板创建组合成功后打开新 manual group 并把 URL 更新为 `manual_group_id=<id>`。
  - 从自定义组合另存模板成功后刷新模板列表并打开 template detail。
- 视觉：
  - 模板 surface 使用紧凑列表/表格，不压过自动组。
  - recommendation 展示为短摘要 + chips + 下一步，不使用大段说明文字。
  - 组详情、自定义组合详情、记录详情在移动端不能横向溢出。

## Compatibility

- 新 migration 只新增模板表，不改变现有记录和手工组合表语义。
- 旧前端未使用新增 query 时，overview/groups 仍保持原行为。
- 旧记录、旧手工组合缺少 recommendation snapshot 时正常降级显示。
- 内置模板由后端返回，前端不 hard-code 模板业务规则。

## Failure Boundaries

- 自定义模板查询失败只影响模板 surface，不影响自动组、手工组合、记录和单台队列。
- 模板创建 manual group 失败时错误留在模板 detail，不伪造 manual group。
- 当前 facts 查询失败时 fail closed，不能把未知事实渲染成缺证据或已对齐。
- 深链对象不存在时显示局部 not found 状态，并允许用户回到列表。
