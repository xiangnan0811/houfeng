# Houfeng 下一步开发计划：VPS Asset Ledger + Fleet Observability

## 0. 计划定位

本文是 Houfeng 在当前 V1 收口之后的 post-V1 / MVP 扩展计划基线，用于指导后续进入开发前的任务拆分、验收和跨层契约确认。

它不改写 frozen V1 baseline：

- V1 业务结构仍以 `docs/design/v1-baseline/` 的 frozen 子集为准。
- V1 收口与发布优先级仍以 `docs/release/next-phase-plan.md`、`docs/release/v1-gap-checklist.md` 和 `CLAUDE.md` 为准。
- 本文描述的是 V1 之后，把 Houfeng 从现有 Fleet Control Plane 扩展成面向真实 VPS 持有、续费、迁移和成本决策的 MVP 方向。

下一阶段产品方向保持为：

```text
Houfeng = VPS Asset Ledger + Fleet Observability

VPS Asset Ledger：资产、服务商、价格、续费、订阅、风险、决策
Fleet Observability：Node、Agent、Target、Probe、主机采样、异常、事件、通知
```

当前已实现的监控控制面不是废弃物，而是 `Fleet Observability` 子系统。下一阶段应在其旁边补齐资产层，并通过明确关联把资产事实与监控事实组合起来。

---

## 1. 总体策略

### 1.1 不推倒现有监控底座

继续保留并维护：

- Go center、systemd agent、PostgreSQL、React Web 的整体架构。
- 现有 migration embed 与启动时自动迁移机制。
- 认证、session、settings、retention、incident、notification 等基础能力。
- `nodes`、`targets`、`probe_items`、`node_heartbeats`、`host_samples`、`probe_observations`、`active_incidents`、`state_change_events`、`notification_records` 等监控表。
- 当前 Dashboard 的系统概览工作台定位。

### 1.2 暂停非必要 Agent 扩张

Agent 当前已经能支撑第一批 Fleet Observability 能力。post-V1 / MVP 第一阶段不继续把 Agent 做重，除非是为了修复现有采集、同步、兼容性问题。

暂缓：

- Provider API 自动同步。
- DNS Provider API 自动同步。
- 服务自动发现。
- Web SSH。
- 插件系统。
- 任意远程脚本或复杂远程执行。
- Agent 侧新增大块业务判断。

### 1.3 新增资产层，不替换监控层

资产层是新的产品主线，但它不接管现有 Node / Target / Agent 语义。

第一阶段核心对象：

- `providers`
- `vps_assets`
- `subscriptions`
- `vps_node_links`

后续对象：

- `vps_spec_snapshots`
- `ip_histories`
- `price_histories`
- `renewal_decisions`
- `experience_logs`
- `services`
- `domains`

第一阶段先让真实 VPS 资产、服务商、订阅与现有 Node 建立闭环；不要一次性实现所有历史、评分、导入和 Dashboard 展示。

---

## 2. 领域语义与跨层契约

### 2.1 核心语义

```text
VPS Asset：一台从服务商购买、续费、取消或迁移的真实 VPS 资产。
Provider：提供 VPS 资产的服务商或平台账号上下文。
Subscription：围绕某台 VPS 的价格、计费周期、续费日期与自动续费状态。
Node：一台被 Houfeng 监控的具体服务器，仍遵守现有 Node 不变量。
Target：一个可观测入口，仍表示 endpoint / service target，不等同于 VPS。
Agent：运行在 Node 上的薄采集与同步进程，不承担资产管理逻辑。
Decision：围绕续费、观察、迁移、取消的人工决策记录。
```

### 2.2 Node / Target / Agent 语义不变

必须保留以下契约：

