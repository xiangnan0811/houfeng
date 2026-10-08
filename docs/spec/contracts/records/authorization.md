# Record 授权边界（`recordauth`）

> 修改 `internal/center/recordauth/`、`internal/center/store/record_auth.go`、session middleware，或接入 records API/store/worker 授权时，必须加载本文件。

## Scenario: 可信 Actor Scope 与统一 `recordauth.Policy`

### 1. Scope / Trigger

- Trigger：新增或修改 records 的身份派生、group 查询、visibility/source evidence、`internal/center/records` revision/subject 输入、资源读写授权，或接入 records API、store、worker。
- `internal/center/recordauth` 是唯一的 v1 授权模型和 `Policy`，只能导入 Go 标准库；不得反向依赖 `auth`、HTTP、`store`、数据库驱动或未来业务包。
- Records API/store/worker 已接入可信 session、只读 group repository 与可复用 policy；所有调用方必须复用本 policy，不能各自重述 visibility。将 `recordauth.ErrDenied` 转换为不暴露资源存在性的 opaque 404。生产路由装配见 `cmd/houfeng-center/bootstrap.go`。

### 2. Signatures

```go
type ScopeRepository interface {
	ListActorGroupIDs(context.Context, ProjectID, string) ([]string, error)
}

func NewPostgresRecordAuthorizationRepository(pool *pgxpool.Pool) *PostgresRecordAuthorizationRepository // package store
func RequireSession(authn handlers.AuthService, scopes recordauth.ScopeRepository) func(http.Handler) http.Handler

func NormalizeActorScope(ActorScope) (ActorScope, error)
func NormalizeVisibilityScope(VisibilityScope) (VisibilityScope, error)
func NormalizeSourceAuthorization(SourceAuthorization) (SourceAuthorization, error)
func (Policy) Authorize(ActorScope, Capability, ResourceScope) error
func Authorize(ActorScope, Capability, ResourceScope) error

func records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues) (records.CompleteRevisionInput, error)
func records.AuthorizeRecordResource(ActorScope, Capability, records.RecordAuthorizationEvidence) error
```

- `sessionctx.WithActorScope` / `ActorScopeFromContext` 存取 typed actor 的防御性副本；`WithUserID` / `UserIDFromContext` 继续保留旧 user-id context 合同。
- production repository 只执行下列 stable-ID 查询，参数顺序固定为 project、user；不得读取 group 名称、描述或任何产品内容：

```sql
select g.group_id
from public.record_access_groups g
join public.record_access_group_members m on m.group_id = g.group_id
where g.project_id = $1 and m.user_id = $2
order by g.group_id asc
```

- 闭合集：project 仅 `ProjectIDDefault == "default"`；role 仅 `project_admin`、`viewer`；visibility kind 仅 `project`、`restricted`；source kind 仅 `vps`、`monitoring_instance`、`target`；各 scope version 与 `PolicyVersionV1` 均为 v1。Capability 仅为 `record.{read,create,update,delete,permanent_delete}`、`draft.{read,create,update,delete,publish}`、`evidence.{read,create,update,delete}`、`attachment.{read,create,update,delete}`、`search.read`、`activity.read`、`comparison.read`、`notification.{read,manage}`、`import.execute`、`export.execute`；未知字符串不是可扩展输入。

### 3. Contracts

