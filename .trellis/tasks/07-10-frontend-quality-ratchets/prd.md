# 前端质量 ratchet 与浏览器门

## Goal

把本轮已经修复的质量缺口转成可重复、失败即阻断、证据可审计的持续门禁：单元覆盖率、严格类型、真实 Chromium 交互、axe、CSP/console/network、bundle/font/CSS 预算、规范同步和认证 staging smoke 必须共同防止同类问题回流。

## User Value

- 后续视觉、结构或依赖调整不能在 633 个单测仍全绿时重新引入错误计数、false-empty、嵌套 Modal 连关、CSP violation、窄屏裁切或非语义键盘死路。
- PR 作者可以用仓库命令在本地复现 CI，不依赖某台开发机上临时安装的 Python Playwright 或手工截图。
- reviewer 能区分 mock contract、部署产物浏览器证据与真实认证 staging 证据，不再把“页面能打开”扩大成生产已验证。
- bundle、字体和 CSS 只能在显式审阅预算后增长，不能静默累积新的全局负担。

## Confirmed Facts

### Repository Baseline

- 实施基线为 `origin/main` commit `5633102739d22f18ae7c52c89e19b6e7d2f2a4d7`；Node 固定为 `22.23.1`，npm 为 `10.9.8`。
- `env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify` 已通过：Go fmt/vet/tests、115 个 Vitest 文件 / 769 tests、ESLint、strict TypeScript 和 production build 全绿，npm install/audit 输出 0 vulnerabilities。
- 基线 main CI run `29154612059` 在同一 commit 上通过 go、web、docker-image；Release Please run `29154612051` 成功且没有生成新的 release PR。
- `web/package.json` 当前没有 `@vitest/coverage-v8`、`@playwright/test`、`@axe-core/playwright`，也没有 `test:coverage`、`test:e2e`、bundle budget scripts。
- `.github/workflows/ci.yml` 只有 go、web、docker-image 三个 job；web 只执行 `NODE_ENV=production make verify-web`。
- 当前浏览器 helper `scripts/visual_evidence.py browser-sanity` 依赖开发机 Python Playwright/CDP，明确不在 CI；它继续作为本地/真实数据辅助，不是持久化 gate 的替代品。

### Current Measurements

- fresh production build 实施快照：入口 JS raw/gzip `355,132 / 110,565` bytes；入口 CSS raw/gzip `289,964 / 37,135` bytes；最大异步 route chunk（Asset Decisions）raw/gzip `136,551 / 31,953` bytes。
- 自托管字体为 7 个 WOFF2，共 `139,072` bytes。Task 9 final CSS 为 26 files / `311,063` source bytes，production raw/gzip `293,270 / 38,119` bytes。
- bundle/font 数值来自 Task 6–9 全部合并、发布和归档后的 fresh `origin/main`，是 deterministic checker 的首次 baseline 候选；Task 10 复用 Task 9 analyzer/budget，不另建 CSS baseline。
- coverage 尚未有可信基线。规划期无跟踪安装尝试暴露 npm 临时 peer-tree/jsdom 解析限制，因此 `0/Unknown` 不得写入预算；正式实现必须先把匹配 Vitest 版本的 provider 写入 package/lockfile，再由 `npm ci` 生成基线。

### Type And Lint Debt

- fresh `noUncheckedIndexedAccess` 探针有 176 errors：pages 75、components 41、styles 34、app 16、lib 7、security 3。
- fresh `exactOptionalPropertyTypes` 探针有 33 errors：pages 23、lib 4、app 3、components 3。
- 两项同时启用共有 209 errors：pages 98、components 44、styles 34、app 19、lib 11、security 3；不能用一次全局开关加大批 `as`、非空断言或 eslint disable 消音。
- ESLint 当前只使用非 type-aware recommended config，没有 `no-floating-promises`、`no-misused-promises`、`await-thenable` 等类型信息规则。

### Spec And Environment Drift