- `Node = 一台具体的服务器`。重装系统仍可保持同一个 `node_id`，换硬件应新建 Node。
- `Target = 一个可观测入口`。地址属于 Target，ProbeItem 只描述如何观测。
- `Agent = observe / buffer / sync / apply plan`。资产服务商、订阅、价格、续费决策不下沉到 Agent。
- 资产层通过 `vps_node_links` 关联现有 `nodes`，不把 `nodes` 改造成 VPS 资产表。
- Node 页面可以显示“关联 VPS”，VPS 页面可以显示“关联 Node 监控状态”，但双方仍属于不同聚合。

### 2.3 Provider 与 `nodes.provider` 的迁移期关系

新增 `providers` 表后，不能让 `providers` 与现有 `nodes.provider` 变成双来源混乱。

迁移期约定：

- `providers` 是资产层的服务商主数据，用于 VPS 资产、订阅、成本和服务商体验聚合。
- `nodes.provider` 继续是监控节点元数据，仅表示 Node 当时录入或展示用的 provider hint。
- 创建或更新 `providers` 不自动批量改写 `nodes.provider`。
- 创建或更新 `vps_assets.provider_id` 不自动改写 `nodes.provider`。
- 建立 `vps_node_links` 时，可以在 API response 中同时返回资产 Provider 与 Node 的 provider hint，供 UI 展示差异；不要在写路径里偷偷同步。
- 如果后续要收敛 `nodes.provider`，必须作为独立迁移任务设计映射规则、冲突处理和回滚策略。

### 2.4 数据流契约

资产层跨层数据流应显式定义：

```text
用户输入 / JSON 导入
  -> HTTP handler 解析与入口校验
  -> domain/service 归一化与业务校验
  -> store 写入稳定机器值
  -> API DTO 返回稳定机器值和必要摘要
  -> web/src/lib/types.ts 定义前端类型
  -> web/src/lib/api.ts 统一请求
  -> UI 层把机器值映射为中文标签
```

责任边界：

- 后端入口负责校验枚举、金额、日期、外键和必填字段。
- DB/API 存储和传输稳定机器值。
- UI 只负责展示映射、筛选控件文案和用户反馈，不定义新的后端合法值。
- 页面不得直接 `fetch`，必须通过 `web/src/lib/api.ts` 和 `web/src/lib/types.ts`。

---

## 3. 状态值策略

资产层使用稳定英文机器值，UI 映射为中文标签。不要在 DB/API 中混入中文展示值，也不要让 UI 自造后端不认识的新状态。

### 3.1 VPS 生命周期状态

| DB/API 值 | UI 中文 |
|-----------|---------|
| `active` | 在用 |
| `idle` | 闲置 |
| `testing` | 测试中 |
| `to_migrate` | 待迁移 |
| `to_cancel` | 待取消 |
| `cancelled` | 已取消 |
| `archived` | 已归档 |

### 3.2 VPS 用途状态

| DB/API 值 | UI 中文 |
|-----------|---------|
| `in_use` | 承载业务 |
| `idle` | 暂无用途 |
| `standby` | 备用 |
| `testing` | 测试用途 |
| `unknown` | 未确认 |

### 3.3 续费决策状态

| DB/API 值 | UI 中文 |
|-----------|---------|
| `unreviewed` | 未评估 |
| `keep` | 保留 |
| `observe` | 观察 |
| `migrate` | 迁移 |
| `cancel` | 取消 |
| `auto_renew_cancelled` | 已取消自动续费 |
| `replaced` | 已替换 |

### 3.4 Subscription 状态

| DB/API 值 | UI 中文 |
|-----------|---------|
| `active` | 生效中 |
| `paused` | 已暂停 |
| `cancelled` | 已取消 |
| `expired` | 已过期 |
| `unknown` | 未确认 |

### 3.5 后端校验要求

每个写入口都必须校验：

- 枚举值必须属于后端定义的允许集合。
- `price >= 0`。
- `billing_months > 0`。
- `currency` 非空并做基本格式归一，例如大写三字符币种代码。
- `monthly_price = price / billing_months` 由后端计算，不接受前端或导入文件直接覆盖。
- `provider_id` 非空时必须指向存在的 provider。
- `vps_node_links.node_id` 必须指向存在的 Node。
- 日期字段使用 ISO date，空值表示未知，不用假日期填充。
- PATCH 不能绕过同样的枚举、金额、外键和日期校验。

