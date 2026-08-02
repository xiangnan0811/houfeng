# Actor scope 与统一 recordauth.Policy

## Goal

交付 records platform 的第一个可运行授权边界：HTTP session 只从服务器端认证结果和持久化 group 成员关系派生 typed actor scope；唯一的 `recordauth.Policy` 以 fail-closed 方式验证 capability、visibility、source authorization 和 project 边界。它是 records core、outbox、worker 与下游 records 子任务可复用的基础，而不是一次性 HTTP 权限补丁。

## Confirmed facts

- 本切片从干净的 `80b563ee`（APP runtime admission）开始；冻结旧树 `/home/murray/.codex/worktrees/23bf/houfeng` 不可读取、复制或续办。
- `auth.User` 已由 session 服务端读出，但当前 middleware 只把 `user_id` 放入 context；唯一现有角色是 `admin`。
- `0051` 已有 `record_access_groups` 和 `record_access_group_members`，运行时 ACL 对二者只有读取权限；它们足以形成当前 actor 的稳定 group ID 集，但没有 group 的显示正文。
- 父任务 Task 2 定义 `ActorScope/Capability/VisibilityScope/SourceAuthorization/ResourceScope/Policy` 和其 intersection 规则。修订 checkpoint 的 Task 9 进一步要求 `RequireSession(authn, scopes)`、持久化 group hydration、无 header scope 路径与 scope-store 故障的安全 503。
- 当前干净基线没有 records deletion status/audit repository、handler、worker 或 `operation_initiator_user_id` 查询面。因此不能把一个未被生产调用的 deletion SQL filter 误报为 Task 9 的完成；该接入由拥有真实数据面的后续 deletion 子切片负责。
- 用户已授权总控自主选择并连续推进单一大切片；本任务仍须在代码开始前完成本 PRD、design 和 implement 的自审。

## Requirements

1. 新建不依赖 HTTP、store、业务域或 future records 包的 `internal/center/recordauth`。它必须拥有所有授权类型、已知 capability 集、project-admin/viewer 角色、严格输入校验、canonical normalization 和唯一 `Policy` 实现；未知 capability、role、scope version、visibility kind、source kind、project 或 malformed ID 均默认拒绝。
2. 现阶段 project ID 精确为 `default`。middleware 只能把经 `auth.Service.UserBySession` 验证的 `auth.RoleAdmin` 映射为 `recordauth.RoleProjectAdmin`；空/未知 user 或 role 均不能产生 actor。浏览器 header、query、cookie 以外的 client 字段、visibility、role、group 和 source floor 都绝不能成为 scope 输入。
3. `RequireSession` 必须接受 `recordauth.ScopeRepository`，在认证成功后读取该 user/project 的持久化 group IDs，经唯一 `recordauth.NormalizeActorScope` 规范化后，同时写入 typed actor 和现有 user ID context。scope repository 失败或返回不合法持久化值时返回不含内部错误细节的 503；不能降级为空 group、不能伪装为 401。
4. `PostgresRecordAuthorizationRepository` 只能从 `record_access_groups` 和 `record_access_group_members` 读取 `group_id`，按 project/user 约束、稳定排序并且不读取名称、描述或其它产品内容。它实现 middleware 所需的最小接口，测试使用窄 pgx seam。
5. policy 的资源授权必须同时验证：actor project、resource visibility、每个 source 的 capture scope，以及 live current scope 或 tombstoned final floor。source state 是严格 union：live 当且仅当 `CurrentScope` 非空且 `FinalFloor`/`LastLiveScope` 为空；tombstoned 当且仅当 `CurrentScope` 为空且 `FinalFloor`/canonical `LastLiveScope` transition witness 非空。live scope 与 tombstone `LastLiveScope` 都不能比 capture scope 更宽，final floor 不能比 `LastLiveScope` 更宽；floor/witness hash 和 source digest 必须重算并匹配。任何缺失、widening、hash 漂移或未知 source kind 都拒绝。
6. `sessionctx` 保持既有 `UserIDFromContext` 兼容性，并新增 typed actor getter/setter。当前 router 的 opaque auth-middleware 注入保持不变；bootstrap 在同一 APP runtime pool 上创建 scope repository 后传给 `RequireSession`，不增加未使用的 RouterOptions 字段。
7. 外部资源调用方将 `recordauth.ErrDenied` 映射为 opaque 404；本切片提供不含资源 ID/正文的内部 deny reason，但不创建 records HTTP endpoint。任何未来 store query/worker 必须复用同一 policy，而非重新解释 visibility。

## Acceptance Criteria

- [x] `recordauth` table-driven tests 覆盖 project admin、viewer role、viewer group allow、角色/组/来源交集拒绝、cross-project 拒绝、未知 capability/kind/version/visibility、restricted deny-all、live widening、缺失/篡改 tombstone floor 或 `LastLiveScope`、`capture=project → last live=restricted → final floor=project` reopening 拒绝，以及 canonical digest 漂移。
- [x] 规范化在不接受宽松字符串的前提下稳定排序和去重 group/role 输入；同一 logical scope 得到相同 canonical bytes/hash，project 与 empty restricted scope 不可混淆。
- [x] scope repository 的 SQL 只返回 stable group IDs，带 project/user 参数；查询错误、scan 错误和非法 DB 值均 fail closed。
- [x] 成功 session 请求的 handler 能观察到 `{UserID, RoleProjectAdmin, ProjectIDDefault, sorted group IDs}`；`X-Project-ID`、`X-Role`、`X-Group-ID` 对该值没有影响。
- [x] scope repository 不可用时 middleware 返回安全 503；缺失/失效 session 仍返回既有 401；不出现空 actor 或 client-controlled fallback。
- [x] 现有非 records HTTP handlers 仍能通过 `UserIDFromContext` 工作；bootstrap 使用 production repository，且不存在 nil/optional production fallback。
- [x] focused tests、全包相关测试、`gofmt` 与 `git diff --check` 有可复核 PASS 证据，并经 spec-compliance 与 code-quality 两阶段审查。

## Out of scope

- 不改 APP ACL/manifest/migrator、0051、runtime admission、projector 或任何外部 recovery domain。
- 不创建 records/evidence/attachment 表、records HTTP endpoint、outbox、worker、delete operation/status/audit repository 或 SQL filter 的生产 caller。
- 不把此基础层单独宣称为 PF-AC-001/PF-AC-002 的最终完成；真实 API、store query builder 和 worker 的全路径 parity 由其各自拥有的后续切片接入后统一验收。
- 不归档仍为 `in_progress` 的 `07-24-app-acl-migration-runtime-handoff`，不放行 Child 2–11。

## Rollback

该切片只新增 Go 类型、只读 repository 和 session wiring，不写新的 migration。回滚 binary 可恢复为此前 session 行为；在任何不确定的 actor/group/scope 情形下保持拒绝，而不是扩大访问。
