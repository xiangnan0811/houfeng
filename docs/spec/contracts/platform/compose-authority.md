# Compose authority 与协调恢复

## Scenario: single-host Compose Records authority and membership heartbeat

### 1. Scope / Trigger

- Trigger：修改 `internal/center/recordauthority/`、Compose deploy init/authority runtime、`0060` 历史 heartbeat、未合并 `0081` 的 membership/worker admission、current APP ACL fragment、activation/rotation projector、deployment-ID file loading，或 `deployment_contract_state` / `deployment_membership` 的 Compose recovery contract。
- 此场景覆盖 single-host ordinary Compose 的 V2 signed membership 与历史 source-enrolled V1。signed state 位于 PostgreSQL 外；activation/rotation 复用已有 migrator-owned projector，renewal 使用受限 heartbeat。不得让 db-init 直接编造 contract/membership，也不得给 Center allow gate。

### 2. Signatures

```go
func recordauthority.CreateComposeState(root string) (recordauthority.VerifiedComposeState, error)
func recordauthority.LoadComposeState(root string) (recordauthority.VerifiedComposeState, error)
func recordauthority.PersistActivationReceipt(root string, state recordauthority.VerifiedComposeState, receipt []byte) error
func recordauthority.VerifyActivationReceipt(root string, state recordauthority.VerifiedComposeState) error
func recordauthority.PreviewComposeMembershipV2(root string) (recordauthority.VerifiedComposeState, error)
func recordauthority.PrepareComposeMembershipV2(root string) (recordauthority.VerifiedComposeState, error)
func recordauthority.PersistMembershipRotationReceipt(root string, state recordauthority.VerifiedComposeState, receipt []byte) error
func recordauthority.VerifyMembershipRotationReceipt(root string, state recordauthority.VerifiedComposeState) error
func recordauthority.MarshalMembershipHeartbeatCommandV2(state recordauthority.VerifiedComposeState, issuedAt time.Time) ([]byte, time.Time, error)
func recordauthority.MarshalMembershipHeartbeatCommandV1(state recordauthority.VerifiedComposeState, issuedAt time.Time) ([]byte, time.Time, error)
func migrate.ProjectComposeMembershipV2(ctx context.Context, db *pgxpool.Pool, roles platformmigrate.AppRoleSetV2, activation, rotation []byte) ([]byte, error)

func deploy.InitializeCompose(ctx context.Context, config deploy.ComposeInitConfig) error
func deploy.RunComposeAuthority(ctx context.Context) error
```

- `ActivationCommand` 始终从原 V1 genesis 派生；V2 `MembershipRotation` 从已验证 genesis 和 successor 重建，不能信任 caller 改写的 pointer、membership 或 digest。持久化前重新加载并比较完整 state。
- ordinary database boundary 是 owner-only `public.record_platform_compose_inventory_upgrade(bytea, bytea)` 与 authority-only `public.record_platform_compose_membership_heartbeat_v2(bytea)`；processor 另经 runtime-only `public.record_processor_worker_admit(text, text, text, text)` 在效果事务中核 membership。历史 V1 仍使用既有 activation projector 与 `public.record_platform_compose_membership_heartbeat(bytea)`。

### 3. Contracts

