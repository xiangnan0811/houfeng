# 证据注册表与首批适配器 Implementation Plan

> **For agentic workers:** Use the native Trellis `trellis-implement` / `trellis-check` workflow (`codex.dispatch_mode: auto`). Every dispatch prompt must begin with `Active task: .trellis/tasks/07-14-vps-records-evidence-platform`. Each bounded slice follows RED -> verified RED -> minimal GREEN -> verified GREEN; do not use the retired Codex inline workflow.

**Goal:** 将现有IP质量、监控、事件、订阅预算和命令审计事实固化为可验证、可长期比较的不可变证据快照。

**Architecture:** registry统一kind合同；adapter从权威store读取；capture intent防漂移；logical snapshot/payload分离；Web只渲染allowlisted DTO。

**Tech Stack:** Go/pgx/PostgreSQL、stdlib gzip/SHA、React TypeScript/SVG MetricChart。

---

## 2026-08-02 execution override

- 子任务 1/2 必须已在 protected main 完成。
- `0054` 同时交付 current APP ACL managed surface/privileges/admission tests。
- 只验证 fresh/repeat；返回不含 `0054` 的代码版本时重建开发数据库。

## Preconditions

- [x] 子任务1/2/3已归档并合入当前main；`0051`/`0052`/`0053` 基线和 `0054` 可用性见 `research/preconditions-current-main.md`。
- [x] 已读取 IP/成本专项、backend/web 规范和共享 thinking guides；权威 source/store/API 路径见 `research/authoritative-sources-current-main.md`。
- [x] baseline 已覆盖当前 sparkline/IP/cost/event/command API；已记录现有 row-count/zero-fill、raw retention 和 metadata-only 边界，见 `research/authoritative-sources-current-main.md`。

## Task 1: Registry、envelope、canonicalization

**Files:** Create `internal/center/evidence/{types,registry,canonical,redaction,conformance}.go` + tests.

- [x] RED tests定义Kind接口、四类时间、quality/sensitivity、unknown version与禁止字段。
- [x] 实现确定性encoding/hash、schema field allowlist和registry startup validation。
- [x] 加fuzz/golden tests；`go test ./internal/center/evidence -run 'Registry|Canonical|Redaction' -count=1` GREEN。

Task 1 evidence (2026-08-11): `go test ./internal/center/evidence -count=1`、`go test -race ./internal/center/evidence -count=1`、`go vet ./internal/center/evidence`、`gofmt -d`、`git diff --check` 和五个 focused fuzz target 通过。native spec review 与 fresh native code-quality review 均无 Critical/Important findings；persistence/copy/delete/permission/quarantine/import-export round-trip 明确保留给后续 task。

## Task 2: 0054 schema/store与capture intent

**Files:** Create migration; `store/evidence.go`; `evidence/{service,capture}.go`; tests.

- [x] migration RED断言logical/payload/ref/intent/lineage/TTL/unique，不允许source cascade。
- [x] Reopened Task 2A：先用跨层RED证明Go只接受`evi_<24 lowercase hex>`而SQL仍接受`eci_[a-z0-9]{1,64}`；确认RED只因grammar漂移失败后，将`0054` intent约束和migration assertion对齐到唯一`evi_`合同并重跑全部migration/ACL/real PG gate。
- [x] Task 2B RED：定义server-owned prepared capture/reference、显式revision commit输入、有序snapshot IDs进入revision canonical hash/fingerprint，以及禁止context/singleton传递的测试合同。
- [x] Task 2B phase 1：事务外重新授权与重捕获，逐项比较persisted preview，content-addressed payload put后产生immutable prepared capture；existing snapshot只重新授权并复用，不重捕获。
- [x] Task 2B phase 2：revision participant在同一`pgx.Tx`中以`DELETE ... RETURNING`消费未过期intent，插logical snapshot和ordered revision refs；任一步失败必须恢复intent且不留下logical rows，payload允许成为24h orphan。
- [x] Task 2B lifecycle primitives：实现expired intent和24h unreferenced payload GC repository primitive；Task 7后续负责worker scheduling、metrics、capacity和alerts。
- [ ] Production capture/save在真实`AdmissionGate`未由Child 10接线前稳定失败关闭；禁止permissive fallback。real PG tests覆盖intent expiry、preview drift、rollback、replay/double consume、orphan grace/reclaim和existing snapshot reuse；GREEN。

