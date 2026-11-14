# Compose authority 与协调恢复

## Scenario: single-host Compose Records authority and membership heartbeat

### 1. Scope / Trigger

- Trigger：修改 `internal/center/recordauthority/`、Compose deploy init/authority runtime、`0060_create_records_authority_heartbeat.sql`、current APP ACL authority fragment、activation projector wiring、deployment-ID file loading、或 `deployment_contract_state` / `deployment_membership` 的 Compose recovery contract。
- 此场景只批准 single-host Compose Records authority：durable signed state 位于 PostgreSQL 外，activation 必须复用现有 migrator-owned projector，membership renewal 必须通过一个窄 heartbeat function。不得让 db-init fabricate contract/membership rows，也不得给 Center allow gate。

### 2. Signatures

```go
func recordauthority.CreateComposeState(root string) (recordauthority.VerifiedComposeState, error)
func recordauthority.LoadComposeState(root string) (recordauthority.VerifiedComposeState, error)
func recordauthority.PersistActivationReceipt(root string, state recordauthority.VerifiedComposeState, receipt []byte) error
func recordauthority.VerifyActivationReceipt(root string, state recordauthority.VerifiedComposeState) error
func recordauthority.MarshalMembershipHeartbeatCommandV1(state recordauthority.VerifiedComposeState, issuedAt time.Time) ([]byte, time.Time, error)

func deploy.InitializeCompose(ctx context.Context, config deploy.ComposeInitConfig) error
func deploy.RunComposeAuthority(ctx context.Context) error
```

- Verified state derives the existing `recordplatform.ContractActivationProjectionCommandV1`; trusted callers do not supply activation/ledger/identity digests.
- Database boundary is exact `public.record_platform_cas_contract_activation_projection(bytea)` for activation and `public.record_platform_compose_membership_heartbeat(bytea)` for renewal.

### 3. Contracts

