# VPS 心跳异常通知策略 Implementation Plan

> **For agentic workers:** User approval is required before `task.py start` or any product-code edit. After approval, use `trellis-before-dev`, `superpowers:test-driven-development` for every behavior change, `trellis-check` for independent review, and `superpowers:verification-before-completion` before any success claim. Do not commit, push, open a PR, merge, release, deploy, or clean worktrees without the corresponding explicit authorization.

**Goal:** 让全局失联阈值在所有心跳判定入口精确生效，默认 12 周期后首次异常，以 3 次连续实时心跳稳定恢复，并让每条心跳通知直接标识 VPS/监控实例。

**Architecture:** 用一个显式 `HeartbeatIncidentPolicy` 统一 settings、periodic sweep、post-sync evaluator 与 recovery evidence；PostgreSQL 提供有界的 live heartbeat receipt 查询；service 在提交成功后的通知边界补充对象身份；`0063` 同步默认值、旧默认数据和查询索引。

**Tech Stack:** Go/pgx v5、PostgreSQL 16、React/TypeScript/Vitest、Node 22、Trellis。

## Risk boundaries

- 不修改历史 migrations、Agent sync DTO、心跳采集/发送频率或通用通知 channel API。
- 不把一次设置读取失败解释为默认策略并继续告警；心跳判定必须 fail closed。
- 不用回填、重复 batch、Agent observed time 或配置变化本身作为恢复证据。
- 公开 `AfterSuccessfulSync` 在 sync 事实已经提交后对评估错误只记录日志并确认成功；返回错误会让 Agent 重试命中 `exact_duplicate` 后跳过 post-sync，造成永久漏评估。
- 非空且全回填的 heartbeat carrier 只抑制 heartbeat transition；同批其他监控维度继续按各自 provenance 评估，空 carrier 与 mixed/live carrier 保持既有评估行为。
- 不改变行政恢复静默、CAS guard、post-commit dispatch、多通道记录和通知开关语义。
- migration 必须带 explicit empty current APP ACL fragment，并由真实 PostgreSQL 证明数据转换与索引，而不是只扫 SQL 字符串。
- accepted Agent sync 的 heartbeat 数量合同固定为 `1..syncing.MaxBatchItems`（当前 256），且同一 HTTP 请求的 heartbeat 必须共享一个 `sync_batch_id`；handler 与恢复查询共用该常量。恢复候选集必须在 WindowAgg 前限制为 `3 * MaxBatchItems`，绕过入口上限/分组约束时只允许 fail-closed 地保持 active。

## TDD execution checklist

- [x] **1. 获批后启动任务并建立基线**
  - 运行：

    ```bash
    python3 ./.trellis/scripts/task.py start .trellis/tasks/08-31-vps-heartbeat-notification-policy
    git status --short --branch
    go test ./internal/center/incidents ./internal/center/settings ./internal/center/store/migrate -count=1
    npm --prefix web run test -- --run src/pages/SettingsPage.test.tsx
    ```

  - 确认分支为 `codex/heartbeat-notification-policy`、hooks 已启用、dirty state 只有本任务内容。

- [x] **2. RED — 冻结显式策略与 N/2N/4N 边界**
  - 在 `internal/center/incidents/evaluator_test.go` 先写 table-driven tests：
    - `N-1` 正常、`N` 关注、`2N` 告警、`4N` 严重；
    - `N=20` 对应 `20/40/80`；
    - 从 normal 跨到 critical 只创建一次实际等级事件；
    - 旧 `N-1/N/N+2` 语义明确 RED。
  - 将 evaluator 调用目标改为必须显式接收 policy 的新签名，使现有漏传调用点先编译失败；不得提供 variadic、默认参数 helper 或兼容 overload 掩盖 RED。
  - focused command：

    ```bash
    go test ./internal/center/incidents -run '^TestEvaluateMonitoringInstanceHeartbeatMissing.*(Boundary|CustomThreshold|Jump)' -count=1
    ```

