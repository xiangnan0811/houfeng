# Fleet exact-current successor 与生产滚动升级设计

## 1. 结论

v0.79.5 不可用于现有生产数据库。修复采用一个代码内显式注册的 current APP transition：只把 exact v0.79.4/0062 revision-1 状态升级到含 0063 的 revision-2 状态。它不是 generic forward migrator，也不放宽 unknown/drift 的 rebuild-required 边界。

修复合入后由 Release Please 生成新的 patch（按当前版本预期为 v0.79.6）。生产依次执行：完整冷备与隔离演练 → Center 一次升级 → `netcup` Agent canary → `informaten` Agent → 观察 → 清理。任何一步失败都停止后续步骤。

## 2. 已确认失败链

```text
fleet v0.79.4 database
  ledger: 0001..0062 (63 rows)
  manifest: revision 1, source ends at 0062
        |
        v
v0.79.5 houfeng-db-init
  -> InitializeCompose
  -> ConvergeAppACLCurrent
  -> exactCandidate
  -> compare current source 0001..0063 with applied 0001..0062
  -> ErrDevelopmentDatabaseRebuildRequired
  -> transaction rollback / db-init non-zero
  -> Center dependency never becomes satisfied
```

`applyPending` 只在 fresh 分支调用，runtime admission 也只接受 revision 1，因此手工先执行 0063 或手工追加 manifest 都不属于受支持路径，且会制造 split state。

## 3. Transition contract

新增一个包内不可变 transition 描述，命名可按实现统一，但语义固定：

```go
type appACLCurrentTransition struct {
    FromLastMigration string
    ToMigrations      []string
    PrivilegesUnchanged bool
}
```

首个且唯一注册项：

```text
from: exact canonical set ending 0062_create_vps_create_idempotency.sql
to:   0063_tune_heartbeat_incident_policy.sql
privilege body: unchanged
manifest: revision 1 -> revision 2
```

实现不能只比较 tail filename，也不能仅从fixed build的embedded source截掉0063后让产品与测试共同自证。release trust root精确冻结为v0.79.4 commit `1481a558b136c2e6e00e59d523fe281acd655ae8`；生成器先断言symbolic tag仍解析到该SHA，再从该commit离线生成代码评审可读的predecessor fixture：冻结全部63个name/checksum、canonical migration body/digest、固定database/role输入下的canonical privilege body/digest和revision-1 manifest digest。测试运行时不访问Git tag、网络或外部artifact。

fixed compiler先证明其0062 predecessor与release golden逐entry/逐byte一致，再把这个已锚定body用于matcher。真实PostgreSQL/Compose fixture也从同一独立golden验证过的released sources建立，而不是先相信新transition compiler。0063自身必须是current source中的exact checksum，fragment closed-world compiler仍在transaction前运行。

Transition registry 必须拒绝：空/重复 migration、非连续 suffix、unknown name、权限改变却标记 unchanged、多个 transition 命中同一 predecessor、会产生非确定状态或越过当前 source 的定义。所有 slice/body defensive-copy。

## 4. 状态分类

在取得现有 advisory lock、解析 constrained roles/database/catalog contract 后读取 phase/head/ledger/manifests。合法状态只有四种：

| State | Ledger | Manifest | 行为 |
| --- | --- | --- | --- |
| Fresh current | 无 ledger/manifest | 无 | 既有 fresh 路径：apply complete 0063 source + DCL + catalog + revision-1 genesis |
| Exact current genesis | complete through 0063 | one revision-1 manifest bound to complete source | 全量验证后 read-only success |
| Registered predecessor | complete through 0062 | exact revision-1 predecessor manifest | 进入唯一 0062→0063 successor closure |
| Exact registered successor | complete through 0063 | exact two-revision chain，head=2 | 全量验证后 read-only success |

其他状态继续拒绝：

- phase 三表不完整、head null；
- predecessor ledger/manifest name、count、checksum、role、privilege body 或 catalog 任一不精确；
- current ledger 配旧 manifest，或 predecessor ledger 配 advanced head；
- revision gap、错误 previous digest、未知 revision 2+；
- current compiled privilege/catalog 与 latest manifest 不一致；
- source/fragment compiler 本身不闭合。

