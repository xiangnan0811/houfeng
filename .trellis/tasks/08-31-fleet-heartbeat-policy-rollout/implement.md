# Fleet current successor、修复发布与生产滚动升级 Implementation Plan

> **Execution authorization:** 用户已明确批准按四个连续、证据绑定阶段执行：A code/PR/CI，B exact v0.79.6 Release Please merge/publish，C `hostcram` Center maintenance/cold backup/database transition，D `netcup`→`informaten` Agent installers/canary及task-owned普通临时物清理。批准不包含删除production recovery points、Agent bundles或任何production failed-state quarantine。任一exact SHA/version/PR-files/host/path/live-state/backup漂移或新增范围仍立即停止并重新获批；批准本身从不替代go/no-go证据。

**Goal:** 交付一个仅接受 exact 0062 predecessor 的原子 0063 successor协议，发布新的修复 patch，并把生产 Center 与两台 Agent从v0.79.4安全升级到该版本。

**Architecture:** current source compiler注册一个有限transition；convergence与runtime admission共享manifest shape verifier；一个SERIALIZABLE closure验证predecessor、应用0063、追加revision2并CAS head。发布后以完整cold restore point保护Center，以单机rollback bundle保护Agents。

**Tech stack:** Go 1.26.2、pgx v5、PostgreSQL 16.12、Docker Compose、systemd、GitHub Actions/Release Please、Trellis。

## Stop conditions

- 任一目标不是已确认的 `hostcram`/`netcup`/`informaten`，或 endpoint不是 `fleet.yading.de`。
- worktree/branch出现不属于本任务的dirty state，或hooks未启用。
- live state不再是Center v0.79.4 + 0062 + revision1、Agents official v0.79.4，且偏差未重新评审。
- strict PostgreSQL/Compose rehearsal有SKIP、使用fake代替生产入口、或不能证明失败零写。
- review仍有Critical/Important/Minor finding，或full gate/CI/release artifact任一失败。
- cold backup未校验、restore rehearsal失败、capacity/authority isolation不明确。
- 生产通知无法保持被动、不存在可用maintenance window、或出现新的真实incident。
- 任何secret可能进入terminal transcript、git、task artifact或共享日志。
- Release Please不是从v0.79.5生成的精确next `v0.79.x` patch（当前必须为v0.79.6），PR body/files/source range含无关变更，或阶段go/no-go receipt与获批证据不一致。
- Center/Agent host-specific执行脚本未通过`set -Eeuo pipefail`、root-`BASHPID`单次恢复、trap/complete-marker、capacity/path和failure-injection评审，或Agent compatibility diff/条件式非空queue升降级演练未完成。

## TDD and delivery checklist

- [x] **1. 获批后正式启动并冻结基线**
  - 运行：

    ```bash
    python3 .trellis/scripts/task.py start .trellis/tasks/08-31-fleet-heartbeat-policy-rollout
    sh scripts/setup-git-hooks.sh
    git status --short --branch
    git diff --check
    GOTOOLCHAIN=go1.26.2 go test ./internal/center/store/migrate ./internal/center/deploy ./cmd/houfeng-record-platform-admin -count=1
    ```

  - branch必须是 `codex/fleet-heartbeat-policy-rollout`，release trust roots必须精确解析为`v0.79.4=1481a558b136c2e6e00e59d523fe281acd655ae8`、`v0.79.5=e427f41b73b3b799f581274ebb1ad11ced56f421`，base固定为`origin/main@89fcf16af98e3bfcd3927309e1d16f3301195e07`；任何新main要求先审阅漂移并重新获批。
  - 再执行三台host的只读preflight；不重复输出secret或完整日志。

