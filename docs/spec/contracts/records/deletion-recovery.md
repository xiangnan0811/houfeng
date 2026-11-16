# Records 永久删除与连续恢复

## Scenario: Records permanent deletion、core purge 与连续 recovery

### 1. Scope / Trigger

- Trigger：修改 `internal/center/recorddeletion/`、`internal/center/store/record_deletions*.go`、`record_deletion_recovery*.go`、`0052_create_records_core.sql` 的删除投影/恢复字段，或后续 Records child 注册新的 permanent-delete adapter 时。
- 本场景建立在 [交付原语](../platform/delivery-primitives.md) 的 reservation、fence、opaque token、owner lease 与 admission 之上；它拥有 Records 删除编排、core 在线清除和 ledger replay recovery，但不拥有独立 deletion ledger/witness 服务或 attachment/evidence/search/activity/collaboration/portability 的内容表。

### 2. Signatures

```go
func NewRegistry([]Adapter) (Registry, error)
func (Registry) RequireReady(context.Context) (ReadinessSnapshot, error)
func NewService(recordplatform.DeploymentID, Registry,
    DeletionRecordSnapshotSource, DeletionWitnessSource,
    DeletionPreviewRepository, ServiceOptions) (*Service, error)
func NewDeletionWorker(DeletionWorkerRepository, DeletionLedger,
    DeletionEntryWitness, DeletionOnlinePurger,
    DeletionWorkerOptions) *DeletionWorker
func NewCoreAdapter(RecordCoreStore) (*CoreAdapter, error)
func NewRecoveryAdapter(RecoveryStore) (*RecoveryAdapter, error)

func NewPostgresRecordDeletionRepository(
    *pgxpool.Pool, AdmissionGate,
) *PostgresRecordDeletionRepository
```

- Production readiness 的 adapter 名称闭合集固定为 `record_core|record_attachments|record_evidence|record_markdown_client|record_search|record_activity_projection|record_comparison|record_collaboration|record_portability`，顺序同时参与 readiness/preview digest。
- `record_purge_operations` 只是可重建应用投影；primary ledger + full witness 才能证明 delete commit、`attempt_not_committed` 或 operation 不存在。

### 3. Contracts

以下 ledger/witness 服务与恢复条款继续约束既有 V1 路径；新 issuer 的备份披露合同单列于本节末尾，不将 V1 token 或历史 operation 重新解释为 V2 授权。

当前 C81 候选已接入受限 core/recovery 命令及不依赖 root 的通知事实、V2 outbox drain 与 collaboration handoff；永久删除 HTTP 继续关闭。隔离 store recovery 测试使用独立 primary/witness 数据库、生产 verifier 和同一存活 maintenance fence；V1 record、preview 与 fenced operation 在真实 C81 迁移前植入，并绑定该 fixture 随后激活的 deployment。该 fixture 不证明离线隔离 APP writer 或真实 source 首次账本写入，not-committed canonical 历史也不替代 deploy absence ceremony。ranked record mutation 的 legacy reservation 与 deletion-fence 行锁通过现有 `record_mutation_rows_lock` 的精确 project/record/reservation 分支取得，继续要求 direct LOGIN、READ COMMITTED 与先持 comparison fence；命令不创建或修改行，也不恢复 runtime DML 权限，缺失行不授予 mutation 权限。通知 retained start/fact/completion/handoff、迟到报告和稳定 receipt 的具体边界见[协作通知合同](collaboration-notifications.md)。

C81 保留 primary-subject validator 的同一 identity、函数体与两个 AFTER ROW、DEFERRABLE INITIALLY DEFERRED constraint triggers，只将执行权限切为 migrator-owned SECURITY DEFINER、固定 `search_path=pg_catalog`。subject→revision→root 仍在同一事务删除；内容 SELECT 与 validator EXEC 不授予 admin/worker。缺 primary 或删除 surviving revision 唯一 primary 仍在 COMMIT 拒绝，真实 receipt CHECK 失败须整体回滚 effects/audit/cursor。current catalog 逐项核函数 owner/security/config/ACL、trigger 事件/attachment/args/when/enabled/deferral 以及查询表 owner 和关闭 RLS；任何漂移拒绝准入与收敛，不能靠提前触发、禁用约束或扩大内容读权绕过。

通用 serving lease 的 `record_serving_deletion_fence_lock(bytea)` 仅授 direct runtime EXEC，输入精确为 `{project_id,object_kind,object_id}`，沿原 ObjectRef 校验。命令要求 READ COMMITTED 与 applicable source admission，按 epoch→existing fence 行锁顺序执行，取得全部锁后用 fresh DB clock 判断 live；不取 comparison advisory fence、不恢复 runtime 原始 UPDATE。无 epoch 拒绝；无 fence 返回零行，由原 Go helper 保持 generation0/live=false 的分类。reservation fencing、content lease 与 serving acquire/renew 共用该受限入口，保留原 owner/expiry/token/revocation 语义。

V1 worker 的永久 fence 传播检查通过 worker-only `public.record_deletion_permanent_fence_applied_v1(bytea)` 继续既有历史 operation，不重新开放 V1 issuer。输入为原十字段 continuation envelope：`operation_id,reservation_id,project_id,record_id,fence_epoch,owner_id,owner_generation,owner_expires_at,ledger_sequence,ledger_entry_hash`。同一 admitted READ COMMITTED transaction 内复用 direct LOGIN、worker/source admission、原锁序、exact owner tuple 和 independently verified entry；operation 必须为 `fence_propagating`、reservation 为 `committed`，persisted 与 verified entry 的 `delete_commit` 类型、sequence/hash/witness 全等。身份、状态、ledger 或 stale owner 以 `42501` 拒绝；只有同 object 的 live deletion fence 或 content lease（沿用 `transaction_timestamp()` 判断）使结果为 `false,nil`。取得全部锁后仍须以 fresh `clock_timestamp()` 重核 owner expiry/tuple，才返回 bool。`false` 表示继续等待，不能进入 retry-required；`true` 允许原 worker 继续推进 read-fenced。检查不修改 operation、reservation、epoch 或 audit，不授予 worker 原表 SELECT，也不借用要求 online-purging 的十二字段 adapter authority。

V1 continuation 回归从预置历史 reservation/operation 和真实历史 `fenced` audit 开始，以独立 LOGIN worker 及真实独立 ledger/witness 校验续接；不声称已关闭的 V1 issuer 仍可创建这些事实。core/delivery-epoch 终态须由完整九 adapter registry 的实际 receipts 与 aggregate 证明；缺任一必要 receipt 的直接 CompleteOnlinePurge 必须拒绝且保留 owner/epoch。not-committed 的 absence-proof test double 只证明应用恢复边界，不能替代真实 source absence。V2 issuer 分别证明旧 preview 在真实 revision 漂移后 pre-write 拒绝且零持久 residue，以及真实 issue 已在原事务写入后因 Commit sentinel 整体回滚、正常 outcome readback 无 operation，同一合法请求恢复后仍可成功。

历史 V1 的 markdown、comparison、collaboration 与 portability 使用独立 worker-only
continuation 命令，authority 始终为 caller 原十二字段 legacy adapter envelope。
`legacy_adapter_lock` 的闭合 surface 集按九 adapter digest 校验，保留 direct LOGIN、
READ COMMITTED、source admission、verified delete_commit、committed reservation、锁序与
fresh exact-owner 检查，不要求或制造 V2 gate。十字段 fence bool 只读，不授权 adapter purge。
client metadata 与 outbox 共享既有效果算法，但每处 owner 重核按版本独立执行；V1 首次真实
metadata purge 原子发布 exact browser event，verify 缺 event 不补写。root-independent
event/target/ack 和真实 start/fact/completion/handoff 的边界见[协作通知合同](collaboration-notifications.md)。

V1 portability 在任何逻辑身份消失前，先以四张 immutable 控制表保存 preparation、物理
targets、每条 export/import artifact 的原始字段，以及每条相关 write obligation 的完整
`to_jsonb` snapshot。发现范围包括 obligation-only、root-independent、record target 关联和
共享 import plans/jobs/mappings；不能仅从 artifact 枚举 key。owners 固定为 export_jobs、
import_jobs、import_plans、mappings、origins 五组按 C 字节序排序的原 tuple。
target 按 operation/backend/key/exact version/namespace/incarnation/storage binding 合并，
原始非 canonical key、NULL 和冲突值均保留；hash/size 冲突不选择任一原值充当已知身份。
inventory digest 使用 `HOUFENG-PORTABILITY-INVENTORY-V1` framing，绑定 operation、record、
surface、ledger domain/sequence/hash/witness、owners、removed_rows、pdf_pending 和有序
artifact/obligation/target immutable 列（含原始物理 created_at），排除 mutable claim、prepared_at
和 receipt；每次重验从保存列独立重算，不能从已删除的 source 重建或重写 snapshot。
frame 中 artifact 的 created_at 与 target 的 blob_created_at 统一使用
`to_jsonb(value AT TIME ZONE 'UTC')`：无时区的 ISO timestamp scalar 在本合同中明确表示 UTC，
NULL 保持 NULL；预计算和保存列重算采用同一编码，不受 session TimeZone 影响。
完整 obligation snapshot 按捕获的 JSON 原值参与 digest，不重写其中 timestamp 字符串。
历史 source 的非有限 timestamp 也按原值保存，相关 target 为 unresolved；不替换为当前时间或
NULL，不删 source 或建可 dispatch task。物理 eligibility、claim 与 completion 仍要求有限时间。

`record_portability_purge_prepare_v1(bytea)` 的外层精确为 authority、storage_binding 两键。
它锁定完整发现范围并复读 route/identity，在同一事务持久 inventory、移交义务、写 origin
tombstone、删除本 record 的逻辑关系并注册 exact local tasks；预先计算和实际 removed rows
必须相同。unknown identity 或 PDF workspace 只保存完整 inventory，removed_rows 为零，
不删 source、不建可 dispatch task。projection 精确包含 inventory_digest、unresolved_count、
required_backends、local_targets、s3_targets；fresh known prepare 在 COMMIT 前由 Go 核所需
backend 与 local binding，缺配置整体 rollback。空 inventory 是完整正常路径；unknown 和
replay 不以当前配置回填历史身份。unknown 永久 pending，返回 `ErrDeletionSafetyUnavailable`，
不发 I/O、不签终态 receipt，也不自动 adopt、清空或替换 manifest。
prepare COMMIT 结果未知时，以原输入和 caller owner 重读既存 immutable projection，核同一
inventory 与所需配置后才继续；不重做 logical effects，不把未确认 candidate 当作物理许可。
已有 preparation 只重验持久 inventory、handoff 与 remaining absence，不依赖已删除 source
重新发现。未知结果、重启或 takeover 均不能隐式采用新 owner、补 identity 或重复结算 quota。
只读回查入口为 worker-only `record_portability_purge_prepare_resolve_v1(bytea)`，输入仍精确为
authority/storage_binding，返回同一五键 projection；继续使用原 legacy authority 与锁序。
缺 preparation、manifest/handoff/binding 不符即拒绝，不能转调 fresh prepare、建 task、刷新
claim/dispatch 或补 receipt/event；未确认 candidate 仍不构成 I/O 许可。

target disposition 仅 local、s3、retained_shared、unresolved。local 必须有真实已准入的
namespace/incarnation、obligation/provenance 与原 `local-v2-<obligation_id>` 身份；关闭但未
materialize 的 obligation-only target 仍由 bind_final 独立证明存在/不存在，不预判 absence。
legacy local-v1、NULL namespace、缺 obligation、冲突 expected final version 均 unresolved。
S3 必须是 canonical SHA256 key、真实非空且非 `null` 的 exact version、匹配 digest/size，
相关 writer/remote 责任已结清，并绑定可信 APP 配置的 endpoint+bucket routing fingerprint。
缺 version、非 canonical、remote_unknown 或配置连续性/能力 closure 不足均 unresolved；
routing fingerprint 只证明该配置路由连续性，不证明 bucket incarnation 或未知 provider 的 D3。

新的 S3 write obligation 在真实 configured `ManagedBlobWriter` 登记时保存 nullable immutable
`storage_binding`，仅 S3 可为 32 bytes；local 保持 NULL。fresh S3 registration 的唯一精确
JSON shape 必含 lowercase 64-hex binding，local 对应值为 null，65536-byte 预算不变。
writer 从同一个实际 blob/temporary store 派生并固定 identity，在登记前、dispatch/I/O 前及
返回后核验；request/HTTP caller 不提供该值。配置或能力漂移不得返回成功 WrittenBlob，
真实已返回 version 作为事实保留，并沿原 S3 remote_unknown 责任记录不确定结果。
registration replay、dispatch/return readback、完整 discovery/recheck 和 artifact publication
均核原不可变 binding、writer/principal/origin/dispatch 与 exact physical tuple；artifact 通过
原 obligation FK 保留来源，不在 publication、late return、handoff 或 cleanup 回填历史 NULL。
SQL 认证受信 APP writer 与持久关联，不独立证明进程实际使用的网络客户端。

known S3 inventory 的 binding 来自已登记 obligation；artifact 必须以该 obligation 的真实
成功 dispatch/return 与 origin、key/version/hash/size 关联证明，obligation-only 同样可成立。
prepare 当前 fingerprint 只与已持久 source binding 核兼容；可信 source 已知但配置缺失或
不符时 fresh prepare 整体 rollback，source provenance 缺失则保存完整 unresolved inventory。
已有 source NULL、remote_unknown、冲突返回或未结 writer 责任不因新字段成为 known；原
ordinary/local GC wire、cleanup hash 和 V2 inventory framing 不变，不改历史 migration/库存。

retained_shared 必须有真实仍存的外部 record/job/artifact/blob accounting owner 和 durable
清理责任，证据为按 kind/id 排序的 `[kind,id,identity_digest]`，绑定完整物理 tuple 与 route。
lease/pin 的时间、未结 writer 或两个删除 operation 相互引用不能代替 handoff。共享 import
只移除本 record mapping，保留其他 owner 的 job/plans/artifacts。最后 owner 移除必须在同一
事务先将同一 exact identity 交给 V1/V2 portability manifest、generic local/S3 GC task 或真实
completed physical receipt；之后 replay 可核该 handoff/删除证明，不将 retention 称为 absence。
reference guard 覆盖 portable jobs/artifacts/plans/mappings、attachments/upload parts/pins/blob
objects、publication intents 与 write obligations/targets；沿同一 parent/job 锁序串行化新增引用
和 prepare。writer 先提交则纳入 inventory，prepare 先提交则拒绝 exclusive exact target 的
新 ownership；不阻挡 append-only provider 返回事实、其他 S3 version 或合法新 local incarnation。
移除最后 retained owner 同样核 durable handoff，V1 自身逻辑删除只能消费 exact transaction-local
portability effect scope 和 manifest membership，不能按 session_user 旁路。
import archive 可先于 plan 注册，原 obligation 的 NULL plan_digest 不回填。target handoff
须在真实 plan 删除前，验证 captured archive 的原 job、owners 中的 plan/job tuple 和锁定
plan 的实际 digest 与 target 一致；snapshot 若已有 digest 也必须一致。不能用两处 NULL
或已消失 plan 代替该证明。

live owner 的完整 digest 在 BEFORE removal/ownership-changing UPDATE 时，以仍存的 OLD 与
parent route 校验。源行消失后的 alternate handoff 改由 target 原始 captured obligation、
`local-v2-<原 obligation_id>`、真实 sealed GC link、不可变 preparation/target 与原 command/
receipt 证明同一物理责任；不能以相同 tuple/task ID 替代 source linkage，也不声称重建已消失
attachment/upload-part 的 digest preimage。pending task 只证明后续清理责任，不能证明 absence。
已验证 original source/provenance 的 closed local obligation 即使 actual version 为 NULL，
identity matching 仍可使用其唯一 expected `local-v2-<原 obligation_id>`，以区分旧版本重放与
同 key 的新来源；不回填 actual version 或预判 absence。未知、冲突、legacy 或未封闭来源
不能据此补造 expected identity，继续按原保守匹配阻止替代授权。
V1 local claim 只 link/seal 本次保存的 target membership。同 key 的未捕获历史仅在原始 source
证明唯一 expected identity、该 identity 与本次 target 不同，且原 exact sealed task/receipt
证明物理清理已完成时才可排除；actual version 为 NULL 的已完成来源也适用，NULL 保持原样。
旧 final 曾存在时，task 的已绑定版本须等于该已认证身份；旧 final 缺失时，task 版本须为 NULL。
这两个完成分支均不得用来源的 actual NULL 代替身份比较或跳过原物理完成证明。
未完成、身份未知、冲突或完成证明不完整的未捕获来源仍拒绝 claim，不能挪入新 task。

source guard 的 13 表集合之外，attachment_uploads、attachment_processor_jobs 与 record_drafts
仅对影响上述 child proof 的 parent route UPDATE/DELETE 增加 dependency guard；lease/retry/
普通 state/expiry bookkeeping 保持原义。guard 以 default-project quota 行作共同序列化锚，
按 blob/upload/parent/source 锁序取得锁，再 fresh READ COMMITTED 重读；向更早序取锁必须
NOWAIT，竞争以 `40001` 整体回滚。live 查询为 VOLATILE，不能只锁已发现 owner 来证明空集合。

ownership removal 另有 DEFERRABLE INITIALLY DEFERRED AFTER DELETE/UPDATE constraint check，
在执行时核最终 surviving owner 或完整 exact successor/receipt；条件在函数内判断，不用事件时
求值的 WHEN。父行消失不能跳过旧 target，task/link/obligation/provenance 后续失效同样触发
重验；GC task 与 sealed-link 的 INSERT/UPDATE/DELETE 也维护该责任连续性。批量或并发删除
不能让两个已删除 owner 相互作 survivor。调用者提前 `SET CONSTRAINTS ... IMMEDIATE` 可因
中间态不完整被安全拒绝，命令不代改 constraint mode，后续变动不能复用早先成功作为豁免。

private portability GC barrier 在每个匹配的 V1、普通、V2 local authority、claim/complete、
identity guard 与 attachment V1 S3 claim 授权边界独立核整个 manifest eligibility 和 digest。
unknown namespace/version 对同 backend/key 保守匹配，不能把 NULL 当作不匹配；不同已知
namespace/incarnation 或 S3 version 不混同。exclusive eligible target 只允许其注册 task，
retained target 只允许真实 owner 或 durable-handoff task；整个 manifest 尚 unresolved/pdf 时，
任何替代路径均不能获得 dispatch/receipt。最终 complete 也独立重核，不能仅信 coordinator。

