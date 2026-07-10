# 质量 Ratchet 与浏览器门实施计划

> **For inline Codex execution:** implementation must use `superpowers:executing-plans`, `superpowers:test-driven-development`, and `superpowers:verification-before-completion` task by task. The approved inline mode forbids runtime sub-agent dispatch.

**Goal:** 在前置修复稳定后，把 coverage、严格类型、Chromium/axe/CSP、bundle/font/CSS 与认证 staging 证据变成可重复的最终质量门。

**Architecture:** `make verify-web` 负责 lint、coverage、TypeScript、build 和静态预算；独立 CI browser job 对 production preview 运行 fail-closed Playwright；独立 staging gate 对目标 release 做真实认证与明确标注的 fault injection。所有 baseline 都从 Task 6–9 合并后的 fresh `origin/main` 生成。

**Tech Stack:** Node 22.23.1、TypeScript 6、Vitest 4/V8 coverage、React Testing Library、Playwright Chromium、axe-core、Vite preview、PostCSS AST（Task 9 owner）、GitHub Actions。

---

## 0. Workflow State And Hard Preconditions

当前 task 必须保持 `planning`。本文件通过 review 不等于允许启动。

- [x] 用户已于 2026-07-10 审阅最终 `prd.md`、`design.md`、`implement.md` 并批准在全部前置条件满足后启动；本项不豁免以下依赖门。
- [ ] `frontend-csp-compat`、`frontend-accessibility-contracts`、`frontend-responsive-workflows`、`frontend-css-ownership` 均已归档；Task 9 的历史包含已完成 Task 8。
- [ ] 每个前置 task 的 implementation PR、release/main CI 与 post-merge checks 均成功。
- [ ] 从 fresh `origin/main` 创建/刷新 `codex/frontend-quality-ratchets` 独立 worktree；不修改本地 `main`。
- [ ] 在 worktree 运行 `sh scripts/setup-git-hooks.sh`，确认 Node `22.23.1`，确认 working tree clean。
- [ ] 只有上述条件满足后运行 `python3 ./.trellis/scripts/task.py start 07-10-frontend-quality-ratchets`。

Inline 执行模式保持不变，不分派运行时子代理。Task 6–9 文档中的 `npm run test:e2e` 是由本 task 建立后的最终合同，不作为那些前置 task 当时的启动条件；前置 task 使用 focused tests + local browser evidence。

## 1. Fresh Inventory Before Any Implementation

在最新前置集成版本重做规划期测量，旧数值只用于解释差异。

- [ ] 记录 commit、Node/npm、`git status`、测试 file/test count。
- [ ] 运行 full baseline：

```bash
env -u NODE_ENV PATH=/home/murray/.nvm/versions/node/v22.23.1/bin:$PATH make verify
npm --prefix web audit --include=dev
git diff --check
```

- [ ] 读取 Task 6/7/8/9 最终 artifacts 与实际文件树，建立交付清单：Tabs/Menu/skip-link selectors、responsive critical texts、Asset command-owning hooks、CSS analyzer/budget/owner paths。
- [ ] 重跑两个 TypeScript probes 并保存按目录/错误码统计；不能沿用规划期 121/35/156 数字做完成判断。
- [ ] fresh production build 后记录 entry JS/CSS、最大 async JS、WOFF2 与 Task 9 CSS 指标。
- [ ] 若 baseline 本身失败，先回到对应前置 owner 修复；Task 10 不在失败基线上制造 budget。

**Rollback/stop:** baseline failure、依赖 task 未归档或 Task 9 budget 缺失时停止，不开始安装新依赖。

## 2. Gate Source Contracts First (RED)

先写能证明 wiring 还不存在的失败测试，再添加实现。

### Files

- Modify/test: `scripts/check-web-toolchain.test.sh` 或新增同层 `check-web-quality-gates.test.sh`。
- Create test: bundle analyzer synthetic dist fixture test。
- Create test: coverage budget critical-path existence contract。
- Modify test: `web/vite.config.test.ts` / CSP source contract only where shared policy wiring needs assertion。

### Checklist

- [ ] RED：package 缺少 `test:coverage`、`test:e2e`、bundle check scripts 和三项 direct dev dependencies。
- [ ] RED：coverage budget 缺全局四项、任一 approved critical path 不存在或 branch <90。
- [ ] RED：bundle checker 对缺入口、多入口、零字体和超预算 fixture 返回非零，并报告 metric/actual/limit。
- [ ] RED：CI 不包含独立 browser job，或 browser job 未读取 `.node-version` 时失败。
- [ ] RED：CSP browser contract 不读取仓库唯一 policy source 时失败。
- [ ] 保持测试检查结构/行为，不写只能匹配一段 YAML whitespace 的脆弱正则。

