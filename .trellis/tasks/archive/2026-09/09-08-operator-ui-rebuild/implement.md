# Implement — operator UI night observatory

## Landing decision — 2026-09-21

- Operator accepted **single-branch mixed landing** on `feat/operator-ui-observatory`. Do not split Go vs UI PRs (F34 closed as process, not a merge blocker).
- Observation-supporting center/agent/migrations (HostMetricPoint extras, `network_rates_valid` / 0064, runtime-summaries, runtime-stream `AcceptedAt` stamp) ship with the UI rebuild. First production deploy is the whole unit; rollback is the whole unit. Test env may be rebuilt.
- Remaining second-pass review work for a later session (do not treat F34 as open): **N0** list batch COMMAND_LIST buttons missing CSS; **N1/N7** TopBar `/targets/:id/records|evidence` and `/evidence/:id` titles; **N6** events dateless `custom` codec; **N5** leftover `legacy-batch.css`; **N10/N11/N12** test gaps. Do not commit `.tmp`. Do not push without consent.

## Current checkpoint — 2026-09-11

- Continue only in the existing `.worktree/operator-ui-rebuild` on `feat/operator-ui-observatory`; the setup instructions below describe the original start, not a request to create another worktree or edit main.
- User-accepted visual baseline: workbench, VPS list, VPS detail **excluding modals**. Preserve these surfaces while continuing other pages; this is not whole-site acceptance.
- Preserve the complete current source graph, including new components/hooks/styles. Shared shell/theme changes and other page adaptations are checkpointed, not declared visually finished.
- All five dirty main-checkout VPS-detail fragments are included or structurally superseded by this candidate. The original fragments contain incomplete duplicate JSX/type declarations; do not reapply them. They are retained in Git stash `fd1eda668d0ae35771af771715225e702fb8a0f1`.
- This consolidation introduces no application-code changes. Fresh Node 22.23.1 checks: lint, 215 test files / 1708 tests, production build, and CSS analysis pass. Browser sanity viewed the workbench, populated VPS directory and rich-resource VPS detail using the existing read-only local sample preview.
- Previous evidence remains tied to unchanged source: 647 frozen source/e2e/design/changelog files match. The earlier 65 E2E, 22 paired visual cases and 24 read-only paths are existing evidence, not newly rerun here. The three-model review covered the bounded last closure diff, not a fresh audit of the whole reconstruction.
- **Not release-ready:** entry JS is 111663 gzip bytes against the unchanged 110738 limit. The user explicitly deferred this issue. Existing CSS-budget increases versus main are also preserved as unresolved budget-policy debt, not newly approved here; CSS passing means the current candidate limits, not the original main limits.
- Next work: one page or modal scope at a time, relevant browser verification, then a small commit. Do not restart the accepted three surfaces, mark the entire task complete, merge main, or push without the corresponding decision.

## VPS facts modal slice — 2026-09-11

- Continued from `4487b0bfaad1c81a026d394d2dcf8f3e45336388` in the existing worktree/branch. Main and development worktree were clean at entry. No historical fragment/stash integration was repeated.
- Scope: shared `VPSFactsEditForm` only, used by overview and legacy detail. Four sections (identity/provider, location, connection/host, usage/note), existing form styles, two desktop columns and one narrow column. API, draft fields, provider snapshot, IPv6/SSH/country behavior, write ownership, conflict recovery and dangerous operations are unchanged. Accepted non-modal surfaces and other dialogs are untouched.
- Final Node 22.23.1 verification: lint, 4 related test files / 141 tests, production build, and current-candidate CSS analysis passed. CSS source 332680 bytes / 2293 rules / 9119 declarations; production CSS 310619 raw / 41774 gzip bytes. No budget files changed. This is not compliance with the original main CSS limits; historical budget-policy debt and deferred entry-JS excess remain unresolved. No full-site E2E or release gate was rerun.
- Browser: dedicated local Vite `http://127.0.0.1:5320`, using the existing mock/sample API at `:5329`, not a real center inventory. Viewed `/vps/vps_rich_resources` through 管理 → 编辑事实 in dark/light at 1440×1000 and 390×900 (DPR 1, scale 1), including narrow form top/bottom, single-column reflow and reachable save/cancel. Viewed `/vps/vps_legacy` through 管理 → 编辑基础资料 in light at both sizes. No document/dialog horizontal overflow observed.
- Browser interactions: continuous name entry, IPv6 enabled/address entry, SSH override off with IPv4 coupling, intercepted PATCH retaining If-Match and edited address fields, pending-save disable, simulated 503 feedback with draft retention, Escape dismissal/focus return to 管理, and fresh draft on reopen. Mutation responses were intercepted locally; no successful backend write or real-data correctness is claimed. Custom-country rehydration is covered by the existing regression test, not a new browser claim.
- Independent discovery gate reviewed only the frozen two application files against the checkpoint: integration `openai-codex/gpt-5.6-sol`, Grok `xai-oauth/grok-4.6`, Gemini `google-antigravity/gemini-3.8-flash`; all completed on their configured families with zero confirmed findings. Browser geometry checks cover the reviewers’ layout-evidence gaps; no wording/markup-pinning tests were added. No repair/verification cycle was required after discovery.
- Updated current component guidance and changelog. Visual quality remains pending user acceptance; this small slice does not accept all VPS dialogs or the overall reconstruction. No merge, push or release. The dedicated `:5320` preview is stopped after verification; pre-existing sample/preview services remain untouched.

