# 监控实例详情页重做

- 状态：已审查并修订，尚未实现，不是现行设计规范
- 范围：监控实例详情页 `/monitoring/:id`。列表页 `/monitoring` 和对比页 `/monitoring/compare` 不在本次范围内
- 代码位置：worktree `operator-ui-rebuild`，分支 `feat/operator-ui-observatory`
- 预览：http://127.0.0.1:5366/monitoring/mi_001 （合成预览，标题「东京边缘网关」）
- 对照质量条：同一产品里已重做的 VPS 详情（结构继承，不是配色发明）。对照实现是 `VPSOverviewIdentityHeader`、`VPSManagementMenu`、`web/src/pages/vps-detail/VPSDetailWorkspace.css`。5366 上 `/vps/vps_001` 的 HTML 路由返回 200（SPA 壳），`GET /api/vps/vps_001` 返回 404。不以该预览页为证据
- 日期：2026-09-18；同日按外部审查修订，再按落地核查修订，再按二次审查补 CSS 预算门

本文不替换 `docs/design/current/`，也不表示产品已经把这一页改完。路径相对于 worktree `operator-ui-rebuild`。实现按第 5 节，不要再二选一。采纳并落地后，再改 `docs/design/current/component-patterns.md` 的 “Monitoring detail diagnostics”，不要把那一节当现行目标。

写作与修订时重新读过的是本 worktree 的源码、CSS、现行设计文档，以及 5366 预览。第 4 节的行业共识来自 2026-09 的文档、仪表盘 JSON 与源码笔记；没有重新打开那些产品的截图。文中凡是和口头描述不一致的数字，以文件或预览量测为准，并在该处写明。

## 1. 背景

候风 / Houfeng Fleet Control Plane 是单人操作的机队控制面。界面语言是中文，密度按工程控制台，不是多租户 SaaS 仪表盘。

VPS 是业务对象。`MonitoringInstance` 是挂在 VPS 上、或被显式关联的运行观测证据，不是第二套资产生命周期。健康状态是推导出来的：`正常` / `关注` / `告警` / `严重`。维护是运行控制，不是健康状态。

VPS、订阅、归档等操作页已经按同一套控制台重做过。监控列表大致可用，但不是本文的任务。监控实例详情还没做完，而且结构是错的：它把「现在是否异常」和快照数字、agent 控制、破坏性生命周期混在一页里。

### 审查后锁定的修订

相对同日初稿，下面几条已经选定，写进第 5 节，不再当作开放问题：

- 位置：省略云区域代号，保留 city（ASCII 也可以）。不要用「含中文才显示」。
- 对比页本次不改。详情用新的图表布局组件。`MonitoringInstanceWatchtowerMetrics` 留给 Compare。
- 绑定冲突：状态带下的通知行 + 对话框。对话框里保留指纹对照和三个动作。
- `未绑定` 且未归档时，页头主按钮是接入 agent，不是把它只放进「管理」。
- 「历史」始终出现在事件行，打开现有抽屉。不要只在超过 3 条事件时才给入口。
- `MetricChart` 的详情增补是 opt-in：等长第二序列、告警带、左沟槽宽度、单点可画、空 label 不画阈值文字、双序列一起判定空态。默认行为保持。真实消费者是监控详情、Compare、`MonitoringEvidenceRenderer`。Target 详情用的是 `Sparkline`，不含 `MetricChart`。
- 活跃异常现在会同时画出 DangerCard 和 `TargetActiveIncidents`。详情页两个都不挂，改成一行。
- 当前详情标题已经是 22px，不是 30–36px。质量条剩下的是字重 600、列宽 1600px、间距 14px、以及不要给每张图再套卡片。
- `VPSDetailWorkspace.css` 是 VPS 路由 code-split，监控路由上没有 `--vps-detail-*` 和完整菜单几何。详情新建自己的路由样式表，按值复刻，不要 import VPS 那份 CSS。
- 管理菜单项沿用 `.btn.lg`（15px / min-height 44px）。不要再写一条「相对 28px 加严到 44px」。
- 网络左沟槽 64px。56px 会裁掉 `12.0 MB/s`。
- `toSeries` 一类辅助函数抽到共享模块。`MonitoringInstanceWatchtowerMetrics` 只改 import，不改布局。
- 新增路由样式表必须同步 re-base `web/css-budget.json`（至少 `sourceFilesMax` 31→32，其余指标按重写后实测重设）。当前分支这条门禁已经红。
- 只删监控详情专用、重写后不再挂载的 CSS。`.watchtower-identity*`、`.watchtower-danger*`、`.watchtower-secondary*`、`.watchtower-property-item*` 仍被 Target 使用；`.watchtower-snapshot-meta` 基规则被 `TargetSnapshotMeta` 使用；Compare 与 `TargetLatencyTrends` 仍用 `.watchtower-metrics*`。一律保留。`.watchtower-stream-status--*` 由模板字符串拼接，不要按 grep 当死代码。`.watchtower-observation` 可删。可选删：`.watchtower-status-badge`、`.watchtower-container-image`、`.watchtower-metric-group--notice/--alert/--critical`、`.watchtower-header__meta-item`。
- 只读预览徽章、近期事件圆点用监控自己的类名，不要引用只存在于 VPS 路由 CSS 里的选择器。
- 空态并集只计入**通过校验的**第二序列。

### 现行文字规范描述的是要否掉的布局

`docs/design/current/component-patterns.md` 第 94–102 行，标题为 “Monitoring detail diagnostics”。不要把这一节当成目标。它写的就是本文要改掉的信息架构。该 worktree 当前文本的要点：

- 第 96 行：`/monitoring/:id` 的顺序是紧凑身份/操作头、最近采样事实加一行采样/读取时间、条件性的问题或接入/绑定提示、资源趋势、近期事件、活动/记录/证据入口，然后是折叠的元数据和折叠的管理。
- 第 98 行：默认窗口 24h，且 URL 持久化；只有显式的实时窗口才开 WebSocket；连接状态不是心跳证据。窗口控件放在资源趋势标题旁边，不放进身份头。CPU/负载、内存、磁盘/I/O、网络是四个稳定主组。5 分钟负载、CPU I/O 等待、Inode 放在主网格下面、默认关闭、全宽的 disclosure 里。网络方向 “stack”，共用刻度、可见速率单位、对齐的 hover。
- 第 100–102 行：最新事实来自一条当前绑定样本，独立于历史窗口。阈值标签留在图表沟槽里，且详情页的缩放和标签选项不得悄悄改变 Compare 或证据图。

第 98 行写的是 “stack network directions”，不是 “两张图”。当前实现把它做成了两张高 90px、已经共用 `yMax` 的图（见第 2 节）。书面规范和当前实现都不是目标：窗口不要粘在「资源趋势」标题上，负载 / I/O 等待 / Inode 不要折叠，网络不要拆成两张图。

详情页的缩放、告警带、第二序列不得改变 `MetricChart` 的默认行为。Compare 继续使用现在的 `MonitoringInstanceWatchtowerMetrics`，包括两张网图和折叠的辅助图。

### 今天的组件树

页面主体是 `web/src/pages/monitoring-detail/MonitoringDetailPageBody.tsx`，根节点仍是 `.page`。从上到下：

1. `MonitoringInstanceWatchtowerHeader`：标题、健康/运行/绑定徽章、心跳和采样年龄、`watchtower-identity__meta`、刷新、查看历史、溢出菜单「…」。
2. `MonitoringInstanceLatestSample`：标题「最近样本」，六列事实条。
3. 条件块：暂停确认、运行错误、指标错误、策略不可用、关联 VPS 错误、未绑定提示、`MonitoringInstanceBindingConflictSection`、`MonitoringInstanceDangerCard`、`TargetActiveIncidents`。
4. 「资源趋势」标题与 `MonitoringInstanceTimeWindowTabs` 同一行，下面是 `MonitoringInstanceWatchtowerMetrics`。
5. 事件：`EventList`，外加活动 / 记录 / 证据链接。
6. 折叠的 `MonitoringInstanceMetadataSection`（「标签与备注」）和 `MonitoringInstanceManagementSection`（「管理实例」）。

`web/src/pages/monitoring-detail/MonitoringInstanceContainersSection.tsx` 存在，但 `MonitoringDetailPageBody` 没有挂载它。本文也不挂载。

`MonitoringComparePage.tsx` 第 104 行 aside 文案是「详细趋势仍使用 MonitoringInstanceWatchtowerMetrics」；第 109 行起把同一个组件用在对比页。这是实现边界，不是详情页的布局缺陷。

### 预览和真路由

预览是 `bun .tmp/monitoring-preview-api.ts`，监听 `127.0.0.1:5366`，非 `/api/` 请求从 `web/dist` 取页面。`mi_001` 的展示名被覆盖成「东京边缘网关」（`.tmp/monitoring-preview-api.ts` 第 8 行）。身份字段来自 `web/e2e/fixtures/profiles.ts` 的 `monitoringInstanceDetailProfile`：`group: 'edge'`，`region: 'ap-northeast-1'`，`city: 'Tokyo'`，`provider: 'Example Cloud'`。

管理审查在真中心是存在的。`internal/center/http/router.go` 第 706 行定义 `monitoringInstanceSubtreeManagementReview = "management-review"`，第 750 行把 `/api/monitoring-instances/{id}/management-review` 分到这条子路径。预览对未实现的 GET 返回 404，文案是「合成预览未提供此接口」（`.tmp/monitoring-preview-api.ts` 第 37 行）。这不是待设计的空状态。

`READ_ONLY_PREVIEW`（`web/src/lib/readOnlyPreview.ts`）在 VPS 详情会显示「只读预览」并隐藏管理。监控详情今天不读这个开关。本稿补上，与 VPS 相同。

## 2. 问题

### 真问题

这一页同时干四件事：现在是否异常；证据新不新；agent 运行控制；破坏性生命周期和元数据编辑。阅读顺序没有把前两件放在前面。

