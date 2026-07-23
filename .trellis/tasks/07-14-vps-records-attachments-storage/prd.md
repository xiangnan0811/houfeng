# Blob、附件、配额与扫描

## Goal

实现 local/S3 Blob、附件准入扫描、配额、引用与回收、下载授权及备份恢复适配。

## Requirements

- 父设计：`../07-13-vps-detail-experience-design/design.md` §13、§15、§21、§23。
- 直接依赖：子任务 1、2 已合入 main 并通过 post-merge CI。
- 实现逻辑 attachment 与物理 content-addressed Blob 分离；相同字节可去重，权限、引用和审计不可合并。
- local 与 S3 backend 使用同一 conformance suite；local 必须持久卷、原子写/rename/fsync，S3 使用私有 bucket、version/hash 与短期 presign。
- 上传状态机为 created→uploading→quarantined/scanning→available|rejected|expired；只有 available attachment 可进入正式 revision。
- 服务端流式计算 SHA-256、MIME/signature/大小/条目/复杂度，禁止执行文件、脚本、SVG/HTML 和伪装类型。
- PNG/JPEG/WebP/PDF/allowlist 文本支持安全预览；复杂 renderer/scanner 在独立 processor service 的隔离 workspace 中运行。
- 压缩包确定危险/加密/炸弹/签名不符为 rejected；仅 scanner 暂不可用保持不可预览/引用/下载的 quarantined 并有界重试/到期。
- 提供项目/记录逻辑配额、物理用量、orphan/pin/GC；永久删除独占 Blob 无宽限 purge，普通孤立保留 24 小时。
- 下载/预览每次重新授权、取得 content stream lease，并在 permission revoke/deletion reservation 时取消流。
- 提供 record revision attachment reference participant、deletion/backup/restore adapter 与 Web 上传队列/授权下载组件。

## Acceptance Criteria

- [ ] local/MinIO 对同一 contract 的上传、dedupe、range/download、hash、delete、version mismatch 结果一致。
- [ ] MIME spoof、超限、zip-slip、duplicate path、symlink/hardlink、炸弹、加密包、恶意扫描命中均 fail closed。
- [ ] scanner unavailable 状态不会被误标 rejected/available，重试/超时/expired 后 workspace/parts 残留为0并有receipt。
- [ ] revision transaction失败不留下可见引用；历史 revision 引用不会因 current移除或 GC 被删除。
- [ ] 跨记录相同 payload 的逻辑权限独立，删除一条记录不删除其他引用；全局无引用才物理删除。
- [ ] reservation/revoke 后新 presign、complete、preview/download bytes为0；已开始stream按lease取消并进入影响预览。
- [ ] local 持久目录、S3/processor/scanner 配置、容量/队列/失败健康在 Compose/systemd 可验证。
- [ ] Web 上传状态、材料失败/重试/移除、授权下载与 390px 材料抽屉组件测试通过。
- [ ] `make verify-go`、Node22 `make verify-web`、local/MinIO/processor integration 与 Docker static tests通过。

## Out of Scope

- 不把用户附件解析成系统证据或自动执行内容。
- PDF 导出属于子任务 10；本任务只产生安全附件预览。
- 不支持可执行文件、Office 宏、远程 URL 抓取或匿名链接。

## Execution Gate

- 保持 planning；依赖与执行授权缺一不可。