- Follow-up F1 after `2379abb6`: removed the root `provider-form` class because it inherited provider-page control density. Retained shared section/grid/wide structure; the facts owner now provides only flex-column layout and spacing. Node 22 form regression and build passed; latest CSS source 332734 / 332823 bytes and 9122 / 9122 declarations, production CSS 310673 raw / 41778 gzip bytes. Actual light-theme overview dialog at 1440×1000 and 390×900 restores 12px labels, 16px input text and 8px 14px input padding; desktop two-column/narrow one-column, no horizontal overflow, and bottom actions reachable. All three original reviewers verified F1 resolved with no new findings (one bounded repair/verification cycle). This supersedes the earlier final CSS figures and preserves pending user visual acceptance. Preview `:5320` was stopped again after this check.

## VPS service/domain modal slice — 2026-09-11

- Base `45c1fb55cb8102cdb3d5c5c175cb3becc2d34f48`; same worktree/branch. Only service/domain reading sections and their shared relation CSS changed in application code. Title/type/status hierarchy, subordinate IDs, stable labelled fact columns, full-width entry/labels/notes and narrow reflow reuse existing atoms/tokens. API, write ownership, collection-modal semantics and accepted non-modal surfaces remain unchanged. Monitoring rows keep their existing flex layout.
- Node 22.23.1: lint, 5 relevant test files / 110 tests, build and current-candidate CSS analysis pass (`artifact://172` in the task session). CSS source 332660 bytes / 2294 rules / 9119 declarations; production CSS 310608 raw / 41778 gzip bytes. Budget files untouched; deferred entry-JS excess and original-main CSS policy debt remain unresolved.
- Actual local sample browser: healthy service/domain modals in dark/light, 1440×1000 and 390×900; 10-service/6-domain scrolling, long service/domain names and full entries, empty collections, service 503 feedback and retry back to two rows, Escape focus return, and unchanged monitoring flex layout. No horizontal overflow observed in checked populated resource modals. Legacy service/domain contents and create-entry visibility were also checked at 390px. Clipboard denial displayed explicit failure; successful copy is covered by the existing test, not a successful browser clipboard claim. No real inventory or successful backend mutation was exercised.
- Discovery ledger: no confirmed findings from integration (`openai-codex/gpt-5.6-sol`) or Grok (`xai-oauth/grok-4.6`). Gemini resolved to its intended `google-antigravity/gemini-3.8-flash` model but failed with API 429; the single permitted availability retry did not produce a completed review. **NOT READY: the three-model gate is incomplete.** No further reviewer was spawned and no broad audit was reopened. This local checkpoint is not an accepted/release-ready change.
- Preview is deliberately left running at `http://127.0.0.1:5320/vps/vps_healthy_full` via `houfeng-modal-user-preview`, using the existing sample API on 5329. Unlike the pre-existing readonly 5198 preview, editing dialogs can open and fields can change; the sample API rejects saves. This supersedes the earlier stopped-preview note. Pre-existing previews/sample services are untouched. Visual acceptance remains with the user; no merge, push or release.

## Balanced resource surfaces — 2026-09-11