「最近样本」是视觉焦点。`MonitoringInstanceLatestSample` 的六列是：运行时间、CPU、内存、磁盘、上行、下行。样式 `.watchtower-factstrip` 默认 `repeat(6, minmax(0, 1fr))`，带表面、边框、圆角（`web/src/styles/partials/legacy-monitoring.css` 第 74 行）。这些是一条 `HostSample` 快照，不是诊断。

标题下面是一堆状态，不是一条身份。`MonitoringInstanceWatchtowerHeader` 用 `HeaderStatusBadge` 画出「健康」「运行」「绑定」三个带前缀的徽章。健康值是 `正常` / `关注` / `告警` / `严重`，没有一个组件名叫「关注」。然后是心跳和采样年龄。再下面是 `watchtower-identity__meta`。操作者说的「标题下那条乱带」就是这一行，不是某个叫「关注」的组件。预览 `mi_001` 上能看见：分组值 `edge`，实例 `mi_001`，位置被 `locationLine` 拼成 `ap-northeast-1 · Tokyo · Example Cloud`，关联是「VPS 未关联」，agent 是 `synthetic-preview`。ID 和代码与名字抢同一行。

`locationLine`（`MonitoringInstanceWatchtowerHeader.tsx` 第 49–55 行）在缺位置或缺服务商时会写出「位置未确认」「Provider 未确认」。这是真文案问题，不是预览偶然。

「资源趋势」和时间窗口粘在一行。`.watchtower-trend-toolbar` 是横向 flex，列间距 `var(--space-3)` = 12px。h2 是 15px / 600 / line-height 1.3（`legacy-monitoring.css` 第 104–105 行）。2026-09-18 在预览视口 1241×1308 量到：h2 60×19.5，窗口控件 216×66。初稿写的约 60×20 和 217×67 按这次量测修正。CSS 已经说明它们共享一行、间距 12px。

网络下行、上行是两张 `height={90}` 的图（`MonitoringInstanceWatchtowerMetrics.tsx` 第 404、421 行）。`yMax` 已经共用：`seriesMax` 取两套 `net_in_bytes_per_sec` / `net_out_bytes_per_sec` 的最大值，再至少为 1（第 200–201 行）。操作者要的是一张图。

5 分钟负载、CPU I/O 等待、Inode 是默认关闭的 `<details class="watchtower-metric-aux">`，展开后是全宽 `height={120}`。主图 CPU / 内存 / 磁盘是 `height={160}`。预览负载窗口末值约 1.0，展开后 Y 轴到 8.0（阈值 4 / 6 / 8），线贴底。详情模式把 `includeThresholdsInScale: true` 和 `thresholdLabelPlacement: 'gutter'` 一起传给图表（第 188–189 行）。默认阈值在 `web/src/config/thresholds.ts` 第 7–12 行：负载 4 / 6 / 8，iowait 20 / 35 / 50。轴被阈值撑开。这不是预览数据特有的。

CPU、内存、磁盘的沟槽阈值标签会叠。`MetricChart.tsx` 第 435–436 行只保证 `minGap = 14`。标签字号是 11px（`legacy-monitoring.css` 第 110 行）。2026-09-18 在 `mi_001` 上量到 CPU 的 80% / 90% / 95% 标签盒高 11px、纵向间距 14px，视觉间隙 3px。默认阈值就会这样，不是预览数据特有的。

图例把快照字段说成趋势。CPU 图的 `titleHint` 写「含 CPU 被窃取时间占比」（第 245 行），序列只有 `cpu_usage_pct`。磁盘使用率图的序列只有 `disk_used_pct`；图下的事实行列出磁盘繁忙、读/写、容量、Inode（第 356–380 行）。那些字段没有进入这张图的历史序列。

底部「标签与备注」和「管理实例」是真功能，但看起来没有职责。元数据 PATCH 和 If-Match 已经存在。管理是破坏性的：退役、归档、永久清理，而且只在展开时才拉 management-review。暂停和维护在标题溢出菜单里，不在这一节。对照：VPS 把危险操作放进分组的「管理」菜单（`VPSManagementMenu`，样式 `.vps-overview-management`）。

英文标签 `Group` 在 `MonitoringInstanceMetadataSection.tsx` 第 55 行，只读描述是「Group：…」（第 98 行）。身份条的 dt 已经是「分组」，问题在这个折叠表单。空元数据展开后是一块稀疏表单。`EventList` 渲染「对象 ID」（`web/src/components/EventList.tsx` 第 111 行）。`TargetActiveIncidents` 在预览 `mi_002` 上同样渲染「对象 ID mi_002」。这一页的对象就是当前监控实例，对象 ID 是重复信息。

活跃异常会画两次。`MonitoringDetailPageBody.tsx` 在 `current_active_incident_count > 0` 时挂 `MonitoringInstanceDangerCard`，又在 `incidentsLoaded && incidents.length > 0` 时挂 `TargetActiveIncidents`。预览 `mi_002` 同时出现「当前主问题」警告 Card 和「当前异常」列表，摘要都是磁盘阈值。

`MetricChart` 只接受一个 `samples: MetricChartSample[]`（`MetricChart.tsx` 第 30 行）。X 按下标投影，不是按时间轴。一张图里画两条网络线，是真的前端前提。两条序列必须等长、共享同一组 `observedAt`。

`MetricChart` 在 `samples.length === 1` 时不画线，只显示「样本不足」（第 244、524、623 行）。实时窗口先有 1 个种子点时，会走进这个分支。

当前详情并不是 30–36px 大标题。`.page:has(.watchtower-identity)`（`legacy-observability.css` 第 314 行）已经把 gap 收到 `var(--space-4)` = 16px；`.watchtower-identity .page__title`（第 317 行）已经是 22px / line-height 1.25。字重仍走 `.page__title` 的 `--type-display-weight` = 700。和 VPS 质量条真正差的是：字重 600、列宽 `min(100%, 1600px)`、间距 14px、每张图还套了 `.watchtower-metric-group` 卡片。不要按「36px 标题」去改一个已经 22px 的 h1。

列表页 `/monitoring` 能用，但不精致。对比页能用，但依赖现在的 `MonitoringInstanceWatchtowerMetrics`。两者都不在本次范围内。不要把列表列宽、筛选、批量、24h 迷你趋势，或对比页的双列趋势，扩进这张详情方案。

### 预览造成、不要设计进去

这些只出现在合成预览里。方案不给它们单独的视觉，也不把它们当成布局缺陷。

- 图表空洞：`.tmp/monitoring-detail-preview.ts` 每个窗口 48 个桶。第 23 行 `j >= 20 && j <= 23`，0 起算下标 20–23 为 null。按 1 起算是第 21–24 个点。缺口渲染本身是对的。不要为了预览里这四个洞去改图。
- 红字「合成预览未提供此接口」：预览对 management-review 返回 404。真路由见第 1 节。不要把这句错误文案设计进页面。
- 「系统摘要已过期」是壳层逻辑（`web/src/app/layout/shellSummaryModel.ts`），不是详情页模块。夹具时间停在 2026-07-10（例如 `web/e2e/fixtures/profiles.ts` 的 `snapshot_generated_at: '2026-07-10T06:28:00Z'`）。不要在详情方案里做一条「摘要过期」横幅。
- 实时窗口先有 1 个种子样本，再靠 WebSocket 追加。这不是布局 bug。新方案不能让「样本不足」占住画面。
- 预览 `mi_008` 的绑定冲突详情经常是空指纹，因为合成 onboarding 没有 pending 字段。真数据仍按现有 `MonitoringInstanceBindingConflictSection` 的字段渲染。不要为预览空指纹发明新空状态。
- 同一预览里 `/vps/vps_001` 的页面壳是 200，`/api/vps/vps_001` 是 404。不要据此给监控页补 VPS 空态。

### 数据事实，不要用虚构图表去修

历史点类型是 `HostMetricPoint`（`web/src/lib/types.ts` 第 260–271 行）。只有这些字段有历史：

- `cpu_usage_pct`
- `mem_used_pct`
- `disk_used_pct`
- `inode_used_pct`
- `cpu_iowait_pct`
- `load_5`
- `net_in_bytes_per_sec`
- `net_out_bytes_per_sec`

没有 `net_in` / `net_out` 这两个短名字。速率单位是 B/s。

只存在于最新 `HostSample`、没有历史序列的字段：`load_1`、`load_15`、`cpu_steal_pct`、`swap_used_pct`、内存和磁盘的字节字段（`mem_available_bytes`、`mem_total_bytes`、`disk_total_bytes`）、`disk_busy_pct`、`disk_read_bytes_per_sec`、`disk_write_bytes_per_sec`、`uptime_seconds`、`containers`、`agent_version`。

没有 CPU 核数。没有按实例查询的 target / probe API；Target 按标签选择执行器。容器区块文件在，本文不挂载。IP 质量属于 VPS，不属于这一页。

无效网络速率保持缺口，不能画成 0。显式的 0 仍是 0。`network_rates_valid` 为 false 或缺失时，对应速率不是 0。`hostNetworkRate`（`runtimeObservation.ts` 第 28–32 行）已经按这个规则把快照速率收成 `number | null`。历史点没有 `network_rates_valid` 字段；无效桶必须在点上就是 `null`，前端不要把 `null` 画成 0。

## 3. 需要

### 操作者

- 网络一张图，同一单位。
- 有历史、且需要看的序列不要藏在折叠里。只有单位相同且历史存在时才合并。
- 最近样本不能领读。
- 状态、心跳、采样不能堆在标题下面。
- 身份显示名字，不显示一串 ID。ID 降级，但不删掉。
- 时间范围是页面级的，不粘在某一节标题上。
- 标签/备注和实例管理要有明确职责，并且不和诊断抢阅读顺序。
- 未绑定实例的第一动作是接入 agent，不能只在菜单里。
- 绑定冲突能处置，不能只变成一句不可点的状态。
- 没有近期事件时，仍然进得了历史抽屉。

### 产品约束

这一页的主任务是三句：现在是否异常，证据新不新，所选窗口里什么变了。

