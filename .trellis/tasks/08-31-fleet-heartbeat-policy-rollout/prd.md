# 修复迁移升级协议并滚动升级 fleet Center 与双 Agent

## Goal

修复已发布 v0.79.5 无法从生产 v0.79.4 exact-current 数据库原地升级的迁移/manifest 阻断，发布新的不可变补丁版本，并在完整冷备、隔离演练和可回滚前提下，将 `fleet.yading.de` Center、`netcup` Agent 与 `informaten` Agent 从 v0.79.4 滚动升级到该修复版本。生产验收只做被动且不制造真实通知噪声的证据读取；强制 19/20、恢复和外发消息测试继续保留在独立非生产任务中。

## Background

- 生产 Center 当前是 v0.79.4，数据库有 63 条 migration，尾部为 `0062_create_vps_create_idempotency.sql`；current APP ACL manifest 只有 revision 1。
- 两台生产 Agent 都是官方 v0.79.4，服务 active/enabled、重启数为 0、队列当前为空，并指向 `https://fleet.yading.de`。
- 生产持久化策略当前为 heartbeat `5s`、`N=20`、sweep `60s`，三类通知开关均开启；只读快照中 active incident 为零，最近十分钟两台实例共有 232 个不同非回填实时批次。升级必须保留 `20` 与 override 指纹，且不得因滚动重启制造事件/通知。
- v0.79.5 已发布并包含 `0063_tune_heartbeat_incident_policy.sql`，但 Compose db-init 对现存 exact-current 数据库在应用 pending migration 之前比较完整 migration set，因此必然返回 `ErrDevelopmentDatabaseRebuildRequired`。
- 严格 PostgreSQL 16 回归已实际证明“previous exact-current + future migration”无写拒绝；仓库未提供 current successor 或受支持的一次性桥接命令。证据见 `research/live-upgrade-blocker-verification.md`。
- 因此不得直接部署 v0.79.5，也不得修改已发布 tag；需要新补丁版本，预期为 v0.79.6。

## Requirements