---

## 4. 数据库计划

### 4.1 Migration 编号

当前仓库已有 migration 到：

```text
db/migrations/0015_add_host_containers.sql
```

资产层首个 migration 应使用当前最大编号之后的下一个未占用编号。按当前仓库状态，应为：

```text
db/migrations/0016_create_asset_ledger.sql
```

执行前必须先运行：

```bash
ls db/migrations | sort | tail
```

如果期间已有新 migration 合入，则使用新的下一个未占用编号。不要复用任何已占用编号。

### 4.2 第一阶段表范围

第一阶段只要求形成可运行闭环：

- `providers`
- `vps_assets`
- `subscriptions`
- `vps_node_links`

`vps_spec_snapshots`、`ip_histories`、`price_histories`、`renewal_decisions` 可以在同一个 schema migration 中预留，也可以拆到后续 migration。是否拆分由实现任务决定，但第一条开发任务不能只落空 schema 和 skeleton，必须至少交付 providers 的可用 API 与测试。

### 4.3 `providers`

用途：资产层服务商主数据。

建议字段：

```sql
create table if not exists providers (
  provider_id text primary key,
  name text not null,
  website text not null default '',
  panel_url text not null default '',
  account_hint text not null default '',
  country text not null default '',
  note text not null default '',
  rating integer,
  labels text[] not null default '{}',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
```

建议约束：

- `name` 入口层必须非空。
- `rating` 如填写，应限制在 1 到 5。
- 第一阶段不做 delete，避免误删影响资产。

### 4.4 `vps_assets`

用途：真实 VPS 资产，不等同于 Node。

建议字段：

```sql
create table if not exists vps_assets (
  vps_id text primary key,
  display_name text not null,
  provider_id text references providers(provider_id) on delete set null,
  provider_name text not null default '',
  product_name text not null default '',
  order_ref text not null default '',
  country text not null default '',
  region text not null default '',
  city text not null default '',
  datacenter text not null default '',
  ipv4 text not null default '',
  ipv6 text not null default '',
  ssh_host text not null default '',
  ssh_port integer not null default 22,
  ssh_user text not null default '',
  os_name text not null default '',
  virtualization text not null default '',
  lifecycle_status text not null,
  usage_status text not null,
  renewal_decision text not null default 'unreviewed',
  importance text not null default 'normal',
  labels text[] not null default '{}',
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  archived_at timestamptz
);
```

`provider_name` 是导入和展示兼容字段。若 `provider_id` 存在，服务商主数据以 `providers` 为准；`provider_name` 不反向创建 provider，除非导入 dry-run/import 明确执行创建策略。

### 4.5 `subscriptions`

用途：价格、周期、续费日期与自动续费状态。

建议字段：

```sql
create table if not exists subscriptions (
  subscription_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  price numeric(12, 2) not null,
  currency text not null,
  billing_cycle text not null default '',
  billing_months integer not null,
  monthly_price numeric(12, 4) not null,
  started_at date,
  renew_at date,
  auto_renew boolean not null default false,
  auto_renew_cancelled boolean not null default false,
  status text not null default 'active',
  payment_method text not null default '',
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
```

`monthly_price` 必须由后端按 `price / billing_months` 计算后入库。UI 和导入文件可以显示或传入原始价格与周期，但不能作为权威来源直接写入 `monthly_price`。

### 4.6 `vps_node_links`

用途：连接资产层与监控层。

建议字段：

```sql
create table if not exists vps_node_links (
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  node_id text not null references nodes(node_id) on delete cascade,
  linked_at timestamptz not null default now(),
  unlinked_at timestamptz,
  note text not null default '',
  primary key (vps_id, node_id, linked_at)
);
```

