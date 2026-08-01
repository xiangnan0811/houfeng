# 集成切换、安全、性能、备份恢复与终验 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`. This repository uses Codex inline execution; do not dispatch implement/check sub-agents. Every behavior follows RED → verify RED → minimal GREEN → verify GREEN → refactor.

**Goal:** 将子任务 1–10 的稳定边界装配为真实可部署的记录平台，交付官方备份恢复、防复活、完整安全/性能/视觉门禁和可回退的最终切换。

**Architecture:** `recordrollout` 保存不可逆写权威与可回退读路由状态；`recovery` 通过显式 adapter registry 编排 application PG + local/S3 Blob 的签名备份和隔离恢复，并依赖独立 ledger/full witness/recovery-control。真实 Compose integration、故障注入、telemetry corpus、固定 benchmark 与六表面 Playwright artifact 共同生成 cutover gate receipt。

**Tech Stack:** Go 1.26.2、pgx/v5、PostgreSQL 16、MinIO/S3 Object Lock、ClamAV/Poppler/Chromium、React 19/TypeScript 6/Playwright/Axe、Docker Compose、GitHub Actions、Node 22.x。

---

## Preflight and authorization boundary

- [ ] 确认用户已单独授权启动本子任务；仅批准本文不能执行 `task.py start`。
- [ ] `python3 ./.trellis/scripts/task.py list` 必须证明子任务 1–10 已完成/归档并合入受保护主线；缺一个直接停止。
- [ ] 对1–10逐项运行Git host只读核验（merged PR base=`main`、required checks成功、merge commit）并执行`git merge-base --is-ancestor <merge_commit> HEAD`；先保存本地无内容临时receipt用于start gate，Task2再由脚本生成schema-validated child-delivery manifest。仅有task状态/本地commit/开放PR/非ancestor或旧run均停止。
- [ ] 从最新 main 创建 `codex/vps-records-integration-rollout` 或同前缀非 main 分支，运行 `sh scripts/setup-git-hooks.sh`；不得在 main/master 上提交。
- [ ] 使用 `trellis-before-dev` 读取 backend/Web/跨层/测试/部署规范；运行 `make verify-go`、Node 22 的 `make verify-web`、`npm --prefix web run test:e2e` 记录 fresh baseline。
- [ ] 运行 `node --version` 并与 `.node-version` 核对；若不是 22.x，先切换工具链。Node 24.18.0 的结果不得写入正式 gate receipt。
- [ ] 核对任务1–10已合入的0051–0059名称与hash并视为不可变；若主线另行占用计划中的0060，只把尚未合入的cutover migration改为下一个空闲编号并同步本任务、父表、tests和fixture，禁止改名/重排/改写0051–0059。
- [ ] 记录届时 `cmd/houfeng-center/bootstrap_test.go` 的真实 worker 数和全部子任务 adapter set；不得沿用父任务的“5”假设覆盖已合入变化。

## Task 1: 0060 cutover schema、状态机与运维命令

**Files:**

- Create: `db/migrations/0060_record_platform_cutover.sql`
- Create: `internal/center/recordrollout/types.go`
- Create: `internal/center/recordrollout/service.go`
- Create: `internal/center/recordrollout/service_test.go`
- Create: `internal/center/store/record_cutover.go`
- Create: `internal/center/store/record_cutover_test.go`
- Create: `cmd/houfeng-records-rollout/main.go`
- Create: `cmd/houfeng-records-rollout/main_test.go`
- Modify: `internal/center/store/migrate/migrate_test.go`
- Modify: `internal/center/store/migrate/postgres_integration_test.go`

- [ ] 先写 migration source 与 PostgreSQL 16 RED tests：fresh、0050 upgrade、repeat apply、phase CHECK、receipt append-only/unique/no-content、禁止 legacy delete/down migration。
- [ ] 实现 0060 的 `record_platform_cutovers` 与 `record_platform_gate_receipts`；所有 stable object/title/content 列扫描命中为 0，repeated migration GREEN。
- [ ] 写状态机 RED matrix：相邻前进、shadow→off、写切换后 legacy 永不重开、读回退、fresh witnessed activation/minimum观察值匹配、stale/伪造observed minimum拒绝、cutover无权威minimum写接口、expired/wrong commit/config receipt拒绝、CAS/幂等冲突。
- [ ] 实现 `recordrollout.Service`，phase更新只接受 `expected_revision/phase/gate_set_digest`；同一 transaction关闭legacy write并开启records write，不存在双写状态。
- [ ] 为 rollout CLI 写参数/JSON/exit-code/confirmation RED tests；实现 `status`、`preflight`、`advance`、`rollback-read`，stdout不含DSN、URL、用户名、record ID或正文。
- [ ] 运行 `go test -race ./internal/center/recordrollout ./internal/center/store ./cmd/houfeng-records-rollout -run 'RecordPlatformCutover|RecordRollout' -count=10`，期望全部 GREEN。

## Task 2: 完整 replay/backup adapter set 与 bootstrap gate

**Files:**