- [x] **3. RED — 冻结稳定恢复证据**
  - 在 evaluator tests 增加事件开始后的 1/2/3 个 distinct live batch，以及 evaluator 可表达的负例：duplicate `sync_batch_id`、pre-incident、相邻 gap 超过 `2 * interval`、invalid policy。`LiveHeartbeatReceipt` 按类型即代表已过滤的 live receipt，因此 backfill 排除由 PostgreSQL reader test 和 post-sync carrier provenance test 负责，不在 evaluator 中伪造字段。
  - 断言第 1/2 个为 noop 且 previous active incident 被保留，第 3 个只恢复一次。
  - 加“只提高阈值、无 heartbeat receipts”用例，确保不产生 recovered event/notification。

- [x] **4. RED — 两条 service 入口必须读取同一 persisted policy**
  - 在 `internal/center/incidents/service_test.go` 增加用户复现：persisted `N=20` 时，直接调用 periodic 与公开 `AfterSuccessfulSync` 均在 19 周期无 incident、20 周期开始。
  - 增加 settings/recovery receipt error 负例：内部评估返回错误；公开 `AfterSuccessfulSync` 记录稳定日志并返回 `nil`，不得退回 3/12 后创建心跳 mutation 或通知。
  - 增加非空全回填、mixed/live heartbeat carrier 与 CAS retry 用例：全回填不改变 heartbeat incident 但不阻断其他维度；第二次 attempt 使用新读到的 settings、record、active incident 与 receipts，并保留 trigger provenance。
  - 预期旧代码至少在 `AfterSuccessfulSync` 的 19 周期 case RED。

- [x] **5. GREEN — 实现显式 heartbeat policy 和统一入口**
  - 在 `internal/center/incidents` 定义/校验 policy 与 live receipt 类型。
  - 移除 evaluator 的 variadic threshold 和 `3` fallback；实现 `N/2N/4N`。
  - service 使用一个 policy resolver，并把同一 policy 传给 periodic/post-sync evaluator；读取错误返回内部评估边界并保留 active projection，公开 post-sync hook 在事实已提交后 best-effort log + ack，由 periodic 后续收敛。
  - settings/domain validation 拒绝会使 duration、`2 * interval` 或 `4 * N` 溢出的值；领域 policy 还必须固定 recovery successes=3、max gap=`2 * interval`；evaluator/service 共用安全、饱和的 missed-interval helper。
  - 同一次 attempt 保留其 notification toggles snapshot；CAS retry 完整重读。
  - 重跑步骤 2–4 focused tests，确认 GREEN。