Task 2A evidence (2026-08-11, reopened; closed 2026-08-12): `0054_create_record_evidence.sql` 定义7张owned table、15分钟row-presence intent、payload/snapshot/revision/lineage/receipt不变量和第三段current APP ACL（7个managed object、21条精确privilege、无APP `UPDATE`）。native `trellis-check` 修复gzip小payload开销边界、snapshot registry envelope与payload hash/size绑定、lineage/receipt约束及6个repeat-safe immutable trigger后无剩余Critical/Important findings。Task 2B设计审查随后发现跨层intent identity漂移：`evidence.validEvidenceID`只接受`evi_`加24位小写hex，而`0054`及migration test接受`eci_[a-z0-9]{1,64}`。RED命令 `go test ./internal/center/store/migrate -run '^TestRecordEvidenceMigrationIntentIDMatchesEvidenceContract$' -count=1` 精确失败为SQL `^eci_[a-z0-9]{1,64}$` 对Go `^evi_[0-9a-f]{24}$`；minimal GREEN将SQL和旧断言对齐。fresh `trellis-check` 随后修复一个Important：原测试source-grep且lowercase SQL可能假绿，现通过公开`evidence.VerifyKindConformance`行为式验证Go合同并从raw SQL提取唯一regex；复审后无剩余Critical/Important findings。主会话最终独立复验focused test、`go test ./internal/center/store/migrate -count=1`、`go test ./internal/center/evidence -count=1`、`go vet ./internal/center/store/migrate`、受影响文件`gofmt -d`、`task.py validate`、`git diff --check HEAD`及真实PostgreSQL `TestPostgresIntegrationAppACLCurrent`均通过。Task 2A重新闭环；capture/store、drift/rollback/orphan/copy auth/delete行为仍属于Task 2B及后续slice。

Task 2B prepared-capture slice evidence (2026-08-12): 先以RED证明缺少server-owned immutable `PreparedCapture` / `PrepareCapture`，再实现domain-only准备值、完整defensive copy、统一的preview/capture逐字段比较，以及masked/forbidden capture disposition规范化；没有引入context value、singleton、store、records transaction或持久化。fresh `trellis-check` 发现并修复一个Important：新capture曾接受canonical tombstoned authorization；现由`PrepareCapture`和adapter conformance共享的authorization normalizer精确要求live source，并以两个行为测试锁定，同时显式证明wall-clock intent expiry留给后续store原子消费。复审后无剩余Critical/Important findings。主会话独立复验focused/full evidence tests、`go test -race ./internal/center/evidence -count=1`、`go test ./... -count=1`、`go vet ./...`、受影响文件`gofmt -d`、`task.py validate`及`git diff --check HEAD`全部通过。payload/intent persistence、existing snapshot prepared reference、revision fingerprint/participant、rollback和orphan GC仍未完成，因此不勾选Task 2B整体RED或phase 1条目。

Task 2B store/payload lifecycle slice evidence (2026-08-14): 实现Admission-gated capture-intent persistence、deterministic `canonical_json_gzip_v1` content-addressed payload persistence、bounded expired-intent cleanup、PostgreSQL时间驱动的24小时全局无引用payload GC及原子content-free receipt。fresh `trellis-check` 修复四个Important/test-gap：store边界重算既有domain-separated canonical digest、拒绝PostgreSQL `timestamptz`无法精确保留的亚微秒时间、在admission/SQL前限制selection/preview JSON、将preview digest唯一冲突稳定映射为`ErrEvidencePersistenceConflict`并保留PG cause；真实PG新增冲突payload replay、24小时边界和receipt插入失败整事务回滚。`FOR UPDATE SKIP LOCKED`因需要有意未授予的APP `UPDATE` privilege而撤销，原子`DELETE`、二次全局引用检查与restrictive FK保持并发正确性且没有扩大ACL。复审无剩余Critical/Important findings。主会话独立复验focused evidence/store tests、focused race、严格真实PG `TestPostgresIntegrationEvidenceIntentPayloadAndOrphanLifecycle`、`go test ./... -count=1`、`go vet ./...`、`gofmt -d`、`task.py validate`及`git diff --check HEAD`全部通过。仅勾选本lifecycle primitive；existing-snapshot reuse、revision hash/fingerprint/participant、intent原子消费与logical rollback仍未完成，因此Task 2B aggregate RED、phase 1、phase 2及production end-to-end gate保持未勾选。