- Create: `internal/center/recovery/adapter_set.go`
- Create: `internal/center/recovery/adapter_set_test.go`
- Create: `internal/center/recovery/replay_conformance_test.go`
- Modify: `internal/center/recovery/replay.go`
- Create: `cmd/houfeng-center/record_recovery_adapters.go`
- Create: `cmd/houfeng-center/record_recovery_adapters_test.go`
- Create: `test/integration/records/child-deliveries.schema.json`
- Create: `scripts/collect-records-child-deliveries.sh`
- Create: `internal/center/deploy/child_deliveries_static_test.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `internal/center/http/handlers/health.go`
- Modify: `internal/center/http/router_test.go`

- [ ] 先写child-delivery RED tests：1–10 task slug、PR number、source/merge commit、required run ID/conclusion、migration/adapter/spec digest必填；Git host merged state与`git merge-base --is-ancestor`双验证，旧run、非main base、非ancestor、duplicate/missing child全部失败。manifest不得含URL/账号/资产ID/credential。
- [ ] 从activation inventory写RED test，逐项要求records（含Markdown revision/conclusion）、attachments、evidence（含comparison result/copied lineage）、collaboration、search、activity、portability、legacy、source-object、processor/recovery的object kind+contract version唯一adapter；missing/duplicate/unknown version启动失败。Markdown/evidence renderer versions另过registry conformance，browser buffer只过client receipt/retention测试，不伪造standalone recovery adapter。
- [ ] 在`record_recovery_adapters.go`实现compile-time typed constructor manifest和canonical adapter-set digest；只调用owner package已经交付的constructor，禁止包级`init`、反射发现、文件名扫描、通用JSON/table清理fallback或task11自建领域adapter。任一owner合同缺失时停止并回到owner新PR，不在本任务补跨包删除。
- [ ] 为每个 adapter运行共同 conformance：幂等 replay、独立 verify、全局引用保护、unknown version拒绝、receipt无正文、reservation/identity epoch旧任务拒绝。
- [ ] 写 source VPS/MonitoringInstance/Target authorization-floor恢复 RED tests，证明 live route断开、identity snapshot按最终floor存续且权限不扩大。
- [ ] 更新 worker count/readiness tests，断言每个新worker显式命名和lifecycle；低于minimum contract或adapter digest不符的backend/worker不能通过LB/queue gate。
- [ ] 运行 `go test -race ./internal/center/recovery ./internal/center/recorddeletion ./cmd/houfeng-center -run 'Adapter|Replay|Bootstrap|MinimumFence' -count=10` GREEN。

## Task 3: Backup service、官方 CLI 与一致性 artifact

**Files:**

- Create: `internal/center/recovery/backup.go`
- Create: `internal/center/recovery/backup_service.go`
- Create: `internal/center/recovery/backup_runner.go`
- Create: `internal/center/recovery/postgres_artifact.go`
- Create: `internal/center/recovery/object_catalog.go`
- Create: `internal/center/recovery/backup_test.go`
- Create: `internal/center/recovery/backup_fault_test.go`
- Create: `cmd/houfeng-backup/main.go`
- Create: `cmd/houfeng-backup/main_test.go`
- Modify: `internal/center/store/recovery_projection.go`
- Modify: `internal/center/store/recovery_projection_test.go`
- Modify: `internal/center/config/config.go`
- Modify: `internal/center/config/config_test.go`

- [ ] 写 backup plan/state machine RED tests：配置/空间/trust/inventory/telemetry失败、deployment-wide lease与deletion reservation互斥、staging策略可证明排除、不可排除时byte前derived-source签名登记/expiry继承、retention过长时零字节拒绝、同key重放、取消、owner lease接管和每个partial先登记。
- [ ] 实现 `planned→leasing→snapshot_registered→copying→verifying→signing→published`，失败只进入 `purging→purged`；published artifact不可改写。
- [ ] 写PostgreSQL 16 transaction RED test：backup lease+短时write barrier、T1 marker/watermark/ref pins commit、T2 `REPEATABLE READ`+`pg_export_snapshot()`、同snapshot refs digest、`pg_dump --snapshot`。在marker前、T1 commit后/T2 export前、export后、catalog枚举和dump期间注入并发record write/deletion/GC，证明barrier窗口无穿透、export后新写不入artifact、pins无遗漏且结束时新ledger head不覆盖signed baseline。
- [ ] 实现固定绝对路径/参数数组的PostgreSQL artifact runner并显式传`--snapshot=<id>`；不调用shell，不把DSN/password/path/snapshot token写日志；T2保持到dump完成，取消/超时关闭子进程/transaction并登记partial receipt。
- [ ] 写local/S3共用catalog conformance：正式/历史双向引用、version/hash、缺失/多余/串包、增量对象、Object Lock/noncurrent version库存。
- [ ] 实现数据库artifact hash、Blob catalog digest、schema/config/policy/recoverable-until的canonical signed manifest发布；signature/manifest conditional put成功后才标记published。
- [ ] 先写 CLI RED tests，再实现 `plan/run/status/verify/inventory/cancel`、JSON schema与exit 0/2/3/4/5/6；危险取消要求operation ID + digest。
- [ ] 运行 `go test -race ./internal/center/recovery ./cmd/houfeng-backup -run 'Backup|Manifest|ObjectCatalog' -count=10` GREEN。

## Task 4: 恢复来源 normalizer、Restore service 与官方 CLI

**Files:**

- Create: `internal/center/recovery/source_normalizer.go`
- Create: `internal/center/recovery/source_full.go`
- Create: `internal/center/recovery/source_pitr.go`
- Create: `internal/center/recovery/source_snapshot.go`
- Create: `internal/center/recovery/restore.go`
- Create: `internal/center/recovery/restore_service.go`
- Create: `internal/center/recovery/restore_runner.go`
- Create: `internal/center/recovery/restore_test.go`
- Create: `internal/center/recovery/restore_fault_test.go`
- Create: `cmd/houfeng-restore/main.go`
- Create: `cmd/houfeng-restore/main_test.go`
- Modify: `internal/center/recovery/trust_store.go`
- Modify: `internal/center/recovery/workspace.go`

- [ ] 写source normalizer RED corpus：full DB、PITR base/WAL/timeline/LSN、atomic snapshot sidecar、S3 exact version；缺signature/binding/catalog/continuous WAL、manifest swap与“latest”引用全部拒绝。
- [ ] 实现统一 `RecoveryPointManifest + replay_baseline`；PITR恢复后较高DB水位只验合法前缀，不能上调signed base baseline。
- [ ] 写trust RED tests：fresh full-witness chain、retired key、compromised/missing/rolled-back head、recoverable-until与watermark篡改；实现fresh authoritative head验证。
- [ ] 写restore workspace RED tests：写第一字节前登记、isolated network/backup exclusion、不可排除时整个workspace byte前注册signed derived source/replay baseline、derived expiry继承、retention过长零字节拒绝、1h lease、24h/7d/source-expiry三上限、forensic审批不延寿、普通续租越界拒绝。
- [ ] 实现六步可重入编排和 `planned→preflight→materializing→replaying→rebuilding→final_sync→ready→transferred` 状态机；step receipt未写入不得前进。
- [ ] 写双head RED tests：开始后的fresh primary/full witness一致、baseline连续到head、final sync新尾部、旧deployment fence和ownership CAS；任一gap/rollback/unknown adapter保持未启动。
- [ ] 先写 CLI RED tests，再实现 `plan/run/status/renew/forensic/cancel/destroy/verify-ready/transfer`；无参数能跳过replay/final-sync/startup gate。
- [ ] 运行 `go test -race ./internal/center/recovery ./cmd/houfeng-restore -run 'Restore|SourceNormalizer|Trust|Workspace' -count=10` GREEN。

## Task 5: 删除重放、防复活与 projection rebuild

**Files:**

- Create: `internal/center/recovery/record_replay.go`
- Create: `internal/center/recovery/record_replay_test.go`
- Create: `internal/center/recovery/rebuild.go`
- Create: `internal/center/recovery/rebuild_test.go`
- Modify: `internal/center/recovery/adapter_set.go`
- Modify: `cmd/houfeng-center/record_recovery_adapters.go`
- Modify: `internal/center/store/record_cutover.go`
- Modify: `internal/center/http/router.go`
- Modify: `internal/center/http/router_api_test.go`

- [ ] 建立真实restored-store fixture，先写`delete_commit` orchestration RED tests：constructor manifest中的records（含Markdown revision/conclusion）、attachments、evidence（含comparison result）、collaboration、search、activity、portability、legacy、source-object、processor/recovery owner receipt全部被调用并verify；record/root/revisions/drafts/comments/actions/watch/notify、legacy、search/activity/cache、export、import、processor/restore workspace最终命中0，跨record合法payload/blob引用仍存续。Markdown renderer另过registry conformance、browser buffer过client receipt，不要求虚构standalone adapter。测试通过owner adapter观察结果，task11不直接删其表/对象。
- [ ] 写 `attempt_not_committed` RED tests，严格只恢复outcome/release epoch并解除reservation，不生成tombstone、不删内容、不占delete identity。
- [ ] 写 `contract_activation` RED tests，必须恢复genesis/inventory/minimum version/membership/readiness/queue gate；未知entry不能推进 `last_fully_applied_sequence/hash`。
- [ ] 实现严格sequence replay + per-adapter receipt + independent verify；全局水位只在当前及此前全部entry receipt完成后原子推进。
- [ ] rebuild coordinator严格按owner manifest调用search/activity/summary/cache recovery adapter，从清理后的权威事实重建；服务端export cache默认不恢复，legacy原行和import origin tombstone由各owner receipt逐项验证。coordinator不持有领域SQL或Blob key。
- [ ] 写旧import plan、旧reservation epoch、迟到processor/projector/cache warmer/outbox与旧binary RED tests，最终提交或外发命中为0。
- [ ] 写owner-adapter unit cutpoints覆盖content-delivery epoch和active export/import/processor/restore inventory，作为Task11真实HTTP/stream黑盒矩阵的前置；本任务只验证orchestration调用完整，不复制child领域purge实现。
- [ ] API contract验证records protected domain在ledger/witness/fence不可证明时统一503 reason code，无权/不存在仍统一404且不泄露计数/内容。
- [ ] 运行 `go test -race ./internal/center/recovery ./internal/center/recorddeletion ./internal/center/http -run 'Replay|Resurrection|Rebuild|ProtectedDomain' -count=10` GREEN。

## Task 6: Cross-domain janitor 与故障注入收敛

**Files:**

- Create: `internal/center/recovery/integration_janitor.go`
- Create: `internal/center/recovery/integration_janitor_test.go`
- Create: `internal/center/recovery/faultpoints.go`
- Create: `internal/center/recovery/faultpoints_test.go`
- Modify: `internal/center/recovery/janitor.go`
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`