- [x] **6. RED/GREEN — PostgreSQL 恢复证据读取**
  - 扩展 `SnapshotReader` 与 `fakeSnapshotReader`，新增事件开始后的最近 live heartbeat receipt 读取。
  - `PostgresSnapshotReader` 查询必须先用 `recent_live AS MATERIALIZED` 按 monitoring instance、`received_at > started_at`、`is_backfilled=false` 和 `(received_at desc,id desc)` 建立候选集，在任何 WindowAgg/dedupe 前 `LIMIT $3`；`$3 = 3 * syncing.MaxBatchItems = 768`。随后按 batch 去重、最终 `LIMIT 3`，只 scan `sync_batch_id/received_at`。
  - 单元测试冻结 SQL filter/order、inner limit 位于 window 前、arg3、final limit 与 scan/rows error wrapping；service tests 冻结 read failure fail-closed。
  - 增加真实 PostgreSQL test，以远超 768 行的旧重复历史和 received_at 相同的最新三个 distinct batch 证明功能；`ANALYZE` 大历史后捕获生产 reader 的 SQL/参数运行 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`，递归拒绝 heartbeat relation 的 Seq/Bitmap path，断言 exact 0063 ordered index、scan rows×loops/filter removals/shared blocks 固定有界，并以第二量级旧历史证明读取不线性增长；WindowAgg/Sort/input actual rows 仍不得超过候选上界。

- [x] **7. RED/GREEN — `0063` 默认值、数据转换、索引与 current APP ACL**
  - 新建 `db/migrations/0063_tune_heartbeat_incident_policy.sql`，只追加：列默认 12、全局 JSON 旧值 3→12、`INCLUDE(sync_batch_id)` 的 partial covering index。
  - `internal/center/settings/types.go` 默认改为 12；更新默认相关 Go/Web fixtures，明确自定义/迁移用的 3 不机械替换。
  - 在 `app_acl_current_contract.go` 注册 `0063` explicit empty fragment，privilege compiler 返回 nil；更新 migration inventory、fragment count/order 和 exact-current tests。
  - 新建 migration test，并通过 strict PostgreSQL 实际证明：
    - global 3→12；
    - global 20 不变；
    - override rule 中显式 3 不变；
    - fresh default 为 12；
    - partial index predicate/columns 存在；
    - runtime existing SELECT privilege 足以执行 recovery query，APP ACL tuple 不扩张。
  - 不修改 `0006`、`0026` 或其他已发布 migration。

- [x] **8. RED/GREEN — 通知正文补全主体且保持开关/通道合同**
  - 在 service tests 先断言 started/escalated/recovered outbound summary 和 `NotificationRecordWrite.Summary` 都包含 `DisplayName` 与 ID；换行/控制/bidi 被移除或替换、空白折叠、超长多字节名称按 rune 安全截断至 80 字符，净化后空名回退“未命名监控实例”。
  - 在 service 通知边界只格式化 heartbeat incident；领域 event/source summary 不变。
  - 回归 started/escalated/recovered toggles、Telegram-only、Feishu-only、mixed channel partial failure、dispatcher nil suppression、行政恢复静默和 mutation failure 零通知。

- [x] **9. RED/GREEN — Settings 页面解释新语义**
  - 更新 `IncidentDefaultsSection.tsx` 的字段说明：首次边界 N、默认 12≈60s、2N/4N 升级、3 次实时心跳恢复。
  - `SettingsPage.test.tsx` 覆盖默认 12 文案和自定义 20 原样 PUT；更新 `web/e2e/fixtures/profiles.ts` 代表默认配置的 fixture。
  - 不新增设置字段、Context、依赖、直接 fetch 或 API type shape。
  - focused commands：

    ```bash
    npm --prefix web run test -- --run src/pages/SettingsPage.test.tsx
    npm --prefix web run lint
    npm --prefix web run build
    ```

- [x] **10. 同步 executable specs**
  - 在 `.trellis/spec/backend/database-guidelines.md` 的 incident/heartbeat 邻近位置记录：显式 policy、N/2N/4N、live receipt recovery、partial index、fail-closed、0063 empty fragment 和 PostgreSQL evidence。
  - 在 `.trellis/spec/web/state-and-data.md` 的 Incident threshold settings contract 记录默认 12、字段语义和 UI copy。
  - 如质量门或测试层级未改变，不重复扩写 quality specs；仅修正文档中被本任务直接推翻的旧事实。

- [x] **11. 全量验证与独立检查**
  - 依次运行当次 fresh gates：

    ```bash
    GOTOOLCHAIN=go1.26.2 go test ./internal/center/incidents ./internal/center/settings ./internal/center/store ./internal/center/store/migrate ./internal/center/http/handlers -count=1
    GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- \
      go test -v ./internal/center/store/migrate \
      -run '^TestPostgresIntegrationHeartbeat(RecoveryReceipts|IncidentPolicyMigration)$' -count=1
    GOTOOLCHAIN=go1.26.2 make verify-go
    PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web
    PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test:e2e
    git diff --check
    git status --short --branch
    ```

  - strict PostgreSQL 输出必须有实际 RUN/PASS 且无 SKIP；如 test package/selector 在实施中采用不同准确名称，同步更新本计划和命令。
  - 使用 `trellis-check` 对照 PRD/design/specs，重点检查阈值两入口一致、恢复 fail-closed、迁移数据保留、empty ACL fragment、通知主体及 toggle/行政恢复无回归。
  - 在完成声称前使用 `superpowers:verification-before-completion` 复核所有当次输出。
  - 2026-08-31 最终状态：仓库 `go.mod`/CI 的精确 Go 1.26.2 下 `make verify-go` 全绿；strict PostgreSQL 两项真实 RUN/PASS 且无 SKIP；Node 22 `make verify-web`、136 项 E2E、视觉证据测试与三视口生产预览 browser sanity 全绿；四次独立审查最后一轮为零发现。

- [ ] **12. 受保护交付与生产验收（仅在明确授权后）**
  - commit/push feature branch、创建 PR；required CI 全绿后合并，再监控 main CI、Release Please、release 和多架构镜像发布。
  - 升级环境后 read back 全局阈值；用可控测试实例分别验证 19/20 边界、3 次 live recovery 和消息主体，不制造真实用户噪声。
  - 报告数据库 migration/index、事件、通知记录、外发消息与 Agent 心跳的联合证据；仅页面显示 20 或进程 active 不算验收完成。
  - 2026-08-31 交付部分已完成：feature PR `#484` head `4b093c78` 的 7 项 CI 全绿，合并为 `ecfbb808`；exact-main CI run `33357793177` 全绿。Release PR `#485` 在删除重复 CHANGELOG 项后以 head `b0035f2b` 的 7 项 CI 全绿，合并为 `e427f41b`。
  - Release `v0.79.5` 精确指向 `e427f41b`；release-main CI run `33358208141` 与 `publish-images` run `33358215951` 均成功。公开 Agent checksum manifest minisign 验签、两架构 SHA256/static/vcs metadata、部署资产 digests 及 Docker Registry OCI index 均已独立复核；`v0.79.5`、`0.79.5`、`latest` 同指 `sha256:a3c75cab7538d6b601a48d7d6a26db1ea1c4658ba14edc528663cff6c9e8ab6e`，包含 linux/arm64 `sha256:24b081a5af62c204474f0608ceb27a7fde59bd480a31cc88c2553ad8b166b911` 与 linux/amd64 `sha256:937f720cd3a12e26e5f2465981973c5bc30bbc8eb8e8cd2959b804980bf68d99`。
  - 真实环境验收未冒充完成：从受保护 main dispatch 的 staging run `33358504336` 要求 `v0.79.5`，但 `/api/healthz` 实际仍为 `v0.59.0`，因此在登录或任何 Settings mutation 前 fail closed。脱敏 artifact `frontend-staging-audit-33358504336` digest 为 `sha256:f5f7fcdbbdeb6a506f2f8da5691ad4f52ae36e2f5ededad177cf85a9f40a5118`；仓库无 staging 部署 workflow/主机授权，升级与 19/20、三次恢复、通知主体联合验收已拆到未启动任务 `08-31-staging-heartbeat-policy-acceptance`。

