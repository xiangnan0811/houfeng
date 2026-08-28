# v0.77.6 VPS Detail hardening bounded verification contract

本文件只描述父任务及三个子任务的最终验证边界，供 implement/check agent 完整注入。通用质量规范仍适用于仓库；本任务不直接注入超过默认 32768-byte 单文件上限的 quality guide。

## Runtime

- Web 命令使用 `/home/murray/.nvm/versions/node/v22.23.1/bin` 提供的 Node.js 22.23.1。
- 所有命令从 `/home/murray/code/houfeng/.worktree/v0776-vps-hardening` 或明确的 `web/` cwd 执行。
- 不得把测试辅助 parser、source assertion 或 spy green 当成 runtime green；每个 race 修复必须先有 controlled deferred RED，再改 production。

## Focused backend gates

```sh
go test ./internal/center/http/handlers -count=1
go vet ./internal/center/http/handlers
```

- scoped subscription create 必须覆盖 required missing/null、optional non-nullable null、nullable date null、显式 zero/false/blank、path `vps_id`、replay/reuse。
- TypeScript DTO 与 Go mirror parser 必须有直接 negative tests，拒绝额外 union member、`undefined`、unknown 与空 union member；`BillingPeriodUnit` / `RenewalMode` 只有在同源、注释外唯一 live 定义被证明为纯 string-literal union 时才可分类为 string，alias 加入 `number` / `undefined`、comment shadow、duplicate live definition 或多行 `&` / `|` continuation 必须失败，quoted literal 内的 pipe/escape 必须保留。未知 Go DTO 类型、near-miss struct-tag keys、commented DTO marker、非 dash anonymous embedding、line/block-comment anonymous embedding、same/later-line object intersection/suffix 以及 manifest 缺失/null `required` / `nullable` 也必须直接 fail closed；raw struct tag 内 comment marker 必须保留，未闭合/多行外部 block comment 必须拒绝，不能只依赖最终 manifest mismatch 或 backend runtime 返回 400。
- scoped `status` unknown-field 用例必须带有效 `Idempotency-Key`，精确断言 `error: "invalid json"`，并证明 repository/idempotency create 零调用。
- collection create path 继续保持既有 direct decode 行为。

## Focused web gates

```sh
npm --prefix web run test -- --run src/lib/vpsSubscriptionCreateContract.test.ts src/pages/VPSDetailPage.legacy-ownership.test.tsx src/pages/vps-detail/vpsWriteOwnerStore.test.ts src/pages/vps-detail/LegacyVPSDetail.test.tsx
npm --prefix web run lint
npm --prefix web exec -- tsc -b web/tsconfig.json --pretty false
```

- Legacy race tests使用 controlled deferred promise，禁止 sleep。
- `npm run` 会把 cwd 切到 `web/`；直接使用 `npm --prefix web exec` 的命令必须给出 root-cwd 可解析的显式路径（例如 `web/tsconfig.json`）。
- 必须覆盖 query-skip route effect 后 deferred mutation → unmount → late settle，以及 cancellation preview pending/loaded A1 → 两类 close → stale/closed → reopen A2。
- 必须覆盖 same-VPS route A1 被 mutation/refresh 或 reload A2 supersede，且 payload、catch 与 functional state commit 都拒绝旧结果；stale-success fixture 必须延迟 detail 后的二阶段请求，并用独立受控 interleaving 让 payload guard 与 functional updater guard 各自实际决定结果。cancellation deep-link route preview A1/A2 与非 cancellation payload-reset/manual-preview 也必须独立覆盖。
- same-VPS query reload/archive review 必须同时覆盖 late rejection 与 late success。stale-finally 证明在 A2 pending 时先 settle A1，断言 A2 loading/confirm authority 不变；独立 stale-data 证明先完成 eligible A2、启用 confirm，再 settle 带 blocker 的 A1，断言 A2 data/confirm 不变。删除 `then` / `finally` guard 必须分别使对应证明 RED。
- 全部 12 个 write operation、A/B transport isolation、A→B→A refresh commit、archive review ID+VPS owner 与 conflict recovery 必须保持 green。

## Formal browser and repository gates

```sh
npm --prefix web run test:e2e
make verify-web
make verify-go
git diff --check
```

- `test:e2e` 是正式 Chromium gate，不可由 Vitest 或 source gate替代。
- 已批准的既有 attachment PNG golden 是唯一可接受的 `make verify-go` 失败：actual `0d749fd4e5010a847bd9b8872b56cf56049caa705d45616e4a823cc2a4768c6e`，expected `dac4e6f598e26f4dcfb32ea88f81375f42a14739719a9761db54160b1267ed9d`。任何其他失败都必须修复或报告为 blocker。
- `make verify-go` 的格式化阶段若触碰任务外基线文件，必须精确恢复用户原内容，并用 status/diff 证明最终 scope。

## Trellis and worktree reconciliation

- validate 父任务和三个子任务；不得再出现 max-byte/context truncation warning。
- 最终 tracked scope 应能逐项解释；不得擅自 stage、commit、push、创建 PR 或归档任务。
- HEAD、merge-base、branch、dirty/untracked task artifacts 与验证结果必须在最终交接中核对。
