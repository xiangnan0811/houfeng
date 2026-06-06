# 资产组合决策工作台 UX 收敛与详情体验重构

## Goal

收敛 /asset-decisions 页面信息架构和详情体验，使资产组合决策中枢从功能平铺升级为清晰的组合发现、场景沉淀、记录回读工作台。

## Requirements

- `/asset-decisions` 首屏必须清楚表达“资产组合决策”的主路径：自动决策组是主 surface，用户优先查看组合问题、上下文筛选、续费窗口和少量高信号指标。
- `下一步导览` 必须保留，但从大面积 dashboard 收敛为紧凑工作导览；只从当前已加载自动组、记录回读、自定义组合和模板派生工作项，加载失败时显示局部不可用，不伪造无问题或真实缺口。
- `single_queue` 不再作为主组合 tabs 的视觉主体；旧 URL `view=single_queue` 必须兼容，并把用户带到单台辅助队列或给出明确锚点入口。
- 场景模板、自定义组合、已保存组合决策必须从三个同权大表收敛为一个低于自动组的“场景与记录”工作区；三者语义必须保持清楚：模板是启动器，自定义组合是用户比较篮子，记录是历史判断与跟进记忆。
- 续费证据区和单台待处理队列必须继续保留，且视觉权重低于组合工作台；单台续费决策仍使用现有 `AssetDecisionWorkPanel` / `PATCH /api/vps/{id}`。
- 记录详情必须继续展示执行回读和执行编排；执行 CTA 只能生成本地深链，不得自动写入 VPS、Subscription、MonitoringInstance 或 Target。
- 移动端和窄屏主路径不得依赖大横向表格完成基本判断；优先使用紧凑卡片、分组和低权重明细区。
- 实施应尽量前端主导，不新增 migration、不新增批量执行、不新增第二套业务状态机、不接入 IP/路由/性能/CPU/IO/超售判断。

## Acceptance Criteria

- [ ] `/asset-decisions?view=needs_decision&renew_within_days=30` 首屏主视觉为决策组列表，顶部只展示紧凑指标、上下文 chips、续费窗口和紧凑下一步导览。
- [ ] 主组合 tabs 不再把 `单台队列` 作为同权 tab；`view=single_queue` 旧链接仍可用，并能引导/定位到单台辅助队列。
- [ ] 页面存在“场景与记录”工作区，模板、自定义组合、已保存记录通过低权重 tab/分组呈现，且不会替代自动组主 surface。
- [ ] 场景模板以启动器形态呈现，仍可打开模板详情并创建自定义组合；模板不会直接创建决策记录。
- [ ] 自定义组合和已保存记录列表仍可打开详情；记录列表继续展示 readback / execution plan 摘要。
- [ ] 记录详情 execution board、成员跟进、CTA URL 映射继续可用；快速跟进只 PATCH asset decision record member followup。
- [ ] 续费证据区和单台待处理队列继续可用；单台队列仍能保存 renewal decision，payload 与现有行为一致。
- [ ] 相关前端测试覆盖首屏主 surface、tabs/query、旧 `single_queue` URL 兼容、场景与记录工作区、记录详情 execution board、单台队列写入和“不得触发业务对象写请求”边界。
- [ ] 桌面和移动端视觉 sanity 通过：页面无横向 body 溢出，主路径不被辅助 surface 淹没，记录详情和 chips 不出现明显重叠。

## Notes

- 当前后端 asset decisions 合同已覆盖本阶段需要的只读数据，默认不改后端。
- 旧 URL-state 包含 `view=single_queue`，实施时必须兼容，不得造成深链失效。
- 用户明确要求先以优秀用户体验为目标，参考之前 VPS、订阅、服务商页面优化思路。