- [x] **2. RED — 生产级 0062→0063 successor**
  - 在 `app_acl_current_convergence_test.go` 将future predecessor场景扩展为明确的registered transition成功目标；旧代码必须先返回rebuild-required。
  - 先写 transition registry compile tests：exact 0062 suffix成功；missing/duplicate/unknown/out-of-order/overlap/privilege-changed及released golden drift必须分别经convergence/runtime真实入口失败且transaction opener零调用。
  - 先写manifest writer tests：revision2 body、previous digest、insert字段、head old revision+digest CAS、RowsAffected、insert/head/readback error。
  - 先断言symbolic `v0.79.4`仍指向精确commit `1481a558b136c2e6e00e59d523fe281acd655ae8`，再从该SHA离线生成并人工复核仓库内golden（63-entry name/checksum canonical body、固定database/roles privilege body、revision1 digest）；测试运行时禁止读取git/network。先写fixed predecessor与golden逐entry/逐byte比较RED，并以pre-0063 migration/fragment/canonicalization mutation负例证明oracle独立。
  - focused RED：

    ```bash
    GOTOOLCHAIN=go1.26.2 go test ./internal/center/store/migrate \
      -run 'Test(AppACLCurrentTransition|ConvergeAppACLCurrentRegisteredPredecessor|InsertAppACLManifestSuccessor)' -count=1
    ```

- [x] **3. GREEN — compile transition 与原子 writer**
  - 新增最小私有transition类型/registry/compiler；它可以从current immutable files构造candidate，但必须先与独立v0.79.4 golden完整匹配，不能把动态prefix本身当oracle。
  - 不接受任意prefix，不导出现场migration API，不修改0063或更早migration。
  - 新增revision2 insert + old-head CAS helper，复用manifest canonical constructor/SQL字段顺序并完整wrap error。
  - 重跑步骤2 focused至GREEN。

- [x] **4. RED/GREEN — convergence 状态机**
  - table tests覆盖fresh、exact current revision1、registered 0062 predecessor、exact revision2 repeat、unknown/partial/mixed/advanced state。
  - 每个transition cutpoint注入失败并断言rollback、commit=0及后续seam零调用；serialization retry完整重读。
  - 实现predecessor preflight → catalog verify → apply registered pending source → current ledger/catalog verify → revision2/head CAS → final readback。
  - transition-specific preflight在写前拒绝任何预置0063同名index/错误released column default，并抓取settings逻辑snapshot；post-apply产品verifier在revision2 insert前精确验证current default、3→12/20与override保留、index keys/order/include/predicate。verifier失败与每个cutpoint都必须rollback migration ledger/settings/index/manifest/head。
  - exact revision2 repeat必须不调用apply/DCL/manifest writer。
  - 保留safe typed rebuild/corruption error分类。

- [x] **5. RED/GREEN — shared runtime admission**
  - 在 `app_acl_current_runtime_admission_test.go` 先写revision1 current与registered revision2成功；unknown successor、wrong previous digest、old/latest privilege drift、ledger mismatch在catalog read前失败。
  - 抽取 convergence/runtime共享的current manifest shape verifier，避免两份registry判断。
  - `REPEATABLE READ READ ONLY`、identity、role、catalog snapshot与commit/rollback合同保持。

- [x] **6. RED/GREEN — real PostgreSQL 16 upgrade**
  - 扩展strict `TestPostgresIntegrationAppACLCurrent`：由独立v0.79.4 golden验证source/fragment/privilege/manifest后真实构造exact 0062 ledger/revision1/catalog，再只调用生产 `ConvergeAppACLCurrent`。
  - 使用两个隔离fixture/subtest及独立durable before/after/repeat snapshot：A为singleton global 3 + override 3 + heartbeat rows，断言3→12且override保持；B为singleton global 20 + override 3 + heartbeat rows，断言两者保持。两案都断言0063 checksum、index exact、revision1/2 chain/head、runtime admission；不得在一个singleton `center_settings` snapshot内伪造两个global值。
  - repeat前后完整durable snapshot深相等；unknown predecessor/cutpoint前后相等。
  - 预置public同名错误index、错误released default与partial-0063负例；断言transition零写或整事务rollback，绝不能发布revision2。
  - 验证direct runtime Records写/读与attachment readback；ACL tuple不扩张。
  - strict命令实际RUN/PASS且无SKIP：

    ```bash
    GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- \
      go test -v ./internal/center/store/migrate \
      -run '^TestPostgresIntegrationAppACLCurrent$' -count=1
    ```