- State root 必须完整验证类型、mode、长度、规范 JSON、Ed25519 签名、ledger 链、deployment、原始 database credential digest 与固定 inventory。真实 V1 是可验证的历史输入；原 genesis、key、credential、activation command 和 activation receipt 保持字节不变。V2 是追加的 `private/membership-successor.json`，带 sequence 2、genesis previous hash、相同 public key/credential digest/base activation digest、mutation ID 和固定有序 membership。存在损坏 successor 时拒绝，不能回退 V1。回归使用修改 writer 前捕获的真实 V1 producer 夹具，不用新 writer 重造前驱。
- V2 顺序固定：`compose-center` / `api` / `records.runtime`，epoch 2、fence 1、LB=true、queue=false；`compose-processor` / `worker` / `records.runtime`，epoch 2、fence 1、LB=false、queue=true。project 均为 `default`。这些 signed flags 是授权 inventory，不是进程存活证明。规范 JSON 使用声明字段顺序；domain-separated digest 对 UTF-8 domain、零字节、payload 字节长度的 uint32BE 和 payload 做 SHA-256。identity digest、rotation mutation、successor entry 和 trust head 分别使用 V2 域，rotation 从 genesis 精确推进 ledger/identity/trust 至 2，adapter policy 与 fence 不变。
- `PreviewComposeMembershipV2` 只验证并推导，不发布文件，也不证明 DB 已应用；`PrepareComposeMembershipV2` 发布或重验确定的 successor，successor 存在本身同样不是 DB 应用证明。source 未启用且 birth 从未安装的普通路径，fresh/inactive DB 可准备并原子投影；已经 active 的普通 V1 必须通过显式 cold upgrade，普通 `InitializeCompose` 拒绝自动升级。active DB 与缺失、损坏或不同的 state 拒绝，不能在 active contract 上重新生成。source-enrolled 路径保留历史 V1 及其外部 W2/maintenance/source guard，不因 ordinary V2 增加 source 升级支持。
- `ProjectComposeMembershipV2` 在同一 `SERIALIZABLE` transaction 内验证 direct migrator、embedded C81 source、manifest、ledger、全部 role 与 catalog 后调用受限 wrapper，只有 COMMIT 成功才返回 rotation receipt。fresh activation + rotation + 两行 inventory 同事务；已 active 的 exact V1 center 可保留原 `created_at` 并升级，即使旧 lease 尚未过期，不能覆盖 processor 冲突或其他未知 identity。存在 immutable command anchor 时 replay 精确比较原 activation、rotation、receipt、manifest/source identity 与完整 head/rows，不重跑 genesis CAS、不写状态；OPEN 的匹配 launch 或 CLOSED/absent launch 均可 replay，真正 transition 必须 launch CLOSED/absent。receipt 仅在成功 COMMIT 后持久化到独立 `private/membership-rotation-receipt.json`；未知 COMMIT 通过新事务 exact replay 判定，冲突或缺少 anchor 的已旋转状态拒绝。
- 新行的 `created_at` 使用真实事务时间，初始两个 membership 都 LB=false、queue=false，placeholder expiry 为事务时间加 90 秒，以满足 frozen `heartbeat_expires_at > created_at` CHECK；不得伪造回溯时间或把 pending 当作 admitted。首次 V2 heartbeat 同事务把两行设置为 signed inventory 的最终 flags 和相同的新 90 秒 expiry，之后只接受完整 all-pending 或完整 all-active pair，renewed/elapsed expiry 不是 identity 漂移。任何 partial pair、混合 flags、不同 head/identity 或错误角色均整体拒绝。
- V2 heartbeat 是 240-byte closed command：projection header version 1、operation 4、9 fields，依次为 deployment、genesis activation bundle digest、ledger sequence/hash、identity epoch/digest、fence、issued/expiry Unix seconds；sequence/epoch 均为 2、fence 为 1，整数必须在正 PostgreSQL bigint 范围。issued 在 DB 时钟 ±30 秒，expiry 必须恰为 issued+90 秒、未来且距 DB transaction time 不超过 120 秒。锁住 source/launch、当前 manifest/contract，再按 center→processor 顺序锁行并原子续租；竞争按现有 NOWAIT/整事务重试规则处理。
- `houfeng_records_authority` 是独立受限 direct login，仅获得 database connect、`public` usage、相应 heartbeat EXECUTE 与 `source_maintenance_status()` / `record_birth_launch_inspect(bigint, bytea)` 等窄只读入口，不获原始表 DML、launch mutation 或通用 projector EXECUTE。V2 heartbeat 还在函数内核对固定 authority direct session；runtime、admin、worker 和 PUBLIC 无 delegated heartbeat EXECUTE。migrator 是函数 owner，不因此把通用 projector 权限授给应用角色。
- 0060 的 177-byte V1 heartbeat 和 epoch-1 signed input 继续服务历史 source-enrolled 合同；0081 对其实施 epoch-1 限制，V1 命令不得续租已切换的 V2 domain，既有 source membership effect trigger、source closure 和 retirement 保护不移除。ordinary authority 每次续租在共享 management guard 内重载 durable state，验证 activation/rotation receipts、当前 launch 与 DB 后调用 V2；source-enrolled authority 维持 V1/W2 验证。30 秒刷新、有效 receipt 内健康，停止时不删除 membership；租约到期后效果准入关闭。
- Center 与 processor 只读各自的公开 launch binding；文件不含 producer、reader 或管理秘密。Center 默认路径为 `/run/houfeng/records-authority/birth-launch.json`，processor 为同目录 `birth-launch-processor.json`，可由 `HOUFENG_RECORD_BIRTH_LAUNCH_FILE` 指定受保护绝对路径。文件必须为 UID10002:GID10001、0440、单链接普通文件；祖先目录不得由运行用户写入或经符号链接绕过。启动器检查规范 JSON、process kind、配置 deployment ID 和实际 `/proc/self/exe` 摘要，缺文件不是 never-installed 证据。
- C81 uses `AppRoleSetV2` with a mandatory, distinct `houfeng_record_deletion_worker` direct login and its own operator-managed password. The bootstrap, runtime, platform-admin, deletion-worker and migrator passwords must be pairwise distinct; the authority credential remains independently derived from signed state. Secret staging exposes the worker credential only to db-init, not Center/processor/authority. Current convergence takes all three application-role bindings and publishes privilege body V2; old V1 manifests retain their original two bindings. A matching schema/ACL does not activate a deletion worker or enable permanent-deletion HTTP.
- Source-enrolled Compose 的独立 W2 和受控 maintenance 配置/持久 flock anchor 位于 APP、attachments、signed authority 的恢复单元之外，不能随其回滚。普通 init/migrate/bootstrap/finalize/activation、secret staging 和每次 authority heartbeat 均使用同一受控目录、同一 Linux kernel 与共享 flock domain 的 shared guard；恢复维护持 exclusive guard。锁文件不得 unlink 或替换，身份不匹配 fail closed；跨 host 的 flock 不互斥，不能用它证明旧 endpoint 隔离。已 enrollment 不得通过省略配置关闭检查；正常参与者须核对 APP/W2 domain、active generation、recovery ID、W2 completed handoff 与 activated enrollment，W2 active 本身不够。
- 当前 generation 的角色 credential commitments 必须验证，旧 `.env`/staged secrets 不得作为修复手段回写旧密码。普通部署的 V1→V2 迁移需要上述 cold 边界；source-enrolled 继续既有 V1 合同，轮换/恢复不能绕过当前 commitment 与受控交接。
- W2 同时保存 purpose/principal 绑定的 credential commitment 与独立的 secret fingerprint。后者只对已核验的高熵明文密码做 domain-separated、长度前缀 hash，不含角色名、用途或 generation；恢复 staging 在同一 authority 锁内拒绝任何历史 generation 使用过的 fingerprint，换角色名或用途不能复用旧秘密。exact 重放必须同时匹配 principal、commitment、fingerprint；数据库不保存明文，fingerprint 不替代真实凭据验证或完整交接证明。
- 恢复中的 fresh W2 producer 可先以受约束 NOLOGIN 角色绑定，仅限当前 generation、mode=recovering 且 exact RID 尚未完成；reader/recovery 与普通 active 模式不借此放宽 LOGIN 要求。绑定和 credential staging 不开放 LOGIN，producer 的实际效果仍要求当前角色、active generation 与 completed handoff。
- 首次启用是外部 fencing 的 offline 操作：新发布的 guard 无法排空已经在无 guard 下运行的 callback。操作者必须实际停止、隔离并等待旧 writer/管理操作及 endpoint 退出，同时阻止其重启，再进行 enrollment；仅创建 guard、等待 heartbeat 过期或调用 `enroll-offline` 都不是安全 live cutover 的证明。相关 helper/围栏接线不等于已提供完整可运行 recovery coordinator/CLI，也不证明实际部署或完整恢复已完成；不能恢复服务直到上述准入及 source reconciliation 可证明。
- 受保护 source maintenance 配置持久保存 `birth_installed`、稳定 lineage、协议版本和独立 birth reader 连接。已安装后停用不清除此要求。普通 init/heartbeat 核 protected expectation、当前 signed activation、实际 Center/processor 二进制、当前 APP/source binding；已安装时先在 APP 事务外读取真实 W2 anchor，再核 APP canonical checkpoint ancestry。never-installed 必须明确发布 ordinary-only launch，不依赖 birth W2。
- APP owner-only `record_birth_launch_stage` 按 revision CAS 同时登记两份规范 launch bytes，先保持 closed；文件持久发布并读回后才可 `record_birth_launch_open`。exact replay 不产生新 revision；不同 build、generation、binding 或已安装却缺失/关闭的登记要求受控恢复，普通 initializer 不自行替换或重开。`source_application_admit()` 对登记的 runtime/worker 每次核当前 launch digest、open 状态、source/witness、birth 和 activation；写事务持锁至结束，close 等待已准入事务退出。无 launch 的已安装 runtime 拒绝。
- processor 的 claim、renew、complete 等 record job 效果必须在同一事务先调用 `record_processor_worker_admit(deployment, project, instance, capability)`；该 owner-owned SECURITY DEFINER 只授 runtime EXECUTE，先执行 source admission，再核对 OPEN/current manifest 的 processor launch digest，拒绝 center digest 和缺省 launch。它以当前 contract 的 active identity epoch 动态比较 exact worker tuple、capability、queue admission、未过期 lease 与 `fence_contract_version >= minimum_fence_contract_version`，相关 launch/manifest/contract/membership 锁持有至事务结束。不能用 runtime 原始 membership UPDATE 或启动时一次检查替代效果时检查；authority 续租也不证明 processor 存活。
- 既有记录的直接 DML、evidence/comparison 命令，以及 outbox、外部投递插入、导入和 processor 的手工 source 准入入口，也必须调用相同 launch 检查；不能依赖新版 Go 调用前缀。缺失/过期 digest、closed 登记、activation 或 manifest 漂移返回 `42501`。可能已持较晚内容锁的入口，对 launch 表和行都只做 NOWAIT 获取，竞争返回 `40001` 并重启整个事务；已取得的共享锁仍保持到事务结束，使 close 排空既有 writer。
- 独立草稿及其 checkpoint 的直接写入由表 trigger 执行相同检查，覆盖只使用旧 membership gate、从不触及 record root 的旧草稿调用者。该检查不把受限 core/recovery 的精确事务删除 scope 当作普通 writer 准入，也不授权其修改或新增草稿。
- launch digest 由已验证的新进程仅注入自己的 APP 连接，不写入角色/数据库默认值或持久环境，不注入 W2 连接。它是可信程序的启动声明，不是远程证明，也不防御主动伪造当前声明的已攻陷 runtime。旧文件和未修改的旧程序不能借默认连接配置取得当前准入。
- 已安装运行时使用独立 continuity reader；W2 的 `record_birth_continuity(bytea)` 在实际调用内要求当前 reader purpose，并拒绝具有 reserve/prepare/confirm EXECUTE 的主体。producer 缺省或运行时认证/网络不可用而 reader/source 正常时，新请求可用 ordinary，既有 certified pending 不降域；不完整配置、不安全或不可读的 credential file 是明确配置错误。reader 或 source 不可用时暂停新效果，停用后仍保留 reader 要求；source heartbeat 继续 30 秒更新、90 秒租约。
- ordinary cold upgrade 仅适用于 source-absent、birth-never-installed 的已知 predecessor；持久 intent 必须绑定原身份、目标签名状态、真实 executable/image、launch bytes、C81 source 与原网络/restart 状态。外部 stop/join/isolate 和 launch close/drain 后才允许转换；文件发布遇 partial 状态继续保持 CLOSED，仅接受 exact predecessor 或 exact target。恢复必须还原原数据库网络并由受控重建后的真实应用证明读写；已提交的待重启状态不是运行验收。未绑定 BlobRoot 仅允许指定 owner/mode、只含空 `sha256` 目录且数据库无现存本地 blob 引用的 released skeleton；有内容的旧目录在任何变更前拒绝。跨 boot/物理 identity 漂移和复制身份不自动收养。
- cold preflight 在任何变更前核 `.env` 的 `COMPOSE_FILE`：必须恰好选择 `compose.yaml` 与一个已发布 proxy overlay，并 pin 所选文件；其他选择以及 wrong-checksum 或不受支持的 C81 predecessor 均拒绝。未绑定的 released skeleton 必须实际存在且精确匹配；缺失或空 BlobRoot 在 cold upgrade 拒绝，fresh 初始化合同不变。namespace marker 原子发布。
- source-enrolled stack 的 namespace one-shot 只验证已有且匹配的 binding，绝不 provision；这不增加 source adoption 或 V2 source upgrade 支持。
- cold controller 首先取得永久 controller-only lock，再取得 ordinary EX；破坏性效果前先持久化并 fsync immutable intent 和 ACTIVE disposition。普通 SH 管理者在持锁后核 disposition，ACTIVE 或已建立记录缺失/损坏时关闭准入；父子进程死亡和 flock 释放不能自动恢复 CLEAN。child 必须是固定 target image 中实际提取并核对的 static admin，绑定 Center/processor binary 与 embedded C81；以受限 UID/GID、无 Docker socket/凭据启动，完成身份、guard 和持久记录握手后才能执行效果。
- 每个 secret 的 commitment 保存在 root-private record，shared intent 只绑定该 record 的 nonce-bound digest；不能在 shared intent 放可供 UID10001 读取的逐项 secret verifier。
- native helper 与 controller 共享 host mount namespace；受信任 UID10002 能访问的文件仍可被 helper 访问，此方案不提供 Landlock、chroot 或独立 mount namespace，也不声称隔离同 UID 的恶意进程。整个操作信任 root、UID10002 和 root 选定的目标代码；约束来自固定目标和受保护根路径、类型/路径核验、最小 credential pipe、zero capabilities、no-new-privileges、无 Docker socket/凭据、race-free FD 关闭，以及 guard/SQL 准入。公开 cold route 的接线本身不构成端到端验收或 READY。
- DB 隔离后先确认旧 endpoint writer backend 已退出。每个 effect-capable child PostgreSQL backend 在执行前持久登记其真实身份；恢复时须在任何新数据库或文件效果前 join 已记录 child 并确认所有已登记旧 backend 和未解 pre-fence sessions 真正消失。flock、进程死亡、socket close、context cancellation 或仅取得 schema lock 均不证明外部 PostgreSQL 命令已经结算；未知结果保留 ACTIVE。完成后先结算 child/DB、读回目标、恢复网络并持久化待重启状态，受控重建和实际读写证明仍在其后。
- ordinary 管理共享单一受保护 flock domain：共享 guard 覆盖 init、namespace initialization、secret staging 和 authority，cold controller 持独占 guard；目录、anchor、owner/mode/link/ancestor 和 inode/nonce 漂移一律拒绝。G2 仍为 NOT_READY，permanent-delete HTTP 仍 CLOSED；本 ordinary workline 不包含 01–09、新 VM/source 场景或部署授权。


### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| fresh ordinary DB + absent authority state | create verified genesis/successor, converge current ACL, commit activation+rotation+pending pair, persist receipts, open matching launch, atomically heartbeat both before effects |
| active ordinary V1 DB + exact V1 state | normal initializer returns cold-upgrade-required; explicit fenced cold path must preserve genesis and perform the exact transition |
| exact V2 repeat, matching OPEN or CLOSED launch | verify committed receipt/anchor, current head and exact pair; no durable mutation or lease reset, including after unknown COMMIT resolution |
| active DB + absent/corrupt/foreign state, malformed successor or conflicting receipt | fail before application startup; zero replacement state/contract/membership mutation |
| inactive DB + valid state | use verified existing genesis and intended successor; never mint a second identity |
| partial pair, mixed flags, stale head, wrong role or invalid heartbeat wire/time | reject with zero partial membership mutation |
| authority tries table DML/projector or runtime/admin/worker/PUBLIC tries heartbeat | PostgreSQL permission denied; V2 also requires the fixed direct authority session |
| processor lacks exact OPEN processor launch, current worker tuple, queue admission, adequate fence or fresh lease | reject claim/renew/complete; hold valid admission locks through effect transaction |
| authority state changes while process runs | re-verification fails before next heartbeat; health becomes unavailable/stale |
| membership expires | Records effect-time membership admission fails closed; catalog admission alone does not prove a live lease |
| DB-only or state-only restore | unsupported split recovery; restore PostgreSQL and authority state together |
| source-enrolled + missing/foreign guard config, W2 unavailable, generation/RID mismatch, incomplete handoff or enrollment not activated | normal init/staging/heartbeat fail closed; do not infer readiness from W2 active |
| new guard published while old unguarded callback or endpoint may still run | no safe cutover proof; externally stop, isolate, join and prevent restart before offline enrollment |