运行控制和破坏性生命周期是次要的，不能占阅读流。`lifecycle_status`（`待接入` / `在用` / `观察中` / `不续费` / `已退役`）不能在页头变成第二套资产生命周期。业务生命周期在 VPS 上。

不要双轴。不要为只有快照的字段画假历史。无效网络速率保持缺口，永远不要画成 0。不要把 CPU 使用率和 iowait 堆在同一条 0–100 的线上：不知道 `cpu_usage_pct` 是否已经包含 iowait。

界面文案用中文。不要出现英文 `Group`。

对比页、列表页、Target 详情、证据图不在本次改动里。`MetricChart` 的现有消费者是监控详情、Compare、`MonitoringEvidenceRenderer`。默认仍是一条序列、单点显示「样本不足」、左沟槽 32px。Target 详情用 `Sparkline`，不在这次回归清单里。

### 从 VPS 详情继承的质量条

对照实现见文首。继承结构，不发明配色。

- 标题是对象名。徽章少。
- 事实是标签/值，不是 KPI 磁贴。
- 页头默认两个动作。未绑定是例外，见第 5 节。危险操作进分组菜单。
- 节标题 h2 15px。内容列 `min(100%, 1600px)`。标题 22px / 600。正文 14px。
- 不为每张图再套一层投影卡片。
- 名字在上，ID 弱。
- `READ_ONLY_PREVIEW` 时出「只读预览」徽章，隐藏写入入口。

## 4. 调研摘要

证据是 2026-09 的文档、仪表盘 JSON 和源码笔记。写作本文时没有重新打开下列产品的界面截图。链接给审查者自己核对。

采纳并改写成候风能做的部分：

- 页头：人能读的名字，加一行次要身份。不放原始 ID 堆、不放 KPI 磁贴、不放时间范围。
- 状态：健康 + 新鲜度 + 暂停/维护。不是六个数字。
- 时间范围全页一个，但仍然挂在「资源趋势」节头里（见第 5 节「时间范围」）。Grafana、Beszel、Netdata、Checkmk、阿里云 ECS 都是页面或仪表盘级时间，而不是粘在某一张图的标题上；区别是这个控件不该自占一行、左边留白。
- 图领读。最新值可以放在图角，不单独做英雄条。
- 网络接收/发送：一张图，同一单位。Grafana 1860 是一个面板，单位 bps，发送用负 Y。Netdata `system.net` 接收为正、发送为负。夜莺官方 `categraf-detail.json` 把入向和出向上下拆开，那是反例，不照抄。候风用两条都为正的线，外加文字图例。
- 业界把 CPU 堆成 user / system / iowait / steal，候风抄不了。没有这些历史分解。只有总 CPU% 和 iowait 历史。
- 负载应单独成图（Netdata 有 load1 / load5 / load15）。候风历史只有 `load_5`。
- Inode 耗尽和磁盘容量不是同一件事。阿里云把 `fs.inode.utilization` 单独列。
- 管理进菜单。Cockpit 的重启在菜单里；VPS 详情的危险操作在「管理」菜单里。不要放成页底表单。
- 高优先级处置（绑定冲突、接入）不能只藏在菜单里。VPS 概览把需要动手的异常放在身份下面的短列表里，并给主按钮。

否掉的布局来源：Uptime Kuma 是可用性检查，Glances 是实时快照。两者都不是「带时间窗口的主机历史页」，不用它们的版式。

来源：

- https://grafana.com/grafana/dashboards/1860-node-exporter-full/
- https://grafana.com/docs/grafana/latest/dashboards/dashboard-ui/
- https://grafana.com/docs/grafana-cloud/monitor-infrastructure/integrations/integration-reference/integration-linux-node
- https://learn.netdata.cloud/docs/dashboards-and-charts/tabs/metrics
- https://learn.netdata.cloud/docs/dashboards-and-charts/charts
- https://docs.datadoghq.com/infrastructure/list/
- https://docs.checkmk.com/latest/en/graphing.html
- https://deepwiki.com/henrygd/beszel/5.2-system-detail-page
- https://docs.oracle.com/en/operating-systems/oracle-linux/cockpit/view_system.html
- https://n9e.github.io/docs/practice/linux/
- https://github.com/ccfos/nightingale/blob/main/integrations/Linux/dashboards/categraf-detail.json
- https://help.aliyun.com/zh/cms/cloudmonitor-1-0/user-guide/overview-of-basic-and-operating-system-monitoring

## 5. 方案

选定下面这一套。实现按本节。没有「或者」。

同一套控制台，不是新仪表盘。第一屏只回答：是否异常，证据新不新，所选窗口里什么变了。页头是名字加一行身份。健康、心跳、维护/暂停是一条状态带。时间范围在图表节头里，管的是这一整节。八个等大格子：宽屏 4×2，中屏 2×4，窄屏单列。图高 168px。只有快照的字段做安静的说明。管理是页头菜单，三组。

I/O 等待不画在 CPU 图上。两者都是百分数，但 iowait 经常远低于 20。锁到 0–100 会把它压扁。双轴禁止。所以 CPU 是一条 0–100 的线；iowait 是自己的、按数据缩放的等大格子。

详情图表不改 `MonitoringInstanceWatchtowerMetrics` 的布局和 JSX。新建详情专用布局组件，下文称观察图。Compare 继续进口现在的 `MonitoringInstanceWatchtowerMetrics`。允许把该文件里的私有辅助函数抽到共享模块，再让它改为 import；这不是布局改动。

### 样式所有权

`VPSDetailWorkspace.css` 只被 `src/pages/VPSDetailPage.tsx` 导入，构建成 `dist/assets/VPSDetailPage-*.css`，只在 VPS 路由加载。文件头写着 “Scoped to the route; do not import from child views”。`--vps-detail-*` 和 `.vps-detail-workspace .vps-overview-management` 的完整几何都在这份文件里。

监控路由全局只能看到 `legacy-vps.css` 的降级菜单：`.vps-overview-management` 是 `min-width:180px`、`z-index:4`；`.vps-overview-management__menu` 只有 padding；`.vps-overview-management__group-label` 没有全局规则。直接复用 VPS 类名会静默降级。

落地决定：

- 新建 `web/src/pages/monitoring-detail/MonitoringDetailWorkspace.css`。由 `MonitoringDetailPage.tsx`（或详情根组件）import，走 VPS 那种路由级 code-split。
- **不要** import `VPSDetailWorkspace.css`。
- **不要** 把这份文件写进 `src/index.css`。`indexCssContract.test.ts` 只要求 `src/styles/partials/` 下的非空 partial 出现在 `index.css` 的 owner 标记里。
- 必须把该路径登记进 `web/css-owners.json` 的 `observability` 数组。`cssAnalyzerContract` / `make verify-web` 会对未登记样式表报 missing。
- 在这份 CSS 的根选择器上声明 `--monitoring-detail-*`，数值按 VPS 质量条复刻，不要假设 `--vps-detail-*` 在监控路由上存在。
- 节盒、页列、页头、菜单容器、组标签、只读徽章、近期事件圆点都写在这份文件里，用监控自己的类名（例如 `.monitoring-detail-route`、`.monitoring-detail-section`、`.monitoring-detail-management`、`.monitoring-detail-readonly`、`.monitoring-detail-recent__marker`）。不要在监控页挂 `.vps-overview-management`、`.vps-overview-identity__readonly`、`.vps-overview-recent__*`。

### CSS 预算

`make verify-web` 最后跑 `npm --prefix web run css:analyze`（`scripts/analyze-web-css.mjs`）。`web/css-budget.json` 当前是 `sourceFilesMax: 31`，仓库恰好 31 个 CSS 文件。新增 `MonitoringDetailWorkspace.css` 会变成 32，**文件数一项就会失败**，与其它指标无关。

这条门禁在本分支上已经红：`sourceBytes` / `rules` / `declarations` / `repeatedSelectorTexts` / `productionCssBytes` / `productionCssGzipBytes` 都超限。HEAD 的源码 CSS 合计也已经超过 `sourceBytesMax: 345373`。历史上预算是随功能提交一起改的（例如 `da6c97d3`）。

落地时，新增样式表那一步必须同时：

1. re-base `web/css-budget.json`：`sourceFilesMax` 至少改为 32；其余上限按重写并清理后的 `css:analyze` 实际值重设。不要只改文件数、留下另外 6 项红。
2. 生产 CSS chunk 会从 6 变 7（详情 lazy 路由，`router.tsx` 第 34 行）。`productionCssBytes` / `productionCssGzipBytes` 会继续上涨，一并重设。详情 CSS 进详情 chunk，不动 `entryCssGzipBytes`。
3. 顺手删掉重写后**只被监控详情使用、且不再挂载**的选择器，把预算买回来。可删：
   - `.watchtower-factstrip`
   - `.watchtower-latest`，以及挂在它下面的 `.watchtower-latest .watchtower-snapshot-meta{margin-top:0}` 覆盖（`legacy-monitoring.css` 第 72–73 行）
   - `.watchtower-onboarding-hint`
   - `.watchtower-trend-toolbar`
   - `.watchtower-recent-events`
   - `.watchtower-subject-nav`
   - `.watchtower-management`（详情页底折叠包装）
   - `.watchtower-observation`（`legacy-observability.css` 第 323 行；消费者只有 `MonitoringDetailPageBody.tsx`）
   - `.watchtower-status-badge` / `__dimension`（基规则 `page.css` 第 105–107 行，scoped 字号 `legacy-monitoring.css` 第 68 行）。全仓只有 `MonitoringInstanceWatchtowerHeader` 在用，Target 页头不用。新页不再用「维度+值」包裹式徽章，可以删。留着不会错，只白占预算。
   - 可选回收（既存死 CSS，与本次重写无关，删了只赚预算；`sourceBytes` / `rules` / `declarations` 都红，顺手清掉更好）：
     - `.watchtower-container-image`（`legacy-observability.css` 第 243 行）。唯一消费者是未挂载的 `MonitoringInstanceContainersSection`，本文本来就不挂容器。
     - `.watchtower-metric-group--notice` / `--alert` / `--critical`（`legacy-monitoring.css` 第 80–82 行）。全仓无构造点；`MonitoringInstanceWatchtowerMetrics.test.tsx` 第 142 行断言 `--critical` 为 null，删 CSS 后该断言仍成立。
     - `.watchtower-header__meta-item`（`page.css` 第 109 行）。只有 `MonitoringInstanceWatchtowerHeader` 的 `linkedVPSSummary` 在用。Target 用的是 `.watchtower-identity__meta-item`。