Task 2B existing-reference/revision-identity slice evidence (2026-08-14): 实现existing snapshot事务外重新授权与immutable prepared reference、显式`RevisionPreparation`传输、ordered `evs_...` IDs的closed grammar/去重/defensive copy，以及ordered IDs进入revision canonical hash与idempotency request fingerprint；空evidence继续保持既有v1 canonical hash。revision service、command、store commit和participant context只接收server-owned preparation，create/revise/restore application入口显式透传，restore必须以与历史revision完全相同顺序的prepared IDs失败关闭，客户端owned IDs继续被拒绝。fresh `trellis-check` 修复两个Important：evidence-bearing revision read原先未加载`record_revision_evidence`而无法重建canonical input；application create/revise/restore原先未携带preparation并可能在restore时静默丢失evidence。现按ordinal读取并校验连续性/ID grammar，新增真实PostgreSQL commit-participant/read round-trip、malformed persisted ordinal/ID、missing restore preparation和literal legacy hash回归；复审无剩余Critical/Important findings。首次RED的测试fixture类型错误被修正后未重新捕获clean RED，作为过程证据保留而非代码缺陷。主会话独立复验`make verify-go`、`go test ./... -count=1`、`go test -race ./internal/center/evidence ./internal/center/records ./internal/center/store -count=1`、严格真实PG `TestPostgresIntegrationRecordReadRoundTripsOrderedEvidenceSnapshotIDs`、`go mod verify`、受影响目录`gofmt -d`、`task.py validate`及`git diff --check HEAD`全部通过。生产revision participant中的intent原子消费、logical snapshot/ordered refs写入、rollback/replay和AdmissionGate接线仍属于后续slice，因此Task 2B aggregate RED、phase 1、phase 2及production end-to-end gate保持未勾选。

Task 2B revision-participant slice evidence (2026-08-14): RED先证明缺少production `NewRecordEvidenceRevisionParticipant`、participant未拒绝PostgreSQL无法精确保留的snapshot时间、intent persistence未绑定server-owned snapshot ID；GREEN在caller-owned `pgx.Tx`内以`DELETE ... RETURNING`原子消费live intent，逐项核对record/kind/schema、preview/source digest、selection、完整preview、snapshot ID/size/time，插入immutable logical snapshot，验证existing prepared reference identity/auth/payload digest，并按canonical ordinal插revision refs。later participant failure整事务恢复intent且不留revision/snapshot/ref，事务外payload保留为可回收orphan；missing/expired/persisted drift/replay/same-transaction double-consume全部失败关闭。首轮规格审查无Critical/Important；第二轮质量审查发现一个Important：intent repository只校验顶层TTL时间，nested window/observed时间可带亚微秒且selection/preview JSON可保留非UTC offset，形成可持久化但participant无法消费的状态。新增focused RED精确失败后，repository在value copy上统一全部intent/preview时间为UTC，并在admission/SQL前拒绝所有PostgreSQL-compared亚微秒时间；真实PG新增`TestPostgresIntegrationRecordEvidenceRevisionParticipantNormalizesOffsetIntentBeforeConsumption`证明offset intent经production repository后可由canonical prepared capture消费。fresh复审无剩余Critical/Important findings。主会话最终独立复验`make verify-go`、`go test ./... -count=1`、`go test -race ./internal/center/evidence ./internal/center/records ./internal/center/store -count=1`、`go mod verify`、全部改动Go文件`gofmt -d`、`task.py validate`、`git diff --check HEAD`，以及严格真实PG participant全矩阵、`TestPostgresIntegrationEvidenceIntentPayloadAndOrphanLifecycle`和`TestPostgresIntegrationAppACLCurrent`均通过且无`SKIP`。Task 2B phase 2现已闭环；事务外production重新授权/重捕获编排与Child 10真实AdmissionGate/bootstrap接线仍未完成，因此aggregate RED、phase 1与production end-to-end gate保持未勾选。