旧 attachment/ordinary S3 GC 行与原 wire 没有可信历史 storage binding；该 route 为 unknown，
不能用当前配置回填或据此排除相同 backend/key/兼容 version 的保护。六参数 barrier 对这些
潜在同一身份保守匹配；每个匹配 manifest 都必须通过 eligibility 和真实 task/owner/handoff
校验，否则 `55000` 且不授 I/O。已知不同 backend/key/version 或 local namespace/incarnation
不受该 target 阻挡。新 V1 private/reference 协议具有可信 binding 时仍精确区分 route，并独立
核 binding；barrier 成功只解除这一层 blocker，不替代其他物理授权或证明历史 route 连续性。

local physical cleanup 复用同一个 immutable LocalBlobStore 和既有五方法 repository 协议。
`LocalCleanupCoordinator.WithRepository` 拒绝 nil/typed nil，不重开或改变 namespace；V1
repository 每事务重新发送 caller 原 target authority，不从数据库采用新 owner。local common
函数第三参数为 legacy authority，NULL 保留原 ordinary/V2 路径；非 NULL 只在 purge=true 时
接受，并逐次校验 manifest、exact registered task、namespace/incarnation/key/version、dispatch、
writer/anchor/provenance、guard/final inode/inventory/reference/quota。五个 V1 local wrapper
外层精确为 authority/request，保留原 flat request bytes、262144-byte nested 预算和
`HOUFENG-LOCAL-GC-V2` completion hash。physical local_contract_version=2 不改变 operation
deletion_contract_version=1；resolve 只读，不刷新 dispatch 或授权。

private `local_blob_gc_authority(jsonb,boolean,bytea,boolean)` 的第四参 preparing 仅由
`local_blob_gc_prepare` 的三次 authority 调用显式传 true；其他 current/claim/resolve/bind/
complete 调用均传 false，无 DEFAULT/旧 overload。两种模式都保留原 LOGIN、namespace、
operation/caller 与 fresh owner 校验。preparation-only barrier 只允许符合原 authority、
manifest/source 与 candidate identity 的 pending task 登记，不适用于 dispatch、claim 或 receipt。
普通 prepare 只按原 sealing 条件提前建立 closed-origin source link；V2 portability 在原
operation assignment 与 immutable preparation 完成后、返回前建立真实 link，不能 seal 共享
保留 source。claim 先建立/重验真实 link 再转 claimed，true seal 不降级。最终责任检查仍要求
实际 preparation/target/source operation/link 完整；公共 request bytes、cleanup hash 不变。

S3 portability 使用 targets 中独立的 pending/claimed/retry_wait/completed 状态，不写 attachment
GC pool、不重复扣 attachment quota。claim、claim_resolve、complete、retry、resolve 均经独立
worker-only 命令；claim 固定一分钟，首次 generation/attempt=1，合法到期接管分别递增，
同 live owner 返回原 tuple，其他 live owner 不接管。exact claim COMMIT 确认后才在事务外
`DeleteGCVersion`；响应未知先以只读 claim_resolve 核原 tuple，不用再次 claim 延长许可。
receipt 精确绑定 key/version/SHA/size/deleted/storage_binding，以
`houfeng.record-portability.blob-gc-receipt.v1` framing 保存 deleted/already_absent。complete/retry
同时核 operation 与 cleanup owner live；completion resolve 只确认同 receipt，completed 不可变。
删除失败按原 DefaultBlobGCRetryDelay 保留 retry_wait，旧 owner 只保留已确认 dispatch 的
exact-version I/O 责任，不能续期、创建 dispatch 或完成 SQL。

`ConfigurePortabilityDeletionBlobStore` 只绑定非 nil/typed-nil S3 deleter，且必须实现
`BlobGCStorageIdentity.GCStorageIdentity() ([32]byte,error)`；同实例幂等、异实例拒绝。
真实 S3 fingerprint 为 SHA256(length-framed `houfeng.s3-gc-storage-route.v1`、实际 client
EndpointURL().String()、bucket)，不含秘密、不联网。bootstrap 绑定同一已配置 attachment
S3 store。restart 和每次 I/O 前后核 immutable fingerprint，漂移或配置丢失保持 pending、
零新 I/O，不能换 route 或清 inventory；测试包装也必须转发真实 store 的身份。

V1 portability 最后 purge/verify 只核已经提交的 inventory/handoff/absence 与 exact physical
proof，不再次删除逻辑 source。local 每 target 均要求 registered GC、completion command、
inode/inventory/obligation；private S3 每 target 独立核 exact claim 和 domain receipt；retention
核 surviving owner 或 durable successor。receipt 保留原
`houfeng.record-portability.deletion-receipt.v1` framing、manifest removed_rows 和首次真实
verified time，重放稳定、不重复 I/O/quota；verify 缺 proof/receipt 为 `55000`，不能补 prepare。
九 adapter terminal oracle 同时要求 browser event、manifest/handoff、真实九 logical receipts
和 actual aggregate。generic operation GC 与 attachment 原 S3/local-v1、portable local-v2 的
归属使用 set equality：missing、duplicate、orphan 均拒绝。只有 online_purging/live owner
可 mutation/complete；online_purged 不因缺 manifest 获得弱校验例外。历史 terminal status/
recovery 仍按原只读合同读取，不补造新证明；新 adapter verify/resolve 只接受已有完整新证明。
pending-obligation 校验仅对 exact captured source 接受等价证明：其 eligible target 已有完整
private S3 completion，或 retained_shared 已验证真实 surviving owner/durable successor。
原 obligation 的 cleanup_completed_at 可继续为 NULL，不为通过终态而补完成字段或结算 quota；
未捕获、关联矛盾、open/remote_unknown 等其他未闭合义务仍阻止完成。


V1 S3 purge 的 prepare/verify 与 claim/complete/retry/resolve 只经六个 worker-only SECURITY DEFINER 命令。authority 为 caller 提交的原 12 字段 owner/operation/ledger/witness envelope，由既有 private `legacy_adapter_lock` 的 `record_attachments` 分支复核 independently installed entry、reservation/epoch/fence 与 applicable source admission；不从数据库补造 caller owner。prepare 在同一事务移除逻辑依赖、结算 logical quota、保留共享引用并写稳定三 surface receipt 和 exclusive exact-version durable claim。claim 从 INSERT 绑定不可变 `operation_id`、V1/S3 身份，不以可变 cleanup owner 充当 operation 索引；ordinary raw GC 不处理这些受保护行。GC 命令分别严格解析 `{authority,deletion_id,cleanup_owner_id,cleanup_lease_ms}`、`{authority,claim,receipt}` 或 `{authority,claim,retry_at}`，拒绝重复/未知字段、错误 scalar 类型、非微秒时间及不匹配 tuple。claim COMMIT 确认后才执行原 `DeleteGCVersion`，网络 I/O 不持 SQL 锁；COMMIT unknown 按原 ID 解析，不能盲目重发。已确认 dispatch 的 exact-version 删除权不被 lease 过期或 takeover 撤回，迟到旧 tuple 不得完成新 claim；complete 校验真实 deleted/already_absent receipt 并一次结算 physical quota，resolve 只读、不授新 I/O。active publication/write obligation 保持 pending；V2/local 与 ordinary GC 继续使用各自原合同，不能把 S3 重贴为 local。实际 source、remote capability 与完整 G2 验收分别保留。

初次 V1 preparation 的私有返回含 exact `required_backends`，由本事务枚举的全部对象生成，包含仍由其他 record 保留的共享对象。coordinator 在 COMMIT 前核实每个 backend 已配置；缺配置时目标依赖、quota、claim 与 receipt 一起 rollback。空对象集合不要求 S3 配置。该字段只供首次提交前检查，现有 preparation 的 replay/verify 返回空数组；unknown COMMIT 仍按原稳定 counts 与 exact claims 解析，不把临时配置检查结果当作新的物理删除权限。

隔离 C80 验收使用 `record-deletion legacy-c80-service --request-file <private-path>`，只接受 UID/GID10002、固定 FD5 的存活 root 监督通道、原 APP 与 guard/phase 围栏。该入口先禁用 coordinator dumpability 再读取私有请求；普通维护入口仍只接受 quiescent。原 callback 重入须先撤销凭据、收束原 child/backend 并取得存活 root 的连续性观察，才能按原 token commitment → reservation → operation 解析结果，不按 record/actor 模糊查找。preview-only 保留 abandoned 行自然过期；冲突记 unresolved 并保持围栏。APP 已提交时仅返回 `recovered_committed/<原 operation>/reserve_committed`，不宣称收到原 child 成功帧或观察到同 token replay。

C80 wrapper 的实际 Reserve 成功必须先取得持久 `reserve_result_ack`；唯一 stdout 行还必须以其原字节摘要取得 `result_written_ack`，随后才关闭 runtime session。父端独立排空 stdout/stderr/IPC，直接 wait、核 pidfd/EOF、refence 后才交付正常结果；维护入口、重入准备、backend 排空、callback 和正常清理共用入口建立的 60 秒截止时间，不在恢复后续期。CLI 输出仅含 `version/status/operation_id/reason`；不能形成可信业务结果时输出受限 unavailable 帧并 exit1，只对已经完整输出该帧的错误抑制通用日志。传输与 CLI 检查不代表 guest 权限、退出停点、same-APP source/handoff 或完整 G2 验收已经通过。

C80/C81 callback 的工作窗口在原始入口截止时间内预留 15 秒用于 refence、backend/child 收束及最终管理员清理；重入和嵌套调用保留更早的截止时间。清理可脱离工作窗口的取消信号，但不能续期开启新的后台超时。预算不足或已过期时返回失败并保留未解析的 intent/围栏，不报告凭据已撤销或进程已收束。

父端在 `predecessor-result-written-<phase>.json` 之前，先不可变发布并 fsync `predecessor-stdout-<phase>.json` 及其目录项。载体精确包含 `version/phase_generation/recovery_id/application/realm_digest/result_digest/stdout_bytes`，最后一项为原始完整 stdout 字节（含 LF）的标准 base64；载体上限 32KiB，解码帧与全部 IPC 帧仍限 16KiB。root 与 host 各自解码、计算摘要、严格解析五字段并核原调用绑定，同时保留两文件的原字节摘要。已有 result-written 而载体缺失或不匹配必须拒绝，即使未单独见证过载体；仅有载体属于可解析原 APP 效果的中断前缀，不能补造收据或 ACK。持久证据不证明 ACK 已送达或 child 正常退出；恢复结果不宣称观察到同 token replay。

隔离 source 故障验收通过固定 FD7 的 credentialled `SOCK_SEQPACKET` 与直接 root 父进程握手，绑定原 operation、phase、nonce 和实际收据摘要。APP、W1/W2 停点只能在真实提交之后触发；W1/W2 还须在提交前确认实际写事务的 backend 与预先登记的 PID/start/role 一致，提交后观察失败仍由原 pending 请求负责解析，精确重放不伪装为新提交。`anchor-fsync` 位于新 anchor 文件及目录 fsync 之后、source-anchored 收据发布之前，root/host 必须见证该 anchor 的原字节与前驱链。handoff-install 与 handoff-return 分别位于实际安装命令提交后和安装收据持久发布后。未配置 observer 的账本调用不增加 backend 查询。

same-APP C81 的隔离候选使用固定 migrator 与原 RID，保留原 inventory，通过独立 successor inventory 和 activation 记录选择新角色状态。普通入口不得接纳未完成的 begin、migration、observation 或 refencing 前缀。FD5 的 `maintenance_checkpoint` 与 `realm_observe` 分开：root 从受保护文件原字节计算摘要，将 ACK 写入独立 maintenance 见证链；它不授予或恢复 no-copy realm 能力，也不证明 SQL 已提交。begin 绑定原 session/inventory/credential 与完整受保护目录库存；intent/preparation/ready/normal 和 phase 的持久化与 root ACK 分别核验。ready ACK 仍要求活 child 的实际 executable、身份、scope 与真实 backend 观察，不能以安装摘要或 PID 消失替代。

COMMIT 后原 normal 未持久化时，恢复先结清原 child/backend 并收紧角色，再用同一 READ ONLY REPEATABLE READ 事务检查 manifest、catalog、principal 和完整角色快照，区分 exact C80 compensation 与 exact C81。C81 的新观察使用独立 `migration-observe-{intent|normal|restrict|settled}-<generation>-<attempt>`，每次 attempt 使用不同 fresh verifier；临时 secret 不交给业务连接。grant ACK 之前不能提交授权，normal 必须在 grant 提交后新开一致快照取得；无法观察的 attempt 先 refence，再以新 attempt 继续，不重放旧 grant。若 restriction 后最终状态无法区分 grant 是否曾提交，只能记 `refenced_without_normal`，不能宣称 `grant_not_committed`。activation 的 `normal_source` 与 `normal_attempt` 选择唯一已 settled normal，不按目录最新记录选择。root 丢失时取消工作，限制性清理继续受原总截止时间约束，紧急目标关闭管理员 LOGIN 和 CONNECT。

Begin publication 之前即建立 pending 责任；发布或 ACK 不确定时，outer teardown 保留原 journal-bound 角色，不用通用管理员禁用改变无效果前缀。候选新增 `migration-begin-settled-<generation>.json`：只允许原 C80、worker absent、原 fenced catalog/完整角色全等的同事务观察，取得 live root ACK 后才发布 refencing 和 quiescent terminal，再以新 generation 开始迁移。原 begin、session、inventory 和 credential bytes 不改；结算进程 provenance 与当前调用 backend 分开绑定。管理连接在整个 migration invocation 内不得建立第二次物理连接；root 的独立 settlement observer 必须实际 COMMIT 并关闭连接，Go 在仍持有的管理连接上确认其 exact backend 已消失才推进。历史 terminal 和目录连续性逐代保留，不以最终 activation 掩盖旧责任。

替代 coordinator 通过独立 `maintenance_ready_disposition` 请求同一 continuous root 对原 ready 原始摘要作认证判定。判定先写入同一 maintenance 全局链，再返回 `witnessed` 或 `unwitnessed_retired`；前者引用原 ready witness 和保留的 live observation，后者只允许原 scope 已实测退休且无后续授权记录的限制性结算。通道错误不是判定；root 死亡不允许重建本轮能力。ready publication、root ACK 和发给 child 的 ACK 分开记录，未确认 root ACK 的原调用只取消/join、执行预授权紧急收紧并保留 pending，不提前发布 refencing。replacement 仅在真实判定及相应 settlement/abandonment 前置完成后重放原 phase。清理未完成时保留 unresolved，不绕过 root 前缀拒绝。

公开 `maintenance_ready_records` 仅导出判定所引用、经严格无秘密 schema 核验的原 ready bytes，按 generation/name/hash/base64 精确绑定且计入整体输出预算。host 独立核原字节、全局链、判定引用、实际 scope retirement 和 settlement；未见证 ready 的 generation 与原 coordinator 关联由 root 从私有 begin 与 registration 核验，公开字段不提供独立重建该关联的证据。systemctl 的 Names、DropInPaths 与依赖数组按固定 formatter quoting 解码，保留字面反斜杠转义的 unit name，拒绝重复元素及畸形输入，guest 与 host 使用同一语义。

隔离验收载体每个 case 只导出一次只读 `cidata.raw`，首次启动和 reboot 绑定同一 held FD、named inode、全盘 hash 与 QMP raw backend；独立 host 导出相等不构成 guest 整盘相等的证据。pregenesis systemd 必须关闭全部非允许 activation routes、实例和 jobs；tmpfiles 只允许经过实际 effective stop 语义检查的固定 runtime override。停止失败保留第一份私有原始诊断，公开结果仅含受限 metadata 和摘要。这些 fixture 行为不扩大普通服务权限。

隔离验收的固定 `realm-genesis.json` 使用独立 512KiB 持久容量，保留完整 canonical 原字节及 systemd closure；Python 与 live Go launcher 的固定名称读写入口共同核验该上限。通用 control 文件、APP binding 与 IPC 的 16KiB 上限不变。文件发布仍要求排他 nofollow 创建、root:root/0600/单链接、file 与目录 fsync、身份和完整原字节读回。host 独立核完整 genesis 容量、realm commitment 与首条 genesis 事件的原字节 SHA256，并拒绝后续重复 genesis 事件。restart 独立 body 的 1MiB 子预算保持；source 的完整 result、证据及 protocol/serial 分别按下述固定预算核验。文件可发布不代表整体证据可传输，超限必须保留失败而不截断或提高其他上限。容量回归不代替真实 source、root restart/kill、同盘 reboot 或完整 G2 验收。

隔离 PostgreSQL 初始化的 held pidfd 退出与 Docker 状态结算分别证明：pidfd 可读后，向原容器 ID 发起一次认证 `wait?condition=not-running`，要求 HTTP 200、整数退出码 0 和无错误，再独立 inspect 原配置与 `Running=false/ExitCode=0/Pid=0`。wait 和 inspect 继承原初始化截止，不重试或延长预算；原 PID/starttime 与双 held pidfd 的 join 检查保留。Docker wait 是 daemon 报告的容器退出事实，不是 supervisor 对 initdb 的 direct-parent wait。

隔离 guest 的 BPF 效果前授权候选仍不可用于 source/C81 准入。固定原镜像缺少两个 allocation 函数符号及精确 `bpf_audit_prog` 名称，但实际存在 `bpf_audit_prog.part.0`；不能把精确名称缺失扩大为所有 split body 均缺失。已批准的替换绑定使用同一原镜像的 System.map、ELF、BTF、配置、headers 与 Module.symvers，生成相对实际 `idr_alloc_cyclic` 的固定调用点和布局；publication 使用返回的 signed ID，audit stamp 在实际 buffer 存活时读取。原输入生成器、CO-RE 字节与原 headers 下模块编译通过，只证明静态输入和编译关系，不证明 guest probe 注册、module seal 或效果前授权已通过。

固定内核在 ID publication 之后才执行 `bpf_map_new_fd` 或 `bpf_prog_new_fd`；FD 创建失败时可在 syscall 返回前移除已发布对象。隔离 source 候选使用 guard ABI v4，保留 192-byte record 和 240-byte STATUS，在 publication 的原存活回调内复制 64-byte object snapshot；program 的最终类型、名称、tag、load time、creator UID、translated/verified instruction count 与 attach BTF，以及 map 的实际尺寸、flags、BTF object/type IDs 和 map_extra 均来自对象，不由请求或默认值补造。对象先按该 snapshot 建立 incarnation；存在 held FD 时另做真实 `OBJ_GET_INFO_BY_FD` 并交叉核对，不存在 FD 时保留真实 errno、publication/removal 与清理责任，不伪造 OBJ_INFO。固定双 map ID 复用只在 owned VM 的 selftest 中绑定真实首次 removal 与下一事务，在固定 allocator 调用点持原锁时保存并恢复 cursor，负返回也须恢复；两次真实 publication 的数值 ID 相同且 cookie 不同，不改写或补造 ID。binding、运行效果与完整重放仍须验收，不能仅凭 ABI 或编译证明。