已知 predecessor mismatch 使用现有 typed rebuild-required；结构损坏/manifest digest/catalog drift 保留具体的 fail-closed corruption/catalog cause。CLI 继续只放行安全 sentinel，不泄露 SQL/DSN/role secret。

## 5. 原子 successor closure

在一个 `SERIALIZABLE` transaction 内：

1. 设置 hardened search path，取得 `appACLSchemaAdvisoryLockV1`。
2. 验证 direct constrained runtime/admin/migrator roles，读取 database name，编译 current catalog 与 privilege body。
3. 读取 phase，并以 `FOR UPDATE` 取得 exact head；读取 complete manifest chain并锁定 migration ledger。即使 advisory lock 外存在不合作写者，最终 old-head CAS 也必须检测变化。
4. 将ledger和revision 1 canonical set与独立release-golden锚定的注册predecessor完整比较；校验revision-1 digest chain、migrator role、current-equivalent privilege body。
5. 在任何写之前执行placement/legacy/catalog readback；因为0063是empty fragment，predecessor与current effective APP catalog必须相同。同时执行transition-specific schema preflight：0063目标index名必须不存在、列default必须是exact released predecessor形状，并抓取singleton settings的完整非秘密逻辑snapshot用于事务内转换/保留比较。任何预置同名index（即使形状看似正确）都视为unsupported partial application并零写拒绝。
6. 调用现有 pending source writer，但只允许注册 suffix 0063；执行 migration SQL 与 ledger insert。
7. 重读并验证complete current ledger；在同一transaction内运行0063 post-apply verifier：列default等于exact current JSON，若pre值全局N=3则仅N变12且`updated_at`推进，其他global值（包括20）、override与其余settings字段逐值保持；index必须在public目标表上valid/ready、btree、non-unique，keys/order为`monitoring_instance_id ASC, received_at DESC, id DESC`，include仅`sync_batch_id`，predicate精确为non-backfilled。该verifier是manifest发布前的产品逻辑，不只存在于测试。
8. 再读catalog，要求与current contract精确一致。0063 verifier或catalog任一失败都使migration SQL、ledger、settings/index在rollback中一起消失。
9. 全部post-apply验证通过后，使用 `NewAppACLManifestPersistedV1(2, role, revision1.digest, currentMigrationBody, currentPrivilegeBody)` 构造successor；插入immutable revision row。
10. `UPDATE app_acl_manifest_head ... WHERE revision=1 AND digest=<old>`，RowsAffected必须为1。
11. 重读完整chain/head/ledger/catalog及0063 effect并逐项验证；提交。

任一 error 或 serialization failure rollback；serialization retry 必须重新执行完整 read/validate/apply/publish closure，不能缓存旧 head、ledger 或 catalog decision。lost ACK 后 repeat 走 exact revision-2 read-only 路径。

## 6. Manifest writer 与 shared verifier

新增窄的 successor insert/CAS helper，复用 genesis 的字段顺序与 SQL约束，但必须：

- revision 精确为 previous + 1；
- previous digest 来自同 transaction 读取并验证的 head；
- insert 后以旧 revision+digest CAS head；
- 不提供任意 caller 指定 revision、任意 body 或跳过 verifier 的 public API。

convergence 与 runtime admission 共享一个 current-manifest shape verifier，接受：

1. one-revision current genesis；或
2. exact registered predecessor revision 1 + exact current revision 2。

latest manifest 必须绑定 complete current source/current privilege body；revision 1 必须绑定注册 predecessor/相同 privilege body；runtime snapshot 的 applied ledger必须等于 latest。这样避免 writer 与 reader 分别复制 transition 规则。

## 7. Compatibility matrix

| Database | Fixed build converge | Fixed runtime admission |
| --- | --- | --- |
| Fresh | 创建含 0063 的 revision 1 | 成功 |
| v0.79.5 fresh revision 1 | read-only success | 成功 |
| v0.79.4 exact 0062 revision 1 | 原子升级到 revision 2 | 升级完成后成功 |
| upgraded 0063 revision 2 | read-only success | 成功 |
| arbitrary older prefix | rebuild-required，零写 | 拒绝 |
| unknown/advanced successor | rebuild-required/chain error，零写 | 拒绝 |
| checksum/ACL/catalog/role drift | 具体 fail-closed error，零写 | 拒绝 |

