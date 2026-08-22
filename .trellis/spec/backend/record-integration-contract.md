# Record 集成、备份与恢复合同

> **项目依据**：以 `internal/center/recordreadiness/`、`internal/center/recordbackup/`、`internal/center/recordrestore/`、`cmd/houfeng-backup/`、`cmd/houfeng-restore/`、`cmd/houfeng-center/bootstrap.go` 的 `newProductionRecordReadinessRegistry` 和 `scripts/run-records-*.sh` 为准。Child 11 是集成 owner，不实现域 purge / recovery SQL，不新增 root migration。

---

## 1. Scope / Trigger

修改下列任一位置时必须加载本文件：

- 能力登记、永久删除开关、content-safe 状态矩阵
- 官方备份 / 恢复编排、typed manifest、local / S3 ArtifactStore
- `houfeng-backup` / `houfeng-restore` CLI
- `scripts/run-records-integration.sh`、`run-records-recovery.sh`、`run-records-security.sh`、`run-records-capacity.sh`、`run-records-browser.sh`
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
- 脚本：`--profile local|s3`；Docker 缺失 fail-closed；任何 `--- SKIP:` 即失败。recovery `--all` 不重跑已知 Alpine portability-deletion 种子缺陷。capacity 默认只跑单元；`--profile` 才用 `HOUFENG_ACTIVITY_PERF_SCALE=0.001`（最少 1000 行）。browser 用 Node 22 + `$HOME/.cache/ms-playwright`（若存在）并扫描 `web/dist`。

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

---

## 5. Good / Base / Bad Cases

- Good：完整健康 fixture 才 `permanent_delete=enabled`；生产当前必须 disabled 并列出缺失行。
- Good：Create 只在全部 artifact 发布后写 manifest；Apply 失败永不 ready。
- Base：生产 readiness 有部分 deletion/recovery 和成对 authority，没有 backup/restore 编排对。
- Bad：为让 PD 变绿而发明 markdown/comparison adapter，或单边接线 backup。
- Bad：把失败集成标成 skip，或把 `AdmissionGateFunc` / allow-all 带进生产。

---

## 6. Tests Required

- `internal/center/recordreadiness/registry_test.go`：20 kind、成对规则、PD 决策、Encode 无泄漏。
- `security_corpus_test.go` / `acceptance_corpus_test.go`：语料清单在磁盘上存在；脚本 ratchet。
- `recordbackup` / `recordrestore` 服务与 CLI source ratchet：`plan`/`create`/`verify` 与 `plan`/`apply`/`verify`。
- `scripts/run-records-security.sh`、`run-records-capacity.sh`、`run-records-browser.sh`、integration / recovery profiles。
- 断言点：PD disabled 原因精确；复活被拦；外部副本无内容；`web/dist` 无 `dashboardTestFixtures` / `coreRouteProfile`。

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
