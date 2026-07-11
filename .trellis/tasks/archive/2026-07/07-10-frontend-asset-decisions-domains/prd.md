# Asset Decisions 领域拆分

## Goal

在不改变后端 wire contract、请求 method/path/body、URL、DOM workflow、用户文案或 mutation 语义的前提下，删除 2,705 行 `AssetDecisionsPageContent.tsx` 总控与 wrapper loophole。让 URL、portfolio、groups、manual groups、templates、records、renewal queue 各有明确 owner，使后续修复能在一个领域内测试、评审和回滚。

## User Value

- 资产决策页继续提供完全相同的组合扫描、场景、记录和单台续费流程。
- 一个领域的加载或写入失败不再需要理解 70+ hook 的全页状态，降低修复引入跨域回归的概率。
- 结构门直接约束真实模块集合和依赖方向，不能再通过改名或 wrapper 搬运绕过。

## Confirmed Facts

- 基线是 `origin/main@a57458dc30ca9dc2ee4333563fdd6ce19e90a6e5`，完整 `make verify` 通过：90 个 Vitest 文件、673 个测试。
- route wrapper 5 行，真实 Content 2,705 行；Content 有 73 个 hook 调用，其中 12 个 effect。
- 3,069 行 page test 包含 33 个业务 workflow 和 3 个结构守护；现有 800 行守护只扫描 wrapper。
- 默认首屏发 11 个 GET；首屏 deep link 各额外发一个 detail GET；挂载后 open-key 变化还会重跑四个 filtered GET。
- 只有单台续费更新使用全局 `refreshToken`；其他 mutation 依靠返回 representation 局部合并，并在切换到新实体时读取新 detail。
- 现有 `components/*`、`modal-content/*`、`businessLogic.ts`、`tableColumns.tsx` 等已提供可复用的展示/纯逻辑边界；真正缺失的是 controller 和依赖方向。
- 详细证据、请求表与 mutation 表见 `research/baseline.md`。

## Requirements

### R1. 行为与网络兼容

- 先以 characterization tests 冻结默认 11 GET、interactive open/close 的四个 filtered reload、partial `allSettled`、四类 deep link、URL 保留规则、所有 write payload、成功后的兼容刷新集合和嵌套确认焦点。
- 保持 `lib/api.ts` helper 的 method/path/body、snake_case 类型和响应 shape 不变。
- 保持当前可见文案、roles/names、CSS classes、modal/tab workflow 和三种 viewport 下的可达性；本任务不做 UI 重设计。
- 读取失败必须继续是局部 unavailable，不能用 `[]`/`null` 推断真实 empty、稳定、无缺口或已闭环。

### R2. 七个 owner

- `useAssetDecisionRouteState` 是唯一调用 `useSearchParams` / `useNavigate` 的模块；URL 是 open selection 的唯一真相。
- `useAssetDecisionPortfolio` 拥有 overview 读取和 overview remote state。
- `useAssetDecisionGroups` 拥有自动组列表、自动组 detail 与 group-local panel state。
- `useAssetDecisionManualGroups` 拥有 manual list/detail、VPS candidate catalog、manual mutation 和 member draft/error/saving。
- `useAssetDecisionTemplates` 拥有 template list/detail、create/patch、template-local draft/error/saving。
- `useAssetDecisionRecords` 拥有 record list/detail、record draft、create/status/follow-up mutation 与对应状态。
- `useAssetDecisionRenewalQueue` 拥有 subscription/VPS queue reads、queue filter、selected VPS、renewal draft 和 `updateVPSAsset`。
- 七个 hook 都只返回 `{state, commands}`；不得把 `React.Dispatch` 或内部 `setState` 暴露给 page/展示层。

### R3. 显式跨域协调

- page 是薄 composition coordinator：组合七个 controller、纯 model、两个 workbench 和五个 modal；不得 import `lib/api`，不得出现读取/mutation effect 堆积。
- 普通 mutation 用 API 返回值更新 owner，并以 typed result 让 page 完成“关闭 A、打开 B”的 route 协调；controller 不直接写另一个 controller 的 state。
- 单台续费成功发出 typed `renewal-decision-saved` invalidation event；event → 六个读取域 revision 的映射是单一 owner，并保持当前兼容刷新集合。
- 不增加全局 Context、event bus、第三方状态库或服务端缓存层。