- [ ] 写orphan matrix RED tests：DB attempt无lease、prefix/volume/multipart无attempt、receipt/location不符、expired normal/forensic、processor/import child receipt缺失、deletion reservation命中scope。
- [ ] 实现跨域inventory对账；unknown partial只触发fail-closed告警，未经owner/scope证明不自动删除。
- [ ] 每个location清理必须执行idempotent delete→verify-not-found/hash-list→receipt；只更新状态、断开引用或保持隔离不能标purged。
- [ ] 加backup marker、DB dump、Blob/multipart、sidecar、manifest/signature/publish与restore step2–6、receipt、final head、transfer faultpoint。
- [ ] 对每个faultpoint强杀owner、重启center/janitor并断言最终published/transferred或purged；悬空attempt、workspace、volume、prefix、multipart、pin数量均为0。
- [ ] 更新bootstrap worker count与lifecycle，运行 `go test -race ./internal/center/recovery ./internal/center/recordplatform ./cmd/houfeng-center -run 'Janitor|Faultpoint|WorkerLifecycle' -count=20` GREEN。

## Task 7: PostgreSQL 16 + MinIO + scanner/processor 真实集成栈

**Files:**

- Create: `test/integration/records/compose.yaml`
- Create: `test/integration/records/.env.example`
- Create: `test/integration/records/minio-init.sh`
- Create: `test/integration/records/run.sh`
- Create: `test/integration/records/assert-isolation.sh`
- Create: `test/integration/records/testdata/0050-schema.sql`
- Create: `test/integration/records/testdata/0050-schema.provenance.json`
- Create: `test/integration/records/generate-0050-schema.sh`
- Create: `test/integration/records/verify-0050-schema.sh`
- Create: `scripts/run-records-integration.sh`
- Create: `scripts/run-records-recovery-drill.sh`
- Modify: `Dockerfile`
- Modify: `compose.yaml`
- Modify: `internal/center/deploy/docker_static_test.go`
- Modify: `scripts/docker-entrypoint.sh`

