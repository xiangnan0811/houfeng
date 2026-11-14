# G2 执行路线

本页是 G2 的当前续接路线，行为以 [记录删除与恢复合同](../spec/contracts/records/deletion-recovery.md)、[记录集成与恢复合同](../spec/contracts/records/integration-recovery.md)、[授权合同](../spec/contracts/records/authorization.md)及其引用的现行规范为准。本页安排工作与验收，不替代合同。

当前 **G2 NOT_READY，永久删除 HTTP CLOSED**。唯一活动候选为 `feat/g2-durable-deletion-integrated` 的完整工作区。运行源码已迁入 `scripts/g2/source/`、`scripts/g2/phase0/` 和 `scripts/g2/http_smoke/`；源码迁移不代表产品验收完成。`phase0` 是固定输入，历史运行数据仍在各自原位置。历史材料如 `tmp/full-audit-2026-10-19/11-remediation-roadmap.md`、`tmp/agent/remediation-engineering-27180de1/target-spec.md`、`tmp/agent/remediation-engineering-27180de1/tickets/` 和 `tmp/agent/settlement-followup-4wg7iie_/remaining-g2-gates.md` 是证据指针，不是干净 checkout 的运行依赖。

## 状态口径与依赖

每个工作项在实际推进时分别记录五种状态：**实现**（存在的范围和缺口）、**本候选验收**（指纹、实际 RUN/PASS/FAIL/NOT_RUN、skip 和失效范围）、**独立审查**（候选、模型和已关闭发现）、**Git 交付**（提交、PR 与 required CI），以及 **环境证据**（运行条件、实际 provider 和恢复目标）。未取得同候选证据时写待验，不将历史局部 PASS、旧审查或阶段提交外推为整项完成。新修复只使实际受影响的证据失效；最终验收仍须绑定同一完整指纹。

|工作包|当前范围和下一动作|关闭条件与依赖|
|---|---|---|
|N：source/native|历史 selection144/build148 的 exact 失败，当前 runner 身份已漂移。先阅读已有 receipt、serial、type7 与控制诊断，定位 postgres-uid removal-settlement 缺口；截断或 unknown scan limit 不能推断原因。下一候选先新 browser prepare，再 source build，再最多一次 exact，失败也 verify。|build 未通过不运行 exact；exact 和独立 verify 均真实通过后才运行 observer-capability、bpf-authorization 和剩余 source。原锁下串行，最终 51 PASS、0 FAIL、0 NOT_RUN，含 33 baseline、13 fault、18 terminal/reader 输入及四 owner、四 launcher 反例。历史 selection 可读不等于当前 build gate 通过。|
|P：独立产品和 M1|ordinary/prepared V2、三版本 catalog、local/S3-race、issuer/九 adapter、client/comparison 浏览器可与 N、K 调查并行。|历史 PASS 与当前候选分列；source-bound issuer 和全 writer 接受仍等真实 source 准入。M1 为 G2-01/05/06 联合验收，再依次 G2-02→03→04；最终同指纹重验。|
|K：正向物理恢复|独立调查设计输入：可信 stopped-writer/restore-unit producer、同 generation APP/W2、旧凭据及 namespace 撤销、W2 inventory/prepare、真实 filesystem identity/handoff、root-independent 义务、seal→activation producer/证据与失败闭合。|现无完整 source recovery coordinator/CLI。具体协议、真实 CLI envelope 和正向 driver 必须先形成决策完整的后续原生 Plan 并获批准。通用 restore 或 local/S3 `--all` 不能关闭此项；见 [集成恢复合同](../spec/contracts/records/integration-recovery.md) 和 [Compose authority](../spec/contracts/platform/compose-authority.md)。|
|J：联合接受|N、P、K 闭合后冻结完整源码、产品、工具链、配置和输入，冻结后不并发写产品。|同 APP/RID、多 generation、C81 fresh/dual carriers/systemd、三 normal/recovery、source handoff/V1 reconcile、birth/namespace/跨 RID、root-absent/late I/O/quota-once 和 D1–D7 均须真实闭合；OLD 未解差异和未知远端 provider 义务各有独立关闭条件。|
|D：阶段交付|完整候选经过适用门禁与独立审查后，可提交、推送并建立或更新 draft PR，持续 required CI。|区分源码迁移与未完成的产品验收；完整 index 一起交付，不隐藏候选。阶段提交不等于 G2 READY；不合并、发布、部署或开放永久删除 HTTP。|

