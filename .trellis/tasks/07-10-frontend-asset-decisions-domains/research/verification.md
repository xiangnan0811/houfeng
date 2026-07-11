# Asset Decisions 领域拆分验证证据

## 验证坐标

- 日期：2026-07-11（Asia/Shanghai）
- 分支：`codex/frontend-asset-decisions-domains`
- 基线：`origin/main@a57458dc30ca9dc2ee4333563fdd6ce19e90a6e5`
- Node：`22.23.1`（`.node-version` 精确 pin）
- 浏览器：Chrome `150.0.7871.114`，CDP protocol `1.3`
- 浏览器数据源：production `web/dist` + `mock-api asset-workflows`；这是 local-only 前端证据，不是 staging / 真实资产证明。

## 实现与结构核对

- 删除 2,705 行 `AssetDecisionsPageContent.tsx`；`AssetDecisionsPage.tsx` 为 394 行 composition coordinator，无 API import、router hook 或 `useEffect`。
- 七个 controller entry 均存在并只公开 `{state, commands}`；API owner、唯一 router owner、controller / presentation 依赖方向由 TypeScript AST repository contract 守护。
- controller 行数为 route 182、portfolio 92、groups 207、manual groups 586、templates 351、records 492、renewal queue 321，均低于 600；route-private production 文件均低于 800。
- 原 37 条综合 workflow 按 route / groups / manual / templates / records / renewal 拆分，名称和断言保留；架构守护包含 synthetic pass/fail fixture，不能以 wrapper、改名或路径白名单绕过。
- 默认 11 GET、open key 四个 filtered GET、四类 detail GET、所有 mutation method/path/body、renewal invalidation refresh multiset 均由 request contract 与浏览器证据覆盖；后端 wire shape 未变。

## 本地质量门

以下命令在当前实现与 spec 更新后通过：

```bash
NODE_ENV=test npm --prefix web run lint
NODE_ENV=test npm --prefix web run test -- --run
NODE_ENV=production npm --prefix web run build
npm --prefix web audit --include=dev
git diff --check
```

结果：113 个 Vitest 文件、758 个测试全部通过；focused Asset Decisions 集合为 25 个文件、122 个测试；npm audit 为 0 vulnerabilities；strict TypeScript 与 Vite production build 通过。

canonical clean-install gate 使用：

```bash
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify-web
```

Node 24.18.0 会在 install 前被 preflight 正确拒绝；精确 Node 22.23.1 下 clean install、lint、全量测试与 production build 通过。

## Local-only Chromium 门

标准 `scripts/visual_evidence.py browser-sanity` 因本机没有 Python Playwright 不可用；按 Web quality spec 改用已安装 Chromium + 原生 CDP/WebSocket，未修改 package / lockfile，也未提交截图。

### Geometry

| Viewport | inner | document width | 横向页面溢出 | 关键文字裁切 | 无局部 scroll owner 的离屏命令 |
| --- | --- | ---: | --- | --- | --- |
| `1440x1000` | `1440x1000` | 1440 | 0 | 0 | 0 |
| `1024x768` | `1024x768` | 1024 | 0 | 0 | 0 |
| `390x900` | `390x900` | 390 | 0 | 0 | 0 |

### Modal / keyboard

- 自动组：聚焦第二个“查看组”→打开详情→Tab 保持在 dialog→Escape；焦点回带相同 trigger marker 的“查看组”，URL 回 `/asset-decisions`，body overflow 解锁。
- 自定义组合：聚焦列表“查看”→打开详情→Escape；焦点回同一组合入口，body overflow 解锁。
- 保存记录：聚焦列表“查看”→打开详情并完成状态/成员跟进→Escape；焦点回同一记录入口，body overflow 解锁。
- 成员移除与模板归档确认：父 dialog 为 `aria-hidden=true` + `inert`，顶层 `alertdialog` 为 `aria-modal=true`；第一次 Escape 回父层原按钮且 body 仍锁定，确认 mutation 后父层保持可用。
- auto→manual 只保留一个可见顶层 dialog；所有最终关闭路径 body overflow 均恢复。

### Workflow / request evidence