**Focused gate:** 只运行新增 source/synthetic tests，确认它们因预期缺失而红，不接受语法错误型 RED。

## 3. Install Exact Test Dependencies And Ignore Outputs

### Files

- Modify: `web/package.json`, `web/package-lock.json`, `web/.gitignore`。

### Checklist

- [ ] 查询并选择彼此兼容的 stable `@vitest/coverage-v8`（必须与已锁 Vitest 一致）、`@playwright/test`、`@axe-core/playwright`。
- [ ] 用 normal tracked install 写入 package/lockfile；不使用规划期失败的 `--no-save` 临时树作为证据。
- [ ] 添加 `coverage/`、`playwright-report/`、`test-results/` 和 browser temp state ignore；不放宽仓库 raster policy。
- [ ] `npm ci --include=dev` 从空 node_modules 可重复成功。
- [ ] `npx playwright install --with-deps chromium` 后记录 Playwright/Chromium 版本；不安装 Firefox/WebKit。
- [ ] 运行 `npm audit --include=dev`；任何漏洞先评估/解决，不用 `--force` 无脑升级。

Expected package-script contract（Task 9 已拥有的 `css:analyze` 保持原名）：

```json
{
  "test:coverage": "vitest --run --coverage",
  "pretest:e2e": "npm run build",
  "test:e2e": "playwright test",
  "test:e2e:staging": "playwright test --config playwright.staging.config.ts",
  "bundle:check": "node ../scripts/check-web-bundle-budget.mjs"
}
```

**Commit boundary:** dependencies/lock/ignore 单独提交，便于依赖或 browser revision 回滚。

## 4. Coverage Baseline And Critical 90% Ratchet

### Files

- Modify: `web/vitest.config.ts`, `web/package.json`。
- Create: `web/coverage-budget.json` and contract test。
- Potential create/modify: `web/src/lib/apiRequest.ts`, tests, compatible re-export in `api.ts`。

### Checklist

- [ ] 配置 production include 与审计过的 test-only excludes；先运行 coverage，不设阈值，确认未测试 production files 也出现在报告。
- [ ] 记录四项 covered/total/percentage 到 task evidence；将真实数值写入 `coverage-budget.json`。
- [ ] contract 枚举 final critical files：Modal stack/focus、Dashboard model/RemoteState、request transport、auth client/context、所有 command-owning Asset hooks。
- [ ] 对低于 90% 的 critical file，逐个写真实分支 RED tests；不为数字添加 production-only test seam。
- [ ] API transport 若需提取：先冻结 request headers/credentials/cache、401、JSON、empty body 与 error parsing；再最小搬移并保持 `api.ts` exports/wire behavior。
- [ ] 运行 coverage，确认 global >= fresh baseline 且每个 critical file branch >=90。
- [ ] 证明将任一阈值临时提高到当前值以上会失败，再恢复审核值；证明把 critical path 改成不存在路径会被 contract test 捕获。
- [ ] `npm run test:coverage` 是一次性 run，不启动 watch；普通 `npm run test` 仍用于 focused feedback。

Expected config mapping（实际数字来自本阶段首次成功报告）：

```ts
import { readFileSync } from 'node:fs'

type CoverageMetrics = {
  statements: number
  branches: number
  functions: number
  lines: number
}
type CoverageBudget = {
  global: CoverageMetrics
  critical: Record<string, Partial<CoverageMetrics>>
}

const budget = JSON.parse(readFileSync(new URL('./coverage-budget.json', import.meta.url), 'utf8')) as CoverageBudget
const criticalThresholds = Object.fromEntries(
  Object.entries(budget.critical).map(([file, value]) => [file, value]),
)

coverage: {
  provider: 'v8',
  include: ['src/**/*.{ts,tsx}'],
  exclude: ['src/**/*.test.{ts,tsx}', 'src/test/**', 'src/**/*.d.ts', 'src/**/*TestFixtures.{ts,tsx}'],
  reporter: ['text', 'json-summary', 'lcov'],
  thresholds: { ...budget.global, ...criticalThresholds, autoUpdate: false },
}
```

