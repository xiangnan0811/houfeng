# Record 集成、备份与恢复合同

> **项目依据**：以 `internal/center/recordreadiness/`、`internal/center/recordbackup/`、`internal/center/recordrestore/`、`cmd/houfeng-backup/`、`cmd/houfeng-restore/`、`cmd/houfeng-center/bootstrap.go` 的 `newProductionRecordReadinessRegistry` 和 `scripts/run-records-*.sh` 为准。Child 11 是集成 owner，不实现域 purge / recovery SQL，不新增 root migration。

---

## 1. Scope / Trigger

修改下列任一位置时必须加载本文件：

- 能力登记、永久删除开关、content-safe 状态矩阵
- 官方备份 / 恢复编排、typed manifest、local / S3 ArtifactStore
- `houfeng-backup` / `houfeng-restore` CLI
- `scripts/run-records-integration.sh`、`run-records-recovery.sh`、`test-record-platform-integration.sh`、`test-records-s3-lifecycle.sh`、`scripts/lib/records-runner-lifecycle.sh`、`run-records-security.sh`、`run-records-capacity.sh`、`run-records-browser.sh`
- 生产 bootstrap 把 deletion / recovery / authority 装进 readiness registry

不在本切片：`record_markdown_client` / `record_comparison` 删除适配器、`0059`/`0060`、ZIP activity 页、把 `houfeng-record-platform-admin` 改成备份 CLI、原地升级 / 混版本恢复、staging / Release Please。

---

## 2. Signatures

```go
func recordreadiness.NewRegistry(recordreadiness.RegistryInput) (*recordreadiness.Registry, error)
func (recordreadiness.StatusMatrix) Encode() ([]byte, error)
func recordreadiness.RequiredCapabilityKinds() []recordreadiness.CapabilityKind
func recordreadiness.RequiredSecurityCorpusTests() []string
func recordreadiness.ScanContentSafe([]byte) error
func recordreadiness.ScanProductionBundleSafe([]byte) error

func recordbackup.NewService(recordbackup.Options) (*recordbackup.Service, error)
func (*recordbackup.Service) Plan(context.Context, recordbackup.Request) (recordbackup.Plan, error)
func (*recordbackup.Service) Create(context.Context, recordbackup.Request) (recordbackup.Manifest, recordbackup.CleanupReceipt, error)
func (*recordbackup.Service) Verify(context.Context, recordbackup.Manifest) error
func recordbackup.NewManifest(recordbackup.ManifestInput) (recordbackup.Manifest, error)
func (recordbackup.Manifest) Encode() ([]byte, error)
func recordbackup.DecodeManifest([]byte) (recordbackup.Manifest, error)
func recordbackup.NewLocalStore(absRoot string) (*recordbackup.LocalStore, error)
func recordbackup.NewS3Store(*minio.Client, bucket string) (*recordbackup.S3Store, error)
func recordbackup.NewProfileReport(recordbackup.ProfileReportInput) (recordbackup.ProfileReport, error)

func recordrestore.NewService(recordrestore.Options) (*recordrestore.Service, error)
func (*recordrestore.Service) Plan(context.Context, recordrestore.Request) (recordrestore.Plan, error)
func (*recordrestore.Service) Apply(context.Context, recordrestore.Request) (recordrestore.Result, recordrestore.CleanupReceipt, error)
func (*recordrestore.Service) Verify(context.Context, recordrestore.Request) error
func recordrestore.EncodeExternalCopies([]recorddeletion.SurvivingCopySummary) ([]byte, error)
```

CLI：

- `houfeng-backup plan|create|verify [--profile local|s3]`
- `houfeng-restore plan|apply|verify [--profile local|s3]`

集成脚本：

- `scripts/run-records-integration.sh --profile local|s3`
- `scripts/run-records-recovery.sh --profile local|s3 --all`
- `scripts/test-record-platform-integration.sh postgres|pg16-catalog -- <command> [args...]`
- `scripts/test-records-s3-lifecycle.sh`
- lifecycle helper：`records_runner_prepare_run_id`、`records_runner_verify_volume_ownership`、`records_runner_finish_evidence`、`records_runner_cleanup`

生产：`cmd/houfeng-center/bootstrap.go` 的 `newProductionRecordReadinessRegistry(...)`；HTTP 仍是 `handlers.RecordDeletions(nil)`。

---

## 3. Contracts

