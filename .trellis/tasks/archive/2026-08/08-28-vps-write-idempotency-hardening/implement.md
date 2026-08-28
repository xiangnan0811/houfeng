# VPS 写入生命周期与幂等合同实施计划

> 执行采用 Trellis implement/check 子代理与 TDD；每个 RED 必须先观察预期失败。实现最初按用户要求保持未提交供外部复审；2026-08-28 外部复审通过后，用户明确授权继续完整 protected delivery、release PR、发布核验与现场清理。

## Goal

在 `codex/vps-write-idempotency-hardening` worktree 中关闭 v0.77.6 复审的 P2-01、P2-02、P3-01；外部复审通过后按逻辑批次提交并交付到新版本，核验发布产物后归档与清理。

## Risk boundaries

- 不修改 `0061` 及更早 migration；新增 `0062` 与 current APP ACL fragment。
- 不以 AbortController 或业务唯一约束代替 owner/receipt 合同。
- 四类 normalized digest、receipt lookup 与结果插入必须在各自 repository 的同一事务内。
- AppShell registry 以用户身份隔离；不得使用 module-global store。
- 每一轮修改后重新搜索所有 `createVPSExperienceLog|createVPSService|createVPSDomain|createVPSMonitoringInstance|createVPSSubscription` 调用点。

## TDD execution checklist

- [x] **1. Baseline and RED inventory**
  - 在 Node 22 环境安装锁定依赖并运行现有 focused tests：`vpsWriteOwnerStore`、`VPSDetailPage.legacy-ownership`、`VPSOverviewManagementActions`、`api`、两个 subscription contract tests、四类 handler/store/migration tests。
  - 记录现有发布基线与任何既有 PNG golden 差异；基线异常在继续实施前单独归因。

- [x] **2. RED — authenticated registry survives route/view changes**
  - 修改/新增 `web/src/app/layout/AppShell.test.tsx`、`web/src/pages/VPSDetailPage.legacy-ownership.test.tsx`、`web/src/pages/vps-detail/VPSOverviewManagementActions.test.tsx`、`web/src/pages/vps-detail/vpsWriteOwnerStore.test.ts`。
  - 先证明当前行为失败：离开 `/vps/A` 再返回会发第二个 POST；Legacy owner 不阻止 Overview；Overview pending remount 失去锁；store 不保留 digest/key/startedAt。
  - 覆盖 exact-token settle、different-VPS parallel、same-digest retry、changed-digest、409 rotate、confirmed clear 与 user-keyed shell reset。

- [x] **3. GREEN — AppShell provider and shared owner implementation**
  - 将共享 store/context 放入 `web/src/lib/` 或符合 web state/data spec 的等价 shared 路径；更新 `web/src/app/layout/AppShell.tsx` 创建 user-scoped provider。
  - 更新 `web/src/pages/VPSDetailPage.tsx`、`web/src/pages/vps-detail/LegacyVPSDetail.tsx`、`web/src/pages/vps-detail/VPSOverviewManagementActions.tsx` 使用同一 store、view token 与 settle revalidation。
  - 增加 route-level pending notice；所有 Legacy/Overview 写入口都通过 registry begin/finish，删除 Overview `submissionLockRef` 与 create-specific本地 UUID authority。
  - 运行步骤 2 的 focused tests，确认 RED 全部转 GREEN。

- [x] **4. RED — four scoped frontend API headers and retry keys**
  - 在 `web/src/lib/api.test.ts` 与 Legacy/route tests 中要求四个 `createVPS*` API 显式接收 key并发送 `Idempotency-Key`。
  - 分别证明 transport reject 后同 body 重试使用相同 key、body 变化或 409 后使用新 key、成功后未来相同表单使用新逻辑 key。

- [x] **5. GREEN — frontend create identity plumbing**
  - 更新 `web/src/lib/api.ts` 的 experience/service/domain/monitoring create 签名与 headers。
  - 更新 `LegacyVPSDetail.tsx` 五个 create（含既有 subscription）在 provisional owner 后计算 canonical SHA-256 digest并使用 registry key；保持 path VPS ID 为唯一 scope authority。
  - 运行 API、Legacy、VPSDetailPage 与 Overview focused tests。