source、产品和恢复的独立调查可以并行；M1 的合法 source 签发、全 writer 接受和最终联合验收不能跳过实际前置。相关历史局部实现和独立审查可在输入未变且覆盖相同时复用，复用理由须绑定具体文件和环境。

## G2 票据与联合前置

下表的实现栏只描述已知切片；所有整票的本候选验收、整合独立审查和 Git 交付均待对应证据确认，环境证据不得由历史隔离结果替代。每项关闭还须满足本页状态口径。

|项目|实现与关闭条件|现行合同|
|---|---|---|
|M1|G2-01、05、06 联合前置：C81/schema/角色、issuer 和资源协调基础，加两个真实 adapter 和浏览器；同一受限授权路径实际签发九 adapter，完整 registry/receipt，不用 subset 或 prepare-gate 旁路。source 准入未证时真实 source-bound 签发待验。三票共同通过才关闭 M1。|[APP ACL](../spec/contracts/platform/app-acl-current.md)、[删除](../spec/contracts/records/deletion-recovery.md)|
|G2-01|已有授权、C81、角色和 V2 局部切片；完整签发待验。真实 resource/source 权限及二次确认绑定 project、record、lifecycle/version、auth epoch、fence；非法 issuer/worker/旧 operation 拒绝，gate 脱离 root 持久且租约到期只接管。验证 restore/edit 两锁序、fresh/注册 predecessor successor 的真实 admission、漂移拒绝、失败回滚、exact replay、重启重读及冻结资产保护。|[授权](../spec/contracts/records/authorization.md)、[APP ACL](../spec/contracts/platform/app-acl-current.md)、[删除](../spec/contracts/records/deletion-recovery.md)|
|G2-02|已有 typed actor 与 participant 资源声明等局部实现，声明不等于取得锁。逐一覆盖 revision、draft、upload/promotion、processor、copy、evidence、search/activity、collaboration/outbox、import/export/PDF 真实 writer；gate 前提交纳入清理，gate 后不发布或复活，仅允许原 obligation 记录迟到事实。验证统一 rank、反序 batch、expiry、source/target 与 import/GC 两顺序、重读变化 rollback/reselect，保留其他记录与 origin tombstone。整票依赖 M1/source。|[删除](../spec/contracts/records/deletion-recovery.md)、[投递原语](../spec/contracts/platform/delivery-primitives.md)|
|G2-03|七个既有 adapter 的单独迁移与实际验收须核候选；依赖 M1、G2-02 和已合并的 S3GC-01。各 destructive 事务内核 current owner/generation/expiry/fence，effects、absence、receipt 原子提交或回滚；失权旧 owner 无副作用，接管重放稳定、quota 一次。保留共享对象/其他 record、checkpoint、origin tombstone；真实精确 claim 与 guard，root-dependent 责任完成或真实移交后 core 最后删除；缺 adapter 或未结义务 fail closed。|[删除](../spec/contracts/records/deletion-recovery.md)、[投递原语](../spec/contracts/platform/delivery-primitives.md)|
|G2-04|已有 local 持久 I/O 和 D5 局部切片；完整 drain 待 M1、02、03 与 local sealing。逐 I/O durable obligation、namespace/incarnation、持久 nonce、跨进程 key guard、keywide 核对和封口；真实 root 已无后重启接管、精确 cleanup 与 quota 一次，阻塞 Put 保持 pending，迟到 version 仅绑定原义务。取消、超时、lease/URL 到期或单次 absence 不证明远端关闭；未知结果可接管且不假报终态。|[删除](../spec/contracts/records/deletion-recovery.md)、[投递原语](../spec/contracts/platform/delivery-primitives.md)|
|G2-05|已有客户端存储/传输与 adapter 局部切片，整票与 M1 待验。真实 event/target/receipt 与效果原子，root 消失后可送达；两 context/两 tab 实测在线关闭、丢 ack 重放、IDB abort 不 ack、迟到 save/unmount/切换拒绝、logout 清当前用户而 401 不删、其他用户/记录保留；启动 TTL 和重连 reconcile。离线副本 unknown/null，不承诺远端设备擦除。|[删除](../spec/contracts/records/deletion-recovery.md)、[Web](../spec/contracts/records/web-surfaces.md)|
|G2-06|已有 signed 来源、copy、索引和 preview 局部切片；真实签发与 A purge/B 保留待验。真实 A/B 包含 metadata-only 输入，历史 canonical 校验与补索引，损坏或未认证阻塞；B 的 payload/refs/lineage/正文 hash 保持，其他 owner 的 distinct 结果精确计数且不泄露身份。浏览器成功/丢响应不重复创建 B，A 失效后旧 save 不得发布。|[删除](../spec/contracts/records/deletion-recovery.md)、[证据快照](../spec/contracts/evidence-snapshot.md)、[Web](../spec/contracts/records/web-surfaces.md)|