- 恰好 20 个 `CapabilityKind`：9 deletion + 7 recovery + membership/witness + backup/restore。`RequiredCapabilityKinds()` 是闭合清单。
- 构造允许不完整（空或只有 deletion）。authority 必须成对；backup 与 restore 必须成对。nil / typed-nil / 未知 / 重复 / 版本不兼容 → `ErrInvalidCapabilityRegistry`。
- `Evaluate`：缺行、不健康、authority 关闭 → `permanent_delete=disabled`。Encode 只允许 `kind` / `family` / `healthy` / `reason` / `version` / `permanent_delete`。禁止 `note`、Health 原文、凭据、URL、内容。
- 生产 registry 包装已有 deletion（core / attachments / evidence / search / activity / collaboration / portability）和已存在的 recovery（core / attachments / evidence / activity）。不接线 backup/restore 编排对。markdown / comparison deletion 保持 unnamed / `missing`。
- Manifest format `houfeng-record-backup/v1`。Create 先 stage database 再 objects，最后发布 manifest。未知 `min_reader_version` → `ErrUnknownManifestVersion`；篡改 completion / database digest → `ErrTamperedManifest`。失败 cutpoint 出 cleanup receipt，失败 Create 不发布 manifest。
- Artifact kind 闭合：`postgres_dump` / `record_attachments` / `record_evidence` / `record_portability` / `manifest`。
- Restore 顺序：空目标 → 校验 manifest/build/migration/ACL → stage → DB → objects → replay deletions → search rebuild → activity rebuild → APP ACL → verify → readiness。非空目标 → `ErrTargetNotEmpty`。不兼容 digest → `ErrIncompatibleRestore`。缺 artifact → `ErrMissingArtifact`。`PurgedKinds` 非空且目标在 replay 后仍有该 kind（或没有 `ArtifactPresence`）→ `ErrResurrectionBlocked`。重试必须换全新空目标。
- Profile report：`houfeng-record-profile-report/v1`，字段只有 format / profile / commit / config_digest / suites / permanent_delete / missing。
- `EncodeExternalCopies` 只输出 `scope` / `kind` / `copy_count`。
- Child 11 不加 root migration。`0059` 是后续 portability owner 修复：musl 安全的 `blob_key` CHECK，不改 `0058` 原文。
- CLI 必须调用 `recordbackup.NewService(` / `recordrestore.NewService(`，禁止 import `houfeng-record-platform-admin`。缺真实依赖时 fail-closed 为 `ErrBackupUnavailable` / `ErrRestoreUnavailable`。
- 脚本 profile：`--profile local|s3`；Docker、`setsid` 或支持 `--default-signal` 的 `env` 缺失时，在创建 workspace/Docker 资源前 fail-closed。recovery `--all` 不重跑已知 Alpine portability-deletion 种子缺陷。capacity 默认只跑单元；`--profile` 才用 `HOUFENG_ACTIVITY_PERF_SCALE=0.001`（最少 1000 行）。browser 用 Node 22 + `$HOME/.cache/ms-playwright`（若存在）并扫描 `web/dist`。
- Runner identity：`HOUFENG_RECORDS_RUN_ID` 只允许 1–80 个 `[A-Za-z0-9_.-]`；默认值来自随机 workspace basename。每个 workspace 另生成唯一 owner id。container 与 volume 必须同时带 `com.houfeng.records.runner`、`com.houfeng.records.run`、`com.houfeng.records.owner`。
- Resource ownership：candidate 在 create 前登记；S3 volume 使用 owner 派生的显式 name，create 后且 mount 前核验三 labels。cleanup 再次核验三 labels；container 只按 inspect 返回的 immutable ID 删除。name collision、ownership mismatch/inspect failure均 fail-closed，不删除或挂载外来资源。禁止 prefix cleanup、prune 和历史 residue 清理。
- Cleanup：三个 runner 共用 `scripts/lib/records-runner-lifecycle.sh`，顺序固定为 containers → volumes → workspace，并尝试完所有项目。body 非零 N 始终返回 N；body 为 0 且任一 cleanup 失败返回非零。`HOUFENG_RECORDS_KEEP_WORKSPACE=1` 仍删除 containers，只保留并打印 exact volume/workspace；required gate 必须 unset。
- Evidence：stdout/stderr sink 必须全部 wait。skip scan 的 grep 0 → skip failure，1 → clean，2+ → evidence failure；sink/scan 失败不得 false-green，body 原非零码仍优先。
- Signal：长跑 body 必须位于 runner-owned process group；INT/TERM 只转发给 owned group并有界等待嵌套 cleanup。cleanup/emergency cleanup 期间后续 INT/TERM 不得打断 Docker/filesystem teardown；最终保持 130/143。SIGKILL/daemon crash 仅由本次唯一 run label 的 real gate 提供精确恢复边界。
- Real lifecycle gate：为 integration/recovery 分配唯一 run labels 与 test-owned TMPDIR，运行前要求 label 为空；每条 S3 runner 后断言 container/volume/TMPDIR 零残留，并比较递归 root-owned `/tmp/houfeng-records-*` 快照。emergency cleanup 只处理已登记的 exact label/path，不能清理未知历史状态或把 residue assertion 变绿。local 两 profile 也必须通过。

---

## 4. Validation & Error Matrix

