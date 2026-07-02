# 资产组合决策页面全面 UX 重构实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Codex inline 执行时先使用 `trellis-before-dev`，并按本文件任务顺序 TDD 实施；如切换到 superpowers 执行计划，使用 `superpowers:executing-plans`。本任务当前不使用子代理，除非用户后续明确切换。

**Goal:** 重构 `/asset-decisions` 页面主体，让当前判断、稳定态、决策组扫描和辅助入口层级清晰，保留既有弹窗分层与写入能力。

**Architecture:** 数据获取、URL 参数和写入 contract 保持在 `AssetDecisionsPage.tsx`；先用测试锁住页面主体、稳定态、深链和弹窗保护，再调整 lead model、辅助入口渲染和局部 CSS。辅助入口保持受控纯展示组件，不读取路由、不发请求。

**Tech Stack:** React 19、TypeScript、React Router、Vitest、Testing Library、现有 `Badge` / `Button` / `PageStateView` / `Tabs` / `DataTable` 原语、`web/src/index.css`。

---

## Phase Gate

- 本文件完成后仍停留在 Trellis `planning`。
- 只有用户审查并同意 `prd.md`、`design.md`、`implement.md` 后，才能运行 `python3 ./.trellis/scripts/task.py start .trellis/tasks/07-02-asset-decisions-comprehensive-ux` 进入实施。
- 进入实施前执行 `trellis-before-dev`，读取 web 相关 spec。
- 本计划不要求后端、API schema、数据库、依赖或路由注册变化。

## Files

- Modify: `web/src/pages/AssetDecisionsPage.test.tsx`
  - 增加页面主体英文噪音 helper、稳定态 fixture、紧凑辅助入口和深链保护测试。
  - 更新旧测试里仍期望 `PORTFOLIO` / `AUTO GROUP` 等英文 marker 的断言。
- Modify: `web/src/pages/AssetDecisionsPage.tsx`
  - 调整 `AssetDecisionNextWorkKind` / `AssetDecisionPortfolioLead` / `deriveNextWorkItems` / `buildPortfolioLead` / `buildSecondaryNavItems`。
  - 更新页面主体 JSX：当前判断、事实条、辅助入口工具条、决策组扫描和空状态文案。
  - 中文化当前页面主体和次级区英文 eyebrow。
- Modify: `web/src/pages/asset-decisions/AssetDecisionSecondaryNav.tsx`
  - 将四张大卡收敛为紧凑工具条；将 nav aria label 更新为 `资产决策辅助入口`，保留按钮 label 和 active class contract。
- Modify: `web/src/pages/asset-decisions/types.ts`
  - 保留现有 `eyebrow` / `summary` 字段以降低改动风险，但字段值必须是中文短文本，不再承载英文 source label 或长说明。
- Modify: `web/src/index.css`
  - 调整 `asset-decision-command-summary`、`asset-decision-command-summary__facts`、`asset-decision-secondary-nav*`、页面主体移动端 media rules。
- Do not modify:
  - API client contract、后端 schema、写入 payload、路由注册、依赖版本。

## Task 1: Add Page-Level Regression Tests

**Files:**
- Modify: `web/src/pages/AssetDecisionsPage.test.tsx`

- [ ] **Step 1: Add helper for page English noise**

Add near `expectDetailDirectoryDensity`:

```tsx
function expectNoAssetDecisionPageEnglishNoise(container: HTMLElement = document.body) {
  expect(container).not.toHaveTextContent(/\b(?:PORTFOLIO|RENEWAL|CLOSED LOOP|EVIDENCE|WORKBENCH|SCENARIO|SCENARIOS|DECISION MEMORY|SINGLE VPS QUEUE|AUTO GROUP|AUX QUEUE)\b/)
}
```

- [ ] **Step 2: Update the default page test to expect Chinese-only chrome**

In `renders the portfolio-first workbench without flattening secondary areas`:

```tsx
expectNoAssetDecisionPageEnglishNoise()
expect(screen.queryByText('PORTFOLIO')).not.toBeInTheDocument()
expect(screen.getByLabelText('资产决策辅助入口')).toBeInTheDocument()
expect(screen.getByRole('heading', { name: '决策组扫描' })).toBeInTheDocument()
expect(screen.getByRole('button', { name: '打开记录' })).toBeInTheDocument()
expect(screen.getByRole('button', { name: '打开场景' })).toBeInTheDocument()
expect(screen.getByRole('button', { name: '查看续费' })).toBeInTheDocument()
expect(screen.getByRole('button', { name: '查看单台队列' })).toBeInTheDocument()
expect(screen.queryByRole('heading', { name: '已保存组合决策' })).not.toBeInTheDocument()
expect(screen.queryByRole('heading', { name: '场景工作区' })).not.toBeInTheDocument()
expect(screen.queryByRole('heading', { name: '续费事实' })).not.toBeInTheDocument()
expect(screen.queryByRole('heading', { name: '单台辅助队列' })).not.toBeInTheDocument()
```

Remove or replace old expectations that require visible `PORTFOLIO` or `AUTO GROUP`.

- [ ] **Step 3: Add stable state fixture test**

Add a test after the default page test:

```tsx
it('shows a quiet stable state without promoting templates or manual groups', async () => {
  const fetchMock = vi.fn()
  mockInitialWorkbench(fetchMock, {
    overviewBody: overview({
      group_count: 0,
      member_vps_count: 0,
      needs_decision_count: 0,
      renewal_group_count: 0,
      region_group_count: 0,
      provider_group_count: 0,
      cost_group_count: 0,
      evidence_group_count: 0,
      top_groups: [],
      type_counts: {},
      view_counts: {},
    }),
    groupsBody: [],
    recordsBody: [],
    manualGroupsBody: [manualGroupSummary({ title: '欧洲主备手工组合', status: 'active' })],
    templatesBody: [scenarioTemplate({ title: '主备取舍模板', status: 'active' })],
    renewalEvidenceBody: [],
    subscriptionsBody: [],
    unreviewedBody: [],
    migrateBody: [],
    cancelBody: [],
  })
  vi.stubGlobal('fetch', fetchMock)

  render(
    <MemoryRouter>
      <AssetDecisionsPage />
    </MemoryRouter>,
  )

  const commandSummary = await screen.findByLabelText('资产组合决策当前判断')
  expect(within(commandSummary).getByRole('heading', { name: '当前没有需要处理的组合决策' })).toBeInTheDocument()
  expect(within(commandSummary).queryByRole('button', { name: /处理|使用模板|继续组合|打开决策组/ })).not.toBeInTheDocument()
  expect(within(commandSummary).queryByText(/主备取舍模板|欧洲主备手工组合/)).not.toBeInTheDocument()
  expect(screen.getByRole('heading', { name: '当前视图暂无决策组' })).toBeInTheDocument()
  expect(screen.queryByRole('heading', { name: '场景工作区' })).not.toBeInTheDocument()
  expectNoAssetDecisionPageEnglishNoise()
})
```

- [ ] **Step 4: Verify tests fail for the intended reasons**

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Expected before implementation:

- FAIL because current page renders English labels.
- FAIL because stable state promotes a template or manual group as the main work item.

## Task 2: Add Support Strip and Deep-Link Tests

**Files:**
- Modify: `web/src/pages/AssetDecisionsPage.test.tsx`

- [ ] **Step 1: Assert compact support strip semantics**

Add to the default page test or a focused new test:

```tsx
const supportStrip = await screen.findByLabelText('资产决策辅助入口')
expect(supportStrip).toHaveClass('asset-decision-support-strip')
expect(within(supportStrip).getAllByRole('button')).toHaveLength(4)
expect(normalizedText(supportStrip).length).toBeLessThanOrEqual(160)
expect(within(supportStrip).queryByText(/回看判断与执行回读|管理比较篮子和启动模板|只读订阅窗口事实|保留单台续费处理/)).not.toBeInTheDocument()
```