- auto→manual：`POST /api/asset-decisions/manual-groups` 保持 `source_type=auto_group`、source id、window、scenario、title/goal/note。
- manual member removal：仅显式确认后 `DELETE /api/asset-decisions/manual-groups/admg_mock_created/members/vps_ams_core`。
- template archive：`PATCH /api/asset-decisions/scenario-templates/adt_mock_manual_primary_standby` body 为 `{"status":"archived"}`。
- record status / follow-up：同一 record endpoint 分别收到 `{"status":"in_progress"}` 与单成员 `followup_status=blocked`、`followup_note=等待迁移窗口`。
- renewal：`PATCH /api/vps/vps_ams_core` body 精确为 `{"renewal_decision":"migrate","renewal_reason":"move to Osaka"}`；成功后 GET multiset 精确为 overview、groups、manual groups、records、templates、两条 subscriptions、四条 VPS，共 11 条。
- diagnostics：console error 0、page exception 0、network failure 0、HTTP >=400 0、CSP issue 0。
- 主文档响应头包含严格同源 policy：`default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'`。

## Bug Analysis: URL revalidation 卸载 Modal 触发器

### 1. Root Cause Category

- **Category**：E - Implicit Assumption；同时暴露 D - Test Coverage Gap。
- **Specific Cause**：route filter 故意依赖完整 `searchParams` identity，以在 open key 变化时重发四个 filtered GET；拆出的 controller 又用 `settled.filter === filter` 判断 UI 是否 current，把“请求 identity 变化”误当成“业务 filter 变化”。列表短暂切到 loading，卸载 Modal restore target，关闭后焦点只能落到 `body`。

### 2. Why Fixes Failed

1. 共享 Modal 单元测试通过：它能正确跳过断开的 target，但不能为业务层猜测 successor；缺少 route + async list + Modal 的集成断言。
2. 首个 group-only 修复验证了根因，但同一引用比较还存在于 portfolio、manual groups 和 records；`trellis-check` 的 same-layer consistency 扫描发现这是 incomplete scope，并用两个新 workflow RED 证实。
3. 第二轮 CDP 首次在续费步骤失败是自动化坐标落在进入动画中的相邻链接；请求日志显示跳转 `/vps/vps_ams_core`，稳定后直接激活目标按钮会打开 Modal。脚本改为等待 `.animate-in` animations settled 后继续真实 pointer hit-test，生产代码未为工具误差让步。

### 3. Prevention Mechanisms

| Priority | Mechanism | Specific Action | Status |
| --- | --- | --- | --- |
| P0 | Architecture | 用共享 `assetDecisionFilterKey` 分离 request identity 与 UI semantic currency；四个 filtered owner 一致使用 | DONE |
| P0 | Test coverage | group/manual/record workflow 覆盖“聚焦入口→打开→Escape→同实体入口恢复”；portfolio 覆盖同值 identity revalidation | DONE |
| P0 | Contract | request tests 与 CDP 同时断言四个 filtered GET、renewal 11 GET，防止以减少请求掩盖焦点问题 | DONE |
| P1 | Documentation | 更新 directory/component/state/quality 四份 Web specs，记录 controller owner、语义 key、AST 与浏览器证据合同 | DONE |
| P2 | Persistent browser gate | Task 10 将 Playwright/axe、固定 browser 版本与 artifact 进入 CI；本任务证据保持 local-only | 由 Task 10 承接 |

### 4. Systematic Expansion

- **Similar Issues**：automatic groups、manual groups、records 和 portfolio 使用同一 filtered identity；已统一修复。templates 不消费该 filter，renewal queue 使用独立 context/revision，不应套用此 key。
- **Design Improvement**：controller 的 loading 表示“没有当前语义结果”，不是“任何 request 正在进行”；如果未来需要展示后台刷新，应新增显式 `refreshing`，不能复用 loading 并卸载交互 surface。
- **Process Improvement**：route-state 重构必须同时冻结网络 inventory、DOM 连通性和 `document.activeElement`；只测请求或只测 Modal primitive 都不足以证明组合行为。

### 5. Knowledge Capture

- [x] `.trellis/spec/web/directory-structure.md`
- [x] `.trellis/spec/web/component-conventions.md`
- [x] `.trellis/spec/web/state-and-data.md`
- [x] `.trellis/spec/web/quality-guidelines.md`
- [x] task-local regression tests 与本验证记录

## 交付门说明

实现 PR：[houfeng#363](https://github.com/xiangnan0811/houfeng/pull/363)。本文件当前记录本地实现与 mock production-dist 证据；PR checks、合并后 main CI、Release Please、GitHub Release、multi-arch image digest 与 released `/app/web/dist` 复验仍是 Task 8 的后续必经门。完成后把具体 run id、version、digest 和 released-dist 结果追加到本文件，再归档任务。