### 5. Good / Base / Bad Cases

- Good：fresh ordinary init preserves the V1 genesis, publishes the authenticated V2 successor, commits exact activation/rotation and two pending rows, persists both receipts, opens the matching launch and admits both memberships through one V2 heartbeat before Records effects.
- Good：authority restart reverifies the same key/credential, successor, receipts and database head; exact replay preserves renewed expiry and never reruns genesis CAS. An active ordinary V1 instead requires the explicitly fenced cold path.
- Base：brief authority outage leaves existing membership usable only until its DB-bounded expiry; afterward Records transactions fail closed until verified renewal returns.
- Bad：db-init writes membership rows, derives trust only from current DB rows, accepts caller-provided witness digests, rotates to new local state over an active DB, or gives authority/privileged credentials to Center.

### 6. Tests Required

```bash
go test ./internal/center/recordauthority ./internal/center/deploy \
  ./internal/center/store/migrate ./cmd/houfeng-record-platform-admin -count=1

scripts/test-record-platform-integration.sh postgres -- \
  go test ./internal/center/deploy -run '^TestPostgresIntegrationComposeInitialize$|^TestPostgresIntegrationSourceMaintenanceInterlock$' -count=1 -v

HOUFENG_RECORD_PLATFORM_POSTGRES_IMAGE=postgres:16.12 \
  scripts/test-record-platform-integration.sh pg16-catalog -- \
  go test ./internal/center/store/migrate -run '^TestPostgresIntegrationAppACLCurrent/ordinary_membership' -count=1 -v
```