- `.trellis/spec/web/styling-guidelines.md` 已由 Task 9 更新为真实 CSS owner/AST 合同；`directory-structure.md` 仍把不存在的 `styles/atoms.css`、`styles/pages.css`、`app/layout/layout.css` 写成现状，Task 10 必须按最终真实树修正。
- `.trellis/spec/web/quality-guidelines.md` 和 `docs/operations/ui-preview-and-browser-sanity.md` 仍明确说仓库没有且普通任务不得加入 Playwright；Task 10 正是批准这项架构变更并同步规范的责任边界。
- 仓库当前没有 GitHub `staging` environment、staging variables 或 staging secrets，且 GitHub API 报告 `main` 尚未启用 branch protection。缺少真实环境/凭据时可合并可重复门禁实现，但本 task 必须保持未完成，不能归档，也不能把 mock 证据标记为 staging 通过；required-check protection 在新 checks 实际出现后配置。
- GitHub 仓库为 public，Actions 默认 token 权限为 read；`workflow_dispatch` 可选择运行 ref，因此仅声明 `environment: staging` 仍不足以阻止 feature ref 上被修改的 workflow 请求 environment secrets。
- 用户已于 2026-07-10 确认采用 `workflow_dispatch + GitHub staging environment` 管理 staging URL、账号凭据和审计 artifact；执行机制已定稿。

## Requirements

### 1. Dependency And Activation Gate

- 直接依赖 `frontend-csp-compat`、`frontend-accessibility-contracts`、`frontend-responsive-workflows`、`frontend-css-ownership` 全部合并、归档并完成 post-merge main CI。
- `frontend-css-ownership` 又依赖 `frontend-asset-decisions-domains`；因此 Task 8 是 Task 10 的传递前置。
- Task 5、6、7、9 均已归档并完成 post-merge main CI；Task 9 历史包含已完成 Task 8，Task 10 已从其归档提交后的 fresh `origin/main` 激活。
- Task 6–9 只需留下各自 focused tests 与本地浏览器证据；它们计划中尚不存在的 `npm run test:e2e` 由本任务统一实现和接管，不能倒置依赖。

### 2. Coverage Ratchet

- 使用与 Vitest 精确兼容并写入 lockfile 的 V8 provider；coverage 必须包含全部 production `src/**/*.{ts,tsx}`，排除 test/setup/type declaration/test fixture，而不是只统计被测试 import 的文件。
- 首次可信 run 记录 statements、branches、functions、lines 的 covered/total 与百分比；全局阈值取 fresh baseline，不设置任意 80%，后续不得下降。
- Modal stack/focus、Dashboard model/RemoteState、API request transport、auth client/context、Task 8 产出的 Asset command hooks 每个文件 branch coverage 不低于 90%。关键路径清单必须使用存在性 contract 防止重命名绕过。
- 为了让 API request helper 的 90% 有真实边界，允许把 transport/error/JSON primitives 从 968 行 `api.ts` 最小提取到无 wire-shape 变化的内部模块；现有 `api.ts` 导出接口保持兼容。
- 不用 coverage ignore、全局 exclude、测试专用生产分支或不安全 cast 提高数字。

### 3. Chromium Browser Gate

- 只引入 Chromium Playwright，版本由 lockfile 固定；CI 安装的 browser revision 与 `@playwright/test` 匹配。
- 固定九条 top-level core routes：`/`、`/vps`、`/asset-decisions`、`/monitoring`、`/targets`、`/events`、`/providers`、`/subscriptions`、`/settings`。
- 固定三个 viewport：`1440x1000`、`1024x768`、`390x900`。九条 route 均验证非空白、主 workflow 可达、document 无横向溢出、关键命令文字不裁切；宽表只能在有可访问名称和键盘入口的局部区域滚动。
- fixture router 必须 fail closed：未声明的 method/path/query 记录为 unexpected 并让测试失败，不能默认返回 `[]`/`null` 制造 false-empty。
- 覆盖 Dashboard 五状态与局部 503、PageState loading/empty/error/success、Modal 栈、Tabs/Menu/skip-link 键盘流程、Settings/Asset/Providers 响应式合同。
- axe 只以 serious/critical 为阻断级别，但不得用全局 disable 或宽泛 selector allowlist；键盘行为必须用真实按键和焦点断言，不能用 axe 代替。
- 每个 browser test 统一收集 `console.error`、page error、unhandled rejection、unexpected request failure/HTTP error 与 `securitypolicyviolation`；除测试显式注入的状态码外全部必须为零。
- CSP header 必须与 `internal/center/http/csp-policy.txt` 精确一致，不加入 `unsafe-inline`；浏览器门读取同一 policy source。
- 不提交像素级 golden screenshot 或 bulk raster；CI 只在失败时上传 screenshot/trace/report，布局使用语义、几何、可见文本与 overflow 断言。

