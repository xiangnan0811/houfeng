# 可访问性交互契约实施计划

> **Inline execution:** 不分派实现/检查子代理。Task 启动后由主会话按 `superpowers:executing-plans`、`superpowers:test-driven-development`、`trellis-before-dev`、`browser`、`trellis-check` 和 `superpowers:verification-before-completion` 顺序执行。

**Goal:** 用原生表单/导航/命令和准确的 Tabs/Menu/skip-link 合同关闭 P2-01、P2-02、P2-03，同时保持现有 API、URL、主题值与业务状态不变。

**Architecture:** Field atoms 负责 required/ref/description；真正视图用 Tabs + TabPanel；值选择器用 SegmentedControl；UserChip/ThemeSwitcher 管理各自 menu focus；TypeScript AST guard 阻止 non-semantic click 回流。

**Baseline:** `origin/main@07a9a77`、Node `22.23.1`、86 files / 633 tests、npm audit 0、`make verify` green。

---

## 0. Workflow State And Activation Gate

当前 task 必须保持 `planning`；本计划被写入不等于允许修改业务代码。

- [x] 直接依赖 `frontend-modal-stack-focus` 已归档，PR #344 和 post-merge evidence 可用。
- [x] 从 latest `origin/main@07a9a77` 创建 `/home/murray/code/houfeng/.worktree/frontend-accessibility-contracts`，branch=`codex/frontend-accessibility-contracts`，base=`main`。
- [x] hooks 已启用；Node 22.23.1 baseline `make verify`、86/633 tests 与 audit 0 已记录。
- [x] 用户已于 2026-07-11 review 并批准最终 `prd.md`、`design.md`、`implement.md`。
- [x] `task.py validate`、metadata、占位词、Markdown fence 和 `git diff --check` 全部通过。
- [x] 只有前述条件都满足后运行：

```bash
python3 ./.trellis/scripts/task.py start 07-10-frontend-accessibility-contracts
```

禁止在 planning 阶段添加 package dependency、修改业务 TSX/CSS 或创建 release。Inline 模式不使用 runtime sub-agent。

## 1. Refresh Inventory Immediately After Start

启动后先确认 `origin/main` 未变化；若前置 main 有新提交，先安全刷新 feature branch/worktree并重跑 baseline，不在旧基线上实现。

- [x] 记录 branch、HEAD、`git status`、Node/npm 与 hooks path。
- [x] 重新运行：

```bash
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify
npm --prefix web audit --include=dev
git diff --check
```

- [x] 重跑精确 inventory：Input/Select/Tabs 调用数、non-test TSX 列表、AST non-semantic click 列表。
- [x] 确认 16 个 Tabs caller matrix 与实际主线一致；新增/删除 caller 时先更新设计矩阵，不能漏迁移。
- [x] 确认 Task 7/8/9/10 仍未向相同文件落入未合并改动。

**Stop condition:** baseline failure、dependency archive 缺失、branch 非预期或 caller inventory 漂移且未解释时停止，回到 planning 更新文档。

## 2. Field Atom Contracts (RED → GREEN)

### Files

- Modify: `web/src/components/atoms/Input.tsx`
- Modify: `web/src/components/atoms/Input.test.tsx`
- Modify: `web/src/components/atoms/Select.tsx`
- Create: `web/src/components/atoms/Select.test.tsx`
- Optional create: `web/src/components/atoms/fieldA11y.ts`（仅保存两个 atoms 共用的 idref token helper）

### 2.1 RED Tests

- [x] Select required label 与 native `<select required>` 同时成立；测试先因 native required 缺失而红。
- [x] Input/Select error 有稳定 id、control `aria-invalid=true` 且 accessible description 等于 error。
- [x] hint 在无 error 时进入 accessible description；error 出现时 hint 不渲染也不被引用。
- [x] caller `aria-describedby="external shared"` 与内部 id 合并，重复 token 去重且 external 顺序保留。
- [x] 无 error 时 caller `aria-invalid="true"` 不被覆盖；有 error 时 caller false 被提升为 true。
- [x] explicit id 与 generated id 两条路径都验证；ref 分别等于真实 HTMLInputElement/HTMLSelectElement。
- [x] Select 的 options 与 children、Input/Select onChange/className/native attrs 回归保持。

Focused RED：

```bash
NODE_ENV=test npm --prefix web run test -- --run \
  src/components/atoms/Input.test.tsx \
  src/components/atoms/Select.test.tsx
```

