# Domain assets MVP

## Goal

在已完成 providers、VPS assets、subscriptions、VPS links、history、services 等资产闭环之后，补上计划文档中尚未实现的 `domains` 后续对象。此任务交付一个最小可用的域名资产闭环：可以手工记录某台 VPS 承载或关联的域名，可选关联已登记服务与现有 Target，并在 VPS 详情页查看与创建。

## What I already know

* `houfeng_codex_下一步开发计划.md` 将 `domains` 列为资产层后续对象，同时明确暂缓 DNS Provider API 自动同步、服务自动发现和 Provider API 自动同步。
* 当前代码没有 `asset_domains` 表、domain asset 领域包、仓库、HTTP handler、router wiring、前端类型或页面显示。
* 已有 `asset_services` 提供相近模式：`db/migrations/0023_create_asset_services.sql`、`internal/center/assetservices`、`internal/center/store/asset_services.go`、`internal/center/http/handlers/asset_services.go`、`/api/services` 与 `/api/vps/{vps_id}/services`。
* 计划的数据流契约要求：用户输入 / JSON 导入 -> handler -> domain/store validation -> API DTO -> `web/src/lib/types.ts` -> `web/src/lib/api.ts` -> UI 展示映射。
* 用户已明确本轮不使用 subagent，并要求继续遵守 Trellis 与 feature branch / PR / CI / merge / sync main 的 git 流程。

## Assumptions

* 本批只做手工维护的 domain asset，不做 DNS 解析记录模型、DNS provider sync、注册商 API sync、whois/RDAP 查询或真实数据导入。
* Domain asset 归属某台 VPS；可选关联某条 `asset_services.service_id` 和某个 `targets.target_id`，用于表达“此域名服务/观测入口”关系，但不改变 Target、Node、Agent 语义。
* `domain_name` 使用规范化小写 ASCII 域名，拒绝 URL、路径、空白、裸主机名和明显非法 label；国际化域名后续独立设计。
* `expires_at` 是可选日期字段，用于人工记录域名过期时间；后端沿用 subscription `Date` 类型，API 传输 `YYYY-MM-DD`。

## Requirements

* 新增 `asset_domains` 迁移，字段覆盖：
  * `domain_id`
  * `vps_id`
  * 可选 `service_id`
  * 可选 `target_id`
  * `domain_name`
  * `purpose`
  * `status`
  * `registrar`
  * 可选 `expires_at`
  * `auto_renew`
  * `https_enabled`
  * `labels`
  * `note`
  * `created_at`
  * `updated_at`
* 后端新增 domain asset 领域包，提供：
  * 稳定状态枚举：`active`、`paused`、`retired`、`unknown`
  * 输入归一化：trim、lowercase、移除尾随 `.`
  * 校验：必填 VPS、合法域名、合法状态、日期格式、labels trim/drop blank
  * sentinel error：invalid input、owner not found、service not found、target not found、conflict/not found（按实际写路径使用）
* Store 新增 repository：
  * collection list：支持 `vps_id`、`service_id`、`target_id`、`status` 过滤
  * scoped list：`ListAssetDomainsForVPS(ctx, vpsID)`
  * create：创建域名资产，映射 FK/check/unique 错误为领域 sentinel
* HTTP API：
  * `GET /api/domains`
  * `POST /api/domains`
  * `GET /api/vps/{vps_id}/domains`
  * `POST /api/vps/{vps_id}/domains`
* Router/bootstrap：
  * collection 与 VPS subtree API 不能落到 SPA fallback
  * 生产 wiring 使用 Postgres repository
* Web：
  * `web/src/lib/types.ts` 定义 domain asset 类型、输入、过滤器和中文展示 label
  * `web/src/lib/api.ts` 提供 collection/scoped list/create helper
  * VPS 详情页加载 domains，与 services 类似展示手工记录域名；支持创建最小域名记录
  * UI 只做本地空值/基本格式保护，合法机器值以后端为准
* Tests：
  * Go domain validation tests
  * migration/store tests
  * HTTP handler tests
  * router/bootstrap tests
  * web API helper tests
  * VPS 详情页 domain display/create tests

## Acceptance Criteria

* [ ] 新迁移编号未与现有 migrations 冲突，`migrate_test` 更新到最新文件。
* [ ] `POST /api/domains` 与 `POST /api/vps/{vps_id}/domains` 可创建 domain asset，并返回稳定机器值。
* [ ] `GET /api/domains` 与 `GET /api/vps/{vps_id}/domains` 可列表，过滤参数经过后端校验。
* [ ] 非法域名、非法状态、缺失 VPS、未知 VPS、未知 service、未知 target、重复域名有明确 HTTP 响应。
* [ ] VPS 详情页会加载 domain assets，空状态、已有记录和创建成功路径可用。
* [ ] 现有 Node / Target / Agent / Dashboard 语义不被修改。
* [ ] Go 与 Web 相关测试、build 和 repo verification 通过。
* [ ] 通过 feature branch PR，CI 绿后合并，并同步本地 `main`。

## Out of Scope

* DNS provider / registrar API 自动同步。
* 解析记录、证书详情、WHOIS/RDAP 拉取。
* 真实域名数据导入。
* 全局域名管理页面。
* 服务自动发现或 Agent 侧业务判断。
* 修改 Node / Target / Agent 的核心语义。

## Technical Notes

* 当前最新迁移是 `0023_create_asset_services.sql`，本任务应使用 `0024_create_asset_domains.sql`。
* 推荐复用 `asset_services` 的 handler/router/store 流程和 `subscriptions.Date` 日期 JSON 约定，避免新的跨层格式。
* 相关规范：
  * `.trellis/spec/backend/index.md`
  * `.trellis/spec/backend/directory-structure.md`
  * `.trellis/spec/backend/database-guidelines.md`
  * `.trellis/spec/backend/error-handling.md`
  * `.trellis/spec/backend/quality-guidelines.md`
  * `.trellis/spec/web/index.md`
  * `.trellis/spec/web/state-and-data.md`
  * `.trellis/spec/web/component-conventions.md`
  * `.trellis/spec/web/quality-guidelines.md`
  * `.trellis/spec/guides/cross-layer-thinking-guide.md`
  * `.trellis/spec/guides/code-reuse-thinking-guide.md`
  * `.trellis/spec/guides/branch-workflow-governance.md`
