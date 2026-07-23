# 集成切换、安全、性能、备份恢复与终验 Design

## 1. 设计状态与边界

本设计承接父任务已批准的领域、安全、恢复和视觉合同。子任务 1–10 提供业务权威与窄 adapter；本任务只负责把这些边界装配为可运行系统、证明所有 adapter 完整、建立真实备份恢复/集成/基准环境，并控制最终切换。任何 adapter 缺失都属于启动或切换硬失败，不能由反射、通用 JSON 删除或“未知对象忽略”兜底。

当前仓库的真实基线是：应用 migration 最大为 0050；center 在 `cmd/houfeng-center/bootstrap.go` 显式装配 5 个 worker；CI 只有无外部 PostgreSQL 的 Go job、Node 浏览器 job和 Docker build；正式 Web toolchain 由 `.node-version` 固定为 Node 22.x，而当前交互 shell 是 Node 24.18.0。实现必须以届时最新主线为准重新核对这些数值，但不能把当前缺口写成已具备能力。

## 2. 运行拓扑与故障域

```text
                         ┌────────────────────────────┐
                         │ houfeng-center / Web SPA   │
                         │ HTTP + explicit workers    │
                         └──────┬───────────┬─────────┘
                                │           │
              ┌─────────────────▼──┐    ┌──▼───────────────────┐
              │ application PG 16   │    │ BlobStore             │
              │ records/projections │    │ local persistent / S3  │
              └──────────┬──────────┘    └──┬───────────────────┘
                         │ backup adapter    │ object catalog/hash
                         └──────────┬────────┘
                                    ▼
                         ┌──────────────────────┐
                         │ signed backup target │
                         │ immutable manifest   │
                         └──────────────────────┘

   separate credentials/volumes/recovery path
   ┌────────────────────┐  ┌────────────────────┐  ┌─────────────────────┐
   │ deletion-ledger PG │→ │ full witness PG or │  │ recovery-control PG │
   │ append-only primary│  │ MinIO WORM entries │  │ trust/inventory/ws  │
   └────────────────────┘  └────────────────────┘  └─────────────────────┘

   isolated restore network
   ┌──────────────────────────────────────────────────────────────────────┐
   │ restore workspace: restored PG + Blob + WAL/tmp + replay adapters   │
   │ no HTTP / no normal worker / no outbound delivery until final gate  │
   └──────────────────────────────────────────────────────────────────────┘
```

应用 backup 只能读取 application PG 和应用管理的 Blob/catalog。它不能读取或复制 ledger、witness、recovery-control 的数据库文件、DSN、凭据或 volume。恢复时反向依赖 fresh ledger/full witness/trust store，但不能用待恢复 application PG 中的副本替代它们。

## 3. 模块与文件所有权

- `internal/center/recovery/`：平台基础提供 manifest/trust/inventory/workspace/replay primitive；本任务增加 backup/restore orchestration、source normalizer、adapter set validation、fault cutpoints 和 ownership transfer。
- `cmd/houfeng-center/record_recovery_adapters.go`：唯一显式constructor manifest，逐项调用子任务1–10 owner package已经交付的typed constructor并生成canonical adapter-set digest；同名test把activation inventory与constructor/result逐项对齐。禁止包级`init`、反射、文件名扫描或task11直接修改owner adapter来凑清单。
- `scripts/collect-records-child-deliveries.sh`、`test/integration/records/child-deliveries.schema.json` 与 `internal/center/deploy/child_deliveries_static_test.go`：在CI/交付控制面验证1–10的merged PR/check run/merge commit/digest receipt和当前HEAD ancestry，输出仅进入leak-scanned gate artifact及其digest；生产center不调用Git/GitHub，也不把交付验证塞进recovery runtime。
- `internal/center/store/record_cutover.go`：0060 cutover 状态、gate receipt、CAS 和审计查询；不承载 feature 判断以外的业务内容。
- `cmd/houfeng-backup/`、`cmd/houfeng-restore/`：官方运维 CLI，只依赖 recovery service 接口和 typed config，不直接拼 SQL、shell command 或对象路径。
- `internal/center/recordsecurity/`：telemetry inventory、配置 digest、内容泄漏扫描结果模型与 capability preflight；不保存扫描 corpus 正文。
- `internal/center/recordbench/` 与 `cmd/houfeng-records-bench/`：确定性 seed/load/report 工具，只用于测试与运维验收，不进入发布镜像的默认运行路径。
- `test/integration/records/`：PostgreSQL 16 四库、MinIO/Object Lock、ClamAV、content processor 的真实集成编排、fixture 和 shell driver。
- `web/e2e/vps-records-*.spec.ts` 与现有 fixture router/profile：六表面状态、视觉、Axe、键盘、焦点、44px、撤权和降级合同。fixture route 只在 Playwright/test build 生效。
- `docs/deploy/` 与 `docs/operations/records/`：正式配置、备份/恢复/删除/切换/回退/runbook、恢复演练证据模板和 telemetry inventory 示例。

