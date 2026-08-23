# VPS 概览管理操作验证证据（2026-08-23）

## 范围与基线

- 分支：`codex/vps-records-parent-closeout`；非 main。
- 实施起点：`HEAD = origin/main = 9cfe19ef6073fa70347f50e1c59a6a073d62bedd`。
- hooks：`core.hooksPath=.githooks`，pre-commit hook 可执行。
- 本 child 未修改后端、迁移、权限、feature flag 或永久删除 wiring。
- `LegacyVPSDetail` 仍为独立 async chunk；overview route 未静态挂载整个 legacy 页面。

## 自动化门禁

全部使用 Node `v22.23.1`：

- focused mutation/route tests：2 files、19 tests，全部通过。
- `make verify-web`：退出码 0。
  - ESLint：通过。
  - Vitest coverage：192 files、1254 tests，全部通过。
  - coverage：statements 81.39%、branches 73.74%、functions 81.38%、lines 85.40%。
  - production build：通过。
  - bundle budget：通过；entry JS gzip 109967/110738、entry CSS gzip
    38314/38314、最大 async JS gzip 48452/48454。
  - CSS budget：通过；26 files、320492 bytes、2186 rules、8796 declarations。
- targeted Playwright Chromium：`--grep 'VPS 概览'`，5 tests 全部通过，覆盖 Axe、
  loading/anomaly states 与 390px visual contract。
- `task.py validate`：management child、parent closeout、final audit child 与原父任务
  四个 task 目录全部通过。
- `git diff --check`：通过。

`npm ci` 仍报告仓库现有的 7 个依赖漏洞（1 moderate、6 high）；本 child 没有修改
依赖或 lockfile，未在本范围内运行自动依赖修复。

## 真实浏览器验收

使用 production preview、Playwright CLI 和仅本地 API mock 验收；未访问或改写线上
测试环境数据。

- desktop 1440x1000 与 mobile 390x900 均无 document/dialog 横向溢出。
- 五个菜单入口均打开真实 facts/decision/subscription/cancellation/archive UI。
- cancellation 保留服务端 warning/blocker，存在 blocker 时确认按钮禁用。
- archive 在 review 完成前禁用；只有完整输入 `东京边缘` 后确认按钮启用。
- “管理”按钮 85.7x47px；五个 menuitem 均为 162x51.5px，满足 44px 目标。
- Enter 打开 facts、Tab 到下一 menuitem、Space 打开 decision、Escape 关闭并把焦点
  返回“管理”按钮。
- 五个 action dialog 的 Axe serious/critical violation 均为 0。
- 唯一 non-blocking Axe 项为 overview 既有 summary `h3` 的 moderate
  `heading-order`；目标不在本 child 新增 action UI，且与仓库现有 Axe blocking
  策略一致。

截图留存在本地 `output/playwright/`：

- `vps-overview-desktop.png`
- `vps-overview-mobile.png`
- `vps-facts-dialog-desktop.png`
- `vps-facts-dialog-mobile.png`
- `vps-facts-dialog-mobile-actions.png`
- `vps-archive-mobile.png`

## 代码复核

- Critical：0。
- Important：0。
- Minor：0 个属于本 child 的未解决问题。
- 并发审查覆盖 panel close、`vpsId` 切换、陈旧 load/mutation response、同步重复
  submit lock 与写后 refresh 失败保留旧 overview。
- 安全审查覆盖 authoritative cancellation preview、archive review、blockers、精确展示
  名确认和服务端再次校验。

## 交付与发布证据

- implementation commit：`40ddb0a9f34ee004191d1acad1798f99876c76f4`；delivery
  metadata head：`7e9080f208a5f1f5cce7e563f5030b9d068629de`。
- PR `#438`：`https://github.com/xiangnan0811/houfeng/pull/438`；7/7 required CI
  通过（run `32637157947`）。
- protected-main squash commit：`af23844adc82ce97e6815a3dbd8706f7fdab10e8`。
- 合入后 main CI：run `32637395760`，7/7 jobs 全部通过。
- Release Please prepare：run `32637395758` 成功；release PR `#439`
  `https://github.com/xiangnan0811/houfeng/pull/439` 的 7/7 CI 全部通过。
- release merge commit：`ab1ad7cdaab4a7ee57b782a3a9a45e5074b591bd`；release
  main CI run `32637630914` 的 7/7 jobs 全部通过。
- GitHub Release：`https://github.com/xiangnan0811/houfeng/releases/tag/v0.75.0`；
  tag `v0.75.0` 指向 release merge commit，包含 amd64/arm64 agent、checksum manifest
  与 minisign 签名四个已上传资产。
- publish-images run `32637639621` 成功；registry `docker.io/linnea7171/houfeng:v0.75.0`
  返回 OCI image index digest
  `sha256:22df0845c806f69f9d4bccecf02227b744b9588e73de86eb03338c068be14415`，
  明确包含 `linux/amd64` 与 `linux/arm64` manifest。
- 原 feature branch/ref 未删除，便于恢复和审计。

以上证据满足 `AC-09`，本 child 可以归档；最终审计 child 的 entry gate 已被解除，
但本次收尾不提前启动它。
