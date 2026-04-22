---
date: 2026-04-22
tags:
  - 自建服务
  - 监控
  - 设计
  - 技术选型
status: review-ready
---

# 服务器舰队控制面 V1 技术选型

> 相关：[[服务器舰队控制面-v1-设计总览]]、[[服务器舰队控制面-v1-详细设计-架构、数据与模型]]、[[服务器舰队控制面-v1-开发交付与新会话使用说明]]

这份文档的目标不是重新讨论 V1 应该长什么样，而是：

> **在当前已冻结的 V1 结构、交互、视觉基线之上，为后续实施选择一套最合适的技术栈。**

它回答三类问题：

1. 哪些技术约束已经被设计冻结，不能在选型时被破坏；
2. 有哪几套可行方案，它们的优缺点分别是什么；
3. 当前最推荐的技术栈是什么，以及为什么不是别的方案。

---

## 0. 当前命名基线（已定）

### 中文主名
**候风**

### 英文主名
**Houfeng**

### 中文完整名
**候风 · 服务器舰队控制面**

### 英文完整名
**Houfeng Fleet Control Plane**

### 推荐仓库名
**houfeng-control-plane**

这意味着后续技术选型中的仓库、模块、部署命名示例，都以 `houfeng-*` 为默认前缀，而不再沿用泛化占位名。

---

## 1. 选型前提：当前已经被冻结的约束

技术选型必须服从这些前提，而不是反过来重塑产品设计：

### 1.1 系统形态
- 单用户个人系统
- `1 个单体中心控制面 + 1 个 PostgreSQL + N 个 systemd agent`
- 不引入额外 MQ
- 不引入额外 TSDB
- 不拆微服务

### 1.2 agent 约束
- 必须是 **systemd 常驻二进制**
- 适配低配 VPS
- 支持短期本地持久化缓冲
- 不依赖 Docker 运行

### 1.3 中心控制面约束
- 统一承担 API / UI / 后台归并 / 通知
- 原始 observation 先入库，再异步归并
- 分层保留
- 补传不追溯 Telegram

### 1.4 前端约束
- dark-first
- 中文主界面语言
- 高密度工程工具风格
- 必须服从已冻结的页面壳、视觉主次和状态表达

### 1.5 数据与对象模型约束
- `Node / Target / ProbeItem / HostSample / ProbeObservation / Incident / Event`
- 需要良好的 PostgreSQL 适配性
- 需要支持时间序列样本、聚合、事件和通知记录

---

## 2. 评估维度

为了避免“只看流行度”，这里明确采用以下评估维度：

### 2.1 运维负担
- 部署复杂度
- 更新复杂度
- 运行时依赖数量

### 2.2 与冻结设计的贴合度
- 是否天然适合单体
- 是否适合后台归并与长期运行
- 是否适合 systemd agent

### 2.3 长期维护成本
- 单人维护是否吃力
- 是否容易随着需求增长而失控
- 是否需要过多框架层知识

### 2.4 性能与资源占用
- 中心控制面能否稳定处理 observation 写入和后台归并
- agent 是否足够轻

### 2.5 前后端协作效率
- 接口契约是否清楚
- 前端是否容易实现已冻结视觉与交互

---

## 3. 候选方案对比

这里把候选方案收敛为 3 套，不再继续无边界发散。

---

## 方案 A：Go 中心控制面 + Go agent + React/Vite 前端（推荐）

### 组成
- 中心控制面后端：Go
- agent：Go
- 前端：React + TypeScript + Vite
- 数据库：PostgreSQL

### 优点
- 中心和 agent 同一门语言，心智统一
- Go 很适合：
  - 单体服务
  - 并发处理
  - 单二进制分发
  - 低资源 agent
- 后端和 agent 都容易做成静态二进制
- 中心控制面后续可做成：
  - 单一服务
  - 前端静态资源可嵌入
  - 运维简单