PID1 在 `finit_module` 前校验 sealed module 中唯一、只读的 `.g2_observer_binding`。当前生成合同的 `allocator_windows` 为必填：按原顺序核两个固定 allocator 指令窗、原 bytes/relocations，以及相对唯一 owner symbol 的 signed delta；IDR size/base/next 与 spinlock size 固定为 24/16/20/4。Go 消费端、生成器与 native 使用同一完整字段合同，继续要求模块 section 摘要、实际 running build-ID 与 BTF 身份。host 的纯元数据/实际 module section 检查不能代替 guest kernel 校验或完整校准。原 isolated PID1 可在启动失败时额外尝试写出一条最多 128-byte 的固定阶段诊断，只含真实观察到的 errno 或 CHECK/OTHER，不输出任意错误文本、输入、路径或凭据；诊断不改变原返回错误、退出或权限，底层 operation errno 与 composite check 的 protocol 返回分别表述。继承 stderr 的 size bound 不保证写入时长或交付。

同一隔离启动链的 audit reader 与 terminal observer 分别使用固定角色 R/O，遵循上述单次失败和继承描述符规则；broker 在 handoff、audit、policy load、SEAL 与 transcript 结算处保留固定失败阶段。R/O 成功路径不写进度记录，诊断不导出控制帧或改变原有协议。

broker 在首次注册 ACK 后仍按原路径关闭自有 console；后续 runtime 拒绝只在内存锁存首次实际失败阶段及错误，待原有清理与 revoke 路径返回后，在 main 原有终态错误输出点发布。此时只借用已继承的 stderr，不重开或保留 console、不修改描述符 flags、不关闭借用 FD，也不重置原记录上限和失败锁存。reader 的后续 poll、原 broker pidfd、boot-ID 和请求拒绝同样保留实际分支；诊断区分真实 pidfd POLLIN 与其它事件，不以串口写入顺序证明 actor 退出先后。Python pinned-process 检查保留原四个条件的短路顺序、系统调用与异常，仅将各拒绝分支分到独立源码行，由现有脱敏 traceback 区分，不输出原始身份或 payload。

control dispatcher 的每次调用使用新的私有小值观察：O/C 仅按同一次已解析请求分类 operation 与 BIND command；P 保留首个实际参与返回的失败边界，R 只随实际替换返回错误的边界更新，D 仅表示本 dispatcher 持有 task 的 drop 未尝试、成功或失败。已有错误优先于 deferred drop 错误，后者仍保留失败事实；原成功、denied ACK 成功和 pending/nil 在全部既有 defer 后清空观察。权限、业务决定、原错误对象、pending、rights/task 收束与 defer 次序保持，不记录被忽略的辅助错误或成功阶段。

BIND handler 仅在实际非 nil 错误返回点记录固定 B 标签，入口九项保持原短路顺序；复合谓词及 callee 内部仍聚合，不补造被折叠的 errno。可选观察在入场清零并读取当时 checkpoint，在原 owner-create sender、task-drop 与 pidfd-close defer 完成后再读取当时的 checkpoint；exit 重新取得当前指针，原被忽略的清理错误不改为返回错误。S/E 分别区分 MISSING、CLEAN、INVALID、FAULTED、INVALID_FAULTED；成功或恢复为 nil 时清空整值。immediate dispatcher 传递嵌套值，queued-start 固定传 nil；观察不改变授权、错误对象、FD 归属或结算。

DELETE 授权 callee 仅在本次实际采用的首个非 nil 拒绝处记录闭合 A 标签；入口同时清空工作观察的 deletePoint 与 targetPoint，group 缺失或已退休及 actor-add 成功保持零值。入口、target 与 held-runc 的原谓词顺序及错误折叠不变；heldRegularDigest 与 kernelDevice 仍在错误判断前连续无条件调用，原 map 遍历不排序、不复制，通用布尔 PIDFD 失败仍拒绝。nil sink 只省去诊断访问，不跳过业务检查；原 caller 的 markFault/B63、FD 归属与 defer 责任保持。A 的 helper 聚合标签不展开内部原因，不补造 errno，也不证明与 owner HTTP500 的关联；成功重用 working sink 不改变已按值捕获的失败槽。

已认证的 DELETE 在原 PIDFD、grant/group/enrollment/root/node/held-FD、精确 target 指针及 phase 2/3 检查后，若 target.offlineSequence、group.OfflineSeq、target.releaseSequence、group.ReleaseSeq 任一非零，仅接受 target offline 非零、两侧 offline 相等、两侧 release 相等且 release 为零或严格晚于 offline；矛盾仍为原 A12/errGuardProtocol。一致时只允许固定 DELETE 继续，不新增 rmdir actor/permit，不改变 registry、phase、checkpoint 或 FD，也不标记 group retired；可能已有的匹配 permit 仍按原 native LSM 规则使用。四字段全零时继续原 held-runc/prior-actor 授权路径，前置失败不能由 offline 分支吞掉。原 wrapper 在 BIND 前后保持固定 runc 的 held/named hash/stat 复核并执行原 held FD，独立 exec 重放不变。offline 不等于进程退出、release 或 retired ACK；正常 settlement 仍须实际更晚 release、permit 撤销/absence、tombstone/guard state 与全部 invocation、事务、对象、attachment 责任完成后退休。组件只证明 broker 状态判定与无新 permit，不能代替 native 事件、实际 runc 继续或成功 settlement；selection140 的历史 HTTP 因果及完整 source/G2 验收仍未证明，永久删除 HTTP 保持 CLOSED。

effectRegistryTarget 在同一 policy 实现中仅记录本次实际首个拒绝谓词的闭合 G 标签；原十六项短路顺序、两组之间唯一 targets lookup、原 errGuardProtocol 与返回 pointer 保持。helper 入场只清 targetPoint，DELETE 传本次 working sink，其余三个 consumer 显式传 nil 并执行相同 policy；成功 phase 0、1、2、3、5 等仍允许，仅原 phase==4 拒绝。G 与 A/B/S/E 随整个值清零并按值捕获，不能将 captured 槽作为 working sink。G 不证明状态形成的 actor、历史原因、native 身份或与 owner HTTP500 的关联，不新增 registry 权限、FD 操作、等待或重试。

只有原 loop 已选择终态 control 拒绝时，才在原 first-failure capture 内按值锁存观察与 T 路由，保留 join report 优先于 checkpoint fault/new invalid 的原分支。沿原终态单行发布固定 54-byte `BROKER_CONTROL_Oxx_Cxx_Pxx_Rxx_Dx_Tx_Bxx_Sx_Ex_Axx_Gxx`：O/C/P/R/B/A/G 为闭合枚举的两位大写十六进制，D 为 0–2，T 为 1–3，S/E 为 0–4，A 为 0–25，G 为 0–16；B=0 要求 S/E=0，非 BIND 的嵌套观察必须全零，A 非零还要求 O=BIND、C=DELETE、B=DELETE_AUTHORIZE，G 非零另要求 A=TARGET。不从 B63/A10 或实测 S1/E4 回填未知 A/G；十进制 G16 编码为 G10。非法组合只输出 INVALID_STAGE，errno 仍只来自原错误链。普通 capture 与终态发布后清空整个 control metadata，不保留调用方指针；仍受原 24-record/128-byte、一次写入及借用 FD 合同约束。独立 host 判读须绑定同候选原 serial 摘要，只接受唯一完整 LF 新格式行；旧无 G、无 A 或无 B/S/E suffix 格式不作兼容，raw 范围与交叉关系先于 bind=None 规范化校验。O/C 不认证 sender，P/R 不证明根因，D 不包括其它认证或拒绝 helper 的 task，T 不证明与 owner HTTP500 的关联或先后；S/E 仅为本次 handler 连同原 defer 的入场/返回状态，不定位首次 fault 来源或证明跨事件因果，B/A 的聚合 callee 标签不证明内部谓词，G 只定位本次 helper 实际采用的首谓词。不输出请求、身份、路径或错误文本，不增加 wire 字段、授权、等待或重试。组件及这些观察均不能替代真实 source/G2 验收。

reader-health 启动失败诊断在 client 内以私有小标量只保留首个实际拒绝分支，区分 nil/invalid、poll 前后 pidfd、boot-ID 读取或不一致、poll 调用、endpoint 与无 pending request 的 unsolicited packet；broker 只在原有返回错误的 capture 点选择固定 stage。reader 的坏 query-poll 分支按实际 ERR/HUP/NVAL 组合选择有限固定标识。原谓词、短路和系统调用顺序、错误返回、fail/failDetected 与已有检测证据不变，不消费 unsolicited frame，不将诊断作为授权或预期故障成功证据。仍只在原终态点、原 24-record/128-byte 有界输出中发布，不增加 console 写入、重开/持有或修改借用 FD，不输出原始事件、请求、身份、环境或错误字符串；不同进程记录的串口顺序不证明因果。固定标识只缩小后续真实探针范围，不能据此宣称 reader、启动、source 或 G2 已修复。

公共 pidfd selector 的 reader-health 细分观察只在原拒绝点采样，并且仅在 broker 诊断可用、client 与全局均未锁存首因时收集。原布尔入口与诊断入口共用唯一检查实现；fd/PID guard、两次 poll、一次有界 fdinfo 读取、精确 Pid 解析及原短路顺序不变。固定 suffix 为 `_BAD_FD`、`_ZERO_PID`、`_PRE_POLL_ERR`、`_PRE_POLL_XX`、`_FDINFO_READ`、`_PID_MISSING`、`_PID_DUP`、`_PID_PARSE`、`_PID_MISMATCH`、`_POST_POLL_ERR`、`_POST_POLL_XX`。XX 是两位大写十六进制的六位归一化事实：bit0=IN、bit1=NVAL、bit2=HUP、bit3=ERR、bit4=其它事件位、bit5=poll count 非零；组合完整保留，不是 raw revents。poll 返回 error 时只保留该原位 error，不解释 readiness；解析失败不保留 NumError、输入或任意错误文本。只有已有清理/revoke 返回后的原 capture 点能锁存并在原终态缓冲发布，观察 errno 不替换真实返回错误。base 与 suffix 合计仍限 64 字符，非法 reason、facts、base 或超长组合只输出 INVALID_STAGE；原 24-record/128-byte、FD ownership、控制/证据协议与权限边界不变。这些标识只证明本次实际分支，不能证明历史失败同因或 reader 已退出。

reader 的零超时健康 poll 返回 EINTR 时，只返回该次原错误，表示观察未完成；范围包括 query socket，以及 socket 前后两个 PIDFD selector 各自的 pre/post poll。observed selector 的第二返回值仅携带本次 poll 的原错误，与可选诊断 sink 是否启用无关；fdinfo open/read 的错误仍只进入原诊断观察，不进入此 error channel。通用 livePIDFD/pidfdSelects 布尔入口仍对任何失败返回 false，不重试或把 EINTR 当作活进程证明。reader 在 poll EINTR 上不 invalid、不复制 selector 观察、不锁存或刷新首个 reason，也不改变 request、deadline、检测状态或 FD。after-selector 中断后不解释此前 socket 的 event bits；socket poll 不消费消息，下一次完整观察仍须拒绝未消费的 HUP 或 unsolicited 数据。

普通 broker loop 在任何 guard/audit/join、death、事务/effect、checkpoint/ACK 或业务分派前跳过整轮，下一轮先检查原 guest/selftest deadline，再重新完整观察。已分类 reader event 通过唯一拒绝入口保留原 capture/revoke 与错误优先级；它与 owner-death 的 committed drain、committed selftest effect 三处仍对任何 health 错误永久 fault checkpoint，即使 reader 本身未 invalid，后续健康成功也不能恢复该 checkpoint。非 EINTR poll 错误、fdinfo EINTR 和其它 selector 拒绝仍沿原终态路径。连续中断不刷新 deadline、不产生新授权或结算，也不保证信号风暴下的公平性或有界进度。测试仅在 owned traced child 主线程精确注入 poll/openat 返回值，并为 loop 的成功 raw CLOCK_MONOTONIC 输出使用固定偏移；它证明真实方法、周围 I/O、未消费事件重验、整轮重入及原 deadline 拒绝，不修改生产时钟或期限，不构成自然信号归因、native guard 成功或 source/G2 验收。

runtime 容器准入的细分诊断复用既有有界 I/O worker 结果内的私有 `admitDiagnostic`，只锁存首个实际拒绝的固定 stage 与局部已观察错误；调用前的临时 stage 不算失败。容器事实、sysinfo、daemon 条件、mount identity 与可选用户数据库文件的拒绝可细分，接受的缺失文件或其它成功分支不得占用失败锁存。原谓词、短路及系统调用顺序、返回错误对象、授权与检测证据不变；为保留实际系统调用结果而拆分条件时，不重复操作或求值原本跳过的分支。worker 仅在内存记录，经原结果 channel 移交后由原 completion capture 和终态点发布；不新增写入、goroutine、console/FD 操作或宽限。不输出实际字段名、路径、namespace 身份、环境、payload、flags 或错误字符串，仍受原 64-character stage、24-record/128-byte 输出边界约束。诊断只定位本次实际拒绝，不能据此推断其它校验成功、历史失败的同一根因或 source/G2 已修复。

mount-mode 首拒绝仅在原 `held.Mode & 0022 != 0` 分支、且诊断尚可锁存时，按实际已检查的规范路径形状细分固定 stage：固定 Docker root 下的完整 64 位小写十六进制 volume `/_data`，以及 container 的 hostname、hosts、resolv.conf；未知形状仍用原通用 stage。volume 仅把已 held stat 的权限及特殊位精确等于 `01777` 与其它被拒绝的可写权限区分为有限固定分类，不输出原始路径或任意 mode 数值。该分类只证明路径形状和本次已观察位，不独立证明 Docker 归属或匿名卷身份；也不推断被短路跳过的后续检查。nil 或已有首拒绝时不计算分类，不新增文件访问、系统调用、分配或权限例外，原返回错误及终态发布边界保持；组件分类通过不代表 guest 实测或 source/G2 已通过。

固定 runtime program-load 的 Go 捕获与独立 host replay 仅对既有 CGROUP_DEVICE/Apache device policy 和 CGROUP_DEVICE/MIT replace probe 接受两种空 metadata 表示：FuncInfoRecSize/LineInfoRecSize 为 0/0，或精确 8/16；后者要求原 attr_size 至少 108 bytes，包含完整 LineInfoCnt。两种形式的 ProgBtfFd、两 metadata pointer 与 count 均须为零，其余原 40-byte 头之后的字段及未知尾字节仍为零。捕获在 pointer normalization 前核验原值，normalized evidence 保留 record-size scalar 和原长度；不将 scalar 归零、增加程序或接受非零 metadata 数据。原 SOCKET_FILTER/MIT 的非零 BTF FD/count 探测仍只接受零 record-size，bootstrap 的真 BTF 装载合同独立不变。nested instruction/license、log retry、原 syscall/copyout 和 FD 责任保持；属性谓词及合成 replay 证明不等于真实 notification memory、kernel load、HTTP500 根因或完整 source/G2 验收，永久删除 HTTP 保持 CLOSED。

PostgreSQL 的 `postgres-uid`、`postgres-gid` owner probe 固定共用 `/run/g2/postgres-owner-probe` 到 `/var/lib/postgresql/data` 的唯一只读 bind。可信 root producer 在原受保护的 `/run/g2` 下排他创建 root:root、精确 0500、空且与父目录同 mount 的 source，连续持有 `/run`、`/run/g2` 和 leaf FD；同名对象存在即失败，不接管、修权限或覆盖。父目录沿原六字段身份检查，leaf 另固定 nlink、size、mtime_ns、ctime_ns；每次枚举从 held leaf 重新打开独立目录流，前后复核完整 held/named 身份、空目录及 mount，不刷新初始值。真实系统调用错误保留 errno，策略拒绝使用固定错误文本。

两个 owner probe 仅将 `POST /containers/create` 固定为 Docker API 1.44，排除固定 Moby 在旧 API 上强制非递归只读的兼容默认；不协商版本或失败回退。inspect、start、wait、delete 及原始 attach 保持 API 1.41，既有 Info 观察保持 1.44。创建 JSON、缺省 BindOptions、单只读 bind、原操作及清理 deadline 不变；API 的其它创建兼容差异仍由完整 inspect、准入及 OCI 比较裁决。API 1.44 本身不证明实际 kernel/runtime 支持 RRO，不允许从 actual 回填 expected、接受 RO/RRO 二选一或强制 recursive；组件传输证明不代替真实 Docker/source 与完整 G2 验收。

producer 在每次 create 前、inspect 后且 admit 前、start 前，以及 delete 成功并收到 completed settlement 的 retired ACK 后复核同一 source。两个 probe 都成功退休后才允许 parent-relative rmdir 和 parent fsync；任何 probe 或移除失败沿原安装异常和 force-delete/failed-settlement 路径收束，不返回成功 owner，不重试或延长原 deadline。rmdir 成功即记录已移除，随后 fsync 失败不得重建；失败保留尚未移除的 source，custody FD 直到原 probe 清理尝试结束后才关闭。该连续 custody 依赖已注册的唯一 BOOT_OWNER、原 process incarnation/session 和可信 host-root 边界，不声称抵抗任意可信 root 篡改或实现原子 conditional-unlink。

Python、Go 准入和独立 bootstrap replay 分别维护固定单只读 bind 合同；拒绝旧 volume-only、额外挂载、错误 source/target、RW 及额外 mount options，通用 mount 可写权限拒绝不变。broker 在首次 mount-source 验证前捕获同 held FD 的严格 metadata、正 mount-ID 与空目录值基线，前后复核及 parent-relative 重开 named leaf；候选 materialize/create 只按值继承，后续 source 验证与原基线比较，缺失或漂移即拒绝，不持有新的长寿命 broker FD。复核仅覆盖既有 admission、materialization、OCI 比较及受 phase guard 保护的 exec；retired ACK 前全部 invocation/pidfd/bind/exec/transaction/attachment 责任必须结清，退休后 retained facts 只作审计。配置 hash 不独立证明 host 对象身份；组件与静态审查不代替 pinned Docker/runc、独立 owner/退休观察、真实 source 或完整 G2 验收，永久删除 HTTP 保持 CLOSED。