**Focused verification:** request/auth/modal/dashboard/Asset hook tests + coverage contract。

**Rollback:** API transport seam 与 coverage config 分提交；若 seam 行为差异，回滚 seam，不能把 `api.ts` 整文件阈值伪装成 helper 目标。

## 5. TypeScript Flags And Type-Aware ESLint

### Stage Order

1. lib；
2. atoms（含 transitive lib）；
3. dashboard（含 shared dependencies）；
4. app/layout/components/pages/routes；
5. 主 tsconfig 全局启用并删除临时 profiles。

### Checklist

- [ ] 为第一层创建临时 profile，同时启用两项 flag，确认规划期类别仍能 RED。
- [ ] 每个 error 先分类：unchecked read、optional omission/null、fixture/type drift、真正的 impossible state。
- [ ] 用 guard、条件构造 payload、discriminated union 和 exhaustive switch 修复；每个行为分支先补测试。
- [ ] 特别验证 PATCH optional vs explicit `null`、URL query omission、数组空态和 async callback 错误传播。
- [ ] 一层为零后才扩大下一层；每层单独 commit，避免 156 类改动不可审阅。
- [ ] 最终在 `web/tsconfig.app.json` 写入：

```json
"noUncheckedIndexedAccess": true,
"exactOptionalPropertyTypes": true
```

- [ ] 删除临时 ratchet profiles，运行 `tsc -b` 证明唯一主配置全绿。
- [ ] ESLint flat config 为 `src/lib/**` 和 final Asset hooks 开启明确 type-aware rules；e2e/config 文件使用正确 Node/browser globals。
- [ ] 搜索并审计新增 `eslint-disable`、` as `、non-null assertion、`any`；无理由 suppressions 为零。

**Focused verification:** 每层 profile + 受影响 unit tests + lint；最终完整 build。

**Rollback:** 每层独立 commit。某 route 修复出现业务差异时回滚该层 commit，不在别的 route 打补丁掩盖。

## 6. Bundle/Font Budget And Task 9 CSS Gate

### Files

- Create: `scripts/check-web-bundle-budget.mjs`, deterministic test, `web/bundle-budget.json`。
- Modify: `web/package.json`, `Makefile`。
- Consume: final Task 9 analyzer/budget files。

### Checklist

- [ ] 用 synthetic dist fixtures 先完成入口/hash/gzip/max-chunk/font/error-path RED→GREEN。
- [ ] 在 fresh production dist 运行 explicit baseline mode，review 后提交最终四项数值；不得复制规划期值。
- [ ] 默认 check 只读 budget；验证超 1 byte 会失败并输出 actual/limit。
- [ ] 确认字体为非零且涵盖 Task 5 七个 WOFF2；预算不把 license 或 SVG 混入 font bytes。
- [ ] `make verify-web` 在 production build 后调用 bundle check 与 Task 9 `css:analyze`。
- [ ] 延续 Task 9 指标只降不升；若 Task 9 已把 CSS check 接入 Make，Task 10 只验证不重复调用。

Budget file contract（数值由 `--write-baseline` 从 final fresh dist 生成）：

```json
{
  "entryJsGzipBytes": 1,
  "entryCssGzipBytes": 1,
  "maxAsyncJsGzipBytes": 1,
  "fontWoff2RawBytes": 1
}
```

这里的 `1` 只定义四个字段必须是正整数的 schema 示例，不是可提交预算；实现阶段生成的文件必须等于当时真实 measured values，并在 PR diff 中展示。

**Focused verification:** Node synthetic test、production build、bundle check、CSS analyzer。

**Commit boundary:** analyzer+tests、baseline、Make wiring 可分别 review/rollback。

## 7. Playwright Base Harness And Fail-Closed Fixtures

### Files

- Create: `web/playwright.config.ts`。
- Create: `web/e2e/fixtures/*`, `web/e2e/support/*`。
- Modify: `web/package.json` with prebuild/test scripts。

### Checklist

- [ ] 配置 Chromium、loopback production preview、locale/timezone、zero retries、bounded workers、failure-only diagnostics artifacts。
- [ ] 先写一个 protected route RED：没有 auth fixture 时必须重定向 login；加入 `/api/auth/me` 后进入目标 route。
- [ ] 建立 method/path/canonical-query router；未知 endpoint 返回测试 501 并在 teardown 失败。
- [ ] 复用 Task 3 Dashboard fixtures；其余 fixture 用 `satisfies` 检查 required TS wire fields。
- [ ] 建立 profile reset，确保 test 间无共享 mutation/state。
- [ ] 增加 source contract：fixture 不得有 catch-all empty response，mutation expectation 必须声明 method/body keys。