- [x] **7. RED/GREEN — Compose production caller upgrade**
  - 在build-tagged successor integration test中用test-only frozen-release fixture直接前向materialize old exact state，再通过只替换PostgreSQL transport opener、其余依赖与公开`InitializeCompose`共用production factory的入口执行fixed init；断言role passwords/authority state不被意外重写，heartbeat rows与exact index正确，Records/attachment读回、runtime admission和repeat成功。
  - 不直接注入“success”fake掩盖store逻辑；strict runner中测试必须actual RUN。
  - 回归safe error redaction，db-init失败不继续authority publish/center startup。
  - strict命令实际RUN/PASS且无SKIP：

    ```bash
    GOTOOLCHAIN=go1.26.2 scripts/test-record-platform-integration.sh postgres -- \
      env GOTOOLCHAIN=go1.26.2 go test -tags appacl_release_fixture -v \
      ./internal/center/deploy \
      -run '^TestPostgresIntegrationComposeInitializeUpgradesExactV0794Predecessor$' -count=1
    ```

- [x] **8. 同步 executable specs 与部署文档**
  - 更新 `.trellis/spec/backend/database-guidelines.md`：registered predecessor/successor matrix、single transaction、shared verifier、future transition registration与generic prefix禁止；把current inventory从陈旧的53-entry/0052同步为64-entry/tail=0063，同时保持frozen R1/R2历史计数与语义原样。
  - 更新 `.trellis/spec/backend/quality-guidelines.md`：frozen predecessor、strict PG、Compose caller、cutpoint、repeat/read-only、release rehearsal gates。
  - 更新 `docs/deploy/local-and-systemd.md`：supported predecessor声明、cold backup/restore rehearsal、init fail stop、schema后rollback必须完整cold restore。
  - 修正源码注释中“development/current genesis only”等已失真语句；frozen R1/R2文档保持。

  - Evidence（2026-09-01）：transition compiler首轮因undefined production symbols真实RED，writer/shape/runtime/convergence各自先RED后GREEN；production role binding测试先暴露错误`houfeng_center_runtime`并修正为released/live `houfeng_runtime`。第二轮将successor子测试接入required strict anchor，补齐full settings snapshot、heartbeat/index、Records/attachment与非允许settings漂移rollback；strict `TestPostgresIntegrationAppACLCurrent`完整RUN/PASS且无SKIP。Compose先因缺production caller seam/released fixture真实编译RED，继而以build-tagged frozen v0.79.4前向fixture及production dependency factory转GREEN；最终fixture使用固定63-entry migration golden、显式11-fragment release清单和独立semantic digest `6a002ce19e63ab9dc7353618227022e5e4259679d31a638acc25db858f8c769c`，不调用product transition compiler或动态截取current fragment registry，strict Compose真实RUN/PASS且无SKIP，并完成Records/attachment读回。全anchor另暴露unknown future shape先被privilege mismatch遮蔽，新增unit RED后调整classification顺序并由focused strict PG复证。missing/duplicate/unknown/out-of-order/overlap/privilege/golden各错误也都经convergence与runtime真实入口证明`BeginTx=0`。released v0.79.4 migration/privilege/manifest goldens与生产只读revision-1摘要逐字一致；生产尚无mutation。