仅 `realm-negative/bpf-broker-kill` 在 owner probe 的 attach 成功后、POST start 前开启有界诊断。POST start 失败时，producer 记录实际逃逸异常对应的首个 response 拒绝分支，在原清理尝试之后采集。私有 root-only tmpfs 接收端按本次 CID 隔离，严格区分固定八字节版本 2 阶段包与固定 64 字节版本 4/type 1 retained-create 摘要；二者都要求内核 root 凭据，旧四字节 V1 与摘要 V3 不再生产或接受。每次最多读取五包，前四包至多各含一个 wrapper、broker、exec、create_history 类别；第五包、截断、附带 FD、异常凭据、无效内容或重复类别均使本次采集无效，所有已交付 FD 均在内容及数量校验前关闭。start 成功立即关闭接收端，不读取数据报。

wrapper 只报告实际 create 失败返回阶段。broker 在原认证通过后才报告 owner probe create 的拒绝，并仅在原 held-runc exec 证明首次完成时报告 `RUNC_EXEC_OBSERVED`。owner 诊断规范化入口只返回全新 JSON-ready 九字段值，`mount_options` 为 null 或固定四枚举对象，`create_history` 为 null 或下述闭合摘要；旧八字段及更早形状不再接受，不保留串口行 producer。只有摘要也可构成 OBSERVED，其它 collection 不得带摘要。诊断投递或收束失败不替换原异常或延长 deadline。root 凭据仅证明发送凭据，缺包及接收顺序不提供运行时阶段或因果证据。exec 观测只证明已认证 invocation 实际换到 held runc，OCI create 和 source 结果由原验收合同独立证明。

owner probe create 的 notification 在原 identity/hold 和 capture 完成后，若七个 BPF handler 之一返回 error 且进入原有 fallback denial 分支，broker 仅对同一 invocation 首次预留固定 broker 阶段 45–51：MAP_CREATE、PROG_LOAD、INFO、GET_ID、QUERY、ATTACH、DETACH。预留要求已认证的 postgres uid/gid create scope、live invocation map 绑定、running 且已有 held-runc exec 证明；随后原 deny 只执行一次，返回 deny 自身结果，原 handler error 只用于固定阶段的 errno 投影。未进入该分支的已结算 kernel errno、QUERY policy 结果、early denial、deny 前置失败或原 close defer 替换均不由此推断；阶段不证明 deny 已发送或交付、内核负结果、首个 fault 或 HTTP500 因果。45–51 与原 broker 阶段共用唯一判重槽，mount detail 必须全零；同一 invocation 不重复发送，跨 invocation 同 CID 重复按既有 INVALID 处理。

仅在原 authenticated owner-probe DELETE 的 authorizeBoundDelete 实际失败后，broker 于原 markFault 之后、原返回及 teardown 之前尝试发送一次 retained-create 摘要；不新增热路径历史状态或改变授权、target、SEND、settlement 和清理。仅读原图中同 grant pointer/id/generation/CID/config 的唯一 create，包括负 key 下的退休对象；invocation 与 transaction map 各最多 4096 项，nil、歧义或不一致图不生成摘要。七个固定 command 槽分别记录 retained 数量及最大 transaction.id 对应的最新 state、native result 分类/有限 errno 与 policyErr 存在性，排除 internal cleanup；不输出 ID、原值、attributes 或错误文本。摘要另记录该 create 的 exec 观察及采样时 group/target 的有限内存状态，不调用 registry 授权或对象身份检查。

摘要 JSON 精确为 `version=2,purpose,create_exec_observed,group,target,program_kind,commands`；purpose 必须匹配 owner，commands 固定顺序七项，每项仅含 `command,count,state,result,errno,policy_error,response`，总 count 不超过 4096。零 count 的 state/result/errno/response 为 null、policy_error=false；非零 count 的 native result 为 UNRECORDED、NEGATIVE、ZERO 或 POSITIVE，NEGATIVE 仅含 1–4095 errno，ZERO/POSITIVE 不输出数值。无 PROG_LOAD 时 program_kind 必须为 null；有 load 时仅对最大 transaction.id 的 normalized attributes 长度/type 与精确 license 投影 DEVICE、EXTENDED_INFO_PROBE 或 REPLACE_PROBE。分类依赖原 producer 对实际 program/attributes/license 的验证，不重新读取、生成、解码或比较指令，不构成独立 instruction attestation；较早 load 仍受原 command/state/result 校验。

响应投影完整扫描当前保留 transcript，不截断为前缀或限为 4096 条 entry。原 append 只有在完整 canonical、预算与 hash 成功后发布，原 16MiB 总预算不变；total/entry 数量仅作 sanity 上界，不作为独立完整性证明。所有 notification_response 必须先取得合法非零 transaction_id 才判断相关性；selected ID 还须与同一最新事务的 invocation/requester/notification 身份精确匹配，重复、冲突或无法完整判读使整份摘要不可用。匹配 body 精确十键，guard_after≥guard_before 只核该 pair，不声明全局单调或重做完整 guard verifier。原 response ioctl、事务 transition、授权、错误对象及 report 调用点不变。

非零槽的 response 为 UNRECORDED、SEND_OK、SEND_ENOENT、ADDFD_OK 或 ADDFD_ENOENT。SEND response 必须为 value/error/flags：有界 int64 value、[-4095,0] error、flags=0，error 非零要求 value=0，returned_fd 为 null。ADDFD_SEND 的 response 为 null，仅允许三个 FD command 且 native 为 ZERO/POSITIVE；ioctl0 要求 returned_fd 为 0..MaxInt32，ENOENT 要求 null。成功 FD 操作的 SEND fallback 仅接受 error=-EPERM/value=0；其它 SEND 不新增与 native 相等的规则，保留原 policy response 转换。RESPONSE_SETTLED/RETIRED 的 returnedFD 与记录核 null 或精确数字，EFFECT_OBSERVED 不要求尚未完成的 transition 已写回该值。状态与响应约束如下，任一不符整份摘要不可用：

|最新事务状态|无匹配响应|一个有效匹配响应|
|---|---|---|
|PREPARED / COMMITTED，native UNRECORDED|UNRECORDED|拒绝摘要|
|EFFECT_OBSERVED，native 已记录|UNRECORDED|投影已记录 response|
|RESPONSE_SETTLED，native 已记录|拒绝摘要|投影并核 returnedFD|
|RETIRED，native UNRECORDED（取消）|UNRECORDED|拒绝摘要|
|RETIRED，native 已记录|拒绝摘要|投影并核 returnedFD|

该值是保留图摘要，不是全部历史响应：count0 仅表示该唯一 create 没有该 command 的 retained 原请求事务，最新项不能排除较早不同结果；UNRECORDED 不证明未尝试 ioctl。native errno 与 policyErr 不等于 response error 或 ioctl errno；SEND_OK/ADDFD_OK 仅指记录的 ioctl_errno=0，SEND body 仍可能携带操作错误，ENOENT 表示该次 ioctl 未送达。已记录成功或 RETIRED 不证明 requester 已消费、load/attach 成功、槽间因果或 DELETE 与 HTTP500 的关系。缺摘要不能解释为零。私有 64 字节 V4 摘要独占一个类别，header 增加固定 program kind、每槽末字节为 response code；零槽全零，跨 kind/count/state/result/response 的非法组合在 guest 与独立 collector 均拒绝，不兼容 V3/JSONv1。原八字节 V2 阶段包、owner 九字段、collect 六元组、2048-byte preflight 上限及 errno/detail 规则保持；组件和单场景结果不能关闭完整 G2，永久删除 HTTP 保持 CLOSED。

仅 `guest-preflight` 的原异常 catch 可向既有失败对象附加 `preflight_diagnostic`，精确包含 `version=6`、`exception={kind,errno,frames}`、`owner_probe`、`docker_runtime_info`、`start_error` 和 `settlement` 六键；owner、Info 和 start_error 保持各自原闭合形状，settlement 为下述八字段值或 null，不接受旧 version 1–5 或缺失、额外字段。kind 仅 OSERROR/JOURNAL/OTHER，errno 仅 OSError 的精确整数 1–4095，否则 null。preflight 不访问异常 cause/context、消息或 locals；只遍历原 traceback，全部节点最多 128 个，超限整项省略。frames 仅保留固定八个 basename 白名单中最后六个 eligible `[basename,line]`，行号为精确整数 1–2147483647，空列表合法；这些标签不独立证明源码 provenance 或真实叶操作。完整诊断以 ASCII、排序键、紧凑分隔符、禁止 NaN 的 canonical 编码计数，最多 2048 bytes、不含 LF；构造失败整项省略，owner、Info 或 start_error 规范化失败分别置 null，不删除其它有效字段。坏 settlement 或与旧三项诊断冲突时仅置 settlement=null；超限时确定性置 settlement=null 并完整重新编码一次，仍超限才省略整项，不截断 JSON、减少帧或提高上限。原异常及两字段 failure 保持，catch 内构造至插入的任何失败均隔离；原 finally 的结果 port emitter 与 authority close 顺序不变，不增加 console 输出、重试或 ACK。

仅同一已准入 BOOT_OWNER 的 postgres-uid/gid probe 在正常 DELETE 成功后、原 completed SETTLE 调用前，尝试开启独立 CID 窗口 `/run/g2/owner-probe-settlement-diagnostic`。原 RPC 必须继续且 deadline 不变；成功立即关闭而不收包，仅同一 completed-settlement 异常仍逃逸时，在原业务 cleanup 与控制连接关闭后采集并附值。可选错误报告最多等待 250ms：收集入口只确定一次绝对 cutoff，取当前 monotonic 加 250ms 与原有限 Authority hard deadline 的较小值；无有效 hard deadline 或已过期时仅作固定次数的非阻塞采集。等待不延长 RPC、owner 的 60 秒或 VM 期限，不重新打开 authority、重复 DELETE/SETTLE，也不刷新 cleanup deadline。构造、角色读取、收集、属性写入或关闭失败不得替换原异常；替代异常、start 失败、后续 source 检查失败和 failed-cleanup SETTLE 不取得该值。source custody 及两次 retired 后才移除的责任不变。此窗口的同 CID/purpose 归属依赖现有唯一 BOOT_OWNER、固定配置和禁止同 owner/purpose 再准入的合同，不声称一般 CID 永久唯一或 root 凭据构成二进制认证。

独立十六字节 type7/version1 数据报承载固定 purpose 和 event，不兼容旧 type6 包。前四个 event 保持原 point、七字节 removal detail 和保留位合同：START_PUBLISHED 在真实 pending 发布后尝试；PENDING_SAMPLE 仅在原存活及 guest deadline 检查通过的实际 nil-error pending return 处，以原 now 首次记起点，至少一秒后对首次到达的责任点采样一次；十三个闭合 point 保留原组合谓词及清理顺序。ACK_COMMITTED 仅在原 retired ACK 构造并进入 principal replay 后尝试；同一 queued ACK 的 connection 只复制 CID/purpose/有效与尝试位，不保留 q/grant/principal/raw/FD。ACK_SENT 仅在原 sendControl 本地成功后尝试；EAGAIN 保留该 ACK/receipt，错误、断开、移除、其它操作、start waiter、replay 或 teardown 不将 receipt 转交下一 ACK。每 event 至多一次 best-effort 尝试，不重试、不 fault、不改变原结果；本地发送成功不证明客户端认证或消费 ACK，retired 仍须原全部责任完成。

settlement 值精确为 `purpose,collection,started,wait,ack_committed,ack_sent,removal,followup`；collection 仅 UNAVAILABLE/EMPTY/OBSERVED/INVALID，wait 为原十三责任标签或 null，事件观察为精确 bool。OBSERVED 至少一项观测，其他 collection 全为 false/null；允许前置包缺失，缺包不推导未准入、未提交或未发送。removal 仅在 OBSERVED 且 wait=OBJECT_REMOVAL 时可为完整七字段值，全零 detail 表示未采集而非空对象快照；followup 只能来自有效 event5，可独立于 event2 到达，此时 wait/removal 可为 null。接收复用 held/named custody、root 内核凭据及截断检查；最多五个不同 event，第六包、重复、错 purpose/形状/保留位、部分 detail、枚举或关系不符、身份漂移均 INVALID，所有已交付 SCM_RIGHTS FD 在数量和内容校验前关闭。有效 event5 后停止阻塞等待，再有界非阻塞排空已排队数据；INVALID 清除全部观察、removal 和 followup，不声称检测收集结束后到达的包。settlement 与非 null owner/Info/start_error 互斥；独立 collector 在完整原 parser 与候选绑定后严格验证 version6，不修复非法输入。受限组件的七个 Go parent、69 个 owner 测试和 24 个 transport 测试不能证明完整 native/source 或 G2 通过；新 C 待验收，完整 G2 为 NOT_READY，永久删除 HTTP 保持 CLOSED。

OBJECT_REMOVAL detail 只在原一次样本到期时，投影该 return 实际 lookup 的同 cookie/object；不改变 map 遍历顺序、不重选最低 cookie、不遍历完整引用图。七个字段为 `object,held_fd,grant_cleanup,creator,creation_cleanup,lifecycle,removal_authorized`：分别报告对象缺失/类别/身份不符、held FD map 成员和符号、同 cookie/grant/creator 的 cleanup 存值、已核 transaction ID/grant/publication.ObjectCookie 的 creator 状态、该 creator 的 cleanupActive、同 cookie 且 kernelID/class 匹配的 lifecycle 标记，以及身份有效对象的 tracker removalAuthorized。对象缺失或身份不符时 creator、creation_cleanup 和 removal_authorized 不可用；creator 未建立或身份不符时 creation_cleanup 不可用；removal_authorized 仅为 null 或精确 bool。lifecycle 缺条目与 checkpoint/lifecycle 不可用分开表示，不能当成已移除。所有字段只读同步内存关系，不查询 FD 或内核；cleanup 的存零不证明 close 已执行，close 成功不等于原生 removal，cleanupActive 不证明当前仍在清理。该对象不是全部未移除对象、唯一/最后引用或 timeout 最终阻塞者；未知 requester/kernel 引用、未 drain 事件及样本后的退休/ACK 仍未由此证明。

event5 的 followup 只关联原 OBJECT_REMOVAL 样本实际选中的 cookie、incarnation pointer 与 grant，并重验原内部身份，不能重新从 map 选择对象。该 q 在后续首次实际到达 CONTROL_DETACHED、ACK_COMMITTED、PRINCIPAL_TERMINAL 或 BROKER_EXIT 时至多尝试一次记录；边界本身不是退休证明。`last_wait` 仅是上次实际 advance 的十三个责任点之一、UNLABELLED 或 null，不能视为出口瞬间的全部责任；ACK_COMMITTED 的 last_wait 为 null。followup 的十二个固定字段为 `boundary,last_wait,identity,grant_close,guard_removal,tracker,lifecycle,cleanup_record,acceptance,attachments,program_type,creator_response`，不导出原始 ID。读取仅限一次最多 4096 条 retained transcript entry、累计最多 16MiB 和最多 4096 项 attachment 的有界观察，不新增 authority syscall 或延长业务对象、FD 的生命周期。

`grant_close` 是同 creator 的 retained close_ref 事实，不能由存储的 closeResult 推定；ADDFD 结果不能证明 requester 已关闭描述符。已观察到 matching guard 记录不等于 tracker 已接受：ACCEPTED 还须同 incarnation 的授权与 removal sequence、tracker 状态、lifecycle 授权和移除状态及 cleanup 记录一致，相关证据缺失、截断或矛盾时不能标为 ACCEPTED。UNKNOWN、TRUNCATED、缺包或未观察到记录均不证明事件或引用不存在，也不证明根因、整个 settlement 已完成或 HTTP 失败的因果。q 仅为该次诊断保留已有对象和 grant 的内部关联，不新增 native 对象或 FD 责任；原授权、责任顺序和退休合同保持。

唯一原 request 状态拒绝点在 deadline 和 65536-byte 大小检查通过后，仅对启用 owner-start 观察的同一 CID、精确 POST start、精确整数 HTTP500，解析本次已经读取的 Docker body：原 strict JSON 必须为唯一 `message` 字符串键，按固定大小写敏感标记优先级投影 DEVICE_QUERY → PROGRAM_FETCH → DEVICE_LIST → DEVICE_ATTACH → PROGRAM_LOAD → CGROUP_CONFIG → CGROUP_APPLY → PROC_READY → INIT_SYNC → PROCESS_START → TASK_CREATE，合法无标记为 OTHER，解析或闭合形状失败为 INVALID。只给原 rejected 异常附固定类别，helper 或属性赋值失败不替换它；不输出原 message、片段、位置、路径、CID、动态尾部或 hash。preflight 仅在同一逃逸异常的规范化 owner 为 RESPONSE_STATUS/精确整数500时采用标签；close 替代异常不沿 context 借用原标签，cleanup 的其它响应不覆盖它。HTTP400 的 Info 观察与非空 start_error 互斥；其它请求、状态、早先拒绝和传输失败不调用分类。类别是同次认证 daemon 响应的词法标记，可能来自引用或日志，不证明执行 provenance、首个/最深错误、syscall/native errno、requester 消费或 DELETE/HTTP 因果。原 V4 history、私有 V2 阶段包、九字段 owner、六元 collect、HTTP/清理顺序和期限保持。

只有原 start 异常仍以同一对象逃逸，且原清理、owner 采集及诊断关闭已完成，并取得 HTTP400 的 PGDATA actual `RBIND_RO_RPRIVATE` / expected `RBIND_RRO_RPRIVATE`、difference `VALUE` witness，才通过原 daemon socket/peer 身份校验发起一次固定 `GET /v1.44/info`。观察使用保存的原 60 秒操作 deadline 与至多一秒窗口，不能借 failed cleanup 的延时；connect 后才启动 daemon Timer，以本次连接对象 abort。取消 Timer、关闭 response 和连接分别尝试，不 join、不 drain、不重试，观察或收束错误不替换业务异常。Info 的版本查询可能产生额外 guest 子进程且 daemon 侧命令无 context；client timeout 不证明这些工作已取消，整台 owned VM 的实际 wait/pidfd/exit 仍沿原收束门禁独立核验。

Info 仅投影固定 default runtime `g2-bpf`、固定 wrapper path 与小写 `status` 中发布的 OCI features，两个 JSON 层均使用原 strict parser；features string 的 UTF-8 上限为 16384 bytes，mountOptions 最多 128 个、每项最多 64 个 ASCII 字符。公开三字段精确为 `collection,features,reason`：OBSERVED 仅有 RRO_PRESENT/RRO_ABSENT/NOT_PUBLISHED 与 OK；UNAVAILABLE 的 features 为 null、reason 仅 DEADLINE/RESPONSE_SIZE/RESPONSE_STATUS/TRANSPORT；INVALID 的 features 为 null、reason 仅 INFO_JSON/RUNTIME/FEATURES_JSON/FEATURES_SHAPE。独立复制的 Info 非 null 时必须关联上述 eligible owner；它只表示查询时发布文档中 exact `rro` 的成员关系或未发布，不证明历史 kernel cache、先前 create 实际消费的配置 generation、挂载完成或允许改变 RO/RRO 权限。无关 Info/features 内容及任意错误文本不输出，诊断不参与授权或成功判定。

