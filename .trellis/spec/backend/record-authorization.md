# Record 授权边界（`recordauth`）

> 修改 `internal/center/recordauth/`、`internal/center/store/record_auth.go`、session middleware，或接入 records API/store/worker 授权时，必须加载本文件。

## Scenario: 可信 Actor Scope 与统一 `recordauth.Policy`

### 1. Scope / Trigger

- Trigger：新增或修改 records 的身份派生、group 查询、visibility/source evidence、资源读写授权，或接入 records API、store、worker。
- `internal/center/recordauth` 是唯一的 v1 授权模型和 `Policy`，只能导入 Go 标准库；不得反向依赖 `auth`、HTTP、`store`、数据库驱动或未来业务包。
- 本切片只落地可信 session seam、只读 group repository 和可复用 policy；**尚无** records endpoint 或当前 deletion adapter。后续 API/store/worker 必须复用本 policy，不能各自重述 visibility；将 `recordauth.ErrDenied` 转换为不暴露资源存在性的 opaque 404。

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
- 每个 source 的 capture/current/floor/witness 必须同 project。live 必须满足 `CurrentScope <= CaptureScope`；tombstone 必须满足 `LastLiveScope <= CaptureScope` 且 `FinalFloor <= LastLiveScope`。source digest 覆盖 kind、ID、state、capture 以及 live current 或 tombstone floor + witness，因此不得跨 source/state/transition 重放。
- `Policy.Authorize` 依次验证 actor、capability、canonical resource、project 相等、role-capability、resource visibility、每个 source 的 capture，及每个 live `CurrentScope` 或 tombstone `FinalFloor`。`project_admin` 拥有全部**已知** capability，但没有资源 scope、跨项目、union 完整性或 digest 的 bypass；所有交集都必须允许才可放行。

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
| visibility hash、source digest 或 tombstone witness 被篡改/漂移 | 拒绝且错误中不得包含资源 ID、正文、scope grant 或 source ID。 |
| 未来 records HTTP 调用者收到 `ErrDenied` | 返回与不存在资源相同的 opaque 404；内部可通过 `DenialReasonFromError` 记录无资源细节的分类。 |

### 5. Good/Base/Bad Cases

- Good：已认证的 `auth.RoleAdmin` 在 `default` project 查询到 `rag_beta, rag_alpha, rag_beta`；context 中得到 `RoleProjectAdmin` 与排序去重后的 `rag_alpha, rag_beta`，legacy user ID 仍可被旧 handler 读取。
- Good：viewer 对 record read 的 role/group grant 同时通过 resource、capture 和 live/final floor，或显式获授 `project_admin` role 的 restricted scope，才由 policy 放行。
- Base：用户没有任何 group membership 时仍创建合法 typed actor（空 group）；它只能依靠 role/project visibility，不可由客户端补 group。空 `restricted` 对所有 actor 仍为 deny-all。
- Bad：从 `X-Role` / `X-Group-ID` 构造 actor，查询 group 显示字段，或 repository 故障时把 groups 当作空数组继续执行。
- Bad：仅因 project admin 直接放行，或 tombstone 删除后把 final floor 放宽为 capture project；两者都会绕过强制交集/单调性证据。
- Bad：API、查询 builder 或 worker 自己翻译 visibility，而没有调用 `recordauth.Policy`；这会造成数据面间的授权漂移。

### 6. Tests Required

- `internal/center/recordauth/policy_test.go`：actor/visibility canonical 排序与副本、closed registry、viewer role/group allow、任一交集 deny、cross-project、empty restricted、project-admin 无 scope bypass、live widening、严格 union、`LastLiveScope`/final-floor 单调性、canonical hash/source digest/witness 篡改。
- `internal/center/store/record_auth_test.go`：精确 SQL（仅两张 ACL 表及 `group_id`）、`default`/user 参数、排序结果、query/scan/rows 错误和非法 DB group ID 全部 fail closed。
- `internal/center/http/middleware_test.go`、`internal/center/http/auth_e2e_test.go`：typed actor + legacy user context、伪造 headers 无效、缺失/过期 session 为 401、scope repository/非法 persisted group 为不泄露的 503。
- `cmd/houfeng-center/bootstrap_test.go`：APP runtime pool 构造 `NewPostgresRecordAuthorizationRepository` 并以两个参数调用 `RequireSession`，没有 nil/旧一参数 fallback。

```sh
go test ./internal/center/recordauth -run RecordAuth -count=1
go test ./internal/center/store -run RecordAuth -count=1
go test ./internal/center/http -run 'SessionScope|RequireSession' -count=1
go test ./cmd/houfeng-center -run 'Bootstrap|Router' -count=1
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
```