## D1–D7、P1–P5 与 Q1 关闭条件

这些项目均须分别记录实现、本候选验收、独立审查、Git 交付和环境证据。历史局部结果只作为对应切片的线索，以下条件均未由本页声明已通过。

|项目|关闭条件|合同|
|---|---|---|
|D1：markdown client|G2-05 的真实 adapter 与双 context/双 tab 清理、事务完成后 ack、未知离线副本披露和其他数据保留；无权 record 不可枚举。|[删除](../spec/contracts/records/deletion-recovery.md)、[Web](../spec/contracts/records/web-surfaces.md)|
|D2：comparison|G2-06 的真实 A/B、历史索引完整性、保留 B、精确聚合、身份保护与旧 intent 拒绝；不能自动改写 B。|[删除](../spec/contracts/records/deletion-recovery.md)、[证据快照](../spec/contracts/evidence-snapshot.md)|
|D3：远端 capability|核实际支持的 provider 合同与慢 body PUT 跨 gate/expiry、断连和晚完成的真实结果；权威证据能关闭所有获准迟到物化才可终态。缺权威 closure 保持 pending，不自定宽限期；MinIO 样本不代替未知真实 provider。|[投递原语](../spec/contracts/platform/delivery-primitives.md)、[删除](../spec/contracts/records/deletion-recovery.md)|
|D4：旧能力和崩溃转换|核历史签发字段与 operation；provider 已接受后 kill/丢响应、root 已无的真实恢复能找回全部物化身份；证据不足则拒绝受影响记录准入并保留补证/迁移前置，不能等 TTL 或补造授权。|[删除](../spec/contracts/records/deletion-recovery.md)、[集成恢复](../spec/contracts/records/integration-recovery.md)|
|D5：publication 与共享|真实 prepared/unbound shared 和 bound noncurrent 交错保留 refs/pins/bytes；旧版本删除须有精确授权，普通 publication current-only，未知保持 pending；跨进程封口及 root-absent 接管另证，不能借 GC 扩权。|[投递原语](../spec/contracts/platform/delivery-primitives.md)、[删除](../spec/contracts/records/deletion-recovery.md)|
|D6：外发与 artifact|明确 processing/unknown/delivered 去重及 root-independent finalize 身份、旧 export/import/PDF artifact 身份转换；真实 send 前 purge 后迟到 finalize、PUT 成功丢响应后重启。不可撤回已发送/下载副本，未知不重发、不假完成。|[删除](../spec/contracts/records/deletion-recovery.md)、[协作通知](../spec/contracts/records/collaboration-notifications.md)、[可移植性](../spec/contracts/records/portability.md)|
|D7：生命周期与自动策略|真实首次、同 key 同 body 重放、异 body 拒绝、新 key 同状态冲突、丢响应及 restore 后旧 purge；不刷新 `trashed_at`，旧重放不撤销后续状态，保留旧 archive 语义。可信 policy issuer 绑定规则与候选，selection→gate 时重新验证授权、最老顺序及引用；完成 UI 和自动过期/容量淘汰。|[删除](../spec/contracts/records/deletion-recovery.md)、[Web](../spec/contracts/records/web-surfaces.md)|
|P1：授权与 owner|真实受限角色下缺权限、错 owner/过期 fence 零越权删除，不能用管理员或 mock 代替。|[授权](../spec/contracts/records/authorization.md)、[删除](../spec/contracts/records/deletion-recovery.md)|
|P2：活跃写入竞争|实际 edit/upload/publication 与 active 直接清除两顺序只有可解释的串行结果；终态后旧 writer 不发布。|[删除](../spec/contracts/records/deletion-recovery.md)|
|P3：恢复竞争|trash restore 与 purge 两顺序只允许恢复且保留内容，或清除且不可恢复；旧 intent 不得重授权。|[删除](../spec/contracts/records/deletion-recovery.md)|
|P4：崩溃与重试|实际事务、receipt、I/O 中断和重启接管，原子回滚或可核对地重入，效果与 quota 不重复。|[删除](../spec/contracts/records/deletion-recovery.md)、[投递原语](../spec/contracts/platform/delivery-primitives.md)|
|P5：共享对象|active 或其他保留内容引用的对象始终可读，仅清除无剩余引用且获准的精确对象版本。|[删除](../spec/contracts/records/deletion-recovery.md)|
|Q1：容量与淘汰|以独立 oracle 复算全部保留正式 revision 正文、附件/预览和 canonical evidence 字节；trash 内共享对象去重，逻辑占用与物理释放分别报告。每次重新计算并淘汰最老整记录，覆盖零释放、共享及授权/并发变化。默认 30 天/1 GB 可配置。|[删除](../spec/contracts/records/deletion-recovery.md)|