## Planned changed files

- `internal/center/incidents/{types.go,evaluator.go,service.go}` 及同包 tests。
- `internal/center/syncing/{service.go,service_test.go}` 与 Agent sync handler 的共享 batch 上限引用。
- `internal/center/settings/types.go` 及 settings/store/handler tests 中的默认 fixtures。
- `db/migrations/0063_tune_heartbeat_incident_policy.sql`。
- `internal/center/store/migrate/app_acl_current_contract.go`、migration/current APP ACL tests。
- `web/src/pages/settings/IncidentDefaultsSection.tsx`、`web/src/pages/SettingsPage.test.tsx`、default browser fixtures。
- `scripts/visual_evidence.py` 与 `scripts/test_visual_evidence.py` 的 active settings mock/default contract。
- `.trellis/spec/backend/database-guidelines.md`、`.trellis/spec/web/state-and-data.md`。

## Delivery authorization and remaining evidence

用户已在 2026-08-31 明确授权按项目规范继续 commit、PR、CI、merge、release 与最终现场清理。步骤 12 在实际完成受保护交付、发布证据和 Trellis 归档前保持未勾选，不以授权本身冒充交付证据。

## TDD 与验证证据（2026-08-31）

所有 RED 均在对应产品实现前实际运行；以下失败不是事后反改断言制造。

### RED