所有新增 Go 命令调用库接口；业务逻辑不复制到 `main.go`。所有外部二进制通过参数数组执行并固定绝对路径，禁止 shell 插值。文件路径、对象 key、标题和正文不进入日志。

## 4. 0060 cutover 数据合同

`0060_record_platform_cutover.sql` 新增两类无内容数据：

1. `record_platform_cutovers`
   - key：`deployment_id + project_id`；
   - `phase`：`off | shadow_read | records_write_legacy_read | records_read_default | records_default_verified`；
   - `contract_version`、只读快照字段 `observed_minimum_fence_contract_version`、`observed_activation_sequence/hash`、`legacy_write_mode`、`records_read_default`；权威minimum仍只来自独立ledger/full witness的`contract_activation`和平台deployment membership/replay state，cutover service无写它的接口；
   - `revision` 单调递增，`changed_by`、`changed_at`、`reason_code` 与 `gate_set_digest`；
   - CHECK 保证 `records_write_legacy_read` 起 legacy 写为 false，后续阶段永不重新打开；default read 只能在后两阶段为 true。
2. `record_platform_gate_receipts`
   - append-only receipt ID、gate kind/version、commit、environment、fixture/seed/config digest、passed_at、valid_until、artifact digest 与无内容 summary counts；
   - 不保存 URL、用户名、记录 ID、正文、截图路径或 CI credential；
   - unique `(deployment_id, project_id, gate_kind, gate_version, artifact_digest)`，过期 receipt 不能推动 phase。

phase 更新使用 `expected_revision + expected_phase + gate_set_digest` CAS，并复用平台幂等服务。每次CAS先从fresh primary+full witness读取权威activation sequence/hash/minimum，要求与cutover行的observed字段和gate receipt完全相等；事务只更新观察快照/phase，不生成或修改权威minimum。允许的转换固定为相邻前进，以及：

- activation 前 `shadow_read → off`；
- 新写启用后只允许把 UI/read default 从后两阶段回到 `records_write_legacy_read`，legacy 写仍保持关闭；
- 任何阶段都不能降低 minimum fence-contract version、清空 ledger activation 或让旧 worker重新入队。

迁移不创建 legacy destructive trigger，也不删除 `experience_logs`。legacy 的写拒绝由 phase-aware service/handler 和数据库约束共同执行；回退 binary 仍必须理解该约束，否则 readiness 拒绝。

## 5. CLI 合同

### 5.1 通用规则

两个 CLI 支持 `--output json|text`，自动化默认 JSON；stdout 只写最终结构化结果，进度和无内容诊断写 stderr。所有结构带 `schema_version`、`operation_id`、`state`、`reason_code`、`observed_contract_version` 和 digest，不输出 DSN、对象 key、record ID、文件名或源内容。

统一 exit code：

- `0`：请求成功且达到命令承诺的终态；
- `2`：参数/schema/本地配置无效；
- `3`：授权、preflight、capability 或 cutover gate 拒绝；
- `4`：操作已持久接受但仍在进行，调用方应按返回 operation ID 查询；
- `5`：完整性、签名、账本连续性或 adapter 合同失败关闭；
- `6`：清理/receipt 尚未收敛，内容保持隔离且需要运维处理。

相同 idempotency key + 相同 fingerprint 返回同一 operation；相同 key + 不同参数返回冲突。CLI 不读取交互确认作为自动化唯一证据；破坏性操作必须同时提供准确 operation/workspace ID 与 `--confirm <digest>`。

### 5.2 `houfeng-backup`

- `plan`：验证配置、inventory、lease 可取得性、空间、source policy、trust key、telemetry 和当前 replay watermark，返回 `plan_digest`、预计 scope/count/bytes、`recoverable_until` 与阻塞 reason。
- `run --plan-digest --idempotency-key`：登记 attempt 后执行备份；同步等待可用 `--wait`，超时返回 exit 4 而不取消服务端操作。
- `status --operation-id`：返回状态、阶段、无内容计数、receipt digest。
- `verify --manifest`：离线/在线验证签名、trust revision、数据库/object digest、source binding、expiry 和 inventory；不恢复数据。
- `inventory`：输出受管恢复点/策略/到期的无内容列表。
- `cancel --operation-id --confirm`：停止未发布 attempt 并进入 janitor；published artifact 不由 cancel 删除，按库存销毁合同处理。

### 5.3 `houfeng-restore`