- [ ] **Step 2: Keep existing deep-link behavior**

Update `openSecondaryWorkbench` and `auto-expands the matching secondary workbench for supported deep links` to use the new support-strip label:

```tsx
async function openSecondaryWorkbench(label: '打开记录' | '打开场景' | '查看续费' | '查看单台队列') {
  const supportStrip = await screen.findByLabelText('资产决策辅助入口')
  fireEvent.click(within(supportStrip).getByRole('button', { name: label }))
}
```

Then keep the active button assertion:

```tsx
const supportStrip = await screen.findByLabelText('资产决策辅助入口')
expect(within(supportStrip).getByRole('button', { name: deepLink.activeButton })).toHaveClass('primary')
```

Remove old test lookups for `资产决策次级工作区`. The final accessible name is `资产决策辅助入口`.


- [ ] **Step 3: Verify the focused test fails before implementation**

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Expected before implementation:

- FAIL because current `AssetDecisionSecondaryNav` renders large card text and `aria-label="资产决策次级工作区"` only.

## Task 3: Update Lead Model and Work Selection

**Files:**
- Modify: `web/src/pages/AssetDecisionsPage.tsx`

- [ ] **Step 1: Add stable lead kind**

Change `AssetDecisionPortfolioLead` to include:

```ts
type AssetDecisionPortfolioLead = {
  kind: 'work' | 'stable'
  tone: BadgeTone
  eyebrow: string
  title: string
  summary: string
  actionLabel?: string
  contextLabel: string
  riskLabel: string
  evidenceLabel: string
  renewalLabel: string
  primaryItem?: AssetDecisionNextWorkItem
  primaryGroupID?: string
}
```

- [ ] **Step 2: Stop templates and manual groups from becoming primary work**

In `deriveNextWorkItems`, remove the loops that push `manual_group` and `scenario_template` items. Keep `manualGroups` and `templates` parameters only if needed for signature compatibility during the first edit; after all call sites compile, remove unused parameters and update the call near `nextWorkItems`.

Expected final behavior:

- Records with drift/blocked/missing evidence can become primary work.
- Automatic groups can become primary work.
- Manual groups and templates remain accessible through the support strip and scenario workbench only.

- [ ] **Step 3: Make stable fallback explicit**

In `buildPortfolioLead`, return `kind: 'stable'` when there is no `first`, no `fallbackGroup`, and no `metrics.partialErrorCount`.

Use these stable strings:

```ts
{
  kind: 'stable',
  tone: 'normal',
  eyebrow: '当前判断',
  title: '当前没有需要处理的组合决策',
  summary: '已加载视图内暂无待处理项；历史记录、场景模板和单台队列可按需打开。',
  contextLabel,
  riskLabel,
  evidenceLabel,
  renewalLabel: renewalLabelText,
}
```

For partial errors, return `kind: 'work'`, `eyebrow: '当前判断'`, `title: '部分资产决策证据不可用'`, `actionLabel: '查看已加载决策组'`.

- [ ] **Step 4: Make `openPortfolioLead` no-op for stable lead**

Change `openPortfolioLead`:

```ts
function openPortfolioLead() {
  if (portfolioLead.kind === 'stable') return
  if (portfolioLead.primaryItem) {
    openNextWorkItem(portfolioLead.primaryItem)
    return
  }
  if (portfolioLead.primaryGroupID) {
    openGroup(portfolioLead.primaryGroupID)
    return
  }
  setWorkbenchView('needs_decision')
}
```

- [ ] **Step 5: Run the focused tests**

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Expected after Task 3:

- Stable-state assertion around template/manual promotion passes.
- English-noise and support-strip assertions may still fail until Tasks 4-5.

## Task 4: Refactor Page Body and Support Strip

**Files:**
- Modify: `web/src/pages/AssetDecisionsPage.tsx`
- Modify: `web/src/pages/asset-decisions/AssetDecisionSecondaryNav.tsx`
- Modify: `web/src/pages/asset-decisions/types.ts`