- R1. 任何产品或生产变更前，保留现有 v0.79.4 服务，确认目标主机/目录/架构/拓扑、迁移账本、manifest、settings 指纹、active incident、通知模式、Agent 身份/版本/队列和磁盘容量；读取不得暴露 token、密码、webhook、完整 Settings 或原始业务日志。
- R2. 新 current APP 升级协议只接受一个显式注册、完整 checksum 匹配的 0062 revision-1 predecessor；release trust roots冻结为 `v0.79.4=1481a558b136c2e6e00e59d523fe281acd655ae8`、`v0.79.5=e427f41b73b3b799f581274ebb1ad11ced56f421` 与开发base `89fcf16af98e3bfcd3927309e1d16f3301195e07`。predecessor golden必须从精确v0.79.4 commit SHA离线生成并先断言symbolic tag仍指向该SHA，包含63-entry name/checksum canonical body、确定性 privilege body与 revision-1 digest；产品matcher和测试fixture不得共同从fixed build动态截取prefix后自证。不得把任意合法前缀、旧 checksum、null head、任意 successor 或 drift 当作可升级状态。
- R3. 0062→0063 必须在现有 advisory lock 下的单个 `SERIALIZABLE` transaction 中完成：先验证 predecessor ledger/manifest/privilege/catalog与0063 schema前提，再应用0063，验证current ledger/catalog、列default、settings转换/保留和index keys/order/include/predicate，全部通过后才追加与revision 1 digest相连的revision 2并CAS推进head。任何失败都零部分持久化；同名错误/预置0063索引不得被 `IF NOT EXISTS` 静默接纳。
- R4. fresh current genesis（revision 1 已包含 0063）、升级后的注册 successor（revision 2）及其重复 convergence/runtime admission 均成功；重复执行不得修改 ledger、manifest、head、catalog、settings 或其他 durable state。
- R5. convergence 与 runtime admission 对同一状态矩阵使用同一权威 transition contract；unknown prefix、缺失/额外 migration、checksum/privilege/catalog/role drift、破损 chain、并发 head 变化和序列化冲突都 fail closed，且保持安全的 typed/redacted error boundary。
- R6. 先以独立v0.79.4 golden建立真实PostgreSQL 16 predecessor，再经真实Compose init路径证明exact state→fixed build成功、0063数据转换/索引正确、runtime admission与代表性Records/attachment行为正常；不得从fixed compiler截取prefix作为唯一oracle，不得用fake、SQL字符串检查或SKIP冒充升级证据。
- R7. 更新被本任务推翻的数据库、部署和质量规范：current contract 从“仅 fresh/exact genesis”扩展为“fresh/exact + 显式注册的 exact successor transition”；不修改任何已发布 migration 或 v0.79.5 artifact。
- R8. 代码通过 TDD、focused/strict/full gates 与至少两轮独立审查；每轮发现必须先修复并复审到 Critical/Important/Minor 均为零，然后按受保护分支完成 commit、PR、required CI、merge、exact-main CI、Release Please 和多架构镜像/Agent/部署资产发布。
- R9. 新发布必须是从 v0.79.5 生成的下一个 `v0.79.x` patch（按当前状态精确预期为 v0.79.6），不得以“任意更高版本”替代。已知 `v0.79.5..89fcf16af98e3bfcd3927309e1d16f3301195e07` 仅允许经独立复核的 `8f8808d4d72de7233f1181cf2f135ebf7818b216`、`1ebae26c54fea96e8e2fed1aa2e47f09ad5e3646`、`c8c1030fa09f111c6a895230393737a51ab5c193` 与 merge `89fcf16af98e3bfcd3927309e1d16f3301195e07` task/journal metadata；随后分别验证 `89fcf16af98e3bfcd3927309e1d16f3301195e07..<feature merge SHA>` 仅含本任务产品/规范/任务变更、Release Please PR只含预期release metadata。合并前必须把PR body、changed files与这三段source range逐项绑定；版本、base、范围或内容出现未allowlist漂移即停止并重新获批。Center image index/amd64 manifest、两个 Agent 架构二进制、checksum/minisig 和 Compose assets 都要从公开发布面重新验证；生产不得使用可漂移 `latest`。
- R10. Center 变更前创建完整冷恢复点，至少包含整个部署目录、PostgreSQL、attachments、Records authority state、`.env`、optional/staged secrets、Compose overlays 和旧镜像；备份位于部署目录外、权限私有、checksum/目录结构通过，并在不启动第二个 authority 的隔离环境完成恢复与升级演练。实际执行必须由SHA-frozen root wrapper以`env -i`+绝对Bash、host-wide `flock`调用经评审的host-specific fail-fast脚本；wrapper从首个rollout写一直持锁到真实exit0 receipt/清理完成，并拒绝并发updater。脚本使用`set -Eeuo pipefail`、错误/信号 recovery trap、停机前 capacity 与 resolved bind-source allowlist、固定 v0.79.4 image ID、`--pull never`；同时冻结本机Docker socket/metadata/daemon ID/platform/空client config，使所有direct Docker与Compose调用指向同一local daemon。任何前置命令失败不得被末尾 health 命令掩盖，只有旧版本/ledger/manifest/authority/heartbeat 全部复核并原子写入 complete marker、且wrapper观察真实exit0后发布绑定marker的execution receipt，下一阶段才可继续。
- R11. 生产 Center 在维护窗口内一次升级；db-init、authority、runtime admission、processor、DB/ClamAV/Center health、公开 health version、OCI revision/digest、0063 ledger/index、settings/override 指纹、代表性 Records/attachment readback 任一不符即停止并按完整冷恢复点回滚，不允许只降镜像。Center成功也必须由同一锁定wrapper在cutover脚本真实exit0后发布独立`CENTER_CUTOVER_EXECUTION_RECEIPT`，Agent阶段逐字段验证该receipt与target marker的精确配对。
- R12. Agent 仅在 Center 完整验收后升级，顺序固定为 `netcup` canary 再 `informaten`；发布/部署前在v0.79.4与fixed SHA分别以`CGO_ENABLED=0 GOOS=linux GOARCH=amd64|arm64 go list -deps -json ./cmd/houfeng-agent`冻结两架构实际选中的全部repo-local production package/Go+Cgo+embed文件closure，并显式加入Center installer-command generator、installer、unit/env template、`go.mod`/`go.sum`、`Makefile`与发布workflow，审阅 `v0.79.4..<fixed SHA>` 的完整差异；同时逐机解析live effective unit/drop-ins/ExecStart/EnvironmentFile/StateDirectory/ReadWritePaths/token/buffer路径及uid/gid/mode。source或live任一语义漂移都必须先用非空 v0.79.4 队列完成隔离升级和降级恢复演练；两者均无变化才保存两SHA×两架构closure清单、diff-empty/source+live证据。每台使用同一sanitized、host-wide锁定wrapper调用经评审的fail-fast host-specific脚本，创建绑定hostname/machine-id/arch/official old SHA/canonical live parent device+owner+mode/effective paths+metadata/token fingerprint/Center stable-ID receipt的私有coherent rollback bundle；state根或后代mount一律拒绝，restore前必须按目标 `st_dev` 汇总该设备上的全部same-filesystem stage字节与已评审headroom后一次性验证容量。随后由不记录secret的systemd transient-cgroup supervisor执行签名安装命令；命令SHA必须与Center签发的root-only issuance data receipt逐字段绑定host/machine/arch/stable-ID/精确v0.79.6/revision/expiry、唯一HTTPS installer URL/public server/release repo、token-stdin/install-missing-deps且无insecure/冲突flag。该data receipt不是脚本成功凭据；supervisor必须在持有同一host lock且紧邻launch之前再次逐字节hash命令与receipt并重跑SHA-frozen parser，消除替换竞态。主机上既存minisign必须是canonical regular executable并绑定byte SHA与provenance；缺失/未知时停止，不授权installer新增持久依赖。installer非零/超时/signal或mixed state都自动进入已验证rollback；root monitor在transient launch前无条件固定85秒total与15秒validation cutoff（取代早期80秒草案）；INT/TERM在`systemd-run`返回码与rc0 acceptance发布完成前只记录pending状态；同一foreground wait内信号合并时采用确定性INT优先（出现INT即130，仅TERM则143），发布后才切换有副作用rollback handler并消费pending，已有signal状态覆盖随后ERR。隔离clone必须证明真实签名下载、安装和fixed-local/first-live成功闭包在15秒内完成。在dispatch acceptance尚未成立时，exact `LoadState=not-found`且预期cgroup路径不存在可证明没有installer unit，因此launch前取消或明确非零dispatch不得disable/stop仍enabled+active的旧Agent；`systemd-run`返回0或已观察到loaded/cgroup后，acceptance必须永久锁存；只有loaded exact transient unit、精确 `ControlGroup=/system.slice/<unit>` 以及regular、readable且为空的 `cgroup.procs` 才能证明installer停止，后续`not-found`、空/漂移ControlGroup、不可读或非空成员均为未证明并要求host isolation，不得并发restore。cgroup kill、旧文件恢复、local-restore receipt、首个fresh heartbeat、verify-local与cleanup必须在85秒内完成，至少早于100秒首次通知边界15秒。验证架构 SHA、版本/revision、unit/env 的语义快照、token 私下逐字节保持、队列 drain 和 Center 通过post-0063 reader先取最近最多768行再去重收到至少3个不同非回填 live batch；严格 PostgreSQL `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` 必须证明 exact 0063 index、无 Seq/Bitmap、scan/loops/blocks 有界。失败只回滚当前 canary且 mixed partial restore 不得启动服务；local old恢复后Center receipt失败保持old enabled+active但不promotion。
  Agent state bundle另以SHA-frozen verifier绑定包含根目录、空目录在内的type/uid/gid/mode canonical metadata，默认只验证而不覆盖较新的live state。只有Agent backup和supervisor这两个实际执行脚本，必须分别由持锁root outer-wrapper在直接观察真实`exit_status=0`后原子发布execution receipt；Center issuance data receipt只证明签发内容与身份，不冒充execution receipt。后一阶段逐path/digest/lock/字段验证完整链，`AGENT_INSTALL_COMPLETE`必须绑定backup/supervisor execution receipts、issuance data receipt、minisign provenance、first-live与fixed-local receipts。
  在任何installer dispatch之前，supervisor必须通过SHA-frozen rollback `--verify-only`路径重新证明当前live old binary/unit/env/token仍逐字节等于冻结bundle、owner/mode/effective unit未漂移且服务仍persisted-enabled+active；该检查完成后必须再次运行SHA-frozen issuance parser重验receipt freshness/expiry与命令语义，再对installer command与issuance receipt做最终byte-hash复核。全部门及pending signal检查通过后才冻结85/15时钟并立即调用`systemd-run`。backup receipt之后、live-old检查期间的command/receipt漂移或receipt在检查期间过期，都必须在dispatch前fail closed并保持旧Agent不变。