- `plan --manifest --target-deployment`：只读取 manifest/trust/inventory，登记或返回 dry-run；在复制前证明 isolation、backup exclusion、空间、剩余窗口、adapter set 和 fresh witness 可用。
- `run --plan-digest --workspace-id --idempotency-key`：执行六步恢复；不能通过参数跳过 replay、final sync 或 startup gate。
- `status --workspace-id`：返回当前 step、source expiry、lease、head/watermark 和无内容 receipt counts。
- `renew --workspace-id --confirm`：只在普通 workspace 上延长活动 lease，仍受三个 expiry 上限约束。
- `forensic --workspace-id --reason-code --approval-id --confirm`：转为隔离取证；不开放 HTTP/worker/outbound，也不重置 expiry。
- `cancel` / `destroy`：停止并清理；destroy 只有逐位置 receipt 完整后返回 0。
- `verify-ready`：重复 fresh head/fence/adapter/ownership gate，不改变状态；只有 `ready` workspace 才能由 `transfer` 原子接管为新 deployment。

## 6. Backup 状态机与一致性

状态机为：

```text
planned → leasing → snapshot_registered → copying → verifying → signing → published
              └──────── any failure/cancel/expiry ───────→ purging → purged
```

- `snapshot_registered` 使用具体PostgreSQL 16协议：先取得deployment-wide backup lease并开启只阻断record-content commit/deletion reservation的短时write-admission barrier；T1事务枚举正式/历史refs，写 `backup_epochs` marker、refs digest、对象pin与当时连续 `last_fully_applied_sequence/hash` 后commit。T2在barrier仍生效时执行 `BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY`、读取同一marker/watermark、调用`pg_export_snapshot()`并从该snapshot重新枚举refs；digest必须与T1 pins相同。然后以参数数组启动 `pg_dump --snapshot=<exported-id>`，T2保持打开到pg_dump完成；T2 snapshot建立且refs核对后即可释放短时barrier，正常新写继续但不进入本artifact。backup lease/pins继续阻止GC和deletion到发布/清理终态。
- 数据库 artifact 必须来自该exported snapshot；Blob manifest只列T2同snapshot全部正式/历史引用及精确version/hash。copy中新增业务数据不属于本恢复点，不能把结束时看到的新ledger head写为baseline。marker/export/ref digest任一不一致、T2提前结束或pg_dump未接受snapshot都进入purging。
- 每个 partial 在 recovery-control 中有 location class、opaque storage ID digest、bytes/hash、owner lease 和状态；目标 storage 写入第一字节前已有行。
- backup staging prefix/volume/multipart必须由preflight读取实际backup/PITR/snapshot/S3 lifecycle配置证明排除；若不能排除，attempt在任何byte前调用inventory/trust service把该域登记为derived recovery source，绑定planned backup ID/baseline、签名manifest和`derived_recoverable_until <= planned_recoverable_until`。Object Lock/volume policy最短保留超出上限时plan失败，不能先写再补登记。
- verifying 从数据库独立枚举引用并与 catalog 双向比对：缺对象、错 version/hash、额外未声明正式 artifact 或 source 绑定不一致即失败。
- signing 使用独立 0400 Ed25519 key file；`algorithm/key_id/signed_at` envelope、`recoverable_until`、baseline、DB/object digests 都在 canonical bytes 内。manifest + signature 以原子/conditional put 发布后 attempt 才是 published。
- published artifact不可就地改写；策略到期销毁只写介质 ID digest、策略、时间和结果。增量对象仍以内容 hash/version不可变，并由每个 manifest独立引用。

## 7. RecoveryPointManifest 与 trust

共同 envelope 包含：format/source kind/version、deployment/project scope、backup/snapshot/base ID、应用 schema/cutover contract、created/recoverable-until、DB artifact/timeline/LSN、Blob catalog digest、applied replay baseline、policy/config digest、signing key/revision 和 canonical hash。

来源专属 binding：

- full/logical：DB artifact SHA-256、snapshot marker、完整 Blob object-list digest；
- PITR：signed base manifest、timeline/start LSN、连续 WAL range/target、内容寻址 Blob durability catalog；恢复后 DB 水位只能证明合法前缀，replay baseline仍取较低 signed base 水位；
- volume/filesystem snapshot：snapshot ID、同一冻结点 DB checkpoint/timeline、watermark、Blob version digest 的 signed sidecar；
- S3/object version：manifest/catalog 中精确 version/hash，不允许按“latest”解析。

RecoveryTrustStore 位于独立 control plane，按 revision/hash chain验证 full witness；retired key 在有效区间继续验证，compromised/missing/unknown key全部拒绝。key 只有在库存证明最后一个依赖它的受支持恢复点过期销毁后才能删除元数据；compromised key 元数据保留用于拒绝和取证。

## 8. Restore 编排与启动 gate

状态机为：