第一阶段建议支持历史保留，即 unlink 时写 `unlinked_at`，不物理删除。实现时需要防止同一 `vps_id` 与 `node_id` 同时存在多条 active link。

### 4.7 索引建议

```sql
create index if not exists idx_vps_assets_provider on vps_assets(provider_id);
create index if not exists idx_vps_assets_status on vps_assets(lifecycle_status, usage_status, renewal_decision);
create index if not exists idx_vps_assets_location on vps_assets(country, region, city);
create index if not exists idx_subscriptions_vps on subscriptions(vps_id);
create index if not exists idx_subscriptions_renew_at on subscriptions(renew_at);
create index if not exists idx_vps_node_links_vps_active on vps_node_links(vps_id) where unlinked_at is null;
create index if not exists idx_vps_node_links_node_active on vps_node_links(node_id) where unlinked_at is null;
```

---

## 5. 后端 API 计划

### 5.1 模块与目录

建议新增领域模块：

```text
internal/center/providers/
internal/center/vpsassets/
internal/center/subscriptions/
internal/center/assetlinks/
internal/center/store/providers.go
internal/center/store/vps_assets.go
internal/center/store/subscriptions.go
internal/center/store/vps_node_links.go
```

新增 endpoint 时必须同步：

- `internal/center/http/handlers/` 中新增 handler。
- `internal/center/http/router.go` 的 `RouterOptions` 与 `New` 注册。
- `cmd/houfeng-center/bootstrap.go` 的实际 wiring。
- handler tests、router tests、store tests。

### 5.2 Providers API

第一条实现任务必须交付 providers 的可用 API，而不是只创建目录 skeleton。

```text
GET    /api/providers
POST   /api/providers
GET    /api/providers/{provider_id}
PATCH  /api/providers/{provider_id}
```

验收语义：

- `GET /api/providers` 可列出服务商。
- `POST /api/providers` 可创建服务商，返回稳定 `provider_id`。
- `PATCH /api/providers/{provider_id}` 可更新基础字段和 labels。
- 空 `name`、非法 `rating` 返回 400。
- 不存在的 provider item 返回 404。
- 所有 route 经过现有 auth middleware，agent route 不受影响。

### 5.3 VPS API

```text
GET    /api/vps
POST   /api/vps
GET    /api/vps/{vps_id}
PATCH  /api/vps/{vps_id}
```

第一版列表需要支持：

- 按 `provider_id` 筛选。
- 按 `lifecycle_status`、`usage_status`、`renewal_decision` 筛选。
- 返回关联订阅摘要和 active Node link 数量。

### 5.4 Subscription API

```text
GET    /api/subscriptions
POST   /api/subscriptions
GET    /api/subscriptions/{subscription_id}
PATCH  /api/subscriptions/{subscription_id}
```

第一版必须支持按 `renew_at` 排序，便于尽早回答“未来 30 天哪些 VPS 要续费”。

### 5.5 VPS 与 Node 关联 API

```text
GET    /api/vps/{vps_id}/nodes
POST   /api/vps/{vps_id}/link-node
POST   /api/vps/{vps_id}/unlink-node
GET    /api/nodes/{node_id}/vps
```

关联 API 必须保持 Node 监控语义不变：

- link 不改写 `nodes.provider`。
- link 不改变 Node lifecycle / monitoring / health 状态。
- unlink 不删除 Node、不删除 VPS，只结束关联。
- VPS 详情页读取的是关联摘要，不直接耦合 Node 表结构。

---

## 6. 前端计划

### 6.1 页面顺序

前端页面应在对应后端 API 可用并有测试后再做。

建议顺序：

1. 服务商页。
2. VPS 列表页。
3. VPS 详情页。
4. 订阅页。
5. 续费决策入口。
6. Dashboard 资产摘要。

### 6.2 导航

第一阶段可以新增：

```text
首页
VPS
服务商
订阅
节点
目标
事件
设置
```

不要把“节点 / 目标”隐藏成资产子页面。它们仍是 Fleet Observability 的主要入口。

