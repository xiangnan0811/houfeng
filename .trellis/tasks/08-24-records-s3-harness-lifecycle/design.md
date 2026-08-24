# Records S3 runner 生命周期技术设计

## 1. 根因与修复边界

两个外层 S3 runner 把 `$workspace/minio` bind-mount 到 root MinIO `/data`，随后用调用用户
执行并吞掉 `rm -rf` 失败；其直接 child `test-record-platform-integration.sh` 也吞掉四个
PostgreSQL container 的 teardown 失败。因此最小完整边界包含 MinIO container/data、四个
PostgreSQL fixture containers、outer/child workspaces 与最终 exit status，不能只替换 mount。

## 2. 资源所有权

MinIO 使用每次运行显式创建、跟踪并标记的 Docker named volume。volume 名由本次随机
workspace owner 派生，并在 `docker volume create` 前登记；container candidate 同样在 create
前登记，以便处理“daemon 已创建资源但 CLI 非零/信号中断”的窗口。container 与 volume 均带
不含敏感内容的 `com.houfeng.records.runner`、`com.houfeng.records.run` 和
`com.houfeng.records.owner` 三个 labels；run id 默认来自随机 workspace basename，test override
只能是短的 `[A-Za-z0-9_.-]+`。

登记 name 不是删除授权。创建后、挂载前必须核验三 labels；cleanup 再次核验三 labels，container
只按 inspect 返回的 immutable ID 删除。name collision、资源被替换或 ownership 无法核验时
fail closed，绝不删除/挂载外来资源。runner 不按 prefix 扫描，不执行 prune，也不触碰已有
Docker 或 `/tmp` 资源。

采用 named volume 优于 host UID 映射：MinIO 内部 UID/GID 不再泄漏到 caller-owned
workspace，且不绑定本机/remote daemon 的用户映射。也不选 tmpfs，避免把 S3 fixture 数据
强制计入容器内存并缩窄到 Linux-only 假设。

## 3. Cleanup 状态机

新增小型共享 Bash lifecycle helper。三个 runner 都在创建第一项资源前安装 EXIT trap，保存
body status 后解除 EXIT trap、让 teardown 继承忽略 INT/TERM、`set +e` 并继续尝试所有清理，
固定顺序 containers → volumes → workspace。每个失败输出 resource kind 与 exact name/path，
不能整体重定向或 `|| true`。

| suite/body | teardown | 最终状态 |
| --- | --- | --- |
| 0 | 成功 | 0 |
| 0 | 任一失败 | 统一 cleanup 非零 |
| 非零 N | 成功 | N |
| 非零 N | 任一失败 | N，另报 cleanup diagnostic |

默认模式清理全部资源。保留既有 `HOUFENG_RECORDS_KEEP_WORKSPACE=1` 作为显式调试例外：仍
删除 containers，但保留并打印 exact workspace 与 MinIO volume，维持旧 bind-mount 模式可
检查数据的价值。CI/required gate 不设置它。长跑 body 在独立、runner-owned process group 中；
INT/TERM 只转发给该 group，以有界分层宽限等待 child cleanup，再进入相同 EXIT cleanup。cleanup
开始后忽略后续 INT/TERM，避免 Docker CLI 在删除途中被打断；SIGKILL/daemon crash 无法由 trap
保证，labels 仅提供精确恢复边界，不授权本任务自动清理历史 residue。

stdout/stderr evidence sink 必须全部 wait。`--- SKIP:` 扫描区分 grep 0（发现 skip）、1（正常
未发现）与 2+（证据读取失败）；sink/scan 失败在 body 为 0 时使 gate 非零，body 非零时仍保留
原错误码。

## 4. 验证分层

默认 Go 测试使用 fake toolchain 和隔离 TMPDIR 执行真实 shell entrypoints，不连接 Docker，
覆盖 local/S3/direct-child success、container/volume/workspace cleanup failure、suite+cleanup
双失败、skip、evidence sink/scan failure、setup/partial-create/name-collision/replacement、keep，
以及 parent-only/process-group INT/TERM、ignored body、二次信号和 cleanup 中信号。现有
source-ratchet 改为要求 helper/volume/三 labels，禁止 MinIO host bind 和 masked `rm`。

显式 `scripts/test-records-s3-lifecycle.sh` 使用唯一 run labels 真实执行 integration S3 与
recovery S3 `--all`。每次 runner 返回后断言 label 下 container/volume 为空、test-owned TMPDIR
无 workspace。wrapper 的 emergency cleanup 只处理自己 exact label/path，并保持断言失败。
再分别运行 local profile，证明共享 helper 没破坏无 MinIO path。

## 5. 文件所有权与兼容

- 新增 `scripts/lib/records-runner-lifecycle.sh`、`scripts/test-records-s3-lifecycle.sh`
- 修改 `scripts/run-records-integration.sh`、`scripts/run-records-recovery.sh`
- 修改直接 child `scripts/test-record-platform-integration.sh`
- 修改 `internal/center/recordbackup/{profile_script,recovery_script}_test.go`
- 新增 `internal/center/recordbackup/runner_lifecycle_test.go`
- 修改 `internal/center/recordrestore/security_assembly_test.go`

不改变 backup/restore/S3 产品语义、MinIO image、manifest 或 bucket format；不清理已存在的未知
residue。rollback 可还原脚本/helper/test，无 schema 或持久产品数据变更，但不能重新接受
false-green required gate。
