# 入口探测 Web 合同

具体页面、状态、请求与回归要求在本文件维护；通用组件与数据层约定见 [Web 规范](../web/README.md)。

资产详情页（VPS `/vps/:id`、入口 `/targets/:id`）统一采用「判断在顶、证据居中、配置进弹层」的三段式信息架构。新详情页应对齐：

- **顶部放决策板**：页面第一屏是 DecisionBoard——一张「下一步动作」卡（按运行/健康状态优先级选出单条 CTA）+ 一条 tone 着色的证据条。决策模型（`build<X>DecisionModel`）是纯函数，只消费已有 contract 字段算出 `nextAction` 与 `evidenceItems`，不发请求、不发明字段。

- **tone 系统统一四档**：`'normal' | 'notice' | 'alert' | 'critical'`，映射到 `StatusGlyph` 的 state；CSS 类前缀按页面命名但结构保持一致，着色走 `var(--color-state-*)` / `color-mix`，不写 hex。新增详情页复制这套 tone→glyph→CSS 约定，不要另造一套色彩语义。
- **页头操作分层**：主 CTA 之外的页头操作收在右上角：「查看历史」「资料维护」是并列按钮且常驻（只读预览只保留「查看历史」），保证退役等状态下仍有维护入口；运行控制（暂停、恢复等）收进 `details.watchtower-actions-menu` 的 `…` 菜单，按状态条件渲染，列表为空或只读预览时不渲染菜单。不要把这些操作散落成页面底部的独立按钮。参考 `web/src/components/target-detail/TargetWatchtowerHeader.tsx`。

- **非实时配置 / 维护 demote 进 modal**：标签备注编辑、退役生命周期这类低频配置从页面主体移入 modal（`web/src/components/atoms/Modal.tsx`），页面主体只保留实时观测证据（决策板、运行控制、ProbeItem 列表与观测、当前异常、事件）。**例外**：本身会再开一个表单 modal 的入口（如 ProbeItem 表单）不要嵌进维护 modal，避免 modal 套 modal——让它贴着对应的实时列表区就近呈现。


用户文案使用“入口探测”，字段标识继续使用 `target_id`。入口详情保持紧凑身份和当前观测；管理与历史为次级，incident/event 请求失败独立可重试，缺失或禁用 probe 不等同于失败观测。空 incident 列表不能证明所有源健康。Probe 对话框操作 footer 保持在滚动正文外。

Target 的 `lifecycle_status` 使用 `active | retired`；`run_status` 仅使用 `启用 | 维护中 | 暂停`。退役状态不能由暂停推断，也不能作为运行控制输入。默认 API 查询只返回当前目标；列表分别批量读取 `scope=current` 与 `scope=retired` 以支持当前及历史页签，不把 `scope=all` 中仅关联归档 VPS 的 active 目标计入当前集合。退役页签使用 `lifecycle_status=retired`。退役目标从异常、暂停、覆盖缺口计数和运行批量操作中排除，详情隐藏运行控制和 Probe 修改；历史仍可查看。

Target 退役操作使用 `/runtime/archive`，将生命周期设为 `retired` 并暂停；显式 `/runtime/restore-to-paused` 恢复到 `active` 与暂停，后续启用另行确认。共享依赖审查、摘要失效重读和明确确认保持生效。回归由 `TargetsPage.test.tsx`、`TargetDetailPage.test.tsx` 和 `targetHelpers.test.ts` 覆盖。

## 观测与执行覆盖

- 列表异常及 `abnormal=1` 只包含当前可见、启用、有成功或失败观测时间且健康为关注/告警/严重的目标。`view=unobserved` 承接当前可见、启用且从未产生成功/失败观测的目标；两类计数不相加。健康筛选包含“数据不可用”，未知徽章使用中性色，运行控制独立显示。
- Target 的 `last_success_at` / `last_failure_at` 只从 `result_kind=success/failure` 且 `maintenance_context=false`、`is_backfilled=false` 的实时探测观测按 `observed_at` 投影；原始观测仍保留，乱序到达不能使任一时间倒退。
- TargetRecord 的只读 `enabled_probe_count` 表示配置为 enabled 的探测项数，暂停目标也保留配置事实。`matching_executor_count` 表示依照 Agent plan 标签交集和 archived/retired/paused 排除规则可接收任务的实例数；仅启用/维护目标可匹配，其余为零。列表与详情由后端一次聚合读取，不逐行请求。匹配不保证在线，不等于已有样本。
- 执行标签允许未来值；无启用探测项、无匹配实例、匹配但尚无样本分别解释。已有历史观测不保证当前健康。观测是否过期只消费服务器 `observation_freshness`，前端不另算阈值，也不把缺失字段当成 0。

## 观测新鲜度

`TargetRecord.observation_freshness` 与启用探测项数组由同一次目标读取返回。列表、详情和摘要使用同一份服务器结果。

