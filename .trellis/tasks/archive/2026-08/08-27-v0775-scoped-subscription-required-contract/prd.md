# 执行 VPS scoped subscription required 合同

## Goal

让 VPS scoped subscription create 在运行时严格执行共享 manifest 的 required/nullability，并用真实 handler HTTP 测试区分 missing、null 与显式零值，同时保持 collection create 和成功 payload 兼容。

## Requirements

1. `price`、`currency`、`billing_cycle`、`billing_months`、`auto_renew`、`auto_renew_cancelled`、`payment_method`、`note` 缺失时必须返回 `400`，且不得调用 repository。
2. 上述八个非空 required 字段显式 `null` 时必须返回 `400`，且不得调用 repository。
3. optional non-nullable 的 `billing_period_unit`、`billing_period_length`、`renewal_mode` 可以缺失但不能为 `null`。
4. optional nullable 的 `started_at`、`renew_at` 可以缺失或显式为 `null`。
5. presence 与值域分离：`price: 0`、`auto_renew: false`、`auto_renew_cancelled: false`、`payment_method: ""`、`note: ""` 都算显式提供，并按原值映射；既有 domain validation 继续处理币种、周期等业务值域。
6. 复用 `subscriptions.Optional*` presence wrappers，禁止再造第二套 JSON presence 类型。
7. 共享 manifest、Go request 与 TypeScript DTO 的 name/type/required/nullable contract test 必须理解 wrapper 与 `required` tag；生产验证不得依赖源码文本 parser。
8. collection `POST /api/subscriptions` 的 request 类型与行为不变。
9. 合同 source parser 必须对 union 做 exact-member 分类；`number | string`、`string | undefined`、未知或空 union member 必须 fail closed，不能按关键字包含关系误判为 manifest-compatible。
10. Go DTO mirror 只接受明确的内建/wrapper 类型；未知 named primitive 不得按 `reflect.Kind` 猜测。manifest 的 `required` / `nullable` 必须显式存在且非 null。
11. 两个 DTO mirrors 对 exported Go 字段缺失/空 JSON tag 必须 fail closed；只有精确 `json:"-"` 可忽略。Go mirror 必须在 pointer `Elem()` 前拒绝 unknown named pointer。
12. 两个 mirrors 对 anonymous embedded field 一律 fail closed，包括未导出 anonymous struct type；唯一例外是 tag 精确为 `json:"-"` 且无 options。普通 non-anonymous unexported field 可继续忽略。
13. scoped `status` unknown-field 回归必须带有效 idempotency key，精确断言 `error: "invalid json"`，并证明 idempotent repository create 未被调用。
14. `BillingPeriodUnit` 与 `RenewalMode` 只有在同一 TypeScript source 中的 alias 定义全部是非空 string literal 时才可映射为 string；alias 加入 `number`、`undefined`、未知 alias 或空 member 必须让 TS/Go mirrors fail closed。
15. TypeScript object source parser 必须拒绝 closing brace 后除空白/单一分号外的同声明 suffix，包括 `& { debug?: string }`；不能在首个 `\n}` 静默截止。
16. Go-source mirror 必须在 anonymous embedding 分类前处理或拒绝 trailing inline comment；合法 embedded declaration 不能因注释变成多 token 后被当作普通 unexported field 跳过。
17. Source markers 与 approved alias definitions 必须各自只有一个注释/字符串之外的 live declaration；block-comment shadow-only 或 shadow+live 都必须失败。alias tokenizer 必须保留 quoted literal 内的 `|` 与 escapes。
18. TS Go-source mirror 必须精确读取 `json` / `required` struct-tag keys，不能接受 `notjson` / `notrequired`；Go mirror 对未知 named primitive 必须直接 panic/reject。
19. 两个 object mirrors 必须拒绝 closing brace 后跨行或跨注释 trivia 的 `&` / `|` declaration continuation；两个 approved alias mirrors 必须对多行 continuation fail closed。TS Go-source mirror 必须处理 raw tag 外的 inline block comment，并拒绝未闭合/跨行 block comment，不能静默省略 anonymous embedding。

## Out of scope

- AST parser、OpenAPI/codegen 或通用 DTO 生成。
- 改变 manifest 字段集、数据库 schema、receipt/idempotency 语义。
- 将允许为空的 payment method 或 note 改成非空业务字段。

## Acceptance Criteria

- [x] 16 个 required missing/null case 通过真实 scoped handler 返回 `400`，repository spy 为零调用。
- [x] 三个 optional non-nullable 字段的 null case 返回 `400`。
- [x] 两个 nullable date 的 null case 成功并映射为 nil date。
- [x] 一条完整请求证明显式零值、false 与空字符串不被误判为 missing，repository 收到精确值。
- [x] scoped handler 的 replay、reuse、repository failure 等现有成功前置 fixture 补齐 required keys 后保持原语义。
- [x] Go/TypeScript contract tests继续检测字段名、类型、requiredness 与 nullability 漂移。
- [x] TypeScript 与 Go 镜像 parser 都有直接负例，证明 mixed primitive union 与 `undefined` 会被拒绝。
- [x] 未知 Go DTO 类型、manifest 缺失/null `required` / `nullable` 都有直接负例并 fail closed。
- [x] missing tag、`json:""`、`json:",omitempty"` 在 Go/TS mirrors 均失败，精确 dash 被忽略；unknown named date pointer 被直接拒绝。
- [x] exported 与 unexported-type anonymous embedding 在 Go/TS mirrors 均失败；只有精确 `json:"-"` 被忽略，`json:"-,omitempty"` 不算例外。
- [x] `status` 回归携带有效 `Idempotency-Key`，返回 `400` + `error: "invalid json"`，`createSubscriptionCalls == 0` 且 key capture 为空。
- [x] TS 与 Go mirror 都有 alias-definition widening negatives；`BillingPeriodUnit` / `RenewalMode` 加入 `number` 或 `undefined` 时合同失败，当前纯 string-literal 定义通过。
- [x] 两个 TypeScript-object mirrors 拒绝 intersection/non-semicolon suffix；TS Go-source mirror 对 trailing-comment anonymous embedding 产生显式 anonymous-field rejection。
- [x] 两侧 direct negatives 覆盖 commented DTO/alias marker、near-miss tag keys 与 unknown named primitive direct rejection；embedded-pipe/escaped string aliases 保持合法。
- [x] 两侧 direct negatives 覆盖 later-line object intersection、两个 approved alias 的 continuation-line widening；TS mirror 还覆盖 block-comment anonymous embedding、raw-tag comment-marker 保留与 unterminated block-comment rejection。
- [x] `go test ./internal/center/http/handlers -count=1` 通过。

## Constraints

- 严格 TDD：先用 handler 测试得到 missing/null RED，再改生产解码边界。
- 错误响应可沿用现有 `invalid json` / `invalid input` 类别；核心合同是 `400` 与无 repository 副作用。
- 只改 scoped request；不得为方便而把 collection request 一并换型。
