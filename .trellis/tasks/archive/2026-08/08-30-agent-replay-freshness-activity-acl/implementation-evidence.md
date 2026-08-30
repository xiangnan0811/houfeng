# Agent Replay Freshness and Activity ACL Implementation Evidence

> 日期：2026-08-30
> 分支：`codex/agent-replay-freshness-activity-acl`
> 状态：本地实现、任务门禁、各代码切片审查与最终 whole-task/spec/task evidence 复审均完成，结论 READY。未 stage、commit、push、PR、merge、release、deploy，也未访问生产数据库。

## 1. Agent durable replay scheduler

- RED：7 个行为测试在旧实现上实际失败，证明旧 runtime 会单轮清空 backlog、retryable backlog 阻断 current、plan 顺序错误，以及以 carrier batch ID 识别 current 会发生碰撞误判。
- GREEN：每轮以 `Enqueue` 返回的本地 entry ID 定位 current；backlog lane FIFO 且最多尝试 2 条，current 最后尝试。retryable backlog 只停止 backlog lane；成功响应先 durable delete 再计 ack/apply plan。
- Durability：command result 与 IP-quality payload 在 enqueue 成功前不从内存确认；current 失败后保留并在下一轮标记为 backfill。
- 验证：`go test ./agent/runtime ./agent/syncqueue -count=1`、focused `-count=10`、两包 `-race` 均 PASS。
- 独立 spec/quality review：无剩余 Critical、Important 或 Minor finding。

## 2. Agent replay observability

- RED：旧实现没有 catching-up/caught-up/retrying episode 状态，也没有一分钟限频；隐私测试证明 retry 行缺失。
- GREEN：固定 message `sync queue replay progress`；健康进度只统计 durable delete 后的 backlog ack，持续追赶每 60 秒最多一条，归零只写一次 caught-up，普通 live tick 静默，retryable round 使用 Error/retrying。
- Invalid 400 discard 可使 remaining 归零，但不计为 ack；Delete 失败不产生虚假 progress。
- 隐私：日志和新增测试失败输出不包含 request/queue identifiers、credentials、raw payload、remote message、private URL 或 raw cause。
- 验证：6 个 exact log tests 与 discard/delete 回归 focused `-count=10`、包测试和 race 均 PASS；独立审查无 finding。

## 3. Center disposition and replay-safe latest reads

- `syncing.ResultDisposition` 明确区分 `recorded`、`exact_duplicate`、`suppressed`；只有 exact duplicate 跳过 post-sync，suppressed 仍允许行政恢复，既有 AcceptedAt/plan/pending action wire 行为保持。
- host/probe/heartbeat agent version/IP-quality/evidence/asset decision/current incident series 使用 canonical ordering：event time、live-over-backfill、received time、stable row key。
- Go normalizer 使用相同 provenance/received stable sort；derived resource sample 保留 `ReceivedAt`。
- RED：unit behavior/SQL tests 在旧 ordering 上失败；strict PostgreSQL 16 actual-row test 在旧 asset latest view 上选择 backfill，且临时移除 `received_at` 后选择高 stable-key 旧 receipt。
- GREEN：strict PG16 两种 arrival order 均 RUN/PASS，独立证明 backfill、received-at 和 stable-key 三层 tie-break，覆盖 host、probe、heartbeat、IP summary/latest report 和 asset decision。
- Production E2E：current convergence 后由 direct runtime role 运行真实 `syncing.Service → PostgresSyncRepository.ApplyBatch → incidents.Service`，覆盖两种 arrival order × pending/in-use 四场景。raw multiset、duplicate 零追加、`last_*`/lifecycle、latest consumers、active/summary/events/DB+external notifications 与 row version 均收敛；临时让 exact duplicate 误入 post-sync 后四场景真实 RED，mutation 已恢复。
- 验证：store/incidents full、focused `-count=10`、race、vet、gofmt/diff-check 均 PASS；独立 spec/quality review READY。

## 4. Incident projection opaque row-version CAS

