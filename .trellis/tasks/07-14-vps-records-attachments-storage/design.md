# Blob、附件、配额与扫描设计

## 1. 数据与接口

`0053_create_record_attachments.sql` 创建 `blob_objects`、`record_attachments`、`attachment_uploads`、`attachment_upload_parts`、`record_revision_attachments`、`processor_workspaces`、`blob_gc_pins` 和 receipts。logical attachment 保存名称/MIME/大小/author/security state；Blob key 为 digest，不含原文件名。

```go
type BlobStore interface {
	Put(context.Context, PutRequest, io.Reader) (ObjectVersion, error)
	Open(context.Context, ObjectVersion, ByteRange) (io.ReadCloser, error)
	Delete(context.Context, ObjectVersion) error
	Stat(context.Context, ObjectVersion) (ObjectInfo, error)
}
```

local/S3 adapter 都要求 conditional create、hash/size match 与 deletion receipt。S3 presign 只允许 upload session 的随机临时 key；complete 后服务端 copy 到 digest final key并删除临时对象。

## 2. Processor isolation

新增 `cmd/houfeng-content-processor`，通过数据库 claim + BlobStore读取隔离对象，固定调用 ClamAV INSTREAM、`pdfinfo/pdftoppm` 或 image/text decoder。禁止 shell、网络、core dump 和复用浏览器 profile；workspace/profile/cache 有 attempt/lease/expiry/receipt。

Docker processor 使用 tmpfs、non-root、`cap_drop: ALL`、read-only root、`LimitCORE=0`；systemd 是单独 service。未配置 required processor 时复杂附件入口明确不可用。

## 3. 配额与 GC

单记录逻辑附件 100 MiB、单文件 25 MiB、项目逻辑附件默认 10 GiB（配置可降低/提高但有硬上限）；证据配额独立。upload reserve在接收字节前建立，complete/expire释放。普通 orphan 24h，永久删除独占对象无宽限；GC 扫描引用+pin 后 CAS 删除。

## 4. HTTP/Web

API 使用 design §19.2 路径；local backend PUT流式，S3返回分段/presign instructions。下载 content-disposition 使用安全展示名，CSP/Content-Type/nosniff；文本预览只返回 UTF-8 有界内容，PDF/图片返回受管衍生物。

Web 组件留在 `pages/records/components`，不直接调用 fetch。上传队列区分本地传输、扫描、可用、拒绝、隔离/重试；材料错误不损坏正文草稿。

## 5. 适配器

- Revision participant在同事务写 logical attachment refs，拒绝非 available/越权/已 reservation对象。
- Deletion adapter移除逻辑引用、取消 upload/processor、验证全局引用后删除独占 Blob。
- Backup adapter枚举 exact object version/hash/pin；restore adapter按 manifest验证后重新挂载。
