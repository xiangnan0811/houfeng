---
date: 2026-04-22
tags:
  - 自建服务
  - 监控
  - 设计
  - 开发计划
status: frozen
---

# 服务器舰队控制面 v1 详细设计：架构、数据与模型

> 相关：[[服务器舰队控制面-v1-设计总览]]、[[服务器舰队控制面-v1-详细设计-异常、交互与默认规则]]

## 1. 系统定位

v1 的定位是：

> **面向个人多服务器舰队的异常发现与观测控制面**

### v1 明确不做

- 任意脚本执行平台
- Docker / 容器编排
- 完整资产管理 / CMDB
- 多用户 / 多租户 / 权限系统
- 复杂规则引擎
- 外部 MQ / 流处理基础设施
- benchmark / 跑分 / 高级超售诊断

## 2. 系统拓扑

### 中心控制面

- 单体服务
- 负责：
  - UI
  - API
  - 配置下发
  - 原始观测接收
  - 后台归并
  - 异常判定
  - 健康状态归并
  - Telegram 通知

### 数据库

- 一个 PostgreSQL
- 存放：
  - Node
  - Target
  - ProbeItem
  - HostSample / ProbeObservation
  - ActiveIncident
  - StateChangeEvent
  - NotificationRecord
  - 各类快照与缓存

### Agent

- 常驻 systemd 二进制
- 宿主机视角
- 主动访问中心
- 不暴露额外管理端口
- 具备短期持久化缓冲

## 3. 数据链路

### 设计原则

- agent 只负责**观测**
- center 负责**解释**
- 上报请求先做原始观测入库
- 异常 / 健康状态 / 通知由单体内部后台处理

### Agent 同步循环

1. 本地采集到期任务
2. 结果写入本地短期缓冲
3. 向中心同步未确认批次
4. 拉取最新配置与计划

### 中心处理顺序

1. 认证与身份校验
2. 原始观测写入库
3. 返回确认与最新配置
4. 后台执行：
   - 规范化
   - 规则评估
   - 去抖 / 持续确认
   - 健康状态归并
   - 事件流生成
   - Telegram 通知决策

### 维护模式语义

- 继续采集 / 继续探针
- 结果入库
- observation 带 maintenance 标记
- 不参与异常成立
- 不影响健康状态
- 不触发 Telegram

## 4. 核心对象模型

## 4.1 Node

### 语义

`Node = 一台具体服务器`

- 同机重装：仍算同一个 Node
- 换机器：必须新建 Node
- 历史永远绑定到具体服务器

### 最小管理集

#### 必填

- `display_name`
- `region`
- `city`
- `provider`
- `lifecycle_status`

#### 可选

- `labels`
- `note`

#### 系统生成 / 维护

- `node_id`
- `enrollment_token_hash`
- `binding_fingerprint`
- `binding_status`
- `monitoring_status`
- `current_health_status`
- `last_heartbeat_at`
- `last_sync_at`
- `current_active_incident_count`
- `current_primary_issue_summary`

### 两层状态

#### 生命周期状态

- 待接入
- 在用
- 观察中
- 不续费
- 已退役

#### 监控运行状态

- 启用
- 维护中
- 暂停

### 绑定状态

- 未绑定
- 已绑定
- 指纹变更待确认

## 4.2 Target

### 语义

`Target = 一个明确的可观测入口`

例如：

- `blog.example.com`
- `api.example.com`
- `1.2.3.4:443`
- 某个固定国内参考域名

### 最小业务集

#### 必填

- `name`
- `target_type`（`service` / `china_reference`）
- `host`
- `execution_node_labels`
- `run_status`

#### 可选

- `base_port`
- `labels`
- `note`

#### 系统维护 / 派生

- `target_id`
- `current_health_status`
- `current_active_incident_count`
- `last_success_at`
- `last_failure_at`
- `current_primary_issue_summary`

### 运行状态

- 启用
- 维护中
- 暂停
- 已归档

### 地址模型

- `host`：必填
- `base_port`：可选

目标地址主要属于 `Target`，`ProbeItem` 只补充“如何观测它”的细节。

## 4.3 ProbeItem