旧 v0.79.4 binary 不能重新接管升级后的 64-row/revision-2 DB，因此 Center 回滚必须恢复匹配的完整冷备，不能只改镜像 tag。

## 8. TDD and integration evidence

### Unit RED/GREEN

- v0.79.4 exact predecessor 被旧代码 rebuild-required 的现状先保留为 RED target；产品目标改为成功 successor。
- transition compile：exact suffix、缺失/重复/乱序/未知/权限变化负例。
- v0.79.4 release oracle：63-entry name/checksum/canonical body、固定roles/database privilege body和revision1 digest与fixed predecessor逐字一致；故意改变任一pre-0063 byte/fragment/canonicalization必须RED。
- convergence cutpoints：predecessor validate、apply pending、ledger reread、catalog verify、revision insert、head CAS、final readback、commit；每个失败点证明零 durable mutation或 transaction rollback。
- exact revision-2 repeat：所有 writer seam 零调用。
- runtime admission：fresh revision1、registered revision2 成功；malformed/unknown successor 在 catalog read 前拒绝。
- safe error、serialization retry、nil dependency/tx 防御保持。

### Real PostgreSQL 16

扩展 `TestPostgresIntegrationAppACLCurrent` 或新增准确命名的 suite：

1. 用仓库提交的独立v0.79.4 release golden验证并构造真实0062 predecessor；不能直接调用fixed transition compiler生成fixture。source SQL可复用当前不可变文件，但必须逐个checksum先匹配golden，privilege/manifest golden也必须独立匹配。
2. 建立两个完全隔离的真实 predecessor fixture/subtest，每案独立提交before snapshot：A为singleton global threshold `3` + explicit override `3` + heartbeat history，B为singleton custom global `20` + explicit override `3` + heartbeat history；同一数据库绝不同时伪造两个global值。
3. 每案只用生产 `ConvergeAppACLCurrent` 执行升级，不调用测试专用 SQL bridge。
4. A断言0063 checksum、global `3→12`且override `3`不变；B断言global `20`与override `3`均不变。两案各自验证covering partial index、revision 1/2 chain/head、current runtime admission与独立repeat snapshot。
5. 预置同名错误index、错误released default及其他partial-0063 shape，要求在revision2发布前失败；断言ledger、settings、index、manifest/head、catalog完整回滚/不变。
6. 断言repeat前后complete durable snapshot相等。
7. 证明代表性direct-runtime Records write/read与attachment metadata/blob readback，不扩大admin/runtime ACL。
8. 对每个失败shape与post-apply verifier cutpoint证明migration、settings、manifest/head、catalog无变化。

### Compose path

严格 PostgreSQL runner 下先用 old exact fixture运行 `InitializeCompose` 的 upgrade形状，再用 fixed `deploy-init` dependency完整收敛；验证 init jobs 与 runtime admission，不只直接调用 store function。

## 9. Specs and docs

更新：

- `.trellis/spec/backend/database-guidelines.md`：将 current 状态矩阵从 fresh/exact-genesis 扩展为显式 predecessor/successor registry，并把current source inventory由陈旧53-entry/0052更新为64-entry/tail=0063；frozen R1/R2历史计数保持不变，generic prefix/repair仍禁止。
- `.trellis/spec/backend/quality-guidelines.md`：每个新 transition 必须有 frozen predecessor、strict PostgreSQL、Compose caller、cutpoint 和 repeat/read-only 证据。
- `docs/deploy/local-and-systemd.md`：升级文档明确只有发布声明支持的 predecessor 才可原地升级；先冷备/演练，db-init failure 不得 bypass，schema升级后只用完整 cold restore 回滚。
- 相关注释、旧“development only/rebuild”文案只改被本任务推翻的部分，不抹除 frozen R1/R2 历史合同。

## 10. Protected delivery and release

在 `codex/fleet-heartbeat-policy-rollout` 开发，hooks启用，不修改 main。流程：focused RED/GREEN → full gates → 独立审查至零 finding → commit/push → PR → required CI → merge → exact-main CI → Release Please patch PR → release CI → publish-images → 公开 artifacts/digests/signature复核。