4. **必须保留**（仍被其它表面挂载）：
   - `.watchtower-identity*`、`.watchtower-actions-menu*`：`TargetWatchtowerHeader`（含 `__copy` / `__statuses` / `__meta` / `__meta-item`）
   - `.watchtower-header__freshness`：Target 页头第 98 行在用。`.watchtower-header__labels`：Target 页头第 86、92 行在用。不要把整个 `.watchtower-header__*` 当成 Target 都在用；`.watchtower-header__meta-item` 见上面可选回收。
   - `.watchtower-snapshot-meta` 基规则（`legacy-observability.css` 第 239 行）：`TargetSnapshotMeta.tsx` 第 5 行在用。不要把这条基规则当成 LatestSample 专用删掉
   - `.watchtower-danger*`：`TargetDangerCard`
   - `.watchtower-secondary*`、`.watchtower-property-item*`：`TargetMetadataSection`、`TargetLifecycleSection`、`TargetLabelsAndNote`
   - `.watchtower-metrics-panel`、`.watchtower-metrics-meta`、`.watchtower-metrics`、`.watchtower-metric-group*`、`.watchtower-metric-plot*`、`.watchtower-metric-facts*`、`.watchtower-metric-aux`：Compare 的 `MonitoringInstanceWatchtowerMetrics`。这些选择器只出现在该组件里，但该组件同时被对比页挂载，**不是详情专用**。不要按「把每个 `.watchtower-*` 映射到直接 TSX 文件」的扫描结果去删它们。
   - `.watchtower-metrics`、`.watchtower-metrics-meta`、`.watchtower-metric-card*`：`TargetLatencyTrends`（第 161–184 行；定义在 `page.css` 第 120–127 行）
   - `.watchtower-window-tabs`、`.watchtower-stream-status*`：时间窗口控件仍用。`--connected` / `--connecting` / `--reconnecting` / `--disconnected` / `--idle` 由 `MonitoringInstanceTimeWindowTabs.tsx` 第 35 行 `` `watchtower-stream-status--${streamStatus}` `` 拼出来。**不要按 class 字符串 grep 判定为死代码。**
   - `.watchtower-activity-note` / `-quiet` / `-grid`：`TargetActiveIncidents`、`TargetRecentEvents`
   - `legacy-monitoring.css` 第 70、120 行 `.watchtower-identity .watchtower-actions-menu__panel`：Target 页头同样是 identity + actions-menu
5. 不要把还在用的前缀写进 `indexCssContract.test.ts` 的 `retiredClassPrefixes`。那条测试是「不得为已下线表面保留 owner」。`.watchtower-identity`、`.watchtower-snapshot-meta` 等仍在线上。清理后跑一遍 `css:analyze`，`repeatedSelectorTexts` 也是当前红项，按实测重设。

### 信息顺序

固定顺序。纵向间距 14px。列宽跟监控列表一样铺满 `.main`（`width: 100%`），不要再套一层居中的 1600px。VPS 详情仍是 1600px 阅读列。八张 168px 高的仪表格铺成 4×2，不要把图高抬到填满视口。根节点不要再用 `.page` 的 24px gap 当这一页的间距。标题 22px / 600，不要再靠 `.page__title` 的 700。

1. 页头
2. 状态带
3. 通知行。没有就不渲染，不留空盒。顺序固定：绑定冲突、活跃异常、运行动作错误、运行指标错误。关联 VPS 失败只出现在身份行，不在这里重复。策略不可用走状态带心跳附注和图表节头，不在这里再画一条。命令轮询错误留在命令抽屉里。
4. 资源趋势（窗口控件在它的节头里，不再单独占一行）
5. 近期事件
6. 管理菜单（默认关闭）
7. 对话框和抽屉按需出现

暂停确认已经是 `ActionConfirmationModal`（`MonitoringInstanceRuntimePauseConfirmation`）。保持模态，不要改回页内卡片。未绑定不在通知区再写「尚未绑定 agent」；页头主按钮就是接入。

### 页头

h1 只有 `display_name`。22px / 600 / line-height 1.25。不要 24px 的资产图标。VPS 现在有 `.vps-overview-identity__mark`（24×24，SVG 22px），这一页不复制那个标记。

`READ_ONLY_PREVIEW` 为真时，标题行右侧出「只读预览」徽章。类名用监控自己的（例如 `.monitoring-detail-readonly`），`flex: 0 0 auto`，标题行内不收缩。不要引用 `.vps-overview-identity__readonly`：它只在 `VPSDetailWorkspace.css` 第 119 行，监控路由上不存在。隐藏「管理」和「接入 agent…」。刷新仍在。

标题下是身份 `dl`。12px，间距 `4px 14px`。只渲染有值的项，顺序固定：关联、服务商、位置、分组、标签、备注、ID。

- 关联：一台 VPS 时链到它的 `display_name`，不显示 `vps_id`。没有关联时，链接文案是「未关联」，目标 `/vps?view=unlinked`。两台：两个 `display_name` 都列出，用 ` · ` 连接，各自链到对应 VPS。三台及以上：前两个名字，然后「等 N 台」，N 是总数；前两个仍是链接。加载中写「加载中」。失败写「未同步」，并给「重试」。可选「返回来源 VPS」只在 `return_vps` 有值、且它不是那唯一一台已关联 VPS 时出现。
- 服务商：provider 文本。空、`未确认`、或以 `Provider` 开头的字符串不显示。不要写「Provider 未确认」。
- 位置：由 `region` 和 `city` 各自过滤后再用 ` · ` 连接。过滤掉空、`未确认`、以及云区域代号。city 有值就显示，ASCII 可以，例如 `Tokyo`。两个片段都空则整项不渲染。不要写「位置未确认」。因此预览 `mi_001` 的位置是 `Tokyo`，不是 `ap-northeast-1 · Tokyo · Example Cloud`。`Example Cloud` 走服务商，不走位置。
- 云区域代号：trim 之后匹配下面任一规则即省略。大小写不敏感。
  1. `^[a-z]{1,3}-[a-z0-9-]+$`（`ap-northeast-1`、`us-east-1`、`cn-hangzhou`、`us-central1`）
  2. `^[a-z]{3}\d+$`（`sgp1`、`nyc3`、`fsn1`）
  3. 全部 ASCII 字母且长度 ≤ 3（`JP`、`SG`、`nrt`）
- 分组：标签写「分组」，值用 `group`。永远不要写 `Group`。
- 标签：前 3 个，其余「等 N 个」。
- 备注：最多两行，完整文本放在 `title`。独占一行。
- ID 最后。等宽，弱色。不省略，但不是身份的主体。

默认动作两个：刷新（ghost，min-height 28）和管理（primary，min-height 36，`aria-haspopup="menu"`）。去掉「查看历史」和溢出「…」。

未绑定例外：`binding_status === '未绑定'`，且未归档，且不是只读预览时，动作为三个：刷新（ghost 28）、管理（ghost 或 secondary，min-height 28，仍 `aria-haspopup="menu"`）、接入按钮（primary 36）。接入按钮文案沿用现有 `isUpgradeOnboarding`：已有心跳、同步或样本时写「升级/重新接入 agent…」，否则写「接入 agent…」。点击打开现有接入抽屉。管理菜单里仍保留同一项，两个入口走同一抽屉。

视口低于 760px 时，动作换到标题下方，左对齐（`justify-content: flex-start`）。与 VPS 在 760px 时的做法一致。不要在监控 CSS 里引用 `.vps-detail-overview__actions`。

### 状态带

无边框，无投影，min-height 28。左侧：

- 健康词单独出现，14px / 600，不加「健康」前缀。颜色走状态 token。暗色主题（`:root` / `.theme-houfeng-dark` / `.theme-classic-dark`）解析为：`正常` `#8FB39F`（`--color-state-normal`），`关注` `#D4B56A`（`--color-state-notice`），`告警` `#D4A05A`（`--color-state-alert`），`严重` `#D4786A`（`--color-state-critical`）。实现写 token，不要把这些十六进制再抄进组件。浅色主题 `.theme-houfeng-light` 里 `--color-state-alert` 等于 `--warn`，与关注同色；本稿不另造浅色第三色。
- 没有心跳时健康词是「未知」，颜色 `var(--text-muted)`。不要保留一份历史的「正常」。维护或暂停不得改写健康词。现有 `monitoringInstanceEffectiveHealth` 已经是这个规则。
- `monitoring_status` 为 `维护中` 或 `暂停` 时才出徽章。`启用` 不显示。
- 绑定为 `未绑定` 或 `指纹变更待确认` 时才出徽章。`已绑定` 不显示。
- 已归档则出徽章。
- 心跳紧挨健康词，12px。形式是 `心跳` 加相对时间。陈旧时追加 ` · 数据陈旧`。没有心跳：`心跳 未收到心跳`。时间非法：`心跳 时间无效`。策略不可用：仍显示时间，并追加 ` · 新鲜度策略不可用`。
- 保持不渲染 `last_sync`。今天页头本来就不画它；`last_sync_at` 只参与 `isUpgradeOnboarding`。这是约束，不是待修缺陷。它是批次到达时间，不是证据新鲜度。

`lifecycle_status` 不是页头徽章。

### 通知行

共用视觉：左边 2px `var(--err)`，`padding-left: 12px`，无卡片、无投影。没有内容就不渲染。

绑定冲突：`binding_status === '指纹变更待确认'` 时出现。文案「绑定冲突待确认」。右侧主按钮「处置绑定冲突」，min-height 28。加载中按钮禁用，文案旁写「正在加载…」。加载失败写错误加「重试」，沿用 `onRetryBindingConflict`。不要再挂 `MonitoringInstanceBindingConflictSection` 那张 `DetailSection` / `metric-card`。