### 缺点
- Web 层生态不如 TypeScript“顺手”
- 后端模板、验证、ORM 这类“开箱即用感”没那么强
- 开发者需要接受 SQL-first / Go 风格而不是重框架体验

### 适配度判断
**最高。**  
这套方案最符合当前已经冻结的“单体 + 低维护 + 二进制 agent + 长期可用”目标。

---

## 方案 B：TypeScript 中心控制面 + Go agent + React/Vite 前端

### 组成
- 中心控制面后端：Node.js / TypeScript
- agent：Go
- 前端：React + TypeScript + Vite
- 数据库：PostgreSQL

### 优点
- 前后端语言统一（Web 层）
- UI、API、类型共享体验更顺
- 对表单、管理台、组件开发更“顺手”

### 缺点
- agent 仍然必须是 Go，最终还是双语言
- Node 侧长期运行的后台归并、批处理与低维护部署，不如 Go 单体自然
- 中心控制面做成单一可执行体不如 Go 顺手
- 更容易引入：
  - 框架层复杂度
  - 运行时依赖
  - 构建链复杂度

### 适配度判断
**中等。**  
如果这是一个以 Web CRUD 为绝对中心的产品，这套方案会更诱人；但当前系统不是纯后台产品，它还强依赖一个长期运行的 agent 生态和单体中心语义。

---

## 方案 C：Python 中心控制面 + Go agent + React/Vite 前端

### 组成
- 中心控制面后端：Python
- agent：Go
- 前端：React + TypeScript + Vite
- 数据库：PostgreSQL

### 优点
- 后端开发速度快
- 数据处理与脚本化体验强
- 某些分析和批处理写起来舒服

### 缺点
- 生产部署与运行时一致性不如 Go 干净
- 长期单体服务 + 后台归并 + 低维护部署，并不天然比 Go 更优
- 仍然是双语言，且与 agent 语言不统一
- 对“单人长期稳定维护”的整体运维负担偏高

### 适配度判断
**中低。**  
适合作为分析型或内部脚本型项目，但不是当前这个“长期运行的控制面 + agent 体系”的最优解。

---

## 4. 推荐结论

### 4.1 推荐方案

# **推荐：方案 A**
# **Go 中心控制面 + Go agent + React/Vite 前端 + PostgreSQL**

### 4.2 推荐原因

这套方案最符合当前冻结设计的 5 个核心目标：

1. **单体优先**  
   Go 很适合把 API、后台归并、通知和静态资源托管在一个服务里。

2. **agent 必须轻**  
   Go 对 systemd 常驻二进制和低资源 VPS 适配最好。

3. **长期维护成本低**  
   二进制分发、部署简单、运行时依赖少。

4. **中心与 agent 心智统一**  
   后端与 agent 同语言，比“Node/Python + Go”更稳。

5. **前端仍能保持现代体验**  
   React + Vite 足够实现已经冻结的高密度控制面 UI。

---

## 5. 推荐技术栈（分层结论）

## 5.1 仓库策略

### 推荐
**单仓多目录（monorepo）**

### 理由
- 这本质上是一个产品，而不是多个独立服务
- 设计、接口、agent、前端会高频联动
- 单仓更利于：
  - 同步版本
  - 统一文档
  - 同步演进

### 推荐目录思路
这里只定方向，不冻结最终目录名：

- `cmd/houfeng-center`：中心控制面入口
- `cmd/houfeng-agent`：agent 入口
- `internal/...`：中心控制面内部模块
- `agent/...`：agent 内部模块
- `web/`：前端
- `docs/design/...`：交付设计包

---

## 5.2 中心控制面后端

### 推荐
- **Go**
- 路由层：**chi**
- HTTP 基础：Go `net/http`
- 日志：优先标准库 `slog`
- 后台任务：进程内 worker / ticker / job loop