Base config shape:

```ts
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: process.env.CI ? [['html', { open: 'never' }], ['list']] : 'list',
  use: {
    baseURL: 'http://127.0.0.1:4173',
    locale: 'zh-CN',
    timezoneId: 'Asia/Shanghai',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'off',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: 'npm run preview -- --host 127.0.0.1 --port 4173',
    url: 'http://127.0.0.1:4173/login',
    reuseExistingServer: false,
  },
})
```

Fixture router public shape:

```ts
type ApiMethod = 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
type ApiRouteKey = `${ApiMethod} ${string}`
type ApiFixtureResponse = { status: number; body: unknown; delayMs?: number }
type ApiFixtureProfile = Readonly<Record<ApiRouteKey, ApiFixtureResponse>>
```

**Focused verification:** 单 route/单 viewport + deliberate unknown endpoint failure。

## 8. Core Route, State And Security Suites

### Core Routes

- [ ] 建立九 route manifest，给每条 route 指定 heading/main/workflow anchor/critical command text。
- [ ] 先在 390px 写预期会捕获已知历史裁切/overflow 的几何断言，再确认 Task 6/7 final code为 GREEN。
- [ ] 扩展 1024/1440；得到 27/27 matrix。
- [ ] 每条 route 断言 URL 未被 auth/wildcard redirect、main 非空白、document/body scrollWidth 不超 viewport。

### States

- [ ] PageState loading/empty/error/success；loading 用受控 deferred response，不用任意 sleep。
- [ ] Dashboard critical/abnormal/maintenance/onboarding/stable，覆盖 abnormal subset 与 VPS 503 failure-not-onboarding。
- [ ] error retry 只重发规定 endpoint；unknown refresh 请求仍 fail closed。

### Security/Diagnostics

- [ ] init script 先于 app 收集 CSP/unhandled rejection；page fixture 统一收集 console/page/network。
- [ ] 精确断言 main document CSP header 等于 repository policy。
- [ ] 故意注入 console error/CSP diagnostic 的 helper self-test先 RED，证明 collector 真会阻断；不要向 production source 注入。
- [ ] 正常 suite 的 violation/error/unexpected network 全为零。

**Focused verification:** `core-routes.spec.ts`, `page-states.spec.ts`, `security.spec.ts`。

## 9. Accessibility, Keyboard And Responsive Contracts

### Checklist

- [ ] axe 扫描 AppShell、Dashboard、Settings、VPS/Asset settled states，serious/critical = 0。
- [ ] skip link：Tab 到链接、Enter 后 main 成为 activeElement。
- [ ] Tabs：唯一 tab stop、ArrowLeft/Right 循环、Home/End、tab/tabpanel ids 与 label。
- [ ] menu：打开、Arrow/Escape（按 Task 6 final contract）、关闭后 focus restore。
- [ ] nested Modal：Tab trap；一次 Escape 只关子层并保持 body lock/父层；第二次关父层并恢复页面 focus。
- [ ] Settings“监控策略”、Asset“场景与组合”、Provider“组合决策”在 390px 完整可见且 hit target 可达。
- [ ] 宽表 wrapper 有可访问名称、`tabIndex=0` 和局部 overflow；heading/toolbar 不随表格横滚。
- [ ] 不创建 tracked screenshot baseline；失败 artifacts 用于定位，人工视觉判断仍由 reviewer/staging evidence承担。

**Focused verification:** `accessibility.spec.ts`, `visual-contracts.spec.ts`。

## 10. CI Wiring

### Existing Web Job

- [ ] setup-node 仍唯一读取 `.node-version`，调用者继续用 `NODE_ENV=production make verify-web` 验证隔离。
- [ ] `make verify-web` 完成 install -> lint/type-aware -> coverage -> build -> bundle/font -> CSS AST。
- [ ] source/toolchain contract 证明 coverage/budget scripts 没有只存在 package.json 而未被 Make/CI调用。

### New Browser Job

- [ ] checkout/setup-node/npm cache/npm ci。
- [ ] cache + install matching Chromium；不能复用系统任意 Chrome 版本冒充 lockfile browser。
- [ ] 运行 `npm run test:e2e`，上传 failure-only Playwright artifacts，retention 有限。
- [ ] job 独立命名，配置为 required check；无 staging secrets、fork 安全。
- [ ] 记录总时长；若过长先用 workers/cache/shard优化，不删断言。