## 后续执行：N source/native

以下是后续命令，本轮源码收敛不执行 VM、browser prepare 或 source build。`R` 指向保留的历史运行数据，代码入口使用维护树。先检查已有证据；一次候选仅在 build gate 通过后运行一次 exact，exact 非零仍运行 verify，失败停止后续 source。原锁必须贯穿 source build/run/verify/negative 的串行操作。

维护树的轻量检查是 `make test-g2-layout`（随 `make test-ci-tools` 运行，不需要历史资产）。依赖历史 R/P0 数据的 source 单元测试在 `scripts/g2/source` 下运行 `python3 -B -m unittest -v source_protocol_test source_copy_test source_resource_test source_retirement_test`；其中 `test_go_early_fixture` 需先用 `go test -c ./source_supervisor/bpf-authority` 生成测试二进制，并以 `G2_EARLY_RESULT_TEST_BINARY` 指向它，缺失即错误，不能以 skip 放行。

```bash
R="$PWD/tmp/agent/g2-c80-inventory-ddac5da9673f4c9ba7d2ad68570fd82d"
python3 -B scripts/g2/source/browser_negative.py prepare --run-dir "$R"
python3 -B scripts/g2/source/source_host.py build --run-dir "$R"
```

确认 prepare 和 build 均 PASS 后，执行 exact；即使失败也必须执行 verify，并保存两者退出状态。任一非零、结果非 PASS 或必要 native 证据缺失时停在此处。

```bash
exact_status=0
python3 -B scripts/g2/source/source_host.py run --case realm-negative --scenario bpf-broker-kill --run-dir "$R" || exact_status=$?
verify_status=0
python3 -B scripts/g2/source/source_host.py verify --run-dir "$R" || verify_status=$?
printf 'exact=%s verify=%s\n' "$exact_status" "$verify_status"
```