- [ ] **Step 1: Make secondary nav item text Chinese and compact**

In `buildSecondaryNavItems`, replace English eyebrows and long summaries:

```ts
return [
  {
    value: 'records',
    eyebrow: '历史记录',
    title: '保存记录',
    summary: recordIssues > 0 ? `待复核 ${recordIssues}` : '可回看',
    meta: recordMeta,
    actionLabel: '打开记录',
    tone: recordsState.error ? 'alert' : recordIssues > 0 ? 'notice' : 'normal',
  },
  {
    value: 'scenarios',
    eyebrow: '场景',
    title: '场景与组合',
    summary: manualGroupsState.error || templatesState.error ? '部分不可用' : '按需打开',
    meta: scenarioMeta,
    actionLabel: '打开场景',
    tone: manualGroupsState.error || templatesState.error ? 'alert' : 'normal',
  },
  {
    value: 'renewals',
    eyebrow: '续费事实',
    title: '续费窗口',
    summary: queueState.renewals.length > 0 ? '有临近项' : '无临近项',
    meta: renewalMeta,
    actionLabel: '查看续费',
    tone: queueState.renewalsError ? 'alert' : queueState.renewals.length > 0 ? 'notice' : 'normal',
  },
  {
    value: 'single_queue',
    eyebrow: '单台辅助',
    title: '单台队列',
    summary: totalDecisionQueue > 0 ? '可逐台处理' : '暂无待处理',
    meta: singleQueueMeta,
    actionLabel: '查看单台队列',
    tone: queueState.queueError ? 'alert' : totalDecisionQueue > 0 ? 'notice' : 'normal',
  },
]
```

- [ ] **Step 2: Convert `AssetDecisionSecondaryNav` to support strip**

Render a compact nav:

```tsx
export function AssetDecisionSecondaryNav({ items, active, onOpen }: AssetDecisionSecondaryNavProps) {
  return (
    <nav className="asset-decision-support-strip" aria-label="资产决策辅助入口">
      {items.map((item) => (
        <article
          key={item.value}
          className={`asset-decision-support-strip__item asset-decision-support-strip__item--${item.tone}${active === item.value ? ' asset-decision-support-strip__item--active' : ''}`}
        >
          <div className="asset-decision-support-strip__copy">
            <span>{item.eyebrow}</span>
            <strong>{item.title}</strong>
            <small>{item.summary}</small>
          </div>
          <div className="asset-decision-support-strip__meta">
            <Badge variant="state" tone={item.tone}>{item.meta}</Badge>
            <button
              className={`btn sm ${active === item.value ? 'primary' : 'secondary'}`}
              type="button"
              onClick={() => onOpen(item.value)}
            >
              {item.actionLabel}
            </button>
          </div>
        </article>
      ))}
    </nav>
  )
}
```

- [ ] **Step 3: Update current judgment JSX**

In the `asset-decision-command-summary__lead` area:

```tsx
<span>{portfolioLead.eyebrow}</span>
<h2>{portfolioLead.title}</h2>
<p>{portfolioLead.summary}</p>
{portfolioLead.kind === 'work' && portfolioLead.actionLabel && (
  <div className="asset-decision-command-summary__actions">
    <button className="btn md primary" type="button" onClick={openPortfolioLead}>
      {portfolioLead.actionLabel}
    </button>
    <Link className="btn md secondary" to={`/asset-decisions?view=evidence&renew_within_days=${renewalWindow}&scenario=evidence_cleanup`}>
      资料缺口
    </Link>
  </div>
)}
```

Stable lead must render no button inside `asset-decision-command-summary__lead`; user-initiated stable actions are available only through the support strip.

- [ ] **Step 4: Replace four big English fact cards with Chinese facts**

Use a compact fact strip:

```tsx
<div className="asset-decision-command-summary__facts" aria-label="资产组合决策当前事实">
  <div className="asset-decision-focus__item asset-decision-focus__item--notice">
    <span>组合组数</span>
    <strong>{portfolioState.overviewLoading ? '...' : overview?.group_count ?? portfolioState.groups.length}</strong>
  </div>
  <div className="asset-decision-focus__item asset-decision-focus__item--alert">
    <span>续费组</span>
    <strong>{portfolioState.overviewLoading ? '...' : overview?.renewal_group_count ?? 0}</strong>
  </div>
  <div className="asset-decision-focus__item asset-decision-focus__item--critical">
    <span>闭环异常</span>
    <strong>{closedLoopMetrics.readbackDriftCount + closedLoopMetrics.readbackBlockedCount + closedLoopMetrics.readbackNeedsEvidenceCount}</strong>
    {closedLoopMetrics.partialErrorCount > 0 && <small>{portfolioLead.riskLabel}</small>}
  </div>
  <div className="asset-decision-focus__item asset-decision-focus__item--normal">
    <span>证据状态</span>
    <strong>{overview ? '已聚合' : '等待'}</strong>
  </div>
</div>
```

- [ ] **Step 5: Chinese page-section eyebrows**

Replace visible English page body labels:

- `PORTFOLIO WORKBENCH` -> `组合扫描`
- `SCENARIO WORKBENCH` -> `场景工作区`
- `SCENARIO TEMPLATES` -> `场景模板`
- `DECISION MEMORY` -> `保存记录`
- `RENEWAL EVIDENCE` -> `续费事实`
- `SINGLE VPS QUEUE` -> `单台辅助`

Do not change machine values, IDs, table data, currency codes, or API fields.

- [ ] **Step 6: Re-run focused tests**

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Expected after Task 4:

- Page body English-noise tests pass.
- Support strip semantic tests pass.
- Deep-link tests still pass.

## Task 5: CSS Polish and Responsive Layout

**Files:**
- Modify: `web/src/index.css`

- [ ] **Step 1: Make the lead panel calmer**

Update existing selectors rather than creating a second style system:

```css
.asset-decision-workbench .asset-decision-command-summary {
  grid-template-columns:minmax(320px,0.7fr) minmax(240px,0.3fr);
}

.asset-decision-command-summary__lead {
  min-height:0;
}
```

Stable lead should not render `asset-decision-command-summary__actions`.

- [ ] **Step 2: Style support strip**

Replace or de-emphasize `.asset-decision-secondary-nav*` with:

```css
.asset-decision-support-strip {
  display:grid;
  grid-template-columns:repeat(4,minmax(0,1fr));
  gap:var(--space-2);
  min-width:0;
}

.asset-decision-support-strip__item {
  display:grid;
  grid-template-columns:minmax(0,1fr) auto;
  gap:var(--space-2);
  align-items:center;
  min-width:0;
  min-height:72px;
  padding:var(--space-2) var(--space-3);
  border:var(--border-w) solid color-mix(in srgb,var(--border) 72%,transparent);
  border-radius:var(--radius-2);
  background:color-mix(in srgb,var(--surface-elevated) 6%,var(--surface));
}

.asset-decision-support-strip__item--active {
  border-color:color-mix(in srgb,var(--accent) 34%,var(--border));
  background:color-mix(in srgb,var(--accent) 5%,var(--surface));
}

.asset-decision-support-strip__copy,
.asset-decision-support-strip__meta {
  min-width:0;
}

.asset-decision-support-strip__copy {
  display:flex;
  flex-direction:column;
  gap:2px;
}

.asset-decision-support-strip__copy span,
.asset-decision-support-strip__copy small {
  color:var(--text-secondary);
  font-size:var(--type-small-size);
  line-height:1.25;
}

.asset-decision-support-strip__copy strong {
  color:var(--text-primary);
  font-size:var(--type-body-size);
  line-height:1.3;
}

.asset-decision-support-strip__meta {
  display:flex;
  align-items:center;
  justify-content:flex-end;
  gap:var(--space-2);
}
```

- [ ] **Step 3: Keep mobile support strip as 2x2**