### 为什么是 Go + chi
- `chi` 足够轻，贴近标准库，不会把单体做成重框架项目
- 相比更重的 Web 框架，它更符合当前“单体但克制”的目标
- 后台归并逻辑和 observation 写入并不是“传统 CRUD-only Web 后台”，Go 更自然

### 为什么不是更重的后端框架
- 当前不需要：
  - 重脚手架
  - 重依赖注入系统
  - 重模块框架
- 设计已经冻结，接下来更重要的是稳和可控，而不是框架炫技

---

## 5.3 数据访问层

### 推荐
- PostgreSQL 驱动：**pgx**
- 查询方式：**SQL-first**
- 查询代码生成：**sqlc**
- migration：**raw SQL 作为唯一真相**

### 为什么推荐 SQL-first
当前系统有很多天然 PostgreSQL 导向的能力：

- observation 写入
- 聚合查询
- incident / event 状态归并
- 时间范围过滤
- 后续可能的分区 / 保留策略

这类系统如果过早上重 ORM，很容易出现：
- 查询可解释性下降
- 时间序列与聚合 SQL 难看
- Postgres 特性被抽象掉

所以更推荐：

> **SQL 是源，`sqlc` 负责生成类型安全访问层。**

### 关于分区
建议策略不是“一上来就过度设计”，而是：

- schema 设计时为 observation 表保留按时间分区的可能性
- 实现初期可以先不急着上分区
- 当样本量与保留策略明确后再启用

---

## 5.4 前端

### 推荐
- **React**
- **TypeScript**
- **Vite**
- 路由：**React Router**
- 服务端状态：**TanStack Query**
- 样式：**Tailwind CSS**
- 基础无样式组件：**Radix Primitives**
- 表格：**TanStack Table**
- 图表：**Apache ECharts**

### 为什么不是 Next.js
当前系统没有：
- SEO 诉求
- SSR 诉求
- 面向公开访问的营销页面诉求

它本质上是一个：
> **登录后 / 内部使用 / 工程工具型控制面**

所以使用 Vite SPA 更简单、更贴合，也更符合单体控制面的部署方式。

### 为什么是 React
- 当前视觉与交互已经冻结为：
  - 高密度面板
  - 复杂状态标签
  - 详情页多区块
  - 抽屉 / 确认弹层 / 危险区
- React 生态在这类工程工具型界面上足够成熟

### 为什么是 Tailwind + Radix
- Tailwind 更适合把当前冻结的视觉 token 快速落到组件层
- Radix 提供：
  - 可访问
  - 无样式
  - 适合深度定制

这比直接套一整套重型组件库更符合当前已经冻结的产品调性。

### 为什么图表推荐 ECharts
- 当前系统虽然不是“大屏监控”，但仍然需要：
  - 趋势线
  - 状态叠加
  - 细粒度 tooltip
  - 后续可能的多序列对比

ECharts 在中高密度监控场景下更有余量。

---

## 5.5 agent

### 推荐
- **Go**
- systemd service
- 单二进制部署
- 本地短期缓冲采用 **纯 Go 嵌入式持久化方案**

### 为什么继续坚持 Go
这里没有悬念：
- agent 需要低资源
- 需要单二进制
- 需要跨多种 Linux VPS
- 需要短期本地 durable buffer

Go 仍然是最自然的选择。

### 关于本地短期缓冲
这里给出方向，不把实现细节过早锁死：

- 优先选择 **纯 Go 嵌入式持久化** 方案
- 避免依赖外部数据库进程
- 避免引入 CGO 级别复杂度

换句话说：

> **方向已经确定：纯 Go、短期、有界、durable。**

具体选 `bbolt` 还是其他纯 Go 嵌入式存储，可留到实施前参数 / 工程化收口。

---

## 5.6 API 风格

### 推荐
- **JSON REST**
- 不做 GraphQL
- 不做 gRPC-first

### 原因
当前系统前后端关系非常清楚：
- 管理端 UI
- agent 同步接口
- 目标 / 节点 / 事件 / 配置 API