### 4. Bundle, Font And CSS Budgets

- production build 后稳定计算入口 JS gzip、入口 CSS gzip、最大非入口 JS chunk gzip、全部 WOFF2 raw bytes；budget 取 Task 6–9 后 fresh baseline，只允许下降或经单独解释/审阅显式更新。
- budget check 必须识别带 hash 的构建文件，不硬编码具体 hash；缺失入口、零匹配字体或多入口歧义必须失败。
- Task 9 拥有 PostCSS direct dependency、CSS AST analyzer 与 `web/css-budget.json`；Task 10 只复用并接入 CI，不复制 analyzer、不另建第二份 CSS baseline。
- coverage、bundle、font、CSS AST、CSP/browser checks 必须成为受保护 PR 的 required CI surface。

### 5. Type And ESLint Ratchets

- 用可执行的 scoped tsconfig 按 `lib -> atoms -> dashboard -> routes` 顺序偿还两项 compiler option；每一层先测试/修复再扩大，最终在主 `tsconfig.app.json` 同时启用 `noUncheckedIndexedAccess` 与 `exactOptionalPropertyTypes`。
- type-aware ESLint 先作用于 `src/lib/**` 与 Task 8 新增 `src/pages/asset-decisions/hooks/**`，至少覆盖 floating/misused promises、await-thenable、unnecessary assertion 与 switch exhaustiveness；保持现有 lint 零 warning。
- 禁止全局 eslint disable、`any`、`unknown as T`、无证据非空断言或把 optional 字段一律扩成 `| undefined` 来消音；修复必须保持 JSON omission/null 语义。

### 6. Specs And Operations Contract

- 使用 `trellis-update-spec` 根据最终真实树更新全部受影响的 web specs；至少覆盖 Node/command portal、coverage、type-aware lint、Modal、Dashboard、CSP、browser、CSS owner 与目录结构。
- 同步更新 `docs/operations/ui-preview-and-browser-sanity.md`：正式 CI Playwright 与保留的 local Python helper 分层，九 route/三 viewport 为 broad gate，detail routes 仍按任务增量验证。
- 不引入 OpenAPI/JSON Schema/codegen；只有后端正式拥有 schema 时另立任务。

### 7. Authenticated Staging Gate

- staging gate 固定由 `.github/workflows/frontend-staging-smoke.yml` 的 `workflow_dispatch` 触发，environment 名称固定为 `staging`。
- URL 使用 environment variable `HOUFENG_STAGING_BASE_URL`；账号使用 environment secrets `HOUFENG_STAGING_USERNAME`、`HOUFENG_STAGING_PASSWORD`；workflow 必须要求 `expected_version` 输入。
- `staging` environment 的 deployment branch/ref policy 只允许 `main`；workflow 先由无 secrets 的 preflight 拒绝其他 ref，再让受 environment 保护的 job 读取凭据。`permissions` 保持 `contents: read`，不得授予写权限。
- staging workflow 使用固定 concurrency group 串行执行且 `cancel-in-progress: false`，避免两个 run 交错保存/恢复设置或新 run 在旧 run 清理前将其取消。
- staging 必须运行部署版本校验（`/api/healthz.version`）、真实登录、真实数据基线 route smoke、Dashboard 五状态、嵌套确认取消、可回滚设置保存、503/慢请求 fault injection、长文本/大列表和主题切换。
- fixture/fault injection 只能证明部署前端在真实 origin/CSP/auth 下的降级行为，必须与真实数据步骤分栏记录，不能称为后端或生产数据通过。
- 证据至少包含 commit/tag、browser version、主文档安全响应头、sanitized console/network 摘要和成功/失败截图。不得记录 cookie、Authorization、密码、token、response body 或敏感 query。
- workflow 上传命名稳定、限期保留的脱敏 audit artifact；长期摘要写回 Trellis task/parent，避免 artifact 到期后丢失结论。
- 设置保存必须先快照原值并在 `finally` 恢复；恢复失败立即阻断并报告，不继续归档。
- staging 不得指向 production；环境、专用账号和非敏感测试数据由 GitHub environment 或操作者提供。