### 语义

挂在 `Target` 下的一种具体观测方式。

### v1 仅支持

- TCP
- HTTP/HTTPS
- TLS

### 公共字段

- `probe_item_id`
- `target_id`
- `probe_kind`
- `enabled`
- `frequency_tier`
- `timeout`
- `created_at`
- `updated_at`

### 类型配置（受控 schema）

#### TCP

- `port`

#### HTTP

- `scheme`
- `path`
- `method`
- `expected_status_range`

#### TLS

- `port`
- `expiry_warning_days`

### 状态

ProbeItem 只保留简单启停语义：

- `enabled = true`
- `enabled = false`

不承担复杂运行状态。

## 4.4 原始事实对象

### NodeHeartbeat

- `node_id`
- `observed_at`
- `received_at`
- `agent_version`
- `fingerprint`
- `sync_batch_id`
- `is_backfilled`

### HostSample

- `node_id`
- `observed_at`
- `received_at`
- `cpu_usage_pct`
- `load_1`
- `load_5`
- `load_15`
- `mem_used_pct`
- `mem_available_bytes`
- `swap_used_pct`
- `disk_used_pct`
- `inode_used_pct`
- `net_in_bytes_per_sec`
- `net_out_bytes_per_sec`
- `cpu_iowait_pct`
- `cpu_steal_pct`
- `disk_read_bytes_per_sec`
- `disk_write_bytes_per_sec`
- `disk_busy_pct`（或等价简化信号）
- `uptime_seconds`
- `maintenance_context`
- `is_backfilled`
- `sync_batch_id`

### ProbeObservation

- `node_id`
- `target_id`
- `probe_item_id`
- `observed_at`
- `received_at`
- `result_kind`
- `latency_ms`
- `http_status`
- `tls_expiry_days`
- `error_code`
- `error_summary`
- `maintenance_context`
- `is_backfilled`
- `sync_batch_id`

## 4.5 派生对象

### ActiveIncident

表示当前仍活跃的问题项。

典型字段：

- `incident_id`
- `object_type`（node / target）
- `object_id`
- `incident_class`
- `severity`
- `started_at`
- `last_evaluated_at`
- `status`
- `source_summary`

### StateChangeEvent

表示重要变化：

- 异常开始 / 恢复
- 健康状态变化
- 维护开始 / 结束
- 绑定冲突
- 节点恢复

### NotificationRecord

表示通知已发送或已抑制的历史记录。

## 5. 状态机草案

## 5.1 Node 生命周期状态机

- 待接入 -> 在用 / 观察中 / 已退役
- 在用 -> 观察中 / 不续费 / 已退役
- 观察中 -> 在用 / 不续费 / 已退役
- 不续费 -> 在用 / 观察中 / 已退役
- 已退役 -> 观察中（显式恢复）

## 5.2 Node 监控运行状态机

- 启用 -> 维护中 / 暂停
- 维护中 -> 启用 / 暂停
- 暂停 -> 启用

## 5.3 Node 绑定状态机

- 未绑定 -> 已绑定
- 已绑定 -> 指纹变更待确认
- 指纹变更待确认 -> 已绑定（确认重绑定或拒绝新指纹）

## 5.4 Target 运行状态机

- 启用 -> 维护中 / 暂停 / 已归档
- 维护中 -> 启用 / 暂停 / 已归档
- 暂停 -> 启用 / 已归档
- 已归档 -> 暂停（保守恢复路径）

## 6. 模型边界原则

1. **Node 管机器**
2. **Target 管入口**
3. **ProbeItem 管观测方式**
4. **HostSample / ProbeObservation 管事实**
5. **Incident / Event / Notification 管解释结果**
6. 健康状态是派生字段，不是手工配置
7. 生命周期状态是管理字段，不是派生字段
8. 维护模式是运行控制字段，不是健康状态

## 7. 备注

这一版模型刻意保持：

- 对象少
- 状态分层清楚
- 地址归属清楚
- 事实层与解释层分离

后续如果增加资产增强、特殊脚本任务或 benchmark，都应优先作为扩展层，而不是回头污染这组核心对象。