- State root is atomic and closed: stable deployment ID, Ed25519 private key, generated constrained database password, canonical bounded signed hash-chained activation ledger, and canonical CAS receipt. The signed ledger binds a domain-separated digest of the exact database credential so a different valid-looking 64-hex secret is corruption, not rotation. `LoadComposeState` rechecks file type/mode/size, canonical JSON, complete ledger sequence, every derived digest, signature, fixed Compose inventory, credential shape, and signed credential digest before returning a capability-bearing `VerifiedComposeState`.
- 对未启用 source authority 的部署，signed authority recovery matrix 为：absent state + inactive/fresh DB 可调用 `CreateComposeState`；valid state + inactive DB 激活；valid state + exact active DB 验证；absent/corrupt/mismatched state + active DB fail closed。绝不能在 active contract 上重新生成。Cold backup/host migration 必须协调恢复 PostgreSQL、attachments 与 authority state；source-enrolled 部署还受以下外部 W2 和 maintenance 条件约束。
- Activation marshals only `VerifiedComposeState.ActivationCommand`, calls the existing migrator-owned CAS projector, verifies/persists its returned receipt, and publishes only a derived deployment ID for Center. Db-init must not insert/update `deployment_contract_state` or `deployment_membership` directly.
- Compose provisions `houfeng_records_authority` as a distinct direct login with constrained attributes and no membership edges. The current APP ACL grants database connect, `public` schema usage, heartbeat execution, and the narrow `source_maintenance_status()` / `record_birth_launch_inspect(bigint, bytea)` projections. It grants no table/sequence DML, launch mutation, or activation/domain-rotation projector execution. `PUBLIC`, runtime, platform-admin, deletion-worker, and migrator cannot execute the heartbeat function.
- Migration `0060` owns a single `SECURITY DEFINER`, `search_path=pg_catalog`, one-`bytea` heartbeat overload with PUBLIC revoked. The closed 177-byte command fixes project `default`, instance `compose-center`, kind `api`, capability `records.runtime`, load-balancer admitted true, queue admitted false, and binds the active deployment ID/epoch/fence.
- Heartbeat accepts issued time only within the bounded clock window and exact 90-second expiry; expiry must be future and at most 120 seconds from DB transaction time. It locks/checks the exact active contract and existing exact membership before inserting or updating only `heartbeat_expires_at`/`updated_at`. Foreign/stale/future/mismatched commands produce zero mutation.
- Authority reloads/reverifies the same state before every renewal, uses only its state-derived DB credential, refreshes every fixed 30 seconds, and is healthy only after a verified successful membership receipt that remains fresh. Shutdown never deletes membership; expiry restores fail-closed admission.
- Center 与 processor 只读各自的公开 launch binding；文件不含 producer、reader 或管理秘密。Center 默认路径为 `/run/houfeng/records-authority/birth-launch.json`，processor 为同目录 `birth-launch-processor.json`，可由 `HOUFENG_RECORD_BIRTH_LAUNCH_FILE` 指定受保护绝对路径。文件必须为 UID10002:GID10001、0440、单链接普通文件；祖先目录不得由运行用户写入或经符号链接绕过。启动器检查规范 JSON、process kind、配置 deployment ID 和实际 `/proc/self/exe` 摘要，缺文件不是 never-installed 证据。
- C81 uses `AppRoleSetV2` with a mandatory, distinct `houfeng_record_deletion_worker` direct login and its own operator-managed password. The bootstrap, runtime, platform-admin, deletion-worker and migrator passwords must be pairwise distinct; the authority credential remains independently derived from signed state. Secret staging exposes the worker credential only to db-init, not Center/processor/authority. Current convergence takes all three application-role bindings and publishes privilege body V2; old V1 manifests retain their original two bindings. A matching schema/ACL does not activate a deletion worker or enable permanent-deletion HTTP.
- Source-enrolled Compose 的独立 W2 和受控 maintenance 配置/持久 flock anchor 位于 APP、attachments、signed authority 的恢复单元之外，不能随其回滚。普通 init/migrate/bootstrap/finalize/activation、secret staging 和每次 authority heartbeat 均使用同一受控目录、同一 Linux kernel 与共享 flock domain 的 shared guard；恢复维护持 exclusive guard。锁文件不得 unlink 或替换，身份不匹配 fail closed；跨 host 的 flock 不互斥，不能用它证明旧 endpoint 隔离。已 enrollment 不得通过省略配置关闭检查；正常参与者须核对 APP/W2 domain、active generation、recovery ID、W2 completed handoff 与 activated enrollment，W2 active 本身不够。
- 当前 generation 的角色 credential commitments 必须验证，旧 `.env`/staged secrets 不得作为修复手段回写旧密码。未启用 source authority 的部署保留普通初始化、八服务 `up` 和现有 rotation 合同；enrolled 轮换/恢复不能绕过当前 commitment 与受控交接。
- W2 同时保存 purpose/principal 绑定的 credential commitment 与独立的 secret fingerprint。后者只对已核验的高熵明文密码做 domain-separated、长度前缀 hash，不含角色名、用途或 generation；恢复 staging 在同一 authority 锁内拒绝任何历史 generation 使用过的 fingerprint，换角色名或用途不能复用旧秘密。exact 重放必须同时匹配 principal、commitment、fingerprint；数据库不保存明文，fingerprint 不替代真实凭据验证或完整交接证明。
- 恢复中的 fresh W2 producer 可先以受约束 NOLOGIN 角色绑定，仅限当前 generation、mode=recovering 且 exact RID 尚未完成；reader/recovery 与普通 active 模式不借此放宽 LOGIN 要求。绑定和 credential staging 不开放 LOGIN，producer 的实际效果仍要求当前角色、active generation 与 completed handoff。
- 首次启用是外部 fencing 的 offline 操作：新发布的 guard 无法排空已经在无 guard 下运行的 callback。操作者必须实际停止、隔离并等待旧 writer/管理操作及 endpoint 退出，同时阻止其重启，再进行 enrollment；仅创建 guard、等待 heartbeat 过期或调用 `enroll-offline` 都不是安全 live cutover 的证明。相关 helper/围栏接线不等于已提供完整可运行 recovery coordinator/CLI，也不证明实际部署或完整恢复已完成；不能恢复服务直到上述准入及 source reconciliation 可证明。
- 受保护 source maintenance 配置持久保存 `birth_installed`、稳定 lineage、协议版本和独立 birth reader 连接。已安装后停用不清除此要求。普通 init/heartbeat 核 protected expectation、当前 signed activation、实际 Center/processor 二进制、当前 APP/source binding；已安装时先在 APP 事务外读取真实 W2 anchor，再核 APP canonical checkpoint ancestry。never-installed 必须明确发布 ordinary-only launch，不依赖 birth W2。
- APP owner-only `record_birth_launch_stage` 按 revision CAS 同时登记两份规范 launch bytes，先保持 closed；文件持久发布并读回后才可 `record_birth_launch_open`。exact replay 不产生新 revision；不同 build、generation、binding 或已安装却缺失/关闭的登记要求受控恢复，普通 initializer 不自行替换或重开。`source_application_admit()` 对登记的 runtime/worker 每次核当前 launch digest、open 状态、source/witness、birth 和 activation；写事务持锁至结束，close 等待已准入事务退出。无 launch 的已安装 runtime 拒绝。
- 既有记录的直接 DML、evidence/comparison 命令，以及 outbox、外部投递插入、导入和 processor 的手工 source 准入入口，也必须调用相同 launch 检查；不能依赖新版 Go 调用前缀。缺失/过期 digest、closed 登记、activation 或 manifest 漂移返回 `42501`。可能已持较晚内容锁的入口，对 launch 表和行都只做 NOWAIT 获取，竞争返回 `40001` 并重启整个事务；已取得的共享锁仍保持到事务结束，使 close 排空既有 writer。
- 独立草稿及其 checkpoint 的直接写入由表 trigger 执行相同检查，覆盖只使用旧 membership gate、从不触及 record root 的旧草稿调用者。该检查不把受限 core/recovery 的精确事务删除 scope 当作普通 writer 准入，也不授权其修改或新增草稿。
- launch digest 由已验证的新进程仅注入自己的 APP 连接，不写入角色/数据库默认值或持久环境，不注入 W2 连接。它是可信程序的启动声明，不是远程证明，也不防御主动伪造当前声明的已攻陷 runtime。旧文件和未修改的旧程序不能借默认连接配置取得当前准入。
- 已安装运行时使用独立 continuity reader；W2 的 `record_birth_continuity(bytea)` 在实际调用内要求当前 reader purpose，并拒绝具有 reserve/prepare/confirm EXECUTE 的主体。producer 缺省或运行时认证/网络不可用而 reader/source 正常时，新请求可用 ordinary，既有 certified pending 不降域；不完整配置、不安全或不可读的 credential file 是明确配置错误。reader 或 source 不可用时暂停新效果，停用后仍保留 reader 要求；source heartbeat 继续 30 秒更新、90 秒租约。