```text
planned → preflight_passed → materializing → replaying → rebuilding
        → final_sync → ready → transferred
 any non-forensic failure/cancel/expiry → stopping → purging → purged
 ready/failed → forensic (explicit approval, same source expiry) → purging → purged
```

六步实现为可重入 checkpoint，每步写无内容 progress/receipt 后才前进：

1. `preflight`：验证 manifest canonical bytes/signature、fresh trust head、来源 binding、inventory、空间、配置、isolated network、workspace backup/version exclusion 和恢复预算；任何 byte 写入前完成。若任一DB/Blob/WAL/tmp/export/processor location不能证明排除，先为整个derived workspace签名登记RecoveryPointManifest/inventory/replay baseline，强制`derived expiry <= source expiry`；无法满足即拒绝。
2. `materialize`：恢复 DB/Blob/WAL，但 deployment HTTP、normal workers、external notification、export 和 browser endpoint 均不存在；校验 marker/timeline/catalog及所有引用 hash。
3. `fetch-ledger`：从恢复开始后取得 fresh authoritative head，证明 primary 与 full witness 相等，从 signed baseline 连续拉取到 head。
4. `replay`：按 sequence 严格调用 registry 中唯一 object-kind/contract-version adapter；每条 entry 有 adapter receipt，未知合同不推进。
5. `rebuild`：从权威事实重建 search/activity/summary，清 cache，验证 legacy/import/export/processor/workspace 和全局引用；不能恢复旧投影冒充完成。
6. `final-sync`：再次读取 fresh head，重放新增尾部，验证 minimum version/deployment fence，原子写 `ready`。`transfer` 使用 recovery-control CAS 把 workspace 全部 location 所有权转为新 deployment，随后才允许 center readiness。

每次启动 `houfeng-center` 都验证 deployment identity/epoch、cutover contract、continuous replay head、adapter set digest、witness freshness 和 recovery ownership。旧 deployment 的 lease/fence失效后不能继续写；失败只让 records protected domain或恢复环境 fail closed，普通生产环境中不含记录内容的监控采集可按平台基础合同局部继续。

## 9. Replay adapter registry

统一接口语义：

```go
type ReplayAdapter interface {
    ObjectKind() string
    ContractVersions() []uint32
    Inventory(context.Context, Scope) (InventoryDigest, error)
    Replay(context.Context, LedgerEntry, RestoredStores) (ReplayReceipt, error)
    Verify(context.Context, LedgerEntry, RestoredStores) (VerificationReceipt, error)
}
```

实现可按仓库惯例拆分方法，但必须保留 object kind、明确版本、inventory digest、幂等 replay 和独立 verify 语义。registry 启动时把完整 adapter set canonical 化并与 activation manifest/cutover receipt digest 比对。task11只验证registry、按sequence调用、收集receipt并编排rebuild；每个owner adapter自己实现本领域Inventory/Replay/Verify/在线Deletion。若conformance发现领域缺陷，停止task11并在对应owner package的新受保护PR修复、合入、发布后重建child receipt；禁止在`recovery`写跨领域SQL/Blob删除或通用JSON/table清理。

`cmd/houfeng-center/record_recovery_adapters.go` 是可执行constructor manifest：它显式列出records core（含Markdown revision/conclusion）、attachments、evidence（含comparison result/copied lineage）、collaboration、search、activity、portability、legacy、source-object、processor/workspace owners的constructor、object kinds与contract versions。Markdown renderer/browser managed buffer没有独立application recovery adapter：renderer版本由registry conformance验证，正式Markdown由records core恢复，浏览器缓冲不属于server backup且只参加revoke/deletion/terminal-retention receipt测试。编译期typed参数使缺constructor直接失败；runtime再验证missing/duplicate/unknown version与activation digest。owner包之外没有第二份清理实现。

| Adapter owner | 恢复/删除范围 | 验证要点 |
|---|---|---|
| records core | root、完整 revision（含Markdown/conclusion）、draft/recovery point、关系、reservation/outcome、最小删除审计 | current 可由 revision 重建；无正文审计；attempt-not-committed 不清数据 |
| attachments | logical attachment、revision refs、Blob version、multipart/quarantine/preview workspace、pin | 全局引用后 GC；独占 byte 无宽限；hash/version 100% |
| evidence | logical snapshot、payload、intent、source identity/authorization floor、`comparison.result/*`与copied lineage | 其他记录合法 copy 保留；source 删除后 floor 不扩大；comparison typed refs完整 |
| collaboration | owner/participant、action、comment history/tombstone、watch/inbox/delivery | 外投重试取消；摘要/recipient/message关联清除 |
| search | search document、cursor/watermark/cache | 从权威revision/relations重建；被删命中0 |
| activity | canonical activity、subject relations、revision intervals、published head/generation、overview summary | 从权威source adapters重建；online deletion与restore均阻止旧projector复活 |
| portability | export artifact、import upload/tree/plan/classification/origin/tombstone | stale plan拒绝；legacy/import origin 不复活 |
| legacy | `experience_logs` 原行与 mapping | delete_commit 清正文；迁移幂等；保留无内容 tombstone/mapping |
| processor/recovery | scanner/renderer/profile/cache、backup partial、restore DB/Blob/WAL/tmp | 每个位置 receipt；失败 workspace残留 0 |