RED 必须是预期属性/accessible-description assertion，不能接受 import、类型或语法错误。

### 2.2 Minimal Implementation

- [x] 实现 token split/dedupe helper；空结果返回 undefined，不输出空 attribute。
- [x] 两个 atom 从最终 control id 派生 `-error` / `-hint`。
- [x] 明确读取 caller describedby/invalid 后再 spread 其余 props，避免 spread 顺序覆盖增强属性。
- [x] required 同时传 native element 和 required label class；保留 forwardRef。
- [x] 不增加 `aria-errormessage`、live region 或隐藏 duplicate text。

### 2.3 GREEN And Call-Site Safety

```bash
NODE_ENV=test npm --prefix web run test -- --run \
  src/components/atoms/Input.test.tsx \
  src/components/atoms/Select.test.tsx \
  src/pages/VPSPage.test.tsx \
  src/pages/SubscriptionsPage.test.tsx \
  src/pages/VPSDetailPage.test.tsx
NODE_ENV=production npm --prefix web run build
```

- [x] 确认 110 Input / 17 Select caller 无需业务性补丁即可编译。
- [x] 搜索新增 `as`、non-null assertion、eslint disable；本阶段无无理由 suppressions。

**Commit boundary:** `fix(web): expose native field accessibility state`。

**Rollback:** atom commit 可独立回滚；不要在 127 个调用方逐页复制 describedby。

## 3. Tabs, TabPanel And SegmentedControl Primitives (RED → GREEN)

### Files

- Modify: `web/src/components/atoms/Tabs.tsx`
- Modify: `web/src/components/atoms/Tabs.test.tsx`
- Create: `web/src/components/atoms/SegmentedControl.tsx`
- Create: `web/src/components/atoms/SegmentedControl.test.tsx`
- Modify: `web/src/components/atoms/index.ts`

### 3.1 Tabs RED

- [x] `label` 命名 tablist；`idBase` 生成 tab/panel ids 与 `aria-controls`。
- [x] selected 是唯一 `tabIndex=0`；其余 -1。
- [x] ArrowRight/Left wrap、Home/End 边界都 focus target 并调用一次 onChange。
- [x] 动态 items 不含 current value 时第一项是唯一 tab stop；空 items 不 crash。
- [x] click 仍传泛型 value；count badge accessible name/视觉类保持。
- [x] `TabPanel` role/id/labelledby/tabIndex 与 helper 完全一致。

### 3.2 SegmentedControl RED

- [x] named group 只包含 native buttons，不出现 tab/tablist/tabpanel role。
- [x] 当前值仅一个 `aria-pressed=true`，click 传泛型 value。
- [x] count=0 不渲染 badge，正数保持 count badge；全部 button 可由普通 Tab 顺序到达。

### 3.3 Minimal Implementation

- [x] Tabs 用 button ref array 管理焦点；不引入 document key listener。
- [x] `tabId` / `tabPanelId` 是单一 id owner，caller 不手抄第二套格式。
- [x] SegmentedControl 复用现有 pill CSS class，不在本任务重命名 CSS owner。
- [x] barrel exports 完整；不增加第三方 dependency。

Focused GREEN：

```bash
NODE_ENV=test npm --prefix web run test -- --run \
  src/components/atoms/Tabs.test.tsx \
  src/components/atoms/SegmentedControl.test.tsx
```

**Commit hold:** primitive 不能单独提交为破坏性 API；必须和下一节 16 个 caller migration 同一逻辑 commit。

## 4. Migrate All 16 Tabs Callers Atomically

### 4.1 Six Value/Filter Callers → SegmentedControl

- [x] `VPSPage.tsx`: label `VPS 快速视图`。
- [x] `EventsFilterDrawer.tsx`: label `事件时间范围`。
- [x] `TargetTimeWindowTabs.tsx`: label `目标观测时间窗口`；组件名可暂保留以限制 diff，语义来自 SegmentedControl。
- [x] `MonitoringInstanceTimeWindowTabs.tsx`: label `监控实例观测时间窗口`。
- [x] `ThemeSettingsSection.tsx`: labels `主题风格`、`主题明暗`。
- [x] 更新这些 caller tests：查询 group/button/pressed，不再查询虚假 tab。

### 4.2 Ten Real Tabs → Tabs + TabPanel