- 显式策略与 N/2N/4N：`go test ./internal/center/incidents -run '^TestEvaluateMonitoringInstanceHeartbeatMissing.*(Boundary|CustomThreshold|Jump)' -count=1`，编译失败为 `undefined: HeartbeatIncidentPolicy`，且旧 evaluator 签名拒绝 policy/receipts 参数。
- 稳定恢复：`go test ./internal/center/incidents -run '^TestEvaluateMonitoringInstanceHeartbeatMissingRecovery' -count=1`，同样在旧签名/缺失策略类型处编译失败；evaluator 用例先固定 1/2/3、duplicate、pre-incident、gap、invalid policy 与只提高阈值负例。backfill 不属于 `LiveHeartbeatReceipt` 的可表达状态，由 PostgreSQL live reader 与下面的 post-sync provenance 用例证明排除。
- 首轮 service policy/recovery：`go test ./internal/center/incidents -run '^TestServiceHeartbeat(Policy|Recovery)' -count=1`，当时名为 post-sync 的 N=20 fixture 实际调用私有 full evaluation，不是公开 `AfterSuccessfulSync`；旧实现仍在 missed=19 错误创建严重 incident，且 recovery query error 路径未实现。第二轮已用真实公开入口补齐，见下。
- PostgreSQL reader：`go test ./internal/center/incidents -run '^TestPostgresSnapshotReaderListRecentLiveHeartbeatReceipts' -count=1`，编译失败为 `PostgresSnapshotReader.ListRecentLiveHeartbeatReceipts undefined`。
- 默认值/inventory：`go test ./internal/center/settings ./internal/center/store/migrate -run '^(TestSettingsDefaultProvidesDeterministicSingletonShape|TestHeartbeatIncidentPolicyMigration|TestFrozenR1RootSourcesRemainExactPrefix|TestRootMigrationsExcludeObsoleteAppExtensionDraft)$' -count=1`，旧默认实际为 3（期望 12），embedded source 实际 63（期望 64）。
- `0063`/empty fragment：`go test ./internal/center/store/migrate -run '^TestHeartbeatIncidentPolicyMigration' -count=1`，失败为 migration 文件不存在、current fragment 实际 11（期望 12）。
- 通知主体：`go test ./internal/center/incidents -run '^TestServiceHeartbeatNotification' -count=1`，started/escalated/recovered dispatcher 与 record 均仍是无名称/ID 的领域摘要。
- Settings 文案与 custom 20：`PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web test -- --run src/pages/SettingsPage.test.tsx`，21 项中新增用例 1 FAIL，原因是找不到默认 12≈60 秒、2N/4N 与三次实时心跳恢复说明；custom 20 输入已经按 persisted 值载入。
- 全回填 post-sync provenance：`go test ./internal/center/incidents -run 'TestServiceAfterSuccessfulSync(SuppressesHeartbeatTransitionsForExplicitAllBackfill|EvaluatesHeartbeatForMixedOrLiveCarrier)$' -count=1`，旧 full attempt 在全回填 carrier 上错误 start heartbeat、把 notice 升级、用 receipts 恢复 active，并与 disk incident 一并创建 heartbeat incident。
- overflow bounds：`go test ./internal/center/settings ./internal/center/incidents -run 'Test(SettingsValidateRejectsOverflowingIncidentTiming|ValidHeartbeatIncidentPolicyRejectsOverflowingDerivedBounds)$' -count=1`，旧 settings/global override 与 domain policy 均接受会溢出的合法正整数；`go test ./internal/center/incidents -run '^TestHeartbeatMissedIntervalsSaturatesAndHandlesClockSkew$' -count=1` 先因安全 helper 不存在而编译失败；fallback/timing RED 还证明 persisted/direct duration 会绕回非安全值。
- 默认 fixture：fresh-install handler focused test 收到 `StaleThresholdIntervals=3, want 12`；Node 22 api focused 的 GET response 与 PUT request fixture 均收到 3、期望 12。
- 有界 recovery candidate：`go test ./internal/center/incidents ./internal/center/store/migrate -run 'Test(PostgresSnapshotReaderListRecentLiveHeartbeatReceiptsSQLContract|HeartbeatIncidentPolicyMigrationSourceContract)$' -count=1`，旧查询缺少 `recent_live AS MATERIALIZED`/窗口前 `LIMIT $3`，且 0063 索引缺少 `INCLUDE(sync_batch_id)`。
- 共享 ingress 上限：`go test ./internal/center/syncing -run '^TestMaxBatchItemsMatchesAgentIngressContract$' -count=1`，旧实现编译失败为 `undefined: MaxBatchItems`。
- 通知名称净化：`go test ./internal/center/incidents -run '^TestHeartbeatNotificationDeliverySanitizesAndBoundsMonitoringInstanceDisplayName$' -count=1`，旧实现原样保留 CRLF/control/bidi、未截断 100 个多字节字符，unsafe-only 名称也未 fallback。
- PostgreSQL index/IO 计划证据：增强断言后执行 `scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store/migrate -run '^TestPostgresIntegrationHeartbeatRecoveryReceipts$' -count=1`，旧测试形状真实走 `Bitmap Heap Scan`/`Bitmap Index Scan`，heartbeat relation 扫描 3075 行（期望不超过 768），且旧 EXPLAIN 缺 BUFFERS、未使用 exact ordered 0063 index。
- visual-evidence settings mock：`python3 scripts/test_visual_evidence.py VisualEvidenceMockAPITest.test_observability_profile_serves_auth_dashboard_nodes_targets`，`/api/settings` 返回 `stale_threshold_intervals=3`，新增断言期望 12 后 1 FAIL。
- 独立审查 policy contract：`go test ./internal/center/incidents -run '^TestValidHeartbeatIncidentPolicyRejectsOverflowingDerivedBounds$' -count=1` 在新增 under/over 断言后失败；旧领域校验错误接受 recovery successes 非 3、max gap 非 `2 * interval`。
- 独立审查 ingress invariant：`go test ./internal/center/http/handlers -run '^TestAgentSyncHandlerRejectsMixedHeartbeatSyncBatchIDsBeforeService$' -count=1` 在产品校验前失败；旧 handler 会把 mixed heartbeat batch IDs 传给 service。

