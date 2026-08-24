# Research: S3 runner MinIO ownership and lifecycle

- Query: 针对审查 finding I-04，确定可移植的 MinIO 存储所有权/清理设计、suite 与 teardown 状态优先级、无资源泄漏的测试方式，以及精确修改文件和验证命令。
- Scope: mixed（仓库代码、既有 Trellis 合同、Docker/Bash 官方文档、受控本机 smoke）
- Date: 2026-08-24

## Findings

### 1. 当前根因与完整调用链

| 文件 | 当前职责 / 问题 |
| --- | --- |
| `scripts/run-records-integration.sh:43-60,85-100` | 创建 caller-owned workspace，却把 `$workspace/minio` bind-mount 到以 root 运行的 MinIO `/data`；`docker rm` 与 `rm -rf` 失败都被吞掉，最终无条件返回进入 trap 前的状态。 |
| `scripts/run-records-recovery.sh:54-71,96-111` | 与 integration runner 相同的 ownership 和 false-green 问题。 |
| `scripts/test-record-platform-integration.sh:43-57,82-94` | 两个外层 runner 的直接 child。PostgreSQL 已使用 Docker tmpfs，不会制造 host root-owned 数据，但 `docker rm -f ... || true` 仍会掩盖四个 fixture container 的 teardown 失败；只修外层仍不能证明整条 runner 生命周期干净。 |
| `internal/center/recordbackup/profile_script_test.go:10-48` | 仅做字符串 presence/forbidden 检查，且当前主动要求 `rm -rf "$workspace" || true`。 |
| `internal/center/recordbackup/recovery_script_test.go:10-48` | 与 integration source-ratchet 相同，未执行 lifecycle 或状态矩阵。 |
| `internal/center/store/migrate/app_acl_r2_postgres_integration_test.go:2309-2418,2460-2702` | 已有可复用的测试模式：隔离 `PATH`/`TMPDIR`、fake Docker/toolchain、执行真实 shell runner、记录 run/rm 生命周期、断言 child skip/failure 与 workspace 清理；应沿用该模式而不是继续只做字符串测试。 |
| `internal/center/recordrestore/security_assembly_test.go:108-123` | 对 integration/recovery 脚本做 content-safe 扫描；新增共享 lifecycle helper 或真实 gate 后应纳入同一 allowlist 扫描。 |
| `.trellis/spec/backend/record-integration-contract.md:9-15,58-71,75-86,100-106` | 规定这些 runner 属于 Records integration owner，Docker/S3 缺失和 skip 必须 fail closed，并要求 integration/recovery profiles 成为真实门禁。 |
| archived Child 11 `design.md:121-130` 与 `prd.md:53-56,70-82,125-145` | runner 明确 owns cleanup；临时 workspace/partial state 必须有界，完整 local/S3 integration 与 recovery gate 必须通过。 |

I-04 不只是 workspace 删除问题。当前调用图是：

```text
run-records-{integration,recovery}.sh
  ├─ MinIO container + host bind-mounted /data
  └─ test-record-platform-integration.sh
       └─ four PostgreSQL containers on tmpfs
```

因此最小完整修复边界必须同时保证：MinIO container、MinIO data volume、四个 PostgreSQL containers、child workspace、outer workspace 都在默认模式下消失；任何一个 teardown 失败都不能在原 suite 成功时返回 0。

### 2. 推荐存储方案：Docker 管理、带标签的命名 volume

推荐把两个外层 runner 的 MinIO `/data` 从 host bind mount 改为每次运行新建的 Docker-managed named volume：

```bash
minio_volume=$(docker volume create \
  --label "com.houfeng.records.runner=$records_runner_kind" \
  --label "com.houfeng.records.run=$records_run_id")
volumes+=("$minio_volume")

docker run --rm -d \
  --name "$name" \
  --label "com.houfeng.records.runner=$records_runner_kind" \
  --label "com.houfeng.records.run=$records_run_id" \
  --network=host \
  --mount "type=volume,source=$minio_volume,target=/data" \
  ...
```

设计理由：