仅 exact、独立 verify 和必要 native 证据均通过后，逐步执行以下命令，每一步失败停止后续步骤：

```bash
python3 -B scripts/g2/source/source_host.py run --case realm-negative --scenario observer-capability --run-dir "$R"
python3 -B scripts/g2/source/source_host.py verify --run-dir "$R"
python3 -B scripts/g2/source/source_host.py run --case realm-negative --scenario bpf-authorization --run-dir "$R"
python3 -B scripts/g2/source/source_host.py verify --run-dir "$R"
python3 -B scripts/g2/source/source_bpf_negative.py --run-dir "$R"
```

还须核 registration/WATCH 顺序、held launcher→target、两个 owner probe 的单次只读 bind 与 retired grants、后续 realm registration、完整 pinned-runc OCI 和 broker-kill 证据。然后按 `source_host.CASES` 与 `source_supervisor.contract.scenarios(case)` 原序补齐剩余 51 个场景，每次 run 后 verify；只复用源码、runner、工具和输入未变的同候选 PASS。

## 后续执行：P/J 环境和产品验收

以下命令只在工作区根的独立 P/J 子 shell 执行，不继承本轮源码检查的 `TMPDIR`、离线 Go module cache 或 `GOPROXY=off`/`GOSUMDB=off`。创建的 PJ 是本次 owned 目录，先核 realpath 在工作区内、文件系统为 btrfs，`tmp` 为 0700；各 lane 分开保存 argv、条件、stdout/stderr 与退出结果，结束后把必要日志保存到本轮私有证据目录，公开记录不写秘密。使用项目正常 Go/npm/Playwright 缓存和网络/代理；获取锁定依赖只按 [贡献指南](../../CONTRIBUTING.md) 的门禁。source 夹具仍只用已有 cache。

```bash
(
  set -eu
  W="$(pwd -P)"
  export PJ="$(mktemp -d "$W/tmp/agent/g2-acceptance-XXXXXXXX")"
  chmod 0700 "$PJ"
  mkdir -m 0700 "$PJ/tmp"
  case "$(realpath "$PJ/tmp")" in "$W"/tmp/agent/g2-acceptance-*/tmp) ;; *) exit 1 ;; esac
  test "$(stat -f -c %T "$PJ/tmp")" = btrfs
  export TMPDIR="$PJ/tmp" TEMP="$PJ/tmp" TMP="$PJ/tmp"
  export GO=/home/murray/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.2.linux-amd64/bin/go
  export PATH="$(dirname "$GO"):$PATH" GOTOOLCHAIN=local GOWORK=off GOENV=off
  export CGO_ENABLED=0 GOFLAGS=
  test "$(command -v go)" = "$GO"
  test "$(command -v gofmt)" = "$(dirname "$GO")/gofmt"
  "$GO" version
  # 对照现有固定工具链的字节 pin 核验 go 和 gofmt 后，进入隔离子 shell。
  bash --noprofile --norc
)
```

在启动的子 shell 中运行以下命令并保存各 lane 证据，完成后 `exit`；普通 lane 使用 `CGO_ENABLED=0`、空 `GOFLAGS`，race 单独设置 `CGO_ENABLED=1 GOFLAGS=-race` 并记录实际 C 编译器。每项失败时停止受影响的依赖步骤。