这类场景 REST 已足够清晰，且更利于：
- 调试
- 文档化
- 后续问题排查

GraphQL / gRPC 并不会为当前 V1 提供足够回报，反而增加复杂度。

---

## 5.7 部署策略

### 推荐
- 中心控制面：**单 Go 服务**
- 数据库：**PostgreSQL**
- 前端：构建为静态资源，优先考虑由中心服务一并提供
- 反向代理 / TLS：部署时可加一层轻量代理（如 Caddy / Nginx），但不把它写死成当前设计前提

### 为什么不建议把前端做成独立长期运行 Node 服务
因为当前产品目标是：
- 少组件
- 低维护
- 单体优先

所以更优的部署语义是：

> **前端构建产物成为中心控制面的一部分，而不是再多一个运行中的 Web 服务。**

---

## 6. 明确不推荐的选择

为了防止后续回摆，这里也明确写出当前不推荐的方向。

### 6.1 不推荐 Next.js / SSR-first
原因：
- 当前不是内容站点
- 没有 SSR 刚需
- 只会增大部署与构建复杂度

### 6.2 不推荐把中心控制面做成 Node 重框架项目
原因：
- 和 agent 语言割裂
- 后台归并与单体服务特性不如 Go 顺手
- 维护税偏高

### 6.3 不推荐把后端做成 ORM-first
原因：
- 这个系统天然 Postgres-heavy
- 时间序列、聚合、保留、事件查询都更适合 SQL-first

### 6.4 不推荐 GraphQL / gRPC-first
原因：
- 超出 V1 需要
- 增加学习与调试成本

---

## 7. 当前推荐技术组合（可直接作为实施前基线）

如果只收成一套简版推荐方案，就是：

### 中心控制面
- Go
- chi + net/http
- pgx + sqlc
- raw SQL migrations
- 进程内后台归并 worker

### 前端
- React + TypeScript
- Vite
- React Router
- TanStack Query
- Tailwind CSS
- Radix Primitives
- TanStack Table
- ECharts

### agent
- Go
- systemd
- 纯 Go 短期 durable buffer

### 数据库
- PostgreSQL

### 部署
- 单体中心 + PostgreSQL + N 个 agent

---

## 8. 仍保留到实施前收口的小项

这些项仍然重要，但已经不是“技术路线”级别的问题：

- Go 版本基线最终定值
- React / Vite / Router / Query / Tailwind 具体大版本锁定
- 纯 Go 本地 durable buffer 最终选包
- migration 执行器最终选型
- 反向代理是否固定为 Caddy

换句话说：

> **技术路线已经可以先定，包级 / 版本级细节留到实施前收口。**

---

## 9. 当前结论

如果把这份技术选型浓缩成一句话，就是：

> **V1 最合适的技术路线，是 Go 负责中心控制面与 agent，React/Vite 负责前端，PostgreSQL 负责唯一数据库；整体保持单体、SQL-first、低维护、二进制优先。**

这条路线最符合当前已经冻结的产品边界，也最适合后续真正进入开发。

---

## 10. 官方参考（本轮选型依据）

- Go documentation: https://go.dev/doc/
- Vite guide: https://vite.dev/guide/
- React learn: https://react.dev/learn
- React Router docs: https://reactrouter.com/
- TanStack Query docs: https://tanstack.com/query/latest/docs/framework/react/overview
- Tailwind CSS docs: https://tailwindcss.com/docs
- Radix UI Primitives: https://www.radix-ui.com/primitives
- ECharts handbook: https://echarts.apache.org/handbook/en/get-started/
- Fastify docs: https://fastify.dev/
- FastAPI docs: https://fastapi.tiangolo.com/
- chi router: https://github.com/go-chi/chi
- pgx: https://github.com/jackc/pgx
- sqlc: https://sqlc.dev/
- PostgreSQL partitioning docs: https://www.postgresql.org/docs/current/ddl-partitioning.html