- [x] Settings main：`系统设置分区` / `settings-sections`；把 subscriptions 与 system form 都放入同一个 active TabPanel。
- [x] Subscription month cost：`月成本展示` / `subscription-month-cost`；empty/pie/ranking 均在 active panel 内。
- [x] Target history：`目标历史类型` / `target-history`。
- [x] Monitoring history：`监控实例历史类型` / `monitoring-history`。
- [x] Portfolio workbench：`资产决策组合视图` / `asset-portfolio-workbench`。
- [x] Decision queue：`单台辅助队列视图` / `asset-decision-queue`。
- [x] Group detail：`决策组详情分区` / `asset-group-detail`。
- [x] Template detail：`场景模板详情分区` / `asset-template-detail`。
- [x] Manual group detail：`自定义组合详情分区` / `asset-manual-group-detail`。
- [x] Record detail：`决策记录详情分区` / `asset-record-detail`。

### 4.3 Cross-Caller Verification

- [x] `rg '<Tabs'` 逐条与 10-row matrix 对齐；每个都有 literal `label`/`idBase`。
- [x] `rg '<SegmentedControl'` 与 6-row matrix 对齐。
- [x] 每个 rendered tab `aria-controls` resolve 到一个 active DOM panel，panel `aria-labelledby` resolve 回 selected tab。
- [x] dynamic Asset tab value（create/save/raw/source 等）切换后 id 仍与当前 value 对齐。
- [x] Settings dirty-tab confirmation、history lazy loading、Asset modal actions、URL filters 和 theme values保持原测试行为。

Focused tests：

```bash
NODE_ENV=test npm --prefix web run test -- --run \
  src/pages/SettingsPage.test.tsx \
  src/pages/SubscriptionsPage.test.tsx \
  src/pages/EventsPage.test.tsx \
  src/pages/VPSPage.test.tsx \
  src/pages/TargetDetailPage.test.tsx \
  src/pages/MonitoringDetailPage.test.tsx \
  src/pages/AssetDecisionsPage.test.tsx
NODE_ENV=production npm --prefix web run build
```

**Commit boundary:** `fix(web): separate tabs from segmented choices`。

**Rollback:** primitive + all callsites 同 commit 回滚，不能保留半迁移 API 或通过 optional props 兼容旧调用。

## 5. User And Theme Menus (RED → GREEN)

### Files

- Modify: `web/src/app/layout/UserChip.tsx`
- Modify: `web/src/app/layout/UserChip.test.tsx`
- Modify: `web/src/app/layout/Sidebar.tsx`
- Modify: `web/src/app/layout/Sidebar.test.tsx`
- Modify: `web/src/app/layout/TopBar.tsx`
- Create: `web/src/app/layout/TopBar.test.tsx`
- Modify: current shell/layout CSS partials only as required for native button reset/focus state

### 5.1 User Menu RED

- [x] Sidebar uses exactly one UserChip trigger; no clickable `.user-chip` div remains。
- [x] trigger has accessible username name、haspopup、controls、expanded false/true。
- [x] menu items are buttons with `menuitem`, not div/span；现有 visible commands仍是修改密码/退出登录。
- [x] click/Enter/Space open；ArrowDown first、ArrowUp last；Arrow wrap、Home/End。
- [x] Escape closes and trigger has focus；Tab closes and focus follows browser default；outside mousedown closes。
- [x] change-password/logout callbacks each fire once and menu disappears；Modal focus由 Task 2 contract接管。

### 5.2 Theme Menu RED

- [x] trigger exposes menu state；menu has four `menuitemradio` buttons and one checked item。
- [x] Arrow navigation moves focus without changing theme；Enter/Space/click activates exactly one option。
- [x] selection retains current preset/mode mapping, closes and returns trigger focus。
- [x] Escape/Tab/outside behavior matches user menu；无 orphan document listener。

### 5.3 GREEN

- [x] Sidebar imports/renders UserChip and deletes inline duplicate state/ref/effect。
- [x] local handlers remain component-owned；不创建 global Menu framework。
- [x] CSS uses current tokens；button reset does not remove focus-visible outline。

```bash
NODE_ENV=test npm --prefix web run test -- --run \
  src/app/layout/UserChip.test.tsx \
  src/app/layout/Sidebar.test.tsx \
  src/app/layout/TopBar.test.tsx \
  src/app/layout/AppShell.test.tsx
```

## 6. Skip Link And VPS Primary Navigation (RED → GREEN)

### Files