- [ ] 先写static RED tests，要求app/ledger/witness/recovery-control四个PostgreSQL 16 service使用独立URL/credential/volume，应用backup/archive路径不能包含其他三个volume。
- [ ] 写0050 fixture provenance RED tests：生成器只接受验证后的不可变`v0.59.0` release tag/commit，hash 0001–0050真实migration，在clean PostgreSQL16运行该release app migrator后用固定`pg_dump --schema-only --no-owner --no-privileges`+canonicalizer输出schema/provenance；CI fresh regenerate必须byte-identical。fixture无provenance、手工编辑、migration/tag/hash漂移均失败，禁止手写schema。
- [ ] 实现测试overlay与MinIO setup，实际启用versioning/Object Lock/COMPLIANCE测试保留、full WORM entries和conditional head；local profile使用独立private durable volume。增加excluded、derived-source、retention-too-long三种真实策略并读回配置，验证derived registration发生在storage write counter之前。
- [ ] 加ClamAV与当前commit的content processor，健康失败时required archive upload保持quarantine/rejected；integration不得用in-process fake替代scanner/renderer。
- [ ] `run.sh` 先fresh运行`generate-0050-schema.sh`与`verify-0050-schema.sh`，再依次执行fresh install、generated 0050→0060 upgrade、repeat migrations、四库隔离、local/S3 Blob conformance、processor kill、E2E record saga、backup/delete/restore、清理对账。
- [ ] `assert-isolation.sh` 对应用backup、volume和manifest扫描ledger/witness/recovery-control schema/credential digest，期望命中0；不输出真实secret。
- [ ] 运行 `./scripts/run-records-integration.sh --profile local` 与 `./scripts/run-records-integration.sh --profile minio`，两者必须GREEN并自动清理容器/volume；失败时保留的诊断先过Task 8扫描。

## Task 8: Telemetry inventory、安全 corpus 与 artifact 防泄漏

**Files:**

- Create: `internal/center/recordsecurity/telemetry_inventory.go`
- Create: `internal/center/recordsecurity/telemetry_inventory_test.go`
- Create: `internal/center/recordsecurity/leakscan.go`
- Create: `internal/center/recordsecurity/leakscan_test.go`
- Create: `testdata/record-security/corpus.json`
- Create: `docs/deploy/records-telemetry-inventory.example.json`
- Create: `scripts/check-record-telemetry.sh`
- Create: `scripts/check-record-artifacts.sh`
- Modify: `internal/center/config/config.go`
- Modify: `internal/center/config/config_test.go`
- Modify: `cmd/houfeng-center/logging.go`
- Modify: `cmd/houfeng-center/logging_test.go`
- Modify: `internal/center/http/csp-policy.txt`