- [x] **6. RED — shared server key/digest contract and handlers**
  - 新增 `internal/center/createidempotency/*_test.go`，覆盖 key normalization、canonical digest稳定性与字段变化。
  - 扩充 `internal/center/http/handlers/vps_test.go`、`asset_services_test.go`、`asset_domains_test.go`、`asset_links_test.go`：每个 scoped POST 缺/非法 key 为 400 且零 create；same key replay 200；different digest 409 stable code。
  - 保持 collection create handler fixture 与既有状态不变。

- [x] **7. GREEN — handler/domain interfaces**
  - 新增 shared `internal/center/createidempotency` helper，并让 subscription helper保持兼容。
  - 更新 `renewals`、`assetservices`、`assetdomains` 与 linked-monitoring creator 的 scoped idempotent interfaces/error mapping。
  - 更新四个 handler 读取 key、调用 idempotent方法、区分 201/200 并映射 allowlisted 400/409 code。
  - 运行四类 handler、router 与 subscription regression tests。

- [x] **8. RED/GREEN — migration, transaction receipts, lost response**
  - 新增 `db/migrations/0062_create_vps_create_idempotency.sql` 与 migration source/registry/current APP ACL fragment；更新 `internal/center/store/migrate/*` 的顺序、对象与权限测试。
  - 为 `internal/center/store/renewal_decisions.go`、`asset_services.go`、`asset_domains.go`、`monitoring_instances.go` 加入 idempotent事务路径；抽取只在能保持 domain scanner/error mapping 清晰时共享的 receipt helper，避免复制事务不变量。
  - 先写 store unit RED：same key replay、mismatch、begin/lock/lookup/insert receipt/commit失败均 fail closed；再实现 GREEN。
  - 新增 PostgreSQL integration：每类第一次持久化后模拟响应未知，再用同 key 重试，断言同 ID、单一结果行、单一 receipt；monitoring 同时断言单一 link。
  - 使用仓库严格 PostgreSQL runner；缺少必需 DSN/fixture 记录为阻塞，不以 skip 当作通过，也不擅自创建/替换基础设施。

- [x] **9. RED/GREEN — explicit DTO date format**
  - 更新 `web/src/lib/vpsSubscriptionCreateContract.test.ts` 与 `internal/center/http/handlers/vps_subscription_create_contract_test.go`，先让 ordinary `string | null` 期望 `type:string`/无 format 并观察当前 date 猜测 RED。
  - 更新 `web/src/lib/types.ts` 增加并使用严格 `ISODate` alias；更新 `vps_subscription_create_fields.json` 为 `type:string, format:date`。
  - 两个 mirror 同步加入可选 format、alias exact-definition与 manifest validation；保留现有 parser fail-closed negatives。
  - 运行 TS/Go contract focused tests。

- [x] **10. Focused integration and route verification**
  - 运行全部 touched package tests、`go vet ./internal/center/http/handlers ./internal/center/store/...`、gofmt检查、web lint/type-check/build。
  - 运行 Chromium route regression，精确覆盖报告的三条 lifecycle 场景；保存实际通过计数。

- [x] **11. Full quality gate and privacy review**
  - 运行 `make verify-go`、Node 22 下 `make verify-web` 与 Trellis check；如环境允许，运行相关完整 Chromium Playwright。
  - 搜索 raw request body、idempotency key/digest、details/note 是否进入日志、错误或快照；检查 migration ACL 与备份/恢复清单影响。
  - 将实际命令、通过数、环境阻塞和既有失败写入任务 research evidence，不用较小 focused GREEN 代替全量门禁。

- [x] **12. External-review handoff without commit**
  - 更新 PRD acceptance 状态和任务证据，但不 archive、不进入 Trellis commit 步骤。
  - 运行 `git diff --check`、`git status --short --branch`、`git diff --stat`、`git diff --cached --quiet`；确认无 staged changes、无新 commit、无 push/PR。
  - 交付绝对路径、精确 dirty 文件、测试证据与剩余阻塞，停下等待外部审查意见。