### R4. 展示与测试边界

- 展示 component/modal 不 import `lib/api` 或 controller；只接收 typed state、value 和 semantic command props。
- 复用现有 `tableColumns.tsx`/`renderHelpers.tsx`，将仍闭包在总控中的 record draft/follow-up/execution JSX 拆为受控展示组件，避免把 UI 搬进 controller。
- 3,069 行综合测试按 route、六个业务域、modal/pure model 和 architecture ownership 拆分；共享 fixture 只有一个 owner。
- 新 production source contract 使用 TypeScript compiler AST，不用正则解析 import/JSX。

### R5. 结构门

- 删除 `AssetDecisionsPageContent.tsx`；Asset Decisions production glob 内不得存在其他 `*PageContent`。
- `AssetDecisionsPage.tsx` 不 import API，不调用 `useSearchParams`/`useEffect`，只做装配。
- controller API symbol 必须落在批准的 owner 白名单；controller 不 import展示模块，展示模块不 import controller/API，controller 之间不互相 import。
- 行数预算覆盖真实 glob：route page ≤400 行、每个 controller ≤600 行、任一 Asset Decisions production 文件 ≤800 行；controller 单文件 `useEffect` ≤3。
- 行数只是 fail-closed 边界，依赖方向和 owner 白名单才是主要质量门。

## Dependency And Delivery Scope

- 依赖 Task 2 `frontend-modal-stack-focus`；该依赖和 Gate A 已合并并验证。
- 从最新受保护 `origin/main` 的独立 `codex/frontend-asset-decisions-domains` 分支/worktree 实施，独立 PR、CI、发布、镜像验证和归档。
- Task 8 完成并归档后才启动 Task 9；Task 10 等 Task 8/9 完成后继续。

## Out Of Scope

- 不修改 Go、数据库、API helper 签名或 `web/src/lib/types.ts` wire shape。
- 不新增 `patchAssetDecisionManualGroupMember` UI、业务动作、路由、文案、样式或筛选项。
- 不优化/减少 renewal invalidation 请求；网络优化另立任务并提供产品/后端证据。
- 不改 package/lockfile、CI、coverage、Playwright/axe 持久门；这些属于 Task 10。
- 不做 CSS owner 化或删除遗留规则；这些属于 Task 9。

## Acceptance Criteria

### Behavior

- [x] 默认首屏、partial failure、filters、tabs、secondary workbench、四类 deep link、close/back/forward 与 URL 参数保留均由回归测试覆盖且行为不变。
- [x] 所有现有 mutation 的 method/path/body、确认步骤、错误归属、本地合并、打开实体和兼容 refresh set 均有测试。
- [x] 自动组、manual group、template、record、renewal queue 的现有核心 workflow 全部通过；测试数不低于 673，除非有证据地拆分/替换同一断言。

### Architecture

- [x] 七个 hook 均存在并统一暴露 `{state, commands}`，不暴露 raw setter。
- [x] `useAssetDecisionRouteState` 是唯一 search-param owner；跨域只通过 typed result/invalidation 协调。
- [x] route page 无 API/effect/mutation 细节，展示层无 API/controller import。
- [x] 删除 `AssetDecisionsPageContent.tsx`，AST contract 无 `*PageContent` 替身且所有预算/方向/owner 断言通过。

### Validation

- [x] focused controller/page/modal/model/architecture tests 通过。
- [x] `env -u NODE_ENV make verify-web`、`npm --prefix web audit --include=dev`、`git diff --check` 通过。
- [x] production dist + `mock-api asset-workflows` 在 Chromium `1440x1000`、`1024x768`、`390x900` 下无 page/console/CSP/network error、无页面横向溢出、关键文字不裁切。
- [x] 浏览器完成：自动组打开/关闭、auto→manual、manual member remove nested confirmation、template archive nested confirmation、record status/follow-up、single VPS renewal；键盘 Tab/Escape/focus restore 和 body scroll lock 保持 Task 2 合同。
- [x] PR CI、合并后 main CI、Release Please、GitHub Release 和 multi-arch image 发布完成；发布镜像 `/app/web/dist` 重跑 Asset Decisions browser gate。

## Open Questions

无。产品语义和执行边界已由父任务、现有 spec 与用户的 inline-execution 决策确定；实施前只等待本版 `prd.md`、`design.md`、`implement.md` 的启动 review。