- 可信 actor 只能由服务端 `authn.UserBySession` 的 `auth.User` 和 `ScopeRepository` 返回的持久化 `group_id` 构造：只把服务器端 `auth.RoleAdmin` 映射为 `recordauth.RoleProjectAdmin`，project 固定为 `default`，再走唯一的 `NormalizeActorScope`。它校验 opaque ID、闭合集，排序去重 group，并返回副本。
- 除服务端验证 session cookie 取得 `auth.User` 外，任何客户端 header、query、body 或其他 cookie 字段都不能决定 project、role、group、visibility、source floor 或 capability；`X-Project-ID`、`X-Role`、`X-Group-ID` 等即使出现也必须无效。成功时同时写 typed actor 与 legacy user ID；没有 optional/nil scope repository 或“空 group 降级”路径。
- `VisibilityScope` 用固定字段顺序、长度前缀的 canonical bytes 计算 SHA-256；角色/group 规范化为排序去重。`project` 不得携带 grant；空 `restricted` 是 deny-all，绝不等价于 project-wide。Policy 要求输入仍等于 canonical 形态及其 hash，不能信任 JSON、map 或调用方给出的摘要。
- `SourceAuthorization` 是严格 tagged union：`live` 当且仅当 `CurrentScope != nil` 且 `FinalFloor == nil && LastLiveScope == nil`；`tombstoned` 当且仅当 `CurrentScope == nil` 且 `FinalFloor != nil && LastLiveScope != nil`。tombstone 的 canonical `LastLiveScope` 是 transition witness，不是另一次可跳过的授权范围。
- `records.NormalizeCompleteRevisionInput` 只能接受服务端 adapter 解析并已经 canonical/digest-checked 的 source authorization，但必须接受完整 `live|tombstoned` 两种 union；不得在 `recordauth.NormalizeSourceAuthorization` 之上再叠加 live-only 条件。来源删除后的新修订继续保存 immutable capture scope，并以 witnessed final floor/last-live evidence 完成授权；live route 必须为空。历史 row 中的 evidence 不得当作 current scope cache，后续保存/读取仍由 adapter 重新解析 live 或 witnessed tombstone。
- 每个 source 的 capture/current/floor/witness 必须同 project。live 必须满足 `CurrentScope <= CaptureScope`；tombstone 必须满足 `LastLiveScope <= CaptureScope` 且 `FinalFloor <= LastLiveScope`。source digest 覆盖 kind、ID、state、capture 以及 live current 或 tombstone floor + witness，因此不得跨 source/state/transition 重放。
- `Policy.Authorize` 依次验证 actor、capability、canonical resource、project 相等、role-capability、resource visibility、每个 source 的 capture，及每个 live `CurrentScope` 或 tombstone `FinalFloor`。`project_admin` 拥有全部**已知** capability，但没有资源 scope、跨项目、union 完整性或 digest 的 bypass；所有交集都必须允许才可放行。
- 初始主管理员的管理权与 Records scope 独立：`is_supervisor` 不进入 actor，不绕过上述交集。组名称目录也不进入 `ScopeRepository`；成员变更在下一请求重新读取生效，已在途请求继续使用原 request-local actor，不宣称撤销在途写入。

### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| 缺 session cookie、session 无效/过期、认证 user 为空或 role 非服务器端 `auth.RoleAdmin` | middleware 固定 401 `{"error":"unauthenticated"}`；不查询 scope repository。 |
| repository 为 nil、DB/query/scan/rows 失败，或持久化 `group_id` / 已认证 user ID 不能规范化 | middleware 固定、不泄露原因的 503 `{"error":"authorization unavailable"}`；不得伪装为 401 或退化为空 group。 |
| repository project/user 参数不合法，或返回不符合 `rag_` 小写字母数字 grammar 的 group ID | repository 返回错误；middleware 按上述 opaque 503 fail closed。 |
| client 传入 role/project/group/visibility/source header、query 或 body | 不参与 actor/scope 构造，不能改变已认证 actor。 |
| 未知 role、capability、project、visibility/source kind/state/version、malformed ID、非 canonical scope/hash，或 resource 无 source | `Policy` 返回满足 `errors.Is(err, ErrDenied)` 的资源无关拒绝 reason。 |
| actor 与 resource project 不同，viewer 不具备 capability，或任一 visibility/capture/current/final floor 不允许 actor | `ErrDenied`；不可因 project admin 身份绕过 restricted scope。 |
| `restricted` 无 role/group grant，live/tombstone union 混用/缺字段，live widening，`LastLiveScope > CaptureScope`，或 `FinalFloor > LastLiveScope` | 拒绝；特别禁止 `capture=project → last live=restricted → final floor=project` 重新放宽。 |
| revision 输入携带 canonical witnessed tombstone，kind/source/project/digest 都匹配 | 接受并保留完整 tombstoned union；不得因 `State != live` 拒绝。 |
| revision 输入携带 adapter 未规范化、digest 漂移、live route 非空的 tombstone，或缺 floor/last-live witness | `ErrInvalidRevisionInput` / `ErrInvalidResolvedSubject`；正式保存前 fail closed。 |
| visibility hash、source digest 或 tombstone witness 被篡改/漂移 | 拒绝且错误中不得包含资源 ID、正文、scope grant 或 source ID。 |
| 未来 records HTTP 调用者收到 `ErrDenied` | 返回与不存在资源相同的 opaque 404；内部可通过 `DenialReasonFromError` 记录无资源细节的分类。 |

