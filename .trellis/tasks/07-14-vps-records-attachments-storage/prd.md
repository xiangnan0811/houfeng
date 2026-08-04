# Blob、附件、配额与扫描

## Goal

实现可由 Records Core 正式修订事务引用的逻辑附件与内容寻址 Blob 平台，覆盖 local/S3 存储、上传隔离、准入扫描、安全预览、授权下载、配额、回收、永久删除和备份恢复接缝。

## 2026-08-04 Execution Rebaseline

- 直接依赖 Child 1、Child 2 已合入 protected `main`；本分支基于 Child 2 closeout 后的 `db8bca69`。
- 本任务拥有 `0053_create_record_attachments.sql` 及其 current APP ACL fragment，只支持 fresh/current development database 与 exact repeat，不提供旧数据库 upgrade、dual write 或 release cutover。
- 使用一个 worktree、一个分支和一个 PR，按三个强制检查点推进。每个检查点先完成 focused RED/GREEN、复核范围和报告剩余风险，再进入下一检查点；发现范围膨胀时先重新规划，不自动扩展。
- 2026-08-04 用户批准此检查点方案；规划材料自审完成并显式执行 Trellis start 后，任务进入 `in_progress`。

## Requirements

### 1. 数据身份与存储

- 逻辑 attachment 与物理 content-addressed Blob 分离。相同字节可共享 Blob；权限、配额、引用、审计、删除和显示名均按逻辑 attachment 独立。
- `record_attachments` 任一时刻满足 `record_id XOR draft_id`。新上传先归作者草稿；正式保存事务才把选中的 available attachment 原子转移到 record 并建立不可变 revision refs。
- Blob key 只由 SHA-256 digest 派生，不含原始或安全文件名。Blob 内容不可变，logical attachment 不可就地替换。
- local 与 S3/MinIO backend 实现同一 `BlobStore` contract，并运行同一 conformance suite：conditional create、hash/size、range、version mismatch、幂等删除与 partial cleanup。
- local 使用持久卷、私有权限、同目录临时文件、fsync、原子 rename 和目录 fsync；容器可写层不是 Blob backend。
- S3 使用私有 bucket 和随机 upload temporary key；应用完成上传后服务端重新读取并验证字节，再发布 digest final key。用户下载始终经过 center 鉴权与 deletion fence，不签发可绕过后续撤权的 S3 URL。

### 2. 上传、准入与处理

- API 路径固定为：
  - `POST /api/attachment-uploads`
  - `PUT /api/attachment-uploads/:id/content`
  - `POST /api/attachment-uploads/:id/complete`
  - `GET /api/attachments/:id`
  - `GET /api/attachments/:id/content`
- 上传状态机为 `created -> uploading -> quarantined -> available | rejected | expired`；扫描执行状态由独立 processor job 表达。非法回退、终态重开和并发 complete 必须稳定拒绝或幂等返回同一结果。
- 服务端流式计算 SHA-256，并验证声明大小、实际大小、magic/MIME、扩展名、文件名、图片像素、PDF 复杂度、文本编码与压缩结构。
- 可安全预览类型为 PNG/JPEG/WebP、PDF，以及父设计列出的 UTF-8 text/Markdown/log/JSON/YAML/CSV/TSV/INI/TOML/patch/diff；文本内联预览最多 5 MiB。
- ZIP/TAR/GZIP/Zstandard 只允许授权下载，必须先通过结构检查和 required scanner。scanner 未配置或不健康时，新的 archive upload 在接收字节前返回稳定 unavailable；已经接收但 scanner 暂不可用的对象保持 quarantined、有界重试并最终 expired/rejected。
- executable、script package、HTML、SVG、macro document、disk image、加密包、嵌套/解压炸弹、签名或扩展不符、malware 全部 fail closed。
- 复杂扫描和 preview 在独立 `houfeng-content-processor` 运行：数据库先登记 job/workspace，再写隔离 workspace；禁止 shell、外网、core dump、共享 profile/cache。成功、失败、取消、超时和 crash 后由幂等 janitor 清理并写 receipt。

### 3. 配额、引用与回收

- 默认限制以父任务为唯一来源：50 MiB/文件、500 MiB/记录或草稿暂定记录、10 GiB/项目、80% 预警、5 MiB 文本内联预览；配置值必须保持正数、层级一致且不会溢出计量类型。
- 配额按 logical attachment 原始字节计费，dedupe 不降低 record/project logical usage。项目同时可读取 logical usage 与 physical deduplicated usage。
- upload reservation 在接收字节前建立；complete、reject、expire 和取消按状态释放或固化用量。配额耗尽只阻止新增材料，不阻止文字修订、移除引用或只复用已有 record attachment 的修订。
- 任一历史 revision、正式 record、活动 draft、未完成事务、backup/restore/import plan 或显式 GC pin 仍引用 Blob 时不得物理回收。
- 普通 orphan 宽限 24 小时；draft attachment 服从草稿生命周期。永久删除独占 Blob 不使用普通宽限，但必须先证明全局引用与 pin 均为零。

### 4. Records Core 集成