- [ ] 写inventory schema/canonical digest RED tests：owner/location/config hash/verification/≤30d TTL必填；unknown/unreadable/unbounded sink让permanent delete capability关闭。
- [ ] 实现runtime preflight、active sink枚举与配置漂移检测；operator-supplied `HOUFENG_TELEMETRY_INVENTORY_FILE` 必填并绑定每个live config digest。sink同时登记为recovery source时删除preview纳入副本窗口，但仍必须独立满足telemetry≤30天。
- [ ] 建立secret+中文/英文正文+stable ID corpus；先让它穿过HTTP/DB/object/worker/processor/browser/import/export/backup/restore全部成功和失败路径。
- [ ] 写RED scanner tests覆盖stdout/stderr、JSON log、PG/MinIO access、browser report、workspace/core、CI artifact staging；同时测试编码/截断/异常链/SDK breadcrumb变体。
- [ ] 收紧HTTP/logger/PG deployment配置、object key/route template、processor diagnostics和browser collector，直到全部sink/归档/日志备份/core dump命中为0。
- [ ] 用fake clock推进31天并调用每个sink的verification method，断言request/correlation/deletion-operation ID在online/archive/backup残留为0、长期只剩不可逆route/status/time-bucket聚合；无法验证的sink让capability保持off。
- [ ] 运行Markdown XSS、IDOR、SSRF、MIME/signature spoof、archive bomb/path/link、manifest swap/tamper、stale plan、download/export越权专项；未知active content或raw JSON renderer不能通过。
- [ ] 先用example只运行inventory schema/source test；正式执行要求环境提供非example的`HOUFENG_TELEMETRY_INVENTORY_FILE`，运行 `./scripts/check-record-telemetry.sh --inventory "$HOUFENG_TELEMETRY_INVENTORY_FILE" --verify-live --advance-days 31` 与 `./scripts/check-record-artifacts.sh web/test-results web/playwright-report web/test-results/playwright web/test-results/staging-audit test/integration/records/artifacts`，期望正文、secret、stable ID、raw resource path和过期operation ID命中均为0；不存在的目录安全跳过但已生成目录不得漏扫。

## Task 9: 固定 seed、负载基准与容量门

**Files:**

- Create: `internal/center/recordbench/seed.go`
- Create: `internal/center/recordbench/seed_test.go`
- Create: `internal/center/recordbench/load.go`
- Create: `internal/center/recordbench/report.go`
- Create: `internal/center/recordbench/report_test.go`
- Create: `cmd/houfeng-records-bench/main.go`
- Create: `cmd/houfeng-records-bench/main_test.go`
- Create: `scripts/run-records-benchmark.sh`
- Create: `docs/quality/vps-records/benchmark-environment.md`
- Create: `docs/quality/vps-records/capacity-budget.md`

- [ ] 写deterministic seed RED tests：固定PRNG/version/hash、10k current/200k revisions/1m activities、中英文/代码/归档/受限/来源删除/partial coverage、median 3 evidence/2 attachments和5MiB fixture。
- [ ] 实现 `seed` 与canonical manifest；重复生成的row counts/distribution/hash必须逐字一致，未知seed version拒绝。
- [ ] 写fixed-arrival load RED tests：overview20rps、search10、timeline10、draft5、revision2、comparison candidate/summary2、6×2,000 detail0.2、evidence0.2；uniform与80/20热点，预期409/404/429单列。comparison fixed selections来自immutable seed，不在负载中capture新证据。
- [ ] 实现5分钟warmup+15分钟measure、每profile三轮；overview/search/timeline成功样本≥5000，draft/revision/comparison summary≥1500，comparison detail/evidence≥150。任一单轮样本不足、非预期错误、p95或capacity门失败使报告失败，不能合并三轮掩盖。
- [ ] 实现环境/commit/schema/config/seed校验和p50/p95/p99、CPU/memory/IO/connections、SQL fingerprint/EXPLAIN、comparison admission wait/reject/active weight/5s drain及cgroup `memory.peak/events`报告；不保存query/content/stable object ID。
- [ ] 把design §13 capacity表编码成machine-readable verifier：comparison request≤96MiB/aggregate≤512MiB/queue≤16/wait≤2s/drain≤5s、application peak≤3GiB且oom/throttle增量0、queue oldest≤60s且≤10m drain、workspace/quota/partial/orphan阈值、reason code和命令逐项必填；不得以改配置抬高阈值让旧报告过线。
- [ ] 只在专用self-hosted runner label `houfeng-records-benchmark-x8664-v1`、登记的`records-benchmark-operator`和父硬件attestation齐全时，分别运行 `./scripts/run-records-benchmark.sh --profile local --rounds 3 --capacity` 与 `--profile minio --rounds 3 --capacity`；每轮达到overview≤750ms、search/timeline≤1s、draft≤500ms、revision≤1s、comparison summary≤1s/detail≤2s、evidence≤10s且非预期错误率0。
- [ ] mixed profile中并发保持comparison 512MiB aggregate饱和并取消，验证2s admission/429、5s worker+writer+token drain、≥1GiB non-comparison headroom和无OOM/throttle；复用task8 `./scripts/run-comparison-capacity.sh`的single/aggregate报告并要求同commit/config。再对Blob/evidence/attachment quota、outbox/projector、processor/import/restore workspace、backup partial、orphan/janitor逐项执行capacity表命令。

## Task 10: 六表面视觉、状态、可访问性与理解测试

**Files:**

- Create: `web/e2e/vps-records-visual-contract.spec.ts`
- Create: `web/e2e/vps-records-accessibility.spec.ts`
- Create: `web/e2e/vps-records-security-states.spec.ts`
- Create: `web/e2e/fixtures/vpsRecords.ts`
- Create: `web/e2e/support/vpsRecordsContract.ts`
- Modify: `web/e2e/fixtures/contracts.ts`
- Modify: `web/e2e/fixtures/profiles.ts`
- Modify: `web/e2e/fixtures/router.ts`
- Modify: `web/e2e/visual-contracts.spec.ts`
- Modify: `web/e2e/accessibility.spec.ts`
- Modify: `web/e2e/page-states.spec.ts`
- Create: `docs/quality/vps-records/comprehension-test-protocol.md`
- Create: `scripts/validate-vps-records-comprehension.mjs`