目标版本必须是从 `v0.79.5=e427f41b73b3b799f581274ebb1ad11ced56f421` 生成的精确下一个 `v0.79.x` patch，按当前状态即 v0.79.6；“任意高于 v0.79.5”不构成等价目标。已知post-release base固定为`89fcf16af98e3bfcd3927309e1d16f3301195e07`：`v0.79.5..89fcf16af98e3bfcd3927309e1d16f3301195e07`只allowlist经复核的`8f8808d4d72de7233f1181cf2f135ebf7818b216`、`1ebae26c54fea96e8e2fed1aa2e47f09ad5e3646`、`c8c1030fa09f111c6a895230393737a51ab5c193`及merge metadata；`89fcf16af98e3bfcd3927309e1d16f3301195e07..<feature merge SHA>`只允许本任务；Release Please PR只允许预期release metadata。合并前逐项审阅body/changed files/三段source range。版本不是v0.79.6、base/tag解析漂移或任一range含未allowlist内容时停止并重新获批。Agent代码若无变化仍会因版本注入产生新签名 binaries；生产 Agents 与 Center 使用同一 fixed release。

## 11. Production Center runbook

### Preflight

- 再次确认 `hostcram`、绝对目录 `/root/data/docker_data/houfeng`、Compose overlays、v0.79.4 image ID/revision、63/0062、manifest revision1、health、磁盘、mounts、local attachments、authority state。
- 只保存 settings所需字段/override hash、active heartbeat incident count、notification mode/presence、两台 Agent ID/version/latest live batches；不输出 secret或完整对象。
- 拉取并按 immutable digest inspect fixed image，但不改变 running stack。

### Recovery point and rehearsal

- maintenance window内只运行经评审的host-specific fail-fast脚本：SHA-frozen root wrapper以`env -i`和绝对Bash入口、在任何rollout写前取得host-wide `flock`，拒绝并发updater并持锁到child exit/receipt/cleanup；脚本使用`set -Eeuo pipefail`且ERR recovery只在root shell执行一次，恢复中忽略后续INT/TERM并保留首次原码。冻结本机Docker unix socket/metadata/daemon ID/platform/空client config，所有direct Docker与Compose调用都走同一daemon closure；Compose另使用`env -i`、显式archived `.env`和精确common+proxy-host文件。停机前做capacity、精确project container set/no-orphan、source存在/类型/containment/tar-listing与三旧image archive manifest/ref→config digest检查。停stack前arm monotonic total 85秒recovery trap，但成功路径在55秒validation cutoff硬停，为old image load/Compose readiness保留30秒；critical为30秒+3秒kill grace，所有timeout都预扣kill grace。旧0062的两条stable-ID heartbeat查询先用现有`(monitoring_instance_id, observed_at desc)`索引取最多768行再过滤post-watermark live row；0063 exact partial-index plan只用于fixed/Agent验收。任何失败以`--pull never`尝试恢复精确v0.79.4且保留原码。checksum、tar/Compose source hash、旧version/0062/revision1/authority/Agents heartbeat全部通过并先持久化payload再原子写phase complete marker；wrapper真实观察脚本exit0后再原子发布绑定marker/lock的execution receipt，二者不能单独放行。
- 从该 cold point恢复到唯一、隔离的绝对目录，以新的 `-p <unique-project>` 和 common `compose.yaml` 启动；不得沿用生产 `COMPOSE_PROJECT_NAME`，不得加载 proxy host/network overlay。渲染后的 rehearsal config 必须证明没有 published port、external proxy network或生产 bind mount。
- 只运行 DB、必要 init jobs与私有 direct-runtime probes；不得启动长驻 Center或第二个 Records authority，不得连接生产 Agents/公网入口。完成 exact 0062→0063 rehearsal，验证后按已解析的project/container/network/目录精确销毁 clone。

### Cutover