- attachment IDs 是 `CompleteRevisionValues` / normalized `CompleteRevisionInput` 的显式、规范化、有序字段，进入 revision canonical hash、idempotency payload、HTTP request/response、draft payload 和 revision read/restore round trip。
- attachment ID 数组在 Go/JSON/TypeScript 边界始终输出 `[]` 而不是 `null`；重复、非法 ID 或顺序漂移必须可测试。
- `RevisionParticipant` 在同一 PostgreSQL transaction 中验证 attachment ownership、状态、授权、quota 与 deletion reservation，转移本次 draft-owned attachments，并写 `record_revision_attachments`。外部 Blob/processor 调用不得进入修订事务。
- 修订事务失败不得产生可见引用或半转移；移除 current 引用不得破坏历史 revision；restore 旧 revision 必须恢复当时 attachment ID 列表。
- Child 3 实现并测试 closed registry 名称 `record_attachments` 的 deletion adapter。由于后续 adapters 尚未齐全，production permanent-deletion transport 继续 fail closed，不在本任务提前打开。

### 5. 授权、下载与接缝

- 草稿 attachment 只允许作者访问；record attachment 使用统一 `recordauth.Policy` 结果，并与当前 source authorization、record visibility、attachment ownership 和 deletion fence 取交集。
- preview/download 每次重新鉴权并取得短 content stream lease。permission revoke 或 deletion reservation 后禁止新 stream/presign/complete，已开始 stream 必须被取消并不再发送后续 bytes。
- 响应使用安全展示名、allowlisted `Content-Type`、`Content-Disposition`、`nosniff` 与受限 CSP；未知状态或处理合同失败关闭。
- Child 3 提供 Blob inventory、exact version/hash enumeration、pin、purge 和 restore verification 接口；Child 11 负责全局 `RecoveryPointManifest`、跨存储复制、删除 replay 与真实恢复控制器。
- Web 只提供 lazy records API、DTO、上传 queue/controller、状态展示和 authorized download primitives；不得在 page/component 直接 `fetch`。完整编辑器、材料侧栏与 390px drawer integration 属于 Child 5。

## Acceptance Criteria

### Checkpoint 1: schema/domain/Blob foundation

- [x] `0053` fresh/repeat migration、current APP ACL source contract、catalog convergence 和 runtime admission 通过。
- [x] 状态转换、logical/physical quota、reservation、revision ref、pin 与 receipt constraints 有纯领域和真实 PostgreSQL tests。
- [x] local 与真实 MinIO 对同一 BlobStore suite 的 conditional put、dedupe、range、hash、version mismatch、delete 和 partial cleanup 结果一致。

### Checkpoint 2: secure upload and content data plane

- [ ] 五个 attachment endpoints 覆盖 local/S3 transport、配额、幂等 complete、状态和稳定 400/404/409/413/422/503 错误。
- [ ] MIME spoof、超限、zip-slip、duplicate path、symlink/hardlink、炸弹、加密包、主动内容和 malware 全部 fail closed。
- [ ] scanner unavailable 不会误标 rejected/available；重试、超时、expired 和 crash 后 parts/workspace/cache 残留为 0 且有 receipt。
- [ ] record/draft authorization、range/preview/download headers、stream lease、revoke 和 deletion reservation tests 证明 fence 后不再发送新 bytes。

### Checkpoint 3: Records integration and delivery seams

- [ ] attachment IDs 在 draft -> HTTP -> normalized input -> canonical hash -> revision refs -> read/restore -> Web DTO 全链路无损，空数组不变成 `null`。
- [ ] revision transaction rollback、历史引用、current 移除、跨记录 copied logical attachment、global dedupe 和 permanent purge 无宽限语义通过真实 PostgreSQL/MinIO tests。
- [ ] `record_attachments` deletion adapter 的 descriptor、health、preview、purge、verify 与 backup/restore inventory seams 可由后续任务注册；缺少其他 adapter 时 production deletion 仍 fail closed。
- [ ] Web lazy transport、queue/controller、状态/重试/取消/移除和 authorized download primitives 通过 focused Vitest、lint、build、bundle 与 CSS gates；本任务不伪造完整 drawer。
- [ ] local 持久目录、S3、processor、scanner、容量/队列/失败健康在 Compose/systemd config/static tests 可验证。
- [ ] `make verify-go`、Node 22 `make verify-web`、local/MinIO/processor integration、Docker static/build 和 Trellis quality review 通过。

## Out of Scope

- 不把用户附件解析成系统证据，不自动执行内容，不自动抓取远程 URL。
- 不建设完整 Markdown 编辑器、材料侧栏、390px drawer 或页面级 workflow；属于 Child 5。
- 不建设通用 record import/export/archive；属于 Child 10。
- 不建设全局 backup/restore controller、RecoveryPointManifest orchestration 或跨存储恢复演练；属于 Child 11。
- PDF export 属于 Child 10；本任务只生成安全 attachment preview。
- 不支持可执行文件、Office 宏、匿名链接、用户直连 S3 下载、旧数据库升级、staging 或 release cutover。

## Execution Gate

- [x] Child 1/2 已合入 protected `main` 并完成 post-merge verification。
- [x] 用户批准 2026-08-04 bounded three-checkpoint design。
- [x] Trellis planning artifacts 完成一致性自审。
- [x] 已执行 `task.py start`，implementation code 仅在当前 feature worktree/branch 推进。