| 条件 | 结果 |
|---|---|
| 缺 markdown / comparison deletion，或 backup/restore 未成对接线 | `permanent_delete=disabled`，矩阵点名 `missing` |
| 生产 `RecordDeletions(nil)` | HTTP 永久删除保持关闭 |
| 非空 restore 目标 | `ErrTargetNotEmpty`，不 serving / 不 workers |
| `PurgedKinds` 仍可见 | `ErrResurrectionBlocked` |
| Encode / 脚本 / CLI 源含 `postgres://`、`password=secret`、`# title` 等 | `ErrContentLeak` |
| 生产 `web/src`（非测试）或 `web/dist` 含 e2e fixture token | `ErrContentLeak` / browser 脚本失败 |
| Alpine `0058` `blob_key` 512-bounded class repeat | `0059` 改为 `char_length` + 无上界 class repeat；不改 `0058` 原文 |
| 集成 / 恢复 / 安全 / 容量命令出现 `--- SKIP:` | 命令失败，不能当通过证据 |
| evidence sink 非零，或 skip scan 返回 2+ | body=0 时 gate 非零；body=N 时保留 N，并输出 evidence diagnostic |
| body=0，任一 container/volume/workspace teardown 失败 | 尝试完后续清理，最终非零 |
| body=N，teardown 同时失败 | 尝试完后续清理并报告 diagnostic，最终仍为 N |
| container/volume name collision、三 labels 不匹配或 inspect 失败 | 不挂载、不删除外来资源；runner fail-closed |
| INT/TERM 到达长跑 body | 只转发 owned process group；等待分层 cleanup 后返回 130/143 |

---

## 5. Good / Base / Bad Cases

- Good：完整健康 fixture 才 `permanent_delete=enabled`；生产当前必须 disabled 并列出缺失行。
- Good：Create 只在全部 artifact 发布后写 manifest；Apply 失败永不 ready。
- Good：S3 runner 在 create 前登记 owner-derived volume candidate，create 后核验三 labels，cleanup 按 immutable container ID 删除；real gate 返回后本次 label/TMPDIR 均为空。
- Base：生产 readiness 有部分 deletion/recovery 和成对 authority，没有 backup/restore 编排对。
- Base：local profile 没有 MinIO volume，但仍复用相同 body/evidence/signal/cleanup 状态机。
- Bad：为让 PD 变绿而发明 markdown/comparison adapter，或单边接线 backup。
- Bad：把失败集成标成 skip，或把 `AdmissionGateFunc` / allow-all 带进生产。
- Bad：仅凭预生成 name 执行 `docker rm`，匿名 create 成功后才登记 volume，吞掉 `tee`/grep/cleanup 状态，或用 prefix/prune 清理。

---

## 6. Tests Required

- `internal/center/recordreadiness/registry_test.go`：20 kind、成对规则、PD 决策、Encode 无泄漏。
- `security_corpus_test.go` / `acceptance_corpus_test.go`：语料清单在磁盘上存在；脚本 ratchet。
- `recordbackup` / `recordrestore` 服务与 CLI source ratchet：`plan`/`create`/`verify` 与 `plan`/`apply`/`verify`。
- `scripts/run-records-security.sh`、`run-records-capacity.sh`、`run-records-browser.sh`、integration / recovery profiles。
- 断言点：PD disabled 原因精确；复活被拦；外部副本无内容；`web/dist` 无 `dashboardTestFixtures` / `coreRouteProfile`。
- `internal/center/recordbackup/runner_lifecycle_test.go` 必须通过 fake toolchain 执行 integration/recovery local+S3 与 direct child 真实 entrypoint，覆盖 body/cleanup precedence、继续清理、evidence sink/scan、partial create、foreign/replaced ownership、keep 和 signal/watchdog matrix。
- `scripts/test-records-s3-lifecycle.sh` 必须真实运行两条 S3 profile，断言唯一 run label 下 container/volume 为零、test-owned TMPDIR 为空、递归 root-owned 快照不变；另跑两条 local profile。测试只能操作本次 label/path。

---

## 7. Wrong vs Correct

```go
// Wrong: 生产打开永久删除，即使 markdown/comparison 缺失。
handlers.RecordDeletions(liveRegistry)

// Correct: 矩阵未全绿前保持关闭。
handlers.RecordDeletions(nil)
```

```go
// Wrong: 只接线 backup。
input.Backup = orchestration

// Correct: backup 与 restore 同时有或同时无。
```

```bash
# Wrong: name 被登记就直接授权删除；cleanup 状态被吞掉。
containers+=("$name")
docker run --name "$name" ...
docker rm -f "$name" || true

# Correct: candidate 可在 create 前登记，但删除前必须核验本次三 labels；
# container 使用 inspect 返回的 immutable ID，body/cleanup 状态由共享 helper 仲裁。
containers+=("$name")
docker run --name "$name" \
  --label "com.houfeng.records.runner=$records_runner_kind" \
  --label "com.houfeng.records.run=$records_run_id" \
  --label "com.houfeng.records.owner=$records_owner_id" ...
records_runner_cleanup "$body_status"
```