- `inactive` 不另做新鲜度提示，只保留暂停、维护或退役等原控制态。`fresh` 为「观测新鲜」，`pending` 为「等待新观测」，`stale` 为「观测已过期」，`unobserved` 为「尚无观测」，`uncovered` 为「未配置启用探测项」。`partial` 在 `stale_probe_count > 0` 时为「部分观测过期」，否则为「部分探测项等待观测」。
- `view=stale`（页签「观测过期」）只包含当前可见、启用、已经有成功或失败观测且 `stale_probe_count > 0` 的目标。pending、没有 stale 的 partial、从未观测、暂停、维护和退役都不计入。该计数与异常、尚无观测分别展示；同一目标可以同时属于异常和观测过期，两个计数不相加。
- 切换互斥页签时清除 `view`、`abnormal`、`coverage_gap`，以及页签拥有的 `run_status=暂停` 和 `lifecycle_status`。分组、类型、健康、标签和执行标签保留。`view=stale` 与 `abnormal=1`、`view=unobserved` 互斥，但不清除 `group`。不新增后端查询参数。直接打开 `?view=stale`、空结果、后退和当前/退役切换与尚无观测相同。分组筛选按名称精确匹配；服务端空白分组名 `未分组` 同时匹配 `group` 为空或仅空白的目标。
- 健康表示最近已知健康，不因过期改写 incident 或宣称恢复。历史健康为正常且当前不是 fresh 时，不显示无保留的「当前正常」，改为「最近一次正常，当前证据不足」。已有异常摘要保留，并同时显示过期提示。
- 列表初次同时读取 `scope=current` 与 `scope=retired`，两者都成功才展示。之后页面可见时每 30 秒只重读 current；隐藏时不因计时或焦点发请求；回到可见或获得焦点时立即重读 current，同时发生的唤醒合并为一次。同一刷新通道只有一个在途请求。批量运行控制开始前使在途读失效，控制完成后再把 current 与 retired 作为同一次发布；控制在途期间不应用周期结果，失效前发出的读不能覆盖控制后的列表。两次都成功才替换列表并清除失败提示。任一次失败都保留上次完整合并结果，显示「更新失败，显示上次结果」及上次成功时间，不用尚未成功的 retired 缓存配上新的 current。此后的周期刷新仍只重读 current，不能因此清除该失败或公布不完整集合。失败提示中的「重试」同时重读 current 与 retired，两者都成功后才恢复 retired 缓存。
- current 重读或上述成对读取遇到 401、403 或 404 时按初次失败清除列表并进入错误态。其他失败保留上次列表和计数，显示「更新失败，显示上次结果」及上次成功时间，不把计数改成 0，也不称为最新。退役集合仍只在初次加载和显式动作（含成对重试）后读取。
- 跨页可见刷新使用 `web/src/lib/useVisibleRefresh.ts`。它不自动发首次请求；`invalidate`、`refreshKey` 变化或卸载使该次回调的 `isCurrent()` 为 false。失效后若已失效请求仍在途又调用 `refresh()`，结束后只补一次新读。返回的 `refresh` 与 `invalidate` 保持稳定。`enabled: false` 只停自动唤醒，手动 `refresh()` 仍执行。

### 详情

入口详情只消费服务器 `observation_freshness`，不在前端再算阈值。首次加载仍走页面原请求；目标投影就绪后才启用可见刷新，避免与首屏并行再打一次 GET。

- 状态区展示聚合状态、「新鲜 n / 启用 m」和评估时间。`inactive` 只保留原控制态（暂停、维护中、已退役），不另标成观测过期。
- 聚合文案：`fresh` 观测新鲜；`pending` 等待新观测；`partial` 且含过期项为部分观测过期，否则为部分探测项等待观测；`stale` 观测已过期；`unobserved` 尚无观测；`uncovered` 未配置启用探测项。
- 探测项表按 `probe_item_id` 使用投影中的启用项：有效周期、最近有效观测、等待或过期期限。停用行显示「已停用」，不进入等待或过期数量。原始最近结果仍标为「最近结果」；`maintenance_context` 与 `is_backfilled` 另标为维护上下文或回填记录，不当作合格实时证据。
- 健康是最近已知健康，不随过期改写 incident。历史健康为正常且当前不是 fresh 时，不出现无保留的「当前正常」，并补充「最近一次正常，当前证据不足」。旧异常与观测过期同时显示，不宣称已恢复。
- 详情可见刷新只更新目标投影，不重置资料表单、焦点或探测草稿。资料草稿的保存冲突令牌是开始编辑时的 `updated_at`；投影刷新可以更新展示中的目标，但不能更换该令牌。取消、保存成功、切换目标，以及用户显式「以当前版本为基准」时才清除或更换令牌。每次资料、生命周期或探测项变更前使在途读失效；重叠变更全部结束后再读一次聚合，与哪一次先完成无关。路由切换或卸载后的迟到结果不覆盖当前目标。探测项创建、更新、停用/启用和删除成功后主动刷新聚合。普通网络失败保留上次快照，并显示「更新失败，显示上次结果」及时间，不把计数改成 0。401、403 或 404 仍按原详情失效语义清空目标。