- [x] **9. Focused gates 与两轮独立审查**
  - 运行affected packages、go vet、strict PG/Compose、`git diff --check`、secret scan。
  - 使用 `trellis-check` 与独立agents分别做spec/security/database/upgrade/operations审查；findings按Critical→Important→Minor修复。
  - 修复后重新运行受影响RED/GREEN与审查；最后一轮必须明确零finding。

  - Evidence（2026-09-01）：11个Bash fence `bash -n`、affected `go vet`、Trellis validate与`git diff --check`均PASS；strict PostgreSQL完整anchor与build-tagged production Compose successor均真实RUN/PASS且无SKIP。冻结dirty digest `590466d5136a51d8236ecedca4e15b06bf15c18b87c57195cb7b78f6d3cf11af`、task digest `85befa80360edc2359160784fa6d0edb34d9dd8ed0aed28a5dc46f7b37484bdc`连续复算一致；代码/数据库、规格一致性、生产Shell三路独立只读复审起止摘要一致并全部返回`ZERO_FINDINGS`。

- [x] **10. Full local verification**
  - Fresh运行：

    ```bash
    GOTOOLCHAIN=go1.26.2 make verify-go
    PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web
    PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test:e2e
    python3 scripts/test_visual_evidence.py
    git diff --check
    git status --short --branch
    ```

  - 没有Web行为变化也必须运行项目规定full gates；任何known unrelated failure先复现、定位、按规范处理，不得直接豁免。
  - 使用 `superpowers:verification-before-completion` 对fresh输出取证。

  - Evidence（2026-09-01）：`GOTOOLCHAIN=go1.26.2 make verify-go`退出0；Node 22 `make verify-web`退出0（206 files/1623 tests、lint、coverage、build、bundle、CSS）；Playwright E2E 136/136 PASS；visual evidence 6/6 PASS；随后strict PostgreSQL anchor 12.55s与production Compose successor 1.69s再次fresh PASS。

- [ ] **11. Commit、PR、CI、merge**
  - 只stage本任务文件，复核 staged diff/secret scan后commit；发出Git directive仅在真实成功后。
  - push feature branch并创建ready PR；监控required CI。失败在同branch按TDD修复并重新审查。
  - required checks全绿后merge；核对merge SHA、`origin/main` exact CI与未触发意外release。

- [ ] **12. Release Please 与公开fixed artifacts**
  - 监控/处理Release Please PR；必须是从v0.79.5生成的精确next `v0.79.x` patch，即当前v0.79.6，不接受任意更高版本替代。
  - 合并前先确认`v0.79.5=e427f41b73b3b799f581274ebb1ad11ced56f421`、base=`89fcf16af98e3bfcd3927309e1d16f3301195e07`；单独allowlist `v0.79.5..89fcf16af98e3bfcd3927309e1d16f3301195e07` 的`8f8808d4d72de7233f1181cf2f135ebf7818b216`/`1ebae26c54fea96e8e2fed1aa2e47f09ad5e3646`/`c8c1030fa09f111c6a895230393737a51ab5c193`与merge metadata，再审阅`89fcf16af98e3bfcd3927309e1d16f3301195e07..<feature merge SHA>`只含本任务、Release Please PR body/files只含预期release metadata。任一额外diff、tag/base或版本漂移停止并重新获批。
  - release PR checks全绿后merge，监控release-main与publish-images。
  - 复核GitHub release tag→source SHA、Center multi-arch OCI index/amd64 manifest、Agent amd64/arm64 SHA/static/buildinfo、minisign、Compose assets；`latest`仅用于一致性核对，生产固定immutable digest。
  - 在v0.79.4与fixed SHA分别对`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`和`GOARCH=arm64`运行`go list -deps -json ./cmd/houfeng-agent`，冻结每个架构实际选中的repo-local package/目录/Go+Cgo+embed文件并比较两架构并集；另显式加入Center installer-command generator、installer、unit/env template、`go.mod`/`go.sum`、`Makefile`与`.github/workflows/publish-images.yml`，审阅完整range diff并单列queue serialization/retry/discard及token argv/stdin/privacy语义。若任一closure/path、持久化格式、installer、env或unit语义有变化，先以非空v0.79.4 queue完成fixed升级和v0.79.4降级恢复演练；无变化则保存两SHA×两架构closure清单和diff-empty/source证据。
  - 如版本不是精确v0.79.6、artifact缺失/漂移、signature失败或Agent compatibility门未通过，停止部署。