### 5. Good/Base/Bad Cases

- Good：已认证的 `auth.RoleAdmin` 在 `default` project 查询到 `rag_beta, rag_alpha, rag_beta`；context 中得到 `RoleProjectAdmin` 与排序去重后的 `rag_alpha, rag_beta`，legacy user ID 仍可被旧 handler 读取。
- Good：viewer 对 record read 的 role/group grant 同时通过 resource、capture 和 live/final floor，或显式获授 `project_admin` role 的 restricted scope，才由 policy 放行。
- Good：来源已删除但 full witness 给出 canonical final floor/last-live scope；adapter 返回空 route，revision normalization 与统一 policy 都接受该 tombstoned evidence，同时继续应用 capture/floor 交集。
- Base：用户没有任何 group membership 时仍创建合法 typed actor（空 group）；它只能依靠 role/project visibility，不可由客户端补 group。空 `restricted` 对所有 actor 仍为 deny-all。
- Bad：从 `X-Role` / `X-Group-ID` 构造 actor，查询 group 显示字段，或 repository 故障时把 groups 当作空数组继续执行。
- Bad：仅因 project admin 直接放行，或 tombstone 删除后把 final floor 放宽为 capture project；两者都会绕过强制交集/单调性证据。
- Bad：API、查询 builder 或 worker 自己翻译 visibility，而没有调用 `recordauth.Policy`；这会造成数据面间的授权漂移。

### 6. Tests Required

- `internal/center/recordauth/policy_test.go`：actor/visibility canonical 排序与副本、closed registry、viewer role/group allow、任一交集 deny、cross-project、empty restricted、project-admin 无 scope bypass、live widening、严格 union、`LastLiveScope`/final-floor 单调性、canonical hash/source digest/witness 篡改。
- `internal/center/store/record_auth_test.go`：精确 SQL（仅两张 ACL 表及 `group_id`）、`default`/user 参数、排序结果、query/scan/rows 错误和非法 DB group ID 全部 fail closed。
- `internal/center/http/middleware_test.go`、`internal/center/http/auth_e2e_test.go`：typed actor + legacy user context、伪造 headers 无效、缺失/过期 session 为 401、scope repository/非法 persisted group 为不泄露的 503。
- `cmd/houfeng-center/bootstrap_test.go`：APP runtime pool 构造 `NewPostgresRecordAuthorizationRepository` 并以两个参数调用 `RequireSession`，没有 nil/旧一参数 fallback。
- `internal/center/records/validation_test.go`、`authorization_test.go`：revision input 同时覆盖 canonical live 与 witnessed tombstoned union、digest 篡改、缺 floor/witness、live widening、删除 route 为空和多来源交集。

```sh
go test ./internal/center/recordauth -run RecordAuth -count=1
go test ./internal/center/store -run RecordAuth -count=1
go test ./internal/center/http -run 'SessionScope|RequireSession' -count=1
go test ./cmd/houfeng-center -run 'Bootstrap|Router' -count=1
go test -race ./internal/center/records -run 'Revision|Subject|Authorization|Tombstone' -count=10
git diff --check
```

### 7. Wrong vs Correct

#### Wrong

```go
// 客户端可伪造 scope，且 admin 绕过资源证据。
actor := recordauth.ActorScope{
	UserID: r.Header.Get("X-User-ID"),
	Role:   recordauth.Role(r.Header.Get("X-Role")),
}
if actor.Role == recordauth.RoleProjectAdmin {
	return nil
}

// records revision normalization：错误地覆盖 recordauth 的合法 tombstone union。
if authorization.State != recordauth.SourceStateLive {
	return records.ErrInvalidRevisionInput
}
```