来源 VPS/MonitoringInstance/Target 的 adapter还必须重建 final authorization floor、断开 live route并保留受 floor约束的 identity snapshot。任何未列入 activation inventory 的新 object kind在发布前必须增加 adapter/version、恢复 fixture和兼容矩阵；仅注册 domain type 不足以发布。

## 10. Workspace janitor 与故障注入

`backup_attempt`、`restore_workspace`、import/processor workspace遵循相同的“先登记、后写字节”纪律，但由各自 owner处理。集成 janitor负责跨域对账：

- 有数据库 attempt 无 owner lease；
- 有 storage prefix/volume/multipart 无数据库 attempt；
- published/transfer receipt 与实际 location不一致；
- source expiry 已过、lease 失效或 deletion reservation 命中 scope；
- forensic workspace 缺 approval/访问审计或到期；
- processor/import 子 receipt 未收敛。

unknown partial 不被自动认作垃圾；先把 permanent delete capability fail closed并创建无内容告警，只有证明 scope/owner后才能清理。janitor每个 location使用幂等 delete + verify-not-found/hash-list，再写 receipt；只改状态或断开引用不算清理。

fault harness在 backup marker 前后、DB dump、Blob copy、multipart、sidecar、manifest、signature、publish，以及 restore step 2–6 的 DB/Blob 落盘、ledger gap、adapter receipt、projector rebuild、二次追平、ownership transfer处提供可重复 kill point。每个 kill point重启后必须走到 published/transferred或purged，不允许悬空。

同一harness提供HTTP/stream黑盒reservation cutpoints：before headers、after headers、first chunk、last chunk，并并发启动download、evidence/attachment preview、export、import classification/apply、processor和restore workspace。测试只通过真实handler与child-owned adapter，不在task11复制领域删除；它等待socket writer停止与content-delivery epoch receipt。任何已发/未知结果使preview stale并阻止ledger append，不能把仍写socket的实例判为drained。

source-object replay scenario分别覆盖record、VPS、MonitoringInstance、Target：来源删除先由owner adapter恢复final authorization floor，再保持source absent/tombstone、断开live links并禁止name relink；合法记录/证据快照继续按floor读取。fake clock随后越过24h lease/guard/member和30d receipt/job/telemetry窗口，扫描全部应用表/object/workspace/log sink，长期allowlist之外content/stable ID命中为0。

## 11. 真实集成测试拓扑

`test/integration/records/compose.yaml` 作为测试 overlay，使用：

- `postgres:16` ×4：app、ledger、witness、recovery-control，各自 network alias、credential 和 volume；
- MinIO server + setup job：预创建 versioning/Object Lock bucket、COMPLIANCE/测试保留和 recovery-trust WORM namespace；
- ClamAV daemon；
- 使用当前 commit 构建的center/content-processor/backup/restore/rollout五个正式binary的同一image；comparison intent keyring等runtime secrets只以预期UID owner的0400 regular-file只读mount注入，不进入image/layer，所有binary以non-root运行；
- 只在测试 network可见的 fake external-notification receiver，记录的只是 template/event digest，不收正文；
- local profile单独挂载 private durable volume，S3 profile不共享该目录。

overlay提供两组明确策略fixture：`excluded`证明staging/workspace不受backup/version policy覆盖；`derived-source`模拟无法排除的volume/prefix，要求写前出现signed inventory并继承更短expiry；第三组`retention-too-long`在写前稳定拒绝。三组都由storage/volume实际配置读回证明，不接受只看环境变量。

0050 upgrade fixture禁止手写：`generate-0050-schema.sh`在clean PostgreSQL16中检出并验证不可变`v0.59.0` release tag/commit和0001–0050 migration hashes，运行该release真实app migrator，再用固定`pg_dump --schema-only --no-owner --no-privileges`与canonicalizer生成fixture/provenance；CI重新生成并byte-compare schema与hash，fixture人工编辑或release provenance漂移立即失败。测试driver等待真实health，应用该generated 0050 schema再升级0051–0060，运行migration repeat、adapter conformance、E2E saga、backup/delete/restore、fault matrix和日志扫描，最后验证volume/prefix/multipart清空。任何服务日志会先过内容corpus scanner再允许上传CI诊断。

## 12. 安全与 telemetry inventory