1. volume 内部 UID/GID 由 Docker daemon 管理，不再把 root-owned `.minio.sys` 暴露到 caller-owned `$TMPDIR`，caller 只需拥有 Docker 访问权即可通过 `docker volume rm` 回收。
2. `docker volume create` 不给 name 时由 Docker 生成唯一 name，并支持 `--label`；避免自己拼接名称后意外复用既有同名 volume。
3. named volume 不会随 container 自动删除，所以生命周期是显式且可验证的：container 必须先删，volume 后删。Docker 官方也明确说明 named volume 需要单独删除，且使用中的 volume 无法删除。
4. `com.houfeng.records.run=<unique-id>` 同时打在 MinIO/PostgreSQL containers 和 MinIO volume 上，使真实 lifecycle gate 能只观察/回收本次运行的 exact resources，不用 `docker system prune`、`docker volume prune`、宽泛 name glob 或删除未知 `/tmp/houfeng-*`。
5. `HOUFENG_RECORDS_RUN_ID` 可作为非敏感 observability/test override；缺省值使用 outer workspace basename。必须只允许短的 `[A-Za-z0-9_.-]+`，且 label 中禁止 credential、URL、DSN 或 fixture content。

本次在当前机器使用精确镜像 `minio/minio:RELEASE.2024-12-18T13-15-44Z` 做了一个有唯一 label 的 named-volume health smoke：MinIO 启动和 `/minio/health/live` 成功；随后删除 container/volume，按 label 查询均为空。该 smoke 只验证 mount/lifecycle 机制，不替代修复后的完整 S3 suites。

#### 不推荐的替代方案

- `--user "$(id -u):$(id -g)"` + bind mount：把 host UID/GID 和镜像运行用户耦合起来；Docker Desktop/remote daemon/不同用户环境下不稳定，也改变 MinIO image 的启动身份。
- Docker tmpfs：仓库 PostgreSQL fixture 已使用该模式，且可消除磁盘残留；但 Docker 官方说明它仅适用于 Linux，数据计入 container memory/cgroup，并可能造成额外 OOM 风险。它可作为 Linux-only 小 fixture 方案，不是这里的首选可移植 S3 profile。
- 匿名 volume + `--rm`：Docker 可自动回收匿名 volume，但难以为 volume 本身附加本次 run 的可查询 identity；这会削弱“失败后能证明/精确恢复”的 lifecycle 测试。显式创建并跟踪 named volume 更适合本任务。

### 3. cleanup 状态合同

建议三条 runner 共用一个小型 Bash helper，例如 `scripts/lib/records-runner-lifecycle.sh`。三者都初始化 `workspace`、`containers=()`、`volumes=()` 和 retain policy，然后使用：

```bash
trap 'records_runner_cleanup "$?"' EXIT
```

helper 的行为必须是：

1. 第一时间接收并保存进入 EXIT trap 前的 `body_status`，随后 `trap - EXIT`，避免显式 `exit` 再次触发 trap。
2. `set +e` 后继续尝试所有清理；不能因第一个失败跳过后续资源。
3. 顺序固定为 containers -> volumes -> workspace。volume 在任何关联 container 存在时不可删除。
4. 每个失败打印 resource kind + exact name/path 到 stderr，并把 `cleanup_status` 置为非零；禁止 `>/dev/null 2>&1 || true` 吞掉全部证据。成功输出可以静默。
5. 最终状态矩阵：

| body/suite | teardown | 最终退出码 |
| --- | --- | --- |
| 0 | 成功 | 0 |
| 0 | 任一失败 | 1（或统一的非零 cleanup code） |
| 非零 N | 成功 | 原始 N |
| 非零 N | 任一失败 | 原始 N；同时在 stderr 报告 teardown failure |

这样既不会用 cleanup failure 覆盖首要 suite failure，也不会在 suite 成功、cleanup 失败时 false-green。

`HOUFENG_RECORDS_KEEP_WORKSPACE=1` 是显式调试例外，不应被当作泄漏：仍必须删除 containers；建议保留 outer workspace 和 MinIO volume，并在 stderr 打印 exact workspace/volume 名。默认/CI/lifecycle gate 必须不设置它。若实现方不希望扩大该变量语义，可改用单独的 `HOUFENG_RECORDS_KEEP_RESOURCES=1`，但需要在 task design 中写清兼容选择。

EXIT trap 无法保证 `SIGKILL`、daemon crash 或主机掉电后的自动回收。run label 是这类异常的恢复边界；正常完成、setup failure、suite failure、skip、INT/TERM 必须由测试覆盖，异常恢复只能精确按 label 操作，不能 prune 全局资源。

### 4. 测试设计：deterministic fault matrix + real S3 no-leak gate

#### A. 默认 Go gate：fake toolchain，不接触真实 Docker

新增 `internal/center/recordbackup/runner_lifecycle_test.go`，沿用 `app_acl_r2_postgres_integration_test.go:2460-2702` 的模式：

