# 窄视口核心流程实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. 本任务固定 inline execution，不分派子代理。

**Goal:** 让 390px 下 Tabs、Asset 辅助入口和 Provider 决策/宽表完整可达，并把横向滚动限制在有语义的局部 owner。

**Architecture:** 保持 Task 6 的 React 组件 API 和业务状态不变；用 owner CSS 建立“Tabs 自滚、命令不裁切、table wrapper 自滚”三种策略。Vitest/source contract 固化 markup 与 CSS，repo 外 Chromium/axe 验证真实几何；正式 Playwright/CI gate 留给 Task 10。

**Tech Stack:** React 19、TypeScript strict、纯 CSS/BEM、Vitest 4、Testing Library、Chromium CDP、repo 外 axe-core 4.10.3。

---

## 1. Preflight And RED Evidence

### Files

- Read: `.trellis/tasks/archive/2026-07/07-10-frontend-accessibility-contracts/research/final-verification.md`
- Read: `web/src/components/atoms/Tabs.tsx`
- Read: `web/src/styles/partials/atoms.css`
- Read: `web/src/styles/partials/legacy-assets.css`
- Read: `web/src/styles/partials/legacy-provider.css`
- Read: `web/src/styles/partials/legacy-misc.css`
- Read: `web/src/pages/ProvidersPage.tsx`
- Record: `.trellis/tasks/07-10-frontend-responsive-workflows/research/baseline.md`

- [x] **Step 1: Confirm dependency and branch invariants**

```bash
git branch --show-current
git status --short --branch
git merge-base HEAD origin/main
python3 ./.trellis/scripts/task.py validate .trellis/tasks/07-10-frontend-responsive-workflows
```

Expected: branch `codex/frontend-responsive-workflows`，clean；merge-base `dfe11a8...`；Task 6 位于 archive，Task 7 为当前 `in_progress`。

- [x] **Step 2: Confirm exact inventories before edits**

```bash
rg -n "tabs--pill|tabs--underline" web/src/styles/partials/atoms.css
rg -n "asset-decision-support-strip" web/src/styles/partials/legacy-assets.css web/src/styles/partials/legacy-misc.css
rg -n "provider-directory-entry-link|provider-directory-panel|provider-directory-table" web/src/pages/ProvidersPage.tsx web/src/styles/partials/legacy-provider.css
rg -n "page-panel--scroll-x" web/src/pages web/src/components
```

Expected: Asset support-strip breakpoint declarations只在既有 owner 中出现；Provider section 仍带 page-level scroll class；无并行 Task 8/9 改动。

- [x] **Step 3: Preserve browser RED measurements**

在 repo 外 CDP harness 中断言并记录以下当前失败：

```js
assert(settingsTab.whiteSpace !== 'nowrap')
assert(assetTitle.scrollWidth > assetTitle.clientWidth)
assert(assetTitle.textOverflow === 'ellipsis')
assert(providerDecision.scrollWidth > providerDecision.clientWidth)
assert(providerPanel.role === null && providerPanel.tabIndex === -1)
```

同时记录九路由 390px document overflow 为 0、Dashboard primary action `bottom < 900`；RED 不能是 fixture 404、console error 或空白页。

**Stop condition:** dependency 未归档、base 漂移、API fixture error、已有其他分支修改同 owner 或 RED 与 baseline 不一致时停止并更新 plan，不能猜测实现。

## 2. CSS And Markup Contract Tests (RED)

### Files

- Modify: `web/src/styles/indexCssContract.test.ts`
- Modify: `web/src/pages/ProvidersPage.test.tsx`

- [x] **Step 1: Add final-cascade CSS helpers**

在 `indexCssContract.test.ts` 保留 `resolveImported()`，新增收集同 selector 全部 rule body 的 helper：