- R13. 生产不得人为停止 Agent 来制造 19/20、40/80 或恢复事件，不得重定向/关闭全局通知渠道，也不得把 staging smoke 指向生产。生产只做 migration/settings/agent-version/live-heartbeat/无新增异常通知的被动验收。
- R14. 完成后保留经批准的恢复点至观察窗结束；连续授权只清理精确的rehearsal clone/container/network、下载资产、临时凭据/命令和本任务worktree/branch。任何production failed-state quarantine可能包含数据库、secret、诊断或升级后唯一数据，必须像cold backup/Agent bundle一样保留，只有单独明确删除授权与精确路径/retention证据后才可移除。清理前先证明无需回滚，任何凭据不得进入git、日志或任务资料。
- R15. 实施批准按四个证据绑定阶段解释：A 代码/PR/CI；B 精确 v0.79.6 Release Please PR、merge 与发布；C `hostcram` Center maintenance/cold backup/database transition；D `netcup`→`informaten` Agent installer/canary与普通任务临时物清理。一次明确批准可以连续覆盖四阶段，但每个 go/no-go checkpoint 都必须满足本 PRD 的 exact SHA/version/host/path/live-state/backup 证据；任何漂移或额外产品/停机/数据库范围都必须停止并重新获批。生产恢复点、Agent rollback bundle和任何production failed-state quarantine的删除不包含在该批准中，未获单独删除授权时必须保留并报告。