- [ ] **13. Production final read-only preflight**
  - `hostcram`：v0.79.4 image/revision、63/0062、manifest revision1、health、Compose render/mounts、capacity、local attachment backend、settings/override fingerprint、active incidents、notification mode。
  - `netcup`/`informaten`：official v0.79.4 SHA/buildinfo、unit、timer/cron/package ownership、endpoint、token mode、queue size/mtime、Center latest heartbeat/version。
  - 逐机解析live FragmentPath/drop-ins/ExecStart/EnvironmentFile/User/Group/StateDirectory/ReadWritePaths/token/buffer路径及uid/gid/mode；与canonical v0.79.4做sanitized semantic diff。任一live drift与source diff同等触发非空queue升降级rehearsal并改用host-specific resolved paths。
  - 建立private execution receipt目录，内容只含安全摘要与checksums，不含secret/raw config/raw logs。
  - 形成阶段C go/no-go receipt：exact fixed SHA/digest、Release PR范围、host/绝对路径、live v0.79.4/0062/revision1、capacity、resolved mounts与维护窗口必须仍等于获批证据；偏差即停。

- [ ] **14. Center cold recovery point**
  - 把已复核值固化为host-specific脚本；生成SHA-frozen root wrapper，以`env -i`+绝对`/bin/bash --noprofile --norc`调用，任何rollout写前取得canonical host-wide `flock`，拒绝并发updater/rollout并持锁到child exit/receipt/cleanup。脚本必须`set -Eeuo pipefail`并用root `BASHPID` guard保证函数/子shell失败只恢复一次，解析并验证精确backup target，不使用`~`、`$HOME`、宽泛glob或已有目录，并在停机前验证capacity及Compose全部resolved bind sources均在归档或精确额外来源清单内。
  - 先保存固定local v0.79.4 image ID；停stack前arm error/signal recovery trap，任何后续失败都以`--pull never`尝试恢复exact old stack并保留原失败码，末尾health不得掩盖前序失败。脚本需shellcheck/评审并做failure-injection演练。
  - 用无敏感dummy值渲染真实Compose回归source parser：冻结local Docker socket/metadata/daemon ID/platform/空client config，所有direct Docker/Compose使用同一显式daemon；Compose必须`env -i`、显式archived `.env`和精确common+proxy-host `-f`，注入`DOCKER_*`/root context/`COMPOSE_*`、Telegram、public URL、attachment/comparison变量均不能覆盖。六个`environment:` secrets绑定`.env`，bind/file source须存在、非symlink、可读、在archive且出现在tar listing，named/unknown/missing source与同project orphan/extra container失败。解析saved-image manifest证明三旧config digest及PG/ClamAV ref映射。Bash语法、hostile caller env、host lock/concurrent updater、root-only ERR recovery恰好一次、恢复中第二信号、producer输出预期值后非零/超时仍fail closed、delayed readiness、旧0062 observed-at-index 768-row reader、55秒validation cutoff/85秒total及30秒recovery reserve均有无副作用/strict PG/clone failure-injection测试；0063 exact index另在fixed/Agent reader门验证。
  - 完整归档deployment directory；保留ACL/xattr/numeric owner，生成checksum并验证archive listing/size/required paths。
  - 立即用old version `--pull never`恢复生产并验证v0.79.4/0062/revision1/authority/Agent heartbeats；仅在所有验证通过并原子写入cold-backup complete marker后解除trap，wrapper真实观察脚本exit0并原子发布绑定marker/lock的`COLD_BACKUP_EXECUTION_RECEIPT`前不继续。