host 只在同 selection/build receipt、实际冻结 seed/CIDATA 与 expected-install 绑定通过后，使用原完整 INSTALL/READY/DATA/END parser 重建 terminal，再以独立字面量 schema 判读可选诊断；不保留手工第二 parser 或旧串口 prefix fallback。只有 completed 精确为 false、guest-preflight failure 和 NOT_READY/CLOSED gates 及全部字段/范围/交叉关系通过才标 OBSERVED。字段缺失、无效或不可用分别保留实际状态，拒绝对象不进入公开摘要；完整 terminal 仅作私有证据。传输完整性不构成 guest 二进制 attestation，owner null 不证明 probe/start 未到达；失败诊断不改变 receipt、parsed.json 或独立成功 verifier 的拒绝，组件通过不代表 source/G2 通过。

bundle prepare 在原实际求值的拒绝点仅记录首个失败谓词，或同一次 stat 比较中按原字段顺序首次不同的字段；通用 stat 比较共用唯一实现，仍不比较 atime。固定编号 80–150 属于原 wrapper 类别，与 1–24 共用判重槽，使用统一 V2 数据报和终态一次发送。原通用权限策略、返回 error 链与 errno 折叠、短路及 I/O 顺序、caller FD 归属和 shared offset 不变，不为观察重复 stat、读取或比较。stage 10 仍表示内部未细分；stage 150 仅聚合实际选中 exact-selftest argv 分支后的失败。缺失、INVALID 或未观测阶段不能排除其它 invocation，新阶段也不证明 metadata 的形成原因、修改 actor 或历史顺序；组件证明不代替真实 guest、独立 source 或完整 G2 验收。

完整 OCI 结构比较仍仅使用原 `reflect.DeepEqual(actual, expected)`；其它准入与身份门禁保持不变。仅在其拒绝后，局部固定 66-frame 迭代栈按 map key 字节序、array 索引序寻找首个结构差异，再以最长固定 segment-prefix 输出 151–239 的 broker 区域分类；array marker 与字符串键 `[]` 区分，missing/null、nil/empty、动态类型及 `json.Number` 原文本保持不同，不标准化或从 actual 回填 expected。每次最多 32768 logical work units、4194304 compared/key bytes、每个 map 64 项；完整 map union 扫描完成后才选 key，计费不依赖随机迭代中变化的 candidate。预算不足返回 240，depth 65 节点仍可检查、需要继续 push 则返回 241；这些预算只约束新增 helper 的定义操作，不是先前完整比较或 runtime 的硬时间上界。无可细分差异时保留原 39，所有分类均沿原 guard error、nil candidate、zero stat 拒绝，不执行后续 gate 或发布 facts。

固定编号 151–241 与 32–44 共用原 broker 判重槽；未知值、重复类别、凭据/rights/截断及 custody 拒绝保持不变。区域分类不输出任意字段值、未知 key、索引或路径，不新增 I/O、FD、syscall、goroutine、等待或发送点；原 error 链独立决定 errno，不将类别编码为 errno。区域分类不证明任一方正确、具体字段值、根因或 actor；39/OTHER/LIMIT/DEPTH、缺失或 INVALID 不能推断其它 invocation 未发生。组件和单场景结果不替代独立 source 与完整 G2 验收，永久删除 HTTP 保持 CLOSED。

仅原首差异最终分类为 188 时，在尚未复用的活 stack 上观察同 index 的两个 mount map。两侧 destination 必须均为不超过 64 bytes 的相等 string，才产生有限 mount 标签、actual/expected 有序 options 签名及 pair difference 四成员 witness；未知相等 destination 仅标 OTHER，缺失、类型、长度或配对不符时保持 null。观察整个 options 字段，保留 absent/null/typed-nil/empty/type 差异，LIMIT 优先于 NON_STRING，以 duplicate-aware multiset 区分 ORDER 和 VALUE；不从 actual 修 expected，也不改变原首差异 stage、遍历预算或 guard 拒绝。每侧最多扫描 16 项和每个 string 64 bytes，九个固定签名及局部 16 项匹配表限定额外工作，总计少于 512 次有界内容比较、两侧比较 bytes 不超过 65536；这不是 Go runtime 或原完整比较的硬时间上界。

诊断 record 沿 verify、bind 和原 cleanup 后的 deferred sender 整值传递，后继阶段用 stage-only record 清空 witness。私有 V2 的尾四字节仅允许 stage188 全零或完整合法枚举；partial、越界、其它 stage 携带 witness、stage188 非零 errno 均拒绝，不能降级修复。公开 `mount_options` 仅允许 OBSERVED 且 broker 精确为 `BROKER_OCI_MOUNT_OPTIONS`/null；该 broker 无论 detail 是否 null 都要求 errno null。witness 只证明本次拒绝的有限 pair 分类，其 difference 可反映晚于原首 leaf 的 shape/limit；不证明完整原值、kernel/runtime probe 结果、actor、根因或权限修复已获授权。

固定 Docker 的 managed containerd 仅经 daemon 专用 PATH 下排他安装的 `/opt/g2/containerd-bin/containerd` 启动；launcher 与 authority/runtime 为相同字节、不同 inode 的 root:root/0500 单链接文件。初始及 reboot daemon-child 在原 SIGSTOP/WATCH 前拒绝既存 execroot/system containerd socket，保留原 fixed Docker argv 和同 PID dockerd exec。launcher 固定 argv/config，锁定原始 OS 线程，恢复基础 PATH，设置并复核 umask 0022，再以同 PID exec 已 held 的固定 `/usr/bin/containerd`；不改写 bundle/TOML、不修权限、不接管 sibling daemon，不放宽通用 0022 或完整 OCI 校验。

broker 从注册时实际 held 的 launcher/package-target key 追加唯一 `containerd_launcher` pair，成功后才发布 registry；追加失败使 epoch faulted，不能重入安装或继续 WATCH。真实 WATCH 必须由原 drain 路径导出并核验后才能记录 stopped-daemon/ACK；顺序为 runtime-root→pair→对应 WATCH guard→stopped-daemon，随后才是 generation2 dockerd exec 与 executing-daemon。launcher 必须是 watched dockerd 进程或其工作线程的直接独立子进程，且为 fork 后首次 own exec；同 task 紧邻 exec 到 pinned containerd 后才获得 managed provenance，fork/thread 不能借用未完成的 launcher 历史。live observer 与独立完整/partial/reboot replay 分别验证 held pair、package pin、同 WATCH root 和历史转换，拒绝重复、别名、缺失或借用历史；失败关闭原 epoch，不新增恢复接管路径。

合成 target 的组件证据只证明 launcher 的权限继承、同 PID exec、环境与 FD 收束。真实 exact 中严格消费者接受，只证明该次被检查对象满足原谓词；不倒推 selection123 的完整 mode、creator/umask/ACL 历史，也不声称所有 containerd 分支都采用该 mask。组件、独立审查和单次 source 场景均不替代完整 G2，永久删除 HTTP 保持 CLOSED。

固定 Docker 29.8.2、overlay2 MergedDir 与既有 none/固定 named bridge 模板的创建期准入，将配置承诺和未物化的运行期事实分开。三个 system-path inspect 字段仍必须是 string：精确空字符串只表示尚未物化，期望路径独立推导为 `/var/lib/g2/docker/containers/<完整 CID>/{hostname,hosts,resolv.conf}`；非空值必须精确匹配并立即通过原身份检查。缺键、null、错误类型、其它 CID 或非规范路径仍拒绝，不从 actual OCI spec 取得期望，不开放网络模式或 mount override。ADMIT 保留独立 image/container 配置、daemon、资源、安全与 mount-source 检查，不读取尚未物化 rootfs 的 passwd/group。

首次 create BIND 在原 principal/invocation/ACK 构造之前，以一份局部候选完成全量 rootfs/system/mount 身份检查，读取有界 nofollow passwd/group 字节及可选缺失状态，再比较完整独立预期 SPEC、device rules，并复核身份、用户文件和 held bundle/config 输入。候选不修改原 grant 或共享 maps/slices；任何验证失败不发布 ready 状态或接管 caller FD。验证返回已核 config stat，后续可失败步骤完成后，原 event-loop 才连续发布候选、bundle custody、createBound 和 invocation/principal/replay maps，不在发布后追加可失败的 Fstat。

prospective facts 不能用于 expected-spec 权限消费者、runtime notification、exec 准入或已接受 bundle 复核；未发布的 ready 候选仅供首次 create 的完整比较。后续操作沿已接受 snapshot 和原 held/named 输入复核，不重新 materialize 或刷新用户文件。相同 task、live pidfd、原 raw bytes 与 held descriptions 的 exact replay 复用既有 ACK；不同 task 的第二次 create 拒绝。bind 返回后 finishTask 或 ACK 发送失败继续承担原 revoke/retirement 责任，不重置已接受快照。本地普通与受限 root 文件 fixture 只证明组件生命周期，不替代 pinned Docker/runc、native failure-cleanup、完整 source 或 G2 验收；永久删除 HTTP 保持 CLOSED。

受限 reader 通过原 PID1 startup 认证同 ELF 的 observer，不增加 reader 的 capability。PID1 在原 observer READY 和全线程 confinement 已核验后，用原 live pidfd 与前后相同的实际 start ticks 绑定该进程，从原 procfs 打开其真实 executable，核 root-owned 只读普通文件及 manifest 摘要，再在原 credentialled witness 中按固定顺序发送两个 rights：原 observer pidfd、该 held executable FD。reader 严格核 PID1 的逐消息凭据、boot/nonce/PID 和恰好两个 rights，继续独立读取实际 status、argv、cgroup、start ticks 并核 confinement 与存活；只将 executable 摘要改为对该只读 FD 的有界 pread。文件与进程的关联来自 PID1 的上述实际获取，不把 sender 声称的摘要或任意同 hash 文件当成该关联的证明。observer 禁止 exec 的全线程约束贯穿交接；PID1 后续向 broker 交接同一已持有对象，原 broker 的独立检查保留。其它 process observation 仍直接读取实际 proc executable，不提供 EACCES fallback；真实校准和完整 source 验收仍为独立门禁。

runtime 的成功 attachment 必须由原 broker 对 retained exact cgroup/program 执行独立、受审计的 query 确认；不能要求 pinned runc 发出其源码没有的 post-attach 请求。短命令的 exec/exit 可由原 native incarnation 与已持有的固定 package executable 结算，实际效果 notification 仍须核 live runc。create 尚未结算时，start 在原截止内等待并继续处理 audit/guard/death/control；继承 filter 的应用在 runc 退出后仍须取得真实 EPERM，拒绝记录不赋予应用执行权限。正常退休逐一释放无后续消费者的 PIN/pidfd/descriptor，保留仍有实际继承者的 denial-only listener 和仍需验证的 bundle custody。

extra-FD 生命周期验收须以真实 ADDFD 把 program 的额外引用交给已核 child，detach 并关闭其余引用后，在正常 grant retirement 或 `SETTLE` 前取得 released checkpoint，证明对象仍 live 且尚无 removal；随后 child 关闭最后 FD，parent 实际 wait 该 child。正常结算仍须等待同 cookie 的 removal 与最终 checkpoint，标准 grant retirement 一律要求观察到全部 owned removal 且 attachment 已释放，不设延迟引用的退休豁免。attachment replacement 的 `replaces` 始终引用先前的 attachment ID。

内部 registry 的删除后 readback 保留真实 `MAP_LOOKUP_ELEM=-ENOENT`；`registry_sequence` 只定位同 map、同 exact key/grant 的原 delete 记录，`value_hex=null` 表示没有返回值字节，不把 locator 当作 kernel copyout 或新权限。其余失败输出不能借此放宽。cgroup offline 回调可能由 kernel worker 执行，不能单凭该回调把 rmdir 归因到某个 runtime actor；实际授权 tuple 与后续生命周期事实分别核验。

隔离 BPF bootstrap 只用纯内存 ELF/BTF 解析固定策略，先校验四 map、三 LSM program、名称、license 与重定位形状；不能使用会隐式发出 feature-probe `SYS_BPF` 的通用 collection loader。解析本身不创建内核对象，后续效果仍由原 guard/observer 逐项登记与校验。

source 的早期 launcher 固定独占安装到 `/opt/g2-launch/g2-bpf-authority`，目录 root:root `0700`、ELF `0500`；`/run/g2` 仅保存 source 脚本，reboot 仍使用后装的 `/opt/g2/g2-bpf-authority`。创建前及 exec 前核真实祖先、held/named 身份、摘要和 held FD 的实际 mount-ID/noexec；已有路径或漂移直接失败，不以 chmod/remount 修复环境。seed 构建显式拒绝 ELF `PT_INTERP`。

每个新建的只读 G2SEED 镜像须在生成阶段将根目录及完整已核 payload inode 固定为 `0:0`，保留原 mode、size 和 bytes，并在封存摘要前逐项读回 owner 与非组/全局可写属性。非特权 host staging 的 owner 不得由 `mke2fs -d` 泄入受信 guest inode；修复派生镜像的生产者，不放宽 launcher 的 root-owner 检查，不 chown host staging 或原位修改历史镜像。

原 locked-main broker 在首次 launch registration effects 前，经原 held procfs 核 PID1 的 cookie/start、live PIDFD、systemd exe 和真实 root；逐 TID 的 `KCMP_FS`、root/cwd、namespace 和有效 `CAP_SYS_CHROOT` 与固定 CGO=0 Go 线程继承路径共同约束一次 all-thread root 交接。传给 syscall 的固定 NUL 结尾路径字节在全部线程调用完成前保持稳定，指针仅在 syscall 调用边界转换。失败状态不可重试，ready 前禁止 runtime admission，后续 admission 继续复核原 PID1 与自身 root，reader/observer 保持原 root 并使用 held procfs。

成功交接仅产生一条 `runtime_root`，精确字段为 `{broker_cookie,pid1_cookie,pid1_start_ns,pid1_exe_sha256,root_mount_id,root_dev,root_ino,handoff_count,thread_ids}`；count 为实际一次完成，thread IDs 为实际排序且唯一的已核线程。guest/host 分别严格关联原 broker、bootstrap `audit_control` 的原 PID1 cookie/start/TGID/TID 与 selected systemd hash；交接前只允许 registration=`bootstrap` 的原 broker、audit reader、terminal observer 三种 principal，其他 principal 和 runtime effects 全部要求先有交接。矛盾或缺失的原 PID1 观察不能由 handoff 自报值补足。这条记录导出 broker 的真实观察，不冒充 host 对 guest `fs_struct` 的独立测量。新 selection 的实际 exec、原 broker 身份不变与 runtime start 均验收后才可通过该步骤，静态检查或消除 EACCES 不足以通过。

typed audit 的 `raw` 字段必须显式存在：普通 transaction/BPF audit 为 null；raw syscall join 为 `{tgid,syscall_payload_sha256,syscall_occurrence,eoe_payload_sha256,eoe_occurrence}`。TGID 取实际 audit PID，不能由 native actor 回填；TID/start 仍由 native incarnation 证明，不声称 audit 独立提供它们。host 核同一完整 stamp、实际命令/结果及 SYSCALL/EOE 的原 wire occurrence。terminal observation 保留 FINISH 返回的 `terminal_sha256`，退出、native start、module instance 和 direct wait 只取实际观测，不用默认值或 tick 换算冒充；early observer principal 3 只是固定协议预留，不能替代尚未产生的普通 principal transcript。

五个 assembly baseline 的 observation 必须保留实际 child frames 与 parent `wait4` 结果，精确 `assembly={probe_cookie,pid,start_ticks,wait_pid,wait_status,frames}`，与原 probe、notifying actor、listener 和 IMPORT 交叉核验。每帧仍受 16KiB 上限、最多三帧；`nonleader-files` 须有成功 unshare 且 leader 保留 A，`shared-files` 须有 leader 观察 B，不能只以相同的 fixture-B import 代替两种文件表行为。frames 是 child 实际回报，不冒充独立 kernel 测量。

`audit-interleaved` 先完整接收两项真实 SYSCALL/EOE，再以固定 A-SYSCALL、B-SYSCALL、A-EOE、B-EOE 顺序交普通 join。`selftest_audit_reorder={name,nonce,received,delivery_order}` 保存四项原始接收次序；每项保留原 `audit_wire` identity，`delivery_order` 为 1–4 的映射排列。host 同时重放原 stamp/hash/occurrence、该映射和实际普通 join，不把受控 userspace 交付重排声称为 kernel 自然乱序。

root claim 继续要求同一完整 stamp 的真实 SYSCALL/EOE；配置 ACK 或尚未收到支持记录不是事件结清证据。

native authority phase 只允许 `1→2`、`1→0`、`2→0`，不按枚举数值大小判断撤权，也不清除 sticky loss。pre-SEAL 的真实 DEAD 状态允许尚未建立的 epoch、bootstrap digest 与 link/program 身份全空；混合身份或 sealed 状态缺项仍拒绝。early terminal 聚合 canonical payload 使用包含唯一 LF 的 64MiB 上限，最多 8192 个 decoded 8192-byte chunk；单条记录、控制帧与每个串行 chunk frame 仍受原 16KiB 上限约束。

host 对每个 exact case/scenario 选取同一 build selection 中最新 committed launch manifest；新声明缺 receipt、未完成或失败不得回退旧 PASS，不同单 scenario 调用可保留其他已声明场景的结果，family 成员必须来自同一 attempt。33 个 baseline 各须真实动作和 typed 事实及对应区间的独立 replay；13 个 snapshot/audit/reader 故障须由实际 detector 与独立 witness 证明输入、检测及降权，保留原 deadline 和输出预算，超限即失败。基线名称、variant 标签、主动撤权或声明的损失布尔值本身不是探针或故障成功。当前候选不代表真实 VM、host 语义负例、source/C81 准入或完整 G2 已验收；永久删除 HTTP 保持 CLOSED。

source VM 的新 runner 使用 `houfeng.source-vm.operational.v2` 固定 4GiB 增长窗口与采样保护，budget JSON 仍为 v1、case/family 收据仍为 v2；policy literal 区分预算语义，不提供 CLI/env/case 覆盖或自动扩额。收据代际由 selected build 的 `runner_inputs` 是否包含 `source_resources.py` 决定，新 build 不能降级为旧收据。历史 selection/收据不改写，当前 CLI 继续核对选中 runner 的真实字节身份，旧 runtime 不能认证新候选。新合同保留每 member 的 16GiB boot overlay、四个 8GiB raw、4GiB MemAvailable 与 KVM API12，不按全部 48GiB 虚拟容量预留；4GiB 是待真实校准的策略，不是完整成功流程的最低需求或峰值测量结论。