- 记录最终 pre-change receipt；只把核验后的 fixed Compose assets下载到生产部署树外的root-only staging目录并固定image digest，不停写、不替换live文件。
- 运行独立host-specific fail-fast cutover脚本：先重算并验证cold marker绑定的archive/source-list/old images/private receipt与树外staged assets，冻结唯一nonexisting failed-state quarantine；在首个生产树asset替换、`.env` image pin、停写、service stop或DB mutation前arm完整cold-restore trap。所有live asset写入都只能发生在该armed closure内。
- `docker compose config --quiet`，pull/inspect后 `up -d`；严格观察 init job exit。任一失败trap停止target、隔离完整failed tree、校验并恢复cold archive+三旧images，以exact v0.79.4 `--pull never`启动并复核0062/revision1/authority/Records/attachments/两台post-restore heartbeat与quiet notification window；restore任一步失败先做有界stop/kill并精确证明target inactive且无partial authority，只有证明成立才称fail-stopped，否则标记unproven并要求service/host isolation，绝不从错误cwd继续。
- target成功后验证health/image/OCI/migration/manifest/settings/index/catalog/authority/Records/attachments/restart count与紧窗口无新增heartbeat incident/event/notification；全部完成才解除full-restore trap并持久化绑定release/cold/private receipt的cutover complete marker。持锁wrapper真实观察cutover脚本exit0后另行原子发布`CENTER_CUTOVER_EXECUTION_RECEIPT`，绑定lock、cutover script path+SHA、target marker path+SHA、cold receipt及exit0；Agent门同时重验marker+receipt。每个mutation/restore cutpoint与signal/delayed-readiness均先在clone failure-inject。

## 12. Agent canary runbook

每台 Agent：

1. 复核 host-specific timer/cron/package owner，防止并发 updater；在v0.79.4与fixed SHA分别对`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`及`GOARCH=arm64`运行`go list -deps -json ./cmd/houfeng-agent`，冻结每个架构实际选中的repo-local package、目录与Go/Cgo/embed文件并比较两架构并集，再把Center installer-command generator、installer、unit/env template、`go.mod`/`go.sum`、`Makefile`和`.github/workflows/publish-images.yml`显式加入审阅范围；审阅两版本完整diff及queue serialization/retry/discard、token argv/stdin/privacy语义。同时解析每台live effective unit/drop-ins/ExecStart/EnvironmentFile/StateDirectory/ReadWritePaths/token/buffer路径和uid/gid/mode。source或live任一漂移都先在隔离环境用非空v0.79.4队列完成fixed升级和v0.79.4降级恢复；都无变化才保存两SHA×两架构closure清单、精确diff-empty/source+live证据。
2. 使用同一sanitized、host-lock outer wrapper调用经评审的host-specific fail-fast脚本短暂停服务，以 root-only路径备份 binary、unit、env、token和state；bundle marker绑定hostname/machine-id/arch/official old SHA、canonical live parent path/device/owner/mode、resolved paths、core uid/gid/mode、token fingerprint、checksum manifest、SHA-frozen state-tree metadata verifier/receipt和Center stable-ID receipt。state root/descendant mount拒绝；rollback在任何stage写入前按目标 `st_dev` 汇总该设备全部stage字节和headroom，一次性验证same-filesystem容量。45秒backup关键区watchdog失败即恢复旧服务；只有bundle checksum、旧版本fresh heartbeat、complete marker与持锁outer-wrapper真实zero-exit receipt组成的精确配对完成后才继续。
   在第3步实际dispatch前，supervisor必须通过同一SHA-frozen `--verify-only`路径再次证明当前live old binary/unit/env/token逐字节等于该bundle、owner/mode/effective unit仍一致且服务仍persisted-enabled+active；随后再次运行SHA-frozen issuance parser重验receipt freshness/expiry与命令语义，再对installer command与issuance receipt做最终byte-hash复核。全部门通过并重查pending signal后才冻结85/15时钟并立即调用`systemd-run`。backup receipt之后、live-old检查期间的command/receipt漂移或receipt过期都在dispatch前停止且不触碰旧Agent。