### 6.3 页面边界

建议文件：

```text
web/src/pages/ProvidersPage.tsx
web/src/pages/VPSPage.tsx
web/src/pages/VPSDetailPage.tsx
web/src/pages/SubscriptionsPage.tsx
```

共享契约：

- API 调用放在 `web/src/lib/api.ts`。
- 类型放在 `web/src/lib/types.ts`。
- 状态中文映射集中定义，页面不各自写一份。
- VPS 列表优先展示决策字段，不做所有字段的表格堆叠。
- VPS 详情页可以分区展示基础信息、订阅、关联监控、决策历史。

### 6.4 VPS 列表核心字段

第一版列表只展示决策所需字段：

```text
名称
服务商
区域
生命周期
用途状态
续费决策
月付折算
下次续费
自动续费
关联 Node 状态摘要
标签
```

IPv4、IPv6、SSH、OS、虚拟化等字段进入详情页，不作为列表默认主列。

---

## 7. 真实数据 dry-run / import

真实 40 多台 VPS 的 JSON dry-run/import 是模型验证，不应排到 Dashboard polish 之后。

### 7.1 顺序

应在以下能力完成后立即执行 dry-run：

1. providers backend 可创建和查询。
2. vps_assets backend 可创建和查询。
3. subscriptions backend 可创建、查询并计算月付折算。
4. 基础状态值、金额、日期、provider 关联校验已落到后端入口。

在 Dashboard 资产卡片之前，必须用真实 JSON dry-run 验证模型是否够用。

### 7.2 第一版导入策略

先做 dry-run，再做 import。

建议提供本地命令或受保护 API 二选一：

```text
go run ./cmd/houfeng-import-vps-json -file ./tmp/vps-assets.json -dry-run
```

或：

```text
POST /api/import/vps-json?dry_run=1
```

dry-run 必须返回：

- 可创建的 providers 数量。
- 可创建的 VPS 数量。
- 可创建的 subscriptions 数量。
- 缺失 provider 的条目。
- 缺失续费日期的条目。
- 非法状态、金额、币种、日期。
- 疑似重复项，例如相同 `provider_name + display_name`。
- 需要人工确认的 Node 关联候选。

### 7.3 建议 JSON 输入

```json
[
  {
    "display_name": "tokyo-01",
    "provider_name": "example-provider",
    "country": "Japan",
    "region": "Tokyo",
    "city": "Tokyo",
    "ipv4": "1.2.3.4",
    "ssh_port": 22,
    "ssh_user": "root",
    "lifecycle_status": "active",
    "usage_status": "in_use",
    "renewal_decision": "unreviewed",
    "labels": ["proxy", "japan"],
    "subscription": {
      "price": 36,
      "currency": "USD",
      "billing_months": 12,
      "renew_at": "2026-09-01",
      "auto_renew": false
    }
  }
]
```

dry-run 后要回答：

1. 现有字段能否覆盖真实 40 多台 VPS？
2. 哪些 VPS 没有明确服务商？
3. 哪些 VPS 没有续费日期？
4. 哪些计费周期不能表达？
5. 哪些 VPS 需要关联现有 Node？
6. 哪些 VPS 闲置但仍在付费？
7. 哪些 VPS 未来 30 天会续费？
8. 哪些应标记为观察、迁移或取消？

---

## 8. Dashboard 边界

Dashboard 资产指标是后置摘要入口，不是资产字段总表。

必须保留当前 Dashboard 的系统概览工作台定位：

- 监控异常仍是主任务面。
- 资产指标只提供少量决策摘要。
- 详细列表进入 VPS、订阅、服务商页面。
- 不把 `/api/dashboard` 重新扩成所有资产字段的 dump。

### 8.1 建议资产摘要指标

第一版最多放 4 到 6 个摘要：

```text
未来 30 天续费 VPS 数
待决策 VPS 数
待取消 / 待迁移 VPS 数
未关联 Node 的 VPS 数
关联 Node 异常的 VPS 数
按币种分组的月付折算成本
```

