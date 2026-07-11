# Task 6 本地验证证据

> 状态：implementation PR 前的本地证据；正式跨路由 Playwright/axe CI 属于 Task 10，真实认证 staging 与发布版本 smoke 尚未完成。

## Baseline And Commits

- Base: `origin/main@07a9a77`
- Branch: `codex/frontend-accessibility-contracts`
- Runtime: Node `22.23.1`
- Implementation HEAD used by the final local gates: `f7db74d`
- Commits:
  - `b96ffc8 fix(web): expose native field accessibility state`
  - `dd55783 fix(web): separate tabs from segmented choices`
  - `f3fd4e0 fix(web): restore shell and row keyboard paths`
  - `3133961 fix(web): meet accessible state contrast`
  - `1380089 fix(web): harden light theme state contrast`
  - `f7db74d refactor(web): centralize interactive row guards`

## Source And Component Gates

- Field contract: Input/Select focused set 15/15；required/ref、generated/explicit id、error/hint、describedby merge/dedupe、caller invalid、options/children 均通过。
- Tabs migration: production 调用为 10 个真实 Tabs + matching TabPanel、6 个 SegmentedControl；focused migration set 9 files / 188 tests 通过。
- Shell/menu/row focused integration: 20 files / 309 tests 通过；最终相关回归 10 files / 113 tests 通过。
- Semantic/CSP source contracts: `semanticInteractionContract.test.ts` + `cspContract.test.ts` 共 18 tests 通过；semantic inventory 为 allowed=7、unexplained=0。
- Full Vitest: 90 files / 669 tests，较 Task baseline 86/633 增加 4 files / 36 tests，无删除基线覆盖。
- `npm audit --include=dev`: 0 vulnerabilities。
- Final command gate on Node `22.23.1`: clean `npm ci --include=dev`、lint、90/669 Vitest、production build、`make verify`（Go + web）与 `git diff --check` 全部返回 0。

## Local Chromium Keyboard And Axe

- Browser: Chromium `150.0.7871.114`（CDP，headless=new）。
- Final clean rerun: branch HEAD `61c28a2`（implementation tree `f7db74d`）；全新 Chromium profile、production dist 与增强后的真实默认 Tab 路径。
- axe-core: exact `4.10.3`，只安装在 `/tmp/houfeng-task6-browser`，未修改 `web/package.json` / lockfile。
- Data source: `mock-api asset-workflows`；production `web/dist` 由本地临时 server 提供，并发送仓库精确 CSP policy。
- Routes: `/`、`/settings`、`/vps`、`/asset-decisions`。
- Viewports: `1440x1000`、`1024x768`、`390x900`；4 routes × 3 viewports = 12/12，无 document-level horizontal overflow、空白 surface 或 shell control clipping。
- 真实按键流程：
  1. 首次 Tab → skip link，Enter → `main#main-content`。
  2. User menu Enter/ArrowDown/Home/End/Escape；Escape 回 trigger；重开后 Tab 关闭并把焦点前移到全局搜索。
  3. Theme menu Enter/ArrowDown/Enter；Arrow 只移动焦点，选择后回 trigger。再次 Space 打开后，真实 Tab 默认行为关闭菜单并把焦点前移到“查看通知事件”，Shift+Tab 回到 theme trigger；再次 Space 打开、Escape 关闭并回 trigger。
  4. Settings Tabs ArrowRight/Home 自动激活并保持 tab/panel id 双向关系；SegmentedControl Space 激活“经典”。
  5. VPS SegmentedControl Space 激活“未关联”；VPS 名称 Link Enter 导航到 `/vps/vps_tokyo_lab`。
  6. Asset detail Modal 的 tab ArrowRight 激活 matching panel；Escape 关闭、恢复“查看组” trigger 并释放 body scroll lock。
- axe settled scans：Dashboard + AppShell `/`、Settings `/settings`、VPS `/vps` 均 serious=0、critical=0；VPS 额外在 `theme-houfeng-dark`、`theme-houfeng-light`、`theme-classic-dark` 三主题矩阵均为 0。
- Diagnostics: page console error=0、runtime exception=0、CSP violation=0、HTTP >=400=0、network loading failure=0。
- Browser RED→GREEN findings:
  - primary action `#fff` / `#3b82f6` 仅 3.68:1，改为 theme-aware `color: var(--bg)`；
  - maintenance token 与 light theme normal/notice/critical/muted 状态色不足，收敛到三主题 owner tokens 后全矩阵通过。

## Limitations And Remaining Gates

- 本地 fixture 只证明受保护页面与代表性资产状态的前端行为，不证明真实后端数据、认证、导入或通知正确性。
- 未提交截图或 bulk raster；本任务不建立长期视觉基线。
- Task 10 仍负责把 coverage、Playwright、axe、CSP、bundle/AST budgets 和 `workflow_dispatch + GitHub staging environment` 写入 CI。
- Task 6 只有在 implementation PR、main CI、Release Please、release/publish-images、发布版本 browser smoke 与 archive PR 完成后才能归档；Task 7 在此之前不得启动。