预算以本次选中 payload 的准确字节 P（含 case 和 SHA256SUMS）为输入：Q=256MiB，C=`max(2GiB, ceil((ceil(5P/4)+Q)/Q)*Q)`，C 超过 8GiB 拒绝；CIDATA D=528482304 bytes，staging S=`ceil((P+C+D+Q)/Q)*Q`。首次 prepare 要求 free≥S+4GiB+1GiB，随后按已计费准备分配减少剩余 S；launch 要求 free≥5GiB。已有 build 与 retained artifacts 已反映在实时 free 中，不重复扣除。每 member 按安全 nofollow 文件树的 `st_blocks*512` 计费；此 charged allocation 不是 btrfs 全局去重后的唯一物理字节。运行阶段最多每 0.2 秒采样一次，free<1GiB 或累计 peak growth>4GiB 则锁存首个容量错误；同盘 reboot 复用原基线、累计窗口和 deadline。

所有 parsed/carrier 输出及真实 wait/PIDFD 退出观察完成后才强制 finalize，要求 peak growth+1MiB≤4GiB、free≥1GiB+1MiB；随后仅发布一份不超过 1MiB 的最终收据。收据通过 held 目录内排他临时文件、file fsync、atomic rename 和目录 fsync 发布。rename 前失败保留旧 primary；rename 后目录 fsync 失败属于 publication unknown，CLI 失败，只有独立验收完成全部语义验证、重新 fsync 并核同一 receipt 身份/hash 后才可解析成功。无退出证明不能补造 reap，收据失败不能覆盖原首因或声称 durable FAIL。采样与余量不是硬 quota，不能保证并发写入或采样间突发后仍能落盘。

25-member family 保留全部已执行成功/失败目录并记录实际 retained allocation；明确容量或 parent ENOSPC 停止后续 member，未知 errno 的写盘 child 失败以 `storage_tool_failure` 停止，不根据 stderr 文字推造 ENOSPC。未执行成员完整列为 NOT_RUN，任何已执行 FAIL 或 NOT_RUN 均不能整体 PASS，也不能回退旧 attempt。首个完整 `realm-negative/bpf-broker-kill` 必须真实 PASS 并通过独立验收，才继续后续矩阵；只记录 observed peak，不自动调高预算或清理历史。局部测试/build/审查不代替真实校准、完整 source 与 G2 门禁，永久删除 HTTP 保持 CLOSED。

新复制的不可变 seed/payload/archive 与 source snapshot 统一经过原 `_copy`：目标排他 nofollow 创建、初始 0600；同 device 且至少 1MiB 时对 held source FD 执行 FICLONE，仅 EXDEV/EOPNOTSUPP/ENOTTY/EINVAL 可清空本次新目标并 full-copy，其余错误原样失败。clone 与 fallback 均完整核 source SHA256、读取前后 held/named 的 dev/inode/size/mtime/ctime 稳定、独立 single-link target inode、实际 size 与 digest，再应用原 mode 并 fsync。失败保留本次部分目标，不覆盖或重建历史 inode；不使用 hardlink，不 clone 成员 seed.raw、CIDATA 或可写业务盘。实际共享 extent 与物理释放独立测量，预算仍按 `st_blocks*512` 计费。

source 产物退休仅通过显式 `retire-plan --run-dir <R>` 冻结清单，再以 `retire-apply --run-dir <R> --manifest-sha256 <digest>` 应用；不按年龄、空间阈值自动清理，没有 force 或任意目标路径。本批只允许 selection 4–7、10–17、19–31、33–53、55–62、64–75、77–98、104–105、107–111、115–143 的已完整退出 FAIL attempt1 磁盘，以及 selection→build 2→2、3→3、8→8、9→9、32→32、99→103、100→104、101→105、102→106、103→107、106→110 的无 run 根构建副本。固定 pin 整个 selection 1、18、54、63、112–114 及引用 build，另保留实际 PASS、IN_PROGRESS、退出或发布未知、当前 selected build 和真实 scenario/family 输入；新发现 owner 不加入本批。VM 仅限直属 boot/app/w1/w2/control/seed 载体，4–7 另限原观察声明的两份 CIDATA block，其余限 receipt 声明的 cidata.raw；payload、CIDATA 模板、日志、parsed、协议和历史 metadata 保留。无本协议 intent 的预先缺盘整 owner 排除并记录 `preexisting_artifact_missing`，不补造退休归因。

退休器按原 receipt bytes 哈希并 strict JSON 解析，分别认证 legacy host-launch、真实 launch manifest 和 build 的历史绑定，不用新资源 oracle 重判旧 FAIL。无 run 构建的 seed/bin/boot/c80 archive 白名单副本，必须先从 held FD 完整核实际内容等于原 receipt 对应摘要，替换内容或缺失摘要整 build 保留。inventory/apply 持同一 `source-vm.lock` 非阻塞排他锁；持久清单记录 R/每个祖先与 leaf parent 的 dev/inode/uid/gid/mode、每个 single-link regular leaf 的完整 stat 身份和 `st_blocks*512`，每次逐组件 nofollow/openat 并核实际 FD mount-ID，不以 st_dev 代替 bind-mount 检查。必须重验连续 selection/receipt、当前 pin、反向运行引用及本 euid 各进程的 fd/cwd/maps/argv：逐字段检查，无论 mount namespace 是否相同或可读，均匹配所有可读路径，fd 的 dev/inode 亦跨 namespace 比较；不同或不可读的 namespace 本身记入披露，不将可读路径字段标为不可确定。可见关联或对象身份不明保留对应 owner，进程或字段不可见的底层引用记入披露，不作为全部 owner 的全局否决。以 lsof 补充，只可排除 `/var/lib/docker/overlay2/` 下以 `/merged` 结尾的 overlay 挂载和 `/run/docker/netns/` 下的 nsfs 挂载，且其 mount ID、设备和路径不得与 R 相交；仍有 stderr、异常退出、无法归属的路径或其他检查异常时失败关闭。不 kill、提权或把一次快照当互斥，也不声称排除了不可见引用。

canonical 清单 ≤4MiB，以完整 bytes SHA256 定名保存到 0700 `source-retirements`；策略为 `houfeng.source-retirement.v2`，manifest 必含 `activity_disclosure`，记录检查进程数及按 PID 排序、最多 4096 项的不可确定进程、原因与字段；apply 用同一策略重查并在返回值披露本次观察，不改变持久 complete schema。manifest/intent/complete 均以排他 0600 临时写入、fchmod 0400、最后 file fsync、原子 rename、目录 fsync 的顺序发布，新目录还 fsync R。任何未知或损坏记录失败关闭，私有临时文件只保留识别，不授权删除。apply 只接受准确 digest，首次应用要求全部原 leaf 身份与 selected_at_plan 不变，先耐久发布不可撤销的 `RETIRING` intent，才逐个复核并 unlink、立即 fsync 父目录；intent 发布未知时当次零删除。恢复只允许同 digest 中的缺失 leaf，已存在者必须仍为原 inode，缺失者也 fsync 同一已核父目录；新增 selection 仅在原历史绑定、pin 与当前引用安全时允许恢复。全部目标缺失、metadata 已核且 leaf FD 关闭后才耐久发布 `RETIRED` complete；重复 apply 重验并返回同一完成记录及本次披露。charged sum 不是物理释放量；complete 的 before free 是实际完成该记录的 apply 调用起始观测，恢复不重建或冒称最初崩溃调用的 free。

有效 intent 从可见起即使整个绑定 owner 失去原盘认证/恢复能力；请求路径与退休 owner 有祖先/后代交集时读取器失败 `artifact_retired`。仅 manifest 尚未退休，坏 schema/hash/目录身份/receipt/历史绑定、冲突记录或孤立 complete 均拒绝认证。selected reader 仍先哈希全部历史 selection/receipt，再在消费当前 binary/seed 前检查；case reader 在确定最新 declaring manifest 后、普通缺 receipt 和 family 缺失/retained 分支前检查，不跳过新声明或回退旧 PASS。semantic-negative 的 build gate、原件加载与真实变异验收持同一锁到 finally 释放；本批不退休 family，不用摘要代替原件或把退休后的部分删除解释为 PASS/NOT_RUN。

source 的新编译候选固定累计 broker distinct audit wire 上限为 16384，独立 observer 的 distinct wire 与累计 serial 各为 18432。checkpoint、bootstrap completion 和 EOE 不释放历史，满额旧 key 仍按原 occurrence/重复检测，新 key 必须保留实际拒绝 identity 与 count。audit-overflow 仅在真实新 wire 越过 broker 边界、独立 observer 同 wire occurrence=1、capacity/count_before 均为 16384、实际 phase2→原 broker 退出→phase0、三 link 与 loss 判定全部成立时通过；bind 次数不是 wire 数。

每条 authority transcript 按原 canonical record 长度加一累计计费，固定上限 16MiB；加一只计分隔开销，不改变 hash 输入或 domain。完整 genesis+suffix、partial 与 terminal observation 各自完整 envelope 上限 32MiB，同时独立核内部 record charge。正常 result 为无 LF 的原 canonical JSON，early result 含唯一 LF，完整 metadata/证据合计均不得超过 64MiB；protocol 与 serial 完整文件分别不得超过 128MiB，chunk 是 result 的分块，不重复计第二份 result。正常 producer 保持 2048-byte chunk、同一 30 秒 batch、原底层期限；consumer 仍接受行长合规的较大 decoded DATA。原 line 8192 bytes、count 65536、control frame 16KiB、每页最多 64 records 且 ACK 实际可表示等约束不变，冻结 P0 不修改。

独立 unicast pre-cut 同时限制为 18432 records 与 16MiB body bytes；post-cut 限 36864 总 occurrences（含重复，prefix replay 不计入）。BPF facts 与 qualifying syscall facts 两 list 各限 16384，correlation 两输入分别同限、combined post-baseline audit rows 最多 32768；未完成 serial pending 仍为 4096，EOE 释放 pending。bootstrap queue、broker pending、rawSeen、native ring/depth、kernel backlog、规则与 capability 均不扩大。所有容量是固定策略，无 CLI/env/case 覆盖或随计数提高；不保证各内部极值能同时装入外层 envelope/result。

同盘 vm-kill reboot 的完整 result 只保留在原 protocol/parsed 文件及现有摘要承诺中，不持久化 receipt root 的 `reboot_result`，最终 receipt 仍最多 1MiB。live/replay 显式向 verifier 传递实际读取的 reboot dict，仅 source-recovery/vm-kill 允许该值。离线读取对两原文件各 nofollow 打开一次，核 current-owner、单链接、0600 regular、held/named 前后稳定身份及有界完整 buffer；protocol 同时匹配原 attempt identity。摘要只核这份实际消费的 buffer，再由真实 parser 与 strict JSON/canonical 检查两份结果一致；缺文件、漂移、超限、错误摘要或 schema 均失败，不从 receipt 补原件。parsed/capture 的实际分配仍计入同一运行 guard，128MiB 文件上限不是额外宿主额度；采样与 reserve 不构成硬 quota 或完整并发物理峰值证明。

mask/alias 路径失败可在第一份诊断持久化之后，另存 root-only、排他创建的私有路径快照：只读固定 unit 搜索目录的 nofollow 身份与固定 systemctl 属性，最多 32 个 unit、192 KiB，受剩余 seal deadline 与 5 秒上限约束。快照绑定原诊断摘要，但不替换原 stdout/stderr、失败或公开 schema；附加诊断失败不得掩盖原始失败。它只用于解释失败，不能证明封闭成功，也不授权覆写真实 unit 或扩大 ALLOWED。

host runner 收到完整、校验通过且 `completed=false` 的终态 END 后立即进入既有 held-pidfd/QMP shutdown，不等满 guest wall deadline；保留原 protocol 失败，并由真实 wait/exit 填写回收事实。外层工具超时杀死 runner 而未留下退出见证的旧 attempt 保持未完成，不能根据事后 PID 消失或读取磁盘补造退出成功。

已存在的受保护 `/etc` unit alias 不被覆写。首次全量安全 preflight 中通过可信、有限、同类型、非模板且不指向 ALLOWED 检查的 forbidden alias 链，可在隔离 guest 的 `/run/systemd/system.control` 排他创建原 alias 名称的 direct mask，同时保留原链和独立 canonical mask。创建前必须从实际 manager 的 `UnitPath` 证明 control 路径优先于 `/etc/systemd/system` 与 `/run/systemd/system`；路径列表每次 reload/observe 均须完全相等，其他未知搜索目录的同名入口仍拒绝。control 目录必须安全且原先为空，缺失时仅允许在 held `/run/systemd` 下排他创建；不收编已有 mask，不覆写或删除冲突。reload 后独立要求 alias 的 Id/Names 精确为自身、LoadState=masked、UnitFileState=masked-runtime、FragmentPath 为本次 control mask 或 `/dev/null`，最终 alias 和 canonical 均须静止。linked unit、循环、未知模板映射、未登记高优先级入口和无法核验的路径仍拒绝。

每次观察重新核 held/named 文件、原 alias 链、父目录身份、control 目录精确清单和 manager 结果，公开 `closure={allowed,files,active,override_digest,unit_path,stop_preflight,stop_transition}` 及其摘要。仅本次 control direct route 增加 `control_origin={terminal,aliases}`，保留原链并引用独立普通 terminal route；其 manager 恰含自身 masked 观察，其他 route 不得冒用该 schema。control 目录创建只允许在创建、父目录 fsync 和身份读回成功后，核对并刷新 netplan runtime 祖先链中 `/run/systemd` 的 ctime；已有空目录不授予此刷新，ordinary mask 与 reload 的既有过渡分别核验。host 严格验证路径顺序、链、身份、manager 与重算摘要；未导出的目录 absence 仍由受信 root 执行路径保证。observer-capability 在 genesis 前返回时也必须导出同一 closure，不能只用握手或退出码代替 systemd 封闭证据。

隔离 guest 的 netplan 例外仅允许在 `/etc/systemd/system/netplan-ovs-cleanup.service` 原入口不存在时排他创建 `/dev/null` symlink。创建前必须核同名 runtime 文件为 root:root、0644、单链接、337 字节的普通文件，manager 无 alias/drop-in/stop 命令、进程或 job，且全部祖先安全；原 runtime 文件不被修改。唯一受控 mask reload 可接受安全属性不变、原字节完全相同的 runtime 文件重建，随后固定新 held/named 身份，后续漂移即拒绝。父目录 ctime 仅在本轮创建及该次 reload 的明确边界核对后刷新，不能全局忽略。该精确 direct `/etc` route 必须导出一项新鲜 masked manager 观察，host 严格核验；其他 direct route 不得冒用此例外。创建时 absence 和未导出的 runtime 字节由受信 root 路径保证，不声称 host 可独立重建这些事实。真实 guest 封闭与完整 G2 验收仍须分别证明。

固定 vendor `udev.service → systemd-udevd.service` 不能因目标在 ALLOWED 中而保留未允许入口。仅在高优先级入口无冲突、runtime 同名入口不存在且 vendor 身份安全时，对原 alias 名称安装 runtime direct mask，保留并复核 vendor symlink 身份。reload 后及每次 observe 独立要求 alias 自身 masked 且静止；若 manager 仍返回 canonical 身份则失败，不停止或 mask canonical。canonical 继续按原 ALLOWED census 核验；此私有路径检查不增加公开 direct-route manager 字段，也不放宽其他 alias 的拒绝规则。

隔离 guest 仅可在所有 activation routes 已 masked 并完成唯一 reload 后，单独停止固定 multipath socket，再观察 `multipathd.service` 的停止窗口。root 以 held `/run` 与原 `multipathd.pid` 文件 handle、fanotify PIDFD 事件和全部 daemon 线程的 ptrace 退出门，证明唯一直属项变化是原 daemon 删除其 PID 文件；任何 exec、新后代、未知事件、额外目录变化或能力缺失均失败关闭。PID inode 只允许 nlink 1→0 与 ctime 变化，其他身份及原字节不变。只有同一活 observer 的内部 proof 才能一次更新所有保留父链的 `/run` ctime，并在最终排空前完成严格读回；不得重建 baseline、忽略后续 ctime 或由 supervisor 删除文件。

`stop_preflight` 固定记录 `already_quiet` 或 `stop_required`；前者要求 PID 文件不存在且 `stop_transition=null`，后者必须提供 actor、目录前后身份、PID inode、事件摘要和实际退出结算。host 核精确字段、分支与全部公开 `/run` parent 投影，并依外层冻结 protocol/installation/genesis 绑定；它不重建未导出的 kernel 事实。真实 fork fixture 已覆盖 observer 路径不等于实际 multipath 停止或完整 systemd closure，通过这些步骤之前仍不得推进依赖的 G2 验收。

固定 PostgreSQL 镜像的 DNS preflight 只允许两个显式 bind：只读 helper，以及原 `/run/g2/dns-preflight` 可写 scratch 映射到容器 `/var/lib/postgresql/data`，覆盖镜像声明的 volume 目标；额外匿名卷仍拒绝。helper 只在该 scratch 写结果，不执行 PostgreSQL entrypoint。DNS NAT 验证严格匹配固定 Docker 实现的六条规则：`POSTROUTING` 跳转匹配 `-d 127.0.0.11/32`，目标链内 SNAT 匹配 `-s`，端口必须等于实际 daemon-owned socket。两种地址谓词不等价，不接受任一形式的宽松匹配；这两处修正不改变 guest 网络规则，完整网络与 systemd closure 验收仍独立要求。

DNS probe 覆盖 PostgreSQL 镜像的 entrypoint，因此同时显式配置并读回 `StopSignal=SIGTERM`，不继承镜像的 SIGINT。probe 在结果发布前注册 TERM 接收，收到后退出 143；该状态表示固定程序响应 TERM，不声明内核默认信号退出。普通错误和超时仍失败，supervisor 继续独立核 held pidfd 退出、精确停止状态以及容器和网络移除，退出码本身不替代这些事实。

普通 source 路径在 systemd seal 前读取当前 `/boot/config-<release>` 的有界原字节，保存为仅进程内不可变 snapshot，绑定实际 boot ID 和 kernel release，并在 seal 前关闭文件。seal 后 audit 初始化重新核 boot/release 与原配置条件；root、架构、DMI、仅 loopback、io_uring 禁用及已有 FD、netlink owner/rules 检查仍实时执行。root-restart 的 fork 仅继承这份配置字节，不恢复任何 realm 权限；缺失、超限或身份变化均失败，不重挂载、不用磁盘缓存或默认配置。observer-only 不因此新增 audit 初始化。