- [ ] **15. Isolated restore + fixed upgrade rehearsal**
  - 从cold point恢复到新、明确、private目录；使用唯一 `-p` project、仅common `compose.yaml`，不得加载production proxy overlays或沿用production project name。
  - 在启动前检查rendered config没有published port、external proxy network或production bind mount；只启动DB/必要init jobs并执行private direct-runtime probes，不启动长驻Center/第二authority，也不连接production Agents。
  - 使用fixed immutable image执行db/init/runtime upgrade；验证0063/revision2/settings/index/catalog/Records/attachments与repeat。
  - 记录sanitized receipt后停止并移除精确rehearsal containers/network/directory；cold source保留。

- [ ] **16. Production Center cutover**
  - 最终snapshot后只把已验证fixed Compose assets放入生产树外的root-only staging目录，保留proxy mode与secret `.env` values的预期语义并固定image digest；此步不得停写或改live tree。
  - cutover前重做阶段C go/no-go；生成并shellcheck/failure-inject独立host-specific脚本，重算cold marker绑定的archive/source-lists/三旧images/private receipt与staged assets，在首个生产树asset替换、`.env` image pin、停写、service stop或DB mutation前arm完整cold-restore trap。所有live asset写入都在该armed closure内。
  - config validate、pull/inspect、`up -d`；逐项检查init exits、health/restarts、public version、OCI、ledger/manifest/settings/index/catalog/authority/Records/attachments与tight-window notification。
  - 任一失败：trap保留原码、停止exact target、移动完整failed tree到唯一nonexisting production quarantine、校验/恢复cold archive与三旧images、以v0.79.4 `--pull never`启动；复核0062/revision1/authority/Records/attachments/两台post-restore heartbeat与无新增incident/event/notification。restore失败先做有界stop/kill并精确证明target inactive且无partial authority，证明失败则明确unproven并要求service/host isolation；始终保留两份证据且不升级Agents。
  - 只有fixed全量证据与quiet window通过才解除trap并原子发布绑定release SHA/digest、cold marker digest和private receipt的cutover complete marker；持锁wrapper真实观察cutover脚本exit0后再原子发布绑定lock/script/marker/cold receipt的`CENTER_CUTOVER_EXECUTION_RECEIPT`，Agent门逐字段重验二者。mutation/restore各cutpoint、INT/TERM及delayed readiness必须在clone中有失败注入证据。