runtime读取版本化 inventory文件；每个 entry至少包含 `sink_id/type/owner/location_class/config_digest/max_retention/archive_sources/content_allowlist_version/verification_method`。preflight把 canonical digest写入 capability和gate receipt，配置漂移会使旧 receipt失效。

`recordsecurity` 只保存 corpus item digest、sink result count和扫描时间，不保存 corpus原文。测试把同一 corpus注入：HTTP success/error、DB constraint/slow path、object put/get、worker retry、processor crash、browser error、import/export、backup/restore和删除失败；随后扫描 container stdout/stderr、结构化日志、PG logs、MinIO access、processor workspace/core、browser diagnostics和CI artifact staging。任一 raw corpus或stable ID命中即 gate失败并保留本地隔离证据，不能上传含命中内容的文件。

生产 inventory不能声称控制组织自行维护的外部备份；这些在删除 preview/文案中作为不可召回边界披露。operator-supplied active inventory由runtime枚举HTTP/DB/object/browser/processor/backup工具和外部sink，逐项读取live config并核对config digest；example只用于schema/source tests。任一受管sink无法证明allowlist与≤30天TTL时永久删除关闭，即使它同时登记为RecoveryPointManifest source也不能豁免telemetry上限。

retention conformance使用fake clock推进31天并检查online sink、archive、日志备份和第三方test receiver：request/correlation/deletion-operation ID均不存在；长期指标只保留route template/status/time bucket等不可逆聚合且无法join回stable object。任何sink无可验证delete/expiry API都不能通过active inventory。

## 13. 可复现基准

`houfeng-records-bench` 提供 `seed`、`run`、`explain`、`capacity`、`report verify`：

- seed使用固定PRNG version与seed值，先生成主体/授权，再生成10k records/200k revisions/1m activities及固定证据/附件分布；输出canonical seed manifest/hash。
- run使用固定到达率而非“尽快”，同时跑uniform和80/20热点profile；除overview/search/timeline/draft/revision/evidence外，必须包含comparison summary 2rps与6×2,000 detail 0.2rps并注入aggregate admission/cancel；写使用独立record避免人为CAS冲突，预期409/404/429明确标类。
- 每轮记录client端时长、server无内容metrics、CPU/memory/IO/DB connection、comparison admission wait/reject/active weight/drain、cgroup peak/events和错误分类；overview/search/timeline成功样本不足5000、draft/revision/comparison summary不足1500、comparison detail/evidence不足150时整轮无效而不是计算小样本p95。
- explain从代表性query digest映射到参数化SQL fixture，只保存规范化SQL fingerprint和plan，不保存搜索词/record ID。
- verify检查环境、版本、config/seed/commit hash、三轮独立门槛、错误率和样本量；不能用三轮合并结果代替单轮通过。

报告目录按commit和profile分开，CI artifact只含无内容聚合、EXPLAIN和环境元数据。正式门槛只接受专用self-hosted runner label `houfeng-records-benchmark-x8664-v1`：无其他租户，application cgroup 4vCPU/4GiB，PostgreSQL 4vCPU/8GiB+独占NVMe≥10k持续随机IOPS、RTT≤1ms，MinIO profile独立2vCPU/2GiB。仓库设置中登记的`records-benchmark-operator`负责运行前清空/验证runner、记录CPU型号/内核/firmware/cgroup/服务版本与config digest并签署无内容attestation；代码作者或实现代理不能自行把不匹配环境标为正式通过。runner label、operator identity/attestation或硬件任一缺失时gate保持失败；普通GitHub hosted结果仅作趋势附录。

容量预算是发布合同，不允许临时放宽：