#### Correct

```go
// session middleware：身份和 group 都来自服务端，再进行唯一规范化。
groups, err := scopes.ListActorGroupIDs(ctx, recordauth.ProjectIDDefault, user.UserID)
if err != nil { writeAuthorizationUnavailable(w); return }
actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
	UserID: user.UserID, Role: recordauth.RoleProjectAdmin,
	ProjectID: recordauth.ProjectIDDefault, GroupIDs: groups,
})
if err != nil { writeAuthorizationUnavailable(w); return }

// 未来资源调用者：每次使用同一 policy；拒绝对外等同不存在。
if err := (recordauth.Policy{}).Authorize(actor, recordauth.CapabilityRecordRead, resource); err != nil {
	if errors.Is(err, recordauth.ErrDenied) { writeNotFound(w); return }
	return err
}

// records revision normalization：唯一 normalizer 已验证 strict union 与 digest；
// records 层只再核对 subject kind/source identity，不增加 live-only 分支。
authorization, err := recordauth.NormalizeSourceAuthorization(input.CaptureAuthorization)
if err != nil || authorization.Digest != input.CaptureAuthorization.Digest {
	return records.ErrInvalidRevisionInput
}
```

## Scenario: 用户与权限组管理

### 管理权与数据边界

- 只有持久化的活跃主管理员可以管理账号、组和成员；新增账号始终是普通 `admin`，
  映射为 Records `project_admin`，没有第二种登录角色。普通管理员访问任何管理路由均
  返回 403 `management_forbidden`，先于输入解析及目标存在性检查。
- `/api/auth/me` 与登录成功响应独立返回 `management_capabilities: {access:boolean}`，
  仅由 `is_supervisor && disabled_at == nil` 得出，不由 role 或 Records 环境开关推断。
  管理路由和本人组目录不依赖 Records 开关；前端能力解析失败必须关闭管理入口。
- 账号可创建、列出、启用、停用及重置密码，不提供删除、改用户名、管理权转移。
  主管理员不可停用，也不可通过管理 API 重置自己的密码；返回 409
  `supervisor_protected`，改密使用需旧密码的既有自助路径。
- 组可创建、改名、增删成员，不提供组删除。稳定 `group_id` 保持 immutable revision
  引用；改名不改变成员或授权。名称 trim 后为 1–100 字符，项目内唯一，冲突为 409。
  旧成员无 user FK；添加必须确认目标账号存在、活跃且为 admin，停用目标返回 409
  `user_disabled`。移除只要求组存在和 user ID 格式合法，可清理悬空旧成员。
- C71 仅追加 `0071_add_access_management.sql`。旧库零用户留给 seed，单个 admin 提升
  为主管理员，多用户或唯一非 admin 整体失败回滚，不猜测初始身份。主管理员 CHECK
  与 partial UNIQUE index 在数据库中保证唯一且活跃。升级遵循
  [APP ACL current](../platform/app-acl-current.md)，Center runtime 不自行提权迁移。

### HTTP 与目录形状

管理 handler 使用既有 session cookie、RequireSession 和 RequireSameOrigin；只从
sessionctx 取 user ID。请求拒绝未知字段，不接受客户端 role、supervisor、project 或 actor。
密码不出现在 DTO、日志、URL 或本地草稿中。以下目录均返回全量稳定排序的 `items`：
用户按 username/user_id，组按 display_name/group_id。

| 方法与路径 | 请求 | 成功响应 |
| --- | --- | --- |
| GET `/api/admin/users` | 无 | 200 `{items:UserSummary[]}` |
| POST `/api/admin/users` | `{username,password,display_name}` | 201 UserSummary |
| POST `/api/admin/users/{id}/disable`、`/enable` | `{}` | 200 UserSummary |
| POST `/api/admin/users/{id}/reset-password` | `{password}` | 204 |
| GET `/api/admin/record-access-groups` | 无 | 200 `{items:GroupSummary[]}` |
| POST `/api/admin/record-access-groups` | `{display_name}` | 201 GroupSummary |
| PATCH `/api/admin/record-access-groups/{id}` | `{display_name}` | 200 GroupSummary |
| GET `/api/admin/record-access-groups/{id}/members` | 无 | 200 `{items:MemberSummary[]}` |
| PUT `/api/admin/record-access-groups/{id}/members/{user_id}` | `{}` | 204 |
| DELETE `/api/admin/record-access-groups/{id}/members/{user_id}` | 无 | 204 |
| GET `/api/record-access-groups/mine` | 无；任何活跃登录账号 | 200 `{items:GroupSummary[]}` |