- New user feedback after `92d0add4`: prior modal layout was too plain; combine refined resource surfaces with compact engineering information, neither extreme. Reused accepted `vps-detail-resource` identity classes, added resource-only light fill/border and compact spacing, kept full entry/labels/notes, and replaced rigid label-column facts with compact wrapping pairs. No new stylesheet, budget changes, API changes or accepted non-modal/monitoring style changes.
- Main verification on Node 22.23.1: lint, 5 relevant files / 110 tests, build and CSS analysis passed. Current CSS source 332780 bytes / 2296 rules / 9121 declarations; production 310726 raw / 41792 gzip bytes. Existing candidate caps only; original-main CSS-policy debt and deferred JS issue remain unresolved.
- Browser initially retained stale Vite CSS; those initial pixels are not evidence for this candidate. Disabled cache and hard-reloaded, then confirmed actual resource padding `10px 12px`, dark translucent row fill, and flex facts. Viewed healthy service/domain surfaces across light/dark and desktop/narrow layouts, long service/domain values and legacy service/domain panels at 390px. No horizontal overflow in checked healthy/long resource modals; Escape returned to the service trigger. Existing tests cover retained copy/error/empty/write-ownership behavior; no real writes or new successful browser clipboard claim.
- New bounded discovery (base `92d0add4`, frozen three-file application diff): integration `openai-codex/gpt-5.6-sol` and Grok `xai-oauth/grok-4.6` completed with no findings. Gemini `google-antigravity/gemini-3.8-flash` returned429 and one availability retry produced no completed review. **NOT READY: required review lane unavailable.** No repair cycle or further reviewer spawned. Local checkpoint only, visual acceptance pending; no merge/push/release. Preview5320 stays running,5198 untouched; sample editing remains non-persistent.

- Further user-requested polish from `87e40ea2`: stable two-column secondary facts replace the inline run, domain hostname uses mono identity, label-echo missing values are shortened without changing facts, and noninteractive tile hover is removed. No new stylesheet or non-modal/monitoring changes. Main Node22 lint,110 relevant tests,build and CSS analysis pass: source332800 bytes/2296 rules/9121 declarations; production310746 raw/41797 gzip (`artifact://319`). Existing caps unchanged; prior budget-policy and JS debt remain.
- Fresh cache-disabled browser checked actual2-column computed tracks, service/domain at1440×1000 and390×900 in both themes, long names/URLs/registrar wrapping and legacy panels at390px. Checked healthy/long modals have no horizontal overflow; Escape restores the domain trigger. No real mutations. New bounded discovery on this user-driven candidate: integration and Grok completed on their intended models with no findings; Gemini429 plus one availability retry yielded no completed review. NOT READY remains due to the unavailable required lane; no repairs or extra reviewers. Saved local candidate only; visual acceptance pending,5320 stays running,5198 untouched,no merge/push/release.

## Original setup (before any product edit)

1. `sh scripts/setup-git-hooks.sh` 在将要提交的 worktree 里执行。
2. 从 `origin/main` 建 worktree，不要在脏的 `feat/ui-ux-craftsmanship-redesign` 上继续：

```bash
mkdir -p .worktree
git fetch origin
git worktree add .worktree/operator-ui-rebuild -b feat/operator-ui-observatory origin/main
cd .worktree/operator-ui-rebuild
sh scripts/setup-git-hooks.sh
python3 ./.trellis/scripts/task.py start 09-08-operator-ui-rebuild
```

3. 记下此时 `web/css-budget.json` 与 `web/bundle-budget.json` 的上限，写入本文件或任务 notes，作为 R7 的比较基线。

Baseline recorded 2026-09-08 from `origin/main` `2cda12e8`:

- css-budget: sourceBytesMax 322682, rulesMax 2199, declarationsMax 8842, repeatedSelectorTextsMax 159, literalColorDeclarationsMax 247, importantDeclarationsMax 11, productionCssBytesMax 304316, productionCssGzipBytesMax 39514
- bundle-budget: entryJsGzipBytes 110738, entryCssGzipBytes 38530, maxAsyncJsGzipBytes 48455, fontWoff2RawBytes 139072
4. 主 checkout 上 Gemini 的未提交改动保持不动。

## Phase order

每一刀结束后：相关单测 + 该刀涉及的 e2e 或浏览器实操（1440 与 390）。不把「全站将要变好」写成已完成。

### 1. Tokens + 反模式拆除

- 重写 `web/src/styles/tokens.css` 暗色身份与浅色映射；classic 变量指向新暗色。
- 删除 glow / 标题渐变 / 卡片 `::before` shine / `fadeUp` / `page-enter` / 中文 uppercase tracking 的消费。
- 扫 `var(--accent)` 与硬编码蓝，改到新令牌。
- 验证：三主题下打开登录页，无未定义变量（DevTools 计算值）。

### 2. 壳 + 登录 + 页面语法

- `Sidebar`：主项工作台/VPS/监控/探测，底设置，「更多」收其余。去分组标签。
- `TopBar`：补 `/archive`、`/command-audit` 等 `PAGE_TITLES`。
- `page.css`：`.page` / `.page__title` / `.page__actions` / `.page__work`。表不包 `page-panel`。
- 登录：去「候」印，词标 + 表单。
- 更新壳与登录测试。
- 浏览器：登录、折叠侧栏、打开「更多」、390px。