- 为每个 case 创建 `t.TempDir()` 下的 `bin/` 和 `TMPDIR/`。
- fake `docker` 支持并记录 `volume create/rm/ls`、`run`、`exec`、`rm`、`logs`；fake `go` 控制 child output/exit；其余必要命令只 symlink 精确 host binary。
- 通过 `/usr/bin/bash` 执行真实 `scripts/run-records-integration.sh --profile s3` 和 `scripts/run-records-recovery.sh --profile s3 --all`，而不是复制 cleanup 算法到测试。
- fake toolchain 只操作自己的 temp tree/log；测试结束由 `t.TempDir()` 回收，因此不会创建真实 container/volume/workspace。

最低表驱动 case：

1. child 0 + teardown 全成功 -> exit 0；每个 created container/volume 恰好 remove 一次；TMPDIR 为空。
2. child 0 + MinIO container rm 失败 -> exit 1；仍尝试 volume 和 workspace cleanup。
3. child 0 + volume rm 失败 -> exit 1；workspace 仍被尝试删除。
4. child 0 + workspace rm 失败 -> exit 1。
5. child 23 + teardown 失败 -> exit 23，并同时含 teardown diagnostic。
6. stdout 或 stderr 出现 `--- SKIP:` -> exit 1，所有资源仍清理。
7. MinIO start/health/setup failure -> 已创建 volume/workspace 被清理，不生成 profile report。
8. `HOUFENG_RECORDS_KEEP_WORKSPACE=1` -> container 删除；约定的 workspace/volume 明确保留并报告，不算默认 gate success 的无残留证据。

现有两个 source-ratchet 应修改为：要求 shared helper、`docker volume create`、label、`--mount type=volume` 和 volume tracking；明确禁止 host MinIO bind mount 与 `rm -rf "$workspace" || true`。不要再把缺陷字符串列在 `want` 中。

同时给 `scripts/test-record-platform-integration.sh` 增加 cleanup failure case；否则四个 PostgreSQL fixture 的 teardown 仍可被 child 0 掩盖。

#### B. 显式真实 gate：完整 S3 suites 后按 unique label 断言零残留

建议新增 `scripts/test-records-s3-lifecycle.sh`，它不是 `make verify-go` 的 Docker-free 单测，而是本任务完整验证入口：

1. 自己创建唯一 `run_id` 和 caller-owned outer `TMPDIR`，运行前断言该 label 没有 container/volume。
2. 分别用不同 run id 执行完整 integration S3 和 recovery S3 `--all`。
3. 每次 child 返回后、wrapper 自己的 emergency cleanup 前，断言：
   - `docker ps -aq --filter "label=com.houfeng.records.run=$run_id"` 为空；
   - `docker volume ls -q --filter "label=com.houfeng.records.run=$run_id"` 为空；
   - test-owned TMPDIR 下没有 runner workspace。
4. wrapper 的 EXIT trap 仅按本次 exact unique label 和自身 TMPDIR 做 emergency cleanup；它在断言发现泄漏后负责恢复环境，但测试仍返回失败。严禁按通用前缀删除或运行任何 prune。
5. wrapper 不设置 keep env，并把两个 runner 任一失败作为失败；inner runner 已负责 skip-to-failure。

该分层同时满足：普通 `go test` 可稳定覆盖失败状态矩阵；显式 S3 gate 又真实证明 MinIO/PostgreSQL/container/volume/workspace 的 end-to-end 生命周期。

### 5. 精确文件范围

建议实现所有权如下：

| 文件 | 修改 |
| --- | --- |
| `scripts/lib/records-runner-lifecycle.sh`（新） | 共享 teardown、状态优先级、diagnostic；仅处理当前 arrays/workspace，不做全局发现或 prune。 |
| `scripts/run-records-integration.sh` | 删除 `$workspace/minio` bind mount；创建/标记/跟踪 named volume；使用 shared cleanup。 |
| `scripts/run-records-recovery.sh` | 与 integration 对称。 |
| `scripts/test-record-platform-integration.sh` | 使用同一状态仲裁，不能再吞掉 PostgreSQL container cleanup failure；给 container 加同一 run label。 |
| `scripts/test-records-s3-lifecycle.sh`（新） | 运行两个真实 S3 profiles，并在 emergency cleanup 前断言本次 exact label/TMPDIR 零残留。 |
| `internal/center/recordbackup/profile_script_test.go` | 删除有缺陷的字符串 expectation；增加 volume/helper/forbidden bind contract。 |
| `internal/center/recordbackup/recovery_script_test.go` | 与 integration 对称。 |
| `internal/center/recordbackup/runner_lifecycle_test.go`（新） | fake toolchain、真实 shell entrypoint、状态矩阵、顺序和无 temp leak 测试。 |
| `internal/center/recordrestore/security_assembly_test.go` | 将 shared helper 与真实 lifecycle gate 纳入 `ScanContentSafe`。 |
| `.trellis/spec/backend/record-integration-contract.md` | 后续由主 session 通过 `trellis-update-spec` 判断是否固化 named-volume ownership、状态矩阵和 exact-label no-leak gate；research agent 不修改 spec。 |