```bash
scripts/test-record-platform-integration.sh postgres -- "$GO" test -v -count=1 -p 1 -timeout=30m ./internal/center/store -run '^(TestPostgresIntegrationRecordDeletionV2(WorkerLifecycle|LocalAuthority|LocalDispatch|TransactionLocks|FinishResponsibilities)|TestPostgresIntegrationRecordDeletionClientAndComparison|TestPostgresIntegrationLegacyAdaptersRejectUntrustedAuthority|TestPostgresIntegrationLocalBlobObligationLifecycle|TestPostgresIntegrationLocalDraftCleanupContract)$'
scripts/test-record-platform-integration.sh postgres-v2-locks -- "$GO" test -v -count=1 -p 1 -timeout=30m ./internal/center/store -run '^TestRecordDeletionV2PreparedLocks$'
scripts/test-record-platform-integration.sh postgres -- "$GO" test -v -count=1 -p 1 -timeout=30m ./internal/center/store/migrate ./internal/center/store -run '^(TestAppACLCurrentDeletionWorkerBindingAndHistoricalC80|TestPostgresIntegrationRecordDeletionSuccessorAuthority|TestPostgresIntegrationAppACLCurrent|TestPostgresIntegrationRecordDeletion.*|TestPostgresIntegrationLocalBlob.*)$'
bash scripts/test-pg16-catalog.sh postgres:16.0
bash scripts/test-pg16-catalog.sh postgres:16.6
bash scripts/test-pg16-catalog.sh postgres:16.12
bash scripts/run-records-integration.sh --profile local
CGO_ENABLED=1 GOFLAGS=-race bash scripts/run-records-integration.sh --profile s3
```

每个所选 parent 及 `TestPostgresIntegrationRecordDeletionSuccessorAuthority` 实际 RUN/PASS、零 skip；prepared 四支为 nonterminal/terminal × COMMIT/ROLLBACK PREPARED，均真实 APP 重启并核 runner 自有容器、volume、system identifier 和 GID。验证普通与 immediate constraints、迟到提交/回滚、savepoint、阻塞接管、fresh owner、物理 unlink/guard、receipt 和 quota。issuer 通过既有 profile 路径实际运行 `TestPostgresIntegrationRecordDeletionV2Issuer`：同 snapshot Preview/Execute、三个 inventory、birth 和 serving lease 拒绝、九 adapter。遵循 [集成恢复合同](../spec/contracts/records/integration-recovery.md)：R2 只在三版本 catalog，十个 S3 store 场景只在 S3 profile，prepared parent 只在 `postgres-v2-locks`；通用 selector 不用 skip 代替专用依赖。

四个 ELF 在同一子 shell 的新建目录中构建并记录 hash；run root 必须已存在、绝对路径、无 symlink、当前 uid 且 0700、位于 checkout 的 `tmp/agent` 下：

```bash
mkdir -m 0700 "$PJ/bin" "$PJ/http-run"
"$GO" build -o "$PJ/bin/houfeng-center" ./cmd/houfeng-center
"$GO" build -o "$PJ/bin/houfeng-content-processor" ./cmd/houfeng-content-processor
"$GO" build -o "$PJ/bin/houfeng-record-platform-admin" ./cmd/houfeng-record-platform-admin
"$GO" build -o "$PJ/bin/deletion-smoke" ./scripts/g2/http_smoke/deletion_smoke.go
sha256sum "$PJ/bin/houfeng-center" "$PJ/bin/houfeng-content-processor" "$PJ/bin/houfeng-record-platform-admin" "$PJ/bin/deletion-smoke"
G2_SMOKE_BIN_DIR="$PJ/bin" G2_SMOKE_RUN_ROOT="$PJ/http-run" scripts/test-record-platform-integration.sh postgres -- python3 -B scripts/g2/http_smoke/bootstrap.py
```

实际 smoke 必须观察受限角色、HTTP 上传→processor→下载/import/export、中断和重启、stream 拒绝与排空、九 adapter 的 core-last、共享内容保留、独占/临时数据清理与 fsync、quota 一次、重启后的终态重放，以及两个永久删除 endpoint 的真实 503。此隔离 smoke 不开放生产 HTTP。

N/P/K 闭合且完成同候选冻结后，J 按实际环境继续：

```bash
CGO_ENABLED=1 "$GO" test -race ./internal/center/recorddeletion ./internal/center/store -run 'Deletion|Purge|Recovery|Reservation' -count=10
bash scripts/run-records-browser.sh
bash scripts/run-records-recovery.sh --profile local --all
bash scripts/run-records-recovery.sh --profile s3 --all
make verify-go
bash scripts/test-business-postgres.sh
env -u GITHUB_ACTIONS make verify-ci
```

