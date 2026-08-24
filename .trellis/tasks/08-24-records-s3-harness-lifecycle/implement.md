# Records S3 runner 生命周期实施计划

> 全程 TDD。测试只可操纵 test-owned TMPDIR、fake toolchain 或唯一 label 的本次真实资源。

## Phase 0: Start gate

- [ ] 用户批准 parent 最终规划；task 正式 start。
- [ ] 运行 `trellis-before-dev`，读取 backend quality、record integration、security 与 branch specs。
- [ ] 确认 Docker/MinIO 可用，但修复前不重跑会制造 root-owned residue 的旧 S3 gate。
- [ ] 记录现有 focused Go baseline；历史 residue 不在本任务清理范围。

## Phase 1: RED — lifecycle contract

**Files:** `internal/center/recordbackup/runner_lifecycle_test.go`（新）及现有 script tests

- [ ] 建 fake docker/go/toolchain，通过 `/usr/bin/bash` 执行三个真实 entrypoints。
- [ ] 表驱动 RED：body 0 + container/volume/workspace cleanup failure 必须非零且继续清后项。
- [ ] RED：body 23 + cleanup failure 保留 23 并报告 teardown；skip/setup failure 仍清资源。
- [ ] RED：默认无 temp residue；keep 模式只保留已约定资源并打印 exact identifiers。
- [ ] 修改 source-ratchet 预期，禁止 bind mount 与 masked cleanup；确认当前实现 RED。

## Phase 2: GREEN — shared cleanup and named volume

**Files:** 三个 runner、`scripts/lib/records-runner-lifecycle.sh`、对应 script tests

- [ ] 实现 body/cleanup 状态仲裁、container→volume→workspace 顺序及逐项 diagnostic。
- [ ] 两个外层 runner 创建随机 labeled named volume，以 `--mount type=volume` 挂 `/data`。
- [ ] MinIO/PostgreSQL containers 使用同一 safe run label；只跟踪 exact created resources。
- [ ] 直接 child 移除 `docker rm ... || true` false-green，接入同一 helper。
- [ ] 运行 lifecycle Go tests 与 `bash -n` 至 GREEN；验证 security content scan。

## Phase 3: Real no-leak gate

**Files:** `scripts/test-records-s3-lifecycle.sh`（新）

- [ ] wrapper 创建两个唯一 run ids 和 test-owned TMPDIR，运行前验证对应 label 为空。
- [ ] 完整运行 `run-records-integration.sh --profile s3`，返回后断言 container/volume/workspace 零。
- [ ] 完整运行 `run-records-recovery.sh --profile s3 --all`，重复零残留断言。
- [ ] emergency cleanup 只按 exact label/path 恢复本次资源，且不得把 leak assertion 变绿。
- [ ] 确认无 SKIP、无新增 root-owned `houfeng-records-*` host state。

## Phase 4: Compatibility gates

- [ ] 运行 integration local 与 recovery local `--all`，验证无-volume 路径。
- [ ] 复跑 S3 integration/recovery 功能、permanent-delete disabled 与 skip rejection assertions。
- [ ] `go test ./internal/center/recordbackup ./internal/center/recordrestore -count=1`。
- [ ] Go 1.26.2 `make verify-go`、所有脚本 `bash -n`、`git diff --check`。

## Phase 5: Review and evidence

- [ ] `trellis-check` 独立复核 exact-resource ownership、failure precedence 与内容安全。
- [ ] 保存 fake fault matrix、real label/TMPDIR residue、local/S3 no-skip evidence。
- [ ] 只有 suite 和 teardown 均通过时把 parent AC-06 标为满足。

## Stop conditions

- 方案需要 prune、prefix-wide 删除或 root 权限；
- 无法将资源唯一关联到本次 run；
- cleanup failure 会覆盖原 suite code 或被吞掉；
- 真实 gate 将访问/删除 pre-existing Docker 或历史 `/tmp` residue。