- [ ] **17. `netcup` Agent canary**
  - 进入阶段D前形成go/no-go receipt：Center complete marker与`CENTER_CUTOVER_EXECUTION_RECEIPT`精确配对、Agent compatibility gate、netcup host/arm64/current official v0.79.4、capacity/path无漂移。
  - 用sanitized/host-lock outer wrapper调用经shellcheck/failure-injection评审的host-specific `set -Eeuo pipefail`+root `BASHPID` guard脚本创建唯一root-only rollback bundle；停服务前arm restart-old trap，短停一致复制binary/unit/env/token/state，SHA-frozen verifier绑定包含根/空目录的state type/uid/gid/mode metadata并拒绝state mount。marker另绑定canonical live parent path/device/owner/mode；restore前按目标 `st_dev` 汇总该设备全部same-filesystem stage字节与headroom并一次性验证容量。checksum/旧heartbeat/marker通过且持锁wrapper直接观察脚本exit0并原子发布绑定marker的backup execution receipt后才继续。
  - 在无副作用dummy bundle上实际运行rollback `--verify-only`，证明host/machine-id/arch/official SHA、canonical bundle/marker/execution-receipt path+digest、rollback-script/state-verifier path+digest、effective unit/env/token/checksum/metadata任一漂移或cross-pair均拒绝。supervisor failure-injection覆盖hostile caller/manager env、host lock/concurrent updater、missing/unknown minisign、installer URL/repo/flags与issuance mismatch/expiry及launch前替换、backup marker signal cutpoints、identity/state/hash producer输出预期值后非零/超时、`sudo use_pty`、transient-unit launch前/dispatch中/return到状态发布之间的record-only INT/TERM cutpoints（rc0先永久锁存acceptance再消费pending；明确非零且exact not-found/no-cgroup才保持旧Agent enabled+active且零disable/stop）、record-only窗口INT→TERM/TERM→INT均确定为130、TERM-only为143且已有signal覆盖随后ERR、loaded-unit空/漂移ControlGroup、dispatch中首次loaded/cgroup→terminate后GC/`not-found`仍锁存accepted并隔离、不可读/非空 `cgroup.procs`、unit result/status/GC、真实下载+安装+fixed-local/first-live的15秒cutoff与85秒total、按设备聚合容量、首rename前disabled读回、local-receipt持久化到phase切换间的信号、local mixed state、marker持久化和rollback/verify-local/cleanup预算；root recovery恰好一次。
  - 最终pre-dispatch闭包先运行同一SHA-frozen `--verify-only`路径，证明当前live old binary/unit/env/token逐字节等于冻结bundle、owner/mode/effective unit一致且服务persisted-enabled+active；随后再次运行SHA-frozen issuance parser重验receipt freshness/expiry与命令语义，再对installer command与issuance receipt做最终byte-hash复核，重查pending signal后才冻结85/15时钟并立即调用`systemd-run`。对backup receipt到launch的live-old漂移、live-old检查期间command/receipt替换及receipt在检查期间过期做失败注入，断言dispatch前拒绝且旧Agent零mutation/disable/stop。
  - 私下生成fixed signed installer到root-only `0600`文件，其SHA与Center issuance data receipt的host/machine/arch/stable-ID/精确v0.79.6/revision/expiry、唯一HTTPS installer URL/public server/release repo、token-stdin/install-missing-deps且无insecure/冲突flag绑定；该data receipt不是execution receipt，持锁supervisor必须在紧邻launch前重算命令/receipt SHA并重跑SHA-frozen parser。冻结已存在canonical minisign SHA/provenance，缺失/未知则停止且不授权自动安装持久依赖。supervisor以sanitized `env -i`在唯一systemd transient service内执行，固定`Type=exec`/`ExitType=cgroup`/`RemainAfterExit=yes`/`KillMode=control-group`；transient launch前无条件冻结85秒total与15秒validation cutoff（取代早期80秒草案），clone先证明真实签名下载、安装、fixed-local与first-live闭包在15秒内完成。timeout/非零/signal/mixed state停止并kill整个cgroup；dispatch acceptance尚未成立时，exact `LoadState=not-found`且预期cgroup路径不存在才可证明未形成installer unit，launch前取消或明确非零dispatch保留旧Agent enabled+active且不得调用其disable/stop；`systemd-run`返回0或已观察到loaded/cgroup后，acceptance必须永久锁存；只有loaded exact unit、精确 `ControlGroup=/system.slice/<unit>` 与regular/readable/empty `cgroup.procs`才允许rollback，后续`not-found`或任何ControlGroup/member proof失败都要求host isolation。cgroup kill、old restore、local receipt、first-live Center receipt、verify-local和cleanup在85秒内完成，至少早于100秒通知边界15秒，不泄露command/token。
  - 证明arm64 public SHA、version/revision、unit active/enabled、NRestarts稳定；对unit/env做sanitized语义比较、token private byte equality、queue drain。
  - Center三批证据必须复用production reader形状：`recent_live AS MATERIALIZED`先按`received_at DESC,id DESC`筛non-backfill并`LIMIT 768`，再在candidate内按batch去重并`LIMIT 3`；candidate内不得按target version过滤。用大历史strict PG `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)`证明0063 exact Index/Index Only Scan、无Seq/Bitmap且scan rows/loops/shared blocks有界。
  - supervisor真实exit0后由持锁root outer wrapper原子发布execution receipt，绑定lock、supervisor script path+SHA、backup marker+execution receipt、installer SHA+issuance data receipt、minisign provenance、first-live+fixed-local receipts及`exit_status=0`。观察无持续transport/auth error、active heartbeat incident或notification后，private orchestrator再原子发布root-only `AGENT_INSTALL_COMPLETE`，绑定该execution receipt path+digest、host/machine/arch、fixed release/binary+unit、issuance data、Center三批与quiet-window receipts；任一链路缺失/漂移不得继续。
  - rollback脚本先拒绝stale/dangling marker、core bundle/stage symlink与cross-pair，再把全部old文件写到各目标同filesystem staging path并fsync校验；持久化disable+systemd-dir sync、stop且精确证明inactive后atomic rename。任一partial失败执行有界disable/stop/kill，不能证明fail-stopped时明确要求主机隔离，绝不启动mixed binary/env/token/unit。完整old set、daemon-reload/enable+sync/start、enabled+active/effective-unit/byte/checksum验证通过后原子发布marker-bound local-restore receipt，再解除停服trap；后续Center receipt失败由supervisor只读复核该receipt与当前old状态，保持健康old Agent运行但不写rollback marker、不promotion。