## Acceptance Criteria

- [ ] AC1. 针对 v0.79.4→v0.79.5 阻断已有可复现 RED、真实 PostgreSQL 证据和根因说明，且生产始终未触碰 v0.79.5。
- [ ] AC2. 独立v0.79.4 release golden与fixed transition predecessor逐字/逐digest一致；该0062 revision-1 state通过显式transition原子升级到含0063的revision-2 successor，且ledger、manifest、head、default/data/index和catalog在revision发布前全部精确。
- [ ] AC3. fresh revision-1 current、revision-2 exact repeat、runtime admission 与序列化重试均通过；unknown predecessor/successor、各类 drift 和 cutpoint 全部零写拒绝。
- [ ] AC4. strict PostgreSQL 16与Compose-upgrade integration使用独立release-pair fixture实际RUN/PASS且无SKIP；升级前后durable snapshot、Records/attachment、0063语义以及同名错误index/partial-effect rollback均有断言。
- [ ] AC5. 所有 focused、`GOTOOLCHAIN=go1.26.2 make verify-go`、Node 22 `make verify-web`、E2E、`git diff --check`、secret/privacy audit 与独立审查全绿、零剩余 finding。
- [ ] AC6. feature PR/CI/merge/exact-main、精确 next-patch v0.79.6 Release Please PR 的 body/files/source-range 审核、release、image/Agent/Compose 发布与公开 digest/signature 证据全部完成；v0.79.5 未被改写，未混入未获批变更。
- [ ] AC7. Center 完整冷备已由经 failure-injection 评审的 sanitized/host-lock/fail-fast 脚本验证并完成隔离 restore+upgrade rehearsal；所有 resolved bind sources 与本地Docker daemon closure已覆盖，生产 rollback 的精确恢复点、complete marker、outer zero-exit receipt、触发条件可用且不含秘密。
- [ ] AC8. 生产 Center 运行 fixed patch，health/OCI/image/0063/manifest/settings/catalog/authority/Records/attachment 全部一致，target marker与独立cutover zero-exit receipt精确配对，观察窗内无异常重启、迁移错误或新增心跳噪声。
- [ ] AC9. `netcup` 与 `informaten` 依次运行 fixed patch Agent；每台 checksum/version/unit/token/queue/三批 live heartbeat 都通过，后一台只在前一台观察通过后升级。
- [ ] AC10. 没有执行生产强制 outage/provider-message 测试；staging 任务未被冒充完成或混入生产变更。
- [ ] AC11. 观察窗结束后按项目规范完成 Trellis 证据、任务归档、远端/本地临时物清理、feature branch/worktree 清理与最终现场核对，生产恢复点按用户约定保留或安全移除。
- [ ] AC12. Agent compatibility diff 与条件式非空队列升降级演练门已完成；两台 Agent 的 fail-fast backup/rollback、语义 unit/env 比较及有界三批查询/执行计划证据通过，且每个生产阶段都在已批准的 exact go/no-go envelope 内。

## Confirmed Decisions

- 不直接部署 v0.79.5，不修改/重发该 tag；实现修复后发布新的 patch，预计 v0.79.6。
- 使用显式、有限的 0062→0063 successor transition，不启用 generic prefix adoption，不跑现场 ad hoc SQL。
- Center 先于 Agents；Agents 采用 arm64 `netcup`→amd64 `informaten` 的单机 canary 顺序。
- 生产只做被动验收；完整 19/20、三次恢复和实际通知正文测试属于独立非生产任务。
- 代码交付、发布、生产部署与最终清理属于同一个端到端任务，但任何阶段的失败都不能跳过其停止/回滚门。
- 获批后的默认连续执行范围固定为 A代码交付、B精确v0.79.6发布、C Center维护升级、D双Agent滚动与临时物清理；证据漂移即停，恢复点、rollback bundle及任何production failed-state quarantine删除始终另行授权。

## Out of Scope

- 不改变 heartbeat policy、Agent DTO、采集频率、通知渠道模型或 Settings UI。
- 不建立通用的任意历史版本迁移框架；本任务只交付首个明确注册的 0062→0063 transition，并留下未来显式扩展合同。
- 不在生产制造心跳异常、发送测试通知、修改通知目标或运行 staging mutation smoke。
- 不删除生产历史 incident/event/notification/heartbeat 数据，不改写 migration/manifest 历史。