- Modify: `web/src/app/layout/AppShell.tsx`
- Modify: `web/src/app/layout/AppShell.test.tsx`
- Modify: shell/base CSS partial containing layout accessibility styles
- Modify: `web/src/pages/VPSPage.tsx`
- Modify: `web/src/pages/VPSPage.test.tsx`

### Checklist

- [x] RED：认证 AppShell 第一可聚焦 link 是 `跳到主内容`，href=`#main-content`；main tabindex=-1。
- [x] RED：VPS name 是 `/vps/:id` Link；keyboard click enters detail without row simulation。
- [x] RED：background row click仍进入同一路由，link/button descendants不会触发 row handler。
- [x] 实现 skip link markup和token CSS；focus-visible进入视口且 z-index高于 shell。
- [x] 实现 VPS primary Link 与 interactive-target guard；row不增加 tabIndex/role/keyDown。
- [x] 不改变 filters、query、fetch、create modal或visible asset name。

```bash
NODE_ENV=test npm --prefix web run test -- --run \
  src/app/layout/AppShell.test.tsx \
  src/pages/VPSPage.test.tsx
```

## 7. TypeScript AST Semantic Guard (RED → GREEN)

### Files

- Create: `web/src/security/semanticInteractionContract.test.ts`
- Modify only current justified nodes to add immediate JSX reason comments

### Checklist

- [x] 写 pure `auditSource(path, source)` fixture cases：native button pass、unmarked div fail、unknown reason fail、四个 approved reasons pass。
- [x] 用 `ts.createSourceFile` 扫描 non-test production TSX；报告 path/line/tag/reason。
- [x] 首次 repository run 因 Sidebar/TopBar/VPS 与未标记现存例外而 RED。
- [x] Sidebar/TopBar 真实缺口通过 native element消失；VPS 只保留 `primary-link-row-enhancement`。
- [x] Modal backdrop、Targets propagation、DataTable/Monitoring/Targets keyboard-complete rows逐个加准确 marker；不添加目录级或 wildcard allowlist。
- [x] 最终未解释 violation = 0；删除任一 marker 的临时 probe 会失败，恢复后重跑 GREEN。
- [x] guard 自身不使用 regex寻找 JSX；regex只解析 AST 节点前的 marker reason。

```bash
NODE_ENV=test npm --prefix web run test -- --run \
  src/security/semanticInteractionContract.test.ts \
  src/security/cspContract.test.ts
```

## 8. Focused Integration Gate

- [x] 运行所有直接相关 tests：

```bash
NODE_ENV=test npm --prefix web run test -- --run \
  src/components/atoms/Input.test.tsx \
  src/components/atoms/Select.test.tsx \
  src/components/atoms/Tabs.test.tsx \
  src/components/atoms/SegmentedControl.test.tsx \
  src/app/layout/UserChip.test.tsx \
  src/app/layout/Sidebar.test.tsx \
  src/app/layout/TopBar.test.tsx \
  src/app/layout/AppShell.test.tsx \
  src/pages/VPSPage.test.tsx \
  src/pages/SettingsPage.test.tsx \
  src/pages/EventsPage.test.tsx \
  src/pages/TargetDetailPage.test.tsx \
  src/pages/MonitoringDetailPage.test.tsx \
  src/pages/SubscriptionsPage.test.tsx \
  src/pages/AssetDecisionsPage.test.tsx \
  src/security/semanticInteractionContract.test.ts
```

- [x] `NODE_ENV=test npm --prefix web run lint`。
- [x] `NODE_ENV=production npm --prefix web run build`。
- [x] 审计 changed diff：无 debug log、broad disable、unsafe cast、unrelated formatting、API/wire change。
- [x] 测试 file/count 不低于 86/633；任何替换列出等价证据。

## 9. Local Chromium Keyboard And Axe Evidence

Task 10 尚未建立 repository Playwright/axe，不调用不存在的 `npm run test:e2e`。

- [x] 使用 production build/preview 与当前 fixture auth/data 启动 `/`、`/settings`、`/vps`、Dashboard以及 representative Asset/history modal。
- [x] 使用 `browser` skill 或 repo local browser helper；若 helper 不能表达按键/焦点，直接通过 CDP 完成。
- [x] 只用 Tab/Shift+Tab/ArrowLeft/Right/Up/Down/Home/End/Enter/Space/Escape 验证：
  - skip link → main；
  - Settings tab + panel；
  - segmented choices；
  - user/theme menu与focus return；
  - VPS name Link；
  - history/Asset modal Tabs，不破坏 Task 2 focus stack。