- Writer RED：旧合同缺少 expected row version 与 typed conflict。GREEN 后，transaction 第一条 DB operation 是静态 MI/target `SELECT xmin::text ... FOR UPDATE`；空/缺失/unsupported fail closed，mismatch 在任何 DML/commit 前退出。
- Service RED/GREEN：每个 attempt token-first 完整重读；首次 typed conflict 只重评一次，retry time 不早于原 trigger；第二次 conflict 脱敏且按对象类型一分钟限频后安全 yield。inactive empty object 零 mutation；枚举删除 safe-yield；普通错误不吞不重试；通知只在 mutation commit 后。
- Writer-guard deletion RED/GREEN：新增稳定 `ErrIncidentProjectionObjectNotFound`；store 只把静态 xmin guard 的 `pgx.ErrNoRows` 映射到该分类并保留 cause，service 安全 yield。MI/target、sweep continue、零 DML/commit/notification、普通 DB 与未分类 `pgx.ErrNoRows` 负控均覆盖；direct-runtime PG16 deletion-window 实际 PASS。
- Committed boundary：连续 projection conflict 不反转已提交 sync response、plan 或 pending action。
- strict PG16 direct-runtime stale-writer evidence：
  - MI/target 重叠 writer：B 在 guard 后、DML 前由 test-only barrier 暂停，catalog lock evidence 证明 A 等待 B 的对象行；释放后 B 成功、A typed conflict。
  - 临时只移除 `FOR UPDATE` 的 mutation 在两个对象上真实 RED；恢复后 GREEN。
  - direct-runtime 无关对象字段 UPDATE 推进 row version；旧 mutation 只能安全 conflict，note/active/summary/events/notifications 保持。
  - 临时绕过 equality 的 mutation 在两个对象上真实 RED；恢复后 GREEN。
  - 实际查询 persisted event payload，证明旧/新 token 计数为零；在隔离测试库注入 token 后断言真实 RED，注入代码已删除。
- 最终 targeted strict command 使用 `^TestPostgresIntegrationIncidentProjectionCAS` 与 `-count=2`；2 轮 × 4 top-level × MI/target 共 16 个 subtest 全部 RUN/PASS。store full/race/vet、gofmt、diff-check 与 migration no-diff 均 PASS。
- 生产代码、migration 与 ACL 中不存在任何临时 mutation；opaque token 不进入日志、API、event payload 或任务证据。

## 5. Activity Projection current runtime ACL

- RED：旧 production hash classification 使用 projection fact `SELECT ... FOR UPDATE`，direct runtime 在 current ACL 下稳定返回 SQLSTATE `42501`；owner 断言 projection/subjects 与 published/allocated watermarks 均未产生 partial state。
- GREEN：只从 immutable projection hash SELECT 删除 `FOR UPDATE`。active head `FOR UPDATE` 仍是 candidate classification、sequence allocation 和 watermark 的串行化根；strict insert/canonical mismatch 保持最后防线。
- direct-runtime PostgreSQL 16 acceptance 覆盖 first insert、exact retry、mixed hash mismatch 整批回滚、两个 publisher 和连续 sequence/hash/watermark。
- Catalog：projection table SELECT/INSERT/DELETE 为 true、UPDATE 为 false、column ACL 为 0；heads/revision intervals 保留必要 UPDATE。
- targeted strict PG `-count=2`、既有 deterministic head contention `-count=2`、store full/race/vet、migrate ACL、gofmt/diff-check 与 migration/schema no-diff 均 PASS。
- 独立 spec/quality review：无 finding。

## 6. Schema, ACL and delivery boundary