成本聚合规则：

```text
monthly_total = sum(active subscriptions monthly_price)
yearly_total = monthly_total * 12
```

汇率换算暂缓。第一版按原币种分组展示：

```json
{
  "cost_by_currency": [
    { "currency": "USD", "monthly_total": 42.5, "yearly_total": 510 },
    { "currency": "EUR", "monthly_total": 18, "yearly_total": 216 }
  ]
}
```

---

## 9. 推荐任务拆分

### Task 1：Asset Ledger schema + providers vertical slice

目标：交付可运行的最小纵向闭环，而不是 skeleton。

范围：

- 新增 `db/migrations/0016_create_asset_ledger.sql`，或执行时的下一个未占用编号。
- 创建 `providers` 表，以及实现 providers 所需索引。
- 可选地在同一 migration 中创建 `vps_assets`、`subscriptions`、`vps_node_links` 基础表；如果同一任务风险过大，可只创建 providers，但后续任务必须紧接资产表。
- 新增 `internal/center/providers/` types、校验、repository interface。
- 新增 `internal/center/store/providers.go`。
- 新增 providers handlers。
- 注册 `RouterOptions`、`router.New`、bootstrap wiring。
- 覆盖 handler、router、store、migration 测试。

验收：

- `POST /api/providers` 可创建服务商。
- `GET /api/providers` 可列出服务商。
- `GET /api/providers/{provider_id}` 可读取服务商。
- `PATCH /api/providers/{provider_id}` 可更新服务商。
- 空名称与非法 rating 返回 400。
- 不存在 provider 返回 404。
- 现有 Node / Target / Agent 行为不变。

建议验证命令：

```bash
git diff --check
go test ./internal/center/store/migrate -v
go test ./internal/center/store -run 'TestPostgresProvider' -v
go test ./internal/center/http/handlers -run 'TestProviders' -v
go test ./internal/center/http -run 'TestRouter.*Provider|TestAuth' -v
make verify-go
```

### Task 2：VPS assets backend

目标：交付 VPS 资产 CRUD 与状态校验。

范围：

- 创建或补齐 `vps_assets` 表。
- 新增 `internal/center/vpsassets/`。
- 新增 `store/vps_assets.go`。
- 新增 `/api/vps` collection 与 item handlers。
- 状态值、provider 外键、日期、基础字段校验落在后端入口。

验收：

- 可创建、查询、更新 VPS 资产。
- 可按 provider 和状态筛选列表。
- API 返回稳定机器状态值。
- UI 中文标签不进入 DB/API。

建议验证命令：

```bash
git diff --check
go test ./internal/center/store -run 'TestPostgresVPSAsset' -v
go test ./internal/center/http/handlers -run 'TestVPS' -v
go test ./internal/center/http -run 'TestRouter.*VPS' -v
make verify-go
```

### Task 3：Subscriptions backend

目标：交付订阅、价格、续费日期与月付折算。

范围：

- 创建或补齐 `subscriptions` 表。
- 新增 `internal/center/subscriptions/`。
- 新增 `store/subscriptions.go`。
- 新增 `/api/subscriptions` collection 与 item handlers。
- 后端计算 `monthly_price`。
- 支持按 `renew_at` 排序。

验收：

- `POST /api/subscriptions` 可创建订阅。
- `PATCH /api/subscriptions/{subscription_id}` 会重新计算 `monthly_price`。
- 非法金额、周期、币种、状态返回 400。
- 可查询未来 30 天续费候选。

建议验证命令：

```bash
git diff --check
go test ./internal/center/store -run 'TestPostgresSubscription' -v
go test ./internal/center/http/handlers -run 'TestSubscriptions' -v
make verify-go
```

### Task 4：真实 VPS JSON dry-run/import 模型验证

目标：用真实 40 多台 VPS 数据验证模型，先 dry-run，再考虑 import。

范围：