点击「处置绑定冲突」打开对话框，标题「处置绑定冲突」。对话框正文复用现在冲突区的字段：当前已绑定指纹、待确认指纹、首次出现、最近出现、尝试次数，以及现有说明句。三个动作按钮仍是「确认重绑定」「拒绝新指纹」「重置绑定」，禁用规则与今天相同。点击其中一项后关闭处置框，打开现有 `ActionConfirmationModal`（`MonitoringDetailPageBody.tsx` 里的 `bindingConfirmationCopy`）。不要另造一套确认文案。只读预览不显示这三个动作，对话框只读说明「只读预览不能处置绑定冲突」。

活跃异常：`current_active_incident_count > 0` 或异常列表非空时出现。不要挂 `MonitoringInstanceDangerCard`，也不要挂 `TargetActiveIncidents`。摘要优先 `current_primary_issue_summary`，否则最早一条异常的 summary，否则「存在活跃异常」。然后是「活跃 N」、相对开始时间、链接「事件」。该链接打开现有历史抽屉，tab 为 `incidents`。加载失败是单独的重试行。计数为 0 且没有错误时，这一行不存在。不要对象 ID，不要对象类型徽章。

运行动作错误：`runtimeError` 有值时出现，文案就是该字符串。

运行指标错误：`runtimeFactsError` 有值时出现。若仍有上次成功的 `runtimeFacts`，文案「运行指标刷新失败，仍显示上次已标记窗口。」加错误串，按钮「重试运行指标」。若没有保留数据，文案「运行指标不可用。」加错误串，同一按钮。不要改成横幅。

### 时间范围

控件在「资源趋势」节头右侧，和时间读数同一条工具带。收回初稿「独立一行、右对齐到 1600px 列边缘」那条：那样会在状态带和图表卡之间留出一条左边什么都没有的空行，四个 pill 悬在最右边，构图没有锚点。全页仍然只有这一个窗口控件，它管的就是这个图表节。

无障碍名称「观测时间窗口」。今天 `MonitoringInstanceTimeWindowTabs` 传给 `SegmentedControl` 的 label 是「监控实例观测时间窗口」，改成这一句。选项：实时 / 24h / 7d / 30d。默认 24h，写入 URL。只有「实时」开 WebSocket。连接词（已连接 / 连接中 / 重连中 / 已断开）只在选中实时时出现，12px，位于控件左侧。节头窄到放不下时，控件换行到节头第二行，仍然对齐节内容，不新增一条空工具栏。

实时种子很薄时：不要写「样本不足」，也不要写现在的「实时滚动 N 点」（`MonitoringInstanceWatchtowerMetrics.tsx` 第 214 行）。八个图位都保留。0 个点：不挂载 `MetricChart`，图位居中 12px「等待实时样本」。1 个及以上：把已有点画出来；缺的时间是缺口，不是 0。1 个点时走下面的 `allowSinglePoint`，画出点，不显示「样本不足」。

历史窗口为空：图表节头下一行「该窗口没有样本」。图仍占位，沿用现有「暂无观测数据」。请求失败时保留上一次成功的数据；通知行加「重试运行指标」。不要改成横幅。

### 图表节

一个节盒，不要每张图再套卡片。几何按 VPS 第 328–339、341–351 行的**数值**复刻到 `--monitoring-detail-*` 和 `.monitoring-detail-section`，不要使用 `.vps-detail-workspace__section` 这个类：

- padding 16px（`--monitoring-detail-pad`）
- 内部 gap 12px
- `1px solid var(--border-muted)`（`--border-w` 为 1px）
- 圆角 10px（`--monitoring-detail-radius` = `--radius-3`）
- 背景 `var(--surface)`。暗色下是 `#141613`。浅色下是 `#FFFFFF`
- `box-shadow: none`

h2「资源趋势」，15px / 600 / line-height 1.35。节头是一条工具带：h2 在左，时间读数和窗口控件在右，`space-between`，窄到放不下就整体换行。

时间读数全节只有一处，在 h2 右侧，12px 弱色：

- 24h / 7d / 30d，未悬停：「窗口末值」加最后一个有限桶的时间
- 悬停：「选中」加悬停时间
- 实时，未悬停：「实时点」加点的时间
- 实时，悬停：「选中」加悬停时间

阈值策略不可用时，追加「 · 阈值策略不可用」，并给文字按钮「重试策略」。

网格：`container-type: inline-size`，间距 10px。八个等大格子。宽屏 4×2、中屏 2×4、窄屏 1×8。图高固定 168px，不随视口升高。

宽（容器 ≥ 1100px），4 列 2 行：

| 行 | 列1 | 列2 | 列3 | 列4 |
| --- | --- | --- | --- | --- |
| 1 | CPU | 内存（占用 + 交换） | 磁盘 | Inode |
| 2 | 负载 1/5/15 | I/O 等待 + 磁盘繁忙 | 网络下行 + 上行 | 磁盘读 + 写 |

Inode 单独一格：耗尽和磁盘满不是同一件事。网络下行 / 上行各一格，**共用 yMax**，对齐 hover。I/O 等待不叠到 CPU 上。

每张图是节内凹进的仪表格：`--panel-bg-muted`、1px 边、顶边 2px（越阈值时换成状态色）、圆角 8px。标题 12px / 600 / 次要色。角值 15px 等宽。不要用旧 `.watchtower-metric-card` 的全大写英文标题。

中（40rem–1099px），2 列 4 行，同一 DOM 顺序。窄：容器 < 40rem，**或**视口 ≤ 760px，单列上下滚。页面不出现横向滚动。

悬停时角值换成该桶的值。缺口显示「—」，永不显示 0。维护中：描边用 `var(--color-state-maintenance)`。暗色下该 token 是 `#8A8B82`。页面其余部分不变。

Y 刻度在左侧外，12px 弱色，最多 4 个。X 最多 5 个：实时和 24h 用 `HH:mm`；7d 和 30d 用 `MM/DD`。左沟槽用 `MetricChart` 的显式 `paddingLeft`：百分数图 32px，负载 36px，网络 **64px**。轴标签 `textAnchor=end`，`x = paddingLeft - 4`。用页面真实字体（IBM Plex Mono 12px）量过：`12.0 MB/s` 宽 54px，`120.0 MB/s` / `999.9 KB/s` 宽 60px。56px 沟槽只留 52px，会裁 `12.0 MB/s`。不要用现在的 `yTickCount <= 2 → left 96` 这条内部规则来凑网络沟槽。现有 `formatNetworkAxis`（`MonitoringInstanceWatchtowerMetrics.tsx` 第 101–107 行）在 ≥ 1 MiB/s 时就是 `toFixed(1)` 的 `MB/s`。右沟槽 8px。这一页不使用阈值沟槽模式。

`null`、非有限数、`network_rates_valid === false` 都是缺口。显式 0 画成 0。空桶留在自己的时间位置上。主序列和第二序列都没有有限值时，该图显示「暂无观测数据」，不要画一条贴底的零线。只有一侧有数据时仍画图，缺口那一侧断线，不得把另一侧画成 0。

### MetricChart 前提

详情页需要的能力全部 opt-in。不传这些 props 时，行为与今天相同。Compare 和 `MonitoringEvidenceRenderer` 不传它们。Target 详情用 `Sparkline`，不是 `MetricChart` 消费者。

空态判定：`numericValues` 必须合并主 `samples` 和**通过校验的**第二序列的有限值。长度或时间戳对不上时第二序列被忽略，空态也不得再看它，否则会出现「非空却不画线」。今天只看主序列（`MetricChart.tsx` 第 203、206 行）。合并网络图时若下行全 null、上行有数据，仍要画图。

1. 可选第二序列：`secondarySamples?: MetricChartSample[]`，`secondaryTone?: MetricChartTone`。默认 `accent-2`。必须与主 `samples` **等长**，且 `samples[i].observedAt === secondarySamples[i].observedAt`。长度或时间戳对不上则忽略第二序列，只画主序列。X 仍按下标投影。悬停仍走现有 `hoveredAt`。各序列自己的 `null` / `gapBefore` 自己断线；一条的缺口不得把另一条画成 0。
2. 可选警告线：`alertBandFrom?: number`。在该 Y 值画 1px 虚线，颜色 `var(--color-state-notice)`，**不填充** 80–100 的色块。浅色主题即便 8% 不透明的 `--chart-warn-fill` 也会在图顶画出一条比数据线更抢眼的米色带。三档数字仍在 title / `aria-label`。与 `thresholds` 互斥于这一页的百分数图：CPU / 内存 / 磁盘 / Inode 用这条线，不传 `thresholds`。
3. 可选 `paddingLeft?: number`。默认仍是 32。详情网络图传 64，负载图传 36。
4. 可选 `allowSinglePoint?: boolean`，默认 false。为 true 且 `samples.length === 1` 且该点有限时：画一个点（半径 3px，与现有 hover 圆点相同），不渲染「样本不足」，不画折线。X 放在绘图区右缘，与今天 `isSingle` 的 `projectX` 一致。可以把该点的 `observedAt` 交给 `onHoverAtChange`。`allowSinglePoint` 为 false 时保持今天的「样本不足」且不画线。
5. 0 个点由页面处理，不把空数组交给「样本不足」分支。
6. 阈值线可无标注。今天每个 threshold 都会渲染 `<text>`（第 427–433、562–571 行），没有「只画线不标注」的开关。`threshold.label === ''` 时不渲染 `<text>` 和对应 leader，线仍画。未传 `label` 仍走 `formatValue`。不要留下空 `<text>` 节点。Compare 今天传的是有内容的 label，不受影响。

测试必须锁住默认：不传上述 props 时，单点仍显示「样本不足」，没有第二序列，左沟槽仍 32，没有告警带，有 label 的阈值文字仍在。

### 系列辅助函数