- [ ] 为六surface×适用state写fixture contract RED tests；fixture只在test build/router可用，production bundle/route命中为0。
- [ ] 三个新spec的每个`test.describe`/test title必须真实包含`@vps-records`，并加source contract test证明grep至少枚举预期六surface/desktop/390矩阵；禁止仅在注释或文件名声称tag。
- [ ] 先写VPS stable DOM/geometry RED test：异常标题/动作/disabled button/container/hidden reserved height命中0；anomaly只在实际fact存在时插入identity与summary之间，恢复后移除且不抢focus。
- [ ] 固定desktop与390px六表面的DOM、状态、几何、overflow与diagnostics合同；加载/空/无结果/局部失败/processing/revoke/deleted/degraded使用结构与行为断言，不能用绿色或0条skeleton冒充加载完成。
- [ ] 覆盖静止click signifier、文字+形状+颜色状态、Axe critical/serious=0、完整键盘、roving menu、Escape、focus restore、BroadcastChannel revoke、background pageshow遮蔽、reduced motion。
- [ ] 使用现有geometry helper断言全部触摸目标≥44px；390px无document横向overflow，比较矩阵仅允许带accessible name的局部scroll和sticky row header。
- [ ] 激活Node22后显式运行 `npm --prefix web run test:e2e -- e2e/vps-records-visual-contract.spec.ts e2e/vps-records-accessibility.spec.ts e2e/vps-records-security-states.spec.ts --grep '@vps-records'`，先验证list/实际test count非0且覆盖六surface，再fresh重跑确保稳定；不得增加tracked screenshot snapshot、screenshot manifest或批量raster文件。
- [ ] 按父设计招募至少20名目标参与者，执行稳定/异常×desktop/390反平衡30秒理解测试；参与者逐一声明未参与本项目需求规划、视觉/技术设计、产品或代码实现、代码审查、测试/fixture实现、Trellis/Codex规划复核且未预知答案。把匿名CSV交给validator，逐类校验排除声明，并要求新手≥10、其余低频、合格参与者≥20、成功≥18/20，任何排除有明确reason；Playwright/staging browser cue结果不得写入参与者行或成功数。
- [ ] 将同commit的语义/几何合同、Axe、keyboard/focus/44px和匿名理解摘要生成gate receipt；本地人工截图只作脱敏短期评审证据，不得提交真实账号、资产、屏幕录像、敏感截图或像素golden。

## Task 11: 恢复演练与全故障矩阵

**Files:**

- Create: `test/integration/records/recovery-scenarios.json`
- Create: `test/integration/records/recovery_test.go`
- Create: `test/integration/records/deletion_delivery_test.go`
- Create: `test/integration/records/terminal_retention_test.go`
- Create: `docs/operations/records/backup.md`
- Create: `docs/operations/records/restore.md`
- Create: `docs/operations/records/deletion-replay.md`
- Create: `docs/operations/records/disaster-recovery-drill.md`
- Create: `docs/operations/records/forensic-workspace.md`
- Modify: `scripts/run-records-recovery-drill.sh`

- [ ] 为local与MinIO逐项运行：正常backup/restore、backup后delete再restore、PITR/base baseline、snapshot sidecar、backup staging/restore workspace的excluded与derived-source分支、retention-too-long零字节拒绝、source expiry边界、ordinary/forensic renew。
- [ ] 分别运行backup→永久删除VPS/MonitoringInstance/Target→restore；断言source row/route复活0、按名称重连0、identity tombstone/final authorization floor一致，合法记录/证据仍可读且未扩大权限。
- [ ] 运行backup manifest key与RecoveryTrustStore两阶段rotation、旧active/retired key manifest恢复、compromised key拒绝、primary trust store+checkpoint同时丢失后从postgres-sync/S3-WORM full entries重建完整chain。
- [ ] 用fake clock到期并物理销毁full/PITR/WAL/snapshot/local/S3 noncurrent/Object-Lock各介质，逐项产生无内容destruction receipt；仍被inventory引用、legal hold或策略未到期的介质不得提前标销毁。
- [ ] 运行ledger中段缺失、尾部截断、primary rollback、witness stale/mismatch、manifest swap、signature/watermark/recoverable-until篡改、unknown entry/object/version、adapter receipt失败、final second-sync失败；错误启动放行数必须0。
- [ ] 对Task 6全部backup/restore cutpoint强杀并重启janitor；未发布partial、非forensic失败workspace、volume/prefix/multipart/pin残留数必须0。
- [ ] `deletion_delivery_test.go`通过真实HTTP在JSON/attachment/evidence/preview/download/export的headers前后、首/末chunk注入reservation，并并发active import/processor/restore workspace；断言ledger append前stream/socket已停并有receipt，可能外发时preview stale/披露，未披露发送与误判drain均为0。
- [ ] `terminal_retention_test.go`以fake clock越过24h和30d窗口，扫描应用PG、Blob、projection、workspace、delivery/job/receipt与telemetry；除ledger/witness、最小audit、origin tombstone、current recovery inventory/trust/deployment allowlist外正文和stable record/object ID命中为0，`attempt_not_committed`不形成tombstone。
- [ ] 验证恢复后正式/历史Blob hash 100%、search/activity从权威重建、legacy/import origin tombstone、外部delivery取消和minimum contract gate。
- [ ] runbook记录RTO测量、`deletion-ledger/full-witness RPO=0`与“应用数据RPO取决于实测backup/PITR策略窗口”的不同边界、操作者/审批、无内容artifact digest、失败处置与取证限制；不记录源内容或secret。
- [ ] 执行 `./scripts/run-records-recovery-drill.sh --profile local --all` 和 `--profile minio --all`，两个报告必须来自当前commit并通过schema verifier。

