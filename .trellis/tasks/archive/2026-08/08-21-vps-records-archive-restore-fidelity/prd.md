# 记录归档恢复保真度

## Goal

在 Child 10 收窄后的官方 ZIP / 文档 apply 之上，补齐「导出再导入后比较、附件、隔离 PDF 仍可用」的保真度。不重做 Child 10 的 admission、tombstone、origin 冲突或扫描合同。

## Background

Child 10（2026-08-21 收窄）交付：

- 具名 AdmissionGate、witness、`0058`、Markdown、比较 Export 消费
- ZIP64 含 document.md + 已知 `Kind.Export` 证据 JSON
- apply 把文档 + origin + job 终态放进同一事务
- 未知 schema 失败关闭；二次导入 dry-run/apply 409
- 派生 PDF 仍是进程内 stub；证据只校验不写 `evidence_snapshots`；附件不进 ZIP

父任务已放弃：ZIP 内 activity 页（导入后重建）；quarantine 落库（未知即拒）。

Child 11 只跑集成验证，不实现本 child 的域能力。

## Requirements

- `P-EVD-01` Apply 在与 `ImportDocumentsFinishing` 同一笔 `RunRecordPlatformTransaction` 内写入本机 registry 已知的 `evidence_snapshots`（经 `EvidencePreparation` / 现有 participant，不另开证据写入通道）。失败整笔回滚，job 保持 `planned` 可重试。
- `P-ATT-01` 官方 archive 纳入当前修订已授权附件字节（走 Child 3 BlobStore / 附件读 API）。体积仍受 archive 上限约束；超限在 preview 点名，不静默丢弃。
- `P-ATT-02` Apply 把附件对象恢复到本机 BlobStore 并绑回导入记录，仍走附件准入，不信任 archive 内的 MIME/path。
- `P-PDF-01` 生产 PDF 走 `houfeng-content-processor`，`ValidateIsolation` + 禁网；空二进制不得再当生产默认。进程内 `WriteDerivedPDF` 只留测试。
- `P-RND-01` 带 `EvidenceSnapshotIDs` 的官方 ZIP 往返后，比较工作台能读回同一 `Kind.Export` 字节（至少 comparison.result + 一种非比较 known kind，例如 `monitoring.probe/v2`）。

## Acceptance Criteria

- [x] Known evidence JSON in an official archive becomes `evidence_snapshots` rows in the same apply transaction as documents/origin/job.
- [x] A second apply of the same plan is idempotent; origin conflict / tombstone still fail before writes.
- [x] Authorized attachment bytes round-trip through ZIP and BlobStore; unauthorized attachments are named, not invented.
- [x] Production bootstrap PDF renderer invokes the isolated processor; unit tests still cover RenderModel parity.
- [x] Official archive fixture with `EvidenceSnapshotIDs` includes a non-comparison known kind (`monitoring.probe/v2` or equivalent) and applies it.
- [x] Child 10 contracts remain: no `/records/compare` download, no second comparison exporter, unknown schema fail-closed, no quarantine rows, no activity pages in ZIP.
- [x] Focused Go/Web tests + `make verify-go` / `make verify-web`. Postgres/MinIO runs stay Child 11.

## Out of Scope

- Child 10 authority / `0058` / origin tombstone / import scan (already shipped).
- ZIP activity pages; persisted quarantine.
- Asset JSON import; `experience_logs`; `0059`.
- Child 11 backup/restore CLI, adapter registry enablement, permanent-delete flag.
- 4 GiB comparison harness.

## Execution Gate

Child 10 is archived on protected main (`9e910d7c`, release `v0.72.0` /
`3c239fa0`). Seams reconciled: `ImportDocumentsFinishing` does not yet
carry `EvidencePreparation`; `knownKindEvidenceImporter` validates only;
`ArchiveClassAttachment` exists but `fillArchive` does not put bytes;
production bootstrap still calls `NewIsolatedDocumentPDFRenderer("")`.