Task 2B production-preparation slice evidence (2026-08-14): RED定义`CaptureIntentBindingSource` / `CapturePayloadSink` / `RevisionPreparer`事务外编排边界；GREEN通过admitted PostgreSQL transaction加载完整live persisted intent/preview binding并在返回前commit，随后才执行selection重验、source重新授权、重捕获、全量preview-bound drift校验和content-addressed payload持久化，最后构造immutable `PreparedCapture`；existing snapshot只重新授权并复用，不调用preview/capture或写payload，mixed request以不可变`RevisionPreparation`保留原始顺序。首轮规格审查发现一个Important：`prepareCapture`在校验前clone会静默规范化custom binding source返回的offset/monotonic时间。新增36-case RED矩阵后，domain boundary现在clone前对12个PostgreSQL-compared时间统一要求非零、exact UTC、无monotonic且微秒对齐，并使用raw exact selection/lifetime comparison；回归同时断言只发生binding load，adapter和payload调用均为0。fresh规格复审与独立代码质量检查均无剩余Critical/Important findings。主会话复验focused evidence/store tests、两包race、`go vet ./internal/center/evidence ./internal/center/store`、严格真实PG `TestPostgresIntegrationEvidenceIntentPayloadAndOrphanLifecycle`、`make verify-go`、`go test ./... -count=1`、三包race、`go mod verify`、全部改动Go文件`gofmt -d`、`task.py validate`及`git diff --check HEAD`均通过。Task 2B aggregate RED与phase 1现已闭环；Child 10真实AdmissionGate/bootstrap接线尚未实现，因此production capture/save end-to-end gate保持未勾选。

## Task 3: IP与监控时序adapter

**Files:** Create `evidence/adapters/ip_quality.go`,`monitoring.go`; modify/add专用store query tests.

- [x] RED fixture证明现有sparkline 0-fill/截断不可接受，adapter必须返回actual coverage/buckets/sample counts/gaps。
- [x] 实现绝对窗口raw/aggregate query、精度/点数上限、IP stale policy与sensitive topology。
- [x] 30d/partial/backfill/maintenance/retention边界真实PG GREEN。

Task 3 evidence (2026-08-14): native `trellis-implement` 先以RED固定zero-fill/truncation、IP failure/failed-row facts、daily retention和probe空指标边界，再实现host/probe绝对窗口raw+daily aggregate query、实际coverage/precision、样本/维护/补传/缺口/有界峰值，以及requested window内最新assigned success/partial IP report和显式stale/sensitive-topology策略。native规格审查直接修复多个Important：adapter clock/window与PostgreSQL微秒精度漂移、默认精度越过2000桶时静默粗化、首尾缺口/重叠bucket、父设计要求的host磁盘/inode/network/IO与probe HTTP/TLS指标、producer version确定性union、IP version/error code/coverage风险语义；随后指出generic hash-only `Summarize`/`Compare`仍为Important。bounded follow-up先以RED证明read model无版本，再在`design.md`定义并实现IP/host/probe版本化safe read model和comparison DTO：同kind/schema/calculation、单位、窗口时长和精度兼容性失败关闭；IP返回status/stale/risk与coverage变化，监控按`series_id + metric`做sample-weighted average、实际min/max和mean-bucket-p95 delta且不补零。第二阶段代码质量复核又补上custom source IP地址/枚举和全部persisted timestamp微秒校验；无剩余Critical/Important。主会话独立复验focused adapter/store、affected race/vet、严格无SKIP真实PostgreSQL `TestPostgresIntegrationEvidenceSources`、`make verify-go`、`go test ./... -count=1`、`go mod verify`、全部改动Go文件`gofmt -d`、`task.py validate`及`git diff --check HEAD`均通过。final post-review 再修复四个 Important：custom source provider/service/bucket/metric 顺序曾直接影响canonical hash，IP闭合枚举、coverage分类与实际rows及future receipt未完全失败关闭，monitoring observed/watermark chronology与per-series gap展开缺少硬上限，以及comparison未比较完整units语义且遗漏stale policy/peak-count输出；同时让read model先验证完整snapshot。新增focused RED均精确命中后GREEN。最终复验adapter/store focused与full package tests、focused/full affected race、affected vet、严格无SKIP真实PostgreSQL `TestPostgresIntegrationEvidenceSources`、`make verify-go`、`go mod verify`、受影响Go文件`gofmt -d`、`task.py validate`和`git diff --check HEAD`均通过；Task 4/API/bootstrap/worker/AdmissionGate及故意未勾选的Task 2B production gate均未改动。