## Task 12: CI、部署配置与静态运行门

**Files:**

- Modify: `.github/workflows/ci.yml`
- Create: `.github/workflows/records-recovery-drill.yml`
- Modify: `.github/workflows/frontend-staging-smoke.yml`
- Modify: `.github/workflows/publish-images.yml`
- Modify: `docs/deploy/compose.env.example`
- Modify: `docs/deploy/local-and-systemd.md`
- Modify: `docs/deploy/systemd/houfeng-center.service`
- Modify: `.env.example`
- Modify: `Makefile`
- Modify: `scripts/verify.sh`
- Modify: `internal/center/deploy/docker_static_test.go`
- Create: `scripts/smoke-records-release-image.sh`
- Create: `test/integration/records/release-image-smoke.sh`

- [ ] 先写workflow/static RED tests：Node22、PG16四库、MinIO/Object Lock、ClamAV/processor、独立volume/credential、records integration/visual job、现有`go/web/web-browser/docker-image`终态required context依赖图、artifact leakscan和timeout/cleanup。
- [ ] CI增加内部`records-integration`与`records-visual`，并保留/构造同名`go/web/web-browser/docker-image`终态聚合job覆盖它们；使用workflow source test与GitHub branch-protection API/设置记录验证这些context确为required且dependency failure/skip不能汇总成功。若需要远端策略变更，交给repository owner显式执行并在PR接收前复核，不静默绕过。
- [ ] records-visual job显式执行三个路径 `web/e2e/vps-records-visual-contract.spec.ts`、`vps-records-accessibility.spec.ts`、`vps-records-security-states.spec.ts`并`--grep '@vps-records'`；source/list test要求实际title tag和非零六surface矩阵，禁止空grep绿色。
- [ ] fault全矩阵workflow可复用/手动/定期运行，但最终cutover必须引用同commit、同release image digest的成功run。
- [ ] Make/verify增加确定性入口：focused unit、真实integration、recovery drill、visual/accessibility、telemetry和benchmark report verify；日常快速门与重型门命名清楚。
- [ ] Compose/systemd文档固定backup/restore binary、PG工具、key file 0400、workspace owner/mode、tmpfs/no-core/swap policy、telemetry inventory和四库/MinIO/processor健康门。
- [ ] Docker image包含`houfeng-center`、`houfeng-content-processor`、`houfeng-backup`、`houfeng-restore`、`houfeng-records-rollout`五个正式binary，不包含benchmark seed、测试credential/keyring或Playwright artifact；固定non-root UID/GID，比较intent等0400 keyring只读mount必须是regular file并以no-follow语义验证，适用命令以read-only rootfs运行。
- [ ] `smoke-records-release-image.sh --image <candidate-digest>`用隔离四库/Blob/secret fixtures逐个执行：center health/version+all flags off、processor安全fixture self-test、backup plan、restore plan（写byte=0）、rollout status/preflight off；五者均核对同一commit/version/digest、non-root/read-only与无secret日志。任何一个缺失/只`--help`/错误binary都让docker-image gate失败。
- [ ] 失败artifact先运行leakscan；扫描范围包含`web/playwright-report`、`web/test-results/playwright`、`web/test-results/staging-audit`和integration/recovery输出。正文、stable ID或raw resource path命中时不上传原文件，只输出reason/digest；验证GitHub workflow artifact retention符合≤30天内容域上限。
- [ ] fresh运行 `make verify-go`、Node22 `make verify-web`、上述三个显式`@vps-records` specs与完整`npm --prefix web run test:e2e`、两个integration profile、两个restore profile、0050 fixture regenerate diff、candidate image五binary smoke、Docker build与`git diff --check`。

## Task 13: 最终质量、受保护 PR/Release、五 binary smoke 与 flags-off 部署

- [ ] 执行 `trellis-check`：spec compliance、lint/type/test、跨层数据流、child-delivery ancestry、constructor adapter set、迁移编号、URL/权限/删除/恢复/视觉/容量合同和上下文漂移；修复后从干净状态重跑Task12全部门并重新生成同commit receipts。
- [ ] 更新 `.trellis/spec/` 中实际形成的backup/restore、adapter、cutover、worker、CI、视觉、capacity和telemetry可执行合同；文档不得把测试配置冒充生产安全配置。
- [ ] `git diff --check`、secret/content scan、工作树审查通过；只stage本任务和必要spec/product文件，不包含`.tmp/`、credential/keyring、真实截图、benchmark大数据或敏感artifact。
- [ ] 创建非main PR并监控required checks至全部通过；同一分支修复失败后重跑受影响门和全量Task12，PR receipt记录head SHA、check run IDs与artifact digests。
- [ ] PR合并后验证merge commit并监控main CI；Release Please PR核对0060/compat/recovery notes，按受保护流程合并并监控release和publish-images。`git merge-base --is-ancestor <merge_commit> <release-commit>`必须返回0。
- [ ] 使用`docker buildx imagetools inspect`验证发布tag架构与精确digest；拉取该digest并运行`smoke-records-release-image.sh`，分别通过center/content-processor/backup/restore/rollout五binary smoke，核对同一release commit/version/digest、non-root/read-only和flags-off行为。
- [ ] 只把该已验证精确digest部署到staging，所有records capability/feature flags与cutover phase保持`off`；从运行实例和部署控制面双向核对commit、release version、image digest、config digest与all-flags-off并写deployment receipt。此时不得运行rollout preflight或shadow。
- [ ] 上述任一步需要代码/配置/image修复时，必须从新的非main分支重新经过PR required checks→merge→main CI→Release Please/release→image publish/smoke→flags-off deploy；禁止现场热补、branch image或沿用旧receipt。

