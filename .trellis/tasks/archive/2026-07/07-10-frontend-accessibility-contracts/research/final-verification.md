# Task 6 交付验证证据

> 状态：implementation、main CI、release、镜像发布与发布产物 smoke 已完成；正式跨路由 Playwright/axe CI 和真实认证 staging 仍属于 Task 10。

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

## PR, Release And Published Image

- Implementation PR [#357](https://github.com/xiangnan0811/houfeng/pull/357) 以 ready 状态提交；PR CI run `29137285402` 的 go、web、docker-image 与 GitGuardian 全部通过，merge commit 为 `df5669e039a82641df1e484c411f2236fd001d4b`。
- implementation merge 后 main CI run `29137362151` 与 Release Please run `29137362153` 全部成功；Release Please 把既有 PR [#355](https://github.com/xiangnan0811/houfeng/pull/355) 更新为 `chore(main): release 0.58.1`，CHANGELOG 明确包含本任务的 fix/docs commits。
- Release PR #355 的 go、web、docker-image 与 GitGuardian 全部通过；merge commit `f8fdb30d6339b00ae49f181527af7afac6ee4a70` 随后通过 main CI run `29137447764` 与 Release Please run `29137447777`。
- GitHub Release [`v0.58.1`](https://github.com/xiangnan0811/houfeng/releases/tag/v0.58.1) 于 2026-07-11 发布，target commit 为 `f8fdb30d6339b00ae49f181527af7afac6ee4a70`；amd64/arm64 agent、checksum manifest 与 minisign signature 四个 assets 均已上传。
- `publish-images` run [`29137451638`](https://github.com/xiangnan0811/houfeng/actions/runs/29137451638) 的 agent-assets、linux/amd64、linux/arm64 与 multi-arch publish 全部成功。
- Docker Hub `linnea7171/houfeng:v0.58.1`、`:0.58.1` 与 `:latest` 均为 OCI image index，三者共同 digest 为 `sha256:ff15def93f7f42d9a9aaf3757e0b450723e1513ce64720a7d38a487583f3cbe6`。本地 pull 后 OCI labels 精确为 revision `f8fdb30d6339b00ae49f181527af7afac6ee4a70`、version `v0.58.1`。
- 发布版本 smoke 直接从上述 digest 对应镜像的 `/app/web/dist` 提取产物，不复用分支 `web/dist`。全新 Chromium profile 下再次完成 4 routes × 3 viewports = 12/12、六条键盘流程与三主题 axe；serious/critical、console、runtime、CSP、HTTP >=400、network failure、document overflow 与 shell control clipping 均为 0。

## Limitations And Remaining Gates

- 本地 fixture 只证明受保护页面与代表性资产状态的前端行为，不证明真实后端数据、认证、导入或通知正确性。
- 未提交截图或 bulk raster；本任务不建立长期视觉基线。
- Task 10 仍负责把 coverage、Playwright、axe、CSP、bundle/AST budgets 和 `workflow_dispatch + GitHub staging environment` 写入 CI。
- implementation、main CI、Release Please、release/publish-images 与发布版本 browser smoke 已完成；当前独立 archive/evidence PR 合并并通过 post-merge 检查后，才从 fresh main 启动 Task 7。
