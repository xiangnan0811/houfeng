# 封箱 v0.77.5 VPS 详情 hardening

## Goal

在 `v0.77.5` 对应提交 `6c91b128a621e0adf0b2ce2e6434ebc3ad758340` 上修复复审确认的 VPS scoped subscription create required/nullability 合同、Legacy 异步写入/刷新/archive review 归属，并封住终审发现的 production capability-gate remount、same-VPS reload/review 交错、early-effect cleanup、cancellation preview、DTO parser 假绿和 Trellis context 截断。用真实 handler、真实 `VPSDetailPage` 入口、严格合同负例、可控 deferred UI 与正式 Chromium 覆盖交付边界。

## Background

`v0.77.5` 已建立字段语义 manifest、每 VPS 写锁和 mutation generation，但仍有三条可复现缝隙：scoped handler 将缺字段折叠成 Go 零值；组件级 submitting 布尔值会跨 VPS 阻塞或被旧请求清空；detail/services/domains 以及 archive review 的晚到结果只按 VPS ID 或完全不校验 owner。附件复审同时指出源码文本 DTO parser 脆弱，本任务仅做新 wrapper 所需的最小兼容，解析器重构不夹带进 P2 修复。

## Requirements

1. VPS scoped subscription create 必须以共享 manifest 的 `required` 与 `nullable` 语义为运行时合同。八个 required 字段缺失或显式 `null` 均返回 `400` 且不得调用 repository；可空日期接受 `null`；显式 `0`、`false` 与空字符串不得被当作缺失。
2. collection `POST /api/subscriptions` 的现有输入与幂等合同保持不变；只收紧 VPS scoped create 门。
3. Legacy 所有异步写入的 transport owner 必须绑定到 `vpsId + token + generation + operation`。同一 VPS 仍串行，不同 VPS 可并发；A 的晚到 `finally` 不得清掉 B 的 pending，也不得在 B 上制造通知、收起抽屉或导航。
4. detail、services、domains 的二阶段刷新在执行任何 state commit 前同时校验当前 VPS 与 generation owner，封住 A→B→A ABA。
5. archive review 使用独立的 request owner；路由切换、关闭弹窗、再次打开均使旧请求失效。旧请求的 `then`、`catch`、`finally` 均不得修改当前弹窗。
6. 回归测试必须使用可控 deferred promise/response，不以 sleep 猜测竞态时序。
7. 将新合同写回 backend subscription 与 web state ownership 规范；不引入新 API、迁移、代码生成或通用请求框架。
8. VPS route effect 的正常与所有 early-return 分支都必须注册完整 cleanup：失效 archive review 与 mutation generation、释放 latest-load authority，但不得提前删除仍在途的 transport owner。
9. 关闭 cancellation Drawer 必须失效普通 preview request generation；旧 preview 不能在关闭后提交，也不能阻止重开时获取新 review。
10. TypeScript 与 Go 镜像 source parser 必须 fail closed：拒绝 `undefined`、混合 JSON primitive union（如 `number | string`）、未知 union member 和空 member；允许当前 DTO 使用的纯 string alias union 与 nullable date。完整 AST/codegen 重构仍不在本轮。
11. 核心 ownership 与验证规则必须位于可完整注入的 bounded spec/task contract 中；父任务及两个 web 子任务不得再依赖被 32768-byte 上限截断后才出现的权威段落。
12. 权威 spec 示例必须调用真实签名，并明确 `finishVpsWrite` 以 map key `vpsId` + 唯一 token 释放；generation/operation 是 view/UI metadata，不扩大 transport release identity。
13. 最终验证矩阵必须显式包含正式 Chromium `npm --prefix web run test:e2e`。
14. Transport owner store 必须存活于 `VPSDetailPage` capability gate 对 Legacy 子树的 probing/unmount/remount；A pending → B → A-before-settle 不得获得空 registry 或第二个 subscription 幂等 key。store 不能是跨 page/test 泄漏的 module-global singleton。
15. 同 VPS query reload 的 route payload 若关闭/reset archive modal，必须先失效期间新开的 archive review request；其 late success/rejection/finally 均不得提交。
16. DTO 合同 parser 对 exported Go 字段的 missing/empty JSON tag 必须 fail closed，仅精确 `json:"-"` 可忽略；Go mirror 必须在解引用前拒绝 unknown named pointer。
17. 每次正常 route load 必须捕获 mutation generation owner；payload、catch、terminal navigation 与 functional state setter 都必须在提交点复核。same-VPS 旧 reload 不得覆盖后继 mutation/refresh 或更新 reload。
18. cancellation deep-link 的 route-owned preview 必须捕获 preview generation；A1 route preview 不得覆盖已提交的 A2。任一 route payload 在切换/清空 Drawer cancellation state 前必须失效期间新开的 preview owner。
19. Go 与 TS DTO mirrors 必须拒绝所有未精确标记 `json:"-"` 的 anonymous embedded fields，包括未导出 anonymous struct type；bounded parser 不得静默忽略 promoted wire surface。
20. scoped `status` unknown-field 回归必须携带有效 `Idempotency-Key`，精确证明响应为 `error: "invalid json"`，且 repository/idempotency create 零调用。
21. same-VPS query reload/archive 交错必须同时证明 late rejection 与 late success/data/finally 均不能提交。
22. same-VPS route success 回归必须把 A1 延迟放在 detail 之后的二阶段请求，分别证明 payload guard 与 functional state updater 的 commit-time guard；不得只让旧 detail 在最早 generation guard 处退出。
23. archive late-success 的 stale-finally 回归必须在 A2 review 仍 pending 时先 settle stale A1，并证明 A1 `finally` 不能清除 A2 loading 或改变 confirm authority；另用 A2 eligible/confirm 已提交后再 settle 带 blocker 的 A1，独立证明 stale `then` 不能改写 data/confirm。
24. DTO mirrors 把 `BillingPeriodUnit` / `RenewalMode` 视为 string 前，必须读取并验证对应 alias 定义只含非空 string literal；加入 `number`、`undefined`、未知或空 member 时必须 fail closed。
25. TypeScript object parser 必须校验 closing brace 同行后缀，只允许空白/分号；intersection 或其它 suffix 必须失败。Go-source mirror 必须处理或拒绝 trailing inline comment，不能把带注释的 anonymous embedding 当作普通未导出字段静默跳过。
26. 任务 review evidence 必须指向 bounded ownership spec 的当前行段，并保留 scoped/collection idempotency 合同的当前权威位置。
27. Source mirrors 对 Go struct tag key 必须精确匹配，不能把 `notjson` / `notrequired` 当作 `json` / `required`；未知 Go named primitive 必须直接 fail closed，而不是返回一个稍后才可能比较失败的字符串。
28. DTO/alias target marker 必须是注释与字符串之外唯一的 live declaration；block-comment shadow-only 或 shadow+live 均失败。alias union tokenizer 必须识别 quoted literal 内的 `|` 与 escape，不能把合法非空 string literal 错拆成额外 member。
29. 两个 TypeScript object mirrors 必须在 closing brace 后跨越换行与注释 trivia 检查声明延续，拒绝下一有效 token 为 `&` / `|`；批准 alias 的任意多行 `&` / `|` continuation 必须被完整验证或 fail closed。TS Go-source mirror 必须在 raw struct tag 之外处理闭合块注释，并对未闭合/多行块注释 fail closed，不能把带注释的 anonymous embedding 当作普通未导出字段省略。
30. `VPSWriteOwnerStore.finish(owner)` 必须报告是否精确释放。Legacy 中央 finalizer 仅在 exact release 且 owning mutation generation 已 stale 时通知持续挂载的 page shell；page 仅在 mounted/current VPS 匹配时触发一次权威 re-probe，使 remount、Drawer close 或 same-VPS query reload 后的 subscription/service/lifecycle 收敛。旧 A closure 不得 re-probe 当前 B，current-view 正常 settle 不得增加 probe。
31. ordinary cancellation preview 必须在 await 前取得新 generation；A2 pending 时 settle A1 不得提交。非 cancellation route payload 不仅要拒绝 late preview，还必须清除 payload 前已提交的 preview blockers/warnings、error/result 与页面 attention。
32. 真实入口多次 capability probe/lazy remount 的证据用例可设置有依据的 scoped timeout；禁止提高全局 Vitest timeout，并需以完整文件/正式 web gate 重跑证明稳定。