| Gate | 阈值 | 稳定 reason code | 最大 drain time | 验证命令 |
|---|---|---|---|---|
| comparison 单请求/aggregate | request peak-idle≤96MiB；active weight≤512MiB；queue≤16；wait≤2s；4GiB容器保留≥1GiB non-comparison headroom | `comparison_request_memory_limit` / `comparison_capacity_exhausted` | cancel/断连后worker+writer+token≤5s | `./scripts/run-comparison-capacity.sh --profile single --runs 3` 与 `--profile aggregate --runs 3` |
| application mixed-load memory | 每轮cgroup `memory.peak≤3GiB`，`memory.events`的oom/oom_kill/high-throttle增量=0 | `record_capacity_headroom_exhausted` | 停止load后≤5s回到idle+512MiB以内 | `./scripts/run-records-benchmark.sh --profile <local|minio> --rounds 3 --capacity` |
| outbox/projector/notification queues | steady oldest age≤60s且queue≤10,000；停止load后pending=0 | `record_queue_backlog` | ≤10m | 同上`--capacity` + `go test ./internal/center/recordplatform/... -run 'QueueCapacityDrain' -count=1` |
| processor/import temporary workspace | warning=configured hard bytes的80%；新job估算越过100%则写首byte前拒绝；取消/失败残留=0 | `record_workspace_capacity_exhausted` | integration clock≤15m | `./scripts/run-records-integration.sh --profile <local|minio> --scenario workspace-capacity` |
| backup/restore workspace与partial | free bytes≥估算materialized+scratch+20% reserve；不足时写入字节=0；失败位置receipt完整 | `recovery_capacity_insufficient` | fault run≤15m收敛purged | `./scripts/run-records-recovery-drill.sh --profile <local|minio> --scenario capacity` |
| attachment/evidence logical quota | 各自默认project 10GiB、80% warning、100%拒绝新增；合法既有内容不为过线而删除 | `attachment_quota_exceeded` / `evidence_quota_exceeded` | N/A（admission gate，不是可drain数据） | `go test ./internal/center/attachments ./internal/center/evidence -run 'QuotaCapacity' -count=1` |
| orphan/backup partial | permanent-delete独占object无宽限；普通orphan在fake clock 24h后命中0；unpublished partial终态命中0 | `record_orphan_cleanup_pending` | 到期/故障注入后≤10m | `./scripts/run-records-recovery-drill.sh --profile <local|minio> --scenario terminal-cleanup` |

## 14. 视觉与交互终验

fixture模型使用六个surface ID与版本化state，而不是让测试任意mock DOM：

| Surface | 核心 fixture |
|---|---|
| `vps-overview` | stable、actual anomaly、partial source failure、records-domain unavailable、permission/deleted |
| `records-center` | first-empty、query-empty、results、append-loading、local failure、revoke |
| `subject-timeline` | empty、mixed sources、one-source failure、projector lag、tombstone/404 |
| `comparison` | under-selection、comparable、no-overlap、stale/missing/registered-but-Compare-incompatible schema、revoked item、running/cancel |
| `record-editor` | new/template、loaded、draft saving/saved/failed、conflict、material failure、revoke |
| `evidence-picker` | source/schema loading、no source/no data、preview ready/stale、capture running/failure、revoke |

VPS stable fixture对 DOM文本、role和关键layout box断言异常容器/动作/保留高度均不存在；anomaly fixture断言它位于identity与常规summary之间且恢复切换后focus保持。正式视觉基线由 Artifact v1 与deterministic DOM/状态/几何合同组成，动态状态使用结构/行为断言避免时间抖动；不新增tracked pixel golden、screenshot manifest或批量raster资源。

Playwright在desktop与390px跑同一commit。正式suite路径固定为`web/e2e/vps-records-visual-contract.spec.ts`、`web/e2e/vps-records-accessibility.spec.ts`、`web/e2e/vps-records-security-states.spec.ts`，每个`test.describe`/test title真实包含`@vps-records`，使`--grep`不是空标签约定；覆盖DOM/geometry、Axe、Tab/Arrow/Home/End/Escape、modal/drawer focus restore、BroadcastChannel revoke、reduced-motion、44px geometry和document overflow。全局页面无横向溢出；比较矩阵允许有accessible name的局部scroll container。本地人工预览可生成脱敏短期截图，但它们只按`docs/operations/ui-preview-and-browser-sanity.md`作为评审证据，不提交到仓库或冒充自动回归门。

30秒理解测试使用固定两组等价数据、桌面/390和稳定/异常反平衡；参与者不得参与本项目需求规划、视觉/技术设计、产品或代码实现、代码审查、测试/fixture实现、Trellis/Codex规划复核，也不得预先知道四项答案。计时从主要内容first paint开始，四项全对且≤30秒才成功。匿名CSV validator检查参与者数量/新手比例/上述每类项目参与排除声明、条件分配/缺失理由/18-of-20门槛，报告不保存姓名、账号、真实资产或未经同意的录像。自动化browser cue/staging smoke只断言信息结构和入口可操作，永不写入该CSV或计入20人结果。

## 15. CI 与质量证据

`.github/workflows/ci.yml` 增加内部 `records-integration` 和 `records-visual` job，避免让普通单元步骤隐式启动重型服务，但不把“job存在”误当branch protection。当前远端 required contexts 是 `go`、`web`、`web-browser`、`docker-image`：实现把原执行job改为明确的unit/browser/build内部job，再保留这四个同名终态聚合job；`go` 必须 `needs` Go unit + records integration/代表性restore，`web-browser` 必须 `needs` browser contract + records visual/security，`docker-image` 必须经过image/artifact scan，任一dependency失败/skip都会让既有required context非成功。workflow source test和GitHub API/设置复核共同证明保护图有效；如仓库owner选择新增required context，必须在接收PR前更新远端规则并记录证据，不能由实现代理静默假设。

