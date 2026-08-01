# Blob、附件、配额与扫描 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans`; Codex inline only; RED/GREEN discipline is mandatory.

**Goal:** 交付持久、安全、可迁移且受记录权限/删除 fence 约束的附件与 Blob 平台。

**Architecture:** logical attachment/store metadata 在 PostgreSQL，immutable bytes 在 local/S3 BlobStore；独立 processor异步扫描/渲染；revision/deletion/backup 通过注册 adapter 接入。

**Tech Stack:** Go/pgx、MinIO SDK、ClamAV INSTREAM、Poppler、local filesystem/S3、React upload UI。

---

## Preconditions

- [ ] 子任务 1/2 已合入 main；确认 0053 migration可用，重新运行 baseline/hook/spec。
- [ ] 明确 Node22 与 local/MinIO test profile；禁止使用当前 Node24 生成正式 lock/build证据。

## Task 1: Schema、状态机与配额

**Files:** Create `0053_create_record_attachments.sql`; `internal/center/attachments/{types,validate,quota}.go`; store `attachments.go`; tests.

- [ ] 先写 migration/domain RED tests覆盖状态转换、hash/size、logical/physical quota、revision ref/pin/receipt constraints。
- [ ] 实现 schema 与纯状态机；非法回退/available前引用/重复逻辑计费稳定拒绝。
- [ ] 真实 PG repeated migration 与并发 reserve/complete quota tests GREEN。

## Task 2: BlobStore local/S3 conformance

**Files:** Create `blob.go`,`local_blob.go`,`s3_blob.go`,`blob_conformance_test.go`; modify `go.mod/go.sum` if Task1尚未引入 MinIO。

- [ ] 写同一 conformance suite RED：conditional put、digest mismatch、range、version、idempotent delete、partial cleanup。
- [ ] local实现 temp+fsync+atomic rename+private modes；S3实现 multipart/presign/copy-to-digest与version/hash verify。
- [ ] 在 temp dir与真实 MinIO运行 suite GREEN；未知 version/Object Lock策略 fail closed。

## Task 3: Upload/complete/download handlers

**Files:** Create `attachments/service.go`; handlers `attachments.go`; tests; modify router/bootstrap/app timeout相关测试。

- [ ] handler RED matrix覆盖 local/S3 session、quota、hash/MIME、complete幂等、404/409/413/422/503、range/download headers。
- [ ] 实现 API；长扫描不在30s HTTP内完成，complete只入队。
- [ ] 加 stream lease/revoke tests，reservation后新bytes/header为0。

## Task 4: Admission、scanner 与独立 processor

**Files:** Create `admission.go`,`archive.go`,`scanner_clamav.go`,`preview.go`,`workspace.go`; `cmd/houfeng-content-processor/*`; tests.

- [ ] 用 hostile corpus写 RED：signature spoof、zip-slip/duplicate/symlink/encrypted/bomb、malware、huge text/PDF/image复杂度。
- [ ] 实现 allowlist/stream bounds、ClamAV窄client、固定参数renderer与 workspace lifecycle；不使用 shell。
- [ ] 强杀每个 cutpoint，janitor后parts/workspace/cache残留为0且 receipt存在。

## Task 5: Revision/deletion/backup adapters

**Files:** Create `revision_adapter.go`,`deletion_adapter.go`,`backup_adapter.go`; modify records participant/recorddeletion registry wiring/tests.

- [ ] RED tests固定历史引用、跨记录dedupe、transaction rollback、permanent purge无宽限、restore version/hash。
- [ ] 实现 adapters；删除只在全局无引用且pin为0时物理删。
- [ ] 真实 PG+MinIO concurrency tests GREEN。

## Task 6: Web材料组件

**Files:** Create `RecordAttachments.tsx`, `AttachmentUploadQueue.tsx`, `AuthorizedDownloadLink.tsx` + tests; extend `web/src/lib/recordsApi.ts` and canonical DTOs in `web/src/lib/types.ts`.

- [ ] RED UI tests覆盖全部状态、重试/取消/移除、草稿不丢、无权/撤权、390px drawer contract。
- [ ] 实现受控组件，API仅在 route controller，object URL在完成/撤权/unmount清理。
- [ ] focused Vitest、lint/build/bundle/CSS GREEN。

## Task 7: 配置、部署与完整门

**Files:** Modify config/bootstrap/app、Dockerfile/compose/systemd/deploy docs/static tests.

- [ ] RED config/static tests要求持久 `houfeng_data`、processor tmpfs/read-only/non-root/core=0、S3/scanner健康与容量。
- [ ] 实现 deploy profile；Compose local profile声明只做conformance，不冒充生产独立恢复域。
- [ ] 跑 local+MinIO+processor integration、race、`make verify-go`、Node22 `make verify-web`、Docker build、`trellis-check`。

## Rollback

- feature off停止新upload；已available bytes继续只读，不能删数据或down migration。
- processor故障只让新复杂材料隔离，已有安全附件仍按授权可读。