每次 root 网络检查继承 caller 的绝对 deadline，并以最多 5 秒的 NETLINK_ROUTE 订阅窗口核 link/address/route 变化、丢失、截断和 namespace 身份。前后 GETLINK 请求与 ACK、完整 link dump、sysfs 身份和最终排空共同约束这一次检查；ACK 不代表全内核工作队列已排空，前后快照相等也不代替连续事件观察。veth 必须有明确 kind、ifindex、peer 和登记 bridge master；其 link-local 路由准入为 exact `fe80::/64`、source `::/0`、零 next-hop、flags=1。原 listener、IPv4、default/gateway 和其他 IPv6 拒绝规则保留，下述 multicast 类别除外。listener 负例只接受精确 TCP listener 拒绝，其他观察错误不能成为负例成功。

当前窗口已认证的 bridge 或 veth 另可接受 exact `ff00::/8`、source `::/0`、零 next-hop、flags=1；metric/ref/use 不作为身份条件。该类别包含多种 multicast scope，不等同 link-local，其隔离结论仍依赖实际 Docker Internal、QEMU 无 NIC、endpoint/进程与原短窗口检查。其他类别继续原有规则，动态通知仍拒绝，真实 guest 不删除路由。私有 fixture 新增 multicast 场景，保留实际 kernel 自动路由并要求 bridge 及至少一个 veth 上真实存在该类别；删除次数为零，前后完整路由 multiset 相等。所有场景独立核 exact `fe80::/64` 的存在，不能由 multicast 行满足其安装证明。parser 通过不代替真实 guest closure 或完整 G2 验收。

该观察器的 kernel fixture 只在独立子进程的私有 network/mount namespace 与新 sysfs 内准备固定设备；父进程保留 namespace FD 并核 named/held 身份与实际 child wait/pidfd。设备先以 down 状态注册，再单独设置 addrgen NONE，取得 ACK 后才 up。准备观察器在首次 UP 前订阅 LINK 与 IPV6_IFINFO，所有请求和通知共享同一绝对截止；分别解析 AF_UNSPEC、AF_INET6 和 AF_BRIDGE，要求三个设备各自的 IF_READY 通知，再各执行一次最终 GETLINK/ACK 验证身份、carrier、operstate 和 addrgen。观察持续至唯一清理和路由安装读回完成；ACK 不代表 workqueue drain。准备阶段只允许一次删除三个 fixture 设备上已观察到的 exact kernel `ff00::/8` 零 gateway 路由；不得将其推广到真实 guest 网络，亦不得因晚到路由而重试删除、过滤清单或重建 baseline。事件负例必须绑定生产观察器实际读取并拒绝的同一内核消息及受控操作的 ACK/child join，不能以任意异常代替事件检出。

私有 fixture 的两个固定 veth 保留历史 FORWARDING，并单独记录当前桥端口状态与待恢复状态。仅在尚未观察到 IF_READY、carrier=1 或 operstate=UP、且已 UP 并仍保持 UP 时，允许 FORWARDING 后收到 DISABLED；同帧就绪信号先永久关闭该例外。其他回退、已观察到的 carrier/operstate/UP 退化仍拒绝，缺省属性不能重开窗口。DISABLED 后必须收到真实 FORWARDING 通知才能清除待恢复状态；三个 ready 通知后排空、每个最终 GET、清理前及路由交接前均核两个端口实际 forwarding，不额外等待或重试。selection45 的原失败与完整通知保持不变；新 parser 通过不代表完整 fixture、guest closure 或 G2 通过。

首次 FORWARDING 之前另允许一个固定初始化状态：此前已观察到 UP 和 carrier=1、本帧仍 UP，且历史及本帧均未出现 IF_READY 或 operstate=UP 时，可接受 AF_BRIDGE DISABLED（包括重复通知）。本帧首次 carrier=1 不取得此例外；状态 1/2/4 仍拒绝。该分支保持 startup 窗口关闭并置 pending，不清除历史；真实 FORWARDING 才能清 pending。历史 FORWARDING 后不得使用此分支，ready 或最终 GET 也不能替代桥端口通知。selection46 原通知作为固定字节回归保留，历史 guest 失败不改记成功。

私有 fixture 的 route/late 负例必须保存实际 writer socket 的 port、固定 add/delete 请求及成功 ACK，并通过有界 seqpacket 从受控 child 返回；父进程核 namespace、完整请求和 ACK、真实 wait/pidfd 及单包后 EOF。透明 recvmsg 记录必须来自生产观察器实际读取的同一 datagram；只有首条致拒消息来自 kernel 的 IPv6 route multicast group，且 sequence/header port 与对应已 ACK writer 交换精确相等、路由语义匹配，才构成事件检出。header port 不是进程 PID，内核 sender 也不代表操作主体；缺 writer 证据、任意 sequence 错误或后续消息匹配均不足以通过。生产拒绝逻辑不变，失败诊断仅有界保存一次；selection47 的历史缺证据失败不补造为成功。parser 通过仍不代表真实 kernel fixture、DNS/systemd closure 或 G2 验收完成。

recreate/updown 在本次真实 IPv6 路由文件读取完成后、原始返回值交给生产验证前执行一次变更；只保留该次读取的原始内容，不过滤、替换或重新读取。原订阅持续覆盖此窗口，随后必须由生产实际接收的通知触发拒绝。该负例证明路由读取后至检查结束前的变更检出，不证明变更后的路由策略合法或拓扑已恢复；master/route 的首次 snapshot 后注入与 late 的末次 snapshot 后注入保持不变。selection48 中路由策略先拒绝、尚无通知记录的失败保留，不能当作事件检出成功。

当前 begin-only、多代 continuity、认证 ready 判定和上述载体实现仍是未完成真实 guest 验收的候选。本地 journal/真实 PostgreSQL fresh-observation 回归及测试 checkpoint 不构成 root/guest 权限证据。未见证 ready 的放弃必须经过认证判定、exact C80/EmergencyTarget 结算与 root ACK，不能把请求失败解释为未见证，亦不能补造 live ACK。不同 RID 的 Compose 排他保护保持有效，不能从 successor inventory 推导跨 RID 转移授权。完整 root/guest 崩溃矩阵、独立实现审查 finding 闭合与 G2 验收完成前，该候选仍不是 G2 READY；永久删除 HTTP 保持 CLOSED。

- Preview 在任何 reservation/operation 写入前要求九个 adapter 全部注册且 health proof 有效，并重新授权 current record。它绑定 actor scope、record/current revision、lock/auth/content-delivery epoch、dependency/backup/processor inventory、adapter readiness/preview 和 witness head；任一未知、缺失或漂移都 fail closed。
- Execute 只接受同一 preview 的 opaque token commitment/request fingerprint，重新授权并重新计算全部 binding 后才建立 provisional fence。相同 token/fingerprint replay 返回同一 operation；token 复用到不同 binding 是只读 conflict。
- delete worker 的 durable 顺序固定为 provisional fence -> append/resolve delete commit -> witness -> permanent fence -> propagate/read fence -> online purge -> content-free receipt。ledger append/outcome 不确定时持续保留 fence，不能猜测成功或补偿释放。
- 只有 sealed ledger absence proof 才能追加 `attempt_not_committed`；该 outcome 经 witness durable 后，才能以单调 `release_epoch` 释放 provisional reservation/fence。旧 owner/generation/observed-expiry 不能完成、释放或复活新 owner 的 operation。
- `record_core` 只拥有 `records`、revisions/subjects/tags/participants、drafts/checkpoints、`record_domain_activities`、`record_core_purge_receipts` 与该对象的 `content_delivery_epochs`。它在一个 transaction 内清除 current projection 与 exact surfaces、写无正文 receipt，并在 commit 前后验证 absence；不得宣称后续 child 的内容表已清除。
- Registry readiness、preview 与 aggregate receipt 编码保留闭合的 root-first adapter 顺序；online purge 必须按该顺序的逆序逐个 purge + verify，让 attachment 等依赖 surface 先释放 restrictive FK，`record_core` 最后删除 revision、draft 与 record root。
- Recovery 按 ledger sequence/hash 连续 replay，绑定 previous hash、witness proof、request fingerprint bytes、entry-type 对应的固定 surface allowlist 与 surface digest。delete commit replay 原子重建 terminal projection并清除 core；`attempt_not_committed` replay只建立/修正 terminal projection并释放 fence，不得 purge content。
- recovery 必须覆盖 existing operation、preview-only reservation 和无本地 projection 三种 cut point；重复同一 entry 幂等，identity/cursor/receipt/audit/fence 任一分歧都保持 fail closed，不推进 cursor。
- existing operation 从 `fenced` 进入 recovery terminal state 时，只能失效该 projection 读取到的 exact deletion-fence owner tuple（owner ID、generation、observed expiry）。已终态 operation、preview-only reservation、synthetic projection 和幂等 replay 不得按对象释放 fence；verification 也不得要求对象全局不存在 active fence。另一个较新 operation 的 fence 必须原样保留并继续阻止内容读取。
- 所有 PostgreSQL mutation 先执行 injected `AdmissionGate`，nil/error gate 产生 0 write。operation status 本地 row 缺失且 ledger+witness fallback 尚未证明不存在时返回 unavailable，不能伪装成 not-found 或完成。
- S3 physical GC 的两条消费者（`BlobGCWorker` 与直接附件 adapter purge）使用
  `BlobGCDeleter`：先 commit 完整身份的 durable claim，再精确维护删除，最后以
  owner/generation/attempt/精确 lease 和 receipt 身份完成 physical quota CAS。
  配置缺失在 claim 前拒绝，不回退普通 Delete。