- 未新增 migration 或 current APP ACL fragment。previous exact-current + future migration 的 PostgreSQL regression 继续要求 rebuild-required 且 durable state 不变。
- 所有 runtime PostgreSQL acceptance 都通过 current convergence/admission 后的 direct runtime role执行生产 DML；owner 仅用于 fixture seed、catalog/readback/assert。
- 本地 PostgreSQL fixture 不是生产数据库；截至本地实现和独立审查完成时，任务未执行远端写入或部署操作。
- Agent scheduler 与 Center monotonicity/CAS 必须在同一 release，Center 先部署或原子同步部署。用户已授权按项目规范完成发布与约定测试环境部署；私有交接上下文中的精确目标不写入仓库，部署前必须重新验证并建立冷恢复点。
- 精确授权目标已从私有来源交接中恢复并完成来源核验；敏感 locator 不写入仓库。部署门禁仍须按该精确目标执行只读 preflight、建立冷恢复点并确认当前版本，任一步失败即 fail closed，不得猜测目标或扩大清理范围。
- 主 checkout 中存在同名的较早、未跟踪任务副本。它不是本分支来源，不得混入提交；仅在本任务归档、推送、合并且可从远端恢复后，才可把该精确副本作为被取代状态做可恢复清理，且不得顺带清理主 checkout 的其他文件或分支。

## 7. Known unrelated verification noise

- 一次过宽的 Activity selector误触既有百万行性能 fixture并遇到测试 PostgreSQL 临时空间不足；数据库随后恢复且无临时库残留，该命令未计门禁。
- Phase 2.2 曾按旧计划尝试对 `store`/`migrate` 做无 selector 的 strict PostgreSQL 整包运行；它同时命中了需额外 MinIO 环境的可选测试（SKIP 会被 strict runner 判失败）和既有百万行性能 fixture，后者填满 runner 的 512 MiB 临时空间并使后续用例在 PostgreSQL recovery 中级联失败。该命令不是 task-scoped acceptance，未记为通过；计划已改为第 8 节实际 RUN/PASS 的精确 strict selector，整包 Go 覆盖仍由 fresh `make verify-go` 提供。
- 中间审查与最终 root 复验都观察到未改动 attachment PNG golden 的环境相关 digest 差异；root 定向 `-count=2` 稳定得到同一 actual digest，独立 reviewer 进程则未复现，且 `git diff --exit-code -- internal/center/attachments` 为空。更宽 Center race 另有既有 retention fake race 噪声。任务相关 full/race/vet、严格 PG16 acceptance 均独立 PASS。

## 8. Final unified local gates

- 最终共享工作树在独立审查修复全部落地后，由主会话 fresh 复跑 `go test ./agent/runtime ./agent/syncqueue ./internal/center/syncing ./internal/center/incidents ./internal/center/store ./internal/center/store/migrate -count=1`：PASS。
- 同一最终树的五包 `go test -race -count=1`：PASS；六包 `go vet`：PASS。
- `make verify-go`：早期 root 复验曾因未改动的 `internal/center/attachments` PNG golden digest 差异整体 exit 2，未把该失败记为通过或改写 golden；Phase 2.2 独立审查和审查修复后的主会话均使用项目规定的 `GOTOOLCHAIN=go1.26.2` fresh 重跑 exit 0，后者确认 `agent/... cmd/... db/... internal/...` 全量 PASS。干净重跑是当前门禁证据，同时保留早期失败的真实时间线。
- 最终共享工作树 strict PostgreSQL 16 targeted `-count=2` 全部实际 RUN/PASS（主会话 19.442s）：
  - production SyncBatch live/backfill/duplicate 两种 arrival order × pending/in-use 四场景：PASS；
  - replay-safe latest actual-row 两种 arrival order：PASS；
  - incident projection CAS 四组 × MI/target，共 16 subtests：PASS；
  - Activity runtime ACL + deterministic head contention：PASS；
  - EvidenceSources direct-runtime fresh-token fixture：PASS；
  - APP current convergence，包括 prior-baseline rebuild-required/no-mutation：PASS。
- `git diff --check`、task JSON/JSONL parse、migration/current ACL no-diff：PASS。
- 生产代码、spec 与 task evidence 的 high-risk credential/DSN pattern scan：零命中；两个测试文件中的 credential-like URL 均是明确的 `.example.test` / `.example.com` privacy sentinel，不是外部秘密。
