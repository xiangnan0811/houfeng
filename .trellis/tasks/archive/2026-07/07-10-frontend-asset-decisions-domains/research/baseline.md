# Asset Decisions 领域拆分基线证据

## 审查坐标

- 基线提交：`origin/main@a57458dc30ca9dc2ee4333563fdd6ce19e90a6e5`
- 分支：`codex/frontend-asset-decisions-domains`
- Node：`.node-version` 固定 `22.23.1`
- 完整基线：`env -u NODE_ENV make verify` 通过；90 个 Vitest 文件、673 个测试、npm audit 0、Go/lint/strict TypeScript/production build 全绿。
- Task 8 只改前端结构与测试所有权；后端 wire contract、业务语义、文案、DOM workflow 和 CSS 不在变更面内。

## 结构基线

| 文件/指标 | 基线 |
| --- | ---: |
| `web/src/pages/AssetDecisionsPage.tsx` | 5 行 wrapper |
| `web/src/pages/AssetDecisionsPageContent.tsx` | 2,705 行真实总控 |
| `web/src/pages/AssetDecisionsPage.test.tsx` | 3,069 行；33 个 workflow + 3 个结构测试 |
| Content hook 调用 | 73：47 `useState`、12 `useEffect`、8 `useMemo`、3 `useRef`、1 `useCallback`、1 `useNavigate`、1 `useSearchParams` |
| `asset-decisions/` production source | 5,310 行；最大文件 627 行 |

现有“主文件不超过 800 行”只扫描 5 行 wrapper。提交 `76dadb6` 把真实实现整体移到 `*PageContent.tsx` 后仍能通过，因此文件名级行数断言不能证明领域边界存在。

现有展示/纯逻辑拆分可直接复用：

- `components/PortfolioWorkbench.tsx`
- `components/SecondaryWorkbenches.tsx`
- `modals/*` 与 `modal-content/*`
- `businessLogic.ts`、`recordDrafts.ts`、`tableColumns.tsx`、`renderHelpers.tsx`、`formatters.ts`、`utils.ts`

缺失的是 URL owner、领域读取 owner、mutation owner、跨域失效协议和无法绕过的依赖方向守护。`tableColumns.tsx` 已有 column factory，但当前总控仍内联一套 column/render handler；Task 8 应复用现有 factory，不再复制第三套。

## 初始读取合同

默认 `/asset-decisions` 在没有打开详情时发出 11 个 GET。筛选 query 由 `AssetDecisionGroupListFilter` 统一传给 overview、groups、manual groups 和 records。

| Owner | API helper | 默认请求 |
| --- | --- | --- |
| portfolio | `getAssetDecisionOverview` | `/api/asset-decisions/overview?view=needs_decision&renew_within_days=30` |
| groups | `listAssetDecisionGroups` | `/api/asset-decisions/groups?view=needs_decision&renew_within_days=30` |
| manual groups | `listAssetDecisionManualGroups` | `/api/asset-decisions/manual-groups?view=needs_decision&renew_within_days=30` |
| manual groups | `listVPSAssets` | `/api/vps`（新增成员候选） |
| templates | `listAssetDecisionScenarioTemplates` | `/api/asset-decisions/scenario-templates` |
| records | `listAssetDecisionRecords` | `/api/asset-decisions/records?view=needs_decision&renew_within_days=30` |
| renewal queue | `listSubscriptions` | `/api/subscriptions?renew_within_days=30&sort=renew_at&order=asc` |
| renewal queue | `listSubscriptions` | `/api/subscriptions?sort=renew_at&order=asc` |
| renewal queue | `listVPSAssets` | `/api/vps?renewal_decision=unreviewed` |
| renewal queue | `listVPSAssets` | `/api/vps?renewal_decision=migrate` |
| renewal queue | `listVPSAssets` | `/api/vps?renewal_decision=cancel` |

首屏直接带 deep link 时，在默认 11 个 GET 上增加一个 detail GET：

- group：`/api/asset-decisions/groups/{id}?renew_within_days={30|60|90}`
- manual group：`/api/asset-decisions/manual-groups/{id}`
- record：`/api/asset-decisions/records/{id}`
- template：`/api/asset-decisions/scenario-templates/{id}`

从已挂载页面交互式打开、关闭或切换实体时，当前 `assetDecisionFilter` 因依赖整个 `searchParams` 对象而改变 identity；即使业务筛选值没有变化，overview、groups、manual groups、records 四个 filtered GET 仍会重跑。打开/切换实体还会增加目标 detail GET。这是 Task 8 必须保留的请求时序，网络优化另立任务。

overview 与 groups 使用 `Promise.allSettled`，两者必须独立保留成功结果和错误；其他读取域也必须局部失败，不能把失败数组解释成真实 empty。

## URL 合同

| 维度 | 值/规则 |
| --- | --- |
| `view` | `needs_decision|renewal|region|provider|cost|evidence|single_queue`；非法值降级 `needs_decision` |
| `renew_within_days` | `30|60|90`；非法值降级 `30` |
| context filters | `provider_id`、`vps_id`、`country`、`region`、`city`、`scenario` |
| open state | `group_id`、`manual_group_id`、`record_id`、`template_id`；优先级按该顺序，命令写入时删除其他 open key |
| default secondary | record → `records`；manual/template → `scenarios`；legacy single_queue → `single_queue`；portfolio renewal → `renewals` |
| close | 只删除当前 open key，保留 view/window/context filters 和用户本地选择的 secondary workbench |

