# 全局命令审计中心

## Goal

让已永久保存的命令审计元数据形成可追溯、可筛选、可稳定翻页的管理闭环，同时补齐敏感命令可信拒绝事件和删除后身份可读性，且不暴露命令输出。

## Confirmed Facts

- 审查基线为 `origin/main@a375c0b0`（v0.58.9）；实施前 fetch 后未发现命令 handler、审计 store/schema、监控实例清理、router/bootstrap、前端路由或历史页面漂移。
- 现有 `0046_create_command_action_audit.sql` 已永久记录 `queued`、`dispatched`、`completed` 元数据，但没有全局读取 API 或 UI。
- 审计写入已经与排队、派发、完成状态写入处于同一数据库事务；`last_action` 才短期保留 stdout/stderr，审计表不应保存或返回输出。
- 系统目前只有 `admin` 一种真实角色，因此本任务沿用 session 与同源保护，不引入无实际隔离价值的 RBAC。
- 旧远端分支 `plan/command-governance-next-stage` 只作为需求来源，不合并、不 cherry-pick，本任务从当前主线重新实现。
- RBAC 后续工作已记录为 GitHub enhancement [#381](https://github.com/xiangnan0811/houfeng/issues/381)。

## Requirements

### Permanent identity and audit integrity

- 永久审计保存监控实例名称、actor 用户名和显示名的事件时快照；稳定 ID 始终是权威身份，旧记录以迁移时仍可见的名称回填。
- 删除用户或永久清理监控实例后审计仍须保留、可查询、可解释；旧二进制回滚后仍能写原有三种事件。
- 新审计写入必须确认实例真实存在；带 actor 的 Web 事件还必须确认 actor 真实存在。任何调用方不得自行拼接审计 INSERT。
- `rejected` 是唯一不产生 action ID 的事件；普通 action 的 `queued`、`dispatched`、`completed` 必须有 action ID。
- 审计详情不得包含 stdout/stderr；拒绝事件只允许公开规范化原因 `sensitive_confirmation_required`。

### Trusted sensitive-command rejection

- 可信拒绝仅指：请求已有认证会话、命令是已知敏感命令、目标实例真实且当前可执行、请求缺少 `confirmed_sensitive:true`。
- 可信拒绝不生成 action、不排队、不更新 `last_action`，只写一条 `rejected` 事件，然后保持现有 400 响应。
- 无效 JSON、缺少或未知命令、不存在实例、归档实例、未绑定实例、暂停实例均不得写拒绝审计。
- 缺少确认仍具有现有 400 对外优先级；为判定可信性发生的实例读取或应写审计失败必须返回 500，不得静默丢审计。

### Global read API

- 提供 session 保护的 `GET /api/command-audits`，当前不增加角色判断。
- 首次请求支持：`window=24h|7d|30d|all|custom`（默认 `30d`）、custom 的 `started_from`/`started_to`、`monitoring_instance`、`command_id`、`sensitivity`、`outcome`、`actor`、`action_id`、`limit`（默认 20，最大 100）。
- `monitoring_instance` 与 `actor` 按转义后的字面量子串匹配，`%`、`_` 和 `\\` 不得变成 SQL 通配符；其余筛选按规范化精确值匹配。
- 非法枚举、时间组合、limit、cursor 以及 cursor 与其他查询参数混用均返回 400。
- 每项代表一次 action/attempt：普通 action 以 action ID 分组，rejected 以 audit ID 作为稳定分组 ID。
- `started_at` 为该组最早事件时间；action 排序固定为 `started_at DESC, id DESC`，嵌套事件固定为 `occurred_at ASC, audit_id ASC`。
- outcome 优先级固定为 rejected → completed（exit 0 为 succeeded，否则 failed）→ dispatched → queued。
- 首次请求固定时间上下界；下一页 cursor 封装版本、规范化筛选、limit、固定边界和末尾排序键。续页只接受 cursor，加载期间不得因时间推进或新事件产生重复、跳页。
- API 使用允许列表映射响应字段，不透传数据库 `details`；类型和 JSON 中均不得出现 stdout/stderr。

### Web experience

- 新增懒加载私有页面 `/command-audit`，侧栏入口位于“观测”分组，并在监控实例详情菜单提供实例预筛选入口。
- 默认 30 天不写入 URL；仅非默认窗口和有效筛选写入 URL，非法或冗余 URL 要 canonicalize。
- 主要筛选为时间、实例、命令、结果；敏感级别、actor、action ID 位于高级筛选抽屉，草稿可应用或丢弃。
- 页面展示高密度 action 汇总表；每行以语义按钮展开原始事件时间线。筛选改变时清除旧结果、cursor 与展开状态。
- 当前实例使用快照名称并链接详情；已删除实例显示稳定 ID、快照名称和“已删除”，不得生成失效详情链接。
- 页面明确声明只含元数据。即使测试或恶意响应附加 stdout/stderr，界面也不得渲染这些字段。
- 样式复用现有 token、DataTable、Badge、PageState、Modal/Drawer 与 events/observability CSS owner，不新增依赖、Context、独立 CSS 或 JSX inline style。

### Cleanup, operations, and documentation

- 管理审查 counts 增加 `command_action_audit_count` 并纳入 evidence；永久清理保留这些审计且不把它们计入 `deleted_reference_count`，确认文案必须说明保留行为。
- 增加真实 PostgreSQL 升级、重复迁移、旧写法兼容、删除后保留、聚合/筛选/cursor/rejected 与清理后读取测试。
- 使用代表性多 action 数据记录 `EXPLAIN (ANALYZE, BUFFERS)` 证据；查询必须受固定窗口、limit 和索引约束，且不得 N+1。若实测不可接受，回到设计而不是带全表扫描上线。
- 新路由加入 fail-closed fixture、core route、accessibility 与 staging smoke，核心矩阵由 9×3 更新为 10×3，并同步浏览器验证文档。
- 更新 backend database、web state/data、页面职责和浏览器验证规范；完成报告区分单元测试、真实 PostgreSQL、fixture 浏览器、真实部署与 CI 证据。

## Acceptance Criteria

- [ ] `0050`（若主线占用则顺延）从 0046 结构升级成功且重复应用安全；快照回填、兼容默认值、具名约束和三个索引在 PostgreSQL 上得到验证。
- [ ] 旧式 queued/dispatched/completed INSERT 在升级后仍成功；删除用户和实例后相应审计行及快照仍存在。
- [ ] queued/dispatched/completed 与状态写入仍原子；统一 helper 在实例或 actor 不存在、INSERT 不是恰好一行时失败并回滚。
- [ ] 所有可信敏感拒绝恰好写一条 rejected；所有非可信输入不写；拒绝路径不创建 action、不改变排队列或 `last_action`，错误优先级符合要求。
- [ ] 管理审查公开 `command_action_audit_count` 并将其纳入 evidence；归档实例永久清理后审计仍可由全局 API 读取且 `deleted_reference_count` 不含审计。
- [ ] API 的五种 outcome、全部筛选、默认/自定义时间、非法输入、事件顺序、action 排序和 `limit+1` 分页均有 RED→GREEN 测试。
- [ ] cursor 续页只需 cursor；固定边界和复合 keyset 在插入新事件及相同时间戳数据下不重复、不漏项。
- [ ] API 和 Web 对构造的 stdout/stderr 字段均不输出、不渲染；拒绝只暴露规范化原因。
- [ ] `/command-audit` 完成 URL canonicalization、草稿应用/丢弃、加载更多、展开、actor fallback、已删除实例、加载/错误/空状态、键盘与局部横向滚动测试。
- [ ] 路由、侧栏、详情入口、fixture、10×3 core routes、accessibility 和 staging smoke 均更新并通过。
- [ ] PostgreSQL integration、fresh-install/upgrade smoke、`make verify-go`、`make verify-web`、`npm --prefix web run test:e2e` 和 `git diff --check` 全部通过。
- [ ] 功能分支已提交、推送并创建 PR；required CI 通过。未经用户新授权不自动合并、发布、删除旧远端分支。

## Out of Scope

- 不实施 RBAC、第二种角色、实例级审计可见范围或 `authorization_denied`；由 #381 触发后续设计。
- 不提供导出、自动保留期、合规删除、摘要表或新的审计写入队列。
- 不恢复、不合并、不删除 `plan/command-governance-next-stage`。
- 不自动合并 PR 或发布版本。

## Residual Boundaries

- 永久审计会持续增长；本期依靠认证、同源保护、默认 30 天、响应上限、keyset cursor 和索引控制风险。
- 多人角色、大规模写入或合规删除要求出现时，必须重新评估访问控制、容量、隐私和保留策略。
- 验收证据可显著降低风险，但不能被描述为“零风险证明”。

## Open Questions

无。用户已审查方案并明确要求按本任务实施。