Browser job wiring shape:

```yaml
web-browser:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v6
    - uses: actions/setup-node@v6
      with:
        node-version-file: .node-version
        cache: npm
        cache-dependency-path: web/package-lock.json
    - uses: actions/cache@v4
      with:
        path: ~/.cache/ms-playwright
        key: playwright-${{ runner.os }}-${{ hashFiles('web/package-lock.json') }}
    - run: env -u NODE_ENV npm --prefix web ci --include=dev
    - run: npm --prefix web exec playwright install -- --with-deps chromium
    - run: npm --prefix web run test:e2e
    - if: failure()
      uses: actions/upload-artifact@v4
      with:
        name: playwright-failure
        path: |
          web/playwright-report
          web/test-results
        retention-days: 7
```

### CI RED/GREEN

- [ ] 在 PR 上先观察各 job 实际出现；故意超 budget/阈值的本地 self-test证明会失败后恢复。
- [ ] 全部 required checks green 才进入 specs/final gate。

## 11. Specs And Operations Update

在实现/路径稳定后使用 `trellis-update-spec`，不能先把理想路径写成现状。

- [ ] `quality-guidelines.md`：新命令门户、coverage、browser/axe、bundle/CSS、type-aware lint、CI evidence。
- [ ] `directory-structure.md`：Task 9 final CSS owner tree、e2e/fixture/config/budget真实路径；删除不存在路径和 inline theme 描述。
- [ ] `styling-guidelines.md`：final CSS owner/import顺序、budget、CSP/static theme resources。
- [ ] `component-conventions.md`：Task 6 final Form/Tabs/Menu/skip link 与正式 keyboard browser gate。
- [ ] `state-and-data.md`：Task 8 `{state,commands}` controllers、request transport seam、Dashboard fixtures/e2e evidence boundary。
- [ ] `index.md`：必要的 guide description/status更新。
- [ ] `docs/operations/ui-preview-and-browser-sanity.md`：CI Playwright vs local helper vs staging、九 route/三 viewport、artifact与敏感数据政策。
- [ ] 全仓搜索 `styles/atoms.css|styles/pages.css|app/layout/layout.css|inline theme|仓库.*没有.*Playwright`，仅保留明确历史/迁移说明。

**Validation:** task/spec links、真实 paths、所有 commands 可执行；不用占位词。

## 12. Full Local Gate Before Implementation PR

```bash
env -u NODE_ENV npm --prefix web ci --include=dev
NODE_ENV=test npm --prefix web run lint
NODE_ENV=test npm --prefix web run test:coverage
NODE_ENV=production npm --prefix web run build
npm --prefix web run bundle:check
npm --prefix web run css:analyze
npm --prefix web run test:e2e
npm --prefix web audit --include=dev
git diff --check
make verify
```

Exit evidence must include:

- fresh vs final coverage table and critical-file branch table;
- final bundle/font/CSS actual vs limit table;
- exact test file/test count;
- 27/27 route matrix plus state/a11y/keyboard/security counts;
- Playwright/Chromium/Node versions and CI duration;
- all intentional exclusions/limitations (staging remains separate).

## 13. Commit And PR Boundaries

Recommended reviewable commits:

1. `build(web): pin browser and coverage tooling`
2. `test(web): establish coverage ratchets`
3. `refactor(web): isolate API request transport`（仅在正式 baseline证明需要时）
4. `refactor(web): enable indexed and optional strictness`（按层可拆多 commit）
5. `build(web): enforce bundle and CSS budgets`
6. `test(web): add Chromium contract gates`
7. `ci(web): require browser and budget checks`
8. `docs(spec): align frontend quality contracts`

- [ ] 每个 commit 自洽、有对应 focused tests；不把 budget update 混进无关代码。
- [ ] push `codex/frontend-quality-ratchets`，创建独立 PR，监控所有 required checks。
- [ ] CI failure 在同一 branch 修复；不 force-push 猜测。
- [ ] checks green 后合并，监控 main CI、Release Please、release PR、publish-images；发布成功并核验版本/镜像后才进入 staging gate。

## 14. Authenticated Staging Gate

### Mechanism Review