3. 从 fixed Center生成新的单次签名安装命令；整条命令作为 secret，仅写入host-local root `0600`文件，且其SHA与root-only Center issuance data receipt的host/machine/arch/stable-ID/v0.79.6/revision/expiry、唯一HTTPS installer URL/public server/release repo、token-stdin/install-missing-deps且无insecure/冲突flag逐字段绑定。该data receipt证明签发身份与内容，不声称脚本执行成功；持锁supervisor紧邻launch前必须再次hash命令和receipt并重跑SHA-frozen parser。预先冻结已存在minisign的canonical regular path、SHA与provenance；缺失/未知时停止，不允许installer新增持久依赖。已评审supervisor用sanitized `env -i`在唯一systemd transient unit内执行，冻结cgroup-v2、`Type=exec`、`ExitType=cgroup`、`RemainAfterExit=yes`与`KillMode=control-group`。transient launch前无条件固定85秒total及15秒validation cutoff（取代早期80秒草案），clone必须证明实际签名下载、安装、fixed-local与first-live闭包在15秒内完成；timeout/非零/signal/mixed state停止并kill整个unit。dispatch acceptance尚未成立时，exact `LoadState=not-found`且预期cgroup路径不存在才可证明未形成installer unit，launch前取消或明确非零dispatch必须保留旧Agent enabled+active且不得调用其disable/stop；`systemd-run`返回0或已观察到loaded/cgroup后，acceptance必须永久锁存；只有loaded exact unit、精确 `ControlGroup=/system.slice/<unit>` 和regular/readable/empty `cgroup.procs`才允许自动rollback，后续`not-found`、空/漂移ControlGroup或无法读取/清空成员都要求host isolation且不并发restore。cgroup终止、old restore、local receipt、first-live Center receipt、verify-local与cleanup共享85秒，至少早于100秒通知边界15秒。supervisor真实exit0后由持锁outer wrapper发布execution receipt，最终promotion marker绑定backup/supervisor execution receipts、issuance data receipt、minisign provenance、first-live/fixed-local/三批/quiet receipts。
4. 核验 public signed checksum、buildinfo version/revision、systemd enabled/active、NRestarts、endpoint hostname；对安装前后unit/env做去秘密的语义比较，token只做private byte equality，queue drain。
5. 从 Center以 stable MonitoringInstance ID执行与生产recovery reader相同的`recent_live AS MATERIALIZED`形状：先按`received_at DESC,id DESC`从non-backfill行取最多768个candidate，再在candidate内按`sync_batch_id`去重并取最新3个；不能先扫描/聚合实例全历史，也不能在candidate阶段按目标版本过滤。上线前以大历史strict PostgreSQL `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`证明使用0063 exact index、无Seq/Bitmap、scan rows/loops/shared blocks有界。
6. 观察无401/403/429/5xx持续重试、无队列增长、无新heartbeat incident/notification后再升级下一台。

`netcup` arm64先，`informaten` amd64后。失败只回滚当前host，保留失败state作诊断；队列默认保留较新的live state，只有格式损坏且评审确认时才恢复旧snapshot。

## 13. Rollback and cleanup

- 代码/CI/发布前：只回退feature branch上的本任务修改，不触碰main。
- Center rehearsal失败：丢弃隔离clone，生产仍为v0.79.4。
- Center cutover失败或验收不完整：停止target stack，隔离failed目录，恢复完整cold point与旧image，验证v0.79.4/0062/revision1/authority/Agents heartbeat。
- Agent失败：使用已评审rollback脚本先把全部old binary/unit/env/token内容和mode/owner写入各目标同文件系统的staging path并fsync校验，再持久化disable、stop并证明inactive后逐个原子rename；任何partial restore失败都做有界disable/stop/kill，不能证明fail-stopped时明确升级为主机隔离告警，绝不启动mixed状态。完整旧文件fsync、daemon-reload、enable/start及本地语义/byte/checksum验证通过后即解除“失败需停服”状态；后续Center receipt失败只不写marker/不promotion，不再停止健康old Agent。另一host不动。
- 观察通过后，连续授权只清理精确rehearsal containers/networks、download与secret command临时文件；cold point、Agent bundles及任何production failed-state quarantine都保留，删除需另行明确授权并绑定精确路径。最后归档Trellis task并移除feature worktree/branch，主checkout保持受保护、干净并与origin/main状态核对。

## 14. Evidence-bound authorization

规划完成后的实施批准可一次连续覆盖四个明确阶段：A feature代码/PR/CI，B精确v0.79.6 Release Please merge/publish，C `hostcram` Center maintenance/cold backup/DB transition，D `netcup`→`informaten` installer/canary与普通任务临时物清理。每个阶段开始前都要形成sanitized go/no-go receipt，并证明SHA、版本、PR文件范围、host、绝对路径、live state、capacity及上一阶段complete marker仍与本设计一致。任何偏差、无关release diff、额外停机/数据库/installer范围都终止连续授权并请求新批准。冷备、Agent rollback bundle与production failed-state quarantine的删除从不隐含在连续授权中。
