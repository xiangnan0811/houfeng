# Implement — operator UI night observatory

## Current checkpoint — 2026-09-11

- Continue only in the existing `.worktree/operator-ui-rebuild` on `feat/operator-ui-observatory`; the setup instructions below describe the original start, not a request to create another worktree or edit main.
- User-accepted visual baseline: workbench, VPS list, VPS detail **excluding modals**. Preserve these surfaces while continuing other pages; this is not whole-site acceptance.
- Preserve the complete current source graph, including new components/hooks/styles. Shared shell/theme changes and other page adaptations are checkpointed, not declared visually finished.
- All five dirty main-checkout VPS-detail fragments are included or structurally superseded by this candidate. The original fragments contain incomplete duplicate JSX/type declarations; do not reapply them. They are retained in Git stash `fd1eda668d0ae35771af771715225e702fb8a0f1`.
- This consolidation introduces no application-code changes. Fresh Node 22.23.1 checks: lint, 215 test files / 1708 tests, production build, and CSS analysis pass. Browser sanity viewed the workbench, populated VPS directory and rich-resource VPS detail using the existing read-only local sample preview.
- Previous evidence remains tied to unchanged source: 647 frozen source/e2e/design/changelog files match. The earlier 65 E2E, 22 paired visual cases and 24 read-only paths are existing evidence, not newly rerun here. The three-model review covered the bounded last closure diff, not a fresh audit of the whole reconstruction.
- **Not release-ready:** entry JS is 111663 gzip bytes against the unchanged 110738 limit. The user explicitly deferred this issue. Existing CSS-budget increases versus main are also preserved as unresolved budget-policy debt, not newly approved here; CSS passing means the current candidate limits, not the original main limits.
- Next work: one page or modal scope at a time, relevant browser verification, then a small commit. Do not restart the accepted three surfaces, mark the entire task complete, merge main, or push without the corresponding decision.

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