### 3. 工作台

- 第一屏：一句判断 + 一条具体 CTA + 可扫的异常列表。删除建议空话与重复标题。
- 更新 `DashboardPage` / `DashboardCommandSurface` 测试与 e2e heading/CTA。
- 浏览器：稳定/异常夹具若本地有；否则用正在跑的 center 数据走一遍主 CTA。

### 4. VPS 列表 + 详情

- 列表：高密度表，生命周期用 `StatusGlyph` + 文字，不要发光点。续费决策降噪但可扫。
- 详情：保留 write-owner 合同；视觉跟新语法；危险动作仍预览确认。
- 更新 `VPSPage` / `VPSDetailPage` 测试。
- 浏览器：打开一行详情、打开筛选、390px 确认无整页横滚。

### 5. 监控列表 + 详情

- 列表：一层工作面；趋势列（CPU/内存/磁盘）在 1440 第一屏可见，禁止再套一层 `page-panel` 把列裁掉。
- 详情：心跳/主机证据优先，受控动作不变。
- 更新监控测试与 e2e CTA（可改掉「从未关联 VPS 接入 agent」的措辞，但必须仍有一个可见主操作）。
- 浏览器：确认趋势列不用横滚才能看见。

### 6. 入口探测

- 去掉双套摘要。第一屏：标题、新建、表。Support surface 的解释段删除或收到抽屉。
- 状态列不要竖着挤成两字圆章。
- 更新 Targets 测试与 e2e。

### 7. 设置

- 阈值芯片可保留结构，视觉跟令牌。
- 保存条：不遮挡字段（页脚在滚动流末尾，或真正 dock 在滚动容器外并给列表留 padding）。禁止 FAB。
- JSON 覆盖规则：左对齐等宽。
- 更新 `SettingsPage` 测试；浏览器切监控策略/高级，确认保存条不挡输入。

### 8. 其余路由换皮

- 资产决策、订阅、服务商、归档、记录、事件、命令审计、对比、IP 质量：换令牌、去 eyebrow/shine、顶栏标题、不重做 IA。
- 抽查 1440/390 无 document 横溢。

### 9. 门禁与文档

- `make verify-web`
- `npm --prefix web run test:e2e`
- 预算若实测下降，把上限 **下调** 到新值。
- 更新 `docs/design/current/interface-language.md`、`component-patterns.md`；`.trellis/spec/web/styling-guidelines.md` 只在约定真正改变时改。

## Validation

```bash
cd .worktree/operator-ui-rebuild
make fmt-go   # 若未动 Go 可跳过
cd web && npm run lint && npm run test -- --run
cd web && npm run build && npm run css:analyze && npm run bundle:check
cd web && npm run test:e2e
```

浏览器：`web` dev `:5173` + 已有 center。每刀记录：URL、看过的路由、视口、服务是否还在跑。

## Risky files

- `web/src/pages/VPSDetailPage.tsx` 与 `vps-detail/*`：异步所有权，禁止顺手改 refresh/owner。
- `web/src/styles/tokens.css`：漏一个主题就会破洞。
- `web/e2e/core-routes.spec.ts`：文案一改就红，必须当产品合同一起改。
- `web/css-budget.json`：禁止上调。

## Rollback points

- Phase 1–2 可独立回退（令牌/壳）。
- Phase 4 若详情写入测试红，先还原详情视觉，保留列表。
- 整个分支不合并则生产不受影响。

## Stop conditions

- 不要为过 `css:analyze` 抬预算。
- 不要写「全站已完成」除非 AC1–AC7 都有证据。
- 不要把 Gemini 分支 cherry-pick 进来。

## Local acceptance checkpoint — 2026-09-22