`toSeries`、`seriesValueAt`、`formatNetworkAxis`、`thresholdLines`、`thresholdTitle`、`timeWindowLabel`、`formatCapacityBytes` 现在是 `MonitoringInstanceWatchtowerMetrics.tsx` 里的模块私有函数，没有 export。观察图需要它们。抽到 `web/src/components/monitoring-detail/metricSeries.ts`（或同级共享模块）。`MonitoringInstanceWatchtowerMetrics` 改为从该模块 import，JSX 和布局不变。不要复制一份。

### 整齐刻度

负载和 I/O 等待：`yMin` 0。`includeThresholdsInScale` 为 false。先取有限值最大值 `dataMax`；没有有限值则走空图。`raw = dataMax * 1.15`，再收到 1–1.5–2–2.5–3–4–5–6–8 序列：

```
function niceMax(raw: number): number {
  const target = Math.max(raw, 0)
  if (!(target > 0)) return 0.1
  const exp = Math.floor(Math.log10(target))
  const frac = target / 10 ** exp
  const niceFrac =
    frac <= 1 ? 1
    : frac <= 1.5 ? 1.5
    : frac <= 2 ? 2
    : frac <= 2.5 ? 2.5
    : frac <= 3 ? 3
    : frac <= 4 ? 4
    : frac <= 5 ? 5
    : frac <= 6 ? 6
    : frac <= 8 ? 8
    : 10
  return niceFrac * 10 ** exp
}
```

`yMax = niceMax(raw)`。这个函数放在观察图组件旁边，不要改 `MetricChart` 的自动 padding 规则。

**不要给 `raw` 加 1 的下限**（初稿的 `max(raw, 1)` 已收回）。I/O 等待常年在 0.5% 量级，下限 1 会把轴撑成 0–1%，把一条常数序列画成悬在轴中间的死线；负载 0.65 也会被撑到 1。轴跟着数据走：`dataMax` 0.5 → `raw` 0.575 → `yMax` 0.6；`dataMax` 0.65 → `yMax` 0.8；`dataMax` 1.2 → `yMax` 1.5；`dataMax` 5.5 → `yMax` 8。阈值 4 / 6 / 8 只有 ≤ `yMax` 才画，小数据下一条都不画。全零序列退回 0.1 的退化轴，不做除零。

### 告警带

用于 CPU、内存、磁盘、Inode。Y 锁在 0–100。只画一件东西：warning 值上一条 1px 虚线，`stroke: var(--color-state-notice)`，`strokeDasharray: 3 3`，opacity 0.7。

**不要填充 80–100。** 浅色主题里 `--warn-bg` 是实心 `#F4EEDC`；即便改成 `--chart-warn-fill` 的 8% 棕，20% 高的色块仍然像图顶的脏色头。健康机器的线都在 warning 以下，最抢眼的会变成那块空白涂色。三档数字继续放 title / `aria-label`。不要在 80 / 90 / 95 画三条线。

三档全部写进 h3 的 title 和 `aria-label`。数字用已解析阈值，不是写死在组件里。`thresholds.ts` 的默认值是：

- CPU：warning 80，alert 90，critical 95。示例：「正常 < 80%，关注 ≥ 80%，告警 ≥ 90%，严重 ≥ 95%」
- 内存：85 / 92 / 95
- 磁盘：85 / 92 / 97
- Inode：80 / 90 / 95

负载和 iowait 的设置覆盖走 `orderedTwoLevelThreshold`：只收 warning 和 critical，alert 取中点。标题数字跟随解析结果。默认对象里负载是 4 / 6 / 8，iowait 是 20 / 35 / 50。

角上数字的颜色：≥ critical 用严重色，≥ alert 用告警色，≥ warning 用关注色，否则 `text-primary`。暗色分别是 `#D4786A`、`#D4A05A`、`#D4B56A`。数字始终在。策略缺失时不画告警带。浅色主题里告警色和关注色相同，角值在 warning 与 alert 之间不会换第三色。

### 各图

- CPU：一条线，`cpu_usage_pct`，强调色 `var(--chart-stroke)`。暗色 `#8FB39F`。Y 为 0–100。warning 80 处一条虚线。不要 iowait、steal、负载。title 说明使用率可能包含也可能不包含 iowait，所以两者分开。
- 内存：`mem_used_pct`，0–100。warning 85 处一条虚线。不要 swap，不要字节字段。
- 网络：一张图，两条都为正的实线，单位 B/s。不用负 Y，不用虚线。下行 `net_in_bytes_per_sec`，描边 `var(--chart-stroke)`，暗色 `#8FB39F`。上行 `net_out_bytes_per_sec`，描边 `var(--chart-stroke-2)`，暗色 `#D4B56A`。标题行用文字图例：8×2 色条 + 「下行」，再加「上行」，12px。角值 `↓ {down} ↑ {up}`，经 `formatBytesPerSecond`（`web/src/lib/format.ts`）。`yMin` 0。`yMax` 为两条序列最大值 × 1.10，至少为 1；这里不再做 1–2–5 整齐化，以免把速率轴拉得过空。轴用现有 `formatNetworkAxis`。无阈值。一条序列的缺口不得把另一条画成 0。
- 磁盘：一张图，两条 0–100 的线。主序列 `disk_used_pct`（使用率），第二序列 `inode_used_pct`（Inode）。warning 线用磁盘的 85。Inode 阈值写在 title 里。图例「使用率 / Inode」。角值 `{disk}% · Inode {inode}%`。繁忙 / 读 / 写 / 容量仍做图下说明，不入趋势。
- 5 分钟负载：始终可见，与其它五格同高。字段 `load_5`。缩放规则见「整齐刻度」。不画核数线。阈值线 4 / 6 / 8（或解析后的值）只有 ≤ `yMax` 才画，1px。落在轴内的线都画；只有最上面一条传 `label: '告警 {值}'`，其余传 `label: ''`，走 MetricChart 第 6 项。数据约 0.2 时一条都不画。title：「关注 ≥ 4，告警 ≥ 6，严重 ≥ 8。无核数，不能按核归一。」数字随解析阈值变，这句是默认策略下的文案。
- I/O 等待：始终可见，与其它五格同高。字段 `cpu_iowait_pct`。缩放规则与负载相同。**调用处不得再写 `Math.max(dataMax * 1.15, 1)`**。轴刻度在 `yMax < 5` 时用一位小数。不要锁到 100，不要叠到 CPU 上。title：「CPU 等待 I/O 的时间占比，不是磁盘繁忙，也不是磁盘使用率。」

### 快照说明

没有历史的字段不做线，也不做六列英雄条。相关图下方用定义列表。`dt` 12px 弱色，`dd` 13px 等宽、次要色。项之间用「 · 」，允许换行。没有样本时为「—」。

- CPU 下：被窃取 {`cpu_steal_pct`}
- 内存下：交换 · 可用 · 容量。字节用 `formatBytes`。容量未知或 ≤ 0 为 —
- 磁盘下：繁忙 · 读 · 写 · 容量。速率用 `formatBytesPerSecond`。非法速率是 —，合法的 0 仍是 0
- 负载下：当前 1 分钟 {`load_1`} · 15 分钟 {`load_15`}。`title` 为「无历史，不是趋势」
- 网络、I/O 等待、Inode：没有说明行

节脚一行，12px 弱色。这是「最近样本」留下的唯一痕迹：

「当前样本 · 采样于 {相对时间} · 已运行 {uptime} · agent {agent_version}」

运行时间用 `formatSampledUptime`（`web/src/pages/monitoring/monitoringHelpers.ts`）。浏览器不得在本地累加。没有样本时，这一行变成「当前样本 尚无」，说明项为「—」。不要在这里显示快照读取时间。那个钟只留在指标刷新失败的那一行。采样时间修饰这些说明，不修饰页头的健康词。agent 版本从页头身份行挪到这里。

### 近期事件

不要用 `EventList`。它会渲染对象 ID。这一页最多 3 行，数值复刻 VPS 近期活动，写到监控自己的类：`.monitoring-detail-recent__marker` 为 9×9 强调色圆点；`.monitoring-detail-recent__item:not(:last-child)::after` 宽 1px 竖线。不要引用 `.vps-overview-recent__marker` / `__item`：它们只在 `VPSDetailWorkspace.css` 第 934 / 944 行。类型标签 14px / 600。摘要 13px。时间 13px。

核对：VPS 近期标题是 14px / 600。资源行里的弱 ID 是 12px，不是 13px。本稿把摘要和时间定成 13px（`--monitoring-detail-helper-size-strong`）。这是这一页的决定，不是把 12px 的 ID 样式原样搬来。

不要对象类型徽章，不要 `object_id`，不要 `monitoring_instance_id`。

有事件才出现节盒。h2「近期事件」。右侧链接固定四项，间距 14px：历史 / 活动 / 记录 / 证据。活动、记录、证据保留现有 `return_vps` 路径。「历史」**始终存在**，打开现有历史抽屉的「事件时间线」。不要只在超过 3 条时才出现入口。抽屉里的「历史异常」页保留，可从抽屉 tab 切过去；活跃异常通知行的「事件」打开同一抽屉并选中「历史异常」。不加新的页面入口。去掉页头「查看历史」。

没有事件、正在加载、或出错时：不要节盒。一行 14px，右侧仍是那四个链接。「暂无新的状态变更」。加载：「正在加载相关事件…」。错误：消息加「重试」。不要大卡片，不要 h3，不要零计数。

### 管理菜单

基座是 `VPSManagementMenu`，不是 `.vps-detail-actions-menu`。这两套菜单不是同一件事：

- `VPSManagementMenu.tsx` 第 149 行：项是 `className="btn lg ghost vps-overview-management__item"`。全局 `.btn.lg`（`layout.css` 第 55 行）是 `min-height: var(--control-min-height-touch)` = 44px，`font-size: var(--type-body-size)` = 15px，`padding: 0 18px`。`legacy-vps.css` 第 370 行的 `__item` 是 36px，特异性更低，会被 `.btn.lg` 盖掉。
- `.vps-detail-actions-menu` 用的是 `.watchtower-actions-menu__panel button`，约 13px + `padding 8px 12px`，没有 min-height。不要拿它当这一页的菜单基座。