- [ ] **18. `informaten` Agent rollout**
  - 重复独立backup/install/evidence流程，使用amd64 public SHA。
  - promote前重做阶段D go/no-go并要求netcup的`AGENT_INSTALL_COMPLETE`路径、digest、全部绑定字段及当前live health仍一致；同样执行semantic unit/env、private token、bounded query与rollback fail-stopped合同。
  - 任一失败只回滚`informaten`；不回滚已健康canary或Center，除非跨层证据指向Center regression。

- [ ] **19. Passive production acceptance**
  - readback Center/Agents版本、0063、revision2、settings/global/override fingerprint、latest live batches、active incidents、notification records tight window、restart counts、queues。
  - 明确证明没有人为制造19/20、恢复或provider消息，没有修改global notification destination。
  - 观察窗中发现自然incident只做私有、被动证据，不为验收操纵现场。

- [ ] **20. Final review, archive and cleanup**
  - 再做一次cross-layer/operations独立审查，确认代码、release、live evidence、rollback与privacy均零finding。
  - cold backup、Agent bundles及任何production failed-state quarantine的删除需要单独、明确授权；本次连续实施批准不包含删除。未获授权时保留并报告安全位置/权限/retention，不反复追问或擅自清理。
  - 只清理精确rehearsal containers/networks、downloads/assets及secret command临时文件，证明没有普通secret/temp残留；production quarantine不进入该清单。
  - 更新task evidence/checklist，`trellis-finish-work`归档；核对PR/merge/release/live SHAs。
  - 仅在feature branch已merge且所有现场证据完成后移除local worktree/branch；不得直接修改main，不删除不属于本任务的branch/worktree。

## Planned files

- `internal/center/store/migrate/app_acl_current_{contract,convergence,runtime_admission}.go` 及tests。
- `internal/center/store/migrate/acl_manifest_{genesis,runtime}.go` 或新增窄successor helper及tests。
- `internal/center/store/migrate/testdata/` 下经v0.79.4 tag离线生成并评审的predecessor migration/privilege/manifest goldens。
- `internal/center/store/migrate/app_acl_current_postgres_integration_test.go`。
- `internal/center/deploy/compose_init*_postgres_integration_test.go` 与必要caller/test-only release fixture。
- `cmd/houfeng-record-platform-admin` safe error/caller tests（仅在行为确需更新时）。
- `.trellis/spec/backend/{database-guidelines,quality-guidelines}.md`。
- `docs/deploy/local-and-systemd.md`。
- 本task的PRD/design/implement/research/context/evidence。

不修改 `db/migrations/0063_tune_heartbeat_incident_policy.sql`、更早migration、v0.79.5 tag/artifacts、heartbeat策略或Agent runtime协议。