- `UserSummary` 仅有 `user_id,username,display_name,role,is_supervisor,disabled_at,created_at`；
  活跃账号的 `disabled_at` 为 null，普通账号的 `is_supervisor` 为 false，字段不省略。
- `GroupSummary` 仅有 `group_id,display_name`。`MemberSummary` 正常行为完整、扁平的
  UserSummary；悬空行仅 `{user_id,missing:true}`，界面显示“账号不可用”，不当作新用户。
- 本人组目录只返回实际成员组；主管理员也不自动获得全组作为编辑器可选项。
  成员逐个 PUT/DELETE，添加已有成员与移除不存在成员均为 204，不提供 bulk replace。
- 错误为 `{error,code}`：400 `invalid_request`、401 既有未认证响应、403
  `management_forbidden`、404 `resource_not_found`、409
  `username_taken|group_name_taken|supervisor_protected|user_disabled`、
  503 `management_unavailable`。SQL 或依赖细节不得回传，客户端只显示白名单中文文案。

### 事务与撤权

- 每个管理读重新确认主管理员；每个写为 ReadCommitted 事务，先锁 actor users 行并
  重新确认 role、supervisor 和活跃状态，再按 actor→target user→group→membership/sessions
  锁序执行。self-target 复用已持有的 actor 锁。密码哈希在事务外计算，事务内再授权。
- 停用与删除该账号全部 sessions 同一事务，重复停用不改首次时间。启用只清
  disabled_at，不恢复旧会话。重置密码使用配置 bcrypt cost，更新密码 watermark 并
  同事务撤销全部 sessions；重置停用账号不自动启用。会话锁内检查 disabled/hash，
  不允许 bcrypt 与停用/重置竞态重新建立旧授权会话。
- 组撤权不注销登录 cookie；被撤权者 `/me` 仍为 200，但 Records current/history/
  附件等读取经过原 policy 返回 opaque 404。恢复成员可恢复 scope；管理员没有资源 bypass。
  客户端重验 403/404/410 时先同步撤销内容租约、关闭闩和代次，并清空内存中的正文、附件与草稿，再异步删除本地未同步 buffer。删除被拒绝或尚未结束时不得继续展示受保护内容，也不得在卸载或迟到的缓冲读取时写回。不把资源 404
  当作 session 失效。导入主体自身仍 project 可见时，记录组撤权不等于撤销 import capability。
- 发布 revision 继续同时检查旧、新 visibility；角色与组 grant 为 OR。选择
  `project_admin` 包含全部当前 admin，不能排除单个 admin；空 restricted 是 deny-all。

### 回归证据入口

- `internal/center/accessadmin` 与 `internal/center/http/handlers/access_admin_test.go`：
  管理授权先于输入验证、未知字段、密码策略、目录 DTO 和脱敏错误。
- store 的 `TestPostgresIntegrationAccessManagement*`：真实事务、并发 seed/登录/
  停用/重置边界、回滚、稳定成员 ID、A/B scope 撤销与恢复且 cookie 仍有效。
- migrate 的同名前缀测试：零/单用户迁移与异常多用户回滚；APP ACL current 套件：
  冻结 C70 前驱、精确四 tuple 增量、191 条 predecessor chain 和并发迁移序列化。
- Web 行为测试覆盖 capability gating、异步成员归属、敏感草稿、project 清 grant、
  本人组选择、目录失败不丢 grant，以及撤权时先清空工作区再删 buffer（删除失败或未完成也不写回）。真实浏览器仍须以两个独立
  cookie jar 和产品管理 UI 验证，不得用测试、直接 ACL 写表或登出替代撤权验收。