- 新增导入解析与 dry-run 结果模型。
- dry-run 使用与 HTTP 写入口一致的校验规则。
- 输出缺失字段、非法字段、重复候选、provider 创建候选、Node 关联候选。
- dry-run 通过后再启用实际 import。

验收：

- 真实 JSON dry-run 能完整跑完。
- dry-run 报告能指出模型缺口。
- import 不会静默创建明显重复数据。
- 如果模型不够用，先修计划或模型，再做 Dashboard。

建议验证命令：

```bash
git diff --check
go test ./internal/center/importing -v
go run ./cmd/houfeng-import-vps-json -file ./tmp/vps-assets.json -dry-run
make verify-go
```

### Task 5：VPS 与 Node 关联

目标：通过 `vps_node_links` 连接资产与监控。

范围：

- 创建或补齐 `vps_node_links` 表和 active link 索引。
- 新增 `internal/center/assetlinks/`。
- 新增 link / unlink / query API。
- VPS 详情 API 返回关联 Node 摘要。
- Node item API 可返回关联 VPS 摘要，或新增独立 `/api/nodes/{node_id}/vps`。

验收：

- link 不改写 Node 状态和 `nodes.provider`。
- unlink 保留历史。
- 同一 VPS 与 Node 不出现重复 active link。
- VPS 详情可看到关联 Node 健康摘要。
- Node 详情可看到关联 VPS 摘要。

建议验证命令：

```bash
git diff --check
go test ./internal/center/store -run 'TestPostgresVPSNodeLink' -v
go test ./internal/center/http/handlers -run 'TestVPSNodeLinks|TestNodeVPS' -v
make verify-go
```

### Task 6：资产前端页面

目标：新增可用的服务商、VPS、订阅页面。

范围：

- `web/src/lib/api.ts` 和 `web/src/lib/types.ts` 增加资产 API。
- 集中定义状态中文映射。
- 新增 ProvidersPage、VPSPage、VPSDetailPage、SubscriptionsPage。
- 导航新增资产入口，但不移除节点、目标、事件。

验收：

- 可以创建和查看 provider。
- 可以创建、筛选和查看 VPS。
- 可以创建和查看 subscription。
- UI 展示中文状态，API 传输机器值。
- 页面不直接 fetch。

建议验证命令：

```bash
git diff --check
cd web && npm run lint
cd web && npm run test -- --run ProvidersPage VPSPage VPSDetailPage SubscriptionsPage
cd web && npm run build
```

### Task 7：Dashboard 资产摘要

目标：在 Dashboard 增加少量资产决策摘要。

范围：

- 后端增加资产摘要查询。
- `/api/dashboard` 只返回决策摘要，不返回资产字段 dump。
- Dashboard 页面增加 4 到 6 个资产摘要入口。

验收：

- 可看到未来 30 天续费、待决策、未关联 Node、异常关联 VPS、按币种月付成本等摘要。
- 不破坏现有异常队列与监控概览。
- Dashboard 仍是系统概览工作台。

建议验证命令：

```bash
git diff --check
go test ./internal/center/store -run 'TestPostgresDashboard' -v
go test ./internal/center/http/handlers -run 'TestDashboard' -v
cd web && npm run test -- --run DashboardPage
cd web && npm run build
make verify-go
```

### Task 8：历史与决策增强

目标：在真实模型稳定后补齐历史能力。

范围：

- `renewal_decisions`
- `price_histories`
- `ip_histories`
- `vps_spec_snapshots`
- VPS timeline API
- 决策页

验收：

- 续费决策有历史记录。
- 价格变化有历史记录。
- IP 和配置变化能追踪。
- 这些能力不阻塞前面的资产录入、订阅和真实数据验证。

---

## 10. 暂缓事项

以下事项不要放入第一阶段：

1. Provider API 自动同步。
2. DNS Provider API 自动同步。
3. Web SSH。
4. 插件系统。
5. 服务自动发现。
6. 完整服务注册表。
7. 完整域名管理。
8. 多用户 RBAC。
9. 汇率换算。
10. 复杂评分算法。
11. Agent 复杂远程命令。