## Task 4: event、cost、asset-history、command adapter

**Files:** Create `events.go`,`subscription_costs.go`,`asset_history.go`,`command_audits.go` + tests; add Task 4 authoritative source queries and the shared typed monitoring-event payload builder; update incident、monitoring-instance binding/lifecycle/runtime、target runtime producers and occurrence-time consumers. `asset_history.go` is a source/activity adapter only; it must not register an unplanned `asset.history/*` kind. Registry keys remain the parent §12.2 set: `ip_quality.report/v1`, `monitoring.host/v1`, `monitoring.probe/v2`, `monitoring.event/v2`, `subscription.cost/v1`, `command.audit/v1`.

- [x] RED tests固定event/backfill/correction、rate/date/base currency、history event time、command metadata-only。
- [x] 实现adapter和summary/export DTO；hostile stdout/stderr/details/raw URL corpus命中0。
- [x] focused source package回归与adapter conformance GREEN。

Task 4 evidence (2026-08-16): native `trellis-implement` 以 RED 固定 event/backfill/correction、rate/date/base currency、四类 asset history event time 与 command metadata-only 合同，随后实现 `monitoring.event/v2`、`subscription.cost/v1`、`command.audit/v1` adapter及versioned summary/compare/export DTO，并保持 `asset_history_source/v1` 为非registry source adapter。多轮独立规格/质量审查的RED→GREEN修复包括：correction delta与event metric unit drift；全部custom/store timestamp canonicality；预算币种、全局spend、继承、zero/exact-limit与rate chronology；asset全局cap early-stop和preflight bounds；command action/actor/event-source identity及scheme-relative URL、userinfo、query、JSON、secret/output hostile corpus；PostgreSQL partial metadata与raw timestamp失败关闭。审查还证明原实现的真实producer不可达：普通`state_change_events` writer不保存Task 4 metadata，而真实PG fixture用direct enriched SQL形成false-green。后续将incident、monitoring-instance binding/lifecycle/runtime、target runtime五类writer统一接入typed builder和共享闭合validator，动态prior state在同一statement内锁定，dashboard按`event_at`解释occurrence并只对legacy回退；真实PG测试改为调用公开writer并逐类经source读取。最终质量审查继续修复multi-series recovery的backfill provenance、lifecycle/archive跨域状态、窗口内incomplete row静默消失及驱动规范化掩盖非canonical JSON timestamp。主会话独立复验`make verify-go`、`go test ./... -count=1`、五包affected race、`go vet ./...`、`go mod verify`、全部改动Go文件`gofmt -d`、`task.py validate`、`git diff --check HEAD`均通过；严格Docker-backed真实PostgreSQL `TestPostgresIntegrationEvidenceSources` PASS且无`SKIP`，覆盖正常incident、backfilled correction、binding、lifecycle、MI runtime与target runtime writer-through-reader。剩余仅两个非阻断观察项：dashboard occurrence表达式暂无functional index，correction link暂无DB foreign key；均不改变Task 4正确性，留待真实query plan/全局引用模型需要时处理。Task 5/API/bootstrap、Task 2B production AdmissionGate及Child 4后续任务未推进。

## Task 5: API/router/bootstrap与deletion/export adapters

**Files:** Create handlers `evidence.go`; create `internal/center/evidence/{deletion_adapter,recovery_adapter,export_adapter}.go` and colocated tests; modify router/bootstrap.

- [x] handler RED matrix覆盖preview/read、unknown kind、source unstable、preview stale、permission intersection、response allowlist。
- [x] 实现 `/api/evidence/capture-previews`、`GET /api/evidence/:id`和records save hook。
- [x] deletion清logical refs/owned snapshots并保留其他copy；export只调用kind.Export。
- [x] `evidence.NewRecoveryAdapter`重放logical snapshot/payload/intent/source floor与`comparison.result/*` kind，基于恢复后全局引用GC；unknown kind/version失败关闭而非通用JSON。