- Local 的 publication、ordinary GC 和 direct purge 统一使用
  [持久本地 I/O 协议](../platform/delivery-primitives.md#scenario-本地持久写入跨进程锁与清理)
  的 `LocalCleanupCoordinator`：先取得跨进程 key guard，再重验并 claim，持锁至
  final/temp/fsync、receipt/quota completion。processor 的 reconciliation loop 调度
  local-only ordinary GC，进程关闭先 cancel/join，再关 Blob descriptor 和 DB。
- 已获准 V1 在 claim 后变为 noncurrent 时，两个 consumer 均只清除 V1，保留 raw
  外部 writer 写入的 V2；V2 无逻辑引用，不证明合法业务能绕过引用/publication/pin
  fence。物理删除后 completion 前失败时，新 owner 真实接管并递增 generation/attempt，
  以 `already_absent` 完成；旧 completion 拒绝，完成/resolve 重放不重复扣 quota。
- 附件 adapter 的 purge → verify → 同 receipt 重放保持 logical/physical quota、
  attachment/ref 归零及现有三类 adapter receipt；重放不重复物理删除。V1 保留其
  ledger 绑定，V2 local 必须显式传递当前 operation owner/generation/observed expiry，
  不从数据库偷读权限或把 V1 ledger 当 V2 authorization。
- local adapter prepare 先在共同 SQL fence 下持久化 root-independent pending targets，
  移交 obligation/原始身份并清理 owner refs，此时尚未取得物理 claim。关闭重开后仍可
  找到责任；共享字节只产生 retained receipt，不能冒充 absence。core-last/九 adapter
  及永久删除 HTTP 的总体准入继续独立验收，不能由这些局部结果关闭。
- S3 普通 publication reconciler 仍只调用 current-only Delete，不借用维护 GC 能力。
  prepared/unbound 的目标若已被相同内容的 metadata 或 upload part 保留，不领取
  destructive claim，也不补造 object version/consumed receipt；既有 metadata 的
  logical/revision refs 与 active pins 因而保持。bound intent 只按精确 version 判定；
  retry_wait 和过期 cleanup_claimed 接管同样不能绕过共享保护。
- publication cleanup_claimed/retry_wait 是持久引用发布围栏，lease 到期不解除它。
  Blob/part 登记在表锁后用 READ COMMITTED 的新 statement 检查该围栏；普通 owning
  published intent 仍可在同事务正常消费。涉及分片登记、metadata 登记及草稿释放的
  repository 事务显式选择 READ COMMITTED。本地跨进程排他、temp 恢复和 root-independent
  responsibility 另由 managed obligation/coordinator 证明；SQL fence 本身不证明远端能力已停止。
- bound S3 V1 已变为 noncurrent 时，普通 reconciler 返回 ErrBlobVersionMismatch，
  保留 V1 绑定并进入 retry_wait，不写 terminal outcome/receipt/completed_at；
  精确 V1/V2 bytes 与 current V2 均保持，不把 pending 解释为物理清除完成。
- import 登记使用 [pre-Put GC 生命周期观察](../platform/delivery-primitives.md#import-的-pre-put-gc-生命周期观察)，
  不能只查 active deletion：Put 后完成的 GC 仍须拒绝迟到引用。import 先提交时 worker
  不 claim 被引用 Blob；直接 purge 可删除原 source 的逻辑附件，但必须保留导入共享
  Blob、目标 refs 与 bytes。两个 claim producer 的锁后检查固定 READ COMMITTED。
- 导入采用 prospective accounting，不回补历史 counters、不新增导入限额政策：每个实际
  新 logical attachment 增加 logical size，仅新 metadata 增加 physical size；重复内容/
  跨 record 的 physical 只计一次，reserved 不变。每个 record 一次 quota update，
  计数或 quota_version 溢出 fail closed；metadata、attachment、ref、quota、
  revision、origin 与 import finish 同事务回滚，applied replay 不重复计费。

#### 新 issuer 的备份库存披露与绑定

- `BackupInventorySnapshot` 的 `available` 必须有非零 digest 和通过原
  `ManagedBackupSummary.Validate` 的 summary。只有成功枚举的空集合才使用
  `retained_copy_count=0`；原 summary 的数值/保留期语义不变。
- `unavailable` 必须同时没有 digest 与 summary。缺生产 retained-backup enumerator
  时使用明确的 `UnavailableDeletionBackupInventorySource`，先检查 context/object；
  nil provider、未知状态、混合 shape 均拒绝，实际 provider 的 cancellation、identity、
  parse 或数据库错误原样返回，不能 catch-all 降级为 unavailable。
- 内部 JSON 使用独立 DTO：`backup_inventory` 包含 `availability`、
  `inventory_digest`（64位小写 hex 或显式 null）、`managed_backup`（原字段或显式
  null）；`completion_scope` 固定为 `online_only`。未知时同时披露库存未知、仅在线
  清除、不承诺删除备份，不填假 LedgerHealth，不改变旧 HTTP/Web DTO。
- V2 binding 域为 `houfeng.record-deletion.issuer-preview.v2`，绑定 actor、object
  CAS/lifecycle/authorization、adapter readiness/preview、dependency/processor/
  obligation inventory 后编码 availability；available 为1及 digest/原 count/days/
  expiry framing，unavailable 为2且不带这些值，最后编码固定 completion scope。
  二次计算出现 availability、digest 或 retention 漂移返回 ErrDeletionPreviewStale。
- `NewPostgresRecordDeletionIssuer` 实现内部 V2 `Preview` 与 `Execute`，不挂 HTTP（永久删除 HTTP 继续关闭）。`record_deletion_previews` 包含不可变、唯一非空 `reservation_id`；`record_deletion_preview_create` 保存该身份，`record_deletion_preview_lock` 按 `{reservation_id, actor_id, record_id}` 锁定并返回持久 commitment，由 `Execute` 以 `Token.MatchesCommitment` 常量时间比对后再调用 `record_deletion_issue`（禁止把 raw token 送入 SQL 或日志）。
- issuer 在 APP 事务前完成外部 birth continuity/certification 与 backup 观察，在 READ COMMITTED 事务内按固定锁序执行：admission → graph shared + comparison writer →（Execute 时）preview_lock → Rank 10 actor/group/membership/source → reservation/epoch/deletion_fence/object_content_lease/gate → Rank 55 `record_birth_continuity_assert` → root 与 `record_birth_certification`（certifiable `rec2_`）及当前授权校验 → quota → Rank 73 `blob_objects` 与 `attachment_upload_parts` 的 `SHARE ROW EXCLUSIVE` → Rank 74 `record_deletion_inventory_lock` → 同事务无二次 SQL 的九 adapter health/preview 与三份有序 inventory（`HOUFENG-DELETION-GRAPH-V2`、`HOUFENG-DELETION-PROCESSOR-V2`、`HOUFENG-DELETION-OBLIGATION-V2`）。
- live serving lease 阻止 `Execute` 并映射 `ErrObjectContentLeaseLive`（保留 PostgreSQL cause 且不改 epoch/gate）；排空后若库存未漂移可直接确认原 preview，漂移返回 `ErrDeletionPreviewStale`。签发后旧 serving token 的 Renew/Assert 返回 `ErrLostOwnerLease`，新 Acquire 在 live fence 阶段返回 `ErrDeletionFenceLeaseLive`，core 删除 epoch 后返回 `ErrContentDeliveryEpochMissing`。已绑定 operation 的 exact replay 在 root 删除后仍可验证当前 enabled admin、token commitment 与持久绑定并幂等读回原 operation。

#### V2 worker 与 compound owner 候选

- `NewV2DeletionWorker` 只使用 claim/complete repository 和现有 `RegistryPurger`；V1 constructor、27 列 scanner、ledger/witness 与 retry-required 状态机独立。V2 claim 使用专用 20 列 scanner，核 reservation/operation/fence 的 owner ID、generation、exact expiry 全等及 DB observed-at 早于 expiry。claim 只接管真实过期项，显式 renew 只接受 live 原 tuple；worker 不后台 renew，同一次九 adapter attempt 不更换 tuple。
- activity/search/evidence 与其余 adapter 共用原 registry，按真实 contract version 选择 V1 ledger 或 V2 八字段受限命令。V2 wrapper 每次独立鉴权，复用现有删除/metadata/receipt 算法，不令旧 legacy lock 接受 V2；purger 保留真实 adapter 错误 cause。实际 purge reverse/core-last，aggregate 按既有 root-first 九 adapter framing 从实际不可变收据重算。
- terminal 候选重查完整 direct/target/draft/upload/processor/export/import route 与 durable operation/handoff 责任。workspace 删除前保留完整排序 target 集合的 count/commitment、实际锁定 tuple 和 purge receipt；该 manifest 证明受信事务的关联与集合完整性，不宣称原 workspace receipt 的 hash 绑定其未编码的 path/job/attempt。旧 per-obligation cleanup receipt 未保存 completion command 时仍是 opaque legacy fact，不补造 bytes 或独立 rehash 证明；V2 GC command、inventory、namespace/key/outcome 与 adapter receipt 则须实际重建。
- comparison 的 canonical health/preview 通过真实 runtime repository 读取，purge/verify 通过独立 deletion-worker repository 调用受限命令；`NewPostgresComparisonDeletionStore` 显式组合两条通道，不扩大 worker 的 evidence/payload SELECT 权限，不跳过 canonical 校验。所有 V2 purge/verify、prepare/finish、operation-bound cleanup/GC 与 outbox 命令都在锁等待结束、最后效果和结果读取之后，以输入的原 compound owner tuple 完整复核 reservation/operation/fence、永久 gate 和 fresh DB clock。最终复核失败须向外抛出并回滚该命令效果与 receipt；不得在最终复核之后再读取内容、写收据或执行新效果。
- V2 使用**命令内最终 live 授权与事务持锁排他**：source/namespace 准入后，依次持有 graph shared、comparison exclusive transaction advisory lock，再按 reservation→存在的 epoch→必须存在的 fence→gate→operation→存在的 root→原 affected 资源顺序取锁。所有命令入口保留 direct-login 和 READ COMMITTED 要求；comparison 不先 shared 再升级。成功命令的保护持续到 COMMIT/ROLLBACK，允许提交时刻晚于原 expiry；后续命令仍独立检查原 tuple，过期即拒绝，不自动续租。接管者等待原事务释放后，用实际持久状态重验：非终态过期项递增 generation，已提交终态无工作。root/epoch/fence 的缺失由 surviving reservation/gate、共同锁前缀及受限 creator 协议保护，不宣称锁住不存在的行。
- finish 在 receipt/完整责任锁等待后再核原 owner，终态效果后核 operation/reservation/gate/receipt、两 owner 为 `''/0/NULL`、exact fence 已删除，然后以捕获的原 expiry 和独立语句中的 fresh `clock_timestamp()` 完成最后授权。exact terminal replay 只在当前 worker/source 准入和全部终态身份不变量成立时只读返回，允许原 expiry 已过；不同 identity/digest 拒绝。同事务后续命令不能恢复 owner/fence 或重新开启终态。V2 不使用 deferred owner/terminal deadline checks；V1 queue 与 deferred 检查继续独立保留，queue 对 V2 operation 明确拒绝。
- savepoint 的锁与效果遵循 PostgreSQL 事务语义：回滚到命令前的 savepoint 撤销二者；RELEASE 并入父事务；命令后创建的 savepoint 不能撤销此前保护。PREPARE TRANSACTION 保留锁与效果直至 COMMIT/ROLLBACK PREPARED，包括隔离 APP 重启恢复；不以 backend 退出或提交时钟代替锁证明，不新增生产 2PC 禁令。未知 prepared 事务继续阻止原 offline drain，不自动处理。
- 历史隔离 PG 反例已证明 EXEC-only LOGIN 可用 `SET CONSTRAINTS ALL IMMEDIATE` 提前清除 deferred 检查，随后过期 COMMIT 仍成功；该证据保留，不改称旧 actual-COMMIT 保证已修复。上述新合同已获批准，并已在绑定候选上完成真实并发、savepoint、最终效果等待（含外部 TOAST 物化反例与修复）、直接调用隔离、2PC/APP 重启与物理 consumer 恢复验收及独立 GPT 复核闭合；永久删除 HTTP 保持 CLOSED。

#### V2 Markdown / Comparison adapter 与受限命令

- `NewMarkdownClientAdapter` 与 `NewComparisonAdapter` 提供实际库存的 health/preview
  和 V2 purge/verify，尚未纳入生产九 adapter 装配。单个 adapter health 成功不证明
  完整 dependency graph、source handoff、writer fence 或删除签发已就绪。
- 两者的 purge/verify 共用严格八字段 JSON 命令：`operation_id`、`record_id`、
  `reservation_id`、`fence_epoch`、`owner_id`、`owner_generation`、`owner_expires_at`、
  `surface_digest`。拒绝缺失、重复、额外字段及错误类型；epoch/generation 为正整数
  JSON number，expiry 必须精确到 PostgreSQL 可表示的微秒。调用方的 reservation
  和 fence 必须与锁定的持久 reservation 完全一致，不从数据库替换调用方身份。
- C81 仅允许直接登录的 deletion-worker 调用四个 purge/verify 函数；runtime 无该
  EXECUTE 权限，worker 无 preview/issue 权限。命令在效果和 receipt 重放前检查
  source admission、V2 reservation、gate、cleanup event 和精确且未过期的 owner
  tuple；等待锁或删除后再次检查 owner，失败回滚效果和 receipt。
- gate 插入在同一事务生产四个 client scope 的 cleanup event。metadata purge 保留
  该 event、targets 与浏览器 ack/receipt；服务端 metadata absence 不等于离线浏览器
  副本已经清除，未知副本继续以 unknown/null 披露。
- Markdown 只清除目标 draft/workspace metadata。Comparison 清除以目标为 destination
  或 source 的 workbench/save-attempt，以及目标拥有的 result 的 dependency/completion
  索引；其他 owner 的结果及认证来源不因引用目标而删除。规范要求真实签发后验证
  A-purge/B-retention、接管、expiry rollback 和 receipt 重放，不能用只读预览代替。
- 当前局部运行证据覆盖真实签名 comparison、两个 adapter 的 health/preview、受限
  角色拒绝以及不存在 operation 的无写入拒绝；未覆盖合法 V2 operation 的成功 purge
  或错误 reservation/fence 拒绝。一致 comparison 决策读取已实现；接线仍需持久 birth/cutover
  证明、全 writer 协调及独立 worker 生命周期，不能将缺失历史视为已关闭。

#### Comparison 只读库存与保留副本预览

- `ComparisonDependencyIndexHealth` 仅报告实际 canonical 结果集的已验证 digest，
  不代表完整 comparison deletion adapter 的 health/readiness，也不注册半成品 adapter。
- index health、maintenance、adapter health 与 preview 共用一个私有 decision reader；公开
  入口保持 admitted REPEATABLE READ READ ONLY。reader 用单条参数化 SQL 在同一 statement
  snapshot 内读取 target/CAS、surface/function、原 PostgreSQL jsonb::text 库存字节和全部结果。
  header 后按 snapshot ID 的 C 顺序逐行解码 canonical payload，每个结果最多读取七条有序依赖；
  LEFT JOIN 明确保留缺 payload/completion 的损坏行，nil target 也验证全部 canonical 结果。
  未索引、损坏或未证明的导入来源返回 `comparison_dependency_index_incomplete`，
  不返回假零或部分 disclosure；目标 CAS 不匹配返回 `ErrDeletionPreviewStale`。
  index/health/preview/impact digest 域和 length framing、库存排序与 UTF-8 字节保持不变。
  单 statement 只保证读取一致性；issuer 必须等待统一 writer fence 后在 READ COMMITTED 下
  捕获一次 decision，并让该次 comparison health/preview 共用它，才能结合 fence 保护 gate 提交。
- 比较库存的原始 runtime DML 已撤销；数据库命令和相关输入 guard 与同一 comparison fence 协调，包括库存当前为空时的新行写入。V1 evidence 清理的 guard 只证明持久 operation 的清理资格、epoch 与 fence 参与；数据库内的 live owner 不等于 SQL 调用方持有 exact owner authority。V1 调用方绑定、真实 V2 issuer 与独立 worker 成功清理仍分别保持未验收，不能因决策锁反例通过而宣称整体授权边界完成。
- 保留副本按同 project、其他 owner 的 `DISTINCT result_snapshot_id` 聚合。
  同结果引用目标的多个 revision/snapshot 只计一次；目标自己拥有的结果不计，
  复制结果按其认证来源计算，不因复制来源的 owner 等于目标就推断依赖目标。
  返回值只有聚合 known count 与绑定 digest，不披露其他 record/result 身份。
- dependency/impact 绑定已验证索引、目标 owned dependency/completion、相关 client
  workbench/save-attempt 及保留结果集合。预览不改 B 的正文、payload、lineage、
  refs 或 client 状态，不签 gate、写 receipt、生产 cleanup event 或执行 purge。
  真实 A-purge/B-retention、九 adapter 准入仍需各自完整集成接受。


### 4. Validation & Error Matrix

| 条件 | 预期行为 |
| --- | --- |
| adapter 缺失/重复/额外、surface 重复、health error/unknown/unhealthy | `ErrDeletionSafetyUnavailable`；0 preview reservation、0 ledger mutation。 |
| preview 后授权、record CAS、dependency/inventory、adapter digest 或 witness head 漂移 | stale/conflict；不建立 provisional fence。 |
| delete append/resolve 或 outcome append 结果未知 | operation 保持 `ledger_commit_unknown` / `release_pending` 且 fence 保留；等待 authoritative resolution。 |
| delete commit 已 witness，purge/fence propagation/receipt 失败 | `retry_required` 记录 exact retry stage；不得回退为未删除或释放 fence。 |
| 合法 V1 fence-propagating owner 仍有同 object 的 live deletion/content lease | worker-only bool 检查返回 `false,nil`，保持等待；drain 后 `true,nil`，检查本身不写业务行、不触发 retry-required。 |
| sealed absence 未证明或 `attempt_not_committed` 未 witness | 不释放 reservation/fence、不恢复普通读写。 |
| core receipt 含正文、identity 漂移、surface/receipt digest 不符或仍有 owned row | rollback/verification error；operation 不得进入 `online_purged`。 |
| recovery cursor 不连续、previous hash/witness/fingerprint/surface digest 不符 | `ErrRecoveryContractUnavailable`；0 content/projection/cursor change。 |
| recovery 要释放的 exact owner tuple 已被续租、替换或不存在 | `ErrRecoveryContractUnavailable`；transaction rollback，不得触碰当前 fence。 |
| terminal/idempotent replay 时对象存在另一个 active fence | replay 按自身 receipt/audit/cursor 合同成功；另一个 fence 的 owner/generation/expiry 完全不变。 |
| status projection missing 且无 authoritative fallback | `ErrDeletionStatusUnavailable`，由 HTTP 映射为 opaque 503。 |
| V1 portability inventory 含 unresolved/PDF 或 digest/source→target 关联不符 | 保留原 snapshot 与 source；所有匹配 V1/ordinary/V2 GC dispatch、complete 和 terminal receipt 拒绝，零物理 I/O。 |
| 最后 retained owner 移除而无同 identity durable handoff，或 late writer 新增 exclusive target 引用 | 事务拒绝；既有 owner、inventory、bytes、quota 和清理责任不丢失。 |
| manifest、client event、九 receipt 或 GC target/task set 有 missing/duplicate/orphan | complete 拒绝且 operation owner/epoch 不变；terminal 不引入无 manifest 的兼容旁路。 |

### 5. Good / Base / Bad Cases

- Good：delete commit 已 witness，worker 先永久 fence，再逐 adapter purge/verify；core receipt 不含 record/revision ID 或任何业务内容，最终 operation 为 `online_purged`。
- Good：ledger 明确证明 delete 未提交，worker 追加并见证 `attempt_not_committed` 后释放 provisional fence；record 内容完全保留，同 token replay 仍返回同一 `not_committed` operation。
- Base：生产装配已注册 core、attachment、evidence、search、activity、collaboration、portability 七个 adapter；`record_markdown_client` 与 `record_comparison` 未在该装配中注册，且永久删除仍为关闭模式。九个 adapter、ledger/witness 与全部 readiness 条件未闭合时，preview 不签发 token；不能把已有七个 adapter 解释为永久删除已上线。
- Base：应用投影丢失后 recovery 从连续 ledger cursor 重建 terminal projection；重复 replay 不重复 receipt/audit，也不跳过未知 entry。
- Good：operation A 已终态后 operation B 建立新 fence；A 的幂等或延迟 replay 不更新 B 的 fence，B 继续让 Records read 返回 reserved。
- Bad：以“后续表当前为空”省略 adapter，收到 ledger timeout 就释放 fence，或仅凭 `record_purge_operations` 无 row 返回 404；这些都会把未知状态误当作安全结论。
- Bad：recovery 以 `(project, kind, object)` 无条件 expire fence，或在幂等验证中要求 active fence count 为零；前者会破坏较新 operation，后者会让合法旧 entry replay 永久卡住。

### 6. Tests Required

```bash
go test -race ./internal/center/recorddeletion ./internal/center/store \
  -run 'Deletion|Purge|Recovery|Reservation' -count=10

scripts/test-record-platform-integration.sh postgres -- \
  go test ./internal/center/store \
  -run '^TestPostgresIntegrationRecord(Platform|Deletion)' -count=1
```

- Registry/service tests必须覆盖 exact nine-adapter readiness、preview reauthorization/CAS/digest drift、same-key replay/reuse、unknown ledger outcome、witness pending 和 sealed-absence-only release。
- Worker/store tests必须覆盖每个 durable cut point、stale owner、permanent fence、retry stage、content-free receipt、core exact ownership 和 purge rollback。
- V1 真实 PostgreSQL continuation 保留 `PreviewReserveAndExpiredReplay`、`ReserveCASDriftRollsBackFenceAndOperation`、`DeleteCommitPurgesCoreAndDeliveryEpoch`、`NotCommittedTakeoverRejectsStaleOwnerAndRestoresRead` 四个公开测试名，并实际运行 `TestPostgresIntegrationRecordDeletionV1PermanentFenceAuthority`。direct worker 下 live content/deletion lease 的 `false,nil`→drain 后 `true,nil`→read-fenced、错 tuple/hash/状态与 stale owner 的 `42501`、runtime/admin/PUBLIC 无 EXECUTE、worker 原表 SELECT 拒绝及只读零变更均须验证。九 logical adapter 分别核其实际 surface receipts（attachment 三 surface、evidence 专属形状），不能以九行单表或 core-only purger 代替。V2 issuer 的 `revision_cas_drift_zero_writes` 与 `post_issue_rollback` 分别验证真实 revision 提交后的旧 token 拒绝，以及真实 issue 写后 Commit 注入、defer rollback、无持久 residue 和恢复 seam 后成功；任何所选 parent/subtest skip 均不构成验收。
- `TestPostgresIntegrationRecordDeletionV1AdapterContinuation` 的 `client_event_and_outbox`、`known_local_and_shared_handoff`、`unresolved_inventory_no_dispatch`、`authority_and_crash_cuts`、`reference_serialization_and_terminal_sets` 必须真实 RUN/PASS。client/outbox 以十二字段 V1 authority、无 gate、真实 metadata 与超过 100 项的 batch 证明 event/receipt 原子、root-independent target/ack、predecessor/started/late-provider handoff。local 用真实 processed bytes、已登记 obligation 和 namespace，证明共享 A 保留 B、最后 B durable task 与 quota 一次；unknown 原值不清空、不补 identity，不经普通/V2 GC 绕过。两连接交错、COMMIT response loss、bind/physical return loss、takeover、constraint/expiry rollback 和 terminal set tamper 均核真实持久效果。
- obligation-only 无 artifact 的 known local、known exact-version S3、remote_unknown 三类必须独立重算完整 inventory digest 和 source→target membership。known 两类经真实各自物理协议完成；remote_unknown 保留完整 snapshot 并拒绝所有替代 dispatch。`RunV1DeletionDeleteCommit` 必须以非空合法 local portability/client/outbox 输入完成真实九 adapter，先拒绝缺 receipt 的非零 bogus aggregate，再核 actual aggregate、core-last、owner 清除和稳定终态。
- S3 profile 另须真实运行 `TestPostgresMinIOIntegrationRecordDeletionV1Portability` 的 `historical_version_preserves_current`、`prepare_commit_unknown`、`delete_ack_loss_process_restart`、`route_binding_mismatch`：授权 historical version 删除、current/neighbor 原 bytes 保留，未确认 claim 零 I/O，真实删除后 kill/接管 exact absence，endpoint/bucket 漂移零 I/O。另核多 artifact 去重、外部引用 handoff、storage/null-version/receipt tamper 拒绝和 private claims 不混入 attachment 计数。该固定 MinIO fixture 不证明未知 provider 的 D3。
- Recovery PostgreSQL tests必须覆盖 existing operation、preview-only reservation、synthetic terminal projection、delete/not-committed replay、连续 cursor、幂等重放、较新/无关 active fence 的 exact tuple 保留，以及 receipt failure 全事务回滚；runner 中任何 `SKIP` 都不能作为验收证据。
- `scripts/run-records-integration.sh --profile s3` 必须真实覆盖
  `TestPostgresMinIOIntegrationAttachmentBlobGCHistoricalVersionWorkflow` 的
  claim→V2 与删除后接管两个场景，以及
  `TestPostgresMinIOIntegrationAttachmentDeletionPurgesHistoricalExactVersion`；
  必须观察精确 V1 absence、V2 current/bytes 保留和 quota 一次性结算，不能用普通
  Stat 对 noncurrent 的拒绝冒充物理 absence。
- local/S3 的 `TestPostgresIntegrationImportAttachmentGCFence` /
  `TestPostgresMinIOIntegrationImportAttachmentGCFence` 必须覆盖真实 Apply 与两条 GC
  消费者双向顺序、active baseline、Put 后 claim/completion、fresh retry、未提交 claim
  和默认 REPEATABLE READ 连接；同时核对 exact bytes/absence、completion、目标
  revision/ref/origin/job 及 quota。无 token、错 key、身份冲突、数据库错误、溢出与多
  record 后项失败均不得留下部分导入；纯内存 importer 不构成并发或权限证据。

### 7. Wrong vs Correct

```go
// 错误：本地没有 operation row 就把不可证明状态当成权威不存在。
if errors.Is(err, pgx.ErrNoRows) {
	return ErrDeletionOperationNotFound
}

// 正确：ledger + full witness 未证明不存在时保持 unavailable。
if errors.Is(err, pgx.ErrNoRows) {
	return ErrDeletionStatusUnavailable
}
```

```go
// 错误：只注册当前已有数据的 core adapter，就开放不可逆删除。
registry, _ := NewRegistry([]Adapter{core})
_ = startPermanentDelete(registry)

// 正确：完整闭合集和每个 health proof 都通过后才允许 preview/execute。
snapshot, err := registry.RequireReady(ctx)
if err != nil || !snapshot.Ready() {
	return ErrDeletionSafetyUnavailable
}
```

```sql
-- 错误：按对象释放，可能命中另一个 operation 的新 fence。
update public.deletion_fence_leases
set expires_at = transaction_timestamp()
where project_id = $1 and object_kind = $2 and object_id = $3;

-- 正确：仅当原 recovery projection 仍是 fenced 时，绑定它读取到的
-- owner_id、owner_generation 与 exact expires_at；terminal replay 不执行该 SQL。
```

---
