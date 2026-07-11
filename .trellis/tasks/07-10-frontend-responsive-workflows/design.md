# 窄视口核心流程设计

## 1. Design Principles

1. **可见文本是真合同**：命令的 aria-label 只能补充上下文，不能为被 ellipsis 的可见文案兜底。
2. **overflow 有唯一 owner**：Tabs 自己滚、宽表 wrapper 自己滚；page/section/document 不替子元素承担水平滚动。
3. **窄屏不是隐藏模式**：允许重新排版、换行和局部滚动，不隐藏命令、字段或 badge。
4. **最小行为变化**：CSS 与 wrapper 只改变布局/可达性；API、路由、受控值、点击 handler、排序和 modal 流程不变。
5. **证据分层**：Vitest/source contract 证明 markup 与规则存在，本地 Chromium 证明真实几何、受控 commit 后的滚动和焦点；Task 10 才把浏览器门持久化进 CI。

## 1.1 Alternatives Considered

### A. 缩小字号或 padding 让命令勉强塞下

只能延后断点，并会制造过小触控目标；更长中文、badge 或浏览器字体仍会再次裁切，不采用。

### B. 保留 ellipsis，用 aria-label 提供全名

读屏可知全名，但视觉用户仍必须猜测，直接违反本任务目标，不采用。

### C. 所有页面统一横向滚动

实现最少，但 heading、筛选和命令会离开视口，键盘用户也不知道滚动区域边界；只允许局部 owner 滚动。

### D. Task 7 直接引入 Playwright/axe

会重复 Task 10 的 package/lockfile/CI 所有权，也与已批准接口合同冲突。Task 7 使用 repo 外固定版本 Chromium/axe 留 local-only evidence，不新增 dependency。

## 2. Baseline Geometry

```text
390px viewport
├─ Settings tab “监控策略”: 78×49, white-space normal
├─ Asset title “场景与组合”: client 26 < scroll 58, ellipsis
├─ Provider “组合决策”: client 46 < scroll 52, ellipsis
├─ Provider panel: client 298 < scroll 986, overflow-x auto, no region/name/focus
└─ Dashboard primary action: y=272..372, already inside first 900px
```

九条核心路由当前 document overflow 为 0；所以实现必须保持 document width，而不是把局部问题转移到 body。

## 3. Target Ownership

| Concern | Current owner | Target change |
| --- | --- | --- |
| Tabs variants | `styles/partials/atoms.css` | 两种 variant 单行、局部 scroll、children 不 shrink |
| Asset support strip base | `styles/partials/legacy-assets.css` | title 不裁切，button/badge 可完整排版 |
| Asset breakpoints | `styles/partials/legacy-misc.css` | 只保留一套 `max-width:920px` 两列规则，删除两组 640px 冲突 |
| Provider table markup | `pages/ProvidersPage.tsx` | section 固定，table wrapper 独立 region |
| Provider styles | `styles/partials/legacy-provider.css` | 完整 links、scroll hint/wrapper/focus、table width |
| Source regressions | `styles/indexCssContract.test.ts`, `pages/ProvidersPage.test.tsx` | CSS/markup contract RED→GREEN |

不创建新的 CSS 文件，不把规则搬到 `modernize.css` 形成第四层覆盖；Task 9 后续基于当前真实 owner 做 AST/减债。

## 4. Tabs Overflow Design

`Tabs` 与 `SegmentedControl` DOM/API 不变。CSS 目标：

```css
.tabs--pill{
  display:flex;
  width:fit-content;
  max-width:100%;
  overflow-x:auto;
  overscroll-behavior-x:contain;
  scrollbar-gutter:stable;
}

.tabs--pill .tab,
.tabs--underline .tab{
  flex:0 0 auto;
  white-space:nowrap;
}

.tabs--underline{
  max-width:100%;
  overflow-x:auto;
  overscroll-behavior-x:contain;
  scrollbar-gutter:stable;
}
```

- `width:fit-content + max-width:100%` 让 pill 桌面仍按内容收束，超宽时才成为 scroll container。
- 键盘事件先把目标 button 写入 pending ref、聚焦并调用 `onChange`；受控 value 与对应 panel commit 后，由 `useLayoutEffect` 调用 `scrollIntoView({block:'nearest',inline:'nearest'})`。只依赖同步 `focus()` 的隐式滚动会在 panel 高度改变父级滚动条后再次裁切目标，因此不能作为最终合同。
- 不增加 document listener、任意 rAF 延时或手写 `scrollLeft`；scroll owner 仍是 tablist。
- scrollbar 不强制隐藏；可用性优先于“干净外观”。