```ts
function ruleBodies(css: string, selector: string): string[] {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return [...css.matchAll(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`, 'g'))]
    .map((match) => match[1] ?? '')
}
```

- [x] **Step 2: Write Tabs and Asset RED assertions**

```ts
it('keeps responsive tabs and asset commands readable within their owner', () => {
  expect(compact(ruleBody(indexCss, '.tabs--pill'))).toContain('overflow-x:auto')
  expect(compact(ruleBody(indexCss, '.tabs--pill .tab'))).toContain('flex:0 0 auto')
  expect(compact(ruleBody(indexCss, '.tabs--pill .tab'))).toContain('white-space:nowrap')

  const title = compact(ruleBody(indexCss, '.asset-decision-support-strip__title'))
  expect(title).toContain('white-space:normal')
  expect(title).not.toContain('text-overflow:ellipsis')
  expect(ruleBodies(indexCss, '.asset-decision-support-strip')
    .filter((body) => body.includes('grid-template-columns'))).toHaveLength(1)
})
```

Expected RED: 当前 pill 无 overflow/nowrap，Asset title 使用 ellipsis，grid breakpoint 多于一套。

- [x] **Step 3: Write Provider RED assertions**

在现有 Provider happy-path test 中加入：

```tsx
const title = screen.getByRole('heading', { name: '服务商与入口' })
const region = screen.getByRole('region', { name: '服务商与入口' })
expect(region).toHaveAttribute('tabindex', '0')
expect(region).toHaveAttribute('aria-labelledby', title.id)
expect(screen.getByText('横向滚动查看完整列')).toHaveAttribute('id')
expect(title.closest('section')).not.toHaveClass('page-panel--scroll-x')
expect(screen.getByRole('link', { name: '查看 Hetzner 服务商组合决策' }))
  .toHaveClass('provider-directory-entry-link--decision')
```

Expected RED: region/hint/id/modifier 不存在，section 仍拥有 `page-panel--scroll-x`。

- [x] **Step 4: Run focused RED**

```bash
NODE_ENV=test PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test -- --run \
  src/styles/indexCssContract.test.ts \
  src/pages/ProvidersPage.test.tsx
```

RED 必须只来自上述 responsive contract assertion；import、syntax、fixture 或既有测试失败不算有效 RED。

## 3. Tabs And Asset Commands (GREEN)

### Files

- Modify: `web/src/styles/partials/atoms.css`
- Modify: `web/src/styles/partials/legacy-assets.css`
- Modify: `web/src/styles/partials/legacy-misc.css`
- Test: `web/src/styles/indexCssContract.test.ts`

- [x] **Step 1: Implement Tabs local overflow**

在现有 variant owner 中加入：

```css
.tabs--underline{max-width:100%;overflow-x:auto;overscroll-behavior-x:contain;scrollbar-gutter:stable}
.tabs--underline .tab{flex:0 0 auto;white-space:nowrap}
.tabs--pill{display:flex;width:fit-content;max-width:100%;overflow-x:auto;overscroll-behavior-x:contain;scrollbar-gutter:stable}
.tabs--pill .tab{flex:0 0 auto;white-space:nowrap}
```

保留原 selected/hover/focus/color/radius；不要新增 scrollbar hiding 或 JavaScript scroll handler。

- [x] **Step 2: Remove Asset title clipping**

```css
.asset-decision-support-strip__title{
  min-width:0;
  white-space:normal;
  overflow:visible;
  text-overflow:clip;
  overflow-wrap:anywhere;
}
```

- [x] **Step 3: Consolidate Asset breakpoints**

在唯一 `@media(max-width:920px)` owner 中保留：

```css
.asset-decision-support-strip{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}
.asset-decision-support-strip__item{
  display:grid;
  grid-template-columns:minmax(0,1fr);
  align-content:space-between;
  justify-content:stretch;
  min-height:72px;
  padding:var(--space-2);
}
.asset-decision-support-strip__item .badge{
  justify-self:start;
  max-width:100%;
  white-space:normal;
  text-align:left;
}
```

从两组 640px media rule 删除相同 support-strip selectors；不要添加新的末尾 override。

- [x] **Step 4: Run focused GREEN and build**

```bash
NODE_ENV=test PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test -- --run \
  src/styles/indexCssContract.test.ts \
  src/components/atoms/Tabs.test.tsx \
  src/components/atoms/SegmentedControl.test.tsx \
  src/pages/SettingsPage.test.tsx \
  src/pages/AssetDecisionsPage.test.tsx