- [x] 在 repo 外临时安装并记录 exact axe-core version，通过 debugging context注入；不修改 package/lockfile。
- [x] 扫描 settled AppShell、Dashboard、Settings、VPS；serious=0、critical=0。
- [x] console error、page exception、CSP violation、unexpected network error = 0；预期 fixture状态单独列出。
- [x] 保存 task-local JSON/Markdown摘要：commit、browser、axe、routes、viewports、focus steps、violation counters、limitations；不提交 bulk raster或秘密。

**Failure rule:** 任何键盘死路、错误 focus return 或 serious/critical violation 回到对应 RED test后修复；不以“Task 10 later”跳过真实缺陷。

## 10. Spec Update

实现路径稳定且 browser evidence通过后使用 `trellis-update-spec`：

- [x] `.trellis/spec/web/component-conventions.md`：field、Tabs/TabPanel、SegmentedControl、menu、skip link、primary-link row contract。
- [x] `.trellis/spec/web/quality-guidelines.md`：semantic AST guard、RTL vs local browser vs Task10 formal gate层级。
- [x] `.trellis/spec/web/directory-structure.md`：新 atom/test/security contract的真实路径（仅在需要时）。
- [x] 不提前写 Task 7 responsive、Task 10 CI/Playwright 为已完成现状。
- [x] 更新本 task evidence和 parent Gate B 的 Task6部分；Gate B仍等待 Task7，不提前勾选整体通过。

## 11. Full Local Gate Before Commit/PR

从 clean install 运行：

```bash
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web ci --include=dev
NODE_ENV=test PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run lint
NODE_ENV=test PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run test -- --run
NODE_ENV=production PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web run build
PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH npm --prefix web audit --include=dev
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify
git diff --check
```

Evidence summary must include：

- exact test files/tests与baseline delta；
- 16 caller migration结果（10 Tabs / 6 segmented）；
- AST total/allowed/unexplained counters；
- browser/axe版本、routes、focus流程与0 serious/critical；
- known limitation：formal cross-route Playwright/axe CI仍属于 Task10。

## 12. Commit, PR, Release And Archive

Recommended commits：

1. `fix(web): expose native field accessibility state`
2. `fix(web): separate tabs from segmented choices`
3. `fix(web): restore shell and row keyboard paths`
4. `docs(spec): record accessible interaction contracts`

- [x] 每个 commit前运行对应 focused gate；不混入 Task7 responsive或Task8/9结构改动。
- [x] 使用 `trellis-check` 对 code/spec/data-flow/重复实现/AST guard做全量检查。
- [x] 使用 `superpowers:verification-before-completion` 重跑第11节并读取完整输出。
- [x] push `codex/frontend-accessibility-contracts`，创建 ready PR `fix(web): restore native interaction contracts`。
- [x] 监控 PR go/web/docker/GitGuardian；failure在同一branch本地复现后修复，不force-push猜测。
- [x] checks green 后通过 GitHub PR merge，不直接 push main；监控 main CI与Release Please。
- [x] 这是 release-worthy accessibility fix：更新/合并 release PR前确认checks；监控 GitHub Release和`publish-images`，核验版本、workflow run、Docker tag与manifest digest。
- [x] 发布版本上重跑 task-local browser keyboard/axe smoke；记录release commit/tag与结果。
- [ ] archive Task6，提交独立 archive/evidence PR并监控main CI；该项由当前 archive branch 完成，不能在归档前自证。
- [ ] Task6归档和post-merge检查完成后，才从fresh main创建/启动 Task7；Task10继续planning。

## 13. Rollback Matrix

| Failure | Rollback owner | Must retain |
| --- | --- | --- |
| field caller regression | field atom commit | unrelated Tabs/menu work |
| Tabs/panel mismatch | entire primitive+16 caller commit | field contract |
| menu focus regression | shell/menu commit | Tabs/fields |
| VPS row navigation regression | VPS portion/shell commit | semantic guard tests where still valid |
| AST false positive | guard implementation only, with fixture proving correction | fixed native controls；不加wildcard |
| browser/axe real failure | offending behavior commit | evidence真实性；不得降级severity或跳route |

任何 rollback 后都必须重跑 focused + full gate。不得用 optional `label/idBase`、role模拟 div、broad comment allowlist、axe rule disable或删除测试换取绿色。