## Task 14: 已发布 digest 的 Staging preflight、切换、回退、soak 与完成

**Files:**

- Create: `docs/operations/records/cutover.md`
- Create: `docs/operations/records/rollback.md`
- Create: `scripts/verify-records-cutover.sh`
- Modify: `web/e2e/staging/audit.ts`
- Modify: `web/e2e/staging/staging-smoke.spec.ts`
- Modify: `web/playwright.staging.config.ts`
- Modify: `internal/center/recordrollout/service_test.go`
- Modify: `cmd/houfeng-records-rollout/main_test.go`

- [ ] 在已验证all-flags-off deployment上首次运行rollout `preflight`，证明migration、child/adapter/recovery、telemetry、安全、性能/capacity、visual/accessibility、人类理解、backup inventory和rollback receipt全部绑定当前release commit/version/image/config；任何receipt早于Task13部署或来自branch image均拒绝。
- [ ] `off→shadow_read`：对0.59.0以来legacy/VPS/monitoring/IP/subscription/command路径做无内容差异对照；唯一写仍是legacy，差异数/原因逐项收敛为0。
- [ ] 在staging重复运行0059 dry-run/apply/reconcile，要求row/revision/identity/origin/hash差异0；ledger/tombstone命中的旧行迁移数0。
- [ ] `shadow_read→records_write_legacy_read`：一个CAS切断legacy写并启用records写；立即验证旧POST被稳定拒绝、新记录/修订/材料/证据/协作可用，数据库不存在双写差异。
- [ ] 验证VPS稳定/异常、记录中心、时间线、比较、编辑器、证据选择器在真实数据下的desktop/390、长文、partial coverage、source archive/delete、permission revoke、processor/ledger局部降级。
- [ ] `records_write_legacy_read→records_read_default`：开放sidebar与新VPS概览；staging smoke只执行显式browser cue、Axe、keyboard/focus/44px和脱敏diagnostics，不声称或计入30秒人类理解测试（该receipt来自Task10独立参与者）。
- [ ] 完整回退演练：route/read回到fence-aware compatibility，legacy写保持关闭，new records仍为唯一写权威，删除fence/adapter/minimum version不降低；再向前恢复。
- [ ] soak期内监控无内容SLO、queue/janitor/inventory/witness/trust/comparison admission与全部capacity预算；全部fresh gate通过后进入`records_default_verified`，仍保留legacy表/mapping/tombstone。
- [ ] 任一staging缺陷停止phase前进；需要修改代码/配置/image时回到Task13完整受保护链并重新以flags off部署，旧preflight/cutover/soak receipts全部失效。
- [ ] 只有`records_default_verified`、回退演练、soak与post-cutover artifact leakscan均通过，才把本子任务标记完成并交父任务执行11-child跨层终验；“PR已开”“已合并”“已发布”或“staging smoke已过”均不是完成。

## Rollback and hard stops

- 0060及此前migration均additive；任何回退不执行down migration、不删除数据、不修改ledger/witness/trust history。
- activation前可关闭全部records capability；activation后任何binary必须满足minimum fence-contract并包含完整adapter set，否则不接流量/队列。
- records写启用前可回到`off`；启用后legacy写永不重新开启，只允许回退到fence-aware compatibility read。
- ledger/witness/trust/replay/fresh-head任一不可证明时是硬停止，不以只读旧cache、跳过unknown entry或人工SQL放行。
- local、MinIO、processor、telemetry、benchmark、六表面视觉/可访问性、staging回退或发布链任一required gate失败，本任务保持未完成；PR合并前可在同一feature分支forward fix，合并/发布/部署后必须新建非main修复分支并重新走完整受保护PR→release→flags-off deploy链。
- `experience_logs`、legacy mapping/tombstone和旧replay adapter只有在所有相关恢复介质到期销毁、兼容矩阵通过且用户另行批准的新任务中才可移除。

## Plan self-review

- Requirement coverage：backup/restore CLI、跨存储、六步恢复、删除不复活、workspace janitor、真实PG/MinIO/processor、security/telemetry、benchmark、六表面视觉/390px/a11y、staging/cutover/rollback和发布链均有唯一任务与验收命令。
- Dependency consistency：只使用子任务1–10的稳定adapter；0058/0059不在本任务重建，0060不删除legacy。
- Failure consistency：所有unknown/gap/stale/mismatch进入fail closed；所有partial在写第一字节前登记并以publish/transfer或purge receipt收敛。
- Toolchain consistency：正式Web证据只接受Node22；当前Node24不能被误记为GREEN。
- Delivery consistency：Task13先完成PR checks→merge→main CI→Release Please/release image→五binary smoke→all-flags-off精确digest部署与核对，Task14才preflight/shadow/cutover/rollback/soak；任何后续修复从受保护链重来。
- Authorization consistency：本文完成仍不启动任务，执行继续等待用户单独批准。