NODE_ENV=production PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run build
```

Expected: source contract green；Settings/Asset 业务测试无 state/ARIA 回归；build 无 CSS/Vite warning。

**Commit boundary:** `fix(web): keep narrow tabs and asset commands readable`。

**Rollback:** 只回滚 Tabs/Asset owner commit；不得恢复 ellipsis 或改变 Task 6 component API。

## 4. Provider Local Table Region (GREEN)

### Files

- Modify: `web/src/pages/ProvidersPage.tsx`
- Modify: `web/src/pages/ProvidersPage.test.tsx`
- Modify: `web/src/styles/partials/legacy-provider.css`
- Test: `web/src/styles/indexCssContract.test.ts`

- [x] **Step 1: Move overflow from section to table wrapper**

实现以下稳定 id 与 DOM 关系：

```tsx
<section className="page-panel provider-directory-panel">
  <div className="section-heading section-heading--inline">
    <div>
      <p className="section-heading__eyebrow">Providers</p>
      <h2 id="provider-directory-table-title" className="section-heading__title">服务商与入口</h2>
    </div>
  </div>
  <div className="provider-directory-toolbar">...</div>
  <p id="provider-directory-table-hint" className="provider-directory-table-hint">
    横向滚动查看完整列
  </p>
  <div
    className="provider-directory-table-scroll"
    role="region"
    aria-labelledby="provider-directory-table-title"
    aria-describedby="provider-directory-table-hint"
    tabIndex={0}
  >
    <DataTable<ProviderDirectoryRow> ... />
  </div>
</section>
```

- [x] **Step 2: Preserve full link text**

把 entry column width 从 `176px` 调整为 `232px`，decision link 使用：

```tsx
<Link
  className="provider-directory-entry-link provider-directory-entry-link--decision"
  to={`/asset-decisions?view=provider&renew_within_days=30&provider_id=${providerID}`}
  aria-label={`查看 ${row.provider.name} 服务商组合决策`}
>
  组合决策
</Link>
```

href、条件与 aria-label 保持不变。

- [x] **Step 3: Implement Provider owner styles**

```css
.provider-directory-table{min-width:1000px;table-layout:fixed}
.provider-directory-table-hint{margin:0;color:var(--text-muted);font-size:var(--type-small-size)}
.provider-directory-table-scroll{max-width:100%;min-width:0;overflow-x:auto;overflow-y:hidden;scrollbar-gutter:stable;border-radius:var(--radius-3)}
.provider-directory-table-scroll:focus-visible{outline:var(--border-w-strong) solid var(--accent);outline-offset:var(--space-1)}
.provider-directory-entry-links{flex-wrap:wrap;overflow:visible}
.provider-directory-entry-link{max-width:none;overflow:visible;text-overflow:clip}
.provider-directory-entry-link--decision{padding-inline:var(--space-2)}
```

- [x] **Step 4: Run focused GREEN**

```bash
NODE_ENV=test PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test -- --run \
  src/pages/ProvidersPage.test.tsx \
  src/styles/indexCssContract.test.ts \
  src/components/atoms/DataTable.test.tsx \
  src/security/semanticInteractionContract.test.ts