- Unit tests cover authentic captured V1, read-only Preview, deterministic successor/rotation, malformed-present successor refusal, unchanged genesis, receipt verification and closed V1/V2 heartbeat codecs.
- Current-ACL and strict PostgreSQL 16 tests independently check exact function identities, owner/search path/PUBLIC revocation, runtime-only processor EXECUTE and no raw table/projector privilege expansion. They exercise fresh and supported C80→C81, exact pending/active replay with OPEN/CLOSED launch, unexpired V1 center, atomic pair renewal, source/birth/role/stale/mixed rejection, rollback and competing locks. Existing source-enrolled direct signed heartbeat closure regression remains required.
- Processor consumer tests exercise real claim, renewal and completion with current worker identity, higher-than-minimum fence, stale epoch/queue/lease and launch refusal; changing or expiring a lease must not allow a partial effect. PostgreSQL evidence is distinct from actual Compose/Docker acceptance.
- Real isolated Compose acceptance separately proves authentic supported predecessor migration and actual two-build cold stop/replace/network restoration/restart with old content read and new write on an already admitted same-physical namespace. It must exercise crash recovery and permanent-delete HTTP CLOSED; a local fixture, release or successful migration alone is not deployment or G2 readiness evidence.
- 已有 cold acceptance 在 owned nested Docker host 运行，不是实际 host 安装；arm64 只有 cross-build，lost-commit 和 pre-lock backend 只有 component coverage。G2 NOT_READY、permanent-delete HTTP CLOSED，本候选未部署；修复合同本身不代替相应回归证据。

### 7. Wrong vs Correct

```sql
-- Wrong: db-init fabricates authority rows or grants runtime a writer.
insert into public.deployment_membership (...) values (...);
grant update on public.deployment_membership to houfeng_runtime;

-- Correct: the direct authority renews both V2 members atomically.
select public.record_platform_compose_membership_heartbeat_v2($1::bytea);
```

```go
// Wrong: trust caller digests or DB rows alone.
command := recordplatform.ContractActivationProjectionCommandV1{ActivationBundleDigest: callerDigest}

// Correct: LoadComposeState verifies the complete external ledger and returns
// the already-derived command capability used by the existing projector.
state, err := recordauthority.LoadComposeState(root)
command := state.ActivationCommand
```

---