## 5. Asset Secondary Navigation

Desktop 保持当前 inline flex。`max-width:920px` 使用唯一两列合同：

```css
@media (max-width:920px){
  .asset-decision-support-strip{
    display:grid;
    grid-template-columns:repeat(2,minmax(0,1fr));
  }

  .asset-decision-support-strip__item{
    display:grid;
    grid-template-columns:minmax(0,1fr);
    align-content:space-between;
    justify-content:stretch;
    min-height:72px;
  }

  .asset-decision-support-strip__item .badge{
    justify-self:start;
    max-width:100%;
    white-space:normal;
    text-align:left;
  }
}
```

Base title 使用 `white-space:normal; overflow:visible; text-overflow:clip; overflow-wrap:anywhere`。920px 合同一直覆盖到最窄视口，因此不再为相同 selector 维护 640px display/grid/min-height 变体。

`AssetDecisionSecondaryNav.tsx` 的 button、`aria-pressed`、title/meta/tone 数据不变；若测试发现无需 markup 变化，则不为“触文件清单”制造空改动。

## 6. Provider Local Scroll Region

Target DOM：

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
    <DataTable ... />
  </div>
</section>
```

设计要点：

- `page-panel--scroll-x` 从 section 删除；heading、meta、toolbar、error/hint 不进入 scroll wrapper。
- wrapper 使用 `overflow-x:auto`、`scrollbar-gutter:stable`、focus-visible outline；表格保持真实 `<table>`。
- entry column 从 176px 扩大到足够显示五个完整命令的宽度，table `min-width` 同步增加；links 允许 wrap 且不 hidden。
- “组合决策”追加 `provider-directory-entry-link--decision`，便于 Task 9 识别长命令 owner；href 与 aria-label 不变。
- visible hint 既是 affordance，也是 `aria-describedby`；不依赖 title tooltip 或 visually-hidden 文本。

## 7. Test And Browser Contract

### Vitest/source

- `ProvidersPage.test.tsx` 断言 region、label/description idref、tabIndex、可见提示、section 不再拥有 `page-panel--scroll-x`、decision modifier 与原 href。
- `indexCssContract.test.ts` 读取最终 import cascade，断言 Tabs 不 shrink/nowrap/overflow，Asset title 不 hidden/ellipsis，Asset grid breakpoint 只有一套，Provider links 不裁切，table wrapper 可滚动且有 focus rule。
- source contract 只保护本次具体规则；Task 9 的 PostCSS AST gate 将替代更宽泛的 CSS 预算职责。

### Local Chromium

三视口 × 九路由；关键断言：

- Settings tab `white-space=nowrap`，高度回到单行；Arrow/End 后 active tab 与 focus 在 tablist 可视区。
- Asset title `scrollWidth <= clientWidth + 1`，button rect height >= 40，computed overflow 不为 hidden/ellipsis。
- Provider decision link `scrollWidth <= clientWidth + 1`；scroll region `role/name/tabIndex` 正确，聚焦后 ArrowRight 使 `scrollLeft > 0`；section 本身不横向滚动。
- Dashboard primary action bottom < 900；document width <= viewport + 1；末尾命令 hit-test 不被 fixed/sticky surface 遮挡。
- Settings、Asset、Providers settled axe serious/critical=0；console/runtime/CSP/HTTP/network=0。

## 8. Compatibility And Rollback

| Failure | Rollback owner | Must retain |
| --- | --- | --- |
| desktop pills 变成整行底板 | Tabs CSS commit | Task 6 ARIA/keyboard API |
| Asset buttons过高或顺序变化 | Asset breakpoint commit | title 不裁切合同 |
| Provider table桌面密度回归 | Provider width/style hunk | local region semantics |
| Provider keyboard scroll失效 | wrapper markup/style hunk | full visible link text |
| browser/axe regression | offending owner commit | RED evidence 与测试，不降级断言 |

三个 owner 分小提交。任何 rollback 后重跑 focused Vitest、production build 与对应 390px browser flow；不使用 hidden/ellipsis、页面级 overflow 或 aria-label-only 作为回滚替代。