- 用户要求先补齐本地验收，再提交、归档和记录；现全部本地工程验收已通过，工作成果已提交为 `0c04c5891d55f53c6e90f1626c66479aa365186d`，按该授权归档本任务。没有 push/merge、远程部署或访问既有数据库。目标分支 `feat/operator-ui-observatory`；提交包含新增的 `globalAssetSearch.ts` 和 `vps-inventory-archive-restore.spec.ts`。两个 heartbeat 任务不在本次归档范围。
- 已批准的首版受控试用策略覆盖历史 R7/AC5 的数值预算要求：全部 13 项预算保留为可见参考线，npm 使用 advisory；CLI 默认 enforce，结构/owner/解析/入口错误仍失败。不继续 CSS 优化，不移植隔离 A/B 实验，不把旧 F08 的 NOT PROVEN 改写为通过。
- Go：使用 `go.mod` 声明的 `go1.26.2`，`make fmt-go vet-go` 和完整 `go test ./agent/... ./cmd/... ./db/... ./internal/...` 范围通过，75 个包通过、7 个包没有测试。普通 Go 测试不代替环境门控的真实 PG 证据。由于 tmpfs 用户配额，编译使用 home 分区 `GOTMPDIR`，测试通过 `-exec 'env -u GOTMPDIR TMPDIR=/tmp'` 恢复短临时路径；未 skip 测试、未修改 socket/工作区安全限制。fmt 仅额外对齐 `store/runtime_facts.go` 五个局部声明。
- PNG：原测试固定标准库压缩字节 SHA-256，改为可解码 PNG、2×1 尺寸和逐像素等于实际 JPEG 解码结果；保留输出上限、媒体类型、去元数据断言。生产图片处理及内容寻址摘要没有改变。附件包全测通过；修正的用例同时在宿主机 Go1.27.1 上通过。runtime-stream/handler 聚焦 race 检查通过。
- Web：Node22.23.1 的精确 `NODE_ENV=production make verify-web` 通过；235 文件 / 2088 测试，lint 0 错误、3 条既有 MonitoringDetailPage 警告；覆盖 statements83.38%、branches76.9%、functions83.9%、lines87.41%；生产构建、bundle advisory、CSS advisory 全部完成，5 项 CSS 超限警告保留。
- Chromium：生产构建后的完整 146/146 通过，0 失败、0 跳过，1 worker、0 retry。修复仅为本次浏览器 `TMPDIR` 放到 home 分区，未改 Playwright/axe/断言。此前 10 项字体共享内存配额崩溃已关闭。预览 `127.0.0.1:4175` 已停止；本地 fixture API 浏览器证据不等于真实部署或真实库存验收。
- PostgreSQL：仓库 strict runner 创建并清理独立、带 ownership label 的临时容器，未连接已有数据库。runtime facts/store、runtime summaries、sparklines、stream eligibility 4 项真实 PG 测试通过。PG16.0/16.6/16.12 的 `TestPostgresIntegrationAppACLR2` 与 `TestPostgresIntegrationAppACLCurrent` 两个 CI anchors 均 RUN/PASS，0 skip/fail。
- 独立双审：当前完整未提交候选（93 个已跟踪改动 + 2 个新增文件）及直接消费者 discovery 已完成。主会话 native hub 元数据确认 integration-reviewer=`openai-codex/gpt-5.6-sol:high`，grok-reviewer=`xai-oauth/grok-4.7`；两路均无有效发现，无修复循环。审查期间冻结实现、不共享兄弟发现。此前预算策略双审结论仍保留，不重新开启。
- 依赖扫描：`npm audit --omit=dev` 为 0 漏洞；完整 audit 仍报告既有开发工具依赖 10 项（6 high、4 moderate）。未改依赖或锁文件，不宣称开发工具依赖全部无告警。
- 镜像下载阻塞已关闭：用户授权重试后，未修改 Dockerfile/依赖锁/TLS/校验和，`docker build --network=host --build-arg VERSION=dev` 完成全部 37 步。工作提交对应本地镜像 `houfeng-local-acceptance:0c04c589`，image ID `sha256:aca5798533b200ef35aedcbbb2b7b496516ac585872c54ebffedd14366423ee2`（linux/amd64）。只读、无网络的容器检查确认 UID10001、三个二进制/entrypoint、SPA、CA 与 Poppler；未启动已配置的真实 center、未发布镜像。历史 EOF 和首次重试超时保留为已解决环境证据。
- 后续真实部署验收仍包括 systemd/部署配置、正式签名发行资产和安装器、真实 agent enroll/sync、Target/ProbeItem 观测和 incident 路径。原 AC 中的人工作品/视觉判断不因自动化测试全绿被自动勾选。
- 原始日志位于本次主会话 `2026-09-22T01-15-14-584Z_01a0c6ae-6b18-72a2-a14a-cfc81bba2351`：Go `artifact://231`、Web `artifact://237`、Chromium `artifact://122`、PG16.0 `artifact://239`、历史镜像失败 `artifact://301` 与 `artifact://328`、成功镜像 `artifact://361`、精确工作提交镜像重建 `artifact://377`；汇总 `local://local-acceptance-evidence.json`。提交前仅清除两个前端文件的多余 EOF 空行；此后镜像重建产物 ID 与此前一致，未产生行为变化。
