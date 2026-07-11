# Task 7 预合并最终验证

## Candidate

- Base: `origin/main@dfe11a8d5305e0318382acdc3c408dec8a8f7ae9`。
- 已提交实现截至 `88d8ed23b03579db6bbff553c492c4d929bf5a2c`；该 commit 包含提交前 review 发现的受控 Tabs 中间 render 时序 RED/GREEN 修复，之前的 Asset light-theme context contrast 修复位于 `30a99dbd755bcffbfea9e14d8c43561b04a9b190`。
- Runtime: Node `22.23.1`；Chrome `150.0.7871.114`；axe-core `4.10.3`。
- 数据：production `web/dist`；仓库 `asset-workflows` fixture，缺失 endpoint 使用 `observability-support` fallback。server/CDP harness 位于 `/tmp`，未修改 package、lockfile 或 CI。

## Focused Evidence

- 实现阶段 focused integration：13 files / 146 tests，lint 与 production build 通过。
- light-theme 扩展矩阵首次准确复现 Asset group context 的 `color-contrast` serious：`#64748b` 位于 `#fbf7f5` 上仅 4.46:1，目标是三个 11.5px group context span。
- RED source contract 先因仍为 `color:var(--text-secondary)` 失败；最小 owner 修复改为 `color-mix(in srgb,var(--text-secondary) 90%,var(--text-primary))` 后，`indexCssContract.test.ts` 6/6 通过。
- 提交前 review 增加 Tabs 旧 value 中间 rerender 用例，先准确复现 premature `scrollIntoView`；pending ref 改为同时保存 target value 后，旧 value render 不清空/不滚、目标 value commit 才滚，`Tabs.test.tsx` 9/9 通过。
- clean full gate（2026-07-11）：`npm ci --include=dev`、lint、90 个 Vitest files / 673 tests、production TypeScript/Vite build、`npm audit --include=dev`（0 vulnerabilities）与 `env -u NODE_ENV make verify` 全部 exit 0。
- Task 7 与 parent Trellis validate 通过；`git diff --check` 通过；package、lockfile、workflow diff 为空；未发现新增 debug log、eslint/TS suppression 或 `as any`。

## Responsive Contracts At 390x900

| Surface | Final computed evidence | Result |
| --- | --- | --- |
| Dashboard primary action | y `271.97..371.56` | 保持在首屏 |
| Settings End target `高级` | `white-space:nowrap`; client/scroll height `30/30`; tablist `scrollLeft=16`; target rect `310..364` within list `68..368` | End/Home 最终 commit 后完整可见 |
| Asset support commands | 四项均高 `72px`; title client/scroll width 均 `127/127`; `overflow:visible`; `text-overflow:clip`; `white-space:normal` | 四项完整、无 ellipsis，hit-test 通过 |
| Provider section | client/scroll width `298/298` | heading/toolbar 固定，section 不承担横滚 |
| Provider table region | `role=region`; name=`服务商与入口`; describedby=`provider-directory-table-hint`; `tabIndex=0`; client/scroll `240/1000`; `overflow-x:auto` | 局部、具名、可聚焦滚动 owner |
| Provider `组合决策` | client/scroll `60/60`; `overflow:visible`; `text-overflow:clip` | 文本完整，hit-test 通过 |
| Provider ArrowRight | focus region 后 `scrollLeft: 0 → 6` | 键盘滚动通过 |

## Route And Viewport Matrix

Routes：`/`、`/settings`、`/vps`、`/asset-decisions`、`/providers`、`/subscriptions`、`/monitoring`、`/targets`、`/events`。

| Viewport | Routes | document overflow | blank surface | clipped shell controls |
| --- | ---: | ---: | ---: | ---: |
| `1440x1000` | 9/9 | 0 | 0 | 0 |
| `1024x768` | 9/9 | 0 | 0 | 0 |
| `390x900` | 9/9 | 0 | 0 | 0 |

27 个组合的 `documentWidth` 均等于 `innerWidth`；关键末尾命令 hit-test 未被 fixed/sticky surface 遮挡。

## Axe And Diagnostics

- settled scans：`/`、`/settings`、`/vps`、`/settings@390x900`、`/asset-decisions@390x900`、`/providers@390x900` 的 serious=0、critical=0。
- 三主题矩阵：VPS、Asset Decisions、Providers 分别在 `theme-houfeng-dark`、`theme-houfeng-light`、`theme-classic-dark` 共 9 次扫描，serious/critical 全部为 0。
- diagnostics：console errors=0、runtime exceptions=0、CSP violations=0、HTTP >=400=0、network loading failures=0。
- 没有禁用 axe rule；每次主题切换均等待有限 animation/transition settled 后扫描。

## Evidence Boundary

- 这是固定版本本地 Chromium/CDP + fixture 的预合并证据，不是 Task 10 的 repository Playwright/axe CI gate。
- fixture 不代表真实认证 Center/PostgreSQL、真实 Provider/Asset 数据或 staging；Task 10 与父任务仍必须完成 `workflow_dispatch` + GitHub staging environment 的认证 smoke。
- implementation PR 合并、release tag/image、发布镜像 smoke 与最终 Gate B 同版结论由后续 archive/evidence PR 记录；本文件不提前宣称它们通过。
- 预合并实现与证据通过 ready PR #360 交付；required checks、merge 与 post-merge 状态以 GitHub 记录为准。