- [x] 用户已于 2026-07-10 确认使用 `workflow_dispatch + GitHub staging environment`；人工本机入口不作为 Gate C 替代机制。
- [ ] 创建 GitHub `staging` environment，将 deployment branch/ref policy 限制为仅 `main`，配置 `HOUFENG_STAGING_BASE_URL` variable 与 username/password secrets；值不写入 repo/task/log。
- [ ] 增加不读取 environment/secrets 的 ref preflight；非 `main` dispatch 必须失败，不能仅 skip 后显示为成功。
- [ ] 使用固定 concurrency group 串行运行并设置 `cancel-in-progress: false`，避免保存/恢复状态被并发或取消破坏。
- [ ] workflow 输入 expected release version；healthz 不匹配立即失败。

Approved workflow contract:

```yaml
on:
  workflow_dispatch:
    inputs:
      expected_version:
        required: true
        type: string

permissions:
  contents: read

concurrency:
  group: frontend-staging-smoke
  cancel-in-progress: false

jobs:
  ref-guard:
    runs-on: ubuntu-latest
    steps:
      - name: Require main ref
        run: test "$GITHUB_REF" = "refs/heads/main"

  staging-smoke:
    needs: ref-guard
    environment: staging
    runs-on: ubuntu-latest
    timeout-minutes: 45
    env:
      HOUFENG_STAGING_BASE_URL: ${{ vars.HOUFENG_STAGING_BASE_URL }}
      HOUFENG_STAGING_USERNAME: ${{ secrets.HOUFENG_STAGING_USERNAME }}
      HOUFENG_STAGING_PASSWORD: ${{ secrets.HOUFENG_STAGING_PASSWORD }}
      HOUFENG_EXPECTED_VERSION: ${{ inputs.expected_version }}
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-node@v6
        with:
          node-version-file: .node-version
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: env -u NODE_ENV npm --prefix web ci --include=dev
      - run: npm --prefix web exec playwright install -- --with-deps chromium
      - run: npm --prefix web run test:e2e:staging
      - if: always()
        uses: actions/upload-artifact@v4
        with:
          name: frontend-staging-audit-${{ github.run_id }}
          path: web/test-results/staging-audit
          if-no-files-found: error
          retention-days: 30
```

### Real Environment Lane

- [ ] 真实登录并确认九条 core route 未跳回 login；记录实际数据限制。
- [ ] 检查 main document CSP/security headers、console/network 无异常。
- [ ] 打开 Asset nested confirmation，验证 focus/Escape/cancel，不执行危险 mutation。
- [ ] Settings 保存一个批准的可逆字段：先 snapshot、保存、readback、`finally` 恢复、再 readback；恢复失败阻断。
- [ ] 切换主题并 reload，确认同源资源/CSP无 violation。

### Explicit Injection Lane

- [ ] 同一已认证部署上覆盖 Dashboard 五状态。
- [ ] 对只读 GET 做 503 与 deferred slow response，验证 loading/stale/unavailable/retry。
- [ ] 注入长文本/大列表，检查 390/1024/1440 overflow、裁切和局部滚动。
- [ ] 报告明确写“deployed frontend fault injection”，不写成真实后端/生产数据通过。

### Evidence

- [ ] 上传脱敏 manifest：run URL/id、commit/tag、healthz version、browser version、routes/viewports、allowed headers、console/network counters。
- [ ] 上传 `frontend-staging-audit-<run-id>`，保留 30 天，包含脱敏 manifest 与成功/失败截图；staging 不上传含凭据/request body的 trace。
- [ ] 将 run/artifact link 和结果写回本 task 与 parent Gate C。

**Hard blocker:** 当前没有 staging environment/variables/secrets。缺少时本节保持未勾选，task status 维持 `in_progress`，不得 archive。

## 15. Gate C And Archive

- [ ] Task 8 总控删除、Task 9 CSS 指标下降与 Task 10 coverage/browser/CSP/bundle/AST CI 在同一 release 版本成立。
- [ ] staging 对该 release/version 通过，证据可访问且不含 secrets。
- [ ] parent Gate C 逐项勾选并列出 run/commit/tag、残余风险。
- [ ] 使用 `trellis-check` 做最终 code/spec/data-flow/CI consistency review。
- [ ] 业务实现、evidence 和 journal commits 均完成后，才归档 `frontend-quality-ratchets`。
- [ ] Task 10 归档 PR/main CI 完成后，parent 仍保持 planning，等待所有 children 完成后的跨任务最终集成，不在 parent 修改业务代码。