容器和组标签的**数值**从 VPS 路由样式（`VPSDetailWorkspace.css` 第 1035–1064 行）复刻到 `.monitoring-detail-management`，不要复用 `.vps-overview-management` 类名：

- `position: absolute; right: 0; z-index: 8`
- `min-width: 12rem`
- padding `var(--space-2)` = 8px
- `1px solid var(--border)`
- 圆角 `var(--radius-3)` = 10px
- 背景 `var(--surface-elevated)`。暗色 `#1A1B18`，不是节背景 `#141613`
- 投影 `var(--shadow-overlay)`
- 组标签 12px / 600，`var(--text-muted)`
- 组与组之间：`margin-top` 8px，`padding-top` 8px，`1px solid var(--border-muted)`

项：`className="btn lg ghost"` 加上监控自己的 item 类。沿用 `.btn.lg` 即 15px / min-height 44px。不要再写「相对现行 28px 加严到 44px」；28px 只对另一种菜单成立。

键盘与 `VPSManagementMenu` 相同。该文件处理 Tab、Escape、ArrowDown、ArrowUp、Home、End。不要另造一套键。

三组，只用中文名：

1. 运行控制。`archived_at` 已设置，或 `lifecycle_status` 为 `已退役` 时，整组隐藏。项来自现有 `monitoringInstanceRuntimeActions`，每一对只显示合法的那一个：进入维护 / 退出维护，暂停监控 / 恢复监控，接入 agent… / 升级 / 重新接入 agent…，执行诊断命令…，查看命令审计（链接 `/command-audit?monitoring_instance=`）。暂停保持现有确认。维护仍直接提交。命令和接入保持现有抽屉。未绑定页头已经有接入主按钮时，菜单里仍保留同一项。
2. 资料。一项：「编辑分组、标签与备注」。打开对话框，不是页底折叠。对话框标题「分组、标签与备注」。字段：分组、标签、备注。按钮：保存 / 取消。保留现有 PATCH 和 If-Match。只改可见文案，去掉 `Group`。已归档时该项禁用，菜单内 12px 文案「已归档实例资料只读」。不要页级横幅。
3. 生命周期。第一行是静态文字「当前：{lifecycle_status}」，12px 弱色，不是菜单项。原因：把 `待接入` / `在用` / `观察中` / `不续费` / `已退役` 放进页头，会在 VPS 旁边造出第二套资产生命周期。菜单打开时加载 management-review。返回前这一组只显示「正在加载…」，不要禁用按钮。然后只显示 `can_*` 允许的动作：退役、恢复生命周期、归档、恢复归档、永久清理。永久清理文字色 `var(--err)`。点击后关闭菜单，打开现有确认。审查计数、阻断、警告、关联 VPS 名字放在确认框里、原因字段之上，不回到页面上。保持先预览再确认：冻结展示名、动作、`updated_at`；版本变了就拒绝提交。归档和永久删除仍要求输入展示名。预览 404 时这一组显示错误加「重试」，不要把「合成预览未提供此接口」写成空状态文案。

### 视觉 token

没有新调色板。颜色和间距 token 用已经全局存在的名字（`--surface`、`--accent`、`--space-*`）。版式尺寸按 VPS 质量条的**数值**复刻到 `MonitoringDetailWorkspace.css` 的 `--monitoring-detail-*`，不要在监控路由上引用 `--vps-detail-*`。下面的十六进制是暗色主题解析值，供对照，不要再硬编码一遍。

| 用途 | 值 | 出处 |
| --- | --- | --- |
| 列 | `width: 100%`，gap 14px，与列表共用 `.main` 内边距 | 监控详情不是 VPS 那种 1600px 阅读列；图高随格子宽度走 |
| 标题 | 22px / 600 / 1.25 | 复刻 `--vps-detail-title-size` |
| h2 | 15px / 600 / 1.35 | 复刻 `--vps-detail-module-size` |
| 正文 | 14px / 1.45 | 复刻 `--vps-detail-body-size`，`--vps-detail-leading` |
| h3、强辅助 | 13px | 复刻 `--vps-detail-helper-size-strong` |
| 身份、轴、说明 | 12px | 复刻 `--vps-detail-helper-size` |
| 角值 | 13px 等宽 / 600 | 本稿；不是 `--vps-detail-amount-size`（那是 20px） |
| 节 | padding 16，1px `var(--border-muted)`，半径 10，`var(--surface)`，无投影 | VPS 第 328–339 行的数值 |
| 节头 | space-between，gap 8px 14px | VPS 第 354–362 行的数值 |
| 页头主按钮 | 36 | 全局 `--control-min-height` |
| 页头次按钮 | 28 | 复刻 `--vps-detail-action-min` 为 `--monitoring-detail-action-min` |
| 菜单项 | 15px / min-height 44 | 全局 `.btn.lg` / `--control-min-height-touch` |
| 焦点 | 2px `var(--accent)`，offset 2 | `--border-w-strong` 为 2px；跳转面板 `:focus-visible` 第 284–286 行 |
| 强调 / 第二色 | `#8FB39F` / `#D4B56A` | 暗色 `--accent`、`--chart-stroke`、`--chart-stroke-2` |
| 图表告警叠加 | `rgba(212,181,106,0.10)` / 浅色 `rgba(138,90,18,0.08)` | 新增 `--chart-warn-fill`，两个主题都是低不透明度；不要用警告卡片底 `--warn-bg` |
| 文字 | `#E8E6DF` / 弱 `#A3A394` / 次 `#8A8B82` | `--text-primary` / `--text-muted` / `--text-secondary` |
| 字体 | IBM Plex Sans + PingFang SC；数字 IBM Plex Mono | `--font-sans`，`--font-mono` |

图表网格相对节内边距对齐，也就是相对「资源趋势」盒子内缩 16px。时间范围在节头右侧，随这一节铺满主栏。

不要入场动画。悬停指示立即出现。`prefers-reduced-motion` 下仍然不要动画。

禁止出现的文案：`Group`，`Provider 未确认`，`位置未确认`，`样本不足`，把「最近样本」当作节标题，`合成预览未提供此接口`，`心跳 心跳时间无效`（改用 `心跳 时间无效`）。

### 与现有组件的差距

这是对照，不是现在要做的任务清单。

| 现状 | 本稿 |
| --- | --- |
| `MonitoringInstanceWatchtowerHeader` | 拆掉标题下的徽章堆和采样行。身份行 + 默认两个动作。未绑定时第三个主按钮是接入。状态带抽出去 |
| `MonitoringInstanceLatestSample` | 删除组件、测试，以及 `components/monitoring-detail/index.ts` 的 barrel export |
| `MonitoringInstanceWatchtowerMetrics` | 布局和 JSX 不改。对比页继续用。详情不走它。私有辅助函数抽到共享模块后改为 import |
| 新的观察图组件 | 一张网络图。三个辅助图始终可见。时间范围不在这个组件里 |
| 新的 `MonitoringDetailWorkspace.css` | 路由级 code-split。登记 `css-owners.json` → `observability`。不 import VPS CSS，不进 `index.css`。同一提交 re-base `css-budget.json` |
| `MonitoringInstanceTimeWindowTabs` | 变成页面级一行。label 改为「观测时间窗口」 |
| `MonitoringInstanceMetadataSection` | 从菜单打开对话框 |
| `MonitoringInstanceManagementSection` | 菜单 + 确认。删掉页底折叠 |
| `MonitoringInstanceDangerCard` | 删除组件。Target 的 `TargetDangerCard` 不受影响 |
| `TargetActiveIncidents` | 详情不再挂载。Target 详情不受影响 |
| `MonitoringInstanceBindingConflictSection` | 详情不再挂载整节。字段进处置对话框 |
| `MetricChart` | 默认不变。详情 opt-in 第二序列、告警带、`paddingLeft` 64、`allowSinglePoint`、空 label 不画字、双序列一起判定空态 |
| `EventList` | 详情不再使用。其它页面不变 |
| `MonitoringDetailPageBody` | 重排。不挂载容器。本地最多 3 行事件。历史链接始终在 |
| 页列 | 铺满 `.main` / 14px / 标题 22px 600。不要用 `.page` 的 24px gap 和 700 字重，也不要居中 1600px |

### 明确不做

- 不做探针区
- 不为仅有快照的字段画假趋势
- 不做双轴，不把负载画到 CPU 上，不把磁盘 B/s 或 inode 画到磁盘百分比上
- 不画核数线
- 不把 iowait 堆到 CPU 上
- 不设计预览错误文案、合成缺口、过期摘要横幅
- 不挂载容器
- 不改列表页
- 不改对比页布局，不把新观察图布局塞进 `MonitoringInstanceWatchtowerMetrics`
- 不改 `MonitoringEvidenceRenderer` 的 `MetricChart` 默认行为。Target 详情用 `Sparkline`，本来就不是这次的回归面
- 不为上行使用负 Y 或虚线
- 不 import `VPSDetailWorkspace.css`，不把 `--vps-detail-*` 当监控路由上的活 token
- 不删除 Target / Compare 仍在用的 `.watchtower-identity*`、`.watchtower-danger*`、`.watchtower-secondary*`、`.watchtower-property-item*`、`.watchtower-metrics*`、`.watchtower-snapshot-meta` 基规则

### 实现顺序