当前实现同时保存 URL 和四个 selected ID，再用一个 0ms timer 同步。目标实现以 URL selection 为唯一真相；观察到的 query、deep link、back/forward、secondary workbench、关闭行为以及 open-key 变化引起的四个 filtered reload 必须保持。

## Mutation 与成功后刷新合同

“刷新集合”在 Task 8 中指兼容基线，而不是借重构优化网络请求。任何减少或扩大都需另立性能任务。

| Mutation | method/path/body owner | 成功后的当前行为 |
| --- | --- | --- |
| auto group → manual group | `POST /api/asset-decisions/manual-groups`；`source_type=auto_group`、source id、window、scenario、title/goal/note | 本地 upsert manual list/detail，切到 `manual_group_id`；open-key 变化重跑四个 filtered GET，并读取新 manual detail |
| auto/manual group → record | `POST /api/asset-decisions/records`；source、window、title/goal/status、完整 members | 本地 upsert record list/detail，切到 `record_id`；open-key 变化重跑四个 filtered GET，并读取新 record detail |
| template → manual group | `POST /api/asset-decisions/scenario-templates/{id}/manual-groups` | 本地 upsert manual list/detail，切到 `manual_group_id`；重跑四个 filtered GET，并读取新 manual detail |
| manual group → template | `POST /api/asset-decisions/scenario-templates` | 本地 upsert template list/detail，切到 `template_id`；重跑四个 filtered GET，并读取新 template detail |
| template status | `PATCH /api/asset-decisions/scenario-templates/{id}`，body `{status}` | 只用响应更新当前 detail/list；无 GET |
| manual group metadata | `PATCH /api/asset-decisions/manual-groups/{id}` | 只用响应更新当前 detail/list；无 GET |
| manual member add | `POST /api/asset-decisions/manual-groups/{id}/members` | 只用响应更新 detail/list/draft；无 GET，不写 VPS |
| manual member remove | `DELETE /api/asset-decisions/manual-groups/{id}/members/{vps_id}` | 仅确认后请求；只用响应更新 detail/list；无 GET |
| record status | `PATCH /api/asset-decisions/records/{id}`，body `{status}` | 只用响应更新 detail/list；无 GET |
| record member follow-up | 同 record PATCH；body `members:[{vps_id,followup_status,followup_note}]` | 只用响应更新 detail/list/drafts；无 GET，不隐式改 record status |
| VPS renewal decision | `PATCH /api/vps/{id}`；仅 renewal decision 与非空 reason | 先合并队列响应，然后触发六个读取域的兼容失效：重跑默认 11 GET；若 group/manual/record/template detail 正打开，再重读该 detail |

当前 UI 没有调用 `patchAssetDecisionManualGroupMember`；Task 8 不应以“补齐 API”为由新增成员编辑 workflow。

## API 归属白名单

| Controller | 允许从 `lib/api` import 的 symbols |
| --- | --- |
| route state | 无 |
| portfolio | `getAssetDecisionOverview` |
| groups | `listAssetDecisionGroups`、`getAssetDecisionGroup` |
| manual groups | `listAssetDecisionManualGroups`、`getAssetDecisionManualGroup`、`listVPSAssets`、`createAssetDecisionManualGroup`、`createManualGroupFromScenarioTemplate`、`patchAssetDecisionManualGroup`、`addAssetDecisionManualGroupMember`、`deleteAssetDecisionManualGroupMember` |
| templates | `listAssetDecisionScenarioTemplates`、`getAssetDecisionScenarioTemplate`、`createAssetDecisionScenarioTemplate`、`patchAssetDecisionScenarioTemplate` |
| records | `listAssetDecisionRecords`、`getAssetDecisionRecord`、`createAssetDecisionRecord`、`patchAssetDecisionRecord` |
| renewal queue | `listSubscriptions`、`listVPSAssets`、`updateVPSAsset` |

## 既有测试所有权映射

- route/composition：首屏、quiet state、legacy URL、四类 deep link、support override、filters/tabs、next-work、partial error。
- groups：group detail、cost-pressure default、preview cap、renewal mutation 后 draft 对齐、保存 record、创建 manual group。
- manual groups：preview cap、成员增删、record draft 对齐、保存 manual record、嵌套移除确认。
- templates：归档确认、从模板创建 manual group、从 manual group 另存模板。
- records：打开/状态 PATCH、member follow-up、execution preview/CTA、evidence/current facts、legacy snapshot。
- renewal queue：单台续费 PATCH、subscription evidence failure。
- architecture：真实生产 glob、import direction、API owner、`useSearchParams` owner、行数/effect budget、禁止 `*PageContent`。

## 已排除方案

1. 单一 reducer/controller：可以减少 setter 数量，但仍把所有读取和 mutation 收回一个新总控，不能关闭 P2-05。
2. page-scoped Context/event bus：表面减少 props，实际把跨域依赖隐藏成运行时订阅；当前只有一条 route，不值得新增第三个 Context。
3. 一次性重写或新增状态库：会同时改变请求、渲染时序与依赖面；仓库没有 React Query/Zustand，Task 8 也禁止 package/lockfile 变更。