不要顺手重构 `run-records-security.sh`、`run-records-capacity.sh` 或 `run-records-browser.sh`；它们没有本 I-04 的 MinIO/root-owned bind mount。若未来统一所有 runner cleanup，应另开范围。

### 6. 精确 RED / GREEN / full-gate 命令

实现前先让新增 lifecycle tests RED；实现后依次运行：

```bash
go test ./internal/center/recordbackup \
  -run 'TestRecords(Integration|Recovery)Script|TestRecordsRunnerLifecycle' \
  -count=1

bash -n \
  scripts/lib/records-runner-lifecycle.sh \
  scripts/test-record-platform-integration.sh \
  scripts/run-records-integration.sh \
  scripts/run-records-recovery.sh \
  scripts/test-records-s3-lifecycle.sh

./scripts/test-records-s3-lifecycle.sh

make verify-go
git diff --check
```

`test-records-s3-lifecycle.sh` 内部必须实际执行：

```bash
./scripts/run-records-integration.sh --profile s3
./scripts/run-records-recovery.sh --profile s3 --all
```

修复后还应各跑一次 local profile，证明 shared cleanup 没破坏无 MinIO/volume 的路径：

```bash
./scripts/run-records-integration.sh --profile local
./scripts/run-records-recovery.sh --profile local --all
```

仓库 `go.mod`/CI 使用 Go 1.26.2；最终 evidence 应使用该精确工具链。以上命令不得用 keep env，也不得把 cleanup failure 解释为“功能断言已通过”而放行。

### 7. External references

- Docker volumes：named volume 与 container 生命周期独立，named volume 需单独删除；`--rm` 只自动清理 anonymous volume。<https://docs.docker.com/engine/storage/volumes/>
- `docker volume create`：name 可省略由 Docker 生成；支持 `--label`。<https://docs.docker.com/reference/cli/docker/volume/create/>
- `docker volume ls`：支持精确 `label=<key>=<value>` filter。<https://docs.docker.com/reference/cli/docker/volume/ls/>
- `docker volume rm`：使用中的 volume 无法删除，因此 teardown 必须 container-first。<https://docs.docker.com/reference/cli/docker/volume/rm/>
- `docker container rm`：`-f` 强制删除；named volume 不随 `-v` 自动删除。<https://docs.docker.com/reference/cli/docker/container/rm/>
- Docker tmpfs：数据随 container 消失，但仅 Linux 可用且计入 container memory limit。<https://docs.docker.com/engine/storage/tmpfs/>
- Bash `trap` / exit status：EXIT action 在 shell 退出时执行，`$?` 是最近命令状态；需要显式保存并仲裁。<https://www.gnu.org/software/bash/manual/html_node/Bourne-Shell-Builtins.html>、<https://www.gnu.org/software/bash/manual/html_node/Exit-Status.html>

## Related Specs

- `.trellis/spec/backend/index.md`
- `.trellis/spec/backend/quality-guidelines.md:113-177,454-463,966-985`
- `.trellis/spec/backend/record-integration-contract.md`
- `.trellis/spec/guides/code-reuse-thinking-guide.md:18-38,63-83`
- `.trellis/spec/guides/cross-layer-thinking-guide.md:18-50,75-87`

## Caveats / Not Found

- 本研究开始时 remediation `prd.md` 尚未填充；当前 parent/child PRD、design 与 implementation
  plan 已结合本结论补齐。本文仍是 I-04 planning input，须在 context manifests/validate 完成并
  取得用户对最终规划的后续明确批准后才能 task start/implementation。
- 本次没有修改产品、script、test 或 spec，也没有运行现有完整 S3 runners；在修复前重跑只会再次制造已知 root-owned residue。只做了一个唯一 label、精确回收的 MinIO named-volume health smoke，结束后确认无对应 container/volume。
- named volume 解决 host ownership，但不会自动解决 teardown 状态；volume tracking、container-first 顺序、failure propagation 和真实 no-leak gate 四项缺一不可。
- EXIT trap 不覆盖不可捕获的 `SIGKILL` 或主机/daemon crash。labels 提供精确事后识别，不授权自动清理其他 run 或预存未知资源。
- `HOUFENG_RECORDS_KEEP_WORKSPACE=1` 是否也保留 MinIO volume 是唯一需要在 task design 中明确的兼容细节；本文建议保留并打印 exact name，以维持原 bind-mount 模式的调试价值。