### 4. Validation & Error Matrix

| Condition | Expected behavior |
| --- | --- |
| fresh DB + absent authority state | atomically create one state, provision role, converge current ACL, activate via projector, persist receipt, heartbeat, admit runtime |
| exact repeat | reuse byte-identical state/identity and active epoch/fence; only bounded membership expiry and approved operator-role passwords may change, subject to source-enrolled credential commitments |
| active DB + absent/corrupt/foreign state | fail before application startup; zero replacement state/contract/membership mutation |
| inactive DB + valid state | activate from the verified existing bundle; never mint a second identity |
| heartbeat deployment/epoch/fence/identity/time mismatch | function rejects with zero membership mutation |
| authority tries table DML/projector or runtime/admin/PUBLIC tries heartbeat | PostgreSQL permission denied |
| authority state changes while process runs | re-verification fails before next heartbeat; health becomes unavailable/stale |
| membership expires | `AdmitAppACLCurrentRuntime`/Records write path fails closed; no local bypass |
| DB-only or state-only restore | unsupported split recovery; restore PostgreSQL and authority state together |
| source-enrolled + missing/foreign guard config, W2 unavailable, generation/RID mismatch, incomplete handoff or enrollment not activated | normal init/staging/heartbeat fail closed; do not infer readiness from W2 active |
| new guard published while old unguarded callback or endpoint may still run | no safe cutover proof; externally stop, isolate, join and prevent restart before offline enrollment |

### 5. Good / Base / Bad Cases

- Good：fresh Compose init creates signed state outside DB, derives `ContractActivationProjectionCommandV1`, calls the existing projector, persists/verifies receipt, then the constrained role establishes exact membership and Center publishes a real admitted Record.
- Good：authority restart loads the same key/credential/deployment ID, renews the same membership, and becomes healthy without changing contract epoch/fence.
- Base：brief authority outage leaves existing membership usable only until its DB-bounded expiry; afterward Records transactions fail closed until verified renewal returns.
- Bad：db-init writes membership rows, derives trust only from current DB rows, accepts caller-provided witness digests, rotates to new local state over an active DB, or gives authority/privileged credentials to Center.

### 6. Tests Required

```bash
go test ./internal/center/recordauthority ./internal/center/deploy \
  ./internal/center/store/migrate ./cmd/houfeng-record-platform-admin -count=1

scripts/test-record-platform-integration.sh postgres -- \
  go test ./internal/center/deploy -run '^TestPostgresIntegrationComposeInitialize$' -count=1 -v
```

- Unit tests cover atomic create, exact repeat, canonical ledger/signature/digest derivation, hostile/truncated/oversized/mode-drift state, receipt CAS, closed heartbeat codec, Center deployment-ID file, safe stage errors, refresh/health/shutdown, and complete recovery matrix.
- Migration/current-ACL tests freeze the one function/role/object set and prove no privilege expansion. Strict PostgreSQL 16 evidence must use unique test role/database names, never pre-drop fixed production names, and prove authority-only heartbeat plus direct-DML/projector/runtime/admin/PUBLIC denials.
- PostgreSQL tests cover exact active contract/membership, TTL, restart renewal, stable epoch/fence/key across operator password rotations, and byte-for-byte zero mutation after stale/foreign/truncated/future commands.
- Real isolated Compose smoke proves fresh automatic init with no SQL helper, authority health, actual admitted Records write, attachment/ClamAV flow, restart/exact repeat, corrupt-state fail-closed, and coordinated portable copy. Task-owned containers/networks/data must be unique and cleaned without touching unrelated resources.

### 7. Wrong vs Correct

```sql
-- Wrong: db-init fabricates authority rows or grants runtime a writer.
insert into public.deployment_membership (...) values (...);
grant update on public.deployment_membership to houfeng_runtime;

-- Correct: only the constrained authority calls the closed security-definer codec.
select public.record_platform_compose_membership_heartbeat($1::bytea);
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