## Out Of Scope

- 不改变后端 JSON wire shape、业务状态机、权限模型或数据库数据语义。
- 不引入 OpenAPI/JSON Schema/codegen、React Query/Redux/Zustand、跨浏览器 Firefox/WebKit matrix 或 Lighthouse 性能结论。
- 不进行新的页面视觉重设计，不借测试任务修 unrelated UI；发现真实业务差异回到对应 owner task。
- 不把 mock Playwright、local CDP 或 staging fault injection 描述为生产数据验证。
- 不提交长期维护的像素截图基线或包含 staging 敏感数据的 artifact。

## Acceptance Criteria

- [x] Task 5、6、7、9 均已归档且 post-merge main CI 通过；Task 9 已包含 Task 8 输出。
- [x] `npm ci` 可重复安装固定的 coverage/Playwright/axe 依赖，`npm audit --include=dev` 为 0 vulnerabilities。
- [x] coverage 包含全部 production TS/TSX；全局四项不低于 fresh baseline。
- [x] 每个关键文件存在且 branch coverage >= 90%，重命名或零匹配会失败。
- [x] `noUncheckedIndexedAccess` 与 `exactOptionalPropertyTypes` 在主 app tsconfig 启用；type-aware lint 对 lib/Asset hooks 生效且零 warning。
- [x] 九 route × 三 viewport Chromium matrix 全绿，无 page/console/CSP/unhandled/unexpected-network error，无 document 横向溢出或关键文字裁切。
- [x] Dashboard 五状态、PageState 四态、Modal/Tabs/Menu/skip-link 键盘流程均有 Playwright 回归。
- [x] AppShell、Dashboard、Settings、VPS/Asset 关键 surface 的 axe serious/critical 为零。
- [ ] 入口 JS/CSS gzip、最大 async JS gzip、字体 raw budget 与 Task 9 CSS AST budget 在本地和 CI 阻断增长。
- [ ] `make verify-web`、browser job、Docker job 与 main post-merge CI 都使用 Node `22.23.1` 并通过。
- [x] 任务启动时的最新 Vitest file/test count 只增不减，或每个替换都有等价覆盖证据；历史 74/578 与当前规划 86/633 都不得无说明回退。
- [x] `.trellis/spec/web/*` 和 browser operations doc 与真实目录、命令、owner 和证据层级一致，不再引用已删除 CSS/layout 路径或“仓库无 Playwright”。
- [x] `frontend-staging-smoke.yml` 仅能通过 `workflow_dispatch` 运行，使用 GitHub `staging` environment，并且不会在 PR/fork CI 中暴露凭据。
- [ ] environment 仅允许 `main` deployment ref；非 `main` dispatch 在读取 environment secrets 前失败；staging run 串行且不会被新 run 取消。
- [ ] 真实认证 staging run 对目标 release/version 通过并保存脱敏证据；没有环境/凭据时本项保持未勾选，task 不归档。
- [ ] Gate C 在同一个集成版本通过，并把 run、commit/tag 和残余风险写回 parent task。

## Confirmed Staging Decision

- 用户已确认独立 `workflow_dispatch` + GitHub `staging` environment 方案，不再保留人工本机入口作为关闭 Gate C 的替代路径。
- 当前仓库尚未创建该 environment，也没有 URL/username/password 配置；这些是 Phase 2 的外部配置前置，不在 planning 阶段写入或索取 secret value。
- environment 的 ref policy 固定为仅 `main`，并通过 non-secret preflight 与非取消式串行 concurrency 做纵深防护。
- audit artifact 保存浏览器/路由/视口、允许的响应头、脱敏 console/network 计数和截图；Trellis task 永久记录 run URL/id、artifact 名、expected/observed version 与结论。