NODE_ENV=production PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run build
```

Expected: Provider region/links/tests green；DataTable 与 semantic allowlist 数量不变。

**Commit boundary:** `fix(web): isolate provider table overflow`。

**Rollback:** Provider commit 可独立回滚；若 width 需调整，保留 region semantics 与 visible link contract。

## 5. Focused Integration Gate

- [x] **Step 1: Run direct regression set**

```bash
NODE_ENV=test PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test -- --run \
  src/styles/indexCssContract.test.ts \
  src/components/atoms/Tabs.test.tsx \
  src/components/atoms/SegmentedControl.test.tsx \
  src/components/atoms/DataTable.test.tsx \
  src/pages/SettingsPage.test.tsx \
  src/pages/AssetDecisionsPage.test.tsx \
  src/pages/ProvidersPage.test.tsx \
  src/pages/DashboardPage.test.tsx \
  src/security/cspContract.test.ts \
  src/security/semanticInteractionContract.test.ts
```

- [x] **Step 2: Static scope review**

```bash
git diff -- web/src
rg -n "max-width:48px|text-overflow:ellipsis|overflow:hidden" web/src/styles/partials/legacy-provider.css web/src/styles/partials/legacy-assets.css
rg -n "asset-decision-support-strip" web/src/styles/partials/legacy-misc.css
git diff -- web/package.json web/package-lock.json .github/workflows
```

Expected: 被审查的两个命令无裁切；Asset breakpoint 只有一套；package/lockfile/workflow diff 为空；无 unrelated formatting、API 或 Task 8/9 refactor。

## 6. Local Chromium And Axe Gate

Task 10 尚未提供 repository Playwright，不运行不存在的 `npm run test:e2e`。

- [x] **Step 1: Build and start production fixture**

```bash
NODE_ENV=production PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run build
```

用 repo 外 server 对 `web/dist` 发送仓库严格 CSP，并组合 `asset-workflows` 与 `observability-support` fixture；axe-core 固定 `4.10.3`，不写 package/lockfile。

- [x] **Step 2: Verify key 390px contracts**

```js
assert(settingsTab.whiteSpace === 'nowrap')
assert(settingsTab.scrollHeight <= settingsTab.clientHeight + 1)
assert(assetTitle.scrollWidth <= assetTitle.clientWidth + 1)
assert(assetTitle.textOverflow !== 'ellipsis' && assetTitle.overflow !== 'hidden')
assert(assetButton.bottom - assetButton.top >= 40)
assert(providerDecision.scrollWidth <= providerDecision.clientWidth + 1)
assert(providerRegion.role === 'region' && providerRegion.tabIndex === 0)
assert(providerRegion.ariaLabel === '服务商与入口')
```

Tab 到 Provider region 后派发 ArrowRight，断言 `scrollLeft` 增加；Settings 用 End/Home 验证 focus target 自动进入 tablist 可视区。

**Implementation finding:** 初版只依赖同步 `focus()` 的浏览器隐式滚动；Settings 的受控 panel commit 后父级滚动条发生变化，End 目标再次部分离开 tablist。最终实现把目标暂存到 ref，并在 value/panel commit 后由 `useLayoutEffect` 执行 nearest/nearest `scrollIntoView`；unit test 明确断言 rerender 前不滚、rerender 后滚，且不使用 rAF。

- [x] **Step 3: Run nine-route viewport matrix**

Routes：`/`、`/settings`、`/vps`、`/asset-decisions`、`/providers`、`/subscriptions`、`/monitoring`、`/targets`、`/events`。

Viewports：`1440x1000`、`1024x768`、`390x900`。

每个组合断言：

```js
assert(documentWidth <= innerWidth + 1)
assert(bodyTextLength > 100)
assert(consoleErrors.length === 0)
assert(exceptions.length === 0)
assert(cspViolations.length === 0)
assert(failedResponses.length === 0)
assert(loadingFailures.length === 0)
```

Dashboard `.dashboard-primary-action` 的 `bottom < 900`；关键末尾命令 `elementFromPoint()` 命中自身或后代，不被 fixed/sticky surface 遮挡。

- [x] **Step 4: Run settled axe**

扫描 `/settings`、`/asset-decisions`、`/providers`；serious=0、critical=0，不禁用 rule，不把结果描述成 Task 10 formal CI gate。

- [x] **Step 5: Record evidence**

创建 `.trellis/tasks/07-10-frontend-responsive-workflows/research/final-verification.md`，记录 commit、browser/axe 版本、fixture、27 个 route/viewport 结果、关键 computed metrics、keyboard scroll、diagnostics、limitations；不提交截图或 bulk raster。

**Failure rule:** 任何裁切、键盘滚动失败、document overflow、serious/critical 或诊断错误都回到对应 RED test/owner 修复；不得以 Task 10 later 跳过。

## 7. Spec Update And Full Gate

- [x] **Step 1: Update executable specs after behavior stabilizes**

使用 `trellis-update-spec` 更新：

- `.trellis/spec/web/styling-guidelines.md`：command visible text、Tabs local overflow、Asset 两列按钮与 Provider table region owner。
- `.trellis/spec/web/component-conventions.md`：宽表 wrapper 的 region/name/hint/tabIndex 与 heading/toolbar boundary。
- `.trellis/spec/web/quality-guidelines.md`：local CDP responsive evidence 与 Task 10 formal gate 分层。
- parent Gate B：只补 Task 7 evidence 后才标记 Gate B integrated pass。

- [x] **Step 2: Run clean full gate**

```bash
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web ci --include=dev
NODE_ENV=test PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run lint
NODE_ENV=test PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test -- --run
NODE_ENV=production PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run build
PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web audit --include=dev
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify
python3 ./.trellis/scripts/task.py validate .trellis/tasks/07-10-frontend-responsive-workflows
python3 ./.trellis/scripts/task.py validate .trellis/tasks/07-10-frontend-comprehensive-audit
git diff --check
```

最低基线：90 test files / 669 tests；package/lockfile 无变化；npm audit 0 vulnerabilities。

- [x] **Step 3: Final self-review**

逐条映射 PRD AC、检查三 owner commit、API/wire/route 零变化、无 debug/suppressions/unsafe cast、无 Task 8/9/10 越界。

## 8. Commit, PR, Release And Archive

Recommended commits：

1. `docs(task): detail responsive workflow repair`
2. `fix(web): keep narrow tabs and asset commands readable`
3. `fix(web): isolate provider table overflow`
4. `fix(web): close responsive accessibility gaps`（受控 commit 后滚动、折叠 Sidebar 稳定名称、critical info badge 对比度）
5. `fix(web): preserve light-theme asset context contrast`
6. `docs(spec): record responsive overflow contracts`

- [ ] push `codex/frontend-responsive-workflows`，创建 ready PR `fix(web): close narrow viewport workflow gaps`。
- [ ] 监控 PR go/web/docker/GitGuardian；同分支本地复现失败，不 force-push 猜测。
- [ ] checks green 后通过 GitHub PR merge；监控 main CI 与 Release Please。
- [ ] 这是 release-worthy responsive fix：release PR checks green 后合并，监控 GitHub Release、agent assets、`publish-images` 与 multi-arch digest。
- [ ] 从发布镜像 `/app/web/dist` 重跑关键 390px browser/axe smoke，记录 release commit/tag/digest。
- [ ] 独立 archive/evidence PR 归档 Task 7，监控 post-merge main CI；完成后才启动 Task 8。

## 9. Rollback Matrix

| Failure | Rollback owner | Must retain |
| --- | --- | --- |
| Settings/通用 pill 桌面回归 | Tabs/Asset commit 中 Tabs hunk | Task 6 ARIA/keyboard contract |
| Asset 两列密度不可接受 | Tabs/Asset commit 中 Asset hunk | visible title 与 >=40px target |
| Provider table density回归 | Provider CSS width hunk | local region semantics 与完整 link |
| Provider keyboard scroll失效 | Provider wrapper hunk | heading/toolbar 固定边界 |
| browser/axe failure | offending owner commit | RED test/evidence，不禁用规则 |

任何 rollback 后重跑 focused、full、browser 三层门。禁止恢复 hidden/ellipsis、page-level table scroll、optional visible text 或删除测试换取绿色。