---

## 11. 第一阶段完成标准

完成第一阶段后，Houfeng 应达到：

1. 可以录入真实 VPS 资产。
2. 可以录入服务商。
3. 可以记录价格、计费周期、续费日期和自动续费状态。
4. 后端自动计算月付折算。
5. 可以把 VPS 关联到现有 Node。
6. 可以在 VPS 详情看到关联 Node 的监控摘要。
7. 可以在 Node 侧看到关联 VPS 摘要。
8. 可以通过 dry-run/import 验证真实 40 多台 VPS 数据。
9. 可以查看未来 30 天续费候选。
10. 可以标记保留、观察、迁移、取消等续费决策。
11. Dashboard 可以显示少量资产决策摘要。
12. 原有 Node / Target / Agent / Dashboard 监控能力不退化。

---

## 12. 给后续 Codex 的总提示词

```text
当前仓库 Houfeng 已经实现 center + agent 的 Fleet Observability 底座，包括 nodes、targets、probe_items、host_samples、probe_observations、incidents、events、settings、auth 和 web 页面。不要推倒现有实现，也不要把 nodes 改造成 VPS 资产表。

下一阶段是 post-V1 / MVP 扩展方向：在现有监控底座旁新增 VPS Asset Ledger。新增资产层后，Houfeng 的产品结构为 VPS Asset Ledger + Fleet Observability。资产层通过 vps_node_links 关联现有 nodes；nodes 继续表示 Agent/监控节点，targets 继续表示可观测入口，agent 继续保持薄采集与同步职责。

第一条实现任务必须是可运行纵向闭环：使用当前 db/migrations 下一个未占用编号创建资产层 schema，并交付 providers 的 domain/store/handler/router/bootstrap wiring 和测试。不要只做 skeleton。

状态策略：DB/API 使用稳定英文机器值，UI 映射为中文标签。枚举、金额、日期、provider 外键、Node link 校验必须在后端入口完成。nodes.provider 在迁移期仍是监控节点元数据，不因 providers 或 vps_assets 写入而自动改写。

真实 40 多台 VPS 的 JSON dry-run/import 应在 providers、vps_assets、subscriptions 后端闭环之后立即做，用于验证模型；Dashboard 资产指标放在后面，只做少量决策摘要，不做字段 dump。

完成每个任务后运行对应 Go/Web 测试与 git diff --check；涉及后端时至少运行 make verify-go，涉及前端时运行 lint、相关 Vitest 和 build。
```

---

## 13. 最终方向

下一阶段的正确方向是：

```text
先把 Houfeng 从 Fleet Control Plane 扩展为 VPS Asset Ledger + Fleet Observability，
而不是继续把监控系统做得更复杂。
```

现有监控能力是产品优势，应继续保留并稳定；新的开发主线是资产、价格、续费、真实数据验证和续费/迁移决策。

---

## 14. 实施状态（2026-05-10）

本计划的 Task 1-3、Task 5-8 以及 VPS-scoped service/domain 轻量扩展已经按仓库当前实现闭合；Task 4 的 dry-run/import 工具链已经完成，但真实 40 多台 VPS 数据执行仍是 user-data-dependent deferred，不能在没有真实数据文件和授权的情况下宣称完成。

完成度审计和证据矩阵见 `docs/release/asset-ledger-roadmap-completion.md`。后续继续推进前应先查该审计文档：

- 如果只是为了继续完成本计划，不应再创建新的立即开发任务。
- 如果用户提供真实 VPS JSON，应先运行 dry-run，依据报告决定是否 import 或新建模型修正任务。
- 如果要扩张到 Provider/DNS 同步、Web SSH、插件、服务发现、完整服务注册表、完整域名管理、RBAC、汇率或评分算法，应先建立新的产品计划和 Trellis task，不应把这些范围自动归入本计划。
