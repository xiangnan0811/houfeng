# Make Records S3 harness teardown reliable

## Goal

关闭 parent 的 I-04：integration/recovery S3 runner 不再生成调用用户无法清理的 host state，
teardown 失败可见且能使原本成功的 suite 非零退出，完整 gate 后零残留。

## Requirements

- MinIO data 使用 runner 明确跟踪、Docker 可回收的 storage owner；不得再依赖 root 容器
  向 user-owned bind mount 写入后由 unprivileged `rm` 清理。
- 两个外层 runner 及其直接 child `test-record-platform-integration.sh` 使用同一退出语义：
  保存 suite status；逐一清理 container/storage/workspace；
  suite 非零优先保留，suite 为零但任一 cleanup 失败时返回非零。
- 资源名称保持随机/隔离，不触碰预存 container、volume、workspace；cleanup 只删除本次跟踪
  的 exact names。
- keep/debug 模式必须明确哪些资源保留及如何定位，不得静默改变普通成功语义。
- 测试不能继续把 `rm ... || true` 字符串当成正确合同；必须覆盖 cleanup status 与完整 S3
  lifecycle residue assertion。

## Acceptance Criteria

- [x] 单元/脚本测试先复现“cleanup fail + suite success 仍 exit 0”，修复后要求非零；suite
  原错误码在 cleanup 同时失败时保持。
- [x] integration/recovery S3 正常运行后，本次 container、volume、workspace 全部不存在；
  host 无新增 root-owned `/tmp/houfeng-records-*`。
- [x] local 与 S3 两种 profile、recovery `--all`、skip rejection 与 permanent-delete disabled
  assertions 全部保持通过。
- [x] 两个 runner 与直接 child 的 cleanup 状态仲裁一致，并由测试防止未来漂移。
- [x] 不清理任何 pre-existing/unknown Docker 或 `/tmp` 资源。

## Out of Scope

- 修改 S3/backup/restore product semantics、MinIO release、bucket format 或生产部署。
- 清理历史未知 residue；该操作需要独立授权和 exact recovery plan。