Task 5 evidence (2026-08-16): native `trellis-implement` 先以RED固定preview/read、unknown kind/version、source unstable、preview stale、record+source授权交集、strict response allowlist，以及create/revise/restore有序evidence preparation；GREEN新增Evidence service/handler、router/bootstrap稳定失败关闭接线、Records save hook与revision response snapshot IDs，并复用既有`RevisionPreparer`和transaction participant。新增closed `record_evidence` deletion surfaces、schema-aware export和deterministic recovery inventory/replay；删除只清record-owned refs/intents/logical snapshots并在全局无引用时清payload，显式copy及lineage存活；export只调用registered `kind.Export`并在runtime复核forbidden corpus；恢复重放canonical gzip payload、logical snapshot、intent、revision ref、source authorization floor和lineage，仅接受registry显式注册kind，`comparison.result/*`不按prefix放行，完成后做全局引用GC。独立`trellis-check`以真实PostgreSQL RED修复删除receipt与recovery replay不可重试、恢复inventory orphan payload、浅拷贝TOCTOU、offset/亚微秒时间规范化、Records非空items被错误preparer静默丢弃、read DTO parity及runtime versioned read-model/export allowlist；同时将PG fixture改为production revision participant和公开RecoveryAdapter路径，重试相同输入幂等、分歧输入失败关闭。主会话最终独立复验严格Docker-backed Task 5 deletion/recovery与完整record-evidence participant矩阵全部PASS且无`SKIP`，`make verify-go`、`go test ./... -count=1`、七包affected race、`go vet ./...`、`go mod verify`、全部改动Go文件`gofmt -d`、`git diff --check 425a758df86c4d138ba80d376e66d10274ff28ae`和`task.py validate`均通过。Child 10真实AdmissionGate/source resolver/read/reference production composition未提前接线，nil/typed-nil production capture/save继续稳定503；Task 2B production gate保持未勾选，Task 6/7未推进。

## Task 6: Web selector与renderer registry

**Files:** Create `pages/records/evidence/EvidenceRendererRegistry.tsx`, kind renderers, `EvidenceCapturePicker.tsx` + tests; extend lazy `web/src/lib/recordsApi.ts` and canonical DTOs in `web/src/lib/types.ts`.

- [x] RED tests覆盖selector顺序、preview fields/stale、sensitive explicit choice、权威unknown schema fail-closed且payload/metadata不进入普通UI、趋势缺口不连线；external quarantine fallback不在本任务实现。
- [x] 实现allowlisted renderers并复用MetricChart；禁止`JSON.stringify(payload)` fallback。
- [x] Vitest/lint/build/bundle/CSS GREEN。

Task 6 evidence (2026-08-16): native `trellis-implement`新增纯注入、未挂载route的ordered capture picker、lazy-only records API DTO/transport，以及六个exact `(kind, schema, renderer, read_model.version)` renderer registry；native `trellis-check`以RED补齐preview source/window parity、稳定clock与async abort/reset、深层bounded fail-closed decoder、hostile nested fields、精确UTC/枚举/数值/chronology语义、provider identity去重、monitoring跨gap/缺bucket分段与单点gap可见性，普通路径无arbitrary JSON renderer/export/fallback。Node 22 focused Task 6/architecture/bundle/CSS矩阵`9 files / 94 tests`通过；完整`make verify-web`以clean install通过ESLint、`131 files / 939 tests`、coverage、TypeScript/Vite build、bundle（entry JS gzip `110734 <= 110738`，CSS `37125 <= 37125`，max async `31904 <= 32052`）和CSS analyze/budget。Task 6仅为前端数据/展示路径，不需要PostgreSQL；Task 7与Child 10未推进。

## Task 7: 容量、janitor与完整门

- [ ] 实现evidence独立capacity/alerts、intent/payload orphan janitor与metrics；不受附件quota阻断。
- [ ] 运行determinism/fuzz、race、真实PG、`make verify-go`、Node22 `make verify-web`、`trellis-check`。
- [ ] 更新IP质量/数据库/Web state spec，提交PR/CI；feature仍off。

## Rollback

- 关闭 evidence capability 不删除已捕获快照，不执行 down migration；返回不含 `0054` 的代码版本时重建开发数据库。
- adapter不稳定时只禁用对应kind；权威unknown contract使evidence protected capability失败关闭，外部unsupported metadata只由task10 quarantine拥有，任何普通路径都无通用渲染。