At `max-width:920px` and `max-width:640px`, ensure:

```css
.asset-decision-support-strip {
  grid-template-columns:repeat(2,minmax(0,1fr));
}

.asset-decision-support-strip__item {
  grid-template-columns:1fr;
  align-content:space-between;
  min-height:104px;
  padding:var(--space-2);
}

.asset-decision-support-strip__meta {
  align-items:flex-start;
  justify-content:space-between;
}
```

Do not collapse to one column at `390px`; the accepted design is mobile 2x2.

- [ ] **Step 4: Run CSS-sensitive build checks**

Run:

```bash
cd web && npm run build
git diff --check
```

Expected:

- Build passes.
- No trailing whitespace or conflict markers.

## Task 6: Preserve Modal Layering and Write Paths

**Files:**
- Modify only if tests expose regressions:
  - `web/src/pages/AssetDecisionsPage.tsx`
  - `web/src/pages/AssetDecisionsPage.test.tsx`

- [ ] **Step 1: Re-run modal-density tests with the same file**

Run:

```bash
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
```

Expected:

- `expectDecisionCoverDensity` and `expectDetailDirectoryDensity` tests still pass.
- No default modal cover shows member rows, form panels, raw tables, or old English markers.

- [ ] **Step 2: Check write-path tests still pass**

Watch these existing test areas in the same test file:

- create manual group
- update manual group
- add/remove manual member
- save decision record
- template create/status update
- record status/member followup
- single VPS renewal decision

If any fail, fix the page wiring only; do not change backend payload expectations unless the existing test proves it already expects the wrong contract.

## Task 7: Browser Audit and Final Verification

**Files:**
- No tracked source file changes unless the audit exposes issues.
- Ignored helpers may live under `tmp/`.

- [ ] **Step 1: Full local verification**

Run:

```bash
cd web && npm run lint
cd web && npm run test -- --run AssetDecisionsPage.test.tsx
cd web && npm run test -- --run
cd web && npm run build
git diff --check
```

Expected:

- All commands pass.

- [ ] **Step 2: Browser audit**

Start or reuse the Vite dev server and mock API used in `research/runtime-audit.md`. Audit:

- Desktop `1440x1000`: default page, stable/empty state, renewal secondary, single queue, automatic group, manual group, template, saved record, source review.
- Mobile `390x900`: default page, stable/empty state, automatic group, saved record, single queue.

Expected:

- 0 document/body horizontal overflow.
- Page body English-noise list empty for `PORTFOLIO`, `RENEWAL`, `CLOSED LOOP`, `EVIDENCE`, `WORKBENCH`, `SCENARIO`, `DECISION MEMORY`, `SINGLE VPS QUEUE`.
- Stable state does not render a primary “处理/使用模板/继续组合/打开决策组” CTA.
- Default first screen shows current judgment, support strip, and start of decision scan without three-screen sprawl.
- Modal cover and directory density does not regress.

- [ ] **Step 3: Update task research if audit differs from expectations**

If any browser finding requires design adjustment, append the finding to:

```text
.trellis/tasks/07-02-asset-decisions-comprehensive-ux/research/runtime-audit.md
```

Then fix code and rerun verification before reporting completion.

## Rollback Points

- If lead-model changes break deep links, revert only `deriveNextWorkItems` / `buildPortfolioLead` changes and keep test additions to expose the defect.
- If support strip CSS causes mobile overlap, revert the CSS block for `.asset-decision-support-strip*` and keep the JSX/Chinese text changes.
- If modal tests regress, restore modal JSX around the failing panel before continuing page-body work; modal cover layering is a protected contract.

## Review Checklist Before `task.py start`

- [ ] `prd.md` states stable silent behavior and compact support-strip acceptance.
- [ ] `design.md` records decision-first IA, support strip, CSS landing in `web/src/index.css`, and modal preservation.
- [ ] `implement.md` starts with tests, preserves contracts, and has exact verification commands.
- [ ] User has reviewed and approved these three artifacts.