1. `MetricChart` 的 opt-in props 和默认行为测试：第二序列、双序列空态、告警带、`paddingLeft`、`allowSinglePoint`、空 label 不画阈值文字。不传新 props 时，现有 MetricChart 测试、Compare、`MonitoringEvidenceRenderer` 断言仍通过。
2. 把 `toSeries` 一类辅助函数抽到共享模块。`MonitoringInstanceWatchtowerMetrics` 只改 import。跑现有 `MonitoringInstanceWatchtowerMetrics.test.tsx` 和 `MonitoringComparePage.test.tsx`，确认布局断言不变。
3. 新增 `MonitoringDetailWorkspace.css`，登记 `web/css-owners.json` 的 `observability`。声明 `--monitoring-detail-*`。不要写进 `src/index.css`。**同一提交**里 re-base `web/css-budget.json`：`sourceFilesMax` 31→32，其余上限按清理后的 `npm --prefix web run css:analyze` 实际值重设。只删第 5 节「CSS 预算」里列出的详情专用选择器；Target / Compare 仍在用的 `.watchtower-identity*`、`.watchtower-danger*`、`.watchtower-metrics*` 等一律不动。跑 `make verify-web` 确认 `css:analyze` 绿。
4. 详情骨架：铺满 `.main` 的列、页头、状态带、页面级时间范围、空的图表节。未绑定页头主按钮在这一步就接上现有抽屉。
5. 新观察图组件：等长第二序列、合并的网络、按数据缩放且始终可见的负载 / iowait / inode、告警带、整齐刻度。不要改 `MonitoringInstanceWatchtowerMetrics` 的 JSX。
6. 去掉英雄样本；补上说明和脚注。删除 `MonitoringInstanceLatestSample.tsx` 及其测试，并同时从 `web/src/components/monitoring-detail/index.ts` 去掉 `export * from './MonitoringInstanceLatestSample'`，否则 barrel import 悬空。不要留成未挂载组件。
7. 管理菜单（`.btn.lg` + 监控自己的菜单容器）和元数据对话框；删掉页底折叠。
8. 通知行、绑定处置对话框、三行事件、始终可见的「历史」。删除 `MonitoringInstanceDangerCard.tsx`。绑定冲突整节卡片不再挂载；字段进处置对话框。
9. 重写锁定旧布局的测试，至少包括 `MonitoringDetailPage.test.tsx`（约 40 个用例，含 8 张指标卡「4×2 grid」、折叠资料区、危险区、绑定冲突卡、hover 读数、空态、运行控制）、`MonitoringInstanceWatchtowerHeader.test.tsx`。Compare 测试应继续锁定旧双网图。不要把 Target 详情列入这次回归清单。
10. 浏览器走一遍：24h 对实时、一张网络图、单点实时、下行全空上行使有值、未关联 / 两台 / 多台 VPS、未绑定接入按钮、指纹冲突处置、活跃异常一行、零事件仍能打开历史、窄屏单列、菜单项 44px。打开 `/monitoring/compare` 确认仍是旧的双网图和折叠辅助图。打开一条 Target 详情，确认页头 identity / 危险卡 / 标签折叠仍在。不要动列表页。
11. 落地后更新 `docs/design/current/component-patterns.md` 的 “Monitoring detail diagnostics”，使书面规范与这一页一致。

## 6. 已锁定的决定

下面每一条都已经选定。实现不要再打开。

1. I/O 等待单独成小图，不与 CPU 共用 0–100 轴。理由：避免贴底；禁止双轴；不知道使用率是否已经包含 iowait，所以也不堆叠。
2. 网络两条都为正值，不用 Grafana / Netdata 的负向 Y，也不用虚线。理由：同一单位、同一方向更好读；负 Y 是那些产品的约定。文字图例和 `↓ / ↑` 承担区分，不只靠颜色。
3. 阈值不用三条沟槽线，改成一条 warning 线加一块从 warning 到 100 的**淡**填充（`--chart-warn-fill`）。理由：现有沟槽在 80 / 90 / 95 上会挤（已量到 3px 间隙）；三档改放到 title 和 `aria-label`。不要用 `--warn-bg`，浅色下它是实心 `#F4EEDC`，会盖过数据线。
4. 负载和 I/O 等待按数据缩放，阈值线只有落在轴内才画。理由：真实值远小于 4 或 20 时，把轴撑到阈值会把线压平。整齐刻度用第 5 节的 1–1.5–2–2.5–3–4–5–6–8 函数，并且**不给 `raw` 加 1 的下限**：0.5% 量级的 I/O 等待会被下限撑成 0–1% 的死线。
5. 位置省略云区域代号，保留 city。ASCII 城市可以显示。理由：`ap-northeast-1`、`sgp1` 不是操作者要读的位置；`Tokyo` 是。VPS 身份行也会显示城市。不写「位置未确认」。不要用「含中文才显示」，那会把 `Tokyo` 整段丢掉。
6. 保持不渲染 `last_sync`。这是约束，不是待修缺陷。今天页头本来就不画它。理由：它是批次到达，不是证据新鲜度。新鲜度看心跳。
7. `lifecycle_status` 只出现在管理菜单里。理由：放进页头会变成 VPS 旁边的第二套资产生命周期。
8. 不挂载容器，不加探针区。理由：容器区块未挂载，且没有按实例的探针 API；IP 质量在 VPS。不要为了填满页面发明这些区。
9. 列表页本次不改。理由：详情的结构问题是独立的。
10. 对比页本次不改。`MonitoringInstanceWatchtowerMetrics` 的布局和 JSX 保持现行为。允许把私有辅助函数抽到共享模块。详情新建观察图组件。理由：Compare 明确依赖现在的组件；把七格新布局塞回去会改变对比页。
11. 未绑定且未归档时，页头主按钮是接入 agent。理由：`mi_005` 今天第一屏就能接入；只放进「管理」会比现在难发现。
12. 绑定冲突是通知行 + 处置对话框，对话框保留指纹对照和三个现有动作。理由：缩成一句不可点的状态会丢掉重绑 / 拒绝 / 重置。
13. 「历史」始终出现在事件行，打开现有抽屉。理由：0–3 条事件时若没有入口，「查看历史」删掉后抽屉不可达。
14. `MetricChart` 新能力全部 opt-in，第二序列必须等长并共享 `observedAt`。空态看主序列和**通过校验的**第二序列的有限值并集。`label === ''` 不画阈值文字。理由：X 按下标投影；默认行为被监控详情、Compare、`MonitoringEvidenceRenderer` 共用。Target 详情用 `Sparkline`。
15. 详情用自己的路由样式表和 `--monitoring-detail-*`。不要 import `VPSDetailWorkspace.css`，不要在监控页复用 `.vps-overview-management`。登记 `css-owners.json` 的 `observability`。理由：VPS CSS 是路由 code-split，监控路由上只有降级 fallback。
16. 管理菜单项用 `.btn.lg`（15px / 44px）。不要按 `.vps-detail-actions-menu` 的 28px 去加严。理由：那是另一套菜单。
17. 网络左沟槽 64px。理由：轴标签 `x = paddingLeft - 4`；`12.0 MB/s` 宽 54px，`120.0 MB/s` 宽 60px；56px 会裁切。
18. 新增样式表的同一提交必须 re-base `web/css-budget.json`，至少 `sourceFilesMax` 31→32，其余上限按清理后实测重设。理由：`css:analyze` 在 32 个文件时会因文件数一项失败；本分支这条门禁已经有多项超限。
19. 只删监控详情专用 CSS。不得删除 Target 仍在用的 `.watchtower-identity*` / `.watchtower-danger*` / `.watchtower-secondary*` / `.watchtower-property-item*` / `.watchtower-snapshot-meta` 基规则 / `.watchtower-header__freshness` / `.watchtower-header__labels`，也不得删除 Compare 与 `TargetLatencyTrends` 仍在用的 `.watchtower-metrics*`（含 `WatchtowerMetrics` 里那些看似「只在该文件出现」的 group/plot/aux）。不得按 grep 删 `.watchtower-stream-status--*`（模板字符串拼接）。不得把这些前缀写进 `retiredClassPrefixes`。`.watchtower-latest` 下的 snapshot-meta 覆盖可以随 `.watchtower-latest` 一起删。`.watchtower-observation` 可删。可选删：`.watchtower-status-badge`、`.watchtower-container-image`、`.watchtower-metric-group--notice/--alert/--critical`、`.watchtower-header__meta-item`。
20. 删除 `MonitoringInstanceLatestSample` 和 `MonitoringInstanceDangerCard`，不要留成未挂载组件。删 LatestSample 时同步摘掉 `web/src/components/monitoring-detail/index.ts` 的 barrel export。理由：详情不再挂它们；Target 有自己的 DangerCard 和 `TargetSnapshotMeta`。
21. 窗口控件进「资源趋势」节头，和时间读数同一条工具带；收回初稿「页面级独立一行、右对齐到 1600px 列边缘」。理由：控件仍然管全页的图表节，但独立空行没有左锚点，状态带和图表卡之间会多出一条只有四个 pill 的空白行。
22. 八个等大格子：宽屏 4×2，中屏 2×4，窄屏单列。图高固定 168px。CPU；内存+交换；磁盘；Inode；负载 1/5/15；I/O 等待+磁盘繁忙；网络下行+上行；磁盘读+写。双线共用 yMax 和对齐 hover。`HostMetricPoint` 必须聚合 load_1、load_15、swap、disk_busy、disk 读写，禁止用最新快照冒充趋势。理由：超宽屏上 3×2 再加高会变成海报；4×2 加矮卡才是仪表盘密度。
23. 百分数图的 warning 只画 1px 虚线，不填充 80–100。理由：浅色实心 `--warn-bg` 和 8% 的 `--chart-warn-fill` 都会在图顶画出比数据线更抢眼的色块。
24. I/O 等待 / 负载的 `niceMax` 调用处不得再 `max(raw, 1)`。理由：0.5% 的 I/O 等待会被撑成 0–1% 中间一条死线。
25. 详情列铺满 `.main`，左缘与监控列表对齐，不再 `min(100%, 1600px)` 居中。理由：列表已经铺满主栏；居中 1600px 在超宽屏左右各空一块，标题也对不齐。VPS 详情仍保持 1600px。八张矮卡铺满主栏，图高不再随视口升高。
26. 每张图是节内凹进的仪表格（`--panel-bg-muted`、1px 边、顶边 2px 带状态色），不是旧 `.watchtower-metric-card`，也不是无框大海报。理由：8 张需要马赛克节奏；旧卡全大写英文标题；无框 3×2 在超宽屏上读成一整块壁画。