- [x] **13. Batched commits and feature PR**
  - 按 DTO mirror、backend persistence、HTTP wiring、frontend ownership/API、Trellis evidence/spec 的逻辑边界分批提交；每批提交前核对 staged diff，不混入任务外文件。
  - 推送 `codex/vps-write-idempotency-hardening`，创建 feature PR，等待全部 required checks 通过后经 protected main 合并。

- [x] **14. Main and release delivery**
  - 核验 feature merge 后 main CI 与 Release Please；等待生成/更新 release PR，并在其 required checks 全绿后合并。
  - 核验新 tag/GitHub Release、签名 agent/checksum/deployment assets、release 后 main CI 与 `publish-images`，并确认 Docker Hub `vX.Y.Z`/`X.Y.Z`/`latest` 指向含 amd64+arm64 的同一 manifest。

- [x] **15. Archive, journal, and cleanup**
  - 把 PR、run、merge commit、release/tag/image digest 写入任务 evidence，完成 PRD acceptance，归档任务并记录 developer journal。
  - 通过非 main 分支/PR 交付归档工件；全部远端检查完成后同步本地 main，删除已合并 feature/archive 分支与 worktree，保留干净主 checkout 供下一次开发。

## Planned validation commands

```bash
go test ./internal/center/createidempotency ./internal/center/http/handlers ./internal/center/store ./internal/center/store/migrate
go vet ./internal/center/createidempotency ./internal/center/http/handlers ./internal/center/store ./internal/center/store/migrate
NODE_ENV=test npm --prefix web run test -- --run src/lib/api.test.ts src/lib/vpsSubscriptionCreateContract.test.ts src/pages/vps-detail/vpsWriteOwnerStore.test.ts src/pages/VPSDetailPage.legacy-ownership.test.tsx src/pages/vps-detail/VPSOverviewManagementActions.test.tsx src/app/layout/AppShell.test.tsx
make verify-go
make verify-web
git diff --check
git status --short --branch
```

PostgreSQL 与 Playwright 命令在实施前从当前 spec/runner 解析准确入口，不硬编码过期模式。

## Delivery and release evidence

- Feature PR #468 merged as `080d2c025bf843d193f9d5fb69542af18083918e` after all seven required checks passed on head `72c0d9912b633ff0de410564e4d1ccf39e7cd217`; feature PR CI run `33182939197` and post-merge main CI run `33183499335` both passed.
- Release Please run `33183499353` created release PR #469. Its head `fed1a072025b5f9a21316ff1e468642a20228124` passed all seven checks in CI run `33183539006`, then merged through the protected branch as `415de509ca853769fa97d480fd9f473896ba5a55`.
- Release Please run `33183993833` published public non-prerelease `v0.78.0`; the tag resolves exactly to `415de509ca853769fa97d480fd9f473896ba5a55`. Release-after-merge main CI run `33183993850` and `publish-images` run `33184005814` passed.
- The six public release assets are the amd64/arm64 agent binaries, signed checksum manifest, `compose.yaml`, and `compose.env.example`. Local minisign verification against the installer-pinned public key and both binary SHA-256 checks passed.
- Docker Hub tags `v0.78.0`, `0.78.0`, and `latest` all resolve to `sha256:73772ba18dcbfb37b622117f2fce9d5b4ffa5018541b3c04ee78001912e7e27a`, containing linux/amd64 (`sha256:c485af5878f978963edbf82c067285ad9a23924cf207abb49e96f26d6af04795`) and linux/arm64 (`sha256:a80bfdbca988728a9e0e89f698530ecbba8cb9e403f9798f6926fad636ef2cd7`) images plus their provenance attestations.
- Exact URLs, run IDs, asset digests, cleanup boundaries, and the initial local-only attachment golden exception are preserved in `research/delivery-release-evidence.md` and `research/final-verification-evidence.md`.