检查 Begin-only/ready disposition、多 generation、C81 fresh recovery/dual carriers/systemd、三 normal 与 recovery；same APP/RID 的 C80 A retirement、W1 common head、C80 B Preview/Execute、join/refence、C81 convergence、handoff/V1 reconciliation（含 C81 A refusal 与 B mutation），以及 credential/birth/namespace、跨 RID owner、identity→inventory→seal/activation。通用 recovery 的成功不代替 K 的 source 物理恢复。实际 source/产品/配置变化时重新评估受影响证据；任何缺失、skip、失败或未知均不得记为通过。

## OLD 工作区未解差异

旧工作区 `feat/g2-durable-deletion`（`7d2f5cf6`）的 218 项未提交改动已在本轮私有归档中精确保全并实际恢复验证，之后按归档可恢复且无活动依赖退役，不代表本分支是其语义超集。逐项归属：96 项与本分支同字节，56 项行为已保留，15 项由不同实现覆盖，**51 项未证明等价**（仅表示未证明，不等于已确认缺陷）。下列文件在 J 联合接受前须逐项关闭：证明已被现行实现、测试与合同覆盖，或补回缺失行为/测试，或经合同修订明确放弃；不得以行匹配率、函数名消失或 integration-proof 代证。旧内容的取回位置记录在私有归档索引，不作为 clean checkout 依赖。

- `cmd/houfeng-record-platform-admin/`：`main_test.go`
- `db/migrations/`：`0081_record_deletion_successor.sql`（约束、索引、触发器、函数、grant 与有效角色权限的完整对照）
- `docs/spec/contracts/`：`platform/app-acl-current.md`、`records/integration-recovery.md`
- `internal/center/attachments/`、`internal/center/portability/`：`deletion_adapter_test.go`
- `internal/center/deploy/`：`production_compose_static_test.go`
- `internal/center/evidence/`：`comparison_dependencies.go`、`comparison_dependencies_test.go`、`comparison_participant.go`
- `internal/center/http/handlers/`：`record_client_cleanup_test.go`
- `internal/center/recordclientcleanup/`：`types.go`
- `internal/center/recorddeletion/`：`issuer.go`、`issuer_test.go`
- `internal/center/records/`：`revision_mutation_resources.go`、`revision_mutation_resources_test.go`、`revisions.go`
- `internal/center/store/`：`attachments.go`、`attachments_publication_crash_postgres_integration_test.go`、`attachments_publication_test.go`（本分支已删除）、`comparison_deletion_preview.go`、`comparison_dependencies.go`、`comparison_dependency_maintenance.go`、`comparison_participant.go`、`evidence_recovery.go`（本分支已删除；正向 source 恢复缺口归 K，不得称已交付）、`record_client_cleanup.go`、`record_collaboration_deletion.go`、`record_collaboration_participant_postgres_integration_test.go`、`record_collaboration_providers.go`、`record_deletion_client_comparison_postgres_integration_test.go`、`record_evidence_participant_test.go`、`record_mutation_resources.go`、`record_revision_mutation_discovery.go`、`record_revision_mutation_discovery_attachments.go`、`record_revision_mutation_discovery_postgres_integration_test.go`、`records_test.go`
- `internal/center/store/migrate/`：`acl_manifest.go`、`acl_manifest_effective_catalog_postgres.go`、`app_acl_current_contract.go`、`app_acl_current_deletion.go`、`app_acl_current_deletion_postgres_test.go`、`app_acl_current_deletion_test.go`、`app_acl_current_postgres_integration_test.go`、`app_acl_current_replay.go`、`app_acl_current_transition.go`、`records_authority_app_acl_test.go`
- `web/src/`：`lib/comparisonSaveAttempts.test.ts`、`lib/recordClientCleanup.test.ts`、`lib/recordClientCleanupConsumer.test.ts`、`lib/recordReadCleanup.ts`、`pages/RecordComparisonPage.test.tsx`