integration job固定PostgreSQL/MinIO/ClamAV/image digest并运行driver；visual job使用`.node-version`、lockfile-compatible Chromium和上述三个显式`@vps-records` spec路径。docker-image gate必须拉取候选digest并用隔离stack分别smoke `houfeng-center`、`houfeng-content-processor`、`houfeng-backup`、`houfeng-restore`、`houfeng-records-rollout`，不是只启动center。恢复fault全矩阵可拆成required scheduled/workflow reusable job，但最终cutover必须引用同commit、同release image digest的成功run，不能只依赖较旧nightly。

失败artifact先执行 `scripts/check-record-artifacts.sh`；命中secret/content corpus时阻止上传并只输出无内容reason/digest。所有报告包含commit/schema/config/fixture hash和命令，便于重现。

## 16. 已发布 digest 上的 Staging 切换状态机

```text
off
  → shadow_read
  → records_write_legacy_read
  → records_read_default
  → records_default_verified
```

- `off`：schema/worker可在，所有用户入口和records capability不可见；永久删除仍按独立activation gate决定。
- `shadow_read`：服务端对同一 legacy集合运行新projection对照并只记无内容差异计数；用户仍只看到legacy，唯一写仍是legacy。
- `records_write_legacy_read`：在一个cutover事务中关闭legacy写并开启records写；legacy UI/API只读且走fence-aware compatibility读取。不存在双写窗口。
- `records_read_default`：侧边栏、VPS新概览和新routes默认开放；compat read只用于回退/核对。
- `records_default_verified`：staging soak、真实数据、恢复、安全、视觉、性能和回退演练receipt完整；仍不删除legacy。

每次前进需要fresh gate set：migration/reconciliation、adapter/recovery、security/telemetry、performance、visual/accessibility、staging regression和rollback drill。receipt绑定commit、release version、container image digest、environment与config digest；配置、binary、image或部署变化使相关receipt失效。权威顺序为PR required checks→merge→main CI→Release Please/release image→五binary smoke→以全部records flags=off部署精确digest→运行实例核对commit/version/digest/off→preflight→本状态机。任何staging修复必须重新走受保护PR/release/deploy并从off重建receipt，禁止现场patch或branch image。staging自动browser cue只验证结构，30秒人类理解证据仍来自§14独立参与者协议。

## 17. 回退与恢复策略

- activation前：关闭feature/capability，停止新worker；additive schema保留。
- shadow阶段：可回到off，不改变数据权威。
- records写启用后：永不重新开放legacy写。UI可回到fence-aware compatibility read，records API仍是唯一写权威；修复后再推进default read。
- read-default后：route flag回退不执行down migration、不降minimum contract、不撤销ledger entry、不恢复旧cache/projection。
- processor/MinIO局部故障：新复杂材料或对应evidence kind fail closed；纯文字记录与不依赖该source的读取按能力继续。
- ledger/witness/fence不可证明：整个records protected domain读写失败关闭，普通生产环境的非记录监控可局部继续；restore环境整体不启动。
- release binary发现严重问题：只部署满足current minimum fence-contract且包含全部replay adapter的前一兼容版本。不存在兼容binary时保持records关闭并修复forward，不降级到不理解账本的版本。

## 18. 发布链与运维所有权

本任务PR必须在非main分支完成required CI；合并后继续监控main CI。Release Please生成的release PR仍需核对迁移/兼容/replay notes，发布后验证release job和Docker Hub多架构镜像，拉取发布digest完成五binary smoke，再以全部records flags=off部署并从运行实例核对version/commit/digest/capability/contract reason。只有这一步完成后才能进入§16 preflight与切换。若PR、main CI、release、image、部署或staging任一步失败，修复都从新的非main分支/PR开始并继续完整受保护链，不能把“代码已合并”或现场修复作为完成。

运维runbook明确：备份策略/库存owner、ledger/witness/trust key轮换、restore/forensic审批、workspace告警、telemetry inventory变更、cutover与回退、旧备份/legacy adapter到期审查。任何删除legacy表或移除旧replay adapter都是新的显式任务，需要库存证明相关恢复介质全部到期且用户另行批准。

## 19. 关键取舍

- 使用两个窄CLI而不是在HTTP管理页暴露恢复：恢复需要隔离网络、文件/volume控制和启动前运行，普通应用HTTP并不是可信控制面。
- 使用明确adapter registry而不是通用表清单/JSON脚本：恢复必须理解每个contract version、全局引用和authorization floor。
- 使用cutover状态机而不是单一boolean：写权威切换不可逆，读路由可回退，两者风险不同。
- 重型真实集成与普通单元job分开，但同commit都是required gate：既保留快速反馈，也不允许mock替代恢复证明。
- 性能报告与视觉/理解报告都绑定commit/fixture/config digest：避免用旧结果为新binary放行。