### GREEN / gates

- `go test ./internal/center/incidents -run '^TestEvaluateMonitoringInstanceHeartbeatMissing.*(Boundary|CustomThreshold|Jump|Recovery)' -count=1`：PASS。
- `go test ./internal/center/incidents -run '^TestServiceHeartbeat(Policy|Recovery)' -count=1`：PASS。
- 公开 post-sync 第二轮：全回填/mixed/live carrier、直接 N=20 的 19/20、settings/receipt error 的 log+nil+零副作用及 CAS provenance 用例均 PASS。
- overflow 第二轮：settings/global override、domain direct policy、饱和 missed helper、invalid persisted timing 与 direct fallback focused tests 均 PASS。
- 默认 fixture 第二轮：`go test ./internal/center/http/handlers -run '^TestSettingsHandlerPreservesEffectiveFreshInstallSettingsOnUnrelatedSave$' -count=1` 与 Node 22 `api.test.ts` GET/PUT focused tests 均 PASS。
- `go test ./internal/center/incidents -run '^TestPostgresSnapshotReaderListRecentLiveHeartbeatReceipts' -count=1`：PASS。
- `go test ./internal/center/settings ./internal/center/store/migrate -run '^(TestSettingsDefaultProvidesDeterministicSingletonShape|TestHeartbeatIncidentPolicyMigration.*|TestFrozenR1RootSourcesRemainExactPrefix|TestRootMigrationsExcludeObsoleteAppExtensionDraft)$' -count=1`：PASS。
- `go test ./internal/center/incidents -run '^TestServiceHeartbeatNotification' -count=1`：PASS。
- `go test ./internal/center/incidents ./internal/center/settings ./internal/center/store ./internal/center/store/migrate ./internal/center/http/handlers ./internal/center/syncing -count=1`：PASS。
- strict PostgreSQL：`scripts/test-record-platform-integration.sh postgres -- go test -v ./internal/center/store/migrate -run '^TestPostgresIntegrationHeartbeat(RecoveryReceipts|IncidentPolicyMigration)$' -count=1`，两项均实际 RUN/PASS，无 SKIP。
- Node 22 focused Settings：21/21 PASS；`npm --prefix web run lint`、`npm --prefix web run build` 均 PASS。
- Node 22 `make verify-web`：exit 0，206 files / 1623 tests、coverage、build、bundle budget、CSS budget 全 PASS；`npm --prefix web run test:e2e`：136/136 PASS。
- `go vet ./agent/... ./cmd/... ./db/... ./internal/...` 与 `git diff --check`：PASS。
- `make verify-go`：本任务包全部 PASS；唯一失败是未触及的 `internal/center/attachments` `TestPreviewImageGoldenMetadataFreeBoundedPNG`，实际 digest `0d749fd4…`、golden `dac4e6f5…`，focused 重跑可复现。
- 共享 ingress 上限第三轮：`go test ./internal/center/syncing ./internal/center/http/handlers -run 'Test(MaxBatchItemsMatchesAgentIngressContract|AgentSyncHandlerRejectsTooManyHeartbeatsBeforeService)$' -count=1`：PASS，handler 不再复制 256。
- 有界查询第三轮：`go test ./internal/center/incidents ./internal/center/store/migrate -run 'Test(PostgresSnapshotReaderListRecentLiveHeartbeatReceiptsSQLContract|HeartbeatIncidentPolicyMigrationSourceContract)$' -count=1`：PASS；上面的 strict PostgreSQL 命令在 3072 条旧重复历史、同 `received_at` 最新三批次上实际 RUN/PASS，无 SKIP，并从生产 reader 捕获 SQL 后以 `EXPLAIN ANALYZE` 证明 WindowAgg/Sort/input actual rows `<= 768`。
- index/IO 证据第四轮：`ANALYZE` 后用 `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` 对捕获的生产 SQL 递归断言 exact `idx_monitoring_instance_heartbeats_live_received` 的 Index/Index Only Scan，禁止 Seq/Bitmap path。strict focused GREEN 的 baseline 为 scan rows 768、filter removed 0、shared blocks 28；再增加 12288 条旧重复历史后仍为 768/0/32，固定上界与非线性读取差分均通过。
- visual-evidence fixture 第四轮：上述 focused Python 用例 GREEN；`python3 scripts/test_visual_evidence.py` 全 6 项 PASS，active `/api/settings` mock 默认固定为 12。
- 通知安全第三轮：`go test ./internal/center/incidents -run '^Test(ServiceHeartbeatNotification(IncludesMonitoringInstanceIdentity|UsesUnnamedMonitoringInstanceFallback)|HeartbeatNotificationDeliverySanitizesAndBoundsMonitoringInstanceDisplayName)$' -count=1`：PASS；名称换行/control/bidi、Unicode 100-rune 截断与 unsafe-only fallback 均覆盖，Telegram/飞书 records 与 dispatcher 正文逐字一致。
- 清理统一 snapshot 后无生产调用的 `incidentTiming`、`heartbeatIntervalFor`、`incidentTimingFor`、`applyIncidentDefaults`、`metricThresholdsFor`；`go test ./internal/center/incidents -run '^Test(SettingsBackedSweepIntervalUsesPersistedSettings|SweepIntervalFallsBackWhenPersistedSettingsAbsentOrUnavailable|ServiceNormalizesOverflowingFallbackHeartbeatInterval)$' -count=1` 在清理后 PASS，独立 sweep scheduler fallback 未改变。
- 独立审查 policy contract：领域校验固定 recovery successes=3、max gap=`2 * interval`；对应 under/over focused regression 在 Go 1.26.2 下 PASS。
- 独立审查 ingress invariant：handler 在 service 前拒绝 mixed heartbeat batch IDs；与共享 `MaxBatchItems=256` 共同证明每个 accepted distinct batch 最多占 256 行，focused regression PASS。
- 工具链诊断：默认 shell 的实验性 `go1.27.0-X:nodwarf5` 令未触及的 PNG byte golden 产生不同摘要；`GOTOOLCHAIN=go1.26.2` 与 `go.mod`/CI 一致，同一 focused attachment test PASS。最终 `make verify-go` 必须使用该精确工具链重新执行。
- 最终 Go 门：`GOTOOLCHAIN=go1.26.2 make verify-go` exit 0，包含 attachments byte golden 与本任务所有 Go package；不再存在阻塞项。
- 最终 strict PostgreSQL 门：Go 1.26.2 下两项 heartbeat migration/recovery integration test 均实际 RUN/PASS、无 SKIP；生产 reader 的两档大历史执行计划继续使用 0063 exact ordered index，并保持 768 行扫描上界。
- 最终 Web 门：Node 22 `make verify-web` exit 0（206 files / 1623 tests、coverage、build、bundle/CSS budgets）；`npm --prefix web run test:e2e` 136/136 PASS（2.3m）。
- 最终视觉门：`python3 scripts/test_visual_evidence.py` 6/6 PASS；使用生产构建预览和 `observability-support` mock 对 `/settings` 执行 browser sanity，1440×1000、1024×768、390×900 均 PASS、路由正确且 document/body 无横向溢出。Vite dev HMR 与 Python Playwright 组合曾触发 react-refresh preamble 空白页，改用项目构建产物的 `vite preview` 后通过；临时服务已停止且端口释放。
- 最终独立审查：依次修复严格 recovery policy、mixed heartbeat batch ID ingress invariant 与默认阈值重复 literal；最后一轮全范围检查 Critical/Important/Minor 均无发现。