## Out of scope

- 重写基于源码文本的 TypeScript DTO parser，或引入 AST/OpenAPI/codegen 管线；本轮只做 bounded exact-member、alias-definition、object-suffix 与 inline-comment fail-closed 检查及对应负例。
- 改变 subscription collection create、receipt 生命周期、数据库 schema 或已有前端 payload 形状。
- 中止已经发往服务端的写请求；本任务只约束客户端状态副作用归属。
- 修复与本任务无关的 attachment PNG golden digest 基线差异。

## Acceptance Criteria

- [x] 每个 required scoped subscription 字段的 missing 与 explicit null 都有真实 HTTP 回归，返回 `400` 且 repository 调用数为零。
- [x] `started_at` / `renew_at` 的 explicit null 成功；`price: 0`、两个 false boolean、`payment_method: ""`、`note: ""` 被精确传入 domain input。
- [x] manifest、Go request 与 TypeScript DTO 的 name/type/required/nullable 合同测试继续通过，并能检测 requiredness 漂移。
- [x] 路由 A 上 pending 的 service 与 subscription 写入不阻塞 B；A 的完成不会清除或关闭 B 的状态；返回 A 时 pending 仍由 A 自己的 owner 决定。
- [x] detail/services/domains 各有 A→B→A 回归，旧 generation 的响应不能覆盖返回 A 后的新状态。
- [x] archive review 覆盖旧 success、旧 rejection、旧 finally 与同 VPS close/reopen，均不能污染新 owner。
- [x] query-driven early effect 后启动 deferred write，再卸载组件；迟到响应不得更新或导航，transport owner 仍由自己的 finally 释放。
- [x] cancellation preview 在 Drawer close 后 settle 不得提交；pending 或已加载 preview 经 workbench 关闭按钮 / Modal X 关闭后，reopen 都必须发起新 GET 并只展示新结果。
- [x] TS/Go parser 负例证明 `number | string` 与 `string | undefined` 不再假绿；未知 Go DTO 类型及 manifest 缺失/null 语义键也必须 fail closed。
- [x] 真实 `VPSDetailPage` + 真实 Legacy deferred 回归覆盖 A pending → B → A-before-settle；A POST/idempotency key count 仍为 1，B 可独立提交，owner 只由 exact token settle 释放。
- [x] 同 VPS query reload pending 期间打开 archive review，payload reset 后迟到 review data/error/finally 不得写回页面。
- [x] 两个 DTO mirrors 直接拒绝 missing/empty/empty-name JSON tags；只忽略 dash，并拒绝 unknown named date pointer。
- [x] same-VPS route A1 pending 后由 mutation/refresh 或 reload A2 提交新状态；A1 的 payload、catch、functional state commit 与 navigation 均被 generation owner 拒绝。
- [x] cancellation deep-link route preview A1/A2 ABA 不覆盖新 digest；reload payload reset 后，期间打开的普通 preview late settle 不改变 cancellation attention/state。
- [x] anonymous embedded field 在 Go/TS mirrors 均 fail closed，只有精确 `json:"-"` 可忽略。
- [x] scoped `status` 回归携带有效 idempotency key，返回 `400` + `error: "invalid json"`，且 repository/idempotency create 调用数为零。
- [x] same-VPS query reload/archive 回归分别覆盖 late rejection 与 late success/data，并证明旧 finally 不夺取 loading authority。
- [x] same-VPS stale success 在二阶段请求后失效，并有受控 updater interleaving 证明 outer payload guard 与 functional setter guard 各自可阻断旧 commit。
- [x] archive late-success 有两个独立 mutation 证明：A2 pending 时先 settle A1 保持 loading/confirm authority；A2 eligible/confirm 已提交后再 settle blocker A1 保持 data/confirm authority。
- [x] TS/Go mirrors 验证 `BillingPeriodUnit` / `RenewalMode` alias 定义仅为 string literals；alias 加入 `number` / `undefined` 的 synthetic negative 必须失败。
- [x] TS/Go object parsers 拒绝 `} & { ... }` suffix；TS Go-source mirror 对带 trailing comment 的 anonymous embedding 不再静默忽略。
- [x] `research/review-evidence.md` 的 ownership/idempotency 引用与 bounded/current spec 对齐。
- [x] 两个 mirrors 拒绝 commented DTO/alias shadow declarations；TS Go-source mirror 精确区分 `json`/`required` 与 near-miss tag keys，Go unknown named primitive 直接失败；escaped/embedded-pipe string aliases 在两侧保持对称。
- [x] 两个 object mirrors 拒绝换行或注释 trivia 后的 `&` / `|` continuation；两个 alias mirrors 对 `BillingPeriodUnit` / `RenewalMode` 的多行 widening fail closed；TS Go-source mirror 对 inline block-comment anonymous embedding 显式失败并保留 raw tag 内 comment marker。
- [x] A pending → B → A-before-settle、当前页 pending subscription close/reopen、pending subscription + same-VPS query reload 三条 stale-view 路径在 POST settle 后自动触发一次当前 A re-probe并展示服务端 subscription/service/lifecycle；POST 与 idempotency key 均唯一。A settle 时当前 route 为 B，以及 current-view 正常 subscription settle，均不增加 page probe。
- [x] route preview A1/A2 都使用 deferred：A2 pending → A1 settle 仍保持 loading且旧 preview/attention 不提交 → A2 settle只显示 A2；另以反向顺序证明 route payload 清除已提交的 preview/error/result/attention。
- [x] 证据较重的真实入口用例只使用 scoped timeout；全局 timeout 不变，focused/full relevant file 与正式 web gate 无随机超时。
- [x] 父任务及两个 web 子任务的 implement/check context 注入使用 bounded 合同；`task.py validate` 不再报告这些任务引用的 spec 超限。
- [x] focused Go/Vitest、正式 Chromium、`make verify-web` 与任务相关的质量门通过；全量 Go 的既有 attachment golden 失败被单独记录，不冒充本任务回归。
- [x] 三个子任务均完成、父任务集成检查通过后，才可声称本次 P2 hardening 完成。

## Delivery constraints

- 所有实现都在 `codex/v0776-vps-hardening` 与独立 worktree 中完成，不直接修改 `main`。
- 子任务 2 与 3 共同修改 `LegacyVPSDetail.tsx` 和对应测试，必须顺序执行并逐个复审。
- 遵循 TDD：先得到针对目标合同的 RED，再写最小实现并跑 focused GREEN。
- 用户已批准在未归档的原任务上继续本轮补救实现；提交、推送或 PR 仍需后续明确批准。
